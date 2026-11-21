package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"block/internal/markdown"
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
				blocks := s.Doc().Blocks
				if len(blocks) != 2 || blocks[0].Kind != model.BlockDivider || !isPlainEmptyParagraph(&blocks[1]) {
					t.Fatalf("expected divider followed by an empty paragraph, got %+v", blocks)
				}
				if s.FocusedBlockIndex() != 1 {
					t.Fatalf("expected focus on the empty paragraph, got block %d", s.FocusedBlockIndex())
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
	if rail.X > 60 {
		t.Fatalf("expected outline-rail in left margin (x=18), got x=%v", rail.X)
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

	// Auto-hide when left margin < 36px (e.g. 800px viewport with 744px column -> 21px margin)
	appUi.ResetSmoke()
	tt.SetSize(800, 700)
	if _, ok := appUi.TargetBounds("outline-rail"); ok {
		t.Fatalf("expected outline-rail hidden when left margin < 36px")
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
	s := NewSession("/tmp/demo.md", doc, markdown.NewCodec(), nil)
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
	const original = "Line A\nLine B\n"
	s.SetDoc(markdown.ParseMarkdown(original))
	plan := &model.BatchPlan{
		ID:      "p1",
		Summary: "Update note",
		Ops: []model.PlanOp{
			{
				Kind: model.PlanOpReplace,
				Page: "/tmp/demo.md",
				Old:  original,
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
	if theme.FontBody < 14 || theme.FontEditor < 15 || theme.EditorStandardWidth != 672 {
		t.Fatalf("unexpected theme constants")
	}
}

func TestSelectionToolbarAdaptiveMoreFoldingAndBlockCapabilities(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Selectable rich text"}}},
			{ID: 2, Kind: model.BlockCode, Language: "go", Code: "fmt.Println(1)"},
			{ID: 3, Kind: model.BlockDivider},
		},
	}
	s := NewSession("/tmp/toolbar.md", doc, nil, nil)
	s.FocusBlock(0)
	s.SelectRange(0, 0, 0, 10)
	// Narrow viewport (320px): toolbar stays single-row and folds tail items into tb-more.
	tt := ui.NewTester(makeSessionView(s, false), 320, 480)
	tt.Frame()
	barRect, ok := appUi.TargetBounds("selection-toolbar")
	if !ok {
		t.Fatalf("expected selection-toolbar bounds at 320px")
	}
	if barRect.H > 40 {
		t.Fatalf("expected single-row selection-toolbar (H<=40), got H=%v", barRect.H)
	}
	if _, ok := appUi.TargetBounds("tb-more"); !ok {
		t.Fatalf("expected tb-more overflow button in narrow viewport")
	}
	if _, ok := appUi.TargetBounds("tb-link"); ok {
		t.Fatalf("expected tb-link folded out of inline row at 320px")
	}
	moreR, _ := appUi.TargetBounds("tb-more")
	clickProbe(t, tt, "tb-more")
	if _, ok := appUi.TargetBounds("toolbar-more-menu"); !ok {
		t.Fatalf("expected toolbar-more-menu after clicking tb-more (moreR=%+v barR=%+v pop=%q selOpen=%v)",
			moreR, barRect, s.toolbarPop, s.IsSelectionToolbarOpen())
	}
	// Click Bold inside overflow menu if bold was folded, or toggle via menu item.
	if r, ok := appUi.TargetBounds("toolbar-more-item-0"); ok {
		tt.ClickAt(r.X+r.W*0.5, r.Y+r.H*0.5)
	}

	// Code block selection: hides rich-text & block-type buttons, keeps comment/reaction/ai.
	s.closeTransientOverlays()
	tt.SetSize(900, 500)
	s.FocusBlock(1)
	s.SelectRange(1, 0, 1, 5)
	appUi.ResetSmoke()
	tt.Frame()
	if !s.IsSelectionToolbarOpen() {
		t.Fatalf("expected selection toolbar open for code block")
	}
	if _, ok := appUi.TargetBounds("tb-comment"); !ok {
		t.Fatalf("expected tb-comment on code block")
	}
	if _, ok := appUi.TargetBounds("tb-bold"); ok {
		t.Fatalf("expected tb-bold hidden on code block")
	}
	if _, ok := appUi.TargetBounds("tb-type"); ok {
		t.Fatalf("expected tb-type hidden on code block")
	}

	// Divider block does not open text selection toolbar.
	s.SelectRange(2, 0, 2, 3)
	if s.IsSelectionToolbarOpen() {
		t.Fatalf("expected selection toolbar disabled on divider block")
	}
}

func TestInlineAIPromptChipsStarLimitAndCancelRestore(t *testing.T) {
	appUi.ResetSmoke()
	dataDir := t.TempDir()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Original sentence here"}}},
			{ID: 2, Kind: model.BlockParagraph},
			{ID: 3, Kind: model.BlockTable, Table: &model.TableData{
				Rows: [][]model.TableCell{
					{{Spans: []model.Span{{Text: "Header"}}}, {Spans: []model.Span{{Text: "Value"}}}},
					{{Spans: []model.Span{{Text: "CellText"}}}, {Spans: []model.Span{{Text: "123"}}}},
				},
			}},
		},
	}
	s := NewSession("/tmp/ai.md", doc, markdown.NewCodec(), nil)
	s.SetDataDir(dataDir)

	// 1) Empty block /ai -> seeded empty starred presets.
	s.FocusBlock(1)
	tt := ui.NewTester(makeSessionView(s, false), 900, 650)
	tt.Type("/ai")
	tt.Key(0, ui.KeyEnter)
	if !s.IsInlineAIOpen() {
		t.Fatalf("expected inline AI open from /ai slash command")
	}
	if _, ok := appUi.TargetBounds("inline-ai"); !ok {
		t.Fatalf("expected inline-ai panel probe")
	}
	emptyEntries := s.ensurePromptStore().list(PromptScopeEmpty)
	if len(emptyEntries) != 4 || !emptyEntries[0].Starred || emptyEntries[0].Preset != PresetContinue {
		t.Fatalf("unexpected seeded empty presets: %+v", emptyEntries)
	}

	// Unstar first empty preset -> moves behind starred items; add custom prompts past 3 rows -> prunes trailing unstarred.
	clickProbe(t, tt, "ai-saved-star-0")
	afterUnstar := s.ensurePromptStore().list(PromptScopeEmpty)
	if afterUnstar[len(afterUnstar)-1].Preset != PresetContinue || afterUnstar[len(afterUnstar)-1].Starred {
		t.Fatalf("expected unstarred preset moved to back, got %+v", afterUnstar)
	}
	for i := 0; i < 16; i++ {
		s.ensurePromptStore().record(PromptScopeEmpty, fmt.Sprintf("CustomPromptNumber%02d", i))
	}
	capped := s.ensurePromptStore().list(PromptScopeEmpty)
	rows := chipRowIndices(capped)
	for i, it := range capped {
		if !it.Starred && rows[i] >= inlineAIMaxChipRows {
			t.Fatalf("unstarred chip %d (%q) exceeded MAX_CHIP_ROWS=%d (row=%d)", i, it.ShortLabel(), inlineAIMaxChipRows, rows[i])
		}
	}
	// Verify persisted to ai-inline-prompts.json and reloaded in a fresh session.
	if _, err := os.Stat(filepath.Join(dataDir, "ai-inline-prompts.json")); err != nil {
		t.Fatalf("expected ai-inline-prompts.json persisted: %v", err)
	}
	s2 := NewSession("/tmp/ai2.md", doc.Clone(), nil, nil)
	s2.SetDataDir(dataDir)
	reloaded := s2.ensurePromptStore().list(PromptScopeEmpty)
	if len(reloaded) != len(capped) {
		t.Fatalf("reloaded prompt count mismatch: %d vs %d", len(reloaded), len(capped))
	}
	s.aiClose()

	// 2) Thinking cancel via Esc restores user's typed instruction.
	release := make(chan struct{})
	s.SetInlineAIResponder(func(ctx, orig, draft, instruction string) string {
		<-release
		return "NeverApplied"
	})
	s.SelectRange(0, 0, 0, len([]rune("Original sentence here")))
	tt.Frame()
	clickProbe(t, tt, "tb-ai")
	if !s.IsInlineAIOpen() {
		t.Fatalf("expected inline AI open from tb-ai")
	}
	tt.Type("Make it crisp and clear")
	tt.Key(0, ui.KeyEnter)
	if s.inlineAI == nil || s.inlineAI.Status != aiStatusThinking {
		t.Fatalf("expected Thinking status while responder is blocked")
	}
	if _, ok := appUi.TargetBounds("ai-stop"); !ok {
		t.Fatalf("expected ai-stop button visible during Thinking")
	}
	tt.Key(0, ui.KeyEscape)
	if s.inlineAI == nil || s.inlineAI.Status != aiStatusInput {
		t.Fatalf("expected Esc during Thinking to return to Input status")
	}
	if s.inlineAI.InputText != "Make it crisp and clear" {
		t.Fatalf("expected typed prompt restored on cancel, got %q", s.inlineAI.InputText)
	}
	close(release)
	s.aiClose()

	// 3) Fast responder -> native block preview -> Enter accepts via ReplaceAIMarkdown & Insert Below.
	s.SetInlineAIResponder(func(ctx, orig, draft, instruction string) string {
		return "### Polished\n- Item A"
	})
	s.SelectRange(0, 0, 0, len([]rune("Original sentence here")))
	tt.Frame()
	clickProbe(t, tt, "tb-ai")
	tt.Type("Polish into list")
	tt.Key(0, ui.KeyEnter)
	if s.inlineAI == nil || s.inlineAI.Status != aiStatusReview {
		t.Fatalf("expected Review status after fast responder")
	}
	if _, ok := appUi.TargetBounds("inline-ai-preview"); !ok {
		t.Fatalf("expected inline-ai-preview on paper")
	}
	if !tt.HasText("Polished") || !tt.HasText("Item A") {
		t.Fatalf("expected native parsed preview blocks rendered on paper")
	}
	// Accept with empty Enter.
	tt.Key(0, ui.KeyEnter)
	if s.IsInlineAIOpen() {
		t.Fatalf("expected inline AI closed after Enter accept")
	}
	if s.Doc().Blocks[0].Kind != model.BlockHeading3 || s.Doc().Blocks[1].Kind != model.BlockBullet {
		t.Fatalf("expected ReplaceAIMarkdown to produce H3 + Bullet, got %+v", s.Doc().Blocks[:2])
	}

	// Table cell inline ReplaceAIMarkdown vs structural rejection.
	tblIdx := len(s.Doc().Blocks) - 1
	tblText := s.Doc().Blocks[tblIdx].PlainText()
	cellOff := strings.Index(tblText, "CellText")
	if cellOff < 0 {
		t.Fatalf("missing CellText in %q", tblText)
	}
	s.SelectRange(tblIdx, cellOff, tblIdx, cellOff+len("CellText"))
	if s.ReplaceAIMarkdown("# HeadingNotAllowedInCell") {
		t.Fatalf("expected structural markdown rejected inside table cell slice")
	}
	if !s.ReplaceAIMarkdown("**UpdatedCell**") {
		t.Fatalf("expected inline markdown applied inside table cell slice")
	}
	if s.Doc().Blocks[tblIdx].Table.Rows[1][0].PlainText() != "UpdatedCell" {
		t.Fatalf("unexpected cell text: %q", s.Doc().Blocks[tblIdx].Table.Rows[1][0].PlainText())
	}
}

func TestSelectedMarkdownAndSelectedDoc(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{
				ID:   1,
				Kind: model.BlockHeading1,
				Spans: []model.Span{
					{Text: "Hello "},
					{Text: "World", Marks: model.MarkBold},
					{Text: " "},
					{Text: "code", Marks: model.MarkCode},
					{Text: " "},
					{Text: "site", Link: "https://example.com"},
				},
			},
			{ID: 2, Kind: model.BlockBullet, Indent: 4, Spans: []model.Span{{Text: "Parent", Marks: model.MarkItalic}}},
			{ID: 3, Kind: model.BlockBullet, Indent: 6, Spans: []model.Span{{Text: "Child"}}},
			{ID: 4, Kind: model.BlockTable, Table: &model.TableData{
				Rows: [][]model.TableCell{
					{{Spans: []model.Span{{Text: "A", Marks: model.MarkBold}}}, {Spans: []model.Span{{Text: "B"}}}, {Spans: []model.Span{{Text: "C"}}}},
					{{Spans: []model.Span{{Text: "D"}}}, {Spans: []model.Span{{Text: "E"}}}, {Spans: []model.Span{{Text: "F"}}}},
				},
			}},
		},
	}
	s := NewSession("/tmp/copy.md", doc, nil, nil)
	s.FocusBlock(0)
	if s.SelectedDoc() != nil || s.SelectedMarkdown() != "" {
		t.Fatalf("expected nil SelectedDoc and empty SelectedMarkdown without selection")
	}

	// Single-block inline selection preserves bold/code/link without heading prefix.
	s.SelectRange(0, 6, 0, len([]rune(doc.Blocks[0].PlainText())))
	md := s.SelectedMarkdown()
	if strings.HasPrefix(md, "# ") || !strings.Contains(md, "**World**") || !strings.Contains(md, "`code`") || !strings.Contains(md, "[site](https://example.com)") {
		t.Fatalf("unexpected inline SelectedMarkdown: %q", md)
	}

	// Multi-block selection normalizes orphan indentation (4->0, 6->2).
	s.SelectRange(1, 0, 2, 5)
	snap := s.SelectedDoc()
	if snap == nil || len(snap.Blocks) != 2 {
		t.Fatalf("expected 2 blocks in SelectedDoc, got %+v", snap)
	}
	if snap.Blocks[0].Indent != 0 || snap.Blocks[1].Indent != 2 {
		t.Fatalf("expected normalized indents [0, 2], got [%d, %d]", snap.Blocks[0].Indent, snap.Blocks[1].Indent)
	}
	multiMD := s.SelectedMarkdown()
	if !strings.Contains(multiMD, "*Parent*") || !strings.Contains(multiMD, "  - Child") {
		t.Fatalf("unexpected multi-block SelectedMarkdown: %q", multiMD)
	}

	// Table partial multi-cell slice.
	tblPlain := doc.Blocks[3].PlainText()
	bIdx := strings.Index(tblPlain, "B")
	dIdx := strings.Index(tblPlain, "D")
	s.SelectRange(3, bIdx, 3, dIdx+1)
	tblSnap := s.SelectedDoc()
	if tblSnap == nil || len(tblSnap.Blocks) != 1 || tblSnap.Blocks[0].Kind != model.BlockTable || len(tblSnap.Blocks[0].Table.Rows) != 2 {
		t.Fatalf("expected sliced table doc, got %+v", tblSnap)
	}

	// Cmd+C copies Markdown formatting via tester.
	s.FocusBlock(0)
	s.SelectRange(0, 6, 0, 11) // "World" (Bold)
	tt := ui.NewTester(makeSessionView(s, false), 900, 500)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "**World**" {
		t.Fatalf("expected Cmd+C to copy **World**, got %q", got)
	}
}

