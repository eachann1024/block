package markdown

import (
	"block/internal/model"

	"github.com/egoist/mygo/ui"
)

// The 12 paper export themes. Each one is a complete, independent design:
// substrate, ornaments, type, and the treatment of every block kind.

// finishSpec derives the palette slots a constructor leaves zero.
func finishSpec(s PaperThemeSpec) PaperThemeSpec {
	s.InnerCanvas = orC(s.InnerCanvas, s.Canvas)
	s.Title = orC(s.Title, s.Text)
	s.Secondary = orC(s.Secondary, mixUIColor(s.InnerCanvas, s.Text, 0.55))
	s.Accent2 = orC(s.Accent2, s.Accent)
	s.CodeBG = orC(s.CodeBG, mixUIColor(s.InnerCanvas, s.Text, 0.05))
	s.CodeBorder = orC(s.CodeBorder, mixUIColor(s.InnerCanvas, s.Text, 0.14))
	s.QuoteFG = orC(s.QuoteFG, s.Text)
	s.QuoteBar = orC(s.QuoteBar, s.Accent)
	s.TableBorder = orC(s.TableBorder, mixUIColor(s.InnerCanvas, s.Text, 0.16))
	s.Divider = orC(s.Divider, mixUIColor(s.InnerCanvas, s.Text, 0.18))
	s.Swatch = [3]ui.Color{s.Canvas, s.Accent, s.InnerCanvas}
	if s.HeadSizes == ([3]float32{}) {
		s.HeadSizes = [3]float32{s.BodySize * 1.65, s.BodySize * 1.3, s.BodySize * 1.12}
	}
	applyBlockStyles(&s)
	if s.Card != nil && s.Card.Fill.A == 255 {
		s.InnerCanvas = s.Card.Fill
		s.Swatch[2] = s.Card.Fill
	}
	return s
}

func monoTheme() PaperThemeSpec {
	ink := rgbHex(0x111111)
	rule := rgbHex(0xdcd8cf)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeMono,
		Canvas:      rgbHex(0xfaf8f3),
		Text:        rgbHex(0x2a2926),
		Title:       ink,
		Secondary:   rgbHex(0x7a776f),
		Accent:      ink,
		QuoteFG:     rgbHex(0x4a4842),
		TableBorder: rule,
		Divider:     rgbHex(0xd9d5cc),
		PadTop:      88, PadRight: 104, PadBottom: 64, PadLeft: 120,
		BodySize: 17, Leading: 1.95, TitleSize: 40,
		HeadSizes:   [3]float32{28, 22, 19},
		Mark:        TitleMarkRule,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSerif,
		Radius: 2,
		Style: PaperStyle{
			Ornaments:  []Ornament{{Kind: OrnamentEdgeRule, X: 72, W: 0.7, Color: rgbHex(0xc9c4b8)}},
			Title:      TitleStyle{Weight: 600, RuleThick: 0.8, Gap: 38},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarDiamond, Color: rgbHex(0x8c877c), Color2: rgbHex(0xccc7bc)},
			Lead:       LeadStyle{Color: ink, Weight: 600},
			Heading:    HeadingStyle{Weight: 600},
			Inline:     InlineStyle{Highlight: &HighlightStyle{Rule: ink, RuleW: 1.5}},
			Table:      TableStyle{Rules: true, Top: 1, HeadSep: 0.5, RowSep: 0.5, Bottom: 1, RuleColor: rule, StrongColor: ink, HeaderFont: fontSans, HeaderSize: 12, HeaderUpper: true, HeaderTrack: 0.12, HeaderWt: 500, HeaderFG: rgbHex(0x7a776f), PadX: 4},
			List:       ListStyle{Bullet: BulletMiddot},
			Quote:      QuoteStyle{Kind: QuoteIndent, Indent: 32, Italic: true},
			Code:       CodeStyle{Kind: CodePlain, Size: 13, Leading: 1.8},
			Divider:    DividerStyle{Kind: DividerText, Text: "·   ·   ·"},
			Footer:     FooterStyle{Left: "cooool.top", Right: "{date}", Rule: FooterNone, Family: fontSerif, Size: 12, Color: rgbHex(0x9a968c)},
		},
	})
}

