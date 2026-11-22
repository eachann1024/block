package ui

import (
	"fmt"
	"math"
	"sync"
	"time"

	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	videoSeekStep          = 5 * time.Second
	videoSeekLargeStep     = 10 * time.Second
	videoFrameStep         = 33 * time.Millisecond
	videoVolumeStep        = 0.10
	videoBoostRate         = 3.0
	videoRightHoldDelay    = 220 * time.Millisecond
	videoOSDDuration       = 1100 * time.Millisecond
	videoBoostSourceNone   = 0
	videoBoostSourceKey    = 1
	videoBoostSourceMouse  = 2
)

var videoPlaybackRates = []float64{0.5, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0}

// videoControlTarget abstracts *ui.VideoPlayer so playback logic and the hold-right
// 3x boost state machine can be exercised deterministically in unit tests.
type videoControlTarget interface {
	Play()
	Pause()
	Playing() bool
	Ended() bool
	Duration() time.Duration
	Position() time.Duration
	Seek(d time.Duration)
	Rate() float64
	SetRate(r float64)
	Volume() float64
	SetVolume(v float64)
	Muted() bool
	SetMuted(m bool)
	Loop() bool
	SetLoop(loop bool)
}

type uiVideoTarget struct {
	p *ui.VideoPlayer
}

func (t uiVideoTarget) Play()                  { t.p.Play() }
func (t uiVideoTarget) Pause()                 { t.p.Pause() }
func (t uiVideoTarget) Playing() bool          { return t.p.Playing() }
func (t uiVideoTarget) Ended() bool            { return t.p.Ended() }
func (t uiVideoTarget) Duration() time.Duration { return t.p.Duration() }
func (t uiVideoTarget) Position() time.Duration { return t.p.Position() }
func (t uiVideoTarget) Seek(d time.Duration)   { t.p.Seek(d) }
func (t uiVideoTarget) Rate() float64          { return t.p.Rate() }
func (t uiVideoTarget) SetRate(r float64)      { t.p.SetRate(r) }
func (t uiVideoTarget) Volume() float64        { return t.p.Volume() }
func (t uiVideoTarget) SetVolume(v float64)    { t.p.SetVolume(v) }
func (t uiVideoTarget) Muted() bool            { return t.p.Muted() }
func (t uiVideoTarget) SetMuted(m bool)        { t.p.SetMuted(m) }
func (t uiVideoTarget) Loop() bool             { return t.p.Loop() }
func (t uiVideoTarget) SetLoop(loop bool)      { t.p.SetLoop(loop) }

// VideoControlState stores per-player UI and playback control state shared
// between inline video players and the full-window media lightbox.
type VideoControlState struct {
	BaseRate        float64
	BoostActive     bool
	BoostSource     uint8
	BoostSavedRate  float64
	BoostWasPaused  bool
	RightKeyPending bool
	RightKeyDownAt  time.Time
	SavedVolume     float64

	Scrubbing     bool
	ScrubTarget   time.Duration
	VolScrubbing  bool
	SpeedMenuOpen bool
	DockHovered   bool

	OSDText      string
	OSDUntil     time.Time
	OSDVolume    float64
	OSDHasVolume bool
}

// ResetTransient clears transient gesture/OSD state while preserving BaseRate and SavedVolume.
func (s *VideoControlState) ResetTransient() {
	if s == nil {
		return
	}
	s.Scrubbing = false
	s.VolScrubbing = false
	s.SpeedMenuOpen = false
	s.RightKeyPending = false
	s.RightKeyDownAt = time.Time{}
	s.OSDText = ""
	s.OSDUntil = time.Time{}
	s.OSDHasVolume = false
}

func (s *VideoControlState) effectiveBaseRate(target videoControlTarget) float64 {
	if s != nil && s.BaseRate > 0 {
		return s.BaseRate
	}
	if target != nil {
		if r := target.Rate(); r > 0 {
			return r
		}
	}
	return 1.0
}

func (s *VideoControlState) showOSD(text string, now time.Time) {
	if s == nil {
		return
	}
	s.OSDText = text
	s.OSDUntil = now.Add(videoOSDDuration)
	s.OSDHasVolume = false
}

func (s *VideoControlState) showVolumeOSD(vol float64, muted bool, now time.Time) {
	if s == nil {
		return
	}
	if muted || vol <= 0.001 {
		s.OSDText = theme.Tr("静音", "Muted")
		s.OSDVolume = 0
	} else {
		pct := int(math.Round(clampFloat64(vol, 0, 1) * 100))
		s.OSDText = fmt.Sprintf("%s %d%%", theme.Tr("音量", "Volume"), pct)
		s.OSDVolume = clampFloat64(vol, 0, 1)
	}
	s.OSDUntil = now.Add(videoOSDDuration)
	s.OSDHasVolume = true
}

// TogglePlayPause toggles playback, restarting from the beginning if the video ended.
func (s *VideoControlState) TogglePlayPause(target videoControlTarget, now time.Time) {
	if target == nil {
		return
	}
	if target.Ended() {
		target.Seek(0)
		target.Play()
		s.showOSD(theme.Tr("重新播放", "Replay"), now)
		return
	}
	if target.Playing() {
		target.Pause()
		s.showOSD(theme.Tr("已暂停", "Paused"), now)
	} else {
		target.Play()
		s.showOSD(theme.Tr("播放", "Playing"), now)
	}
}

// SeekBy moves the playhead by delta seconds, clamped to [0, duration].
func (s *VideoControlState) SeekBy(target videoControlTarget, delta time.Duration, now time.Time) {
	if target == nil {
		return
	}
	pos := target.Position()
	dur := target.Duration()
	if target.Ended() && dur > 0 {
		pos = dur
	}
	next := pos + delta
	if next < 0 {
		next = 0
	}
	if dur > 0 && next > dur {
		next = dur
	}
	target.Seek(next)
	secs := int(math.Round(delta.Seconds()))
	if secs >= 0 {
		s.showOSD(fmt.Sprintf("+%ds", secs), now)
	} else {
		s.showOSD(fmt.Sprintf("%ds", secs), now)
	}
}

// SeekToRatio jumps to ratio in [0, 1] of the video's duration.
func (s *VideoControlState) SeekToRatio(target videoControlTarget, ratio float64, now time.Time, showOSD bool) {
	if target == nil {
		return
	}
	dur := target.Duration()
	if dur <= 0 {
		return
	}
	r := clampFloat64(ratio, 0, 1)
	dest := time.Duration(float64(dur) * r)
	target.Seek(dest)
	if showOSD {
		s.showOSD(FormatMediaDuration(dest), now)
	}
}

