package files

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"block/internal/model"
	"block/internal/theme"
)

func setupTestDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetDataDirOverride(dir)
	t.Cleanup(func() {
		SetDataDirOverride("")
	})
	return dir
}

func TestDataDirAndLogging(t *testing.T) {
	t.Setenv("BLOCK_DATA_DIR", filepath.Join(t.TempDir(), "env-block"))
	SetDataDirOverride("")
	if got, want := AppDataDir(), filepath.Clean(os.Getenv("BLOCK_DATA_DIR")); got != want {
		t.Fatalf("AppDataDir env = %q, want %q", got, want)
	}

	overrideDir := setupTestDataDir(t)
	if got := AppDataDir(); got != overrideDir {
		t.Fatalf("AppDataDir override = %q, want %q", got, overrideDir)
	}

	LogWarnf("warn message %d", 42)
	LogErrorf("error message %s", "boom")

	logBytes, err := os.ReadFile(filepath.Join(overrideDir, "logs", "block.log"))
	if err != nil {
		t.Fatalf("read block.log: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "[WARN]") || !strings.Contains(logText, "warn message 42") {
		t.Fatalf("expected WARN line in block.log, got %q", logText)
	}
	if !strings.Contains(logText, "[ERROR]") || !strings.Contains(logText, "error message boom") {
		t.Fatalf("expected ERROR line in block.log, got %q", logText)
	}
}

func TestStateLoadSaveUpdateAndCorruptBackup(t *testing.T) {
	dataDir := setupTestDataDir(t)

	// Missing state.json returns defaults.
	st := LoadState()
	if st.Theme != model.ThemeSystem || st.AssetsFolder != "assets" {
		t.Fatalf("unexpected default state: %+v", st)
	}

	// Corrupt state.json is backed up to state.json.corrupt and falls back to defaults.
	statePath := filepath.Join(dataDir, "state.json")
	if err := os.WriteFile(statePath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st = LoadState()
	if st.Theme != model.ThemeSystem {
		t.Fatalf("expected default theme after corrupt state, got %v", st.Theme)
	}
	if _, err := os.Stat(statePath + ".corrupt"); err != nil {
		t.Fatalf("expected .corrupt backup: %v", err)
	}

	// UpdateState caps notebooks at 12.
	var notebooks []string
	for i := range 15 {
		notebooks = append(notebooks, fmt.Sprintf("/tmp/nb-%02d", i))
	}
	updated, err := UpdateState(func(s *model.AppState) {
		s.Notebooks = notebooks
		s.Folder = "/tmp/nb-00"
	})
	if err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if len(updated.Notebooks) != 12 {
		t.Fatalf("expected 12 notebooks max, got %d", len(updated.Notebooks))
	}

	reloaded := LoadState()
	if len(reloaded.Notebooks) != 12 || reloaded.Folder != "/tmp/nb-00" {
		t.Fatalf("reloaded mismatch: %+v", reloaded)
	}

	reloaded.Theme = model.ThemeDark
	if err := SaveState(reloaded); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if got := LoadState().Theme; got != model.ThemeDark {
		t.Fatalf("expected dark theme after SaveState, got %v", got)
	}

	// OpenTabs deduplication and ActiveTab normalization.
	tabState, err := UpdateState(func(s *model.AppState) {
		s.OpenTabs = []string{"", "/tmp/nb-00/./a.md", "/tmp/nb-00/b.md", "/tmp/nb-00/a.md"}
		s.ActiveTab = "/tmp/nb-00/missing.md"
	})
	if err != nil {
		t.Fatalf("UpdateState tabs: %v", err)
	}
	if len(tabState.OpenTabs) != 2 || tabState.OpenTabs[0] != "/tmp/nb-00/a.md" || tabState.OpenTabs[1] != "/tmp/nb-00/b.md" {
		t.Fatalf("unexpected normalized OpenTabs: %v", tabState.OpenTabs)
	}
	if tabState.ActiveTab != "/tmp/nb-00/a.md" {
		t.Fatalf("expected fallback ActiveTab /tmp/nb-00/a.md, got %q", tabState.ActiveTab)
	}
}

func TestScanNotebookSortingFilteringAndFileOrder(t *testing.T) {
	_ = setupTestDataDir(t)
	root := t.TempDir()

	// Create folders & files.
	for _, dir := range []string{"FolderB", "FolderA", "assets", ".hidden", "custom_hide", "node_modules"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"note10.md", "note2.md", "note1.md", "笔记2.md", "笔记10.md", "image.png", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("# "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	theme.SetFolderRules([]model.FolderRule{
		{Pattern: "^custom_.*$", Regex: true, Hidden: true},
	})
	t.Cleanup(func() { theme.SetFolderRules(nil) })

	entries, err := ScanNotebook(root, ScanOptions{
		HideNonMarkdown: true,
		HideAssets:      true,
		AssetsFolder:    "assets",
	})
	if err != nil {
		t.Fatalf("ScanNotebook: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.RawName)
	}
	want := []string{"FolderA", "FolderB", "note1.md", "note2.md", "note10.md", "笔记2.md", "笔记10.md"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("sorted/filtered names = %v, want %v", names, want)
	}

	// Custom FileOrder reorders folders and files within their own groups while
	// folders always retain higher priority than files.
	entriesOrdered, err := ScanNotebook(root, ScanOptions{
		HideNonMarkdown: true,
		HideAssets:      true,
		AssetsFolder:    "assets",
		FileOrder: map[string][]string{
			root: {
				filepath.Join(root, "note10.md"),
				filepath.Join(root, "FolderB"),
			},
		},
	})
	if err != nil {
		t.Fatalf("ScanNotebook ordered: %v", err)
	}
	var orderedNames []string
	for _, e := range entriesOrdered {
		orderedNames = append(orderedNames, e.RawName)
	}
	wantOrdered := []string{"FolderB", "FolderA", "note10.md", "note1.md", "note2.md", "笔记2.md", "笔记10.md"}
	if strings.Join(orderedNames, ",") != strings.Join(wantOrdered, ",") {
		t.Fatalf("custom FileOrder with folder priority: got %v, want %v", orderedNames, wantOrdered)
	}

	// Newly created folders always sort at the bottom of folders and above all files across all sort modes.
	for _, dir := range []string{"Zebra", "阅读"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nf1, err := CreateNewFolder(root, false)
	if err != nil {
		t.Fatal(err)
	}
	nf2, err := CreateNewFolder(root, false)
	if err != nil {
		t.Fatal(err)
	}
	en1, err := CreateNewFolder(root, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = os.Chtimes(filepath.Join(root, "FolderA"), now.Add(-30*time.Second), now.Add(-30*time.Second))
	_ = os.Chtimes(filepath.Join(root, "Zebra"), now.Add(-20*time.Second), now.Add(-20*time.Second))
	_ = os.Chtimes(nf1, now.Add(-3*time.Second), now.Add(-3*time.Second))
	_ = os.Chtimes(nf2, now.Add(-2*time.Second), now.Add(-2*time.Second))
	_ = os.Chtimes(en1, now.Add(-1*time.Second), now.Add(-1*time.Second))
	_ = os.Chtimes(filepath.Join(root, "note1.md"), now, now)

	for _, mode := range []model.TreeSortMode{model.TreeSortManual, model.TreeSortName, model.TreeSortModified} {
		gotEntries, err := ScanNotebook(root, ScanOptions{
			HideNonMarkdown: true,
			HideAssets:      true,
			AssetsFolder:    "assets",
			SortMode:        mode,
			FileOrder: map[string][]string{
				root: {"note10.md", "FolderB"},
			},
		})
		if err != nil {
			t.Fatalf("ScanNotebook mode=%q: %v", mode, err)
		}
		// All 7 folders must come before all 5 files, and the 3 default new folders must be the last 3 folders.
		for i, e := range gotEntries {
			wantDir := i < 7
			if e.IsDir != wantDir {
				t.Fatalf("mode=%q idx=%d (%s) IsDir=%v, want %v", mode, i, e.RawName, e.IsDir, wantDir)
			}
		}
		tailDirs := []string{gotEntries[4].RawName, gotEntries[5].RawName, gotEntries[6].RawName}
		wantTail := []string{"新建文件夹", "新建文件夹 2", "New Folder"}
		if strings.Join(tailDirs, ",") != strings.Join(wantTail, ",") {
			t.Fatalf("mode=%q bottom folders = %v, want %v", mode, tailDirs, wantTail)
		}
	}
}

func TestOpsCreateRenameMoveTrashReadWriteRecovery(t *testing.T) {
	dataDir := setupTestDataDir(t)
	root := t.TempDir()

	// Exclusive note creation (zh & en).
	n1, err := CreateNewNote(root, false)
	if err != nil || filepath.Base(n1) != "未命名.md" {
		t.Fatalf("CreateNewNote zh #1 = (%q, %v)", n1, err)
	}
	n2, err := CreateNewNote(root, false)
	if err != nil || filepath.Base(n2) != "未命名 2.md" {
		t.Fatalf("CreateNewNote zh #2 = (%q, %v)", n2, err)
	}
	e1, err := CreateNewNote(root, true)
	if err != nil || filepath.Base(e1) != "Untitled.md" {
		t.Fatalf("CreateNewNote en #1 = (%q, %v)", e1, err)
	}
	e2, err := CreateNewNote(root, true)
	if err != nil || filepath.Base(e2) != "Untitled 2.md" {
		t.Fatalf("CreateNewNote en #2 = (%q, %v)", e2, err)
	}

	// Exclusive folder creation.
	f1, err := CreateNewFolder(root, true)
	if err != nil || filepath.Base(f1) != "New Folder" {
		t.Fatalf("CreateNewFolder #1 = (%q, %v)", f1, err)
	}
	f2, err := CreateNewFolder(root, true)
	if err != nil || filepath.Base(f2) != "New Folder 2" {
		t.Fatalf("CreateNewFolder #2 = (%q, %v)", f2, err)
	}

	// Write & Read atomic.
	fpWrite, err := WriteNoteAtomic(e1, "# Hello\nWorld\n")
	if err != nil {
		t.Fatalf("WriteNoteAtomic: %v", err)
	}
	content, fpRead, err := ReadNote(e1)
	if err != nil || content != "# Hello\nWorld\n" || fpRead != fpWrite {
		t.Fatalf("ReadNote = (%q, %d, %v), want fp %d", content, fpRead, err, fpWrite)
	}

	// Recovery save & clear.
	if err := SaveRecovery(e1, "draft content"); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}
	recFiles, err := os.ReadDir(filepath.Join(dataDir, "Recovery"))
	if err != nil || len(recFiles) != 1 {
		t.Fatalf("expected 1 recovery file, got %d (%v)", len(recFiles), err)
	}
	ClearRecovery(e1)
	recFiles, _ = os.ReadDir(filepath.Join(dataDir, "Recovery"))
	if len(recFiles) != 0 {
		t.Fatalf("expected 0 recovery files after ClearRecovery, got %d", len(recFiles))
	}

	// Rename note preserves .md extension.
	renamed, err := RenameItem(e1, "RenamedNote")
	if err != nil || filepath.Base(renamed) != "RenamedNote.md" {
		t.Fatalf("RenameItem = (%q, %v)", renamed, err)
	}

	// Move note into f1 and reject moving folder into itself.
	moved, err := MoveItem(renamed, f1)
	if err != nil || moved != filepath.Join(f1, "RenamedNote.md") {
		t.Fatalf("MoveItem = (%q, %v)", moved, err)
	}
	if _, err := MoveItem(f1, f1); err == nil {
		t.Fatal("expected MoveItem into itself to fail")
	}

	// TrashItem fallback moves/removes safely in test env.
	if err := TrashItem(moved); err != nil {
		t.Fatalf("TrashItem: %v", err)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("expected trashed item to be absent at %s", moved)
	}
}

func TestSearchNotebooksAndIgnoredFolders(t *testing.T) {
	_ = setupTestDataDir(t)
	nb := t.TempDir()
	subVisible := filepath.Join(nb, "guides")
	subIgnored := filepath.Join(nb, "archive")
	if err := os.MkdirAll(subVisible, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subIgnored, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(nb, "Golang.md"), []byte("Title match only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subVisible, "notes.md"), []byte("Line one\nUsing golang in practice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subIgnored, "secret.md"), []byte("golang ignored folder\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	theme.SetIgnoredSearchFolders([]string{subIgnored})
	t.Cleanup(func() { theme.SetIgnoredSearchFolders(nil) })

	hits, err := SearchNotebooks([]string{nb}, "golang", ScanOptions{
		HideAssets:   true,
		AssetsFolder: "assets",
	})
	if err != nil {
		t.Fatalf("SearchNotebooks: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits (ignored folder excluded), got %d: %+v", len(hits), hits)
	}
	if hits[0].Title != "Golang" {
		t.Fatalf("expected title match first, got %+v", hits[0])
	}
	if hits[1].Title != "notes" || hits[1].Line != 2 {
		t.Fatalf("expected body hit on line 2, got %+v", hits[1])
	}
	if hits[1].MatchEnd <= hits[1].MatchStart {
		t.Fatalf("expected valid match offsets in snippet %q: [%d:%d]", hits[1].Snippet, hits[1].MatchStart, hits[1].MatchEnd)
	}
}

func TestScanAndSearchShowHiddenDirs(t *testing.T) {
	_ = setupTestDataDir(t)
	root := t.TempDir()

	dirPath := filepath.Join(root, ".visible-dot")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "note.md"), []byte("findme"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := ScanOptions{
		HideNonMarkdown: true,
		HideAssets:      true,
		ShowHidden:      true,
		AssetsFolder:    "assets",
	}

	entries, err := ScanNotebook(root, opts)
	if err != nil {
		t.Fatalf("ScanNotebook: %v", err)
	}
	if len(entries) != 1 || entries[0].RawName != ".visible-dot" {
		t.Fatalf("ScanNotebook(ShowHidden=true) entries = %+v, want only .visible-dot", entries)
	}

	hits, err := SearchNotebooks([]string{root}, "findme", opts)
	if err != nil {
		t.Fatalf("SearchNotebooks: %v", err)
	}
	if len(hits) != 1 || hits[0].Path != filepath.Join(root, ".visible-dot", "note.md") {
		t.Fatalf("SearchNotebooks(ShowHidden=true) hits = %+v, want only .visible-dot/note.md", hits)
	}
}

func TestHistorySnapshotsAndMyersDiff(t *testing.T) {
	_ = setupTestDataDir(t)
	nb := t.TempDir()
	notePath := filepath.Join(nb, "doc.md")

	s1, err := RecordHistorySnapshot(notePath, "line 1\nline 2\n", "initial")
	if err != nil {
		t.Fatalf("RecordHistorySnapshot #1: %v", err)
	}
	// Duplicate content hash should deduplicate.
	s1Dup, err := RecordHistorySnapshot(notePath, "line 1\nline 2\n", "dup")
	if err != nil {
		t.Fatalf("RecordHistorySnapshot dup: %v", err)
	}
	if s1Dup.ID != s1.ID {
		t.Fatalf("expected hash dedup to return %s, got %s", s1.ID, s1Dup.ID)
	}

	s2, err := RecordHistorySnapshot(notePath, "line 1\nline 2 edited\nline 3\n", "edit")
	if err != nil {
		t.Fatalf("RecordHistorySnapshot #2: %v", err)
	}

	list, err := ListHistorySnapshots(notePath)
	if err != nil {
		t.Fatalf("ListHistorySnapshots: %v", err)
	}
	if len(list) != 2 || list[0].ID != s2.ID || list[1].ID != s1.ID {
		t.Fatalf("unexpected snapshot list: %+v", list)
	}

	body1, err := ReadHistorySnapshot(notePath, s1.ID)
	if err != nil || body1 != "line 1\nline 2\n" {
		t.Fatalf("ReadHistorySnapshot #1 = (%q, %v)", body1, err)
	}
	body2, err := ReadHistorySnapshot(notePath, s2.ID)
	if err != nil || body2 != "line 1\nline 2 edited\nline 3\n" {
		t.Fatalf("ReadHistorySnapshot #2 = (%q, %v)", body2, err)
	}

	diff := DiffLines("a\nb\nc\n", "a\nx\nc\nd\n")
	want := []model.DiffLine{
		{Kind: model.DiffEqual, OldLine: 1, NewLine: 1, Text: "a"},
		{Kind: model.DiffDelete, OldLine: 2, NewLine: 0, Text: "b"},
		{Kind: model.DiffInsert, OldLine: 0, NewLine: 2, Text: "x"},
		{Kind: model.DiffEqual, OldLine: 3, NewLine: 3, Text: "c"},
		{Kind: model.DiffInsert, OldLine: 0, NewLine: 4, Text: "d"},
	}
	if len(diff) != len(want) {
		t.Fatalf("DiffLines len = %d, want %d: %+v", len(diff), len(want), diff)
	}
	for i := range want {
		if diff[i] != want[i] {
			t.Fatalf("DiffLines[%d] = %+v, want %+v", i, diff[i], want[i])
		}
	}
}

func TestClassifyHelpers(t *testing.T) {
	for _, img := range []string{"a.png", "b.JPG", "c.jpeg", "d.gif", "e.webp", "f.bmp", "g.ico", "h.avif", "i.SVG"} {
		if !IsImagePath(img) {
			t.Fatalf("IsImagePath(%q) = false, want true", img)
		}
	}
	if IsImagePath("notes.md") || IsImagePath("readme") {
		t.Fatal("expected non-image paths to return false for IsImagePath")
	}
	if !IsSVGPath("icon.SVG") || IsSVGPath("icon.png") {
		t.Fatal("IsSVGPath mismatch")
	}

	langCases := map[string]string{
		"index.html": "html",
		"page.xhtml": "html",
		"data.json":  "json",
		"cfg.yaml":   "yaml",
		"cfg.yml":    "yaml",
		"cfg.toml":   "plaintext",
		"app.mjs":    "javascript",
		"app.tsx":    "typescript",
		"app.mts":    "typescript",
		"main.go":    "go",
		"lib.rs":     "rust",
		"run.sh":     "bash",
		"init.fish":  "bash",
		"icon.svg":   "xml",
		"style.xsl":  "xml",
		"README":     "plaintext",
		"app.log":    "plaintext",
		".env":       "plaintext",
	}
	for p, want := range langCases {
		if got := LanguageTagForPath(p); got != want {
			t.Fatalf("LanguageTagForPath(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestClassifyFileImagesAndSVG(t *testing.T) {
	dir := t.TempDir()

	// Raster image > 5MB with binary bytes still returns Preview without reading body.
	bigPNG := filepath.Join(dir, "huge.png")
	f, err := os.Create(bigPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(TextLimit + 1024); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	for _, ext := range []string{"png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "avif"} {
		p := bigPNG
		if ext != "png" {
			p = filepath.Join(dir, "sample."+ext)
			if err := os.WriteFile(p, []byte{0x00, 0xFF, 0x01}, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := ClassifyFile(p)
		if err != nil {
			t.Fatalf("ClassifyFile(%s): %v", p, err)
		}
		if got.Kind != model.OpenKindPreview || got.Source != nil {
			t.Fatalf("ClassifyFile(%s) = %+v, want Preview with nil Source", p, got)
		}
	}

	// Editable SVG with BOM.
	svgBody := "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>\n"
	svgPath := filepath.Join(dir, "editable.svg")
	if _, err := WriteRawFileAtomic(svgPath, svgBody, true); err != nil {
		t.Fatal(err)
	}
	gotSVG, err := ClassifyFile(svgPath)
	if err != nil {
		t.Fatalf("ClassifyFile(svg): %v", err)
	}
	if gotSVG.Kind != model.OpenKindPreview || gotSVG.Source == nil {
		t.Fatalf("expected editable SVG Preview, got %+v", gotSVG)
	}
	if !gotSVG.Source.SVG || !gotSVG.Source.BOM || gotSVG.Source.Language != "xml" || gotSVG.Source.Text != svgBody {
		t.Fatalf("unexpected SVG source: %+v", gotSVG.Source)
	}

	// Read-only SVG fallbacks: binary byte, long line, >5MB.
	badSVG := filepath.Join(dir, "bin.svg")
	if err := os.WriteFile(badSVG, []byte("<svg>\x00</svg>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(badSVG); err != nil || got.Kind != model.OpenKindPreview || got.Source != nil {
		t.Fatalf("expected read-only SVG for binary byte, got (%+v, %v)", got, err)
	}

	longLineSVG := filepath.Join(dir, "long.svg")
	if err := os.WriteFile(longLineSVG, bytes.Repeat([]byte("a"), lineLimit+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(longLineSVG); err != nil || got.Kind != model.OpenKindPreview || got.Source != nil {
		t.Fatalf("expected read-only SVG for long line, got (%+v, %v)", got, err)
	}

	hugeSVG := filepath.Join(dir, "huge.svg")
	if err := os.WriteFile(hugeSVG, bytes.Repeat([]byte(" "), int(TextLimit)+10), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(hugeSVG); err != nil || got.Kind != model.OpenKindPreview || got.Source != nil {
		t.Fatalf("expected read-only SVG for >5MB, got (%+v, %v)", got, err)
	}
}

func TestClassifyFileMarkdownSourceBOMAndBlocks(t *testing.T) {
	theme.SetLanguage(model.LangEn)
	t.Cleanup(func() { theme.SetLanguage(model.LangZh) })

	dir := t.TempDir()

	// Markdown valid.
	mdPath := filepath.Join(dir, "note.md")
	if err := os.WriteFile(mdPath, []byte("# Title\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotMD, err := ClassifyFile(mdPath)
	if err != nil || gotMD.Kind != model.OpenKindMarkdown || gotMD.Text != "# Title\nBody\n" {
		t.Fatalf("valid markdown got (%+v, %v)", gotMD, err)
	}

	// Markdown long line blocked.
	mdLong := filepath.Join(dir, "long.md")
	if err := os.WriteFile(mdLong, bytes.Repeat([]byte("x"), lineLimit+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(mdLong); err != nil || got.Kind != model.OpenKindBlocked || got.Reason != "A line is too long to preview" {
		t.Fatalf("long markdown got (%+v, %v)", got, err)
	}

	// Markdown invalid UTF-8 blocked.
	mdBadUTF8 := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(mdBadUTF8, []byte{0xff, 0xfe, 0xfd}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(mdBadUTF8); err != nil || got.Kind != model.OpenKindBlocked || got.Reason != "Not valid UTF-8" {
		t.Fatalf("invalid UTF-8 markdown got (%+v, %v)", got, err)
	}

	// Source UTF-8 BOM roundtrip + HTML flag + highlight boundary.
	htmlPath := filepath.Join(dir, "page.xhtml")
	htmlText := "<html><body>Hello</body></html>\n"
	if _, err := WriteRawFileAtomic(htmlPath, htmlText, true); err != nil {
		t.Fatal(err)
	}
	gotHTML, err := ClassifyFile(htmlPath)
	if err != nil || gotHTML.Kind != model.OpenKindSource || gotHTML.Source == nil {
		t.Fatalf("ClassifyFile(xhtml) = (%+v, %v)", gotHTML, err)
	}
	if !gotHTML.Source.BOM || !gotHTML.Source.HTML || !gotHTML.Source.Highlight || gotHTML.Source.Language != "html" || gotHTML.Source.Text != htmlText {
		t.Fatalf("unexpected xhtml Source: %+v", gotHTML.Source)
	}
	if _, err := WriteRawFileAtomic(htmlPath, gotHTML.Source.Text+"<!-- edited -->\n", gotHTML.Source.BOM); err != nil {
		t.Fatal(err)
	}
	rawDisk, err := os.ReadFile(htmlPath)
	if err != nil || !bytes.HasPrefix(rawDisk, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("expected UTF-8 BOM preserved on writeback, got %v (err=%v)", rawDisk[:min(3, len(rawDisk))], err)
	}

	// Extensionless plain text & >128KB highlight off.
	plainPath := filepath.Join(dir, "README")
	largeText := strings.Repeat("line\n", (HighlightLimit/5)+100)
	if _, err := WriteRawFileAtomic(plainPath, largeText, false); err != nil {
		t.Fatal(err)
	}
	gotPlain, err := ClassifyFile(plainPath)
	if err != nil || gotPlain.Kind != model.OpenKindSource || gotPlain.Source == nil {
		t.Fatalf("ClassifyFile(README) = (%+v, %v)", gotPlain, err)
	}
	if gotPlain.Source.BOM || gotPlain.Source.Highlight || gotPlain.Source.Language != "plaintext" {
		t.Fatalf("unexpected large plain Source: %+v", gotPlain.Source)
	}

	// Binary known text blocked.
	binJSON := filepath.Join(dir, "data.json")
	if err := os.WriteFile(binJSON, []byte("{\"a\":\x001}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(binJSON); err != nil || got.Kind != model.OpenKindBlocked || got.Reason != "File contains binary data" {
		t.Fatalf("binary json got (%+v, %v)", got, err)
	}

	// >5MB text blocked.
	bigLog := filepath.Join(dir, "big.log")
	if err := os.WriteFile(bigLog, bytes.Repeat([]byte("a\n"), int(TextLimit/2)+10), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(bigLog); err != nil || got.Kind != model.OpenKindBlocked || got.Reason != "File is larger than 5 MB" {
		t.Fatalf("big log got (%+v, %v)", got, err)
	}

	// Unsupported binary file blocked.
	binUnknown := filepath.Join(dir, "archive.bin")
	if err := os.WriteFile(binUnknown, []byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyFile(binUnknown); err != nil || got.Kind != model.OpenKindBlocked || got.Reason != "This file type is not supported" {
		t.Fatalf("unknown binary got (%+v, %v)", got, err)
	}
}

func TestLatestRecoveryDraft(t *testing.T) {
	dataDir := setupTestDataDir(t)
	if _, _, ok := LatestRecoveryDraft(); ok {
		t.Fatal("expected ok=false when Recovery directory does not exist")
	}

	nb := t.TempDir()
	p1 := filepath.Join(nb, "first.md")
	p2 := filepath.Join(nb, "second.md")

	if err := SaveRecovery(p1, "older draft"); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecovery(p2, "newer draft"); err != nil {
		t.Fatal(err)
	}

	// Ensure distinct mtimes even on coarse filesystems, plus ignore corrupt JSON.
	rec1, _ := recoveryDraftPath(p1)
	rec2, _ := recoveryDraftPath(p2)
	now := time.Now()
	_ = os.Chtimes(rec1, now.Add(-10*time.Second), now.Add(-10*time.Second))
	_ = os.Chtimes(rec2, now.Add(-2*time.Second), now.Add(-2*time.Second))
	corrupt := filepath.Join(dataDir, "Recovery", "zzzz-corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(corrupt, now, now)

	gotPath, gotContent, ok := LatestRecoveryDraft()
	if !ok || gotPath != p2 || gotContent != "newer draft" {
		t.Fatalf("LatestRecoveryDraft() = (%q, %q, %v), want (%q, %q, true)", gotPath, gotContent, ok, p2, "newer draft")
	}

	ClearRecovery(p2)
	gotPath, gotContent, ok = LatestRecoveryDraft()
	if !ok || gotPath != p1 || gotContent != "older draft" {
		t.Fatalf("LatestRecoveryDraft() after ClearRecovery = (%q, %q, %v), want (%q, %q, true)", gotPath, gotContent, ok, p1, "older draft")
	}
}

func TestAnnotationStoreNeverClobbersOtherNotes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "annotations.json")
	corrupt := `{"notes":{"/a.md":[{"id":1,"block_index":0,"start":0,"end":1,"quote":"x","body":"","reactions":[]}],}}`
	if err := os.WriteFile(p, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveNoteAnnotations(dir, "/c.md", []StoredAnnotation{{ID: 2, Quote: "y", Reactions: []string{}}}); err == nil {
		t.Fatalf("expected save to refuse an unreadable store")
	}
	if b, _ := os.ReadFile(p); string(b) != corrupt {
		t.Fatalf("unreadable store was rewritten: %s", b)
	}
}

func TestMoveNoteAnnotationsFollowsFolder(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "nb")
	for _, n := range []string{filepath.Join(root, "f", "a.md"), filepath.Join(root, "f", "sub", "b.md"), filepath.Join(root, "other.md")} {
		if err := SaveNoteAnnotations(dir, n, []StoredAnnotation{{ID: 1, Quote: "q", Reactions: []string{}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := MoveNoteAnnotations(dir, filepath.Join(root, "f"), filepath.Join(root, "g")); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]bool{
		filepath.Join(root, "g", "a.md"):        true,
		filepath.Join(root, "g", "sub", "b.md"): true,
		filepath.Join(root, "f", "a.md"):        false,
		filepath.Join(root, "other.md"):         true,
	} {
		got, err := LoadNoteAnnotations(dir, n)
		if err != nil {
			t.Fatal(err)
		}
		if (len(got) > 0) != want {
			t.Fatalf("%s: have=%v want=%v", n, len(got) > 0, want)
		}
	}
}