func journalTheme() PaperThemeSpec {
	brown := rgbHex(0x6b5b44)
	orange := rgbHex(0xb45309)
	muted := rgbHex(0x8a7f6e)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeJournal,
		Canvas:      rgbHex(0xfbf5df),
		Text:        rgbHex(0x2a2620),
		Title:       rgbHex(0x2a2620),
		Secondary:   muted,
		Accent:      rgbHex(0xd97706),
		Accent2:     rgbHex(0x9fc5e8),
		CodeBG:      rgbaHex(0xc9dcef, 0.35),
		CodeBorder:  rgbHex(0x9fb6cc),
		QuoteBG:     rgbHex(0xffe9a8),
		QuoteFG:     rgbHex(0x3a3226),
		QuoteBar:    rgbHex(0xe0b64a),
		TableBorder: rgbHex(0xc9b994),
		Divider:     rgbHex(0xd8c9a4),
		Card: &CardSpec{Inset: 56, InsetTop: 64, Radius: 4, Fill: rgbHex(0xfffdf5), Border: rgbHex(0xefe6cc), HasBorder: true,
			Shadow: rgbaHex(0x6e5014, 0.14), ShadowY: 10, Blur: 24},
		PadTop: 60, PadRight: 52, PadBottom: 44, PadLeft: 52,
		BodySize: 17, Leading: 1.95, TitleSize: 38,
		HeadSizes:   [3]float32{30, 26, 21},
		Mark:        TitleMarkWavy,
		TitleFamily: fontKai, HeadingFamily: fontKai, BodyFamily: fontKai,
		Radius: 8,
		Style: PaperStyle{
			Substrate: []SubstrateLayer{{Kind: SubstrateGrid, Color: rgbHex(0xd3e2f1), Gap: 16, Size: 1}},
			Ornaments: []Ornament{
				{Kind: OrnamentTape, Color: rgbaHex(0xf2ba60, 0.55), Y: 49, W: 120, H: 30, Angle: -3},
				{Kind: OrnamentStamp, Text: "{date}", Family: fontKai, Size: 22, Italic: true, Color: orange, X: 100, Y: 84, Angle: 6, BottomWhenNoTitle: true},
			},
			Title:   TitleStyle{Weight: 400, RuleWidth: 0.5, RuleColor: rgbHex(0xe08a1e)},
			Lead:    LeadStyle{Color: brown, Family: fontKai, Weight: 600},
			Heading: HeadingStyle{Prefix: HeadPrefixDot, Weight: 400, Accent: rgbHex(0xf2ba60), Accent2: rgbHex(0x9fc5e8), TopGap: 18},
			Inline: InlineStyle{Highlight: &HighlightStyle{BG: rgbaHex(0xfccd46, 0.75)},
				LinkColor: orange, LinkWavy: true},
			Table: TableStyle{Rules: true, Outer: 1.5, OuterRadius: 8, HeadSep: 1, HeadSepLine: LineDashed, RowSep: 1, RowSepLine: LineDashed,
				ColSep: 1, ColSepLine: LineDashed, RuleColor: rgbHex(0xc9b994), StrongColor: brown, HeaderFont: fontKai, HeaderSize: 16, HeaderWt: 400, HeaderFG: brown},
			List:    ListStyle{BulletColor: rgbHex(0xe08a1e), TodoBorder: brown, TodoCheck: rgbHex(0xd97706), DoneFG: muted, DoneStrike: true, NumberColor: rgbHex(0xd97706)},
			Quote:   QuoteStyle{Kind: QuoteSticky, Width: 330, Angle: 1.6, Family: fontKai, Size: 19, Leading: 1.7, PadX: 22, PadY: 18},
			Code:    CodeStyle{BorderW: 1.5, Dashed: true, Label: rgbHex(0x6f8aa6)},
			Divider: DividerStyle{Kind: DividerText, Text: "✿  ✿  ✿", Color: rgbHex(0xe0a95a)},
			Footer:  FooterStyle{Left: "cooool.top", Right: "{dateCN}", Rule: FooterDashed, RuleColor: rgbHex(0xd8c9a4), Family: fontKai, Size: 14, Color: muted},
		},
	})
}

func risoTheme() PaperThemeSpec {
	blue := rgbHex(0x1d3fa8)
	pink := rgbHex(0xff5aa8)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeRiso,
		Canvas:      rgbHex(0xf4efe4),
		Text:        rgbHex(0x262a44),
		Title:       blue,
		Secondary:   blue,
		Accent:      blue,
		Accent2:     pink,
		CodeBG:      ui.Color{},
		CodeBorder:  blue,
		QuoteBG:     rgbHex(0xffd2e6),
		QuoteFG:     blue,
		QuoteBar:    blue,
		TableBorder: blue,
		Divider:     blue,
		PadTop:      72, PadRight: 72, PadBottom: 56, PadLeft: 72,
		BodySize: 17, Leading: 1.85, TitleSize: 50,
		HeadSizes:   [3]float32{30, 24, 20},
		Mark:        TitleMarkOffset,
		TitleFamily: fontSans, HeadingFamily: fontSans, BodyFamily: fontSans,
		TitleBold: true,
		Radius:    0,
		Style: PaperStyle{
			Substrate:  []SubstrateLayer{{Kind: SubstrateGrain, Color: rgbaHex(0x281e14, 0.06), Gap: 3, Size: 0.6}},
			Ornaments:  []Ornament{{Kind: OrnamentRegMarks, Color: rgbaHex(0x1d3fa8, 0.55), X: 24, Y: 20, Size: 14}},
			Title:      TitleStyle{Weight: 900, Leading: 1.15, RuleColor: pink, Gap: 44},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarOverprint, Color: blue, Color2: rgbaHex(0xff5aa8, 0.85)},
			Lead:       LeadStyle{Color: blue, Weight: 700},
			Heading:    HeadingStyle{Prefix: HeadPrefixTag, Weight: 800, Color: blue, Accent: blue, TagFG: rgbHex(0xffffff), RuleColor: blue, PrefixFont: fontMono, TopGap: 20},
			Inline: InlineStyle{BoldColor: blue, BoldWeight: 800, Highlight: &HighlightStyle{BG: rgbaHex(0xff5aa8, 0.45)},
				LinkColor: blue, LinkDeco: pink, LinkThick: 3, LinkOffset: 2, CodeBG: rgbaHex(0xff5aa8, 0.22), CodeFG: blue},
			Table: TableStyle{Rules: true, HeadSep: 0, RowSep: 1.5, Bottom: 1.5, RuleColor: blue, HeaderBG: pink, HeaderFG: blue, HeaderWt: 800,
				Size: 15, Leading: 1.7, PadY: 8},
			List:    ListStyle{Bullet: BulletSquare, BulletSize: 9, BulletColor: pink, NumberColor: pink, NumberFont: fontMono, TodoBorder: blue, TodoCheck: pink},
			Quote:   QuoteStyle{Kind: QuoteOffset, Accent: blue, Weight: 700, Size: 20, Leading: 1.6, PadX: 24, PadY: 18},
			Code:    CodeStyle{BorderW: 2, Size: 13, Leading: 1.7, FG: blue, Label: pink},
			Divider: DividerStyle{Kind: DividerRepeat, Text: "■ ", Color: pink},
			Footer: FooterStyle{Left: "cooool.top", Right: "{date}", Rule: FooterLine, RuleColor: blue, Family: fontMono, Size: 11, Track: 0.12,
				Color: blue, Swatches: []ui.Color{blue, pink}},
		},
	})
}