// FrameStep steps one video frame (~33ms) forward or backward when paused.
func (s *VideoControlState) FrameStep(target videoControlTarget, forward bool, now time.Time) {
	if target == nil {
		return
	}
	if target.Playing() {
		target.Pause()
	}
	delta := videoFrameStep
	label := theme.Tr("下一帧", "Next frame")
	if !forward {
		delta = -videoFrameStep
		label = theme.Tr("上一帧", "Prev frame")
	}
	pos := target.Position() + delta
	if pos < 0 {
		pos = 0
	}
	if dur := target.Duration(); dur > 0 && pos > dur {
		pos = dur
	}
	target.Seek(pos)
	s.showOSD(label, now)
}

// AdjustVolume changes volume by delta in [-1, 1], automatically unmuting when increasing.
func (s *VideoControlState) AdjustVolume(target videoControlTarget, delta float64, now time.Time) {
	if target == nil {
		return
	}
	cur := target.Volume()
	if target.Muted() {
		if delta > 0 {
			cur = 0
			target.SetMuted(false)
		} else {
			s.showVolumeOSD(0, true, now)
			return
		}
	}
	next := clampFloat64(math.Round((cur+delta)*100)/100, 0, 1)
	target.SetVolume(next)
	if next > 0.001 {
		target.SetMuted(false)
		s.SavedVolume = next
	} else {
		target.SetMuted(true)
	}
	s.showVolumeOSD(next, next <= 0.001, now)
}

// SetVolumeDirect sets volume from a slider interaction in [0, 1].
func (s *VideoControlState) SetVolumeDirect(target videoControlTarget, vol float64, now time.Time) {
	if target == nil {
		return
	}
	next := clampFloat64(vol, 0, 1)
	target.SetVolume(next)
	if next <= 0.001 {
		target.SetMuted(true)
	} else {
		target.SetMuted(false)
		s.SavedVolume = next
	}
	s.showVolumeOSD(next, next <= 0.001, now)
}

// ToggleMute toggles mute while remembering the previous non-zero volume.
func (s *VideoControlState) ToggleMute(target videoControlTarget, now time.Time) {
	if target == nil {
		return
	}
	if target.Muted() || target.Volume() <= 0.001 {
		restore := s.SavedVolume
		if restore <= 0.05 {
			restore = 1.0
		}
		target.SetVolume(restore)
		target.SetMuted(false)
		s.showVolumeOSD(restore, false, now)
	} else {
		s.SavedVolume = target.Volume()
		target.SetMuted(true)
		s.showVolumeOSD(0, true, now)
	}
}

// ToggleLoop toggles continuous looping for the player.
func (s *VideoControlState) ToggleLoop(target videoControlTarget, now time.Time) {
	if target == nil {
		return
	}
	next := !target.Loop()
	target.SetLoop(next)
	if next {
		s.showOSD(theme.Tr("循环播放：开", "Loop: On"), now)
	} else {
		s.showOSD(theme.Tr("循环播放：关", "Loop: Off"), now)
	}
}

// SetBaseRate updates the user's preferred playback speed and applies it immediately
// unless a temporary 3x boost is currently held.
func (s *VideoControlState) SetBaseRate(target videoControlTarget, rate float64, now time.Time) {
	if rate <= 0 {
		rate = 1.0
	}
	s.BaseRate = rate
	if target != nil {
		if s.BoostActive {
			s.BoostSavedRate = rate
		} else {
			target.SetRate(rate)
		}
	}
	s.showOSD(formatPlaybackRate(rate), now)
}

// StepRate moves to the next slower (dir < 0) or faster (dir > 0) preset playback speed.
func (s *VideoControlState) StepRate(target videoControlTarget, dir int, now time.Time) {
	cur := s.effectiveBaseRate(target)
	bestIdx := 2 // 1.0x
	bestDiff := math.MaxFloat64
	for i, r := range videoPlaybackRates {
		if d := math.Abs(r - cur); d < bestDiff {
			bestDiff = d
			bestIdx = i
		}
	}
	nextIdx := bestIdx + dir
	if nextIdx < 0 {
		nextIdx = 0
	} else if nextIdx >= len(videoPlaybackRates) {
		nextIdx = len(videoPlaybackRates) - 1
	}
	s.SetBaseRate(target, videoPlaybackRates[nextIdx], now)
}

// StartBoost activates temporary 3x playback speed, remembering base rate and pause state.
func (s *VideoControlState) StartBoost(target videoControlTarget, source uint8) {
	if s == nil || target == nil {
		return
	}
	if s.BoostActive {
		return
	}
	base := s.effectiveBaseRate(target)
	s.BaseRate = base
	s.BoostSavedRate = base
	s.BoostWasPaused = !target.Playing()
	s.BoostActive = true
	s.BoostSource = source
	target.SetRate(videoBoostRate)
	if !target.Playing() {
		if target.Ended() {
			target.Seek(0)
		}
		target.Play()
	}
}

// EndBoost restores the original playback speed (and paused state if it was paused before boost).
func (s *VideoControlState) EndBoost(target videoControlTarget) {
	if s == nil {
		return
	}
	wasBoosted := s.BoostActive
	savedRate := s.BoostSavedRate
	wasPaused := s.BoostWasPaused
	s.BoostActive = false
	s.BoostSource = videoBoostSourceNone
	s.BoostSavedRate = 0
	s.BoostWasPaused = false
	s.RightKeyPending = false
	s.RightKeyDownAt = time.Time{}

	if !wasBoosted || target == nil {
		return
	}
	if savedRate <= 0 {
		savedRate = s.effectiveBaseRate(target)
	}
	target.SetRate(savedRate)
	if wasPaused {
		target.Pause()
	}
}

// EndPlayerBoost is a convenience wrapper for *ui.VideoPlayer callers.
func (s *VideoControlState) EndPlayerBoost(player *ui.VideoPlayer) {
	if s == nil {
		return
	}
	if player == nil {
		s.EndBoost(nil)
		return
	}
	s.EndBoost(uiVideoTarget{p: player})
}

// BeginRightKey records a fresh right-arrow press. If stillDown is already false
// (single-frame tap), it immediately seeks forward +5s; otherwise it waits up to
// videoRightHoldDelay to distinguish a tap (+5s) from a hold (3x speed).
func (s *VideoControlState) BeginRightKey(target videoControlTarget, stillDown bool, now time.Time) (bool, time.Duration) {
	if s == nil || target == nil {
		return false, 0
	}
	if !stillDown {
		s.SeekBy(target, videoSeekStep, now)
		return false, 0
	}
	s.RightKeyPending = true
	s.RightKeyDownAt = now
	return true, videoRightHoldDelay
}

