package ui

import (
	"math"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// RichBand describes a background highlight band (e.g. annotation, find match, or selection)
// drawn behind or tinted over runs in a RichText / RichInput without splitting Span layout.
type RichBand struct {
	Start, End   int
	Fill         ui.Color
	Ring         ui.Color
	Underline    ui.Color
	Radius       float32
	PadX         float32
	FullLineBand bool
	TintOver     bool
}

// RichLinkRange marks a [Start, End) rune range linked to URL.
type RichLinkRange struct {
	Start, End   int
	URL          string
	ModifierOnly bool
}

// RichCodeEdge disambiguates caret placement and typing style at inline-code boundaries.
type RichCodeEdge uint8

const (
	RichCodeEdgeNone RichCodeEdge = iota
	// RichCodeEdgeAfter places the caret just outside the right edge of an inline code chip
	// whose End == caret; typing exits MarkCode.
	RichCodeEdgeAfter
	// RichCodeEdgeInsideStart places the caret inside the left edge of an inline code chip
	// whose Start == caret; typing enters MarkCode.
	RichCodeEdgeInsideStart
)

// RichCodeRange marks a [Start, End) inline code chip range whose background fill
// transitions uniformly across rest, hover, active caret/edit, and selection states.
type RichCodeRange struct {
	Start, End int
	PadX       float32
	PadY       float32
	Radius     float32
}

// RichDecorOptions configures shared decoration painting for both unfocused RichText and focused RichInput.
type RichDecorOptions struct {
	FontSize        float32
	Spans           []ui.Span
	Bands           []RichBand
	Links           []RichLinkRange
	CodeRanges      []RichCodeRange
	CodeBG          ui.Color
	CodeHoverBG     ui.Color
	CodeEditBG      ui.Color
	SelRange        [2]int
	OpaqueRanges    [][2]int
	LinkColor       ui.Color
	CaretPos        int // < 0 means do not draw caret
	CaretAff        ui.TextAffinity
	CaretCodeEdge   RichCodeEdge
	CaretColor      ui.Color
	CaretAlpha      float32
	HasChipBG       bool // whether any span carries an opaque background chip
	PointerWin      ui.Point
	HasPointer      bool
	LocalPointer    ui.Point
	HasLocalPointer bool
	CmdActive       bool
}

// RichInputOptions configures a single-block rich text editor whose glyph shaping and inline
// chips (monospace inline code, PaddingX, italic, bold, underline, strikethrough, colors)
// are 100% identical to unfocused ui.RichText, guaranteeing zero layout jitter on click/edit.
type RichInputOptions struct {
	Key               any
	Text              string
	Spans             []ui.Span
	Font              string
	FontSize          float32
	FontWeight        int
	LineHeight        float32
	MinHeight         float32
	TextColor         ui.Color
	TextAlign         ui.Align
	SelectionBG       ui.Color
	SelectionFG       ui.Color
	CaretColor        ui.Color
	LinkColor         ui.Color
	HideCaret         bool
	SuppressSelection bool
	SuppressNavKeys   bool
	ReadOnly          bool
	SyncSelection     bool
	FocusPending      bool
	Anchor            int
	Caret             int
	Bands             []RichBand
	Links             []RichLinkRange
	CodeRanges        []RichCodeRange
	CodeBG            ui.Color
	CodeHoverBG       ui.Color
	CodeEditBG        ui.Color
	SelRange          [2]int
	OpaqueRanges      [][2]int
	HasChipBG         bool

	// OnBounds reports the input box after layout.
	OnBounds func(ui.Rect)
	// OnChange runs synchronously on every edit. If the caller normalizes or transforms
	// the text (e.g. "->" to "→" or "# " heading conversion), it may call st.Sync(...)
	// inside OnChange so subsequent keystrokes in the same frame see the transformed text.
	OnChange func(newText string, anchor, caret int)
	// OnSelect runs when selection/caret moves without editing text.
	OnSelect func(anchor, caret int)
	// OnCopy optionally overrides copy command / Cmd+C (e.g. to copy rich Markdown).
	OnCopy func(svc ui.Services) bool
	// OnCut optionally overrides cut command / Cmd+X (e.g. to cut rich Markdown).
	OnCut func(svc ui.Services) bool
	// OnPaste intercepts paste commands and Cmd/Ctrl+V before plain text insertion.
	OnPaste func(svc ui.Services, pastedText string) bool
	// OnPastePlain intercepts pasteAndMatchStyle and Shift+Cmd/Ctrl+V for unformatted literal paste.
	OnPastePlain func(svc ui.Services, pastedText string) bool
	// OnKey sees non-character keys before RichInput handles them; returning true consumes the key.
	OnKey func(key ui.Key, mods ui.Modifiers, curText string, anchor, caret int) bool
	// OnText sees committed character input before RichInput replaces the selection; returning true consumes it.
	OnText func(text string, anchor, caret int) bool
	// OnEdge runs when an arrow key would move the caret past the start or end of this input
	// (Up/Down on the first/last visual line, Left/Right at offset 0/len); returning true consumes it.
	OnEdge func(RichEdge) bool
	// CarryColumnX is a window X the next Up/Down keeps as its column, so arrow navigation that
	// enters this input from another block stays in the same visual column. Read only with SyncSelection.
	CarryColumnX float32
	CarryColumn  bool
	// OnOpenLink opens a clicked link (e.g. Cmd+Click).
	OnOpenLink func(url string)
	// OnResolveCmdLink resolves a modifier-only link target on Cmd+Click even before async hover completes.
	OnResolveCmdLink func(pt ui.Point, lay *ui.TextLayout, text string) string
	// OnContextMenu appends optional host items to the RichInput context menu.
	OnContextMenu func(m *ui.Menu)
}

type richLayoutKey struct {
	text      string
	spans     string
	width     float32
	family    string
	size      float32
	weight    int
	lh        float32
	align     ui.Align
	fontEpoch uint64
}

// RichEdge describes an arrow-key caret move that runs past the start or end of a RichInput,
// so the host can continue it into the neighbouring block or table cell.
type RichEdge struct {
	// Dir is -1 for Up/Left and +1 for Down/Right.
	Dir int
	// Vertical reports Up/Down rather than Left/Right.
	Vertical bool
	// Extend reports Shift held: the host grows the selection instead of moving the caret.
	Extend bool
	// ColumnX is the window X of the caret column to keep on vertical moves.
	ColumnX float32
}

// RichInputState holds persistent editing, IME composition, selection, and cached layout state
// for a single block or table cell.
type RichInputState struct {
	text                string
	anchor              int
	caret               int
	affinity            ui.TextAffinity
	desiredX            float32
	hasDesired          bool
	carryWinX           float32
	hasCarry            bool
	compose             string
	composeCaret        int
	composeSelected     ui.TextInputRange
	composeStart        int
	composeEnd          int
	composeSavedAnchor  int
	composeSavedCaret   int
	composeHasRange     bool
	composing           bool
	inBatchContinuation bool
	detached            bool
	onDetachedReplace   func(delStart, delEnd int, ins string)

	dragging         bool
	dragUnit         int
	dragStart        [2]int
	pressPt          ui.Point
	pressMods        ui.Modifiers
	pressLink        string
	pressLinkPending bool

	layKey       richLayoutKey
	layout       *ui.TextLayout
	displayText  string
	displaySpans []ui.Span
	font         ui.Font
	align        ui.Align
	lastRect     ui.Rect

	services        ui.Services
	readOnly        bool
	suppressNavKeys bool
	links           []RichLinkRange
	codeRanges      []RichCodeRange
	codeEdge        RichCodeEdge
	onChange        func(newText string, anchor, caret int)
	onSelect        func(anchor, caret int)
	onCopy          func(ui.Services) bool
	onCut           func(ui.Services) bool
	onPaste         func(svc ui.Services, pastedText string) bool
	onPastePlain    func(svc ui.Services, pastedText string) bool
	onKey           func(key ui.Key, mods ui.Modifiers, curText string, anchor, caret int) bool
	onText          func(text string, anchor, caret int) bool
	onEdge           func(RichEdge) bool
	onOpenLink       func(url string)
	onResolveCmdLink func(pt ui.Point, lay *ui.TextLayout, text string) string

	edgeSyncedInChange bool
	blinkPos           int
	blinkEpoch         time.Time
}

var _ ui.TextInputClient = (*RichInputState)(nil)

const (
	richCaretOnMs  = 530
	richCaretOffMs = 530
)

// Sync updates the state's underlying text and rune selection immediately.
func (s *RichInputState) Sync(text string, anchor, caret int) {
	s.SyncWithCodeEdge(text, anchor, caret, RichCodeEdgeNone)
}

// SyncWithCodeEdge updates text, selection, and inline-code/math boundary bias immediately.
func (s *RichInputState) SyncWithCodeEdge(text string, anchor, caret int, edge RichCodeEdge) {
	n := utf8.RuneCountInString(text)
	s.text = text
	s.anchor = clampRune(anchor, n)
	s.caret = clampRune(caret, n)
	s.affinity = ui.TextDownstream
	s.codeEdge = edge
	if edge != RichCodeEdgeNone {
		s.edgeSyncedInChange = true
	}
	s.resetColumn()
}

// SetCodeEdge sets the inline-code/math boundary bias directly.
func (s *RichInputState) SetCodeEdge(edge RichCodeEdge) {
	s.codeEdge = edge
	s.blinkEpoch = time.Time{}
}

// CodeEdge reports the current inline-code boundary state for the caret.
func (s *RichInputState) CodeEdge() RichCodeEdge {
	return s.codeEdge
}

// CancelPendingLink cancels any armed Cmd+Click link gesture on this input.
func (s *RichInputState) CancelPendingLink() {
	s.pressLink = ""
	s.pressLinkPending = false
}

func (s *RichInputState) codeRangeEndingAt(pos int) (RichCodeRange, bool) {
	for _, cr := range s.codeRanges {
		if cr.End > cr.Start && cr.End == pos {
			return cr, true
		}
	}
	return RichCodeRange{}, false
}

func (s *RichInputState) codeRangeStartingAt(pos int) (RichCodeRange, bool) {
	for _, cr := range s.codeRanges {
		if cr.End > cr.Start && cr.Start == pos {
			return cr, true
		}
	}
	return RichCodeRange{}, false
}

func (s *RichInputState) normalizeCodeEdge() {
	if s.anchor != s.caret {
		s.codeEdge = RichCodeEdgeNone
		return
	}
	switch s.codeEdge {
	case RichCodeEdgeAfter:
		if _, ok := s.codeRangeEndingAt(s.caret); !ok {
			s.codeEdge = RichCodeEdgeNone
		}
	case RichCodeEdgeInsideStart:
		if _, ok := s.codeRangeStartingAt(s.caret); !ok {
			s.codeEdge = RichCodeEdgeNone
		}
	}
}

func (s *RichInputState) setCodeEdge(edge RichCodeEdge) {
	s.codeEdge = edge
	s.normalizeCodeEdge()
	s.blinkEpoch = time.Time{}
}

// SelectionRunes returns the current (anchor, caret) rune offsets.
func (s *RichInputState) SelectionRunes() (anchor, caret int) {
	return s.anchor, s.caret
}

// OrderedSelection returns (min(anchor, caret), max(anchor, caret)).
func (s *RichInputState) OrderedSelection() (lo, hi int) {
	if s.anchor <= s.caret {
		return s.anchor, s.caret
	}
	return s.caret, s.anchor
}

// Dragging reports whether a pointer drag gesture initiated inside this input is active.
func (s *RichInputState) Dragging() bool {
	return s.dragging && !s.pressLinkPending
}

// Composing reports whether an IME preedit session is active.
func (s *RichInputState) Composing() bool {
	return s.composing && s.compose != ""
}

func clampRune(v, maxLen int) int {
	if v < 0 {
		return 0
	}
	if v > maxLen {
		return maxLen
	}
	return v
}

func runeToUTF16Off(str string, runeOff int) int {
	if runeOff <= 0 || str == "" {
		return 0
	}
	u := 0
	i := 0
	for _, r := range str {
		if i >= runeOff {
			return u
		}
		u += utf16.RuneLen(r)
		i++
	}
	return u
}

func utf16ToRuneOff(str string, u16Off int) int {
	if u16Off <= 0 || str == "" {
		return 0
	}
	u := 0
	i := 0
	for _, r := range str {
		rl := utf16.RuneLen(r)
		if u+rl > u16Off {
			return i
		}
		u += rl
		i++
	}
	return i
}

func spansLayoutSig(spans []ui.Span) string {
	if len(spans) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(spans) * 24)
	for _, sp := range spans {
		b.WriteString(sp.Font)
		b.WriteByte(0)
		b.WriteRune(rune(math.Float32bits(sp.Size)))
		b.WriteRune(rune(sp.Weight))
		if sp.Italic {
			b.WriteByte('I')
		}
		b.WriteRune(rune(math.Float32bits(sp.PaddingX)))
		b.WriteRune(rune(math.Float32bits(sp.PaddingStart)))
		b.WriteRune(rune(utf8.RuneCountInString(sp.Text)))
		b.WriteByte('|')
	}
	return b.String()
}

