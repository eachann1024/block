package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// TreemapEntry represents a single file or grouped breakdown item inside a TreemapItem.
type TreemapEntry struct {
	Name  string
	Path  string
	Bytes uint64
	Count int
}

// TreemapItem describes one storage or breakdown category rendered inside RenderStorageTreemap.
type TreemapItem struct {
	ID        string
	Label     string
	Detail    string
	Bytes     uint64
	FileCount int
	Color     ui.Color
	Entries   []TreemapEntry
}

// TreemapTileRect pairs a TreemapItem with its computed squarified rectangle.
type TreemapTileRect struct {
	Item TreemapItem
	Rect ui.Rect
}

// TreemapState caches layout geometry and tracks the active/hovered tile for a treemap widget.
type TreemapState struct {
	mu           sync.Mutex
	selectedID   string
	cachedValid  bool
	cachedBounds ui.Rect
	cachedItems  []TreemapItem
	cachedTiles  []TreemapTileRect
}

// SelectedID returns the currently selected category ID.
func (s *TreemapState) SelectedID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedID
}

// SetSelectedID updates the selected category ID and reports whether it changed.
func (s *TreemapState) SetSelectedID(id string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	changed := s.selectedID != id
	if changed {
		s.selectedID = id
	}
	s.mu.Unlock()
	return changed
}

// TreemapOptions customizes probe identifiers and selection callbacks for RenderStorageTreemap.
type TreemapOptions struct {
	CanvasKey       string
	CanvasProbeID   string
	DetailKey       string
	LegendKey       string
	TileProbePrefix string
	Height          float32
	OnSelect        func(id string)
}

