package markdown

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

// ---- HTML Export (12 Differentiated Paper Themes + Legacy Ocean Fallback) ----

type htmlRenderState struct {
	num        listNumbering
	h2Count    int
	quoteOrd   int
	calloutOrd int
	imageOrd   int
	imageBytes int
}

func exportHTML(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	pal, legacy := resolveExportPaper(prefs)
	if !legacy {
		theme.EnsureExportFontsForFamilies(ThemeWebFontFamilies(pal.Theme))
	}
	width := resolveExportWidth(prefs)
	showTitle := resolveShowTitle(prefs)
	watermark := resolveWatermark(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	vars := newTemplateVars(notePath, title, time.Now().UTC())
	css := buildHTMLStylesheet(pal, legacy, width, title, showTitle)

	var bodyOut strings.Builder
	bodyOut.WriteString(`<div class="page">`)

	if !legacy {
		bodyOut.WriteString(renderHTMLOrnaments(pal, legacy, vars, showTitle))
	}

	bodyBlocks := projectExportPreviews(exportBodyBlocks(doc.Blocks, title, showTitle))
	st := htmlRenderState{imageBytes: maxExportImageBytes}

	switch {
	case !legacy && pal.Style.Sections != nil:
		sec := pal.Style.Sections
		intro, runs := partitionSectionCards(bodyBlocks, showTitle)
		hasIntro := showTitle || len(intro) > 0 || (len(bodyBlocks) == 0 && len(runs) == 0)
		if hasIntro {
			if showTitle && len(intro) == 0 {
				padY := minFloat32(sec.PadY, 24)
				bodyOut.WriteString(fmt.Sprintf(`<section class="paper-section" style="padding-top:%.0fpx;padding-bottom:%.0fpx">`, padY, padY))
			} else {
				bodyOut.WriteString(`<section class="paper-section">`)
			}
			if showTitle {
				bodyOut.WriteString(renderHTMLMeta(pal, vars))
				bodyOut.WriteString(renderHTMLHeader(pal, legacy, title, len(intro) == 0))
			}
			renderHTMLBlockSlice(&bodyOut, intro, notePath, pal, legacy, &st)
			bodyOut.WriteString(`</section>`)
		}
		for _, run := range runs {
			st.num = listNumbering{}
			bodyOut.WriteString(`<section class="paper-section">`)
			renderHTMLBlockSlice(&bodyOut, run, notePath, pal, legacy, &st)
			bodyOut.WriteString(`</section>`)
		}
		if watermark {
			bodyOut.WriteString(renderHTMLFooter(pal, legacy, vars))
		}

	case !legacy && pal.Card != nil && pal.Card.BodyOnly:
		if showTitle {
			bodyOut.WriteString(renderHTMLMeta(pal, vars))
		}
		bodyOut.WriteString(`<article class="paper">`)
		bodyOut.WriteString(renderHTMLCardLabel(pal, vars))
		if showTitle {
			bodyOut.WriteString(renderHTMLHeader(pal, legacy, title, false))
		} else {
			bodyOut.WriteString(renderHTMLNoTitleBar(pal, legacy))
		}
		renderHTMLBlockSlice(&bodyOut, bodyBlocks, notePath, pal, legacy, &st)
		bodyOut.WriteString(renderHTMLRelocatedOrnaments(pal, legacy, vars, showTitle))
		bodyOut.WriteString(`</article>`)
		if watermark {
			bodyOut.WriteString(renderHTMLFooter(pal, legacy, vars))
		}

	default:
		bodyOut.WriteString(`<article class="paper">`)
		if !legacy {
			bodyOut.WriteString(renderHTMLCardLabel(pal, vars))
		}
		if showTitle {
			if !legacy {
				bodyOut.WriteString(renderHTMLMeta(pal, vars))
			}
			bodyOut.WriteString(renderHTMLHeader(pal, legacy, title, false))
		} else {
			bodyOut.WriteString(renderHTMLNoTitleBar(pal, legacy))
		}
		renderHTMLBlockSlice(&bodyOut, bodyBlocks, notePath, pal, legacy, &st)
		bodyOut.WriteString(renderHTMLRelocatedOrnaments(pal, legacy, vars, showTitle))
		if watermark {
			bodyOut.WriteString(renderHTMLFooter(pal, legacy, vars))
		} else {
			bodyOut.WriteString(`<div style="height:16px"></div>`)
		}
		bodyOut.WriteString(`</article>`)
	}

	bodyOut.WriteString("</div>")
	bodyHTML := bodyOut.String()
	embeddedFontCSS := buildHTMLEmbeddedFontsCSS(pal, legacy, bodyHTML)

	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	out.WriteString(fmt.Sprintf("<meta name=\"viewport\" content=\"width=%d\">", width))
	out.WriteString("<title>")
	out.WriteString(escapeHTMLText(title))
	out.WriteString(`</title><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; font-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><style>`)
	out.WriteString(embeddedFontCSS)
	out.WriteString(css)
	out.WriteString("</style></head><body>")
	out.WriteString(bodyHTML)
	out.WriteString("</body></html>\n")
	return out.String()
}

func renderHTMLBlockSlice(out *strings.Builder, blocks []model.Block, notePath string, pal PaperThemeSpec, legacy bool, st *htmlRenderState) {
	depth := 0
	for i := 0; i < len(blocks); i++ {
		b := &blocks[i]
		nextDepth := min(exportQuoteLevels(b), 8)
		for depth > nextDepth {
			out.WriteString("</div>\n")
			depth--
		}
		for depth < nextDepth {
			out.WriteString(fmt.Sprintf(`<div style="border-left:2px solid %s;padding-left:14px">`, uiColorCSS(pal.QuoteBar)))
			depth++
		}
		if b.Kind.IsList() {
			start := i
			kind := b.Kind
			for i+1 < len(blocks) && blocks[i+1].Kind == kind && min(exportQuoteLevels(&blocks[i+1]), 8) == depth {
				i++
			}
			renderHTMLList(out, blocks[start:i+1], &st.num, pal, legacy)
			continue
		}
		st.num.next(b)
		lead := !legacy && (pal.Style.Lead.Weight > 0 || pal.Style.Lead.Color.A > 0 || pal.Style.Lead.Family != "") && isLeadParagraph(blocks, i)
		renderHTMLBlock(out, b, notePath, pal, legacy, st, lead, i > 0)
	}
	out.WriteString(strings.Repeat("</div>\n", depth))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func htmlFontStack(fam string) string {
	switch fam {
	case fontSerif:
		return `'Noto Serif SC','Source Han Serif SC',Georgia,'Songti SC','SimSun','Noto Serif CJK SC',serif`
	case fontKai:
		return `'LXGW WenKai','LXGW WenKai Screen','Kaiti SC','STKaiti','KaiTi','FZKai-Z03',cursive,serif`
	case fontMono:
		return `'JetBrains Mono',Menlo,Monaco,'Cascadia Mono',Consolas,'DejaVu Sans Mono','HarmonyOS Sans SC','PingFang SC','Microsoft YaHei','Noto Sans CJK SC',monospace`
	case fontMonoCJK:
		return `'JetBrains Mono','HarmonyOS Sans SC',Menlo,'PingFang SC',Consolas,'Microsoft YaHei','Noto Sans CJK SC',monospace`
	case fontSans:
		return `'HarmonyOS Sans SC',-apple-system,BlinkMacSystemFont,'PingFang SC','Microsoft YaHei','Hiragino Sans GB','Noto Sans CJK SC','Noto Sans SC',sans-serif`
	default:
		if trimmed := strings.TrimSpace(fam); trimmed != "" {
			return strings.ReplaceAll(trimmed, `"`, `'`)
		}
		return `'HarmonyOS Sans SC',-apple-system,BlinkMacSystemFont,'PingFang SC','Microsoft YaHei','Hiragino Sans GB','Noto Sans CJK SC','Noto Sans SC',sans-serif`
	}
}

func htmlLineStyleCSS(ls LineStyle) string {
	switch ls {
	case LineDashed:
		return "dashed"
	case LineDotted:
		return "dotted"
	default:
		return "solid"
	}
}

func buildHTMLSubstrateCSS(pal PaperThemeSpec, legacy bool) string {
	base := fmt.Sprintf("background-color:%s;", uiColorCSS(pal.Canvas))
	if legacy || len(pal.Style.Substrate) == 0 {
		return base
	}
	var imgs, sizes []string
	for _, layer := range pal.Style.Substrate {
		switch layer.Kind {
		case SubstrateGradient:
			if layer.Color3.A > 0 {
				imgs = append(imgs, fmt.Sprintf("linear-gradient(%.0fdeg,%s 0%%,%s 55%%,%s 100%%)",
					layer.Angle, uiColorCSS(layer.Color), uiColorCSS(layer.Color2), uiColorCSS(layer.Color3)))
			} else {
				imgs = append(imgs, fmt.Sprintf("linear-gradient(%.0fdeg,%s,%s)",
					layer.Angle, uiColorCSS(layer.Color), uiColorCSS(layer.Color2)))
			}
			sizes = append(sizes, "100% 100%")
		case SubstrateGlow:
			imgs = append(imgs, fmt.Sprintf("radial-gradient(circle at %.0f%% %.0f%%,%s 0,transparent %.0f%%)",
				layer.At.X*100, layer.At.Y*100, uiColorCSS(layer.Color), layer.Radius*100))
			sizes = append(sizes, "100% 100%")
		case SubstrateGrid:
			gap := orF(layer.Gap, 16)
			sz := orF(layer.Size, 1)
			c := uiColorCSS(layer.Color)
			imgs = append(imgs,
				fmt.Sprintf("linear-gradient(%s %.1fpx,transparent %.1fpx)", c, sz, sz),
				fmt.Sprintf("linear-gradient(90deg,%s %.1fpx,transparent %.1fpx)", c, sz, sz))
			sizes = append(sizes, fmt.Sprintf("%.0fpx %.0fpx", gap, gap), fmt.Sprintf("%.0fpx %.0fpx", gap, gap))
		case SubstrateDots, SubstrateGrain:
			gap := maxFloat32(layer.Gap, 4)
			r := orF(layer.Size, 0.8)
			imgs = append(imgs, fmt.Sprintf("radial-gradient(%s %.1fpx,transparent %.1fpx)", uiColorCSS(layer.Color), r, r+0.3))
			sizes = append(sizes, fmt.Sprintf("%.0fpx %.0fpx", gap, gap))
		case SubstrateScanlines:
			gap := maxFloat32(layer.Gap, 3)
			sz := orF(layer.Size, 1)
			imgs = append(imgs, fmt.Sprintf("repeating-linear-gradient(%s 0 %.1fpx,transparent %.1fpx %.1fpx)",
				uiColorCSS(layer.Color), sz, sz, gap))
			sizes = append(sizes, "100% 100%")
		case SubstrateVignette:
			imgs = append(imgs, fmt.Sprintf("radial-gradient(ellipse at center,rgba(0,0,0,0) 55%%,%s 100%%)", uiColorCSS(layer.Color)))
			sizes = append(sizes, "100% 100%")
		}
	}
	if len(imgs) == 0 {
		return base
	}
	return fmt.Sprintf("%sbackground-image:%s;background-size:%s;", base, strings.Join(imgs, ","), strings.Join(sizes, ","))
}

func buildHTMLStylesheet(pal PaperThemeSpec, legacy bool, width int, title string, showTitle bool) string {
	substrateCSS := buildHTMLSubstrateCSS(pal, legacy)
	padTop := effectivePadTop(pal, legacy, showTitle)
	pagePad := "0px"
	paperCSS := fmt.Sprintf("background:%s;padding:%.0fpx %.0fpx %.0fpx %.0fpx;",
		uiColorCSS(pal.InnerCanvas), padTop, pal.PadRight, pal.PadBottom, pal.PadLeft)

	if !legacy && pal.Style.Sections != nil {
		sec := pal.Style.Sections
		pagePad = fmt.Sprintf("%.0fpx %.0fpx %.0fpx %.0fpx", padTop, pal.PadRight, pal.PadBottom, pal.PadLeft)
		paperCSS = fmt.Sprintf("background:%s;border:1px solid %s;border-radius:%.0fpx;box-shadow:0 8px 24px %s;padding:%.0fpx %.0fpx;",
			uiColorCSS(sec.Fill), uiColorCSS(sec.Border), sec.Radius, uiColorCSS(sec.Shadow), sec.PadY, sec.PadX)
	} else if pal.Card != nil {
		c := pal.Card
		pagePad = fmt.Sprintf("%.0fpx %.0fpx %.0fpx %.0fpx", c.top(), c.Inset, c.Inset, c.Inset)
		var pBuf strings.Builder
		if c.Kind == CardOutline {
			pBuf.WriteString("background:transparent;")
		} else {
			pBuf.WriteString(fmt.Sprintf("background:%s;", uiColorCSS(orC(c.Fill, pal.InnerCanvas))))
		}
		pBuf.WriteString(fmt.Sprintf("border-radius:%.0fpx;", c.Radius))
		if c.HasBorder {
			pBuf.WriteString(fmt.Sprintf("border:%.1fpx solid %s;", orF(c.BorderW, 1), uiColorCSS(c.Border)))
		}
		if c.TopEdge.A > 0 {
			pBuf.WriteString(fmt.Sprintf("border-top-color:%s;", uiColorCSS(c.TopEdge)))
		}
		if c.Shadow.A > 0 {
			pBuf.WriteString(fmt.Sprintf("box-shadow:0 %.0fpx %.0fpx %s;", orF(c.ShadowY, 8), orF(c.Blur, 16), uiColorCSS(c.Shadow)))
		}
		pBuf.WriteString(fmt.Sprintf("padding:%.0fpx %.0fpx %.0fpx %.0fpx;", padTop, pal.PadRight, pal.PadBottom, pal.PadLeft))
		paperCSS = pBuf.String()
	}

	titleFam := pal.TitleFamily
	if titleFam == "" || (legacy && !isASCII(title)) {
		titleFam = pal.HeadingFamily
	}
	titleWeight := pal.Style.Title.Weight
	if titleWeight == 0 {
		if pal.TitleBold {
			titleWeight = 700
		} else {
			titleWeight = 400
		}
	}
	titleLead := orF(pal.Style.Title.Leading, 1.3)
	titleTrack := ""
	if !legacy && pal.Style.Title.Track != 0 {
		titleTrack = fmt.Sprintf(";letter-spacing:%.3fem", pal.Style.Title.Track)
	}
	if !legacy && pal.Style.Title.Glow.A > 0 {
		titleTrack += fmt.Sprintf(";text-shadow:0 0 10px %s", uiColorCSS(pal.Style.Title.Glow))
	}

	textGlowCSS := ""
	if !legacy && pal.Style.TextGlow.A > 0 {
		textGlowCSS = fmt.Sprintf(";text-shadow:0 0 6px %s", uiColorCSS(pal.Style.TextGlow))
	}
	paraJustify := ""
	if !legacy && pal.Style.Justify {
		paraJustify = ";text-align:justify"
	}

	linkColor := uiColorCSS(pal.LinkFG())
	linkDeco := "text-decoration:underline"
	if !legacy {
		inl := pal.Style.Inline
		decoKind := "underline"
		if inl.LinkWavy {
			decoKind = "underline wavy"
		} else if inl.LinkDotted {
			decoKind = "underline dotted"
		}
		linkDeco = fmt.Sprintf("text-decoration:%s;text-decoration-color:%s", decoKind, uiColorCSS(orC(inl.LinkDeco, pal.LinkFG())))
		if inl.LinkThick > 0 {
			linkDeco += fmt.Sprintf(";text-decoration-thickness:%.1fpx", inl.LinkThick)
		}
		if inl.LinkOffset != 0 {
			linkDeco += fmt.Sprintf(";text-underline-offset:%.1fpx", inl.LinkOffset)
		}
		if inl.LinkMono {
			linkDeco += fmt.Sprintf(";font-family:%s;font-size:.9em", htmlFontStack(fontMono))
		}
	}

	headCol := uiColorCSS(orC(pal.Style.Heading.Color, pal.Title))
	headWt := 700
	if !legacy && pal.Style.Heading.Weight > 0 {
		headWt = pal.Style.Heading.Weight
	}
	headTrack := ""
	if !legacy && pal.Style.Heading.Track != 0 {
		headTrack = fmt.Sprintf(";letter-spacing:%.3fem", pal.Style.Heading.Track)
	}

	codeBG := uiColorCSS(pal.CodeBG)
	codeFG := uiColorCSS(orC(pal.Style.Code.FG, pal.Text))
	inlineCodeCol := orC(pal.Style.Inline.CodeBG, pal.CodeBG)
	inlineCodeBG := uiColorCSS(inlineCodeCol)
	inlineCodeExtra := ";border-radius:4px"
	if !legacy {
		inl := pal.Style.Inline
		r := float32(4)
		if inl.CodeRadius < 0 {
			r = 0
		} else if inl.CodeRadius > 0 {
			r = inl.CodeRadius
		}
		inlineCodeExtra = fmt.Sprintf(";border-radius:%.0fpx", r)
		if inl.CodeFG.A > 0 {
			inlineCodeExtra += ";color:" + uiColorCSS(inl.CodeFG)
			if pal.Style.TextGlow.A > 0 && inlineCodeCol.A > 160 && colorLuma(inl.CodeFG) < 0.35 {
				inlineCodeExtra += ";text-shadow:none"
			}
		}
	}
	inlineDecoCSS := ""
	if !legacy {
		inl := pal.Style.Inline
		if inl.StrikeColor.A > 0 {
			inlineDecoCSS += fmt.Sprintf("\ndel,s{text-decoration-color:%s}", uiColorCSS(inl.StrikeColor))
		}
		if inl.UnderColor.A > 0 {
			th := ""
			if inl.UnderThick > 0 {
				th = fmt.Sprintf(";text-decoration-thickness:%.1fpx", inl.UnderThick)
			}
			inlineDecoCSS += fmt.Sprintf("\nu{text-decoration-color:%s;text-underline-offset:3px%s}", uiColorCSS(inl.UnderColor), th)
		}
	}

	preBorder := fmt.Sprintf("border:1px solid %s;", uiColorCSS(pal.CodeBorder))
	if !legacy {
		cs := pal.Style.Code
		switch {
		case cs.Kind == CodePlain:
			codeBG = "transparent"
			preBorder = "border:0;padding:2px 0;"
		case cs.Kind == CodeFenced:
			codeBG = "transparent"
			preBorder = fmt.Sprintf("border:0;border-left:2px solid %s;border-radius:0;padding:6px 0 6px 14px;", uiColorCSS(pal.CodeBorder))
		case cs.BorderW < 0:
			preBorder = "border:0;"
		default:
			ls := "solid"
			if cs.Dashed {
				ls = "dashed"
			}
			preBorder = fmt.Sprintf("border:%.1fpx %s %s;", orF(cs.BorderW, 1), ls, uiColorCSS(pal.CodeBorder))
		}
	}

	tableCSS := buildHTMLTableCSS(pal, legacy)
	secGap := float32(16)
	if pal.Style.Sections != nil {
		secGap = orF(pal.Style.Sections.Gap, 16)
	}

	return fmt.Sprintf(`*{box-sizing:border-box}
html,body{margin:0;padding:0;background:%s;color:%s;font-family:%s%s}
.page{position:relative;overflow:hidden;width:%dpx;max-width:100%%;margin:0 auto;padding:%s;%sdisplay:flex;flex-direction:column;gap:%.0fpx}
.paper,.paper-section{position:relative;%sfont-size:%.1fpx;line-height:%.2f;color:%s;overflow-wrap:anywhere}
.doc-header{margin-bottom:28px}.doc-title{margin:0;font-family:%s;font-size:%.1fpx;line-height:%.2f;font-weight:%d;color:%s%s}
.doc-footer{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:28px;font-size:14px;line-height:19.6px;color:%s}
h1,h2,h3,h4,h5,h6{margin:18px 0 8px;line-height:1.35;font-family:%s;color:%s;font-weight:%d%s}
h1{font-size:%.1fpx}h2{font-size:%.1fpx}h3{font-size:%.1fpx}h4{font-size:%.1fpx}h5,h6{font-size:%.1fpx;font-weight:500}
p{margin:0 0 10px%s}
ul,ol{margin:0 0 10px;padding-left:26px}ul.custom-list{list-style:none;padding-left:0}li{margin:4px 0}
blockquote{margin:0 0 16px}
.callout{margin:0 0 14px;padding:12px 16px;border-radius:8px;border-left:4px solid}
.callout-title{font-size:14px;font-weight:700;letter-spacing:.04em;margin-bottom:6px}
pre{margin:0 0 16px;padding:14px 18px;background:%s;%sborder-radius:%.0fpx;overflow-x:auto;font-family:%s;font-size:%.1fpx;line-height:%.2f;color:%s}
code{font-family:%s;font-size:.9em;background:%s;padding:1px 5px%s}%s
code.math{font-style:italic}
pre code{background:transparent;padding:0;color:inherit}
%s
hr{border:0;border-top:1px solid %s;margin:18px 0}
a,table a,th a,td a{color:%s;%s}
img{max-width:100%%;height:auto;border-radius:%.0fpx}`,
		uiColorCSS(pal.Canvas), uiColorCSS(pal.Text), htmlFontStack(pal.BodyFamily), textGlowCSS,
		width, pagePad, substrateCSS, secGap,
		paperCSS, pal.BodySize, pal.Leading, uiColorCSS(pal.Text),
		htmlFontStack(titleFam), pal.TitleSize, titleLead, titleWeight, uiColorCSS(pal.Title), titleTrack,
		uiColorCSS(pal.Secondary),
		htmlFontStack(pal.HeadingFamily), headCol, headWt, headTrack,
		pal.HeadSizes[0], pal.HeadSizes[1], pal.HeadSizes[2], pal.BodySize*1.12, pal.BodySize,
		paraJustify,
		codeBG, preBorder, pal.BlockRadius(), htmlFontStack(fontMono), orF(pal.Style.Code.Size, 14.5), orF(pal.Style.Code.Leading, 1.6), codeFG,
		htmlFontStack(fontMono), inlineCodeBG, inlineCodeExtra, inlineDecoCSS,
		tableCSS,
		uiColorCSS(pal.Divider),
		linkColor, linkDeco,
		maxFloat32(pal.BlockRadius(), 2),
	)
}

func buildHTMLTableCSS(pal PaperThemeSpec, legacy bool) string {
	ts := pal.Style.Table
	if legacy || !ts.Rules {
		rc := uiColorCSS(orC(ts.RuleColor, pal.TableBorder))
		hbg := uiColorCSS(orC(ts.HeaderBG, pal.TableHeaderBG()))
		hfg := uiColorCSS(orC(ts.HeaderFG, pal.Title))
		px, py := orF(ts.PadX, 12), orF(ts.PadY, 8)
		return fmt.Sprintf(`table{width:100%%;border-collapse:collapse;margin:0 0 14px;font-size:%.1fpx;line-height:%.2f}
th,td{border:1px solid %s;padding:%.0fpx %.0fpx;text-align:left;vertical-align:top}
th{background:%s;color:%s;font-weight:600}`,
			orF(ts.Size, 14), orF(ts.Leading, 1.45), rc, py, px, hbg, hfg)
	}

	rc := uiColorCSS(orC(ts.RuleColor, pal.TableBorder))
	sc := uiColorCSS(orC(ts.StrongColor, orC(ts.RuleColor, pal.TableBorder)))
	px, py := orF(ts.PadX, 8), orF(ts.PadY, 8)
	var b strings.Builder
	if ts.Outer > 0 {
		b.WriteString(fmt.Sprintf("table{width:100%%;border-collapse:separate;border-spacing:0;margin:0 0 14px;border:%.1fpx solid %s;border-radius:%.0fpx;overflow:hidden;font-size:%.1fpx;line-height:%.2f}\n",
			ts.Outer, sc, ts.OuterRadius, orF(ts.Size, 14.5), orF(ts.Leading, 1.55)))
	} else {
		topRule := ""
		botRule := ""
		if ts.Top > 0 {
			topRule = fmt.Sprintf("border-top:%.1fpx solid %s;", ts.Top, sc)
		}
		if ts.Bottom > 0 {
			botRule = fmt.Sprintf("border-bottom:%.1fpx solid %s;", ts.Bottom, sc)
		}
		b.WriteString(fmt.Sprintf("table{width:100%%;border-collapse:collapse;margin:0 0 14px;%s%sfont-size:%.1fpx;line-height:%.2f}\n",
			topRule, botRule, orF(ts.Size, 14.5), orF(ts.Leading, 1.55)))
	}
	b.WriteString(fmt.Sprintf("th,td{padding:%.0fpx %.0fpx;text-align:left;vertical-align:top}\n", py, px))
	if ts.ColSep > 0 {
		b.WriteString(fmt.Sprintf("th+th,td+td{border-left:%.1fpx %s %s}\n", ts.ColSep, htmlLineStyleCSS(ts.ColSepLine), rc))
	}
	hbg := "transparent"
	if ts.HeaderBG.A > 0 {
		hbg = uiColorCSS(ts.HeaderBG)
	}
	hfg := uiColorCSS(orC(ts.HeaderFG, pal.Title))
	hwt := orI(ts.HeaderWt, 700)
	hsz := orF(ts.HeaderSize, orF(ts.Size, 14))
	hfam := htmlFontStack(orS(ts.HeaderFont, pal.BodyFamily))
	upper := ""
	if ts.HeaderUpper {
		upper = ";text-transform:uppercase"
	}
	track := ""
	if ts.HeaderTrack != 0 {
		track = fmt.Sprintf(";letter-spacing:%.2fem", ts.HeaderTrack)
	}
	headSep := ""
	if ts.HeadSep > 0 {
		headSep = fmt.Sprintf(";border-bottom:%.1fpx %s %s", ts.HeadSep, htmlLineStyleCSS(ts.HeadSepLine), sc)
	}
	b.WriteString(fmt.Sprintf("th{background:%s;color:%s;font-family:%s;font-size:%.1fpx;font-weight:%d%s%s%s}\n",
		hbg, hfg, hfam, hsz, hwt, upper, track, headSep))
	if ts.RowSep > 0 {
		b.WriteString(fmt.Sprintf("tbody tr+tr td{border-top:%.1fpx %s %s}", ts.RowSep, htmlLineStyleCSS(ts.RowSepLine), rc))
	}
	return b.String()
}

func renderHTMLOrnaments(pal PaperThemeSpec, legacy bool, vars templateVars, showTitle bool) string {
	var out strings.Builder
	for _, o := range pal.Style.Ornaments {
		if !showTitle && (o.Kind == OrnamentStamp || o.Kind == OrnamentSeal) {
			continue
		}
		switch o.Kind {
		case OrnamentEdgeRule:
			pos := fmt.Sprintf("left:%.0fpx", o.X)
			if o.X < 0 {
				pos = fmt.Sprintf("right:%.0fpx", -o.X)
			}
			tb := "top:0;bottom:0"
			if orF(o.W, 1) < 1 {
				tb = fmt.Sprintf("top:%.0fpx;bottom:%.0fpx", effectivePadTop(pal, legacy, showTitle), pal.PadBottom)
			}
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;%s;%s;width:%.1fpx;background:%s;pointer-events:none;z-index:1"></div>`,
				pos, tb, orF(o.W, 1), uiColorCSS(o.Color)))
		case OrnamentRegMarks:
			sz := orF(o.Size, 14)
			c := uiColorCSS(o.Color)
			svg := fmt.Sprintf(`<svg width="%.0f" height="%.0f" viewBox="0 0 14 14" aria-hidden="true"><path d="M7 0v14M0 7h14" stroke="%s" stroke-width="1"/></svg>`, sz, sz, c)
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;left:%.0fpx;top:%.0fpx;pointer-events:none;z-index:2">%s</div>`, o.X, o.Y, svg))
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;right:%.0fpx;top:%.0fpx;pointer-events:none;z-index:2">%s</div>`, o.X, o.Y, svg))
		case OrnamentSprockets:
			pitch := maxFloat32(o.Y, 36)
			h := orF(o.H, 16)
			pad := (pitch - h) * 0.5
			grad := fmt.Sprintf("repeating-linear-gradient(transparent 0 %.1fpx,%s %.1fpx %.1fpx,transparent %.1fpx %.1fpx)",
				pad, uiColorCSS(o.Color), pad, pad+h, pad+h, pitch)
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;left:%.0fpx;top:0;bottom:0;width:%.0fpx;background:%s;pointer-events:none;z-index:1"></div>`,
				o.X, orF(o.W, 20), grad))
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;right:%.0fpx;top:0;bottom:0;width:%.0fpx;background:%s;pointer-events:none;z-index:1"></div>`,
				o.X, orF(o.W, 20), grad))
		case OrnamentSideText:
			txt := escapeHTMLText(vars.expand(o.Text))
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;right:%.0fpx;top:%.0fpx;writing-mode:vertical-rl;font-family:%s;font-size:%.0fpx;letter-spacing:%.2fem;color:%s;pointer-events:none;z-index:2">%s</div>`,
				o.X, o.Y, htmlFontStack(orS(o.Family, fontMono)), orF(o.Size, 11), o.Track, uiColorCSS(o.Color), txt))
		case OrnamentTape:
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;top:%.0fpx;left:50%%;margin-left:%.0fpx;width:%.0fpx;height:%.0fpx;background:%s;background-image:repeating-linear-gradient(90deg,rgba(255,255,255,.3) 0 6px,transparent 6px 12px);transform:rotate(%.1fdeg);pointer-events:none;z-index:3"></div>`,
				o.Y, -orF(o.W, 120)*0.5+o.X, orF(o.W, 120), orF(o.H, 30), uiColorCSS(o.Color), o.Angle))
		case OrnamentStamp:
			txt := escapeHTMLText(vars.expand(o.Text))
			it := ""
			if o.Italic {
				it = "font-style:italic;"
			}
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;right:%.0fpx;top:%.0fpx;font-family:%s;font-size:%.0fpx;%scolor:%s;transform:rotate(%.1fdeg);pointer-events:none;z-index:3">%s</div>`,
				o.X, o.Y, htmlFontStack(orS(o.Family, pal.BodyFamily)), orF(o.Size, 20), it, uiColorCSS(o.Color), o.Angle, txt))
		case OrnamentSeal:
			inset := float32(0)
			if pal.Card != nil {
				inset = pal.Card.Inset
			}
			d := orF(o.W, 90)
			sz := orF(o.Size, 15)
			c := uiColorCSS(o.Color)
			t1 := escapeHTMLText(vars.expand(o.Text))
			t2 := escapeHTMLText(vars.expand(o.Text2))
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;right:%.0fpx;top:%.0fpx;width:%.0fpx;height:%.0fpx;border:1.5px solid %s;border-radius:50%%;display:flex;flex-direction:column;align-items:center;justify-content:center;transform:rotate(%.1fdeg);color:%s;pointer-events:none;z-index:3"><div style="position:absolute;inset:4px;border:0.5px solid %s;border-radius:50%%"></div><span style="font-family:%s;font-size:%.0fpx;font-weight:600;line-height:1.1">%s</span><span style="font-family:%s;font-size:9px;letter-spacing:.12em;margin-top:3px">%s</span></div>`,
				inset+o.X, inset+o.X+o.Y, d, d, c, o.Angle, c, c, htmlFontStack(orS(o.Family, pal.BodyFamily)), sz, t1, htmlFontStack(fontMono), t2))
		}
	}
	return out.String()
}

