package markdown

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/codepreview"
	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
)

const (
	defaultExportWidth = 816
	// ExportScale is the fixed raster density of PNG/PDF exports (device px per logical px).
	ExportScale        = 4.0
	exportScale        = ExportScale
	maxPDFPageHeightPt = 1152
	watermarkLabel     = "cooool.top"
)

func parseExportThemeName(raw string) (model.ExportTheme, bool) {
	v := model.ExportTheme(strings.ToLower(strings.TrimSpace(raw)))
	switch v {
	case model.ExportThemeMono,
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
		model.ExportThemeTerminal:
		return v, true
	default:
		return "", false
	}
}

func resolveExportPaper(prefs model.ExportPreferences) (PaperThemeSpec, bool) {
	if prefs.Style != "" {
		return PaperTheme(prefs.Style), false
	}
	if parsed, ok := parseExportThemeName(prefs.Theme); ok {
		return PaperTheme(parsed), false
	}
	switch strings.ToLower(strings.TrimSpace(prefs.Theme)) {
	case "light":
		return legacyOceanPaperSpec(theme.OceanLight), true
	case "dark":
		return legacyOceanPaperSpec(theme.OceanDark), true
	case "current":
		return legacyOceanPaperSpec(theme.Ocean()), true
	default:
		return PaperTheme(model.ExportThemeMono), false
	}
}

func resolveShowTitle(prefs model.ExportPreferences) bool {
	if prefs.ShowTitle != nil {
		return *prefs.ShowTitle
	}
	return true
}

func resolveWatermark(prefs model.ExportPreferences) bool {
	if prefs.Watermark != nil {
		return *prefs.Watermark
	}
	return true
}

func resolveTitleText(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	if t := strings.TrimSpace(prefs.Title); t != "" {
		return t
	}
	return resolveDocTitle(doc, notePath)
}

func exportBodyBlocks(blocks []model.Block, title string, showTitle bool) []model.Block {
	if len(blocks) > 0 && blocks[0].Kind == model.BlockCode && blocks[0].Language == "yaml-frontmatter" {
		blocks = blocks[1:]
	}
	if !showTitle || len(blocks) == 0 {
		return blocks
	}
	trimmedTitle := strings.Join(strings.Fields(title), " ")
	if trimmedTitle == "" {
		return blocks
	}
	first := &blocks[0]
	if first.Kind == model.BlockHeading1 {
		firstText := strings.Join(strings.Fields(first.PlainText()), " ")
		if firstText == trimmedTitle {
			return blocks[1:]
		}
	}
	return blocks
}

// projectExportPreviews gives styled exports the same code preview content as
// the editor. Each row stays editable in HTML and Word.
func projectExportPreviews(blocks []model.Block) []model.Block {
	projected := make([]model.Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != model.BlockCode {
			projected = append(projected, b)
			continue
		}
		var rows [][]string
		var aligns []model.ColAlign
		lang := codepreview.CanonicalLanguage(b.Language)
		switch lang {
		case "csv":
			rows = codepreview.ParseCSV(b.Code)
		case "latex":
			if formula, errMsg := codepreview.FormatMath(b.Code); errMsg == "" {
				rows = [][]string{{formula}}
				aligns = []model.ColAlign{model.ColAlignCenter}
			}
		case "mermaid":
			nodes, labels, edges, _ := codepreview.ParseMermaid(b.Code)
			for _, edge := range edges {
				arrow := "→"
				if edge.Label != "" {
					arrow = "— " + edge.Label + " →"
				}
				rows = append(rows, []string{labels[edge.From], arrow, labels[edge.To]})
			}
			if len(edges) == 0 {
				for _, node := range nodes {
					rows = append(rows, []string{labels[node]})
				}
			}
			aligns = []model.ColAlign{model.ColAlignCenter}
			if len(edges) > 0 {
				aligns = append(aligns, model.ColAlignCenter, model.ColAlignCenter)
			}
		}
		if len(rows) == 0 {
			projected = append(projected, b)
			continue
		}
		table := &model.TableData{NoHeaderRow: lang != "csv", Aligns: aligns}
		for _, row := range rows {
			cells := make([]model.TableCell, 0, len(row))
			for _, cell := range row {
				cells = append(cells, model.TableCell{Spans: []model.Span{{Text: cell}}})
			}
			table.Rows = append(table.Rows, cells)
		}
		projected = append(projected, model.Block{
			Kind: model.BlockTable, Table: table, Indent: b.Indent, QuoteDepth: b.QuoteDepth,
		})
		if b.ShowSource {
			projected = append(projected, b)
		}
	}
	return projected
}

