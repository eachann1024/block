package files

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
)

const (
	maxSearchFileBytes = 4 * 1024 * 1024
	snippetBeforeRunes = 24
	snippetAfterRunes  = 56
)

type rankedSearchHit struct {
	hit        model.SearchHit
	titleMatch bool
	titleStart int
}

// SearchNotebooks walks each root notebook directory and performs a
// case-insensitive search across note titles and bodies while respecting
// ScanOptions and theme.IsSearchFolderIgnored.
func SearchNotebooks(roots []string, query string, opts ScanOptions) ([]model.SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	seenRoots := make(map[string]bool, len(roots))
	seenFiles := make(map[string]bool)
	var titleHits []rankedSearchHit
	var bodyHits []rankedSearchHit

	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if seenRoots[cleanRoot] {
			continue
		}
		seenRoots[cleanRoot] = true

		if theme.IsSearchFolderIgnored(cleanRoot) {
			continue
		}
		if info, err := os.Stat(cleanRoot); err != nil || !info.IsDir() {
			continue
		}

		notebookName := filepath.Base(cleanRoot)
		walkSearchDir(cleanRoot, notebookName, q, opts, 0, seenFiles, &titleHits, &bodyHits)
	}

	sort.Slice(titleHits, func(i, j int) bool {
		a, b := titleHits[i], titleHits[j]
		ra := titleHitRank(a, q)
		rb := titleHitRank(b, q)
		if ra.tier != rb.tier {
			return ra.tier < rb.tier
		}
		if ra.start != rb.start {
			return ra.start < rb.start
		}
		la, lb := strings.ToLower(a.hit.Title), strings.ToLower(b.hit.Title)
		if la != lb {
			return la < lb
		}
		return a.hit.Path < b.hit.Path
	})

	sort.Slice(bodyHits, func(i, j int) bool {
		a, b := bodyHits[i], bodyHits[j]
		la, lb := strings.ToLower(a.hit.Title), strings.ToLower(b.hit.Title)
		if la != lb {
			return la < lb
		}
		return a.hit.Path < b.hit.Path
	})

	out := make([]model.SearchHit, 0, len(titleHits)+len(bodyHits))
	for _, h := range titleHits {
		out = append(out, h.hit)
	}
	for _, h := range bodyHits {
		out = append(out, h.hit)
	}
	return out, nil
}

type titleRank struct {
	tier  uint8
	start int
}

func titleHitRank(h rankedSearchHit, query string) titleRank {
	if h.titleStart == 0 && len(h.hit.Title) == len(query) && strings.EqualFold(h.hit.Title, query) {
		return titleRank{tier: 0, start: 0}
	}
	if h.titleStart == 0 {
		return titleRank{tier: 1, start: 0}
	}
	return titleRank{tier: 2, start: h.titleStart}
}

func walkSearchDir(
	dir string,
	notebook string,
	query string,
	opts ScanOptions,
	depth int,
	seenFiles map[string]bool,
	titleHits *[]rankedSearchHit,
	bodyHits *[]rankedSearchHit,
) {
	if depth > maxScanDepth || theme.IsSearchFolderIgnored(dir) {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		rawName := entry.Name()
		fullPath := filepath.Join(dir, rawName)
		mode := entry.Type()

		if mode.IsDir() {
			if isIgnoredDir(rawName, opts) || theme.IsSearchFolderIgnored(fullPath) {
				continue
			}
			walkSearchDir(fullPath, notebook, query, opts, depth+1, seenFiles, titleHits, bodyHits)
			continue
		}

		isRegular := mode.IsRegular()
		if !isRegular && mode&os.ModeSymlink != 0 {
			if st, statErr := os.Stat(fullPath); statErr == nil && st.Mode().IsRegular() {
				isRegular = true
			}
		}
		if !isRegular {
			continue
		}
		if strings.HasPrefix(rawName, ".") || isCommonIgnoredName(rawName) || !IsMarkdown(rawName) {
			continue
		}
		if seenFiles[fullPath] {
			continue
		}
		seenFiles[fullPath] = true

		if ranked, ok := inspectSearchFile(fullPath, rawName, notebook, query); ok {
			if ranked.titleMatch {
				*titleHits = append(*titleHits, ranked)
			} else {
				*bodyHits = append(*bodyHits, ranked)
			}
		}
	}
}

