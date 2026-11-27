package markdown

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"block/assets"
	"block/internal/files"
	"block/internal/model"

	tdfont "github.com/tdewolff/font"
)

const (
	maxHTMLEmbeddedFontBytes = 4 << 20 // 4 MiB total raw WOFF2 budget per standalone HTML
	maxDOCXEmbeddedFontBytes = 6 << 20 // 6 MiB total embedded .odttf budget per DOCX
	htmlFullRuneWeightLimit  = 600     // use full rune set for non-regular weights on typical notes
)

type exportFontFace struct {
	family string
	weight int
	italic bool
	data   []byte
}

var builtinJetBrainsFaces = []struct {
	path   string
	weight int
	italic bool
}{
	{"fonts/JetBrainsMono-Regular.ttf", 400, false},
	{"fonts/JetBrainsMono-Medium.ttf", 500, false},
	{"fonts/JetBrainsMono-Bold.ttf", 700, false},
	{"fonts/JetBrainsMono-Italic.ttf", 400, true},
	{"fonts/JetBrainsMono-BoldItalic.ttf", 700, true},
}

// loadExportFontFamilyFaces returns all locally available static TrueType faces
// for a managed family (bundled JetBrains Mono or cached webfonts).
func loadExportFontFamilyFaces(dataDir, family string) []exportFontFace {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "jetbrains mono":
		out := make([]exportFontFace, 0, len(builtinJetBrainsFaces))
		for _, spec := range builtinJetBrainsFaces {
			raw, err := assets.Read(spec.path)
			if err != nil || len(raw) == 0 {
				continue
			}
			out = append(out, exportFontFace{
				family: "JetBrains Mono",
				weight: spec.weight,
				italic: spec.italic,
				data:   raw,
			})
		}
		return out
	case "harmonyos sans sc", "lxgw wenkai", "noto serif sc":
		cached := files.ReadCachedWebFontFaces(dataDir, family)
		if len(cached) == 0 {
			return nil
		}
		out := make([]exportFontFace, 0, len(cached))
		for _, f := range cached {
			out = append(out, exportFontFace{
				family: f.Family,
				weight: f.Weight,
				italic: f.Italic,
				data:   f.Data,
			})
		}
		return out
	default:
		return nil
	}
}

type sfntNameMeta struct {
	family             string
	subfamily          string
	weight             int
	italic             bool
	normalizeWordStyle bool
}

func parseSFNTForSubset(rawTTF []byte) (*tdfont.SFNT, error) {
	sf, err := tdfont.ParseSFNT(rawTTF, 0)
	if err == nil {
		return sf, nil
	}
	// Some CJK TrueType fonts (e.g. LXGW WenKai) ship a post v2 table with
	// non-standard glyph name lengths; subsetting emits post v3 anyway, so
	// normalizing the post header to v3 (32 bytes) safely unblocks parsing.
	if patched, ok := normalizeSFNTPostToV3(rawTTF); ok {
		return tdfont.ParseSFNT(patched, 0)
	}
	return nil, err
}

func normalizeSFNTPostToV3(raw []byte) ([]byte, bool) {
	if len(raw) < 12 {
		return nil, false
	}
	numTables := int(binary.BigEndian.Uint16(raw[4:6]))
	if len(raw) < 12+numTables*16 {
		return nil, false
	}
	for i := 0; i < numTables; i++ {
		recOff := 12 + i*16
		if string(raw[recOff:recOff+4]) != "post" {
			continue
		}
		tblOff := int(binary.BigEndian.Uint32(raw[recOff+8 : recOff+12]))
		tblLen := int(binary.BigEndian.Uint32(raw[recOff+12 : recOff+16]))
		if tblLen <= 32 || tblOff < 12 || tblOff+32 > len(raw) {
			return nil, false
		}
		cp := append([]byte(nil), raw...)
		binary.BigEndian.PutUint32(cp[tblOff:tblOff+4], 0x00030000)
		var sum uint32
		for p := tblOff; p < tblOff+32; p += 4 {
			sum += binary.BigEndian.Uint32(cp[p : p+4])
		}
		binary.BigEndian.PutUint32(cp[recOff+4:recOff+8], sum)
		binary.BigEndian.PutUint32(cp[recOff+12:recOff+16], 32)
		return cp, true
	}
	return nil, false
}

