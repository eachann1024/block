package ai

import (
	"context"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"block/internal/fuzzy"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/tarot"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	mdparser "github.com/egoist/mygo/ui/markdown/parser"
)

// RenderPanel renders the complete AI assistant side/fullscreen panel.
func (c *Controller) RenderPanel(cUI *ui.Context) {
	c.mu.Lock()
	if !c.providerCfg.Enabled {
		c.mu.Unlock()
		return
	}
	c.services = cUI.Services()
	shouldProbe := !c.panelProbedOnce && !c.mockResponder
	if shouldProbe {
		c.panelProbedOnce = true
	}
	initKind := c.selectedBackendLocked()
	c.mu.Unlock()
	if shouldProbe {
		c.probeAllBackendsAsync()
		c.refreshModelsAsync(initKind)
	}

	c.refreshPickerIfNeeded()
	c.flushPendingClipboard(cUI.Services())

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
		if theme.IsTarotMode() {
			panel.BorderWidth(0, 0, 0, 1).BorderColor(theme.Tarot().GoldBorderHair)
		}
	}
	if b := panel.Bounds(); b.W > 0 {
		c.mu.Lock()
		c.panelBox = b
		c.mu.Unlock()
	}
	bui.Probe(panel, "ai-chat-panel")

	c.mu.RLock()
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	anyOverlayOpen := c.historyOpen || c.deckOpen || c.lightboxOpen
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

	c.renderAttachmentLightbox(cUI)
}

