package markdown

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"block/internal/model"
)

func TestCodecInterface(t *testing.T) {
	var codec model.MarkdownCodec = NewCodec()
	src := "# Hello\n\nWorld\n"
	doc := codec.Parse(src)
	if got := codec.Serialize(doc); got != src {
		t.Fatalf("expected lossless round-trip %q, got %q", src, got)
	}
	norm := codec.NormalizePaste("＃ 标题\r\n\r\n－\u3000项目")
	if !strings.Contains(norm, "# 标题") || !strings.Contains(norm, "- 项目") {
		t.Fatalf("unexpected normalized paste: %q", norm)
	}
}

func TestLosslessRoundTripAndPartialDirty(t *testing.T) {
	src := "---\ntitle: Demo\n---\n\nHeading Setext\n==============\n\n- item 1\n  - nested item\n\n* * *\n\nUntouched [ref][r1] paragraph.\n\n[r1]: https://example.com\n"
	doc := ParseMarkdown(src)
	if got := SerializeMarkdown(doc); got != src {
		t.Fatalf("untouched round-trip mismatch:\nwant:\n%q\ngot:\n%q", src, got)
	}

	// Edit only the Setext heading; other clean blocks must retain CleanSource.
	edited := false
	for i := range doc.Blocks {
		if doc.Blocks[i].Kind == model.BlockHeading1 {
			doc.Blocks[i].SetText("Heading Edited")
			edited = true
			break
		}
	}
	if !edited {
		t.Fatal("expected to find BlockHeading1")
	}
	out := SerializeMarkdown(doc)
	if !strings.Contains(out, "# Heading Edited\n") {
		t.Fatalf("expected updated ATX heading, got:\n%s", out)
	}
	if !strings.Contains(out, "---\ntitle: Demo\n---\n") {
		t.Fatalf("expected untouched frontmatter CleanSource, got:\n%s", out)
	}
	if !strings.Contains(out, "* * *\n") {
		t.Fatalf("expected untouched divider CleanSource '* * *', got:\n%s", out)
	}

	// Deleting a block from an untouched parsed doc must NOT return stale CleanRaw.
	doc2 := ParseMarkdown("# One\n\nTwo\n\nThree\n")
	doc2.Blocks = append(doc2.Blocks[:1], doc2.Blocks[2:]...)
	out2 := SerializeMarkdown(doc2)
	if strings.Contains(out2, "Two") {
		t.Fatalf("deleted block leaked from CleanRaw: %q", out2)
	}
	if !strings.Contains(out2, "# One") || !strings.Contains(out2, "Three") {
		t.Fatalf("remaining blocks missing after delete: %q", out2)
	}

	// Mutating spans directly without setting Dirty must still be detected.
	doc3 := ParseMarkdown("# Title\n\nBody\n")
	doc3.Blocks[1].Spans[0].Text = "Mutated"
	out3 := SerializeMarkdown(doc3)
	if !strings.Contains(out3, "Mutated") || strings.Contains(out3, "Body") {
		t.Fatalf("direct span mutation was ignored: %q", out3)
	}

	// No trailing newline preserved on untouched doc.
	noNL := "Single line without newline"
	if got := SerializeMarkdown(ParseMarkdown(noNL)); got != noNL {
		t.Fatalf("expected %q, got %q", noNL, got)
	}
}

