package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"block/internal/markdown"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestCopyAndCutAllRichTextBlockKinds(t *testing.T) {
	richKinds := []struct {
		name    string
		kind    model.BlockKind
		callout model.CalloutKind
		checked bool
	}{
		{"Paragraph", model.BlockParagraph, "", false},
		{"Heading1", model.BlockHeading1, "", false},
		{"Heading2", model.BlockHeading2, "", false},
		{"Heading3", model.BlockHeading3, "", false},
		{"Heading4", model.BlockHeading4, "", false},
		{"Heading5", model.BlockHeading5, "", false},
		{"Heading6", model.BlockHeading6, "", false},
		{"Bullet", model.BlockBullet, "", false},
		{"Numbered", model.BlockNumbered, "", false},
		{"Todo", model.BlockTodo, "", true},
		{"Quote", model.BlockQuote, "", false},
		{"Callout", model.BlockCallout, model.CalloutTip, false},
	}

	for _, tc := range richKinds {
		t.Run(tc.name, func(t *testing.T) {
			doc := &model.Doc{
				Blocks: []model.Block{
					{
						ID:      1,
						Kind:    tc.kind,
						Callout: tc.callout,
						Checked: tc.checked,
						Spans: []model.Span{
							{Text: "prefix "},
							{Text: "agent-desktop", Marks: model.MarkBold},
							{Text: " suffix"},
						},
					},
				},
			}
			s := NewSession("/tmp/rich-copy.md", doc, nil, nil)
			s.FocusBlock(0)
			s.SelectRange(0, 7, 0, 20) // "agent-desktop" (bold)

			tt := ui.NewTester(makeSessionView(s, false), 900, 500)

			// macOS Edit -> Copy (RoleCopy / Cmd+C sends InputCommand("copy"))
			tt.SetClipboard("stale")
			tt.Command("copy")
			if got := tt.Clipboard(); got != "**agent-desktop**" {
				t.Fatalf("%s: Command(copy) got %q, want %q", tc.name, got, "**agent-desktop**")
			}

			// Direct Cmd+C key event (Windows/Linux or non-menu path)
			tt.SetClipboard("stale")
			tt.Key(ui.Cmd, ui.KeyC)
			if got := tt.Clipboard(); got != "**agent-desktop**" {
				t.Fatalf("%s: Key(Cmd+C) got %q, want %q", tc.name, got, "**agent-desktop**")
			}

			// macOS Edit -> Cut must both write Markdown to clipboard and delete selection.
			tt.SetClipboard("stale")
			tt.Command("cut")
			if got := tt.Clipboard(); got != "**agent-desktop**" {
				t.Fatalf("%s: Command(cut) clipboard got %q, want %q", tc.name, got, "**agent-desktop**")
			}
			if got := s.doc.Blocks[0].PlainText(); got != "prefix  suffix" {
				t.Fatalf("%s: Command(cut) remaining text got %q, want %q", tc.name, got, "prefix  suffix")
			}
		})
	}
}

func TestCopyAndCutCodeAndRawHTMLBlocks(t *testing.T) {
	t.Run("NormalCodeBlock", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{ID: 1, Kind: model.BlockCode, Language: "go", Code: "fmt.Println(\"hello\")"},
			},
		}
		s := NewSession("/tmp/code.md", doc, nil, nil)
		s.FocusBlock(0)
		s.SelectRange(0, 4, 0, 11) // "Println"

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "Println" {
			t.Fatalf("Code Command(copy) got %q, want %q", got, "Println")
		}

		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "Println" {
			t.Fatalf("Code Key(Cmd+C) got %q, want %q", got, "Println")
		}
	})

	t.Run("RawHTMLBlock", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{ID: 1, Kind: model.BlockRawHTML, Code: "<div class=\"card\">hi</div>"},
			},
		}
		s := NewSession("/tmp/raw.md", doc, nil, nil)
		s.FocusBlock(0)
		s.SelectRange(0, 18, 0, 20) // "hi"

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "hi" {
			t.Fatalf("RawHTML Command(copy) got %q, want %q", got, "hi")
		}

		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "hi" {
			t.Fatalf("RawHTML Key(Cmd+C) got %q, want %q", got, "hi")
		}
	})

	t.Run("PreviewOnlyMermaidCodeBlock", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{ID: 1, Kind: model.BlockCode, Language: "mermaid", Code: "graph TD\nA-->B"},
				{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "after"}}},
			},
		}
		s := NewSession("/tmp/mermaid.md", doc, nil, nil)
		s.FocusBlock(0) // atomic focus without range selection

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); !strings.Contains(got, "```mermaid") || !strings.Contains(got, "A-->B") {
			t.Fatalf("PreviewOnlyCode focused Command(copy) got %q", got)
		}

		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); !strings.Contains(got, "```mermaid") || !strings.Contains(got, "A-->B") {
			t.Fatalf("PreviewOnlyCode focused Key(Cmd+C) got %q", got)
		}

		// Whole-block selection copy
		s.SelectRange(0, 0, 0, s.blockSelectionLen(0))
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); !strings.Contains(got, "```mermaid") || !strings.Contains(got, "A-->B") {
			t.Fatalf("PreviewOnlyCode selected Command(copy) got %q", got)
		}

		// Cut deletes block after copying
		tt.SetClipboard("stale")
		tt.Command("cut")
		if got := tt.Clipboard(); !strings.Contains(got, "```mermaid") {
			t.Fatalf("PreviewOnlyCode Command(cut) clipboard got %q", got)
		}
		if len(s.doc.Blocks) != 1 || s.doc.Blocks[0].PlainText() != "after" {
			t.Fatalf("PreviewOnlyCode Command(cut) left blocks %+v", s.doc.Blocks)
		}
	})
}

