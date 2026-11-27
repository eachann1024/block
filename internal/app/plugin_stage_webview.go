package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"block/internal/files"
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	pluginStageScheme           = "block-plugin"
	pluginStageEventName        = "block:stage-event"
	pluginStageWinHostSuffix    = ".block-plugin.localhost"
	maxPluginStageServeBytes    = 16 * 1024 * 1024
	pluginStagePlaceholderCardW = float32(440)
	pluginStageCSP              = "default-src 'self' 'unsafe-inline' data: blob:; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'"
)

const pluginStageOfflineRulesJSON = `[
  {"trigger":{"url-filter":"^https?://"},"action":{"type":"block"}},
  {"trigger":{"url-filter":"^http://[^/]+\\.block-plugin\\.localhost/"},"action":{"type":"ignore-previous-rules"}},
  {"trigger":{"url-filter":"^wss?://"},"action":{"type":"block"}}
]`

const pluginStagePreloadScript = `(() => {
  if (window.blockView) return;
  window.blockView = Object.freeze({
    getInitState: () => window.mygo.call("blockView.GetInitState"),
    invoke: (action, payload) => window.mygo.call("blockView.Invoke", action, payload ?? {}),
    onEvent: (fn) => window.mygo.on("block:stage-event", fn),
    close: () => window.mygo.call("blockView.Close"),
  });
})();`

var (
	pluginStageRuntimeOnce sync.Once
	pluginStageRuntimeErr  error
	pluginStageEvent       *mygo.Event[map[string]any]

	pluginStageRulesOnce sync.Once
	pluginStageRules     *mygo.ContentRules
)

type stagePageBinding struct {
	workspace *Workspace
	pluginID  string
	stageID   string
	entry     string
}

var stagePages = struct {
	mu sync.RWMutex
	m  map[*mygo.Page]stagePageBinding
}{
	m: make(map[*mygo.Page]stagePageBinding),
}

func registerStagePage(page *mygo.Page, b stagePageBinding) {
	if page == nil {
		return
	}
	stagePages.mu.Lock()
	stagePages.m[page] = b
	stagePages.mu.Unlock()
}

func unregisterStagePage(page *mygo.Page) {
	if page == nil {
		return
	}
	stagePages.mu.Lock()
	delete(stagePages.m, page)
	stagePages.mu.Unlock()
}

func lookupStagePage(page *mygo.Page) (stagePageBinding, bool) {
	if page == nil {
		return stagePageBinding{}, false
	}
	stagePages.mu.RLock()
	b, ok := stagePages.m[page]
	stagePages.mu.RUnlock()
	return b, ok
}

// RegisterPluginStageRuntime registers the read-only block-plugin:// protocol,
// window.blockView IPC bridge, and stage event channel once (lazily on first WebView stage).
func RegisterPluginStageRuntime() error {
	pluginStageRuntimeOnce.Do(func() {
		if err := mygo.Protocol.Handle(pluginStageScheme, http.HandlerFunc(servePluginStageProtocol)); err != nil {
			pluginStageRuntimeErr = err
			return
		}
		mygo.BindAs("blockView", &stageBridgeService{})
		pluginStageEvent = mygo.NewEvent[map[string]any](pluginStageEventName)
	})
	return pluginStageRuntimeErr
}

func ensurePluginStageContentRules() []*mygo.ContentRules {
	if !mygo.App.IsReady() {
		return nil
	}
	pluginStageRulesOnce.Do(func() {
		rules, err := mygo.CompileContentRules("block-plugin-offline", pluginStageOfflineRulesJSON)
		if err != nil {
			files.LogWarnf("compile plugin stage offline content rules failed: %v", err)
			return
		}
		pluginStageRules = rules
	})
	if pluginStageRules == nil {
		return nil
	}
	return []*mygo.ContentRules{pluginStageRules}
}

func parsePluginStageHost(rawHost string) (string, bool) {
	host := strings.TrimSpace(rawHost)
	if host == "" || strings.ContainsAny(host, ":/\\@\x00\r\n") {
		return "", false
	}
	lower := strings.ToLower(host)
	if strings.HasSuffix(lower, pluginStageWinHostSuffix) {
		host = host[:len(host)-len(pluginStageWinHostSuffix)]
	}
	if host == "" || host == "." || host == ".." || strings.Contains(host, "..") || strings.ContainsAny(host, "/\\:") {
		return "", false
	}
	return host, true
}

