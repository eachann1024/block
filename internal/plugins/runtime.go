package plugins

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"block/internal/files"
)

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	action  *systemAction
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (err *rpcError) Error() string {
	return fmt.Sprintf("rpc error (%d): %s", err.Code, err.Message)
}

type PerfBreakdownEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Bytes uint64 `json:"bytes"`
	Count int    `json:"count,omitempty"`
}

type PerfDiskCategory struct {
	ID        string               `json:"id"`
	Label     string               `json:"label"`
	Detail    string               `json:"detail"`
	Bytes     uint64               `json:"bytes"`
	FileCount int                  `json:"file_count,omitempty"`
	Entries   []PerfBreakdownEntry `json:"entries,omitempty"`
}

type PerfSnapshot struct {
	CPUPercent float64            `json:"cpu_percent"`
	RSSBytes   uint64             `json:"rss_bytes"`
	TotalBytes uint64             `json:"total_bytes"`
	LastScan   string             `json:"last_scan"`
	ScanError  string             `json:"scan_error,omitempty"`
	Categories []PerfDiskCategory `json:"categories"`
}

type HostConfig struct {
	PluginDir       string
	DataDir         string
	DownloadsDir    string
	NotebookRoot    string
	ActiveNote      string
	Locale          string
	Theme           string
	Tarot           bool
	HostVersion     string
	GetNotebooks    func() []string
	GetPerfSnapshot func(refreshDisk bool) (*PerfSnapshot, error)
	ApplyNoteWrite  func(absPath, content string, write func() (bool, error)) (bool, error)
	// ApplyNoteTrash runs verify then trash under the editor save lock so an
	// open note with unsaved edits is never moved to Trash by a plugin.
	ApplyNoteTrash   func(absPath string, verify, trash func() error) error
	OnNoteWritten    func(pluginID, notebookRoot, absPath string)
	OnActionProgress func(rt *Runtime, progress ActionProgress)
	OpenNote         func(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte) error
	// ContribHost handles host.titlebar/status/ui/editor/sidebar methods; ok=false falls through.
	ContribHost       func(rt *Runtime, method string, params json.RawMessage, action *systemAction) (any, *rpcError, bool)
	SystemIntegration bool
	credentialScope   string
}

var ErrNoteConflict = errors.New("note changed; read it again before writing")

type ActionResult struct {
	OK              *bool          `json:"ok,omitempty"`
	Tone            string         `json:"tone,omitempty"`
	Values          map[string]any `json:"values,omitempty"`
	Page            *UIPage        `json:"page,omitempty"`
	Status          string         `json:"status,omitempty"`
	OpenPath        string         `json:"open_path,omitempty"`
	RevealPath      string         `json:"reveal_path,omitempty"`
	SyncedNotebooks []string       `json:"synced_notebooks,omitempty"`
}

const (
	maxStderrBytes = 64 * 1024
	// host.log caps so a plugin cannot flood block.log.
	maxPluginLogEntries = 200
	maxPluginLogField   = 400
	maxPluginStorage    = 8 * 1024 * 1024
	maxPluginNote       = 16 * 1024 * 1024
	maxPluginRPCLine    = maxPluginNote*6 + 2*1024*1024 // worst-case 6x JSON \u00xx escape + RPC envelope
	maxPluginHostRPCs   = 8
)

type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	head int
	full bool
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		buf: make([]byte, 0, min(capacity, 4096)),
		cap: capacity,
	}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if r.cap <= 0 {
		return n, nil
	}
	if n >= r.cap {
		if cap(r.buf) < r.cap {
			r.buf = make([]byte, r.cap)
		} else {
			r.buf = r.buf[:r.cap]
		}
		copy(r.buf, p[n-r.cap:])
		r.head = 0
		r.full = true
		return n, nil
	}
	if !r.full {
		if len(r.buf)+n <= r.cap {
			r.buf = append(r.buf, p...)
			if len(r.buf) == r.cap {
				r.full = true
				r.head = 0
			}
			return n, nil
		}
		oldLen := len(r.buf)
		fullBuf := make([]byte, r.cap)
		copy(fullBuf, r.buf)
		r.buf = fullBuf
		r.full = true
		r.head = oldLen
	}
	first := min(n, r.cap-r.head)
	copy(r.buf[r.head:], p[:first])
	if first < n {
		copy(r.buf[0:], p[first:])
	}
	r.head = (r.head + n) % r.cap
	return n, nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return string(r.buf)
	}
	out := make([]byte, r.cap)
	tail := r.cap - r.head
	copy(out, r.buf[r.head:])
	copy(out[tail:], r.buf[:r.head])
	return string(out)
}

// Runtime hosts a single out-of-process plugin speaking newline-delimited
// bidirectional JSON-RPC 2.0 over stdin/stdout.
type Runtime struct {
	exited   chan struct{}
	reaped   chan struct{}
	manifest Manifest
	cfg      HostConfig
	dataRoot *os.Root

	cmd    *exec.Cmd
	stdin  *os.File
	stdout io.ReadCloser
	stderr *ringBuffer

	writeGate    chan struct{}
	callGate     chan struct{}
	hostCalls    chan struct{}
	pendingMu    sync.Mutex
	pending      map[string]chan rpcEnvelope
	nextID       atomic.Int64
	systemAction atomic.Pointer[systemAction]

	noteMu       sync.Mutex
	cfgMu        sync.RWMutex
	closed       atomic.Bool
	shuttingDown atomic.Bool
	// initialized is set once plugin.initialize succeeds; events wait for it.
	initialized atomic.Bool
	syncedEpoch atomic.Uint64
	abortOnce   sync.Once
}

