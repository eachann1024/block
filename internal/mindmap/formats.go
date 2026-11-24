package mindmap

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"block/internal/theme"
)

// Format identifies a supported mind map import or export format.
type Format string

const (
	FormatMarkdown Format = "md"
	FormatOPML     Format = "opml"
	FormatFreeMind Format = "mm"
	FormatXMind    Format = "xmind"
	FormatPNG      Format = "png"
	FormatPDF      Format = "pdf"
)

// DetectFormat infers a mind map Format from a filename extension.
func DetectFormat(filename string) Format {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filename))) {
	case ".md", ".markdown":
		return FormatMarkdown
	case ".opml":
		return FormatOPML
	case ".mm":
		return FormatFreeMind
	case ".xmind":
		return FormatXMind
	case ".png":
		return FormatPNG
	case ".pdf":
		return FormatPDF
	default:
		return ""
	}
}

// ImportBytes parses a mind map file (Markdown, OPML, FreeMind .mm, or XMind .xmind)
// into a normalized Node tree and returns an ImportReport of imported and dropped items.
func ImportBytes(filename string, data []byte) (*Node, ImportReport, error) {
	format := DetectFormat(filename)
	if format == "" {
		format = sniffFormat(data)
	}
	report := ImportReport{
		FileName: filepath.Base(strings.TrimSpace(filename)),
		Format:   string(format),
	}
	if report.FileName == "." || report.FileName == "/" {
		report.FileName = ""
	}

	var root *Node
	var err error
	switch format {
	case FormatMarkdown:
		root = ParseMarkdown(string(data))
	case FormatOPML:
		root, err = importOPML(data, &report)
	case FormatFreeMind:
		root, err = importFreeMind(data, &report)
	case FormatXMind:
		root, err = importXMind(data, &report)
	default:
		return nil, report, fmt.Errorf("unsupported mindmap import format: %q", filename)
	}
	if err != nil {
		return nil, report, err
	}
	if root == nil {
		root = &Node{Text: "中心主题"}
	}
	if strings.TrimSpace(root.Text) == "" {
		root.Text = "中心主题"
	}
	root.RepairParents(nil)
	root.AssignIDs(0)
	report.Nodes, report.Tasks, report.Links = root.CountStats()
	return root, report, nil
}

// ExportBytes serializes a mind map tree into the requested format.
func ExportBytes(root *Node, layout LayoutMode, format Format, pal theme.Palette) ([]byte, error) {
	if root == nil {
		return nil, errors.New("cannot export nil mindmap")
	}
	switch format {
	case FormatMarkdown:
		return []byte(SerializeMarkdown(root)), nil
	case FormatOPML:
		return exportOPML(root)
	case FormatFreeMind:
		return exportFreeMind(root)
	case FormatXMind:
		return exportXMind(root, layout)
	case FormatPNG:
		return ExportPNG(root, layout, pal)
	case FormatPDF:
		return ExportPDF(root, layout, pal)
	default:
		return nil, fmt.Errorf("unsupported mindmap export format: %q", format)
	}
}

func sniffFormat(data []byte) Format {
	if len(data) >= 4 && data[0] == 'P' && data[1] == 'K' &&
		((data[2] == 0x03 && data[3] == 0x04) || (data[2] == 0x05 && data[3] == 0x06)) {
		return FormatXMind
	}
	trimmed := strings.ToLower(strings.TrimSpace(string(data)))
	if strings.HasPrefix(trimmed, "<") {
		if strings.Contains(trimmed, "<opml") {
			return FormatOPML
		}
		if strings.Contains(trimmed, "<map") {
			return FormatFreeMind
		}
	}
	return FormatMarkdown
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLText(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "%3C") || strings.Contains(raw, "%3c") || strings.Contains(raw, "%20") {
		if unescaped, err := url.QueryUnescape(raw); err == nil {
			raw = unescaped
		}
	}
	noTags := htmlTagRe.ReplaceAllString(raw, " ")
	plain := html.UnescapeString(noTags)
	fields := strings.Fields(plain)
	return strings.Join(fields, " ")
}

