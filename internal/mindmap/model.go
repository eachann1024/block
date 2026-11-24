package mindmap

import (
	"encoding/json"
	"regexp"
	"strings"
)

// LayoutMode selects the branch expansion geometry.
type LayoutMode string

const (
	LayoutRight LayoutMode = "right"
	LayoutBoth  LayoutMode = "both"
)

// ViewMode selects the active mindmap block view tab.
type ViewMode string

const (
	ViewMap     ViewMode = "map"
	ViewOutline ViewMode = "out"
	ViewSource  ViewMode = "src"
)

// HeightMode selects the inline canvas height preset.
type HeightMode string

const (
	HeightCompact  HeightMode = "compact"
	HeightStandard HeightMode = "standard"
	HeightTall     HeightMode = "tall"
)

// Config controls native mindmap rendering, synced from the block-mindmap plugin.
type Config struct {
	Enabled       bool
	DefaultLayout LayoutMode
	Height        HeightMode
	ShowHints     bool
}

// DefaultConfig returns the standard enabled mindmap configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		DefaultLayout: LayoutRight,
		Height:        HeightStandard,
		ShowHints:     true,
	}
}

// InlineHeightDIP returns the inline block canvas height in DIPs.
func (cfg Config) InlineHeightDIP() float32 {
	switch cfg.Height {
	case HeightCompact:
		return 310
	case HeightTall:
		return 480
	default:
		return 370
	}
}

// Node represents a single topic in a mind map tree.
type Node struct {
	ID       int     `json:"id"`
	Text     string  `json:"t"`
	Link     string  `json:"l,omitempty"`
	Task     *bool   `json:"k,omitempty"`
	Folded   bool    `json:"fold,omitempty"`
	Children []*Node `json:"c,omitempty"`

	// Runtime layout fields (not serialized).
	Parent *Node   `json:"-"`
	Depth  int     `json:"-"`
	Branch int     `json:"-"`
	Dir    int     `json:"-"` // +1 right, -1 left
	X      float32 `json:"-"`
	Y      float32 `json:"-"`
	W      float32 `json:"-"`
	H      float32 `json:"-"`
	SubH   float32 `json:"-"`
}

// ImportReport summarizes an external mind map file import, including any
// unsupported features that could not be represented in Markdown.
type ImportReport struct {
	FileName      string
	Format        string // "md" | "opml" | "mm" | "xmind"
	Nodes         int
	Tasks         int
	Links         int
	Relationships int // Dropped cross-node relationship arrows
	Summaries     int // Dropped brace summaries
	Boundaries    int // Dropped visual boundaries/frames
	Notes         int // Dropped long rich-text topic notes
	Callouts      int // Dropped floating callouts / detached topics
	Images        int // Dropped embedded images
	ExtraSheets   int // Dropped additional workbook sheets (e.g. multi-sheet XMind)
}

// HasDropped reports whether any unsupported elements were omitted during import.
func (r ImportReport) HasDropped() bool {
	return r.Relationships > 0 || r.Summaries > 0 || r.Boundaries > 0 ||
		r.Notes > 0 || r.Callouts > 0 || r.Images > 0 || r.ExtraSheets > 0
}

// BoolPtr returns a newly allocated pointer to v.
func BoolPtr(v bool) *bool {
	b := v
	return &b
}

// Clone deep-copies the tree and repairs Parent back-pointers.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	cp := &Node{
		ID:     n.ID,
		Text:   n.Text,
		Link:   n.Link,
		Folded: n.Folded,
	}
	if n.Task != nil {
		cp.Task = BoolPtr(*n.Task)
	}
	if len(n.Children) > 0 {
		cp.Children = make([]*Node, len(n.Children))
		for i, ch := range n.Children {
			child := ch.Clone()
			child.Parent = cp
			cp.Children[i] = child
		}
	}
	return cp
}

// RepairParents refreshes Parent pointers across the entire subtree.
func (n *Node) RepairParents(parent *Node) {
	if n == nil {
		return
	}
	n.Parent = parent
	for _, ch := range n.Children {
		ch.RepairParents(n)
	}
}

