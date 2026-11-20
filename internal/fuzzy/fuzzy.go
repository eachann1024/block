package fuzzy

import (
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
// Whitespace in needle is ignored. Returns 0 when needle does not match as a subsequence.
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
func Score(query string, primary, secondary []string, fuzzySecondary bool) (int, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return 1, true
	}

	best := 0
	matched := false
	update := func(sc int) {
		if !matched || sc > best {
			best = sc
			matched = true
		}
	}

	lowPrimary := make([]string, 0, len(primary))
	for _, p := range primary {
		if p == "" {
			continue
		}
		lp := strings.ToLower(p)
		lowPrimary = append(lowPrimary, lp)
		if lp == q {
			update(10000)
			continue
		}
		if strings.HasPrefix(lp, q) {
			bonus := max(0, 500-(utf8.RuneCountInString(lp)-utf8.RuneCountInString(q))*4)
			update(8000 + bonus)
			continue
		}
		if idx := strings.Index(lp, q); idx >= 0 {
			posPenalty := min(idx*2, 400)
			boundaryBonus := 0
			if isByteBoundary(p, idx) {
				boundaryBonus = 120
			}
			update(6000 + boundaryBonus - posPenalty)
		}
	}
	if best >= 6000 {
		return best, true
	}

	lowSecondary := make([]string, 0, len(secondary))
	for _, s := range secondary {
		if s == "" {
			continue
		}
		ls := strings.ToLower(s)
		lowSecondary = append(lowSecondary, ls)
		if idx := strings.Index(ls, q); idx >= 0 {
			posPenalty := min(idx, 300)
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
					if sc := SubsequenceScore(primary[primaryIndexFor(primary, i)], tok); sc > 0 {
						tokBest = max(tokBest, min(sc/2+15, 50))
					}
				}
			}
			if tokBest == 0 {
				for i, ls := range lowSecondary {
					if strings.Contains(ls, tok) {
						tokBest = max(tokBest, 35)
					} else if fuzzySecondary {
						if sc := SubsequenceScore(secondary[primaryIndexFor(secondary, i)], tok); sc > 0 {
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
	for _, p := range primary {
		if p == "" {
			continue
		}
		if sc := SubsequenceScore(p, q); sc > bestFuzzyPrimary {
			bestFuzzyPrimary = sc
		}
	}
	if bestFuzzyPrimary > 0 {
		return 2000 + bestFuzzyPrimary, true
	}

	if fuzzySecondary && len(secondary) > 0 {
		var combined strings.Builder
		for _, p := range primary {
			if p == "" {
				continue
			}
			if combined.Len() > 0 {
				combined.WriteByte(' ')
			}
			combined.WriteString(p)
		}
		for _, s := range secondary {
			if s == "" {
				continue
			}
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

func primaryIndexFor(items []string, nonEmptyIdx int) int {
	seen := 0
	for i, it := range items {
		if it == "" {
			continue
		}
		if seen == nonEmptyIdx {
			return i
		}
		seen++
	}
	return 0
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

func compactNonSpaceLowerRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out = append(out, unicode.ToLower(r))
		}
	}
	return out
}

const (
	maxDPNeedle = 48
	maxDPHay    = 256
)

func matchSubsequence(text, needle string, wantRanges bool) (int, [][2]int, bool) {
	nRunes := compactNonSpaceLowerRunes(needle)
	nLen := len(nRunes)
	if nLen == 0 || text == "" {
		return 0, nil, false
	}

	// Fast ASCII/UTF-8 subsequence pre-check with zero allocation on miss.
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

	// Use exact DP for typical UI strings so word-boundary/contiguous matches win cleanly
	// (e.g. "jbm" -> "JetBrains Mono", "sonnet" -> "claude-sonnet-4-5").
	if nLen <= maxDPNeedle && hLen <= maxDPHay {
		const negInf = -1 << 29
		// dpScore[j] = best score matching nRunes[0..i] ending at hay[j]
		// dpConsec[j] = consecutive match run length ending at hay[j]
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
			// Track best previous match position k < j (non-contiguous)
			bestPrevVal := negInf
			bestPrevIdx := -1
			for j := i; j < hLen; j++ {
				k := j - 1
				if prevScore[k] > negInf {
					// Account for distance from k when scanning forward; we keep the best
					// (prevScore[k] + k) so gap penalty -(j-k-1) can be evaluated in O(1),
					// capped softly by checking direct neighbor separately.
					if prevScore[k] > bestPrevVal {
						bestPrevVal = prevScore[k]
						bestPrevIdx = k
					}
				}
				if hay[j].lower != nRunes[i] {
					continue
				}

				bestJ := negInf
				bestConsec := 1
				bestPar := -1

				// Case 1: consecutive match from j-1
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

				// Case 2: non-contiguous match from best previous k < j-1
				if bestPrevIdx >= 0 && bestPrevIdx < j-1 && bestPrevVal > negInf {
					gap := j - bestPrevIdx - 1
					sc := bestPrevVal + 10 - min(gap, 12)
					if hay[j].boundary {
						sc += 14
					}
					if sc > bestJ {
						bestJ = sc
						bestConsec = 1
						bestPar = bestPrevIdx
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
		// Prefer tighter strings slightly when scores tie.
		score := max(1, bestTotal-min((hLen-nLen)/4, 20))
		if !wantRanges {
			return score, nil, true
		}

		matchedIdx := make([]int, nLen)
		cur := bestEnd
		for i := nLen - 1; i >= 0; i-- {
			matchedIdx[i] = cur
			if i > 0 {
				cur = int(parent[i*hLen+cur])
			}
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

	// Greedy fallback for very long strings.
	hi := 0
	consec := 0
	score := 0
	var ranges [][2]int
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
				if wantRanges {
					r := [2]int{hay[hi].bytePos, hay[hi].byteEnd}
					if len(ranges) > 0 && ranges[len(ranges)-1][1] == r[0] {
						ranges[len(ranges)-1][1] = r[1]
					} else {
						ranges = append(ranges, r)
					}
				}
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
	return max(1, score), ranges, true
}