func normalizeStageWebRelPath(rawPath string) (string, error) {
	if strings.ContainsAny(rawPath, "\\\x00\r\n") {
		return "", errors.New("invalid stage asset path")
	}
	trimmed := strings.TrimPrefix(rawPath, "/")
	if trimmed == "" {
		return "index.html", nil
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return "", errors.New("stage asset path traversal is forbidden")
		}
	}
	clean := path.Clean(trimmed)
	if clean == "." || clean == "" {
		return "index.html", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", errors.New("stage asset path escapes web directory")
	}
	return clean, nil
}

func stageContentType(relPath string) string {
	switch strings.ToLower(path.Ext(relPath)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	case ".wasm":
		return "application/wasm"
	default:
		if mt := mime.TypeByExtension(path.Ext(relPath)); mt != "" {
			return mt
		}
		return "application/octet-stream"
	}
}

var errStagePathBlocked = errors.New("stage asset path blocked")

func openStageWebFile(pluginRoot, relPath string) ([]byte, error) {
	if strings.TrimSpace(pluginRoot) == "" {
		return nil, errStagePathBlocked
	}
	clean, err := normalizeStageWebRelPath(relPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errStagePathBlocked, err)
	}
	root, err := os.OpenRoot(pluginRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	webInfo, err := root.Lstat("web")
	if err != nil {
		return nil, err
	}
	if webInfo.Mode()&os.ModeSymlink != 0 || !webInfo.IsDir() {
		return nil, fmt.Errorf("%w: web must be a real directory", errStagePathBlocked)
	}

	fullRel := path.Join("web", clean)
	parts := strings.Split(fullRel, "/")
	for i := 1; i < len(parts); i++ {
		prefix := path.Join(parts[:i]...)
		info, err := root.Lstat(prefix)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("%w: parent directory must not be a symlink", errStagePathBlocked)
		}
	}

	info, err := root.Lstat(fullRel)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: target must be a regular non-symlink file", errStagePathBlocked)
	}
	if info.Size() > maxPluginStageServeBytes {
		return nil, fmt.Errorf("%w: asset exceeds size limit", errStagePathBlocked)
	}

	f, err := root.Open(fullRel)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, fmt.Errorf("%w: file changed during open", errStagePathBlocked)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPluginStageServeBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxPluginStageServeBytes {
		return nil, fmt.Errorf("%w: asset exceeds size limit", errStagePathBlocked)
	}
	return data, nil
}

type stageProtocolLookup func(ctx context.Context) (pluginID, pluginRoot string, ok bool)

func lookupCallerPluginStage(ctx context.Context) (string, string, bool) {
	page := mygo.CallerPage(ctx)
	b, ok := lookupStagePage(page)
	if !ok || b.workspace == nil {
		return "", "", false
	}
	sup := b.workspace.Plugins()
	if sup == nil {
		return "", "", false
	}
	active := sup.ActiveStage()
	if active == nil || active.PluginID != b.pluginID || active.StageID != b.stageID || strings.TrimSpace(active.PluginRoot) == "" {
		return "", "", false
	}
	return active.PluginID, active.PluginRoot, true
}

func servePluginStageProtocol(w http.ResponseWriter, r *http.Request) {
	handlePluginStageRequest(w, r, lookupCallerPluginStage)
}

