package editor

import (
	"fmt"
	"strings"
	"testing"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func clickProbe(t *testing.T, tt *ui.Tester, id string) {
	t.Helper()
	r, ok := appUi.TargetBounds(id)
	if !ok {
		t.Fatalf("probe %q has no bounds", id)
	}
	tt.ClickAt(r.X+r.W*0.5, r.Y+r.H*0.5)
}

func makeSessionView(s *Session, readOnly bool) func(c *ui.Context) {
	return func(c *ui.Context) {
		w, _ := c.Size()
		s.Render(c, w, readOnly)
	}
}

func TestSessionBasicsAndUndoRedo(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Hello World"}}},
		},
		TrailingLine: "\n",
	}
	s := NewSession("/tmp/note.md", doc, nil, nil)
	if s.NotePath() != "/tmp/note.md" {
		t.Fatalf("unexpected notePath: %q", s.NotePath())
	}
	s.SetNotePath("/tmp/renamed.md")
	if s.NotePath() != "/tmp/renamed.md" {
		t.Fatalf("SetNotePath failed")
	}
	if s.Dirty() {
		t.Fatalf("expected clean initial session")
	}

	s.SelectRange(0, 0, 0, 5)
	s.ToggleMarkOnSelection(model.MarkBold)
	if !s.Dirty() {
		t.Fatalf("expected dirty after ToggleMarkOnSelection")
	}
	if !strings.Contains(s.Markdown(), "**Hello**") {
		t.Fatalf("expected **Hello** in markdown, got %q", s.Markdown())
	}
	s.MarkSaved()
	if s.Dirty() {
		t.Fatalf("expected clean after MarkSaved")
	}

	if !s.CanUndo() || !s.Undo() {
		t.Fatalf("Undo failed")
	}
	if strings.Contains(s.Markdown(), "**Hello**") {
		t.Fatalf("expected bold undone, got %q", s.Markdown())
	}
	if !s.CanRedo() || !s.Redo() {
		t.Fatalf("Redo failed")
	}
	if !strings.Contains(s.Markdown(), "**Hello**") {
		t.Fatalf("expected bold redone, got %q", s.Markdown())
	}

	s.PasteText("# Pasted Heading\n\n- Pasted Item")
	if len(s.Doc().Blocks) < 2 || s.Doc().Blocks[0].Kind != model.BlockHeading1 {
		t.Fatalf("expected pasted markdown blocks, got %+v", s.Doc().Blocks)
	}
}

func TestChineseAndEnglishMarkdownInputRules(t *testing.T) {
	cases := []struct {
		name     string
		typed    string
		wantKind model.BlockKind
		wantLang string
		wantDiv  bool
		wantText string
	}{
		{"en_h1", "# Title", model.BlockHeading1, "", false, "Title"},
		{"zh_h1", "＃ 标题", model.BlockHeading1, "", false, "标题"},
		{"en_num", "1. First", model.BlockNumbered, "", false, "First"},
		{"zh_num", "1。 第一项", model.BlockNumbered, "", false, "第一项"},
		{"en_bullet", "- Bullet", model.BlockBullet, "", false, "Bullet"},
		{"zh_bullet", "· 列表", model.BlockBullet, "", false, "列表"},
		{"en_todo", "[] Task", model.BlockTodo, "", false, "Task"},
		{"zh_todo", "【】 待办", model.BlockTodo, "", false, "待办"},
		{"en_quote", "> Quote", model.BlockQuote, "", false, "Quote"},
		{"zh_quote", "＞ 引用", model.BlockQuote, "", false, "引用"},
		{"en_code", "```go ", model.BlockCode, "go", false, ""},
		{"zh_code", "···py ", model.BlockCode, "python", false, ""},
		{"en_div", "---", model.BlockParagraph, "", true, ""},
		{"zh_div", "——", model.BlockParagraph, "", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession("", nil, nil, nil)
			s.FocusBlock(0)
			tt := ui.NewTester(makeSessionView(s, false), 900, 600)
			tt.Type(tc.typed)
			if tc.wantDiv {
				if len(s.Doc().Blocks) < 2 || s.Doc().Blocks[0].Kind != model.BlockDivider {
					t.Fatalf("expected divider block, got %+v", s.Doc().Blocks)
				}
				return
			}
			b := s.Doc().Blocks[0]
			if b.Kind != tc.wantKind {
				t.Fatalf("got kind %v, want %v (text=%q)", b.Kind, tc.wantKind, b.PlainText())
			}
			if tc.wantLang != "" && b.Language != tc.wantLang {
				t.Fatalf("got lang %q, want %q", b.Language, tc.wantLang)
			}
			if tc.wantText != "" && b.PlainText() != tc.wantText {
				t.Fatalf("got text %q, want %q", b.PlainText(), tc.wantText)
			}
		})
	}

	// Arrow replacement rules: -> and -》
	s := NewSession("", nil, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)
	tt.Type("a->b and c-》d")
	got := s.Doc().Blocks[0].PlainText()
	if got != "a→b and c→d" {
		t.Fatalf("expected arrow replacement, got %q", got)
	}
}

