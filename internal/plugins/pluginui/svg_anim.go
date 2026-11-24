package pluginui

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/colornames"
)

const (
	svgAnimMaxBytes   = 256 << 10
	svgAnimMaxDim     = 4096
	svgAnimMaxRender  = 1600
	svgAnimMaxPixels  = 2_000_000
	svgAnimFPS        = 15
	svgAnimMaxCycle   = 12 * time.Second
	svgAnimMaxSamples = 360
	svgAnimMaxStops   = 2048
)

type svgAnimationDecoder struct {
	root     *svgXMLNode
	bindings []*svgAnimBinding
	frames   []svgAnimFrame
	totalDur time.Duration
	renderW  int
	renderH  int
	idx      int
	buf      bytes.Buffer
}

type svgAnimFrame struct {
	overrides []svgElemOverride
	dur       time.Duration
}

type svgElemOverride struct {
	styleExtra    string
	transformAttr string
}

type svgXMLAttr struct {
	name  string
	value string
}

type svgXMLNode struct {
	name     string
	text     string
	attrs    []svgXMLAttr
	kids     []*svgXMLNode
	styleIdx int
	transIdx int
}

type svgKeyframeStop struct {
	pct   float64
	props map[string]string
}

type svgKeyframes struct {
	stops []svgKeyframeStop
}

type svgTimingKind uint8

const (
	timingEase svgTimingKind = iota
	timingLinear
	timingEaseIn
	timingEaseOut
	timingEaseInOut
	timingCubicBezier
	timingSteps
)

type svgStepPos uint8

const (
	stepEnd svgStepPos = iota
	stepStart
	stepJumpNone
	stepJumpBoth
)

type svgTimingFunc struct {
	kind       svgTimingKind
	x1, y1     float64
	x2, y2     float64
	steps      int
	stepPos    svgStepPos
	configured bool
}

type svgCSSAnim struct {
	name        string
	dur         float64
	delay       float64
	timing      svgTimingFunc
	infinite    bool
	hasInfinite bool
}

type svgCSSRule struct {
	anim        svgCSSAnim
	hasOrigin   bool
	originX     float64
	originY     float64
	staticProps map[string]string
}

type svgAnimBinding struct {
	node         *svgXMLNode
	hasBaseStyle bool
	baseStyle    string
	hasBaseTrans bool
	baseTrans    string
	hasOrigin    bool
	originX      float64
	originY      float64
	anim         svgCSSAnim
	keyframes    *svgKeyframes
	baseProps    map[string]string
}

// newSVGAnimationDecoder builds a frame-streaming VideoDecoder for SVGs that
// animate via CSS @keyframes. Static or unsupported SVGs return ok=false with
// minimal overhead so callers keep the zero-copy vector path.
func newSVGAnimationDecoder(data []byte) (ui.VideoDecoder, int64, bool) {
	if len(data) == 0 || len(data) > svgAnimMaxBytes {
		return nil, 0, false
	}
	if !containsFoldASCII(data, "@keyframes") && !containsFoldASCII(data, "@-webkit-keyframes") {
		return nil, 0, false
	}

	baseSVG, err := ui.ParseSVG(data)
	if err != nil {
		return nil, 0, false
	}
	w, h := baseSVG.Size()
	if w <= 0 || h <= 0 || w > svgAnimMaxDim || h > svgAnimMaxDim {
		return nil, 0, false
	}

	root, styleText, err := parseSVGXML(data)
	if err != nil || root == nil || strings.TrimSpace(styleText) == "" {
		return nil, 0, false
	}

	keyframes, rulesByClass, rulesByID := parseSVGAnimationCSS(styleText)
	if len(keyframes) == 0 || (len(rulesByClass) == 0 && len(rulesByID) == 0) {
		return nil, 0, false
	}

	var bindings []*svgAnimBinding
	collectSVGAnimBindings(root, keyframes, rulesByClass, rulesByID, &bindings)
	if len(bindings) == 0 {
		return nil, 0, false
	}

	// Each sample scans every bound animation's stops. Count shared keyframes
	// once per binding to bound the work before allocating the timeline.
	stopCount := 0
	for _, binding := range bindings {
		stopCount += len(binding.keyframes.stops)
		if stopCount > svgAnimMaxStops {
			return nil, 0, false
		}
	}

	frames, totalDur := sampleSVGAnimationTimeline(bindings)
	if len(frames) <= 1 || totalDur <= 0 {
		return nil, 0, false
	}

	renderW, renderH := computeSVGRenderSize(w, h)
	pixels := 4 * int64(renderW) * int64(renderH)
	return &svgAnimationDecoder{
		root:     root,
		bindings: bindings,
		frames:   frames,
		totalDur: totalDur,
		renderW:  renderW,
		renderH:  renderH,
	}, pixels, true
}

func computeSVGRenderSize(w, h float32) (int, int) {
	fw, fh := float64(w), float64(h)
	scale := 1.0
	if maxSide := math.Max(fw, fh); maxSide > svgAnimMaxRender {
		scale = min(scale, float64(svgAnimMaxRender)/maxSide)
	}
	if area := fw * fh * scale * scale; area > svgAnimMaxPixels {
		scale *= math.Sqrt(float64(svgAnimMaxPixels) / area)
	}
	rw := max(1, int(math.Round(fw*scale)))
	rh := max(1, int(math.Round(fh*scale)))
	return rw, rh
}

func (d *svgAnimationDecoder) Next() (ui.VideoFrame, error) {
	if d.idx >= len(d.frames) {
		return ui.VideoFrame{}, io.EOF
	}
	fr := d.frames[d.idx]
	for i, b := range d.bindings {
		b.applyOverride(fr.overrides[i])
	}
	d.buf.Reset()
	writeSVGXML(&d.buf, d.root)
	parsed, err := ui.ParseSVG(d.buf.Bytes())
	if err != nil {
		return ui.VideoFrame{}, err
	}
	img := parsed.WithDark(theme.IsDark()).Image(d.renderW, d.renderH, nil)
	d.idx++
	return ui.VideoFrame{Image: img, Duration: fr.dur}, nil
}

