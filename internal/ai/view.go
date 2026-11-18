package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// RenderPanel renders the complete AI assistant side/fullscreen panel.
func (c *Controller) RenderPanel(cUI *ui.Context) {
	c.mu.Lock()
	c.services = cUI.Services()
	c.mu.Unlock()

	c.refreshPickerIfNeeded()

	pal := theme.Ocean()
	c.mu.RLock()
	fullscreen := c.fullscreen
	panelW := c.panelWidth
	if panelW <= 0 {
		panelW = defaultPanelWidth
	}
	if !fullscreen && c.renderWidth > 0 {
		panelW = c.renderWidth
	}
	provCfg := c.providerCfg
	selectedBackend := c.selectedBackendLocked()
	c.mu.RUnlock()

	blocked := !provCfg.Enabled || (selectedBackend == model.BackendBuiltin && !builtinReady(provCfg))

	panel := ui.Column(cUI.Key("ai-chat-panel")).FillHeight().Background(pal.Paper).TextColor(pal.Text)
	if fullscreen {
		panel.FillWidth().Grow(1)
	} else {
		panel.Width(panelW).Shrink(0)
	}
	if b := panel.Bounds(); b.W > 0 {
		c.mu.Lock()
		c.panelBox = b
		c.mu.Unlock()
	}
	bui.Probe(panel, "ai-chat-panel")

	c.mu.RLock()
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	anyOverlayOpen := c.historyOpen || c.deckOpen
	c.mu.RUnlock()
	if !anyOverlayOpen && pickerOpen && panel.Shortcut(0, ui.KeyEscape) {
		c.mu.Lock()
		c.pickerDismissed = true
		c.mu.Unlock()
	}

	panel.Children(func() {
		c.renderPanelHeader(cUI, pal, fullscreen, blocked, panelW)

		if blocked {
			c.renderBlockedCover(cUI, pal, fullscreen, provCfg.Enabled)
			return
		}

		c.renderMessageStream(cUI, pal, fullscreen)
		c.renderTurnError(cUI, pal, fullscreen)
		c.renderDeferredDraftBanner(cUI, pal, fullscreen)
		c.renderPicker(cUI, pal, fullscreen)
		c.renderComposerArea(cUI, pal, fullscreen, panelW)
	})
}

func (c *Controller) renderPanelHeader(cUI *ui.Context, pal theme.Palette, fullscreen bool, blocked bool, panelW float32) {
	c.mu.RLock()
	title := theme.Tr("新会话", "New Chat")
	if conv := c.findConvLocked(c.currentID); conv != nil && strings.TrimSpace(conv.Title) != "" {
		title = conv.Title
	}
	histOpen := c.historyOpen
	c.mu.RUnlock()

	header := ui.Row(cUI.Key("ai-header")).FillWidth().Height(52).Shrink(0).PaddingX(16).Gap(6).
		AlignItems(ui.Center).Background(pal.Paper).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)

	var histAnchor ui.Element
	header.Children(func() {
		ui.Text(cUI, title).FontSize(15).FontWeight(600).TextColor(pal.Text).Grow(1).SingleLine()

		if !blocked {
			newBtn := bui.IconButton(cUI, icons.AiPlus, bui.IconButtonOptions{
				ID:      "ai-new-chat",
				Label:   theme.Tr("新建会话", "New chat"),
				Tooltip: theme.Tr("新建会话", "New chat"),
			})
			if newBtn.Clicked() {
				c.NewConversation()
			}
		}

		fsIcon := icons.WindowMax
		fsTip := theme.Tr("全屏", "Full screen")
		if fullscreen {
			fsIcon = icons.WindowRestore
			fsTip = theme.Tr("切回侧栏", "Switch to sidebar")
		}
		fsBtn := bui.IconButton(cUI, fsIcon, bui.IconButtonOptions{
			ID:      "ai-fullscreen-toggle",
			Label:   fsTip,
			Tooltip: fsTip,
			Active:  fullscreen,
		})
		if fsBtn.Clicked() {
			c.ToggleFullscreen()
		}

		if !blocked {
			histBtn := bui.IconButton(cUI, icons.History, bui.IconButtonOptions{
				ID:      "ai-history-toggle",
				Label:   theme.Tr("历史会话", "Chat history"),
				Tooltip: theme.Tr("历史会话", "Chat history"),
				Active:  histOpen,
			})
			histAnchor = histBtn
			if b := histBtn.Bounds(); b.W > 0 {
				c.mu.Lock()
				c.histTriggerBox = b
				c.mu.Unlock()
			}
			if histBtn.Clicked() {
				c.mu.Lock()
				c.historyOpen = !c.historyOpen
				if c.historyOpen {
					c.deckOpen = false
					c.pickerDismissed = true
				}
				c.mu.Unlock()
			}
		}

		closeBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
			ID:      "ai-chat-close",
			Label:   theme.Tr("关闭 AI 面板", "Close AI panel"),
			Tooltip: theme.Tr("关闭", "Close"),
		})
		if closeBtn.Clicked() {
			c.SetOpen(false)
		}
	})

	if !blocked && histAnchor.Valid() {
		c.renderHistoryPopover(cUI, pal, histAnchor, fullscreen, panelW)
	}
}

func (c *Controller) renderHistoryPopover(cUI *ui.Context, pal theme.Palette, anchor ui.Element, fullscreen bool, panelW float32) {
	c.mu.RLock()
	open := c.historyOpen
	trigger := c.histTriggerBox
	panelBox := c.panelBox
	convs := make([]model.Conversation, 0, len(c.convs))
	for _, conv := range c.convs {
		if len(conv.Messages) > 0 {
			convs = append(convs, conv)
		}
	}
	curID := c.currentID
	c.mu.RUnlock()

	if !open {
		return
	}

	winW, winH := cUI.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}
	if panelBox.W <= 0 {
		px := max(winW-panelW, 0)
		if fullscreen {
			px = 0
			panelW = winW
		}
		panelBox = ui.Rect{X: px, Y: 0, W: panelW, H: winH}
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: max(panelBox.X+panelBox.W-42, 8), Y: panelBox.Y + 13, W: 26, H: 26}
	}

	menuRoom := panelW
	if fullscreen {
		menuRoom = winW
	}
	menuW := min(max(menuRoom-32, 220), 320)
	menuW = min(menuW, max(winW-16, 1))
	capH := float32(304)
	if fullscreen {
		capH = 420
	}
	historyMaxH := min(max(winH-140, 160), capH)
	spec := bui.PlaceBelow(trigger).WithAlignX(bui.AlignX{Kind: bui.AlignRight}).WithGap(6).WithMargin(8)
	maxH := bui.MaxHeightForSpec(spec, winW, winH, historyMaxH)
	estH := float32(44)
	if len(convs) > 0 {
		estH = min(float32(len(convs))*32+float32(max(len(convs)-1, 0))*2+theme.MenuPadding*2+2, maxH)
	}
	x, y := bui.ResolveOrigin(spec, menuW, estH, winW, winH)

	scrimAlpha := float32(0.08)
	if fullscreen {
		if theme.IsDark() {
			scrimAlpha = 0.46
		} else {
			scrimAlpha = 0.28
		}
	} else if theme.IsDark() {
		scrimAlpha = 0.28
	}

	ui.Overlay(cUI, func() {
		scrim := ui.Box(cUI.Key("ai-history-scrim")).Absolute().
			Left(panelBox.X).Top(panelBox.Y + 52).Width(panelBox.W).Height(max(panelBox.H-52, 0)).
			Background(ui.RGBA(0, 0, 0, scrimAlpha)).
			Transition(ui.ElementTransition{
				Duration: time.Duration(theme.MotionMs) * time.Millisecond,
				Ease:     ui.EaseOut,
				Enter:    &ui.Motion{Opacity: 0.01},
				Exit:     &ui.Motion{},
			})
		if scrim.Clicked() {
			c.mu.Lock()
			c.historyOpen = false
			c.mu.Unlock()
		}

		panel := ui.Column(cUI.Key("ai-history-popover")).Absolute().Left(x).Top(y).
			Width(menuW).MaxHeight(maxH).Padding(theme.MenuPadding).Gap(2).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16)).Clip().
			Transition(bui.PopupTransition())
		bui.Probe(panel, "ai-history-list")

		panel.Children(func() {
			if len(convs) == 0 {
				ui.Text(cUI, theme.Tr("暂无历史会话", "No chat history")).
					FontSize(14).TextColor(pal.TextSecondary).Padding(12)
				return
			}

			ui.Scroll(cUI.Key("ai-history-scroll")).TrackScroll(&c.historyScroll).
				FillWidth().MaxHeight(max(maxH-theme.MenuPadding*2, 48)).Children(func() {
				ui.Column(cUI).FillWidth().Gap(2).Children(func() {
					for _, conv := range convs {
						convID := conv.ID
						active := convID == curID
						rowID := fmt.Sprintf("conv-%d", convID)
						row := ui.Row(cUI.Key(rowID)).FillWidth().Height(32).PaddingX(8).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusControl).Border(1, ui.Transparent)
						if active {
							row.Background(pal.Selected).Border(1, pal.Accent)
						} else if row.Hovered() {
							row.Background(pal.Hover)
						}
						bui.Probe(row, rowID)
						row.Children(func() {
							selBtn := ui.ButtonBase(cUI.Key("sel-" + rowID)).Grow(1).MinWidth(0).FillHeight().
								AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(conv.Title)
							selBtn.Children(func() {
								ui.Text(cUI, conv.Title).FontSize(14).TextColor(pal.Text).SingleLine().Ellipsis("…")
							})
							if selBtn.Clicked() {
								c.SelectConversation(convID)
							}

							ui.Text(cUI, backendLabel(conv.Backend)).FontSize(13).TextColor(pal.TextSecondary).SingleLine().Shrink(0)

							delID := fmt.Sprintf("conv-del-%d", convID)
							delBtn := bui.IconButton(cUI, icons.Trash, bui.IconButtonOptions{
								ID:       delID,
								Label:    theme.Tr("删除会话 ", "Delete ") + conv.Title,
								Size:     22,
								IconSize: 14,
								Danger:   true,
							})
							if delBtn.Clicked() {
								c.deleteConversation(convID)
							}
						})
					}
				})
			})
		})

		if panel.OverlayShortcut(0, ui.KeyEscape) || (!anchor.PressedInside() && panel.PressedOutside()) {
			c.mu.Lock()
			c.historyOpen = false
			c.mu.Unlock()
		}
	})
}

func (c *Controller) deleteConversation(id uint64) {
	c.mu.Lock()
	if c.currentID == id {
		c.stopStreamingLocked()
		c.clearActivePlanLocked()
		c.currentID = 0
		c.draftText = ""
		c.draftRefs = nil
		c.draftSkills = nil
		c.composerEpoch++
	}
	delete(c.tabDrafts, id)
	filtered := c.convs[:0]
	for _, conv := range c.convs {
		if conv.ID != id {
			filtered = append(filtered, conv)
		}
	}
	c.convs = filtered
	c.persistChatsLocked()
	c.persistActiveDraftLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) renderBlockedCover(cUI *ui.Context, pal theme.Palette, fullscreen bool, enabled bool) {
	maxW := float32(128)
	if fullscreen {
		maxW = 160
	}
	title := theme.Tr("先完成 AI 配置", "Configure AI Assistant")
	cta := theme.Tr("前往设置", "Open AI Settings")
	mascot := icons.MascotAIGreeting
	if !enabled {
		title = theme.Tr("先启用 AI 助手", "Enable AI Assistant")
		cta = theme.Tr("前往设置", "Open AI Settings")
		mascot = icons.MascotAIDisabled
	}

	ui.Column(cUI.Key("ai-empty-cover")).Fill().Padding(24).Gap(18).Center().Children(func() {
		icons.Mascot(cUI, mascot, maxW)
		ui.Text(cUI, title).FontSize(18).FontWeight(600).TextColor(pal.Text).TextAlign(ui.Center)
		lead := icons.ArrowRight
		btn := bui.ActionButton(cUI, "ai-cover-settings", cta, &lead, bui.ButtonPrimary, false)
		if btn.Clicked() {
			if c.host != nil {
				c.host.OpenSettings("ai")
			}
		}
	})
}

func (c *Controller) renderEmptySuggestions(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	maxW := float32(128)
	if fullscreen {
		maxW = 160
	}
	prompts := []struct {
		id     string
		label  string
		prompt string
	}{
		{"ai-empty-polish", theme.Tr("润色并精简当前笔记", "Polish and condense note"), "请帮我润色并精简当前笔记的结构与表述"},
		{"ai-empty-table", theme.Tr("整理内容为对齐表格", "Format content as table"), "请将当前笔记中的结构化条目整理为对齐的 Markdown 表格"},
		{"ai-empty-summary", theme.Tr("提炼核心要点与待办", "Extract key points and todos"), "请提炼当前笔记的核心要点，并整理出可执行的待办事项"},
		{"ai-empty-proofread", theme.Tr("检查错别字与语病", "Check typos and phrasing"), "请检查当前笔记中的错别字、标点与语病并给出修改建议"},
	}

	ui.Column(cUI.Key("ai-empty-state")).FillWidth().Padding(32, 16).Gap(14).AlignItems(ui.Center).Children(func() {
		icons.Mascot(cUI, icons.MascotAIGreeting, maxW)
		ui.Text(cUI, theme.Tr("有什么可以帮你处理？", "How can I help with this note?")).
			FontSize(16).FontWeight(600).TextColor(pal.Text).TextAlign(ui.Center)
		ui.Column(cUI).FillWidth().MaxWidth(540).Gap(8).AlignItems(ui.Center).Children(func() {
			for _, p := range prompts {
				btn := bui.ActionButton(cUI, p.id, p.label, nil, bui.ButtonSecondary, false)
				if btn.Clicked() {
					c.mu.Lock()
					c.draftText = p.prompt
					c.composerSelectEnd = true
					c.composerHandle.Focus()
					c.persistActiveDraftLocked()
					c.mu.Unlock()
				}
			}
		})
	})
}

