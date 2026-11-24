package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"

	"github.com/egoist/mygo/ui"
)

// An AI plan is reviewed in place: the note keeps its original blocks (and the
// file keeps its original text) while each changed region is painted with the
// plan's version. The whole plan is accepted or rejected at once.

type planPeek uint8

const (
	planPeekNone planPeek = iota
	planPeekDiff
	planPeekOrig
)

type planHunk struct {
	oldIDs     []model.BlockID
	oldBlocks  []model.Block
	oldChanged []bool
	newBlocks  []model.Block
	newChanged []bool
	rect       ui.Rect // window rect of the hunk content, last frame
}

type planReviewState struct {
	op       model.PlanOp
	doc      *model.Doc
	baseMD   string
	stale    bool
	isCreate bool
	hunks    []planHunk
	lead     map[model.BlockID]int
	member   map[model.BlockID]int
	peek     planPeek
	active   int
	barRect  ui.Rect
}

func (s *Session) planOpForNote(plan *model.BatchPlan) (model.PlanOp, bool) {
	if plan == nil || len(plan.Ops) == 0 {
		return model.PlanOp{}, false
	}
	if len(plan.Ops) > 1 && s.notePath != "" {
		want := filepath.Clean(s.notePath)
		for _, op := range plan.Ops {
			name := filepath.FromSlash(op.Page)
			if filepath.Ext(name) == "" {
				name += ".md"
			}
			if filepath.Clean(filepath.Join(plan.Root, name)) == want || filepath.Base(name) == filepath.Base(want) {
				return op, true
			}
		}
	}
	return plan.Ops[0], true
}

// setupPlanReview aligns the plan's text with the note's current blocks.
func (s *Session) setupPlanReview(plan *model.BatchPlan) {
	s.planReview = nil
	op, ok := s.planOpForNote(plan)
	if !ok || s.doc == nil {
		return
	}
	pr := &planReviewState{op: op, doc: s.doc, baseMD: s.Markdown(), active: -1, isCreate: op.Kind == model.PlanOpCreateNote}
	s.planReview = pr
	s.rebuildPlanHunks()
	s.unfoldPlanHunks()
}

func (s *Session) rebuildPlanHunks() {
	pr := s.planReview
	pr.hunks = nil
	pr.lead = map[model.BlockID]int{}
	pr.member = map[model.BlockID]int{}
	old := s.doc.Blocks
	if len(old) == 0 {
		return
	}
	if pr.isCreate {
		h := planHunk{newBlocks: cloneBlocks(old), newChanged: make([]bool, len(old))}
		for i := range old {
			h.oldIDs = append(h.oldIDs, old[i].ID)
			h.newChanged[i] = true
		}
		s.addPlanHunk(h)
		return
	}
	var newBlocks []model.Block
	if doc := s.parseMarkdownDoc(pr.op.New); doc != nil {
		newBlocks = doc.Blocks
	}
	sig := func(b model.Block) string {
		return strings.TrimSpace(s.serializeBlocks([]model.Block{b}))
	}
	a := make([]string, len(old))
	for i := range old {
		a[i] = sig(old[i])
	}
	b := make([]string, len(newBlocks))
	for i := range newBlocks {
		b[i] = sig(newBlocks[i])
	}
	for _, r := range alignBlockSigs(a, b) {
		oLo, oHi, nLo, nHi := r[0], r[1], r[2], r[3]
		padTail, padHead := false, false
		if oLo == oHi {
			// A pure insertion borrows its unchanged neighbour so it has a place to sit.
			if oHi < len(old) {
				oHi++
				nHi++
				padTail = true
			} else {
				oLo--
				nLo--
				padHead = true
			}
		}
		h := planHunk{
			oldBlocks:  cloneBlocks(old[oLo:oHi]),
			oldChanged: make([]bool, oHi-oLo),
			newBlocks:  cloneBlocks(newBlocks[nLo:nHi]),
			newChanged: make([]bool, nHi-nLo),
		}
		for i := range h.oldBlocks {
			h.oldIDs = append(h.oldIDs, old[oLo+i].ID)
			h.oldChanged[i] = !(padTail && i == len(h.oldBlocks)-1) && !(padHead && i == 0)
		}
		for i := range h.newBlocks {
			h.newChanged[i] = !(padTail && i == len(h.newBlocks)-1) && !(padHead && i == 0)
		}
		s.addPlanHunk(h)
	}
}

func (s *Session) addPlanHunk(h planHunk) {
	pr := s.planReview
	if len(h.oldIDs) == 0 {
		return
	}
	k := len(pr.hunks)
	pr.hunks = append(pr.hunks, h)
	pr.lead[h.oldIDs[0]] = k
	for _, id := range h.oldIDs {
		pr.member[id] = k
	}
}