func (d *svgAnimationDecoder) Rewind() error {
	d.idx = 0
	return nil
}

func (d *svgAnimationDecoder) Duration() time.Duration {
	return d.totalDur
}

func (b *svgAnimBinding) applyOverride(ov svgElemOverride) {
	if ov.styleExtra == "" {
		if b.hasBaseStyle {
			b.node.attrs[b.node.styleIdx].value = b.baseStyle
		} else if b.node.styleIdx >= 0 {
			b.node.attrs[b.node.styleIdx].value = ""
		}
	} else {
		val := ov.styleExtra
		if b.hasBaseStyle && b.baseStyle != "" {
			val = b.baseStyle + ";" + ov.styleExtra
		}
		if b.node.styleIdx >= 0 {
			b.node.attrs[b.node.styleIdx].value = val
		} else {
			b.node.styleIdx = len(b.node.attrs)
			b.node.attrs = append(b.node.attrs, svgXMLAttr{name: "style", value: val})
		}
	}

	var trans string
	if ov.transformAttr == "" {
		if b.hasBaseTrans {
			trans = b.baseTrans
		}
	} else if b.hasBaseTrans && b.baseTrans != "" {
		trans = b.baseTrans + " " + ov.transformAttr
	} else {
		trans = ov.transformAttr
	}
	if b.node.transIdx >= 0 {
		b.node.attrs[b.node.transIdx].value = trans
	} else if trans != "" {
		b.node.transIdx = len(b.node.attrs)
		b.node.attrs = append(b.node.attrs, svgXMLAttr{name: "transform", value: trans})
	}
}

func containsFoldASCII(haystack []byte, needle string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	n0 := lowerASCII(needle[0])
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		if lowerASCII(haystack[i]) != n0 {
			continue
		}
		matched := true
		for j := 1; j < len(needle); j++ {
			if lowerASCII(haystack[i+j]) != lowerASCII(needle[j]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func svgCharsetReader(label string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		b.Grow(len(data))
		for _, c := range data {
			b.WriteRune(rune(c))
		}
		return strings.NewReader(b.String()), nil
	}
	return r, nil
}

func parseSVGXML(data []byte) (*svgXMLNode, string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = svgCharsetReader

	var root *svgXMLNode
	var stack []*svgXMLNode
	var styleBuf strings.Builder
	hasXLink := false
	hasRootXLinkNS := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &svgXMLNode{
				name:     t.Name.Local,
				styleIdx: -1,
				transIdx: -1,
			}
			for _, a := range t.Attr {
				attrName, ok := formatXMLAttrName(a.Name)
				if !ok {
					continue
				}
				if attrName == "xlink:href" {
					hasXLink = true
				}
				if attrName == "xmlns:xlink" {
					hasRootXLinkNS = true
				}
				if attrName == "style" {
					node.styleIdx = len(node.attrs)
				} else if attrName == "transform" {
					node.transIdx = len(node.attrs)
				}
				node.attrs = append(node.attrs, svgXMLAttr{name: attrName, value: a.Value})
			}
			if len(stack) == 0 {
				if node.name != "svg" {
					return nil, "", fmt.Errorf("svg: root is %q", node.name)
				}
				hasXMLNS := false
				for _, a := range node.attrs {
					if a.name == "xmlns" {
						hasXMLNS = true
						break
					}
				}
				if !hasXMLNS {
					node.attrs = append(node.attrs, svgXMLAttr{name: "xmlns", value: "http://www.w3.org/2000/svg"})
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			parent := stack[len(stack)-1]
			s := string(t)
			if parent.name == "style" {
				styleType := strings.ToLower(strings.TrimSpace(parent.attrValue("type")))
				if styleType == "" || styleType == "text/css" {
					styleBuf.WriteString(s)
					styleBuf.WriteByte('\n')
				}
			}
			parent.kids = append(parent.kids, &svgXMLNode{text: s, styleIdx: -1, transIdx: -1})
		}
	}
	if root == nil {
		return nil, "", fmt.Errorf("svg: empty document")
	}
	if hasXLink && !hasRootXLinkNS {
		root.attrs = append(root.attrs, svgXMLAttr{name: "xmlns:xlink", value: "http://www.w3.org/1999/xlink"})
	}
	return root, styleBuf.String(), nil
}

func formatXMLAttrName(n xml.Name) (string, bool) {
	switch n.Space {
	case "":
		return n.Local, true
	case "xmlns":
		return "xmlns:" + n.Local, true
	case "xml", "http://www.w3.org/XML/1998/namespace":
		return "xml:" + n.Local, true
	case "xlink", "http://www.w3.org/1999/xlink":
		return "xlink:" + n.Local, true
	default:
		return "", false
	}
}

func (n *svgXMLNode) attrValue(name string) string {
	for _, a := range n.attrs {
		if a.name == name {
			return a.value
		}
	}
	return ""
}

func writeSVGXML(buf *bytes.Buffer, n *svgXMLNode) {
	if n.name == "" {
		_ = xml.EscapeText(buf, []byte(n.text))
		return
	}
	buf.WriteByte('<')
	buf.WriteString(n.name)
	for _, a := range n.attrs {
		if (a.name == "style" || a.name == "transform") && a.value == "" {
			continue
		}
		buf.WriteByte(' ')
		buf.WriteString(a.name)
		buf.WriteString(`="`)
		_ = xml.EscapeText(buf, []byte(a.value))
		buf.WriteByte('"')
	}
	if len(n.kids) == 0 {
		buf.WriteString("/>")
		return
	}
	buf.WriteByte('>')
	for _, k := range n.kids {
		writeSVGXML(buf, k)
	}
	buf.WriteString("</")
	buf.WriteString(n.name)
	buf.WriteByte('>')
}

func stripCSSComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + " " + s[i+2+j+2:]
	}
	s = strings.ReplaceAll(s, "<!--", " ")
	s = strings.ReplaceAll(s, "-->", " ")
	return s
}

