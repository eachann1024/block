package ui

import (
	"math"
	"sync"
	"testing"
	"time"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// SwitchSize selects the track and knob geometry of a Pluck Switch.
type SwitchSize uint8

const (
	// SwitchStandard is the default 38x22 switch used across settings rows,
	// dialogs, AI settings, export options, and plugin-declared controls.
	SwitchStandard SwitchSize = iota
	// SwitchCompact is the 34x20 switch used in compact chrome such as the
	// global plugin settings titlebar.
	SwitchCompact
	// SwitchHero is the 52x30 master switch (with a 44px hit height) used for
	// primary toggles such as plugin page headers.
	SwitchHero
)

// SwitchPhase communicates asynchronous transition feedback on a switch.
type SwitchPhase uint8

const (
	// SwitchIdle is the resting enabled or disabled state.
	SwitchIdle SwitchPhase = iota
	// SwitchBusy indicates an in-flight enable/disable/startup transition:
	// the track fill dims to 45%, a rotating accent arc renders inside the
	// knob, and clicks are ignored without dimming the entire control.
	SwitchBusy
	// SwitchError highlights a failed toggle with a danger inner ring, a
	// one-shot horizontal shake, and a danger pluck on any paired divider.
	SwitchError
)

// SwitchOptions configures SwitchWithOptions. Zero values give a ready-to-use
// standard switch; Loading and Error act as ergonomic aliases for Phase so
// callers never misconfigure busy or failed states.
type SwitchOptions struct {
	ID       string
	Label    string
	Value    *bool
	Size        SwitchSize
	Phase       SwitchPhase
	Loading     bool   // alias for Phase = SwitchBusy
	Error       bool   // alias for Phase = SwitchError
	ErrorKey    string // optional error identity; a new non-empty value retriggers the shake & fail pluck
	AsyncEnable bool   // true when turning on starts async work: enters SwitchBusy immediately and plucks upon completion
	Disabled    bool
	PluckKey    string // optional key shared with PluckDivider to emit wave ripples below the switch
	Tooltip     string // optional hover tooltip
}

type pluckKind uint8

const (
	pluckNone pluckKind = iota
	pluckOn
	pluckOff
	pluckFail
)

type scalarTween struct {
	from, to float32
	start    time.Time
	dur      time.Duration
	active   bool
}

func (tw *scalarTween) set(now time.Time, cur, target float32, dur time.Duration, instant bool) {
	if instant || dur <= 0 || almostZero(cur-target, 0.0005) {
		tw.from = target
		tw.to = target
		tw.active = false
		return
	}
	if tw.active && almostZero(tw.to-target, 0.0005) {
		return
	}
	tw.from = cur
	tw.to = target
	tw.start = now
	tw.dur = dur
	tw.active = true
}

func (tw *scalarTween) sample(now time.Time, ease func(float32) float32) (float32, bool) {
	if !tw.active {
		return tw.to, false
	}
	elapsed := now.Sub(tw.start)
	if elapsed >= tw.dur {
		tw.active = false
		tw.from = tw.to
		return tw.to, false
	}
	if elapsed <= 0 {
		return tw.from, true
	}
	u := float32(elapsed) / float32(tw.dur)
	if ease != nil {
		u = ease(u)
	}
	return tw.from + (tw.to-tw.from)*u, true
}

type switchState struct {
	initialized  bool
	lastOn       bool
	lastPhase    SwitchPhase
	lastPressed  bool
	lastErrorKey string

	knobPos   scalarTween // 0 = off (left), 1 = on (right)
	fillW     scalarTween // 0 = collapsed to trackH, 1 = full trackW
	fillAlpha scalarTween // 0 = hidden, 0.45 = busy, 1 = on
	stretch   scalarTween // 0 = resting circle, 1 = pressed stretch

	shakeStart  time.Time
	shakeActive bool
	spinStart   time.Time
	lastSeen    time.Time
}

type pluckChannel struct {
	kind     pluckKind
	start    time.Time
	anchorX  float32
	hasX     bool
	lastSeen time.Time
}

var (
	switchMu       sync.Mutex
	switchStates   = make(map[string]*switchState)
	pluckChannels  = make(map[string]*pluckChannel)
	switchPruneSeq int
)

const (
	switchKnobDur  = 180 * time.Millisecond
	switchFillDur  = 180 * time.Millisecond
	switchFadeDur  = 100 * time.Millisecond
	switchPressDur = 100 * time.Millisecond
	switchShakeDur = 300 * time.Millisecond
	switchSpinDur  = 700 * time.Millisecond
)

func easeSwitchKnob(u float32) float32 {
	return cubicBezierY(u, 0.3, 1.3, 0.5, 1.0)
}

func easeSwitchFill(u float32) float32 {
	return cubicBezierY(u, 0.2, 0.8, 0.2, 1.0)
}

func easeSwitchLinear(u float32) float32 {
	return cubicBezierY(u, 0.25, 0.1, 0.25, 1.0)
}

// cubicBezierY evaluates the Y coordinate of a CSS cubic-bezier(x1,y1,x2,y2) curve at progress x in [0,1].
func cubicBezierY(x, x1, y1, x2, y2 float32) float32 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	t := x
	for range 6 {
		inv := 1 - t
		bx := 3*inv*inv*t*x1 + 3*inv*t*t*x2 + t*t*t
		dx := 3*inv*inv*x1 + 6*inv*t*(x2-x1) + 3*t*t*(1-x2)
		if math.Abs(float64(dx)) < 1e-5 {
			break
		}
		t -= (bx - x) / dx
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	inv := 1 - t
	return 3*inv*inv*t*y1 + 3*inv*t*t*y2 + t*t*t
}

func pruneSwitchMapsLocked(now time.Time) {
	switchPruneSeq++
	if switchPruneSeq%128 != 0 {
		return
	}
	const ttl = 60 * time.Second
	if len(switchStates) > 128 {
		for k, st := range switchStates {
			if now.Sub(st.lastSeen) > ttl && !st.knobPos.active && !st.shakeActive {
				delete(switchStates, k)
			}
		}
	}
	if len(pluckChannels) > 128 {
		for k, ch := range pluckChannels {
			if now.Sub(ch.lastSeen) > ttl && ch.kind == pluckNone {
				delete(pluckChannels, k)
			}
		}
	}
}

func resolveSwitchPhase(opts SwitchOptions) SwitchPhase {
	switch {
	case opts.Phase == SwitchBusy || opts.Loading:
		return SwitchBusy
	case opts.Phase == SwitchError || opts.Error:
		return SwitchError
	default:
		return SwitchIdle
	}
}

func switchMetrics(size SwitchSize) (trackW, trackH, knobSize, pad, stretch, hitH float32) {
	switch size {
	case SwitchCompact:
		return theme.SwitchCompactTrackW, theme.SwitchCompactTrackH, theme.SwitchCompactKnobSize,
			theme.SwitchCompactPad, theme.SwitchCompactPressStretch, theme.SwitchCompactTrackH
	case SwitchHero:
		return theme.SwitchHeroTrackW, theme.SwitchHeroTrackH, theme.SwitchHeroKnobSize,
			theme.SwitchHeroPad, theme.SwitchHeroPressStretch, theme.SwitchHeroHitH
	default:
		return theme.SwitchTrackW, theme.SwitchTrackH, theme.SwitchKnobSize,
			theme.SwitchPad, theme.SwitchPressStretch, theme.SwitchTrackH
	}
}

func fillAlphaTarget(on bool, phase SwitchPhase) float32 {
	if !on {
		return 0
	}
	if phase == SwitchBusy {
		return 0.45
	}
	return 1
}

// Switch renders the global Block Pluck Switch at compact size.
func Switch(c *ui.Context, id, label string, value *bool) bool {
	return SwitchWithOptions(c, SwitchOptions{
		ID:    id,
		Label: label,
		Value: value,
		Size:  SwitchCompact,
	})
}

// SwitchWithOptions renders the global Block Pluck Switch with full state
// coverage: off/on, hover, directional press stretch, busy spinner, error ring
// and shake, disabled (both on & off), keyboard focus ring, reduced motion,
// and optional PluckDivider ripple coordination.
func SwitchWithOptions(c *ui.Context, opts SwitchOptions) bool {
	pal := theme.Ocean()
	var fallback bool
	valPtr := opts.Value
	if valPtr == nil {
		valPtr = &fallback
		opts.Disabled = true
	}
	prev := *valPtr
	phase := resolveSwitchPhase(opts)
	isBusy := phase == SwitchBusy
	isDisabled := opts.Disabled
	blocked := isDisabled || isBusy

	if opts.ID != "" {
		c.Key(opts.ID)
	}

	trackW, trackH, knobSize, pad, maxStretch, hitH := switchMetrics(opts.Size)
	cursor := ui.CursorPointer
	if blocked {
		cursor = ui.CursorDefault
	}

	sw := ui.SwitchBase(c, valPtr).Size(trackW, hitH).Shrink(0).
		Cursor(cursor).Disabled(blocked).DimDisabled(false).Label(opts.Label)

	// Guard against any value mutation while busy or disabled.
	if blocked && *valPtr != prev {
		*valPtr = prev
	}

	curOn := *valPtr
	changed := curOn != prev
	if changed && curOn && opts.AsyncEnable {
		phase = SwitchBusy
		isBusy = true
	}
	hovered := sw.Hovered() && !blocked
	pressed := sw.Pressed() && !blocked
	focused := sw.Focused() && !isDisabled

	if isDisabled {
		sw.Opacity(theme.DisabledOpacity)
	}
	if opts.Tooltip != "" {
		Tooltip(c, sw, opts.Tooltip)
	}

	stateKey := opts.ID
	if stateKey == "" {
		stateKey = opts.PluckKey
	}
	if stateKey == "" && opts.Label != "" {
		stateKey = "anon:" + opts.Label
	}
	reduceMotion := testing.Testing() || c.Preferences().ReduceMotion
	now := time.Now()
	if stateKey != "" {
		updateSwitchState(stateKey, opts.PluckKey, curOn, phase, pressed, opts.ErrorKey, now, reduceMotion)
	}

	sw.Draw(func(p *ui.Painter, bounds ui.Rect) {
		pNow := p.Now()
		if pNow.IsZero() {
			pNow = time.Now()
		}
		rm := testing.Testing() || p.Preferences().ReduceMotion

		pos := float32(0)
		fillRatio := float32(0)
		fillA := fillAlphaTarget(curOn, phase)
		stretchAmt := float32(0)
		shakeX := float32(0)
		animating := false
		var spinAngle float32

		if stateKey != "" {
			pos, fillRatio, fillA, stretchAmt, shakeX, spinAngle, animating = sampleSwitchState(stateKey, curOn, phase, pressed, pNow, rm)
		} else {
			if curOn {
				pos = 1
				fillRatio = 1
			}
			if pressed {
				stretchAmt = 1
			}
		}

		// Center the visual track vertically inside the hit box (e.g. 30px track in 44px hero hit box).
		trackY := bounds.Y + (bounds.H-trackH)*0.5
		trackRect := ui.Rect{
			X: bounds.X + shakeX,
			Y: trackY,
			W: trackW,
			H: trackH,
		}
		trackR := trackH * 0.5

		// Report real switch center X so any paired PluckDivider starts right below this switch.
		if opts.PluckKey != "" {
			recordPluckAnchor(opts.PluckKey, bounds.X+bounds.W*0.5, pNow)
		}

		// Keyboard focus ring (outside the track with a 2px gap).
		if focused {
			ringRect := ui.Rect{
				X: trackRect.X - 3,
				Y: trackRect.Y - 3,
				W: trackRect.W + 6,
				H: trackRect.H + 6,
			}
			p.Stroke(ringRect, pal.Accent, trackR+3, 2)
		}

		// Base track surface: quiet grey when off (design V4 方案 C).
		trackCol := pal.SwitchTrackOff
		switch {
		case pressed:
			trackCol = pal.SwitchTrackPressed
		case hovered:
			trackCol = pal.SwitchTrackHover
		}
		p.Fill(trackRect, trackCol, trackR)
		// Hero switches sit on the shell titlebar, where the quiet off track blends in:
		// outline it so the off state still reads as a control.
		if opts.Size == SwitchHero && fillA < 0.5 {
			p.Stroke(ui.Rect{X: trackRect.X + 0.5, Y: trackRect.Y + 0.5, W: trackRect.W - 1, H: trackRect.H - 1},
				pal.BtnBorder, trackR-0.5, 1)
		}

		// Animated light-accent fill sweeping from left (trackH -> trackW).
		if fillA > 0.005 {
			fillAlpha := theme.SwitchOnTrackAlpha
			if !isBusy {
				switch {
				case pressed:
					fillAlpha = theme.SwitchOnTrackPressedAlpha
				case hovered:
					fillAlpha = theme.SwitchOnTrackHoverAlpha
				}
			}
			fillW := trackH + (trackW-trackH)*clamp01(fillRatio)
			p.Clip(trackRect, trackR, func() {
				fillRect := ui.Rect{
					X: trackRect.X,
					Y: trackRect.Y,
					W: fillW,
					H: trackRect.H,
				}
				p.Fill(fillRect, pal.Accent.Alpha(fillAlpha*clamp01(fillA)), trackR)
			})
		}

		// Failed state inner danger ring (1.5px inset).
		if phase == SwitchError {
			errRect := ui.Rect{
				X: trackRect.X + 0.75,
				Y: trackRect.Y + 0.75,
				W: trackRect.W - 1.5,
				H: trackRect.H - 1.5,
			}
			p.Stroke(errRect, pal.Danger, max(trackR-0.75, 0), 1.5)
		}

		// Knob geometry: the knob grows from the small off size to the on size as
		// it travels; directional press stretch keeps the leading edge anchored.
		kPos := clamp01(pos)
		knobH := theme.SwitchKnobOffSize + (knobSize-theme.SwitchKnobOffSize)*kPos
		inset := pad + (knobSize-knobH)*0.5
		travel := trackW - knobH - inset*2
		extraW := maxStretch * clamp01(stretchAmt)
		knobW := knobH + extraW
		knobX := trackRect.X + inset + travel*pos - extraW*kPos
		maxOver := pad * 0.6
		minKnobX := trackRect.X + inset
		maxKnobX := trackRect.X + trackW - inset - knobW
		if knobX < minKnobX-maxOver {
			knobX = minKnobX - maxOver
		} else if knobX > maxKnobX+maxOver {
			knobX = maxKnobX + maxOver
		}
		knobRect := ui.Rect{
			X: knobX,
			Y: trackRect.Y + (trackH-knobH)*0.5,
			W: knobW,
			H: knobH,
		}
		knobR := knobH * 0.5

		// Knob colour follows the fill: grey off, accent on. No shadow, no border.
		knobCol := pal.SwitchKnobOff.Mix(pal.Accent, clamp01(fillRatio)*clamp01(fillA/fillAlphaTarget(true, SwitchIdle)))
		p.Fill(knobRect, knobCol, knobR)

		// Busy spinner arc centered inside the knob.
		if isBusy {
			drawKnobSpinner(p, knobRect, pal.AccentText, spinAngle, rm)
		}

		if animating && !rm {
			p.AnimationFrame()
		}
	})

	if opts.ID != "" {
		Probe(sw, opts.ID)
	}
	return changed
}

func drawKnobSpinner(p *ui.Painter, knob ui.Rect, col ui.Color, angle float32, reduced bool) {
	cx := knob.X + knob.W*0.5
	cy := knob.Y + knob.H*0.5
	r := min(knob.W, knob.H) * 0.27
	if r < 2 {
		return
	}
	if reduced {
		// Static dotted arc when motion is reduced.
		angle = -math.Pi * 0.5
	}
	const segs = 18
	const sweep = float32(math.Pi * 1.15)
	var path ui.Path
	path.Cap(ui.RoundCap).Join(ui.RoundJoin)
	for i := 0; i <= segs; i++ {
		a := angle + sweep*float32(i)/segs
		x := cx + r*float32(math.Cos(float64(a)))
		y := cy + r*float32(math.Sin(float64(a)))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	strokeW := float32(1.5)
	if knob.H >= theme.SwitchHeroKnobSize {
		strokeW = 1.8
	}
	p.StrokePath(&path, strokeW, col)
}

func updateSwitchState(key, pluckKey string, on bool, phase SwitchPhase, pressed bool, errorKey string, now time.Time, instant bool) {
	switchMu.Lock()
	defer switchMu.Unlock()

	pruneSwitchMapsLocked(now)
	st, ok := switchStates[key]
	if !ok {
		st = &switchState{}
		switchStates[key] = st
	}
	st.lastSeen = now

	targetPos := float32(0)
	targetFillW := float32(0)
	if on {
		targetPos = 1
		targetFillW = 1
	}
	targetFillA := fillAlphaTarget(on, phase)
	targetStretch := float32(0)
	if pressed {
		targetStretch = 1
	}

	if !st.initialized {
		st.initialized = true
		st.lastOn = on
		st.lastPhase = phase
		st.lastPressed = pressed
		st.lastErrorKey = errorKey
		st.knobPos = scalarTween{from: targetPos, to: targetPos}
		st.fillW = scalarTween{from: targetFillW, to: targetFillW}
		st.fillAlpha = scalarTween{from: targetFillA, to: targetFillA}
		st.stretch = scalarTween{from: targetStretch, to: targetStretch}
		if phase == SwitchBusy {
			st.spinStart = now
		}
		return
	}

	curPos, _ := st.knobPos.sample(now, easeSwitchKnob)
	curFillW, _ := st.fillW.sample(now, easeSwitchFill)
	curFillA, _ := st.fillAlpha.sample(now, easeSwitchLinear)
	curStretch, _ := st.stretch.sample(now, easeSwitchLinear)

	onChanged := on != st.lastOn
	phaseChanged := phase != st.lastPhase
	errorRetrigger := phase == SwitchError && errorKey != "" && errorKey != st.lastErrorKey

	if onChanged {
		st.knobPos.set(now, curPos, targetPos, switchKnobDur, instant)
		st.fillW.set(now, curFillW, targetFillW, switchFillDur, instant)
	}
	if onChanged || phaseChanged {
		st.fillAlpha.set(now, curFillA, targetFillA, switchFadeDur, instant)
	}
	if pressed != st.lastPressed {
		st.stretch.set(now, curStretch, targetStretch, switchPressDur, instant)
	}
	if phase == SwitchBusy && st.lastPhase != SwitchBusy {
		st.spinStart = now
	}

	// Trigger failure shake & fail pluck exactly once when entering error state.
	if (phase == SwitchError && st.lastPhase != SwitchError) || errorRetrigger {
		if !instant {
			st.shakeStart = now
			st.shakeActive = true
		}
		if pluckKey != "" && !instant {
			emitPluckLocked(pluckKey, pluckFail, now)
		}
	} else if phase != SwitchError {
		st.shakeActive = false
	}

	// Trigger on/off pluck:
	// - Off: immediate soft pluck.
	// - On + Idle: immediate accent pluck.
	// - Busy -> Idle while On: accent pluck upon startup completion.
	if pluckKey != "" && !instant {
		switch {
		case onChanged && !on && phase != SwitchError:
			emitPluckLocked(pluckKey, pluckOff, now)
		case onChanged && on && phase == SwitchIdle:
			emitPluckLocked(pluckKey, pluckOn, now)
		case !onChanged && on && st.lastPhase == SwitchBusy && phase == SwitchIdle:
			emitPluckLocked(pluckKey, pluckOn, now)
		}
	}

	st.lastOn = on
	st.lastPhase = phase
	st.lastPressed = pressed
	st.lastErrorKey = errorKey
}

func sampleSwitchState(key string, on bool, phase SwitchPhase, pressed bool, now time.Time, reduced bool) (pos, fillRatio, fillA, stretchAmt, shakeX, spinAngle float32, animating bool) {
	switchMu.Lock()
	defer switchMu.Unlock()

	st, ok := switchStates[key]
	if !ok {
		if on {
			pos = 1
			fillRatio = 1
		}
		fillA = fillAlphaTarget(on, phase)
		if pressed {
			stretchAmt = 1
		}
		return pos, fillRatio, fillA, stretchAmt, 0, 0, phase == SwitchBusy && !reduced
	}

	if reduced {
		if on {
			pos = 1
			fillRatio = 1
		}
		fillA = fillAlphaTarget(on, phase)
		if pressed {
			stretchAmt = 1
		}
		st.knobPos.active = false
		st.fillW.active = false
		st.fillAlpha.active = false
		st.stretch.active = false
		st.shakeActive = false
		return pos, fillRatio, fillA, stretchAmt, 0, 0, false
	}

	var a1, a2, a3, a4 bool
	pos, a1 = st.knobPos.sample(now, easeSwitchKnob)
	fillRatio, a2 = st.fillW.sample(now, easeSwitchFill)
	fillA, a3 = st.fillAlpha.sample(now, easeSwitchLinear)
	stretchAmt, a4 = st.stretch.sample(now, easeSwitchLinear)
	animating = a1 || a2 || a3 || a4

	if st.shakeActive {
		elapsed := now.Sub(st.shakeStart)
		if elapsed >= switchShakeDur {
			st.shakeActive = false
		} else if elapsed >= 0 {
			u := float32(elapsed) / float32(switchShakeDur)
			shakeX = sampleShakeOffset(u)
			animating = true
		}
	}

	if phase == SwitchBusy {
		if st.spinStart.IsZero() {
			st.spinStart = now
		}
		cyc := float64(now.Sub(st.spinStart)%switchSpinDur) / float64(switchSpinDur)
		spinAngle = float32(cyc * 2 * math.Pi)
		animating = true
	}

	return pos, fillRatio, fillA, stretchAmt, shakeX, spinAngle, animating
}

// sampleShakeOffset reproduces the 300ms damped failure shake: 0 -> -3 -> +2 -> -1 -> 0.
func sampleShakeOffset(u float32) float32 {
	switch {
	case u < 0.25:
		return -3 * (u / 0.25)
	case u < 0.55:
		return -3 + 5*((u-0.25)/0.30)
	case u < 0.80:
		return 2 - 3*((u-0.55)/0.25)
	default:
		return -1 * (1 - (u-0.80)/0.20)
	}
}

func emitPluckLocked(key string, kind pluckKind, now time.Time) {
	ch, ok := pluckChannels[key]
	if !ok {
		ch = &pluckChannel{}
		pluckChannels[key] = ch
	}
	ch.kind = kind
	ch.start = now
	ch.lastSeen = now
}

func recordPluckAnchor(key string, centerX float32, now time.Time) {
	switchMu.Lock()
	ch, ok := pluckChannels[key]
	if !ok {
		ch = &pluckChannel{}
		pluckChannels[key] = ch
	}
	ch.anchorX = centerX
	ch.hasX = true
	ch.lastSeen = now
	switchMu.Unlock()
}

// ResetPluck immediately silences an in-flight pluck wave for key (e.g. when
// switching tabs or uninstalling a plugin).
func ResetPluck(key string) {
	if key == "" {
		return
	}
	switchMu.Lock()
	if ch, ok := pluckChannels[key]; ok {
		ch.kind = pluckNone
	}
	switchMu.Unlock()
}

type pluckParams struct {
	amp   float64
	durMs float64
	tauMs float64
	hz    float64
}

func pluckParamsFor(kind pluckKind) pluckParams {
	switch kind {
	case pluckOn:
		return pluckParams{amp: 7, durMs: 640, tauMs: 170, hz: 7}
	case pluckFail:
		return pluckParams{amp: 4, durMs: 420, tauMs: 110, hz: 11}
	default:
		return pluckParams{amp: 3, durMs: 340, tauMs: 90, hz: 6}
	}
}

// PluckDivider renders a quiet 1px hairline that ripples like a plucked string
// whenever a paired SwitchWithOptions(PluckKey: key) changes state. Zero frames
// are scheduled at rest or under ReduceMotion.
func PluckDivider(c *ui.Context, key string) ui.Element {
	pal := theme.Ocean()
	el := ui.Box(c.Key("pluck-div-" + key)).FillWidth().Height(theme.PluckDividerHeight).MinWidth(0)
	el.Draw(func(p *ui.Painter, bounds ui.Rect) {
		if bounds.W <= 0 {
			return
		}
		midY := bounds.Y + bounds.H*0.5
		rm := testing.Testing() || p.Preferences().ReduceMotion
		if rm || key == "" {
			p.Line(bounds.X, midY, bounds.X+bounds.W, midY, 1, pal.Hairline)
			return
		}

		now := p.Now()
		if now.IsZero() {
			now = time.Now()
		}

		switchMu.Lock()
		ch, ok := pluckChannels[key]
		var (
			kind    pluckKind
			start   time.Time
			anchorX float32
			hasX    bool
		)
		if ok {
			kind = ch.kind
			start = ch.start
			anchorX = ch.anchorX
			hasX = ch.hasX
			ch.lastSeen = now
		}
		switchMu.Unlock()

		if kind == pluckNone {
			p.Line(bounds.X, midY, bounds.X+bounds.W, midY, 1, pal.Hairline)
			return
		}

		cfg := pluckParamsFor(kind)
		tMs := float64(now.Sub(start)) / float64(time.Millisecond)
		if tMs < 0 {
			tMs = 0
		}
		if tMs >= cfg.durMs {
			switchMu.Lock()
			if ch2, ok2 := pluckChannels[key]; ok2 && ch2.start.Equal(start) {
				ch2.kind = pluckNone
			}
			switchMu.Unlock()
			p.Line(bounds.X, midY, bounds.X+bounds.W, midY, 1, pal.Hairline)
			return
		}

		W := float64(bounds.W)
		x0 := W - float64(theme.SwitchHeroTrackW)*0.5
		if hasX {
			x0 = float64(anchorX - bounds.X)
			if x0 < 0 {
				x0 = 0
			} else if x0 > W {
				x0 = W
			}
		}
		speed := math.Max(W, 120) / 200.0
		kWave := (2 * math.Pi) / 76.0
		decay := math.Exp(-tMs / cfg.tauMs)

		const step = 3.0
		var path ui.Path
		path.Cap(ui.RoundCap).Join(ui.RoundJoin)
		first := true
		for x := 0.0; x < W; x += step {
			px := bounds.X + float32(x)
			py := midY + pluckOffsetY(x, x0, W, tMs, speed, kWave, decay, cfg)
			if first {
				path.MoveTo(px, py)
				first = false
			} else {
				path.LineTo(px, py)
			}
		}
		path.LineTo(bounds.X+bounds.W, midY)

		tintCol := pal.Accent
		switch kind {
		case pluckFail:
			tintCol = pal.Danger
		case pluckOff:
			tintCol = pal.TextSecondary
		}
		mixAmt := float32(math.Min(1, decay*1.25))
		strokeCol := pal.Hairline.Mix(tintCol, mixAmt)
		strokeW := float32(1 + 0.6*decay)
		p.StrokePath(&path, strokeW, strokeCol)
		p.AnimationFrame()
	})
	return el
}

func pluckOffsetY(x, x0, W, tMs, speed, kWave, decay float64, cfg pluckParams) float32 {
	dist := math.Abs(x - x0)
	tl := tMs - dist/speed
	if tl <= 0 {
		return 0
	}
	edge := math.Min(1, math.Min(x/28.0, (W-x)/28.0))
	if edge <= 0 {
		return 0
	}
	front := 1 - math.Exp(-tl/28.0)
	reach := math.Exp(-dist / (math.Max(W, 120) * 0.55))
	dy := -cfg.amp * decay * front * reach * edge * math.Sin(kWave*dist-2*math.Pi*cfg.hz*tMs/1000.0)
	return float32(dy)
}

// PluckSwitchRow renders a self-contained settings row with a Pluck Switch on
// the right and an automatically wired PluckDivider underneath. Callers only
// supply id, label, and value; no manual coordinate or wave wiring is needed.
func PluckSwitchRow(c *ui.Context, id, label string, value *bool, opts SwitchOptions) bool {
	pal := theme.Ocean()
	opts.ID = id
	opts.Label = label
	opts.Value = value
	if opts.PluckKey == "" {
		opts.PluckKey = id
	}
	var changed bool
	ui.Column(c.Key("pluck-row-" + id)).FillWidth().MinWidth(0).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).MinHeight(theme.SettingsRowHeight).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space16).Children(func() {
			ui.Text(c, label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
				FontWeight(theme.WeightRegular).TextColor(pal.Text).SingleLine().MinWidth(0).Shrink(1).Ellipsis("…")
			changed = SwitchWithOptions(c, opts)
		})
		PluckDivider(c, opts.PluckKey)
	})
	return changed
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
