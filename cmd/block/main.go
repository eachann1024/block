package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"block/internal/app"
	"block/internal/files"
	"block/internal/installer"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/theme"
	blockui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

func main() {
	if maybeRunMacOpenRelay() {
		return
	}
	theme.ConfigureDefaultFontSmoothing()

	var (
		dataDirFlag   string
		notebookFlag  string
		hiddenFlag    bool
		smokeFlag           bool
		smokeOnceFlag       bool
		mockAIFlag          bool
		versionFlag         bool
		cleanupFileAssocFlg bool
	)
	flag.StringVar(&dataDirFlag, "data-dir", "", "Override Block application data directory")
	flag.StringVar(&notebookFlag, "notebook", "", "Open a specific notebook folder on launch")
	flag.BoolVar(&hiddenFlag, "hidden", false, "Create windows hidden (for background automation)")
	flag.BoolVar(&smokeFlag, "smoke", false, "Enable visual smoke HUD and interactive smoke runner")
	flag.BoolVar(&smokeOnceFlag, "smoke-once", false, "Run headless/automated full click acceptance suite once and exit")
	flag.BoolVar(&mockAIFlag, "mock-ai", false, "Enable deterministic offline AI responder for E2E verification")
	flag.BoolVar(&versionFlag, "version", false, "Print version and exit")
	flag.BoolVar(&cleanupFileAssocFlg, "cleanup-file-associations", false, "Remove current user's Block-managed OS file associations and exit")
	var syncSysLinuxDesktopFlg bool
	var cleanupSysLinuxDesktopFlg bool
	var syncUserLinuxFileAssocFlg bool
	var requireDesktopSessionFlg bool
	var printLinuxHelperIDFlg bool
	flag.BoolVar(&syncSysLinuxDesktopFlg, "sync-system-linux-desktop", false, "Sync system Linux block.desktop with default-app protection and exit")
	flag.BoolVar(&cleanupSysLinuxDesktopFlg, "cleanup-system-linux-desktop", false, "Remove Block-managed system Linux default-app pins and exit")
	flag.BoolVar(&syncUserLinuxFileAssocFlg, "sync-user-linux-file-associations", false, "Sync current non-root user's protected Linux Open-With associations and exit")
	flag.BoolVar(&requireDesktopSessionFlg, "require-desktop-session", false, "With --sync-user-linux-file-associations, skip silently when XDG_CURRENT_DESKTOP is unset")
	flag.BoolVar(&printLinuxHelperIDFlg, "print-linux-helper-id", false, "Print this installation's Linux helper desktop ID and exit")
	installerUIFlag := flag.Bool("installer-ui", false, "Show the Windows setup window (started by the setup executable)")
	flag.Parse()

	if *installerUIFlag {
		os.Exit(installer.Run())
	}
	if versionFlag {
		fmt.Println(updater.CurrentVersion())
		return
	}
	if printLinuxHelperIDFlg {
		if id := plugins.LinuxHelperDesktopID(); id != "" {
			fmt.Println(id)
		}
		return
	}
	if syncSysLinuxDesktopFlg {
		if err := plugins.SyncSystemLinuxDesktop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cleanupSysLinuxDesktopFlg {
		if err := plugins.CleanupSystemLinuxDesktop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if syncUserLinuxFileAssocFlg {
		if dataDirFlag != "" {
			files.SetDataDirOverride(dataDirFlag)
		}
		if err := plugins.SyncLinuxUserFileAssociations(files.AppDataDir(), requireDesktopSessionFlg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cleanupFileAssocFlg {
		isolated := dataDirFlag != "" || strings.TrimSpace(os.Getenv("BLOCK_DATA_DIR")) != ""
		if dataDirFlag != "" {
			files.SetDataDirOverride(dataDirFlag)
		}
		if err := plugins.CleanupFileAssociations(files.AppDataDir(), isolated); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	isIsolatedRun := smokeFlag || smokeOnceFlag || dataDirFlag != "" || os.Getenv("MYGO_AUTOMATION") != ""

	if smokeOnceFlag {
		code := runSmokeOnceCLI(dataDirFlag)
		os.Exit(code)
	}

	if !isIsolatedRun {
		if !mygo.App.RequestSingleInstanceLock() {
			if strings.TrimSpace(os.Getenv("BLOCK_DATA_DIR")) == "" {
				_ = plugins.SyncCurrentExecFileAssociations(files.AppDataDir())
			}
			return
		}
		if strings.TrimSpace(os.Getenv("BLOCK_DATA_DIR")) == "" {
			plugins.SetFileAssociationSyncEnabled(true)
		}
	}

	mygo.App.SetName("Block")
	mygo.App.SetLanguages("zh-Hans", "en")
	if smokeFlag || os.Getenv("MYGO_AUTOMATION") != "" {
		blockui.EnableSmokeMode()
	}

	ws := app.NewWorkspace(dataDirFlag)
	if !isIsolatedRun {
		files.EnsureHarmonyFontsAsync(files.AppDataDir(), ws.InvalidateAllWindows)
	}
	ws.SetPersistWindowBounds(!smokeFlag && !smokeOnceFlag && !hiddenFlag)
	if mockAIFlag {
		ws.AI().EnableMockResponder(true)
	}
	if notebookFlag != "" {
		_ = ws.SetNotebook(notebookFlag)
	}
	var launchStoreLink string
	if launchArgs := flag.Args(); len(launchArgs) > 0 {
		launchWD, _ := os.Getwd()
		for _, raw := range launchArgs {
			if raw == "" || strings.HasPrefix(raw, "-psn_") {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(raw))
			if strings.HasPrefix(lower, "block://") || strings.HasPrefix(lower, "blockmd://") {
				if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
					switch u.Host {
					case "open":
						if p := u.Query().Get("path"); p != "" {
							_ = ws.OpenDeepLinkedFile(p)
						}
					case "store", "install":
						launchStoreLink = strings.TrimSpace(raw)
					}
				}
				continue
			}
			_ = ws.OpenExternalTarget(raw, launchWD)
		}
	}

	st := ws.State()
	switch st.Theme {
	case model.ThemeDark:
		mygo.Theme.SetSource(mygo.ThemeDark)
	case model.ThemeLight:
		mygo.Theme.SetSource(mygo.ThemeLight)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	files.LogInfof("app started: version=%s os=%s arch=%s lang=%s theme=%s",
		updater.CurrentVersion(), runtime.GOOS, runtime.GOARCH, theme.Language(), st.Theme)

	isQuitting := false

	var openMainWindow func() *mygo.Window

	revealMainWindow := func() *mygo.Window {
		w := openMainWindow()
		if hiddenFlag {
			return w
		}
		if w != nil {
			w.Restore()
			w.Show()
			w.Focus()
		}
		if mygo.App.IsReady() {
			mygo.App.Focus()
		}
		return w
	}
	ws.OnRevealMainWindow = func() {
		_ = revealMainWindow()
	}
	ws.OnCloseMainWindow = func() {
		if win := ws.Window(); win != nil {
			win.Close()
		}
	}

	menuActions := app.MenuActions{
		OnAbout: func() {
			ws.Modals().OpenAbout()
		},
		OnCheckUpdates: func() {
			revealMainWindow()
			ws.Modals().OpenUpdater()
			if ws.Updater() != nil {
				ws.Updater().CheckAsync(true)
			}
		},
		OnPerformance: func() {
			ws.Modals().OpenPerformance()
		},
		OnSettings: func() {
			ws.OpenSettings("ai")
		},
		OnNewNote: func() {
			_, _ = ws.CreateNewNote()
		},
		OnNewFolder: func() {
			_, _ = ws.CreateNewFolder()
		},
		OnNewTab: func() {
			_, _ = ws.CreateNewNote()
		},
		OnQuickNote: func() {
			ws.QuickNote().Toggle()
		},
		OnSearch: func() {
			ws.SearchWindow().Hide()
			revealMainWindow()
			ws.Modals().OpenSearch(false)
			ws.RequestRedraw()
		},
		OnExportImage: func() {
			ws.OpenExportStage()
		},
		OnCloseTab: func(win *mygo.Window) {
			if ws.SearchWindow().IsFocused() {
				ws.SearchWindow().Hide()
				return
			}
			if ws.QuickNote().IsFocused() {
				ws.QuickNote().Hide()
				return
			}
			if ws.Modals().IsSearchOpen() {
				ws.Modals().CloseSearch()
				return
			}
			ws.CloseActiveTab()
		},
		OnReopenTab: func() {
			ws.ReopenClosedTab()
		},
		OnFind: func() {
			if ws.QuickNote().IsFocused() {
				ws.QuickNote().OpenFind()
				return
			}
			if sess := ws.ActiveSession(); sess != nil {
				sess.ToggleFind()
				ws.RequestRedraw()
			}
		},
		OnToggleSidebar: func() {
			ws.ToggleSidebar(0)
		},
		OnToggleAIPanel: func() {
			ws.ToggleAIPanel()
		},
		OnZoomReset: func() {
			if ws.Modals().IsRecordingShortcut() {
				return
			}
			if ws.QuickNote().IsFocused() {
				ws.QuickNote().ResetZoom()
				return
			}
			if ws.Modals().IsSettingsOpen() || ws.Modals().HasActiveModal() || ws.Export().IsOpen() || ws.IsPageHistoryOpen() {
				return
			}
			if sess := ws.ActiveSession(); sess != nil && sess.HandleMindMapZoom(0) {
				ws.RequestRedraw()
				return
			}
			ws.ResetZoomPct()
		},
		OnZoomIn: func() {
			if ws.Modals().IsRecordingShortcut() {
				return
			}
			if ws.QuickNote().IsFocused() {
				ws.QuickNote().StepZoom(10)
				return
			}
			if ws.Modals().IsSettingsOpen() || ws.Modals().HasActiveModal() || ws.Export().IsOpen() || ws.IsPageHistoryOpen() {
				return
			}
			if sess := ws.ActiveSession(); sess != nil && sess.HandleMindMapZoom(1) {
				ws.RequestRedraw()
				return
			}
			ws.StepZoomPct(app.MainZoomStepPct)
		},
		OnZoomOut: func() {
			if ws.Modals().IsRecordingShortcut() {
				return
			}
			if ws.QuickNote().IsFocused() {
				ws.QuickNote().StepZoom(-10)
				return
			}
			if ws.Modals().IsSettingsOpen() || ws.Modals().HasActiveModal() || ws.Export().IsOpen() || ws.IsPageHistoryOpen() {
				return
			}
			if sess := ws.ActiveSession(); sess != nil && sess.HandleMindMapZoom(-1) {
				ws.RequestRedraw()
				return
			}
			ws.StepZoomPct(-app.MainZoomStepPct)
		},
		OnNextTab: func() { ws.StepTab(1) },
		OnPrevTab: func() { ws.StepTab(-1) },
		OnNavigateBack: func() {
			ws.NavigateBack()
		},
		OnNavigateFwd: func() {
			ws.NavigateForward()
		},
	}

	buildShortcutsMap := func(cur model.AppState) map[string]string {
		return map[string]string{
			"global_summon":  modals.EffectiveShortcut(cur, "global_summon"),
			"quick_note":     modals.EffectiveShortcut(cur, "quick_note"),
			"search":         modals.EffectiveShortcut(cur, "search"),
			"find":           modals.EffectiveShortcut(cur, "find"),
			"new_note":       modals.EffectiveShortcut(cur, "new_note"),
			"toggle_sidebar": modals.EffectiveShortcut(cur, "toggle_sidebar"),
			"toggle_ai":      modals.EffectiveShortcut(cur, "toggle_ai"),
			"export_image":   modals.EffectiveShortcut(cur, "export_image"),
			"performance":    modals.EffectiveShortcut(cur, "performance"),
		}
	}

	syncMenu := func(cur model.AppState) {
		actions := menuActions
		if !ws.AI().Enabled() {
			actions.OnToggleAIPanel = nil
		}
		mygo.App.SetMenu(app.BuildMenuBar(buildShortcutsMap(cur), actions))
	}
	syncMenuAndHotkeys := func(cur model.AppState) {
		syncMenu(cur)
		if !mygo.App.IsReady() || isIsolatedRun {
			return
		}
		mygo.GlobalShortcut.UnregisterAll()
		if quickAccel := blockui.ToMyGoAccelerator(modals.EffectiveShortcut(cur, "quick_note")); quickAccel != "" {
			_ = mygo.GlobalShortcut.Register(quickAccel, func() {
				if ws.Modals().IsRecordingShortcut() {
					return
				}
				ws.QuickNote().TriggerHotkey()
			})
		}
		if summonAccel := blockui.ToMyGoAccelerator(modals.EffectiveShortcut(cur, "global_summon")); summonAccel != "" {
			_ = mygo.GlobalShortcut.Register(summonAccel, func() {
				if ws.Modals().IsRecordingShortcut() || !modals.MarkSearchTriggered() {
					return
				}
				mainWin := ws.Window()
				mainVisible := mainWin != nil && !mygo.App.IsHidden() && mainWin.IsVisible() && !mainWin.IsMinimized()
				if !mainVisible {
					ws.SearchWindow().Hide()
					revealMainWindow()
					ws.Modals().OpenSearch(true)
					ws.RequestRedraw()
					return
				}
				if mainWin.IsFocused() && mygo.App.IsActive() {
					ws.SearchWindow().Hide()
					ws.Modals().OpenSearch(true)
					ws.RequestRedraw()
					return
				}
				ws.SearchWindow().Toggle()
			})
		}
	}

	ws.OnAppMenuOrHotkeysChanged = syncMenuAndHotkeys
	syncMenu(st)

	openMainWindow = func() *mygo.Window {
		if existing := ws.Window(); existing != nil {
			if !hiddenFlag {
				if mygo.App.IsReady() && !existing.IsFullScreen() && !existing.IsMaximized() &&
					!app.IsMainRectVisibleOnDisplays(existing.Bounds(), mygo.Screen.Displays()) {
					resolved := ws.ResolveMainWindowOptions()
					existing.SetBounds(resolved.Rect)
				}
				existing.Show()
				existing.Focus()
			}
			return existing
		}
		resolved := ws.ResolveMainWindowOptions()
		winOpts := mygo.WindowOptions{
			Title:                "Block",
			Width:                resolved.Rect.Width,
			Height:               resolved.Rect.Height,
			MinWidth:             int(app.WindowMinW),
			MinHeight:            int(app.WindowMinH),
			Hidden:               hiddenFlag,
			Maximized:            resolved.Maximized,
			FullScreen:           resolved.Fullscreen,
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       int(app.TitlebarHeight),
			TrafficLightPosition: &mygo.Point{X: 16, Y: 13},
			BackgroundColor:      "light-dark(#f0f2f4, #14191e)",
			Content:              ui.View(ws.Render),
			Page:                 mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
		}
		if resolved.HasPosition {
			winOpts.X = resolved.Rect.X
			winOpts.Y = resolved.Rect.Y
		}
		win := mygo.NewWindow(winOpts)
		ws.AttachWindow(win)
		if resolved.HasPosition && resolved.Rect.X == 0 && resolved.Rect.Y == 0 &&
			!resolved.Maximized && !resolved.Fullscreen && !hiddenFlag {
			win.SetBounds(resolved.Rect)
		}
		win.OnClose(func(e *mygo.CloseEvent) {
			ws.FlushWindowBounds()
			if err := ws.FlushDirtySessions(); err != nil {
				e.PreventDefault()
				return
			}
			if runtime.GOOS == "darwin" && !isIsolatedRun && !isQuitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		return win
	}

	handleDeepLink := func(rawURL string) {
		u, err := url.Parse(rawURL)
		if err != nil || u.User != nil || (u.Scheme != "block" && u.Scheme != "blockmd") {
			return
		}
		switch u.Host {
		case "quick":
			ws.QuickNote().Show()
		case "open":
			if p := u.Query().Get("path"); p != "" {
				if mygo.App.IsReady() {
					revealMainWindow()
				}
				_ = ws.OpenDeepLinkedFile(p)
			}
		case "store", "install":
			if !mygo.App.IsReady() {
				launchStoreLink = rawURL
				return
			}
			revealMainWindow()
			target := strings.TrimSpace(u.Query().Get("repo"))
			if target == "" {
				target = strings.TrimSpace(u.Query().Get("url"))
			}
			if target == "" {
				target = strings.TrimSpace(u.Query().Get("id"))
			}
			ws.OpenPluginStoreSource(target)
		}
	}

	mygo.App.OnSecondInstance(func(args []string, wd string) {
		revealMainWindow()
		endOfFlags := false
		for _, raw := range args {
			if raw == "" {
				continue
			}
			if !endOfFlags {
				if raw == "--" {
					endOfFlags = true
					continue
				}
				if strings.HasPrefix(raw, "-") {
					continue
				}
			}
			lower := strings.ToLower(strings.TrimSpace(raw))
			if strings.HasPrefix(lower, "block://") || strings.HasPrefix(lower, "blockmd://") {
				handleDeepLink(strings.TrimSpace(raw))
				continue
			}
			_ = ws.OpenExternalTarget(raw, wd)
		}
	})

	mygo.App.OnOpenFile(func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		if mygo.App.IsReady() {
			revealMainWindow()
		}
		_ = ws.OpenExternalTarget(path, "")
	})

	mygo.App.OnOpenURL(handleDeepLink)
	var launchNotification string
	pluginSystemAttached := false
	if !smokeFlag && os.Getenv("MYGO_AUTOMATION") == "" {
		mygo.App.OnNotificationClick(func(id string) {
			if !pluginSystemAttached {
				launchNotification = id
				return
			}
			ws.HandlePluginNotificationClick(id)
		})
	}

	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" || isIsolatedRun {
			mygo.App.Quit()
		}
	})

	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hiddenFlag {
			mainWin := ws.Window()
			if !hasVisibleWindows || mainWin == nil || !mainWin.IsVisible() {
				openMainWindow()
			}
		}
	})

	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if err := ws.FlushDirtySessions(); err != nil {
			e.PreventDefault()
			return
		}
		if err := ws.QuickNote().Flush(); err != nil {
			e.PreventDefault()
			return
		}
		isQuitting = true
		modals.SetAppQuitting(true)
		ws.FlushWindowBounds()
		if sup := ws.Plugins(); sup != nil {
			sup.ShutdownAll()
		}
		plugins.WaitFileAssociationSyncTimeout(800 * time.Millisecond)
	})

	mygo.App.OnQuitCanceled(func() {
		isQuitting = false
		modals.SetAppQuitting(false)
	})

	mygo.App.WhenReady(func() {
		theme.SetThemeMode(ws.State().Theme, mygo.Theme.IsDark())
		mygo.Theme.OnUpdated(func() {
			theme.SetThemeMode(ws.State().Theme, mygo.Theme.IsDark())
			ws.SearchWindow().Invalidate()
			ws.QuickNote().RefreshStyle()
			ws.RequestRedraw()
		})

		openMainWindow()
		if !smokeFlag && os.Getenv("MYGO_AUTOMATION") == "" {
			ws.AttachPluginSystem(func() { revealMainWindow() })
			pluginSystemAttached = true
			if launchNotification != "" {
				ws.HandlePluginNotificationClick(launchNotification)
				launchNotification = ""
			}
		}
		if !smokeFlag && !smokeOnceFlag && os.Getenv("MYGO_AUTOMATION") == "" {
			ws.AI().PrewarmModelsAsync()
		}
		if launchStoreLink != "" {
			handleDeepLink(launchStoreLink)
			launchStoreLink = ""
		}
		ws.QuickNote().AttachDesktop()
		ws.SearchWindow().AttachDesktop()
		syncMenuAndHotkeys(ws.State())
		if (!isIsolatedRun || os.Getenv("BLOCK_MOCK_UPDATE") == "1") && ws.Updater() != nil {
			ws.Updater().StartAutomatic()
		}
	})

	if err := mygo.App.Run(); err != nil {
		files.LogErrorf("App.Run exited with error: %v", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
