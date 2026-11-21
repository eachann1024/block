package editor

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"block/internal/files"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

var lastAnnotID atomic.Uint64

func nextAnnotationID() string {
	now := uint64(time.Now().UnixMilli())
	if now == 0 {
		now = 1
	}
	for {
		prev := lastAnnotID.Load()
		next := now
		if prev >= next {
			next = prev + 1
		}
		if lastAnnotID.CompareAndSwap(prev, next) {
			return strconv.FormatUint(next, 10)
		}
	}
}

func parseAnnotationUint64(id string) uint64 {
	if v, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64); err == nil && v > 0 {
		return v
	}
	return 0
}

func byteOffsetToRuneOffset(s string, byteOff int) int {
	if byteOff <= 0 || s == "" {
		return 0
	}
	if byteOff >= len(s) {
		return utf8.RuneCountInString(s)
	}
	runes := 0
	for i := range s {
		if i >= byteOff {
			return runes
		}
		runes++
	}
	return runes
}

func runeOffsetToByteOffset(s string, runeOff int) int {
	if runeOff <= 0 || s == "" {
		return 0
	}
	runes := 0
	for i := range s {
		if runes == runeOff {
			return i
		}
		runes++
	}
	return len(s)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func findInBlockText(text string, preferredByteStart int, needle string) (startByte, endByte int, ok bool) {
	if needle == "" {
		return 0, 0, false
	}
	if preferredByteStart >= 0 && preferredByteStart+len(needle) <= len(text) &&
		text[preferredByteStart:preferredByteStart+len(needle)] == needle {
		return preferredByteStart, preferredByteStart + len(needle), true
	}
	bestDist := 1 << 30
	bestPos := -1
	offset := 0
	for {
		idx := strings.Index(text[offset:], needle)
		if idx < 0 {
			break
		}
		pos := offset + idx
		dist := absInt(pos - preferredByteStart)
		if dist < bestDist {
			bestDist = dist
			bestPos = pos
		}
		offset = pos + 1
	}
	if bestPos < 0 {
		return 0, 0, false
	}
	return bestPos, bestPos + len(needle), true
}

func findSnippetInDoc(doc *model.Doc, preferredIdx, preferredByteStart int, needle string) (blockIdx, startRune, endRune int, ok bool) {
	return findSnippetInDocExcluding(doc, preferredIdx, preferredByteStart, needle, nil)
}

func findSnippetInDocExcluding(doc *model.Doc, preferredIdx, preferredByteStart int, needle string, exclude map[model.BlockID]bool) (blockIdx, startRune, endRune int, ok bool) {
	if doc == nil || len(doc.Blocks) == 0 || needle == "" {
		return 0, 0, 0, false
	}
	if preferredIdx >= 0 && preferredIdx < len(doc.Blocks) && !exclude[doc.Blocks[preferredIdx].ID] {
		text := doc.Blocks[preferredIdx].PlainText()
		if bs, be, found := findInBlockText(text, preferredByteStart, needle); found {
			return preferredIdx, byteOffsetToRuneOffset(text, bs), byteOffsetToRuneOffset(text, be), true
		}
	}
	bestBlockDist := 1 << 30
	bestStartDist := 1 << 30
	bestIdx := -1
	bestBS, bestBE := 0, 0
	for idx := range doc.Blocks {
		if exclude[doc.Blocks[idx].ID] {
			continue
		}
		text := doc.Blocks[idx].PlainText()
		bs, be, found := findInBlockText(text, preferredByteStart, needle)
		if !found {
			continue
		}
		bDist := absInt(idx - preferredIdx)
		sDist := absInt(bs - preferredByteStart)
		if bDist < bestBlockDist || (bDist == bestBlockDist && sDist < bestStartDist) {
			bestBlockDist = bDist
			bestStartDist = sDist
			bestIdx = idx
			bestBS = bs
			bestBE = be
		}
	}
	if bestIdx < 0 {
		return 0, 0, 0, false
	}
	text := doc.Blocks[bestIdx].PlainText()
	return bestIdx, byteOffsetToRuneOffset(text, bestBS), byteOffsetToRuneOffset(text, bestBE), true
}

func resolveAnchorPos(doc *model.Doc, blockIdx, byteStart, byteEnd int, quote string, exclude map[model.BlockID]bool) (model.BlockID, int, int, bool) {
	if doc == nil || len(doc.Blocks) == 0 {
		return 0, 0, 0, false
	}
	if quote != "" {
		if idx, rs, re, ok := findSnippetInDocExcluding(doc, blockIdx, byteStart, quote, exclude); ok {
			return doc.Blocks[idx].ID, rs, re, true
		}
		if len(exclude) > 0 {
			if idx, rs, re, ok := findSnippetInDocExcluding(doc, blockIdx, byteStart, quote, nil); ok {
				return doc.Blocks[idx].ID, rs, re, true
			}
		}
		return 0, 0, 0, false
	}
	if blockIdx < 0 || blockIdx >= len(doc.Blocks) {
		return 0, 0, 0, false
	}
	text := doc.Blocks[blockIdx].PlainText()
	rLen := utf8.RuneCountInString(text)
	rs := clampInt(byteOffsetToRuneOffset(text, byteStart), 0, rLen)
	re := clampInt(byteOffsetToRuneOffset(text, byteEnd), rs, rLen)
	return doc.Blocks[blockIdx].ID, rs, re, true
}

func storedToModelAnnotation(doc *model.Doc, raw files.StoredAnnotation) *model.Annotation {
	if doc == nil || len(doc.Blocks) == 0 {
		return nil
	}
	applied := false
	if raw.Applied != nil {
		applied = *raw.Applied
	} else if raw.AIRewritten != nil && *raw.AIRewritten != "" {
		applied = true
	}

	var startID, endID model.BlockID
	var startRune, endRune int
	if len(raw.Anchors) >= 2 {
		used := make(map[model.BlockID]bool, len(raw.Anchors))
		for i, a := range raw.Anchors {
			bid, rs, re, ok := resolveAnchorPos(doc, a.BlockIndex, a.Start, a.End, a.Quote, used)
			if !ok {
				return nil
			}
			used[bid] = true
			if i == 0 {
				startID, startRune = bid, rs
			}
			if i == len(raw.Anchors)-1 {
				endID, endRune = bid, re
			}
		}
		if startID == endID && endRune < startRune {
			startRune, endRune = endRune, startRune
		}
	} else {
		bIdx, bStart, bEnd, quote := raw.BlockIndex, raw.Start, raw.End, raw.Quote
		if len(raw.Anchors) == 1 {
			a := raw.Anchors[0]
			bIdx, bStart, bEnd, quote = a.BlockIndex, a.Start, a.End, a.Quote
		}
		if applied && raw.AIRewritten != nil && *raw.AIRewritten != "" {
			if idx, rs, re, ok := findSnippetInDoc(doc, bIdx, bStart, *raw.AIRewritten); ok {
				startID, startRune = doc.Blocks[idx].ID, rs
				endID, endRune = doc.Blocks[idx].ID, re
			}
		}
		if startID == 0 {
			var ok bool
			startID, startRune, endRune, ok = resolveAnchorPos(doc, bIdx, bStart, bEnd, quote, nil)
			if !ok {
				return nil
			}
			endID = startID
		}
	}

	idStr := ""
	if uint64(raw.ID) > 0 {
		idStr = strconv.FormatUint(uint64(raw.ID), 10)
	} else {
		idStr = nextAnnotationID()
	}

	var reactions []string
	if len(raw.Reactions) > 0 {
		reactions = []string{raw.Reactions[0]}
	}
	note := strings.TrimSpace(raw.Body)
	rewrite := ""
	if raw.AIRewritten != nil {
		rewrite = *raw.AIRewritten
	}
	aiErr := ""
	if raw.AIError != nil {
		aiErr = *raw.AIError
	}
	answer := ""
	if raw.AIAnswer != nil {
		answer = *raw.AIAnswer
	}
	var thread []model.AnnotationTurn
	for _, t := range raw.AIThread {
		thread = append(thread, model.AnnotationTurn{Q: t.Q, A: t.A})
	}
	if applied {
		// Applied annotations show no card; their body / rewrite stay in the
		// store (see modelToStoredAnnotation) so the Rust app keeps tracking them.
		note = ""
		rewrite = ""
		aiErr = ""
		answer = ""
		thread = nil
	}

	return &model.Annotation{
		ID:            idStr,
		StartBlockID:  startID,
		StartOffset:   startRune,
		EndBlockID:    endID,
		EndOffset:     endRune,
		QuotedText:    raw.Quote,
		Note:          note,
		Reactions:     reactions,
		RewriteResult: rewrite,
		Answer:        answer,
		Thread:        thread,
		AIError:       aiErr,
		Applied:       applied,
	}
}

func modelToStoredAnnotation(s *Session, ann *model.Annotation) (files.StoredAnnotation, bool) {
	if s.doc == nil || ann == nil {
		return files.StoredAnnotation{}, false
	}
	startIdx := s.blockIndexByID(ann.StartBlockID)
	endIdx := s.blockIndexByID(ann.EndBlockID)
	if startIdx < 0 || endIdx < 0 {
		return files.StoredAnnotation{}, false
	}
	startRune := ann.StartOffset
	endRune := ann.EndOffset
	if startIdx > endIdx || (startIdx == endIdx && startRune > endRune) {
		startIdx, endIdx = endIdx, startIdx
		startRune, endRune = endRune, startRune
	}

	numID := parseAnnotationUint64(ann.ID)
	if numID == 0 {
		numID = parseAnnotationUint64(nextAnnotationID())
	}

	reactions := []string{}
	if len(ann.Reactions) > 0 {
		reactions = []string{ann.Reactions[0]}
	}

	var aiRewritten *string
	if ann.RewriteResult != "" {
		v := ann.RewriteResult
		aiRewritten = &v
	}
	var aiErr *string
	if ann.AIError != "" {
		v := ann.AIError
		aiErr = &v
	}
	var aiAnswer *string
	if ann.Answer != "" {
		v := ann.Answer
		aiAnswer = &v
	}
	applied := ann.Applied
	body := ann.Note
	if applied {
		if raw, ok := s.annotAppliedRaw[ann.ID]; ok {
			if body == "" {
				body = raw.Body
			}
			if aiRewritten == nil {
				aiRewritten = raw.AIRewritten
			}
			if aiErr == nil {
				aiErr = raw.AIError
			}
		}
	}

	startText := s.doc.Blocks[startIdx].PlainText()
	firstStartByte := runeOffsetToByteOffset(startText, startRune)
	var firstEndByte int
	var anchors []files.StoredAnnotationAnchor

	if startIdx == endIdx {
		firstEndByte = runeOffsetToByteOffset(startText, endRune)
	} else {
		firstEndByte = len(startText)
		for idx := startIdx; idx <= endIdx; idx++ {
			txt := s.doc.Blocks[idx].PlainText()
			rLen := utf8.RuneCountInString(txt)
			lo, hi := 0, rLen
			if idx == startIdx {
				lo = clampInt(startRune, 0, rLen)
			}
			if idx == endIdx {
				hi = clampInt(endRune, 0, rLen)
				if hi == 0 && idx > startIdx {
					continue
				}
			}
			bs := runeOffsetToByteOffset(txt, lo)
			be := runeOffsetToByteOffset(txt, hi)
			anchors = append(anchors, files.StoredAnnotationAnchor{
				BlockIndex: idx,
				Start:      bs,
				End:        be,
				Quote:      txt[bs:be],
			})
		}
	}

	return files.StoredAnnotation{
		ID:          files.FlexibleUint64(numID),
		BlockIndex:  startIdx,
		Start:       firstStartByte,
		End:         firstEndByte,
		Quote:       ann.QuotedText,
		Body:        body,
		Anchors:     anchors,
		Reactions:   reactions,
		AIRewritten: aiRewritten,
		AIError:     aiErr,
		AIAnswer:    aiAnswer,
		AIThread:    storedThread(ann.Thread),
		Applied:     &applied,
	}, true
}

// onNotePathChanged follows a rename/move. The store itself is re-keyed by the
// workspace (files.MoveNoteAnnotations covers the note and whole folders); the
// call here is idempotent and keeps sessions without a workspace consistent.
func (s *Session) onNotePathChanged(oldPath, newPath string) {
	if oldPath != "" && newPath != "" && len(s.annotations) > 0 {
		_ = files.MoveNoteAnnotations(s.dataDir, oldPath, newPath)
		s.annotLoadedPath = newPath
		s.persistAnnotations()
		return
	}
	s.annotLoadedPath = ""
	s.ensureAnnotationsLoaded()
}

func storedThread(in []model.AnnotationTurn) []files.StoredAnnotationTurn {
	if len(in) == 0 {
		return nil
	}
	out := make([]files.StoredAnnotationTurn, len(in))
	for i, t := range in {
		out[i] = files.StoredAnnotationTurn{Q: t.Q, A: t.A}
	}
	return out
}

func (s *Session) toggleReactionByID(id, emoji string) {
	if emoji == "" {
		return
	}
	s.ensureAnnotationsLoaded()
	for i, ann := range s.annotations {
		if ann.ID == id {
			s.toggleReactionOnIndex(i, emoji)
			return
		}
	}
}

func (s *Session) dismissAnnotationRewrite(id string) {
	for _, ann := range s.annotations {
		if ann.ID == id {
			if ann.RewriteResult == "" && ann.Answer == "" && ann.AIError == "" {
				return
			}
			s.pushUndo()
			ann.RewriteResult = ""
			ann.Answer = ""
			ann.AIError = ""
			s.persistAnnotations()
			return
		}
	}
}

func (s *Session) ensureAnnotationsLoaded() {
	if s.minimal || s.notePath == "" {
		s.annotLoadedPath = ""
		return
	}
	if s.annotLoadedPath == s.notePath {
		return
	}
	s.annotLoadedPath = s.notePath
	s.annotations = nil
	s.annotAppliedRaw = nil
	s.annotUnresolved = nil
	stored, err := files.LoadNoteAnnotations(s.dataDir, s.notePath)
	s.annotLoadFailed = err != nil
	if err != nil {
		return
	}
	loaded := make([]*model.Annotation, 0, len(stored))
	for _, raw := range stored {
		ann := storedToModelAnnotation(s.doc, raw)
		if ann == nil {
			// Anchor not found in this document (e.g. edited elsewhere): keep the
			// entry verbatim so saving this note never drops it.
			s.annotUnresolved = append(s.annotUnresolved, raw)
			continue
		}
		loaded = append(loaded, ann)
		if ann.Applied {
			if s.annotAppliedRaw == nil {
				s.annotAppliedRaw = make(map[string]files.StoredAnnotation)
			}
			s.annotAppliedRaw[ann.ID] = raw
		}
	}
	s.annotations = loaded
}

func (s *Session) persistAnnotations() {
	if s.minimal || s.notePath == "" || s.doc == nil || s.annotLoadFailed {
		return
	}
	s.annotLoadedPath = s.notePath
	stored := make([]files.StoredAnnotation, 0, len(s.annotations)+len(s.annotUnresolved))
	for _, ann := range s.annotations {
		if item, ok := modelToStoredAnnotation(s, ann); ok {
			stored = append(stored, item)
		}
	}
	stored = append(stored, s.annotUnresolved...)
	_ = files.SaveNoteAnnotations(s.dataDir, s.notePath, stored)
}

func isAnnotationVisible(ann *model.Annotation) bool {
	if ann == nil {
		return false
	}
	if ann.Applied {
		return len(ann.Reactions) > 0
	}
	return true
}

func (s *Session) annotationBlockRange(ann *model.Annotation, blockIdx int) (startRune, endRune int, ok bool) {
	if ann == nil || s.doc == nil || blockIdx < 0 || blockIdx >= len(s.doc.Blocks) {
		return 0, 0, false
	}
	aIdx := s.blockIndexByID(ann.StartBlockID)
	bIdx := s.blockIndexByID(ann.EndBlockID)
	if aIdx < 0 || bIdx < 0 {
		return 0, 0, false
	}
	aOff, bOff := ann.StartOffset, ann.EndOffset
	if aIdx > bIdx || (aIdx == bIdx && aOff > bOff) {
		aIdx, bIdx = bIdx, aIdx
		aOff, bOff = bOff, aOff
	}
	if blockIdx < aIdx || blockIdx > bIdx {
		return 0, 0, false
	}
	rLen := utf8.RuneCountInString(s.doc.Blocks[blockIdx].PlainText())
	lo := 0
	if blockIdx == aIdx {
		lo = clampInt(aOff, 0, rLen)
	}
	hi := rLen
	if blockIdx == bIdx {
		hi = clampInt(bOff, 0, rLen)
	}
	if hi <= lo {
		return 0, 0, false
	}
	return lo, hi, true
}

func (s *Session) splitSpansForHighlights(blockIdx int, blockID model.BlockID, spans []model.Span) []model.Span {
	if len(spans) == 0 {
		return spans
	}
	hasAnn := len(s.annotations) > 0
	matches, _ := s.findMatchesForBlock(blockIdx)
	if !hasAnn && len(matches) == 0 {
		return spans
	}

	var cuts []int
	total := 0
	for _, sp := range spans {
		cuts = append(cuts, total)
		total += utf8.RuneCountInString(sp.Text)
	}
	cuts = append(cuts, total)

	addedCut := false
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		if lo, hi, ok := s.annotationBlockRange(ann, blockIdx); ok {
			cuts = append(cuts, lo, hi)
			addedCut = true
		}
	}
	for _, m := range matches {
		lo := clampInt(m.StartRune, 0, total)
		hi := clampInt(m.EndRune, lo, total)
		if hi > lo {
			cuts = append(cuts, lo, hi)
			addedCut = true
		}
	}
	if !addedCut {
		return spans
	}

	sort.Ints(cuts)
	uniq := cuts[:0]
	for _, c := range cuts {
		if len(uniq) == 0 || uniq[len(uniq)-1] != c {
			uniq = append(uniq, c)
		}
	}

	type spanRun struct {
		s0, s1 int
		sp     model.Span
		runes  []rune
	}
	runs := make([]spanRun, len(spans))
	pos := 0
	for i, sp := range spans {
		r := []rune(sp.Text)
		runs[i] = spanRun{s0: pos, s1: pos + len(r), sp: sp, runes: r}
		pos += len(r)
	}

	out := make([]model.Span, 0, len(uniq))
	ri := 0
	for i := 0; i+1 < len(uniq); i++ {
		c0, c1 := uniq[i], uniq[i+1]
		if c1 <= c0 {
			continue
		}
		for ri < len(runs) && runs[ri].s1 <= c0 {
			ri++
		}
		if ri >= len(runs) {
			break
		}
		cur := runs[ri]
		lo := clampInt(c0-cur.s0, 0, len(cur.runes))
		hi := clampInt(c1-cur.s0, lo, len(cur.runes))
		if hi <= lo {
			continue
		}
		seg := cur.sp
		seg.Text = string(cur.runes[lo:hi])
		out = append(out, seg)
	}
	_ = blockID
	return out
}