func TestBlockTypesParsingAndSerialization(t *testing.T) {
	src := strings.Join([]string{
		"# H1",
		"## H2",
		"### H3",
		"#### H4",
		"##### H5",
		"###### H6",
		"",
		"<p data-indent=\"2\">Indented para</p>",
		"",
		"- Bullet A",
		"  - Bullet B",
		"3. Ordered start",
		"4. Ordered next",
		"- [ ] Todo open",
		"- [x] Todo done",
		"",
		"> Simple quote line 1",
		"> Simple quote line 2",
		"",
		"> [!NOTE]",
		"> Note callout body",
		"",
		"> [!WARNING]",
		"> Warning callout body",
		"",
		"::: tip Custom Tip",
		"Tip line",
		":::",
		"",
		"```go",
		"fmt.Println(\"```\")",
		"```",
		"",
		"$$",
		"E = mc^2",
		"$$",
		"",
		"---",
		"",
		"| Name | Score | Rate |",
		"| :--- | :---: | ---: |",
		"| Alice | 100 | 99% |",
		"",
		"<table><tr><th style=\"text-align:center\">Col</th><th>Val</th></tr><tr><td>A</td><td>B</td></tr></table>",
		"",
		"<!-- thinkly:image crop=0.1,0.2,0.1,0.2 -->",
		"![alt](assets/pic.png)",
		"",
		"<p align=\"center\"><img src=\"assets/wide.png\" alt=\"wide\" width=\"60%\"></p>",
		"",
		"<svg width=\"10\" height=\"10\"><circle cx=\"5\" cy=\"5\" r=\"4\"/></svg>",
	}, "\n") + "\n"

	doc := ParseMarkdown(src)

	var counts = map[model.BlockKind]int{}
	var hasH6, hasIndentedPara, hasTodoChecked, hasWarningCallout, hasMath, hasHTMLTable, hasPipeTable bool
	var hasCroppedImg, hasAlignedImg, hasRawHTML bool

	for _, b := range doc.Blocks {
		counts[b.Kind]++
		switch b.Kind {
		case model.BlockHeading6:
			hasH6 = true
		case model.BlockParagraph:
			if b.Indent == 2 && b.PlainText() == "Indented para" {
				hasIndentedPara = true
			}
		case model.BlockTodo:
			if b.Checked && b.PlainText() == "Todo done" {
				hasTodoChecked = true
			}
		case model.BlockCallout:
			if b.Callout == model.CalloutWarning {
				hasWarningCallout = true
			}
		case model.BlockCode:
			if b.Language == "math" && b.Code == "E = mc^2" {
				hasMath = true
			}
		case model.BlockTable:
			if b.Table != nil {
				if b.Table.WasHTML {
					hasHTMLTable = true
				} else {
					hasPipeTable = true
				}
			}
		case model.BlockImage:
			if !b.ImageCrop.IsZero() && b.ImageSrc == "assets/pic.png" {
				hasCroppedImg = true
			}
			if b.ImageAlign == model.ImageAlignCenter && b.ImageWidth == 60 {
				hasAlignedImg = true
			}
		case model.BlockRawHTML:
			if strings.Contains(b.Code, "<svg") {
				hasRawHTML = true
			}
		}
	}

	if !hasH6 || !hasIndentedPara || !hasTodoChecked || !hasWarningCallout || !hasMath ||
		!hasHTMLTable || !hasPipeTable || !hasCroppedImg || !hasAlignedImg || !hasRawHTML {
		t.Fatalf("missing expected parsed blocks: h6=%v indent=%v todo=%v warn=%v math=%v htmlTbl=%v pipeTbl=%v cropImg=%v alignImg=%v rawHTML=%v",
			hasH6, hasIndentedPara, hasTodoChecked, hasWarningCallout, hasMath,
			hasHTMLTable, hasPipeTable, hasCroppedImg, hasAlignedImg, hasRawHTML)
	}

	// Force every block dirty and confirm re-serialization produces valid re-parseable Markdown.
	for i := range doc.Blocks {
		doc.Blocks[i].Dirty = true
		doc.Blocks[i].CleanSource = ""
	}
	doc.CleanRaw = ""
	rebuilt := SerializeMarkdown(doc)
	reparsed := ParseMarkdown(rebuilt)
	if len(reparsed.Blocks) < 15 {
		t.Fatalf("expected rich reparsed block list, got %d blocks:\n%s", len(reparsed.Blocks), rebuilt)
	}
	if !strings.Contains(rebuilt, "````go\nfmt.Println(\"```\")\n````") {
		t.Fatalf("expected dynamic 4-backtick fence for code containing ```, got:\n%s", rebuilt)
	}
	if !strings.Contains(rebuilt, "<!-- thinkly:image crop=0.1,0.2,0.1,0.2 -->") {
		t.Fatalf("expected image crop metadata comment on serialize, got:\n%s", rebuilt)
	}
}

