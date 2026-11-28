package markdown

import "github.com/egoist/mygo/ui"

// PaperStyle is the per-theme design vocabulary beyond colours: page substrate,
// ornaments, header/footer furniture and the treatment of every block kind.
// Each field's zero value is the plain look the legacy Ocean export uses, so a
// spec without a Style renders as before.
type PaperStyle struct {
	Substrate  []SubstrateLayer
	Ornaments  []Ornament
	Meta       MetaStyle
	Title      TitleStyle
	NoTitleBar NoTitleBarStyle
	Lead       LeadStyle
	Heading    HeadingStyle
	Sub       SubHeadStyle
	Inline    InlineStyle
	Quote     QuoteStyle
	Callout   CalloutStyle
	Code      CodeStyle
	Table     TableStyle
	List      ListStyle
	Divider   DividerStyle
	Image     ImageStyle
	Footer    FooterStyle
	// Sections wraps every H2-delimited run of blocks in its own card (Mist).
	Sections *SectionCards
	// Justify sets body paragraphs flush on both edges.
	Justify bool
	// TextGlow, when opaque, gives all text a soft phosphor glow.
	TextGlow ui.Color
}

// SubstrateKind is a texture painted over the canvas colour.
type SubstrateKind uint8

const (
	SubstrateGrid      SubstrateKind = iota + 1 // Size-wide lines every Gap
	SubstrateDots                               // Size-radius dots every Gap
	SubstrateGrain                              // scattered Size-radius specks, Gap apart on average
	SubstrateScanlines                          // Size-tall horizontal lines every Gap
	SubstrateGlow                               // radial glow of Color at At, reaching Radius × page width
	SubstrateGradient                           // linear Color → Color2 → Color3 at Angle
	SubstrateVignette                           // darkens the page edges towards Color
)

type SubstrateLayer struct {
	Kind                  SubstrateKind
	Color, Color2, Color3 ui.Color
	Gap, Size             float32
	At                    ui.Point
	Radius                float32
	Angle                 float32
}

// OrnamentKind is page furniture that does not flow with the content.
type OrnamentKind uint8

const (
	OrnamentEdgeRule  OrnamentKind = iota + 1 // full-height hairline at X (negative: from the right)
	OrnamentSprockets                         // film perforations, W wide, X from both edges
	OrnamentSideText                          // Text set vertically, X from the right edge, from Y down
	OrnamentStamp                             // Text tilted by Angle, right edge X from the page's, at Y
	OrnamentSeal                              // ring of diameter W holding Text over Text2, tilted by Angle
	OrnamentTape                              // washi strip W×H centred X from the page centre, at Y
	OrnamentRegMarks                          // corner print registration crosshairs (+) at top-left and top-right
)

type Ornament struct {
	Kind              OrnamentKind
	Color, Color2     ui.Color
	X, Y, W, H        float32
	Angle             float32
	Text, Text2       string // templates, see expandTemplate
	Family            string
	Size              float32
	Italic            bool
	Track             float32 // em
	BottomWhenNoTitle bool    // relocate to bottom-right when showTitle is false instead of hiding
}

// MetaRule frames the meta line.
type MetaRule uint8

const (
	MetaRuleNone   MetaRule = iota
	MetaRuleSingle          // hairline below
	MetaRuleDouble          // 3px + 1px newspaper rules below
)

// MetaStyle is the small line set above the title (follows the show-title switch).
type MetaStyle struct {
	Left, Right string // templates; all empty = no meta line
	// Masthead is a large centred nameplate between Left and Right (Press).
	Masthead       string
	MastheadFamily string
	MastheadSize   float32
	MastheadTrack  float32
	Family         string
	Size           float32
	Track          float32 // em
	Upper          bool
	Italic         bool
	Color          ui.Color
	RightColor     ui.Color
	Gap            float32 // space between meta and title
	Moon           bool    // a crescent before Left
	Rule           MetaRule
	RuleColor      ui.Color
}

// TitleMark describes how the document title is separated from the body.
type TitleMark uint8

const (
	TitleMarkRule TitleMark = iota
	TitleMarkBar
	TitleMarkWavy
	TitleMarkHighlight
	TitleMarkDoubleRule
	TitleMarkBand
	TitleMarkCursor
	TitleMarkNone
	TitleMarkOffset // a flat ink block offset behind the title (Riso)
	TitleMarkRing   // a ring followed by a rule (Kraft)
)