// subsetTrueTypeFont trims rawTTF to only glyphs required by runes, rebuilds a
// valid SFNT name table (and optional Word 4-slot style flags), and returns
// checksum-valid TrueType bytes plus the OS/2 PANOSE bytes when present.
func subsetTrueTypeFont(rawTTF []byte, runes map[rune]struct{}, meta sfntNameMeta) (ttf []byte, panose [10]byte, hasPanose bool, err error) {
	if len(rawTTF) < 12 || len(runes) == 0 {
		return nil, panose, false, nil
	}
	sf, err := parseSFNTForSubset(rawTTF)
	if err != nil || !sf.IsTrueType {
		return nil, panose, false, err
	}

	glyphSet := make(map[uint16]struct{}, len(runes)+4)
	glyphSet[0] = struct{}{} // .notdef always at GID 0
	matchedNonSpace := 0
	for r := range runes {
		gid := sf.GlyphIndex(r)
		if gid == 0 {
			continue
		}
		glyphSet[gid] = struct{}{}
		if !unicode.IsSpace(r) {
			matchedNonSpace++
		}
	}
	if matchedNonSpace == 0 {
		return nil, panose, false, nil
	}
	// Ensure common spaces are always present in the subset when supported.
	for _, sp := range []rune{' ', '\u00A0'} {
		if gid := sf.GlyphIndex(sp); gid != 0 {
			glyphSet[gid] = struct{}{}
		}
	}

	glyphIDs := make([]uint16, 0, len(glyphSet))
	glyphIDs = append(glyphIDs, 0)
	for gid := range glyphSet {
		if gid != 0 {
			glyphIDs = append(glyphIDs, gid)
		}
	}
	sort.Slice(glyphIDs[1:], func(i, j int) bool { return glyphIDs[1+i] < glyphIDs[1+j] })

	// Preserve essential TrueType tables plus hinting tables when available; omit
	// DSIG because subsetting invalidates any original digital signature.
	tables := []string{"cmap", "glyf", "head", "hhea", "hmtx", "loca", "maxp", "name", "OS/2", "post"}
	for _, opt := range []string{"cvt ", "fpgm", "prep"} {
		if _, ok := sf.Tables[opt]; ok {
			tables = append(tables, opt)
		}
	}

	sub, err := sf.Subset(glyphIDs, tdfont.SubsetOptions{Tables: tables})
	if err != nil {
		return nil, panose, false, err
	}

	// Rebuild a spec-compliant SFNT name table (tdewolff/font writes an empty one).
	sub.Tables["name"] = buildSFNTNameTable(meta.family, meta.subfamily)

	if os2, ok := sub.Tables["OS/2"]; ok && len(os2) >= 42 {
		copy(panose[:], os2[32:42])
		hasPanose = true
		if meta.normalizeWordStyle && len(os2) >= 64 {
			cp := append([]byte(nil), os2...)
			wClass := uint16(400)
			if meta.weight >= 600 {
				wClass = 700
			}
			binary.BigEndian.PutUint16(cp[4:6], wClass)
			fsSel := binary.BigEndian.Uint16(cp[62:64])
			fsSel &^= 0x0001 | 0x0020 | 0x0040 // clear ITALIC | BOLD | REGULAR
			switch {
			case meta.weight >= 600 && meta.italic:
				fsSel |= 0x0021
			case meta.weight >= 600:
				fsSel |= 0x0020
			case meta.italic:
				fsSel |= 0x0001
			default:
				fsSel |= 0x0040
			}
			binary.BigEndian.PutUint16(cp[62:64], fsSel)
			sub.Tables["OS/2"] = cp
		}
	}

	if meta.normalizeWordStyle {
		if head, ok := sub.Tables["head"]; ok && len(head) >= 46 {
			cp := append([]byte(nil), head...)
			macStyle := binary.BigEndian.Uint16(cp[44:46])
			macStyle &^= 0x0003 // clear Bold (bit 0) | Italic (bit 1)
			if meta.weight >= 600 {
				macStyle |= 0x0001
			}
			if meta.italic {
				macStyle |= 0x0002
			}
			binary.BigEndian.PutUint16(cp[44:46], macStyle)
			sub.Tables["head"] = cp
		}
	}

	return sub.Write(), panose, hasPanose, nil
}

// encodeWOFF2FromTTF re-parses a serialized subset TTF so SFNT.Length and table
// metadata are 100% consistent before WOFF2 compression (required by browser OTS).
func encodeWOFF2FromTTF(ttf []byte) ([]byte, error) {
	parsed, err := tdfont.ParseSFNT(ttf, 0)
	if err != nil {
		return nil, err
	}
	return parsed.WriteWOFF2()
}

