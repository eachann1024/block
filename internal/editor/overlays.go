package editor

import (
	"fmt"
	"strings"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// --- 1. In-Note Find Bar (Cmd+F) ---

func (s *Session) renderFindBar(c *ui.Context) {
	if !s.findOpen {
		return
	}
	pal := theme.Ocean()
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("find-bar")).Absolute().Top(12).Right(20).Height(40).
			PaddingX(10).Gap(6).AlignItems(ui.Center).
			Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
		appUi.Probe(bar, "find-bar")

		bar.Children(func() {
			icons.Draw(c, icons.FindSearch, 16, pal.MonoIconColors(pal.TextSecondary))

			in := ui.TextInputBase(c.Key("find-input"), &s.findQuery).Width(180).Height(28).
				FontSize(14).TextColor(pal.Text).CaretColor(pal.Accent).
				Placeholder(theme.Tr("查找…", "Find…")).Label("find-input").
				Bind(&s.findInputHandle)
			appUi.Probe(in, "find-input")
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter {
					if k.Mods == ui.Shift {
						s.FindPrev()
					} else {
						s.FindNext()
					}
					return true
				}
				if k.Key == ui.KeyEscape {
					s.CloseFind()
					return true
				}
				return false
			})
			if s.findFocusPending {
				in.Focus()
				s.findFocusPending = false
			}
			if in.Changed() {
				s.SetFindQuery(s.findQuery)
			}

			countText := "0/0"
			if len(s.findMatches) > 0 {
				countText = fmt.Sprintf("%d/%d", s.findCurrent+1, len(s.findMatches))
			}
			ui.Text(c, countText).FontSize(14).TextColor(pal.TextSecondary).SingleLine()

			prevBtn := appUi.IconButton(c, icons.ChevronUp, appUi.IconButtonOptions{
				ID:       "find-prev",
				Label:    theme.Tr("上一个", "Previous match"),
				Size:     26,
				IconSize: 15,
				Disabled: len(s.findMatches) == 0,
			})
			if prevBtn.Clicked() {
				s.FindPrev()
			}

			nextBtn := appUi.IconButton(c, icons.ChevronDown, appUi.IconButtonOptions{
				ID:       "find-next",
				Label:    theme.Tr("下一个", "Next match"),
				Size:     26,
				IconSize: 15,
				Disabled: len(s.findMatches) == 0,
			})
			if nextBtn.Clicked() {
				s.FindNext()
			}

			closeBtn := appUi.IconButton(c, icons.Close, appUi.IconButtonOptions{
				ID:       "find-close",
				Label:    theme.Tr("关闭查找", "Close find"),
				Size:     26,
				IconSize: 15,
			})
			if closeBtn.Clicked() {
				s.CloseFind()
			}
		})
	})
}

// --- 2. Single-Row Adaptive Selection Floating Toolbar & Format Popovers ---

type toolbarItemKind uint8

const (
	tbAIRewrite toolbarItemKind = iota
	tbComment
	tbReaction
	tbTextType
	tbBold
	tbItalic
	tbUnderline
	tbStrike
	tbCode
	tbPalette
	tbLists
	tbLink
)

type toolbarItemSpec struct {
	Kind       toolbarItemKind
	ID         string
	Group      uint8
	Width      float32
	Icon       icons.Icon
	Label      string
	Shortcut   string
	HasSubmenu bool
}

func (s *Session) enabledToolbarItems(caps toolbarCaps) []toolbarItemSpec {
	var items []toolbarItemSpec
	if !s.readOnly && s.isMarkdownNote() && s.canReplaceSelection() && s.hasNonEmptySelection() {
		w := float32(96)
		if theme.IsEn() {
			w = 116
		}
		items = append(items, toolbarItemSpec{
			Kind: tbAIRewrite, ID: "tb-ai", Group: 0, Width: w,
			Icon: icons.Sparkles, Label: theme.Tr("AI 改写", "AI Rewrite"),
		})
	}
	items = append(items,
		toolbarItemSpec{
			Kind: tbComment, ID: "tb-comment", Group: 1, Width: 28,
			Icon: icons.TbComment, Label: theme.Tr("添加批注", "Add annotation"),
			Shortcut: formatShortcutDisplay("⌥⌘M", "Ctrl+Alt+M"),
		},
		toolbarItemSpec{
			Kind: tbReaction, ID: "tb-reaction", Group: 1, Width: 28,
			Icon: icons.TbReaction, Label: theme.Tr("添加表情回应", "Add reaction"),
		},
	)
	if caps.BlockType {
		items = append(items, toolbarItemSpec{
			Kind: tbTextType, ID: "tb-type", Group: 2, Width: 41,
			Icon: icons.TbType, Label: theme.Tr("文字类型", "Text type"), HasSubmenu: true,
		})
	}
	if caps.RichText {
		if caps.Bold {
			items = append(items, toolbarItemSpec{
				Kind: tbBold, ID: "tb-bold", Group: 3, Width: 28,
				Icon: icons.TbBold, Label: theme.Tr("粗体", "Bold"),
				Shortcut: formatShortcutDisplay("⌘B", "Ctrl+B"),
			})
		}
		items = append(items,
			toolbarItemSpec{
				Kind: tbItalic, ID: "tb-italic", Group: 3, Width: 28,
				Icon: icons.TbItalic, Label: theme.Tr("斜体", "Italic"),
				Shortcut: formatShortcutDisplay("⌘I", "Ctrl+I"),
			},
			toolbarItemSpec{
				Kind: tbUnderline, ID: "tb-underline", Group: 3, Width: 28,
				Icon: icons.TbUnderline, Label: theme.Tr("下划线", "Underline"),
				Shortcut: formatShortcutDisplay("⌘U", "Ctrl+U"),
			},
			toolbarItemSpec{
				Kind: tbStrike, ID: "tb-strike", Group: 3, Width: 28,
				Icon: icons.TbStrike, Label: theme.Tr("删除线", "Strikethrough"),
				Shortcut: formatShortcutDisplay("⇧⌘S", "Ctrl+Shift+S"),
			},
			toolbarItemSpec{
				Kind: tbCode, ID: "tb-code", Group: 3, Width: 28,
				Icon: icons.TbCode, Label: theme.Tr("行内代码", "Inline code"),
				Shortcut: formatShortcutDisplay("⌘E", "Ctrl+E"),
			},
			toolbarItemSpec{
				Kind: tbPalette, ID: "tb-palette", Group: 3, Width: 41,
				Icon: icons.TbPalette, Label: theme.Tr("文本与高亮颜色", "Text and highlight color"), HasSubmenu: true,
			},
		)
	}
	if caps.BlockType {
		items = append(items, toolbarItemSpec{
			Kind: tbLists, ID: "tb-list", Group: 4, Width: 41,
			Icon: icons.TbList, Label: theme.Tr("列表", "Lists"), HasSubmenu: true,
		})
	}
	if caps.Link {
		items = append(items, toolbarItemSpec{
			Kind: tbLink, ID: "tb-link", Group: 5, Width: 28,
			Icon: icons.TbLink, Label: theme.Tr("链接", "Link"),
			Shortcut: formatShortcutDisplay("⇧⌘K", "Ctrl+Shift+K"),
		})
	}
	return items
}

func sequenceToolbarWidth(items []toolbarItemSpec, withMore bool) float32 {
	if len(items) == 0 && !withMore {
		return 0
	}
	w := float32(14)
	hasPrev := false
	var prevGroup uint8
	for _, it := range items {
		if hasPrev {
			if prevGroup == it.Group {
				w += 2
			} else {
				w += 13
			}
		}
		w += it.Width
		prevGroup = it.Group
		hasPrev = true
	}
	if withMore {
		if hasPrev {
			w += 13
		}
		w += 28
	}
	return w
}

