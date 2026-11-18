package markdown

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	"os"
	"strings"
	"time"

	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// ---- PNG & PDF Visual Rendering (fixed ExportScale Paper Styles via mygo/ui) ----

type paintedBlock struct {
	kind       model.BlockKind
	x, y, w, h float32
	textX      float32
	textY      float32
	layout     *ui.TextLayout
	textColor  ui.Color
	bgColor    ui.Color
	borderCol  ui.Color
	accentCol  ui.Color
	labelLay   *ui.TextLayout
	markerLay  *ui.TextLayout
	markerCol  ui.Color
	checked    bool
	table      *paintedTable
	img        *paintedImage
}

type paintedTable struct {
	colWidths []float32
	rows      []paintedTableRow
}

type paintedTableRow struct {
	y, h  float32
	cells []paintedTableCell
}

type paintedTableCell struct {
	x, w      float32
	layout    *ui.TextLayout
	header    bool
	textColor ui.Color
	bgColor   ui.Color
}

type paintedImage struct {
	bitmap *ui.Bitmap
	src    ui.Rect
	dst    ui.Rect
}

type exportScene struct {
	width, height float32
	marginLeft    float32
	contentWidth  float32
	pal           PaperThemeSpec
	legacy        bool
	titleLayout   *ui.TextLayout
	titleX        float32
	titleY        float32
	titleH        float32
	blocks        []paintedBlock
	footerY       float32
	footerLeft    *ui.TextLayout
	footerRight   *ui.TextLayout
	showFooter    bool
}

func toUISpans(spans []model.Span, pal PaperThemeSpec, baseSize float32, isCodeBlock bool) []ui.Span {
	if len(spans) == 0 {
		return []ui.Span{{Text: " "}}
	}
	linkFG := pal.LinkFG()
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		us := ui.Span{
			Text:          sp.Text,
			Size:          baseSize,
			Italic:        sp.Marks&model.MarkItalic != 0,
			Underline:     sp.Marks&model.MarkUnderline != 0,
			Strikethrough: sp.Marks&model.MarkStrike != 0,
		}
		if sp.Marks&model.MarkBold != 0 {
			us.Weight = 700
		}
		if sp.Marks&model.MarkCode != 0 || isCodeBlock {
			us.Font = fontMono
			if !isCodeBlock {
				us.Background = pal.CodeBG
				us.Radius = 4
				us.PaddingX = 3
			}
		}
		if sp.Link != "" {
			us.Color = linkFG
			us.Underline = true
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			us.Color = c
		}
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			us.Background = c
			us.Radius = 3
		}
		out = append(out, us)
	}
	return out
}

