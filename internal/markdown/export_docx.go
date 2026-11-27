package markdown

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"
	"time"

	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

// ---- DOCX Export (OOXML zip, aligned with 12 paper themes & PaperStyle) ----

const (
	docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

type docxAssets struct {
	rels       []string
	media      []docxMedia
	nextRelID  int
	imageBytes int
}

type docxMedia struct {
	path string
	data []byte
}

func (a *docxAssets) addLink(href string) string {
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`,
		id, escapeAttrValue(href),
	))
	return id
}

func (a *docxAssets) addImage(data []byte, ext string, widthPx, heightPx float32, alt string, crop model.ImageCrop, style ImageStyle) string {
	num := len(a.media) + 1
	relTarget := fmt.Sprintf("media/image%d.%s", num, ext)
	id := fmt.Sprintf("rId%d", a.nextRelID)
	a.nextRelID++
	a.rels = append(a.rels, fmt.Sprintf(
		`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="%s"/>`,
		id, relTarget,
	))
	a.media = append(a.media, docxMedia{path: "word/" + relTarget, data: data})
	cx := int64(widthPx * 9525)
	cy := int64(heightPx * 9525)
	srcRect := ""
	if !crop.IsZero() {
		srcRect = fmt.Sprintf(`<a:srcRect l="%.0f" t="%.0f" r="%.0f" b="%.0f"/>`,
			crop.Left*100000, crop.Top*100000, crop.Right*100000, crop.Bottom*100000)
	}
	rotation := ""
	if style.Angle != 0 {
		rotation = fmt.Sprintf(` rot="%.0f"`, style.Angle*60000)
	}
	outline := ""
	if style.Border.A > 0 {
		outline = fmt.Sprintf(`<a:ln w="%.0f"><a:solidFill><a:srgbClr val="%s"><a:alpha val="%.0f"/></a:srgbClr></a:solidFill></a:ln>`,
			orF(style.BorderW, 1)*9525, docxHex(style.Border), float32(style.Border.A)*100000/255)
	}
	return fmt.Sprintf(
		`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Image %d" descr="%s"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image%d.%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/>%s<a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm%s><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>%s</pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, num, num, escapeAttrValue(alt), num, ext, id, srcRect, rotation, cx, cy, outline,
	)
}

func docxHex(c ui.Color) string {
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

func docxHexOn(c, base ui.Color) string {
	if c.A == 0 {
		return docxHex(base)
	}
	if c.A < 255 {
		c = mixUIColor(base, c, float32(c.A)/255.0)
	}
	return docxHex(c)
}

func docxTwips(px float32) int {
	v := int(px*15 + 0.5)
	if v < 0 {
		return 0
	}
	return v
}

func docxHalfPt(px float32) int {
	v := int(px*1.5 + 0.5)
	if v < 16 {
		return 16
	}
	return v
}

func docxFontXML(fam string) string {
	latin, east := splitFontPair(fam)
	l := escapeAttrValue(latin)
	e := escapeAttrValue(east)
	return fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, l, l, e, l)
}

func docxBorderVal(ls LineStyle) string {
	switch ls {
	case LineDashed:
		return "dashed"
	case LineDotted:
		return "dotted"
	default:
		return "single"
	}
}

func docxBorderSz(px float32) int {
	sz := int(px*6 + 0.5)
	if sz < 4 {
		return 4
	}
	return sz
}

