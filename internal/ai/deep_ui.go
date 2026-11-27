package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// showDeepAISetting keeps the "深层次 AI" switch hidden until it is ready to
// ship; the feature itself stays on by default.
const showDeepAISetting = false

const deepAIConfigFile = "ai-deep.json"

// deepUIFenceLangs are the code fence languages rendered as native components.
var deepUIFenceLangs = map[string]bool{"ui": true, "blockui": true, "genui": true}

// deepUIPromptHint is the only always-on prompt text; the component catalog
// stays in the deepUI skill and is loaded on demand.
const deepUIPromptHint = "# 深层次界面\n\n回答含对比、≥3 项数据或趋势、流程步骤、多项结论或清单时，积极用界面组件代替长文：先 `loadSkill` 加载 `deepUI` 取组件目录，再输出 ```ui 围栏 JSON。按内容灵活搭配多种组件，不要每次只用同一种。纯问答、单句结论或修改笔记时不用。组件外正文不超过 3 行，不复述画面内容。"

type deepAIConfig struct {
	Enabled bool `json:"enabled"`
}

type deepFoldKey struct{}
type deepTabsKey struct{}

func (c *Controller) loadDeepAIConfigLocked() {
	cfg := deepAIConfig{Enabled: true}
	_ = c.readJSONFile(deepAIConfigFile, &cfg)
	c.deepAI = cfg.Enabled
}

// SetDeepAI toggles native component replies and their prompt hint.
func (c *Controller) SetDeepAI(on bool) {
	c.mu.Lock()
	c.deepAI = on
	_ = c.writeJSONFile(deepAIConfigFile, deepAIConfig{Enabled: on})
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) deepAIEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deepAI
}

func (c *Controller) renderDeepAISettingRow(cUI *ui.Context) {
	if !showDeepAISetting {
		return
	}
	on := c.deepAIEnabled()
	bui.SettingsRow(cUI, theme.Tr("深层次 AI", "Deep AI"), func() {
		if bui.Switch(cUI, "ai-deep-ui-switch", theme.Tr("深层次 AI", "Deep AI"), &on) {
			c.SetDeepAI(on)
		}
	})
}

// ---- spec parsing ----

type deepNode = map[string]any

func parseDeepUISpec(code string) (title, foot string, blocks []deepNode, ok bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", "", nil, false
	}
	var raw any
	if err := json.Unmarshal([]byte(code), &raw); err != nil {
		return "", "", nil, false
	}
	switch v := raw.(type) {
	case []any:
		blocks = deepNodes(v)
	case map[string]any:
		if _, has := v["blocks"]; has {
			title = deepStr(v, "title")
			foot = deepStr(v, "foot", "note")
			blocks = deepNodes(deepArr(v, "blocks"))
		} else if deepType(v) != "" {
			blocks = []deepNode{v}
		}
	}
	return title, foot, blocks, len(blocks) > 0
}

func deepNodes(xs []any) []deepNode {
	out := make([]deepNode, 0, len(xs))
	for _, x := range xs {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func deepType(m deepNode) string {
	return strings.ToLower(deepStr(m, "t", "type", "kind"))
}

func deepStr(m deepNode, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(v)
		}
	}
	return ""
}

