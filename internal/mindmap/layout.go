package mindmap

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	layoutHGap        float32 = 34
	layoutVGapPrimary float32 = 18
	layoutVGapLeaf    float32 = 9
	layoutRootH       float32 = 40
	layoutPrimaryH    float32 = 30
	layoutLeafH       float32 = 24
	layoutRootLineH   float32 = 21
	layoutBranchLineH float32 = 18
	layoutLeafGap     float32 = 3
	layoutTaskBox     float32 = 13
	layoutTaskGap     float32 = 5
	layoutLinkBadge   float32 = 16
	layoutLinkGap     float32 = 5
	layoutFoldOffset  float32 = 13
	layoutFoldRadius  float32 = 8.5
	layoutActionBtnR  float32 = 7.5
	layoutActionGap   float32 = 17
)

// Bounds holds the world-space bounding box of a laid-out mind map.
type Bounds struct {
	MinX, MinY float32
	MaxX, MaxY float32
}

func (b Bounds) Width() float32  { return b.MaxX - b.MinX }
func (b Bounds) Height() float32 { return b.MaxY - b.MinY }

// Camera maps world coordinates to screen coordinates:
//   screenX = (worldX - X) * Scale
//   screenY = (worldY - Y) * Scale
type Camera struct {
	X     float32
	Y     float32
	Scale float32
}

func (cam Camera) WorldToScreen(wx, wy float32) (float32, float32) {
	return (wx - cam.X) * cam.Scale, (wy - cam.Y) * cam.Scale
}

func (cam Camera) ScreenToWorld(sx, sy float32) (float32, float32) {
	if cam.Scale <= 0 {
		return sx, sy
	}
	return sx/cam.Scale + cam.X, sy/cam.Scale + cam.Y
}

// HitKind distinguishes what part of a node was clicked.
type HitKind uint8

const (
	HitNone HitKind = iota
	HitNode
	HitCheckbox
	HitFoldBadge
	HitLinkBadge
	HitHoverFold
	HitHoverAdd
)

// HitResult identifies the clicked mind map element.
type HitResult struct {
	Kind HitKind
	Node *Node
}

// TextMeasurer is satisfied by both *ui.Painter and *ui.Context so layout
// during build and paint shares exact native text measurements.
type TextMeasurer interface {
	MeasureText(width float32, spans ...ui.Span) (float32, float32)
}

func validMeasurer(tm TextMeasurer) TextMeasurer {
	switch v := tm.(type) {
	case nil:
		return nil
	case *ui.Painter:
		if v == nil {
			return nil
		}
	case *ui.Context:
		if v == nil {
			return nil
		}
	}
	return tm
}

type nodeSize struct {
	w, h float32
}

// MeasureCache caches estimated or measured node sizes so repeated pan/zoom frames are O(1).
type MeasureCache struct {
	sizes        map[measureKey]nodeSize
	overrideID   int
	overrideText string
}

// SetTextOverride temporarily substitutes text for nodeID during layout measurement
// (e.g. live inline editing or IME composition) without mutating the node itself.
func (mc *MeasureCache) SetTextOverride(nodeID int, text string) {
	if mc == nil {
		return
	}
	mc.overrideID = nodeID
	mc.overrideText = text
}

// ClearTextOverride removes any active inline editing size override.
func (mc *MeasureCache) ClearTextOverride() {
	if mc == nil {
		return
	}
	mc.overrideID = 0
	mc.overrideText = ""
}

type measureKey struct {
	text    string
	depth   int
	hasTask bool
	hasLink bool
}

// MeasureNodeWidth returns the horizontal DIP width of node n, caching by (text, depth, hasTask, hasLink).
func (mc *MeasureCache) MeasureNodeWidth(n *Node, tm TextMeasurer) float32 {
	w, _ := mc.MeasureNodeSize(n, tm)
	return w
}

// MeasureNodeSize returns the DIP (width, height) of node n, supporting multi-line text.
func (mc *MeasureCache) MeasureNodeSize(n *Node, tm TextMeasurer) (float32, float32) {
	tm = validMeasurer(tm)
	text := n.Text
	if mc != nil && mc.overrideID != 0 && n.ID == mc.overrideID {
		text = mc.overrideText
	}
	hasLink := strings.TrimSpace(n.Link) != ""
	if mc == nil {
		return estimateNodeSizeText(text, n.Depth, n.Task != nil, hasLink, tm)
	}
	if mc.sizes == nil {
		mc.sizes = make(map[measureKey]nodeSize, 64)
	}
	k := measureKey{text: text, depth: min(n.Depth, 2), hasTask: n.Task != nil, hasLink: hasLink}
	if sz, ok := mc.sizes[k]; ok {
		return sz.w, sz.h
	}
	if len(mc.sizes) > 8192 {
		clear(mc.sizes)
	}
	w, h := estimateNodeSizeText(text, n.Depth, n.Task != nil, hasLink, tm)
	if tm != nil {
		mc.sizes[k] = nodeSize{w: w, h: h}
	}
	return w, h
}

func nodeBaseHeight(depth int) float32 {
	switch depth {
	case 0:
		return layoutRootH
	case 1:
		return layoutPrimaryH
	default:
		return layoutLeafH
	}
}

func nodeLineHeight(depth int) float32 {
	if depth == 0 {
		return layoutRootLineH
	}
	return layoutBranchLineH
}

func nodeFontSize(depth int) float32 {
	if depth == 0 {
		return theme.FontTitle
	}
	return theme.FontSmall
}

func nodeFontWeight(depth int) int {
	switch depth {
	case 0:
		return theme.WeightTitle
	case 1:
		return theme.WeightControl
	default:
		return theme.WeightRegular
	}
}

func fallbackLineWidth(line string, sz float32) float32 {
	var units float32
	for _, r := range line {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) || (r >= 0xFF00 && r <= 0xFFEF):
			units += 1.02
		case r == ' ':
			units += 0.34
		case unicode.IsUpper(r):
			units += 0.68
		default:
			units += 0.56
		}
	}
	return units * sz
}

func estimateNodeSizeText(text string, depth int, hasTask, hasLink bool, tm TextMeasurer) (float32, float32) {
	lines := strings.Split(text, "\n")
	sz := nodeFontSize(depth)
	wt := nodeFontWeight(depth)
	var textW float32
	for _, line := range lines {
		if line == "" {
			continue
		}
		var lw float32
		if tm != nil {
			lw, _ = tm.MeasureText(0, ui.Span{Text: line, Size: sz, Weight: wt})
		} else {
			lw = fallbackLineWidth(line, sz)
		}
		if lw > textW {
			textW = lw
		}
	}
	var padX float32
	switch depth {
	case 0:
		padX = 36
	case 1:
		padX = 24
	default:
		padX = 16
	}
	if hasTask {
		padX += layoutTaskBox + layoutTaskGap
	}
	if hasLink {
		padX += layoutLinkBadge + layoutLinkGap
	}
	minW := float32(44)
	if depth == 0 {
		minW = 76
	} else if depth == 1 {
		minW = 54
	}
	w := max(float32(math.Ceil(float64(textW+padX))), minW)
	h := nodeBaseHeight(depth) + float32(max(len(lines)-1, 0))*nodeLineHeight(depth)
	return w, h
}