func pressTheme() PaperThemeSpec {
	red := rgbHex(0x9c2b1f)
	ink := rgbHex(0x1f1a12)
	rule := rgbHex(0x8c826e)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemePress,
		Canvas:      rgbHex(0xefe7d3),
		Text:        rgbHex(0x2a2418),
		Title:       ink,
		Secondary:   rgbHex(0x5a5244),
		Accent:      red,
		CodeBG:      rgbaHex(0xffffff, 0.25),
		CodeBorder:  rule,
		QuoteFG:     ink,
		QuoteBar:    ink,
		TableBorder: rule,
		Divider:     ink,
		PadTop:      56, PadRight: 72, PadBottom: 56, PadLeft: 72,
		BodySize: 18, Leading: 1.9, TitleSize: 46,
		HeadSizes:   [3]float32{26, 20, 18},
		Mark:        TitleMarkDoubleRule,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSerif,
		TitleBold: true,
		Radius:    0,
		Style: PaperStyle{
			Substrate: []SubstrateLayer{
				{Kind: SubstrateGrain, Color: rgbaHex(0x3c2d14, 0.07), Gap: 3, Size: 0.6},
				{Kind: SubstrateGrain, Color: rgbaHex(0x3c2d14, 0.05), Gap: 5, Size: 0.9},
			},
			Title:      TitleStyle{Weight: 900, Leading: 1.2, Track: 0.08, Center: true, TopRule: true, RuleColor: ink, Gap: 32},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarDoubleRule, Color: ink},
			Lead:       LeadStyle{Color: ink, Weight: 700},
			Heading:    HeadingStyle{Weight: 900, Underline: true, RuleColor: ink, TopGap: 16},
			Inline: InlineStyle{Highlight: &HighlightStyle{BG: rgbaHex(0x9c2b1f, 0.12), Rule: red, RuleW: 2},
				LinkColor: red, LinkThick: 1},
			Table: TableStyle{Rules: true, Top: 2, HeadSep: 1, RowSep: 1, RowSepLine: LineDotted, Bottom: 2, RuleColor: rule, StrongColor: ink,
				HeaderSize: 12, HeaderWt: 700, HeaderTrack: 0.1},
			List:    ListStyle{Number: NumberCJK, NumberFont: fontSerif, NumberColor: red, BulletColor: red, Bullet: BulletSquare, BulletSize: 5},
			Quote:   QuoteStyle{Kind: QuotePull, Family: fontSerif, Weight: 900, Size: 30, Leading: 1.45, Accent: red, PadY: 22},
			Code:    CodeStyle{BorderW: 0.5, Size: 12, Leading: 1.6},
			Divider: DividerStyle{Kind: DividerText, Text: "＊　＊　＊", Color: red},
			Footer:  FooterStyle{Left: "cooool.top 编辑部 出品", Right: "{date}", Rule: FooterLine, RuleColor: ink, Family: fontSerif, Size: 12, Track: 0.1, Color: rgbHex(0x5a5244)},
			Justify: true,
		},
	})
}

