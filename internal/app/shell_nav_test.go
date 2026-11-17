package app

import "testing"

func TestResolveShellLayout(t *testing.T) {
	// Wide window: sidebar docks + chat docks + outline shows.
	wide := ResolveShellLayout(ShellInput{
		WindowWidth:     1360,
		SidebarVisible:  true,
		SidebarDesiredW: 252,
		ChatVisible:     true,
		ChatDesiredW:    380,
	})
	if !wide.SidebarDocked || !wide.ChatDocked || wide.ChatOverlay {
		t.Fatalf("expected docked sidebar+chat on 1360px, got %+v", wide)
	}
	if wide.MainWidth < NoteFloor || !wide.ShowOutline {
		t.Fatalf("expected main width >= %v and ShowOutline=true, got %+v", NoteFloor, wide)
	}

	// Narrow window (700px): sidebar (252 + 480 = 732 > 700) yields; if pinned, opens drawer.
	narrow := ResolveShellLayout(ShellInput{
		WindowWidth:     700,
		SidebarVisible:  true,
		SidebarPinned:   true,
		SidebarDesiredW: 252,
		ChatVisible:     true,
		ChatDesiredW:    380,
	})
	if narrow.SidebarDocked || !narrow.SidebarYielded || !narrow.SidebarDrawer {
		t.Fatalf("expected sidebar yielded+drawer at 700px, got %+v", narrow)
	}
	if narrow.ChatDocked || !narrow.ChatOverlay {
		t.Fatalf("expected chat overlay at 700px, got %+v", narrow)
	}

	// Settings blocks AI panel and enforces SettingsFloor=680.
	settings := ResolveShellLayout(ShellInput{
		WindowWidth:     860,
		InSettings:      true,
		SidebarVisible:  true,
		SidebarDesiredW: 252,
		ChatVisible:     true,
		ChatDesiredW:    380,
	})
	if settings.SidebarDocked || !settings.SidebarYielded {
		t.Fatalf("expected sidebar yielded in settings at 860px (252+680=932), got %+v", settings)
	}
	if settings.ChatDocked || settings.ChatOverlay {
		t.Fatalf("expected AI panel suppressed in settings, got %+v", settings)
	}
}

func TestNavHistoryAndTabs(t *testing.T) {
	nav := NewNavHistory()
	nav.Push("/a.md")
	nav.Push("/b.md")
	nav.Push("/c.md")
	if !nav.CanBack() || nav.CanForward() {
		t.Fatalf("unexpected nav flags after pushes")
	}
	if got := nav.Back(); got != "/b.md" {
		t.Fatalf("expected /b.md on Back, got %q", got)
	}
	if !nav.CanForward() {
		t.Fatalf("expected CanForward after Back")
	}
	nav.ReplacePath("/c.md", "/c-renamed.md")
	if got := nav.Forward(); got != "/c-renamed.md" {
		t.Fatalf("expected /c-renamed.md on Forward, got %q", got)
	}

	closed := NewClosedTabs()
	closed.Push("/x.md")
	closed.Push("/y.md")
	closed.Push("/x.md") // dedup moves /x.md to top
	if got := closed.Pop(); got != "/x.md" {
		t.Fatalf("expected LIFO /x.md, got %q", got)
	}
	if got := closed.Pop(); got != "/y.md" {
		t.Fatalf("expected /y.md, got %q", got)
	}

	var ops TabStripOps
	tabs, active := ops.OpenOrFocus([]string{"/1.md", "/2.md"}, "/1.md", "/new.md")
	if len(tabs) != 3 || tabs[1] != "/new.md" || active != "/new.md" {
		t.Fatalf("unexpected OpenOrFocus result: %v active=%s", tabs, active)
	}
	if last := ops.SelectOneBased(tabs, 9); last != "/2.md" {
		t.Fatalf("expected Cmd+9 to select last tab /2.md, got %q", last)
	}
}
