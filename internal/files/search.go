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
	foldedQ := foldPreserveLen(q)
	if foldedQ == "" {
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
		walkSearchDir(cleanRoot, notebookName, q, foldedQ, opts, 0, seenFiles, &titleHits, &bodyHits)
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
	rawQuery string,
	foldedQuery string,
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
			walkSearchDir(fullPath, notebook, rawQuery, foldedQuery, opts, depth+1, seenFiles, titleHits, bodyHits)
			continue
		}

		isRegular := mode.IsRegular()
		var fileInfo os.FileInfo
		if !isRegular && mode&os.ModeSymlink != 0 {
			if st, statErr := os.Stat(fullPath); statErr == nil && st.Mode().IsRegular() {
				isRegular = true
				fileInfo = st
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

		if ranked, ok := inspectSearchFile(entry, fileInfo, fullPath, rawName, notebook, rawQuery, foldedQuery); ok {
			if ranked.titleMatch {
				*titleHits = append(*titleHits, ranked)
			} else {
				*bodyHits = append(*bodyHits, ranked)
			}
		}
	}
}

func inspectSearchFile(entry os.DirEntry, info os.FileInfo, fullPath, rawName, notebook, rawQuery, foldedQuery string) (rankedSearchHit, bool) {
	stem := entryDisplayName(rawName, false)
	titleStart, _, titleMatched := findFoldMatchFolded(stem, foldedQuery)

	if info == nil && entry != nil {
		if entryInfo, err := entry.Info(); err == nil {
			info = entryInfo
		}
	}
	var text string
	if info != nil && info.Size() <= maxSearchFileBytes {
		if data, readErr := os.ReadFile(fullPath); readErr == nil && utf8.Valid(data) {
			text = string(data)
		}
	}

	matchLine, matchColumn := 0, 0
	var lineSnippet string
	var mStart, mEnd int
	bodyMatched := false

	if text != "" {
		rest := text
		lineNum := 0
		for {
			lineNum++
			var line string
			nl := strings.IndexByte(rest, '\n')
			if nl >= 0 {
				line = rest[:nl]
				rest = rest[nl+1:]
			} else {
				line = rest
			}
			line = strings.TrimSuffix(line, "\r")
			if s, e, ok := findFoldMatchFolded(line, foldedQuery); ok {
				bodyMatched = true
				matchLine, matchColumn = lineNum, s
				lineSnippet = snippetAround(line, s, e)
				if ms, me, found := findFoldMatchFolded(lineSnippet, foldedQuery); found {
					mStart, mEnd = ms, me
				}
				break
			}
			if nl < 0 {
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
			Query:      rawQuery,
			Line:       matchLine,
			Column:     matchColumn,
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

// ResolveSearchMatch revalidates a search result against the open note. If edits
// moved the match, prefer the occurrence nearest its original line and column.
func ResolveSearchMatch(text string, hit model.SearchHit) (line, column int) {
	if hit.Line <= 0 || hit.Query == "" {
		return hit.Line, hit.Column
	}
	bestLineDistance, bestColumnDistance := len(text)+hit.Line, len(text)+hit.Column
	var i int
	for raw := range strings.SplitSeq(text, "\n") {
		i++
		lineDistance := absSearchDistance(i - hit.Line)
		if lineDistance > bestLineDistance {
			if i > hit.Line {
				break
			}
			continue
		}
		for _, match := range FindFoldMatches(raw, hit.Query) {
			columnDistance := absSearchDistance(match[0] - hit.Column)
			if lineDistance < bestLineDistance || (lineDistance == bestLineDistance && columnDistance < bestColumnDistance) {
				line, column = i, match[0]
				bestLineDistance, bestColumnDistance = lineDistance, columnDistance
			}
		}
		if bestLineDistance == 0 {
			break
		}
	}
	return line, column
}

func absSearchDistance(distance int) int {
	return max(distance, -distance)
}

func findFoldMatch(text, needle string) (int, int, bool) {
	return findFoldMatchFolded(text, foldPreserveLen(needle))
}

func findFoldMatchFolded(text, foldedNeedle string) (int, int, bool) {
	if foldedNeedle == "" {
		return 0, 0, false
	}
	hay := foldPreserveLen(text)
	idx := strings.Index(hay, foldedNeedle)
	if idx < 0 {
		return 0, 0, false
	}
	end := idx + len(foldedNeedle)
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

func nextLineSlice(s string) (line, rest string, hasMore bool) {
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		return strings.TrimSuffix(s[:nl], "\r"), s[nl+1:], true
	}
	return strings.TrimSuffix(s, "\r"), "", false
}

func firstContentLine(text string) string {
	rest := text
	if first, afterFirst, hasMore := nextLineSlice(text); hasMore && strings.TrimSpace(first) == "---" {
		scan := afterFirst
		for {
			line, next, more := nextLineSlice(scan)
			if strings.TrimSpace(line) == "---" {
				if more {
					rest = next
				} else {
					rest = ""
				}
				break
			}
			if !more {
				break
			}
			scan = next
		}
	}
	for {
		line, next, more := nextLineSlice(rest)
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if trimmed != "" {
				runes := []rune(trimmed)
				if len(runes) > 70 {
					return string(runes[:70])
				}
				return trimmed
			}
		}
		if !more {
			break
		}
		rest = next
	}
	return ""
}