func deepNum(m deepNode, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v, true
		case string:
			if f, ok := leadingNumber(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func leadingNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	end := 0
	for end < len(s) {
		ch := s[end]
		if (ch >= '0' && ch <= '9') || ch == '.' || (end == 0 && (ch == '-' || ch == '+')) {
			end++
			continue
		}
		break
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	return f, err == nil
}

func deepArr(m deepNode, keys ...string) []any {
	for _, k := range keys {
		if v, ok := m[k].([]any); ok {
			return v
		}
	}
	return nil
}

func deepStrs(m deepNode, keys ...string) []string {
	var out []string
	for _, x := range deepArr(m, keys...) {
		switch v := x.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				out = append(out, s)
			}
		case float64:
			out = append(out, strconv.FormatFloat(v, 'f', -1, 64))
		case map[string]any:
			if s := deepStr(v, "l", "label", "name", "text"); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func deepBool(m deepNode, keys ...string) bool {
	for _, k := range keys {
		switch v := m[k].(type) {
		case bool:
			return v
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			return s == "true" || s == "done" || s == "ok" || s == "yes" || s == "1"
		case float64:
			return v != 0
		}
	}
	return false
}

func formatDeepNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ---- tones ----

func deepTone(pal theme.Palette, tone string) (fg, surface ui.Color) {
	switch strings.ToLower(tone) {
	case "ok", "good", "success", "pass", "up":
		return pal.Success, pal.SuccessSurface
	case "warn", "warning", "risk":
		return pal.Warning, pal.WarningSurface
	case "bad", "danger", "error", "fail", "down":
		return pal.Danger, pal.DangerSurface
	case "info", "accent", "primary":
		return pal.Accent, pal.Accent.Alpha(0.1)
	}
	return pal.TextSecondary, pal.CodeBlockBG
}

func deepSeries(pal theme.Palette, i int) ui.Color {
	cs := []ui.Color{pal.Accent, pal.SyntaxType, pal.SyntaxString, pal.Warning, pal.SyntaxKeyword, pal.SyntaxNumber}
	return cs[((i%len(cs))+len(cs))%len(cs)]
}

// ---- rendering ----

// renderDeepUICodeBlock renders a ```ui fence as native components. It returns
// false when the block should fall back to an ordinary code card.
func (c *Controller) renderDeepUICodeBlock(cUI *ui.Context, b *markdown.CodeBlockView) bool {
	if !deepUIFenceLangs[strings.ToLower(strings.TrimSpace(b.Language))] || !c.deepAIEnabled() {
		return false
	}
	pal := theme.Ocean()
	key := fmt.Sprintf("ai-deep-%d-%d", b.SourceLine(), b.Index)
	if b.Open {
		ui.Row(cUI.Key(key+"-pending")).FillWidth().Height(theme.HeightLarge).PaddingX(theme.Space12).Gap(theme.Space8).
			AlignItems(ui.Center).Background(pal.CodeBlockBG).Border(1, pal.Hairline).Radius(theme.RadiusLG).Children(func() {
			bui.SparkleLoader(cUI, theme.IconInline, pal.TextSecondary, pal.Accent)
			bui.ShimmerText(cUI, theme.Tr("正在生成界面…", "Building view…"), theme.FontMeta, theme.WeightControl, pal.TextSecondary, pal.Text)
		})
		return true
	}
	title, foot, blocks, ok := parseDeepUISpec(b.Code())
	if !ok {
		return false
	}
	card := ui.Column(cUI.Key(key)).FillWidth().Gap(theme.Space12).Padding(theme.Space12).
		Background(pal.Paper).Border(1, pal.Hairline).Radius(theme.RadiusLG)
	card.Role(ui.RoleGroup)
	card.Children(func() {
		if title != "" {
			ui.Text(cUI, title).FontSize(theme.FontBody).FontWeight(theme.WeightTitle).TextColor(pal.Text).FillWidth()
		}
		for i, blk := range blocks {
			wrap := ui.Column(cUI.Key(fmt.Sprintf("%s-b%d", key, i))).FillWidth()
			wrap.Transition(probeSafeTransition(bui.CascadeItemTransition(i, false)))
			wrap.Children(func() {
				renderDeepBlock(cUI, pal, fmt.Sprintf("%s-b%d", key, i), blk, 0)
			})
		}
		if foot != "" {
			ui.Text(cUI, foot).FontSize(theme.FontMicro).LineHeight(1.45).TextColor(pal.TextSecondary).FillWidth()
		}
	})
	return true
}

func renderDeepBlock(cUI *ui.Context, pal theme.Palette, key string, m deepNode, depth int) {
	if depth > 3 {
		return
	}
	t := deepType(m)
	title := deepStr(m, "title")
	sub := deepStr(m, "sub", "subtitle")
	ui.Column(cUI.Key(key+"-sec")).FillWidth().Gap(theme.Space8).Children(func() {
		if t != "fold" && t != "callout" && (title != "" || sub != "") {
			ui.Column(cUI).FillWidth().Gap(theme.Space2).Children(func() {
				if title != "" {
					ui.Text(cUI, title).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).FillWidth()
				}
				if sub != "" {
					ui.Text(cUI, sub).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).FillWidth()
				}
			})
		}
		switch t {
		case "kpi", "metric", "metrics", "stats":
			renderDeepKPI(cUI, pal, key, m)
		case "bars", "bar":
			renderDeepBars(cUI, pal, key, m, false)
		case "progress":
			renderDeepBars(cUI, pal, key, m, true)
		case "line", "trend":
			renderDeepLine(cUI, pal, key, m)
		case "share", "pie", "donut":
			renderDeepShare(cUI, pal, key, m)
		case "table":
			renderDeepTable(cUI, pal, key, m)
		case "cards", "options":
			renderDeepCards(cUI, pal, key, m)
		case "steps", "timeline", "flow":
			renderDeepSteps(cUI, pal, key, m)
		case "checklist", "todo", "checks":
			renderDeepChecklist(cUI, pal, key, m)
		case "tags", "chips":
			renderDeepTags(cUI, pal, key, m)
		case "callout", "note", "alert":
			renderDeepCallout(cUI, pal, m)
		case "proscons", "compare":
			renderDeepProsCons(cUI, pal, key, m)
		case "group", "row", "grid":
			renderDeepGroup(cUI, pal, key, m, depth)
		case "fold", "details":
			renderDeepFold(cUI, pal, key, m, depth)
		case "tabs":
			renderDeepTabs(cUI, pal, key, m, depth)
		default:
			renderDeepText(cUI, pal, key, m)
		}
	})
}

func renderDeepText(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	if s := deepStr(m, "text", "md", "body", "content"); s != "" {
		ui.Text(cUI.Key(key+"-txt"), s).FontSize(theme.FontSmall).LineHeight(1.55).TextColor(pal.Text).FillWidth()
	}
	if pts := deepStrs(m, "items", "pts", "points"); len(pts) > 0 {
		renderDeepBullets(cUI, pal, key, pts, pal.TextSecondary)
	}
}

func renderDeepBullets(cUI *ui.Context, pal theme.Palette, key string, pts []string, dot ui.Color) {
	ui.Column(cUI.Key(key+"-pts")).FillWidth().Gap(theme.Space4).Children(func() {
		for i, p := range pts {
			ui.Row(cUI.Key(fmt.Sprintf("%s-pt%d", key, i))).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
				ui.Box(cUI).Size(5, 5).Margin(7, 0, 0, 0).Radius(theme.RadiusPill).Background(dot).Shrink(0)
				ui.Text(cUI, p).FontSize(theme.FontSmall).LineHeight(1.5).TextColor(pal.Text).Grow(1).MinWidth(0)
			})
		}
	})
}

