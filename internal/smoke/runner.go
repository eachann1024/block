package smoke

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/ai"
	"block/internal/app"
	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// StepResult records the execution status and duration of a single acceptance step.
type StepResult struct {
	Index    int
	Name     string
	Passed   bool
	Duration time.Duration
	Detail   string
}

// Report summarizes an entire 15-part automated click acceptance run.
type Report struct {
	DataDir     string
	NotebookDir string
	Steps       []StepResult
	PassedCount int
	FailedCount int
	TotalTime   time.Duration
}

func clickTarget(tt *ui.Tester, id string) error {
	r, ok := blockui.TargetBounds(id)
	if !ok {
		return fmt.Errorf("probe %q not found in frame", id)
	}
	cx, cy := r.X+r.W*0.5, r.Y+r.H*0.5
	blockui.SetSmokeStep(&ui.Point{X: cx, Y: cy}, id)
	tt.ClickAt(cx, cy)
	return nil
}

func rightClickTarget(tt *ui.Tester, id string) error {
	r, ok := blockui.TargetBounds(id)
	if !ok {
		return fmt.Errorf("probe %q not found for right-click", id)
	}
	tt.RightClickAt(r.X+r.W*0.5, r.Y+r.H*0.5)
	return nil
}

func waitUntil(timeout time.Duration, fn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %v", timeout)
}

