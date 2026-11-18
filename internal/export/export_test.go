package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestThemeSpecAll12ThemesAndSwatches(t *testing.T) {
	all := AllThemes()
	if len(all) != 12 {
		t.Fatalf("expected 12 themes, got %d", len(all))
	}
	if len(LightThemes()) != 6 || len(DarkThemes()) != 6 {
		t.Fatalf("expected 6 light + 6 dark themes")
	}

	seen := make(map[model.ExportTheme]bool)
	for _, th := range all {
		seen[th] = true
		sp := ThemeSpec(th)
		if sp.Theme != th {
			t.Fatalf("ThemeSpec(%q) returned Theme=%q", th, sp.Theme)
		}
		if sp.Dark != th.IsDark() {
			t.Fatalf("ThemeSpec(%q) Dark mismatch: got %v want %v", th, sp.Dark, th.IsDark())
		}
		if sp.Canvas.A == 0 || sp.InnerCanvas.A == 0 || sp.Title.A == 0 || sp.Text.A == 0 ||
			sp.Secondary.A == 0 || sp.Accent.A == 0 || sp.CodeBG.A == 0 || sp.CodeBorder.A == 0 ||
			sp.TableBorder.A == 0 || sp.QuoteBar.A == 0 {
			t.Fatalf("ThemeSpec(%q) has zero color component: %+v", th, sp)
		}
		if sp.Swatch[0].A == 0 || sp.Swatch[1].A == 0 || sp.Swatch[2].A == 0 {
			t.Fatalf("ThemeSpec(%q) has incomplete Swatch triad: %+v", th, sp.Swatch)
		}
		if sp.Label == "" {
			t.Fatalf("ThemeSpec(%q) missing label", th)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("expected 12 unique themes, got %d", len(seen))
	}
}

func TestControllerPreferencesPersistenceAndActions(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	dataDir := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "exports")
	redraws := 0

	ctrl := NewController(dataDir, func() { redraws++ })
	whole := markdown.ParseMarkdown("# 全文标题\n\n这是整篇笔记正文。\n")
	sel := markdown.ParseMarkdown("这是选区导出内容。\n")

	ctrl.Open("测试笔记", filepath.Join(dataDir, "note.md"), whole, sel)
	if !ctrl.IsOpen() {
		t.Fatalf("expected controller open")
	}
	if ctrl.ActiveFormat() != model.ExportPNG || ctrl.ActiveStyle() != model.ExportThemeMono {
		t.Fatalf("unexpected defaults: fmt=%q style=%q", ctrl.ActiveFormat(), ctrl.ActiveStyle())
	}
	if !ctrl.ShowTitle() || !ctrl.Watermark() || ctrl.UseSelection() {
		t.Fatalf("unexpected initial toggles: showTitle=%v watermark=%v useSel=%v", ctrl.ShowTitle(), ctrl.Watermark(), ctrl.UseSelection())
	}

	ctrl.SetFormat(model.ExportHTML)
	ctrl.SetStyle(model.ExportThemeJournal)
	ctrl.SetStyle(model.ExportThemeNeon)
	ctrl.SetUseSelection(true)
	ctrl.ToggleShowTitle()
	ctrl.ToggleWatermark()

	outPath := filepath.Join(outDir, "custom.html")
	if err := ctrl.SaveToPath(outPath); err != nil {
		t.Fatalf("SaveToPath failed: %v", err)
	}
	if ctrl.SavedPath() != outPath {
		t.Fatalf("expected SavedPath=%q, got %q", outPath, ctrl.SavedPath())
	}
	rawSaved, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read saved output: %v", err)
	}
	savedStr := string(rawSaved)
	if !strings.Contains(savedStr, "这是选区导出内容") || strings.Contains(savedStr, "这是整篇笔记正文") {
		t.Fatalf("expected selection content in saved HTML, got:\n%s", savedStr)
	}
	if strings.Contains(savedStr, `<footer class="doc-footer">`) {
		t.Fatalf("expected watermark off in saved HTML")
	}

	// Verify export-preferences.json persistence and reload into a new Controller.
	prefsBytes, err := os.ReadFile(filepath.Join(dataDir, exportPrefsFilename))
	if err != nil {
		t.Fatalf("read export-preferences.json: %v", err)
	}
	var diskPrefs map[string]any
	if err := json.Unmarshal(prefsBytes, &diskPrefs); err != nil {
		t.Fatalf("unmarshal export-preferences.json: %v", err)
	}
	if diskPrefs["format"] != "html" || diskPrefs["style"] != "neon" ||
		diskPrefs["light_style"] != "journal" || diskPrefs["dark_style"] != "neon" ||
		diskPrefs["show_title"] != false || diskPrefs["watermark"] != false ||
		diskPrefs["export_directory"] != outDir {
		t.Fatalf("unexpected persisted preferences JSON: %s", string(prefsBytes))
	}

	ctrl2 := NewController(dataDir, nil)
	if ctrl2.ActiveFormat() != model.ExportHTML || ctrl2.ActiveStyle() != model.ExportThemeNeon ||
		ctrl2.ShowTitle() || ctrl2.Watermark() {
		t.Fatalf("reloaded controller did not restore preferences: fmt=%q style=%q showTitle=%v wm=%v",
			ctrl2.ActiveFormat(), ctrl2.ActiveStyle(), ctrl2.ShowTitle(), ctrl2.Watermark())
	}

	if redraws == 0 {
		t.Fatalf("expected onRedraw callbacks to fire")
	}
}

