package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/plugins"
)

func main() {
	bilingualFlag := flag.Bool("bilingual", false, "require bilingual README (README.md + README.en.md) and English UI (auto-enabled for official blockmd-plugins/* repos)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: block-plugin-check [-bilingual] <plugin-dir>")
		os.Exit(2)
	}
	pluginDir, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fail("resolve plugin path: %v", err)
	}

	m, err := plugins.LoadManifest(pluginDir)
	if err != nil {
		fail("manifest validation failed: %v", err)
	}
	fmt.Printf("[OK] manifest: app=%s id=%s category=%s (%s / %s v%s beta=%v) surfaces=%v permissions=%v\n",
		m.App, m.ID, m.EffectiveCategory(), m.Name.Zh, m.Name.En, m.Version, m.Beta, m.Surfaces, m.Permissions)

	if m.Contributes != nil && len(m.Contributes.Languages) > 0 {
		if err := plugins.ValidatePluginLanguages(pluginDir, m); err != nil {
			fail("language grammar validation failed: %v", err)
		}
		fmt.Printf("[OK] languages: %d grammar(s) compiled cleanly (tags=%v)\n",
			len(m.Contributes.Languages), m.DisplayFileExtensions())
	}

	isOfficial := plugins.IsOfficialRepo(m.Repo)
	requireBilingual := *bilingualFlag || isOfficial
	if requireBilingual {
		if err := plugins.ValidateManifestEnglish(m); err != nil {
			fail("manifest English validation failed: %v", err)
		}
		if err := plugins.ValidateBilingualDocs(pluginDir); err != nil {
			fail("bilingual docs validation failed: %v", err)
		}
		fmt.Println("[OK] docs: manifest English fields, README.md (zh), and README.en.md (en) validated")
	}

	if m.IsDeclarativeOnly() {
		sandboxDir, err := os.MkdirTemp("", "block-plugin-check-lang-*")
		if err != nil {
			fail("create sandbox: %v", err)
		}
		defer os.RemoveAll(sandboxDir)
		storeMgr := plugins.NewStoreManager(filepath.Join(sandboxDir, "block-appdata"))
		installed, err := storeMgr.InstallFromDirectory(pluginDir)
		if err != nil {
			fail("store install simulation failed: %v", err)
		}
		if err := plugins.ValidatePluginLanguages(installed.InstallDir, &installed.Manifest); err != nil {
			fail("installed language grammar verification failed: %v", err)
		}
		fmt.Printf("[OK] install: declarative language plugin verified in isolated store (enabled=%v)\n", installed.Enabled)
		fmt.Printf("[PASS] Plugin %q passed all Block App Store checks.\n", m.ID)
		return
	}

	zhSnap, err := runLocaleChecks(m, pluginDir, "zh", false)
	if err != nil {
		fail("%v", err)
	}

	if requireBilingual {
		enSnap, err := runLocaleChecks(m, pluginDir, "en", true)
		if err != nil {
			fail("%v", err)
		}
		for _, surf := range allPluginSurfaces(m) {
			if zhSnap[surf] == enSnap[surf] {
				fail("surface %s rendered identical output for zh and en locales", surf)
			}
		}
		fmt.Println("[OK] i18n: English UI passed with zero Chinese text leakage")
	}

	fmt.Printf("[PASS] Plugin %q passed all Block App Store checks.\n", m.ID)
}

func allPluginSurfaces(m *plugins.Manifest) []plugins.Surface {
	surfaces := append([]plugins.Surface(nil), m.Surfaces...)
	if m.Contributes != nil {
		for _, sec := range m.Contributes.SettingsSections {
			surfaces = append(surfaces, plugins.SettingsSectionSurface(sec.ID))
		}
		for _, p := range m.Contributes.Panels {
			surfaces = append(surfaces, plugins.PanelSurface(p.ID))
		}
		for _, s := range m.Contributes.SidebarSections {
			surfaces = append(surfaces, plugins.SidebarSectionSurface(s.ID))
		}
		for _, d := range m.Contributes.Dialogs {
			surfaces = append(surfaces, plugins.DialogSurface(d.ID))
		}
	}
	return surfaces
}

