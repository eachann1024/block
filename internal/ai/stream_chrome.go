package ai

import (
	"fmt"
	"math"
	"strings"
	"time"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

type streamChromeKey struct{}
type codeBlockCopiedKey struct{}
type composerRimCacheKey struct{}

type thinkTimingEntry struct {
	active   bool
	lastTick time.Time
	duration time.Duration
}

type streamChromeState struct {
	thinkTiming        map[string]*thinkTimingEntry
	codeTheme          *codeed.Theme
	codeThemeDark      bool
	codeThemeSkinID    string
	codeThemeSet       bool
	lastReplyRevealing bool
}

type composerRimCache struct {
	rect ui.Rect
	rad  float32
	path *ui.Path
}

func ensureStreamChrome(panel ui.Element) *streamChromeState {
	return ui.Local(panel, streamChromeKey{}, func() streamChromeState {
		return streamChromeState{
			thinkTiming: make(map[string]*thinkTimingEntry),
		}
	})
}

func aiMarkdownReveal() *markdown.Reveal {
	return &markdown.Reveal{
		Unit:    markdown.ByWord,
		CatchUp: 320 * time.Millisecond,
		Fade:    420 * time.Millisecond,
	}
}

func aiResolvedCodeFont() string {
	_, cdFont, _ := theme.Typography()
	f := strings.TrimSpace(cdFont)
	if f == "" || strings.EqualFold(f, "auto") {
		return "JetBrains Mono, Menlo, Monaco, Cascadia Mono, Consolas, DejaVu Sans Mono, Liberation Mono, monospace"
	}
	return f
}

func aiCodeEditorTheme(pal theme.Palette, chrome *streamChromeState) *codeed.Theme {
	skinID := ""
	if skin := theme.ActiveSkin(); skin != nil {
		skinID = skin.ID
	}
	if chrome != nil && chrome.codeThemeSet && chrome.codeThemeDark == pal.Dark && chrome.codeThemeSkinID == skinID && chrome.codeTheme != nil {
		return chrome.codeTheme
	}
	ct := &codeed.Theme{
		Background:        pal.CodeBlockBG,
		Text:              pal.Text,
		LineNumber:        pal.GutterText,
		ActiveLineNumber:  pal.Text,
		ActiveLine:        pal.CodeLine,
		Selection:         pal.SelectionBG,
		InactiveSelection: pal.SelectionBG.Alpha(0.55),
		Cursor:            pal.Accent,
		Match:             pal.FindMatch,
		CurrentMatch:      pal.FindActive,
		IndentGuide:       pal.Hairline,
		ActiveIndentGuide: pal.BtnBorder,
		WrapGuide:         pal.Hairline,
		Error:             pal.Danger,
		Warning:           pal.Warning,
		Info:              pal.Accent,
		Added:             pal.Success,
		Deleted:           pal.Danger,
		Modified:          pal.Accent,
	}
	set := func(c ui.Color, kinds ...codeed.TokenKind) {
		for _, k := range kinds {
			ct.Tokens[k].Color = c
		}
	}
	set(pal.SyntaxKeyword, codeed.TokenKeyword, codeed.TokenOperator)
	set(pal.SyntaxType, codeed.TokenType)
	set(pal.SyntaxFunction, codeed.TokenFunction, codeed.TokenMeta)
	set(pal.SyntaxKey, codeed.TokenProperty, codeed.TokenAttribute, codeed.TokenHeading)
	set(pal.SyntaxLiteral, codeed.TokenConstant)
	set(pal.SyntaxNumber, codeed.TokenNumber)
	set(pal.SyntaxString, codeed.TokenString, codeed.TokenEscape, codeed.TokenRegexp, codeed.TokenLink, codeed.TokenCode)
	set(pal.SyntaxTag, codeed.TokenTag)
	set(pal.SyntaxComment, codeed.TokenComment)
	set(pal.DiffAddFG, codeed.TokenInserted)
	set(pal.DiffDelFG, codeed.TokenDeleted)
	set(pal.Danger, codeed.TokenInvalid)
	ct.Tokens[codeed.TokenComment].Italic = false
	ct.Tokens[codeed.TokenHeading].Bold = true
	ct.Tokens[codeed.TokenStrong].Bold = true
	ct.Tokens[codeed.TokenEmphasis].Italic = true
	if chrome != nil {
		chrome.codeTheme = ct
		chrome.codeThemeDark = pal.Dark
		chrome.codeThemeSkinID = skinID
		chrome.codeThemeSet = true
	}
	return ct
}

func aiMarkdownTheme(pal theme.Palette, chrome *streamChromeState) *markdown.Theme {
	return &markdown.Theme{
		FontSize:       theme.FontBody,
		CodeFont:       aiResolvedCodeFont(),
		LineHeight:     1.62,
		CodeLineHeight: 1.55,
		Text:           pal.Text,
		Muted:          pal.TextSecondary,
		Link:           pal.Accent,
		Strong:         pal.Text,
		StrongWeight:   theme.WeightTitle,
		Code:           pal.Text,
		CodeBackground: pal.InlineCodeBG,
		CodeSize:       0.88,
		BlockSpacing:   0.95,
		ItemSpacing:    0.35,
		ListIndent:     1.45,
		Headings: [6]markdown.HeadingStyle{
			{Size: 1.30, Weight: theme.WeightTitle, Color: pal.Text, Top: 1.25, Bottom: 0.45, Rule: false},
			{Size: 1.16, Weight: theme.WeightTitle, Color: pal.Text, Top: 1.15, Bottom: 0.40, Rule: false},
			{Size: 1.05, Weight: theme.WeightTitle, Color: pal.Text, Top: 1.05, Bottom: 0.35, Rule: false},
			{Size: 1.00, Weight: theme.WeightTitle, Color: pal.Text, Top: 0.95, Bottom: 0.30, Rule: false},
			{Size: 0.96, Weight: theme.WeightTitle, Color: pal.Text, Top: 0.95, Bottom: 0.30, Rule: false},
			{Size: 0.92, Weight: theme.WeightTitle, Color: pal.TextSecondary, Top: 0.95, Bottom: 0.30, Rule: false},
		},
		QuoteBar:            pal.QuoteBar,
		QuoteBarWidth:       2,
		QuoteText:           pal.TextSecondary,
		Rule:                pal.Hairline,
		CodeBlockBackground: pal.CodeBlockBG,
		CodeBlockBorder:     pal.Hairline,
		CodeBlockRadius:     theme.RadiusLG,
		CodeBlockPadding:    0.85,
		CodeColors:          aiCodeEditorTheme(pal, chrome),
		TableBorder:         pal.Hairline,
		TableHeader:         pal.CodeBlockBG,
	}
}

func (c *Controller) renderAICodeBlock(cUI *ui.Context, b *markdown.CodeBlockView, chrome *streamChromeState) {
	if c.renderDeepUICodeBlock(cUI, b) {
		return
	}
	pal := theme.Ocean()
	cardKey := fmt.Sprintf("ai-code-%d-%d", b.SourceLine(), b.Index)
	card := ui.Column(cUI.Key(cardKey)).FillWidth().
		Background(pal.CodeBlockBG).
		Border(1, pal.Hairline).
		Radius(theme.RadiusLG).
		Clip()
	card.Role(ui.RoleGroup)

	copiedUntilPtr := ui.Local(card, codeBlockCopiedKey{}, func() time.Time {
		return time.Time{}
	})
	now := cUI.Now()
	copied := !copiedUntilPtr.IsZero() && now.Before(*copiedUntilPtr)

	lang := strings.TrimSpace(b.Language)
	if lang == "" {
		lang = "code"
	}

	card.Children(func() {
		head := ui.Row(cUI).FillWidth().Height(theme.HeightCompact).
			Padding(0, theme.Space8, 0, theme.Space12).Gap(theme.Space6).
			AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
		head.Children(func() {
			icons.Draw(cUI, icons.TbCode, theme.IconInline, pal.DuoIconColors(pal.TextSecondary))
			ui.Text(cUI, lang).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
				TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")

			copyLabel := theme.Tr("复制", "Copy")
			copyIcon := icons.Copy
			copyFG := pal.TextSecondary
			if copied {
				copyLabel = theme.Tr("已复制", "Copied")
				copyIcon = icons.Check
				copyFG = pal.Accent
			}
			copyBtn := ui.ButtonBase(cUI.Key("ai-code-copy-"+cardKey)).
				Height(theme.HeightMicro).Padding(0, theme.Space6).Gap(theme.Space4).
				AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
				Label(copyLabel)
			hovered := copyBtn.Hovered()
			pressed := copyBtn.Pressed()
			bg := ui.Transparent
			if pressed {
				bg = pal.Selected
			} else if hovered {
				bg = pal.Hover
			}
			bui.AnimateSurfaceColors(copyBtn, bg, ui.Transparent, true)
			copyBtn.Children(func() {
				icons.Draw(cUI, copyIcon, theme.IconMicro, pal.DuoIconColors(copyFG))
				ui.Text(cUI, copyLabel).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).
					TextColor(copyFG).SingleLine()
			})
			if copyBtn.Clicked() {
				c.mu.Lock()
				c.cancelPendingClipboardLocked()
				c.mu.Unlock()
				cUI.WriteClipboard(b.Code())
				*copiedUntilPtr = now.Add(1200 * time.Millisecond)
				cUI.After(1200 * time.Millisecond)
			}
		})

		ui.Box(cUI).FillWidth().Padding(theme.Space4, 0, 0, 0).Children(func() {
			b.Body(cUI)
		})
	})
}