func (c *Controller) renderPanelHeader(cUI *ui.Context, pal theme.Palette, fullscreen bool, blocked bool, panelW float32) {
	c.mu.RLock()
	title := theme.Tr("新会话", "New Chat")
	if conv := c.findConvLocked(c.currentID); conv != nil {
		title = displayConversationTitle(*conv)
	}
	histOpen := c.historyOpen
	c.mu.RUnlock()

	leftPad := theme.Space16
	rightPad := theme.Space12
	if fullscreen {
		tb := cUI.TitleBar()
		if theme.IsMacOS() {
			leftPad = max(theme.TrafficLeadingMacOS, tb.Left+theme.Space12)
			rightPad = theme.TitleBarActionTrailing()
		} else if tb.Right > 0 {
			rightPad = tb.Right + theme.Space8
		}
	}

	header := ui.Row(cUI.Key("ai-header")).FillWidth().Height(theme.TitleBarHeight).Shrink(0).
		Padding(0, rightPad, 0, leftPad).Gap(theme.Space4).
		AlignItems(ui.Center).Background(pal.Paper)
	if fullscreen {
		header.DragWindow()
	}

	var histAnchor ui.Element
	header.Children(func() {
		ui.Text(cUI, title).
			FontSize(theme.FontSmall).
			LineHeight(theme.LineSmall / theme.FontSmall).
			FontWeight(theme.WeightControl).
			TextColor(pal.TextSecondary).
			MaxWidth(theme.TitleBarTitleMaxWidth).
			Shrink(1).
			MinWidth(0).
			SingleLine().
			Ellipsis("…")
		ui.Spacer(cUI)

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
		trigger = ui.Rect{
			X: max(panelBox.X+panelBox.W-42, 8),
			Y: panelBox.Y + (theme.TitleBarHeight-theme.ChromeBoxSize)/2,
			W: theme.ChromeBoxSize,
			H: theme.ChromeBoxSize,
		}
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
	rowH := float32(32)
	scrimBox := ui.Rect{X: panelBox.X, Y: panelBox.Y + theme.TitleBarHeight, W: panelBox.W, H: max(panelBox.H-theme.TitleBarHeight, 0)}
	if fullscreen {
		// Full-screen mode docks history as a full-height right drawer instead of a small popover.
		menuW = min(theme.AIHistoryDrawerWidth, max(panelBox.W-2*theme.Space8, 1))
		x = panelBox.X + panelBox.W - menuW - theme.Space8
		y = panelBox.Y + theme.TitleBarHeight
		maxH = max(panelBox.H-theme.TitleBarHeight-theme.Space8, 48)
		rowH = theme.AIHistoryDrawerRowHeight
	}

	ui.Overlay(cUI, func() {
		scrim := ui.Box(cUI.Key("ai-history-scrim")).Absolute().
			Left(scrimBox.X).Top(scrimBox.Y).Width(scrimBox.W).Height(scrimBox.H).
			Background(pal.Scrim()).
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
			Width(menuW).MaxHeight(maxH)
		if fullscreen {
			panel.Height(maxH)
		}
		panel = bui.Elevate(panel.Padding(theme.MenuPadding).Gap(theme.Space2).
			Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).
			Clip().
			Transition(probeSafeTransition(bui.PopoverSpringTransition())), theme.ElevationPopover)
		type histMenuCursorKey struct{}
		histCursor := ui.Local(panel, histMenuCursorKey{}, func() int { return -1 })
		bui.ObserveMenuPointer(cUI, panel, histCursor)
		if len(convs) > 0 {
			if *histCursor >= len(convs) {
				*histCursor = len(convs) - 1
			}
			down := panel.OverlayShortcut(0, ui.KeyDown)
			right := panel.OverlayShortcut(0, ui.KeyRight)
			up := panel.OverlayShortcut(0, ui.KeyUp)
			left := panel.OverlayShortcut(0, ui.KeyLeft)
			enter := panel.OverlayShortcut(0, ui.KeyEnter)
			switch {
			case down || right:
				*histCursor = (*histCursor + 1 + len(convs)) % len(convs)
			case up || left:
				if *histCursor < 0 {
					*histCursor = len(convs) - 1
				} else {
					*histCursor = (*histCursor - 1 + len(convs)) % len(convs)
				}
			case enter && *histCursor >= 0 && *histCursor < len(convs):
				c.SelectConversation(convs[*histCursor].ID)
				return
			}
		}
		bui.Probe(panel, "ai-history-list")

		panel.Children(func() {
			if len(convs) == 0 {
				ui.Text(cUI, theme.Tr("暂无历史会话", "No chat history")).
					FontSize(theme.FontBody).TextColor(pal.TextSecondary).Padding(theme.Space12)
				return
			}

			bui.Scroll(cUI.Key("ai-history-scroll"), &c.historyScroll).
				FillWidth().MaxHeight(max(bui.MenuInnerMaxHeight(maxH), 48)).Children(func() {
				ui.Column(cUI).FillWidth().Gap(2).Children(func() {
					for idx, conv := range convs {
						convID := conv.ID
						active := convID == curID
						highlighted := idx == *histCursor
						rowID := fmt.Sprintf("conv-%d", convID)
						row := ui.Row(cUI.Key(rowID)).FillWidth().Height(rowH).PaddingX(8).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusMD).
							Transition(probeSafeTransition(bui.CascadeItemTransition(idx, false)))
						bui.RevealOnHighlight(row, highlighted)
						hovered := highlighted || (*histCursor < 0 && row.Hovered())
						ais := pal.AISurface()
						bg := ui.Transparent
						if active {
							bg = ais.SelectedBG
						} else if hovered {
							bg = ais.HoverBG
						}
						bui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
						bui.TactileCard(row, hovered, false, false, false)
						bui.Probe(row, rowID)
						row.Children(func() {
							convTitle := displayConversationTitle(conv)
							selBtn := ui.ButtonBase(cUI.Key("sel-" + rowID)).Grow(1).MinWidth(0).FillHeight().
								AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer).Label(convTitle)
							selBtn.Children(func() {
								ui.Text(cUI, convTitle).FontSize(theme.FontBody).TextColor(pal.Text).SingleLine().Ellipsis("…")
							})
							if selBtn.Clicked() {
								c.SelectConversation(convID)
							}

							ui.Text(cUI, backendLabel(conv.Backend)).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine().Shrink(0)

							delID := fmt.Sprintf("conv-del-%d", convID)
							delBtn := bui.IconButton(cUI, icons.Trash, bui.IconButtonOptions{
								ID:       delID,
								Label:    theme.Tr("删除会话 ", "Delete ") + convTitle,
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
		c.draftAttachments = nil
		c.retryDraft = nil
		c.composerEpoch++
	}
	delete(c.tabDrafts, id)
	before := len(c.deferredDrafts)
	c.deferredDrafts = slices.DeleteFunc(c.deferredDrafts, func(d model.SentDraft) bool {
		return d.Conversation == id
	})
	if len(c.deferredDrafts) != before {
		c.persistFailedDraftsLocked()
	}
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
	title := theme.Tr("先完成 AI 配置", "Configure AI Assistant")
	cta := theme.Tr("前往设置", "Open AI Settings")
	mascot := icons.MascotAIGreeting
	if !enabled {
		title = theme.Tr("先启用 AI 助手", "Enable AI Assistant")
		mascot = icons.MascotAIDisabled
	}
	width := theme.EmptyActionWidth
	if fullscreen {
		width = theme.EmptyStateWidth
	}
	cUI.Key("ai-empty-cover")
	bui.EmptyStage(cUI, ui.Transparent, width, func() {
		icons.Mascot(cUI, mascot, theme.EmptyMascotCompactSize)
		bui.EmptyHeading(cUI, title).Margin(theme.Space16, 0, 0, 0)
		ui.Box(cUI).Margin(theme.Space16, 0, 0, 0).Children(func() {
			lead := icons.ArrowRight
			btn := bui.ActionButton(cUI, "ai-cover-settings", cta, &lead, bui.ButtonPrimary, false)
			if btn.Clicked() && c.host != nil {
				c.host.OpenSettings("ai")
			}
		})
	})
}

func (c *Controller) renderEmptySuggestions(cUI *ui.Context, pal theme.Palette, fullscreen bool, minH float32) {
	fontSize := theme.FontTitle
	lineHeight := theme.LineTitle
	if fullscreen {
		fontSize = theme.FontHeading
		lineHeight = theme.LineHeading
	}
	col := ui.Column(cUI.Key("ai-empty-state")).FillWidth().Padding(theme.Space24, theme.Space16).Center()
	if minH > 0 {
		col.MinHeight(minH)
	}
	col.Children(func() {
		ui.Text(cUI, theme.Tr("我们来做些什么？", "What shall we work on?")).
			FontSize(fontSize).FixedLineHeight(lineHeight).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).TextAlign(ui.Center)
	})
}

type mdPaletteKey struct {
	paper, text, textSecondary, accent, codeBlockBG, hairline ui.Color
}

type cachedMsgEntry struct {
	role       string
	content    string
	toolCallID string
	procLen    int
	srcLen     int
	attLen     int
	planStatus string
	qSubmitted bool
	appDec     model.ApprovalDecision
	appFailed  bool
	appErr     string
	msg        model.ChatMessage
}

type cachedTurnCard struct {
	msgIdx   int
	plan     *model.BatchPlan
	question *model.QuestionCard
	approval *model.ApprovalCard
}

type cachedReply struct {
	replyIdx int
	content  string
}

type cachedTurn struct {
	key                     string
	startIdx                int
	endIdx                  int
	hasUser                 bool
	userText                string
	userAttachments         []model.Attachment
	userAttachmentsSeparate bool
	steps                   []model.ProcessStep
	notes                   []string
	sources                 []model.SourceNote
	hasPendingPlan          bool
	hasInteractive          bool
	outcome                 string
	cards                   []cachedTurnCard
	replies                 []cachedReply
	nextReplyIdx            int
	estHeight               float32
}

type cachedParsedMarkdown struct {
	blockCount int
	lineCount  int
}

type chatStreamCache struct {
	convID         uint64
	lastWidth      float32
	lastFullscreen bool
	msgs           []cachedMsgEntry
	turns          []cachedTurn
	turnHeights    map[string]float32
	mdMeta         map[string]cachedParsedMarkdown
}

func newChatStreamCache() *chatStreamCache {
	return &chatStreamCache{
		turnHeights: make(map[string]float32),
		mdMeta:      make(map[string]cachedParsedMarkdown),
	}
}

func cloneSingleMessage(m model.ChatMessage) model.ChatMessage {
	cp := m
	if m.Process != nil {
		cp.Process = append([]model.ProcessStep(nil), m.Process...)
	}
	if m.Sources != nil {
		cp.Sources = append([]model.SourceNote(nil), m.Sources...)
	}
	if m.Refs != nil {
		cp.Refs = append([]string(nil), m.Refs...)
	}
	if m.Skills != nil {
		cp.Skills = append([]string(nil), m.Skills...)
	}
	if m.Attachments != nil {
		cp.Attachments = slices.Clone(m.Attachments)
	}
	if m.Plan != nil {
		p := *m.Plan
		p.Ops = append([]model.PlanOp(nil), m.Plan.Ops...)
		if m.Plan.Expected != nil {
			p.Expected = make(map[string]string, len(m.Plan.Expected))
			for k, v := range m.Plan.Expected {
				p.Expected[k] = v
			}
		}
		cp.Plan = &p
	}
	if m.Question != nil {
		q := *m.Question
		q.Questions = append([]model.QuestionItem(nil), m.Question.Questions...)
		cp.Question = &q
	}
	if m.Approval != nil {
		a := *m.Approval
		cp.Approval = &a
	}
	return cp
}

func msgFingerprintMatches(cached cachedMsgEntry, m model.ChatMessage) bool {
	if cached.role != m.Role || cached.content != m.Content || cached.toolCallID != m.ToolCallID ||
		cached.procLen != len(m.Process) || cached.srcLen != len(m.Sources) || cached.attLen != len(m.Attachments) {
		return false
	}
	planSt := ""
	if m.Plan != nil {
		planSt = m.Plan.Status
	}
	if cached.planStatus != planSt {
		return false
	}
	qSub := m.Question != nil && m.Question.Submitted
	if cached.qSubmitted != qSub {
		return false
	}
	var appDec model.ApprovalDecision
	var appFailed bool
	var appErr string
	if m.Approval != nil {
		appDec = m.Approval.Decision
		appFailed = m.Approval.Failed
		appErr = m.Approval.ErrorText
	}
	return cached.appDec == appDec && cached.appFailed == appFailed && cached.appErr == appErr
}

func makeCachedMsgEntry(m model.ChatMessage) cachedMsgEntry {
	planSt := ""
	if m.Plan != nil {
		planSt = m.Plan.Status
	}
	qSub := m.Question != nil && m.Question.Submitted
	var appDec model.ApprovalDecision
	var appFailed bool
	var appErr string
	if m.Approval != nil {
		appDec = m.Approval.Decision
		appFailed = m.Approval.Failed
		appErr = m.Approval.ErrorText
	}
	return cachedMsgEntry{
		role:       m.Role,
		content:    m.Content,
		toolCallID: m.ToolCallID,
		procLen:    len(m.Process),
		srcLen:     len(m.Sources),
		attLen:     len(m.Attachments),
		planStatus: planSt,
		qSubmitted: qSub,
		appDec:     appDec,
		appFailed:  appFailed,
		appErr:     appErr,
		msg:        cloneSingleMessage(m),
	}
}

func (sc *chatStreamCache) parseMarkdownMeta(src string) cachedParsedMarkdown {
	if meta, ok := sc.mdMeta[src]; ok {
		return meta
	}
	if len(sc.mdMeta) >= 256 {
		clear(sc.mdMeta)
	}
	doc := mdparser.Parse(src)
	blocks := 0
	lines := 0
	for range doc.Children(0) {
		blocks++
	}
	n := doc.Len()
	for i := 1; i < n; i++ {
		lines += max(doc.LineCount(i), 1)
	}
	meta := cachedParsedMarkdown{blockCount: blocks, lineCount: max(lines, 1)}
	sc.mdMeta[src] = meta
	return meta
}

func (sc *chatStreamCache) syncLocked(found *model.Conversation) {
	if found == nil {
		sc.convID = 0
		sc.msgs = sc.msgs[:0]
		sc.turns = sc.turns[:0]
		clear(sc.turnHeights)
		return
	}
	if sc.convID != found.ID {
		sc.convID = found.ID
		sc.msgs = sc.msgs[:0]
		sc.turns = sc.turns[:0]
		clear(sc.turnHeights)
	}
	messages := found.Messages
	sharedLen := min(len(sc.msgs), len(messages))
	dirtyFrom := 0
	for dirtyFrom < sharedLen && msgFingerprintMatches(sc.msgs[dirtyFrom], messages[dirtyFrom]) {
		dirtyFrom++
	}
	if dirtyFrom == len(sc.msgs) && dirtyFrom == len(messages) {
		return
	}
	if len(messages) < len(sc.msgs) {
		clear(sc.msgs[len(messages):])
		sc.msgs = sc.msgs[:len(messages)]
	}
	for i := dirtyFrom; i < len(messages); i++ {
		m := messages[i]
		if i < len(sc.msgs) && msgFingerprintMatches(sc.msgs[i], m) {
			continue
		}
		entry := makeCachedMsgEntry(m)
		if i < len(sc.msgs) {
			sc.msgs[i] = entry
		} else {
			sc.msgs = append(sc.msgs, entry)
		}
	}
	// Include the preceding turn at a boundary: a changed or new message
	// may now belong to that turn instead of starting a new one.
	rebuildFrom := sort.Search(len(sc.turns), func(i int) bool {
		return sc.turns[i].endIdx >= dirtyFrom
	})
	start := 0
	replyIdx := 0
	if rebuildFrom > 0 {
		prev := &sc.turns[rebuildFrom-1]
		start = prev.endIdx
		replyIdx = prev.nextReplyIdx
	}
	clear(sc.turns[rebuildFrom:])
	sc.turns = sc.turns[:rebuildFrom]
	for start < len(sc.msgs) {
		end := start + 1
		for end < len(sc.msgs) && sc.msgs[end].msg.Role != "user" {
			end++
		}
		turnKey := fmt.Sprintf("%d-%d", found.ID, start)
		ct := cachedTurn{
			key:      turnKey,
			startIdx: start,
			endIdx:   end,
		}
		if sc.msgs[start].msg.Role == "user" {
			ct.hasUser = true
			ct.userText = sc.msgs[start].msg.Content
			ct.userAttachments = sc.msgs[start].msg.Attachments
			ct.userAttachmentsSeparate = sc.msgs[start].msg.AttachmentsSeparate
		}

		var turnSlice []model.ChatMessage
		for idx := start; idx < end; idx++ {
			m := sc.msgs[idx].msg
			turnSlice = append(turnSlice, m)
			if m.Role == "turn_outcome" {
				ct.outcome = m.Content
			}
			if m.Plan != nil || m.Question != nil || m.Approval != nil {
				ct.cards = append(ct.cards, cachedTurnCard{
					msgIdx:   idx,
					plan:     m.Plan,
					question: m.Question,
					approval: m.Approval,
				})
				if m.Plan != nil && m.Plan.Status == "pending" {
					ct.hasPendingPlan = true
					ct.hasInteractive = true
				}
				if m.Question != nil && !m.Question.Submitted {
					ct.hasInteractive = true
				}
				if m.Approval != nil && (m.Approval.Decision == model.ApprovalPending || m.Approval.Failed) {
					ct.hasInteractive = true
				}
			}
		}

		ct.steps, ct.notes = collectTurnProcess(turnSlice, false, "", nil)
		ct.sources = collectTurnSources(turnSlice)

		estH := float32(0)
		if ct.hasUser {
			estH += 44 + estimateAttachmentsHeight(ct.userAttachments)
		}
		if len(ct.steps) > 0 {
			estH += 48
		}
		if len(ct.cards) > 0 {
			estH += float32(len(ct.cards)) * 96
		}
		if !ct.hasPendingPlan {
			for idx := start; idx < end; idx++ {
				m := sc.msgs[idx].msg
				if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
					meta := sc.parseMarkdownMeta(m.Content)
					ct.replies = append(ct.replies, cachedReply{
						replyIdx: replyIdx,
						content:  m.Content,
					})
					estH += float32(meta.lineCount)*22 + float32(meta.blockCount)*12 + 30
					replyIdx++
				}
			}
		}
		if len(ct.sources) > 0 {
			estH += 40
		}
		ct.nextReplyIdx = replyIdx
		ct.estHeight = max(estH, 40)
		sc.turns = append(sc.turns, ct)
		start = end
	}
}

func (c *Controller) renderMessageStream(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.Lock()
	if c.streamCache == nil {
		c.streamCache = newChatStreamCache()
	}
	prevConvID := c.streamCache.convID
	found := c.findConvLocked(c.currentID)
	hasConv := found != nil
	c.streamCache.syncLocked(found)
	turns := c.streamCache.turns
	turnHeights := c.streamCache.turnHeights
	panelW := c.panelWidth
	if !fullscreen && c.renderWidth > 0 {
		panelW = c.renderWidth
	}
	if c.streamCache.lastWidth != panelW || c.streamCache.lastFullscreen != fullscreen {
		c.streamCache.lastWidth = panelW
		c.streamCache.lastFullscreen = fullscreen
		clear(turnHeights)
	}
	streaming := c.streaming
	now := cUI.Now()
	if c.tarotWasStreaming && !streaming {
		c.tarotGlintAt = now
	}
	c.tarotWasStreaming = streaming
	tarotGlintProg := float32(0)
	if !streaming && !c.tarotGlintAt.IsZero() {
		if el := now.Sub(c.tarotGlintAt).Seconds(); el >= 0 && el < 1.3 {
			tarotGlintProg = float32(el / 1.3)
		}
	}
	partial := c.partialText.String()
	var partialSteps []model.ProcessStep
	if streaming && len(c.partialSteps) > 0 {
		partialSteps = append([]model.ProcessStep(nil), c.partialSteps...)
	}
	convID := c.currentID
	if prevConvID != convID {
		if convID != 0 {
			c.messagesScroll = ui.ScrollState{Y: math.MaxFloat32}
			c.scrollToBottom = true
		} else {
			c.messagesScroll = ui.ScrollState{}
			c.scrollToBottom = false
		}
	}
	needScrollBottom := c.scrollToBottom && convID != 0
	if needScrollBottom {
		c.messagesScroll.X = 0
		c.messagesScroll.Y = math.MaxFloat32
	}
	expandedSnap := make(map[string]bool, len(turns))
	for i := range turns {
		k := turns[i].key
		if c.expandedProcess[k] || c.expandedSources[k] {
			expandedSnap[k] = true
		}
	}
	c.mu.Unlock()

	scroll := bui.Scroll(cUI.Key("ai-chat-scroll"), &c.messagesScroll).FillWidth().Grow(1)
	bui.Probe(scroll, "ai-chat-scroll")
	scrollH := scroll.Bounds().H
	if needScrollBottom {
		if scrollH > 0 {
			c.mu.Lock()
			if c.currentID == convID {
				c.scrollToBottom = false
			}
			c.mu.Unlock()
		} else {
			c.requestRedraw()
		}
	}
	scrollY := c.messagesScroll.Y
	overscan := max(scrollH*1.5, 600)
	visTop := scrollY - overscan
	visBot := scrollY + scrollH + overscan

	scroll.Children(func() {
		outer := ui.Column(cUI).FillWidth().Padding(16).Gap(16).Selectable()
		if fullscreen {
			outer.MaxWidth(720).Margin(0, ui.Auto)
		}
		outer.Children(func() {
			if !hasConv || (len(turns) == 0 && !streaming) {
				c.renderEmptySuggestions(cUI, pal, fullscreen, max(scrollH-32, 0))
				return
			}

			const virtualizeThreshold = 20
			curY := float32(16)
			for ti := range turns {
				t := &turns[ti]
				turnStreaming := streaming && ti == len(turns)-1

				h := turnHeights[t.key]
				canVirtualize := len(turns) > virtualizeThreshold &&
					scrollH > 0 &&
					ti < len(turns)-2 &&
					!turnStreaming &&
					!t.hasInteractive &&
					!expandedSnap[t.key] &&
					(h > 0 || (scrollY > overscan && ti < len(turns)-12))
				if canVirtualize {
					phH := h
					if phH <= 0 {
						phH = t.estHeight
					}
					yBottom := curY + phH
					if yBottom < visTop || curY > visBot {
						ui.Box(cUI.Key("ai-turn-ph-" + t.key)).FillWidth().Height(phH)
						curY = yBottom + 16
						continue
					}
				}

				turnWrap := ui.Column(cUI.Key("ai-turn-" + t.key)).FillWidth().Gap(16)
				turnWrap.Children(func() {
					if t.hasUser {
						c.renderUserMessage(cUI, pal, convID, t.startIdx, t.userText, t.userAttachments, t.userAttachmentsSeparate)
					}

					if theme.IsTarotMode() && ti == len(turns)-1 && (turnStreaming || tarotGlintProg > 0) {
						tarot.RenderAIDevelopingCard(cUI, turnStreaming, tarotGlintProg)
					}

					steps := t.steps
					notes := t.notes
					if turnStreaming {
						steps, notes = appendStreamingProcess(steps, notes, partial, partialSteps)
					}
					if len(steps) > 0 {
						c.renderProcessCard(cUI, pal, t.key, t.outcome, steps, notes, turnStreaming)
					}

					for _, card := range t.cards {
						if card.plan != nil {
							c.renderPlanCard(cUI, pal, convID, card.msgIdx, card.plan)
						}
						if card.question != nil {
							c.renderQuestionCard(cUI, pal, convID, card.msgIdx, card.question)
						}
						if card.approval != nil {
							c.renderApprovalCard(cUI, pal, convID, card.msgIdx, card.approval)
						}
					}

					if !t.hasPendingPlan {
						nextReplyIdx := 0
						for _, rep := range t.replies {
							c.renderAssistantReply(cUI, pal, convID, rep.replyIdx, rep.content, !turnStreaming, false)
							nextReplyIdx = rep.replyIdx + 1
						}
						if turnStreaming && strings.TrimSpace(partial) != "" {
							c.renderAssistantReply(cUI, pal, convID, nextReplyIdx, partial, false, true)
						}
					}

					if len(t.sources) > 0 {
						c.renderSourcesCard(cUI, pal, t.key, t.sources)
					}
				})
				if b := turnWrap.Bounds(); b.H > 0 {
					turnHeights[t.key] = b.H
					curY += b.H + 16
				} else if h > 0 {
					curY += h + 16
				} else {
					curY += t.estHeight + 16
				}
			}
		})
	})
}

func appendStreamingProcess(baseSteps []model.ProcessStep, baseNotes []string, partial string, partialSteps []model.ProcessStep) ([]model.ProcessStep, []string) {
	steps := make([]model.ProcessStep, len(baseSteps), len(baseSteps)+len(partialSteps)+1)
	copy(steps, baseSteps)
	notes := make([]string, len(baseNotes), len(baseNotes)+len(partialSteps))
	copy(notes, baseNotes)
	seen := make(map[string]bool, len(notes))
	for _, n := range notes {
		seen[n] = true
	}
	for _, st := range partialSteps {
		steps = append(steps, st)
		if ref := strings.TrimSpace(st.FileRef); ref != "" && !seen[ref] {
			seen[ref] = true
			notes = append(notes, ref)
		}
	}
	lbl := theme.Tr("思考中", "Thinking")
	det := theme.Tr("等待模型响应…", "Waiting for model response…")
	if strings.TrimSpace(partial) != "" {
		lbl = theme.Tr("正在生成回答", "Generating answer")
		det = latestStreamLine(partial)
	}
	steps = append(steps, model.ProcessStep{
		ID:     "streaming",
		Kind:   model.ToolKindThink,
		Title:  lbl,
		Detail: det,
		Done:   false,
	})
	return steps, notes
}

func (c *Controller) renderUserMessage(cUI *ui.Context, pal theme.Palette, convID uint64, idx int, text string, atts []model.Attachment, separate bool) {
	ais := pal.AISurface()
	cleanText := displayUserMessageText(text, atts, separate)
	hasText := strings.TrimSpace(cleanText) != ""
	ui.Row(cUI.Key(fmt.Sprintf("user-row-%d-%d", convID, idx))).FillWidth().Justify(ui.End).
		Transition(probeSafeTransition(bui.CascadeItemTransition(idx, true))).Children(func() {
		ui.Column(cUI).AlignItems(ui.End).Gap(theme.Space6).Children(func() {
			if len(atts) > 0 {
				c.renderUserMessageAttachments(cUI, pal, convID, idx, atts, !hasText)
			}
			if hasText || len(atts) == 0 {
				bubble := ui.Box(cUI).MaxWidth(320).Padding(theme.Space8, theme.Space12).Radius(theme.RadiusXL).Background(ais.UserBubbleBG)
				bui.Probe(bubble, "ai-user-message")
				bubble.Children(func() {
					ui.Text(cUI, cleanText).FontSize(theme.FontBody).LineHeight(1.5).TextColor(pal.Text)
				})
			}
		})
	})
}

func (c *Controller) renderAssistantReply(cUI *ui.Context, pal theme.Palette, convID uint64, replyIdx int, text string, copyable bool, isStreaming bool) {
	boxKey := fmt.Sprintf("reply-%d-%d", convID, replyIdx)
	wrap := ui.Column(cUI.Key(boxKey)).FillWidth().Gap(4).
		Transition(probeSafeTransition(bui.CascadeItemTransition(replyIdx, false)))
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
					c.mu.Lock()
					c.cancelPendingClipboardLocked()
					c.mu.Unlock()
					cUI.WriteClipboard(text)
				}
			})
		}
	})
}

func (c *Controller) markdownOptions(pal theme.Palette) *markdown.Options {
	key := mdPaletteKey{
		paper:         pal.Paper,
		text:          pal.Text,
		textSecondary: pal.TextSecondary,
		accent:        pal.Accent,
		codeBlockBG:   pal.CodeBlockBG,
		hairline:      pal.Hairline,
	}
	if c.mdOpts.Theme == nil || c.mdThemeKey != key {
		c.mdThemeKey = key
		c.mdOpts.ShareSelection = true
		c.mdOpts.Theme = &markdown.Theme{
			FontSize:            theme.FontBody,
			LineHeight:          1.55,
			Text:                pal.Text,
			Muted:               pal.TextSecondary,
			Link:                pal.Accent,
			Strong:              pal.Text,
			StrongWeight:        600,
			Code:                pal.Text,
			CodeBackground:      pal.CodeBlockBG,
			CodeBlockBackground: pal.CodeBlockBG,
			CodeBlockBorder:     pal.Hairline,
			Rule:                pal.Hairline,
			TableBorder:         pal.Hairline,
			TableHeader:         pal.CodeBlockBG,
		}
	}
	if c.mdOpts.OnLink == nil {
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
			det = latestStreamLine(partial)
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

// processDisplayOrder lists step indexes to draw; while streaming the live step leads,
// so it stays on the first row instead of being pushed down by finished steps.
func processDisplayOrder(steps []model.ProcessStep, streaming bool) []int {
	order := make([]int, 0, len(steps))
	live := -1
	if streaming {
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].ID == "streaming" && !steps[i].Done {
				live = i
				break
			}
		}
	}
	if live >= 0 {
		order = append(order, live)
	}
	for i := range steps {
		if i != live {
			order = append(order, i)
		}
	}
	return order
}

// latestStreamLine is the newest non-empty line of a reply being written, as plain text.
func latestStreamLine(s string) string {
	lines := strings.Split(s, "\n")
	// Code is shown as written: print("**hi**") keeps its stars.
	same := make([]int, len(lines))
	code := bui.DiffFenceMask(lines, same)
	// Only a table's own separator is skipped; a later "| --- |" is a data row.
	_, seps := bui.DiffTableRoles(lines, same)
	// Show reference text and skip definitions consumed by the note parser.
	refs, defs := bui.DiffReferences(lines, same)
	// Only a table row's edge bars are its own: "| 是 Shell 的管道符" keeps its "|".
	cols := bui.DiffTableCols(lines, same)
	// Preserve ordered markers that the note reads as literal text.
	nums := bui.DiffListNumbers(lines, same)
	// A setext underline ("===") only makes the line above a heading.
	setext := bui.DiffSetextLevels(lines, same)
	fences, quotes := bui.DiffFenceLines(lines, same)
	// Only a quote's first "[!NOTE]" marks an alert, which shows no text of its own.
	alerts := bui.DiffAlertTitles(lines, same)
	// A formula shows its source without the "$$" lines around it.
	math := bui.DiffMathMask(lines, same)
	// "安装完成\" over more text breaks the line there; at a paragraph's end the "\" is text.
	breaks := bui.DiffHardBreaks(lines, same)
	for i := len(lines) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(lines[i])
		// A fence line is no text of its own: after "```" closes, the code shows.
		if raw == "" || seps[i] || fences[i] || setext[i] < 0 || defs[i] && !code[i] {
			continue
		}
		if math[i] && !code[i] {
			if f := plainMathText(raw); f != "" {
				return plainClip(f)
			}
			continue
		}
		// A quote line with nothing behind its ">" only separates paragraphs.
		if !code[i] && strings.Trim(raw, "> \t") == "" {
			continue
		}
		// A rule ("---") or an alert marker ("> [!NOTE]") is no text.
		if !code[i] && (plainRuleRE.MatchString(raw) || alerts[i] != "") {
			continue
		}
		// "$$x^2$$" alone on its line is a formula too.
		if !code[i] && plainMathLineRE.MatchString(raw) {
			if f := plainMathText(raw); f != "" {
				return plainClip(f)
			}
			continue
		}
		t := raw
		// Code in a quoted fence shows without the quote's markers.
		for q := quotes[i]; q > 0 && strings.HasPrefix(t, ">"); q-- {
			t = strings.TrimPrefix(strings.TrimPrefix(t, ">"), " ")
		}
		if !code[i] {
			if breaks[i] {
				raw = raw[:len(raw)-1]
			}
			if nums[i] == -1 {
				if m := plainParaMarkerRE.FindStringSubmatchIndex(raw); m != nil {
					raw = raw[:m[3]] + `\` + raw[m[3]:]
				}
			}
			// An indented line under paragraph text is text: "    1. 启动服务" keeps "1.".
			if setext[i] == bui.DiffParaText {
				raw = bui.InlineStart(raw)
			}
			t = plainInline(raw, refs[i], cols[i] > 0)
		}
		if t == "" {
			// 只有 ** 等半截标记时保留原文，避免实时行退回“正在生成回答”。
			t = raw
		}
		return plainClip(t)
	}
	return ""
}

// plainURLUnescaper drops the escapes PlainLinks puts on a link's text:
// "<https://x/?q=go&lang=zh>" keeps its "&".
var plainURLUnescaper = strings.NewReplacer(`\\`, `\`, "\\`", "`", `\*`, "*", `\_`, "_", `\~`, "~",
	`\[`, "[", `\]`, "]", `\$`, "$", `\&`, "&", "&#91;", "[", "&#93;", "]")

// plainClip keeps the last 80 characters of a live line.
func plainClip(t string) string {
	if r := []rune(t); len(r) > 80 {
		return "…" + string(r[len(r)-80:])
	}
	return t
}

// plainMathText is a display math line without its "$$" or "\[" "\]" delimiters.
func plainMathText(t string) string {
	for _, d := range [2][2]string{{"$$", "$$"}, {`\[`, `\]`}} {
		t = strings.TrimSuffix(strings.TrimPrefix(t, d[0]), d[1])
	}
	return strings.TrimSpace(t)
}

// plainRuleRE matches a thematic break ("---", "* * *"), behind quote markers too.
var plainRuleRE = regexp.MustCompile(`^(?:>\s*)*(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)

// plainMathLineRE matches a one-line display formula ("$$x^2$$", `\[x^2\]`); with
// a closer inside ("$$a$$ + $$b$$") the line holds inline formulas instead.
var plainMathLineRE = regexp.MustCompile(`^(?:\$\$(?:[^$]|\$[^$])+\$\$|\\\[(?:[^\\]|\\[^\]])+\\\])$`)

// plainBreakRE matches a line break tag ("<br>", "<BR/>").
var plainBreakRE = regexp.MustCompile(`(?i)^<br\b`)

// plainTagRE matches an inline HTML tag the note styles ("<kbd>", "</kbd>", "<br/>");
// any other ("<TOKEN>", "<div>") shows as written, as in the note.
var plainTagRE = regexp.MustCompile(`(?i)</?(?:br|b|strong|i|em|u|ins|s|del|strike|code|kbd|samp|mark|sub|sup|small)(?:\s[^<>]*)?/?>`)

// plainBlockPrefixRE matches leading quote and list markers (not "-5") and a heading
// marker after them; what follows a heading is its text: "## 1. 安装" keeps "1.".
// A task item's box goes too: "- [x] 备份完成" reads 备份完成.
var plainBlockPrefixRE = regexp.MustCompile(`^(?:>\s*|(?:[-*+]|\d{1,9}[.)])\s+(?:\[[ xX]\]\s+)?)*(?:#{1,6}\s+)?`)

// plainInlineMathRE matches an inline formula: "\(…\)", "\[…\]" or "$$…$$".
var plainInlineMathRE = regexp.MustCompile(`\\\(.+?\\\)|\\\[.+?\\\]|\$\$.+?\$\$`)

// plainEntityRE matches an HTML entity reference, which ends with ";".
var plainEntityRE = regexp.MustCompile(`&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// plainClosingHashRE matches an ATX heading's optional closing "#"s ("## 安装 ##").
var plainClosingHashRE = regexp.MustCompile(`(?:^|\s+)#+\s*$`)

// plainParaMarkerRE finds an ordered marker ("2." behind any ">") read as paragraph text.
var plainParaMarkerRE = regexp.MustCompile(`^(?:>\s*)*\d{1,9}()[.)]`)

// plainEmphasisREs match **bold**, __bold__, *italic*, _italic_ and ~~struck~~ text.
// Stars may sit inside a word ("设置API**密钥**后"); underscores may not, so
// snake_case stays, and Python's 2**3 or a lone "**" has no closer and stays too.
// Doubled markers go first, so "**x**" is never read as "*" around "*x*", and bold
// may hold italic ("**再看 *配置***").
var plainEmphasisREs = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^*\\])\*\*(\S(?:.*?\S)??)\*\*($|[^*])`),
	regexp.MustCompile(`(^|[^\p{L}\p{N}_\\])__(\S(?:.*?\S)??)__($|[^\p{L}\p{N}_])`),
	// A single "~" may sit inside: "~~20~25 元~~" and "~~1~2~~" are struck through.
	regexp.MustCompile(`(^|[^~\\])~~([^\s~](?:(?:[^~]|~[^~])*?(?:[^\s~]|~[^\s~]))?)~~($|[^~])`),
	// A single "~" between letters or digits is a range ("20~25"), not strikethrough.
	regexp.MustCompile(`(^|[^~\\\p{L}\p{N}])~(\S(?:[^~]*\S)?)~($|[^~\p{L}\p{N}])`),
	// An escaped marker ("*匹配 \* 号*") is text inside, never the closer.
	regexp.MustCompile(`(^|[^*\\])\*((?:\\.|[^\s*\\])(?:(?:\\.|[^*\\])*(?:\\.|[^\s*\\]))?)\*($|[^*])`),
	regexp.MustCompile(`(^|[^\p{L}\p{N}_\\])_((?:\\.|[^\s_\\])(?:(?:\\.|[^_\\])*(?:\\.|[^\s_\\]))?)_($|[^\p{L}\p{N}_])`),
}

// plainEmphasis drops the markers of re's matches in t; a "*" run that can't open
// or close there stays, as in the note: "file*.csv 和 data*.json" keeps both.
func plainEmphasis(re *regexp.Regexp, t string) string {
	// A code or link placeholder stands for text in backticks.
	near := func(r rune) rune {
		if r == '\uE000' || r == '\uE001' {
			return '`'
		}
		return r
	}
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(t, -1) {
		o, c, n := m[3], m[5], m[4]-m[3]
		if t[o] == '*' {
			before, after := '\n', '\n'
			if o > 0 {
				before, _ = utf8.DecodeLastRuneInString(t[:o])
			}
			if c+n < len(t) {
				after, _ = utf8.DecodeRuneInString(t[c+n:])
			}
			first, _ := utf8.DecodeRuneInString(t[o+n:])
			end, _ := utf8.DecodeLastRuneInString(t[:c])
			open, _ := bui.StarFlanks(near(before), near(first))
			_, closes := bui.StarFlanks(near(end), near(after))
			if !open || !closes {
				continue
			}
		}
		b.WriteString(t[last:m[0]])
		b.WriteString(t[m[2]:m[3]])
		b.WriteString(t[m[4]:m[5]])
		b.WriteString(t[m[6]:m[7]])
		last = m[1]
	}
	b.WriteString(t[last:])
	return b.String()
}

// plainEscapeRE matches a backslash escape of ASCII punctuation ("\*" shows "*").
var plainEscapeRE = regexp.MustCompile("\\\\([!-/:-@\\[-`{-~])")

// plainInline strips the Markdown markers that read as noise in a one-line label.
// refs are the reference labels defined around s (nil: none); a table row loses
// its edge bars.
func plainInline(s string, refs map[string]bool, table bool) string {
	s = strings.TrimSpace(s)
	// Each cell reads on its own, as in the note: "| */src | dist/* |" keeps both
	// stars. A cell's leading "-" or "1." is text, not a list marker.
	if table {
		if cells := bui.ParseDiffTableCells(s); cells != nil {
			for k, c := range cells {
				cells[k] = plainInline(bui.InlineStart(c), refs, false)
			}
			return strings.Join(cells, " | ")
		}
	}
	if strings.Contains(s, "#") {
		if h := plainBlockPrefixRE.FindString(s); strings.Contains(h, "#") {
			s = h + plainClosingHashRE.ReplaceAllString(s[len(h):], "")
		}
	}
	// Links show their text: "[文档](/guide)" reads 文档.
	t := bui.PlainLinks(refs, plainBlockPrefixRE.ReplaceAllString(s, ""))
	// Code spans stand in as placeholders while emphasis markers are stripped, so
	// `**/node_modules/**` keeps its stars and **run `x`** still loses its own.
	var codes []string
	var b strings.Builder
	last := 0
	for _, sp := range bui.CodeSpans(t) {
		b.WriteString(t[last:sp[0]])
		b.WriteString("\uE000" + strconv.Itoa(len(codes)) + "\uE001")
		codes = append(codes, strings.Trim(t[sp[0]:sp[1]], "`"))
		last = sp[1]
	}
	b.WriteString(t[last:])
	t = b.String()
	// A bare link keeps its "_" and "*": "https://example.com/_draft_/index" stays.
	if spans := bui.BareURLs(t); len(spans) > 0 {
		var u strings.Builder
		last := 0
		for _, sp := range spans {
			u.WriteString(t[last:sp[0]])
			u.WriteString("\uE000" + strconv.Itoa(len(codes)) + "\uE001")
			// A "<https://…>" link made text has its markers escaped already.
			codes = append(codes, plainURLUnescaper.Replace(t[sp[0]:sp[1]]))
			last = sp[1]
		}
		u.WriteString(t[last:])
		t = u.String()
	}
	// A formula ("\(a*b*c\)") shows its source, stars and all, like code.
	t = plainInlineMathRE.ReplaceAllStringFunc(t, func(m string) string {
		k := "\uE000" + strconv.Itoa(len(codes)) + "\uE001"
		codes = append(codes, strings.TrimSpace(m[2:len(m)-2]))
		return k
	})
	// Adjacent matches share the space between them, so repeat until settled.
	for prev := ""; prev != t; {
		prev = t
		for _, re := range plainEmphasisREs {
			t = plainEmphasis(re, t)
		}
	}
	// Inline HTML shows its text ("请按 <kbd>Ctrl</kbd>" reads 请按 Ctrl); it goes
	// before entities decode, so "&lt;b&gt;" stays as text.
	t = plainTagRE.ReplaceAllStringFunc(t, func(m string) string {
		// "安装完成<br>验证服务" keeps a break between its parts.
		if plainBreakRE.MatchString(m) {
			return " "
		}
		return ""
	})
	// Stray backticks go; an escaped one ("\`") is a literal backtick.
	t = strings.NewReplacer("\\`", "`", "`", "").Replace(t)
	// Escaped markers ("\**提示**") were never bold; they show without the backslash.
	// An escaped bar ("\| grep error") is text, so it waits out the trim below.
	t = plainEscapeRE.ReplaceAllStringFunc(t, func(m string) string {
		switch m {
		case `\|`:
			return ""
		case `\&`:
			return ""
		}
		return m[1:]
	})
	// Table bars are trimmed before entities and code return, so `| grep error` and
	// "&#124; grep error" keep their pipe.
	if table {
		t = strings.Trim(t, "| ")
	}
	t = strings.ReplaceAll(t, "", "|")
	// Entities show their character ("A &amp; B" reads A & B); "\&amp;" was text.
	// Only a ";"-ended one counts: "?lang=zh&currency=CNY" keeps its "&currency".
	t = plainEntityRE.ReplaceAllStringFunc(t, html.UnescapeString)
	t = strings.ReplaceAll(t, "", "&")
	// Last first: a link's text may hold an earlier placeholder ("<https://x>(`/api`)").
	for k := len(codes) - 1; k >= 0; k-- {
		code := codes[k]
		// In a table row "\|" is a bar even in code: `foo\|bar` reads foo|bar.
		if table {
			code = strings.ReplaceAll(code, `\|`, "|")
		}
		t = strings.Replace(t, "\uE000"+strconv.Itoa(k)+"\uE001", code, 1)
	}
	return strings.TrimSpace(t)
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

func (c *Controller) renderProcessCard(cUI *ui.Context, pal theme.Palette, turnKey string, outcome string, steps []model.ProcessStep, notes []string, streaming bool) {
	c.mu.RLock()
	// 执行中始终展开步骤列表，结束后按用户的展开状态。
	expanded := streaming || c.expandedProcess[turnKey]
	c.mu.RUnlock()

	headline := fmt.Sprintf("%d 步执行完成", len(steps))
	if theme.IsEn() {
		headline = fmt.Sprintf("%d steps completed", len(steps))
	}
	if outcome == "failed" {
		headline = theme.Tr("执行失败", "Execution failed")
	} else if outcome == "stopped" {
		headline = theme.Tr("已停止", "Stopped")
	}

	ais := pal.AISurface()
	card := ui.Column(cUI.Key("ai-process-card-"+turnKey)).FillWidth().Padding(8, 10).Gap(6).
		Radius(theme.RadiusMD).Background(ais.WellBG).Border(1, pal.Hairline).
		Transition(probeSafeTransition(bui.CascadeItemTransition(0, false)))
	bui.Probe(card, "ai-process-card")
	bui.RevealOnExpand(card, !streaming && expanded)

	card.Children(func() {
		if !streaming {
			// 执行中只保留步骤列表：顶部标题与最后一步重复，直接省掉。
			toggleBtn := ui.ButtonBase(cUI.Key("ai-process-toggle-" + turnKey)).FillWidth().MinHeight(28).
				PaddingX(4).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(headline)
			bui.Probe(toggleBtn, "ai-process-toggle")
			toggleBtn.Children(func() {
				chev := icons.ChevronRight
				if expanded {
					chev = icons.ChevronDown
				}
				chevActive := toggleBtn.Hovered() || toggleBtn.Pressed()
				icons.DrawAnimated(cUI, chev, 14, pal.HoverIconColors(chevActive), chevActive)
				ui.Text(cUI.Key("ai-process-headline-"+turnKey), headline).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).Grow(1).SingleLine()
			})
			if toggleBtn.Clicked() {
				c.mu.Lock()
				c.expandedProcess[turnKey] = !c.expandedProcess[turnKey]
				c.mu.Unlock()
			}
		}

		if expanded {
			// 不再内嵌滚动：步骤随内容自然撑开，由外层聊天区滚动。
			stepsWrap := ui.Column(cUI.Key("ai-process-steps-" + turnKey)).FillWidth()
			bui.Probe(stepsWrap, "ai-process-steps")
			stepsWrap.Children(func() {
				ui.Column(cUI).FillWidth().Gap(4).Children(func() {
					for _, i := range processDisplayOrder(steps, streaming) {
						st := steps[i]
						// 按步骤 ID 记展开状态：流式中新步骤插在实时行之前，按下标会串位。
						stepKey := fmt.Sprintf("%s#%d", turnKey, i)
						if st.ID != "" {
							stepKey = turnKey + "#id:" + st.ID
						}
						c.mu.RLock()
						detailOpen := c.expandedStepDetails[stepKey]
						c.mu.RUnlock()

						chipID := fmt.Sprintf("ai-process-tool-chip-%d", i)
						row := ui.ButtonBase(cUI.Key(chipID + "-" + turnKey)).FillWidth().MinHeight(28).PaddingX(6).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(st.Title).
							Transition(probeSafeTransition(bui.CascadeItemTransition(i, false)))
						hovered := row.Hovered()
						pressed := row.Pressed()
						bg := ui.Transparent
						if pressed {
							bg = pal.Selected
						} else if hovered {
							bg = pal.Hover
						}
						bui.AnimateSurfaceColors(row, bg, ui.Transparent, true)
						bui.TactileCard(row, hovered, pressed, detailOpen, false)
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
							if st.ID == "streaming" && !st.Done {
								bui.SparkleLoader(cUI, 14, pal.TextSecondary, pal.Accent)
							} else {
								icons.DrawAnimated(cUI, iconGlyph, 14, pal.MonoIconColors(pal.TextSecondary), hovered || pressed || detailOpen)
							}
							// The streaming row's detail is already plain (latestStreamLine);
							// stripping it again would eat stars that sat in code.
							detail := strings.TrimSpace(st.Detail)
							if p := plainInline(st.Detail, nil, false); st.ID != "streaming" && p != "" {
								detail = p
							}
							if live := st.ID == "streaming" && !st.Done; live && detail != "" {
								// 实时行空间有限：只显示正在生成的内容，不再重复“正在生成回答”。
								ui.Text(cUI, detail).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							} else {
								stepTitle := ui.Text(cUI, st.Title).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine().Shrink(0)
								if detail == "" {
									// 无详情时由标题占满剩余宽度，避免整行被居中。
									stepTitle.Grow(1)
								} else {
									ui.Text(cUI, detail).FontSize(theme.FontBody).TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
								}
							}
						})
						if row.Clicked() {
							c.mu.Lock()
							c.expandedStepDetails[stepKey] = !c.expandedStepDetails[stepKey]
							c.mu.Unlock()
						}

						type stepDetailRevealKey struct{}
						type stepDetailRevealState struct {
							initialized bool
							open        bool
						}
						detRev := ui.Local(row, stepDetailRevealKey{}, func() stepDetailRevealState {
							return stepDetailRevealState{}
						})
						justOpenedDetail := false
						if !detRev.initialized {
							detRev.initialized = true
							detRev.open = detailOpen
						} else if detailOpen != detRev.open {
							detRev.open = detailOpen
							justOpenedDetail = detailOpen
						}

						if detailOpen {
							detID := fmt.Sprintf("ai-process-tool-detail-%d", i)
							detBox := ui.Column(cUI.Key(detID+"-"+turnKey)).FillWidth().Padding(4, 12).
								BorderWidth(0, 0, 0, 2).BorderColor(pal.Hairline)
							bui.Probe(detBox, detID)
							if justOpenedDetail {
								detBox.ScrollIntoView(ui.ScrollIntoViewOptions{
									Block:     ui.ScrollNearest,
									Animation: bui.JumpScrollAnimation(true),
								})
							}
							detBox.Children(func() {
								txt := st.Detail
								if txt == "" {
									txt = st.Title
								}
								if st.Done {
									txt = "✓ " + txt
								}
								ui.Text(cUI, txt).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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
						chip := ui.Box(cUI).Padding(3, 8).Radius(theme.RadiusSM).Background(ais.CapsuleBG).Border(1, ais.CapsuleBorder)
						chip.Children(func() {
							ui.Text(cUI, n+".md").FontSize(theme.FontBody).TextColor(pal.Text).SingleLine()
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
	ais := pal.AISurface()

	card := ui.Column(cUI.Key("ai-sources-" + turnKey)).FillWidth().Gap(6).
		Transition(probeSafeTransition(bui.CascadeItemTransition(0, false)))
	bui.Probe(card, "ai-context-card")
	bui.RevealOnExpand(card, expanded)
	card.Children(func() {
		toggle := ui.ButtonBase(cUI.Key("ai-sources-toggle-" + turnKey)).Height(28).PaddingX(6).Gap(8).
			AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
			Label(theme.Tr("参考笔记", "Sources"))
		bui.Probe(toggle, "ai-sources-toggle")
		toggle.Children(func() {
			ui.Text(cUI, theme.Tr("参考笔记", "Sources")).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text)
			ui.Box(cUI).Padding(2, 6).Radius(theme.RadiusXS).Background(ais.CapsuleBG).Children(func() {
				ui.Textf(cUI, "%d", len(sources)).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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
					AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(src.Title).
					Transition(probeSafeTransition(bui.CascadeItemTransition(i, false)))
				hovered := btn.Hovered()
				pressed := btn.Pressed()
				bg := ais.WellBG
				if pressed {
					bg = ais.SelectedBG
				} else if hovered {
					bg = ais.HoverBG
				}
				bui.AnimateSurfaceColors(btn, bg, pal.Hairline, true)
				bui.TactileCard(btn, hovered, pressed, false, false)
				bui.Probe(btn, srcID)
				btn.Children(func() {
					active := hovered || pressed
					icons.DrawAnimated(cUI, icons.Note, theme.IconRow, pal.DuoIconColors(pal.Text), active)
					ui.Text(cUI, src.Title+".md").FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).SingleLine()
					icons.DrawAnimated(cUI, icons.ChevronRight, theme.IconInline, pal.HoverIconColors(active), active)
				})
				if btn.Clicked() && c.host != nil {
					_ = c.host.OpenNote(src.Path, 0)
				}
			}
		}
	})
}

func (c *Controller) renderPlanCard(cUI *ui.Context, pal theme.Palette, convID uint64, msgIdx int, plan *model.BatchPlan) {
	cardID := fmt.Sprintf("plan-op-%d-0", msgIdx)
	card := ui.Column(cUI.Key(cardID)).FillWidth().Radius(theme.RadiusLG).Clip().
		Background(pal.Paper).Border(1, pal.Hairline).
		Transition(probeSafeTransition(bui.CascadeItemTransition(msgIdx, false)))
	bui.Probe(card, "ai-plan-card")

	pending := plan.Status == "pending"
	title := plan.Summary
	if len(plan.Ops) > 0 {
		if name, err := noteFilename(plan.Ops[0].Page); err == nil {
			title = filepath.ToSlash(name)
		}
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

	ais := pal.AISurface()
	card.Children(func() {
		topBtn := ui.ButtonBase(cUI.Key("plan-open-"+cardID)).FillWidth().Padding(theme.Space10, theme.Space16).Gap(theme.Space6).
			Radius(theme.RadiusLG, theme.RadiusLG, 0, 0).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Column().AlignItems(ui.Start).Cursor(ui.CursorPointer).Label(title)
		topBG := ais.WellBG
		if topBtn.Pressed() {
			topBG = ais.SelectedBG
		} else if topBtn.Hovered() {
			topBG = ais.HoverBG
		}
		bui.AnimateSurfaceColors(topBtn, topBG, ui.Transparent, false)
		bui.Probe(topBtn, cardID)
		topBtn.Children(func() {
			ui.Row(cUI).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				icons.DrawAnimated(cUI, icons.Sparkles, 16, pal.MonoIconColors(pal.Accent), topBtn.Hovered() || topBtn.Pressed() || pending)
				ui.Text(cUI, title).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).SingleLine()
				if isCreate && pending {
					ui.Box(cUI).Padding(2, 7).Radius(theme.RadiusXS).Background(pal.Selected).Children(func() {
						ui.Text(cUI, theme.Tr("新建预览", "Create preview")).FontSize(theme.FontSmall).TextColor(pal.Accent)
					})
				}
			})
			if plan.Summary != "" {
				ui.Text(cUI, plan.Summary).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
			}
		})
		if topBtn.Clicked() {
			var linkedPlan *model.BatchPlan
			var host model.WorkspaceHost
			c.mu.Lock()
			if pending {
				linkedPlan = c.linkPlanLocked(convID, msgIdx)
			} else if plan.Status == "applied" && len(plan.Ops) > 0 && c.host != nil {
				host = c.host
			}
			c.mu.Unlock()
			c.previewPlanInEditor(linkedPlan)
			if host != nil {
				if path, exists, err := LocatePlanNote(host, plan.Root, plan.Ops[0].Page); err == nil && exists {
					_ = host.OpenNote(path, 0)
				}
			}
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
			texts := make([]string, len(lines))
			changes := make([]int, len(lines))
			for i, dl := range lines {
				texts[i], changes[i] = dl.Text, dl.Change
			}
			fenced := bui.DiffFenceMask(texts, changes)
			setext := bui.DiffSetextLevels(texts, changes)
			listNums := bui.DiffListNumbers(texts, changes)
			refs, refDefs := bui.DiffReferences(texts, changes)
			aligns := bui.DiffTableAligns(texts, changes)
			math := bui.DiffMathMask(texts, changes)
			alerts, alertBodies := bui.DiffAlerts(texts, changes)
			fenceLines, fenceQuotes := bui.DiffFenceLines(texts, changes)
			hardBreaks := bui.DiffHardBreaks(texts, changes)
			tableCols := bui.DiffTableCols(texts, changes)
			headers, seps := bui.DiffTableRoles(texts, changes)
			var visible []PlanDiffLine
			for _, dl := range lines {
				if seps[dl.Index] {
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
			headerBG := pal.Table().HeaderBG
			previewWrap.Children(func() {
				listBox := ui.Column(cUI.Key("ai-plan-diff-list-"+cardID)).FillWidth().Radius(theme.RadiusLG).
					Border(1, pal.Hairline).Background(pal.Paper).Clip()
				listBox.Children(func() {
					totalVis := len(visible)
					for pos, dl := range visible {
						trimmed := strings.TrimSpace(dl.Text)
						isFirst := pos == 0
						isLast := pos+1 == totalVis
						code := fenced[dl.Index]
						isTable := !code && tableCols[dl.Index] > 0
						nextIsTable := pos+1 < totalVis && tableCols[visible[pos+1].Index] > 0
						isHeader := headers[dl.Index] ||
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
									bui.DiffTableRow(cUI, trimmed, tableCols[dl.Index], bui.DiffLineStyle{Text: rowFg, CodeBG: pal.CodeBlockBG, Refs: refs[dl.Index], Aligns: aligns[dl.Index], Math: math[dl.Index]}, isHeader, pal.Hairline)
								} else {
									bui.DiffMarkdownLine(cUI, dl.Text, bui.DiffLineStyle{Text: rowFg, CodeBG: pal.CodeBlockBG, Code: code, Setext: setext[dl.Index], ListNum: listNums[dl.Index], Refs: refs[dl.Index], Aligns: aligns[dl.Index], Math: math[dl.Index], RefDef: refDefs[dl.Index], Alert: alerts[dl.Index], AlertBody: alertBodies[dl.Index], Fence: fenceLines[dl.Index], Quote: fenceQuotes[dl.Index], Break: hardBreaks[dl.Index]})
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
									bui.DiffTableRow(cUI, trimmed, tableCols[dl.Index], bui.DiffLineStyle{Text: pal.Text, CodeBG: pal.CodeBlockBG, Refs: refs[dl.Index], Aligns: aligns[dl.Index], Math: math[dl.Index]}, isHeader, pal.Hairline)
								} else {
									bui.DiffMarkdownLine(cUI, dl.Text, bui.DiffLineStyle{Text: pal.TextSecondary, CodeBG: pal.CodeBlockBG, Code: code, Setext: setext[dl.Index], ListNum: listNums[dl.Index], Refs: refs[dl.Index], Aligns: aligns[dl.Index], Math: math[dl.Index], RefDef: refDefs[dl.Index], Alert: alerts[dl.Index], AlertBody: alertBodies[dl.Index], Fence: fenceLines[dl.Index], Quote: fenceQuotes[dl.Index], Break: hardBreaks[dl.Index]})
								}
							})
						}
					}
				})
			})
		}

		ui.Row(cUI).FillWidth().Padding(theme.Space10, theme.Space16).Gap(theme.Space8).Radius(0, 0, theme.RadiusLG, theme.RadiusLG).Background(ais.WellBG).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			ui.Text(cUI, badge).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(badgeCol)
			ui.Row(cUI).Gap(8).Children(func() {
				switch plan.Status {
				case "pending":
					rejIc := icons.Close
					rejectBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-reject-%d", msgIdx), theme.Tr("拒绝", "Reject"), &rejIc, bui.ButtonSecondary, false)
					if rejectBtn.Clicked() {
						_ = c.RejectPlan(convID, msgIdx)
					}
					accIc := icons.Check
					acceptBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-accept-%d", msgIdx), theme.Tr("接受", "Accept"), &accIc, bui.ButtonPrimary, false)
					if acceptBtn.Clicked() {
						_ = c.AcceptPlan(convID, msgIdx)
					}
				case "applied":
					undoIc := icons.TbReset
					undoBtn := bui.ActionButton(cUI, fmt.Sprintf("plan-undo-%d", msgIdx), theme.Tr("撤销", "Undo"), &undoIc, bui.ButtonSecondary, false)
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
		card := ui.Row(cUI.Key("ai-q-done-"+qKey)).FillWidth().Padding(10, 12).Radius(theme.RadiusMD).
			Background(pal.AISurface().WellBG).Border(1, pal.Hairline).Children(func() {
			ui.Text(cUI, theme.Tr("已提交选择", "Response submitted")).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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

	card := bui.Elevate(ui.Column(cUI.Key("ai-question-card-"+qKey)).FillWidth().Padding(theme.Space16).Gap(theme.Space10).
		Radius(theme.RadiusLG).Background(pal.Paper).Border(1, pal.BtnBorder).
		Transition(probeSafeTransition(bui.CascadeItemTransition(0, false))), theme.ElevationPopover)
	bui.Probe(card, "ai-question-card")

	card.Children(func() {
		ui.Row(cUI).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Text(cUI, q.Prompt).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1)
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
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(opt.Label)
				hovered := row.Hovered()
				pressed := row.Pressed()
				bg := ui.Transparent
				bd := ui.Transparent
				if on {
					bg = pal.Selected
					bd = pal.SelectedBorder
				} else if pressed {
					bg = pal.Selected
				} else if hovered {
					bg = pal.Hover
				}
				bui.AnimateSurfaceColors(row, bg, bd, true)
				bui.TactileCard(row, hovered, pressed, on, false)
				bui.Probe(row, optID)
				row.Children(func() {
					ind := ui.Box(cUI).Size(16, 16).Shrink(0).Border(1, pal.BtnBorder).Center()
					if q.MultiSelect {
						ind.Radius(theme.RadiusXS)
					} else {
						ind.Radius(theme.RadiusPill)
					}
					if on {
						ind.BorderColor(pal.Text).Background(pal.Text)
						ind.Children(func() {
							if q.MultiSelect {
								icons.DrawAnimated(cUI, icons.Check, 12, pal.MonoIconColors(pal.Paper), on)
							} else {
								dot := ui.Box(cUI.Key("qdot-"+optID)).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Paper)
								sc := dot.AnimateTo("qdotSc", 1.0, bui.SpringTactile)
								if sc != 1.0 {
									dot.Scale(sc, sc)
								}
							}
						})
					}
					fg := pal.TextSecondary
					if on {
						fg = pal.Text
					}
					ui.Text(cUI, opt.Label).FontSize(theme.FontBody).TextColor(fg).Grow(1)
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
				FillWidth().FontSize(theme.FontBody).Placeholder(theme.Tr("其他补充…", "Something else…")).PlaceholderColor(pal.Placeholder)
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
				ui.Textf(cUI, "%d / %d", qi+1, total).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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
		Radius(theme.RadiusLG).Background(pal.Paper).Border(1, pal.BtnBorder).
		Transition(probeSafeTransition(bui.CascadeItemTransition(0, false)))
	bui.Probe(card, "ai-approval-card")

	card.Children(func() {
		ui.Text(cUI, cardModel.ToolName).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text)
		if cardModel.Command != "" {
			ui.Text(cUI, cardModel.Command).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
		}
		if cardModel.Reason != "" {
			ui.Text(cUI, cardModel.Reason).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
		}

		renderOpt := func(id, label string, dec model.ApprovalDecision) {
			selected := choice == dec
			row := ui.ButtonBase(cUI.Key(id+"-"+aKey)).FillWidth().MinHeight(30).Padding(5, 8).Gap(8).
				AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(label)
			hovered := row.Hovered() && !submitted
			pressed := row.Pressed() && !submitted
			bg := ui.Transparent
			bd := ui.Transparent
			if selected {
				bg = pal.Selected
				bd = pal.SelectedBorder
			} else if pressed {
				bg = pal.Selected
			} else if hovered {
				bg = pal.Hover
			}
			bui.AnimateSurfaceColors(row, bg, bd, true)
			bui.TactileCard(row, hovered, pressed, selected, submitted)
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
				ui.Text(cUI, label).FontSize(theme.FontBody).TextColor(fg)
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
			ui.Text(cUI, cardModel.ErrorText).FontSize(theme.FontBody).TextColor(pal.Danger)
		}

		enabled := choice != model.ApprovalPending && !submitted
		ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, "1 / 1").FontSize(theme.FontBody).TextColor(pal.TextSecondary)
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
		ui.Column(cUI).Grow(1).Padding(10, 12).Gap(6).Radius(theme.RadiusMD).
			Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).Children(func() {
			ui.Row(cUI).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(cUI, theme.Tr("本轮失败原因", "Request failed")).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Danger)
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
			ui.Text(cUI, errText).FontSize(theme.FontBody).TextColor(pal.Text)
			if needSettings && c.host != nil {
				slIc := icons.Sliders
				btn := bui.ActionButton(cUI, "ai-chat-settings", theme.Tr("打开 AI 设置", "Open AI settings"), &slIc, bui.ButtonSecondary, false)
				if btn.Clicked() {
					c.host.OpenSettings("ai")
				}
			}
		})
	})
}

func (c *Controller) renderDeferredDraftBanner(cUI *ui.Context, pal theme.Palette, fullscreen bool) {
	c.mu.RLock()
	canRestore := !c.streaming && c.composerDraftEmptyLocked()
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
			FontSize(theme.FontBody).TextColor(pal.TextSecondary).Grow(1)
		histIc := icons.History
		btn := bui.ActionButton(cUI, "ai-restore-draft", theme.Tr("恢复草稿", "Restore draft"), &histIc, bui.ButtonSecondary, !canRestore)
		if !canRestore {
			bui.Tooltip(cUI, btn, theme.Tr("回复结束且输入框清空后可恢复。", "Restore when the response ends and the composer is empty."))
		}
		if btn.Clicked() {
			c.restoreDeferredDraft()
		}
	})
}

func (c *Controller) restoreDeferredDraft() {
	c.mu.Lock()
	if c.streaming || !c.composerDraftEmptyLocked() {
		c.mu.Unlock()
		return
	}
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
	c.draftAttachments = append([]model.Attachment(nil), d.Attachments...)
	c.retryDraft = &d
	c.composerEpoch++
	c.composerSelectEnd = true
	c.saveTabDraftLocked()
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

type pickerItemsCache struct {
	valid       bool
	token       string
	folder      string
	rawRoot     string
	rawNotePath string
	pickerRoot  string
	foldersHash uint64
	foldersLen  int
	pagesHash   uint64
	pagesLen    int
	skillsHash  uint64
	skillsLen   int
	refsHash    uint64
	refsLen     int
	localeEn    bool
	items       []pickItem
}

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

func hashString64(h uint64, s string) uint64 {
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	h ^= 0xff
	h *= fnvPrime64
	return h
}

func hashStringSlice64(xs []string) uint64 {
	h := fnvOffset64
	for _, s := range xs {
		h = hashString64(h, s)
	}
	return h
}

func hashSkillSlice64(xs []model.SkillSpec) uint64 {
	h := fnvOffset64
	for i := range xs {
		h = hashString64(h, xs[i].Name)
		h = hashString64(h, xs[i].Description)
	}
	return h
}

func hashModelOptions64(xs []ModelOption) uint64 {
	h := fnvOffset64
	for i := range xs {
		h = hashString64(h, xs[i].Value)
		h = hashString64(h, xs[i].Label)
		h = hashString64(h, xs[i].Group)
	}
	return h
}

func (c *Controller) pickerItemsLocked(token string) []pickItem {
	rawRoot := c.activeRootLocked()
	if strings.HasPrefix(token, "@") && !c.pickerInjected && c.pickerRoot != "" && rawRoot != "" && filepath.Clean(c.pickerRoot) != filepath.Clean(rawRoot) {
		return nil
	}
	rawNotePath := ""
	if c.host != nil {
		rawNotePath = c.host.ActiveNotePath()
	}
	foldersHash := hashStringSlice64(c.pickerFolders)
	pagesHash := hashStringSlice64(c.pickerPages)
	skillsHash := hashSkillSlice64(c.pickerSkills)
	refsHash := hashStringSlice64(c.draftRefs)
	localeEn := theme.IsEn()

	if c.pickerCache.valid &&
		c.pickerCache.token == token &&
		c.pickerCache.folder == c.pickerFolder &&
		c.pickerCache.rawRoot == rawRoot &&
		c.pickerCache.rawNotePath == rawNotePath &&
		c.pickerCache.pickerRoot == c.pickerRoot &&
		c.pickerCache.foldersLen == len(c.pickerFolders) &&
		c.pickerCache.foldersHash == foldersHash &&
		c.pickerCache.pagesLen == len(c.pickerPages) &&
		c.pickerCache.pagesHash == pagesHash &&
		c.pickerCache.skillsLen == len(c.pickerSkills) &&
		c.pickerCache.skillsHash == skillsHash &&
		c.pickerCache.refsLen == len(c.draftRefs) &&
		c.pickerCache.refsHash == refsHash &&
		c.pickerCache.localeEn == localeEn {
		return c.pickerCache.items
	}

	currentPage := c.activePageLocked(rawRoot)
	items := c.computePickerItemsLocked(token, currentPage)
	c.pickerCache = pickerItemsCache{
		valid:       true,
		token:       token,
		folder:      c.pickerFolder,
		rawRoot:     rawRoot,
		rawNotePath: rawNotePath,
		pickerRoot:  c.pickerRoot,
		foldersHash: foldersHash,
		foldersLen:  len(c.pickerFolders),
		pagesHash:   pagesHash,
		pagesLen:    len(c.pickerPages),
		skillsHash:  skillsHash,
		skillsLen:   len(c.pickerSkills),
		refsHash:    refsHash,
		refsLen:     len(c.draftRefs),
		localeEn:    localeEn,
		items:       items,
	}
	return items
}

func (c *Controller) computePickerItemsLocked(token, currentPage string) []pickItem {
	type scoredPickItem struct {
		item  pickItem
		score int
	}
	if strings.HasPrefix(token, "@") {
		q := strings.TrimSpace(strings.TrimPrefix(token, "@"))
		folder := c.pickerFolder
		if q == "" {
			var items []pickItem
			if folder != "" {
				parent := pathDirSlash(folder)
				items = append(items, pickItem{
					kind:  pickFolder,
					value: parent,
					label: theme.Tr("返回上级", "Parent folder"),
					root:  c.pickerRoot,
				})
			}
			for _, f := range c.pickerFolders {
				if pathDirSlash(f) == folder {
					items = append(items, pickItem{kind: pickFolder, value: f, label: pathBaseSlash(f), root: c.pickerRoot})
				}
			}
			refSet := make(map[string]bool, len(c.draftRefs))
			for _, r := range c.draftRefs {
				refSet[referencePageID(c.pickerRoot, r)] = true
			}
			if currentPage != "" && !refSet[currentPage] && pathDirSlash(currentPage) == folder {
				items = append(items, pickItem{
					kind:  pickPage,
					value: currentPage,
					label: pathBaseSlash(currentPage),
					hint:  theme.Tr("当前文件", "Current file"),
					root:  c.pickerRoot,
				})
			}
			for _, p := range c.pickerPages {
				if p == currentPage || refSet[p] {
					continue
				}
				if pathDirSlash(p) == folder {
					items = append(items, pickItem{kind: pickPage, value: p, label: pathBaseSlash(p), root: c.pickerRoot})
				}
			}
			return items
		}

		var scored []scoredPickItem
		for _, f := range c.pickerFolders {
			if sc, ok := fuzzy.ScoreFields(q, pathBaseSlash(f), f); ok {
				scored = append(scored, scoredPickItem{
					item:  pickItem{kind: pickFolder, value: f, label: f, root: c.pickerRoot},
					score: sc,
				})
			}
		}
		refSet := make(map[string]bool, len(c.draftRefs))
		for _, r := range c.draftRefs {
			refSet[referencePageID(c.pickerRoot, r)] = true
		}
		if currentPage != "" && !refSet[currentPage] {
			if sc, ok := fuzzy.ScoreFields(q, pathBaseSlash(currentPage), currentPage); ok {
				scored = append(scored, scoredPickItem{
					item: pickItem{
						kind:  pickPage,
						value: currentPage,
						label: currentPage,
						hint:  theme.Tr("当前文件", "Current file"),
						root:  c.pickerRoot,
					},
					score: sc + 50,
				})
			}
		}
		for _, p := range c.pickerPages {
			if p == currentPage || refSet[p] {
				continue
			}
			if sc, ok := fuzzy.ScoreFields(q, pathBaseSlash(p), p); ok {
				scored = append(scored, scoredPickItem{
					item:  pickItem{kind: pickPage, value: p, label: p, root: c.pickerRoot},
					score: sc,
				})
			}
		}
		sort.SliceStable(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})
		items := make([]pickItem, len(scored))
		for i, s := range scored {
			items[i] = s.item
		}
		return items
	}

	if strings.HasPrefix(token, "/") {
		q := strings.TrimSpace(strings.TrimPrefix(token, "/"))
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
		if q == "" {
			return all
		}
		var scored []scoredPickItem
		for _, it := range all {
			var primary []string
			var secondary []string
			if it.kind == pickNewConv {
				primary = []string{"new", "新会话", it.label}
				secondary = []string{it.hint, "开启空白对话", "Start a new chat"}
			} else {
				primary = []string{it.value, it.label}
				secondary = []string{it.hint}
			}
			if sc, ok := fuzzy.Score(q, primary, secondary, false); ok {
				scored = append(scored, scoredPickItem{item: it, score: sc})
			}
		}
		sort.SliceStable(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})
		out := make([]pickItem, len(scored))
		for i, s := range scored {
			out[i] = s.item
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
		rawRoot := c.activeRootLocked()
		if it.root != "" && rawRoot != "" && filepath.Clean(it.root) != filepath.Clean(rawRoot) {
			return c.draftText, false
		}
		if it.value != "" {
			if !c.pickerInjected && c.pickerRoot != "" && rawRoot != "" && filepath.Clean(c.pickerRoot) != filepath.Clean(rawRoot) {
				return c.draftText, false
			}
			if !containsString(c.pickerFolders, it.value) {
				return c.draftText, false
			}
		}
		c.pickerFolder = it.value
		c.pickIndex = 0
		c.draftText = rest + "@"
		c.composerSelectEnd = true
		c.persistActiveDraftLocked()
		return c.draftText, true
	case pickPage:
		rawRoot := c.activeRootLocked()
		if it.root != "" && rawRoot != "" && filepath.Clean(it.root) != filepath.Clean(rawRoot) {
			return c.draftText, false
		}
		root, err := AuthorizedNotebookRoot(c.host, rawRoot)
		filename, nameErr := noteFilename(it.value)
		if err != nil || nameErr != nil {
			return c.draftText, false
		}
		if c.pickerRoot != "" {
			cached, err := canonicalNotebook(c.pickerRoot)
			if err != nil || cached != root {
				return c.draftText, false
			}
		}
		ref := filepath.Join(root, filename)
		if !c.pickerInjected {
			info, statErr := os.Stat(ref)
			if statErr != nil || info.IsDir() {
				return c.draftText, false
			}
		}
		if !containsString(c.draftRefs, ref) {
			c.draftRefs = append(c.draftRefs, ref)
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
	c.mu.Lock()
	token, open := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	if !open {
		c.mu.Unlock()
		return
	}
	items := c.pickerItemsLocked(token)
	selected := c.pickIndex
	if selected >= len(items) {
		selected = max(len(items)-1, 0)
	}
	loading := c.pickerLoading
	pickerRootSnap := c.pickerRoot
	c.mu.Unlock()

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
		Transition(probeSafeTransition(bui.PopoverSpringTransition()))
	if fullscreen {
		wrap.MaxWidth(720).Margin(0, ui.Auto)
	}
	wrap.Children(func() {
		card := bui.Elevate(ui.Column(cUI.Key(probeID)).FillWidth().MaxHeight(pickerMaxH).Radius(theme.RadiusXL).
			Background(pal.Menu).Border(1, pal.MenuBorder).Clip(), theme.ElevationPopover)
		type pickerNavKey struct{}
		type pickerNavState struct {
			lastSelected int
			pointerMode  bool
		}
		nav := ui.Local(card, pickerNavKey{}, func() pickerNavState {
			return pickerNavState{lastSelected: selected}
		})
		if selected != nav.lastSelected {
			nav.lastSelected = selected
			nav.pointerMode = false
		}
		svc := cUI.Services()
		card.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputPointerMove:
				if !nav.pointerMode {
					nav.pointerMode = true
					return true
				}
			case ui.InputScroll:
				if !nav.pointerMode {
					nav.pointerMode = true
					svc.Invalidate()
				}
			}
			return false
		})
		bui.Probe(card, probeID)
		card.Children(func() {
			ui.Text(cUI, headerTitle).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).Padding(8, 12, 4, 12)
			if len(items) == 0 {
				msg := emptyMsg
				if loading {
					msg = theme.Tr("正在加载…", "Loading…")
				}
				ui.Text(cUI, msg).FontSize(theme.FontBody).TextColor(pal.TextSecondary).Padding(10, 12)
				return
			}

			listH := min(float32(len(items))*36, max(pickerMaxH-32, 36))
			c.pickerList.Key = func(i int) any {
				return fmt.Sprintf("%s:%d:%s", pickerRootSnap, items[i].kind, items[i].value)
			}
			pickerListEl := ui.List(cUI.Key("ai-picker-virtual-list"), &c.pickerList, len(items), func(i int) {
				it := items[i]
				rowID := fmt.Sprintf("ai-pick-%d", i)
				row := ui.ButtonBase(cUI.Key(fmt.Sprintf("ai-pick-%s:%d:%s", pickerRootSnap, it.kind, it.value))).FillWidth().Height(36).PaddingX(10).Gap(8).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(it.label)
				isSel := !nav.pointerMode && i == selected
				bui.RevealOnHighlight(row, isSel)
				hovered := nav.pointerMode && row.Hovered()
				pressed := row.Pressed()
				ais := pal.AISurface()
				bg := ui.Transparent
				if isSel || pressed {
					bg = ais.SelectedBG
				} else if hovered {
					bg = ais.HoverBG
				}
				bui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
				bui.TactileCard(row, hovered, pressed, false, false)
				bui.Probe(row, rowID)
				row.Children(func() {
					ic := icons.Sparkles
					switch it.kind {
					case pickFolder:
						ic = icons.FolderIcon(it.label, false)
					case pickPage:
						ic = icons.FileIconOrDefault(it.value)
					}
					active := isSel || hovered || pressed
					icons.DrawAnimated(cUI, ic, theme.IconRow, pal.MonoIconColors(pal.TextSecondary), active)
					pickQ := strings.TrimSpace(token[1:])
					var labelMarks [][2]int
					if pickQ != "" {
						labelMarks = fuzzy.HighlightRanges(it.label, pickQ)
					}
					bui.HighlightedText(cUI, it.label, labelMarks, theme.FontBody, 0, pal.Text, pal.SearchHighlight).Grow(1).MinWidth(0)
					if it.hint != "" {
						var hintMarks [][2]int
						if pickQ != "" && len(labelMarks) == 0 {
							hintMarks = fuzzy.HighlightRanges(it.hint, pickQ)
						}
						bui.HighlightedText(cUI, it.hint, hintMarks, theme.FontBody, 0, pal.TextSecondary, pal.SearchHighlight).Shrink(0)
					}
					if it.kind == pickFolder {
						icons.DrawAnimated(cUI, icons.ChevronRight, theme.IconInline, pal.MonoIconColors(pal.TextSecondary), active)
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
			bui.AutoHideScrollbars(cUI, pickerListEl)
		})
	})
}

func (c *Controller) renderComposerArea(cUI *ui.Context, pal theme.Palette, fullscreen bool, panelW float32) {
	c.mu.RLock()
	draft := c.draftText
	refs := append([]string(nil), c.draftRefs...)
	skills := append([]string(nil), c.draftSkills...)
	atts := append([]model.Attachment(nil), c.draftAttachments...)
	var sel *EditorSelection
	if c.open && !c.fullscreen && c.providerCfg.Enabled && c.draftSelection != nil {
		cp := *c.draftSelection
		sel = &cp
	}
	epoch := c.composerEpoch
	busy := c.streaming || c.hasPendingQuestionLocked()
	hasConv := c.currentID != 0
	backend := c.selectedBackendLocked()
	provider := resolveProviderExecutionConfig(c.providerCfg)
	nativeTools := supportsCLI(backend) && c.backendsCfg.Adapters[string(backend)].AllowsNativeTools()
	host := c.host
	selectEnd := c.composerSelectEnd
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	onPickAttachments := c.onPickAttachments
	c.mu.RUnlock()
	var notebook string
	if host != nil {
		if root := host.ActiveNotebook(); root != "" {
			notebook = filepath.Base(root)
		}
	}

	ph := theme.Tr("向 AI 提问，/ 技能，@ 文件", "Ask AI, / skills, @ files")
	if hasConv {
		ph = theme.Tr("继续追问…", "Ask a follow-up…")
	}

	padTop := float32(12)
	if pickerOpen {
		padTop = 0
	}
	outer := ui.Column(cUI.Key("ai-composer-outer")).FillWidth().Padding(padTop, theme.Space12, theme.Space12, theme.Space12)
	if fullscreen {
		outer.MaxWidth(720).Margin(0, ui.Auto)
	}

	ais := pal.AISurface()
	var deckAnchor ui.Element
	outer.Children(func() {
		shell := ui.Column(cUI.Key("ai-composer-shell")).FillWidth().Padding(theme.Space10, theme.Space10, theme.Space6, theme.Space12).Gap(theme.Space4).Radius(theme.RadiusLG).
			Background(ais.ComposerBG)
		dragOver := shell.FileDragOver()
		shellBorder := ais.ComposerBorder
		if dragOver {
			shellBorder = pal.Accent
		} else if shell.FocusWithin() {
			shellBorder = ais.ComposerFocusBorder
		} else if shell.Hovered() {
			shellBorder = pal.BtnBorder
		}
		shell.Border(1, shellBorder)
		var chipOrActionPressed bool
		shell.Children(func() {
			if dragOver {
				ui.Row(cUI.Key("ai-composer-drop-hint")).FillWidth().Height(theme.HeightMicro).
					PaddingX(theme.Space8).Gap(theme.Space6).AlignItems(ui.Center).
					Radius(theme.RadiusSM).Background(ais.TintedSelectedBG).Border(1, pal.Accent).Children(func() {
					icons.Draw(cUI, icons.Image, theme.IconMicro, pal.MonoIconColors(pal.Accent))
					ui.Text(cUI, theme.Tr("松开以添加图片或文件", "Drop images or files to attach")).
						FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.Accent).SingleLine()
				})
			}
			if len(atts) > 0 {
				attWrap := ui.Column(cUI.Key("ai-composer-atts")).FillWidth()
				attWrap.Children(func() {
					c.renderComposerAttachments(cUI, pal, atts)
				})
				if attWrap.PressedInside() {
					chipOrActionPressed = true
				}
			}
			if sel != nil || len(refs) > 0 || len(skills) > 0 {
				tagRow := ui.Row(cUI).FillWidth().Gap(theme.Space6).Wrap()
				tagRow.Children(func() {
					if sel != nil {
						c.renderSelectionChip(cUI, pal, sel)
					}
					for i, r := range refs {
						c.renderDraftChip(cUI, pal, fmt.Sprintf("chip-ref-%d", i), true, r)
					}
					for i, s := range skills {
						c.renderDraftChip(cUI, pal, fmt.Sprintf("chip-skill-%d", i), false, s)
					}
				})
				if tagRow.PressedInside() {
					chipOrActionPressed = true
				}
			}

			composerKey := fmt.Sprintf("ai-composer-%d", epoch)
			inputEl := ui.TextAreaBase(cUI.Key(composerKey), &draft).
				FillWidth().Lines(1, 6).Padding(0, 0, theme.ComposerTailSpace, 0).FontSize(theme.FontSmall).TextColor(pal.Text).
				CaretColor(pal.Accent).CaretWidth(2).
				Placeholder(ph).PlaceholderColor(pal.Placeholder).Bind(&c.composerHandle).
				Label(theme.Tr("AI 输入框", "AI composer"))
			bui.AutoHideScrollbars(cUI, inputEl)
			bui.Probe(inputEl, "ai-composer")
			svc := cUI.Services()
			inputEl.OnPaste(func(string) bool {
				return c.handleComposerPaste(svc)
			})

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
					if first, last := c.pickerList.Visible(); c.pickIndex < first || c.pickIndex > last {
						c.pickerList.ScrollTo(c.pickIndex, ui.Center)
					}
					c.mu.Unlock()
					return true
				case k.Key == ui.KeyDown && open && len(items) > 0:
					c.pickIndex = (c.pickIndex + 1) % len(items)
					if first, last := c.pickerList.Visible(); c.pickIndex < first || c.pickIndex > last {
						c.pickerList.ScrollTo(c.pickIndex, ui.Center)
					}
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
					if open && len(items) == 0 && (c.pickerLoading || (strings.HasPrefix(tok, "@") && !c.pickerInjected && c.pickerRefreshed.IsZero())) {
						c.mu.Unlock()
						return true
					}
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
					if c.streaming || (strings.TrimSpace(curText) == "" && len(c.draftAttachments) == 0) {
						c.mu.Unlock()
						return true
					}
					c.mu.Unlock()
					c.SendPrompt(curText)
					c.mu.RLock()
					remainingDraft := c.draftText
					c.mu.RUnlock()
					k.SetText(remainingDraft)
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
					c.schedulePersistActiveDraftLocked()
				}
				c.mu.Unlock()
			}

			actionsRow := ui.Row(cUI.Key("ai-composer-actions")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8)
			bui.Probe(actionsRow, "ai-composer-actions")
			actionsRow.Children(func() {
				deckAnchor = c.renderDeckTrigger(cUI, pal)

				ui.Row(cUI).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
					if onPickAttachments != nil {
						attachFull := len(atts) >= maxComposerAttachments
						attachTip := theme.Tr("添加图片或文件", "Attach image or file")
						if attachFull {
							attachTip = theme.Tr("最多添加 10 个附件", "Up to 10 attachments")
						}
						attachBtn := bui.IconButton(cUI, icons.Image, bui.IconButtonOptions{
							ID:           "ai-attach-file",
							Label:        attachTip,
							Tooltip:      attachTip,
							TooltipAbove: true,
							Size:         theme.HeightMicro,
							IconSize:     theme.IconInline,
							Disabled:     busy || attachFull,
						})
						if attachBtn.Clicked() {
							onPickAttachments()
						}
					}

					if busy {
						stopBtn := bui.ActionButton(cUI, "ai-chat-stop", theme.Tr("停止", "Stop"), nil, bui.ButtonSecondary, false)
						if stopBtn.Clicked() {
							c.StopStreaming()
						}
					} else {
						canSend := strings.TrimSpace(draft) != "" || len(atts) > 0
						dest := backend.DisplayName()
						scope := theme.Tr("发送聊天文字与附件", "Sends chat text and attachments")
						switch backend {
						case model.BackendBuiltin:
							dest = endpointHostLabel(provider.BaseURL)
							if dest == "" {
								dest = backend.DisplayName()
							}
							scope = theme.Tr("发送聊天文字、附件、当前页名称与本轮笔记工具结果", "Sends chat text, attachments, the current page name, and this turn’s notebook tool results")
						case model.BackendClaude, model.BackendCodex, model.BackendAgy:
							scope = theme.Tr("发送聊天文字、附件与本轮笔记工具结果", "Sends chat text, attachments, and this turn’s notebook tool results")
						}
						sendTip := bui.TooltipSpec{
							Placement: bui.TooltipAbove,
							Rows: []bui.TooltipRow{
								{Detail: scope + " · " + dest},
								{
									Trigger:     "Enter",
									TriggerTone: bui.TooltipToneAccent,
									Detail:      theme.Tr("发送消息", "to send"),
								},
								{
									Trigger: "⇧ + Enter",
									Detail:  theme.Tr("换行输入", "for new line"),
								},
							},
						}
						if notebook != "" && (backend == model.BackendBuiltin || backend == model.BackendClaude || backend == model.BackendCodex || backend == model.BackendAgy) {
							label := fmt.Sprintf(theme.Tr("Block 笔记工具可读取「%s」内已保存的 Markdown", "Block’s note tools may read saved Markdown in %s"), notebook)
							sendTip.Rows = append(sendTip.Rows, bui.TooltipRow{Detail: label})
						}
						if nativeTools {
							sendTip.Rows = append(sendTip.Rows, bui.TooltipRow{
								Detail: theme.Tr("本机工具已启用，可运行命令和访问本机文件。", "Local tools are enabled and can run commands and access local files."),
							})
						}
						sendBtn := bui.IconButton(cUI, icons.ArrowUp, bui.IconButtonOptions{
							ID:          "ai-chat-send",
							Label:       theme.Tr("发送", "Send") + " · " + scope + " · " + dest,
							TooltipSpec: &sendTip,
							Size:        theme.HeightMicro,
							IconSize:    theme.IconMicro,
							Active:      canSend,
							Disabled:    !canSend,
						})
						if sendBtn.Clicked() && canSend {
							c.SendPrompt(draft)
						}
					}
				})
			})
			if actionsRow.PressedInside() {
				chipOrActionPressed = true
			}
		})
		if shell.Clicked() && !chipOrActionPressed {
			c.composerHandle.Focus()
		}
		if dropped := shell.DroppedFiles(); len(dropped) > 0 {
			c.AddAttachmentFiles(dropped)
		}
	})

	if deckAnchor.Valid() {
		c.renderDeckPopover(cUI, pal, deckAnchor, fullscreen, panelW)
	}
}

func (c *Controller) renderSelectionChip(cUI *ui.Context, pal theme.Palette, sel *EditorSelection) {
	if sel == nil {
		return
	}
	label := selectionExcerpt(sel, 16)
	chip := bui.ChipSurface(ui.Row(cUI.Key("ai-selection-chip")), false)
	bui.Probe(chip, "ai-selection-chip")
	if sel.PlainPreview != "" {
		bui.Tooltip(cUI, chip, sel.PlainPreview)
	}
	chip.Children(func() {
		icons.Draw(cUI, icons.Quote, theme.IconMicro, pal.MonoIconColors(pal.TextSecondary))
		ui.Text(cUI, label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
		xBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
			ID:       "ai-selection-chip-x",
			Label:    theme.Tr("移除选中内容", "Remove selection"),
			Size:     theme.HeightMicro - theme.Space6,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusXS,
		})
		if xBtn.Clicked() {
			c.DismissEditorSelection()
		}
	})
}

func (c *Controller) renderDraftChip(cUI *ui.Context, pal theme.Palette, id string, isRef bool, name string) {
	ais := pal.AISurface()
	label := name
	ic := icons.Note
	if isRef {
		label = filepath.Base(name)
	} else {
		label = "/" + name
		ic = icons.Sparkles
	}
	chip := ui.Row(cUI.Key(id)).Height(theme.HeightCompact).Padding(0, theme.Space4, 0, theme.Space8).Gap(theme.Space4).AlignItems(ui.Center).Radius(theme.RadiusMD)
	hovered := chip.Hovered()
	border := ais.CapsuleBorder
	bg := ais.CapsuleBG
	if hovered {
		border = pal.BtnBorder
		bg = ais.HoverBG
	}
	bui.AnimateSurfaceColors(chip, bg, border, true)
	chip.Children(func() {
		icons.DrawAnimated(cUI, ic, theme.IconAction, pal.MonoIconColors(pal.Accent), hovered)
		ui.Text(cUI, label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine().MaxWidth(180).Ellipsis("…")
		xBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
			ID:       id + "-x",
			Label:    theme.Tr("移除 ", "Remove ") + label,
			Size:     theme.HeightMicro - theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusXS,
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
	mdl := c.effectiveBackendModelLocked(kind)
	mdlLbl := c.modelLabelLocked(kind, mdl)
	fullLbl := c.modelFullLabelLocked(kind, mdl)
	thinking := c.backendThinkingLocked(kind)
	open := c.deckOpen
	fullscreen := c.fullscreen
	panelW := c.renderWidth
	if panelW <= 0 {
		panelW = c.panelWidth
	}
	c.mu.RUnlock()

	label := backendLabel(kind)
	if mdlLbl != "" {
		label = mdlLbl
	}
	thinkLbl := thinkingDisplayLabel(thinking)
	tipLbl := fullLbl
	if tipLbl == "" {
		tipLbl = label
	}
	tipText := tipLbl + " · " + thinkLbl

	maxTrigW := float32(212)
	if fullscreen {
		maxTrigW = 360
	} else if panelW > 0 {
		maxTrigW = min(max(panelW-68, 140), 340)
	}

	ais := pal.AISurface()
	btn := ui.ButtonBase(cUI.Key("ai-deck-trigger")).Height(theme.HeightMicro).PaddingX(theme.Space6).Gap(theme.Space4).Margin(0, 0, 0, -theme.Space6).
		MaxWidth(maxTrigW).Shrink(1).MinWidth(0).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(label)
	bui.Tooltip(cUI, btn, tipText)
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	bg := ui.Transparent
	if open {
		bg = ais.TintedSelectedBG
	} else if pressed {
		bg = ais.SelectedBG
	} else if hovered {
		bg = ais.HoverBG
	}
	bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	bui.TactileCard(btn, hovered, pressed, false, false)
	if b := btn.Bounds(); b.W > 0 {
		c.mu.Lock()
		c.deckTriggerBox = b
		c.mu.Unlock()
	}
	bui.Probe(btn, "ai-deck-trigger")

	btn.Children(func() {
		icons.Brand(cUI, kind, theme.IconMicro, pal.TextSecondary)
		ui.Text(cUI, label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
		ui.Text(cUI, "· "+thinkLbl).FontSize(theme.FontMeta).TextColor(pal.Placeholder).SingleLine().Shrink(0)
		chev := icons.ChevronDown
		if open {
			chev = icons.ChevronUp
		}
		icons.DrawAnimated(cUI, chev, 10, pal.MonoIconColors(pal.Placeholder), hovered || pressed || open)
	})
	if btn.Clicked() {
		c.mu.Lock()
		c.deckOpen = !c.deckOpen
		nextOpen := c.deckOpen
		if nextOpen {
			c.historyOpen = false
			c.pickerDismissed = true
			c.deckSearch = ""
		}
		shouldProbe := nextOpen && !c.mockResponder
		c.mu.Unlock()
		if shouldProbe {
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
	curModel := c.effectiveBackendModelLocked(selected)
	curThinking := c.backendThinkingLocked(selected)
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
		railW = 54
		railPadY, railPadX, railGap = 10, 6, 8
		btnSz, icoSz = 38, 20
		rowH, rowPX = 38, 12
	}
	if fullscreen {
		listH = 276
	}
	overhead := headerH + 4
	if !wide {
		overhead += 38
	}
	effectiveListH := min(listH, max(maxH-overhead, 64))
	popH := min(effectiveListH+overhead, maxH)
	x, y := bui.ResolveOrigin(spec, popW, popH, winW, winH)

	ais := pal.AISurface()
	ui.Overlay(cUI, func() {
		panel := bui.Elevate(ui.Column(cUI.Key("ai-agent-deck")).Absolute().Left(x).Top(y).
			Width(popW).MaxHeight(maxH).Radius(theme.RadiusXL).
			Background(pal.Menu).Border(1, pal.MenuBorder).Clip().
			Transition(probeSafeTransition(bui.PopoverSpringTransition())), theme.ElevationPopover)
		bui.Probe(panel, "ai-deck")

		panel.Children(func() {
			// Full-height split: left backend icon rail (with AI Settings pinned at bottom) + right model/thinking split.
			ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Children(func() {
				// 1. Left backend icon rail (recessed surface, full height).
				_ = railPadX
				ui.Column(cUI).Width(railW).Shrink(0).Padding(railPadY, 0).Gap(railGap).
					Background(ais.RailBG).BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline).
					AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Column(cUI).FillWidth().Gap(railGap).AlignItems(ui.Center).Children(func() {
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
							ui.Box(cUI.Key("wrap-" + cardID)).FillWidth().Height(btnSz).Center().Children(func() {
								btn := ui.ButtonBase(cUI.Key(cardID)).Size(btnSz, btnSz).Center().
									Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
									Label(backendLabel(k))
								bui.Tooltip(cUI, btn, backendLabel(k))
								hovered := btn.Hovered()
								pressed := btn.Pressed()
								bg := ui.Transparent
								if active || pressed {
									bg = ais.SelectedBG
								} else if hovered {
									bg = ais.HoverBG
								}
								bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
								bui.TactileButton(btn, hovered, pressed, false)
								bui.Probe(btn, cardID)
								btn.Children(func() {
									icCol := pal.TextSecondary
									if active || hovered || pressed {
										icCol = pal.Text
									}
									icons.Brand(cUI, k, icoSz, icCol)
									dot := ui.Box(cUI.Key("dot-"+cardID)).Absolute().Right(4).Bottom(4).Size(5, 5).
										Radius(theme.RadiusPill).Background(pal.TextSecondary)
									targetDot := float32(0)
									if !ready {
										targetDot = 1.0
										if hovered || active {
											targetDot = 1.25
										}
									}
									dotSc := dot.AnimateTo("dotSc", targetDot, bui.SpringTactile)
									if dotSc > 0.02 {
										dot.Scale(dotSc, dotSc).Opacity(min(dotSc, 1))
									} else {
										dot.Opacity(0)
									}
								})
								if active {
									ui.Box(cUI).Absolute().Right(0).Top(8).Size(2.5, btnSz-16).
										Radius(theme.RadiusPill).Background(pal.Accent)
								}
								if btn.Clicked() {
									c.mu.Lock()
									if c.streaming {
										c.turnError = theme.Tr("请等待当前回复结束后再切换智能体", "Wait for the current response before changing agent")
										c.mu.Unlock()
									} else {
										changed := c.selectedBackendLocked() != k
										c.selectBackendLocked(k)
										if changed {
											c.deckSearch = ""
										}
										shouldRefresh := !c.mockResponder
										c.mu.Unlock()
										if shouldRefresh {
											c.refreshModelsAsync(k)
										}
									}
								}
							})
						}
					})

					setLbl := theme.Tr("AI 设置", "AI settings")
					ui.Box(cUI.Key("wrap-ai-deck-settings")).FillWidth().Height(btnSz).Center().Children(func() {
						setBtn := ui.ButtonBase(cUI.Key("ai-deck-settings")).Size(btnSz, btnSz).Center().
							Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(setLbl)
						bui.Tooltip(cUI, setBtn, setLbl)
						setHov := setBtn.Hovered()
						setPrs := setBtn.Pressed()
						setBG := ui.Transparent
						if setPrs {
							setBG = ais.SelectedBG
						} else if setHov {
							setBG = ais.HoverBG
						}
						bui.AnimateSurfaceColors(setBtn, setBG, ui.Transparent, false)
						bui.TactileButton(setBtn, setHov, setPrs, false)
						bui.Probe(setBtn, "ai-deck-settings")
						setBtn.Children(func() {
							icCol := pal.TextSecondary
							if setHov || setPrs {
								icCol = pal.Text
							}
							icons.DrawAnimated(cUI, icons.Sliders, icoSz-2, pal.MonoIconColors(icCol), setHov || setPrs)
						})
						if setBtn.Clicked() {
							c.mu.Lock()
							c.deckOpen = false
							c.mu.Unlock()
							if c.host != nil {
								c.host.OpenSettings("ai")
							}
						}
					})
				})

				ui.Column(cUI).Grow(1).MinWidth(0).Children(func() {
					ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Children(func() {
						// 2. Center search + model list (+ compact thinking row when !wide).
						centerCol := ui.Column(cUI).Grow(1).MinWidth(0)
						type deckModelCursorKey struct{}
						deckCursor := ui.Local(centerCol, deckModelCursorKey{}, func() int { return -1 })
						bui.ObserveMenuPointer(cUI, centerCol, deckCursor)
						type deckModelFilterCacheKey struct{}
						type deckModelFilterCache struct {
							valid    bool
							query    string
							count    int
							hash     uint64
							filtered []ModelOption
						}
						deckCache := ui.Local(centerCol, deckModelFilterCacheKey{}, func() deckModelFilterCache { return deckModelFilterCache{} })
						filterDeckModels := func(rawQ string) []ModelOption {
							q := strings.TrimSpace(rawQ)
							if q == "" {
								return models
							}
							mh := hashModelOptions64(models)
							if deckCache.valid && deckCache.query == q && deckCache.count == len(models) && deckCache.hash == mh {
								return deckCache.filtered
							}
							type scoredModel struct {
								opt   ModelOption
								score int
							}
							var scored []scoredModel
							for _, m := range models {
								if sc, ok := fuzzy.Score(q, []string{m.Label, m.Value}, []string{m.Group}, true); ok {
									scored = append(scored, scoredModel{opt: m, score: sc})
								}
							}
							sort.SliceStable(scored, func(i, j int) bool {
								return scored[i].score > scored[j].score
							})
							out := make([]ModelOption, len(scored))
							for i, s := range scored {
								out[i] = s.opt
							}
							*deckCache = deckModelFilterCache{
								valid: true, query: q, count: len(models), hash: mh, filtered: out,
							}
							return out
						}
						lowQ := strings.TrimSpace(search)
						filtered := filterDeckModels(lowQ)
						if *deckCursor >= len(filtered) {
							*deckCursor = len(filtered) - 1
						}
						centerCol.Children(func() {
							ui.Row(cUI).FillWidth().Height(headerH).PaddingX(headerPX).Gap(8).AlignItems(ui.Center).
								BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
								icons.Draw(cUI, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
								q := search
								sInput := ui.TextInputBase(cUI.Key("ai-deck-search"), &q).
									Grow(1).Height(24).Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).
									CaretColor(pal.Accent).CaretWidth(2).
									Placeholder(theme.Tr("搜索模型…", "Search models…")).PlaceholderColor(pal.Placeholder).
									Bind(&c.deckSearchHandle).AutoFocus()
								bui.Probe(sInput, "ai-deck-search")
								if sInput.Changed() {
									*deckCursor = 0
									c.deckScroll.Y = 0
									lowQ = strings.TrimSpace(q)
									filtered = filterDeckModels(lowQ)
									c.mu.Lock()
									c.deckSearch = q
									c.mu.Unlock()
								}
								sInput.OnTextKey(func(k ui.TextKey) bool {
									switch k.Key {
									case ui.KeyEscape:
										c.mu.Lock()
										c.deckOpen = false
										c.mu.Unlock()
										return true
									case ui.KeyDown:
										if len(filtered) > 0 {
											*deckCursor = (*deckCursor + 1 + len(filtered)) % len(filtered)
										}
										return true
									case ui.KeyUp:
										if len(filtered) > 0 {
											if *deckCursor <= 0 {
												*deckCursor = len(filtered) - 1
											} else {
												*deckCursor--
											}
										}
										return true
									case ui.KeyEnter:
										if *deckCursor >= 0 && *deckCursor < len(filtered) {
											pickVal := filtered[*deckCursor].Value
											c.mu.Lock()
											if c.streaming {
												c.turnError = theme.Tr("请等待当前回复结束后再切换模型", "Wait for the current response before changing model")
											} else {
												_ = c.setBackendModelLocked(selected, pickVal)
												c.deckOpen = false
											}
											c.mu.Unlock()
											return true
										}
									}
									return false
								})
							})

							bui.Scroll(cUI.Key("ai-deck-models"), &c.deckScroll).
								FillWidth().Height(effectiveListH).Padding(listPad).Gap(listGap).Children(func() {
								if len(filtered) == 0 {
									msg := theme.Tr("暂无可选模型", "No models available")
									if lowQ != "" {
										msg = theme.Tr("无匹配模型", "No matching models")
									} else if mStatus == "loading" {
										msg = theme.Tr("正在获取模型…", "Loading models…")
									} else if strings.HasPrefix(mStatus, "error:") {
										msg = theme.Tr("无法获取模型", "Models unavailable")
									}
									ui.Box(cUI).Height(36).PaddingX(12).AlignItems(ui.Center).Children(func() {
										ui.Text(cUI, msg).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
									})
									return
								}
								lastGroup := ""
								for idx, m := range filtered {
									if grp := strings.TrimSpace(m.Group); grp != "" && grp != lastGroup {
										lastGroup = grp
										topPad := float32(6)
										if idx == 0 {
											topPad = 2
										}
										ui.Box(cUI.Key(fmt.Sprintf("ai-model-grp-%d-%s", idx, grp))).FillWidth().Padding(topPad, rowPX, 2, rowPX).Children(func() {
											ui.Text(cUI, grp).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
										})
									}
									active := m.Value == curModel
									highlighted := idx == *deckCursor
									mID := fmt.Sprintf("ai-model-%d", idx)
									row := ui.ButtonBase(cUI.Key(mID)).FillWidth().Height(rowH).PaddingX(rowPX).Gap(8).
										AlignItems(ui.Center).Radius(theme.RadiusMD).
										Cursor(ui.CursorPointer).Label(m.Label).
										Transition(probeSafeTransition(bui.CascadeItemTransition(idx, false)))
									bui.RevealOnHighlight(row, highlighted)
									hovered := highlighted || (*deckCursor < 0 && row.Hovered())
									pressed := row.Pressed()
									bg := ui.Transparent
									if active || pressed {
										bg = ais.SelectedBG
									} else if hovered {
										bg = ais.HoverBG
									}
									bui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
									bui.TactileCard(row, hovered, pressed, false, false)
									bui.Probe(row, mID)
									row.Children(func() {
										fw := theme.WeightRegular
										if active {
											fw = theme.WeightControl
										}
										var marks [][2]int
										if lowQ != "" {
											marks = fuzzy.HighlightRanges(m.Label, lowQ)
										}
										bui.HighlightedText(cUI, m.Label, marks, theme.FontBody, fw, pal.Text, pal.SearchHighlight).Grow(1).MinWidth(0)
										if active {
											icons.DrawAnimated(cUI, icons.Check, 14, pal.MonoIconColors(pal.Text), active)
										}
									})
									if row.Clicked() {
										c.mu.Lock()
										if c.streaming {
											c.turnError = theme.Tr("请等待当前回复结束后再切换模型", "Wait for the current response before changing model")
										} else {
											_ = c.setBackendModelLocked(selected, m.Value)
											c.deckOpen = false
										}
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
									ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
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
									ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
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
											AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusMD).
											Cursor(ui.CursorPointer).Label(lv.label)
										hovered := btn.Hovered()
										pressed := btn.Pressed()
										bg := ui.Transparent
										if isAct || pressed {
											bg = ais.SelectedBG
										} else if hovered {
											bg = ais.HoverBG
										}
										bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
										bui.TactileCard(btn, hovered, pressed, false, false)
										bui.Probe(btn, btnID)
										btn.Children(func() {
											fw := theme.WeightRegular
											if isAct {
												fw = theme.WeightControl
											}
											ui.Text(cUI, lv.label).FontSize(theme.FontBody).FontWeight(fw).TextColor(pal.Text)
											if isAct {
												icons.DrawAnimated(cUI, icons.Check, 14, pal.MonoIconColors(pal.Text), isAct)
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
				})
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

func settingsSelectedSurface(pal theme.Palette) ui.Color {
	return pal.AISurface().TintedSelectedBG
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

// RenderSettingsTab renders the Settings > AI Assistant tab.
func (c *Controller) RenderSettingsTab(cUI *ui.Context) {
	c.mu.Lock()
	c.services = cUI.Services()
	shouldProbe := c.providerCfg.Enabled && !c.settingsProbedOnce && !c.mockResponder
	if shouldProbe {
		c.settingsProbedOnce = true
	}
	initAnnotKind := c.annotCfg.Backend
	if initAnnotKind == "" {
		initAnnotKind = model.BackendBuiltin
	}
	c.mu.Unlock()
	if shouldProbe {
		c.probeAllBackendsAsync()
		c.refreshAnnotationModelsAsync(initAnnotKind)
	}

	pal := theme.Ocean()

	c.mu.RLock()
	cfg := cloneProviderConfig(c.providerCfg)
	backends := cloneBackendsConfig(c.backendsCfg)
	recognized := c.recognizedBackendsLocked()
	cliAgents := c.addedCLIAgentsLocked()
	expandedProv := c.settingsExpandedProv
	expandedAg := c.settingsExpandedAg
	testing := c.settingsTesting
	testingProv := c.settingsTestingProv
	testOK := c.settingsTestOK
	testErr := c.settingsTestErr
	if c.providerError != nil {
		testErr = providerCredentialsMessage(c.providerError)
	}
	annotCfg := c.annotCfg
	annotOpen := c.annotMenuOpen
	provModalOpen := c.addProvModalOpen
	agentModalOpen := c.addAgentModalOpen
	c.mu.RUnlock()

	provEntries := orderedProviderEntries(cfg)
	if expandedProv != "" {
		found := false
		for _, p := range provEntries {
			if p.ID == expandedProv {
				found = true
				break
			}
		}
		if !found && len(provEntries) > 0 {
			expandedProv = provEntries[0].ID
		}
	}

	root := ui.Column(cUI.Key("ai-settings-section")).FillWidth().MinWidth(0).Gap(theme.SettingsSectionGap)
	bui.Probe(root, "ai-settings-section")

	secW := root.Bounds().W
	if secW <= 0 {
		winW, _ := cUI.Size()
		if winW <= 0 {
			winW = 860
		}
		if winW >= 920 {
			secW = winW - 240 - 56
		} else if winW >= 560 {
			secW = winW - 56
		} else {
			secW = max(winW-32, 280)
		}
	}

	root.Children(func() {
		// 0. Master AI Assistant toggle row.
		bui.SettingsSection(cUI, func() {
			en := cfg.Enabled
			row := bui.SettingsRow(cUI, theme.Tr("启用 AI 助手", "Enable AI Assistant"), func() {
				if bui.TogglePill(cUI, "ai-enabled-switch", theme.Tr("启用 AI 助手", "Enable AI Assistant"), &en, false) {
					cfg.Enabled = en
					_ = c.SaveProviderConfig(cfg)
				}
			})
			row.BorderWidth(1, 0, 1, 0).BorderColor(pal.Hairline)
		})

		if !cfg.Enabled {
			return
		}

		// 1. API Providers: flat hairline-separated rows + "+ 添加" text action.
		bui.SettingsSection(cUI, func() {
			bui.SettingsSectionTitle(cUI, theme.Tr("API 供应商", "API Providers"), func() {
				plusIcon := icons.Plus
				addBtn := bui.SettingsTextAction(cUI, "ai-add-provider-btn", theme.Tr("添加", "Add"), &plusIcon)
				addBtn.Label(theme.Tr("添加供应商", "Add Provider"))
				if addBtn.Clicked() {
					c.openAddProviderModal()
				}
			})

			ui.Column(cUI.Key("ai-providers-card")).FillWidth().MinWidth(0).Children(func() {
				for _, entry := range provEntries {
					p := entry
					isExpanded := expandedProv == p.ID
					rowID := "ai-provider-row-" + p.ID
					toggleID := "ai-provider-toggle-" + p.ID

					ui.Column(cUI.Key("prov-item-"+p.ID)).FillWidth().MinWidth(0).
						BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
						row := ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.SettingsListRowHeight).
							Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween)
						rowHov := row.Hovered()

						toggleExpand := func() {
							c.mu.Lock()
							nextExp := p.ID
							if isExpanded {
								nextExp = ""
							}
							c.settingsExpandedProv = nextExp
							c.settingsTestErr = ""
							c.settingsTestOK = false
							c.mu.Unlock()
							if nextExp != "" && p.Enabled {
								_ = c.ActivateProviderEntry(p.ID)
							}
						}

						row.Children(func() {
							selBtn := ui.ButtonBase(cUI.Key(rowID)).Grow(1).MinWidth(0).Height(theme.SettingsListRowHeight).
								Gap(theme.Space10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(p.Name)
							bui.Probe(selBtn, rowID)
							selBtn.Children(func() {
								ui.Box(cUI).Size(20, 20).Shrink(0).Center().Children(func() {
									icons.DrawAnimated(cUI, p.Icon, theme.IconRow, pal.MonoIconColors(p.BrandColor), isExpanded || rowHov)
								})
								ui.Text(cUI, p.Name).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
									FontWeight(theme.WeightRegular).TextColor(pal.Text).
									Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
								if p.Host != "" && secW >= 380 {
									ui.Text(cUI, p.Host).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.TextSecondary).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
								}
								ui.Box(cUI).Grow(1).MinWidth(theme.Space4)
							})
							if selBtn.Clicked() {
								toggleExpand()
							}

							ui.Row(cUI).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
								chev := icons.ChevronDown
								chevLbl := theme.Tr("展开", "Expand")
								if isExpanded {
									chev = icons.ChevronUp
									chevLbl = theme.Tr("收起", "Collapse")
								}
								chevBtn := ui.ButtonBase(cUI.Key(rowID+"-chev")).Size(theme.HeightChrome, theme.HeightChrome).
									Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(chevLbl + " " + p.Name)
								if chevBtn.Hovered() {
									chevBtn.Background(pal.Hover)
								}
								chevBtn.Children(func() {
									icons.Draw(cUI, chev, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
								})
								if chevBtn.Clicked() {
									toggleExpand()
								}

								on := p.Enabled
								if bui.TogglePill(cUI, toggleID, p.Name, &on, false) {
									if err := c.SetProviderEntryEnabled(p.ID, on); err != nil {
										c.mu.Lock()
										c.settingsExpandedProv = p.ID
										c.settingsTestErr = err.Error()
										c.mu.Unlock()
									}
								}
							})
						})

						if isExpanded {
							c.renderProviderDetailRows(cUI, pal, p, len(provEntries) > 1, testing && (testingProv == "" || testingProv == p.ID), testOK && (testingProv == "" || testingProv == p.ID), testErr, secW)
						}
					})
				}
			})
		})

		// 2. Local CLI Agents: flat hairline-separated rows + "+ 添加" text action.
		bui.SettingsSection(cUI, func() {
			bui.SettingsSectionTitle(cUI, theme.Tr("本机 Agent", "Local Agents"), func() {
				plusIcon := icons.Plus
				addAgBtn := bui.SettingsTextAction(cUI, "ai-add-agent-btn", theme.Tr("添加", "Add"), &plusIcon)
				addAgBtn.Label(theme.Tr("添加 Agent", "Add Agent"))
				if addAgBtn.Clicked() {
					c.openAddAgentModal()
				}
			})

			ui.Column(cUI.Key("ai-agents-card")).FillWidth().MinWidth(0).Children(func() {
				if len(cliAgents) == 0 {
					bui.SettingsRowFrame(ui.Row(cUI.Key("ai-agents-empty"))).MinHeight(theme.SettingsListRowHeight).
						Justify(ui.SpaceBetween).Children(func() {
						ui.Text(cUI, theme.Tr("尚未添加本机 Agent", "No local agents added")).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).
							Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						empBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
							ID:       "ai-empty-add-agent",
							Label:    theme.Tr("立即添加", "Add Now"),
							Style:    bui.ButtonSecondary,
							Height:   theme.HeightCompact,
							PaddingX: theme.Space10,
						})
						if empBtn.Clicked() {
							c.openAddAgentModal()
						}
					})
					return
				}

				for _, kind := range cliAgents {
					k := kind
					opt := backends.Adapters[string(k)]
					enabled := isBackendEnabled(backends, k)
					suffix := backendProbeSuffix(k)
					cardID := "ai-backend-card-" + suffix
					toggleID := "ai-backend-toggle-" + suffix
					isExpanded := expandedAg == k
					lbl := strings.TrimSpace(opt.Name)
					if lbl == "" {
						lbl = backendLabel(k)
					}

					c.mu.RLock()
					thinking := c.backendThinkingLocked(k)
					st, hasSt := c.backendStatuses[k]
					c.mu.RUnlock()
					verTag := formatBackendVersionTag(st, hasSt)
					if verTag == "" && hasSt && st.State == BackendStateMissing {
						verTag = theme.Tr("未检测到 CLI", "CLI not found")
					}

					ui.Column(cUI.Key("ag-item-"+suffix)).FillWidth().MinWidth(0).
						BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
						row := ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.SettingsListRowHeight).
							Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween)
						rowHov := row.Hovered()

						toggleExpand := func() {
							c.mu.Lock()
							if isExpanded {
								c.settingsExpandedAg = ""
							} else {
								c.settingsExpandedAg = k
							}
							c.settingsFocused = k
							c.mu.Unlock()
						}

						row.Children(func() {
							selBtn := ui.ButtonBase(cUI.Key(cardID)).Grow(1).MinWidth(0).Height(theme.SettingsListRowHeight).
								Gap(theme.Space10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(lbl)
							bui.Probe(selBtn, cardID)
							selBtn.Children(func() {
								ui.Box(cUI).Size(20, 20).Shrink(0).Center().Children(func() {
									icons.Brand(cUI, k, theme.IconRow, pal.Text)
								})
								ui.Text(cUI, lbl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
									FontWeight(theme.WeightRegular).TextColor(pal.Text).
									Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
								if verTag != "" {
									ui.Text(cUI, verTag).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
										TextColor(pal.TextSecondary).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
								}
								ui.Box(cUI).Grow(1).MinWidth(theme.Space4)
							})
							if selBtn.Clicked() {
								toggleExpand()
							}

							ui.Row(cUI).Gap(theme.Space6).AlignItems(ui.Center).Shrink(0).Children(func() {
								delAg := bui.IconButton(cUI, icons.Trash, bui.IconButtonOptions{
									ID:      "ai-agent-delete-" + suffix,
									Label:   theme.Tr("移除 Agent", "Remove Agent"),
									Tooltip: theme.Tr("移除 Agent", "Remove Agent"),
									Variant: bui.IconPlain,
									Danger:  true,
								})
								if !rowHov && !delAg.Hovered() {
									delAg.Opacity(0)
								}
								if delAg.Clicked() {
									if err := c.RemoveAgentEntry(k); err != nil {
										c.mu.Lock()
										c.settingsTestErr = err.Error()
										c.mu.Unlock()
									}
								}

								chev := icons.ChevronDown
								chevLbl := theme.Tr("展开", "Expand")
								if isExpanded {
									chev = icons.ChevronUp
									chevLbl = theme.Tr("收起", "Collapse")
								}
								chevBtn := ui.ButtonBase(cUI.Key(cardID+"-chev")).Size(theme.HeightChrome, theme.HeightChrome).
									Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(chevLbl + " " + lbl)
								if chevBtn.Hovered() {
									chevBtn.Background(pal.Hover)
								}
								chevBtn.Children(func() {
									icons.Draw(cUI, chev, theme.IconInline, pal.MonoIconColors(pal.TextSecondary))
								})
								if chevBtn.Clicked() {
									toggleExpand()
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
						})

						if isExpanded {
							c.renderAddedAgentDetailRows(cUI, pal, k, opt, thinking, st, hasSt, secW)
						}
					})
				}
			})
		})

		// Programmatic priority probes for backend promotion.
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

		// 3. General Features (`普通功能` -> `标注功能`).
		annotAnchor := c.renderGeneralFeaturesSection(cUI, pal, annotCfg, annotOpen)

		if cfg.Enabled && annotOpen {
			c.renderAnnotationModelPopover(cUI, pal, recognized, backends, annotAnchor)
		}

		if provModalOpen {
			c.renderAddProviderModal(cUI, pal)
		}
		if agentModalOpen {
			c.renderAddAgentModal(cUI, pal, backends)
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

func (c *Controller) renderProviderDetailRows(cUI *ui.Context, pal theme.Palette, p ProviderEntry, canDelete, testing, testOK bool, testErr string, secW float32) {
	c.mu.RLock()
	showKey := c.settingsProvShowKey[p.ID] || c.settingsShowKey
	c.mu.RUnlock()

	stackFields := secW > 0 && secW < 420
	lblW := theme.SettingsFormLabelWidth
	indentLeft := theme.Space24 + theme.Space6
	if stackFields {
		indentLeft = theme.Space16
	}

	ui.Column(cUI.Key("prov-detail-"+p.ID)).FillWidth().MinWidth(0).
		Padding(theme.Space2, 0, theme.Space16, indentLeft).Gap(theme.Space10).Children(func() {
		// Protocol
		protoIdx := 0
		if p.Protocol == model.ProtocolAnthropic {
			protoIdx = 1
		}
		renderProtoControls := func() {
			if bui.SettingsSegmented(cUI, "ai-prov-proto-"+p.ID, &protoIdx, []bui.SegmentOption{
				{ID: "ai-prov-openai-" + p.ID, Label: theme.Tr("OpenAI 协议", "OpenAI")},
				{ID: "ai-prov-claude-" + p.ID, Label: theme.Tr("Claude 协议", "Claude")},
			}) {
				nextProto := model.ProtocolOpenAI
				if protoIdx == 1 {
					nextProto = model.ProtocolAnthropic
				}
				_ = c.UpdateProviderEntry(p.ID, func(prof *model.ProviderProfile) {
					prof.Protocol = nextProto
				})
			}
		}
		if stackFields {
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				ui.Text(cUI, theme.Tr("协议类型", "Protocol")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				renderProtoControls()
			})
		} else {
			ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Text(cUI, theme.Tr("协议类型", "Protocol")).Width(lblW).Shrink(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				renderProtoControls()
			})
		}

		// Base URL
		renderBaseURLInput := func() {
			u := p.BaseURL
			in := ui.TextInput(cUI.Key("ai-settings-baseurl-"+p.ID), &u).Grow(1).MinWidth(0).FillWidth().
				FontSize(theme.FontBody).Placeholder("https://api.example.com/v1").PlaceholderColor(pal.Placeholder)
			bui.Probe(in, "ai-settings-baseurl")
			if in.Changed() {
				_ = c.UpdateProviderEntry(p.ID, func(prof *model.ProviderProfile) {
					prof.BaseURL = u
				})
			}
		}
		if stackFields {
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				ui.Text(cUI, theme.Tr("接口地址", "Base URL")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				renderBaseURLInput()
			})
		} else {
			ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Text(cUI, theme.Tr("接口地址", "Base URL")).Width(lblW).Shrink(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				renderBaseURLInput()
			})
		}

		// API Key
		renderAPIKeyControls := func() {
			ui.Row(cUI).Grow(1).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
				k := p.APIKey
				in := ui.TextInput(cUI.Key("ai-settings-apikey-"+p.ID), &k).Grow(1).MinWidth(0).
					FontSize(theme.FontBody).Placeholder("sk-…").PlaceholderColor(pal.Placeholder)
				if !showKey {
					in.Password()
				}
				bui.Probe(in, "ai-settings-apikey")
				if in.Changed() {
					_ = c.UpdateProviderEntry(p.ID, func(prof *model.ProviderProfile) {
						prof.APIKey = k
					})
				}
				toggleLbl := theme.Tr("显示", "Show")
				toggleIc := icons.Eye
				if showKey {
					toggleLbl = theme.Tr("隐藏", "Hide")
					toggleIc = icons.EyeOff
				}
				tBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
					ID:       "ai-toggle-key",
					Label:    toggleLbl,
					Leading:  &toggleIc,
					Style:    bui.ButtonGhost,
					Height:   theme.HeightCompact,
					PaddingX: theme.Space8,
				})
				if tBtn.Clicked() {
					c.mu.Lock()
					if c.settingsProvShowKey == nil {
						c.settingsProvShowKey = make(map[string]bool)
					}
					c.settingsProvShowKey[p.ID] = !showKey
					c.mu.Unlock()
				}
			})
		}
		if stackFields {
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				ui.Text(cUI, theme.Tr("API 密钥", "API Key")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				renderAPIKeyControls()
			})
		} else {
			ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Text(cUI, theme.Tr("API 密钥", "API Key")).Width(lblW).Shrink(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				renderAPIKeyControls()
			})
		}

		// Delete Provider & Test Connection
		ui.Row(cUI).FillWidth().MinWidth(0).Padding(theme.Space2, 0, 0, 0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			trashIc := icons.Trash
			delBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
				ID:       "ai-provider-delete-" + p.ID,
				Label:    theme.Tr("删除", "Delete"),
				Leading:  &trashIc,
				Style:    bui.ButtonDanger,
				Disabled: !canDelete,
				Height:   theme.HeightCompact,
				PaddingX: theme.Space10,
			})
			if !canDelete {
				bui.Tooltip(cUI, delBtn, theme.Tr("至少保留一个 API 供应商", "Keep at least one API provider"))
			} else if delBtn.Clicked() {
				_ = c.RemoveProviderEntry(p.ID)
			}

			testLabel := theme.Tr("连接测试", "Test Connection")
			testIc := icons.Zap
			testStyle := bui.ButtonSecondary
			testAlwaysDuo := true
			if testing {
				testLabel = theme.Tr("测试中…", "Testing…")
			} else if testOK {
				testLabel = theme.Tr("连接正常", "Connected")
				testIc = icons.Check
				testStyle = bui.ButtonSuccess
				testAlwaysDuo = false
			}
			testBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
				ID:        "ai-test",
				Label:     testLabel,
				Leading:   &testIc,
				AlwaysDuo: testAlwaysDuo,
				Style:     testStyle,
				Disabled:  testing,
				Loading:   testing,
				Height:    theme.HeightCompact,
				PaddingX:  theme.Space10,
			})
			if testBtn.Clicked() && !testing {
				c.runProviderConnectionTestAsync(p.ID)
			}
		})

		if testErr != "" {
			ui.Text(cUI, testErr).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Danger)
		}
	})
}

func (c *Controller) renderAddedAgentDetailRows(cUI *ui.Context, pal theme.Palette, k model.BackendKind, opt model.AIBackendAdapterOptions, thinking string, st BackendStatus, hasSt bool, secW float32) {
	suffix := backendProbeSuffix(k)
	stackFields := secW > 0 && secW < 420
	lblW := theme.SettingsFormLabelWidth
	indentLeft := theme.Space24 + theme.Space6
	if stackFields {
		indentLeft = theme.Space16
	}
	autoBin, _ := resolveAutoCLIBinary(string(k))
	if autoBin == "" {
		autoBin = "/usr/local/bin/" + string(k)
	}
	installCmd, loginCmd := AgentDriverGuide(k)

	c.mu.RLock()
	probing := c.settingsAgentProbing[k]
	probeOK := c.settingsAgentProbeOK[k]
	probeErr := c.settingsAgentProbeErr[k]
	c.mu.RUnlock()

	ui.Column(cUI.Key("ag-detail-"+suffix)).FillWidth().MinWidth(0).
		Padding(theme.Space2, 0, theme.Space16, indentLeft).Gap(theme.Space10).Children(func() {
		if hasSt && (st.State == BackendStateMissing || st.State == BackendStateUnauth) {
			guideCmd := installCmd
			guideText := theme.Tr("未检测到 CLI，可执行安装或在下方指定路径", "CLI not found in PATH; install or set path below")
			if st.State == BackendStateMissing && strings.TrimSpace(st.Message) != "" {
				guideText = st.Message
			}
			if st.State == BackendStateUnauth {
				guideCmd = loginCmd
				guideText = theme.Tr("CLI 尚未登录，请在终端执行登录", "Not signed in; run sign-in command in terminal")
				if loginCmd == "" && strings.TrimSpace(st.Message) != "" {
					guideText = st.Message
				}
			}
			ui.Row(cUI).FillWidth().MinWidth(0).Wrap().Padding(theme.Space4, 0).Gap(theme.Space8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(cUI, guideText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).Grow(1).MinWidth(120).SingleLine().Ellipsis("…")
				if guideCmd != "" {
					cpIc := icons.Copy
					cpBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
						ID:       "ai-agent-copy-cmd-" + suffix,
						Label:    guideCmd,
						Leading:  &cpIc,
						Style:    bui.ButtonSecondary,
						Height:   theme.HeightCompact,
						PaddingX: theme.Space10,
					})
					if cpBtn.Clicked() {
						cUI.WriteClipboard(guideCmd)
					}
				}
			})
		}

		// Binary Path + Re-probe
		renderBinControls := func() {
			ui.Row(cUI).Grow(1).FillWidth().MinWidth(0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
				bp := opt.BinaryPath
				in := ui.TextInput(cUI.Key("ai-agent-binpath-"+suffix), &bp).Grow(1).MinWidth(0).
					FontSize(theme.FontBody).Placeholder(autoBin).PlaceholderColor(pal.Placeholder)
				bui.Probe(in, "ai-agent-binpath-"+suffix)
				if in.Changed() {
					c.mu.Lock()
					c.resetAgentProbeStateLocked(k)
					c.mu.Unlock()
					_ = c.UpdateAgentEntry(k, func(o *model.AIBackendAdapterOptions) {
						o.BinaryPath = bp
					})
				}
				probeLabel := theme.Tr("检测", "Verify")
				probeIc := icons.Refresh
				probeStyle := bui.ButtonSecondary
				if probing {
					probeLabel = theme.Tr("检测中…", "Checking…")
				} else if probeOK {
					probeLabel = theme.Tr("检测通过", "Verified")
					probeIc = icons.Check
					probeStyle = bui.ButtonSuccess
				}
				reprobeBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
					ID:       "ai-agent-reprobe-" + suffix,
					Label:    probeLabel,
					Leading:  &probeIc,
					Style:    probeStyle,
					Disabled: probing,
					Loading:  probing,
					Height:   theme.HeightCompact,
					PaddingX: theme.Space10,
				})
				if reprobeBtn.Clicked() && !probing {
					c.reprobeSingleBackendAsync(k)
				}
			})
		}
		if stackFields {
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				ui.Text(cUI, theme.Tr("可执行路径", "Binary Path")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				renderBinControls()
			})
		} else {
			ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Text(cUI, theme.Tr("可执行路径", "Binary Path")).Width(lblW).Shrink(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				renderBinControls()
			})
		}

		errText := ""
		if hasSt && (st.State == BackendStateError || st.State == BackendStateUnsupported) {
			errText = strings.TrimSpace(st.Message)
			if errText == "" {
				errText = probeErr
			}
			if errText == "" {
				if st.State == BackendStateUnsupported {
					errText = theme.Tr("当前 CLI 版本不受支持，请升级后重试", "This CLI version is not supported; please upgrade and try again")
				} else {
					errText = theme.Tr("CLI 检测失败，请检查可执行路径", "CLI verification failed; check the executable path")
				}
			}
		} else if probeErr != "" && !(hasSt && (st.State == BackendStateMissing || st.State == BackendStateUnauth)) {
			errText = probeErr
		}
		if errText != "" {
			errPadLeft := float32(0)
			if !stackFields {
				errPadLeft = lblW + theme.Space12
			}
			ui.Row(cUI).FillWidth().MinWidth(0).Padding(0, 0, 0, errPadLeft).Children(func() {
				ui.Text(cUI, errText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Danger).Grow(1).MinWidth(0)
			})
		}

		// Thinking Level
		levels := []string{"low", "medium", "high"}
		lvIdx := 2
		for i, lv := range levels {
			if lv == thinking {
				lvIdx = i
			}
		}
		renderThinkingControls := func() {
			if bui.SettingsSegmented(cUI, "ai-agent-thinking-"+suffix, &lvIdx, []bui.SegmentOption{
				{ID: "ai-agent-think-low-" + suffix, Label: "Low"},
				{ID: "ai-agent-think-medium-" + suffix, Label: "Medium"},
				{ID: "ai-agent-think-high-" + suffix, Label: "High"},
			}) {
				c.mu.Lock()
				_ = c.setBackendThinkingLocked(k, levels[lvIdx])
				c.mu.Unlock()
			}
		}
		if stackFields {
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine()
				renderThinkingControls()
			})
		} else {
			ui.Row(cUI).FillWidth().MinWidth(0).MinHeight(theme.HeightControl).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).Width(lblW).Shrink(0).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).SingleLine().Ellipsis("…")
				renderThinkingControls()
			})
		}
		if supportsCLI(k) {
			ui.Row(cUI).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				label := theme.Tr("允许本机工具", "Allow local tools")
				ui.Column(cUI).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(cUI, label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.Text)
					ui.Text(cUI, theme.Tr("允许 Agent 运行命令、读写本机文件，无需逐项审批。", "Lets the agent run commands and read or write local files without per-action approval.")).
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
				})
				enabled := opt.AllowsNativeTools()
				if bui.TogglePill(cUI, "ai-agent-native-tools-"+suffix, label, &enabled, opt.Locked) {
					_ = c.UpdateAgentEntry(k, func(o *model.AIBackendAdapterOptions) { o.NativeTools = &enabled })
				}
			})
		}
	})
}

