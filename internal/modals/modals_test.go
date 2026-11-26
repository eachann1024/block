package modals

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
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

func (h *stubHost) ActiveNotebook() string { return h.activeNotebook }
func (h *stubHost) Notebooks() []string    { return h.notebooks }
func (h *stubHost) ActiveNotePath() string { return h.activeNote }
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

	// Switch to Folders tab, toggle show-non-md and add exact rule.
	if err := tt.Click("Folders"); err != nil {
		t.Fatal(err)
	}
	if r, ok := internalui.TargetBounds("settings-show-non-md-pill"); ok {
		tt.ClickAt(r.X+r.W*0.5, r.Y+r.H*0.5)
	} else {
		t.Fatal("missing settings-show-non-md-pill probe")
	}
	if state.HideNonMarkdownFiles == nil || *state.HideNonMarkdownFiles {
		t.Fatalf("expected HideNonMarkdownFiles to toggle false when Show Non-Markdown Files is turned On")
	}
	if err := tt.Click("Add Hidden Folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Folder Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("drafts")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(state.FolderRules) != 1 || state.FolderRules[0].Pattern != "drafts" {
		t.Fatalf("expected drafts folder rule, got %+v", state.FolderRules)
	}

	// Switch to Shortcuts tab, start recording, record Cmd+Shift+L, then verify.
	if err := tt.Click("Shortcuts"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(formatShortcutLabel(defaultQuickShortcut)); err != nil {
		t.Fatal(err)
	}
	if !m.IsRecordingShortcut() {
		t.Fatalf("expected IsRecordingShortcut() == true")
	}
	// ui.Cmd is Command on macOS and Ctrl elsewhere; both are stored as "cmd".
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
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
	// The real upload needs the network; the hook stands in for it.
	m.FeedbackSubmitFn = func(category, message, contact string) error { return nil }
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
	if err := tt.Click("全部"); err != nil {
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
	probes := []string{"quick-slot-0", "quick-slot-1", "quick-slot-2", "quick-save", "save-to-note", "quick-close", "quick-note-editor"}
	for _, probeID := range probes {
		if _, ok := internalui.TargetBounds(probeID); !ok {
			t.Fatalf("expected probe %q recorded", probeID)
		}
	}

	// Wheel inside editor body (y=260) never switches slots; only wheel over the header (y<=44) switches slots.
	if q.Session().CanScrollVertically() {
		t.Fatalf("expected short slot not to report vertical overflow")
	}
	tt.Wheel(220, 260, 0, 24)
	if q.ActiveSlot() != 0 {
		t.Fatalf("expected vertical wheel on editor body NOT to switch slot, got %d", q.ActiveSlot())
	}
	tt.Wheel(220, 260, 24, 0)
	if q.ActiveSlot() != 0 {
		t.Fatalf("expected horizontal/Shift wheel on editor body NOT to switch slot, got %d", q.ActiveSlot())
	}
	tt.Wheel(40, 22, 0, 24)
	if q.ActiveSlot() != 1 {
		t.Fatalf("expected wheel on header blank area to advance to slot 1, got %d", q.ActiveSlot())
	}
	time.Sleep(170 * time.Millisecond)
	tt.Wheel(220, 22, 0, -24)
	if q.ActiveSlot() != 0 {
		t.Fatalf("expected upward wheel on header pager to return to slot 0, got %d", q.ActiveSlot())
	}
	time.Sleep(170 * time.Millisecond)

	// Direct OnWheelScroll threshold & cooldown checks.
	if got := q.OnWheelScroll(8); got != 0 {
		t.Fatalf("expected partial +8 delta not to switch yet, got slot %d", got)
	}
	if got := q.OnWheelScroll(8); got != 1 || q.ActiveSlot() != 1 {
		t.Fatalf("expected +16 accumulated delta to advance to slot 1, got %d", got)
	}
	if got := q.OnWheelScroll(24); got != 1 {
		t.Fatalf("expected 160ms cooldown to suppress immediate switch, got %d", got)
	}
	time.Sleep(170 * time.Millisecond)
	if got := q.OnWheelScroll(-20); got != 0 || q.ActiveSlot() != 0 {
		t.Fatalf("expected negative DY after cooldown to move back to slot 0, got %d", got)
	}

	// Quick Note retains focused empty-line hint band & compact "/" keycap while omitting 56px side-handle gutter.
	q.SetText("")
	internalui.ClearTargets()
	tt.Frame()
	for _, expectedProbe := range []string{"empty-line-band", "empty-hint-menu-keys"} {
		if _, ok := internalui.TargetBounds(expectedProbe); !ok {
			t.Fatalf("expected probe %q rendered in Quick Note", expectedProbe)
		}
	}

	q.SetText("# Heading\nBody paragraph")
	internalui.ClearTargets()
	tt.Frame()
	doc := q.Session().Doc()
	if len(doc.Blocks) < 2 {
		t.Fatalf("expected 2 blocks in quick note, got %d", len(doc.Blocks))
	}
	headingID := doc.Blocks[0].ID
	if _, ok := internalui.TargetBounds(fmt.Sprintf("block-grip-%d", headingID)); ok {
		t.Fatalf("expected 56px side-handle grip omitted in compact Quick Note")
	}
	q.Session().SelectAllBlocks()
	if !q.Session().IsSelectionToolbarOpen() {
		t.Fatalf("expected selection formatting toolbar enabled in Quick Note")
	}

	// QuickEditorZoom 10% step shortcuts: Cmd+= (1.1 -> 1.2), Cmd+- (1.1), Cmd+0 (1.0).
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Key(ui.Cmd, ui.KeyMinus)
	tt.Key(ui.Cmd, ui.Key0)
	if len(zoomChanges) != 4 || zoomChanges[0] != 1.1 || zoomChanges[1] != 1.2 || zoomChanges[2] != 1.1 || zoomChanges[3] != 1.0 {
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
	internalui.ResetSmoke()
	internalui.EnableSmokeMode()
	defer internalui.DisableSmokeMode()

	m, _, state := setupManager(t)
	defs := AllShortcutDefaults()
	for _, k := range []string{"global_summon", "quick_note", "search", "new_note", "toggle_sidebar", "toggle_ai", "export_image"} {
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

	// Verify exact 6-row Shortcuts ordering from top to bottom (performance extracted to plugin):
	wantShortcutOrder := []string{
		"settings-rec-search",
		"settings-rec-quick",
		"settings-keycap-search",
		"settings-keycap-find",
		"settings-keycap-new",
		"settings-keycap-ai",
	}
	var prevY float32 = -1
	for _, id := range wantShortcutOrder {
		b, ok := internalui.TargetBounds(id)
		if !ok {
			t.Fatalf("missing shortcut row probe %q", id)
		}
		if b.Y <= prevY {
			t.Fatalf("shortcut row %q out of vertical order: Y=%v <= prevY=%v", id, b.Y, prevY)
		}
		prevY = b.Y
	}
	for _, extraID := range []string{"settings-rec-sidebar", "settings-rec-export"} {
		if _, ok := internalui.TargetBounds(extraID); ok {
			t.Fatalf("unexpected non-reference shortcut row %q rendered", extraID)
		}
	}

	// Record a custom shortcut for global_summon.
	if err := tt.Click(formatShortcutLabel(defaultSearchShortcut)); err != nil {
		t.Fatal(err)
	}
	if !m.IsRecordingShortcut() || m.recordingShortcut != "global_summon" {
		t.Fatalf("expected recording global_summon, got %q", m.recordingShortcut)
	}
	// ui.Cmd is Command on macOS and Ctrl elsewhere; both are stored as cmd-f5.
	tt.Key(ui.Cmd, ui.KeyF5)
	if got := EffectiveShortcut(*state, "global_summon"); got != "cmd-f5" {
		t.Fatalf("expected global_summon=cmd-f5, got %q", got)
	}
	if state.ShortcutSearch != "cmd-f5" {
		t.Fatalf("expected ShortcutSearch=cmd-f5, got %q", state.ShortcutSearch)
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

	// Conflict resolution: record "alt-n" (currently quick_note's default) onto global_summon.
	tt.Frame()
	if err := tt.Click(formatShortcutLabel("cmd-f5")); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Alt, ui.KeyN)
	if got := EffectiveShortcut(*state, "global_summon"); got != "alt-n" {
		t.Fatalf("expected global_summon=alt-n after conflict, got %q", got)
	}
	if got := EffectiveShortcut(*state, "quick_note"); got != "cmd-f5" {
		t.Fatalf("expected quick_note reassigned to cmd-f5 on conflict, got %q", got)
	}
	tt.Frame()
	if _, ok := internalui.TargetBounds("settings-shortcut-conflict-notice"); !ok {
		t.Fatalf("expected conflict notice probe rendered")
	}

	// Also customize quick_note to cmd-\ and verify reset clears all custom fields.
	if err := tt.Click(formatShortcutLabel("cmd-f5")); err != nil {
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
	if EffectiveShortcut(*state, "global_summon") != "cmd-shift-k" || EffectiveShortcut(*state, "quick_note") != "alt-n" {
		t.Fatalf("expected defaults restored after reset")
	}
}

func TestFontPickerSortOrderAndFoldersListOrder(t *testing.T) {
	internalui.ResetSmoke()
	internalui.EnableSmokeMode()
	defer internalui.DisableSmokeMode()

	m, host, state := setupManager(t)

	// 1. Font Picker sorting: Body = A->Z across all non-dot fonts; Code = monospace first, then non-monospace A->Z, NO 80-item truncation.
	var synthetic []ui.FontFamily
	synthetic = append(synthetic,
		ui.FontFamily{Name: ".AppleSystemUIFont", Monospaced: false},
		ui.FontFamily{Name: "Zebra Sans", Monospaced: false},
		ui.FontFamily{Name: "JetBrains Mono", Monospaced: false}, // matched by name heuristic
		ui.FontFamily{Name: "Avenir Next", Monospaced: false},
		ui.FontFamily{Name: "Courier New", Monospaced: true},
	)
	for i := 0; i < 95; i++ {
		synthetic = append(synthetic, ui.FontFamily{Name: filepath.Base(t.TempDir()) + "-" + string(rune('a'+(i%26))) + string(rune('0'+(i/26)))})
	}
	m.fontFamilies = synthetic

	m.fontPickerOpen = "body"
	m.fontQuery = ""
	bodyFonts := m.filteredFonts()
	if len(bodyFonts) <= 85 {
		t.Fatalf("expected full body font catalog without 80-item cap, got %d", len(bodyFonts))
	}
	for i := 1; i < len(bodyFonts); i++ {
		if strings.HasPrefix(bodyFonts[i].Name, ".") {
			t.Fatalf("unexpected hidden dot-font in catalog: %q", bodyFonts[i].Name)
		}
		if strings.ToLower(bodyFonts[i-1].Name) > strings.ToLower(bodyFonts[i].Name) {
			t.Fatalf("body fonts not sorted A->Z at %d: %q > %q", i, bodyFonts[i-1].Name, bodyFonts[i].Name)
		}
	}

	m.fontPickerOpen = "code"
	codeFonts := m.filteredFonts()
	if len(codeFonts) != len(bodyFonts) {
		t.Fatalf("expected code font catalog to retain non-mono fonts after mono fonts, got %d vs %d", len(codeFonts), len(bodyFonts))
	}
	seenNonMono := false
	for i, f := range codeFonts {
		isMono := isMonospaceFontName(f.Name)
		if !isMono {
			seenNonMono = true
		} else if seenNonMono {
			t.Fatalf("monospace font %q appeared after non-monospace font at index %d", f.Name, i)
		}
	}

	// 2. Folders tab: active notebook FIRST with "当前", secondary notebook SECOND with remove button.
	nbSecondary := filepath.Join(t.TempDir(), "archive-notes")
	if err := os.MkdirAll(nbSecondary, 0o755); err != nil {
		t.Fatal(err)
	}
	nbActive := host.activeNotebook
	// Deliberately put nbSecondary first in Notebooks slice to verify ActiveNotebook still sorts FIRST!
	host.notebooks = []string{nbSecondary, nbActive}
	state.Notebooks = []string{nbSecondary, nbActive}
	m.fontPickerOpen = ""
	m.OpenSettings("folders")

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderSettings(c)
		})
	}, 1000, 820)

	orderedNBs := m.knownNotebooks(*state)
	if len(orderedNBs) != 2 || orderedNBs[0] != nbActive || orderedNBs[1] != nbSecondary {
		t.Fatalf("expected active notebook first in knownNotebooks, got %v", orderedNBs)
	}
	if _, ok := internalui.TargetBounds("folder-hide-non-md"); !ok {
		t.Fatal("missing folder-hide-non-md probe")
	}
	clickProbe := func(id string) {
		t.Helper()
		r, ok := internalui.TargetBounds(id)
		if !ok {
			t.Fatalf("missing %s probe", id)
		}
		tt.ClickAt(r.X+r.W*0.5, r.Y+r.H*0.5)
		tt.Frame()
	}
	// Every notebook is searchable by default; excluding one goes through the add menu.
	clickProbe("settings-ignore-search-add")
	clickProbe("settings-ignore-search-opt-0")
	if !slices.Contains(state.IgnoredSearchFolders, nbActive) {
		t.Fatalf("expected active notebook excluded from search, got %v", state.IgnoredSearchFolders)
	}
	if _, ok := internalui.TargetBounds("folder-search-ignore-0"); !ok {
		t.Fatal("missing folder-search-ignore-0 chip after excluding a notebook")
	}
	if !tt.HasText("隐藏的文件夹") || !tt.HasText("不参与搜索的文件夹") {
		t.Fatal("missing expected Folders tab labels (隐藏的文件夹 / 不参与搜索的文件夹)")
	}
}