// ComputeLayout performs an O(N) MindNode-style tree layout and returns the world bounding box.
func ComputeLayout(root *Node, mode LayoutMode, tm TextMeasurer, mc *MeasureCache) Bounds {
	if root == nil {
		return Bounds{}
	}
	tm = validMeasurer(tm)

	// Pass 1: annotate Depth, Branch, W, H.
	var prep func(n, parent *Node, depth, branch int)
	prep = func(n, parent *Node, depth, branch int) {
		n.Parent = parent
		n.Depth = depth
		n.Branch = branch
		n.W, n.H = mc.MeasureNodeSize(n, tm)
		for i, ch := range n.Children {
			b := branch
			if depth == 0 {
				b = i
			}
			prep(ch, n, depth+1, b)
		}
	}
	prep(root, nil, 0, 0)

	// Pass 2: bottom-up subtree height.
	vgap := func(depth int) float32 {
		if depth == 0 {
			return layoutVGapPrimary
		}
		return layoutVGapLeaf
	}
	sumHeight := func(depth int, list []*Node) float32 {
		if len(list) == 0 {
			return 0
		}
		var sum float32
		g := vgap(depth)
		for i, ch := range list {
			sum += ch.SubH
			if i > 0 {
				sum += g
			}
		}
		return sum
	}
	var calcSub func(n *Node) float32
	calcSub = func(n *Node) float32 {
		if !n.IsOpen() {
			n.SubH = n.H
			return n.SubH
		}
		for _, ch := range n.Children {
			calcSub(ch)
		}
		n.SubH = max(n.H, sumHeight(n.Depth, n.Children))
		return n.SubH
	}
	calcSub(root)

	// Pass 3: top-down placement.
	var place func(n *Node, x, y float32, dir int)
	place = func(n *Node, x, y float32, dir int) {
		n.Dir = dir
		if dir > 0 {
			n.X = x
		} else {
			n.X = x - n.W
		}
		n.Y = y
		if !n.IsOpen() {
			return
		}
		g := vgap(n.Depth)
		curY := y - sumHeight(n.Depth, n.Children)/2
		nextX := n.X - layoutHGap
		if dir > 0 {
			nextX = n.X + n.W + layoutHGap
		}
		for _, ch := range n.Children {
			place(ch, nextX, curY+ch.SubH/2, dir)
			curY += ch.SubH + g
		}
	}

	if mode == LayoutBoth && root.IsOpen() {
		k := (len(root.Children) + 1) / 2
		rightKids := root.Children[:k]
		leftKids := root.Children[k:]
		root.X = -root.W / 2
		root.Y = 0
		root.Dir = 1

		curY := -sumHeight(0, rightKids) / 2
		for _, ch := range rightKids {
			place(ch, root.W/2+layoutHGap, curY+ch.SubH/2, 1)
			curY += ch.SubH + layoutVGapPrimary
		}
		curY = -sumHeight(0, leftKids) / 2
		for _, ch := range leftKids {
			place(ch, -root.W/2-layoutHGap, curY+ch.SubH/2, -1)
			curY += ch.SubH + layoutVGapPrimary
		}
	} else {
		place(root, 0, 0, 1)
	}

	// Pass 4: compute world bounds (including fold badges).
	bounds := Bounds{
		MinX: float32(1e9),
		MinY: float32(1e9),
		MaxX: float32(-1e9),
		MaxY: float32(-1e9),
	}
	var walkBounds func(n *Node)
	walkBounds = func(n *Node) {
		bounds.MinX = min(bounds.MinX, n.X)
		bounds.MaxX = max(bounds.MaxX, n.X+n.W)
		bounds.MinY = min(bounds.MinY, n.Y-n.H/2)
		bounds.MaxY = max(bounds.MaxY, n.Y+n.H/2)
		if n.Folded && len(n.Children) > 0 {
			bx := n.X - layoutFoldOffset
			if n.Dir > 0 {
				bx = n.X + n.W + layoutFoldOffset
			}
			bounds.MinX = min(bounds.MinX, bx-layoutFoldRadius-2)
			bounds.MaxX = max(bounds.MaxX, bx+layoutFoldRadius+2)
			by := n.Y
			if n.Depth > 1 {
				by = n.Y + n.H/2
			}
			bounds.MinY = min(bounds.MinY, by-layoutFoldRadius-2)
			bounds.MaxY = max(bounds.MaxY, by+layoutFoldRadius+2)
			return
		}
		for _, ch := range n.Children {
			walkBounds(ch)
		}
	}
	walkBounds(root)
	if bounds.MinX > bounds.MaxX {
		return Bounds{}
	}
	return bounds
}

// FitCamera computes a Camera that centers the mind map within a viewport of (vw, vh).
func FitCamera(bounds Bounds, vw, vh, padX, padY float32) Camera {
	if vw <= 0 {
		vw = 600
	}
	if vh <= 0 {
		vh = 360
	}
	bw := max(bounds.Width()+padX*2, 1)
	bh := max(bounds.Height()+padY*2, 1)
	s := min(float32(1.0), min(vw/bw, vh/bh))
	if s < 0.15 {
		s = 0.15
	}
	visW := vw / s
	visH := vh / s
	return Camera{
		X:     (bounds.MinX+bounds.MaxX)/2 - visW/2,
		Y:     (bounds.MinY+bounds.MaxY)/2 - visH/2,
		Scale: s,
	}
}

type worldCircle struct {
	cx, cy, r float32
}

func (c worldCircle) hit(wx, wy, pad float32) bool {
	dx, dy := wx-c.cx, wy-c.cy
	rr := c.r + pad
	return dx*dx+dy*dy <= rr*rr
}

func nodePadRight(n *Node) float32 {
	switch n.Depth {
	case 0:
		return 14
	case 1:
		return 10
	default:
		return 6
	}
}

const (
	minLinkBadgeScreenR float32 = 5.5
	minActionBtnScreenR float32 = 6.0
)

func effectiveWorldRadius(worldR, scale, minScreenR float32) float32 {
	if scale <= 0 {
		scale = 1
	}
	if minScreenR <= 0 {
		return worldR
	}
	return max(worldR, minScreenR/scale)
}

