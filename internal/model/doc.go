package model

import (
	"strings"
	"sync/atomic"
)

type BlockID uint64

var nextBlockID atomic.Uint64

func NewBlockID() BlockID {
	return BlockID(nextBlockID.Add(1))
}

type BlockKind uint8

const (
	BlockParagraph BlockKind = iota
	BlockHeading1
	BlockHeading2
	BlockHeading3
	BlockHeading4
	BlockHeading5
	BlockHeading6
	BlockBullet
	BlockNumbered
	BlockTodo
	BlockQuote
	BlockCallout
	BlockCode
	BlockDivider
	BlockImage
	BlockTable
	BlockRawHTML
)

func (k BlockKind) IsHeading() bool {
	return k >= BlockHeading1 && k <= BlockHeading6
}

func (k BlockKind) HeadingLevel() int {
	if !k.IsHeading() {
		return 0
	}
	return int(k-BlockHeading1) + 1
}

func HeadingKind(level int) BlockKind {
	switch {
	case level <= 1:
		return BlockHeading1
	case level == 2:
		return BlockHeading2
	case level == 3:
		return BlockHeading3
	case level == 4:
		return BlockHeading4
	case level == 5:
		return BlockHeading5
	default:
		return BlockHeading6
	}
}

func (k BlockKind) IsList() bool {
	return k == BlockBullet || k == BlockNumbered || k == BlockTodo
}

// NumberedListMarkers computes the displayed list numbers in document order.
type NumberedListMarkers struct {
	runLen [13]int
	base   [13]int
}

// Next returns the block's visible number, or zero for a non-numbered block.
func (n *NumberedListMarkers) Next(b Block) int {
	if b.Kind == BlockParagraph && b.Indent == 0 && b.QuoteDepth == 0 && len(b.Spans) == 0 {
		return 0
	}
	ind := min(max(b.Indent, 0), len(n.runLen)-1)
	clear(n.runLen[ind+1:])
	clear(n.base[ind+1:])
	if b.Kind != BlockNumbered {
		n.runLen[ind], n.base[ind] = 0, 0
		return 0
	}

	n.runLen[ind]++
	num := n.runLen[ind]
	if n.base[ind] > 1 {
		num += n.base[ind] - 1
	} else if b.ListIndex > 1 {
		num = b.ListIndex
	}
	if b.ListIndex > 1 {
		n.runLen[ind], n.base[ind] = 1, b.ListIndex
	}
	return num
}

type CalloutKind string

const (
	CalloutNote      CalloutKind = "NOTE"
	CalloutTip       CalloutKind = "TIP"
	CalloutImportant CalloutKind = "IMPORTANT"
	CalloutWarning   CalloutKind = "WARNING"
	CalloutCaution   CalloutKind = "CAUTION"
)

type ImageAlign string

const (
	ImageAlignLeft   ImageAlign = "left"
	ImageAlignCenter ImageAlign = "center"
	ImageAlignRight  ImageAlign = "right"
)

type ColAlign string

const (
	ColAlignNone   ColAlign = ""
	ColAlignLeft   ColAlign = "left"
	ColAlignCenter ColAlign = "center"
	ColAlignRight  ColAlign = "right"
)

type TableTint string

const (
	TableTintNone    TableTint = ""
	TableTintSlate   TableTint = "slate"
	TableTintOcean   TableTint = "ocean"
	TableTintEmerald TableTint = "emerald"
	TableTintAmber   TableTint = "amber"
	TableTintRose    TableTint = "rose"
)

type ImageCrop struct {
	Top    float32 `json:"top,omitempty"`
	Right  float32 `json:"right,omitempty"`
	Bottom float32 `json:"bottom,omitempty"`
	Left   float32 `json:"left,omitempty"`
}

func (c ImageCrop) IsZero() bool {
	return c.Top == 0 && c.Right == 0 && c.Bottom == 0 && c.Left == 0
}

type Mark uint16

const (
	MarkBold Mark = 1 << iota
	MarkItalic
	MarkUnderline
	MarkStrike
	MarkCode
	MarkSuperscript
	MarkSubscript
	MarkMath
)

type Span struct {
	Text          string `json:"text"`
	Marks         Mark   `json:"marks,omitempty"`
	Link          string `json:"link,omitempty"`
	LinkTitle     string `json:"linkTitle,omitempty"`
	ImageSrc      string `json:"imageSrc,omitempty"`
	ImageTitle    string `json:"imageTitle,omitempty"`
	ImageAltEmpty bool   `json:"imageAltEmpty,omitempty"`
	FGColor       string `json:"fgColor,omitempty"`
	BGColor       string `json:"bgColor,omitempty"`
}