func drawScrollEdgeMasks(wrap ui.Element, scrollState *ui.ScrollState, pal theme.Palette) {
	wrap.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 || scrollState == nil {
			return
		}
		y := scrollState.Y
		maxY := scrollState.MaxY
		if maxY <= 0 {
			return
		}
		const edgeH float32 = 24
		h := min(edgeH, r.H*0.5)
		if h <= 0 {
			return
		}
		if y > 2 {
			alpha := min((y-2)/edgeH, 1)
			topRect := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: h}
			p.FillGradient(topRect, ui.LinearGradient{
				Angle: 180,
				From:  pal.Paper.Alpha(alpha),
				To:    pal.Paper.Alpha(0),
			}, 0)
		}
		rem := maxY - y
		if rem > 2 {
			alpha := min((rem-2)/edgeH, 1)
			botRect := ui.Rect{X: r.X, Y: r.Y + r.H - h, W: r.W, H: h}
			p.FillGradient(botRect, ui.LinearGradient{
				Angle: 180,
				From:  pal.Paper.Alpha(0),
				To:    pal.Paper.Alpha(alpha),
			}, 0)
		}
	})
}

func roundRectStrokePath(x, y, w, h, rad float32) *ui.Path {
	p := &ui.Path{}
	if w <= 0 || h <= 0 {
		return p
	}
	r := min(max(rad, 0), min(w, h)*0.5)
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y)
	if r > 0 {
		const k float32 = 0.55228475
		c := r * k
		p.CubeTo(x+w-r+c, y, x+w, y+r-c, x+w, y+r)
		p.LineTo(x+w, y+h-r)
		p.CubeTo(x+w, y+h-r+c, x+w-r+c, y+h, x+w-r, y+h)
		p.LineTo(x+r, y+h)
		p.CubeTo(x+r-c, y+h, x, y+h-r+c, x, y+h-r)
		p.LineTo(x, y+r)
		p.CubeTo(x, y+r-c, x+r-c, y, x+r, y)
	} else {
		p.LineTo(x+w, y)
		p.LineTo(x+w, y+h)
		p.LineTo(x, y+h)
	}
	p.Close()
	return p
}

