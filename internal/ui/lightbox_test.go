package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
)

func makeTestPNGImage(t *testing.T, w, h int) ui.ImageSource {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0x3b, G: 0x6b, B: 0xaa, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	src, err := ui.DecodeBitmap(buf.Bytes())
	if err != nil {
		t.Fatalf("decode bitmap: %v", err)
	}
	return src
}

func makeTestVideoPlayer(t *testing.T, frames int) *ui.VideoPlayer {
	t.Helper()
	pal := color.Palette{
		color.RGBA{R: 0x20, G: 0x40, B: 0x80, A: 0xff},
		color.RGBA{R: 0x80, G: 0xa0, B: 0xd0, A: 0xff},
	}
	anim := &gif.GIF{}
	for i := 0; i < frames; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 160, 90), pal)
		anim.Image = append(anim.Image, p)
		anim.Delay = append(anim.Delay, 100) // 1s per frame
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	dec, err := ui.NewGIFDecoder(buf.Bytes())
	if err != nil {
		t.Fatalf("new gif decoder: %v", err)
	}
	player, err := ui.NewVideoPlayer(dec)
	if err != nil {
		t.Fatalf("new video player: %v", err)
	}
	t.Cleanup(player.Close)
	return player
}

func TestLightboxArrowNavigationZoomPanAndEscape(t *testing.T) {
	ResetSmoke()
	img1 := makeTestPNGImage(t, 1600, 1200)
	img2 := makeTestPNGImage(t, 1600, 1200)
	items := []LightboxItem{
		{ID: "img-1", Name: "one.png", IsImage: true, Image: img1, ImageW: 1600, ImageH: 1200},
		{ID: "img-2", Name: "two.png", IsImage: true, Image: img2, ImageW: 1600, ImageH: 1200},
	}
	st := LightboxState{Open: true, Index: 0}
	st.ResetView()

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill()
		RenderLightbox(c, "test-lb", &st, items, LightboxCallbacks{})
	}, 900, 640)
	tt.Frame()

	// Fit mode: Right/Left/Down/Up navigate between images.
	tt.Key(0, ui.KeyRight)
	if st.Index != 1 {
		t.Fatalf("KeyRight in fit mode: index=%d, want 1", st.Index)
	}
	tt.Key(0, ui.KeyLeft)
	if st.Index != 0 {
		t.Fatalf("KeyLeft in fit mode: index=%d, want 0", st.Index)
	}
	tt.Key(0, ui.KeyDown)
	if st.Index != 1 {
		t.Fatalf("KeyDown in fit mode: index=%d, want 1", st.Index)
	}
	tt.Key(0, ui.KeyUp)
	if st.Index != 0 {
		t.Fatalf("KeyUp in fit mode: index=%d, want 0", st.Index)
	}

	// Zoom to 100% (1600x1200 overflows 900x640 stage): arrow keys pan instead of switching items!
	tt.Key(0, ui.Key1)
	if st.FitMode || st.Zoom != 1.0 {
		t.Fatalf("Key1 should zoom to 100%%: fit=%v zoom=%v", st.FitMode, st.Zoom)
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyDown)
	if st.Index != 0 {
		t.Fatalf("arrows while zoomed should pan, not switch index (got %d)", st.Index)
	}
	if st.Scroll.X <= 0 || st.Scroll.Y <= 0 {
		t.Fatalf("expected positive pan offsets after Right+Down, got X=%v Y=%v (maxX=%v maxY=%v)",
			st.Scroll.X, st.Scroll.Y, st.Scroll.MaxX, st.Scroll.MaxY)
	}

	// Cmd+Right still switches gallery items while zoomed in.
	tt.Key(ui.Cmd, ui.KeyRight)
	if st.Index != 1 || !st.FitMode {
		t.Fatalf("Cmd+Right should switch item and reset fit: index=%d fit=%v", st.Index, st.FitMode)
	}

	// Escape closes lightbox cleanly.
	tt.Key(0, ui.KeyEscape)
	if st.Open {
		t.Fatalf("Escape should close lightbox")
	}
}