func runLocaleChecks(m *plugins.Manifest, pluginDir, locale string, enforceNoChinese bool) (map[plugins.Surface]string, error) {
	sandboxDir, err := os.MkdirTemp("", "block-plugin-check-"+locale+"-*")
	if err != nil {
		return nil, fmt.Errorf("[%s] create sandbox: %w", locale, err)
	}
	defer os.RemoveAll(sandboxDir)

	notebookDir := filepath.Join(sandboxDir, "notebook")
	downloadsDir := filepath.Join(sandboxDir, "Downloads")
	dataDir := filepath.Join(sandboxDir, "apps-data", m.ID)
	if err := os.MkdirAll(filepath.Join(notebookDir, "notes"), 0o755); err != nil {
		return nil, fmt.Errorf("[%s] prepare sample notebook: %w", locale, err)
	}
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("[%s] prepare sample downloads dir: %w", locale, err)
	}
	if enforceNoChinese {
		_ = os.WriteFile(filepath.Join(notebookDir, "welcome.md"), []byte("# Welcome to Block\n\nFirst sample note with [website](https://blockmd.app) link.\n\n---\n\n## Part Two\n\n> Modular quote preview paragraph.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(notebookDir, "notes", "ideas.md"), []byte("# Architecture Notes\n\nModular plugin runtime.\n"), 0o644)
	} else {
		_ = os.WriteFile(filepath.Join(notebookDir, "welcome.md"), []byte("# 欢迎使用 Block\n\n第一篇示例笔记，支持 [官网](https://blockmd.app) 外链测试。\n\n---\n\n## 第二部分\n\n> 模块化引用预览段落。\n"), 0o644)
		_ = os.WriteFile(filepath.Join(notebookDir, "notes", "ideas.md"), []byte("# 架构手记\n\n模块化插件体系。\n"), 0o644)
	}

	// Verify StoreManager installation flow into an isolated app store directory.
	storeMgr := plugins.NewStoreManager(filepath.Join(sandboxDir, "block-appdata"))
	installed, err := storeMgr.InstallFromDirectory(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("[%s] store install simulation failed: %w", locale, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rt, err := plugins.StartRuntime(ctx, installed.Manifest, plugins.HostConfig{
		PluginDir:    installed.InstallDir,
		DataDir:      dataDir,
		DownloadsDir: downloadsDir,
		NotebookRoot: notebookDir,
		ActiveNote:   "welcome.md",
		Locale:       locale,
		HostVersion:  "1.0.696",
	})
	if err != nil {
		return nil, fmt.Errorf("[%s] start plugin runtime: %w", locale, err)
	}
	defer rt.Shutdown(context.Background())

	initCtx, initCancel := context.WithTimeout(ctx, 15*time.Second)
	err = rt.Initialize(initCtx)
	initCancel()
	if err != nil {
		return nil, fmt.Errorf("[%s] plugin.initialize (desktop 15s budget): %w", locale, err)
	}
	fmt.Printf("[OK] [%s] lifecycle: plugin.initialize ready=true\n", locale)

	surfaces := allPluginSurfaces(m)
	snapshots := make(map[plugins.Surface]string, len(surfaces))
	for _, surf := range surfaces {
		page, err := rt.Render(ctx, surf)
		if err != nil {
			return nil, fmt.Errorf("[%s] plugin.render(%s): %w", locale, surf, err)
		}
		if enforceNoChinese {
			if err := checkEnglishPage(page, sandboxDir); err != nil {
				return nil, fmt.Errorf("[%s] plugin.render(%s): %w", locale, surf, err)
			}
		}
		rawSnap, _ := json.Marshal(page)
		snapshots[surf] = string(rawSnap)

		values := page.ExtractDefaultValues()
		livePage, err := rt.RenderWithValues(ctx, surf, values)
		if err != nil || livePage == nil {
			return nil, fmt.Errorf("[%s] plugin.render(%s, values): %w", locale, surf, err)
		}
		if enforceNoChinese {
			if err := checkEnglishPage(livePage, sandboxDir); err != nil {
				return nil, fmt.Errorf("[%s] plugin.render(%s, defaults): %w", locale, surf, err)
			}
		}

		permutations := 0
		var controls []plugins.UIItem
		for _, card := range page.Cards {
			collectPermutationControls(card.Items, &controls)
		}
		for _, item := range controls {
			switch item.Kind {
			case plugins.KindSegmented, plugins.KindSelect:
				for _, opt := range item.Options {
					trial := cloneValues(values)
					trial[item.ID] = opt.Value
					p, err := rt.RenderWithValues(ctx, surf, trial)
					if err != nil {
						return nil, fmt.Errorf("[%s] plugin.render(%s) failed when %s=%q: %w", locale, surf, item.ID, opt.Value, err)
					}
					if enforceNoChinese {
						if err := checkEnglishPage(p, sandboxDir); err != nil {
							return nil, fmt.Errorf("[%s] plugin.render(%s) when %s=%q: %w", locale, surf, item.ID, opt.Value, err)
						}
					}
					permutations++
				}
			case plugins.KindSwitch:
				bVal, _ := item.Value.(bool)
				trial := cloneValues(values)
				trial[item.ID] = !bVal
				p, err := rt.RenderWithValues(ctx, surf, trial)
				if err != nil {
					return nil, fmt.Errorf("[%s] plugin.render(%s) failed when %s=%v: %w", locale, surf, item.ID, !bVal, err)
				}
				if enforceNoChinese {
					if err := checkEnglishPage(p, sandboxDir); err != nil {
						return nil, fmt.Errorf("[%s] plugin.render(%s) when %s=%v: %w", locale, surf, item.ID, !bVal, err)
					}
				}
				permutations++
			}
		}
		actions := page.CollectActionIDs()
		fmt.Printf("[OK] [%s] surface %s: title=%q cards=%d controls=%d permutations=%d actions=%v\n",
			locale, surf, page.Title, len(page.Cards), len(values), permutations, actions)

		for _, actID := range actions {
			res, err := rt.Action(ctx, surf, actID, values)
			if err != nil {
				return nil, fmt.Errorf("[%s] plugin.action(%s, %s): %w", locale, surf, actID, err)
			}
			if enforceNoChinese {
				if err := checkEnglishText(fmt.Sprintf("action(%s).status", actID), stripSandboxPrefix(res.Status, sandboxDir)); err != nil {
					return nil, fmt.Errorf("[%s] %w", locale, err)
				}
				if res.Page != nil {
					if err := checkEnglishPage(res.Page, sandboxDir); err != nil {
						return nil, fmt.Errorf("[%s] plugin.action(%s, %s).page: %w", locale, surf, actID, err)
					}
				}
			}
			if res.OpenPath != "" {
				if _, statErr := os.Stat(res.OpenPath); statErr != nil {
					return nil, fmt.Errorf("[%s] plugin.action(%s, %s) returned missing open_path %q: %w", locale, surf, actID, res.OpenPath, statErr)
				}
			}
			if res.RevealPath != "" {
				if _, statErr := os.Stat(res.RevealPath); statErr != nil {
					return nil, fmt.Errorf("[%s] plugin.action(%s, %s) returned missing reveal_path %q: %w", locale, surf, actID, res.RevealPath, statErr)
				}
			}
			if res.Values != nil {
				for k, v := range res.Values {
					values[k] = v
				}
			}
			fmt.Printf("[OK] [%s] action %s/%s -> status=%q updatedPage=%v\n",
				locale, surf, actID, res.Status, res.Page != nil)
		}
	}

	if m.HasPermission(plugins.PermEventNoteSaved) {
		if err := rt.NotifyNoteSaved(ctx, notebookDir, "welcome.md"); err != nil {
			return nil, fmt.Errorf("[%s] plugin.event(note.saved): %w", locale, err)
		}
		fmt.Printf("[OK] [%s] event: note.saved request delivered\n", locale)
	}

	if err := rt.Shutdown(ctx); err != nil {
		return nil, fmt.Errorf("[%s] plugin.shutdown: %w", locale, err)
	}
	return snapshots, nil
}

func collectPermutationControls(items []plugins.UIItem, out *[]plugins.UIItem) {
	for _, item := range items {
		switch item.Kind {
		case plugins.KindSegmented, plugins.KindSelect, plugins.KindSwitch:
			*out = append(*out, item)
		case plugins.KindGroup:
			collectPermutationControls(item.Fields, out)
		}
	}
}

func cloneValues(values map[string]any) map[string]any {
	trial := make(map[string]any, len(values))
	for k, v := range values {
		trial[k] = v
	}
	return trial
}

func stripSandboxPrefix(val, sandboxDir string) string {
	if sandboxDir == "" || val == "" {
		return val
	}
	seen := make(map[string]bool, 4)
	var prefixes []string
	addPrefix := func(p string) {
		p = strings.TrimRight(p, string(filepath.Separator))
		if p == "" || p == "." || seen[p] {
			return
		}
		seen[p] = true
		prefixes = append(prefixes, p)
	}
	addPrefix(sandboxDir)
	addPrefix(filepath.Clean(sandboxDir))
	if abs, err := filepath.Abs(sandboxDir); err == nil {
		addPrefix(abs)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(sandboxDir)); err == nil {
		addPrefix(resolved)
	}
	// Replace longest prefixes first and require a path/token boundary so
	// sibling names like "<sandbox>-backup" are never stripped.
	for i := 0; i < len(prefixes); i++ {
		for j := i + 1; j < len(prefixes); j++ {
			if len(prefixes[j]) > len(prefixes[i]) {
				prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
			}
		}
	}
	out := val
	for _, pref := range prefixes {
		out = stripBoundedPathPrefix(out, pref)
	}
	return out
}

func stripBoundedPathPrefix(s, pref string) string {
	if pref == "" || !strings.Contains(s, pref) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], pref)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + idx
		end := start + len(pref)
		beforeOK := start == 0 || isPathPrefixBoundaryBefore(s[start-1])
		afterOK := isPathPrefixBoundaryAfter(s, pref, end)
		if beforeOK && afterOK {
			b.WriteString(s[i:start])
			i = end
			continue
		}
		b.WriteString(s[i : start+1])
		i = start + 1
	}
	return b.String()
}