func cloneBlocks(src []model.Block) []model.Block {
	out := make([]model.Block, len(src))
	for i := range src {
		out[i] = src[i].Clone()
	}
	return out
}

// alignBlockSigs returns changed ranges [oldLo, oldHi, newLo, newHi] between two block lists.
func alignBlockSigs(a, b []string) [][4]int {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(ma) == 0 && len(mb) == 0 {
		return nil
	}
	if len(ma) == 0 || len(mb) == 0 || len(ma)*len(mb) > 4_000_000 {
		return [][4]int{{pre, pre + len(ma), pre, pre + len(mb)}}
	}
	n, m := len(ma), len(mb)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out [][4]int
	i, j := 0, 0
	startI, startJ := -1, -1
	flush := func() {
		if startI >= 0 {
			out = append(out, [4]int{pre + startI, pre + i, pre + startJ, pre + j})
			startI, startJ = -1, -1
		}
	}
	for i < n || j < m {
		switch {
		case i < n && j < m && ma[i] == mb[j]:
			flush()
			i++
			j++
		default:
			if startI < 0 {
				startI, startJ = i, j
			}
			if j < m && (i >= n || lcs[i][j+1] >= lcs[i+1][j]) {
				j++
			} else {
				i++
			}
		}
	}
	flush()
	return out
}

// unfoldPlanHunks opens folded sections that would hide a pending change.
func (s *Session) unfoldPlanHunks() {
	pr := s.planReview
	if pr == nil || len(s.folded) == 0 {
		return
	}
	for idx := range s.doc.Blocks {
		if _, ok := pr.member[s.doc.Blocks[idx].ID]; !ok || !s.isBlockHiddenByFold(idx) {
			continue
		}
		for h := 0; h < idx; h++ {
			head := &s.doc.Blocks[h]
			if head.Kind.IsHeading() && s.folded[head.ID] && idx < s.foldSectionEnd(h) {
				delete(s.folded, head.ID)
				delete(s.foldTails, head.ID)
			}
		}
	}
}

// syncPlanReview marks the review stale when the note was reloaded underneath it.
func (s *Session) syncPlanReview() {
	pr := s.planReview
	if pr == nil || pr.doc == s.doc {
		return
	}
	pr.doc = s.doc
	if s.Markdown() != pr.baseMD {
		pr.stale = true
	}
	s.rebuildPlanHunks()
	s.unfoldPlanHunks()
}

// ApplyAcceptedDoc swaps in an accepted plan's document as one undo step,
// so ⌘Z returns to the text before the plan.
func (s *Session) ApplyAcceptedDoc(doc *model.Doc) {
	prev := s.captureUndoSnapshot()
	undo := s.undoStack
	s.SetDoc(doc)
	s.undoStack = undo
	s.pushUndoSnapshot(prev)
}

func (s *Session) isPlanReviewMember(id model.BlockID) bool {
	if s.planReview == nil {
		return false
	}
	_, ok := s.planReview.member[id]
	return ok
}

