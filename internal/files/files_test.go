package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// Custom FileOrder puts note10.md before FolderA.
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
	if entriesOrdered[0].RawName != "note10.md" || entriesOrdered[1].RawName != "FolderB" {
		t.Fatalf("custom FileOrder not applied: got %s, %s", entriesOrdered[0].RawName, entriesOrdered[1].RawName)
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