func insertComposeIntoSpans(base []ui.Span, plain string, replaceStart, replaceEnd int, compose string, codeRanges []RichCodeRange, codeEdge RichCodeEdge) []ui.Span {
	if compose == "" {
		return base
	}
	if len(base) == 0 {
		if plain == "" {
			return []ui.Span{{Text: compose, Underline: true}}
		}
		base = []ui.Span{{Text: plain}}
	}
	if replaceEnd < replaceStart {
		replaceEnd = replaceStart
	}
	hasCodeStartAt := func(pos int) bool {
		for _, cr := range codeRanges {
			if cr.End > cr.Start && cr.Start == pos {
				return true
			}
		}
		return false
	}
	hasCodeEndAt := func(pos int) bool {
		for _, cr := range codeRanges {
			if cr.End > cr.Start && cr.End == pos {
				return true
			}
		}
		return false
	}

	out := make([]ui.Span, 0, len(base)+2)
	pos := 0
	inserted := false
	for _, sp := range base {
		rs := []rune(sp.Text)
		s0, s1 := pos, pos+len(rs)
		pos = s1

		// When composing over a non-empty selection [replaceStart, replaceEnd), hide the replaced
		// runes visually during pre-edit without mutating the underlying document.
		if replaceEnd > replaceStart {
			if s1 <= replaceStart {
				out = append(out, sp)
				continue
			}
			if s0 >= replaceEnd {
				if !inserted {
					out = append(out, ui.Span{Text: compose, Underline: true})
					inserted = true
				}
				out = append(out, sp)
				continue
			}
			if s0 < replaceStart {
				left := sp
				left.Text = string(rs[:replaceStart-s0])
				out = append(out, left)
			}
			if !inserted {
				compSpan := ui.Span{
					Text:      compose,
					Font:      sp.Font,
					Size:      sp.Size,
					Weight:    sp.Weight,
					Italic:    sp.Italic,
					Color:     sp.Color,
					Underline: true,
				}
				out = append(out, compSpan)
				inserted = true
			}
			if s1 > replaceEnd {
				right := sp
				right.Text = string(rs[replaceEnd-s0:])
				out = append(out, right)
			}
			continue
		}

		caretRune := replaceStart
		if !inserted && caretRune >= s0 && caretRune <= s1 {
			// If caret is inside the start of the upcoming code range, skip the preceding
			// span's end boundary so composition inherits the code span's styling.
			if caretRune == s1 && codeEdge == RichCodeEdgeInsideStart && hasCodeStartAt(caretRune) {
				out = append(out, sp)
				continue
			}
			// Caret outside the right edge of a code range: place plain composition after it.
			if caretRune == s1 && codeEdge == RichCodeEdgeAfter && hasCodeEndAt(caretRune) {
				out = append(out, sp)
				out = append(out, ui.Span{Text: compose, Underline: true})
				inserted = true
				continue
			}
			// Caret outside the left edge of a leading code range: place plain composition before it.
			if caretRune == s0 && codeEdge == RichCodeEdgeNone && hasCodeStartAt(caretRune) {
				out = append(out, ui.Span{Text: compose, Underline: true})
				out = append(out, sp)
				inserted = true
				continue
			}
			local := caretRune - s0
			if local > 0 {
				left := sp
				left.Text = string(rs[:local])
				out = append(out, left)
			}
			compSpan := ui.Span{
				Text:      compose,
				Font:      sp.Font,
				Size:      sp.Size,
				Weight:    sp.Weight,
				Italic:    sp.Italic,
				Color:     sp.Color,
				Underline: true,
			}
			out = append(out, compSpan)
			if local < len(rs) {
				right := sp
				right.Text = string(rs[local:])
				out = append(out, right)
			}
			inserted = true
			continue
		}
		out = append(out, sp)
	}
	if !inserted {
		out = append(out, ui.Span{Text: compose, Underline: true})
	}
	return out
}

func (s *RichInputState) ensureLayout(width float32) *ui.TextLayout {
	w := max(width, 1)
	key := richLayoutKey{
		text:      s.displayText,
		spans:     spansLayoutSig(s.displaySpans),
		width:     w,
		family:    s.font.Family,
		size:      s.font.Size,
		weight:    s.font.Weight,
		lh:        s.font.LineHeight,
		align:     s.align,
		fontEpoch: theme.FontEpoch(),
	}
	if s.layout != nil && s.layKey == key {
		return s.layout
	}
	spans := s.displaySpans
	if len(spans) == 0 {
		spans = []ui.Span{{Text: ""}}
	}
	s.layout = ui.ShapeRichTextWith(spans, s.font, ui.TextShape{
		Width: w,
		Align: s.align,
	})
	s.layKey = key
	return s.layout
}

func (s *RichInputState) currentLayout() *ui.TextLayout {
	w := s.lastRect.W
	if w <= 0 {
		w = 600
	}
	return s.ensureLayout(w)
}

func (s *RichInputState) composeBaseBounds(n int) (int, int) {
	if s.composeHasRange {
		a := clampRune(s.composeStart, n)
		z := clampRune(s.composeEnd, n)
		if a > z {
			a, z = z, a
		}
		return a, z
	}
	at := clampRune(s.caret, n)
	return at, at
}

func (s *RichInputState) virtualString() string {
	if s.compose == "" {
		return s.text
	}
	rs := []rune(s.text)
	a, z := s.composeBaseBounds(len(rs))
	return string(rs[:a]) + s.compose + string(rs[z:])
}

func (s *RichInputState) virtualUTF16ToBaseRune(u16 int) int {
	if s.compose == "" {
		return utf16ToRuneOff(s.text, u16)
	}
	rs := []rune(s.text)
	a, z := s.composeBaseBounds(len(rs))
	prefix := string(rs[:a])
	prefU := ui.UTF16Len(prefix)
	compU := ui.UTF16Len(s.compose)
	if u16 <= prefU {
		return utf16ToRuneOff(prefix, u16)
	}
	if u16 <= prefU+compU {
		return a
	}
	suffix := string(rs[z:])
	return z + utf16ToRuneOff(suffix, u16-prefU-compU)
}

func (s *RichInputState) virtualUTF16RangeToBaseRunes(u0, u1 int) (int, int) {
	if u0 > u1 {
		u0, u1 = u1, u0
	}
	if s.compose == "" {
		return utf16ToRuneOff(s.text, u0), utf16ToRuneOff(s.text, u1)
	}
	rs := []rune(s.text)
	a, z := s.composeBaseBounds(len(rs))
	prefix := string(rs[:a])
	prefU := ui.UTF16Len(prefix)
	compU := ui.UTF16Len(s.compose)
	var baseA, baseZ int
	switch {
	case u0 <= prefU:
		baseA = utf16ToRuneOff(prefix, u0)
	case u0 <= prefU+compU:
		baseA = a
	default:
		baseA = z + utf16ToRuneOff(string(rs[z:]), u0-prefU-compU)
	}
	switch {
	case u1 <= prefU:
		baseZ = utf16ToRuneOff(prefix, u1)
	case u1 <= prefU+compU:
		baseZ = z
	default:
		baseZ = z + utf16ToRuneOff(string(rs[z:]), u1-prefU-compU)
	}
	if baseA > baseZ {
		baseA, baseZ = baseZ, baseA
	}
	return baseA, baseZ
}

// --- ui.TextInputClient Implementation ---

func (s *RichInputState) TextForRange(r ui.TextInputRange) (string, ui.TextInputRange) {
	if r.Start > r.End {
		r.Start, r.End = r.End, r.Start
	}
	virt := s.virtualString()
	totalU := ui.UTF16Len(virt)
	u0 := min(max(r.Start, 0), totalU)
	u1 := min(max(r.End, u0), totalU)
	b0 := ui.UTF16ByteOffset(virt, u0)
	b1 := ui.UTF16ByteOffset(virt, u1)
	actual0 := ui.UTF16Len(virt[:b0])
	actual1 := ui.UTF16Len(virt[:b1])
	return virt[b0:b1], ui.TextInputRange{Start: actual0, End: actual1}
}

