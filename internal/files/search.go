package files

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/fuzzy"
	"block/internal/model"
	"block/internal/theme"
)

const (
	maxSearchFileBytes = 4 * 1024 * 1024
	maxSearchHits      = 500
	snippetBeforeRunes = 24
	snippetAfterRunes  = 56
)

type rankedSearchHit struct {
	hit        model.SearchHit
	titleMatch bool
	titleTier  uint8
	titleStart int
	titleScore int
	bodyTier   uint8
}

type searchQuerySpec struct {
	raw          string
	folded       string
	isASCII      bool
	foldedTokens []string
	tokenASCII   []bool
}

func compileSearchQuery(q string) searchQuerySpec {
	foldedQ := foldPreserveLen(q)
	spec := searchQuerySpec{
		raw:     q,
		folded:  foldedQ,
		isASCII: isASCIIString(foldedQ),
	}
	rawTokens := strings.Fields(q)
	if len(rawTokens) > 1 {
		seen := make(map[string]bool, len(rawTokens))
		for _, rt := range rawTokens {
			ft := foldPreserveLen(rt)
			if ft == "" || seen[ft] {
				continue
			}
			seen[ft] = true
			spec.foldedTokens = append(spec.foldedTokens, ft)
			spec.tokenASCII = append(spec.tokenASCII, isASCIIString(ft))
		}
	}
	return spec
}

// SearchNotebooks walks each root notebook directory and performs a
// case-insensitive search across note titles and bodies while respecting
// ScanOptions and theme.IsSearchFolderIgnored.
func SearchNotebooks(roots []string, query string, opts ScanOptions) ([]model.SearchHit, error) {
	return SearchNotebooksContext(context.Background(), roots, query, opts)
}

// SearchNotebooksContext is like SearchNotebooks but stops walking early when ctx is cancelled.
func SearchNotebooksContext(ctx context.Context, roots []string, query string, opts ScanOptions) ([]model.SearchHit, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	spec := compileSearchQuery(q)
	if spec.folded == "" {
		return nil, nil
	}

	seenRoots := make(map[string]bool, len(roots))
	seenFiles := make(map[string]bool)
	var titleHits []rankedSearchHit
	var bodyHits []rankedSearchHit
	exactTitleCount := 0
	exactBodyCount := 0

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if exactTitleCount >= maxSearchHits {
			break
		}
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
		if err := walkSearchDir(ctx, cleanRoot, "", notebookName, spec, opts, 0, seenFiles, &titleHits, &bodyHits, &exactTitleCount, &exactBodyCount); err != nil {
			return nil, err
		}
	}

	sort.Slice(titleHits, func(i, j int) bool {
		a, b := titleHits[i], titleHits[j]
		if a.titleTier != b.titleTier {
			return a.titleTier < b.titleTier
		}
		if a.titleTier <= 2 {
			if a.titleStart != b.titleStart {
				return a.titleStart < b.titleStart
			}
		} else if a.titleScore != b.titleScore {
			return a.titleScore > b.titleScore
		}
		la, lb := strings.ToLower(a.hit.Title), strings.ToLower(b.hit.Title)
		if la != lb {
			return la < lb
		}
		return a.hit.Path < b.hit.Path
	})

	sort.Slice(bodyHits, func(i, j int) bool {
		a, b := bodyHits[i], bodyHits[j]
		if a.bodyTier != b.bodyTier {
			return a.bodyTier < b.bodyTier
		}
		la, lb := strings.ToLower(a.hit.Title), strings.ToLower(b.hit.Title)
		if la != lb {
			return la < lb
		}
		return a.hit.Path < b.hit.Path
	})

	out := make([]model.SearchHit, 0, min(len(titleHits)+len(bodyHits), maxSearchHits))
	for _, h := range titleHits {
		if len(out) >= maxSearchHits {
			break
		}
		out = append(out, h.hit)
	}
	for _, h := range bodyHits {
		if len(out) >= maxSearchHits {
			break
		}
		out = append(out, h.hit)
	}
	return out, nil
}

