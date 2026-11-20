package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"block/internal/files"
)

const (
	PermSystemNotify   Permission = "system:notify"
	PermSystemSecrets  Permission = "system:secrets"
	PermSystemOpen     Permission = "system:open"
	PermSystemDialogs  Permission = "system:dialogs"
	PermClipboardRead  Permission = "system:clipboard_read"
	PermClipboardWrite Permission = "system:clipboard_write"
	PermSystemTrash    Permission = "system:trash"
	PermSystemBadge    Permission = "system:badge"
	PermEventsSystem   Permission = "events:system"
)

const blockSyncCredentialEvent = "plugin.credentials_migrate"

var systemPermissions = map[Permission][4]string{
	PermSystemNotify:   {"系统通知", "System Notifications", "在 Block 窗口外发送系统通知。", "Send system notifications outside the Block window."},
	PermSystemSecrets:  {"系统凭据", "System Credentials", "在系统凭据库中读写该插件专属的凭据。", "Read and write this plugin's own credentials in the system credential store."},
	PermSystemOpen:     {"打开系统链接", "Open Links and Reveal Files", "在用户操作时打开网页或在文件管理器中显示笔记本文件。", "Open a web page or reveal a notebook file in the file manager during a user action."},
	PermSystemDialogs:  {"系统文件选择框", "System File Pickers", "在用户操作时弹出文件、文件夹或保存位置选择框。", "Show a native file, folder, or save-location picker during a user action."},
	PermClipboardRead:  {"读取剪贴板", "Read Clipboard", "在用户操作且 Block 有焦点时读取剪贴板文本。", "Read clipboard text during a user action while Block has focus."},
	PermClipboardWrite: {"写入剪贴板", "Write Clipboard", "在用户操作且 Block 有焦点时写入剪贴板文本。", "Write clipboard text during a user action while Block has focus."},
	PermSystemTrash:    {"移到回收站", "Move to Trash", "在用户确认后将笔记本内的普通文件移到系统回收站。", "Move a regular notebook file to the system trash after the user confirms."},
	PermSystemBadge:    {"应用图标角标", "App Icon Badge", "为 Block 应用图标显示数字角标。", "Show a numeric badge on the Block app icon."},
	PermEventsSystem:   {"系统事件", "System Events", "在系统唤醒、网络状态变化或 Block 获得焦点时收到事件。", "Receive events when the system wakes, network status changes, or Block gains focus."},
}

var systemMethods = map[string]Permission{
	"host.system.notify":      PermSystemNotify,
	"host.system.open_url":    PermSystemOpen,
	"host.system.reveal":      PermSystemOpen,
	"host.system.open_dialog": PermSystemDialogs,
	"host.system.save_dialog": PermSystemDialogs,
	"host.system.trash":       PermSystemTrash,
	"host.system.badge":       PermSystemBadge,
	"host.clipboard.read":     PermClipboardRead,
	"host.clipboard.write":    PermClipboardWrite,
	"host.secrets.get":        PermSystemSecrets,
	"host.secrets.set":        PermSystemSecrets,
	"host.secrets.delete":     PermSystemSecrets,
}

func init() {
	for permission := range systemPermissions {
		validPermissions[permission] = true
	}
}

var (
	ErrSystemUnavailable = errors.New("system feature unavailable")
	ErrSystemBusy        = errors.New("system feature busy")
	ErrSystemCanceled    = errors.New("system operation canceled")
	secretKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	extensionPattern     = regexp.MustCompile(`^(\*|\.?[A-Za-z0-9][A-Za-z0-9._+-]{0,31})$`)
)

type SystemHost interface {
	Capabilities() map[string]bool
	Notify(ctx context.Context, pluginID, title, body, tone, root, relPath string, allowed func() bool) error
	OpenURL(rawURL string, allowed func() bool) error
	Reveal(path string, allowed func() bool) error
	Pick(save, directory, multiple bool, title, defaultPath string, extensions []string, allowed func() bool) ([]string, error)
	Clipboard(read bool, text string, allowed func() bool) (string, error)
	Trash(path string, allowed, validPath func() bool) error
	Badge(count int) error
	Secret(method, account, value string) (string, bool, error)
}

type systemState struct {
	mu           sync.Mutex
	badgeMu      sync.Mutex
	notices      map[string]systemNotice
	badges       map[string]systemBadge
	events       map[string]*systemEventQueue
	activeEvents map[string]*Runtime
	syncErrors   map[string]string
}