func exportDOCX(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	pal, legacy := resolveExportPaper(prefs)
	if !legacy {
		theme.EnsureExportFontsForFamilies(ThemeWebFontFamilies(pal.Theme))
	}
	pageW := float32(resolveExportWidth(prefs))
	showTitle := resolveShowTitle(prefs)
	topM, rightM, bottomM, leftM := pal.Margins()
	if !showTitle && !legacy {
		topM = effectivePadTop(pal, legacy, showTitle)
		if pal.Card != nil {
			topM += pal.Card.top()
		} else if pal.Style.Sections != nil {
			topM += pal.Style.Sections.PadY
		}
	}
	contentW := pageW - leftM - rightM
	if contentW < 240 {
		contentW = 240
	}
	bodyHalfPt := docxHalfPt(pal.BodySize)
	line240 := int(pal.Leading*240 + 0.5)

	assets := &docxAssets{nextRelID: 3, imageBytes: maxExportImageBytes}
	var body strings.Builder

	title := resolveTitleText(doc, notePath, prefs)
	vars := newTemplateVars(notePath, title, time.Now().UTC())
	if showTitle {
		if !legacy {
			body.WriteString(renderDOCXMeta(pal, vars, contentW))
		}
		body.WriteString(renderDOCXTitle(pal, legacy, title, pageW, topM, rightM, leftM))
	} else {
		body.WriteString(renderDOCXNoTitleBar(pal, legacy, contentW))
	}

	bodyBlocks := projectExportPreviews(exportBodyBlocks(doc.Blocks, title, showTitle))
	var num listNumbering
	h2Count := 0
	quoteOrd := 0
	imageOrd := 0
	paraJC := ""
	if !legacy && pal.Style.Justify {
		paraJC = `<w:jc w:val="both"/>`
	}

	for i := range bodyBlocks {
		b := &bodyBlocks[i]
		ord := num.next(b)
		indentPx := float32(b.Indent) * 12
		if !legacy && b.Kind.IsList() {
			indentPx = float32(b.Indent) * orF(pal.Style.List.Gutter, 28)
		}
		indentPx = min(indentPx+16*float32(exportQuoteLevels(b)), contentW-120)
		avail := contentW - indentPx
		if avail < 120 {
			avail = 120
		}

		switch {
		case b.Kind == model.BlockDivider:
			ds := pal.Style.Divider
			divCol := orC(ds.Color, pal.Divider)
			if !legacy && ds.Kind == DividerText && ds.Text != "" {
				body.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="120" w:after="160"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r></w:p>`,
					docxHex(divCol), docxTextXML(ds.Text),
				))
			} else if !legacy && ds.Kind == DividerWave {
				body.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="120" w:after="160"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr><w:t>〰〰〰〰〰〰〰〰</w:t></w:r></w:p>`,
					docxHex(divCol),
				))
			} else if !legacy && ds.Kind == DividerRepeat && ds.Text != "" {
				rep := strings.Repeat(ds.Text, 32)
				body.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:spacing w:before="120" w:after="160"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s</w:r></w:p>`,
					docxFontXML(fontMono), docxHex(divCol), docxTextXML(rep),
				))
			} else if !legacy && ds.EndColor.A > 0 {
				rule := func(color ui.Color) string {
					return fmt.Sprintf(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="%d" w:space="0" w:color="%s"/></w:pBdr><w:spacing w:before="120" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
						docxBorderSz(orF(ds.Width, 1)), docxHex(color))
				}
				end := min(ds.EndWidth, avail)
				body.WriteString(renderDOCXBoxCells([]docxBoxCell{{avail - end, rule(divCol)}, {end, rule(ds.EndColor)}}, avail, indentPx, pal.InnerCanvas, "", 0, 0))
			} else {
				sz := docxBorderSz(orF(ds.Width, 1))
				body.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="%d" w:space="1" w:color="%s"/></w:pBdr><w:spacing w:before="120" w:after="160"/></w:pPr></w:p>`,
					sz, docxHex(divCol),
				))
			}

		case b.Kind == model.BlockImage:
			imageOrd++
			body.WriteString(renderDOCXFigure(b, notePath, imageOrd, avail, indentPx, pal, legacy, assets))

		case b.Kind == model.BlockTable && b.Table != nil:
			body.WriteString(renderDOCXTable(b.Table, avail, indentPx, pal, legacy, assets))
			body.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="100" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			body.WriteString(renderDOCXCode(b, avail, indentPx, pal, legacy))

		case b.Kind == model.BlockCallout:
			body.WriteString(renderDOCXCallout(b, avail, indentPx, line240, bodyHalfPt, pal, legacy, assets))

		case b.Kind == model.BlockQuote:
			quoteOrd++
			body.WriteString(renderDOCXQuote(b, quoteOrd, avail, indentPx, line240, bodyHalfPt, pal, legacy, assets))

		case b.Kind.IsList():
			body.WriteString(renderDOCXListItem(b, ord, indentPx, line240, bodyHalfPt, pal, legacy, assets))

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
			if lvl == 2 {
				h2Count++
			}
			body.WriteString(renderDOCXHeading(b, lvl, h2Count, indentPx, pal, legacy, assets))

		default:
			lead := !legacy && (pal.Style.Lead.Weight > 0 || pal.Style.Lead.Color.A > 0 || pal.Style.Lead.Family != "") && isLeadParagraph(bodyBlocks, i)
			if lead {
				leadPal := pal
				if pal.Style.Lead.Family != "" {
					leadPal.BodyFamily = pal.Style.Lead.Family
				}
				leadBold := orI(pal.Style.Lead.Weight, 600) >= 600
				leadCol := orC(pal.Style.Lead.Color, pal.Title)
				runs := renderDOCXRuns(b.Spans, leadPal, legacy, leadBold, false, bodyHalfPt, leadCol, assets)
				before := 0
				if i > 0 {
					before = 140
				}
				body.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:keepNext/><w:ind w:left="%d"/><w:spacing w:before="%d" w:after="80" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
					docxTwips(indentPx), before, line240, runs,
				))
				continue
			}
			runs := renderDOCXRuns(b.Spans, pal, legacy, false, false, bodyHalfPt, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr>%s<w:ind w:left="%d"/><w:spacing w:before="0" w:after="140" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				paraJC, docxTwips(indentPx), line240, runs,
			))
		}
	}

	if resolveWatermark(prefs) {
		body.WriteString(renderDOCXFooter(pal, legacy, vars, contentW))
	}

	body.WriteString(fmt.Sprintf(
		`<w:sectPr><w:pgSz w:w="%d" w:h="16838"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
		docxTwips(pageW), docxTwips(topM), docxTwips(rightM), docxTwips(bottomM), docxTwips(leftM),
	))

	docXML := fmt.Sprintf(`%s<w:document %s><w:background w:color="%s"/><w:body>%s</w:body></w:document>`,
		xmlDecl, docxNS, docxHex(pal.InnerCanvas), body.String(),
	)

	fontBundle := buildDOCXEmbeddedFonts(docXML, pal)
	stylesXML := buildDOCXStyles(pal, legacy)
	embedSettings := ""
	fontContentTypes := ""
	fontTableRel := ""
	if fontBundle.hasFonts() {
		embedSettings = `<w:embedTrueTypeFonts/><w:saveSubsetFonts/>`
		fontContentTypes = `<Default Extension="odttf" ContentType="application/vnd.openxmlformats-officedocument.obfuscatedFont"/>` +
			`<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>`
		fontTableRelID := fmt.Sprintf("rId%d", assets.nextRelID)
		assets.nextRelID++
		fontTableRel = fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/>`, fontTableRelID)
	}

	settingsXML := fmt.Sprintf(
		`%s<w:settings %s><w:displayBackgroundShape/>%s<w:autoHyphenation w:val="0"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`,
		xmlDecl, docxNS, embedSettings,
	)
	contentTypes := xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-officedocument.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/>` +
		`<Default Extension="jpg" ContentType="image/jpeg"/>` +
		`<Default Extension="gif" ContentType="image/gif"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
		fontContentTypes +
		`</Types>`
	rootRels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	docRels := fmt.Sprintf(
		`%s<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>%s%s</Relationships>`,
		xmlDecl, fontTableRel, strings.Join(assets.rels, ""),
	)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	writeZipEntry := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	entries := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(rootRels)},
		{"word/_rels/document.xml.rels", []byte(docRels)},
		{"word/styles.xml", []byte(stylesXML)},
		{"word/settings.xml", []byte(settingsXML)},
		{"word/document.xml", []byte(docXML)},
	}
	if fontBundle.hasFonts() {
		entries = append(entries,
			struct {
				name string
				data []byte
			}{"word/fontTable.xml", []byte(fontBundle.fontTableXML)},
			struct {
				name string
				data []byte
			}{"word/_rels/fontTable.xml.rels", []byte(fontBundle.fontTableRelsXML)},
		)
	}
	for _, e := range entries {
		if err := writeZipEntry(e.name, e.data); err != nil {
			return nil, err
		}
	}
	for _, fp := range fontBundle.parts {
		if err := writeZipEntry(fp.path, fp.data); err != nil {
			return nil, err
		}
	}
	for _, m := range assets.media {
		if err := writeZipEntry(m.path, m.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

func renderDOCXFigure(b *model.Block, notePath string, ord int, avail, indent float32, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	if ord <= maxExportImageRefs {
		if entry, ok := loadExportImage(notePath, b.ImageSrc, assets.imageBytes); ok {
			raw, ext := docxImageData(entry.raw, entry.format)
			if ext != "" && len(raw) <= assets.imageBytes {
				assets.imageBytes -= len(raw)
				return renderDOCXPicture(b, raw, ext, entry.cfg, ord, avail, indent, pal, legacy, assets)
			}
		}
	}
	alt := strings.TrimSpace(orS(b.ImageAlt, b.ImageSrc))
	label := "[Image]"
	if alt != "" {
		label += " " + alt
	}
	return fmt.Sprintf(`<w:p><w:pPr><w:spacing w:before="0" w:after="120"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
		docxHex(pal.Secondary), docxTextXML(label))
}

// Word can open PNG, JPEG and GIF across the supported readers. Transcode
// other editor-supported bitmap formats before embedding them.
func docxImageData(raw []byte, format string) ([]byte, string) {
	switch format {
	case "jpeg":
		return raw, "jpg"
	case "png", "gif":
		return raw, format
	}
	bitmap, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ""
	}
	var result bytes.Buffer
	if err := png.Encode(&result, bitmap); err != nil {
		return nil, ""
	}
	return result.Bytes(), "png"
}

func renderDOCXPicture(b *model.Block, raw []byte, ext string, cfg image.Config, ord int, avail, indent float32, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	is := pal.Style.Image
	if legacy {
		is = ImageStyle{}
	}
	pad := float32(0)
	if is.Frame == ImageFrameMat || is.Frame == ImageFramePolaroid {
		pad = orF(is.Pad, 10)
	}
	side := imageCaptionSide(is, b.ImageAlt, avail)
	reserved := float32(0)
	if side > 0 {
		reserved = side + 18
	}
	imageAvail := avail - 2*pad - reserved
	if is.Frame == ImageFramePolaroid && is.Angle != 0 {
		imageAvail *= .72
	}
	width, height := float32(cfg.Width), float32(cfg.Height)
	crop := exportImageCrop(b.ImageCrop, width, height)
	width *= 1 - crop.Left - crop.Right
	height *= 1 - crop.Top - crop.Bottom
	targetW := min(imageAvail, width)
	if b.ImageWidth > 0 && b.ImageWidth <= 100 {
		targetW = imageAvail * b.ImageWidth / 100
	}
	targetH := targetW * height / width

	widthWithFrame := targetW + 2*pad + reserved
	switch b.ImageAlign {
	case model.ImageAlignCenter:
		indent += (avail - widthWithFrame) / 2
	case model.ImageAlignRight:
		indent += avail - widthWithFrame
	}
	run := assets.addImage(raw, ext, targetW, targetH, b.ImageAlt, crop, is)
	figure := `<w:p><w:pPr><w:keepNext/><w:spacing w:after="0"/></w:pPr>` + run + "</w:p>"
	caption := ""
	if alt := strings.TrimSpace(b.ImageAlt); alt != "" && is.Caption {
		alt = strings.ReplaceAll(is.CaptionPrefix, "{n}", fmt.Sprintf("%02d", ord)) + alt
		jc := "left"
		if is.CaptionCenter {
			jc = "center"
		}
		italic := ""
		if is.CaptionItalic {
			italic = "<w:i/><w:iCs/>"
		}
		hp := docxHalfPt(orF(is.CaptionSize, 12))
		caption = fmt.Sprintf(`<w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="90" w:after="0"/></w:pPr><w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r></w:p>`,
			jc, docxFontXML(orS(is.CaptionFamily, pal.BodyFamily)), italic, docxHex(orC(is.CaptionColor, pal.Secondary)), hp, hp, docxTextXML(alt))
	}
	if side > 0 {
		cells := []docxBoxCell{{targetW, figure}, {18, "<w:p/>"}, {side, caption}}
		return renderDOCXBoxCells(cells, widthWithFrame, indent, pal.InnerCanvas, "", 0, 0)
	}
	if pad > 0 || caption != "" {
		bg := is.BG.Over(pal.InnerCanvas)
		return renderDOCXBoxCells([]docxBoxCell{{widthWithFrame, figure + caption}}, widthWithFrame, indent, bg, "", pad, pad)
	}
	return fmt.Sprintf(`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:after="160"/></w:pPr>%s</w:p>`, docxTwips(indent), run)
}

func renderDOCXMeta(pal PaperThemeSpec, vars templateVars, contentW float32) string {
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
	var out strings.Builder
	famXML := docxFontXML(orS(ms.Family, pal.BodyFamily))
	hp := docxHalfPt(orF(ms.Size, 12))
	col := docxHex(orC(ms.Color, pal.Secondary))
	rCol := docxHex(orC(ms.RightColor, orC(ms.Color, pal.Secondary)))
	itXML := ""
	if ms.Italic {
		itXML = "<w:i/><w:iCs/>"
	}

	if mast != "" {
		mHP := docxHalfPt(orF(ms.MastheadSize, 42))
		out.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr>%s<w:b/><w:bCs/><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r></w:p>`,
			docxFontXML(orS(ms.MastheadFamily, pal.TitleFamily)), docxHex(pal.Title), mHP, mHP, docxTextXML(mast),
		))
	}
	bdr := ""
	switch ms.Rule {
	case MetaRuleSingle:
		bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(orC(ms.RuleColor, pal.Divider)))
	case MetaRuleDouble:
		bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="double" w:sz="12" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(orC(ms.RuleColor, pal.Title)))
	}
	prefix := ""
	if ms.Moon {
		prefix = "☾ "
	}
	rightXML := ""
	if right != "" {
		rightXML = fmt.Sprintf(`<w:r><w:tab/></w:r><w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r>`,
			famXML, itXML, rCol, hp, hp, docxTextXML(right))
	}
	out.WriteString(fmt.Sprintf(
		`<w:p><w:pPr>%s<w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="0" w:after="180"/></w:pPr><w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r>%s</w:p>`,
		bdr, docxTwips(contentW), famXML, itXML, col, hp, hp, docxTextXML(prefix+left), rightXML,
	))
	return out.String()
}