type sfntNameRec struct {
	platformID uint16
	encodingID uint16
	languageID uint16
	nameID     uint16
	value      []byte
}

func encodeUTF16BE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.BigEndian.PutUint16(out[i*2:], u)
	}
	return out
}

func sanitizePostScriptName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "BlockSubsetFont"
	}
	if b.Len() > 63 {
		return b.String()[:63]
	}
	return b.String()
}

func buildSFNTNameTable(family, subfamily string) []byte {
	family = strings.TrimSpace(family)
	if family == "" {
		family = "Block Font"
	}
	subfamily = strings.TrimSpace(subfamily)
	if subfamily == "" {
		subfamily = "Regular"
	}
	fullName := family
	if !strings.EqualFold(subfamily, "Regular") {
		fullName = family + " " + subfamily
	}
	psName := sanitizePostScriptName(family + "-" + subfamily)
	uniqueID := "BlockSubset-" + psName
	entries := []struct {
		id  uint16
		val string
	}{
		{1, family},
		{2, subfamily},
		{3, uniqueID},
		{4, fullName},
		{5, "Version 1.0"},
		{6, psName},
		{16, family},
		{17, subfamily},
	}

	recs := make([]sfntNameRec, 0, len(entries)*2)
	// Platform 1 (Macintosh Roman, English) must sort before Platform 3 (Windows Unicode BMP).
	for _, e := range entries {
		recs = append(recs, sfntNameRec{
			platformID: 1,
			encodingID: 0,
			languageID: 0,
			nameID:     e.id,
			value:      []byte(e.val),
		})
	}
	for _, e := range entries {
		recs = append(recs, sfntNameRec{
			platformID: 3,
			encodingID: 1,
			languageID: 0x0409,
			nameID:     e.id,
			value:      encodeUTF16BE(e.val),
		})
	}

	sort.Slice(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if a.platformID != b.platformID {
			return a.platformID < b.platformID
		}
		if a.encodingID != b.encodingID {
			return a.encodingID < b.encodingID
		}
		if a.languageID != b.languageID {
			return a.languageID < b.languageID
		}
		return a.nameID < b.nameID
	})

	headerSize := 6 + len(recs)*12
	var strBuf bytes.Buffer
	var out bytes.Buffer
	out.Grow(headerSize + 256)
	var hdr [6]byte
	binary.BigEndian.PutUint16(hdr[0:2], 0)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(recs)))
	binary.BigEndian.PutUint16(hdr[4:6], uint16(headerSize))
	out.Write(hdr[:])

	var recBuf [12]byte
	for _, r := range recs {
		off := strBuf.Len()
		strBuf.Write(r.value)
		binary.BigEndian.PutUint16(recBuf[0:2], r.platformID)
		binary.BigEndian.PutUint16(recBuf[2:4], r.encodingID)
		binary.BigEndian.PutUint16(recBuf[4:6], r.languageID)
		binary.BigEndian.PutUint16(recBuf[6:8], r.nameID)
		binary.BigEndian.PutUint16(recBuf[8:10], uint16(len(r.value)))
		binary.BigEndian.PutUint16(recBuf[10:12], uint16(off))
		out.Write(recBuf[:])
	}
	out.Write(strBuf.Bytes())
	return out.Bytes()
}

// obfuscateODTTF applies ECMA-376 / ISO 29500-2 §14.2.2 font obfuscation:
// XORs the first 32 bytes twice with the byte-reversed 16-byte GUID key.
func obfuscateODTTF(ttf []byte, salt string) (odttf []byte, fontKeyGUID string) {
	h := sha256.New()
	h.Write([]byte(salt))
	h.Write(ttf)
	sum := h.Sum(nil)
	var key [16]byte
	copy(key[:], sum[:16])
	// Mark as RFC 4122 v4 GUID for broad Word/LibreOffice/WPS compatibility.
	key[6] = (key[6] & 0x0f) | 0x40
	key[8] = (key[8] & 0x3f) | 0x80

	fontKeyGUID = fmt.Sprintf("{%08X-%04X-%04X-%04X-%012X}",
		binary.BigEndian.Uint32(key[0:4]),
		binary.BigEndian.Uint16(key[4:6]),
		binary.BigEndian.Uint16(key[6:8]),
		binary.BigEndian.Uint16(key[8:10]),
		key[10:16],
	)

	odttf = append([]byte(nil), ttf...)
	n := min(len(odttf), 32)
	for i := 0; i < n; i++ {
		odttf[i] ^= key[15-(i%16)]
	}
	return odttf, fontKeyGUID
}

