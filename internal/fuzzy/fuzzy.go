package fuzzy

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tokens splits query on whitespace and lowercases each token.
func Tokens(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	return strings.Fields(q)
}

// ContainsAllTokens reports whether text contains every lowercased token (case-insensitively).
func ContainsAllTokens(text string, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	for _, tok := range tokens {
		if tok != "" && len(FindFoldRanges(text, tok)) == 0 {
			return false
		}
	}
	return true
}

// IsASCII reports whether every byte in s is < 0x80.
func IsASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

type foldedChar struct {
	ch       rune
	bytePos  int
	byteEnd  int
	boundary bool
}

// expandFolded converts text into folded comparison characters while keeping exact
// original UTF-8 byte ranges. Handles Turkish İ -> i, Kelvin K -> k, long-s ſ -> s,
// German ß/ẞ -> ss, and skips combining dot U+0307 after i.
func expandFolded(text string) []foldedChar {
	out := make([]foldedChar, 0, len(text))
	var prev rune
	var prevFolded rune
	for bytePos, r := range text {
		sz := utf8.RuneLen(r)
		if sz < 0 {
			sz = 1
		}
		byteEnd := bytePos + sz
		// Skip combining dot above immediately following i/İ so decomposed i+U+0307 matches "i".
		if r == '\u0307' && prevFolded == 'i' {
			continue
		}
		bnd := bytePos == 0 || isRuneBoundary(prev, r)
		switch r {
		case 'ß', 'ẞ':
			out = append(out,
				foldedChar{ch: 's', bytePos: bytePos, byteEnd: byteEnd, boundary: bnd},
				foldedChar{ch: 's', bytePos: bytePos, byteEnd: byteEnd, boundary: false},
			)
			prevFolded = 's'
		case '\u0130': // Latin capital letter I with dot above (İ)
			out = append(out, foldedChar{ch: 'i', bytePos: bytePos, byteEnd: byteEnd, boundary: bnd})
			prevFolded = 'i'
		case '\u212A': // Kelvin sign (K)
			out = append(out, foldedChar{ch: 'k', bytePos: bytePos, byteEnd: byteEnd, boundary: bnd})
			prevFolded = 'k'
		case '\u017F': // Latin small letter long s (ſ)
			out = append(out, foldedChar{ch: 's', bytePos: bytePos, byteEnd: byteEnd, boundary: bnd})
			prevFolded = 's'
		default:
			lr := unicode.ToLower(r)
			out = append(out, foldedChar{ch: lr, bytePos: bytePos, byteEnd: byteEnd, boundary: bnd})
			prevFolded = lr
		}
		prev = r
	}
	return out
}

func expandFoldedNeedleRunes(s string, skipSpace bool) ([]rune, bool) {
	out := make([]rune, 0, len(s))
	allASCIIAlnum := true
	var prevFolded rune
	for _, r := range s {
		if skipSpace && unicode.IsSpace(r) {
			continue
		}
		if r == '\u0307' && prevFolded == 'i' {
			continue
		}
		switch r {
		case 'ß', 'ẞ':
			out = append(out, 's', 's')
			prevFolded = 's'
		case '\u0130':
			out = append(out, 'i')
			prevFolded = 'i'
		case '\u212A':
			out = append(out, 'k')
			prevFolded = 'k'
		case '\u017F':
			out = append(out, 's')
			prevFolded = 's'
		default:
			lr := unicode.ToLower(r)
			out = append(out, lr)
			prevFolded = lr
			if lr >= utf8.RuneSelf || (!unicode.IsLetter(lr) && !unicode.IsDigit(lr)) {
				allASCIIAlnum = false
			}
		}
	}
	return out, allASCIIAlnum
}