func renderDeepKPI(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepNodes(deepArr(m, "items"))
	ui.Row(cUI.Key(key+"-kpi")).FillWidth().Wrap().Gap(theme.Space8).Children(func() {
		for i, it := range items {
			fg, _ := deepTone(pal, deepStr(it, "tone"))
			tile := ui.Column(cUI.Key(fmt.Sprintf("%s-k%d", key, i))).Basis(116).Grow(1).MinWidth(0).
				Gap(theme.Space2).Padding(theme.Space8, theme.Space10).Background(pal.CodeBlockBG).Radius(theme.RadiusMD)
			tile.Children(func() {
				ui.Text(cUI, deepStr(it, "l", "label", "name")).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).FillWidth().SingleLine().Ellipsis("…")
				ui.Row(cUI).FillWidth().Gap(theme.Space6).AlignItems(ui.Baseline).Children(func() {
					ui.Text(cUI, deepStr(it, "v", "value")).FontSize(theme.FontTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text).MinWidth(0).SingleLine().Ellipsis("…")
					if tag := deepStr(it, "tag", "delta"); tag != "" {
						ui.Text(cUI, tag).FontSize(theme.FontMicro).FontWeight(theme.WeightControl).TextColor(fg).SingleLine().Shrink(0)
					}
				})
				if s := deepStr(it, "s", "sub", "note"); s != "" {
					ui.Text(cUI, s).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).FillWidth()
				}
			})
		}
	})
}

func renderDeepBars(cUI *ui.Context, pal theme.Palette, key string, m deepNode, percent bool) {
	items := deepNodes(deepArr(m, "items"))
	unit := deepStr(m, "unit")
	maxV := 0.0
	if percent {
		maxV = 100
	}
	for _, it := range items {
		v, _ := deepNum(it, "v", "value")
		maxV = math.Max(maxV, math.Abs(v))
	}
	if maxV <= 0 {
		maxV = 1
	}
	ui.Column(cUI.Key(key+"-bars")).FillWidth().Gap(theme.Space8).Children(func() {
		for i, it := range items {
			v, _ := deepNum(it, "v", "value")
			label := deepStr(it, "l", "label", "name")
			valText := deepStr(it, "tag")
			if valText == "" {
				valText = formatDeepNum(v) + unit
				if percent && unit == "" {
					valText = formatDeepNum(v) + "%"
				}
			}
			col := deepSeries(pal, 0)
			if tone := deepStr(it, "tone"); tone != "" {
				col, _ = deepTone(pal, tone)
			} else if !percent && deepBool(m, "multi", "colorful") {
				col = deepSeries(pal, i)
			}
			frac := float32(math.Min(math.Abs(v)/maxV, 1))
			row := ui.Column(cUI.Key(fmt.Sprintf("%s-bar%d", key, i))).FillWidth().Gap(theme.Space4)
			row.Children(func() {
				ui.Row(cUI).FillWidth().Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
					ui.Text(cUI, label).FontSize(theme.FontMeta).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					ui.Text(cUI, valText).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				})
				track := ui.Box(cUI).FillWidth().Height(6).Radius(theme.RadiusPill).Background(pal.Hover)
				track.Draw(func(p *ui.Painter, r ui.Rect) {
					if frac <= 0 {
						return
					}
					w := max(r.W*frac, r.H)
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, col, r.H/2)
				})
			})
			if s := deepStr(it, "s", "note"); s != "" {
				bui.Tooltip(cUI, row, s)
			}
		}
	})
}