// AssignIDs ensures every node in the tree has a unique positive ID and returns the max ID.
func (n *Node) AssignIDs(start int) int {
	if n == nil {
		return start
	}
	cur := start
	var walk func(x *Node)
	walk = func(x *Node) {
		cur++
		x.ID = cur
		for _, ch := range x.Children {
			walk(ch)
		}
	}
	walk(n)
	return cur
}

// FindByID locates a node in the subtree by ID.
func (n *Node) FindByID(id int) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, ch := range n.Children {
		if found := ch.FindByID(id); found != nil {
			return found
		}
	}
	return nil
}

// IsAncestorOf reports whether n is an ancestor of (or identical to) target.
func (n *Node) IsAncestorOf(target *Node) bool {
	for cur := target; cur != nil; cur = cur.Parent {
		if cur == n {
			return true
		}
	}
	return false
}

// DescendantCount returns the number of descendants below n (excluding n itself).
func (n *Node) DescendantCount() int {
	if n == nil {
		return 0
	}
	total := 0
	for _, ch := range n.Children {
		total += 1 + ch.DescendantCount()
	}
	return total
}

// CountStats counts nodes, tasks, and links across the entire tree (including root).
func (n *Node) CountStats() (nodes, tasks, links int) {
	if n == nil {
		return 0, 0, 0
	}
	var walk func(x *Node)
	walk = func(x *Node) {
		nodes++
		if x.Task != nil {
			tasks++
		}
		if strings.TrimSpace(x.Link) != "" {
			links++
		}
		for _, ch := range x.Children {
			walk(ch)
		}
	}
	walk(n)
	return
}

// IsOpen reports whether node n has visible children.
func (n *Node) IsOpen() bool {
	return n != nil && len(n.Children) > 0 && !n.Folded
}

// IndexInParent returns n's index in n.Parent.Children or -1.
func (n *Node) IndexInParent() int {
	if n == nil || n.Parent == nil {
		return -1
	}
	for i, ch := range n.Parent.Children {
		if ch == n {
			return i
		}
	}
	return -1
}

// --- Pure Markdown Codec (Single Source of Truth) ---

