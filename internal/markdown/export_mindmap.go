package markdown

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
	"sync"

	"block/internal/codepreview"
	"block/internal/model"
	"block/internal/theme"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// MindMapRenderFunc renders a Markdown mind map block into a raster image,
// returning the image, 1x logical dimensions, root alt text, and any error.
type MindMapRenderFunc func(code, layout string, pal theme.Palette) (rgba *image.RGBA, pageW, pageH int, alt string, err error)

var (
	mindMapRendererMu sync.RWMutex
	mindMapRenderer   MindMapRenderFunc
)

// RegisterMindMapRenderer installs the visual MindMap rasterizer used by
// styled document exports and previews without introducing an import cycle.
func RegisterMindMapRenderer(fn MindMapRenderFunc) {
	mindMapRendererMu.Lock()
	mindMapRenderer = fn
	mindMapRendererMu.Unlock()
}

func currentMindMapRenderer() MindMapRenderFunc {
	mindMapRendererMu.RLock()
	fn := mindMapRenderer
	mindMapRendererMu.RUnlock()
	return fn
}

func isMindMapCodeBlock(b *model.Block) bool {
	return b != nil && b.Kind == model.BlockCode &&
		codepreview.CanonicalLanguage(b.Language) == "mindmap" &&
		strings.TrimSpace(b.Code) != ""
}

func mindMapPaletteForPaper(pal PaperThemeSpec) theme.Palette {
	base := theme.OceanLight
	if pal.Dark {
		base = theme.OceanDark
	}
	paper := pal.InnerCanvas
	if paper.A < 255 {
		paper = paper.Over(pal.Canvas)
	}
	if paper.A < 255 {
		paper.A = 255
	}
	base.Paper = paper
	base.Card = paper
	if pal.Text.A > 0 {
		base.Text = pal.Text
	}
	if pal.Secondary.A > 0 {
		base.TextSecondary = pal.Secondary
	}
	if pal.Accent.A > 0 {
		base.Accent = pal.Accent
	}
	if pal.CodeBorder.A > 0 {
		base.Hairline = pal.CodeBorder
	}
	if pal.CodeBG.A > 0 {
		base.CodeBG = pal.CodeBG
	}
	return base
}

func renderMindMapExportRaster(b *model.Block, pal PaperThemeSpec) (alt string, rgba *image.RGBA, pageW, pageH int, ok bool) {
	if !isMindMapCodeBlock(b) {
		return "", nil, 0, 0, false
	}
	fn := currentMindMapRenderer()
	if fn == nil {
		return "", nil, 0, 0, false
	}
	rgba, pageW, pageH, alt, err := fn(b.Code, b.MindMapLayout, mindMapPaletteForPaper(pal))
	if err != nil || rgba == nil || rgba.Bounds().Empty() || pageW <= 0 || pageH <= 0 {
		return "", nil, 0, 0, false
	}
	if strings.TrimSpace(alt) == "" {
		alt = "Mind Map"
	}
	return alt, rgba, pageW, pageH, true
}