func buildExportScene(doc *model.Doc, notePath string, prefs model.ExportPreferences) exportScene {
	pal, legacy := resolveExportPaper(prefs)
	pageW := float32(resolveExportWidth(prefs))
	padTop, padRight, padBottom, padLeft := pal.Margins()
	contentW := pageW - padLeft - padRight

	sc := exportScene{
		width:        pageW,
		marginLeft:   padLeft,
		contentWidth: contentW,
		pal:          pal,
		legacy:       legacy,
	}
	y := padTop

	showTitle := resolveShowTitle(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	if showTitle {
		tWeight := 400
		if pal.TitleBold {
			tWeight = 700
		}
		tFam := pal.HeadingFamily
		if isASCII(title) {
			tFam = pal.TitleFamily
		}
		tFont := ui.Font{Family: tFam, Size: pal.TitleSize, Weight: tWeight, LineHeight: 1.3}
		shape := ui.TextShape{Width: contentW}
		if pal.Mark == TitleMarkDoubleRule {
			shape.Align = ui.Center
		}
		tLay := ui.ShapeRichTextWith([]ui.Span{{Text: title, Font: tFam, Weight: tWeight, Size: pal.TitleSize}}, tFont, shape)
		_, th := tLay.Size()
		sc.titleLayout = tLay
		sc.titleX = padLeft
		sc.titleY = y
		sc.titleH = th
		below := float32(36)
		switch pal.Mark {
		case TitleMarkRule:
			below = 20 + 2 + 36
		case TitleMarkBar:
			below = 18 + 4 + 34
		case TitleMarkWavy:
			below = 8 + 12 + 30
		case TitleMarkDoubleRule:
			below = 18 + 7 + 34
		case TitleMarkBand:
			below = 44 + 52
		}
		if legacy {
			below = 14 + 24
		}
		y += th + below
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := range bodyBlocks {
		b := &bodyBlocks[i]
		ord := num.next(b)
		indent := float32(b.Indent) * 12
		if indent > contentW-120 {
			indent = contentW - 120
		}
		if indent < 0 {
			indent = 0
		}
		bx := padLeft + indent
		avail := contentW - indent

		switch {
		case b.Kind == model.BlockDivider:
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockDivider,
				x:         bx,
				y:         y,
				w:         avail,
				h:         20,
				borderCol: pal.Divider,
			})
			y += 28

		case b.Kind == model.BlockImage:
			if pb, ok := buildPaintedImage(b, notePath, bx, y, avail, pal); ok {
				y += pb.h + 14
				sc.blocks = append(sc.blocks, pb)
			}

		case b.Kind == model.BlockTable && b.Table != nil:
			pb := buildPaintedTable(b.Table, bx, y, avail, pal)
			y += pb.h + 14
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			codeText := b.Code
			if codeText == "" {
				codeText = " "
			}
			font := ui.Font{Family: fontMono, Size: 14.5, LineHeight: 1.6}
			padBoxX, padBoxY := float32(18), float32(14)
			var labelLay *ui.TextLayout
			labelH := float32(0)
			if b.Language != "" {
				langLabel := b.Language
				if strings.EqualFold(langLabel, "yaml-frontmatter") || strings.EqualFold(langLabel, "frontmatter") {
					langLabel = "yaml"
				}
				lf := ui.Font{Size: 13, Weight: 600, LineHeight: 1.3}
				labelLay = ui.ShapeRichTextWith([]ui.Span{{Text: strings.ToUpper(langLabel), Size: 13, Weight: 600}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
				_, lh := labelLay.Size()
				labelH = lh + 6
			}
			lay := ui.ShapeRichTextWith([]ui.Span{{Text: codeText, Size: 14.5, Font: fontMono}}, font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY + labelH
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + labelH,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   pal.CodeBG,
				borderCol: pal.CodeBorder,
				labelLay:  labelLay,
			})
			y += bh + 18

		case b.Kind == model.BlockCallout:
			bg, border, accent, cTitle := pal.CalloutColors(b.Callout)
			padBoxX, padBoxY := float32(18), float32(12)
			lf := ui.Font{Size: 13, Weight: 700, LineHeight: 1.3}
			labelLay := ui.ShapeRichTextWith([]ui.Span{{Text: cTitle, Size: 13, Weight: 700}}, lf, ui.TextShape{Width: avail - 2*padBoxX})
			_, lh := labelLay.Size()
			bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), bodyFont, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + lh + 6 + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockCallout,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY + lh + 6,
				layout:    lay,
				textColor: pal.Text,
				bgColor:   bg,
				borderCol: border,
				accentCol: accent,
				labelLay:  labelLay,
			})
			y += bh + 14

		case b.Kind == model.BlockQuote:
			padBoxX, padBoxY := float32(20), float32(12)
			if !legacy && pal.Theme == model.ExportThemePress {
				padBoxX = 4
			} else if pal.QuoteBG.A == 0 {
				padBoxY = 2
			}
			font := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), font, ui.TextShape{Width: avail - 2*padBoxX})
			_, th := lay.Size()
			bh := th + 2*padBoxY
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockQuote,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				textX:     bx + padBoxX,
				textY:     y + padBoxY,
				layout:    lay,
				textColor: pal.QuoteFG,
				bgColor:   pal.QuoteBG,
				accentCol: pal.QuoteBar,
			})
			y += bh + 18

		case b.Kind.IsList():
			var mLay *ui.TextLayout
			gutter := float32(28)
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker := fmt.Sprintf("%d.", ord)
				mFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, Weight: 600, LineHeight: pal.Leading}
				mLay = ui.ShapeRichTextWith([]ui.Span{{Text: marker, Size: pal.BodySize, Weight: 600}}, mFont, ui.TextShape{Width: 64})
				mw, _ := mLay.Size()
				if mw+12 > gutter {
					gutter = mw + 12
				}
			}
			bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), bodyFont, ui.TextShape{Width: avail - gutter})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx + gutter,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
				markerLay: mLay,
				markerCol: pal.Title,
				accentCol: pal.Accent,
				checked:   b.Checked,
			})
			y += th + 4

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			sz := pal.BodySize
			switch lvl {
			case 1:
				sz = pal.HeadSizes[0]
			case 2:
				sz = pal.HeadSizes[1]
			case 3:
				sz = pal.HeadSizes[2]
			case 4:
				sz = pal.BodySize * 1.12
			}
			topGap := float32(8)
			if lvl == 1 {
				topGap = 18
			} else if lvl == 2 {
				topGap = 12
			}
			y += topGap
			weight := 700
			if lvl == 6 {
				weight = 500
			}
			font := ui.Font{Family: pal.HeadingFamily, Size: sz, Weight: weight, LineHeight: 1.35}
			spans := toUISpans(b.Spans, pal, sz, false)
			for idx := range spans {
				spans[idx].Weight = weight
			}
			lay := ui.ShapeRichTextWith(spans, font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			botPad := float32(0)
			if lvl == 2 && !legacy && pal.Theme == model.ExportThemePress {
				botPad = 6
			}
			gapAfter := float32(8)
			if lvl == 1 {
				gapAfter = 10
			}
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      b.Kind,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th + botPad,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Title,
				accentCol: pal.Accent,
				borderCol: pal.Divider,
			})
			y += th + botPad + gapAfter

		default: // BlockParagraph
			if len(b.Spans) == 0 {
				y += 14
				continue
			}
			font := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
			lay := ui.ShapeRichTextWith(toUISpans(b.Spans, pal, pal.BodySize, false), font, ui.TextShape{Width: avail})
			_, th := lay.Size()
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockParagraph,
				x:         bx,
				y:         y,
				w:         avail,
				h:         th,
				textX:     bx,
				textY:     y,
				layout:    lay,
				textColor: pal.Text,
			})
			y += th + 10
		}
	}

	if resolveWatermark(prefs) {
		sc.showFooter = true
		sc.footerY = y + 28
		ff := ui.Font{Family: pal.BodyFamily, Size: 14, LineHeight: 1.4}
		sc.footerLeft = ui.ShapeRichTextWith([]ui.Span{{Text: watermarkLabel, Size: 14}}, ff, ui.TextShape{Width: contentW / 2})
		dateStr := time.Now().UTC().Format("2006.01.02")
		sc.footerRight = ui.ShapeRichTextWith([]ui.Span{{Text: dateStr, Size: 14}}, ff, ui.TextShape{Width: contentW / 2})
		y = sc.footerY + 16 + 14*1.4
	} else {
		y += 12
	}

	sc.height = y + padBottom
	if sc.height < 160 {
		sc.height = 160
	}
	return sc
}