// --- OPML 2.0 ---

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title string `xml:"title"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Text      string        `xml:"text,attr"`
	MubuText  string        `xml:"_mubu_text,attr,omitempty"`
	URL       string        `xml:"url,attr,omitempty"`
	Link      string        `xml:"_link,attr,omitempty"`
	Complete  string        `xml:"_complete,attr,omitempty"`
	Status    string        `xml:"_status,attr,omitempty"`
	Note      string        `xml:"_note,attr,omitempty"`
	Collapsed string        `xml:"_collapsed,attr,omitempty"`
	Outlines  []opmlOutline `xml:"outline"`
}

func importOPML(data []byte, report *ImportReport) (*Node, error) {
	var doc opmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse opml: %w", err)
	}
	var convert func(o opmlOutline) *Node
	convert = func(o opmlOutline) *Node {
		text := strings.TrimSpace(stripHTMLText(o.Text))
		if text == "" && o.MubuText != "" {
			text = strings.TrimSpace(stripHTMLText(o.MubuText))
		}
		if text == "" {
			text = "新节点"
		}
		link := strings.TrimSpace(o.URL)
		if link == "" {
			link = strings.TrimSpace(o.Link)
		}
		var task *bool
		switch strings.ToLower(strings.TrimSpace(o.Complete)) {
		case "true":
			task = BoolPtr(true)
		case "false":
			task = BoolPtr(false)
		default:
			switch strings.ToLower(strings.TrimSpace(o.Status)) {
			case "checked":
				task = BoolPtr(true)
			case "unchecked":
				task = BoolPtr(false)
			}
		}
		if strings.TrimSpace(o.Note) != "" {
			report.Notes++
		}
		n := &Node{
			Text:   text,
			Link:   link,
			Task:   task,
			Folded: strings.EqualFold(strings.TrimSpace(o.Collapsed), "true"),
		}
		for _, ch := range o.Outlines {
			n.Children = append(n.Children, convert(ch))
		}
		return n
	}

	switch len(doc.Body.Outlines) {
	case 0:
		title := strings.TrimSpace(doc.Head.Title)
		if title == "" {
			title = "中心主题"
		}
		return &Node{Text: title}, nil
	case 1:
		return convert(doc.Body.Outlines[0]), nil
	default:
		title := strings.TrimSpace(doc.Head.Title)
		if title == "" {
			title = "中心主题"
		}
		root := &Node{Text: title}
		for _, o := range doc.Body.Outlines {
			root.Children = append(root.Children, convert(o))
		}
		return root, nil
	}
}

func exportOPML(root *Node) ([]byte, error) {
	var toOutline func(n *Node) opmlOutline
	toOutline = func(n *Node) opmlOutline {
		o := opmlOutline{
			Text: n.Text,
			URL:  strings.TrimSpace(n.Link),
		}
		if n.Task != nil {
			if *n.Task {
				o.Complete = "true"
			} else {
				o.Complete = "false"
			}
		}
		if n.Folded && len(n.Children) > 0 {
			o.Collapsed = "true"
		}
		for _, ch := range n.Children {
			o.Outlines = append(o.Outlines, toOutline(ch))
		}
		return o
	}
	doc := opmlDoc{
		Version: "2.0",
		Head:    opmlHead{Title: root.Text},
		Body:    opmlBody{Outlines: []opmlOutline{toOutline(root)}},
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// --- FreeMind / Freeplane (.mm) ---

type mmMap struct {
	XMLName xml.Name `xml:"map"`
	Version string   `xml:"version,attr"`
	Nodes   []mmNode `xml:"node"`
}

type mmNode struct {
	Text        string          `xml:"TEXT,attr,omitempty"`
	Link        string          `xml:"LINK,attr,omitempty"`
	Folded      string          `xml:"FOLDED,attr,omitempty"`
	Icons       []mmIcon        `xml:"icon"`
	RichContent []mmRichContent `xml:"richcontent"`
	ArrowLinks  []mmArrowLink   `xml:"arrowlink"`
	Clouds      []mmCloud       `xml:"cloud"`
	Children    []mmNode        `xml:"node"`
}

type mmIcon struct {
	Builtin string `xml:"BUILTIN,attr"`
}

type mmRichContent struct {
	Type  string `xml:"TYPE,attr"`
	Inner string `xml:",innerxml"`
}

type mmArrowLink struct {
	Destination string `xml:"DESTINATION,attr,omitempty"`
}

type mmCloud struct{}

func importFreeMind(data []byte, report *ImportReport) (*Node, error) {
	var m mmMap
	if err := xml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse freemind: %w", err)
	}
	var convert func(mn mmNode) *Node
	convert = func(mn mmNode) *Node {
		text := strings.TrimSpace(mn.Text)
		for _, rc := range mn.RichContent {
			switch strings.ToUpper(strings.TrimSpace(rc.Type)) {
			case "NODE":
				if text == "" {
					text = strings.TrimSpace(stripHTMLText(rc.Inner))
				}
			case "NOTE":
				if strings.TrimSpace(stripHTMLText(rc.Inner)) != "" || strings.TrimSpace(rc.Inner) != "" {
					report.Notes++
				}
			}
		}
		if text == "" {
			text = "新节点"
		}
		var task *bool
		for _, ic := range mn.Icons {
			switch strings.ToLower(strings.TrimSpace(ic.Builtin)) {
			case "checked":
				task = BoolPtr(true)
			case "unchecked":
				task = BoolPtr(false)
			}
		}
		report.Relationships += len(mn.ArrowLinks)
		report.Boundaries += len(mn.Clouds)
		n := &Node{
			Text:   text,
			Link:   strings.TrimSpace(mn.Link),
			Task:   task,
			Folded: strings.EqualFold(strings.TrimSpace(mn.Folded), "true"),
		}
		for _, ch := range mn.Children {
			n.Children = append(n.Children, convert(ch))
		}
		return n
	}

	switch len(m.Nodes) {
	case 0:
		return &Node{Text: "中心主题"}, nil
	case 1:
		return convert(m.Nodes[0]), nil
	default:
		root := &Node{Text: "中心主题"}
		for _, mn := range m.Nodes {
			root.Children = append(root.Children, convert(mn))
		}
		return root, nil
	}
}

func exportFreeMind(root *Node) ([]byte, error) {
	var toMM func(n *Node) mmNode
	toMM = func(n *Node) mmNode {
		mn := mmNode{
			Text: n.Text,
			Link: strings.TrimSpace(n.Link),
		}
		if n.Folded && len(n.Children) > 0 {
			mn.Folded = "true"
		}
		if n.Task != nil {
			if *n.Task {
				mn.Icons = []mmIcon{{Builtin: "checked"}}
			} else {
				mn.Icons = []mmIcon{{Builtin: "unchecked"}}
			}
		}
		for _, ch := range n.Children {
			mn.Children = append(mn.Children, toMM(ch))
		}
		return mn
	}
	m := mmMap{
		Version: "1.0.1",
		Nodes:   []mmNode{toMM(root)},
	}
	body, err := xml.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// --- XMind (.xmind: Zen content.json + Legacy XMind 8 content.xml) ---

type xmindSheet struct {
	ID            string            `json:"id"`
	Class         string            `json:"class,omitempty"`
	Title         string            `json:"title"`
	RootTopic     xmindTopic        `json:"rootTopic"`
	Relationships []json.RawMessage `json:"relationships,omitempty"`
}

type xmindTopic struct {
	ID             string            `json:"id"`
	Class          string            `json:"class,omitempty"`
	Title          string            `json:"title"`
	StructureClass string            `json:"structureClass,omitempty"`
	Href           string            `json:"href,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	Markers        []xmindMarker     `json:"markers,omitempty"`
	Notes          json.RawMessage   `json:"notes,omitempty"`
	Image          json.RawMessage   `json:"image,omitempty"`
	Boundaries     []json.RawMessage `json:"boundaries,omitempty"`
	Summaries      []json.RawMessage `json:"summaries,omitempty"`
	Children       *xmindChildren    `json:"children,omitempty"`
}

