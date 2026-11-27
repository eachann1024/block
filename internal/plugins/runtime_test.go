package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

	// Install a plugin whose sandbox exits before responding to plugin.initialize.
	src := t.TempDir()
	m := Manifest{
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "test-surface-fail",
		Name:          LocalizedText{Zh: "失败测试", En: "Fail Test"},
		Version:       "1.0.0",
		Author:        "Test",
		Summary:       LocalizedText{Zh: "测试", En: "Test"},
		Entry:         EntrySpec{JS: "fail.js"},
		Surfaces:      []Surface{SurfaceSettingsPage},
		Icon:          AppStoreIconSpec(),
	}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "fail.js"), []byte(`import * as std from 'qjs:std';
serve(() => {
  log('init failed');
  std.exit(1);
});`), 0o600); err != nil {
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
	if !strings.Contains(string(logBytes), "plugin surface load failed: id=test-surface-fail") {
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

func TestRuntimeCallAndExitedErrorTruncateStderr(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))

	// 1. exitedError truncates >1KB stderr and retains tail.
	pluginDir := t.TempDir()
	script := `import * as std from 'qjs:std';
serve(() => {
  log('AAAAAAAAAAAAAAAAAAAA '.repeat(200) + 'TAIL_MARKER');
  std.exit(1);
});`
	if err := os.WriteFile(filepath.Join(pluginDir, "crash.js"), []byte(script), 0o600); err != nil {
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
		Entry:         EntrySpec{JS: "crash.js"},
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

func TestRefreshPreviewForSeqIgnoresRenderErrorAfterInstallEpochChange(t *testing.T) {
	t.Setenv("BLOCK_LOG_FILE", filepath.Join(t.TempDir(), "block.log"))

	pluginDir := t.TempDir()
	script := `import * as std from 'qjs:std';
serve((method) => {
  if (method === 'plugin.initialize') return { ready: true };
  if (method === 'plugin.render') {
    log('render_started');
    std.in.getline();
  }
});`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.js"), []byte(script), 0o600); err != nil {
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
		Entry:         EntrySpec{JS: "plugin.js"},
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
		if strings.Contains(rt.Stderr(), "render_started") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(rt.Stderr(), "render_started") {
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