func TestSlashMenuAndCompactZoomScrollContainment(t *testing.T) {
	appUi.ResetSmoke()
	var blocks []model.Block
	for i := 0; i < 18; i++ {
		blocks = append(blocks, model.Block{
			ID:    model.NewBlockID(),
			Kind:  model.BlockParagraph,
			Spans: []model.Span{{Text: fmt.Sprintf("Paragraph line %d with enough text", i)}},
		})
	}
	s := NewSession("", &model.Doc{Blocks: blocks}, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 650)

	// Open slash menu by clearing block 0 or typing on empty block
	s.FocusBlock(0)
	s.Doc().Blocks[0].SetText("")
	tt.Frame()
	tt.Type("/")
	if !s.IsSlashMenuOpen() {
		t.Fatalf("expected slash menu open after typing '/'")
	}
	tt.Type("todo")
	tt.Key(0, ui.KeyEnter)
	if s.IsSlashMenuOpen() {
		t.Fatalf("expected slash menu closed after Enter")
	}
	if s.Doc().Blocks[0].Kind != model.BlockTodo {
		t.Fatalf("expected block 0 converted to Todo, got %v", s.Doc().Blocks[0].Kind)
	}
	tt.Type("TaskOne")
	if !strings.Contains(s.Markdown(), "- [ ] TaskOne") {
		t.Fatalf("expected todo TaskOne in markdown, got %q", s.Markdown())
	}

	// Compact window (380x371) + 90% zoom: slash menu must scroll internally without scrolling body!
	tt.SetSize(380, 371)
	const zoom = float32(0.9)
	tt.SetZoom(zoom)
	s.doc.Blocks = append(s.doc.Blocks, model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph})
	lastIdx := len(s.doc.Blocks) - 1
	s.FocusBlock(lastIdx)
	tt.Frame()
	tt.Type("/")
	if !s.IsSlashMenuOpen() {
		t.Fatalf("expected slash menu open in compact window")
	}
	menuBounds, ok := appUi.TargetBounds("block-menu")
	if !ok || menuBounds.H <= 0 {
		t.Fatalf("missing block-menu bounds")
	}
	if (menuBounds.Y+menuBounds.H)*zoom > 371 {
		t.Fatalf("slash menu overflows compact viewport: %+v (scaled bottom=%v)", menuBounds, (menuBounds.Y+menuBounds.H)*zoom)
	}
	bodyBefore := s.bodyScroll.Y
	mx, my := (menuBounds.X+menuBounds.W*0.5)*zoom, (menuBounds.Y+menuBounds.H*0.5)*zoom
	for i := 0; i < 6; i++ {
		tt.Scroll(mx, my, 0, 80)
	}
	if s.blockMenu.Scroll.Y <= 0 {
		t.Fatalf("expected slash menu internal scroll, got Y=%v", s.blockMenu.Scroll.Y)
	}
	if s.bodyScroll.Y != bodyBefore {
		t.Fatalf("slash menu scroll bled into body: %v -> %v", bodyBefore, s.bodyScroll.Y)
	}
	for i := 0; i < 10; i++ {
		tt.Scroll(mx, my, 0, -80)
	}
	if s.blockMenu.Scroll.Y != 0 || s.bodyScroll.Y != bodyBefore {
		t.Fatalf("expected menu scroll clamped at top without body scroll: menu=%v body=%v/%v",
			s.blockMenu.Scroll.Y, s.bodyScroll.Y, bodyBefore)
	}
}