func (c *Controller) reprobeSingleBackendAsync(kind model.BackendKind) {
	c.mu.Lock()
	if c.settingsAgentProbing == nil {
		c.settingsAgentProbing = make(map[model.BackendKind]bool)
	}
	if c.settingsAgentProbeOK == nil {
		c.settingsAgentProbeOK = make(map[model.BackendKind]bool)
	}
	if c.settingsAgentProbeErr == nil {
		c.settingsAgentProbeErr = make(map[model.BackendKind]string)
	}
	if c.settingsAgentProbeSeq == nil {
		c.settingsAgentProbeSeq = make(map[model.BackendKind]uint64)
	}
	if c.settingsAgentProbing[kind] {
		c.mu.Unlock()
		return
	}
	c.settingsAgentProbing[kind] = true
	delete(c.settingsAgentProbeOK, kind)
	delete(c.settingsAgentProbeErr, kind)
	c.settingsAgentProbeSeq[kind]++
	seq := c.settingsAgentProbeSeq[kind]
	binPath := c.backendsCfg.Adapters[string(kind)].BinaryPath
	ov, hasOv := c.statusOverrides[kind]
	isMock := c.mockResponder
	c.mu.Unlock()
	c.requestRedraw()

	go func() {
		start := time.Now()
		var st BackendStatus
		if hasOv && strings.TrimSpace(binPath) == "" {
			st = ov
		} else {
			st = ProbeBackendWithPath(kind, binPath)
		}
		if !isMock && !hasOv {
			if rem := 180*time.Millisecond - time.Since(start); rem > 0 {
				time.Sleep(rem)
			}
		}
		refreshModels := false
		c.mu.Lock()
		if c.settingsAgentProbeSeq[kind] != seq {
			c.mu.Unlock()
			return
		}
		c.settingsAgentProbing[kind] = false
		c.backendStatuses[kind] = st
		c.ensureValidSelectionLocked()
		switch st.State {
		case BackendStateReady, BackendStateInstalled:
			c.settingsAgentProbeOK[kind] = true
			delete(c.settingsAgentProbeErr, kind)
			refreshModels = !c.mockResponder
		default:
			c.settingsAgentProbeOK[kind] = false
			msg := strings.TrimSpace(st.Message)
			if msg == "" {
				switch st.State {
				case BackendStateMissing:
					msg = theme.Tr("未检测到可用的 CLI 可执行文件", "CLI executable not found")
				case BackendStateUnauth:
					msg = theme.Tr("CLI 尚未登录，请在终端完成登录", "CLI is not signed in; sign in via terminal")
				case BackendStateUnsupported:
					msg = theme.Tr("当前 CLI 版本不受支持", "This CLI version is not supported")
				default:
					msg = theme.Tr("CLI 检测失败，请检查可执行路径", "CLI verification failed; check the executable path")
				}
			}
			c.settingsAgentProbeErr[kind] = msg
		}
		c.mu.Unlock()
		if refreshModels {
			c.refreshModelsAsync(kind)
		}
		c.requestRedraw()
	}()
}