type TitleStyle struct {
	Weight   int     // 0 = TitleBold decides
	Track    float32 // em
	Leading  float32 // 0 = 1.3
	Center   bool
	Prefix   string // e.g. "# "
	TopRule  bool   // hairline above the title (Press)
	TopTick  ui.Color
	Moon     bool // crescent icon aligned before the title (Night)
	MaxLines int  // shrink the title until it fits; 0 = no limit
	MinSize  float32
	// RuleWidth is the rule / wave length as a fraction of the measure (0 = full).
	RuleWidth float32
	RuleThick float32
	RuleColor ui.Color // 0 = Accent
	Gap       float32  // space between the title mark and the body; 0 = per-mark default
	Glow      ui.Color
	// Accent styles the title's last word (Poster block, Neon glowing word).
	Accent *TitleAccent
}

// NoTitleBarKind draws a restrained geometric header skeleton when ShowTitle is false.
type NoTitleBarKind uint8

const (
	NoTitleBarNone       NoTitleBarKind = iota
	NoTitleBarDiamond                   // hairline — ◆ — hairline (Mono, Ink)
	NoTitleBarOverprint                 // offset two-ink bars (Riso)
	NoTitleBarDoubleRule                // 3px + 1px newspaper double rule (Press)
	NoTitleBarSplitBar                  // bold 4:1 two-tone bar (Poster)
	NoTitleBarRing                      // small ring + hairline (Kraft)
	NoTitleBarGradient                  // subtle gradient edge along the card top (Neon)
	NoTitleBarTickRule                  // short accent tick + hairline (Film)
	NoTitleBarMoonRule                  // crescent icon + subtle hairline (Night)
)

type NoTitleBarStyle struct {
	Kind     NoTitleBarKind
	Color    ui.Color
	Color2   ui.Color
	Centered bool
}

// LeadStyle styles a short single-line paragraph immediately preceding a list.
type LeadStyle struct {
	Color  ui.Color
	Family string
	Weight int
}

type TitleAccent struct {
	FG, BG ui.Color
	Glow   ui.Color
}

// HeadPrefix decorates H2 headings.
type HeadPrefix uint8

const (
	HeadPrefixNone   HeadPrefix = iota
	HeadPrefixDot               // round dot alternating Accent / Accent2
	HeadPrefixTag               // ordinal in a filled tag, a rule fills the rest of the line
	HeadPrefixLabel             // "01 —" label line above with a fading rule
	HeadPrefixNumber            // small ordinal before the heading text
	HeadPrefixHashes            // markdown hashes, "## "
)

type HeadingStyle struct {
	Prefix      HeadPrefix
	Weight      int // 0 = 700
	Track       float32
	Color       ui.Color // 0 = Title
	Accent      ui.Color // 0 = spec Accent
	Accent2     ui.Color
	TagFG       ui.Color
	Underline   bool // hairline under H2
	RuleColor   ui.Color
	PrefixColor ui.Color
	PrefixFont  string
	TopGap      float32 // space above H2; 0 = 12
	Glow        ui.Color
}

// HighlightStyle restyles the default (yellow) ==highlight== into the theme's marker.
type HighlightStyle struct {
	BG     ui.Color
	FG     ui.Color
	Rule   ui.Color // underline drawn under the highlight
	RuleW  float32
	Weight int
}

type InlineStyle struct {
	BoldColor  ui.Color
	BoldWeight int
	BoldFamily string
	LinkColor  ui.Color // 0 = LinkFG()
	LinkDeco   ui.Color // underline colour; 0 = link colour
	LinkThick  float32
	LinkOffset float32
	LinkWavy   bool
	LinkDotted bool
	LinkMono   bool
	Highlight  *HighlightStyle
	CodeBG     ui.Color // 0 = CodeBG
	CodeFG     ui.Color
	// CodeRadius rounds inline code; negative = square corners, 0 = 4.
	CodeRadius float32
	// StrikeColor / UnderColor recolour ~~strike~~ and <u>underline</u> decorations.
	StrikeColor ui.Color
	UnderColor  ui.Color
	UnderThick  float32
}

// SubMark decorates H3–H6 headings.
type SubMark uint8

const (
	SubMarkNone      SubMark = iota
	SubMarkDashUnder         // dashed rule under the text (Journal)
	SubMarkBand              // marker band behind the lower half of the text (Riso)
	SubMarkSquare            // small filled square before the text (Poster)
)

// SubHeadStyle is the treatment of H3 and below; zero keeps the H2 look at a smaller size.
type SubHeadStyle struct {
	Prefix      string // literal text set before the heading, e.g. "§ ", "▍", "> "
	PrefixColor ui.Color
	PrefixFont  string
	Family      string
	Color       ui.Color
	Size        float32 // 0 = HeadSizes[2]
	Weight      int
	Track       float32
	Mark        SubMark
	MarkColor   ui.Color
	TopGap      float32
}

// CalloutLabelPos places the callout's label.
type CalloutLabelPos uint8