func renderDeepLine(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	xs := deepStrs(m, "x", "labels")
	type series struct {
		name string
		vals []float64
		col  ui.Color
	}
	var ss []series
	for i, sv := range deepNodes(deepArr(m, "series")) {
		var vals []float64
		for _, x := range deepArr(sv, "vals", "values", "data") {
			switch n := x.(type) {
			case float64:
				vals = append(vals, n)
			case string:
				if f, ok := leadingNumber(n); ok {
					vals = append(vals, f)
				}
			}
		}
		if len(vals) > 0 {
			ss = append(ss, series{name: deepStr(sv, "name", "l", "label"), vals: vals, col: deepSeries(pal, i)})
		}
	}
	if len(ss) == 0 {
		return
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	n := 0
	for _, s := range ss {
		n = max(n, len(s.vals))
		for _, v := range s.vals {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if hi-lo < 1e-9 {
		lo, hi = lo-1, hi+1
	}
	pad := (hi - lo) * 0.08
	lo, hi = lo-pad, hi+pad
	unit := deepStr(m, "yUnit", "unit")
	ui.Column(cUI.Key(key+"-line")).FillWidth().Gap(theme.Space6).Children(func() {
		ui.Row(cUI).FillWidth().Justify(ui.SpaceBetween).Children(func() {
			ui.Text(cUI, formatDeepNum(math.Round((hi-pad)*100)/100)+unit).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).SingleLine()
			ui.Text(cUI, formatDeepNum(math.Round((lo+pad)*100)/100)+unit).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).SingleLine()
		})
		chart := ui.Box(cUI.Key(key+"-chart")).FillWidth().Height(132)
		chart.Draw(func(p *ui.Painter, r ui.Rect) {
			if r.W <= 0 || r.H <= 0 {
				return
			}
			top, bot := r.Y+6, r.Y+r.H-6
			for g := 0; g < 4; g++ {
				y := top + (bot-top)*float32(g)/3
				p.Line(r.X, y, r.X+r.W, y, 1, pal.Hairline)
			}
			xAt := func(i int) float32 {
				if n <= 1 {
					return r.X + r.W/2
				}
				return r.X + 4 + (r.W-8)*float32(i)/float32(n-1)
			}
			yAt := func(v float64) float32 {
				return bot - (bot-top)*float32((v-lo)/(hi-lo))
			}
			for _, s := range ss {
				pts := make([][2]float32, 0, len(s.vals))
				for i, v := range s.vals {
					pts = append(pts, [2]float32{xAt(i), yAt(v)})
				}
				if len(pts) > 1 {
					p.StrokePolyline(pts, 2, s.col)
				}
				for _, pt := range pts {
					p.Fill(ui.Rect{X: pt[0] - 3, Y: pt[1] - 3, W: 6, H: 6}, pal.Paper, 3)
					p.Fill(ui.Rect{X: pt[0] - 2, Y: pt[1] - 2, W: 4, H: 4}, s.col, 2)
				}
			}
		})
		if len(xs) > 0 {
			shown := xs
			if len(xs) > 5 {
				shown = []string{xs[0], xs[len(xs)/2], xs[len(xs)-1]}
			}
			ui.Row(cUI).FillWidth().Justify(ui.SpaceBetween).Children(func() {
				for _, x := range shown {
					ui.Text(cUI, x).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).SingleLine()
				}
			})
		}
		if len(ss) > 1 || ss[0].name != "" {
			renderDeepLegend(cUI, pal, key, func(yield func(string, ui.Color)) {
				for _, s := range ss {
					yield(s.name, s.col)
				}
			})
		}
		if note := deepStr(m, "note"); note != "" {
			ui.Text(cUI, note).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).FillWidth()
		}
	})
}

func renderDeepLegend(cUI *ui.Context, pal theme.Palette, key string, each func(yield func(string, ui.Color))) {
	ui.Row(cUI.Key(key+"-legend")).FillWidth().Wrap().Gap(theme.Space12).Children(func() {
		each(func(name string, col ui.Color) {
			ui.Row(cUI).Gap(theme.Space4).AlignItems(ui.Center).Children(func() {
				ui.Box(cUI).Size(8, 8).Radius(theme.RadiusPill).Background(col).Shrink(0)
				ui.Text(cUI, name).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).SingleLine()
			})
		})
	})
}