func renderHTMLRelocatedOrnaments(pal PaperThemeSpec, legacy bool, vars templateVars, showTitle bool) string {
	if legacy || showTitle {
		return ""
	}
	var out strings.Builder
	for _, o := range pal.Style.Ornaments {
		if !o.BottomWhenNoTitle {
			continue
		}
		switch o.Kind {
		case OrnamentStamp:
			txt := escapeHTMLText(vars.expand(o.Text))
			it := ""
			if o.Italic {
				it = "font-style:italic;"
			}
			sz := maxFloat32(orF(o.Size, 20)-2, 16)
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="display:flex;justify-content:flex-end;margin-top:8px"><span style="display:inline-block;font-family:%s;font-size:%.0fpx;%scolor:%s;opacity:.82;transform:rotate(-5deg)">%s</span></div>`,
				htmlFontStack(orS(o.Family, pal.BodyFamily)), sz, it, uiColorCSS(o.Color), txt))
		case OrnamentSeal:
			d := minFloat32(orF(o.W, 88), 76)
			sz := maxFloat32(orF(o.Size, 16)-2, 14)
			c := uiColorCSS(o.Color)
			t1 := escapeHTMLText(vars.expand(o.Text))
			t2 := escapeHTMLText(vars.expand(o.Text2))
			out.WriteString(fmt.Sprintf(`<div aria-hidden="true" style="display:flex;justify-content:flex-end;margin-top:10px"><div style="position:relative;width:%.0fpx;height:%.0fpx;border:1.5px solid %s;border-radius:50%%;display:flex;flex-direction:column;align-items:center;justify-content:center;transform:rotate(%.1fdeg);color:%s;opacity:.84"><div style="position:absolute;inset:4px;border:0.5px solid %s;border-radius:50%%"></div><span style="font-family:%s;font-size:%.0fpx;font-weight:600;line-height:1.1">%s</span><span style="font-family:%s;font-size:9px;letter-spacing:.12em;margin-top:3px">%s</span></div></div>`,
				d, d, c, o.Angle, c, c, htmlFontStack(orS(o.Family, pal.BodyFamily)), sz, t1, htmlFontStack(fontMono), t2))
		}
	}
	return out.String()
}

func renderHTMLNoTitleBar(pal PaperThemeSpec, legacy bool) string {
	nb := pal.Style.NoTitleBar
	if legacy || nb.Kind == NoTitleBarNone {
		return ""
	}
	c1 := uiColorCSS(orC(nb.Color, pal.Accent))
	c2 := uiColorCSS(orC(nb.Color2, pal.Divider))
	switch nb.Kind {
	case NoTitleBarDiamond:
		if nb.Centered {
			return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;align-items:center;justify-content:center;gap:10px;margin-bottom:24px"><span style="width:48px;height:1px;background:%s"></span><span style="width:7px;height:7px;background:%s;transform:rotate(45deg)"></span><span style="width:48px;height:1px;background:%s"></span></div>`,
				c2, c1, c2)
		}
		return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;align-items:center;gap:12px;margin-bottom:24px"><span style="flex:1;height:0.8px;background:%s"></span><span style="width:7px;height:7px;background:%s;transform:rotate(45deg)"></span><span style="flex:1;height:0.8px;background:%s"></span></div>`,
			c2, c1, c2)
	case NoTitleBarOverprint:
		return fmt.Sprintf(`<div aria-hidden="true" style="position:relative;height:10px;margin-bottom:22px"><div style="position:absolute;left:0;top:0;width:62%%;height:5px;background:%s"></div><div style="position:absolute;left:14%%;top:4px;width:54%%;height:5px;background:%s"></div></div>`,
			c1, c2)
	case NoTitleBarDoubleRule:
		return fmt.Sprintf(`<div aria-hidden="true" style="margin-bottom:22px"><div style="height:3px;background:%s"></div><div style="height:1px;margin-top:3px;background:%s"></div></div>`,
			c1, c1)
	case NoTitleBarSplitBar:
		return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;gap:8px;margin-bottom:24px"><span style="flex:4;height:6px;background:%s"></span><span style="flex:1;height:6px;background:%s"></span></div>`,
			c1, c2)
	case NoTitleBarRing:
		return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;align-items:center;gap:10px;margin-bottom:22px"><span style="width:10px;height:10px;border-radius:50%%;border:1.2px solid %s;flex-shrink:0"></span><span style="flex:1;height:1px;background:%s"></span></div>`,
			c1, uiColorCSS(orC(nb.Color, pal.Accent).Alpha(0.75)))
	case NoTitleBarGradient:
		rad := float32(0)
		if pal.Card != nil {
			rad = pal.Card.Radius
		}
		return fmt.Sprintf(`<div aria-hidden="true" style="position:absolute;left:0;right:0;top:0;height:2.5px;border-radius:%.0fpx %.0fpx 0 0;background:linear-gradient(90deg,%s 0%%,%s 100%%)"></div>`,
			rad, rad, c1, c2)
	case NoTitleBarTickRule:
		return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;align-items:center;gap:10px;margin-bottom:24px"><span style="width:28px;height:3px;background:%s;flex-shrink:0"></span><span style="flex:1;height:1px;background:%s"></span></div>`,
			c1, c2)
	case NoTitleBarMoonRule:
		moon := fmt.Sprintf(`<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="1.8" style="flex-shrink:0" aria-hidden="true"><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/></svg>`, c1)
		return fmt.Sprintf(`<div aria-hidden="true" style="display:flex;align-items:center;gap:10px;margin-bottom:22px">%s<span style="flex:1;height:1px;background:%s"></span></div>`,
			moon, c2)
	default:
		return ""
	}
}

