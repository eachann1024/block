package plugins

import (
	"slices"
	"strings"
	"testing"
)

func TestAllCategoriesOrderAndValidation(t *testing.T) {
	want := []Category{
		CategoryAppearance,
		CategoryEdit,
		CategoryOrganize,
		CategoryAI,
		CategoryTools,
		CategoryLanguage,
	}
	got := AllCategories()
	if !slices.Equal(got, want) {
		t.Fatalf("AllCategories() = %v, want %v", got, want)
	}
	if got[len(got)-1] != CategoryLanguage {
		t.Fatalf("expected %q last, got %q", CategoryLanguage, got[len(got)-1])
	}
	for _, c := range want {
		if !ValidCategory(c) {
			t.Errorf("ValidCategory(%q) = false, want true", c)
		}
		zh := CategoryLabel(c, "zh")
		en := CategoryLabel(c, "en")
		if zh == "" || zh == string(c) {
			t.Errorf("CategoryLabel(%q, zh) = %q, want localized label", c, zh)
		}
		if en == "" || en == string(c) {
			t.Errorf("CategoryLabel(%q, en) = %q, want localized label", c, en)
		}
	}
	for _, invalid := range []Category{"", "export", "unknown"} {
		if ValidCategory(invalid) {
			t.Errorf("ValidCategory(%q) = true, want false", invalid)
		}
	}
}

func TestCategoryEmptyCopyAndFallback(t *testing.T) {
	for _, c := range AllCategories() {
		zhTitle, zhBody := CategoryEmptyCopy(c, "zh")
		if !strings.Contains(zhTitle, CategoryLabel(c, "zh")) || zhBody == "" {
			t.Errorf("CategoryEmptyCopy(%q, zh) = (%q, %q)", c, zhTitle, zhBody)
		}
		enTitle, enBody := CategoryEmptyCopy(c, "en")
		if !strings.Contains(enTitle, CategoryLabel(c, "en")) || enBody == "" {
			t.Errorf("CategoryEmptyCopy(%q, en) = (%q, %q)", c, enTitle, enBody)
		}
	}

	fbTitle, fbBody := CategoryEmptyCopy(Category("future-cat"), "zh")
	if fbTitle != "这个分类暂无插件" || fbBody != "插件上架后会出现在这里。" {
		t.Fatalf("fallback zh = (%q, %q)", fbTitle, fbBody)
	}
	fbTitleEn, fbBodyEn := CategoryEmptyCopy(Category("future-cat"), "en")
	if fbTitleEn == "" || fbBodyEn == "" {
		t.Fatalf("fallback en empty: (%q, %q)", fbTitleEn, fbBodyEn)
	}
}

func TestManifestAcceptsNewCategories(t *testing.T) {
	base := func(cat Category) Manifest {
		return Manifest{
			App:           TargetApp,
			SchemaVersion: CurrentSchemaVersion,
			ID:            "sample-plugin",
			Category:      cat,
			Name:          LocalizedText{Zh: "示例插件", En: "Sample Plugin"},
			Version:       "1.0.0",
			Author:        "Block",
			Summary:       LocalizedText{Zh: "测试摘要", En: "Test summary"},
			Entry:         EntrySpec{JS: "plugin.js"},
			Surfaces:      []Surface{SurfaceSettingsPage},
			Icon:          AppStoreIconSpec(),
		}
	}
	for _, cat := range []Category{CategoryOrganize, CategoryAI, CategoryTools} {
		m := base(cat)
		if err := m.Validate(); err != nil {
			t.Errorf("Manifest.Validate() failed for category %q: %v", cat, err)
		}
		if m.EffectiveCategory() != cat {
			t.Errorf("EffectiveCategory() = %q, want %q", m.EffectiveCategory(), cat)
		}
	}
	for _, retired := range []Category{"sync", "publish"} {
		m := base(retired)
		if err := m.Validate(); err != nil {
			t.Errorf("Manifest.Validate() failed for retired category %q: %v", retired, err)
		}
		if m.EffectiveCategory() != CategoryTools {
			t.Errorf("EffectiveCategory(%q) = %q, want %q", retired, m.EffectiveCategory(), CategoryTools)
		}
	}
	invalid := base(Category("export"))
	err := invalid.Validate()
	if err == nil {
		t.Fatal("expected error for invalid category \"export\"")
	}
	if !strings.Contains(err.Error(), "organize") || !strings.Contains(err.Error(), "tools") {
		t.Fatalf("expected updated allowed list in error, got: %v", err)
	}
}