func nodeLinkBadgeCircle(n *Node) worldCircle {
	cx := n.X + n.W - nodePadRight(n) - layoutLinkBadge*0.5
	cy := nodeFirstLineCenterY(n)
	return worldCircle{cx: cx, cy: cy, r: layoutLinkBadge * 0.5}
}

func nodeLinkBadgeCircleAtScale(n *Node, scale float32) worldCircle {
	base := nodeLinkBadgeCircle(n)
	base.r = effectiveWorldRadius(base.r, scale, minLinkBadgeScreenR)
	return base
}

func nodeActionButtonsAtScale(n *Node, readOnly bool, scale float32) (foldBtn worldCircle, showFold bool, addBtn worldCircle, showAdd bool) {
	if n == nil {
		return
	}
	if scale <= 0 {
		scale = 1
	}
	r := effectiveWorldRadius(layoutActionBtnR, scale, minActionBtnScreenR)
	minGap := max(float32(layoutActionGap), r*2+4/scale)
	edgeGap := max(float32(11), r+3/scale)
	ay := n.Y
	if n.Depth > 1 {
		ay = n.Y + n.H/2
	}
	dir := n.Dir
	if dir == 0 {
		dir = 1
	}
	edgeX := n.X + n.W
	sign := float32(1)
	if dir < 0 {
		edgeX = n.X
		sign = -1
	}
	if n.Folded && len(n.Children) > 0 {
		if !readOnly {
			showAdd = true
			addBtn = worldCircle{
				cx: edgeX + sign*(layoutFoldOffset+layoutFoldRadius+r+4/scale),
				cy: ay,
				r:  r,
			}
		}
		return
	}
	if len(n.Children) > 0 {
		showFold = true
		foldBtn = worldCircle{cx: edgeX + sign*edgeGap, cy: ay, r: r}
		if !readOnly {
			showAdd = true
			addBtn = worldCircle{cx: edgeX + sign*(edgeGap+minGap), cy: ay, r: r}
		}
		return
	}
	if !readOnly {
		showAdd = true
		addBtn = worldCircle{cx: edgeX + sign*max(float32(12), r+3/scale), cy: ay, r: r}
	}
	return
}

func isNodeVisibleInTree(n *Node) bool {
	if n == nil {
		return false
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Folded {
			return false
		}
	}
	return true
}

// HitTestWithActions extends HitTestAtScale by testing the floating collapse (−) and add (+)
// action buttons on activeIDs (typically [HoveredID, SelectedID]) and preserving a small
// hover bridge between the node edge and those buttons.
func HitTestWithActions(root *Node, activeIDs [2]int, readOnly bool, wx, wy, scale float32) HitResult {
	if root == nil {
		return HitResult{}
	}
	if scale <= 0 {
		scale = 1
	}
	hitPad := 2.0 / scale
	seen := [2]int{}
	for idx, id := range activeIDs {
		if id == 0 || (idx == 1 && id == seen[0]) {
			continue
		}
		seen[idx] = id
		n := root.FindByID(id)
		if n == nil || !isNodeVisibleInTree(n) {
			continue
		}
		foldBtn, showFold, addBtn, showAdd := nodeActionButtonsAtScale(n, readOnly, scale)
		if showFold && foldBtn.hit(wx, wy, hitPad) {
			return HitResult{Kind: HitHoverFold, Node: n}
		}
		if showAdd && addBtn.hit(wx, wy, hitPad) {
			return HitResult{Kind: HitHoverAdd, Node: n}
		}
	}

	hit := HitTestAtScale(root, wx, wy, scale)
	if hit.Kind != HitNone {
		return hit
	}

	// Keep hover sticky across the small gap between the hovered node's edge and its action buttons.
	if activeIDs[0] != 0 {
		if n := root.FindByID(activeIDs[0]); n != nil && isNodeVisibleInTree(n) {
			foldBtn, showFold, addBtn, showAdd := nodeActionButtonsAtScale(n, readOnly, scale)
			if showFold || showAdd {
				ay := n.Y
				if n.Depth > 1 {
					ay = n.Y + n.H/2
				}
				minX, maxX := n.X, n.X+n.W
				btnR := float32(0)
				if showFold {
					minX = min(minX, foldBtn.cx-foldBtn.r-hitPad)
					maxX = max(maxX, foldBtn.cx+foldBtn.r+hitPad)
					btnR = max(btnR, foldBtn.r)
				}
				if showAdd {
					minX = min(minX, addBtn.cx-addBtn.r-hitPad)
					maxX = max(maxX, addBtn.cx+addBtn.r+hitPad)
					btnR = max(btnR, addBtn.r)
				}
				top := min(n.Y-n.H/2, ay-btnR-hitPad)
				bot := max(n.Y+n.H/2, ay+btnR+hitPad)
				if wx >= minX && wx <= maxX && wy >= top && wy <= bot {
					return HitResult{Kind: HitNone, Node: n}
				}
			}
		}
	}
	return HitResult{}
}

// HitTest finds which node, link badge, task checkbox, or fold badge lies under world point (wx, wy).
func HitTest(root *Node, wx, wy float32) HitResult {
	return HitTestAtScale(root, wx, wy, 1)
}

// HitTestAtScale matches the rendered minimum screen-space geometry for badges at any zoom level.
func HitTestAtScale(root *Node, wx, wy, scale float32) HitResult {
	if root == nil {
		return HitResult{}
	}
	if scale <= 0 {
		scale = 1
	}
	badgePad := 2.0 / scale
	var hit HitResult
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		// Check fold badge first (when folded).
		if n.Folded && len(n.Children) > 0 {
			bx := n.X - layoutFoldOffset
			if n.Dir > 0 {
				bx = n.X + n.W + layoutFoldOffset
			}
			by := n.Y
			if n.Depth > 1 {
				by = n.Y + n.H/2
			}
			dx, dy := wx-bx, wy-by
			r := layoutFoldRadius + 4
			if dx*dx+dy*dy <= r*r {
				hit = HitResult{Kind: HitFoldBadge, Node: n}
				return true
			}
		}

		// Check link badge using the exact scale-clamped circle geometry used for painting.
		if strings.TrimSpace(n.Link) != "" {
			lb := nodeLinkBadgeCircleAtScale(n, scale)
			if lb.hit(wx, wy, badgePad) {
				hit = HitResult{Kind: HitLinkBadge, Node: n}
				return true
			}
		}

		// Check node rect (with generous 2px vertical hit padding).
		top := n.Y - n.H/2 - 2
		bot := n.Y + n.H/2 + 2
		if wx >= n.X-2 && wx <= n.X+n.W+2 && wy >= top && wy <= bot {
			if n.Task != nil {
				tx := nodeContentLeft(n)
				ty := nodeFirstLineCenterY(n)
				if wx >= tx-3 && wx <= tx+layoutTaskBox+4 && wy >= ty-layoutTaskBox/2-3 && wy <= ty+layoutTaskBox/2+3 {
					hit = HitResult{Kind: HitCheckbox, Node: n}
					return true
				}
			}
			hit = HitResult{Kind: HitNode, Node: n}
			return true
		}

		if n.Folded {
			return false
		}
		for _, ch := range n.Children {
			if walk(ch) {
				return true
			}
		}
		return false
	}
	walk(root)
	return hit
}