func renderHTMLCardLabel(pal PaperThemeSpec, vars templateVars) string {
	if pal.Card == nil || pal.Card.Label == "" {
		return ""
	}
	lbl := escapeHTMLText(vars.expand(pal.Card.Label))
	col := uiColorCSS(orC(pal.Card.LabelColor, pal.Card.Border))
	return fmt.Sprintf(`<span style="position:absolute;left:18px;top:-11px;background:%s;padding:0 8px;color:%s;font-size:12px;line-height:20px">%s</span>`,
		uiColorCSS(pal.Canvas), col, lbl)
}

func renderHTMLMeta(pal PaperThemeSpec, vars templateVars) string {
	ms := pal.Style.Meta
	left := vars.expand(ms.Left)
	right := vars.expand(ms.Right)
	mast := vars.expand(ms.Masthead)
	if left == "" && right == "" && mast == "" {
		return ""
	}
	if ms.Upper {
		left = strings.ToUpper(left)
		right = strings.ToUpper(right)
	}
	fam := htmlFontStack(orS(ms.Family, pal.BodyFamily))
	sz := orF(ms.Size, 12)
	col := uiColorCSS(orC(ms.Color, pal.Secondary))
	rCol := uiColorCSS(orC(ms.RightColor, orC(ms.Color, pal.Secondary)))
	it := ""
	if ms.Italic {
		it = "font-style:italic;"
	}
	moonHTML := ""
	if ms.Moon {
		moonHTML = fmt.Sprintf(`<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="1.8" style="vertical-align:-2px;margin-right:8px" aria-hidden="true"><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/></svg>`, uiColorCSS(pal.Accent))
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf(`<div class="doc-meta" style="margin-bottom:%.0fpx;font-family:%s;font-size:%.0fpx;%sletter-spacing:%.2fem;color:%s">`,
		orF(ms.Gap, 24), fam, sz, it, ms.Track, col))
	if mast != "" {
		rc := uiColorCSS(orC(ms.RuleColor, pal.Title))
		out.WriteString(`<div style="display:flex;justify-content:space-between;align-items:flex-end;gap:12px">`)
		out.WriteString(fmt.Sprintf(`<span style="border:1px solid %s;padding:4px 8px">%s</span>`, rc, escapeHTMLText(left)))
		out.WriteString(fmt.Sprintf(`<span style="font-family:%s;font-size:%.0fpx;font-weight:700;letter-spacing:%.2fem;color:%s;line-height:1">%s</span>`,
			htmlFontStack(orS(ms.MastheadFamily, pal.TitleFamily)), orF(ms.MastheadSize, 48), ms.MastheadTrack, uiColorCSS(pal.Title), escapeHTMLText(mast)))
		out.WriteString(fmt.Sprintf(`<span style="border:1px solid %s;padding:4px 8px;color:%s">%s</span>`, rc, rCol, escapeHTMLText(right)))
		out.WriteString(`</div>`)
	} else {
		out.WriteString(`<div style="display:flex;justify-content:space-between;align-items:center;gap:12px">`)
		out.WriteString(`<span>` + moonHTML + escapeHTMLText(left) + `</span>`)
		if right != "" {
			out.WriteString(fmt.Sprintf(`<span style="color:%s">%s</span>`, rCol, escapeHTMLText(right)))
		}
		out.WriteString(`</div>`)
	}
	switch ms.Rule {
	case MetaRuleSingle:
		out.WriteString(fmt.Sprintf(`<div style="height:1px;margin-top:10px;background:%s"></div>`, uiColorCSS(orC(ms.RuleColor, pal.Divider))))
	case MetaRuleDouble:
		rc := uiColorCSS(orC(ms.RuleColor, pal.Title))
		out.WriteString(fmt.Sprintf(`<div style="height:3px;margin-top:12px;border-top:3px solid %s;border-bottom:1px solid %s"></div>`, rc, rc))
	}
	out.WriteString(`</div>`)
	return out.String()
}