func (c *Controller) renderMessageStream(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.RLock()
	var conv model.Conversation
	hasConv := false
	if found := c.findConvLocked(c.currentID); found != nil {
		conv = cloneConversation(*found)
		hasConv = true
	}
	streaming := c.streaming
	partial := c.partialText
	partialSteps := append([]model.ProcessStep(nil), c.partialSteps...)
	c.mu.RUnlock()

	scroll := ui.Scroll(cUI.Key("ai-chat-scroll")).FillWidth().Grow(1)
	bui.Probe(scroll, "ai-chat-scroll")

	scroll.Children(func() {
		outer := ui.Column(cUI).FillWidth().Padding(16).Gap(16).Selectable()
		if fullscreen {
			outer.MaxWidth(720).Margin(0, ui.Auto)
		}
		outer.Children(func() {
			if !hasConv || (len(conv.Messages) == 0 && !streaming) {
				c.renderEmptySuggestions(cUI, pal, fullscreen)
				return
			}

			replyIdx := 0
			msgs := conv.Messages
			for start := 0; start < len(msgs); {
				end := start + 1
				for end < len(msgs) && msgs[end].Role != "user" {
					end++
				}
				turn := msgs[start:end]
				turnStreaming := streaming && end == len(msgs)

				if turn[0].Role == "user" {
					c.renderUserMessage(cUI, pal, conv.ID, start, turn[0].Content)
				}

				turnKey := fmt.Sprintf("%d-%d", conv.ID, start)
				steps, notes := collectTurnProcess(turn, turnStreaming, partial, partialSteps)
				if len(steps) > 0 {
					c.renderProcessCard(cUI, pal, turnKey, turn, steps, notes, turnStreaming)
				}

				for offset, m := range turn {
					if m.Plan != nil {
						c.renderPlanCard(cUI, pal, conv.ID, start+offset, m.Plan)
					}
					if m.Question != nil {
						c.renderQuestionCard(cUI, pal, conv.ID, start+offset, m.Question)
					}
					if m.Approval != nil {
						c.renderApprovalCard(cUI, pal, conv.ID, start+offset, m.Approval)
					}
				}

				hasPendingPlan := false
				for _, m := range turn {
					if m.Plan != nil && m.Plan.Status == "pending" {
						hasPendingPlan = true
						break
					}
				}
				if !hasPendingPlan {
					for _, m := range turn {
						if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
							c.renderAssistantReply(cUI, pal, conv.ID, replyIdx, m.Content, !turnStreaming, false)
							replyIdx++
						}
					}
					if turnStreaming && strings.TrimSpace(partial) != "" {
						c.renderAssistantReply(cUI, pal, conv.ID, replyIdx, partial, false, true)
						replyIdx++
					}
				}

				sources := collectTurnSources(turn)
				if len(sources) > 0 {
					c.renderSourcesCard(cUI, pal, turnKey, sources)
				}

				start = end
			}
		})
	})
}

func (c *Controller) renderUserMessage(cUI *ui.Context, pal theme.Palette, convID uint64, idx int, text string) {
	ui.Row(cUI.Key(fmt.Sprintf("user-row-%d-%d", convID, idx))).FillWidth().Justify(ui.End).Children(func() {
		bubble := ui.Box(cUI).MaxWidth(320).Padding(8, 12).Radius(theme.RadiusSurface).Background(pal.Hover)
		bui.Probe(bubble, "ai-user-message")
		bubble.Children(func() {
			ui.Text(cUI, text).FontSize(14).LineHeight(1.5).TextColor(pal.Text)
		})
	})
}

func (c *Controller) renderAssistantReply(cUI *ui.Context, pal theme.Palette, convID uint64, replyIdx int, text string, copyable bool, isStreaming bool) {
	boxKey := fmt.Sprintf("reply-%d-%d", convID, replyIdx)
	wrap := ui.Column(cUI.Key(boxKey)).FillWidth().Gap(4)
	hoveredWrap := wrap.Hovered()
	mdOpts := c.markdownOptions(pal)

	wrap.Children(func() {
		msgBox := ui.Box(cUI).FillWidth()
		bui.Probe(msgBox, "ai-assistant-message")
		msgBox.Children(func() {
			if isStreaming {
				markdown.Stream(cUI, text, mdOpts)
			} else {
				markdown.View(cUI, text, mdOpts)
			}
		})
		if copyable {
			copyID := fmt.Sprintf("ai-msg-copy-%d", replyIdx)
			ui.Row(cUI).FillWidth().Justify(ui.End).Children(func() {
				copyBtn := bui.IconButton(cUI, icons.Copy, bui.IconButtonOptions{
					ID:       copyID,
					Label:    theme.Tr("复制回复", "Copy reply"),
					Tooltip:  theme.Tr("复制", "Copy"),
					Size:     26,
					IconSize: 14,
				})
				if !hoveredWrap && !copyBtn.Hovered() {
					copyBtn.Opacity(0.05)
				}
				if copyBtn.Clicked() {
					cUI.WriteClipboard(text)
				}
			})
		}
	})
}

func (c *Controller) markdownOptions(pal theme.Palette) *markdown.Options {
	c.mdOpts.ShareSelection = true
	c.mdOpts.Theme = &markdown.Theme{
		FontSize:            14,
		LineHeight:          1.55,
		Text:                pal.Text,
		Muted:               pal.TextSecondary,
		Link:                pal.Accent,
		Strong:              pal.Text,
		StrongWeight:        600,
		Code:                pal.Text,
		CodeBackground:      pal.CodeBG,
		CodeBlockBackground: pal.CodeBG,
		CodeBlockBorder:     pal.Hairline,
		Rule:                pal.Hairline,
		TableBorder:         pal.Hairline,
		TableHeader:         pal.Hover,
	}
	c.mdOpts.OnLink = func(url string) {
		if ui.SafeToOpen(url) {
			cUI := c.services
			cUI.OpenURL(url)
			return
		}
		if c.host != nil && url != "" && !strings.HasPrefix(url, "#") {
			_ = c.host.OpenNote(url, 0)
		}
	}
	return &c.mdOpts
}

func collectTurnProcess(turn []model.ChatMessage, streaming bool, partial string, partialSteps []model.ProcessStep) ([]model.ProcessStep, []string) {
	var steps []model.ProcessStep
	var notes []string
	seenNotes := make(map[string]bool)
	addNote := func(n string) {
		n = strings.TrimSpace(n)
		if n != "" && !seenNotes[n] {
			seenNotes[n] = true
			notes = append(notes, n)
		}
	}

	for _, m := range turn {
		for _, st := range m.Process {
			steps = append(steps, st)
			if st.FileRef != "" {
				addNote(st.FileRef)
			}
		}
		for _, s := range m.Sources {
			addNote(s.Title)
		}
	}
	if streaming {
		for _, st := range partialSteps {
			steps = append(steps, st)
			if st.FileRef != "" {
				addNote(st.FileRef)
			}
		}
		lbl := theme.Tr("思考中", "Thinking")
		det := theme.Tr("等待模型响应…", "Waiting for model response…")
		if strings.TrimSpace(partial) != "" {
			lbl = theme.Tr("正在生成回答", "Generating answer")
			det = firstTitleLine(partial)
		}
		steps = append(steps, model.ProcessStep{
			ID:     "streaming",
			Kind:   model.ToolKindThink,
			Title:  lbl,
			Detail: det,
			Done:   false,
		})
	}
	return steps, notes
}

