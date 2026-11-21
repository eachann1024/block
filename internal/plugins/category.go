package plugins

import (
	"fmt"
	"strings"
)

// Category classifies plugins in the Block App Store.
//
// Block is a note app, so the store only shows six everyday categories.
// CategoryTools ("other") and CategoryLanguage (code highlighting) stay valid
// manifest values for compatibility, but have no store tab: "other" plugins
// only appear under "All", and language packs stay out of sight unless the
// user searches for them or already has them installed.
type Category string

const (
	CategoryAppearance Category = "appearance"
	CategoryEdit       Category = "edit"
	CategoryOrganize   Category = "organize"
	CategoryAI         Category = "ai"
	CategoryPublish    Category = "publish"
	CategorySync       Category = "sync"
	CategoryTools      Category = "tools"
	CategoryLanguage   Category = "language"
)

// storeCategories are the tabs shown in the App Store, in display order.
var storeCategories = []Category{
	CategoryAppearance,
	CategoryEdit,
	CategoryOrganize,
	CategoryAI,
	CategoryPublish,
	CategorySync,
}

// orderedCategories lists every valid manifest category: store tabs first,
// then the hidden ones.
var orderedCategories = append(append([]Category{}, storeCategories...), CategoryTools, CategoryLanguage)

var validCategories = func() map[Category]bool {
	m := make(map[Category]bool, len(orderedCategories))
	for _, c := range orderedCategories {
		m[c] = true
	}
	return m
}()

// legacyPluginCategories provides backwards-compatible category defaults for
// existing store entries published before manifest.category was introduced.
var legacyPluginCategories = map[string]Category{
	"block-sync":        CategorySync,
	"block-migrator":    CategoryPublish,
	"block-image-host":  CategoryEdit,
	"block-canvas-dots": CategoryAppearance,
	"block-titlebar":    CategoryAppearance,
	"block-tarot":       CategoryAppearance,
}

// AllCategories returns every valid manifest category: the store tabs in
// display order, followed by the hidden CategoryTools and CategoryLanguage.
func AllCategories() []Category {
	out := make([]Category, len(orderedCategories))
	copy(out, orderedCategories)
	return out
}

// StoreCategories returns the categories shown as App Store tabs.
func StoreCategories() []Category {
	out := make([]Category, len(storeCategories))
	copy(out, storeCategories)
	return out
}

// AllowedCategoryNames returns the comma-separated canonical category IDs
// in display order, for use in validation errors and diagnostics.
func AllowedCategoryNames() string {
	names := make([]string, len(orderedCategories))
	for i, c := range orderedCategories {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}

// ValidCategory reports whether c is one of the canonical plugin categories.
func ValidCategory(c Category) bool {
	return validCategories[c]
}

// CategoryLabel returns the localized display name for c.
func CategoryLabel(c Category, locale string) string {
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	switch c {
	case CategoryAppearance:
		if en {
			return "Appearance"
		}
		return "外观"
	case CategoryEdit:
		if en {
			return "Writing"
		}
		return "写作"
	case CategoryOrganize:
		if en {
			return "Organize"
		}
		return "整理"
	case CategoryAI:
		return "AI"
	case CategoryPublish:
		if en {
			return "Import & Export"
		}
		return "导入导出"
	case CategorySync:
		if en {
			return "Sync"
		}
		return "同步"
	case CategoryTools:
		if en {
			return "Other"
		}
		return "其他"
	case CategoryLanguage:
		if en {
			return "Code Highlighting"
		}
		return "代码高亮"
	default:
		return string(c)
	}
}

// CategoryEmptyCopy returns the localized empty-state title and description for
// an App Store category. Unknown or future categories without dedicated copy
// fall back to a generic placeholder so newly added categories always render
// cleanly.
func CategoryEmptyCopy(c Category, locale string) (title, body string) {
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	if !ValidCategory(c) {
		if en {
			return "No plugins in this category yet", "Plugins will appear here once published."
		}
		return "这个分类暂无插件", "插件上架后会出现在这里。"
	}
	label := CategoryLabel(c, locale)
	if en {
		title = fmt.Sprintf("No plugins in %s yet", label)
	} else {
		title = fmt.Sprintf("「%s」暂无插件", label)
	}
	switch c {
	case CategorySync:
		if en {
			body = "Plugins that back up notes and keep them in sync across devices will appear here once published."
		} else {
			body = "把笔记存到网盘、多台设备随时查看的插件，上架后会出现在这里。"
		}
	case CategoryEdit:
		if en {
			body = "Plugins for formatting, templates, and proofreading will appear here once published."
		} else {
			body = "排版、模板、纠错这类让写作更顺手的插件，上架后会出现在这里。"
		}
	case CategoryOrganize:
		if en {
			body = "Plugins for tags, linked notes, outlines, and quick search will appear here once published."
		} else {
			body = "标签、笔记互链、目录与快速查找这类帮你理清笔记的插件，上架后会出现在这里。"
		}
	case CategoryAI:
		if en {
			body = "Plugins that polish, summarize, and answer questions about your notes will appear here once published."
		} else {
			body = "帮你润色、总结和随时问答的 AI 插件，上架后会出现在这里。"
		}
	case CategoryPublish:
		if en {
			body = "Plugins that bring notes in from other apps or export them as web pages and more will appear here once published."
		} else {
			body = "把别家笔记搬进来、或导出成网页等格式的插件，上架后会出现在这里。"
		}
	case CategoryAppearance:
		if en {
			body = "Plugins for themes, fonts, and icons will appear here once published."
		} else {
			body = "换主题、换字体和图标的插件，上架后会出现在这里。"
		}
	case CategoryTools:
		if en {
			body = "Plugins that do not fit other categories will appear here once published."
		} else {
			body = "暂时不好归类的插件，上架后会出现在这里。"
		}
	case CategoryLanguage:
		if en {
			body = "Highlighting packs for code blocks will appear here once published."
		} else {
			body = "代码块高亮扩展，上架后会出现在这里。"
		}
	default:
		if en {
			return "No plugins in this category yet", "Plugins will appear here once published."
		}
		return "这个分类暂无插件", "插件上架后会出现在这里。"
	}
	return title, body
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