func splitForRoom(enabled []toolbarItemSpec, room float32) (keep int, width float32) {
	full := sequenceToolbarWidth(enabled, false)
	if full <= room {
		return len(enabled), full
	}
	for k := len(enabled) - 1; k >= 0; k-- {
		if w := sequenceToolbarWidth(enabled[:k], true); w <= room {
			return k, w
		}
	}
	return 0, sequenceToolbarWidth(nil, true)
}

func (s *Session) toolbarItemActive(kind toolbarItemKind) bool {
	switch kind {
	case tbBold:
		return s.selectionHasMark(model.MarkBold)
	case tbItalic:
		return s.selectionHasMark(model.MarkItalic)
	case tbUnderline:
		return s.selectionHasMark(model.MarkUnderline)
	case tbStrike:
		return s.selectionHasMark(model.MarkStrike)
	case tbCode:
		return s.selectionHasMark(model.MarkCode)
	case tbPalette:
		fg, bg := s.selectionColors()
		return fg != "default" || bg != "default" || s.toolbarPop == "palette"
	case tbLink:
		return s.selectionLinkURL() != "" || s.toolbarPop == "link"
	case tbComment:
		return s.toolbarPop == "comment"
	case tbReaction:
		return s.toolbarPop == "reactions"
	case tbTextType:
		return s.toolbarPop == "kinds"
	case tbLists:
		return s.toolbarPop == "lists"
	default:
		return false
	}
}

func (s *Session) runToolbarItem(kind toolbarItemKind, fromMore bool) {
	if fromMore && kind != tbTextType && kind != tbPalette && kind != tbLists {
		s.toolbarPop = ""
	}
	switch kind {
	case tbAIRewrite:
		s.OpenInlineAIOnSelection()
	case tbComment:
		s.toggleToolbarPop("comment")
	case tbReaction:
		s.toggleToolbarPop("reactions")
	case tbTextType:
		s.toggleToolbarPop("kinds")
	case tbBold:
		s.ToggleMarkOnSelection(model.MarkBold)
	case tbItalic:
		s.ToggleMarkOnSelection(model.MarkItalic)
	case tbUnderline:
		s.ToggleMarkOnSelection(model.MarkUnderline)
	case tbStrike:
		s.ToggleMarkOnSelection(model.MarkStrike)
	case tbCode:
		s.ToggleMarkOnSelection(model.MarkCode)
	case tbPalette:
		s.toggleToolbarPop("palette")
	case tbLists:
		s.toggleToolbarPop("lists")
	case tbLink:
		if s.toolbarPop != "link" {
			s.linkDraft = s.selectionLinkURL()
			s.linkFocusPending = true
		}
		s.toggleToolbarPop("link")
	}
}

func (s *Session) renderSelectionToolbar(c *ui.Context) {
	if !s.IsSelectionToolbarOpen() && s.toolbarPop == "" {
		return
	}
	pal := theme.Ocean()
	a, _ := s.orderedSelection()
	anchorY := float32(60)
	anchorX := float32(120)
	if a.Block >= 0 && a.Block < len(s.doc.Blocks) {
		if r, ok := s.blockRects[s.doc.Blocks[a.Block].ID]; ok && r.W > 0 {
			anchorX = max(r.X+editorHandleWidth, 16)
			anchorY = r.Y - 46
			if anchorY < 10 {
				anchorY = r.Y + r.H + 6
			}
		}
	}

	availW := s.viewportWidth
	if s.editorRect.W > 0 && s.editorRect.W < availW {
		availW = s.editorRect.W
	}
	room := max(availW-16, 48)
	caps := s.toolbarCapabilities()
	enabled := s.enabledToolbarItems(caps)
	keep, barW := splitForRoom(enabled, room)
	inline := enabled[:keep]
	overflow := enabled[keep:]
	if s.toolbarPop == "more" && len(overflow) == 0 {
		s.toolbarPop = ""
	}
	minX, maxX, _, _ := s.overlayBounds(c)
	anchorX = clampFloat32(anchorX, minX, max(maxX-barW, minX))

	ui.Overlay(c, func() {
		if s.IsSelectionToolbarOpen() {
			bar := ui.Row(c.Key("selection-toolbar")).Absolute().Left(anchorX).Top(anchorY).
				Height(38).Padding(4, 6).Gap(2).AlignItems(ui.Center).
				Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.Hairline).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
			bar.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				s.toolbarRect = r
				appUi.RecordBounds("selection-toolbar", r)
			})

			bar.Children(func() {
				hasPrev := false
				var prevGroup uint8
				for _, item := range inline {
					it := item
					if hasPrev && prevGroup != it.Group {
						toolbarDivider(c)
					}
					hasPrev = true
					prevGroup = it.Group

					if it.Kind == tbAIRewrite {
						aiBtn := ui.ButtonBase(c.Key("tb-ai")).Height(28).PaddingX(8).Gap(4).
							AlignItems(ui.Center).Radius(6).Background(pal.Selected).
							Border(1, pal.Accent).Cursor(ui.CursorPointer).
							Label(it.Label)
						aiBtn.Children(func() {
							icons.Draw(c, icons.Sparkles, 15, pal.MonoIconColors(pal.Accent))
							ui.Text(c, it.Label).FontSize(14).TextColor(pal.Text).SingleLine()
						})
						appUi.Probe(aiBtn, "tb-ai")
						if aiBtn.Clicked() {
							s.runToolbarItem(tbAIRewrite, false)
						}
						continue
					}

					active := s.toolbarItemActive(it.Kind)
					if it.HasSubmenu {
						btn := ui.ButtonBase(c.Key(it.ID)).Height(28).MinWidth(41).PaddingX(5).Gap(2).
							Center().Radius(6).Cursor(ui.CursorPointer).Label(it.Label)
						if active {
							btn.Background(pal.Selected)
						} else if btn.Hovered() {
							btn.Background(pal.Hover)
						}
						btn.Children(func() {
							ic := pal.MonoIconColors(pal.TextSecondary)
							if active || btn.Hovered() {
								ic = pal.DuoIconColors(pal.Text)
							}
							icons.Draw(c, it.Icon, 16, ic)
							icons.Draw(c, icons.TbChevron, 10, pal.MonoIconColors(pal.TextSecondary))
						})
						appUi.Probe(btn, it.ID)
						if btn.Clicked() {
							s.runToolbarItem(it.Kind, false)
						}
					} else {
						btn := appUi.IconButton(c, it.Icon, appUi.IconButtonOptions{
							ID: it.ID, Label: it.Label, Size: 28, IconSize: 16, Active: active,
						})
						if btn.Clicked() {
							s.runToolbarItem(it.Kind, false)
						}
					}
				}

				if len(overflow) > 0 {
					if hasPrev {
						toolbarDivider(c)
					}
					moreActive := s.toolbarPop == "more"
					for _, ov := range overflow {
						if s.toolbarItemActive(ov.Kind) {
							moreActive = true
							break
						}
					}
					moreBtn := appUi.IconButton(c, icons.More, appUi.IconButtonOptions{
						ID: "tb-more", Label: theme.Tr("更多格式", "More formatting"), Size: 28, IconSize: 16, Active: moreActive,
					})
					if moreBtn.Clicked() {
						s.toggleToolbarPop("more")
					}
				}
			})
		}

		// Toolbar sub-popovers
		popX := anchorX
		popY := anchorY + 42
		if s.toolbarRect.W > 0 {
			popX = s.toolbarRect.X
			popY = s.toolbarRect.Y + s.toolbarRect.H + 4
		}
		if popY+220 > s.viewportHeight-8 {
			popY = max(anchorY-224, 8)
		}

		switch s.toolbarPop {
		case "more":
			s.renderToolbarMoreMenu(c, popX+max(barW-228, 0), popY, overflow)
		case "kinds":
			s.renderToolbarKindsMenu(c, popX, popY, false)
		case "lists":
			s.renderToolbarKindsMenu(c, popX+180, popY, true)
		case "palette":
			s.renderPalettePanel(c, popX+140, popY)
		case "link":
			s.renderLinkCardPopover(c, popX+200, popY)
		case "reactions":
			s.renderReactionPicker(c, popX+60, popY)
		case "comment":
			s.renderCommentComposer(c, popX+40, popY)
		}
	})
}

