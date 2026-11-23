package files

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
)

const (
	// TextLimit is the maximum file size (5 MiB) opened as editable text/Markdown/SVG.
	TextLimit int64 = 5 * 1024 * 1024
	// PreviewLimit is the maximum file size (16 MiB) read for image/SVG previews.
	PreviewLimit int64 = 16 * 1024 * 1024
	// HighlightLimit is the maximum UTF-8 text length (128 KiB) eligible for syntax highlighting.
	HighlightLimit = 128 * 1024

	lineLimit  = 200 * 1024
	sniffBytes = 4096
)

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

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
	"htaccess": true, "apacheconf": true, "vhost": true, "haproxy": true, "cron": true, "crontab": true, "procfile": true, "pip": true,
	"service": true, "socket": true, "timer": true, "target": true, "mount": true, "desktop": true, "inf": true,
	"json": true, "jsonc": true, "json5": true, "jsonl": true, "ndjson": true, "xml": true, "svg": true, "xsl": true,
	"html": true, "htm": true, "xhtml": true, "css": true, "scss": true, "less": true,
	"hbs": true, "handlebars": true, "mustache": true, "hjs": true, "ejs": true,
	"tmpl": true, "gotmpl": true, "gohtml": true, "tpl": true,
	"http": true, "rest": true, "prisma": true, "thrift": true,
	"yml": true, "yaml": true,
	"js": true, "jsx": true, "mjs": true, "cjs": true,
	"ts": true, "tsx": true, "mts": true, "cts": true,
	"py": true, "rs": true, "go": true, "java": true,
	"c": true, "h": true, "cc": true, "cpp": true, "hpp": true, "hh": true,
	"cs": true, "rb": true, "php": true, "lua": true, "sql": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "ps1": true, "psm1": true, "psd1": true, "ps1xml": true, "bat": true, "cmd": true,
	"vue": true, "svelte": true, "astro": true,
	"lock": true, "gradle": true, "groovy": true, "gvy": true, "gy": true, "gsh": true, "cmake": true, "just": true,
	"mod": true, "sum": true, "work": true, "plist": true, "entitlements": true, "strings": true, "pbxproj": true,
	"rst": true, "tex": true, "latex": true, "ltx": true,
	"lean": true, "mmd": true, "mermaid": true, "bib": true, "graphql": true, "proto": true, "zig": true,
	"kt": true, "kts": true, "swift": true, "dart": true,
	"ex": true, "exs": true, "heex": true, "leex": true, "eex": true, "erl": true, "hs": true, "ml": true, "clj": true, "scala": true,
	"diff": true, "patch": true,
	"gitignore": true, "dockerignore": true, "npmignore": true, "prettierignore": true, "eslintignore": true, "cursorignore": true, "hgignore": true, "ignore": true, "editorconfig": true,
	"npmrc": true, "prettierrc": true, "eslintrc": true, "babelrc": true,
	"markdown": true, "mdx": true,
}

func extOf(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// KnownTextExtensions returns the sorted dot-prefixed text, config, and code
// file extensions recognized by Block.
func KnownTextExtensions() []string {
	out := make([]string, 0, len(knownTextExts))
	for ext := range knownTextExts {
		out = append(out, "."+ext)
	}
	sort.Strings(out)
	return out
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
	case "xml", "svg", "xsl", "ps1xml":
		return "xml"
	case "yaml", "yml", "yaml-frontmatter", "frontmatter":
		return "yaml"
	default:
		return "plaintext"
	}
}

// ErrNotRegularFile is returned when a non-regular filesystem node (FIFO, socket,
// device, or directory) is encountered where a regular file is required.
var ErrNotRegularFile = errors.New("not a regular file")

// OpenRegularNonBlockNoFollow opens path without blocking on FIFOs and without
// following a final-component symlink (on Unix), verifying regular-file mode on
// the opened descriptor.
func OpenRegularNonBlockNoFollow(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNonBlockFlag|oNoFollowFlag, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, ErrNotRegularFile
	}
	return f, info, nil
}

