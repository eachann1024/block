package modals

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type stubHost struct {
	activeNotebook string
	notebooks      []string
	activeNote     string
	openedPath     string
	openedLine     int
	written        map[string]string
	redraws        int
}

func newStubHost(nb string) *stubHost {
	return &stubHost{
		activeNotebook: nb,
		notebooks:      []string{nb},
		written:        make(map[string]string),
	}
}

func (h *stubHost) ActiveNotebook() string                       { return h.activeNotebook }
func (h *stubHost) Notebooks() []string                          { return h.notebooks }
func (h *stubHost) ActiveNotePath() string                       { return h.activeNote }
func (h *stubHost) ListNotebookFiles(string) ([]model.FileEntry, error) {
	return nil, nil
}
func (h *stubHost) SearchWorkspace(string, bool) ([]model.SearchHit, error) {
	return nil, nil
}
func (h *stubHost) ReadNote(p string) (string, error) {
	if s, ok := h.written[p]; ok {
		return s, nil
	}
	b, err := os.ReadFile(p)
	return string(b), err
}
func (h *stubHost) WriteNote(absPath string, content string) error {
	h.written[absPath] = content
	return os.WriteFile(absPath, []byte(content), 0o644)
}
func (h *stubHost) OpenNote(absPath string, lineHint int) error {
	h.openedPath = absPath
	h.openedLine = lineHint
	return nil
}
func (h *stubHost) PreviewPlanInEditor(*model.BatchPlan) error { return nil }
func (h *stubHost) ApplyBatchPlan(*model.BatchPlan) error      { return nil }
func (h *stubHost) RejectBatchPlan(string) error               { return nil }
func (h *stubHost) UndoLastPlan(*model.BatchPlan) error        { return nil }
func (h *stubHost) OpenSettings(string)                        {}
func (h *stubHost) RequestRedraw()                             { h.redraws++ }

func setupManager(t *testing.T) (*Manager, *stubHost, *model.AppState) {
	t.Helper()
	theme.SetLanguage(model.LangZh)
	tmp := t.TempDir()
	nb := filepath.Join(tmp, "notebook")
	if err := os.MkdirAll(nb, 0o755); err != nil {
		t.Fatal(err)
	}
	host := newStubHost(nb)
	state := model.DefaultAppState(model.LangZh)
	state.Folder = nb
	state.Notebooks = []string{nb}

	m := NewManager(filepath.Join(tmp, "data"), host)
	m.GetState = func() model.AppState { return state }
	m.OnStateChanged = func(fn func(*model.AppState)) { fn(&state) }
	return m, host, &state
}