func (s *Session) renderToolbarMoreMenu(c *ui.Context, x, y float32, overflow []toolbarItemSpec) {
	if len(overflow) == 0 {
		return
	}
	pal := theme.Ocean()
	menuW := float32(228)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	panel := ui.Column(c.Key("toolbar-more-menu")).Absolute().Left(x).Top(y).Width(menuW).
		Padding(4).Gap(2).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(panel, "toolbar-more-menu")

	panel.Children(func() {
		hasPrev := false
		var prevGroup uint8
		for i, item := range overflow {
			it := item
			if hasPrev && prevGroup != it.Group {
				appUi.MenuRow(c, appUi.MenuItemSpec{Separator: true}, false)
			}
			hasPrev = true
			prevGroup = it.Group
			ic := it.Icon
			appUi.MenuRow(c, appUi.MenuItemSpec{
				ID:         fmt.Sprintf("toolbar-more-item-%d", i),
				Label:      it.Label,
				Shortcut:   it.Shortcut,
				Icon:       &ic,
				Checked:    s.toolbarItemActive(it.Kind),
				HasSubmenu: it.HasSubmenu,
				OnClick: func() {
					s.runToolbarItem(it.Kind, true)
				},
			}, false)
		}
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func toolbarDivider(c *ui.Context) {
	pal := theme.Ocean()
	ui.Box(c).Width(1).Height(20).MarginX(2).Background(pal.Hairline)
}

func (s *Session) toggleToolbarPop(name string) {
	if s.toolbarPop == name {
		s.toolbarPop = ""
		return
	}
	s.toolbarPop = name
	if name == "comment" {
		s.commentFocusPending = true
	}
}

func (s *Session) renderToolbarKindsMenu(c *ui.Context, x, y float32, listsOnly bool) {
	pal := theme.Ocean()
	menuW := float32(206)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	items := []struct {
		id    string
		kind  model.BlockKind
		icon  icons.Icon
		label string
	}{
		{"kind-p", model.BlockParagraph, icons.Text, theme.Tr("正文", "Text")},
		{"kind-h1", model.BlockHeading1, icons.Heading1, theme.Tr("一级标题", "Heading 1")},
		{"kind-h2", model.BlockHeading2, icons.Heading2, theme.Tr("二级标题", "Heading 2")},
		{"kind-h3", model.BlockHeading3, icons.Heading3, theme.Tr("三级标题", "Heading 3")},
	}
	if listsOnly {
		items = []struct {
			id    string
			kind  model.BlockKind
			icon  icons.Icon
			label string
		}{
			{"list-bullet", model.BlockBullet, icons.ListBullet, theme.Tr("无序列表", "Bulleted list")},
			{"list-num", model.BlockNumbered, icons.ListNumbered, theme.Tr("有序列表", "Numbered list")},
			{"list-todo", model.BlockTodo, icons.Todo, theme.Tr("待办清单", "Checklist")},
		}
	}

	var curKind model.BlockKind
	if s.focusedBlock >= 0 && s.focusedBlock < len(s.doc.Blocks) {
		curKind = s.doc.Blocks[s.focusedBlock].Kind
	}

	panel := ui.Column(c.Key("toolbar-kind-menu")).Absolute().Left(x).Top(y).Width(menuW).
		Padding(4).Gap(2).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(panel, "toolbar-kind-menu")
	panel.Children(func() {
		for _, it := range items {
			ic := it.icon
			appUi.MenuRow(c, appUi.MenuItemSpec{
				ID:      it.id,
				Label:   it.label,
				Icon:    &ic,
				Checked: curKind == it.kind,
				OnClick: func() {
					s.SetSelectionBlockKind(it.kind, listsOnly)
					s.toolbarPop = ""
				},
			}, false)
		}
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderPalettePanel(c *ui.Context, x, y float32) {
	pal := theme.Ocean()
	dark := theme.IsDark()
	menuW := float32(196)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))
	curFG, curBG := s.selectionColors()

	panel := ui.Column(c.Key("palette-panel")).Absolute().Left(x).Top(y).Width(menuW).
		Padding(6).Gap(2).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(panel, "palette-panel")

	panel.Children(func() {
		ui.Text(c, theme.Tr("文字颜色", "Text color")).FontSize(14).FontWeight(600).
			TextColor(pal.TextSecondary).Padding(4, 8)
		swatches := theme.FormatPalette(dark)
		limit := min(len(swatches), 6)
		for _, sw := range swatches[:limit] {
			name := sw.Name
			lbl := sw.Zh
			if theme.IsEn() {
				lbl = sw.En
			}
			row := ui.ButtonBase(c.Key("fg-sw-"+name)).FillWidth().Height(28).PaddingX(8).Gap(8).
				AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(lbl)
			if curFG == name {
				row.Background(pal.Selected)
			} else if row.Hovered() {
				row.Background(pal.Hover)
			}
			row.Children(func() {
				ui.Box(c).Size(16, 16).Radius(4).Background(sw.FG).Border(1, pal.Hairline)
				ui.Text(c, lbl).FontSize(14).TextColor(pal.Text).Grow(1)
				if curFG == name {
					icons.Draw(c, icons.Check, 14, pal.MonoIconColors(pal.Accent))
				}
			})
			appUi.Probe(row, "fg-sw-"+name)
			if row.Clicked() {
				s.ApplyColorOnSelection(&name, nil)
				s.toolbarPop = ""
			}
		}

		ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.Hairline)
		ui.Text(c, theme.Tr("高亮背景", "Highlight")).FontSize(14).FontWeight(600).
			TextColor(pal.TextSecondary).Padding(4, 8)

		for _, bgName := range []string{"default", "yellow"} {
			lbl := theme.Tr("无高亮", "None")
			swatchCol := pal.Paper
			if bgName == "yellow" {
				lbl = theme.Tr("黄色", "Yellow")
				if cVal, ok := theme.ResolveNamedBG("yellow", dark); ok {
					swatchCol = cVal
				}
			}
			row := ui.ButtonBase(c.Key("bg-sw-"+bgName)).FillWidth().Height(28).PaddingX(8).Gap(8).
				AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(lbl)
			if curBG == bgName {
				row.Background(pal.Selected)
			} else if row.Hovered() {
				row.Background(pal.Hover)
			}
			row.Children(func() {
				ui.Box(c).Size(16, 16).Radius(4).Background(swatchCol).Border(1, pal.BtnBorder)
				ui.Text(c, lbl).FontSize(14).TextColor(pal.Text).Grow(1)
				if curBG == bgName {
					icons.Draw(c, icons.Check, 14, pal.MonoIconColors(pal.Accent))
				}
			})
			appUi.Probe(row, "bg-sw-"+bgName)
			if row.Clicked() {
				bg := bgName
				s.ApplyColorOnSelection(nil, &bg)
				s.toolbarPop = ""
			}
		}
	})
	if panel.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderLinkCardPopover(c *ui.Context, x, y float32) {
	pal := theme.Ocean()
	cardW := float32(320)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-cardW, minX))

	card := ui.Row(c.Key("format-link-card")).Absolute().Left(x).Top(y).Width(cardW).
		Padding(8, 10).Gap(6).AlignItems(ui.Center).
		Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(card, "format-link-card")

	card.Children(func() {
		in := ui.TextInputBase(c.Key("link-input"), &s.linkDraft).Grow(1).Height(28).
			PaddingX(8).Radius(6).Border(1, pal.BtnBorder).FontSize(14).
			TextColor(pal.Text).Placeholder("https://...").Label("link-url").
			Bind(&s.linkInputHandle)
		if s.linkFocusPending {
			in.Focus()
			s.linkFocusPending = false
		}
		if in.Submitted() {
			s.ApplyLinkOnSelection(s.linkDraft)
			s.toolbarPop = ""
		}

		applyBtn := appUi.ActionButton(c, "link-apply", theme.Tr("应用", "Apply"), nil, appUi.ButtonPrimary, false)
		if applyBtn.Clicked() {
			s.ApplyLinkOnSelection(s.linkDraft)
			s.toolbarPop = ""
		}
		if s.selectionLinkURL() != "" {
			rmBtn := appUi.IconButton(c, icons.LinkOff, appUi.IconButtonOptions{
				ID: "link-remove", Label: theme.Tr("移除链接", "Remove link"), Size: 28, IconSize: 15, Danger: true,
			})
			if rmBtn.Clicked() {
				s.ApplyLinkOnSelection("")
				s.toolbarPop = ""
			}
		}
	})
	if card.PressedOutside() {
		s.toolbarPop = ""
	}
}

var reactionEmojis = []string{"👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"}

func (s *Session) renderReactionPicker(c *ui.Context, x, y float32) {
	pal := theme.Ocean()
	w := float32(288)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-w, minX))

	bar := ui.Row(c.Key("annotate-reactions")).Absolute().Left(x).Top(y).Padding(6, 8).Gap(4).
		AlignItems(ui.Center).Radius(theme.RadiusSurface).Background(pal.Menu).
		Border(1, pal.Hairline).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(bar, "annotate-reactions")

	bar.Children(func() {
		for idx, emoji := range reactionEmojis {
			id := fmt.Sprintf("pick-emoji-%d", idx)
			btn := ui.ButtonBase(c.Key(id)).Size(30, 30).Center().Radius(6).
				Cursor(ui.CursorPointer).Label(emoji)
			if btn.Hovered() {
				btn.Background(pal.Hover)
			}
			btn.Children(func() {
				ui.Text(c, emoji).FontSize(16)
			})
			appUi.Probe(btn, id)
			if btn.Clicked() {
				s.AddReaction(emoji)
				s.toolbarPop = ""
			}
		}
	})
	if bar.PressedOutside() {
		s.toolbarPop = ""
	}
}

