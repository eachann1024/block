package icons

import (
	"io/fs"
	"strings"
	"testing"

	"block/assets"
	"github.com/egoist/mygo/ui"
)

func TestFileAndFolderSVGsParse(t *testing.T) {
	entries, err := fs.ReadDir(assets.FS, "icons")
	if err != nil {
		t.Fatalf("fs.ReadDir(icons): %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("assets/icons is empty")
	}

	validSuffixes := map[string]bool{
		"fill":   true,
		"shade":  true,
		"line":   true,
		"detail": true,
		"accent": true,
	}
	layersByIcon := make(map[Icon]map[string]bool)

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".svg") {
			continue
		}
		stem := strings.TrimSuffix(name, ".svg")
		dot := strings.LastIndexByte(stem, '.')
		if dot <= 0 || dot == len(stem)-1 {
			t.Errorf("icons/%s: expected <name>.<layer>.svg", name)
			continue
		}
		glyph := Icon(stem[:dot])
		layer := stem[dot+1:]
		if !validSuffixes[layer] {
			t.Errorf("icons/%s: unexpected layer suffix %q", name, layer)
			continue
		}
		rel := "icons/" + name
		raw, err := assets.Read(rel)
		if err != nil {
			t.Errorf("%s: assets.Read failed: %v", rel, err)
			continue
		}
		parsed, err := ui.ParseSVG(raw)
		if err != nil || parsed == nil {
			t.Errorf("%s: ui.ParseSVG failed: parsed=%v err=%v", rel, parsed, err)
			continue
		}
		if layersByIcon[glyph] == nil {
			layersByIcon[glyph] = make(map[string]bool)
		}
		layersByIcon[glyph][layer] = true
	}

	if len(layersByIcon) != 190 {
		t.Errorf("expected 190 distinct icons in assets/icons, got %d", len(layersByIcon))
	}

	// Every file/folder/tag/wrench icon must exist with at least a primary line layer.
	required := []Icon{
		Note, LangPlain, Code, LangJs, LangTs, LangPython, LangRust, LangHtml, LangCss,
		LangBash, LangSql, LangJson, LangYaml, LangXml, LangLean, Image, Video, Folder,
		FolderOpen, Package, FilePdf, FileWord, FileSheet, FileSlides, FileLog, FileBook,
		LangReact, LangGo, LangJava, LangKotlin, LangSwift, LangC, LangCpp, LangCsharp,
		LangRuby, LangPhp, LangLua, LangVue, FileConfig, FileEnv, FileLock, FileSvg,
		FileAudio, FileFont, FileArchive, FileBinary, FileReadme, FileLicense, FileDocker,
		FileGit, Wrench, Tag, FolderSrc, FolderDocs, FolderAssets, FolderTests, FolderGit,
		FolderDeps,
	}
	for _, glyph := range required {
		ls := layersByIcon[glyph]
		if !ls["line"] {
			t.Errorf("icon %q: missing required .line.svg layer", glyph)
		}
	}
	for glyph, ls := range layersByIcon {
		if !ls["line"] && !ls["accent"] {
			t.Errorf("icon %q: must have at least .line.svg or .accent.svg", glyph)
		}
	}
}