func collectTurnSources(turn []model.ChatMessage) []model.SourceNote {
	var out []model.SourceNote
	seen := make(map[string]bool)
	for _, m := range turn {
		for _, s := range m.Sources {
			key := s.Path + "|" + s.Title
			if !seen[key] {
				seen[key] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func (c *Controller) renderProcessCard(cUI *ui.Context, pal theme.Palette, turnKey string, turn []model.ChatMessage, steps []model.ProcessStep, notes []string, streaming bool) {
	c.mu.RLock()
	expanded := c.expandedProcess[turnKey]
	c.mu.RUnlock()

	outcome := ""
	for i := len(turn) - 1; i >= 0; i-- {
		if turn[i].Role == "turn_outcome" {
			outcome = turn[i].Content
			break
		}
	}

	headline := fmt.Sprintf("%d 步执行完成", len(steps))
	if theme.IsEn() {
		headline = fmt.Sprintf("%d steps completed", len(steps))
	}
	if streaming {
		headline = theme.Tr("正在执行工具与思考…", "Running tools & thinking…")
	} else if outcome == "failed" {
		headline = theme.Tr("执行失败", "Execution failed")
	} else if outcome == "stopped" {
		headline = theme.Tr("已停止", "Stopped")
	}

	card := ui.Column(cUI.Key("ai-process-card-"+turnKey)).FillWidth().Padding(8, 10).Gap(6).
		Radius(theme.RadiusControl).Background(pal.Shell).Border(1, pal.Hairline)
	bui.Probe(card, "ai-process-card")

	card.Children(func() {
		toggleBtn := ui.ButtonBase(cUI.Key("ai-process-toggle-" + turnKey)).FillWidth().MinHeight(28).
			PaddingX(4).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(headline)
		bui.Probe(toggleBtn, "ai-process-toggle")

		toggleBtn.Children(func() {
			if streaming {
				ui.Spinner(cUI).Label(theme.Tr("执行中", "Running"))
			} else {
				chev := icons.ChevronRight
				if expanded {
					chev = icons.ChevronDown
				}
				icons.Draw(cUI, chev, 14, pal.HoverIconColors(toggleBtn.Hovered()))
			}
			ui.Text(cUI, headline).FontSize(14).FontWeight(500).TextColor(pal.TextSecondary).Grow(1).SingleLine()
		})
		if toggleBtn.Clicked() {
			c.mu.Lock()
			c.expandedProcess[turnKey] = !c.expandedProcess[turnKey]
			c.mu.Unlock()
		}

		if expanded {
			stepsWrap := ui.Scroll(cUI.Key("ai-process-steps-" + turnKey)).FillWidth().MaxHeight(180)
			bui.Probe(stepsWrap, "ai-process-steps")
			stepsWrap.Children(func() {
				ui.Column(cUI).FillWidth().Gap(4).Children(func() {
					for i, st := range steps {
						stepKey := fmt.Sprintf("%s#%d", turnKey, i)
						c.mu.RLock()
						detailOpen := c.expandedStepDetails[stepKey]
						c.mu.RUnlock()

						chipID := fmt.Sprintf("ai-process-tool-chip-%d", i)
						row := ui.ButtonBase(cUI.Key(chipID+"-"+turnKey)).FillWidth().MinHeight(28).PaddingX(6).Gap(8).
							AlignItems(ui.Center).Radius(6).Border(1, ui.Transparent).Cursor(ui.CursorPointer).Label(st.Title)
						if row.Hovered() {
							row.Background(pal.Hover)
						}
						bui.Probe(row, chipID)

						row.Children(func() {
							iconGlyph := icons.ToolRead
							switch st.Kind {
							case model.ToolKindThink:
								iconGlyph = icons.ToolThink
							case model.ToolKindWrite:
								iconGlyph = icons.ToolWrite
							case model.ToolKindRun:
								iconGlyph = icons.ToolRun
							}
							icons.Draw(cUI, iconGlyph, 14, pal.MonoIconColors(pal.TextSecondary))
							ui.Text(cUI, st.Title).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
							if st.Detail != "" {
								ui.Text(cUI, st.Detail).FontSize(14).TextColor(pal.TextSecondary).Grow(1).SingleLine()
							}
						})
						if row.Clicked() {
							c.mu.Lock()
							c.expandedStepDetails[stepKey] = !c.expandedStepDetails[stepKey]
							c.mu.Unlock()
						}

						if detailOpen {
							detID := fmt.Sprintf("ai-process-tool-detail-%d", i)
							detBox := ui.Column(cUI.Key(detID+"-"+turnKey)).FillWidth().Padding(4, 12).
								BorderWidth(0, 0, 0, 2).BorderColor(pal.Hairline)
							bui.Probe(detBox, detID)
							detBox.Children(func() {
								txt := st.Detail
								if txt == "" {
									txt = st.Title
								}
								ui.Text(cUI, "✓ "+txt).FontSize(14).TextColor(pal.TextSecondary)
							})
						}
					}
				})
			})

			if len(notes) > 0 {
				ui.Row(cUI).FillWidth().Padding(6, 4, 2, 4).Gap(6).Wrap().Children(func() {
					for i, n := range notes {
						if i >= 6 {
							break
						}
						chip := ui.Box(cUI).Padding(3, 8).Radius(6).Background(pal.Paper).Border(1, pal.BtnBorder)
						chip.Children(func() {
							ui.Text(cUI, n+".md").FontSize(14).TextColor(pal.Text).SingleLine()
						})
					}
				})
			}
		}
	})
}

func (c *Controller) renderSourcesCard(cUI *ui.Context, pal theme.Palette, turnKey string, sources []model.SourceNote) {
	c.mu.RLock()
	expanded := c.expandedSources[turnKey]
	c.mu.RUnlock()

	card := ui.Column(cUI.Key("ai-sources-" + turnKey)).FillWidth().Gap(6)
	bui.Probe(card, "ai-context-card")
	card.Children(func() {
		toggle := ui.ButtonBase(cUI.Key("ai-sources-toggle-" + turnKey)).Height(28).PaddingX(6).Gap(8).
			AlignItems(ui.Center).Radius(6).Cursor(ui.CursorPointer).
			Label(theme.Tr("参考笔记", "Sources"))
		bui.Probe(toggle, "ai-sources-toggle")
		toggle.Children(func() {
			ui.Text(cUI, theme.Tr("参考笔记", "Sources")).FontSize(14).FontWeight(600).TextColor(pal.Text)
			ui.Box(cUI).Padding(2, 6).Radius(5).Background(pal.Hover).Children(func() {
				ui.Textf(cUI, "%d", len(sources)).FontSize(14).TextColor(pal.TextSecondary)
			})
		})
		if toggle.Clicked() {
			c.mu.Lock()
			c.expandedSources[turnKey] = !c.expandedSources[turnKey]
			c.mu.Unlock()
		}

		if expanded {
			for i, src := range sources {
				srcID := fmt.Sprintf("ai-source-%s-%d", turnKey, i)
				btn := ui.ButtonBase(cUI.Key(srcID)).FillWidth().Padding(8, 10).Gap(8).
					AlignItems(ui.Center).Radius(theme.RadiusControl).
					Background(pal.Shell).Border(1, pal.BtnBorder).Cursor(ui.CursorPointer).Label(src.Title)
				bui.Probe(btn, srcID)
				if btn.Hovered() {
					btn.Background(pal.Hover)
				}
				btn.Children(func() {
					icons.Draw(cUI, icons.Note, theme.IconRow, pal.DuoIconColors(pal.Text))
					ui.Text(cUI, src.Title+".md").FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).SingleLine()
					icons.Draw(cUI, icons.ChevronRight, theme.IconInline, pal.HoverIconColors(btn.Hovered()))
				})
				if btn.Clicked() && c.host != nil {
					_ = c.host.OpenNote(src.Path, 0)
				}
			}
		}
	})
}

func isDiffTableRowLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2
}

func isDiffTableScaffolding(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	inner := strings.Trim(trimmed, "|")
	if strings.TrimSpace(inner) == "" {
		return false
	}
	for _, cell := range strings.Split(inner, "|") {
		t := strings.TrimSpace(cell)
		t = strings.TrimPrefix(t, ":")
		t = strings.TrimSuffix(t, ":")
		if t == "" || strings.Trim(t, "-") != "" {
			return false
		}
	}
	return true
}

func parseDiffTableCells(trimmed string) []string {
	s := strings.TrimSpace(trimmed)
	if !strings.HasPrefix(s, "|") {
		return nil
	}
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func (c *Controller) renderPlanCard(cUI *ui.Context, pal theme.Palette, convID uint64, msgIdx int, plan *model.BatchPlan) {
	cardID := fmt.Sprintf("plan-op-%d-0", msgIdx)
	card := ui.Column(cUI.Key(cardID)).FillWidth().Radius(12).Clip().
		Background(pal.Paper).Border(1, pal.Hairline)
	bui.Probe(card, "ai-plan-card")

	pending := plan.Status == "pending"
	title := plan.Summary
	if len(plan.Ops) > 0 {
		title = plan.Ops[0].Page + ".md"
	}
	isCreate := len(plan.Ops) > 0 && plan.Ops[0].Kind == model.PlanOpCreateNote

	badge := theme.Tr("待审阅", "Pending review")
	badgeCol := pal.Accent
	switch plan.Status {
	case "applied":
		badge = theme.Tr("已应用", "Applied")
		badgeCol = pal.Success
	case "rejected":
		badge = theme.Tr("已拒绝", "Rejected")
		badgeCol = pal.TextSecondary
	case "undone":
		badge = theme.Tr("已撤回", "Undone")
		badgeCol = pal.TextSecondary
	case "undo_conflict":
		badge = theme.Tr("撤回冲突", "Undo conflict")
		badgeCol = pal.Danger
	}

	card.Children(func() {
		topBtn := ui.ButtonBase(cUI.Key("plan-open-"+cardID)).FillWidth().Padding(10, 14).Gap(6).
			Radius(11, 11, 0, 0).Background(pal.Shell).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Column().AlignItems(ui.Start).Cursor(ui.CursorPointer).Label(title)
		bui.Probe(topBtn, cardID)
		topBtn.Children(func() {
			ui.Row(cUI).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				icons.Draw(cUI, icons.Sparkles, 16, pal.MonoIconColors(pal.Accent))
				ui.Text(cUI, title).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).SingleLine()
				if isCreate && pending {
					ui.Box(cUI).Padding(2, 7).Radius(5).Background(pal.Selected).Children(func() {
						ui.Text(cUI, theme.Tr("新建预览", "Create preview")).FontSize(theme.FontSmall).TextColor(pal.Accent)
					})
				}
			})
			if plan.Summary != "" {
				ui.Text(cUI, plan.Summary).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			}
		})
		if topBtn.Clicked() {
			c.mu.Lock()
			if pending {
				c.linkPlanLocked(convID, msgIdx)
			} else if plan.Status == "applied" && len(plan.Ops) > 0 && c.host != nil {
				abs := plan.Ops[0].Page + ".md"
				if plan.Root != "" {
					abs = filepath.Join(plan.Root, filepath.FromSlash(plan.Ops[0].Page)+".md")
				}
				_ = c.host.OpenNote(abs, 0)
			}
			c.mu.Unlock()
		}

		// Show inline diff preview when this plan is actively linked.
		c.mu.RLock()
		isLinked := c.activePlan != nil && c.activePlan.ConvID == convID && c.activePlan.MsgIndex == msgIdx
		var preview *ActivePlanPreview
		if isLinked {
			cp := *c.activePlan
			preview = &cp
		}
		c.mu.RUnlock()

		if preview != nil && pending {
			lines := c.PlanDiffLines(convID, msgIdx, preview.Page, preview.BeforeText, preview.AfterText)
			var visible []PlanDiffLine
			for _, dl := range lines {
				if isDiffTableScaffolding(strings.TrimSpace(dl.Text)) {
					continue
				}
				visible = append(visible, dl)
			}
			previewWrap := ui.Column(cUI.Key("ai-plan-preview-" + cardID)).FillWidth().Padding(14).
				Background(pal.Paper)
			if preview.IsCreate {
				bui.Probe(previewWrap, "ai-create-preview")
			} else {
				bui.Probe(previewWrap, "ai-editor-plan-card")
			}
			headerBG := ui.Hex("#f5f3ed")
			if theme.IsDark() {
				headerBG = ui.RGBA(255, 255, 255, 0.03)
			}
			previewWrap.Children(func() {
				listBox := ui.Column(cUI.Key("ai-plan-diff-list-"+cardID)).FillWidth().Radius(10).
					Border(1, pal.Hairline).Background(pal.Paper).Clip()
				listBox.Children(func() {
					totalVis := len(visible)
					for pos, dl := range visible {
						trimmed := strings.TrimSpace(dl.Text)
						isFirst := pos == 0
						isLast := pos+1 == totalVis
						isTable := isDiffTableRowLine(trimmed)
						nextIsTable := pos+1 < totalVis && isDiffTableRowLine(strings.TrimSpace(visible[pos+1].Text))
						isHeader := (dl.Index+1 < len(lines) && isDiffTableScaffolding(strings.TrimSpace(lines[dl.Index+1].Text))) ||
							strings.Contains(strings.ToLower(trimmed), "<th")

						rowBg := pal.Paper
						rowFg := pal.Text
						gutterSym := ""
						gutterFg := pal.TextSecondary
						switch {
						case !dl.Included:
							rowBg = pal.Paper
							rowFg = pal.TextSecondary
							if dl.Change < 0 {
								gutterSym = "−"
							} else if dl.Change > 0 {
								gutterSym = "+"
							}
						case dl.Change < 0:
							rowBg = pal.DiffDelBG
							rowFg = pal.DiffDelFG
							gutterSym = "−"
							gutterFg = pal.DiffDelFG
						case dl.Change > 0:
							rowBg = pal.DiffAddBG
							rowFg = pal.DiffAddFG
							gutterSym = "+"
							gutterFg = pal.DiffAddFG
						case isHeader:
							rowBg = headerBG
						}
						showBottomBorder := !isLast && (isTable || nextIsTable || visible[pos+1].Change != dl.Change)

						lineID := fmt.Sprintf("ai-plan-diff-line-%d", dl.Index)
						var tl, tr, br, bl float32
						if isFirst {
							tl, tr = 9, 9
						}
						if isLast {
							br, bl = 9, 9
						}
						if dl.Change != 0 {
							rowBtn := ui.ButtonBase(cUI.Key(lineID)).FillWidth().Padding(4, 10).Gap(8).
								AlignItems(ui.Center).Radius(tl, tr, br, bl).Background(rowBg).
								BorderWidth(0, 0, boolToBorder(showBottomBorder), 0).BorderColor(pal.Hairline).
								Cursor(ui.CursorPointer).Label(gutterSym + " " + trimmed)
							bui.Probe(rowBtn, lineID)
							selIdx := dl.SelectionIndex
							rowBtn.Children(func() {
								ui.Text(cUI, gutterSym).Width(12).FontSize(theme.FontBody).TextColor(gutterFg).SingleLine()
								if isTable {
									cells := parseDiffTableCells(trimmed)
									ui.Row(cUI).Grow(1).MinWidth(0).Children(func() {
										for ci, cellTxt := range cells {
											ui.Box(cUI).Grow(1).Shrink(1).Padding(4, 8).
												BorderWidth(0, boolToBorder(ci+1 < len(cells)), 0, 0).BorderColor(pal.Hairline).
												Children(func() {
													w := theme.WeightRegular
													if isHeader {
														w = theme.WeightControl
													}
													ui.Text(cUI, cellTxt).FontSize(theme.FontBody).FontWeight(w).TextColor(rowFg)
												})
										}
									})
								} else {
									ui.Text(cUI, strings.TrimRight(dl.Text, "\r\n")).FontSize(theme.FontBody).TextColor(rowFg).Grow(1)
								}
							})
							if rowBtn.Clicked() {
								c.TogglePlanDiffLine(convID, msgIdx, preview.Page, selIdx)
							}
						} else {
							ctxRow := ui.Row(cUI.Key(lineID)).FillWidth().Padding(4, 10).Gap(8).
								AlignItems(ui.Center).Radius(tl, tr, br, bl).Background(rowBg).
								BorderWidth(0, 0, boolToBorder(showBottomBorder), 0).BorderColor(pal.Hairline)
							ctxRow.Children(func() {
								ui.Box(cUI).Width(12).Shrink(0)
								if isTable {
									cells := parseDiffTableCells(trimmed)
									ui.Row(cUI).Grow(1).MinWidth(0).Children(func() {
										for ci, cellTxt := range cells {
											ui.Box(cUI).Grow(1).Shrink(1).Padding(4, 8).
												BorderWidth(0, boolToBorder(ci+1 < len(cells)), 0, 0).BorderColor(pal.Hairline).
												Children(func() {
													w := theme.WeightRegular
													if isHeader {
														w = theme.WeightControl
													}
													ui.Text(cUI, cellTxt).FontSize(theme.FontBody).FontWeight(w).TextColor(pal.Text)
												})
										}
									})
								} else {
									ui.Text(cUI, strings.TrimRight(dl.Text, "\r\n")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).Grow(1)
								}
							})
						}
					}
				})
			})
		}

		ui.Row(cUI).FillWidth().Padding(10, 14).Gap(8).Radius(0, 0, 11, 11).Background(pal.Shell).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			ui.Text(cUI, badge).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(badgeCol)
			ui.Row(cUI).Gap(8).Children(func() {
				switch plan.Status {
				case "pending":
					rejectBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-reject-%d", msgIdx), theme.Tr("拒绝", "Reject"), nil, bui.ButtonSecondary, false)
					if rejectBtn.Clicked() {
						_ = c.RejectPlan(convID, msgIdx)
					}
					acceptBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-accept-%d", msgIdx), theme.Tr("接受", "Accept"), nil, bui.ButtonPrimary, false)
					if acceptBtn.Clicked() {
						_ = c.AcceptPlan(convID, msgIdx)
					}
				case "applied":
					undoBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-undo-%d", msgIdx), theme.Tr("撤销", "Undo"), nil, bui.ButtonSecondary, false)
					if undoBtn.Clicked() {
						_ = c.UndoPlan(convID, msgIdx)
					}
				}
			})
		})
	})
}