// RenderStorageTreemap draws the interactive Squarified Treemap canvas, live hover/selection
// breakdown panel, and clickable category legend pills shared by the Performance modal and plugin pages.
func RenderStorageTreemap(
	c *ui.Context,
	state *TreemapState,
	items []TreemapItem,
	totalBytes uint64,
	opts TreemapOptions,
) {
	if state == nil || len(items) == 0 {
		return
	}
	pal := theme.Ocean()
	canvasKey := opts.CanvasKey
	if canvasKey == "" {
		canvasKey = "treemap-canvas"
	}
	detailKey := opts.DetailKey
	if detailKey == "" {
		detailKey = canvasKey + "-detail"
	}
	legendKey := opts.LegendKey
	if legendKey == "" {
		legendKey = canvasKey + "-legend"
	}
	tilePrefix := opts.TileProbePrefix
	if tilePrefix == "" {
		tilePrefix = canvasKey + "-tile-"
	}
	canvasH := opts.Height
	if canvasH <= 0 {
		canvasH = 210
	}

	selectedID := state.SelectedID()
	var active *TreemapItem
	for i := range items {
		if items[i].ID == selectedID {
			active = &items[i]
			break
		}
	}
	if active == nil {
		active = &items[0]
		selectedID = active.ID
	}

	selectID := func(id string) {
		if state.SetSelectedID(id) && opts.OnSelect != nil {
			opts.OnSelect(id)
		}
	}

	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space12).Children(func() {
		canvas := ui.Box(c.Key(canvasKey)).FillWidth().Height(canvasH).Radius(theme.RadiusMD).
			Background(pal.Shell).Border(1, pal.Hairline)
		if opts.CanvasProbeID != "" {
			Probe(canvas, opts.CanvasProbeID)
		}

		canvas.Draw(func(p *ui.Painter, r ui.Rect) {
			state.mu.Lock()
			var tiles []TreemapTileRect
			if state.cachedValid && state.cachedBounds == r && treemapItemsEqual(state.cachedItems, items) {
				tiles = state.cachedTiles
			} else {
				state.mu.Unlock()
				tiles = LayoutSquarifiedTreemap(items, r)
				itemsCopy := append([]TreemapItem(nil), items...)
				state.mu.Lock()
				state.cachedValid = true
				state.cachedBounds = r
				state.cachedItems = itemsCopy
				state.cachedTiles = tiles
			}
			state.mu.Unlock()

			for _, t := range tiles {
				tr := t.Rect
				if tr.W < 2 || tr.H < 2 {
					continue
				}
				isActive := t.Item.ID == selectedID
				bg := pal.Paper
				border := pal.Hairline
				bw := float32(1)
				if isActive {
					bg = pal.Selected
					border = pal.Accent
					bw = 1.5
				}
				p.Fill(tr, bg, theme.RadiusMD)
				p.Stroke(tr, border, theme.RadiusMD, bw)

				if tr.W >= 68 && tr.H >= 38 {
					padX := theme.Space12
					textW := max(tr.W-padX*2, 1)
					titleSpan := ui.Span{Text: t.Item.Label, Size: theme.FontBody, Weight: theme.WeightTitle, Color: pal.Text}
					p.RichText(tr.X+padX, tr.Y+theme.Space10, textW, titleSpan)
					if tr.H >= 58 {
						sub := fmt.Sprintf("%s · %s", FormatBytesIEC(t.Item.Bytes), FormatPercent(t.Item.Bytes, totalBytes))
						subSpan := ui.Span{Text: sub, Size: theme.FontSmall, Weight: theme.WeightRegular, Color: pal.TextSecondary}
						p.RichText(tr.X+padX, tr.Y+32, textW, subSpan)
					}
					if tr.H >= 96 && t.Item.FileCount > 0 {
						countText := fmt.Sprintf(theme.Tr("%d 项", "%d items"), t.Item.FileCount)
						countSpan := ui.Span{Text: countText, Size: theme.FontMeta, Weight: theme.WeightRegular, Color: pal.TextSecondary}
						p.RichText(tr.X+padX, tr.Y+tr.H-theme.Space24, textW, countSpan)
					}
				}
			}
		})

		canvas.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown && ev.Kind != ui.InputPointerMove {
				return false
			}
			state.mu.Lock()
			tiles := append([]TreemapTileRect(nil), state.cachedTiles...)
			state.mu.Unlock()
			for _, t := range tiles {
				if ev.X >= t.Rect.X && ev.X <= t.Rect.X+t.Rect.W && ev.Y >= t.Rect.Y && ev.Y <= t.Rect.Y+t.Rect.H {
					selectID(t.Item.ID)
					return ev.Kind == ui.InputPointerDown
				}
			}
			return false
		})

		// Selected / hovered category breakdown card.
		detailBox := ui.Column(c.Key(detailKey)).FillWidth().MinWidth(0).
			Padding(theme.Space12, theme.Space16).Gap(theme.Space10).
			Radius(theme.RadiusMD)
		AnimateSurfaceColors(detailBox, pal.Shell, pal.Hairline, true)
		detailBox.Children(func() {
			// Header row: Category label, description, file count, and total size.
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, active.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightTitle).TextColor(pal.Text).Shrink(0).SingleLine()
					meta := active.Detail
					if active.FileCount > 0 {
						countLbl := fmt.Sprintf(theme.Tr("共 %d 个文件", "%d files"), active.FileCount)
						if meta != "" {
							meta = meta + " · " + countLbl
						} else {
							meta = countLbl
						}
					}
					if meta != "" {
						ui.Text(c, meta).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					}
				})
				ui.Text(c, fmt.Sprintf("%s (%s)", FormatBytesIEC(active.Bytes), FormatPercent(active.Bytes, totalBytes))).
					Font("monospace").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightTitle).TextColor(pal.Text).Shrink(0).SingleLine()
			})

			// Granular file / group breakdown list.
			if len(active.Entries) == 0 {
				ui.Text(c, theme.Tr("暂无文件占用", "No files in this category")).
					FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
			} else {
				ui.Box(c).FillWidth().Height(1).Background(pal.Hairline)
				var shownBytes uint64
				var shownCount int
				ui.Column(c.Key(detailKey + "-entries")).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
					for i, ent := range active.Entries {
						e := ent
						shownBytes += e.Bytes
						cCount := e.Count
						if cCount <= 0 {
							cCount = 1
						}
						shownCount += cCount
						displayName := strings.TrimSpace(e.Name)
						if displayName == "" {
							displayName = e.Path
						}
						tip := strings.TrimSpace(e.Path)
						if tip == "" {
							tip = displayName
						}
						ui.Row(c.Key(fmt.Sprintf("%s-ent-%d", detailKey, i))).FillWidth().MinWidth(0).
							Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
								nameEl := ui.Text(c, displayName).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
									TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
								WrappedTooltip(c, nameEl, tip)
								if e.Count > 1 {
									ui.Text(c, fmt.Sprintf(theme.Tr("· %d 项", "· %d files"), e.Count)).
										FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.TextSecondary).Shrink(0).SingleLine()
								}
							})
							ui.Text(c, fmt.Sprintf("%s · %s", FormatBytesIEC(e.Bytes), FormatPercent(e.Bytes, active.Bytes))).
								Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								TextColor(pal.TextSecondary).Shrink(0).SingleLine()
						})
					}

					remFiles := active.FileCount - shownCount
					var remBytes uint64
					if active.Bytes > shownBytes {
						remBytes = active.Bytes - shownBytes
					}
					if remFiles > 0 || remBytes > 0 {
						remLabel := theme.Tr("其余文件", "Other files")
						if remFiles > 0 {
							remLabel = fmt.Sprintf(theme.Tr("其余 %d 个文件", "%d other files"), remFiles)
						}
						ui.Row(c.Key(detailKey + "-ent-rest")).FillWidth().MinWidth(0).
							Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, remLabel).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine()
							ui.Text(c, fmt.Sprintf("%s · %s", FormatBytesIEC(remBytes), FormatPercent(remBytes, active.Bytes))).
								Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								TextColor(pal.TextSecondary).Shrink(0).SingleLine()
						})
					}
				})
			}
		})

		// Clickable category pills strip (hover or click switches breakdown).
		ui.Row(c.Key(legendKey)).FillWidth().Wrap().Gap(theme.Space8).Children(func() {
			for _, item := range items {
				it := item
				isSelected := it.ID == selectedID
				tileID := tilePrefix + it.ID
				btn := ui.ButtonBase(c.Key(tileID)).Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space6).
					AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
					Label(it.Label)
				hovered := btn.Hovered()
				pressed := btn.Pressed()
				bg := pal.Card
				bd := pal.Hairline
				if isSelected {
					bg = pal.Selected
					bd = pal.Accent
				} else if pressed {
					bg = pal.Selected
				} else if hovered {
					bg = pal.Hover
				}
				AnimateSurfaceColors(btn, bg, bd, true)
				TactileCard(btn, hovered, pressed, isSelected, false)
				btn.Children(func() {
					ui.Text(c, it.Label).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
					ui.Text(c, FormatBytesIEC(it.Bytes)).Font("monospace").FontSize(theme.FontBody).
						TextColor(pal.TextSecondary).SingleLine()
				})
				Probe(btn, tileID)
				if btn.Clicked() || hovered {
					selectID(it.ID)
				}
			}
		})
	})
}