func inspectSearchFile(fullPath, rawName, notebook, query string) (rankedSearchHit, bool) {
	stem := entryDisplayName(rawName, false)
	titleStart, _, titleMatched := findFoldMatch(stem, query)

	var text string
	if info, err := os.Stat(fullPath); err == nil && info.Size() <= maxSearchFileBytes {
		if data, readErr := os.ReadFile(fullPath); readErr == nil && utf8.Valid(data) {
			text = string(data)
		}
	}

	matchLine := 0
	var lineSnippet string
	var mStart, mEnd int
	bodyMatched := false

	if text != "" {
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		for idx, line := range lines {
			s, e, ok := findFoldMatch(line, query)
			if ok {
				bodyMatched = true
				matchLine = idx + 1
				lineSnippet = snippetAround(line, s, e)
				if ms, me, found := findFoldMatch(lineSnippet, query); found {
					mStart, mEnd = ms, me
				}
				break
			}
		}
	}

	if !titleMatched && !bodyMatched {
		return rankedSearchHit{}, false
	}

	if !bodyMatched {
		lineSnippet = firstContentLine(text)
	}

	return rankedSearchHit{
		hit: model.SearchHit{
			Path:       fullPath,
			Notebook:   notebook,
			Title:      stem,
			Line:       matchLine,
			Snippet:    lineSnippet,
			MatchStart: mStart,
			MatchEnd:   mEnd,
		},
		titleMatch: titleMatched,
		titleStart: titleStart,
	}, true
}

// foldPreserveLen lowercases text while keeping byte lengths valid when possible.
func foldPreserveLen(text string) string {
	lower := strings.ToLower(text)
	if len(lower) == len(text) {
		return lower
	}
	return asciiLower(text)
}

func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// FindFoldMatches returns all non-overlapping case-insensitive byte ranges [start, end]
// of needle in text that align with UTF-8 rune boundaries.
func FindFoldMatches(text, needle string) [][2]int {
	hay := foldPreserveLen(text)
	q := foldPreserveLen(needle)
	if q == "" || len(hay) < len(q) {
		return nil
	}
	var out [][2]int
	offset := 0
	for offset <= len(hay)-len(q) {
		rel := strings.Index(hay[offset:], q)
		if rel < 0 {
			break
		}
		start := offset + rel
		end := start + len(q)
		if (start == 0 || utf8.RuneStart(text[start])) && (end == len(text) || utf8.RuneStart(text[end])) {
			out = append(out, [2]int{start, end})
			offset = end
		} else {
			_, sz := utf8.DecodeRuneInString(hay[start:])
			if sz <= 0 {
				sz = 1
			}
			offset = start + sz
		}
	}
	return out
}

func findFoldMatch(text, needle string) (int, int, bool) {
	hay := foldPreserveLen(text)
	q := foldPreserveLen(needle)
	if q == "" {
		return 0, 0, false
	}
	idx := strings.Index(hay, q)
	if idx < 0 {
		return 0, 0, false
	}
	end := idx + len(q)
	if !utf8.RuneStart(text[idx]) && idx > 0 {
		return 0, 0, false
	}
	if end < len(text) && !utf8.RuneStart(text[end]) {
		return 0, 0, false
	}
	return idx, end, true
}

func snippetAround(text string, matchStart, matchEnd int) string {
	if matchStart < 0 {
		matchStart = 0
	}
	if matchEnd > len(text) {
		matchEnd = len(text)
	}

	start := matchStart
	for count := 0; start > 0 && count < snippetBeforeRunes; count++ {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
	}

	end := matchEnd
	for count := 0; end < len(text) && count < snippetAfterRunes; count++ {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	for _, r := range text[start:end] {
		if unicode.IsControl(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	if end < len(text) {
		b.WriteString("…")
	}
	return b.String()
}

func firstContentLine(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				lines = lines[i+1:]
				break
			}
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if len(runes) > 70 {
			return string(runes[:70])
		}
		return trimmed
	}
	return ""
}