func TestTodoToggleToolbarBoldAndKeyboardUndoRedo(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 10, Kind: model.BlockTodo, Checked: false, Spans: []model.Span{{Text: "TaskOne"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	clickProbe(t, tt, "todo-checkbox")
	if !s.Doc().Blocks[0].Checked {
		t.Fatalf("expected todo checked after click")
	}
	clickProbe(t, tt, "todo-checkbox")
	if s.Doc().Blocks[0].Checked {
		t.Fatalf("expected todo unchecked after second click")
	}

	s.FocusBlock(0)
	s.SelectRange(0, 0, 0, 7)
	tt.Frame()
	if !s.IsSelectionToolbarOpen() {
		t.Fatalf("expected selection toolbar open")
	}
	clickProbe(t, tt, "tb-bold")
	if !strings.Contains(s.Markdown(), "**TaskOne**") {
		t.Fatalf("expected bold TaskOne, got %q", s.Markdown())
	}

	tt.Key(ui.Cmd, ui.KeyZ)
	if strings.Contains(s.Markdown(), "**TaskOne**") {
		t.Fatalf("expected Cmd+Z to undo bold, got %q", s.Markdown())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if !strings.Contains(s.Markdown(), "**TaskOne**") {
		t.Fatalf("expected Cmd+Shift+Z to redo bold, got %q", s.Markdown())
	}
}

func TestBackspaceMergeAndFirstLineTwoStepDelete(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph},
			{ID: 2, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "CRUD Title"}}},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Tail"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	if s.FirstLineDeleteArmed() {
		t.Fatalf("expected armed=false initially")
	}
	tt.Key(0, ui.KeyBackspace)
	if !s.FirstLineDeleteArmed() || len(s.Doc().Blocks) != 3 {
		t.Fatalf("expected first Backspace to arm delete without removing block")
	}
	if _, ok := appUi.TargetBounds("first-line-delete-badge"); !ok {
		t.Fatalf("expected first-line-delete-badge probe visible when armed")
	}
	tt.Key(0, ui.KeyBackspace)
	if s.FirstLineDeleteArmed() || len(s.Doc().Blocks) != 2 || s.Doc().Blocks[0].PlainText() != "CRUD Title" {
		t.Fatalf("expected second Backspace to delete block 0, got blocks=%+v", s.Doc().Blocks)
	}

	// Merge block 1 ("Tail") into block 0 ("CRUD Title") via Backspace at offset 0
	s.FocusBlock(1)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if len(s.Doc().Blocks) != 1 || s.Doc().Blocks[0].PlainText() != "CRUD TitleTail" {
		t.Fatalf("expected block merge, got %+v", s.Doc().Blocks)
	}
}

func TestFindBarNavigation(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Alpha beta"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Gamma alpha"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	tt.Key(ui.Cmd, ui.KeyF)
	if !s.IsFindOpen() {
		t.Fatalf("expected Cmd+F to open find bar")
	}
	if _, ok := appUi.TargetBounds("find-bar"); !ok {
		t.Fatalf("expected find-bar probe")
	}
	tt.Type("alpha")
	if s.FindMatchCount() != 2 {
		t.Fatalf("expected 2 matches, got %d", s.FindMatchCount())
	}
	if s.FindCurrentIndex() != 0 {
		t.Fatalf("expected match index 0, got %d", s.FindCurrentIndex())
	}
	clickProbe(t, tt, "find-next")
	if s.FindCurrentIndex() != 1 {
		t.Fatalf("expected match index 1 after find-next, got %d", s.FindCurrentIndex())
	}
	clickProbe(t, tt, "find-prev")
	if s.FindCurrentIndex() != 0 {
		t.Fatalf("expected match index 0 after find-prev, got %d", s.FindCurrentIndex())
	}
	clickProbe(t, tt, "find-close")
	if s.IsFindOpen() {
		t.Fatalf("expected find-close to close find bar")
	}
}

