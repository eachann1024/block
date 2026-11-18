package export

import (
	"image"
	"image/draw"
)

const (
	// previewDensity is the device-px-per-logical-px the on-screen preview needs; the
	// export itself is rasterised at exportRasterScale, which is wasteful for display.
	previewDensity = float32(2)
	// maxPreviewTextureSide keeps the preview bitmap within the GPU texture limit
	// (Metal / GL reject larger textures and draw nothing).
	maxPreviewTextureSide = 16384
)

// previewImage box-downsamples the exported raster by an integer factor so the
// preview bitmap stays sharp at screen density and always fits in one texture.
func previewImage(src image.Image) image.Image {
	b := src.Bounds()
	factor := max(1, int(exportRasterScale/previewDensity))
	for b.Dx()/factor > maxPreviewTextureSide || b.Dy()/factor > maxPreviewTextureSide {
		factor++
	}
	if factor == 1 {
		return src
	}
	rgba, ok := src.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(b)
		draw.Draw(rgba, b, src, b.Min, draw.Src)
	}
	dw, dh := b.Dx()/factor, b.Dy()/factor
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	area := uint32(factor * factor)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var r, g, bl, a uint32
			for sy := 0; sy < factor; sy++ {
				row := (y*factor+sy)*rgba.Stride + x*factor*4
				for sx := 0; sx < factor; sx++ {
					i := row + sx*4
					r += uint32(rgba.Pix[i])
					g += uint32(rgba.Pix[i+1])
					bl += uint32(rgba.Pix[i+2])
					a += uint32(rgba.Pix[i+3])
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/area), uint8(g/area), uint8(bl/area), uint8(a/area)
		}
	}
	return dst
}