func PlainTextOfSpans(spans []Span) string {
	if len(spans) == 0 {
		return ""
	}
	if len(spans) == 1 {
		return spans[0].Text
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

type TableCell struct {
	Spans  []Span    `json:"spans"`
	Align  ColAlign  `json:"align,omitempty"`
	Tint   TableTint `json:"tint,omitempty"`
	Header bool      `json:"header,omitempty"`
}

func (c TableCell) PlainText() string {
	return PlainTextOfSpans(c.Spans)
}

type TableData struct {
	Aligns      []ColAlign    `json:"aligns,omitempty"`
	Widths      []float32     `json:"widths,omitempty"`
	NoHeaderRow bool          `json:"noHeaderRow,omitempty"`
	HeaderCol   bool          `json:"headerCol,omitempty"`
	Rows        [][]TableCell `json:"rows"` // Row 0 is the header row unless NoHeaderRow is true
	WasHTML     bool          `json:"wasHtml,omitempty"`
}

func (t *TableData) IsHeaderCell(row, col int) bool {
	if t == nil {
		return false
	}
	if row >= 0 && row < len(t.Rows) && col >= 0 && col < len(t.Rows[row]) && t.Rows[row][col].Header {
		return true
	}
	return (row == 0 && !t.NoHeaderRow) || (col == 0 && t.HeaderCol)
}

func (t *TableData) ColBaseAlign(col int) ColAlign {
	if t != nil && col >= 0 && col < len(t.Aligns) && t.Aligns[col] != ColAlignNone {
		return t.Aligns[col]
	}
	return ColAlignLeft
}

func (t *TableData) CellAlign(row, col int) ColAlign {
	if t == nil {
		return ColAlignNone
	}
	if row >= 0 && row < len(t.Rows) && col >= 0 && col < len(t.Rows[row]) {
		if a := t.Rows[row][col].Align; a != ColAlignNone {
			return a
		}
	}
	if col >= 0 && col < len(t.Aligns) {
		return t.Aligns[col]
	}
	return ColAlignNone
}

func (t *TableData) CellEffectiveAlign(row, col int) ColAlign {
	if a := t.CellAlign(row, col); a != ColAlignNone {
		return a
	}
	return ColAlignLeft
}

func (t *TableData) CellTint(row, col int) TableTint {
	if t != nil && row >= 0 && row < len(t.Rows) && col >= 0 && col < len(t.Rows[row]) {
		return t.Rows[row][col].Tint
	}
	return TableTintNone
}

func (t *TableData) Clone() *TableData {
	if t == nil {
		return nil
	}
	out := &TableData{
		WasHTML:     t.WasHTML,
		NoHeaderRow: t.NoHeaderRow,
		HeaderCol:   t.HeaderCol,
		Aligns:      append([]ColAlign(nil), t.Aligns...),
		Widths:      append([]float32(nil), t.Widths...),
		Rows:        make([][]TableCell, len(t.Rows)),
	}
	for r, row := range t.Rows {
		out.Rows[r] = make([]TableCell, len(row))
		for c, cell := range row {
			out.Rows[r][c] = TableCell{
				Spans: append([]Span(nil), cell.Spans...),
				Align: cell.Align,
				Tint:  cell.Tint,
			}
		}
	}
	return out
}

type Block struct {
	ID               BlockID     `json:"id"`
	Kind             BlockKind   `json:"kind"`
	Indent           int         `json:"indent,omitempty"`
	QuoteDepth       int         `json:"quoteDepth,omitempty"`
	ListIndex        int         `json:"listIndex,omitempty"`
	Checked          bool        `json:"checked,omitempty"`
	Callout          CalloutKind `json:"callout,omitempty"`
	DecoratorPrefix  string      `json:"decoratorPrefix,omitempty"`
	DecoratorKind    string      `json:"decoratorKind,omitempty"`
	Language         string      `json:"language,omitempty"`
	MindMapLayout    string      `json:"-"` // Transient live layout ("right"|"both") synced from editor state
	ShowSource       bool        `json:"showSource,omitempty"`
	WrapCode         bool        `json:"wrapCode,omitempty"`
	Spans            []Span      `json:"spans,omitempty"`
	Code             string      `json:"code,omitempty"` // Used by BlockCode and BlockRawHTML
	ImageSrc         string      `json:"imageSrc,omitempty"`
	ImageAlt         string      `json:"imageAlt,omitempty"`
	ImageTitle       string      `json:"imageTitle,omitempty"`
	ImageWidth       float32     `json:"imageWidth,omitempty"` // Percentage 10..100 or 0 for auto
	ImageAlign       ImageAlign  `json:"imageAlign,omitempty"`
	ImageCrop        ImageCrop   `json:"imageCrop,omitempty"`
	Table            *TableData  `json:"table,omitempty"`
	CleanSource      string      `json:"-"` // Preserved verbatim when block is untouched
	Dirty            bool        `json:"-"`
	SourceLine       int         `json:"-"` // 1-based start line in source Markdown
	SourceEndLine    int         `json:"-"` // 1-based inclusive end line in source Markdown
	QuoteNewGroup    bool        `json:"-"` // Starts a top-level blockquote distinct from any preceding quote block
	QuoteTightBefore bool        `json:"-"` // Immediately follows a shallower blockquote line with no blank ">" separator
}

func (b *Block) PlainText() string {
	switch b.Kind {
	case BlockCode, BlockRawHTML:
		return b.Code
	case BlockTable:
		if b.Table == nil {
			return ""
		}
		var sb strings.Builder
		for r, row := range b.Table.Rows {
			if r > 0 {
				sb.WriteByte('\n')
			}
			for c, cell := range row {
				if c > 0 {
					sb.WriteByte('\t')
				}
				sb.WriteString(cell.PlainText())
			}
		}
		return sb.String()
	case BlockImage:
		return b.ImageAlt
	case BlockDivider:
		return "---"
	default:
		return PlainTextOfSpans(b.Spans)
	}
}

func (b *Block) SetText(text string) {
	b.Dirty = true
	b.CleanSource = ""
	if b.Kind == BlockCode || b.Kind == BlockRawHTML {
		b.Code = text
		return
	}
	if text == "" {
		b.Spans = nil
		return
	}
	b.Spans = []Span{{Text: text}}
}

func (b *Block) Clone() Block {
	cp := *b
	if len(b.Spans) > 0 {
		cp.Spans = make([]Span, len(b.Spans))
		copy(cp.Spans, b.Spans)
	}
	if b.Table != nil {
		cp.Table = b.Table.Clone()
	}
	return cp
}

type Doc struct {
	Blocks       []Block `json:"blocks"`
	CleanRaw     string  `json:"-"` // Full original file if completely untouched
	TrailingLine string  `json:"-"` // Original trailing newline style ("\n", "\r\n", or "")
}

func NewEmptyDoc() *Doc {
	return &Doc{
		Blocks:       []Block{{ID: NewBlockID(), Kind: BlockParagraph}},
		TrailingLine: "\n",
	}
}

func (d *Doc) Clone() *Doc {
	if d == nil {
		return NewEmptyDoc()
	}
	out := &Doc{
		CleanRaw:     d.CleanRaw,
		TrailingLine: d.TrailingLine,
		Blocks:       make([]Block, len(d.Blocks)),
	}
	for i := range d.Blocks {
		out.Blocks[i] = d.Blocks[i].Clone()
	}
	return out
}

func (d *Doc) Title() string {
	if d == nil {
		return ""
	}
	for i, b := range d.Blocks {
		if i == 0 && b.Kind == BlockCode && b.Language == "yaml-frontmatter" {
			continue
		}
		if b.Kind == BlockDivider || b.Kind == BlockImage {
			continue
		}
		s := strings.TrimSpace(b.PlainText())
		if s != "" {
			if nl := strings.IndexByte(s, '\n'); nl >= 0 {
				s = strings.TrimSpace(s[:nl])
			}
			return s
		}
	}
	return ""
}

// Annotation tracks single-block or multi-block margin notes, emoji reactions,
// and AI rewrite proposals attached to block ranges.
type Annotation struct {
	ID            string           `json:"id"`
	StartBlockID  BlockID          `json:"startBlockId"`
	StartOffset   int              `json:"startOffset"` // rune offset in StartBlock
	EndBlockID    BlockID          `json:"endBlockId"`
	EndOffset     int              `json:"endOffset"` // rune offset in EndBlock
	QuotedText    string           `json:"quotedText"`
	Note          string           `json:"note,omitempty"`
	Reactions     []string         `json:"reactions,omitempty"`
	RewritePrompt string           `json:"rewritePrompt,omitempty"`
	RewriteResult string           `json:"rewriteResult,omitempty"`
	Answer        string           `json:"answer,omitempty"` // AI reply when the note asks a question instead of requesting an edit
	Thread        []AnnotationTurn `json:"thread,omitempty"` // earlier note/reply rounds; Note holds the latest follow-up
	AIError       string           `json:"aiError,omitempty"`
	Applied       bool             `json:"applied,omitempty"`
	RewriteBusy   bool             `json:"-"`
	CreatedAtUnix int64            `json:"createdAt"`
}

// AnnotationTurn is one earlier round on an annotation: what was asked and what the AI said.
type AnnotationTurn struct {
	Q    string `json:"q"`
	A    string `json:"a"`
	Kind string `json:"kind,omitempty"` // "" / "edit" / "answer"
}

// AnnotationReply is the AI's reply to one annotation: an edit proposal or an answer.
type AnnotationReply struct {
	Kind string // "edit" or "answer"
	Text string
}

const AnnotationReplyAnswer = "answer"

type OutlineItem struct {
	BlockIndex int
	BlockID    BlockID
	Level      int // 1..4
	Title      string
	Preview    string
}