func renderDeepShare(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepNodes(deepArr(m, "items"))
	total := 0.0
	vals := make([]float64, len(items))
	for i, it := range items {
		v, _ := deepNum(it, "v", "value")
		vals[i] = math.Max(v, 0)
		total += vals[i]
	}
	if total <= 0 {
		return
	}
	ui.Row(cUI.Key(key+"-share")).FillWidth().Wrap().Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
		ring := ui.Box(cUI.Key(key+"-ring")).Size(104, 104).Shrink(0)
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			const thick float32 = 14
			rad := min(r.W, r.H)/2 - thick/2
			start := -math.Pi / 2
			gap := 0.0
			if len(vals) > 1 {
				gap = 0.04
			}
			for i, v := range vals {
				sweep := v / total * 2 * math.Pi
				a0, a1 := start+gap/2, start+sweep-gap/2
				start += sweep
				if a1 <= a0 {
					continue
				}
				steps := max(int((a1-a0)/0.05), 2)
				pts := make([][2]float32, 0, steps+1)
				for s := 0; s <= steps; s++ {
					a := a0 + (a1-a0)*float64(s)/float64(steps)
					pts = append(pts, [2]float32{cx + rad*float32(math.Cos(a)), cy + rad*float32(math.Sin(a))})
				}
				col := deepSeries(pal, i)
				if tone := deepStr(items[i], "tone"); tone != "" {
					col, _ = deepTone(pal, tone)
				}
				p.StrokePolyline(pts, thick, col)
			}
		})
		ui.Column(cUI).Basis(140).Grow(1).MinWidth(0).Gap(theme.Space6).Children(func() {
			for i, it := range items {
				col := deepSeries(pal, i)
				if tone := deepStr(it, "tone"); tone != "" {
					col, _ = deepTone(pal, tone)
				}
				pct := vals[i] / total * 100
				ui.Row(cUI.Key(fmt.Sprintf("%s-sl%d", key, i))).FillWidth().Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
					ui.Box(cUI).Size(8, 8).Radius(theme.RadiusPill).Background(col).Shrink(0)
					ui.Text(cUI, deepStr(it, "l", "label", "name")).FontSize(theme.FontMeta).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					ui.Text(cUI, fmt.Sprintf("%.0f%%", pct)).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
				})
			}
		})
	})
}

func renderDeepTable(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	cols := deepStrs(m, "cols", "columns", "head")
	var rows [][]string
	for _, r := range deepArr(m, "rows") {
		switch rv := r.(type) {
		case []any:
			var cells []string
			for _, cell := range rv {
				switch cv := cell.(type) {
				case string:
					cells = append(cells, cv)
				case float64:
					cells = append(cells, formatDeepNum(cv))
				case bool:
					if cv {
						cells = append(cells, "✓")
					} else {
						cells = append(cells, "—")
					}
				default:
					cells = append(cells, "")
				}
			}
			rows = append(rows, cells)
		case map[string]any:
			var cells []string
			for _, c := range cols {
				cells = append(cells, deepStr(rv, c))
			}
			rows = append(rows, cells)
		}
	}
	n := len(cols)
	for _, r := range rows {
		n = max(n, len(r))
	}
	if n == 0 {
		return
	}
	cellRow := func(rk string, cells []string, head bool, last bool) {
		row := ui.Row(cUI.Key(rk)).FillWidth().Gap(theme.Space8).Padding(theme.Space6, theme.Space10).AlignItems(ui.Start)
		if head {
			row.Background(pal.CodeBlockBG)
		}
		if !last {
			row.BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
		}
		hovered := !head && row.Hovered()
		if hovered {
			row.Background(pal.Hover)
		}
		row.Children(func() {
			for i := 0; i < n; i++ {
				s := ""
				if i < len(cells) {
					s = cells[i]
				}
				fg, weight := pal.Text, theme.WeightRegular
				if head {
					fg, weight = pal.TextSecondary, theme.WeightControl
				} else if i == 0 {
					weight = theme.WeightControl
				}
				ui.Text(cUI, s).FontSize(theme.FontMeta).FontWeight(weight).LineHeight(1.45).TextColor(fg).Basis(0).Grow(1).MinWidth(0)
			}
		})
	}
	ui.Column(cUI.Key(key+"-table")).FillWidth().Border(1, pal.Hairline).Radius(theme.RadiusMD).Clip().Children(func() {
		if len(cols) > 0 {
			cellRow(key+"-th", cols, true, len(rows) == 0)
		}
		for i, r := range rows {
			cellRow(fmt.Sprintf("%s-tr%d", key, i), r, false, i == len(rows)-1)
		}
	})
}

