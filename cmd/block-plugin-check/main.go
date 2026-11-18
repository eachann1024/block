package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"block/internal/plugins"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: block-plugin-check <plugin-dir>")
		os.Exit(2)
	}
	pluginDir, err := filepath.Abs(os.Args[1])
	if err != nil {
		fail("resolve plugin path: %v", err)
	}

	m, err := plugins.LoadManifest(pluginDir)
	if err != nil {
		fail("manifest validation failed: %v", err)
	}
	fmt.Printf("[OK] manifest: app=%s id=%s (%s v%s) surfaces=%v permissions=%v\n",
		m.App, m.ID, m.Name.Zh, m.Version, m.Surfaces, m.Permissions)

	sandboxDir, err := os.MkdirTemp("", "block-plugin-check-*")
	if err != nil {
		fail("create sandbox: %v", err)
	}
	defer os.RemoveAll(sandboxDir)

	notebookDir := filepath.Join(sandboxDir, "notebook")
	dataDir := filepath.Join(sandboxDir, "apps-data", m.ID)
	if err := os.MkdirAll(filepath.Join(notebookDir, "notes"), 0o755); err != nil {
		fail("prepare sample notebook: %v", err)
	}
	_ = os.WriteFile(filepath.Join(notebookDir, "welcome.md"), []byte("# 欢迎使用 Block\n\n第一篇示例笔记。\n"), 0o644)
	_ = os.WriteFile(filepath.Join(notebookDir, "notes", "ideas.md"), []byte("# 架构手记\n\n模块化插件体系。\n"), 0o644)

	// Verify StoreManager installation flow into an isolated app store directory.
	storeMgr := plugins.NewStoreManager(filepath.Join(sandboxDir, "block-appdata"))
	installed, err := storeMgr.InstallFromDirectory(pluginDir)
	if err != nil {
		fail("store install simulation failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rt, err := plugins.StartRuntime(ctx, installed.Manifest, plugins.HostConfig{
		PluginDir:    installed.InstallDir,
		DataDir:      dataDir,
		NotebookRoot: notebookDir,
		Locale:       "zh",
		HostVersion:  "1.0.301",
	})
	if err != nil {
		fail("start plugin runtime: %v", err)
	}
	defer rt.Shutdown(context.Background())

	if err := rt.Initialize(ctx); err != nil {
		fail("plugin.initialize: %v", err)
	}
	fmt.Println("[OK] lifecycle: plugin.initialize ready=true")

	for _, surf := range m.Surfaces {
		page, err := rt.Render(ctx, surf)
		if err != nil {
			fail("plugin.render(%s): %v", surf, err)
		}
		values := page.ExtractDefaultValues()
		actions := page.CollectActionIDs()
		fmt.Printf("[OK] surface %s: title=%q cards=%d controls=%d actions=%v\n",
			surf, page.Title, len(page.Cards), len(values), actions)

		for _, actID := range actions {
			res, err := rt.Action(ctx, surf, actID, values)
			if err != nil {
				fail("plugin.action(%s, %s): %v", surf, actID, err)
			}
			if res.Values != nil {
				for k, v := range res.Values {
					values[k] = v
				}
			}
			fmt.Printf("[OK] action %s/%s -> status=%q updatedPage=%v\n",
				surf, actID, res.Status, res.Page != nil)
		}
	}

	if m.HasPermission(plugins.PermEventNoteSaved) {
		if err := rt.NotifyNoteSaved(ctx, "welcome.md"); err != nil {
			fail("plugin.event(note.saved): %v", err)
		}
		fmt.Println("[OK] event: note.saved handled=true")
	}

	if err := rt.Shutdown(ctx); err != nil {
		fail("plugin.shutdown: %v", err)
	}
	fmt.Printf("[PASS] Plugin %q passed all Block App Store checks.\n", m.ID)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[FAIL] "+format+"\n", args...)
	os.Exit(1)
}
