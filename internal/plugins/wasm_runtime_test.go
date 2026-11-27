package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWasmJSPluginLifecycleAndStorageWithoutPython(t *testing.T) {
	t.Setenv("PATH", "")

	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugin")
	dataDir := filepath.Join(tmp, "apps-data", "js-lifecycle-test")
	cacheDir := filepath.Join(tmp, "wasm-cache")
	notebookDir := filepath.Join(tmp, "notebook")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(notebookDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsMain := `
import { callHost, serve } from '/runtime/block-sdk.js';

serve((method, params) => {
  if (method === 'plugin.initialize') {
    const prev = callHost('host.storage.get', { key: 'boot_count' });
    const nextCount = (prev.found && typeof prev.value === 'number' ? prev.value : 0) + 1;
    callHost('host.storage.set', { key: 'boot_count', value: nextCount });
    callHost('host.storage.set', { key: 'last_locale', value: params.locale || 'zh' });
    return { ready: true };
  }
  if (method === 'plugin.render') {
    const boot = callHost('host.storage.get', { key: 'boot_count' });
    return {
      page: {
        title: 'JS Plugin',
        status: 'boot=' + String(boot.value),
        cards: [
          {
            id: 'main',
            title: 'Main',
            items: [
              { kind: 'kv', rows: [{ label: 'Count', value: String(boot.value) }] },
            ],
          },
        ],
      },
    };
  }
  if (method === 'plugin.action') {
    callHost('host.storage.set', { key: 'last_action', value: params.action_id });
    const stored = callHost('host.storage.get', { key: 'last_action' });
    return {
      ok: true,
      status: 'action:' + String(stored.value),
      values: { echo: stored.value },
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
		App:           TargetApp,
		SchemaVersion: CurrentSchemaVersion,
		ID:            "js-lifecycle-test",
		Name:          LocalizedText{Zh: "JS 测试插件", En: "JS Test Plugin"},
		Version:       "0.1.0",
		Author:        "tester",
		Summary:       LocalizedText{Zh: "无 Python 测试", En: "No Python test"},
		Entry:         EntrySpec{JS: "main.js"},
		Permissions:   []Permission{PermStorage},
		Surfaces:      []Surface{SurfaceSettingsPage},
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
		Locale:       "en",
		HostVersion:  "1.0.696",
	})
	if err != nil {
		t.Fatalf("StartRuntime (PATH=''): %v", err)
	}

	if err := rt.Initialize(ctx); err != nil {
		_ = rt.Shutdown(context.Background())
		t.Fatalf("Initialize: %v (stderr=%s)", err, rt.Stderr())
	}

	page, err := rt.Render(ctx, SurfaceSettingsPage)
	if err != nil {
		_ = rt.Shutdown(context.Background())
		t.Fatalf("Render: %v", err)
	}
	if page.Status != "boot=1" {
		t.Fatalf("page.Status = %q, want %q", page.Status, "boot=1")
	}

	res, err := rt.Action(ctx, SurfaceSettingsPage, "save_note", map[string]any{"k": "v"})
	if err != nil {
		_ = rt.Shutdown(context.Background())
		t.Fatalf("Action: %v", err)
	}
	if res.Status != "action:save_note" {
		t.Fatalf("Action status = %q, want %q", res.Status, "action:save_note")
	}

	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Second boot must read persisted host.storage state across restarts.
	rt2, err := StartRuntime(ctx, m, HostConfig{
		PluginDir:    pluginDir,
		DataDir:      dataDir,
		CacheDir:     cacheDir,
		NotebookRoot: notebookDir,
		Locale:       "en",
		HostVersion:  "1.0.696",
	})
	if err != nil {
		t.Fatalf("StartRuntime #2: %v", err)
	}
	defer rt2.Shutdown(context.Background())

	if err := rt2.Initialize(ctx); err != nil {
		t.Fatalf("Initialize #2: %v", err)
	}
	page2, err := rt2.Render(ctx, SurfaceSettingsPage)
	if err != nil {
		t.Fatalf("Render #2: %v", err)
	}
	if page2.Status != "boot=2" {
		t.Fatalf("page2.Status = %q, want %q", page2.Status, "boot=2")
	}
}