func renderDOCXTitle(pal PaperThemeSpec, legacy bool, title string, pageW, topM, rightM, leftM float32) string {
	fam := pal.TitleFamily
	if fam == "" || (legacy && !isASCII(title)) {
		fam = pal.HeadingFamily
	}
	halfPt := docxHalfPt(pal.TitleSize)
	boldXML := ""
	if pal.TitleBold || pal.Style.Title.Weight >= 600 {
		boldXML = "<w:b/><w:bCs/>"
	}
	fg := pal.Title
	if !legacy && pal.Mark == TitleMarkBand {
		fg = pal.Accent2
	}
	hl := ""
	if !legacy && pal.Mark == TitleMarkHighlight {
		hl = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.HighlightBG()))
	} else if !legacy && pal.Mark == TitleMarkOffset {
		rc := orC(pal.Style.Title.RuleColor, pal.Accent2)
		hl = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(mixUIColor(pal.InnerCanvas, rc, 0.45)))
	}

	var run strings.Builder
	if !legacy && pal.Style.Title.Moon {
		run.WriteString(fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">☾ </w:t></w:r>`,
			docxHex(pal.Accent), halfPt))
	}
	if !legacy && pal.Style.Title.Prefix != "" {
		run.WriteString(fmt.Sprintf(`<w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r>`,
			docxFontXML(fam), boldXML, docxHex(pal.Secondary), halfPt, halfPt, docxTextXML(pal.Style.Title.Prefix)))
	}
	run.WriteString(fmt.Sprintf(`<w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/>%s</w:rPr>%s</w:r>`,
		docxFontXML(fam), boldXML, docxHex(fg), halfPt, halfPt, hl, docxTextXML(title)))
	if !legacy && pal.Mark == TitleMarkCursor {
		run.WriteString(fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t> ▌</w:t></w:r>`, docxHex(pal.Accent), halfPt))
	}
	if !legacy && pal.Mark == TitleMarkBand {
		full := docxTwips(pageW)
		return fmt.Sprintf(
			`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="-%d" w:type="dxa"/><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="660" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr><w:p><w:pPr><w:keepNext/><w:spacing w:before="0" w:after="0"/></w:pPr>%s</w:p></w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="240" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
			full, docxTwips(leftM), docxTwips(topM), docxTwips(leftM), docxTwips(rightM), full, full, docxHex(pal.Accent), run.String(),
		)
	}
	jc := ""
	if (!legacy && pal.Style.Title.Center) || pal.Mark == TitleMarkDoubleRule {
		jc = `<w:jc w:val="center"/>`
	}
	topBdr := ""
	if !legacy {
		if pal.Style.Title.TopRule {
			topBdr = fmt.Sprintf(`<w:top w:val="single" w:sz="4" w:space="8" w:color="%s"/>`,
				docxHexOn(orC(pal.Style.Title.RuleColor, pal.Title).Alpha(0.45), pal.InnerCanvas))
		} else if pal.Style.Title.TopTick.A > 0 {
			topBdr = fmt.Sprintf(`<w:top w:val="single" w:sz="12" w:space="6" w:color="%s"/>`, docxHex(pal.Style.Title.TopTick))
		}
	}
	botBdr := fmt.Sprintf(`<w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/>`, docxHex(pal.Accent))
	if !legacy {
		rc := orC(pal.Style.Title.RuleColor, pal.Accent)
		switch pal.Mark {
		case TitleMarkNone, TitleMarkHighlight, TitleMarkCursor, TitleMarkOffset:
			botBdr = ""
		case TitleMarkDoubleRule:
			botBdr = fmt.Sprintf(`<w:bottom w:val="double" w:sz="8" w:space="6" w:color="%s"/>`, docxHex(orC(pal.Style.Title.RuleColor, pal.Divider)))
		case TitleMarkRule:
			botBdr = fmt.Sprintf(`<w:bottom w:val="single" w:sz="%d" w:space="6" w:color="%s"/>`,
				docxBorderSz(orF(pal.Style.Title.RuleThick, 1.5)), docxHex(orC(pal.Style.Title.RuleColor, pal.Title)))
		case TitleMarkRing, TitleMarkBar:
			botBdr = fmt.Sprintf(`<w:bottom w:val="single" w:sz="8" w:space="6" w:color="%s"/>`, docxHex(rc))
		case TitleMarkWavy:
			botBdr = fmt.Sprintf(`<w:bottom w:val="wave" w:sz="8" w:space="6" w:color="%s"/>`, docxHex(rc))
		}
	}
	bdr := ""
	if topBdr != "" || botBdr != "" {
		bdr = "<w:pBdr>" + topBdr + botBdr + "</w:pBdr>"
	}
	return fmt.Sprintf(
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/><w:keepNext/>%s%s<w:spacing w:before="0" w:after="260"/></w:pPr>%s</w:p>`,
		jc, bdr, run.String(),
	)
}

func renderDOCXNoTitleBar(pal PaperThemeSpec, legacy bool, contentW float32) string {
	nb := pal.Style.NoTitleBar
	if legacy || nb.Kind == NoTitleBarNone {
		return ""
	}
	c1 := docxHexOn(orC(nb.Color, pal.Accent), pal.InnerCanvas)
	c2 := docxHexOn(orC(nb.Color2, pal.Divider), pal.InnerCanvas)
	switch nb.Kind {
	case NoTitleBarDiamond:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="200"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="18"/></w:rPr><w:t>────  </w:t></w:r><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="18"/></w:rPr><w:t>◆</w:t></w:r><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="18"/></w:rPr><w:t>  ────</w:t></w:r></w:p>`,
			c2, c1, c2,
		)
	case NoTitleBarDoubleRule:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:bottom w:val="double" w:sz="12" w:space="1" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="220" w:line="40" w:lineRule="exact"/></w:pPr></w:p>`,
			c1,
		)
	case NoTitleBarSplitBar, NoTitleBarOverprint:
		rule := func(hex string, sz int) string {
			return fmt.Sprintf(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="%d" w:space="0" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="0" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`, sz, hex)
		}
		leftW := contentW * 0.78
		rightW := contentW - leftW
		return renderDOCXBoxCells([]docxBoxCell{{leftW, rule(c1, 24)}, {rightW, rule(c2, 24)}}, contentW, 0, pal.InnerCanvas, "", 0, 0) +
			`<w:p><w:pPr><w:spacing w:before="0" w:after="160" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`
	case NoTitleBarRing:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="200"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="18"/></w:rPr><w:t>○</w:t></w:r></w:p>`,
			c1, c1,
		)
	case NoTitleBarGradient:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="10" w:space="4" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="160" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
			c1,
		)
	case NoTitleBarTickRule:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="4" w:space="4" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="200"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="16"/></w:rPr><w:t>━━</w:t></w:r></w:p>`,
			c2, c1,
		)
	case NoTitleBarMoonRule:
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="4" w:space="4" w:color="%s"/></w:pBdr><w:spacing w:before="0" w:after="200"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr><w:t>☾</w:t></w:r></w:p>`,
			c2, c1,
		)
	default:
		return ""
	}
}

func renderDOCXHeading(b *model.Block, lvl, h2Idx int, indentPx float32, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	px := pal.BodySize
	switch lvl {
	case 1:
		px = pal.HeadSizes[0]
	case 2:
		px = pal.HeadSizes[1]
	case 3:
		px = pal.HeadSizes[2]
	case 4:
		px = pal.BodySize * 1.12
	}
	halfPts := docxHalfPt(px)
	hs := pal.Style.Heading
	sub := pal.Style.Sub
	isSub := !legacy && lvl >= 3
	if isSub && sub.Size > 0 {
		halfPts = docxHalfPt(exportSubHeadingSize(sub.Size, lvl))
	}
	bold := lvl != 6 && (legacy || hs.Weight == 0 || hs.Weight >= 600)
	if isSub && sub.Weight > 0 {
		bold = sub.Weight >= 600
	}
	headCol := pal.Title
	if !legacy && hs.Color.A > 0 {
		headCol = hs.Color
	}
	if isSub && sub.Color.A > 0 {
		headCol = sub.Color
	}

	var pre strings.Builder
	if !legacy {
		switch {
		case hs.Prefix == HeadPrefixHashes:
			pre.WriteString(fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r>`,
				docxHex(orC(hs.PrefixColor, pal.Secondary)), halfPts, halfPts, docxTextXML(strings.Repeat("#", lvl)+" ")))
		case isSub && sub.Mark == SubMarkSquare:
			pre.WriteString(fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">■ </w:t></w:r>`,
				docxHex(orC(sub.MarkColor, pal.Accent)), halfPts))
		case isSub && sub.Prefix != "":
			pre.WriteString(fmt.Sprintf(`<w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r>`,
				docxFontXML(orS(sub.PrefixFont, orS(sub.Family, pal.HeadingFamily))), docxHex(orC(sub.PrefixColor, pal.Accent)), halfPts, halfPts, docxTextXML(sub.Prefix)))
		case lvl == 2 && hs.Prefix == HeadPrefixDot:
			dc := orC(hs.Accent, pal.Accent)
			if h2Idx%2 == 0 && hs.Accent2.A > 0 {
				dc = hs.Accent2
			}
			pre.WriteString(fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t>● </w:t></w:r>`, docxHex(dc), halfPts))
		case lvl == 2 && (hs.Prefix == HeadPrefixTag || hs.Prefix == HeadPrefixNumber || hs.Prefix == HeadPrefixLabel):
			pc := orC(hs.PrefixColor, orC(hs.Accent, pal.Accent))
			pre.WriteString(fmt.Sprintf(`<w:r><w:rPr>%s<w:b/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r>`,
				docxFontXML(orS(hs.PrefixFont, fontMono)), docxHex(pc), docxTextXML(fmt.Sprintf("%02d  ", max(h2Idx, 1)))))
		}
	}

	spans := b.Spans
	if isSub && sub.Mark == SubMarkBand {
		spans = append([]model.Span(nil), spans...)
		band := docxHexOn(orC(sub.MarkColor, pal.Accent), pal.InnerCanvas)
		for i := range spans {
			if spans[i].BGColor == "" {
				spans[i].BGColor = "#" + band
			}
		}
	}
	runPal := pal
	runPal.BodyFamily = pal.HeadingFamily
	if isSub {
		runPal.BodyFamily = orS(sub.Family, runPal.BodyFamily)
	}
	runs := renderDOCXRuns(spans, runPal, legacy, bold, false, halfPts, headCol, assets)
	bdr := ""
	if lvl == 2 && !legacy && hs.Underline {
		bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="%s"/></w:pBdr>`, docxHex(orC(hs.RuleColor, pal.Divider)))
	} else if isSub && sub.Mark == SubMarkDashUnder {
		bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="dashed" w:sz="6" w:space="3" w:color="%s"/></w:pBdr>`, docxHex(orC(sub.MarkColor, pal.Accent)))
	}
	return fmt.Sprintf(
		`<w:p><w:pPr><w:pStyle w:val="Heading%d"/>%s<w:spacing w:before="200" w:after="120"/><w:ind w:left="%d"/></w:pPr>%s%s</w:p>`,
		lvl, bdr, docxTwips(indentPx), pre.String(), runs,
	)
}

func renderDOCXCode(b *model.Block, avail, indentPx float32, pal PaperThemeSpec, legacy bool) string {
	cs := pal.Style.Code
	fg := pal.Text
	lblCol := pal.Secondary
	szHP := 22
	if !legacy {
		fg = orC(cs.FG, pal.Text)
		lblCol = orC(cs.Label, pal.Secondary)
		szHP = docxHalfPt(orF(cs.Size, 13.5))
	}
	lang := strings.TrimSpace(b.Language)
	if strings.EqualFold(lang, "yaml-frontmatter") || strings.EqualFold(lang, "frontmatter") {
		lang = "yaml"
	}

	if !legacy && cs.Kind == CodePlain {
		return fmt.Sprintf(
			`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="60" w:after="140" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
			docxTwips(indentPx), docxFontXML(fontMono), szHP, szHP, docxHex(fg), docxTextXML(b.Code),
		)
	}
	if !legacy && cs.Kind == CodeFenced {
		inner := fmt.Sprintf(
			`<w:p><w:pPr><w:spacing w:before="0" w:after="20"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr>%s</w:r></w:p>`+
				`<w:p><w:pPr><w:spacing w:before="0" w:after="20" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>%s</w:r></w:p>`+
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr>%s</w:r></w:p>`,
			docxFontXML(fontMono), docxHex(lblCol), szHP, docxTextXML("```"+lang),
			docxFontXML(fontMono), docxHex(fg), szHP, szHP, docxTextXML(b.Code),
			docxFontXML(fontMono), docxHex(lblCol), szHP, docxTextXML("```"),
		)
		borders := fmt.Sprintf(`<w:left w:val="single" w:sz="12" w:space="0" w:color="%s"/>`, docxHex(pal.CodeBorder))
		return renderDOCXBox(inner, avail, indentPx, pal.InnerCanvas, borders)
	}

	var inner strings.Builder
	if lang != "" && (legacy || !cs.NoLabel) {
		inner.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s</w:r></w:p>`,
			docxHex(lblCol), docxTextXML(strings.ToUpper(lang)),
		))
	}
	inner.WriteString(fmt.Sprintf(
		`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr>%s<w:sz w:val="%d"/><w:szCs w:val="%d"/><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
		docxFontXML(fontMono), szHP, szHP, docxHex(fg), docxTextXML(b.Code),
	))
	borders := ""
	if legacy || cs.BorderW >= 0 {
		val := "single"
		if !legacy && cs.Dashed {
			val = "dashed"
		}
		sz := 6
		if !legacy {
			sz = docxBorderSz(orF(cs.BorderW, 1))
		}
		bc := docxHexOn(pal.CodeBorder, pal.InnerCanvas)
		borders = fmt.Sprintf(
			`<w:top w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:left w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:bottom w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:right w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`,
			val, sz, bc, val, sz, bc, val, sz, bc, val, sz, bc,
		)
	}
	bg := pal.CodeBG
	if bg.A == 0 {
		bg = pal.InnerCanvas
	} else if bg.A < 255 {
		bg = mixUIColor(pal.InnerCanvas, bg, float32(bg.A)/255.0)
	}
	return renderDOCXBox(inner.String(), avail, indentPx, bg, borders)
}

