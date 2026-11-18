package markdown

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"os"
	"strings"
	"time"

	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// ---- DOCX Export (OOXML zip, aligned with 12 paper styles) ----

const (
	docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
		`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

type docxAssets struct {
	rels      []string
	media     []docxMedia
	nextRelID int
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

func (a *docxAssets) addImage(data []byte, ext string, widthPx, heightPx float32, alt string) string {
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
	return fmt.Sprintf(
		`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Image %d" descr="%s"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image%d.%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, num, num, escapeAttrValue(alt), num, ext, id, cx, cy,
	)
}

func docxHex(c ui.Color) string {
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
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
	if fam == fontMono {
		return `<w:rFonts w:ascii="Menlo" w:hAnsi="Menlo" w:eastAsia="PingFang SC" w:cs="Menlo"/>`
	}
	return fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, fam, fam, fam, fam)
}

func exportDOCX(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	pal, legacy := resolveExportPaper(prefs)
	pageW := float32(resolveExportWidth(prefs))
	topM, rightM, bottomM, leftM := pal.Margins()
	contentW := pageW - leftM - rightM
	if contentW < 240 {
		contentW = 240
	}
	bodyHalfPt := docxHalfPt(pal.BodySize)
	line240 := int(pal.Leading*240 + 0.5)

	assets := &docxAssets{nextRelID: 3}
	var body strings.Builder

	showTitle := resolveShowTitle(prefs)
	title := resolveTitleText(doc, notePath, prefs)
	if showTitle {
		body.WriteString(renderDOCXTitle(pal, legacy, title, pageW, topM, rightM, leftM))
	}

	bodyBlocks := exportBodyBlocks(doc.Blocks, title, showTitle)
	var num listNumbering
	for i := range bodyBlocks {
		b := &bodyBlocks[i]
		ord := num.next(b)
		indentPx := float32(b.Indent) * 12
		avail := contentW - indentPx
		if avail < 120 {
			avail = 120
		}

		switch {
		case b.Kind == model.BlockDivider:
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="%s"/></w:pBdr><w:spacing w:before="120" w:after="160"/></w:pPr></w:p>`,
				docxHex(pal.Divider),
			))

		case b.Kind == model.BlockImage:
			if lp, ok := resolveLocalImagePath(b.ImageSrc, notePath); ok {
				if raw, err := os.ReadFile(lp); err == nil {
					if cfg, formatName, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
						ext := "png"
						switch formatName {
						case "jpeg":
							ext = "jpg"
						case "gif":
							ext = "gif"
						}
						targetW := avail
						if b.ImageWidth > 0 && b.ImageWidth <= 100 {
							targetW = avail * b.ImageWidth / 100
						} else if float32(cfg.Width) < targetW {
							targetW = float32(cfg.Width)
						}
						targetH := targetW * float32(cfg.Height) / float32(cfg.Width)
						run := assets.addImage(raw, ext, targetW, targetH, b.ImageAlt)
						jc := "left"
						if b.ImageAlign == model.ImageAlignCenter {
							jc = "center"
						} else if b.ImageAlign == model.ImageAlignRight {
							jc = "right"
						}
						body.WriteString(fmt.Sprintf(
							`<w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="160"/></w:pPr>%s</w:p>`,
							jc, run,
						))
						continue
					}
				}
			}
			alt := strings.TrimSpace(b.ImageAlt)
			if alt == "" {
				alt = strings.TrimSpace(b.ImageSrc)
			}
			label := "[Image]"
			if alt != "" {
				label = "[Image] " + alt
			}
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="120"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxHex(pal.Secondary), docxTextXML(label),
			))

		case b.Kind == model.BlockTable && b.Table != nil:
			body.WriteString(renderDOCXTable(b.Table, avail, indentPx, pal, assets))
			body.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="100" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`)

		case b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML:
			var inner strings.Builder
			if b.Language != "" {
				inner.WriteString(fmt.Sprintf(
					`<w:p><w:pPr><w:spacing w:before="0" w:after="60"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="20"/></w:rPr>%s</w:r></w:p>`,
					docxHex(pal.Secondary), docxTextXML(strings.ToUpper(b.Language)),
				))
			}
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr><w:r><w:rPr>%s<w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="%s"/></w:rPr>%s</w:r></w:p>`,
				docxFontXML(fontMono), docxHex(pal.Text), docxTextXML(b.Code),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(pal.CodeBorder), docxHex(pal.CodeBorder), docxHex(pal.CodeBorder), docxHex(pal.CodeBorder),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, pal.CodeBG, borders))

		case b.Kind == model.BlockCallout:
			bg, border, accent, cTitle := pal.CalloutColors(b.Callout)
			var inner strings.Builder
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="80"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr>%s</w:r></w:p>`,
				docxHex(accent), docxTextXML(cTitle),
			))
			inner.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				line240, renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets),
			))
			borders := fmt.Sprintf(
				`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
				docxHex(border), docxHex(accent), docxHex(border), docxHex(border),
			)
			body.WriteString(renderDOCXBox(inner.String(), avail, indentPx, bg, borders))

		case b.Kind == model.BlockQuote:
			inner := fmt.Sprintf(
				`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				line240, renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.QuoteFG, assets),
			)
			borders := fmt.Sprintf(`<w:left w:val="single" w:sz="24" w:space="0" w:color="%s"/>`, docxHex(pal.QuoteBar))
			if !legacy && pal.Theme == model.ExportThemePress {
				borders = fmt.Sprintf(
					`<w:top w:val="single" w:sz="6" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="6" w:space="0" w:color="%s"/>`,
					docxHex(pal.QuoteBar), docxHex(pal.QuoteBar),
				)
			}
			bg := pal.QuoteBG
			if bg.A == 0 {
				bg = pal.InnerCanvas
			}
			body.WriteString(renderDOCXBox(inner, avail, indentPx, bg, borders))

		case b.Kind.IsList():
			marker := "•"
			if !legacy && pal.UsesSquareBullets() {
				marker = "▪"
			}
			if b.Kind == model.BlockNumbered {
				if ord <= 0 {
					ord = 1
				}
				marker = fmt.Sprintf("%d.", ord)
			} else if b.Kind == model.BlockTodo {
				if b.Checked {
					marker = "☑"
				} else {
					marker = "☐"
				}
			}
			gutter := float32(24)
			runs := renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="%d"/></w:tabs><w:ind w:left="%d" w:hanging="%d"/><w:spacing w:before="0" w:after="80" w:line="%d" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/></w:rPr>%s</w:r><w:r><w:tab/></w:r>%s</w:p>`,
				docxTwips(indentPx+gutter), docxTwips(indentPx+gutter), docxTwips(gutter), line240,
				docxHex(pal.Accent), docxTextXML(marker), runs,
			))

		case b.Kind.IsHeading():
			lvl := b.Kind.HeadingLevel()
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
			bold := lvl != 6
			runs := renderDOCXRuns(b.Spans, pal, bold, halfPts, pal.Title, assets)
			bdr := ""
			if lvl == 2 && !legacy && pal.Theme == model.ExportThemePress {
				bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="%s"/></w:pBdr>`, docxHex(pal.Divider))
			}
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:pStyle w:val="Heading%d"/>%s<w:spacing w:before="200" w:after="120"/></w:pPr>%s</w:p>`,
				lvl, bdr, runs,
			))

		default:
			runs := renderDOCXRuns(b.Spans, pal, false, bodyHalfPt, pal.Text, assets)
			body.WriteString(fmt.Sprintf(
				`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="0" w:after="140" w:line="%d" w:lineRule="auto"/></w:pPr>%s</w:p>`,
				docxTwips(indentPx), line240, runs,
			))
		}
	}

	if resolveWatermark(prefs) {
		dateStr := time.Now().UTC().Format("2006.01.02")
		body.WriteString(fmt.Sprintf(
			`<w:p><w:pPr><w:pBdr><w:top w:val="single" w:sz="6" w:space="6" w:color="%s"/></w:pBdr><w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs><w:spacing w:before="280" w:after="0"/></w:pPr><w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="21"/></w:rPr>%s<w:tab/>%s</w:r></w:p>`,
			docxHex(pal.Divider), docxTwips(contentW), docxHex(pal.Secondary), docxTextXML(watermarkLabel), docxTextXML(dateStr),
		))
	}

	body.WriteString(fmt.Sprintf(
		`<w:sectPr><w:pgSz w:w="%d" w:h="16838"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
		docxTwips(pageW), docxTwips(topM), docxTwips(rightM), docxTwips(bottomM), docxTwips(leftM),
	))

	docXML := fmt.Sprintf(`%s<w:document %s><w:background w:color="%s"/><w:body>%s</w:body></w:document>`,
		xmlDecl, docxNS, docxHex(pal.InnerCanvas), body.String(),
	)

	stylesXML := buildDOCXStyles(pal)
	settingsXML := fmt.Sprintf(
		`%s<w:settings %s><w:displayBackgroundShape/><w:autoHyphenation w:val="0"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`,
		xmlDecl, docxNS,
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
		`</Types>`
	rootRels := xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	docRels := fmt.Sprintf(
		`%s<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>%s</Relationships>`,
		xmlDecl, strings.Join(assets.rels, ""),
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
	for _, e := range entries {
		if err := writeZipEntry(e.name, e.data); err != nil {
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

func renderDOCXTitle(pal PaperThemeSpec, legacy bool, title string, pageW, topM, rightM, leftM float32) string {
	fam := pal.HeadingFamily
	if isASCII(title) {
		fam = pal.TitleFamily
	}
	halfPt := docxHalfPt(pal.TitleSize)
	boldXML := ""
	if pal.TitleBold {
		boldXML = "<w:b/><w:bCs/>"
	}
	fg := pal.Title
	if !legacy && pal.Mark == TitleMarkBand {
		fg = pal.Accent2
	}
	hl := ""
	if !legacy && pal.Mark == TitleMarkHighlight {
		hl = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.HighlightBG()))
	}
	run := fmt.Sprintf(`<w:r><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/>%s</w:rPr>%s</w:r>`,
		docxFontXML(fam), boldXML, docxHex(fg), halfPt, halfPt, hl, docxTextXML(title))
	if !legacy && pal.Mark == TitleMarkCursor {
		run += fmt.Sprintf(`<w:r><w:rPr><w:color w:val="%s"/><w:sz w:val="%d"/></w:rPr><w:t> ▌</w:t></w:r>`, docxHex(pal.Accent), halfPt)
	}
	if !legacy && pal.Mark == TitleMarkBand {
		full := docxTwips(pageW)
		return fmt.Sprintf(
			`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="-%d" w:type="dxa"/><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="660" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr><w:p><w:pPr><w:keepNext/><w:spacing w:before="0" w:after="0"/></w:pPr>%s</w:p></w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="240" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
			full, docxTwips(leftM), docxTwips(topM), docxTwips(leftM), docxTwips(rightM), full, full, docxHex(pal.Accent), run,
		)
	}
	jc := ""
	bdr := fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Accent))
	if !legacy {
		switch pal.Mark {
		case TitleMarkDoubleRule:
			jc = `<w:jc w:val="center"/>`
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="double" w:sz="8" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Divider))
		case TitleMarkRule:
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="12" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Title))
		case TitleMarkWavy:
			bdr = fmt.Sprintf(`<w:pBdr><w:bottom w:val="wave" w:sz="8" w:space="6" w:color="%s"/></w:pBdr>`, docxHex(pal.Accent))
		case TitleMarkHighlight, TitleMarkCursor:
			bdr = ""
		}
	}
	return fmt.Sprintf(
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/><w:keepNext/>%s%s<w:spacing w:before="0" w:after="260"/></w:pPr>%s</w:p>`,
		jc, bdr, run,
	)
}

func buildDOCXStyles(pal PaperThemeSpec) string {
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
		out.WriteString(fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr>%s%s<w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			lvl, lvl, lvl-1, docxFontXML(pal.HeadingFamily), bTag, docxHex(pal.Title), hp, hp,
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

func renderDOCXRuns(spans []model.Span, pal PaperThemeSpec, baseBold bool, halfPt int, baseFG ui.Color, assets *docxAssets) string {
	if len(spans) == 0 {
		return ""
	}
	linkFG := pal.LinkFG()
	var out strings.Builder
	for _, sp := range spans {
		var rpr strings.Builder
		if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(docxFontXML(fontMono))
		}
		if baseBold || sp.Marks&model.MarkBold != 0 {
			rpr.WriteString("<w:b/><w:bCs/>")
		}
		if sp.Marks&model.MarkItalic != 0 {
			rpr.WriteString("<w:i/><w:iCs/>")
		}
		if sp.Marks&model.MarkStrike != 0 {
			rpr.WriteString("<w:strike/>")
		}
		if sp.Marks&model.MarkUnderline != 0 || sp.Link != "" {
			rpr.WriteString(`<w:u w:val="single"/>`)
		}
		fg := baseFG
		if sp.Link != "" {
			fg = linkFG
		}
		if c, ok := resolveSpanColor(sp.FGColor, false, pal); ok {
			fg = c
		}
		rpr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, docxHex(fg)))
		rpr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPt, halfPt))
		if c, ok := resolveSpanColor(sp.BGColor, true, pal); ok {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(c)))
		} else if sp.Marks&model.MarkCode != 0 {
			rpr.WriteString(fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(pal.CodeBG)))
		}
		run := fmt.Sprintf(`<w:r><w:rPr>%s</w:rPr>%s</w:r>`, rpr.String(), docxTextXML(sp.Text))
		if sp.Link != "" {
			rID := assets.addLink(sp.Link)
			out.WriteString(fmt.Sprintf(`<w:hyperlink r:id="%s">%s</w:hyperlink>`, rID, run))
		} else {
			out.WriteString(run)
		}
	}
	return out.String()
}