func walkSearchDir(
	ctx context.Context,
	dir string,
	relPrefix string,
	notebook string,
	spec searchQuerySpec,
	opts ScanOptions,
	depth int,
	seenFiles map[string]bool,
	titleHits *[]rankedSearchHit,
	bodyHits *[]rankedSearchHit,
	exactTitleCount *int,
	exactBodyCount *int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxScanDepth || theme.IsSearchFolderIgnored(dir) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Exact title matches alone fill the entire result list.
		if *exactTitleCount >= maxSearchHits {
			return nil
		}
		rawName := entry.Name()
		mode := entry.Type()

		if mode.IsDir() {
			if isIgnoredDir(rawName, opts) {
				continue
			}
			fullPath := filepath.Join(dir, rawName)
			nextPrefix := rawName
			if relPrefix != "" {
				nextPrefix = relPrefix + "/" + rawName
			}
			if err := walkSearchDir(ctx, fullPath, nextPrefix, notebook, spec, opts, depth+1, seenFiles, titleHits, bodyHits, exactTitleCount, exactBodyCount); err != nil {
				return err
			}
			continue
		}

		if isIgnoredFile(rawName) {
			continue
		}
		mdExt := markdownExtension(rawName)
		if mdExt == "" {
			continue
		}
		fullPath := filepath.Join(dir, rawName)
		var fileInfo os.FileInfo
		if mode&os.ModeSymlink != 0 {
			st, statErr := os.Stat(fullPath)
			if statErr != nil || !st.Mode().IsRegular() {
				continue
			}
			fileInfo = st
		} else if !mode.IsRegular() {
			continue
		}
		if seenFiles[fullPath] {
			continue
		}
		seenFiles[fullPath] = true

		skipBody := *exactBodyCount >= maxSearchHits
		stem := DisplayName(rawName)
		relStem := stem
		if relPrefix != "" {
			relStem = relPrefix + "/" + stem
		}
		if ranked, ok := inspectSearchFile(entry, fileInfo, fullPath, stem, relStem, notebook, spec, skipBody); ok {
			if ranked.titleMatch {
				if ranked.titleTier <= 2 {
					*exactTitleCount++
					*titleHits = append(*titleHits, ranked)
				} else if len(*titleHits) < maxSearchHits*4 {
					*titleHits = append(*titleHits, ranked)
				}
			} else if ranked.bodyTier == 0 {
				*exactBodyCount++
				*bodyHits = append(*bodyHits, ranked)
			} else if len(*bodyHits) < maxSearchHits*4 {
				*bodyHits = append(*bodyHits, ranked)
			}
		}
	}
	return nil
}

func matchTitleOrPath(stem, relStem string, spec searchQuerySpec) (matched bool, tier uint8, start int, score int) {
	var titleStart int
	var exactMatched bool
	if spec.isASCII {
		titleStart, _, exactMatched = findFoldMatchASCIIString(stem, spec.folded)
	} else {
		titleStart, _, exactMatched = findFoldMatchFolded(stem, spec.folded)
	}
	if exactMatched {
		if titleStart == 0 && len(stem) == len(spec.raw) && strings.EqualFold(stem, spec.raw) {
			return true, 0, 0, 10000
		}
		if titleStart == 0 {
			return true, 1, 0, 8000
		}
		return true, 2, titleStart, 6000
	}

	if sc, ok := fuzzy.Score(spec.raw, []string{stem}, nil, false); ok {
		if len(spec.foldedTokens) > 1 && sc >= 3500 {
			return true, 3, 0, sc
		}
		return true, 4, 0, sc
	}

	if relStem != stem {
		if len(spec.foldedTokens) > 1 || strings.Contains(spec.raw, "/") {
			if sc, ok := fuzzy.Score(spec.raw, []string{relStem}, nil, false); ok {
				return true, 5, 0, sc
			}
		} else {
			var pathMatched bool
			if spec.isASCII {
				_, _, pathMatched = findFoldMatchASCIIString(relStem, spec.folded)
			} else {
				_, _, pathMatched = findFoldMatchFolded(relStem, spec.folded)
			}
			if pathMatched {
				return true, 5, 0, 4500
			}
		}
	}

	return false, 0, 0, 0
}

