package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

var systemPermissions = map[Permission][4]string{
	PermSystemNotify:   {"系统通知", "System Notifications", "发送操作系统原生系统通知。", "Send native operating system notifications."},
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
	ErrSystemQueued      = errors.New("system notification queued")
	secretKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	extensionPattern     = regexp.MustCompile(`^(\*|\.?[A-Za-z0-9][A-Za-z0-9._+-]{0,31})$`)
)

type SystemHost interface {
	Capabilities() map[string]bool
	Notify(ctx context.Context, pluginID, title, body, tone, root, relPath, eventKey string, allowed func() bool) error
	OpenURL(rawURL string, allowed func() bool) error
	Reveal(path string, allowed func() bool) error
	Pick(ctx context.Context, save, directory, multiple bool, title, defaultPath string, extensions []string, allowed func() bool) ([]string, error)
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
	onlineKnown  bool
	online       bool
}

// systemNoticeBurst caps native notifications per plugin inside
// systemNoticeWindow. With the 2-minute dedup window this bounds live keys to
// 60, so the 64-entry dedup map never evicts a key that is still in force.
const (
	systemNoticeBurst  = 5
	systemNoticeWindow = 10 * time.Second
)

type systemNotice struct {
	at   time.Time
	keys map[[32]byte]time.Time
	sent []time.Time
	done chan struct{}
}

// allowSend reports whether another delivery fits the per-plugin rate window
// and records the attempt when it does.
func (notice *systemNotice) allowSend(now time.Time) bool {
	kept := notice.sent[:0]
	for _, at := range notice.sent {
		if now.Sub(at) < systemNoticeWindow {
			kept = append(kept, at)
		}
	}
	notice.sent = kept
	if len(notice.sent) >= systemNoticeBurst {
		return false
	}
	notice.sent = append(notice.sent, now)
	return true
}

type systemBadge struct {
	runtime *Runtime
	count   int
}

type systemEventQueue struct {
	pending map[string]map[string]any
	ordered []map[string]any
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
	DedupKey     string   `json:"dedup_key"`
	Tone         string   `json:"tone"`
	NotebookRoot string   `json:"notebook_root"`
	RelPath      string   `json:"rel_path"`
	TimeoutMS    int      `json:"timeout_ms"`
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
	if method == "host.store.changelog" {
		res, rerr := s.dispatchStoreChangelogMethod(rt, params)
		return res, rerr, true
	}
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
		paths, pickErr := host.Pick(action.ctx, save, p.Directory, p.Multiple, systemText(p.Title, 80), p.DefaultPath, p.Extensions, allowed)
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
	if len(p.DedupKey) > 128 {
		return nil, rpcErr(-32602, "notification dedup key must be at most 128 bytes")
	}
	root, rel := "", ""
	if p.RelPath != "" || p.NotebookRoot != "" {
		if !rt.manifest.HasPermission(PermNotesRead) {
			return nil, missingPerm(PermNotesRead)
		}
		var err error
		root, err = rt.resolveNotebookRoot(p.NotebookRoot)
		if err != nil {
			return nil, rpcErr(-32602, "notebook is no longer registered")
		}
		if p.RelPath != "" {
			if _, err := SystemNotificationNotePath(root, p.RelPath, []string{root}); err != nil {
				return nil, rpcErr(-32602, err.Error())
			}
			rel = p.RelPath
		}
	}
	id := rt.manifest.ID
	if p.TimeoutMS < 0 || p.TimeoutMS > 15000 {
		return nil, rpcErr(-32602, "notification timeout must be between 0 and 15000 milliseconds")
	}
	timeout := 15 * time.Second
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	go func() {
		select {
		case <-rt.exited:
			cancel()
		case <-ctx.Done():
		}
	}()
	suppressed, delivered, err := s.sendSystemNotice(ctx, id, title, body, normalizeTone(p.Tone), root, rel, p.DedupKey, allowed)
	if errors.Is(err, ErrSystemQueued) {
		return map[string]any{"ok": false, "suppressed": false, "queued": true, "delivered": false}, nil
	}
	if err != nil {
		return nil, systemRPCError(err)
	}
	return map[string]any{"ok": true, "suppressed": suppressed, "delivered": delivered}, nil
}