func TestDefaultBodyFontShowsFirstFontWithMutedDefaultSuffix(t *testing.T) {
	internalui.ResetSmoke()
	internalui.EnableSmokeMode()
	defer internalui.DisableSmokeMode()

	m, _, state := setupManager(t)
	theme.SetLanguage(model.LangZh)
	theme.SetTypography("", "Menlo", model.LineHeightNormal)
	state.EditorFont = ""
	m.fontFamilies = []ui.FontFamily{
		{Name: "Avenir Next"},
		{Name: "HarmonyOS Sans SC"},
		{Name: "Menlo", Monospaced: true},
	}
	m.OpenSettings("appearance")

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderSettings(c)
		})
	}, 1000, 820)

	primary := defaultBodyFontPrimary(m.fontFamilies)
	if strings.Contains(primary, "+") || strings.Contains(primary, "HarmonyOS") {
		t.Fatalf("expected only primary font name without fallback chain, got %q", primary)
	}
	if !tt.HasText(primary) || !tt.HasText("（默认）") {
		t.Fatalf("expected default body font trigger to render %q and muted '（默认）'", primary)
	}
	if tt.HasText("HarmonyOS Sans SC") {
		t.Fatal("did not expect fallback font 'HarmonyOS Sans SC' in closed trigger label")
	}

	b, ok := internalui.TargetBounds("settings-body-font-trigger")
	if !ok {
		t.Fatal("missing settings-body-font-trigger bounds")
	}
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	tt.Frame()
	if m.fontPickerOpen != "body" {
		t.Fatal("expected body font picker popover to open")
	}
	if full := defaultBodyFontLabel(m.fontFamilies); full != primary+"（默认）" {
		t.Fatalf("unexpected default body font label in zh: %q", full)
	}
}

