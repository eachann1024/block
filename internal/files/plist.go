package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"block/internal/theme"
	"howett.net/plist"
)

var binaryPlistMagic = []byte("bplist00")

// IsBinaryPlist reports whether data begins with the Apple binary property list
// header ("bplist00").
func IsBinaryPlist(data []byte) bool {
	return len(data) >= len(binaryPlistMagic) && bytes.HasPrefix(data, binaryPlistMagic)
}

// IsBinaryPlistFile reports whether path on disk begins with the Apple binary
// property list header.
func IsBinaryPlistFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	var head [8]byte
	n, err := io.ReadFull(f, head[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}
	return IsBinaryPlist(head[:n])
}

// DecodeBinaryPlist decodes an Apple binary property list (bplist00) into
// indented, human-readable XML property list text.
func DecodeBinaryPlist(data []byte) (string, error) {
	if !IsBinaryPlist(data) {
		return "", errors.New(theme.Tr("不是二进制 Plist 文件", "Not a binary property list"))
	}
	var val any
	format, err := plist.Unmarshal(data, &val)
	if err != nil {
		return "", err
	}
	if format != plist.BinaryFormat {
		return "", errors.New(theme.Tr("不是二进制 Plist 文件", "Not a binary property list"))
	}
	xmlBytes, err := plist.MarshalIndent(val, plist.XMLFormat, "\t")
	if err != nil {
		return "", err
	}
	text := wrapPlistDataTags(string(xmlBytes))
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}

// EncodeBinaryPlist validates XML property list text and encodes it back into
// Apple binary property list (bplist00) bytes.
func EncodeBinaryPlist(xmlText string) ([]byte, error) {
	var val any
	format, err := plist.Unmarshal([]byte(xmlText), &val)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", theme.Tr("Plist XML 格式无效", "Invalid plist XML"), err)
	}
	if format != plist.XMLFormat {
		return nil, errors.New(theme.Tr("Plist XML 格式无效，须为 XML 属性列表", "Invalid plist XML: must be an XML property list"))
	}
	if val == nil {
		return nil, errors.New(theme.Tr("Plist 内容为空", "Empty plist content"))
	}
	out, err := plist.Marshal(val, plist.BinaryFormat)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", theme.Tr("二进制 Plist 编码失败", "Failed to encode binary plist"), err)
	}
	return out, nil
}

// WriteBinaryPlistAtomic encodes xmlText as a binary property list and
// atomically writes it to path, returning the new disk fingerprint and byte size.
func WriteBinaryPlistAtomic(path, xmlText string) (int64, int64, error) {
	data, err := EncodeBinaryPlist(xmlText)
	if err != nil {
		return 0, 0, err
	}
	fp, err := WriteBytesAtomic(path, data)
	if err != nil {
		return 0, 0, err
	}
	return fp, int64(len(data)), nil
}

// wrapPlistDataTags wraps long single-line base64 <data>...</data> elements to
// 68 columns (matching macOS plutil -convert xml1) for readability and to avoid
// tripping the editor's per-line length guard on large embedded blobs.
func wrapPlistDataTags(xmlText string) string {
	const maxChunk = 68
	if !strings.Contains(xmlText, "<data>") {
		return xmlText
	}
	lines := strings.Split(xmlText, "\n")
	var out strings.Builder
	out.Grow(len(xmlText) + 64)
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		trimmed := strings.TrimLeft(line, "\t ")
		if !strings.HasPrefix(trimmed, "<data>") || !strings.HasSuffix(trimmed, "</data>") {
			out.WriteString(line)
			continue
		}
		body := trimmed[len("<data>") : len(trimmed)-len("</data>")]
		if len(body) <= maxChunk {
			out.WriteString(line)
			continue
		}
		indent := line[:len(line)-len(trimmed)]
		out.WriteString(indent)
		out.WriteString("<data>\n")
		for start := 0; start < len(body); start += maxChunk {
			end := min(start+maxChunk, len(body))
			out.WriteString(indent)
			out.WriteString(body[start:end])
			out.WriteByte('\n')
		}
		out.WriteString(indent)
		out.WriteString("</data>")
	}
	return out.String()
}
