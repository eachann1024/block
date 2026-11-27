package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

var outlineFadeAnim = ui.Animation{
	Duration:  theme.OutlineFadeDurationMs * time.Millisecond,
	Ease:      ui.CubicBezier(0.2, 0.8, 0.25, 1),
	NonMotion: true,
}

// sampleOutlineTicks returns sorted original indices into items, capped at maxTicks.
// When downsampling, first, last, activeIdx, and hoverIdx are always preserved.
func sampleOutlineTicks(items []model.OutlineItem, activeIdx, hoverIdx, maxTicks int) []int {
	n := len(items)
	if n == 0 || maxTicks <= 0 {
		return nil
	}
	if n <= maxTicks {
		out := make([]int, n)
		for i := range n {
			out[i] = i
		}
		return out
	}
	if maxTicks == 1 {
		if activeIdx >= 0 && activeIdx < n {
			return []int{activeIdx}
		}
		return []int{0}
	}

	grid := make([]int, maxTicks)
	for k := range maxTicks {
		grid[k] = int(math.Round(float64(k) * float64(n-1) / float64(maxTicks-1)))
	}

	mandatory := []int{0, n - 1}
	if activeIdx >= 0 && activeIdx < n {
		mandatory = append(mandatory, activeIdx)
	}
	if hoverIdx >= 0 && hoverIdx < n {
		mandatory = append(mandatory, hoverIdx)
	}

	locked := make(map[int]bool, len(mandatory))
	for _, m := range mandatory {
		locked[m] = true
	}

	present := make(map[int]bool, maxTicks)
	for _, idx := range grid {
		present[idx] = true
	}

	for _, m := range mandatory {
		if present[m] {
			continue
		}
		bestSlot := -1
		bestDist := n + 1
		for slot, cur := range grid {
			if locked[cur] {
				continue
			}
			dist := cur - m
			if dist < 0 {
				dist = -dist
			}
			if dist < bestDist {
				bestDist = dist
				bestSlot = slot
			}
		}
		if bestSlot >= 0 {
			delete(present, grid[bestSlot])
			grid[bestSlot] = m
			present[m] = true
		}
	}

	slices.Sort(grid)
	return grid
}

func (s *Session) outlineRightMargin() float32 {
	if s == nil || s.outlineForceNarrow {
		return 0
	}
	editorW := s.viewportWidth
	if editorW <= 0 {
		editorW = s.editorRect.W
	}
	return max((editorW-theme.ScrollGutter-s.outerFlowWidth())*0.5, 0)
}

func (s *Session) outlineVisible() bool {
	if s == nil || s.minimal || s.embedded || s.outlineRightMargin() < theme.OutlineRailMinRightMargin {
		return false
	}
	return len(s.Outline()) > 0
}

func (s *Session) resetOutlineState(clearAll bool) {
	if s == nil {
		return
	}
	s.clearOutlineOverlay(false)
	s.outlineSuppressReopen = false
	s.outlineRailHovered = false
	if clearAll {
		s.outlineListScroll = ui.ScrollState{}
		s.outlineFilter = ""
		s.outlineJumpLockID = 0
		s.outlineJumpLockUntil = time.Time{}
		s.outlineLastClickedID = 0
		s.outlineWakeUntil = time.Time{}
		s.outlineIdleFadeAt = time.Time{}
		s.outlineWasAwake = false
		s.outlineLastScrollY = 0
		s.outlineScrollInit = false
		s.outlineLastFocusBlock = 0
		s.outlineLastDocRev = 0
		s.outlinePreferScroll = false
	}
}

func (s *Session) clearOutlineOverlay(suppressReopen bool) {
	if s == nil {
		return
	}
	s.outlineMode = outlineModeClosed
	s.outlineHoverBlockID = 0
	s.outlineCardBlockID = 0
	s.outlineListDwellID = 0
	s.outlineListDwellAt = time.Time{}
	s.outlineLeaveAt = time.Time{}
	s.outlineFocusPending = false
	s.outlineFilter = ""
	s.outlineListScroll.Y = 0
	if suppressReopen {
		s.outlineSuppressReopen = true
	}
}

