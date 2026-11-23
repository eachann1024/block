package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// PageMenuStat represents one key-value metadata row at the top of the titlebar page menu.
type PageMenuStat struct {
	ID    string
	Label string
	Value string
}

// PageMenuStats builds metadata rows for non-Markdown tabs shown at the top of the page menu.
func (s *TabSurface) PageMenuStats() []PageMenuStat {
	if s == nil {
		return nil
	}
	switch s.TitlebarSurface() {
	case SurfaceImagePreview:
		var stats []PageMenuStat
		s.imgMu.Lock()
		imgSrc := s.ImgSrc
		s.imgMu.Unlock()
		if imgSrc != nil {
			if iw, ih := ui.ImageSize(imgSrc); iw > 0 && ih > 0 {
				stats = append(stats, PageMenuStat{
					ID:    "page-stat-dimensions",
					Label: theme.Tr("尺寸", "Dimensions"),
					Value: fmt.Sprintf("%d × %d", int(iw+0.5), int(ih+0.5)),
				})
			}
		}
		if s.FileSize > 0 {
			stats = append(stats, PageMenuStat{
				ID:    "page-stat-size",
				Label: theme.Tr("大小", "Size"),
				Value: formatFileBytes(s.FileSize),
			})
		}
		if ext := upperFileExt(s.Path); ext != "" {
			stats = append(stats, PageMenuStat{
				ID:    "page-stat-format",
				Label: theme.Tr("格式", "Format"),
				Value: ext,
			})
		}
		return stats

	case SurfaceBlocked:
		var stats []PageMenuStat
		if s.FileSize > 0 {
			stats = append(stats, PageMenuStat{
				ID:    "page-stat-size",
				Label: theme.Tr("大小", "Size"),
				Value: formatFileBytes(s.FileSize),
			})
		}
		fmtVal := upperFileExt(s.Path)
		if fmtVal == "" {
			fmtVal = theme.Tr("二进制", "Binary")
		}
		stats = append(stats, PageMenuStat{
			ID:    "page-stat-format",
			Label: theme.Tr("格式", "Format"),
			Value: fmtVal,
		})
		return stats

	case SurfaceSVG:
		var stats []PageMenuStat
		if s.SVGSrc != nil {
			if iw, ih := ui.ImageSize(s.SVGSrc); iw > 0 && ih > 0 {
				stats = append(stats, PageMenuStat{
					ID:    "page-stat-dimensions",
					Label: theme.Tr("尺寸", "Dimensions"),
					Value: fmt.Sprintf("%d × %d", int(iw+0.5), int(ih+0.5)),
				})
			}
		}
		if text := s.CurrentText(); text != "" || s.HasSource {
			stats = append(stats,
				PageMenuStat{
					ID:    "page-stat-lines",
					Label: theme.Tr("行数", "Lines"),
					Value: strconv.Itoa(countTextLines(text)),
				},
				PageMenuStat{
					ID:    "page-stat-characters",
					Label: theme.Tr("字符数", "Characters"),
					Value: strconv.Itoa(utf8.RuneCountInString(text)),
				},
			)
		}
		formatVal := "SVG · UTF-8"
		if s.Source.BOM {
			formatVal = "SVG · UTF-8 BOM"
		} else if !s.HasSource {
			formatVal = theme.Tr("SVG · 只读", "SVG · Read-only")
		}
		stats = append(stats, PageMenuStat{
			ID:    "page-stat-format",
			Label: theme.Tr("格式", "Format"),
			Value: formatVal,
		})
		return stats

	default: // SurfaceCode, SurfaceHTML
		text := s.CurrentText()
		return []PageMenuStat{
			{
				ID:    "page-stat-lines",
				Label: theme.Tr("行数", "Lines"),
				Value: strconv.Itoa(countTextLines(text)),
			},
			{
				ID:    "page-stat-characters",
				Label: theme.Tr("字符数", "Characters"),
				Value: strconv.Itoa(utf8.RuneCountInString(text)),
			},
			{
				ID:    "page-stat-format",
				Label: theme.Tr("格式", "Format"),
				Value: s.sourceFormatLabel(),
			},
		}
	}
}

// CurrentText returns the latest in-memory text for a text-backed surface.
func (s *TabSurface) CurrentText() string {
	if s == nil {
		return ""
	}
	if s.code != nil && s.code.Buffer != nil {
		var sb strings.Builder
		_, _ = s.code.Buffer.WriteTo(&sb)
		return sb.String()
	}
	return s.Source.Text
}

// HasCopyableText reports whether this surface has text content that can be copied.
func (s *TabSurface) HasCopyableText() bool {
	if s == nil {
		return false
	}
	return s.HasSource || (s.IsSVG && s.Source.Text != "")
}

