package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

const maxThemeSkinJSONBytes = 512 * 1024

var skinIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ThemeSkinLocalizedText holds bilingual copy embedded in a theme skin JSON.
type ThemeSkinLocalizedText struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

// Resolve returns the localized copy matching locale, falling back to whichever is non-empty.
func (t ThemeSkinLocalizedText) Resolve(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		if strings.TrimSpace(t.En) != "" {
			return t.En
		}
		return t.Zh
	}
	if strings.TrimSpace(t.Zh) != "" {
		return t.Zh
	}
	return t.En
}

// ThemeSkinFonts declares optional font stacks supplied by a theme skin.
type ThemeSkinFonts struct {
	Serif   string `json:"serif,omitempty"`
	Display string `json:"display,omitempty"`
}

// ThemeSkinQuickNote styles the floating quick-note window when a skin is active.
type ThemeSkinQuickNote struct {
	BorderAccent string                 `json:"border_accent,omitempty"`
	HeaderTint   string                 `json:"header_tint,omitempty"`
	BadgeText    ThemeSkinLocalizedText `json:"badge_text,omitempty"`
	FlashAccent  string                 `json:"flash_accent,omitempty"`
}

// ThemeSkinAIIndicator customizes the AI activity indicator when a skin is active.
type ThemeSkinAIIndicator struct {
	Style  string                 `json:"style,omitempty"`
	Accent string                 `json:"accent,omitempty"`
	Label  ThemeSkinLocalizedText `json:"label,omitempty"`
}

// ThemeSkinSpec describes a declarative plugin theme skin loaded from JSON.
type ThemeSkinSpec struct {
	ID          string                       `json:"id"`
	Fonts       ThemeSkinFonts               `json:"fonts,omitempty"`
	ChromeDark  bool                         `json:"chrome_dark,omitempty"`
	Light       map[string]string            `json:"light,omitempty"`
	Dark        map[string]string            `json:"dark,omitempty"`
	Surfaces    map[string]map[string]string `json:"surfaces,omitempty"`
	QuickNote   *ThemeSkinQuickNote          `json:"quick_note,omitempty"`
	AIIndicator *ThemeSkinAIIndicator        `json:"ai_indicator,omitempty"`
}

// ParseHexColor parses "#rrggbb" or "#rrggbbaa" without panicking.
func ParseHexColor(s string) (ui.Color, error) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) != 7 && len(trimmed) != 9 {
		return ui.Color{}, fmt.Errorf("invalid hex color %q: must be #rrggbb or #rrggbbaa", s)
	}
	if trimmed[0] != '#' {
		return ui.Color{}, fmt.Errorf("invalid hex color %q: must start with #", s)
	}
	parseByte := func(hi, lo byte) (uint8, bool) {
		v1, ok1 := hexNibble(hi)
		v2, ok2 := hexNibble(lo)
		if !ok1 || !ok2 {
			return 0, false
		}
		return (v1 << 4) | v2, true
	}
	r, okR := parseByte(trimmed[1], trimmed[2])
	g, okG := parseByte(trimmed[3], trimmed[4])
	b, okB := parseByte(trimmed[5], trimmed[6])
	if !okR || !okG || !okB {
		return ui.Color{}, fmt.Errorf("invalid hex color %q", s)
	}
	if len(trimmed) == 7 {
		return ui.RGB(r, g, b), nil
	}
	a, okA := parseByte(trimmed[7], trimmed[8])
	if !okA {
		return ui.Color{}, fmt.Errorf("invalid hex color %q", s)
	}
	return ui.RGBA(r, g, b, float32(a)/255.0), nil
}

func hexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// ParseThemeSkinJSON decodes and validates a theme skin JSON document.
func ParseThemeSkinJSON(raw []byte) (*ThemeSkinSpec, error) {
	if len(raw) == 0 {
		return nil, errors.New("theme skin file is empty")
	}
	if len(raw) > maxThemeSkinJSONBytes {
		return nil, fmt.Errorf("theme skin exceeds %d bytes", maxThemeSkinJSONBytes)
	}
	var spec ThemeSkinSpec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("invalid theme skin JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing data after theme skin JSON object")
	}
	if err := validateThemeSkinSpec(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func validateThemeSkinSpec(spec *ThemeSkinSpec) error {
	if spec == nil {
		return errors.New("theme skin is nil")
	}
	spec.ID = strings.TrimSpace(spec.ID)
	if !skinIDRe.MatchString(spec.ID) {
		return fmt.Errorf("theme skin id %q must match %s", spec.ID, skinIDRe.String())
	}
	if len(spec.Light) == 0 && len(spec.Dark) == 0 && len(spec.Surfaces) == 0 {
		return errors.New("theme skin must define at least one of light, dark, or surfaces colors")
	}
	for _, field := range []struct {
		name string
		val  string
	}{
		{"fonts.serif", spec.Fonts.Serif},
		{"fonts.display", spec.Fonts.Display},
	} {
		if strings.ContainsAny(field.val, "\r\n\x00") || len(field.val) > 512 {
			return fmt.Errorf("theme skin %s is invalid", field.name)
		}
	}
	if err := validateSkinColorMap("light", spec.Light); err != nil {
		return err
	}
	if err := validateSkinColorMap("dark", spec.Dark); err != nil {
		return err
	}
	for surf, colors := range spec.Surfaces {
		if strings.TrimSpace(surf) == "" {
			return errors.New("theme skin surfaces key must not be empty")
		}
		if err := validateSkinColorMap("surfaces."+surf, colors); err != nil {
			return err
		}
	}
	if spec.QuickNote != nil {
		for _, pair := range []struct {
			name string
			val  string
		}{
			{"quick_note.border_accent", spec.QuickNote.BorderAccent},
			{"quick_note.header_tint", spec.QuickNote.HeaderTint},
			{"quick_note.flash_accent", spec.QuickNote.FlashAccent},
		} {
			if strings.TrimSpace(pair.val) == "" {
				continue
			}
			if _, err := ParseHexColor(pair.val); err != nil {
				return fmt.Errorf("%s: %w", pair.name, err)
			}
		}
	}
	if spec.AIIndicator != nil && strings.TrimSpace(spec.AIIndicator.Accent) != "" {
		if _, err := ParseHexColor(spec.AIIndicator.Accent); err != nil {
			return fmt.Errorf("ai_indicator.accent: %w", err)
		}
	}
	return nil
}

func validateSkinColorMap(section string, m map[string]string) error {
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("theme skin %s contains empty token key", section)
		}
		if _, err := ParseHexColor(v); err != nil {
			return fmt.Errorf("theme skin %s.%s: %w", section, k, err)
		}
	}
	return nil
}

func cloneThemeSkinSpec(spec *ThemeSkinSpec) *ThemeSkinSpec {
	if spec == nil {
		return nil
	}
	cp := *spec
	if spec.Light != nil {
		cp.Light = maps.Clone(spec.Light)
	}
	if spec.Dark != nil {
		cp.Dark = maps.Clone(spec.Dark)
	}
	if spec.Surfaces != nil {
		cp.Surfaces = make(map[string]map[string]string, len(spec.Surfaces))
		for k, v := range spec.Surfaces {
			cp.Surfaces[k] = maps.Clone(v)
		}
	}
	if spec.QuickNote != nil {
		qn := *spec.QuickNote
		cp.QuickNote = &qn
	}
	if spec.AIIndicator != nil {
		ai := *spec.AIIndicator
		cp.AIIndicator = &ai
	}
	return &cp
}

var (
	skinMu          sync.RWMutex
	activeSkin      *ThemeSkinSpec
	activeSkinOwner string
)

// SetActiveSkin activates spec on behalf of pluginID across the workspace.
func SetActiveSkin(pluginID string, spec *ThemeSkinSpec) {
	if spec == nil {
		ClearActiveSkin(pluginID)
		return
	}
	cp := cloneThemeSkinSpec(spec)
	skinMu.Lock()
	activeSkin = cp
	activeSkinOwner = strings.TrimSpace(pluginID)
	skinMu.Unlock()
	maybeEnsureHarmonyFont(cp.Fonts.Serif, cp.Fonts.Display)
	BumpFontEpoch()
}

// ClearActiveSkin clears the active theme skin when pluginID is empty or owns it.
func ClearActiveSkin(pluginID string) {
	owner := strings.TrimSpace(pluginID)
	skinMu.Lock()
	changed := false
	if activeSkin != nil && (owner == "" || activeSkinOwner == owner) {
		activeSkin = nil
		activeSkinOwner = ""
		changed = true
	}
	skinMu.Unlock()
	if changed {
		BumpFontEpoch()
	}
}

