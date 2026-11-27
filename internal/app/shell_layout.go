package app

// ShellLayout constants mirror thinkly-fast/src/ui/shell_layout.rs 1:1.
const (
	NoteFloor            float32 = 480
	ThreeColumnNoteFloor float32 = 560
	SettingsFloor        float32 = 680
	WindowMinW           float32 = 760
	WindowMinH           float32 = 560

	SidebarMinW     float32 = 240
	SidebarDefaultW float32 = 240
	SidebarMaxW     float32 = 420

	ChatMinW     float32 = 320
	ChatDefaultW float32 = 380
	ChatMaxW     float32 = 720

	TitlebarHeight float32 = 40
	OutlineShowMin float32 = 960
)

// ShellInput describes the current window geometry and user-requested panel state.
type ShellInput struct {
	WindowWidth     float32
	InSettings      bool
	SidebarVisible  bool
	SidebarPinned   bool // explicitly toggled open while narrow
	SidebarDesiredW float32
	ChatVisible     bool
	ChatFullscreen  bool
	ChatDesiredW    float32
	// TitlebarNeedW is the titlebar width (right of the sidebar) that keeps every tab
	// title whole with plugin buttons folded; 0 means no requirement.
	TitlebarNeedW float32
}

// ShellLayout describes the resolved responsive split layout.
type ShellLayout struct {
	ContentFloor   float32
	SidebarDocked  bool
	SidebarDrawer  bool
	SidebarYielded bool
	SidebarWidth   float32
	ChatDocked     bool
	ChatOverlay    bool
	ChatFullscreen bool
	ChatWidth      float32
	MainWidth      float32
	ShowOutline    bool
}

// ClampSidebarWidth clamps a user-dragged sidebar width to [240, 420].
func ClampSidebarWidth(w float32) float32 {
	if w <= 0 {
		return SidebarDefaultW
	}
	if w < SidebarMinW {
		return SidebarMinW
	}
	if w > SidebarMaxW {
		return SidebarMaxW
	}
	return w
}

// ClampChatWidth clamps a user-dragged AI panel width to [320, 720].
func ClampChatWidth(w float32) float32 {
	if w <= 0 {
		return ChatDefaultW
	}
	if w < ChatMinW {
		return ChatMinW
	}
	if w > ChatMaxW {
		return ChatMaxW
	}
	return w
}

// ChatLimit returns the widest chat width a drag may commit without pushing the note under its floor.
func ChatLimit(windowW, sidebarW float32, sidebarCollapsed bool, chatMin, chatMax float32) float32 {
	threeColFloor := max(NoteFloor, ThreeColumnNoteFloor)
	docks := !sidebarCollapsed && sidebarW+threeColFloor+chatMin <= windowW+0.5
	var room float32
	if docks {
		room = windowW - sidebarW - threeColFloor
	} else {
		room = windowW - NoteFloor
	}
	if room < chatMin {
		return chatMin
	}
	if room > chatMax {
		return chatMax
	}
	return room
}

// ResolveShellLayout resolves sidebar docking/yielding and AI panel docking
// using the exact priority rules from thinkly-fast/src/ui/shell_layout.rs:
//  1. When space runs short with AI Chat open, the left file tree steps aside first
//     (requiring ThreeColumnNoteFloor = 560 beside sidebar + chatMin = 320).
//  2. The right AI chat panel stays docked rather than floating over the page,
//     shrinking down toward ChatMinW (320) only after the file tree has yielded.
//  3. The yielded file tree remains reachable as a floating drawer when SidebarPinned.
//  4. Settings blocks AI panel and enforces SettingsFloor (680).
//  5. The file tree also steps aside before titlebar tabs get squeezed (TitlebarNeedW).
func ResolveShellLayout(in ShellInput) ShellLayout {
	winW := in.WindowWidth
	if winW <= 0 {
		winW = WindowMinW
	}
	floor := NoteFloor
	if in.InSettings {
		floor = SettingsFloor
	}
	sbW := ClampSidebarWidth(in.SidebarDesiredW)
	desiredChat := ClampChatWidth(in.ChatDesiredW)
	titlebarFits := in.TitlebarNeedW <= 0 || sbW+in.TitlebarNeedW <= winW+0.5

	out := ShellLayout{
		ContentFloor: floor,
		SidebarWidth: sbW,
		ChatWidth:    desiredChat,
	}

	chatVis := in.ChatVisible && !in.InSettings
	if !chatVis || in.ChatFullscreen {
		if in.SidebarVisible {
			if sbW+floor <= winW+0.5 && titlebarFits {
				out.SidebarDocked = true
			} else {
				out.SidebarYielded = true
				out.SidebarDrawer = in.SidebarPinned
			}
		}
		availAfterSidebar := winW
		if out.SidebarDocked {
			availAfterSidebar -= sbW
		}
		if chatVis && in.ChatFullscreen {
			out.ChatFullscreen = true
			out.ChatDocked = true
			out.ChatWidth = availAfterSidebar
			out.MainWidth = 0
			out.ShowOutline = false
			return out
		}
		out.MainWidth = max(availAfterSidebar, 0)
		out.ShowOutline = !in.InSettings && winW >= OutlineShowMin && out.MainWidth >= 620
		return out
	}

	threeColFloor := max(floor, ThreeColumnNoteFloor)
	canDockSidebar := in.SidebarVisible && sbW+threeColFloor+ChatMinW <= winW+0.5 && titlebarFits
	if canDockSidebar {
		out.SidebarDocked = true
	} else if in.SidebarVisible {
		out.SidebarYielded = true
		out.SidebarDrawer = in.SidebarPinned
	}

	dockedSidebar := float32(0)
	pageFloor := floor
	if out.SidebarDocked {
		dockedSidebar = sbW
		pageFloor = threeColFloor
	}

	free := winW - dockedSidebar - pageFloor
	cw := free
	if cw < ChatMinW {
		cw = ChatMinW
	}
	if cw > desiredChat {
		cw = desiredChat
	}
	if cw > winW {
		cw = winW
	}
	out.ChatDocked = true
	out.ChatOverlay = false
	out.ChatWidth = cw

	mainW := winW - dockedSidebar - out.ChatWidth
	if mainW < 0 {
		mainW = 0
	}
	out.MainWidth = mainW
	out.ShowOutline = !in.InSettings && winW >= OutlineShowMin && mainW >= 620
	return out
}