func cssBlockEnd(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

func parseSVGAnimationCSS(raw string) (map[string]*svgKeyframes, map[string]*svgCSSRule, map[string]*svgCSSRule) {
	s := stripCSSComments(raw)
	keyframes := make(map[string]*svgKeyframes)
	byClass := make(map[string]*svgCSSRule)
	byID := make(map[string]*svgCSSRule)

	for {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		if s[0] == '@' {
			sep := strings.IndexAny(s, ";{")
			if sep < 0 {
				break
			}
			hdr := strings.TrimSpace(s[1:sep])
			if s[sep] == ';' {
				s = s[sep+1:]
				continue
			}
			end := cssBlockEnd(s, sep)
			body := s[sep+1 : max(sep+1, end-1)]
			fields := strings.Fields(hdr)
			if len(fields) >= 2 {
				atName := strings.ToLower(fields[0])
				if atName == "keyframes" || atName == "-webkit-keyframes" {
					if kf := parseKeyframesBody(body); kf != nil && len(kf.stops) > 0 {
						keyframes[fields[1]] = kf
					}
				}
			}
			s = s[end:]
			continue
		}

		open := strings.IndexByte(s, '{')
		if open < 0 {
			break
		}
		end := cssBlockEnd(s, open)
		selText := strings.TrimSpace(s[:open])
		body := s[open+1 : max(open+1, end-1)]
		s = s[end:]

		ruleData := parseCSSRuleBody(body)
		if ruleData == nil {
			continue
		}
		for _, sel := range strings.Split(selText, ",") {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			if cls, ok := parseSimpleSelector(sel, '.'); ok {
				byClass[cls] = mergeCSSRule(byClass[cls], ruleData)
			} else if id, ok := parseSimpleSelector(sel, '#'); ok {
				byID[id] = mergeCSSRule(byID[id], ruleData)
			}
		}
	}
	return keyframes, byClass, byID
}

func parseSimpleSelector(sel string, prefix byte) (string, bool) {
	if len(sel) < 2 || sel[0] != prefix {
		return "", false
	}
	name := sel[1:]
	for i := 0; i < len(name); i++ {
		c := name[i]
		isIdent := c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
		if !isIdent {
			return "", false
		}
	}
	return name, true
}

func mergeCSSRule(dst, src *svgCSSRule) *svgCSSRule {
	if dst == nil {
		cp := *src
		if len(src.staticProps) > 0 {
			cp.staticProps = make(map[string]string, len(src.staticProps))
			for k, v := range src.staticProps {
				cp.staticProps[k] = v
			}
		}
		return &cp
	}
	if src.anim.name != "" {
		dst.anim.name = src.anim.name
	}
	if src.anim.dur > 0 {
		dst.anim.dur = src.anim.dur
	}
	if src.anim.delay != 0 {
		dst.anim.delay = src.anim.delay
	}
	if src.anim.timing.configured {
		dst.anim.timing = src.anim.timing
	}
	if src.anim.hasInfinite {
		dst.anim.infinite = src.anim.infinite
		dst.anim.hasInfinite = true
	}
	if src.hasOrigin {
		dst.hasOrigin = true
		dst.originX = src.originX
		dst.originY = src.originY
	}
	if len(src.staticProps) > 0 {
		if dst.staticProps == nil {
			dst.staticProps = make(map[string]string, len(src.staticProps))
		}
		for k, v := range src.staticProps {
			dst.staticProps[k] = v
		}
	}
	return dst
}

func parseKeyframesBody(body string) *svgKeyframes {
	byPct := make(map[float64]map[string]string)
	s := body
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		open := strings.IndexByte(s, '{')
		if open < 0 {
			break
		}
		end := cssBlockEnd(s, open)
		selList := s[:open]
		declBody := s[open+1 : max(open+1, end-1)]
		s = s[end:]

		props := parseKeyframeDecls(declBody)
		if len(props) == 0 {
			continue
		}
		for _, tok := range strings.Split(selList, ",") {
			pct, ok := parseKeyframeOffset(tok)
			if !ok {
				continue
			}
			dst := byPct[pct]
			if dst == nil {
				dst = make(map[string]string, len(props))
				byPct[pct] = dst
			}
			for k, v := range props {
				dst[k] = v
			}
		}
	}
	if len(byPct) == 0 {
		return nil
	}
	stops := make([]svgKeyframeStop, 0, len(byPct))
	for pct, props := range byPct {
		stops = append(stops, svgKeyframeStop{pct: pct, props: props})
	}
	slices.SortFunc(stops, func(a, b svgKeyframeStop) int {
		if a.pct < b.pct {
			return -1
		}
		if a.pct > b.pct {
			return 1
		}
		return 0
	})
	return &svgKeyframes{stops: stops}
}