func (c *Controller) openAddProviderModal() {
	c.mu.Lock()
	c.addProvModalOpen = true
	c.addProvPreset = "openai"
	c.addProvName = "OpenAI 兼容"
	c.addProvProto = model.ProtocolOpenAI
	c.addProvBaseURL = "https://api.openai.com/v1"
	c.addProvAPIKey = ""
	c.addProvShowKey = false
	c.addProvTesting = false
	c.addProvTestOK = false
	c.addProvTestErr = ""
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) renderAddProviderModal(cUI *ui.Context, pal theme.Palette) {
	c.mu.RLock()
	open := c.addProvModalOpen
	selPreset := c.addProvPreset
	draftName := c.addProvName
	draftProto := c.addProvProto
	draftURL := c.addProvBaseURL
	draftKey := c.addProvAPIKey
	showKey := c.addProvShowKey
	testing := c.addProvTesting
	testOK := c.addProvTestOK
	testErr := c.addProvTestErr
	c.mu.RUnlock()

	if !open {
		return
	}

	modalOpen := open
	title := theme.Tr("添加供应商", "Add Provider")
	ais := pal.AISurface()

	bui.Dialog(cUI, "ai-add-provider-modal", title, &modalOpen, bui.ModalLayout{
		Width:     436,
		MaxHeight: 540,
		Centered:  true,
	}, func() {
		ui.Column(cUI).FillWidth().MinWidth(0).Padding(18, 20).Gap(14).Children(func() {
			// Header
			ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				ui.Text(cUI, title).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine()
				closeBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
					ID:      "ai-add-provider-close",
					Label:   theme.Tr("关闭", "Close"),
					Variant: bui.IconPlain,
				})
				if closeBtn.Clicked() {
					modalOpen = false
				}
			})

			// Exact 3 provider capsules: DeepSeek / OpenAI 兼容 / Claude 协议
			ui.Row(cUI).FillWidth().MinWidth(0).Wrap().Gap(theme.Space4).Children(func() {
				for _, preset := range providerPresets {
					pr := preset
					isSel := selPreset == pr.ID
					lbl := theme.Tr(pr.Zh, pr.En)
					chipID := "ai-preset-" + pr.ID
					chip := ui.ButtonBase(cUI.Key(chipID)).Grow(1).MinWidth(108).Height(theme.HeightCompact).PaddingX(theme.Space10).Gap(theme.Space6).
						AlignItems(ui.Center).Justify(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(lbl)
					hov := chip.Hovered()
					prs := chip.Pressed()
					bg := ui.Transparent
					bd := ui.Transparent
					if isSel {
						bg = pal.Selected
					} else if prs {
						bg = ais.SelectedBG
					} else if hov {
						bg = ais.HoverBG
					}
					bui.AnimateSurfaceColors(chip, bg, bd, true)
					bui.TactileCard(chip, hov, prs, isSel, false)
					bui.Probe(chip, chipID)
					chip.Children(func() {
						col := pal.TextSecondary
						if isSel || hov || prs {
							col = pal.Text
						}
						icons.Draw(cUI, pr.Icon, 14, pal.MonoIconColors(pr.BrandColor))
						ui.Text(cUI, lbl).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(col).SingleLine()
					})
					if chip.Clicked() {
						c.mu.Lock()
						c.addProvPreset = pr.ID
						c.addProvName = pr.DefaultName
						c.addProvProto = pr.Protocol
						c.addProvBaseURL = pr.BaseURL
						c.addProvTestOK = false
						c.addProvTestErr = ""
						c.mu.Unlock()
					}
				}
			})

			// Stacked full-width fields
			ui.Column(cUI).FillWidth().MinWidth(0).Gap(10).Children(func() {
				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("名称", "Name")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					nm := draftName
					nmIn := ui.TextInput(cUI.Key("ai-add-provider-name"), &nm).FillWidth().MinWidth(0).
						FontSize(theme.FontBody).Placeholder(theme.Tr("例如 DeepSeek / 自定义中转", "e.g. DeepSeek / Custom Endpoint")).PlaceholderColor(pal.Placeholder)
					bui.Probe(nmIn, "ai-add-provider-name")
					if nmIn.Changed() {
						c.mu.Lock()
						c.addProvName = nm
						c.mu.Unlock()
					}
				})

				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("接口地址", "Base URL")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					u := draftURL
					uIn := ui.TextInput(cUI.Key("ai-add-provider-baseurl"), &u).FillWidth().MinWidth(0).
						FontSize(theme.FontBody).Placeholder("https://api.example.com/v1").PlaceholderColor(pal.Placeholder)
					bui.Probe(uIn, "ai-add-provider-baseurl")
					if uIn.Changed() {
						c.mu.Lock()
						c.addProvBaseURL = u
						c.addProvTestOK = false
						c.addProvTestErr = ""
						c.mu.Unlock()
					}
				})

				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("API 密钥", "API Key")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					ui.Row(cUI).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
						k := draftKey
						kIn := ui.TextInput(cUI.Key("ai-add-provider-apikey"), &k).Grow(1).MinWidth(0).
							FontSize(theme.FontBody).Placeholder("sk-…").PlaceholderColor(pal.Placeholder)
						if !showKey {
							kIn.Password()
						}
						bui.Probe(kIn, "ai-add-provider-apikey")
						if kIn.Changed() {
							c.mu.Lock()
							c.addProvAPIKey = k
							c.addProvTestOK = false
							c.addProvTestErr = ""
							c.mu.Unlock()
						}
						lbl := theme.Tr("显示", "Show")
						visIc := icons.Eye
						if showKey {
							lbl = theme.Tr("隐藏", "Hide")
							visIc = icons.EyeOff
						}
						visBtn := bui.ActionButton(cUI, "ai-add-toggle-key", lbl, &visIc, bui.ButtonSecondary, false)
						if visBtn.Clicked() {
							c.mu.Lock()
							c.addProvShowKey = !showKey
							c.mu.Unlock()
						}
					})
				})
			})

			if testErr != "" {
				ui.Box(cUI).FillWidth().MinWidth(0).Padding(theme.Space8, theme.Space12).Radius(theme.RadiusMD).
					Background(pal.DangerSurface).Children(func() {
					ui.Text(cUI, testErr).FontSize(theme.FontBody).TextColor(pal.Danger)
				})
			}

			// Footer actions
			ui.Row(cUI).FillWidth().MinWidth(0).Wrap().Padding(4, 0, 0, 0).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				tLbl := theme.Tr("连接测试", "Test")
				tIcon := icons.Zap
				tStyle := bui.ButtonSecondary
				tAlwaysDuo := true
				if testing {
					tLbl = theme.Tr("测试中…", "Testing…")
				} else if testOK {
					tLbl = theme.Tr("连接正常", "Connected")
					tIcon = icons.Check
					tStyle = bui.ButtonSuccess
					tAlwaysDuo = false
				}
				tBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
					ID:        "ai-add-provider-test",
					Label:     tLbl,
					Leading:   &tIcon,
					AlwaysDuo: tAlwaysDuo,
					Style:     tStyle,
					Disabled:  testing,
					Loading:   testing,
				})
				if tBtn.Clicked() && !testing {
					c.runAddDraftConnectionTestAsync()
				}

				ui.Row(cUI).Gap(8).AlignItems(ui.Center).Children(func() {
					cancelBtn := bui.ActionButton(cUI, "ai-add-provider-cancel", theme.Tr("取消", "Cancel"), nil, bui.ButtonSecondary, false)
					if cancelBtn.Clicked() {
						modalOpen = false
					}
					subIc := icons.Check
					submitBtn := bui.ActionButton(cUI, "ai-add-provider-submit", theme.Tr("添加并启用", "Add & Enable"), &subIc, bui.ButtonPrimary, false)
					if submitBtn.Clicked() {
						if strings.TrimSpace(draftURL) == "" {
							c.mu.Lock()
							c.addProvTestErr = theme.Tr("请填写接口地址", "Please enter a Base URL")
							c.mu.Unlock()
						} else if _, err := c.AddProviderEntry(selPreset, draftName, draftProto, draftURL, draftKey); err != nil {
							c.mu.Lock()
							c.addProvTestErr = err.Error()
							c.mu.Unlock()
						} else {
							modalOpen = false
						}
					}
				})
			})
		})
	})

	if !modalOpen {
		c.mu.Lock()
		c.addProvModalOpen = false
		c.mu.Unlock()
	}
}