func (s *Session) closeOutline(suppressReopen bool) {
	if s == nil || s.outlineMode == outlineModePinned {
		return
	}
	s.clearOutlineOverlay(suppressReopen)
}

func outlineIndexByBlockID(items []model.OutlineItem, id model.BlockID) int {
	if id == 0 {
		return -1
	}
	for i := range items {
		if items[i].BlockID == id {
			return i
		}
	}
	return -1
}

func (s *Session) resolveActiveOutlineIndex(items []model.OutlineItem, now time.Time) int {
	if len(items) == 0 {
		return -1
	}
	if s.outlineJumpLockID != 0 && (s.outlineJumpLockUntil.IsZero() || now.Before(s.outlineJumpLockUntil)) {
		if idx := outlineIndexByBlockID(items, s.outlineJumpLockID); idx >= 0 {
			return idx
		}
	}
	if s.outlinePreferScroll {
		if s.bodyScroll.MaxY > 0 && s.bodyScroll.Y >= s.bodyScroll.MaxY-4 {
			return len(items) - 1
		}
		vpTop := s.scrollRect.Y + max(s.jumpTopClearance(), theme.Space24)
		sawRect := false
		scrollIdx := 0
		for i, it := range items {
			if r, ok := s.blockRect(it.BlockID); ok {
				sawRect = true
				if r.Y <= vpTop {
					scrollIdx = i
				}
			}
		}
		if sawRect {
			return scrollIdx
		}
	}
	if s.outlineLastClickedID != 0 {
		if idx := outlineIndexByBlockID(items, s.outlineLastClickedID); idx >= 0 {
			return idx
		}
	}

	anchorIdx := s.focusedBlock
	if s.jump.block != 0 {
		if hi := s.blockIndexByID(s.jump.block); hi >= 0 {
			anchorIdx = hi
		}
	}
	activeIdx := 0
	for i, it := range items {
		if it.BlockIndex <= anchorIdx {
			activeIdx = i
		}
	}
	return activeIdx
}

func (s *Session) syncOutlineTracking(c *ui.Context, now time.Time) {
	if s.outlineJumpLockID != 0 {
		if s.outlineJumpLockUntil.IsZero() {
			s.outlineJumpLockUntil = now.Add(theme.OutlineJumpLockMs * time.Millisecond)
			s.outlineWakeUntil = now.Add(theme.OutlineScrollWakeMs * time.Millisecond)
			s.outlineIdleFadeAt = time.Time{}
		}
		if now.Before(s.outlineJumpLockUntil) {
			if !testing.Testing() {
				c.After(s.outlineJumpLockUntil.Sub(now) + 5*time.Millisecond)
			}
		} else {
			s.outlineJumpLockID = 0
			s.outlineJumpLockUntil = time.Time{}
		}
	}

	jumpLocked := s.outlineJumpLockID != 0 && now.Before(s.outlineJumpLockUntil)
	if !s.outlineScrollInit {
		s.outlineScrollInit = true
		s.outlineLastScrollY = s.bodyScroll.Y
		s.outlineLastFocusBlock = s.focusedBlock
		s.outlineLastDocRev = s.docRev
	} else if math.Abs(float64(s.bodyScroll.Y-s.outlineLastScrollY)) > 0.5 {
		s.outlineLastScrollY = s.bodyScroll.Y
		s.outlineWakeUntil = now.Add(theme.OutlineScrollWakeMs * time.Millisecond)
		s.outlineIdleFadeAt = time.Time{}
		if !jumpLocked {
			s.outlinePreferScroll = true
			s.outlineLastClickedID = 0
		}
	}

	if s.docRev != s.outlineLastDocRev {
		s.outlineLastDocRev = s.docRev
		s.outlineJumpLockID = 0
		s.outlineJumpLockUntil = time.Time{}
		jumpLocked = false
		s.outlinePreferScroll = false
		s.outlineLastClickedID = 0
	}
	if !jumpLocked && s.focusedBlock != s.outlineLastFocusBlock {
		s.outlineLastFocusBlock = s.focusedBlock
		s.outlinePreferScroll = false
		s.outlineLastClickedID = 0
	}
}