func renderHTMLTitleContent(pal PaperThemeSpec, legacy bool, title string) string {
	ts := pal.Style.Title
	var b strings.Builder
	if !legacy && ts.Prefix != "" {
		b.WriteString(fmt.Sprintf(`<span style="color:%s">%s</span>`, uiColorCSS(pal.Secondary), escapeHTMLText(ts.Prefix)))
	}
	if !legacy && ts.Accent != nil {
		head, tail := splitTitleAccent(title)
		if tail != "" {
			b.WriteString(escapeHTMLText(head))
			var st strings.Builder
			st.WriteString("color:" + uiColorCSS(orC(ts.Accent.FG, pal.Title)) + ";")
			if ts.Accent.BG.A > 0 {
				st.WriteString("background:" + uiColorCSS(ts.Accent.BG) + ";padding:0 8px;")
			}
			if ts.Accent.Glow.A > 0 {
				st.WriteString(fmt.Sprintf("text-shadow:0 0 14px %s;", uiColorCSS(ts.Accent.Glow)))
			}
			b.WriteString(fmt.Sprintf(`<span style="%s">%s</span>`, st.String(), escapeHTMLText(tail)))
			return b.String()
		}
	}
	b.WriteString(escapeHTMLText(title))
	return b.String()
}

func renderHTMLHeader(pal PaperThemeSpec, legacy bool, title string, compact bool) string {
	if legacy {
		return fmt.Sprintf(`<header class="doc-header"><h1 class="doc-title">%s</h1><div style="height:2px;margin-top:14px;background:%s"></div></header>`,
			escapeHTMLText(title), uiColorCSS(pal.Accent))
	}
	ts := pal.Style.Title
	gap := orF(ts.Gap, 34)
	if compact {
		gap = 0
	}
	centerCSS := ""
	if ts.Center || pal.Mark == TitleMarkDoubleRule {
		centerCSS = ` style="text-align:center"`
	}
	topAcc := ""
	if ts.TopRule {
		topAcc = fmt.Sprintf(`<div style="height:1px;margin-bottom:14px;background:%s"></div>`, uiColorCSS(orC(ts.RuleColor, pal.Title).Alpha(0.45)))
	} else if ts.TopTick.A > 0 {
		topAcc = fmt.Sprintf(`<div style="width:32px;height:3px;margin-bottom:12px;background:%s"></div>`, uiColorCSS(ts.TopTick))
	}
	inner := renderHTMLTitleContent(pal, legacy, title)
	titleHTML := fmt.Sprintf(`<h1 class="doc-title"%s>%s</h1>`, centerCSS, inner)
	if ts.Moon {
		moonSVG := fmt.Sprintf(`<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="1.8" style="flex-shrink:0;margin-top:12px" aria-hidden="true"><path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/></svg>`, uiColorCSS(pal.Accent))
		titleHTML = fmt.Sprintf(`<div style="display:flex;align-items:flex-start;gap:12px">%s%s</div>`, moonSVG, titleHTML)
	}
	ruleCol := uiColorCSS(orC(ts.RuleColor, pal.Accent))

	switch pal.Mark {
	case TitleMarkNone:
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s</header>`, gap, topAcc, titleHTML)
	case TitleMarkBar:
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s<div style="height:4px;width:48px;border-radius:2px;margin-top:18px;background:%s"></div></header>`,
			gap, topAcc, titleHTML, ruleCol)
	case TitleMarkWavy:
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s<svg width="240" height="12" style="display:block;margin-top:8px" aria-hidden="true"><path d="M2 6 Q9 0 16 6 T30 6 T44 6 T58 6 T72 6 T86 6 T100 6 T114 6 T128 6 T142 6 T156 6 T170 6 T184 6 T198 6 T212 6 T226 6" fill="none" stroke="%s" stroke-width="2" stroke-linecap="round"/></svg></header>`,
			gap, topAcc, titleHTML, ruleCol)
	case TitleMarkDoubleRule:
		dc := uiColorCSS(orC(ts.RuleColor, pal.Divider))
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s<div style="height:7px;margin-top:18px;border-top:3px solid %s;border-bottom:1px solid %s"></div></header>`,
			gap, topAcc, titleHTML, dc, dc)
	case TitleMarkRing:
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s<div style="display:flex;align-items:center;gap:10px;margin-top:18px"><span style="width:10px;height:10px;border-radius:50%%;border:1.2px solid %s;flex-shrink:0"></span><div style="flex:1;height:1px;background:%s"></div></div></header>`,
			gap, topAcc, titleHTML, ruleCol, ruleCol)
	case TitleMarkOffset:
		return fmt.Sprintf(`<header style="position:relative;margin-bottom:%.0fpx;padding-bottom:8px">%s<div style="position:absolute;left:6px;top:10px;right:18%%;bottom:0;background:%s"></div><h1 class="doc-title" style="position:relative">%s</h1></header>`,
			gap, topAcc, ruleCol, inner)
	case TitleMarkHighlight:
		rc := orC(ts.RuleColor, pal.Accent)
		ink := uiColorCSS(ui.Color{R: rc.R, G: rc.G, B: rc.B, A: 105})
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s<h1 class="doc-title"><span style="background:linear-gradient(transparent 50%%,%s 50%%,%s 88%%,transparent 88%%);box-decoration-break:clone">%s</span></h1></header>`,
			gap, topAcc, ink, ink, inner)
	case TitleMarkCursor:
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s<h1 class="doc-title">%s<span style="display:inline-block;vertical-align:middle;margin-left:8px;width:%.1fpx;height:%.1fpx;background:%s"></span></h1></header>`,
			gap, topAcc, inner, pal.TitleSize*0.52, pal.TitleSize*0.88, uiColorCSS(pal.Accent))
	case TitleMarkBand:
		top, right, _, left := pal.Margins()
		return fmt.Sprintf(`<header style="margin:-%.0fpx -%.0fpx 52px -%.0fpx;padding:%.0fpx %.0fpx 44px %.0fpx;background:%s"><h1 class="doc-title" style="color:%s">%s</h1></header>`,
			top, right, left, top, right, left, uiColorCSS(pal.Accent), uiColorCSS(pal.Accent2), inner)
	default: // TitleMarkRule
		rc := uiColorCSS(orC(ts.RuleColor, pal.Title))
		rw := orF(ts.RuleWidth, 1) * 100
		rh := orF(ts.RuleThick, 2)
		return fmt.Sprintf(`<header style="margin-bottom:%.0fpx">%s%s<div style="width:%.0f%%;height:%.1fpx;margin-top:20px;background:%s"></div></header>`,
			gap, topAcc, titleHTML, rw, rh, rc)
	}
}

func renderHTMLFooter(pal PaperThemeSpec, legacy bool, vars templateVars) string {
	if legacy {
		return fmt.Sprintf(`<footer class="doc-footer" style="padding-top:16px;border-top:1px solid %s"><span>%s</span><span>%s</span></footer>`,
			uiColorCSS(pal.Divider), watermarkLabel, vars.expand("{date}"))
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
	fam := htmlFontStack(orS(fs.Family, pal.BodyFamily))
	sz := orF(fs.Size, 13)
	col := uiColorCSS(orC(fs.Color, pal.Secondary))
	rCol := uiColorCSS(orC(fs.RightColor, orC(fs.Color, pal.Secondary)))
	rc := uiColorCSS(orC(fs.RuleColor, pal.Divider))

	var bdr string
	switch fs.Rule {
	case FooterLine:
		bdr = fmt.Sprintf("padding-top:14px;border-top:1px solid %s;", rc)
	case FooterDashed:
		bdr = fmt.Sprintf("padding-top:14px;border-top:1.5px dashed %s;", rc)
	}
	style := fmt.Sprintf(`%smargin-top:%.0fpx;font-family:%s;font-size:%.0fpx;letter-spacing:%.2fem;color:%s`,
		bdr, orF(fs.Gap, 24), fam, sz, fs.Track, col)

	if fs.Rule == FooterFlank && center != "" {
		return fmt.Sprintf(`<footer class="doc-footer" style="%s"><span style="flex:1;border-top:1px dotted %s"></span><span>%s</span><span style="flex:1;border-top:1px dotted %s"></span></footer>`,
			style, rc, escapeHTMLText(center), rc)
	}
	if center != "" && left == "" && right == "" {
		return fmt.Sprintf(`<footer class="doc-footer" style="%s;justify-content:center"><span>%s</span></footer>`, style, escapeHTMLText(center))
	}

	var chips strings.Builder
	for _, sw := range fs.Swatches {
		chips.WriteString(fmt.Sprintf(`<span style="display:inline-block;width:12px;height:12px;background:%s;margin-left:4px;vertical-align:middle"></span>`, uiColorCSS(sw)))
	}
	return fmt.Sprintf(`<footer class="doc-footer" style="%s"><span>%s%s</span><span style="color:%s">%s</span></footer>`,
		style, escapeHTMLText(left), chips.String(), rCol, escapeHTMLText(right))
}

func renderHTMLSpans(out *strings.Builder, spans []model.Span, pal PaperThemeSpec, legacy bool) {
	linkFG := pal.LinkFG()
	inl := pal.Style.Inline
	for _, sp := range spans {
		var openTags, closeTags []string
		fgColor, hasFG := resolveSpanColor(sp.FGColor, false, pal)
		if href := media.WebLink(sp.Link); href != "" {
			linkStyle := uiColorCSS(linkFG)
			if hasFG {
				linkStyle = uiColorCSS(fgColor)
			}
			openTags = append(openTags, fmt.Sprintf(`<a href="%s" style="color:%s">`, escapeAttrValue(href), linkStyle))
			closeTags = append(closeTags, "</a>")
		}
		var css strings.Builder
		isThemedHighlight := false
		if !legacy && isDefaultHighlightSpan(sp) && inl.Highlight != nil {
			isThemedHighlight = true
			hl := inl.Highlight
			if hl.BG.A > 0 {
				css.WriteString("background-color:" + uiColorCSS(hl.BG) + ";padding:1px 3px;border-radius:3px;")
			} else {
				css.WriteString("background-color:transparent;color:inherit;")
			}
			if hl.FG.A > 0 && !hasFG {
				css.WriteString("color:" + uiColorCSS(hl.FG) + ";")
			}
			if hl.Weight > 0 {
				css.WriteString(fmt.Sprintf("font-weight:%d;", hl.Weight))
			}
			if hl.Rule.A > 0 {
				css.WriteString(fmt.Sprintf("border-bottom:%.1fpx solid %s;", orF(hl.RuleW, 1.5), uiColorCSS(hl.Rule)))
			}
			if hasFG {
				css.WriteString("color:" + uiColorCSS(fgColor) + ";")
			}
		} else {
			if hasFG {
				css.WriteString("color:" + uiColorCSS(fgColor) + ";")
			} else if !legacy && sp.Marks&model.MarkBold != 0 && inl.BoldColor.A > 0 {
				css.WriteString("color:" + uiColorCSS(inl.BoldColor) + ";")
			}
			if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
				css.WriteString("background-color:" + uiColorCSS(c) + ";padding:1px 3px;border-radius:3px;")
			}
		}
		if !legacy && sp.Marks&model.MarkBold != 0 && inl.BoldWeight > 0 {
			css.WriteString(fmt.Sprintf("font-weight:%d;", inl.BoldWeight))
		}
		if isThemedHighlight {
			openTags = append(openTags, fmt.Sprintf(`<mark style="%s">`, css.String()))
			closeTags = append(closeTags, "</mark>")
		} else if css.Len() > 0 {
			openTags = append(openTags, fmt.Sprintf(`<span style="%s">`, css.String()))
			closeTags = append(closeTags, "</span>")
		}
		if sp.Marks&model.MarkUnderline != 0 {
			openTags = append(openTags, "<u>")
			closeTags = append(closeTags, "</u>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			openTags = append(openTags, "<del>")
			closeTags = append(closeTags, "</del>")
		}
		if sp.Marks&model.MarkBold != 0 {
			openTags = append(openTags, "<strong>")
			closeTags = append(closeTags, "</strong>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			openTags = append(openTags, "<em>")
			closeTags = append(closeTags, "</em>")
		}
		if sp.Marks&model.MarkSuperscript != 0 {
			openTags = append(openTags, "<sup>")
			closeTags = append(closeTags, "</sup>")
		}
		if sp.Marks&model.MarkSubscript != 0 {
			openTags = append(openTags, "<sub>")
			closeTags = append(closeTags, "</sub>")
		}
		if sp.Marks&model.MarkCode != 0 {
			openTags = append(openTags, "<code>")
			closeTags = append(closeTags, "</code>")
		}
		if sp.Marks&model.MarkMath != 0 {
			openTags = append(openTags, `<code class="math">`)
			closeTags = append(closeTags, "</code>")
		}
		for _, t := range openTags {
			out.WriteString(t)
		}
		out.WriteString(strings.ReplaceAll(escapeHTMLText(sp.Text), "\n", "<br>"))
		for i := len(closeTags) - 1; i >= 0; i-- {
			out.WriteString(closeTags[i])
		}
	}
}

func renderHTMLList(out *strings.Builder, items []model.Block, num *listNumbering, pal PaperThemeSpec, legacy bool) {
	if len(items) == 0 {
		return
	}
	first := items[0]
	if legacy {
		openTag, closeTag := "<ul>", "</ul>"
		switch first.Kind {
		case model.BlockNumbered:
			if first.ListIndex > 1 {
				openTag = fmt.Sprintf(`<ol start="%d">`, first.ListIndex)
			} else {
				openTag = "<ol>"
			}
			closeTag = "</ol>"
		case model.BlockTodo:
			openTag = `<ul class="custom-list">`
		}
		out.WriteString(openTag + "\n")
		for i := range items {
			it := &items[i]
			num.next(it)
			styleAttr := ""
			if it.Indent > 0 {
				styleAttr = fmt.Sprintf(` style="margin-left:%dpx"`, it.Indent*10)
			}
			if it.Kind == model.BlockTodo {
				chk := ""
				if it.Checked {
					chk = " checked"
				}
				out.WriteString(fmt.Sprintf(`<li%s><input type="checkbox" disabled%s> `, styleAttr, chk))
			} else {
				out.WriteString(fmt.Sprintf(`<li%s>`, styleAttr))
			}
			renderHTMLSpans(out, it.Spans, pal, legacy)
			out.WriteString("</li>\n")
		}
		out.WriteString(closeTag + "\n")
		return
	}

	ls := pal.Style.List
	gutter := int(orF(ls.Gutter, 28))
	out.WriteString(`<ul class="custom-list">` + "\n")
	for i := range items {
		it := &items[i]
		ord := num.next(it)
		indentCSS := ""
		if it.Indent > 0 {
			indentCSS = fmt.Sprintf("margin-left:%dpx;", it.Indent*gutter)
		}
		align := "baseline"
		if it.Kind == model.BlockNumbered && ls.NumberShape != NumberShapeNone {
			align = "center"
		}
		out.WriteString(fmt.Sprintf(`<li style="%sdisplay:flex;gap:10px;align-items:%s;margin-block:%.0fpx">`, indentCSS, align, orF(ls.Gap, 4)))
		out.WriteString(renderHTMLListMarker(it, ord, pal))
		var bodyWrapOpen, bodyWrapClose string
		if it.Kind == model.BlockTodo && it.Checked {
			var st strings.Builder
			if ls.DoneFG.A > 0 {
				st.WriteString("color:" + uiColorCSS(ls.DoneFG) + ";")
			}
			if ls.DoneStrike {
				st.WriteString("text-decoration:line-through;")
				if pal.Style.Inline.StrikeColor.A > 0 {
					st.WriteString("text-decoration-color:" + uiColorCSS(pal.Style.Inline.StrikeColor) + ";")
				}
			}
			if st.Len() > 0 {
				bodyWrapOpen = fmt.Sprintf(`<span style="%s">`, st.String())
				bodyWrapClose = `</span>`
			}
		}
		out.WriteString(`<span>` + bodyWrapOpen)
		renderHTMLSpans(out, it.Spans, pal, legacy)
		out.WriteString(bodyWrapClose + `</span></li>` + "\n")
	}
	out.WriteString("</ul>\n")
}

func renderHTMLListMarker(it *model.Block, ord int, pal PaperThemeSpec) string {
	ls := pal.Style.List
	switch it.Kind {
	case model.BlockNumbered:
		nfam := htmlFontStack(orS(ls.NumberFont, pal.BodyFamily))
		ncol := uiColorCSS(orC(ls.NumberColor, pal.Title))
		text := escapeHTMLText(formatListOrdinal(ord, ls.Number))
		if ls.NumberShape != NumberShapeNone {
			nsz := orF(ls.NumberSize, 12)
			d := maxFloat32(nsz*1.75, 20)
			rad := "50%"
			if ls.NumberShape == NumberShapeSquare {
				rad = "0"
			}
			bg := "transparent"
			if ls.NumberFill.A > 0 {
				bg = uiColorCSS(ls.NumberFill)
			}
			bd := ""
			if ls.NumberStroke.A > 0 {
				bd = fmt.Sprintf("border:1.2px solid %s;", uiColorCSS(ls.NumberStroke))
			}
			return fmt.Sprintf(`<span style="display:inline-flex;width:%.0fpx;height:%.0fpx;border-radius:%s;background:%s;%sfont-family:%s;font-size:%.0fpx;font-weight:600;color:%s;align-items:center;justify-content:center;flex-shrink:0">%s</span>`,
				d, d, rad, bg, bd, nfam, nsz, ncol, text)
		}
		return fmt.Sprintf(`<span style="font-family:%s;font-weight:600;color:%s;flex-shrink:0">%s</span>`, nfam, ncol, text)
	case model.BlockTodo:
		bd := uiColorCSS(orC(ls.TodoBorder, pal.Accent))
		ck := uiColorCSS(orC(ls.TodoCheck, pal.Accent))
		switch ls.Todo {
		case TodoText:
			if it.Checked {
				return fmt.Sprintf(`<span style="font-family:%s;color:%s;flex-shrink:0">[x]</span>`, htmlFontStack(fontMono), ck)
			}
			return fmt.Sprintf(`<span style="font-family:%s;color:%s;flex-shrink:0">[ ]</span>`, htmlFontStack(fontMono), bd)
		case TodoRing:
			bg := "transparent"
			if it.Checked {
				bg = ck
			}
			return fmt.Sprintf(`<span style="display:inline-block;width:11px;height:11px;border-radius:50%%;border:1.3px solid %s;background:%s;flex-shrink:0;transform:translateY(1px)"></span>`, bd, bg)
		default:
			r := orF(ls.TodoRadius, 3)
			rad := fmt.Sprintf("%.0fpx", r)
			if r >= 7 {
				rad = "50%"
			} else if r < 0.5 {
				rad = "0"
			}
			if it.Checked && ls.TodoFill {
				mark := fmt.Sprintf(`<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="3.2" aria-hidden="true"><path d="M4 13l5 5 11-12"/></svg>`, uiColorCSS(pal.InnerCanvas))
				return fmt.Sprintf(`<span style="display:inline-flex;width:15px;height:15px;background:%s;border-radius:%s;align-items:center;justify-content:center;flex-shrink:0;transform:translateY(2px)">%s</span>`, ck, rad, mark)
			}
			mark := ""
			if it.Checked {
				mark = fmt.Sprintf(`<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="3" aria-hidden="true"><path d="M4 13l5 5 11-12"/></svg>`, ck)
			}
			return fmt.Sprintf(`<span style="display:inline-flex;width:14px;height:14px;border:1.5px solid %s;border-radius:%s;align-items:center;justify-content:center;flex-shrink:0;transform:translateY(2px)">%s</span>`, bd, rad, mark)
		}
	default:
		bkind, bsz, bcolC := ls.Bullet, ls.BulletSize, orC(ls.BulletColor, pal.Accent)
		if it.Indent > 0 && (ls.Bullet2Size > 0 || ls.Bullet2Glyph != "" || ls.Bullet2Color.A > 0) {
			bcol := uiColorCSS(orC(ls.Bullet2Color, pal.Secondary))
			if ls.Bullet2Glyph != "" {
				return fmt.Sprintf(`<span style="font-family:%s;color:%s;flex-shrink:0">%s</span>`, htmlFontStack(fontMonoCJK), bcol, escapeHTMLText(ls.Bullet2Glyph))
			}
			bkind, bsz, bcolC = ls.Bullet2, ls.Bullet2Size, orC(ls.Bullet2Color, pal.Secondary)
		}
		bcol := uiColorCSS(bcolC)
		sz := orF(bsz, 6.5)
		switch bkind {
		case BulletSquare:
			return fmt.Sprintf(`<span style="display:inline-block;width:%.0fpx;height:%.0fpx;background:%s;flex-shrink:0;transform:translateY(-1px)"></span>`, sz, sz, bcol)
		case BulletDiamond:
			return fmt.Sprintf(`<span style="display:inline-block;width:%.0fpx;height:%.0fpx;background:%s;flex-shrink:0;transform:translateY(-1px) rotate(45deg)"></span>`, sz, sz, bcol)
		case BulletRing:
			return fmt.Sprintf(`<span style="display:inline-block;width:%.0fpx;height:%.0fpx;border-radius:50%%;border:1.3px solid %s;flex-shrink:0"></span>`, sz, sz, bcol)
		case BulletDash:
			return fmt.Sprintf(`<span style="font-family:%s;color:%s;flex-shrink:0">–</span>`, htmlFontStack(fontMono), bcol)
		case BulletArrow:
			return fmt.Sprintf(`<span style="font-family:%s;color:%s;flex-shrink:0">→</span>`, htmlFontStack(fontMono), bcol)
		case BulletMiddot:
			return fmt.Sprintf(`<span style="color:%s;font-weight:700;flex-shrink:0">·</span>`, bcol)
		default:
			return fmt.Sprintf(`<span style="display:inline-block;width:%.0fpx;height:%.0fpx;border-radius:50%%;background:%s;flex-shrink:0;transform:translateY(-1px)"></span>`, sz, sz, bcol)
		}
	}
}

func renderHTMLBlock(out *strings.Builder, b *model.Block, notePath string, pal PaperThemeSpec, legacy bool, st *htmlRenderState, lead bool, hasPrev bool) {
	switch {
	case b.Kind.IsHeading():
		lvl := b.Kind.HeadingLevel()
		if lvl == 2 {
			st.h2Count++
		}
		renderHTMLHeading(out, b, lvl, st.h2Count, pal, legacy)

	case b.Kind == model.BlockDivider:
		ds := pal.Style.Divider
		if !legacy && ds.Kind == DividerText && ds.Text != "" {
			out.WriteString(fmt.Sprintf(`<div role="separator" style="text-align:center;margin:20px 0;color:%s;letter-spacing:%.2fem">%s</div>`+"\n",
				uiColorCSS(orC(ds.Color, pal.Divider)), orF(ds.Track, 0.25), escapeHTMLText(ds.Text)))
		} else if !legacy && ds.Kind == DividerRepeat && ds.Text != "" {
			out.WriteString(fmt.Sprintf(`<div role="separator" style="margin:18px 0;color:%s;font-family:%s;overflow:hidden;white-space:nowrap">%s</div>`+"\n",
				uiColorCSS(orC(ds.Color, pal.Divider)), htmlFontStack(fontMono), escapeHTMLText(strings.Repeat(ds.Text, 80))))
		} else if !legacy && ds.Kind == DividerWave {
			out.WriteString(fmt.Sprintf(`<div role="separator" style="margin:22px 0;text-align:center"><svg width="220" height="12" viewBox="0 0 220 12" aria-hidden="true"><path d="M2 6q5.5-6 11 0t11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0 11 0" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round"/></svg></div>`+"\n",
				uiColorCSS(orC(ds.Color, pal.Divider)), orF(ds.Width, 1.5)))
		} else if !legacy && ds.EndColor.A > 0 {
			out.WriteString(fmt.Sprintf(`<div role="separator" style="height:%.1fpx;margin:18px 0;background:linear-gradient(to left,%s %.0fpx,%s %.0fpx)"></div>`+"\n",
				orF(ds.Width, 1), uiColorCSS(ds.EndColor), ds.EndWidth, uiColorCSS(orC(ds.Color, pal.Divider)), ds.EndWidth))
		} else {
			w := float32(1)
			c := pal.Divider
			if !legacy {
				w = orF(ds.Width, 1)
				c = orC(ds.Color, pal.Divider)
			}
			out.WriteString(fmt.Sprintf(`<hr style="border-top:%.1fpx solid %s">`+"\n", w, uiColorCSS(c)))
		}

	case b.Kind == model.BlockQuote:
		st.quoteOrd++
		renderHTMLQuote(out, b, st.quoteOrd, pal, legacy)

	case b.Kind == model.BlockCallout:
		st.calloutOrd++
		renderHTMLCallout(out, b, st.calloutOrd, pal, legacy)

	case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
		cls := ""
		lang := strings.TrimSpace(b.Language)
		if b.Kind == model.BlockRawHTML && lang == "" {
			lang = "html"
		}
		if lang != "" && lang != "yaml-frontmatter" {
			cls = fmt.Sprintf(` class="language-%s"`, escapeAttrValue(lang))
		}
		cs := pal.Style.Code
		if !legacy && cs.Kind == CodeFenced {
			lbl := uiColorCSS(orC(cs.Label, pal.Secondary))
			out.WriteString(fmt.Sprintf("<pre><span style=\"color:%s\">```%s</span>\n<code%s>%s</code>\n<span style=\"color:%s\">```</span></pre>\n",
				lbl, escapeHTMLText(lang), cls, escapeHTMLText(b.Code), lbl))
		} else {
			lblHTML := ""
			if !legacy && cs.Kind == CodeBox && !cs.NoLabel && lang != "" && lang != "yaml-frontmatter" {
				lblHTML = fmt.Sprintf(`<div style="font-size:11px;font-weight:600;letter-spacing:.08em;color:%s;margin-bottom:6px">%s</div>`,
					uiColorCSS(orC(cs.Label, pal.Secondary)), escapeHTMLText(strings.ToUpper(lang)))
			}
			out.WriteString(fmt.Sprintf("<pre>%s<code%s>%s</code></pre>\n", lblHTML, cls, escapeHTMLText(b.Code)))
		}

	case b.Kind == model.BlockTable && b.Table != nil:
		renderHTMLTable(out, b.Table, pal, legacy)

	case b.Kind == model.BlockImage:
		st.imageOrd++
		renderHTMLFigure(out, b, notePath, st.imageOrd, pal, legacy, &st.imageBytes)

	default:
		if len(b.Spans) == 0 {
			out.WriteString("<p>&nbsp;</p>\n")
			return
		}
		var sb strings.Builder
		if b.Indent > 0 {
			sb.WriteString(fmt.Sprintf("margin-left:%dpx;", b.Indent*12))
		}
		if lead {
			if hasPrev {
				sb.WriteString("margin-top:10px;")
			}
			sb.WriteString(fmt.Sprintf("margin-bottom:6px;font-weight:%d;color:%s;text-align:left;",
				orI(pal.Style.Lead.Weight, 600), uiColorCSS(orC(pal.Style.Lead.Color, pal.Title))))
			if pal.Style.Lead.Family != "" {
				sb.WriteString("font-family:" + htmlFontStack(pal.Style.Lead.Family) + ";")
			}
		}
		style := ""
		if sb.Len() > 0 {
			style = fmt.Sprintf(` style="%s"`, sb.String())
		}
		out.WriteString("<p" + style + ">")
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</p>\n")
	}
}