func (c *Controller) openAddAgentModal() {
	c.mu.Lock()
	pick := model.BackendCodex
	candidates := []model.BackendKind{model.BackendCodex, model.BackendClaude, model.BackendDsh, model.BackendPi, model.BackendAgy}
	for _, k := range candidates {
		if !c.backendsCfg.Adapters[string(k)].Added {
			pick = k
			break
		}
	}
	opt := c.backendsCfg.Adapters[string(pick)]
	nm := strings.TrimSpace(opt.Name)
	if nm == "" {
		nm = backendLabel(pick)
	}
	th := opt.ThinkingLevel
	if th == "" {
		th = "high"
	}
	st, hasSt := c.backendStatuses[pick]
	if ov, hasOv := c.statusOverrides[pick]; hasOv {
		st, hasSt = ov, true
	}
	c.addAgentModalOpen = true
	c.addAgentStep = 1
	c.addAgentShowMore = false
	c.addAgentKind = pick
	c.addAgentName = nm
	c.addAgentBinPath = opt.BinaryPath
	c.addAgentThinking = th
	c.addAgentProbing = false
	c.addAgentStatus = st
	c.addAgentHasStatus = hasSt
	c.addAgentCopiedCmd = ""
	c.addAgentErr = ""
	shouldProbe := !c.mockResponder
	c.mu.Unlock()

	if shouldProbe {
		c.probeAllBackendsAsync()
	}
	c.requestRedraw()
}