func TestExportViewUIWithTester(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	theme.SetDark(false)
	dataDir := t.TempDir()
	saveDir := t.TempDir()

	ctrl := NewController(dataDir, nil)
	whole := markdown.ParseMarkdown("# 导出测试文档\n\n第一段整篇内容。\n\n| A | B |\n|---|---|\n| 1 | [链接](https://example.com) |\n")
	sel := markdown.ParseMarkdown("仅选区片段。\n")
	ctrl.Open("导出测试文档", filepath.Join(dataDir, "doc.md"), whole, sel)

	savedTarget := filepath.Join(saveDir, "preview-out.html")
	ctrl.OnSaveDialog = func(dir, suggested string, done func(string)) {
		done(savedTarget)
	}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			ctrl.Render(c)
		})
	}, 1280, 820)

	// Verify structural anchors exist via both Tester and Probe bounds.
	for _, id := range []string{
		"export-view",
		"export-back",
		"export-stage",
		"export-options",
		"export-title-toggle",
		"export-watermark",
		"export-copy",
		"export-save",
	} {
		if _, ok := internalui.TargetBounds(id); !ok {
			t.Fatalf("expected probed bounds for %q", id)
		}
	}
	if !tt.HasText("导出测试文档") || !tt.HasText("· 导出预览") {
		t.Fatalf("expected header title in rendered view, texts=%v", tt.Texts())
	}

	// Switch to dark themes group and pick Kraft, then back to light and pick Journal.
	if err := tt.Click("深色 · 6"); err != nil {
		t.Fatalf("click dark group: %v", err)
	}
	if ctrl.ActiveStyle() != model.ExportThemeInk {
		t.Fatalf("expected default dark Ink, got %q", ctrl.ActiveStyle())
	}
	if err := tt.Click("export-style-kraft"); err != nil {
		t.Fatalf("click export-style-kraft: %v", err)
	}
	if ctrl.ActiveStyle() != model.ExportThemeKraft {
		t.Fatalf("expected Kraft style, got %q", ctrl.ActiveStyle())
	}
	if err := tt.Click("浅色 · 6"); err != nil {
		t.Fatalf("click light group: %v", err)
	}
	if err := tt.Click("export-style-journal"); err != nil {
		t.Fatalf("click export-style-journal: %v", err)
	}
	if ctrl.ActiveStyle() != model.ExportThemeJournal {
		t.Fatalf("expected Journal style, got %q", ctrl.ActiveStyle())
	}

	// Switch scope to selection and back.
	if err := tt.Click("选中导出"); err != nil {
		t.Fatalf("click selection scope: %v", err)
	}
	if !ctrl.UseSelection() {
		t.Fatalf("expected UseSelection=true after clicking 选中导出")
	}

	// Toggle Show Title and Watermark.
	if err := tt.Click("export-title-toggle"); err != nil {
		t.Fatalf("click export-title-toggle: %v", err)
	}
	if ctrl.ShowTitle() {
		t.Fatalf("expected ShowTitle=false after toggle")
	}
	if err := tt.Click("export-watermark"); err != nil {
		t.Fatalf("click export-watermark: %v", err)
	}
	if ctrl.Watermark() {
		t.Fatalf("expected Watermark=false after toggle")
	}

	// Switch format to HTML and copy via button + Cmd+C.
	if err := tt.Click("HTML"); err != nil {
		t.Fatalf("click HTML format: %v", err)
	}
	if ctrl.ActiveFormat() != model.ExportHTML {
		t.Fatalf("expected ExportHTML, got %q", ctrl.ActiveFormat())
	}
	if err := tt.Click("export-copy"); err != nil {
		t.Fatalf("click export-copy: %v", err)
	}
	clipBytes, err := tt.ClipboardData().Read(transfer.HTML)
	if err != nil || !strings.Contains(string(clipBytes), "仅选区片段") {
		t.Fatalf("expected HTML in clipboard data, got err=%v len=%d", err, len(clipBytes))
	}

	// Trigger Save via button and verify export-reveal appears.
	if err := tt.Click("export-save"); err != nil {
		t.Fatalf("click export-save: %v", err)
	}
	if ctrl.SavedPath() != savedTarget {
		t.Fatalf("expected SavedPath=%q, got %q", savedTarget, ctrl.SavedPath())
	}
	if _, ok := internalui.TargetBounds("export-reveal"); !ok {
		t.Fatalf("expected export-reveal button after save")
	}
	if _, err := os.Stat(savedTarget); err != nil {
		t.Fatalf("expected saved file on disk: %v", err)
	}

	// Escape closes the view.
	tt.Key(0, ui.KeyEscape)
	if ctrl.IsOpen() {
		t.Fatalf("expected Escape to close export view")
	}
}
