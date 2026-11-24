package mindmap

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	outlineIndentStep float32 = 22
	outlineFoldSlotW  float32 = 14
	outlineMarkSlotW  float32 = 20
	outlineGuideX     float32 = outlineFoldSlotW + outlineMarkSlotW*0.5
	outlineGuideHitW  float32 = 9
)

type outlineActionKind uint8

const (
	outlineActTitleEnter outlineActionKind = iota + 1
	outlineActTitleDown
	outlineActNodeEnter
	outlineActIndent
	outlineActOutdent
	outlineActBackspaceStart
	outlineActMoveUp
	outlineActMoveDown
	outlineActToggleTask
	outlineActToggleFold
	outlineActToggleFoldAll
	outlineActArrowUp
	outlineActArrowDown
	outlineActUndo
	outlineActRedo
	outlineActZoomTo
	outlineActZoomOut
	outlineActExitFullscreen
	outlineActAddChild
	outlineActDeleteNode
)

type outlineAction struct {
	kind     outlineActionKind
	nodeID   int
	text     string
	selStart int
	selEnd   int
}

func (st *BlockState) outlineActiveRoot() *Node {
	if st.Root == nil {
		return nil
	}
	if st.OutlineZoomID != 0 {
		if n := st.Root.FindByID(st.OutlineZoomID); n != nil {
			return n
		}
		st.OutlineZoomID = 0
	}
	return st.Root
}

func (st *BlockState) outlineHasAnyExpanded() bool {
	root := st.outlineActiveRoot()
	if root == nil {
		return false
	}
	found := false
	var walk func(n *Node)
	walk = func(n *Node) {
		if found {
			return
		}
		for _, ch := range n.Children {
			if len(ch.Children) > 0 && !ch.Folded {
				found = true
				return
			}
			walk(ch)
		}
	}
	walk(root)
	return found
}

func (st *BlockState) outlineToggleFoldAll() {
	root := st.outlineActiveRoot()
	if root == nil {
		return
	}
	collapse := st.outlineHasAnyExpanded()
	var walk func(n *Node)
	walk = func(n *Node) {
		for _, ch := range n.Children {
			if len(ch.Children) > 0 {
				ch.Folded = collapse
			}
			walk(ch)
		}
	}
	walk(root)
	// Keep focus on a visible ancestor if the focused node became hidden.
	if collapse && st.OutlineFocusID != 0 && st.OutlineFocusID != root.ID {
		if cur := root.FindByID(st.OutlineFocusID); cur != nil {
			vis := cur
			for p := cur.Parent; p != nil && p != root; p = p.Parent {
				if p.Folded {
					vis = p
				}
			}
			if vis != cur {
				st.focusOutlineNode(vis.ID, utf8.RuneCountInString(vis.Text))
			}
		}
	}
}