func TestSettingsNavigationControlsAndShortcutRecording(t *testing.T) {
	m, _, state := setupManager(t)
	aiRendered := false
	m.RenderAISettings = func(c *ui.Context) {
		aiRendered = true
		ui.Text(c, "Custom AI Settings")
	}

	m.OpenSettings("ai")
	if !m.IsSettingsOpen() || m.ActiveSettingsTab() != "ai" {
		t.Fatalf("expected settings open on ai tab")
	}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderSettings(c)
		})
	}, 1000, 720)

	if !aiRendered || !tt.HasText("Custom AI Settings") {
		t.Fatalf("expected AI settings callback to render")
	}

	// Switch to Appearance tab, toggle theme & language.
	if err := tt.Click("外观与字体"); err != nil {
		t.Fatal(err)
	}
	if m.ActiveSettingsTab() != "appearance" {
		t.Fatalf("expected appearance tab, got %q", m.ActiveSettingsTab())
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	if state.Theme != model.ThemeDark {
		t.Fatalf("expected ThemeDark, got %q", state.Theme)
	}
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	if state.Language != model.LangEn {
		t.Fatalf("expected LangEn, got %q", state.Language)
	}

	// Switch to Folders tab, toggle hide-non-md and add exact rule.
	if err := tt.Click("Folders"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Hide Non-Markdown Files"); err != nil {
		t.Fatal(err)
	}
	if state.HideNonMarkdownFiles == nil || !*state.HideNonMarkdownFiles {
		t.Fatalf("expected HideNonMarkdownFiles to toggle true")
	}
	if err := tt.Click("Folder Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("drafts")
	if err := tt.Click("Add Folder"); err != nil {
		t.Fatal(err)
	}
	if len(state.FolderRules) != 1 || state.FolderRules[0].Pattern != "drafts" {
		t.Fatalf("expected drafts folder rule, got %+v", state.FolderRules)
	}

	// Switch to Shortcuts tab, start recording, record Cmd+Shift+L, then verify.
	if err := tt.Click("Shortcuts"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("⌥N"); err != nil {
		t.Fatal(err)
	}
	if !m.IsRecordingShortcut() {
		t.Fatalf("expected IsRecordingShortcut() == true")
	}
	tt.Key(ui.Super|ui.Shift, ui.KeyL)
	if m.IsRecordingShortcut() {
		t.Fatalf("expected recording to stop after valid chord")
	}
	if state.ShortcutQuick != "shift-cmd-l" {
		t.Fatalf("expected recorded shortcut shift-cmd-l, got %q", state.ShortcutQuick)
	}

	// Close settings via Back button.
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if m.IsSettingsOpen() {
		t.Fatalf("expected settings closed after clicking Back")
	}
}

func TestPerformanceDiskSparseAndZeroFilesAndUI(t *testing.T) {
	m, _, _ := setupManager(t)
	root := t.TempDir()

	// Create all 7 categories, including a 60 GiB sparse file and a 0 B file.
	writeBytes := func(rel string, n int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeBytes("note.md", 4096)
	writeBytes("history/snap.md", 4096)
	writeBytes("recovery/rec.dat", 4096)
	writeBytes("ai-chats.json", 4096)
	writeBytes("logs/block.log", 4096)
	writeBytes("state.json", 4096)

	// 60 GiB sparse file in assets (0 allocated physical blocks on APFS/Unix) + 0B file.
	sparsePath := filepath.Join(root, "assets/huge.bin")
	if err := os.MkdirAll(filepath.Dir(sparsePath), 0o755); err != nil {
		t.Fatal(err)
	}
	sf, err := os.Create(sparsePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.Truncate(60 * 1024 * 1024 * 1024); err != nil {
		sf.Close()
		t.Fatal(err)
	}
	sf.Close()

	zeroPath := filepath.Join(root, "assets/empty.png")
	if err := os.WriteFile(zeroPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	m.RefreshPerformanceDisk(root)
	cats := m.PerformanceCategories()
	if len(cats) != 7 {
		t.Fatalf("expected 7 categories, got %d", len(cats))
	}
	byID := make(map[string]PerfCategory, len(cats))
	for _, c := range cats {
		byID[c.ID] = c
	}
	if byID["assets"].Bytes >= 1024*1024 {
		t.Fatalf("expected 60GiB sparse + 0B file to use <1MiB physical blocks, got %d", byID["assets"].Bytes)
	}
	if byID["notes"].Bytes == 0 || byID["history"].Bytes == 0 || byID["recovery"].Bytes == 0 ||
		byID["ai_sessions"].Bytes == 0 || byID["logs"].Bytes == 0 || byID["config"].Bytes == 0 {
		t.Fatalf("expected all non-sparse categories to have >0 physical bytes: %+v", byID)
	}

	// Open Performance modal and test Squarified Treemap + Escape close in ui.NewTester.
	m.OpenPerformance()
	defer m.ClosePerformance()
	m.RefreshPerformanceDisk(root)

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderModals(c)
		})
	}, 900, 680)

	if !tt.HasText("性能与占用") {
		t.Fatalf("expected performance modal title")
	}
	if err := tt.Click("AI 会话"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if m.IsPerformanceOpen() {
		t.Fatalf("expected Escape to close performance modal")
	}
}

func TestFeedbackSearchHistoryExportAboutUpdaterModals(t *testing.T) {
	m, host, state := setupManager(t)

	// 1. Feedback modal: switch category, draft auto-save, submit -> done.
	m.OpenFeedback()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderModals(c)
		})
	}, 900, 680)

	if err := tt.Click("功能建议"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("反馈内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Great native UI")
	if state.FeedbackDraft == nil || state.FeedbackDraft.Category != "idea" || state.FeedbackDraft.Message != "Great native UI" {
		t.Fatalf("expected auto-saved FeedbackDraft, got %+v", state.FeedbackDraft)
	}
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("收到了，谢谢你") {
		t.Fatalf("expected thank-you done state after feedback submit")
	}
	if state.FeedbackDraft != nil {
		t.Fatalf("expected draft cleared after submit")
	}
	if err := tt.Click("好的"); err != nil {
		t.Fatal(err)
	}
	if m.IsFeedbackOpen() {
		t.Fatalf("expected feedback modal closed")
	}

	// 2. Search modal: search input, scope toggle, hit selection.
	var lastAll bool
	m.SearchFn = func(query string, all bool) ([]model.SearchHit, error) {
		lastAll = all
		return []model.SearchHit{{
			Path:     filepath.Join(host.activeNotebook, "hello.md"),
			Notebook: "notebook",
			Title:    "Hello Note",
			Line:     3,
			Snippet:  "matched " + query,
		}}, nil
	}
	m.OpenSearch(false)
	tt.Frame()
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type("world")
	if !tt.HasText("Hello Note") {
		t.Fatalf("expected search hit rendered")
	}
	if err := tt.Click("notebook"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("全部文件夹"); err != nil {
		t.Fatal(err)
	}
	if !lastAll {
		t.Fatalf("expected allNotebooks=true after scope toggle")
	}
	if err := tt.Click("Hello Note"); err != nil {
		t.Fatal(err)
	}
	if m.IsSearchOpen() || host.openedLine != 3 {
		t.Fatalf("expected search hit click to open note and close modal")
	}

	// 3. History modal: snapshots + Myers Diff + restore.
	notePath := filepath.Join(host.activeNotebook, "doc.md")
	m.ListHistoryFn = func(string) ([]model.HistorySnapshot, error) {
		return []model.HistorySnapshot{{ID: "snap1", Timestamp: 1700000000, Size: 12}}, nil
	}
	m.ReadHistoryFn = func(string, string) (string, error) {
		return "line1\nold line", nil
	}
	m.OpenHistory(notePath, "line1\nnew line")
	tt.Frame()
	if !tt.HasText("- old line") || !tt.HasText("+ new line") {
		t.Fatalf("expected Myers diff lines in history modal, texts=%v", tt.Texts())
	}
	if err := tt.Click("恢复此版本"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("确认恢复"); err != nil {
		t.Fatal(err)
	}
	if got := host.written[notePath]; got != "line1\nold line" {
		t.Fatalf("expected restored note content, got %q", got)
	}

	// 4. Export modal: select HTML & run ExportFn.
	var exportedFmt model.ExportFormat
	var exportedOut string
	m.ExportFn = func(format model.ExportFormat, outPath string) error {
		exportedFmt = format
		exportedOut = outPath
		return nil
	}
	m.OpenExport(notePath)
	tt.Frame()
	if err := tt.Click("HTML"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("导出"); err != nil {
		t.Fatal(err)
	}
	if exportedFmt != model.ExportHTML || !strings.HasSuffix(exportedOut, ".html") {
		t.Fatalf("expected html export, got fmt=%q out=%q", exportedFmt, exportedOut)
	}
	m.CloseExport()

	// 5. About & Updater modals.
	m.OpenAbout()
	tt.Frame()
	if !m.IsAboutOpen() || !tt.HasText("Block") {
		t.Fatalf("expected about modal open")
	}
	m.CloseAbout()

	m.OpenUpdater()
	tt.Frame()
	if !m.HasActiveModal() || !tt.HasText("检查更新") {
		t.Fatalf("expected updater modal open")
	}
	m.CloseUpdater()
	if m.HasActiveModal() {
		t.Fatalf("expected no active modal")
	}
}

func TestQuickNoteControllerDebounceAndSave(t *testing.T) {
	tmp := t.TempDir()
	nb := filepath.Join(tmp, "notebook")
	if err := os.MkdirAll(nb, 0o755); err != nil {
		t.Fatal(err)
	}
	host := newStubHost(nb)
	q := NewQuickNoteController(filepath.Join(tmp, "data"), host, nil)

	// 180ms debounce check.
	if q.IsVisible() {
		t.Fatalf("expected quick note initially hidden")
	}
	if !q.TriggerHotkey() || !q.IsVisible() {
		t.Fatalf("expected first TriggerHotkey to succeed and show panel")
	}
	if q.TriggerHotkey() || !q.IsVisible() {
		t.Fatalf("expected second TriggerHotkey within 180ms to be debounced")
	}
	time.Sleep(195 * time.Millisecond)
	if !q.TriggerHotkey() || q.IsVisible() {
		t.Fatalf("expected TriggerHotkey after 180ms to toggle panel hidden")
	}

	// RenderView + Text persistence + SaveToNotebook.
	q.Show()
	q.SetText("# Quick Idea\nRemember to ship")
	tt := ui.NewTester(q.RenderView, 440, 520)
	if !tt.HasText("# Quick Idea\nRemember to ship") && q.Text() != "# Quick Idea\nRemember to ship" {
		t.Fatalf("expected quick note text preserved")
	}
	savedPath, err := q.SaveToNotebook()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(savedPath, nb) || filepath.Base(savedPath) != "Quick Idea.md" {
		t.Fatalf("unexpected saved path: %q", savedPath)
	}
	if q.Text() != "" {
		t.Fatalf("expected active slot cleared after saving to notebook")
	}
}

func TestQuickNoteParityPagerScrollZoomAndCallbacks(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	tmp := t.TempDir()
	nb := filepath.Join(tmp, "notebook")
	if err := os.MkdirAll(nb, 0o755); err != nil {
		t.Fatal(err)
	}
	host := newStubHost(nb)
	q := NewQuickNoteController(filepath.Join(tmp, "data"), host, nil)

	var zoomVal float32 = 1.0
	var zoomChanges []float32
	var savedNotes []string
	q.GetQuickZoom = func() float32 { return zoomVal }
	q.OnQuickZoomChanged = func(z float32) {
		zoomVal = z
		zoomChanges = append(zoomChanges, z)
	}
	q.OnSavedNote = func(p string) {
		savedNotes = append(savedNotes, p)
	}

	internalui.ClearTargets()
	tt := ui.NewTester(q.RenderView, 440, 520)

	// Centered 3-dot pager & header probes ("quick-save" + "save-to-note" + "quick-close").
	pagerRect, ok := internalui.TargetBounds("quick-pager")
	if !ok {
		t.Fatalf("expected quick-pager bounds recorded")
	}
	pagerCenter := pagerRect.X + pagerRect.W/2
	if math.Abs(float64(pagerCenter-220)) > 6 {
		t.Fatalf("expected centered quick-pager near x=220, got center=%.1f (rect=%+v)", pagerCenter, pagerRect)
	}
	for _, probeID := range []string{"quick-slot-0", "quick-slot-1", "quick-slot-2", "quick-save", "save-to-note", "quick-close"} {
		if _, ok := internalui.TargetBounds(probeID); !ok {
			t.Fatalf("expected probe %q recorded", probeID)
		}
	}

	// OnShiftScroll: 16px threshold, negative -> next, 160ms cooldown, positive -> previous.
	if got := q.OnShiftScroll(-8); got != 0 {
		t.Fatalf("expected partial -8 delta not to switch yet, got slot %d", got)
	}
	if got := q.OnShiftScroll(-8); got != 1 || q.ActiveSlot() != 1 {
		t.Fatalf("expected -16 accumulated delta to advance to slot 1, got %d", got)
	}
	if got := q.OnShiftScroll(-24); got != 1 {
		t.Fatalf("expected 160ms cooldown to suppress immediate switch, got %d", got)
	}
	time.Sleep(170 * time.Millisecond)
	if got := q.OnShiftScroll(20); got != 0 || q.ActiveSlot() != 0 {
		t.Fatalf("expected positive delta after cooldown to move back to slot 0, got %d", got)
	}

	// QuickEditorZoom shortcuts: Cmd+= (1.1), Cmd+- (0.9), Cmd+0 (1.0).
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Key(ui.Cmd, ui.KeyMinus)
	tt.Key(ui.Cmd, ui.Key0)
	if len(zoomChanges) != 3 || zoomChanges[0] != 1.1 || zoomChanges[1] != 0.9 || zoomChanges[2] != 1.0 {
		t.Fatalf("unexpected QuickEditorZoom sequence: %v", zoomChanges)
	}

	// SaveToNotebook triggers OnSavedNote callback and sets notice.
	q.SetText("# Callback Test\nBody")
	savedPath, err := q.SaveToNotebook()
	if err != nil {
		t.Fatal(err)
	}
	if len(savedNotes) != 1 || savedNotes[0] != savedPath {
		t.Fatalf("expected OnSavedNote(%q), got %v", savedPath, savedNotes)
	}
}

func TestShortcutSettingsCustomActionsConflictsExpandedKeysAndReset(t *testing.T) {
	m, _, state := setupManager(t)
	defs := AllShortcutDefaults()
	for _, k := range []string{"global_summon", "quick_note", "search", "new_note", "toggle_sidebar", "toggle_ai", "export_image", "performance"} {
		if defs[k] == "" {
			t.Fatalf("expected default for %q", k)
		}
	}
	if got := EffectiveShortcut(*state, "toggle_ai"); got != "cmd-j" {
		t.Fatalf("expected default toggle_ai=cmd-j, got %q", got)
	}

	m.OpenSettings("shortcuts")
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderSettings(c)
		})
	}, 1000, 760)

	// Record custom shortcut onto toggle_ai ("⌘J" button).
	if err := tt.Click("⌘J"); err != nil {
		t.Fatal(err)
	}
	if !m.IsRecordingShortcut() || m.recordingShortcut != "toggle_ai" {
		t.Fatalf("expected recording toggle_ai, got %q", m.recordingShortcut)
	}
	tt.Key(ui.Cmd, ui.KeyF5)
	if got := EffectiveShortcut(*state, "toggle_ai"); got != "cmd-f5" {
		t.Fatalf("expected toggle_ai=cmd-f5, got %q", got)
	}
	if state.CustomShortcuts["toggle_ai"] != "cmd-f5" {
		t.Fatalf("expected CustomShortcuts[toggle_ai]=cmd-f5, got %+v", state.CustomShortcuts)
	}

	// Expanded key names verification.
	for tcKey, want := range map[ui.Key]string{
		ui.KeyBackslash: "\\",
		ui.KeySemicolon: ";",
		ui.KeyQuote:     "'",
		ui.KeyBackquote: "`",
		ui.KeyBackspace: "backspace",
		ui.KeyF12:       "f12",
	} {
		if got := shortcutKeyName(tcKey); got != want {
			t.Fatalf("shortcutKeyName(%v) = %q, want %q", tcKey, got, want)
		}
	}

	// Conflict resolution: record "cmd-k" (currently search's default) onto toggle_ai.
	tt.Frame()
	if err := tt.Click("⌘F5"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyK)
	if got := EffectiveShortcut(*state, "toggle_ai"); got != "cmd-k" {
		t.Fatalf("expected toggle_ai=cmd-k after conflict, got %q", got)
	}
	if got := EffectiveShortcut(*state, "search"); got == "cmd-k" {
		t.Fatalf("expected search reassigned away from cmd-k on conflict, still %q", got)
	}
	tt.Frame()
	if _, ok := internalui.TargetBounds("settings-shortcut-conflict-notice"); !ok {
		t.Fatalf("expected conflict notice probe rendered")
	}

	// Also customize quick_note and verify reset clears all 3 fields.
	if err := tt.Click("⌥N"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	if state.ShortcutQuick != "cmd-\\" {
		t.Fatalf("expected ShortcutQuick=cmd-\\, got %q", state.ShortcutQuick)
	}

	if err := tt.Click("恢复默认"); err != nil {
		t.Fatal(err)
	}
	if state.ShortcutSearch != "" || state.ShortcutQuick != "" || len(state.CustomShortcuts) != 0 {
		t.Fatalf("expected reset to clear all custom shortcuts, got search=%q quick=%q custom=%+v",
			state.ShortcutSearch, state.ShortcutQuick, state.CustomShortcuts)
	}
	if EffectiveShortcut(*state, "toggle_ai") != "cmd-j" || EffectiveShortcut(*state, "search") != "cmd-k" {
		t.Fatalf("expected defaults restored after reset")
	}
}