type systemNotice struct {
	at    time.Time
	key   [32]byte
	title string
	busy  bool
}

type systemBadge struct {
	runtime *Runtime
	count   int
}

type systemEventQueue struct {
	pending map[string]map[string]any
}

type systemActionKey struct{}

type systemAction struct {
	ctx          context.Context
	rpcID        string
	notebookRoot string
	epoch        uint64
}

type systemParams struct {
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Tone         string   `json:"tone"`
	NotebookRoot string   `json:"notebook_root"`
	RelPath      string   `json:"rel_path"`
	URL          string   `json:"url"`
	DefaultPath  string   `json:"default_path"`
	Extensions   []string `json:"extensions"`
	Directory    bool     `json:"directory"`
	Multiple     bool     `json:"multiple"`
	Text         *string  `json:"text"`
	Count        *int     `json:"count"`
	Key          string   `json:"key"`
	Value        *string  `json:"value"`
}

func (s *Supervisor) dispatchExtensionHostMethod(rt *Runtime, method string, params json.RawMessage, action *systemAction) (any, *rpcError, bool) {
	if method != "host.system.capabilities" && systemMethods[method] == "" {
		return s.dispatchContribHostMethod(rt, method, params)
	}
	res, rerr := s.dispatchSystemMethod(rt, method, params, action)
	return res, rerr, true
}

func (s *Supervisor) dispatchSystemMethod(rt *Runtime, method string, params json.RawMessage, action *systemAction) (any, *rpcError) {
	permission := systemMethods[method]
	if permission != "" && !rt.manifest.HasPermission(permission) {
		return nil, missingPerm(permission)
	}
	interactive := permission == PermSystemOpen || permission == PermSystemDialogs || permission == PermSystemTrash ||
		permission == PermClipboardRead || permission == PermClipboardWrite
	allowed := func() bool {
		if interactive && (action == nil || action.ctx.Err() != nil || rt.systemAction.Load() != action ||
			action.epoch != s.notebookEpoch.Load() || action.notebookRoot != s.NotebookRoot()) {
			return false
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.isRuntimeActiveLocked(rt.manifest.ID, rt) && (!interactive || s.activeActionRT[rt.manifest.ID] == rt)
	}
	if !allowed() {
		return nil, rpcErr(-32007, "system call requires an active runtime and, for interactive methods, a user action")
	}
	if len(params) > 512*1024 {
		return nil, rpcErr(-32602, "system params too large")
	}
	var p systemParams
	if len(params) > 0 && json.Unmarshal(params, &p) != nil {
		return nil, rpcErr(-32602, "invalid system params")
	}
	host := s.cfg.System
	if host == nil {
		return nil, systemRPCError(ErrSystemUnavailable)
	}
	if method == "host.system.capabilities" {
		caps := host.Capabilities()
		if rt.cfg.credentialScope == "" && caps != nil {
			caps["secrets"] = false
		}
		return caps, nil
	}
	result := map[string]any{"ok": true}
	var err error
	switch method {
	case "host.system.notify":
		return s.systemNotify(rt, p, allowed)
	case "host.system.open_url":
		parsed, parseErr := url.Parse(p.URL)
		if parseErr != nil || len(p.URL) > 4096 || parsed.Hostname() == "" || parsed.User != nil ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") || strings.ContainsAny(p.URL, "\\\r\n\t\x00") {
			return nil, rpcErr(-32602, "URL must be an HTTP or HTTPS link without credentials")
		}
		err = host.OpenURL(parsed.String(), allowed)
	case "host.system.reveal", "host.system.trash":
		required := PermNotesRead
		trash := method == "host.system.trash"
		if trash {
			required = PermNotesWrite
		}
		if !rt.manifest.HasPermission(required) {
			return nil, missingPerm(required)
		}
		path, pathErr := systemNotebookPath(rt, p.NotebookRoot, p.RelPath, trash)
		if pathErr != nil {
			return nil, rpcErr(-32602, pathErr.Error())
		}
		if trash {
			err = host.Trash(path, allowed, func() bool {
				current, err := systemNotebookPath(rt, p.NotebookRoot, p.RelPath, true)
				return err == nil && current == path
			})
		} else {
			err = host.Reveal(path, allowed)
		}
	case "host.system.open_dialog", "host.system.save_dialog":
		save := method == "host.system.save_dialog"
		if len(p.DefaultPath) > 4096 || strings.ContainsRune(p.DefaultPath, 0) || len(p.Extensions) > 32 ||
			(save && (p.Directory || p.Multiple)) || (p.Directory && len(p.Extensions) != 0) {
			return nil, rpcErr(-32602, "invalid picker options")
		}
		for _, ext := range p.Extensions {
			if !extensionPattern.MatchString(ext) {
				return nil, rpcErr(-32602, "invalid file extension")
			}
		}
		paths, pickErr := host.Pick(save, p.Directory, p.Multiple, systemText(p.Title, 80), p.DefaultPath, p.Extensions, allowed)
		if pickErr == nil {
			if paths == nil {
				paths = []string{}
			}
			result["paths"] = paths
			result["canceled"] = len(paths) == 0
		}
		err = pickErr
	case "host.clipboard.read", "host.clipboard.write":
		read := method == "host.clipboard.read"
		var text string
		if !read {
			if p.Text == nil || len(*p.Text) > 64*1024 || strings.ContainsRune(*p.Text, 0) {
				return nil, rpcErr(-32602, "text must contain at most 64 KiB and no NUL")
			}
			text = *p.Text
		}
		text, err = host.Clipboard(read, text, allowed)
		if read && err == nil {
			if len(text) > 64*1024 || !utf8.ValidString(text) {
				return nil, rpcErr(-32002, "clipboard text is invalid or too large")
			}
			result["text"] = text
		}
	case "host.secrets.get", "host.secrets.set", "host.secrets.delete":
		if rt.cfg.credentialScope == "" {
			return nil, systemRPCError(ErrSystemUnavailable)
		}
		if !secretKeyPattern.MatchString(p.Key) {
			return nil, rpcErr(-32602, "invalid credential key")
		}
		var value string
		if method == "host.secrets.set" {
			if p.Value == nil || len(*p.Value) == 0 || len(*p.Value) > 1536 || strings.ContainsRune(*p.Value, 0) {
				return nil, rpcErr(-32602, "credential must contain 1–1536 bytes and no NUL")
			}
			value = *p.Value
		}
		account := fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(s.cfg.AppDataDir)+"\x00"+rt.manifest.ID+"\x00"+rt.cfg.credentialScope+"\x00"+p.Key)))
		var found bool
		value, found, err = host.Secret(method, account, value)
		if method == "host.secrets.get" && err == nil {
			result["found"] = found
			if found {
				result["value"] = value
			}
		}
	case "host.system.badge":
		if p.Count == nil || *p.Count < 0 || *p.Count > 999 {
			return nil, rpcErr(-32602, "count must be an integer from 0 to 999")
		}
		err = s.setSystemBadge(rt, *p.Count, allowed)
	}
	if err != nil {
		return nil, systemRPCError(err)
	}
	return result, nil
}