// OpenRootRegularNonBlockNoFollow opens rel strictly inside root without blocking
// on FIFOs and without following a final-component symlink (on Unix), verifying
// regular-file mode on the opened descriptor.
func OpenRootRegularNonBlockNoFollow(root *os.Root, rel string) (*os.File, os.FileInfo, error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|oNonBlockFlag|oNoFollowFlag, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, ErrNotRegularFile
	}
	return f, info, nil
}

// ReadBoundedRegular reads up to capBytes from a regular file using O_NONBLOCK
// and descriptor-level mode verification so FIFOs/devices never block.
func ReadBoundedRegular(path string, capBytes int64) ([]byte, bool, error) {
	return readBounded(path, capBytes)
}

// RootDirIdentityToken returns the FileInfo and stable cross-restart identity token
// of the directory bound to root by opening "." directly through root.
func RootDirIdentityToken(root *os.Root) (os.FileInfo, string, error) {
	if root == nil {
		return nil, "", errors.New("nil root")
	}
	d, err := root.Open(".")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = d.Close() }()
	info, err := d.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", errors.New("root is not a directory")
	}
	return info, FileIdentityToken(info, d), nil
}

// FileIdentityMatches checks whether info/f matches a previously recorded identity token.
func FileIdentityMatches(info os.FileInfo, f *os.File, token string) bool {
	if token == "" {
		return false
	}
	got := FileIdentityToken(info, f)
	return got != "" && got == token
}

func verifyNoSymlinkPathInRoot(root *os.Root, cleanRel string) error {
	parts := strings.Split(filepath.ToSlash(cleanRel), "/")
	for i := range parts {
		prefix := filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		info, err := root.Lstat(prefix)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrNotRegularFile
		}
	}
	return nil
}

// ClassifyFile inspects path and classifies how it should be opened in the editor,
// aligning with thinkly-fast/src/files/classify.rs.
// Opens once with O_NONBLOCK and verifies regular-file mode on the opened descriptor
// so a TOCTOU swap to a FIFO/device never blocks the UI thread.
func ClassifyFile(path string) (model.ClassifiedFile, error) {
	cf, _, _, err := ClassifyFileWithInfo(path)
	return cf, err
}

// ClassifyFileWithInfo classifies path from a single non-blocking open descriptor
// and also returns the opened file's FileInfo and stable cross-restart identity token.
func ClassifyFileWithInfo(path string) (model.ClassifiedFile, os.FileInfo, string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return model.ClassifiedFile{}, nil, "", fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
	}
	id := FileIdentityToken(info, f)
	cf, err := ClassifyFileHandle(path, f, info)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", err
	}
	return cf, info, id, nil
}

// ClassifyFileInRoot opens and classifies confineRel strictly inside confineRoot via os.Root,
// verifying both the root directory identity and the target regular file identity.
func ClassifyFileInRoot(confineRoot, confineRel string, expectedRootInfo os.FileInfo, expectedRootID string, expectedFileInfo os.FileInfo, expectedFileID string) (cf model.ClassifiedFile, fInfo os.FileInfo, fID string, rInfo os.FileInfo, rID string, err error) {
	if strings.TrimSpace(confineRoot) == "" {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("无效的受限目录", "Invalid confined directory"))
	}
	cleanRel := filepath.Clean(confineRel)
	if cleanRel == "." || !filepath.IsLocal(cleanRel) {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("目标路径超出允许范围", "Target path escapes allowed directory"))
	}
	root, err := os.OpenRoot(confineRoot)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	defer func() { _ = root.Close() }()

	rInfo, rID, err = RootDirIdentityToken(root)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	if expectedRootInfo != nil && !os.SameFile(rInfo, expectedRootInfo) {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("受限目录身份已变化", "Confined root directory identity changed"))
	}
	if expectedRootID != "" && rID != expectedRootID {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("受限目录身份已变化", "Confined root directory identity changed"))
	}
	if err = verifyNoSymlinkPathInRoot(root, cleanRel); err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	linfo, err := root.Lstat(cleanRel)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	if !linfo.Mode().IsRegular() {
		return model.ClassifiedFile{}, nil, "", nil, "", ErrNotRegularFile
	}
	f, fInfo, err := OpenRootRegularNonBlockNoFollow(root, cleanRel)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	defer func() { _ = f.Close() }()

	if !os.SameFile(linfo, fInfo) {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("目标文件身份已变化", "Target file identity changed"))
	}
	fID = FileIdentityToken(fInfo, f)
	if expectedFileInfo != nil && !os.SameFile(fInfo, expectedFileInfo) {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("目标文件身份已变化", "Target file identity changed"))
	}
	if expectedFileID != "" && fID != expectedFileID {
		return model.ClassifiedFile{}, nil, "", nil, "", errors.New(theme.Tr("目标文件身份已变化", "Target file identity changed"))
	}
	absPath := filepath.Clean(filepath.Join(confineRoot, cleanRel))
	cf, err = ClassifyFileHandle(absPath, f, fInfo)
	if err != nil {
		return model.ClassifiedFile{}, nil, "", nil, "", err
	}
	return cf, fInfo, fID, rInfo, rID, nil
}

