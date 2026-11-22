package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
)

const (
	// TextLimit is the maximum file size (5 MiB) opened as editable text/Markdown/SVG.
	TextLimit int64 = 5 * 1024 * 1024
	// HighlightLimit is the maximum UTF-8 text length (128 KiB) eligible for syntax highlighting.
	HighlightLimit = 128 * 1024

	lineLimit  = 200 * 1024
	sniffBytes = 4096
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var rasterImageExts = map[string]bool{
	"png":  true,
	"jpg":  true,
	"jpeg": true,
	"gif":  true,
	"webp": true,
	"bmp":  true,
	"ico":  true,
	"avif": true,
}

var knownTextExts = map[string]bool{
	"txt": true, "text": true, "log": true, "csv": true, "tsv": true,
	"toml": true, "ini": true, "cfg": true, "cnf": true, "conf": true, "env": true, "properties": true,
	"sgmodule": true, "sgsnippet": true, "dconf": true, "lpx": true, "plugin": true, "module": true, "qx": true, "snippet": true, "list": true, "stoverride": true,
	"nginx": true, "nginxconf": true, "caddy": true, "caddyfile": true, "sshconfig": true, "xcconfig": true, "hosts": true,
	"service": true, "socket": true, "timer": true, "target": true, "mount": true, "desktop": true, "inf": true,
	"json": true, "jsonc": true, "json5": true, "jsonl": true, "ndjson": true, "xml": true, "svg": true, "xsl": true,
	"html": true, "htm": true, "xhtml": true, "css": true, "scss": true, "less": true,
	"yml": true, "yaml": true,
	"js": true, "jsx": true, "mjs": true, "cjs": true,
	"ts": true, "tsx": true, "mts": true, "cts": true,
	"py": true, "rs": true, "go": true, "java": true,
	"c": true, "h": true, "cc": true, "cpp": true, "hpp": true, "hh": true,
	"cs": true, "rb": true, "php": true, "lua": true, "sql": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "ps1": true, "bat": true, "cmd": true,
	"vue": true, "svelte": true, "astro": true,
	"lock": true, "gradle": true, "mod": true, "sum": true, "work": true, "plist": true, "entitlements": true, "strings": true, "pbxproj": true,
	"rst": true, "tex": true, "latex": true, "ltx": true,
	"lean": true, "mmd": true, "mermaid": true, "bib": true, "graphql": true, "proto": true, "zig": true,
	"kt": true, "kts": true, "swift": true, "dart": true,
	"ex": true, "exs": true, "erl": true, "hs": true, "ml": true, "clj": true, "scala": true,
	"diff": true, "patch": true,
	"gitignore": true, "dockerignore": true, "npmignore": true, "prettierignore": true, "eslintignore": true, "cursorignore": true, "hgignore": true, "ignore": true, "editorconfig": true,
	"npmrc": true, "prettierrc": true, "eslintrc": true, "babelrc": true,
	"markdown": true, "mdx": true,
}

func extOf(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// IsSVGPath reports whether path has an .svg extension (case-insensitive).
func IsSVGPath(path string) bool {
	return extOf(path) == "svg"
}

// IsImagePath reports whether path has a supported raster image or SVG extension.
func IsImagePath(path string) bool {
	ext := extOf(path)
	return ext == "svg" || rasterImageExts[ext]
}

// LanguageTagForPath maps a file path's extension to its canonical language tag.
func LanguageTagForPath(path string) string {
	switch extOf(path) {
	case "csv":
		return "csv"
	case "latex", "math", "tex", "katex":
		return "latex"
	case "markdown", "md":
		return "markdown"
	case "mermaid", "mmd":
		return "mermaid"
	case "bash", "sh", "shell", "zsh", "fish":
		return "bash"
	case "css":
		return "css"
	case "go", "golang":
		return "go"
	case "html", "htm", "xhtml":
		return "html"
	case "javascript", "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "json", "jsonc":
		return "json"
	case "lean", "lean4":
		return "lean"
	case "python", "py":
		return "python"
	case "rust", "rs":
		return "rust"
	case "sql":
		return "sql"
	case "typescript", "ts", "tsx", "mts", "cts":
		return "typescript"
	case "xml", "svg", "xsl":
		return "xml"
	case "yaml", "yml", "yaml-frontmatter", "frontmatter":
		return "yaml"
	default:
		return "plaintext"
	}
}

// ClassifyFile inspects path and classifies how it should be opened in the editor,
// aligning with thinkly-fast/src/files/classify.rs.
func ClassifyFile(path string) (model.ClassifiedFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
	}
	if !info.Mode().IsRegular() {
		return model.ClassifiedFile{
			Kind:   model.OpenKindBlocked,
			Reason: theme.Tr("此文件类型暂不支持", "This file type is not supported"),
		}, nil
	}

	rawSize := info.Size()
	if IsImagePath(path) && !IsSVGPath(path) {
		return model.ClassifiedFile{
			Kind:    model.OpenKindPreview,
			RawSize: rawSize,
		}, nil
	}

	if rawSize > TextLimit {
		return classifyOverLimit(path, rawSize), nil
	}

	data, tooLarge, err := readBounded(path, TextLimit)
	if err != nil {
		return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
	}
	if tooLarge {
		return classifyOverLimit(path, int64(len(data))), nil
	}

	return classifyBytes(path, data), nil
}

func readBounded(path string, capBytes int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, capBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > capBytes {
		return data, true, nil
	}
	return data, false, nil
}

func classifyOverLimit(path string, rawSize int64) model.ClassifiedFile {
	if IsSVGPath(path) {
		return model.ClassifiedFile{
			Kind:    model.OpenKindPreview,
			RawSize: rawSize,
		}
	}
	name := filepath.Base(path)
	sample := sniffHead(path)
	textual := IsMarkdown(name) || knownTextExts[extOf(path)] || sampleIsText(sample)
	reason := theme.Tr("此文件类型暂不支持", "This file type is not supported")
	if textual {
		reason = theme.Tr("文件超过 5 MB", "File is larger than 5 MB")
	}
	return model.ClassifiedFile{
		Kind:    model.OpenKindBlocked,
		Reason:  reason,
		RawSize: rawSize,
	}
}

func sniffHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, sniffBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	return buf[:n]
}