// UpdateRightKey advances the right-arrow tap-vs-hold state machine on every frame.
func (s *VideoControlState) UpdateRightKey(target videoControlTarget, stillDown bool, now time.Time) (bool, time.Duration) {
	if s == nil {
		return false, 0
	}
	if !s.RightKeyPending {
		if s.BoostActive && s.BoostSource == videoBoostSourceKey && !stillDown {
			s.EndBoost(target)
		}
		return false, 0
	}
	if !stillDown {
		if s.BoostActive && s.BoostSource == videoBoostSourceKey {
			s.EndBoost(target)
		} else {
			s.RightKeyPending = false
			s.RightKeyDownAt = time.Time{}
			s.SeekBy(target, videoSeekStep, now)
		}
		return false, 0
	}
	if s.BoostActive {
		return false, 0
	}
	elapsed := now.Sub(s.RightKeyDownAt)
	if elapsed >= videoRightHoldDelay {
		s.StartBoost(target, videoBoostSourceKey)
		return false, 0
	}
	return true, videoRightHoldDelay - elapsed
}

// UpdateMouseBoost activates 3x speed while the secondary (right) mouse button is held
// over the video canvas and restores the original speed as soon as it is released.
func (s *VideoControlState) UpdateMouseBoost(target videoControlTarget, rightHeld bool, now time.Time) {
	if s == nil || target == nil {
		return
	}
	if rightHeld {
		if !s.BoostActive {
			s.StartBoost(target, videoBoostSourceMouse)
		}
	} else if s.BoostActive && s.BoostSource == videoBoostSourceMouse {
		s.EndBoost(target)
	}
}

// FormatMediaDuration formats a media timestamp as MM:SS or H:MM:SS.
func FormatMediaDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	sec := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func formatPlaybackRate(r float64) string {
	if math.Abs(r-math.Round(r)) < 0.01 {
		return fmt.Sprintf("%.0fx", math.Round(r))
	}
	if math.Abs(r*10-math.Round(r*10)) < 0.01 {
		return fmt.Sprintf("%.1fx", r)
	}
	return fmt.Sprintf("%.2fx", r)
}

func clampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// VideoPlayerOptions configures RenderVideoPlayer for inline or lightbox usage.
type VideoPlayerOptions struct {
	ID              string
	Label           string
	PlayProbeID     string
	MaxHeight       float32
	StageWidth      float32
	StageHeight     float32
	InLightbox         bool
	FillMode           bool
	HideVideo          bool
	IsFullscreen       bool
	ChromeHidden       bool
	BottomInset        float32
	HandleKeyboard     bool
	AllowVolumeKeys    bool
	KeyboardOverlay    *ui.Element
	OnExpand           func()
	OnToggleFullscreen func()
	OnToggleFill       func()
	OnOpenExternal     func()
	OnInteract         func()
}

// HandleVideoShortcuts processes video keyboard controls (Space/K, Left/Right, Hold-Right 3x,
// Up/Down volume, M mute, R loop, F expand, J/L ±10s, Home/End, 0-9 jump, ,/. frame step, </> speed).
func HandleVideoShortcuts(c *ui.Context, overlay *ui.Element, player *ui.VideoPlayer, st *VideoControlState, allowVolume bool, onExpand func()) {
	if player == nil || st == nil {
		return
	}
	target := uiVideoTarget{p: player}
	now := c.Now()

	match := func(mods ui.Modifiers, key ui.Key) bool {
		if overlay != nil && overlay.Valid() {
			return overlay.OverlayShortcut(mods, key)
		}
		return c.Shortcut(mods, key)
	}

	// Keep hold-right 3x boost responsive across frames.
	if wake, wait := st.UpdateRightKey(target, c.KeyDown(ui.KeyRight), now); wake {
		c.After(wait + 10*time.Millisecond)
	}

	if match(0, ui.KeySpace) || match(0, ui.KeyK) {
		if !c.ShortcutRepeated() {
			st.TogglePlayPause(target, now)
		}
	}
	if match(0, ui.KeyRight) {
		if !c.ShortcutRepeated() {
			if wake, wait := st.BeginRightKey(target, c.KeyDown(ui.KeyRight), now); wake {
				c.After(wait + 10*time.Millisecond)
			}
		}
	}
	if match(0, ui.KeyLeft) {
		st.SeekBy(target, -videoSeekStep, now)
	}
	if match(ui.Shift, ui.KeyRight) || match(0, ui.KeyL) {
		st.SeekBy(target, videoSeekLargeStep, now)
	}
	if match(ui.Shift, ui.KeyLeft) || match(0, ui.KeyJ) {
		st.SeekBy(target, -videoSeekLargeStep, now)
	}
	if allowVolume {
		if match(0, ui.KeyUp) {
			st.AdjustVolume(target, videoVolumeStep, now)
		}
		if match(0, ui.KeyDown) {
			st.AdjustVolume(target, -videoVolumeStep, now)
		}
	}
	if match(0, ui.KeyM) && !c.ShortcutRepeated() {
		st.ToggleMute(target, now)
	}
	if match(0, ui.KeyR) && !c.ShortcutRepeated() {
		st.ToggleLoop(target, now)
	}
	if onExpand != nil && match(0, ui.KeyF) && !c.ShortcutRepeated() {
		onExpand()
	}
	if match(0, ui.KeyHome) {
		st.SeekToRatio(target, 0, now, true)
	}
	if match(0, ui.KeyEnd) {
		st.SeekToRatio(target, 0.99, now, true)
	}
	if match(0, ui.KeyComma) {
		st.FrameStep(target, false, now)
	}
	if match(0, ui.KeyPeriod) {
		st.FrameStep(target, true, now)
	}
	if match(ui.Shift, ui.KeyComma) {
		st.StepRate(target, -1, now)
	}
	if match(ui.Shift, ui.KeyPeriod) {
		st.StepRate(target, 1, now)
	}
	digitKeys := [10]ui.Key{
		ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4,
		ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9,
	}
	for i, dk := range digitKeys {
		if match(0, dk) && !c.ShortcutRepeated() {
			st.SeekToRatio(target, float64(i)/10.0, now, true)
		}
	}
}

// Inline media SVGs parsed once so video controls stay razor sharp across DPI scales
// without adding extra icon asset files.
var (
	videoIconsOnce  sync.Once
	svgVidPlay      *ui.SVG
	svgVidPause     *ui.SVG
	svgVidReplay    *ui.SVG
	svgVidRewind    *ui.SVG
	svgVidForward   *ui.SVG
	svgVidVolHigh   *ui.SVG
	svgVidVolLow    *ui.SVG
	svgVidVolMute   *ui.SVG
	svgVidLoop       *ui.SVG
	svgVidExpand     *ui.SVG
	svgVidExitExpand *ui.SVG
	svgVidFastSpeed  *ui.SVG
)