func (rt *Runtime) SetNotebookRoot(root string) {
	rt.cfgMu.Lock()
	rt.cfg.NotebookRoot = root
	rt.cfgMu.Unlock()
}

func (rt *Runtime) SetActiveNote(relPath string) {
	rt.cfgMu.Lock()
	rt.cfg.ActiveNote = filepath.ToSlash(strings.TrimSpace(relPath))
	rt.cfgMu.Unlock()
}

func normalizeLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		return "en"
	}
	return "zh"
}

func (rt *Runtime) locale() string {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return normalizeLocale(rt.cfg.Locale)
}

func (rt *Runtime) notebookRoot() string {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return rt.cfg.NotebookRoot
}

func (rt *Runtime) activeNote() string {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return rt.cfg.ActiveNote
}

func StartRuntime(ctx context.Context, m Manifest, cfg HostConfig) (*Runtime, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	cmdName, err := pluginCommand(cfg.PluginDir, m.Entry.Command[0])
	if err != nil {
		return nil, err
	}
	dataRoot, err := openPluginDataRoot(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open plugin data directory: %w", err)
	}
	started := false
	defer func() {
		if !started {
			dataRoot.Close()
		}
	}()
	downloadsDir := strings.TrimSpace(cfg.DownloadsDir)
	if downloadsDir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			downloadsDir = filepath.Join(home, "Downloads")
		}
	}
	cfg.DownloadsDir = downloadsDir

	cmd := exec.CommandContext(ctx, cmdName, m.Entry.Command[1:]...)
	configurePluginProcess(cmd)
	cmd.WaitDelay = time.Second
	cmd.Dir = cfg.PluginDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.TempDir(),
		"PYTHONUNBUFFERED=1",
		"BLOCK_PLUGIN_ID=" + m.ID,
		"BLOCK_DATA_DIR=" + cfg.DataDir,
		"BLOCKMD_PLUGIN_ID=" + m.ID,
		"BLOCKMD_DATA_DIR=" + cfg.DataDir,
		"BLOCKMD_DOWNLOADS_DIR=" + downloadsDir,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Own the stdout pipe instead of cmd.StdoutPipe: Wait must run while the
	// read loop is still draining, so a crashed plugin whose children keep
	// the pipe open can still be reaped.
	stdout, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stdout = stdoutW
	rt := &Runtime{
		exited:    make(chan struct{}),
		reaped:    make(chan struct{}),
		manifest:  m,
		cfg:       cfg,
		dataRoot:  dataRoot,
		cmd:       cmd,
		stdin:     stdin.(*os.File),
		stdout:    stdout,
		stderr:    newRingBuffer(maxStderrBytes),
		pending:   make(map[string]chan rpcEnvelope),
		writeGate: make(chan struct{}, 1),
		callGate:  make(chan struct{}, 1),
		hostCalls: make(chan struct{}, maxPluginHostRPCs),
	}
	cmd.Stderr = rt.stderr

	if err := cmd.Start(); err != nil {
		files.LogErrorf("plugin start failed: id=%s version=%s err=%v", m.ID, m.Version, err)
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("start plugin process %v: %w", m.Entry.Command, err)
	}
	files.LogInfof("plugin started: id=%s version=%s", m.ID, m.Version)
	_ = stdoutW.Close()
	started = true

	readDone := make(chan struct{})
	go func() {
		rt.readLoop(stdout)
		close(readDone)
	}()
	go func() {
		waitErr := cmd.Wait()
		if !rt.shuttingDown.Load() && ctx.Err() == nil {
			files.LogErrorf("plugin exited unexpectedly: id=%s err=%v", rt.manifest.ID, waitErr)
		}
		select {
		case <-readDone:
		case <-time.After(time.Second):
			// Leftover children still hold stdout: stop the process group.
			_ = stopPluginProcess(cmd)
			_ = stdout.Close()
			<-readDone
		}
		_ = stdout.Close()
		_ = dataRoot.Close()
		close(rt.reaped)
	}()
	return rt, nil
}

func pluginCommand(pluginDir, command string) (string, error) {
	commandPath := filepath.FromSlash(strings.ReplaceAll(command, "\\", "/"))
	if strings.Contains(commandPath, ":") || !filepath.IsLocal(commandPath) || filepath.Clean(commandPath) == "." {
		return "", errors.New("plugin command must be a PATH executable or a file within the plugin")
	}
	if commandPath == filepath.Base(commandPath) {
		return exec.LookPath(commandPath)
	}
	name := filepath.Clean(commandPath)
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := checkPluginParents(root, name); err != nil {
		return "", err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("bundled plugin command must be a regular file")
	}
	return filepath.Abs(filepath.Join(pluginDir, name))
}

func openPluginDataRoot(dataDir string) (*os.Root, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("plugin data directory is required")
	}
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return nil, errors.New("plugin data directory cannot be a filesystem root")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(dir)
	if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("plugin data path must be a directory")
	}
	dataRoot, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := dataRoot.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		dataRoot.Close()
		return nil, errors.New("plugin data directory changed while opening")
	}
	return dataRoot, nil
}

func (rt *Runtime) Stderr() string {
	if rt.stderr == nil {
		return ""
	}
	return rt.stderr.String()
}

func (rt *Runtime) knownNotebooks() []string {
	rt.cfgMu.RLock()
	active := strings.TrimSpace(rt.cfg.NotebookRoot)
	getter := rt.cfg.GetNotebooks
	rt.cfgMu.RUnlock()

	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	add := func(raw string) {
		p := strings.TrimSpace(raw)
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		} else {
			p = filepath.Clean(p)
		}
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	if getter != nil {
		for _, nb := range getter() {
			add(nb)
		}
		return out
	}
	add(active)
	return out
}

