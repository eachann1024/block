package app

import (
	"time"

	"block/internal/editor"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type edgeFloatSide uint8

const (
	edgeSideNone edgeFloatSide = iota
	edgeSideLeft
	edgeSideRight
)

const (
	edgeFloatLeaveDuration = theme.EdgeFloatLeaveMs * time.Millisecond
)

// EdgeFloatController drives Scheme A's colored left/right window-edge hot-zones,
// instant hover summoning, rounded Ocean Paper floating Sidebar / AI cards, and
// synchronized handle-safe editor insets so block handles are never occluded.
type EdgeFloatController struct {
	active                  edgeFloatSide
	leaveStart              time.Time
	suppressLeftUntilLeave  bool
	suppressRightUntilLeave bool
	openDocRev              uint64
	primaryDown             bool
	pressedInCard           bool
	cardHovered             bool
	leftZoneHovered         bool
	rightZoneHovered        bool
	openedWithoutHover      bool
	leftCardHandle          ui.Handle
	rightCardHandle         ui.Handle
}

// LeftOpen reports whether the left floating sidebar card is currently open.
func (ef *EdgeFloatController) LeftOpen() bool {
	return ef.active == edgeSideLeft
}

// RightOpen reports whether the right floating AI card is currently open.
func (ef *EdgeFloatController) RightOpen() bool {
	return ef.active == edgeSideRight
}

// DismissAll closes any open floating edge card immediately.
func (ef *EdgeFloatController) DismissAll(w *Workspace) {
	if ef.active == edgeSideNone {
		return
	}
	ef.closeSide(w, ef.active, false)
}

// DismissRightWithCooldown closes the right floating AI card and suppresses hover
// re-opening until the pointer leaves the right edge zone.
func (ef *EdgeFloatController) DismissRightWithCooldown(w *Workspace) {
	if ef.active == edgeSideRight {
		ef.closeSide(w, edgeSideRight, true)
	}
}

func (ef *EdgeFloatController) openSide(side edgeFloatSide, sess *editor.Session, fromHover bool) {
	ef.active = side
	ef.leaveStart = time.Time{}
	ef.openedWithoutHover = !fromHover
	if sess != nil {
		ef.openDocRev = sess.DocRevision()
	}
}

func (ef *EdgeFloatController) closeSide(w *Workspace, side edgeFloatSide, suppressUntilLeave bool) {
	if side == edgeSideLeft {
		if w != nil && w.sidebarUI != nil {
			w.sidebarUI.ClosePopups()
		}
		if suppressUntilLeave {
			ef.suppressLeftUntilLeave = true
		}
	}
	if side == edgeSideRight {
		if w != nil && w.ai != nil {
			w.ai.CloseTransientPopups()
		}
		if suppressUntilLeave {
			ef.suppressRightUntilLeave = true
		}
	}
	if ef.active == side {
		ef.active = edgeSideNone
		ef.cardHovered = false
		ef.openedWithoutHover = false
	}
	ef.leaveStart = time.Time{}
}

func (ef *EdgeFloatController) cardWidths(vpW float32, w *Workspace) (leftW, rightW float32) {
	leftW = min(ClampSidebarWidth(w.sidebarWidth), max(vpW-40, SidebarMinW))
	rightW = min(ClampChatWidth(w.ai.PanelWidth()), max(vpW-40, ChatMinW))
	return leftW, rightW
}

func (ef *EdgeFloatController) isPointInsideOpenCard(px, vpW, leftCardW, rightCardW float32) bool {
	switch ef.active {
	case edgeSideLeft:
		return px <= theme.EdgeFloatInset+leftCardW+theme.EdgeFloatHoverBuffer
	case edgeSideRight:
		return px >= vpW-theme.EdgeFloatInset-rightCardW-theme.EdgeFloatHoverBuffer
	default:
		return false
	}
}

// PrepareFrame validates active edge-float state against current workspace layout,
// handles Escape dismissal and auto-collapse on editor typing / block dragging,
// and returns the target handle-safe left/right insets for the active editor session.
func (ef *EdgeFloatController) PrepareFrame(
	c *ui.Context,
	w *Workspace,
	vpW float32,
	layout ShellLayout,
	inSettings bool,
	aiImmersive bool,
	activeSess *editor.Session,
) (canLeft, canRight bool, leftInset, rightInset float32) {
	blocked := aiImmersive || w.IsPageHistoryOpen() || w.exportCtrl.IsOpen() || w.modals.HasActiveModal()
	canLeft = !blocked && layout.SidebarYielded && !layout.SidebarDrawer
	canRight = !blocked && !inSettings && w.ai.IsOpen() && !layout.ChatDocked

	if ef.active == edgeSideLeft && !canLeft {
		ef.closeSide(w, edgeSideLeft, false)
	}
	if ef.active == edgeSideRight && !canRight {
		ef.closeSide(w, edgeSideRight, false)
	}

	// Escape closes an open floating edge card when no inner menu/popover owns Escape.
	if ef.active != edgeSideNone && !w.sidebarUI.HasActiveOverlay() && !w.ai.HasActiveOverlay() {
		if c.Shortcut(0, ui.KeyEscape) {
			ef.closeSide(w, ef.active, true)
			c.Invalidate()
		}
	}

	// Starting a block drag or typing directly in the editor smoothly tucks away the floating card.
	if ef.active != edgeSideNone && activeSess != nil {
		if activeSess.IsDraggingBlock() {
			ef.closeSide(w, ef.active, false)
			c.Invalidate()
		} else if curRev := activeSess.DocRevision(); curRev != ef.openDocRev {
			cardFocused := (ef.active == edgeSideLeft && ef.leftCardHandle.FocusWithin(c)) ||
				(ef.active == edgeSideRight && ef.rightCardHandle.FocusWithin(c))
			if cardFocused {
				ef.openDocRev = curRev
			} else {
				ef.closeSide(w, ef.active, false)
				c.Invalidate()
			}
		}
	}

	// Pure floating overlay: opening or closing an edge card never pushes or reflows
	// the main editor column; only keep a static 10px edge clearance when a yielded panel
	// exposes an edge hot-zone so the 54px block-handle gutter never overlaps the 8px hot-zone.
	if (canLeft || layout.SidebarDrawer) && activeSess != nil {
		naturalMargin := (layout.MainWidth - activeSess.OuterFlowWidth()) * 0.5
		if naturalMargin < theme.EdgeFloatSafeClear {
			leftInset = theme.EdgeFloatSafeClear
		}
	}

	if canRight && activeSess != nil {
		remW := layout.MainWidth - leftInset
		naturalRight := (remW - activeSess.OuterFlowWidth()) * 0.5
		if naturalRight < theme.EdgeFloatSafeClear {
			rightInset = theme.EdgeFloatSafeClear
		}
	}

	return canLeft, canRight, leftInset, rightInset
}

// RenderBodyOverlays tracks edge dwell / leave timing on the workspace split body
// and renders the rounded floating Sidebar / AI cards plus the left/right colored edge hot-zones.
func (ef *EdgeFloatController) RenderBodyOverlays(
	c *ui.Context,
	body ui.Element,
	w *Workspace,
	vpW float32,
	inSettings bool,
	canLeft, canRight bool,
	stSnap model.AppState,
	activeNB string,
	notebooks []string,
	entries []model.FileEntry,
	activeTab string,
	notice string,
	activeSess *editor.Session,
) {
	if !canLeft && !canRight {
		ef.leftZoneHovered = false
		ef.rightZoneHovered = false
		return
	}
	if !canLeft {
		ef.leftZoneHovered = false
	}
	if !canRight {
		ef.rightZoneHovered = false
	}

	if !c.WindowActive() {
		ef.primaryDown = false
		ef.pressedInCard = false
	}

	leftCardW, rightCardW := ef.cardWidths(vpW, w)

	body.ObserveInput(func(ev ui.InputEvent) {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button == 0 {
				ef.primaryDown = true
				ef.pressedInCard = ef.isPointInsideOpenCard(ev.X, vpW, leftCardW, rightCardW)
				if ef.active != edgeSideNone && !ef.pressedInCard &&
					!w.sidebarUI.HasActiveOverlay() && !w.ai.HasActiveOverlay() {
					inLeftStrip := canLeft && ev.X <= theme.EdgeHotZoneWidth
					inRightStrip := canRight && ev.X >= vpW-theme.EdgeHotZoneWidth
					if !inLeftStrip && !inRightStrip {
						ef.closeSide(w, ef.active, false)
						c.Invalidate()
					}
				}
			}
		case ui.InputPointerUp:
			if ev.Button == 0 || ev.Canceled {
				ef.primaryDown = false
				ef.pressedInCard = false
			}
		}
	})

	now := c.Now()
	px, py, over := body.PointerPosition()
	bodyH := body.Bounds().H
	inVertBounds := over && py >= 0 && (bodyH <= 0 || py <= bodyH)

	inLeftZone := canLeft && ((inVertBounds && px >= 0 && px <= theme.EdgeHotZoneWidth) || ef.leftZoneHovered)
	inRightZone := canRight && ((inVertBounds && px >= vpW-theme.EdgeHotZoneWidth && px <= vpW) || ef.rightZoneHovered)

	if !inLeftZone {
		ef.suppressLeftUntilLeave = false
	}
	if !inRightZone {
		ef.suppressRightUntilLeave = false
	}

	editorBusy := activeSess != nil && activeSess.IsBusyForEdgeFloat()
	editorScrolling := activeSess != nil && activeSess.IsScrollingRecently(now)

	// 1. Instant edge hover summoning (opens floating card as soon as pointer enters outer strip).
	switch {
	case inLeftZone && ef.active != edgeSideLeft && !ef.suppressLeftUntilLeave && !ef.primaryDown && !editorBusy:
		ef.openSide(edgeSideLeft, activeSess, true)
		c.Invalidate()
	case inRightZone && ef.active != edgeSideRight && !ef.suppressRightUntilLeave && !ef.primaryDown && !editorBusy && !editorScrolling:
		ef.openSide(edgeSideRight, activeSess, true)
		c.Invalidate()
	}

	// 2. Active card hold / 160ms leave-grace state machine.
	switch ef.active {
	case edgeSideLeft:
		inCardCorridor := (inVertBounds && px <= theme.EdgeFloatInset+leftCardW+theme.EdgeFloatHoverBuffer) || ef.cardHovered || ef.leftZoneHovered
		if inCardCorridor {
			ef.openedWithoutHover = false
		}
		hold := inCardCorridor ||
			(ef.openedWithoutHover && !inVertBounds) ||
			w.sidebarUI.HasActiveOverlay() ||
			ef.leftCardHandle.FocusWithin(c) ||
			(ef.primaryDown && ef.pressedInCard)
		if hold {
			ef.leaveStart = time.Time{}
		} else {
			if ef.leaveStart.IsZero() {
				ef.leaveStart = now
			}
			elapsed := now.Sub(ef.leaveStart)
			if elapsed >= edgeFloatLeaveDuration {
				ef.closeSide(w, edgeSideLeft, false)
				c.Invalidate()
			} else {
				c.After(edgeFloatLeaveDuration - elapsed + 5*time.Millisecond)
			}
		}
	case edgeSideRight:
		inCardCorridor := (inVertBounds && px >= vpW-theme.EdgeFloatInset-rightCardW-theme.EdgeFloatHoverBuffer) || ef.cardHovered || ef.rightZoneHovered
		if inCardCorridor {
			ef.openedWithoutHover = false
		}
		hold := inCardCorridor ||
			(ef.openedWithoutHover && !inVertBounds) ||
			w.ai.HasActiveOverlay() ||
			ef.rightCardHandle.FocusWithin(c) ||
			(ef.primaryDown && ef.pressedInCard)
		if hold {
			ef.leaveStart = time.Time{}
		} else {
			if ef.leaveStart.IsZero() {
				ef.leaveStart = now
			}
			elapsed := now.Sub(ef.leaveStart)
			if elapsed >= edgeFloatLeaveDuration {
				ef.closeSide(w, edgeSideRight, false)
				c.Invalidate()
			} else {
				c.After(edgeFloatLeaveDuration - elapsed + 5*time.Millisecond)
			}
		}
	}

	pal := theme.Ocean()
	efPal := pal.EdgeFloat()
	ef.cardHovered = false

	// 3. Rounded Floating Left Sidebar Card.
	if canLeft && ef.active == edgeSideLeft {
		c.Key("edge-float-sidebar")
		card := blockui.Elevate(
			ui.Row(c).Absolute().
				Left(theme.EdgeFloatInset).Top(0).Bottom(theme.EdgeFloatInset).
				Width(leftCardW).Radius(theme.RadiusLG).Clip().
				Background(pal.Sidebar).Border(1, efPal.CardBorder).
				Bind(&ef.leftCardHandle).
				Transition(blockui.EdgeFloatCardTransition(false)),
			theme.ElevationDrawer,
		)
		ef.cardHovered = card.Hovered()
		blockui.Probe(card, "edge-float-sidebar")
		card.Children(func() {
			if inSettings {
				w.modals.RenderSettingsSidebar(c, true, func() {
					ef.closeSide(w, edgeSideLeft, false)
					w.RequestRedraw()
				})
			} else {
				w.renderFloatingSidebarCard(c, leftCardW, stSnap, activeNB, notebooks, entries, activeTab, notice)
			}
		})
	}

	// 4. Rounded Floating Right AI Card.
	if canRight && ef.active == edgeSideRight {
		c.Key("edge-float-ai")
		card := blockui.Elevate(
			ui.Column(c).Absolute().
				Right(theme.EdgeFloatInset).Top(0).Bottom(theme.EdgeFloatInset).
				Width(rightCardW).Radius(theme.RadiusLG).Clip().
				Background(pal.Paper).Border(1, efPal.CardBorder).
				Bind(&ef.rightCardHandle).
				Transition(blockui.EdgeFloatCardTransition(true)),
			theme.ElevationDrawer,
		)
		ef.cardHovered = card.Hovered()
		blockui.Probe(card, "edge-float-ai")
		card.Children(func() {
			w.ai.SetRenderWidth(rightCardW)
			w.ai.RenderPanel(c)
		})
	}

	// 5. Invisible Left & Right Edge Hot-Zones.
	if canLeft {
		prevHover := ef.leftZoneHovered
		ef.leftZoneHovered = renderEdgeHotZone(
			c,
			"edge-hotzone-left",
			false,
			theme.Tr("贴边唤出浮动侧栏", "Float Sidebar"),
			func() {
				if ef.active == edgeSideLeft {
					ef.closeSide(w, edgeSideLeft, true)
				} else {
					ef.openSide(edgeSideLeft, activeSess, true)
				}
				c.Invalidate()
			},
		)
		if ef.leftZoneHovered && !prevHover && ef.active != edgeSideLeft &&
			!ef.suppressLeftUntilLeave && !ef.primaryDown && !editorBusy {
			ef.openSide(edgeSideLeft, activeSess, true)
			c.Invalidate()
		}
	}

	if canRight {
		prevHover := ef.rightZoneHovered
		ef.rightZoneHovered = renderEdgeHotZone(
			c,
			"edge-hotzone-right",
			true,
			theme.Tr("贴边唤出浮动 AI", "Float AI Panel"),
			func() {
				if ef.active == edgeSideRight {
					ef.closeSide(w, edgeSideRight, true)
				} else {
					ef.openSide(edgeSideRight, activeSess, true)
				}
				c.Invalidate()
			},
		)
		if ef.rightZoneHovered && !prevHover && ef.active != edgeSideRight &&
			!ef.suppressRightUntilLeave && !ef.primaryDown && !editorBusy && !editorScrolling {
			ef.openSide(edgeSideRight, activeSess, true)
			c.Invalidate()
		}
	}
}