func foldString(s string) string {
	if IsASCII(s) {
		return asciiLower(s)
	}
	runes, _ := expandFoldedNeedleRunes(s, false)
	return string(runes)
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

// FindFoldRanges returns all non-overlapping case-folded byte ranges [start, end)
// of needle in text, guaranteed to align with valid UTF-8 rune boundaries in text.
func FindFoldRanges(text, needle string) [][2]int {
	if needle == "" || text == "" {
		return nil
	}
	if IsASCII(text) && IsASCII(needle) {
		lowText := asciiLower(text)
		lowNeedle := asciiLower(needle)
		if len(lowText) < len(lowNeedle) {
			return nil
		}
		var out [][2]int
		offset := 0
		for offset <= len(lowText)-len(lowNeedle) {
			rel := strings.Index(lowText[offset:], lowNeedle)
			if rel < 0 {
				break
			}
			start := offset + rel
			end := start + len(lowNeedle)
			out = append(out, [2]int{start, end})
			offset = end
		}
		return out
	}

	nRunes, _ := expandFoldedNeedleRunes(needle, false)
	if len(nRunes) == 0 {
		return nil
	}
	hay := expandFolded(text)
	if len(hay) < len(nRunes) {
		return nil
	}
	var out [][2]int
	for i := 0; i <= len(hay)-len(nRunes); {
		ok := true
		for j := range nRunes {
			if hay[i+j].ch != nRunes[j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{hay[i].bytePos, hay[i+len(nRunes)-1].byteEnd})
			i += len(nRunes)
		} else {
			i++
		}
	}
	return MergeRanges(out)
}

// SubsequenceScore computes a fuzzy subsequence match score of needle inside haystack.
// Whitespace in needle is ignored. Returns 0 when needle does not match as a subsequence
// or is rejected by the anti-noise guard.
func SubsequenceScore(haystack, needle string) int {
	sc, _, ok := matchSubsequence(haystack, needle, false)
	if !ok {
		return 0
	}
	return sc
}

// SubsequenceRanges returns merged UTF-8 byte ranges [start, end) in text for the
// best fuzzy subsequence match of needle. Returns nil if no match is found.
func SubsequenceRanges(text, needle string) [][2]int {
	_, ranges, ok := matchSubsequence(text, needle, true)
	if !ok {
		return nil
	}
	return ranges
}

// HighlightRanges returns merged UTF-8 byte ranges in text matching query:
// exact substring first, then whitespace/delimiter token substrings (with per-token
// fuzzy fallback when needed), then fuzzy subsequence.
func HighlightRanges(text, query string) [][2]int {
	q := strings.TrimSpace(query)
	if q == "" || text == "" {
		return nil
	}
	if exact := FindFoldRanges(text, q); len(exact) > 0 {
		return exact
	}
	if toks := strings.Fields(q); len(toks) > 1 {
		if ranges := highlightTokens(text, toks); len(ranges) > 0 {
			return ranges
		}
	}
	if ranges := SubsequenceRanges(text, q); len(ranges) > 0 {
		return ranges
	}
	if hasQueryDelimiter(q) {
		if normToks := splitDelimiterTokens(q); len(normToks) > 0 {
			if ranges := highlightTokens(text, normToks); len(ranges) > 0 {
				return ranges
			}
			if ranges := SubsequenceRanges(text, strings.Join(normToks, "")); len(ranges) > 0 {
				return ranges
			}
		}
	}
	return nil
}

func highlightTokens(text string, tokens []string) [][2]int {
	var all [][2]int
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if r := FindFoldRanges(text, tok); len(r) > 0 {
			all = append(all, r...)
		} else if r := SubsequenceRanges(text, tok); len(r) > 0 {
			all = append(all, r...)
		}
	}
	return MergeRanges(all)
}