func TestCopyAndCutAtomicBlocksDividerAndImageAndVideo(t *testing.T) {
	cases := []struct {
		name string
		blk  model.Block
		want string
	}{
		{
			name: "Divider",
			blk:  model.Block{ID: 1, Kind: model.BlockDivider},
			want: "---",
		},
		{
			name: "Image",
			blk:  model.Block{ID: 2, Kind: model.BlockImage, ImageAlt: "diagram", ImageSrc: "assets/pic.png"},
			want: "![diagram](assets/pic.png)",
		},
		{
			name: "Video",
			blk:  model.Block{ID: 3, Kind: model.BlockImage, ImageAlt: "demo", ImageSrc: "assets/demo.mp4"},
			want: "![demo](assets/demo.mp4)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &model.Doc{
				Blocks: []model.Block{
					tc.blk,
					{ID: 99, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "tail"}}},
				},
			}
			s := NewSession("/tmp/atomic.md", doc, nil, nil)
			s.FocusBlock(0) // focused atomic block (no range selection)

			tt := ui.NewTester(makeSessionView(s, false), 900, 500)

			tt.SetClipboard("stale")
			tt.Command("copy")
			if got := tt.Clipboard(); got != tc.want {
				t.Fatalf("%s focused Command(copy) got %q, want %q", tc.name, got, tc.want)
			}

			tt.SetClipboard("stale")
			tt.Key(ui.Cmd, ui.KeyC)
			if got := tt.Clipboard(); got != tc.want {
				t.Fatalf("%s focused Key(Cmd+C) got %q, want %q", tc.name, got, tc.want)
			}

			// Whole-block selection
			s.SelectRange(0, 0, 0, s.blockSelectionLen(0))
			tt.Frame()
			tt.SetClipboard("stale")
			tt.Command("copy")
			if got := tt.Clipboard(); got != tc.want {
				t.Fatalf("%s selected Command(copy) got %q, want %q", tc.name, got, tc.want)
			}

			// Cut copies and removes the block
			tt.SetClipboard("stale")
			tt.Command("cut")
			if got := tt.Clipboard(); got != tc.want {
				t.Fatalf("%s Command(cut) clipboard got %q, want %q", tc.name, got, tc.want)
			}
			if len(s.doc.Blocks) != 1 || s.doc.Blocks[0].PlainText() != "tail" {
				t.Fatalf("%s Command(cut) left blocks %+v", tc.name, s.doc.Blocks)
			}
		})
	}
}

func TestCopyAndCutTableAllSelectionModes(t *testing.T) {
	makeTableDoc := func() *model.Doc {
		return &model.Doc{
			Blocks: []model.Block{
				{
					ID:   10,
					Kind: model.BlockTable,
					Table: &model.TableData{
						Aligns: []model.ColAlign{model.ColAlignNone, model.ColAlignNone},
						Rows: [][]model.TableCell{
							{
								{Spans: []model.Span{{Text: "Hi "}, {Text: "Bold", Marks: model.MarkBold}}},
								{Spans: []model.Span{{Text: "Code", Marks: model.MarkCode}}},
							},
							{
								{Spans: []model.Span{{Text: "Row2A"}}},
								{Spans: []model.Span{{Text: "Row2B"}}},
							},
						},
					},
				},
			},
		}
	}

	t.Run("SingleCellFormatted", func(t *testing.T) {
		doc := makeTableDoc()
		s := NewSession("/tmp/table.md", doc, nil, nil)
		s.focusedBlock = 0
		s.tableActiveBlock = 10
		s.tableActiveRow = 0
		s.tableActiveCol = 0
		s.focusPending = true
		s.SelectRange(0, 3, 0, 7) // "Bold" inside cell (0,0)

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "**Bold**" {
			t.Fatalf("Table single cell Command(copy) got %q, want %q", got, "**Bold**")
		}

		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "**Bold**" {
			t.Fatalf("Table single cell Key(Cmd+C) got %q, want %q", got, "**Bold**")
		}

		tt.SetClipboard("stale")
		tt.Command("cut")
		if got := tt.Clipboard(); got != "**Bold**" {
			t.Fatalf("Table single cell Command(cut) clipboard got %q, want %q", got, "**Bold**")
		}
		if got := s.doc.Blocks[0].Table.Rows[0][0].PlainText(); got != "Hi " {
			t.Fatalf("Table single cell Command(cut) remaining cell text got %q, want %q", got, "Hi ")
		}
	})

	t.Run("MultiCellPartialSelection", func(t *testing.T) {
		doc := makeTableDoc()
		s := NewSession("/tmp/table-multi.md", doc, nil, nil)
		tbl := doc.Blocks[0].Table
		startOff := tableCellStartOffset(tbl, 0, 1) // start of cell (0,1) "`Code`"
		endOff := tableCellStartOffset(tbl, 1, 0) + len([]rune("Row2A"))
		s.SelectRange(0, startOff, 0, endOff)

		if !s.hasMultiCellTableSelection() {
			t.Fatalf("expected hasMultiCellTableSelection() == true")
		}
		if !s.needsDetachedSelectionFocus() {
			t.Fatalf("expected needsDetachedSelectionFocus() == true for multi-cell table selection")
		}

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "`Code`\nRow2A" {
			t.Fatalf("Table multi-cell Command(copy) got %q, want %q", got, "`Code`\nRow2A")
		}

		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "`Code`\nRow2A" {
			t.Fatalf("Table multi-cell Key(Cmd+C) got %q, want %q", got, "`Code`\nRow2A")
		}
	})

	t.Run("WholeTableSelection", func(t *testing.T) {
		doc := makeTableDoc()
		s := NewSession("/tmp/table-whole.md", doc, nil, nil)
		s.SelectRange(0, 0, 0, s.blockSelectionLen(0))

		tt := ui.NewTester(makeSessionView(s, false), 900, 500)

		tt.SetClipboard("stale")
		tt.Command("copy")
		got := tt.Clipboard()
		if !strings.Contains(got, "| Hi **Bold** | `Code` |") || !strings.Contains(got, "| Row2A | Row2B |") {
			t.Fatalf("Whole table Command(copy) got %q", got)
		}
	})

	t.Run("TableHandleMenuCopyShortcut", func(t *testing.T) {
		doc := makeTableDoc()
		s := NewSession("/tmp/table-menu.md", doc, nil, nil)
		s.focusedBlock = 0
		tt := ui.NewTester(makeSessionView(s, false), 900, 500)
		// The Edit menu's copy command reaches overlay shortcuts only on macOS;
		// elsewhere the key itself comes first.
		menuCopy := func() {
			if runtime.GOOS == "darwin" {
				tt.Command("copy")
			} else {
				tt.Key(ui.Cmd, ui.KeyC)
			}
		}

		// Row handle menu selected -> both Command(copy) (macOS Cmd+C) and Key(Cmd+C) copy row with inline Markdown.
		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 0
		tt.Frame()
		tt.SetClipboard("stale")
		menuCopy()
		if got := tt.Clipboard(); got != "Hi **Bold**\t`Code`" {
			t.Fatalf("Table row menu Command(copy) got %q, want %q", got, "Hi **Bold**\t`Code`")
		}

		// Column handle menu selected -> both Command(copy) and Key(Cmd+C) copy column values.
		s.tableMenuBlock = 10
		s.tableMenuKind = "col"
		s.tableMenuIndex = 1
		tt.Frame()
		tt.SetClipboard("stale")
		menuCopy()
		if got := tt.Clipboard(); got != "`Code`\nRow2B" {
			t.Fatalf("Table col menu Command(copy) got %q, want %q", got, "`Code`\nRow2B")
		}

		// Whole table handle menu selected -> both Command(copy) and Key(Cmd+C) copy Markdown table.
		s.tableMenuBlock = 10
		s.tableMenuKind = "table"
		s.tableMenuIndex = 0
		tt.Frame()
		tt.SetClipboard("stale")
		menuCopy()
		if got := tt.Clipboard(); !strings.Contains(got, "| Hi **Bold** | `Code` |") {
			t.Fatalf("Table whole menu Command(copy) got %q", got)
		}

		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 0
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "Hi **Bold**\t`Code`" {
			t.Fatalf("Table row menu Key(Cmd+C) got %q, want %q", got, "Hi **Bold**\t`Code`")
		}
	})
}

