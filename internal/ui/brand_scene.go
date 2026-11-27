package ui

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// BrandSceneID identifies one of the 8 time-of-day window top landscape scenes.
type BrandSceneID string

const (
	SceneDawn     BrandSceneID = "dawn"
	SceneMeadow   BrandSceneID = "meadow"
	SceneBreeze   BrandSceneID = "breeze"
	SceneDusk     BrandSceneID = "dusk"
	SceneDaybreak BrandSceneID = "daybreak"
	SceneFirefly  BrandSceneID = "firefly"
	SceneNight    BrandSceneID = "night"
	SceneMoon     BrandSceneID = "moon"

	BrandBandHeight float32 = 88
	BrandTileWidth  float32 = 260

	brandDesignWidth float32 = 1200
	brandSkyMaxWidth float32 = 2800
	brandOrbX        float32 = 190
)

// BrandDensity controls how many scene elements are drawn. Animation speed is always leisurely.
type BrandDensity uint8

const (
	BrandDensityRich    BrandDensity = iota // Every element, including the occasional passers-by.
	BrandDensityCalm                        // Landmarks plus a moderate amount of ambient detail.
	BrandDensityMinimal                     // Only the landscape, the orb and the landmark figures.
)

// brandSceneLeisurelyRate is the fixed unhurried animation clock speed for all brand scenes.
const brandSceneLeisurelyRate float32 = 0.64

// BrandMotion holds the runtime brand scene motion settings.
type BrandMotion struct {
	Density BrandDensity
}

// BrandScenePaintOpts configures how PaintBrandSceneBand renders and animates the top band.
type BrandScenePaintOpts struct {
	ShowOrb    bool
	ShowCat    bool
	SceneWidth float32 // sidebar artwork width; <= 0 paints only the fading sky backdrop
	Motion     BrandMotion
	PauseAnim  bool
	Now        time.Time
	SkinChrome bool     // a plugin theme skin overrides chrome ground/accents
	SkinAccent ui.Color // accent color of the active theme skin
}

// ResolveBrandMotion converts persisted BrandDeco preferences into runtime motion settings.
// The rich density option is hidden from settings for now; unset or legacy rich values fall back to calm.
func ResolveBrandMotion(deco model.BrandDeco) BrandMotion {
	switch deco.EffectiveDensity() {
	case model.BrandDecoDensityMinimal:
		return BrandMotion{Density: BrandDensityMinimal}
	default:
		return BrandMotion{Density: BrandDensityCalm}
	}
}

// BrandSceneSpec describes metadata for settings cards and scene selection.
type BrandSceneSpec struct {
	ID     BrandSceneID
	Dark   bool
	Hours  string
	nameZh string
	nameEn string
}

// Label returns the localized display name of the scene.
func (s BrandSceneSpec) Label() string {
	return theme.Tr(s.nameZh, s.nameEn)
}

var allBrandScenes = []BrandSceneSpec{
	{ID: SceneDawn, Dark: false, Hours: "05–10", nameZh: "晨雾", nameEn: "Morning Mist"},
	{ID: SceneMeadow, Dark: false, Hours: "10–15", nameZh: "草原", nameEn: "Meadow"},
	{ID: SceneBreeze, Dark: false, Hours: "15–18", nameZh: "微风", nameEn: "Breeze"},
	{ID: SceneDusk, Dark: false, Hours: "18–05", nameZh: "晚霞", nameEn: "Dusk"},
	{ID: SceneDaybreak, Dark: true, Hours: "04–07", nameZh: "破晓", nameEn: "Daybreak"},
	{ID: SceneFirefly, Dark: true, Hours: "07–20", nameZh: "萤火", nameEn: "Fireflies"},
	{ID: SceneNight, Dark: true, Hours: "20–01", nameZh: "星空", nameEn: "Starry Night"},
	{ID: SceneMoon, Dark: true, Hours: "01–04", nameZh: "新月", nameEn: "Crescent Moon"},
}

// AllBrandScenes returns all 8 brand scenes in canonical display order (4 light, 4 dark).
func AllBrandScenes() []BrandSceneSpec {
	out := make([]BrandSceneSpec, len(allBrandScenes))
	copy(out, allBrandScenes)
	return out
}

// LightBrandScenes returns the 4 light-theme brand scenes.
func LightBrandScenes() []BrandSceneSpec {
	out := make([]BrandSceneSpec, 4)
	copy(out, allBrandScenes[:4])
	return out
}

// DarkBrandScenes returns the 4 dark-theme brand scenes.
func DarkBrandScenes() []BrandSceneSpec {
	out := make([]BrandSceneSpec, 4)
	copy(out, allBrandScenes[4:])
	return out
}

// IsValidBrandScene reports whether id names one of the 8 scenes (optionally restricted to dark/light).
func IsValidBrandScene(id string, dark bool) bool {
	for _, sc := range allBrandScenes {
		if string(sc.ID) == id && sc.Dark == dark {
			return true
		}
	}
	return false
}

// LightSceneByHour picks the light scene for local hour h (0..23).
func LightSceneByHour(h int) BrandSceneID {
	switch {
	case h >= 5 && h < 10:
		return SceneDawn
	case h >= 10 && h < 15:
		return SceneMeadow
	case h >= 15 && h < 18:
		return SceneBreeze
	default:
		return SceneDusk
	}
}

// DarkSceneByHour picks the dark scene for local hour h (0..23).
func DarkSceneByHour(h int) BrandSceneID {
	switch {
	case h >= 4 && h < 7:
		return SceneDaybreak
	case h >= 7 && h < 20:
		return SceneFirefly
	case h >= 1 && h < 4:
		return SceneMoon
	default:
		return SceneNight
	}
}

// EffectiveBrandScenes returns the active light and dark scene IDs for time now.
func EffectiveBrandScenes(_ model.BrandDeco, now time.Time) (light, dark BrandSceneID) {
	h := now.Hour()
	return LightSceneByHour(h), DarkSceneByHour(h)
}

// ResolveBrandScene returns the active BrandSceneID to paint (or "" when disabled).
func ResolveBrandScene(deco model.BrandDeco, dark bool, now time.Time) BrandSceneID {
	if deco.Mode == model.BrandDecoOff {
		return ""
	}
	l, d := EffectiveBrandScenes(deco, now)
	if dark {
		return d
	}
	return l
}

// sceneColor parses scenic artwork colors used exclusively inside the decorative brand band.
func sceneColor(hex string) ui.Color {
	return ui.Hex(hex) // design:allow
}

// sceneProgress returns [0..1] progress within a scene's time-of-day window.
func sceneProgress(id BrandSceneID, now time.Time) float32 {
	if now.IsZero() {
		return 0.5
	}
	var lo, hi float64
	switch id {
	case SceneDawn:
		lo, hi = 5, 10
	case SceneMeadow:
		lo, hi = 10, 15
	case SceneBreeze:
		lo, hi = 15, 18
	case SceneDusk:
		lo, hi = 18, 20
	case SceneDaybreak:
		lo, hi = 4, 7
	case SceneMoon:
		lo, hi = 1, 4
	default:
		return 0.5
	}
	t := float64(now.Hour()) + float64(now.Minute())/60.0 + float64(now.Second())/3600.0
	if id == SceneDusk && (t > hi || t < 5) {
		return 1
	}
	if t < lo || t > hi {
		return 0.5
	}
	return float32((t - lo) / (hi - lo))
}

