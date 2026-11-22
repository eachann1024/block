package codepreview

import (
	"block/internal/theme"
	"strings"
)

func CanonicalLanguage(tag string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(tag)))
	if len(fields) == 0 {
		return "plaintext"
	}
	switch fields[0] {
	case "", "auto", "plaintext", "text", "plain", "txt":
		return "plaintext"
	case "csv":
		return "csv"
	case "latex", "math", "tex", "katex":
		return "latex"
	case "markdown", "md":
		return "markdown"
	case "mermaid", "mmd":
		return "mermaid"
	case "bash", "sh", "shell", "zsh":
		return "bash"
	case "css":
		return "css"
	case "go", "golang":
		return "go"
	case "html", "htm":
		return "html"
	case "javascript", "js", "jsx":
		return "javascript"
	case "json", "jsonc", "json5", "jsonl", "ndjson", "singbox", "sing-box", "v2ray", "xray":
		return "json"
	case "lean", "lean4":
		return "lean"
	case "python", "py":
		return "python"
	case "rust", "rs":
		return "rust"
	case "sql":
		return "sql"
	case "typescript", "ts", "tsx":
		return "typescript"
	case "xml", "svg":
		return "xml"
	case "yaml", "yml", "yaml-frontmatter", "frontmatter", "clash", "mihomo", "stash", "stoverride":
		return "yaml"
	default:
		return fields[0]
	}
}

func IsPreviewLanguage(lang string) bool {
	c := CanonicalLanguage(lang)
	return c == "mermaid" || c == "latex" || c == "csv"
}

var mathPreviewReplacer = strings.NewReplacer(
	`\alpha`, "α", `\beta`, "β", `\gamma`, "γ", `\delta`, "δ", `\theta`, "θ", `\lambda`, "λ", `\pi`, "π", `\sigma`, "σ", `\omega`, "ω",
	`\sum`, "∑", `\prod`, "∏", `\int`, "∫", `\infty`, "∞", `\sqrt`, "√", `\pm`, "±", `\times`, "×", `\div`, "÷",
	`\leq`, "≤", `\geq`, "≥", `\neq`, "≠", `\approx`, "≈", `\rightarrow`, "→", `\Rightarrow`, "⇒", `\in`, "∈",
	`\partial`, "∂", `\nabla`, "∇", `^2`, "²", `^3`, "³", `^n`, "ⁿ",
	`$$`, "", `$`, "",
)

// --- Preview Parsers (CSV, Mermaid, LaTeX) ---

func ParseCSV(src string) [][]string {
	var rows [][]string
	for line := range strings.SplitSeq(src, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.ContainsRune(line, '"') {
			cells := strings.Split(line, ",")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			rows = append(rows, cells)
			continue
		}
		var cells []string
		var cur strings.Builder
		inQuotes := false
		for i := 0; i < len(line); i++ {
			ch := line[i]
			switch {
			case ch == '"' && inQuotes && i+1 < len(line) && line[i+1] == '"':
				cur.WriteByte('"')
				i++
			case ch == '"':
				inQuotes = !inQuotes
			case ch == ',' && !inQuotes:
				cells = append(cells, strings.TrimSpace(cur.String()))
				cur.Reset()
			default:
				cur.WriteByte(ch)
			}
		}
		cells = append(cells, strings.TrimSpace(cur.String()))
		rows = append(rows, cells)
	}
	return rows
}

type MermaidEdge struct {
	From  string
	To    string
	Label string
}

func ParseMermaid(src string) (nodes []string, labels map[string]string, edges []MermaidEdge, errMsg string) {
	labels = make(map[string]string)
	addNode := func(raw string) string {
		raw = strings.TrimSpace(raw)
		id := raw
		lbl := raw
		if i := strings.IndexByte(raw, '['); i > 0 && strings.HasSuffix(raw, "]") {
			id = strings.TrimSpace(raw[:i])
			lbl = strings.TrimSpace(raw[i+1 : len(raw)-1])
		} else if i := strings.IndexByte(raw, '('); i > 0 && strings.HasSuffix(raw, ")") {
			id = strings.TrimSpace(raw[:i])
			lbl = strings.TrimSpace(raw[i+1 : len(raw)-1])
		}
		if id == "" {
			return ""
		}
		_, exists := labels[id]
		if !exists {
			nodes = append(nodes, id)
		}
		if !exists || lbl != id {
			labels[id] = lbl
		}
		return id
	}

	for raw := range strings.SplitSeq(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") ||
			strings.HasPrefix(line, "graph ") || strings.HasPrefix(line, "flowchart ") ||
			strings.HasPrefix(line, "sequenceDiagram") {
			continue
		}
		if idx := strings.Index(line, "-->"); idx >= 0 {
			left := strings.TrimSpace(line[:idx])
			right := strings.TrimSpace(line[idx+3:])
			edgeLbl := ""
			if strings.HasPrefix(right, "|") {
				if end := strings.IndexByte(right[1:], '|'); end >= 0 {
					edgeLbl = strings.TrimSpace(right[1 : end+1])
					right = strings.TrimSpace(right[end+2:])
				}
			}
			a := addNode(left)
			b := addNode(right)
			if a != "" && b != "" {
				edges = append(edges, MermaidEdge{From: a, To: b, Label: edgeLbl})
			}
		} else if idx := strings.Index(line, "->"); idx >= 0 {
			a := addNode(line[:idx])
			b := addNode(line[idx+2:])
			if a != "" && b != "" {
				edges = append(edges, MermaidEdge{From: a, To: b})
			}
		} else {
			addNode(line)
		}
	}
	if len(nodes) == 0 {
		return nil, nil, nil, theme.Tr("Mermaid 图表内容为空或无法解析", "Empty or unrecognized Mermaid diagram")
	}
	return nodes, labels, edges, ""
}

func FormatMath(src string) (string, string) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return "", theme.Tr("数学公式为空", "Math formula is empty")
	}
	out := strings.TrimSpace(mathPreviewReplacer.Replace(trimmed))
	// Balance braces check for basic error bar feedback.
	depth := 0
	for _, ch := range trimmed {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth < 0 {
				return out, theme.Tr("LaTeX 花括号不匹配", "Unbalanced LaTeX braces")
			}
		}
	}
	if depth != 0 {
		return out, theme.Tr("LaTeX 花括号未闭合", "Unclosed LaTeX brace")
	}
	return out, ""
}
