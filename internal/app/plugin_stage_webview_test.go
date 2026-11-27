package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"block/internal/plugins"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func setupTestStagePluginDir(t *testing.T) string {
	t.Helper()
	pluginRoot := t.TempDir()
	webDir := filepath.Join(pluginRoot, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html><html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "app.js"), []byte("export const ready = true;\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "style.css"), []byte("body { margin: 0; }\n"), 0o644); err != nil {
		t.Fatalf("write style.css: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "secret.txt"), []byte("top-secret"), 0o600); err != nil {
		t.Fatalf("write secret.txt: %v", err)
	}
	return pluginRoot
}

func assertStageOfflineHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Security-Policy"); got != pluginStageCSP {
		t.Fatalf("unexpected CSP header:\n got: %q\nwant: %q", got, pluginStageCSP)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected X-Content-Type-Options: %q", got)
	}
}

func TestPluginStageProtocolReadAndHeaders(t *testing.T) {
	pluginRoot := setupTestStagePluginDir(t)
	lookup := func(context.Context) (string, string, bool) {
		return "alpha-plugin", pluginRoot, true
	}

	cases := []struct {
		url         string
		wantType    string
		wantContain string
	}{
		{
			url:         "block-plugin://alpha-plugin/index.html",
			wantType:    "text/html; charset=utf-8",
			wantContain: "<body>ok</body>",
		},
		{
			url:         "block-plugin://alpha-plugin/app.js",
			wantType:    "text/javascript; charset=utf-8",
			wantContain: "ready = true",
		},
		{
			url:         "block-plugin://alpha-plugin/style.css",
			wantType:    "text/css; charset=utf-8",
			wantContain: "margin: 0",
		},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		rec := httptest.NewRecorder()
		handlePluginStageRequest(rec, req, lookup)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d (%s)", tc.url, rec.Code, rec.Body.String())
		}
		assertStageOfflineHeaders(t, rec)
		if got := rec.Header().Get("Content-Type"); got != tc.wantType {
			t.Fatalf("%s: Content-Type = %q, want %q", tc.url, got, tc.wantType)
		}
		if !strings.Contains(rec.Body.String(), tc.wantContain) {
			t.Fatalf("%s: body %q missing %q", tc.url, rec.Body.String(), tc.wantContain)
		}
	}
}

func TestPluginStageProtocolHostSuffixAndNavigation(t *testing.T) {
	pluginRoot := setupTestStagePluginDir(t)
	lookup := func(context.Context) (string, string, bool) {
		return "alpha-plugin", pluginRoot, true
	}

	for _, rawURL := range []string{
		"http://alpha-plugin.block-plugin.localhost/app.js",
		"block-plugin://alpha-plugin.block-plugin.localhost/app.js",
	} {
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		rec := httptest.NewRecorder()
		handlePluginStageRequest(rec, req, lookup)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d (%s)", rawURL, rec.Code, rec.Body.String())
		}
		assertStageOfflineHeaders(t, rec)
		if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
			t.Fatalf("%s: Content-Type = %q", rawURL, got)
		}
	}

	if !isAllowedStageNavigation("block-plugin://alpha-plugin/index.html", "alpha-plugin") {
		t.Fatal("expected own block-plugin:// navigation to be allowed")
	}
	if !isAllowedStageNavigation("http://alpha-plugin.block-plugin.localhost/app.js", "alpha-plugin") {
		t.Fatal("expected own Windows localhost-mapped navigation to be allowed")
	}
	for _, blocked := range []string{
		"https://example.com/",
		"http://example.com/",
		"block-plugin://beta-plugin/index.html",
		"http://beta-plugin.block-plugin.localhost/index.html",
		"javascript:alert(1)",
		"file:///etc/passwd",
	} {
		if isAllowedStageNavigation(blocked, "alpha-plugin") {
			t.Fatalf("expected navigation %q to be blocked", blocked)
		}
	}
}