func TestSearchTabTogglesCurrentAndAllWithShortcutPlaceholder(t *testing.T) {
	internalui.ResetSmoke()
	internalui.EnableSmokeMode()
	defer internalui.DisableSmokeMode()

	theme.SetLanguage(model.LangZh)
	m, host, state := setupManager(t)

	nbWork := filepath.Join(t.TempDir(), "work")
	nbStudy := filepath.Join(t.TempDir(), "study")
	for _, dir := range []string{nbWork, nbStudy} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nbCurrent := host.activeNotebook
	host.notebooks = []string{nbCurrent, nbWork, nbStudy}
	state.Notebooks = []string{nbCurrent, nbWork, nbStudy}

	m.OpenSearch(false)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			m.RenderModals(c)
		})
	}, 900, 680)

	st := m.searchState()
	if !cleanPathEqual(st.scopeFolder, nbCurrent) {
		t.Fatalf("expected default scope to be current notebook %q, got %q", nbCurrent, st.scopeFolder)
	}
	if got := st.searchPlaceholderParts(nbCurrent); got != (searchPlaceholderSpec{prefix: "搜索标题与正文，按", keycap: "Tab", suffix: "搜全部"}) {
		t.Fatalf("unexpected current-notebook placeholder parts: %+v", got)
	}
	if !tt.HasText("Tab") || !tt.HasText("搜全部") {
		t.Fatalf("expected inline Tab keycap placeholder rendered, texts=%v", tt.Texts())
	}
	if tt.HasText("全部文件夹") || tt.HasText("当前 / 全部") || tt.HasText("切换文件夹") ||
		tt.HasText("输入关键词搜索标题与正文") || tt.HasText("输入关键词搜索全部笔记的标题与正文") {
		t.Fatalf("did not expect legacy folder, footer Tab hint, or empty-query prompt text, texts=%v", tt.Texts())
	}
	if emptyRect, ok := internalui.TargetBounds("search-empty"); !ok || emptyRect.H < searchEmptyHeight-1 {
		t.Fatalf("expected generous reserved empty body height >= %.0f, got ok=%v rect=%+v", searchEmptyHeight, ok, emptyRect)
	}

	// Verify header icon, input, placeholder, and scope button share the same vertical center.
	iconRect, okIcon := internalui.TargetBounds("search-header-icon")
	inRect, okIn := internalui.TargetBounds("search-input")
	phRect, okPH := internalui.TargetBounds("search-placeholder")
	trigRect, okTrig := internalui.TargetBounds("search-scope-trigger")
	if !okIcon || !okIn || !okPH || !okTrig {
		t.Fatalf("expected header probes recorded: icon=%v in=%v ph=%v trig=%v", okIcon, okIn, okPH, okTrig)
	}
	refMid := trigRect.Y + trigRect.H/2
	for name, r := range map[string]ui.Rect{
		"search-header-icon": iconRect,
		"search-input":       inRect,
		"search-placeholder": phRect,
	} {
		mid := r.Y + r.H/2
		if math.Abs(float64(mid-refMid)) > 1.0 {
			t.Fatalf("%s mid=%.2f differs from scope trigger mid=%.2f (rect=%+v vs %+v)", name, mid, refMid, r, trigRect)
		}
	}

	// Tab toggles current notebook -> All (never cycling to nbWork or nbStudy).
	tt.Key(0, ui.KeyTab)
	if st.scopeFolder != "" || !m.searchAllNB {
		t.Fatalf("expected first Tab to switch to All, got scope=%q all=%v", st.scopeFolder, m.searchAllNB)
	}
	if got := st.scopeLabel(); got != "全部" {
		t.Fatalf("expected All scope label '全部', got %q", got)
	}
	if got := st.searchPlaceholderParts(nbCurrent); got != (searchPlaceholderSpec{prefix: "搜索全部，按", keycap: "Tab", suffix: "切回当前"}) {
		t.Fatalf("unexpected All placeholder parts: %+v", got)
	}

	// Second Tab toggles All -> current notebook.
	tt.Key(0, ui.KeyTab)
	if !cleanPathEqual(st.scopeFolder, nbCurrent) || m.searchAllNB {
		t.Fatalf("expected second Tab to return to current notebook %q, got %q", nbCurrent, st.scopeFolder)
	}

	// Shift+Tab also toggles current notebook <-> All.
	tt.Key(ui.Shift, ui.KeyTab)
	if st.scopeFolder != "" {
		t.Fatalf("expected Shift+Tab to switch to All, got %q", st.scopeFolder)
	}
	tt.Key(ui.Shift, ui.KeyTab)
	if !cleanPathEqual(st.scopeFolder, nbCurrent) {
		t.Fatalf("expected second Shift+Tab to return to current notebook %q, got %q", nbCurrent, st.scopeFolder)
	}

	// Picking another notebook via dropdown then pressing Tab switches to All, then back to current notebook.
	if !m.selectScopeIndex(2) || !cleanPathEqual(st.scopeFolder, nbWork) {
		t.Fatalf("expected dropdown selection to pick %q, got %q", nbWork, st.scopeFolder)
	}
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	if st.scopeFolder != "" {
		t.Fatalf("expected Tab from non-current notebook to switch to All, got %q", st.scopeFolder)
	}
	tt.Key(0, ui.KeyTab)
	if !cleanPathEqual(st.scopeFolder, nbCurrent) {
		t.Fatalf("expected next Tab from All to switch to current notebook %q, got %q", nbCurrent, st.scopeFolder)
	}

	// English labels and fallback placeholder when no active notebook exists.
	theme.SetLanguage(model.LangEn)
	defer theme.SetLanguage(model.LangZh)
	if got := st.searchPlaceholderParts(nbCurrent); got != (searchPlaceholderSpec{prefix: "Search titles & content,", keycap: "Tab", suffix: "for all"}) {
		t.Fatalf("unexpected en current placeholder parts: %+v", got)
	}
	tt.Key(0, ui.KeyTab)
	if got := st.scopeLabel(); got != "All" {
		t.Fatalf("expected en All scope label 'All', got %q", got)
	}
	if got := st.searchPlaceholderParts(nbCurrent); got != (searchPlaceholderSpec{prefix: "Search all,", keycap: "Tab", suffix: "for current"}) {
		t.Fatalf("unexpected en All placeholder parts: %+v", got)
	}
	if got := st.searchPlaceholderParts(""); got != (searchPlaceholderSpec{prefix: "Search titles and content"}) {
		t.Fatalf("unexpected en fallback placeholder without active notebook: %+v", got)
	}
}