func loadVideoIcons() {
	videoIconsOnce.Do(func() {
		parse := func(src string) *ui.SVG {
			s, _ := ui.ParseSVG([]byte(src))
			return s
		}
		svgVidPlay = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M4.5 2.9a.75.75 0 0 1 1.13-.65l8.1 4.85a.75.75 0 0 1 0 1.29l-8.1 4.85a.75.75 0 0 1-1.13-.65V2.9z"/></svg>`)
		svgVidPause = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="2.75" width="3.2" height="10.5" rx="1"/><rect x="9.3" y="2.75" width="3.2" height="10.5" rx="1"/></svg>`)
		svgVidReplay = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.2 7.5A4.8 4.8 0 1 1 4.6 11"/><path d="M3.2 4.2v3.3h3.3"/></svg>`)
		svgVidRewind = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M7.8 4.1a.65.65 0 0 0-1-.54L2.3 7.1a.65.65 0 0 0 0 1.02l4.5 3.54a.65.65 0 0 0 1-.54V4.1z"/><path d="M13.8 4.1a.65.65 0 0 0-1-.54L8.3 7.1a.65.65 0 0 0 0 1.02l4.5 3.54a.65.65 0 0 0 1-.54V4.1z"/></svg>`)
		svgVidForward = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M2.2 4.1a.65.65 0 0 1 1-.54l4.5 3.54a.65.65 0 0 1 0 1.02l-4.5 3.54a.65.65 0 0 1-1-.54V4.1z"/><path d="M8.2 4.1a.65.65 0 0 1 1-.54l4.5 3.54a.65.65 0 0 1 0 1.02l-4.5 3.54a.65.65 0 0 1-1-.54V4.1z"/></svg>`)
		svgVidVolHigh = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.2 6.2h2.6l3.4-2.8v9.2L4.8 9.8H2.2z" fill="currentColor" stroke="none"/><path d="M10.8 5.5a3.5 3.5 0 0 1 0 5"/><path d="M12.8 3.6a6 6 0 0 1 0 8.8"/></svg>`)
		svgVidVolLow = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.2 6.2h2.6l3.4-2.8v9.2L4.8 9.8H2.2z" fill="currentColor" stroke="none"/><path d="M10.8 5.6a3.4 3.4 0 0 1 0 4.8"/></svg>`)
		svgVidVolMute = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.2 6.2h2.6l3.4-2.8v9.2L4.8 9.8H2.2z" fill="currentColor" stroke="none"/><path d="M10.5 6l3.5 4M14 6l-3.5 4"/></svg>`)
		svgVidLoop = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M11.5 2.5L13.8 4.8 11.5 7.1"/><path d="M2.5 7.8V6.3a1.5 1.5 0 0 1 1.5-1.5h9.5"/><path d="M4.5 13.5L2.2 11.2 4.5 8.9"/><path d="M13.5 8.2v1.5a1.5 1.5 0 0 1-1.5 1.5H2.5"/></svg>`)
		svgVidExpand = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 2.5h3.5V6"/><path d="M6 13.5H2.5V10"/><path d="M13.5 2.5L9.3 6.7"/><path d="M2.5 13.5l4.2-4.2"/></svg>`)
		svgVidExitExpand = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M13.5 6H10V2.5"/><path d="M2.5 10H6v3.5"/><path d="M10 6l3.5-3.5"/><path d="M6 10l-3.5 3.5"/></svg>`)
		svgVidFastSpeed = parse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M1.8 4.2a.6.6 0 0 1 .93-.5l4.2 3.3a.6.6 0 0 1 0 .96l-4.2 3.3a.6.6 0 0 1-.93-.5V4.2z"/><path d="M6.8 4.2a.6.6 0 0 1 .93-.5l4.2 3.3a.6.6 0 0 1 0 .96l-4.2 3.3a.6.6 0 0 1-.93-.5V4.2z"/><path d="M11.8 4.2a.6.6 0 0 1 .93-.5l2.1 1.7a.6.6 0 0 1 0 .96l-2.1 1.7a.6.6 0 0 1-.93-.5V4.2z"/></svg>`)
	})
}

// DrawMediaExpandBadge renders a subtle translucent top-right zoom/expand badge over
// an image or video when hovered. Returns true if the badge was clicked.
func DrawMediaExpandBadge(c *ui.Context, id string, tooltip string, visible bool) bool {
	if !visible {
		return false
	}
	loadVideoIcons()
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).
		Absolute().Top(theme.Space8).Right(theme.Space8).
		Size(theme.HeightCompact, theme.HeightCompact).Center().
		Radius(theme.RadiusMD).Cursor(ui.CursorPointer).
		Label(tooltip)
	bg := pal.ImageOverlayBadgeBG()
	if btn.Hovered() || btn.Pressed() {
		bg = pal.ImageOverlayBadgeHoverBG()
	}
	btn.Background(bg)
	Elevate(btn, theme.ElevationRaised)
	if tooltip != "" {
		Tooltip(c, btn, tooltip)
	}
	btn.Children(func() {
		if svgVidExpand != nil {
			ui.Icon(c, svgVidExpand).Size(theme.IconInline, theme.IconInline).TextColor(pal.ImageOverlayBadgeFG())
		}
	})
	return btn.Clicked()
}