func TestLightboxVideoPlaybackShortcuts(t *testing.T) {
	ResetSmoke()
	player := makeTestVideoPlayer(t, 20) // 20s duration
	items := []LightboxItem{
		{ID: "vid-1", Name: "demo.mp4", IsVideo: true, Video: player},
		{ID: "img-2", Name: "shot.png", IsImage: true, Image: makeTestPNGImage(t, 400, 300), ImageW: 400, ImageH: 300},
	}
	st := LightboxState{Open: true, Index: 0}
	st.ResetView()

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill()
		RenderLightbox(c, "test-vid-lb", &st, items, LightboxCallbacks{})
	}, 960, 680)
	tt.Frame()

	// Tap Right seeks forward +5s (not switching item!).
	tt.Key(0, ui.KeyRight)
	if st.Index != 0 {
		t.Fatalf("KeyRight on active video should seek, not switch item (index=%d)", st.Index)
	}
	if st.Video.OSDText != "+5s" {
		t.Fatalf("expected +5s OSD after KeyRight, got %q", st.Video.OSDText)
	}

	// Tap Left rewinds -5s.
	tt.Key(0, ui.KeyLeft)
	if st.Video.OSDText != "-5s" {
		t.Fatalf("expected -5s OSD after KeyLeft, got %q", st.Video.OSDText)
	}

	// Up / Down adjust volume with OSD volume indicator.
	tt.Key(0, ui.KeyDown)
	if !st.Video.OSDHasVolume {
		t.Fatalf("expected volume OSD after KeyDown")
	}

	// Loop toggle (R).
	beforeLoop := player.Loop()
	tt.Key(0, ui.KeyR)
	if player.Loop() == beforeLoop {
		t.Fatalf("KeyR should toggle loop")
	}

	// Cmd+Right switches to next item and pauses outgoing video.
	tt.Key(ui.Cmd, ui.KeyRight)
	if st.Index != 1 {
		t.Fatalf("Cmd+Right should switch to item 1, got %d", st.Index)
	}
	if player.Playing() {
		t.Fatalf("switching away from video should pause outgoing video")
	}
}

func TestLightboxCrossPlatformTitlebarClearanceAndShortcuts(t *testing.T) {
	// Fullscreen / standard OS titlebar: zero clearance, compact 12px top gap.
	if got := lightboxTitlebarClearance(ui.TitleBar{}); got != 0 {
		t.Fatalf("fullscreen clearance = %v, want 0", got)
	}
	if got := lightboxToolbarTopGap(0); got != 12 {
		t.Fatalf("fullscreen top gap = %v, want 12", got)
	}

	// macOS hidden titlebar: clears traffic lights band.
	macTB := ui.TitleBar{Height: 40, Left: 78, Bottom: 28}
	if got := lightboxTitlebarClearance(macTB); got != 40 {
		t.Fatalf("macOS clearance = %v, want 40", got)
	}
	if got := lightboxToolbarTopGap(40); got != 6 {
		t.Fatalf("windowed top gap = %v, want 6", got)
	}

	// Windows hidden titlebar: clears right caption buttons band.
	winTB := ui.TitleBar{Height: 40, Right: 138, Bottom: 40}
	if got := lightboxTitlebarClearance(winTB); got != 40 {
		t.Fatalf("Windows clearance = %v, want 40", got)
	}

	// Fallback if a platform reports Left/Right insets without Height/Bottom.
	linuxFallbackTB := ui.TitleBar{Right: 96}
	if got := lightboxTitlebarClearance(linuxFallbackTB); got <= 0 {
		t.Fatalf("fallback clearance should be positive, got %v", got)
	}

	// Platform-aware arrow shortcuts parse and format cleanly ("⌘←" on macOS, "Ctrl+←" elsewhere).
	if got := FormatShortcutDisplay("cmd-left"); got != "⌘←" && got != "Ctrl+←" {
		t.Fatalf("FormatShortcutDisplay(cmd-left) = %q", got)
	}
	if got := FormatShortcutDisplay("cmd-right"); got != "⌘→" && got != "Ctrl+→" {
		t.Fatalf("FormatShortcutDisplay(cmd-right) = %q", got)
	}
}