// ActiveSkin returns a copy of the active theme skin, or nil if none is active.
func ActiveSkin() *ThemeSkinSpec {
	skinMu.RLock()
	defer skinMu.RUnlock()
	return cloneThemeSkinSpec(activeSkin)
}

// ActiveSkinOwner returns the plugin ID that owns the currently active theme skin.
func ActiveSkinOwner() string {
	skinMu.RLock()
	defer skinMu.RUnlock()
	return activeSkinOwner
}

// ActiveSerifFont returns the active skin's serif font stack, or empty when none is set.
func ActiveSerifFont() string {
	skinMu.RLock()
	defer skinMu.RUnlock()
	if activeSkin == nil {
		return ""
	}
	return strings.TrimSpace(activeSkin.Fonts.Serif)
}

// ActiveDisplayFont returns the active skin's display font stack, or empty when none is set.
func ActiveDisplayFont() string {
	skinMu.RLock()
	defer skinMu.RUnlock()
	if activeSkin == nil {
		return ""
	}
	return strings.TrimSpace(activeSkin.Fonts.Display)
}

// ApplySkinPalette overlays the currently active theme skin onto base for surface
// ("paper", "chrome", or ""). When no skin is active, base is returned unchanged.
func ApplySkinPalette(base Palette, surface string) Palette {
	skinMu.RLock()
	skin := activeSkin
	skinMu.RUnlock()
	if skin == nil {
		return base
	}

	out := base
	normSurface := strings.ToLower(strings.TrimSpace(surface))
	switch normSurface {
	case "chrome":
		if skin.ChromeDark && !out.Dark {
			out = OceanDark
			out.Dark = true
		}
	case "paper":
		if out.Dark && surfaceOverridesLightBackground(skin.Surfaces["paper"]) {
			out = OceanLight
			out.Dark = false
		}
	}

	if out.Dark {
		applyColorMap(&out, skin.Dark)
	} else {
		applyColorMap(&out, skin.Light)
	}
	if normSurface != "" {
		if surfColors, ok := skin.Surfaces[normSurface]; ok {
			applyColorMap(&out, surfColors)
		}
	}
	return out
}