func (c *Controller) runAddAgentProbeAsync() {
	c.mu.Lock()
	if c.addAgentProbing {
		c.mu.Unlock()
		return
	}
	kind := c.addAgentKind
	binPath := c.addAgentBinPath
	ov, hasOv := c.statusOverrides[kind]
	c.addAgentProbing = true
	c.addAgentErr = ""
	c.mu.Unlock()
	c.requestRedraw()

	go func() {
		var st BackendStatus
		if hasOv && strings.TrimSpace(binPath) == "" {
			st = ov
		} else {
			st = ProbeBackendWithPath(kind, binPath)
		}
		c.mu.Lock()
		c.addAgentProbing = false
		if c.addAgentKind == kind {
			c.addAgentStatus = st
			c.addAgentHasStatus = true
			c.backendStatuses[kind] = st
		}
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func (c *Controller) renderAddAgentModal(cUI *ui.Context, pal theme.Palette, backends model.AIBackendsConfig) {
	c.mu.RLock()
	open := c.addAgentModalOpen
	step := c.addAgentStep
	showMore := c.addAgentShowMore
	selKind := c.addAgentKind
	draftName := c.addAgentName
	draftBin := c.addAgentBinPath
	draftThink := c.addAgentThinking
	probing := c.addAgentProbing
	curStatus := c.addAgentStatus
	hasCurStatus := c.addAgentHasStatus
	copiedCmd := c.addAgentCopiedCmd
	errMsg := c.addAgentErr
	if !hasCurStatus {
		if st, ok := c.backendStatuses[selKind]; ok {
			curStatus, hasCurStatus = st, true
		} else if ov, ok := c.statusOverrides[selKind]; ok {
			curStatus, hasCurStatus = ov, true
		}
	}
	statuses := make(map[model.BackendKind]BackendStatus, len(c.backendStatuses)+len(c.statusOverrides))
	for k, v := range c.backendStatuses {
		statuses[k] = v
	}
	for k, v := range c.statusOverrides {
		statuses[k] = v
	}
	c.mu.RUnlock()

	if !open {
		return
	}
	if step < 1 {
		step = 1
	}

	modalOpen := open
	title := theme.Tr("添加本机 Agent", "Add Local Agent")
	ais := pal.AISurface()

	drivers := []model.BackendKind{model.BackendCodex, model.BackendClaude, model.BackendDsh, model.BackendPi}
	if showMore {
		drivers = append(drivers, model.BackendAgy)
	}

	bui.Dialog(cUI, "ai-add-agent-modal", title, &modalOpen, bui.ModalLayout{
		Width:     440,
		MaxHeight: 580,
		Centered:  true,
	}, func() {
		ui.Column(cUI).FillWidth().MinWidth(0).Padding(18, 20).Gap(14).Children(func() {
			// Step 1: T3 Code style Agent Catalog picker -> clicking any row advances to Step 2 configuration.
			if step == 1 {
				ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
					ui.Text(cUI, title).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine()
					closeBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
						ID:      "ai-add-agent-close",
						Label:   theme.Tr("关闭", "Close"),
						Variant: bui.IconPlain,
					})
					if closeBtn.Clicked() {
						modalOpen = false
					}
				})

				ui.Column(cUI.Key("ai-add-agent-step1")).FillWidth().MinWidth(0).Radius(theme.RadiusLG).
					Background(pal.Card).Border(1, pal.Hairline).Clip().Children(func() {
					for idx, drv := range drivers {
						k := drv
						suffix := backendProbeSuffix(k)
						alreadyAdded := backends.Adapters[string(k)].Added
						lbl := backendLabel(k)
						rowID := "ai-agent-driver-" + suffix
						hasNext := idx+1 < len(drivers)

						st, hasSt := statuses[k]
						badge := ""
						badgeCol := pal.TextSecondary
						if alreadyAdded {
							badge = theme.Tr("已添加", "Added")
							badgeCol = pal.Accent
						} else if hasSt {
							switch st.State {
							case BackendStateReady, BackendStateInstalled:
								ver := formatBackendVersionTag(st, true)
								if ver != "" {
									badge = ver
								} else {
									badge = theme.Tr("已就绪", "Ready")
								}
								badgeCol = pal.Accent
							case BackendStateUnauth:
								badge = theme.Tr("未登录", "Sign in")
							case BackendStateMissing:
								badge = theme.Tr("未安装", "Not installed")
							}
						} else if _, ok := resolveAutoCLIBinary(string(k)); ok {
							badge = theme.Tr("已安装", "Installed")
						}

						btn := ui.ButtonBase(cUI.Key(rowID)).FillWidth().MinWidth(0).Height(48).PaddingX(14).Gap(10).
							AlignItems(ui.Center).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer).Label(lbl).
							BorderWidth(0, 0, boolToBorder(hasNext), 0).BorderColor(pal.Hairline)
						hov := btn.Hovered()
						prs := btn.Pressed()
						bg := ui.Transparent
						if prs {
							bg = ais.SelectedBG
						} else if hov {
							bg = pal.Hover
						}
						bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
						bui.Probe(btn, rowID)
						btn.Children(func() {
							ui.Row(cUI).Gap(10).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
								ui.Box(cUI).Size(28, 28).Shrink(0).Center().Radius(theme.RadiusSM).
									Background(ais.CapsuleBG).Border(1, pal.Hairline).Children(func() {
									icons.Brand(cUI, k, 16, pal.Text)
								})
								ui.Text(cUI, lbl).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(pal.Text).
									Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							})

							ui.Row(cUI).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
								if badge != "" {
									ui.Text(cUI, badge).Font("monospace").FontSize(theme.FontMeta).TextColor(badgeCol).SingleLine()
								}
								icons.Draw(cUI, icons.ChevronRight, 14, pal.MonoIconColors(pal.TextSecondary))
							})
						})
						if btn.Clicked() {
							opt := backends.Adapters[string(k)]
							nm := strings.TrimSpace(opt.Name)
							if nm == "" {
								nm = backendLabel(k)
							}
							th := opt.ThinkingLevel
							if th == "" {
								th = "high"
							}
							c.mu.Lock()
							c.addAgentKind = k
							c.addAgentStep = 2
							c.addAgentName = nm
							c.addAgentBinPath = opt.BinaryPath
							c.addAgentThinking = th
							c.addAgentStatus = statuses[k]
							_, c.addAgentHasStatus = statuses[k]
							c.addAgentCopiedCmd = ""
							c.addAgentErr = ""
							shouldProbe := !c.mockResponder
							c.mu.Unlock()
							if shouldProbe {
								c.runAddAgentProbeAsync()
							}
						}
					}
				})

				ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
					if !showMore {
						moreBtn := bui.ActionButton(cUI, "ai-agent-show-more", theme.Tr("显示更多", "Show more"), nil, bui.ButtonGhost, false)
						if moreBtn.Clicked() {
							c.mu.Lock()
							c.addAgentShowMore = true
							c.mu.Unlock()
						}
					} else {
						ui.Box(cUI)
					}
					cancelBtn := bui.ActionButton(cUI, "ai-add-agent-cancel", theme.Tr("取消", "Cancel"), nil, bui.ButtonSecondary, false)
					if cancelBtn.Clicked() {
						modalOpen = false
					}
				})
				return
			}

			// Step 2: Dedicated Agent Setup & Verification (Environment Check + Display Name + Binary Path + Thinking)
			ui.Row(cUI).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				ui.Row(cUI).Gap(8).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
					backBtn := bui.IconButton(cUI, icons.ChevronLeft, bui.IconButtonOptions{
						ID:      "ai-add-agent-back",
						Label:   theme.Tr("返回选择", "Back"),
						Tooltip: theme.Tr("返回上一步", "Back"),
						Variant: bui.IconPlain,
					})
					if backBtn.Clicked() {
						c.mu.Lock()
						c.addAgentStep = 1
						c.addAgentErr = ""
						c.mu.Unlock()
					}
					icons.Brand(cUI, selKind, 18, pal.Text)
					ui.Text(cUI, backendLabel(selKind)).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				})
				closeBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
					ID:      "ai-add-agent-close",
					Label:   theme.Tr("关闭", "Close"),
					Variant: bui.IconPlain,
				})
				if closeBtn.Clicked() {
					modalOpen = false
				}
			})

			autoBin, hasAutoBin := resolveAutoCLIBinary(string(selKind))
			installCmd, loginCmd := AgentDriverGuide(selKind)
			ui.Column(cUI).FillWidth().MinWidth(0).Padding(10, 12).Gap(8).Radius(theme.RadiusMD).
				Background(ais.RailBG).Border(1, pal.Hairline).Children(func() {
				statusLine := theme.Tr("正在检测本机 CLI 环境…", "Checking local CLI environment…")
				var actionCmd string
				if probing {
					statusLine = theme.Tr("正在检测可执行文件与登录状态…", "Verifying binary and login status…")
				} else if hasCurStatus {
					ver := formatBackendVersionTag(curStatus, true)
					switch curStatus.State {
					case BackendStateReady, BackendStateInstalled:
						statusLine = theme.Tr("已检测到可用 CLI", "CLI detected and ready")
						if ver != "" {
							statusLine += " · " + ver
						}
					case BackendStateUnauth:
						statusLine = theme.Tr("已检测到 CLI，请先在终端完成登录", "CLI installed; sign in via terminal")
						if loginCmd == "" && curStatus.Message != "" {
							statusLine = curStatus.Message
						}
						actionCmd = loginCmd
					case BackendStateUnsupported:
						statusLine = curStatus.Message
						actionCmd = installCmd
					default:
						statusLine = theme.Tr("未在系统 PATH 中检测到可执行文件", "CLI not found in system PATH")
						actionCmd = installCmd
					}
				} else if hasAutoBin {
					statusLine = theme.Tr("已定位可执行文件：", "Located binary: ") + autoBin
				} else {
					statusLine = theme.Tr("未在系统 PATH 中检测到可执行文件", "CLI not found in system PATH")
					actionCmd = installCmd
				}

				ui.Row(cUI).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
					ic := icons.HelpCircle
					icCol := pal.TextSecondary
					if hasCurStatus && (curStatus.State == BackendStateReady || curStatus.State == BackendStateInstalled) {
						ic = icons.Check
						icCol = pal.Accent
					}
					icons.Draw(cUI, ic, 15, pal.MonoIconColors(icCol))
					ui.Text(cUI, statusLine).FontSize(theme.FontBody).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				})

				if actionCmd != "" {
					ui.Row(cUI).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).
						Padding(6, 10).Radius(theme.RadiusSM).Background(pal.Card).Border(1, pal.Hairline).Children(func() {
						ui.Text(cUI, actionCmd).Font("monospace").FontSize(theme.FontSmall).TextColor(pal.TextSecondary).
							Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
						cpLbl := theme.Tr("复制", "Copy")
						cpIc := icons.Copy
						if copiedCmd == actionCmd {
							cpLbl = theme.Tr("已复制", "Copied")
							cpIc = icons.Check
						}
						cpBtn := bui.ActionButton(cUI, "ai-add-agent-copy-cmd", cpLbl, &cpIc, bui.ButtonSecondary, false)
						if cpBtn.Clicked() {
							cUI.WriteClipboard(actionCmd)
							c.mu.Lock()
							c.addAgentCopiedCmd = actionCmd
							c.mu.Unlock()
						}
					})
				}
			})

			ui.Column(cUI).FillWidth().MinWidth(0).Gap(10).Children(func() {
				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("显示名称", "Display Name")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					nm := draftName
					nmIn := ui.TextInput(cUI.Key("ai-add-agent-name"), &nm).FillWidth().MinWidth(0).
						FontSize(theme.FontBody).Placeholder(backendLabel(selKind)).PlaceholderColor(pal.Placeholder)
					bui.Probe(nmIn, "ai-add-agent-name")
					if nmIn.Changed() {
						c.mu.Lock()
						c.addAgentName = nm
						c.mu.Unlock()
					}
				})

				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("可执行文件路径", "Binary Path")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					ph := autoBin
					if ph == "" {
						ph = theme.Tr("留空自动从 PATH 查找，或输入如 ~/.local/bin/", "Auto-resolve from PATH, or enter e.g. ~/.local/bin/") + string(selKind)
					}
					bp := draftBin
					bpIn := ui.TextInput(cUI.Key("ai-add-agent-binpath"), &bp).FillWidth().MinWidth(0).
						FontSize(theme.FontBody).Placeholder(ph).PlaceholderColor(pal.Placeholder)
					bui.Probe(bpIn, "ai-add-agent-binpath")
					if bpIn.Changed() {
						c.mu.Lock()
						c.addAgentBinPath = bp
						c.addAgentErr = ""
						c.mu.Unlock()
					}
				})

				ui.Column(cUI).FillWidth().MinWidth(0).Gap(5).Children(func() {
					ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(theme.FontBody).TextColor(pal.TextSecondary).SingleLine()
					levels := []string{"low", "medium", "high"}
					lvIdx := 2
					for i, lv := range levels {
						if lv == draftThink {
							lvIdx = i
						}
					}
					if bui.SegmentedControlWithOptions(cUI, "ai-add-agent-thinking", &lvIdx, []bui.SegmentOption{
						{ID: "ai-add-agent-think-low", Label: "Low"},
						{ID: "ai-add-agent-think-med", Label: "Medium"},
						{ID: "ai-add-agent-think-high", Label: "High"},
					}, bui.SegmentedControlOptions{FillWidth: true}) {
						c.mu.Lock()
						c.addAgentThinking = levels[lvIdx]
						c.mu.Unlock()
					}
				})
			})

			if errMsg != "" {
				ui.Box(cUI).FillWidth().MinWidth(0).Padding(8, 12).Radius(theme.RadiusMD).
					Background(pal.DangerSurface).Border(1, pal.Danger.Alpha(0.35)).Children(func() {
					ui.Text(cUI, errMsg).FontSize(theme.FontBody).TextColor(pal.Danger)
				})
			}

			ui.Row(cUI).FillWidth().MinWidth(0).Wrap().Padding(4, 0, 0, 0).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				reLbl := theme.Tr("重新检测", "Verify")
				if probing {
					reLbl = theme.Tr("检测中…", "Checking…")
				}
				reIc := icons.Refresh
				reBtn := bui.ActionButtonWithOptions(cUI, bui.ActionButtonOptions{
					ID:       "ai-add-agent-probe",
					Label:    reLbl,
					Leading:  &reIc,
					Style:    bui.ButtonSecondary,
					Disabled: probing,
					Loading:  probing,
				})
				if reBtn.Clicked() && !probing {
					c.runAddAgentProbeAsync()
				}

				ui.Row(cUI).Gap(8).AlignItems(ui.Center).Children(func() {
					backIc := icons.ArrowLeft
					backBtn := bui.ActionButton(cUI, "ai-add-agent-step1-btn", theme.Tr("上一步", "Back"), &backIc, bui.ButtonSecondary, false)
					if backBtn.Clicked() {
						c.mu.Lock()
						c.addAgentStep = 1
						c.addAgentErr = ""
						c.mu.Unlock()
					}
					subLbl := theme.Tr("添加并启用", "Add & Enable")
					if backends.Adapters[string(selKind)].Added {
						subLbl = theme.Tr("保存配置", "Save")
					}
					subIc := icons.Check
					submitBtn := bui.ActionButton(cUI, "ai-add-agent-submit", subLbl, &subIc, bui.ButtonPrimary, false)
					if submitBtn.Clicked() {
						if trimmed := strings.TrimSpace(draftBin); trimmed != "" {
							cand := expandHomePath(trimmed)
							if info, statErr := os.Stat(cand); statErr != nil || info.IsDir() {
								c.mu.Lock()
								c.addAgentErr = theme.Tr("指定的可执行文件路径不存在", "Specified executable path does not exist")
								c.mu.Unlock()
								return
							}
						}
						if err := c.AddAgentEntry(selKind, draftName, draftBin, draftThink); err != nil {
							c.mu.Lock()
							c.addAgentErr = err.Error()
							c.mu.Unlock()
						} else {
							modalOpen = false
						}
					}
				})
			})
		})
	})

	if !modalOpen {
		c.mu.Lock()
		c.addAgentModalOpen = false
		c.mu.Unlock()
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

func (c *Controller) renderGeneralFeaturesSection(cUI *ui.Context, pal theme.Palette, annotCfg model.AnnotationConfig, annotOpen bool) ui.Element {
	kind := annotCfg.Backend
	if kind == "" {
		kind = model.BackendBuiltin
	}
	c.mu.RLock()
	lbl := c.modelLabelLocked(kind, strings.TrimSpace(annotCfg.Model))
	fullLbl := c.modelFullLabelLocked(kind, strings.TrimSpace(annotCfg.Model))
	errTargetKind := kind
	if annotOpen && c.annotMenuBackend != "" {
		errTargetKind = c.annotMenuBackend
	}
	annotModelStatus := c.modelStatus[errTargetKind]
	c.mu.RUnlock()
	if lbl == "" {
		lbl = backendLabel(kind)
	}
	thinkLbl := thinkingDisplayLabel(annotCfg.Thinking)
	tipLbl := fullLbl
	if tipLbl == "" {
		tipLbl = lbl
	}
	annotTipText := tipLbl + " · " + thinkLbl
	annotErr := strings.TrimPrefix(annotModelStatus, "error:")
	if annotErr == annotModelStatus {
		annotErr = ""
	}

	var trigAnchor ui.Element
	bui.SettingsSection(cUI, func() {
		bui.SettingsSectionTitle(cUI, theme.Tr("普通功能", "General Features"), nil)

		bui.SettingsRow(cUI, theme.Tr("标注功能", "Annotations"), func() {
			trig := ui.ButtonBase(cUI.Key("annotation-model-trigger")).Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space8).
				MaxWidth(360).Shrink(1).MinWidth(0).
				AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
				Label(lbl)
			bui.Tooltip(cUI, trig, annotTipText)
			trigAnchor = trig
			ais := pal.AISurface()
			hovered := trig.Hovered()
			pressed := trig.Pressed()
			bg := ais.CapsuleBG
			bd := ais.CapsuleBorder
			if annotOpen {
				bg = settingsSelectedSurface(pal)
				bd = pal.SelectedBorder
			} else if pressed {
				bg = ais.SelectedBG
				bd = pal.SelectedBorder
			} else if hovered {
				bg = ais.HoverBG
				bd = pal.ShellChrome().FieldBorderHover
			}
			bui.AnimateSurfaceColors(trig, bg, bd, true)
			bui.TactileCard(trig, hovered, pressed, false, false)
			trig.DrawOver(func(_ *ui.Painter, r ui.Rect) {
				c.mu.Lock()
				c.annotMenuTriggerBox = r
				c.mu.Unlock()
				bui.RecordBounds("annotation-model-trigger", r)
			})
			trig.Children(func() {
				icons.Brand(cUI, kind, theme.IconAction, pal.Text)
				ui.Text(cUI, lbl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
				ui.Text(cUI, "·").FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).Shrink(0)
				ui.Text(cUI, thinkLbl).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				chev := icons.ChevronDown
				if annotOpen {
					chev = icons.ChevronUp
				}
				icons.DrawAnimated(cUI, chev, theme.IconInline, pal.MonoIconColors(pal.TextSecondary), hovered || pressed || annotOpen)
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
					c.refreshAnnotationModelsAsync(targetKind)
				}
			}
		})

		if annotErr != "" {
			ui.Text(cUI, annotErr).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
		}
	})
	return trigAnchor
}

