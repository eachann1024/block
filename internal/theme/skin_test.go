package theme

import (
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseHexColor(t *testing.T) {
	c6, err := ParseHexColor("#1a2b3c")
	if err != nil {
		t.Fatalf("ParseHexColor(#1a2b3c) failed: %v", err)
	}
	if c6 != ui.RGB(0x1a, 0x2b, 0x3c) {
		t.Fatalf("got %+v, want #1a2b3c", c6)
	}

	c8, err := ParseHexColor(" #c9a25a80 ")
	if err != nil {
		t.Fatalf("ParseHexColor(#c9a25a80) failed: %v", err)
	}
	if c8.R != 0xc9 || c8.G != 0xa2 || c8.B != 0x5a || c8.A != 0x80 {
		t.Fatalf("got %+v, want rgba(201,162,90,128)", c8)
	}

	for _, bad := range []string{"", "123456", "#fff", "#12345", "#1234567", "#123456789", "#gggggg", "rgb(1,2,3)"} {
		if _, err := ParseHexColor(bad); err == nil {
			t.Errorf("ParseHexColor(%q) expected error, got nil", bad)
		}
	}
}

func TestParseThemeSkinJSONAndValidation(t *testing.T) {
	validJSON := []byte(`{
		"id": "midnight-copper",
		"fonts": {
			"serif": "Noto Serif SC, serif",
			"display": "Cormorant Garamond, serif"
		},
		"chrome_dark": true,
		"light": {
			"Paper": "#f3ecdc",
			"Text": "#221d16",
			"Hover": "#7a5e2c17",
			"SyntaxKeyword": "#395fb0"
		},
		"dark": {
			"Shell": "#09111e",
			"Paper": "#0a1322",
			"Text": "#ede4cf",
			"SyntaxKeyword": "#9dbcf5"
		},
		"surfaces": {
			"paper": {
				"Paper": "#f4ecd9",
				"Accent": "#8a6526"
			},
			"chrome": {
				"Shell": "#08101c",
				"Accent": "#c9a25a"
			}
		},
		"quick_note": {
			"border_accent": "#c9a25a",
			"header_tint": "#09111e",
			"badge_text": {"zh": "速记", "en": "Quick Note"},
			"flash_accent": "#f3d48c"
		},
		"ai_indicator": {
			"style": "developing_card",
			"accent": "#c9a25a",
			"label": {"zh": "显影中", "en": "Developing"}
		}
	}`)

	spec, err := ParseThemeSkinJSON(validJSON)
	if err != nil {
		t.Fatalf("ParseThemeSkinJSON failed: %v", err)
	}
	if spec.ID != "midnight-copper" || !spec.ChromeDark {
		t.Fatalf("unexpected spec header: %+v", spec)
	}
	if spec.QuickNote == nil || spec.QuickNote.BadgeText.Resolve("en") != "Quick Note" {
		t.Fatalf("unexpected quick_note: %+v", spec.QuickNote)
	}
	if spec.AIIndicator == nil || spec.AIIndicator.Label.Resolve("zh") != "显影中" {
		t.Fatalf("unexpected ai_indicator: %+v", spec.AIIndicator)
	}

	invalidPayloads := []string{
		``,
		`{}`,
		`{"id":"Bad_ID","light":{"Paper":"#ffffff"}}`,
		`{"id":"ok-id"}`,
		`{"id":"ok-id","light":{"Paper":"not-a-color"}}`,
		`{"id":"ok-id","light":{"Paper":"#ffffff"},"quick_note":{"border_accent":"#123"}}`,
		`{"id":"ok-id","light":{"Paper":"#ffffff"},"ai_indicator":{"accent":"bad"}}`,
		`{"id":"ok-id","light":{"Paper":"#ffffff"},"unknown_field":1}`,
	}
	for i, raw := range invalidPayloads {
		if _, err := ParseThemeSkinJSON([]byte(raw)); err == nil {
			t.Errorf("case %d (%s): expected error", i, raw)
		}
	}
}

func TestActiveSkinStateAndApplySkinPalette(t *testing.T) {
	t.Cleanup(func() { ClearActiveSkin("") })
	ClearActiveSkin("")

	// No active skin returns base unchanged.
	if got := ApplySkinPalette(OceanLight, "paper"); got.Paper != OceanLight.Paper {
		t.Fatalf("expected OceanLight unchanged when no active skin")
	}
	if ActiveSerifFont() != "" || ActiveDisplayFont() != "" {
		t.Fatalf("expected empty fonts when no active skin")
	}

	spec, err := ParseThemeSkinJSON([]byte(`{
		"id": "copper-skin",
		"fonts": {
			"serif": "Noto Serif SC, serif",
			"display": "Georgia, serif"
		},
		"chrome_dark": true,
		"light": {
			"Paper": "#f3ecdc",
			"Text": "#221d16",
			"SyntaxKeyword": "#395fb0"
		},
		"dark": {
			"Shell": "#09111e",
			"Paper": "#0a1322",
			"Text": "#ede4cf",
			"SyntaxKeyword": "#9dbcf5"
		},
		"surfaces": {
			"paper": {
				"Paper": "#f4ecd9",
				"Accent": "#8a6526"
			},
			"chrome": {
				"Shell": "#08101c",
				"Accent": "#c9a25a"
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	SetActiveSkin("plugin-a", spec)
	if ActiveSkinOwner() != "plugin-a" || ActiveSkin() == nil || ActiveSkin().ID != "copper-skin" {
		t.Fatalf("unexpected active skin owner/spec: owner=%q spec=%+v", ActiveSkinOwner(), ActiveSkin())
	}
	if ActiveSerifFont() != "Noto Serif SC, serif" || ActiveDisplayFont() != "Georgia, serif" {
		t.Fatalf("unexpected active fonts: serif=%q display=%q", ActiveSerifFont(), ActiveDisplayFont())
	}

	// Empty surface follows base.Dark.
	lightPal := ApplySkinPalette(OceanLight, "")
	if lightPal.Dark || lightPal.Paper != ui.Hex("#f3ecdc") || lightPal.Text != ui.Hex("#221d16") {
		t.Fatalf("unexpected light palette: dark=%v paper=%+v", lightPal.Dark, lightPal.Paper)
	}
	darkPal := ApplySkinPalette(OceanDark, "")
	if !darkPal.Dark || darkPal.Shell != ui.Hex("#09111e") || darkPal.Paper != ui.Hex("#0a1322") {
		t.Fatalf("unexpected dark palette: dark=%v shell=%+v", darkPal.Dark, darkPal.Shell)
	}

	// Chrome surface honors chrome_dark=true even from OceanLight base.
	chromePal := ApplySkinPalette(OceanLight, "chrome")
	if !chromePal.Dark || chromePal.Shell != ui.Hex("#08101c") || chromePal.Accent != ui.Hex("#c9a25a") || chromePal.SyntaxKeyword != ui.Hex("#9dbcf5") {
		t.Fatalf("unexpected chrome palette: dark=%v shell=%+v accent=%+v", chromePal.Dark, chromePal.Shell, chromePal.Accent)
	}

	// A light paper surface is light-mode only: dark mode keeps the dark palette.
	if darkPaper := ApplySkinPalette(OceanDark, "paper"); !darkPaper.Dark || darkPaper.Paper != ui.Hex("#0a1322") {
		t.Fatalf("dark paper should follow the dark palette: dark=%v paper=%+v", darkPaper.Dark, darkPaper.Paper)
	}
	paperPal := ApplySkinPalette(OceanLight, "paper")
	if paperPal.Dark || paperPal.Paper != ui.Hex("#f4ecd9") || paperPal.Accent != ui.Hex("#8a6526") || paperPal.SyntaxKeyword != ui.Hex("#395fb0") {
		t.Fatalf("unexpected paper palette: dark=%v paper=%+v accent=%+v", paperPal.Dark, paperPal.Paper, paperPal.Accent)
	}

	// ClearActiveSkin by a different plugin must NOT clear plugin-a's skin.
	ClearActiveSkin("plugin-b")
	if ActiveSkin() == nil {
		t.Fatalf("ClearActiveSkin by non-owner should not clear active skin")
	}
	ClearActiveSkin("plugin-a")
	if ActiveSkin() != nil || ActiveSkinOwner() != "" {
		t.Fatalf("ClearActiveSkin by owner should clear active skin")
	}

	// Concurrent reads/writes must remain race-free.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if idx%2 == 0 {
					SetActiveSkin("plugin-a", spec)
				} else {
					_ = ApplySkinPalette(OceanLight, "paper")
					_ = ActiveSerifFont()
					_ = ActiveDisplayFont()
				}
			}
		}(i)
	}
	wg.Wait()
	ClearActiveSkin("")
}

func TestWithSurfaceOceanAndResolvedFonts(t *testing.T) {
	t.Cleanup(func() {
		ClearActiveSkin("")
		SetDark(false)
	})
	ClearActiveSkin("")
	SetDark(false)

	if got := Ocean(); got.Paper != OceanLight.Paper {
		t.Fatalf("expected OceanLight without active skin, got %+v", got.Paper)
	}
	defaultFont := ResolvedEditorFont()

	spec, err := ParseThemeSkinJSON([]byte(`{
		"id": "dual-surface",
		"fonts": {
			"serif": "Noto Serif SC, serif",
			"display": "Cormorant Garamond, serif"
		},
		"chrome_dark": true,
		"light": {
			"Paper": "#f3ecdc",
			"Text": "#221d16"
		},
		"dark": {
			"Shell": "#09111e",
			"Paper": "#0a1322"
		},
		"surfaces": {
			"paper": {"Paper": "#f4ecd9"},
			"chrome": {"Shell": "#08101c"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	SetActiveSkin("plugin-a", spec)
	if ResolvedEditorFont() != "Noto Serif SC, serif" {
		t.Fatalf("ResolvedEditorFont got %q, want Noto Serif SC, serif", ResolvedEditorFont())
	}
	if ResolvedDisplayFont() != "Cormorant Garamond, serif" {
		t.Fatalf("ResolvedDisplayFont got %q, want Cormorant Garamond, serif", ResolvedDisplayFont())
	}

	WithSurface("chrome", func() {
		chromePal := Ocean()
		if !chromePal.Dark || chromePal.Shell != ui.Hex("#08101c") {
			t.Fatalf("chrome surface unexpected: dark=%v shell=%+v", chromePal.Dark, chromePal.Shell)
		}
		WithSurface("paper", func() {
			paperPal := Ocean()
			if paperPal.Dark || paperPal.Paper != ui.Hex("#f4ecd9") {
				t.Fatalf("nested paper surface unexpected: dark=%v paper=%+v", paperPal.Dark, paperPal.Paper)
			}
		})
		afterNested := Ocean()
		if !afterNested.Dark || afterNested.Shell != ui.Hex("#08101c") {
			t.Fatalf("expected chrome surface restored after nested WithSurface, got dark=%v shell=%+v", afterNested.Dark, afterNested.Shell)
		}
	})

	ClearActiveSkin("plugin-a")
	if ResolvedEditorFont() != defaultFont {
		t.Fatalf("expected ResolvedEditorFont restored to %q, got %q", defaultFont, ResolvedEditorFont())
	}
}