const (
	CalloutLabelAbove  CalloutLabelPos = iota // label on its own line above the body
	CalloutLabelColumn                        // label in a fixed-width column left of the body
	CalloutLabelInline                        // label runs into the start of the body text
)

// CalloutTone overrides one callout kind's colours; zero fields fall back to CalloutColors.
type CalloutTone struct {
	BG, Border, Label, LabelBG, FG ui.Color
	TopRule                        ui.Color
	TopRuleW                       float32
	Dashed                         bool
	Bold                           bool // body text set in the bold weight
}

// CalloutStyle is the treatment of > [!NOTE] … > [!CAUTION] callouts. Tones are
// indexed NOTE, TIP, IMPORTANT, WARNING, CAUTION, so each theme can escalate the
// five kinds from a quiet note to an inverted warning.
type CalloutStyle struct {
	Labels      [5]string // shown label per kind; "" = the English kind name
	LabelPos    CalloutLabelPos
	LabelW      float32 // column width; 0 = 92
	LabelFamily string
	LabelSize   float32 // 0 = 13
	LabelTrack  float32
	LabelWeight int // 0 = 700
	Radius      float32
	BorderW     float32 // 0 = 1 when a border colour is set
	Dashed      bool
	Bar         float32  // left accent bar width (0 = 4 for the default look, negative = none)
	Rule        ui.Color // hairline under each untinted, unbordered callout (row lists)
	Angle       float32  // alternating tilt (sticky notes)
	Shadow      ui.Color
	PadX, PadY  float32
	Gap         float32 // space after each callout; 0 = 14
	Tones       [5]CalloutTone
	Custom      bool // use Tones and the fields above instead of the default tinted box
}

// ImageFrame is how a figure is mounted.
type ImageFrame uint8

const (
	ImageFrameNone     ImageFrame = iota
	ImageFrameBorder              // hairline border flush with the picture
	ImageFrameMat                 // picture inset Pad inside a filled, bordered mat
	ImageFramePolaroid            // white print with a deep bottom margin holding the caption
)

// ImageStyle is the treatment of images and their alt-text captions.
type ImageStyle struct {
	Frame         ImageFrame
	Pad           float32
	BG            ui.Color
	Border        ui.Color
	BorderW       float32
	Radius        float32 // 0 = theme Radius
	InnerRadius   float32 // picture corner radius inside a mat
	Offset        float32 // misregistered outline offset (Riso)
	Corners       bool    // photo-album corner brackets (Kraft)
	Shadow        ui.Color
	Angle         float32
	Tape          ui.Color // washi strip across the top edge
	Caption       bool     // show alt text as a caption
	CaptionPrefix string   // e.g. "图 · ", "FIG.01 " ("{n}" = two-digit ordinal)
	CaptionFamily string
	CaptionSize   float32 // 0 = 12
	CaptionItalic bool
	CaptionTrack  float32
	CaptionColor  ui.Color // 0 = Secondary
	CaptionCenter bool
	CaptionSide   float32 // reserved caption width; 0 places it below the picture
}

type QuoteKind uint8

const (
	QuoteBar     QuoteKind = iota // filled box (QuoteBG) with a left bar
	QuoteIndent                   // indented text, no ornament
	QuoteSticky                   // tilted sticky note pinned to the right
	QuoteOffset                   // filled box with a misregistered outline
	QuotePull                     // centred pull quote between rules with corner brackets
	QuoteBigMark                  // oversized quotation mark behind the text
	QuoteFramed                   // outlined box with a label on its top border
	QuotePrefix                   // "> " before the text
)

type QuoteStyle struct {
	Kind    QuoteKind
	BarW    float32 // 0 = 4
	Italic  bool
	Size    float32 // 0 = body size
	Leading float32 // 0 = body leading
	Family  string  // 0 = body family
	Weight  int
	Track   float32
	Accent  ui.Color // bar, brackets, big mark, outline; 0 = QuoteBar
	Glow    ui.Color
	Indent  float32
	PadX    float32
	PadY    float32
	Width   float32 // sticky note width
	Angle   float32 // sticky note tilt
	Label   string  // framed label template; "{n}" = two-digit ordinal
	Card    bool    // big-mark quote drawn on QuoteBG
}

type CodeKind uint8

const (
	CodeBox    CodeKind = iota // filled, outlined box with language label
	CodePlain                  // bare monospace text
	CodeFenced                 // left rule with ```lang fences
)

type CodeStyle struct {
	Kind    CodeKind
	BorderW float32 // 0 = 1; negative = none
	Dashed  bool
	Size    float32 // 0 = 14.5
	Leading float32 // 0 = 1.6
	PadX    float32 // 0 = 18
	PadY    float32 // 0 = 14
	FG      ui.Color
	Label   ui.Color // 0 = Secondary
	NoLabel bool
}

