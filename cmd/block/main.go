package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"block/internal/app"
	"block/internal/files"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

func main() {
	theme.ConfigureDefaultFontSmoothing()

	var (
		dataDirFlag   string
		notebookFlag  string
		hiddenFlag    bool
		smokeFlag     bool
		smokeOnceFlag bool
		mockAIFlag    bool
	)
	flag.StringVar(&dataDirFlag, "data-dir", "", "Override Block application data directory")
	flag.StringVar(&notebookFlag, "notebook", "", "Open a specific notebook folder on launch")
	flag.BoolVar(&hiddenFlag, "hidden", false, "Create windows hidden (for background automation)")
	flag.BoolVar(&smokeFlag, "smoke", false, "Enable visual smoke HUD and interactive smoke runner")
	flag.BoolVar(&smokeOnceFlag, "smoke-once", false, "Run headless/automated full click acceptance suite once and exit")
	flag.BoolVar(&mockAIFlag, "mock-ai", false, "Enable deterministic offline AI responder for E2E verification")
	flag.Parse()

	isIsolatedRun := smokeFlag || smokeOnceFlag || dataDirFlag != "" || os.Getenv("MYGO_AUTOMATION") != ""

	if smokeOnceFlag {
		code := runSmokeOnceCLI(dataDirFlag)
		os.Exit(code)
	}

	if !isIsolatedRun {
		if !mygo.App.RequestSingleInstanceLock() {
			return
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

	st := ws.State()
	switch st.Theme {
	case model.ThemeDark:
		mygo.Theme.SetSource(mygo.ThemeDark)
	case model.ThemeLight:
		mygo.Theme.SetSource(mygo.ThemeLight)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}

	isQuitting := false

	var openMainWindow func() *mygo.Window

	revealMainWindow := func() *mygo.Window {
		w := openMainWindow()
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
			if ws.ClosePluginReadmeWindow(win) {
				return
			}
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
				sess.OpenFind()
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
			ws.StepZoomPct(-app.MainZoomStepPct)
		},
		OnNextTab:   func() { ws.StepTab(1) },
		OnPrevTab:   func() { ws.StepTab(-1) },
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
			"new_note":       modals.EffectiveShortcut(cur, "new_note"),
			"toggle_sidebar": modals.EffectiveShortcut(cur, "toggle_sidebar"),
			"toggle_ai":      modals.EffectiveShortcut(cur, "toggle_ai"),
			"export_image":   modals.EffectiveShortcut(cur, "export_image"),
			"performance":    modals.EffectiveShortcut(cur, "performance"),
		}
	}

	syncMenuAndHotkeys := func(cur model.AppState) {
		mygo.App.SetMenu(app.BuildMenuBar(buildShortcutsMap(cur), menuActions))
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
	mygo.App.SetMenu(app.BuildMenuBar(buildShortcutsMap(st), menuActions))

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
			ws.FlushDirtySessions()
			if runtime.GOOS == "darwin" && !isIsolatedRun && !isQuitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		return win
	}

	mygo.App.OnSecondInstance(func(args []string, _ string) {
		if w := openMainWindow(); w != nil {
			w.Restore()
			w.Focus()
		}
		for _, a := range args[1:] {
			if !strings.HasPrefix(a, "-") && files.IsMarkdown(a) {
				if abs, err := filepath.Abs(a); err == nil {
					_ = ws.OpenNote(abs, 0)
				}
			}
		}
	})

	mygo.App.OnOpenFile(func(path string) {
		if path == "" {
			return
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			_ = ws.SetNotebook(path)
			return
		}
		_ = ws.OpenNote(path, 0)
	})

	mygo.App.OnOpenURL(func(rawURL string) {
		u, err := url.Parse(rawURL)
		if err != nil {
			return
		}
		switch u.Host {
		case "quick":
			ws.QuickNote().Show()
		case "open":
			if p := u.Query().Get("path"); p != "" {
				_ = ws.OpenNote(p, 0)
			}
		case "store", "install":
			revealMainWindow()
			ws.OpenSettings(modals.TabAppStore)
			target := strings.TrimSpace(u.Query().Get("repo"))
			if target == "" {
				target = strings.TrimSpace(u.Query().Get("url"))
			}
			if target == "" {
				target = strings.TrimSpace(u.Query().Get("id"))
			}
			if target != "" && ws.Plugins() != nil {
				ws.Plugins().InstallFromSourceAsync(target, func(pluginID string) {
					ws.OpenSettings(modals.TabPluginPrefix + pluginID)
				})
			}
		}
	})

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

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		isQuitting = true
		modals.SetAppQuitting(true)
		ws.FlushWindowBounds()
		ws.FlushDirtySessions()
		ws.QuickNote().Flush()
		if sup := ws.Plugins(); sup != nil {
			sup.ShutdownAll()
		}
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
