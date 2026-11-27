package markdown

import (
	"context"
	"fmt"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

// ---- PNG & PDF Visual Layout (12 Differentiated Paper Themes via mygo/ui) ----

type metaScene struct {
	present    bool
	x, y, w, h float32
	leftLay    *ui.TextLayout
	rightLay   *ui.TextLayout
	mastLay    *ui.TextLayout
	leftCol    ui.Color
	rightCol   ui.Color
	mastCol    ui.Color
	moon       bool
	earBoxes   bool
	rule       MetaRule
	ruleCol    ui.Color
	ruleY      float32
}

type titleScene struct {
	present      bool
	x, y, w      float32
	textX, textY float32
	h            float32
	bottomY      float32
	fontSize     float32
	layout       *ui.TextLayout
	color        ui.Color
	mark         TitleMark
	topRule      bool
	topTick      ui.Color
	moon         bool
	ruleWidth    float32
	ruleThick    float32
	ruleCol      ui.Color
}

type noTitleBarScene struct {
	present  bool
	kind     NoTitleBarKind
	x, y, w  float32
	h        float32
	color    ui.Color
	color2   ui.Color
	centered bool
}

type footerScene struct {
	present    bool
	x, y, w, h float32
	textY      float32
	leftLay    *ui.TextLayout
	centerLay  *ui.TextLayout
	rightLay   *ui.TextLayout
	color      ui.Color
	rightColor ui.Color
	rule       FooterRule
	ruleCol    ui.Color
	swatches   []ui.Color
}

type ornamentScene struct {
	spec      Ornament
	textLay   *ui.TextLayout
	text2Lay  *ui.TextLayout
	relocated bool
}

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
	glowCol    ui.Color
	borderW    float32
	dashed     bool

	labelLay  *ui.TextLayout
	label2Lay *ui.TextLayout
	labelX    float32
	labelY    float32
	labelCol  ui.Color
	labelBG   ui.Color

	markerLay  *ui.TextLayout
	markerCol  ui.Color
	bulletKind BulletKind
	bulletSize float32
	todoKind   TodoKind
	todoBorder ui.Color
	todoCheck  ui.Color
	checked    bool

	headPrefix  HeadPrefix
	headRule    bool
	headRuleCol ui.Color

	quoteKind   QuoteKind
	quoteAngle  float32
	quoteBarW   float32
	quoteCard   bool
	quoteLevels int
	quoteLeft   float32

	codeKind CodeKind

	divKind  DividerKind
	divWidth float32

	// Shared by the callout, image and sub-heading looks.
	radius     float32
	angle      float32
	shadowCol  ui.Color
	ruleCol    ui.Color
	tapeCol    ui.Color
	barW       float32
	frame      ui.Rect
	frameKind  ImageFrame
	subMark    SubMark
	numShape   NumberShape
	numFill    ui.Color
	numStroke  ui.Color
	numD       float32
	todoFill   bool
	topRuleCol ui.Color
	topRuleW   float32
	imgRadius  float32
	imgOffset  float32
	imgCorners bool

	table *paintedTable
	img   *paintedImage
}

type paintedTable struct {
	colWidths []float32
	rows      []paintedTableRow
	noHeader  bool
	style     TableStyle
	padX      float32
	padY      float32
	ruleCol   ui.Color
	strongCol ui.Color
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
	showTitle     bool
	bodyBottom    float32
	cardRect      ui.Rect
	cardLabelLay  *ui.TextLayout
	sections      []ui.Rect
	ornaments     []ornamentScene
	meta          metaScene
	title         titleScene
	noTitleBar    noTitleBarScene
	blocks        []paintedBlock
	footer        footerScene
}

type spanStyleOpts struct {
	family      string
	size        float32
	weight      int
	italic      bool
	track       float32
	defaultFG   ui.Color
	isCodeBlock bool
	strikeAll   bool
	noGlow      bool
}

func toUISpansWith(spans []model.Span, pal PaperThemeSpec, legacy bool, o spanStyleOpts) []ui.Span {
	if len(spans) == 0 {
		return []ui.Span{{Text: " ", Font: o.family, Size: o.size}}
	}
	inl := pal.Style.Inline
	linkFG := pal.LinkFG()
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		us := ui.Span{
			Text:          sp.Text,
			Font:          o.family,
			Size:          o.size,
			Weight:        o.weight,
			Italic:        o.italic || sp.Marks&model.MarkItalic != 0,
			Underline:     sp.Marks&model.MarkUnderline != 0,
			Strikethrough: o.strikeAll || sp.Marks&model.MarkStrike != 0,
			LetterSpacing: o.track * o.size,
		}
		if !legacy && us.Strikethrough && inl.StrikeColor.A > 0 {
			us.DecorationColor = inl.StrikeColor
		}
		if !legacy && us.Underline && inl.UnderColor.A > 0 {
			us.DecorationColor = inl.UnderColor
			if inl.UnderThick > 0 {
				us.DecorationThickness = inl.UnderThick
			}
			us.UnderlineOffset = 3
		}
		if o.defaultFG.A > 0 {
			us.Color = o.defaultFG
		}
		if !legacy && pal.Style.TextGlow.A > 0 && !o.noGlow {
			us.Shadow = ui.NewTextShadow(0, 0, 6, pal.Style.TextGlow)
		}
		if sp.Marks&model.MarkBold != 0 {
			bw := 700
			if !legacy && inl.BoldWeight > 0 {
				bw = inl.BoldWeight
			}
			if bw > us.Weight {
				us.Weight = bw
			}
			if !legacy && inl.BoldColor.A > 0 {
				us.Color = inl.BoldColor
			}
			if !legacy && inl.BoldFamily != "" {
				us.Font = inl.BoldFamily
			}
		}
		if sp.Marks&model.MarkSuperscript != 0 {
			us.Size = maxFloat32(o.size*0.72, 9)
			us.BaselineShift = o.size * 0.34
		} else if sp.Marks&model.MarkSubscript != 0 {
			us.Size = maxFloat32(o.size*0.72, 9)
			us.BaselineShift = -o.size * 0.18
		}
		if sp.Marks&model.MarkCode != 0 || sp.Marks&model.MarkMath != 0 || o.isCodeBlock {
			us.Font = fontMono
			us.NoLetterSpacing = true
			if sp.Marks&model.MarkMath != 0 {
				us.Italic = true
			}
			if !o.isCodeBlock {
				us.Size = o.size * 0.9
				cbg := pal.CodeBG
				if !legacy && inl.CodeBG.A > 0 {
					cbg = inl.CodeBG
				}
				us.Background = cbg
				us.Radius = 4
				if !legacy && inl.CodeRadius != 0 {
					us.Radius = maxFloat32(inl.CodeRadius, 0.01)
				}
				us.PaddingX = 4
				us.PaddingY = 1
				if !legacy && inl.CodeFG.A > 0 {
					us.Color = inl.CodeFG
					if cbg.A > 160 && colorLuma(inl.CodeFG) < 0.35 {
						us.Shadow = ui.TextShadow{}
					}
				}
			}
		}
		if sp.Link != "" {
			us.Color = linkFG
			if !legacy {
				us.DecorationColor = orC(inl.LinkDeco, linkFG)
				if inl.LinkThick > 0 {
					us.DecorationThickness = inl.LinkThick
				}
				if inl.LinkOffset != 0 {
					us.UnderlineOffset = inl.LinkOffset
				}
				switch {
				case inl.LinkWavy:
					us.WavyUnderline = true
				case inl.LinkDotted:
					us.DottedUnderline = true
				default:
					us.Underline = true
				}
				if inl.LinkMono {
					us.Font = fontMono
					us.Size = o.size * 0.9
					us.NoLetterSpacing = true
				}
			} else {
				us.Underline = true
			}
		}
		if !legacy && isDefaultHighlightSpan(sp) && inl.Highlight != nil {
			hl := inl.Highlight
			if hl.BG.A > 0 {
				us.Background = hl.BG
				us.Radius = 3
				us.PaddingX = 3
				us.PaddingY = 1
			}
			if hl.FG.A > 0 {
				us.Color = hl.FG
			}
			if hl.Weight > us.Weight {
				us.Weight = hl.Weight
			}
			if hl.Rule.A > 0 {
				us.Underline = true
				us.DecorationColor = hl.Rule
				us.DecorationThickness = orF(hl.RuleW, 1.5)
				us.UnderlineOffset = 3
			}
			if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
				us.Color = c
			}
		} else {
			if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
				us.Color = c
			}
			if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
				us.Background = c
				us.Radius = 3
				us.PaddingX = 2
			}
		}
		out = append(out, us)
	}
	return out
}