func renderDeepCards(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepNodes(deepArr(m, "items"))
	ui.Row(cUI.Key(key+"-cards")).FillWidth().Wrap().Gap(theme.Space8).Children(func() {
		for i, it := range items {
			fg, surface := deepTone(pal, deepStr(it, "tone"))
			pick := deepBool(it, "pick", "best", "recommended")
			bd := pal.Hairline
			if pick {
				bd = pal.Accent
			}
			card := ui.Column(cUI.Key(fmt.Sprintf("%s-c%d", key, i))).Basis(200).Grow(1).MinWidth(0).
				Gap(theme.Space6).Padding(theme.Space10, theme.Space12).Border(1, bd).Radius(theme.RadiusMD)
			if card.Hovered() {
				card.Background(pal.Hover)
			}
			card.Children(func() {
				ui.Row(cUI).FillWidth().Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
					ui.Text(cUI, deepStr(it, "name", "l", "label", "title")).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(pal.Text).Grow(1).MinWidth(0)
					badge := deepStr(it, "badge", "tag")
					if badge == "" && pick {
						badge = theme.Tr("推荐", "Pick")
						fg, surface = pal.Accent, pal.Accent.Alpha(0.1)
					}
					if badge != "" {
						ui.Text(cUI, badge).FontSize(theme.FontMicro).FontWeight(theme.WeightControl).TextColor(fg).
							Background(surface).Radius(theme.RadiusPill).Padding(theme.Space2, theme.Space6).SingleLine().Shrink(0)
					}
				})
				if d := deepStr(it, "desc", "s", "text"); d != "" {
					ui.Text(cUI, d).FontSize(theme.FontMeta).LineHeight(1.45).TextColor(pal.TextSecondary).FillWidth()
				}
				if pts := deepStrs(it, "pts", "points", "items"); len(pts) > 0 {
					renderDeepBullets(cUI, pal, fmt.Sprintf("%s-c%d", key, i), pts, fg)
				}
			})
		}
	})
}

func renderDeepSteps(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepArr(m, "items", "steps")
	ui.Column(cUI.Key(key+"-steps")).FillWidth().Children(func() {
		for i, raw := range items {
			var label, sub, state string
			switch v := raw.(type) {
			case string:
				label = v
			case map[string]any:
				label = deepStr(v, "l", "label", "name", "title")
				sub = deepStr(v, "s", "sub", "desc", "note")
				state = strings.ToLower(deepStr(v, "state", "status"))
				if state == "" && deepBool(v, "done", "ok") {
					state = "done"
				}
			}
			last := i == len(items)-1
			const dot float32 = 16
			row := ui.Row(cUI.Key(fmt.Sprintf("%s-s%d", key, i))).FillWidth().Gap(theme.Space10).AlignItems(ui.Start)
			if !last {
				row.Padding(0, 0, theme.Space10, 0)
				row.DrawOver(func(p *ui.Painter, r ui.Rect) {
					x := r.X + dot/2
					p.Line(x, r.Y+dot+3, x, r.Y+r.H-1, 1, pal.Hairline)
				})
			}
			row.Children(func() {
				ind := ui.Box(cUI).Size(dot, dot).Radius(theme.RadiusPill).Shrink(0).AlignItems(ui.Center).Justify(ui.Center)
				switch state {
				case "done", "ok", "complete", "completed":
					ind.Background(pal.Accent).Children(func() {
						icons.Draw(cUI, icons.Check, theme.IconMicro, pal.DuoIconColors(pal.AccentText))
					})
				case "doing", "active", "current", "now", "running":
					ind.Border(2, pal.Accent).Children(func() {
						ui.Box(cUI).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Accent)
					})
				case "fail", "bad", "blocked", "error":
					ind.Background(pal.DangerSurface).Border(1, pal.Danger)
				default:
					ind.Border(1, pal.BtnBorder)
				}
				ui.Column(cUI).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					fg := pal.Text
					if state == "" || state == "todo" || state == "pending" {
						fg = pal.TextSecondary
					}
					ui.Text(cUI, label).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).LineHeight(1.4).TextColor(fg).FillWidth()
					if sub != "" {
						ui.Text(cUI, sub).FontSize(theme.FontMeta).LineHeight(1.45).TextColor(pal.TextSecondary).FillWidth()
					}
				})
			})
		}
	})
}