type sceneTimeVars struct {
	orbDy  float32
	glowOp float32
	starOp float32
}

func evalSceneTimeVars(id BrandSceneID, p float32) sceneTimeVars {
	v := sceneTimeVars{glowOp: 1, starOp: 1}
	switch id {
	case SceneDawn:
		v.orbDy = lerp(3, -9, p)
	case SceneMeadow:
		v.orbDy = -6 * float32(math.Sin(math.Pi*float64(p)))
	case SceneBreeze:
		v.orbDy = lerp(-4, 3, p)
	case SceneDusk:
		v.orbDy = lerp(-5, 7, p)
		v.starOp = lerp(0, 0.9, p)
	case SceneDaybreak:
		v.glowOp = lerp(0.5, 1.0, p)
		v.starOp = lerp(1.0, 0.3, p)
	case SceneMoon:
		v.orbDy = lerp(-3, 7, p)
	}
	return v
}

// -----------------------------------------------------------------------------
// Mathematical continuous terrain functions (non-tiling across any width)
// -----------------------------------------------------------------------------

type ridgeComp struct {
	amp, wl, phase float64
}

type ridgeCurve struct {
	base  float64
	comps []ridgeComp
	fill  ui.Color
}

func (r ridgeCurve) y(x float64) float32 {
	v := r.base
	for _, c := range r.comps {
		v -= c.amp * math.Sin(2*math.Pi*x/c.wl+c.phase)
	}
	return float32(v)
}

type peakCurve struct {
	base, amp, wl, phase, sharp float64
	fill                        ui.Color
}

func (p peakCurve) y(x float64) float32 {
	v1 := math.Pow(1-math.Abs(math.Sin(math.Pi*x/p.wl+p.phase)), p.sharp)
	v2 := math.Pow(1-math.Abs(math.Sin(math.Pi*x/(p.wl*0.37)+p.phase*2.3)), p.sharp)
	env := 0.55 + 0.45*math.Sin(2*math.Pi*x/977.0+p.phase*1.7)
	return float32(p.base - p.amp*env*(0.7*v1+0.3*v2))
}

// -----------------------------------------------------------------------------
// Precomputed deterministic scene primitives
// -----------------------------------------------------------------------------

type starTwinkleKind uint8

const (
	twinkleNone starTwinkleKind = iota
	twinkleSmooth
	twinkleIrregular
)

type sceneStar struct {
	x, y     float32
	r        float32
	color    ui.Color
	opacity  float32
	dim      float32
	twinkle  starTwinkleKind
	tier     uint8 // 0 always, 1 calm and richer, 2 rich only
	delaySec float32
	durSec   float32
}

type sceneBlade struct {
	x, y     float32
	h, lean  float32
	tier     uint8
	delaySec float32
	durSec   float32
}

type sceneDewDrop struct {
	x, y     float32
	delaySec float32
	durSec   float32
}

type mistAnimKind uint8

const (
	mistDriftRight mistAnimKind = iota // v-mist: translateX 0..24, op 0.4..0.9
	mistDriftLeft                      // dw-mb: translateX 0..-28, scaleX 1..1.14, op 0.85..0.35
	mistPulse                          // dw-mc: op 0.12..0.8
)

type sceneMistBand struct {
	x, y, w, h float32
	color      ui.Color
	baseOp     float32
	kind       mistAnimKind
	tier       uint8 // 0 always, 1 calm and richer, 2 rich only
	delaySec   float32
	durSec     float32
}

type sceneCloud struct {
	x, y, scale float32
	drift       float32 // horizontal sway in px
	tier        uint8
	delaySec    float32
	durSec      float32
}

type sceneFlower struct {
	x, y, r float32
	color   ui.Color
}

type sceneWindRibbon struct {
	approxL  float32
	endX     float32 // rightmost x the ribbon reaches
	color    ui.Color
	delaySec float32
	durSec   float32
}

type wanderWaypoint struct {
	x, y float32
}

type wanderTrack struct {
	pts      []wanderWaypoint
	restFrac float32
}

type driftWaypoint struct {
	x, y, rot, op float32
}

type sceneDriftItem struct {
	pts      []driftWaypoint
	delaySec float32
	durSec   float32
	color    ui.Color
	isLeaf   bool
	tier     uint8
}

type sceneLakeGlint struct {
	x, y, w  float32
	delaySec float32
	durSec   float32
}

type sceneLake struct {
	cx, cy, rx, ry float32
	fill           ui.Color
	glintColor     ui.Color
	glints         []sceneLakeGlint
}

type fireflyBlinkKind uint8

const (
	ffBlinkSingle fireflyBlinkKind = iota
	ffBlinkDouble
	ffBlinkSlow
)

type sceneFirefly struct {
	x, y          float32
	size          float32
	tier          uint8
	flipX         bool
	wanderIdx     int
	moveDelaySec  float32
	moveDurSec    float32
	blinkKind     fireflyBlinkKind
	blinkDelaySec float32
	blinkDurSec   float32
}

type sceneFlareStar struct {
	x, y, size float32
	delaySec   float32
	durSec     float32
}

type sceneMeteor struct {
	x0, y0     float32
	dx, dy     float32
	tailX      float32
	tailY      float32
	durSec     float32
	atFrac     float32
	delaySec   float32
	isNightMet bool
	tier       uint8
}

type sceneMoonWisp struct {
	y, scale float32
	tier     uint8
	minW     float32 // narrowest sidebar the wisp shows in; 0 means any
	delaySec float32
	durSec   float32
}

type sceneCatSpec struct {
	x, y     float32
	color    ui.Color
	sleep    bool
	flip     bool
	tailDur  float32
	lookDeg  float32
	hasEyes  bool
	eyeColor ui.Color
	hasZzz   bool
	zzzColor ui.Color
}

type sceneTerrainCache struct {
	widthBucket int
	peakPath    ui.Path
	farPath     ui.Path
	midPath     ui.Path
	nearPath    ui.Path
	hasMid      bool
	peakRim     ui.Path
	farRim      ui.Path
}

