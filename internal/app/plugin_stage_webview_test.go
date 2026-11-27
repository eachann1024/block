package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