func renderDeepChecklist(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepArr(m, "items")
	ui.Column(cUI.Key(key+"-checks")).FillWidth().Gap(theme.Space6).Children(func() {
		for i, raw := range items {
			label, ok := "", false
			switch v := raw.(type) {
			case string:
				label = v
			case map[string]any:
				label = deepStr(v, "l", "label", "name", "text")
				ok = deepBool(v, "ok", "done", "checked", "state")
			}
			ui.Row(cUI.Key(fmt.Sprintf("%s-ck%d", key, i))).FillWidth().Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
				box := ui.Box(cUI).Size(16, 16).Margin(1, 0, 0, 0).Radius(theme.RadiusXS).Shrink(0).AlignItems(ui.Center).Justify(ui.Center)
				if ok {
					box.Background(pal.Success).Children(func() {
						icons.Draw(cUI, icons.Check, theme.IconMicro, pal.DuoIconColors(pal.AccentText))
					})
				} else {
					box.Border(1, pal.BtnBorder)
				}
				fg := pal.Text
				if ok {
					fg = pal.TextSecondary
				}
				ui.Text(cUI, label).FontSize(theme.FontSmall).LineHeight(1.45).TextColor(fg).Grow(1).MinWidth(0)
			})
		}
	})
}

func renderDeepTags(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	items := deepArr(m, "items", "tags")
	ui.Row(cUI.Key(key+"-tags")).FillWidth().Wrap().Gap(theme.Space6).Children(func() {
		for _, raw := range items {
			label, tone := "", ""
			switch v := raw.(type) {
			case string:
				label = v
			case map[string]any:
				label = deepStr(v, "l", "label", "name", "text")
				tone = deepStr(v, "tone")
			}
			fg, surface := deepTone(pal, tone)
			if tone == "" {
				fg = pal.Text
			}
			ui.Text(cUI, label).FontSize(theme.FontMeta).TextColor(fg).Background(surface).
				Radius(theme.RadiusPill).Padding(theme.Space2, theme.Space8).SingleLine()
		}
	})
}

func renderDeepCallout(cUI *ui.Context, pal theme.Palette, m deepNode) {
	tone := deepStr(m, "tone")
	if tone == "" {
		tone = "info"
	}
	fg, surface := deepTone(pal, tone)
	box := ui.Column(cUI).FillWidth().Gap(theme.Space2).Padding(theme.Space8, theme.Space12, theme.Space8, theme.Space16).
		Background(surface).Radius(theme.RadiusMD).Clip()
	box.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, fg, 0)
	})
	box.Children(func() {
		if t := deepStr(m, "title"); t != "" {
			ui.Text(cUI, t).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(fg).FillWidth()
		}
		if s := deepStr(m, "text", "body", "md", "sub"); s != "" {
			ui.Text(cUI, s).FontSize(theme.FontSmall).LineHeight(1.5).TextColor(pal.Text).FillWidth()
		}
	})
}

func renderDeepProsCons(cUI *ui.Context, pal theme.Palette, key string, m deepNode) {
	type side struct {
		name string
		pts  []string
		fg   ui.Color
		mark string
	}
	sides := []side{
		{deepStr(m, "prosTitle", "aName"), deepStrs(m, "pros", "a", "good"), pal.Success, "+"},
		{deepStr(m, "consTitle", "bName"), deepStrs(m, "cons", "b", "bad"), pal.Danger, "−"},
	}
	if sides[0].name == "" {
		sides[0].name = theme.Tr("优势", "Pros")
	}
	if sides[1].name == "" {
		sides[1].name = theme.Tr("顾虑", "Cons")
	}
	ui.Row(cUI.Key(key+"-pc")).FillWidth().Wrap().Gap(theme.Space8).Children(func() {
		for si, s := range sides {
			if len(s.pts) == 0 {
				continue
			}
			ui.Column(cUI.Key(fmt.Sprintf("%s-pc%d", key, si))).Basis(200).Grow(1).MinWidth(0).Gap(theme.Space6).
				Padding(theme.Space10, theme.Space12).Background(pal.CodeBlockBG).Radius(theme.RadiusMD).Children(func() {
				ui.Text(cUI, s.name).FontSize(theme.FontMeta).FontWeight(theme.WeightTitle).TextColor(s.fg).FillWidth()
				for pi, pt := range s.pts {
					ui.Row(cUI.Key(fmt.Sprintf("%s-pc%d-%d", key, si, pi))).FillWidth().Gap(theme.Space6).AlignItems(ui.Start).Children(func() {
						ui.Text(cUI, s.mark).FontSize(theme.FontSmall).FontWeight(theme.WeightTitle).TextColor(s.fg).Shrink(0)
						ui.Text(cUI, pt).FontSize(theme.FontSmall).LineHeight(1.45).TextColor(pal.Text).Grow(1).MinWidth(0)
					})
				}
			})
		}
	})
}