func TestPluginStageProtocolCrossPluginCallerPageBlocked(t *testing.T) {
	pluginRootA := setupTestStagePluginDir(t)
	pluginRootB := setupTestStagePluginDir(t)

	pageA := &mygo.Page{}
	pageB := &mygo.Page{}
	registerStagePage(pageA, stagePageBinding{pluginID: "alpha-plugin", stageID: "main"})
	registerStagePage(pageB, stagePageBinding{pluginID: "beta-plugin", stageID: "main"})
	t.Cleanup(func() {
		unregisterStagePage(pageA)
		unregisterStagePage(pageB)
	})

	lookupByPage := func(page *mygo.Page) stageProtocolLookup {
		return func(context.Context) (string, string, bool) {
			b, ok := lookupStagePage(page)
			if !ok {
				return "", "", false
			}
			root := pluginRootA
			if b.pluginID == "beta-plugin" {
				root = pluginRootB
			}
			return b.pluginID, root, true
		}
	}

	// pageA bound to alpha-plugin must not read beta-plugin assets.
	reqCross := httptest.NewRequest(http.MethodGet, "block-plugin://beta-plugin/index.html", nil)
	recCross := httptest.NewRecorder()
	handlePluginStageRequest(recCross, reqCross, lookupByPage(pageA))
	if recCross.Code != http.StatusForbidden {
		t.Fatalf("cross-plugin read: expected 403, got %d", recCross.Code)
	}
	assertStageOfflineHeaders(t, recCross)

	// Unregistered page must be rejected.
	reqUnknown := httptest.NewRequest(http.MethodGet, "block-plugin://alpha-plugin/index.html", nil)
	recUnknown := httptest.NewRecorder()
	handlePluginStageRequest(recUnknown, reqUnknown, lookupByPage(&mygo.Page{}))
	if recUnknown.Code != http.StatusForbidden {
		t.Fatalf("unregistered page read: expected 403, got %d", recUnknown.Code)
	}
}

func TestPluginStageProtocolTraversalAndSymlinkBlocked(t *testing.T) {
	pluginRoot := setupTestStagePluginDir(t)
	lookup := func(context.Context) (string, string, bool) {
		return "alpha-plugin", pluginRoot, true
	}

	for _, badURL := range []string{
		"block-plugin://alpha-plugin/../secret.txt",
		"block-plugin://alpha-plugin/sub/../../secret.txt",
		"block-plugin://alpha-plugin/%2e%2e/secret.txt",
		"block-plugin://alpha-plugin/..\\secret.txt",
	} {
		req := httptest.NewRequest(http.MethodGet, badURL, nil)
		rec := httptest.NewRecorder()
		handlePluginStageRequest(rec, req, lookup)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403 for traversal, got %d", badURL, rec.Code)
		}
		assertStageOfflineHeaders(t, rec)
	}

	// File symlink inside web/ pointing outside must be blocked.
	linkPath := filepath.Join(pluginRoot, "web", "leak.txt")
	if err := os.Symlink(filepath.Join(pluginRoot, "secret.txt"), linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reqSym := httptest.NewRequest(http.MethodGet, "block-plugin://alpha-plugin/leak.txt", nil)
	recSym := httptest.NewRecorder()
	handlePluginStageRequest(recSym, reqSym, lookup)
	if recSym.Code != http.StatusForbidden {
		t.Fatalf("symlink file read: expected 403, got %d", recSym.Code)
	}

	// Symlink directory inside web/ must be blocked.
	dirLink := filepath.Join(pluginRoot, "web", "dirlink")
	if err := os.Symlink(pluginRoot, dirLink); err != nil {
		t.Skipf("dir symlinks unavailable: %v", err)
	}
	reqDirSym := httptest.NewRequest(http.MethodGet, "block-plugin://alpha-plugin/dirlink/secret.txt", nil)
	recDirSym := httptest.NewRecorder()
	handlePluginStageRequest(recDirSym, reqDirSym, lookup)
	if recDirSym.Code != http.StatusForbidden {
		t.Fatalf("symlink dir read: expected 403, got %d", recDirSym.Code)
	}
}

func TestPluginStageRuntimeIdempotentAndHeadlessPlaceholder(t *testing.T) {
	if err := RegisterPluginStageRuntime(); err != nil {
		t.Fatalf("first RegisterPluginStageRuntime failed: %v", err)
	}
	if err := RegisterPluginStageRuntime(); err != nil {
		t.Fatalf("second RegisterPluginStageRuntime failed: %v", err)
	}
	if !mygo.Protocol.IsHandled(pluginStageScheme) {
		t.Fatal("expected block-plugin protocol to be registered")
	}

	ws := NewWorkspace(t.TempDir())
	t.Cleanup(func() {
		if sup := ws.Plugins(); sup != nil {
			sup.ShutdownAll()
		}
		ws.CleanupPluginStage()
	})

	// Headless placeholder renders without panicking when win == nil.
	stage := &plugins.ActiveStage{
		PluginID:   "alpha-plugin",
		StageID:    "main-stage",
		Title:      "Plugin Stage",
		Entry:      "web/index.html",
		FullWindow: true,
		Route:      "overview",
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ws.stageCtrl.renderPlaceholder(c, theme.Ocean(), ws.Plugins(), stage, "")
	}, 1000, 700)
	tt.Frame()

	st := ws.pluginUI.Status()
	if st.Ready {
		t.Fatalf("expected unconfigured AI status Ready=false, got %+v", st)
	}
}

