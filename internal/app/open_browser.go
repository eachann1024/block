package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	blockui "block/internal/ui"
	"github.com/egoist/mygo"
)

// browserOpenFlight keeps a double-click from launching two browsers.
var browserOpenFlight atomic.Bool

// openFileInDefaultBrowser opens a local file in the default web browser.
// Shell.OpenPath is the wrong tool here: it asks Launch Services / the file
// association for the file type, and Block itself is often that association
// for HTML, so the click only reactivates Block.
func openFileInDefaultBrowser(path string) error {
	if blockui.IsProbeEnabled() {
		return nil
	}
	if !browserOpenFlight.CompareAndSwap(false, true) {
		return nil
	}
	defer browserOpenFlight.Store(false)

	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", abs)
	}

	var errs []error
	if mygo.App.IsReady() {
		app, err := mygo.Shell.DefaultApp("https://example.com")
		switch {
		case err != nil:
			errs = append(errs, err)
		case browserIsSelf(app):
			errs = append(errs, errors.New("default https handler is Block"))
		default:
			if err := launchInstalledBrowser(app, abs); err != nil {
				errs = append(errs, err)
			} else {
				return nil
			}
		}
	}
	if err := launchFallbackBrowsers(abs); err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	return nil
}

func launchInstalledBrowser(app mygo.InstalledApp, file string) error {
	switch runtime.GOOS {
	case "darwin":
		return launchDarwinBrowser(app.ID, app.Path, file)
	case "linux":
		return launchLinuxBrowser(app, file)
	case "windows":
		return launchWindowsBrowser(app, file)
	default:
		return fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
}

func launchFallbackBrowsers(file string) error {
	var errs []error
	switch runtime.GOOS {
	case "darwin":
		for _, b := range darwinFallbackBrowsers {
			if err := launchDarwinBrowser(b.id, b.path, file); err != nil {
				errs = append(errs, err)
				continue
			}
			return nil
		}
	case "linux":
		seen := map[string]bool{}
		try := func(desktop string) bool {
			desktop = strings.TrimSpace(desktop)
			key := strings.ToLower(desktop)
			if desktop == "" || seen[key] || strings.Contains(key, "block") {
				return false
			}
			seen[key] = true
			if err := launchLinuxDesktop(desktop, file); err != nil {
				errs = append(errs, err)
				return false
			}
			return true
		}
		if try(linuxDefaultWebBrowser()) {
			return nil
		}
		for _, desktop := range linuxFallbackDesktops {
			if try(desktop) {
				return nil
			}
		}
	case "windows":
		for _, exe := range windowsBrowserCandidates() {
			if !usableWindowsBrowserExe(exe) {
				continue
			}
			if err := shellExecute(exe, quoteWindowsArg(file)); err != nil {
				errs = append(errs, err)
				continue
			}
			return nil
		}
		if err := shellExecute("microsoft-edge:"+LocalFileURL(file), ""); err != nil {
			errs = append(errs, err)
		} else {
			return nil
		}
	default:
		return fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	if len(errs) == 0 {
		return errors.New("no browser found")
	}
	return errors.Join(errs...)
}

type darwinBrowser struct {
	id   string
	path string
}

// Safari is first so a failed default-browser launch still lands in a browser
// that ships with the OS, instead of walking every installed browser.
var darwinFallbackBrowsers = []darwinBrowser{
	{"com.apple.Safari", "/Applications/Safari.app"},
	{"com.google.Chrome", "/Applications/Google Chrome.app"},
	{"com.microsoft.edgemac", "/Applications/Microsoft Edge.app"},
	{"org.mozilla.firefox", "/Applications/Firefox.app"},
	{"com.brave.Browser", "/Applications/Brave Browser.app"},
	{"company.thebrowser.Browser", "/Applications/Arc.app"},
	{"com.operasoftware.Opera", "/Applications/Opera.app"},
	{"com.vivaldi.Vivaldi", "/Applications/Vivaldi.app"},
	{"org.chromium.Chromium", "/Applications/Chromium.app"},
}

var linuxFallbackDesktops = []string{
	"firefox.desktop",
	"google-chrome.desktop",
	"google-chrome-stable.desktop",
	"chromium.desktop",
	"chromium-browser.desktop",
	"microsoft-edge.desktop",
	"brave-browser.desktop",
	"opera.desktop",
	"vivaldi-stable.desktop",
	"org.gnome.Epiphany.desktop",
}

func launchDarwinBrowser(id, appPath, file string) error {
	var errs []error
	if appPath != "" {
		if st, err := os.Stat(appPath); err == nil && st.IsDir() {
			if err := runLauncher(exec.Command("open", "-a", appPath, file)); err == nil {
				return nil
			} else {
				errs = append(errs, err)
			}
		}
	}
	if bundleID := strings.TrimSpace(id); bundleID != "" && !strings.ContainsAny(bundleID, `/\`) && !strings.HasSuffix(bundleID, ".app") {
		if err := runLauncher(exec.Command("open", "-b", bundleID, file)); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return fmt.Errorf("browser not installed: %s", id)
	}
	return errors.Join(errs...)
}

func launchLinuxBrowser(app mygo.InstalledApp, file string) error {
	desktop := strings.TrimSpace(app.Path)
	if !strings.HasSuffix(strings.ToLower(desktop), ".desktop") {
		desktop = strings.TrimSpace(app.ID)
	}
	if strings.HasSuffix(strings.ToLower(desktop), ".desktop") {
		return launchLinuxDesktop(desktop, file)
	}
	if app.Path != "" {
		if st, err := os.Stat(app.Path); err == nil && st.Mode().IsRegular() {
			return startBrowserProcess(app.Path, file)
		}
	}
	return fmt.Errorf("no desktop entry for %s", app.Name)
}

func launchLinuxDesktop(desktop, file string) error {
	var errs []error
	if bin, err := exec.LookPath("gio"); err == nil {
		if err := runLauncher(exec.Command(bin, "launch", desktop, file)); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	id := desktop
	if filepath.IsAbs(desktop) {
		id = filepath.Base(desktop)
	}
	if bin, err := exec.LookPath("gtk-launch"); err == nil {
		if err := runLauncher(exec.Command(bin, id, LocalFileURL(file))); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	for _, name := range []string{"kioclient5", "kioclient"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := runLauncher(exec.Command(bin, "exec", desktop, file)); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return fmt.Errorf("cannot launch %s", desktop)
	}
	return errors.Join(errs...)
}

func linuxDefaultWebBrowser() string {
	bin, err := exec.LookPath("xdg-settings")
	if err != nil {
		return ""
	}
	out, err := exec.Command(bin, "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(out))
	if !strings.HasSuffix(strings.ToLower(id), ".desktop") {
		return ""
	}
	return id
}

func launchWindowsBrowser(app mygo.InstalledApp, file string) error {
	if isShellAppsFolder(app.Path) || isShellAppsFolder(app.ID) {
		return launchWindowsStoreBrowser(app, file)
	}
	if !usableWindowsBrowserExe(app.Path) {
		return fmt.Errorf("not a browser executable: %s", app.Path)
	}
	return shellExecute(app.Path, quoteWindowsArg(file))
}

func launchWindowsStoreBrowser(app mygo.InstalledApp, file string) error {
	blob := strings.ToLower(app.ID + " " + app.Name + " " + app.Path)
	if strings.Contains(blob, "edge") {
		return shellExecute("microsoft-edge:"+LocalFileURL(file), "")
	}
	return fmt.Errorf("cannot pass a file to packaged app %s", app.ID)
}

func windowsBrowserCandidates() []string {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	}
	rels := []string{
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join("Mozilla Firefox", "firefox.exe"),
		filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}
	var out []string
	seen := map[string]bool{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		for _, rel := range rels {
			p := filepath.Join(root, rel)
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

func usableWindowsBrowserExe(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || isShellAppsFolder(path) {
		return false
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "openwith.exe", "launchwinapp.exe", "explorer.exe", "rundll32.exe",
		"pickerhost.exe", "applicationframehost.exe", "systemsettings.exe",
		"cmd.exe", "powershell.exe", "pwsh.exe":
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func quoteWindowsArg(s string) string {
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func isShellAppsFolder(path string) bool {
	low := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(low, `shell:appsfolder\`) || strings.HasPrefix(low, "shell:appsfolder/")
}

func runLauncher(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg != "" {
		return fmt.Errorf("%s: %s", strings.Join(cmd.Args, " "), msg)
	}
	return fmt.Errorf("%s: %w", strings.Join(cmd.Args, " "), err)
}

func startBrowserProcess(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func browserIsSelf(app mygo.InstalledApp) bool {
	if appIDIsBlock(app.ID) || appIDIsBlock(app.Path) {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	exe = filepath.Clean(exe)
	bundle := macAppBundle(exe)
	for _, candidate := range []string{app.Path, app.ID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || isShellAppsFolder(candidate) {
			continue
		}
		if strings.HasSuffix(strings.ToLower(candidate), ".desktop") {
			if desktopPointsAt(candidate, exe) {
				return true
			}
			continue
		}
		resolved := candidate
		if real, err := filepath.EvalSymlinks(candidate); err == nil && real != "" {
			resolved = real
		}
		resolved = filepath.Clean(resolved)
		if strings.EqualFold(resolved, exe) {
			return true
		}
		if bundle != "" && strings.EqualFold(resolved, bundle) {
			return true
		}
	}
	return false
}

func appIDIsBlock(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return strings.Contains(id, "dev.eachann.block")
}

func macAppBundle(exe string) string {
	slash := filepath.ToSlash(filepath.Clean(exe))
	low := strings.ToLower(slash)
	i := strings.Index(low, ".app/")
	if i < 0 {
		if strings.HasSuffix(low, ".app") {
			return filepath.Clean(exe)
		}
		return ""
	}
	return filepath.Clean(filepath.FromSlash(slash[:i+len(".app")]))
}

func desktopPointsAt(desktop, exe string) bool {
	raw, err := os.ReadFile(desktop)
	if err != nil {
		return false
	}
	base := filepath.Base(exe)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		rest = strings.Trim(rest, `"`)
		bin := rest
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			bin = strings.Trim(rest[:i], `"`)
		}
		if bin == "" {
			return false
		}
		if strings.EqualFold(filepath.Clean(bin), exe) {
			return true
		}
		return filepath.Base(bin) == base && strings.Contains(strings.ToLower(bin), "block")
	}
	return false
}
