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
	layoutTaskBox     float32 = 13
	layoutTaskGap     float32 = 5
	layoutFoldOffset  float32 = 13
	layoutFoldRadius  float32 = 8.5
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
)

// HitResult identifies the clicked mind map element.
type HitResult struct {
	Kind HitKind
	Node *Node
}

// MeasureCache caches estimated or measured node widths so repeated pan/zoom frames are O(1).
type MeasureCache struct {
	widths map[measureKey]float32
}

type measureKey struct {
	text    string
	depth   int
	hasTask bool
}

// MeasureNodeWidth returns the horizontal DIP width of node n, caching by (text, depth, hasTask).
func (mc *MeasureCache) MeasureNodeWidth(n *Node, p *ui.Painter) float32 {
	if mc == nil {
		return estimateNodeWidth(n, p)
	}
	if mc.widths == nil {
		mc.widths = make(map[measureKey]float32, 64)
	}
	k := measureKey{text: n.Text, depth: min(n.Depth, 2), hasTask: n.Task != nil}
	if w, ok := mc.widths[k]; ok {
		return w
	}
	if len(mc.widths) > 8192 {
		clear(mc.widths)
	}
	w := estimateNodeWidth(n, p)
	mc.widths[k] = w
	return w
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

func estimateNodeWidth(n *Node, p *ui.Painter) float32 {
	text := n.Text
	if text == "" {
		text = " "
	}
	var textW float32
	if p != nil {
		sz := nodeFontSize(n.Depth)
		wt := nodeFontWeight(n.Depth)
		textW, _ = p.MeasureText(0, ui.Span{Text: text, Size: sz, Weight: wt})
	} else {
		// Fast deterministic fallback when measuring outside a Painter (e.g. hit-tests or tests).
		sz := nodeFontSize(n.Depth)
		var units float32
		for _, r := range text {
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
		textW = units * sz
	}
	var padX float32
	switch n.Depth {
	case 0:
		padX = 36
	case 1:
		padX = 24
	default:
		padX = 12
	}
	if n.Task != nil {
		padX += layoutTaskBox + layoutTaskGap
	}
	minW := float32(44)
	if n.Depth == 0 {
		minW = 76
	} else if n.Depth == 1 {
		minW = 54
	}
	return max(float32(math.Ceil(float64(textW+padX))), minW)
}

// ComputeLayout performs an O(N) MindNode-style tree layout and returns the world bounding box.
func ComputeLayout(root *Node, mode LayoutMode, p *ui.Painter, mc *MeasureCache) Bounds {
	if root == nil {
		return Bounds{}
	}

	// Pass 1: annotate Depth, Branch, W, H.
	var prep func(n, parent *Node, depth, branch int)
	prep = func(n, parent *Node, depth, branch int) {
		n.Parent = parent
		n.Depth = depth
		n.Branch = branch
		switch depth {
		case 0:
			n.H = layoutRootH
		case 1:
			n.H = layoutPrimaryH
		default:
			n.H = layoutLeafH
		}
		n.W = mc.MeasureNodeWidth(n, p)
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
				by = n.Y + layoutLeafH/2
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

// HitTest finds which node, task checkbox, or fold badge lies under world point (wx, wy).
func HitTest(root *Node, wx, wy float32) HitResult {
	if root == nil {
		return HitResult{}
	}
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
				by = n.Y + layoutLeafH/2
			}
			dx, dy := wx-bx, wy-by
			r := layoutFoldRadius + 4
			if dx*dx+dy*dy <= r*r {
				hit = HitResult{Kind: HitFoldBadge, Node: n}
				return true
			}
		}

		// Check node rect (with generous 2px vertical hit padding).
		top := n.Y - n.H/2 - 2
		bot := n.Y + n.H/2 + 2
		if wx >= n.X-2 && wx <= n.X+n.W+2 && wy >= top && wy <= bot {
			if n.Task != nil {
				tx := nodeContentLeft(n)
				ty := nodeTextCenterY(n)
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

func nodeContentLeft(n *Node) float32 {
	switch n.Depth {
	case 0:
		return n.X + 18
	case 1:
		return n.X + 12
	default:
		return n.X + 4
	}
}

func nodeTextCenterY(n *Node) float32 {
	if n.Depth > 1 {
		return n.Y - 1
	}
	return n.Y
}

// PaintOptions configures how PaintScene renders a mind map onto a ui.Painter.
type PaintOptions struct {
	SelectedID int
	HoveredID  int
	EditingID  int
	Palette    theme.Palette
	// Background fills the viewport before painting if transparent is false.
	FillBackground bool
	Background     ui.Color
	// ForceLabels keeps node text visible even at small export scales.
	ForceLabels bool
}

// PaintScene renders the entire mind map (edges, nodes, checkboxes, links, fold badges,
// selection ring) into viewport r using Camera cam. Uses viewport culling so 10,000+ nodes
// render smoothly at 60fps. Shared by both the live interactive canvas and PNG/PDF export.
func PaintScene(p *ui.Painter, root *Node, r ui.Rect, cam Camera, opt PaintOptions) {
	if p == nil || root == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	pal := opt.Palette
	if opt.FillBackground {
		p.Fill(r, opt.Background, 0)
	}
	branches := pal.MindMapBranchColors()
	scale := cam.Scale
	if scale <= 0 {
		scale = 1
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
			if n.Parent != nil {
				par := n.Parent
				d := n.Dir
				px := par.X
				if d > 0 {
					px = par.X + par.W
				}
				py := par.Y
				if par.Depth > 1 {
					py = par.Y + layoutLeafH/2
				}
				nx := n.X + n.W
				if d > 0 {
					nx = n.X
				}
				ny := n.Y
				if n.Depth > 1 {
					ny = n.Y + layoutLeafH/2
				}

				edgeMinX := min(px, min(n.X, n.X+n.W))
				edgeMaxX := max(px, max(n.X, n.X+n.W))
				edgeMinY := min(py, ny)
				edgeMaxY := max(py, ny)
				if edgeMaxX >= visMinX && edgeMinX <= visMaxX && edgeMaxY >= visMinY && edgeMinY <= visMaxY {
					midX := (px + nx) * 0.5
					var path ui.Path
					path.Cap(ui.RoundCap).Join(ui.RoundJoin)
					path.MoveTo(wxToScreen(px), wyToScreen(py))
					path.CubeTo(
						wxToScreen(midX), wyToScreen(py),
						wxToScreen(midX), wyToScreen(ny),
						wxToScreen(nx), wyToScreen(ny),
					)
					if n.Depth > 1 {
						endX := n.X
						if d > 0 {
							endX = n.X + n.W
						}
						path.LineTo(wxToScreen(endX), wyToScreen(ny))
					}
					sw := float32(1.6)
					if n.Depth == 1 {
						sw = 2.2
					}
					p.StrokePath(&path, max(sw*scale, 1), col)
				}
			}

			if n.Folded && len(n.Children) > 0 {
				bx := n.X - layoutFoldOffset
				startX := n.X
				if n.Dir > 0 {
					bx = n.X + n.W + layoutFoldOffset
					startX = n.X + n.W
				}
				by := n.Y
				if n.Depth > 1 {
					by = n.Y + layoutLeafH/2
				}
				if max(startX, bx) >= visMinX && min(startX, bx) <= visMaxX && by >= visMinY && by <= visMaxY {
					bc := branches[0]
					if n.Depth > 0 {
						bc = branches[n.Branch%len(branches)]
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

		// Pass 2: Nodes, checkboxes, labels, fold badges, hover & selection rings.
		var paintNodes func(n *Node)
		paintNodes = func(n *Node) {
			col := branches[n.Branch%len(branches)]
			nodeVisible := n.X+n.W >= visMinX && n.X <= visMaxX && n.Y+n.H/2 >= visMinY && n.Y-n.H/2 <= visMaxY

			if nodeVisible {
				sx := wxToScreen(n.X)
				sy := wyToScreen(n.Y - n.H/2)
				sw := n.W * scale
				sh := n.H * scale
				rect := ui.Rect{X: sx, Y: sy, W: sw, H: sh}

				switch n.Depth {
				case 0:
					p.Fill(rect, pal.Text, sh*0.5)
				case 1:
					rad := theme.RadiusMD * scale
					p.Fill(rect, col.Alpha(0.13), rad)
					p.Stroke(rect, col, rad, max(1.2*scale, 1))
				default:
					if n.ID == opt.HoveredID && n.ID != opt.SelectedID {
						p.Fill(rect, pal.Hover, theme.RadiusSM*scale)
					}
				}

				if n.ID != opt.EditingID {
					tx := nodeContentLeft(n)
					ty := nodeTextCenterY(n)
					if n.Task != nil {
						cbX := wxToScreen(tx)
						cbY := wyToScreen(ty - layoutTaskBox/2)
						cbS := layoutTaskBox * scale
						cbR := ui.Rect{X: cbX, Y: cbY, W: cbS, H: cbS}
						cbRad := float32(3.5) * scale
						if *n.Task {
							p.Fill(cbR, pal.Accent, cbRad)
							var ck ui.Path
							ck.Cap(ui.RoundCap).Join(ui.RoundJoin)
							ck.MoveTo(cbX+cbS*0.24, cbY+cbS*0.52)
							ck.LineTo(cbX+cbS*0.43, cbY+cbS*0.71)
							ck.LineTo(cbX+cbS*0.76, cbY+cbS*0.30)
							p.StrokePath(&ck, max(1.5*scale, 1), pal.Paper)
						} else {
							p.Fill(cbR, pal.Paper, cbRad)
							p.Stroke(cbR, pal.TextSecondary, cbRad, max(1.2*scale, 1))
						}
						tx += layoutTaskBox + layoutTaskGap
					}

					txtColor := pal.Text
					if n.Depth == 0 {
						txtColor = pal.Paper
					} else if strings.TrimSpace(n.Link) != "" {
						txtColor = pal.Accent
					}
					if n.Task != nil && *n.Task {
						txtColor = txtColor.Alpha(0.55)
					}

					fontSize := nodeFontSize(n.Depth) * scale
					if opt.ForceLabels || fontSize >= 5 {
						if fontSize < 2.5 {
							fontSize = 2.5
						}
						sp := ui.Span{
							Text:   n.Text,
							Size:   fontSize,
							Weight: nodeFontWeight(n.Depth),
							Color:  txtColor,
						}
						_, th := p.MeasureText(0, sp)
						p.RichText(wxToScreen(tx), wyToScreen(ty)-th*0.5, 0, sp)
					}
				}

				// Selection ring (3px outset, Accent stroke).
				if n.ID == opt.SelectedID {
					pad := float32(3) * scale
					ringR := ui.Rect{X: sx - pad, Y: sy - pad, W: sw + pad*2, H: sh + pad*2}
					ringRad := theme.RadiusLG * scale
					if n.Depth == 0 {
						ringRad = ringR.H * 0.5
					}
					p.Stroke(ringR, pal.Accent, ringRad, max(1.6*scale, 1.2))
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
					by = n.Y + layoutLeafH/2
				}
				if bx+layoutFoldRadius >= visMinX && bx-layoutFoldRadius <= visMaxX && by+layoutFoldRadius >= visMinY && by-layoutFoldRadius <= visMaxY {
					bc := branches[0]
					if n.Depth > 0 {
						bc = branches[n.Branch%len(branches)]
					}
					scx := wxToScreen(bx)
					scy := wyToScreen(by)
					sr := layoutFoldRadius * scale
					badgeR := ui.Rect{X: scx - sr, Y: scy - sr, W: sr * 2, H: sr * 2}
					bg := opt.Background
					if bg.A == 0 {
						bg = pal.CodeBG
					}
					p.Fill(badgeR, bg, sr)
					p.Stroke(badgeR, bc, sr, max(1.2*scale, 1))
					label := fmt.Sprintf("%d", n.DescendantCount())
					fs := max(float32(10)*scale, 6)
					sp := ui.Span{Text: label, Size: fs, Weight: theme.WeightMono, Color: pal.Text}
					tw, th := p.MeasureText(0, sp)
					p.RichText(scx-tw*0.5, scy-th*0.5, 0, sp)
				}
				return
			}
			for _, ch := range n.Children {
				paintNodes(ch)
			}
		}
		paintNodes(root)
	})
}