func inspectSearchFile(entry os.DirEntry, info os.FileInfo, fullPath, stem, relStem, notebook string, spec searchQuerySpec, skipBody bool) (rankedSearchHit, bool) {
	titleMatched, titleTier, titleStart, titleScore := matchTitleOrPath(stem, relStem, spec)
	if !titleMatched && skipBody {
		return rankedSearchHit{}, false
	}

	if info == nil && entry != nil {
		if entryInfo, err := entry.Info(); err == nil {
			info = entryInfo
		}
	}
	var data []byte
	if info != nil && info.Size() <= maxSearchFileBytes {
		if raw, readErr := os.ReadFile(fullPath); readErr == nil && utf8.Valid(raw) {
			data = raw
		}
	}

	matchLine, matchColumn := 0, 0
	var lineSnippet string
	var mStart, mEnd int
	bodyMatched := false
	var bodyTier uint8

	if len(data) > 0 && !skipBody {
		multiTok := len(spec.foldedTokens) > 1
		var (
			hasSameLine   bool
			sameLineNum   int
			sameLineCol   int
			sameLineEnd   int
			sameLineBytes []byte

			hasFirstTok   bool
			firstTokLine  int
			firstTokCol   int
			firstTokEnd   int
			firstTokBytes []byte
			seenTokens    []bool
			seenCount     int
		)
		if multiTok {
			seenTokens = make([]bool, len(spec.foldedTokens))
		}

		rest := data
		lineNum := 0
		for {
			lineNum++
			var lineBytes []byte
			nl := bytes.IndexByte(rest, '\n')
			if nl >= 0 {
				lineBytes = rest[:nl]
				rest = rest[nl+1:]
			} else {
				lineBytes = rest
			}
			if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
				lineBytes = lineBytes[:len(lineBytes)-1]
			}
			s, e, ok := findFoldMatchBytes(lineBytes, spec.folded, spec.isASCII)
			if ok {
				lineStr := string(lineBytes)
				bodyMatched = true
				bodyTier = 0
				matchLine, matchColumn = lineNum, s
				lineSnippet = snippetAround(lineStr, s, e)
				if spec.isASCII {
					if ms, me, found := findFoldMatchASCIIString(lineSnippet, spec.folded); found {
						mStart, mEnd = ms, me
					}
				} else if ms, me, found := findFoldMatchFolded(lineSnippet, spec.folded); found {
					mStart, mEnd = ms, me
				}
				break
			}
			if multiTok && len(lineBytes) > 0 {
				lineAll := !hasSameLine
				firstStart, firstEnd := -1, 0
				for ti, ft := range spec.foldedTokens {
					ts, te, tokOK := findFoldMatchBytes(lineBytes, ft, spec.tokenASCII[ti])
					if tokOK {
						if !seenTokens[ti] {
							seenTokens[ti] = true
							seenCount++
						}
						if firstStart < 0 || ts < firstStart {
							firstStart, firstEnd = ts, te
						}
					} else {
						lineAll = false
					}
				}
				if lineAll && firstStart >= 0 {
					hasSameLine = true
					sameLineNum = lineNum
					sameLineCol = firstStart
					sameLineEnd = firstEnd
					sameLineBytes = lineBytes
				}
				if !hasFirstTok && firstStart >= 0 {
					hasFirstTok = true
					firstTokLine = lineNum
					firstTokCol = firstStart
					firstTokEnd = firstEnd
					firstTokBytes = lineBytes
				}
			}
			if nl < 0 {
				break
			}
		}

		if !bodyMatched && multiTok {
			if hasSameLine {
				bodyMatched = true
				bodyTier = 1
				matchLine, matchColumn = sameLineNum, sameLineCol
				lineSnippet = snippetAround(string(sameLineBytes), sameLineCol, sameLineEnd)
				if ranges := FindBodyHighlights(lineSnippet, spec.raw); len(ranges) > 0 {
					mStart, mEnd = ranges[0][0], ranges[0][1]
				}
			} else if seenCount == len(spec.foldedTokens) && hasFirstTok {
				bodyMatched = true
				bodyTier = 2
				matchLine, matchColumn = firstTokLine, firstTokCol
				lineSnippet = snippetAround(string(firstTokBytes), firstTokCol, firstTokEnd)
				if ranges := FindBodyHighlights(lineSnippet, spec.raw); len(ranges) > 0 {
					mStart, mEnd = ranges[0][0], ranges[0][1]
				}
			}
		}
	}

	if !titleMatched && !bodyMatched {
		return rankedSearchHit{}, false
	}

	if !bodyMatched {
		lineSnippet = firstContentLineBytes(data)
	}

	return rankedSearchHit{
		hit: model.SearchHit{
			Path:       fullPath,
			Notebook:   notebook,
			Title:      stem,
			TitleMatch: titleMatched,
			Query:      spec.raw,
			Line:       matchLine,
			Column:     matchColumn,
			Snippet:    lineSnippet,
			MatchStart: mStart,
			MatchEnd:   mEnd,
		},
		titleMatch: titleMatched,
		titleTier:  titleTier,
		titleStart: titleStart,
		titleScore: titleScore,
		bodyTier:   bodyTier,
	}, true
}