func buildPaintedImage(b *model.Block, notePath string, bx, by, avail float32, pal PaperThemeSpec) (paintedBlock, bool) {
	if localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
		if raw, err := os.ReadFile(localPath); err == nil {
			if bm, err := ui.DecodeBitmap(raw); err == nil {
				bw, bh := bm.Size()
				if bw > 0 && bh > 0 {
					c := b.ImageCrop
					sx := c.Left * float32(bw)
					sy := c.Top * float32(bh)
					sw := (1 - c.Left - c.Right) * float32(bw)
					sh := (1 - c.Top - c.Bottom) * float32(bh)
					if sw <= 1 {
						sw = float32(bw)
						sx = 0
					}
					if sh <= 1 {
						sh = float32(bh)
						sy = 0
					}
					targetW := avail
					if b.ImageWidth > 0 && b.ImageWidth <= 100 {
						targetW = avail * b.ImageWidth / 100.0
					} else if sw < targetW {
						targetW = sw
					}
					targetH := targetW * (sh / sw)
					dx := bx
					switch b.ImageAlign {
					case model.ImageAlignCenter:
						dx = bx + (avail-targetW)/2
					case model.ImageAlignRight:
						dx = bx + avail - targetW
					}
					return paintedBlock{
						kind: model.BlockImage,
						x:    bx,
						y:    by,
						w:    avail,
						h:    targetH,
						img: &paintedImage{
							bitmap: bm,
							src:    ui.Rect{X: sx, Y: sy, W: sw, H: sh},
							dst:    ui.Rect{X: dx, Y: by, W: targetW, H: targetH},
						},
					}, true
				}
			}
		}
	}
	label := b.ImageAlt
	if label == "" {
		label = b.ImageSrc
	}
	if label == "" {
		label = "Image"
	}
	font := ui.Font{Size: 14, LineHeight: 1.5}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: "[Image] " + label, Size: 14}}, font, ui.TextShape{Width: avail - 24})
	_, th := lay.Size()
	bh := th + 20
	return paintedBlock{
		kind:      model.BlockImage,
		x:         bx,
		y:         by,
		w:         avail,
		h:         bh,
		textX:     bx + 12,
		textY:     by + 10,
		layout:    lay,
		textColor: pal.Secondary,
		bgColor:   pal.CodeBG,
		borderCol: pal.CodeBorder,
	}, true
}