func posterTheme() PaperThemeSpec {
	ink := rgbHex(0x141414)
	orange := rgbHex(0xe2471b)
	paper := rgbHex(0xf2efe9)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemePoster,
		Canvas:      paper,
		Text:        rgbHex(0x2b2b2b),
		Title:       ink,
		Secondary:   rgbHex(0x6b6b6b),
		Accent:      orange,
		Accent2:     ink,
		CodeBG:      ink,
		CodeBorder:  ink,
		QuoteFG:     ink,
		QuoteBar:    orange,
		TableBorder: ink,
		Divider:     ink,
		PadTop:      64, PadRight: 72, PadBottom: 56, PadLeft: 64,
		BodySize: 17, Leading: 1.85, TitleSize: 132,
		HeadSizes:   [3]float32{36, 28, 22},
		Mark:        TitleMarkRule,
		TitleFamily: fontSans, HeadingFamily: fontSans, BodyFamily: fontSans,
		TitleBold: true,
		Radius:    0,
		Style: PaperStyle{
			Substrate:  []SubstrateLayer{{Kind: SubstrateDots, Color: rgbaHex(0x141414, 0.045), Gap: 4, Size: 0.7}},
			Ornaments:  []Ornament{{Kind: OrnamentSideText, Text: "{date}", Family: fontMono, Size: 11, Track: 0.3, X: 30, Y: 64, Color: rgbHex(0x8a8a8a)}},
			Title:      TitleStyle{Weight: 900, Leading: 0.98, Track: -0.04, MaxLines: 3, MinSize: 56, RuleThick: 4, RuleColor: ink, Gap: 36, Accent: &TitleAccent{FG: paper, BG: orange}},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarSplitBar, Color: ink, Color2: orange},
			Lead:       LeadStyle{Color: ink, Weight: 800},
			Heading:    HeadingStyle{Weight: 900, TopGap: 22},
			Inline:     InlineStyle{BoldWeight: 900, BoldColor: ink, Highlight: &HighlightStyle{FG: orange, Weight: 700}, LinkColor: ink, LinkDeco: orange, LinkThick: 3, LinkOffset: 2},
			Table:      TableStyle{Rules: true, Top: 6, HeadSep: 1, RowSep: 1, Bottom: 1, RuleColor: rgbHex(0xc9c5bd), StrongColor: ink, HeaderWt: 900, HeaderSize: 13, HeaderUpper: true, PadX: 6},
			List:       ListStyle{Bullet: BulletArrow, BulletColor: orange, NumberColor: orange, NumberFont: fontMono, TodoBorder: ink, TodoCheck: orange},
			Quote:      QuoteStyle{Kind: QuoteIndent, Weight: 900, Size: 34, Leading: 1.25, Accent: orange, Indent: 0, PadY: 6},
			Code:       CodeStyle{BorderW: -1, Size: 12, Leading: 1.6, FG: paper, Label: orange},
			Divider:    DividerStyle{Width: 6, Color: ink},
			Footer:     FooterStyle{Left: "cooool.top", Right: "{date}", Rule: FooterLine, RuleColor: ink, Family: fontMono, Size: 11, Track: 0.2, Color: ink},
		},
	})
}

func mistTheme() PaperThemeSpec {
	accent := rgbHex(0x3b5b8c)
	soft := rgbHex(0xb9c8de)
	secondary := rgbHex(0x8a94a6)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeMist,
		Canvas:      rgbHex(0xe3e8ef),
		InnerCanvas: rgbHex(0xffffff),
		Text:        rgbHex(0x3c4656),
		Title:       rgbHex(0x141b26),
		Secondary:   secondary,
		Accent:      accent,
		Accent2:     soft,
		CodeBG:      rgbHex(0xf5f7fa),
		CodeBorder:  rgbHex(0xe6e9ee),
		QuoteBG:     rgbHex(0xf5f7fa),
		QuoteFG:     rgbHex(0x3c4656),
		QuoteBar:    soft,
		TableBorder: rgbHex(0xeef0f3),
		Divider:     rgbHex(0xe3e6eb),
		PadTop:      56, PadRight: 56, PadBottom: 48, PadLeft: 56,
		BodySize: 16, Leading: 1.9, TitleSize: 38,
		HeadSizes:   [3]float32{28, 22, 18},
		Mark:        TitleMarkBar,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSans,
		Radius: 10,
		Style: PaperStyle{
			Substrate: []SubstrateLayer{{Kind: SubstrateGradient, Color: rgbHex(0xeef1f5), Color2: rgbHex(0xe3e8ef), Color3: rgbHex(0xdde2ea), Angle: 165}},
			Sections:  &SectionCards{Fill: rgbHex(0xffffff), Border: rgbHex(0xe4e8ef), Shadow: rgbaHex(0x1e2d46, 0.05), Radius: 16, PadX: 36, PadY: 28, Gap: 16},
			Title:     TitleStyle{Weight: 600, Leading: 1.3, RuleColor: soft, Gap: 22},
			Lead:      LeadStyle{Color: rgbHex(0x1e3557), Weight: 600},
			Heading:   HeadingStyle{Weight: 400, TopGap: 4},
			Inline:    InlineStyle{Highlight: &HighlightStyle{BG: rgbaHex(0x7896c8, 0.18)}, LinkColor: accent, LinkDeco: soft, LinkThick: 1.5, LinkOffset: 3},
			Table: TableStyle{Rules: true, HeadSep: 1, RowSep: 1, RuleColor: rgbHex(0xeef0f3), StrongColor: rgbHex(0xe3e6eb),
				HeaderSize: 12, HeaderWt: 500, HeaderFG: secondary, HeaderUpper: true, HeaderTrack: 0.1, HeaderFont: fontSans, PadX: 12, PadY: 10, Secondary: rgbHex(0x5a6476)},
			List:    ListStyle{BulletSize: 6, BulletColor: rgbHex(0x9fb3cf), NumberColor: rgbHex(0x9fb3cf), TodoBorder: rgbHex(0x9fb3cf), TodoCheck: accent},
			Quote:   QuoteStyle{Kind: QuoteBigMark, Card: true, Accent: soft, PadX: 28, PadY: 20},
			Code:    CodeStyle{BorderW: -1, Size: 13.5},
			Divider: DividerStyle{Kind: DividerText, Text: "·  ·  ·", Color: soft},
			Footer:  FooterStyle{Center: "cooool.top · {date}", Rule: FooterNone, Family: fontSans, Size: 11, Track: 0.2, Color: rgbHex(0x9aa3b2), Gap: 24},
		},
	})
}