// ExportDocument renders doc into the requested format bytes.
func ExportDocument(doc *model.Doc, notePath string, format model.ExportFormat, prefs model.ExportPreferences) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("cannot export nil document")
	}
	if format == "" {
		format = prefs.Format
	}
	if format == "" {
		format = model.ExportMarkdown
	}
	switch format {
	case model.ExportMarkdown:
		return []byte(exportMarkdown(doc, notePath, prefs)), nil
	case model.ExportHTML:
		return []byte(exportHTML(doc, notePath, prefs)), nil
	case model.ExportPNG:
		return exportPNG(doc, notePath, prefs)
	case model.ExportPDF:
		return exportPDF(doc, notePath, prefs)
	case model.ExportDOCX:
		return exportDOCX(doc, notePath, prefs)
	case model.ExportZIP:
		return exportZIP(doc, notePath, prefs)
	default:
		return nil, fmt.Errorf("unsupported export format: %q", format)
	}
}

// ExportToFile exports doc and atomically writes the resulting bytes to outPath.
func ExportToFile(doc *model.Doc, notePath, outPath string, format model.ExportFormat, prefs model.ExportPreferences) error {
	if strings.TrimSpace(outPath) == "" {
		return errors.New("output path cannot be empty")
	}
	data, err := ExportDocument(doc, notePath, format, prefs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(outPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".block-export-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func resolveExportWidth(prefs model.ExportPreferences) int {
	if prefs.Width >= 360 && prefs.Width <= 2400 {
		return prefs.Width
	}
	return defaultExportWidth
}

func resolveDocTitle(doc *model.Doc, notePath string) string {
	if t := strings.TrimSpace(doc.Title()); t != "" {
		return t
	}
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" {
			return stem
		}
	}
	return theme.Tr("未命名", "Untitled")
}

func uiColorCSS(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float32(c.A)/255.0)
}

func mixUIColor(a, b ui.Color, t float32) ui.Color {
	lerp := func(x, y uint8) uint8 {
		v := float32(x) + (float32(y)-float32(x))*t
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	return ui.RGB(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B))
}

func resolveSpanColor(raw string, bg bool, pal PaperThemeSpec) (ui.Color, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || v == "default" || v == "none" || v == "transparent" {
		return ui.Transparent, false
	}
	if bg {
		if c, ok := theme.ResolveNamedBG(v, pal.Dark); ok {
			return c, true
		}
	} else {
		if c, ok := theme.ResolveNamedFG(v, pal.Dark); ok {
			return c, true
		}
	}
	if v == "black" {
		c := ui.RGB(0, 0, 0)
		if !bg {
			c = pal.FG(c)
		} else {
			c = pal.BG(c)
		}
		return c, true
	}
	if v == "white" {
		c := ui.RGB(255, 255, 255)
		if bg {
			c = pal.BG(c)
		}
		return c, true
	}
	var parsed ui.Color
	var ok bool
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 {
			r, e1 := strconv.ParseUint(h[0:1]+h[0:1], 16, 8)
			g, e2 := strconv.ParseUint(h[1:2]+h[1:2], 16, 8)
			b, e3 := strconv.ParseUint(h[2:3]+h[2:3], 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				parsed, ok = ui.RGB(uint8(r), uint8(g), uint8(b)), true
			}
		} else if len(h) == 6 || len(h) == 8 {
			r, e1 := strconv.ParseUint(h[0:2], 16, 8)
			g, e2 := strconv.ParseUint(h[2:4], 16, 8)
			b, e3 := strconv.ParseUint(h[4:6], 16, 8)
			a := uint64(255)
			var e4 error
			if len(h) == 8 {
				a, e4 = strconv.ParseUint(h[6:8], 16, 8)
			}
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				parsed, ok = ui.Color{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, true
			}
		}
	} else if strings.HasPrefix(v, "rgb(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[4:len(v)-1], ",")
		if len(parts) == 3 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			if e1 == nil && e2 == nil && e3 == nil {
				parsed, ok = ui.RGB(clampU8(r), clampU8(g), clampU8(b)), true
			}
		}
	} else if strings.HasPrefix(v, "rgba(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(v[5:len(v)-1], ",")
		if len(parts) == 4 {
			r, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			a, e4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				parsed, ok = ui.Color{R: clampU8(r), G: clampU8(g), B: clampU8(b), A: clampU8(int(a*255 + 0.5))}, true
			}
		}
	}
	if !ok {
		return ui.Transparent, false
	}
	if bg {
		return pal.BG(parsed), true
	}
	return pal.FG(parsed), true
}

func clampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// exportImageCrop normalizes saved crop fractions for the dimensions being exported.
func exportImageCrop(c model.ImageCrop, width, height float32) model.ImageCrop {
	c.Left, c.Right = max(c.Left, 0), max(c.Right, 0)
	c.Top, c.Bottom = max(c.Top, 0), max(c.Bottom, 0)
	if c.Left+c.Right >= 1 || (1-c.Left-c.Right)*width <= 1 {
		c.Left, c.Right = 0, 0
	}
	if c.Top+c.Bottom >= 1 || (1-c.Top-c.Bottom)*height <= 1 {
		c.Top, c.Bottom = 0, 0
	}
	return c
}

// ---- Markdown Export ----

func exportMarkdown(doc *model.Doc, notePath string, prefs model.ExportPreferences) string {
	md := SerializeMarkdown(doc)
	showTitle := resolveShowTitle(prefs)
	title := strings.TrimSpace(prefs.Title)
	if title == "" && (prefs.IncludeMeta || (prefs.ShowTitle != nil && *prefs.ShowTitle)) {
		title = resolveDocTitle(doc, notePath)
	}
	if showTitle && title != "" {
		hasFM := len(doc.Blocks) > 0 && doc.Blocks[0].Kind == model.BlockCode && doc.Blocks[0].Language == "yaml-frontmatter"
		duplicated := false
		for i := range doc.Blocks {
			b := &doc.Blocks[i]
			if strings.TrimSpace(b.PlainText()) != "" || b.Table != nil {
				duplicated = b.Kind == model.BlockHeading1 && strings.TrimSpace(b.PlainText()) == title
				break
			}
		}
		if !hasFM && !duplicated {
			return "# " + title + "\n\n" + md
		}
	}
	return md
}

// ---- ZIP Export (Markdown + Local Referenced Images) ----

func exportZIP(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	cloned := doc.Clone()
	usedNames := make(map[string]bool)
	pathToZip := make(map[string]string)
	budget := maxExportImageBytes
	attempts := 0
	dir := ""
	if notePath != "" {
		dir = filepath.Dir(notePath)
	}

	type zipAsset struct {
		zipPath string
		data    []byte
	}
	var assets []zipAsset

	for i := range cloned.Blocks {
		b := &cloned.Blocks[i]
		if b.Kind != model.BlockImage {
			continue
		}
		rel, err := media.RelativePath(dir, b.ImageSrc)
		zipPath, seen := pathToZip[rel]
		if err == nil && !seen && budget > 0 && attempts < maxExportImageRefs {
			attempts++
			pathToZip[rel] = ""
			if entry, ok := loadExportImage(notePath, b.ImageSrc, budget); ok {
				base := filepath.Base(rel)
				ext := "." + entry.format
				if entry.format == "jpeg" {
					ext = ".jpg"
				}
				stem := safeArchiveName(strings.TrimSuffix(base, filepath.Ext(base)), "image")
				name := stem + ext
				for n := 1; usedNames[strings.ToLower(name)]; n++ {
					name = fmt.Sprintf("%s-%d%s", stem, n, ext)
				}
				usedNames[strings.ToLower(name)] = true
				zipPath = "assets/" + name
				pathToZip[rel] = zipPath
				budget -= len(entry.raw)
				assets = append(assets, zipAsset{zipPath: zipPath, data: entry.raw})
			}
		}
		b.Dirty = true
		b.CleanSource = ""
		if zipPath != "" {
			b.ImageSrc = zipPath
		} else {
			b.Kind = model.BlockParagraph
			b.Spans = []model.Span{{Text: b.ImageAlt}}
		}
	}

	mdContent := exportMarkdown(cloned, notePath, prefs)
	mdName := ""
	if notePath != "" {
		base := filepath.Base(notePath)
		if strings.EqualFold(filepath.Ext(base), ".md") {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
		mdName = safeArchiveName(base, "note")
	} else {
		mdName = safeArchiveName(doc.Title(), "note")
	}
	mdName += ".md"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mw, err := zw.Create(mdName)
	if err != nil {
		return nil, err
	}
	if _, err := mw.Write([]byte(mdContent)); err != nil {
		return nil, err
	}
	for _, a := range assets {
		aw, err := zw.Create(a.zipPath)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeArchiveName(raw, fallback string) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:%*?"<>|#&[]()`, r) || unicode.IsControl(r) {
			return '-'
		}
		return r
	}, raw)
	// Truncate at a rune boundary and reserve space for a suffix and extension.
	for i := range name {
		if i > 180 {
			name = name[:i]
			break
		}
	}
	name = strings.Trim(name, " .")
	if name == "" {
		return fallback
	}
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return "_" + name
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return "_" + name
	}
	return name
}