func buildExportScene(ctx context.Context, doc *model.Doc, notePath string, prefs model.ExportPreferences) exportScene {
	theme.EnsureBuiltinCodeFonts()
	pal, legacy := resolveExportPaper(prefs)
	if !legacy {
		theme.EnsureExportFontsForFamilies(ThemeWebFontFamilies(pal.Theme))
	}
	pageW := float32(resolveExportWidth(prefs))
	showTitle := resolveShowTitle(prefs)
	showFooter := resolveWatermark(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	now := time.Now().UTC()
	vars := newTemplateVars(notePath, title, now)
	bodyBlocks := projectExportPreviews(exportBodyBlocks(doc.Blocks, title, showTitle))

	sc := exportScene{
		width:     pageW,
		pal:       pal,
		legacy:    legacy,
		showTitle: showTitle,
	}

	st := layoutCounters{imagePixels: maxExportImagePixels}

	switch {
	case !legacy && pal.Style.Sections != nil:
		sec := pal.Style.Sections
		cardX := pal.PadLeft
		cardW := pageW - pal.PadLeft - pal.PadRight
		padLeft := cardX + sec.PadX
		contentW := cardW - sec.PadX*2
		sc.marginLeft = padLeft
		sc.contentWidth = contentW

		y := effectivePadTop(pal, legacy, showTitle)
		intro, runs := partitionSectionCards(bodyBlocks, showTitle)
		hasIntroCard := showTitle || len(intro) > 0 || (len(bodyBlocks) == 0 && len(runs) == 0)

		if hasIntroCard {
			cardTop := y
			cardPadY := sec.PadY
			if showTitle && len(intro) == 0 {
				cardPadY = minFloat32(sec.PadY, 24)
			}
			y += cardPadY
			if showTitle {
				if ms, adv := buildMetaScene(pal, vars, padLeft, y, contentW); ms.present {
					sc.meta = ms
					y += adv
				}
				ts, adv := buildTitleScene(pal, legacy, title, padLeft, y, contentW)
				sc.title = ts
				if len(intro) > 0 {
					y += adv
				} else {
					y = ts.bottomY
				}
			}
			if len(intro) > 0 {
				y = appendSceneBlocks(ctx, &sc, intro, notePath, padLeft, y, contentW, pal, legacy, &st, false)
			}
			y += cardPadY
			sc.sections = append(sc.sections, ui.Rect{X: cardX, Y: cardTop, W: cardW, H: y - cardTop})
		}

		for _, run := range runs {
			if ctx.Err() != nil {
				return sc
			}
			if len(sc.sections) > 0 {
				y += sec.Gap
			}
			st.num = listNumbering{}
			cardTop := y
			y += sec.PadY
			y = appendSceneBlocks(ctx, &sc, run, notePath, padLeft, y, contentW, pal, legacy, &st, true)
			y += sec.PadY
			sc.sections = append(sc.sections, ui.Rect{X: cardX, Y: cardTop, W: cardW, H: y - cardTop})
		}
		sc.bodyBottom = y

		if showFooter {
			gap := orF(pal.Style.Footer.Gap, 24)
			sc.footer = buildFooterScene(pal, legacy, vars, cardX, y+gap, cardW)
			y = sc.footer.y + sc.footer.h
		} else {
			y += 12
		}
		sc.height = maxFloat32(y+pal.PadBottom, 160)

	case !legacy && pal.Card != nil && pal.Card.BodyOnly:
		inset, insetTop := pal.Card.Inset, pal.Card.top()
		outerX := inset
		outerW := pageW - inset*2
		padLeft := inset + pal.PadLeft
		contentW := outerW - pal.PadLeft - pal.PadRight
		sc.marginLeft = padLeft
		sc.contentWidth = contentW

		y := insetTop
		if showTitle {
			if ms, adv := buildMetaScene(pal, vars, outerX, y, outerW); ms.present {
				sc.meta = ms
				y += adv
			}
		}
		cardTop := y
		y = cardTop + effectivePadTop(pal, legacy, showTitle)
		if showTitle {
			ts, adv := buildTitleScene(pal, legacy, title, padLeft, y, contentW)
			sc.title = ts
			y += adv
		} else if nb, adv := buildNoTitleBarScene(pal, legacy, padLeft, y, contentW); nb.present {
			sc.noTitleBar = nb
			y += adv
		}
		if len(bodyBlocks) > 0 {
			y = appendSceneBlocks(ctx, &sc, bodyBlocks, notePath, padLeft, y, contentW, pal, legacy, &st, false)
		}
		sc.bodyBottom = y
		cardBottom := y + pal.PadBottom
		sc.cardRect = ui.Rect{X: inset, Y: cardTop, W: outerW, H: maxFloat32(cardBottom-cardTop, 80)}
		y = cardTop + sc.cardRect.H

		if showFooter {
			gap := orF(pal.Style.Footer.Gap, 24)
			sc.footer = buildFooterScene(pal, legacy, vars, outerX, y+gap, outerW)
			y = sc.footer.y + sc.footer.h
		} else {
			y += 8
		}
		sc.height = maxFloat32(y+inset, 160)

	default:
		_, padRight, padBottom, padLeft := pal.Margins()
		padTop := effectivePadTop(pal, legacy, showTitle)
		if pal.Card != nil {
			padTop += pal.Card.top()
		}
		contentW := pageW - padLeft - padRight
		sc.marginLeft = padLeft
		sc.contentWidth = contentW

		y := padTop
		if showTitle {
			if !legacy {
				if ms, adv := buildMetaScene(pal, vars, padLeft, y, contentW); ms.present {
					sc.meta = ms
					y += adv
				}
			}
			ts, adv := buildTitleScene(pal, legacy, title, padLeft, y, contentW)
			sc.title = ts
			y += adv
		} else if nb, adv := buildNoTitleBarScene(pal, legacy, padLeft, y, contentW); nb.present {
			sc.noTitleBar = nb
			y += adv
		}
		if len(bodyBlocks) > 0 {
			y = appendSceneBlocks(ctx, &sc, bodyBlocks, notePath, padLeft, y, contentW, pal, legacy, &st, false)
		}
		sc.bodyBottom = y
		y += relocatedOrnamentClearance(pal, legacy, showTitle)
		if showFooter {
			gap := float32(28)
			if !legacy {
				gap = orF(pal.Style.Footer.Gap, 28)
			}
			sc.footer = buildFooterScene(pal, legacy, vars, padLeft, y+gap, contentW)
			y = sc.footer.y + sc.footer.h
		} else {
			y += 12
		}
		sc.height = maxFloat32(y+padBottom, 160)
		if pal.Card != nil {
			inset, insetTop := pal.Card.Inset, pal.Card.top()
			sc.cardRect = ui.Rect{
				X: inset,
				Y: insetTop,
				W: pageW - inset*2,
				H: maxFloat32(sc.height-insetTop-inset, 80),
			}
			if !legacy && pal.Card.Label != "" {
				lbl := vars.expand(pal.Card.Label)
				lf := ui.Font{Family: pal.BodyFamily, Size: 12, LineHeight: 1.2}
				sp := ui.Span{Text: lbl, Font: pal.BodyFamily, Size: 12}
				if pal.Style.TextGlow.A > 0 {
					sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
				}
				sc.cardLabelLay = ui.ShapeRichTextWith([]ui.Span{sp}, lf, ui.TextShape{Width: sc.cardRect.W - 36})
			}
		}
	}

	if !legacy && len(pal.Style.Ornaments) > 0 {
		sc.ornaments = buildOrnaments(pal, vars, sc.height, showTitle)
	}
	return sc
}

func relocatedOrnamentClearance(pal PaperThemeSpec, legacy, showTitle bool) float32 {
	if legacy || showTitle {
		return 0
	}
	var extra float32
	for _, o := range pal.Style.Ornaments {
		if !o.BottomWhenNoTitle {
			continue
		}
		switch o.Kind {
		case OrnamentSeal:
			extra = maxFloat32(extra, 68)
		case OrnamentStamp:
			extra = maxFloat32(extra, 32)
		}
	}
	return extra
}

func buildNoTitleBarScene(pal PaperThemeSpec, legacy bool, x, y, w float32) (noTitleBarScene, float32) {
	nb := pal.Style.NoTitleBar
	if legacy || nb.Kind == NoTitleBarNone {
		return noTitleBarScene{}, 0
	}
	out := noTitleBarScene{
		present:  true,
		kind:     nb.Kind,
		x:        x,
		y:        y,
		w:        w,
		color:    orC(nb.Color, pal.Accent),
		color2:   orC(nb.Color2, pal.Divider),
		centered: nb.Centered,
	}
	switch nb.Kind {
	case NoTitleBarGradient:
		return out, 0
	case NoTitleBarDiamond:
		out.h = 12
		return out, 34
	case NoTitleBarOverprint:
		out.h = 10
		return out, 32
	case NoTitleBarDoubleRule:
		out.h = 7
		return out, 30
	case NoTitleBarSplitBar:
		out.h = 6
		return out, 30
	case NoTitleBarRing:
		out.h = 10
		return out, 30
	case NoTitleBarTickRule:
		out.h = 4
		return out, 28
	case NoTitleBarMoonRule:
		out.h = 16
		return out, 34
	default:
		return noTitleBarScene{}, 0
	}
}

func buildOrnaments(pal PaperThemeSpec, vars templateVars, pageH float32, showTitle bool) []ornamentScene {
	out := make([]ornamentScene, 0, len(pal.Style.Ornaments))
	for _, o := range pal.Style.Ornaments {
		if !showTitle && (o.Kind == OrnamentStamp || o.Kind == OrnamentSeal) && !o.BottomWhenNoTitle {
			continue
		}
		relocated := !showTitle && o.BottomWhenNoTitle && (o.Kind == OrnamentStamp || o.Kind == OrnamentSeal)
		os := ornamentScene{spec: o, relocated: relocated}
		fam := orS(o.Family, pal.BodyFamily)
		sz := orF(o.Size, 12)
		switch o.Kind {
		case OrnamentSideText:
			txt := vars.expand(o.Text)
			f := ui.Font{Family: fam, Size: sz, Italic: o.Italic, LineHeight: 1.2}
			os.textLay = ui.ShapeRichTextWith([]ui.Span{{
				Text:          txt,
				Font:          fam,
				Size:          sz,
				Italic:        o.Italic,
				LetterSpacing: o.Track * sz,
			}}, f, ui.TextShape{Width: maxFloat32(pageH-o.Y, 400)})
		case OrnamentStamp:
			txt := vars.expand(o.Text)
			if relocated {
				sz = maxFloat32(sz-2, 16)
			}
			f := ui.Font{Family: fam, Size: sz, Italic: o.Italic, LineHeight: 1.2}
			os.textLay = ui.ShapeRichTextWith([]ui.Span{{
				Text:          txt,
				Font:          fam,
				Size:          sz,
				Italic:        o.Italic,
				LetterSpacing: o.Track * sz,
			}}, f, ui.TextShape{Width: 240})
		case OrnamentSeal:
			t1 := vars.expand(o.Text)
			t2 := vars.expand(o.Text2)
			sealW := orF(o.W, 88)
			s1 := orF(o.Size, 16)
			if relocated {
				sealW = minFloat32(sealW, 76)
				s1 = maxFloat32(s1-2, 14)
			}
			os.spec.W = sealW
			f1 := ui.Font{Family: fam, Size: s1, Weight: 600, LineHeight: 1.1}
			os.textLay = ui.ShapeRichTextWith([]ui.Span{{
				Text:          t1,
				Font:          fam,
				Size:          s1,
				Weight:        600,
				LetterSpacing: 0.08 * s1,
			}}, f1, ui.TextShape{Width: sealW, Align: ui.Center})
			if t2 != "" {
				f2 := ui.Font{Family: fontMono, Size: 9, LineHeight: 1.1}
				os.text2Lay = ui.ShapeRichTextWith([]ui.Span{{
					Text:          t2,
					Font:          fontMono,
					Size:          9,
					LetterSpacing: 0.9,
				}}, f2, ui.TextShape{Width: sealW, Align: ui.Center})
			}
		}
		out = append(out, os)
	}
	return out
}

func buildMetaScene(pal PaperThemeSpec, vars templateVars, x, y, w float32) (metaScene, float32) {
	ms := pal.Style.Meta
	left := vars.expand(ms.Left)
	right := vars.expand(ms.Right)
	mast := vars.expand(ms.Masthead)
	if left == "" && right == "" && mast == "" {
		return metaScene{}, 0
	}
	if ms.Upper {
		left = strings.ToUpper(left)
		right = strings.ToUpper(right)
	}
	fam := orS(ms.Family, pal.BodyFamily)
	sz := orF(ms.Size, 12)
	col := orC(ms.Color, pal.Secondary)
	rCol := orC(ms.RightColor, col)

	out := metaScene{
		present:  true,
		x:        x,
		y:        y,
		w:        w,
		leftCol:  col,
		rightCol: rCol,
		mastCol:  pal.Title,
		moon:     ms.Moon,
		earBoxes: mast != "",
		rule:     ms.Rule,
		ruleCol:  orC(ms.RuleColor, pal.Divider),
	}

	makeMetaSpan := func(txt string) []ui.Span {
		sp := ui.Span{
			Text:          txt,
			Font:          fam,
			Size:          sz,
			Italic:        ms.Italic,
			LetterSpacing: ms.Track * sz,
		}
		if pal.Style.TextGlow.A > 0 {
			sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		return []ui.Span{sp}
	}

	sideMaxW := w * 0.6
	if mast != "" {
		sideMaxW = w * 0.28
	}
	f := ui.Font{Family: fam, Size: sz, Italic: ms.Italic, LineHeight: 1.35}
	var h float32
	if left != "" {
		out.leftLay = ui.ShapeRichTextWith(makeMetaSpan(left), f, ui.TextShape{Width: sideMaxW})
		_, lh := out.leftLay.Size()
		if out.earBoxes {
			lh += 10
		}
		h = maxFloat32(h, lh)
	}
	if right != "" {
		out.rightLay = ui.ShapeRichTextWith(makeMetaSpan(right), f, ui.TextShape{Width: sideMaxW})
		_, rh := out.rightLay.Size()
		if out.earBoxes {
			rh += 10
		}
		h = maxFloat32(h, rh)
	}
	if mast != "" {
		mfam := orS(ms.MastheadFamily, pal.TitleFamily)
		msz := orF(ms.MastheadSize, 48)
		mf := ui.Font{Family: mfam, Size: msz, Weight: 700, LineHeight: 1.05}
		out.mastLay = ui.ShapeRichTextWith([]ui.Span{{
			Text:          mast,
			Font:          mfam,
			Size:          msz,
			Weight:        700,
			LetterSpacing: ms.MastheadTrack * msz,
		}}, mf, ui.TextShape{Width: w, Align: ui.Center})
		_, mh := out.mastLay.Size()
		h = maxFloat32(h, mh)
	}

	ruleSpace := float32(0)
	switch ms.Rule {
	case MetaRuleSingle:
		out.ruleY = y + h + 10
		ruleSpace = 11
	case MetaRuleDouble:
		out.ruleY = y + h + 12
		ruleSpace = 19
	}
	out.h = h + ruleSpace
	return out, out.h + orF(ms.Gap, 24)
}

func buildTitleScene(pal PaperThemeSpec, legacy bool, title string, x, y, w float32) (titleScene, float32) {
	ts := pal.Style.Title
	sz := pal.TitleSize
	weight := ts.Weight
	if weight == 0 {
		if pal.TitleBold {
			weight = 700
		} else {
			weight = 400
		}
	}
	fam := pal.TitleFamily
	if !isASCII(title) && fam == "" {
		fam = pal.HeadingFamily
	}
	lead := orF(ts.Leading, 1.3)
	align := ui.Start
	if (!legacy && ts.Center) || pal.Mark == TitleMarkDoubleRule {
		align = ui.Center
	}

	textX := x
	textW := w
	textY := y
	if !legacy {
		if ts.TopRule {
			textY += 18
		} else if ts.TopTick.A > 0 {
			textY += 16
		}
		if ts.Moon {
			textX += 34
			textW = maxFloat32(w-34, 80)
		}
	}

	minSz := orF(ts.MinSize, 36)
	var tLay *ui.TextLayout
	for {
		spans := buildTitleSpans(pal, legacy, title, fam, sz, weight)
		tFont := ui.Font{Family: fam, Size: sz, Weight: weight, LineHeight: lead}
		tLay = ui.ShapeRichTextWith(spans, tFont, ui.TextShape{Width: textW, Align: align})
		if legacy || ts.MaxLines <= 0 || len(tLay.Lines()) <= ts.MaxLines || sz <= minSz {
			break
		}
		sz = maxFloat32(sz-8, minSz)
	}
	_, th := tLay.Size()

	out := titleScene{
		present:   true,
		x:         x,
		y:         y,
		w:         w,
		textX:     textX,
		textY:     textY,
		h:         th,
		fontSize:  sz,
		layout:    tLay,
		color:     pal.Title,
		mark:      pal.Mark,
		topRule:   !legacy && ts.TopRule,
		topTick:   ts.TopTick,
		moon:      !legacy && ts.Moon,
		ruleWidth: ts.RuleWidth,
		ruleThick: ts.RuleThick,
		ruleCol:   ts.RuleColor,
	}

	if legacy {
		out.bottomY = textY + th + 16
		return out, th + 14 + 24
	}
	markH := float32(0)
	defGap := float32(34)
	switch pal.Mark {
	case TitleMarkRule:
		markH = 20 + orF(ts.RuleThick, 2)
	case TitleMarkBar:
		markH = 18 + 4
	case TitleMarkWavy:
		markH = 8 + 12
		defGap = 28
	case TitleMarkDoubleRule:
		markH = 18 + 7
	case TitleMarkRing:
		markH = 18 + 10
	case TitleMarkBand:
		markH = 44
		defGap = 52
	}
	out.bottomY = textY + th + markH
	gap := orF(ts.Gap, defGap)
	return out, (out.bottomY - y) + gap
}

func buildTitleSpans(pal PaperThemeSpec, legacy bool, title, fam string, sz float32, weight int) []ui.Span {
	ts := pal.Style.Title
	var baseShadow ui.TextShadow
	glow := orC(ts.Glow, pal.Style.TextGlow)
	if !legacy && glow.A > 0 {
		baseShadow = ui.NewTextShadow(0, 0, 10, glow)
	}
	makeSpan := func(txt string) ui.Span {
		return ui.Span{
			Text:          txt,
			Font:          fam,
			Size:          sz,
			Weight:        weight,
			Color:         pal.Title,
			LetterSpacing: ts.Track * sz,
			Shadow:        baseShadow,
		}
	}
	var out []ui.Span
	if !legacy && ts.Prefix != "" {
		ps := makeSpan(ts.Prefix)
		ps.Color = pal.Secondary
		out = append(out, ps)
	}
	if !legacy && ts.Accent != nil {
		head, tail := splitTitleAccent(title)
		if tail != "" {
			if head != "" {
				out = append(out, makeSpan(head))
			}
			as := makeSpan(tail)
			as.Color = orC(ts.Accent.FG, pal.Title)
			if ts.Accent.BG.A > 0 {
				as.Background = ts.Accent.BG
				as.PaddingX = maxFloat32(sz*0.07, 6)
				as.PaddingY = 2
			}
			if ts.Accent.Glow.A > 0 {
				as.Shadow = ui.NewTextShadow(0, 0, 14, ts.Accent.Glow)
			}
			out = append(out, as)
			return out
		}
	}
	out = append(out, makeSpan(title))
	return out
}

func buildFooterScene(pal PaperThemeSpec, legacy bool, vars templateVars, x, y, w float32) footerScene {
	if legacy {
		ff := ui.Font{Family: pal.BodyFamily, Size: 14, LineHeight: 1.4}
		left := ui.ShapeRichTextWith([]ui.Span{{Text: watermarkLabel, Font: pal.BodyFamily, Size: 14}}, ff, ui.TextShape{Width: w / 2})
		right := ui.ShapeRichTextWith([]ui.Span{{Text: vars.expand("{date}"), Font: pal.BodyFamily, Size: 14}}, ff, ui.TextShape{Width: w / 2})
		_, lh := left.Size()
		return footerScene{
			present:    true,
			x:          x,
			y:          y,
			w:          w,
			textY:      y + 16,
			h:          16 + lh,
			leftLay:    left,
			rightLay:   right,
			color:      pal.Secondary,
			rightColor: pal.Secondary,
			rule:       FooterLine,
			ruleCol:    pal.Divider,
		}
	}

	fs := pal.Style.Footer
	left := vars.expand(fs.Left)
	center := vars.expand(fs.Center)
	right := vars.expand(fs.Right)
	if left == "" && center == "" && right == "" {
		left = watermarkLabel
		right = vars.expand("{date}")
	}
	if fs.Upper {
		left = strings.ToUpper(left)
		center = strings.ToUpper(center)
		right = strings.ToUpper(right)
	}
	fam := orS(fs.Family, pal.BodyFamily)
	sz := orF(fs.Size, 13)
	col := orC(fs.Color, pal.Secondary)
	rCol := orC(fs.RightColor, col)
	ff := ui.Font{Family: fam, Size: sz, Italic: fs.Italic, LineHeight: 1.4}

	makeSpan := func(txt string) []ui.Span {
		sp := ui.Span{
			Text:          txt,
			Font:          fam,
			Size:          sz,
			Italic:        fs.Italic,
			LetterSpacing: fs.Track * sz,
		}
		if pal.Style.TextGlow.A > 0 {
			sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		return []ui.Span{sp}
	}

	textOff := float32(0)
	if fs.Rule == FooterLine || fs.Rule == FooterDashed {
		textOff = 14
	}
	out := footerScene{
		present:    true,
		x:          x,
		y:          y,
		w:          w,
		textY:      y + textOff,
		color:      col,
		rightColor: rCol,
		rule:       fs.Rule,
		ruleCol:    orC(fs.RuleColor, pal.Divider),
		swatches:   fs.Swatches,
	}
	var maxH float32
	if left != "" {
		out.leftLay = ui.ShapeRichTextWith(makeSpan(left), ff, ui.TextShape{Width: w * 0.6})
		_, lh := out.leftLay.Size()
		maxH = maxFloat32(maxH, lh)
	}
	if center != "" {
		align := ui.Center
		cw := w
		if fs.Rule == FooterFlank {
			align = ui.Start
			cw = w * 0.7
		}
		out.centerLay = ui.ShapeRichTextWith(makeSpan(center), ff, ui.TextShape{Width: cw, Align: align})
		_, ch := out.centerLay.Size()
		maxH = maxFloat32(maxH, ch)
	}
	if right != "" {
		out.rightLay = ui.ShapeRichTextWith(makeSpan(right), ff, ui.TextShape{Width: w * 0.6})
		_, rh := out.rightLay.Size()
		maxH = maxFloat32(maxH, rh)
	}
	if maxH == 0 {
		maxH = sz * 1.4
	}
	out.h = textOff + maxH
	return out
}

type layoutCounters struct {
	num         listNumbering
	h2Count     int
	quoteOrd    int
	calloutOrd  int
	imageOrd    int
	imagePixels int
}

// appendSceneBlocks lays out blocks starting at startY and returns the bottom Y
// of the last rendered block (without trailing inter-block gap, so section cards hug content).
func appendSceneBlocks(
	ctx context.Context,
	sc *exportScene,
	blocks []model.Block,
	notePath string,
	padLeft, startY, contentW float32,
	pal PaperThemeSpec,
	legacy bool,
	st *layoutCounters,
	suppressFirstHeadingGap bool,
) float32 {
	y := startY
	lastGap := float32(0)
	for i := range blocks {
		if ctx.Err() != nil {
			break
		}
		b := &blocks[i]
		ord := st.num.next(b)
		indent := float32(b.Indent) * 12
		if !legacy && b.Kind.IsList() {
			indent = float32(b.Indent) * orF(pal.Style.List.Gutter, 28)
		}
		quoteLevels := min(exportQuoteLevels(b), max(int((contentW-120)/16), 0))
		indent += float32(quoteLevels) * 16
		if indent > contentW-120 {
			indent = contentW - 120
		}
		if indent < 0 {
			indent = 0
		}
		bx := padLeft + indent
		avail := contentW - indent

		blockStart := len(sc.blocks)
		switch {
		case b.Kind == model.BlockDivider:
			ds := pal.Style.Divider
			divCol := orC(ds.Color, pal.Divider)
			var lay *ui.TextLayout
			bh := float32(20)
			if !legacy && ds.Kind == DividerText && ds.Text != "" {
				f := ui.Font{Family: pal.BodyFamily, Size: 14, LineHeight: 1.4}
				sp := ui.Span{Text: ds.Text, Font: pal.BodyFamily, Size: 14, LetterSpacing: orF(ds.Track, 0.25) * 14}
				if pal.Style.TextGlow.A > 0 {
					sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
				}
				lay = ui.ShapeRichTextWith([]ui.Span{sp}, f, ui.TextShape{Width: avail, Align: ui.Center})
				_, th := lay.Size()
				bh = maxFloat32(th+8, 22)
			} else if !legacy && ds.Kind == DividerRepeat && ds.Text != "" {
				unit := ds.Text
				rep := strings.Repeat(unit, max(int(avail/6)+4, 8))
				f := ui.Font{Family: fontMono, Size: 13, LineHeight: 1.3}
				sp := ui.Span{Text: rep, Font: fontMono, Size: 13}
				if pal.Style.TextGlow.A > 0 {
					sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
				}
				lay = ui.ShapeRichTextWith([]ui.Span{sp}, f, ui.TextShape{Width: avail * 3})
				bh = 22
			}
			sc.blocks = append(sc.blocks, paintedBlock{
				kind:      model.BlockDivider,
				x:         bx,
				y:         y,
				w:         avail,
				h:         bh,
				layout:    lay,
				borderCol: divCol,
				divKind:   ds.Kind,
				divWidth:  orF(ds.Width, 1),
			})
			lastGap = 12
			y += bh + lastGap

		case b.Kind == model.BlockImage:
			st.imageOrd++
			if st.imageOrd > maxExportImageRefs {
				st.imagePixels = 0
			}
			if pb, ok := buildPaintedImage(b, notePath, bx, y, avail, pal, legacy, st.imageOrd, &st.imagePixels); ok {
				lastGap = 14
				y += pb.h + lastGap
				sc.blocks = append(sc.blocks, pb)
			}

		case b.Kind == model.BlockTable && b.Table != nil:
			pb := buildPaintedTable(b.Table, bx, y, avail, pal, legacy)
			lastGap = 16
			y += pb.h + lastGap
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			pb := buildPaintedCode(b, bx, y, avail, pal, legacy)
			lastGap = 18
			y += pb.h + lastGap
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockCallout:
			st.calloutOrd++
			pb, gap := buildPaintedCallout(b, bx, y, avail, pal, legacy, st.calloutOrd)
			lastGap = gap
			y += pb.h + lastGap
			sc.blocks = append(sc.blocks, pb)

		case b.Kind == model.BlockQuote:
			st.quoteOrd++
			pb := buildPaintedQuote(b, bx, y, avail, pal, legacy, st.quoteOrd)
			lastGap = 18
			y += pb.h + lastGap
			sc.blocks = append(sc.blocks, pb)

		case b.Kind.IsList():
			pb, itemGap := buildPaintedListItem(b, ord, bx, y, avail, pal, legacy)
			lastGap = itemGap
			y += pb.h + lastGap
			sc.blocks = append(sc.blocks, pb)

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			if lvl == 2 {
				st.h2Count++
			}
			pb, topGap, afterGap := buildPaintedHeading(b, lvl, st.h2Count, bx, y, avail, pal, legacy, i == 0 && suppressFirstHeadingGap)
			y = pb.y + pb.h + afterGap
			lastGap = afterGap
			_ = topGap
			sc.blocks = append(sc.blocks, pb)

		default: // BlockParagraph
			if len(b.Spans) == 0 {
				lastGap = 0
				y += 14
				continue
			}
			lead := !legacy && (pal.Style.Lead.Weight > 0 || pal.Style.Lead.Color.A > 0 || pal.Style.Lead.Family != "") && isLeadParagraph(blocks, i)
			pFam := pal.BodyFamily
			pWeight := 0
			pCol := pal.Text
			pJustify := !legacy && pal.Style.Justify
			pGap := float32(10)
			if lead {
				if i > 0 {
					y += 10
				}
				pFam = orS(pal.Style.Lead.Family, pal.BodyFamily)
				pWeight = orI(pal.Style.Lead.Weight, 600)
				pCol = orC(pal.Style.Lead.Color, pal.Title)
				pJustify = false
				pGap = 6
			}
			font := ui.Font{Family: pFam, Size: pal.BodySize, Weight: pWeight, LineHeight: pal.Leading}
			spans := toUISpansWith(b.Spans, pal, legacy, spanStyleOpts{
				family: pFam, size: pal.BodySize, weight: pWeight, defaultFG: pCol,
			})
			shape := ui.TextShape{Width: avail, Justify: pJustify}
			lay := ui.ShapeRichTextWith(spans, font, shape)
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
				textColor: pCol,
			})
			lastGap = pGap
			y += th + lastGap
		}
		if len(sc.blocks) > blockStart {
			sc.blocks[blockStart].quoteLevels = quoteLevels
			sc.blocks[blockStart].quoteLeft = padLeft
		}
	}
	return y - lastGap
}

func buildPaintedHeading(
	b *model.Block,
	lvl, h2Idx int,
	bx, curY, avail float32,
	pal PaperThemeSpec,
	legacy bool,
	suppressTopGap bool,
) (paintedBlock, float32, float32) {
	hs := pal.Style.Heading
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
	switch lvl {
	case 1:
		topGap = 18
	case 2:
		if !legacy {
			topGap = orF(hs.TopGap, 14)
		} else {
			topGap = 12
		}
	}
	if suppressTopGap {
		topGap = 0
	}
	y := curY + topGap

	weight := 700
	if !legacy && hs.Weight > 0 {
		weight = hs.Weight
	}
	if lvl == 6 && weight > 500 {
		weight = 500
	}
	headCol := pal.Title
	if !legacy && hs.Color.A > 0 {
		headCol = hs.Color
	}
	track := float32(0)
	if !legacy {
		track = hs.Track
	}
	headFam := pal.HeadingFamily
	sub := pal.Style.Sub
	isSub := !legacy && lvl >= 3
	if isSub {
		if sub.Size > 0 {
			sz = exportSubHeadingSize(sub.Size, lvl)
		}
		headFam = orS(sub.Family, headFam)
		if sub.Weight > 0 {
			weight = sub.Weight
		}
		headCol = orC(sub.Color, headCol)
		if sub.Track != 0 {
			track = sub.Track
		}
		if sub.TopGap > 0 && !suppressTopGap {
			y = curY + sub.TopGap
		}
	}

	pb := paintedBlock{
		kind:        b.Kind,
		x:           bx,
		y:           y,
		w:           avail,
		textX:       bx,
		textY:       y,
		textColor:   headCol,
		accentCol:   orC(hs.Accent, pal.Accent),
		borderCol:   pal.Divider,
		headRuleCol: orC(hs.RuleColor, pal.Divider),
	}

	var spans []ui.Span
	if !legacy && hs.Prefix == HeadPrefixHashes {
		pCol := orC(hs.PrefixColor, pal.Secondary)
		ps := ui.Span{
			Text:          strings.Repeat("#", lvl) + " ",
			Font:          orS(hs.PrefixFont, headFam),
			Size:          sz,
			Weight:        weight,
			Color:         pCol,
			LetterSpacing: track * sz,
		}
		if pal.Style.TextGlow.A > 0 {
			ps.Shadow = ui.NewTextShadow(0, 0, 6, pal.Style.TextGlow)
		}
		spans = append(spans, ps)
	}
	if isSub && sub.Prefix != "" {
		ps := ui.Span{
			Text:   sub.Prefix,
			Font:   orS(sub.PrefixFont, headFam),
			Size:   sz,
			Weight: weight,
			Color:  orC(sub.PrefixColor, pal.Accent),
		}
		if pal.Style.TextGlow.A > 0 {
			ps.Shadow = ui.NewTextShadow(0, 0, 6, pal.Style.TextGlow)
		}
		spans = append(spans, ps)
	}
	spans = append(spans, toUISpansWith(b.Spans, pal, legacy, spanStyleOpts{
		family: headFam, size: sz, weight: weight, track: track, defaultFG: headCol,
	})...)

	textAvail := avail
	if isSub && sub.Mark != SubMarkNone {
		pb.subMark = sub.Mark
		pb.accentCol = orC(sub.MarkColor, pal.Accent)
		if sub.Mark == SubMarkSquare {
			pb.textX = bx + 22
			textAvail = maxFloat32(avail-22, 60)
		}
	}
	if lvl == 2 && !legacy {
		pb.headPrefix = hs.Prefix
		pb.headRule = hs.Underline
		switch hs.Prefix {
		case HeadPrefixDot:
			dotCol := orC(hs.Accent, pal.Accent)
			if h2Idx%2 == 0 && hs.Accent2.A > 0 {
				dotCol = hs.Accent2
			}
			pb.accentCol = dotCol
			pb.textX = bx + 22
			textAvail = maxFloat32(avail-22, 60)
		case HeadPrefixTag:
			ordStr := fmt.Sprintf("%02d", max(h2Idx, 1))
			pfam := orS(hs.PrefixFont, fontMono)
			pf := ui.Font{Family: pfam, Size: 13, Weight: 700, LineHeight: 1.2}
			pb.labelLay = ui.ShapeRichTextWith([]ui.Span{{Text: ordStr, Font: pfam, Size: 13, Weight: 700}}, pf, ui.TextShape{Width: 60})
			lw, lh := pb.labelLay.Size()
			tagW := lw + 12
			pb.labelX = bx
			pb.labelY = y + maxFloat32((sz*1.35-lh-6)*0.5, 0)
			pb.labelCol = orC(hs.TagFG, ui.RGB(255, 255, 255))
			pb.labelBG = orC(hs.Accent, pal.Accent)
			pb.textX = bx + tagW + 12
			textAvail = maxFloat32(avail-tagW-36, 60)
		case HeadPrefixLabel:
			ordStr := fmt.Sprintf("%02d —", max(h2Idx, 1))
			pfam := orS(hs.PrefixFont, fontMono)
			pf := ui.Font{Family: pfam, Size: 11, Weight: 500, LineHeight: 1.2}
			pb.labelLay = ui.ShapeRichTextWith([]ui.Span{{
				Text: ordStr, Font: pfam, Size: 11, Weight: 500, LetterSpacing: 0.24 * 11,
			}}, pf, ui.TextShape{Width: 120})
			_, lh := pb.labelLay.Size()
			pb.labelX = bx
			pb.labelY = y
			pb.labelCol = orC(hs.PrefixColor, pal.Accent)
			pb.textY = y + lh + 8
		case HeadPrefixNumber:
			ordStr := fmt.Sprintf("%02d", max(h2Idx, 1))
			pfam := orS(hs.PrefixFont, fontMono)
			pf := ui.Font{Family: pfam, Size: 12, Weight: 500, LineHeight: 1.2}
			pb.labelLay = ui.ShapeRichTextWith([]ui.Span{{
				Text: ordStr, Font: pfam, Size: 12, Weight: 500, LetterSpacing: 0.18 * 12,
			}}, pf, ui.TextShape{Width: 60})
			lw, _ := pb.labelLay.Size()
			gutter := maxFloat32(lw+14, 30)
			pb.labelX = bx
			pb.labelY = y + maxFloat32(sz*1.35*0.32, 4)
			pb.labelCol = orC(hs.PrefixColor, pal.Accent)
			pb.textX = bx + gutter
			textAvail = maxFloat32(avail-gutter, 60)
		}
	}

	font := ui.Font{Family: headFam, Size: sz, Weight: weight, LineHeight: 1.35}
	lay := ui.ShapeRichTextWith(spans, font, ui.TextShape{Width: textAvail})
	_, th := lay.Size()
	pb.layout = lay

	botPad := float32(0)
	if pb.headRule {
		botPad = 7
	}
	if pb.subMark == SubMarkDashUnder {
		botPad = 6
	}
	pb.h = (pb.textY - y) + th + botPad

	afterGap := float32(10)
	if lvl == 1 {
		afterGap = 12
	}
	return pb, topGap, afterGap
}

func buildPaintedCode(b *model.Block, bx, y, avail float32, pal PaperThemeSpec, legacy bool) paintedBlock {
	codeText := b.Code
	if codeText == "" {
		codeText = " "
	}
	cs := pal.Style.Code
	kind := CodeBox
	if !legacy {
		kind = cs.Kind
	}
	sz := float32(14.5)
	lead := float32(1.6)
	if !legacy {
		sz = orF(cs.Size, 14)
		lead = orF(cs.Leading, 1.65)
	}
	fg := pal.Text
	if !legacy && cs.FG.A > 0 {
		fg = cs.FG
	}
	lblCol := pal.Secondary
	if !legacy && cs.Label.A > 0 {
		lblCol = cs.Label
	}
	langLabel := strings.TrimSpace(b.Language)
	if strings.EqualFold(langLabel, "yaml-frontmatter") || strings.EqualFold(langLabel, "frontmatter") {
		langLabel = "yaml"
	}

	makeCodeSpan := func(txt string, col ui.Color, w int) []ui.Span {
		sp := ui.Span{Text: txt, Font: fontMono, Size: sz, Weight: w, Color: col}
		if !legacy && pal.Style.TextGlow.A > 0 {
			sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		return []ui.Span{sp}
	}
	font := ui.Font{Family: fontMono, Size: sz, LineHeight: lead}

	switch kind {
	case CodePlain:
		lay := ui.ShapeRichTextWith(makeCodeSpan(codeText, fg, 400), font, ui.TextShape{Width: avail})
		_, th := lay.Size()
		return paintedBlock{
			kind:      b.Kind,
			codeKind:  CodePlain,
			x:         bx,
			y:         y,
			w:         avail,
			h:         th + 4,
			textX:     bx,
			textY:     y + 2,
			layout:    lay,
			textColor: fg,
		}

	case CodeFenced:
		padX, padY := float32(14), float32(6)
		openFence := "```" + langLabel
		topLay := ui.ShapeRichTextWith(makeCodeSpan(openFence, lblCol, 400), font, ui.TextShape{Width: avail - padX})
		botLay := ui.ShapeRichTextWith(makeCodeSpan("```", lblCol, 400), font, ui.TextShape{Width: avail - padX})
		_, fh1 := topLay.Size()
		_, fh2 := botLay.Size()
		lay := ui.ShapeRichTextWith(makeCodeSpan(codeText, fg, 400), font, ui.TextShape{Width: maxFloat32(avail-padX, 60)})
		_, th := lay.Size()
		bh := padY*2 + fh1 + th + fh2 + 4
		return paintedBlock{
			kind:      b.Kind,
			codeKind:  CodeFenced,
			x:         bx,
			y:         y,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + padY + fh1 + 2,
			layout:    lay,
			textColor: fg,
			borderCol: pal.CodeBorder,
			labelLay:  topLay,
			label2Lay: botLay,
			labelX:    bx + padX,
			labelY:    y + padY,
			labelCol:  lblCol,
		}

	default: // CodeBox
		padX := float32(18)
		padY := float32(14)
		if !legacy {
			padX = orF(cs.PadX, 18)
			padY = orF(cs.PadY, 14)
		}
		var labelLay *ui.TextLayout
		labelH := float32(0)
		if langLabel != "" && (legacy || !cs.NoLabel) {
			lf := ui.Font{Family: fontMono, Size: 12, Weight: 600, LineHeight: 1.3}
			labelLay = ui.ShapeRichTextWith([]ui.Span{{
				Text: strings.ToUpper(langLabel), Font: fontMono, Size: 12, Weight: 600, LetterSpacing: 0.9,
			}}, lf, ui.TextShape{Width: maxFloat32(avail-2*padX, 60)})
			_, lh := labelLay.Size()
			labelH = lh + 6
		}
		lay := ui.ShapeRichTextWith(makeCodeSpan(codeText, fg, 400), font, ui.TextShape{Width: maxFloat32(avail-2*padX, 60)})
		_, th := lay.Size()
		bh := th + 2*padY + labelH
		bw := float32(1)
		if !legacy {
			if cs.BorderW < 0 {
				bw = 0
			} else {
				bw = orF(cs.BorderW, 1)
			}
		}
		return paintedBlock{
			kind:      b.Kind,
			codeKind:  CodeBox,
			x:         bx,
			y:         y,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + padY + labelH,
			layout:    lay,
			textColor: fg,
			bgColor:   pal.CodeBG,
			borderCol: pal.CodeBorder,
			borderW:   bw,
			dashed:    !legacy && cs.Dashed,
			labelLay:  labelLay,
			labelX:    bx + padX,
			labelY:    y + padY,
			labelCol:  lblCol,
		}
	}
}

func buildPaintedQuote(b *model.Block, bx, y, avail float32, pal PaperThemeSpec, legacy bool, quoteOrd int) paintedBlock {
	qs := pal.Style.Quote
	kind := QuoteBar
	if !legacy {
		kind = qs.Kind
	}
	fam := pal.BodyFamily
	sz := pal.BodySize
	lead := pal.Leading
	weight := 400
	italic := false
	track := float32(0)
	if !legacy {
		fam = orS(qs.Family, pal.BodyFamily)
		sz = orF(qs.Size, pal.BodySize)
		lead = orF(qs.Leading, pal.Leading)
		weight = orI(qs.Weight, 400)
		italic = qs.Italic
		track = qs.Track
	}
	accent := orC(qs.Accent, pal.QuoteBar)
	fg := pal.QuoteFG

	opts := spanStyleOpts{
		family: fam, size: sz, weight: weight, italic: italic, track: track, defaultFG: fg,
	}

	switch kind {
	case QuoteIndent:
		ind := qs.Indent
		padY := qs.PadY
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-ind, 80)})
		_, th := lay.Size()
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuoteIndent,
			x:         bx,
			y:         y,
			w:         avail,
			h:         th + 2*padY,
			textX:     bx + ind,
			textY:     y + padY,
			layout:    lay,
			textColor: fg,
			accentCol: accent,
		}

	case QuoteSticky:
		sw := orF(qs.Width, 330)
		if sw > avail {
			sw = avail
		}
		padX := orF(qs.PadX, 20)
		padY := orF(qs.PadY, 16)
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(sw-2*padX, 80)})
		_, th := lay.Size()
		sh := th + 2*padY
		sx := bx + avail - sw
		return paintedBlock{
			kind:       model.BlockQuote,
			quoteKind:  QuoteSticky,
			quoteAngle: qs.Angle,
			x:          sx,
			y:          y + 4,
			w:          sw,
			h:          sh + 8,
			textX:      sx + padX,
			textY:      y + 4 + padY,
			layout:     lay,
			textColor:  fg,
			bgColor:    pal.QuoteBG,
			accentCol:  accent,
		}

	case QuoteOffset:
		padX := orF(qs.PadX, 24)
		padY := orF(qs.PadY, 18)
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-2*padX-6, 80)})
		_, th := lay.Size()
		bh := th + 2*padY
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuoteOffset,
			x:         bx,
			y:         y,
			w:         avail - 6,
			h:         bh + 6,
			textX:     bx + padX,
			textY:     y + padY,
			layout:    lay,
			textColor: fg,
			bgColor:   pal.QuoteBG,
			accentCol: accent,
		}

	case QuotePull:
		padX := orF(qs.PadX, 20)
		padY := orF(qs.PadY, 22)
		var spans []ui.Span
		markSp := ui.Span{Text: "「", Font: fam, Size: sz, Weight: weight, Color: accent}
		endSp := ui.Span{Text: "」", Font: fam, Size: sz, Weight: weight, Color: accent}
		spans = append(spans, markSp)
		spans = append(spans, toUISpansWith(b.Spans, pal, legacy, opts)...)
		spans = append(spans, endSp)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-2*padX, 80), Align: ui.Center})
		_, th := lay.Size()
		bh := th + 2*padY + 6
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuotePull,
			x:         bx,
			y:         y,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + padY + 4,
			layout:    lay,
			textColor: fg,
			accentCol: pal.QuoteBar,
		}

	case QuoteBigMark:
		padX := orF(qs.PadX, 28)
		padY := orF(qs.PadY, 16)
		markSz := float32(84)
		topExtra := float32(0)
		if qs.Card {
			markSz = 56
			topExtra = 20
		}
		mf := ui.Font{Family: fontSerif, Size: markSz, LineHeight: 0.9}
		markLay := ui.ShapeRichTextWith([]ui.Span{{Text: "“", Font: fontSerif, Size: markSz, Color: accent}}, mf, ui.TextShape{Width: 120})
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-2*padX, 80)})
		_, th := lay.Size()
		bh := th + 2*padY + topExtra
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuoteBigMark,
			quoteCard: qs.Card,
			x:         bx,
			y:         y,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + padY + topExtra,
			layout:    lay,
			textColor: fg,
			bgColor:   pal.QuoteBG,
			borderCol: pal.CodeBorder,
			accentCol: accent,
			label2Lay: markLay,
		}

	case QuoteFramed:
		padX := orF(qs.PadX, 24)
		padY := orF(qs.PadY, 20)
		var labelLay *ui.TextLayout
		if qs.Label != "" {
			lbl := strings.ReplaceAll(qs.Label, "{n}", fmt.Sprintf("%02d", quoteOrd))
			lf := ui.Font{Family: fontMono, Size: 10, Weight: 500, LineHeight: 1.2}
			labelLay = ui.ShapeRichTextWith([]ui.Span{{
				Text: lbl, Font: fontMono, Size: 10, Weight: 500, LetterSpacing: 2,
			}}, lf, ui.TextShape{Width: avail - 32})
		}
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-2*padX, 80)})
		_, th := lay.Size()
		bh := th + 2*padY
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuoteFramed,
			x:         bx,
			y:         y + 6,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + 6 + padY,
			layout:    lay,
			textColor: fg,
			accentCol: accent,
			labelLay:  labelLay,
			labelCol:  accent,
			labelBG:   pal.InnerCanvas,
		}

	case QuotePrefix:
		var spans []ui.Span
		ps := ui.Span{Text: "> ", Font: fam, Size: sz, Color: accent}
		if pal.Style.TextGlow.A > 0 {
			ps.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		spans = append(spans, ps)
		bodySpans := toUISpansWith(b.Spans, pal, legacy, opts)
		for idx := range bodySpans {
			if strings.Contains(bodySpans[idx].Text, "\n") {
				bodySpans[idx].Text = strings.ReplaceAll(bodySpans[idx].Text, "\n", "\n> ")
			}
		}
		spans = append(spans, bodySpans...)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: avail})
		_, th := lay.Size()
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuotePrefix,
			x:         bx,
			y:         y,
			w:         avail,
			h:         th,
			textX:     bx,
			textY:     y,
			layout:    lay,
			textColor: fg,
		}

	default: // QuoteBar
		padX := float32(20)
		padY := float32(12)
		if pal.QuoteBG.A == 0 {
			padY = 4
		}
		if !legacy {
			padX = orF(qs.PadX, padX)
			padY = orF(qs.PadY, padY)
		}
		barW := float32(4)
		if !legacy {
			barW = orF(qs.BarW, 4)
		}
		spans := toUISpansWith(b.Spans, pal, legacy, opts)
		f := ui.Font{Family: fam, Size: sz, Weight: weight, Italic: italic, LineHeight: lead}
		lay := ui.ShapeRichTextWith(spans, f, ui.TextShape{Width: maxFloat32(avail-padX-8, 80)})
		_, th := lay.Size()
		bh := th + 2*padY
		return paintedBlock{
			kind:      model.BlockQuote,
			quoteKind: QuoteBar,
			quoteBarW: barW,
			x:         bx,
			y:         y,
			w:         avail,
			h:         bh,
			textX:     bx + padX,
			textY:     y + padY,
			layout:    lay,
			textColor: fg,
			bgColor:   pal.QuoteBG,
			accentCol: accent,
			glowCol:   qs.Glow,
		}
	}
}

