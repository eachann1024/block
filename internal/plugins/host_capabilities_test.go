package plugins

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestCapabilityRuntime(t *testing.T, perms []Permission, domains []string, execAllow []string) (*Runtime, string, string, string, string) {
	t.Helper()
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugin")
	dataDir := filepath.Join(tmp, "apps-data", "cap-test")
	downloadsDir := filepath.Join(tmp, "Downloads")
	notebookDir := filepath.Join(tmp, "notebook")
	for _, d := range []string{pluginDir, dataDir, downloadsDir, notebookDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dataRoot, err := openPluginDataRoot(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataRoot.Close() })

	rt := &Runtime{
		exited: make(chan struct{}),
		reaped: make(chan struct{}),
		manifest: Manifest{
			App:            TargetApp,
			SchemaVersion:  CurrentSchemaVersion,
			ID:             "cap-test",
			Permissions:    perms,
			NetworkDomains: domains,
			ExecAllowlist:  execAllow,
		},
		cfg: HostConfig{
			PluginDir:    pluginDir,
			DataDir:      dataDir,
			DownloadsDir: downloadsDir,
			NotebookRoot: notebookDir,
			GetNotebooks: func() []string { return []string{notebookDir} },
		},
		dataRoot: dataRoot,
	}
	return rt, tmp, dataDir, downloadsDir, notebookDir
}