func TestListDepthMarkersAndGuides(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 10, Kind: model.BlockBullet, Indent: 0, Spans: []model.Span{{Text: "Root bullet"}}},
			{ID: 11, Kind: model.BlockBullet, Indent: 2, Spans: []model.Span{{Text: "Depth 1 bullet"}}},
			{ID: 12, Kind: model.BlockBullet, Indent: 4, Spans: []model.Span{{Text: "Depth 2 bullet"}}},
			{ID: 13, Kind: model.BlockParagraph, Indent: 4, Spans: []model.Span{{Text: "Continuation paragraph"}}},
			{ID: 14, Kind: model.BlockNumbered, Indent: 0, Spans: []model.Span{{Text: "First numbered"}}},
			{ID: 15, Kind: model.BlockNumbered, Indent: 2, Spans: []model.Span{{Text: "Nested numbered"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	_ = ui.NewTester(makeSessionView(s, false), 900, 600)

	if _, ok := appUi.TargetBounds("list-bullet-10"); !ok {
		t.Fatalf("expected root bullet marker")
	}
	if r, ok := appUi.TargetBounds("list-bullet-10"); !ok || r.W != 6 {
		t.Fatalf("expected depth 0 bullet diameter 6px, got %+v", r)
	}
	if r, ok := appUi.TargetBounds("list-bullet-12"); !ok || r.W != 5 {
		t.Fatalf("expected depth 2 bullet diameter 5px, got %+v", r)
	}
	if _, ok := appUi.TargetBounds("list-guide-11-0"); !ok {
		t.Fatalf("expected ancestor guide line on depth 1 bullet")
	}
	if _, ok := appUi.TargetBounds("list-guide-12-1"); !ok {
		t.Fatalf("expected two ancestor guide lines on depth 2 bullet")
	}
	if _, ok := appUi.TargetBounds("list-guide-13-0"); !ok {
		t.Fatalf("expected ancestor guide line on indented continuation paragraph")
	}
	if _, ok := appUi.TargetBounds("list-number-14"); !ok {
		t.Fatalf("expected numbered marker")
	}
	if _, ok := appUi.TargetBounds("list-stem-10"); !ok {
		t.Fatalf("expected downward guide stem on parent bullet 10")
	}

	// Wrapped multi-line parent bullet + loose blank line must connect seamlessly to child guides.
	appUi.ResetSmoke()
	longParent := "Parent bullet that wraps onto multiple lines when rendered inside standard editor width: " +
		"~/Library/Application Support/Block/ ; $XDG_DATA_HOME/Block/ ; %APPDATA%\\Block\\ ; extra words to force wrapping."
	wrappedDoc := &model.Doc{
		Blocks: []model.Block{
			{ID: 20, Kind: model.BlockBullet, Indent: 0, Spans: []model.Span{{Text: longParent}}},
			{ID: 21, Kind: model.BlockBullet, Indent: 2, Spans: []model.Span{{Text: "First nested child"}}},
			{ID: 22, Kind: model.BlockParagraph, Indent: 0},
			{ID: 23, Kind: model.BlockBullet, Indent: 2, Spans: []model.Span{{Text: "Second nested child after blank line"}}},
			{ID: 24, Kind: model.BlockParagraph, Indent: 0},
			{ID: 25, Kind: model.BlockBullet, Indent: 0, Spans: []model.Span{{Text: longParent}}},
		},
	}
	ws := NewSession("", wrappedDoc, nil, nil)
	_ = ui.NewTester(makeSessionView(ws, false), 600, 600)

	stem20, ok := appUi.TargetBounds("list-stem-20")
	if !ok {
		t.Fatalf("expected list-stem-20 on wrapped parent bullet")
	}
	guide21, ok := appUi.TargetBounds("list-guide-21-0")
	if !ok {
		t.Fatalf("expected list-guide-21-0 on first child")
	}
	if stem20.X != guide21.X {
		t.Fatalf("expected parent stem X (%v) == child guide X (%v)", stem20.X, guide21.X)
	}
	if diff := stem20.Y + stem20.H - guide21.Y; diff < -0.5 || diff > 0.5 {
		t.Fatalf("expected parent stem bottom (%v) to meet child guide top (%v) without gap", stem20.Y+stem20.H, guide21.Y)
	}
	if _, ok := appUi.TargetBounds("list-guide-22-0"); !ok {
		t.Fatalf("expected loose-list blank line 22 to bridge parent guide")
	}
	if _, ok := appUi.TargetBounds("list-guide-23-0"); !ok {
		t.Fatalf("expected child 23 after loose blank line to retain parent guide")
	}
	if _, ok := appUi.TargetBounds("list-guide-24-0"); ok {
		t.Fatalf("expected trailing blank line 24 outside list subtree to have no guide")
	}
	if _, ok := appUi.TargetBounds("list-stem-25"); ok {
		t.Fatalf("expected leaf multi-line bullet 25 without children to have no stem")
	}
}

func TestEmptyLineHintBandKeycapsAndOverlaySuppressesHandles(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph},
			{ID: 2, Kind: model.BlockHeading1},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Body line"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 500)

	if _, ok := appUi.TargetBounds("empty-line-band"); !ok {
		t.Fatalf("expected empty-line-band on focused empty paragraph")
	}
	if _, ok := appUi.TargetBounds("empty-hint-menu-keys"); !ok {
		t.Fatalf("expected empty-hint-menu-keys on wide empty paragraph")
	}

	// Narrow column drops keycaps to avoid crowding hint text.
	appUi.ResetSmoke()
	tt.SetSize(250, 500)
	if _, ok := appUi.TargetBounds("empty-hint-menu-keys"); ok {
		t.Fatalf("expected keycaps hidden in narrow 250px viewport")
	}

	// Empty heading shows Heading 1 placeholder without menu keycaps.
	tt.SetSize(900, 500)
	s.FocusBlock(1)
	appUi.ResetSmoke()
	tt.Frame()
	if !tt.HasText(theme.Tr("标题 1", "Heading 1")) {
		t.Fatalf("expected empty heading hint text")
	}
	if _, ok := appUi.TargetBounds("empty-hint-menu-keys"); ok {
		t.Fatalf("expected no menu keycaps on empty heading")
	}

	// The active block shows its handle when the pointer enters its gutter.
	s.FocusBlock(2)
	tt.Frame()
	body, ok := appUi.TargetBounds("block-3")
	if !ok {
		t.Fatal("expected active block bounds")
	}
	appUi.ResetSmoke()
	tt.Move(body.X+body.W/2, body.Y+body.H/2)
	tt.Type("x")
	if s.Doc().Blocks[2].PlainText() == "Body line" {
		t.Fatal("expected typing to edit the active block")
	}
	if _, ok := appUi.TargetBounds("block-grip-3"); ok {
		t.Fatal("expected no block handle while editing text")
	}
	appUi.ResetSmoke()
	tt.Move(body.X+s.handleWidth()/2, body.Y+body.H/2)
	if _, ok := appUi.TargetBounds("block-grip-3"); !ok {
		t.Fatal("expected block-grip-3 when hovering the gutter")
	}
	appUi.ResetSmoke()
	s.OpenFind()
	tt.Frame()
	if _, ok := appUi.TargetBounds("block-grip-3"); ok {
		t.Fatalf("expected block-grip-3 suppressed while find overlay is open")
	}
	_ = time.Millisecond
}

func TestCalloutFiveStateHeaderMenuAndImageResize(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockCallout, Callout: model.CalloutNote, Spans: []model.Span{{Text: "Watch "}, {Text: "inline", Marks: model.MarkCode}}},
			{ID: 2, Kind: model.BlockImage, ImageSrc: "missing.png", ImageAlt: "Cover", ImageAlign: model.ImageAlignLeft},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 700)

	// Open 5-state Callout menu via header pill click.
	clickProbe(t, tt, "callout-pill")
	if _, ok := appUi.TargetBounds("callout-kind-menu"); !ok {
		t.Fatalf("expected callout-kind-menu open")
	}
	clickProbe(t, tt, "callout-opt-3") // Warning
	if s.Doc().Blocks[0].Callout != model.CalloutWarning {
		t.Fatalf("expected CalloutWarning after opt-3 click, got %q", s.Doc().Blocks[0].Callout)
	}

	// Re-open and convert to plain paragraph.
	clickProbe(t, tt, "callout-icon")
	clickProbe(t, tt, "callout-opt-plain")
	if s.Doc().Blocks[0].Kind != model.BlockParagraph {
		t.Fatalf("expected converted to BlockParagraph, got %v", s.Doc().Blocks[0].Kind)
	}

	// Image block defaults to full width and shows left/right resize handles when focused.
	s.FocusBlock(1)
	tt.Frame()
	if _, ok := appUi.TargetBounds("img-resize-left"); !ok {
		t.Fatalf("expected img-resize-left probe on focused image")
	}
	rightR, ok := appUi.TargetBounds("img-resize-right")
	if !ok {
		t.Fatalf("expected img-resize-right probe on focused image")
	}
	tt.Press(rightR.X+rightR.W*0.5, rightR.Y+rightR.H*0.5)
	tt.Move(rightR.X+rightR.W*0.5-120, rightR.Y+rightR.H*0.5)
	tt.Release(rightR.X+rightR.W*0.5-120, rightR.Y+rightR.H*0.5)
	if s.Doc().Blocks[1].ImageWidth <= 0 || s.Doc().Blocks[1].ImageWidth >= 100 {
		t.Fatalf("expected resized ImageWidth in (0, 100), got %v", s.Doc().Blocks[1].ImageWidth)
	}
}