func (s *RichInputState) Selection() ui.TextInputSelection {
	if s.compose != "" {
		a, _ := s.composeBaseBounds(utf8.RuneCountInString(s.text))
		startU := runeToUTF16Off(s.text, a)
		return ui.TextInputSelection{
			Range: ui.TextInputRange{
				Start: startU + s.composeSelected.Start,
				End:   startU + s.composeSelected.End,
			},
		}
	}
	lo, hi := s.OrderedSelection()
	return ui.TextInputSelection{
		Range: ui.TextInputRange{
			Start: runeToUTF16Off(s.text, lo),
			End:   runeToUTF16Off(s.text, hi),
		},
		Reversed: s.caret < s.anchor,
	}
}

func (s *RichInputState) MarkedRange() (ui.TextInputRange, bool) {
	if s.compose == "" {
		return ui.TextInputRange{}, false
	}
	a, _ := s.composeBaseBounds(utf8.RuneCountInString(s.text))
	startU := runeToUTF16Off(s.text, a)
	return ui.TextInputRange{Start: startU, End: startU + ui.UTF16Len(s.compose)}, true
}

func (s *RichInputState) clearComposeState() {
	s.compose = ""
	s.composing = false
	s.composeCaret = 0
	s.composeSelected = ui.TextInputRange{}
	s.composeStart = 0
	s.composeEnd = 0
	s.composeHasRange = false
}

func (s *RichInputState) ReplaceText(r *ui.TextInputRange, ins string) {
	if s.readOnly {
		return
	}
	ins = strings.ReplaceAll(ins, "\r\n", "\n")
	ins = strings.ReplaceAll(ins, "\r", "\n")

	wasComposing := s.compose != "" || s.composing
	if wasComposing && r != nil {
		rs := []rune(s.text)
		baseA, baseZ := s.composeBaseBounds(len(rs))
		comp := s.compose
		compLen := utf8.RuneCountInString(comp)
		virt := string(rs[:baseA]) + comp + string(rs[baseZ:])
		virtR := []rune(virt)
		u0, u1 := min(r.Start, r.End), max(r.Start, r.End)
		v0 := clampRune(utf16ToRuneOff(virt, u0), len(virtR))
		v1 := clampRune(utf16ToRuneOff(virt, u1), len(virtR))
		if v0 > v1 {
			v0, v1 = v1, v0
		}
		// When the explicit replacement range is disjoint from the composition range,
		// apply the composition commit and the replacement as two separate edits so any
		// untouched formatted spans between them keep their styling intact.
		if v0 >= baseA+compLen || v1 <= baseA {
			beforeStep1 := s.text
			s.clearComposeState()
			s.replaceRunes(baseA, baseZ, comp)
			if s.detached {
				delStart, delEnd := 0, 0
				if v0 >= baseA+compLen {
					delStart = v0 - (baseA + compLen)
					delEnd = v1 - (baseA + compLen)
				}
				if (delEnd > delStart || ins != "") && s.onDetachedReplace != nil {
					s.onDetachedReplace(delStart, delEnd, ins)
				}
				return
			}
			step1Changed := s.text != beforeStep1
			curLen := utf8.RuneCountInString(s.text)
			if v0 >= baseA+compLen {
				delta := curLen - len(virtR)
				v0 = clampRune(v0+delta, curLen)
				v1 = clampRune(v1+delta, curLen)
			} else {
				v0 = clampRune(v0, curLen)
				v1 = clampRune(v1, curLen)
			}
			if step1Changed {
				s.inBatchContinuation = true
			}
			s.replaceRunes(v0, v1, ins)
			s.inBatchContinuation = false
			return
		}
		virtToBaseLo := func(v int) int {
			switch {
			case v <= baseA:
				return v
			case v <= baseA+compLen:
				return baseA
			default:
				return baseZ + (v - baseA - compLen)
			}
		}
		virtToBaseHi := func(v int) int {
			switch {
			case v < baseA:
				return v
			case v < baseA+compLen:
				return baseZ
			default:
				return baseZ + (v - baseA - compLen)
			}
		}
		repStart := min(baseA, virtToBaseLo(v0))
		repEnd := max(baseZ, virtToBaseHi(v1))
		virtEnd := clampRune(baseA+compLen+(repEnd-baseZ), len(virtR))
		mergedIns := string(virtR[repStart:v0]) + ins + string(virtR[v1:virtEnd])
		finalCaret := v0 + utf8.RuneCountInString(ins)
		s.clearComposeState()
		s.replaceRunesWithCaret(repStart, repEnd, mergedIns, finalCaret)
		return
	}

	var a, z int
	switch {
	case r != nil:
		a, z = s.virtualUTF16RangeToBaseRunes(r.Start, r.End)
	case wasComposing:
		a, z = s.composeBaseBounds(utf8.RuneCountInString(s.text))
	default:
		a, z = s.OrderedSelection()
	}
	s.clearComposeState()
	if !wasComposing && ins != "" && a < z && s.onText != nil && s.onText(ins, a, z) {
		return
	}
	s.replaceRunes(a, z, ins)
}

func (s *RichInputState) SetMarkedText(r *ui.TextInputRange, text string, selected ui.TextInputRange) {
	if s.readOnly {
		return
	}
	if text == "" && r == nil && s.compose == "" && !s.composing {
		return
	}
	n := utf8.RuneCountInString(s.text)
	if !s.composing && !s.composeHasRange {
		s.composeSavedAnchor = s.anchor
		s.composeSavedCaret = s.caret
		if r != nil {
			a, z := s.virtualUTF16RangeToBaseRunes(r.Start, r.End)
			s.composeStart, s.composeEnd = a, z
		} else {
			lo, hi := s.OrderedSelection()
			s.composeStart, s.composeEnd = lo, hi
		}
		s.composeHasRange = true
	} else if r != nil {
		a, z := s.virtualUTF16RangeToBaseRunes(r.Start, r.End)
		s.composeStart, s.composeEnd = a, z
		s.composeHasRange = true
	}

	if text == "" {
		// Canceling IME composition restores the untouched original text and selection.
		savedA := clampRune(s.composeSavedAnchor, n)
		savedC := clampRune(s.composeSavedCaret, n)
		s.clearComposeState()
		s.anchor = savedA
		s.caret = savedC
		s.displayText = s.virtualString()
		return
	}

	if selected.Start > selected.End {
		selected.Start, selected.End = selected.End, selected.Start
	}
	compU := ui.UTF16Len(text)
	s0 := min(max(selected.Start, 0), compU)
	s1 := min(max(selected.End, s0), compU)
	s0 = ui.UTF16Len(text[:ui.UTF16ByteOffset(text, s0)])
	s1 = ui.UTF16Len(text[:ui.UTF16ByteOffset(text, s1)])

	s.compose = text
	s.composing = true
	s.composeSelected = ui.TextInputRange{Start: s0, End: s1}
	s.composeCaret = utf16ToRuneOff(text, s1)
	s.displayText = s.virtualString()
}

func (s *RichInputState) UnmarkText() {
	if s.compose != "" {
		committed := s.compose
		a, z := s.composeBaseBounds(utf8.RuneCountInString(s.text))
		s.clearComposeState()
		s.replaceRunes(a, z, committed)
		return
	}
	if s.composeHasRange {
		n := utf8.RuneCountInString(s.text)
		s.anchor = clampRune(s.composeSavedAnchor, n)
		s.caret = clampRune(s.composeSavedCaret, n)
	}
	s.clearComposeState()
	s.displayText = s.virtualString()
}

func (s *RichInputState) BoundsForRange(r ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	if s.lastRect.W <= 0 {
		return ui.Rect{}, ui.TextInputRange{}, false
	}
	lay := s.ensureLayout(s.lastRect.W)
	virt := s.virtualString()
	totalU := ui.UTF16Len(virt)
	u0 := min(max(min(r.Start, r.End), 0), totalU)
	u1 := min(max(max(r.Start, r.End), u0), totalU)
	u0 = ui.UTF16Len(virt[:ui.UTF16ByteOffset(virt, u0)])
	u1 = ui.UTF16Len(virt[:ui.UTF16ByteOffset(virt, u1)])
	cr := lay.CaretAt(ui.TextCaretPosition{Index: u0, Affinity: ui.TextDownstream})
	actual := ui.TextInputRange{Start: u0, End: u0}
	w := float32(1)
	if u1 > u0 {
		cr2 := lay.CaretAt(ui.TextCaretPosition{Index: u1, Affinity: ui.TextUpstream})
		if math.Abs(float64(cr2.Y-cr.Y)) < 1 {
			w = max(cr2.X-cr.X, 1)
			actual.End = u1
		}
	}
	return ui.Rect{X: cr.X, Y: cr.Y, W: w, H: cr.H}, actual, true
}

func (s *RichInputState) IndexForPoint(p ui.Point) (int, bool) {
	if s.lastRect.W <= 0 {
		return 0, false
	}
	lay := s.ensureLayout(s.lastRect.W)
	cp := lay.PositionAt(p)
	return cp.Index, true
}

// --- Editing, Navigation & Input Handling ---

func (s *RichInputState) InBatchContinuation() bool {
	return s.inBatchContinuation
}

func (s *RichInputState) SetDetached(v bool) {
	s.detached = v
}

func (s *RichInputState) SetOnDetachedReplace(fn func(delStart, delEnd int, ins string)) {
	s.onDetachedReplace = fn
}

func (s *RichInputState) replaceRunes(a, z int, ins string) {
	s.replaceRunesWithCaret(a, z, ins, -1)
}