func isPathPrefixBoundaryBefore(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '"' || c == '\'' || c == '(' || c == '[' || c == ':' || c == '=' || c == '`' || c == '<'
}

func isPathPrefixBoundaryAfter(s, pref string, end int) bool {
	if end == len(s) {
		return true
	}
	c := s[end]
	if c == '/' || (os.PathSeparator == '\\' && c == '\\') {
		tokenEnd := end
		for tokenEnd < len(s) && !isPathTokenTerminator(s[tokenEnd]) {
			tokenEnd++
		}
		cleanPref := strings.TrimRight(filepath.Clean(pref), string(filepath.Separator))
		joined := filepath.Clean(pref + s[end:tokenEnd])
		return joined == cleanPref || strings.HasPrefix(joined, cleanPref+string(filepath.Separator))
	}
	if c == ':' || c == ',' || c == ';' {
		return end+1 == len(s) || isASCIIWhitespace(s[end+1])
	}
	return isASCIIWhitespace(c) || c == '"' || c == '\'' || c == ')' || c == ']' || c == '`' || c == '>'
}

func isASCIIWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isPathTokenTerminator(c byte) bool {
	return isASCIIWhitespace(c) || c == '"' || c == '\'' || c == ')' || c == ']' || c == '`' || c == '>' || c == ',' || c == ';'
}