func isASCIIString(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func hasNonASCIIBytes(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return true
		}
	}
	return false
}

func findFoldMatchBytes(line []byte, foldedNeedle string, needleIsASCII bool) (int, int, bool) {
	if len(foldedNeedle) == 0 || len(line) < len(foldedNeedle) {
		return 0, 0, false
	}
	if needleIsASCII {
		idx := indexFoldASCIIBytes(line, foldedNeedle)
		if idx < 0 {
			return 0, 0, false
		}
		return idx, idx + len(foldedNeedle), true
	}
	if !hasNonASCIIBytes(line) {
		return 0, 0, false
	}
	return findFoldMatchFolded(string(line), foldedNeedle)
}

func indexFoldASCIIBytes(hay []byte, foldedNeedle string) int {
	nLen := len(foldedNeedle)
	if nLen == 0 {
		return 0
	}
	if len(hay) < nLen {
		return -1
	}
	first := foldedNeedle[0]
	limit := len(hay) - nLen
	for i := 0; i <= limit; i++ {
		c := hay[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != first {
			continue
		}
		matched := true
		for j := 1; j < nLen; j++ {
			hc := hay[i+j]
			if hc >= 'A' && hc <= 'Z' {
				hc += 'a' - 'A'
			}
			if hc != foldedNeedle[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func findFoldMatchASCIIString(text, foldedNeedle string) (int, int, bool) {
	nLen := len(foldedNeedle)
	if nLen == 0 || len(text) < nLen {
		return 0, 0, false
	}
	first := foldedNeedle[0]
	limit := len(text) - nLen
	for i := 0; i <= limit; i++ {
		c := text[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != first {
			continue
		}
		matched := true
		for j := 1; j < nLen; j++ {
			hc := text[i+j]
			if hc >= 'A' && hc <= 'Z' {
				hc += 'a' - 'A'
			}
			if hc != foldedNeedle[j] {
				matched = false
				break
			}
		}
		if matched {
			return i, i + nLen, true
		}
	}
	return 0, 0, false
}

func nextLineByteSlice(b []byte) (line, rest []byte, hasMore bool) {
	if nl := bytes.IndexByte(b, '\n'); nl >= 0 {
		line = b[:nl]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return line, b[nl+1:], true
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b, nil, false
}

func firstContentLineBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	rest := data
	if first, afterFirst, hasMore := nextLineByteSlice(data); hasMore && len(bytes.TrimSpace(first)) == 3 && bytes.Equal(bytes.TrimSpace(first), []byte("---")) {
		scan := afterFirst
		for {
			line, next, more := nextLineByteSlice(scan)
			if bytes.Equal(bytes.TrimSpace(line), []byte("---")) {
				if more {
					rest = next
				} else {
					rest = nil
				}
				break
			}
			if !more {
				break
			}
			scan = next
		}
	}
	for len(rest) > 0 {
		line, next, more := nextLineByteSlice(rest)
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			trimmed = bytes.TrimSpace(bytes.TrimLeft(trimmed, "#"))
			if len(trimmed) > 0 {
				s := string(trimmed)
				if utf8.RuneCountInString(s) > 70 {
					runes := []rune(s)
					return string(runes[:70])
				}
				return s
			}
		}
		if !more {
			break
		}
		rest = next
	}
	return ""
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

// FindTitleHighlights returns merged UTF-8 byte ranges in title matching query:
// exact substring first, then multi-token substrings, then fuzzy subsequence ranges.
func FindTitleHighlights(title, query string) [][2]int {
	q := strings.TrimSpace(query)
	if q == "" || title == "" {
		return nil
	}
	if exact := FindFoldMatches(title, q); len(exact) > 0 {
		return exact
	}
	if multi := findTokenFoldMatches(title, q); len(multi) > 0 {
		return multi
	}
	return fuzzy.SubsequenceRanges(title, q)
}

// FindBodyHighlights returns merged UTF-8 byte ranges in text matching query:
// exact substring first, then multi-token substrings (never char-level fuzzy).
func FindBodyHighlights(text, query string) [][2]int {
	q := strings.TrimSpace(query)
	if q == "" || text == "" {
		return nil
	}
	if exact := FindFoldMatches(text, q); len(exact) > 0 {
		return exact
	}
	return findTokenFoldMatches(text, q)
}

func findTokenFoldMatches(text, query string) [][2]int {
	tokens := strings.Fields(query)
	if len(tokens) <= 1 {
		return nil
	}
	var all [][2]int
	for _, tok := range tokens {
		all = append(all, FindFoldMatches(text, tok)...)
	}
	return mergeHighlightRanges(all)
}

func mergeHighlightRanges(ranges [][2]int) [][2]int {
	if len(ranges) <= 1 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i][0] != ranges[j][0] {
			return ranges[i][0] < ranges[j][0]
		}
		return ranges[i][1] < ranges[j][1]
	})
	out := make([][2]int, 0, len(ranges))
	for _, r := range ranges {
		if r[0] >= r[1] {
			continue
		}
		if len(out) > 0 && r[0] <= out[len(out)-1][1] {
			if r[1] > out[len(out)-1][1] {
				out[len(out)-1][1] = r[1]
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// ResolveSearchMatch revalidates a search result against the open note. If edits
// moved the match, prefer the occurrence nearest its original line and column.
func ResolveSearchMatch(text string, hit model.SearchHit) (line, column int) {
	if hit.Line <= 0 || hit.Query == "" {
		return hit.Line, hit.Column
	}
	if l, c, ok := resolveSearchMatchWith(text, hit, FindFoldMatches); ok {
		return l, c
	}
	if len(strings.Fields(hit.Query)) > 1 {
		if l, c, ok := resolveSearchMatchWith(text, hit, FindBodyHighlights); ok {
			return l, c
		}
	}
	return hit.Line, hit.Column
}

func resolveSearchMatchWith(text string, hit model.SearchHit, matcher func(string, string) [][2]int) (line, column int, found bool) {
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
		for _, match := range matcher(raw, hit.Query) {
			columnDistance := absSearchDistance(match[0] - hit.Column)
			if !found || lineDistance < bestLineDistance || (lineDistance == bestLineDistance && columnDistance < bestColumnDistance) {
				line, column = i, match[0]
				bestLineDistance, bestColumnDistance = lineDistance, columnDistance
				found = true
			}
		}
		if found && bestLineDistance == 0 {
			break
		}
	}
	return line, column, found
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