func (s *RichInputState) replaceRunesWithCaret(a, z int, ins string, caretOverride int) {
	rs := []rune(s.text)
	a = clampRune(a, len(rs))
	z = clampRune(z, len(rs))
	if a > z {
		a, z = z, a
	}
	var postDeleteEdge RichCodeEdge
	if ins == "" && a < z {
		// Deleting plain text right after a code chip keeps the caret outside the chip's
		// right edge so the user can keep typing plain text instead of falling back inside.
		if _, ok := s.codeRangeEndingAt(a); ok {
			postDeleteEdge = RichCodeEdgeAfter
		} else if cr, ok := s.codeRangeStartingAt(a); ok && s.codeEdge == RichCodeEdgeInsideStart && z < cr.End {
			postDeleteEdge = RichCodeEdgeInsideStart
		}
	}
	// Preserve code-boundary bias only for pure insertions at the active boundary so OnChange
	// can apply the right MarkCode inheritance; deletions/replacements clear it immediately.
	if a != z || ins == "" {
		s.codeEdge = RichCodeEdgeNone
	} else {
		s.normalizeCodeEdge()
	}
	next := string(rs[:a]) + ins + string(rs[z:])
	newCaret := a + utf8.RuneCountInString(ins)
	if caretOverride >= 0 {
		newCaret = clampRune(caretOverride, utf8.RuneCountInString(next))
	}
	s.text = next
	s.anchor = newCaret
	s.caret = newCaret
	s.affinity = ui.TextDownstream
	s.resetColumn()
	s.blinkEpoch = time.Time{}
	s.edgeSyncedInChange = false
	if s.onChange != nil {
		s.onChange(s.text, s.anchor, s.caret)
	}
	if ins == "" && a < z && len(s.codeRanges) > 0 {
		delLen := z - a
		updated := make([]RichCodeRange, 0, len(s.codeRanges))
		for _, cr := range s.codeRanges {
			switch {
			case cr.End <= a:
				updated = append(updated, cr)
			case cr.Start >= z:
				cr.Start -= delLen
				cr.End -= delLen
				updated = append(updated, cr)
			default:
				ns := min(cr.Start, a)
				ne := max(cr.End-delLen, a)
				if ne > ns {
					cr.Start = ns
					cr.End = ne
					updated = append(updated, cr)
				}
			}
		}
		s.codeRanges = updated
	}
	if !s.edgeSyncedInChange {
		s.codeEdge = postDeleteEdge
		s.normalizeCodeEdge()
	}
	s.edgeSyncedInChange = false
	s.displayText = s.virtualString()
}

func (s *RichInputState) moveCaretTo(pos int, aff ui.TextAffinity, extend bool) {
	n := utf8.RuneCountInString(s.text)
	pos = clampRune(pos, n)
	s.caret = pos
	s.affinity = aff
	if !extend {
		s.anchor = pos
	}
	s.codeEdge = RichCodeEdgeNone
	s.blinkEpoch = time.Time{}
	if s.onSelect != nil {
		s.onSelect(s.anchor, s.caret)
	}
}

func isRichWordCat(r rune) uint8 {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r >= 0x4e00 && r <= 0x9fff:
		return 1
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
		return 2
	default:
		return 3
	}
}

func richWordRangeAt(rs []rune, off int) (int, int) {
	if len(rs) == 0 {
		return 0, 0
	}
	at := min(max(off, 0), len(rs)-1)
	cat := isRichWordCat(rs[at])
	lo, hi := at, at+1
	for lo > 0 && isRichWordCat(rs[lo-1]) == cat {
		lo--
	}
	for hi < len(rs) && isRichWordCat(rs[hi]) == cat {
		hi++
	}
	return lo, hi
}

func prevWordOff(rs []rune, caret int) int {
	i := clampRune(caret, len(rs))
	for i > 0 && unicode.IsSpace(rs[i-1]) {
		i--
	}
	if i == 0 {
		return 0
	}
	cat := isRichWordCat(rs[i-1])
	for i > 0 && isRichWordCat(rs[i-1]) == cat {
		i--
	}
	return i
}

func nextWordOff(rs []rune, caret int) int {
	n := len(rs)
	i := clampRune(caret, n)
	for i < n && unicode.IsSpace(rs[i]) {
		i++
	}
	if i >= n {
		return n
	}
	cat := isRichWordCat(rs[i])
	for i < n && isRichWordCat(rs[i]) == cat {
		i++
	}
	return i
}

func (s *RichInputState) lineRuneEdges(pos int) (int, int) {
	lay := s.currentLayout()
	u := runeToUTF16Off(s.text, pos)
	lines := lay.Lines()
	for i, ln := range lines {
		if u >= ln.Start && (u < ln.End || i == len(lines)-1) {
			return utf16ToRuneOff(s.text, ln.Start), utf16ToRuneOff(s.text, ln.End)
		}
	}
	n := utf8.RuneCountInString(s.text)
	return 0, n
}

// resetColumn forgets the sticky Up/Down column after a non-vertical caret move.
func (s *RichInputState) resetColumn() {
	s.hasDesired = false
	s.hasCarry = false
}

// emitEdge hands a caret move past this input's edge to the host.
func (s *RichInputState) emitEdge(dir int, vertical, extend bool, columnX float32) bool {
	if s.onEdge == nil {
		return false
	}
	return s.onEdge(RichEdge{Dir: dir, Vertical: vertical, Extend: extend, ColumnX: s.lastRect.X + columnX})
}

func (s *RichInputState) moveVertical(dir int, extend bool) bool {
	lay := s.currentLayout()
	lines := lay.Lines()
	u := runeToUTF16Off(s.text, s.caret)
	cr := lay.CaretAt(ui.TextCaretPosition{Index: u, Affinity: s.affinity})
	if !s.hasDesired {
		s.desiredX = cr.X
		if s.hasCarry {
			s.desiredX = s.carryWinX - s.lastRect.X
		}
		s.hasDesired = true
	}
	s.hasCarry = false
	lineIdx := 0
	for i, ln := range lines {
		if cr.Y+cr.H*0.5 >= ln.Rect.Y && cr.Y+cr.H*0.5 <= ln.Rect.Y+ln.Rect.H {
			lineIdx = i
			break
		}
	}
	targetLine := lineIdx + dir
	if len(lines) <= 1 || targetLine < 0 || targetLine >= len(lines) {
		boundary := 0
		if dir > 0 {
			boundary = utf8.RuneCountInString(s.text)
		}
		// Shift+Up/Down first extends to the block edge, the next press crosses into the neighbour.
		if (!extend || s.caret == boundary) && s.emitEdge(dir, true, extend, s.desiredX) {
			return true
		}
		s.moveCaretTo(boundary, ui.TextDownstream, extend)
		s.hasDesired = true
		return true
	}
	tln := lines[targetLine]
	cp := lay.PositionAt(ui.Point{X: s.desiredX, Y: tln.Rect.Y + tln.Rect.H*0.5})
	s.moveCaretTo(utf16ToRuneOff(s.text, cp.Index), cp.Affinity, extend)
	s.hasDesired = true
	return true
}

func (s *RichInputState) linkAtPoint(pt ui.Point, cmdActive bool) string {
	if len(s.links) == 0 || s.Composing() {
		return ""
	}
	lay := s.currentLayout()
	for _, lr := range s.links {
		if lr.URL == "" || lr.End <= lr.Start || (lr.ModifierOnly && !cmdActive) {
			continue
		}
		for _, rect := range RichLinkRects(lay, s.text, lr) {
			if rect.Contains(pt.X, pt.Y) {
				return lr.URL
			}
		}
	}
	return ""
}

// RichLinkRects returns the visual selection rectangles covering link inside lay.
func RichLinkRects(lay *ui.TextLayout, text string, link RichLinkRange) []ui.Rect {
	if lay == nil || link.End <= link.Start {
		return nil
	}
	return lay.SelectionRects(ui.TextInputRange{
		Start: runeToUTF16Off(text, link.Start),
		End:   runeToUTF16Off(text, link.End),
	})
}

func (s *RichInputState) clearComposition() {
	s.compose = ""
	s.composing = false
	s.composeCaret = 0
	s.composeSelected = ui.TextInputRange{}
}

func (s *RichInputState) settleCompositionBeforeCommand() {
	if !s.Composing() {
		return
	}
	if s.readOnly {
		s.clearComposition()
		return
	}
	s.UnmarkText()
}

func (s *RichInputState) handleCommand(cmd string) bool {
	switch cmd {
	case "copy":
		if s.onCopy != nil && s.onCopy(s.services) {
			return true
		}
		lo, hi := s.OrderedSelection()
		if hi > lo {
			rs := []rune(s.text)
			s.services.WriteClipboard(string(rs[lo:hi]))
		}
		return true
	case "cut":
		if s.readOnly {
			return true
		}
		s.settleCompositionBeforeCommand()
		if s.onCut != nil && s.onCut(s.services) {
			return true
		}
		lo, hi := s.OrderedSelection()
		if hi > lo {
			rs := []rune(s.text)
			s.services.WriteClipboard(string(rs[lo:hi]))
			s.replaceRunes(lo, hi, "")
		}
		return true
	case "paste":
		if s.readOnly {
			return true
		}
		s.settleCompositionBeforeCommand()
		clip := s.services.ReadClipboard()
		if s.onPaste != nil && s.onPaste(s.services, clip) {
			return true
		}
		if clip != "" {
			lo, hi := s.OrderedSelection()
			s.replaceRunes(lo, hi, clip)
		}
		return true
	case "pasteAndMatchStyle":
		if s.readOnly {
			return true
		}
		s.settleCompositionBeforeCommand()
		clip := s.services.ReadClipboard()
		if s.onPastePlain != nil && s.onPastePlain(s.services, clip) {
			return true
		}
		if clip != "" {
			lo, hi := s.OrderedSelection()
			s.replaceRunes(lo, hi, clip)
		}
		return true
	case "selectAll":
		s.settleCompositionBeforeCommand()
		if s.onKey != nil && s.onKey(ui.KeyA, ui.Cmd, s.text, s.anchor, s.caret) {
			return true
		}
		n := utf8.RuneCountInString(s.text)
		s.anchor = 0
		s.caret = n
		s.affinity = ui.TextDownstream
		s.codeEdge = RichCodeEdgeNone
		s.resetColumn()
		if s.onSelect != nil {
			s.onSelect(s.anchor, s.caret)
		}
		return true
	case "delete":
		if s.readOnly {
			return true
		}
		s.settleCompositionBeforeCommand()
		lo, hi := s.OrderedSelection()
		if hi > lo {
			s.replaceRunes(lo, hi, "")
		}
		return true
	case "undo":
		if s.readOnly {
			return true
		}
		s.clearComposition()
		if s.onKey != nil {
			return s.onKey(ui.KeyZ, ui.Cmd, s.text, s.anchor, s.caret)
		}
	case "redo":
		if s.readOnly {
			return true
		}
		s.clearComposition()
		if s.onKey != nil {
			return s.onKey(ui.KeyZ, ui.Cmd|ui.Shift, s.text, s.anchor, s.caret)
		}
	}
	return false
}