// ---- HTML Standalone WOFF2 Subsetting ----

type htmlTextBuckets struct {
	all   map[rune]struct{}
	bold  map[rune]struct{}
	ascii map[rune]struct{}
}

func extractHTMLTextBuckets(htmlBody string) htmlTextBuckets {
	buckets := htmlTextBuckets{
		all:   make(map[rune]struct{}, 256),
		bold:  make(map[rune]struct{}, 128),
		ascii: make(map[rune]struct{}, 96),
	}
	// Track nested element flags so bold/heading text can be bucketed for very long docs.
	type tagFrame struct {
		name string
		skip bool
		bold bool
	}
	var stack []tagFrame
	inSkip := func() bool {
		for _, f := range stack {
			if f.skip {
				return true
			}
		}
		return false
	}
	inBold := func() bool {
		for _, f := range stack {
			if f.bold {
				return true
			}
		}
		return false
	}

	i := 0
	n := len(htmlBody)
	for i < n {
		if htmlBody[i] == '<' {
			end := strings.IndexByte(htmlBody[i:], '>')
			if end < 0 {
				break
			}
			tagRaw := strings.TrimSpace(htmlBody[i+1 : i+end])
			i += end + 1
			if tagRaw == "" || strings.HasPrefix(tagRaw, "!") || strings.HasPrefix(tagRaw, "?") {
				continue
			}
			if strings.HasPrefix(tagRaw, "/") {
				closeName := strings.ToLower(strings.TrimSpace(tagRaw[1:]))
				for idx := len(stack) - 1; idx >= 0; idx-- {
					if stack[idx].name == closeName {
						stack = stack[:idx]
						break
					}
				}
				continue
			}
			selfClosing := strings.HasSuffix(tagRaw, "/")
			if selfClosing {
				tagRaw = strings.TrimSpace(strings.TrimSuffix(tagRaw, "/"))
			}
			spIdx := strings.IndexAny(tagRaw, " \t\r\n")
			name := tagRaw
			attrs := ""
			if spIdx >= 0 {
				name = tagRaw[:spIdx]
				attrs = tagRaw[spIdx+1:]
			}
			name = strings.ToLower(strings.TrimSpace(name))
			if selfClosing || name == "br" || name == "hr" || name == "img" || name == "meta" || name == "input" || name == "path" {
				continue
			}
			skip := name == "style" || name == "script" || name == "svg"
			bold := name == "h1" || name == "h2" || name == "h3" || name == "h4" || name == "h5" || name == "h6" ||
				name == "strong" || name == "b" || name == "th" || name == "blockquote" ||
				strings.Contains(attrs, "doc-title") ||
				strings.Contains(attrs, "callout-title") ||
				strings.Contains(attrs, "font-weight:5") ||
				strings.Contains(attrs, "font-weight:6") ||
				strings.Contains(attrs, "font-weight:7") ||
				strings.Contains(attrs, "font-weight:8") ||
				strings.Contains(attrs, "font-weight:9")
			stack = append(stack, tagFrame{name: name, skip: skip, bold: bold})
			continue
		}

		nextTag := strings.IndexByte(htmlBody[i:], '<')
		var chunk string
		if nextTag < 0 {
			chunk = htmlBody[i:]
			i = n
		} else {
			chunk = htmlBody[i : i+nextTag]
			i += nextTag
		}
		if inSkip() || chunk == "" {
			continue
		}
		text := html.UnescapeString(chunk)
		isB := inBold()
		for _, r := range text {
			if r == '\r' || r == '\n' || r == '\t' {
				continue
			}
			buckets.all[r] = struct{}{}
			if isB {
				buckets.bold[r] = struct{}{}
			}
			if r < 0x80 || (r >= 0x2000 && r <= 0x2BFF) {
				buckets.ascii[r] = struct{}{}
			}
		}
	}
	return buckets
}