// DragDropPos indicates where a dragged node will be placed relative to a target node.
type DragDropPos uint8

const (
	DropNone DragDropPos = iota
	DropInside
	DropBefore
	DropAfter
)

// HitTestDropTarget finds a valid drop target node and relative insertion position
// for dragged at world coordinates (wx, wy). Never matches dragged or its descendants.
func HitTestDropTarget(root, dragged *Node, wx, wy float32) (*Node, DragDropPos) {
	if root == nil || dragged == nil {
		return nil, DropNone
	}
	var target *Node
	var pos DragDropPos
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		if n == dragged {
			return false
		}
		if wx >= n.X-6 && wx <= n.X+n.W+6 && wy >= n.Y-n.H/2-6 && wy <= n.Y+n.H/2+6 {
			target = n
			if n.Parent == nil {
				pos = DropInside
			} else if wy < n.Y-n.H*0.25 {
				pos = DropBefore
			} else if wy > n.Y+n.H*0.25 {
				pos = DropAfter
			} else {
				pos = DropInside
			}
			return true
		}
		if n.Folded {
			return false
		}
		for _, ch := range n.Children {
			if walk(ch) {
				return true
			}
		}
		return false
	}
	walk(root)
	return target, pos
}

func nodeContentLeft(n *Node) float32 {
	switch n.Depth {
	case 0:
		return n.X + 18
	case 1:
		return n.X + 12
	default:
		return n.X + 8
	}
}

func nodeTextCenterY(n *Node) float32 {
	if n.Depth > 1 {
		return n.Y - layoutLeafGap*0.5
	}
	return n.Y
}

func nodeFirstLineCenterY(n *Node) float32 {
	lineH := nodeLineHeight(n.Depth)
	textBlockH := max(n.H-(nodeBaseHeight(n.Depth)-lineH), lineH)
	return nodeTextCenterY(n) - textBlockH*0.5 + lineH*0.5
}

// PaintOptions configures how PaintScene renders a mind map onto a ui.Painter.
type PaintOptions struct {
	SelectedID int
	HoveredID  int
	HoveredHit HitKind
	EditingID  int
	Palette    theme.Palette
	// Background fills the viewport before painting if FillBackground is true.
	FillBackground bool
	Background     ui.Color
	// ForceLabels keeps node text visible even at small export scales.
	ForceLabels bool
	ReadOnly    bool
	ExportMode  bool

	// LinkStatusFn classifies a node's link target for visual badge styling.
	LinkStatusFn func(target string) LinkStatus

	// Canvas find highlight state.
	FindActive    bool
	FindMatchIDs  map[int]bool
	FindCurrentID int

	// Drag-and-drop preview state (all zero when not dragging).
	DragNodeID   int
	DragTargetID int
	DragPos      DragDropPos
	DragGhostX   float32 // Pointer X relative to viewport r.X
	DragGhostY   float32 // Pointer Y relative to viewport r.Y
}