func systemRPCError(err error) *rpcError {
	switch {
	case errors.Is(err, ErrSystemUnavailable), errors.Is(err, errors.ErrUnsupported):
		return rpcErr(-32005, "system feature unavailable or blocked by system settings")
	case errors.Is(err, ErrSystemBusy):
		return rpcErr(-32006, "system feature busy")
	case errors.Is(err, ErrSystemCanceled), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return rpcErr(-32007, "system operation canceled or expired")
	default:
		return rpcErr(-32002, "system operation failed")
	}
}

func systemText(raw string, limit int) string {
	return truncateRunes(strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, raw)), limit)
}

func systemNotebookPath(rt *Runtime, requestedRoot, rel string, trash bool) (string, error) {
	if len(requestedRoot) > 4096 {
		return "", errors.New("invalid notebook root")
	}
	root, err := rt.resolveNotebookRoot(requestedRoot)
	if err != nil {
		return "", errors.New("notebook is no longer registered")
	}
	return validatedSystemPath(root, rel, trash)
}

func SystemNotificationNotePath(root, rel string, notebooks []string) (string, error) {
	if !files.IsMarkdown(rel) {
		return "", errors.New("notification target must be a Markdown note")
	}
	for _, notebook := range notebooks {
		if candidate, err := filepath.Abs(notebook); err == nil && candidate == root {
			return validatedSystemPath(root, rel, true)
		}
	}
	return "", errors.New("notebook is no longer registered")
}