func inkTheme() PaperThemeSpec {
	gold := rgbHex(0xc8a66a)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeInk,
		Dark:        true,
		Canvas:      rgbHex(0x141311),
		Text:        rgbHex(0xd9d4c9),
		Title:       rgbHex(0xf2efe8),
		Secondary:   rgbHex(0x8f8a7f),
		Accent:      gold,
		CodeBG:      rgbHex(0x1b1a17),
		CodeBorder:  rgbHex(0x2a2824),
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0xe6e1d6),
		QuoteBar:    rgbHex(0x2a2824),
		TableBorder: rgbHex(0x3a3732),
		Divider:     rgbHex(0x3a3732),
		PadTop:      96, PadRight: 112, PadBottom: 72, PadLeft: 112,
		BodySize: 17, Leading: 2.05, TitleSize: 40,
		HeadSizes:   [3]float32{26, 20, 18},
		Mark:        TitleMarkRule,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSerif,
		Radius: 2,
		Style: PaperStyle{
			Substrate:  []SubstrateLayer{{Kind: SubstrateDots, Color: rgbaHex(0xfffaeb, 0.025), Gap: 5, Size: 0.8}},
			Title:      TitleStyle{Weight: 400, Leading: 1.35, Track: 0.16, RuleWidth: 0.3, RuleThick: 1, RuleColor: rgbaHex(0xc8a66a, 0.6), Gap: 42},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarDiamond, Color: gold, Color2: rgbaHex(0xc8a66a, 0.55), Centered: true},
			Lead:       LeadStyle{Color: rgbHex(0xf2efe8), Weight: 600},
			Heading:    HeadingStyle{Weight: 400, Track: 0.16, TopGap: 24},
			Inline:     InlineStyle{BoldColor: rgbHex(0xf2efe8), BoldWeight: 600, Highlight: &HighlightStyle{Rule: gold, RuleW: 1, FG: rgbHex(0xf2efe8)}, LinkColor: rgbHex(0xe6e1d6), LinkDeco: rgbHex(0x6a665d)},
			Table:      TableStyle{Rules: true, HeadSep: 0.5, RowSep: 0.5, RuleColor: rgbHex(0x3a3732), StrongColor: rgbHex(0x5a564e), HeaderWt: 400, HeaderFG: rgbHex(0x8f8a7f), HeaderTrack: 0.12, Secondary: rgbHex(0x8f8a7f), PadX: 6},
			List:       ListStyle{Bullet: BulletSquare, BulletSize: 4, BulletColor: gold, NumberColor: gold, NumberFont: fontSerif, TodoBorder: rgbHex(0x5a564e), TodoCheck: gold},
			Quote:      QuoteStyle{Kind: QuoteBigMark, Accent: rgbHex(0x2a2824), Size: 22, Leading: 1.8, PadX: 36, PadY: 10},
			Code:       CodeStyle{Kind: CodePlain, FG: rgbHex(0x8f8a7f), Size: 13.5, Leading: 1.8},
			Divider:    DividerStyle{Kind: DividerText, Text: "—", Color: rgbHex(0x5a564e)},
			Footer:     FooterStyle{Center: "·  cooool.top  ·", Rule: FooterNone, Family: fontSerif, Size: 12, Track: 0.3, Color: rgbHex(0x5a564e)},
		},
	})
}

func kraftTheme() PaperThemeSpec {
	gold := rgbHex(0xc9a352)
	bright := rgbHex(0xd6b05c)
	muted := rgbHex(0xa89b80)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeKraft,
		Dark:        true,
		Canvas:      rgbHex(0x1d1b18),
		Text:        rgbHex(0xe8dfca),
		Title:       rgbHex(0xf2e9d2),
		Secondary:   muted,
		Accent:      bright,
		Accent2:     gold,
		CodeBG:      rgbaHex(0x000000, 0.28),
		CodeBorder:  rgbaHex(0xc9a352, 0.45),
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0xf2e9d2),
		QuoteBar:    gold,
		TableBorder: rgbaHex(0xc9a352, 0.35),
		Divider:     rgbaHex(0xc9a352, 0.5),
		Card:        &CardSpec{Kind: CardOutline, Inset: 28, Radius: 8, Border: gold, HasBorder: true},
		PadTop:      64, PadRight: 64, PadBottom: 40, PadLeft: 64,
		BodySize: 19, Leading: 1.95, TitleSize: 40,
		HeadSizes:   [3]float32{30, 26, 21},
		Mark:        TitleMarkRing,
		TitleFamily: fontKai, HeadingFamily: fontKai, BodyFamily: fontKai,
		Radius: 6,
		Style: PaperStyle{
			Substrate:  []SubstrateLayer{{Kind: SubstrateGrid, Color: rgbaHex(0xd6b05c, 0.08), Gap: 20, Size: 1}},
			Ornaments:  []Ornament{{Kind: OrnamentSeal, Text: "✦", Text2: "{date}", W: 92, X: 40, Y: 4, Angle: -12, Color: gold, Color2: rgbHex(0x1d1b18), Family: fontSans, Size: 22, BottomWhenNoTitle: true}},
			Title:      TitleStyle{Weight: 400, RuleColor: gold, Gap: 36},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarRing, Color: gold},
			Lead:       LeadStyle{Color: rgbHex(0xf2e9d2), Weight: 600},
			Heading:    HeadingStyle{Weight: 400, Color: bright, TopGap: 18},
			Inline:     InlineStyle{BoldColor: rgbHex(0xf2e9d2), Highlight: &HighlightStyle{BG: rgbaHex(0xd6b05c, 0.22)}, LinkColor: bright, LinkDotted: true},
			Table: TableStyle{Rules: true, HeadSep: 1, RowSep: 0.5, RuleColor: rgbaHex(0xc9a352, 0.35), StrongColor: gold,
				HeaderSize: 15, HeaderWt: 400, HeaderFG: muted, Size: 15},
			List:    ListStyle{Bullet: BulletRing, BulletSize: 9, BulletColor: gold, NumberColor: bright, NumberFont: fontKai, Todo: TodoRing, TodoBorder: gold, TodoCheck: bright, DoneFG: muted},
			Quote:   QuoteStyle{Kind: QuoteBar, BarW: 2, Size: 22, Leading: 1.75, Accent: gold, PadX: 22, PadY: 4},
			Code:    CodeStyle{BorderW: 0.5, Size: 13.5, Label: gold},
			Divider: DividerStyle{Kind: DividerText, Text: "◆", Color: gold},
			Footer:  FooterStyle{Center: "cooool.top · {date}", Rule: FooterFlank, RuleColor: gold, Family: fontKai, Size: 15, Color: muted},
		},
	})
}

