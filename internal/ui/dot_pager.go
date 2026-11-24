package ui

import (
	"fmt"
	"math"

	"block/internal/icons"
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

	// Optional morphing capsule countdown mode for auto-advancing carousels.
	CountdownMode     bool
	CountdownRunning  bool
	CountdownProgress float32  // 0..1 forward fill progress for the active capsule
	AccentColor       ui.Color // optional active tint; defaults to pal.Accent
}

// DotPager renders the recessed-track circular bead pager ("嵌槽圆珠")
// driven by the shared short control animation. When CountdownMode is enabled,
// the active bead morphs into a pill track that fills forward from 0 to 1.
func DotPager(c *ui.Context, opts DotPagerOptions) ui.Element {
	if opts.CountdownMode {
		return capsuleCountdownPager(c, opts)
	}
	pal := theme.Ocean()
	dp := pal.DotPager()

	pagerID := opts.PagerID
	if pagerID == "" {
		pagerID = opts.IDPrefix + "-pager"
	}
	pager := ui.Row(c.Key(pagerID)).Height(dotPagerTrackH).PaddingX(dotPagerTrackPad).
		AlignItems(ui.Center).Radius(theme.RadiusPill).KeepFocus()
	AnimateSurfaceColors(pager, dp.TrackBG, dp.TrackBorder, true)
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
					Radius(theme.RadiusPill).Cursor(ui.CursorPointer).KeepFocus().
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
				targetPipCol := dp.Idle
				switch {
				case isActive:
					targetPipSize = 3.5
				case pressed:
					targetPipSize = 4.0
					targetPipLift = 0.3
					targetPipCol = dp.Hover
				case hovered:
					targetPipSize = 6.0
					targetPipLift = -0.6
					targetPipCol = dp.Hover
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

				targetScale := float32(1.08)
				targetLiftY := float32(0.0)
				targetCore := float32(2.5)
				targetBeadCol := pal.Accent
				switch {
				case activePressed:
					targetScale = 0.94
					targetLiftY = 0.4
					targetCore = 2.0
					targetBeadCol = pal.AccentPressed
				case activeHovered:
					targetScale = 1.14
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

				travel := min(float32(math.Abs(float64(targetX-beadX)))/step, 1.0)
				sx := sc * (1.0 + 0.10*travel)
				sy := sc * (1.0 - 0.06*travel)

				beadWrap.Children(func() {
					bead := ui.Row(c).Size(dotPagerBeadSize, dotPagerBeadSize).Center().
						Radius(theme.RadiusPill).Background(beadCol)
					if !almostZero(sx-1, 0.002) || !almostZero(sy-1, 0.002) || !almostZero(dy, 0.02) {
						bead.Translate(0, dy).Scale(sx, sy)
					}
					bead.Children(func() {
						ui.Box(c).Size(coreSz, coreSz).Radius(theme.RadiusPill).Background(dp.Core)
					})
				})
			}
		})
	})
	return pager
}