func TestOutlineRailHoverJumpAndAutoHide(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "H1 One"}}},
			{ID: 2, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "H2 Two"}}},
			{ID: 3, Kind: model.BlockHeading3, Spans: []model.Span{{Text: "H3 Three"}}},
			{ID: 4, Kind: model.BlockHeading4, Spans: []model.Span{{Text: "H4 Four"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 1100, 700)

	items := s.Outline()
	if len(items) != 4 || items[0].Level != 1 || items[1].Level != 2 || items[2].Level != 3 || items[3].Level != 4 {
		t.Fatalf("unexpected 4-level outline: %+v", items)
	}
	rail, ok := appUi.TargetBounds("outline-rail")
	if !ok {
		t.Fatalf("expected outline-rail visible at 1100px")
	}
	if rail.X < 900 {
		t.Fatalf("expected outline-rail on the right side, got x=%v", rail.X)
	}

	// Hover outline rail -> opens outline-popover
	tt.Move(rail.X+rail.W*0.5, rail.Y+rail.H*0.5)
	if _, ok := appUi.TargetBounds("outline-popover"); !ok {
		t.Fatalf("expected outline-popover on hover")
	}
	caretBefore := s.FocusedBlockIndex()
	clickProbe(t, tt, "outline-item-3")
	if s.FocusedBlockIndex() != caretBefore {
		t.Fatalf("outline jump must preserve caret block %d, got %d", caretBefore, s.FocusedBlockIndex())
	}
	if s.HighlightedBlockID() != 4 {
		t.Fatalf("expected highlighted block ID 4, got %d", s.HighlightedBlockID())
	}
	if _, ok := appUi.TargetBounds("outline-target-highlight"); !ok {
		t.Fatalf("expected outline-target-highlight probe")
	}

	// Auto-hide when viewportWidth < 960
	appUi.ResetSmoke()
	tt.SetSize(900, 700)
	if _, ok := appUi.TargetBounds("outline-rail"); ok {
		t.Fatalf("expected outline-rail hidden at width 900 (<960)")
	}
	tt.SetSize(1100, 700)
	if _, ok := appUi.TargetBounds("outline-rail"); !ok {
		t.Fatalf("expected outline-rail visible again at width 1100")
	}
}

func TestMultiBlockAnnotationsReactionsAndRewrite(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "古Alpha怎么刷牙"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "中间第一行"}}},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "中间第二行"}}},
			{ID: 4, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "冰箱Beta晚上做什么"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.SelectRange(0, 1, 3, 2) // "Alpha怎么刷牙\n中间第一行\n中间第二行\n冰箱"
	ann := s.AddAnnotation("Review")
	if ann == nil || !strings.Contains(ann.QuotedText, "Alpha") || !strings.Contains(ann.QuotedText, "冰箱") {
		t.Fatalf("unexpected annotation: %+v", ann)
	}
	s.AddReaction("👍")
	if len(s.Annotations()) != 1 || len(s.Annotations()[0].Reactions) != 1 {
		t.Fatalf("expected single annotation with reaction, got %+v", s.Annotations())
	}
	// Toggle same reaction off
	s.AddReaction("👍")
	if len(s.Annotations()[0].Reactions) != 0 {
		t.Fatalf("expected reaction toggled off")
	}

	tt := ui.NewTester(makeSessionView(s, false), 1100, 700)
	if _, ok := appUi.TargetBounds("annotation-rail"); !ok {
		t.Fatalf("expected annotation-rail probe")
	}

	// Apply multi-block rewrite
	if !s.ApplyAnnotationRewrite(ann.ID, "Alpha\nBeta") {
		t.Fatalf("ApplyAnnotationRewrite failed")
	}
	if len(s.Doc().Blocks) != 2 {
		t.Fatalf("expected 4 blocks collapsed to 2, got %d", len(s.Doc().Blocks))
	}
	if s.Doc().Blocks[0].PlainText() != "古Alpha" || s.Doc().Blocks[1].PlainText() != "BetaBeta晚上做什么" {
		t.Fatalf("unexpected rewritten text: %q | %q", s.Doc().Blocks[0].PlainText(), s.Doc().Blocks[1].PlainText())
	}
	tt.Frame()
	if !s.Undo() || len(s.Doc().Blocks) != 4 {
		t.Fatalf("expected Undo to restore 4 blocks")
	}
}