func (s *RichInputState) handleKey(key ui.Key, mods ui.Modifiers) bool {
	if s.Composing() {
		return false
	}
	// Let the host editor handle slash-menu keys, block shortcuts, formatting marks,
	// Enter split, Backspace at start of block, undo/redo, etc. first.
	if s.onKey != nil && s.onKey(key, mods, s.text, s.anchor, s.caret) {
		return true
	}
	if s.suppressNavKeys {
		switch key {
		case ui.KeyUp, ui.KeyDown, ui.KeyLeft, ui.KeyRight, ui.KeyHome, ui.KeyEnd, ui.KeyEnter, ui.KeyEscape:
			return false
		}
		if runtime.GOOS == "darwin" && mods&^ui.Shift == ui.Ctrl {
			switch key {
			case ui.KeyP, ui.KeyN, ui.KeyB, ui.KeyF:
				return false
			}
		}
	}

	mac := runtime.GOOS == "darwin"
	shift := mods&ui.Shift != 0
	m := mods &^ ui.Shift

	if mac && m == ui.Ctrl {
		switch key {
		case ui.KeyB:
			key, m = ui.KeyLeft, 0
		case ui.KeyF:
			key, m = ui.KeyRight, 0
		case ui.KeyP:
			key, m = ui.KeyUp, 0
		case ui.KeyN:
			key, m = ui.KeyDown, 0
		case ui.KeyH:
			key, m = ui.KeyBackspace, 0
		case ui.KeyD:
			key, m = ui.KeyDelete, 0
		case ui.KeyA:
			ls, _ := s.lineRuneEdges(s.caret)
			s.moveCaretTo(ls, ui.TextDownstream, shift)
			s.resetColumn()
			return true
		case ui.KeyE:
			_, le := s.lineRuneEdges(s.caret)
			s.moveCaretTo(le, ui.TextUpstream, shift)
			if !shift {
				if _, ok := s.codeRangeEndingAt(le); ok {
					s.setCodeEdge(RichCodeEdgeAfter)
				}
			}
			s.resetColumn()
			return true
		case ui.KeyK:
			if !s.readOnly {
				_, le := s.lineRuneEdges(s.caret)
				if le > s.caret {
					s.replaceRunes(s.caret, le, "")
				}
			}
			return true
		}
	}

	if !shift && m == ui.Cmd {
		switch key {
		case ui.KeyA:
			s.anchor = 0
			s.caret = utf8.RuneCountInString(s.text)
			s.affinity = ui.TextDownstream
			s.codeEdge = RichCodeEdgeNone
			s.resetColumn()
			if s.onSelect != nil {
				s.onSelect(s.anchor, s.caret)
			}
			return true
		case ui.KeyC:
			return s.handleCommand("copy")
		case ui.KeyX:
			return s.handleCommand("cut")
		case ui.KeyV:
			return s.handleCommand("paste")
		}
	}
	if (mods == (ui.Cmd|ui.Shift) || mods == (ui.Cmd|ui.Shift|ui.Alt)) && key == ui.KeyV {
		return s.handleCommand("pasteAndMatchStyle")
	}
	// Classic Windows/Linux CUA clipboard shortcuts.
	switch {
	case mods == ui.Ctrl && key == ui.KeyInsert:
		return s.handleCommand("copy")
	case mods == ui.Shift && key == ui.KeyInsert:
		return s.handleCommand("paste")
	case mods == ui.Shift && key == ui.KeyDelete:
		return s.handleCommand("cut")
	}

	word := (mac && m == ui.Alt) || (!mac && m == ui.Ctrl)
	lo, hi := s.OrderedSelection()
	rs := []rune(s.text)
	n := len(rs)

	switch key {
	case ui.KeyLeft, ui.KeyRight:
		left := key == ui.KeyLeft
		switch {
		case mac && m == ui.Super:
			ls, le := s.lineRuneEdges(s.caret)
			if left {
				s.moveCaretTo(ls, ui.TextDownstream, shift)
			} else {
				s.moveCaretTo(le, ui.TextUpstream, shift)
				if !shift {
					if _, ok := s.codeRangeEndingAt(le); ok {
						s.setCodeEdge(RichCodeEdgeAfter)
					}
				}
			}
		case word && left:
			s.moveCaretTo(prevWordOff(rs, s.caret), ui.TextDownstream, shift)
		case word:
			s.moveCaretTo(nextWordOff(rs, s.caret), ui.TextDownstream, shift)
		case m != 0:
			return false
		case lo != hi && !shift && left:
			s.moveCaretTo(lo, ui.TextDownstream, false)
		case lo != hi && !shift:
			s.moveCaretTo(hi, ui.TextDownstream, false)
		default:
			s.normalizeCodeEdge()
			if !shift {
				if !left {
					if _, ok := s.codeRangeStartingAt(s.caret); ok && s.codeEdge == RichCodeEdgeNone {
						s.setCodeEdge(RichCodeEdgeInsideStart)
						s.resetColumn()
						return true
					}
					if _, ok := s.codeRangeEndingAt(s.caret); ok && s.codeEdge == RichCodeEdgeNone {
						s.setCodeEdge(RichCodeEdgeAfter)
						s.resetColumn()
						return true
					}
				} else {
					if _, ok := s.codeRangeEndingAt(s.caret); ok && s.codeEdge == RichCodeEdgeAfter {
						s.setCodeEdge(RichCodeEdgeNone)
						s.resetColumn()
						return true
					}
					if _, ok := s.codeRangeStartingAt(s.caret); ok && s.codeEdge == RichCodeEdgeInsideStart {
						s.setCodeEdge(RichCodeEdgeNone)
						s.resetColumn()
						return true
					}
				}
			}
			prevCaret := s.caret
			lay := s.currentLayout()
			curPos := ui.TextCaretPosition{Index: runeToUTF16Off(s.text, s.caret), Affinity: s.affinity}
			dir := 1
			if left {
				dir = -1
			}
			next := lay.MoveCaret(curPos, dir)
			if next == curPos {
				if left && s.caret > 0 {
					s.moveCaretTo(s.caret-1, ui.TextDownstream, shift)
				} else if !left && s.caret < n {
					s.moveCaretTo(s.caret+1, ui.TextDownstream, shift)
				} else if s.emitEdge(dir, false, shift, 0) {
					return true
				}
			} else {
				s.moveCaretTo(utf16ToRuneOff(s.text, next.Index), next.Affinity, shift)
			}
			if !shift && left && s.caret < prevCaret {
				if cr, ok := s.codeRangeStartingAt(s.caret); ok && prevCaret > cr.Start && prevCaret <= cr.End {
					s.setCodeEdge(RichCodeEdgeInsideStart)
				} else if cr, ok := s.codeRangeEndingAt(s.caret); ok && prevCaret > cr.End {
					s.setCodeEdge(RichCodeEdgeAfter)
				}
			}
		}
		s.resetColumn()
		return true

	case ui.KeyUp, ui.KeyDown:
		if mac && m == ui.Super {
			if key == ui.KeyUp {
				s.moveCaretTo(0, ui.TextDownstream, shift)
			} else {
				s.moveCaretTo(n, ui.TextDownstream, shift)
				if !shift {
					if _, ok := s.codeRangeEndingAt(n); ok {
						s.setCodeEdge(RichCodeEdgeAfter)
					}
				}
			}
			s.resetColumn()
			return true
		}
		if m != 0 {
			return false
		}
		dir := 1
		if key == ui.KeyUp {
			dir = -1
		}
		return s.moveVertical(dir, shift)

	case ui.KeyHome:
		if m == ui.Ctrl {
			s.moveCaretTo(0, ui.TextDownstream, shift)
		} else {
			ls, _ := s.lineRuneEdges(s.caret)
			s.moveCaretTo(ls, ui.TextDownstream, shift)
		}
		s.resetColumn()
		return true

	case ui.KeyEnd:
		if m == ui.Ctrl {
			s.moveCaretTo(n, ui.TextDownstream, shift)
			if !shift {
				if _, ok := s.codeRangeEndingAt(n); ok {
					s.setCodeEdge(RichCodeEdgeAfter)
				}
			}
		} else {
			_, le := s.lineRuneEdges(s.caret)
			s.moveCaretTo(le, ui.TextUpstream, shift)
			if !shift {
				if _, ok := s.codeRangeEndingAt(le); ok {
					s.setCodeEdge(RichCodeEdgeAfter)
				}
			}
		}
		s.resetColumn()
		return true
	}

	if s.readOnly {
		return false
	}

	switch key {
	case ui.KeyBackspace:
		switch {
		case lo != hi:
			s.replaceRunes(lo, hi, "")
		case mac && m == ui.Super:
			ls, _ := s.lineRuneEdges(s.caret)
			if s.caret > ls {
				s.replaceRunes(ls, s.caret, "")
			}
		case word:
			pw := prevWordOff(rs, s.caret)
			if pw < s.caret {
				s.replaceRunes(pw, s.caret, "")
			}
		case m == 0 && s.caret > 0:
			lay := s.currentLayout()
			prevU := lay.PreviousBoundary(runeToUTF16Off(s.text, s.caret))
			prevR := utf16ToRuneOff(s.text, prevU)
			if prevR >= s.caret {
				prevR = s.caret - 1
			}
			s.replaceRunes(prevR, s.caret, "")
		default:
			return false
		}
		return true

	case ui.KeyDelete:
		switch {
		case lo != hi:
			s.replaceRunes(lo, hi, "")
		case word:
			nw := nextWordOff(rs, s.caret)
			if nw > s.caret {
				s.replaceRunes(s.caret, nw, "")
			}
		case m == 0 && s.caret < n:
			lay := s.currentLayout()
			nextU := lay.NextBoundary(runeToUTF16Off(s.text, s.caret))
			nextR := utf16ToRuneOff(s.text, nextU)
			if nextR <= s.caret {
				nextR = s.caret + 1
			}
			s.replaceRunes(s.caret, nextR, "")
		default:
			return false
		}
		return true
	}

	return false
}