func capsuleCountdownPager(c *ui.Context, opts DotPagerOptions) ui.Element {
	pal := theme.Ocean()
	dp := pal.DotPager()

	accent := opts.AccentColor
	if accent.A == 0 {
		accent = pal.Accent
	}
	pagerID := opts.PagerID
	if pagerID == "" {
		pagerID = opts.IDPrefix + "-pager"
	}
	pager := ui.Row(c.Key(pagerID)).Height(dotPagerTrackH).PaddingX(dotPagerTrackPad + 1).
		AlignItems(ui.Center).Radius(theme.RadiusPill).KeepFocus()
	AnimateSurfaceColors(pager, dp.TrackBG, dp.TrackBorder, true)
	Probe(pager, pagerID)

	progress := min(max(opts.CountdownProgress, 0), 1)

	pager.Children(func() {
		inner := ui.Row(c.Key(pagerID + "-inner")).FillHeight().Gap(theme.Space4).AlignItems(ui.Center)
		inner.Children(func() {
			for i := 0; i < opts.Count; i++ {
				slot := i
				isActive := slot == opts.Active
				dotID := fmt.Sprintf("%s-%d", opts.IDPrefix, slot)

				targetPillW := float32(7)
				if isActive {
					if opts.CountdownRunning {
						targetPillW = 34
					} else {
						targetPillW = 18
					}
				}
				btn := ui.ButtonBase(c.Key(dotID)).Height(dotPagerSlotSize).Center().
					Radius(theme.RadiusPill).Cursor(ui.CursorPointer).KeepFocus().
					Role(ui.RoleTab).AccessSelected(isActive).
					Label(fmt.Sprintf("Slot %d", slot+1))
				hovered := btn.Hovered()
				pressed := btn.Pressed()

				pillW := btn.AnimateTo("capW", targetPillW, TreeDrawerOpen)
				hitW := max(pillW+6, 14)
				btn.Width(hitW)

				if opts.SlotTooltip != nil {
					if spec := opts.SlotTooltip(slot); spec != nil {
						RichTooltip(c, btn, *spec)
					}
				} else if opts.Tooltip != "" {
					Tooltip(c, btn, opts.Tooltip)
				}

				targetTrackCol := dp.Idle
				if isActive {
					if opts.CountdownRunning {
						targetTrackCol = accent.Alpha(0.22)
					} else {
						targetTrackCol = accent
					}
				} else if hovered || pressed {
					targetTrackCol = dp.Hover
				}
				trackCol := btn.AnimateColor("capTrackCol", targetTrackCol, FadeColor)
				fillCol := btn.AnimateColor("capFillCol", accent, FadeColor)

				targetScale := float32(1)
				if !isActive && hovered {
					targetScale = 1.18
				} else if pressed {
					targetScale = 0.92
				}
				pillScale := btn.AnimateTo("capScale", targetScale, SpringTactile)

				activeNow := isActive
				runningNow := opts.CountdownRunning
				btn.Children(func() {
					pill := ui.Box(c).Size(pillW, 7).Radius(theme.RadiusPill).Clip().
						Draw(func(p *ui.Painter, bounds ui.Rect) {
							p.Fill(bounds, trackCol, theme.RadiusPill)
							if activeNow && runningNow {
								baseW := min(bounds.H, bounds.W)
								fillW := baseW + (bounds.W-baseW)*progress
								if fillW > 0.5 {
									p.Fill(ui.Rect{
										X: bounds.X,
										Y: bounds.Y,
										W: fillW,
										H: bounds.H,
									}, fillCol, theme.RadiusPill)
								}
							}
						})
					if !almostZero(pillScale-1, 0.005) {
						pill.Scale(pillScale, pillScale)
					}
				})
				Probe(btn, dotID)
				if btn.Clicked() && opts.OnSelect != nil {
					opts.OnSelect(slot)
				}
			}
		})
	})
	return pager
}

// WindowDismissDot renders a quiet 12px circular dismiss dot inside a 24x24 hit target,
// colour-matched to DotPager's recessed track at rest and blooming a crisp danger "x" on hover.
func WindowDismissDot(c *ui.Context, id, label, tooltip, shortcut string) ui.Element {
	pal := theme.Ocean()
	dp := pal.DotPager()
	kc := c
	if id != "" {
		kc = c.Key(id)
	}
	btn := ui.ButtonBase(kc).Size(theme.IconButtonBox, theme.IconButtonBox).Shrink(0).Center().
		Radius(theme.RadiusPill).Cursor(ui.CursorPointer).Label(label)
	if tooltip != "" || shortcut != "" {
		RichTooltip(c, btn, TooltipSpec{
			Rows: []TooltipRow{{
				Trigger:  tooltip,
				Shortcut: shortcut,
			}},
		})
	}
	hovered := btn.Hovered()
	pressed := btn.Pressed()
	active := hovered || pressed
	TactileButton(btn, hovered, pressed, false)

	targetVis := float32(0)
	dotBG := dp.TrackBG
	dotBorder := dp.TrackBorder
	if active {
		targetVis = 1
		dotBG = pal.DangerSurface
		dotBorder = dp.CloseHoverBorder
	}
	vis := btn.AnimateTo("xVis", targetVis, SpringTactile)

	btn.Children(func() {
		dot := ui.Row(c).Size(theme.IconMicro, theme.IconMicro).Center().Radius(theme.RadiusPill)
		AnimateSurfaceColors(dot, dotBG, dotBorder, true)
		if vis > 0.02 {
			alpha := min(max(vis, 0), 1)
			sc := 0.72 + 0.28*vis
			dot.Children(func() {
				glyphWrap := ui.Row(c).Size(8, 8).Center()
				if !almostZero(sc-1, 0.01) {
					glyphWrap.Scale(sc, sc)
				}
				glyphWrap.Children(func() {
					icons.Draw(c, icons.Close, 7, pal.MonoIconColors(pal.Danger.Alpha(alpha)))
				})
			})
		}
	})
	if id != "" {
		Probe(btn, id)
	}
	return btn
}