// MergeRanges sorts and merges overlapping or adjacent byte ranges [start, end).
func MergeRanges(ranges [][2]int) [][2]int {
	if len(ranges) <= 1 {
		if len(ranges) == 1 && ranges[0][0] >= ranges[0][1] {
			return nil
		}
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

// ScoreFields is a convenience wrapper for Score with one primary field and optional secondary fields,
// allowing fuzzy subsequence matching across secondary fields as well.
func ScoreFields(query, primary string, secondary ...string) (int, bool) {
	return Score(query, []string{primary}, secondary, true)
}

// Score evaluates how well query matches primary and secondary candidate texts.
// Score bands are strictly separated so weaker tiers can never outrank stronger tiers:
//   - exact match on primary:              10000
//   - prefix match on primary:             8000..8500
//   - substring match on primary:          5600..6120
//   - substring match on secondary:        4500..4800
//   - multi-token match across fields:     3500..4400
//   - delimiter-stripped compact match:    3000..3400
//   - fuzzy subsequence match on primary:  2000..2900
//   - delimiter-normalized token match:    1400..1950
//   - fuzzy subsequence across combined:   1000..1350
func Score(query string, primary, secondary []string, fuzzySecondary bool) (int, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return 1, true
	}

	foldedQ := foldString(q)
	if foldedQ == "" {
		return 0, false
	}

	if sc, ok := scoreCore(foldedQ, primary, secondary, fuzzySecondary); ok {
		return sc, true
	}

	// Delimiter-tolerant fallback: compare delimiter-stripped compact forms first so
	// "gpt-54" ranks "GPT 5.4" (compact exact "gpt54") above "gpt separate 54".
	if hasQueryDelimiter(foldedQ) {
		normToks := splitDelimiterTokens(foldedQ)
		if len(normToks) > 0 {
			compactQ := strings.Join(normToks, "")
			if compactQ != "" {
				if sc, ok := scoreCompactDelimiterMatch(compactQ, primary); ok {
					return sc, true
				}
			}
			normQ := strings.Join(normToks, " ")
			if normQ != foldedQ {
				if sc, ok := scoreCore(normQ, primary, secondary, fuzzySecondary); ok {
					return 1400 + min(sc/20, 550), true
				}
			}
			if compactQ != "" && compactQ != foldedQ && compactQ != normQ {
				if sc, ok := scoreCore(compactQ, primary, secondary, fuzzySecondary); ok {
					return 1200 + min(sc/25, 350), true
				}
			}
		}
	}

	return 0, false
}

func stripDelimiters(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range foldString(s) {
		if !isQueryDelimiter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func scoreCompactDelimiterMatch(compactQ string, primary []string) (int, bool) {
	best := 0
	for _, p := range primary {
		if p == "" {
			continue
		}
		cp := stripDelimiters(p)
		if cp == "" {
			continue
		}
		if cp == compactQ {
			best = max(best, 3400)
			continue
		}
		if strings.HasPrefix(cp, compactQ) {
			bonus := max(0, 150-(utf8.RuneCountInString(cp)-utf8.RuneCountInString(compactQ))*4)
			best = max(best, 3200+bonus)
			continue
		}
		if idx := strings.Index(cp, compactQ); idx >= 0 {
			runeIdx := utf8.RuneCountInString(cp[:idx])
			best = max(best, 3050-min(runeIdx*3, 100))
		}
	}
	if best > 0 {
		return best, true
	}
	return 0, false
}

func scoreCore(q string, primary, secondary []string, fuzzySecondary bool) (int, bool) {
	best := 0
	matched := false
	update := func(sc int) {
		if !matched || sc > best {
			best = sc
			matched = true
		}
	}

	rawPrimary := make([]string, 0, len(primary))
	lowPrimary := make([]string, 0, len(primary))
	qRunes := utf8.RuneCountInString(q)

	for _, p := range primary {
		if p == "" {
			continue
		}
		lp := foldString(p)
		rawPrimary = append(rawPrimary, p)
		lowPrimary = append(lowPrimary, lp)
		if lp == q {
			update(10000)
			continue
		}
		if strings.HasPrefix(lp, q) {
			bonus := max(0, 500-(utf8.RuneCountInString(lp)-qRunes)*4)
			update(8000 + bonus)
			continue
		}
		if idx := strings.Index(lp, q); idx >= 0 {
			runeIdx := utf8.RuneCountInString(lp[:idx])
			posPenalty := min(runeIdx*3, 400)
			boundaryBonus := 0
			src := lp
			if IsASCII(p) && len(lp) == len(p) {
				src = p
			}
			if isByteBoundary(src, idx) {
				boundaryBonus = 120
			}
			update(6000 + boundaryBonus - posPenalty)
		}
	}
	if matched {
		return best, true
	}

	rawSecondary := make([]string, 0, len(secondary))
	lowSecondary := make([]string, 0, len(secondary))
	for _, s := range secondary {
		if s == "" {
			continue
		}
		ls := foldString(s)
		rawSecondary = append(rawSecondary, s)
		lowSecondary = append(lowSecondary, ls)
		if idx := strings.Index(ls, q); idx >= 0 {
			runeIdx := utf8.RuneCountInString(ls[:idx])
			posPenalty := min(runeIdx*2, 300)
			update(4800 - posPenalty)
		}
	}
	if matched {
		return best, true
	}

	tokens := strings.Fields(q)
	if len(tokens) > 1 {
		// Deduplicate repeated tokens so repeating "a a a ..." cannot inflate or slow scoring.
		uniqTokens := make([]string, 0, len(tokens))
		seenTok := make(map[string]bool, len(tokens))
		for _, tok := range tokens {
			if tok == "" || seenTok[tok] {
				continue
			}
			seenTok[tok] = true
			uniqTokens = append(uniqTokens, tok)
		}
		allTokens := len(uniqTokens) > 0
		sumTok := 0
		for _, tok := range uniqTokens {
			tokBest := 0
			for i, lp := range lowPrimary {
				switch {
				case lp == tok:
					tokBest = max(tokBest, 120)
				case strings.HasPrefix(lp, tok):
					tokBest = max(tokBest, 95)
				case strings.Contains(lp, tok):
					tokBest = max(tokBest, 70)
				default:
					if sc := SubsequenceScore(rawPrimary[i], tok); sc > 0 {
						tokBest = max(tokBest, min(sc/3+20, 55))
					}
				}
			}
			if tokBest == 0 {
				for i, ls := range lowSecondary {
					if strings.Contains(ls, tok) {
						tokBest = max(tokBest, 40)
					} else if fuzzySecondary {
						if sc := SubsequenceScore(rawSecondary[i], tok); sc > 0 {
							tokBest = max(tokBest, min(sc/5+10, 30))
						}
					}
				}
			}
			if tokBest == 0 {
				allTokens = false
				break
			}
			sumTok += tokBest
		}
		if allTokens {
			// Strictly bounded in [3500, 4400] so multi-token scores never outrank substring/exact matches.
			avgTok := sumTok / len(uniqTokens)
			return 3500 + min(avgTok*7, 900), true
		}
	}

	bestFuzzyPrimary := 0
	for _, p := range rawPrimary {
		if sc := SubsequenceScore(p, q); sc > bestFuzzyPrimary {
			bestFuzzyPrimary = sc
		}
	}
	if bestFuzzyPrimary > 0 {
		return 2000 + min(bestFuzzyPrimary, 900), true
	}

	if fuzzySecondary && len(rawSecondary) > 0 {
		var combined strings.Builder
		for _, p := range rawPrimary {
			if combined.Len() > 0 {
				combined.WriteByte(' ')
			}
			combined.WriteString(p)
		}
		for _, s := range rawSecondary {
			if combined.Len() > 0 {
				combined.WriteByte(' ')
			}
			combined.WriteString(s)
		}
		if sc := SubsequenceScore(combined.String(), q); sc > 0 {
			return 1000 + min(sc, 350), true
		}
	}

	return 0, false
}

func isQueryDelimiter(r rune) bool {
	return unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == '/'
}

func hasQueryDelimiter(s string) bool {
	for _, r := range s {
		if r == '-' || r == '_' || r == '.' || r == '/' {
			return true
		}
	}
	return false
}

func splitDelimiterTokens(s string) []string {
	return strings.FieldsFunc(s, isQueryDelimiter)
}

func isByteBoundary(s string, byteIdx int) bool {
	if byteIdx <= 0 || byteIdx >= len(s) {
		return byteIdx == 0
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:byteIdx])
	curr, _ := utf8.DecodeRuneInString(s[byteIdx:])
	return isRuneBoundary(prev, curr)
}

func isRuneBoundary(prev, curr rune) bool {
	if unicode.IsSpace(prev) || prev == '-' || prev == '_' || prev == '/' || prev == ':' || prev == '.' || prev == '(' || prev == '[' {
		return true
	}
	// Non-ASCII ideographs/scripts act as natural word boundaries.
	if curr >= utf8.RuneSelf && unicode.IsLetter(curr) {
		return true
	}
	// CamelCase boundary: lower -> upper
	if unicode.IsLower(prev) && unicode.IsUpper(curr) {
		return true
	}
	// Digit/letter transition
	if (unicode.IsLetter(prev) && unicode.IsDigit(curr)) || (unicode.IsDigit(prev) && unicode.IsLetter(curr)) {
		return true
	}
	return false
}

const (
	maxDPNeedle   = 48
	maxDPHay      = 256
	maxGapPenalty = 12
	consecBonus   = 18
)

func passesAntiNoiseGuard(hay []foldedChar, matchedIdx []int, needleASCIIAlnum bool) bool {
	nLen := len(matchedIdx)
	if !needleASCIIAlnum || nLen < 2 {
		return true
	}
	boundaryHits := 0
	consecHits := 0
	for i, idx := range matchedIdx {
		if hay[idx].boundary {
			boundaryHits++
		}
		if i > 0 && idx == matchedIdx[i-1]+1 {
			consecHits++
		}
	}
	span := matchedIdx[nLen-1] - matchedIdx[0] + 1
	if boundaryHits == 0 && consecHits == 0 {
		if (nLen == 2 && span > 6) || (nLen >= 3 && span > nLen*4) {
			return false
		}
	}
	return true
}

// findTightestMatch finds a minimal-span subsequence match in hay for nRunes so the
// anti-noise guard never rejects a string that genuinely contains a compact occurrence.
func findTightestMatch(hay []foldedChar, nRunes []rune) ([]int, bool) {
	nLen := len(nRunes)
	hLen := len(hay)
	bestSpan := hLen + 1
	var best []int

	for end := nLen - 1; end < hLen; end++ {
		if hay[end].ch != nRunes[nLen-1] {
			continue
		}
		cur := make([]int, nLen)
		cur[nLen-1] = end
		ni := nLen - 2
		pos := end - 1
		for ni >= 0 && pos >= 0 {
			if hay[pos].ch == nRunes[ni] {
				cur[ni] = pos
				ni--
			}
			pos--
		}
		if ni < 0 {
			span := cur[nLen-1] - cur[0] + 1
			if span < bestSpan {
				bestSpan = span
				best = cur
			}
		}
	}
	return best, best != nil
}

func scoreMatchedPath(hay []foldedChar, matchedIdx []int) int {
	nLen := len(matchedIdx)
	if nLen == 0 {
		return 0
	}
	first := matchedIdx[0]
	total := 10
	if first == 0 {
		total += 20
	} else if hay[first].boundary {
		total += 14
	} else {
		total -= min(first/2, 6)
	}
	for i := 1; i < nLen; i++ {
		prev := matchedIdx[i-1]
		curr := matchedIdx[i]
		if curr == prev+1 {
			total += 10 + consecBonus
			if hay[curr].boundary {
				total += 6
			}
		} else {
			gap := min(curr-prev-1, maxGapPenalty)
			total += 10 - gap
			if hay[curr].boundary {
				total += 14
			}
		}
	}
	return max(1, total-min((len(hay)-nLen)/4, 20))
}

func matchSubsequence(text, needle string, wantRanges bool) (int, [][2]int, bool) {
	nRunes, needleASCIIAlnum := expandFoldedNeedleRunes(needle, true)
	nLen := len(nRunes)
	if nLen == 0 || text == "" {
		return 0, nil, false
	}

	// Fast zero-allocation pre-check for pure ASCII text & needle.
	if IsASCII(text) && IsASCII(needle) {
		ni := 0
		for i := range len(text) {
			c := text[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if rune(c) == nRunes[ni] {
				ni++
				if ni == nLen {
					break
				}
			}
		}
		if ni < nLen {
			return 0, nil, false
		}
	}

	hay := expandFolded(text)
	hLen := len(hay)
	if nLen > hLen {
		return 0, nil, false
	}

	// Subsequence pre-check for non-ASCII paths.
	if !IsASCII(text) || !IsASCII(needle) {
		ni := 0
		for _, hc := range hay {
			if hc.ch == nRunes[ni] {
				ni++
				if ni == nLen {
					break
				}
			}
		}
		if ni < nLen {
			return 0, nil, false
		}
	}

	var matchedIdx []int
	var score int

	// Exact linear-edge-weight DP: satisfies strict optimal substructure so the
	// result is mathematically identical to exhaustive global search over all subsequences.
	if nLen <= maxDPNeedle && hLen <= maxDPHay {
		const negInf = -1 << 29
		prevScore := make([]int, hLen)
		currScore := make([]int, hLen)
		parent := make([]int16, nLen*hLen)
		for idx := range parent {
			parent[idx] = -1
		}

		for j := 0; j < hLen; j++ {
			if hay[j].ch == nRunes[0] {
				sc := 10
				if j == 0 {
					sc += 20
				} else if hay[j].boundary {
					sc += 14
				} else {
					sc -= min(j/2, 6)
				}
				prevScore[j] = sc
			} else {
				prevScore[j] = negInf
			}
		}

		for i := 1; i < nLen; i++ {
			for j := range currScore {
				currScore[j] = negInf
			}
			bestFarVal := negInf
			bestFarIdx := -1
			for j := i; j < hLen; j++ {
				if farK := j - maxGapPenalty - 2; farK >= i-1 && prevScore[farK] > negInf {
					if prevScore[farK] >= bestFarVal {
						bestFarVal = prevScore[farK]
						bestFarIdx = farK
					}
				}
				if hay[j].ch != nRunes[i] {
					continue
				}

				bestJ := negInf
				bestPar := -1

				// Case 1: consecutive match from j-1.
				if prevScore[j-1] > negInf {
					sc := prevScore[j-1] + 10 + consecBonus
					if hay[j].boundary {
						sc += 6
					}
					bestJ = sc
					bestPar = j - 1
				}

				// Case 2: near non-contiguous match with exact gap penalty 1..maxGapPenalty.
				// Break ties in favor of larger k (tighter span).
				nearStart := max(i-1, j-maxGapPenalty-1)
				for k := nearStart; k < j-1; k++ {
					if prevScore[k] <= negInf {
						continue
					}
					gap := j - k - 1
					sc := prevScore[k] + 10 - gap
					if hay[j].boundary {
						sc += 14
					}
					if sc > bestJ || (sc == bestJ && k > bestPar) {
						bestJ = sc
						bestPar = k
					}
				}

				// Case 3: far non-contiguous match with capped gap penalty.
				if bestFarIdx >= 0 && bestFarVal > negInf {
					sc := bestFarVal + 10 - maxGapPenalty
					if hay[j].boundary {
						sc += 14
					}
					if sc > bestJ || (sc == bestJ && bestFarIdx > bestPar) {
						bestJ = sc
						bestPar = bestFarIdx
					}
				}

				if bestJ > negInf {
					currScore[j] = bestJ
					parent[i*hLen+j] = int16(bestPar)
				}
			}
			prevScore, currScore = currScore, prevScore
		}

		bestEnd := -1
		bestTotal := negInf
		for j := nLen - 1; j < hLen; j++ {
			if prevScore[j] > bestTotal {
				bestTotal = prevScore[j]
				bestEnd = j
			}
		}
		if bestEnd < 0 {
			return 0, nil, false
		}
		score = max(1, bestTotal-min((hLen-nLen)/4, 20))

		matchedIdx = make([]int, nLen)
		cur := bestEnd
		for i := nLen - 1; i >= 0; i-- {
			matchedIdx[i] = cur
			if i > 0 {
				cur = int(parent[i*hLen+cur])
			}
		}
	} else {
		// Greedy fallback for very long strings.
		hi := 0
		matchedIdx = make([]int, 0, nLen)
		for _, nr := range nRunes {
			found := false
			for hi < hLen {
				if hay[hi].ch == nr {
					found = true
					matchedIdx = append(matchedIdx, hi)
					hi++
					break
				}
				hi++
			}
			if !found {
				return 0, nil, false
			}
		}
		score = scoreMatchedPath(hay, matchedIdx)
	}

	if !passesAntiNoiseGuard(hay, matchedIdx, needleASCIIAlnum) {
		if tight, ok := findTightestMatch(hay, nRunes); ok && passesAntiNoiseGuard(hay, tight, needleASCIIAlnum) {
			matchedIdx = tight
			score = scoreMatchedPath(hay, tight)
		} else {
			return 0, nil, false
		}
	}

	if !wantRanges {
		return score, nil, true
	}

	ranges := make([][2]int, 0, nLen)
	for _, idx := range matchedIdx {
		r := [2]int{hay[idx].bytePos, hay[idx].byteEnd}
		if len(ranges) > 0 && r[0] <= ranges[len(ranges)-1][1] {
			if r[1] > ranges[len(ranges)-1][1] {
				ranges[len(ranges)-1][1] = r[1]
			}
		} else {
			ranges = append(ranges, r)
		}
	}
	return score, ranges, true
}