func (s *Supervisor) sendSystemNotice(ctx context.Context, id, title, body, tone, root, rel, dedupKey string, allowed func() bool) (suppressed, delivered bool, err error) {
	key := sha256.Sum256([]byte(strings.Join([]string{title, body, root, rel, dedupKey}, "\x00")))
	for {
		if ctx.Err() != nil || !allowed() {
			return false, false, ErrSystemCanceled
		}
		s.system.mu.Lock()
		if s.system.notices == nil {
			s.system.notices = make(map[string]systemNotice)
		}
		now := time.Now()
		for pluginID, entry := range s.system.notices {
			if entry.done == nil && now.Sub(entry.at) >= 2*time.Minute {
				delete(s.system.notices, pluginID)
			}
		}
		prev := s.system.notices[id]
		if prev.done != nil {
			s.system.mu.Unlock()
			select {
			case <-prev.done:
				continue
			case <-ctx.Done():
				return false, false, ErrSystemCanceled
			}
		}
		alreadyDelivered := now.Sub(prev.keys[key]) < 2*time.Minute
		if alreadyDelivered {
			s.system.mu.Unlock()
			return true, alreadyDelivered, nil
		}
		if !prev.allowSend(now) {
			s.system.notices[id] = prev
			s.system.mu.Unlock()
			return false, false, ErrSystemBusy
		}
		done := make(chan struct{})
		prev.done = done
		s.system.notices[id] = prev
		s.system.mu.Unlock()
		err := s.cfg.System.Notify(ctx, id, title, body, tone, root, rel, fmt.Sprintf("%x", key), allowed)
		s.system.mu.Lock()
		prev = s.system.notices[id]
		prev.done = nil
		if err == nil {
			prev.markDelivered(key)
		}
		s.system.notices[id] = prev
		close(done)
		s.system.mu.Unlock()
		return false, err == nil, err
	}
}

func (s *Supervisor) MarkSystemNoticeDelivered(pluginID, eventKey string) {
	var key [32]byte
	if len(eventKey) != hex.EncodedLen(len(key)) {
		return
	}
	if _, err := hex.Decode(key[:], []byte(eventKey)); err != nil {
		return
	}
	s.system.mu.Lock()
	defer s.system.mu.Unlock()
	if s.system.notices == nil {
		s.system.notices = make(map[string]systemNotice)
	}
	notice := s.system.notices[pluginID]
	notice.markDelivered(key)
	s.system.notices[pluginID] = notice
}