type compiledScene struct {
	id       BrandSceneID
	dark     bool
	ground   ui.Color
	skyStops []ui.GradientStop

	peak   peakCurve
	far    ridgeCurve
	mid    ridgeCurve
	hasMid bool
	near   ridgeCurve

	// Rim highlights on peak & far ridge (daybreak, moon)
	rimX0, rimX1 float32
	rimColor     ui.Color
	rimPeakOp    float32
	rimMidStop   float32
	rimMidOp     float32

	// Shared optional features
	stars       []sceneStar
	spill       []sceneStar // x is relative to the sidebar edge; trails into the titlebar
	mists       []sceneMistBand
	blades      []sceneBlade
	bladeColor  ui.Color
	blades2     []sceneBlade
	blade2Color ui.Color
	dewDrops    []sceneDewDrop
	cat         sceneCatSpec
	hasLake     bool
	lake        sceneLake

	// Meadow-specific
	clouds       []sceneCloud
	flowers      []sceneFlower
	butterflyTrk wanderTrack

	// Breeze-specific
	winds      []sceneWindRibbon
	driftItems []sceneDriftItem

	// Firefly-specific
	ffWanders   []wanderTrack
	fireflies   []sceneFirefly
	farFlies    []sceneFirefly
	pondReflY   [4]float32
	pondReflDel [4]float32
	pondReflDur [4]float32
	cattailDel  [5]float32
	cattailDur  [5]float32
	mushDel     [3]float32
	mushDur     [3]float32

	// Night-specific
	flares []sceneFlareStar

	// Meteors (night & moon)
	meteors []sceneMeteor

	// Moon-specific
	moonWisps []sceneMoonWisp

	// Cached continuous terrain paths keyed by quantized width
	terrainMu sync.Mutex
	terrain   sceneTerrainCache
}

var (
	compiledOnce sync.Once
	compiledByID map[BrandSceneID]*compiledScene

	// Titlebar controls pinned to the window's left edge: traffic lights, sidebar toggle, wordmark.
	// The back/forward arrows hug the sidebar's right edge instead, so paintStarField avoids them.
	staticAvoidRects = [][4]float32{
		{6, 9, 70, 31},
		{82, 8, 104, 32},
		{108, 6, 183, 34},
	}
)

func isSkyClear(x, y float32) bool {
	if math.Abs(float64(x-brandOrbX)) < 13 && math.Abs(float64(y-38)) < 13 {
		return false
	}
	for _, r := range staticAvoidRects {
		if x > r[0] && x < r[2] && y > r[1] && y < r[3] {
			return false
		}
	}
	return true
}

func makeTuftXs(rng *rand.Rand, lo, hi float32, n, perMin, perMax int, spread float32) []float32 {
	xs := make([]float32, 0, n*perMax)
	for range n {
		c := lo + rng.Float32()*(hi-lo)
		cnt := perMin
		if perMax > perMin {
			cnt += rng.Intn(perMax - perMin + 1)
		}
		for range cnt {
			xs = append(xs, c+float32(rng.NormFloat64())*(spread*0.5))
		}
	}
	// Insertion sort keeps blades nicely ordered left-to-right.
	for i := 1; i < len(xs); i++ {
		v := xs[i]
		j := i - 1
		for j >= 0 && xs[j] > v {
			xs[j+1] = xs[j]
			j--
		}
		xs[j+1] = v
	}
	return xs
}

func makeBlades(rng *rand.Rand, yf func(float64) float32, xs []float32, hMin, hMax, leanMin, leanMax float32, animMode string) ([]sceneBlade, [][2]float32) {
	out := make([]sceneBlade, 0, len(xs))
	tips := make([][2]float32, 0, len(xs))
	for _, x := range xs {
		h := hMin + rng.Float32()*(hMax-hMin)
		lean := leanMin + rng.Float32()*(leanMax-leanMin)
		y := yf(float64(x)) + 1.2
		b := sceneBlade{x: x, y: y, h: h, lean: lean, tier: uint8(len(out) % 3)}
		switch animMode {
		case "gust":
			rem := math.Mod(float64(x)/46.0, 7.0)
			if rem < 0 {
				rem += 7.0
			}
			b.delaySec = float32(7.0 - rem)
			b.durSec = 7.0
		case "flut":
			b.delaySec = rng.Float32() * 2.0
			b.durSec = 1.8 + rng.Float32()*1.4
		case "reed":
			b.delaySec = rng.Float32() * 8.0
			b.durSec = 6.0 + rng.Float32()*3.0
		}
		out = append(out, b)
		tips = append(tips, [2]float32{x + lean, y - h})
	}
	return out, tips
}

func makeStarsV2(rng *rand.Rand, count1200 int, yMin, yMax, twProb float32, colors []ui.Color, dimFn func(x, y float32) float32) []sceneStar {
	// Generate exact count across [4, 1200-4] plus seamless continuation across [1200, brandSkyMaxWidth-4] for ultrawide displays.
	extra := int(float32(count1200) * (brandSkyMaxWidth - brandDesignWidth) / brandDesignWidth)
	total := count1200 + extra
	out := make([]sceneStar, 0, total)
	for i := range total {
		var x float32
		if i < count1200 {
			x = 4 + rng.Float32()*(brandDesignWidth-8)
		} else {
			x = brandDesignWidth + rng.Float32()*(brandSkyMaxWidth-brandDesignWidth-4)
		}
		y := yMin + rng.Float32()*(yMax-yMin)
		u := rng.Float32()
		r := 0.38 + 0.78*u*u*u
		op := 0.30 + 0.65*float32(math.Pow(float64(u), 1.5))
		col := colors[rng.Intn(len(colors))]
		twk := rng.Float32() < twProb
		irreg := rng.Intn(3) == 2
		dl := rng.Float32() * 6.0
		du := 2.6 + rng.Float32()*3.8
		if !isSkyClear(x, y) {
			continue
		}
		dim := float32(1.0)
		if dimFn != nil {
			dim = dimFn(x, y)
		}
		st := sceneStar{
			x:       x,
			y:       y,
			r:       r,
			color:   col,
			opacity: op,
			dim:     dim,
			tier:    uint8(len(out) % 3),
		}
		if twk {
			if irreg {
				st.twinkle = twinkleIrregular
				st.durSec = du * 2.2
			} else {
				st.twinkle = twinkleSmooth
				st.durSec = du
			}
			st.delaySec = dl
		}
		out = append(out, st)
	}
	return out
}

// makeSidebarStars adds stars to the strip a docked sidebar can actually show: makeStarsV2 spreads
// its count over the whole design width, so only a fraction ever lands there. They keep to the
// lanes above and below the titlebar controls and start at the calm tier, which leaves the
// minimal density with the original sparse sky.
func makeSidebarStars(rng *rand.Rand, n int, yMax, twProb float32, colors []ui.Color, dimFn func(x, y float32) float32) []sceneStar {
	out := make([]sceneStar, 0, n)
	for i := range n {
		x := 4 + rng.Float32()*416
		y := 2.5 + rng.Float32()*5.5
		if rng.Float32() < 0.62 {
			y = 30 + rng.Float32()*(yMax-30)
		}
		u := rng.Float32()
		st := sceneStar{
			x:       x,
			y:       y,
			r:       0.36 + 0.62*u*u*u,
			color:   colors[rng.Intn(len(colors))],
			opacity: 0.30 + 0.6*float32(math.Pow(float64(u), 1.5)),
			dim:     1,
			tier:    uint8(1 + i%2),
		}
		if rng.Float32() < twProb {
			st.twinkle = twinkleSmooth
			st.delaySec = rng.Float32() * 6.0
			st.durSec = 3.4 + rng.Float32()*4.2
		}
		if !isSkyClear(x, y) {
			continue
		}
		if dimFn != nil {
			st.dim = dimFn(x, y)
		}
		out = append(out, st)
	}
	return out
}