func renderHTMLHeading(out *strings.Builder, b *model.Block, lvl, h2Idx int, pal PaperThemeSpec, legacy bool) {
	hs := pal.Style.Heading
	if legacy {
		out.WriteString(fmt.Sprintf("<h%d>", lvl))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString(fmt.Sprintf("</h%d>\n", lvl))
		return
	}
	uCSS := ""
	if lvl == 2 && hs.Underline {
		uCSS = fmt.Sprintf(";padding-bottom:5px;border-bottom:1px solid %s", uiColorCSS(orC(hs.RuleColor, pal.Divider)))
	}
	hashPrefix := ""
	if hs.Prefix == HeadPrefixHashes {
		hashPrefix = fmt.Sprintf(`<span style="color:%s">%s </span>`,
			uiColorCSS(orC(hs.PrefixColor, pal.Secondary)), strings.Repeat("#", lvl))
	}
	if lvl == 2 {
		switch hs.Prefix {
		case HeadPrefixDot:
			dc := orC(hs.Accent, pal.Accent)
			if h2Idx%2 == 0 && hs.Accent2.A > 0 {
				dc = hs.Accent2
			}
			out.WriteString(fmt.Sprintf(`<h2 style="display:flex;align-items:center;gap:10px;margin-top:%.0fpx%s"><span style="width:12px;height:12px;border-radius:50%%;background:%s;flex-shrink:0"></span><span>`,
				orF(hs.TopGap, 18), uCSS, uiColorCSS(dc)))
			renderHTMLSpans(out, b.Spans, pal, legacy)
			out.WriteString("</span></h2>\n")
			return
		case HeadPrefixTag:
			out.WriteString(fmt.Sprintf(`<div style="display:flex;align-items:center;gap:12px;margin:%.0fpx 0 10px"><span style="font-family:%s;font-size:13px;font-weight:700;color:%s;background:%s;padding:2px 6px">%02d</span><h2 style="margin:0">`,
				orF(hs.TopGap, 20), htmlFontStack(orS(hs.PrefixFont, fontMono)), uiColorCSS(orC(hs.TagFG, ui.RGB(255, 255, 255))), uiColorCSS(orC(hs.Accent, pal.Accent)), max(h2Idx, 1)))
			renderHTMLSpans(out, b.Spans, pal, legacy)
			out.WriteString(fmt.Sprintf(`</h2><div style="flex:1;height:4px;background:%s"></div></div>`+"\n", uiColorCSS(orC(hs.RuleColor, pal.Accent))))
			return
		case HeadPrefixLabel:
			pc := uiColorCSS(orC(hs.PrefixColor, pal.Accent))
			rc := orC(hs.RuleColor, pal.Accent)
			out.WriteString(fmt.Sprintf(`<div style="display:flex;align-items:center;gap:12px;margin:%.0fpx 0 6px;font-family:%s;font-size:11px;letter-spacing:.24em;color:%s"><span>%02d —</span><div style="flex:1;height:1px;background:linear-gradient(90deg,%s,transparent)"></div></div><h2 style="margin:0 0 10px">`,
				orF(hs.TopGap, 20), htmlFontStack(orS(hs.PrefixFont, fontMono)), pc, max(h2Idx, 1), uiColorCSS(rc.Alpha(0.6))))
			renderHTMLSpans(out, b.Spans, pal, legacy)
			out.WriteString("</h2>\n")
			return
		case HeadPrefixNumber:
			out.WriteString(fmt.Sprintf(`<div style="display:flex;align-items:baseline;gap:14px;margin:%.0fpx 0 10px"><span style="font-family:%s;font-size:12px;letter-spacing:.18em;color:%s">%02d</span><h2 style="margin:0">`,
				orF(hs.TopGap, 20), htmlFontStack(orS(hs.PrefixFont, fontMono)), uiColorCSS(orC(hs.PrefixColor, pal.Accent)), max(h2Idx, 1)))
			renderHTMLSpans(out, b.Spans, pal, legacy)
			out.WriteString("</h2></div>\n")
			return
		}
	}
	if lvl >= 3 && !legacy {
		sub := pal.Style.Sub
		var st strings.Builder
		st.WriteString(fmt.Sprintf("margin-top:%.0fpx;", orF(sub.TopGap, 14)))
		if sub.Family != "" {
			st.WriteString("font-family:" + htmlFontStack(sub.Family) + ";")
		}
		if sub.Size > 0 {
			sz := exportSubHeadingSize(sub.Size, lvl)
			st.WriteString(fmt.Sprintf("font-size:%.1fpx;", sz))
		}
		if sub.Weight > 0 {
			st.WriteString(fmt.Sprintf("font-weight:%d;", sub.Weight))
		}
		if sub.Color.A > 0 {
			st.WriteString("color:" + uiColorCSS(sub.Color) + ";")
		}
		if sub.Track != 0 {
			st.WriteString(fmt.Sprintf("letter-spacing:%.3fem;", sub.Track))
		}
		if sub.Mark == SubMarkSquare {
			st.WriteString("display:flex;align-items:center;gap:8px;")
		}
		out.WriteString(fmt.Sprintf(`<h%d style="%s">%s`, lvl, st.String(), hashPrefix))
		if sub.Mark == SubMarkSquare {
			out.WriteString(fmt.Sprintf(`<span style="display:inline-block;width:14px;height:14px;background:%s;flex-shrink:0"></span>`, uiColorCSS(orC(sub.MarkColor, pal.Accent))))
		}
		if sub.Prefix != "" {
			pf := ""
			if sub.PrefixFont != "" {
				pf = "font-family:" + htmlFontStack(sub.PrefixFont) + ";"
			}
			out.WriteString(fmt.Sprintf(`<span style="%scolor:%s">%s</span>`, pf, uiColorCSS(orC(sub.PrefixColor, pal.Accent)), escapeHTMLText(sub.Prefix)))
		}
		wrapOpen, wrapClose := "", ""
		switch sub.Mark {
		case SubMarkBand:
			wrapOpen = fmt.Sprintf(`<span style="background:linear-gradient(transparent 56%%,%s 56%%);padding:0 2px">`, uiColorCSS(orC(sub.MarkColor, pal.Accent)))
			wrapClose = `</span>`
		case SubMarkDashUnder:
			wrapOpen = fmt.Sprintf(`<span style="border-bottom:1.5px dashed %s;padding-bottom:2px">`, uiColorCSS(orC(sub.MarkColor, pal.Accent)))
			wrapClose = `</span>`
		}
		out.WriteString(wrapOpen)
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString(wrapClose)
		out.WriteString(fmt.Sprintf("</h%d>\n", lvl))
		return
	}
	out.WriteString(fmt.Sprintf(`<h%d style="margin-top:%.0fpx%s">%s`, lvl, orF(hs.TopGap, 16), uCSS, hashPrefix))
	renderHTMLSpans(out, b.Spans, pal, legacy)
	out.WriteString(fmt.Sprintf("</h%d>\n", lvl))
}