func (c *Controller) renderQuestionCard(cUI *ui.Context, pal theme.Palette, convID uint64, msgIdx int, qcard *model.QuestionCard) {
	if len(qcard.Questions) == 0 {
		return
	}
	qKey := fmt.Sprintf("q-%d-%d", convID, msgIdx)
	c.mu.Lock()
	st := c.questionUI[qKey]
	if st == nil {
		st = &questionUIState{
			answers:    make(map[int]map[int]bool),
			customText: make(map[int]string),
		}
		c.questionUI[qKey] = st
	}
	if qcard.Submitted {
		st.submitted = true
	}
	qi := st.qi
	if qi >= len(qcard.Questions) {
		qi = len(qcard.Questions) - 1
	}
	st.qi = qi
	dismissed := st.dismissed
	c.mu.Unlock()

	if qcard.Submitted {
		card := ui.Row(cUI.Key("ai-q-done-"+qKey)).FillWidth().Padding(10, 12).Radius(theme.RadiusControl).
			Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
			ui.Text(cUI, theme.Tr("已提交选择", "Response submitted")).FontSize(14).TextColor(pal.TextSecondary)
		})
		bui.Probe(card, "ai-question-card-done")
		return
	}

	if dismissed {
		reopen := bui.ActionButton(cUI, "ai-question-reopen", theme.Tr("展开待确认问题", "Open question"), nil, bui.ButtonSecondary, false)
		if reopen.Clicked() {
			c.mu.Lock()
			st.dismissed = false
			c.mu.Unlock()
		}
		return
	}

	q := qcard.Questions[qi]
	total := len(qcard.Questions)
	last := qi+1 >= total

	card := ui.Column(cUI.Key("ai-question-card-"+qKey)).FillWidth().Padding(14).Gap(10).
		Radius(theme.RadiusCard).Background(pal.Paper).Border(1, pal.BtnBorder).
		Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.08))
	bui.Probe(card, "ai-question-card")

	card.Children(func() {
		ui.Row(cUI).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Text(cUI, q.Prompt).FontSize(14).FontWeight(600).TextColor(pal.Text).Grow(1)
			closeBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
				ID:       "ai-question-dismiss",
				Label:    theme.Tr("收起问题", "Dismiss question"),
				Size:     24,
				IconSize: 14,
			})
			if closeBtn.Clicked() {
				c.mu.Lock()
				st.dismissed = true
				c.mu.Unlock()
			}
		})

		c.mu.RLock()
		pickedMap := st.answers[qi]
		customVal := st.customText[qi]
		c.mu.RUnlock()

		ui.Column(cUI).FillWidth().Gap(4).Children(func() {
			for idx, opt := range q.Options {
				optID := fmt.Sprintf("ai-question-opt-%d-%d", qi, idx)
				on := pickedMap != nil && pickedMap[idx]
				row := ui.ButtonBase(cUI.Key(optID+"-"+qKey)).FillWidth().MinHeight(32).Padding(6, 8).Gap(8).
					AlignItems(ui.Center).Radius(6).Border(1, ui.Transparent).Cursor(ui.CursorPointer).Label(opt.Label)
				if on {
					row.Background(pal.Selected).Border(1, pal.Accent)
				} else if row.Hovered() {
					row.Background(pal.Hover)
				}
				bui.Probe(row, optID)
				row.Children(func() {
					ind := ui.Box(cUI).Size(16, 16).Shrink(0).Border(1, pal.BtnBorder).Center()
					if q.MultiSelect {
						ind.Radius(4)
					} else {
						ind.Radius(theme.RadiusPill)
					}
					if on {
						ind.BorderColor(pal.Text).Background(pal.Text)
						ind.Children(func() {
							if q.MultiSelect {
								icons.Draw(cUI, icons.Check, 12, pal.MonoIconColors(pal.Paper))
							} else {
								ui.Box(cUI).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Paper)
							}
						})
					}
					fg := pal.TextSecondary
					if on {
						fg = pal.Text
					}
					ui.Text(cUI, opt.Label).FontSize(14).TextColor(fg).Grow(1)
				})
				if row.Clicked() {
					c.mu.Lock()
					if st.answers[qi] == nil {
						st.answers[qi] = make(map[int]bool)
					}
					if q.MultiSelect {
						if st.answers[qi][idx] {
							delete(st.answers[qi], idx)
						} else {
							st.answers[qi][idx] = true
						}
					} else {
						st.answers[qi] = map[int]bool{idx: true}
						st.customText[qi] = ""
					}
					c.mu.Unlock()
				}
			}

			custom := customVal
			in := ui.TextInput(cUI.Key(fmt.Sprintf("ai-q-custom-%s-%d", qKey, qi)), &custom).
				FillWidth().FontSize(14).Placeholder(theme.Tr("其他补充…", "Something else…"))
			bui.Probe(in, "ai-question-custom")
			if in.Changed() {
				c.mu.Lock()
				st.customText[qi] = custom
				if !q.MultiSelect && strings.TrimSpace(custom) != "" {
					st.answers[qi] = nil
				}
				c.mu.Unlock()
			}
		})

		hasAnswer := len(pickedMap) > 0 || strings.TrimSpace(customVal) != ""
		ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(cUI).Gap(6).AlignItems(ui.Center).Children(func() {
				prevBtn := bui.IconButton(cUI, icons.ChevronUp, bui.IconButtonOptions{
					ID:       "ai-question-prev",
					Label:    theme.Tr("上一题", "Previous question"),
					Size:     24,
					IconSize: 14,
					Disabled: qi == 0,
				})
				if prevBtn.Clicked() && qi > 0 {
					c.mu.Lock()
					st.qi = qi - 1
					c.mu.Unlock()
				}
				ui.Textf(cUI, "%d / %d", qi+1, total).FontSize(14).TextColor(pal.TextSecondary)
				nextBtn := bui.IconButton(cUI, icons.ChevronDown, bui.IconButtonOptions{
					ID:       "ai-question-next",
					Label:    theme.Tr("下一题", "Next question"),
					Size:     24,
					IconSize: 14,
					Disabled: last,
				})
				if nextBtn.Clicked() && !last {
					c.mu.Lock()
					st.qi = qi + 1
					c.mu.Unlock()
				}
			})

			ui.Row(cUI).Gap(8).Children(func() {
				skipBtn := bui.ActionButton(cUI, "ai-question-skip", theme.Tr("跳过", "Skip"), nil, bui.ButtonSecondary, false)
				if skipBtn.Clicked() {
					c.mu.Lock()
					delete(st.answers, qi)
					st.customText[qi] = ""
					c.mu.Unlock()
					if !last {
						c.mu.Lock()
						st.qi = qi + 1
						c.mu.Unlock()
					} else {
						c.SubmitQuestion(convID, msgIdx, true)
					}
				}
				contLabel := theme.Tr("继续", "Continue")
				if last {
					contLabel = theme.Tr("发送", "Send")
				}
				contBtn := bui.ActionButton(cUI, "ai-question-continue", contLabel, nil, bui.ButtonPrimary, !hasAnswer)
				if contBtn.Clicked() && hasAnswer {
					if !last {
						c.mu.Lock()
						st.qi = qi + 1
						c.mu.Unlock()
					} else {
						c.SubmitQuestion(convID, msgIdx, false)
					}
				}
			})
		})
	})
}

func (c *Controller) renderApprovalCard(cUI *ui.Context, pal theme.Palette, convID uint64, msgIdx int, cardModel *model.ApprovalCard) {
	aKey := fmt.Sprintf("a-%d-%d", convID, msgIdx)
	c.mu.Lock()
	st := c.approvalUI[aKey]
	if st == nil {
		st = &approvalUIState{}
		c.approvalUI[aKey] = st
	}
	if cardModel.Decision != model.ApprovalPending && !cardModel.Failed {
		st.choice = cardModel.Decision
		st.submitted = true
	}
	choice := st.choice
	submitted := st.submitted
	c.mu.Unlock()

	card := ui.Column(cUI.Key("ai-approval-card-"+aKey)).FillWidth().MaxWidth(340).Padding(14).Gap(10).
		Radius(theme.RadiusCard).Background(pal.Paper).Border(1, pal.BtnBorder)
	bui.Probe(card, "ai-approval-card")

	card.Children(func() {
		ui.Text(cUI, cardModel.ToolName).FontSize(14).FontWeight(600).TextColor(pal.Text)
		if cardModel.Command != "" {
			ui.Text(cUI, cardModel.Command).FontSize(14).TextColor(pal.TextSecondary)
		}
		if cardModel.Reason != "" {
			ui.Text(cUI, cardModel.Reason).FontSize(14).TextColor(pal.TextSecondary)
		}

		renderOpt := func(id, label string, dec model.ApprovalDecision) {
			selected := choice == dec
			row := ui.ButtonBase(cUI.Key(id+"-"+aKey)).FillWidth().MinHeight(30).Padding(5, 8).Gap(8).
				AlignItems(ui.Center).Radius(6).Border(1, ui.Transparent).Cursor(ui.CursorPointer).Label(label)
			if selected {
				row.Background(pal.Selected).Border(1, pal.Accent)
			} else if row.Hovered() {
				row.Background(pal.Hover)
			}
			bui.Probe(row, id)
			if selected {
				bui.Probe(row, "ai-approval-selected-"+string(dec))
			}
			row.Children(func() {
				dot := ui.Box(cUI).Size(16, 16).Shrink(0).Radius(theme.RadiusPill).
					Border(1, pal.BtnBorder).Center()
				if selected {
					dot.BorderColor(pal.Text).Background(pal.Text)
					dot.Children(func() {
						ui.Box(cUI).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Paper)
					})
				}
				fg := pal.TextSecondary
				if selected {
					fg = pal.Text
				}
				ui.Text(cUI, label).FontSize(14).TextColor(fg)
			})
			if row.Clicked() && !submitted {
				c.mu.Lock()
				st.choice = dec
				c.mu.Unlock()
			}
		}

		ui.Column(cUI).FillWidth().Gap(4).Children(func() {
			renderOpt("ai-approval-option-allow", theme.Tr("允许一次", "Allow once"), model.ApprovalAllow)
			renderOpt("ai-approval-option-deny", theme.Tr("拒绝", "Deny"), model.ApprovalDeny)
		})

		if cardModel.Failed && cardModel.ErrorText != "" {
			ui.Text(cUI, cardModel.ErrorText).FontSize(14).TextColor(pal.Danger)
		}

		enabled := choice != model.ApprovalPending && !submitted
		ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, "1 / 1").FontSize(14).TextColor(pal.TextSecondary)
			ui.Row(cUI).Gap(8).Children(func() {
				skipBtn := bui.ActionButton(cUI, "ai-approval-skip", theme.Tr("跳过", "Skip"), nil, bui.ButtonSecondary, submitted)
				if skipBtn.Clicked() && !submitted {
					c.mu.Lock()
					st.choice = model.ApprovalSkip
					st.submitted = true
					c.mu.Unlock()
					c.RespondApproval(convID, msgIdx, model.ApprovalDeny)
				}
				contLabel := theme.Tr("继续", "Continue")
				if cardModel.Failed {
					contLabel = theme.Tr("重试", "Retry")
				}
				contBtn := bui.ActionButton(cUI, "ai-approval-continue", contLabel, nil, bui.ButtonPrimary, !enabled)
				if !enabled {
					bui.Probe(contBtn, "ai-approval-continue-disabled")
				}
				if contBtn.Clicked() && enabled {
					c.mu.Lock()
					st.submitted = true
					c.mu.Unlock()
					c.RespondApproval(convID, msgIdx, choice)
				}
			})
		})
	})
}

func (c *Controller) renderTurnError(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.RLock()
	errText := c.turnError
	needSettings := c.turnErrorSettings
	c.mu.RUnlock()
	if errText == "" {
		return
	}

	box := ui.Row(cUI.Key("ai-turn-error")).FillWidth().Padding(10, 14).Gap(8).AlignItems(ui.Start)
	if fullscreen {
		box.MaxWidth(720).Margin(0, ui.Auto)
	}
	box.Children(func() {
		ui.Column(cUI).Grow(1).Padding(10, 12).Gap(6).Radius(theme.RadiusControl).
			Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).Children(func() {
			ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(cUI, theme.Tr("本轮失败原因", "Request failed")).FontSize(14).FontWeight(600).TextColor(pal.Danger)
				closeErr := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
					ID:       "ai-chat-error-close",
					Label:    theme.Tr("关闭错误提示", "Dismiss error"),
					Size:     24,
					IconSize: 14,
				})
				if closeErr.Clicked() {
					c.mu.Lock()
					c.turnError = ""
					c.turnErrorSettings = false
					c.mu.Unlock()
				}
			})
			ui.Text(cUI, errText).FontSize(14).TextColor(pal.Text)
			if needSettings && c.host != nil {
				btn := bui.ActionButton(cUI, "ai-chat-settings", theme.Tr("打开 AI 设置", "Open AI settings"), nil, bui.ButtonSecondary, false)
				if btn.Clicked() {
					c.host.OpenSettings("ai")
				}
			}
		})
	})
}

func (c *Controller) renderDeferredDraftBanner(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.RLock()
	hasDeferred := false
	for _, d := range c.deferredDrafts {
		if c.currentID == 0 || d.Conversation == c.currentID {
			hasDeferred = true
			break
		}
	}
	c.mu.RUnlock()
	if !hasDeferred {
		return
	}

	row := ui.Row(cUI.Key("ai-deferred-banner")).FillWidth().Padding(4, 16).Gap(8).AlignItems(ui.Center)
	if fullscreen {
		row.MaxWidth(720).Margin(0, ui.Auto)
	}
	row.Children(func() {
		ui.Text(cUI, theme.Tr("上一条未完成的草稿可恢复。", "An unfinished draft can be restored.")).
			FontSize(14).TextColor(pal.TextSecondary).Grow(1)
		btn := bui.ActionButton(cUI, "ai-restore-draft", theme.Tr("恢复草稿", "Restore draft"), nil, bui.ButtonSecondary, false)
		if btn.Clicked() {
			c.restoreDeferredDraft()
		}
	})
}