func parseKeyframeOffset(tok string) (float64, bool) {
	t := strings.ToLower(strings.TrimSpace(tok))
	switch t {
	case "from":
		return 0, true
	case "to":
		return 1, true
	}
	if !strings.HasSuffix(t, "%") {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(t[:len(t)-1]), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return math.Max(0, math.Min(1, v/100)), true
}

func splitCSSDecls(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func parseKeyframeDecls(s string) map[string]string {
	var props map[string]string
	for _, part := range splitCSSDecls(s) {
		name, val, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		val = stripImportant(strings.TrimSpace(val))
		if val == "" {
			continue
		}
		switch name {
		case "opacity", "transform", "fill", "stroke", "display":
			if props == nil {
				props = make(map[string]string, 4)
			}
			props[name] = val
		}
	}
	return props
}

func stripImportant(val string) string {
	if i := strings.Index(strings.ToLower(val), "!important"); i >= 0 {
		return strings.TrimSpace(val[:i])
	}
	return val
}

func parseCSSRuleBody(s string) *svgCSSRule {
	var r svgCSSRule
	relevant := false
	for _, part := range splitCSSDecls(s) {
		name, val, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		val = stripImportant(strings.TrimSpace(val))
		if val == "" {
			continue
		}
		switch name {
		case "animation", "-webkit-animation":
			if anim, ok := parseAnimationShorthand(val); ok {
				r.anim = anim
				relevant = true
			}
		case "animation-name":
			r.anim.name = firstCommaItem(val)
			relevant = true
		case "animation-duration":
			if d, ok := parseCSSTime(firstCommaItem(val)); ok && d > 0 {
				r.anim.dur = d
				relevant = true
			}
		case "animation-delay":
			if d, ok := parseCSSTime(firstCommaItem(val)); ok {
				r.anim.delay = d
				relevant = true
			}
		case "animation-timing-function":
			if tf, ok := parseCSSTimingFunc(firstCommaItem(val)); ok {
				r.anim.timing = tf
				relevant = true
			}
		case "animation-iteration-count":
			r.anim.hasInfinite = true
			r.anim.infinite = strings.EqualFold(firstCommaItem(val), "infinite")
			relevant = true
		case "transform-origin":
			if ox, oy, ok := parseTransformOrigin(val); ok {
				r.hasOrigin = true
				r.originX, r.originY = ox, oy
				relevant = true
			}
		case "opacity", "fill", "stroke", "display":
			if r.staticProps == nil {
				r.staticProps = make(map[string]string, 2)
			}
			r.staticProps[name] = val
		}
	}
	if !relevant {
		return nil
	}
	return &r
}

func firstCommaItem(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

func splitCSSTokens(s string) []string {
	var out []string
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' {
			if start < 0 {
				start = i
			}
			depth++
			continue
		}
		if c == ')' {
			if depth > 0 {
				depth--
			}
			continue
		}
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
		if isSpace && depth == 0 {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func parseAnimationShorthand(val string) (svgCSSAnim, bool) {
	first := firstCommaItem(val)
	tokens := splitCSSTokens(first)
	if len(tokens) == 0 {
		return svgCSSAnim{}, false
	}

	var anim svgCSSAnim
	timeCount := 0
	for _, tok := range tokens {
		low := strings.ToLower(tok)
		if low == "infinite" {
			anim.infinite = true
			anim.hasInfinite = true
			continue
		}
		if d, ok := parseCSSTime(tok); ok {
			if timeCount == 0 {
				anim.dur = d
			} else if timeCount == 1 {
				anim.delay = d
			}
			timeCount++
			continue
		}
		if tf, ok := parseCSSTimingFunc(tok); ok {
			anim.timing = tf
			continue
		}
		switch low {
		case "normal", "reverse", "alternate", "alternate-reverse",
			"none", "forwards", "backwards", "both",
			"running", "paused":
			if low == "none" && anim.name == "" {
				return svgCSSAnim{}, false
			}
			continue
		}
		if anim.name == "" && !strings.Contains(tok, "(") {
			anim.name = tok
		}
	}
	if anim.name == "" || anim.dur <= 0 || !anim.infinite {
		return svgCSSAnim{}, false
	}
	return anim, true
}

func parseCSSTime(tok string) (float64, bool) {
	const maxCSSTimeSec = 3600.0
	low := strings.ToLower(strings.TrimSpace(tok))
	if strings.HasSuffix(low, "ms") {
		v, err := strconv.ParseFloat(low[:len(low)-2], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		sec := v / 1000
		if math.Abs(sec) > maxCSSTimeSec {
			return 0, false
		}
		return sec, true
	}
	if strings.HasSuffix(low, "s") {
		v, err := strconv.ParseFloat(low[:len(low)-1], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxCSSTimeSec {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func parseCSSTimingFunc(tok string) (svgTimingFunc, bool) {
	low := strings.ToLower(strings.TrimSpace(tok))
	switch low {
	case "linear":
		return svgTimingFunc{kind: timingLinear, configured: true}, true
	case "ease":
		return svgTimingFunc{kind: timingEase, configured: true}, true
	case "ease-in":
		return svgTimingFunc{kind: timingEaseIn, configured: true}, true
	case "ease-out":
		return svgTimingFunc{kind: timingEaseOut, configured: true}, true
	case "ease-in-out":
		return svgTimingFunc{kind: timingEaseInOut, configured: true}, true
	case "step-start":
		return svgTimingFunc{kind: timingSteps, steps: 1, stepPos: stepStart, configured: true}, true
	case "step-end":
		return svgTimingFunc{kind: timingSteps, steps: 1, stepPos: stepEnd, configured: true}, true
	}
	if strings.HasPrefix(low, "cubic-bezier(") && strings.HasSuffix(low, ")") {
		inner := low[len("cubic-bezier(") : len(low)-1]
		parts := strings.Split(inner, ",")
		if len(parts) != 4 {
			return svgTimingFunc{}, false
		}
		var coords [4]float64
		for i, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return svgTimingFunc{}, false
			}
			coords[i] = v
		}
		return svgTimingFunc{
			kind:       timingCubicBezier,
			x1:         math.Max(0, math.Min(1, coords[0])),
			y1:         coords[1],
			x2:         math.Max(0, math.Min(1, coords[2])),
			y2:         coords[3],
			configured: true,
		}, true
	}
	if strings.HasPrefix(low, "steps(") && strings.HasSuffix(low, ")") {
		inner := low[len("steps(") : len(low)-1]
		parts := strings.Split(inner, ",")
		if len(parts) < 1 || len(parts) > 2 {
			return svgTimingFunc{}, false
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || n < 1 {
			return svgTimingFunc{}, false
		}
		pos := stepEnd
		if len(parts) == 2 {
			switch strings.TrimSpace(parts[1]) {
			case "start", "jump-start":
				pos = stepStart
			case "end", "jump-end":
				pos = stepEnd
			case "jump-none":
				pos = stepJumpNone
			case "jump-both":
				pos = stepJumpBoth
			default:
				return svgTimingFunc{}, false
			}
		}
		return svgTimingFunc{kind: timingSteps, steps: n, stepPos: pos, configured: true}, true
	}
	return svgTimingFunc{}, false
}

func (tf svgTimingFunc) eval(t float64) float64 {
	if t <= 0 {
		if tf.kind == timingSteps && (tf.stepPos == stepJumpBoth) {
			return 1.0 / float64(tf.steps+1)
		}
		return 0
	}
	if t >= 1 {
		return 1
	}
	switch tf.kind {
	case timingLinear:
		return t
	case timingEase:
		return evalCubicBezier(0.25, 0.1, 0.25, 1.0, t)
	case timingEaseIn:
		return evalCubicBezier(0.42, 0, 1.0, 1.0, t)
	case timingEaseOut:
		return evalCubicBezier(0, 0, 0.58, 1.0, t)
	case timingEaseInOut:
		return evalCubicBezier(0.42, 0, 0.58, 1.0, t)
	case timingCubicBezier:
		return evalCubicBezier(tf.x1, tf.y1, tf.x2, tf.y2, t)
	case timingSteps:
		n := float64(max(1, tf.steps))
		switch tf.stepPos {
		case stepStart:
			return math.Min(1, math.Ceil(t*n)/n)
		case stepJumpNone:
			if tf.steps <= 1 {
				return 0
			}
			return math.Min(1, math.Floor(t*n)/float64(tf.steps-1))
		case stepJumpBoth:
			return math.Min(1, (math.Floor(t*n)+1)/float64(tf.steps+1))
		default:
			return math.Floor(t*n) / n
		}
	default:
		return evalCubicBezier(0.25, 0.1, 0.25, 1.0, t)
	}
}

func evalCubicBezier(x1, y1, x2, y2, x float64) float64 {
	sample := func(a, b, u float64) float64 {
		inv := 1 - u
		return 3*inv*inv*u*a + 3*inv*u*u*b + u*u*u
	}
	lo, hi := 0.0, 1.0
	for range 16 {
		mid := 0.5 * (lo + hi)
		if sample(x1, x2, mid) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return sample(y1, y2, 0.5*(lo+hi))
}

func parseTransformOrigin(val string) (float64, float64, bool) {
	fields := strings.Fields(strings.TrimSpace(val))
	if len(fields) == 0 || len(fields) > 2 {
		return 0, 0, false
	}
	x, ok := parseUnitlessPx(fields[0])
	if !ok {
		return 0, 0, false
	}
	y := 0.0
	if len(fields) == 2 {
		y, ok = parseUnitlessPx(fields[1])
		if !ok {
			return 0, 0, false
		}
	}
	return x, y, true
}

func parseUnitlessPx(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	low := strings.ToLower(t)
	if strings.HasSuffix(low, "px") {
		t = strings.TrimSpace(t[:len(t)-2])
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func collectSVGAnimBindings(
	n *svgXMLNode,
	keyframes map[string]*svgKeyframes,
	byClass map[string]*svgCSSRule,
	byID map[string]*svgCSSRule,
	out *[]*svgAnimBinding,
) {
	if n.name != "" {
		var combined *svgCSSRule
		if clsAttr := n.attrValue("class"); clsAttr != "" {
			for _, cls := range strings.Fields(clsAttr) {
				if rule, ok := byClass[cls]; ok {
					combined = mergeCSSRule(combined, rule)
				}
			}
		}
		if idAttr := n.attrValue("id"); idAttr != "" {
			if rule, ok := byID[idAttr]; ok {
				combined = mergeCSSRule(combined, rule)
			}
		}
		if inlineStyle := n.attrValue("style"); inlineStyle != "" {
			if inlineRule := parseCSSRuleBody(inlineStyle); inlineRule != nil {
				combined = mergeCSSRule(combined, inlineRule)
			}
		}
		if combined != nil && combined.anim.infinite && combined.anim.dur > 0 && combined.anim.name != "" {
			if kf, ok := keyframes[combined.anim.name]; ok && len(kf.stops) > 0 {
				baseProps := map[string]string{
					"opacity": "1",
					"display": "inline",
				}
				for _, prop := range []string{"opacity", "fill", "stroke", "display"} {
					if v := strings.TrimSpace(n.attrValue(prop)); v != "" {
						baseProps[prop] = v
					}
					if v, ok := combined.staticProps[prop]; ok && v != "" {
						baseProps[prop] = v
					}
				}
				b := &svgAnimBinding{
					node:         n,
					hasBaseStyle: n.styleIdx >= 0,
					baseStyle:    n.attrValue("style"),
					hasBaseTrans: n.transIdx >= 0,
					baseTrans:    n.attrValue("transform"),
					hasOrigin:    combined.hasOrigin,
					originX:      combined.originX,
					originY:      combined.originY,
					anim:         combined.anim,
					keyframes:    kf,
					baseProps:    baseProps,
				}
				*out = append(*out, b)
			}
		}
	}
	for _, k := range n.kids {
		collectSVGAnimBindings(k, keyframes, byClass, byID, out)
	}
}

func sampleSVGAnimationTimeline(bindings []*svgAnimBinding) ([]svgAnimFrame, time.Duration) {
	totalDur := computeTimelineCycleDuration(bindings)
	if totalDur <= 0 {
		return nil, 0
	}
	n := max(2, min(svgAnimMaxSamples, int(math.Round(totalDur.Seconds()*svgAnimFPS))))
	dt := totalDur / time.Duration(n)

	frames := make([]svgAnimFrame, 0, n)
	var prevSig string
	for i := range n {
		sampleDur := dt
		if i == n-1 {
			sampleDur = totalDur - dt*time.Duration(n-1)
		}
		tSec := (time.Duration(i) * dt).Seconds()
		overrides := make([]svgElemOverride, len(bindings))
		var sigBuf strings.Builder
		for bi, b := range bindings {
			ov := b.evalAt(tSec)
			overrides[bi] = ov
			sigBuf.WriteString(ov.styleExtra)
			sigBuf.WriteByte('|')
			sigBuf.WriteString(ov.transformAttr)
			sigBuf.WriteByte('\n')
		}
		sig := sigBuf.String()
		if len(frames) > 0 && sig == prevSig {
			frames[len(frames)-1].dur += sampleDur
			continue
		}
		prevSig = sig
		frames = append(frames, svgAnimFrame{overrides: overrides, dur: sampleDur})
	}
	return frames, totalDur
}

func computeTimelineCycleDuration(bindings []*svgAnimBinding) time.Duration {
	maxMs := int64(0)
	lcmMs := int64(1)
	lcmOverflow := false
	for _, b := range bindings {
		ms := max(10, int64(math.Round(b.anim.dur*1000)))
		if ms > maxMs {
			maxMs = ms
		}
		if !lcmOverflow {
			g := gcdInt64(lcmMs, ms)
			next := lcmMs / g
			if next > svgAnimMaxCycle.Milliseconds()/ms {
				lcmOverflow = true
			} else {
				lcmMs = next * ms
			}
		}
	}
	if !lcmOverflow && lcmMs >= maxMs && lcmMs <= svgAnimMaxCycle.Milliseconds() {
		return time.Duration(lcmMs) * time.Millisecond
	}
	return min(time.Duration(maxMs)*time.Millisecond, svgAnimMaxCycle)
}

func gcdInt64(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return max(1, a)
}

func (b *svgAnimBinding) evalAt(tSec float64) svgElemOverride {
	elapsed := tSec - b.anim.delay
	phase := math.Mod(elapsed, b.anim.dur)
	if phase < 0 {
		phase += b.anim.dur
	}
	p := phase / b.anim.dur

	var styleParts []string
	var transformAttr string

	for _, prop := range []string{"opacity", "fill", "stroke", "display", "transform"} {
		v0, v1, u, ok := b.sampleProp(prop, p)
		if !ok {
			continue
		}
		switch prop {
		case "opacity":
			if val := interpolateOpacity(v0, v1, u); val != "" {
				styleParts = append(styleParts, "opacity:"+val)
			}
		case "fill":
			if val := interpolateColor(v0, v1, u); val != "" {
				styleParts = append(styleParts, "fill:"+val)
			}
		case "stroke":
			if val := interpolateColor(v0, v1, u); val != "" {
				styleParts = append(styleParts, "stroke:"+val)
			}
		case "display":
			val := v0
			if u >= 0.5 {
				val = v1
			}
			if val != "" {
				styleParts = append(styleParts, "display:"+val)
			}
		case "transform":
			transformAttr = interpolateTransform(v0, v1, u, b.hasOrigin, b.originX, b.originY)
		}
	}

	return svgElemOverride{
		styleExtra:    strings.Join(styleParts, ";"),
		transformAttr: transformAttr,
	}
}

func (b *svgAnimBinding) sampleProp(prop string, p float64) (string, string, float64, bool) {
	type propStop struct {
		pct float64
		val string
	}
	var stops []propStop
	for _, st := range b.keyframes.stops {
		if val, ok := st.props[prop]; ok {
			stops = append(stops, propStop{pct: st.pct, val: val})
		}
	}
	if len(stops) == 0 {
		return "", "", 0, false
	}
	if stops[0].pct > 0 {
		def := b.baseProps[prop]
		if def == "" {
			if prop == "transform" {
				def = "none"
			} else {
				def = stops[0].val
			}
		}
		stops = append([]propStop{{pct: 0, val: def}}, stops...)
	}
	if stops[len(stops)-1].pct < 1 {
		def := b.baseProps[prop]
		if def == "" {
			if prop == "transform" {
				def = "none"
			} else {
				def = stops[len(stops)-1].val
			}
		}
		stops = append(stops, propStop{pct: 1, val: def})
	}
	if len(stops) == 1 {
		return stops[0].val, stops[0].val, 0, true
	}
	for i := 0; i < len(stops)-1; i++ {
		s0, s1 := stops[i], stops[i+1]
		if p >= s0.pct && (p <= s1.pct || i == len(stops)-2) {
			span := s1.pct - s0.pct
			if span <= 1e-9 {
				return s1.val, s1.val, 0, true
			}
			local := (p - s0.pct) / span
			return s0.val, s1.val, b.anim.timing.eval(local), true
		}
	}
	last := stops[len(stops)-1].val
	return last, last, 0, true
}

func interpolateOpacity(a, b string, u float64) string {
	v0, ok0 := parseClampedOpacity(a)
	v1, ok1 := parseClampedOpacity(b)
	if !ok0 || !ok1 {
		if u < 0.5 {
			return a
		}
		return b
	}
	v := math.Max(0, math.Min(1, v0+(v1-v0)*u))
	return formatCompactNum(v)
}

func parseClampedOpacity(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	pct := strings.HasSuffix(t, "%")
	if pct {
		t = strings.TrimSpace(t[:len(t)-1])
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	if pct {
		v /= 100
	}
	return math.Max(0, math.Min(1, v)), true
}

type cssColor struct {
	r, g, b, a  float64
	transparent bool
}

func interpolateColor(a, b string, u float64) string {
	c0, ok0 := parseCSSColor(a)
	c1, ok1 := parseCSSColor(b)
	if !ok0 || !ok1 {
		if u < 0.5 {
			return a
		}
		return b
	}
	if c0.transparent && !c1.transparent {
		c0.r, c0.g, c0.b = c1.r, c1.g, c1.b
	} else if c1.transparent && !c0.transparent {
		c1.r, c1.g, c1.b = c0.r, c0.g, c0.b
	}
	r := math.Max(0, math.Min(1, c0.r+(c1.r-c0.r)*u))
	g := math.Max(0, math.Min(1, c0.g+(c1.g-c0.g)*u))
	bl := math.Max(0, math.Min(1, c0.b+(c1.b-c0.b)*u))
	al := math.Max(0, math.Min(1, c0.a+(c1.a-c0.a)*u))
	ri := int(math.Round(r * 255))
	gi := int(math.Round(g * 255))
	bi := int(math.Round(bl * 255))
	if al >= 0.999 {
		return fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%s)", ri, gi, bi, formatCompactNum(al))
}

func parseCSSColor(raw string) (cssColor, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "none" || s == "currentcolor" || strings.HasPrefix(s, "url(") {
		return cssColor{}, false
	}
	if s == "transparent" {
		return cssColor{transparent: true}, true
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		parseHexByte := func(h string) (float64, bool) {
			v, err := strconv.ParseUint(h, 16, 8)
			return float64(v) / 255, err == nil
		}
		parseHexNib := func(b byte) (float64, bool) {
			v, err := strconv.ParseUint(string([]byte{b, b}), 16, 8)
			return float64(v) / 255, err == nil
		}
		switch len(hex) {
		case 3, 4:
			r, ok1 := parseHexNib(hex[0])
			g, ok2 := parseHexNib(hex[1])
			b, ok3 := parseHexNib(hex[2])
			a := 1.0
			ok4 := true
			if len(hex) == 4 {
				a, ok4 = parseHexNib(hex[3])
			}
			if ok1 && ok2 && ok3 && ok4 {
				return cssColor{r: r, g: g, b: b, a: a}, true
			}
		case 6, 8:
			r, ok1 := parseHexByte(hex[0:2])
			g, ok2 := parseHexByte(hex[2:4])
			b, ok3 := parseHexByte(hex[4:6])
			a := 1.0
			ok4 := true
			if len(hex) == 8 {
				a, ok4 = parseHexByte(hex[6:8])
			}
			if ok1 && ok2 && ok3 && ok4 {
				return cssColor{r: r, g: g, b: b, a: a}, true
			}
		}
		return cssColor{}, false
	}
	if (strings.HasPrefix(s, "rgb(") || strings.HasPrefix(s, "rgba(")) && strings.HasSuffix(s, ")") {
		open := strings.IndexByte(s, '(')
		inner := s[open+1 : len(s)-1]
		fields := strings.FieldsFunc(inner, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '/'
		})
		if len(fields) != 3 && len(fields) != 4 {
			return cssColor{}, false
		}
		var vals [4]float64
		vals[3] = 1
		for i, f := range fields {
			pct := strings.HasSuffix(f, "%")
			if pct {
				f = f[:len(f)-1]
			}
			v, err := strconv.ParseFloat(f, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return cssColor{}, false
			}
			if pct {
				vals[i] = v / 100
			} else if i == 3 {
				vals[i] = v
			} else {
				vals[i] = v / 255
			}
			vals[i] = math.Max(0, math.Min(1, vals[i]))
		}
		return cssColor{r: vals[0], g: vals[1], b: vals[2], a: vals[3]}, true
	}
	if c, ok := colornames.Map[s]; ok {
		return cssColor{
			r: float64(c.R) / 255,
			g: float64(c.G) / 255,
			b: float64(c.B) / 255,
			a: float64(c.A) / 255,
		}, true
	}
	return cssColor{}, false
}

type svgTransformKind uint8

const (
	tfTranslate svgTransformKind = iota + 1
	tfScale
	tfRotate
)

type svgTransformOp struct {
	kind svgTransformKind
	a    float64
	b    float64
	c    float64
	hasC bool
}

func interpolateTransform(a, b string, u float64, hasOrigin bool, ox, oy float64) string {
	ops0, ok0 := parseCSSTransform(a)
	ops1, ok1 := parseCSSTransform(b)
	if !ok0 || !ok1 {
		if u < 0.5 {
			return stripTransformUnits(a)
		}
		return stripTransformUnits(b)
	}
	var out []svgTransformOp
	switch {
	case len(ops0) == 0 && len(ops1) == 0:
		return ""
	case len(ops0) == 0:
		out = make([]svgTransformOp, len(ops1))
		for i, op := range ops1 {
			out[i] = lerpTransformOp(identityTransformOp(op), op, u)
		}
	case len(ops1) == 0:
		out = make([]svgTransformOp, len(ops0))
		for i, op := range ops0 {
			out[i] = lerpTransformOp(op, identityTransformOp(op), u)
		}
	case len(ops0) == len(ops1):
		compatible := true
		for i := range ops0 {
			if ops0[i].kind != ops1[i].kind {
				compatible = false
				break
			}
		}
		if compatible {
			out = make([]svgTransformOp, len(ops0))
			for i := range ops0 {
				out[i] = lerpTransformOp(ops0[i], ops1[i], u)
			}
		} else if u < 0.5 {
			out = ops0
		} else {
			out = ops1
		}
	default:
		if u < 0.5 {
			out = ops0
		} else {
			out = ops1
		}
	}
	return formatSVGTransform(out, hasOrigin, ox, oy)
}

func identityTransformOp(ref svgTransformOp) svgTransformOp {
	switch ref.kind {
	case tfScale:
		return svgTransformOp{kind: tfScale, a: 1, b: 1}
	case tfRotate:
		return svgTransformOp{kind: tfRotate, a: 0, b: ref.b, c: ref.c, hasC: ref.hasC}
	default:
		return svgTransformOp{kind: tfTranslate, a: 0, b: 0}
	}
}

func lerpTransformOp(x, y svgTransformOp, u float64) svgTransformOp {
	res := svgTransformOp{
		kind: x.kind,
		a:    x.a + (y.a-x.a)*u,
		b:    x.b + (y.b-x.b)*u,
	}
	if x.hasC && y.hasC {
		res.c = x.c + (y.c-x.c)*u
		res.hasC = true
	}
	return res
}

func parseCSSTransform(raw string) ([]svgTransformOp, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.EqualFold(s, "none") {
		return nil, true
	}
	var ops []svgTransformOp
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		for i < len(s) && s[i] != '(' {
			i++
		}
		if i >= len(s) {
			return nil, false
		}
		fn := strings.ToLower(strings.TrimSpace(s[start:i]))
		closeIdx := strings.IndexByte(s[i+1:], ')')
		if closeIdx < 0 {
			return nil, false
		}
		argStr := s[i+1 : i+1+closeIdx]
		i = i + 1 + closeIdx + 1

		args := strings.FieldsFunc(argStr, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		switch fn {
		case "translate":
			if len(args) != 1 && len(args) != 2 {
				return nil, false
			}
			x, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			y := 0.0
			if len(args) == 2 {
				y, ok = parseUnitlessPx(args[1])
				if !ok {
					return nil, false
				}
			}
			ops = append(ops, svgTransformOp{kind: tfTranslate, a: x, b: y})
		case "translatex":
			if len(args) != 1 {
				return nil, false
			}
			x, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			ops = append(ops, svgTransformOp{kind: tfTranslate, a: x, b: 0})
		case "translatey":
			if len(args) != 1 {
				return nil, false
			}
			y, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			ops = append(ops, svgTransformOp{kind: tfTranslate, a: 0, b: y})
		case "scale":
			if len(args) != 1 && len(args) != 2 {
				return nil, false
			}
			sx, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			sy := sx
			if len(args) == 2 {
				sy, ok = parseUnitlessPx(args[1])
				if !ok {
					return nil, false
				}
			}
			ops = append(ops, svgTransformOp{kind: tfScale, a: sx, b: sy})
		case "scalex":
			if len(args) != 1 {
				return nil, false
			}
			sx, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			ops = append(ops, svgTransformOp{kind: tfScale, a: sx, b: 1})
		case "scaley":
			if len(args) != 1 {
				return nil, false
			}
			sy, ok := parseUnitlessPx(args[0])
			if !ok {
				return nil, false
			}
			ops = append(ops, svgTransformOp{kind: tfScale, a: 1, b: sy})
		case "rotate":
			if len(args) != 1 && len(args) != 3 {
				return nil, false
			}
			degStr := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(args[0])), "deg")
			deg, err := strconv.ParseFloat(degStr, 64)
			if err != nil || math.IsNaN(deg) || math.IsInf(deg, 0) {
				return nil, false
			}
			op := svgTransformOp{kind: tfRotate, a: deg}
			if len(args) == 3 {
				cx, ok1 := parseUnitlessPx(args[1])
				cy, ok2 := parseUnitlessPx(args[2])
				if !ok1 || !ok2 {
					return nil, false
				}
				op.b, op.c, op.hasC = cx, cy, true
			}
			ops = append(ops, op)
		default:
			return nil, false
		}
	}
	return ops, true
}

func formatSVGTransform(ops []svgTransformOp, hasOrigin bool, ox, oy float64) string {
	var parts []string
	needsOrigin := false
	for _, op := range ops {
		switch op.kind {
		case tfTranslate:
			if math.Abs(op.a) > 1e-4 || math.Abs(op.b) > 1e-4 {
				parts = append(parts, "translate("+formatCompactNum(op.a)+" "+formatCompactNum(op.b)+")")
			}
		case tfScale:
			if math.Abs(op.a-1) > 1e-4 || math.Abs(op.b-1) > 1e-4 {
				parts = append(parts, "scale("+formatCompactNum(op.a)+" "+formatCompactNum(op.b)+")")
				needsOrigin = true
			}
		case tfRotate:
			if math.Abs(op.a) > 1e-4 {
				if op.hasC {
					parts = append(parts, "rotate("+formatCompactNum(op.a)+" "+formatCompactNum(op.b)+" "+formatCompactNum(op.c)+")")
				} else {
					parts = append(parts, "rotate("+formatCompactNum(op.a)+")")
					needsOrigin = true
				}
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	core := strings.Join(parts, " ")
	if needsOrigin && hasOrigin && (math.Abs(ox) > 1e-4 || math.Abs(oy) > 1e-4) {
		return fmt.Sprintf("translate(%s %s) %s translate(%s %s)",
			formatCompactNum(ox), formatCompactNum(oy),
			core,
			formatCompactNum(-ox), formatCompactNum(-oy),
		)
	}
	return core
}

func stripTransformUnits(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return ""
	}
	s = strings.ReplaceAll(s, "PX", "")
	s = strings.ReplaceAll(s, "px", "")
	return s
}

func formatCompactNum(v float64) string {
	r := math.Round(v*1000) / 1000
	if math.Abs(r) < 1e-4 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}