func neonTheme() PaperThemeSpec {
	pink := rgbHex(0xff3d8b)
	cyan := rgbHex(0x7dd3fc)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeNeon,
		Dark:        true,
		Canvas:      rgbHex(0x0d0c14),
		InnerCanvas: rgbHex(0x14121d),
		Text:        rgbHex(0xbdb9d1),
		Title:       rgbHex(0xf4f1ff),
		Secondary:   rgbHex(0x8f8aa8),
		Accent:      pink,
		Accent2:     cyan,
		CodeBG:      rgbaHex(0x000000, 0.35),
		CodeBorder:  rgbaHex(0xffffff, 0.08),
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0xe4e0f5),
		QuoteBar:    pink,
		TableBorder: rgbaHex(0xffffff, 0.12),
		Divider:     rgbaHex(0xffffff, 0.12),
		Card: &CardSpec{Inset: 56, InsetTop: 64, Radius: 10, Fill: rgbaHex(0xffffff, 0.035), Border: rgbaHex(0xffffff, 0.1), HasBorder: true,
			TopEdge: rgbaHex(0xffffff, 0.22), BodyOnly: true},
		PadTop: 44, PadRight: 44, PadBottom: 36, PadLeft: 44,
		BodySize: 16, Leading: 1.9, TitleSize: 44,
		HeadSizes:   [3]float32{28, 22, 18},
		Mark:        TitleMarkNone,
		TitleFamily: fontSans, HeadingFamily: fontSans, BodyFamily: fontSans,
		TitleBold: true,
		Radius:    8,
		Style: PaperStyle{
			Substrate:  []SubstrateLayer{{Kind: SubstrateGlow, Color: rgbHex(0x3a1450), At: ui.Point{X: 0.82, Y: 0.12}, Radius: 0.42}},
			Title:      TitleStyle{Weight: 800, Leading: 1.25, Gap: 32, Accent: &TitleAccent{FG: pink, Glow: rgbaHex(0xff3d8b, 0.7)}},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarGradient, Color: pink, Color2: cyan},
			Lead:       LeadStyle{Color: rgbHex(0xf4f1ff), Weight: 700},
			Heading:    HeadingStyle{Prefix: HeadPrefixLabel, Weight: 700, PrefixColor: pink, PrefixFont: fontMono, RuleColor: pink, TopGap: 20},
			Inline: InlineStyle{BoldColor: rgbHex(0xf4f1ff), Highlight: &HighlightStyle{BG: rgbaHex(0x7dd3fc, 0.14), FG: rgbHex(0xe0f2fe)},
				LinkColor: cyan, LinkDeco: rgbaHex(0x7dd3fc, 0.5), LinkMono: true, CodeBG: rgbaHex(0x7dd3fc, 0.1), CodeFG: cyan},
			Table: TableStyle{Rules: true, HeadSep: 1, RowSep: 1, RowSepLine: LineDashed, RuleColor: rgbaHex(0xffffff, 0.1), StrongColor: rgbaHex(0xff3d8b, 0.6),
				HeaderFont: fontMono, HeaderSize: 11, HeaderUpper: true, HeaderTrack: 0.16, HeaderWt: 500, HeaderFG: rgbHex(0x8f8aa8)},
			List:    ListStyle{Bullet: BulletDash, BulletColor: cyan, NumberColor: cyan, NumberFont: fontMono, Number: NumberPadded, TodoBorder: rgbaHex(0x7dd3fc, 0.6), TodoCheck: cyan},
			Quote:   QuoteStyle{Kind: QuoteBar, BarW: 2, Size: 19, Accent: pink, Glow: rgbaHex(0xff3d8b, 0.8), PadX: 22, PadY: 4},
			Code:    CodeStyle{BorderW: 1, Size: 13.5, FG: rgbHex(0xd8d4ec), Label: pink},
			Divider: DividerStyle{Color: rgbaHex(0xff3d8b, 0.5)},
			Footer:  FooterStyle{Left: "— SIGNAL END —", Right: "cooool.top", Rule: FooterNone, Family: fontMono, Size: 11, Track: 0.24, Color: rgbHex(0x5d5874), RightColor: pink, Gap: 24},
		},
	})
}