func TestCopyCutPasteMultiBlockAndReadOnlyMode(t *testing.T) {
	t.Run("MultiBlockCopyCutAndPlainPaste", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{ID: 1, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "agent-desktop"}}},
				{ID: 2, Kind: model.BlockBullet, Spans: []model.Span{{Text: "item ", Marks: 0}, {Text: "one", Marks: model.MarkItalic}}},
				{ID: 3, Kind: model.BlockDivider},
				{ID: 4, Kind: model.BlockImage, ImageAlt: "shot", ImageSrc: "assets/s.png"},
				{ID: 5, Kind: model.BlockCode, Language: "sh", Code: "echo ok"},
			},
		}
		s := NewSession("/tmp/multi.md", doc, nil, nil)
		s.SelectAllBlocks()

		tt := ui.NewTester(makeSessionView(s, false), 900, 600)

		tt.SetClipboard("stale")
		tt.Command("copy")
		got := tt.Clipboard()
		for _, want := range []string{"## agent-desktop", "- item *one*", "---", "![shot](assets/s.png)", "```sh", "echo ok"} {
			if !strings.Contains(got, want) {
				t.Fatalf("Multi-block Command(copy) missing %q in:\n%s", want, got)
			}
		}

		// Paste plain text over multi-block selection must read clipboard via ui.Services (not stale Context).
		tt.SetClipboard("replaced-plain")
		tt.Command("pasteAndMatchStyle")
		if len(s.doc.Blocks) != 1 || s.doc.Blocks[0].PlainText() != "replaced-plain" {
			t.Fatalf("Multi-block pasteAndMatchStyle got blocks %+v", s.doc.Blocks)
		}
	})

	t.Run("ReadOnlySingleBlockCopy", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{ID: 1, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "agent-desktop"}}},
				{ID: 2, Kind: model.BlockCode, Language: "go", Code: "return 42"},
				{ID: 3, Kind: model.BlockImage, ImageAlt: "pic", ImageSrc: "assets/a.png"},
			},
		}
		s := NewSession("/tmp/readonly.md", doc, nil, nil)
		tt := ui.NewTester(makeSessionView(s, true), 900, 500)

		// Single heading selection in read-only mode
		s.SelectRange(0, 0, 0, len([]rune("agent-desktop")))
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "agent-desktop" {
			t.Fatalf("ReadOnly heading Command(copy) got %q, want %q", got, "agent-desktop")
		}

		// Single code selection in read-only mode
		s.SelectRange(1, 0, 1, len([]rune("return 42")))
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "return 42" {
			t.Fatalf("ReadOnly code Key(Cmd+C) got %q, want %q", got, "return 42")
		}

		// Image block selection in read-only mode
		s.SelectRange(2, 0, 2, s.blockSelectionLen(2))
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Command("copy")
		if got := tt.Clipboard(); got != "![pic](assets/a.png)" {
			t.Fatalf("ReadOnly image Command(copy) got %q, want %q", got, "![pic](assets/a.png)")
		}
	})
}

