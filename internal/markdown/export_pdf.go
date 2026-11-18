package markdown

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"block/internal/model"
)

func exportPNG(doc *model.Doc, notePath string, prefs model.ExportPreferences) ([]byte, error) {
	rgba := renderDocRGBA(doc, notePath, prefs)
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
	rgba := renderDocRGBA(doc, notePath, prefs)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, errors.New("pdf render returned empty image")
	}
	bounds := rgba.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	maxSliceH := int(maxPDFPageHeightPt * exportScale)

	type pdfPage struct {
		w, h    int
		ptW     int
		ptH     int
		zStream []byte
	}
	var pages []pdfPage
	for y := 0; y < imgH; y += maxSliceH {
		sliceH := imgH - y
		if sliceH > maxSliceH {
			sliceH = maxSliceH
		}
		rgb := make([]byte, imgW*sliceH*3)
		pos := 0
		for row := 0; row < sliceH; row++ {
			rowOff := (y + row) * rgba.Stride
			for col := 0; col < imgW; col++ {
				px := rgba.Pix[rowOff+col*4 : rowOff+col*4+4]
				// Composite premultiplied RGBA over white.
				gap := uint8(255 - px[3])
				rgb[pos] = addSaturate(px[0], gap)
				rgb[pos+1] = addSaturate(px[1], gap)
				rgb[pos+2] = addSaturate(px[2], gap)
				pos += 3
			}
		}
		var zBuf bytes.Buffer
		zw := zlib.NewWriter(&zBuf)
		if _, err := zw.Write(rgb); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		ptW := int(float32(imgW)/exportScale + 0.5)
		ptH := int(float32(sliceH)/exportScale + 0.5)
		if ptW < 1 {
			ptW = 1
		}
		if ptH < 1 {
			ptH = 1
		}
		pages = append(pages, pdfPage{
			w: imgW, h: sliceH, ptW: ptW, ptH: ptH, zStream: zBuf.Bytes(),
		})
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

func addSaturate(a, b uint8) uint8 {
	s := int(a) + int(b)
	if s > 255 {
		return 255
	}
	return uint8(s)
}