// RenderVideoPlayer draws a complete Ocean Paper video player with interactive progress
// scrubber, seek ±5s, hold-right 3x speed boost (both keyboard → and right mouse button),
// volume/mute slider, playback speed selector, loop toggle, OSD feedback, and expand action.
func RenderVideoPlayer(c *ui.Context, player *ui.VideoPlayer, st *VideoControlState, opts VideoPlayerOptions) ui.Element {
	loadVideoIcons()
	pal := theme.Ocean()
	if opts.ID == "" {
		opts.ID = "video-player"
	}
	if st == nil {
		st = &VideoControlState{}
	}
	if st.BaseRate <= 0 {
		if player != nil && player.Rate() > 0 {
			st.BaseRate = player.Rate()
		} else {
			st.BaseRate = 1.0
		}
	}
	if st.SavedVolume <= 0 {
		if player != nil && player.Volume() > 0 {
			st.SavedVolume = player.Volume()
		} else {
			st.SavedVolume = 1.0
		}
	}

	now := c.Now()
	target := uiVideoTarget{p: player}

	// Keep the UI ticking smoothly while playing or while an OSD pill is visible.
	if player != nil && player.Playing() {
		c.After(100 * time.Millisecond)
	}
	osdActive := !st.OSDUntil.IsZero() && now.Before(st.OSDUntil)
	if osdActive {
		if rem := st.OSDUntil.Sub(now); rem > 0 {
			c.After(rem + 10*time.Millisecond)
		}
	}

	st.DockHovered = false
	darkDock := opts.InLightbox

	var root ui.Element
	if opts.InLightbox {
		root = ui.Box(c.Key(opts.ID)).
			Width(max(opts.StageWidth, 240)).
			Height(max(opts.StageHeight, 180)).
			Background(pal.LightboxStageBG()).Clip()
	} else {
		root = ui.Column(c.Key(opts.ID)).FillWidth().MinWidth(0).Background(pal.CodeBlockBG)
	}

	if opts.HandleKeyboard && player != nil && !opts.HideVideo {
		allowVol := opts.AllowVolumeKeys || opts.InLightbox || root.Hovered()
		onF := opts.OnExpand
		if opts.InLightbox && opts.OnToggleFullscreen != nil {
			onF = opts.OnToggleFullscreen
		}
		HandleVideoShortcuts(c, opts.KeyboardOverlay, player, st, allowVol, onF)
	}

	root.Children(func() {
		// 1. Video visual stage.
		stage := ui.Box(c.Key(opts.ID + "-stage")).Center().Clip()
		if opts.InLightbox {
			stage.Fill().Background(pal.LightboxStageBG())
		} else {
			stage.FillWidth().Background(pal.AISurface().MediaFrameBG)
			if opts.MaxHeight > 0 {
				stage.MaxHeight(opts.MaxHeight)
			}
		}

		vw, vh := 0, 0
		if player != nil {
			vw, vh = player.Size()
		}
		if !opts.HideVideo {
			stage.Cursor(ui.CursorPointer)
		}

		// Right-mouse-button hold for 3x speed on the video stage!
		rightHeld := false
		if !opts.HideVideo && player != nil {
			if btn, pressing := stage.PressedButton(); pressing && btn == 1 {
				rightHeld = true
			}
			st.UpdateMouseBoost(target, rightHeld, now)
		}

		stageHovered := stage.Hovered()
		expandBadgeClicked := false

		stage.Children(func() {
			switch {
			case opts.HideVideo:
				ui.Column(c).FillWidth().Height(theme.VideoPlaceholderHeight).Center().Gap(theme.Space8).Children(func() {
					icons.Duo(c, icons.Video, theme.IconChrome, true)
					ui.Text(c, theme.Tr("正在放大视图中播放", "Playing in expanded viewer")).
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						TextColor(pal.TextSecondary).SingleLine()
				})

			case player != nil && player.Ready() && vw > 0 && vh > 0:
				vidEl := ui.Video(c, player).Label(opts.Label)
				if opts.InLightbox {
					fit := ui.Contain
					if opts.FillMode {
						fit = ui.Cover
					}
					vidEl.Fill().Fit(fit)
				} else if opts.MaxHeight > 0 {
					vidEl.FillWidth().MaxHeight(opts.MaxHeight).Fit(ui.Contain)
				} else {
					vidEl.FillWidth().Fit(ui.Contain)
				}

			case player != nil:
				ui.Video(c, player).FillWidth().Height(theme.VideoPlaceholderHeight).Fit(ui.Contain).Label(opts.Label)
				ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Center().IgnoreInput(true).Children(func() {
					SparkleLoader(c, theme.IconChrome, pal.Accent, pal.AccentHover)
				})
			}

			// Buffering spinner overlay when video is loading or stalling.
			if !opts.HideVideo && player != nil && player.Ready() && player.Buffering() {
				ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Center().IgnoreInput(true).Children(func() {
					SparkleLoader(c, theme.IconChrome, pal.Accent, pal.AccentHover)
				})
			}

			// Center translucent play/replay badge when paused & ready.
			if !opts.HideVideo && player != nil && player.Ready() && !player.Playing() && !st.BoostActive {
				ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Center().IgnoreInput(true).Children(func() {
					badge := ui.Box(c).Size(theme.VideoPlayBadgeSize, theme.VideoPlayBadgeSize).Center().
						Radius(theme.RadiusPill).Background(pal.ImageOverlayBadgeBG())
					Elevate(badge, theme.ElevationPopover)
					badge.Children(func() {
						glyph := svgVidPlay
						if player.Ended() {
							glyph = svgVidReplay
						}
						if glyph != nil {
							ui.Icon(c, glyph).Size(theme.IconChrome, theme.IconChrome).TextColor(pal.ImageOverlayBadgeFG())
						}
					})
				})
			}

			// Floating top-center OSD pill: persistent during 3x hold boost, transient for seek/volume/speed/loop.
			if !opts.HideVideo && (st.BoostActive || osdActive) {
				ui.Row(c).Absolute().Top(theme.Space12).Left(0).Right(0).Justify(ui.Center).IgnoreInput(true).Children(func() {
					pill := ui.Row(c.Key(opts.ID+"-osd")).
						Padding(theme.Space6, theme.Space12).Gap(theme.Space6).
						AlignItems(ui.Center).Radius(theme.RadiusPill).
						Background(pal.ImageOverlayBadgeBG())
					Elevate(pill, theme.ElevationPopover)
					pill.Children(func() {
						if st.BoostActive {
							if svgVidFastSpeed != nil {
								ui.Icon(c, svgVidFastSpeed).Size(theme.IconInline, theme.IconInline).TextColor(pal.Accent)
							}
							ui.Text(c, theme.Tr("3.0x 倍速播放中", "3.0x Speed")).
								FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								FontWeight(theme.WeightControl).TextColor(pal.ImageOverlayBadgeFG()).SingleLine()
						} else {
							ui.Text(c, st.OSDText).
								FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								FontWeight(theme.WeightControl).FontFeatures("tnum").
								TextColor(pal.ImageOverlayBadgeFG()).SingleLine()
							if st.OSDHasVolume {
								volFrac := float32(clampFloat64(st.OSDVolume, 0, 1))
								bar := ui.Box(c).Width(theme.VideoOSDVolumeBarW).Height(theme.VideoOSDVolumeBarH).
									Radius(theme.RadiusPill).Background(pal.ImageOverlayBadgeFG().Alpha(0.28)).Clip()
								bar.Children(func() {
									ui.Box(c).Width(theme.VideoOSDVolumeBarW * volFrac).Height(theme.VideoOSDVolumeBarH).
										Radius(theme.RadiusPill).Background(pal.ImageOverlayBadgeFG())
								})
							}
						}
					})
				})
			}

			// Top-right expand badge on hover for inline videos.
			if !opts.HideVideo && !opts.InLightbox && opts.OnExpand != nil {
				if DrawMediaExpandBadge(c, opts.ID+"-expand-badge", theme.Tr("放大观看", "Expand"), stageHovered) {
					expandBadgeClicked = true
				}
			}
		})

		if expandBadgeClicked && opts.OnExpand != nil {
			st.EndBoost(target)
			opts.OnExpand()
		} else if !opts.HideVideo && player != nil {
			if stage.DoubleClicked() {
				if opts.OnInteract != nil {
					opts.OnInteract()
				}
				if opts.InLightbox && opts.OnToggleFullscreen != nil {
					opts.OnToggleFullscreen()
				} else if opts.InLightbox && opts.OnToggleFill != nil {
					opts.OnToggleFill()
				} else if !opts.InLightbox && opts.OnExpand != nil {
					st.EndBoost(target)
					opts.OnExpand()
				}
			} else if stage.Clicked() {
				if opts.OnInteract != nil {
					opts.OnInteract()
				}
				st.SpeedMenuOpen = false
				st.TogglePlayPause(target, now)
			}
		}

		// 2. Control dock (scrubber + controls row).
		if opts.HideVideo || player == nil {
			return
		}
		if opts.InLightbox && opts.ChromeHidden && !st.Scrubbing && !st.VolScrubbing && !st.SpeedMenuOpen && player.Playing() {
			return
		}
		dock := ui.Column(c.Key(opts.ID + "-dock")).MinWidth(0)
		if darkDock {
			dock.Absolute().Left(theme.Space16).Right(theme.Space16).Bottom(theme.Space12+opts.BottomInset).
				Radius(theme.RadiusXL).Background(pal.LightboxChromeBG()).
				Border(1, pal.LightboxChromeBorder()).Clip()
			Elevate(dock, theme.ElevationDialog)
		} else {
			dock.FillWidth().Background(pal.Paper).BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
		}
		st.DockHovered = dock.Hovered()
		dock.Children(func() {
			dur := player.Duration()
			pos := player.Position()
			if player.Ended() && dur > 0 {
				pos = dur
			}
			if st.Scrubbing {
				pos = st.ScrubTarget
			}
			renderVideoScrubber(c, opts.ID+"-scrubber", pal, darkDock, target, st, pos, dur, opts.OnInteract)

			// Controls row.
			bar := ui.Row(c.Key(opts.ID+"-controls")).FillWidth().MinWidth(0).
				Height(theme.VideoControlBarHeight).PaddingX(theme.Space10).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8)
			bar.Children(func() {
				// Left cluster: Play/Pause, -5s, +5s, Time.
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(1).MinWidth(0).Children(func() {
					playIcon := svgVidPlay
					playTip := theme.Tr("播放", "Play")
					if player.Playing() {
						playIcon = svgVidPause
						playTip = theme.Tr("暂停", "Pause")
					} else if player.Ended() {
						playIcon = svgVidReplay
						playTip = theme.Tr("重新播放", "Replay")
					}
					playBtn := renderVideoGlyphButton(c, opts.ID+"-play", playIcon, playTip, "Space", false, darkDock)
					if opts.PlayProbeID != "" {
						Probe(playBtn, opts.PlayProbeID)
					}
					if playBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.TogglePlayPause(target, now)
					}

					rewBtn := renderVideoGlyphButton(c, opts.ID+"-rew", svgVidRewind, theme.Tr("后退 5 秒", "Rewind 5s"), "←", false, darkDock)
					if rewBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.SeekBy(target, -videoSeekStep, now)
					}

					fwdBtn := renderVideoGlyphButton(c, opts.ID+"-fwd", svgVidForward, theme.Tr("快进 5 秒（按住 → 3 倍速）", "Forward 5s (Hold → for 3x)"), "→", false, darkDock)
					if fwdBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.SeekBy(target, videoSeekStep, now)
					}

					timeStr := FormatMediaDuration(pos)
					if dur > 0 {
						timeStr += " / " + FormatMediaDuration(dur)
					}
					timeColor := pal.TextSecondary
					if darkDock {
						timeColor = pal.LightboxChromeMutedFG()
					}
					ui.Text(c, timeStr).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						FontFeatures("tnum").TextColor(timeColor).SingleLine().Shrink(0).
						Margin(0, 0, 0, theme.Space4)
				})

				// Right cluster: Mute + Volume slider, Speed selector, Loop, Expand/Fullscreen, Open External.
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0).Children(func() {
					vol := player.Volume()
					muted := player.Muted() || vol <= 0.001
					volIcon := svgVidVolHigh
					volTip := theme.Tr("静音", "Mute")
					if muted {
						volIcon = svgVidVolMute
						volTip = theme.Tr("取消静音", "Unmute")
					} else if vol < 0.5 {
						volIcon = svgVidVolLow
					}
					muteBtn := renderVideoGlyphButton(c, opts.ID+"-mute", volIcon, volTip, "M", false, darkDock)
					if muteBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.ToggleMute(target, now)
					}

					renderVideoVolumeSlider(c, opts.ID+"-vol", pal, darkDock, target, st, vol, muted, opts.OnInteract)

					// Playback speed pill (click to open compact speed picker or cycle).
					shownRate := st.effectiveBaseRate(target)
					if st.BoostActive {
						shownRate = videoBoostRate
					}
					rateText := formatPlaybackRate(shownRate)
					speedBtn := ui.ButtonBase(c.Key(opts.ID+"-speed")).
						Height(theme.HeightMicro).PaddingX(theme.Space6).
						Center().Radius(theme.RadiusSM).Cursor(ui.CursorPointer).
						Label(theme.Tr("倍速", "Speed"))
					speedBG := ui.Transparent
					speedFG := pal.TextSecondary
					if darkDock {
						speedFG = pal.LightboxChromeMutedFG()
						if st.BoostActive || st.SpeedMenuOpen || math.Abs(shownRate-1.0) > 0.01 {
							speedBG = pal.LightboxChromeActive()
							speedFG = pal.LightboxChromeFG()
						} else if speedBtn.Hovered() {
							speedBG = pal.LightboxChromeHover()
							speedFG = pal.LightboxChromeFG()
						}
					} else if st.BoostActive || st.SpeedMenuOpen || math.Abs(shownRate-1.0) > 0.01 {
						speedBG = pal.Selected
						speedFG = pal.Accent
					} else if speedBtn.Hovered() {
						speedBG = pal.Hover
						speedFG = pal.Text
					}
					AnimateSurfaceColors(speedBtn, speedBG, ui.Transparent, false)
					RichTooltip(c, speedBtn, TooltipSpec{
						Placement: TooltipAbove,
						Rows: []TooltipRow{
							{Trigger: theme.Tr("切换播放倍速", "Playback speed"), Shortcut: "< / >"},
							{Trigger: theme.Tr("按住右键 / → 3x 倍速", "Hold Right / → for 3x")},
						},
					})
					speedBtn.Children(func() {
						ui.Text(c, rateText).
							FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightControl).FontFeatures("tnum").
							TextColor(speedFG).SingleLine()
					})
					if speedBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.SpeedMenuOpen = !st.SpeedMenuOpen
					}

					loopBtn := renderVideoGlyphButton(c, opts.ID+"-loop", svgVidLoop, theme.Tr("循环播放", "Loop playback"), "R", player.Loop(), darkDock)
					if loopBtn.Clicked() {
						if opts.OnInteract != nil {
							opts.OnInteract()
						}
						st.ToggleLoop(target, now)
					}

					if opts.InLightbox && opts.OnToggleFullscreen != nil {
						fsIcon := svgVidExpand
						fsTip := theme.Tr("系统全屏", "System Fullscreen")
						if opts.IsFullscreen {
							fsIcon = svgVidExitExpand
							fsTip = theme.Tr("退出全屏", "Exit Fullscreen")
						}
						fsBtn := renderVideoGlyphButton(c, opts.ID+"-fullscreen", fsIcon, fsTip, "F", opts.IsFullscreen, darkDock)
						if fsBtn.Clicked() {
							if opts.OnInteract != nil {
								opts.OnInteract()
							}
							opts.OnToggleFullscreen()
						}
					} else if !opts.InLightbox && opts.OnExpand != nil {
						expBtn := renderVideoGlyphButton(c, opts.ID+"-expand", svgVidExpand, theme.Tr("放大观看", "Expand"), "F", false, darkDock)
						if expBtn.Clicked() {
							st.EndBoost(target)
							opts.OnExpand()
						}
					}

					if opts.OnOpenExternal != nil {
						openBtn := IconButton(c, icons.LinkOpen, IconButtonOptions{
							ID:       opts.ID + "-open-ext",
							Label:    theme.Tr("用默认播放器打开", "Open in default player"),
							Tooltip:  theme.Tr("用默认播放器打开", "Open in default player"),
							Size:     theme.IconButtonBox,
							IconSize: theme.IconInline,
						})
						if openBtn.Clicked() {
							player.Pause()
							opts.OnOpenExternal()
						}
					}
				})
			})

			// Inline compact speed picker tray when opened.
			if st.SpeedMenuOpen {
				curBase := st.effectiveBaseRate(target)
				trayBG := pal.Shell
				trayBorder := pal.Hairline
				trayMuted := pal.TextSecondary
				if darkDock {
					trayBG = pal.LightboxChromeBG()
					trayBorder = pal.LightboxChromeBorder()
					trayMuted = pal.LightboxChromeMutedFG()
				}
				ui.Row(c.Key(opts.ID+"-speed-tray")).FillWidth().
					Padding(theme.Space6, theme.Space10, theme.Space8, theme.Space10).
					Gap(theme.Space4).AlignItems(ui.Center).Justify(ui.End).
					Background(trayBG).BorderWidth(1, 0, 0, 0).BorderColor(trayBorder).Children(func() {
					ui.Text(c, theme.Tr("倍速", "Speed")).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(trayMuted).Margin(0, theme.Space6, 0, 0).SingleLine()
					for _, rate := range videoPlaybackRates {
						r := rate
						active := math.Abs(curBase-r) < 0.01
						lbl := formatPlaybackRate(r)
						optBtn := ui.ButtonBase(c.Key(fmt.Sprintf("%s-rate-%s", opts.ID, lbl))).
							Height(theme.HeightMicro).PaddingX(theme.Space8).Center().
							Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(lbl)
						bg := ui.Transparent
						fg := pal.TextSecondary
						if darkDock {
							fg = pal.LightboxChromeMutedFG()
							if active {
								bg = pal.LightboxChromeActive()
								fg = pal.LightboxChromeFG()
							} else if optBtn.Hovered() {
								bg = pal.LightboxChromeHover()
								fg = pal.LightboxChromeFG()
							}
						} else if active {
							bg = pal.Selected
							fg = pal.Accent
						} else if optBtn.Hovered() {
							bg = pal.Hover
							fg = pal.Text
						}
						AnimateSurfaceColors(optBtn, bg, ui.Transparent, false)
						optBtn.Children(func() {
							ui.Text(c, lbl).
								FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
								FontWeight(theme.WeightControl).FontFeatures("tnum").
								TextColor(fg).SingleLine()
						})
						if optBtn.Clicked() {
							st.SetBaseRate(target, r, now)
							st.SpeedMenuOpen = false
						}
					}
				})
			}
		})
	})

	return root
}

