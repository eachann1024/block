package pluginui

import (
	"strings"
	"testing"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
)

func TestStoreCategoryIconCoversAllCategoriesAndFallback(t *testing.T) {
	want := map[plugins.Category]icons.Icon{
		plugins.CategorySync:       icons.Refresh,
		plugins.CategoryEdit:       icons.Pencil,
		plugins.CategoryOrganize:   icons.Tag,
		plugins.CategoryAI:         icons.Sparkles,
		plugins.CategoryPublish:    icons.Upload,
		plugins.CategoryAppearance: icons.TbPalette,
		plugins.CategoryTools:      icons.Wrench,
		plugins.CategoryLanguage:   icons.Code,
	}
	for _, cat := range plugins.AllCategories() {
		got := storeCategoryIcon(cat)
		if got != want[cat] {
			t.Errorf("storeCategoryIcon(%q) = %q, want %q", cat, got, want[cat])
		}
	}
	if got := storeCategoryIcon(plugins.Category("future-cat")); got != icons.Package {
		t.Errorf("storeCategoryIcon(future-cat) = %q, want %q", got, icons.Package)
	}
}

func TestResolveStoreEmptySpecVariants(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	t.Cleanup(func() { theme.SetLanguage(model.LangZh) })

	ctrl := NewController(nil)
	var suggested []plugins.Category
	ctrl.SetOnSuggestPlugin(func(cat plugins.Category) {
		suggested = append(suggested, cat)
	})

	// 1. Empty category shows dedicated icon, copy, suggest button, and guide link.
	spec := resolveStoreEmptySpec(nil, ctrl, plugins.CategoryAI, "zh")
	if spec.glyph != icons.Sparkles {
		t.Fatalf("AI empty glyph = %q, want %q", spec.glyph, icons.Sparkles)
	}
	if spec.title != "「AI」暂无插件" || !strings.Contains(spec.body, "润色") {
		t.Fatalf("AI empty copy = (%q, %q)", spec.title, spec.body)
	}
	if !spec.showGuide || spec.primaryID != "app-store-empty-idea" {
		t.Fatalf("unexpected AI empty actions: %+v", spec)
	}
	spec.onPrimary()
	if len(suggested) != 1 || suggested[0] != plugins.CategoryAI {
		t.Fatalf("suggestPlugin callback got %v, want [%q]", suggested, plugins.CategoryAI)
	}

	// 2. Unknown future category falls back cleanly.
	fb := resolveStoreEmptySpec(nil, ctrl, plugins.Category("future"), "zh")
	if fb.glyph != icons.Package || fb.title != "这个分类暂无插件" || fb.body != "插件上架后会出现在这里。" {
		t.Fatalf("fallback spec = %+v", fb)
	}

	// 3. Search query with no results uses search icon, omits guide link, and clears query.
	ctrl.storeQuery = "思维导图"
	searchSpec := resolveStoreEmptySpec(nil, ctrl, plugins.CategoryAI, "zh")
	if searchSpec.glyph != icons.FindSearch || searchSpec.showGuide || searchSpec.primaryID != "app-store-empty-clear-search" {
		t.Fatalf("search empty spec = %+v", searchSpec)
	}
	if searchSpec.title != "没有找到「思维导图」相关插件" {
		t.Fatalf("search empty title = %q", searchSpec.title)
	}

	// 4. Source filter without query resets filters.
	ctrl.storeQuery = ""
	ctrl.storeSource = storeSourceOfficial
	filterSpec := resolveStoreEmptySpec(nil, ctrl, plugins.CategoryTools, "zh")
	if filterSpec.glyph != icons.FindSearch || filterSpec.showGuide || filterSpec.primaryID != "app-store-empty-reset-filter" {
		t.Fatalf("filter empty spec = %+v", filterSpec)
	}
}