func TestFileIcon(t *testing.T) {
	cases := []struct {
		name string
		want Icon
		ok   bool
	}{
		// Special filenames take precedence over extensions
		{"README", FileReadme, true},
		{"README.md", FileReadme, true},
		{"docs/readme.txt", FileReadme, true},
		{"LICENSE", FileLicense, true},
		{"LICENSE.md", FileLicense, true},
		{"LICENCE", FileLicense, true},
		{"COPYING", FileLicense, true},
		{"NOTICE", FileLicense, true},
		{".editorconfig", FileConfig, true},
		{"Dockerfile", FileDocker, true},
		{"Dockerfile.prod", FileDocker, true},
		{"app.dockerfile", FileDocker, true},
		{".dockerignore", FileDocker, true},
		{"compose.yml", FileDocker, true},
		{"compose.yaml", FileDocker, true},
		{"docker-compose.dev.yml", FileDocker, true},
		{"Makefile", Wrench, true},
		{"GNUmakefile", Wrench, true},
		{"justfile", Wrench, true},
		{".justfile", Wrench, true},
		{"CMakeLists.txt", Wrench, true},
		{"Rakefile", Wrench, true},
		{"Taskfile.yml", Wrench, true},
		{"build/rules.mk", Wrench, true},
		{".gitignore", FileGit, true},
		{".gitattributes", FileGit, true},
		{".gitmodules", FileGit, true},
		{".gitkeep", FileGit, true},
		{".gitconfig", FileGit, true},
		{"package.json", Package, true},
		{"go.mod", Package, true},
		{"go.work", Package, true},
		{"Cargo.toml", Package, true},
		{"pyproject.toml", Package, true},
		{"requirements.txt", Package, true},
		{"composer.json", Package, true},
		{"Gemfile", Package, true},
		{"pom.xml", Package, true},
		{"build.gradle", Package, true},
		{"pubspec.yaml", Package, true},
		{"deno.json", Package, true},
		{"bunfig.toml", Package, true},
		{".env", FileEnv, true},
		{".env.local", FileEnv, true},
		{"/srv/app/.ENV.PRODUCTION", FileEnv, true},
		{"go.sum", FileLock, true},
		{"package-lock.json", FileLock, true},
		{"pnpm-lock.yaml", FileLock, true},
		{"yarn.lock", FileLock, true},
		{"poetry.lock", FileLock, true},
		{"Gemfile.lock", FileLock, true},
		{"composer.lock", FileLock, true},
		{"Cargo.lock", FileLock, true},

		// Documents
		{"notes.md", Note, true},
		{"notes.MARKDOWN", Note, true},
		{"intro.mdx", Note, true},
		{"plain.txt", LangPlain, true},
		{"plain.text", LangPlain, true},
		{"report.pdf", FilePdf, true},
		{"spec.docx", FileWord, true},
		{"letter.rtf", FileWord, true},
		{"memo.pages", FileWord, true},
		{"data.csv", FileSheet, true},
		{"metrics.xlsx", FileSheet, true},
		{"budget.numbers", FileSheet, true},
		{"deck.pptx", FileSlides, true},
		{"talk.key", FileSlides, true},
		{"app.log", FileLog, true},
		{"nohup.out", FileLog, true},
		{"book.epub", FileBook, true},
		{"book.mobi", FileBook, true},

		// Code
		{"app.js", LangJs, true},
		{"mod.mjs", LangJs, true},
		{"mod.cjs", LangJs, true},
		{"index.ts", LangTs, true},
		{"index.mts", LangTs, true},
		{"index.cts", LangTs, true},
		{"Button.jsx", LangReact, true},
		{"Button.tsx", LangReact, true},
		{"script.py", LangPython, true},
		{"types.pyi", LangPython, true},
		{"nb.ipynb", LangPython, true},
		{"main.go", LangGo, true},
		{"lib.rs", LangRust, true},
		{"Main.java", LangJava, true},
		{"app.jar", LangJava, true},
		{"Main.class", LangJava, true},
		{"Main.kt", LangKotlin, true},
		{"build.kts", LangKotlin, true},
		{"App.swift", LangSwift, true},
		{"main.c", LangC, true},
		{"header.h", LangC, true},
		{"engine.cpp", LangCpp, true},
		{"engine.cc", LangCpp, true},
		{"engine.cxx", LangCpp, true},
		{"engine.hpp", LangCpp, true},
		{"Program.cs", LangCsharp, true},
		{"script.csx", LangCsharp, true},
		{"app.rb", LangRuby, true},
		{"my.gemspec", LangRuby, true},
		{"index.php", LangPhp, true},
		{"init.lua", LangLua, true},
		{"App.vue", LangVue, true},
		{"index.html", LangHtml, true},
		{"index.htm", LangHtml, true},
		{"style.css", LangCss, true},
		{"style.scss", LangCss, true},
		{"style.sass", LangCss, true},
		{"style.less", LangCss, true},
		{"run.sh", LangBash, true},
		{"run.bash", LangBash, true},
		{"run.zsh", LangBash, true},
		{"run.fish", LangBash, true},
		{"schema.sql", LangSql, true},
		{"data.db", LangSql, true},
		{"data.sqlite", LangSql, true},
		{"Proof.lean", LangLean, true},

		// Config & Data
		{"data.json", LangJson, true},
		{"tsconfig.jsonc", LangJson, true},
		{"data.json5", LangJson, true},
		{"config.yaml", LangYaml, true},
		{"config.yml", LangYaml, true},
		{"feed.xml", LangXml, true},
		{"Info.plist", LangXml, true},
		{"schema.xsd", LangXml, true},
		{"config.toml", FileConfig, true},
		{"php.ini", FileConfig, true},
		{"nginx.conf", FileConfig, true},
		{"setup.cfg", FileConfig, true},

		// Media
		{"shot.png", Image, true},
		{"photo.jpg", Image, true},
		{"photo.jpeg", Image, true},
		{"anim.gif", Image, true},
		{"pic.webp", Image, true},
		{"live.heic", Image, true},
		{"fav.ico", Image, true},
		{"logo.svg", FileSvg, true},
		{"clip.mp4", Video, true},
		{"clip.mov", Video, true},
		{"clip.webm", Video, true},
		{"clip.mkv", Video, true},
		{"song.mp3", FileAudio, true},
		{"sound.wav", FileAudio, true},
		{"track.flac", FileAudio, true},
		{"voice.m4a", FileAudio, true},
		{"Inter.ttf", FileFont, true},
		{"Inter.otf", FileFont, true},
		{"Inter.woff", FileFont, true},
		{"Inter.woff2", FileFont, true},

		// Archives & Binaries
		{"bundle.zip", FileArchive, true},
		{"src.tar", FileArchive, true},
		{"src.tar.gz", FileArchive, true},
		{"archive.7z", FileArchive, true},
		{"archive.rar", FileArchive, true},
		{"setup.exe", FileBinary, true},
		{"Block.dmg", FileBinary, true},
		{"Block.app", FileBinary, true},
		{"firmware.bin", FileBinary, true},
		{"disk.iso", FileBinary, true},

		// Unmatched fallback
		{"unknown.xyz", Code, false},
		{"noext", Code, false},
		{"", Code, false},
		{"/", Code, false},
	}

	for _, tc := range cases {
		got, ok := FileIcon(tc.name)
		if got != tc.want || ok != tc.ok {
			t.Errorf("FileIcon(%q) = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
		if def := FileIconOrDefault(tc.name); def != tc.want {
			t.Errorf("FileIconOrDefault(%q) = %q, want %q", tc.name, def, tc.want)
		}
	}
}

func TestFolderIcon(t *testing.T) {
	semantic := []struct {
		name string
		want Icon
	}{
		{"src", FolderSrc},
		{"/repo/LIB/", FolderSrc},
		{"cmd", FolderSrc},
		{"internal", FolderSrc},
		{"docs", FolderDocs},
		{"Documentation", FolderDocs},
		{"assets", FolderAssets},
		{"public", FolderAssets},
		{"attachments", FolderAssets},
		{"test", FolderTests},
		{"__tests__", FolderTests},
		{"testdata", FolderTests},
		{".git", FolderGit},
		{".github", FolderGit},
		{"node_modules", FolderDeps},
		{"vendor", FolderDeps},
		{".venv", FolderDeps},
	}

	for _, tc := range semantic {
		if got := FolderIcon(tc.name, false); got != tc.want {
			t.Errorf("FolderIcon(%q, false) = %q, want %q", tc.name, got, tc.want)
		}
		if got := FolderIcon(tc.name, true); got != tc.want {
			t.Errorf("FolderIcon(%q, true) = %q, want %q", tc.name, got, tc.want)
		}
	}

	if got := FolderIcon("custom-dir", false); got != Folder {
		t.Errorf("FolderIcon(custom-dir, false) = %q, want %q", got, Folder)
	}
	if got := FolderIcon("custom-dir", true); got != FolderOpen {
		t.Errorf("FolderIcon(custom-dir, true) = %q, want %q", got, FolderOpen)
	}
	if got := FolderIcon("", false); got != Folder {
		t.Errorf("FolderIcon(\"\", false) = %q, want %q", got, Folder)
	}
	if got := FolderIcon("", true); got != FolderOpen {
		t.Errorf("FolderIcon(\"\", true) = %q, want %q", got, FolderOpen)
	}
}