func (s *Session) renderCommentComposer(c *ui.Context, x, y float32) {
	pal := theme.Ocean()
	w := float32(280)
	minX, maxX, _, _ := s.overlayBounds(c)
	x = clampFloat32(x, minX, max(maxX-w, minX))

	card := ui.Row(c.Key("annotate-composer")).Absolute().Left(x).Top(y).Width(w).
		Padding(8, 10).Gap(6).AlignItems(ui.Center).
		Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Accent).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
	appUi.Probe(card, "annotate-composer")

	card.Children(func() {
		in := ui.TextInputBase(c.Key("comment-input"), &s.commentDraft).Grow(1).Height(28).
			PaddingX(8).FontSize(14).TextColor(pal.Text).
			Placeholder(theme.Tr("添加批注…", "Add annotation…")).Label("annotation-note").
			Bind(&s.commentHandle)
		if s.commentFocusPending {
			in.Focus()
			s.commentFocusPending = false
		}
		submit := func() {
			if strings.TrimSpace(s.commentDraft) != "" {
				s.AddAnnotation(s.commentDraft)
				s.commentDraft = ""
				s.toolbarPop = ""
			}
		}
		if in.Submitted() {
			submit()
		}
		btn := appUi.ActionButton(c, "annotate-submit", theme.Tr("保存", "Save"), nil, appUi.ButtonPrimary, strings.TrimSpace(s.commentDraft) == "")
		if btn.Clicked() {
			submit()
		}
	})
	if card.PressedOutside() {
		s.toolbarPop = ""
	}
}

// Callout 5-state semantic style & dropdown menu (aligned with src/ui/callout_controls.rs)

type calloutStyle struct {
	BG       ui.Color
	Border   ui.Color
	Accent   ui.Color
	IconFill ui.Color
	Text     ui.Color
}

var allCalloutKinds = []model.CalloutKind{
	model.CalloutNote,
	model.CalloutTip,
	model.CalloutImportant,
	model.CalloutWarning,
	model.CalloutCaution,
}

func calloutTone(kind model.CalloutKind, dark bool) calloutStyle {
	switch {
	case kind == model.CalloutTip && !dark:
		return calloutStyle{BG: ui.Hex("#edf7f2"), Border: ui.Hex("#bce3cf"), Accent: ui.Hex("#1f8a5b"), IconFill: ui.Hex("#d3efe1"), Text: ui.Hex("#166541")}
	case kind == model.CalloutImportant && !dark:
		return calloutStyle{BG: ui.Hex("#f3f0fb"), Border: ui.Hex("#d6ccf2"), Accent: ui.Hex("#6e56cf"), IconFill: ui.Hex("#e4ddf8"), Text: ui.Hex("#4c389e")}
	case kind == model.CalloutWarning && !dark:
		return calloutStyle{BG: ui.Hex("#fdf5e8"), Border: ui.Hex("#edd4a8"), Accent: ui.Hex("#b86b11"), IconFill: ui.Hex("#f7e2be"), Text: ui.Hex("#8a4e08")}
	case kind == model.CalloutCaution && !dark:
		return calloutStyle{BG: ui.Hex("#fceeee"), Border: ui.Hex("#f1c4c4"), Accent: ui.Hex("#c93b3b"), IconFill: ui.Hex("#f8dada"), Text: ui.Hex("#962727")}
	case !dark:
		return calloutStyle{BG: ui.Hex("#eef3fc"), Border: ui.Hex("#c9d8f3"), Accent: ui.Hex("#547dd0"), IconFill: ui.Hex("#dce7fa"), Text: ui.Hex("#2c4c8c")}
	case kind == model.CalloutTip && dark:
		return calloutStyle{BG: ui.Hex("#182923"), Border: ui.Hex("#284d3e"), Accent: ui.Hex("#4fd69c"), IconFill: ui.Hex("#203d31"), Text: ui.Hex("#7ce8b8")}
	case kind == model.CalloutImportant && dark:
		return calloutStyle{BG: ui.Hex("#231e34"), Border: ui.Hex("#3f3366"), Accent: ui.Hex("#a78bfa"), IconFill: ui.Hex("#312750"), Text: ui.Hex("#c4b5fd")}
	case kind == model.CalloutWarning && dark:
		return calloutStyle{BG: ui.Hex("#2b2316"), Border: ui.Hex("#544021"), Accent: ui.Hex("#f5a623"), IconFill: ui.Hex("#3d2f19"), Text: ui.Hex("#f8c165")}
	case kind == model.CalloutCaution && dark:
		return calloutStyle{BG: ui.Hex("#2d1b1e"), Border: ui.Hex("#592e34"), Accent: ui.Hex("#f26d6d"), IconFill: ui.Hex("#422328"), Text: ui.Hex("#ff9b9b")}
	default:
		return calloutStyle{BG: ui.Hex("#1b2434"), Border: ui.Hex("#2f4368"), Accent: ui.Hex("#7aa2f7"), IconFill: ui.Hex("#253552"), Text: ui.Hex("#9ab8ff")}
	}
}

