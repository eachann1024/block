// Package installer draws Block's own setup window. The NSIS setup executable
// starts it (block.exe --installer-ui) in place of its wizard pages; the window
// then runs that same setup executable silently to lay down files and registry
// keys, so every install rule stays in scripts/release/block.nsi.
package installer

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/assets"
	"block/internal/icons"
	"block/internal/theme"
	bui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Environment handed over by the NSIS stub (see .onInit in block.nsi).
const (
	EnvSetupExe = "BLOCK_SETUP_EXE"      // setup executable to run silently
	EnvDir      = "BLOCK_SETUP_DIR"      // initial install directory
	EnvUpdate   = "BLOCK_SETUP_UPDATE"   // "1": started by the in-app updater
	EnvAllUsers = "BLOCK_SETUP_ALLUSERS" // "1": machine-wide install requested
	// EnvPreview shows the window without installing anything, for design
	// review on any platform: a comma list of a stage (ready, progress, done,
	// error) and the modifiers hidden and dark.
	EnvPreview = "BLOCK_SETUP_PREVIEW"
)

const (
	windowWidth  = 520
	windowHeight = 340
	titlebarH    = 36
	footerH      = 44
	iconSize     = 72
	actionAreaH  = 76
	progressW    = 220
	progressH    = 3
	badgeSize    = 22
	appExeName   = "block.exe"
	// lockWait is how long an install waits for a running Block to exit.
	lockWait = 20 * time.Second
)

type stage int

const (
	stageReady stage = iota
	stageInstalling
	stageDone
	stageError
)

var errAppRunning = errors.New("block is running")

type setup struct {
	mu       sync.Mutex
	win      *mygo.Window
	stage    stage
	dir      string
	setupExe string
	update   bool
	allUsers bool
	preview  bool
	previews map[string]bool
	errText  string
	started  time.Time
	picking  bool
	icon     ui.ImageSource
	tr       stringsTable
}

// Run shows the setup window and blocks until it closes. It returns the process
// exit code: 0 once the window took over the install, non-zero when it could
// not start, which makes the NSIS stub fall back to its classic pages.
func Run() int {
	s := &setup{
		dir:      strings.TrimSpace(os.Getenv(EnvDir)),
		setupExe: strings.TrimSpace(os.Getenv(EnvSetupExe)),
		update:   os.Getenv(EnvUpdate) == "1",
		allUsers: os.Getenv(EnvAllUsers) == "1",
		previews: map[string]bool{},
	}
	var previewLang string
	for _, part := range strings.Split(os.Getenv(EnvPreview), ",") {
		if part = strings.TrimSpace(part); part != "" {
			s.preview = true
			if after, ok := strings.CutPrefix(part, "lang="); ok {
				previewLang = strings.TrimSpace(after)
				continue
			}
			s.previews[part] = true
		}
	}
	if s.setupExe == "" && !s.preview {
		return 2
	}
	if s.dir == "" {
		s.dir = defaultInstallDir()
	}
	if data, err := assets.Read("app-icon/apple-touch-icon.png"); err == nil {
		if img, decErr := ui.DecodeImage(data); decErr == nil {
			s.icon = img
		}
	}
	s.tr = resolveStrings(previewLang)
	theme.SetLanguage(s.tr.appLang)

	mygo.App.SetLanguages("en", "zh-Hans", "zh-Hant", "ja")
	mygo.App.SetName(s.tr.title)
	if s.previews["dark"] {
		mygo.Theme.SetSource(mygo.ThemeDark)
	}
	mygo.App.WhenReady(func() {
		theme.SetDark(mygo.Theme.IsDark())
		mygo.Theme.OnUpdated(func() {
			theme.SetDark(mygo.Theme.IsDark())
			s.invalidate()
		})
		s.open()
	})
	if err := mygo.App.Run(); err != nil {
		return 1
	}
	return 0
}