func surfaceOverridesLightBackground(colors map[string]string) bool {
	if len(colors) == 0 {
		return false
	}
	for _, key := range []string{"Paper", "Shell"} {
		raw, ok := colors[key]
		if !ok {
			continue
		}
		c, err := ParseHexColor(raw)
		if err != nil {
			continue
		}
		// Perceived luminance threshold: > 0.5 indicates a light surface.
		lum := (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255.0
		return lum > 0.5
	}
	return false
}

var paletteColorSetters = map[string]func(*Palette, ui.Color){
	"Shell":               func(p *Palette, c ui.Color) { p.Shell = c },
	"Sidebar":             func(p *Palette, c ui.Color) { p.Sidebar = c },
	"SettingsBG":          func(p *Palette, c ui.Color) { p.SettingsBG = c },
	"Paper":               func(p *Palette, c ui.Color) { p.Paper = c },
	"Card":                func(p *Palette, c ui.Color) { p.Card = c },
	"Menu":                func(p *Palette, c ui.Color) { p.Menu = c },
	"Text":                func(p *Palette, c ui.Color) { p.Text = c },
	"TextSecondary":       func(p *Palette, c ui.Color) { p.TextSecondary = c },
	"Placeholder":         func(p *Palette, c ui.Color) { p.Placeholder = c },
	"Accent":              func(p *Palette, c ui.Color) { p.Accent = c },
	"AccentHover":         func(p *Palette, c ui.Color) { p.AccentHover = c },
	"AccentPressed":       func(p *Palette, c ui.Color) { p.AccentPressed = c },
	"AccentText":          func(p *Palette, c ui.Color) { p.AccentText = c },
	"IconFill":            func(p *Palette, c ui.Color) { p.IconFill = c },
	"IconShade":           func(p *Palette, c ui.Color) { p.IconShade = c },
	"IconDetail":          func(p *Palette, c ui.Color) { p.IconDetail = c },
	"IconLitFill":         func(p *Palette, c ui.Color) { p.IconLitFill = c },
	"IconLitShade":        func(p *Palette, c ui.Color) { p.IconLitShade = c },
	"IconLitDetail":       func(p *Palette, c ui.Color) { p.IconLitDetail = c },
	"Hover":               func(p *Palette, c ui.Color) { p.Hover = c },
	"Selected":            func(p *Palette, c ui.Color) { p.Selected = c },
	"SelectedBorder":      func(p *Palette, c ui.Color) { p.SelectedBorder = c },
	"Hairline":            func(p *Palette, c ui.Color) { p.Hairline = c },
	"SidebarBorder":       func(p *Palette, c ui.Color) { p.SidebarBorder = c },
	"BtnBorder":           func(p *Palette, c ui.Color) { p.BtnBorder = c },
	"CodeBG":              func(p *Palette, c ui.Color) { p.CodeBG = c },
	"QuoteBar":            func(p *Palette, c ui.Color) { p.QuoteBar = c },
	"SelectionBG":         func(p *Palette, c ui.Color) { p.SelectionBG = c },
	"SelectionFG":         func(p *Palette, c ui.Color) { p.SelectionFG = c },
	"Danger":              func(p *Palette, c ui.Color) { p.Danger = c },
	"DangerSurface":       func(p *Palette, c ui.Color) { p.DangerSurface = c },
	"Warning":             func(p *Palette, c ui.Color) { p.Warning = c },
	"WarningSurface":      func(p *Palette, c ui.Color) { p.WarningSurface = c },
	"Success":             func(p *Palette, c ui.Color) { p.Success = c },
	"SuccessSurface":      func(p *Palette, c ui.Color) { p.SuccessSurface = c },
	"DiffAddBG":           func(p *Palette, c ui.Color) { p.DiffAddBG = c },
	"DiffAddFG":           func(p *Palette, c ui.Color) { p.DiffAddFG = c },
	"DiffDelBG":           func(p *Palette, c ui.Color) { p.DiffDelBG = c },
	"DiffDelFG":           func(p *Palette, c ui.Color) { p.DiffDelFG = c },
	"MenuBorder":          func(p *Palette, c ui.Color) { p.MenuBorder = c },
	"MenuIcon":            func(p *Palette, c ui.Color) { p.MenuIcon = c },
	"MenuMuted":           func(p *Palette, c ui.Color) { p.MenuMuted = c },
	"MenuHover":           func(p *Palette, c ui.Color) { p.MenuHover = c },
	"MenuSeparator":       func(p *Palette, c ui.Color) { p.MenuSeparator = c },
	"MenuDangerFG":        func(p *Palette, c ui.Color) { p.MenuDangerFG = c },
	"MenuDangerHoverBG":   func(p *Palette, c ui.Color) { p.MenuDangerHoverBG = c },
	"MenuDangerHoverFG":   func(p *Palette, c ui.Color) { p.MenuDangerHoverFG = c },
	"SwitchTrackOff":      func(p *Palette, c ui.Color) { p.SwitchTrackOff = c },
	"SwitchTrackHover":    func(p *Palette, c ui.Color) { p.SwitchTrackHover = c },
	"SwitchTrackPressed":  func(p *Palette, c ui.Color) { p.SwitchTrackPressed = c },
	"SwitchKnob":          func(p *Palette, c ui.Color) { p.SwitchKnob = c },
	"SwitchKnobOff":       func(p *Palette, c ui.Color) { p.SwitchKnobOff = c },
	"SwitchKnobBorder":    func(p *Palette, c ui.Color) { p.SwitchKnobBorder = c },
	"SwitchKnobShadow":    func(p *Palette, c ui.Color) { p.SwitchKnobShadow = c },
	"TooltipBG":           func(p *Palette, c ui.Color) { p.TooltipBG = c },
	"TooltipBorder":       func(p *Palette, c ui.Color) { p.TooltipBorder = c },
	"TooltipDivider":      func(p *Palette, c ui.Color) { p.TooltipDivider = c },
	"TooltipMuted":        func(p *Palette, c ui.Color) { p.TooltipMuted = c },
	"TooltipAccent":       func(p *Palette, c ui.Color) { p.TooltipAccent = c },
	"TooltipDanger":       func(p *Palette, c ui.Color) { p.TooltipDanger = c },
	"TooltipDangerBorder": func(p *Palette, c ui.Color) { p.TooltipDangerBorder = c },
	"TooltipPillBG":       func(p *Palette, c ui.Color) { p.TooltipPillBG = c },
	"TooltipPillBorder":   func(p *Palette, c ui.Color) { p.TooltipPillBorder = c },
	"TooltipPillFG":       func(p *Palette, c ui.Color) { p.TooltipPillFG = c },
	"ScrollThumb":         func(p *Palette, c ui.Color) { p.ScrollThumb = c },
	"ScrollThumbHover":    func(p *Palette, c ui.Color) { p.ScrollThumbHover = c },
	"Link":                func(p *Palette, c ui.Color) { p.Link = c },
	"InlineCodeFG":        func(p *Palette, c ui.Color) { p.InlineCodeFG = c },
	"InlineCodeBG":        func(p *Palette, c ui.Color) { p.InlineCodeBG = c },
	"InlineCodeHoverBG":   func(p *Palette, c ui.Color) { p.InlineCodeHoverBG = c },
	"InlineCodeEditBG":    func(p *Palette, c ui.Color) { p.InlineCodeEditBG = c },
	"CodeBlockBG":         func(p *Palette, c ui.Color) { p.CodeBlockBG = c },
	"HintBand":            func(p *Palette, c ui.Color) { p.HintBand = c },
	"ListGuide":           func(p *Palette, c ui.Color) { p.ListGuide = c },
	"ListBullet":          func(p *Palette, c ui.Color) { p.ListBullet = c },
	"TodoBG":              func(p *Palette, c ui.Color) { p.TodoBG = c },
	"TodoRing":            func(p *Palette, c ui.Color) { p.TodoRing = c },
	"TodoRingHover":       func(p *Palette, c ui.Color) { p.TodoRingHover = c },
	"AnnotationBG":        func(p *Palette, c ui.Color) { p.AnnotationBG = c },
	"AnnotationActive":    func(p *Palette, c ui.Color) { p.AnnotationActive = c },
	"AnnotationDone":      func(p *Palette, c ui.Color) { p.AnnotationDone = c },
	"AnnotSendFG":         func(p *Palette, c ui.Color) { p.AnnotSendFG = c },
	"AnnotSendDot":        func(p *Palette, c ui.Color) { p.AnnotSendDot = c },
	"FindMatch":           func(p *Palette, c ui.Color) { p.FindMatch = c },
	"FindActive":          func(p *Palette, c ui.Color) { p.FindActive = c },
	"FindRing":            func(p *Palette, c ui.Color) { p.FindRing = c },
	"SearchHighlight":     func(p *Palette, c ui.Color) { p.SearchHighlight = c },
	"OutlineBarLit":       func(p *Palette, c ui.Color) { p.OutlineBarLit = c },
	"OutlineBarDim":       func(p *Palette, c ui.Color) { p.OutlineBarDim = c },
	"CodeLine":            func(p *Palette, c ui.Color) { p.CodeLine = c },
	"GutterText":          func(p *Palette, c ui.Color) { p.GutterText = c },
	"SyntaxKeyword":       func(p *Palette, c ui.Color) { p.SyntaxKeyword = c },
	"SyntaxString":        func(p *Palette, c ui.Color) { p.SyntaxString = c },
	"SyntaxComment":       func(p *Palette, c ui.Color) { p.SyntaxComment = c },
	"SyntaxNumber":        func(p *Palette, c ui.Color) { p.SyntaxNumber = c },
	"SyntaxFunction":      func(p *Palette, c ui.Color) { p.SyntaxFunction = c },
	"SyntaxType":          func(p *Palette, c ui.Color) { p.SyntaxType = c },
	"SyntaxKey":           func(p *Palette, c ui.Color) { p.SyntaxKey = c },
	"SyntaxTag":           func(p *Palette, c ui.Color) { p.SyntaxTag = c },
	"SyntaxLiteral":       func(p *Palette, c ui.Color) { p.SyntaxLiteral = c },
}

func applyColorMap(p *Palette, colors map[string]string) {
	for k, raw := range colors {
		setter, ok := paletteColorSetters[k]
		if !ok {
			continue
		}
		c, err := ParseHexColor(raw)
		if err != nil {
			continue
		}
		setter(p, c)
	}
}