func filmTheme() PaperThemeSpec {
	red := rgbHex(0xd9482f)
	frame := rgbHex(0x2b231d)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeFilm,
		Dark:        true,
		Canvas:      rgbHex(0x120f0d),
		Text:        rgbHex(0xcfc4b0),
		Title:       rgbHex(0xefe4cf),
		Secondary:   rgbHex(0x7d7264),
		Accent:      red,
		Accent2:     rgbHex(0xe8a25a),
		CodeBG:      rgbHex(0x0a0807),
		CodeBorder:  rgbHex(0x3a3029),
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0xefe4cf),
		QuoteBar:    red,
		TableBorder: rgbHex(0x3a3029),
		Divider:     rgbHex(0x3a3029),
		PadTop:      72, PadRight: 92, PadBottom: 72, PadLeft: 92,
		BodySize: 17, Leading: 1.95, TitleSize: 40,
		HeadSizes:   [3]float32{28, 22, 19},
		Mark:        TitleMarkRule,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSerif,
		Radius: 4,
		Style: PaperStyle{
			Ornaments: []Ornament{
				{Kind: OrnamentSprockets, X: 22, W: 22, H: 16, Y: 40, Color: frame},
				{Kind: OrnamentEdgeRule, X: 56, W: 1, Color: frame},
				{Kind: OrnamentEdgeRule, X: -56, W: 1, Color: frame},
			},
			Title:      TitleStyle{Weight: 600, Leading: 1.35, TopTick: red, RuleThick: 1, RuleColor: frame, Gap: 32},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarTickRule, Color: red, Color2: frame},
			Lead:       LeadStyle{Color: rgbHex(0xefe4cf), Weight: 600},
			Heading:    HeadingStyle{Prefix: HeadPrefixNumber, Weight: 600, PrefixColor: red, PrefixFont: fontMono, TopGap: 22},
			Inline: InlineStyle{BoldColor: rgbHex(0xefe4cf), Highlight: &HighlightStyle{BG: rgbaHex(0xd9482f, 0.2), FG: rgbHex(0xefe4cf)},
				LinkColor: rgbHex(0xefe4cf), LinkDeco: red, LinkOffset: 4, LinkThick: 1, CodeBG: rgbHex(0x0a0807), CodeFG: rgbHex(0xe8a25a)},
			Table: TableStyle{Rules: true, HeadSep: 1, RowSep: 0.5, RuleColor: rgbHex(0x3a3029), StrongColor: rgbHex(0x5a4b40),
				HeaderFont: fontMono, HeaderSize: 10, HeaderUpper: true, HeaderTrack: 0.16, HeaderWt: 500, HeaderFG: rgbHex(0x7d7264),
				FirstColor: rgbHex(0xefe4cf), Size: 14},
			List:    ListStyle{Bullet: BulletDash, BulletColor: rgbHex(0x7d7264), Number: NumberPadded, NumberFont: fontMono, NumberColor: red, TodoBorder: rgbHex(0x7d7264), TodoCheck: red},
			Quote:   QuoteStyle{Kind: QuoteFramed, Accent: red, Label: "SCENE {n}", Family: fontSerif, Italic: true, Size: 20, Leading: 1.75, PadX: 26, PadY: 22},
			Code:    CodeStyle{BorderW: 0.5, Size: 13.5, FG: rgbHex(0xe8a25a), Label: rgbHex(0x7d7264)},
			Divider: DividerStyle{Kind: DividerRepeat, Text: "▪ ", Color: frame},
			Footer:  FooterStyle{Left: "cooool.top FILM", Right: "{date}", Rule: FooterLine, RuleColor: frame, Family: fontMono, Size: 11, Track: 0.2, Color: rgbHex(0x7d7264), RightColor: red},
		},
	})
}