func (s *Session) outlineOpacity(c *ui.Context, el ui.Element, now time.Time) float32 {
	hoverOrOpen := s.outlineRailHovered || s.outlineMode != outlineModeClosed
	scrollingWake := !s.outlineWakeUntil.IsZero() && now.Before(s.outlineWakeUntil)
	if scrollingWake && !testing.Testing() {
		c.After(s.outlineWakeUntil.Sub(now) + 5*time.Millisecond)
	} else if !s.outlineWakeUntil.IsZero() && !now.Before(s.outlineWakeUntil) {
		s.outlineWakeUntil = time.Time{}
	}

	target := theme.OutlineIdleOpacity
	if hoverOrOpen {
		s.outlineWasAwake = true
		s.outlineIdleFadeAt = time.Time{}
		target = 1.0
	} else if scrollingWake {
		s.outlineIdleFadeAt = time.Time{}
		target = 1.0
	} else if testing.Testing() {
		s.outlineWasAwake = false
		s.outlineIdleFadeAt = time.Time{}
	} else {
		if s.outlineWasAwake {
			s.outlineWasAwake = false
			s.outlineIdleFadeAt = now.Add(theme.OutlineIdleFadeDelayMs * time.Millisecond)
		}
		if !s.outlineIdleFadeAt.IsZero() {
			if rem := s.outlineIdleFadeAt.Sub(now); rem > 0 {
				target = 1.0
				c.After(rem + 5*time.Millisecond)
			} else {
				s.outlineIdleFadeAt = time.Time{}
			}
		}
	}
	if testing.Testing() {
		return target
	}
	return el.AnimateTo("outlineRailOpacity", target, outlineFadeAnim)
}

