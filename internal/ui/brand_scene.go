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
)

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

// EffectiveBrandScenes returns the active light and dark scene IDs for deco at time now
// (used both to highlight the active tiles in Settings and to resolve the window band).
func EffectiveBrandScenes(deco model.BrandDeco, now time.Time) (light, dark BrandSceneID) {
	h := now.Hour()
	light = LightSceneByHour(h)
	dark = DarkSceneByHour(h)
	if deco.Mode == model.BrandDecoFixed {
		if IsValidBrandScene(deco.PinLight, false) {
			light = BrandSceneID(deco.PinLight)
		} else {
			light = SceneMeadow
		}
		if IsValidBrandScene(deco.PinDark, true) {
			dark = BrandSceneID(deco.PinDark)
		} else {
			dark = SceneNight
		}
	}
	return light, dark
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

// Precomputed deterministic geometry for zero-allocation 60fps painting.
const (
	sceneSpanTiles = 8
	sceneSpanWidth = BrandTileWidth * sceneSpanTiles // 2080px seamless period
	brandOrbX      = float32(190)
)

type sceneBlade struct {
	x, y     float32
	h, lean  float32
}

type sceneBladeGroup struct {
	color    ui.Color
	delaySec float32
	blades   []sceneBlade
}

type sceneMistBar struct {
	x, y, w float32
	opacity float32
}

type sceneWindLine struct {
	x, y     float32
	color    ui.Color
	delaySec float32
	durSec   float32
}

type sceneLayer struct {
	driftSec float32
	hasHill  bool
	hillB    float32
	hillA    float32
	hillInv  bool
	hillFill ui.Color
	mists    []sceneMistBar
	winds    []sceneWindLine
	blades   []sceneBladeGroup
}

type sceneStar struct {
	x, y     float32
	r        float32
	twinkle  bool
	opacity  float32
	delaySec float32
	durSec   float32
}

type sceneFirefly struct {
	x, y          float32
	moveDelaySec  float32
	moveDurSec    float32
	blinkDelaySec float32
}

type sceneOrbKind uint8

const (
	orbNone sceneOrbKind = iota
	orbSun
	orbDaybreak
	orbCrescent
)

type compiledScene struct {
	id         BrandSceneID
	dark       bool
	ground     ui.Color
	skyStops   []ui.GradientStop
	horizonGlow bool
	galaxyBand bool
	shootStar  bool
	birds      bool
	orbKind    sceneOrbKind
	orbCY      float32
	orbR       float32
	orbCore    ui.Color
	orbHalo    ui.Color
	orbHaloOp  float32
	stars      []sceneStar
	layers     []sceneLayer
	fireflies  []sceneFirefly
}

var (
	compiledOnce  sync.Once
	compiledByID  map[BrandSceneID]*compiledScene
	starAvoidRect = [][4]float32{
		{8, 10, 84, 30},
		{110, 4, 186, 36},
		{196, 4, 256, 36},
		{264, 4, 640, 36},
	}
	starRadii = [...]float32{0.55, 0.6, 0.7, 0.8, 0.9, 1.05}
)

func hillY(x, b, a float32, inv bool) float32 {
	amp := a
	if inv {
		amp = -a
	}
	return b - 0.75*amp*float32(math.Sin(2*math.Pi*float64(x)/float64(BrandTileWidth)))
}

func makeBladeGroup(b, a float32, inv bool, xs []float32, hex string, seed int64, delaySec float32) sceneBladeGroup {
	rng := rand.New(rand.NewSource(seed*7919 + 104729))
	out := make([]sceneBlade, 0, len(xs)*sceneSpanTiles)
	for _, bx := range xs {
		by := hillY(bx, b, a, inv) + 1.2
		for k := 0; k < sceneSpanTiles; k++ {
			h := 4.0 + rng.Float32()*3.5
			lean := -1.6 + rng.Float32()*3.2
			out = append(out, sceneBlade{
				x:    bx + float32(k)*BrandTileWidth,
				y:    by,
				h:    h,
				lean: lean,
			})
		}
	}
	return sceneBladeGroup{
		color:    ui.Hex(hex), // design:allow
		delaySec: delaySec,
		blades:   out,
	}
}

func makeStars(density int, yMin, yMax, twinkleProb float32, seed int64) []sceneStar {
	rng := rand.New(rand.NewSource(seed*65537 + 17))
	count := int(float32(density) * sceneSpanWidth / BrandTileWidth)
	out := make([]sceneStar, 0, count)
	for range count {
		x := 4 + rng.Float32()*(sceneSpanWidth-8)
		y := yMin + rng.Float32()*(yMax-yMin)
		skip := false
		for _, av := range starAvoidRect {
			if x > av[0] && x < av[2] && y > av[1] && y < av[3] {
				skip = true
				break
			}
		}
		if skip || (math.Abs(float64(x-brandOrbX)) < 9 && math.Abs(float64(y-38)) < 9) {
			continue
		}
		rad := starRadii[rng.Intn(len(starRadii))]
		if rng.Float32() < twinkleProb {
			out = append(out, sceneStar{
				x:        x,
				y:        y,
				r:        rad,
				twinkle:  true,
				delaySec: rng.Float32() * 4.0,
				durSec:   2.8 + rng.Float32()*2.4,
			})
		} else {
			out = append(out, sceneStar{
				x:       x,
				y:       y,
				r:       rad,
				opacity: 0.45 + rng.Float32()*0.50,
			})
		}
	}
	return out
}

func makeFireflies(seed int64) []sceneFirefly {
	rng := rand.New(rand.NewSource(seed*48271 + 31))
	count := int(9 * sceneSpanWidth / BrandTileWidth)
	out := make([]sceneFirefly, 0, count)
	for range count {
		out = append(out, sceneFirefly{
			x:             12 + rng.Float32()*(sceneSpanWidth-24),
			y:             38 + rng.Float32()*28,
			moveDelaySec:  rng.Float32() * 8.0,
			moveDurSec:    6.0 + rng.Float32()*4.0,
			blinkDelaySec: rng.Float32() * 4.0,
		})
	}
	return out
}

func ensureCompiledScenes() {
	compiledOnce.Do(func() {
		sceneHex := func(s string) ui.Color { return ui.Hex(s) } // design:allow
		lightGround := sceneHex("#f1f1f1")
		darkGround := sceneHex("#222222")
		nightSky := []ui.GradientStop{
			{Color: sceneHex("#1b2131"), At: 0},
			{Color: darkGround.Alpha(0), At: 1},
		}
		compiledByID = map[BrandSceneID]*compiledScene{
			SceneDawn: {
				id:     SceneDawn,
				ground: lightGround,
				skyStops: []ui.GradientStop{
					{Color: sceneHex("#fbe9dc"), At: 0},
					{Color: lightGround.Alpha(0), At: 1},
				},
				orbKind:   orbSun,
				orbCY:     41,
				orbR:      3.8,
				orbCore:   sceneHex("#ffc993"),
				orbHalo:   sceneHex("#ffe6cc"),
				orbHaloOp: 1,
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 44, hillA: 6, hillFill: sceneHex("#e7ece3")},
					{driftSec: 38, mists: []sceneMistBar{
						{x: 20, y: 40, w: 90, opacity: 0.70},
						{x: 150, y: 47, w: 70, opacity: 0.60},
					}},
					{driftSec: 26, hasHill: true, hillB: 56, hillA: 5, hillInv: true, hillFill: sceneHex("#dce6d6")},
					{driftSec: 140, mists: []sceneMistBar{
						{x: 60, y: 58, w: 110, opacity: 0.55},
					}},
					{
						driftSec: 18, hasHill: true, hillB: 64, hillA: 3, hillFill: sceneHex("#d2e0cb"),
						blades: []sceneBladeGroup{
							makeBladeGroup(64, 3, false, []float32{20, 64, 112, 170, 214}, "#b2cca8", 3, 0),
						},
					},
				},
			},
			SceneMeadow: {
				id:        SceneMeadow,
				ground:    lightGround,
				orbKind:   orbSun,
				orbCY:     40,
				orbR:      3.6,
				orbCore:   sceneHex("#ffdf96"),
				orbHalo:   sceneHex("#fff0c4"),
				orbHaloOp: 1,
				layers: []sceneLayer{
					{driftSec: 38, hasHill: true, hillB: 41, hillA: 6, hillFill: sceneHex("#e2eadf")},
					{driftSec: 26, hasHill: true, hillB: 50, hillA: 6, hillInv: true, hillFill: sceneHex("#d6e4d0")},
					{
						driftSec: 18, hasHill: true, hillB: 60, hillA: 4, hillFill: sceneHex("#cadcc2"),
						blades: []sceneBladeGroup{
							makeBladeGroup(60, 4, false, []float32{14, 22, 57, 63, 101, 139, 146, 183, 224, 231}, "#a9c69f", 7, 0),
							makeBladeGroup(60, 4, false, []float32{36, 82, 118, 165, 203, 247}, "#9bbd90", 8, 2.2),
						},
					},
				},
			},
			SceneBreeze: {
				id:     SceneBreeze,
				ground: lightGround,
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 50, hillA: 5, hillFill: sceneHex("#e3ebdf")},
					{driftSec: 26, winds: []sceneWindLine{
						{x: 6, y: 36, color: sceneHex("#b7d1ae"), delaySec: 0, durSec: 5.5},
						{x: 96, y: 44, color: sceneHex("#c3d6ec"), delaySec: 1.8, durSec: 6.5},
						{x: 170, y: 30, color: sceneHex("#c3d6ec"), delaySec: 3.1, durSec: 6.0},
						{x: 150, y: 52, color: sceneHex("#b7d1ae"), delaySec: 2.4, durSec: 7.0},
					}},
					{
						driftSec: 38, hasHill: true, hillB: 60, hillA: 3, hillInv: true, hillFill: sceneHex("#d7e5d1"),
						blades: []sceneBladeGroup{
							makeBladeGroup(60, 3, true, []float32{18, 26, 74, 128, 136, 196, 238}, "#a9c69f", 9, 0),
						},
					},
				},
			},
			SceneDusk: {
				id:     SceneDusk,
				ground: lightGround,
				skyStops: []ui.GradientStop{
					{Color: sceneHex("#f6dfd2"), At: 0},
					{Color: sceneHex("#efe3ee"), At: 0.45},
					{Color: lightGround.Alpha(0), At: 1},
				},
				birds:     true,
				orbKind:   orbSun,
				orbCY:     44,
				orbR:      4.2,
				orbCore:   sceneHex("#f2a679"),
				orbHalo:   sceneHex("#f8d3bb"),
				orbHaloOp: 1,
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 46, hillA: 6, hillFill: sceneHex("#e8e0d8")},
					{driftSec: 38, hasHill: true, hillB: 55, hillA: 5, hillInv: true, hillFill: sceneHex("#ddd6cb")},
					{
						driftSec: 26, hasHill: true, hillB: 63, hillA: 3, hillFill: sceneHex("#d3cfc0"),
						blades: []sceneBladeGroup{
							makeBladeGroup(63, 3, false, []float32{30, 92, 150, 206}, "#bdb6a2", 4, 0),
						},
					},
				},
			},
			SceneDaybreak: {
				id:          SceneDaybreak,
				dark:        true,
				ground:      darkGround,
				skyStops:    nightSky,
				horizonGlow: true,
				orbKind:     orbDaybreak,
				orbCY:       56,
				orbR:        7,
				orbHalo:     sceneHex("#f2b48a"),
				orbHaloOp:   0.35,
				stars:       makeStars(14, 3, 30, 0.70, 31),
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 54, hillA: 6, hillFill: sceneHex("#2a2a33")},
					{driftSec: 140, hasHill: true, hillB: 63, hillA: 4, hillInv: true, hillFill: sceneHex("#23242b")},
				},
			},
			SceneFirefly: {
				id:       SceneFirefly,
				dark:     true,
				ground:   darkGround,
				skyStops: nightSky,
				stars:    makeStars(10, 3, 36, 0.50, 41),
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 52, hillA: 6, hillFill: sceneHex("#252b31")},
					{
						driftSec: 140, hasHill: true, hillB: 62, hillA: 4, hillInv: true, hillFill: sceneHex("#1f2428"),
						blades: []sceneBladeGroup{
							makeBladeGroup(62, 4, true, []float32{22, 70, 130, 188, 232}, "#34413c", 5, 0),
						},
					},
				},
				fireflies: makeFireflies(51),
			},
			SceneNight: {
				id:         SceneNight,
				dark:       true,
				ground:     darkGround,
				skyStops:   nightSky,
				galaxyBand: true,
				stars:      makeStars(40, 3, 50, 0.45, 11),
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 56, hillA: 6, hillFill: sceneHex("#252a35")},
					{driftSec: 140, hasHill: true, hillB: 64, hillA: 4, hillInv: true, hillFill: sceneHex("#20232b")},
				},
			},
			SceneMoon: {
				id:        SceneMoon,
				dark:      true,
				ground:    darkGround,
				skyStops:  nightSky,
				shootStar: true,
				orbKind:   orbCrescent,
				orbCY:     37.5,
				orbR:      4.5,
				orbCore:   sceneHex("#e7ecf8"),
				orbHalo:   sceneHex("#9fb4e6"),
				orbHaloOp: 0.12,
				stars:     makeStars(18, 4, 46, 0.60, 23),
				layers: []sceneLayer{
					{driftSec: 90, hasHill: true, hillB: 60, hillA: 5, hillInv: true, hillFill: sceneHex("#22262f")},
				},
			},
		}
	})
}