// ParseMarkdown parses a ```mindmap code block or Markdown outline into a Node tree.
// Supports:
//   - `# Root Title` (and nested `##`, `###` headings when importing regular Markdown docs)
//   - Indented `- `, `* `, `+ `, `1. ` lists (2-space, 4-space, or tab indentation)
//   - Task checkboxes `- [ ] ...` and `- [x] ...`
//   - Standard Markdown links `[label](target)` (including inside task items)
func ParseMarkdown(src string) *Node {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	if inner, ok := extractFencedMindMap(src); ok {
		src = inner
	}
	lines := strings.Split(src, "\n")

	type stackEntry struct {
		node    *Node
		indent  int // -1 for headings, >=0 for list indentation columns
		heading int // heading level (1..6) for heading entries, 0 for list entries
	}

	var root *Node
	var stack []stackEntry

	ensureRoot := func(title string) {
		if root != nil {
			return
		}
		t := strings.TrimSpace(title)
		if t == "" {
			t = "中心主题"
		}
		text, link := parseInlineLink(t)
		root = &Node{Text: text, Link: link}
		stack = []stackEntry{{node: root, indent: -1, heading: 1}}
	}

	for _, rawLine := range lines {
		if strings.TrimSpace(rawLine) == "" {
			continue
		}
		trimmed := strings.TrimSpace(rawLine)

		// Heading line: # ...
		if lvl, rest, ok := parseHeadingLine(trimmed); ok {
			task, restText := parseTaskPrefix(rest)
			if root == nil {
				text, link := parseInlineLink(restText)
				root = &Node{Text: text, Link: link, Task: task}
				stack = []stackEntry{{node: root, indent: -1, heading: lvl}}
				continue
			}
			// Pop back to a heading strictly shallower than lvl.
			for len(stack) > 1 && (stack[len(stack)-1].heading == 0 || stack[len(stack)-1].heading >= lvl) {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1].node
			text, link := parseInlineLink(restText)
			node := &Node{Text: text, Link: link, Task: task, Parent: parent}
			parent.Children = append(parent.Children, node)
			stack = append(stack, stackEntry{node: node, indent: -1, heading: lvl})
			continue
		}

		// List item line: optional indent + (- | * | + | 1.) + space + content
		indent, content, ok := parseListLine(rawLine)
		if ok {
			ensureRoot("中心主题")
			task, rest := parseTaskPrefix(content)
			text, link := parseInlineLink(rest)
			if text == "" {
				text = "新节点"
			}
			// Pop list entries with indent >= current indent; keep heading ancestors intact.
			for len(stack) > 1 && stack[len(stack)-1].heading == 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1].node
			node := &Node{
				Text:   text,
				Link:   link,
				Task:   task,
				Parent: parent,
			}
			parent.Children = append(parent.Children, node)
			stack = append(stack, stackEntry{node: node, indent: indent, heading: 0})
			continue
		}

		// Bare text line before any root becomes the root title; otherwise honor indentation
		// so plain indented outlines (without "- ") also form a proper hierarchy.
		if root == nil {
			ensureRoot(trimmed)
		} else {
			indent := countLeadingIndent(rawLine)
			for len(stack) > 1 && stack[len(stack)-1].heading == 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1].node
			task, rest := parseTaskPrefix(trimmed)
			text, link := parseInlineLink(rest)
			if text == "" {
				text = "新节点"
			}
			node := &Node{Text: text, Link: link, Task: task, Parent: parent}
			parent.Children = append(parent.Children, node)
			stack = append(stack, stackEntry{node: node, indent: indent, heading: 0})
		}
	}

	ensureRoot("中心主题")
	root.RepairParents(nil)
	root.AssignIDs(0)
	return root
}

// SerializeMarkdown renders a Node tree into clean, round-trip Markdown.
func SerializeMarkdown(root *Node) string {
	if root == nil {
		return "# 中心主题\n"
	}
	var b strings.Builder
	b.WriteString("# ")
	if root.Task != nil {
		if *root.Task {
			b.WriteString("[x] ")
		} else {
			b.WriteString("[ ] ")
		}
	}
	b.WriteString(formatNodeText(root))
	b.WriteByte('\n')

	var writeChildren func(parent *Node, depth int)
	writeChildren = func(parent *Node, depth int) {
		for _, ch := range parent.Children {
			for i := 0; i < depth; i++ {
				b.WriteString("  ")
			}
			b.WriteString("- ")
			if ch.Task != nil {
				if *ch.Task {
					b.WriteString("[x] ")
				} else {
					b.WriteString("[ ] ")
				}
			}
			b.WriteString(formatNodeText(ch))
			b.WriteByte('\n')
			writeChildren(ch, depth+1)
		}
	}
	writeChildren(root, 0)
	return strings.TrimRight(b.String(), "\n")
}

// SerializeSubtreeMarkdown serializes a single node (and its descendants) for clipboard copy.
// A plain leaf without task/link/children is copied as plain text for easy pasting elsewhere;
// structured subtrees, tasks, or links are serialized as clean Markdown list items.
func SerializeSubtreeMarkdown(n *Node) string {
	if n == nil {
		return ""
	}
	if len(n.Children) == 0 && n.Task == nil && strings.TrimSpace(n.Link) == "" {
		return encodeNodeLineBreaks(n.Text)
	}
	var b strings.Builder
	var writeNode func(cur *Node, depth int)
	writeNode = func(cur *Node, depth int) {
		for i := 0; i < depth; i++ {
			b.WriteString("  ")
		}
		b.WriteString("- ")
		if cur.Task != nil {
			if *cur.Task {
				b.WriteString("[x] ")
			} else {
				b.WriteString("[ ] ")
			}
		}
		b.WriteString(formatNodeText(cur))
		b.WriteByte('\n')
		for _, ch := range cur.Children {
			writeNode(ch, depth+1)
		}
	}
	writeNode(n, 0)
	return strings.TrimRight(b.String(), "\n")
}