func TestCanvasDragUnfocusedHeadingThenCommandCopy(t *testing.T) {
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "intro paragraph"}}},
			{ID: 2, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "agent-desktop"}}},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "body text"}}},
		},
	}
	s := NewSession("/tmp/unfocused-drag.md", doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	// Mimic opening a non-empty note (no block focused) and dragging across heading "agent-desktop".
	r, ok := s.blockTextRect(2)
	if !ok || r.W <= 0 {
		t.Fatalf("expected blockTextRect for heading block 2, got %+v ok=%v", r, ok)
	}
	y := r.Y + r.H*0.5
	tt.Press(r.X+1, y)
	tt.Move(r.X+r.W-1, y)
	tt.Release(r.X+r.W-1, y)
	tt.Frame()

	if !s.IsSelectionToolbarOpen() {
		t.Fatalf("expected selection toolbar open after dragging heading")
	}

	tt.SetClipboard("stale")
	tt.Command("copy")
	if got := tt.Clipboard(); got != "agent-desktop" {
		t.Fatalf("Unfocused heading drag + Command(copy) got %q, want %q", got, "agent-desktop")
	}

	// Verify Cmd+Shift+V inside RichInput pastes literally without interpreting Markdown.
	tt.SetClipboard("**literal**")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyV)
	if got := s.doc.Blocks[1].PlainText(); got != "**literal**" {
		t.Fatalf("RichInput Cmd+Shift+V got plain text %q, want %q", got, "**literal**")
	}
	if len(s.doc.Blocks[1].Spans) != 1 || s.doc.Blocks[1].Spans[0].Marks != 0 {
		t.Fatalf("RichInput Cmd+Shift+V should not apply Markdown marks, got spans %+v", s.doc.Blocks[1].Spans)
	}
}

func TestMultiFormatClipboardHTMLMarkdownAndLocalImagePNG(t *testing.T) {
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	imgPath := filepath.Join(assetsDir, "tiny.png")
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 2))
	rgba.Set(0, 0, color.RGBA{R: 12, G: 34, B: 56, A: 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, rgba); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(imgPath, pngBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	notePath := filepath.Join(dir, "note.md")
	doc := &model.Doc{
		Blocks: []model.Block{
			{
				ID:   1,
				Kind: model.BlockHeading2,
				Spans: []model.Span{
					{Text: "Hello "},
					{Text: "World", Marks: model.MarkBold},
				},
			},
			{
				ID:   2,
				Kind: model.BlockBullet,
				Spans: []model.Span{
					{Text: "item "},
					{Text: "code", Marks: model.MarkCode},
				},
			},
			{
				ID:       3,
				Kind:     model.BlockImage,
				ImageAlt: "tiny",
				ImageSrc: "assets/tiny.png",
			},
		},
	}
	s := NewSession(notePath, doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	// 1. Single-block inline selection writes inline HTML (no <p> wrapper) + Markdown + PlainText.
	s.FocusBlock(0)
	s.SelectRange(0, 6, 0, 11) // "World"
	tt.Frame()
	tt.Command("copy")
	data := tt.ClipboardData()
	htmlRaw, err := data.Read(transfer.HTML)
	if err != nil || string(htmlRaw) != "<strong>World</strong>" {
		t.Fatalf("inline copy HTML got %q (err=%v), want %q", string(htmlRaw), err, "<strong>World</strong>")
	}
	mdRaw, err := data.Read(transfer.Markdown)
	if err != nil || string(mdRaw) != "**World**" {
		t.Fatalf("inline copy Markdown got %q (err=%v), want %q", string(mdRaw), err, "**World**")
	}

	// 2. Multi-block selection writes semantic HTML blocks (<h2>, <ul><li>) alongside Markdown.
	s.SelectRange(0, 0, 1, s.blockSelectionLen(1))
	tt.Frame()
	tt.Command("copy")
	multiHTML, err := tt.ClipboardData().Read(transfer.HTML)
	if err != nil || !strings.Contains(string(multiHTML), "<h2>Hello <strong>World</strong></h2>") || !strings.Contains(string(multiHTML), "<ul><li>item <code>code</code></li></ul>") {
		t.Fatalf("multi-block copy HTML got %q (err=%v)", string(multiHTML), err)
	}

	// 3. Single local image block writes Markdown text, HTML <img>, AND raw PNG bytes.
	s.FocusBlock(2)
	tt.Frame()
	tt.Command("copy")
	if got := tt.Clipboard(); got != "![tiny](assets/tiny.png)" {
		t.Fatalf("image copy plain text got %q", got)
	}
	imgClip := tt.ClipboardData()
	gotPNG, err := imgClip.Read(transfer.PNG)
	if _, decodeErr := png.Decode(bytes.NewReader(gotPNG)); err != nil || decodeErr != nil {
		t.Fatalf("image copy expected valid PNG bytes, got len=%d err=%v", len(gotPNG), err)
	}
	gotImgHTML, err := imgClip.Read(transfer.HTML)
	if err != nil || !strings.Contains(string(gotImgHTML), "<img ") {
		t.Fatalf("image copy expected <img> HTML, got %q err=%v", string(gotImgHTML), err)
	}
}

func TestCodeBlockCommandUndoRedoUsesSessionStack(t *testing.T) {
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockCode, Language: "go", Code: "v1"},
		},
	}
	s := NewSession("/tmp/code-undo.md", doc, nil, nil)
	s.FocusBlock(0)
	s.SelectRange(0, 2, 0, 2)
	tt := ui.NewTester(makeSessionView(s, false), 900, 500)

	// Type inside code block so both TextAreaBase and Session record an edit.
	tt.Type("2")
	if got := s.doc.Blocks[0].Code; got != "v12" {
		t.Fatalf("expected code %q after typing, got %q", "v12", got)
	}

	// Simulate macOS Edit -> Undo menu command.
	tt.Command("undo")
	if got := s.doc.Blocks[0].Code; got != "v1" {
		t.Fatalf("expected Command(undo) in code block to restore %q, got %q", "v1", got)
	}

	// Simulate macOS Edit -> Redo menu command.
	tt.Command("redo")
	if got := s.doc.Blocks[0].Code; got != "v12" {
		t.Fatalf("expected Command(redo) in code block to restore %q, got %q", "v12", got)
	}
}