func (s *setup) open() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             s.tr.title,
		Width:             windowWidth,
		Height:            windowHeight,
		Hidden:            s.previews["hidden"],
		TitleBarStyle:     mygo.TitleBarHidden,
		HideWindowButtons: true,
		DisableResize:     true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		BackgroundColor:   "light-dark(#ffffff, #181818)",
		Content:           ui.View(s.render),
		Page:              mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
	})
	s.mu.Lock()
	s.win = win
	s.mu.Unlock()
	win.OnClose(func(e *mygo.CloseEvent) {
		// Closing mid-install would leave a half-written installation behind.
		if s.snapshot().stage == stageInstalling {
			e.PreventDefault()
		}
	})
	win.OnClosed(func() { mygo.App.Quit() })

	switch {
	case s.previews["progress"]:
		s.mu.Lock()
		s.stage, s.started = stageInstalling, time.Now()
		s.mu.Unlock()
	case s.previews["done"]:
		s.setStage(stageDone, "")
	case s.previews["error"]:
		s.setStage(stageError, s.tr.errAppRunning)
	default:
		if s.update {
			s.startInstall()
		}
	}
}

type snapshot struct {
	stage   stage
	dir     string
	errText string
	started time.Time
	update  bool
}

func (s *setup) snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshot{stage: s.stage, dir: s.dir, errText: s.errText, started: s.started, update: s.update}
}

func (s *setup) invalidate() {
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
}

func (s *setup) setStage(st stage, errText string) {
	s.mu.Lock()
	s.stage, s.errText = st, errText
	s.mu.Unlock()
	s.invalidate()
}

func (s *setup) closeWindow() {
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win != nil {
		win.Close()
	}
}

func (s *setup) minimizeWindow() {
	s.mu.Lock()
	win := s.win
	s.mu.Unlock()
	if win != nil {
		win.Minimize()
	}
}

// startInstall runs the silent setup off the UI thread and moves to the done or
// error stage when it returns.
func (s *setup) startInstall() {
	s.mu.Lock()
	if s.stage == stageInstalling {
		s.mu.Unlock()
		return
	}
	s.stage, s.errText, s.started = stageInstalling, "", time.Now()
	dir, setupExe, update, allUsers, preview := s.dir, s.setupExe, s.update, s.allUsers, s.preview
	s.mu.Unlock()
	s.invalidate()

	go func() {
		err := install(setupExe, dir, allUsers, preview)
		if err != nil {
			s.setStage(stageError, s.installErrorText(err))
			return
		}
		// Let the bar visibly finish before the stage switches.
		time.Sleep(350 * time.Millisecond)
		s.setStage(stageDone, "")
		if update && !preview {
			// An update returns the user straight to the app they came from.
			time.Sleep(600 * time.Millisecond)
			_ = launchInstalledApp(filepath.Join(dir, appExeName))
			s.closeWindow()
		}
	}()
}

func install(setupExe, dir string, allUsers, preview bool) error {
	if preview {
		time.Sleep(2500 * time.Millisecond)
		return nil
	}
	dir = cleanInstallDir(dir)
	if !validInstallDir(dir) {
		return errInvalidDir
	}
	target := filepath.Join(dir, appExeName)
	if !setupNeedsElevation(dir, allUsers) && !waitWritable(target, lockWait) {
		return errAppRunning
	}
	if err := runSilentSetup(setupExe, dir, allUsers); err != nil {
		return err
	}
	// A silent NSIS run can exit cleanly without having used /D=, so confirm
	// the executable really landed where the user asked.
	if _, err := os.Stat(target); err != nil {
		return errNotInstalled
	}
	return nil
}

var (
	errInvalidDir        = errors.New("invalid install directory")
	errNotInstalled      = errors.New("setup finished without installing")
	errElevationCanceled = errors.New("administrator elevation was canceled")
)

func (s *setup) installErrorText(err error) string {
	switch {
	case errors.Is(err, errAppRunning):
		return s.tr.errAppRunning
	case errors.Is(err, errInvalidDir):
		return s.tr.errInvalidDir
	case errors.Is(err, errElevationCanceled):
		return s.tr.errElevationCanceled
	default:
		return s.tr.errGeneric
	}
}

