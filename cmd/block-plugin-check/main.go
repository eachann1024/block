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
	downloadsDir := filepath.Join(sandboxDir, "Downloads")
	dataDir := filepath.Join(sandboxDir, "apps-data", m.ID)
	if err := os.MkdirAll(filepath.Join(notebookDir, "notes"), 0o755); err != nil {
		fail("prepare sample notebook: %v", err)
	}
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		fail("prepare sample downloads dir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(notebookDir, "welcome.md"), []byte("# 欢迎使用 Block\n\n第一篇示例笔记，支持 [官网](https://blockmd.app) 外链测试。\n\n---\n\n## 第二部分\n\n> 模块化引用预览段落。\n"), 0o644)
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
		DownloadsDir: downloadsDir,
		NotebookRoot: notebookDir,
		ActiveNote:   "welcome.md",
		Locale:       "zh",
		HostVersion:  "1.0.379",
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
		if livePage, err := rt.RenderWithValues(ctx, surf, values); err != nil || livePage == nil {
			fail("plugin.render(%s, values): %v", surf, err)
		}
		permutations := 0
		for _, card := range page.Cards {
			for _, item := range card.Items {
				switch item.Kind {
				case plugins.KindSegmented, plugins.KindSelect:
					for _, opt := range item.Options {
						trial := make(map[string]any, len(values))
						for k, v := range values {
							trial[k] = v
						}
						trial[item.ID] = opt.Value
						if _, err := rt.RenderWithValues(ctx, surf, trial); err != nil {
							fail("plugin.render(%s) failed when %s=%q: %v", surf, item.ID, opt.Value, err)
						}
						permutations++
					}
				case plugins.KindSwitch:
					bVal, _ := item.Value.(bool)
					trial := make(map[string]any, len(values))
					for k, v := range values {
						trial[k] = v
					}
					trial[item.ID] = !bVal
					if _, err := rt.RenderWithValues(ctx, surf, trial); err != nil {
						fail("plugin.render(%s) failed when %s=%v: %v", surf, item.ID, !bVal, err)
					}
					permutations++
				}
			}
		}
		actions := page.CollectActionIDs()
		fmt.Printf("[OK] surface %s: title=%q cards=%d controls=%d permutations=%d actions=%v\n",
			surf, page.Title, len(page.Cards), len(values), permutations, actions)

		for _, actID := range actions {
			res, err := rt.Action(ctx, surf, actID, values)
			if err != nil {
				fail("plugin.action(%s, %s): %v", surf, actID, err)
			}
			if res.OpenPath != "" {
				if _, statErr := os.Stat(res.OpenPath); statErr != nil {
					fail("plugin.action(%s, %s) returned missing open_path %q: %v", surf, actID, res.OpenPath, statErr)
				}
			}
			if res.RevealPath != "" {
				if _, statErr := os.Stat(res.RevealPath); statErr != nil {
					fail("plugin.action(%s, %s) returned missing reveal_path %q: %v", surf, actID, res.RevealPath, statErr)
				}
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
		if err := rt.NotifyNoteSaved(ctx, notebookDir, "welcome.md"); err != nil {
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
