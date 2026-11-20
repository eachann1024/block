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
	low := strings.ToLower(text)
	for _, tok := range tokens {
		if tok != "" && !strings.Contains(low, tok) {
			return false
		}
	}
	return true
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
// exact substring first, then whitespace/delimiter token substrings, then fuzzy subsequence.
func HighlightRanges(text, query string) [][2]int {
	q := strings.TrimSpace(query)
	if q == "" || text == "" {
		return nil
	}
	if exact := findCaseInsensitiveRanges(text, q); len(exact) > 0 {
		return exact
	}
	if multi := findTokenRanges(text, strings.Fields(q)); len(multi) > 0 {
		return multi
	}
	if ranges := SubsequenceRanges(text, q); len(ranges) > 0 {
		return ranges
	}
	if hasQueryDelimiter(q) {
		if normToks := splitDelimiterTokens(q); len(normToks) > 0 {
			if multi := findTokenRanges(text, normToks); len(multi) > 0 {
				return multi
			}
			if ranges := SubsequenceRanges(text, strings.Join(normToks, "")); len(ranges) > 0 {
				return ranges
			}
		}
	}
	return nil
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
// Higher scores represent better matches:
//   - exact match on primary (10000)
//   - prefix match on primary (8000+)
//   - substring match on primary (6000+)
//   - substring match on secondary (4500+)
//   - multi-token match across fields (3500+)
//   - fuzzy subsequence match on primary (2000+)
//   - fuzzy subsequence match across combined fields when fuzzySecondary is true (1000+)
//   - delimiter-normalized fallback when query contains '-', '_', '.', '/' (800+)
func Score(query string, primary, secondary []string, fuzzySecondary bool) (int, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return 1, true
	}

	if sc, ok := scoreCore(q, primary, secondary, fuzzySecondary); ok {
		return sc, true
	}

	// Delimiter-tolerant fallback: treat '-', '_', '.', '/' as word separators so queries like
	// "gpt-54" match "GPT 5.4", "jetbrains-mono" matches "JetBrains Mono", etc.
	if hasQueryDelimiter(q) {
		normToks := splitDelimiterTokens(q)
		if len(normToks) > 0 {
			normQ := strings.Join(normToks, " ")
			if normQ != q {
				if sc, ok := scoreCore(normQ, primary, secondary, fuzzySecondary); ok {
					return max(1, min(sc/2, 2400)), true
				}
			}
			compactQ := strings.Join(normToks, "")
			if compactQ != q && compactQ != normQ {
				if sc, ok := scoreCore(compactQ, primary, secondary, fuzzySecondary); ok {
					return max(1, min(sc/2-100, 2200)), true
				}
			}
		}
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
		lp := strings.ToLower(p)
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
			if len(lp) == len(p) {
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
		ls := strings.ToLower(s)
		rawSecondary = append(rawSecondary, s)
		lowSecondary = append(lowSecondary, ls)
		if idx := strings.Index(ls, q); idx >= 0 {
			runeIdx := utf8.RuneCountInString(ls[:idx])
			posPenalty := min(runeIdx*2, 300)
			update(4500 - posPenalty)
		}
	}
	if matched {
		return best, true
	}

	tokens := strings.Fields(q)
	if len(tokens) > 1 {
		allTokens := true
		tokenScore := 3500
		for _, tok := range tokens {
			tokBest := 0
			for i, lp := range lowPrimary {
				switch {
				case lp == tok:
					tokBest = max(tokBest, 120)
				case strings.HasPrefix(lp, tok):
					tokBest = max(tokBest, 90)
				case strings.Contains(lp, tok):
					tokBest = max(tokBest, 60)
				default:
					if sc := SubsequenceScore(rawPrimary[i], tok); sc > 0 {
						tokBest = max(tokBest, min(sc/2+15, 50))
					}
				}
			}
			if tokBest == 0 {
				for i, ls := range lowSecondary {
					if strings.Contains(ls, tok) {
						tokBest = max(tokBest, 35)
					} else if fuzzySecondary {
						if sc := SubsequenceScore(rawSecondary[i], tok); sc > 0 {
							tokBest = max(tokBest, min(sc/4+10, 28))
						}
					}
				}
			}
			if tokBest == 0 {
				allTokens = false
				break
			}
			tokenScore += tokBest
		}
		if allTokens {
			return tokenScore, true
		}
	}

	bestFuzzyPrimary := 0
	for _, p := range rawPrimary {
		if sc := SubsequenceScore(p, q); sc > bestFuzzyPrimary {
			bestFuzzyPrimary = sc
		}
	}
	if bestFuzzyPrimary > 0 {
		return 2000 + bestFuzzyPrimary, true
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
			return 1000 + sc, true
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

func findTokenRanges(text string, tokens []string) [][2]int {
	if len(tokens) <= 1 {
		return nil
	}
	var all [][2]int
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		all = append(all, findCaseInsensitiveRanges(text, tok)...)
	}
	return MergeRanges(all)
}

func findCaseInsensitiveRanges(text, needle string) [][2]int {
	q := strings.ToLower(needle)
	if q == "" || text == "" {
		return nil
	}
	low := strings.ToLower(text)
	if len(low) != len(text) {
		// Fall back to rune-indexed matching for rare Unicode case-folding length changes.
		return findCaseInsensitiveRuneRanges(text, q)
	}
	if len(low) < len(q) {
		return nil
	}
	var out [][2]int
	offset := 0
	for offset <= len(low)-len(q) {
		rel := strings.Index(low[offset:], q)
		if rel < 0 {
			break
		}
		start := offset + rel
		end := start + len(q)
		if (start == 0 || utf8.RuneStart(text[start])) && (end == len(text) || utf8.RuneStart(text[end])) {
			out = append(out, [2]int{start, end})
			offset = end
		} else {
			_, sz := utf8.DecodeRuneInString(low[start:])
			if sz <= 0 {
				sz = 1
			}
			offset = start + sz
		}
	}
	return out
}

func findCaseInsensitiveRuneRanges(text, lowerNeedle string) [][2]int {
	nRunes := []rune(lowerNeedle)
	if len(nRunes) == 0 {
		return nil
	}
	type runeSpan struct {
		lower rune
		start int
		end   int
	}
	hay := make([]runeSpan, 0, utf8.RuneCountInString(text))
	for pos, r := range text {
		sz := utf8.RuneLen(r)
		if sz < 0 {
			sz = 1
		}
		hay = append(hay, runeSpan{lower: unicode.ToLower(r), start: pos, end: pos + sz})
	}
	if len(hay) < len(nRunes) {
		return nil
	}
	var out [][2]int
	for i := 0; i <= len(hay)-len(nRunes); {
		ok := true
		for j := range nRunes {
			if hay[i+j].lower != nRunes[j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{hay[i].start, hay[i+len(nRunes)-1].end})
			i += len(nRunes)
		} else {
			i++
		}
	}
	return out
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

func compactNonSpaceLowerRunes(s string) ([]rune, bool) {
	out := make([]rune, 0, len(s))
	allASCIIAlnum := true
	for _, r := range s {
		if !unicode.IsSpace(r) {
			lr := unicode.ToLower(r)
			out = append(out, lr)
			if lr >= utf8.RuneSelf || (!unicode.IsLetter(lr) && !unicode.IsDigit(lr)) {
				allASCIIAlnum = false
			}
		}
	}
	return out, allASCIIAlnum
}

const (
	maxDPNeedle   = 48
	maxDPHay      = 256
	maxGapPenalty = 12
)

func matchSubsequence(text, needle string, wantRanges bool) (int, [][2]int, bool) {
	nRunes, needleASCIIAlnum := compactNonSpaceLowerRunes(needle)
	nLen := len(nRunes)
	if nLen == 0 || text == "" {
		return 0, nil, false
	}

	// Fast subsequence pre-check with zero heap allocation on miss.
	ni := 0
	for _, r := range text {
		if unicode.ToLower(r) == nRunes[ni] {
			ni++
			if ni == nLen {
				break
			}
		}
	}
	if ni < nLen {
		return 0, nil, false
	}

	type hayRune struct {
		lower    rune
		bytePos  int
		byteEnd  int
		boundary bool
	}
	hay := make([]hayRune, 0, utf8.RuneCountInString(text))
	var prev rune
	for bytePos, r := range text {
		sz := utf8.RuneLen(r)
		if sz < 0 {
			sz = 1
		}
		hay = append(hay, hayRune{
			lower:    unicode.ToLower(r),
			bytePos:  bytePos,
			byteEnd:  bytePos + sz,
			boundary: bytePos == 0 || isRuneBoundary(prev, r),
		})
		prev = r
	}
	hLen := len(hay)
	if nLen > hLen {
		return 0, nil, false
	}

	var matchedIdx []int
	var score int

	// Exact DP for typical UI strings so word-boundary/contiguous matches win cleanly
	// (e.g. "jbm" -> "JetBrains Mono", "sonnet" -> "claude-sonnet-4-5").
	if nLen <= maxDPNeedle && hLen <= maxDPHay {
		const negInf = -1 << 29
		prevScore := make([]int, hLen)
		currScore := make([]int, hLen)
		prevConsec := make([]int, hLen)
		currConsec := make([]int, hLen)
		parent := make([]int16, nLen*hLen)
		for idx := range parent {
			parent[idx] = -1
		}

		for j := 0; j < hLen; j++ {
			if hay[j].lower == nRunes[0] {
				sc := 10
				if j == 0 {
					sc += 20
				} else if hay[j].boundary {
					sc += 14
				} else {
					sc -= min(j, 8)
				}
				prevScore[j] = sc
				prevConsec[j] = 1
			} else {
				prevScore[j] = negInf
			}
		}

		for i := 1; i < nLen; i++ {
			for j := range currScore {
				currScore[j] = negInf
				currConsec[j] = 0
			}
			// bestFarVal tracks best prevScore[k] where gap (j - k - 1) > maxGapPenalty (fixed -maxGapPenalty).
			bestFarVal := negInf
			bestFarIdx := -1
			for j := i; j < hLen; j++ {
				if farK := j - maxGapPenalty - 2; farK >= i-1 && prevScore[farK] > bestFarVal {
					bestFarVal = prevScore[farK]
					bestFarIdx = farK
				}
				if hay[j].lower != nRunes[i] {
					continue
				}

				bestJ := negInf
				bestConsec := 1
				bestPar := -1

				// Case 1: consecutive match from j-1.
				if prevScore[j-1] > negInf {
					c := prevConsec[j-1] + 1
					sc := prevScore[j-1] + 10 + min(c, 8)*8
					if hay[j].boundary {
						sc += 6
					}
					if sc > bestJ {
						bestJ = sc
						bestConsec = c
						bestPar = j - 1
					}
				}

				// Case 2: near non-contiguous match with exact gap penalty 1..maxGapPenalty.
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
					if sc > bestJ {
						bestJ = sc
						bestConsec = 1
						bestPar = k
					}
				}

				// Case 3: far non-contiguous match with capped gap penalty.
				if bestFarIdx >= 0 && bestFarVal > negInf {
					sc := bestFarVal + 10 - maxGapPenalty
					if hay[j].boundary {
						sc += 14
					}
					if sc > bestJ {
						bestJ = sc
						bestConsec = 1
						bestPar = bestFarIdx
					}
				}

				if bestJ > negInf {
					currScore[j] = bestJ
					currConsec[j] = bestConsec
					parent[i*hLen+j] = int16(bestPar)
				}
			}
			prevScore, currScore = currScore, prevScore
			prevConsec, currConsec = currConsec, prevConsec
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
		consec := 0
		matchedIdx = make([]int, 0, nLen)
		for _, nr := range nRunes {
			found := false
			for hi < hLen {
				if hay[hi].lower == nr {
					found = true
					consec++
					score += 10 + min(consec, 8)*4
					if hay[hi].boundary {
						score += 8
					}
					matchedIdx = append(matchedIdx, hi)
					hi++
					break
				}
				consec = 0
				hi++
			}
			if !found {
				return 0, nil, false
			}
		}
		score = max(1, score)
	}

	// Anti-noise guard: reject widely scattered mid-word ASCII hits with no word-boundary
	// or consecutive anchor (e.g. "eyg" matching "v[e]ry ... quarterl[y] ... en[g]ineering").
	if needleASCIIAlnum && nLen >= 2 {
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
				return 0, nil, false
			}
		}
	}

	if !wantRanges {
		return score, nil, true
	}

	ranges := make([][2]int, 0, nLen)
	for _, idx := range matchedIdx {
		r := [2]int{hay[idx].bytePos, hay[idx].byteEnd}
		if len(ranges) > 0 && ranges[len(ranges)-1][1] == r[0] {
			ranges[len(ranges)-1][1] = r[1]
		} else {
			ranges = append(ranges, r)
		}
	}
	return score, ranges, true
}