func (s *Session) renderOutlineRail(c *ui.Context) {
	if !s.outlineVisible() {
		s.closeOutline(false)
		s.outlineRailHovered = false
		if s.outlineMode != outlineModePinned {
			s.outlineWasAwake = false
			s.outlineIdleFadeAt = time.Time{}
		}
		return
	}
	items := s.Outline()
	if len(items) == 0 {
		s.closeOutline(false)
		s.outlineRailHovered = false
		if s.outlineMode != outlineModePinned {
			s.outlineWasAwake = false
			s.outlineIdleFadeAt = time.Time{}
		}
		return
	}

	now := c.Now()
	s.syncOutlineTracking(c, now)

	activeIdx := s.resolveActiveOutlineIndex(items, now)
	hoverIdx := outlineIndexByBlockID(items, s.outlineHoverBlockID)

	pal := theme.Ocean()
	vp, winW, winH := s.overlayViewport(c)
	maxTicks := theme.OutlineMaxTicks
	if vp.H > 0 {
		maxTicks = min(theme.OutlineMaxTicks, max(1, int((vp.H-theme.Space24)/theme.OutlineTickHitHeight)))
	}
	ticks := sampleOutlineTicks(items, activeIdx, hoverIdx, maxTicks)
	listMode := len(items) > theme.OutlineMaxTicks

	panelMaxH := clampFloat32(vp.H*theme.OutlinePanelViewportRatio, theme.OutlinePanelMinHeight, theme.OutlinePanelMaxHeight)
	if vp.H > 0 {
		panelMaxH = min(panelMaxH, max(vp.H-theme.Space16, 0))
	}

	railH := float32(len(ticks)) * theme.OutlineTickHitHeight

	var (
		popupEl            ui.Element
		rail               ui.Element
		popupHovered       bool
		popupPendingLayout bool
		bridgeHovered      bool
		railHovered        bool
		hoveredTickID      model.BlockID
		listHoveredID      model.BlockID
		inputFocused       bool
		expandRequested    bool
	)

	rightEdge := max(winW-(vp.X+vp.W), 0) + theme.OutlineRailRightOffset
	bottomEdge := max(winH-(vp.Y+vp.H), 0)
	popupRight := rightEdge + theme.OutlineRailWidth + theme.OutlineBridgeWidth
	popupStageW := max(theme.OutlineHoverCardWidth, theme.OutlinePanelWidth)

	ui.Overlay(c, func() {
		popupStage := ui.Column(c.Key("outline-popup-stage")).
			Absolute().
			Top(vp.Y).
			Bottom(bottomEdge).
			Right(popupRight).
			Width(popupStageW).
			Justify(ui.Center).
			AlignItems(ui.End).
			PassThrough()
		appUi.Probe(popupStage, "outline-popup-stage")
		popupStage.Children(func() {
			switch s.outlineMode {
			case outlineModeHoverCard:
				card := s.renderOutlineHoverCard(c, pal, items, activeIdx, &expandRequested)
				popupEl = card
				popupPendingLayout = card.Bounds().W <= 0
				popupHovered = card.Hovered() || s.pointerInElement(card)
			case outlineModePanel, outlineModePinned:
				var panel ui.Element
				panel, listHoveredID, inputFocused = s.renderOutlinePanel(c, pal, items, activeIdx, panelMaxH)
				popupEl = panel
				popupPendingLayout = panel.Bounds().W <= 0
				popupHovered = panel.Hovered() || s.pointerInElement(panel)
			}
		})

		if s.outlineMode != outlineModeClosed {
			bridgeH := max(railH, 96)
			if s.outlineMode == outlineModePanel || s.outlineMode == outlineModePinned {
				estPanelH := min(panelMaxH, float32(len(items))*theme.HeightCompact+60)
				bridgeH = max(railH, estPanelH*0.5)
			}
			bridgeStage := ui.Column(c.Key("outline-bridge-stage")).
				Absolute().
				Top(vp.Y).
				Bottom(bottomEdge).
				Right(rightEdge + theme.OutlineRailWidth).
				Width(theme.OutlineBridgeWidth).
				Justify(ui.Center).
				AlignItems(ui.Center).
				PassThrough()
			bridgeStage.Children(func() {
				bridge := ui.Box(c.Key("outline-bridge")).Width(theme.OutlineBridgeWidth).Height(bridgeH).PassThrough()
				bridgeHovered = s.pointerInElement(bridge)
			})
		}

		outer := ui.Column(c.Key("outline-overlay-root")).
			Absolute().
			Top(vp.Y).
			Bottom(bottomEdge).
			Right(rightEdge).
			Justify(ui.Center).
			AlignItems(ui.End).
			PassThrough()
		outer.Children(func() {
			rail, railHovered, hoveredTickID = s.renderOutlineTicks(c, pal, items, ticks, activeIdx, hoverIdx)
			rail.Opacity(s.outlineOpacity(c, rail, now))
		})
	})

	if expandRequested {
		s.outlineMode = outlineModePanel
		s.outlineLeaveAt = time.Time{}
		c.Invalidate()
		return
	}

	if (s.outlineMode == outlineModeHoverCard || s.outlineMode == outlineModePanel) && popupEl.Valid() && popupEl.PressedOutside() && (!rail.Valid() || rail.PressedOutside()) {
		s.clearOutlineOverlay(false)
		c.Invalidate()
		return
	}

	inHotZone := popupHovered || bridgeHovered || railHovered || hoveredTickID != 0 || listHoveredID != 0
	s.updateOutlineHoverState(c, now, items, listMode, inHotZone, popupPendingLayout, railHovered, hoveredTickID, listHoveredID, inputFocused)
}

func (s *Session) pointerInElement(el ui.Element) bool {
	if !el.Valid() {
		return false
	}
	r := el.Bounds()
	if r.W <= 0 || r.H <= 0 {
		return false
	}
	pt, over := s.windowPointerStatus()
	return over && pt.X >= r.X && pt.X <= r.X+r.W && pt.Y >= r.Y && pt.Y <= r.Y+r.H
}