func renderDOCXQuote(b *model.Block, ord int, avail, indentPx float32, line240, bodyHalfPt int, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	qs := pal.Style.Quote
	hp := bodyHalfPt
	bold := false
	if !legacy {
		hp = docxHalfPt(orF(qs.Size, pal.BodySize))
		bold = qs.Weight >= 600
	}
	accent := orC(qs.Accent, pal.QuoteBar)

	if !legacy && qs.Kind == QuoteIndent {
		runs := renderDOCXRuns(b.Spans, pal, legacy, bold, false, hp, pal.QuoteFG, assets)
		return fmt.Sprintf(
			`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="80" w:after="160" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
			docxTwips(indentPx+qs.Indent), line240, runs,
		)
	}
	if !legacy && qs.Kind == QuotePrefix {
		pre := fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">&gt; </w:t></w:r>`, docxHex(accent), hp)
		runs := renderDOCXRuns(b.Spans, pal, legacy, bold, false, hp, pal.QuoteFG, assets)
		return fmt.Sprintf(
			`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="60" w:after="140" w:line="%d" w:lineRule="auto"/></w:pPr>%s%s</w:p>`,
			docxTwips(indentPx), line240, pre, runs,
		)
	}
	if !legacy && qs.Kind == QuotePull {
		openQ := fmt.Sprintf(`<w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t>「</w:t></w:r>`, docxHex(accent), hp)
		closeQ := fmt.Sprintf(`<w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t>」</w:t></w:r>`, docxHex(accent), hp)
		inner := fmt.Sprintf(
			`<w:p><w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s%s%s</w:p>`,
			line240, openQ, renderDOCXRuns(b.Spans, pal, legacy, bold, false, hp, pal.QuoteFG, assets), closeQ,
		)
		bc := docxHex(pal.QuoteBar)
		borders := fmt.Sprintf(`<w:top w:val="single" w:sz="18" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/>`, bc, bc)
		return renderDOCXBox(inner, avail, indentPx, pal.InnerCanvas, borders)
	}

	var inner strings.Builder
	if !legacy && qs.Kind == QuoteFramed && qs.Label != "" {
		lbl := strings.ReplaceAll(qs.Label, "{n}", fmt.Sprintf("%02d", ord))
		inner.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="18"/></w:rPr>%s</w:r></w:p>`,
			docxFontXML(fontMono), docxHex(accent), docxTextXML(lbl),
		))
	}
	inner.WriteString(fmt.Sprintf(
		`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
		line240, renderDOCXRuns(b.Spans, pal, legacy, bold, false, hp, pal.QuoteFG, assets),
	))

	acHex := docxHex(accent)
	borders := fmt.Sprintf(`<w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/>`, docxHex(pal.QuoteBar))
	if !legacy {
		switch qs.Kind {
		case QuoteFramed, QuoteOffset:
			borders = fmt.Sprintf(
				`<w:top w:val="single" w:sz="8" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="8" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="8" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="8" w:space="0" w:color="%s"/>`,
				acHex, acHex, acHex, acHex,
			)
		case QuoteSticky, QuoteBigMark:
			if pal.QuoteBG.A > 0 {
				borders = ""
			} else {
				borders = fmt.Sprintf(`<w:left w:val="single" w:sz="12" w:space="0" w:color="%s"/>`, acHex)
			}
		default:
			borders = fmt.Sprintf(`<w:left w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`, docxBorderSz(orF(qs.BarW, 3)), acHex)
		}
	}
	bg := pal.QuoteBG
	if bg.A == 0 {
		bg = pal.InnerCanvas
	}
	return renderDOCXBox(inner.String(), avail, indentPx, bg, borders)
}