func TestDetachedSelectionIMECompositionAndCaretPlacement(t *testing.T) {
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "first paragraph"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "second paragraph"}}},
		},
	}
	s := NewSession("/tmp/ime-detached.md", doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	s.SelectAllBlocks()
	tt.Frame()

	// Caret for detached IME should sit near block 1 (not top-left 0,0).
	caret, ok := tt.TextCaret()
	if !ok || (caret.X == 0 && caret.Y == 0) {
		t.Fatalf("expected detached IME caret near selection, got %+v ok=%v", caret, ok)
	}

	// Preedit composition should be accepted and shown rather than rejected.
	tt.Compose("nihao", 5)
	if s.detachedCompose != "nihao" {
		t.Fatalf("expected detachedCompose %q, got %q", "nihao", s.detachedCompose)
	}

	// Committing Chinese text replaces the multi-block selection cleanly.
	tt.Type("你好")
	if s.detachedCompose != "" {
		t.Fatalf("expected detachedCompose cleared after commit, got %q", s.detachedCompose)
	}
	if len(s.doc.Blocks) != 1 || s.doc.Blocks[0].PlainText() != "你好" {
		t.Fatalf("expected committed IME text to replace selection, got blocks %+v", s.doc.Blocks)
	}
}

func TestEditorCanvasContextMenuOnMultiBlockAndReadOnly(t *testing.T) {
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockHeading2, Spans: []model.Span{{Text: "Title"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Body"}}},
		},
	}
	s := NewSession("/tmp/ctx-menu.md", doc, nil, nil)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	s.SelectAllBlocks()
	tt.Frame()

	r, ok := s.blockRect(1)
	if !ok {
		t.Fatalf("missing block 1 rect")
	}
	tt.RightClickAt(r.X+20, r.Y+r.H*0.5)
	items := tt.Menu()
	if len(items) == 0 {
		t.Fatalf("expected context menu on multi-block selection")
	}
	tt.SetClipboard("stale")
	copyLbl := theme.Tr("拷贝", "Copy")
	if err := tt.ChooseMenuItem(copyLbl); err != nil {
		t.Fatalf("ChooseMenuItem(%q) failed (menu=%v): %v", copyLbl, items, err)
	}
	if got := tt.Clipboard(); !strings.Contains(got, "## Title") || !strings.Contains(got, "Body") {
		t.Fatalf("context menu copy got %q", got)
	}

	// Read-only mode context menu also exposes Copy without Cut/Paste.
	sRO := NewSession("/tmp/ctx-ro.md", doc.Clone(), nil, nil)
	ttRO := ui.NewTester(makeSessionView(sRO, true), 900, 600)
	sRO.SelectRange(0, 0, 0, 5)
	ttRO.Frame()
	rRO, _ := sRO.blockRect(1)
	ttRO.RightClickAt(rRO.X+20, rRO.Y+rRO.H*0.5)
	roItems := ttRO.Menu()
	if len(roItems) == 0 {
		t.Fatalf("expected context menu in read-only mode")
	}
	cutLbl := theme.Tr("剪切", "Cut")
	for _, it := range roItems {
		if it == cutLbl {
			t.Fatalf("read-only context menu should not show Cut, got %v", roItems)
		}
	}
	ttRO.SetClipboard("stale")
	if err := ttRO.ChooseMenuItem(copyLbl); err != nil {
		t.Fatalf("read-only ChooseMenuItem(%q) failed: %v", copyLbl, err)
	}
	if got := ttRO.Clipboard(); got != "Title" {
		t.Fatalf("read-only context menu copy got %q, want %q", got, "Title")
	}
}

func TestWindowsLinuxCUAClipboardShortcuts(t *testing.T) {
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "alpha "}, {Text: "beta", Marks: model.MarkBold}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "gamma"}}},
		},
	}
	s := NewSession("/tmp/cua.md", doc, nil, nil)
	s.FocusBlock(0)
	s.SelectRange(0, 6, 0, 10) // "beta"
	tt := ui.NewTester(makeSessionView(s, false), 900, 500)

	// Ctrl+Insert copies Markdown in RichInput
	tt.SetClipboard("stale")
	tt.Key(ui.Ctrl, ui.KeyInsert)
	if got := tt.Clipboard(); got != "**beta**" {
		t.Fatalf("RichInput Ctrl+Insert got %q, want %q", got, "**beta**")
	}

	// Shift+Delete cuts in RichInput
	tt.SetClipboard("stale")
	tt.Key(ui.Shift, ui.KeyDelete)
	if got := tt.Clipboard(); got != "**beta**" {
		t.Fatalf("RichInput Shift+Delete clipboard got %q, want %q", got, "**beta**")
	}
	if got := s.doc.Blocks[0].PlainText(); got != "alpha " {
		t.Fatalf("RichInput Shift+Delete remaining text got %q, want %q", got, "alpha ")
	}

	// Shift+Insert pastes in RichInput
	tt.SetClipboard("delta")
	tt.Key(ui.Shift, ui.KeyInsert)
	if got := s.doc.Blocks[0].PlainText(); got != "alpha delta" {
		t.Fatalf("RichInput Shift+Insert got %q, want %q", got, "alpha delta")
	}

	// Ctrl+Insert in multi-block detached selection
	s.SelectAllBlocks()
	tt.Frame()
	tt.SetClipboard("stale")
	tt.Key(ui.Ctrl, ui.KeyInsert)
	if got := tt.Clipboard(); !strings.Contains(got, "alpha delta") || !strings.Contains(got, "gamma") {
		t.Fatalf("Multi-block Ctrl+Insert got %q", got)
	}
}