func (s *Session) renderOutlineHoverCard(c *ui.Context, pal theme.Palette, items []model.OutlineItem, activeIdx int, expandOut *bool) ui.Element {
	cardIdx := outlineIndexByBlockID(items, s.outlineCardBlockID)
	if cardIdx < 0 {
		cardIdx = max(activeIdx, 0)
		s.outlineCardBlockID = items[cardIdx].BlockID
	}
	cardItem := items[cardIdx]

	card := appUi.Elevate(
		ui.Column(c.Key("outline-hovercard")).
			Width(theme.OutlineHoverCardWidth).
			Padding(theme.Space10, theme.Space12, theme.Space10, theme.Space12).
			Gap(theme.Space6).
			Radius(theme.RadiusXL).
			Background(pal.Menu).
			Border(1, pal.MenuBorder).
			TransformOrigin(1, 0.5).
			Transition(appUi.OutlinePopoverTransition()).
			HandleInput(s.forwardRailScroll),
		theme.ElevationPopover,
	)
	appUi.Probe(card, "outline-hovercard")

	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
			ui.Box(c).Padding(theme.Space2, theme.Space6).Radius(theme.RadiusXS).Background(pal.Selected).Children(func() {
				ui.Text(c, fmt.Sprintf("H%d", cardItem.Level)).
					FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
			})
			ui.Text(c, fmt.Sprintf("%d / %d", cardIdx+1, len(items))).
				FontSize(theme.FontMeta).TextColor(pal.TextSecondary).Grow(1).SingleLine()

			expandBtn := appUi.IconButton(c, icons.AiList, appUi.IconButtonOptions{
				ID:       "outline-expand-btn",
				Label:    theme.Tr("展开完整目录", "Expand outline"),
				Tooltip:  theme.Tr("展开完整目录", "Expand outline"),
				Size:     theme.HeightMicro,
				IconSize: theme.IconInline,
			})
			if expandBtn.Clicked() {
				*expandOut = true
			}
		})

		body := ui.ButtonBase(c.Key("outline-hovercard-body")).
			FillWidth().Padding(theme.Space4, theme.Space6).Radius(theme.RadiusMD).
			Cursor(ui.CursorPointer).KeepFocus().FocusRing(false).Label(cardItem.Title)
		bodyBG := ui.Transparent
		if body.Pressed() {
			bodyBG = pal.Selected
		} else if body.Hovered() {
			bodyBG = pal.Hover
		}
		appUi.AnimateSurfaceColors(body, bodyBG, ui.Transparent, false)
		body.Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space4).Children(func() {
				ui.Text(c, cardItem.Title).
					FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text).MaxLines(2)
				if cardItem.Preview != "" {
					ui.Text(c, cardItem.Preview).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).MaxLines(3)
				}
			})
		})
		if body.Clicked() {
			s.JumpToBlock(cardItem.BlockIndex)
			c.Invalidate()
		}
	})

	return card
}

