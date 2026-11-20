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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/files"
)

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
	HostVersion     string
	GetNotebooks    func() []string
	GetPerfSnapshot func(refreshDisk bool) (*PerfSnapshot, error)
	ApplyNoteWrite  func(absPath, content string, write func() (bool, error)) (bool, error)
	OnNoteWritten   func(pluginID, notebookRoot, absPath string)
	// ContribHost handles host.titlebar/status/ui/editor methods; ok=false falls through.
	ContribHost func(m Manifest, method string, params json.RawMessage) (any, *rpcError, bool)
}

var ErrNoteConflict = errors.New("note changed; read it again before writing")

type ActionResult struct {
	OK         *bool          `json:"ok,omitempty"`
	Tone       string         `json:"tone,omitempty"`
	Values     map[string]any `json:"values,omitempty"`
	Page       *UIPage        `json:"page,omitempty"`
	Status     string         `json:"status,omitempty"`
	OpenPath   string         `json:"open_path,omitempty"`
	RevealPath string         `json:"reveal_path,omitempty"`
}

const (
	maxStderrBytes    = 64 * 1024
	maxPluginStorage  = 8 * 1024 * 1024
	maxPluginNote     = 16 * 1024 * 1024
	maxPluginHostRPCs = 8
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

	writeGate chan struct{}
	callGate  chan struct{}
	hostCalls chan struct{}
	pendingMu sync.Mutex
	pending   map[string]chan rpcEnvelope
	nextID    atomic.Int64

	storageMu sync.Mutex
	noteMu    sync.Mutex
	cfgMu     sync.RWMutex
	closed    atomic.Bool
	abortOnce sync.Once
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
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("start plugin process %v: %w", m.Entry.Command, err)
	}
	_ = stdoutW.Close()
	started = true

	readDone := make(chan struct{})
	go func() {
		rt.readLoop(stdout)
		close(readDone)
	}()
	go func() {
		_ = cmd.Wait()
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
		return filepath.Abs(nbRoot)
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
	params := map[string]any{
		"plugin_id":     rt.manifest.ID,
		"data_dir":      rt.cfg.DataDir,
		"downloads_dir": rt.cfg.DownloadsDir,
		"notebook_root": notebookRoot,
		"notebooks":     notebooks,
		"locale":        locale,
		"host_version":  ver,
		"ui_capabilities": []string{
			"kv_tooltip", "treemap", "host_perf", "checklist", "form_layout", "multi_notebook",
			"card_collapse", "metric_tone", "log_columns", "file_picker", "action_feedback", "note_revision",
		},
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
	if values == nil {
		values = map[string]any{}
	}
	var rawRes struct {
		OK         *bool           `json:"ok,omitempty"`
		Tone       string          `json:"tone,omitempty"`
		Values     map[string]any  `json:"values,omitempty"`
		Page       json.RawMessage `json:"page,omitempty"`
		Status     string          `json:"status,omitempty"`
		OpenPath   string          `json:"open_path,omitempty"`
		RevealPath string          `json:"reveal_path,omitempty"`
	}
	params := map[string]any{
		"surface":   string(surface),
		"action_id": actionID,
		"values":    values,
	}
	if err := rt.call(ctx, "plugin.action", params, &rawRes); err != nil {
		return nil, err
	}
	out := &ActionResult{
		OK:         rawRes.OK,
		Tone:       strings.TrimSpace(rawRes.Tone),
		Values:     rawRes.Values,
		Status:     rawRes.Status,
		OpenPath:   strings.TrimSpace(rawRes.OpenPath),
		RevealPath: strings.TrimSpace(rawRes.RevealPath),
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
	if !rt.manifest.HasPermission(PermEventNoteSaved) {
		return nil
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
		Handled bool `json:"handled"`
	}
	return rt.call(ctx, "plugin.event", params, &res)
}

func (rt *Runtime) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
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
	if err := rt.writeLine(ctx, req); err != nil {
		rt.abort()
		return fmt.Errorf("send %s: %w (stderr: %s)", method, err, strings.TrimSpace(rt.Stderr()))
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("%s rpc error (%d): %s", method, resp.Error.Code, resp.Error.Message)
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
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
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
	res, rpcErr := rt.dispatchHostMethod(req.Method, req.Params)
	resp := rpcEnvelope{
		JSONRPC: "2.0",
		ID:      req.ID,
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

func (rt *Runtime) dispatchHostMethod(method string, params json.RawMessage) (any, *rpcError) {
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
		return map[string]any{"value": val, "found": ok}, nil

	case "host.storage.set":
		if !rt.manifest.HasPermission(PermStorage) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: storage"}
		}
		var p struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Key) == "" {
			return nil, &rpcError{Code: -32602, Message: "host.storage.set requires non-empty key"}
		}
		if err := rt.setStorageKey(p.Key, p.Value); err != nil {
			return nil, &rpcError{Code: -32002, Message: err.Error()}
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
		return map[string]any{"path": filepath.ToSlash(cleanRel), "content": string(b), "revision": noteRevision(b)}, nil

	case "host.notes.write":
		if !rt.manifest.HasPermission(PermNotesWrite) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:write"}
		}
		var p struct {
			NotebookRoot     string  `json:"notebook_root"`
			Path             string  `json:"path"`
			Content          string  `json:"content"`
			ExpectedRevision *string `json:"expected_revision"`
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
		if err := checkPluginParents(root, cleanRel); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		abs := filepath.Join(rootPath, cleanRel)
		write := func() (bool, error) {
			if rt.closed.Load() {
				return false, errors.New("plugin is stopping")
			}
			return writePluginNote(root, cleanRel, p.Content, p.ExpectedRevision)
		}
		var changed bool
		if rt.cfg.ApplyNoteWrite != nil {
			changed, err = rt.cfg.ApplyNoteWrite(abs, p.Content, write)
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
		if changed && rt.cfg.OnNoteWritten != nil {
			rt.cfg.OnNoteWritten(rt.manifest.ID, rootPath, abs)
		}
		return map[string]any{"ok": true, "changed": changed}, nil

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
			if res, rerr, ok := rt.cfg.ContribHost(rt.manifest, method, params); ok {
				return res, rerr
			}
		}
		return nil, &rpcError{Code: -32601, Message: "unknown host method: " + method}
	}
}

func (rt *Runtime) loadStorageMap() (map[string]any, error) {
	rt.storageMu.Lock()
	defer rt.storageMu.Unlock()
	return rt.readStorageMap()
}

func (rt *Runtime) readStorageMap() (map[string]any, error) {
	b, err := readPluginFile(rt.dataRoot, "state.json", maxPluginStorage)
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

func (rt *Runtime) setStorageKey(key string, val any) error {
	rt.storageMu.Lock()
	defer rt.storageMu.Unlock()
	cur, err := rt.readStorageMap()
	if err != nil {
		return err
	}
	cur[key] = val
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
	prior, err := readPluginFile(root, rel, maxPluginNote)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	exists := err == nil
	if exists && bytes.Equal(prior, []byte(content)) {
		return false, nil
	}
	if expected != nil && ((!exists && *expected != "") || (exists && *expected != noteRevision(prior))) {
		return false, ErrNoteConflict
	}
	if err := files.WriteNoteAt(root, rel, content); err != nil {
		return false, err
	}
	return true, nil
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

func (rt *Runtime) notebookNotePath(requestedRoot, rel string) (string, string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", "", errors.New("note path is empty")
	}
	rootAbs, err := rt.resolveNotebookRoot(requestedRoot)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(cleaned) || strings.Contains(cleaned, ":") || !files.IsMarkdown(cleaned) {
		return "", "", fmt.Errorf("path %q escapes notebook root", rel)
	}
	for part := range strings.SplitSeq(cleaned, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return "", "", errors.New("plugin notes must be visible Markdown files")
		}
	}
	return rootAbs, cleaned, nil
}