func (s *RichInputState) handleInput(ev ui.InputEvent) bool {
	if s.pressLink != "" || s.pressLinkPending {
		switch ev.Kind {
		case ui.InputKeyDown, ui.InputKeyUp, ui.InputPointerMove, ui.InputScroll, ui.InputZoom:
			if ev.Mods != ui.Cmd {
				s.pressLink = ""
				s.pressLinkPending = false
			}
		case ui.InputText, ui.InputCompose, ui.InputCommand:
			s.pressLink = ""
			s.pressLinkPending = false
		}
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		return s.handleKey(ev.Key, ev.Mods)

	case ui.InputText:
		if s.readOnly || ev.Text == "" {
			return false
		}
		s.clearComposeState()
		lo, hi := s.OrderedSelection()
		if hi > lo && s.onText != nil && s.onText(ev.Text, lo, hi) {
			return true
		}
		s.replaceRunes(lo, hi, ev.Text)
		return true

	case ui.InputCommand:
		return s.handleCommand(ev.Text)

	case ui.InputSelect:
		n := utf8.RuneCountInString(s.text)
		s.anchor = clampRune(ev.Start, n)
		s.caret = clampRune(ev.End, n)
		s.affinity = ui.TextDownstream
		if s.onSelect != nil {
			s.onSelect(s.anchor, s.caret)
		}
		return true

	case ui.InputPointerDown:
		if ev.Button != 0 {
			if s.pressLinkPending {
				s.dragging = false
			}
			s.pressLink = ""
			s.pressLinkPending = false
			return false
		}
		s.pressPt = ui.Point{X: ev.X, Y: ev.Y}
		s.pressMods = ev.Mods
		s.pressLink = ""
		s.pressLinkPending = false
		if ev.Clicks == 1 && ev.Mods == ui.Cmd && s.onOpenLink != nil && !s.Composing() {
			s.pressLink = s.linkAtPoint(s.pressPt, true)
			if s.pressLink == "" && s.onResolveCmdLink != nil {
				s.pressLink = s.onResolveCmdLink(s.pressPt, s.currentLayout(), s.text)
			}
			if s.pressLink != "" {
				s.pressLinkPending = true
				s.dragging = true
				s.dragUnit = 1
				s.dragStart = [2]int{s.anchor, s.caret}
				return true
			}
		}
		if s.Composing() {
			s.UnmarkText()
		}
		lay := s.currentLayout()
		cp := lay.PositionAt(s.pressPt)
		pos := utf16ToRuneOff(s.text, cp.Index)
		s.dragging = true
		s.resetColumn()
		s.dragUnit = min(max(ev.Clicks, 1), 3)
		rs := []rune(s.text)
		switch s.dragUnit {
		case 1:
			s.moveCaretTo(pos, cp.Affinity, ev.Mods&ui.Shift != 0)
			if ev.Mods&ui.Shift == 0 && len(s.codeRanges) > 0 {
				if cr, ok := s.codeRangeEndingAt(pos); ok {
					if rects := resolveCodeChipRects(lay, lay.Lines(), s.displayText, s.font.Size, cr); len(rects) > 0 {
						last := rects[len(rects)-1]
						if s.pressPt.X >= last.X+last.W-cr.PadX*0.35 || s.pressPt.Y > last.Y+last.H {
							s.setCodeEdge(RichCodeEdgeAfter)
						}
					}
				} else if cr, ok := s.codeRangeStartingAt(pos); ok {
					if rects := resolveCodeChipRects(lay, lay.Lines(), s.displayText, s.font.Size, cr); len(rects) > 0 {
						first := rects[0]
						if s.pressPt.X >= first.X+cr.PadX*0.35 && s.pressPt.Y >= first.Y && s.pressPt.Y <= first.Y+first.H {
							s.setCodeEdge(RichCodeEdgeInsideStart)
						}
					}
				}
			}
		case 2:
			w0, w1 := richWordRangeAt(rs, pos)
			s.anchor, s.caret = w0, w1
			s.affinity = ui.TextDownstream
			s.codeEdge = RichCodeEdgeNone
			if s.onSelect != nil {
				s.onSelect(s.anchor, s.caret)
			}
		default:
			s.anchor, s.caret = 0, len(rs)
			s.affinity = ui.TextDownstream
			s.codeEdge = RichCodeEdgeNone
			if s.onSelect != nil {
				s.onSelect(s.anchor, s.caret)
			}
		}
		s.dragStart = [2]int{s.anchor, s.caret}
		return true

	case ui.InputPointerMove:
		if !s.dragging {
			return false
		}
		if s.pressLinkPending {
			dx, dy := ev.X-s.pressPt.X, ev.Y-s.pressPt.Y
			if dx*dx+dy*dy < 9 {
				return true
			}
			s.pressLink = ""
			s.pressLinkPending = false
			lay := s.currentLayout()
			startCP := lay.PositionAt(s.pressPt)
			startPos := utf16ToRuneOff(s.text, startCP.Index)
			s.resetColumn()
			s.moveCaretTo(startPos, startCP.Affinity, s.pressMods&ui.Shift != 0)
			s.dragStart = [2]int{s.anchor, s.caret}
		}
		lay := s.currentLayout()
		cp := lay.PositionAt(ui.Point{X: ev.X, Y: ev.Y})
		pos := utf16ToRuneOff(s.text, cp.Index)
		rs := []rune(s.text)
		switch s.dragUnit {
		case 2:
			w0, w1 := richWordRangeAt(rs, pos)
			if pos < s.dragStart[0] {
				s.anchor, s.caret = s.dragStart[1], w0
			} else {
				s.anchor, s.caret = s.dragStart[0], max(w1, s.dragStart[1])
			}
		case 3:
			if pos < s.dragStart[0] {
				s.anchor, s.caret = s.dragStart[1], 0
			} else {
				s.anchor, s.caret = 0, len(rs)
			}
		default:
			s.caret = pos
			s.affinity = cp.Affinity
		}
		if s.anchor != s.caret {
			s.codeEdge = RichCodeEdgeNone
		}
		dx, dy := ev.X-s.pressPt.X, ev.Y-s.pressPt.Y
		if dx*dx+dy*dy >= 9 {
			s.pressLink = ""
		}
		if s.onSelect != nil {
			s.onSelect(s.anchor, s.caret)
		}
		return true

	case ui.InputPointerUp:
		if ev.Button != 0 {
			s.pressLink = ""
			s.pressLinkPending = false
			return false
		}
		if !s.dragging {
			return false
		}
		s.dragging = false
		s.pressLinkPending = false
		link := s.pressLink
		s.pressLink = ""
		if link != "" && !ev.Canceled && ev.Mods == ui.Cmd && s.onOpenLink != nil {
			s.onOpenLink(link)
		}
		return true
	}
	return false
}

func caretBlinkAlpha(tMs float64) (alpha float32, waitMs int) {
	period := float64(richCaretOnMs + richCaretOffMs)
	m := tMs - period*float64(int64(tMs/period))
	if m < 0 {
		m += period
	}
	if m < richCaretOnMs {
		return 1, int(richCaretOnMs-m) + 1
	}
	return 0, int(period-m) + 1
}

func spanBoundaryPad(spans []ui.Span, runeOff int, atStart bool, defaultPad float32) float32 {
	if len(spans) == 0 {
		return defaultPad
	}
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		s0, s1 := pos, pos+n
		pos = s1
		if sp.PaddingX <= 0 {
			continue
		}
		if atStart && runeOff == s0 && n > 0 {
			return sp.PaddingX
		}
		if !atStart && runeOff == s1 && n > 0 {
			return sp.PaddingX
		}
		if runeOff > s0 && runeOff < s1 {
			return 0
		}
	}
	return defaultPad
}

func maxChipPadX(spans []ui.Span) float32 {
	var m float32
	for _, sp := range spans {
		if sp.PaddingX > m {
			m = sp.PaddingX
		}
	}
	return m
}

func resolveBandLineRects(lay *ui.TextLayout, text string, spans []ui.Span, bd RichBand) []ui.Rect {
	if lay == nil || bd.End <= bd.Start {
		return nil
	}
	u0 := runeToUTF16Off(text, bd.Start)
	u1 := runeToUTF16Off(text, bd.End)
	raw := lay.SelectionRects(ui.TextInputRange{Start: u0, End: u1})
	if len(raw) == 0 {
		return nil
	}
	chipPad := maxChipPadX(spans)
	maxJoinGap := max(chipPad*2+4, 2)
	merged := make([]ui.Rect, 0, len(raw))
	for _, sr := range raw {
		if n := len(merged); n > 0 {
			prev := &merged[n-1]
			if float32(math.Abs(float64(sr.Y-prev.Y))) < 0.5 && sr.X-(prev.X+prev.W) <= maxJoinGap && sr.X >= prev.X-0.5 {
				right := max(prev.X+prev.W, sr.X+sr.W)
				prev.X = min(prev.X, sr.X)
				prev.W = right - prev.X
				prev.H = max(prev.H, sr.H)
				continue
			}
		}
		merged = append(merged, sr)
	}
	startPad := spanBoundaryPad(spans, bd.Start, true, bd.PadX)
	endPad := spanBoundaryPad(spans, bd.End, false, bd.PadX)
	for i := range merged {
		lp := bd.PadX
		rp := bd.PadX
		if i == 0 {
			lp = startPad
		} else if chipPad > 0 && merged[i].X > 0 && merged[i].X <= chipPad+0.5 {
			lp = merged[i].X + bd.PadX
		}
		if i == len(merged)-1 {
			rp = endPad
		}
		merged[i].X -= lp
		merged[i].W += lp + rp
	}
	return merged
}

func resolveCodeChipRects(lay *ui.TextLayout, lines []ui.TextLine, text string, fontSize float32, cr RichCodeRange) []ui.Rect {
	if lay == nil || cr.End <= cr.Start {
		return nil
	}
	u0 := runeToUTF16Off(text, cr.Start)
	u1 := runeToUTF16Off(text, cr.End)
	raw := lay.SelectionRects(ui.TextInputRange{Start: u0, End: u1})
	if len(raw) == 0 {
		return nil
	}
	maxJoinGap := max(cr.PadX*2+4, 2)
	merged := make([]ui.Rect, 0, len(raw))
	for _, sr := range raw {
		if n := len(merged); n > 0 {
			prev := &merged[n-1]
			if float32(math.Abs(float64(sr.Y-prev.Y))) < 0.5 && sr.X-(prev.X+prev.W) <= maxJoinGap && sr.X >= prev.X-0.5 {
				right := max(prev.X+prev.W, sr.X+sr.W)
				prev.X = min(prev.X, sr.X)
				prev.W = right - prev.X
				prev.H = max(prev.H, sr.H)
				continue
			}
		}
		merged = append(merged, sr)
	}
	chipH := fontSize*1.32 + 2*cr.PadY
	for i, sr := range merged {
		chipY := sr.Y + (sr.H-chipH)*0.5
		for _, ln := range lines {
			if sr.Y+sr.H*0.5 >= ln.Rect.Y && sr.Y+sr.H*0.5 <= ln.Rect.Y+ln.Rect.H {
				chipY = ln.Baseline - fontSize*0.98 - cr.PadY
				break
			}
		}
		merged[i] = ui.Rect{
			X: sr.X - cr.PadX,
			Y: chipY,
			W: sr.W + 2*cr.PadX,
			H: chipH,
		}
	}
	return merged
}