func calloutLabel(kind model.CalloutKind) string {
	switch kind {
	case model.CalloutTip:
		return theme.Tr("技巧", "Tip")
	case model.CalloutImportant:
		return theme.Tr("重要", "Important")
	case model.CalloutWarning:
		return theme.Tr("警告", "Warning")
	case model.CalloutCaution:
		return theme.Tr("注意", "Caution")
	default:
		return theme.Tr("提示", "Note")
	}
}

func (s *Session) renderCalloutMenuOverlay(c *ui.Context) {
	if s.calloutMenuBlock == 0 {
		return
	}
	idx := s.blockIndexByID(s.calloutMenuBlock)
	if idx < 0 || s.doc.Blocks[idx].Kind != model.BlockCallout {
		s.calloutMenuBlock = 0
		return
	}
	pal := theme.Ocean()
	dark := theme.IsDark()
	b := &s.doc.Blocks[idx]
	curKind := b.Callout
	if curKind == "" {
		curKind = model.CalloutNote
	}

	trigger, ok := appUi.TargetBounds(fmt.Sprintf("callout-pill-%d", b.ID))
	if !ok {
		trigger = s.blockRects[b.ID]
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: 120, Y: 100, W: 140, H: 28}
	}
	menuW := float32(204)
	minX, maxX, winW, winH := s.overlayBounds(c)
	spec := appUi.PlaceBelow(trigger).WithGap(4).WithMargin(8)
	x, y := appUi.ResolveOrigin(spec, menuW, 240, winW, winH)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	ui.Overlay(c, func() {
		shell := ui.Column(c.Key("callout-kind-menu")).Absolute().Left(x).Top(y).Width(menuW).
			Padding(4).Gap(2).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
		appUi.Probe(shell, "callout-kind-menu")

		shell.Children(func() {
			for ix, k := range allCalloutKinds {
				kind := k
				st := calloutTone(kind, dark)
				checked := kind == curKind
				highlighted := s.calloutMenuIndex == ix
				row := ui.ButtonBase(c.Key(fmt.Sprintf("callout-opt-%d", ix))).FillWidth().Height(32).
					PaddingX(10).Gap(8).AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).
					Label(calloutLabel(kind))
				if checked {
					row.Background(pal.Selected).Border(1, pal.Accent)
				} else if highlighted || row.Hovered() {
					row.Background(pal.Hover)
				}
				row.Children(func() {
					ui.Box(c).Size(10, 10).Shrink(0).Radius(5).Background(st.BG).Border(1, st.Accent)
					ui.Text(c, calloutLabel(kind)).FontSize(14).FontWeight(boolToWeight(checked)).TextColor(pal.Text).Grow(1).SingleLine()
					ui.Text(c, string(kind)).Font("monospace").FontSize(12).TextColor(pal.TextSecondary).SingleLine()
				})
				appUi.Probe(row, fmt.Sprintf("callout-opt-%d", ix))
				if row.Clicked() {
					s.setCalloutKind(b.ID, &kind)
				}
			}

			ui.Box(c).FillWidth().Height(1).MarginY(4).Background(pal.Hairline)

			plainHighlighted := s.calloutMenuIndex == len(allCalloutKinds)
			ic := icons.Text
			appUi.MenuRow(c, appUi.MenuItemSpec{
				ID:    "callout-opt-plain",
				Label: theme.Tr("转为普通段落", "Convert to Text"),
				Icon:  &ic,
				OnClick: func() {
					s.setCalloutKind(b.ID, nil)
				},
			}, plainHighlighted)
		})

		if shell.PressedOutside() {
			s.calloutMenuBlock = 0
		}
	})
}

func boolToWeight(on bool) int {
	if on {
		return 600
	}
	return 400
}

// --- 3. Slash & Handle Block Menu Overlay (with internal scroll containment) ---

func (s *Session) renderBlockMenuOverlay(c *ui.Context) {
	if s.blockMenu == nil {
		return
	}
	entries := s.slashMenuEntries()
	if len(entries) == 0 {
		return
	}
	pal := theme.Ocean()
	menu := s.blockMenu
	menuW := float32(224)
	trigger := menu.TriggerRect
	if trigger.W <= 0 && menu.BlockIdx >= 0 && menu.BlockIdx < len(s.doc.Blocks) {
		if r, ok := s.blockRects[s.doc.Blocks[menu.BlockIdx].ID]; ok {
			trigger = r
		} else {
			trigger = ui.Rect{X: 80, Y: 80, W: 200, H: 28}
		}
	}

	minX, maxX, winW, winH := s.overlayBounds(c)
	spec := appUi.PlaceBelow(trigger).WithGap(4).WithMargin(8).WithReserveHeight(260)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 320)
	estH := min(float32(len(entries))*34+12, maxH)
	x, y := appUi.ResolveOrigin(spec, menuW, estH, winW, winH)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	ui.Overlay(c, func() {
		// Absorb wheel events inside the menu so boundary scrolling never leaks to the document body!
		shell := ui.Column(c.Key("block-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).
			Padding(4).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.18)).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputScroll {
					next := max(0, min(menu.Scroll.Y+ev.DY, menu.Scroll.MaxY))
					menu.Scroll.Y = next
					return true
				}
				return false
			})
		appUi.Probe(shell, "block-menu")

		shell.Children(func() {
			sc := ui.Scroll(c.Key("block-menu-scroll")).TrackScroll(&menu.Scroll).FillWidth().MaxHeight(maxH - 8)
			sc.Children(func() {
				list := ui.Column(c.Key("block-menu-list")).FillWidth().Gap(2)
				list.Children(func() {
					for i, item := range entries {
						if item.Separator {
							ui.Box(c.Key(fmt.Sprintf("bm-sep-%d", i))).FillWidth().Height(1).MarginY(3).Background(pal.Hairline)
							continue
						}
						cmd := item.Kind
						ic := cmdIcon(cmd)
						row := appUi.MenuRow(c, appUi.MenuItemSpec{
							ID:       fmt.Sprintf("block-menu-item-%d", i),
							Label:    item.Label,
							Shortcut: item.Hint,
							Icon:     &ic,
							Danger:   item.Danger,
							OnClick: func() {
								s.executeBlockCmd(menu.BlockIdx, menu.Mode, cmd)
							},
						}, i == menu.ActiveIndex)
						if i == menu.ActiveIndex && s.focusPending {
							row.ScrollIntoView()
						}
					}
				})
			})
		})

		if shell.PressedOutside() {
			s.blockMenu = nil
		}
	})
}

func cmdIcon(cmd blockCmdKind) icons.Icon {
	switch cmd {
	case cmdTurnParagraph:
		return icons.Text
	case cmdTurnH1:
		return icons.Heading1
	case cmdTurnH2:
		return icons.Heading2
	case cmdTurnH3:
		return icons.Heading3
	case cmdTurnBullet:
		return icons.ListBullet
	case cmdTurnNumbered:
		return icons.ListNumbered
	case cmdTurnTodo:
		return icons.Todo
	case cmdTurnQuote:
		return icons.Quote
	case cmdCallout:
		return icons.Callout
	case cmdCode:
		return icons.Code
	case cmdCodeMermaid:
		return icons.Diagram
	case cmdCodeMath:
		return icons.Math
	case cmdTable:
		return icons.Table
	case cmdDivider:
		return icons.Divider
	case cmdImage:
		return icons.Image
	case cmdAI:
		return icons.Sparkles
	case cmdDuplicate:
		return icons.Duplicate
	case cmdMoveUp:
		return icons.ArrowUp
	case cmdMoveDown:
		return icons.ArrowDown
	case cmdDelete:
		return icons.Trash
	default:
		return icons.Text
	}
}