// renderPlanReviewRow draws a pending hunk in place of its original blocks.
func (s *Session) renderPlanReviewRow(c *ui.Context, idx int, meta frameBlockMeta) (handled, hovered bool) {
	pr := s.planReview
	if pr == nil {
		return false, false
	}
	id := s.doc.Blocks[idx].ID
	k, isMember := pr.member[id]
	if !isMember {
		return false, false
	}
	if pr.lead[id] != k {
		return true, false
	}
	h := &pr.hunks[k]
	pal := theme.Ocean()

	type item struct {
		b    *model.Block
		tint ui.Color
	}
	var items []item
	switch pr.peek {
	case planPeekOrig:
		if k == pr.active {
			for i := range h.oldBlocks {
				items = append(items, item{b: &h.oldBlocks[i]})
			}
			break
		}
		fallthrough
	case planPeekDiff:
		if pr.peek == planPeekDiff && k == pr.active {
			for i := range h.oldBlocks {
				if h.oldChanged[i] {
					items = append(items, item{b: &h.oldBlocks[i], tint: pal.DiffDelBG})
				}
			}
			for i := range h.newBlocks {
				it := item{b: &h.newBlocks[i]}
				if h.newChanged[i] && !pr.isCreate {
					it.tint = pal.DiffAddBG
				}
				items = append(items, it)
			}
			break
		}
		fallthrough
	default:
		for i := range h.newBlocks {
			items = append(items, item{b: &h.newBlocks[i]})
		}
	}

	var first *model.Block
	if len(items) > 0 {
		first = items[0].b
	} else {
		first = &h.oldBlocks[0]
	}
	topGap := s.blockGap(meta.PrevVisible, first)
	leftGutter := s.leftContentGutter()
	row := ui.Column(c.Key(fmt.Sprintf("plan-hunk-%d", id))).FillWidth().
		Padding(topGap, s.rightContentInset(), 0, leftGutter)
	s.recordBlockLayoutBounds(id, row)

	lineColor := pal.Accent
	if pr.stale {
		lineColor = pal.Hairline
	}
	minH := s.scale(theme.HeightCompact)
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		x := r.X + leftGutter - s.scale(theme.Space12)
		y := r.Y + topGap
		p.Fill(ui.Rect{X: x, Y: y, W: s.scale(2), H: max(r.H-topGap, minH)}, lineColor, theme.RadiusXS)
	})
	row.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		s.recordBlockWinRect(id, r)
		content := ui.Rect{X: r.X + leftGutter, Y: r.Y + topGap, W: max(r.W-leftGutter-s.rightContentInset(), 0), H: max(r.H-topGap, minH)}
		if h.rect != content {
			h.rect = content
			c.Invalidate()
		}
		appUi.RecordBounds(fmt.Sprintf("plan-hunk-%d", k), content)
	})
	row.Children(func() {
		if len(items) == 0 {
			// Removed blocks leave a thin dashed placeholder.
			ui.Box(c.Key("plan-del-mark")).FillWidth().Height(minH).Draw(func(p *ui.Painter, r ui.Rect) {
				y := r.Y + r.H*0.5
				dash, gap := s.scale(6), s.scale(4)
				for x := r.X; x < r.X+r.W; x += dash + gap {
					p.Fill(ui.Rect{X: x, Y: y, W: min(dash, r.X+r.W-x), H: 1}, lineColor, 0)
				}
			})
			return
		}
		numByIndent := map[int]int{}
		for i, it := range items {
			blk := it.b
			numRun := 0
			if blk.Kind == model.BlockNumbered {
				if blk.ListIndex > 0 && numByIndent[blk.Indent] == 0 {
					numByIndent[blk.Indent] = blk.ListIndex
				} else {
					numByIndent[blk.Indent]++
				}
				numRun = numByIndent[blk.Indent]
			} else if !blk.Kind.IsList() {
				clear(numByIndent)
			}
			gap := float32(0)
			if i > 0 {
				gap = s.blockGap(items[i-1].b, blk)
			}
			slot := ui.Box(c.Key(fmt.Sprintf("plan-slot-%d", i))).FillWidth().Padding(gap, 0, 0, 0)
			slot.Children(func() {
				inner := ui.Box(c).FillWidth()
				if it.tint != (ui.Color{}) {
					inner.Background(it.tint).Radius(theme.RadiusXS)
				}
				inner.Children(func() {
					s.renderPreviewNativeBlock(c, i, blk, numRun)
				})
			})
		}
	})
	return true, row.Hovered()
}

func (s *Session) planPointerHunk() int {
	pr := s.planReview
	pt, ok := s.windowPointerStatus()
	if !ok {
		return -1
	}
	in := func(r ui.Rect) bool {
		return r.W > 0 && r.H > 0 && pt.X >= r.X && pt.X <= r.X+r.W && pt.Y >= r.Y && pt.Y <= r.Y+r.H
	}
	if pr.active >= 0 && pr.active < len(pr.hunks) && in(pr.barRect) {
		return pr.active
	}
	gutter := s.scale(theme.Space16)
	for k := range pr.hunks {
		r := pr.hunks[k].rect
		r.X -= gutter
		r.W += gutter
		if in(r) {
			return k
		}
		// The strip between the bar and its hunk keeps the bar open while the pointer crosses it.
		if k == pr.active && pr.barRect.W > 0 && in(ui.Rect{X: pr.barRect.X, Y: pr.barRect.Y, W: pr.barRect.W, H: max(r.Y-pr.barRect.Y, 0)}) {
			return k
		}
	}
	return -1
}

