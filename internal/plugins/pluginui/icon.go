package pluginui

import (
	"fmt"
	"html"
	"strings"
	"sync"

	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var (
	specSVGMu      sync.RWMutex
	specSVGCache   = make(map[string]*ui.SVG)
	menuIconMu     sync.RWMutex
	menuIconCache  = make(map[string]ui.MenuIcon)
)

const (
	maxSpecSVGCacheEntries  = 512
	maxMenuIconCacheEntries = 256
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

// DrawUIIcon renders a plugin UI icon: a host built-in glyph by name, or an inline vector.
func DrawUIIcon(c *ui.Context, ic plugins.UIIcon, size float32, colors theme.IconColors) {
	if ic.Name != "" {
		icons.Draw(c, icons.Icon(ic.Name), size, colors)
		return
	}
	DrawDuoSpec(c, ic.Spec(), size, colors)
}

// BuildMenuIcon builds and caches a native colored ui.MenuIcon from a plugin DuoIconSpec.
func BuildMenuIcon(spec plugins.DuoIconSpec, colors theme.IconColors) ui.MenuIcon {
	raw := assembleColoredSVG(spec, colors)
	if raw == "" {
		return nil
	}
	menuIconMu.RLock()
	cached, ok := menuIconCache[raw]
	menuIconMu.RUnlock()
	if ok {
		return cached
	}
	parsed, err := ui.ParseSVG([]byte(raw))
	var out ui.MenuIcon
	if err == nil && parsed != nil {
		if ic, icErr := mygo.NewMenuIconSVG(parsed, false); icErr == nil && ic != nil {
			out = ic
		}
	}
	menuIconMu.Lock()
	if len(menuIconCache) >= maxMenuIconCacheEntries {
		clear(menuIconCache)
	}
	menuIconCache[raw] = out
	menuIconMu.Unlock()
	return out
}

func svgRGBA(c ui.Color) (hex string, opacity string) {
	s := c.SRGB()
	hex = fmt.Sprintf("#%02x%02x%02x", s.R, s.G, s.B)
	if s.A >= 255 {
		return hex, "1"
	}
	return hex, fmt.Sprintf("%.3f", float64(s.A)/255.0)
}

func assembleColoredSVG(spec plugins.DuoIconSpec, colors theme.IconColors) string {
	var b strings.Builder
	hasPath := false
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`)
	writeGroup := func(fill bool, paths []string, col ui.Color) {
		if col.A == 0 || len(paths) == 0 {
			return
		}
		hex, op := svgRGBA(col)
		if fill {
			b.WriteString(`<g fill="`)
			b.WriteString(hex)
			b.WriteString(`" fill-opacity="`)
			b.WriteString(op)
			b.WriteString(`" stroke="none">`)
		} else {
			b.WriteString(`<g fill="none" stroke="`)
			b.WriteString(hex)
			b.WriteString(`" stroke-opacity="`)
			b.WriteString(op)
			b.WriteString(`" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">`)
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
		b.WriteString(`</g>`)
	}
	writeGroup(true, spec.FillPaths, colors.Fill)
	writeGroup(false, spec.StrokePaths, colors.Line)
	writeGroup(false, spec.AccentPaths, colors.Accent)
	if !hasPath {
		return ""
	}
	b.WriteString(`</svg>`)
	return b.String()
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
	if len(specSVGCache) >= maxSpecSVGCacheEntries {
		clear(specSVGCache)
	}
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