func outlinePathTo(root, target *Node) []*Node {
	if root == nil || target == nil {
		return nil
	}
	var rev []*Node
	for cur := target; cur != nil; cur = cur.Parent {
		rev = append(rev, cur)
		if cur == root {
			break
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func outlineVisibleNodes(root *Node) []*Node {
	if root == nil {
		return nil
	}
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		for _, ch := range n.Children {
			out = append(out, ch)
			if !ch.Folded && len(ch.Children) > 0 {
				walk(ch)
			}
		}
	}
	walk(root)
	return out
}

func (st *BlockState) setOutlineFolded(node *Node, folded bool) {
	node.Folded = folded
	if folded && st.OutlineFocusID != node.ID && node.FindByID(st.OutlineFocusID) != nil {
		st.focusOutlineNode(node.ID, utf8.RuneCountInString(node.Text))
	}
}

func (st *BlockState) estimateOutlineSelection(nodeID int, liveText string) (int, int) {
	nRunes := utf8.RuneCountInString(liveText)
	if st.OutlineSelNodeID != nodeID {
		return nRunes, nRunes
	}
	s0, s1 := st.OutlineSelStart, st.OutlineSelEnd
	if delta := nRunes - st.OutlineSelRuneLen; delta != 0 {
		s0 = clampInt(s0+delta, 0, nRunes)
		s1 = clampInt(s1+delta, 0, nRunes)
	}
	return clampInt(s0, 0, nRunes), clampInt(s1, 0, nRunes)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isOutlinePrimaryMod(mods ui.Modifiers) bool {
	if theme.IsMacOS() {
		return mods == ui.Cmd
	}
	return mods == ui.Ctrl || mods == ui.Cmd
}

func isOutlinePrimaryShiftMod(mods ui.Modifiers) bool {
	if theme.IsMacOS() {
		return mods == ui.Cmd|ui.Shift
	}
	return mods == ui.Ctrl|ui.Shift || mods == ui.Cmd|ui.Shift
}

func (st *BlockState) focusOutlineNode(nodeID, caret int) {
	st.OutlineFocusID = nodeID
	st.SelectedID = nodeID
	st.OutlineFocusPending = true
	st.OutlineCaretPending = true
	st.OutlineCaretTarget = caret
}

// renderOutlineView renders a Mubu-style direct-edit outliner aligned with Ocean Paper tokens
// and Block's native list bullet, vertical guide, and todo checkbox design language.
func (st *BlockState) renderOutlineView(c *ui.Context, id string, fullscreen bool, cb Callbacks) {
	st.LastCanvasFocused = false
	pal := theme.Ocean()
	activeRoot := st.outlineActiveRoot()
	if activeRoot == nil {
		return
	}

	padY := theme.Space16
	padX := theme.Space20
	if fullscreen {
		padY = theme.Space24
		padX = theme.Space24
	}

	focusJustMoved := false
	focusIDAtStart := 0
	if st.OutlineFocusPending {
		focusIDAtStart = st.OutlineFocusID
	}
	scr := bui.Scroll(c.Key("mm-out-scroll-"+id), &st.OutlineScroll).Fill().
		Background(pal.Paper).Padding(padY, padX)
	scr.Children(func() {
		outer := ui.Column(c.Key("mm-out-outer-" + id)).FillWidth().Bind(&st.OutlineHandle)
		if fullscreen {
			outer.AlignItems(ui.Center)
		}
		outer.Children(func() {
			col := ui.Column(c.Key("mm-out-col-" + id)).FillWidth().Gap(theme.Space2)
			if fullscreen {
				col.MaxWidth(theme.EditorStandardWidth)
			}
			col.Children(func() {
				// Zoom breadcrumb bar (only visible when zoomed into a branch).
				if st.OutlineZoomID != 0 && activeRoot != st.Root {
					st.renderOutlineBreadcrumb(c, id, activeRoot)
				}

				// Editable heading for the active root (root topic or zoomed branch).
				st.renderOutlineTitle(c, id, activeRoot, fullscreen, &focusJustMoved, cb)

				// Recursive branch items (or subtle add prompt when branch has no children).
				if len(activeRoot.Children) == 0 && !cb.ReadOnly {
					addBtn := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-empty-add-%s-%d", id, activeRoot.ID))).
						FillWidth().Height(theme.HeightChrome).Padding(0, theme.Space8).
						AlignItems(ui.Center).Gap(theme.Space6).Radius(theme.RadiusSM).
						Cursor(ui.CursorPointer).Label(theme.Tr("添加分支", "Add item"))
					if addBtn.Hovered() || addBtn.Pressed() {
						addBtn.Background(pal.Hover.Alpha(0.6))
					}
					addBtn.Children(func() {
						icons.DrawAnimated(c, icons.Plus, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary), addBtn.Hovered())
						ui.Text(c, theme.Tr("点击添加第一个分支，或在标题按 ↵", "Click to add first item, or press Enter in title")).
							FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					})
					if addBtn.Clicked() {
						st.pushUndo()
						st.OutlineUndoNodeID = 0
						created := &Node{ID: st.allocID(), Text: "", Parent: activeRoot}
						activeRoot.Children = append(activeRoot.Children, created)
						st.commitChange(cb)
						st.focusOutlineNode(created.ID, 0)
						c.Invalidate()
					}
				} else {
					st.renderOutlineBranch(c, id, activeRoot, 0, fullscreen, &focusJustMoved, cb)
				}
			})
		})
	})

	// Only discard earlier requests that could not find their target. A click
	// in this pass can change which rows are built in the next pass.
	if st.OutlineFocusPending && st.OutlineFocusID == focusIDAtStart && !focusJustMoved {
		st.OutlineFocusPending = false
		st.OutlineCaretPending = false
	}

	// Apply any structural keyboard action queued during OnTextKey safely outside tree iteration.
	if st.outlinePending != nil {
		act := st.outlinePending
		st.outlinePending = nil
		st.applyOutlineAction(c, act, fullscreen, cb)
	}
}

func (st *BlockState) renderOutlineBreadcrumb(c *ui.Context, id string, activeRoot *Node) {
	pal := theme.Ocean()
	path := outlinePathTo(st.Root, activeRoot)
	if len(path) <= 1 {
		return
	}
	bar := ui.Row(c.Key("mm-out-crumb-"+id)).FillWidth().AlignItems(ui.Center).
		Gap(theme.Space4).Margin(0, 0, theme.Space6, 0).Wrap()
	bar.Children(func() {
		for idx, p := range path {
			node := p
			label := strings.ReplaceAll(strings.TrimSpace(node.Text), "\n", " ")
			if label == "" {
				label = theme.Tr("无标题", "Untitled")
			}
			if idx > 0 {
				ui.Text(c, "›").FontSize(theme.FontMeta).TextColor(pal.TextSecondary.Alpha(0.55)).SingleLine().Shrink(0)
			}
			if idx < len(path)-1 {
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-crumb-%s-%d", id, node.ID))).
					Padding(theme.Space2, theme.Space6).Radius(theme.RadiusXS).
					Cursor(ui.CursorPointer).Shrink(0).Label(label)
				if btn.Hovered() || btn.Pressed() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					ui.Text(c, label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
						TextColor(pal.TextSecondary).SingleLine()
				})
				if btn.Clicked() {
					st.outlinePending = &outlineAction{kind: outlineActZoomTo, nodeID: node.ID}
				}
			} else {
				ui.Text(c, label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
					TextColor(pal.Text).Padding(theme.Space2, theme.Space4).SingleLine().Shrink(0)
			}
		}
	})
}

func (st *BlockState) renderOutlineTitle(c *ui.Context, id string, activeRoot *Node, fullscreen bool, focusJustMoved *bool, cb Callbacks) {
	pal := theme.Ocean()
	rootID := activeRoot.ID
	draft := activeRoot.Text
	titleRow := ui.Row(c.Key(fmt.Sprintf("mm-out-title-row-%s-%d", id, rootID))).
		FillWidth().Padding(theme.Space2, theme.Space4, theme.Space8, theme.Space4).
		AlignItems(ui.Start).Gap(theme.Space6).Radius(theme.RadiusSM)
	titleHovered := titleRow.Hovered()

	titleRow.Children(func() {
		ta := ui.TextAreaBase(c.Key(fmt.Sprintf("mm-out-title-%s-%d", id, rootID)), &draft).
			Grow(1).Shrink(1).MinWidth(0).Lines(1, 20).Scrollbars(false).
			Background(ui.Transparent).BorderWidth(0).Padding(0).
			Font(theme.ResolvedEditorFont()).
			FontSize(theme.FontHeading).FontWeight(theme.WeightTitle).
			FixedLineHeight(theme.LineHeading).
			TextColor(pal.Text).CaretColor(pal.Accent).SelectionColor(pal.EditorSelectionBG()).
			Placeholder(theme.Tr("输入主题…", "Untitled topic…")).PlaceholderColor(pal.Placeholder).
			Label(theme.Tr("中心主题", "Central topic"))
		bui.Probe(ta, fmt.Sprintf("mm-out-title-%s-%d", id, rootID))
		if cb.ReadOnly {
			ta.ReadOnly(true)
		}

		ta.OnTextKey(func(k ui.TextKey) bool {
			if isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyZ {
				if !cb.ReadOnly {
					st.outlinePending = &outlineAction{kind: outlineActUndo}
					return true
				}
			}
			if (isOutlinePrimaryShiftMod(k.Mods) && k.Key == ui.KeyZ) || (isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyY) {
				if !cb.ReadOnly {
					st.outlinePending = &outlineAction{kind: outlineActRedo}
					return true
				}
			}
			if isOutlinePrimaryShiftMod(k.Mods) && k.Key == ui.KeyPeriod {
				st.outlinePending = &outlineAction{kind: outlineActToggleFoldAll}
				return true
			}
			if k.Mods == 0 && k.Key == ui.KeyEscape {
				if st.OutlineZoomID != 0 {
					st.outlinePending = &outlineAction{kind: outlineActZoomOut}
					return true
				}
				if fullscreen {
					st.outlinePending = &outlineAction{kind: outlineActExitFullscreen}
					return true
				}
			}
			if k.Mods == 0 && k.Key == ui.KeyEnter {
				if !cb.ReadOnly {
					st.outlinePending = &outlineAction{kind: outlineActTitleEnter}
					return true
				}
			}
			if k.Mods == 0 && k.Key == ui.KeyDown {
				live := k.Text()
				_, s1 := st.estimateOutlineSelection(rootID, live)
				runes := []rune(live)
				if !strings.ContainsRune(string(runes[s1:]), '\n') {
					st.outlinePending = &outlineAction{kind: outlineActTitleDown}
					return true
				}
			}
			return false
		})

		if st.OutlineFocusPending && st.OutlineFocusID == rootID {
			ta.Focus()
			st.OutlineFocusPending = false
			*focusJustMoved = true
			if st.OutlineCaretPending {
				pos := clampInt(st.OutlineCaretTarget, 0, utf8.RuneCountInString(draft))
				ta.SetTextSelection(pos, pos)
				st.OutlineCaretPending = false
				st.OutlineSelNodeID = rootID
				st.OutlineSelStart = pos
				st.OutlineSelEnd = pos
				st.OutlineSelRuneLen = utf8.RuneCountInString(draft)
			}
		}

		taFocused := ta.Focused()
		if !st.OutlineFocusPending && !*focusJustMoved && taFocused {
			if st.OutlineFocusID != rootID {
				c.Invalidate()
			}
			st.OutlineFocusID = rootID
			st.SelectedID = rootID
			s0, s1 := ta.TextSelection()
			st.OutlineSelNodeID = rootID
			st.OutlineSelStart = s0
			st.OutlineSelEnd = s1
			st.OutlineSelRuneLen = utf8.RuneCountInString(draft)
		}

		if !cb.ReadOnly && ta.Changed() {
			next := normalizeNodeLineEndings(draft)
			if next != activeRoot.Text {
				if st.OutlineUndoNodeID != rootID {
					st.pushUndo()
					st.OutlineUndoNodeID = rootID
				}
				activeRoot.Text = next
				st.commitChange(cb)
			}
		}

		hasLink := strings.TrimSpace(activeRoot.Link) != ""
		showActions := !cb.ReadOnly && (titleHovered || st.OutlineFocusID == rootID || taFocused || st.LinkNodeID == rootID)
		if hasLink || showActions {
			trail := ui.Row(c.Key(fmt.Sprintf("mm-out-title-trail-%s-%d", id, rootID))).
				Height(theme.LineHeading).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0)
			trail.Children(func() {
				if hasLink {
					st.renderOutlineLinkBadge(c, fmt.Sprintf("mm-out-title-link-%s-%d", id, rootID), activeRoot, true, cb)
				}
				if showActions {
					st.renderOutlineNodeQuickActions(c, fmt.Sprintf("mm-out-title-act-%s-%d", id, rootID), activeRoot, false, cb)
				}
			})
		}
	})
}

func (st *BlockState) renderOutlineBranch(c *ui.Context, id string, parent *Node, depth int, fullscreen bool, focusJustMoved *bool, cb Callbacks) {
	pal := theme.Ocean()
	for _, ch := range parent.Children {
		node := ch
		hasKids := len(node.Children) > 0
		focused := st.OutlineFocusID == node.ID

		item := ui.Column(c.Key(fmt.Sprintf("mm-out-item-%s-%d", id, node.ID))).FillWidth()
		item.Children(func() {
			row := ui.Row(c.Key(fmt.Sprintf("mm-out-row-%s-%d", id, node.ID))).
				FillWidth().Padding(theme.Space2, theme.Space6, theme.Space2, 0).
				AlignItems(ui.Start).Radius(theme.RadiusSM)
			rowHovered := row.Hovered()
			if focused {
				bgAlpha := float32(0.045)
				if pal.Dark {
					bgAlpha = 0.075
				}
				row.Background(pal.Accent.Alpha(bgAlpha))
			} else if rowHovered {
				row.Background(pal.Hover.Alpha(0.55))
			}

			row.Children(func() {
				// Leading gutter aligned to first text line (theme.LineTitle = 22px).
				gutter := ui.Row(c.Key(fmt.Sprintf("mm-out-gut-%s-%d", id, node.ID))).
					Height(theme.LineTitle).AlignItems(ui.Center).Shrink(0)
				gutter.Children(func() {
					// Fold chevron slot (14px).
					if hasKids {
						foldBtn := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-fold-%s-%d", id, node.ID))).
							Size(outlineFoldSlotW, theme.LineTitle).Center().Radius(theme.RadiusXS).
							Cursor(ui.CursorPointer).Shrink(0).KeepFocus().
							Label(theme.Tr("折叠或展开分支", "Toggle branch fold"))
						showFold := node.Folded || rowHovered || focused || foldBtn.Hovered()
						if showFold {
							iconGlyph := icons.ChevronDown
							if node.Folded {
								iconGlyph = icons.ChevronRight
							}
							fg := pal.TextSecondary.Alpha(0.75)
							if foldBtn.Hovered() || node.Folded {
								fg = pal.Text
							}
							foldBtn.Children(func() {
								icons.DrawAnimated(c, iconGlyph, theme.IconMicro, pal.MonoIconColors(fg), foldBtn.Hovered())
							})
						}
						if foldBtn.Clicked() {
							st.setOutlineFolded(node, !node.Folded)
							c.Invalidate()
						}
					} else {
						ui.Box(c).Size(outlineFoldSlotW, theme.LineTitle).Shrink(0)
					}

					// Bullet / Todo checkbox slot (20px), aligned with Block editor's list_marker.go.
					markCell := ui.Row(c.Key(fmt.Sprintf("mm-out-mk-%s-%d", id, node.ID))).
						Size(outlineMarkSlotW, theme.LineTitle).Center().Shrink(0)
					markCell.Children(func() {
						if node.Task != nil {
							st.renderOutlineCheckbox(c, id, node, cb)
						} else {
							st.renderOutlineBullet(c, id, node, depth, hasKids, focused)
						}
					})
				})

				// Direct-edit multi-line text area (Mubu flow).
				hasLink := strings.TrimSpace(node.Link) != ""
				linkBroken := hasLink && st.resolveLinkStatusCached(c.Now(), node.Link, cb) == LinkStatusBroken
				draft := node.Text
				txtCol := pal.Text
				if hasLink {
					if linkBroken {
						txtCol = pal.Warning
					} else {
						txtCol = pal.Accent
					}
				}
				if node.Task != nil && *node.Task {
					txtCol = pal.TextSecondary.Alpha(0.62)
				}
				weight := theme.WeightRegular
				if depth == 0 {
					weight = theme.WeightControl
				}

				ta := ui.TextAreaBase(c.Key(fmt.Sprintf("mm-out-ta-%s-%d", id, node.ID)), &draft).
					Grow(1).Shrink(1).MinWidth(0).Lines(1, 200).Scrollbars(false).
					Margin(0, 0, 0, theme.Space4).Padding(0, theme.Space2).
					Background(ui.Transparent).BorderWidth(0).
					Font(theme.ResolvedEditorFont()).
					FontSize(theme.FontBody).FontWeight(weight).
					FixedLineHeight(theme.LineTitle).
					TextColor(txtCol).CaretColor(pal.Accent).SelectionColor(pal.EditorSelectionBG()).
					Placeholder(theme.Tr("输入内容…", "Type something…")).PlaceholderColor(pal.Placeholder).
					Label(theme.Tr("大纲节点", "Outline item"))
				bui.Probe(ta, fmt.Sprintf("mm-out-ta-%s-%d", id, node.ID))
				if cb.ReadOnly {
					ta.ReadOnly(true)
				}

				nodeID := node.ID
				ta.OnTextKey(func(k ui.TextKey) bool {
					live := k.Text()
					s0, s1 := st.estimateOutlineSelection(nodeID, live)
					switch {
					case isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyZ:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActUndo}
							return true
						}
					case (isOutlinePrimaryShiftMod(k.Mods) && k.Key == ui.KeyZ) || (isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyY):
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActRedo}
							return true
						}
					case isOutlinePrimaryShiftMod(k.Mods) && k.Key == ui.KeyPeriod:
						st.outlinePending = &outlineAction{kind: outlineActToggleFoldAll}
						return true
					case isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyPeriod:
						st.outlinePending = &outlineAction{kind: outlineActToggleFold, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
						return true
					case isOutlinePrimaryMod(k.Mods) && k.Key == ui.KeyEnter:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActToggleTask, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == ui.Alt && k.Key == ui.KeyUp:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActMoveUp, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == ui.Alt && k.Key == ui.KeyDown:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActMoveDown, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == 0 && k.Key == ui.KeyEnter:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActNodeEnter, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == ui.Shift && k.Key == ui.KeyEnter:
						// Shift+Enter inserts a line break inside the current outline node.
						return false
					case k.Mods == 0 && k.Key == ui.KeyTab:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActIndent, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == ui.Shift && k.Key == ui.KeyTab:
						if !cb.ReadOnly {
							st.outlinePending = &outlineAction{kind: outlineActOutdent, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == 0 && k.Key == ui.KeyBackspace:
						if !cb.ReadOnly && s0 == 0 && s1 == 0 {
							st.outlinePending = &outlineAction{kind: outlineActBackspaceStart, nodeID: nodeID, text: live, selStart: 0, selEnd: 0}
							return true
						}
					case k.Mods == 0 && k.Key == ui.KeyUp:
						runes := []rune(live)
						if !strings.ContainsRune(string(runes[:s0]), '\n') {
							st.outlinePending = &outlineAction{kind: outlineActArrowUp, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == 0 && k.Key == ui.KeyDown:
						runes := []rune(live)
						if !strings.ContainsRune(string(runes[s1:]), '\n') {
							st.outlinePending = &outlineAction{kind: outlineActArrowDown, nodeID: nodeID, text: live, selStart: s0, selEnd: s1}
							return true
						}
					case k.Mods == 0 && k.Key == ui.KeyEscape:
						if st.OutlineZoomID != 0 {
							st.outlinePending = &outlineAction{kind: outlineActZoomOut}
							return true
						}
						if fullscreen {
							st.outlinePending = &outlineAction{kind: outlineActExitFullscreen}
							return true
						}
					}
					return false
				})

				if st.OutlineFocusPending && st.OutlineFocusID == nodeID {
					ta.Focus()
					st.OutlineFocusPending = false
					*focusJustMoved = true
					if st.OutlineCaretPending {
						pos := clampInt(st.OutlineCaretTarget, 0, utf8.RuneCountInString(draft))
						ta.SetTextSelection(pos, pos)
						st.OutlineCaretPending = false
						st.OutlineSelNodeID = nodeID
						st.OutlineSelStart = pos
						st.OutlineSelEnd = pos
						st.OutlineSelRuneLen = utf8.RuneCountInString(draft)
					}
				}

				taFocused := ta.Focused()
				if !st.OutlineFocusPending && !*focusJustMoved && taFocused {
					if st.OutlineFocusID != nodeID {
						c.Invalidate()
					}
					st.OutlineFocusID = nodeID
					st.SelectedID = nodeID
					s0, s1 := ta.TextSelection()
					st.OutlineSelNodeID = nodeID
					st.OutlineSelStart = s0
					st.OutlineSelEnd = s1
					st.OutlineSelRuneLen = utf8.RuneCountInString(draft)
				}

				if !cb.ReadOnly && ta.Changed() {
					next := normalizeNodeLineEndings(draft)
					if done, stripped, matched := matchOutlineTodoPrefix(next); node.Task == nil && matched {
						_, caret := ta.TextAnchorCaret()
						prefixLen := len(next) - len(stripped)
						st.pushUndo()
						st.OutlineUndoNodeID = 0
						node.Task = BoolPtr(done)
						node.Text = stripped
						st.commitChange(cb)
						st.focusOutlineNode(nodeID, max(0, caret-prefixLen))
						c.Invalidate()
					} else if next != node.Text {
						if st.OutlineUndoNodeID != nodeID {
							st.pushUndo()
							st.OutlineUndoNodeID = nodeID
						}
						node.Text = next
						st.commitChange(cb)
					}
				}

				// Link jump button and hover/focus row-tail quick actions.
				showActions := !cb.ReadOnly && (rowHovered || focused || taFocused || st.LinkNodeID == node.ID)
				if hasLink || showActions {
					trail := ui.Row(c.Key(fmt.Sprintf("mm-out-trail-%s-%d", id, node.ID))).
						Height(theme.LineTitle).AlignItems(ui.Center).Gap(theme.Space2).
						Margin(0, 0, 0, theme.Space4).Shrink(0)
					trail.Children(func() {
						if hasLink {
							st.renderOutlineLinkBadge(c, fmt.Sprintf("mm-out-link-%s-%d", id, node.ID), node, false, cb)
						}
						if showActions {
							st.renderOutlineNodeQuickActions(c, fmt.Sprintf("mm-out-act-%s-%d", id, node.ID), node, node.Parent != nil, cb)
						}
					})
				}
			})

			// Children with vertical Block-style ListGuide aligned to the parent bullet center.
			if hasKids && !node.Folded {
				chWrap := ui.Column(c.Key(fmt.Sprintf("mm-out-ch-%s-%d", id, node.ID))).
					FillWidth().Padding(0, 0, 0, outlineIndentStep).Gap(theme.Space2)
				chWrap.Children(func() {
					guide := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-guide-%s-%d", id, node.ID))).
						Absolute().Top(0).Bottom(theme.Space4).Left(outlineGuideX - outlineGuideHitW*0.5).Width(outlineGuideHitW).
						Cursor(ui.CursorPointer).KeepFocus().ZIndex(1).
						Label(theme.Tr("点击折叠分支", "Collapse branch"))
					guideHov := guide.Hovered()
					guide.Draw(func(p *ui.Painter, r ui.Rect) {
						col := pal.ListGuide
						w := float32(1)
						if guideHov {
							col = pal.Accent
							w = 1.5
						}
						cx := float32(math.Round(float64(r.X + r.W*0.5)))
						p.Line(cx, r.Y, cx, r.Y+r.H, w, col)
					})
					if guide.Clicked() {
						st.setOutlineFolded(node, true)
						c.Invalidate()
					}
					st.renderOutlineBranch(c, id, node, depth+1, fullscreen, focusJustMoved, cb)
				})
			}
		})
	}
}

func (st *BlockState) renderOutlineBullet(c *ui.Context, id string, node *Node, depth int, hasKids, focused bool) {
	pal := theme.Ocean()
	foldedHalo := hasKids && node.Folded
	btn := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-bul-%s-%d", id, node.ID))).
		Size(18, 18).Center().Radius(theme.RadiusPill).
		Cursor(ui.CursorPointer).KeepFocus().
		Label(theme.Tr("聚焦此分支", "Zoom into branch"))
	hov := btn.Hovered()
	if foldedHalo {
		alpha := float32(0.16)
		if pal.Dark {
			alpha = 0.24
		}
		btn.Background(pal.Accent.Alpha(alpha))
		if hov {
			btn.Border(1, pal.Accent)
		}
	} else if hov {
		btn.Background(pal.Hover)
	}
	bui.Tooltip(c, btn, theme.Tr("点击聚焦此分支", "Zoom into branch"))

	// Mirror Block editor's 3-tier list bullet hierarchy (internal/editor/list_marker.go).
	dotSz := float32(5)
	if depth == 0 {
		dotSz = 6
	}
	btn.Children(func() {
		dot := ui.Box(c).Size(dotSz, dotSz).Radius(theme.RadiusPill)
		color := pal.ListBullet
		if depth == 0 || focused || foldedHalo {
			color = pal.Accent
		}
		if depth >= 2 && !foldedHalo {
			dot.Border(1.25, color)
		} else {
			dot.Background(color)
		}
	})
	if btn.Clicked() {
		st.outlinePending = &outlineAction{kind: outlineActZoomTo, nodeID: node.ID}
	}
}

// renderOutlineCheckbox mirrors Block editor's native Ocean Paper todo checkbox
// (16x16, RadiusSM, TodoBG/TodoRing + tactile spring Accent check glyph).
func (st *BlockState) renderOutlineCheckbox(c *ui.Context, id string, node *Node, cb Callbacks) {
	pal := theme.Ocean()
	checked := node.Task != nil && *node.Task
	cbBtn := ui.ButtonBase(c.Key(fmt.Sprintf("mm-out-cb-%s-%d", id, node.ID))).
		Size(16, 16).Center().Radius(theme.RadiusSM).
		Cursor(ui.CursorPointer).Disabled(cb.ReadOnly).KeepFocus().
		Label(theme.Tr("待办复选框", "Todo checkbox"))
	hov := cbBtn.Hovered() && !cb.ReadOnly
	prs := cbBtn.Pressed() && !cb.ReadOnly
	bg := pal.TodoBG
	bd := pal.TodoRing
	if checked || hov {
		bd = pal.TodoRingHover
	}
	if checked && hov {
		bg = pal.Selected
	}
	bui.AnimateSurfaceColors(cbBtn, bg, bd, true)
	bui.TactileButton(cbBtn, hov, prs, cb.ReadOnly)
	targetChk := float32(0)
	if checked {
		targetChk = 1
	}
	chkP := cbBtn.AnimateTo("olTodoChk", targetChk, bui.SpringTactile)
	cbBtn.Children(func() {
		if chkP > 0.01 {
			chkScale := 0.55 + 0.45*chkP
			chkAlpha := min(max(chkP, 0), 1)
			mark := ui.Box(c).Center().Opacity(chkAlpha)
			if float32(math.Abs(float64(chkScale-1))) > 0.001 {
				mark.Scale(chkScale, chkScale)
			}
			mark.Children(func() {
				icons.DrawAnimated(c, icons.Check, theme.IconMicro, pal.MinimalIconColors(pal.Accent), hov || prs || checked)
			})
		}
	})
	if cbBtn.Clicked() && !cb.ReadOnly {
		st.opFlipTaskDone(node, cb)
	}
}

func outlineLinkBadgeLabel(target string, status LinkStatus) string {
	s := strings.TrimSpace(target)
	if status == LinkStatusExternal {
		for _, pfx := range []string{"https://", "http://", "mailto:"} {
			if len(s) > len(pfx) && strings.EqualFold(s[:len(pfx)], pfx) {
				s = s[len(pfx):]
				break
			}
		}
		return strings.TrimRight(s, "/")
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if idx := strings.LastIndex(s, "/"); idx >= 0 && idx+1 < len(s) {
		s = s[idx+1:]
	}
	s = strings.TrimSuffix(s, ".md")
	s = strings.TrimSuffix(s, ".MD")
	if s == "" {
		return strings.TrimSpace(target)
	}
	return s
}

func (st *BlockState) renderOutlineLinkBadge(c *ui.Context, key string, node *Node, showLabel bool, cb Callbacks) {
	pal := theme.Ocean()
	target := strings.TrimSpace(node.Link)
	status := st.resolveLinkStatusCached(c.Now(), target, cb)

	glyph := icons.TbLink
	tip := target
	switch status {
	case LinkStatusBroken:
		glyph = icons.LinkOff
		tip = theme.Tr("目标笔记不存在 · 点击重连：", "Missing note · click to reconnect: ") + target
	case LinkStatusExternal:
		glyph = icons.LinkOpen
	}

	btn := ui.ButtonBase(c.Key(key)).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(tip)
	if showLabel {
		btn.Height(theme.HeightMicro).PaddingX(theme.Space8).Radius(theme.RadiusPill).
			Gap(theme.Space4).MaxWidth(220).Shrink(0)
	} else {
		btn.Size(theme.HeightMicro, theme.HeightMicro).Center().Radius(theme.RadiusXS).Shrink(0)
	}
	bui.Tooltip(c, btn, tip)

	hov := btn.Hovered()
	prs := btn.Pressed()
	var fg, bg, bd ui.Color
	hasBorder := false
	switch status {
	case LinkStatusBroken:
		fg = pal.Warning
		bg = pal.WarningSurface
		bd = pal.Warning.Alpha(0.45)
		if prs {
			bg = pal.Warning.Alpha(0.26)
			bd = pal.Warning
		} else if hov {
			bg = pal.Warning.Alpha(0.20)
			bd = pal.Warning
		}
		hasBorder = true
	default:
		fg = pal.Accent
		if showLabel {
			bg = pal.Accent.Alpha(0.10)
			bd = pal.Accent.Alpha(0.32)
			if prs {
				bg = pal.SelectionBG
				bd = pal.Accent
			} else if hov {
				bg = pal.SelectionBG
				bd = pal.Accent.Alpha(0.65)
			}
			hasBorder = true
		} else {
			bg = ui.Transparent
			bd = ui.Transparent
			if prs {
				bg = pal.Selected
			} else if hov {
				bg = pal.Hover
			}
		}
	}

	bui.AnimateSurfaceColors(btn, bg, bd, hasBorder)
	bui.TactileButton(btn, hov, prs, false)
	btn.Children(func() {
		icons.DrawAnimated(c, glyph, theme.IconMicro, pal.DuoIconColors(fg), hov || prs)
		if showLabel {
			ui.Text(c, outlineLinkBadgeLabel(target, status)).
				FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
				TextColor(fg).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
	})
	bui.Probe(btn, key)
	if btn.Clicked() {
		st.openNodeLink(c.Now(), node, cb)
		c.Invalidate()
	}
}

func (st *BlockState) renderOutlineNodeQuickActions(c *ui.Context, keyPrefix string, node *Node, allowDelete bool, cb Callbacks) {
	pal := theme.Ocean()
	addBtn := bui.IconButton(c, icons.Plus, bui.IconButtonOptions{
		ID:       keyPrefix + "-add",
		Label:    theme.Tr("添加子节点", "Add child"),
		Tooltip:  theme.Tr("添加子节点", "Add child"),
		Variant:  bui.IconPlain,
		Size:     theme.HeightMicro,
		IconSize: theme.IconMicro,
		Radius:   theme.RadiusXS,
	})
	if addBtn.Clicked() {
		st.outlinePending = &outlineAction{kind: outlineActAddChild, nodeID: node.ID}
	}

	hasLink := strings.TrimSpace(node.Link) != ""
	linkBroken := hasLink && st.resolveLinkStatusCached(c.Now(), node.Link, cb) == LinkStatusBroken
	linkGlyph := icons.TbLink
	linkTip := theme.Tr("添加链接", "Add link")
	if linkBroken {
		linkGlyph = icons.LinkEdit
		linkTip = theme.Tr("重新选择链接", "Reconnect link")
	} else if hasLink {
		linkGlyph = icons.LinkEdit
		linkTip = theme.Tr("编辑链接", "Edit link")
	}
	linkOpts := bui.IconButtonOptions{
		ID:       keyPrefix + "-link",
		Label:    linkTip,
		Tooltip:  linkTip,
		Variant:  bui.IconPlain,
		Size:     theme.HeightMicro,
		IconSize: theme.IconMicro,
		Radius:   theme.RadiusXS,
	}
	if linkBroken {
		linkOpts.RestColor = pal.Warning
	} else if hasLink {
		linkOpts.RestColor = pal.Accent
	}
	linkEditBtn := bui.IconButton(c, linkGlyph, linkOpts)
	if linkEditBtn.Clicked() {
		if linkBroken {
			st.beginLinkEditBroken(node)
		} else {
			st.beginLinkEdit(node)
		}
		c.Invalidate()
	}

	if allowDelete {
		delBtn := bui.IconButton(c, icons.Trash, bui.IconButtonOptions{
			ID:       keyPrefix + "-del",
			Label:    theme.Tr("删除分支", "Delete branch"),
			Tooltip:  theme.Tr("删除分支", "Delete branch"),
			Variant:  bui.IconPlain,
			Size:     theme.HeightMicro,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusXS,
			Danger:   true,
		})
		if delBtn.Clicked() {
			st.outlinePending = &outlineAction{kind: outlineActDeleteNode, nodeID: node.ID}
		}
	}
}

func matchOutlineTodoPrefix(s string) (done bool, rest string, ok bool) {
	switch {
	case strings.HasPrefix(s, "[] "):
		return false, s[3:], true
	case strings.HasPrefix(s, "[ ] "):
		return false, s[4:], true
	case strings.HasPrefix(s, "[x] "), strings.HasPrefix(s, "[X] "):
		return true, s[4:], true
	default:
		return false, s, false
	}
}

func (st *BlockState) applyOutlineAction(c *ui.Context, act *outlineAction, fullscreen bool, cb Callbacks) {
	if act == nil || st.Root == nil {
		return
	}
	activeRoot := st.outlineActiveRoot()
	if activeRoot == nil {
		return
	}

	switch act.kind {
	case outlineActUndo:
		st.OutlineUndoNodeID = 0
		st.opUndo(cb)
		c.Invalidate()
		return
	case outlineActRedo:
		st.OutlineUndoNodeID = 0
		st.opRedo(cb)
		c.Invalidate()
		return
	case outlineActToggleFoldAll:
		st.outlineToggleFoldAll()
		c.Invalidate()
		return
	case outlineActZoomTo:
		if target := st.Root.FindByID(act.nodeID); target != nil {
			st.OutlineZoomID = target.ID
			if target == st.Root {
				st.OutlineZoomID = 0
			}
			target.Folded = false
			st.focusOutlineNode(target.ID, utf8.RuneCountInString(target.Text))
			c.Invalidate()
		}
		return
	case outlineActZoomOut:
		if activeRoot != st.Root && activeRoot.Parent != nil {
			if activeRoot.Parent == st.Root {
				st.OutlineZoomID = 0
			} else {
				st.OutlineZoomID = activeRoot.Parent.ID
			}
			activeRoot.Parent.Folded = false
			st.focusOutlineNode(activeRoot.ID, utf8.RuneCountInString(activeRoot.Text))
		} else {
			st.OutlineZoomID = 0
		}
		c.Invalidate()
		return
	case outlineActExitFullscreen:
		if fullscreen {
			st.Fullscreen = false
			st.AutoFitPending = true
			c.Invalidate()
		}
		return
	case outlineActTitleEnter:
		if cb.ReadOnly {
			return
		}
		if len(activeRoot.Children) == 0 {
			st.pushUndo()
			st.OutlineUndoNodeID = 0
			created := &Node{ID: st.allocID(), Text: "", Parent: activeRoot}
			activeRoot.Children = append(activeRoot.Children, created)
			st.commitChange(cb)
			st.focusOutlineNode(created.ID, 0)
		} else {
			first := activeRoot.Children[0]
			st.focusOutlineNode(first.ID, 0)
		}
		c.Invalidate()
		return
	case outlineActTitleDown:
		if len(activeRoot.Children) > 0 {
			first := activeRoot.Children[0]
			st.focusOutlineNode(first.ID, 0)
			c.Invalidate()
		}
		return
	case outlineActAddChild:
		if cb.ReadOnly {
			return
		}
		target := activeRoot.FindByID(act.nodeID)
		if target == nil {
			return
		}
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		target.Folded = false
		created := &Node{ID: st.allocID(), Text: "", Parent: target}
		target.Children = append(target.Children, created)
		st.commitChange(cb)
		st.focusOutlineNode(created.ID, 0)
		c.Invalidate()
		return
	}

	node := activeRoot.FindByID(act.nodeID)
	if node == nil || node == activeRoot {
		return
	}
	live := normalizeNodeLineEndings(act.text)

	switch act.kind {
	case outlineActDeleteNode:
		if cb.ReadOnly || node.Parent == nil {
			return
		}
		parent := node.Parent
		idx := node.IndexInParent()
		if idx < 0 {
			return
		}
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		if st.LinkNodeID != 0 && (st.LinkNodeID == node.ID || node.FindByID(st.LinkNodeID) != nil) {
			st.LinkNodeID = 0
			st.LinkBrokenNotice = false
		}
		parent.Children = append(parent.Children[:idx], parent.Children[idx+1:]...)
		st.commitChange(cb)
		if st.OutlineFocusID == 0 || st.OutlineFocusID == node.ID || node.FindByID(st.OutlineFocusID) != nil {
			var nextFocus *Node
			if idx < len(parent.Children) {
				nextFocus = parent.Children[idx]
			} else if idx-1 >= 0 {
				nextFocus = parent.Children[idx-1]
			} else {
				nextFocus = parent
			}
			if nextFocus != nil {
				st.focusOutlineNode(nextFocus.ID, utf8.RuneCountInString(nextFocus.Text))
			}
		}
		c.Invalidate()
	case outlineActToggleFold:
		if len(node.Children) > 0 {
			node.Folded = !node.Folded
			st.focusOutlineNode(node.ID, act.selStart)
			c.Invalidate()
		}

	case outlineActToggleTask:
		if cb.ReadOnly {
			return
		}
		node.Text = live
		st.OutlineUndoNodeID = 0
		st.opToggleTaskDoneOrCreate(node, cb)
		st.focusOutlineNode(node.ID, act.selStart)
		c.Invalidate()

	case outlineActNodeEnter:
		if cb.ReadOnly || node.Parent == nil {
			return
		}
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		runes := []rune(live)
		s0 := clampInt(act.selStart, 0, len(runes))
		s1 := clampInt(act.selEnd, s0, len(runes))

		var inheritedTask *bool
		if node.Task != nil {
			inheritedTask = BoolPtr(false)
		}

		// At the start of a non-empty node, insert above and keep the caret in place.
		if s0 == 0 && s1 == 0 && len(runes) > 0 {
			node.Text = live
			idx := node.IndexInParent()
			blank := &Node{ID: st.allocID(), Task: inheritedTask, Parent: node.Parent}
			node.Parent.Children = append(node.Parent.Children, nil)
			copy(node.Parent.Children[idx+1:], node.Parent.Children[idx:])
			node.Parent.Children[idx] = blank
			st.commitChange(cb)
			st.focusOutlineNode(node.ID, 0)
			c.Invalidate()
			return
		}

		before := string(runes[:s0])
		after := string(runes[s1:])
		node.Text = before

		created := &Node{ID: st.allocID(), Text: after, Task: inheritedTask}

		// Mubu flow: Enter at end of an expanded parent inserts as its first child; otherwise inserts sibling below.
		if after == "" && len(node.Children) > 0 && !node.Folded {
			created.Parent = node
			node.Children = append([]*Node{created}, node.Children...)
		} else {
			idx := node.IndexInParent()
			created.Parent = node.Parent
			node.Parent.Children = append(node.Parent.Children, nil)
			copy(node.Parent.Children[idx+2:], node.Parent.Children[idx+1:])
			node.Parent.Children[idx+1] = created
		}
		st.commitChange(cb)
		st.focusOutlineNode(created.ID, 0)
		c.Invalidate()

	case outlineActIndent:
		if cb.ReadOnly || node.Parent == nil {
			return
		}
		idx := node.IndexInParent()
		if idx <= 0 {
			return
		}
		node.Text = live
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		prevSib := node.Parent.Children[idx-1]
		node.Parent.Children = append(node.Parent.Children[:idx], node.Parent.Children[idx+1:]...)
		prevSib.Folded = false
		node.Parent = prevSib
		prevSib.Children = append(prevSib.Children, node)
		st.commitChange(cb)
		st.focusOutlineNode(node.ID, act.selStart)
		c.Invalidate()

	case outlineActOutdent:
		if cb.ReadOnly || node.Parent == nil || node.Parent == activeRoot || node.Parent.Parent == nil {
			return
		}
		node.Text = live
		par := node.Parent
		grand := par.Parent
		idx := node.IndexInParent()
		parIdx := par.IndexInParent()
		if idx < 0 || parIdx < 0 {
			return
		}
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		par.Children = append(par.Children[:idx], par.Children[idx+1:]...)
		node.Parent = grand
		grand.Children = append(grand.Children, nil)
		copy(grand.Children[parIdx+2:], grand.Children[parIdx+1:])
		grand.Children[parIdx+1] = node
		st.commitChange(cb)
		st.focusOutlineNode(node.ID, act.selStart)
		c.Invalidate()

	case outlineActBackspaceStart:
		if cb.ReadOnly || node.Parent == nil {
			return
		}
		// Match Block editor: Backspace at line start of a todo item converts it back to a bullet first.
		if node.Task != nil {
			node.Text = live
			st.pushUndo()
			st.OutlineUndoNodeID = 0
			node.Task = nil
			st.commitChange(cb)
			st.focusOutlineNode(node.ID, 0)
			c.Invalidate()
			return
		}
		if len(node.Children) > 0 {
			return
		}
		flat := outlineVisibleNodes(activeRoot)
		flatIdx := -1
		for i, n := range flat {
			if n.ID == node.ID {
				flatIdx = i
				break
			}
		}
		idx := node.IndexInParent()
		if idx < 0 {
			return
		}
		if live == "" {
			st.pushUndo()
			st.OutlineUndoNodeID = 0
			node.Parent.Children = append(node.Parent.Children[:idx], node.Parent.Children[idx+1:]...)
			st.commitChange(cb)
			if flatIdx > 0 {
				prev := flat[flatIdx-1]
				st.focusOutlineNode(prev.ID, utf8.RuneCountInString(prev.Text))
			} else if len(flat) > 1 {
				st.focusOutlineNode(flat[1].ID, 0)
			} else {
				st.focusOutlineNode(activeRoot.ID, utf8.RuneCountInString(activeRoot.Text))
			}
			c.Invalidate()
			return
		}
		if flatIdx > 0 {
			prev := flat[flatIdx-1]
			joinCaret := utf8.RuneCountInString(prev.Text)
			st.pushUndo()
			st.OutlineUndoNodeID = 0
			prev.Text += live
			node.Parent.Children = append(node.Parent.Children[:idx], node.Parent.Children[idx+1:]...)
			st.commitChange(cb)
			st.focusOutlineNode(prev.ID, joinCaret)
			c.Invalidate()
		}

	case outlineActMoveUp, outlineActMoveDown:
		if cb.ReadOnly || node.Parent == nil {
			return
		}
		idx := node.IndexInParent()
		target := idx - 1
		if act.kind == outlineActMoveDown {
			target = idx + 1
		}
		if idx < 0 || target < 0 || target >= len(node.Parent.Children) {
			return
		}
		node.Text = live
		st.pushUndo()
		st.OutlineUndoNodeID = 0
		ch := node.Parent.Children
		ch[idx], ch[target] = ch[target], ch[idx]
		st.commitChange(cb)
		st.focusOutlineNode(node.ID, act.selStart)
		c.Invalidate()

	case outlineActArrowUp:
		flat := outlineVisibleNodes(activeRoot)
		for i, n := range flat {
			if n.ID == node.ID {
				if i > 0 {
					prev := flat[i-1]
					st.focusOutlineNode(prev.ID, utf8.RuneCountInString(prev.Text))
				} else {
					st.focusOutlineNode(activeRoot.ID, utf8.RuneCountInString(activeRoot.Text))
				}
				c.Invalidate()
				return
			}
		}

	case outlineActArrowDown:
		flat := outlineVisibleNodes(activeRoot)
		for i, n := range flat {
			if n.ID == node.ID && i+1 < len(flat) {
				next := flat[i+1]
				caret := min(act.selStart, utf8.RuneCountInString(next.Text))
				st.focusOutlineNode(next.ID, caret)
				c.Invalidate()
				return
			}
		}
	}
}