func (c *Controller) restoreDeferredDraft() {
	c.mu.Lock()
	idx := -1
	for i, d := range c.deferredDrafts {
		if c.currentID == 0 || d.Conversation == c.currentID {
			idx = i
			break
		}
	}
	if idx < 0 {
		c.mu.Unlock()
		return
	}
	d := c.deferredDrafts[idx]
	c.deferredDrafts = append(c.deferredDrafts[:idx], c.deferredDrafts[idx+1:]...)
	if c.currentID == 0 && c.hasConvLocked(d.Conversation) {
		c.currentID = d.Conversation
	}
	c.draftText = d.Text
	c.draftRefs = append([]string(nil), d.Refs...)
	c.draftSkills = append([]string(nil), d.Skills...)
	c.composerEpoch++
	c.composerSelectEnd = true
	c.persistFailedDraftsLocked()
	c.persistActiveDraftLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

func activePickerToken(text string, composing bool, dismissed bool) (string, bool) {
	if composing || dismissed {
		return "", false
	}
	idx := strings.LastIndexAny(text, " \t\n\r")
	tok := text
	if idx >= 0 {
		tok = text[idx+1:]
	}
	if strings.HasPrefix(tok, "@") || strings.HasPrefix(tok, "/") {
		return tok, true
	}
	return "", false
}

func (c *Controller) pickerItemsLocked(token string) []pickItem {
	if strings.HasPrefix(token, "@") {
		q := strings.ToLower(strings.TrimPrefix(token, "@"))
		folder := c.pickerFolder
		currentPage := c.activePageLocked(c.activeRootLocked())
		var items []pickItem
		if q == "" && folder != "" {
			parent := pathDirSlash(folder)
			items = append(items, pickItem{
				kind:  pickFolder,
				value: parent,
				label: theme.Tr("返回上级", "Parent folder"),
			})
		}
		for _, f := range c.pickerFolders {
			if (q == "" && pathDirSlash(f) == folder) || (q != "" && strings.Contains(strings.ToLower(f), q)) {
				lbl := f
				if q == "" {
					lbl = pathBaseSlash(f)
				}
				items = append(items, pickItem{kind: pickFolder, value: f, label: lbl})
			}
		}
		refSet := make(map[string]bool, len(c.draftRefs))
		for _, r := range c.draftRefs {
			refSet[r] = true
		}
		if currentPage != "" && !refSet[currentPage] {
			if (q == "" && pathDirSlash(currentPage) == folder) || (q != "" && strings.Contains(strings.ToLower(currentPage), q)) {
				lbl := currentPage
				if q == "" {
					lbl = pathBaseSlash(currentPage)
				}
				items = append(items, pickItem{
					kind:  pickPage,
					value: currentPage,
					label: lbl,
					hint:  theme.Tr("当前文件", "Current file"),
				})
			}
		}
		for _, p := range c.pickerPages {
			if p == currentPage || refSet[p] {
				continue
			}
			if (q == "" && pathDirSlash(p) == folder) || (q != "" && strings.Contains(strings.ToLower(p), q)) {
				lbl := p
				if q == "" {
					lbl = pathBaseSlash(p)
				}
				items = append(items, pickItem{kind: pickPage, value: p, label: lbl})
			}
		}
		return items
	}

	if strings.HasPrefix(token, "/") {
		q := strings.ToLower(strings.TrimPrefix(token, "/"))
		newLabel := theme.Tr("/新会话", "/new")
		all := []pickItem{{
			kind:  pickNewConv,
			value: "",
			label: newLabel,
			hint:  theme.Tr("开启空白对话", "Start a new chat"),
		}}
		for _, sk := range c.pickerSkills {
			all = append(all, pickItem{
				kind:  pickSkill,
				value: sk.Name,
				label: "/" + sk.Name,
				hint:  sk.Description,
			})
		}
		var out []pickItem
		for _, it := range all {
			if q == "" ||
				strings.Contains(strings.ToLower(it.label), q) ||
				strings.Contains(strings.ToLower(it.hint), q) ||
				(q == "new" && it.kind == pickNewConv) {
				out = append(out, it)
			}
		}
		return out
	}
	return nil
}

func (c *Controller) applyPickerChoiceLocked(it pickItem, token string) (newDraft string, handled bool) {
	if !strings.HasSuffix(c.draftText, token) {
		return c.draftText, false
	}
	rest := c.draftText[:len(c.draftText)-len(token)]
	switch it.kind {
	case pickFolder:
		c.pickerFolder = it.value
		c.pickIndex = 0
		c.draftText = rest + "@"
		c.composerSelectEnd = true
		c.persistActiveDraftLocked()
		return c.draftText, true
	case pickPage:
		if !containsString(c.draftRefs, it.value) {
			c.draftRefs = append(c.draftRefs, it.value)
		}
	case pickSkill:
		if !containsString(c.draftSkills, it.value) {
			c.draftSkills = append(c.draftSkills, it.value)
		}
	case pickNewConv:
		return "", true
	}
	c.pickIndex = 0
	c.draftText = rest
	c.composerSelectEnd = true
	c.persistActiveDraftLocked()
	return c.draftText, true
}

func (c *Controller) renderPicker(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.RLock()
	token, open := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	if !open {
		c.mu.RUnlock()
		return
	}
	items := c.pickerItemsLocked(token)
	selected := c.pickIndex
	if selected >= len(items) {
		selected = max(len(items)-1, 0)
	}
	loading := c.pickerLoading
	c.mu.RUnlock()

	_, winH := cUI.Size()
	if winH <= 0 {
		winH = 760
	}
	pickerMaxH := min(max(winH-260, 80), 200)

	isRef := strings.HasPrefix(token, "@")
	probeID := "ai-skill-picker"
	headerTitle := theme.Tr("指令与技能", "Commands & Skills")
	emptyMsg := theme.Tr("没有匹配的指令或技能", "No matching commands or skills")
	if isRef {
		probeID = "ai-ref-picker"
		headerTitle = theme.Tr("引用文件", "Reference File")
		emptyMsg = theme.Tr("没有匹配的文件", "No matching files")
	}

	wrap := ui.Column(cUI.Key("ai-picker-wrap")).FillWidth().Padding(0, 16, 4, 16).
		Transition(bui.PopupTransition())
	if fullscreen {
		wrap.MaxWidth(720).Margin(0, ui.Auto)
	}
	wrap.Children(func() {
		card := ui.Column(cUI.Key(probeID)).FillWidth().MaxHeight(pickerMaxH).Radius(theme.RadiusSurface).
			Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.12)).Clip()
		bui.Probe(card, probeID)
		card.Children(func() {
			ui.Text(cUI, headerTitle).FontSize(14).FontWeight(500).TextColor(pal.TextSecondary).Padding(8, 12, 4, 12)
			if len(items) == 0 {
				msg := emptyMsg
				if loading {
					msg = theme.Tr("正在加载…", "Loading…")
				}
				ui.Text(cUI, msg).FontSize(14).TextColor(pal.TextSecondary).Padding(10, 12)
				return
			}

			listH := min(float32(len(items))*36, max(pickerMaxH-32, 36))
			c.pickerList.Key = func(i int) any {
				return fmt.Sprintf("%d:%s", items[i].kind, items[i].value)
			}
			ui.List(cUI.Key("ai-picker-virtual-list"), &c.pickerList, len(items), func(i int) {
				it := items[i]
				rowID := fmt.Sprintf("ai-pick-%d", i)
				row := ui.ButtonBase(cUI.Key(rowID)).FillWidth().Height(36).PaddingX(10).Gap(8).
					AlignItems(ui.Center).Radius(6).Border(1, ui.Transparent).Cursor(ui.CursorPointer).Label(it.label)
				if i == selected {
					row.Background(pal.Selected).Border(1, pal.Accent)
				} else if row.Hovered() {
					row.Background(pal.Hover)
				}
				bui.Probe(row, rowID)
				row.Children(func() {
					ic := icons.Sparkles
					switch it.kind {
					case pickFolder:
						ic = icons.Folder
					case pickPage:
						ic = icons.Note
					}
					icons.Draw(cUI, ic, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
					ui.Text(cUI, it.label).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).SingleLine()
					if it.hint != "" {
						ui.Text(cUI, it.hint).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					}
					if it.kind == pickFolder {
						icons.Draw(cUI, icons.ChevronRight, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
					}
				})
				if row.Clicked() {
					if it.kind == pickNewConv {
						c.NewConversation()
						return
					}
					c.mu.Lock()
					_, _ = c.applyPickerChoiceLocked(it, token)
					c.composerHandle.Focus()
					c.mu.Unlock()
				}
			}).FillWidth().Height(listH)
		})
	})
}

func (c *Controller) renderComposerArea(cUI *ui.Context, pal theme.Palette, fullscreen bool, panelW float32) {
	c.mu.RLock()
	draft := c.draftText
	refs := append([]string(nil), c.draftRefs...)
	skills := append([]string(nil), c.draftSkills...)
	epoch := c.composerEpoch
	busy := c.streaming || c.hasPendingQuestionLocked()
	hasConv := c.currentID != 0
	selectEnd := c.composerSelectEnd
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	c.mu.RUnlock()

	ph := theme.Tr("向 AI 提问，/ 调用技能，@ 引用文件…", "Ask AI, / for skills, @ to reference files…")
	if hasConv {
		ph = theme.Tr("继续追问…", "Ask a follow-up…")
	}

	padTop := float32(12)
	if pickerOpen {
		padTop = 0
	}
	outer := ui.Column(cUI.Key("ai-composer-outer")).FillWidth().Padding(padTop, 16, 16, 16).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
	if fullscreen {
		outer.MaxWidth(720).Margin(0, ui.Auto)
	}

	var deckAnchor ui.Element
	outer.Children(func() {
		shell := ui.Column(cUI).FillWidth().Padding(8, 10).Gap(6).Radius(theme.RadiusControl).
			Background(pal.Hover).Border(1, pal.BtnBorder)
		shell.Children(func() {
			if len(refs) > 0 || len(skills) > 0 {
				ui.Row(cUI).FillWidth().Gap(6).Wrap().Children(func() {
					for i, r := range refs {
						c.renderDraftChip(cUI, pal, fmt.Sprintf("chip-ref-%d", i), true, r)
					}
					for i, s := range skills {
						c.renderDraftChip(cUI, pal, fmt.Sprintf("chip-skill-%d", i), false, s)
					}
				})
			}

			composerKey := fmt.Sprintf("ai-composer-%d", epoch)
			inputEl := ui.TextAreaBase(cUI.Key(composerKey), &draft).
				FillWidth().Lines(1, 6).Padding(0).FontSize(theme.FontBody).TextColor(pal.Text).
				CaretColor(pal.Accent).CaretWidth(2).
				Placeholder(ph).Bind(&c.composerHandle).
				Label(theme.Tr("AI 输入框", "AI composer"))
			bui.Probe(inputEl, "ai-composer")

			composingNow := inputEl.Composing()
			inputEl.OnTextKey(func(k ui.TextKey) bool {
				c.mu.Lock()
				curText := k.Text()
				c.draftText = curText
				tok, open := activePickerToken(curText, c.composerComposing, c.pickerDismissed)
				var items []pickItem
				if open {
					items = c.pickerItemsLocked(tok)
				}
				switch {
				case k.Key == ui.KeyEscape && open:
					c.pickerDismissed = true
					c.mu.Unlock()
					return true
				case k.Key == ui.KeyUp && open && len(items) > 0:
					if c.pickIndex > 0 {
						c.pickIndex--
					} else {
						c.pickIndex = len(items) - 1
					}
					c.pickerList.ScrollIntoView(c.pickIndex)
					c.mu.Unlock()
					return true
				case k.Key == ui.KeyDown && open && len(items) > 0:
					c.pickIndex = (c.pickIndex + 1) % len(items)
					c.pickerList.ScrollIntoView(c.pickIndex)
					c.mu.Unlock()
					return true
				case k.Key == ui.KeyTab && k.Mods == 0 && open && len(items) > 0:
					idx := min(c.pickIndex, len(items)-1)
					it := items[idx]
					if it.kind == pickNewConv {
						c.mu.Unlock()
						c.NewConversation()
						k.SetText("")
						return true
					}
					next, ok := c.applyPickerChoiceLocked(it, tok)
					c.mu.Unlock()
					if ok {
						k.SetText(next)
					}
					return true
				case k.Key == ui.KeyEnter && k.Mods == 0:
					if open && len(items) > 0 {
						idx := min(c.pickIndex, len(items)-1)
						it := items[idx]
						if it.kind == pickNewConv {
							c.mu.Unlock()
							c.NewConversation()
							k.SetText("")
							return true
						}
						next, ok := c.applyPickerChoiceLocked(it, tok)
						c.mu.Unlock()
						if ok {
							k.SetText(next)
						}
						return true
					}
					if c.streaming || strings.TrimSpace(curText) == "" {
						c.mu.Unlock()
						return true
					}
					c.mu.Unlock()
					c.SendPrompt(curText)
					k.SetText("")
					return true
				}
				c.mu.Unlock()
				return false
			})

			if selectEnd {
				n := utf8.RuneCountInString(draft)
				inputEl.SetTextSelection(n, n)
				c.mu.Lock()
				c.composerSelectEnd = false
				c.mu.Unlock()
			}

			if inputEl.Changed() || composingNow != c.composerComposing {
				c.mu.Lock()
				c.composerComposing = composingNow
				if inputEl.Changed() {
					c.draftText = draft
					c.pickerDismissed = false
					c.pickIndex = 0
					c.saveTabDraftLocked()
					c.persistActiveDraftLocked()
				}
				c.mu.Unlock()
			}

			actionsRow := ui.Row(cUI.Key("ai-composer-actions")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8)
			bui.Probe(actionsRow, "ai-composer-actions")
			actionsRow.Children(func() {
				deckAnchor = c.renderDeckTrigger(cUI, pal)

				if busy {
					stopBtn := bui.ActionButton(cUI, "ai-chat-stop", theme.Tr("停止", "Stop"), nil, bui.ButtonSecondary, false)
					if stopBtn.Clicked() {
						c.StopStreaming()
					}
				} else {
					canSend := strings.TrimSpace(draft) != ""
					sendBtn := bui.IconButton(cUI, icons.ArrowUp, bui.IconButtonOptions{
						ID:       "ai-chat-send",
						Label:    theme.Tr("发送", "Send"),
						Tooltip:  theme.Tr("发送（回车）", "Send (Enter)"),
						Size:     28,
						IconSize: 16,
						Active:   canSend,
						Disabled: !canSend,
					})
					if sendBtn.Clicked() && canSend {
						c.SendPrompt(draft)
					}
				}
			})
		})
	})

	if deckAnchor.Valid() {
		c.renderDeckPopover(cUI, pal, deckAnchor, fullscreen, panelW)
	}
}

func (c *Controller) renderDraftChip(cUI *ui.Context, pal theme.Palette, id string, isRef bool, name string) {
	label := name
	ic := icons.Note
	if !isRef {
		label = "/" + name
		ic = icons.Sparkles
	}
	ui.Row(cUI.Key(id)).Padding(3, 8).Gap(6).AlignItems(ui.Center).Radius(6).
		Background(pal.Paper).Border(1, pal.BtnBorder).Children(func() {
		icons.Draw(cUI, ic, theme.IconRow, pal.MonoIconColors(pal.Accent))
		ui.Text(cUI, label).FontSize(theme.FontBody).TextColor(pal.Text)
		xBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
			ID:       id + "-x",
			Label:    theme.Tr("移除 ", "Remove ") + label,
			Size:     20,
			IconSize: 12,
		})
		if xBtn.Clicked() {
			c.mu.Lock()
			if isRef {
				c.draftRefs = removeString(c.draftRefs, name)
			} else {
				c.draftSkills = removeString(c.draftSkills, name)
			}
			c.saveTabDraftLocked()
			c.persistActiveDraftLocked()
			c.mu.Unlock()
		}
	})
}

func (c *Controller) renderDeckTrigger(cUI *ui.Context, pal theme.Palette) ui.Element {
	c.mu.RLock()
	kind := c.selectedBackendLocked()
	mdl := c.backendModelLocked(kind)
	thinking := c.backendThinkingLocked(kind)
	open := c.deckOpen
	c.mu.RUnlock()

	label := backendLabel(kind)
	if mdl != "" {
		label = mdl
	}

	btn := ui.ButtonBase(cUI.Key("ai-deck-trigger")).Height(30).PaddingX(10).Gap(6).
		AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(label)
	if open {
		btn.Background(pal.Selected).Border(1, pal.Accent)
	} else {
		btn.Background(pal.Paper).Border(1, pal.BtnBorder)
	}
	if b := btn.Bounds(); b.W > 0 {
		c.mu.Lock()
		c.deckTriggerBox = b
		c.mu.Unlock()
	}
	bui.Probe(btn, "ai-deck-trigger")

	btn.Children(func() {
		icons.Brand(cUI, kind, 15, pal.Text)
		ui.Text(cUI, label).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
		ui.Text(cUI, strings.ToUpper(thinking[:1])+thinking[1:]).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
		chev := icons.ChevronDown
		if open {
			chev = icons.ChevronUp
		}
		icons.Draw(cUI, chev, 12, pal.MonoIconColors(pal.TextSecondary))
	})
	if btn.Clicked() {
		c.mu.Lock()
		c.deckOpen = !c.deckOpen
		if c.deckOpen {
			c.historyOpen = false
			c.pickerDismissed = true
			c.deckSearch = ""
		}
		c.mu.Unlock()
		if c.deckOpen {
			c.probeAllBackendsAsync()
			c.refreshModelsAsync(kind)
		}
	}
	return btn
}