func isEmptyParagraphBlock(b *model.Block) bool {
	return b.Kind == model.BlockParagraph && len(b.Spans) == 0
}

func trimEmptyEdgeBlocks(blocks []model.Block) []model.Block {
	for len(blocks) > 0 && isEmptyParagraphBlock(&blocks[0]) {
		blocks = blocks[1:]
	}
	for len(blocks) > 0 && isEmptyParagraphBlock(&blocks[len(blocks)-1]) {
		blocks = blocks[:len(blocks)-1]
	}
	return blocks
}

// isLeadParagraph reports whether blocks[idx] is a short single-line paragraph
// that acts as a group label immediately above a list.
func isLeadParagraph(blocks []model.Block, idx int) bool {
	if idx < 0 || idx+1 >= len(blocks) {
		return false
	}
	b := &blocks[idx]
	next := &blocks[idx+1]
	if b.Kind != model.BlockParagraph || b.Indent != 0 || !next.Kind.IsList() {
		return false
	}
	if b.QuoteDepth != next.QuoteDepth {
		return false
	}
	txt := strings.TrimSpace(b.PlainText())
	if txt == "" || strings.ContainsRune(txt, '\n') {
		return false
	}
	return utf8.RuneCountInString(txt) <= 42
}

// partitionSectionCards groups bodyBlocks into floating cards for SectionCards themes (Mist).
// Priority: H2 headings -> H1/H3 headings -> dividers -> short lead+list groups -> single card.
func partitionSectionCards(blocks []model.Block, showTitle bool) (intro []model.Block, runs [][]model.Block) {
	splitByKind := func(kind model.BlockKind) (bool, []model.Block, [][]model.Block) {
		first := -1
		for i := range blocks {
			if blocks[i].Kind == kind {
				first = i
				break
			}
		}
		if first < 0 {
			return false, nil, nil
		}
		in := trimEmptyEdgeBlocks(blocks[:first])
		var out [][]model.Block
		idx := first
		for idx < len(blocks) {
			start := idx
			idx++
			for idx < len(blocks) && blocks[idx].Kind != kind {
				idx++
			}
			if chunk := trimEmptyEdgeBlocks(blocks[start:idx]); len(chunk) > 0 {
				out = append(out, chunk)
			}
		}
		return true, in, out
	}

	if ok, in, out := splitByKind(model.BlockHeading2); ok {
		return in, out
	}
	if ok, in, out := splitByKind(model.BlockHeading1); ok {
		return in, out
	}
	if ok, in, out := splitByKind(model.BlockHeading3); ok {
		return in, out
	}

	hasDiv := false
	for i := range blocks {
		if blocks[i].Kind == model.BlockDivider {
			hasDiv = true
			break
		}
	}
	if hasDiv {
		var chunks [][]model.Block
		start := 0
		for i := range blocks {
			if blocks[i].Kind == model.BlockDivider {
				if chunk := trimEmptyEdgeBlocks(blocks[start:i]); len(chunk) > 0 {
					chunks = append(chunks, chunk)
				}
				start = i + 1
			}
		}
		if chunk := trimEmptyEdgeBlocks(blocks[start:]); len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}
		if len(chunks) > 0 {
			if showTitle {
				return nil, chunks
			}
			return chunks[0], chunks[1:]
		}
	}

	leadCount := 0
	firstLead := -1
	for i := range blocks {
		if isLeadParagraph(blocks, i) {
			if firstLead < 0 {
				firstLead = i
			}
			leadCount++
		}
	}
	if leadCount >= 2 || (leadCount == 1 && (showTitle || len(trimEmptyEdgeBlocks(blocks[:firstLead])) > 0)) {
		in := trimEmptyEdgeBlocks(blocks[:firstLead])
		var out [][]model.Block
		idx := firstLead
		for idx < len(blocks) {
			start := idx
			idx++
			for idx < len(blocks) && !isLeadParagraph(blocks, idx) {
				idx++
			}
			if chunk := trimEmptyEdgeBlocks(blocks[start:idx]); len(chunk) > 0 {
				out = append(out, chunk)
			}
		}
		return in, out
	}

	return trimEmptyEdgeBlocks(blocks), nil
}

func effectivePadTop(pal PaperThemeSpec, legacy, showTitle bool) float32 {
	if showTitle || legacy {
		return pal.PadTop
	}
	if pal.Card != nil || pal.Style.Sections != nil {
		return minFloat32(pal.PadTop, 44)
	}
	return minFloat32(pal.PadTop, 56)
}