// renderEdgeHotZone is the invisible window-edge sensor that summons a
// floating card; it draws nothing so the edge stays clean.
func renderEdgeHotZone(c *ui.Context, zoneID string, rightEdge bool, label string, onClick func()) bool {
	zone := ui.ButtonBase(c.Key(zoneID)).Absolute().Top(0).Bottom(0).
		Width(theme.EdgeHotZoneWidth).
		Cursor(ui.CursorPointer).KeepFocus().Label(label)
	if rightEdge {
		zone.Right(0)
	} else {
		zone.Left(0)
	}
	blockui.Probe(zone, zoneID)
	if zone.Clicked() && onClick != nil {
		onClick()
	}
	return zone.Hovered()
}

func (w *Workspace) renderFloatingSidebarCard(
	c *ui.Context,
	cardWidth float32,
	stSnap model.AppState,
	activeNB string,
	notebooks []string,
	entries []model.FileEntry,
	activeNote string,
	notice string,
) {
	fw := cardWidth
	RenderSidebar(c, SidebarProps{
		State:           w.sidebarUI,
		Width:           &fw,
		FloatingCard:    true,
		ActiveNotebook:  activeNB,
		Notebooks:       notebooks,
		Entries:         entries,
		ActiveNote:      activeNote,
		Notice:          notice,
		SettingsOpen:    w.modals.IsSettingsOpen(),
		ShortcutNewNote: w.shortcutForSidebar(stSnap),
		OnNewFolder: func() {
			_, _ = w.CreateNewFolder()
		},
		OnNewNote: func() {
			_, _ = w.CreateNewNote()
		},
		OnOpenNote: func(path string, inNewTab bool) {
			if !inNewTab {
				w.edgeFloat.DismissAll(w)
			}
			_ = w.openNoteInternal(path, inNewTab, true, 0)
		},
		OnRenameItem: w.finishSidebarRename,
		OnMoveItem: func(srcPath, dstDir string) {
			_, _ = w.MoveWorkspaceItem(srcPath, dstDir)
		},
		OnReorderSibling:   w.ReorderSibling,
		OnReorderNotebooks: w.ReorderNotebooks,
		OnTrashItem: func(path string) {
			_ = w.TrashWorkspaceItem(path)
		},
		OnRevealItem: func(path string) {
			w.revealInShell(path)
		},
		OnSwitchNotebook: func(root string) {
			_ = w.SetNotebook(root)
		},
		OnForgetNotebook: w.ForgetNotebook,
		OnChooseNotebook: w.ChooseNotebookDialog,
		RequestRedraw:    w.RequestRedraw,
		OnOpenSettings: func() {
			w.edgeFloat.DismissAll(w)
			w.OpenSettings("ai")
		},
		FileBadges:           w.pluginSidebarFileBadges(activeNB),
		PluginContextMenu:    w.pluginSidebarMenuItems(),
		OnPluginContextMenu:  w.onPluginSidebarMenuItem,
		RenderPluginSections: w.renderPluginSidebarSections,
	})
}