// waitWritable waits until target is absent or can be opened for writing, i.e.
// no running Block still holds its executable.
func waitWritable(target string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			return true
		}
		f, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err == nil {
			_ = f.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// cleanInstallDir trims the path the way NSIS reads /D=: no quotes and no
// trailing separator (a drive root keeps its own).
func cleanInstallDir(dir string) string {
	dir = strings.TrimSpace(strings.Trim(strings.TrimSpace(dir), `"`))
	for len(dir) > 3 && (strings.HasSuffix(dir, `\`) || strings.HasSuffix(dir, "/")) {
		dir = dir[:len(dir)-1]
	}
	return dir
}

// pickedInstallDir turns a folder chosen in the picker into the install
// directory: Block gets its own "Block" folder unless the user picked one.
func pickedInstallDir(picked string) string {
	picked = cleanInstallDir(picked)
	if picked == "" {
		return ""
	}
	base := picked
	if i := strings.LastIndexAny(picked, `\/`); i >= 0 {
		base = picked[i+1:]
	}
	if strings.EqualFold(base, "Block") {
		return picked
	}
	sep := `\`
	if !strings.Contains(picked, `\`) && strings.Contains(picked, "/") {
		sep = "/"
	}
	if strings.HasSuffix(picked, `\`) || strings.HasSuffix(picked, "/") {
		return picked + "Block"
	}
	return picked + sep + "Block"
}

func (s *setup) chooseDir() {
	s.mu.Lock()
	if s.picking || s.stage == stageInstalling {
		s.mu.Unlock()
		return
	}
	s.picking = true
	win, current := s.win, s.dir
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.picking = false
			s.mu.Unlock()
		}()
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            win,
			Title:             s.tr.chooseDirTitle,
			DefaultPath:       existingAncestor(current),
			ButtonLabel:       s.tr.selectBtn,
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		if dir := pickedInstallDir(paths[0]); dir != "" {
			s.mu.Lock()
			s.dir = dir
			if s.stage == stageError {
				s.stage, s.errText = stageReady, ""
			}
			s.mu.Unlock()
			s.invalidate()
		}
	}()
}

// existingAncestor returns the nearest directory on the way up from dir that
// exists, so the picker opens somewhere real before the first install.
func existingAncestor(dir string) string {
	for dir != "" {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (s *setup) render(c *ui.Context) {
	pal := theme.Ocean()
	snap := s.snapshot()
	busy := snap.stage == stageInstalling

	ui.Column(c).Fill().Background(pal.Paper).Children(func() {
		ui.Row(c).FillWidth().Height(titlebarH).Shrink(0).AlignItems(ui.Center).PaddingX(theme.Space6).Children(func() {
			ui.Box(c).Fill().DragWindow()
			minBtn := bui.IconButton(c, icons.Minus, bui.IconButtonOptions{
				ID: "setup-minimize", Label: s.tr.minimize, Variant: bui.IconPlain,
			})
			if minBtn.Clicked() {
				s.minimizeWindow()
			}
			closeBtn := bui.IconButton(c, icons.Close, bui.IconButtonOptions{
				ID: "setup-close", Label: s.tr.close, Variant: bui.IconPlain, Disabled: busy,
			})
			if closeBtn.Clicked() {
				s.closeWindow()
			}
		})

		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).DragWindow().Children(func() {
			s.renderIcon(c, snap.stage == stageDone)
			ui.Box(c).Size(1, theme.Space12).Shrink(0)
			ui.Text(c, "Block").FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
				FontWeight(theme.WeightTitle).TextColor(pal.Text).SingleLine()
			ui.Box(c).Size(1, theme.Space2).Shrink(0)
			ui.Text(c, updater.CurrentVersion()).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.TextSecondary).SingleLine()

			ui.Column(c).FillWidth().Height(actionAreaH).Shrink(0).AlignItems(ui.Center).Justify(ui.End).
				PaddingX(theme.Space24).Children(func() {
				s.renderAction(c, snap)
			})
		})

		footer := ui.Row(c).FillWidth().Height(footerH).Shrink(0).AlignItems(ui.Center).
			Justify(ui.SpaceBetween).Gap(theme.Space12).PaddingX(theme.Space16)
		footer.Children(func() {
			if snap.update || (snap.stage != stageReady && snap.stage != stageError) {
				return
			}
			ui.Text(c, snap.dir).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
				TextColor(pal.TextSecondary).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			change := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
				ID:          "setup-change-dir",
				Label:       s.tr.changeLocation,
				Style:       bui.ButtonGhost,
				Height:      theme.HeightChrome,
				PaddingX:    theme.Space8,
				CompactText: true,
			})
			if change.Clicked() {
				s.chooseDir()
			}
		})
	})
}

func (s *setup) renderIcon(c *ui.Context, done bool) {
	pal := theme.Ocean()
	box := ui.Box(c.Key("setup-icon")).Size(iconSize, iconSize).Shrink(0)
	box.Children(func() {
		if s.icon != nil {
			ui.Image(c, s.icon).Size(iconSize, iconSize)
		}
	})
	if !done {
		return
	}
	box.DrawOver(func(p *ui.Painter, r ui.Rect) {
		const ring = 2
		outer := ui.Rect{X: r.X + r.W - badgeSize + 6, Y: r.Y + r.H - badgeSize + 6, W: badgeSize, H: badgeSize}
		p.Fill(outer, pal.Paper, badgeSize/2)
		inner := ui.Rect{X: outer.X + ring, Y: outer.Y + ring, W: outer.W - 2*ring, H: outer.H - 2*ring}
		p.Fill(inner, pal.Accent, inner.W/2)
		cx, cy := inner.X+inner.W/2, inner.Y+inner.H/2
		p.StrokePolyline([][2]float32{{cx - 4, cy + 0.5}, {cx - 1.2, cy + 3.2}, {cx + 4.2, cy - 3}}, 1.8, pal.AccentText)
	})
}

func (s *setup) renderAction(c *ui.Context, snap snapshot) {
	pal := theme.Ocean()
	switch snap.stage {
	case stageReady:
		btn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID: "setup-install", Label: s.tr.install, Style: bui.ButtonPrimary,
			MinWidth: 120, Height: theme.HeightControl,
		})
		if btn.Clicked() {
			s.startInstall()
		}
	case stageInstalling:
		started := snap.started
		ui.Box(c.Key("setup-progress")).Size(progressW, progressH).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			// The silent setup reports no progress, so ease toward 92% and let
			// completion close the gap.
			elapsed := p.Now().Sub(started).Seconds()
			pct := float32(0.92 * (1 - math.Exp(-elapsed/1.1)))
			p.Fill(r, pal.Selected, r.H/2)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * pct, H: r.H}, pal.Accent, r.H/2)
			p.AnimationFrameRate(60)
		})
		label := s.tr.installing
		if snap.update {
			label = s.tr.updating
		}
		ui.Box(c).Size(1, theme.Space10).Shrink(0)
		ui.Text(c, label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
			TextColor(pal.TextSecondary).SingleLine()
		ui.Box(c).Size(1, theme.Space4).Shrink(0)
	case stageDone:
		btn := bui.ActionButtonWithOptions(c, bui.ActionButtonOptions{
			ID: "setup-open", Label: s.tr.openBlock, Style: bui.ButtonPrimary,
			MinWidth: 120, Height: theme.HeightControl,
		})
		if btn.Clicked() {
			if !s.preview {
				_ = launchInstalledApp(filepath.Join(cleanInstallDir(snap.dir), appExeName))
			}
			s.closeWindow()
		}
	case stageError:
		ui.Text(c, snap.errText).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
			TextColor(pal.Danger).SingleLine().Ellipsis("…")
		ui.Box(c).Size(1, theme.Space8).Shrink(0)
		btn := bui.BtnRetry(c, "setup-retry", bui.PresetOptions{Label: s.tr.retry, MinWidth: 120, Height: theme.HeightControl})
		if btn.Clicked() {
			s.startInstall()
		}
	}
}