// makeSpill scatters a thinning trail of stars past the sidebar's right edge. x is relative to
// that edge, density and brightness fall off with distance, and most stars keep to the strips
// above and below the titlebar's text line so they read as an accent rather than noise.
func makeSpill(rng *rand.Rand, colors []ui.Color, width float32, n int) []sceneStar {
	out := make([]sceneStar, 0, n)
	for i := range n {
		u := float32(math.Pow(float64(rng.Float32()), 1.6))
		lane := rng.Float32()
		v := rng.Float32()
		st := sceneStar{
			x:       -10 + u*width,
			r:       0.38 + 0.7*v*v*v,
			color:   colors[rng.Intn(len(colors))],
			opacity: 0.35 + 0.6*v,
			dim:     float32(math.Pow(float64(1-u), 1.3)),
			tier:    uint8(i % 3),
		}
		switch {
		case lane < 0.45:
			st.y = 2.5 + rng.Float32()*5.5
		case lane < 0.80:
			st.y = 30 + rng.Float32()*7
		default:
			st.y = 9 + rng.Float32()*20
			st.dim *= 0.5
		}
		if rng.Float32() < 0.4 {
			st.twinkle = twinkleSmooth
			st.delaySec = rng.Float32() * 6.0
			st.durSec = 2.6 + rng.Float32()*3.8
		}
		out = append(out, st)
	}
	return out
}

// makeDustLane scatters a faint diagonal lane of star dust that starts just inside the sidebar
// edge and climbs away across the titlebar, like a far-off arm of the Milky Way. x is relative to
// the sidebar edge, and the lane dims where it crosses the titlebar's text line.
func makeDustLane(rng *rand.Rand, colors []ui.Color, n int) []sceneStar {
	out := make([]sceneStar, 0, n)
	for range n {
		t := rng.Float32()
		st := sceneStar{
			x:       -34 + 200*t + float32(rng.NormFloat64())*7,
			y:       40 - 36*t + float32(rng.NormFloat64())*3.5,
			r:       0.26 + rng.Float32()*0.24,
			color:   colors[rng.Intn(len(colors))],
			opacity: 0.20 + rng.Float32()*0.40,
			dim:     1 - 0.55*t,
			tier:    2,
		}
		if st.y < 1.5 || st.y > 44 {
			continue
		}
		if st.y > 9 && st.y < 30 {
			st.dim *= 0.5
		}
		out = append(out, st)
	}
	return out
}

func makeWanderTrack(rng *rand.Rand, n int, rx, ry, restFrac float32) wanderTrack {
	pts := make([]wanderWaypoint, n+1)
	for i := 1; i < n; i++ {
		pts[i] = wanderWaypoint{
			x: (rng.Float32()*2 - 1) * rx,
			y: (rng.Float32()*2 - 1) * ry,
		}
	}
	return wanderTrack{pts: pts, restFrac: restFrac}
}

func makeDriftItem(rng *rand.Rand, x0, y0, endX float32, n int, yLo, yHi, rotMax, peak, delay, dur float32, col ui.Color, isLeaf bool) sceneDriftItem {
	pts := make([]driftWaypoint, n+1)
	for i := 0; i <= n; i++ {
		t := float32(i) / float32(n)
		x := x0 + (endX-x0)*t
		if i > 0 && i < n {
			x += (rng.Float32()*2 - 1) * 18
		}
		y := y0
		if i > 0 {
			y = yLo + rng.Float32()*(yHi-yLo)
		}
		op := peak
		if i == 0 || i == n {
			op = 0
		}
		pts[i] = driftWaypoint{
			x:   x,
			y:   y,
			rot: (rng.Float32()*2 - 1) * rotMax,
			op:  op,
		}
	}
	return sceneDriftItem{
		pts:      pts,
		delaySec: delay,
		durSec:   dur,
		color:    col,
		isLeaf:   isLeaf,
	}
}

func makeLakeGlints(rng *rand.Rand, cy, gx float32) []sceneLakeGlint {
	specs := [][2]float32{{-1.7, 5}, {-0.8, 9}, {0.1, 12}, {1.0, 8}, {1.8, 4}}
	out := make([]sceneLakeGlint, len(specs))
	for i, sp := range specs {
		out[i] = sceneLakeGlint{
			x:        gx - sp[1]*0.5 + (rng.Float32()*4 - 2),
			y:        cy + sp[0],
			w:        sp[1],
			delaySec: rng.Float32() * 2.0,
			durSec:   1.6 + rng.Float32()*1.4,
		}
	}
	return out
}

func ensureCompiledScenes() {
	compiledOnce.Do(func() {
		lightGround := sceneColor("#f1f1f1")
		darkGround := sceneColor("#222222")
		nightSky := []ui.GradientStop{
			{Color: sceneColor("#1b2131"), At: 0},
			{Color: darkGround.Alpha(0), At: 1},
		}
		starCols := []ui.Color{
			sceneColor("#dfe6f5"), sceneColor("#dfe6f5"), sceneColor("#dfe6f5"),
			sceneColor("#dfe6f5"), sceneColor("#dfe6f5"), sceneColor("#dfe6f5"),
			sceneColor("#f5ead8"), sceneColor("#cfdcff"), sceneColor("#ffe9c9"),
		}
		duskStarCols := []ui.Color{
			sceneColor("#ffffff"), sceneColor("#fff4e6"), sceneColor("#f3eaff"),
		}

		compiledByID = map[BrandSceneID]*compiledScene{
			SceneDawn:     compileDawn(lightGround),
			SceneMeadow:   compileMeadow(lightGround),
			SceneBreeze:   compileBreeze(lightGround),
			SceneDusk:     compileDusk(lightGround, duskStarCols),
			SceneDaybreak: compileDaybreak(darkGround, nightSky, starCols),
			SceneFirefly:  compileFirefly(darkGround, nightSky, starCols),
			SceneNight:    compileNight(darkGround, nightSky, starCols),
			SceneMoon:     compileMoon(darkGround, nightSky, starCols),
		}
	})
}

