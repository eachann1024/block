package markdown

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	watermarkLabel     = "Block"
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

func resolveLocalImagePath(src, notePath string) (string, bool) {
	s := strings.TrimSpace(src)
	if s == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		if u, err := url.Parse(s); err == nil && u.Path != "" {
			s = u.Path
		} else {
			s = strings.TrimPrefix(s[7:], "localhost")
		}
	} else if strings.Contains(s, "://") {
		return "", false
	}
	if decoded, err := url.PathUnescape(s); err == nil {
		s = decoded
	}
	p := filepath.FromSlash(s)
	if !filepath.IsAbs(p) {
		if notePath == "" {
			return "", false
		}
		p = filepath.Join(filepath.Dir(notePath), p)
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", false
	}
	return p, true
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
	usedNames := make(map[string]int)
	pathToZip := make(map[string]string)

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
		localPath, ok := resolveLocalImagePath(b.ImageSrc, notePath)
		if !ok {
			continue
		}
		zipPath, seen := pathToZip[localPath]
		if !seen {
			raw, err := os.ReadFile(localPath)
			if err != nil {
				continue
			}
			base := filepath.Base(localPath)
			if base == "" || base == "." || base == string(filepath.Separator) {
				base = "image.png"
			}
			count := usedNames[base]
			usedNames[base] = count + 1
			name := base
			if count > 0 {
				ext := filepath.Ext(base)
				stem := strings.TrimSuffix(base, ext)
				name = fmt.Sprintf("%s-%d%s", stem, count, ext)
			}
			zipPath = "assets/" + name
			pathToZip[localPath] = zipPath
			assets = append(assets, zipAsset{zipPath: zipPath, data: raw})
		}
		if b.ImageSrc != zipPath {
			b.ImageSrc = zipPath
			b.Dirty = true
			b.CleanSource = ""
		}
	}

	mdContent := exportMarkdown(cloned, notePath, prefs)
	mdName := "note.md"
	if base := strings.TrimSpace(filepath.Base(notePath)); base != "" && base != "." && base != string(filepath.Separator) {
		if !strings.HasSuffix(strings.ToLower(base), ".md") {
			base += ".md"
		}
		mdName = base
	} else if t := strings.TrimSpace(doc.Title()); t != "" {
		safe := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
				return '-'
			}
			return r
		}, t)
		if safe != "" {
			mdName = safe + ".md"
		}
	}

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