func (rt *Runtime) resolveNotebookRoot(requested string) (string, error) {
	req := strings.TrimSpace(requested)
	if req == "" {
		nbRoot := rt.notebookRoot()
		if strings.TrimSpace(nbRoot) == "" {
			return "", errors.New("no active notebook folder")
		}
		reqAbs, err := filepath.Abs(nbRoot)
		if err != nil {
			return "", err
		}
		for _, allowed := range rt.knownNotebooks() {
			if allowed == reqAbs {
				return reqAbs, nil
			}
		}
		return "", fmt.Errorf("notebook root %q is no longer registered in host", nbRoot)
	}
	reqAbs, err := filepath.Abs(req)
	if err != nil {
		return "", err
	}
	for _, allowed := range rt.knownNotebooks() {
		if allowed == reqAbs {
			return reqAbs, nil
		}
	}
	return "", fmt.Errorf("notebook root %q is not registered in host", requested)
}

func (rt *Runtime) Initialize(ctx context.Context) error {
	locale := rt.cfg.Locale
	if locale == "" {
		locale = "zh"
	}
	ver := rt.cfg.HostVersion
	if ver == "" {
		ver = "1.0.301"
	}
	var notebookRoot string
	notebooks := []string{}
	if rt.manifest.HasPermission(PermNotesRead) || rt.manifest.HasPermission(PermNotesWrite) ||
		rt.manifest.HasPermission(PermEventNoteSaved) {
		notebookRoot = rt.notebookRoot()
	}
	if rt.manifest.HasPermission(PermNotesRead) {
		notebooks = rt.knownNotebooks()
	}
	themeName := strings.TrimSpace(rt.cfg.Theme)
	if themeName == "" {
		themeName = "system"
	}
	capabilities := []string{
		"kv_tooltip", "treemap", "host_perf", "checklist", "form_layout", "multi_notebook",
		"card_collapse", "metric_tone", "log_columns", "file_picker", "action_feedback", "note_revision",
		"sidebar_sections", "file_badges", "sidebar_menu", "backdrops", "status_icon", "toast_icon",
		"notes_open", "log_actions", "action_progress", "panel_toggle", "editor_insert_mode", "dialog_token",
		"checklist_add_folder", "item_icons", "tab_menu", "file_path", "log_timeline",
		"progress_bar", "segmented_status", "toast_banner", "open_settings",
	}
	if rt.cfg.SystemIntegration {
		capabilities = append(capabilities, "system_integration")
	}
	// Every desktop host has menu bar icons; only macOS takes drops on them.
	capabilities = append(capabilities, "menubar")
	if goruntime.GOOS == "darwin" {
		capabilities = append(capabilities, "menubar_drop")
	}
	capabilities = append(capabilities, "store_changelog")
	params := map[string]any{
		"plugin_id":       rt.manifest.ID,
		"data_dir":        rt.cfg.DataDir,
		"downloads_dir":   rt.cfg.DownloadsDir,
		"notebook_root":   notebookRoot,
		"notebooks":       notebooks,
		"locale":          locale,
		"theme":           themeName,
		"tarot":           rt.cfg.Tarot,
		"host_version":    ver,
		"ui_capabilities": capabilities,
	}
	var res struct {
		Ready bool `json:"ready"`
	}
	if err := rt.call(ctx, "plugin.initialize", params, &res); err != nil {
		return err
	}
	if !res.Ready {
		return errors.New("plugin.initialize did not return {\"ready\": true}")
	}
	rt.initialized.Store(true)
	return nil
}

func (rt *Runtime) Render(ctx context.Context, surface Surface) (*UIPage, error) {
	return rt.RenderWithValues(ctx, surface, nil)
}

func (rt *Runtime) RenderWithValues(ctx context.Context, surface Surface, values map[string]any) (*UIPage, error) {
	page, _, err := rt.RenderWithValuesResult(ctx, surface, values)
	return page, err
}

func (rt *Runtime) RenderWithValuesResult(ctx context.Context, surface Surface, values map[string]any) (*UIPage, map[string]any, error) {
	if !rt.manifest.HasSurface(surface) {
		return nil, nil, fmt.Errorf("surface %q not declared in manifest", surface)
	}
	var rawRes struct {
		Page   json.RawMessage `json:"page"`
		Values map[string]any  `json:"values,omitempty"`
	}
	params := map[string]any{"surface": string(surface)}
	if len(values) > 0 {
		params["values"] = values
	}
	if err := rt.call(ctx, "plugin.render", params, &rawRes); err != nil {
		return nil, nil, err
	}
	if len(rawRes.Page) == 0 {
		return nil, nil, errors.New("plugin.render response missing \"page\" object")
	}
	page, err := DecodePageStrict(rawRes.Page)
	if err != nil {
		return nil, nil, err
	}
	return page, rawRes.Values, nil
}

func (rt *Runtime) Action(ctx context.Context, surface Surface, actionID string, values map[string]any) (*ActionResult, error) {
	return rt.ActionWithDialogToken(ctx, surface, actionID, values, "")
}