func buildPaintedListItem(b *model.Block, ord int, bx, y, avail float32, pal PaperThemeSpec, legacy bool) (paintedBlock, float32) {
	ls := pal.Style.List
	gutter := float32(28)
	if !legacy {
		gutter = orF(ls.Gutter, 28)
	}
	var mLay *ui.TextLayout
	mCol := pal.Title
	numD := float32(0)
	bulletKind, bulletSize, bulletCol := ls.Bullet, ls.BulletSize, orC(ls.BulletColor, pal.Accent)
	nested := !legacy && b.Indent > 0 && (ls.Bullet2Size > 0 || ls.Bullet2Glyph != "" || ls.Bullet2Color.A > 0)
	if nested {
		bulletKind, bulletSize, bulletCol = ls.Bullet2, ls.Bullet2Size, orC(ls.Bullet2Color, pal.Secondary)
	}

	switch b.Kind {
	case model.BlockNumbered:
		var marker string
		nfam := pal.BodyFamily
		nsz := pal.BodySize
		weight := 600
		if legacy {
			if ord <= 0 {
				ord = 1
			}
			marker = fmt.Sprintf("%d.", ord)
		} else {
			marker = formatListOrdinal(ord, ls.Number)
			nfam = orS(ls.NumberFont, pal.BodyFamily)
			mCol = orC(ls.NumberColor, pal.Title)
			if ls.NumberShape != NumberShapeNone {
				nsz = orF(ls.NumberSize, 12)
			} else {
				nsz = orF(ls.NumberSize, pal.BodySize)
			}
		}
		lh := pal.Leading
		if nsz != pal.BodySize {
			lh = 1.2
		}
		mFont := ui.Font{Family: nfam, Size: nsz, Weight: weight, LineHeight: lh}
		sp := ui.Span{Text: marker, Font: nfam, Size: nsz, Weight: weight}
		if !legacy && pal.Style.TextGlow.A > 0 {
			sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
		}
		mLay = ui.ShapeRichTextWith([]ui.Span{sp}, mFont, ui.TextShape{Width: 72})
		mw, _ := mLay.Size()
		if !legacy && ls.NumberShape != NumberShapeNone {
			numD = maxFloat32(mw+10, nsz*1.75)
			mw = numD
		}
		if mw+10 > gutter {
			gutter = mw + 10
		}

	case model.BlockTodo:
		if !legacy && ls.Todo == TodoText {
			tok := "[ ]"
			mCol = orC(ls.TodoBorder, pal.Accent)
			if b.Checked {
				tok = "[x]"
				mCol = orC(ls.TodoCheck, pal.Accent)
			}
			mFont := ui.Font{Family: fontMono, Size: pal.BodySize, LineHeight: pal.Leading}
			sp := ui.Span{Text: tok, Font: fontMono, Size: pal.BodySize}
			if pal.Style.TextGlow.A > 0 {
				sp.Shadow = ui.NewTextShadow(0, 0, 5, pal.Style.TextGlow)
			}
			mLay = ui.ShapeRichTextWith([]ui.Span{sp}, mFont, ui.TextShape{Width: 60})
			mw, _ := mLay.Size()
			if mw+10 > gutter {
				gutter = mw + 10
			}
		}

	default: // BlockBullet
		if nested && ls.Bullet2Glyph != "" {
			mCol = bulletCol
			mFont := ui.Font{Family: fontMonoCJK, Size: pal.BodySize, LineHeight: pal.Leading}
			mLay = ui.ShapeRichTextWith([]ui.Span{{Text: ls.Bullet2Glyph, Font: fontMonoCJK, Size: pal.BodySize}}, mFont, ui.TextShape{Width: 40})
		} else if !legacy && (bulletKind == BulletArrow || bulletKind == BulletMiddot) {
			glyph := "→"
			gfam := fontMono
			if bulletKind == BulletMiddot {
				glyph = "·"
				gfam = pal.BodyFamily
			}
			mCol = bulletCol
			mFont := ui.Font{Family: gfam, Size: pal.BodySize, Weight: 700, LineHeight: pal.Leading}
			mLay = ui.ShapeRichTextWith([]ui.Span{{Text: glyph, Font: gfam, Size: pal.BodySize, Weight: 700}}, mFont, ui.TextShape{Width: 32})
		}
	}

	itemFG := pal.Text
	strike := false
	if !legacy && b.Kind == model.BlockTodo && b.Checked {
		if ls.DoneFG.A > 0 {
			itemFG = ls.DoneFG
		}
		strike = ls.DoneStrike
	}
	bodyFont := ui.Font{Family: pal.BodyFamily, Size: pal.BodySize, LineHeight: pal.Leading}
	spans := toUISpansWith(b.Spans, pal, legacy, spanStyleOpts{
		family: pal.BodyFamily, size: pal.BodySize, defaultFG: itemFG, strikeAll: strike,
	})
	lay := ui.ShapeRichTextWith(spans, bodyFont, ui.TextShape{Width: maxFloat32(avail-gutter, 60), Justify: !legacy && pal.Style.Justify})
	_, th := lay.Size()

	gap := float32(4)
	if !legacy {
		gap = orF(ls.Gap, 4)
	}
	return paintedBlock{
		kind:       b.Kind,
		x:          bx,
		y:          y,
		w:          avail,
		h:          th,
		textX:      bx + gutter,
		textY:      y,
		layout:     lay,
		textColor:  itemFG,
		markerLay:  mLay,
		markerCol:  mCol,
		accentCol:  bulletCol,
		bulletKind: bulletKind,
		bulletSize: bulletSize,
		todoKind:   ls.Todo,
		todoBorder: orC(ls.TodoBorder, pal.Accent),
		todoCheck:  orC(ls.TodoCheck, pal.Accent),
		checked:    b.Checked,
		radius:     ls.TodoRadius,
		todoFill:   ls.TodoFill,
		numShape:   ls.NumberShape,
		numFill:    ls.NumberFill,
		numStroke:  ls.NumberStroke,
		numD:       numD,
	}, gap
}