// CanFormatJSON reports whether this surface edits a JSON file.
func (s *TabSurface) CanFormatJSON() bool {
	if !s.IsEditableSource() {
		return false
	}
	return strings.EqualFold(filepath.Ext(s.Path), ".json")
}

// FormatJSON pretty-prints a JSON source surface in place with undo support and saves it.
func (s *TabSurface) FormatJSON() (changed bool, err error) {
	if !s.CanFormatJSON() {
		return false, errors.New(theme.Tr("当前文件不支持 JSON 格式化", "Current file is not an editable JSON file"))
	}
	raw := s.CurrentText()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return false, err
	}
	formatted := buf.String()
	if strings.HasSuffix(raw, "\r\n") {
		formatted = strings.ReplaceAll(formatted, "\n", "\r\n") + "\r\n"
	} else if strings.HasSuffix(raw, "\n") {
		formatted += "\n"
	}
	if formatted == raw {
		return false, nil
	}
	st := s.ensureCodeState()
	st.Buffer.Set(formatted)
	s.codeVersion = st.Buffer.Version()
	s.Source.Text = formatted
	if err := s.saveSource(); err != nil {
		return true, err
	}
	return true, nil
}

// MarkdownCodeFence wraps the surface's source text in a safe fenced Markdown code block.
func (s *TabSurface) MarkdownCodeFence() string {
	if s == nil {
		return ""
	}
	text := s.CurrentText()
	lang := s.fenceLanguageTag()
	fence := strings.Repeat("`", max(3, maxConsecutiveBackticks(text)+1))
	body := strings.TrimRight(text, "\r\n")
	if body == "" {
		return fence + lang + "\n" + fence
	}
	return fence + lang + "\n" + body + "\n" + fence
}

func (s *TabSurface) fenceLanguageTag() string {
	if s.IsSVG {
		return "svg"
	}
	if tag := strings.ToLower(strings.TrimSpace(s.Source.Language)); tag != "" && tag != "text" && tag != "plain" {
		return tag
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(s.Path), "."))
	return ext
}

func (s *TabSurface) sourceFormatLabel() string {
	lang := strings.ToUpper(strings.TrimSpace(s.Source.Language))
	if s.IsSVG {
		lang = "SVG"
	} else if lang == "" {
		if ext := upperFileExt(s.Path); ext != "" {
			lang = ext
		} else {
			lang = "TEXT"
		}
	}
	enc := "UTF-8"
	if s.Source.BinaryPlist {
		enc = theme.Tr("二进制 Plist", "Binary Plist")
	} else if s.Source.Encoding == model.TextEncodingUTF16LE {
		enc = "UTF-16 LE"
	} else if s.Source.Encoding == model.TextEncodingUTF16BE {
		enc = "UTF-16 BE"
	} else if s.Source.BOM {
		enc = "UTF-8 BOM"
	}
	return fmt.Sprintf("%s · %s", lang, enc)
}

func countTextLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") && n > 1 {
		n--
	}
	return n
}

func upperFileExt(path string) string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
}

func formatFileBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
	}
}

func maxConsecutiveBackticks(s string) int {
	best, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 0
		}
	}
	return best
}

// markdownImageRef builds a clean Markdown image reference for filePath, using a
// relative path when filePath resides inside notebookDir.
func markdownImageRef(filePath, notebookDir string) string {
	base := filepath.Base(filePath)
	alt := strings.TrimSuffix(base, filepath.Ext(base))
	target := base
	if notebookDir != "" {
		if rel, err := filepath.Rel(notebookDir, filePath); err == nil && rel != "" && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			target = filepath.ToSlash(rel)
		} else {
			target = filepath.ToSlash(filePath)
		}
	} else {
		target = filepath.ToSlash(filePath)
	}
	if strings.ContainsAny(target, " ()") {
		target = "<" + target + ">"
	}
	return fmt.Sprintf("![%s](%s)", alt, target)
}

// copyImageFileToClipboard encodes path as PNG (passing PNG files through verbatim)
// and writes it to the system clipboard.
func copyImageFileToClipboard(path string, svc ui.Services) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pngBytes := raw
	if !bytes.HasPrefix(raw, pngSignature) {
		img, _, decErr := image.Decode(bytes.NewReader(raw))
		if decErr != nil {
			return decErr
		}
		var out bytes.Buffer
		if encErr := png.Encode(&out, img); encErr != nil {
			return encErr
		}
		pngBytes = out.Bytes()
	}
	data := transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBytes)))
	if mygo.App.IsReady() {
		return mygo.Clipboard.Write(data)
	}
	svc.WriteClipboardData(data)
	return nil
}