func (rt *Runtime) ActionWithDialogToken(ctx context.Context, surface Surface, actionID string, values map[string]any, dialogToken string) (*ActionResult, error) {
	if values == nil {
		values = map[string]any{}
	}
	var rawRes struct {
		OK              *bool           `json:"ok,omitempty"`
		Tone            string          `json:"tone,omitempty"`
		Values          map[string]any  `json:"values,omitempty"`
		Page            json.RawMessage `json:"page,omitempty"`
		Status          string          `json:"status,omitempty"`
		OpenPath        string          `json:"open_path,omitempty"`
		RevealPath      string          `json:"reveal_path,omitempty"`
		SyncedNotebooks []string        `json:"synced_notebooks,omitempty"`
	}
	params := map[string]any{
		"surface":   string(surface),
		"action_id": actionID,
		"values":    values,
	}
	if strings.TrimSpace(dialogToken) != "" {
		params["dialog_token"] = strings.TrimSpace(dialogToken)
	}
	if err := rt.call(ctx, "plugin.action", params, &rawRes); err != nil {
		return nil, err
	}
	out := &ActionResult{
		OK:              rawRes.OK,
		Tone:            strings.TrimSpace(rawRes.Tone),
		Values:          rawRes.Values,
		Status:          rawRes.Status,
		OpenPath:        strings.TrimSpace(rawRes.OpenPath),
		RevealPath:      strings.TrimSpace(rawRes.RevealPath),
		SyncedNotebooks: rawRes.SyncedNotebooks,
	}
	if len(rawRes.Page) > 0 && string(rawRes.Page) != "null" {
		page, err := DecodePageStrict(rawRes.Page)
		if err != nil {
			return nil, fmt.Errorf("plugin.action(%s) invalid page: %w", actionID, err)
		}
		out.Page = page
		if out.Page != nil {
			out.Status = strings.TrimSpace(out.Status)
		}
	}
	return out, nil
}

func (rt *Runtime) NotifyNoteSaved(ctx context.Context, notebookRoot, relPath string) error {
	_, err := rt.NotifyNoteSavedResult(ctx, notebookRoot, relPath)
	return err
}

func (rt *Runtime) NotifyNoteSavedResult(ctx context.Context, notebookRoot, relPath string) (string, error) {
	if !rt.manifest.HasPermission(PermEventNoteSaved) {
		return "", nil
	}
	nbRoot := strings.TrimSpace(notebookRoot)
	if nbRoot == "" {
		nbRoot = rt.notebookRoot()
	}
	params := map[string]any{
		"event":         "note.saved",
		"notebook_root": nbRoot,
		"path":          filepath.ToSlash(relPath),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	var res struct {
		Handled bool   `json:"handled"`
		Outcome string `json:"outcome,omitempty"`
	}
	if err := rt.call(ctx, "plugin.event", params, &res); err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Outcome), nil
}

func (rt *Runtime) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rt.shuttingDown.Store(true)
	if !rt.closed.Swap(true) {
		_ = rt.call(ctx, "plugin.shutdown", map[string]any{}, nil)
		_ = rt.stdin.Close()
	}
	select {
	case <-rt.reaped:
		return nil
	case <-ctx.Done():
		rt.abort()
		select {
		case <-rt.reaped:
			return nil
		case <-time.After(time.Second):
			return errors.New("plugin process did not exit")
		}
	}
}

func (rt *Runtime) abort() {
	rt.closed.Store(true)
	rt.abortOnce.Do(func() {
		select {
		case <-rt.reaped:
		default:
			_ = stopPluginProcess(rt.cmd)
		}
		_ = rt.stdin.Close()
		_ = rt.stdout.Close()
	})
}

func isInteractivePluginRPC(method string) bool {
	return method == "plugin.action" || method == "plugin.command"
}

func (rt *Runtime) call(ctx context.Context, method string, params any, out any) error {
	select {
	case rt.callGate <- struct{}{}:
		defer func() { <-rt.callGate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.exited:
		return errors.New("plugin exited")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if rt.closed.Load() && method != "plugin.shutdown" {
		return errors.New("plugin is stopping")
	}
	stopAbort := context.AfterFunc(ctx, rt.abort)
	defer stopAbort()
	idNum := rt.nextID.Add(1)
	idKey := fmt.Sprintf("%d", idNum)
	idRaw := json.RawMessage(idKey)
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return err
	}

	ch := make(chan rpcEnvelope, 1)
	rt.pendingMu.Lock()
	rt.pending[idKey] = ch
	rt.pendingMu.Unlock()
	defer func() {
		rt.pendingMu.Lock()
		delete(rt.pending, idKey)
		rt.pendingMu.Unlock()
	}()

	req := rpcEnvelope{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  paramsRaw,
	}
	if scope, ok := ctx.Value(systemActionKey{}).(systemAction); ok && isInteractivePluginRPC(method) {
		scope.ctx, scope.rpcID = ctx, idKey
		rt.systemAction.Store(&scope)
		defer rt.systemAction.CompareAndSwap(&scope, nil)
	}
	if err := rt.writeLine(ctx, req); err != nil {
		rt.abort()
		return fmt.Errorf("send %s: %w (stderr: %s)", method, err, strings.TrimSpace(rt.Stderr()))
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("%s %w", method, resp.Error)
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	case <-rt.exited:
		return fmt.Errorf("%s: plugin exited", method)
	case <-ctx.Done():
		rt.abort()
		return fmt.Errorf("%s timed out: %w (stderr: %s)", method, ctx.Err(), strings.TrimSpace(rt.Stderr()))
	}
}

func (rt *Runtime) writeLine(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case rt.writeGate <- struct{}{}:
		defer func() { <-rt.writeGate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.exited:
		return errors.New("plugin exited")
	}
	deadline, _ := ctx.Deadline()
	if err := rt.stdin.SetWriteDeadline(deadline); err != nil && !errors.Is(err, os.ErrNoDeadline) {
		return err
	}
	b = append(b, '\n')
	_, err = rt.stdin.Write(b)
	return err
}

func (rt *Runtime) readLoop(r io.Reader) {
	defer close(rt.exited)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxPluginRPCLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var env rpcEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Method != "" {
			if env.Method == "host.action.progress" {
				// Process all progress updates inline in readLoop to guarantee strict ordering
				// across notifications and calls, and prevent semaphore saturation on tight loops.
				res, rpcErr := rt.dispatchHostMethod(env.Method, env.Params, nil)
				trimmedID := bytes.TrimSpace(env.ID)
				if len(trimmedID) > 0 && !bytes.Equal(trimmedID, []byte("null")) {
					select {
					case rt.hostCalls <- struct{}{}:
						go func(reqID json.RawMessage, result any, callErr *rpcError) {
							defer func() { <-rt.hostCalls }()
							rt.writeHostResponse(reqID, result, callErr)
						}(env.ID, res, rpcErr)
					default:
						rt.abort()
						return
					}
				}
				continue
			}
			env.action = rt.systemAction.Load()
			select {
			case rt.hostCalls <- struct{}{}:
				go func() {
					defer func() { <-rt.hostCalls }()
					rt.handleHostCall(env)
				}()
			default:
				rt.abort()
				return
			}
			continue
		}
		idKey := normalizeID(env.ID)
		if scope := rt.systemAction.Load(); scope != nil && scope.rpcID == idKey {
			rt.systemAction.CompareAndSwap(scope, nil)
		}
		rt.pendingMu.Lock()
		ch, ok := rt.pending[idKey]
		rt.pendingMu.Unlock()
		if ok {
			select {
			case ch <- env:
			default:
			}
		}
	}
	if sc.Err() != nil {
		rt.abort()
	}
}