func buildRawImage(b *model.Block, notePath string, bx, by, avail float32, pal PaperThemeSpec, pixels *int) (paintedBlock, bool) {
	if bm := takeExportBitmap(notePath, b.ImageSrc, pixels); bm != nil {
		bw, bh := bm.Size()
		if bw > 0 && bh > 0 {
			c := exportImageCrop(b.ImageCrop, float32(bw), float32(bh))
			sx := c.Left * float32(bw)
			sy := c.Top * float32(bh)
			sw := (1 - c.Left - c.Right) * float32(bw)
			sh := (1 - c.Top - c.Bottom) * float32(bh)
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
	label := b.ImageAlt
	if label == "" {
		label = b.ImageSrc
	}
	if label == "" {
		label = "Image"
	}
	font := ui.Font{Family: pal.BodyFamily, Size: 14, LineHeight: 1.5}
	lay := ui.ShapeRichTextWith([]ui.Span{{Text: "[Image] " + label, Font: pal.BodyFamily, Size: 14}}, font, ui.TextShape{Width: avail - 24})
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

func buildPaintedTable(t *model.TableData, bx, by, avail float32, pal PaperThemeSpec, legacy bool) paintedBlock {
	cols := 0
	for _, r := range t.Rows {
		if len(r) > cols {
			cols = len(r)
		}
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

	ts := pal.Style.Table
	if legacy {
		ts = TableStyle{}
	}
	padCellX := orF(ts.PadX, 10)
	padCellY := orF(ts.PadY, 8)
	bodySz := orF(ts.Size, 14)
	bodyLead := orF(ts.Leading, 1.45)
	ruleCol := orC(ts.RuleColor, pal.TableBorder)
	strongCol := orC(ts.StrongColor, ruleCol)

	var defaultHeaderBG ui.Color
	if ts.Rules {
		defaultHeaderBG = ts.HeaderBG // zero in rules mode unless explicitly set (e.g. Riso pink)
	} else {
		defaultHeaderBG = orC(ts.HeaderBG, pal.TableHeaderBG())
	}

	pt := &paintedTable{
		colWidths: colWidths,
		noHeader:  t.NoHeaderRow,
		style:     ts,
		padX:      padCellX,
		padY:      padCellY,
		ruleCol:   ruleCol,
		strongCol: strongCol,
	}

	curY := by
	for r, row := range t.Rows {
		isHeader := r == 0 && !t.NoHeaderRow
		rowH := float32(28)
		pRow := paintedTableRow{y: curY}
		curX := bx
		for c := 0; c < cols; c++ {
			cw := colWidths[c]
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			plain := model.PlainTextOfSpans(cellSpans)
			isNum := !isHeader && ts.NumFamily != "" && isNumericCellText(plain)

			fam := pal.BodyFamily
			sz := bodySz
			weight := 400
			track := float32(0)
			fg := pal.Text
			var bg ui.Color
			if !ts.Rules {
				bg = pal.InnerCanvas
			}
			if c == 0 && ts.FirstColor.A > 0 {
				fg = ts.FirstColor
			} else if c > 0 && ts.Secondary.A > 0 {
				fg = ts.Secondary
			}
			if isNum {
				fam = ts.NumFamily
				sz = orF(ts.NumSize, sz)
				weight = orI(ts.NumWeight, weight)
				if ts.NumColor.A > 0 {
					fg = ts.NumColor
				}
			}
			isHeaderCell := isHeader || (c == 0 && t.HeaderCol)
			if isHeaderCell {
				fam = orS(ts.HeaderFont, pal.BodyFamily)
				sz = orF(ts.HeaderSize, bodySz)
				weight = orI(ts.HeaderWt, 700)
				track = ts.HeaderTrack
				fg = orC(ts.HeaderFG, pal.Title)
				bg = defaultHeaderBG
				if isHeader && ts.HeaderUpper && len(cellSpans) > 0 {
					cloned := make([]model.Span, len(cellSpans))
					for k, s := range cellSpans {
						s.Text = strings.ToUpper(s.Text)
						cloned[k] = s
					}
					cellSpans = cloned
				}
			}

			if tint, ok := exportTableTint(t, r, c, pal); ok {
				bg = tint
			}

			align := ui.Start
			if a := t.CellAlign(r, c); a != model.ColAlignNone {
				switch a {
				case model.ColAlignCenter:
					align = ui.Center
				case model.ColAlignRight:
					align = ui.End
				}
			} else if isNum && !isHeaderCell {
				align = ui.End
			}

			font := ui.Font{Family: fam, Size: sz, Weight: weight, LineHeight: bodyLead}
			uSpans := toUISpansWith(cellSpans, pal, legacy, spanStyleOpts{
				family: fam, size: sz, weight: weight, track: track, defaultFG: fg,
			})
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
		borderCol: ruleCol,
		table:     pt,
	}
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