func renderDeepGroup(cUI *ui.Context, pal theme.Palette, key string, m deepNode, depth int) {
	kids := deepNodes(deepArr(m, "blocks", "items"))
	basis := float32(220)
	if c, ok := deepNum(m, "cols"); ok && c >= 3 {
		basis = 150
	}
	ui.Row(cUI.Key(key+"-grp")).FillWidth().Wrap().Gap(theme.Space12).AlignItems(ui.Start).Children(func() {
		for i, k := range kids {
			ui.Column(cUI.Key(fmt.Sprintf("%s-g%d", key, i))).Basis(basis).Grow(1).MinWidth(0).Children(func() {
				renderDeepBlock(cUI, pal, fmt.Sprintf("%s-g%d", key, i), k, depth+1)
			})
		}
	})
}

func renderDeepFold(cUI *ui.Context, pal theme.Palette, key string, m deepNode, depth int) {
	kids := deepNodes(deepArr(m, "blocks", "items"))
	title := deepStr(m, "title", "l", "label")
	if title == "" {
		title = theme.Tr("展开详情", "Details")
	}
	wrap := ui.Column(cUI.Key(key+"-fold")).FillWidth().Gap(theme.Space8)
	open := ui.Local(wrap, deepFoldKey{}, func() bool { return deepBool(m, "open") })
	if open == nil {
		return
	}
	wrap.Children(func() {
		head := ui.ButtonBase(cUI.Key(key+"-fold-head")).FillWidth().Height(theme.HeightCompact).PaddingX(theme.Space6).
			Gap(theme.Space6).AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(title)
		head.Expanded(*open)
		hovered := head.Hovered()
		bg := ui.Transparent
		if hovered {
			bg = pal.Hover
		}
		bui.AnimateSurfaceColors(head, bg, ui.Transparent, true)
		head.Children(func() {
			chev := icons.ChevronRight
			if *open {
				chev = icons.ChevronDown
			}
			icons.DrawAnimated(cUI, chev, theme.IconInline, pal.DuoIconColors(pal.TextSecondary), hovered)
			ui.Text(cUI, title).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).TextColor(pal.Text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if s := deepStr(m, "sub"); s != "" {
				ui.Text(cUI, s).FontSize(theme.FontMicro).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
			}
		})
		if head.Clicked() {
			*open = !*open
		}
		if *open {
			ui.Column(cUI.Key(key+"-fold-body")).FillWidth().Gap(theme.Space12).Padding(0, 0, 0, theme.Space20).Children(func() {
				for i, k := range kids {
					renderDeepBlock(cUI, pal, fmt.Sprintf("%s-f%d", key, i), k, depth+1)
				}
			})
		}
	})
}

func renderDeepTabs(cUI *ui.Context, pal theme.Palette, key string, m deepNode, depth int) {
	tabs := deepNodes(deepArr(m, "items", "tabs"))
	if len(tabs) == 0 {
		return
	}
	wrap := ui.Column(cUI.Key(key+"-tabs")).FillWidth().Gap(theme.Space10)
	sel := ui.Local(wrap, deepTabsKey{}, func() int { return 0 })
	if sel == nil {
		return
	}
	if *sel < 0 || *sel >= len(tabs) {
		*sel = 0
	}
	wrap.Children(func() {
		ui.Row(cUI.Key(key+"-tabbar")).FillWidth().Wrap().Gap(theme.Space4).Padding(theme.Space2).
			Background(pal.CodeBlockBG).Radius(theme.RadiusMD).Children(func() {
			for i, tb := range tabs {
				label := deepStr(tb, "label", "l", "name", "title")
				if label == "" {
					label = strconv.Itoa(i + 1)
				}
				active := i == *sel
				btn := ui.ButtonBase(cUI.Key(fmt.Sprintf("%s-tab%d", key, i))).Height(theme.HeightMicro).PaddingX(theme.Space10).
					AlignItems(ui.Center).Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(label)
				btn.Selected(active)
				bg := ui.Transparent
				if active {
					bg = pal.Paper
				} else if btn.Hovered() {
					bg = pal.Hover
				}
				bui.AnimateSurfaceColors(btn, bg, ui.Transparent, true)
				fg := pal.TextSecondary
				if active {
					fg = pal.Text
				}
				btn.Children(func() {
					ui.Text(cUI, label).FontSize(theme.FontMeta).FontWeight(theme.WeightControl).TextColor(fg).SingleLine()
				})
				if btn.Clicked() {
					*sel = i
				}
			}
		})
		cur := tabs[*sel]
		kids := deepNodes(deepArr(cur, "blocks"))
		ui.Column(cUI.Key(fmt.Sprintf("%s-tabbody%d", key, *sel))).FillWidth().Gap(theme.Space12).Children(func() {
			if len(kids) == 0 {
				renderDeepText(cUI, pal, fmt.Sprintf("%s-tt%d", key, *sel), cur)
			}
			for i, k := range kids {
				renderDeepBlock(cUI, pal, fmt.Sprintf("%s-t%d-%d", key, *sel, i), k, depth+1)
			}
		})
	})
}