func validatedSystemPath(root, rel string, regular bool) (string, error) {
	if len(rel) > 4096 || strings.ContainsAny(rel, ":\x00\\") || rel == "." || rel == "" {
		return "", errors.New("invalid notebook-relative path")
	}
	for part := range strings.SplitSeq(rel, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", errors.New("path must identify a visible item inside a notebook")
		}
	}
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return "", errors.New("path must be notebook-relative")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("notebook is unavailable")
	}
	path := filepath.Join(root, local)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", errors.New("notebook item is unavailable or uses a symbolic link")
	}
	info, err := os.Lstat(path)
	if err != nil || (regular && !info.Mode().IsRegular()) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", errors.New("unsupported notebook item")
	}
	return path, nil
}

func (s *Supervisor) systemNotify(rt *Runtime, p systemParams, allowed func() bool) (any, *rpcError) {
	title, body := systemText(p.Title, 80), systemText(p.Body, 240)
	if title == "" || body == "" {
		return nil, rpcErr(-32602, "notification title and body are required")
	}
	root, rel := "", ""
	if p.RelPath != "" {
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, missingPerm(PermNotesRead)
		}
		if !files.IsMarkdown(p.RelPath) {
			return nil, rpcErr(-32602, "notification target must be a Markdown note")
		}
		if _, err := systemNotebookPath(rt, p.NotebookRoot, p.RelPath, true); err != nil {
			return nil, rpcErr(-32602, err.Error())
		}
		var err error
		root, err = rt.resolveNotebookRoot(p.NotebookRoot)
		if err != nil {
			return nil, rpcErr(-32602, "notebook is no longer registered")
		}
		rel = p.RelPath
	}
	id := rt.manifest.ID
	key := sha256.Sum256([]byte(strings.Join([]string{title, body, root, rel}, "\x00")))
	now := time.Now()
	s.system.mu.Lock()
	if s.system.notices == nil {
		s.system.notices = make(map[string]systemNotice)
	}
	for pluginID, entry := range s.system.notices {
		if !entry.busy && now.Sub(entry.at) >= 2*time.Minute {
			delete(s.system.notices, pluginID)
		}
	}
	prev := s.system.notices[id]
	newSyncState := id == "block-sync" && title != prev.title
	if prev.busy || (now.Sub(prev.at) < 10*time.Second && !newSyncState) || (prev.key == key && now.Sub(prev.at) < 2*time.Minute) {
		s.system.mu.Unlock()
		return map[string]any{"ok": true, "suppressed": true}, nil
	}
	s.system.notices[id] = systemNotice{at: now, key: key, title: title, busy: true}
	s.system.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		select {
		case <-rt.exited:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := s.cfg.System.Notify(ctx, id, title, body, normalizeTone(p.Tone), root, rel, allowed)
	s.system.mu.Lock()
	if err == nil {
		s.system.notices[id] = systemNotice{at: time.Now(), key: key, title: title}
	} else {
		s.system.notices[id] = prev
	}
	s.system.mu.Unlock()
	if err != nil {
		return nil, systemRPCError(err)
	}
	return map[string]any{"ok": true, "suppressed": false}, nil
}

func (s *Supervisor) setSystemBadge(rt *Runtime, count int, allowed func() bool) error {
	s.system.badgeMu.Lock()
	defer s.system.badgeMu.Unlock()
	if !allowed() {
		return ErrSystemCanceled
	}
	id := rt.manifest.ID
	total := count
	for pluginID, value := range s.system.badges {
		if pluginID != id {
			select {
			case <-value.runtime.exited:
				delete(s.system.badges, pluginID)
				continue
			default:
			}
			total = min(999, total+value.count)
		}
	}
	if err := s.cfg.System.Badge(total); err != nil {
		return err
	}
	if count == 0 {
		delete(s.system.badges, id)
	} else {
		if s.system.badges == nil {
			s.system.badges = make(map[string]systemBadge)
		}
		s.system.badges[id] = systemBadge{runtime: rt, count: count}
	}
	return nil
}

func (s *Supervisor) clearInactiveSystemBadge(id string) {
	if s.cfg.System == nil {
		return
	}
	s.system.badgeMu.Lock()
	defer s.system.badgeMu.Unlock()
	entry, ok := s.system.badges[id]
	if !ok {
		return
	}
	s.mu.RLock()
	active := s.isRuntimeActiveLocked(id, entry.runtime)
	s.mu.RUnlock()
	select {
	case <-entry.runtime.exited:
		active = false
	default:
	}
	if active {
		return
	}
	delete(s.system.badges, id)
	total := 0
	for _, value := range s.system.badges {
		total = min(999, total+value.count)
	}
	_ = s.cfg.System.Badge(total)
}