func compileDawn(ground ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(11))
	pk := peakCurve{base: 45, amp: 17, wl: 263, phase: 0.6, sharp: 1.7, fill: sceneColor("#e8ecec")}
	far := ridgeCurve{base: 43, comps: []ridgeComp{{4.5, 410, 0.4}, {2.5, 173, 1.9}, {1.4, 67, 0.7}}, fill: sceneColor("#e7ece3")}
	mid := ridgeCurve{base: 53, comps: []ridgeComp{{4, 330, 2.2}, {2, 121, 0.3}}, fill: sceneColor("#dce6d6")}
	near := ridgeCurve{base: 63, comps: []ridgeComp{{2.6, 260, 1.1}, {1.2, 93, 2.5}}, fill: sceneColor("#d2e0cb")}

	bl, tips := makeBlades(rng, near.y, makeTuftXs(rng, 6, 258, 9, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "")
	dewDurs := []float32{7.3, 9.1, 11.7, 13.3}
	dews := make([]sceneDewDrop, 0, len(tips)/2+1)
	for i, pt := range tips {
		// Dew sits on the calm-tier blades, so it thins out together with the grass.
		if i%3 == 1 {
			dews = append(dews, sceneDewDrop{
				x:        pt[0],
				y:        pt[1],
				delaySec: rng.Float32() * 12.0,
				durSec:   dewDurs[rng.Intn(len(dewDurs))],
			})
		}
	}
	white := sceneColor("#ffffff")
	return &compiledScene{
		id:     SceneDawn,
		ground: ground,
		skyStops: []ui.GradientStop{
			{Color: sceneColor("#fbe9dc"), At: 0},
			{Color: ground.Alpha(0), At: 1},
		},
		peak:   pk,
		far:    far,
		mid:    mid,
		hasMid: true,
		near:   near,
		mists: []sceneMistBand{
			// 0: behind sun/far ridge
			{x: 250, y: 39, w: 420, h: 5, color: white, baseOp: 0.42, kind: mistPulse, tier: 1, delaySec: 14, durSec: 33},
			// 1..5: between far & mid ridges (plus wide-screen extension)
			{x: -30, y: 37, w: 180, h: 7, color: white, baseOp: 0.42, kind: mistDriftRight, tier: 1, delaySec: 5, durSec: 23},
			{x: 430, y: 32, w: 380, h: 5, color: white, baseOp: 0.42, kind: mistDriftLeft, tier: 1, delaySec: 11, durSec: 31},
			{x: 890, y: 34, w: 250, h: 6, color: white, baseOp: 0.42, kind: mistPulse, tier: 1, delaySec: 3, durSec: 19},
			{x: 640, y: 24, w: 150, h: 3.5, color: white, baseOp: 0.42, kind: mistPulse, tier: 1, delaySec: 9, durSec: 27},
			{x: 1220, y: 33, w: 340, h: 5.5, color: white, baseOp: 0.4, kind: mistDriftLeft, tier: 1, delaySec: 7, durSec: 29},
			// 6: between mid & near ridges
			{x: 60, y: 50, w: 150, h: 8, color: white, baseOp: 0.42, kind: mistDriftLeft, tier: 1, delaySec: 17, durSec: 37},
			// 7: foreground mist
			{x: -20, y: 61, w: 150, h: 7, color: white, baseOp: 0.42, kind: mistDriftRight, tier: 2, delaySec: 20, durSec: 41},
		},
		blades:     bl,
		bladeColor: sceneColor("#b2cca8"),
		dewDrops:   dews,
		cat: sceneCatSpec{
			x: 152, y: mid.y(152) + 0.8,
			color:   sceneColor("#a3b39b"),
			tailDur: 6.2,
		},
	}
}

func compileMeadow(ground ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(23))
	pk := peakCurve{base: 46, amp: 13, wl: 341, phase: 1.9, sharp: 1.1, fill: sceneColor("#e9eee6")}
	far := ridgeCurve{base: 43, comps: []ridgeComp{{5, 380, 2.8}, {2.2, 151, 0.2}, {1.2, 59, 1.4}}, fill: sceneColor("#e2eadf")}
	mid := ridgeCurve{base: 51, comps: []ridgeComp{{4.5, 300, 0.9}, {2, 133, 2.4}}, fill: sceneColor("#d6e4d0")}
	near := ridgeCurve{base: 61, comps: []ridgeComp{{3, 240, 2.0}, {1.3, 101, 0.6}}, fill: sceneColor("#cadcc2")}

	bt := makeWanderTrack(rng, 6, 34, 7, 0.14)
	flowerCols := []ui.Color{sceneColor("#ffffff"), sceneColor("#f6dc8c"), sceneColor("#f1c9d3")}
	flowers := make([]sceneFlower, 15)
	for i := range flowers {
		x := 8 + rng.Float32()*278
		yf := near.y
		if rng.Intn(3) == 0 {
			yf = mid.y
		}
		flowers[i] = sceneFlower{
			x:     x,
			y:     yf(float64(x)) + 1.6 + rng.Float32()*2.4,
			r:     0.7 + rng.Float32()*0.4,
			color: flowerCols[rng.Intn(len(flowerCols))],
		}
	}
	b1, _ := makeBlades(rng, near.y, makeTuftXs(rng, 2, 298, 11, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "gust")
	b2, _ := makeBlades(rng, near.y, makeTuftXs(rng, 6, 294, 5, 2, 4, 5.0), 5.0, 9.0, -1.6, 1.6, "gust")

	return &compiledScene{
		id:           SceneMeadow,
		ground:       ground,
		peak:         pk,
		far:          far,
		mid:          mid,
		hasMid:       true,
		near:         near,
		butterflyTrk: bt,
		clouds: []sceneCloud{
			// A small low cloud that fits inside the sidebar, under the titlebar controls
			{x: 96, y: 31.5, scale: 0.62, drift: 16, tier: 1, delaySec: 0, durSec: 64},
			{x: 462, y: 9, scale: 1.0, drift: 30, delaySec: 12, durSec: 71},
			{x: 836, y: 3, scale: 1.35, drift: 30, delaySec: 40, durSec: 97},
			{x: 1012, y: 19, scale: 0.7, drift: 30, delaySec: 5, durSec: 59},
			{x: 281, y: 1.5, scale: 0.6, drift: 30, tier: 2, delaySec: 22, durSec: 83},
			// Wide-screen continuation clouds
			{x: 1320, y: 7, scale: 1.05, drift: 30, delaySec: 18, durSec: 79},
			{x: 1680, y: 14, scale: 0.85, drift: 30, delaySec: 33, durSec: 67},
			{x: 2040, y: 5, scale: 1.2, drift: 30, delaySec: 9, durSec: 89},
		},
		flowers:     flowers,
		blades:      b1,
		bladeColor:  sceneColor("#a9c69f"),
		blades2:     b2,
		blade2Color: sceneColor("#9bbd90"),
		cat: sceneCatSpec{
			x: 150, y: mid.y(150) + 0.8,
			color:   sceneColor("#8fae86"),
			tailDur: 3.4,
		},
	}
}

