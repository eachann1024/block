package pluginui

import (
	"html"
	"strings"
	"sync"

	"block/internal/plugins"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

var (
	specSVGMu    sync.RWMutex
	specSVGCache = make(map[string]*ui.SVG)
)

// DrawDuoSpec renders an Ocean Paper Scheme A two-tone icon from a declarative DuoIconSpec
// by assembling cached viewBox="0 0 24 24" SVG layers for fill, stroke, and accent paths.
func DrawDuoSpec(c *ui.Context, spec plugins.DuoIconSpec, size float32, colors theme.IconColors) {
	if size <= 0 {
		return
	}
	root := ui.Box(c).Size(size, size).Shrink(0)
	root.Children(func() {
		if colors.Fill.A > 0 && len(spec.FillPaths) > 0 {
			if fill := cachedLayerSVG("fill", spec.FillPaths); fill != nil {
				ui.Icon(c, fill).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Fill)
			}
		}
		if colors.Line.A > 0 && len(spec.StrokePaths) > 0 {
			if line := cachedLayerSVG("stroke", spec.StrokePaths); line != nil {
				ui.Icon(c, line).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Line)
			}
		}
		if colors.Accent.A > 0 && len(spec.AccentPaths) > 0 {
			if acc := cachedLayerSVG("accent", spec.AccentPaths); acc != nil {
				ui.Icon(c, acc).Absolute().Top(0).Left(0).Size(size, size).TextColor(colors.Accent)
			}
		}
	})
}

func cachedLayerSVG(layer string, paths []string) *ui.SVG {
	raw := assembleLayerSVG(layer, paths)
	if raw == "" {
		return nil
	}
	specSVGMu.RLock()
	cached, ok := specSVGCache[raw]
	specSVGMu.RUnlock()
	if ok {
		return cached
	}
	parsed, err := ui.ParseSVG([]byte(raw))
	if err != nil {
		parsed = nil
	}
	specSVGMu.Lock()
	specSVGCache[raw] = parsed
	specSVGMu.Unlock()
	return parsed
}

func assembleLayerSVG(layer string, paths []string) string {
	var b strings.Builder
	hasPath := false
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`)
	if layer == "fill" {
		b.WriteString(`<g fill="black" stroke="none">`)
	} else {
		b.WriteString(`<g fill="none" stroke="black" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">`)
	}
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		hasPath = true
		b.WriteString(`<path d="`)
		b.WriteString(html.EscapeString(trimmed))
		b.WriteString(`"/>`)
	}
	if !hasPath {
		return ""
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}