func TestInlineParsingAndSerialization(t *testing.T) {
	input := `**bold** *italic* <u>under</u> ~~strike~~ ` + "`code`" + ` [link](https://go.dev) <span style="color:red;background-color:yellow">hi</span>`
	spans := ParseInline(input)
	var gotBold, gotItalic, gotUnder, gotStrike, gotCode, gotLink, gotColor bool
	for _, s := range spans {
		if s.Text == "bold" && s.Marks&model.MarkBold != 0 {
			gotBold = true
		}
		if s.Text == "italic" && s.Marks&model.MarkItalic != 0 {
			gotItalic = true
		}
		if s.Text == "under" && s.Marks&model.MarkUnderline != 0 {
			gotUnder = true
		}
		if s.Text == "strike" && s.Marks&model.MarkStrike != 0 {
			gotStrike = true
		}
		if s.Text == "code" && s.Marks&model.MarkCode != 0 {
			gotCode = true
		}
		if s.Text == "link" && s.Link == "https://go.dev" {
			gotLink = true
		}
		if s.Text == "hi" && s.FGColor == "red" && s.BGColor == "yellow" {
			gotColor = true
		}
	}
	if !(gotBold && gotItalic && gotUnder && gotStrike && gotCode && gotLink && gotColor) {
		t.Fatalf("inline parse missed styles: %+v", spans)
	}

	rt := SerializeInline(spans)
	reparsed := ParseInline(rt)
	if !spansEqual(spans, reparsed) {
		t.Fatalf("inline round-trip mismatch:\nwant: %+v\nmd:   %q\ngot:  %+v", spans, rt, reparsed)
	}

	// CJK fullwidth punctuation bold (`**11/11：**汕头`).
	cjk := ParseInline("**11/11：**汕头龙湖区")
	if len(cjk) != 2 || cjk[0].Text != "11/11：" || cjk[0].Marks&model.MarkBold == 0 || cjk[1].Text != "汕头龙湖区" {
		t.Fatalf("CJK bold failed: %+v", cjk)
	}
}

func TestNormalizePasteText(t *testing.T) {
	// 1. Chaoshan / fullwidth punctuation paste cleanup.
	raw := "\uFEFF＃ 潮汕五日行程攻略\r\n\r\n\r\n\r\n－\u3000＊＊11/11：＊＊汕头龙湖区\u200B\r\n• ［x］ 预订高铁票\r\n１．\u3000牌坊街\r\n\r\n｜ 餐别 ｜ 店名 ｜\r\n｜ --- ｜ --- ｜\r\n｜ 午餐 ｜ 杏花吴记 ｜\r\n\r\n```go\r\n//   keep code   \r\n```"
	norm := NormalizePasteText(raw)
	doc := ParseMarkdown(norm)

	var hasH1, hasBulletBold, hasTodoDone, hasOrdered, hasTable, hasCode bool
	for _, b := range doc.Blocks {
		if b.Kind == model.BlockHeading1 && b.PlainText() == "潮汕五日行程攻略" {
			hasH1 = true
		}
		if b.Kind == model.BlockBullet && strings.Contains(b.PlainText(), "11/11：") && len(b.Spans) > 0 && b.Spans[0].Marks&model.MarkBold != 0 {
			hasBulletBold = true
		}
		if b.Kind == model.BlockTodo && b.Checked && b.PlainText() == "预订高铁票" {
			hasTodoDone = true
		}
		if b.Kind == model.BlockNumbered && b.PlainText() == "牌坊街" {
			hasOrdered = true
		}
		if b.Kind == model.BlockTable && b.Table != nil && len(b.Table.Rows) == 2 {
			hasTable = true
		}
		if b.Kind == model.BlockCode && b.Language == "go" && b.Code == "//   keep code   " {
			hasCode = true
		}
	}
	if !(hasH1 && hasBulletBold && hasTodoDone && hasOrdered && hasTable && hasCode) {
		t.Fatalf("chaoshan paste normalize failed:\nnorm:\n%s", norm)
	}

	// 2. Entity-escaped Markdown recovery.
	escaped := "&#35; 潮汕之旅\n\n&#45; &#42;&#42;11&#47;11：&#42;&#42;汕头龙湖区。\n\n&#124; 餐别 &#124; 店名 &#124;\n&#124; &#45;&#45;&#45; &#124; &#45;&#45;&#45; &#124;\n&#124; 午餐 &#124; 杏花吴记 &#124;"
	rec := NormalizePasteText(escaped)
	recDoc := ParseMarkdown(rec)
	if len(recDoc.Blocks) < 3 || recDoc.Blocks[0].Kind != model.BlockHeading1 || recDoc.Blocks[1].Kind != model.BlockBullet || recDoc.Blocks[2].Kind != model.BlockTable {
		t.Fatalf("entity-escaped recovery failed:\nrec:\n%s", rec)
	}
}

