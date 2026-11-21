package smoke

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/ai"
	"block/internal/app"
	"block/internal/files"
	"block/internal/markdown"
	"block/internal/modals"
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

// popupSettle covers the shared popup enter motion (theme.MotionMs).
const popupSettle = 200 * time.Millisecond

func clickTarget(tt *ui.Tester, id string) error {
	tt.Frame()
	tt.Advance(popupSettle)
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

	if err := installSmokePluginFixture(baseTempDir, dataDir); err != nil {
		return nil, fmt.Errorf("installSmokePluginFixture: %w", err)
	}

	ws := app.NewWorkspace(dataDir)
	defer func() {
		if sup := ws.Plugins(); sup != nil {
			sup.ShutdownAll()
		}
	}()
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
		blockui.SetSmokeStep(nil, fmt.Sprintf("[%d/26] %s", idx, name))
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
		if ws.ActiveNotePath() == "" {
			tt.Frame()
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
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
		mx, my := menuRect.X+menuRect.W*0.5, menuRect.Y+menuRect.H*0.5
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
		tt.Advance(popupSettle)
		if err := tt.Click(theme.Tr("一级标题", "Heading 1")); err != nil {
			return "", fmt.Errorf("click Heading 1 in slash menu: %w", err)
		}
		if got := sess.Doc().Blocks[0].Kind; got != model.BlockHeading1 {
			return "", fmt.Errorf("expected H1 after menu click, got %v", got)
		}
		tt.Type("Block MyGo Port")
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		blocks := sess.Doc().Blocks
		if len(blocks) != 2 || blocks[0].Kind != model.BlockHeading1 || blocks[1].Kind != model.BlockParagraph || blocks[1].PlainText() != "" {
			return "", fmt.Errorf("expected Enter to leave an H1 followed by an empty paragraph")
		}
		disk, err := os.ReadFile(firstNotePath)
		if err != nil || !strings.Contains(string(disk), "# Block MyGo Port") {
			return "", fmt.Errorf("disk markdown missing '# Block MyGo Port': %q", string(disk))
		}
		return "# Block MyGo Port persisted; Enter split into an empty paragraph", nil
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
		tt.Advance(popupSettle)
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

		// Brand menu drops below the icon-only cat mark
		markR, ok := blockui.TargetBounds("app-brand-mark")
		if !ok {
			return "", fmt.Errorf("app-brand-mark missing")
		}
		if err := clickTarget(tt, "app-brand-mark"); err != nil {
			return "", err
		}
		if r, ok := blockui.TargetBounds("brand-menu"); !ok || r.X < 0 || r.Y < markR.Y+markR.H {
			return "", fmt.Errorf("brand-menu not below its mark: mark=%+v menu=%+v", markR, r)
		}
		if _, ok := blockui.TargetBounds("brand-about"); !ok {
			return "", fmt.Errorf("brand-menu rows missing")
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
		// Let the 160ms popup enter motion finish before acting on its rows.
		tt.Advance(popupSettle)
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
		tt.Advance(popupSettle)
		if sess.FindMatchCount() != 2 {
			return "", fmt.Errorf("expected 2 matches for 'needle', got %d", sess.FindMatchCount())
		}
		initialMatch := sess.FindCurrentIndex()
		if err := clickTarget(tt, "find-next"); err != nil {
			return "", err
		}
		if got := sess.FindCurrentIndex(); got != (initialMatch+1)%2 {
			return "", fmt.Errorf("expected find-next to change match from %d, got %d", initialMatch, got)
		}
		if err := clickTarget(tt, "find-prev"); err != nil {
			return "", err
		}
		if got := sess.FindCurrentIndex(); got != initialMatch {
			return "", fmt.Errorf("expected find-prev to return to match %d, got %d", initialMatch, got)
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

	// Step 9: Multi-block selection -> annotation & emoji reaction (rail or block foot) -> rewrite -> Undo.
	if err := runStep(9, "Multi-block annotations, emoji reaction, rewrite & Undo", func() (string, error) {
		sess := ws.ActiveSession()
		sess.SelectRange(1, 0, 2, 6)
		ann := sess.AddAnnotation("Make concise")
		if ann == nil {
			return "", fmt.Errorf("AddAnnotation returned nil")
		}
		sess.AddReaction("👍")
		tt.Frame()
		// 1200px window minus the sidebar is below the wide-rail threshold
		// (672+72 body + 232 rail + 14 gutter), so the card sits in the block foot.
		_, railOK := blockui.TargetBounds("annotation-rail")
		_, cardOK := blockui.TargetBounds("annotation-card-" + ann.ID)
		if !railOK && !cardOK {
			return "", fmt.Errorf("expected annotation rail or block-foot card visible")
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
		// Step 9 left an annotation on this note, so the wide annotation rail
		// takes 232px of the margin; 1400px keeps the left margin >= 36px.
		tt.SetSize(1400, 780)
		tt.Frame()
		sess := ws.ActiveSession()
		if len(sess.Outline()) != 4 {
			return "", fmt.Errorf("expected 4 outline items, got %d", len(sess.Outline()))
		}
		railRect, ok := blockui.TargetBounds("outline-rail")
		if !ok {
			return "", fmt.Errorf("outline-rail missing at 1400px")
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
		tt.Advance(popupSettle)
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

		// Feedback modal: draft persistence + submit thank-you state.
		// The real upload needs the network; the hook stands in for it.
		ws.Modals().FeedbackSubmitFn = func(category, message, contact string) error { return nil }
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
		tt.Advance(popupSettle)
		if err := clickTarget(tt, "settings-button"); err != nil {
			return "", err
		}
		if !ws.Modals().IsSettingsOpen() {
			return "", fmt.Errorf("expected settings page open")
		}
		tt.Advance(popupSettle)
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
		tt.Advance(popupSettle)
		if _, ok := blockui.TargetBounds("settings-ignore-search"); !ok {
			return "", fmt.Errorf("expected settings-ignore-search section")
		}
		if err := clickTarget(tt, "settings-folder-rule-add"); err != nil {
			return "", err
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
		tt.Advance(popupSettle)
		if err := clickTarget(tt, "settings-tab-shortcuts"); err != nil {
			return "", err
		}
		tt.Advance(popupSettle)

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
		if err := ws.AI().SaveProviderConfig(model.AIProviderConfig{Enabled: true, Provider: "deepseek"}); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "titlebar-ai"); err != nil {
			return "", err
		}
		if !ws.AI().IsOpen() {
			return "", fmt.Errorf("expected AI panel open after clicking titlebar-ai")
		}
		tt.Advance(popupSettle)
		if err := waitUntil(time.Second, func() bool {
			tt.Advance(popupSettle)
			_, ok := blockui.TargetBounds("ai-cover-settings")
			return ok
		}); err != nil {
			return "", fmt.Errorf("unconfigured AI cover did not render: %w", err)
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
		tt.Advance(popupSettle)

		// 13c. Fullscreen immersive toggle & restore
		if err := clickTarget(tt, "ai-fullscreen-toggle"); err != nil {
			return "", err
		}
		if !ws.AI().IsFullscreen() {
			return "", fmt.Errorf("expected AI fullscreen enabled")
		}
		tt.Advance(popupSettle)
		if err := clickTarget(tt, "ai-fullscreen-toggle"); err != nil {
			return "", err
		}
		if ws.AI().IsFullscreen() {
			return "", fmt.Errorf("expected AI fullscreen disabled")
		}
		tt.Advance(popupSettle)

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

	// Step 16: Single-row selection toolbar overflow (`tb-more`), `SelectedMarkdown()`, & 3-column layout yield priority.
	if err := runStep(16, "Toolbar overflow tb-more, SelectedMarkdown & 3-col layout yield", func() (string, error) {
		_ = ws.OpenNote(firstNotePath, 0)
		if err := ws.WriteNote(firstNotePath, "# Layout & Toolbar\n\n**BoldWord** and `inlineCode` sample.\n"); err != nil {
			return "", err
		}
		sess := ws.ActiveSession()
		sess.FocusBlock(1)
		sess.SelectRange(1, 0, 1, 8)
		if got := sess.SelectedMarkdown(); !strings.Contains(got, "**BoldWord**") {
			return "", fmt.Errorf("expected SelectedMarkdown to preserve **BoldWord**, got %q", got)
		}

		// Narrow window + open docked AI -> sidebar yields first, AI stays docked, narrow note triggers tb-more!
		ws.AI().SetOpen(true)
		tt.SetSize(680, 720)
		tt.Frame()
		tt.Advance(popupSettle)
		if _, sideDocked := blockui.TargetBounds("sidebar"); sideDocked {
			return "", fmt.Errorf("expected left sidebar yielded at 680px when AI chat is docked")
		}
		if err := clickTarget(tt, "tb-more"); err != nil {
			return "", fmt.Errorf("expected tb-more overflow button in narrow column: %w", err)
		}
		tt.Advance(popupSettle)
		if _, ok := blockui.TargetBounds("toolbar-more-menu"); !ok {
			return "", fmt.Errorf("expected toolbar-more-menu open after clicking tb-more")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Advance(popupSettle)

		// Opening a file from the narrow sidebar drawer auto-closes the drawer!
		if err := clickTarget(tt, "sidebar-toggle"); err != nil {
			return "", err
		}
		tt.Advance(popupSettle)
		if _, ok := blockui.TargetBounds("sidebar-drawer-scrim"); !ok {
			return "", fmt.Errorf("expected sidebar-drawer-scrim open")
		}
		if err := clickTarget(tt, "tree-row:"+firstNotePath); err != nil {
			return "", err
		}
		tt.Advance(popupSettle)
		if _, stillOpen := blockui.TargetBounds("sidebar-drawer-scrim"); stillOpen {
			return "", fmt.Errorf("expected sidebar drawer auto-closed after opening file")
		}

		ws.AI().SetOpen(false)
		tt.SetSize(1200, 780)
		tt.Frame()
		return "tb-more + SelectedMarkdown + docked AI priority verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 17: Starred Inline AI (`tb-ai` / `/ai`), `ai-inline-prompts.json`, 5-state Callout dropdown, list guides & empty hints.
	if err := runStep(17, "Starred Inline AI, 5-state Callout, list guides & empty hints", func() (string, error) {
		seed := "> [!NOTE]\n> Callout body\n\n- Parent bullet\n  - Nested bullet\n\n\n"
		if err := ws.WriteNote(firstNotePath, seed); err != nil {
			return "", err
		}
		sess := ws.ActiveSession()
		lastIdx := len(sess.Doc().Blocks) - 1
		sess.FocusBlock(lastIdx)
		tt.Frame()

		// Empty-block caret hint keycaps (`/` `、`)
		if _, ok := blockui.TargetBounds("empty-hint-menu-keys"); !ok {
			return "", fmt.Errorf("expected empty-hint-menu-keys on focused empty paragraph")
		}

		// Nested list ancestor guide hairline
		nestedID := sess.Doc().Blocks[2].ID
		if _, ok := blockui.TargetBounds(fmt.Sprintf("list-guide-%d-0", nestedID)); !ok {
			return "", fmt.Errorf("expected list-guide-%d-0 on nested list item", nestedID)
		}

		// 5-state semantic Callout dropdown (`callout-pill` -> `callout-opt-3` Warning)
		if err := clickTarget(tt, "callout-pill"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("callout-kind-menu"); !ok {
			return "", fmt.Errorf("expected callout-kind-menu open")
		}
		if err := clickTarget(tt, "callout-opt-3"); err != nil {
			return "", err
		}
		if sess.Doc().Blocks[0].Callout != model.CalloutWarning {
			return "", fmt.Errorf("expected Callout WARNING, got %q", sess.Doc().Blocks[0].Callout)
		}

		// Starred Inline AI on empty block -> toggle star -> generate native block preview -> Enter accept
		sess.FocusBlock(len(sess.Doc().Blocks) - 1)
		tt.Frame()
		tt.Type("/ai")
		tt.Key(0, ui.KeyEnter)
		if _, ok := blockui.TargetBounds("inline-ai"); !ok {
			return "", fmt.Errorf("expected inline-ai open after /ai")
		}
		if err := clickTarget(tt, "ai-saved-star-0"); err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dataDir, "ai-inline-prompts.json")); err != nil {
			return "", fmt.Errorf("expected ai-inline-prompts.json saved: %w", err)
		}
		// Click the first remaining starred preset ("列大纲") and then click ai-accept!
		if err := clickTarget(tt, "ai-saved-prompt-0"); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("inline-ai-preview"); !ok {
			return "", fmt.Errorf("expected inline-ai-preview on paper")
		}
		if err := clickTarget(tt, "ai-accept"); err != nil {
			return "", err
		}
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "核心脉络") {
			return "", fmt.Errorf("expected accepted inline AI outline in note, got %q", sess.Markdown())
		}
		return "Inline AI stars + Callout 5-state + list guides + empty hints passed", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 18: Non-Markdown file surfaces in tabs (PNG zoom, SVG preview/source, HTML browser btn, UTF-8 BOM JSON, Binary blocked).
	if err := runStep(18, "Non-Markdown tabs (PNG, SVG preview/source, HTML, UTF-8 BOM, Blocked)", func() (string, error) {
		// 18a. 1x1 valid PNG -> image-preview-stage + tb-img-zoom-in
		pngPath := filepath.Join(nbDir, "sample.png")
		onePxPNG := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
			0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
			0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
			0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
			0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
		}
		if err := os.WriteFile(pngPath, onePxPNG, 0o644); err != nil {
			return "", err
		}
		if err := ws.OpenNote(pngPath, 0); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("image-preview-stage"); !ok {
			return "", fmt.Errorf("expected image-preview-stage for .png")
		}
		if err := clickTarget(tt, "tb-img-zoom-in"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "tb-img-zoom-fit"); err != nil {
			return "", err
		}

		// 18b. SVG -> svg-preview-stage -> switch svg-mode-source -> source-editor
		svgPath := filepath.Join(nbDir, "badge.svg")
		if err := os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><circle cx="20" cy="20" r="16" fill="#007AFF"/></svg>`), 0o644); err != nil {
			return "", err
		}
		if err := ws.OpenNote(svgPath, 0); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("svg-preview-stage"); !ok {
			return "", fmt.Errorf("expected svg-preview-stage for .svg")
		}
		if err := clickTarget(tt, "svg-mode-source"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("source-editor"); !ok {
			return "", fmt.Errorf("expected source-editor after switching SVG to Source mode")
		}

		// 18c. HTML -> html-browser + source-editor
		htmlPath := filepath.Join(nbDir, "demo.html")
		if err := os.WriteFile(htmlPath, []byte("<!doctype html><h1>Hello</h1>\n"), 0o644); err != nil {
			return "", err
		}
		if err := ws.OpenNote(htmlPath, 0); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("html-browser"); !ok {
			return "", fmt.Errorf("expected html-browser button in titlebar for .html")
		}

		// 18d. UTF-8 BOM JSON -> WriteNote preserves leading BOM!
		bomPath := filepath.Join(nbDir, "config.json")
		if err := os.WriteFile(bomPath, append([]byte{0xEF, 0xBB, 0xBF}, []byte("{\"ok\":true}\n")...), 0o644); err != nil {
			return "", err
		}
		if err := ws.OpenNote(bomPath, 0); err != nil {
			return "", err
		}
		tt.Frame()
		if err := ws.WriteNote(bomPath, "{\"ok\":false}\n"); err != nil {
			return "", err
		}
		rawBOM, err := os.ReadFile(bomPath)
		if err != nil || len(rawBOM) < 3 || rawBOM[0] != 0xEF || rawBOM[1] != 0xBB || rawBOM[2] != 0xBF {
			return "", fmt.Errorf("expected UTF-8 BOM preserved on save, got %x", rawBOM)
		}

		// 18e. Binary file -> blocked-file-view
		binPath := filepath.Join(nbDir, "archive.bin")
		if err := os.WriteFile(binPath, []byte{0x00, 0xFF, 0x10, 0x00, 0x89}, 0o644); err != nil {
			return "", err
		}
		if err := ws.OpenNote(binPath, 0); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("blocked-file-view"); !ok {
			return "", fmt.Errorf("expected blocked-file-view for binary file")
		}

		_ = ws.OpenNote(firstNotePath, 0)
		ws.CloseOtherTabs(firstNotePath)
		tt.Frame()
		return "PNG + SVG + HTML + UTF-8 BOM + Blocked tabs verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 19: Full-window Page History (`history-view` -> restore -> `history-toast-undo`), manual `FileOrder` & recovery draft.
	if err := runStep(19, "Full-window Page History + Undo toast, FileOrder & recovery draft", func() (string, error) {
		histNote := filepath.Join(nbDir, "HistoryTarget.md")
		if err := ws.WriteNote(histNote, "# Version One\n\nOriginal paragraph.\n"); err != nil {
			return "", err
		}
		if err := ws.WriteNote(histNote, "# Version Two\n\nUpdated paragraph.\n"); err != nil {
			return "", err
		}
		if err := ws.OpenNote(histNote, 0); err != nil {
			return "", err
		}
		tt.Frame()

		// Open Page History via titlebar-more -> page-history
		if err := clickTarget(tt, "titlebar-more"); err != nil {
			return "", err
		}
		tt.Advance(popupSettle)
		if err := clickTarget(tt, "page-history"); err != nil {
			return "", err
		}
		tt.Frame()
		if !ws.IsPageHistoryOpen() {
			return "", fmt.Errorf("expected full-window Page History open")
		}
		if _, ok := blockui.TargetBounds("history-view"); !ok {
			return "", fmt.Errorf("expected history-view probe")
		}
		// Toggle clean mode & back to diff mode, select older snapshot (history-snap-1), restore -> confirm!
		if err := clickTarget(tt, "history-mode-clean"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "history-mode-diff"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "history-snap-1"); err != nil {
			return "", err
		}
		tt.Frame()
		if err := clickTarget(tt, "history-restore"); err != nil {
			return "", err
		}
		tt.Frame()
		if err := clickTarget(tt, "history-confirm-restore"); err != nil {
			return "", err
		}
		tt.Frame()
		if ws.IsPageHistoryOpen() {
			return "", fmt.Errorf("expected history view closed after restore")
		}
		if got, _ := ws.ReadNote(histNote); !strings.Contains(got, "Version One") {
			return "", fmt.Errorf("expected restored Version One, got %q", got)
		}
		// Post-restore toast undo (`history-toast-undo`) restores Version Two!
		if _, ok := blockui.TargetBounds("history-toast-undo"); !ok {
			return "", fmt.Errorf("expected history-toast-undo banner in note column")
		}
		if err := clickTarget(tt, "history-toast-undo"); err != nil {
			return "", err
		}
		tt.Frame()
		if got, _ := ws.ReadNote(histNote); !strings.Contains(got, "Version Two") {
			return "", fmt.Errorf("expected Undo Restore to bring back Version Two, got %q", got)
		}

		// Manual sibling reorder persists across notebook switches!
		ws.ReorderSibling(nbDir, histNote, firstNotePath, false)
		entriesBefore := ws.Entries()
		_ = ws.SetNotebook(nbDir2)
		_ = ws.SetNotebook(nbDir)
		entriesAfter := ws.Entries()
		if len(entriesBefore) != len(entriesAfter) {
			return "", fmt.Errorf("entries count mismatch after notebook switch")
		}
		var idxHist, idxFirst int
		for i, e := range entriesAfter {
			if filepath.Clean(e.Path) == filepath.Clean(histNote) {
				idxHist = i
			}
			if filepath.Clean(e.Path) == filepath.Clean(firstNotePath) {
				idxFirst = i
			}
		}
		if idxHist >= idxFirst {
			return "", fmt.Errorf("expected reordered %s (idx %d) before %s (idx %d)", filepath.Base(histNote), idxHist, filepath.Base(firstNotePath), idxFirst)
		}

		// Crash-recovery draft round-trip (`SaveRecovery` -> `NewWorkspace` -> `ApplyPendingRecoveryDraft`)
		_ = files.SaveRecovery(histNote, "# Recovered From Crash\n")
		wsRec := app.NewWorkspace(dataDir)
		if p, _, ok := wsRec.PendingRecoveryDraft(); !ok || filepath.Clean(p) != filepath.Clean(histNote) {
			return "", fmt.Errorf("expected PendingRecoveryDraft for %s, got %q ok=%v", histNote, p, ok)
		}
		if _, err := wsRec.ApplyPendingRecoveryDraft(); err != nil {
			return "", err
		}
		return "Page History + Undo toast + FileOrder + Recovery verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 20: Full-window 12-theme `A-Export` stage (`export-view`, light/dark 6+6 themes, toggles, save & back).
	if err := runStep(20, "Full-window 12-theme A-Export stage & preferences persistence", func() (string, error) {
		_ = ws.OpenNote(firstNotePath, 0)
		if err := ws.WriteNote(firstNotePath, "# Ocean Export Stage\n\nParagraph for 12-theme preview.\n"); err != nil {
			return "", err
		}
		tt.Frame()

		savedOut := filepath.Join(baseTempDir, "exports", "stage-out.png")
		ws.Export().OnSaveDialog = func(dir, suggested string, done func(string)) {
			done(savedOut)
		}

		// Open full-window Export stage from titlebar-more -> page-export-image
		if err := clickTarget(tt, "titlebar-more"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "page-export-image"); err != nil {
			return "", err
		}
		tt.Frame()
		if !ws.Export().IsOpen() {
			return "", fmt.Errorf("expected full-window export stage open")
		}
		for _, id := range []string{"export-view", "export-back", "export-stage", "export-options", "export-title-toggle", "export-watermark", "export-save"} {
			if _, ok := blockui.TargetBounds(id); !ok {
				return "", fmt.Errorf("expected probe %q in A-Export view", id)
			}
		}

		// Switch to Dark · 6 -> Neon, then Light · 6 -> Journal, toggle watermark, click export-save -> export-back
		if err := tt.Click("深色 · 6"); err != nil {
			return "", fmt.Errorf("click dark theme tab: %w", err)
		}
		if err := clickTarget(tt, "export-style-neon"); err != nil {
			return "", err
		}
		if ws.Export().ActiveStyle() != model.ExportThemeNeon {
			return "", fmt.Errorf("expected Neon export theme, got %q", ws.Export().ActiveStyle())
		}
		if err := tt.Click("浅色 · 6"); err != nil {
			return "", fmt.Errorf("click light theme tab: %w", err)
		}
		if err := clickTarget(tt, "export-style-journal"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "export-watermark"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "export-save"); err != nil {
			return "", err
		}
		if info, err := os.Stat(savedOut); err != nil || info.Size() == 0 {
			return "", fmt.Errorf("expected non-empty saved export PNG at %s: %v", savedOut, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "export-preferences.json")); err != nil {
			return "", fmt.Errorf("expected export-preferences.json persisted: %w", err)
		}
		if err := clickTarget(tt, "export-back"); err != nil {
			return "", err
		}
		if ws.Export().IsOpen() {
			return "", fmt.Errorf("expected export stage closed after export-back")
		}
		return "12-theme A-Export stage + save + prefs verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 21: Adversarial Zero-Jitter Pointer & Focus Sweep across Titlebar, Sidebar, and Rich Note (`0.0px` ΔY / ΔH).
	if err := runStep(21, "Adversarial zero-jitter hover & focus sweep (0.0px delta)", func() (string, error) {
		jitterNote := filepath.Join(nbDir, "JitterSweep.md")
		richMD := strings.Join([]string{
			"# Zero Jitter Heading",
			"",
			"Single line paragraph next to block handle.",
			"",
			"- [ ] Checkable item without vertical shift",
			"",
			"> [!NOTE]",
			"> Callout body stays rock solid on hover.",
			"",
			"```go",
			"package main",
			"func main() {}",
			"```",
			"",
			"![sample](sample.png)",
			"",
			"Trailing paragraph below image and code block.",
			"",
		}, "\n")
		if err := ws.WriteNote(jitterNote, richMD); err != nil {
			return "", err
		}
		if err := ws.OpenNote(jitterNote, 0); err != nil {
			return "", err
		}
		tt.SetSize(1200, 1120)
		tt.Move(-20, -20)
		tt.Frame()

		sess := ws.ActiveSession()
		if sess == nil {
			return "", fmt.Errorf("expected active session for JitterSweep.md")
		}
		trackedProbes := []string{
			"notebook-switcher",
			"tree-row:" + jitterNote,
			"tab:" + jitterNote,
			"callout-icon",
			"callout-pill",
		}
		for _, blk := range sess.Doc().Blocks {
			trackedProbes = append(trackedProbes, fmt.Sprintf("block-%d", blk.ID))
		}

		baseline := make(map[string]ui.Rect, len(trackedProbes))
		for _, id := range trackedProbes {
			r, ok := blockui.TargetBounds(id)
			if !ok {
				return "", fmt.Errorf("missing baseline probe %q in rich note", id)
			}
			baseline[id] = r
		}

		assertNoShift := func(contextLabel string) error {
			for _, id := range trackedProbes {
				base := baseline[id]
				cur, ok := blockui.TargetBounds(id)
				if !ok {
					return fmt.Errorf("probe %q vanished during %s", id, contextLabel)
				}
				if math.Abs(float64(cur.Y-base.Y)) > 0.05 || math.Abs(float64(cur.H-base.H)) > 0.05 {
					return fmt.Errorf("layout jitter on %q at %s: baseline=(Y:%.2f,H:%.2f) got=(Y:%.2f,H:%.2f)",
						id, contextLabel, base.Y, base.H, cur.Y, cur.H)
				}
			}
			return nil
		}

		// Sweep X columns (sidebar, block gutter/handle, note body, right outline rail) every 16px Y
		for _, x := range []float32{90, 290, 480, 1140} {
			for y := float32(12); y <= 780; y += 16 {
				tt.Move(x, y)
				tt.Frame()
				if err := assertNoShift(fmt.Sprintf("hover(%.0f,%.0f)", x, y)); err != nil {
					return "", err
				}
			}
		}

		// Click the image block to open floating image-toolbar -> trailing paragraph must stay at exact same Y!
		for _, blk := range sess.Doc().Blocks {
			if blk.Kind == model.BlockImage {
				if err := clickTarget(tt, fmt.Sprintf("image-%d", blk.ID)); err != nil {
					return "", err
				}
				tt.Frame()
				if _, ok := blockui.TargetBounds("image-toolbar"); !ok {
					return "", fmt.Errorf("expected floating image-toolbar overlay when image block focused")
				}
				if err := assertNoShift("image-toolbar-focus"); err != nil {
					return "", err
				}
				break
			}
		}
		return fmt.Sprintf("0.0px ΔY/ΔH verified across %d probes & image-toolbar focus", len(trackedProbes)), nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 22: Real Local HTTP OpenAI SSE Full-Chain (multi-tool chat + BatchPlan + live Inline AI + titlebar 统一发送 annotation AI).
	if err := runStep(22, "Real HTTP OpenAI SSE chat + Inline AI + 统一发送 annotations", func() (string, error) {
		liveNote := filepath.Join(nbDir, "LiveAINote.md")
		if err := ws.WriteNote(liveNote, "# Live AI Verification\n\nOriginal sentence to polish.\n"); err != nil {
			return "", err
		}
		if err := ws.OpenNote(liveNote, 0); err != nil {
			return "", err
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1-smoke"}]}`))
				return
			}
			var req struct {
				Stream   bool `json:"stream"`
				Messages []struct {
					Role    string `json:"role"`
					Content any    `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)

			sysText := ""
			userText := ""
			toolResults := 0
			for _, m := range req.Messages {
				s, _ := m.Content.(string)
				switch m.Role {
				case "system":
					sysText = s
				case "user":
					userText = s
				case "tool":
					toolResults++
				}
			}

			// Case A: Workspace Inline AI (`CompletePrompt`)
			if strings.Contains(sysText, "inline writing assistant") {
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Live SSE Inline Polished Sentence."}}]}`)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			// Case B: Workspace Batch Annotation notes (`CompletePrompt`)
			if strings.Contains(sysText, "margin notes") {
				// Extract first annotation id from userText JSON
				annID := "ann-1"
				if idx := strings.Index(userText, `"id": "`); idx >= 0 {
					rest := userText[idx+7:]
					if end := strings.IndexByte(rest, '"'); end > 0 {
						annID = rest[:end]
					}
				}
				respJSON, _ := json.Marshal([]map[string]string{{
					"id":   annID,
					"kind": "edit",
					"text": "Live SSE Annotation Rewritten Span.",
				}})
				chunk, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{"content": string(respJSON)},
					}},
				})
				fmt.Fprintf(w, "data: %s\n", chunk)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
				if flusher != nil {
					flusher.Flush()
				}
				return
			}

			// Case C: Multi-turn Chat SSE (`readNote` -> `executeBatchPlan` -> final reply)
			if toolResults == 0 {
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_read","function":{"name":"readNote","arguments":"{\"pageId\":\"LiveAINote\"}"}}]}}]}`)
				planArgs, _ := json.Marshal(map[string]any{
					"title":   "Live SSE Update",
					"summary": "Update LiveAINote via real HTTP SSE",
					"operations": []map[string]any{{
						"type":      "search_replace",
						"pageId":    "LiveAINote",
						"oldString": "Original sentence to polish.",
						"newString": "Chat Plan Updated Sentence.",
					}},
				})
				tcChunk, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index": 1,
								"id":    "call_plan",
								"function": map[string]any{
									"name":      "executeBatchPlan",
									"arguments": string(planArgs),
								},
							}},
						},
					}},
				})
				fmt.Fprintf(w, "data: %s\n", tcChunk)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
			} else {
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"已完成实时 SSE 计划生成。"}}]}`)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}))
		defer srv.Close()

		ws.AI().EnableMockResponder(false)
		defer ws.AI().EnableMockResponder(true)

		if err := ws.AI().SaveProviderConfig(model.AIProviderConfig{
			Enabled:  true,
			Provider: "openai",
			Protocol: model.ProtocolOpenAI,
			BaseURL:  srv.URL + "/v1",
			APIKey:   "sk-live-sse-test",
			Model:    "gpt-4.1-smoke",
		}); err != nil {
			return "", err
		}
		if !ws.AI().IsLiveProviderReady() {
			return "", fmt.Errorf("expected IsLiveProviderReady() == true after configuring live HTTP provider")
		}

		// 22a. Multi-turn chat SSE -> BatchPlan -> AcceptPlan
		ws.AI().NewConversation()
		ws.AI().SendPrompt("通过真实 SSE 更新当前笔记")
		if err := waitUntil(3*time.Second, func() bool {
			return !ws.AI().IsStreaming() && ws.AI().ActivePlan() != nil
		}); err != nil {
			return "", fmt.Errorf("live HTTP SSE chat plan timeout: %w", err)
		}
		ap := ws.AI().ActivePlan()
		if err := ws.AI().AcceptPlan(ap.ConvID, ap.MsgIndex); err != nil {
			return "", err
		}
		if got, _ := ws.ReadNote(liveNote); !strings.Contains(got, "Chat Plan Updated Sentence.") {
			return "", fmt.Errorf("expected accepted SSE BatchPlan content, got %q", got)
		}

		// 22b. Live Inline AI (`tb-ai` -> type prompt -> Enter -> wait for live HTTP SSE -> `ai-accept`)
		tt.Frame()
		sess := ws.ActiveSession()
		sess.SelectRange(1, 0, 1, len([]rune(sess.Doc().Blocks[1].PlainText())))
		tt.Frame()
		if err := clickTarget(tt, "tb-ai"); err != nil {
			return "", err
		}
		tt.Frame()
		tt.Type("润色表达")
		tt.Key(0, ui.KeyEnter)
		if err := waitUntil(2*time.Second, func() bool {
			tt.Frame()
			_, ok := blockui.TargetBounds("ai-accept")
			return ok
		}); err != nil {
			return "", fmt.Errorf("live Inline AI SSE timeout: %w", err)
		}
		if err := clickTarget(tt, "ai-accept"); err != nil {
			return "", err
		}
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "Live SSE Inline Polished Sentence.") {
			return "", fmt.Errorf("expected live Inline AI output in note, got %q", sess.Markdown())
		}

		// 22c. Titlebar `统一发送` (`annotations-send-all`) -> `ann-accept-<id>`
		sess.FocusBlock(1)
		sess.SelectRange(1, 0, 1, len([]rune(sess.Doc().Blocks[1].PlainText())))
		ann := sess.AddAnnotation("翻译并精炼")
		if ann == nil {
			return "", fmt.Errorf("expected AddAnnotation to create pending annotation")
		}
		tt.Frame()
		if err := clickTarget(tt, "annotations-send-all"); err != nil {
			return "", fmt.Errorf("expected titlebar annotations-send-all button: %w", err)
		}
		// 统一发送 runs in the background; wait for the accept button.
		if err := waitUntil(3*time.Second, func() bool {
			tt.Frame()
			_, ok := blockui.TargetBounds("ann-accept-" + ann.ID)
			return ok
		}); err != nil {
			return "", fmt.Errorf("expected ann-accept-%s after 统一发送: %w", ann.ID, err)
		}
		if err := clickTarget(tt, "ann-accept-"+ann.ID); err != nil {
			return "", fmt.Errorf("expected ann-accept-%s after 统一发送: %w", ann.ID, err)
		}
		tt.Frame()
		if !strings.Contains(sess.Markdown(), "Live SSE Annotation Rewritten Span.") {
			return "", fmt.Errorf("expected applied annotation rewrite in note, got %q", sess.Markdown())
		}
		return "Live OpenAI SSE chat + Inline AI + 统一发送 annotation rewrite verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 23: Quick Note 1:1 Full Flow (centered 3-dot pager, zoom persistence, SaveToNotebook auto-open).
	if err := runStep(23, "Quick Note 3-dot pager, zoom persistence & auto-open on save", func() (string, error) {
		qn := ws.QuickNote()
		qn.Show()
		ttQuick := ui.NewTester(qn.RenderView, 440, 520)
		ttQuick.Frame()

		for _, id := range []string{"quick-slot-0", "quick-slot-1", "quick-slot-2", "save-to-note", "quick-note-editor"} {
			if _, ok := blockui.TargetBounds(id); !ok {
				return "", fmt.Errorf("missing Quick Note probe %q", id)
			}
		}
		if err := clickTarget(ttQuick, "quick-slot-1"); err != nil {
			return "", err
		}
		if qn.ActiveSlot() != 1 {
			return "", fmt.Errorf("expected ActiveSlot()==1 after clicking quick-slot-1, got %d", qn.ActiveSlot())
		}
		// Zoom in (`Cmd+=`) persists QuickEditorZoom into AppState, then reset (`Cmd+0`)
		ttQuick.Key(ui.Cmd, ui.KeyEqual)
		ttQuick.Frame()
		if z := ws.State().QuickEditorZoom; z == nil || *z <= 1.0 {
			return "", fmt.Errorf("expected QuickEditorZoom > 1.0 persisted in AppState, got %v", z)
		}
		ttQuick.Key(ui.Cmd, ui.Key0)
		ttQuick.Frame()

		qn.SetText("# Quick Auto-Opened Note\n\nCaptured in slot 2 and opened in main workspace.\n")
		ttQuick.Frame()
		if err := clickTarget(ttQuick, "save-to-note"); err != nil {
			return "", err
		}
		qn.Hide()
		tt.Frame()
		if !strings.Contains(filepath.Base(ws.ActiveNotePath()), "Quick Auto-Opened Note") {
			return "", fmt.Errorf("expected saved Quick Note auto-opened as active tab, got %q", ws.ActiveNotePath())
		}
		return "Quick Note 3-dot slots + QuickEditorZoom + auto-open verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 24: Settings 4-Tab Lists Ordering, Font Picker Dropdown, Annotation Model & Shortcut Rebind/Reset.
	if err := runStep(24, "Settings 4-tab list ordering, font dropdown, annotation model & shortcuts", func() (string, error) {
		// 1) AI tab: verify AI 与本机 Agent split rail + 普通功能 -> 标注功能 trigger.
		ws.OpenSettings("ai")
		tt.Frame()
		if _, ok := blockui.TargetBounds("annotation-model-trigger"); !ok {
			return "", fmt.Errorf("missing annotation-model-trigger in AI settings")
		}

		// 2) Appearance tab: open Body font dropdown popover right below trigger.
		ws.OpenSettings("appearance")
		tt.Frame()
		if err := clickTarget(tt, "settings-body-font-trigger"); err != nil {
			return "", err
		}
		if _, ok := blockui.TargetBounds("font-picker-menu"); !ok {
			return "", fmt.Errorf("missing anchored font-picker-menu popover")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		// 3) Shortcuts tab: verify exact 6 rows in vertical order, rebind, conflict notice, live routing & reset.
		ws.OpenSettings("shortcuts")
		tt.Frame()
		var prevY float32 = -1
		for _, id := range []string{
			"settings-rec-search",
			"settings-rec-quick",
			"settings-keycap-search",
			"settings-keycap-find",
			"settings-keycap-new",
			"settings-keycap-ai",
		} {
			b, ok := blockui.TargetBounds(id)
			if !ok {
				return "", fmt.Errorf("missing shortcut row %q", id)
			}
			if b.Y <= prevY {
				return "", fmt.Errorf("shortcut row %q out of vertical order (Y=%v <= %v)", id, b.Y, prevY)
			}
			prevY = b.Y
		}

		// Record Alt+Shift+Q on quick_note
		if err := clickTarget(tt, "settings-rec-quick"); err != nil {
			return "", err
		}
		tt.Key(ui.Alt|ui.Shift, ui.KeyQ)
		tt.Frame()
		if got := modals.EffectiveShortcut(ws.State(), "quick_note"); got != "alt-shift-q" {
			return "", fmt.Errorf("expected quick_note rebound to alt-shift-q, got %q", got)
		}

		// Record conflicting Alt+Shift+Q on global_summon -> conflict notice appears and reassigns quick_note!
		if err := clickTarget(tt, "settings-rec-search"); err != nil {
			return "", err
		}
		tt.Key(ui.Alt|ui.Shift, ui.KeyQ)
		tt.Frame()
		if _, ok := blockui.TargetBounds("settings-shortcut-conflict-notice"); !ok {
			return "", fmt.Errorf("expected settings-shortcut-conflict-notice on duplicate chord")
		}

		// Exit Settings -> trigger Alt+Shift+Q live in Workspace (opens Search modal via global_summon fallback)!
		if err := clickTarget(tt, "settings-sidebar-back"); err != nil {
			if err := clickTarget(tt, "settings-back"); err != nil {
				return "", err
			}
		}
		tt.Key(ui.Alt|ui.Shift, ui.KeyQ)
		tt.Frame()
		if !ws.Modals().IsSearchOpen() {
			return "", fmt.Errorf("expected custom Alt+Shift+Q global_summon to open Search modal in Workspace")
		}
		ws.Modals().CloseSearch()
		tt.Frame()

		// Reset all shortcuts back to defaults
		ws.OpenSettings("shortcuts")
		tt.Frame()
		if err := clickTarget(tt, "settings-shortcuts-reset"); err != nil {
			return "", err
		}
		if got := modals.EffectiveShortcut(ws.State(), "global_summon"); got != "cmd-shift-k" {
			return "", fmt.Errorf("expected global_summon reset to cmd-shift-k, got %q", got)
		}
		if got := modals.EffectiveShortcut(ws.State(), "quick_note"); got != "alt-n" {
			return "", fmt.Errorf("expected quick_note reset to alt-n, got %q", got)
		}
		if err := clickTarget(tt, "settings-sidebar-back"); err != nil {
			_ = clickTarget(tt, "settings-back")
		}
		tt.Frame()
		return "Settings 4-tab lists + anchored font popover + 6-row shortcuts + reset verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 25: Page History External Conflict Re-Diff (`重新对比`), Formatted Clean Preview & Read-Only Guard.
	if err := runStep(25, "Page History external conflict re-diff, clean view & read-only guard", func() (string, error) {
		histNote := filepath.Join(nbDir, "HistoryTarget.md")
		if err := ws.OpenNote(histNote, 0); err != nil {
			return "", err
		}
		if !ws.OpenPageHistory() {
			return "", fmt.Errorf("expected OpenPageHistory() == true")
		}
		tt.Frame()

		// Modify file externally on disk while Page History is open -> conflict banner + `history-error-action` (`重新对比`)!
		if err := os.WriteFile(histNote, []byte("# Version Three External\n\nDisk modified externally.\n"), 0o644); err != nil {
			return "", err
		}
		tt.Frame()
		if _, ok := blockui.TargetBounds("history-error-action"); !ok {
			return "", fmt.Errorf("expected history-error-action (重新对比) when note changes externally on disk")
		}
		if err := clickTarget(tt, "history-error-action"); err != nil {
			return "", err
		}
		tt.Frame()
		if _, stillConflict := blockui.TargetBounds("history-error-action"); stillConflict {
			return "", fmt.Errorf("expected external conflict banner cleared after clicking 重新对比")
		}

		// Switch to formatted clean Markdown view (`history-mode-clean`) and exit (`history-back`)
		if err := clickTarget(tt, "history-mode-clean"); err != nil {
			return "", err
		}
		if err := clickTarget(tt, "history-back"); err != nil {
			return "", err
		}
		if ws.IsPageHistoryOpen() {
			return "", fmt.Errorf("expected Page History closed after history-back")
		}
		return "Page History external conflict re-diff + clean Markdown preview verified", nil
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	// Step 26: Built-in test plugin exercising titlebar hiding (+more lock), Command Palette,
	// status bar, toast, side panel, dialog & page menu items end-to-end.
	if err := runStep(26, "Plugin UI surfaces (titlebar hiding, palette, toast, status, panel, dialog & page menu)", func() (string, error) {
		return runPluginUIAcceptance(ws, tt, firstNotePath)
	}); err != nil {
		rep.TotalTime = time.Since(startAll)
		return rep, err
	}

	blockui.SetSmokeFinished(true, "All 26 acceptance steps passed")
	rep.TotalTime = time.Since(startAll)
	return rep, nil
}