type xmindMarker struct {
	MarkerID string `json:"markerId"`
}

type xmindChildren struct {
	Attached []xmindTopic `json:"attached,omitempty"`
	Detached []xmindTopic `json:"detached,omitempty"`
	Callout  []xmindTopic `json:"callout,omitempty"`
	Summary  []xmindTopic `json:"summary,omitempty"`
}

func importXMind(data []byte, report *ImportReport) (*Node, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open xmind zip: %w", err)
	}
	var contentJSON, contentXML []byte
	for _, f := range zr.File {
		switch f.Name {
		case "content.json":
			contentJSON, err = readZipFile(f)
			if err != nil {
				return nil, err
			}
		case "content.xml":
			contentXML, err = readZipFile(f)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(contentJSON) > 0 {
		return importXMindJSON(contentJSON, report)
	}
	if len(contentXML) > 0 {
		return importXMindXML(contentXML, report)
	}
	return nil, errors.New("xmind archive missing content.json and content.xml")
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	const maxSize = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(rc, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("xmind entry %q exceeds %d MiB size limit", f.Name, maxSize/(1024*1024))
	}
	return data, nil
}

func xmindTopicEmpty(t xmindTopic) bool {
	return strings.TrimSpace(t.Title) == "" &&
		strings.TrimSpace(t.Href) == "" &&
		len(t.Markers) == 0 &&
		!hasNonEmptyJSON(t.Notes) &&
		!hasNonEmptyJSON(t.Image) &&
		len(t.Boundaries) == 0 &&
		len(t.Summaries) == 0 &&
		(t.Children == nil || (len(t.Children.Attached) == 0 && len(t.Children.Detached) == 0 && len(t.Children.Summary) == 0 && len(t.Children.Callout) == 0))
}

func xmind8TopicEmpty(t xmind8Topic) bool {
	return strings.TrimSpace(t.Title) == "" &&
		strings.TrimSpace(t.Href) == "" &&
		len(t.Markers) == 0 &&
		strings.TrimSpace(t.Notes) == "" &&
		len(t.Boundaries) == 0 &&
		len(t.Summaries) == 0 &&
		len(t.Topics) == 0
}

func importXMindJSON(data []byte, report *ImportReport) (*Node, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil, errors.New("xmind content.json is empty or null")
	}
	var sheets []xmindSheet
	if err := json.Unmarshal(data, &sheets); err != nil {
		var single xmindSheet
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return nil, fmt.Errorf("parse xmind content.json: %w", err)
		}
		sheets = []xmindSheet{single}
	}
	if len(sheets) == 0 || xmindTopicEmpty(sheets[0].RootTopic) {
		return nil, errors.New("xmind content.json contains no valid root topic")
	}
	sheet := sheets[0]
	report.Relationships += len(sheet.Relationships)

	var convert func(t xmindTopic) *Node
	convert = func(t xmindTopic) *Node {
		title := strings.TrimSpace(t.Title)
		if title == "" {
			title = "新节点"
		}
		var task *bool
		for _, m := range t.Markers {
			switch m.MarkerID {
			case "task-done":
				task = BoolPtr(true)
			case "task-start", "task-oct", "task-quarter", "task-half", "task-3quar", "task-pause":
				task = BoolPtr(false)
			}
		}
		if hasNonEmptyJSON(t.Notes) {
			report.Notes++
		}
		if hasNonEmptyJSON(t.Image) {
			report.Images++
		}
		report.Boundaries += len(t.Boundaries)

		n := &Node{
			Text:   title,
			Link:   strings.TrimSpace(t.Href),
			Task:   task,
			Folded: t.Branch == "folded",
		}
		if t.Children != nil {
			report.Summaries += max(len(t.Summaries), len(t.Children.Summary))
			report.Callouts += len(t.Children.Detached) + len(t.Children.Callout)
			for _, ch := range t.Children.Attached {
				n.Children = append(n.Children, convert(ch))
			}
		} else {
			report.Summaries += len(t.Summaries)
		}
		return n
	}

	root := convert(sheet.RootTopic)
	if len(sheets) > 1 {
		report.ExtraSheets += len(sheets) - 1
		var countExtra func(t xmindTopic)
		countExtra = func(t xmindTopic) {
			if hasNonEmptyJSON(t.Notes) {
				report.Notes++
			}
			if hasNonEmptyJSON(t.Image) {
				report.Images++
			}
			report.Boundaries += len(t.Boundaries)
			if t.Children != nil {
				report.Summaries += max(len(t.Summaries), len(t.Children.Summary))
				report.Callouts += len(t.Children.Detached) + len(t.Children.Callout)
				for _, ch := range t.Children.Attached {
					countExtra(ch)
				}
			} else {
				report.Summaries += len(t.Summaries)
			}
		}
		for _, extra := range sheets[1:] {
			report.Relationships += len(extra.Relationships)
			countExtra(extra.RootTopic)
		}
	}
	return root, nil
}

func hasNonEmptyJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "{}" && s != "[]"
}

type xmind8Content struct {
	XMLName xml.Name     `xml:"xmap-content"`
	Sheets  []xmind8Sheet `xml:"sheet"`
}

type xmind8Sheet struct {
	Topic         xmind8Topic    `xml:"topic"`
	Relationships []xmind8Rel    `xml:"relationships>relationship"`
}

type xmind8Rel struct{}

type xmind8Topic struct {
	Branch     string          `xml:"branch,attr"`
	Href       string          `xml:"href,attr"`
	Title      string          `xml:"title"`
	Markers    []xmind8Marker  `xml:"marker-refs>marker-ref"`
	Notes      string          `xml:"notes"`
	Boundaries []xmind8Elem    `xml:"boundaries>boundary"`
	Summaries  []xmind8Elem    `xml:"summaries>summary"`
	Topics     []xmind8Topics  `xml:"children>topics"`
}

type xmind8Marker struct {
	MarkerID string `xml:"marker-id,attr"`
}

type xmind8Elem struct{}

type xmind8Topics struct {
	Type   string        `xml:"type,attr"`
	Topics []xmind8Topic `xml:"topic"`
}

func importXMindXML(data []byte, report *ImportReport) (*Node, error) {
	var doc xmind8Content
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse xmind content.xml: %w", err)
	}
	if len(doc.Sheets) == 0 || xmind8TopicEmpty(doc.Sheets[0].Topic) {
		return nil, errors.New("xmind content.xml contains no valid root topic")
	}
	sheet := doc.Sheets[0]
	report.Relationships += len(sheet.Relationships)

	var convert func(t xmind8Topic) *Node
	convert = func(t xmind8Topic) *Node {
		title := strings.TrimSpace(t.Title)
		if title == "" {
			title = "新节点"
		}
		var task *bool
		for _, m := range t.Markers {
			switch m.MarkerID {
			case "task-done":
				task = BoolPtr(true)
			case "task-start", "task-oct", "task-quarter", "task-half", "task-3quar", "task-pause":
				task = BoolPtr(false)
			}
		}
		if strings.TrimSpace(stripHTMLText(t.Notes)) != "" {
			report.Notes++
		}
		report.Boundaries += len(t.Boundaries)

		n := &Node{
			Text:   title,
			Link:   strings.TrimSpace(t.Href),
			Task:   task,
			Folded: t.Branch == "folded",
		}
		summaryTopics := 0
		for _, grp := range t.Topics {
			switch grp.Type {
			case "attached", "":
				for _, ch := range grp.Topics {
					n.Children = append(n.Children, convert(ch))
				}
			case "summary":
				summaryTopics += len(grp.Topics)
			case "detached", "callout":
				report.Callouts += len(grp.Topics)
			}
		}
		report.Summaries += max(len(t.Summaries), summaryTopics)
		return n
	}

	root := convert(sheet.Topic)
	if len(doc.Sheets) > 1 {
		report.ExtraSheets += len(doc.Sheets) - 1
		var countExtra func(t xmind8Topic)
		countExtra = func(t xmind8Topic) {
			if strings.TrimSpace(t.Notes) != "" {
				report.Notes++
			}
			report.Boundaries += len(t.Boundaries)
			report.Summaries += len(t.Summaries)
			for _, tg := range t.Topics {
				switch strings.ToLower(strings.TrimSpace(tg.Type)) {
				case "", "attached":
					for _, ch := range tg.Topics {
						countExtra(ch)
					}
				case "summary":
					report.Summaries += len(tg.Topics)
				default:
					report.Callouts += len(tg.Topics)
				}
			}
		}
		for _, extra := range doc.Sheets[1:] {
			report.Relationships += len(extra.Relationships)
			countExtra(extra.Topic)
		}
	}
	return root, nil
}