func normalizeID(raw json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(raw)), "\"")
}

func (rt *Runtime) handleHostCall(req rpcEnvelope) {
	if rt.closed.Load() {
		return
	}
	res, rpcErr := rt.dispatchHostMethod(req.Method, req.Params, req.action)
	trimmedID := bytes.TrimSpace(req.ID)
	if len(trimmedID) == 0 || bytes.Equal(trimmedID, []byte("null")) {
		// JSON-RPC 2.0 notification: no response is expected.
		return
	}
	rt.writeHostResponse(req.ID, res, rpcErr)
}

func (rt *Runtime) writeHostResponse(reqID json.RawMessage, res any, rpcErr *rpcError) {
	if rt.closed.Load() {
		return
	}
	resp := rpcEnvelope{
		JSONRPC: "2.0",
		ID:      reqID,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		raw, _ := json.Marshal(res)
		resp.Result = raw
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopAbort := context.AfterFunc(ctx, rt.abort)
	defer stopAbort()
	if err := rt.writeLine(ctx, resp); err != nil {
		rt.abort()
	}
}

func hasUnpairedSurrogateEscape(raw []byte) bool {
	inStr := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if !inStr {
			if b == '"' {
				inStr = true
			}
			continue
		}
		if b == '"' {
			inStr = false
			continue
		}
		if b != '\\' {
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		v, ok := parseHex4(raw[i+1 : i+5])
		if !ok {
			continue
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return true // lone low surrogate
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
				if v2, ok2 := parseHex4(raw[i+3 : i+7]); ok2 && v2 >= 0xDC00 && v2 <= 0xDFFF {
					i += 6
					continue
				}
			}
			return true // lone high surrogate
		}
	}
	return false
}

