package app

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"block/internal/files"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

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

	reordered, moved := ops.ReorderTab(tabs, "/1.md", "/2.md", true)
	if !moved || len(reordered) != 3 || reordered[0] != "/new.md" || reordered[1] != "/2.md" || reordered[2] != "/1.md" {
		t.Fatalf("unexpected ReorderTab result: %v moved=%v", reordered, moved)
	}
	movedTabs, ok := ops.MoveToIndex(reordered, "/1.md", 0)
	if !ok || movedTabs[0] != "/1.md" || movedTabs[1] != "/new.md" {
		t.Fatalf("unexpected MoveToIndex result: %v ok=%v", movedTabs, ok)
	}
}

func TestWorkspaceTabsReorderReopenPersistenceAndNotebookSwitch(t *testing.T) {
	dataDir := t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() { files.SetDataDirOverride("") })

	nb1 := filepath.Join(t.TempDir(), "nb1")
	nb2 := filepath.Join(t.TempDir(), "nb2")
	subDir := filepath.Join(nb1, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nb2, 0o755); err != nil {
		t.Fatal(err)
	}

	pA := filepath.Join(nb1, "A.md")
	pB := filepath.Join(nb1, "B.md")
	pC := filepath.Join(subDir, "C.md")
	for _, p := range []string{pA, pB, pC} {
		if err := os.WriteFile(p, []byte("# "+filepath.Base(p)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(nb1); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenNote(pA, 0); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenNote(pB, 0); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenNote(pC, 0); err != nil {
		t.Fatal(err)
	}

	// ReorderTab: move C before A -> [C, A, B]
	if !ws.ReorderTab(pC, pA, false) {
		t.Fatal("expected ReorderTab to succeed")
	}
	tabs := ws.Tabs()
	if len(tabs) != 3 || tabs[0] != pC || tabs[1] != pA || tabs[2] != pB {
		t.Fatalf("unexpected tab order after reorder: %v", tabs)
	}

	// Reload workspace from persisted state.json -> order & active tab survive!
	wsReloaded := NewWorkspace(dataDir)
	tabsReloaded := wsReloaded.Tabs()
	if len(tabsReloaded) != 3 || tabsReloaded[0] != pC || tabsReloaded[1] != pA || tabsReloaded[2] != pB {
		t.Fatalf("persisted tab order mismatch: %v", tabsReloaded)
	}
	if wsReloaded.ActiveNotePath() != pC {
		t.Fatalf("expected active tab %s after reload, got %s", pC, wsReloaded.ActiveNotePath())
	}

	// Close A at index 1, then ReopenClosedTab -> A returns to index 1!
	wsReloaded.CloseTab(pA)
	if got := wsReloaded.Tabs(); len(got) != 2 || got[0] != pC || got[1] != pB {
		t.Fatalf("unexpected tabs after CloseTab(A): %v", got)
	}
	if !wsReloaded.ReopenClosedTab() {
		t.Fatal("expected ReopenClosedTab to reopen A")
	}
	if got := wsReloaded.Tabs(); len(got) != 3 || got[0] != pC || got[1] != pA || got[2] != pB {
		t.Fatalf("expected A reopened at index 1, got %v", got)
	}

	// Rename B -> B2, then close B2, then rename sub -> sub2: nav & closed tabs rebase too!
	pB2, err := wsReloaded.RenameWorkspaceItem(pB, "B2")
	if err != nil {
		t.Fatal(err)
	}
	wsReloaded.CloseTab(pB2)
	if _, err := wsReloaded.RenameWorkspaceItem(subDir, "sub2"); err != nil {
		t.Fatal(err)
	}
	pC2 := filepath.Join(nb1, "sub2", "C.md")
	if wsReloaded.ActiveNotePath() != pC2 && wsReloaded.Tabs()[0] != pC2 {
		t.Fatalf("expected C rebased to %s, got tabs=%v active=%s", pC2, wsReloaded.Tabs(), wsReloaded.ActiveNotePath())
	}

	// Select the rebased note before creating its sibling.
	if err := wsReloaded.OpenNote(pC2, 0); err != nil {
		t.Fatal(err)
	}
	createdSub, err := wsReloaded.CreateNewNote()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(createdSub) != filepath.Join(nb1, "sub2") {
		t.Fatalf("expected CreateNewNote in active subfolder sub2, got %s", createdSub)
	}

	// CloseOtherTabs keeps C2 and records others in left-to-right reopen order.
	wsReloaded.CloseOtherTabs(pC2)
	if len(wsReloaded.Tabs()) != 1 {
		t.Fatalf("expected 1 tab after CloseOtherTabs, got %v", wsReloaded.Tabs())
	}
	if !wsReloaded.ReopenClosedTab() {
		t.Fatal("expected ReopenClosedTab after CloseOtherTabs")
	}

	// Switching to a different notebook clears previous notebook's open tabs!
	if err := wsReloaded.SetNotebook(nb2); err != nil {
		t.Fatal(err)
	}
	if len(wsReloaded.Tabs()) != 0 || wsReloaded.ActiveNotePath() != "" {
		t.Fatalf("expected tabs cleared after switching notebook, got tabs=%v active=%q", wsReloaded.Tabs(), wsReloaded.ActiveNotePath())
	}
	persisted := files.LoadState()
	if len(persisted.OpenTabs) != 0 || persisted.ActiveTab != "" {
		t.Fatalf("expected persisted tabs cleared on notebook switch, got %+v", persisted)
	}
}

func TestSingleTabTitleStyleHasNoBackground(t *testing.T) {
	theme.SetDark(false)
	pal := theme.Ocean()
	menu := &TabMenuState{}
	tabs := []string{"/notes/claude.md"}
	active := "/notes/claude.md"

	th := pal.ToUITheme("")
	blockui.ClearTargets()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(&th)
		ui.Box(c).Fill().Background(pal.ShellChrome().Ground).Children(func() {
			RenderTitlebar(c, TitlebarProps{
				Tabs:         tabs,
				ActiveTab:    active,
				SidebarWide:  true,
				SidebarWidth: 240,
				MenuState:    menu,
			})
		})
	}, 960, 80)
	tt.Frame()

	info, ok := tt.AccessByRole(ui.RoleTab, "claude")
	if !ok || !info.Selected {
		t.Fatalf("expected single tab accessibility node selected, got ok=%v info=%+v", ok, info)
	}

	r, ok := blockui.TargetBounds("tab-0")
	if !ok {
		t.Fatal("expected tab-0 bounds recorded for single tab")
	}
	img := tt.Image()
	// Sample inside the left padding of tab-0 (before label glyphs): should match pal.Shell (#f1f1f1), with no pill background.
	gotSingle := img.RGBAAt(int(r.X+4), int(r.Y+r.H/2))
	wantShell := color.RGBA{R: 0xf1, G: 0xf1, B: 0xf1, A: 0xff}
	if gotSingle != wantShell {
		t.Fatalf("expected single tab background to match titlebar shell %+v, got %+v", wantShell, gotSingle)
	}

	// Once a second tab opens, the active tab renders with its selected pill surface (#e5edfc).
	tabs = []string{"/notes/claude.md", "/notes/other.md"}
	tt.Frame()

	rMulti, ok := blockui.TargetBounds("tab-0")
	if !ok {
		t.Fatal("expected tab-0 bounds recorded for multi-tab")
	}
	imgMulti := tt.Image()
	gotMulti := imgMulti.RGBAAt(int(rMulti.X+4), int(rMulti.Y+rMulti.H/2))
	wantSelected := color.RGBA{R: 0xe5, G: 0xed, B: 0xfc, A: 0xff}
	if gotMulti != wantSelected {
		t.Fatalf("expected multi-tab active pill background %+v, got %+v", wantSelected, gotMulti)
	}
}