func compileBreeze(ground ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(37))
	pk := peakCurve{base: 47, amp: 12, wl: 297, phase: 2.7, sharp: 1.2, fill: sceneColor("#eaefe7")}
	far := ridgeCurve{base: 44, comps: []ridgeComp{{4.5, 440, 1.6}, {2.4, 163, 0.9}, {1.2, 71, 2.9}}, fill: sceneColor("#e3ebdf")}
	mid := ridgeCurve{base: 54, comps: []ridgeComp{{3.5, 310, 0.2}, {1.8, 127, 1.7}}, fill: sceneColor("#dde8d7")}
	near := ridgeCurve{base: 62, comps: []ridgeComp{{2.6, 250, 2.6}, {1.2, 97, 0.4}}, fill: sceneColor("#d7e5d1")}

	windSpecs := []struct {
		approxL float32
		endX    float32
		hex     string
		delay   float32
		dur     float32
	}{
		{170, 184, "#b7d1ae", 1.0, 9.5},
		{230, 512, "#c3d6ec", 4.2, 11.0},
		{160, 762, "#b7d1ae", 6.5, 8.3},
		{165, 924, "#c3d6ec", 2.4, 13.7},
		{220, 1113, "#b7d1ae", 9.0, 12.1},
		{140, 176, "#c3d6ec", 5.2, 10.3},
		{110, 794, "#c3d6ec", 12.0, 15.3},
		// Wide-screen wind ribbons
		{145, 1392, "#c3d6ec", 3.4, 11.4},
		{220, 1741, "#b7d1ae", 7.8, 13.1},
	}
	winds := make([]sceneWindRibbon, len(windSpecs))
	for i, ws := range windSpecs {
		winds[i] = sceneWindRibbon{
			approxL:  ws.approxL,
			endX:     ws.endX,
			color:    sceneColor(ws.hex),
			delaySec: ws.delay,
			durSec:   ws.dur,
		}
	}
	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 4, 294, 12, 2, 5, 5.0), 4.0, 7.5, 1.2, 3.4, "flut")

	// Seeds lift off the dandelions and leaves tumble through below the titlebar controls; the
	// painter stretches each flight to end at the sidebar edge.
	seedCol := sceneColor("#a3b39c")
	seedSpecs := [][5]float32{ // x0, y0, dur, delay, tier
		{63, 50, 34, 2, 1},
		{63, 50, 47, 19, 2},
		{207, 48, 29, 11, 2},
		{63, 50, 39, 30, 2},
	}
	drifts := make([]sceneDriftItem, 0, len(seedSpecs)+3)
	for _, sp := range seedSpecs {
		it := makeDriftItem(rng, sp[0], sp[1], 262, 4, 29, 43, 34, 0.9, sp[3], sp[2], seedCol, false)
		it.tier = uint8(sp[4])
		drifts = append(drifts, it)
	}
	leafSpecs := []struct {
		hex   string
		dur   float32
		delay float32
	}{
		{"#b7d1ae", 34, 6},
		{"#d8cba6", 41, 17},
		{"#c5d9bd", 49, 3},
	}
	for _, ls := range leafSpecs {
		y0 := 30 + rng.Float32()*10
		it := makeDriftItem(rng, -12, y0, 262, 5, 28, 44, 220, 0.9, ls.delay, ls.dur, sceneColor(ls.hex), true)
		it.tier = 2
		drifts = append(drifts, it)
	}
	return &compiledScene{
		id:         SceneBreeze,
		ground:     ground,
		peak:       pk,
		far:        far,
		mid:        mid,
		hasMid:     true,
		near:       near,
		winds:      winds,
		blades:     bl,
		bladeColor: sceneColor("#a9c69f"),
		driftItems: drifts,
		cat: sceneCatSpec{
			x: 152, y: mid.y(152) + 0.8,
			color:   sceneColor("#98b190"),
			tailDur: 3.2,
		},
	}
}

func compileDusk(ground ui.Color, starCols []ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(41))
	pk := peakCurve{base: 47, amp: 18, wl: 229, phase: 0.3, sharp: 2.1, fill: sceneColor("#ebe1e0")}
	far := ridgeCurve{base: 45, comps: []ridgeComp{{4.5, 400, 3.3}, {2.3, 157, 1.2}, {1.2, 63, 0.1}}, fill: sceneColor("#e8e0d8")}
	mid := ridgeCurve{base: 55, comps: []ridgeComp{{4, 320, 1.4}, {1.8, 119, 2.7}}, fill: sceneColor("#ddd6cb")}
	near := ridgeCurve{base: 65, comps: []ridgeComp{{2.2, 250, 0.3}, {1.0, 91, 1.9}}, fill: sceneColor("#d3cfc0")}

	sts := makeStarsV2(rng, 70, 2.5, 40, 0.5, starCols, nil)
	sts = append(sts, makeSidebarStars(rand.New(rand.NewSource(1041)), 40, 40, 0.5, starCols, nil)...)
	lk := sceneLake{
		cx: 178, cy: 60.5, rx: 46, ry: 3.3,
		fill:       sceneColor("#ecdcd3"),
		glintColor: sceneColor("#f4a877"),
		glints:     makeLakeGlints(rng, 60.5, 188),
	}
	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 10, 252, 7, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "")

	return &compiledScene{
		id:     SceneDusk,
		ground: ground,
		skyStops: []ui.GradientStop{
			{Color: sceneColor("#f6dfd2"), At: 0},
			{Color: sceneColor("#efe3ee"), At: 0.45},
			{Color: ground.Alpha(0), At: 1},
		},
		peak:       pk,
		far:        far,
		mid:        mid,
		hasMid:     true,
		near:       near,
		stars:      sts,
		hasLake:    true,
		lake:       lk,
		blades:     bl,
		bladeColor: sceneColor("#bdb6a2"),
		cat: sceneCatSpec{
			x: 128, y: mid.y(128) + 0.8,
			color:   sceneColor("#b3a896"),
			tailDur: 5.4,
		},
	}
}

func compileDaybreak(ground ui.Color, nightSky []ui.GradientStop, starCols []ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(53))
	pk := peakCurve{base: 50, amp: 19, wl: 247, phase: 1.3, sharp: 2.0, fill: sceneColor("#252733")}
	far := ridgeCurve{base: 54, comps: []ridgeComp{{4.5, 390, 0.8}, {2.2, 149, 2.1}, {1.1, 61, 0.5}}, fill: sceneColor("#2a2a33")}
	near := ridgeCurve{base: 63, comps: []ridgeComp{{3, 270, 2.4}, {1.2, 99, 1.0}}, fill: sceneColor("#23242b")}

	dimFn := func(x, y float32) float32 {
		d := float32(math.Hypot(float64(x-brandOrbX), float64((y-58)*3)))
		return clampF((d-90)/560, 0.12, 1.0)
	}
	sts := makeStarsV2(rng, 120, 3, 42, 0.45, starCols, dimFn)
	sts = append(sts, makeSidebarStars(rand.New(rand.NewSource(1053)), 30, 42, 0.45, starCols, dimFn)...)
	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 10, 254, 7, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "")
	mistCol := sceneColor("#cfd6e6")

	return &compiledScene{
		id:         SceneDaybreak,
		dark:       true,
		ground:     ground,
		skyStops:   nightSky,
		peak:       pk,
		far:        far,
		near:       near,
		rimX0:      24,
		rimX1:      432,
		rimColor:   sceneColor("#f2b48a"),
		rimPeakOp:  0.6,
		rimMidStop: 0.4,
		rimMidOp:   0.6,
		stars:      sts,
		spill:      makeSpill(rand.New(rand.NewSource(2053)), starCols, 120, 16),
		mists: []sceneMistBand{
			{x: -30, y: 56, w: 210, h: 6, color: mistCol, baseOp: 0.09, kind: mistDriftRight, tier: 1, delaySec: 8, durSec: 29},
			{x: 40, y: 64, w: 170, h: 5, color: mistCol, baseOp: 0.09, kind: mistDriftRight, tier: 1, delaySec: 20, durSec: 37},
		},
		blades:     bl,
		bladeColor: sceneColor("#2d2f38"),
		cat: sceneCatSpec{
			x: 150, y: far.y(150) + 0.8,
			color: sceneColor("#1b1c22"),
			sleep: true,
		},
	}
}