func checkEnglishPage(page *plugins.UIPage, sandboxDir string) error {
	if page == nil {
		return nil
	}
	if err := checkEnglishText("page.title", stripSandboxPrefix(page.Title, sandboxDir)); err != nil {
		return err
	}
	if err := checkEnglishText("page.status", stripSandboxPrefix(page.Status, sandboxDir)); err != nil {
		return err
	}
	for i, b := range page.HeaderActions {
		if err := checkEnglishText(fmt.Sprintf("page.header_actions[%d].label", i), b.Label); err != nil {
			return err
		}
	}
	for ci, card := range page.Cards {
		cp := fmt.Sprintf("page.cards[%d]", ci)
		if err := checkEnglishText(cp+".title", card.Title); err != nil {
			return err
		}
		if err := checkEnglishText(cp+".meta", stripSandboxPrefix(card.Meta, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(cp+".badge", card.Badge); err != nil {
			return err
		}
		for ii, item := range card.Items {
			if err := checkEnglishItem(fmt.Sprintf("%s.items[%d]", cp, ii), item, sandboxDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkEnglishItem(prefix string, item plugins.UIItem, sandboxDir string) error {
	if err := checkEnglishText(prefix+".label", item.Label); err != nil {
		return err
	}
	if err := checkEnglishText(prefix+".placeholder", item.Placeholder); err != nil {
		return err
	}
	if sVal, ok := item.Value.(string); ok {
		if err := checkEnglishText(prefix+".value", stripSandboxPrefix(sVal, sandboxDir)); err != nil {
			return err
		}
	}
	if err := checkEnglishText(prefix+".empty_text", item.EmptyText); err != nil {
		return err
	}
	if err := checkEnglishText(prefix+".status", stripSandboxPrefix(item.Status, sandboxDir)); err != nil {
		return err
	}
	if err := checkEnglishText(prefix+".tooltip", stripSandboxPrefix(item.Tooltip, sandboxDir)); err != nil {
		return err
	}
	for i, opt := range item.Options {
		op := fmt.Sprintf("%s.options[%d]", prefix, i)
		if err := checkEnglishText(op+".label", stripSandboxPrefix(opt.Label, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(op+".detail", stripSandboxPrefix(opt.Detail, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(op+".badge", opt.Badge); err != nil {
			return err
		}
		if err := checkEnglishText(op+".tooltip", stripSandboxPrefix(opt.Tooltip, sandboxDir)); err != nil {
			return err
		}
	}
	for i, b := range item.Buttons {
		if err := checkEnglishText(fmt.Sprintf("%s.buttons[%d].label", prefix, i), b.Label); err != nil {
			return err
		}
	}
	for i, row := range item.Rows {
		rp := fmt.Sprintf("%s.rows[%d]", prefix, i)
		if err := checkEnglishText(rp+".label", row.Label); err != nil {
			return err
		}
		if err := checkEnglishText(rp+".value", stripSandboxPrefix(row.Value, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(rp+".tooltip", stripSandboxPrefix(row.Tooltip, sandboxDir)); err != nil {
			return err
		}
	}
	for i, m := range item.Metrics {
		mp := fmt.Sprintf("%s.metrics[%d]", prefix, i)
		if err := checkEnglishText(mp+".label", m.Label); err != nil {
			return err
		}
		if err := checkEnglishText(mp+".value", stripSandboxPrefix(m.Value, sandboxDir)); err != nil {
			return err
		}
	}
	if item.Summary != nil {
		if err := checkEnglishText(prefix+".summary.total_label", item.Summary.TotalLabel); err != nil {
			return err
		}
		if err := checkEnglishText(prefix+".summary.total_value", item.Summary.TotalValue); err != nil {
			return err
		}
		if err := checkEnglishText(prefix+".summary.sub_text", item.Summary.SubText); err != nil {
			return err
		}
		for i, m := range item.Summary.Metrics {
			mp := fmt.Sprintf("%s.summary.metrics[%d]", prefix, i)
			if err := checkEnglishText(mp+".label", m.Label); err != nil {
				return err
			}
			if err := checkEnglishText(mp+".value", m.Value); err != nil {
				return err
			}
		}
	}
	for i, tile := range item.Tiles {
		tp := fmt.Sprintf("%s.tiles[%d]", prefix, i)
		if err := checkEnglishText(tp+".label", tile.Label); err != nil {
			return err
		}
		if err := checkEnglishText(tp+".detail", tile.Detail); err != nil {
			return err
		}
	}
	if item.Preview != nil {
		pp := prefix + ".preview"
		if err := checkEnglishText(pp+".badge", item.Preview.Badge); err != nil {
			return err
		}
		if err := checkEnglishText(pp+".title", stripSandboxPrefix(item.Preview.Title, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(pp+".subtitle", stripSandboxPrefix(item.Preview.Subtitle, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(pp+".before", stripSandboxPrefix(item.Preview.Before, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(pp+".after", stripSandboxPrefix(item.Preview.After, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(pp+".body", stripSandboxPrefix(item.Preview.Body, sandboxDir)); err != nil {
			return err
		}
		for i, card := range item.Preview.Cards {
			cp := fmt.Sprintf("%s.cards[%d]", pp, i)
			if err := checkEnglishText(cp+".badge", card.Badge); err != nil {
				return err
			}
			if err := checkEnglishText(cp+".title", stripSandboxPrefix(card.Title, sandboxDir)); err != nil {
				return err
			}
			if err := checkEnglishText(cp+".body", stripSandboxPrefix(card.Body, sandboxDir)); err != nil {
				return err
			}
		}
	}
	for i, entry := range item.Entries {
		ep := fmt.Sprintf("%s.entries[%d]", prefix, i)
		if err := checkEnglishText(ep+".title", stripSandboxPrefix(entry.Title, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(ep+".sub_text", stripSandboxPrefix(entry.SubText, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(ep+".status", stripSandboxPrefix(entry.Status, sandboxDir)); err != nil {
			return err
		}
		if err := checkEnglishText(ep+".tooltip", stripSandboxPrefix(entry.Tooltip, sandboxDir)); err != nil {
			return err
		}
	}
	for i, field := range item.Fields {
		if err := checkEnglishItem(fmt.Sprintf("%s.fields[%d]", prefix, i), field, sandboxDir); err != nil {
			return err
		}
	}
	return nil
}

func checkEnglishText(field, value string) error {
	if bad, ok := plugins.FindChineseRune(value); ok {
		return fmt.Errorf("%s contains untranslated Chinese character %q in %q", field, bad, value)
	}
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[FAIL] "+format+"\n", args...)
	os.Exit(1)
}