func (c *Controller) renderDeckPopover(cUI *ui.Context, pal theme.Palette, anchor ui.Element, fullscreen bool, panelW float32) {
	c.mu.RLock()
	open := c.deckOpen
	trigger := c.deckTriggerBox
	panelBox := c.panelBox
	visible := c.visibleBackendsLocked()
	selected := c.selectedBackendLocked()
	search := c.deckSearch
	models := append([]ModelOption(nil), c.backendModels[selected]...)
	mStatus := c.modelStatus[selected]
	curModel := c.backendModelLocked(selected)
	curThinking := c.backendThinkingLocked(selected)
	fullPerm := selected == model.BackendBuiltin || c.backendsCfg.Adapters[string(selected)].FullPermissions
	c.mu.RUnlock()

	if !open {
		return
	}

	winW, winH := cUI.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}
	if panelBox.W <= 0 {
		px := max(winW-panelW, 0)
		if fullscreen {
			px = 0
			panelW = winW
		}
		panelBox = ui.Rect{X: px, Y: 0, W: panelW, H: winH}
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: max(panelBox.X+16, 16), Y: max(winH-54, 80), W: 140, H: 30}
	}

	wide := fullscreen || panelW >= 500
	var targetW float32
	if fullscreen {
		targetW = 688
	} else if wide {
		targetW = min(max(panelW-24, 460), 640)
	} else {
		targetW = min(max(panelW-24, 308), 420)
	}
	popW := min(targetW, max(winW-16, 1))

	spec := bui.PlaceAbove(trigger).WithAlignX(bui.AlignX{Kind: bui.AlignPinLeft}).WithGap(4).WithMargin(8)
	maxH := bui.MaxHeightForSpec(spec, winW, winH, max(winH-16, bui.MinScrollHeight))

	headerH := float32(38)
	headerPX := float32(10)
	listH := float32(172)
	listPad := float32(6)
	listGap := float32(2)
	footerH := float32(36)
	footerPX := float32(10)
	railW := float32(42)
	railPadY, railPadX, railGap := float32(8), float32(4), float32(6)
	btnSz, icoSz := float32(34), float32(18)
	rowH, rowPX := float32(34), float32(10)
	if wide {
		headerH = 44
		headerPX = 14
		listH = 220
		listPad = 8
		listGap = 3
		footerH = 40
		footerPX = 12
		railW = 54
		railPadY, railPadX, railGap = 10, 6, 8
		btnSz, icoSz = 38, 20
		rowH, rowPX = 38, 12
	}
	if fullscreen {
		listH = 276
	}
	overhead := headerH + footerH + 4
	if !wide {
		overhead += 38
	}
	effectiveListH := min(listH, max(maxH-overhead, 64))
	popH := min(effectiveListH+overhead, maxH)
	x, y := bui.ResolveOrigin(spec, popW, popH, winW, winH)

	ui.Overlay(cUI, func() {
		panel := ui.Column(cUI.Key("ai-agent-deck")).Absolute().Left(x).Top(y).
			Width(popW).MaxHeight(maxH).Radius(theme.RadiusSurface).
			Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18)).Clip().
			Transition(bui.PopupTransition())
		bui.Probe(panel, "ai-deck")

		panel.Children(func() {
			// Upper split: left backend rail + center search/models + (wide) right thinking column.
			ui.Row(cUI).FillWidth().MinWidth(0).Children(func() {
				// 1. Left backend icon rail (matching chat_deck.rs:57-77).
				ui.Column(cUI).Width(railW).Shrink(0).Padding(railPadY, railPadX).Gap(railGap).
					Background(pal.Shell).BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline).
					AlignItems(ui.Center).Children(func() {
					for _, kind := range visible {
						k := kind
						cardID := "ai-backend-" + backendProbeSuffix(k)
						active := k == selected
						ready := k == model.BackendBuiltin || func() bool {
							c.mu.RLock()
							st, ok := c.backendStatuses[k]
							c.mu.RUnlock()
							return ok && (st.State == BackendStateReady || st.State == BackendStateInstalled)
						}()
						btn := ui.ButtonBase(cUI.Key(cardID)).Size(btnSz, btnSz).Center().
							Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
							Label(backendLabel(k)).Tooltip(backendLabel(k))
						if active {
							btn.Background(pal.Selected).Border(1, pal.Accent)
						} else {
							if btn.Hovered() {
								btn.Background(pal.Hover)
							}
							btn.Border(1, ui.Transparent)
						}
						bui.Probe(btn, cardID)
						btn.Children(func() {
							icCol := pal.TextSecondary
							if active {
								icCol = pal.Text
							}
							icons.Brand(cUI, k, icoSz, icCol)
							if !ready {
								ui.Box(cUI).Absolute().Right(4).Bottom(4).Size(5, 5).
									Radius(theme.RadiusPill).Background(pal.TextSecondary)
							}
						})
						if btn.Clicked() {
							c.mu.Lock()
							changed := c.selectedBackendLocked() != k
							c.selectBackendLocked(k)
							c.mu.Unlock()
							if changed {
								c.refreshModelsAsync(k)
							}
						}
					}
				})

				// 2. Center search + model list (+ compact thinking row when !wide).
				ui.Column(cUI).Grow(1).MinWidth(0).Children(func() {
					ui.Row(cUI).FillWidth().Height(headerH).PaddingX(headerPX).Gap(8).AlignItems(ui.Center).
						BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
						icons.Draw(cUI, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
						q := search
						sInput := ui.TextInputBase(cUI.Key("ai-deck-search"), &q).
							Grow(1).Height(24).Padding(0).FontSize(14).TextColor(pal.Text).
							CaretColor(pal.Accent).CaretWidth(2).
							Placeholder(theme.Tr("搜索模型…", "Search models…")).
							Bind(&c.deckSearchHandle).AutoFocus()
						bui.Probe(sInput, "ai-deck-search")
						if sInput.Changed() {
							c.mu.Lock()
							c.deckSearch = q
							c.mu.Unlock()
						}
						sInput.OnTextKey(func(k ui.TextKey) bool {
							if k.Key == ui.KeyEscape {
								c.mu.Lock()
								c.deckOpen = false
								c.mu.Unlock()
								return true
							}
							return false
						})
					})

					lowQ := strings.ToLower(strings.TrimSpace(search))
					var filtered []ModelOption
					for _, m := range models {
						if lowQ == "" || strings.Contains(strings.ToLower(m.Value), lowQ) || strings.Contains(strings.ToLower(m.Label), lowQ) {
							filtered = append(filtered, m)
						}
					}

					ui.Scroll(cUI.Key("ai-deck-models")).TrackScroll(&c.deckScroll).
						FillWidth().Height(effectiveListH).Padding(listPad).Gap(listGap).Children(func() {
						if len(filtered) == 0 {
							msg := theme.Tr("暂无可选模型", "No models available")
							if lowQ != "" {
								msg = theme.Tr("无匹配模型", "No matching models")
							} else if mStatus == "loading" {
								msg = theme.Tr("正在获取模型…", "Loading models…")
							}
							ui.Box(cUI).Height(36).PaddingX(12).AlignItems(ui.Center).Children(func() {
								ui.Text(cUI, msg).FontSize(14).TextColor(pal.TextSecondary)
							})
							return
						}
						for idx, m := range filtered {
							active := m.Value == curModel
							mID := fmt.Sprintf("ai-model-%d", idx)
							row := ui.ButtonBase(cUI.Key(mID)).FillWidth().Height(rowH).PaddingX(rowPX).Gap(8).
								AlignItems(ui.Center).Radius(theme.RadiusControl).Border(1, ui.Transparent).
								Cursor(ui.CursorPointer).Label(m.Label)
							if active {
								row.Background(pal.Selected).Border(1, pal.Accent)
							} else if row.Hovered() {
								row.Background(pal.Hover)
							}
							bui.Probe(row, mID)
							row.Children(func() {
								icons.Brand(cUI, selected, 15, pal.Text)
								ui.Text(cUI, m.Label).FontSize(14).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
								if active {
									icons.Draw(cUI, icons.Check, 14, pal.MonoIconColors(pal.Accent))
								}
							})
							if row.Clicked() {
								c.mu.Lock()
								_ = c.setBackendModelLocked(selected, m.Value)
								c.deckOpen = false
								c.mu.Unlock()
							}
						}
					})

					if !wide {
						levels := []string{"low", "medium", "high"}
						levelIdx := 2
						for i, lv := range levels {
							if lv == curThinking {
								levelIdx = i
							}
						}
						ui.Row(cUI).FillWidth().Padding(6, 10).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).
							BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
							ui.Text(cUI, "Thinking Level").FontSize(12).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
							if bui.SegmentedControl(cUI, "ai-deck-thinking", &levelIdx, []bui.SegmentOption{
								{ID: "ai-thinking-low", Label: "Low"},
								{ID: "ai-thinking-medium", Label: "Medium"},
								{ID: "ai-thinking-high", Label: "High"},
							}) {
								c.mu.Lock()
								_ = c.setBackendThinkingLocked(selected, levels[levelIdx])
								c.mu.Unlock()
							}
						})
					}
				})

				// 3. Right Thinking Level column in wide/fullscreen mode.
				if wide {
					thinkW := float32(176)
					if fullscreen {
						thinkW = 216
					}
					ui.Column(cUI).Width(thinkW).Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(pal.Hairline).Children(func() {
						ui.Row(cUI).FillWidth().Height(headerH).PaddingX(headerPX).AlignItems(ui.Center).
							BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
							ui.Text(cUI, "Thinking Level").FontSize(13).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
						})
						ui.Column(cUI).FillWidth().Padding(8).Gap(4).Children(func() {
							for _, lv := range []struct{ id, label string }{
								{"low", "Low"},
								{"medium", "Medium"},
								{"high", "High"},
							} {
								level := lv.id
								isAct := curThinking == level || (curThinking == "" && level == "high")
								btnID := "ai-thinking-" + level
								btn := ui.ButtonBase(cUI.Key(btnID)).FillWidth().Height(36).PaddingX(12).
									AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
									Cursor(ui.CursorPointer).Label(lv.label)
								if isAct {
									btn.Background(pal.Selected).Border(1, pal.Accent)
								} else {
									if btn.Hovered() {
										btn.Background(pal.Hover)
									}
									btn.Border(1, ui.Transparent)
								}
								bui.Probe(btn, btnID)
								btn.Children(func() {
									ui.Text(cUI, lv.label).FontSize(14).FontWeight(theme.WeightControl).TextColor(pal.Text)
									if isAct {
										icons.Draw(cUI, icons.Check, 14, pal.MonoIconColors(pal.Accent))
									}
								})
								if btn.Clicked() {
									c.mu.Lock()
									_ = c.setBackendThinkingLocked(selected, level)
									c.mu.Unlock()
								}
							}
						})
					})
				}
			})

			// Footer row
			ui.Row(cUI).FillWidth().Height(footerH).PaddingX(footerPX).AlignItems(ui.Center).Justify(ui.SpaceBetween).
				BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
				setIcon := icons.Sliders
				setBtn := bui.ActionButton(cUI, "ai-deck-settings", theme.Tr("AI 设置", "AI settings"), &setIcon, bui.ButtonGhost, false)
				if setBtn.Clicked() {
					c.mu.Lock()
					c.deckOpen = false
					c.mu.Unlock()
					if c.host != nil {
						c.host.OpenSettings("ai")
					}
				}

				permLabel := theme.Tr("标准权限", "Standard access")
				if fullPerm {
					permLabel = "Full access"
				}
				permBtn := bui.ActionButton(cUI, "ai-deck-permissions", permLabel, nil, bui.ButtonGhost, false)
				if permBtn.Clicked() {
					if selected == model.BackendBuiltin && c.host != nil {
						c.mu.Lock()
						c.deckOpen = false
						c.mu.Unlock()
						c.host.OpenSettings("ai")
					} else {
						c.mu.Lock()
						_ = c.setBackendFullPermissionsLocked(selected, !fullPerm)
						c.mu.Unlock()
					}
				}
			})
		})

		if panel.OverlayShortcut(0, ui.KeyEscape) || (!anchor.PressedInside() && panel.PressedOutside()) {
			c.mu.Lock()
			c.deckOpen = false
			c.mu.Unlock()
		}
	})
}

func backendLabel(k model.BackendKind) string {
	if k == model.BackendBuiltin {
		return theme.Tr("内置 AI", "Built-in AI")
	}
	return k.DisplayName()
}

func settingsCardSurface(pal theme.Palette) ui.Color {
	if theme.IsDark() {
		return pal.Menu
	}
	return pal.Paper
}

func settingsSelectedSurface(pal theme.Palette) ui.Color {
	if theme.IsDark() {
		return ui.Hex("#25354f")
	}
	return pal.Selected
}

func formatBackendVersionTag(st BackendStatus, hasStatus bool) string {
	if !hasStatus {
		return ""
	}
	switch st.State {
	case BackendStateReady, BackendStateInstalled:
		for _, part := range strings.Fields(st.Version) {
			hasDigit := false
			for _, r := range part {
				if r >= '0' && r <= '9' {
					hasDigit = true
					break
				}
			}
			if hasDigit {
				if strings.HasPrefix(part, "v") {
					return part
				}
				return "v" + part
			}
		}
		return ""
	case BackendStateUnauth:
		return theme.Tr("未登录", "Unauthenticated")
	case BackendStateUnsupported, BackendStateError:
		return theme.Tr("异常", "Error")
	default:
		return ""
	}
}