func parseHex4(b []byte) (uint16, bool) {
	if len(b) != 4 {
		return 0, false
	}
	var out uint16
	for _, c := range b {
		out <<= 4
		switch {
		case c >= '0' && c <= '9':
			out |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			out |= uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			out |= uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return out, true
}

func (rt *Runtime) dispatchHostMethod(method string, params json.RawMessage, action *systemAction) (any, *rpcError) {
	if len(params) > 0 && (!utf8.Valid(params) || hasUnpairedSurrogateEscape(params)) {
		return nil, &rpcError{Code: -32602, Message: "params must be valid UTF-8"}
	}
	switch method {
	case "host.storage.get":
		if !rt.manifest.HasPermission(PermStorage) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: storage"}
		}
		var p struct {
			Key string `json:"key"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		store, err := rt.loadStorageMap()
		if err != nil {
			return nil, &rpcError{Code: -32002, Message: err.Error()}
		}
		if p.Key == "" {
			return map[string]any{"data": store}, nil
		}
		val, ok := store[p.Key]
		if !ok {
			val = json.RawMessage("null")
		}
		return map[string]any{"value": val, "found": ok}, nil

	case "host.storage.set":
		if !rt.manifest.HasPermission(PermStorage) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: storage"}
		}
		var p struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Key) == "" {
			return nil, &rpcError{Code: -32602, Message: "host.storage.set requires non-empty key"}
		}
		if err := rt.setStorageKey(p.Key, p.Value); err != nil {
			return nil, &rpcError{Code: -32002, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "host.storage.set_many":
		if !rt.manifest.HasPermission(PermStorage) {
			return nil, missingPerm(PermStorage)
		}
		var p struct {
			Values map[string]json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal(params, &p); err != nil || len(p.Values) == 0 || len(p.Values) > 64 {
			return nil, rpcErr(-32602, "values must contain 1–64 storage entries")
		}
		for key := range p.Values {
			if strings.TrimSpace(key) == "" {
				return nil, rpcErr(-32602, "storage keys must not be blank")
			}
		}
		if err := rt.setStorageValues(p.Values); err != nil {
			return nil, rpcErr(-32002, err.Error())
		}
		return map[string]any{"ok": true}, nil

	case "host.notes.active":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		return map[string]any{"path": rt.activeNote()}, nil

	case "host.notebooks.list":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		activeRoot := strings.TrimSpace(rt.notebookRoot())
		if activeRoot != "" {
			if abs, err := filepath.Abs(activeRoot); err == nil {
				activeRoot = abs
			}
		}
		type notebookEntry struct {
			Path   string `json:"path"`
			Name   string `json:"name"`
			Active bool   `json:"active"`
		}
		nbs := rt.knownNotebooks()
		entries := make([]notebookEntry, 0, len(nbs))
		for _, nb := range nbs {
			name := filepath.Base(nb)
			if name == "" || name == "." || name == string(filepath.Separator) {
				name = nb
			}
			entries = append(entries, notebookEntry{
				Path:   nb,
				Name:   name,
				Active: activeRoot != "" && nb == activeRoot,
			})
		}
		return map[string]any{"notebooks": entries}, nil

	case "host.notes.list":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		var p struct {
			NotebookRoot string `json:"notebook_root"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		notes, err := rt.listNotebookNotes(p.NotebookRoot)
		if err != nil {
			return nil, &rpcError{Code: -32003, Message: err.Error()}
		}
		return map[string]any{"notes": notes}, nil

	case "host.notes.read":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		var p struct {
			NotebookRoot string `json:"notebook_root"`
			Path         string `json:"path"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		rootPath, cleanRel, err := rt.notebookNotePath(p.NotebookRoot, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		defer root.Close()
		b, err := readPluginFile(root, cleanRel, maxPluginNote)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		if !utf8.Valid(b) {
			return nil, &rpcError{Code: -32004, Message: "note file is not valid UTF-8"}
		}
		return map[string]any{"path": filepath.ToSlash(cleanRel), "content": string(b), "revision": noteRevision(b)}, nil

	case "host.notes.write":
		if !rt.manifest.HasPermission(PermNotesWrite) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:write"}
		}
		var p struct {
			NotebookRoot     string  `json:"notebook_root"`
			Path             string  `json:"path"`
			Content          *string `json:"content"`
			ExpectedRevision *string `json:"expected_revision"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		// A missing or null content must never be read as "clear the note".
		if p.Content == nil {
			return nil, &rpcError{Code: -32602, Message: "host.notes.write requires a string content; pass \"\" to clear"}
		}
		content := *p.Content
		rootPath, cleanRel, err := rt.notebookNotePath(p.NotebookRoot, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		defer root.Close()
		if err := checkPluginParents(root, cleanRel); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		abs := filepath.Join(rootPath, cleanRel)
		_, priorErr := root.Lstat(cleanRel)
		created := errors.Is(priorErr, os.ErrNotExist)
		write := func() (bool, error) {
			if rt.closed.Load() {
				return false, errors.New("plugin is stopping")
			}
			// Re-verify notebook registration at commit point (serialized with main-thread
			// ApplyNoteWrite / notebook removal) so a removed notebook can never receive a write.
			if _, err := rt.resolveNotebookRoot(rootPath); err != nil {
				return false, err
			}
			return writePluginNote(root, cleanRel, content, p.ExpectedRevision)
		}
		var changed bool
		if rt.cfg.ApplyNoteWrite != nil {
			changed, err = rt.cfg.ApplyNoteWrite(abs, content, write)
		} else {
			rt.noteMu.Lock()
			changed, err = write()
			rt.noteMu.Unlock()
		}
		if errors.Is(err, ErrNoteConflict) {
			return nil, &rpcError{Code: -32006, Message: err.Error()}
		}
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		if changed {
			files.LogInfof("plugin note written: plugin=%s path=%s created=%t", rt.manifest.ID, abs, created)
		}
		if changed && rt.cfg.OnNoteWritten != nil {
			rt.cfg.OnNoteWritten(rt.manifest.ID, rootPath, abs)
		}
		return map[string]any{"ok": true, "changed": changed}, nil

	case "host.notes.trash":
		if !rt.manifest.HasPermission(PermNotesWrite) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:write"}
		}
		var p struct {
			NotebookRoot     string  `json:"notebook_root"`
			Path             string  `json:"path"`
			ExpectedRevision *string `json:"expected_revision"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		// Trashing is only allowed against a known revision so a plugin can never
		// discard content it has not seen.
		if p.ExpectedRevision == nil || strings.TrimSpace(*p.ExpectedRevision) == "" {
			return nil, &rpcError{Code: -32602, Message: "host.notes.trash requires expected_revision"}
		}
		expected := strings.ToLower(strings.TrimSpace(*p.ExpectedRevision))
		rootPath, cleanRel, err := rt.notebookFilePath(p.NotebookRoot, p.Path, false)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		defer root.Close()
		abs := filepath.Join(rootPath, cleanRel)
		verify := func() error {
			if rt.closed.Load() {
				return errors.New("plugin is stopping")
			}
			if _, err := rt.resolveNotebookRoot(rootPath); err != nil {
				return err
			}
			rev, err := pluginFileRevision(root, cleanRel)
			if err != nil {
				return err
			}
			if rev != expected {
				return ErrNoteConflict
			}
			return nil
		}
		trash := func() error { return files.TrashItem(abs) }
		if rt.cfg.ApplyNoteTrash != nil {
			err = rt.cfg.ApplyNoteTrash(abs, verify, trash)
		} else {
			rt.noteMu.Lock()
			if err = verify(); err == nil {
				err = trash()
			}
			rt.noteMu.Unlock()
		}
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"ok": true, "trashed": false}, nil
		}
		if errors.Is(err, ErrNoteConflict) {
			files.LogWarnf("plugin trash skipped (changed): plugin=%s path=%s", rt.manifest.ID, abs)
			return nil, &rpcError{Code: -32006, Message: err.Error()}
		}
		if err != nil {
			files.LogErrorf("plugin trash failed: plugin=%s path=%s err=%v", rt.manifest.ID, abs, err)
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		files.LogInfof("plugin trashed file: plugin=%s path=%s", rt.manifest.ID, abs)
		return map[string]any{"ok": true, "trashed": true}, nil

	case "host.notes.open":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		var p struct {
			NotebookRoot string `json:"notebook_root"`
			Path         string `json:"path"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		rootPath, cleanRel, err := rt.notebookNotePath(p.NotebookRoot, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		defer root.Close()
		if err := checkPluginParents(root, cleanRel); err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		info, err := root.Lstat(cleanRel)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		if !info.Mode().IsRegular() {
			return nil, &rpcError{Code: -32004, Message: "target note must be a regular file"}
		}
		if rt.cfg.OpenNote == nil {
			return nil, &rpcError{Code: -32005, Message: "host note open unavailable"}
		}
		rootInfo, err := root.Stat(".")
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		content, err := readPluginFile(root, cleanRel, maxPluginNote)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		if err := rt.cfg.OpenNote(rootPath, filepath.ToSlash(cleanRel), rootInfo, content); err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		return map[string]any{"ok": true, "path": filepath.ToSlash(cleanRel)}, nil

	case "host.log":
		var p struct {
			Entries []struct {
				Level        string `json:"level"`
				Event        string `json:"event"`
				NotebookRoot string `json:"notebook_root"`
				Path         string `json:"path"`
				Protocol     string `json:"protocol"`
				Detail       string `json:"detail"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		if len(p.Entries) > maxPluginLogEntries {
			p.Entries = p.Entries[:maxPluginLogEntries]
		}
		for _, e := range p.Entries {
			// Notebook-relative paths are logged as absolute paths so their masked
			// tags match the host's own note lines in block.log.
			target := pluginLogField(e.Path)
			if target != "" {
				if rootPath, cleanRel, err := rt.notebookFilePath(e.NotebookRoot, e.Path, false); err == nil {
					target = filepath.Join(rootPath, cleanRel)
				}
			}
			event := pluginLogField(e.Event)
			proto := pluginLogField(e.Protocol)
			detail := pluginLogField(e.Detail)
			switch strings.ToLower(strings.TrimSpace(e.Level)) {
			case "error":
				files.LogErrorf("plugin %s: event=%s protocol=%s path=%s detail=%s", rt.manifest.ID, event, proto, target, detail)
			case "warn", "warning":
				files.LogWarnf("plugin %s: event=%s protocol=%s path=%s detail=%s", rt.manifest.ID, event, proto, target, detail)
			default:
				files.LogInfof("plugin %s: event=%s protocol=%s path=%s detail=%s", rt.manifest.ID, event, proto, target, detail)
			}
		}
		return map[string]any{"ok": true}, nil

	case "host.action.progress":
		var p struct {
			Channel  string   `json:"channel,omitempty"`
			Progress *float64 `json:"progress,omitempty"`
			Done     int      `json:"done,omitempty"`
			Total    int      `json:"total,omitempty"`
			Label    string   `json:"label,omitempty"`
			Tone     string   `json:"tone,omitempty"`
			Running  *bool    `json:"running,omitempty"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &rpcError{Code: -32602, Message: "invalid host.action.progress params"}
			}
		}
		ch := strings.TrimSpace(p.Channel)
		if ch != "" {
			if err := validateProgressChannel("channel", ch); err != nil {
				return nil, &rpcError{Code: -32602, Message: err.Error()}
			}
		}
		tone := strings.ToLower(strings.TrimSpace(p.Tone))
		if err := validateStatusTone("tone", tone); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		lbl := strings.TrimSpace(p.Label)
		if runes := []rune(lbl); len(runes) > 32 {
			lbl = string(runes[:32])
		}
		ap := ActionProgress{
			Channel: ch,
			Label:   lbl,
			Tone:    tone,
		}
		if p.Total > 0 {
			ap.Total = p.Total
			ap.Done = min(max(p.Done, 0), p.Total)
		} else if p.Progress != nil {
			v := *p.Progress
			if !(v >= 0 && v <= 1) {
				// Reject NaN, Inf, or out-of-range ratios instead of clamping bogus values.
				return map[string]any{"ok": false}, nil
			}
			ap.HasProgress = true
			ap.Progress = v
		}
		if p.Running != nil {
			ap.Running = *p.Running
		} else if ch != "" {
			terminal := tone == "success" || tone == "danger" || tone == "error" || tone == "warning" || tone == "warn" ||
				(ap.HasProgress && ap.Progress >= 1) || (ap.Total > 0 && ap.Done >= ap.Total)
			ap.Running = !terminal
		}
		// Ignore empty or malformed payloads (e.g. {}, null, done without total) so they
		// never overwrite valid in-flight progress.
		if ap.Total == 0 && !ap.HasProgress && ap.Label == "" && ap.Channel == "" {
			return map[string]any{"ok": false}, nil
		}
		if rt.cfg.OnActionProgress != nil {
			rt.cfg.OnActionProgress(rt, ap)
		}
		return map[string]any{"ok": true}, nil

	case "host.perf.snapshot":
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:read"}
		}
		if rt.cfg.GetPerfSnapshot == nil {
			return nil, &rpcError{Code: -32005, Message: "host performance metrics unavailable"}
		}
		var p struct {
			RefreshDisk bool `json:"refresh_disk"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &p)
		}
		snap, err := rt.cfg.GetPerfSnapshot(p.RefreshDisk)
		if err != nil {
			return nil, &rpcError{Code: -32005, Message: err.Error()}
		}
		return snap, nil

	default:
		if rt.cfg.ContribHost != nil {
			if res, rerr, ok := rt.cfg.ContribHost(rt, method, params, action); ok {
				return res, rerr
			}
		}
		return nil, &rpcError{Code: -32601, Message: "unknown host method: " + method}
	}
}

// pluginStorageLocks serializes state.json access per data directory, so an
// old runtime still finishing a request cannot overwrite what its replacement
// wrote after a disable/enable or restart.
var pluginStorageLocks sync.Map // cleaned DataDir -> *sync.Mutex

func (rt *Runtime) storageLock() *sync.Mutex {
	mu, _ := pluginStorageLocks.LoadOrStore(filepath.Clean(rt.cfg.DataDir), &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func (rt *Runtime) loadStorageMap() (map[string]json.RawMessage, error) {
	mu := rt.storageLock()
	mu.Lock()
	defer mu.Unlock()
	return rt.readStorageMap()
}

// readStorageMap keeps every value as raw JSON so numbers such as 64-bit
// cursors round-trip exactly instead of passing through float64.
func (rt *Runtime) readStorageMap() (map[string]json.RawMessage, error) {
	b, err := readPluginFile(rt.dataRoot, "state.json", maxPluginStorage)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]json.RawMessage{}, nil
	}
	return out, nil
}

func readPluginStorageMap(root *os.Root) (map[string]any, error) {
	b, err := readPluginFile(root, "state.json", maxPluginStorage)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (rt *Runtime) setStorageKey(key string, val json.RawMessage) error {
	return rt.setStorageValues(map[string]json.RawMessage{key: val})
}

func (rt *Runtime) setStorageValues(values map[string]json.RawMessage) error {
	mu := rt.storageLock()
	mu.Lock()
	defer mu.Unlock()
	cur, err := rt.readStorageMap()
	if err != nil {
		return err
	}
	for key, val := range values {
		if len(val) == 0 {
			val = json.RawMessage("null")
		}
		cur[key] = val
	}
	// Compact JSON: indentation grows quadratically with nesting depth and
	// would allocate far past the limit before the size check below.
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	if len(raw) > maxPluginStorage {
		return errors.New("plugin storage exceeds size limit")
	}
	return files.WriteNoteAt(rt.dataRoot, "state.json", string(raw))
}

type NoteMeta struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

func (rt *Runtime) listNotebookNotes(requestedRoot string) ([]NoteMeta, error) {
	var nbRoot string
	if strings.TrimSpace(requestedRoot) == "" {
		nbRoot = rt.notebookRoot()
		if nbRoot == "" {
			return []NoteMeta{}, nil
		}
	} else {
		var err error
		nbRoot, err = rt.resolveNotebookRoot(requestedRoot)
		if err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(nbRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	list := []NoteMeta{}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".") && path != "." {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !files.IsMarkdown(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		list = append(list, NoteMeta{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	return list, err
}

func noteRevision(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func writePluginNote(root *os.Root, rel, content string, expected *string) (bool, error) {
	if !utf8.ValidString(content) {
		return false, errors.New("note content must be valid UTF-8")
	}
	if len(content) > maxPluginNote {
		return false, fmt.Errorf("note exceeds maximum size (%d bytes)", maxPluginNote)
	}
	prior, err := readPluginFile(root, rel, maxPluginNote)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	exists := err == nil
	if exists && !utf8.Valid(prior) {
		return false, errors.New("note file is not valid UTF-8")
	}
	if exists && bytes.Equal(prior, []byte(content)) {
		return false, nil
	}
	if expected != nil && ((!exists && *expected != "") || (exists && *expected != noteRevision(prior))) {
		return false, ErrNoteConflict
	}
	return files.WriteNoteConditionalAt(root, rel, content, expected, ErrNoteConflict)
}

func checkPluginParents(root *os.Root, name string) error {
	dir := filepath.Dir(name)
	if dir == "." {
		return nil
	}
	parent := ""
	for part := range strings.SplitSeq(dir, string(filepath.Separator)) {
		parent = filepath.Join(parent, part)
		info, err := root.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("plugin file parent must be a directory")
		}
	}
	return nil
}

func openPluginFile(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	if err := checkPluginParents(root, name); err != nil {
		return nil, nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("plugin file must be a regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		file.Close()
		return nil, nil, errors.New("plugin file changed while opening")
	}
	return file, actual, nil
}

func readPluginFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, info, err := openPluginFile(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() > limit {
		return nil, errors.New("plugin file exceeds size limit")
	}
	return readLimited(file, limit)
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("plugin data exceeds size limit")
	}
	return data, nil
}

// pluginLogField flattens one plugin-supplied log value to a short single line.
func pluginLogField(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxPluginLogField {
		cut := maxPluginLogField
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// pluginFileRevision hashes a regular notebook file without size limits so
// attachments can be verified before they are trashed.
func pluginFileRevision(root *os.Root, name string) (string, error) {
	file, _, err := openPluginFile(root, name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (rt *Runtime) notebookNotePath(requestedRoot, rel string) (string, string, error) {
	return rt.notebookFilePath(requestedRoot, rel, true)
}

// notebookFilePath validates a visible file inside a registered notebook;
// markdownOnly restricts it to supported note extensions.
func (rt *Runtime) notebookFilePath(requestedRoot, rel string, markdownOnly bool) (string, string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", "", errors.New("note path is empty")
	}
	rootAbs, err := rt.resolveNotebookRoot(requestedRoot)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(cleaned) || strings.Contains(cleaned, ":") || (markdownOnly && !files.IsMarkdown(cleaned)) {
		return "", "", fmt.Errorf("path %q escapes notebook root", rel)
	}
	for part := range strings.SplitSeq(cleaned, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return "", "", errors.New("plugin notes must be visible Markdown files")
		}
	}
	return rootAbs, cleaned, nil
}
