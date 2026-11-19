package editor

import (
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"

	"block/internal/media"

	"github.com/egoist/mygo/ui"
)

func loadLocalImage(dir, src string) (ui.ImageSource, error) {
	f, _, err := media.OpenLocal(dir, src, media.MaxImageBytes)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := media.ReadFile(f, media.MaxImageBytes)
	if err != nil {
		return nil, err
	}
	if _, _, err := media.ImageConfig(raw); err == nil {
		return ui.DecodeBitmap(raw)
	}
	if strings.EqualFold(filepath.Ext(f.Name()), ".svg") {
		return parseLocalSVG(raw)
	}
	return nil, media.ErrInvalid
}

// parseLocalSVG keeps local SVG editing in the native shape renderer. SVGs are
// excluded from portable raster exports and clipboard image representations.
func parseLocalSVG(raw []byte) (*ui.SVG, error) {
	const maxSVGBytes = 1 << 20
	if len(raw) == 0 || len(raw) > maxSVGBytes {
		return nil, media.ErrInvalid
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, nodes := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok := token.(type) {
		case xml.Directive:
			return nil, media.ErrInvalid
		case xml.StartElement:
			if depth == 0 && (nodes != 0 || tok.Name.Local != "svg") {
				return nil, media.ErrInvalid
			}
			depth++
			nodes++
			if depth > 64 || nodes > 16_384 {
				return nil, media.ErrInvalid
			}
			for _, attr := range tok.Attr {
				if attr.Name.Local == "href" && !strings.HasPrefix(strings.TrimSpace(attr.Value), "#") {
					return nil, media.ErrInvalid
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 || nodes == 0 {
		return nil, media.ErrInvalid
	}
	svg, err := ui.ParseSVG(raw)
	if err != nil {
		return nil, err
	}
	w, h := svg.Size()
	if !(w > 0 && h > 0 && w <= media.MaxImageSide && h <= media.MaxImageSide && w*h <= media.MaxImagePixels) {
		return nil, media.ErrInvalid
	}
	return svg, nil
}