func evalOfflineContentRules(t *testing.T, rawURL string) bool {
	t.Helper()
	var rules []struct {
		Trigger struct {
			URLFilter     string `json:"url-filter"`
			CaseSensitive bool   `json:"url-filter-is-case-sensitive"`
		} `json:"trigger"`
		Action struct {
			Type string `json:"type"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(pluginStageOfflineRulesJSON), &rules); err != nil {
		t.Fatalf("unmarshal pluginStageOfflineRulesJSON: %v", err)
	}
	blocked := false
	for _, r := range rules {
		pat := r.Trigger.URLFilter
		if !r.Trigger.CaseSensitive {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			t.Fatalf("compile rule %q: %v", r.Trigger.URLFilter, err)
		}
		if re.MatchString(rawURL) {
			switch r.Action.Type {
			case "block":
				blocked = true
			case "ignore-previous-rules":
				blocked = false
			}
		}
	}
	return blocked
}

func TestPluginStageOfflineContentRulesRejectsPathBypass(t *testing.T) {
	// Valid mapped localhost stage asset URL must be permitted.
	if evalOfflineContentRules(t, "http://alpha-plugin.block-plugin.localhost/index.html") {
		t.Fatal("expected http://alpha-plugin.block-plugin.localhost/index.html to be allowed")
	}

	// Path-based bypass attempts must remain blocked by content rules AND navigation filter.
	for _, bypassURL := range []string{
		"http://evil.com/.block-plugin.localhost/",
		"http://evil.com/alpha.block-plugin.localhost/x",
		"https://alpha-plugin.block-plugin.localhost/index.html",
		"https://evil.com/data",
		"http://evil.com/data",
		"wss://evil.com/socket",
	} {
		if !evalOfflineContentRules(t, bypassURL) {
			t.Fatalf("expected offline content rules to block %q", bypassURL)
		}
		if isAllowedStageNavigation(bypassURL, "alpha-plugin") {
			t.Fatalf("expected isAllowedStageNavigation to reject %q", bypassURL)
		}
	}
}

type fakeStageWebView struct {
	mu        sync.Mutex
	page      *mygo.Page
	destroyed bool
	loadedURL string
	navHook   func(*mygo.NavigateEvent)
}

func (f *fakeStageWebView) Page() *mygo.Page {
	return f.page
}

func (f *fakeStageWebView) IsDestroyed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destroyed
}

func (f *fakeStageWebView) Destroy() {
	f.mu.Lock()
	f.destroyed = true
	f.mu.Unlock()
}

func (f *fakeStageWebView) AttachHandlers(onNav func(*mygo.NavigateEvent)) func() {
	f.mu.Lock()
	f.navHook = onNav
	f.mu.Unlock()
	return func() {}
}

func (f *fakeStageWebView) LoadURL(rawURL string) error {
	f.mu.Lock()
	f.loadedURL = rawURL
	f.mu.Unlock()
	return nil
}

func (f *fakeStageWebView) WebView() *mygo.WebView {
	return nil
}

func setupStageTestPlugin(t *testing.T, appDataDir, pluginID string) {
	t.Helper()
	srcDir := filepath.Join(t.TempDir(), pluginID)
	if err := os.MkdirAll(filepath.Join(srcDir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "web", "index.html"), []byte("<!doctype html><html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainJS := `
import { callHost, serve } from '/runtime/block-sdk.js';
serve((method, params) => {
  if (method === 'plugin.initialize') return { ready: true };
  if (method === 'plugin.command' && params?.command_id === 'open') {
    callHost('host.stage.open', { id: 'main-stage' });
    return { ok: true };
  }
  if (method === 'plugin.shutdown') return { ok: true };
  return {};
});
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.js"), []byte(mainJS), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := plugins.Manifest{
		App:           plugins.TargetApp,
		SchemaVersion: plugins.CurrentSchemaVersion,
		ID:            pluginID,
		Name:          plugins.LocalizedText{Zh: "测试舞台", En: "Test Stage"},
		Version:       "1.0.0",
		Author:        "tester",
		Summary:       plugins.LocalizedText{Zh: "舞台测试", En: "Stage test"},
		Entry:         plugins.EntrySpec{JS: "main.js"},
		Permissions:   []plugins.Permission{plugins.PermUICommands},
		Icon:          plugins.DuoIconSpec{ViewBox: "0 0 24 24", StrokePaths: []string{"M4 4H20V20H4Z"}},
		Contributes: &plugins.Contributes{
			Commands: []plugins.CommandContrib{{
				ID:    "open",
				Title: plugins.LocalizedText{Zh: "打开", En: "Open"},
			}},
			Stages: []plugins.StageContrib{{
				ID:         "main-stage",
				Title:      plugins.LocalizedText{Zh: "主舞台", En: "Main Stage"},
				Entry:      "web/index.html",
				FullWindow: true,
			}},
		},
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	store := plugins.NewStoreManager(appDataDir)
	if _, err := store.InstallFromDirectory(srcDir); err != nil {
		t.Fatalf("InstallFromDirectory: %v", err)
	}
	if err := store.SetEnabled(pluginID, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
}

func openStageAndWait(t *testing.T, sup *plugins.Supervisor, pluginID string, stageCh <-chan *plugins.ActiveStage) *plugins.ActiveStage {
	t.Helper()
	sup.RunCommandAsync(pluginID, "open", plugins.CommandContext{})
	deadline := time.After(15 * time.Second)
	for {
		select {
		case st := <-stageCh:
			if st != nil && st.PluginID == pluginID {
				return st
			}
		case <-deadline:
			t.Fatalf("timed out waiting for stage %s to open", pluginID)
		}
	}
}

func TestPluginStageControllerConcurrentCloseAndFailedKey(t *testing.T) {
	t.Setenv("PATH", "")

	appDataDir := t.TempDir()
	setupStageTestPlugin(t, appDataDir, "race-stage-plugin")

	var ctrl *pluginStageController
	stageCh := make(chan *plugins.ActiveStage, 8)
	sup := plugins.NewSupervisor(plugins.SupervisorConfig{
		AppDataDir: appDataDir,
		GetLocale:  func() string { return "en" },
		OnStageChange: func(st *plugins.ActiveStage) {
			if ctrl != nil {
				ctrl.OnStageChange(st)
			}
			select {
			case stageCh <- st:
			default:
			}
		},
	})
	t.Cleanup(sup.ShutdownAll)
	ctrl = newPluginStageController(nil)
	t.Cleanup(ctrl.Destroy)

	stage := openStageAndWait(t, sup, "race-stage-plugin", stageCh)

	// 1) Concurrent CloseStage while NewWebView is in flight must destroy the stale WebView and never mount it.
	createdHandle := &fakeStageWebView{page: &mygo.Page{}}
	ctrl.createWebView = func(_ *mygo.Window, _ mygo.WebViewOptions) (stageWebViewHandle, error) {
		sup.CloseStage(stage.PluginID, stage.StageID)
		return createdHandle, nil
	}
	wv, errMsg := ctrl.ensureWebView(nil, sup, stage, theme.Ocean())
	if wv != nil || errMsg != "" {
		t.Fatalf("expected nil webview and empty err after concurrent close, got wv=%v err=%q", wv, errMsg)
	}
	if !createdHandle.IsDestroyed() {
		t.Fatal("expected stale WebView created during concurrent CloseStage to be destroyed")
	}
	if _, ok := lookupStagePage(createdHandle.page); ok {
		t.Fatal("stale WebView page must not remain registered")
	}
	ctrl.mu.Lock()
	mounted := ctrl.handle != nil || ctrl.page != nil
	ctrl.mu.Unlock()
	if mounted {
		t.Fatal("stale WebView must not be mounted on controller")
	}

	// 2) Re-open stage and simulate WebView creation failure -> failedKey suppresses per-frame retries.
	stage = openStageAndWait(t, sup, "race-stage-plugin", stageCh)

	attempts := 0
	ctrl.createWebView = func(_ *mygo.Window, _ mygo.WebViewOptions) (stageWebViewHandle, error) {
		attempts++
		return nil, errors.New("webview init failed")
	}
	for i := 0; i < 5; i++ {
		_, gotErr := ctrl.ensureWebView(nil, sup, stage, theme.Ocean())
		if gotErr != "webview init failed" {
			t.Fatalf("frame %d: expected cached/create error, got %q", i, gotErr)
		}
	}
	if attempts != 1 {
		t.Fatalf("expected failedKey to prevent retries across frames, got %d attempts", attempts)
	}

	// Closing and reopening clears failedKey so creation can be retried once again.
	sup.CloseStage(stage.PluginID, stage.StageID)
	stage = openStageAndWait(t, sup, "race-stage-plugin", stageCh)
	_, _ = ctrl.ensureWebView(nil, sup, stage, theme.Ocean())
	if attempts != 2 {
		t.Fatalf("expected retry after close/reopen, got %d attempts", attempts)
	}
}