func renderDOCXListItem(b *model.Block, ord int, indentPx float32, line240, bodyHalfPt int, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	ls := pal.Style.List
	marker := "•"
	mCol := pal.Accent
	if !legacy {
		bkind := ls.Bullet
		mCol = orC(ls.BulletColor, pal.Accent)
		if b.Indent > 0 && (ls.Bullet2Size > 0 || ls.Bullet2Glyph != "" || ls.Bullet2Color.A > 0) {
			mCol = orC(ls.Bullet2Color, pal.Secondary)
			if ls.Bullet2Glyph != "" {
				bkind = 255
				marker = ls.Bullet2Glyph
			} else {
				bkind = ls.Bullet2
			}
		}
		switch bkind {
		case 255:
		case BulletSquare:
			marker = "▪"
		case BulletDiamond:
			marker = "◆"
		case BulletRing:
			marker = "◦"
		case BulletDash:
			marker = "–"
		case BulletArrow:
			marker = "→"
		case BulletMiddot:
			marker = "·"
		}
	}
	if b.Kind == model.BlockNumbered {
		if legacy {
			if ord <= 0 {
				ord = 1
			}
			marker = fmt.Sprintf("%d.", ord)
		} else {
			marker = formatListOrdinal(ord, ls.Number)
			mCol = orC(ls.NumberColor, pal.Title)
		}
	} else if b.Kind == model.BlockTodo {
		if !legacy && ls.Todo == TodoText {
			if b.Checked {
				marker = "[x]"
				mCol = orC(ls.TodoCheck, pal.Accent)
			} else {
				marker = "[ ]"
				mCol = orC(ls.TodoBorder, pal.Accent)
			}
		} else if !legacy && ls.Todo == TodoRing {
			if b.Checked {
				marker = "◉"
				mCol = orC(ls.TodoCheck, pal.Accent)
			} else {
				marker = "○"
				mCol = orC(ls.TodoBorder, pal.Accent)
			}
		} else if b.Checked {
			marker = "☑"
			if !legacy {
				mCol = orC(ls.TodoCheck, pal.Accent)
			}
		} else {
			marker = "☐"
			if !legacy {
				mCol = orC(ls.TodoBorder, pal.Accent)
			}
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

	gutter := orF(ls.Gutter, 28)
	markerHP := bodyHalfPt
	markerProps := ""
	if !legacy && b.Kind == model.BlockNumbered {
		markerProps = docxFontXML(orS(ls.NumberFont, pal.BodyFamily))
		if ls.NumberSize > 0 {
			markerHP = docxHalfPt(ls.NumberSize)
		}
		if ls.NumberShape != NumberShapeNone {
			marker = " " + marker + " "
			if ls.NumberStroke.A > 0 {
				markerProps += fmt.Sprintf(`<w:bdr w:val="single" w:sz="6" w:space="0" w:color="%s"/>`, docxHexOn(ls.NumberStroke, pal.InnerCanvas))
			}
			if ls.NumberFill.A > 0 {
				markerProps += fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHexOn(ls.NumberFill, pal.InnerCanvas))
			}
		}
	}
	runs := renderDOCXRuns(b.Spans, pal, legacy, false, strike, bodyHalfPt, itemFG, assets)
	return fmt.Sprintf(
		`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="%d"/></w:tabs><w:ind w:left="%d" w:hanging="%d"/><w:spacing w:before="0" w:after="80" w:line="%d" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/>%s</w:rPr>%s</w:r><w:r><w:tab/></w:r>%s</w:p>`,
		docxTwips(indentPx+gutter), docxTwips(indentPx+gutter), docxTwips(gutter), line240,
		docxHex(mCol), markerHP, markerProps, docxTextXML(marker), runs,
	)
}

func renderDOCXFooter(pal PaperThemeSpec, legacy bool, vars templateVars, contentW float32) string {
	if legacy {
		return fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr><w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="21"/></w:rPr>%s<w:tab/>%s</w:r></w:p>`,
			docxHex(pal.Divider), docxTwips(contentW), docxHex(pal.Secondary), docxTextXML(watermarkLabel), docxTextXML(vars.expand("{date}")),
		)
	}
	fs := pal.Style.Footer
	left := vars.expand(fs.Left)
	center := vars.expand(fs.Center)
	right := vars.expand(fs.Right)
	if left == "" && center == "" && right == "" {
		left = watermarkLabel
		right = vars.expand("{date}")
	}
	bdr := ""
	rc := docxHex(orC(fs.RuleColor, pal.Divider))
	switch fs.Rule {
	case FooterLine:
		bdr = fmt.Sprintf(`<w:pBdr><w:top w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr>`, rc)
	case FooterDashed, FooterFlank:
		bdr = fmt.Sprintf(`<w:pBdr><w:top w:val="dashed" w:sz="6" w:space="6" w:color="%s"/></w:pBdr>`, rc)
	}
	col := docxHex(orC(fs.Color, pal.Secondary))
	rCol := docxHex(orC(fs.RightColor, orC(fs.Color, pal.Secondary)))
	hp := docxHalfPt(orF(fs.Size, 12))
	famXML := docxFontXML(orS(fs.Family, pal.BodyFamily))

	if center != "" && left == "" && right == "" {
		return fmt.Sprintf(
			`<w:p><w:pPr>%s<w:jc w:val="center"/><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr>%s</w:r></w:p>`,
			bdr, famXML, col, hp, docxTextXML(center),
		)
	}
	return fmt.Sprintf(
		`<w:p><w:pPr>%s<w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr>%s</w:r><w:r><w:tab/></w:r><w:r><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr>%s</w:r></w:p>`,
		bdr, docxTwips(contentW), famXML, col, hp, docxTextXML(left), famXML, rCol, hp, docxTextXML(right),
	)
}

func buildDOCXStyles(pal PaperThemeSpec, legacy bool) string {
	bodyHalfPt := docxHalfPt(pal.BodySize)
	line240 := int(pal.Leading*240 + 0.5)
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`%s<w:styles %s><w:docDefaults><w:rPrDefault><w:rPr>%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:widowControl w:val="0"/><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`,
		xmlDecl, docxNS, docxFontXML(pal.BodyFamily), docxHex(pal.Text), bodyHalfPt, bodyHalfPt, line240,
	))
	for lvl := 1; lvl <= 6; lvl++ {
		px := pal.BodySize
		switch lvl {
		case 1:
			px = pal.HeadSizes[0]
		case 2:
			px = pal.HeadSizes[1]
		case 3:
			px = pal.HeadSizes[2]
		case 4:
			px = pal.BodySize * 1.12
		}
		hp := docxHalfPt(px)
		bTag := "<w:b/><w:bCs/>"
		if lvl == 6 {
			bTag = `<w:b w:val="0"/><w:bCs w:val="0"/>`
		}
		family := pal.HeadingFamily
		if !legacy && lvl >= 3 {
			family = orS(pal.Style.Sub.Family, family)
		}
		out.WriteString(fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			lvl, lvl, lvl-1, docxFontXML(family), bTag, docxHex(pal.Title), hp, hp,
		))
	}
	out.WriteString("</w:styles>")
	return out.String()
}

func docxTextXML(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	for li, line := range lines {
		if li > 0 {
			out.WriteString("<w:br/>")
		}
		parts := strings.Split(line, "\t")
		for pi, piece := range parts {
			if pi > 0 {
				out.WriteString("<w:tab/>")
			}
			if piece != "" {
				out.WriteString(`<w:t xml:space="preserve">`)
				out.WriteString(escapeXMLText(piece))
				out.WriteString("</w:t>")
			}
		}
	}
	return out.String()
}

func escapeXMLText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func renderDOCXRuns(spans []model.Span, pal PaperThemeSpec, legacy, baseBold, baseStrike bool, halfPt int, baseFG ui.Color, assets *docxAssets) string {
	if len(spans) == 0 {
		return ""
	}
	linkFG := pal.LinkFG()
	inl := pal.Style.Inline
	var out strings.Builder
	for _, sp := range spans {
		var rpr strings.Builder
		if sp.Marks&model.MarkCode != 0 || sp.Marks&model.MarkMath != 0 || (!legacy && sp.Link != "" && inl.LinkMono) {
			rpr.WriteString(docxFontXML(fontMono))
		} else {
			rpr.WriteString(docxFontXML(pal.BodyFamily))
		}
		isHi := !legacy && isDefaultHighlightSpan(sp) && inl.Highlight != nil
		if baseBold || sp.Marks&model.MarkBold != 0 || (isHi && inl.Highlight.Weight >= 600) {
			rpr.WriteString("<w:b/><w:bCs/>")
		}
		if sp.Marks&model.MarkItalic != 0 || sp.Marks&model.MarkMath != 0 {
			rpr.WriteString("<w:i/><w:iCs/>")
		}
		if sp.Marks&model.MarkSuperscript != 0 {
			rpr.WriteString(`<w:vertAlign w:val="superscript"/>`)
		} else if sp.Marks&model.MarkSubscript != 0 {
			rpr.WriteString(`<w:vertAlign w:val="subscript"/>`)
		}
		if baseStrike || sp.Marks&model.MarkStrike != 0 {
			rpr.WriteString("<w:strike/>")
		}
		if sp.Link != "" {
			uVal := "single"
			if !legacy {
				if inl.LinkWavy {
					uVal = "wave"
				} else if inl.LinkDotted {
					uVal = "dotted"
				}
			}
			rpr.WriteString(fmt.Sprintf(`<w:u w:val="%s"/>`, uVal))
		} else if sp.Marks&model.MarkUnderline != 0 || (isHi && inl.Highlight.Rule.A > 0) {
			uCol := ""
			if !legacy && sp.Marks&model.MarkUnderline != 0 && inl.UnderColor.A > 0 {
				uCol = fmt.Sprintf(` w:color="%s"`, docxHex(inl.UnderColor))
			} else if isHi && inl.Highlight.Rule.A > 0 {
				uCol = fmt.Sprintf(` w:color="%s"`, docxHex(inl.Highlight.Rule))
			}
			rpr.WriteString(fmt.Sprintf(`<w:u w:val="single"%s/>`, uCol))
		}

		fg := baseFG
		if !legacy && sp.Marks&model.MarkBold != 0 && inl.BoldColor.A > 0 {
			fg = inl.BoldColor
		}
		if !legacy && sp.Marks&model.MarkCode != 0 && inl.CodeFG.A > 0 {
			fg = inl.CodeFG
		}
		if isHi && inl.Highlight.FG.A > 0 {
			fg = inl.Highlight.FG
		}
		if sp.Link != "" {
			fg = linkFG
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			fg = c
		}
		rpr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, docxHex(fg)))
		rpr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPt, halfPt))

		if isHi {
			if inl.Highlight.BG.A > 0 {
				rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(mixUIColor(pal.InnerCanvas, inl.Highlight.BG, float32(inl.Highlight.BG.A)/255.0))))
			}
		} else if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(c)))
		} else if sp.Marks&model.MarkCode != 0 {
			cbg := orC(inl.CodeBG, pal.CodeBG)
			if cbg.A > 0 {
				rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHexOn(cbg, pal.InnerCanvas)))
			}
		}
		run := fmt.Sprintf(`<w:r><w:rPr>%s</w:rPr>%s</w:r>`, rpr.String(), docxTextXML(sp.Text))
		if href := media.WebLink(sp.Link); href != "" {
			rID := assets.addLink(href)
			out.WriteString(fmt.Sprintf(`<w:hyperlink r:id="%s">%s</w:hyperlink>`, rID, run))
		} else {
			out.WriteString(run)
		}
	}
	return out.String()
}

func renderDOCXCallout(b *model.Block, avail, indentPx float32, line240, bodyHalfPt int, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	cs := pal.Style.Callout
	if legacy || !cs.Custom {
		bg, border, accent, cTitle := pal.CalloutColors(b.Callout)
		var inner strings.Builder
		inner.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:spacing w:before="0" w:after="80"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r></w:p>`,
			docxHex(accent), docxTextXML(cTitle),
		))
		inner.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
			line240, renderDOCXRuns(b.Spans, pal, legacy, false, false, bodyHalfPt, pal.Text, assets),
		))
		borders := fmt.Sprintf(
			`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
			docxHex(border), docxHex(accent), docxHex(border), docxHex(border),
		)
		return renderDOCXBox(inner.String(), avail, indentPx, bg, borders)
	}

	tone, label := pal.resolveCallout(b.Callout, legacy)
	boxed := tone.BG.A > 0 || tone.Border.A > 0
	bg := pal.InnerCanvas
	if tone.BG.A > 0 {
		bg = mixUIColor(pal.InnerCanvas, tone.BG, float32(tone.BG.A)/255.0)
	}
	lHP := docxHalfPt(orF(cs.LabelSize, 13))
	lFam := docxFontXML(orS(cs.LabelFamily, pal.BodyFamily))
	lShd := ""
	if tone.LabelBG.A > 0 {
		lShd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHexOn(tone.LabelBG, bg))
	}
	lBold := ""
	if orI(cs.LabelWeight, 700) >= 600 {
		lBold = "<w:b/><w:bCs/>"
	}

	labelText := label
	if cs.LabelPos == CalloutLabelInline {
		labelText += "  "
	}
	labelRun := fmt.Sprintf(`<w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/>%s</w:rPr>%s</w:r>`,
		lFam, lBold, docxHex(tone.Label), lHP, lHP, lShd, docxTextXML(labelText))
	bodyRuns := renderDOCXRuns(b.Spans, pal, legacy, tone.Bold, false, bodyHalfPt, tone.FG, assets)
	if cs.LabelPos == CalloutLabelInline {
		bodyRuns = labelRun + bodyRuns
	}
	bodyPara := fmt.Sprintf(`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`, line240, bodyRuns)
	labelPara := `<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr>` + labelRun + "</w:p>"
	var cells []docxBoxCell
	switch cs.LabelPos {
	case CalloutLabelColumn:
		labelWidth := min(orF(cs.LabelW, 92)+14, avail/2)
		cells = []docxBoxCell{{labelWidth, labelPara}, {avail - labelWidth, bodyPara}}
	case CalloutLabelInline:
		cells = []docxBoxCell{{avail, bodyPara}}
	default:
		cells = []docxBoxCell{{avail, labelPara + bodyPara}}
	}

	var bdrs strings.Builder
	if tone.Border.A > 0 {
		val := "single"
		if tone.Dashed {
			val = "dashed"
		}
		sz := docxBorderSz(orF(cs.BorderW, 1))
		bc := docxHexOn(tone.Border, pal.InnerCanvas)
		bdrs.WriteString(fmt.Sprintf(
			`<w:top w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:left w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:bottom w:val="%s" w:sz="%d" w:space="0" w:color="%s"/><w:right w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`,
			val, sz, bc, val, sz, bc, val, sz, bc, val, sz, bc,
		))
	} else {
		if tone.TopRule.A > 0 {
			bdrs.WriteString(fmt.Sprintf(`<w:top w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`, docxBorderSz(orF(tone.TopRuleW, 1)), docxHexOn(tone.TopRule, pal.InnerCanvas)))
		}
		if !boxed && cs.Bar > 0 {
			bdrs.WriteString(fmt.Sprintf(`<w:left w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`, docxBorderSz(cs.Bar), docxHexOn(tone.Label, pal.InnerCanvas)))
		}
		if !boxed && cs.Rule.A > 0 {
			bdrs.WriteString(fmt.Sprintf(`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="%s"/>`, docxHexOn(cs.Rule, pal.InnerCanvas)))
		}
	}
	return renderDOCXBoxCells(cells, avail, indentPx, bg, bdrs.String(), 14, 10)
}

type docxBoxCell struct {
	width   float32
	content string
}

func renderDOCXBox(content string, widthPx, indentPx float32, bg ui.Color, borders string) string {
	return renderDOCXBoxCells([]docxBoxCell{{widthPx, content}}, widthPx, indentPx, bg, borders, 14, 10)
}

func renderDOCXBoxCells(cells []docxBoxCell, widthPx, indentPx float32, bg ui.Color, borders string, padX, padY float32) string {
	var out strings.Builder
	fmt.Fprintf(&out, `<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/>`,
		docxTwips(widthPx), docxTwips(indentPx))
	if borders != "" {
		out.WriteString("<w:tblBorders>" + borders + "</w:tblBorders>")
	}
	x, y := int(padX*15+0.5), int(padY*15+0.5)
	fmt.Fprintf(&out, `<w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>`, y, y, x, x)
	for _, cell := range cells {
		fmt.Fprintf(&out, `<w:gridCol w:w="%d"/>`, docxTwips(cell.width))
	}
	out.WriteString("</w:tblGrid><w:tr>")
	for _, cell := range cells {
		fmt.Fprintf(&out, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>%s</w:tc>`,
			docxTwips(cell.width), docxHex(bg), cell.content)
	}
	out.WriteString(`</w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="120" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`)
	return out.String()
}