func (s *Supervisor) EmitSystemEvent(name string, online bool) {
	if name != "system.wake" && name != "network.changed" && name != "app.focus" {
		return
	}
	for _, installed := range s.Installed() {
		manifest := installed.Manifest
		if !installed.Enabled || manifest.IsDeclarativeOnly() || !manifest.HasPermission(PermEventsSystem) {
			continue
		}
		params := map[string]any{"event": name, "timestamp": time.Now().UTC().Format(time.RFC3339)}
		if name == "network.changed" || name == "system.wake" {
			params["online"] = online
		}
		s.queueSystemEvent(manifest.ID, name, params)
	}
}

func (s *Supervisor) queueSystemEvent(pluginID, name string, params map[string]any) {
	s.system.mu.Lock()
	if s.system.events == nil {
		s.system.events = make(map[string]*systemEventQueue)
	}
	if queue := s.system.events[pluginID]; queue != nil {
		queue.pending[name] = params
		s.system.mu.Unlock()
		return
	}
	queue := &systemEventQueue{pending: map[string]map[string]any{name: params}}
	s.system.events[pluginID] = queue
	s.system.mu.Unlock()
	go s.dispatchSystemEvents(pluginID, queue)
}

func (s *Supervisor) dispatchSystemEvents(pluginID string, queue *systemEventQueue) {
	for {
		unlock := s.lockPluginRPC(pluginID)
		s.system.mu.Lock()
		if len(queue.pending) == 0 {
			delete(s.system.events, pluginID)
			s.system.mu.Unlock()
			unlock()
			return
		}
		var params map[string]any
		for _, name := range []string{"system.wake", "network.changed", "app.focus", blockSyncCredentialEvent} {
			if params = queue.pending[name]; params != nil {
				delete(queue.pending, name)
				break
			}
		}
		s.system.mu.Unlock()
		s.deliverSystemEvent(pluginID, params)
		unlock()
	}
}

func (s *Supervisor) deliverSystemEvent(pluginID string, params map[string]any) {
	root := s.NotebookRoot()
	installed, ok := s.GetInstalled(pluginID)
	if !ok || !installed.Enabled || !installed.Manifest.HasPermission(PermEventsSystem) {
		return
	}
	syncEvent := pluginID == "block-sync" && (params["event"] == "system.wake" || params["event"] == "network.changed")
	if syncEvent && params["event"] == "network.changed" && params["online"] != true {
		return
	}
	startCtx, finishStart := context.WithTimeout(context.Background(), 15*time.Second)
	rt, err := s.ensureRuntime(startCtx, pluginID, root)
	finishStart()
	if err != nil {
		if syncEvent {
			s.reportSystemSyncFailure(installed, nil, root, false, false)
		}
		return
	}
	if !rt.manifest.HasPermission(PermEventsSystem) {
		return
	}
	s.mu.Lock()
	if !s.isRuntimeActiveLocked(pluginID, rt) {
		s.mu.Unlock()
		return
	}
	if s.system.activeEvents == nil {
		s.system.activeEvents = make(map[string]*Runtime)
	}
	s.system.activeEvents[pluginID] = rt
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.system.activeEvents, pluginID)
		s.mu.Unlock()
	}()
	if rt.manifest.HasPermission(PermNotesRead) || rt.manifest.HasPermission(PermNotesWrite) || rt.manifest.HasPermission(PermEventNoteSaved) {
		params["notebook_root"] = root
	}
	var res struct {
		Outcome         string   `json:"outcome"`
		SyncedNotebooks []string `json:"synced_notebooks"`
	}
	timeout := 45 * time.Second
	if syncEvent {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := rt.call(ctx, "plugin.event", params, &res); err != nil {
		if syncEvent {
			s.reportSystemSyncFailure(installed, rt, root, errors.Is(ctx.Err(), context.DeadlineExceeded), false)
		}
		return
	}
	if params["event"] == blockSyncCredentialEvent {
		switch res.Outcome {
		case "migration_pending":
			s.queueSystemEvent(pluginID, blockSyncCredentialEvent, map[string]any{"event": blockSyncCredentialEvent})
		case "migration_retry":
			attempt, _ := params["retry_attempt"].(int)
			next := min(attempt+1, 5)
			time.AfterFunc(time.Second<<next, func() {
				s.mu.RLock()
				active := s.isRuntimeActiveLocked(pluginID, rt)
				s.mu.RUnlock()
				if active {
					s.queueSystemEvent(pluginID, blockSyncCredentialEvent, map[string]any{"event": blockSyncCredentialEvent, "retry_attempt": next})
				}
			})
		}
		return
	}
	if syncEvent && res.Outcome == "state_error" {
		s.reportSystemSyncFailure(installed, rt, root, false, true)
		return
	}
	if res.Outcome != "skipped" && res.Outcome != "" {
		s.mu.Lock()
		if !s.isRuntimeActiveLocked(pluginID, rt) {
			s.mu.Unlock()
			return
		}
		if pluginID == "block-sync" && res.Outcome == "synced" {
			s.clearSyncedNotebookErrorsLocked(pluginID, res.SyncedNotebooks)
		}
		if pluginID == "block-sync" {
			delete(s.system.syncErrors, root)
		}
		view := s.views[viewKey{pluginID, root, SurfaceSettingsPage}]
		refresh := view != nil && view.Page != nil && view.BusyAction == ""
		s.mu.Unlock()
		if refresh {
			s.reloadSurfaceViewForNotebookAsync(pluginID, root, SurfaceSettingsPage)
		}
		s.notifyChanged()
	}
}