func renderVideoGlyphButton(c *ui.Context, id string, glyph *ui.SVG, tip, shortcut string, active, dark bool) ui.Element {
	pal := theme.Ocean()
	btn := ui.ButtonBase(c.Key(id)).
		Size(theme.IconButtonBox, theme.IconButtonBox).Center().
		Radius(theme.RadiusSM).Cursor(ui.CursorPointer).Label(tip)
	bg := ui.Transparent
	fg := pal.TextSecondary
	if dark {
		fg = pal.LightboxChromeMutedFG()
		if active {
			bg = pal.LightboxChromeActive()
			fg = pal.LightboxChromeFG()
		} else if btn.Pressed() {
			bg = pal.LightboxChromeActive()
			fg = pal.LightboxChromeFG()
		} else if btn.Hovered() {
			bg = pal.LightboxChromeHover()
			fg = pal.LightboxChromeFG()
		}
	} else {
		if active {
			bg = pal.Selected
			fg = pal.Accent
		} else if btn.Pressed() {
			bg = pal.Selected
			fg = pal.Text
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}
	}
	AnimateSurfaceColors(btn, bg, ui.Transparent, false)
	if tip != "" {
		if shortcut != "" {
			RichTooltip(c, btn, TooltipSpec{
				Placement: TooltipAbove,
				Rows:      []TooltipRow{{Trigger: tip, Shortcut: shortcut}},
			})
		} else {
			Tooltip(c, btn, tip)
		}
	}
	btn.Children(func() {
		if glyph != nil {
			ui.Icon(c, glyph).Size(theme.IconInline, theme.IconInline).TextColor(fg)
		}
	})
	return btn
}