func TestKeyDownCmdXAndStagedTableCmdANotHijackedByGlobalShortcuts(t *testing.T) {
	// 1. Single rich text block Key(Cmd+X) must cut Markdown (Windows/Linux Ctrl+X path).
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "keep "}, {Text: "cutme", Marks: model.MarkBold}}},
			{ID: 2, Kind: model.BlockDivider},
			{
				ID:   3,
				Kind: model.BlockTable,
				Table: &model.TableData{
					Aligns: []model.ColAlign{model.ColAlignNone, model.ColAlignNone},
					Rows: [][]model.TableCell{
						{{Spans: []model.Span{{Text: "CellA"}}}, {Spans: []model.Span{{Text: "CellB"}}}},
						{{Spans: []model.Span{{Text: "CellC"}}}, {Spans: []model.Span{{Text: "CellD"}}}},
					},
				},
			},
		},
	}
	s := NewSession("/tmp/hijack.md", doc, nil, nil)
	s.FocusBlock(0)
	s.SelectRange(0, 5, 0, 10)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	tt.SetClipboard("stale")
	tt.Key(ui.Cmd, ui.KeyX)
	if got := tt.Clipboard(); got != "**cutme**" {
		t.Fatalf("RichInput Key(Cmd+X) clipboard got %q, want %q", got, "**cutme**")
	}
	if got := s.doc.Blocks[0].PlainText(); got != "keep " {
		t.Fatalf("RichInput Key(Cmd+X) remaining text got %q, want %q", got, "keep ")
	}

	// 2. Focused atomic Divider Key(Cmd+X) must copy "---" and delete divider.
	s.FocusBlock(1)
	tt.Frame()
	tt.SetClipboard("stale")
	tt.Key(ui.Cmd, ui.KeyX)
	if got := tt.Clipboard(); got != "---" {
		t.Fatalf("Atomic Divider Key(Cmd+X) clipboard got %q, want %q", got, "---")
	}
	if len(s.doc.Blocks) != 2 {
		t.Fatalf("Atomic Divider Key(Cmd+X) expected 2 remaining blocks, got %d", len(s.doc.Blocks))
	}

	// 3. Inside a table cell, first Key(Cmd+A) selects ONLY the cell, second selects whole table.
	tblIdx := 1
	s.focusedBlock = tblIdx
	s.tableActiveBlock = 3
	s.tableActiveRow = 0
	s.tableActiveCol = 0
	s.selAnchor = Position{Block: tblIdx, Offset: 1}
	s.selHead = Position{Block: tblIdx, Offset: 1}
	s.focusPending = true
	s.selSyncPending = true
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyA)
	a, b := s.orderedSelection()
	if a.Block != tblIdx || b.Block != tblIdx || a.Offset != 0 || b.Offset != len([]rune("CellA")) {
		t.Fatalf("First Key(Cmd+A) in table cell should select only CellA [0..5], got [%d:%d..%d:%d]", a.Block, a.Offset, b.Block, b.Offset)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	a, b = s.orderedSelection()
	if a.Block != tblIdx || b.Block != tblIdx || a.Offset != 0 || b.Offset != s.blockSelectionLen(tblIdx) {
		t.Fatalf("Second Key(Cmd+A) in table cell should select whole table, got [%d:%d..%d:%d]", a.Block, a.Offset, b.Block, b.Offset)
	}
}