// --- 4. Code Block Language Picker & Table Menu & Image Sub-Popovers ---

func (s *Session) renderCodeLangMenuOverlay(c *ui.Context) {
	if s.codeLangMenuBlock == 0 {
		return
	}
	idx := s.blockIndexByID(s.codeLangMenuBlock)
	if idx < 0 {
		s.codeLangMenuBlock = 0
		return
	}
	pal := theme.Ocean()
	b := &s.doc.Blocks[idx]
	curCanon := canonicalLangTag(b.Language)

	trigger := s.blockRects[b.ID]
	if trigger.W <= 0 {
		trigger = ui.Rect{X: 200, Y: 100, W: 300, H: 40}
	}
	menuW := float32(200)
	minX, maxX, winW, winH := s.overlayBounds(c)
	spec := appUi.PlaceBelow(trigger).WithAlignX(appUi.AlignX{Kind: appUi.AlignRight}).WithGap(4)
	maxH := appUi.MaxHeightForSpec(spec, winW, winH, 300)
	x, y := appUi.ResolveOrigin(spec, menuW, maxH, winW, winH)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	ui.Overlay(c, func() {
		shell := ui.Column(c.Key("code-lang-menu")).Absolute().Left(x).Top(y).Width(menuW).MaxHeight(maxH).
			Padding(4).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16)).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputScroll {
					s.codeLangScroll.Y = max(0, min(s.codeLangScroll.Y+ev.DY, s.codeLangScroll.MaxY))
					return true
				}
				return false
			})
		appUi.Probe(shell, "code-lang-menu")
		shell.Children(func() {
			ui.Scroll(c.Key("code-lang-scroll")).TrackScroll(&s.codeLangScroll).FillWidth().MaxHeight(maxH - 8).
				Children(func() {
					for i, lang := range codeLanguages {
						opt := lang
						appUi.MenuRow(c, appUi.MenuItemSpec{
							ID:      fmt.Sprintf("code-lang-item-%d", i),
							Label:   opt.Label,
							Checked: curCanon == opt.Tag,
							OnClick: func() {
								s.pushUndo()
								b.Language = opt.Tag
								b.Dirty = true
								b.CleanSource = ""
								s.codeLangMenuBlock = 0
								s.markDirty()
							},
						}, false)
					}
				})
		})
		if shell.PressedOutside() {
			s.codeLangMenuBlock = 0
		}
	})
}

func (s *Session) renderTableMenuOverlay(c *ui.Context) {
	if s.tableMenuBlock == 0 || s.tableMenuKind == "" {
		return
	}
	idx := s.blockIndexByID(s.tableMenuBlock)
	if idx < 0 {
		s.tableMenuBlock = 0
		s.tableMenuKind = ""
		return
	}
	pal := theme.Ocean()
	b := &s.doc.Blocks[idx]
	if b.Table == nil {
		return
	}
	id := b.ID
	r := s.tableActiveRow
	col := s.tableActiveCol
	trigger := s.blockRects[id]
	if trigger.W <= 0 {
		trigger = ui.Rect{X: 160, Y: 120, W: 300, H: 60}
	}
	menuW := float32(210)
	minX, maxX, winW, winH := s.overlayBounds(c)
	spec := appUi.PlaceBelow(trigger).WithAlignX(appUi.AlignX{Kind: appUi.AlignRight}).WithGap(4)
	x, y := appUi.ResolveOrigin(spec, menuW, 240, winW, winH)
	x = clampFloat32(x, minX, max(maxX-menuW, minX))

	ui.Overlay(c, func() {
		shell := ui.Column(c.Key("table-menu")).Absolute().Left(x).Top(y).Width(menuW).
			Padding(4).Gap(2).Radius(theme.RadiusCard).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
		appUi.Probe(shell, "table-menu")
		shell.Children(func() {
			rowInsAbove := icons.RowInsertAbove
			rowInsBelow := icons.RowInsertBelow
			rowDel := icons.RowDelete
			colInsLeft := icons.ColInsertLeft
			colInsRight := icons.ColInsertRight
			colDel := icons.ColDelete
			trash := icons.Trash

			entries := []appUi.MenuItemSpec{
				{ID: "tbl-row-above", Label: theme.Tr("上方插入行", "Insert row above"), Icon: &rowInsAbove, OnClick: func() {
					s.tableAddRow(id, r)
					s.tableMenuBlock = 0
				}},
				{ID: "tbl-row-below", Label: theme.Tr("下方插入行", "Insert row below"), Icon: &rowInsBelow, OnClick: func() {
					s.tableAddRow(id, r+1)
					s.tableMenuBlock = 0
				}},
				{ID: "tbl-row-del", Label: theme.Tr("删除当前行", "Delete row"), Icon: &rowDel, Disabled: len(b.Table.Rows) <= 1, OnClick: func() {
					s.tableDeleteRow(id, r)
					s.tableMenuBlock = 0
				}},
				{Separator: true},
				{ID: "tbl-col-left", Label: theme.Tr("左侧插入列", "Insert column left"), Icon: &colInsLeft, OnClick: func() {
					s.tableAddCol(id, col)
					s.tableMenuBlock = 0
				}},
				{ID: "tbl-col-right", Label: theme.Tr("右侧插入列", "Insert column right"), Icon: &colInsRight, OnClick: func() {
					s.tableAddCol(id, col+1)
					s.tableMenuBlock = 0
				}},
				{ID: "tbl-col-del", Label: theme.Tr("删除当前列", "Delete column"), Icon: &colDel, Disabled: len(b.Table.Rows[0]) <= 1, OnClick: func() {
					s.tableDeleteCol(id, col)
					s.tableMenuBlock = 0
				}},
				{Separator: true},
				{ID: "tbl-delete", Label: theme.Tr("删除表格", "Delete table"), Icon: &trash, Danger: true, OnClick: func() {
					s.tableMenuBlock = 0
					s.deleteBlock(idx)
				}},
			}
			for _, it := range entries {
				appUi.MenuRow(c, it, false)
			}
		})
		if shell.PressedOutside() {
			s.tableMenuBlock = 0
			s.tableMenuKind = ""
		}
	})
}

func (s *Session) renderImageSubPopover(c *ui.Context, idx int, b *model.Block) {
	pal := theme.Ocean()
	switch s.imagePopKind {
	case "size":
		pop := ui.Row(c.Key("img-size-pop")).Padding(6, 8).Gap(4).AlignItems(ui.Center).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.14))
		appUi.Probe(pop, "img-size-pop")
		pop.Children(func() {
			for _, pct := range []float32{25, 50, 75, 100} {
				p := pct
				lbl := fmt.Sprintf("%.0f%%", p)
				btn := ui.ButtonBase(c.Key("img-pct-"+lbl)).Height(28).PaddingX(8).Center().
					Radius(6).Cursor(ui.CursorPointer).Label(lbl)
				if int(b.ImageWidth) == int(p) || (b.ImageWidth == 0 && p == 100) {
					btn.Background(pal.Selected)
				} else if btn.Hovered() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					ui.Text(c, lbl).FontSize(14).TextColor(pal.Text)
				})
				if btn.Clicked() {
					s.pushUndo()
					b.ImageWidth = p
					b.Dirty = true
					s.imagePopKind = ""
					s.markDirty()
				}
			}
		})
	case "crop":
		pop := ui.Row(c.Key("image-crop-popover")).Padding(6, 8).Gap(4).AlignItems(ui.Center).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.Hairline).
			Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.14))
		appUi.Probe(pop, "image-crop-popover")
		pop.Children(func() {
			presets := []struct {
				label string
				crop  model.ImageCrop
			}{
				{"16:9", model.ImageCrop{Top: 0.1, Bottom: 0.1}},
				{"4:3", model.ImageCrop{Top: 0.06, Bottom: 0.06}},
				{"1:1", model.ImageCrop{Left: 0.15, Right: 0.15}},
				{theme.Tr("裁边 15%", "Trim 15%"), model.ImageCrop{Top: 0.15, Right: 0.15, Bottom: 0.15, Left: 0.15}},
				{theme.Tr("重置", "Reset"), model.ImageCrop{}},
			}
			for i, pr := range presets {
				item := pr
				btn := ui.ButtonBase(c.Key(fmt.Sprintf("img-crop-%d", i))).Height(28).PaddingX(8).Center().
					Radius(6).Cursor(ui.CursorPointer).Label(item.label)
				if btn.Hovered() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					ui.Text(c, item.label).FontSize(14).TextColor(pal.Text)
				})
				if btn.Clicked() {
					s.pushUndo()
					b.ImageCrop = item.crop
					b.Dirty = true
					s.imagePopKind = ""
					s.markDirty()
				}
			}
		})
	}
	_ = idx
}

