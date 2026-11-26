package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	name string
	dir  bool
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func makeFakePythonEnv(goos string, envMap map[string]string, files map[string]bool, working map[string]bool, hasCLT bool) (pythonEnv, *[]string) {
	var verified []string
	env := pythonEnv{
		goos:   goos,
		getenv: func(k string) string { return envMap[k] },
		homeDir: func() (string, error) {
			if h := envMap["HOME"]; h != "" {
				return h, nil
			}
			return envMap["USERPROFILE"], nil
		},
		stat: func(p string) (os.FileInfo, error) {
			if files[p] {
				return fakeFileInfo{name: filepath.Base(p), mode: 0o755}, nil
			}
			return nil, os.ErrNotExist
		},
		glob: func(pattern string) ([]string, error) {
			seen := map[string]bool{}
			var matches []string
			prefix, suffix, hasStar := strings.Cut(pattern, "*")
			candidates := make(map[string]bool, len(files))
			for path := range files {
				candidates[path] = true
				for dir := path; ; {
					idx := strings.LastIndexAny(dir, `/\`)
					if idx <= 0 {
						break
					}
					dir = dir[:idx]
					candidates[dir] = true
				}
			}
			for path := range candidates {
				if hasStar {
					if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) {
						rest := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
						if !strings.ContainsAny(rest, `/\`) && !seen[path] {
							seen[path] = true
							matches = append(matches, path)
						}
					}
				} else if path == pattern && !seen[path] {
					seen[path] = true
					matches = append(matches, path)
				}
			}
			return matches, nil
		},
		verify: func(p string) bool {
			verified = append(verified, p)
			return working[p]
		},
		hasCLT: func() bool { return hasCLT },
	}
	return env, &verified
}

func TestResolvePython3MacOSPriorityAndSkipCLTStub(t *testing.T) {
	files := map[string]bool{
		"/usr/bin/python3":          true,
		"/opt/homebrew/bin/python3": true,
	}
	working := map[string]bool{
		"/opt/homebrew/bin/python3": true,
	}
	env, verified := makeFakePythonEnv(
		"darwin",
		map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/Users/test"},
		files,
		working,
		false,
	)

	got, err := env.resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/opt/homebrew/bin/python3" {
		t.Fatalf("got %q, want /opt/homebrew/bin/python3", got)
	}
	if slices.Contains(*verified, "/usr/bin/python3") {
		t.Fatalf("should never verify /usr/bin/python3 when CLT is absent or Homebrew exists, verified=%v", *verified)
	}
}

func TestResolvePython3MacOSFrameworkAndPyenvAndCLTFallback(t *testing.T) {
	// Python.framework versioned fallback when Homebrew is absent.
	fwFiles := map[string]bool{
		"/usr/bin/python3": true,
		"/Library/Frameworks/Python.framework/Versions/3.11/bin/python3": true,
		"/Library/Frameworks/Python.framework/Versions/3.12/bin/python3": true,
	}
	fwWorking := map[string]bool{
		"/Library/Frameworks/Python.framework/Versions/3.11/bin/python3": true,
		"/Library/Frameworks/Python.framework/Versions/3.12/bin/python3": true,
	}
	env, verified := makeFakePythonEnv("darwin", map[string]string{"HOME": "/Users/test"}, fwFiles, fwWorking, false)
	got, err := env.resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/Library/Frameworks/Python.framework/Versions/3.12/bin/python3" {
		t.Fatalf("got %q, want 3.12 framework python3", got)
	}
	if slices.Contains(*verified, "/usr/bin/python3") {
		t.Fatalf("should skip /usr/bin/python3 when CLT is not installed")
	}

	// pyenv shim fallback.
	pyenvFiles := map[string]bool{
		"/Users/test/.pyenv/shims/python3": true,
	}
	pyenvEnv, _ := makeFakePythonEnv("darwin", map[string]string{"HOME": "/Users/test"}, pyenvFiles, pyenvFiles, false)
	got, err = pyenvEnv.resolve()
	if err != nil || got != "/Users/test/.pyenv/shims/python3" {
		t.Fatalf("got (%q, %v), want pyenv shim", got, err)
	}

	// Only /usr/bin/python3 present, CLT NOT installed -> must skip and error cleanly.
	cltMissingEnv, cltMissingVerified := makeFakePythonEnv(
		"darwin",
		map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/Users/test"},
		map[string]bool{"/usr/bin/python3": true},
		map[string]bool{"/usr/bin/python3": true},
		false,
	)
	if _, err := cltMissingEnv.resolve(); err == nil || !strings.Contains(err.Error(), "python3 not found") {
		t.Fatalf("expected clear error when CLT missing, got %v", err)
	}
	if len(*cltMissingVerified) != 0 {
		t.Fatalf("must not invoke /usr/bin/python3 without CLT, verified=%v", *cltMissingVerified)
	}

	// Only /usr/bin/python3 present, CLT installed -> fallback succeeds.
	cltOKEnv, _ := makeFakePythonEnv(
		"darwin",
		map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/Users/test"},
		map[string]bool{"/usr/bin/python3": true},
		map[string]bool{"/usr/bin/python3": true},
		true,
	)
	if got, err := cltOKEnv.resolve(); err != nil || got != "/usr/bin/python3" {
		t.Fatalf("got (%q, %v), want /usr/bin/python3 when CLT installed", got, err)
	}
}

func TestResolvePython3WindowsSkipsWindowsAppsAndFallbacks(t *testing.T) {
	winAppsStub := `C:\Users\u\AppData\Local\Microsoft\WindowsApps\python3.exe`
	winAppsPy := `C:\Users\u\AppData\Local\Microsoft\WindowsApps\python.exe`
	real312 := `C:\Users\u\AppData\Local\Programs\Python\Python312\python.exe`
	real311 := `C:\Users\u\AppData\Local\Programs\Python\Python311\python.exe`
	files := map[string]bool{
		winAppsStub: true,
		winAppsPy:   true,
		real311:     true,
		real312:     true,
	}
	working := map[string]bool{
		winAppsStub: true,
		winAppsPy:   true,
		real311:     true,
		real312:     true,
	}
	env, verified := makeFakePythonEnv(
		"windows",
		map[string]string{
			"PATH":         `C:\Users\u\AppData\Local\Microsoft\WindowsApps;C:\Windows\System32`,
			"LOCALAPPDATA": `C:\Users\u\AppData\Local`,
			"USERPROFILE":  `C:\Users\u`,
			"SystemDrive":  `C:`,
		},
		files,
		working,
		false,
	)

	got, err := env.resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != real312 {
		t.Fatalf("got %q, want %q", got, real312)
	}
	for _, v := range *verified {
		if isWindowsAppsStub(v) {
			t.Fatalf("should never verify WindowsApps stub: %q", v)
		}
	}

	// Fallback to py.exe when Python3* dirs absent.
	launcherPath := `C:\Windows\py.exe`
	launcherEnv, _ := makeFakePythonEnv(
		"windows",
		map[string]string{"PATH": `C:\Users\u\AppData\Local\Microsoft\WindowsApps;C:\Windows`, "SystemRoot": `C:\Windows`},
		map[string]bool{winAppsStub: true, launcherPath: true},
		map[string]bool{launcherPath: true},
		false,
	)
	if got, err := launcherEnv.resolve(); err != nil || got != launcherPath {
		t.Fatalf("got (%q, %v), want %q", got, err, launcherPath)
	}

	// Unavailable on Windows -> clear error.
	emptyEnv, _ := makeFakePythonEnv(
		"windows",
		map[string]string{"PATH": `C:\Users\u\AppData\Local\Microsoft\WindowsApps`},
		map[string]bool{winAppsStub: true},
		map[string]bool{},
		false,
	)
	if _, err := emptyEnv.resolve(); err == nil || !strings.Contains(err.Error(), "Microsoft Store") {
		t.Fatalf("expected clear Windows error hint, got %v", err)
	}
}

func TestResolvePython3LinuxFallbackAndCaching(t *testing.T) {
	files := map[string]bool{
		"/usr/local/bin/python3": true,
	}
	working := map[string]bool{
		"/usr/local/bin/python3": true,
	}
	env, verified := makeFakePythonEnv("linux", map[string]string{"HOME": "/home/u"}, files, working, false)

	var cache pythonCache
	got1, err := resolvePython3Cached(&cache, env)
	if err != nil || got1 != "/usr/local/bin/python3" {
		t.Fatalf("first resolve got (%q, %v)", got1, err)
	}
	got2, err := resolvePython3Cached(&cache, env)
	if err != nil || got2 != "/usr/local/bin/python3" {
		t.Fatalf("second resolve got (%q, %v)", got2, err)
	}
	if len(*verified) != 1 {
		t.Fatalf("expected cached verify count=1, got %d (%v)", len(*verified), *verified)
	}

	// Delete cached file -> invalidates cache and re-probes cleanly.
	delete(files, "/usr/local/bin/python3")
	if _, err := resolvePython3Cached(&cache, env); err == nil {
		t.Fatal("expected error after cached interpreter disappeared")
	}

	// Failures are not cached: adding /usr/bin/python3 later succeeds.
	files["/usr/bin/python3"] = true
	working["/usr/bin/python3"] = true
	got3, err := resolvePython3Cached(&cache, env)
	if err != nil || got3 != "/usr/bin/python3" {
		t.Fatalf("expected recovery after failure, got (%q, %v)", got3, err)
	}
}

func TestPrependEnvPathDirForOS(t *testing.T) {
	if got := prependEnvPathDirForOS("darwin", "/usr/bin:/bin", "/opt/homebrew/bin"); got != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Fatalf("darwin prepend got %q", got)
	}
	if got := prependEnvPathDirForOS("darwin", "/opt/homebrew/bin/:/usr/bin", "/opt/homebrew/bin"); got != "/opt/homebrew/bin/:/usr/bin" {
		t.Fatalf("darwin duplicate check failed, got %q", got)
	}
	if got := prependEnvPathDirForOS("windows", `C:\Windows\System32`, `C:\Python312`); got != `C:\Python312;C:\Windows\System32` {
		t.Fatalf("windows prepend got %q", got)
	}
	if got := prependEnvPathDirForOS("windows", `c:\python312\;C:\Windows`, `C:\Python312`); got != `c:\python312\;C:\Windows` {
		t.Fatalf("windows case-insensitive duplicate check failed, got %q", got)
	}
}

func TestRuntimeCrashAttachesTrimmedStderrAndStartPrependsPath(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))

	binDir := filepath.Join(t.TempDir(), "custom-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginDir := t.TempDir()
	dataDir := t.TempDir()

	// Bundled script that exits after writing to stderr.
	scriptPath := filepath.Join(pluginDir, "crash.sh")
	script := "#!/bin/sh\necho '  boom: missing dependency  ' >&2\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-crash",
		Name:          LocalizedText{Zh: "测试", En: "Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"./crash.sh"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	rt, err := StartRuntime(context.Background(), m, HostConfig{
		PluginDir: pluginDir,
		DataDir:   dataDir,
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	defer rt.abort()

	// Verify pluginDir was prepended to child PATH since ./crash.sh lives in pluginDir.
	hasDir := false
	for _, env := range rt.cmd.Env {
		if strings.HasPrefix(env, "PATH=") {
			dirs := filepath.SplitList(strings.TrimPrefix(env, "PATH="))
			if len(dirs) > 0 && dirs[0] == pluginDir {
				hasDir = true
			}
		}
	}
	if !hasDir {
		t.Fatalf("expected child PATH to prepend %q, env=%v", pluginDir, rt.cmd.Env)
	}

	err = rt.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected Initialize to fail when plugin crashes")
	}
	if !strings.Contains(err.Error(), "boom: missing dependency") {
		t.Fatalf("expected trimmed stderr in error, got %v", err)
	}
}

func TestSupervisorInitFailureCleanupLoggingAndReplacingRecovery(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "block.log")
	t.Setenv("BLOCK_LOG_FILE", logPath)

	appData := t.TempDir()
	nbDir := t.TempDir()
	sup := NewSupervisor(SupervisorConfig{
		AppDataDir:      appData,
		GetNotebookRoot: func() string { return nbDir },
		GetLocale:       func() string { return "zh" },
	})

	// Install a plugin whose process crashes before responding to plugin.initialize.
	src := t.TempDir()
	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-tarot-fail",
		Name:          LocalizedText{Zh: "失败测试", En: "Fail Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"./fail.sh"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "fail.sh"), []byte("#!/bin/sh\necho 'traceback: init failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.store.InstallFromDirectory(src); err != nil {
		t.Fatal(err)
	}
	if err := sup.store.SetEnabled(m.ID, true); err != nil {
		t.Fatal(err)
	}
	installed, _ := sup.store.ListInstalled()
	for i := range installed {
		installed[i].Enabled = sup.store.IsEnabled(installed[i].Manifest.ID)
	}
	sup.mu.Lock()
	sup.installed = installed
	sup.mu.Unlock()

	// 1. ensureRuntimeWithLocale must clean up s.runtimes[pluginID] on Initialize failure.
	_, _, err := sup.ensureRuntimeWithLocale(context.Background(), m.ID, nbDir)
	if err == nil {
		t.Fatal("expected ensureRuntimeWithLocale to fail")
	}
	sup.mu.RLock()
	rtLeft := sup.runtimes[m.ID]
	sup.mu.RUnlock()
	if rtLeft != nil {
		t.Fatalf("expected s.runtimes[%q] to be cleaned up after Initialize failure", m.ID)
	}

	// 2. loadAndRenderSurfaceOnce must log failure to block.log.
	key := viewKey{pluginID: m.ID, notebookRoot: nbDir, surface: SurfaceSettingsPage}
	sup.mu.Lock()
	sup.views[key] = &PluginViewState{Loading: true, Values: map[string]any{}, initialLoadStarted: true}
	sup.mu.Unlock()
	sup.loadAndRenderSurfaceOnce(m.ID, nbDir, SurfaceSettingsPage, false)

	sup.mu.RLock()
	vs := sup.views[key]
	errMsg := vs.Error
	sup.mu.RUnlock()
	if errMsg == "" {
		t.Fatal("expected vs.Error to be populated")
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read block.log: %v", err)
	}
	if !strings.Contains(string(logBytes), "plugin surface load failed: id=test-tarot-fail") {
		t.Fatalf("expected surface load failure in block.log, got:\n%s", string(logBytes))
	}

	// 3. Concurrent replacingID must never stick view permanently in Error; finishInstallAsync failure recovers it.
	sup.mu.Lock()
	sup.views[key] = &PluginViewState{Loading: true, Values: map[string]any{}, initialLoadStarted: true}
	sup.replacingID = m.ID
	sup.mu.Unlock()

	snap := sup.EnsureSurfaceView(m.ID, SurfaceSettingsPage)
	if snap.Error != "" || !snap.Loading {
		t.Fatalf("expected Loading snapshot without error during replace, got %+v", snap)
	}
	// Even if a background load ran during replacingID, it must not set vs.Error.
	sup.loadAndRenderSurfaceOnce(m.ID, nbDir, SurfaceSettingsPage, false)
	sup.mu.RLock()
	if sup.views[key].Error != "" {
		t.Fatalf("unexpected Error set during replacingID: %q", sup.views[key].Error)
	}
	sup.mu.RUnlock()

	// finishInstallAsync fails -> clears replacingID and resets/reloads the waiting view.
	sup.finishInstallAsync(nil, errors.New("network error"), nil, true)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sup.mu.RLock()
		cur := sup.views[key]
		curErr := cur.Error
		loading := cur.Loading
		replacing := sup.replacingID
		sup.mu.RUnlock()
		if replacing == "" && !loading && curErr != "" && !strings.Contains(curErr, "being replaced") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	sup.mu.RLock()
	finalErr := sup.views[key].Error
	sup.mu.RUnlock()
	if strings.Contains(finalErr, "being replaced") {
		t.Fatalf("view got stuck on replacing error: %q", finalErr)
	}
}

type tarotUnavailableHost struct {
	err error
}

func (h tarotUnavailableHost) Toast(ToastRequest) ToastResult                         { return ToastResult{} }
func (h tarotUnavailableHost) OpenPluginSettings(string)                              {}
func (h tarotUnavailableHost) OpenDialog(string, string, func() error) (string, error) { return "", nil }
func (h tarotUnavailableHost) ActiveDialogToken(string, string) string                { return "" }
func (h tarotUnavailableHost) CloseDialog(string, string)                             {}
func (h tarotUnavailableHost) OpenPanel(string, string)                               {}
func (h tarotUnavailableHost) ClosePanel(string, string)                              {}
func (h tarotUnavailableHost) TogglePanel(string, string) (bool, error)               { return false, nil }
func (h tarotUnavailableHost) OpenNote(string, string, os.FileInfo, []byte, func() error) error {
	return nil
}
func (h tarotUnavailableHost) EditorSelection() (EditorSelection, error) {
	return EditorSelection{}, nil
}
func (h tarotUnavailableHost) EditorInsert(string, string, string, string, string, func() error) error {
	return nil
}
func (h tarotUnavailableHost) TarotState() (TarotState, error) {
	return TarotState{}, h.err
}
func (h tarotUnavailableHost) SetTarotMode(*bool, bool, string) (TarotState, error) {
	return TarotState{}, h.err
}

func TestHostUISetTarotUnavailableReturns32005(t *testing.T) {
	m := Manifest{
		ID:          "block-tarot",
		Permissions: []Permission{PermUISettings},
	}
	registerActive := func(sup *Supervisor) *Runtime {
		rt := &Runtime{manifest: m}
		sup.mu.Lock()
		sup.installed = []InstalledApp{{Manifest: m, Enabled: true}}
		sup.runtimes[m.ID] = rt
		sup.mu.Unlock()
		return rt
	}

	sup := NewSupervisor(SupervisorConfig{
		AppDataDir: t.TempDir(),
		UI:         tarotUnavailableHost{err: errors.New("host is shutting down")},
	})
	rt := registerActive(sup)
	_, rpcErr, handled := sup.dispatchExtensionHostMethod(rt, "host.ui.set_tarot", json.RawMessage(`{"toggle":true}`), nil)
	if !handled || rpcErr == nil {
		t.Fatalf("expected handled rpcError, got handled=%v err=%v", handled, rpcErr)
	}
	if rpcErr.Code != -32005 {
		t.Fatalf("got rpc error code %d, want -32005", rpcErr.Code)
	}

	// Also verify nil UIHost returns -32005.
	supNil := NewSupervisor(SupervisorConfig{AppDataDir: t.TempDir()})
	rtNil := registerActive(supNil)
	_, rpcErrNil, handledNil := supNil.dispatchExtensionHostMethod(rtNil, "host.ui.set_tarot", json.RawMessage(`{"toggle":true}`), nil)
	if !handledNil || rpcErrNil == nil || rpcErrNil.Code != -32005 {
		t.Fatalf("nil UIHost got (%v, %v), want -32005", handledNil, rpcErrNil)
	}
}

func TestSplitEnvPathAndIsRunnableFileRejectRelativeAndNonRegular(t *testing.T) {
	if got := splitEnvPath("darwin", ".:./bin::relative/dir:/opt/homebrew/bin"); !slices.Equal(got, []string{"/opt/homebrew/bin"}) {
		t.Fatalf("darwin splitEnvPath got %v, want [/opt/homebrew/bin]", got)
	}
	if got := splitEnvPath("windows", `.;.\bin;C:rel;\rootrel;;"C:\Python312"`); !slices.Equal(got, []string{`C:\Python312`}) {
		t.Fatalf("windows splitEnvPath got %v, want [C:\\Python312]", got)
	}

	// Relative PATH candidates must never be verified or returned.
	relFiles := map[string]bool{
		"./bin/python3":             true,
		"bin/python3":               true,
		"/opt/homebrew/bin/python3": true,
	}
	env, verified := makeFakePythonEnv(
		"darwin",
		map[string]string{"PATH": "./bin:.:bin:/opt/homebrew/bin", "HOME": "/Users/test"},
		relFiles,
		relFiles,
		false,
	)
	got, err := env.resolve()
	if err != nil || got != "/opt/homebrew/bin/python3" {
		t.Fatalf("got (%q, %v), want /opt/homebrew/bin/python3", got, err)
	}
	for _, v := range *verified {
		if !strings.HasPrefix(v, "/") {
			t.Fatalf("verified non-absolute path: %q", v)
		}
	}

	// Non-regular files (FIFO/dir/symlink-mode) and relative paths must fail isRunnableFile.
	fifoEnv := pythonEnv{
		goos: "darwin",
		stat: func(p string) (os.FileInfo, error) {
			switch p {
			case "/tmp/fifo-python3":
				return fakeFileInfo{name: "fifo-python3", mode: os.ModeNamedPipe | 0o755}, nil
			case "/tmp/dir-python3":
				return fakeFileInfo{name: "dir-python3", dir: true, mode: 0o755}, nil
			case "/tmp/ok-python3":
				return fakeFileInfo{name: "ok-python3", mode: 0o755}, nil
			default:
				return nil, os.ErrNotExist
			}
		},
	}
	if fifoEnv.isRunnableFile("./bin/python3") || fifoEnv.isRunnableFile(".") {
		t.Fatal("isRunnableFile must reject relative paths")
	}
	if fifoEnv.isRunnableFile("/tmp/fifo-python3") {
		t.Fatal("isRunnableFile must reject non-regular files (named pipe)")
	}
	if fifoEnv.isRunnableFile("/tmp/dir-python3") {
		t.Fatal("isRunnableFile must reject directories")
	}
	if !fifoEnv.isRunnableFile("/tmp/ok-python3") {
		t.Fatal("isRunnableFile should accept regular executable")
	}
}

func TestSortedGlobDescNumericVersionOrdering(t *testing.T) {
	fwEnv, _ := makeFakePythonEnv(
		"darwin",
		map[string]string{"HOME": "/Users/test"},
		map[string]bool{
			"/Library/Frameworks/Python.framework/Versions/3.9/bin/python3":  true,
			"/Library/Frameworks/Python.framework/Versions/3.10/bin/python3": true,
			"/Library/Frameworks/Python.framework/Versions/3.12/bin/python3": true,
		},
		map[string]bool{
			"/Library/Frameworks/Python.framework/Versions/3.9/bin/python3":  true,
			"/Library/Frameworks/Python.framework/Versions/3.10/bin/python3": true,
			"/Library/Frameworks/Python.framework/Versions/3.12/bin/python3": true,
		},
		false,
	)
	if got, err := fwEnv.resolve(); err != nil || got != "/Library/Frameworks/Python.framework/Versions/3.12/bin/python3" {
		t.Fatalf("darwin numeric version sort got (%q, %v), want 3.12", got, err)
	}

	winEnv, _ := makeFakePythonEnv(
		"windows",
		map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "USERPROFILE": `C:\Users\u`},
		map[string]bool{
			`C:\Users\u\AppData\Local\Programs\Python\Python39\python.exe`:  true,
			`C:\Users\u\AppData\Local\Programs\Python\Python310\python.exe`: true,
			`C:\Users\u\AppData\Local\Programs\Python\Python312\python.exe`: true,
		},
		map[string]bool{
			`C:\Users\u\AppData\Local\Programs\Python\Python39\python.exe`:  true,
			`C:\Users\u\AppData\Local\Programs\Python\Python310\python.exe`: true,
			`C:\Users\u\AppData\Local\Programs\Python\Python312\python.exe`: true,
		},
		false,
	)
	if got, err := winEnv.resolve(); err != nil || got != `C:\Users\u\AppData\Local\Programs\Python\Python312\python.exe` {
		t.Fatalf("windows numeric version sort got (%q, %v), want Python312", got, err)
	}
}

func TestRuntimeCallAndExitedErrorTruncateStderr(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))

	// 1. exitedError truncates >1KB stderr and retains tail.
	pluginDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"i=0; while [ $i -lt 200 ]; do printf 'AAAAAAAAAAAAAAAAAAAA ' >&2; i=$((i+1)); done\n" +
		"printf 'TAIL_MARKER\\n' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "crash.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-stderr-trunc",
		Name:          LocalizedText{Zh: "测试", En: "Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"./crash.sh"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	rt, err := StartRuntime(context.Background(), m, HostConfig{
		PluginDir: pluginDir,
		DataDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	defer rt.abort()

	initErr := rt.Initialize(context.Background())
	if initErr == nil {
		t.Fatal("expected Initialize error")
	}
	if len(initErr.Error()) > maxPluginLogStderr+256 {
		t.Fatalf("exitedError not truncated, len=%d", len(initErr.Error()))
	}
	if !strings.Contains(initErr.Error(), "…") || !strings.Contains(initErr.Error(), "TAIL_MARKER") {
		t.Fatalf("exitedError missing ellipsis or tail marker: %q", initErr.Error())
	}

	// 2. call() send and timed out paths truncate stderr from ringBuffer.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	_ = pw.Close() // closed pipe causes write error on send
	rb := newRingBuffer(maxStderrBytes)
	_, _ = rb.Write([]byte(strings.Repeat("B", 4096) + " SEND_TAIL"))
	sendRT := &Runtime{
		exited:    make(chan struct{}),
		reaped:    make(chan struct{}),
		manifest:  m,
		stdin:     pw,
		stdout:    pr,
		stderr:    rb,
		pending:   make(map[string]chan rpcEnvelope),
		writeGate: make(chan struct{}, 1),
		callGate:  make(chan struct{}, 1),
		hostCalls: make(chan struct{}, maxPluginHostRPCs),
	}
	close(sendRT.reaped)
	sendErr := sendRT.call(context.Background(), "plugin.render", map[string]any{}, nil)
	if sendErr == nil || !strings.Contains(sendErr.Error(), "send plugin.render:") {
		t.Fatalf("expected send error, got %v", sendErr)
	}
	if len(sendErr.Error()) > maxPluginLogStderr+256 || !strings.Contains(sendErr.Error(), "…") || !strings.Contains(sendErr.Error(), "SEND_TAIL") {
		t.Fatalf("send error stderr not truncated properly: len=%d err=%q", len(sendErr.Error()), sendErr.Error())
	}

	// 3. timed out path truncates stderr.
	pr2, pw2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr2.Close()
	defer pw2.Close()
	rb2 := newRingBuffer(maxStderrBytes)
	_, _ = rb2.Write([]byte(strings.Repeat("C", 4096) + " TIMEOUT_TAIL"))
	toRT := &Runtime{
		exited:    make(chan struct{}),
		reaped:    make(chan struct{}),
		manifest:  m,
		stdin:     pw2,
		stdout:    pr2,
		stderr:    rb2,
		pending:   make(map[string]chan rpcEnvelope),
		writeGate: make(chan struct{}, 1),
		callGate:  make(chan struct{}, 1),
		hostCalls: make(chan struct{}, maxPluginHostRPCs),
	}
	close(toRT.reaped)
	toCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	toErr := toRT.call(toCtx, "plugin.render", map[string]any{}, nil)
	if toErr == nil || !strings.Contains(toErr.Error(), "timed out:") {
		t.Fatalf("expected timed out error, got %v", toErr)
	}
	if len(toErr.Error()) > maxPluginLogStderr+256 || !strings.Contains(toErr.Error(), "…") || !strings.Contains(toErr.Error(), "TIMEOUT_TAIL") {
		t.Fatalf("timed out error stderr not truncated properly: len=%d err=%q", len(toErr.Error()), toErr.Error())
	}
}

func TestPython3CacheInvalidatedOnStartOrEarlyExitFailure(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))
	python3Cache.invalidate("")
	t.Cleanup(func() { python3Cache.invalidate("") })

	// 1. Direct invalidate semantics.
	var c pythonCache
	c.mu.Lock()
	c.path = "/tmp/py1"
	c.mu.Unlock()
	c.invalidate("/tmp/other")
	c.mu.Lock()
	if c.path != "/tmp/py1" {
		c.mu.Unlock()
		t.Fatalf("invalidate cleared unrelated path: %q", c.path)
	}
	c.mu.Unlock()
	c.invalidate("/tmp/py1")
	c.mu.Lock()
	if c.path != "" {
		c.mu.Unlock()
		t.Fatalf("invalidate did not clear matching path: %q", c.path)
	}
	c.mu.Unlock()

	// 2. StartRuntime early exit failure invalidates python3Cache.
	binDir := t.TempDir()
	fakePy := filepath.Join(binDir, "python3")
	// Passes `-c ...` verification probe, then exits 1 when launched as plugin runtime.
	script := "#!/bin/sh\nif [ \"$1\" = \"-c\" ]; then exit 0; fi\necho 'broken shim' >&2\nexit 1\n"
	if err := os.WriteFile(fakePy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-cache-invalidate",
		Name:          LocalizedText{Zh: "测试", En: "Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"python3", "main.py"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	rt, err := StartRuntime(context.Background(), m, HostConfig{
		PluginDir: t.TempDir(),
		DataDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	defer rt.abort()

	python3Cache.mu.Lock()
	cachedBefore := python3Cache.path
	python3Cache.mu.Unlock()
	if cachedBefore != fakePy {
		t.Fatalf("expected python3Cache=%q, got %q", fakePy, cachedBefore)
	}

	if err := rt.Initialize(context.Background()); err == nil {
		t.Fatal("expected Initialize to fail on broken shim")
	}
	<-rt.reaped

	python3Cache.mu.Lock()
	cachedAfter := python3Cache.path
	python3Cache.mu.Unlock()
	if cachedAfter != "" {
		t.Fatalf("expected python3Cache to be invalidated after early exit, got %q", cachedAfter)
	}
}

func TestEnsureRuntimeDoesNotHoldSupervisorLockDuringStartRuntime(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))
	python3Cache.invalidate("")
	t.Cleanup(func() { python3Cache.invalidate("") })

	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	probingFile := filepath.Join(workDir, "probing")
	releaseFile := filepath.Join(workDir, "release")
	fakePy := filepath.Join(binDir, "python3")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-c\" ]; then\n" +
		"  touch '" + probingFile + "'\n" +
		"  i=0; while [ ! -f '" + releaseFile + "' ] && [ $i -lt 100 ]; do sleep 0.02; i=$((i+1)); done\n" +
		"  exit 0\n" +
		"fi\n" +
		"sleep 5\n"
	if err := os.WriteFile(fakePy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	appData := t.TempDir()
	nbDir := t.TempDir()
	sup := NewSupervisor(SupervisorConfig{
		AppDataDir:      appData,
		GetNotebookRoot: func() string { return nbDir },
		GetLocale:       func() string { return "zh" },
	})
	src := t.TempDir()
	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-nonblocking-start",
		Name:          LocalizedText{Zh: "测试", En: "Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"python3", "main.py"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.store.InstallFromDirectory(src); err != nil {
		t.Fatal(err)
	}
	if err := sup.store.SetEnabled(m.ID, true); err != nil {
		t.Fatal(err)
	}
	installed, _ := sup.store.ListInstalled()
	for i := range installed {
		installed[i].Enabled = sup.store.IsEnabled(installed[i].Manifest.ID)
	}
	sup.mu.Lock()
	sup.installed = installed
	sup.mu.Unlock()

	type res struct {
		rt  *Runtime
		err error
	}
	done := make(chan res, 1)
	go func() {
		rt, _, err := sup.ensureRuntimeWithLocale(context.Background(), m.ID, nbDir)
		done <- res{rt: rt, err: err}
	}()

	// Wait until StartRuntime -> resolvePython3 -> defaultVerifyPython3 is actively running.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(probingFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(probingFile); err != nil {
		_ = os.WriteFile(releaseFile, []byte("1"), 0o600)
		t.Fatal("timed out waiting for StartRuntime probe")
	}

	// Supervisor lock must NOT be held while StartRuntime is probing/starting.
	locked := make(chan struct{})
	go func() {
		sup.mu.Lock()
		sup.stopping = true
		sup.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(500 * time.Millisecond):
		_ = os.WriteFile(releaseFile, []byte("1"), 0o600)
		t.Fatal("s.mu.Lock() blocked while StartRuntime was running")
	}

	// Release StartRuntime; post-start recheck must abort the started runtime and report changed.
	if err := os.WriteFile(releaseFile, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.rt != nil {
		r.rt.abort()
		t.Fatal("expected nil runtime when supervisor stopped during StartRuntime")
	}
	if r.err == nil || !strings.Contains(r.err.Error(), "plugin changed while starting") {
		t.Fatalf("expected 'plugin changed while starting', got %v", r.err)
	}
	sup.mu.RLock()
	_, registered := sup.runtimes[m.ID]
	sup.mu.RUnlock()
	if registered {
		t.Fatal("aborted runtime must not remain registered in s.runtimes")
	}
}

func TestRefreshPreviewForSeqIgnoresRenderErrorAfterInstallEpochChange(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))

	workDir := t.TempDir()
	renderStarted := filepath.Join(workDir, "render_started")
	pluginDir := filepath.Join(workDir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    *plugin.initialize*)\n" +
		"      id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p')\n" +
		"      printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ready\":true}}\\n' \"$id\"\n" +
		"      ;;\n" +
		"    *plugin.render*)\n" +
		"      touch '" + renderStarted + "'\n" +
		"      sleep 5\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-preview-epoch",
		Name:          LocalizedText{Zh: "测试", En: "Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{Command: []string{"./plugin.sh"}},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	nbDir := t.TempDir()
	sup := NewSupervisor(SupervisorConfig{
		AppDataDir:      t.TempDir(),
		GetNotebookRoot: func() string { return nbDir },
		GetLocale:       func() string { return "zh" },
	})
	rt, err := StartRuntime(context.Background(), m, HostConfig{
		PluginDir:    pluginDir,
		DataDir:      t.TempDir(),
		NotebookRoot: nbDir,
		Locale:       "zh",
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	defer rt.abort()
	if err := rt.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	key := viewKey{pluginID: m.ID, notebookRoot: nbDir, surface: SurfaceSettingsPage}
	vs := &PluginViewState{
		Values:         map[string]any{},
		editedControls: map[string]bool{"k": true},
		editSeq:        1,
	}
	sup.mu.Lock()
	sup.installed = []InstalledApp{{Manifest: m, Enabled: true, InstallDir: pluginDir}}
	sup.runtimes[m.ID] = rt
	sup.installEpoch[m.ID] = 1
	sup.views[key] = vs
	sup.mu.Unlock()

	done := make(chan struct{})
	go func() {
		sup.refreshPreviewForSeq(m.ID, nbDir, SurfaceSettingsPage, 1, 0)
		close(done)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(renderStarted); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(renderStarted); err != nil {
		t.Fatal("timed out waiting for plugin.render")
	}

	// Simulate plugin replacement completing/clearing replacingID while closing old rt.
	sup.mu.Lock()
	sup.installEpoch[m.ID]++
	delete(sup.runtimes, m.ID)
	sup.replacingID = ""
	sup.mu.Unlock()
	rt.abort()

	<-done

	sup.mu.RLock()
	gotErr := vs.Error
	pending := vs.pendingReload
	sup.mu.RUnlock()
	if gotErr != "" {
		t.Fatalf("expected empty vs.Error after installEpoch change, got %q", gotErr)
	}
	if !pending {
		t.Fatal("expected pendingReload=true after installEpoch change")
	}
}