// ClassifyFileHandle classifies an already-opened file descriptor without re-opening
// path from disk, reading bounded preview/source bytes directly from f.
func ClassifyFileHandle(path string, f *os.File, info os.FileInfo) (model.ClassifiedFile, error) {
	if f == nil || info == nil {
		return model.ClassifiedFile{}, errors.New("missing file handle")
	}
	if !info.Mode().IsRegular() {
		return model.ClassifiedFile{
			Kind:   model.OpenKindBlocked,
			Reason: theme.Tr("此文件类型暂不支持", "This file type is not supported"),
		}, nil
	}

	rawSize := info.Size()
	if IsImagePath(path) && !IsSVGPath(path) {
		if rawSize > PreviewLimit {
			return model.ClassifiedFile{
				Kind:    model.OpenKindBlocked,
				Reason:  theme.Tr("文件超过预览大小上限", "File exceeds preview size limit"),
				RawSize: rawSize,
			}, nil
		}
		data, err := io.ReadAll(io.LimitReader(f, PreviewLimit+1))
		if err != nil {
			return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
		}
		if int64(len(data)) > PreviewLimit {
			return model.ClassifiedFile{
				Kind:    model.OpenKindBlocked,
				Reason:  theme.Tr("文件超过预览大小上限", "File exceeds preview size limit"),
				RawSize: int64(len(data)),
			}, nil
		}
		return model.ClassifiedFile{
			Kind:         model.OpenKindPreview,
			PreviewBytes: data,
			RawSize:      int64(len(data)),
		}, nil
	}

	if rawSize > TextLimit {
		if IsSVGPath(path) {
			if rawSize > PreviewLimit {
				return model.ClassifiedFile{
					Kind:    model.OpenKindBlocked,
					Reason:  theme.Tr("文件超过预览大小上限", "File exceeds preview size limit"),
					RawSize: rawSize,
				}, nil
			}
			data, err := io.ReadAll(io.LimitReader(f, PreviewLimit+1))
			if err != nil {
				return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
			}
			if int64(len(data)) > PreviewLimit {
				return model.ClassifiedFile{
					Kind:    model.OpenKindBlocked,
					Reason:  theme.Tr("文件超过预览大小上限", "File exceeds preview size limit"),
					RawSize: int64(len(data)),
				}, nil
			}
			return model.ClassifiedFile{
				Kind:         model.OpenKindPreview,
				PreviewBytes: data,
				RawSize:      int64(len(data)),
			}, nil
		}
		var sample []byte
		buf := make([]byte, sniffBytes)
		if n, rErr := f.Read(buf); n > 0 && (rErr == nil || errors.Is(rErr, io.EOF)) {
			sample = buf[:n]
		}
		return classifyOverLimitSample(path, rawSize, sample), nil
	}

	if IsSVGPath(path) {
		data, err := io.ReadAll(io.LimitReader(f, PreviewLimit+1))
		if err != nil {
			return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
		}
		if int64(len(data)) > PreviewLimit {
			return model.ClassifiedFile{
				Kind:    model.OpenKindBlocked,
				Reason:  theme.Tr("文件超过预览大小上限", "File exceeds preview size limit"),
				RawSize: int64(len(data)),
			}, nil
		}
		if int64(len(data)) > TextLimit {
			return model.ClassifiedFile{
				Kind:         model.OpenKindPreview,
				PreviewBytes: data,
				RawSize:      int64(len(data)),
			}, nil
		}
		return classifyBytes(path, data), nil
	}

	data, err := io.ReadAll(io.LimitReader(f, TextLimit+1))
	if err != nil {
		return model.ClassifiedFile{}, fmt.Errorf("%s: %w", theme.Tr("无法读取", "Cannot read"), err)
	}
	if int64(len(data)) > TextLimit {
		sample := data
		if len(sample) > sniffBytes {
			sample = sample[:sniffBytes]
		}
		return classifyOverLimitSample(path, int64(len(data)), sample), nil
	}

	return classifyBytes(path, data), nil
}