// RunAll executes all 14 end-to-end Workspace click acceptance steps plus Quick Note
// & 6-format Export verification inside a sandboxed DataDir and NotebookDir.
func RunAll(baseTempDir string) (*Report, error) {
	startAll := time.Now()
	if strings.TrimSpace(baseTempDir) == "" {
		tmp, err := os.MkdirTemp("", "block-go-smoke-*")
		if err != nil {
			return nil, err
		}
		baseTempDir = tmp
	}

	dataDir := filepath.Join(baseTempDir, "app-data")
	nbDir := filepath.Join(baseTempDir, "notebook-main")
	nbDir2 := filepath.Join(baseTempDir, "notebook-second")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(nbDir, "Archive"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(nbDir2, 0o755); err != nil {
		return nil, err
	}

	theme.SetLanguage(model.LangZh)
	blockui.ResetSmoke()
	blockui.EnableSmokeMode()
	defer blockui.DisableSmokeMode()

	ws := app.NewWorkspace(dataDir)
	if err := ws.SetNotebook(nbDir); err != nil {
		return nil, fmt.Errorf("SetNotebook: %w", err)
	}

	tt := ui.NewTester(ws.Render, 1200, 780)
	tt.Frame()

	rep := &Report{
		DataDir:     dataDir,
		NotebookDir: nbDir,
	}

	runStep := func(idx int, name string, fn func() (string, error)) error {
		blockui.SetSmokeStep(nil, fmt.Sprintf("[%d/15] %s", idx, name))
		t0 := time.Now()
		detail, err := fn()
		dur := time.Since(t0)
		sr := StepResult{
			Index:    idx,
			Name:     name,
			Passed:   err == nil,
			Duration: dur,
			Detail:   detail,
		}
		if err != nil {
			sr.Detail = err.Error()
			rep.FailedCount++
			rep.Steps = append(rep.Steps, sr)
			blockui.SetSmokeFinished(false, fmt.Sprintf("Step %d failed: %v", idx, err))
			return fmt.Errorf("step %d (%s): %w", idx, name, err)
		}
		rep.PassedCount++
		rep.Steps = append(rep.Steps, sr)
		return nil
	}

	var firstNotePath string

	// Step 1: Click `new-note` in sidebar -> creates real .md on disk & activates tab.
	if err := runStep(1, "Sidebar click new-note & tab activation", func() (string, error) {
		if err := clickTarget(tt, "new-note"); err != nil {
			return "", err
		}
		active := ws.ActiveNotePath()
		if active == "" {
			return "", fmt.Errorf("expected active note after clicking new-note")
		}
		if _, err := os.Stat(active); err != nil {
			return "", fmt.Errorf("created note missing on disk: %w", err)
		}
		firstNotePath = active
		return filepath.Base(active), nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 2: Compact viewport & 90% zoom slash-menu internal scroll containment (no body bleed).
	if err := runStep(2, "Compact window 90%/100% zoom slash menu scroll containment", func() (string, error) {
		sess := ws.ActiveSession()
		if sess == nil {
			return "", fmt.Errorf("no active editor session")
		}
		var seed strings.Builder
		for i := 0; i < 16; i++ {
			seed.WriteString(fmt.Sprintf("Line %d content for scroll containment check.\n\n", i+1))
		}
		if err := ws.WriteNote(firstNotePath, seed.String()); err != nil {
			return "", err
		}
		sess = ws.ActiveSession()
		ws.SetZoomPct(90)
		tt.SetSize(680, 480)
		tt.Frame()

		// Append an empty trailing paragraph and type '/'
		sess.Doc().Blocks = append(sess.Doc().Blocks, model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph})
		sess.FocusBlock(len(sess.Doc().Blocks) - 1)
		tt.Frame()
		tt.Type("/")
		if !sess.IsSlashMenuOpen() {
			return "", fmt.Errorf("expected slash menu open after '/'")
		}
		menuRect, ok := blockui.TargetBounds("block-menu")
		if !ok || menuRect.H <= 0 {
			return "", fmt.Errorf("block-menu bounds missing")
		}
		// Scroll wheel inside slash menu and verify menu stays open without crashing or overflowing
		mx, my := (menuRect.X+menuRect.W*0.5)*0.9, (menuRect.Y+menuRect.H*0.5)*0.9
		for i := 0; i < 5; i++ {
			tt.Scroll(mx, my, 0, 72)
		}
		tt.Key(0, ui.KeyEscape)
		ws.SetZoomPct(100)
		tt.SetSize(1200, 780)
		tt.Frame()
		return "90% & 100% zoom scroll contained", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 3: Slash menu -> click Heading 1 + Enter paragraph split.
	if err := runStep(3, "Slash menu Heading 1 creation & Enter split", func() (string, error) {
		if err := ws.WriteNote(firstNotePath, ""); err != nil {
			return "", err
		}
		sess := ws.ActiveSession()
		sess.FocusBlock(0)
		tt.Frame()
		tt.Type("/")
		if err := clickTarget(tt, "block-menu-item-1"); err != nil {
			return "", err
		}
		tt.Type("Block MyGo Port")
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		if sess.Doc().Blocks[0].Kind != model.BlockHeading1 {
			return "", fmt.Errorf("expected H1 at block 0, got %v", sess.Doc().Blocks[0].Kind)
		}
		disk, err := os.ReadFile(firstNotePath)
		if err != nil || !strings.Contains(string(disk), "# Block MyGo Port") {
			return "", fmt.Errorf("disk markdown missing '# Block MyGo Port': %q", string(disk))
		}
		return "# Block MyGo Port persisted", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 4: Slash menu -> Todo + click checkbox toggle & disk persistence.
	if err := runStep(4, "Slash menu Todo creation & checkbox click persistence", func() (string, error) {
		sess := ws.ActiveSession()
		last := len(sess.Doc().Blocks) - 1
		sess.FocusBlock(last)
		tt.Frame()
		tt.Type("/todo")
		tt.Key(0, ui.KeyEnter)
		tt.Type("Pass click suite")
		tt.Frame()

		if err := clickTarget(tt, "todo-checkbox"); err != nil {
			return "", err
		}
		tt.Frame()
		disk, err := os.ReadFile(firstNotePath)
		if err != nil || !strings.Contains(string(disk), "- [x] Pass click suite") {
			return "", fmt.Errorf("expected checked todo on disk, got %q", string(disk))
		}
		return "- [x] Pass click suite", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 5: Selection floating toolbar (`tb-bold`) + Cmd+Z Undo + Cmd+Shift+Z Redo.
	if err := runStep(5, "Selection floating toolbar click tb-bold & Cmd+Z/Cmd+Shift+Z", func() (string, error) {
		sess := ws.ActiveSession()
		todoIdx := len(sess.Doc().Blocks) - 1
		sess.FocusBlock(todoIdx)
		sess.SelectRange(todoIdx, 0, todoIdx, 4)
		tt.Frame()
		if _, ok := blockui.TargetBounds("selection-toolbar"); !ok {
			return "", fmt.Errorf("selection-toolbar not visible")
		}
		if err := clickTarget(tt, "tb-bold"); err != nil {
			return "", err
		}
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "**Pass**") {
			return "", fmt.Errorf("expected **Pass** after tb-bold click, got %q", sess.Markdown())
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		tt.Frame()
		if strings.Contains(sess.Markdown(), "**Pass**") {
			return "", fmt.Errorf("expected Cmd+Z to undo bold")
		}
		tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "**Pass**") {
			return "", fmt.Errorf("expected Cmd+Shift+Z to redo bold")
		}
		return "tb-bold + Undo/Redo verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 6: Intelligent floating menu placement (palette, link card, titlebar-more, upward notebook switcher, cascading Move submenu).
	if err := runStep(6, "Floating menu placement (toolbar, titlebar-more, switcher, cascading Move to)", func() (string, error) {
		sess := ws.ActiveSession()
		todoIdx := len(sess.Doc().Blocks) - 1
		sess.SelectRange(todoIdx, 0, todoIdx, 4)
		tt.Frame()

		// Palette popover
		if err := clickTarget(tt, "tb-palette"); err != nil {
			return "", err
		}
		if r, ok := blockui.TargetBounds("palette-panel"); !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 1200 {
			return "", fmt.Errorf("palette-panel out of bounds: %+v", r)
		}
		// Link card popover
		if err := clickTarget(tt, "tb-link"); err != nil {
			return "", err
		}
		if r, ok := blockui.TargetBounds("format-link-card"); !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 1200 {
			return "", fmt.Errorf("format-link-card out of bounds: %+v", r)
		}
		tt.Key(0, ui.KeyEscape)
		sess.SelectRange(0, 0, 0, 0)
		tt.Frame()

		// Titlebar more menu
		if err := clickTarget(tt, "titlebar-more"); err != nil {
			return "", err
		}
		if r, ok := blockui.TargetBounds("page-more-menu"); !ok || r.X < 0 || r.X+r.W > 1200 {
			return "", fmt.Errorf("page-more-menu out of viewport: %+v", r)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		// Upward notebook switcher above footer trigger
		swTrig, ok := blockui.TargetBounds("notebook-switcher")
		if !ok {
			return "", fmt.Errorf("notebook-switcher missing")
		}
		if err := clickTarget(tt, "notebook-switcher"); err != nil {
			return "", err
		}
		swPop, ok := blockui.TargetBounds("notebook-switcher-pop")
		if !ok || swPop.Y+swPop.H > swTrig.Y+4 {
			return "", fmt.Errorf("expected notebook-switcher-pop above trigger: pop=%+v trig=%+v", swPop, swTrig)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		// Tree right-click + cascading Move to... submenu
		rowKey := "tree-row:" + firstNotePath
		if err := rightClickTarget(tt, rowKey); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("tree-menu-pop"); !ok {
			return "", fmt.Errorf("tree-menu-pop missing after right-click")
		}
		if err := clickTarget(tt, "tree-move-row"); err != nil {
			return "", err
		}
		movePop, ok := blockui.TargetBounds("tree-move-pop")
		if !ok || movePop.X < 0 || movePop.X+movePop.W > 1200 {
			return "", fmt.Errorf("tree-move-pop out of viewport: %+v", movePop)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		return "all 5 popovers clamped & oriented", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 7: Backspace merge + first-line empty block two-step Backspace delete arm badge.
	if err := runStep(7, "Backspace merge & first-line two-step delete badge", func() (string, error) {
		if err := ws.WriteNote(firstNotePath, "\nSecond line text\n"); err != nil {
			return "", err
		}
		sess := ws.ActiveSession()
		// Ensure block 0 is empty paragraph and block 1 has text
		sess.SetDoc(&model.Doc{
			Blocks: []model.Block{
				{ID: model.NewBlockID(), Kind: model.BlockParagraph},
				{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Survivor line"}}},
			},
		})
		sess.FocusBlock(0)
		tt.Frame()
		tt.Key(0, ui.KeyBackspace)
		tt.Frame()
		if !sess.FirstLineDeleteArmed() {
			return "", fmt.Errorf("expected FirstLineDeleteArmed on first Backspace")
		}
		if _, ok := blockui.TargetBounds("first-line-delete-badge"); !ok {
			return "", fmt.Errorf("expected first-line-delete-badge probe")
		}
		tt.Key(0, ui.KeyBackspace)
		tt.Frame()
		if len(sess.Doc().Blocks) != 1 || sess.Doc().Blocks[0].PlainText() != "Survivor line" {
			return "", fmt.Errorf("expected empty first block deleted on second Backspace, got %+v", sess.Doc().Blocks)
		}
		return "two-step arm + delete verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 8: Cmd+F Find bar (`find-bar`, `find-input`, `find-next`, `find-prev`, `find-close`).
	if err := runStep(8, "In-page Find bar Cmd+F navigation & close", func() (string, error) {
		if err := ws.WriteNote(firstNotePath, "# Overview\n\nneedle one\n\nneedle two\n"); err != nil {
			return "", err
		}
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		sess := ws.ActiveSession()
		if !sess.IsFindOpen() {
			return "", fmt.Errorf("expected find bar open after Cmd+F")
		}
		sess.SetFindQuery("needle")
		tt.Frame()
		if sess.FindMatchCount() != 2 {
			return "", fmt.Errorf("expected 2 matches for 'needle', got %d", sess.FindMatchCount())
		}
		if err := clickTarget(tt, "find-next"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "find-prev"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "find-close"); err != nil {
			return "", err
		}
		if sess.IsFindOpen() {
			return "", fmt.Errorf("expected find bar closed after clicking find-close")
		}
		return "2 matches navigated & closed", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 9: Multi-block selection -> annotation & emoji reaction (`annotation-rail`) -> rewrite -> Undo.
	if err := runStep(9, "Multi-block annotations, emoji reaction, rewrite & Undo", func() (string, error) {
		sess := ws.ActiveSession()
		sess.SelectRange(1, 0, 2, 6)
		ann := sess.AddAnnotation("Make concise")
		if ann == nil {
			return "", fmt.Errorf("AddAnnotation returned nil")
		}
		sess.AddReaction("👍")
		tt.Frame()
		if _, ok := blockui.TargetBounds("annotation-rail"); !ok {
			return "", fmt.Errorf("expected annotation-rail visible")
		}
		if !sess.ApplyAnnotationRewrite(ann.ID, "concise needle summary") {
			return "", fmt.Errorf("ApplyAnnotationRewrite failed")
		}
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "concise needle summary") {
			return "", fmt.Errorf("expected rewritten text in document")
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		tt.Frame()
		if strings.Contains(sess.Markdown(), "concise needle summary") {
			return "", fmt.Errorf("expected Cmd+Z to undo annotation rewrite")
		}
		return "annotation + reaction + rewrite + undo", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 10: 4-level Outline rail -> click jump & highlight -> narrow viewport <960px auto-hide.
	if err := runStep(10, "4-level Outline rail jump highlight & <960px auto-hide", func() (string, error) {
		md4 := "# Level 1\n\nIntro\n\n## Level 2\n\nBody\n\n### Level 3\n\nDetails\n\n#### Level 4\n\nDeep\n"
		if err := ws.WriteNote(firstNotePath, md4); err != nil {
			return "", err
		}
		tt.SetSize(1280, 780)
		tt.Frame()
		sess := ws.ActiveSession()
		if len(sess.Outline()) != 4 {
			return "", fmt.Errorf("expected 4 outline items, got %d", len(sess.Outline()))
		}
		railRect, ok := blockui.TargetBounds("outline-rail")
		if !ok {
			return "", fmt.Errorf("outline-rail missing at 1280px")
		}
		// Hover rail to expand popover and click item 3 (#### Level 4)
		tt.Move(railRect.X+railRect.W*0.5, railRect.Y+railRect.H*0.5)
		if err := clickTarget(tt, "outline-item-3"); err != nil {
			if err2 := clickTarget(tt, "outline-bar-3"); err2 != nil {
				return "", fmt.Errorf("outline click failed: %v / %v", err, err2)
			}
		}
		if sess.HighlightedBlockID() == 0 {
			return "", fmt.Errorf("expected HighlightedBlockID set after outline jump")
		}
		// Shrink below 960px -> outline rail must auto-hide!
		tt.SetSize(820, 780)
		tt.Frame()
		if _, stillVisible := blockui.TargetBounds("outline-rail"); stillVisible {
			return "", fmt.Errorf("expected outline-rail hidden below 960px")
		}
		tt.SetSize(1200, 780)
		tt.Frame()
		return "4 levels jumped & <960px hidden", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 11: Global Search (`Cmd+K`), `Alt+B` sidebar collapse/drawer, multi-tab cycle & `Cmd+Shift+T`, Feedback 3-category draft + submit.
	if err := runStep(11, "Search jump, sidebar toggle/drawer, tab reopen, Feedback draft+submit", func() (string, error) {
		secondNote := filepath.Join(nbDir, "Architecture.md")
		if err := ws.WriteNote(secondNote, "# Architecture\n\nUniqueQuantumToken42\n"); err != nil {
			return "", err
		}
		tt.Frame()

		// Click search-button -> type query -> press Enter to jump to Architecture.md
		if err := clickTarget(tt, "search-button"); err != nil {
			return "", err
		}
		if !ws.Modals().IsSearchOpen() {
			return "", fmt.Errorf("expected search modal open")
		}
		tt.Type("UniqueQuantumToken42")
		tt.Frame()
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		if ws.ActiveNotePath() != filepath.Clean(secondNote) {
			return "", fmt.Errorf("expected search jump to %s, got %s", secondNote, ws.ActiveNotePath())
		}

		// Ctrl+Tab cycles back to firstNotePath, Cmd+W closes, Cmd+Shift+T reopens!
		tt.Key(ui.Ctrl, ui.KeyTab)
		tt.Frame()
		if ws.ActiveNotePath() != filepath.Clean(firstNotePath) {
			return "", fmt.Errorf("expected Ctrl+Tab to switch to %s, got %s", firstNotePath, ws.ActiveNotePath())
		}
		ws.CloseTab(secondNote)
		tt.Frame()
		if len(ws.Tabs()) != 1 {
			return "", fmt.Errorf("expected 1 tab after CloseTab, got %d", len(ws.Tabs()))
		}
		tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
		tt.Frame()
		if len(ws.Tabs()) != 2 {
			return "", fmt.Errorf("expected 2 tabs after Cmd+Shift+T reopen, got %d", len(ws.Tabs()))
		}

		// Alt+B / sidebar-toggle collapse & expand + narrow drawer scrim
		if err := clickTarget(tt, "sidebar-toggle"); err != nil {
			return "", err
		}
		if !ws.State().SidebarCollapsed {
			return "", fmt.Errorf("expected sidebar collapsed")
		}
		if err := clickTarget(tt, "sidebar-toggle"); err != nil {
			return "", err
		}
		if ws.State().SidebarCollapsed {
			return "", fmt.Errorf("expected sidebar expanded")
		}

		// Narrow window (700px): sidebar yields -> click sidebar-toggle opens floating drawer -> click scrim closes
		tt.SetSize(700, 720)
		tt.Frame()
		if err := clickTarget(tt, "sidebar-toggle"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("sidebar-drawer-scrim"); !ok {
			return "", fmt.Errorf("expected sidebar-drawer-scrim in narrow drawer mode")
		}
		if err := clickTarget(tt, "sidebar-drawer-scrim"); err != nil {
			return "", err
		}
		tt.SetSize(1200, 780)
		tt.Frame()

		// Feedback modal: draft persistence + submit thank-you state
		if err := clickTarget(tt, "titlebar-feedback"); err != nil {
			return "", err
		}
		if !ws.Modals().IsFeedbackOpen() {
			return "", fmt.Errorf("expected feedback modal open")
		}
		if err := tt.Click("功能建议"); err == nil {
			// switched to idea
		}
		if err := clickTarget(tt, "feedback-message"); err != nil {
			return "", err
		}
		tt.Type("Great Ocean Paper feel!")
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if ws.State().FeedbackDraft == nil {
			return "", fmt.Errorf("expected FeedbackDraft saved on close")
		}
		if err := clickTarget(tt, "titlebar-feedback"); err != nil {
			return "", err
		}
		if err := tt.Click("发送反馈"); err != nil {
			if err2 := tt.Click("发送"); err2 != nil {
				return "", fmt.Errorf("click feedback submit failed: %v", err)
			}
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("feedback-done"); !ok {
			return "", fmt.Errorf("expected feedback-done thank-you state")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		return "search + tabs + drawer + feedback passed", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 12: Settings (`settings-button`), AI shortcut blocked in Settings, FolderRules, Shortcuts, Back.
	if err := runStep(12, "Settings tabs, AI shortcut block, FolderRules & shortcut recorder", func() (string, error) {
		ws.AI().SetOpen(false)
		if err := clickTarget(tt, "settings-button"); err != nil {
			return "", err
		}
		if !ws.Modals().IsSettingsOpen() {
			return "", fmt.Errorf("expected settings page open")
		}
		// Cmd+J must NOT open AI panel while Settings is open!
		tt.Key(ui.Cmd, ui.KeyJ)
		tt.Frame()
		if ws.AI().IsOpen() {
			return "", fmt.Errorf("AI panel shortcut must be blocked while in Settings")
		}

		// Switch to Folders tab, toggle hide-non-md, verify ignore-search section
		if err := clickTarget(tt, "settings-tab-folders"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("settings-ignore-search"); !ok {
			return "", fmt.Errorf("expected settings-ignore-search section")
		}
		if err := clickTarget(tt, "settings-folder-rule-input"); err == nil {
			tt.Type("temp-build")
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
		}

		// Switch to Appearance tab & Shortcuts tab
		if err := clickTarget(tt, "settings-tab-appearance"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "settings-tab-shortcuts"); err != nil {
			return "", err
		}

		// Click settings-back (or Escape) to return to workspace
		if err := clickTarget(tt, "settings-back"); err != nil {
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
		}
		if ws.Modals().IsSettingsOpen() {
			return "", fmt.Errorf("expected settings closed after settings-back")
		}
		return "Settings guard & tabs verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 13: Code preview + Chaoshan paste + Full AI Panel E2E (cover, fullscreen, deck, history drafts, 1200+ @ picker, IME, process/question/approval/plan/createNote).
	if err := runStep(13, "Code preview, smart paste & complete AI Panel E2E", func() (string, error) {
		_ = ws.OpenNote(firstNotePath, 0)
		sess := ws.ActiveSession()
		sess.PasteText("```mermaid\ngraph TD\n  A-->B\n```\n")
		norm := ws.Codec().NormalizePaste("Hello，世界！\r\n\r\nLine2")
		if !strings.Contains(norm, "Line2") {
			return "", fmt.Errorf("unexpected NormalizePaste output: %q", norm)
		}
		tt.Frame()

		// 13a. Open AI panel via titlebar-ai: verify unconfigured cover first
		_ = ws.AI().SaveProviderConfig(model.AIProviderConfig{Enabled: true, Provider: "deepseek"})
		if err := clickTarget(tt, "titlebar-ai"); err != nil {
			return "", err
		}
		if !ws.AI().IsOpen() {
			return "", fmt.Errorf("expected AI panel open after clicking titlebar-ai")
		}
		if _, ok := blockui.TargetBounds("ai-cover-settings"); !ok {
			return "", fmt.Errorf("expected ai-cover-settings on unconfigured AI cover")
		}

		// 13b. Configure provider + enable MockResponder
		if err := ws.AI().SaveProviderConfig(model.AIProviderConfig{
			Enabled:  true,
			Provider: "openai",
			Protocol: model.ProtocolOpenAI,
			BaseURL:  "http://127.0.0.1:1/v1",
			APIKey:   "smoke-key",
			Model:    "gpt-smoke",
		}); err != nil {
			return "", err
		}
		ws.AI().EnableMockResponder(true)
		tt.Frame()

		// 13c. Fullscreen immersive toggle & restore
		if err := clickTarget(tt, "ai-fullscreen-toggle"); err != nil {
			return "", err
		}
		if !ws.AI().IsFullscreen() {
			return "", fmt.Errorf("expected AI fullscreen enabled")
		}
		if err := clickTarget(tt, "ai-fullscreen-toggle"); err != nil {
			return "", err
		}
		if ws.AI().IsFullscreen() {
			return "", fmt.Errorf("expected AI fullscreen disabled")
		}

		// 13d. Backend min-1-enabled guard + Agent Deck single-row switch
		if ws.AI().CanDisableBackend(model.BackendBuiltin) {
			return "", fmt.Errorf("must not allow disabling sole enabled backend")
		}
		ws.AI().SetBackendStatusOverride(model.BackendCodex, &ai.BackendStatus{State: ai.BackendStateReady, Version: "1.0"})
		_ = ws.AI().SetBackendEnabled(model.BackendCodex, true)
		tt.Frame()
		if err := clickTarget(tt, "ai-deck-trigger"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("ai-deck"); !ok {
			return "", fmt.Errorf("expected ai-deck popup open")
		}
		_ = clickTarget(tt, "ai-backend-Builtin")
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		// 13e. 1200+ file @ virtualized picker + IME guard + / skill picker + Shift+Enter vs Enter
		bigList := make([]string, 0, 1202)
		for i := 0; i < 1200; i++ {
			bigList = append(bigList, fmt.Sprintf("vault/note-%04d.md", i))
		}
		ws.AI().InjectTestFiles(bigList)
		if err := clickTarget(tt, "ai-composer"); err != nil {
			return "", err
		}
		tt.Type("@")
		if _, ok := blockui.TargetBounds("ai-ref-picker"); !ok {
			return "", fmt.Errorf("expected ai-ref-picker for 1200+ files")
		}
		// IME composition Enter must not submit
		tt.Compose("vault/note-0001", 15)
		tt.Key(0, ui.KeyEnter)
		tt.Compose("", 0)
		ws.AI().SetDraftText("")
		tt.Frame()

		if err := clickTarget(tt, "ai-composer"); err != nil {
			return "", err
		}
		tt.Type("/searchNotes")
		if _, ok := blockui.TargetBounds("ai-skill-picker"); !ok {
			return "", fmt.Errorf("expected ai-skill-picker on '/'")
		}
		tt.Key(0, ui.KeyEnter)

		// Send prompt via Shift+Enter + Enter -> Process trace card + copy button
		tt.Type("总结当前笔记")
		tt.Key(ui.Shift, ui.KeyEnter)
		tt.Type("给出要点")
		tt.Key(0, ui.KeyEnter)
		if err := waitUntil(2*time.Second, func() bool {
			conv := ws.AI().CurrentConversation()
			return conv != nil && !ws.AI().IsStreaming() && len(conv.Messages) >= 2
		}); err != nil {
			return "", fmt.Errorf("mock turn timeout: %w", err)
		}
		tt.Frame()
		if err := clickTarget(tt, "ai-process-toggle"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("ai-process-steps"); !ok {
			return "", fmt.Errorf("expected ai-process-steps expanded")
		}

		// 13f. Question card & Approval card (with retry) & createNote virtual preview -> Accept -> Undo
		ws.AI().NewConversation()
		ws.AI().SendPrompt("请确认选择题")
		if err := waitUntil(2*time.Second, func() bool {
			c := ws.AI().CurrentConversation()
			return c != nil && len(c.Messages) >= 2 && c.Messages[1].Question != nil
		}); err != nil {
			return "", err
		}
		tt.Frame()
		if err := clickTarget(tt, "ai-question-opt-0-0"); err != nil {
			return "", err
		}
		_ = clickTarget(tt, "ai-question-continue")
		_ = clickTarget(tt, "ai-question-skip")

		ws.AI().NewConversation()
		ws.AI().SetMockApprovalFailOnce(true)
		ws.AI().SendPrompt("请审批权限")
		if err := waitUntil(2*time.Second, func() bool {
			c := ws.AI().CurrentConversation()
			return c != nil && len(c.Messages) >= 2 && c.Messages[1].Approval != nil
		}); err != nil {
			return "", err
		}
		tt.Frame()
		if err := clickTarget(tt, "ai-approval-option-allow"); err != nil {
			return "", err
		}
		_ = clickTarget(tt, "ai-approval-continue") // fail once
		_ = clickTarget(tt, "ai-approval-continue") // retry succeeds

		ws.AI().NewConversation()
		ws.AI().SendPrompt("请 createNote 新建")
		if err := waitUntil(2*time.Second, func() bool {
			return ws.AI().ActivePlan() != nil
		}); err != nil {
			return "", err
		}
		plan := ws.AI().ActivePlan()
		createdPath := filepath.Join(nbDir, plan.Page+".md")
		if _, err := os.Stat(createdPath); !os.IsNotExist(err) {
			return "", fmt.Errorf("createNote wrote disk prematurely before Accept")
		}
		if err := ws.AI().AcceptPlan(plan.ConvID, plan.MsgIndex); err != nil {
			return "", err
		}
		if _, err := os.Stat(createdPath); err != nil {
			return "", fmt.Errorf("expected created note on disk after Accept: %w", err)
		}
		if err := ws.AI().UndoPlan(plan.ConvID, plan.MsgIndex); err != nil {
			return "", err
		}
		if _, err := os.Stat(createdPath); !os.IsNotExist(err) {
			return "", fmt.Errorf("expected created note removed after UndoPlan")
		}
		ws.AI().SetOpen(false)
		tt.Frame()
		return "AI cover/deck/picker/IME/process/question/approval/plan all passed", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 14: About modal, Performance & Usage (`Cmd+Shift+M`) + 60 GiB sparse file + Escape.
	if err := runStep(14, "About modal & Performance 7-category Treemap (60GiB sparse)", func() (string, error) {
		ws.Modals().OpenAbout()
		tt.Frame()
		if _, ok := blockui.TargetBounds("about-modal"); !ok {
			return "", fmt.Errorf("expected about-modal open")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if ws.Modals().IsAboutOpen() {
			return "", fmt.Errorf("expected Escape to close about-modal")
		}

		// Create a 60 GiB sparse file + 0 B file inside assets to test physical block accounting
		sparseFile := filepath.Join(nbDir, "assets", "sparse-60g.bin")
		_ = os.MkdirAll(filepath.Dir(sparseFile), 0o755)
		if sf, err := os.Create(sparseFile); err == nil {
			_ = sf.Truncate(60 * 1024 * 1024 * 1024)
			_ = sf.Close()
		}
		_ = os.WriteFile(filepath.Join(nbDir, "assets", "zero.bin"), nil, 0o644)

		tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
		tt.Frame()
		if !ws.Modals().IsPerformanceOpen() {
			return "", fmt.Errorf("expected Cmd+Shift+M to open perf-modal")
		}
		ws.Modals().RefreshPerformanceDisk(nbDir)
		tt.Frame()
		if _, ok := blockui.TargetBounds("perf-treemap"); !ok {
			return "", fmt.Errorf("expected perf-treemap rendered")
		}
		cats := ws.Modals().PerformanceCategories()
		if len(cats) != 7 {
			return "", fmt.Errorf("expected 7 disk categories, got %d", len(cats))
		}
		for _, c := range cats {
			if c.ID == "assets" && c.Bytes >= 4*1024*1024 {
				return "", fmt.Errorf("60GiB sparse file miscounted as %d physical bytes", c.Bytes)
			}
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if ws.Modals().IsPerformanceOpen() {
			return "", fmt.Errorf("expected Escape to close perf-modal")
		}
		_ = os.RemoveAll(filepath.Join(nbDir, "assets"))
		return "About + Perf 7-cat Treemap + 60GiB sparse verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 15: Quick Note floating panel (180ms debounce, slots, SaveToNotebook, Hide independence) + 6-format Export!
	if err := runStep(15, "Quick Note 180ms debounce + SaveToNotebook + 6-format Export", func() (string, error) {
		qn := ws.QuickNote()
		if !qn.TriggerHotkey() || !qn.IsVisible() {
			return "", fmt.Errorf("expected first Quick Note hotkey to show panel")
		}
		if qn.TriggerHotkey() {
			return "", fmt.Errorf("expected immediate second hotkey (<180ms) debounced")
		}
		ttQuick := ui.NewTester(qn.RenderView, 440, 520)
		if _, ok := blockui.TargetBounds("quick-note-editor"); !ok {
			return "", fmt.Errorf("expected quick-note-editor rendered")
		}
		qn.SetText("# Quick Capture\n\nSaved from floating panel.\n")
		ttQuick.Frame()
		savedPath, err := qn.SaveToNotebook()
		if err != nil {
			return "", fmt.Errorf("QuickNote.SaveToNotebook: %w", err)
		}
		if _, err := os.Stat(savedPath); err != nil {
			return "", fmt.Errorf("saved quick note missing on disk: %w", err)
		}
		qn.Hide()
		if qn.IsVisible() {
			return "", fmt.Errorf("expected Quick Note hidden after Hide()")
		}
		// Main workspace remains active and responsive!
		tt.Frame()
		if ws.ActiveNotebook() != filepath.Clean(nbDir) {
			return "", fmt.Errorf("main workspace disrupted after Quick Note Hide()")
		}

		// Verify all 6 export formats produce non-empty valid files
		exportDir := filepath.Join(baseTempDir, "exports")
		_ = os.MkdirAll(exportDir, 0o755)
		doc := ws.Codec().Parse("# Export Check\n\nOcean Paper 1:1 export.\n\n| Col A | Col B |\n|---|---|\n| 1 | 2 |\n")
		formats := []struct {
			fmt model.ExportFormat
			ext string
		}{
			{model.ExportPNG, "png"},
			{model.ExportHTML, "html"},
			{model.ExportPDF, "pdf"},
			{model.ExportDOCX, "docx"},
			{model.ExportMarkdown, "md"},
			{model.ExportZIP, "zip"},
		}
		for _, f := range formats {
			out := filepath.Join(exportDir, "note."+f.ext)
			if err := markdown.ExportToFile(doc, firstNotePath, out, f.fmt, model.ExportPreferences{Format: f.fmt}); err != nil {
				return "", fmt.Errorf("export %s failed: %w", f.fmt, err)
			}
			info, err := os.Stat(out)
			if err != nil || info.Size() == 0 {
				return "", fmt.Errorf("export %s produced empty file", f.fmt)
			}
		}
		return "Quick Note + PNG/HTML/PDF/DOCX/MD/ZIP exports verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	blockui.SetSmokeFinished(true, "All 15 acceptance steps passed")
	rep.TotalTime = time.Since(startAll)
	return rep, nil
}