// --- 5. Right-Side 4-Level Outline Rail (< 960px auto-hide) ---

func (s *Session) renderOutlineRail(c *ui.Context) {
	if s.outlineForceNarrow || s.viewportWidth < 960 {
		s.outlineHovered = false
		return
	}
	items := s.Outline()
	if len(items) == 0 {
		s.outlineHovered = false
		return
	}
	pal := theme.Ocean()

	// Determine active heading based on highlightedBlockID or focusedBlock.
	anchorIdx := s.focusedBlock
	if s.highlightedBlockID != 0 {
		if hi := s.blockIndexByID(s.highlightedBlockID); hi >= 0 {
			anchorIdx = hi
		}
	}
	activeItemIdx := 0
	for i, it := range items {
		if it.BlockIndex <= anchorIdx {
			activeItemIdx = i
		}
	}

	ui.Overlay(c, func() {
		topY := max(s.editorRect.Y+24, float32(36))
		wrap := ui.Column(c.Key("outline-rail-wrap")).Absolute().Right(18).Top(topY).AlignItems(ui.End)
		appUi.Probe(wrap, "outline-rail")
		hovered := wrap.Hovered()
		if s.outlinePinnedClosed {
			if !hovered {
				s.outlinePinnedClosed = false
			}
		} else if hovered != s.outlineHovered {
			s.outlineHovered = hovered
			c.Invalidate()
		}

		wrap.Children(func() {
			if s.outlineHovered {
				card := ui.Column(c.Key("outline-popover")).Width(236).MaxHeight(360).
					Padding(6).Gap(2).Radius(theme.RadiusSurface).Background(pal.Menu).
					Border(1, pal.Hairline).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16))
				appUi.Probe(card, "outline-popover")

				card.Children(func() {
					ui.Scroll(c.Key("outline-scroll")).FillWidth().MaxHeight(348).Children(func() {
						for idx, it := range items {
							item := it
							isActive := idx == activeItemIdx
							indent := float32(8 + (item.Level-1)*10)
							row := ui.ButtonBase(c.Key(fmt.Sprintf("outline-item-%d", idx))).
								FillWidth().Height(30).Padding(0, 8, 0, indent).Gap(8).
								AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).Label(item.Title)
							if isActive {
								row.Background(pal.Selected).Border(1, pal.Accent)
							} else {
								row.Border(1, ui.Transparent)
								if row.Hovered() {
									row.Background(pal.Hover)
								}
							}
							row.Children(func() {
								barW := outlineBarWidth(item.Level)
								barCol := pal.BtnBorder
								if isActive {
									barCol = pal.Accent
								}
								barBox := ui.Box(c).Width(barW).Height(2).Radius(1).Background(barCol).Shrink(0)
								appUi.Probe(barBox, fmt.Sprintf("outline-bar-%d", idx))
								ui.Text(c, item.Title).FontSize(14).TextColor(pal.Text).Grow(1).SingleLine()
							})
							appUi.Probe(row, fmt.Sprintf("outline-item-%d", idx))
							if row.Clicked() {
								s.JumpToBlock(item.BlockIndex)
							}
						}
					})
				})
			} else {
				rail := ui.Column(c.Key("outline-rail")).Padding(4, 4, 4, 8).Gap(7).AlignItems(ui.End)
				rail.Children(func() {
					for idx, it := range items {
						item := it
						isActive := idx == activeItemIdx
						barBtn := ui.ButtonBase(c.Key(fmt.Sprintf("outline-bar-%d", idx))).
							Height(8).PaddingX(4).AlignItems(ui.Center).Justify(ui.End).
							Cursor(ui.CursorPointer).Label(item.Title)
						barBtn.Children(func() {
							barCol := pal.BtnBorder
							if isActive {
								barCol = pal.Text
							}
							ui.Box(c).Width(outlineBarWidth(item.Level)).Height(2).Radius(1).Background(barCol)
						})
						appUi.Probe(barBtn, fmt.Sprintf("outline-bar-%d", idx))
						if barBtn.Clicked() {
							s.JumpToBlock(item.BlockIndex)
						}
					}
				})
			}
		})
	})
}

func outlineBarWidth(level int) float32 {
	switch level {
	case 1:
		return 20
	case 2:
		return 14
	case 3:
		return 10
	default:
		return 7
	}
}

// --- 6. Annotation Rail ---

