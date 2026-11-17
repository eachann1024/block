package app

// ShellLayout constants mirror thinkly-fast/src/ui/shell_layout.rs 1:1.
const (
	NoteFloor     float32 = 480
	SettingsFloor float32 = 680
	WindowMinW    float32 = 680
	WindowMinH    float32 = 480

	SidebarMinW     float32 = 240
	SidebarDefaultW float32 = 252
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

// ResolveShellLayout resolves sidebar docking/yielding and AI panel docking/overlay
// using the exact priority rules from thinkly-fast/src/ui/shell_layout.rs:
//  1. Content floor (480 for Note, 680 for Settings) is always guaranteed.
//  2. Sidebar docks when sidebarWidth + floor <= windowWidth + 0.5; otherwise it
//     yields (hides automatically, or opens as a floating drawer if SidebarPinned).
//  3. In Settings, AI panel is never shown. Otherwise if ChatFullscreen is active,
//     chat takes the full workspace width after any docked sidebar.
//  4. Otherwise Chat docks when remaining width >= floor + chatWidth (shrinking
//     chatWidth down toward ChatMinW if needed), else overlays on the right.
func ResolveShellLayout(in ShellInput) ShellLayout {
	winW := in.WindowWidth
	if winW < WindowMinW {
		winW = WindowMinW
	}
	floor := NoteFloor
	if in.InSettings {
		floor = SettingsFloor
	}
	sbW := ClampSidebarWidth(in.SidebarDesiredW)
	chatW := ClampChatWidth(in.ChatDesiredW)

	out := ShellLayout{
		ContentFloor: floor,
		SidebarWidth: sbW,
		ChatWidth:    chatW,
	}

	if in.SidebarVisible {
		if sbW+floor <= winW+0.5 {
			out.SidebarDocked = true
		} else {
			out.SidebarYielded = true
			if in.SidebarPinned {
				out.SidebarDrawer = true
			}
		}
	}

	availAfterSidebar := winW
	if out.SidebarDocked {
		availAfterSidebar -= sbW
	}

	// Settings page blocks AI panel per workspace contract.
	chatVis := in.ChatVisible && !in.InSettings
	if chatVis {
		if in.ChatFullscreen {
			out.ChatFullscreen = true
			out.ChatDocked = true
			out.ChatWidth = availAfterSidebar
			out.MainWidth = 0
			out.ShowOutline = false
			return out
		}
		maxDockChat := availAfterSidebar - floor
		if maxDockChat+0.5 >= ChatMinW {
			out.ChatDocked = true
			if chatW > maxDockChat {
				chatW = maxDockChat
				if chatW < ChatMinW {
					chatW = ChatMinW
				}
			}
			out.ChatWidth = chatW
		} else {
			out.ChatOverlay = true
			if chatW > winW-32 {
				chatW = winW - 32
				if chatW < ChatMinW {
					chatW = ChatMinW
				}
			}
			out.ChatWidth = chatW
		}
	}

	mainW := availAfterSidebar
	if out.ChatDocked {
		mainW -= out.ChatWidth
	}
	if mainW < 0 {
		mainW = 0
	}
	out.MainWidth = mainW
	out.ShowOutline = !in.InSettings && winW >= OutlineShowMin && mainW >= 620
	return out
}