func compileFirefly(ground ui.Color, nightSky []ui.GradientStop, starCols []ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(67))
	pk := peakCurve{base: 49, amp: 15, wl: 311, phase: 2.2, sharp: 1.4, fill: sceneColor("#222830")}
	far := ridgeCurve{base: 52, comps: []ridgeComp{{4.5, 370, 1.9}, {2.2, 141, 0.6}, {1.1, 57, 2.2}}, fill: sceneColor("#252b31")}
	near := ridgeCurve{base: 63, comps: []ridgeComp{{2.6, 255, 0.5}, {1.2, 103, 2.0}}, fill: sceneColor("#1f2428")}

	wanders := make([]wanderTrack, 6)
	wCounts := []int{5, 6, 7}
	for i := range wanders {
		wanders[i] = makeWanderTrack(rng, wCounts[rng.Intn(len(wCounts))], 10, 6, 0)
	}
	sts := makeStarsV2(rng, 70, 3, 42, 0.4, starCols, nil)
	sts = append(sts, makeSidebarStars(rand.New(rand.NewSource(1067)), 25, 42, 0.4, starCols, nil)...)

	mkFly := func(x, y, size float32) sceneFirefly {
		bk := fireflyBlinkKind(rng.Intn(3))
		baseDur := float32(3.4)
		switch bk {
		case ffBlinkDouble:
			baseDur = 5.3
		case ffBlinkSlow:
			baseDur = 6.7
		}
		return sceneFirefly{
			x:             x,
			y:             y,
			size:          size,
			flipX:         rng.Intn(2) == 1,
			wanderIdx:     rng.Intn(len(wanders)),
			moveDelaySec:  rng.Float32() * 12.0,
			moveDurSec:    9.0 + rng.Float32()*10.0,
			blinkKind:     bk,
			blinkDelaySec: rng.Float32() * 6.0,
			blinkDurSec:   baseDur * (0.9 + rng.Float32()*0.25),
		}
	}

	farCoords := [][2]float32{
		{30, 47}, {84, 45}, {146, 48}, {204, 44}, {244, 49},
		{338, 36}, {612, 37}, {834, 36}, {1022, 37},
		{1340, 36}, {1690, 35},
	}
	farFlies := make([]sceneFirefly, len(farCoords))
	for i, c := range farCoords {
		farFlies[i] = mkFly(c[0], c[1], 0.6)
		farFlies[i].tier = uint8(i % 3)
	}

	var pondY, pondDel, pondDur [4]float32
	for i := range 4 {
		pondY[i] = 60 + (rng.Float32()*1.8 - 0.8)
		pondDel[i] = rng.Float32() * 5.0
		pondDur[i] = 3.4 + rng.Float32()*3.3
	}

	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 6, 288, 10, 2, 5, 5.0), 5.0, 11.0, -1.6, 1.6, "reed")
	var catDel, catDur [5]float32
	for i := range 5 {
		catDel[i] = rng.Float32() * 8.0
		catDur[i] = 7.0 + rng.Float32()*3.0
	}
	var mDel, mDur [3]float32
	for i := range 3 {
		mDel[i] = rng.Float32() * 5.0
		mDur[i] = 5.0 + rng.Float32()*3.0
	}

	nearCoords := [][2]float32{
		{22, 54}, {48, 44}, {71, 60}, {97, 50}, {118, 66},
		{139, 43}, {164, 55}, {189, 50}, {212, 64}, {200, 57}, {176, 45},
	}
	skyCoords := [][2]float32{
		{304, 33}, {388, 27}, {521, 34}, {742, 30}, {921, 35}, {1124, 31},
		{1420, 32}, {1760, 29}, {2080, 33},
	}
	flies := make([]sceneFirefly, 0, len(nearCoords)+len(skyCoords)+2)
	for i, c := range nearCoords {
		sz := float32(1.0)
		if rng.Intn(3) == 2 {
			sz = 1.25
		}
		ff := mkFly(c[0], c[1], sz)
		ff.tier = uint8(i % 3)
		flies = append(flies, ff)
	}
	for _, c := range skyCoords {
		ff := mkFly(c[0], c[1], 1.0)
		ff.tier = 2
		flies = append(flies, ff)
	}
	// Two more drift through the sky under the titlebar controls when the scene is rich
	for _, c := range [][2]float32{{60, 38}, {146, 38.5}} {
		ff := mkFly(c[0], c[1], 0.9)
		ff.tier = 2
		flies = append(flies, ff)
	}
	return &compiledScene{
		id:          SceneFirefly,
		dark:        true,
		ground:      ground,
		skyStops:    nightSky,
		peak:        pk,
		far:         far,
		near:        near,
		stars:       sts,
		spill:       makeSpill(rand.New(rand.NewSource(2067)), starCols, 120, 14),
		ffWanders:   wanders,
		farFlies:    farFlies,
		fireflies:   flies,
		pondReflY:   pondY,
		pondReflDel: pondDel,
		pondReflDur: pondDur,
		blades:      bl,
		bladeColor:  sceneColor("#34413c"),
		cattailDel:  catDel,
		cattailDur:  catDur,
		mushDel:     mDel,
		mushDur:     mDur,
		cat: sceneCatSpec{
			x: 128, y: far.y(128) + 0.8,
			color:    sceneColor("#15181b"),
			tailDur:  4.8,
			hasEyes:  true,
			eyeColor: sceneColor("#d7f08a"),
		},
	}
}

func compileNight(ground ui.Color, nightSky []ui.GradientStop, starCols []ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(79))
	pk := peakCurve{base: 52, amp: 17, wl: 271, phase: 0.9, sharp: 1.9, fill: sceneColor("#222734")}
	far := ridgeCurve{base: 56, comps: []ridgeComp{{4.5, 400, 2.9}, {2.2, 153, 0.4}, {1.1, 65, 1.8}}, fill: sceneColor("#252a35")}
	near := ridgeCurve{base: 64, comps: []ridgeComp{{3, 262, 1.3}, {1.2, 95, 0.2}}, fill: sceneColor("#20232b")}

	sts := makeStarsV2(rng, 210, 3, 50, 0.3, starCols, nil)
	sts = append(sts, makeSidebarStars(rand.New(rand.NewSource(1079)), 50, 48, 0.3, starCols, nil)...)
	// The densest sky of the set trails furthest into the titlebar, with a lane of fine dust.
	spillRng := rand.New(rand.NewSource(2079))
	spill := makeSpill(spillRng, starCols, 170, 34)
	spill = append(spill, makeDustLane(spillRng, []ui.Color{sceneColor("#cfdcff"), sceneColor("#cfdcff"), sceneColor("#f1e4ff")}, 44)...)

	flareSpecs := [][4]float32{
		{204, 36, 3.0, 9.7},
		{836, 9, 3.6, 13.1},
		{988, 33, 2.8, 11.3},
		{1046, 7, 3.2, 17.9},
		{96, 38, 2.6, 15.1},
		{668, 34, 2.4, 12.7},
	}
	flares := make([]sceneFlareStar, len(flareSpecs))
	for i, fs := range flareSpecs {
		flares[i] = sceneFlareStar{
			x: fs[0], y: fs[1], size: fs[2],
			delaySec: rng.Float32() * 9.0,
			durSec:   fs[3],
		}
	}
	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 10, 254, 6, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "")
	tx := float64(106)

	return &compiledScene{
		id:         SceneNight,
		dark:       true,
		ground:     ground,
		skyStops:   nightSky,
		peak:       pk,
		far:        far,
		near:       near,
		spill:      spill,
		stars:      sts,
		flares:     flares,
		blades:     bl,
		bladeColor: sceneColor("#2a2f3a"),
		meteors: []sceneMeteor{
			{x0: 150, y0: 33, dx: -46, dy: 10, tailX: 19.6, tailY: -4.3, durSec: 37, atFrac: 0.88, delaySec: 22, isNightMet: true, tier: 2},
		},
		cat: sceneCatSpec{
			x: float32(tx + 26), y: far.y(tx+26) + 0.8,
			color:   sceneColor("#171a20"),
			flip:    true,
			tailDur: 5.6,
			lookDeg: -20,
		},
	}
}