func renderDOCXBox(content string, widthPx, indentPx float32, bg ui.Color, borders string) string {
	wTwips := docxTwips(widthPx)
	iTwips := docxTwips(indentPx)
	return fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="150" w:type="dxa"/><w:bottom w:w="150" w:type="dxa"/><w:left w:w="210" w:type="dxa"/><w:right w:w="210" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>%s</w:tc></w:tr></w:tbl><w:p><w:pPr><w:spacing w:before="0" w:after="120" w:line="20" w:lineRule="exact"/></w:pPr></w:p>`,
		wTwips, iTwips, borders, wTwips, wTwips, docxHex(bg), content,
	)
}

func renderDOCXTable(t *model.TableData, widthPx, indentPx float32, pal PaperThemeSpec, assets *docxAssets) string {
	cols := 0
	if len(t.Rows) > 0 {
		cols = len(t.Rows[0])
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
	bc := docxHex(pal.TableBorder)
	borders := fmt.Sprintf(
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:left w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:right w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="%s"/>`,
		bc, bc, bc, bc, bc, bc,
	)
	var out strings.Builder
	out.WriteString(fmt.Sprintf(
		`<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblInd w:w="%d" w:type="dxa"/><w:tblBorders>%s</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="90" w:type="dxa"/><w:bottom w:w="90" w:type="dxa"/><w:left w:w="120" w:type="dxa"/><w:right w:w="120" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>%s</w:tblGrid>`,
		totalTwips, docxTwips(indentPx), borders, grid.String(),
	))
	headerBG := pal.TableHeaderBG()
	for r, row := range t.Rows {
		isHeader := r == 0
		out.WriteString("<w:tr>")
		for c := 0; c < cols; c++ {
			var cellSpans []model.Span
			if c < len(row) {
				cellSpans = row[c].Spans
			}
			shd := ""
			baseFG := pal.Text
			if isHeader {
				shd = fmt.Sprintf(`<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, docxHex(headerBG))
				baseFG = pal.Title
			}
			jc := "left"
			if c < len(t.Aligns) && t.Aligns[c] != model.ColAlignNone {
				jc = string(t.Aligns[c])
			}
			runs := renderDOCXRuns(cellSpans, pal, isHeader, 22, baseFG, assets)
			out.WriteString(fmt.Sprintf(
				`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s<w:vAlign w:val="top"/></w:tcPr><w:p><w:pPr><w:jc w:val="%s"/><w:spacing w:before="0" w:after="0" w:line="320" w:lineRule="auto"/></w:pPr>%s</w:p></w:tc>`,
				colWidths[c], shd, jc, runs,
			))
		}
		out.WriteString("</w:tr>")
	}
	out.WriteString("</w:tbl>")
	return out.String()
}