type LineStyle uint8

const (
	LineSolid LineStyle = iota
	LineDashed
	LineDotted
)

type TableStyle struct {
	// Rules switches from a full cell grid (zero) to typographic rules.
	Rules       bool
	Outer       float32 // outline width in rules mode
	OuterRadius float32
	Top         float32
	HeadSep     float32
	HeadSepLine LineStyle
	RowSep      float32
	RowSepLine  LineStyle
	ColSep      float32
	ColSepLine  LineStyle
	Bottom      float32
	RuleColor   ui.Color // 0 = TableBorder
	StrongColor ui.Color // top / header / bottom rules; 0 = RuleColor
	HeaderBG    ui.Color // rules mode: 0 = none; grid: 0 = TableHeaderBG
	HeaderFG    ui.Color
	HeaderSize  float32
	HeaderUpper bool
	HeaderTrack float32
	HeaderFont  string
	HeaderWt    int
	PadX, PadY  float32 // 0 = 10 / 7
	Size        float32 // 0 = 14
	Leading     float32 // 0 = 1.45
	FirstColor  ui.Color // first column text colour
	Secondary   ui.Color // colour for columns after the first
}

type BulletKind uint8

const (
	BulletDot BulletKind = iota
	BulletSquare
	BulletRing
	BulletDash
	BulletArrow
	BulletMiddot
	BulletDiamond // a square turned 45°
)

type NumberKind uint8

const (
	NumberArabic NumberKind = iota // "1."
	NumberPadded                   // "01"
	NumberCJK                      // "一、"
	NumberRoman                    // "i."
	NumberBare                     // "1"
)

// NumberShape draws a badge behind the ordinal.
type NumberShape uint8

const (
	NumberShapeNone   NumberShape = iota
	NumberShapeCircle             // ring (NumberStroke) and/or disc (NumberFill)
	NumberShapeSquare             // filled square tag
)

type TodoKind uint8

const (
	TodoBox TodoKind = iota
	TodoRing
	TodoText // "[ ]" / "[x]"
)

type ListStyle struct {
	Bullet      BulletKind
	BulletSize  float32
	BulletColor ui.Color // 0 = Accent
	// Bullet2 marks nested (indented) bullet items; Bullet2Glyph, when set, is drawn as text instead.
	Bullet2      BulletKind
	Bullet2Size  float32
	Bullet2Color ui.Color // 0 = Secondary
	Bullet2Glyph string
	Number       NumberKind
	NumberColor  ui.Color // 0 = Title
	NumberFont   string
	NumberShape  NumberShape
	NumberFill   ui.Color
	NumberStroke ui.Color
	NumberSize   float32 // ordinal text size; 0 = body size (badges: 12)
	Gutter       float32 // 0 = 28
	Gap          float32 // space after each item; 0 = 4
	Todo         TodoKind
	TodoBorder   ui.Color // 0 = Accent
	TodoCheck    ui.Color // 0 = Accent
	TodoFill     bool     // a checked box is filled with TodoCheck and the tick drawn in the canvas colour
	TodoRadius   float32  // 0 = 3; ≥ 7 draws a circle
	DoneFG       ui.Color // checked item text colour; 0 = Text
	DoneStrike   bool
}

type DividerKind uint8

const (
	DividerLine   DividerKind = iota
	DividerText               // Text centred
	DividerRepeat             // Text repeated across the measure
	DividerWave               // hand-drawn wave line
)

type DividerStyle struct {
	Kind     DividerKind
	Width    float32 // line width; 0 = 1
	Text     string
	Track    float32
	Color    ui.Color // 0 = Divider
	EndColor ui.Color
	EndWidth float32
}

type FooterRule uint8

const (
	FooterLine FooterRule = iota
	FooterNone
	FooterDashed
	FooterFlank // dotted rules either side of the centred text
)

type FooterStyle struct {
	Left, Center, Right string // templates; all empty = "cooool.top" / date
	Rule                FooterRule
	RuleColor           ui.Color // 0 = Divider
	Family              string
	Size                float32 // 0 = 14
	Track               float32 // em
	Upper               bool
	Italic              bool
	Color               ui.Color // 0 = Secondary
	RightColor          ui.Color
	Swatches            []ui.Color // small ink chips after the left text
	Gap                 float32    // space above the footer; 0 = 28
}

type SectionCards struct {
	Fill, Border, Shadow ui.Color
	Radius, PadX, PadY   float32
	Gap                  float32
}

func orF(v, def float32) float32 {
	if v == 0 {
		return def
	}
	return v
}

func orI(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func orC(c, def ui.Color) ui.Color {
	if c == (ui.Color{}) {
		return def
	}
	return c
}

func orS(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