func (s *Session) renderAnnotationRail(c *ui.Context) {
	if len(s.annotations) == 0 {
		return
	}
	pal := theme.Ocean()
	// Position rail to the right of the 672px body (avoid colliding with right-side outline rail).
	railRight := float32(64)
	if s.viewportWidth < 1120 {
		railRight = 16
	}
	topOffset := float32(64)
	if len(s.Outline()) > 0 && s.viewportWidth >= 960 && !s.outlineForceNarrow {
		topOffset = 140
	}

	ui.Overlay(c, func() {
		rail := ui.Column(c.Key("annotation-rail")).Absolute().Right(railRight).Top(topOffset).Width(224).Gap(8)
		appUi.Probe(rail, "annotation-rail")
		rail.Children(func() {
			for _, a := range s.annotations {
				ann := a
				isActive := s.activeAnnotID == ann.ID
				card := ui.Column(c.Key("annotation-card-"+ann.ID)).FillWidth().Padding(10).Gap(6).
					Radius(theme.RadiusCard).Background(pal.Menu).
					Border(1, boolColor(isActive, pal.Accent, pal.Hairline)).
					Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08))
				appUi.Probe(card, "annotation-card-"+ann.ID)

				card.Children(func() {
					if ann.QuotedText != "" {
						ui.Text(c, ann.QuotedText).FontSize(14).TextColor(pal.TextSecondary).MaxLines(2)
					}
					if s.editingAnnotID == ann.ID {
						in := ui.TextInputBase(c.Key("edit-ann-"+ann.ID), &s.editingAnnotText).FillWidth().
							Height(28).PaddingX(6).Radius(6).Border(1, pal.Accent).
							FontSize(14).TextColor(pal.Text).Bind(&s.editingHandle)
						if in.Submitted() {
							s.updateAnnotationNote(ann.ID, s.editingAnnotText)
						}
						ui.Row(c).FillWidth().Justify(ui.End).Gap(4).Children(func() {
							cancel := appUi.ActionButton(c, "ann-edit-cancel-"+ann.ID, theme.Tr("取消", "Cancel"), nil, appUi.ButtonGhost, false)
							if cancel.Clicked() {
								s.editingAnnotID = ""
							}
							save := appUi.ActionButton(c, "ann-edit-save-"+ann.ID, theme.Tr("保存", "Save"), nil, appUi.ButtonPrimary, false)
							if save.Clicked() {
								s.updateAnnotationNote(ann.ID, s.editingAnnotText)
							}
						})
					} else if ann.Note != "" {
						ui.Text(c, ann.Note).FontSize(14).TextColor(pal.Text)
					}

					if ann.RewriteResult != "" {
						ui.Box(c).FillWidth().Padding(6, 8).Radius(6).Background(pal.SuccessSurface).Children(func() {
							ui.Text(c, ann.RewriteResult).FontSize(14).TextColor(pal.Text)
						})
					}

					if len(ann.Reactions) > 0 {
						ui.Row(c).FillWidth().Wrap().Gap(4).Children(func() {
							for rIdx, em := range ann.Reactions {
								emoji := em
								pill := ui.ButtonBase(c.Key(fmt.Sprintf("rx-%s-%d", ann.ID, rIdx))).
									Height(24).PaddingX(8).Gap(4).Radius(theme.RadiusPill).
									Background(pal.Selected).AlignItems(ui.Center).Cursor(ui.CursorPointer).
									Label(emoji)
								pill.Children(func() {
									ui.Text(c, emoji).FontSize(14)
									ui.Text(c, "1").FontSize(14).TextColor(pal.Accent)
								})
								if pill.Clicked() {
									s.AddReaction(emoji)
								}
							}
						})
					}

					// Actions row
					if !s.readOnly {
						ui.Row(c).FillWidth().Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(4).Children(func() {
							if ann.RewriteResult != "" {
								dismiss := appUi.ActionButton(c, "ann-dismiss-"+ann.ID, theme.Tr("忽略", "Dismiss"), nil, appUi.ButtonGhost, false)
								if dismiss.Clicked() {
									ann.RewriteResult = ""
								}
								acc := appUi.ActionButton(c, "ann-accept-"+ann.ID, theme.Tr("应用", "Apply"), nil, appUi.ButtonPrimary, false)
								if acc.Clicked() {
									s.ApplyAnnotationRewrite(ann.ID, ann.RewriteResult)
								}
							} else if ann.RewritePrompt != "" {
								acc := appUi.ActionButton(c, "annotation-accept-"+ann.ID, theme.Tr("应用", "Apply"), nil, appUi.ButtonPrimary, false)
								if acc.Clicked() {
									s.ApplyAnnotationRewrite(ann.ID, ann.RewritePrompt)
								}
							}
							editBtn := appUi.IconButton(c, icons.Pencil, appUi.IconButtonOptions{
								ID: "annotation-edit-" + ann.ID, Label: theme.Tr("编辑批注", "Edit annotation"), Size: 24, IconSize: 14,
							})
							if editBtn.Clicked() {
								s.editingAnnotID = ann.ID
								s.editingAnnotText = ann.Note
							}
							delBtn := appUi.IconButton(c, icons.Trash, appUi.IconButtonOptions{
								ID: "annotation-delete-" + ann.ID, Label: theme.Tr("删除批注", "Delete annotation"), Size: 24, IconSize: 14, Danger: true,
							})
							if delBtn.Clicked() {
								s.deleteAnnotationByID(ann.ID)
							}
						})
					}
				})
				if card.Clicked() {
					s.activeAnnotID = ann.ID
				}
			}
		})
	})
}

// --- 7. AI BatchPlan Diff Review Card ---

func (s *Session) renderPlanDiffCard(c *ui.Context) {
	if s.activePlan == nil {
		return
	}
	plan := s.activePlan
	pal := theme.Ocean()
	cardW := min(max(s.viewportWidth-32, 320), 660)
	maxH := min(max(s.viewportHeight-48, 220), 460)

	ui.Overlay(c, func() {
		card := ui.Column(c.Key("plan-diff-card")).Absolute().Bottom(16).Left((s.viewportWidth-cardW)*0.5).
			Width(cardW).MaxHeight(maxH).Padding(12).Gap(10).
			Radius(theme.RadiusSurface).Background(pal.Paper).Border(1, pal.Accent).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.22))
		appUi.Probe(card, "plan-diff-card")

		card.Children(func() {
			// Header
			ui.Row(c.Key("plan-hdr")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				title := plan.Summary
				if title == "" {
					title = theme.Tr("AI 修改审阅", "AI Plan Diff Review")
				}
				ui.Row(c).AlignItems(ui.Center).Gap(6).Grow(1).Children(func() {
					icons.Draw(c, icons.Sparkles, 16, pal.MonoIconColors(pal.Accent))
					ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(pal.Text).SingleLine()
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					rej := appUi.ActionButton(c, "plan-reject", theme.Tr("拒绝", "Reject"), nil, appUi.ButtonDanger, false)
					if rej.Clicked() {
						s.rejectActivePlan()
					}
					acc := appUi.ActionButton(c, "plan-accept", theme.Tr("接受选中修改", "Accept Selected"), nil, appUi.ButtonPrimary, false)
					if acc.Clicked() {
						s.acceptActivePlan()
					}
				})
			})

			// Diff body with per-line checkboxes
			ui.Scroll(c.Key("plan-diff-scroll")).TrackScroll(&s.planDiffScroll).FillWidth().Grow(1).
				MaxHeight(maxH - 72).Children(func() {
				ui.Column(c.Key("plan-ops-col")).FillWidth().Gap(8).Children(func() {
					for opIdx := range plan.Ops {
						op := &plan.Ops[opIdx]
						diff := s.ensurePlanOpLineSelection(op)
						ui.Column(c.Key(fmt.Sprintf("plan-op-%d", opIdx))).FillWidth().
							Radius(6).Border(1, pal.Hairline).Clip().Children(func() {
							ui.Row(c.Key("op-title")).FillWidth().Padding(6, 10).Background(pal.Shell).Children(func() {
								ui.Text(c, op.Page).FontSize(14).FontWeight(600).TextColor(pal.Text).SingleLine()
							})
							selIdx := 0
							for lineIdx, dl := range diff {
								switch dl.Kind {
								case model.DiffEqual:
									ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().Padding(3, 10).
										Background(pal.Paper).Children(func() {
										ui.Text(c, "  "+dl.Text).Font("monospace").FontSize(14).TextColor(pal.TextSecondary)
									})
								case model.DiffDelete, model.DiffInsert:
									curSelIdx := selIdx
									selIdx++
									checked := true
									if curSelIdx < len(op.LineSelected) {
										checked = op.LineSelected[curSelIdx]
									}
									bg := pal.DiffAddBG
									fg := pal.DiffAddFG
									prefix := "+ "
									if dl.Kind == model.DiffDelete {
										bg = pal.DiffDelBG
										fg = pal.DiffDelFG
										prefix = "- "
									}
									row := ui.Row(c.Key(fmt.Sprintf("dl-%d-%d", opIdx, lineIdx))).FillWidth().
										Padding(4, 10).Gap(8).AlignItems(ui.Center).Background(bg)
									row.Children(func() {
										cbID := fmt.Sprintf("plan-diff-line-%d-%d", opIdx, curSelIdx)
										cb := ui.Checkbox(c.Key(cbID), &checked, "").Label(cbID)
										appUi.Probe(cb, cbID)
										if cb.Changed() && curSelIdx < len(op.LineSelected) {
											op.LineSelected[curSelIdx] = checked
										}
										ui.Text(c, prefix+dl.Text).Font("monospace").FontSize(14).TextColor(fg).Grow(1)
									})
								}
							}
						})
					}
				})
			})
		})
	})
}

func boolColor(cond bool, a, b ui.Color) ui.Color {
	if cond {
		return a
	}
	return b
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