func renderHTMLQuote(out *strings.Builder, b *model.Block, ord int, pal PaperThemeSpec, legacy bool) {
	if legacy {
		out.WriteString(fmt.Sprintf(`<blockquote style="padding:12px 20px;border-left:4px solid %s;background:%s;border-radius:%.0fpx;color:%s"><p style="margin:0">`,
			uiColorCSS(pal.QuoteBar), uiColorCSS(pal.QuoteBG), pal.BlockRadius(), uiColorCSS(pal.QuoteFG)))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</p></blockquote>\n")
		return
	}
	qs := pal.Style.Quote
	fam := htmlFontStack(orS(qs.Family, pal.BodyFamily))
	sz := orF(qs.Size, pal.BodySize)
	lead := orF(qs.Leading, pal.Leading)
	wt := orI(qs.Weight, 400)
	it := ""
	if qs.Italic {
		it = "font-style:italic;"
	}
	accent := uiColorCSS(orC(qs.Accent, pal.QuoteBar))
	fg := uiColorCSS(pal.QuoteFG)
	fontCSS := fmt.Sprintf("font-family:%s;font-size:%.1fpx;line-height:%.2f;font-weight:%d;%scolor:%s;", fam, sz, lead, wt, it, fg)

	switch qs.Kind {
	case QuoteIndent:
		out.WriteString(fmt.Sprintf(`<blockquote style="%smargin-left:%.0fpx;padding:%.0fpx 0">`, fontCSS, qs.Indent, qs.PadY))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	case QuoteSticky:
		out.WriteString(fmt.Sprintf(`<blockquote style="%smargin:18px 0 18px auto;max-width:%.0fpx;background:%s;padding:%.0fpx %.0fpx;transform:rotate(%.1fdeg);box-shadow:0 6px 14px rgba(110,80,20,.16)">`,
			fontCSS, orF(qs.Width, 330), uiColorCSS(pal.QuoteBG), orF(qs.PadY, 16), orF(qs.PadX, 20), qs.Angle))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	case QuoteOffset:
		out.WriteString(fmt.Sprintf(`<div style="position:relative;margin:18px 6px 24px 0"><div style="position:absolute;left:6px;top:6px;right:-6px;bottom:-6px;border:2px solid %s"></div><blockquote style="position:relative;margin:0;background:%s;padding:%.0fpx %.0fpx;%s">`,
			accent, uiColorCSS(pal.QuoteBG), orF(qs.PadY, 18), orF(qs.PadX, 24), fontCSS))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote></div>\n")
	case QuotePull:
		barCol := uiColorCSS(pal.QuoteBar)
		out.WriteString(fmt.Sprintf(`<blockquote style="%stext-align:center;border-top:3px solid %s;border-bottom:1px solid %s;padding:%.0fpx 16px"><span style="color:%s">「</span>`,
			fontCSS, barCol, barCol, orF(qs.PadY, 20), accent))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString(fmt.Sprintf(`<span style="color:%s">」</span></blockquote>`+"\n", accent))
	case QuoteBigMark:
		cardCSS := ""
		if qs.Card {
			cardCSS = fmt.Sprintf("background:%s;border:1px solid %s;border-radius:14px;", uiColorCSS(pal.QuoteBG), uiColorCSS(pal.CodeBorder))
		}
		out.WriteString(fmt.Sprintf(`<blockquote style="position:relative;%s%spadding:%.0fpx %.0fpx"><div aria-hidden="true" style="font-family:%s;font-size:48px;line-height:.7;color:%s;margin-bottom:6px">“</div>`,
			cardCSS, fontCSS, orF(qs.PadY, 16), orF(qs.PadX, 24), htmlFontStack(fontSerif), accent))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	case QuoteFramed:
		lblHTML := ""
		if qs.Label != "" {
			lbl := strings.ReplaceAll(qs.Label, "{n}", fmt.Sprintf("%02d", ord))
			lblHTML = fmt.Sprintf(`<span style="position:absolute;left:12px;top:-8px;background:%s;padding:0 6px;font-family:%s;font-size:10px;letter-spacing:.2em;color:%s">%s</span>`,
				uiColorCSS(pal.InnerCanvas), htmlFontStack(fontMono), accent, escapeHTMLText(lbl))
		}
		out.WriteString(fmt.Sprintf(`<blockquote style="position:relative;border:1px solid %s;padding:%.0fpx %.0fpx;%s">%s`,
			accent, orF(qs.PadY, 20), orF(qs.PadX, 24), fontCSS, lblHTML))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	case QuotePrefix:
		out.WriteString(fmt.Sprintf(`<blockquote style="%spadding:0"><span style="color:%s">&gt; </span>`, fontCSS, accent))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	default: // QuoteBar
		bgCSS := ""
		if pal.QuoteBG.A > 0 {
			bgCSS = fmt.Sprintf("background:%s;border-radius:%.0fpx;", uiColorCSS(pal.QuoteBG), pal.BlockRadius())
		}
		glowCSS := ""
		if qs.Glow.A > 0 {
			glowCSS = fmt.Sprintf("box-shadow:-6px 0 14px -8px %s;", uiColorCSS(qs.Glow))
		}
		out.WriteString(fmt.Sprintf(`<blockquote style="%s%s%sborder-left:%.0fpx solid %s;padding:%.0fpx %.0fpx">`,
			fontCSS, bgCSS, glowCSS, orF(qs.BarW, 4), accent, orF(qs.PadY, 6), orF(qs.PadX, 20)))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</blockquote>\n")
	}
}