// RenderSettingsTab renders the Settings > AI Assistant tab matching thinkly-fast 1:1.
func (c *Controller) RenderSettingsTab(cUI *ui.Context) {
	c.mu.Lock()
	c.services = cUI.Services()
	shouldProbe := !c.settingsProbedOnce && !c.mockResponder
	if shouldProbe {
		c.settingsProbedOnce = true
	}
	c.mu.Unlock()
	if shouldProbe {
		c.probeAllBackendsAsync()
	}

	pal := theme.Ocean()
	cardBG := settingsCardSurface(pal)
	selBG := settingsSelectedSurface(pal)

	c.mu.RLock()
	cfg := cloneProviderConfig(c.providerCfg)
	backends := cloneBackendsConfig(c.backendsCfg)
	recognized := c.recognizedBackendsLocked()
	focused := c.settingsFocused
	showKey := c.settingsShowKey
	testing := c.settingsTesting
	testOK := c.settingsTestOK
	testErr := c.settingsTestErr
	baseURL := c.settingsBaseURL
	apiKey := c.settingsAPIKey
	annotCfg := c.annotCfg
	annotOpen := c.annotMenuOpen
	c.mu.RUnlock()

	hasFocused := false
	for _, k := range recognized {
		if k == focused {
			hasFocused = true
			break
		}
	}
	if !hasFocused && len(recognized) > 0 {
		focused = recognized[0]
	}

	root := ui.Column(cUI.Key("ai-settings-section")).FillWidth().Gap(24)
	bui.Probe(root, "ai-settings-section")

	root.Children(func() {
		renderEnabledHeader := func() {
			ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(cUI, theme.Tr("AI 与本机 Agent", "AI & Local Agents")).FontSize(16).FontWeight(600).TextColor(pal.Text)
				enLbl := theme.Tr("已关闭", "Disabled")
				if cfg.Enabled {
					enLbl = theme.Tr("已开启", "Enabled")
				}
				swBtn := ui.ButtonBase(cUI.Key("ai-enabled-switch")).Height(32).PaddingX(12).Gap(6).
					AlignItems(ui.Center).Radius(theme.RadiusPill).Cursor(ui.CursorPointer).
					Label(theme.Tr("启用 AI 助手", "Enable AI Assistant"))
				if cfg.Enabled {
					swBtn.Background(selBG).Border(1, pal.Accent)
				} else {
					swBtn.Background(pal.Shell).Border(1, pal.BtnBorder)
					if swBtn.Hovered() {
						swBtn.Background(pal.Hover)
					}
				}
				bui.Probe(swBtn, "ai-enabled-switch")
				swBtn.Children(func() {
					icCol := pal.TextSecondary
					if cfg.Enabled {
						icCol = pal.Accent
					}
					icons.Draw(cUI, icons.Sparkles, 14, pal.MonoIconColors(icCol))
					ui.Text(cUI, enLbl).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
				})
				if swBtn.Clicked() {
					cfg.Enabled = !cfg.Enabled
					_ = c.SaveProviderConfig(cfg)
				}
			})
		}

		if !cfg.Enabled {
			ui.Column(cUI).FillWidth().Padding(18, 20).Radius(theme.RadiusSurface).
				Background(cardBG).Border(1, pal.Hairline).Children(func() {
				renderEnabledHeader()
			})
			return
		}

		// 1. Agent Overview: Header row + Split Card (260px flush left rail + right detail pane).
		ui.Column(cUI).FillWidth().Gap(12).Children(func() {
			renderEnabledHeader()

			ui.Row(cUI).FillWidth().MinHeight(320).Radius(theme.RadiusSurface).
				Background(cardBG).Border(1, pal.Hairline).Clip().Children(func() {
				// Left 260px flush agent_card rail
				ui.Column(cUI).Width(260).Shrink(0).FillHeight().Background(pal.Sidebar).
					BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline).Children(func() {
					total := len(recognized)
					for idx, kind := range recognized {
						k := kind
						enabled := isBackendEnabled(backends, k)
						suffix := backendProbeSuffix(k)
						cardID := "ai-backend-card-" + suffix
						toggleID := "ai-backend-toggle-" + suffix
						isFocused := focused == k
						lbl := backendLabel(k)

						card := ui.Row(cUI.Key(cardID)).FillWidth().Height(50).PaddingX(14).Gap(10).
							AlignItems(ui.Center).Justify(ui.SpaceBetween)
						if idx+1 < total {
							card.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
						}
						if isFocused {
							card.Background(selBG)
						} else if card.Hovered() {
							card.Background(pal.Hover)
						}
						bui.Probe(card, cardID)

						card.Children(func() {
							selBtn := ui.ButtonBase(cUI.Key("focus-" + cardID)).Grow(1).MinWidth(0).FillHeight().Gap(9).
								AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(lbl)
							selBtn.Children(func() {
								ui.Box(cUI).Size(26, 26).Shrink(0).Center().Radius(6).
									Background(cardBG).Border(1, pal.Hairline).Children(func() {
									icons.Brand(cUI, k, 16, pal.Text)
								})
								ui.Text(cUI, lbl).FontSize(14).FontWeight(550).TextColor(pal.Text).
									Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							})
							if selBtn.Clicked() {
								c.mu.Lock()
								c.settingsFocused = k
								c.settingsTestErr = ""
								c.settingsTestOK = false
								c.mu.Unlock()
							}

							on := enabled
							if bui.TogglePill(cUI, toggleID, lbl, &on, false) {
								c.mu.Lock()
								c.settingsFocused = k
								c.mu.Unlock()
								if err := c.SetBackendEnabled(k, on); err != nil {
									c.mu.Lock()
									c.settingsTestErr = err.Error()
									c.mu.Unlock()
								}
							}
						})
					}
				})

				// Right detail pane
				ui.Column(cUI).Grow(1).MinWidth(0).Padding(16, 20).Gap(14).Children(func() {
					if focused == model.BackendBuiltin {
						c.renderBuiltinSettings(cUI, pal, cfg, backends, baseURL, apiKey, showKey, testing, testOK, testErr)
					} else {
						c.renderAgentSettings(cUI, pal, focused, backends, testErr)
					}
				})
			})

			// Hidden programmatic priority probes (matching settings_agent.rs:104-111)
			ui.Row(cUI).Height(0).Clip().Children(func() {
				for _, kind := range recognized {
					k := kind
					pillID := "ai-priority-pill-" + backendProbeSuffix(k)
					pb := ui.ButtonBase(cUI.Key(pillID)).Size(1, 1).Label(pillID)
					bui.Probe(pb, pillID)
					if pb.Clicked() {
						_ = c.PromoteBackend(k)
					}
				}
			})
		})

		// 2. General Features (`普通功能` -> `标注功能`) matching settings_general.rs 1:1.
		annotAnchor := c.renderGeneralFeaturesSection(cUI, pal, cardBG, annotCfg, annotOpen)

		if cfg.Enabled && annotOpen {
			c.renderAnnotationModelPopover(cUI, pal, recognized, backends, annotAnchor)
		}
	})
}

func backendProbeSuffix(k model.BackendKind) string {
	switch k {
	case model.BackendBuiltin:
		return "Builtin"
	case model.BackendCodex:
		return "Codex"
	case model.BackendClaude:
		return "Claude"
	case model.BackendDsh:
		return "Dsh"
	case model.BackendPi:
		return "Pi"
	case model.BackendAgy:
		return "Agy"
	default:
		return string(k)
	}
}

func (c *Controller) renderInlineDisabledBanner(cUI *ui.Context, pal theme.Palette, kind model.BackendKind, opt model.AIBackendAdapterOptions, enabled bool) {
	dark := theme.IsDark()
	bg := ui.Hex("#eef4ff")
	brd := ui.Hex("#c9d8f3")
	fg := ui.Hex("#345498")
	if dark {
		bg = ui.Hex("#1f2b40")
		brd = ui.Hex("#31456a")
		fg = ui.Hex("#9dbcf5")
	}
	banner := ui.Row(cUI.Key("ai-focused-locked")).FillWidth().MinHeight(54).Padding(9, 12).Gap(10).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
		Background(bg).Border(1, brd)
	bui.Probe(banner, "ai-focused-locked")
	banner.Children(func() {
		msg := theme.Tr("当前未在对话栏启用，开启后可在模型切换器中选择", "Disabled in chat. Enable to select in the model switcher")
		if enabled && opt.Locked {
			msg = theme.Tr("该后端配置已锁定", "Backend configuration is locked")
		}
		ui.Row(cUI).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
			icons.Draw(cUI, icons.HelpCircle, 15, pal.MonoIconColors(fg))
			ui.Text(cUI, msg).FontSize(14).TextColor(fg).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		})
		if !enabled {
			enBtn := bui.ActionButton(cUI, "ai-enable-focused", theme.Tr("立即启用", "Enable"), nil, bui.ButtonPrimary, false)
			if enBtn.Clicked() {
				_ = c.SetBackendEnabled(kind, true)
			}
		} else {
			unBtn := bui.ActionButton(cUI, "ai-unlock-focused", theme.Tr("解锁", "Unlock"), nil, bui.ButtonPrimary, false)
			if unBtn.Clicked() {
				_ = c.SetBackendLocked(kind, false)
			}
		}
	})
}

func (c *Controller) renderBuiltinSettings(cUI *ui.Context, pal theme.Palette, cfg model.AIProviderConfig, backends model.AIBackendsConfig, baseURL, apiKey string, showKey, testing, testOK bool, testErr string) {
	cardBG := settingsCardSurface(pal)
	selBG := settingsSelectedSurface(pal)
	enabled := isBackendEnabled(backends, model.BackendBuiltin)
	opt := backends.Adapters[string(model.BackendBuiltin)]

	ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).
		Padding(0, 0, 12, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
		ui.Row(cUI).Gap(8).AlignItems(ui.Center).Children(func() {
			icons.Brand(cUI, model.BackendBuiltin, 18, pal.Text)
			ui.Text(cUI, backendLabel(model.BackendBuiltin)).FontSize(16).FontWeight(600).TextColor(pal.Text)
		})
		testLabel := theme.Tr("连接测试", "Test Connection")
		testIcon := icons.Sparkles
		if testing {
			testLabel = theme.Tr("测试中…", "Testing…")
		} else if testOK {
			testLabel = theme.Tr("连接正常", "Connected")
			testIcon = icons.Check
		}
		testBtn := bui.ActionButton(cUI, "ai-test", testLabel, &testIcon, bui.ButtonSecondary, testing)
		if testBtn.Clicked() && !testing {
			c.runConnectionTestAsync()
		}
	})

	if !enabled || opt.Locked {
		c.renderInlineDisabledBanner(cUI, pal, model.BackendBuiltin, opt, enabled)
	}

	// 3 provider cards (`DeepSeek`, `OpenAI 协议`, `Claude 协议`) with 26x26 icon wells
	ui.Row(cUI).FillWidth().Gap(8).Children(func() {
		for _, p := range providerPresets {
			preset := p
			active := cfg.Provider == preset.ID
			lbl := theme.Tr(preset.Zh, preset.En)
			btn := ui.ButtonBase(cUI.Key("ai-pcard-"+preset.ID)).Grow(1).MinWidth(0).Padding(9, 10).Gap(8).
				AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(lbl)
			if active {
				btn.Background(selBG).Border(1, pal.Accent)
			} else {
				bg := pal.Shell
				if btn.Hovered() {
					bg = pal.Hover
				}
				btn.Background(bg).Border(1, pal.BtnBorder)
			}
			bui.Probe(btn, "ai-pcard-"+preset.ID)
			btn.Children(func() {
				ui.Box(cUI).Size(26, 26).Shrink(0).Center().Radius(6).
					Background(cardBG).Border(1, pal.Hairline).Children(func() {
					icons.Draw(cUI, preset.Icon, 16, pal.MonoIconColors(preset.BrandColor))
				})
				ui.Text(cUI, lbl).FontSize(14).FontWeight(500).TextColor(pal.Text).
					Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			})
			if btn.Clicked() {
				next := cfg
				next.Provider = preset.ID
				next.Protocol = preset.Protocol
				if prof, ok := next.Providers[preset.ID]; ok {
					next.BaseURL = prof.BaseURL
					next.APIKey = prof.APIKey
					next.Model = prof.Model
				} else {
					next.BaseURL = preset.BaseURL
					next.APIKey = ""
					next.Model = ""
				}
				if next.BaseURL == "" {
					next.BaseURL = preset.BaseURL
				}
				_ = c.SaveProviderConfig(next)
			}
		}
	})

	// Base URL (with bottom hairline matching settings_view.rs:89-90)
	ui.Box(cUI).FillWidth().Padding(0, 0, 10, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
		ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, theme.Tr("接口地址", "Base URL")).Width(96).Shrink(0).FontSize(14).TextColor(pal.Text)
			u := baseURL
			in := ui.TextInput(cUI.Key("ai-settings-baseurl"), &u).Grow(1).FontSize(14).Placeholder("https://api.example.com/v1")
			if in.Changed() {
				next := cfg
				next.BaseURL = u
				_ = c.SaveProviderConfig(next)
			}
		})
	})

	// API Key
	ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(cUI, theme.Tr("API 密钥", "API Key")).Width(96).Shrink(0).FontSize(14).TextColor(pal.Text)
		ui.Row(cUI).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			k := apiKey
			in := ui.TextInput(cUI.Key("ai-settings-apikey"), &k).Grow(1).FontSize(14).Placeholder("sk-…")
			if !showKey {
				in.Password()
			}
			if in.Changed() {
				next := cfg
				next.APIKey = k
				_ = c.SaveProviderConfig(next)
			}
			toggleLbl := theme.Tr("显示", "Show")
			if showKey {
				toggleLbl = theme.Tr("隐藏", "Hide")
			}
			tBtn := bui.ActionButton(cUI, "ai-toggle-key", toggleLbl, nil, bui.ButtonSecondary, false)
			if tBtn.Clicked() {
				c.mu.Lock()
				c.settingsShowKey = !c.settingsShowKey
				c.mu.Unlock()
			}
		})
	})

	if testErr != "" {
		ui.Box(cUI).FillWidth().Padding(8, 12).Radius(theme.RadiusControl).
			Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).Children(func() {
			ui.Text(cUI, testErr).FontSize(14).TextColor(pal.Danger)
		})
	}
}