func TestEditorZeroHoverAndFocusJitter(t *testing.T) {
	appUi.ResetSmoke()
	origEd, origCd, origLH := theme.Typography()
	defer theme.SetTypography(origEd, origCd, origLH)

	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "Title H1"}}},
			{ID: 2, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "Section H2"}}},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Body paragraph with text"}}},
			{ID: 4, Kind: model.BlockParagraph},
			{ID: 5, Kind: model.BlockBullet, Indent: 0, Spans: []model.Span{{Text: "Bullet item"}}},
			{ID: 6, Kind: model.BlockBullet, Indent: 1, Spans: []model.Span{{Text: "Nested bullet"}}},
			{ID: 7, Kind: model.BlockNumbered, Indent: 0, Spans: []model.Span{{Text: "Numbered item"}}},
			{ID: 8, Kind: model.BlockTodo, Checked: true, Spans: []model.Span{{Text: "Todo item"}}},
			{ID: 9, Kind: model.BlockQuote, Spans: []model.Span{{Text: "Quoted text"}}},
			{ID: 10, Kind: model.BlockCallout, Callout: model.CalloutTip, Spans: []model.Span{{Text: "Callout body"}}},
			{ID: 11, Kind: model.BlockCode, Language: "go", Code: "fmt.Println(\"hi\")"},
			{ID: 12, Kind: model.BlockCode, Language: "mermaid", Code: "flowchart LR\nA --> B"},
			{ID: 13, Kind: model.BlockTable, Table: &model.TableData{
				Rows: [][]model.TableCell{
					{{Spans: []model.Span{{Text: "H1"}}}, {Spans: []model.Span{{Text: "H2"}}}},
					{{Spans: []model.Span{{Text: "C1"}}}, {Spans: []model.Span{{Text: "C2"}}}},
				},
			}},
			{ID: 14, Kind: model.BlockDivider},
			{ID: 15, Kind: model.BlockImage, ImageSrc: "missing.png", ImageAlt: "Hero"},
		},
	}

	s := NewSession("", doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 1080, 960)
	tt.Move(-20, -20)
	tt.Frame()

	captureAll := func(phase string) map[model.BlockID]ui.Rect {
		out := make(map[model.BlockID]ui.Rect, len(doc.Blocks))
		for _, b := range doc.Blocks {
			r, ok := appUi.TargetBounds(fmt.Sprintf("block-%d", b.ID))
			if !ok {
				t.Fatalf("%s: missing bounds for block-%d", phase, b.ID)
			}
			out[b.ID] = r
		}
		return out
	}

	baseline := captureAll("baseline")

	assertMatchesBaseline := func(phase string) {
		t.Helper()
		for _, b := range doc.Blocks {
			base := baseline[b.ID]
			r, ok := appUi.TargetBounds(fmt.Sprintf("block-%d", b.ID))
			if !ok {
				t.Fatalf("%s: missing bounds for block-%d", phase, b.ID)
			}
			if math.Abs(float64(r.Y-base.Y)) >= 0.05 || math.Abs(float64(r.H-base.H)) >= 0.05 {
				t.Fatalf("%s: block-%d (%v) jittered: base=%+v got=%+v", phase, b.ID, b.Kind, base, r)
			}
		}
	}

	// Adversarial hover sweep in 8px increments across entire viewport height.
	for y := float32(24); y <= 920; y += 8 {
		tt.Move(420, y)
		assertMatchesBaseline(fmt.Sprintf("hover-y=%.0f", y))
	}
	tt.Move(-20, -20)

	// Focus sweep across every block type (including empty paragraph, code, mermaid, table, divider, image).
	for i, b := range doc.Blocks {
		s.FocusBlock(i)
		tt.Frame()
		assertMatchesBaseline(fmt.Sprintf("focus-block-%d(%v)", b.ID, b.Kind))
	}

	// Verify typography LineHeightMode and EditorFont sync in blockTextMetrics.
	theme.SetTypography("Georgia", "JetBrains Mono", model.LineHeightTight)
	if _, _, lh := blockTextMetrics(model.BlockParagraph); math.Abs(float64(lh-1.35)) > 0.001 {
		t.Fatalf("expected LineHeightTight=1.35, got %v", lh)
	}
	theme.SetTypography("", "", model.LineHeightNormal)
	if _, _, lh := blockTextMetrics(model.BlockParagraph); math.Abs(float64(lh-1.5)) > 0.001 {
		t.Fatalf("expected LineHeightNormal=1.5, got %v", lh)
	}
	theme.SetTypography("", "", model.LineHeightLoose)
	if _, _, lh := blockTextMetrics(model.BlockParagraph); math.Abs(float64(lh-1.75)) > 0.001 {
		t.Fatalf("expected LineHeightLoose=1.75, got %v", lh)
	}
	theme.SetTypography(origEd, origCd, origLH)

	// Verify batch AI annotation rewrite + accept/dismiss flow.
	s.FocusBlock(2)
	s.SelectRange(2, 0, 2, 4) // "Body"
	ann1 := s.AddAnnotation("Make clearer")
	if ann1 == nil {
		t.Fatalf("expected annotation created")
	}
	s.SelectRange(2, 5, 2, 14) // "paragraph"
	ann2 := s.AddAnnotation("Shorten")
	if ann2 == nil {
		t.Fatalf("expected second annotation created")
	}
	s.SelectRange(2, 0, 2, 0)
	s.MarkSaved()

	s.SetAnnotationAIResponder(func(docMarkdown string, pending []*model.Annotation) map[string]model.AnnotationReply {
		if !strings.Contains(docMarkdown, "Body paragraph") || len(pending) != 2 {
			t.Errorf("unexpected responder args: pending=%d", len(pending))
		}
		return map[string]model.AnnotationReply{
			ann1.ID: {Kind: "edit", Text: "Main"},
			ann2.ID: {Kind: "edit", Text: "para"},
		}
	})
	if n := s.GeneratePendingAnnotations(); n != 2 {
		t.Fatalf("expected 2 sent annotations, got %d", n)
	}
	for i := 0; i < 200 && s.annotGenCh != nil; i++ {
		time.Sleep(5 * time.Millisecond)
		s.pollAnnotationGeneration()
	}
	if s.Dirty() {
		t.Fatalf("expected session not dirty until rewrite accepted")
	}
	tt.Frame()
	if _, ok := appUi.TargetBounds("ann-accept-" + ann1.ID); !ok {
		t.Fatalf("expected ann-accept-%s button", ann1.ID)
	}
	if _, ok := appUi.TargetBounds("ann-dismiss-" + ann2.ID); !ok {
		t.Fatalf("expected ann-dismiss-%s button", ann2.ID)
	}

	clickProbe(t, tt, "ann-dismiss-"+ann2.ID)
	if ann2.RewriteResult != "" {
		t.Fatalf("expected ann2 RewriteResult cleared on dismiss, got %q", ann2.RewriteResult)
	}
	clickProbe(t, tt, "ann-accept-"+ann1.ID)
	if !s.Dirty() || !s.CanUndo() {
		t.Fatalf("expected dirty and undoable after accepting annotation rewrite")
	}
	if !strings.HasPrefix(s.Doc().Blocks[2].PlainText(), "Main ") {
		t.Fatalf("expected rewritten block text, got %q", s.Doc().Blocks[2].PlainText())
	}
}