func (s *Session) renderOutlinePanel(c *ui.Context, pal theme.Palette, items []model.OutlineItem, activeIdx int, panelMaxH float32) (ui.Element, model.BlockID, bool) {
	pinned := s.outlineMode == outlineModePinned
	panel := appUi.Elevate(
		ui.Column(c.Key("outline-popover")).
			Width(theme.OutlinePanelWidth).MaxHeight(panelMaxH).
			Radius(theme.RadiusXL).
			Background(pal.Menu).
			Border(1, pal.MenuBorder).
			Clip().
			TransformOrigin(1, 0.5).
			Transition(appUi.OutlinePopoverTransition()),
		theme.ElevationPopover,
	)
	appUi.Probe(panel, "outline-popover")

	var (
		listHoveredID model.BlockID
		inputFocused  bool
	)

	panel.Children(func() {
		ui.Row(c.Key("outline-panel-header")).FillWidth().
			Padding(theme.Space8, theme.Space8, theme.Space6, theme.Space8).
			Gap(theme.Space6).AlignItems(ui.Center).
			Children(func() {
				inFocusNow := s.outlineInputHandle.Focused(c)
				inBorder := pal.Hairline
				if inFocusNow {
					inBorder = pal.Accent
				}
				ui.Row(c).Grow(1).MinWidth(0).Height(theme.HeightCompact).
					PaddingX(theme.Space8).Gap(theme.Space6).AlignItems(ui.Center).
					Radius(theme.RadiusMD).Background(pal.Paper).Border(1, inBorder).
					Children(func() {
						icons.DrawAnimated(c, icons.FindSearch, theme.IconMicro, pal.DuoIconColors(pal.TextSecondary), s.outlineFilter != "")
						in := ui.TextInputBase(c.Key("outline-filter-input"), &s.outlineFilter).
							Grow(1).MinWidth(0).Height(theme.HeightCompact).Padding(0).
							FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.Text).CaretColor(pal.Accent).
							Placeholder(theme.Tr("筛选目录…", "Filter headings…")).
							PlaceholderColor(pal.Placeholder).
							Label(theme.Tr("筛选目录", "Filter headings")).
							Bind(&s.outlineInputHandle)
						appUi.Probe(in, "outline-filter-input")
						in.OnTextKey(func(k ui.TextKey) bool {
							if k.Key == ui.KeyEscape {
								if s.outlineFilter != "" {
									s.outlineFilter = ""
									s.outlineListScroll.Y = 0
									c.Invalidate()
									return true
								}
								s.clearOutlineOverlay(true)
								if s.doc != nil && len(s.doc.Blocks) > 0 {
									if s.needsDetachedSelectionFocus() {
										s.multiSelFocusPending = true
										s.focusPending = false
									} else if !s.readOnly {
										s.EnsureEditorFocus()
										s.focusPreventScroll = true
									}
								}
								c.Invalidate()
								return true
							}
							return false
						})
						if s.outlineFocusPending {
							in.Focus()
							s.outlineFocusPending = false
						}
						if in.Changed() {
							s.outlineListScroll.Y = 0
						}
						inputFocused = in.Focused()
					})

				pinTooltip := theme.Tr("钉住目录", "Pin outline")
				if pinned {
					pinTooltip = theme.Tr("取消钉住", "Unpin outline")
				}
				pinBtn := appUi.IconButton(c, icons.FileLock, appUi.IconButtonOptions{
					ID:       "outline-pin-btn",
					Label:    pinTooltip,
					Tooltip:  pinTooltip,
					Size:     theme.HeightCompact,
					IconSize: theme.IconInline,
					Active:   pinned,
				})
				if pinBtn.Clicked() {
					if pinned {
						s.outlineMode = outlineModePanel
						s.outlineLeaveAt = time.Time{}
						if inputFocused {
							pinBtn.Focus(ui.FocusOptions{PreventScroll: true})
							inputFocused = false
						}
					} else {
						s.outlineMode = outlineModePinned
						s.outlineLeaveAt = time.Time{}
					}
					c.Invalidate()
				}
			})

		ui.Box(c).FillWidth().Height(1).Background(pal.MenuSeparator)

		headerH := theme.HeightCompact + theme.Space8 + theme.Space6 + 3
		scrollMaxH := max(panelMaxH-headerH, 0)
		appUi.Scroll(c.Key("outline-scroll"), &s.outlineListScroll).FillWidth().MaxHeight(scrollMaxH).Children(func() {
			ui.Column(c.Key("outline-list")).FillWidth().Padding(theme.Space6).Gap(theme.Space2).Children(func() {
				query := strings.ToLower(strings.TrimSpace(s.outlineFilter))
				matched := 0
				for origIdx, it := range items {
					if query != "" && !strings.Contains(strings.ToLower(it.Title), query) {
						continue
					}
					matched++
					item := it
					isActive := origIdx == activeIdx
					indent := outlinePanelIndent(item.Level)
					row := ui.ButtonBase(c.Key(origIdx)).
						FillWidth().Height(theme.HeightCompact).
						Padding(0, theme.Space8, 0, indent).Gap(theme.Space6).
						AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
						KeepFocus().FocusRing(false).Label(item.Title)
					rowHovered := row.Hovered()
					rowPressed := row.Pressed()
					if rowHovered {
						listHoveredID = item.BlockID
					}

					rowBG, rowBD := ui.Transparent, ui.Transparent
					textCol := pal.TextSecondary
					fontW := theme.WeightRegular
					if isActive {
						rowBG, rowBD = pal.Selected, pal.SelectedBorder
						textCol = pal.Text
						fontW = theme.WeightControl
					} else if rowPressed {
						rowBG = pal.Selected
						textCol = pal.Text
					} else if rowHovered {
						rowBG = pal.Hover
						textCol = pal.Text
					}
					appUi.AnimateSurfaceColors(row, rowBG, rowBD, isActive)

					row.Children(func() {
						barCol := ui.Transparent
						if isActive {
							barCol = pal.Accent
						}
						ui.Box(c).Width(theme.OutlineActiveBarWidth).Height(theme.OutlineActiveBarHeight).
							Radius(theme.RadiusPill).Background(barCol).Shrink(0)
						ui.Text(c, item.Title).FontSize(theme.FontSmall).FontWeight(fontW).
							TextColor(textCol).Grow(1).SingleLine()
					})
					appUi.Probe(row, fmt.Sprintf("outline-item-%d", origIdx))
					if row.Clicked() {
						s.JumpToBlock(item.BlockIndex)
						c.Invalidate()
					}
				}
				if matched == 0 {
					ui.Row(c).FillWidth().Padding(theme.Space12, theme.Space8).Justify(ui.Center).Children(func() {
						ui.Text(c, theme.Tr("无匹配标题", "No matching headings")).
							FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					})
				}
			})
		})
	})

	return panel, listHoveredID, inputFocused
}