func nightTheme() PaperThemeSpec {
	amber := rgbHex(0xe8a642)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeNight,
		Dark:        true,
		Canvas:      rgbHex(0x211915),
		Text:        rgbHex(0xdccfb8),
		Title:       rgbHex(0xf1e6d2),
		Secondary:   rgbHex(0xa8977c),
		Accent:      amber,
		Accent2:     rgbHex(0xb88a3e),
		CodeBG:      rgbHex(0x221a15),
		CodeBorder:  rgbHex(0x3a2e26),
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0xefe2c9),
		QuoteBar:    amber,
		TableBorder: rgbHex(0x4a3b31),
		Divider:     rgbHex(0x4a3b31),
		Card: &CardSpec{Inset: 56, Radius: 20, Fill: rgbHex(0x2c221c), TopEdge: rgbaHex(0xffebc8, 0.05),
			Shadow: rgbaHex(0x0a0603, 0.5), ShadowY: 20, Blur: 40},
		PadTop: 60, PadRight: 60, PadBottom: 44, PadLeft: 60,
		BodySize: 17, Leading: 1.95, TitleSize: 38,
		HeadSizes:   [3]float32{28, 22, 19},
		Mark:        TitleMarkNone,
		TitleFamily: fontSerif, HeadingFamily: fontSerif, BodyFamily: fontSans,
		Radius: 10,
		Style: PaperStyle{
			Title:      TitleStyle{Weight: 600, Leading: 1.35, Moon: true, Gap: 32},
			NoTitleBar: NoTitleBarStyle{Kind: NoTitleBarMoonRule, Color: amber, Color2: rgbaHex(0xe8a642, 0.24)},
			Lead:       LeadStyle{Color: rgbHex(0xf1e6d2), Weight: 600},
			Heading:    HeadingStyle{Weight: 600, TopGap: 20},
			Inline:     InlineStyle{BoldColor: rgbHex(0xf1e6d2), Highlight: &HighlightStyle{BG: rgbaHex(0xe8a642, 0.24), FG: rgbHex(0xf6e3bf)}, LinkColor: amber, LinkDeco: rgbaHex(0xe8a642, 0.5)},
			Table:      TableStyle{Rules: true, HeadSep: 1, RowSep: 0.5, RuleColor: rgbHex(0x4a3b31), StrongColor: rgbHex(0x6a5545), HeaderWt: 600, HeaderFG: rgbHex(0xa8977c), HeaderSize: 13},
			List:       ListStyle{BulletSize: 6, BulletColor: rgbHex(0xb88a3e), NumberColor: amber, TodoBorder: rgbHex(0xb88a3e), TodoCheck: amber},
			Quote:      QuoteStyle{Kind: QuoteBar, BarW: 2, Family: fontSerif, Italic: true, Size: 20, Accent: amber, PadX: 22, PadY: 4},
			Code:       CodeStyle{BorderW: -1, Size: 13.5, FG: rgbHex(0xe8d8bc)},
			Divider:    DividerStyle{Kind: DividerText, Text: "☾", Color: rgbHex(0x8a7a62)},
			Footer:     FooterStyle{Center: "cooool.top · {date}", Rule: FooterNone, Family: fontSerif, Size: 14, Color: rgbHex(0x8a7a62)},
		},
	})
}

func terminalTheme() PaperThemeSpec {
	green := rgbHex(0x7ee787)
	bright := rgbHex(0xb5f5bb)
	dim := rgbHex(0x3f8a47)
	return finishSpec(PaperThemeSpec{
		Theme:       model.ExportThemeTerminal,
		Dark:        true,
		Canvas:      rgbHex(0x071008),
		Text:        green,
		Title:       bright,
		Secondary:   dim,
		Accent:      green,
		Accent2:     bright,
		CodeBG:      rgbHex(0x0b1a0d),
		CodeBorder:  dim,
		QuoteBG:     ui.Color{},
		QuoteFG:     rgbHex(0x9fe7a6),
		QuoteBar:    dim,
		TableBorder: dim,
		Divider:     dim,
		Card:        &CardSpec{Kind: CardOutline, Inset: 48, InsetTop: 52, Radius: 6, Border: dim, HasBorder: true, Label: "┤ ● ● ● ├", LabelColor: dim},
		PadTop:      28, PadRight: 32, PadBottom: 28, PadLeft: 32,
		BodySize: 15, Leading: 1.8, TitleSize: 30,
		HeadSizes:   [3]float32{22, 18, 16},
		Mark:        TitleMarkCursor,
		TitleFamily: fontMonoCJK, HeadingFamily: fontMonoCJK, BodyFamily: fontMonoCJK,
		TitleBold: true,
		Radius:    0,
		Style: PaperStyle{
			Substrate: []SubstrateLayer{
				{Kind: SubstrateScanlines, Color: rgbaHex(0x7ee787, 0.06), Gap: 3, Size: 1},
				{Kind: SubstrateVignette, Color: rgbaHex(0x000000, 0.55)},
			},
			Title:   TitleStyle{Prefix: "# ", Weight: 700, Leading: 1.35, Glow: rgbaHex(0xb5f5bb, 0.45), Gap: 24},
			Lead:    LeadStyle{Color: bright, Weight: 700},
			Heading: HeadingStyle{Prefix: HeadPrefixHashes, Weight: 700, PrefixColor: dim, TopGap: 14},
			Inline: InlineStyle{BoldColor: bright, Highlight: &HighlightStyle{BG: rgbHex(0x1d4a24), FG: rgbHex(0xd4fcd7)},
				LinkColor: bright, LinkDeco: dim, CodeBG: rgbHex(0x0f2a13), CodeFG: bright},
			Table:    TableStyle{RuleColor: dim, HeaderBG: rgbHex(0x0d2410), HeaderFG: bright, PadX: 12, PadY: 4, Size: 14, Leading: 1.6},
			List:     ListStyle{Bullet: BulletDash, BulletColor: dim, NumberColor: dim, Todo: TodoText, TodoBorder: dim, TodoCheck: bright, DoneFG: dim},
			Quote:    QuoteStyle{Kind: QuotePrefix, Accent: dim},
			Code:     CodeStyle{Kind: CodeFenced, Size: 14, Leading: 1.7, FG: rgbHex(0xa6f0ad), Label: dim},
			Divider:  DividerStyle{Kind: DividerRepeat, Text: "─", Color: dim},
			Footer:   FooterStyle{Left: "-- cooool.top --", Right: "[utf-8]", Rule: FooterNone, Family: fontMonoCJK, Size: 12, Color: dim, Gap: 24},
			TextGlow: rgbaHex(0x7ee787, 0.35),
		},
	})
}