// PaintScene renders the entire mind map (edges, nodes, checkboxes, links, fold badges,
// selection ring) into viewport r using Camera cam. Uses viewport culling so 10,000+ nodes
// render smoothly at 60fps. Shared by both the live interactive canvas and PNG/PDF export.
func PaintScene(p *ui.Painter, root *Node, r ui.Rect, cam Camera, opt PaintOptions) {
	if p == nil || root == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	pal := opt.Palette
	bg := opt.Background
	if bg.A == 0 {
		bg = pal.CodeBG
	}
	if opt.FillBackground {
		p.Fill(r, bg, 0)
	}
	branches := pal.MindMapBranchColors()
	scale := cam.Scale
	if scale <= 0 {
		scale = 1
	}

	var draggedNode *Node
	if opt.DragNodeID != 0 {
		draggedNode = root.FindByID(opt.DragNodeID)
	}

	// Visible world rect with margin for curves and strokes.
	margin := float32(64) / scale
	visMinX := cam.X - margin
	visMaxX := cam.X + r.W/scale + margin
	visMinY := cam.Y - margin
	visMaxY := cam.Y + r.H/scale + margin

	wxToScreen := func(wx float32) float32 { return r.X + (wx-cam.X)*scale }
	wyToScreen := func(wy float32) float32 { return r.Y + (wy-cam.Y)*scale }

	p.Clip(r, 0, func() {
		// Pass 1: Bezier branch edges + leaf underlines (drawn behind nodes).
		var paintEdges func(n *Node)
		paintEdges = func(n *Node) {
			col := branches[n.Branch%len(branches)]
			if draggedNode != nil && draggedNode.IsAncestorOf(n) {
				col = col.Alpha(0.35)
			} else if opt.FindActive && !opt.FindMatchIDs[n.ID] {
				col = col.Alpha(0.28)
			}
			if n.Parent != nil {
				par := n.Parent
				d := n.Dir
				px := par.X
				if d > 0 {
					px = par.X + par.W
				}
				py := par.Y
				if par.Depth > 1 {
					py = par.Y + par.H/2
				}
				nx := n.X + n.W
				if d > 0 {
					nx = n.X
				}
				ny := n.Y
				if n.Depth > 1 {
					ny = n.Y + n.H/2
				}

				edgeMinX := min(px, min(n.X, n.X+n.W))
				edgeMaxX := max(px, max(n.X, n.X+n.W))
				edgeMinY := min(py, ny)
				edgeMaxY := max(py, ny)
				if edgeMaxX >= visMinX && edgeMinX <= visMaxX && edgeMaxY >= visMinY && edgeMinY <= visMaxY {
					midX := (px + nx) * 0.5
					var path ui.Path
					path.Cap(ui.RoundCap).Join(ui.RoundJoin)
					if par.Depth > 1 && par.ID == opt.EditingID {
						// Tuck outgoing curve start under the editing chip's rounded bottom corner.
						tuck := min(float32(6), par.W*0.5)
						startX := px + tuck
						if d > 0 {
							startX = px - tuck
						}
						path.MoveTo(wxToScreen(startX), wyToScreen(py))
						path.LineTo(wxToScreen(px), wyToScreen(py))
					} else {
						path.MoveTo(wxToScreen(px), wyToScreen(py))
					}
					path.CubeTo(
						wxToScreen(midX), wyToScreen(py),
						wxToScreen(midX), wyToScreen(ny),
						wxToScreen(nx), wyToScreen(ny),
					)
					if n.Depth > 1 {
						if n.ID == opt.EditingID {
							// Tuck incoming curve cleanly into the editing chip corner to avoid double lines.
							tuck := min(float32(6), n.W*0.5)
							tuckX := nx - tuck
							if d > 0 {
								tuckX = nx + tuck
							}
							path.LineTo(wxToScreen(tuckX), wyToScreen(ny))
						} else {
							endX := n.X
							if d > 0 {
								endX = n.X + n.W
							}
							path.LineTo(wxToScreen(endX), wyToScreen(ny))
						}
					}
					sw := float32(1.6)
					if n.Depth == 1 {
						sw = 2.2
					}
					p.StrokePath(&path, max(sw*scale, 1), col)
					if n.Depth > 1 && n.ID == opt.SelectedID && n.ID != opt.EditingID {
						var selLine ui.Path
						selLine.Cap(ui.RoundCap)
						selLine.MoveTo(wxToScreen(n.X+0.5), wyToScreen(ny))
						selLine.LineTo(wxToScreen(n.X+n.W-0.5), wyToScreen(ny))
						p.StrokePath(&selLine, max(2.2*scale, 1.4), col)
					}
				}
			}

			if n.Folded && len(n.Children) > 0 {
				bx := n.X - layoutFoldOffset
				startX := n.X
				if n.Dir > 0 {
					bx = n.X + n.W + layoutFoldOffset
					startX = n.X + n.W
				}
				if n.Depth > 1 && n.ID == opt.EditingID {
					tuck := min(float32(6), n.W*0.5)
					if n.Dir > 0 {
						startX -= tuck
					} else {
						startX += tuck
					}
				}
				by := n.Y
				if n.Depth > 1 {
					by = n.Y + n.H/2
				}
				if max(startX, bx) >= visMinX && min(startX, bx) <= visMaxX && by >= visMinY && by <= visMaxY {
					bc := branches[0]
					if n.Depth > 0 {
						bc = branches[n.Branch%len(branches)]
					}
					if draggedNode != nil && draggedNode.IsAncestorOf(n) {
						bc = bc.Alpha(0.35)
					}
					p.Line(wxToScreen(startX), wyToScreen(by), wxToScreen(bx), wyToScreen(by), max(1.6*scale, 1), bc)
				}
				return
			}
			for _, ch := range n.Children {
				paintEdges(ch)
			}
		}
		paintEdges(root)

		// Pass 2: Nodes, checkboxes, labels, link badges, fold badges, hover & selection rings.
		resolveLinkStatus := func(raw string) LinkStatus {
			if strings.TrimSpace(raw) == "" {
				return LinkStatusNone
			}
			if opt.LinkStatusFn != nil {
				return opt.LinkStatusFn(raw)
			}
			if ClassifyLink(raw) == LinkWeb {
				return LinkStatusExternal
			}
			return LinkStatusValid
		}

		var paintNodes func(n *Node)
		paintNodes = func(n *Node) {
			col := branches[n.Branch%len(branches)]
			dimmed := draggedNode != nil && draggedNode.IsAncestorOf(n)
			isFindMatch := opt.FindActive && opt.FindMatchIDs[n.ID]
			isFindCurrent := opt.FindActive && n.ID == opt.FindCurrentID
			findDimmed := opt.FindActive && !isFindMatch
			nodeVisible := n.X+n.W >= visMinX && n.X <= visMaxX && n.Y+n.H/2 >= visMinY && n.Y-n.H/2 <= visMaxY

			if nodeVisible {
				sx := wxToScreen(n.X)
				sy := wyToScreen(n.Y - n.H/2)
				sw := n.W * scale
				sh := n.H * scale
				rect := ui.Rect{X: sx, Y: sy, W: sw, H: sh}

				isEditing := n.ID == opt.EditingID
				isSelected := n.ID == opt.SelectedID && !isEditing
				isHovered := n.ID == opt.HoveredID && !isSelected && !isEditing && opt.DragNodeID == 0
				hasLink := strings.TrimSpace(n.Link) != ""
				linkStatus := LinkStatusNone
				if hasLink {
					linkStatus = resolveLinkStatus(n.Link)
				}

				pad := float32(3) * scale
				ringR := ui.Rect{X: sx - pad, Y: sy - pad, W: sw + pad*2, H: sh + pad*2}
				rootRad := min(sh*0.5, layoutRootH*0.5*scale)
				ringRad := theme.RadiusLG * scale
				if n.Depth == 0 {
					ringRad = rootRad + pad
				}

				leafR := ui.Rect{X: sx, Y: sy, W: sw, H: max((n.H-layoutLeafGap)*scale, 1)}
				leafRad := theme.RadiusSM * scale
				dimFactor := float32(1)
				if dimmed {
					dimFactor = 0.35
				} else if findDimmed {
					dimFactor = 0.28
				}

				switch n.Depth {
				case 0:
					if isEditing {
						haloW := float32(3.5) * scale
						haloR := ui.Rect{X: sx - haloW, Y: sy - haloW, W: sw + haloW*2, H: sh + haloW*2}
						haloAlpha := float32(0.24)
						if pal.Dark {
							haloAlpha = 0.30
						}
						p.Fill(haloR, pal.Accent.Alpha(haloAlpha), rootRad+haloW)
						p.Fill(rect, pal.Text, rootRad)
						p.Stroke(rect, pal.Accent, rootRad, max(1.5*scale, 1.2))
					} else {
						rootBG := pal.Text
						if dimFactor < 1 {
							rootBG = rootBG.Alpha(dimFactor)
						}
						p.Fill(rect, rootBG, rootRad)
					}
				case 1:
					rad := theme.RadiusMD * scale
					if isEditing {
						haloW := float32(3.5) * scale
						haloR := ui.Rect{X: sx - haloW, Y: sy - haloW, W: sw + haloW*2, H: sh + haloW*2}
						haloAlpha := float32(0.22)
						mixT := float32(0.10)
						if pal.Dark {
							haloAlpha = 0.28
							mixT = 0.16
						}
						p.Fill(haloR, col.Alpha(haloAlpha), rad+haloW)
						p.Fill(rect, pal.Card.Mix(col, mixT), rad)
						p.Stroke(rect, col, rad, max(1.6*scale, 1.2))
					} else {
						fillCol := col.Alpha(0.13)
						strokeCol := col
						strokeW := max(1.2*scale, 1)
						if isSelected {
							fillCol = pal.SelectionBG
							strokeCol = pal.Accent
							strokeW = max(1.5*scale, 1.2)
							if dimFactor < 1 {
								fillCol = fillCol.Alpha(0.45)
								strokeCol = strokeCol.Alpha(0.45)
							}
						} else {
							if isHovered {
								fillCol = col.Alpha(0.22)
							}
							if dimFactor < 1 {
								fillCol = col.Alpha(0.06)
								strokeCol = col.Alpha(dimFactor)
							}
						}
						p.Fill(rect, fillCol, rad)
						p.Stroke(rect, strokeCol, rad, strokeW)
					}
				default:
					if isEditing {
						editLeafR := ui.Rect{
							X: sx,
							Y: sy - layoutLeafGap*scale,
							W: sw,
							H: (n.H + layoutLeafGap) * scale,
						}
						haloW := float32(3.0) * scale
						haloR := ui.Rect{
							X: editLeafR.X - haloW,
							Y: editLeafR.Y - haloW,
							W: editLeafR.W + haloW*2,
							H: editLeafR.H + haloW*2,
						}
						haloAlpha := float32(0.20)
						mixT := float32(0.09)
						if pal.Dark {
							haloAlpha = 0.28
							mixT = 0.14
						}
						p.Fill(haloR, col.Alpha(haloAlpha), leafRad+haloW)
						p.Fill(editLeafR, pal.Card.Mix(col, mixT), leafRad)
						p.Stroke(editLeafR, col, leafRad, max(1.5*scale, 1.1))
					} else if isSelected {
						selBG := pal.SelectionBG
						if dimFactor < 1 {
							selBG = selBG.Alpha(0.45)
						}
						p.Fill(leafR, selBG, leafRad)
					} else if isHovered {
						hoverAlpha := float32(0.06)
						if pal.Dark {
							hoverAlpha = 0.08
						}
						p.Fill(leafR, pal.Text.Alpha(hoverAlpha), leafRad)
					}
				}

				// Find match highlight fill & active match focus ring.
				if isFindMatch && !isEditing {
					hlFill := pal.FindMatch
					if isFindCurrent {
						hlFill = pal.FindActive
					}
					switch n.Depth {
					case 0:
						p.Fill(ringR, hlFill.Alpha(0.45), ringRad)
						if isFindCurrent {
							p.Stroke(ringR, pal.FindRing, ringRad, max(2*scale, 1.6))
						}
					case 1:
						rad := theme.RadiusMD * scale
						p.Fill(rect, hlFill.Alpha(0.55), rad)
						if isFindCurrent {
							p.Stroke(ringR, pal.FindRing, ringRad, max(2*scale, 1.6))
						}
					default:
						p.Fill(leafR, hlFill.Alpha(0.65), leafRad)
						if isFindCurrent {
							p.Stroke(leafR, pal.FindRing, leafRad, max(1.8*scale, 1.4))
						}
					}
				}

				// Highlight drop-inside target beneath label so text stays sharp.
				if opt.DragNodeID != 0 && n.ID == opt.DragTargetID && opt.DragPos == DropInside {
					if n.Depth > 1 {
						p.Fill(leafR, pal.Accent.Alpha(0.14), leafRad)
					} else {
						p.Fill(ringR, pal.Accent.Alpha(0.14), ringRad)
					}
				}

				tx := nodeContentLeft(n)
				ty := nodeTextCenterY(n)
				if n.Task != nil {
					cbCenterY := nodeFirstLineCenterY(n)
					cbX := wxToScreen(tx)
					cbY := wyToScreen(cbCenterY - layoutTaskBox/2)
					cbS := layoutTaskBox * scale
					cbR := ui.Rect{X: cbX, Y: cbY, W: cbS, H: cbS}
					cbRad := float32(3.5) * scale
					if *n.Task {
						acc := pal.Accent
						if dimFactor < 1 {
							acc = acc.Alpha(dimFactor)
						}
						p.Fill(cbR, acc, cbRad)
						var ck ui.Path
						ck.Cap(ui.RoundCap).Join(ui.RoundJoin)
						ck.MoveTo(cbX+cbS*0.24, cbY+cbS*0.52)
						ck.LineTo(cbX+cbS*0.43, cbY+cbS*0.71)
						ck.LineTo(cbX+cbS*0.76, cbY+cbS*0.30)
						p.StrokePath(&ck, max(1.5*scale, 1), pal.Paper)
					} else {
						p.Fill(cbR, pal.Paper, cbRad)
						borderC := pal.TextSecondary
						if dimFactor < 1 {
							borderC = borderC.Alpha(dimFactor)
						}
						p.Stroke(cbR, borderC, cbRad, max(1.2*scale, 1))
					}
					tx += layoutTaskBox + layoutTaskGap
				}

				if !isEditing {
					txtColor := pal.Text
					if n.Depth == 0 {
						txtColor = pal.Paper
					} else if hasLink {
						if linkStatus == LinkStatusBroken {
							txtColor = pal.Warning
						} else {
							txtColor = pal.Accent
						}
					} else if isSelected && (n.Task == nil || !*n.Task) {
						txtColor = pal.SelectionFG
					}
					if n.Task != nil && *n.Task {
						txtColor = txtColor.Alpha(0.55)
					}
					if dimFactor < 1 {
						txtColor = txtColor.Alpha(dimFactor)
					}

					fontSize := nodeFontSize(n.Depth) * scale
					if opt.ForceLabels || fontSize >= 5 {
						if fontSize < 2.5 {
							fontSize = 2.5
						}
						wt := nodeFontWeight(n.Depth)
						if n.Depth > 1 && isSelected {
							wt = theme.WeightControl
						}
						lines := strings.Split(n.Text, "\n")
						lineH := nodeLineHeight(n.Depth) * scale
						totalH := float32(len(lines)) * lineH
						textTop := wyToScreen(ty) - totalH*0.5

						textAreaRight := n.X + n.W - nodePadRight(n)
						if hasLink {
							textAreaRight -= layoutLinkBadge + layoutLinkGap
						}
						for i, line := range lines {
							if line == "" {
								continue
							}
							sp := ui.Span{
								Text:   line,
								Size:   fontSize,
								Weight: wt,
								Color:  txtColor,
							}
							tw, th := p.MeasureText(0, sp)
							drawX := wxToScreen(tx)
							if n.Task == nil {
								if hasLink {
									availW := max((textAreaRight-tx)*scale, tw)
									drawX = wxToScreen(tx) + (availW-tw)*0.5
								} else {
									drawX = sx + (sw-tw)*0.5
								}
							}
							lineCenterY := textTop + (float32(i)+0.5)*lineH
							p.RichText(drawX, lineCenterY-th*0.5, 0, sp)
						}
					}
				}

				// Clickable link indicator badge on ALL depths (including root depth 0!).
				if hasLink {
					lb := nodeLinkBadgeCircle(n)
					if !opt.ExportMode {
						lb = nodeLinkBadgeCircleAtScale(n, scale)
					}
					badgeHot := !opt.ExportMode && n.ID == opt.HoveredID && opt.HoveredHit == HitLinkBadge
					badgeAlpha := dimFactor
					if isEditing {
						badgeAlpha *= 0.45
					}
					paintLinkBadge(p, wxToScreen(lb.cx), wyToScreen(lb.cy), lb.r*scale, scale, pal, linkStatus, n.Depth == 0, badgeHot, badgeAlpha)
				}

				// Root node uses an outset selection pill ring; depth >= 1 uses soft selection tint.
				if isSelected && n.Depth == 0 {
					ringStroke := pal.Accent
					if dimFactor < 1 {
						ringStroke = pal.Accent.Alpha(0.45)
					}
					p.Stroke(ringR, ringStroke, ringRad, max(1.6*scale, 1.2))
				}
				if opt.DragNodeID != 0 && n.ID == opt.DragTargetID && opt.DragPos == DropInside {
					if n.Depth > 1 {
						p.Stroke(leafR, pal.Accent, leafRad, max(1.6*scale, 1.2))
					} else {
						p.Stroke(ringR, pal.Accent, ringRad, max(2*scale, 1.5))
					}
				}
			}

			// Fold badge (+hidden count) when collapsed.
			if n.Folded && len(n.Children) > 0 {
				bx := n.X - layoutFoldOffset
				if n.Dir > 0 {
					bx = n.X + n.W + layoutFoldOffset
				}
				by := n.Y
				if n.Depth > 1 {
					by = n.Y + n.H/2
				}
				if bx+layoutFoldRadius*2 >= visMinX && bx-layoutFoldRadius*2 <= visMaxX && by+layoutFoldRadius >= visMinY && by-layoutFoldRadius <= visMaxY {
					bc := branches[0]
					if n.Depth > 0 {
						bc = branches[n.Branch%len(branches)]
					}
					dimFactor := float32(1)
					if dimmed {
						dimFactor = 0.35
					} else if findDimmed {
						dimFactor = 0.28
					}
					if dimFactor < 1 {
						bc = bc.Alpha(dimFactor)
					}
					scx := wxToScreen(bx)
					scy := wyToScreen(by)
					sr := layoutFoldRadius * scale
					label := fmt.Sprintf("+%d", n.DescendantCount())
					fs := max(float32(9.5)*scale, 6)
					txtC := pal.Text
					if dimFactor < 1 {
						txtC = txtC.Alpha(dimFactor)
					}
					sp := ui.Span{Text: label, Size: fs, Weight: theme.WeightMono, Color: txtC}
					tw, th := p.MeasureText(0, sp)
					pillW := max(sr*2, tw+7*scale)
					pillH := sr * 2
					badgeR := ui.Rect{X: scx - pillW*0.5, Y: scy - pillH*0.5, W: pillW, H: pillH}
					fillC := bg
					if !opt.ExportMode && n.ID == opt.HoveredID && opt.HoveredHit == HitFoldBadge {
						fillC = pal.Hover
					}
					p.Fill(badgeR, fillC, sr)
					p.Stroke(badgeR, bc, sr, max(1.2*scale, 1))
					p.RichText(scx-tw*0.5, scy-th*0.5, 0, sp)
				}
				return
			}
			for _, ch := range n.Children {
				paintNodes(ch)
			}
		}
		paintNodes(root)

		// Pass 2.5: Floating hover/selected collapse (−) and quick-add (+) action circles.
		if !opt.ExportMode && opt.EditingID == 0 && opt.DragNodeID == 0 {
			paintActionsFor := func(nodeID int) {
				if nodeID == 0 {
					return
				}
				n := root.FindByID(nodeID)
				if n == nil || !isNodeVisibleInTree(n) {
					return
				}
				foldBtn, showFold, addBtn, showAdd := nodeActionButtonsAtScale(n, opt.ReadOnly, scale)
				col := branches[n.Branch%len(branches)]
				if n.Depth == 0 {
					col = pal.Accent
				}
				if showFold {
					hot := n.ID == opt.HoveredID && opt.HoveredHit == HitHoverFold
					paintCanvasCircleAction(p, wxToScreen(foldBtn.cx), wyToScreen(foldBtn.cy), foldBtn.r*scale, scale, pal, col, false, hot)
				}
				if showAdd {
					hot := n.ID == opt.HoveredID && opt.HoveredHit == HitHoverAdd
					paintCanvasCircleAction(p, wxToScreen(addBtn.cx), wyToScreen(addBtn.cy), addBtn.r*scale, scale, pal, pal.Accent, true, hot)
				}
			}
			if opt.SelectedID != 0 {
				paintActionsFor(opt.SelectedID)
			}
			if opt.HoveredID != 0 && opt.HoveredID != opt.SelectedID {
				paintActionsFor(opt.HoveredID)
			}
		}

		// Pass 3: Drag-and-drop insertion indicator and semantic floating ghost card.
		if draggedNode != nil {
			var targetNode *Node
			if opt.DragTargetID != 0 {
				targetNode = root.FindByID(opt.DragTargetID)
			}
			if targetNode != nil && (opt.DragPos == DropBefore || opt.DragPos == DropAfter) {
				lineY := targetNode.Y - targetNode.H/2 - 4
				if opt.DragPos == DropAfter {
					lineY = targetNode.Y + targetNode.H/2 + 4
				}
				sx1 := wxToScreen(targetNode.X)
				sx2 := wxToScreen(targetNode.X + targetNode.W)
				sy := wyToScreen(lineY)
				p.Line(sx1, sy, sx2, sy, max(2.2*scale, 2), pal.Accent)
				dotX := sx1
				if targetNode.Dir < 0 {
					dotX = sx2
				}
				dotR := max(3.5*scale, 3)
				p.Fill(ui.Rect{X: dotX - dotR, Y: sy - dotR, W: dotR * 2, H: dotR * 2}, pal.Accent, dotR)
			}

			// Floating ghost card following pointer with live drop-position semantics.
			label := truncateRunes(strings.ReplaceAll(strings.TrimSpace(draggedNode.Text), "\n", " "), 18)
			if label == "" {
				label = " "
			}
			titleSpan := ui.Span{Text: label, Size: theme.FontSmall, Weight: theme.WeightControl, Color: pal.Text}
			tw, th := p.MeasureText(0, titleSpan)

			hintText := ""
			if targetNode != nil && opt.DragPos != DropNone {
				tLbl := truncateRunes(strings.ReplaceAll(strings.TrimSpace(targetNode.Text), "\n", " "), 12)
				if tLbl == "" {
					tLbl = theme.Tr("节点", "Node")
				}
				switch opt.DragPos {
				case DropInside:
					hintText = theme.Tr("↳ 放入「"+tLbl+"」下", "↳ Into \""+tLbl+"\"")
				case DropBefore:
					hintText = theme.Tr("↑ 移到「"+tLbl+"」前", "↑ Before \""+tLbl+"\"")
				case DropAfter:
					hintText = theme.Tr("↓ 移到「"+tLbl+"」后", "↓ After \""+tLbl+"\"")
				}
			}

			var hw, hh float32
			var hintSpan ui.Span
			if hintText != "" {
				hintSpan = ui.Span{Text: hintText, Size: theme.FontMeta, Weight: theme.WeightControl, Color: pal.Accent}
				hw, hh = p.MeasureText(0, hintSpan)
			}

			contentW := max(tw, hw)
			pillW := min(max(contentW+theme.Space20, 76), 248)
			pillH := max(th+theme.Space10, theme.HeightCompact)
			if hintText != "" {
				pillH = th + hh + theme.Space12 + 2
			}
			gx := r.X + opt.DragGhostX + theme.Space12
			gy := r.Y + opt.DragGhostY - pillH*0.5
			if gx+pillW > r.X+r.W-6 {
				gx = r.X + opt.DragGhostX - pillW - theme.Space10
			}
			if gx < r.X+6 {
				gx = r.X + 6
			}
			if gy+pillH > r.Y+r.H-6 {
				gy = r.Y + r.H - pillH - 6
			}
			if gy < r.Y+6 {
				gy = r.Y + 6
			}
			pillR := ui.Rect{X: gx, Y: gy, W: pillW, H: pillH}
			p.Fill(pillR, pal.Menu.Alpha(0.95), theme.RadiusMD)
			p.Stroke(pillR, pal.Accent, theme.RadiusMD, 1.4)
			p.Clip(pillR, theme.RadiusMD, func() {
				if hintText == "" {
					p.RichText(gx+theme.Space10, gy+(pillH-th)*0.5, 0, titleSpan)
				} else {
					p.RichText(gx+theme.Space10, gy+theme.Space6, 0, titleSpan)
					p.RichText(gx+theme.Space10, gy+theme.Space6+th+2, 0, hintSpan)
				}
			})
		}
	})
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func paintLinkBadge(p *ui.Painter, cx, cy, sr, scale float32, pal theme.Palette, status LinkStatus, isRoot, isHot bool, alpha float32) {
	if sr <= 0 {
		return
	}
	r := ui.Rect{X: cx - sr, Y: cy - sr, W: sr * 2, H: sr * 2}
	var bg, border, fg ui.Color
	switch status {
	case LinkStatusBroken:
		if isRoot {
			bg = pal.Warning
			border = pal.Paper.Alpha(0.5)
			fg = pal.Paper
		} else {
			bg = pal.WarningSurface
			if isHot {
				bg = pal.Warning.Alpha(0.24)
			}
			border = pal.Warning
			fg = pal.Warning
		}
	default:
		if isRoot {
			bgAlpha := float32(0.18)
			if isHot {
				bgAlpha = 0.34
			}
			bg = pal.Paper.Alpha(bgAlpha)
			border = pal.Paper.Alpha(0.45)
			fg = pal.Paper
		} else {
			bgAlpha := float32(0.12)
			if isHot {
				bgAlpha = 0.26
			}
			bg = pal.Accent.Alpha(bgAlpha)
			border = pal.Accent.Alpha(0.38)
			if isHot {
				border = pal.Accent
			}
			fg = pal.Accent
		}
	}
	if alpha < 1 {
		bg = bg.Alpha(alpha)
		border = border.Alpha(alpha)
		fg = fg.Alpha(alpha)
	}

	p.Fill(r, bg, sr)
	p.Stroke(r, border, sr, max(1.0*scale, 0.9))

	sw := max(1.3*scale, 1.0)
	switch status {
	case LinkStatusBroken:
		// Crisp exclamation mark inside warning badge.
		var ex ui.Path
		ex.Cap(ui.RoundCap)
		ex.MoveTo(cx, cy-sr*0.42)
		ex.LineTo(cx, cy+sr*0.08)
		p.StrokePath(&ex, sw, fg)
		dotR := max(0.95*scale, 0.8)
		p.Fill(ui.Rect{X: cx - dotR, Y: cy + sr*0.36 - dotR, W: dotR * 2, H: dotR * 2}, fg, dotR)
	case LinkStatusExternal:
		// Crisp external-link northeast arrow (↗).
		d := sr * 0.38
		var arr ui.Path
		arr.Cap(ui.RoundCap).Join(ui.RoundJoin)
		arr.MoveTo(cx-d, cy+d)
		arr.LineTo(cx+d, cy-d)
		arr.MoveTo(cx-d*0.1, cy-d)
		arr.LineTo(cx+d, cy-d)
		arr.LineTo(cx+d, cy+d*0.1)
		p.StrokePath(&arr, sw, fg)
	default:
		// Crisp interlocking note-link glyph (two tilted link brackets + bridge).
		d := sr * 0.42
		var ln ui.Path
		ln.Cap(ui.RoundCap).Join(ui.RoundJoin)
		// Top-right hook
		ln.MoveTo(cx-d*0.1, cy-d*0.55)
		ln.LineTo(cx+d*0.45, cy-d*0.55)
		ln.LineTo(cx+d*0.45, cy+d*0.05)
		// Bottom-left hook
		ln.MoveTo(cx+d*0.1, cy+d*0.55)
		ln.LineTo(cx-d*0.45, cy+d*0.55)
		ln.LineTo(cx-d*0.45, cy-d*0.05)
		// Diagonal link bar
		ln.MoveTo(cx-d*0.32, cy+d*0.32)
		ln.LineTo(cx+d*0.32, cy-d*0.32)
		p.StrokePath(&ln, sw, fg)
	}
}

func paintCanvasCircleAction(p *ui.Painter, cx, cy, sr, scale float32, pal theme.Palette, accent ui.Color, isPlus, isHot bool) {
	if sr <= 0 {
		return
	}
	r := ui.Rect{X: cx - sr, Y: cy - sr, W: sr * 2, H: sr * 2}
	bg := pal.Paper
	border := accent.Alpha(0.55)
	fg := pal.TextSecondary
	if isPlus {
		fg = accent
	}
	if isHot {
		if isPlus {
			bg = accent
			border = accent
			fg = pal.Paper
		} else {
			bg = pal.Hover
			border = accent
			fg = pal.Text
		}
	}
	p.Fill(r, bg, sr)
	p.Stroke(r, border, sr, max(1.1*scale, 1.0))
	arm := sr * 0.44
	sw := max(1.4*scale, 1.1)
	var glyph ui.Path
	glyph.Cap(ui.RoundCap)
	glyph.MoveTo(cx-arm, cy)
	glyph.LineTo(cx+arm, cy)
	if isPlus {
		glyph.MoveTo(cx, cy-arm)
		glyph.LineTo(cx, cy+arm)
	}
	p.StrokePath(&glyph, sw, fg)
}
