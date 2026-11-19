package markdown

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"sort"
	"strings"

	"block/internal/model"
)

func exportPNG(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	ctx := context.Background()
	sc := buildExportScene(ctx, doc, notePath, prefs)
	w, h := int(sc.width), max(int(math.Ceil(float64(sc.height))), 1)
	rgba := paintExportRaster(ctx, sc, 0, h, exportPNGScale(w, h), buildSubstrateTiles(sc.pal, sc.legacy))
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("png render returned empty image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportPDF(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	ctx := context.Background()
	sc := buildExportScene(ctx, doc, notePath, prefs)
	height := max(int(math.Ceil(float64(sc.height))), 1)
	tiles := buildSubstrateTiles(sc.pal, sc.legacy)

	type pdfPage struct {
		w, h    int
		ptW     int
		ptH     int
		zStream []byte
	}
	var pages []pdfPage
	top := 0
	for _, bottom := range pdfPageEnds(sc, height) {
		pageHeight := bottom - top
		rgba := paintExportRaster(ctx, sc, top, pageHeight, exportScale, tiles)
		if rgba == nil || rgba.Bounds().Empty() {
			return nil, errors.New("pdf render returned empty image")
		}
		imgW, imgH := rgba.Bounds().Dx(), rgba.Bounds().Dy()
		rgb := make([]byte, imgW*3)
		var zBuf bytes.Buffer
		zw := zlib.NewWriter(&zBuf)
		for row := 0; row < imgH; row++ {
			rowOff := row * rgba.Stride
			for col := 0; col < imgW; col++ {
				px := rgba.Pix[rowOff+col*4 : rowOff+col*4+4]
				gap := uint8(255 - px[3])
				rgb[col*3] = addSaturate(px[0], gap)
				rgb[col*3+1] = addSaturate(px[1], gap)
				rgb[col*3+2] = addSaturate(px[2], gap)
			}
			if _, err := zw.Write(rgb); err != nil {
				return nil, err
			}
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		pages = append(pages, pdfPage{
			w: imgW, h: imgH, ptW: int(sc.width), ptH: pageHeight, zStream: zBuf.Bytes(),
		})
		top = bottom
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	var offsets []int
	writeObj := func(num int, body []byte) {
		offsets = append(offsets, out.Len())
		out.WriteString(fmt.Sprintf("%d 0 obj\n", num))
		out.Write(body)
		out.WriteString("\nendobj\n")
	}

	var kids strings.Builder
	for i := range pages {
		if i > 0 {
			kids.WriteByte(' ')
		}
		kids.WriteString(fmt.Sprintf("%d 0 R", 3+i*3))
	}
	writeObj(1, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	writeObj(2, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(pages))))

	for i, pg := range pages {
		pageObj := 3 + i*3
		imgObj := 4 + i*3
		contentObj := 5 + i*3

		pageDict := fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
			pg.ptW, pg.ptH, imgObj, contentObj,
		)
		writeObj(pageObj, []byte(pageDict))

		var imgBuf bytes.Buffer
		imgBuf.WriteString(fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
			pg.w, pg.h, len(pg.zStream),
		))
		imgBuf.Write(pg.zStream)
		imgBuf.WriteString("\nendstream")
		writeObj(imgObj, imgBuf.Bytes())

		cmd := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", pg.ptW, pg.ptH)
		contentDict := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmd), cmd)
		writeObj(contentObj, []byte(contentDict))
	}

	xrefOffset := out.Len()
	totalObjs := 2 + len(pages)*3 + 1
	out.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", totalObjs))
	for _, off := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", totalObjs, xrefOffset))
	return out.Bytes(), nil
}

// Break in the gap before the next block. Oversized blocks can break at a
// table row or text line boundary, with a fixed break for oversized figures.
func pdfPageEnds(sc exportScene, height int) []int {
	var breaks []int
	for _, b := range sc.blocks {
		breaks = append(breaks, int(math.Floor(float64(b.y))))
		if b.h < maxPDFPageHeightPt {
			continue
		}
		if b.table != nil {
			for _, row := range b.table.rows[min(1, len(b.table.rows)):] {
				breaks = append(breaks, int(math.Round(float64(row.y))))
			}
		}
		if b.layout != nil && b.angle == 0 && b.quoteAngle == 0 {
			lines := b.layout.Lines()
			for _, line := range lines[min(1, len(lines)):] {
				breaks = append(breaks, int(math.Round(float64(b.textY+line.Rect.Y))))
			}
		}
	}
	sort.Ints(breaks)
	var ends []int
	for top := 0; top < height; {
		end := min(top+maxPDFPageHeightPt, height)
		if end < height {
			i := sort.SearchInts(breaks, end+1)
			if i > 0 && breaks[i-1] >= top+maxPDFPageHeightPt/4 {
				end = breaks[i-1]
			}
		}
		ends = append(ends, end)
		top = end
	}
	return ends
}

func addSaturate(a, b uint8) uint8 {
	s := int(a) + int(b)
	if s > 255 {
		return 255
	}
	return uint8(s)
}