func renderVideoScrubber(
	c *ui.Context,
	id string,
	pal theme.Palette,
	dark bool,
	target videoControlTarget,
	st *VideoControlState,
	pos time.Duration,
	dur time.Duration,
	onInteract func(),
) {
	scrubber := ui.Box(c.Key(id)).FillWidth().Height(theme.VideoScrubberHeight).
		PaddingX(theme.Space10).Cursor(ui.CursorPointer)
	bounds := scrubber.Bounds()
	px, _, hasPos := scrubber.PointerPosition()
	hovered := scrubber.Hovered()
	pressed := scrubber.Pressed()
	_, _, dragging := scrubber.Dragged()

	padX := theme.Space10
	trackW := max(bounds.W-2*padX, 1)
	hoverRatio := float64(0)
	if hasPos && bounds.W > 0 {
		hoverRatio = clampFloat64(float64(px-padX)/float64(trackW), 0, 1)
	}

	if dur > 0 && (pressed || dragging) {
		if !st.Scrubbing && onInteract != nil {
			onInteract()
		}
		st.Scrubbing = true
		if hasPos {
			st.ScrubTarget = time.Duration(float64(dur) * hoverRatio)
			target.Seek(st.ScrubTarget)
			pos = st.ScrubTarget
		}
	} else if st.Scrubbing {
		st.Scrubbing = false
		if dur > 0 {
			target.Seek(st.ScrubTarget)
		}
	}

	progress := float32(0)
	if dur > 0 {
		progress = float32(clampFloat64(float64(pos)/float64(dur), 0, 1))
	}

	expanded := hovered || st.Scrubbing
	trackCol := pal.Hairline
	hoverCol := pal.TextSecondary.Alpha(0.18)
	thumbRing := pal.Paper
	if dark {
		trackCol = pal.LightboxChromeTrack()
		hoverCol = pal.LightboxChromeFG().Alpha(0.32)
		thumbRing = pal.LightboxChromeFG()
	}
	scrubber.Draw(func(p *ui.Painter, r ui.Rect) {
		tw := max(r.W-2*padX, 1)
		tx := r.X + padX
		th := theme.VideoScrubberTrackH
		if expanded {
			th = theme.VideoScrubberTrackHoverH
		}
		ty := r.Y + (r.H-th)*0.5
		p.Fill(ui.Rect{X: tx, Y: ty, W: tw, H: th}, trackCol, th*0.5)
		if hovered && hasPos && dur > 0 {
			hw := tw * float32(hoverRatio)
			p.Fill(ui.Rect{X: tx, Y: ty, W: hw, H: th}, hoverCol, th*0.5)
		}
		fw := tw * progress
		if fw > 0 {
			p.Fill(ui.Rect{X: tx, Y: ty, W: fw, H: th}, pal.Accent, th*0.5)
		}
		if expanded || dur > 0 {
			thumbSize := theme.VideoScrubberThumbSize
			if !expanded {
				thumbSize = theme.VideoScrubberTrackHoverH + theme.Space2
			}
			cx := tx + fw
			thumbR := ui.Rect{
				X: cx - thumbSize*0.5,
				Y: r.Y + (r.H-thumbSize)*0.5,
				W: thumbSize,
				H: thumbSize,
			}
			p.Fill(thumbR, pal.Accent, thumbSize*0.5)
			if expanded {
				p.Stroke(thumbR, thumbRing, thumbSize*0.5, 1.5)
			}
		}
	})

	// Floating timestamp preview while hovering or scrubbing the timeline.
	if dur > 0 && hasPos && (hovered || st.Scrubbing) {
		previewTime := time.Duration(float64(dur) * hoverRatio)
		tipText := FormatMediaDuration(previewTime)
		tipW := float32(46)
		tipLeft := min(max(px-tipW*0.5, padX), max(bounds.W-padX-tipW, padX))
		scrubber.Children(func() {
			tip := ui.Row(c.Key(id+"-tip")).
				Absolute().Left(tipLeft).Top(-theme.HeightMicro-theme.Space2).
				Width(tipW).Height(theme.HeightMicro).Center().
				Radius(theme.RadiusXS).Background(pal.ImageOverlayBadgeBG()).IgnoreInput(true)
			tip.Children(func() {
				ui.Text(c, tipText).
					FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					FontFeatures("tnum").TextColor(pal.ImageOverlayBadgeFG()).SingleLine()
			})
		})
	}
}