func TestMultiCellTablePasteAndArrowCollapseFocus(t *testing.T) {
	makeDoc := func() *model.Doc {
		return &model.Doc{
			Blocks: []model.Block{
				{
					ID:   10,
					Kind: model.BlockTable,
					Table: &model.TableData{
						Aligns: []model.ColAlign{model.ColAlignNone, model.ColAlignNone},
						Rows: [][]model.TableCell{
							{{Spans: []model.Span{{Text: "A1"}}}, {Spans: []model.Span{{Text: "B1"}}}},
							{{Spans: []model.Span{{Text: "A2"}}}, {Spans: []model.Span{{Text: "B2"}}}},
						},
					},
				},
			},
		}
	}

	// 1. Plain paste over multi-cell selection clears all selected cells and restores cell focus.
	s := NewSession("/tmp/tbl-paste-plain.md", makeDoc(), nil, nil)
	tbl := s.doc.Blocks[0].Table
	startOff := tableCellStartOffset(tbl, 0, 1) // B1
	endOff := tableCellStartOffset(tbl, 1, 0) + len([]rune("A2"))
	s.SelectRange(0, startOff, 0, endOff)
	tt := ui.NewTester(makeSessionView(s, false), 900, 500)

	tt.SetClipboard("NEW")
	tt.Command("pasteAndMatchStyle")
	if s.needsDetachedSelectionFocus() {
		t.Fatalf("expected detached selection focus cleared after multi-cell plain paste")
	}
	if got := s.doc.Blocks[0].Table.Rows[0][1].PlainText(); got != "NEW" {
		t.Fatalf("expected B1=%q, got %q", "NEW", got)
	}
	if got := s.doc.Blocks[0].Table.Rows[1][0].PlainText(); got != "" {
		t.Fatalf("expected A2 cleared, got %q", got)
	}

	// 2. TSV paste over multi-cell selection pastes from top-left selected cell and collapses selection.
	s2 := NewSession("/tmp/tbl-paste-tsv.md", makeDoc(), nil, nil)
	tbl2 := s2.doc.Blocks[0].Table
	s2.SelectRange(0, tableCellStartOffset(tbl2, 0, 0), 0, tableCellStartOffset(tbl2, 0, 1)+2)
	tt2 := ui.NewTester(makeSessionView(s2, false), 900, 500)

	tt2.SetClipboard("X1\tY1")
	tt2.Command("paste")
	if s2.needsDetachedSelectionFocus() {
		t.Fatalf("expected detached selection focus cleared after multi-cell TSV paste")
	}
	if s2.doc.Blocks[0].Table.Rows[0][0].PlainText() != "X1" || s2.doc.Blocks[0].Table.Rows[0][1].PlainText() != "Y1" {
		t.Fatalf("unexpected TSV paste result: %q, %q", s2.doc.Blocks[0].Table.Rows[0][0].PlainText(), s2.doc.Blocks[0].Table.Rows[0][1].PlainText())
	}

	// 3. Arrow key collapse on multi-cell selection syncs tableActiveBlock/Row/Col.
	s3 := NewSession("/tmp/tbl-collapse.md", makeDoc(), nil, nil)
	tbl3 := s3.doc.Blocks[0].Table
	s3.SelectRange(0, tableCellStartOffset(tbl3, 0, 1), 0, tableCellStartOffset(tbl3, 1, 0)+2)
	tt3 := ui.NewTester(makeSessionView(s3, false), 900, 500)
	tt3.Key(0, ui.KeyLeft)
	if s3.tableActiveBlock != 10 || s3.tableActiveRow != 0 || s3.tableActiveCol != 1 {
		t.Fatalf("expected Left collapse to focus cell (0,1), got block=%d row=%d col=%d", s3.tableActiveBlock, s3.tableActiveRow, s3.tableActiveCol)
	}
}