func applyComposerStreamingRim(shell ui.Element, pal theme.Palette, streaming bool) {
	target := float32(0)
	if streaming {
		target = 1
	}
	alpha := shell.AnimateTo("ai-composer-rim", target, ui.Animation{
		Duration:  220 * time.Millisecond,
		NonMotion: true,
	})
	if alpha <= 0.005 {
		return
	}
	cache := ui.Local(shell, composerRimCacheKey{}, func() composerRimCache {
		return composerRimCache{}
	})
	ais := pal.AISurface()
	baseBorder := ais.ComposerBorder
	secondaryHue := pal.SyntaxType

	shell.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 2 || r.H <= 2 {
			return
		}
		const inset float32 = 0.5
		rad := max(theme.RadiusLG-inset, 0)
		if cache.path == nil || cache.rect != r || cache.rad != rad {
			cache.rect = r
			cache.rad = rad
			cache.path = roundRectStrokePath(r.X+inset, r.Y+inset, r.W-inset*2, r.H-inset*2, rad)
		}
		if p.Preferences().ReduceMotion {
			p.StrokePath(cache.path, 1, pal.Accent.Alpha(0.65*alpha))
			return
		}
		const periodMs int64 = 2800
		phase := float32(p.Now().UnixMilli()%periodMs) / float32(periodMs)
		angle := phase * 360
		p.StrokePathConicGradient(cache.path, 1.25, ui.ConicGradient{
			Angle: angle,
			Oklab: true,
			Stops: []ui.GradientStop{
				{Color: baseBorder.Alpha(0), At: 0.00},
				{Color: pal.Accent.Alpha(0.32 * alpha), At: 0.14},
				{Color: pal.Accent.Alpha(0.88 * alpha), At: 0.28},
				{Color: secondaryHue.Alpha(0.72 * alpha), At: 0.40},
				{Color: baseBorder.Alpha(0), At: 0.56},
				{Color: baseBorder.Alpha(0), At: 1.00},
			},
		})
		p.AnimationFrameRate(60)
	})
}