func encodeMindMapPNG(rgba *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildPaintedMindMap(b *model.Block, bx, by, avail float32, pal PaperThemeSpec, legacy bool, pixels *int) (paintedBlock, bool) {
	if pixels != nil && *pixels <= 0 {
		return paintedBlock{}, false
	}
	_, rgba, pageW, pageH, ok := renderMindMapExportRaster(b, pal)
	if !ok {
		return paintedBlock{}, false
	}
	if pixels != nil {
		rgba = fitRasterToPixels(rgba, *pixels)
		if rgba == nil {
			return paintedBlock{}, false
		}
		*pixels -= rgba.Bounds().Dx() * rgba.Bounds().Dy()
	}
	bm := ui.NewBitmap(rgba)
	if bm == nil {
		return paintedBlock{}, false
	}
	bw, bh := bm.Size()
	if bw <= 0 || bh <= 0 {
		return paintedBlock{}, false
	}
	targetW := minFloat32(avail, float32(pageW))
	targetH := targetW * (float32(pageH) / float32(pageW))
	dx := bx + (avail-targetW)/2
	r := orF(pal.Radius, 8)
	if legacy {
		r = 8
	}
	borderCol := pal.CodeBorder
	if borderCol.A == 0 {
		borderCol = pal.Divider
	}
	frame := ui.Rect{X: dx, Y: by, W: targetW, H: targetH}
	return paintedBlock{
		kind:      model.BlockImage,
		x:         bx,
		y:         by,
		w:         avail,
		h:         targetH,
		frame:     frame,
		frameKind: ImageFrameBorder,
		radius:    r,
		imgRadius: r,
		ruleCol:   borderCol,
		borderW:   1,
		img: &paintedImage{
			bitmap: bm,
			src:    ui.Rect{W: float32(bw), H: float32(bh)},
			dst:    frame,
		},
	}, true
}

// fitRasterToPixels downsamples rgba so it holds at most budget pixels;
// it returns nil when the remaining budget is too small to be useful.
func fitRasterToPixels(rgba *image.RGBA, budget int) *image.RGBA {
	w, h := rgba.Bounds().Dx(), rgba.Bounds().Dy()
	if w*h <= budget {
		return rgba
	}
	scale := math.Sqrt(float64(budget) / float64(w*h))
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 64 || nh < 64 {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), rgba, rgba.Bounds(), xdraw.Src, nil)
	return dst
}

func renderHTMLMindMap(out *strings.Builder, b *model.Block, pal PaperThemeSpec, legacy bool, budget *int) bool {
	if budget != nil && *budget <= 0 {
		return false
	}
	alt, rgba, pageW, _, ok := renderMindMapExportRaster(b, pal)
	if !ok {
		return false
	}
	raw, err := encodeMindMapPNG(rgba)
	if err != nil {
		return false
	}
	if budget != nil {
		if len(raw) > *budget {
			return false
		}
		*budget -= len(raw)
	}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	r := orF(pal.Radius, 8)
	if legacy {
		r = 8
	}
	borderCol := pal.CodeBorder
	if borderCol.A == 0 {
		borderCol = pal.Divider
	}
	borderCSS := ""
	if borderCol.A > 0 {
		borderCSS = fmt.Sprintf("border:1px solid %s;", uiColorCSS(borderCol))
	}
	var indentCSS string
	if b.Indent > 0 {
		indentCSS = fmt.Sprintf("margin-left:%dpx;", b.Indent*12)
	}
	out.WriteString(fmt.Sprintf(
		`<figure style="margin:18px auto;max-width:100%%;width:min(%dpx,100%%);%s"><img src="%s" alt="%s" style="display:block;width:100%%;height:auto;border-radius:%.0fpx;%s"></figure>`+"\n",
		pageW, indentCSS, escapeAttrValue(src), escapeAttrValue(alt), r, borderCSS,
	))
	return true
}

// docxPageHeightPx is the A4 page height (16838 twips) in export pixels.
const docxPageHeightPx = 16838.0 / 15

func renderDOCXMindMap(b *model.Block, avail, availH, indent float32, pal PaperThemeSpec, assets *docxAssets) (string, bool) {
	if assets == nil || assets.imageBytes <= 0 {
		return "", false
	}
	alt, rgba, pageW, pageH, ok := renderMindMapExportRaster(b, pal)
	if !ok {
		return "", false
	}
	raw, err := encodeMindMapPNG(rgba)
	if err != nil || len(raw) > assets.imageBytes {
		return "", false
	}
	assets.imageBytes -= len(raw)
	targetW := minFloat32(avail, float32(pageW))
	targetH := targetW * float32(pageH) / float32(pageW)
	if availH > 0 && targetH > availH {
		targetW *= availH / targetH
		targetH = availH
	}
	left := indent + maxFloat32(0, (avail-targetW)/2)
	run := assets.addImage(raw, "png", targetW, targetH, alt, model.ImageCrop{}, ImageStyle{})
	return fmt.Sprintf(`<w:p><w:pPr><w:ind w:left="%d"/><w:spacing w:before="80" w:after="160"/></w:pPr>%s</w:p>`, docxTwips(left), run), true
}