func compileMoon(ground ui.Color, nightSky []ui.GradientStop, starCols []ui.Color) *compiledScene {
	rng := rand.New(rand.NewSource(83))
	pk := peakCurve{base: 50, amp: 20, wl: 219, phase: 1.6, sharp: 2.4, fill: sceneColor("#232834")}
	far := ridgeCurve{base: 54, comps: []ridgeComp{{4, 420, 1.1}, {2, 147, 2.6}, {1, 69, 0.9}}, fill: sceneColor("#262b36")}
	near := ridgeCurve{base: 64, comps: []ridgeComp{{2.4, 268, 2.2}, {1.1, 105, 0.7}}, fill: sceneColor("#22262f")}

	sts := makeStarsV2(rng, 105, 4, 46, 0.5, starCols, nil)
	sts = append(sts, makeSidebarStars(rand.New(rand.NewSource(1083)), 15, 46, 0.5, starCols, nil)...)
	shotSpecs := [][8]float32{ // x0, y0, dx, dy, dur, at%, from%, tier
		{300, 5, 66, 21, 19, 62, 8, 1},
		{772, 3, 50, 25, 31, 44, 22, 1},
		{1034, 9, -54, 19, 43, 71, 30, 1},
		// Two short streaks that fit inside the sidebar: above the wordmark, and low on the left
		{184, 2.5, -44, 5, 29, 55, 12, 2},
		{20, 30, 44, 9, 41, 35, 20, 2},
	}
	meteors := make([]sceneMeteor, len(shotSpecs))
	for i, sh := range shotSpecs {
		n := float32(math.Hypot(float64(sh[2]), float64(sh[3])))
		meteors[i] = sceneMeteor{
			x0: sh[0], y0: sh[1],
			dx: sh[2], dy: sh[3],
			tailX:    -sh[2] / n * 24,
			tailY:    -sh[3] / n * 24,
			durSec:   sh[4],
			atFrac:   sh[5] / 100.0,
			delaySec: sh[4] * (sh[5] - sh[6]) / 100.0,
			tier:     uint8(sh[7]),
		}
	}
	lk := sceneLake{
		cx: 182, cy: 60.5, rx: 46, ry: 3.2,
		fill:       sceneColor("#1e2530"),
		glintColor: sceneColor("#aebfe8"),
		glints:     makeLakeGlints(rng, 60.5, 189),
	}
	bl, _ := makeBlades(rng, near.y, makeTuftXs(rng, 12, 252, 6, 2, 5, 5.0), 4.0, 7.5, -1.6, 1.6, "")

	return &compiledScene{
		id:         SceneMoon,
		dark:       true,
		ground:     ground,
		skyStops:   nightSky,
		peak:       pk,
		far:        far,
		near:       near,
		rimX0:      48,
		rimX1:      336,
		rimColor:   sceneColor("#aebfe8"),
		rimPeakOp:  0.7,
		rimMidStop: 0.5,
		rimMidOp:   0.5,
		stars:      sts,
		meteors:    meteors,
		spill:      makeSpill(rand.New(rand.NewSource(2083)), starCols, 120, 14),
		moonWisps: []sceneMoonWisp{
			{y: 31, scale: 0.42, tier: 1, delaySec: 60, durSec: 118},
			{y: 2.5, scale: 0.34, tier: 2, delaySec: 14, durSec: 96},
			{y: 41, scale: 0.3, tier: 2, minW: 300, delaySec: 92, durSec: 131},
		},
		hasLake:    true,
		lake:       lk,
		blades:     bl,
		bladeColor: sceneColor("#303746"),
		cat: sceneCatSpec{
			x: 128, y: far.y(128) + 0.8,
			color:    sceneColor("#191c24"),
			sleep:    true,
			hasZzz:   true,
			zzzColor: sceneColor("#8fa0c8"),
		},
	}
}

func buildCurvePath(dst *ui.Path, yf func(float64) float32, x0, x1, step float32, closeBottom bool) {
	dst.Reset()
	n := int((x1-x0)/step) + 1
	if n < 3 {
		return
	}
	px0, py0 := x0, yf(float64(x0))
	dst.MoveTo(px0, py0)
	prevX, prevY := px0, py0
	for i := 1; i < n; i++ {
		cx := x0 + float32(i)*step
		if cx > x1 {
			cx = x1
		}
		cy := yf(float64(cx))
		if i > 1 {
			mx := (prevX + cx) * 0.5
			my := (prevY + cy) * 0.5
			dst.QuadTo(prevX, prevY, mx, my)
		}
		prevX, prevY = cx, cy
	}
	dst.LineTo(prevX, prevY)
	if closeBottom {
		bottom := BrandBandHeight + 16
		dst.LineTo(x1, bottom).LineTo(x0, bottom).Close()
	}
}

func (sc *compiledScene) ensureTerrain(w float32) *sceneTerrainCache {
	bucket := (int(w)/64 + 1) * 64
	if bucket < int(brandDesignWidth) {
		bucket = int(brandDesignWidth)
	}
	sc.terrainMu.Lock()
	defer sc.terrainMu.Unlock()
	if sc.terrain.widthBucket != bucket {
		x0 := float32(-24)
		x1 := float32(bucket) + 24
		buildCurvePath(&sc.terrain.peakPath, sc.peak.y, x0, x1, 6, true)
		buildCurvePath(&sc.terrain.farPath, sc.far.y, x0, x1, 12, true)
		sc.terrain.hasMid = sc.hasMid
		if sc.hasMid {
			buildCurvePath(&sc.terrain.midPath, sc.mid.y, x0, x1, 12, true)
		}
		buildCurvePath(&sc.terrain.nearPath, sc.near.y, x0, x1, 12, true)
		if sc.rimX1 > sc.rimX0 {
			buildCurvePath(&sc.terrain.peakRim, sc.peak.y, sc.rimX0, sc.rimX1, 6, false)
			buildCurvePath(&sc.terrain.farRim, sc.far.y, sc.rimX0, sc.rimX1, 12, false)
		}
		sc.terrain.widthBucket = bucket
	}
	return &sc.terrain
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