func mustDispatch(t *testing.T, rt *Runtime, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, rerr := rt.dispatchHostMethod(method, raw, nil)
	if rerr != nil {
		t.Fatalf("%s failed unexpectedly: %v", method, rerr)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustFailDispatch(t *testing.T, rt *Runtime, method string, params any, wantCode int) *rpcError {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	_, rerr := rt.dispatchHostMethod(method, raw, nil)
	if rerr == nil {
		t.Fatalf("%s unexpectedly succeeded", method)
	}
	if wantCode != 0 && rerr.Code != wantCode {
		t.Fatalf("%s error code = %d (%s), want %d", method, rerr.Code, rerr.Message, wantCode)
	}
	return rerr
}

func TestHostFSGrantsScopeAndOperations(t *testing.T) {
	// Without PermSystemFS, all host.fs.* calls must be rejected.
	rtNoPerm, _, dataDirNoPerm, _, _ := newTestCapabilityRuntime(t, []Permission{PermStorage}, nil, nil)
	mustFailDispatch(t, rtNoPerm, "host.fs.list_grants", map[string]any{}, -32001)
	mustFailDispatch(t, rtNoPerm, "host.fs.stat", map[string]any{"path": dataDirNoPerm}, -32001)

	// With PermSystemFS + PermNotesRead (but without PermNotesWrite), notebook is read-only.
	rt, tmp, dataDir, downloadsDir, notebookDir := newTestCapabilityRuntime(t, []Permission{PermSystemFS, PermNotesRead}, nil, nil)
	nbNote := filepath.Join(notebookDir, "readme.md")
	if err := os.WriteFile(nbNote, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reading notebook works; writing to notebook is blocked without PermNotesWrite.
	readRes := mustDispatch(t, rt, "host.fs.read", map[string]any{"path": nbNote})
	if readRes["content"] != "# Hello\n" {
		t.Fatalf("notebook read = %v", readRes["content"])
	}
	mustFailDispatch(t, rt, "host.fs.write", map[string]any{"path": nbNote, "content": "hack"}, -32001)

	// Outside scope is blocked, including .. traversal and symlinks pointing outside.
	outsideDir := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustFailDispatch(t, rt, "host.fs.read", map[string]any{"path": outsideFile}, -32001)
	mustFailDispatch(t, rt, "host.fs.read", map[string]any{"path": filepath.Join(dataDir, "..", "..", "outside", "secret.txt")}, -32001)

	symlinkEscape := filepath.Join(dataDir, "escape")
	if err := os.Symlink(outsideDir, symlinkEscape); err != nil {
		t.Fatal(err)
	}
	mustFailDispatch(t, rt, "host.fs.read", map[string]any{"path": filepath.Join(symlinkEscape, "secret.txt")}, -32001)
	mustFailDispatch(t, rt, "host.fs.stat", map[string]any{"path": symlinkEscape}, -32001)

	// Cannot tamper with granted-paths.json inside DataDir.
	mustFailDispatch(t, rt, "host.fs.write", map[string]any{
		"path":    filepath.Join(dataDir, grantedPathsFile),
		"content": `[{"path":"/","is_dir":true}]`,
	}, -32001)

	// Write atomic + mkdir_parents in DownloadsDir, plus iCloud placeholder detection.
	nestedFile := filepath.Join(downloadsDir, "sub", "dir", "hello.txt")
	mustDispatch(t, rt, "host.fs.write", map[string]any{
		"path":          nestedFile,
		"content":       "world",
		"atomic":        true,
		"mkdir_parents": true,
	})
	if err := os.WriteFile(filepath.Join(downloadsDir, ".hidden.md.icloud"), []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	listRes := mustDispatch(t, rt, "host.fs.list", map[string]any{"path": downloadsDir})
	entries := listRes["entries"].([]any)
	foundPlaceholder := false
	for _, rawEntry := range entries {
		entry := rawEntry.(map[string]any)
		if entry["name"] == ".hidden.md.icloud" && entry["is_icloud_placeholder"] == true {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Fatalf("expected .hidden.md.icloud with is_icloud_placeholder=true, got %v", entries)
	}

	// mkdir, rename, atomic_swap, delete, and fallbackSwapFilesInDir.
	swapDir := filepath.Join(dataDir, "swap-box")
	mustDispatch(t, rt, "host.fs.mkdir", map[string]any{"path": swapDir, "recursive": true})
	fileA := filepath.Join(swapDir, "a.txt")
	fileB := filepath.Join(swapDir, "b.txt")
	mustDispatch(t, rt, "host.fs.write", map[string]any{"path": fileA, "content": "AAA"})
	mustDispatch(t, rt, "host.fs.write", map[string]any{"path": fileB, "content": "BBB"})
	mustDispatch(t, rt, "host.fs.atomic_swap", map[string]any{"path_a": fileA, "path_b": fileB})
	if gotA := mustDispatch(t, rt, "host.fs.read", map[string]any{"path": fileA})["content"]; gotA != "BBB" {
		t.Fatalf("after atomic_swap a.txt = %v, want BBB", gotA)
	}
	if gotB := mustDispatch(t, rt, "host.fs.read", map[string]any{"path": fileB})["content"]; gotB != "AAA" {
		t.Fatalf("after atomic_swap b.txt = %v, want AAA", gotB)
	}

	// Test fallbackSwapFilesInDir directly as well.
	swapRoot, err := os.OpenRoot(swapDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := fallbackSwapFilesInDir(swapRoot, "a.txt", "b.txt"); err != nil {
		_ = swapRoot.Close()
		t.Fatalf("fallbackSwapFilesInDir: %v", err)
	}
	_ = swapRoot.Close()
	if gotA := mustDispatch(t, rt, "host.fs.read", map[string]any{"path": fileA})["content"]; gotA != "AAA" {
		t.Fatalf("after fallbackSwapFilesInDir a.txt = %v, want AAA", gotA)
	}

	renamed := filepath.Join(swapDir, "renamed.txt")
	mustDispatch(t, rt, "host.fs.rename", map[string]any{"from": fileB, "to": renamed})
	statOld := mustDispatch(t, rt, "host.fs.stat", map[string]any{"path": fileB})
	if statOld["exists"] != false {
		t.Fatalf("expected old file not to exist after rename: %v", statOld)
	}
	mustDispatch(t, rt, "host.fs.delete", map[string]any{"path": renamed})

	// Persisted GrantFSPath for directory & exact file.
	grantedDir := filepath.Join(tmp, "granted-dir")
	if err := os.MkdirAll(grantedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	grantedFile := filepath.Join(outsideDir, "single.txt")
	if err := os.WriteFile(grantedFile, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GrantFSPath(dataDir, grantedDir, "test_dir"); err != nil {
		t.Fatal(err)
	}
	if err := GrantFSPath(dataDir, grantedFile, "test_file"); err != nil {
		t.Fatal(err)
	}
	grantsRes := mustDispatch(t, rt, "host.fs.list_grants", map[string]any{})
	if len(grantsRes["grants"].([]any)) != 2 {
		t.Fatalf("expected 2 grants, got %v", grantsRes["grants"])
	}
	mustDispatch(t, rt, "host.fs.write", map[string]any{"path": filepath.Join(grantedDir, "new.txt"), "content": "dir-ok"})
	if got := mustDispatch(t, rt, "host.fs.read", map[string]any{"path": grantedFile})["content"]; got != "ok" {
		t.Fatalf("granted exact file read = %v", got)
	}
	// Sibling in outsideDir remains blocked when only single.txt was granted.
	mustFailDispatch(t, rt, "host.fs.read", map[string]any{"path": outsideFile}, -32001)
}

func TestSupervisorAndMenubarAutoGrantFSPath(t *testing.T) {
	tmp := t.TempDir()
	appDataDir := filepath.Join(tmp, "block-appdata")
	pickedDir := filepath.Join(tmp, "picked-dir")
	checklistDir := filepath.Join(tmp, "checklist-dir")
	droppedFile := filepath.Join(tmp, "dropped.png")
	for _, d := range []string{appDataDir, pickedDir, checklistDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(droppedFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	pluginID := "auto-grant-plugin"
	var pickTarget string
	s := NewSupervisor(SupervisorConfig{
		AppDataDir: appDataDir,
		GetNotebookRoot: func() string {
			return filepath.Join(tmp, "nb")
		},
		PickDirectory: func(_, _ string, onPicked func(string)) {
			onPicked(pickTarget)
		},
	})
	s.mu.Lock()
	s.installed = []InstalledApp{{
		Manifest: Manifest{
			ID: pluginID,
			Contributes: &Contributes{
				MenubarItems: []MenubarItemContrib{{
					ID: "mb",
					Drop: &MenubarDrop{
						Accept:     []string{MenubarAcceptFiles},
						Extensions: []string{"png"},
						Command:    "upload",
					},
				}},
			},
		},
		DataDir: filepath.Join(appDataDir, "apps-data", pluginID),
		Enabled: true,
	}}
	s.views[viewKey{pluginID: pluginID, notebookRoot: filepath.Join(tmp, "nb"), surface: SurfaceSettingsPage}] = &PluginViewState{
		Values: map[string]any{},
	}
	s.mu.Unlock()

	pickTarget = pickedDir
	s.PickSurfaceDirectoryForControl(pluginID, SurfaceSettingsPage, "export_dir", "Pick")
	pickTarget = checklistDir
	s.PickChecklistFolderForControl(pluginID, SurfaceSettingsPage, "folders", "Add Folder")

	var cc CommandContext
	ok := s.fillMenubarPayload(pluginID, &MenubarDrop{
		Accept:     []string{MenubarAcceptFiles},
		Extensions: []string{"png"},
	}, MenubarPayload{Files: []string{droppedFile}}, &cc)
	if !ok || len(cc.Files) != 1 {
		t.Fatalf("fillMenubarPayload failed: ok=%v cc=%+v", ok, cc)
	}

	grants, err := ListFSGrants(filepath.Join(appDataDir, "apps-data", pluginID))
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 3 {
		t.Fatalf("expected 3 persisted grants (picker, checklist, menubar_drop), got %d: %+v", len(grants), grants)
	}
}

func TestHostCryptoMethods(t *testing.T) {
	// Zero permissions: text/base64 hash & hmac must succeed.
	rtNoPerm, tmp, _, _, _ := newTestCapabilityRuntime(t, nil, nil, nil)
	res := mustDispatch(t, rtNoPerm, "host.crypto.hash", map[string]any{
		"algorithm": "sha256",
		"text":      "hello world",
	})
	wantSum := sha256.Sum256([]byte("hello world"))
	if res["hex"] != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("sha256 hex = %v, want %s", res["hex"], hex.EncodeToString(wantSum[:]))
	}
	if res["base64"] != base64.StdEncoding.EncodeToString(wantSum[:]) {
		t.Fatalf("sha256 base64 = %v", res["base64"])
	}

	// md5 with base64 input.
	raw := []byte{0x01, 0x02, 0x03}
	md5Res := mustDispatch(t, rtNoPerm, "host.crypto.hash", map[string]any{
		"algorithm": "md5",
		"base64":    base64.StdEncoding.EncodeToString(raw),
	})
	wantMD5 := md5.Sum(raw)
	if md5Res["hex"] != hex.EncodeToString(wantMD5[:]) {
		t.Fatalf("md5 hex = %v", md5Res["hex"])
	}

	// HMAC-SHA256.
	hmacRes := mustDispatch(t, rtNoPerm, "host.crypto.hmac", map[string]any{
		"algorithm": "sha256",
		"key_text":  "secret-key",
		"data_text": "payload",
	})
	mac := hmac.New(sha256.New, []byte("secret-key"))
	mac.Write([]byte("payload"))
	if hmacRes["hex"] != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("hmac hex = %v", hmacRes["hex"])
	}

	// file_path hashing requires PermSystemFS + scope check.
	fileInTemp := filepath.Join(tmp, "outside.bin")
	if err := os.WriteFile(fileInTemp, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustFailDispatch(t, rtNoPerm, "host.crypto.hash", map[string]any{
		"algorithm": "sha256",
		"file_path": fileInTemp,
	}, -32001)

	rtFS, _, dataDir, _, _ := newTestCapabilityRuntime(t, []Permission{PermSystemFS}, nil, nil)
	scopedFile := filepath.Join(dataDir, "blob.bin")
	if err := os.WriteFile(scopedFile, []byte("file-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustFailDispatch(t, rtFS, "host.crypto.hash", map[string]any{
		"algorithm": "sha256",
		"file_path": fileInTemp,
	}, -32001)

	fileHash := mustDispatch(t, rtFS, "host.crypto.hash", map[string]any{
		"algorithm": "sha256",
		"file_path": scopedFile,
	})
	wantFileSum := sha256.Sum256([]byte("file-payload"))
	if fileHash["hex"] != hex.EncodeToString(wantFileSum[:]) || int(fileHash["size"].(float64)) != len("file-payload") {
		t.Fatalf("file hash = %v", fileHash)
	}
}

func TestHostHTTPRequest(t *testing.T) {
	var seenAuthOnTarget string
	var seenCookieOnTarget string
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthOnTarget = r.Header.Get("Authorization")
		seenCookieOnTarget = r.Header.Get("Cookie")
		w.Header().Set("X-Custom-Header", "Val")
		if r.Method == "PUT" {
			var buf [64]byte
			n, _ := r.Body.Read(buf[:])
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("uploaded:" + string(buf[:n])))
			return
		}
		_, _ = w.Write([]byte("ok-target"))
	}))
	defer targetSrv.Close()

	var redirSrv *httptest.Server
	redirSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cross" {
			http.Redirect(w, r, targetSrv.URL+"/landing", http.StatusFound)
			return
		}
		if r.URL.Path == "/same" {
			http.Redirect(w, r, redirSrv.URL+"/same-dest", http.StatusFound)
			return
		}
		if r.URL.Path == "/same-dest" {
			w.Header().Set("X-Auth-Seen", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte("same-origin-ok"))
			return
		}
		if r.URL.Path == "/blocked-redir" {
			http.Redirect(w, r, "http://blocked.invalid/steal", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer redirSrv.Close()

	u1, _ := url.Parse(redirSrv.URL)
	u2, _ := url.Parse(targetSrv.URL)

	// Without PermNetwork -> denied.
	rtNoNet, _, _, _, _ := newTestCapabilityRuntime(t, []Permission{PermSystemFS}, nil, nil)
	mustFailDispatch(t, rtNoNet, "host.http.request", map[string]any{"url": redirSrv.URL}, -32001)

	rt, _, dataDir, _, _ := newTestCapabilityRuntime(t, []Permission{PermNetwork, PermSystemFS}, []string{u1.Host, u2.Host}, nil)

	// Domain not in NetworkDomains -> denied; file:// scheme -> denied.
	mustFailDispatch(t, rt, "host.http.request", map[string]any{"url": "https://evil.example.org/data"}, -32001)
	mustFailDispatch(t, rt, "host.http.request", map[string]any{"url": "file:///etc/passwd"}, -32602)
	// Redirect to non-allowlisted domain -> fails.
	mustFailDispatch(t, rt, "host.http.request", map[string]any{"url": redirSrv.URL + "/blocked-redir"}, -32005)

	// Cross-origin redirect strips Authorization and Cookie.
	seenAuthOnTarget = "UNSET"
	seenCookieOnTarget = "UNSET"
	crossRes := mustDispatch(t, rt, "host.http.request", map[string]any{
		"url": redirSrv.URL + "/cross",
		"headers": map[string]string{
			"Authorization": "Bearer secret",
			"Cookie":        "sid=abc",
		},
	})
	if seenAuthOnTarget != "" || seenCookieOnTarget != "" {
		t.Fatalf("cross-origin redirect leaked credentials: auth=%q cookie=%q", seenAuthOnTarget, seenCookieOnTarget)
	}
	hdrs := crossRes["headers"].(map[string]any)
	if hdrs["x-custom-header"] != "Val" {
		t.Fatalf("expected lowercase header x-custom-header=Val, got %v", hdrs)
	}

	// Same-origin redirect preserves Authorization.
	sameRes := mustDispatch(t, rt, "host.http.request", map[string]any{
		"url":     redirSrv.URL + "/same",
		"headers": map[string]string{"Authorization": "Bearer keepme"},
	})
	if sameRes["headers"].(map[string]any)["x-auth-seen"] != "Bearer keepme" {
		t.Fatalf("same-origin redirect lost Authorization: %v", sameRes["headers"])
	}

	// body_file_path streaming upload + save_to_path atomic download.
	uploadSrc := filepath.Join(dataDir, "upload.txt")
	if err := os.WriteFile(uploadSrc, []byte("file-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveDst := filepath.Join(dataDir, "out", "resp.txt")
	putRes := mustDispatch(t, rt, "host.http.request", map[string]any{
		"url":            targetSrv.URL + "/upload",
		"method":         "PUT",
		"body_file_path": uploadSrc,
		"save_to_path":   saveDst,
		"mkdir_parents":  true,
	})
	if int(putRes["status"].(float64)) != http.StatusCreated {
		t.Fatalf("PUT status = %v", putRes["status"])
	}
	savedBytes, err := os.ReadFile(saveDst)
	if err != nil || string(savedBytes) != "uploaded:file-bytes" {
		t.Fatalf("saved file = %q, err=%v", string(savedBytes), err)
	}

	// Wildcard NetworkDomains check.
	if !matchNetworkDomains(&url.URL{Scheme: "https", Host: "api.sub.example.com"}, []string{"*.example.com"}) {
		t.Fatal("expected *.example.com to match api.sub.example.com")
	}
	if matchNetworkDomains(&url.URL{Scheme: "https", Host: "example.com"}, []string{"*.example.com"}) {
		t.Fatal("did not expect *.example.com to match bare example.com")
	}
}

func TestHostExecRun(t *testing.T) {
	t.Setenv("BLOCK_TEST_SECRET", "top-secret-value")

	// Missing PermSystemExec -> denied.
	rtNoExec, _, _, _, _ := newTestCapabilityRuntime(t, []Permission{PermSystemFS}, nil, []string{"pwd"})
	mustFailDispatch(t, rtNoExec, "host.exec.run", map[string]any{"program": "pwd"}, -32001)

	rt, tmp, dataDir, _, _ := newTestCapabilityRuntime(t, []Permission{PermSystemExec}, nil, []string{"env", "pwd", "sleep", "cat"})

	// Program not in ExecAllowlist or shell -> denied.
	mustFailDispatch(t, rt, "host.exec.run", map[string]any{"program": "ls"}, -32001)
	mustFailDispatch(t, rt, "host.exec.run", map[string]any{"program": "sh"}, -32001)
	mustFailDispatch(t, rt, "host.exec.run", map[string]any{"program": "/bin/pwd"}, -32001)

	// cwd outside authorized scope -> denied.
	outsideDir := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustFailDispatch(t, rt, "host.exec.run", map[string]any{"program": "pwd", "cwd": outsideDir}, -32001)

	// cwd defaults to DataDir.
	pwdRes := mustDispatch(t, rt, "host.exec.run", map[string]any{"program": "pwd"})
	canonDataDir, _ := resolveCanonicalPath(dataDir)
	if strings.TrimSpace(pwdRes["stdout"].(string)) != canonDataDir {
		t.Fatalf("pwd = %q, want %q", pwdRes["stdout"], canonDataDir)
	}

	// Sanitized env: keeps PATH/HOME, injects GIT_TERMINAL_PROMPT=0, passes allowlisted env, strips parent secrets & dangerous env.
	envRes := mustDispatch(t, rt, "host.exec.run", map[string]any{
		"program": "env",
		"env": map[string]string{
			"GIT_AUTHOR_NAME":       "BlockBot",
			"GIT_TERMINAL_PROMPT":   "1",
			"DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib",
			"LD_PRELOAD":            "/tmp/evil.so",
		},
	})
	envOut := envRes["stdout"].(string)
	if strings.Contains(envOut, "BLOCK_TEST_SECRET") || strings.Contains(envOut, "DYLD_INSERT_LIBRARIES") || strings.Contains(envOut, "LD_PRELOAD") {
		t.Fatalf("env leaked or accepted forbidden variables:\n%s", envOut)
	}
	if !strings.Contains(envOut, "GIT_TERMINAL_PROMPT=0") || !strings.Contains(envOut, "GIT_AUTHOR_NAME=BlockBot") {
		t.Fatalf("env missing expected variables:\n%s", envOut)
	}

	// Stdin + stdout truncation.
	bigInput := strings.Repeat("X", 512)
	catRes := mustDispatch(t, rt, "host.exec.run", map[string]any{
		"program":          "cat",
		"stdin":            bigInput,
		"max_output_bytes": 64,
	})
	if len(catRes["stdout"].(string)) != 64 || catRes["stdout_truncated"] != true {
		t.Fatalf("expected 64-byte truncated stdout, got len=%d truncated=%v", len(catRes["stdout"].(string)), catRes["stdout_truncated"])
	}

	// Timeout kills process group promptly.
	sleepRes := mustDispatch(t, rt, "host.exec.run", map[string]any{
		"program":    "sleep",
		"args":       []string{"5"},
		"timeout_ms": 80,
	})
	if sleepRes["timed_out"] != true {
		t.Fatalf("expected timed_out=true, got %v", sleepRes)
	}
}

func TestEndToEndJSSandboxCallsPhase2ACapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello-from-http"))
	}))
	defer srv.Close()
	srvURL, _ := url.Parse(srv.URL)

	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugin")
	dataDir := filepath.Join(tmp, "apps-data", "e2e-js-caps")
	cacheDir := filepath.Join(tmp, "wasm-cache")
	notebookDir := filepath.Join(tmp, "notebook")
	for _, d := range []string{pluginDir, dataDir, notebookDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	jsMain := `
import { callHost, serve } from '/runtime/block-sdk.js';

serve((method, params) => {
  if (method === 'plugin.initialize') {
    return { ready: true };
  }
  if (method === 'plugin.render') {
    return { page: { title: 'E2E', cards: [] } };
  }
  if (method === 'plugin.action') {
    const dataFile = params.values.data_dir + '/sample.txt';
    callHost('host.fs.write', { path: dataFile, content: 'from-js', atomic: true });
    const readBack = callHost('host.fs.read', { path: dataFile });
    const digest = callHost('host.crypto.hash', { algorithm: 'sha256', file_path: dataFile });
    const httpRes = callHost('host.http.request', { url: params.values.url });
    const execRes = callHost('host.exec.run', { program: 'pwd' });
    return {
      ok: true,
      values: {
        fs: readBack.content,
        sha256: digest.hex,
        http: httpRes.body,
        pwd: execRes.stdout.trim(),
      },
    };
  }
  if (method === 'plugin.shutdown') {
    return { ok: true };
  }
  throw new Error('unsupported method: ' + method);
});
`
	if err := os.WriteFile(filepath.Join(pluginDir, "main.js"), []byte(jsMain), 0o644); err != nil {
		t.Fatal(err)
	}

	m := Manifest{
		App:            TargetApp,
		SchemaVersion:  CurrentSchemaVersion,
		ID:             "e2e-js-caps",
		Name:           LocalizedText{Zh: "端到端测试", En: "E2E Test"},
		Version:        "0.1.0",
		Author:         "tester",
		Summary:        LocalizedText{Zh: "端到端能力测试", En: "E2E capability test"},
		Entry:          EntrySpec{JS: "main.js"},
		Permissions:    []Permission{PermSystemFS, PermNetwork, PermSystemExec},
		NetworkDomains: []string{srvURL.Host},
		ExecAllowlist:  []string{"pwd"},
		Surfaces:       []Surface{SurfaceSettingsPage},
		Icon: DuoIconSpec{
			ViewBox:     "0 0 24 24",
			StrokePaths: []string{"M4 4H20V20H4Z"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := StartRuntime(ctx, m, HostConfig{
		PluginDir:    pluginDir,
		DataDir:      dataDir,
		CacheDir:     cacheDir,
		NotebookRoot: notebookDir,
		HostVersion:  "1.0.696",
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	defer rt.Shutdown(context.Background())

	if err := rt.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	res, err := rt.Action(ctx, SurfaceSettingsPage, "run_all", map[string]any{
		"data_dir": dataDir,
		"url":      srv.URL,
	})
	if err != nil {
		t.Fatalf("Action: %v", err)
	}
	wantSum := sha256.Sum256([]byte("from-js"))
	if res.Values["fs"] != "from-js" ||
		res.Values["sha256"] != hex.EncodeToString(wantSum[:]) ||
		res.Values["http"] != "hello-from-http" {
		t.Fatalf("unexpected e2e values: %+v", res.Values)
	}
	canonDataDir, _ := resolveCanonicalPath(dataDir)
	if res.Values["pwd"] != canonDataDir {
		t.Fatalf("pwd = %v, want %q", res.Values["pwd"], canonDataDir)
	}
}