func buildPaintedTable(t *model.TableData, bx, by, avail float32, pal PaperThemeSpec) paintedBlock {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
	}
	if len(t.Aligns) > cols {
		cols = len(t.Aligns)
	}
	if cols == 0 {
		return paintedBlock{kind: model.BlockTable, x: bx, y: by, w: avail, h: 20}
	}
	colWidths := make([]float32, cols)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = w / sum * avail
			}
		}
	}
	if colWidths[0] == 0 {
		each := avail / float32(cols)
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	const padCellX = float32(10)
	const padCellY = float32(7)
	pt := &paintedTable{colWidths: colWidths}
	curY := by
	headerBG := pal.TableHeaderBG()
	for r, row := range t.Rows {
		isHeader := r == 0
		rowH := float32(28)
		pRow := paintedTableRow{y: curY}
		curX := bx
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			weight := 400
			fg := pal.Text
			bg := pal.InnerCanvas
			if isHeader {
				weight = 700
				fg = pal.Title
				bg = headerBG
			}
			font := ui.Font{Family: pal.BodyFamily, Size: 14, Weight: weight, LineHeight: 1.45}
			uSpans := toUISpans(cellSpans, pal, 14, false)
			if isHeader {
				for idx := range uSpans {
					uSpans[idx].Weight = 700
				}
			}
			align := ui.Start
			if c < len(t.Aligns) {
				switch t.Aligns[c] {
				case model.ColAlignCenter:
					align = ui.Center
				case model.ColAlignRight:
					align = ui.End
				}
			}
			lay := ui.ShapeRichTextWith(uSpans, font, ui.TextShape{Width: maxFloat32(cw-2*padCellX, 20), Align: align})
			_, th := lay.Size()
			if th+2*padCellY > rowH {
				rowH = th + 2*padCellY
			}
			pRow.cells = append(pRow.cells, paintedTableCell{
				x:         curX,
				w:         cw,
				layout:    lay,
				header:    isHeader,
				textColor: fg,
				bgColor:   bg,
			})
			curX += cw
		}
		pRow.h = rowH
		pt.rows = append(pt.rows, pRow)
		curY += rowH
	}

	return paintedBlock{
		kind:      model.BlockTable,
		x:         bx,
		y:         by,
		w:         avail,
		h:         curY - by,
		borderCol: pal.TableBorder,
		table:     pt,
	}
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
