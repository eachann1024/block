package smoke

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"block/assets"
	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/plugins"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// nonAssocLanguageExtensions lists built-in language extensions that are
// intentionally excluded from OS file associations (for example, unsafe binary
// or multi-segment patterns).
var nonAssocLanguageExtensions = map[string]string{
	".exe": "unsafe native executable",
	".dll": "unsafe native library",
	".so":  "unsafe native library",
}

// genericIconExtensions lists built-in language or file-association extensions
// that intentionally fall back to the generic Code icon in the sidebar rather
// than a dedicated language icon.
var genericIconExtensions = map[string]bool{
	".atom":         true,
	".awk":          true,
	".cbl":          true,
	".clj":          true,
	".cljc":         true,
	".cljs":         true,
	".cob":          true,
	".coffee":       true,
	".command":      true,
	".cr":           true,
	".csh":          true,
	".csproj":       true,
	".cue":          true,
	".d":            true,
	".dart":         true,
	".di":           true,
	".diff":         true,
	".dna":          true,
	".editorconfig": true,
	".edn":          true,
	".elm":          true,
	".erb":          true,
	".erl":          true,
	".f":            true,
	".f90":          true,
	".f95":          true,
	".fish":         true,
	".fs":           true,
	".fsi":          true,
	".fsx":          true,
	".gleam":        true,
	".gql":          true,
	".graphql":      true,
	".graphqls":     true,
	".groovy":       true,
	".hcl":          true,
	".hrl":          true,
	".hs":           true,
	".ino":          true,
	".j2":           true,
	".jinja":        true,
	".jinja2":       true,
	".jl":           true,
	".ksh":          true,
	".lhs":          true,
	".lisp":         true,
	".lsp":          true,
	".m":            true,
	".mak":          true,
	".ml":           true,
	".mli":          true,
	".mm":           true,
	".mod":          true,
	".nim":          true,
	".nix":          true,
	".nu":           true,
	".odin":         true,
	".pas":          true,
	".patch":        true,
	".pcss":         true,
	".pl":           true,
	".pm":           true,
	".pp":           true,
	".proto":        true,
	".purs":         true,
	".r":            true,
	".rbs":          true,
	".rej":          true,
	".rhtml":        true,
	".rkt":          true,
	".rmd":          true,
	".rss":          true,
	".sbt":          true,
	".sc":           true,
	".scala":        true,
	".scm":          true,
	".sol":          true,
	".ss":           true,
	".storyboard":   true,
	".sum":          true,
	".sv":           true,
	".svelte":       true,
	".tex":          true,
	".tf":           true,
	".tfvars":       true,
	".tt":           true,
	".v":            true,
	".vala":         true,
	".vh":           true,
	".vhd":          true,
	".vhdl":         true,
	".wast":         true,
	".wat":          true,
	".webmanifest":  true,
	".wgsl":         true,
	".work":         true,
	".xib":          true,
	".zig":          true,
	".zon":          true,
	".zsh":          true,
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// internal/smoke -> repo root
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestCrossPlatformRegistryParity(t *testing.T) {
	root := repoRootDir(t)

	t.Run("FileAssociationsAndSidebarIcons", func(t *testing.T) {
		mjsPath := filepath.Join(root, "scripts", "file-associations.mjs")
		mjsRaw, err := os.ReadFile(mjsPath)
		if err != nil {
			t.Fatalf("read %s: %v", mjsPath, err)
		}
		if !strings.Contains(string(mjsRaw), "./cmd/block-fileassoc") {
			t.Fatalf("%s no longer delegates to ./cmd/block-fileassoc", mjsPath)
		}

		allAssoc := plugins.BuiltinAssocExtensions()
		if len(allAssoc) == 0 {
			t.Fatal("plugins.BuiltinAssocExtensions returned empty list")
		}
		assocSet := make(map[string]bool, len(allAssoc))
		for _, ext := range allAssoc {
			assocSet[ext] = true
		}

		mdBare := plugins.BuiltinMarkdownBareExtensions()
		docBare := plugins.BuiltinDocumentBareExtensions()
		if len(mdBare) == 0 || len(docBare) == 0 {
			t.Fatalf("expected non-empty bare extension lists: md=%d doc=%d", len(mdBare), len(docBare))
		}

		bareUnion := make(map[string]bool, len(mdBare)+len(docBare))
		for _, b := range mdBare {
			bareUnion["."+b] = true
		}
		for _, b := range docBare {
			bareUnion["."+b] = true
		}

		for _, ext := range allAssoc {
			bare := strings.TrimPrefix(ext, ".")
			if !strings.Contains(bare, ".") && !bareUnion[ext] {
				t.Errorf("single-segment BuiltinAssocExtensions entry %q missing from bare markdown/document lists", ext)
			}
		}

		for _, ext := range files.MarkdownExtensions() {
			if !assocSet[ext] {
				t.Errorf("Markdown extension %q missing from BuiltinAssocExtensions", ext)
			}
			if _, ok := icons.FileIcon("sample" + ext); !ok {
				t.Errorf("Markdown extension %q has no dedicated sidebar FileIcon", ext)
			}
		}

		mimeTypes := plugins.LinuxPackageMIMETypes()
		if len(mimeTypes) == 0 {
			t.Fatal("plugins.LinuxPackageMIMETypes returned empty list")
		}
		mimeSet := make(map[string]bool, len(mimeTypes))
		for _, m := range mimeTypes {
			mimeSet[m] = true
		}
		for _, req := range []string{"text/markdown", "text/plain"} {
			if !mimeSet[req] {
				t.Errorf("LinuxPackageMIMETypes missing required MIME type %q", req)
			}
		}

		for _, lang := range codeed.Languages() {
			if lang == nil {
				continue
			}
			for _, rawExt := range lang.Extensions {
				ext := strings.ToLower(strings.TrimSpace(rawExt))
				if ext == "" {
					continue
				}
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				if _, excluded := nonAssocLanguageExtensions[ext]; !excluded && !assocSet[ext] {
					t.Errorf("built-in language %q extension %q missing from BuiltinAssocExtensions (scripts/file-associations.mjs)", lang.Name, ext)
				}
				if _, ok := icons.FileIcon("sample" + ext); !ok && !genericIconExtensions[ext] {
					t.Errorf("built-in language %q extension %q has no sidebar FileIcon (and is not in genericIconExtensions)", lang.Name, ext)
				}
			}
		}
	})

	t.Run("DeclaredIconsResolveValidSVG", func(t *testing.T) {
		iconsPath := filepath.Join(root, "internal", "icons", "icons.go")
		iconConsts, mascotConsts := parseDeclaredIconConstants(t, iconsPath)
		if len(iconConsts) == 0 {
			t.Fatal("parsed 0 Icon constants from internal/icons/icons.go")
		}
		if len(mascotConsts) == 0 {
			t.Fatal("parsed 0 MascotState constants from internal/icons/icons.go")
		}

		layerSuffixes := []string{".fill.svg", ".shade.svg", ".line.svg", ".detail.svg", ".accent.svg"}
		for _, glyph := range iconConsts {
			if icons.LineSVG(glyph) == nil {
				t.Errorf("icons.LineSVG(%q) returned nil (missing or invalid .line.svg / .accent.svg)", glyph)
			}
			validLayers := 0
			for _, suffix := range layerSuffixes {
				rel := "icons/" + string(glyph) + suffix
				raw, err := assets.Read(rel)
				if err != nil {
					continue
				}
				parsed, err := ui.ParseSVG(raw)
				if err != nil || parsed == nil {
					t.Errorf("%s: ui.ParseSVG failed: parsed=%v err=%v", rel, parsed, err)
					continue
				}
				validLayers++
			}
			if validLayers == 0 {
				t.Errorf("icon %q has 0 valid SVG layers in assets/icons", glyph)
			}
		}

		for _, state := range mascotConsts {
			rel := "mascot/" + string(state) + ".1.svg"
			raw, err := assets.Read(rel)
			if err != nil {
				t.Errorf("mascot %q missing base layer %s: %v", state, rel, err)
				continue
			}
			parsed, err := ui.ParseSVG(raw)
			if err != nil || parsed == nil {
				t.Errorf("mascot %q base layer %s failed to parse: %v", state, rel, err)
			}
		}

		for _, kind := range []model.BackendKind{
			model.BackendCodex,
			model.BackendClaude,
			model.BackendDsh,
			model.BackendPi,
			model.BackendAgy,
		} {
			if kind.AssetPath() == "" {
				continue
			}
			if icons.AgentBrandSVG(kind) == nil {
				t.Errorf("icons.AgentBrandSVG(%q) returned nil for asset %q", kind, kind.AssetPath())
			}
		}
	})
}

func parseDeclaredIconConstants(t *testing.T, path string) ([]icons.Icon, []icons.MascotState) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var iconList []icons.Icon
	var mascotList []icons.MascotState
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		var lastType string
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); ok {
				lastType = id.Name
			} else if vs.Type != nil {
				lastType = ""
			}
			if lastType != "Icon" && lastType != "MascotState" {
				continue
			}
			for _, val := range vs.Values {
				bl, ok := val.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(bl.Value)
				if err != nil || unquoted == "" {
					continue
				}
				if lastType == "Icon" {
					iconList = append(iconList, icons.Icon(unquoted))
				} else {
					mascotList = append(mascotList, icons.MascotState(unquoted))
				}
			}
		}
	}
	return iconList, mascotList
}
