package app

import "testing"

func TestResolveShellLayout(t *testing.T) {
	// 1. Wide window: sidebar docks + chat docks + outline shows.
	wide := ResolveShellLayout(ShellInput{
		WindowWidth:     1400,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if !wide.SidebarDocked || wide.SidebarYielded || !wide.ChatDocked || wide.ChatOverlay {
		t.Fatalf("expected docked sidebar+chat on 1400px, got %+v", wide)
	}
	if wide.ChatWidth != 396 || wide.MainWidth < ThreeColumnNoteFloor || !wide.ShowOutline {
		t.Fatalf("expected chat=396, main >= %v and ShowOutline=true, got %+v", ThreeColumnNoteFloor, wide)
	}

	// 2. Three-column window shrinks chat above ThreeColumnNoteFloor (560).
	threeCol := ResolveShellLayout(ShellInput{
		WindowWidth:     1150,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if !threeCol.SidebarDocked || threeCol.SidebarYielded || !threeCol.ChatDocked || threeCol.ChatOverlay {
		t.Fatalf("expected 3-col docked at 1150px, got %+v", threeCol)
	}
	if threeCol.ChatWidth != 350 || threeCol.MainWidth != ThreeColumnNoteFloor {
		t.Fatalf("expected chat=350 and main=%v at 1150px, got %+v", ThreeColumnNoteFloor, threeCol)
	}

	// 3. Left sidebar hides first (240 + 560 + 320 = 1120 > 1018) and right chat stays docked!
	leftYields := ResolveShellLayout(ShellInput{
		WindowWidth:     1018,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    457,
	})
	if !leftYields.SidebarYielded || leftYields.SidebarDrawer || !leftYields.ChatDocked || leftYields.ChatOverlay {
		t.Fatalf("expected sidebar yielded and chat docked at 1018px, got %+v", leftYields)
	}
	if leftYields.ChatWidth != 457 || leftYields.MainWidth < NoteFloor {
		t.Fatalf("expected chat=457 and main >= %v, got %+v", NoteFloor, leftYields)
	}

	// 4. Narrow two-column window shrinks docked chat without floating; pinned sidebar opens drawer.
	narrow := ResolveShellLayout(ShellInput{
		WindowWidth:     840,
		SidebarVisible:  true,
		SidebarPinned:   true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if narrow.SidebarDocked || !narrow.SidebarYielded || !narrow.SidebarDrawer {
		t.Fatalf("expected sidebar yielded+drawer at 840px, got %+v", narrow)
	}
	if !narrow.ChatDocked || narrow.ChatOverlay || narrow.ChatWidth != 360 || narrow.MainWidth != NoteFloor {
		t.Fatalf("expected docked chat=360 and main=%v at 840px, got %+v", NoteFloor, narrow)
	}

	// 5. Minimum window (680px) keeps chat docked at ChatMinW (320) and hides sidebar.
	minWin := ResolveShellLayout(ShellInput{
		WindowWidth:     SettingsFloor,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if !minWin.SidebarYielded || !minWin.ChatDocked || minWin.ChatOverlay || minWin.ChatWidth != ChatMinW {
		t.Fatalf("expected docked chat=%v at min window, got %+v", ChatMinW, minWin)
	}

	// 6. Settings blocks AI panel and enforces SettingsFloor=680.
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

	// 7. Collapsed sidebar stays hidden and chat never floats.
	collapsed := ResolveShellLayout(ShellInput{
		WindowWidth:     800,
		SidebarVisible:  false,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if collapsed.SidebarYielded || collapsed.SidebarDrawer || collapsed.ChatOverlay || collapsed.ChatWidth != ChatMinW {
		t.Fatalf("expected collapsed sidebar not yielded and chat=%v at 800px, got %+v", ChatMinW, collapsed)
	}

	// 8. Pinned file tree becomes drawer while chat stays docked.
	pinned := ResolveShellLayout(ShellInput{
		WindowWidth:     1000,
		SidebarVisible:  true,
		SidebarPinned:   true,
		SidebarDesiredW: 240,
		ChatVisible:     true,
		ChatDesiredW:    396,
	})
	if !pinned.SidebarYielded || !pinned.SidebarDrawer || pinned.ChatOverlay || pinned.ChatWidth != 396 || pinned.MainWidth < NoteFloor {
		t.Fatalf("expected drawer+docked chat=396 at 1000px, got %+v", pinned)
	}

	// 9. Closed chat hides a sidebar that cannot dock.
	closedWide := ResolveShellLayout(ShellInput{
		WindowWidth:     1100,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
		ChatVisible:     false,
		ChatDesiredW:    396,
	})
	if closedWide.SidebarYielded {
		t.Fatalf("expected sidebar docked at 1100px with closed chat, got %+v", closedWide)
	}
	closedNarrow := ResolveShellLayout(ShellInput{
		WindowWidth:     800,
		SidebarVisible:  true,
		SidebarDesiredW: 420,
		ChatVisible:     false,
		ChatDesiredW:    396,
	})
	if !closedNarrow.SidebarYielded || closedNarrow.SidebarDrawer || closedNarrow.ChatOverlay {
		t.Fatalf("expected sidebar yielded at 800px (420+480=900), got %+v", closedNarrow)
	}

	// 10. Settings nav hides until the floor fits, and opens as a drawer when pinned.
	sNarrow := ResolveShellLayout(ShellInput{
		WindowWidth:     900,
		InSettings:      true,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
	})
	if !sNarrow.SidebarYielded || sNarrow.SidebarDrawer || sNarrow.ChatOverlay {
		t.Fatalf("expected settings nav yielded at 900px, got %+v", sNarrow)
	}
	sWide := ResolveShellLayout(ShellInput{
		WindowWidth:     960,
		InSettings:      true,
		SidebarVisible:  true,
		SidebarDesiredW: 240,
	})
	if sWide.SidebarYielded || sWide.SidebarDrawer {
		t.Fatalf("expected settings nav docked at 960px, got %+v", sWide)
	}
	sPinned := ResolveShellLayout(ShellInput{
		WindowWidth:     800,
		InSettings:      true,
		SidebarVisible:  true,
		SidebarPinned:   true,
		SidebarDesiredW: 240,
	})
	if !sPinned.SidebarYielded || !sPinned.SidebarDrawer || sPinned.ChatOverlay {
		t.Fatalf("expected settings nav drawer when pinned at 800px, got %+v", sPinned)
	}
	if ClampSidebarWidth(0) != 240 {
		t.Fatalf("expected default sidebar width 240, got %v", ClampSidebarWidth(0))
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