// ParseClipboardNodes parses plain text, indented outlines, or Markdown lists/headings
// from the clipboard into one or more top-level Node subtrees without wrapping them
// in a synthetic root node.
func ParseClipboardNodes(src string) []*Node {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	if inner, ok := extractFencedMindMap(src); ok {
		src = inner
	}
	type stackEntry struct {
		node    *Node
		indent  int
		heading int
	}
	virtualRoot := &Node{}
	stack := []stackEntry{{node: virtualRoot, indent: -1, heading: 0}}

	for _, rawLine := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if lvl, rest, ok := parseHeadingLine(trimmed); ok {
			task, restText := parseTaskPrefix(rest)
			text, link := parseInlineLink(restText)
			if text == "" {
				continue
			}
			for len(stack) > 1 && (stack[len(stack)-1].heading == 0 || stack[len(stack)-1].heading >= lvl) {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1].node
			node := &Node{Text: text, Link: link, Task: task, Parent: parent}
			parent.Children = append(parent.Children, node)
			stack = append(stack, stackEntry{node: node, indent: -1, heading: lvl})
			continue
		}
		if indent, content, ok := parseListLine(rawLine); ok {
			task, rest := parseTaskPrefix(content)
			text, link := parseInlineLink(rest)
			if text == "" {
				continue
			}
			for len(stack) > 1 && stack[len(stack)-1].heading == 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1].node
			node := &Node{Text: text, Link: link, Task: task, Parent: parent}
			parent.Children = append(parent.Children, node)
			stack = append(stack, stackEntry{node: node, indent: indent, heading: 0})
			continue
		}
		indent := countLeadingIndent(rawLine)
		task, rest := parseTaskPrefix(trimmed)
		text, link := parseInlineLink(rest)
		if text == "" {
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].heading == 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1].node
		node := &Node{Text: text, Link: link, Task: task, Parent: parent}
		parent.Children = append(parent.Children, node)
		stack = append(stack, stackEntry{node: node, indent: indent, heading: 0})
	}
	for _, ch := range virtualRoot.Children {
		ch.RepairParents(nil)
	}
	return virtualRoot.Children
}

var brTagRe = regexp.MustCompile(`(?i)<br\s*/?>`)

func normalizeNodeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

func encodeNodeLineBreaks(s string) string {
	return strings.ReplaceAll(normalizeNodeText(s), "\n", "<br>")
}

func decodeNodeLineBreaks(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	return brTagRe.ReplaceAllString(s, "\n")
}

func formatNodeText(n *Node) string {
	text := encodeNodeLineBreaks(n.Text)
	if text == "" {
		text = "新节点"
	}
	link := strings.TrimSpace(n.Link)
	if link == "" {
		return text
	}
	escapedText := strings.ReplaceAll(text, "]", `\]`)
	escapedLink := strings.ReplaceAll(link, ")", `\)`)
	return "[" + escapedText + "](" + escapedLink + ")"
}

func parseHeadingLine(trimmed string) (level int, text string, ok bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	i := 0
	for i < len(trimmed) && i < 6 && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i >= len(trimmed) || (trimmed[i] != ' ' && trimmed[i] != '\t') {
		return 0, "", false
	}
	return i, strings.TrimSpace(trimmed[i+1:]), true
}

// extractFencedMindMap extracts the inner content of a ```mindmap fenced code block
// when importing a Markdown document that wraps its mind map in a code fence.
func extractFencedMindMap(src string) (string, bool) {
	lines := strings.Split(src, "\n")
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if ch, n, info, ok := parseFenceOpen(trimmed); ok {
				lang := strings.ToLower(strings.TrimSpace(info))
				if lang == "mindmap" || lang == "mind-map" {
					inFence = true
					fenceChar = ch
					fenceLen = n
					body = body[:0]
				}
			}
			continue
		}
		if isFenceClose(trimmed, fenceChar, fenceLen) {
			return strings.Join(body, "\n"), true
		}
		body = append(body, line)
	}
	if inFence {
		return strings.Join(body, "\n"), true
	}
	return "", false
}