func renderHTMLTable(out *strings.Builder, t *model.TableData, pal PaperThemeSpec, legacy bool) {
	ts := pal.Style.Table
	out.WriteString("<table>\n")
	if len(t.Widths) > 0 {
		var total float32
		for _, w := range t.Widths {
			total += w
		}
		if total > 0 {
			out.WriteString("<colgroup>")
			for _, w := range t.Widths {
				out.WriteString(fmt.Sprintf(`<col style="width:%.2f%%">`, w/total*100))
			}
			out.WriteString("</colgroup>\n")
		}
	}
	for r, row := range t.Rows {
		isHeader := r == 0 && !t.NoHeaderRow
		if isHeader {
			out.WriteString("<thead><tr>")
		} else if r == 0 || (r == 1 && !t.NoHeaderRow) {
			out.WriteString("<tbody><tr>")
		} else {
			out.WriteString("<tr>")
		}
		tag := "td"
		if isHeader {
			tag = "th"
		}
		for c, cell := range row {
			isHeaderCell := isHeader || (c == 0 && t.HeaderCol)
			cellTag := tag
			if isHeaderCell {
				cellTag = "th"
			}
			var cellCSS strings.Builder
			plain := cell.PlainText()
			isNum := !legacy && !isHeaderCell && ts.NumFamily != "" && isNumericCellText(plain)
			if a := t.CellAlign(r, c); a != model.ColAlignNone {
				cellCSS.WriteString(fmt.Sprintf("text-align:%s;", a))
			} else if isNum {
				cellCSS.WriteString("text-align:right;")
			}
			if !legacy && !isHeaderCell {
				if c == 0 && ts.FirstColor.A > 0 {
					cellCSS.WriteString("color:" + uiColorCSS(ts.FirstColor) + ";")
				} else if c > 0 && ts.Secondary.A > 0 {
					cellCSS.WriteString("color:" + uiColorCSS(ts.Secondary) + ";")
				}
				if isNum {
					cellCSS.WriteString("font-family:" + htmlFontStack(ts.NumFamily) + ";")
					if ts.NumSize > 0 {
						fmt.Fprintf(&cellCSS, "font-size:%.1fpx;", ts.NumSize)
					}
					if ts.NumWeight > 0 {
						fmt.Fprintf(&cellCSS, "font-weight:%d;", ts.NumWeight)
					}
					if ts.NumColor.A > 0 {
						cellCSS.WriteString("color:" + uiColorCSS(ts.NumColor) + ";")
					}
				}
			}
			if bg, ok := exportTableTint(t, r, c, pal); ok {
				cellCSS.WriteString("background-color:" + uiColorCSS(bg) + ";")
			}

			styleAttr := ""
			if cellCSS.Len() > 0 {
				styleAttr = fmt.Sprintf(` style="%s"`, cellCSS.String())
			}
			out.WriteString("<" + cellTag + styleAttr + ">")
			renderHTMLSpans(out, cell.Spans, pal, legacy)
			out.WriteString("</" + cellTag + ">")
		}
		if isHeader {
			out.WriteString("</tr></thead>\n")
		} else {
			out.WriteString("</tr>\n")
		}
	}
	if len(t.Rows) > 1 || (len(t.Rows) == 1 && t.NoHeaderRow) {
		out.WriteString("</tbody>")
	}
	out.WriteString("</table>\n")
}

