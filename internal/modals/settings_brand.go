package modals

import (
	"time"

	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func (m *Manager) renderBrandDecoCard(c *ui.Context, st model.AppState) {
	pal := theme.Ocean()
	deco := st.BrandDeco
	now := time.Now()
	curLight, curDark := internalui.EffectiveBrandScenes(deco, now)

	modeIdx := 0
	switch deco.Mode {
	case model.BrandDecoFixed:
		modeIdx = 1
	case model.BrandDecoOff:
		modeIdx = 2
	}

	internalui.Card(c, func() {
		cardTitle(c, theme.Tr("品牌装饰", "Brand Decoration"))

		settingRow(c, theme.Tr("侧栏装饰", "Scene Mode"), func() {
			if internalui.SegmentedControl(c, "settings-brand-mode-seg", &modeIdx, []internalui.SegmentOption{
				{ID: "settings-brand-mode-time", Label: theme.Tr("随时段", "By Time")},
				{ID: "settings-brand-mode-fixed", Label: theme.Tr("固定", "Pinned")},
				{ID: "settings-brand-mode-off", Label: theme.Tr("关闭", "Off")},
			}) {
				modes := []model.BrandDecoMode{model.BrandDecoByTime, model.BrandDecoFixed, model.BrandDecoOff}
				picked := modes[modeIdx]
				m.mutateState(func(s *model.AppState) {
					s.BrandDeco.Mode = picked
					if picked == model.BrandDecoFixed {
						if !internalui.IsValidBrandScene(s.BrandDeco.PinLight, false) {
							s.BrandDeco.PinLight = string(curLight)
						}
						if !internalui.IsValidBrandScene(s.BrandDeco.PinDark, true) {
							s.BrandDeco.PinDark = string(curDark)
						}
					}
				})
			}
		})

		if deco.Mode != model.BrandDecoOff {
			ui.Column(c.Key("settings-brand-scene-groups")).FillWidth().Gap(theme.Space12).Children(func() {
				m.renderBrandSceneGroup(c, theme.Tr("浅色", "Light"), internalui.LightBrandScenes(), curLight, !deco.HideOrb, func(picked internalui.BrandSceneID) {
					m.mutateState(func(s *model.AppState) {
						s.BrandDeco.Mode = model.BrandDecoFixed
						s.BrandDeco.PinLight = string(picked)
						if !internalui.IsValidBrandScene(s.BrandDeco.PinDark, true) {
							s.BrandDeco.PinDark = string(curDark)
						}
					})
				})
				m.renderBrandSceneGroup(c, theme.Tr("深色", "Dark"), internalui.DarkBrandScenes(), curDark, !deco.HideOrb, func(picked internalui.BrandSceneID) {
					m.mutateState(func(s *model.AppState) {
						s.BrandDeco.Mode = model.BrandDecoFixed
						s.BrandDeco.PinDark = string(picked)
						if !internalui.IsValidBrandScene(s.BrandDeco.PinLight, false) {
							s.BrandDeco.PinLight = string(curLight)
						}
					})
				})
			})

			showOrb := !deco.HideOrb
			settingRow(c, theme.Tr("显示日月", "Show Sun & Moon"), func() {
				if internalui.TogglePill(c, "settings-brand-orb-toggle", theme.Tr("显示日月", "Show Sun & Moon"), &showOrb, false) {
					hide := !showOrb
					m.mutateState(func(s *model.AppState) {
						s.BrandDeco.HideOrb = hide
					})
				}
			})
		}

		showMark := !deco.HideMark
		settingRow(c, theme.Tr("显示 Logo 与名称", "Show Logo & Title"), func() {
			if internalui.TogglePill(c, "settings-brand-mark-toggle", theme.Tr("显示 Logo 与名称", "Show Logo & Title"), &showMark, false) {
				hide := !showMark
				m.mutateState(func(s *model.AppState) {
					s.BrandDeco.HideMark = hide
				})
			}
		})
	})
	_ = pal
}

func (m *Manager) renderBrandSceneGroup(
	c *ui.Context,
	groupLabel string,
	scenes []internalui.BrandSceneSpec,
	activeID internalui.BrandSceneID,
	showOrb bool,
	onPick func(internalui.BrandSceneID),
) {
	pal := theme.Ocean()
	ui.Column(c).FillWidth().Gap(theme.Space8).Children(func() {
		ui.Text(c, groupLabel).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).SingleLine()
		ui.Row(c).FillWidth().Gap(theme.Space12).Children(func() {
			for _, sc := range scenes {
				sc := sc
				active := sc.ID == activeID
				tileID := "settings-brand-tile-" + string(sc.ID)
				c.Key(tileID)
				tile := ui.ButtonBase(c).Grow(1).MinWidth(0).Cursor(ui.CursorPointer).Label(sc.Label())
				hovered := tile.Hovered()
				pressed := tile.Pressed()
				internalui.TactileCard(tile, hovered, pressed, active, false)
				tile.Children(func() {
					ui.Column(c).FillWidth().Gap(theme.Space6).Children(func() {
						borderW := float32(1)
						borderCol := pal.Hairline
						if active {
							borderW = 2
							borderCol = pal.Accent
						} else if hovered || pressed {
							borderCol = pal.BtnBorder
						}
						frame := ui.Box(c).FillWidth().Height(54).Radius(theme.RadiusMD).Clip().
							Border(borderW, borderCol)
						frame.Children(func() {
							internalui.BrandSceneThumbnail(c, sc.ID, showOrb)
						})
						ui.Row(c).FillWidth().PaddingX(theme.Space2).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space4).Children(func() {
							nameCol := pal.Text
							nameWeight := theme.WeightRegular
							if active {
								nameCol = pal.Accent
								nameWeight = theme.WeightTitle
							}
							ui.Text(c, sc.Label()).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								FontWeight(nameWeight).TextColor(nameCol).SingleLine().Shrink(1).MinWidth(0).Ellipsis("…")
							ui.Text(c, sc.Hours).Font("monospace").FontSize(theme.FontMicro).FixedLineHeight(16).
								FontWeight(theme.WeightMono).TextColor(pal.TextSecondary).SingleLine().Shrink(0)
						})
					})
				})
				internalui.Probe(tile, tileID)
				if tile.Clicked() && onPick != nil {
					onPick(sc.ID)
				}
			}
		})
	})
}
