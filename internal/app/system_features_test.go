package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"block/internal/files"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestBuildMenuBarCrossPlatformRolesAndStructure(t *testing.T) {
	macMenu := BuildMenuBarForOS("darwin", nil, MenuActions{})
	macItems := macMenu.Items()
	if len(macItems) != 5 || macItems[0].Label != "Block" {
		t.Fatalf("expected 5 macOS top-level menus starting with Block, got %d", len(macItems))
	}
	var hasPastePlain, hasDelete bool
	for _, it := range macItems[2].Submenu {
		if it.Role == mygo.RolePasteAndMatchStyle {
			hasPastePlain = true
		}
		if it.Role == mygo.RoleDelete {
			hasDelete = true
		}
	}
	if !hasPastePlain || !hasDelete {
		t.Fatalf("macOS Edit menu missing roles: pasteAndMatchStyle=%v delete=%v", hasPastePlain, hasDelete)
	}

	for _, goos := range []string{"windows", "linux"} {
		menu := BuildMenuBarForOS(goos, nil, MenuActions{})
		items := menu.Items()
		if len(items) != 5 || items[0].Label == "Block" {
			t.Fatalf("%s menu should not start with macOS Block menu, got first=%q len=%d", goos, items[0].Label, len(items))
		}
		for _, top := range items {
			for _, sub := range top.Submenu {
				if sub.Role == mygo.RoleServices || sub.Role == mygo.RoleHide ||
					sub.Role == mygo.RoleHideOthers || sub.Role == mygo.RoleUnhide ||
					sub.Role == mygo.RolePasteAndMatchStyle {
					t.Fatalf("%s menu should not contain macOS-only role %q", goos, sub.Role)
				}
			}
		}
		var hasQuit, hasHelpAbout, hasEditDelete bool
		for _, it := range items[0].Submenu {
			if it.Role == mygo.RoleQuit {
				hasQuit = true
			}
		}
		for _, it := range items[1].Submenu {
			if it.Role == mygo.RoleDelete {
				hasEditDelete = true
			}
		}
		for _, it := range items[4].Submenu {
			if it.ID == "menu-about" {
				hasHelpAbout = true
			}
		}
		if !hasQuit || !hasEditDelete || !hasHelpAbout {
			t.Fatalf("%s menu missing expected items: quit=%v editDelete=%v helpAbout=%v", goos, hasQuit, hasEditDelete, hasHelpAbout)
		}
	}
}

func TestLocalFileURLEncodingCrossPlatform(t *testing.T) {
	gotWin := LocalFileURL(`C:\Users\Alice\My Notes\笔记.md`)
	if !strings.HasPrefix(gotWin, "file:///C:/Users/Alice/My%20Notes/") || !strings.HasSuffix(gotWin, ".md") || strings.Contains(gotWin, " ") {
		t.Fatalf("unexpected Windows LocalFileURL: %q", gotWin)
	}
	gotUnix := LocalFileURL("/Users/alice/hello world/test.md")
	if gotUnix != "file:///Users/alice/hello%20world/test.md" {
		t.Fatalf("unexpected Unix LocalFileURL: %q", gotUnix)
	}
	gotUNC := LocalFileURL(`\\fileserver\shared\My Notes\doc.md`)
	if gotUNC != "file://fileserver/shared/My%20Notes/doc.md" {
		t.Fatalf("unexpected UNC LocalFileURL: %q", gotUNC)
	}
}

func TestWorkspaceCopyMarkdownShortcutAndSaveFlush(t *testing.T) {
	dataDir := t.TempDir()
	nbDir := t.TempDir()
	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(nbDir); err != nil {
		t.Fatalf("SetNotebook: %v", err)
	}
	notePath := filepath.Join(nbDir, "demo.md")
	if err := os.WriteFile(notePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write demo.md: %v", err)
	}
	if err := ws.OpenNote(notePath, 0); err != nil {
		t.Fatalf("OpenNote: %v", err)
	}
	sess := ws.ActiveSession()
	if sess == nil {
		t.Fatalf("expected active session")
	}
	sess.FocusBlock(0)
	sess.PasteText("# Hello\n\n**World**")
	sess.ClearFocus()

	tt := ui.NewTester(ws.Render, 1100, 700)

	// Alt+Cmd+C (Ctrl+Alt+C on Win/Linux) copies full note Markdown when nothing is selected.
	tt.SetClipboard("stale")
	tt.Key(ui.Cmd|ui.Alt, ui.KeyC)
	if got := tt.Clipboard(); !strings.Contains(got, "# Hello") || !strings.Contains(got, "**World**") {
		t.Fatalf("Alt+Cmd+C got clipboard %q", got)
	}

	// Cmd+S / Ctrl+S flushes dirty note immediately to disk.
	tt.Key(ui.Cmd, ui.KeyS)
	raw, err := os.ReadFile(filepath.Clean(notePath))
	if err != nil {
		t.Fatalf("read flushed note: %v", err)
	}
	if !strings.Contains(string(raw), "# Hello") || !strings.Contains(string(raw), "**World**") {
		t.Fatalf("expected Cmd+S to flush note content, disk has %q", string(raw))
	}
}

func TestWorkspaceRetainsDraftOnExternalSaveConflict(t *testing.T) {
	dataDir, notebook := t.TempDir(), t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() { files.SetDataDirOverride("") })
	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(notebook); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"note.md", "script.txt"} {
		path := filepath.Join(notebook, name)
		if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ws.OpenNote(path, 0); err != nil {
			t.Fatal(err)
		}
		if sess := ws.ActiveSession(); sess != nil {
			sess.FocusBlock(0)
			sess.PasteText("local draft")
		} else {
			ws.ActiveSurface().Source.Text = "local draft"
		}
		if err := os.WriteFile(path, []byte("external edit"), 0o600); err != nil {
			t.Fatal(err)
		}
		ws.CloseTab(path)
		if ws.ActiveNotePath() != path {
			t.Fatalf("%s closed with an unsaved draft", name)
		}
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != "external edit" {
			t.Fatalf("%s external edit was lost: %q, %v", name, raw, err)
		}
		if name == "note.md" {
			_, draft, _, ok := files.LatestVersionedRecoveryDraft()
			if !ok || !strings.Contains(draft, "local draft") {
				t.Fatalf("recoverable local draft missing: %q", draft)
			}
		}
	}
}

func TestDeepLinkRespectsChosenNotebook(t *testing.T) {
	dataDir, notebook, other := t.TempDir(), t.TempDir(), t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() { files.SetDataDirOverride("") })
	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(notebook); err != nil {
		t.Fatal(err)
	}
	chosen := filepath.Join(notebook, "chosen.md")
	outside := filepath.Join(other, "other.md")
	for _, path := range []string{chosen, outside} {
		if err := os.WriteFile(path, []byte("# Local fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.OpenDeepLinkedFile(chosen); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenDeepLinkedFile(outside); err == nil {
		t.Fatal("deep link opened a file outside chosen notebooks")
	}
	if got := ws.ActiveNotePath(); !sameFileOnDisk(got, chosen) {
		t.Fatalf("active note unexpectedly changed to %q", got)
	}
}