func TestEditorRenderingParity(t *testing.T) {
	appUi.ResetSmoke()
	origEd, origCd, origLH := theme.Typography()
	defer theme.SetTypography(origEd, origCd, origLH)

	// Serif font resolution.
	theme.SetTypography("system-serif", "auto", model.LineHeightLoose)
	if ed := resolvedEditorFont(); !strings.Contains(ed, "LXGW WenKai") || !strings.Contains(ed, "Songti SC") {
		t.Fatalf("unexpected serif resolution: %q", ed)
	}
	if cd := resolvedCodeFont(); !strings.Contains(cd, "Menlo") {
		t.Fatalf("unexpected auto code font resolution: %q", cd)
	}
	theme.SetTypography("", "", model.LineHeightNormal)

	// Heading metrics.
	for _, tc := range []struct {
		kind model.BlockKind
		sz   float32
		w    int
		lh   float32
	}{
		{model.BlockHeading1, 28, theme.WeightTitle, 1.25},
		{model.BlockHeading2, 20, theme.WeightTitle, 1.3},
		{model.BlockHeading3, 17, theme.WeightTitle, 1.35},
		{model.BlockHeading4, 15, theme.WeightTitle, 1.4},
		{model.BlockHeading5, 14, theme.WeightTitle, 1.45},
		{model.BlockHeading6, 14, 500, 1.45},
	} {
		sz, w, lh := blockTextMetrics(tc.kind)
		if sz != tc.sz || w != tc.w || math.Abs(float64(lh-tc.lh)) > 0.001 {
			t.Fatalf("heading %v: got (%v,%d,%v), want (%v,%d,%v)", tc.kind, sz, w, lh, tc.sz, tc.w, tc.lh)
		}
	}

	// Collapsed block gaps.
	h1 := &model.Block{Kind: model.BlockHeading1}
	h2 := &model.Block{Kind: model.BlockHeading2}
	h3 := &model.Block{Kind: model.BlockHeading3}
	p := &model.Block{Kind: model.BlockParagraph}
	q := &model.Block{Kind: model.BlockQuote}
	d := &model.Block{Kind: model.BlockDivider}
	if g := blockGutterSpacing(h1, p); g != 8 {
		t.Fatalf("H1->P gap: got %v, want 8", g)
	}
	if g := blockGutterSpacing(p, h2); math.Abs(float64(g-35.2)) > 0.01 {
		t.Fatalf("P->H2 gap: got %v, want 35.2", g)
	}
	if g := blockGutterSpacing(p, h3); math.Abs(float64(g-25.2)) > 0.01 {
		t.Fatalf("P->H3 gap: got %v, want 25.2", g)
	}
	if g := blockGutterSpacing(p, q); math.Abs(float64(g-12.8)) > 0.01 {
		t.Fatalf("P->Quote gap: got %v, want 12.8", g)
	}
	if g := blockGutterSpacing(p, d); math.Abs(float64(g-16.5)) > 0.01 {
		t.Fatalf("P->Divider gap: got %v, want 16.5", g)
	}

	// Inline code, links, bold spans & input ranges.
	pal := theme.Ocean()
	richBlk := model.Block{
		ID:   2,
		Kind: model.BlockParagraph,
		Spans: []model.Span{
			{Text: "bold", Marks: model.MarkBold},
			{Text: "code", Marks: model.MarkCode},
			{Text: "site", Link: "https://example.com"},
		},
	}
	s := NewSession("", &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Top"}}},
			richBlk,
			{ID: 3, Kind: model.BlockCode, Language: "go", Code: "x := 1"},
		},
	}, nil, nil)
	spans := s.buildDisplaySpans(1, &richBlk, 16, 400)
	if spans[0].Weight != 600 {
		t.Fatalf("bold weight: got %d, want 600", spans[0].Weight)
	}
	if spans[1].Size != 16 || spans[1].Weight != 400 || spans[1].Color != pal.InlineCodeFG || spans[1].PaddingX != 5.5 || spans[1].Radius != 4 {
		t.Fatalf("inline code span mismatch: %+v", spans[1])
	}
	if spans[2].Color != pal.Link || spans[2].Underline {
		t.Fatalf("link span mismatch (expected pal.Link and no default underline): %+v", spans[2])
	}
	ranges := s.buildInputTextRanges(1, &richBlk)
	if ranges[0].Weight != 600 || ranges[1].Color != pal.InlineCodeFG || ranges[2].Color != pal.Link {
		t.Fatalf("input TextRanges mismatch: %+v", ranges)
	}

	// Top padding 64px in rendered layout + clicking unfocused rich block focuses it.
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)
	r1, ok := appUi.TargetBounds("block-1")
	if !ok || math.Abs(float64(r1.Y-64)) > 0.5 {
		t.Fatalf("expected block-1 at Y=64, got %+v (ok=%v)", r1, ok)
	}
	clickProbe(t, tt, "block-2")
	if s.FocusedBlockIndex() != 1 {
		t.Fatalf("expected clicking block-2 to focus index 1, got %d", s.FocusedBlockIndex())
	}
}