func TestEdgeHotZoneOnlyWhenYieldedAndHovered(t *testing.T) {
	dataDir := t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() { files.SetDataDirOverride("") })

	nb := filepath.Join(t.TempDir(), "nb")
	if err := os.MkdirAll(nb, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(nb, "Note.md")
	if err := os.WriteFile(notePath, []byte("# Title\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(nb); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenNote(notePath, 0); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(ws.Render, 1200, 760)
	tt.Frame()

	// 1. Wide window, sidebar docked, AI closed -> neither edge hot-zone exists.
	if _, ok := blockui.TargetBounds("edge-hotzone-left"); ok {
		t.Fatal("unexpected left edge hot-zone when sidebar is docked")
	}
	if _, ok := blockui.TargetBounds("edge-hotzone-right"); ok {
		t.Fatal("unexpected right edge hot-zone when AI panel is closed")
	}

	// 2. User collapses sidebar explicitly -> still no left edge hot-zone!
	ws.ToggleSidebar(1200)
	tt.Frame()
	if !ws.State().SidebarCollapsed {
		t.Fatal("expected sidebar collapsed")
	}
	if _, ok := blockui.TargetBounds("edge-hotzone-left"); ok {
		t.Fatal("unexpected left edge hot-zone when user explicitly collapsed sidebar")
	}

	// Narrow window while sidebar is still closed by user -> still no left or right hot-zone!
	tt.SetSize(700, 760)
	tt.Frame()
	if _, ok := blockui.TargetBounds("edge-hotzone-left"); ok {
		t.Fatal("unexpected left edge hot-zone on narrow window when sidebar was not opened by user")
	}
	if _, ok := blockui.TargetBounds("edge-hotzone-right"); ok {
		t.Fatal("unexpected right edge hot-zone on narrow window when AI panel was not opened by user")
	}

	// 3. Re-expand sidebar at wide size, then shrink window so sidebar yields due to space:
	// hot-zone exists, strip width is 0 at rest, and reveals only on hover.
	tt.SetSize(1200, 760)
	tt.Frame()
	ws.ToggleSidebar(1200)
	tt.Frame()
	if ws.State().SidebarCollapsed {
		t.Fatal("expected sidebar open")
	}

	tt.Move(400, 400)
	tt.SetSize(680, 760)
	ws.AI().SetOpen(true) // forces sidebar (240+560+320 > 680) to yield while sidebar is open
	tt.Frame()

	if _, ok := blockui.TargetBounds("edge-hotzone-left"); !ok {
		t.Fatal("expected left edge hot-zone when open sidebar yielded due to narrow space")
	}
	if stripIdle, ok := blockui.TargetBounds("edge-strip-left"); ok && stripIdle.W > 0 {
		t.Fatalf("expected left edge strip hidden (width 0) at rest, got %+v", stripIdle)
	}

	// Hovering over the left edge immediately reveals both the indicator strip and the floating sidebar card.
	tt.Move(2, 300)
	tt.Frame()
	stripHover, ok := blockui.TargetBounds("edge-strip-left")
	if !ok || stripHover.W <= 0 {
		t.Fatalf("expected left edge strip visible when hovered, got ok=%v rect=%+v", ok, stripHover)
	}
	if _, ok := blockui.TargetBounds("edge-float-sidebar"); !ok {
		t.Fatal("expected floating sidebar card to open immediately on hover without dwell delay")
	}
}

func TestBrandSceneStaysBehindCardAndTransparentSearch(t *testing.T) {
	dataDir := t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() {
		files.SetDataDirOverride("")
		theme.SetDark(false)
	})

	nb := filepath.Join(t.TempDir(), "nb")
	if err := os.MkdirAll(nb, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(nb, "Note.md")
	if err := os.WriteFile(notePath, []byte("# Title\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(nb); err != nil {
		t.Fatal(err)
	}
	if err := ws.OpenNote(notePath, 0); err != nil {
		t.Fatal(err)
	}
	ws.state.Theme = "dark"
	ws.state.BrandDeco.Mode = "pin"
	ws.state.BrandDeco.PinDark = "moon"
	theme.SetDark(true)
	blockui.ClearTargets()

	tt := ui.NewTester(ws.Render, 1200, 760)
	tt.Frame()

	cardRect, ok := blockui.TargetBounds("workspace-panel-card")
	if !ok {
		t.Fatal("expected workspace-panel-card bounds")
	}
	img := tt.Image()
	// Sample inside the top 48px of the floating paper card (y = 60, inside the 88px brand scene band):
	// must be exact dark card surface #181818, never covered by the #222222 / hill wave band.
	gotCardTop := img.RGBAAt(int(cardRect.X+120), int(cardRect.Y+20))
	wantCard := color.RGBA{R: 0x18, G: 0x18, B: 0x18, A: 0xff}
	if gotCardTop != wantCard {
		t.Fatalf("expected top of workspace-panel-card to cover brand scene with %+v, got %+v", wantCard, gotCardTop)
	}

	searchRect, ok := blockui.TargetBounds("search-button")
	if !ok {
		t.Fatal("expected search-button bounds")
	}
	// Sample the right side of search-button vs the gap just outside it on the same scanline:
	// a transparent T3-style search button has no #2a2a2a FieldBG box or #303030 kbd box.
	sampleY := int(searchRect.Y + searchRect.H/2)
	gotSearchRight := img.RGBAAt(int(searchRect.X+searchRect.W-6), sampleY)
	unwantedFieldBG := color.RGBA{R: 0x2a, G: 0x2a, B: 0x2a, A: 0xff}
	if gotSearchRight == unwantedFieldBG {
		t.Fatalf("expected transparent search-button without FieldBG %+v, got %+v", unwantedFieldBG, gotSearchRight)
	}
}

func TestCreateNewFolderStaysAtFolderBottomAndAboveFiles(t *testing.T) {
	dataDir := t.TempDir()
	files.SetDataDirOverride(dataDir)
	t.Cleanup(func() { files.SetDataDirOverride("") })

	nb := filepath.Join(t.TempDir(), "nb")
	for _, dir := range []string{"Beta", "Zebra", "阅读"} {
		if err := os.MkdirAll(filepath.Join(nb, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p1 := filepath.Join(nb, "note1.md")
	p2 := filepath.Join(nb, "note2.md")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("# "+filepath.Base(p)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ws := NewWorkspace(dataDir)
	if err := ws.SetNotebook(nb); err != nil {
		t.Fatal(err)
	}

	// Reorder notes first so FileOrder[nb] exists before creating any folder.
	ws.ReorderSibling(nb, p2, p1, false)

	created1, err := ws.CreateNewFolder()
	if err != nil {
		t.Fatal(err)
	}

	assertOrder := func(step string, want []string) {
		t.Helper()
		entries := ws.Entries()
		if len(entries) != len(want) {
			t.Fatalf("%s: entries len=%d, want %d (%v)", step, len(entries), len(want), entries)
		}
		for i, w := range want {
			if entries[i].RawName != w {
				t.Fatalf("%s: entries[%d]=%q, want %q", step, i, entries[i].RawName, w)
			}
		}
	}

	assertOrder("after CreateNewFolder #1", []string{
		"Beta", "Zebra", "阅读", filepath.Base(created1), "note2.md", "note1.md",
	})

	// Renaming the newly created folder in manual mode keeps it at the bottom of folders (above files).
	if _, err := ws.RenameWorkspaceItem(created1, "Alpha"); err != nil {
		t.Fatal(err)
	}
	assertOrder("after rename to Alpha in manual mode", []string{
		"Beta", "Zebra", "阅读", "Alpha", "note2.md", "note1.md",
	})

	// Creating a second new folder appends it after Alpha and still above all files.
	created2, err := ws.CreateNewFolder()
	if err != nil {
		t.Fatal(err)
	}
	assertOrder("after CreateNewFolder #2", []string{
		"Beta", "Zebra", "阅读", "Alpha", filepath.Base(created2), "note2.md", "note1.md",
	})
}