func (s *Session) renderOutlineTicks(c *ui.Context, pal theme.Palette, items []model.OutlineItem, ticks []int, activeIdx, hoverIdx int) (ui.Element, bool, model.BlockID) {
	rail := ui.Column(c.Key("outline-rail")).
		Width(theme.OutlineRailWidth).
		AlignItems(ui.End).
		Gap(0).
		HandleInput(s.forwardRailScroll)
	appUi.Probe(rail, "outline-rail")

	var hoveredBlockID model.BlockID
	rail.Children(func() {
		for _, origIdx := range ticks {
			item := items[origIdx]
			isActive := origIdx == activeIdx
			isHover := origIdx == hoverIdx
			tickBtn := ui.ButtonBase(c.Key(origIdx)).
				FillWidth().
				Height(theme.OutlineTickHitHeight).
				Padding(0, theme.OutlineTickHitPadX).
				AlignItems(ui.Center).
				Justify(ui.End).
				Cursor(ui.CursorPointer).
				KeepFocus().
				FocusRing(false).
				Label(item.Title)

			btnHovered := tickBtn.Hovered()
			if btnHovered {
				hoveredBlockID = item.BlockID
				isHover = true
			}

			barW := theme.OutlineTickWidthIdle
			barCol := pal.OutlineBarDim
			if isHover {
				barW = theme.OutlineTickWidthLit
				barCol = pal.Text
			} else if isActive {
				barW = theme.OutlineTickWidthLit
				barCol = pal.OutlineBarLit
			}

			tickBtn.Children(func() {
				barBox := ui.Box(c).Height(theme.OutlineTickHeight).Radius(theme.RadiusPill)
				w := appUi.SpringTo(barBox, "w", barW)
				barBox.Width(w)
				appUi.AnimateSurfaceColors(barBox, barCol, ui.Transparent, false)
			})
			appUi.Probe(tickBtn, fmt.Sprintf("outline-bar-%d", origIdx))
			if tickBtn.Clicked() {
				s.JumpToBlock(item.BlockIndex)
				c.Invalidate()
			}
		}
	})

	return rail, rail.Hovered(), hoveredBlockID
}

