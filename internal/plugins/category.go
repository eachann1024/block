package plugins

import "strings"

// Category classifies plugins in the Block App Store.
// Language extensions are always ordered last.
type Category string

const (
	CategorySync       Category = "sync"
	CategoryEdit       Category = "edit"
	CategoryAppearance Category = "appearance"
	CategoryTools      Category = "tools"
	CategoryLanguage   Category = "language"
)

var orderedCategories = []Category{
	CategorySync,
	CategoryEdit,
	CategoryAppearance,
	CategoryTools,
	CategoryLanguage,
}

var validCategories = map[Category]bool{
	CategorySync:       true,
	CategoryEdit:       true,
	CategoryAppearance: true,
	CategoryTools:      true,
	CategoryLanguage:   true,
}

// legacyPluginCategories provides backwards-compatible category defaults for
// existing store entries published before manifest.category was introduced.
var legacyPluginCategories = map[string]Category{
	"block-sync":        CategorySync,
	"block-migrator":    CategorySync,
	"block-image-host":  CategoryEdit,
	"block-canvas-dots": CategoryAppearance,
	"block-titlebar":    CategoryAppearance,
	"block-tarot":       CategoryTools,
}

// AllCategories returns the canonical App Store category order, with
// CategoryLanguage always last.
func AllCategories() []Category {
	out := make([]Category, len(orderedCategories))
	copy(out, orderedCategories)
	return out
}

// ValidCategory reports whether c is one of the canonical plugin categories.
func ValidCategory(c Category) bool {
	return validCategories[c]
}

// CategoryLabel returns the localized display name for c.
func CategoryLabel(c Category, locale string) string {
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	switch c {
	case CategorySync:
		if en {
			return "Sync & Migration"
		}
		return "同步与迁移"
	case CategoryEdit:
		if en {
			return "Writing & Editing"
		}
		return "编辑增强"
	case CategoryAppearance:
		if en {
			return "Appearance"
		}
		return "外观"
	case CategoryTools:
		if en {
			return "Tools"
		}
		return "工具"
	case CategoryLanguage:
		if en {
			return "Languages"
		}
		return "语言扩展"
	default:
		return string(c)
	}
}

// ResolveCategory determines the effective App Store category for a plugin,
// falling back gracefully for legacy manifests without an explicit category.
func ResolveCategory(explicit Category, pluginID string, hasLanguages bool) Category {
	if ValidCategory(explicit) {
		return explicit
	}
	if hasLanguages {
		return CategoryLanguage
	}
	if cat, ok := legacyPluginCategories[strings.ToLower(strings.TrimSpace(pluginID))]; ok {
		return cat
	}
	return CategoryTools
}