func readBounded(path string, capBytes int64) ([]byte, bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("not a regular file")
	}

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
	return classifyOverLimitSample(path, rawSize, sniffHead(path))
}

func classifyOverLimitSample(path string, rawSize int64, sample []byte) model.ClassifiedFile {
	if IsSVGPath(path) {
		return model.ClassifiedFile{
			Kind:    model.OpenKindPreview,
			RawSize: rawSize,
		}
	}
	name := filepath.Base(path)
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
	f, err := os.OpenFile(path, os.O_RDONLY|oNonBlockFlag, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}

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
				Kind:         model.OpenKindPreview,
				PreviewBytes: data,
				RawSize:      rawSize,
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
		if ExceedsTextSizeLimit(int64(len(data)), int64(len(xmlText))) {
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
	enc := model.TextEncodingUTF8
	rest := data
	var text string
	if utf16Text, utf16Enc, hasUTF16BOM, utf16Err := decodeUTF16BOM(data); hasUTF16BOM {
		if utf16Err != "" {
			return model.SourceFile{}, utf16Err
		}
		if ExceedsTextSizeLimit(int64(len(data)), int64(len(utf16Text))) {
			return model.SourceFile{}, theme.Tr("文件超过 5 MB", "File is larger than 5 MB")
		}
		bom = true
		enc = utf16Enc
		text = utf16Text
	} else {
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
		text = string(rest)
	}
	if lineTooLong(text) {
		return model.SourceFile{}, theme.Tr("单行过长，无法预览", "A line is too long to preview")
	}
	ext := extOf(path)
	return model.SourceFile{
		Text:            text,
		BOM:             bom,
		Encoding:        enc,
		DiskFingerprint: fingerprintBytes(data),
		Language:        LanguageTagForPath(path),
		Highlight:       len(text) <= HighlightLimit,
		HTML:            ext == "html" || ext == "htm" || ext == "xhtml",
		SVG:             svg,
	}, ""
}

// decodeUTF16BOM decodes UTF-16 LE (0xFF 0xFE) or UTF-16 BE (0xFE 0xFF) text
// with strict surrogate validation and binary-control rejection.
func decodeUTF16BOM(data []byte) (string, model.TextEncoding, bool, string) {
	var enc model.TextEncoding
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(data, utf16LEBOM):
		// Reject UTF-32 LE BOM (0xFF 0xFE 0x00 0x00) explicitly as binary.
		if len(data) >= 4 && data[2] == 0 && data[3] == 0 {
			return "", model.TextEncodingUTF8, true, theme.Tr("包含二进制内容", "File contains binary data")
		}
		enc = model.TextEncodingUTF16LE
		order = binary.LittleEndian
	case bytes.HasPrefix(data, utf16BEBOM):
		enc = model.TextEncodingUTF16BE
		order = binary.BigEndian
	default:
		return "", model.TextEncodingUTF8, false, ""
	}
	rest := data[2:]
	if len(rest)%2 != 0 {
		return "", enc, true, theme.Tr("不是有效的 UTF-16", "Not valid UTF-16")
	}
	units := make([]uint16, len(rest)/2)
	for i := range units {
		units[i] = order.Uint16(rest[i*2:])
	}
	var b strings.Builder
	b.Grow(len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		var r rune
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= len(units) {
				return "", enc, true, theme.Tr("不是有效的 UTF-16", "Not valid UTF-16")
			}
			low := units[i+1]
			if low < 0xDC00 || low > 0xDFFF {
				return "", enc, true, theme.Tr("不是有效的 UTF-16", "Not valid UTF-16")
			}
			r = utf16.DecodeRune(rune(u), rune(low))
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return "", enc, true, theme.Tr("不是有效的 UTF-16", "Not valid UTF-16")
		default:
			r = rune(u)
		}
		if r == 0 || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return "", enc, true, theme.Tr("包含二进制内容", "File contains binary data")
		}
		b.WriteRune(r)
	}
	return b.String(), enc, true, ""
}