func updateThinkTiming(chrome *streamChromeState, turnKey string, now time.Time, activelyThinking bool) int {
	if chrome == nil {
		return 0
	}
	entry := chrome.thinkTiming[turnKey]
	if entry == nil {
		entry = &thinkTimingEntry{}
		chrome.thinkTiming[turnKey] = entry
	}
	if activelyThinking {
		if !entry.active {
			entry.active = true
			entry.lastTick = now
		} else {
			if d := now.Sub(entry.lastTick); d > 0 && d < 30*time.Second {
				entry.duration += d
			}
			entry.lastTick = now
		}
	} else if entry.active {
		if d := now.Sub(entry.lastTick); d > 0 && d < 30*time.Second {
			entry.duration += d
		}
		entry.active = false
	}
	if entry.duration <= 0 {
		return 0
	}
	secs := int(math.Round(entry.duration.Seconds()))
	if secs == 0 && entry.duration >= 300*time.Millisecond {
		secs = 1
	}
	return secs
}

func formatProcessHeadline(steps []model.ProcessStep, outcome string, thinkSecs int) (headline string, toolSummary string, toolCount int) {
	thinkCount := 0
	var toolParts []string
	for _, st := range steps {
		if st.Kind == model.ToolKindThink {
			thinkCount++
			continue
		}
		toolCount++
		part := strings.TrimSpace(st.Title)
		if p := plainInline(st.Detail, nil, false); p != "" {
			if part != "" {
				part += " · " + p
			} else {
				part = p
			}
		}
		if part != "" && len(toolParts) < 4 {
			toolParts = append(toolParts, part)
		}
	}
	toolSummary = strings.Join(toolParts, "  ·  ")

	if outcome == "failed" {
		return theme.Tr("执行失败", "Execution failed"), toolSummary, toolCount
	}
	if outcome == "stopped" {
		return theme.Tr("已停止", "Stopped"), toolSummary, toolCount
	}

	toolNoun := "tool call"
	if toolCount != 1 {
		toolNoun = "tool calls"
	}
	switch {
	case thinkCount > 0 && thinkSecs > 0 && toolCount > 0:
		if !theme.IsEn() {
			headline = fmt.Sprintf("思考了 %d 秒，调用了 %d 次工具", thinkSecs, toolCount)
		} else {
			headline = fmt.Sprintf("Thought for %ds · %d %s", thinkSecs, toolCount, toolNoun)
		}
	case thinkCount > 0 && thinkSecs > 0:
		headline = fmt.Sprintf(theme.Tr("思考了 %d 秒", "Thought for %ds"), thinkSecs)
	case thinkCount > 0 && toolCount > 0:
		if !theme.IsEn() {
			headline = fmt.Sprintf("已思考，调用了 %d 次工具", toolCount)
		} else {
			headline = fmt.Sprintf("Thought · %d %s", toolCount, toolNoun)
		}
	case thinkCount > 0:
		headline = theme.Tr("已思考", "Thought")
	case toolCount > 0:
		if !theme.IsEn() {
			headline = fmt.Sprintf("调用了 %d 次工具", toolCount)
		} else {
			headline = fmt.Sprintf("Used %d %s", toolCount, toolNoun)
		}
	default:
		if !theme.IsEn() {
			headline = fmt.Sprintf("%d 步执行完成", len(steps))
		} else {
			headline = fmt.Sprintf("%d steps completed", len(steps))
		}
	}
	return headline, toolSummary, toolCount
}

func recentThinkPreviewLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	var nonEmpty []string
	for _, line := range parts {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}
	if len(nonEmpty) > 2 {
		nonEmpty = nonEmpty[len(nonEmpty)-2:]
	}
	return nonEmpty
}

func renderThinkingLive(cUI *ui.Context, pal theme.Palette, turnKey string, preview string) {
	slot := ui.Column(cUI.Key("ai-process-live-" + turnKey)).FillWidth().Gap(theme.Space4)
	bui.Probe(slot, "ai-process-live")
	slot.Children(func() {
		ui.Row(cUI.Key("ai-process-live-think-hdr-" + turnKey)).FillWidth().Height(theme.HeightCompact).
			PaddingX(theme.Space6).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
			bui.SparkleLoader(cUI, theme.IconInline, pal.TextSecondary, pal.Accent)
			bui.ShimmerText(cUI, theme.Tr("正在思考…", "Thinking…"), theme.FontBody, theme.WeightControl, pal.TextSecondary, pal.Text)
		})
		if lines := recentThinkPreviewLines(preview); len(lines) > 0 {
			prevBox := ui.Column(cUI.Key("ai-process-live-think-preview-"+turnKey)).FillWidth().
				Padding(0, theme.Space8, 0, theme.Space24+theme.Space4).Gap(theme.Space2).Clip()
			prevBox.DrawOver(func(p *ui.Painter, r ui.Rect) {
				if r.W <= 0 || r.H <= 0 {
					return
				}
				const fadeW float32 = 48
				w := min(fadeW, r.W)
				fadeRect := ui.Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}
				p.FillGradient(fadeRect, ui.LinearGradient{
					Angle: 90,
					From:  pal.Paper.Alpha(0),
					To:    pal.Paper,
				}, 0)
			})
			prevBox.Children(func() {
				for i, ln := range lines {
					ui.Text(cUI.Key(fmt.Sprintf("ai-think-ln-%s-%d", turnKey, i)), ln).
						FontSize(theme.FontMeta).LineHeight(1.45).
						TextColor(pal.TextSecondary.Alpha(0.75)).
						FillWidth().SingleLine()
				}
			})
		}
	})
}

func streamInstantSettle() bool {
	return bui.IsProbeEnabled()
}

func streamingScrollAnimation() *ui.Animation {
	if bui.IsProbeEnabled() {
		return nil
	}
	return &ui.Smooth
}

