package markdown

import (
	"block/internal/model"
)

// applyBlockStyles gives each paper theme its own treatment of the blocks the
// design canvas (designs/export-themes) added in its second pass: H3 and below,
// callouts, figures, nested and ordered list markers, and inline strike /
// underline / code. finishSpec calls it after a constructor fills the rest.
func applyBlockStyles(s *PaperThemeSpec) {
	st := &s.Style
	ls := &st.List
	in := &st.Inline
	switch s.Theme {
	case model.ExportThemeMono:
		ink, grey := rgbHex(0x111111), rgbHex(0x7a776f)
		paper := rgbHex(0xfaf8f3)
		st.Sub = SubHeadStyle{Prefix: "§  ", PrefixColor: grey, PrefixFont: fontSerif, Size: 18, Weight: 600}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"○ 提示", "◐ 技巧", "● 重要", "警告", "当心"},
			LabelPos: CalloutLabelColumn, LabelW: 84, LabelFamily: fontSans, LabelSize: 12, LabelTrack: 0.14, LabelWeight: 500,
			Bar: -1, Rule: rgbHex(0xc9c5bb), PadY: 10, Gap: 0.01,
			Tones: [5]CalloutTone{
				{Label: grey, TopRule: ink, TopRuleW: 1},
				{Label: rgbHex(0x1c1b19)},
				{Label: ink, Bold: true},
				{Label: paper, LabelBG: ink},
				{Label: paper, LabelBG: ink, TopRule: ink, TopRuleW: 2, Bold: true},
			}}
		ls.NumberColor, ls.NumberFont = grey, fontSerif
		ls.Bullet2Glyph, ls.Bullet2Color = "–", grey
		ls.TodoBorder, ls.TodoCheck, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = rgbHex(0x1c1b19), rgbHex(0x1c1b19), 0.01, grey, true
		in.StrikeColor, in.UnderColor, in.UnderThick = grey, ink, 0.5
		in.CodeBG, in.CodeFG, in.CodeRadius = rgbaHex(0x111111, 0.06), ink, -1
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: rgbHex(0x1c1b19), BorderW: 0.5, Radius: 0.01,
			Caption: true, CaptionPrefix: "图 {n} · ", CaptionFamily: fontSerif, CaptionItalic: true, CaptionColor: grey, CaptionCenter: true}

	case model.ExportThemeJournal:
		brown := rgbHex(0x6b5b44)
		st.Sub = SubHeadStyle{Mark: SubMarkDashUnder, MarkColor: rgbHex(0xe08a1e), Size: 21, Color: rgbHex(0x3b3223), Weight: 400}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"提示｜", "技巧｜", "重要｜", "警告｜", "当心｜"},
			LabelPos: CalloutLabelInline, LabelFamily: fontKai, LabelWeight: 400,
			Bar: -1, Angle: 0.5, Shadow: rgbaHex(0x6e5014, 0.12), PadX: 16, PadY: 10, Gap: 10,
			Tones: [5]CalloutTone{
				{BG: rgbHex(0xe3eef9), Label: rgbHex(0x2f5f8a)},
				{BG: rgbHex(0xe4f2dc), Label: rgbHex(0x3f6b2c)},
				{BG: rgbHex(0xefe4f7), Label: rgbHex(0x6a3f8a)},
				{BG: rgbHex(0xffe9a8), Label: rgbHex(0x7a4f00)},
				{BG: rgbHex(0xfbd9d3), Label: rgbHex(0x9a3324)},
			}}
		ls.NumberShape, ls.NumberStroke, ls.NumberColor, ls.NumberFont, ls.NumberSize = NumberShapeCircle, brown, rgbHex(0x2a2620), fontKai, 13
		ls.Number = NumberBare
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletDiamond, 7, rgbHex(0xf2ba60)
		in.StrikeColor, in.UnderColor, in.UnderThick = rgbHex(0xd97706), rgbHex(0x9fc5e8), 2
		in.CodeBG, in.CodeFG = rgbHex(0xfff3c4), rgbHex(0x3a3226)
		st.Divider = DividerStyle{Kind: DividerWave, Width: 1.5, Color: rgbHex(0xc9b88f)}
		st.Image = ImageStyle{Frame: ImageFramePolaroid, Pad: 12, BG: rgbHex(0xffffff), Shadow: rgbaHex(0x6e5014, 0.18), Angle: 1.4,
			Tape: rgbaHex(0x9fc5e8, 0.6), Caption: true, CaptionFamily: fontKai, CaptionSize: 16, CaptionColor: rgbHex(0x5a5040), CaptionCenter: true}

	case model.ExportThemeRiso:
		blue, pink := rgbHex(0x1d3fa8), rgbHex(0xff5aa8)
		st.Sub = SubHeadStyle{Mark: SubMarkBand, MarkColor: rgbaHex(0xff5aa8, 0.75), Color: blue, Weight: 800, Size: 18}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"NOTE 提示", "TIP 技巧", "IMPORTANT 重要", "WARNING 警告", "CAUTION 当心"},
			LabelPos: CalloutLabelColumn, LabelW: 118, LabelFamily: fontMono, LabelSize: 11, LabelTrack: 0.12,
			Bar: -1, BorderW: 1.5, PadX: 14, PadY: 10, Gap: 8,
			Tones: [5]CalloutTone{
				{Border: blue, Label: blue},
				{BG: rgbHex(0xffd2e6), Label: blue},
				{BG: rgbaHex(0x1d3fa8, 0.1), Border: blue, Label: blue, FG: blue, Bold: true},
				{BG: pink, Label: blue, FG: rgbHex(0x1f2a44), Bold: true},
				{BG: blue, Label: rgbHex(0xffd2e6), FG: rgbHex(0xffffff), Bold: true},
			}}
		ls.Number, ls.NumberShape, ls.NumberFill, ls.NumberColor, ls.NumberFont, ls.NumberSize = NumberBare, NumberShapeSquare, blue, rgbHex(0xffffff), fontMono, 12
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletSquare, 6, pink
		ls.TodoFill, ls.TodoCheck, ls.TodoBorder, ls.TodoRadius, ls.DoneStrike = true, pink, blue, 0.01, true
		in.StrikeColor, in.UnderColor, in.UnderThick = pink, blue, 2
		in.CodeBG, in.CodeFG, in.CodeRadius = blue, rgbHex(0xffffff), -1
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: blue, BorderW: 2, Offset: 6, Radius: 0.01,
			Caption: true, CaptionPrefix: "FIG.{n} ", CaptionFamily: fontMono, CaptionSize: 11, CaptionTrack: 0.12, CaptionColor: blue}

	case model.ExportThemePress:
		red, ink := rgbHex(0x9c2b1f), rgbHex(0x1a1814)
		st.Sub = SubHeadStyle{Prefix: "▍", PrefixColor: red, Family: fontSans, Color: red, Size: 14, Weight: 600, Track: 0.2}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"【提示】", "【技巧】", "【重要】", "【警告】", "【当心】"},
			LabelPos: CalloutLabelInline, LabelFamily: fontSans,
			Bar: -1, Rule: rgbHex(0x8c826e), PadX: 10, PadY: 7, Gap: 4,
			Tones: [5]CalloutTone{
				{Label: ink}, {Label: ink}, {Label: red},
				{Border: rgbHex(0x23201b), Label: red},
				{BG: ink, Label: rgbHex(0xe8a092), FG: rgbHex(0xefe7d3)},
			}}
		ls.Bullet2Glyph, ls.Bullet2Color = "▪", rgbHex(0x5a5244)
		ls.TodoBorder, ls.TodoCheck, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = ink, red, 0.01, rgbHex(0x5a5244), true
		in.StrikeColor, in.UnderColor = red, ink
		in.CodeBG, in.CodeRadius = rgbaHex(0xffffff, 0.35), -1
		st.Divider = DividerStyle{Kind: DividerText, Text: "❦", Track: 1, Color: rgbHex(0x5a5244)}
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: rgbHex(0x23201b), BorderW: 0.5, Radius: 0.01,
			Caption: true, CaptionPrefix: "图 · ", CaptionFamily: fontSerif, CaptionColor: rgbHex(0x5a5244)}

	case model.ExportThemePoster:
		ink, orange, paper := rgbHex(0x141414), rgbHex(0xe2471b), rgbHex(0xf2efe9)
		st.Sub = SubHeadStyle{Mark: SubMarkSquare, MarkColor: orange, Weight: 900, Size: 20}
		st.Callout = CalloutStyle{Custom: true,
			Labels:      [5]string{"NOTE / 提示", "TIP / 技巧", "IMPORTANT / 重要", "WARNING / 警告", "CAUTION / 当心"},
			LabelFamily: fontMono, LabelSize: 11, LabelTrack: 0.2, LabelWeight: 500,
			Bar: -1, PadX: 12, PadY: 10, Gap: 6,
			Tones: [5]CalloutTone{
				{Label: ink, TopRule: ink, TopRuleW: 3},
				{Label: ink, TopRule: ink, TopRuleW: 3},
				{Label: orange, TopRule: orange, TopRuleW: 3, Bold: true},
				{BG: ink, Label: orange, FG: paper, Bold: true},
				{BG: orange, Label: paper, FG: paper, Bold: true},
			}}
		ls.Number, ls.NumberColor = NumberPadded, ink
		ls.Bullet2Glyph, ls.Bullet2Color = "/", orange
		ls.TodoFill, ls.TodoCheck, ls.TodoBorder, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = true, ink, ink, 0.01, rgbHex(0x8a8a8a), true
		in.StrikeColor, in.UnderColor, in.UnderThick = orange, ink, 3
		in.CodeBG, in.CodeFG, in.CodeRadius = ink, paper, -1
		st.Image = ImageStyle{Radius: 0.01, Caption: true, CaptionPrefix: "FIG.{n}\n", CaptionFamily: fontMono, CaptionSize: 11, CaptionTrack: 0.16, CaptionColor: ink, CaptionSide: 120}
		st.Divider.EndColor, st.Divider.EndWidth = orange, 48

	case model.ExportThemeMist:
		accent := rgbHex(0x3b5b8c)
		st.Sub = SubHeadStyle{Family: fontSans, Color: accent, Size: 15, Weight: 600}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"提示 ·", "技巧 ·", "重要 ·", "警告 ·", "当心 ·"},
			LabelPos: CalloutLabelInline, LabelWeight: 600,
			Bar: -1, Radius: 12, PadX: 14, PadY: 11, Gap: 10,
			Tones: [5]CalloutTone{
				{BG: rgbHex(0xeef3fb), Label: accent, FG: rgbHex(0x2a3f60)},
				{BG: rgbHex(0xedf5ef), Label: rgbHex(0x4f7a5c), FG: rgbHex(0x2c4a35)},
				{BG: rgbHex(0xf2effa), Label: rgbHex(0x6b5ca8), FG: rgbHex(0x43386b)},
				{BG: rgbHex(0xfbf3e4), Label: rgbHex(0xa87a26), FG: rgbHex(0x5e4413)},
				{BG: rgbHex(0xfbeeee), Label: rgbHex(0xb04848), FG: rgbHex(0x6b2c2c)},
			}}
		ls.Number, ls.NumberShape, ls.NumberFill, ls.NumberColor, ls.NumberSize = NumberBare, NumberShapeCircle, rgbHex(0xeef2f8), accent, 11
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletRing, 5, rgbHex(0x9fb3cf)
		ls.TodoFill, ls.TodoCheck, ls.TodoBorder, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = true, accent, rgbHex(0xc3cfe0), 5, rgbHex(0x8a94a6), true
		in.StrikeColor, in.UnderColor = rgbHex(0x8a94a6), rgbHex(0x9fb3cf)
		in.CodeBG, in.CodeFG, in.CodeRadius = rgbHex(0xeef2f8), accent, 5
		st.Image = ImageStyle{Frame: ImageFrameMat, Pad: 10, BG: rgbHex(0xffffff), Border: rgbHex(0xe6e2dc), Radius: 16, InnerRadius: 10,
			Shadow: rgbaHex(0x1e2d46, 0.06), Caption: true, CaptionColor: rgbHex(0x8a94a6)}

	case model.ExportThemeInk:
		gold, light := rgbHex(0xc8a66a), rgbHex(0xf2efe8)
		st.Sub = SubHeadStyle{Size: 15, Track: 0.3, Color: gold, Weight: 400}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"提示", "技巧", "重要", "警告", "当心"},
			LabelPos: CalloutLabelColumn, LabelW: 56, LabelSize: 12, LabelTrack: 0.4, LabelWeight: 400,
			Bar: -1, BorderW: 0.5, PadY: 8, Gap: 8,
			Tones: [5]CalloutTone{
				{Label: rgbHex(0x8f8a7f)}, {Label: rgbHex(0xa9b58a)}, {Label: gold, FG: light},
				{Border: gold, Label: rgbHex(0xe0b46a), FG: light},
				{BG: light, Label: rgbHex(0x8a2e22), FG: rgbHex(0x141311)},
			}}
		ls.Number, ls.NumberColor, ls.NumberFont = NumberRoman, gold, fontSerif
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletDash, 1, rgbHex(0x6a665d)
		ls.TodoBorder, ls.TodoCheck, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = rgbHex(0x8f8a7f), gold, 0.01, rgbHex(0x6a665d), true
		in.StrikeColor, in.UnderColor = rgbHex(0x6a665d), gold
		in.CodeBG, in.CodeFG = rgbaHex(0xc8a66a, 0.1), gold
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: rgbHex(0x3a3732), BorderW: 0.5, Radius: 0.01,
			Caption: true, CaptionPrefix: "— ", CaptionFamily: fontSerif, CaptionItalic: true, CaptionTrack: 0.1, CaptionColor: rgbHex(0x7d786e)}

	case model.ExportThemeKraft:
		gold, bright, muted := rgbHex(0xc9a352), rgbHex(0xd6b05c), rgbHex(0xa89b80)
		light := rgbHex(0xf2e9d2)
		st.Sub = SubHeadStyle{Prefix: "✦ ", PrefixColor: gold, Color: light, Size: 21, Weight: 400}
		st.Callout = CalloutStyle{Custom: true,
			Labels:      [5]string{"❋ 提示", "❋ 技巧", "✦ 重要", "▲ 警告", "✕ 当心"},
			LabelFamily: fontKai, LabelSize: 14, LabelWeight: 400,
			Bar: -1, Radius: 6, PadX: 16, PadY: 12, Gap: 10,
			Tones: [5]CalloutTone{
				{Border: rgbaHex(0xc9a352, 0.6), Dashed: true, Label: muted},
				{Border: rgbaHex(0xa9be78, 0.7), Dashed: true, Label: rgbHex(0xb5c48a)},
				{BG: rgbaHex(0xd6b05c, 0.08), Border: gold, Label: bright, FG: light},
				{BG: rgbaHex(0xd08a4a, 0.12), Border: rgbHex(0xd08a4a), Label: rgbHex(0xe6a466), FG: light},
				{BG: gold, Label: rgbHex(0x5a3a12), FG: rgbHex(0x1d1b18), Bold: true},
			}}
		ls.Number, ls.NumberShape, ls.NumberStroke, ls.NumberColor, ls.NumberSize = NumberBare, NumberShapeCircle, gold, bright, 13
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletRing, 6, gold
		ls.Todo, ls.TodoBorder, ls.TodoCheck, ls.DoneStrike = TodoBox, gold, bright, true
		in.StrikeColor, in.UnderColor = gold, gold
		in.CodeBG, in.CodeFG = rgbaHex(0x000000, 0.25), bright
		st.Image = ImageStyle{Frame: ImageFrameMat, Pad: 10, BG: rgbaHex(0x000000, 0.25), Border: gold, Radius: 4, Angle: -1.2, Corners: true,
			Caption: true, CaptionFamily: fontKai, CaptionSize: 15, CaptionColor: muted, CaptionCenter: true}

	case model.ExportThemeNeon:
		pink, cyan, light := rgbHex(0xff3d8b), rgbHex(0x7dd3fc), rgbHex(0xf4f1ff)
		st.Sub = SubHeadStyle{Prefix: "> ", PrefixColor: rgbHex(0x5d5874), PrefixFont: fontMono, Color: cyan, Size: 16, Weight: 600}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"[NOTE]", "[TIP]", "[IMPORTANT]", "[WARNING]", "[CAUTION]"},
			LabelPos: CalloutLabelColumn, LabelW: 92, LabelFamily: fontMono, LabelSize: 11, LabelTrack: 0.16, LabelWeight: 500,
			Bar: -1, Radius: 8, PadX: 14, PadY: 10, Gap: 8,
			Tones: [5]CalloutTone{
				{BG: rgbaHex(0x7dd3fc, 0.06), Border: rgbaHex(0x7dd3fc, 0.3), Label: cyan},
				{BG: rgbaHex(0x86efac, 0.06), Border: rgbaHex(0x86efac, 0.3), Label: rgbHex(0x86efac)},
				{BG: rgbaHex(0xc4b5fd, 0.08), Border: rgbaHex(0xc4b5fd, 0.45), Label: rgbHex(0xc4b5fd), FG: light},
				{BG: rgbaHex(0xfbbf24, 0.08), Border: rgbaHex(0xfbbf24, 0.5), Label: rgbHex(0xfcd34d), FG: light},
				{BG: rgbaHex(0xff3d8b, 0.12), Border: pink, Label: pink, FG: light},
			}}
		ls.NumberColor = pink
		ls.Bullet2Glyph, ls.Bullet2Color = "└", rgbHex(0x5d5874)
		ls.TodoFill, ls.TodoCheck, ls.DoneFG, ls.DoneStrike = true, pink, rgbHex(0x5d5874), true
		in.StrikeColor, in.UnderColor = pink, pink
		in.CodeBG, in.CodeFG = rgbaHex(0xff3d8b, 0.12), rgbHex(0xff8fbd)
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: rgbaHex(0xffffff, 0.12), Radius: 8,
			Caption: true, CaptionPrefix: "IMG_{n} · ", CaptionFamily: fontMono, CaptionSize: 11, CaptionTrack: 0.2, CaptionColor: rgbHex(0x8f8aa8)}

	case model.ExportThemeFilm:
		red, amber, light := rgbHex(0xd9482f), rgbHex(0xe8a25a), rgbHex(0xefe4cf)
		st.Sub = SubHeadStyle{Prefix: "▸ ", PrefixColor: red, Family: fontMonoCJK, Color: amber, Size: 14, Track: 0.16, Weight: 400}
		st.Callout = CalloutStyle{Custom: true,
			LabelPos: CalloutLabelColumn, LabelW: 100, LabelFamily: fontMono, LabelSize: 10, LabelTrack: 0.2, LabelWeight: 500,
			Bar: -1, Rule: rgbHex(0x3a3029), PadY: 10, Gap: 4,
			Tones: [5]CalloutTone{
				{Label: rgbHex(0x9a8f7e)}, {Label: rgbHex(0xa9b07a)}, {Label: amber, FG: light},
				{Border: amber, Label: amber, FG: light},
				{BG: red, Label: rgbHex(0x120f0d), FG: rgbHex(0x120f0d), Bold: true},
			}}
		ls.Bullet2Glyph, ls.Bullet2Color = "·", rgbHex(0x7d7264)
		ls.TodoRadius, ls.DoneFG, ls.DoneStrike = 0.01, rgbHex(0x7d7264), true
		in.StrikeColor, in.UnderColor = red, red
		st.Image = ImageStyle{Frame: ImageFrameMat, Pad: 10, BG: rgbHex(0x0a0807), Border: rgbHex(0x3a3029), BorderW: 0.5,
			Caption: true, CaptionPrefix: "{n} · ", CaptionFamily: fontMonoCJK, CaptionSize: 10, CaptionTrack: 0.2, CaptionColor: rgbHex(0x7d7264)}

	case model.ExportThemeNight:
		amber, light := rgbHex(0xe8a642), rgbHex(0xf1e6d2)
		st.Sub = SubHeadStyle{Color: amber, Size: 18, Weight: 600}
		st.Callout = CalloutStyle{Custom: true,
			Labels:   [5]string{"提示", "技巧", "重要", "警告", "当心"},
			LabelPos: CalloutLabelInline, LabelFamily: fontSerif, LabelWeight: 600,
			Bar: -1, Radius: 12, PadX: 16, PadY: 12, Gap: 8,
			Tones: [5]CalloutTone{
				{BG: rgbHex(0x33302c), Label: rgbHex(0xc9bca6)},
				{BG: rgbHex(0x2f3226), Label: rgbHex(0xb9c48a)},
				{BG: rgbHex(0x3a2f22), Label: amber, FG: light},
				{BG: rgbHex(0x45311c), Border: rgbaHex(0xe8a642, 0.35), Label: rgbHex(0xf5c26b), FG: light},
				{BG: rgbHex(0x4a2620), Border: rgbaHex(0xe87860, 0.45), Label: rgbHex(0xf09a84), FG: light},
			}}
		ls.Number, ls.NumberShape, ls.NumberFill, ls.NumberColor, ls.NumberFont, ls.NumberSize = NumberBare, NumberShapeCircle, rgbHex(0x3a2d24), amber, fontSerif, 12
		ls.Bullet2, ls.Bullet2Size, ls.Bullet2Color = BulletRing, 5, rgbHex(0xb88a3e)
		ls.TodoFill, ls.TodoCheck, ls.TodoBorder, ls.TodoRadius, ls.DoneFG, ls.DoneStrike = true, amber, rgbHex(0x6a5848), 8, rgbHex(0x8a7a62), true
		in.StrikeColor, in.UnderColor = rgbHex(0x8a7a62), amber
		in.CodeBG, in.CodeFG, in.CodeRadius = rgbHex(0x221a15), rgbHex(0xf5c26b), 6
		st.Image = ImageStyle{Radius: 14, Caption: true, CaptionFamily: fontSerif, CaptionSize: 13, CaptionItalic: true, CaptionColor: rgbHex(0xa8977c), CaptionCenter: true}

	case model.ExportThemeTerminal:
		green, bright, dim := rgbHex(0x7ee787), rgbHex(0xb5f5bb), rgbHex(0x3f8a47)
		yellow, bg := rgbHex(0xe3d26f), rgbHex(0x071008)
		st.Sub = SubHeadStyle{Size: 15, Color: green, Weight: 700}
		st.Callout = CalloutStyle{Custom: true,
			Labels:      [5]string{"> [!NOTE]", "> [!TIP]", "> [!IMPORTANT]", "> [!WARNING]", "> [!CAUTION]"},
			LabelFamily: fontMono, LabelSize: 14, LabelWeight: 400,
			Bar: 2, PadX: 12, PadY: 4, Gap: 8,
			Tones: [5]CalloutTone{
				{Label: dim}, {Label: rgbHex(0x5fb768)}, {Label: bright, FG: bright},
				{Border: yellow, Label: yellow, FG: yellow},
				{BG: green, Label: bg, FG: bg, Bold: true},
			}}
		ls.Bullet2Glyph, ls.Bullet2Color = "-", dim
		in.StrikeColor, in.UnderColor = dim, green
		in.CodeBG, in.CodeFG, in.CodeRadius = green, bg, -1
		st.Image = ImageStyle{Frame: ImageFrameBorder, Border: dim, Radius: 0.01,
			Caption: true, CaptionPrefix: "fig.{n}  ", CaptionFamily: fontMonoCJK, CaptionColor: dim}
	}
}