func htmlThemeManagedFamilies(pal PaperThemeSpec, legacy bool, htmlBody string) []string {
	hasCode := strings.Contains(htmlBody, "<code") || strings.Contains(htmlBody, "<pre")
	if legacy {
		if hasCode {
			return []string{"HarmonyOS Sans SC", "JetBrains Mono"}
		}
		return []string{"HarmonyOS Sans SC"}
	}
	out := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	add := func(name string) {
		k := strings.ToLower(name)
		if !seen[k] {
			seen[k] = true
			out = append(out, name)
		}
	}
	t := NormalizeExportTheme(pal.Theme)
	switch t {
	case model.ExportThemeJournal, model.ExportThemeKraft:
		add("LXGW WenKai")
		if hasCode {
			add("HarmonyOS Sans SC")
		}
	case model.ExportThemeInk:
		add("Noto Serif SC")
		if hasCode {
			add("HarmonyOS Sans SC")
		}
	default:
		for _, fam := range ThemeWebFontFamilies(t) {
			add(fam)
		}
	}
	// Terminal, Riso, Poster, Neon, Film use JetBrains Mono in ornaments/headers/footers;
	// other themes only need it when code/math spans appear in the body.
	usesMonoChrome := t == model.ExportThemeTerminal ||
		t == model.ExportThemeRiso ||
		t == model.ExportThemePoster ||
		t == model.ExportThemeNeon ||
		t == model.ExportThemeFilm
	if hasCode || usesMonoChrome {
		add("JetBrains Mono")
	}
	return out
}

type htmlFontFaceTask struct {
	face        exportFontFace
	weightRange string
	priority    int // lower = embed first under size budget
	runes       map[rune]struct{}
}

func computeCSSWeightRanges(weights []int) map[int]string {
	if len(weights) == 0 {
		return nil
	}
	sorted := append([]int(nil), weights...)
	sort.Ints(sorted)
	res := make(map[int]string, len(sorted))
	if len(sorted) == 1 {
		res[sorted[0]] = "100 900"
		return res
	}
	for i, w := range sorted {
		lo := 100
		if i > 0 {
			lo = (sorted[i-1]+w)/2 + 1
		}
		hi := 900
		if i+1 < len(sorted) {
			hi = (w + sorted[i+1]) / 2
		}
		if lo == hi {
			res[w] = fmt.Sprintf("%d", w)
		} else {
			res[w] = fmt.Sprintf("%d %d", lo, hi)
		}
	}
	return res
}

func htmlSubfamilyName(weight int, italic bool) string {
	switch {
	case weight >= 900 && italic:
		return "Black Italic"
	case weight >= 900:
		return "Black"
	case weight >= 700 && italic:
		return "Bold Italic"
	case weight >= 700:
		return "Bold"
	case weight >= 600:
		return "SemiBold"
	case weight >= 500:
		return "Medium"
	case italic:
		return "Italic"
	default:
		return "Regular"
	}
}