func TestTableRectangularCopyCutPasteFidelityAndReadOnly(t *testing.T) {
	codec := markdown.NewCodec()

	t.Run("PartialRowsColsAndEmptyCells", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{
					ID:   10,
					Kind: model.BlockTable,
					Table: &model.TableData{
						Aligns: []model.ColAlign{model.ColAlignLeft, model.ColAlignCenter, model.ColAlignRight},
						Rows: [][]model.TableCell{
							{{Spans: []model.Span{{Text: "H1"}}}, {Spans: nil}, {Spans: []model.Span{{Text: "H3"}}}},
							{{Spans: []model.Span{{Text: "A2"}}}, {Spans: []model.Span{{Text: "B2"}}}, {Spans: nil}},
							{{Spans: []model.Span{{Text: "A3"}}}, {Spans: []model.Span{{Text: "B3"}}}, {Spans: []model.Span{{Text: "C3"}}}},
						},
					},
				},
			},
		}
		s := NewSession("/tmp/tbl-partial.md", doc, codec, nil)
		tbl := doc.Blocks[0].Table

		// Select rows 0..1 including trailing empty cell (1,2), excluding row 2.
		end12 := tableCellStartOffset(tbl, 1, 2)
		s.SelectRange(0, 0, 0, end12)
		if got := s.SelectedMarkdown(); got != "H1\t\tH3\nA2\tB2\t" {
			t.Fatalf("SelectedMarkdown partial rows got %q, want %q", got, "H1\t\tH3\nA2\tB2\t")
		}
		snap := s.SelectedDoc()
		if snap == nil || len(snap.Blocks) != 1 || snap.Blocks[0].Table == nil {
			t.Fatalf("expected sliced table block, got %+v", snap)
		}
		st := snap.Blocks[0].Table
		if len(st.Rows) != 2 || len(st.Rows[0]) != 3 {
			t.Fatalf("expected 2x3 sliced table preserving empty col 2, got %dx%d", len(st.Rows), len(st.Rows[0]))
		}

		// Boundary check: ending exactly at cStart of (1,0) must not include (1,0) when (1,0) is non-empty.
		endRow0 := tableCellStartOffset(tbl, 1, 0)
		s.SelectRange(0, 0, 0, endRow0)
		if got := s.SelectedMarkdown(); got != "H1\t\tH3" {
			t.Fatalf("SelectedMarkdown row 0 boundary got %q, want %q", got, "H1\t\tH3")
		}

		// Body-only slice sets NoHeaderRow.
		s.SelectRange(0, tableCellStartOffset(tbl, 1, 0), 0, tableCellStartOffset(tbl, 2, 2)+2)
		bodySnap := s.SelectedDoc()
		if bodySnap == nil || !bodySnap.Blocks[0].Table.NoHeaderRow {
			t.Fatalf("expected body-only sliced table to set NoHeaderRow=true")
		}
	})

	t.Run("SpacesAndMenuCutPasteWithCodec", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{
					ID:   10,
					Kind: model.BlockTable,
					Table: &model.TableData{
						Aligns: []model.ColAlign{model.ColAlignNone, model.ColAlignCenter},
						Rows: [][]model.TableCell{
							{
								{Spans: []model.Span{{Text: "   "}}, Align: model.ColAlignRight, Tint: model.TableTintOcean},
								{Spans: []model.Span{{Text: "  "}, {Text: "Hi", Marks: model.MarkBold}, {Text: "  "}}},
							},
							{
								{Spans: []model.Span{{Text: "X"}}},
								{Spans: nil},
							},
						},
					},
				},
			},
		}
		s := NewSession("/tmp/tbl-spaces.md", doc, codec, nil)
		s.focusedBlock = 0
		var svc ui.Services
		tt := ui.NewTester(func(c *ui.Context) {
			svc = c.Services()
			makeSessionView(s, false)(c)
		}, 900, 500)

		// Out-of-bounds column copy returns false without writing clipboard.
		if s.tableCopyMenuSelection(svc, 0, "col", 0, 5) {
			t.Fatalf("expected out-of-bounds col copy to return false")
		}

		// Copy row 0 (spaces-only + leading/trailing spaces + bold) and paste into row 1 via Cmd+V on row menu.
		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 0
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "   \t  **Hi**  " {
			t.Fatalf("Row copy with spaces got %q, want %q", got, "   \t  **Hi**  ")
		}

		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 1
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyV)
		r1 := s.doc.Blocks[0].Table.Rows[1]
		if r1[0].PlainText() != "   " || r1[1].PlainText() != "  Hi  " || len(r1[1].Spans) < 2 || r1[1].Spans[1].Marks&model.MarkBold == 0 {
			t.Fatalf("Row paste did not preserve spaces/marks: %+v", r1)
		}

		// Copy col 1 (has trailing empty cell in a 2-row table after clearing (1,1)) and paste into col 0.
		s.doc.Blocks[0].Table.Rows[1][1].Spans = nil
		s.tableMenuBlock = 10
		s.tableMenuKind = "col"
		s.tableMenuIndex = 1
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyC)

		s.tableActiveBlock = 10
		s.tableActiveRow = 0
		s.tableActiveCol = 0
		s.selAnchor = Position{Block: 0, Offset: 0}
		s.selHead = s.selAnchor
		s.PasteFromClipboard(svc, tt.Clipboard(), false)
		c0r0 := s.doc.Blocks[0].Table.Rows[0][0]
		c0r1 := s.doc.Blocks[0].Table.Rows[1][0]
		if c0r0.PlainText() != "  Hi  " || c0r1.PlainText() != "" {
			t.Fatalf("Col paste with trailing empty cell got (%q, %q), want (%q, %q)", c0r0.PlainText(), c0r1.PlainText(), "  Hi  ", "")
		}
		if c0r0.Align != model.ColAlignRight || c0r0.Tint != model.TableTintOcean {
			t.Fatalf("TSV paste overwrote destination cell Align/Tint: %+v", c0r0)
		}

		// PastePlainText into table must not parse Markdown markers.
		s.tableActiveBlock = 10
		s.tableActiveRow = 0
		s.tableActiveCol = 0
		s.PastePlainText("**raw**\t*lit*")
		if got := s.doc.Blocks[0].Table.Rows[0][0].PlainText(); got != "**raw**" {
			t.Fatalf("PastePlainText parsed markdown in cell: %q", got)
		}

		// Menu Cmd+X cuts row and deletes it when rows > 1.
		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 1
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyX)
		if len(s.doc.Blocks[0].Table.Rows) != 1 {
			t.Fatalf("Menu Cmd+X expected 1 remaining row, got %d", len(s.doc.Blocks[0].Table.Rows))
		}
	})

	t.Run("ReadOnlyBlocksAllTableMutations", func(t *testing.T) {
		doc := &model.Doc{
			Blocks: []model.Block{
				{
					ID:   10,
					Kind: model.BlockTable,
					Table: &model.TableData{
						Rows: [][]model.TableCell{
							{{Spans: []model.Span{{Text: "A1"}}}, {Spans: []model.Span{{Text: "B1"}}}},
							{{Spans: []model.Span{{Text: "A2"}}}, {Spans: []model.Span{{Text: "B2"}}}},
						},
					},
				},
			},
		}
		s := NewSession("/tmp/tbl-ro.md", doc, codec, nil)
		tbl := doc.Blocks[0].Table
		s.SelectRange(0, 0, 0, tableCellStartOffset(tbl, 1, 0)+2)
		tt := ui.NewTester(makeSessionView(s, true), 900, 500)

		// Cut in read-only mode copies selection without modifying cells.
		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyX)
		if got := tt.Clipboard(); got != "A1\tB1\nA2" {
			t.Fatalf("ReadOnly table multi-cell Cmd+X got %q, want %q", got, "A1\tB1\nA2")
		}
		if tbl.Rows[0][0].PlainText() != "A1" || tbl.Rows[1][0].PlainText() != "A2" {
			t.Fatalf("ReadOnly table Cmd+X mutated cells")
		}

		// Paste & direct table mutations are ignored in read-only mode.
		s.PasteText("Z1\tZ2")
		s.PastePlainText("Z1\tZ2")
		s.tableDeleteRow(10, 0)
		s.tableClearRow(10, 0)
		if len(tbl.Rows) != 2 || tbl.Rows[0][0].PlainText() != "A1" {
			t.Fatalf("ReadOnly mutations changed table: %+v", tbl.Rows)
		}

		// Menu Cmd+X in read-only copies row without deleting it.
		s.tableMenuBlock = 10
		s.tableMenuKind = "row"
		s.tableMenuIndex = 0
		tt.Frame()
		tt.SetClipboard("stale")
		tt.Key(ui.Cmd, ui.KeyX)
		if got := tt.Clipboard(); got != "A1\tB1" {
			t.Fatalf("ReadOnly table menu Cmd+X got %q, want %q", got, "A1\tB1")
		}
		if len(tbl.Rows) != 2 || tbl.Rows[0][0].PlainText() != "A1" {
			t.Fatalf("ReadOnly table menu Cmd+X mutated table")
		}
	})
}