// ClassifyBytes classifies already-read file bytes using the same UTF-8,
// single-line length, and TextLimit rules as ClassifyFile without reopening
// the path from disk.
func ClassifyBytes(path string, data []byte) model.ClassifiedFile {
	rawSize := int64(len(data))
	if rawSize > TextLimit {
		return model.ClassifiedFile{
			Kind:    model.OpenKindBlocked,
			Reason:  theme.Tr("文件超过 5 MiB 编辑上限", "File exceeds the 5 MiB edit limit"),
			RawSize: rawSize,
		}
	}
	return classifyBytes(path, data)
}

func ClassifyNotebookNote(root *os.Root, name string) (model.ClassifiedFile, os.FileInfo, error) {
	expected, err := root.Lstat(name)
	if err != nil {
		return model.ClassifiedFile{}, nil, err
	}
	if !expected.Mode().IsRegular() {
		return model.ClassifiedFile{}, nil, errors.New("notebook note must be a regular file")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return model.ClassifiedFile{}, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return model.ClassifiedFile{}, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return model.ClassifiedFile{}, nil, errors.New("notebook note changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, TextLimit+1))
	if err != nil {
		return model.ClassifiedFile{}, nil, err
	}
	return ClassifyBytes(name, data), info, nil
}

func classifyBytes(path string, data []byte) model.ClassifiedFile {
	rawSize := int64(len(data))
	if IsSVGPath(path) {
		src, errMsg := decodeSource(path, data, true)
		if errMsg != "" {
			return model.ClassifiedFile{
				Kind:    model.OpenKindPreview,
				RawSize: rawSize,
			}
		}
		return model.ClassifiedFile{
			Kind:    model.OpenKindPreview,
			Source:  &src,
			RawSize: rawSize,
		}
	}

	name := filepath.Base(path)
	if IsMarkdown(name) {
		if !utf8.Valid(data) {
			return model.ClassifiedFile{
				Kind:    model.OpenKindBlocked,
				Reason:  theme.Tr("不是有效的 UTF-8", "Not valid UTF-8"),
				RawSize: rawSize,
			}
		}
		text := string(data)
		if lineTooLong(text) {
			return model.ClassifiedFile{
				Kind:    model.OpenKindBlocked,
				Reason:  theme.Tr("单行过长，无法预览", "A line is too long to preview"),
				RawSize: rawSize,
			}
		}
		return model.ClassifiedFile{
			Kind:    model.OpenKindMarkdown,
			Text:    text,
			RawSize: rawSize,
		}
	}

	src, errMsg := decodeSource(path, data, false)
	if errMsg == "" {
		return model.ClassifiedFile{
			Kind:    model.OpenKindSource,
			Source:  &src,
			RawSize: rawSize,
		}
	}
	if knownTextExts[extOf(path)] {
		return model.ClassifiedFile{
			Kind:    model.OpenKindBlocked,
			Reason:  errMsg,
			RawSize: rawSize,
		}
	}
	return model.ClassifiedFile{
		Kind:    model.OpenKindBlocked,
		Reason:  theme.Tr("此文件类型暂不支持", "This file type is not supported"),
		RawSize: rawSize,
	}
}

func decodeSource(path string, data []byte, svg bool) (model.SourceFile, string) {
	if !svg && IsBinaryPlist(data) {
		xmlText, err := DecodeBinaryPlist(data)
		if err != nil {
			return model.SourceFile{}, theme.Tr("无法解析二进制 Plist 文件", "Failed to decode binary plist")
		}
		if int64(len(xmlText)) > TextLimit {
			return model.SourceFile{}, theme.Tr("文件超过 5 MB", "File is larger than 5 MB")
		}
		if lineTooLong(xmlText) {
			return model.SourceFile{}, theme.Tr("单行过长，无法预览", "A line is too long to preview")
		}
		lang := LanguageTagForPath(path)
		if lang == "plaintext" {
			lang = "xml"
		}
		return model.SourceFile{
			Text:            xmlText,
			BinaryPlist:     true,
			DiskFingerprint: fingerprintBytes(data),
			Language:        lang,
			Highlight:       len(xmlText) <= HighlightLimit,
		}, ""
	}
	bom := false
	rest := data
	if bytes.HasPrefix(data, utf8BOM) {
		bom = true
		rest = data[len(utf8BOM):]
	}
	if hasBinaryControls(rest) {
		return model.SourceFile{}, theme.Tr("包含二进制内容", "File contains binary data")
	}
	if !utf8.Valid(rest) {
		return model.SourceFile{}, theme.Tr("不是有效的 UTF-8", "Not valid UTF-8")
	}
	text := string(rest)
	if lineTooLong(text) {
		return model.SourceFile{}, theme.Tr("单行过长，无法预览", "A line is too long to preview")
	}
	ext := extOf(path)
	return model.SourceFile{
		Text:            text,
		BOM:             bom,
		DiskFingerprint: fingerprintBytes(data),
		Language:        LanguageTagForPath(path),
		Highlight:       len(text) <= HighlightLimit,
		HTML:            ext == "html" || ext == "htm" || ext == "xhtml",
		SVG:             svg,
	}, ""
}

func lineTooLong(text string) bool {
	start := 0
	for {
		idx := strings.IndexByte(text[start:], '\n')
		if idx < 0 {
			return len(text)-start > lineLimit
		}
		if idx > lineLimit {
			return true
		}
		start += idx + 1
	}
}

func hasBinaryControls(data []byte) bool {
	for _, b := range data {
		if b == 0 || (b < 0x20 && b != '\t' && b != '\n' && b != '\r') {
			return true
		}
	}
	return false
}

func sampleIsText(data []byte) bool {
	end := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] < 0x80 {
			end = i
			break
		}
	}
	sample := data
	if end > 0 && len(data) > 0 {
		sample = data[:end+1]
	}
	return utf8.Valid(sample) && !hasBinaryControls(sample)
}
