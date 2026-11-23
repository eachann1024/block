package plugins

import (
	"fmt"
	"strings"
)

// Category classifies plugins in the Block App Store.
//
// Categories answer one question: where does the plugin act?
//   - appearance: how the interface looks
//   - edit:       inside a single note
//   - organize:   between notes
//   - ai:         done by a model
//   - tools:      outside the notes (sync, import/export, publishing, integrations)
//
// CategoryLanguage (code highlighting) stays a valid manifest value without a
// store tab: language packs stay out of sight unless the user searches for
// them or already has them installed. The retired "sync" and "publish" values
// are still accepted in manifests and resolve to CategoryTools.
type Category string

const (
	CategoryAppearance Category = "appearance"
	CategoryEdit       Category = "edit"
	CategoryOrganize   Category = "organize"
	CategoryAI         Category = "ai"
	CategoryTools      Category = "tools"
	CategoryLanguage   Category = "language"
)

// storeCategories are the tabs shown in the App Store, in display order.
var storeCategories = []Category{
	CategoryAppearance,
	CategoryEdit,
	CategoryOrganize,
	CategoryAI,
	CategoryTools,
}

// orderedCategories lists every canonical manifest category: store tabs
// first, then the hidden language category.
var orderedCategories = append(append([]Category{}, storeCategories...), CategoryLanguage)

// retiredCategories are former store tabs that manifests may still declare;
// they resolve to their replacement category.
var retiredCategories = map[Category]Category{
	"sync":    CategoryTools,
	"publish": CategoryTools,
}

var validCategories = func() map[Category]bool {
	m := make(map[Category]bool, len(orderedCategories))
	for _, c := range orderedCategories {
		m[c] = true
	}
	return m
}()

// AllCategories returns every canonical manifest category: the store tabs in
// display order, followed by the hidden CategoryLanguage.
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

// RetiredCategory reports whether c is a former store tab that manifests may
// still declare but new plugins should no longer use.
func RetiredCategory(c Category) bool {
	_, ok := retiredCategories[c]
	return ok
}

// AcceptedCategory reports whether a manifest may declare c: a canonical
// category or a retired one that still resolves to a canonical category.
func AcceptedCategory(c Category) bool {
	_, retired := retiredCategories[c]
	return ValidCategory(c) || retired
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
			return "Editing"
		}
		return "编辑"
	case CategoryOrganize:
		if en {
			return "Organize"
		}
		return "整理"
	case CategoryAI:
		return "AI"
	case CategoryTools:
		if en {
			return "Tools"
		}
		return "工具"
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
	case CategoryEdit:
		if en {
			body = "Plugins that work inside a note, such as syntax, templates, images, and diagrams, will appear here once published."
		} else {
			body = "在一篇笔记里帮你干活的插件，比如语法扩展、模板、图片和图表，上架后会出现在这里。"
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
	case CategoryAppearance:
		if en {
			body = "Plugins for themes, fonts, and icons will appear here once published."
		} else {
			body = "换主题、换字体和图标的插件，上架后会出现在这里。"
		}
	case CategoryTools:
		if en {
			body = "Plugins that work outside your notes, such as backup, moving, sharing, and connecting other apps, will appear here once published."
		} else {
			body = "在笔记之外帮你干活的插件，比如备份、搬运、分享和其他软件打通，上架后会出现在这里。"
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

// ResolveCategory determines the effective App Store category for a plugin.
// The manifest alone decides the category: retired values map to their
// replacement, language packs default to CategoryLanguage, and everything
// else without a category falls back to CategoryTools.
func ResolveCategory(explicit Category, hasLanguages bool) Category {
	if ValidCategory(explicit) {
		return explicit
	}
	if replacement, ok := retiredCategories[explicit]; ok {
		return replacement
	}
	if hasLanguages {
		return CategoryLanguage
	}
	return CategoryTools
}