func escapeHTMLText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func renderHTMLCallout(out *strings.Builder, b *model.Block, ord int, pal PaperThemeSpec, legacy bool) {
	cs := pal.Style.Callout
	if legacy || !cs.Custom {
		bg, border, accent, title := pal.CalloutColors(b.Callout)
		out.WriteString(fmt.Sprintf(
			`<div class="callout callout-%s" style="background:%s;border-left-color:%s;box-shadow:inset 0 0 0 1px %s"><div class="callout-title" style="color:%s">%s</div><div>`,
			strings.ToLower(title), uiColorCSS(bg), uiColorCSS(accent), uiColorCSS(border), uiColorCSS(accent), title,
		))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</div></div>\n")
		return
	}

	tone, label := pal.resolveCallout(b.Callout, legacy)
	boxed := tone.BG.A > 0 || tone.Border.A > 0
	padX, padY := orF(cs.PadX, 16), orF(cs.PadY, 11)
	if !boxed {
		padX, padY = orF(cs.PadX, 0), orF(cs.PadY, 10)
	}
	var box strings.Builder
	box.WriteString(fmt.Sprintf("margin:0 0 %.0fpx;padding:%.0fpx %.0fpx;color:%s;",
		orF(cs.Gap, 14), padY, padX, uiColorCSS(tone.FG)))
	if tone.BG.A > 0 {
		box.WriteString("background:" + uiColorCSS(tone.BG) + ";")
		if pal.Style.TextGlow.A > 0 && tone.BG.A > 160 && colorLuma(tone.FG) < 0.35 {
			box.WriteString("text-shadow:none;")
		}
	}
	if cs.Radius > 0 {
		box.WriteString(fmt.Sprintf("border-radius:%.0fpx;", cs.Radius))
	}
	if tone.Border.A > 0 {
		ls := "solid"
		if tone.Dashed {
			ls = "dashed"
		}
		box.WriteString(fmt.Sprintf("border:%.1fpx %s %s;", orF(cs.BorderW, 1), ls, uiColorCSS(tone.Border)))
	}
	if tone.TopRule.A > 0 {
		box.WriteString(fmt.Sprintf("border-top:%.1fpx solid %s;", orF(tone.TopRuleW, 1), uiColorCSS(tone.TopRule)))
	}
	if !boxed && cs.Bar > 0 {
		box.WriteString(fmt.Sprintf("border-left:%.1fpx solid %s;", cs.Bar, uiColorCSS(tone.Label)))
	}
	if !boxed && cs.Rule.A > 0 {
		box.WriteString(fmt.Sprintf("border-bottom:0.5px solid %s;", uiColorCSS(cs.Rule)))
	}
	if cs.Shadow.A > 0 {
		box.WriteString(fmt.Sprintf("box-shadow:0 3px 8px %s;", uiColorCSS(cs.Shadow)))
	}
	if cs.Angle != 0 {
		ang := cs.Angle
		if ord%2 == 0 {
			ang = -ang * 0.7
		}
		box.WriteString(fmt.Sprintf("transform:rotate(%.2fdeg);", ang))
	}
	if tone.Bold {
		box.WriteString("font-weight:600;")
	}

	var lbl strings.Builder
	lbl.WriteString(fmt.Sprintf("font-family:%s;font-size:%.1fpx;font-weight:%d;color:%s;",
		htmlFontStack(orS(cs.LabelFamily, pal.BodyFamily)), orF(cs.LabelSize, 13), orI(cs.LabelWeight, 700), uiColorCSS(tone.Label)))
	if cs.LabelTrack != 0 {
		lbl.WriteString(fmt.Sprintf("letter-spacing:%.3fem;", cs.LabelTrack))
	}
	if tone.LabelBG.A > 0 {
		lbl.WriteString(fmt.Sprintf("background:%s;padding:1px 6px;justify-self:start;align-self:start;", uiColorCSS(tone.LabelBG)))
		if pal.Style.TextGlow.A > 0 && tone.LabelBG.A > 160 && colorLuma(tone.Label) < 0.35 {
			lbl.WriteString("text-shadow:none;")
		}
	}

	switch cs.LabelPos {
	case CalloutLabelColumn:
		box.WriteString(fmt.Sprintf("display:grid;grid-template-columns:%.0fpx minmax(0,1fr);gap:12px;align-items:baseline;", orF(cs.LabelW, 92)))
		out.WriteString(fmt.Sprintf(`<div class="callout-row" style="%s"><span style="%s">%s</span><div>`, box.String(), lbl.String(), escapeHTMLText(label)))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</div></div>\n")
	case CalloutLabelInline:
		out.WriteString(fmt.Sprintf(`<div class="callout-row" style="%s"><span style="%smargin-right:8px">%s</span>`, box.String(), lbl.String(), escapeHTMLText(label)))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</div>\n")
	default:
		out.WriteString(fmt.Sprintf(`<div class="callout-row" style="%s"><div style="%smargin-bottom:4px">%s</div><div>`, box.String(), lbl.String(), escapeHTMLText(label)))
		renderHTMLSpans(out, b.Spans, pal, legacy)
		out.WriteString("</div></div>\n")
	}
}

func renderHTMLFigure(out *strings.Builder, b *model.Block, notePath string, ord int, pal PaperThemeSpec, legacy bool, budget *int) {
	if ord > maxExportImageRefs {
		*budget = 0
	}
	entry, ok := loadExportImage(notePath, b.ImageSrc, *budget)
	if !ok {
		out.WriteString("<p>" + escapeHTMLText(b.ImageAlt) + "</p>\n")
		return
	}
	*budget -= len(entry.raw)
	src := "data:" + media.ImageMIME(entry.format) + ";base64," + base64.StdEncoding.EncodeToString(entry.raw)
	width, height := float32(entry.cfg.Width), float32(entry.cfg.Height)
	is := pal.Style.Image
	if legacy {
		is = ImageStyle{Caption: true, CaptionSize: 13}
	}
	crop := exportImageCrop(b.ImageCrop, width, height)
	cropW, cropH := 1-crop.Left-crop.Right, 1-crop.Top-crop.Bottom
	placeCrop := !crop.IsZero()
	var imgCSS strings.Builder
	if placeCrop {
		imgCSS.WriteString(fmt.Sprintf("position:absolute;max-width:none;width:%.6g%%;left:%.6g%%;top:%.6g%%;",
			100/cropW, -100*crop.Left/cropW, -100*crop.Top/cropH))
	}
	outerR := orF(is.Radius, pal.Radius)
	if outerR < 0.5 {
		outerR = 0
	}
	imgR := is.InnerRadius
	if imgR == 0 && (is.Frame == ImageFrameNone || is.Frame == ImageFrameBorder) && is.Offset == 0 {
		imgR = outerR
	}
	imgCSS.WriteString("display:block;height:auto;")
	if !placeCrop {
		imgCSS.WriteString("width:100%;")
	}
	var pictureCSS strings.Builder
	if placeCrop {
		pictureCSS.WriteString(fmt.Sprintf("position:relative;overflow:hidden;aspect-ratio:%.6g;",
			width*cropW/(height*cropH)))
	}
	pictureCSS.WriteString(fmt.Sprintf("border-radius:%.0fpx;", imgR))
	if !placeCrop {
		imgCSS.WriteString(fmt.Sprintf("border-radius:%.0fpx;", imgR))
	}
	if is.Frame == ImageFrameBorder && is.Offset == 0 && is.Border.A > 0 {
		pictureCSS.WriteString(fmt.Sprintf("border:%.1fpx solid %s;", orF(is.BorderW, 1), uiColorCSS(is.Border)))
	}

	var figCSS strings.Builder
	figCSS.WriteString("margin:18px 0;position:relative;width:fit-content;max-width:100%;")
	switch b.ImageAlign {
	case model.ImageAlignCenter:
		figCSS.WriteString("margin-inline:auto;")
	case model.ImageAlignRight:
		figCSS.WriteString("margin-left:auto;")
	}
	pad, fraction := float32(0), float32(1)
	if is.Frame == ImageFramePolaroid || is.Frame == ImageFrameMat {
		pad = orF(is.Pad, 10)
		bottom := pad
		if is.Frame == ImageFramePolaroid {
			bottom, outerR = 14, 2
			if is.Angle != 0 {
				fraction = .72
			}
		}
		figCSS.WriteString(fmt.Sprintf("padding:%.0fpx %.0fpx %.0fpx;background:%s;border-radius:%.0fpx;",
			pad, pad, bottom, uiColorCSS(orC(is.BG, pal.InnerCanvas)), outerR))
		if is.Border.A > 0 {
			figCSS.WriteString(fmt.Sprintf("border:%.1fpx solid %s;", orF(is.BorderW, 1), uiColorCSS(is.Border)))
		}
	}
	side := imageCaptionSide(is, b.ImageAlt, 260)
	reserved := float32(0)
	if side > 0 {
		reserved = side + 18
		figCSS.WriteString(fmt.Sprintf("display:grid;grid-template-columns:minmax(0,1fr) min(%.0fpx,36%%);gap:18px;align-items:end;", side))
	}
	if b.ImageWidth > 0 && b.ImageWidth <= 100 {
		share := fraction * b.ImageWidth / 100
		figCSS.WriteString(fmt.Sprintf("width:calc(%.6g%% + %.6gpx);", 100*share, (2*pad+reserved)*(1-share)))
	} else {
		figCSS.WriteString(fmt.Sprintf("width:min(%.6gpx, calc(%.6g%% + %.6gpx));", width*cropW+2*pad+reserved, 100*fraction, (2*pad+reserved)*(1-fraction)))
	}
	if is.Shadow.A > 0 {
		figCSS.WriteString(fmt.Sprintf("box-shadow:0 6px 16px %s;", uiColorCSS(is.Shadow)))
	}
	if is.Angle != 0 {
		figCSS.WriteString(fmt.Sprintf("transform:rotate(%.2fdeg);", is.Angle))
	}

	out.WriteString(fmt.Sprintf(`<figure style="%s">`, figCSS.String()))
	if is.Tape.A > 0 {
		out.WriteString(fmt.Sprintf(`<span aria-hidden="true" style="position:absolute;top:-12px;left:50%%;margin-left:-44px;width:88px;height:24px;background:%s;transform:rotate(-4deg)"></span>`, uiColorCSS(is.Tape)))
	}
	if is.Corners && is.Border.A > 0 {
		bc := uiColorCSS(is.Border)
		out.WriteString(fmt.Sprintf(`<span aria-hidden="true" style="position:absolute;left:-6px;top:-6px;width:22px;height:22px;border-left:2px solid %s;border-top:2px solid %s"></span>`, bc, bc))
		out.WriteString(fmt.Sprintf(`<span aria-hidden="true" style="position:absolute;right:-6px;bottom:-6px;width:22px;height:22px;border-right:2px solid %s;border-bottom:2px solid %s"></span>`, bc, bc))
	}
	if is.Offset > 0 && is.Border.A > 0 {
		out.WriteString(fmt.Sprintf(`<div style="position:relative;margin-bottom:%.0fpx"><span aria-hidden="true" style="position:absolute;left:%.0fpx;top:%.0fpx;right:-%.0fpx;bottom:-%.0fpx;border:%.1fpx solid %s"></span>`,
			is.Offset, is.Offset, is.Offset, is.Offset, is.Offset, orF(is.BorderW, 2), uiColorCSS(is.Border)))
	}
	out.WriteString(fmt.Sprintf(`<div style="position:relative;%s"><img src="%s" alt="%s" style="%s"></div>`,
		pictureCSS.String(), escapeAttrValue(src), escapeAttrValue(b.ImageAlt), imgCSS.String()))
	if is.Offset > 0 && is.Border.A > 0 {
		out.WriteString("</div>")
	}
	if capText := strings.TrimSpace(b.ImageAlt); capText != "" && is.Caption {
		if is.CaptionPrefix != "" {
			capText = strings.ReplaceAll(is.CaptionPrefix, "{n}", fmt.Sprintf("%02d", max(ord, 1))) + capText
		}
		var capCSS strings.Builder
		capCSS.WriteString(fmt.Sprintf("margin-top:8px;font-family:%s;font-size:%.1fpx;color:%s;",
			htmlFontStack(orS(is.CaptionFamily, pal.BodyFamily)), orF(is.CaptionSize, 12), uiColorCSS(orC(is.CaptionColor, pal.Secondary))))
		if is.CaptionItalic {
			capCSS.WriteString("font-style:italic;")
		}
		if is.CaptionTrack != 0 {
			capCSS.WriteString(fmt.Sprintf("letter-spacing:%.3fem;", is.CaptionTrack))
		}
		if is.CaptionCenter {
			capCSS.WriteString("text-align:center;")
		}
		out.WriteString(fmt.Sprintf(`<figcaption style="%s">%s</figcaption>`, capCSS.String(), strings.ReplaceAll(escapeHTMLText(capText), "\n", "<br>")))
	}
	out.WriteString("</figure>\n")
}
