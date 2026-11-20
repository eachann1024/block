package ui

import (
	"testing"
	"time"
)

type fakeVideoTarget struct {
	playing  bool
	ended    bool
	duration time.Duration
	position time.Duration
	rate     float64
	volume   float64
	muted    bool
	loop     bool
}

func newFakeVideo(dur time.Duration) *fakeVideoTarget {
	return &fakeVideoTarget{
		playing:  true,
		duration: dur,
		rate:     1.0,
		volume:   0.8,
	}
}

func (f *fakeVideoTarget) Play()                  { f.playing = true; f.ended = false }
func (f *fakeVideoTarget) Pause()                 { f.playing = false }
func (f *fakeVideoTarget) Playing() bool          { return f.playing }
func (f *fakeVideoTarget) Ended() bool            { return f.ended }
func (f *fakeVideoTarget) Duration() time.Duration { return f.duration }
func (f *fakeVideoTarget) Position() time.Duration { return f.position }
func (f *fakeVideoTarget) Seek(d time.Duration) {
	f.position = d
	if d < f.duration {
		f.ended = false
	}
}
func (f *fakeVideoTarget) Rate() float64      { return f.rate }
func (f *fakeVideoTarget) SetRate(r float64)  { f.rate = r }
func (f *fakeVideoTarget) Volume() float64    { return f.volume }
func (f *fakeVideoTarget) SetVolume(v float64) { f.volume = v }
func (f *fakeVideoTarget) Muted() bool        { return f.muted }
func (f *fakeVideoTarget) SetMuted(m bool)    { f.muted = m }
func (f *fakeVideoTarget) Loop() bool         { return f.loop }
func (f *fakeVideoTarget) SetLoop(loop bool)  { f.loop = loop }

func TestVideoControlSeekClampAndEnded(t *testing.T) {
	now := time.Unix(1000, 0)
	v := newFakeVideo(30 * time.Second)
	v.position = 10 * time.Second
	st := &VideoControlState{BaseRate: 1.0, SavedVolume: 0.8}

	st.SeekBy(v, videoSeekStep, now)
	if v.position != 15*time.Second || st.OSDText != "+5s" {
		t.Fatalf("after +5s: pos=%v osd=%q", v.position, st.OSDText)
	}

	st.SeekBy(v, -20*time.Second, now)
	if v.position != 0 {
		t.Fatalf("expected clamp to 0, got %v", v.position)
	}

	st.SeekBy(v, 45*time.Second, now)
	if v.position != 30*time.Second {
		t.Fatalf("expected clamp to duration 30s, got %v", v.position)
	}

	// Rewinding from Ended state steps back from duration.
	v.ended = true
	v.playing = false
	st.SeekBy(v, -videoSeekStep, now)
	if v.position != 25*time.Second || v.ended {
		t.Fatalf("expected rewind from ended to 25s, got pos=%v ended=%v", v.position, v.ended)
	}

	// Ratio seek & frame step.
	st.SeekToRatio(v, 0.5, now, true)
	if v.position != 15*time.Second {
		t.Fatalf("expected 50%% ratio seek to 15s, got %v", v.position)
	}
	v.playing = true
	st.FrameStep(v, true, now)
	if v.playing || v.position != 15*time.Second+videoFrameStep {
		t.Fatalf("expected frame step paused at 15s+33ms, got playing=%v pos=%v", v.playing, v.position)
	}
}

func TestVideoControlHoldRightKey3xBoostAndRestore(t *testing.T) {
	t0 := time.Unix(2000, 0)
	v := newFakeVideo(60 * time.Second)
	v.position = 10 * time.Second
	v.rate = 1.5
	st := &VideoControlState{BaseRate: 1.5, SavedVolume: 0.8}

	// 1. Single-frame synthetic tap seeks immediately without boosting.
	wake, _ := st.BeginRightKey(v, false, t0)
	if wake || st.BoostActive || v.position != 15*time.Second || v.rate != 1.5 {
		t.Fatalf("single-frame tap: wake=%v boost=%v pos=%v rate=%v", wake, st.BoostActive, v.position, v.rate)
	}

	// 2. Short press (< 220ms) and release seeks +5s and keeps base rate.
	wake, wait := st.BeginRightKey(v, true, t0)
	if !wake || wait != videoRightHoldDelay {
		t.Fatalf("expected wake after %v, got wake=%v wait=%v", videoRightHoldDelay, wake, wait)
	}
	wake, _ = st.UpdateRightKey(v, true, t0.Add(100*time.Millisecond))
	if !wake || st.BoostActive || v.rate != 1.5 {
		t.Fatalf("at 100ms hold should not boost yet: wake=%v boost=%v rate=%v", wake, st.BoostActive, v.rate)
	}
	st.UpdateRightKey(v, false, t0.Add(120*time.Millisecond))
	if st.BoostActive || v.position != 20*time.Second || v.rate != 1.5 {
		t.Fatalf("short release should seek +5s to 20s: boost=%v pos=%v rate=%v", st.BoostActive, v.position, v.rate)
	}

	// 3. Long hold (>= 220ms) boosts to 3.0x and release restores 1.5x (not 1.0x!).
	st.BeginRightKey(v, true, t0)
	st.UpdateRightKey(v, true, t0.Add(videoRightHoldDelay))
	if !st.BoostActive || v.rate != 3.0 || !v.playing {
		t.Fatalf("expected 3.0x boost on hold, got boost=%v rate=%v playing=%v", st.BoostActive, v.rate, v.playing)
	}
	st.UpdateRightKey(v, false, t0.Add(500*time.Millisecond))
	if st.BoostActive || v.rate != 1.5 || v.position != 20*time.Second {
		t.Fatalf("expected restore to 1.5x without seeking, got boost=%v rate=%v pos=%v", st.BoostActive, v.rate, v.position)
	}

	// 4. Holding while paused temporarily plays at 3.0x and pauses again on release.
	v.Pause()
	st.BeginRightKey(v, true, t0)
	st.UpdateRightKey(v, true, t0.Add(videoRightHoldDelay+10*time.Millisecond))
	if !st.BoostActive || v.rate != 3.0 || !v.playing {
		t.Fatalf("hold while paused should play at 3x: boost=%v rate=%v playing=%v", st.BoostActive, v.rate, v.playing)
	}
	st.UpdateRightKey(v, false, t0.Add(600*time.Millisecond))
	if st.BoostActive || v.rate != 1.5 || v.playing {
		t.Fatalf("release after paused hold should restore rate=1.5 & paused: boost=%v rate=%v playing=%v", st.BoostActive, v.rate, v.playing)
	}
}