// renderPlanReviewBar floats the review actions over the hovered hunk.
func (s *Session) renderPlanReviewBar(c *ui.Context) {
	pr := s.planReview
	if pr == nil || s.activePlan == nil {
		return
	}
	active := s.planPointerHunk()
	if active != pr.active {
		pr.active = active
		c.Invalidate()
	}
	if active < 0 {
		pr.barRect = ui.Rect{}
		if pr.peek != planPeekNone {
			pr.peek = planPeekNone
			c.Invalidate()
		}
		return
	}
	h := pr.hunks[active].rect
	if h.W <= 0 || h.H <= 0 {
		return
	}
	pal := theme.Ocean()
	vp, winW, _ := s.overlayViewport(c)
	barH := s.scale(theme.HeightCompact) + 2*theme.Space2
	gap := s.scale(theme.Space4)
	top := h.Y - barH - gap
	if top < vp.Y+gap {
		// Pinned to the visible top of a tall hunk, never past its end.
		top = max(min(vp.Y+gap, h.Y+h.H-barH), h.Y)
	}
	right := max(winW-(h.X+h.W), 0)

	peek := planPeekNone
	ui.Overlay(c, func() {
		bar := appUi.Elevate(appUi.MenuSurface(ui.Row(c.Key("plan-review-bar"))).Absolute().Top(top).Right(right).
			Padding(theme.Space2, theme.Space4).Gap(theme.Space2).AlignItems(ui.Center), theme.ElevationPopover)
		appUi.Probe(bar, "plan-review-bar")
		bar.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			if pr.barRect != r {
				pr.barRect = r
				c.Invalidate()
			}
		})
		bar.Children(func() {
			if _, hov := s.planBarButton(c, "plan-peek-diff", theme.Tr("差异", "Changes"), pal.TextSecondary, pr.isCreate); hov {
				peek = planPeekDiff
			}
			if _, hov := s.planBarButton(c, "plan-peek-orig", theme.Tr("原文", "Original"), pal.TextSecondary, pr.isCreate); hov {
				peek = planPeekOrig
			}
			ui.Box(c).Width(1).Height(s.scale(theme.Space12)).Margin(0, theme.Space2).Background(pal.Hairline)
			if clicked, _ := s.planBarButton(c, "plan-reject", theme.Tr("拒绝", "Reject"), pal.TextSecondary, false); clicked {
				s.rejectActivePlan()
			}
			if clicked, _ := s.planBarButton(c, "plan-retry", theme.Tr("重试", "Retry"), pal.TextSecondary, !s.canRetryPlan()); clicked {
				s.retryActivePlan()
			}
			if clicked, _ := s.planBarButton(c, "plan-accept", theme.Tr("接受", "Accept"), pal.Accent, !s.canAcceptPlan()); clicked {
				s.acceptActivePlan()
			}
		})
	})
	if s.planReview != nil && peek != pr.peek {
		pr.peek = peek
		c.Invalidate()
	}
}

func (s *Session) planBarButton(c *ui.Context, id, label string, fg ui.Color, disabled bool) (clicked, hovered bool) {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).Height(s.scale(theme.HeightCompact)).Padding(0, theme.Space8).Center().
		Radius(theme.RadiusSM).Label(label)
	if disabled {
		btn.Cursor(ui.CursorDefault)
		fg = pal.Hairline
	} else {
		btn.Cursor(ui.CursorPointer)
	}
	hovered = btn.Hovered() && !disabled
	pressed := btn.Pressed() && !disabled
	bg := ui.Transparent
	if pressed {
		bg = pal.Selected
	} else if hovered {
		bg = pal.Hover
	}
	appUi.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	btn.Children(func() {
		ui.Text(c, label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
	})
	appUi.Probe(btn, id)
	return btn.Clicked() && !disabled, hovered
}

// canAcceptPlan: a stale review or a read-only note can only be rejected or retried.
func (s *Session) canAcceptPlan() bool {
	return s.planReview != nil && !s.planReview.stale && !s.planBaseReadOnly
}

func (s *Session) planRetryHost() (interface{ RetryBatchPlan(string) error }, bool) {
	if s.host == nil {
		return nil, false
	}
	h, ok := s.host.(interface{ RetryBatchPlan(string) error })
	return h, ok
}

func (s *Session) canRetryPlan() bool {
	_, ok := s.planRetryHost()
	return ok && s.activePlan != nil
}

func (s *Session) retryActivePlan() {
	h, ok := s.planRetryHost()
	if !ok || s.activePlan == nil {
		return
	}
	_ = h.RetryBatchPlan(s.activePlan.ID)
}

// handlePlanReviewShortcuts: ⌘⏎ accepts and ⌘⌫ rejects the pending plan.
func (s *Session) handlePlanReviewShortcuts(c *ui.Context, annotInputFocused bool) {
	if s.planReview == nil || s.activePlan == nil || annotInputFocused {
		return
	}
	if !s.HasFocus(c) && c.FocusTakesText() {
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		if s.canAcceptPlan() {
			s.acceptActivePlan()
		}
	} else if c.Shortcut(ui.Cmd, ui.KeyBackspace) {
		s.rejectActivePlan()
	}
}