func (notice *systemNotice) markDelivered(key [32]byte) {
	now := time.Now()
	if notice.keys == nil {
		notice.keys = make(map[[32]byte]time.Time)
	}
	var oldest [32]byte
	var oldestTime time.Time
	for event, at := range notice.keys {
		if now.Sub(at) >= 2*time.Minute {
			delete(notice.keys, event)
		} else if oldestTime.IsZero() || at.Before(oldestTime) {
			oldest, oldestTime = event, at
		}
	}
	if len(notice.keys) >= 64 {
		delete(notice.keys, oldest)
	}
	notice.at = now
	notice.keys[key] = now
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

func (s *Supervisor) hasActiveSystemBadge(id string) bool {
	if s == nil {
		return false
	}
	s.system.badgeMu.Lock()
	defer s.system.badgeMu.Unlock()
	entry, ok := s.system.badges[id]
	return ok && entry.count > 0
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
	if name == "network.changed" || name == "system.wake" {
		s.system.mu.Lock()
		s.system.onlineKnown = true
		s.system.online = online
		s.system.mu.Unlock()
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
	s.dispatchScheduledJobsForTrigger(name, online)
}

func (s *Supervisor) EmitNotebookRenamedEvent(id, oldPath, newPath string) {
	if s == nil || id == "" || oldPath == "" || newPath == "" {
		return
	}
	for _, installed := range s.Installed() {
		manifest := installed.Manifest
		if !installed.Enabled || manifest.IsDeclarativeOnly() || !manifest.HasPermission(PermEventsSystem) {
			continue
		}
		if !manifest.HasPermission(PermNotesRead) && !manifest.HasPermission(PermNotesWrite) && !manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		s.queueOrderedSystemEvent(manifest.ID, map[string]any{
			"event":     "notebook.renamed",
			"id":        id,
			"old_path":  oldPath,
			"new_path":  newPath,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func (s *Supervisor) EmitNotebookRemovedEvent(id, path string) {
	if s == nil || id == "" || path == "" {
		return
	}
	for _, installed := range s.Installed() {
		manifest := installed.Manifest
		if !installed.Enabled || manifest.IsDeclarativeOnly() || !manifest.HasPermission(PermEventsSystem) {
			continue
		}
		if !manifest.HasPermission(PermNotesRead) && !manifest.HasPermission(PermNotesWrite) && !manifest.HasPermission(PermEventNoteSaved) {
			continue
		}
		s.queueOrderedSystemEvent(manifest.ID, map[string]any{
			"event":     "notebook.removed",
			"id":        id,
			"path":      path,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func (s *Supervisor) queueSystemEvent(pluginID, name string, params map[string]any) {
	s.system.mu.Lock()
	if s.system.events == nil {
		s.system.events = make(map[string]*systemEventQueue)
	}
	if queue := s.system.events[pluginID]; queue != nil {
		if queue.pending == nil {
			queue.pending = make(map[string]map[string]any)
		}
		queue.pending[name] = params
		s.system.mu.Unlock()
		return
	}
	queue := &systemEventQueue{pending: map[string]map[string]any{name: params}}
	s.system.events[pluginID] = queue
	s.system.mu.Unlock()
	go s.dispatchSystemEvents(pluginID, queue)
}

func (s *Supervisor) queueOrderedSystemEvent(pluginID string, params map[string]any) {
	s.system.mu.Lock()
	if s.system.events == nil {
		s.system.events = make(map[string]*systemEventQueue)
	}
	if queue := s.system.events[pluginID]; queue != nil {
		queue.ordered = append(queue.ordered, params)
		s.system.mu.Unlock()
		return
	}
	queue := &systemEventQueue{ordered: []map[string]any{params}}
	s.system.events[pluginID] = queue
	s.system.mu.Unlock()
	go s.dispatchSystemEvents(pluginID, queue)
}

func (s *Supervisor) dispatchSystemEvents(pluginID string, queue *systemEventQueue) {
	for {
		unlock := s.lockPluginRPC(pluginID)
		s.system.mu.Lock()
		if len(queue.pending) == 0 && len(queue.ordered) == 0 {
			delete(s.system.events, pluginID)
			s.system.mu.Unlock()
			unlock()
			return
		}
		var params map[string]any
		for _, name := range []string{"system.wake", "network.changed", "app.focus"} {
			if params = queue.pending[name]; params != nil {
				delete(queue.pending, name)
				break
			}
		}
		if params == nil && len(queue.ordered) > 0 {
			params = queue.ordered[0]
			queue.ordered = queue.ordered[1:]
		}
		if params == nil {
			keys := make([]string, 0, len(queue.pending))
			for k := range queue.pending {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if len(keys) > 0 {
				params = queue.pending[keys[0]]
				delete(queue.pending, keys[0])
			}
		}
		if params == nil {
			delete(s.system.events, pluginID)
			s.system.mu.Unlock()
			unlock()
			return
		}
		s.system.mu.Unlock()
		s.deliverSystemEvent(pluginID, params)
		unlock()
	}
}

func hasNotebookSystemEventPermission(m Manifest) bool {
	return m.HasPermission(PermNotesRead) || m.HasPermission(PermNotesWrite) || m.HasPermission(PermEventNoteSaved)
}

func (s *Supervisor) deliverSystemEvent(pluginID string, params map[string]any) {
	root := s.NotebookRoot()
	eventName, _ := params["event"].(string)
	needsNotePerm := eventName == "notebook.renamed" || eventName == "notebook.removed"
	installed, ok := s.GetInstalled(pluginID)
	if !ok || !installed.Enabled || !installed.Manifest.HasPermission(PermEventsSystem) {
		return
	}
	if needsNotePerm && !hasNotebookSystemEventPermission(installed.Manifest) {
		return
	}
	startCtx, finishStart := context.WithTimeout(context.Background(), 15*time.Second)
	rt, err := s.ensureRuntime(startCtx, pluginID, root)
	finishStart()
	if err != nil {
		return
	}
	if !rt.manifest.HasPermission(PermEventsSystem) {
		return
	}
	if needsNotePerm && !hasNotebookSystemEventPermission(rt.manifest) {
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
	if rt.manifest.HasPermission(PermNetwork) || rt.manifest.HasPermission(PermSystemFS) || rt.manifest.HasPermission(PermSystemExec) {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := rt.call(ctx, "plugin.event", params, &res); err != nil {
		return
	}
	if res.Outcome != "skipped" && res.Outcome != "" {
		s.mu.Lock()
		if !s.isRuntimeActiveLocked(pluginID, rt) {
			s.mu.Unlock()
			return
		}
		if len(res.SyncedNotebooks) > 0 {
			s.clearNotebookSaveErrorsLocked(pluginID, res.SyncedNotebooks)
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