func treemapItemsEqual(a, b []TreemapItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Bytes != b[i].Bytes || a[i].FileCount != b[i].FileCount || a[i].Label != b[i].Label || a[i].Detail != b[i].Detail || a[i].Color != b[i].Color {
			return false
		}
	}
	return true
}

// LayoutSquarifiedTreemap computes a readable squarified treemap layout using log-hybrid weights
// so extreme skew (e.g. 60 GiB vs 0 B) still keeps every category visible and clickable.
func LayoutSquarifiedTreemap(items []TreemapItem, bounds ui.Rect) []TreemapTileRect {
	if len(items) == 0 || bounds.W <= 4 || bounds.H <= 4 {
		return nil
	}
	type weightedItem struct {
		item   TreemapItem
		weight float64
	}
	weighted := make([]weightedItem, len(items))
	for i, it := range items {
		w := 1.0 + math.Min(math.Log10(float64(it.Bytes)+1)/6.0, 2.0)
		weighted[i] = weightedItem{item: it, weight: w}
	}
	sort.SliceStable(weighted, func(i, j int) bool {
		return weighted[i].weight > weighted[j].weight
	})

	var totalWeight float64
	for _, it := range weighted {
		totalWeight += it.weight
	}
	if totalWeight <= 0 {
		return nil
	}

	inner := ui.Rect{
		X: bounds.X + 4,
		Y: bounds.Y + 4,
		W: max(bounds.W-8, 1),
		H: max(bounds.H-8, 1),
	}
	totalArea := float64(inner.W) * float64(inner.H)
	areas := make([]float64, len(weighted))
	for i, it := range weighted {
		areas[i] = it.weight / totalWeight * totalArea
	}

	var out []TreemapTileRect
	var squarify func(idx int, rect ui.Rect)
	squarify = func(idx int, rect ui.Rect) {
		if idx >= len(weighted) || rect.W <= 0 || rect.H <= 0 {
			return
		}
		if idx == len(weighted)-1 {
			out = append(out, TreemapTileRect{Item: weighted[idx].item, Rect: insetTreemapRect(rect, 3)})
			return
		}

		shortSide := float64(min(rect.W, rect.H))
		if shortSide <= 0 {
			return
		}
		end := idx + 1
		best := worstTreemapAspect(areas[idx:end], shortSide)
		for end < len(weighted) {
			next := worstTreemapAspect(areas[idx:end+1], shortSide)
			if next > best {
				break
			}
			best = next
			end++
		}

		var rowArea float64
		for k := idx; k < end; k++ {
			rowArea += areas[k]
		}
		if rect.W >= rect.H {
			stripW := float32(rowArea / float64(rect.H))
			if stripW > rect.W {
				stripW = rect.W
			}
			curY := rect.Y
			for k := idx; k < end; k++ {
				h := float32(areas[k] / float64(max(stripW, 1)))
				if k == end-1 {
					h = rect.Y + rect.H - curY
				}
				tileR := ui.Rect{X: rect.X, Y: curY, W: stripW, H: max(h, 1)}
				out = append(out, TreemapTileRect{Item: weighted[k].item, Rect: insetTreemapRect(tileR, 3)})
				curY += h
			}
			squarify(end, ui.Rect{X: rect.X + stripW, Y: rect.Y, W: max(rect.W-stripW, 0), H: rect.H})
		} else {
			stripH := float32(rowArea / float64(rect.W))
			if stripH > rect.H {
				stripH = rect.H
			}
			curX := rect.X
			for k := idx; k < end; k++ {
				w := float32(areas[k] / float64(max(stripH, 1)))
				if k == end-1 {
					w = rect.X + rect.W - curX
				}
				tileR := ui.Rect{X: curX, Y: rect.Y, W: max(w, 1), H: stripH}
				out = append(out, TreemapTileRect{Item: weighted[k].item, Rect: insetTreemapRect(tileR, 3)})
				curX += w
			}
			squarify(end, ui.Rect{X: rect.X, Y: rect.Y + stripH, W: rect.W, H: max(rect.H-stripH, 0)})
		}
	}

	squarify(0, inner)
	return out
}

