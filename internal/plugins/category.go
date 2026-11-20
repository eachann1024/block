package plugins

import (
	"fmt"
	"strings"
)

// Category classifies plugins in the Block App Store.
// Language extensions are always ordered last.
type Category string

const (
	CategorySync       Category = "sync"
	CategoryEdit       Category = "edit"
	CategoryOrganize   Category = "organize"
	CategoryAI         Category = "ai"
	CategoryPublish    Category = "publish"
	CategoryAppearance Category = "appearance"
	CategoryTools      Category = "tools"
	CategoryLanguage   Category = "language"
)

var orderedCategories = []Category{
	CategorySync,
	CategoryEdit,
	CategoryOrganize,
	CategoryAI,
	CategoryPublish,
	CategoryAppearance,
	CategoryTools,
	CategoryLanguage,
}

var validCategories = map[Category]bool{
	CategorySync:       true,
	CategoryEdit:       true,
	CategoryOrganize:   true,
	CategoryAI:         true,
	CategoryPublish:    true,
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
	case CategoryOrganize:
		if en {
			return "Organize & Search"
		}
		return "整理与检索"
	case CategoryAI:
		if en {
			return "AI Enhancements"
		}
		return "AI 增强"
	case CategoryPublish:
		if en {
			return "Export & Publish"
		}
		return "导出与发布"
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
			body = "Plugins for multi-device sync and note migration will appear here once published."
		} else {
			body = "多端同步与外部笔记迁移类插件，上架后会出现在这里。"
		}
	case CategoryEdit:
		if en {
			body = "Plugins that enhance writing and editing workflows will appear here once published."
		} else {
			body = "增强写作与编辑体验的插件，上架后会出现在这里。"
		}
	case CategoryOrganize:
		if en {
			body = "Plugins for tags, backlinks, and full-text search will appear here once published."
		} else {
			body = "标签、反向链接、全文检索这类帮你理清笔记的插件，上架后会出现在这里。"
		}
	case CategoryAI:
		if en {
			body = "Plugins for extra AI models, prompt templates, and writing assistants will appear here once published."
		} else {
			body = "接入更多模型、提示词模板与写作助手的插件，上架后会出现在这里。"
		}
	case CategoryPublish:
		if en {
			body = "Plugins for extra export formats and publishing to blogs or newsletters will appear here once published."
		} else {
			body = "导出为更多格式、一键发布到博客或公众号的插件，上架后会出现在这里。"
		}
	case CategoryAppearance:
		if en {
			body = "Plugins for customizing appearance, themes, and backgrounds will appear here once published."
		} else {
			body = "自定义界面样式、主题与背景的插件，上架后会出现在这里。"
		}
	case CategoryTools:
		if en {
			body = "Utility and productivity plugins will appear here once published."
		} else {
			body = "实用小工具与效率扩展类插件，上架后会出现在这里。"
		}
	case CategoryLanguage:
		if en {
			body = "Syntax highlighting extensions for code files and fenced blocks will appear here once published."
		} else {
			body = "源码与代码块语法高亮扩展，上架后会出现在这里。"
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