func buildHTMLEmbeddedFontsCSS(pal PaperThemeSpec, legacy bool, htmlBody string) string {
	buckets := extractHTMLTextBuckets(htmlBody)
	if len(buckets.all) == 0 {
		return ""
	}
	families := htmlThemeManagedFamilies(pal, legacy, htmlBody)
	hasItalic := strings.Contains(htmlBody, "<em") ||
		strings.Contains(htmlBody, "<i>") ||
		strings.Contains(htmlBody, "class=\"math\"") ||
		strings.Contains(htmlBody, "font-style:italic")
	var tasks []htmlFontFaceTask

	for famIdx, fam := range families {
		faces := loadExportFontFamilyFaces("", fam)
		if len(faces) == 0 {
			continue
		}
		// Separate upright and italic weights to build continuous CSS weight ranges.
		var normalWeights, italicWeights []int
		for _, f := range faces {
			if f.italic {
				if !hasItalic {
					continue
				}
				italicWeights = append(italicWeights, f.weight)
			} else {
				normalWeights = append(normalWeights, f.weight)
			}
		}
		normalRanges := computeCSSWeightRanges(normalWeights)
		italicRanges := computeCSSWeightRanges(italicWeights)

		isMono := strings.EqualFold(fam, "JetBrains Mono")
		for _, f := range faces {
			if f.italic && !hasItalic {
				continue
			}
			wRange := normalRanges[f.weight]
			if f.italic {
				wRange = italicRanges[f.weight]
			}
			// Pick appropriate rune set for compact output without dropping glyphs.
			var runes map[rune]struct{}
			switch {
			case isMono:
				runes = buckets.ascii
			case f.weight <= 400 && !f.italic:
				runes = buckets.all
			case len(buckets.all) <= htmlFullRuneWeightLimit:
				runes = buckets.all
			default:
				if len(buckets.bold) > 0 {
					runes = buckets.bold
				} else {
					runes = buckets.all
				}
			}
			// Prioritize primary Regular faces, then Bold/Medium/SemiBold, then Black/Italic.
			prio := famIdx * 10
			switch {
			case f.weight == 400 && !f.italic:
				prio += 0
			case f.weight == 700 && !f.italic:
				prio += 2
			case f.weight == 500 || f.weight == 600:
				prio += 3
			case f.weight >= 900:
				prio += 4
			default:
				prio += 5
			}
			tasks = append(tasks, htmlFontFaceTask{
				face:        f,
				weightRange: wRange,
				priority:    prio,
				runes:       runes,
			})
		}
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].priority < tasks[j].priority
	})

	var css strings.Builder
	totalBytes := 0
	for _, t := range tasks {
		ttf, _, _, err := subsetTrueTypeFont(t.face.data, t.runes, sfntNameMeta{
			family:    t.face.family,
			subfamily: htmlSubfamilyName(t.face.weight, t.face.italic),
			weight:    t.face.weight,
			italic:    t.face.italic,
		})
		if err != nil || len(ttf) == 0 {
			continue
		}
		woff2, err := encodeWOFF2FromTTF(ttf)
		if err != nil || len(woff2) == 0 {
			continue
		}
		if totalBytes+len(woff2) > maxHTMLEmbeddedFontBytes {
			continue
		}
		totalBytes += len(woff2)
		style := "normal"
		if t.face.italic {
			style = "italic"
		}
		css.WriteString("@font-face{font-family:'")
		css.WriteString(t.face.family)
		css.WriteString("';font-weight:")
		css.WriteString(t.weightRange)
		css.WriteString(";font-style:")
		css.WriteString(style)
		css.WriteString(";font-display:swap;src:url(data:font/woff2;base64,")
		css.WriteString(base64.StdEncoding.EncodeToString(woff2))
		css.WriteString(") format('woff2')}\n")
	}
	return css.String()
}

// ---- DOCX TrueType .odttf Subsetting & fontTable.xml ----

type docxFontSlot uint8

const (
	docxSlotRegular docxFontSlot = iota
	docxSlotBold
	docxSlotItalic
	docxSlotBoldItalic
)

func (s docxFontSlot) xmlTag() string {
	switch s {
	case docxSlotBold:
		return "w:embedBold"
	case docxSlotItalic:
		return "w:embedItalic"
	case docxSlotBoldItalic:
		return "w:embedBoldItalic"
	default:
		return "w:embedRegular"
	}
}

func (s docxFontSlot) subfamily() string {
	switch s {
	case docxSlotBold:
		return "Bold"
	case docxSlotItalic:
		return "Italic"
	case docxSlotBoldItalic:
		return "Bold Italic"
	default:
		return "Regular"
	}
}

func (s docxFontSlot) weight() int {
	if s == docxSlotBold || s == docxSlotBoldItalic {
		return 700
	}
	return 400
}

func (s docxFontSlot) italic() bool {
	return s == docxSlotItalic || s == docxSlotBoldItalic
}

type docxFontUsageKey struct {
	family string
	slot   docxFontSlot
}

type docxEmbeddedFontPart struct {
	path string // e.g. "word/fonts/font1.odttf"
	data []byte
}

type docxFontBundle struct {
	fontTableXML     string
	fontTableRelsXML string
	parts            []docxEmbeddedFontPart
}

func (b docxFontBundle) hasFonts() bool {
	return len(b.parts) > 0 && b.fontTableXML != "" && b.fontTableRelsXML != ""
}

func parseDOCXToggleVal(attrs []xml.Attr) bool {
	for _, a := range attrs {
		if a.Name.Local == "val" {
			v := strings.ToLower(strings.TrimSpace(a.Value))
			return v != "0" && v != "false" && v != "off"
		}
	}
	return true
}