// SeedScenePreview populates an in-memory conversation for offscreen scene
// snapshots ("ai" and "ai-stream") without writing to disk.
func (c *Controller) SeedScenePreview(streaming bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mockResponder = true
	if !c.providerCfg.Enabled || !builtinReady(normalizeProviderConfig(c.providerCfg)) {
		c.providerCfg = model.AIProviderConfig{
			Enabled:  true,
			Provider: "deepseek",
			Protocol: model.ProtocolOpenAI,
			BaseURL:  "https://api.deepseek.com/v1",
			APIKey:   "sk-preview-shot",
			Model:    "deepseek-chat",
		}
	}
	c.open = true
	c.historyOpen = false
	c.deckOpen = false
	c.lightboxOpen = false
	c.draftText = ""
	c.pickerDismissed = false

	const previewConvID uint64 = 900001
	steps := []model.ProcessStep{
		{
			ID:     "think-1",
			Kind:   model.ToolKindThink,
			Title:  "梳理流墨纸面与细环流光实现边界",
			Detail: "先确认 Ocean Paper 的圆角、字号与色彩 token，再把代码块顶栏复制状态和输入框 1px 细环流光全部收进 ui.Local 状态里，避免污染持久层。",
			Done:   true,
		},
		{
			ID:       "read-1",
			Kind:     model.ToolKindRead,
			Title:    "读取样式基线",
			Detail:   "Ocean Paper.md",
			ToolName: "readPage",
			FileRef:  "Ocean Paper",
			Done:     true,
		},
		{
			ID:       "run-1",
			Kind:     model.ToolKindRun,
			Title:    "校验设计令牌",
			Detail:   "lint_design.py",
			ToolName: "runCheck",
			Done:     true,
		},
	}

	turn1Answer := strings.Join([]string{
		"已读取 **Ocean Paper.md** 的设计约束，核心纸面与容器规则如下：",
		"",
		"- **主纸面与层级**：对话区直接使用 `pal.Paper` 与 `theme.FontBody`，取消多余卡片底框以保持阅读呼吸感。",
		"- **圆角与描边**：代码块与过程胶囊统一采用 `theme.RadiusLG` / `theme.RadiusSM` 配合 `1px` `pal.Hairline` 细描边。",
		"- **字体与间距**：正文行高 `1.62`、代码块等宽行高 `1.55`，列表缩进 `1.45em`，标题移除底线改为克制字阶。",
	}, "\n")

	turn2Answer := strings.Join([]string{
		"### 流墨纸面与细环流光要点",
		"",
		"已按 **Ocean Paper** 规范将流式渲染与控件边界统一到 `stream_chrome.go`：",
		"",
		"- **流墨渐显**：`markdown.ByWord` 配合 `320ms` 追赶与 `420ms` 淡入，取消标题底线。",
		"- **轻量代码块**：保留 `b.Body(cUI)` 原生跨块选区与语法高亮，顶栏支持即时复制反馈。",
		"- **1px 细环流光**：在 `ai-composer-shell` 边框线巡航，静止时零重绘。",
		"",
		"```go",
		"func renderStreamHint(shell ui.Element, pal theme.Palette, active bool) {",
		"\talpha := shell.AnimateTo(\"ai-composer-rim\", boolToFloat(active), rimAnim)",
		"\tif alpha <= 0.005 { return }",
		"\tshell.DrawOver(func(p *ui.Painter, r ui.Rect) {",
		"\t\tp.StrokePathConicGradient(cachedRimPath(r), 1.25, conicGradient(pal, alpha))",
		"\t})",
		"}",
		"```",
		"",
		"结论：过程摘要在正文输出后自动收拢为低噪单行入口，长列表滚动时边缘显现 `24px` 柔化纸面。",
	}, "\n")

	msgs := []model.ChatMessage{
		{
			Role:    "user",
			Content: "先帮我看一下 Ocean Paper.md 里的卡片圆角和描边规范。",
		},
		{
			Role:    "assistant",
			Content: turn1Answer,
			Sources: []model.SourceNote{{Title: "Ocean Paper", Path: "Ocean Paper.md"}},
		},
		{
			Role:    "user",
			Content: "帮我落地流墨纸面和输入框 1px 细环流光，并给出关键 Go 代码示例与验证要点。",
		},
	}

	if !streaming {
		msgs = append(msgs, model.ChatMessage{
			Role:    "assistant",
			Content: turn2Answer,
			Process: steps,
			Sources: []model.SourceNote{{Title: "Ocean Paper", Path: "Ocean Paper.md"}},
		})
	}

	conv := model.Conversation{
		ID:       previewConvID,
		Title:    "流墨纸面与细环流光预览",
		Backend:  model.BackendBuiltin,
		Messages: msgs,
	}

	replaced := false
	for i := range c.convs {
		if c.convs[i].ID == previewConvID {
			c.convs[i] = conv
			replaced = true
			break
		}
	}
	if !replaced {
		c.convs = append(c.convs, conv)
	}
	c.currentID = previewConvID
	delete(c.expandedProcess, fmt.Sprintf("%d-0", previewConvID))
	delete(c.expandedProcess, fmt.Sprintf("%d-2", previewConvID))

	st := c.ensureRunStateLocked(previewConvID)
	st.turnError = ""
	st.turnErrorSettings = false
	st.sentDraft = nil
	st.sentSelection = nil
	st.partialThink.Reset()
	if streaming {
		st.streaming = true
		st.partialSteps = steps
		st.partialText.Reset()
		st.partialText.WriteString(turn2Answer)
	} else {
		st.streaming = false
		st.partialSteps = nil
		st.partialText.Reset()
	}

	c.scrollToBottom = true
	c.scrollToBottomForce = true
	c.streamCache = nil
}