func clearCodeRangeSpanBG(spans []ui.Span, codeRanges []RichCodeRange) []ui.Span {
	if len(spans) == 0 || len(codeRanges) == 0 {
		return spans
	}
	out := make([]ui.Span, len(spans))
	copy(out, spans)
	pos := 0
	for i := range out {
		n := utf8.RuneCountInString(out[i].Text)
		s0, s1 := pos, pos+n
		pos = s1
		if out[i].Background.A == 0 || n == 0 {
			continue
		}
		for _, cr := range codeRanges {
			if s1 > cr.Start && s0 < cr.End {
				out[i].Background = ui.Color{}
				break
			}
		}
	}
	return out
}

func shiftCodeRangesForCompose(codeRanges []RichCodeRange, replaceStart, replaceEnd, compLen int, codeEdge RichCodeEdge) []RichCodeRange {
	if len(codeRanges) == 0 || (compLen <= 0 && replaceEnd <= replaceStart) {
		return codeRanges
	}
	if replaceEnd < replaceStart {
		replaceEnd = replaceStart
	}
	delta := compLen - (replaceEnd - replaceStart)
	out := make([]RichCodeRange, 0, len(codeRanges))
	for _, cr := range codeRanges {
		if replaceEnd > replaceStart {
			switch {
			case cr.End <= replaceStart:
				out = append(out, cr)
			case cr.Start >= replaceEnd:
				cr.Start += delta
				cr.End += delta
				out = append(out, cr)
			default:
				ns := min(cr.Start, replaceStart)
				ne := max(cr.End+delta, replaceStart+compLen)
				if ne > ns {
					cr.Start = ns
					cr.End = ne
					out = append(out, cr)
				}
			}
			continue
		}
		caretRune := replaceStart
		switch {
		case caretRune < cr.Start:
			cr.Start += compLen
			cr.End += compLen
		case caretRune == cr.Start:
			if codeEdge == RichCodeEdgeInsideStart {
				cr.End += compLen
			} else {
				cr.Start += compLen
				cr.End += compLen
			}
		case caretRune < cr.End:
			cr.End += compLen
		case caretRune == cr.End:
			if codeEdge != RichCodeEdgeAfter {
				cr.End += compLen
			}
		}
		out = append(out, cr)
	}
	return out
}

// DrawRichDecorations paints inline code chips, highlight bands (behind text + subtle chip tint when needed),
// hover/caret link underlines, and the smooth blinking caret using the exact frame width r.W.
// Note: never call target.PointerPosition() here, because setting flagTrackPointer on an unfocused
// RichText child would mark it interactive and intercept canvas click/drag selection.
func DrawRichDecorations(c *ui.Context, target ui.Element, text string, layFn func(width float32) *ui.TextLayout, opts RichDecorOptions) {
	if len(opts.Bands) == 0 && len(opts.Links) == 0 && len(opts.CodeRanges) == 0 && opts.CaretPos < 0 {
		return
	}
	resolvePtr := func(r ui.Rect) (px, py float32, inside bool) {
		if opts.HasLocalPointer {
			return opts.LocalPointer.X, opts.LocalPointer.Y, true
		}
		if opts.HasPointer {
			lx := opts.PointerWin.X - r.X
			ly := opts.PointerWin.Y - r.Y
			if lx >= 0 && lx <= r.W && ly >= 0 && ly <= r.H {
				return lx, ly, true
			}
		}
		return 0, 0, false
	}
	scale := min(opts.FontSize/16, 1)
	chipTargetH := float32(math.Round(float64(opts.FontSize + 5*scale)))
	bandH := float32(math.Round(float64(opts.FontSize * 1.32)))

	if len(opts.CodeRanges) > 0 || len(opts.Bands) > 0 {
		target.Draw(func(p *ui.Painter, r ui.Rect) {
			if r.W <= 0 {
				return
			}
			lay := layFn(r.W)
			if lay == nil {
				return
			}
			if len(opts.CodeRanges) > 0 {
				px, py, inside := resolvePtr(r)
				lines := lay.Lines()
				hasSel := opts.SelRange[1] > opts.SelRange[0]
				selLo, selHi := opts.SelRange[0], opts.SelRange[1]
				for _, cr := range opts.CodeRanges {
					if cr.End <= cr.Start {
						continue
					}
					if hasSel && selLo <= cr.Start && selHi >= cr.End {
						continue
					}
					rects := resolveCodeChipRects(lay, lines, text, opts.FontSize, cr)
					if len(rects) == 0 {
						continue
					}
					caretInCode := false
					if !hasSel && opts.CaretPos >= 0 {
						switch {
						case opts.CaretPos > cr.Start && opts.CaretPos < cr.End:
							caretInCode = true
						case opts.CaretPos == cr.Start:
							caretInCode = opts.CaretCodeEdge == RichCodeEdgeInsideStart
						case opts.CaretPos == cr.End:
							caretInCode = opts.CaretCodeEdge != RichCodeEdgeAfter
						}
					}
					isEdit := (hasSel && selLo < cr.End && cr.Start < selHi) || caretInCode
					isHover := false
					if !isEdit && inside {
						for _, crRect := range rects {
							if px >= crRect.X && px <= crRect.X+crRect.W && py >= crRect.Y && py <= crRect.Y+crRect.H {
								isHover = true
								break
							}
						}
					}
					fill := opts.CodeBG
					if isEdit && opts.CodeEditBG.A > 0 {
						fill = opts.CodeEditBG
					} else if isHover && opts.CodeHoverBG.A > 0 {
						fill = opts.CodeHoverBG
					}
					if fill.A == 0 {
						continue
					}
					for _, crRect := range rects {
						p.Fill(ui.Rect{
							X: r.X + crRect.X,
							Y: r.Y + crRect.Y,
							W: crRect.W,
							H: crRect.H,
						}, fill, cr.Radius)
					}
				}
			}
			for _, bd := range opts.Bands {
				if bd.End <= bd.Start {
					continue
				}
				for _, sr := range resolveBandLineRects(lay, text, opts.Spans, bd) {
					h := min(bandH, sr.H)
					if bd.FullLineBand {
						h = max(sr.H-1, chipTargetH+2)
					}
					rr := ui.Rect{
						X: r.X + sr.X,
						Y: r.Y + sr.Y + (sr.H-h)*0.5,
						W: sr.W,
						H: h,
					}
					if bd.Fill.A > 0 {
						p.Fill(rr, bd.Fill, bd.Radius)
					}
					if bd.Underline.A > 0 {
						const underlineH float32 = 2
						p.Fill(ui.Rect{X: rr.X, Y: rr.Y + rr.H - underlineH, W: rr.W, H: underlineH}, bd.Underline, underlineH*0.5)
					}
					if bd.Ring.A > 0 {
						p.Stroke(rr, bd.Ring, bd.Radius, 1)
					}
				}
			}
		})
	}

	needOver := len(opts.Links) > 0 || opts.CaretPos >= 0
	if !needOver && len(opts.OpaqueRanges) > 0 {
		for _, bd := range opts.Bands {
			if bd.TintOver && bd.Fill.A > 0 && bd.End > bd.Start {
				needOver = true
				break
			}
		}
	}
	if !needOver {
		return
	}

	target.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 {
			return
		}
		lay := layFn(r.W)
		if lay == nil {
			return
		}
		px, py, inside := resolvePtr(r)
		lines := lay.Lines()

		if len(opts.OpaqueRanges) > 0 {
			for _, bd := range opts.Bands {
				if !bd.TintOver || bd.Fill.A == 0 || bd.End <= bd.Start {
					continue
				}
				tint := bd.Fill.Alpha(min((float32(bd.Fill.A)/255.0)*0.45, 0.36))
				for _, op := range opts.OpaqueRanges {
					lo := max(bd.Start, op[0])
					hi := min(bd.End, op[1])
					if hi <= lo {
						continue
					}
					sub := RichBand{Start: lo, End: hi, Radius: min(bd.Radius, 4), FullLineBand: bd.FullLineBand}
					for _, sr := range resolveBandLineRects(lay, text, opts.Spans, sub) {
						h := min(bandH, sr.H)
						if bd.FullLineBand {
							h = max(sr.H-1, chipTargetH+2)
						}
						rr := ui.Rect{
							X: r.X + sr.X,
							Y: r.Y + sr.Y + (sr.H-h)*0.5,
							W: sr.W,
							H: h,
						}
						p.Fill(rr, tint, sub.Radius)
						if bd.Underline.A > 0 {
							const underlineH float32 = 2
							p.Fill(ui.Rect{X: rr.X, Y: rr.Y + rr.H - underlineH, W: rr.W, H: underlineH}, bd.Underline, underlineH*0.5)
						}
					}
				}
			}
		}

		for _, lr := range opts.Links {
			if lr.End <= lr.Start {
				continue
			}
			if lr.ModifierOnly && !opts.CmdActive {
				continue
			}
			rects := RichLinkRects(lay, text, lr)
			show := !lr.ModifierOnly && opts.CaretPos >= 0 && opts.CaretPos >= lr.Start && opts.CaretPos <= lr.End
			if !show && inside && r.W > 0 {
				for _, sr := range rects {
					if px >= sr.X && px <= sr.X+sr.W && py >= sr.Y && py <= sr.Y+sr.H {
						show = true
						break
					}
				}
			}
			if !show {
				continue
			}
			for _, sr := range rects {
				baseline := sr.Y + sr.H*0.8
				for _, ln := range lines {
					if sr.Y+sr.H*0.5 >= ln.Rect.Y && sr.Y+sr.H*0.5 < ln.Rect.Y+ln.Rect.H {
						baseline = ln.Baseline
						break
					}
				}
				p.Fill(ui.Rect{X: r.X + sr.X, Y: r.Y + baseline + 3, W: sr.W, H: 1}, opts.LinkColor, 0)
			}
		}

		if opts.CaretPos >= 0 && opts.CaretAlpha > 0 {
			u := runeToUTF16Off(text, opts.CaretPos)
			cr := lay.CaretAt(ui.TextCaretPosition{Index: u, Affinity: opts.CaretAff})
			h := float32(math.Round(float64(opts.FontSize * 1.125)))
			caretYForMid := func(midY, fallbackY, fallbackH float32) float32 {
				y := fallbackY + (fallbackH-h)*0.5
				for _, ln := range lines {
					if midY >= ln.Rect.Y && midY <= ln.Rect.Y+ln.Rect.H {
						ty := ln.Baseline - 0.95*opts.FontSize
						hiY := max(ln.Rect.Y+ln.Rect.H-h, ln.Rect.Y)
						return min(max(ty, ln.Rect.Y), hiY)
					}
				}
				return y
			}
			caretY := caretYForMid(cr.Y+cr.H*0.5, cr.Y, cr.H)
			caretX := cr.X - 1
			if opts.CaretPos > 0 && cr.X > 1 {
				caretX = cr.X - 0.5
			}
			for _, codeRange := range opts.CodeRanges {
				if codeRange.End <= codeRange.Start {
					continue
				}
				if opts.CaretPos == codeRange.End {
					if rects := resolveCodeChipRects(lay, lines, text, opts.FontSize, codeRange); len(rects) > 0 {
						last := rects[len(rects)-1]
						if opts.CaretCodeEdge == RichCodeEdgeAfter {
							caretX = last.X + last.W + 0.5
						} else {
							caretX = last.X + last.W - codeRange.PadX - 0.5
						}
						caretY = caretYForMid(last.Y+last.H*0.5, last.Y, last.H)
					}
					break
				}
				if opts.CaretPos == codeRange.Start {
					if rects := resolveCodeChipRects(lay, lines, text, opts.FontSize, codeRange); len(rects) > 0 {
						first := rects[0]
						if opts.CaretCodeEdge == RichCodeEdgeInsideStart {
							caretX = first.X + codeRange.PadX - 1
						} else {
							caretX = first.X - 2.5
							if first.X < 1.5 {
								caretX = first.X - 1
							}
						}
						caretY = caretYForMid(first.Y+first.H*0.5, first.Y, first.H)
					}
					break
				}
			}
			col := opts.CaretColor.Alpha(opts.CaretAlpha)
			p.Fill(ui.Rect{X: r.X + caretX, Y: r.Y + caretY, W: 2, H: h}, col, 1)
		}
	})
}