func TestCodePreviewTableImageHandlesAndBatchPlanDiff(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Unfocused line"}}},
			{ID: 2, Kind: model.BlockCode, Language: "mermaid", Code: "flowchart LR\nA[Start] --> B[End]"},
			{ID: 3, Kind: model.BlockCode, Language: "latex", Code: `\alpha + \beta = \sum x^2`},
			{ID: 4, Kind: model.BlockCode, Language: "csv", Code: "Name,Role\nAlice,Dev"},
			{ID: 5, Kind: model.BlockTable, Table: &model.TableData{
				Rows: [][]model.TableCell{
					{{Spans: []model.Span{{Text: "Col1"}}}, {Spans: []model.Span{{Text: "Col2"}}}},
					{{Spans: []model.Span{{Text: "A"}}}, {Spans: []model.Span{{Text: "B"}}}},
				},
			}},
			{ID: 6, Kind: model.BlockImage, ImageSrc: "missing.png", ImageAlt: "Cover", ImageAlign: model.ImageAlignCenter, ImageWidth: 80},
		},
	}
	s := NewSession("/tmp/demo.md", doc, nil, nil)
	s.FocusBlock(5) // Focus image block, so block 0 ("Unfocused line") has no handle!
	tt := ui.NewTester(makeSessionView(s, false), 1100, 1100)

	if _, ok := appUi.TargetBounds("block-grip-1"); ok {
		t.Fatalf("unfocused/unhovered block 1 must NOT render block handle")
	}
	if _, ok := appUi.TargetBounds("block-grip-6"); !ok {
		t.Fatalf("focused block 6 must render block handle")
	}
	if _, ok := appUi.TargetBounds("image-toolbar"); !ok {
		t.Fatalf("focused image block must render image-toolbar")
	}
	if !tt.HasText("Start") || !tt.HasText("Alice") {
		t.Fatalf("expected Mermaid & CSV previews rendered")
	}

	// Toggle source on Mermaid block
	clickProbe(t, tt, "cb-source-toggle-2")
	if !s.Doc().Blocks[1].ShowSource {
		t.Fatalf("expected Mermaid ShowSource toggled on")
	}

	// Table add row & column via UI clicks
	s.FocusBlock(4)
	tt.Frame()
	clickProbe(t, tt, "table-add-row")
	if len(s.Doc().Blocks[4].Table.Rows) != 3 {
		t.Fatalf("expected 3 table rows after add-row, got %d", len(s.Doc().Blocks[4].Table.Rows))
	}
	clickProbe(t, tt, "table-add-col")
	if len(s.Doc().Blocks[4].Table.Rows[0]) != 3 {
		t.Fatalf("expected 3 table cols after add-col, got %d", len(s.Doc().Blocks[4].Table.Rows[0]))
	}

	// Image alignment & crop popover via UI clicks
	s.FocusBlock(5)
	tt.Frame()
	lbl := theme.Tr("右对齐", "Align right")
	foundR, foundOk := tt.Find(lbl)
	probeR, probeOk := appUi.TargetBounds("img-align-right")
	if err := tt.Click(lbl); err != nil {
		t.Fatalf("click align right failed: %v", err)
	}
	if s.Doc().Blocks[5].ImageAlign != model.ImageAlignRight {
		t.Fatalf("expected ImageAlignRight, got %v (foundOk=%v foundR=%+v probeOk=%v probeR=%+v undo=%d dirty=%v)",
			s.Doc().Blocks[5].ImageAlign, foundOk, foundR, probeOk, probeR, len(s.undoStack), s.dirty)
	}
	clickProbe(t, tt, "img-crop")
	if _, ok := appUi.TargetBounds("image-crop-popover"); !ok {
		t.Fatalf("expected image-crop-popover probe")
	}
	clickProbe(t, tt, "img-crop") // close crop popover before selection toolbar check

	// Selection toolbar palette & link card via UI clicks
	s.FocusBlock(0)
	s.SelectRange(0, 0, 0, 4)
	tt.Frame()
	clickProbe(t, tt, "tb-palette")
	if _, ok := appUi.TargetBounds("palette-panel"); !ok {
		t.Fatalf("expected palette-panel probe")
	}
	clickProbe(t, tt, "tb-link")
	if _, ok := appUi.TargetBounds("format-link-card"); !ok {
		t.Fatalf("expected format-link-card probe")
	}
	s.SelectRange(0, 0, 0, 0)
	s.closeTransientOverlays()

	// BatchPlan Diff review card
	plan := &model.BatchPlan{
		ID:      "p1",
		Summary: "Update note",
		Ops: []model.PlanOp{
			{
				Kind: model.PlanOpReplace,
				Page: "/tmp/demo.md",
				Old:  "Line A\nLine B\n",
				New:  "Line A\nLine B2\nLine C\n",
			},
		},
	}
	s.SetActivePlan(plan)
	tt.Frame()
	if _, ok := appUi.TargetBounds("plan-diff-card"); !ok {
		t.Fatalf("expected plan-diff-card probe")
	}
	// Uncheck "Line C" (changed line index 2: 0 is -Line B, 1 is +Line B2, 2 is +Line C)
	clickProbe(t, tt, "plan-diff-line-0-2")
	clickProbe(t, tt, "plan-accept")
	if s.ActivePlan() != nil {
		t.Fatalf("expected activePlan cleared after plan-accept")
	}
	md := s.Markdown()
	if !strings.Contains(md, "Line B2") || strings.Contains(md, "Line C") {
		t.Fatalf("expected selective diff applied, got %q", md)
	}

	// Check font sizes >= 14 across theme constants used
	if theme.FontSmallMinSize < 14 || theme.FontBodySize < 16 || theme.EditorStandardWidth != 672 {
		t.Fatalf("unexpected theme constants")
	}
}