func (s *Supervisor) reportSystemSyncFailure(inst InstalledApp, rt *Runtime, root string, timedOut, storageFailed bool) {
	const pluginID = "block-sync"
	config := systemSyncConfig(inst.DataDir, root)
	if rt == nil {
		if enabled, ok := config["auto_sync"].(bool); config == nil || (ok && !enabled) {
			return
		}
	}
	title, body := "自动同步未完成", "打开同步设置查看状态或重试。"
	if timedOut {
		title = "自动同步超时"
	}
	locale := normalizeLocale(s.Locale())
	if rt != nil {
		locale = rt.locale()
	}
	if locale == "en" {
		title, body = "Automatic sync did not finish", "Open Sync settings to check the status or retry."
		if timedOut {
			title = "Automatic sync timed out"
		}
	}
	if rt == nil {
		title = "自动同步未能启动"
		if locale == "en" {
			title = "Automatic sync could not start"
		}
	}
	if storageFailed {
		title = "同步状态未保存"
		if locale == "en" {
			title = "Sync state could not be saved"
		}
	}
	currentLocked := func() bool {
		if s.stopping || (rt != nil && s.runtimes[pluginID] != rt) {
			return false
		}
		for _, installed := range s.installed {
			if installed.Manifest.ID == pluginID {
				return installed.Enabled && s.replacingID != pluginID && installed.InstallDir == inst.InstallDir && installed.InstalledAt.Equal(inst.InstalledAt)
			}
		}
		return false
	}
	s.mu.Lock()
	if !currentLocked() {
		s.mu.Unlock()
		return
	}
	if s.system.syncErrors == nil {
		s.system.syncErrors = make(map[string]string)
	}
	s.system.syncErrors[root] = title + " · " + body
	s.mu.Unlock()
	s.notifyChanged()
	files.LogWarnf("plugin %s background system sync failed (timed out: %t)", pluginID, timedOut)
	if storageFailed || s.cfg.System == nil || !inst.Manifest.HasPermission(PermSystemNotify) || config == nil {
		return
	}
	if enabled, ok := config["system_notifications"].(bool); ok && !enabled {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.cfg.System.Notify(ctx, pluginID, title, body, "error", "", "", func() bool {
			s.mu.RLock()
			defer s.mu.RUnlock()
			return currentLocked()
		})
	}()
}

func systemSyncConfig(dataDir, root string) map[string]any {
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil
	}
	defer data.Close()
	state, err := readPluginStorageMap(data)
	if err != nil {
		return nil
	}
	config, _ := state["config"].(map[string]any)
	if _, global := config["selected_notebooks"]; !global {
		notebooks, _ := state["notebook_configs"].(map[string]any)
		key := "default"
		if root != "" {
			key = syncNotebookKey(root)
		}
		if local, ok := notebooks[key].(map[string]any); ok {
			config = local
		}
	}
	return config
}
