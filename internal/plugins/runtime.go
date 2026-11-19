package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
}

type ActionResult struct {
	Values     map[string]any `json:"values,omitempty"`
	Page       *UIPage        `json:"page,omitempty"`
	Status     string         `json:"status,omitempty"`
	OpenPath   string         `json:"open_path,omitempty"`
	RevealPath string         `json:"reveal_path,omitempty"`
}

const maxStderrBytes = 64 * 1024

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

// Runtime hosts a single sandboxed out-of-process plugin speaking newline-delimited
// bidirectional JSON-RPC 2.0 over stdin/stdout.
type Runtime struct {
	exited   chan struct{}
	manifest Manifest
	cfg      HostConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *ringBuffer

	writeMu   sync.Mutex
	callMu    sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan rpcEnvelope
	nextID    atomic.Int64

	storageMu sync.Mutex
	cfgMu     sync.RWMutex
	closed    atomic.Bool
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
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin data dir: %w", err)
	}
	cmdName := m.Entry.Command[0]
	var cmdArgs []string
	if len(m.Entry.Command) > 1 {
		cmdArgs = m.Entry.Command[1:]
	}
	if strings.HasPrefix(cmdName, "./") || strings.HasPrefix(cmdName, ".\\") {
		cmdName = filepath.Join(cfg.PluginDir, cmdName)
	}
	downloadsDir := strings.TrimSpace(cfg.DownloadsDir)
	if downloadsDir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			downloadsDir = filepath.Join(home, "Downloads")
		}
	}
	cfg.DownloadsDir = downloadsDir

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	rt := &Runtime{
		exited:   make(chan struct{}),
		manifest: m,
		cfg:      cfg,
		cmd:      cmd,
		stdin:    stdin,
		stderr:   newRingBuffer(maxStderrBytes),
		pending:  make(map[string]chan rpcEnvelope),
	}
	cmd.Stderr = rt.stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin process %v: %w", m.Entry.Command, err)
	}

	go rt.readLoop(stdout)
	return rt, nil
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
	params := map[string]any{
		"plugin_id":       rt.manifest.ID,
		"data_dir":        rt.cfg.DataDir,
		"downloads_dir":   rt.cfg.DownloadsDir,
		"notebook_root":   rt.cfg.NotebookRoot,
		"notebooks":       rt.knownNotebooks(),
		"locale":          locale,
		"host_version":    ver,
		"ui_capabilities": []string{
			"kv_tooltip", "treemap", "host_perf", "checklist", "form_layout", "multi_notebook",
			"card_collapse", "metric_tone", "log_columns", "file_picker",
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
	if rt.closed.Swap(true) {
		return nil
	}
	var res map[string]any
	_ = rt.call(ctx, "plugin.shutdown", map[string]any{}, &res)
	_ = rt.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- rt.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = rt.cmd.Process.Kill()
		return nil
	}
}

func (rt *Runtime) call(ctx context.Context, method string, params any, out any) error {
	rt.callMu.Lock()
	defer rt.callMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err := rt.writeLine(req); err != nil {
		return fmt.Errorf("send %s: %w (stderr: %s)", method, err, strings.TrimSpace(rt.Stderr()))
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: plugin exited unexpectedly (stderr: %s)", method, strings.TrimSpace(rt.Stderr()))
		}
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
		return fmt.Errorf("%s timed out: %w (stderr: %s)", method, ctx.Err(), strings.TrimSpace(rt.Stderr()))
	}
}

func (rt *Runtime) writeLine(v any) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
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
			// Incoming capability request from plugin -> handle on a goroutine so readLoop never blocks.
			go rt.handleHostCall(env)
			continue
		}
		idKey := normalizeID(env.ID)
		rt.pendingMu.Lock()
		ch, ok := rt.pending[idKey]
		rt.pendingMu.Unlock()
		if ok {
			ch <- env
		}
	}
	rt.pendingMu.Lock()
	for k, ch := range rt.pending {
		close(ch)
		delete(rt.pending, k)
	}
	rt.pendingMu.Unlock()
}

func normalizeID(raw json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(raw)), "\"")
}

func (rt *Runtime) handleHostCall(req rpcEnvelope) {
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
	_ = rt.writeLine(resp)
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
		abs, cleanRel, err := rt.jailNotePathFor(p.NotebookRoot, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		return map[string]any{"path": cleanRel, "content": string(b)}, nil

	case "host.notes.write":
		if !rt.manifest.HasPermission(PermNotesWrite) {
			return nil, &rpcError{Code: -32001, Message: "missing permission: notes:write"}
		}
		var p struct {
			NotebookRoot string `json:"notebook_root"`
			Path         string `json:"path"`
			Content      string `json:"content"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		abs, _, err := rt.jailNotePathFor(p.NotebookRoot, p.Path)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
		}
		if err := os.WriteFile(abs, []byte(p.Content), 0o644); err != nil {
			return nil, &rpcError{Code: -32004, Message: err.Error()}
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
		return nil, &rpcError{Code: -32601, Message: "unknown host method: " + method}
	}
}

func (rt *Runtime) storageFilePath() string {
	return filepath.Join(rt.cfg.DataDir, "state.json")
}

func (rt *Runtime) loadStorageMap() (map[string]any, error) {
	rt.storageMu.Lock()
	defer rt.storageMu.Unlock()
	b, err := os.ReadFile(rt.storageFilePath())
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
	return out, nil
}

func (rt *Runtime) setStorageKey(key string, val any) error {
	rt.storageMu.Lock()
	defer rt.storageMu.Unlock()
	p := rt.storageFilePath()
	cur := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	cur[key] = val
	raw, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o644)
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
	var list []NoteMeta
	err := filepath.Walk(nbRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != nbRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(nbRoot, path)
		if err != nil {
			return nil
		}
		list = append(list, NoteMeta{
			Path:    filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	return list, err
}

func (rt *Runtime) jailNotePath(rel string) (string, string, error) {
	return rt.jailNotePathFor("", rel)
}

func (rt *Runtime) jailNotePathFor(requestedRoot, rel string) (string, string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", "", errors.New("note path is empty")
	}
	rootAbs, err := rt.resolveNotebookRoot(requestedRoot)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", "", fmt.Errorf("path %q escapes notebook root", rel)
	}
	targetAbs := filepath.Join(rootAbs, cleaned)
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path %q escapes notebook root", rel)
	}
	return targetAbs, filepath.ToSlash(cleaned), nil
}