// RichInput builds a focusable rich-text block input backed by the exact same ui.RichText
// pipeline as unfocused blocks so clicking/editing never shifts a single glyph or line height.
func RichInput(c *ui.Context, st *RichInputState, opts RichInputOptions) ui.Element {
	st.services = c.Services()
	st.readOnly = opts.ReadOnly
	st.suppressNavKeys = opts.SuppressNavKeys
	st.links = opts.Links
	st.codeRanges = opts.CodeRanges
	st.onChange = opts.OnChange
	st.onSelect = opts.OnSelect
	st.onCopy = opts.OnCopy
	st.onCut = opts.OnCut
	st.onPaste = opts.OnPaste
	st.onPastePlain = opts.OnPastePlain
	st.onKey = opts.OnKey
	st.onText = opts.OnText
	st.onEdge = opts.OnEdge
	st.onOpenLink = opts.OnOpenLink
	st.onResolveCmdLink = opts.OnResolveCmdLink
	if (st.pressLink != "" || st.pressLinkPending) && (!c.WindowActive() || c.Modifiers() != ui.Cmd) {
		st.pressLink = ""
		st.pressLinkPending = false
	}

	n := utf8.RuneCountInString(opts.Text)
	if !st.Composing() && st.text != opts.Text {
		st.text = opts.Text
		st.anchor = clampRune(st.anchor, n)
		st.caret = clampRune(st.caret, n)
	}
	if opts.SyncSelection && !st.Composing() {
		st.anchor = clampRune(opts.Anchor, n)
		st.caret = clampRune(opts.Caret, n)
		st.affinity = ui.TextDownstream
		st.codeEdge = RichCodeEdgeNone
		st.resetColumn()
		if opts.CarryColumn {
			st.carryWinX, st.hasCarry = opts.CarryColumnX, true
		}
	} else {
		st.anchor = clampRune(st.anchor, n)
		st.caret = clampRune(st.caret, n)
	}
	st.normalizeCodeEdge()

	dispSpans := opts.Spans
	if len(dispSpans) == 0 && opts.Text != "" {
		dispSpans = []ui.Span{{Text: opts.Text}}
	}
	if len(opts.CodeRanges) > 0 && len(dispSpans) > 0 {
		dispSpans = clearCodeRangeSpanBG(dispSpans, opts.CodeRanges)
	}
	codeRanges := opts.CodeRanges
	if st.compose != "" {
		compA, compZ := st.composeBaseBounds(n)
		dispSpans = insertComposeIntoSpans(dispSpans, opts.Text, compA, compZ, st.compose, opts.CodeRanges, st.codeEdge)
		codeRanges = shiftCodeRangesForCompose(codeRanges, compA, compZ, utf8.RuneCountInString(st.compose), st.codeEdge)
	}

	fam := opts.Font
	if fam == "" {
		fam = c.Theme().Font
	}
	st.font = ui.Font{
		Family:     fam,
		Size:       opts.FontSize,
		Weight:     opts.FontWeight,
		LineHeight: opts.LineHeight,
	}
	st.align = opts.TextAlign
	st.displaySpans = dispSpans
	st.displayText = st.virtualString()

	box := ui.Box(c.Key(opts.Key)).FillWidth().Role(ui.RoleTextField)
	if !opts.ReadOnly {
		box.HandleTextInput(st).Cursor(ui.CursorText)
	} else {
		box.Focusable().ReadOnly(true)
	}
	box.HandleInput(st.handleInput)
	box.ContextMenu(func(m *ui.Menu) {
		if !opts.ReadOnly {
			m.EditItems()
			if opts.OnContextMenu != nil {
				opts.OnContextMenu(m)
			}
			return
		}
		lo, hi := st.OrderedSelection()
		if m.Item(theme.Tr("拷贝", "Copy")).Disabled(hi <= lo).Chosen() {
			st.handleCommand("copy")
		}
		m.Separator()
		if m.Item(theme.Tr("全选", "Select All")).Disabled(st.text == "").Chosen() {
			st.handleCommand("selectAll")
		}
		if opts.OnContextMenu != nil {
			opts.OnContextMenu(m)
		}
	})

	bands := opts.Bands
	selRange := opts.SelRange
	lo, hi := st.OrderedSelection()
	if !opts.SuppressSelection && !st.Composing() && hi > lo {
		selRange = [2]int{lo, hi}
		bands = append(bands[:len(bands):len(bands)], RichBand{
			Start:        lo,
			End:          hi,
			Fill:         opts.SelectionBG,
			Radius:       theme.RadiusXS,
			PadX:         3,
			FullLineBand: true,
			TintOver:     true,
		})
	}

	caretPos := -1
	var caretAlpha float32
	isFocused := box.Focused() || opts.FocusPending
	if !opts.ReadOnly && !opts.HideCaret && !opts.SuppressSelection && (lo == hi || st.Composing()) && isFocused && c.WindowActive() {
		now := c.Now()
		visCaret := st.caret
		if st.compose != "" {
			compA, _ := st.composeBaseBounds(n)
			visCaret = compA + st.composeCaret
		}
		if st.blinkPos != visCaret || st.blinkEpoch.IsZero() {
			st.blinkPos = visCaret
			st.blinkEpoch = now
		}
		alpha, wait := caretBlinkAlpha(float64(now.Sub(st.blinkEpoch).Milliseconds()))
		c.After(time.Duration(wait) * time.Millisecond)
		caretPos = visCaret
		caretAlpha = alpha
	}

	cmdActive := c.WindowActive() && c.Modifiers() == ui.Cmd
	var localPtr ui.Point
	var hasLocalPtr bool
	if len(codeRanges) > 0 || len(opts.Links) > 0 {
		lx, ly, over := box.PointerPosition()
		if over {
			localPtr = ui.Point{X: lx, Y: ly}
			hasLocalPtr = true
		}
	}
	if hasLocalPtr && opts.OnOpenLink != nil && cmdActive && st.linkAtPoint(localPtr, true) != "" {
		box.Cursor(ui.CursorPointer)
	}

	box.Children(func() {
		var rt ui.Element
		if len(dispSpans) == 0 {
			rt = ui.Text(c.Key("rt-empty"), "").PreserveSpaces().
				FontSize(opts.FontSize).FontWeight(opts.FontWeight).LineHeight(opts.LineHeight).
				MinHeight(opts.MinHeight).TextColor(opts.TextColor).FillWidth().AccessHidden(true)
		} else {
			rt = ui.RichText(c.Key("rt"), dispSpans...).PreserveSpaces().
				FontSize(opts.FontSize).FontWeight(opts.FontWeight).LineHeight(opts.LineHeight).
				MinHeight(opts.MinHeight).TextColor(opts.TextColor).FillWidth().AccessHidden(true)
		}
		if opts.Font != "" {
			rt.Font(opts.Font)
		}
		if opts.TextAlign != ui.Start {
			rt.TextAlign(opts.TextAlign)
		}
		DrawRichDecorations(c, rt, st.displayText, st.ensureLayout, RichDecorOptions{
			FontSize:        opts.FontSize,
			Spans:           dispSpans,
			Bands:           bands,
			Links:           opts.Links,
			CodeRanges:      codeRanges,
			CodeBG:          opts.CodeBG,
			CodeHoverBG:     opts.CodeHoverBG,
			CodeEditBG:      opts.CodeEditBG,
			SelRange:        selRange,
			OpaqueRanges:    opts.OpaqueRanges,
			LinkColor:       opts.LinkColor,
			CaretPos:        caretPos,
			CaretAff:        st.affinity,
			CaretCodeEdge:   st.codeEdge,
			CaretColor:      opts.CaretColor,
			CaretAlpha:      caretAlpha,
			HasChipBG:       opts.HasChipBG,
			LocalPointer:    localPtr,
			HasLocalPointer: hasLocalPtr,
			CmdActive:       cmdActive,
		})
	})

	box.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		st.lastRect = r
		if opts.OnBounds != nil {
			opts.OnBounds(r)
		}
	})

	return box
}