// ExceedsTextSizeLimit reports whether either the raw on-disk encoded byte size
// or the decoded UTF-8 text byte size exceeds TextLimit (5 MiB). Both open and
// save paths share this invariant so saved content can always be reopened.
func ExceedsTextSizeLimit(rawBytes, decodedBytes int64) bool {
	return rawBytes > TextLimit || decodedBytes > TextLimit
}

// EncodedSourceByteLen returns the exact byte size that EncodeSourceText would
// produce for (text, bom, enc) without allocating the encoded buffer.
func EncodedSourceByteLen(text string, bom bool, enc model.TextEncoding) int64 {
	switch enc {
	case model.TextEncodingUTF16LE, model.TextEncodingUTF16BE:
		var total int64 = 2 // UTF-16 BOM
		for _, r := range text {
			if r >= 0x10000 && r <= 0x10FFFF {
				total += 4
			} else {
				total += 2
			}
		}
		return total
	default:
		total := int64(len(text))
		if bom {
			total += int64(len(utf8BOM))
		}
		return total
	}
}

// EncodeSourceText serializes UTF-8 text back to disk bytes preserving the
// original BOM and UTF-8 / UTF-16 LE / UTF-16 BE encoding.
func EncodeSourceText(text string, bom bool, enc model.TextEncoding) []byte {
	switch enc {
	case model.TextEncodingUTF16LE, model.TextEncodingUTF16BE:
		units := utf16.Encode([]rune(text))
		out := make([]byte, 2+len(units)*2)
		var order binary.ByteOrder
		if enc == model.TextEncodingUTF16LE {
			copy(out, utf16LEBOM)
			order = binary.LittleEndian
		} else {
			copy(out, utf16BEBOM)
			order = binary.BigEndian
		}
		for i, u := range units {
			order.PutUint16(out[2+i*2:], u)
		}
		return out
	default:
		if bom {
			out := make([]byte, len(utf8BOM)+len(text))
			copy(out, utf8BOM)
			copy(out[len(utf8BOM):], text)
			return out
		}
		return []byte(text)
	}
}

// DetectFileTextEncoding inspects the leading BOM bytes of path on disk so
// out-of-band saves preserve UTF-8 BOM and UTF-16 LE/BE encodings.
func DetectFileTextEncoding(path string) (bool, model.TextEncoding) {
	f, err := os.Open(path)
	if err != nil {
		return false, model.TextEncodingUTF8
	}
	defer func() { _ = f.Close() }()
	var head [4]byte
	n, err := f.Read(head[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return false, model.TextEncodingUTF8
	}
	b := head[:n]
	if bytes.HasPrefix(b, utf8BOM) {
		return true, model.TextEncodingUTF8
	}
	if bytes.HasPrefix(b, utf16LEBOM) && !(len(b) >= 4 && b[2] == 0 && b[3] == 0) {
		return true, model.TextEncodingUTF16LE
	}
	if bytes.HasPrefix(b, utf16BEBOM) {
		return true, model.TextEncodingUTF16BE
	}
	return false, model.TextEncodingUTF8
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
	if bytes.HasPrefix(data, utf16LEBOM) || bytes.HasPrefix(data, utf16BEBOM) {
		sample := data
		if len(sample) > 2 && len(sample)%2 != 0 {
			sample = sample[:len(sample)-1]
		}
		// Drop a trailing high surrogate if sniffBytes truncated a surrogate pair.
		if len(sample) >= 4 {
			var last uint16
			if bytes.HasPrefix(data, utf16LEBOM) {
				last = binary.LittleEndian.Uint16(sample[len(sample)-2:])
			} else {
				last = binary.BigEndian.Uint16(sample[len(sample)-2:])
			}
			if last >= 0xD800 && last <= 0xDBFF {
				sample = sample[:len(sample)-2]
			}
		}
		_, _, hasBOM, errMsg := decodeUTF16BOM(sample)
		return hasBOM && errMsg == ""
	}
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