func renderDOCXTable(t *model.TableData, widthPx, indentPx float32, pal PaperThemeSpec, legacy bool, assets *docxAssets) string {
	cols := 0
	for _, r := range t.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return ""
	}
	colWidths := make([]int, cols)
	totalTwips := docxTwips(widthPx)
	if len(t.Widths) == cols {
		var sum float32
		for _, w := range t.Widths {
			sum += w
		}
		if sum > 0 {
			for i, w := range t.Widths {
				colWidths[i] = int(float32(totalTwips)*(w/sum) + 0.5)
			}
		}
	}
	if colWidths[0] == 0 {
		each := totalTwips / cols
		for i := range colWidths {
			colWidths[i] = each
		}
	}

	var grid strings.Builder
	for _, cw := range colWidths {
		grid.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, cw))
	}

	ts := pal.Style.Table
	if legacy {
		ts = TableStyle{}
	}
	rcHex := docxHexOn(orC(ts.RuleColor, pal.TableBorder), pal.InnerCanvas)
	scHex := docxHexOn(orC(ts.StrongColor, orC(ts.RuleColor, pal.TableBorder)), pal.InnerCanvas)

	var borders string
	if !ts.Rules {
		borders = fmt.Sprintf(
			`<w:top w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="%s"/>`,
			rcHex, rcHex, rcHex, rcHex, rcHex, rcHex,
		)
	} else {
		var b strings.Builder
		if ts.Outer > 0 {
			sz := docxBorderSz(ts.Outer)
			b.WriteString(fmt.Sprintf(
				`<w:top w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="%d" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`,
				sz, scHex, sz, scHex, sz, scHex, sz, scHex,
			))
		} else {
			if ts.Top > 0 {
				b.WriteString(fmt.Sprintf(`<w:top w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`, docxBorderSz(ts.Top), scHex))
			}
			if ts.Bottom > 0 {
				b.WriteString(fmt.Sprintf(`<w:bottom w:val="single" w:sz="%d" w:space="0" w:color="%s"/>`, docxBorderSz(ts.Bottom), scHex))
			}
		}
		if ts.RowSep > 0 {
			b.WriteString(fmt.Sprintf(`<w:insideH w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`,
				docxBorderVal(ts.RowSepLine), docxBorderSz(ts.RowSep), rcHex))
		}
		if ts.ColSep > 0 {
			b.WriteString(fmt.Sprintf(`<w:insideV w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`,
				docxBorderVal(ts.ColSepLine), docxBorderSz(ts.ColSep), rcHex))
		}
		borders = b.String()
	}

	var out strings.Builder
	bdrTag := ""
	if borders != "" {
		bdrTag = "<w:tblBorders>" + borders + "</w:tblBorders>"
	}
	out.WriteString(fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/>%s<w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="90" w:type="dxa"/><w:bottom w:w="90" w:type="dxa"/><w:left w:w="120" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>%s</w:tblGrid>`,
		totalTwips, docxTwips(indentPx), bdrTag, grid.String(),
	))

	var defaultHeaderBG ui.Color
	if ts.Rules {
		defaultHeaderBG = ts.HeaderBG
	} else {
		defaultHeaderBG = orC(ts.HeaderBG, pal.TableHeaderBG())
	}

	for r, row := range t.Rows {
		isHeader := r == 0 && !t.NoHeaderRow
		out.WriteString("<w:tr>")
		for c := 0; c < cols; c++ {
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			plain := model.PlainTextOfSpans(cellSpans)
			isHeaderCell := isHeader || (c == 0 && t.HeaderCol)
			isNum := !legacy && !isHeaderCell && ts.NumFamily != "" && isNumericCellText(plain)

			shd := ""
			cellBdr := ""
			baseFG := pal.Text
			if !isHeaderCell {
				if c == 0 && ts.FirstColor.A > 0 {
					baseFG = ts.FirstColor
				} else if c > 0 && ts.Secondary.A > 0 {
					baseFG = ts.Secondary
				}
				if isNum && ts.NumColor.A > 0 {
					baseFG = ts.NumColor
				}
			} else {
				if defaultHeaderBG.A > 0 {
					shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(defaultHeaderBG))
				}
				baseFG = orC(ts.HeaderFG, pal.Title)
				if isHeader && ts.Rules && ts.HeadSep > 0 {
					cellBdr = fmt.Sprintf(`<w:tcBorders><w:bottom w:val="%s" w:sz="%d" w:space="0" w:color="%s"/></w:tcBorders>`,
						docxBorderVal(ts.HeadSepLine), docxBorderSz(ts.HeadSep), scHex)
				}
			}

			if bg, ok := exportTableTint(t, r, c, pal); ok {
				shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(bg))
			}

			jc := "left"
			if a := t.CellAlign(r, c); a != model.ColAlignNone {
				jc = string(a)
			} else if isNum {
				jc = "right"
			}
			cellHP := docxHalfPt(orF(ts.Size, 14.67))
			cellBold := false
			cellPal := pal
			if isNum {
				cellHP = docxHalfPt(orF(ts.NumSize, orF(ts.Size, 14.67)))
				cellBold = ts.NumWeight >= 600
				cellPal.BodyFamily = ts.NumFamily
			}
			if isHeaderCell {
				cellHP = docxHalfPt(orF(ts.HeaderSize, orF(ts.Size, 14.67)))
				cellBold = orI(ts.HeaderWt, 700) >= 600
				cellPal.BodyFamily = orS(ts.HeaderFont, pal.BodyFamily)
			}
			runs := renderDOCXRuns(cellSpans, cellPal, legacy, cellBold, false, cellHP, baseFG, assets)
			out.WriteString(fmt.Sprintf(
				`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s%s<w:vAlign w:val="top"/></w:tcPr><w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr>%s</w:p></w:tc>`,
				colWidths[c], cellBdr, shd, jc, runs,
			))
		}
		out.WriteString("</w:tr>")
	}
	out.WriteString("</w:tbl>")
	return out.String()
}
