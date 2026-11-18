package ui

import (
	"fmt"
	"math"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	dotPagerTrackH   float32 = 20
	dotPagerTrackPad float32 = 5
	dotPagerSlotSize float32 = 20
	dotPagerSlotGap  float32 = 2
	dotPagerBeadSize float32 = 12
)

// DotPagerOptions configures a recessed-track tactile bead slot indicator.
type DotPagerOptions struct {
	PagerID     string
	IDPrefix    string
	Count       int
	Active      int
	Tooltip     string
	SlotTooltip func(index int) *TooltipSpec
	OnSelect    func(index int)
}

// DotPager renders the recessed-track circular bead pager ("嵌槽圆珠")
// driven by Scheme B ("灵动弹簧 · Tactile Spring") physics.
func DotPager(c *ui.Context, opts DotPagerOptions) ui.Element {
	pal := theme.Ocean()
	trackBG := ui.Hex("#f1f3f7")
	trackBorder := ui.Hex("#e2e6ee")
	idleDot := ui.Hex("#bcc4d2")
	hoverDot := ui.Hex("#7b869a")
	beadShadow := ui.RGBA(46, 79, 150, 0.32)
	if pal.Dark {
		trackBG = ui.Hex("#24272f")
		trackBorder = ui.Hex("#333844")
		idleDot = ui.Hex("#4d5565")
		hoverDot = ui.Hex("#8d97ab")
		beadShadow = ui.RGBA(0, 0, 0, 0.65)
	}
	rimCol := pal.Paper
	coreCol := ui.Hex("#eef4ff")

	pagerID := opts.PagerID
	if pagerID == "" {
		pagerID = opts.IDPrefix + "-pager"
	}
	pager := ui.Row(c.Key(pagerID)).Height(dotPagerTrackH).PaddingX(dotPagerTrackPad).
		AlignItems(ui.Center).Radius(theme.RadiusPill)
	AnimateSurfaceColors(pager, trackBG, trackBorder, true)
	Probe(pager, pagerID)

	pager.Children(func() {
		inner := ui.Row(c.Key(pagerID + "-inner")).FillHeight().Gap(dotPagerSlotGap).AlignItems(ui.Center)
		inner.Children(func() {
			activeHovered := false
			activePressed := false

			for i := 0; i < opts.Count; i++ {
				slot := i
				isActive := slot == opts.Active
				dotID := fmt.Sprintf("%s-%d", opts.IDPrefix, slot)
				btn := ui.ButtonBase(c.Key(dotID)).Size(dotPagerSlotSize, dotPagerSlotSize).Center().
					Radius(theme.RadiusPill).Cursor(ui.CursorPointer).
					Role(ui.RoleTab).AccessSelected(isActive).
					Label(fmt.Sprintf("Slot %d", slot+1))
				if opts.SlotTooltip != nil {
					if spec := opts.SlotTooltip(slot); spec != nil {
						RichTooltip(c, btn, *spec)
					}
				} else if opts.Tooltip != "" {
					Tooltip(c, btn, opts.Tooltip)
				}

				hovered := btn.Hovered()
				pressed := btn.Pressed()
				if isActive {
					activeHovered = hovered
					activePressed = pressed
				}

				targetPipSize := float32(4.5)
				targetPipLift := float32(0)
				targetPipCol := idleDot
				switch {
				case isActive:
					targetPipSize = 3.5
				case pressed:
					targetPipSize = 4.0
					targetPipLift = 0.3
					targetPipCol = hoverDot
				case hovered:
					targetPipSize = 6.0
					targetPipLift = -0.6
					targetPipCol = hoverDot
				}
				pipSize := btn.AnimateTo("pipSz", targetPipSize, SpringTactile)
				pipLift := btn.AnimateTo("pipY", targetPipLift, SpringTactile)
				pipCol := btn.AnimateColor("pipCol", targetPipCol, FadeColor)

				btn.Children(func() {
					pip := ui.Box(c).Size(pipSize, pipSize).Radius(theme.RadiusPill).Background(pipCol)
					if !almostZero(pipLift, 0.02) {
						pip.Translate(0, pipLift)
					}
				})
				Probe(btn, dotID)
				if btn.Clicked() && opts.OnSelect != nil {
					opts.OnSelect(slot)
				}
			}

			if opts.Count > 0 {
				clamped := min(max(opts.Active, 0), opts.Count-1)
				step := dotPagerSlotSize + dotPagerSlotGap
				targetX := float32(clamped) * step
				beadX := inner.AnimateTo("beadX", targetX, SpringTactile)

				targetScale := float32(1.0)
				targetLiftY := float32(0.0)
				targetCore := float32(2.5)
				targetBeadCol := pal.Accent
				switch {
				case activePressed:
					targetScale = 0.88
					targetLiftY = 0.4
					targetCore = 2.0
					targetBeadCol = pal.AccentPressed
				case activeHovered:
					targetScale = 1.08
					targetLiftY = -0.6
					targetCore = 3.2
					targetBeadCol = pal.AccentHover
				}

				beadWrap := ui.Row(c.Key(pagerID + "-bead")).Absolute().
					Top(0).Bottom(0).Left(beadX).Width(dotPagerSlotSize).
					Center().PassThrough()

				sc := beadWrap.AnimateTo("beadSc", targetScale, SpringTactile)
				dy := beadWrap.AnimateTo("beadDy", targetLiftY, SpringTactile)
				coreSz := beadWrap.AnimateTo("beadCore", targetCore, SpringTactile)
				beadCol := beadWrap.AnimateColor("beadBg", targetBeadCol, FadeColor)
				beadRim := beadWrap.AnimateColor("beadRim", rimCol, FadeColor)

				travel := min(float32(math.Abs(float64(targetX-beadX)))/step, 1.0)
				sx := sc * (1.0 + 0.10*travel)
				sy := sc * (1.0 - 0.06*travel)

				beadWrap.Children(func() {
					bead := ui.Row(c).Size(dotPagerBeadSize, dotPagerBeadSize).Center().
						Radius(theme.RadiusPill).Background(beadCol).Border(2, beadRim).
						Shadow(0, 1, 3, 0, beadShadow)
					if !almostZero(sx-1, 0.002) || !almostZero(sy-1, 0.002) || !almostZero(dy, 0.02) {
						bead.Translate(0, dy).Scale(sx, sy)
					}
					bead.Children(func() {
						ui.Box(c).Size(coreSz, coreSz).Radius(theme.RadiusPill).Background(coreCol)
					})
				})
			}
		})
	})
	return pager
}