// collectDOCXFontUsage walks the generated word/document.xml and records the
// exact Unicode runes rendered under each (font family, Regular/Bold/Italic) slot.
func collectDOCXFontUsage(docXML string, defaultLatin, defaultEast string) map[docxFontUsageKey]map[rune]struct{} {
	usage := make(map[docxFontUsageKey]map[rune]struct{})
	addRune := func(family string, slot docxFontSlot, r rune) {
		family = strings.TrimSpace(family)
		if family == "" {
			return
		}
		k := docxFontUsageKey{family: family, slot: slot}
		m := usage[k]
		if m == nil {
			m = make(map[rune]struct{}, 64)
			usage[k] = m
		}
		m[r] = struct{}{}
	}

	dec := xml.NewDecoder(strings.NewReader(docXML))
	inRun := false
	inRPr := false
	inText := false
	runLatin := defaultLatin
	runEast := defaultEast
	runBold := false
	runItalic := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "r":
				inRun = true
				inRPr = false
				inText = false
				runLatin = defaultLatin
				runEast = defaultEast
				runBold = false
				runItalic = false
			case "rPr":
				if inRun {
					inRPr = true
				}
			case "rFonts":
				if inRPr {
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "ascii", "hAnsi":
							if v := strings.TrimSpace(a.Value); v != "" {
								runLatin = v
							}
						case "eastAsia":
							if v := strings.TrimSpace(a.Value); v != "" {
								runEast = v
							}
						}
					}
				}
			case "b":
				if inRPr {
					runBold = parseDOCXToggleVal(t.Attr)
				}
			case "i":
				if inRPr {
					runItalic = parseDOCXToggleVal(t.Attr)
				}
			case "t":
				if inRun {
					inText = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "r":
				inRun = false
				inRPr = false
				inText = false
			case "rPr":
				inRPr = false
			case "t":
				inText = false
			}
		case xml.CharData:
			if !inText {
				continue
			}
			slot := docxSlotRegular
			switch {
			case runBold && runItalic:
				slot = docxSlotBoldItalic
			case runBold:
				slot = docxSlotBold
			case runItalic:
				slot = docxSlotItalic
			}
			for _, r := range string(t) {
				if r == '\r' || r == '\n' || r == '\t' {
					continue
				}
				if r < 0x80 {
					addRune(runLatin, slot, r)
				} else {
					addRune(runEast, slot, r)
				}
			}
		}
	}
	return usage
}

func selectDOCXFaceForSlot(faces []exportFontFace, slot docxFontSlot) (exportFontFace, bool) {
	pick := func(wantItalic bool, weights ...int) (exportFontFace, bool) {
		for _, w := range weights {
			for _, f := range faces {
				if f.italic == wantItalic && f.weight == w {
					return f, true
				}
			}
		}
		return exportFontFace{}, false
	}
	switch slot {
	case docxSlotRegular:
		return pick(false, 400, 500)
	case docxSlotBold:
		return pick(false, 700, 600, 500, 900)
	case docxSlotItalic:
		// Only embed a real italic face when one exists; otherwise let Word synthesize slant from Regular.
		return pick(true, 400)
	case docxSlotBoldItalic:
		return pick(true, 700)
	default:
		return exportFontFace{}, false
	}
}

func docxFontMetaTags(family string, panose [10]byte, hasPanose bool) string {
	var b strings.Builder
	if hasPanose {
		b.WriteString(`<w:panose1 w:val="`)
		b.WriteString(strings.ToUpper(hex.EncodeToString(panose[:])))
		b.WriteString(`"/>`)
	}
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "jetbrains mono":
		b.WriteString(`<w:charset w:val="00"/><w:family w:val="modern"/><w:pitch w:val="fixed"/>`)
	case "noto serif sc":
		b.WriteString(`<w:charset w:val="86"/><w:family w:val="roman"/><w:pitch w:val="variable"/>`)
	case "lxgw wenkai":
		b.WriteString(`<w:charset w:val="86"/><w:family w:val="script"/><w:pitch w:val="variable"/>`)
	default:
		b.WriteString(`<w:charset w:val="86"/><w:family w:val="swiss"/><w:pitch w:val="variable"/>`)
	}
	return b.String()
}