func parseFenceOpen(trimmed string) (ch byte, count int, info string, ok bool) {
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, "", false
	}
	ch = trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	return ch, n, trimmed[n:], true
}

func isFenceClose(trimmed string, ch byte, minLen int) bool {
	if len(trimmed) < minLen {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	return n >= minLen && strings.TrimSpace(trimmed[n:]) == ""
}

func countLeadingIndent(raw string) int {
	cols := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case ' ':
			cols++
		case '\t':
			cols += 2
		default:
			return cols
		}
	}
	return cols
}

func parseListLine(raw string) (indent int, content string, ok bool) {
	i := 0
	cols := 0
	for i < len(raw) {
		switch raw[i] {
		case ' ':
			cols++
			i++
		case '\t':
			cols += 2
			i++
		default:
			goto marker
		}
	}
marker:
	rest := raw[i:]
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && (rest[1] == ' ' || rest[1] == '\t') {
		return cols, strings.TrimSpace(rest[2:]), true
	}
	// Ordered list marker: digits + '.' + space
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j > 0 && j+1 < len(rest) && (rest[j] == '.' || rest[j] == ')') && (rest[j+1] == ' ' || rest[j+1] == '\t') {
		return cols, strings.TrimSpace(rest[j+2:]), true
	}
	return 0, "", false
}

func parseTaskPrefix(s string) (*bool, string) {
	if len(s) >= 4 && s[0] == '[' && s[2] == ']' && (s[3] == ' ' || s[3] == '\t') {
		switch s[1] {
		case ' ':
			return BoolPtr(false), strings.TrimSpace(s[4:])
		case 'x', 'X':
			return BoolPtr(true), strings.TrimSpace(s[4:])
		}
	}
	return nil, s
}

func parseInlineLink(s string) (text, link string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, ")") {
		return normalizeNodeText(decodeNodeLineBreaks(s)), ""
	}
	// Find matching unescaped "]("
	closeIdx := -1
	for i := 1; i+1 < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == ']' && s[i+1] == '(' {
			closeIdx = i
			break
		}
	}
	if closeIdx <= 1 {
		return normalizeNodeText(decodeNodeLineBreaks(s)), ""
	}
	rawText := s[1:closeIdx]
	rawLink := s[closeIdx+2 : len(s)-1]
	rawText = strings.ReplaceAll(rawText, `\]`, "]")
	rawLink = strings.ReplaceAll(rawLink, `\)`, ")")
	return normalizeNodeText(decodeNodeLineBreaks(rawText)), strings.TrimSpace(rawLink)
}

// --- Tree Snapshot & Fold State Preservation ---

func marshalSnapshot(root *Node) []byte {
	b, _ := json.Marshal(root)
	return b
}

func unmarshalSnapshot(b []byte) *Node {
	var root Node
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}
	root.RepairParents(nil)
	return &root
}

// applyFoldPaths preserves folded node states across re-parses using slash-joined text paths.
func captureFoldPaths(root *Node) map[string]bool {
	out := make(map[string]bool)
	if root == nil {
		return out
	}
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		key := prefix + "/" + n.Text
		if n.Folded {
			out[key] = true
		}
		for _, ch := range n.Children {
			walk(ch, key)
		}
	}
	walk(root, "")
	return out
}

func restoreFoldPaths(root *Node, paths map[string]bool) {
	if root == nil || len(paths) == 0 {
		return
	}
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		key := prefix + "/" + n.Text
		if paths[key] && len(n.Children) > 0 {
			n.Folded = true
		}
		for _, ch := range n.Children {
			walk(ch, key)
		}
	}
	walk(root, "")
}