func exportXMind(root *Node, layout LayoutMode) ([]byte, error) {
	seq := 0
	nextID := func(prefix string) string {
		seq++
		return fmt.Sprintf("%s-%d", prefix, seq)
	}
	structure := "org.xmind.ui.map.unbalanced"
	if layout == LayoutRight {
		structure = "org.xmind.ui.logic.right"
	}

	var toTopic func(n *Node, isRoot bool) xmindTopic
	toTopic = func(n *Node, isRoot bool) xmindTopic {
		t := xmindTopic{
			ID:    nextID("topic"),
			Title: n.Text,
			Href:  strings.TrimSpace(n.Link),
		}
		if isRoot {
			t.Class = "topic"
			t.StructureClass = structure
		}
		if n.Folded && len(n.Children) > 0 {
			t.Branch = "folded"
		}
		if n.Task != nil {
			if *n.Task {
				t.Markers = []xmindMarker{{MarkerID: "task-done"}}
			} else {
				t.Markers = []xmindMarker{{MarkerID: "task-start"}}
			}
		}
		if len(n.Children) > 0 {
			attached := make([]xmindTopic, len(n.Children))
			for i, ch := range n.Children {
				attached[i] = toTopic(ch, false)
			}
			t.Children = &xmindChildren{Attached: attached}
		}
		return t
	}

	sheets := []xmindSheet{{
		ID:        nextID("sheet"),
		Class:     "sheet",
		Title:     root.Text,
		RootTopic: toTopic(root, true),
	}}
	contentBytes, err := json.Marshal(sheets)
	if err != nil {
		return nil, err
	}
	metadataBytes := []byte(`{"creator":{"name":"Block","version":"1.0"}}`)
	manifestBytes := []byte(`{"file-entries":{"content.json":{},"metadata.json":{}}}`)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"content.json", contentBytes},
		{"metadata.json", metadataBytes},
		{"manifest.json", manifestBytes},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