func (c *Controller) renderAgentSettings(cUI *ui.Context, pal theme.Palette, kind model.BackendKind, backends model.AIBackendsConfig, testErr string) {
	enabled := isBackendEnabled(backends, kind)
	opt := backends.Adapters[string(kind)]
	locked := !enabled || opt.Locked

	c.mu.RLock()
	thinking := c.backendThinkingLocked(kind)
	st, hasSt := c.backendStatuses[kind]
	c.mu.RUnlock()
	verTag := formatBackendVersionTag(st, hasSt)

	ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).
		Padding(0, 0, 12, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
		ui.Row(cUI).Gap(8).AlignItems(ui.Center).Children(func() {
			icons.Brand(cUI, kind, 18, pal.Text)
			ui.Text(cUI, backendLabel(kind)).FontSize(15).FontWeight(600).TextColor(pal.Text)
		})
		if verTag != "" {
			ui.Text(cUI, verTag).Font("monospace").FontSize(14).TextColor(pal.TextSecondary).SingleLine()
		}
	})

	if locked {
		c.renderInlineDisabledBanner(cUI, pal, kind, opt, enabled)
	}

	binPath := string(kind)
	if found, ok := resolveCLIBinary(string(kind)); ok {
		binPath = found
	}
	ui.Box(cUI).FillWidth().Padding(0, 0, 10, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
		ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, theme.Tr("可执行文件路径", "Binary Path")).Shrink(0).FontSize(14).TextColor(pal.Text)
			ui.Box(cUI).Height(32).PaddingX(12).Center().Radius(theme.RadiusControl).
				Background(pal.Shell).Border(1, pal.Hairline).MaxWidth(420).Children(func() {
				ui.Text(cUI, binPath).Font("monospace").FontSize(14).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
			})
		})
	})

	ui.Box(cUI).FillWidth().Padding(0, 0, 10, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
		ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).Shrink(0).FontSize(14).TextColor(pal.Text)
			levels := []string{"low", "medium", "high"}
			lvIdx := 2
			for i, lv := range levels {
				if lv == thinking {
					lvIdx = i
				}
			}
			if bui.SegmentedControl(cUI, "ai-agent-thinking", &lvIdx, []bui.SegmentOption{
				{ID: "ai-agent-think-low", Label: "Low"},
				{ID: "ai-agent-think-medium", Label: "Medium"},
				{ID: "ai-agent-think-high", Label: "High"},
			}) {
				c.mu.Lock()
				_ = c.setBackendThinkingLocked(kind, levels[lvIdx])
				c.mu.Unlock()
			}
		})
	})

	ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(cUI, theme.Tr("最大权限", "Full Permissions")).Shrink(0).FontSize(14).TextColor(pal.Text)
		fp := opt.FullPermissions
		if bui.TogglePill(cUI, "ai-agent-full-permissions", theme.Tr("最大权限", "Full Permissions"), &fp, locked) {
			c.mu.Lock()
			_ = c.setBackendFullPermissionsLocked(kind, fp)
			c.mu.Unlock()
		}
	})

	if testErr != "" {
		ui.Text(cUI, testErr).FontSize(14).TextColor(pal.Danger)
	}
}

func thinkingDisplayLabel(lv string) string {
	switch lv {
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	default:
		return "High"
	}
}

func (c *Controller) renderGeneralFeaturesSection(cUI *ui.Context, pal theme.Palette, cardBG ui.Color, annotCfg model.AnnotationConfig, annotOpen bool) ui.Element {
	kind := annotCfg.Backend
	if kind == "" {
		kind = model.BackendBuiltin
	}
	lbl := strings.TrimSpace(annotCfg.Model)
	if lbl == "" {
		lbl = backendLabel(kind)
	}
	thinkLbl := thinkingDisplayLabel(annotCfg.Thinking)

	var trigAnchor ui.Element
	ui.Column(cUI).FillWidth().Gap(12).Children(func() {
		ui.Text(cUI, theme.Tr("普通功能", "General Features")).FontSize(16).FontWeight(600).TextColor(pal.Text)

		ui.Column(cUI).FillWidth().Padding(18, 20).Radius(theme.RadiusSurface).
			Background(cardBG).Border(1, pal.Hairline).Children(func() {
			ui.Row(cUI).FillWidth().MinHeight(36).Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(cUI, theme.Tr("标注功能", "Annotations")).FontSize(14).TextColor(pal.Text)

				trig := ui.ButtonBase(cUI.Key("annotation-model-trigger")).Height(34).PaddingX(12).Gap(8).
					AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
					Label(lbl)
				trigAnchor = trig
				if annotOpen {
					trig.Background(settingsSelectedSurface(pal)).Border(1, pal.Accent)
				} else {
					bg := pal.Shell
					if trig.Hovered() {
						bg = pal.Hover
					}
					trig.Background(bg).Border(1, pal.BtnBorder)
				}
				trig.DrawOver(func(_ *ui.Painter, r ui.Rect) {
					c.mu.Lock()
					c.annotMenuTriggerBox = r
					c.mu.Unlock()
					bui.RecordBounds("annotation-model-trigger", r)
				})
				trig.Children(func() {
					icons.Brand(cUI, kind, 15, pal.Text)
					ui.Text(cUI, lbl).FontSize(14).FontWeight(500).TextColor(pal.Text).SingleLine()
					ui.Text(cUI, "·").FontSize(14).TextColor(pal.TextSecondary)
					ui.Text(cUI, thinkLbl).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
					chev := icons.ChevronDown
					if annotOpen {
						chev = icons.ChevronUp
					}
					icons.Draw(cUI, chev, 14, pal.MonoIconColors(pal.TextSecondary))
				})
				if trig.Clicked() {
					c.mu.Lock()
					c.annotMenuOpen = !c.annotMenuOpen
					nextOpen := c.annotMenuOpen
					if nextOpen {
						c.annotMenuSearch = ""
						if c.annotCfg.Backend != "" {
							c.annotMenuBackend = c.annotCfg.Backend
						} else {
							c.annotMenuBackend = model.BackendBuiltin
						}
					}
					targetKind := c.annotMenuBackend
					c.mu.Unlock()
					if nextOpen {
						c.refreshModelsAsync(targetKind)
					}
				}
			})
		})
	})
	return trigAnchor
}

func (c *Controller) renderAnnotationModelPopover(cUI *ui.Context, pal theme.Palette, recognized []model.BackendKind, backends model.AIBackendsConfig, anchor ui.Element) {
	c.mu.RLock()
	trigger := c.annotMenuTriggerBox
	activeKind := c.annotMenuBackend
	annotCfg := c.annotCfg
	query := strings.ToLower(strings.TrimSpace(c.annotMenuSearch))
	choices := append([]ModelOption(nil), c.backendModels[activeKind]...)
	status := c.modelStatus[activeKind]
	enabledOrder := c.visibleBackendsLocked()
	c.mu.RUnlock()

	if activeKind == "" {
		activeKind = model.BackendBuiltin
	}
	_ = recognized
	_ = backends
	winW, winH := cUI.Size()
	if winW <= 0 {
		winW = 1100
	}
	if winH <= 0 {
		winH = 760
	}
	if trigger.W <= 0 {
		trigger = ui.Rect{X: max(winW-220, 16), Y: max(winH-120, 80), W: 180, H: 34}
	}
	popW := min(float32(520), max(winW-16, 300))
	spec := bui.PlaceAbove(trigger).WithAlignX(bui.AlignX{Kind: bui.AlignRight}).WithGap(6).WithMargin(8)
	maxH := bui.MaxHeightForSpec(spec, winW, winH, 256)
	popH := min(float32(256), maxH)
	x, y := bui.ResolveOrigin(spec, popW, popH, winW, winH)

	ui.Overlay(cUI, func() {
		panel := ui.Row(cUI.Key("annotation-model-menu")).Absolute().Left(x).Top(y).Width(popW).Height(popH).
			Radius(theme.RadiusSurface).Background(pal.Menu).Border(1, pal.MenuBorder).
			Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.22)).Clip().
			Transition(bui.PopupTransition())
		bui.Probe(panel, "annotation-model-menu")

		panel.Children(func() {
			// 1. Left enabled backend icon rail
			ui.Column(cUI).Width(46).Shrink(0).FillHeight().Padding(6).Gap(6).
				Background(pal.Shell).BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline).
				AlignItems(ui.Center).Children(func() {
				for _, b := range enabledOrder {
					bk := b
					id := "annotation-backend-" + backendProbeSuffix(bk)
					sel := bk == activeKind
					btn := ui.ButtonBase(cUI.Key(id)).Size(34, 34).Center().Radius(8).
						Cursor(ui.CursorPointer).Label(backendLabel(bk)).Tooltip(backendLabel(bk))
					if sel {
						btn.Background(settingsSelectedSurface(pal)).Border(1, pal.Accent)
					} else {
						if btn.Hovered() {
							btn.Background(pal.Hover)
						}
						btn.Border(1, ui.Transparent)
					}
					bui.Probe(btn, id)
					btn.Children(func() {
						icons.Brand(cUI, bk, 16, pal.Text)
					})
					if btn.Clicked() {
						c.mu.Lock()
						c.annotMenuBackend = bk
						c.annotMenuSearch = ""
						c.mu.Unlock()
						c.refreshModelsAsync(bk)
					}
				}
			})

			// 2. Center model search + list
			ui.Column(cUI).Grow(1).MinWidth(0).FillHeight().Children(func() {
				ui.Row(cUI).FillWidth().Height(40).PaddingX(10).Gap(8).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
					icons.Draw(cUI, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
					in := ui.TextInputBase(cUI.Key("annotation-model-search"), &c.annotMenuSearch).
						Grow(1).Height(28).Padding(0).FontSize(theme.FontBody).TextColor(pal.Text).
						CaretColor(pal.Accent).CaretWidth(2).
						Placeholder(theme.Tr("搜索模型…", "Search models…")).Bind(&c.annotSearchHandle).AutoFocus()
					bui.Probe(in, "annotation-model-search")
					in.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEscape {
							c.mu.Lock()
							c.annotMenuOpen = false
							c.mu.Unlock()
							return true
						}
						return false
					})
				})

				ui.Scroll(cUI.Key("annotation-model-list")).TrackScroll(&c.annotMenuScroll).
					Grow(1).MinHeight(0).FillWidth().Padding(6).Gap(2).Children(func() {
					matched := 0
					for idx, m := range choices {
						opt := m
						if query != "" && !strings.Contains(strings.ToLower(opt.Value), query) && !strings.Contains(strings.ToLower(opt.Label), query) {
							continue
						}
						matched++
						isAct := annotCfg.Backend == activeKind && annotCfg.Model == opt.Value
						rowID := fmt.Sprintf("annotation-model-%d", idx)
						row := ui.ButtonBase(cUI.Key(rowID)).FillWidth().Height(34).PaddingX(10).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(opt.Label)
						if isAct {
							row.Background(settingsSelectedSurface(pal)).Border(1, pal.Accent)
						} else {
							if row.Hovered() {
								row.Background(pal.Hover)
							}
							row.Border(1, ui.Transparent)
						}
						bui.Probe(row, rowID)
						row.Children(func() {
							icons.Brand(cUI, activeKind, 15, pal.Text)
							ui.Text(cUI, opt.Label).FontSize(14).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							if isAct {
								icons.Draw(cUI, icons.Check, 14, pal.MonoIconColors(pal.Accent))
							}
						})
						if row.Clicked() {
							c.mu.Lock()
							next := c.annotCfg
							next.Backend = activeKind
							if activeKind == model.BackendBuiltin && c.providerCfg.Provider != "" {
								next.Provider = c.providerCfg.Provider
							}
							next.Model = opt.Value
							_ = c.saveAnnotationConfigLocked(next)
							c.annotMenuOpen = false
							c.mu.Unlock()
						}
					}
					if matched == 0 {
						msg := theme.Tr("暂无可选模型", "No models available")
						if status == "loading" {
							msg = theme.Tr("正在获取模型…", "Loading models…")
						}
						ui.Box(cUI).Padding(8).Children(func() {
							ui.Text(cUI, msg).FontSize(14).TextColor(pal.TextSecondary)
						})
					}
				})
			})

			// 3. Right Thinking Level column
			ui.Column(cUI).Width(140).Shrink(0).FillHeight().
				BorderWidth(0, 0, 0, 1).BorderColor(pal.Hairline).Children(func() {
				ui.Row(cUI).FillWidth().Height(40).PaddingX(10).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
					ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(14).TextColor(pal.TextSecondary)
				})
				ui.Column(cUI).FillWidth().Padding(8).Gap(4).Children(func() {
					for _, lv := range []struct{ id, label string }{
						{"low", "Low"},
						{"medium", "Medium"},
						{"high", "High"},
					} {
						level := lv.id
						isAct := annotCfg.Thinking == level || (annotCfg.Thinking == "" && level == "high")
						btnID := "annotation-thinking-" + level
						btn := ui.ButtonBase(cUI.Key(btnID)).FillWidth().Height(32).PaddingX(10).
							AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusControl).
							Cursor(ui.CursorPointer).Label(lv.label)
						if isAct {
							btn.Background(settingsSelectedSurface(pal)).Border(1, pal.Accent)
						} else {
							if btn.Hovered() {
								btn.Background(pal.Hover)
							}
							btn.Border(1, ui.Transparent)
						}
						bui.Probe(btn, btnID)
						btn.Children(func() {
							ui.Text(cUI, lv.label).FontSize(14).FontWeight(500).TextColor(pal.Text)
							if isAct {
								icons.Draw(cUI, icons.Check, 14, pal.MonoIconColors(pal.Accent))
							}
						})
						if btn.Clicked() {
							c.mu.Lock()
							next := c.annotCfg
							next.Thinking = level
							_ = c.saveAnnotationConfigLocked(next)
							c.mu.Unlock()
						}
					}
				})
			})
		})

		if panel.OverlayShortcut(0, ui.KeyEscape) || ((!anchor.Valid() || !anchor.PressedInside()) && panel.PressedOutside()) {
			c.mu.Lock()
			c.annotMenuOpen = false
			c.mu.Unlock()
		}
	})
}

func (c *Controller) runConnectionTestAsync() {
	c.mu.Lock()
	if c.settingsTesting {
		c.mu.Unlock()
		return
	}
	c.settingsTesting = true
	c.settingsTestOK = false
	c.settingsTestErr = ""
	c.mu.Unlock()
	c.requestRedraw()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.TestConnection(ctx)
	}()
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func removeString(list []string, target string) []string {
	out := list[:0]
	for _, s := range list {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}

func boolToBorder(v bool) float32 {
	if v {
		return 1
	}
	return 0
}