func makeSampleImage(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 84, G: 125, B: 208, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode sample png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write sample png: %v", err)
	}
}

func TestExportAllFormats(t *testing.T) {
	dir := t.TempDir()
	imgDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(imgDir, "hero.png")
	makeSampleImage(t, imgPath)
	notePath := filepath.Join(dir, "潮汕之旅.md")

	src := strings.Join([]string{
		"# 潮汕美食与行程",
		"",
		"中文、日本語テスト、English with **bold**, *italic*, <u>underline</u>, ~~strike~~, `code`, [link](https://example.com), and <span style=\"color:blue;background-color:yellow\">highlight</span>.",
		"",
		"> [!TIP]",
		"> 记得提前预约牛肉火锅！",
		"",
		"- [x] 潮汕站接驳",
		"1. 龙湖区午餐",
		"",
		"| 项目 | 评分 |",
		"| --- | :---: |",
		"| 杏花吴记 | ★★★★★ |",
		"",
		"```go",
		"println(\"hello\")",
		"```",
		"",
		"![Hero](assets/hero.png)",
	}, "\n") + "\n"
	doc := ParseMarkdown(src)
	prefs := model.ExportPreferences{
		Theme:       "light",
		IncludeMeta: true,
		Width:       816,
	}

	// 1. Markdown
	mdBytes, err := ExportDocument(doc, notePath, model.ExportMarkdown, prefs)
	if err != nil || !strings.Contains(string(mdBytes), "潮汕美食与行程") {
		t.Fatalf("ExportMarkdown failed: err=%v", err)
	}

	// 2. HTML
	htmlBytes, err := ExportDocument(doc, notePath, model.ExportHTML, prefs)
	if err != nil {
		t.Fatalf("ExportHTML failed: %v", err)
	}
	htmlStr := string(htmlBytes)
	if !strings.Contains(htmlStr, "<!DOCTYPE html>") ||
		!strings.Contains(htmlStr, "callout-tip") ||
		!strings.Contains(htmlStr, "data:image/png;base64,") ||
		!strings.Contains(htmlStr, "#547dd0") {
		t.Fatalf("ExportHTML missing expected Ocean Paper content")
	}

	// 3. PNG (ExportScale)
	pngBytes, err := ExportDocument(doc, notePath, model.ExportPNG, prefs)
	if err != nil {
		t.Fatalf("ExportPNG failed: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("ExportPNG invalid PNG: %v", err)
	}
	if cfg.Width != int(816*ExportScale) || cfg.Height < 400 {
		t.Fatalf("unexpected PNG dimensions: %dx%d", cfg.Width, cfg.Height)
	}

	// 4. PDF (single-page and multi-page)
	pdfBytes, err := ExportDocument(doc, notePath, model.ExportPDF, prefs)
	if err != nil {
		t.Fatalf("ExportPDF failed: %v", err)
	}
	pdfStr := string(pdfBytes)
	if !strings.HasPrefix(pdfStr, "%PDF-1.4") || !strings.Contains(pdfStr, "/FlateDecode") || !strings.HasSuffix(strings.TrimSpace(pdfStr), "%%EOF") {
		t.Fatalf("ExportPDF invalid PDF structure")
	}

	// Multi-page PDF test using a tall doc.
	var tall strings.Builder
	tall.WriteString("# Tall CJK/JP/EN Document\n\n")
	for i := 0; i < 65; i++ {
		tall.WriteString("这是一段用于验证多页 PDF 导出的长文本，包含日本語のテスト与 English words。\n\n")
	}
	tallDoc := ParseMarkdown(tall.String())
	tallPDF, err := ExportDocument(tallDoc, notePath, model.ExportPDF, prefs)
	if err != nil {
		t.Fatalf("multi-page ExportPDF failed: %v", err)
	}
	if !strings.Contains(string(tallPDF), "/Count 2") && !strings.Contains(string(tallPDF), "/Count 3") {
		t.Fatalf("expected multi-page PDF /Count >= 2")
	}

	// 5. DOCX (OOXML zip + valid XML)
	docxBytes, err := ExportDocument(doc, notePath, model.ExportDOCX, prefs)
	if err != nil {
		t.Fatalf("ExportDOCX failed: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(docxBytes), int64(len(docxBytes)))
	if err != nil {
		t.Fatalf("ExportDOCX invalid zip: %v", err)
	}
	docxFiles := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		docxFiles[f.Name] = b.Bytes()
	}
	for _, req := range []string{"[Content_Types].xml", "_rels/.rels", "word/_rels/document.xml.rels", "word/styles.xml", "word/settings.xml", "word/document.xml", "word/media/image1.png"} {
		data, ok := docxFiles[req]
		if !ok {
			t.Fatalf("DOCX missing %s", req)
		}
		if strings.HasSuffix(req, ".xml") || strings.HasSuffix(req, ".rels") {
			var v any
			if err := xml.Unmarshal(data, &v); err != nil {
				t.Fatalf("DOCX invalid XML in %s: %v", req, err)
			}
		}
	}
	if !strings.Contains(string(docxFiles["word/document.xml"]), "潮汕美食与行程") {
		t.Fatalf("DOCX document.xml missing CJK content")
	}

	// 6. ZIP (note Markdown + local images)
	zipBytes, err := ExportDocument(doc, notePath, model.ExportZIP, prefs)
	if err != nil {
		t.Fatalf("ExportZIP failed: %v", err)
	}
	zr2, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("ExportZIP invalid zip: %v", err)
	}
	var hasZipMD, hasZipImg bool
	for _, f := range zr2.File {
		if f.Name == "潮汕之旅.md" {
			hasZipMD = true
		}
		if f.Name == "assets/hero.png" {
			hasZipImg = true
		}
	}
	if !hasZipMD || !hasZipImg {
		t.Fatalf("ExportZIP missing expected entries: md=%v img=%v", hasZipMD, hasZipImg)
	}

	// 7. ExportToFile atomic write
	outFile := filepath.Join(dir, "out", "export.html")
	if err := ExportToFile(doc, notePath, outFile, model.ExportHTML, model.ExportPreferences{Theme: "dark"}); err != nil {
		t.Fatalf("ExportToFile failed: %v", err)
	}
	diskData, err := os.ReadFile(outFile)
	if err != nil || !strings.Contains(string(diskData), "#181818") {
		t.Fatalf("ExportToFile dark theme output invalid: err=%v", err)
	}
}

func TestExportAll12PaperThemesAndOptions(t *testing.T) {
	dir := t.TempDir()
	notePath := filepath.Join(dir, "paper-themes.md")

	src := strings.Join([]string{
		"# 纸面主题导出",
		"",
		"正文段落包含 [普通链接](https://example.com/body)、<mark>高亮重点</mark> 与 **加粗**。",
		"",
		"## 核心书单",
		"",
		"| 列名 | 链接 | 页数 |",
		"| --- | --- | ---: |",
		"| 文档 | [表格链接](https://example.com/table) | 176 |",
		"",
		"## 读前准备",
		"",
		"> 引用文本",
		"",
		"- 列表项",
		"1. 有序步骤",
		"- [ ] 待办事项",
		"- [x] 已完成",
		"",
		"---",
		"",
		"```go",
		"fmt.Println(1)",
		"```",
	}, "\n") + "\n"
	doc := ParseMarkdown(src)

	allThemes := []model.ExportTheme{
		model.ExportThemeMono,
		model.ExportThemeJournal,
		model.ExportThemeRiso,
		model.ExportThemePress,
		model.ExportThemePoster,
		model.ExportThemeMist,
		model.ExportThemeInk,
		model.ExportThemeKraft,
		model.ExportThemeNeon,
		model.ExportThemeFilm,
		model.ExportThemeNight,
		model.ExportThemeTerminal,
	}

	for _, th := range allThemes {
		spec := PaperTheme(th)
		prefs := model.ExportPreferences{
			Style: th,
			Title: "自定义标题",
		}

		// PNG (ExportScale)
		pngBytes, err := ExportDocument(doc, notePath, model.ExportPNG, prefs)
		if err != nil {
			t.Fatalf("theme %s ExportPNG failed: %v", th, err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
		if err != nil || cfg.Width != int(defaultExportWidth*ExportScale) || cfg.Height < 200 {
			t.Fatalf("theme %s invalid PNG %dx%d err=%v", th, cfg.Width, cfg.Height, err)
		}

		// HTML: defaults ShowTitle=true, Watermark=true ("cooool.top"), table link preserves theme color, no extra theme badge
		htmlBytes, err := ExportDocument(doc, notePath, model.ExportHTML, prefs)
		if err != nil {
			t.Fatalf("theme %s ExportHTML failed: %v", th, err)
		}
		htmlStr := string(htmlBytes)
		linkCSS := uiColorCSS(spec.LinkFG())
		if !strings.Contains(htmlStr, "自定义标题") {
			t.Fatalf("theme %s HTML missing custom Title", th)
		}
		if !strings.Contains(htmlStr, `<footer class="doc-footer"`) || !strings.Contains(strings.ToLower(htmlStr), "cooool.top") {
			t.Fatalf("theme %s HTML missing default cooool.top watermark", th)
		}
		if !strings.Contains(htmlStr, "table a,th a,td a{color:"+linkCSS) {
			t.Fatalf("theme %s HTML missing themed table link CSS color %s", th, linkCSS)
		}
		if strings.Contains(htmlStr, "theme-badge") || strings.Contains(htmlStr, "theme-tag") {
			t.Fatalf("theme %s HTML unexpectedly emitted extra theme badge", th)
		}

		// PDF
		pdfBytes, err := ExportDocument(doc, notePath, model.ExportPDF, prefs)
		if err != nil || !strings.HasPrefix(string(pdfBytes), "%PDF-1.4") {
			t.Fatalf("theme %s ExportPDF failed: err=%v", th, err)
		}

		// DOCX
		docxBytes, err := ExportDocument(doc, notePath, model.ExportDOCX, prefs)
		if err != nil {
			t.Fatalf("theme %s ExportDOCX failed: %v", th, err)
		}
		zr, err := zip.NewReader(bytes.NewReader(docxBytes), int64(len(docxBytes)))
		if err != nil {
			t.Fatalf("theme %s DOCX invalid zip: %v", th, err)
		}
		var docXML string
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			_ = rc.Close()
			if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
				var v any
				if err := xml.Unmarshal(b.Bytes(), &v); err != nil {
					t.Fatalf("theme %s DOCX invalid XML in %s: %v", th, f.Name, err)
				}
			}
			if f.Name == "word/document.xml" {
				docXML = b.String()
			}
		}
		if !strings.Contains(docXML, "自定义标题") || !strings.Contains(strings.ToLower(docXML), "cooool.top") {
			t.Fatalf("theme %s DOCX missing title or cooool.top watermark", th)
		}
	}

	// Verify ShowTitle=false and Watermark=false suppress header & footer.
	fBool := false
	hiddenPrefs := model.ExportPreferences{
		Style:     model.ExportThemeJournal,
		ShowTitle: &fBool,
		Watermark: &fBool,
		Title:     "隐藏标题测试",
	}
	htmlNoMeta, err := ExportDocument(doc, notePath, model.ExportHTML, hiddenPrefs)
	if err != nil {
		t.Fatal(err)
	}
	htmlNoMetaStr := string(htmlNoMeta)
	if strings.Contains(htmlNoMetaStr, `<footer class="doc-footer">`) {
		t.Fatalf("expected watermark footer suppressed when Watermark=false")
	}
	if bodyIdx := strings.Index(htmlNoMetaStr, "<body>"); bodyIdx >= 0 && strings.Contains(htmlNoMetaStr[bodyIdx:], "隐藏标题测试") {
		t.Fatalf("expected custom title suppressed in body when ShowTitle=false")
	}

	// Markdown: ShowTitle=true prepends prefs.Title when not duplicated; ShowTitle=false omits it.
	plainDoc := ParseMarkdown("Only body paragraph.\n")
	tBool := true
	mdWithTitle, err := ExportDocument(plainDoc, notePath, model.ExportMarkdown, model.ExportPreferences{
		ShowTitle: &tBool,
		Title:     "Injected Title",
	})
	if err != nil || !strings.HasPrefix(string(mdWithTitle), "# Injected Title\n\n") {
		t.Fatalf("expected prepended title in Markdown, got %q (err=%v)", string(mdWithTitle), err)
	}
	mdWithoutTitle, err := ExportDocument(plainDoc, notePath, model.ExportMarkdown, model.ExportPreferences{
		ShowTitle: &fBool,
		Title:     "Injected Title",
	})
	if err != nil || strings.Contains(string(mdWithoutTitle), "Injected Title") {
		t.Fatalf("expected no prepended title when ShowTitle=false, got %q", string(mdWithoutTitle))
	}
}

func TestGenericBlockDecoratorQuoteParsing(t *testing.T) {
	doc := ParseMarkdown(strings.Join([]string{
		"> Hello <!-- acme-notes:stamp:ribbon -->",
		"",
		"> [acme:classic] Legacy body <!-- acme-notes:stamp -->",
		"",
		"> Escaped \\<!-- acme-notes:stamp -->",
		"",
		"> Normal quote <!-- plain comment -->",
	}, "\n"))
	if len(doc.Blocks) != 4 {
		t.Fatalf("expected 4 quote blocks, got %d", len(doc.Blocks))
	}
	b0 := doc.Blocks[0]
	if b0.DecoratorPrefix != "acme-notes:stamp" || b0.DecoratorKind != "ribbon" || strings.TrimSpace(b0.PlainText()) != "Hello" {
		t.Fatalf("block 0 mismatch: prefix=%q kind=%q text=%q", b0.DecoratorPrefix, b0.DecoratorKind, b0.PlainText())
	}
	b1 := doc.Blocks[1]
	if b1.DecoratorPrefix != "acme-notes:stamp" || b1.DecoratorKind != "classic" || strings.TrimSpace(b1.PlainText()) != "Legacy body" {
		t.Fatalf("block 1 mismatch: prefix=%q kind=%q text=%q", b1.DecoratorPrefix, b1.DecoratorKind, b1.PlainText())
	}
	b2 := doc.Blocks[2]
	if b2.DecoratorPrefix != "" || b2.DecoratorKind != "" {
		t.Fatalf("escaped comment should not match decorator: prefix=%q kind=%q", b2.DecoratorPrefix, b2.DecoratorKind)
	}
	b3 := doc.Blocks[3]
	if b3.DecoratorPrefix != "" || b3.DecoratorKind != "" {
		t.Fatalf("plain comment should not match decorator: prefix=%q kind=%q", b3.DecoratorPrefix, b3.DecoratorKind)
	}
}