func worstTreemapAspect(row []float64, side float64) float64 {
	if len(row) == 0 || side <= 0 {
		return math.Inf(1)
	}
	var sum, maxA float64
	minA := math.Inf(1)
	for _, a := range row {
		sum += a
		if a > maxA {
			maxA = a
		}
		if a < minA {
			minA = a
		}
	}
	if sum <= 0 || minA <= 0 {
		return math.Inf(1)
	}
	s2 := side * side
	sum2 := sum * sum
	return math.Max((s2*maxA)/sum2, sum2/(s2*minA))
}

func insetTreemapRect(r ui.Rect, pad float32) ui.Rect {
	if r.W <= pad*2 || r.H <= pad*2 {
		return r
	}
	return ui.Rect{X: r.X + pad, Y: r.Y + pad, W: r.W - pad*2, H: r.H - pad*2}
}

// FormatBytesIEC formats byte counts using binary IEC units (B, KiB, MiB, GiB, TiB).
func FormatBytesIEC(b uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	val := float64(b)
	idx := 0
	for val >= 1024.0 && idx < len(units)-1 {
		val /= 1024.0
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f %s", val, units[idx])
}

// FormatPercent formats a part/total ratio as a percentage string.
func FormatPercent(b, total uint64) string {
	if total == 0 {
		return "0.0%"
	}
	pct := float64(b) / float64(total) * 100.0
	if b > 0 && pct < 0.01 {
		return "<0.01%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}