func (c *Controller) renderAnnotationModelPopover(cUI *ui.Context, pal theme.Palette, recognized []model.BackendKind, backends model.AIBackendsConfig, anchor ui.Element) {
	c.mu.RLock()
	trigger := c.annotMenuTriggerBox
	activeKind := c.annotMenuBackend
	annotCfg := c.annotCfg
	query := strings.TrimSpace(c.annotMenuSearch)
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
	popW := min(float32(520), max(winW-16, 260))
	widePop := popW >= 460
	targetH := float32(256)
	if !widePop {
		targetH = 292
	}
	spec := bui.PlaceAbove(trigger).WithAlignX(bui.AlignX{Kind: bui.AlignRight}).WithGap(6).WithMargin(8)
	maxH := bui.MaxHeightForSpec(spec, winW, winH, targetH)
	popH := min(targetH, maxH)
	x, y := bui.ResolveOrigin(spec, popW, popH, winW, winH)

	ais := pal.AISurface()
	ui.Overlay(cUI, func() {
		panel := bui.Elevate(ui.Row(cUI.Key("annotation-model-menu")).Absolute().Left(x).Top(y).Width(popW).Height(popH).
			AlignItems(ui.Stretch).Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.MenuBorder).Clip().
			Transition(probeSafeTransition(bui.PopoverSpringTransition())), theme.ElevationPopover)
		bui.Probe(panel, "annotation-model-menu")

		panel.Children(func() {
			// 1. Left enabled backend icon rail (recessed surface, full height).
			ui.Column(cUI).Width(46).Shrink(0).Padding(6, 0).Gap(6).
				Background(ais.RailBG).BorderWidth(0, 1, 0, 0).BorderColor(pal.Hairline).
				AlignItems(ui.Center).Children(func() {
				for _, b := range enabledOrder {
					bk := b
					id := "annotation-backend-" + backendProbeSuffix(bk)
					sel := bk == activeKind
					ui.Box(cUI.Key("wrap-" + id)).FillWidth().Height(34).Center().Children(func() {
						btn := ui.ButtonBase(cUI.Key(id)).Size(34, 34).Center().Radius(theme.RadiusMD).
							Cursor(ui.CursorPointer).Label(backendLabel(bk))
						bui.Tooltip(cUI, btn, backendLabel(bk))
						hovered := btn.Hovered()
						pressed := btn.Pressed()
						bg := ui.Transparent
						if sel || pressed {
							bg = ais.SelectedBG
						} else if hovered {
							bg = ais.HoverBG
						}
						bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
						bui.TactileButton(btn, hovered, pressed, false)
						bui.Probe(btn, id)
						btn.Children(func() {
							icCol := pal.TextSecondary
							if sel || hovered || pressed {
								icCol = pal.Text
							}
							icons.Brand(cUI, bk, 16, icCol)
						})
						if sel {
							ui.Box(cUI).Absolute().Right(0).Top(8).Size(2.5, 18).
								Radius(theme.RadiusPill).Background(pal.Accent)
						}
						if btn.Clicked() {
							c.mu.Lock()
							c.annotMenuBackend = bk
							c.annotMenuSearch = ""
							c.mu.Unlock()
							c.refreshAnnotationModelsAsync(bk)
						}
					})
				}
			})

			// 2. Center model search + list (+ compact bottom thinking row when !widePop)
			annotCol := ui.Column(cUI).Grow(1).MinWidth(0)
			type annotModelCursorKey struct{}
			annotCursor := ui.Local(annotCol, annotModelCursorKey{}, func() int { return -1 })
			bui.ObserveMenuPointer(cUI, annotCol, annotCursor)
			type indexedAnnotChoice struct {
				idx   int
				opt   ModelOption
				score int
			}
			type annotFilterCacheKey struct{}
			type annotFilterCache struct {
				valid    bool
				query    string
				count    int
				hash     uint64
				filtered []indexedAnnotChoice
			}
			annotCache := ui.Local(annotCol, annotFilterCacheKey{}, func() annotFilterCache { return annotFilterCache{} })
			filterAnnotChoices := func(rawQ string) []indexedAnnotChoice {
				q := strings.TrimSpace(rawQ)
				mh := hashModelOptions64(choices)
				if annotCache.valid && annotCache.query == q && annotCache.count == len(choices) && annotCache.hash == mh {
					return annotCache.filtered
				}
				var out []indexedAnnotChoice
				for idx, m := range choices {
					if q == "" {
						out = append(out, indexedAnnotChoice{idx: idx, opt: m})
					} else if sc, ok := fuzzy.Score(q, []string{m.Label, m.Value}, []string{m.Group}, true); ok {
						out = append(out, indexedAnnotChoice{idx: idx, opt: m, score: sc})
					}
				}
				if q != "" {
					sort.SliceStable(out, func(i, j int) bool {
						return out[i].score > out[j].score
					})
				}
				*annotCache = annotFilterCache{
					valid: true, query: q, count: len(choices), hash: mh, filtered: out,
				}
				return out
			}
			filteredAnnot := filterAnnotChoices(query)
			if *annotCursor >= len(filteredAnnot) {
				*annotCursor = len(filteredAnnot) - 1
			}
			annotCol.Children(func() {
				ui.Row(cUI).FillWidth().Height(40).PaddingX(10).Gap(8).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
					icons.Draw(cUI, icons.FindSearch, theme.IconRow, pal.MonoIconColors(pal.TextSecondary))
					in := ui.TextInputBase(cUI.Key("annotation-model-search"), &c.annotMenuSearch).
						Grow(1).MinWidth(0).Height(28).Padding(0).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.Text).
						CaretColor(pal.Accent).CaretWidth(2).
						Placeholder(theme.Tr("搜索模型…", "Search models…")).PlaceholderColor(pal.Placeholder).Bind(&c.annotSearchHandle).AutoFocus()
					bui.Probe(in, "annotation-model-search")
					if in.Changed() {
						*annotCursor = 0
						c.annotMenuScroll.Y = 0
						query = strings.TrimSpace(c.annotMenuSearch)
						filteredAnnot = filterAnnotChoices(query)
					}
					in.OnTextKey(func(k ui.TextKey) bool {
						switch k.Key {
						case ui.KeyEscape:
							c.mu.Lock()
							c.annotMenuOpen = false
							c.mu.Unlock()
							return true
						case ui.KeyDown:
							if len(filteredAnnot) > 0 {
								*annotCursor = (*annotCursor + 1 + len(filteredAnnot)) % len(filteredAnnot)
							}
							return true
						case ui.KeyUp:
							if len(filteredAnnot) > 0 {
								if *annotCursor <= 0 {
									*annotCursor = len(filteredAnnot) - 1
								} else {
									*annotCursor--
								}
							}
							return true
						case ui.KeyEnter:
							if *annotCursor >= 0 && *annotCursor < len(filteredAnnot) {
								pickVal := filteredAnnot[*annotCursor].opt.Value
								c.mu.Lock()
								next := c.annotCfg
								next.Backend = activeKind
								if activeKind == model.BackendBuiltin && c.providerCfg.Provider != "" {
									next.Provider = c.providerCfg.Provider
								}
								next.Model = pickVal
								_ = c.saveAnnotationConfigLocked(next)
								c.annotMenuOpen = false
								c.mu.Unlock()
								return true
							}
						}
						return false
					})
				})

				bui.Scroll(cUI.Key("annotation-model-list"), &c.annotMenuScroll).
					Grow(1).MinHeight(0).FillWidth().Padding(6).Gap(2).Children(func() {
					lastGroup := ""
					for pos, entry := range filteredAnnot {
						idx := entry.idx
						opt := entry.opt
						if grp := strings.TrimSpace(opt.Group); grp != "" && grp != lastGroup {
							lastGroup = grp
							topPad := float32(6)
							if pos == 0 {
								topPad = 2
							}
							ui.Box(cUI.Key(fmt.Sprintf("annotation-model-grp-%d-%s", pos, grp))).FillWidth().Padding(topPad, 10, 2, 10).Children(func() {
								ui.Text(cUI, grp).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine()
							})
						}
						isAct := annotCfg.Backend == activeKind && annotCfg.Model == opt.Value
						highlighted := pos == *annotCursor
						rowID := fmt.Sprintf("annotation-model-%d", idx)
						row := ui.ButtonBase(cUI.Key(rowID)).FillWidth().Height(34).PaddingX(10).Gap(8).
							AlignItems(ui.Center).Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(opt.Label).
							Transition(probeSafeTransition(bui.CascadeItemTransition(pos, false)))
						bui.RevealOnHighlight(row, highlighted)
						hovered := highlighted || (*annotCursor < 0 && row.Hovered())
						pressed := row.Pressed()
						bg := ui.Transparent
						if isAct || pressed {
							bg = ais.SelectedBG
						} else if hovered {
							bg = ais.HoverBG
						}
						bui.AnimateSurfaceColors(row, bg, ui.Transparent, false)
						bui.TactileCard(row, hovered, pressed, false, false)
						bui.Probe(row, rowID)
						row.Children(func() {
							fw := theme.WeightRegular
							if isAct {
								fw = theme.WeightControl
							}
							var marks [][2]int
							if query != "" {
								marks = fuzzy.HighlightRanges(opt.Label, query)
							}
							bui.HighlightedText(cUI, opt.Label, marks, theme.FontBody, fw, pal.Text, pal.SearchHighlight).Grow(1).MinWidth(0)
							if isAct {
								icons.DrawAnimated(cUI, icons.Check, 14, pal.MonoIconColors(pal.Text), isAct)
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
					if len(filteredAnnot) == 0 {
						msg := theme.Tr("暂无可选模型", "No models available")
						if query != "" && len(choices) > 0 {
							msg = theme.Tr("无匹配模型", "No matching models")
						} else if status == "loading" {
							msg = theme.Tr("正在获取模型…", "Loading models…")
						} else if strings.HasPrefix(status, "error:") {
							msg = theme.Tr("无法获取模型", "Models unavailable")
						}
						ui.Box(cUI).Padding(8).Children(func() {
							ui.Text(cUI, msg).FontSize(theme.FontBody).TextColor(pal.TextSecondary)
						})
					}
				})

				if !widePop {
					levels := []string{"low", "medium", "high"}
					lvIdx := 2
					for i, lv := range levels {
						if annotCfg.Thinking == lv {
							lvIdx = i
						}
					}
					ui.Row(cUI).FillWidth().Padding(6, 10).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).
						BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
						ui.Text(cUI, theme.Tr("思考强度", "Thinking")).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
						if bui.SegmentedControl(cUI, "annotation-thinking-compact", &lvIdx, []bui.SegmentOption{
							{ID: "annotation-thinking-low", Label: "Low"},
							{ID: "annotation-thinking-medium", Label: "Medium"},
							{ID: "annotation-thinking-high", Label: "High"},
						}) {
							c.mu.Lock()
							next := c.annotCfg
							next.Thinking = levels[lvIdx]
							_ = c.saveAnnotationConfigLocked(next)
							c.mu.Unlock()
						}
					})
				}
			})

			// 3. Right Thinking Level column in wide mode
			if widePop {
				ui.Column(cUI).Width(140).Shrink(0).
					BorderWidth(0, 0, 0, 1).BorderColor(pal.Hairline).Children(func() {
					ui.Row(cUI).FillWidth().Height(40).PaddingX(10).AlignItems(ui.Center).
						BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).Children(func() {
						ui.Text(cUI, theme.Tr("思考强度", "Thinking Level")).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary)
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
							btn := ui.ButtonBase(cUI.Key(btnID)).FillWidth().Height(34).PaddingX(10).
								AlignItems(ui.Center).Justify(ui.SpaceBetween).Radius(theme.RadiusMD).
								Cursor(ui.CursorPointer).Label(lv.label)
							hovered := btn.Hovered()
							pressed := btn.Pressed()
							bg := ui.Transparent
							if isAct || pressed {
								bg = ais.SelectedBG
							} else if hovered {
								bg = ais.HoverBG
							}
							bui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
							bui.TactileCard(btn, hovered, pressed, false, false)
							bui.Probe(btn, btnID)
							btn.Children(func() {
								fw := theme.WeightRegular
								if isAct {
									fw = theme.WeightControl
								}
								ui.Text(cUI, lv.label).FontSize(theme.FontBody).FontWeight(fw).TextColor(pal.Text)
								if isAct {
									icons.DrawAnimated(cUI, icons.Check, 14, pal.MonoIconColors(pal.Text), isAct)
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
			}
		})

		if panel.OverlayShortcut(0, ui.KeyEscape) || ((!anchor.Valid() || !anchor.PressedInside()) && panel.PressedOutside()) {
			c.mu.Lock()
			c.annotMenuOpen = false
			c.mu.Unlock()
		}
	})
}

func (c *Controller) runProviderConnectionTestAsync(provID string) {
	c.mu.Lock()
	if c.settingsTesting {
		c.mu.Unlock()
		return
	}
	c.settingsTesting = true
	c.settingsTestingProv = provID
	c.settingsTestOK = false
	c.settingsTestErr = ""
	c.mu.Unlock()
	c.requestRedraw()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := c.ProviderConfig()
		entries := orderedProviderEntries(cfg)
		for _, e := range entries {
			if e.ID == provID {
				cfg.Enabled = true
				cfg.Provider = e.ID
				cfg.Protocol = e.Protocol
				cfg.BaseURL = e.BaseURL
				cfg.APIKey = e.APIKey
				cfg.Model = e.Model
				break
			}
		}
		err := testBuiltinConnection(ctx, cfg)
		c.mu.Lock()
		c.settingsTesting = false
		if err != nil {
			c.settingsTestOK = false
			c.settingsTestErr = err.Error()
		} else {
			c.settingsTestOK = true
			c.settingsTestErr = ""
		}
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func (c *Controller) runAddDraftConnectionTestAsync() {
	c.mu.Lock()
	if c.addProvTesting {
		c.mu.Unlock()
		return
	}
	c.addProvTesting = true
	c.addProvTestOK = false
	c.addProvTestErr = ""
	draft := model.AIProviderConfig{
		Enabled:  true,
		Provider: c.addProvPreset,
		Protocol: c.addProvProto,
		BaseURL:  c.addProvBaseURL,
		APIKey:   c.addProvAPIKey,
	}
	c.mu.Unlock()
	c.requestRedraw()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := testBuiltinConnection(ctx, draft)
		c.mu.Lock()
		c.addProvTesting = false
		if err != nil {
			c.addProvTestOK = false
			c.addProvTestErr = err.Error()
		} else {
			c.addProvTestOK = true
			c.addProvTestErr = ""
		}
		c.mu.Unlock()
		c.requestRedraw()
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

// probeSafeTransition keeps Scheme B enter/exit physics in normal app runs while
// settling layout transitions immediately when layout probes are active so
// frame-0 TargetBounds/TargetCenter probes match hit-test coordinates.
func probeSafeTransition(tr ui.ElementTransition) ui.ElementTransition {
	if bui.IsProbeEnabled() {
		tr.Animation = nil
		tr.Duration = -1
		tr.ExitDuration = -1
	}
	return tr
}