func buildDOCXEmbeddedFonts(docXML string, pal PaperThemeSpec) docxFontBundle {
	defLatin, defEast := splitFontPair(pal.BodyFamily)
	usage := collectDOCXFontUsage(docXML, defLatin, defEast)
	if len(usage) == 0 {
		return docxFontBundle{}
	}

	// Deterministic family order; merge runes needed by fallback Italic slots into Regular/Bold
	// when a CJK family has no dedicated italic file, so oblique runs never miss glyphs.
	familiesSet := make(map[string]bool)
	for k := range usage {
		familiesSet[k.family] = true
	}
	families := make([]string, 0, len(familiesSet))
	for fam := range familiesSet {
		families = append(families, fam)
	}
	sort.Strings(families)

	type embeddedSlot struct {
		slot   docxFontSlot
		relID  string
		key    string
		panose [10]byte
		hasPan bool
	}
	type embeddedFamily struct {
		family string
		slots  []embeddedSlot
	}

	var (
		bundle     docxFontBundle
		famEntries []embeddedFamily
		rels       []string
		fontIdx    = 1
		totalBytes = 0
	)

	slotOrder := []docxFontSlot{docxSlotRegular, docxSlotBold, docxSlotItalic, docxSlotBoldItalic}

	for _, fam := range families {
		faces := loadExportFontFamilyFaces("", fam)
		if len(faces) == 0 {
			continue
		}

		// Fold glyphs from italic/bold-italic runs into Regular/Bold when the family has no real italic face.
		slotRunes := make(map[docxFontSlot]map[rune]struct{}, 4)
		for _, slot := range slotOrder {
			if rs := usage[docxFontUsageKey{family: fam, slot: slot}]; len(rs) > 0 {
				targetSlot := slot
				if _, hasDedicated := selectDOCXFaceForSlot(faces, slot); !hasDedicated {
					switch slot {
					case docxSlotItalic:
						targetSlot = docxSlotRegular
					case docxSlotBoldItalic:
						targetSlot = docxSlotBold
					}
				}
				dst := slotRunes[targetSlot]
				if dst == nil {
					dst = make(map[rune]struct{}, len(rs))
					slotRunes[targetSlot] = dst
				}
				for r := range rs {
					dst[r] = struct{}{}
				}
			}
		}

		var entry embeddedFamily
		entry.family = fam
		for _, slot := range slotOrder {
			runes := slotRunes[slot]
			if len(runes) == 0 {
				continue
			}
			face, ok := selectDOCXFaceForSlot(faces, slot)
			if !ok {
				continue
			}
			ttf, panose, hasPan, err := subsetTrueTypeFont(face.data, runes, sfntNameMeta{
				family:             fam,
				subfamily:          slot.subfamily(),
				weight:             slot.weight(),
				italic:             slot.italic(),
				normalizeWordStyle: true,
			})
			if err != nil || len(ttf) == 0 {
				continue
			}
			if totalBytes+len(ttf) > maxDOCXEmbeddedFontBytes {
				continue
			}
			totalBytes += len(ttf)

			odttf, guid := obfuscateODTTF(ttf, fmt.Sprintf("%s:%d", fam, slot))
			relID := fmt.Sprintf("rId%d", fontIdx)
			relTarget := fmt.Sprintf("fonts/font%d.odttf", fontIdx)
			fontIdx++

			bundle.parts = append(bundle.parts, docxEmbeddedFontPart{
				path: "word/" + relTarget,
				data: odttf,
			})
			rels = append(rels, fmt.Sprintf(
				`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/font" Target="%s"/>`,
				relID, relTarget,
			))
			entry.slots = append(entry.slots, embeddedSlot{
				slot:   slot,
				relID:  relID,
				key:    guid,
				panose: panose,
				hasPan: hasPan,
			})
		}
		if len(entry.slots) > 0 {
			famEntries = append(famEntries, entry)
		}
	}

	if len(famEntries) == 0 {
		return docxFontBundle{}
	}

	var ft strings.Builder
	ft.WriteString(xmlDecl)
	ft.WriteString(`<w:fonts xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for _, fe := range famEntries {
		var panose [10]byte
		hasPan := false
		for _, s := range fe.slots {
			if s.hasPan {
				panose = s.panose
				hasPan = true
				break
			}
		}
		ft.WriteString(fmt.Sprintf(`<w:font w:name="%s">%s`, escapeAttrValue(fe.family), docxFontMetaTags(fe.family, panose, hasPan)))
		for _, s := range fe.slots {
			ft.WriteString(fmt.Sprintf(`<%s r:id="%s" w:fontKey="%s" w:subsetted="1"/>`, s.slot.xmlTag(), s.relID, s.key))
		}
		ft.WriteString(`</w:font>`)
	}
	ft.WriteString(`</w:fonts>`)

	bundle.fontTableXML = ft.String()
	bundle.fontTableRelsXML = fmt.Sprintf(
		`%s<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">%s</Relationships>`,
		xmlDecl, strings.Join(rels, ""),
	)
	return bundle
}