func (s *Session) updateOutlineHoverState(
	c *ui.Context,
	now time.Time,
	items []model.OutlineItem,
	listMode bool,
	inHotZone bool,
	popupPendingLayout bool,
	railHovered bool,
	hoveredTickID model.BlockID,
	listHoveredID model.BlockID,
	inputFocused bool,
) {
	busy := s.isMouseDragging() || s.dragBlockID != 0 || s.isTableExtending() || s.imgResizeBlock != 0 || s.tableDragBlock != 0 || s.tableResizeBlock != 0

	if busy {
		s.outlineRailHovered = false
		s.outlineListDwellID = 0
		s.outlineListDwellAt = time.Time{}
		if s.outlineMode != outlineModePinned && (s.outlineMode != outlineModeClosed || s.outlineHoverBlockID != 0 || s.outlineCardBlockID != 0 || !s.outlineLeaveAt.IsZero()) {
			s.clearOutlineOverlay(false)
			c.Invalidate()
		}
		return
	}
	if !inHotZone && popupPendingLayout {
		c.Invalidate()
		return
	}
	if s.outlineRailHovered != inHotZone {
		s.outlineRailHovered = inHotZone
		c.Invalidate()
	}

	nextHoverID := hoveredTickID
	if nextHoverID == 0 {
		nextHoverID = listHoveredID
	}
	if nextHoverID != s.outlineHoverBlockID {
		s.outlineHoverBlockID = nextHoverID
		c.Invalidate()
	}

	if !inHotZone {
		s.outlineSuppressReopen = false
		s.outlineListDwellID = 0
		s.outlineListDwellAt = time.Time{}

		switch s.outlineMode {
		case outlineModePinned:
			s.outlineLeaveAt = time.Time{}
		case outlineModeHoverCard:
			if testing.Testing() {
				s.clearOutlineOverlay(false)
				c.Invalidate()
				return
			}
			if s.outlineLeaveAt.IsZero() {
				s.outlineLeaveAt = now
			}
			leaveDelay := theme.OutlineCardLeaveCloseMs * time.Millisecond
			if elapsed := now.Sub(s.outlineLeaveAt); elapsed >= leaveDelay {
				s.clearOutlineOverlay(false)
				c.Invalidate()
			} else {
				c.After(leaveDelay - elapsed + 5*time.Millisecond)
			}
		case outlineModePanel:
			if inputFocused {
				s.outlineMode = outlineModePinned
				s.outlineLeaveAt = time.Time{}
				c.Invalidate()
				return
			}
			if testing.Testing() {
				s.clearOutlineOverlay(false)
				c.Invalidate()
				return
			}
			if s.outlineLeaveAt.IsZero() {
				s.outlineLeaveAt = now
			}
			leaveDelay := theme.OutlineListLeaveCloseMs * time.Millisecond
			if elapsed := now.Sub(s.outlineLeaveAt); elapsed >= leaveDelay {
				s.clearOutlineOverlay(false)
				c.Invalidate()
			} else {
				c.After(leaveDelay - elapsed + 5*time.Millisecond)
			}
		}
		return
	}

	s.outlineLeaveAt = time.Time{}

	switch s.outlineMode {
	case outlineModeClosed:
		if s.outlineSuppressReopen || (hoveredTickID == 0 && !railHovered) {
			return
		}
		targetID := hoveredTickID
		if targetID == 0 {
			targetID = items[0].BlockID
		}
		if !listMode {
			// <= 20 headings: 0ms into HoverCard; never schedule a timer to auto-switch to outlinePanel.
			s.outlineMode = outlineModeHoverCard
			s.outlineCardBlockID = targetID
			s.outlineHoverBlockID = targetID
			s.outlineListDwellID = 0
			s.outlineListDwellAt = time.Time{}
			c.Invalidate()
			return
		}
		// > 20 headings (listMode): dwell 200ms directly into outlinePanel without flashing HoverCard.
		if testing.Testing() {
			s.outlineMode = outlineModePanel
			s.outlineListDwellID = 0
			s.outlineListDwellAt = time.Time{}
			c.Invalidate()
			return
		}
		s.outlineListDwellID = targetID
		if s.outlineListDwellAt.IsZero() {
			s.outlineListDwellAt = now
		}
		dwellDelay := theme.OutlineListDwellOpenMs * time.Millisecond
		if elapsed := now.Sub(s.outlineListDwellAt); elapsed >= dwellDelay {
			s.outlineMode = outlineModePanel
			s.outlineListDwellID = 0
			s.outlineListDwellAt = time.Time{}
			c.Invalidate()
		} else {
			c.After(dwellDelay - elapsed + 5*time.Millisecond)
		}

	case outlineModeHoverCard:
		if listMode {
			s.outlineMode = outlineModePanel
			c.Invalidate()
			return
		}
		if hoveredTickID != 0 && hoveredTickID != s.outlineCardBlockID {
			s.outlineCardBlockID = hoveredTickID
			c.Invalidate()
		}

	case outlineModePanel:
		if inputFocused {
			s.outlineMode = outlineModePinned
			c.Invalidate()
		}
	}
}

func outlinePanelIndent(level int) float32 {
	switch {
	case level <= 1:
		return theme.Space8
	case level == 2:
		return theme.Space16
	case level == 3:
		return theme.Space24
	default:
		return theme.Space24 + theme.Space8
	}
}