func renderVideoVolumeSlider(
	c *ui.Context,
	id string,
	pal theme.Palette,
	dark bool,
	target videoControlTarget,
	st *VideoControlState,
	vol float64,
	muted bool,
	onInteract func(),
) {
	slider := ui.Box(c.Key(id)).Width(theme.VideoVolumeSliderW).Height(theme.IconButtonBox).
		Cursor(ui.CursorPointer)
	bounds := slider.Bounds()
	px, _, hasPos := slider.PointerPosition()
	hovered := slider.Hovered()
	pressed := slider.Pressed()
	_, _, dragging := slider.Dragged()

	padX := theme.Space4
	trackW := max(bounds.W-2*padX, 1)
	if pressed || dragging {
		if !st.VolScrubbing && onInteract != nil {
			onInteract()
		}
		st.VolScrubbing = true
		if hasPos && bounds.W > 0 {
			ratio := clampFloat64(float64(px-padX)/float64(trackW), 0, 1)
			st.SetVolumeDirect(target, ratio, c.Now())
			vol = ratio
			muted = ratio <= 0.001
		}
	} else {
		st.VolScrubbing = false
	}

	shownVol := float32(clampFloat64(vol, 0, 1))
	if muted {
		shownVol = 0
	}
	expanded := hovered || st.VolScrubbing
	trackCol := pal.Hairline
	thumbRing := pal.Paper
	if dark {
		trackCol = pal.LightboxChromeTrack()
		thumbRing = pal.LightboxChromeFG()
	}
	slider.Draw(func(p *ui.Painter, r ui.Rect) {
		tw := max(r.W-2*padX, 1)
		tx := r.X + padX
		th := theme.VideoScrubberTrackH
		ty := r.Y + (r.H-th)*0.5
		p.Fill(ui.Rect{X: tx, Y: ty, W: tw, H: th}, trackCol, th*0.5)
		fw := tw * shownVol
		if fw > 0 {
			p.Fill(ui.Rect{X: tx, Y: ty, W: fw, H: th}, pal.Accent, th*0.5)
		}
		thumbSize := theme.Space8
		if expanded {
			thumbSize = theme.Space10
		}
		cx := tx + fw
		thumbR := ui.Rect{
			X: cx - thumbSize*0.5,
			Y: r.Y + (r.H-thumbSize)*0.5,
			W: thumbSize,
			H: thumbSize,
		}
		p.Fill(thumbR, pal.Accent, thumbSize*0.5)
		if expanded {
			p.Stroke(thumbR, thumbRing, thumbSize*0.5, 1)
		}
	})
}
