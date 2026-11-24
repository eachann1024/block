package mindmap

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

const (
	rasterPadDIP     float32 = 40
	rasterMaxSide            = 8192
	rasterMaxPixels          = 8 * 1024 * 1024
)

// ExportPNG renders the mind map at up to 2x resolution onto a padded canvas and encodes it as PNG.
func ExportPNG(root *Node, layout LayoutMode, pal theme.Palette) ([]byte, error) {
	rgba, _, _, err := renderMindmapRaster(root, layout, pal)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportPDF renders the mind map at high resolution and wraps it in a single-page FlateDecode PDF-1.4 document.
func ExportPDF(root *Node, layout LayoutMode, pal theme.Palette) ([]byte, error) {
	rgba, pageW, pageH, err := renderMindmapRaster(root, layout, pal)
	if err != nil {
		return nil, err
	}
	imgW, imgH := rgba.Bounds().Dx(), rgba.Bounds().Dy()

	var zBuf bytes.Buffer
	zw := zlib.NewWriter(&zBuf)
	rgb := make([]byte, imgW*3)
	for row := 0; row < imgH; row++ {
		rowOff := row * rgba.Stride
		for col := 0; col < imgW; col++ {
			px := rgba.Pix[rowOff+col*4 : rowOff+col*4+4]
			gap := uint8(255 - px[3])
			rgb[col*3] = addSaturateByte(px[0], gap)
			rgb[col*3+1] = addSaturateByte(px[1], gap)
			rgb[col*3+2] = addSaturateByte(px[2], gap)
		}
		if _, err := zw.Write(rgb); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, 0, 5)
	beginObj := func(num int) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n", num)
	}
	writeObj := func(num int, body string) {
		beginObj(num)
		out.WriteString(body)
		out.WriteString("\nendobj\n")
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, fmt.Sprintf(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>",
		pageW, pageH,
	))

	beginObj(4)
	fmt.Fprintf(&out,
		"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
		imgW, imgH, zBuf.Len(),
	)
	out.Write(zBuf.Bytes())
	out.WriteString("\nendstream\nendobj\n")

	cmd := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", pageW, pageH)
	writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmd), cmd))

	xrefOffset := out.Len()
	totalObjs := len(offsets) + 1
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", totalObjs)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", totalObjs, xrefOffset)
	return out.Bytes(), nil
}

func renderMindmapRaster(root *Node, layout LayoutMode, pal theme.Palette) (*image.RGBA, int, int, error) {
	if root == nil {
		return nil, 0, 0, errors.New("cannot render nil mindmap")
	}
	if layout != LayoutRight && layout != LayoutBoth {
		layout = LayoutRight
	}
	if pal.Paper.A == 0 && pal.Text.A == 0 {
		pal = theme.Ocean()
	}
	bg := pal.Paper
	if bg.A == 0 {
		bg = pal.CodeBG
	}

	cloned := root.Clone()
	var mc MeasureCache
	var bounds Bounds
	// Measure layout using a real Painter first so node widths match rendered text metrics.
	_ = ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(1, 1).Draw(func(p *ui.Painter, _ ui.Rect) {
			bounds = ComputeLayout(cloned, layout, p, &mc)
		})
	}, 1, 1, 1)
	if bounds.Width() <= 0 || bounds.Height() <= 0 {
		bounds = ComputeLayout(cloned, layout, nil, &mc)
	}

	worldW := max(bounds.Width(), 1) + rasterPadDIP*2
	worldH := max(bounds.Height(), 1) + rasterPadDIP*2
	pageW := max(int(math.Ceil(float64(worldW))), 1)
	pageH := max(int(math.Ceil(float64(worldH))), 1)

	scale := min(
		float32(2),
		float32(rasterMaxSide)/float32(max(pageW, pageH)),
		float32(math.Sqrt(float64(rasterMaxPixels)/(float64(pageW)*float64(pageH)))),
	)
	if scale <= 0 {
		scale = 1
	}

	renderW, renderH := pageW, pageH
	renderScale := scale
	camScale := float32(1)
	if scale < 1 {
		if scale < 0.35 {
			return nil, 0, 0, errors.New(theme.Tr(
				"思维导图尺寸过大，单张图片缩放后文字将难以辨认，请折叠部分分支或改用 Markdown / OPML / XMind 导出",
				"Mind map is too large to export legibly as a single image; fold some branches or export as Markdown / OPML / XMind",
			))
		}
		renderW = max(int(float32(pageW)*scale+0.5), 1)
		renderH = max(int(float32(pageH)*scale+0.5), 1)
		renderScale = 1
		camScale = scale
	}

	rgba := ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(float32(renderW), float32(renderH)).Draw(func(p *ui.Painter, _ ui.Rect) {
			PaintScene(p, cloned, ui.Rect{W: float32(renderW), H: float32(renderH)}, Camera{
				X:     bounds.MinX - rasterPadDIP,
				Y:     bounds.MinY - rasterPadDIP,
				Scale: camScale,
			}, PaintOptions{
				Palette:        pal,
				FillBackground: true,
				Background:     bg,
				ForceLabels:    true,
				ExportMode:     true,
			})
		})
	}, renderW, renderH, renderScale)
	if rgba == nil || rgba.Bounds().Empty() {
		return nil, 0, 0, errors.New("mindmap raster render returned empty image")
	}
	return rgba, pageW, pageH, nil
}

func addSaturateByte(a, b uint8) uint8 {
	s := int(a) + int(b)
	if s > 255 {
		return 255
	}
	return uint8(s)
}