func (s *Session) annotationHighlightColor(id model.BlockID, start, end int, pal theme.Palette) (ui.Color, bool) {
	idx := s.blockIndexByID(id)
	if idx < 0 {
		return ui.Transparent, false
	}
	found := false
	active := false
	for _, ann := range s.annotations {
		if !isAnnotationVisible(ann) {
			continue
		}
		lo, hi, ok := s.annotationBlockRange(ann, idx)
		if !ok || end <= lo || start >= hi {
			continue
		}
		found = true
		if ann.ID == s.activeAnnotID || ann.ID == s.hoveredAnnotID {
			active = true
			break
		}
	}
	if !found {
		return ui.Transparent, false
	}
	if active {
		return pal.AnnotationActive, true
	}
	return pal.AnnotationBG, true
}

func (s *Session) findHighlightColor(blockIdx, start, end int, pal theme.Palette) (ui.Color, bool) {
	if !s.findOpen {
		return ui.Transparent, false
	}
	if i := s.findCurrent; i >= 0 && i < len(s.findMatches) {
		m := s.findMatches[i]
		if m.BlockIdx == blockIdx && end > m.StartRune && start < m.EndRune {
			return pal.FindActive, true
		}
	}
	if s.rangeHasFindMatch(blockIdx, start, end) {
		return pal.FindMatch, true
	}
	return ui.Transparent, false
}

func init() {
	_ = slices.Equal[[]rune]
}
