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
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
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
	if smokeFlag || os.Getenv("MYGO_AUTOMATION") != "" {
		blockui.EnableSmokeMode()
	}

	ws := app.NewWorkspace(dataDirFlag)
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

	shortcuts := map[string]string{
		"search":     st.ShortcutSearch,
		"quick_note": st.ShortcutQuick,
	}
	menu := app.BuildMenuBar(shortcuts, app.MenuActions{
		OnAbout: func() {
			ws.Modals().OpenAbout()
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
			ws.Modals().OpenSearch(false)
		},
		OnExportImage: func() {
			if active := ws.ActiveNotePath(); active != "" {
				ws.Modals().OpenExport(active)
			}
		},
		OnCloseTab: func() {
			ws.CloseActiveTab()
		},
		OnReopenTab: func() {
			ws.ReopenClosedTab()
		},
		OnFind: func() {
			if sess := ws.ActiveSession(); sess != nil {
				sess.OpenFind()
				ws.RequestRedraw()
			}
		},
		OnToggleSidebar: func() {
			winW := float32(1100)
			if win := ws.Window(); win != nil {
				cw, _ := win.ContentSize()
				if cw > 0 {
					winW = float32(cw)
				}
			}
			ws.ToggleSidebar(winW)
		},
		OnToggleAIPanel: func() {
			ws.ToggleAIPanel()
		},
		OnZoomReset: func() { ws.SetZoomPct(100) },
		OnZoomIn:    func() { ws.SetZoomPct(110) },
		OnZoomOut:   func() { ws.SetZoomPct(90) },
		OnNextTab:   func() { ws.StepTab(1) },
		OnPrevTab:   func() { ws.StepTab(-1) },
		OnNavigateBack: func() {
			ws.NavigateBack()
		},
		OnNavigateFwd: func() {
			ws.NavigateForward()
		},
	})
	mygo.App.SetMenu(menu)

	openMainWindow := func() *mygo.Window {
		if existing := ws.Window(); existing != nil {
			if !hiddenFlag {
				existing.Show()
				existing.Focus()
			}
			return existing
		}
		stateKey := "main"
		if isIsolatedRun {
			stateKey = ""
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:                "Block",
			Width:                1100,
			Height:               760,
			MinWidth:             int(app.WindowMinW),
			MinHeight:            int(app.WindowMinH),
			Hidden:               hiddenFlag,
			StateKey:             stateKey,
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       int(app.TitlebarHeight),
			TrafficLightPosition: &mygo.Point{X: 16, Y: 13},
			BackgroundColor:      "light-dark(#f0f2f4, #14191e)",
			Content:              ui.View(ws.Render),
		})
		ws.AttachWindow(win)
		win.OnClose(func(*mygo.CloseEvent) {
			ws.FlushDirtySessions()
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
		}
	})

	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" || isIsolatedRun {
			mygo.App.Quit()
		}
	})

	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && !hiddenFlag {
			openMainWindow()
		}
	})

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		ws.FlushDirtySessions()
	})

	mygo.App.WhenReady(func() {
		theme.SetThemeMode(ws.State().Theme, mygo.Theme.IsDark())
		mygo.Theme.OnUpdated(func() {
			theme.SetThemeMode(ws.State().Theme, mygo.Theme.IsDark())
			ws.RequestRedraw()
		})

		openMainWindow()
		if !hiddenFlag {
			ws.QuickNote().AttachDesktop()
		}

		if !isIsolatedRun {
			quickAccel := blockui.ToMyGoAccelerator("alt-n")
			if custom := strings.TrimSpace(ws.State().ShortcutQuick); custom != "" {
				if parsed := blockui.ToMyGoAccelerator(custom); parsed != "" {
					quickAccel = parsed
				}
			}
			_ = mygo.GlobalShortcut.Register(quickAccel, func() {
				ws.QuickNote().TriggerHotkey()
			})
		}
	})

	if err := mygo.App.Run(); err != nil {
		files.LogErrorf("App.Run exited with error: %v", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
