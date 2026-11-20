package icons

import (
	"testing"

	"block/assets"
	"github.com/egoist/mygo/ui"
)

func TestFileAndFolderSVGsParse(t *testing.T) {
	type layerSpec struct {
		icon   Icon
		fill   bool
		line   bool
		accent bool
	}

	specs := []layerSpec{
		// Reused existing (20)
		{Note, true, true, true},
		{LangPlain, true, true, true},
		{Code, true, true, true},
		{LangJs, true, true, true},
		{LangTs, true, true, true},
		{LangPython, true, true, true},
		{LangRust, true, true, true},
		{LangHtml, true, true, true},
		{LangCss, true, true, true},
		{LangBash, true, true, true},
		{LangSql, true, true, true},
		{LangJson, true, true, true},
		{LangYaml, true, true, true},
		{LangXml, true, true, true},
		{LangLean, true, true, true},
		{Image, true, true, true},
		{Video, true, true, true},
		{Folder, true, true, true},
		{FolderOpen, true, true, true},
		{Package, true, true, true},

		// New documents (6)
		{FilePdf, true, true, true},
		{FileWord, true, true, true},
		{FileSheet, true, true, true},
		{FileSlides, true, true, true},
		{FileLog, true, true, true},
		{FileBook, true, true, true},

		// New code languages (12)
		{LangReact, true, true, true},
		{LangGo, true, true, true},
		{LangJava, true, true, true},
		{LangKotlin, true, true, true},
		{LangSwift, true, true, false},
		{LangC, true, true, true},
		{LangCpp, true, true, true},
		{LangCsharp, true, true, true},
		{LangRuby, true, true, false},
		{LangPhp, true, true, true},
		{LangLua, true, true, true},
		{LangVue, true, true, true},

		// New config & data (3)
		{FileConfig, true, true, true},
		{FileEnv, true, true, true},
		{FileLock, true, true, true},

		// New media (3)
		{FileSvg, false, true, true},
		{FileAudio, true, true, true},
		{FileFont, true, true, false},

		// New archive, binary, special, store tag (8)
		{FileArchive, true, true, true},
		{FileBinary, true, true, true},
		{FileReadme, true, true, true},
		{FileLicense, true, true, true},
		{FileDocker, true, true, true},
		{FileGit, true, true, false},
		{Wrench, false, true, true},
		{Tag, true, true, true},

		// New semantic folders (6)
		{FolderSrc, true, true, true},
		{FolderDocs, true, true, true},
		{FolderAssets, true, true, true},
		{FolderTests, true, true, true},
		{FolderGit, true, true, true},
		{FolderDeps, true, true, true},
	}

	if len(specs) != 58 {
		t.Fatalf("expected 58 icon specs (56 file/folder + Tag + Wrench), got %d", len(specs))
	}

	checkLayer := func(glyph Icon, suffix string, expected bool) {
		t.Helper()
		rel := "icons/" + string(glyph) + "." + suffix + ".svg"
		raw, err := assets.Read(rel)
		if !expected {
			if err == nil {
				t.Errorf("%s: expected absent layer, but file exists (%d bytes)", rel, len(raw))
			}
			return
		}
		if err != nil {
			t.Errorf("%s: assets.Read failed: %v", rel, err)
			return
		}
		parsed, err := ui.ParseSVG(raw)
		if err != nil || parsed == nil {
			t.Errorf("%s: ui.ParseSVG failed: parsed=%v err=%v", rel, parsed, err)
		}
	}

	for _, sp := range specs {
		checkLayer(sp.icon, "fill", sp.fill)
		checkLayer(sp.icon, "line", sp.line)
		checkLayer(sp.icon, "accent", sp.accent)
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