func TestVideoControlMouseRightHold3xBoostAndRestore(t *testing.T) {
	now := time.Unix(3000, 0)
	v := newFakeVideo(40 * time.Second)
	v.rate = 1.25
	st := &VideoControlState{BaseRate: 1.25, SavedVolume: 1.0}

	st.UpdateMouseBoost(v, true, now)
	if !st.BoostActive || st.BoostSource != videoBoostSourceMouse || v.rate != 3.0 {
		t.Fatalf("mouse right hold should engage 3x immediately: boost=%v src=%d rate=%v", st.BoostActive, st.BoostSource, v.rate)
	}

	st.UpdateMouseBoost(v, false, now.Add(400*time.Millisecond))
	if st.BoostActive || v.rate != 1.25 {
		t.Fatalf("mouse right release should restore 1.25x: boost=%v rate=%v", st.BoostActive, v.rate)
	}

	// EndBoost restores safely on lightbox close / item switch.
	st.UpdateMouseBoost(v, true, now)
	st.EndBoost(v)
	if st.BoostActive || v.rate != 1.25 {
		t.Fatalf("EndBoost should restore 1.25x: boost=%v rate=%v", st.BoostActive, v.rate)
	}
}

func TestVideoControlVolumeMuteSpeedLoopAndReplay(t *testing.T) {
	now := time.Unix(4000, 0)
	v := newFakeVideo(60 * time.Second)
	v.volume = 0.6
	st := &VideoControlState{BaseRate: 1.0, SavedVolume: 0.6}

	// Up/Down volume adjustments.
	st.AdjustVolume(v, videoVolumeStep, now)
	if v.volume != 0.7 || v.muted || !st.OSDHasVolume {
		t.Fatalf("volume +10%%: vol=%v muted=%v osdHasVol=%v", v.volume, v.muted, st.OSDHasVolume)
	}
	st.ToggleMute(v, now)
	if !v.muted {
		t.Fatalf("expected muted after ToggleMute")
	}
	// Increasing volume while muted unmutes cleanly.
	st.AdjustVolume(v, videoVolumeStep, now)
	if v.muted || v.volume != 0.1 {
		t.Fatalf("expected unmute to 0.1 on volume up, got muted=%v vol=%v", v.muted, v.volume)
	}
	// Toggle mute off restores saved volume.
	st.ToggleMute(v, now)
	st.ToggleMute(v, now)
	if v.muted || v.volume != 0.1 {
		t.Fatalf("expected unmute restore 0.1, got muted=%v vol=%v", v.muted, v.volume)
	}

	// Speed step & loop.
	st.StepRate(v, 1, now)
	if v.rate != 1.25 || st.BaseRate != 1.25 {
		t.Fatalf("expected StepRate(+1) -> 1.25x, got rate=%v base=%v", v.rate, st.BaseRate)
	}
	st.ToggleLoop(v, now)
	if !v.loop {
		t.Fatalf("expected loop=true")
	}

	// Replay from ended state.
	v.ended = true
	v.playing = false
	v.position = 60 * time.Second
	st.TogglePlayPause(v, now)
	if !v.playing || v.ended || v.position != 0 {
		t.Fatalf("expected replay from 0, got playing=%v ended=%v pos=%v", v.playing, v.ended, v.position)
	}

	if got := FormatMediaDuration(75 * time.Second); got != "01:15" {
		t.Fatalf("FormatMediaDuration(75s) = %q, want 01:15", got)
	}
	if got := FormatMediaDuration(3665 * time.Second); got != "1:01:05" {
		t.Fatalf("FormatMediaDuration(3665s) = %q, want 1:01:05", got)
	}
}
