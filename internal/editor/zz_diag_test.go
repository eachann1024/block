package editor

import (
	"fmt"
	"math"
	"testing"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func TestDiagExhaustiveCellCodeParity(t *testing.T) {
	origZoom := theme.Zoom()
	origDark := theme.IsDark()
	origEd, origCd, origLH := theme.Typography()
	defer func() {
		theme.SetZoom(origZoom)
		theme.SetDark(origDark)
		theme.SetTypography(origEd, origCd, origLH)
	}()

	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockParagraph, Spans: []model.Span{
				{Text: "Normal "}, {Text: "code", Marks: model.MarkCode}, {Text: " text"},
			}},
			{
				ID: 2, Kind: model.BlockTable,
				Table: &model.TableData{
					Aligns: []model.ColAlign{model.ColAlignLeft, model.ColAlignCenter, model.ColAlignRight},
					Rows: [][]model.TableCell{
						{
							{Spans: []model.Span{{Text: "c0", Marks: model.MarkCode}}},
							{Spans: []model.Span{{Text: "pre "}, {Text: "c1", Marks: model.MarkCode | model.MarkItalic}, {Text: " post"}}},
							{Spans: []model.Span{{Text: "sub "}, {Text: "c2", Marks: model.MarkCode | model.MarkUnderline | model.MarkStrike}}},
						},
						{
							{Spans: []model.Span{{Text: "math "}, {Text: "x^2", Marks: model.MarkMath}, {Text: " end"}}},
							{Spans: []model.Span{{Text: "sup"}, {Text: "2", Marks: model.MarkSuperscript}, {Text: " "}, {Text: "code", Marks: model.MarkCode}}},
							{Spans: []model.Span{{Text: "fg "}, {Text: "redcode", Marks: model.MarkCode, FGColor: "red"}, {Text: " bg "}, {Text: "bgcode", Marks: model.MarkCode, BGColor: "yellow"}}},
						},
						{
							{Spans: []model.Span{{Text: "c1 ", Marks: model.MarkCode}, {Text: "and"}, {Text: " c2", Marks: model.MarkCode}}},
							{Spans: []model.Span{{Text: "wrap_test "}, {Text: "a_long_inline_code_fragment", Marks: model.MarkCode}, {Text: " after_code"}}},
							{Spans: []model.Span{{Text: "日本語 "}, {Text: "fmt.Println()", Marks: model.MarkCode}, {Text: " です"}}},
						},
					},
				},
			},
			{ID: 3, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Tail"}}},
		},
	}

	// Test every zoom step, typography, and minimal mode
	for _, z := range []float32{0.75, 0.8, 0.9, 1.0, 1.1, 1.25, 1.5} {
		for _, minimal := range []bool{false, true} {
			for _, cd := range []string{"auto", "Menlo", "Monaco"} {
				theme.SetZoom(z)
				theme.SetTypography("Georgia", cd, model.LineHeightTight)

				s := NewSession("", doc, nil, nil)
				s.SetMinimal(minimal)
				tt := ui.NewTester(func(c *ui.Context) {
					uiTheme := theme.Ocean().ToUITheme(theme.DefaultUIFontStack())
					c.SetTheme(&uiTheme)
					s.Render(c, 520, false)
				}, 520, 600)
				tt.SetScale(2)
				tt.Move(-20, -20)
				tt.Frame()

				// Capture baseline cell rects & img
				var baseCR [3][3]ui.Rect
				for r := 0; r < 3; r++ {
					for c := 0; c < 3; c++ {
						baseCR[r][c], _ = s.tableCellRect(2, r, c)
					}
				}
				baseImg := tt.Image()

				// Test every cell: click, hover, select, lose focus
				for r := 0; r < 3; r++ {
					for c := 0; c < 3; c++ {
						cr := baseCR[r][c]
						baseOff := tableCellStartOffset(doc.Blocks[1].Table, r, c)

						// Focus cell
						s.focusedBlock = 1
						s.tableActiveBlock = 2
						s.tableActiveRow = r
						s.tableActiveCol = c
						s.selAnchor = Position{Block: 1, Offset: baseOff}
						s.selHead = s.selAnchor
						s.focusPending = true
						s.selSyncPending = true
						tt.Frame()

						crFoc, _ := s.tableCellRect(2, r, c)
						if math.Abs(float64(crFoc.W-cr.W)) > 0.01 || math.Abs(float64(crFoc.H-cr.H)) > 0.01 ||
							math.Abs(float64(crFoc.X-cr.X)) > 0.01 || math.Abs(float64(crFoc.Y-cr.Y)) > 0.01 {
							fmt.Printf("SHIFT [z=%v min=%v cd=%s] (%d,%d): base=%+v foc=%+v\n",
								z, minimal, cd, r, c, cr, crFoc)
						}

						// Compare image pixels excluding non-text chip tint & caret:
						// check whether text glyphs shifted by checking dark pixel match
						fImg := tt.Image()
						darkMismatch := 0
						x0, y0 := int((cr.X+2)*2), int((cr.Y+2)*2)
						x1, y1 := int((cr.X+cr.W-2)*2), int((cr.Y+cr.H-2)*2)
						for y := y0; y < y1; y++ {
							for x := x0; x < x1; x++ {
								ca, cb := baseImg.RGBAAt(x, y), fImg.RGBAAt(x, y)
								da := ca.R < 160 && ca.G < 160 && ca.B < 160
								db := cb.R < 160 && cb.G < 160 && cb.B < 160
								if da != db {
									darkMismatch++
								}
							}
						}
						// Caret at 2x is ~4x34=136 px max; allow up to 160 for caret + AA
						if darkMismatch > 180 {
							fmt.Printf("GLYPH SHIFT [z=%v min=%v cd=%s] (%d,%d): darkMismatch=%d\n",
								z, minimal, cd, r, c, darkMismatch)
						}
					}
				}
			}
		}
	}
	_ = appUi.RichCodeEdgeNone
}