func setStageOfflineHeaders(h http.Header) {
	h.Set("Content-Security-Policy", pluginStageCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
}

func handlePluginStageRequest(w http.ResponseWriter, r *http.Request, lookup stageProtocolLookup) {
	setStageOfflineHeaders(w.Header())

	if r == nil || r.URL == nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if lookup == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	boundPluginID, pluginRoot, ok := lookup(r.Context())
	if !ok || boundPluginID == "" || pluginRoot == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.User != nil || r.URL.Port() != "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reqPluginID, hostOK := parsePluginStageHost(r.URL.Host)
	if !hostOK {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(reqPluginID, boundPluginID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rawLower := strings.ToLower(r.URL.RawPath)
	if strings.Contains(rawLower, "..") || strings.Contains(rawLower, "%2e%2e") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	relPath, err := normalizeStageWebRelPath(r.URL.Path)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	data, err := openStageWebFile(pluginRoot, relPath)
	if err != nil {
		switch {
		case errors.Is(err, errStagePathBlocked):
			http.Error(w, "forbidden", http.StatusForbidden)
		case os.IsNotExist(err):
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "unable to read stage asset", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", stageContentType(relPath))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func isAllowedStageNavigation(rawURL, pluginID string) bool {
	if strings.TrimSpace(pluginID) == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.User != nil || u.Port() != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case pluginStageScheme:
		host, ok := parsePluginStageHost(u.Host)
		return ok && strings.EqualFold(host, pluginID)
	case "http":
		if !strings.HasSuffix(strings.ToLower(u.Host), pluginStageWinHostSuffix) {
			return false
		}
		host, ok := parsePluginStageHost(u.Host)
		return ok && strings.EqualFold(host, pluginID)
	default:
		return false
	}
}

type stageBridgeService struct{}

func resolveBridgeStage(ctx context.Context) (*Workspace, *plugins.Supervisor, *plugins.ActiveStage, error) {
	page := mygo.CallerPage(ctx)
	b, ok := lookupStagePage(page)
	if !ok || b.workspace == nil {
		return nil, nil, nil, errors.New("stage page is not registered")
	}
	sup := b.workspace.Plugins()
	if sup == nil {
		return nil, nil, nil, errors.New("plugin supervisor unavailable")
	}
	active := sup.ActiveStage()
	if active == nil || active.PluginID != b.pluginID || active.StageID != b.stageID {
		return nil, nil, nil, errors.New("stage is no longer active")
	}
	return b.workspace, sup, active, nil
}

func (s *stageBridgeService) GetInitState(ctx context.Context) (any, error) {
	_, _, active, err := resolveBridgeStage(ctx)
	if err != nil {
		return nil, err
	}
	return buildStageStatePayload(active), nil
}

func (s *stageBridgeService) Invoke(ctx context.Context, action string, payload any) (any, error) {
	_, sup, active, err := resolveBridgeStage(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := sup.InvokeStageAction(ctx, active.PluginID, active.StageID, action, payload)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func (s *stageBridgeService) Close(ctx context.Context) error {
	page := mygo.CallerPage(ctx)
	b, ok := lookupStagePage(page)
	if !ok || b.workspace == nil {
		return errors.New("stage page is not registered")
	}
	if sup := b.workspace.Plugins(); sup != nil {
		sup.CloseStage(b.pluginID, b.stageID)
	}
	b.workspace.RequestRedraw()
	return nil
}

func buildStageStatePayload(st *plugins.ActiveStage) map[string]any {
	out := map[string]any{}
	if st != nil && len(bytes.TrimSpace(st.State)) > 0 {
		var decoded any
		if err := json.Unmarshal(st.State, &decoded); err == nil && decoded != nil {
			if m, ok := decoded.(map[string]any); ok {
				out = m
			} else {
				out["value"] = decoded
			}
		}
	}
	if st != nil {
		if _, ok := out["plugin_id"]; !ok {
			out["plugin_id"] = st.PluginID
		}
		if _, ok := out["stage_id"]; !ok {
			out["stage_id"] = st.StageID
		}
		if _, ok := out["route"]; !ok {
			out["route"] = st.Route
		}
	}
	if _, ok := out["locale"]; !ok {
		out["locale"] = string(theme.Language())
	}
	if _, ok := out["dark"]; !ok {
		out["dark"] = theme.IsDark()
	}
	return out
}

func emitStageEvent(page *mygo.Page, event string, payload any) {
	if page == nil || pluginStageEvent == nil || strings.TrimSpace(event) == "" {
		return
	}
	_ = pluginStageEvent.EmitPage(page, map[string]any{
		"event":   event,
		"payload": payload,
	})
}

type stageWebViewHandle interface {
	Page() *mygo.Page
	IsDestroyed() bool
	Destroy()
	AttachHandlers(onNav func(*mygo.NavigateEvent)) func()
	LoadURL(url string) error
	WebView() *mygo.WebView
}

type mygoStageWebView struct {
	wv *mygo.WebView
}

func (h *mygoStageWebView) Page() *mygo.Page {
	if h == nil || h.wv == nil {
		return nil
	}
	return h.wv.Page()
}

func (h *mygoStageWebView) IsDestroyed() bool {
	return h == nil || h.wv == nil || h.wv.IsDestroyed()
}

func (h *mygoStageWebView) Destroy() {
	if h != nil && h.wv != nil && !h.wv.IsDestroyed() {
		h.wv.Destroy()
	}
}

func (h *mygoStageWebView) AttachHandlers(onNav func(*mygo.NavigateEvent)) func() {
	page := h.Page()
	if page == nil {
		return nil
	}
	page.SetWindowOpenHandler(func(_ mygo.WindowOpenRequest) *mygo.WindowOptions {
		return nil
	})
	return page.OnWillNavigate(onNav)
}

func (h *mygoStageWebView) LoadURL(rawURL string) error {
	page := h.Page()
	if page == nil {
		return errors.New("stage webview page unavailable")
	}
	return page.LoadURL(rawURL)
}

func (h *mygoStageWebView) WebView() *mygo.WebView {
	if h == nil {
		return nil
	}
	return h.wv
}

func pluginStageKey(stage *plugins.ActiveStage) string {
	if stage == nil {
		return ""
	}
	return stage.PluginID + "\x00" + stage.StageID + "\x00" + stage.Entry
}

type pluginStageController struct {
	w *Workspace

	createWebView func(win *mygo.Window, opts mygo.WebViewOptions) (stageWebViewHandle, error)

	mu         sync.Mutex
	win        *mygo.Window
	handle     stageWebViewHandle
	wv         *mygo.WebView
	page       *mygo.Page
	unsubNav   func()
	pluginID   string
	stageID    string
	entry      string
	route      string
	lastDark   bool
	lastLocale string
	createErr  string
	failedKey  string
	failedWin  *mygo.Window
	generation uint64
}

func newPluginStageController(w *Workspace) *pluginStageController {
	return &pluginStageController{w: w}
}

func (c *pluginStageController) detachWebViewLocked() (stageWebViewHandle, *mygo.Page, func()) {
	handle := c.handle
	page := c.page
	unsub := c.unsubNav
	c.win = nil
	c.handle = nil
	c.wv = nil
	c.page = nil
	c.unsubNav = nil
	c.pluginID = ""
	c.stageID = ""
	c.entry = ""
	c.route = ""
	c.createErr = ""
	c.generation++
	return handle, page, unsub
}

func cleanupDetachedWebView(handle stageWebViewHandle, page *mygo.Page, unsub func()) {
	unregisterStagePage(page)
	if unsub != nil {
		unsub()
	}
	if handle != nil && !handle.IsDestroyed() {
		handle.Destroy()
	}
}

// Destroy releases the active stage WebView and unregisters its page binding.
func (c *pluginStageController) Destroy() {
	if c == nil {
		return
	}
	c.mu.Lock()
	handle, page, unsub := c.detachWebViewLocked()
	c.failedKey = ""
	c.failedWin = nil
	c.mu.Unlock()
	cleanupDetachedWebView(handle, page, unsub)
}

// OnStageChange responds to supervisor ActiveStage transitions.
func (c *pluginStageController) OnStageChange(stage *plugins.ActiveStage) {
	if c == nil {
		return
	}
	var (
		oldH     stageWebViewHandle
		oldPage  *mygo.Page
		oldUnsub func()
		emitPage *mygo.Page
	)
	c.mu.Lock()
	if stage == nil {
		oldH, oldPage, oldUnsub = c.detachWebViewLocked()
		c.failedKey = ""
		c.failedWin = nil
	} else if c.handle != nil && !c.handle.IsDestroyed() &&
		c.pluginID == stage.PluginID && c.stageID == stage.StageID && c.entry == stage.Entry {
		c.route = stage.Route
		c.createErr = ""
		emitPage = c.page
	} else {
		oldH, oldPage, oldUnsub = c.detachWebViewLocked()
		if c.failedKey != pluginStageKey(stage) {
			c.failedKey = ""
			c.failedWin = nil
		}
	}
	c.mu.Unlock()

	cleanupDetachedWebView(oldH, oldPage, oldUnsub)
	if emitPage != nil && stage != nil {
		emitStageEvent(emitPage, "stage.state", buildStageStatePayload(stage))
	}
	if c.w != nil {
		c.w.RequestRedraw()
	}
}

// OnStageEmit forwards a host.stage.emit event to the matching open WebView page.
func (c *pluginStageController) OnStageEmit(ev plugins.StageEvent) {
	if c == nil {
		return
	}
	c.mu.Lock()
	var target *mygo.Page
	if c.page != nil && c.handle != nil && !c.handle.IsDestroyed() &&
		c.pluginID == ev.PluginID && c.stageID == ev.StageID {
		target = c.page
	}
	c.mu.Unlock()
	if target == nil {
		return
	}
	var payload any = map[string]any{}
	if trimmed := bytes.TrimSpace(ev.Payload); len(trimmed) > 0 {
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err == nil {
			payload = decoded
		}
	}
	emitStageEvent(target, ev.Event, payload)
}

// NotifyThemeChanged pushes theme.changed to an open stage WebView when dark/mode updates.
func (c *pluginStageController) NotifyThemeChanged(mode string) {
	if c == nil {
		return
	}
	dark := theme.IsDark()
	c.mu.Lock()
	var target *mygo.Page
	if c.page != nil && c.handle != nil && !c.handle.IsDestroyed() {
		c.lastDark = dark
		target = c.page
	}
	c.mu.Unlock()
	if target == nil {
		return
	}
	if strings.TrimSpace(mode) == "" {
		mode = "system"
	}
	emitStageEvent(target, "theme.changed", map[string]any{
		"theme": mode,
		"dark":  dark,
	})
}

// NotifyLocaleChanged pushes locale.changed to an open stage WebView.
func (c *pluginStageController) NotifyLocaleChanged() {
	if c == nil {
		return
	}
	locale := string(theme.Language())
	c.mu.Lock()
	var target *mygo.Page
	if c.page != nil && c.handle != nil && !c.handle.IsDestroyed() && c.lastLocale != locale {
		c.lastLocale = locale
		target = c.page
	}
	c.mu.Unlock()
	if target == nil {
		return
	}
	emitStageEvent(target, "locale.changed", map[string]any{
		"locale": locale,
	})
}

func stagePaletteHex(col ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
}

func (c *pluginStageController) ensureWebView(win *mygo.Window, sup *plugins.Supervisor, stage *plugins.ActiveStage, pal theme.Palette) (*mygo.WebView, string) {
	if c == nil || sup == nil || stage == nil {
		return nil, ""
	}
	locale := string(theme.Language())
	dark := pal.Dark
	stageKey := pluginStageKey(stage)

	c.mu.Lock()
	if c.createWebView == nil && (win == nil || win.IsDestroyed()) {
		c.mu.Unlock()
		return nil, ""
	}
	if c.handle != nil && !c.handle.IsDestroyed() && c.win == win &&
		c.pluginID == stage.PluginID && c.stageID == stage.StageID && c.entry == stage.Entry {
		wv := c.wv
		page := c.page
		themeChanged := c.lastDark != dark
		localeChanged := c.lastLocale != locale
		c.lastDark = dark
		c.lastLocale = locale
		c.mu.Unlock()
		if themeChanged {
			emitStageEvent(page, "theme.changed", map[string]any{"dark": dark})
		}
		if localeChanged {
			emitStageEvent(page, "locale.changed", map[string]any{"locale": locale})
		}
		return wv, ""
	}
	if c.failedKey == stageKey && c.failedWin == win {
		errMsg := c.createErr
		c.mu.Unlock()
		return nil, errMsg
	}
	oldH, oldPage, oldUnsub := c.detachWebViewLocked()
	c.failedKey = ""
	c.failedWin = nil
	gen := c.generation
	createFn := c.createWebView
	c.mu.Unlock()
	cleanupDetachedWebView(oldH, oldPage, oldUnsub)

	if err := RegisterPluginStageRuntime(); err != nil {
		files.LogErrorf("register plugin stage runtime failed: %v", err)
		c.mu.Lock()
		cur := sup.ActiveStage()
		if c.generation == gen && cur != nil && pluginStageKey(cur) == stageKey {
			c.failedKey = stageKey
			c.failedWin = win
			c.createErr = err.Error()
		}
		c.mu.Unlock()
		return nil, err.Error()
	}

	if createFn == nil {
		createFn = func(w *mygo.Window, opts mygo.WebViewOptions) (stageWebViewHandle, error) {
			wv, err := w.NewWebView(opts)
			if err != nil {
				return nil, err
			}
			return &mygoStageWebView{wv: wv}, nil
		}
	}

	rules := ensurePluginStageContentRules()
	handle, err := createFn(win, mygo.WebViewOptions{
		BackgroundColor: stagePaletteHex(pal.Paper),
		Page: mygo.PageOptions{
			PreloadScript: pluginStagePreloadScript,
			ContentRules:  rules,
			DevTools:      mygo.DevToolsDisabled,
		},
	})
	if err != nil {
		c.mu.Lock()
		cur := sup.ActiveStage()
		if c.generation == gen && cur != nil && pluginStageKey(cur) == stageKey {
			c.failedKey = stageKey
			c.failedWin = win
			c.createErr = err.Error()
		}
		c.mu.Unlock()
		return nil, err.Error()
	}

	page := handle.Page()
	if page == nil {
		handle.Destroy()
		errMsg := "stage webview page unavailable"
		c.mu.Lock()
		cur := sup.ActiveStage()
		if c.generation == gen && cur != nil && pluginStageKey(cur) == stageKey {
			c.failedKey = stageKey
			c.failedWin = win
			c.createErr = errMsg
		}
		c.mu.Unlock()
		return nil, errMsg
	}

	c.mu.Lock()
	cur := sup.ActiveStage()
	if c.generation != gen || cur == nil || pluginStageKey(cur) != stageKey || (c.createWebView == nil && win.IsDestroyed()) {
		c.mu.Unlock()
		handle.Destroy()
		return nil, ""
	}
	if c.handle != nil && !c.handle.IsDestroyed() {
		// Another frame/goroutine already installed a WebView; discard this one.
		existing := c.wv
		c.mu.Unlock()
		handle.Destroy()
		return existing, ""
	}

	pluginID := cur.PluginID
	stageID := cur.StageID
	entry := cur.Entry
	registerStagePage(page, stagePageBinding{
		workspace: c.w,
		pluginID:  pluginID,
		stageID:   stageID,
		entry:     entry,
	})
	unsubNav := handle.AttachHandlers(func(e *mygo.NavigateEvent) {
		if e == nil {
			return
		}
		if !isAllowedStageNavigation(e.URL, pluginID) {
			e.PreventDefault()
		}
	})

	c.win = win
	c.handle = handle
	c.wv = handle.WebView()
	c.page = page
	c.unsubNav = unsubNav
	c.pluginID = pluginID
	c.stageID = stageID
	c.entry = entry
	c.route = cur.Route
	c.lastDark = dark
	c.lastLocale = locale
	c.createErr = ""
	c.failedKey = ""
	c.failedWin = nil
	wv := c.wv
	c.mu.Unlock()

	relEntry := strings.TrimPrefix(entry, "web/")
	targetURL := pluginStageScheme + "://" + pluginID + "/" + strings.TrimPrefix(relEntry, "/")
	if err := handle.LoadURL(targetURL); err != nil {
		files.LogWarnf("load plugin stage URL %s failed: %v", targetURL, err)
	}
	return wv, ""
}

func (c *pluginStageController) renderPlaceholder(uiCtx *ui.Context, pal theme.Palette, sup *plugins.Supervisor, stage *plugins.ActiveStage, errMsg string) {
	title := strings.TrimSpace(stage.Title)
	if title == "" {
		title = stage.StageID
	}
	if title == "" {
		title = stage.PluginID
	}

	wrap := ui.Column(uiCtx.Key("plugin-stage-placeholder-wrap")).
		Fill().MinWidth(0).MinHeight(0).
		Background(pal.Shell).
		Padding(theme.Space24).
		Justify(ui.Center).
		AlignItems(ui.Center)
	wrap.Children(func() {
		card := blockui.Elevate(
			ui.Column(uiCtx.Key("plugin-stage-placeholder")).
				Width(pluginStagePlaceholderCardW).MaxWidth(pluginStagePlaceholderCardW).MinWidth(0).
				Background(pal.Card).Border(1, pal.Hairline).Radius(theme.RadiusLG).
				Padding(theme.Space24).Gap(theme.Space12),
			theme.ElevationCard,
		)
		blockui.Probe(card, "plugin-stage-placeholder")
		card.Children(func() {
			header := ui.Row(uiCtx.Key("plugin-stage-placeholder-header")).
				FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(theme.Space8)
			header.Children(func() {
				ui.Text(uiCtx, title).
					Grow(1).MinWidth(0).SingleLine().Ellipsis("…").
					FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
					FontWeight(theme.WeightTitle).TextColor(pal.Text)
				closeIcon := blockui.IconButton(uiCtx, icons.Close, blockui.IconButtonOptions{
					ID:      "plugin-stage-placeholder-close-icon",
					Label:   theme.Tr("关闭", "Close"),
					Tooltip: theme.Tr("关闭", "Close"),
				})
				if closeIcon.Clicked() {
					sup.CloseStage(stage.PluginID, stage.StageID)
				}
			})

			meta := stage.PluginID
			if strings.TrimSpace(stage.Route) != "" {
				meta += " · " + stage.Route
			}
			ui.Text(uiCtx, meta).
				FillWidth().MinWidth(0).SingleLine().Ellipsis("…").
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				TextColor(pal.TextSecondary)

			if strings.TrimSpace(errMsg) != "" {
				ui.Text(uiCtx, errMsg).
					FillWidth().MinWidth(0).
					FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					TextColor(pal.Danger)
			}

			actions := ui.Row(uiCtx.Key("plugin-stage-placeholder-actions")).
				FillWidth().MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(theme.Space8)
			actions.Children(func() {
				closeBtn := ui.ButtonBase(uiCtx.Key("plugin-stage-placeholder-close-btn")).
					Height(theme.HeightControl).Padding(0, theme.Space16).
					Radius(theme.RadiusMD).Center().Cursor(ui.CursorPointer).
					Background(pal.Hover).Border(1, pal.BtnBorder).
					Label(theme.Tr("关闭", "Close"))
				closeBtn.Children(func() {
					ui.Text(uiCtx, theme.Tr("关闭", "Close")).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						FontWeight(theme.WeightControl).TextColor(pal.Text)
				})
				if closeBtn.Clicked() {
					sup.CloseStage(stage.PluginID, stage.StageID)
				}
			})
		})
	})
}

// RenderPluginStage renders the active plugin WebView stage (or a themed placeholder
// when running headless without a native window).
func (w *Workspace) RenderPluginStage(c *ui.Context, pal theme.Palette) {
	if w == nil || w.stageCtrl == nil {
		return
	}
	sup := w.Plugins()
	if sup == nil {
		w.stageCtrl.Destroy()
		return
	}
	stage := sup.ActiveStage()
	if stage == nil {
		w.stageCtrl.Destroy()
		return
	}

	if !(w.modals.HasActiveModal() || w.hasActivePluginModal() || w.modals.IsRecordingShortcut()) && c.Shortcut(0, ui.KeyEscape) {
		sup.CloseStage(stage.PluginID, stage.StageID)
		return
	}

	win := w.Window()
	if win == nil || win.IsDestroyed() {
		w.stageCtrl.renderPlaceholder(c, pal, sup, stage, "")
		return
	}
	wv, errMsg := w.stageCtrl.ensureWebView(win, sup, stage, pal)
	if wv == nil || wv.IsDestroyed() {
		w.stageCtrl.renderPlaceholder(c, pal, sup, stage, errMsg)
		return
	}
	ui.WebView(c.Key("plugin-stage-webview-"+stage.PluginID+"-"+stage.StageID), wv).Fill()
}
