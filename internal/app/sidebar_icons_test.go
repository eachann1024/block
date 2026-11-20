package app

import (
	"testing"

	"block/internal/model"
)

func TestSplitSidebarFileLabel(t *testing.T) {
	tests := []struct {
		name      string
		entry     model.FileEntry
		wantTitle string
		wantBadge string
	}{
		{
			name:      "canonical markdown hides extension",
			entry:     model.FileEntry{Path: "/nb/roadmap.md", Name: "roadmap", RawName: "roadmap.md"},
			wantTitle: "roadmap",
			wantBadge: "",
		},
		{
			name:      "dedicated code file keeps full filename without badge",
			entry:     model.FileEntry{Path: "/nb/main.go", Name: "main.go", RawName: "main.go"},
			wantTitle: "main.go",
			wantBadge: "",
		},
		{
			name:      "dedicated pdf keeps full filename without badge",
			entry:     model.FileEntry{Path: "/nb/api-spec.pdf", Name: "api-spec.pdf", RawName: "api-spec.pdf"},
			wantTitle: "api-spec.pdf",
			wantBadge: "",
		},
		{
			name:      "special filename keeps full name without badge",
			entry:     model.FileEntry{Path: "/nb/package.json", Name: "package.json", RawName: "package.json"},
			wantTitle: "package.json",
			wantBadge: "",
		},
		{
			name:      "unknown extension retains fallback badge",
			entry:     model.FileEntry{Path: "/nb/custom.xyz", Name: "custom.xyz", RawName: "custom.xyz"},
			wantTitle: "custom",
			wantBadge: "XYZ",
		},
		{
			name:      "folder stays badge-free",
			entry:     model.FileEntry{Path: "/nb/src", Name: "src", RawName: "src", IsDir: true},
			wantTitle: "src",
			wantBadge: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTitle, gotBadge := splitSidebarFileLabel(tc.entry)
			if gotTitle != tc.wantTitle || gotBadge != tc.wantBadge {
				t.Fatalf("splitSidebarFileLabel(%+v) = (%q, %q), want (%q, %q)",
					tc.entry, gotTitle, gotBadge, tc.wantTitle, tc.wantBadge)
			}
		})
	}
}
