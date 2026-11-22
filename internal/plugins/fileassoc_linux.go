//go:build linux

package plugins

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	linuxLegacyHelperDesktopName = "block-plugins.desktop"
	linuxManagedMarker           = "# Managed-By=block-file-associations"
	linuxLegacyMIMEPackage       = "block-plugins.xml"
)

func linuxXDGDataHome() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Clean(xdg)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share")
	}
	return ""
}

func linuxXDGConfigHome() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Clean(xdg)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config")
	}
	return ""
}

func resolveLinuxDesktopExec() (string, error) {
	var raw string
	if appImage := strings.TrimSpace(os.Getenv("APPIMAGE")); appImage != "" {
		raw = appImage
	} else {
		exePath, err := os.Executable()
		if err != nil {
			return "", err
		}
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
			exePath = resolved
		}
		raw = exePath
	}
	if !filepath.IsAbs(raw) {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return "", err
		}
		raw = abs
	}
	if strings.ContainsAny(raw, "\n\r\x00") {
		return "", fmt.Errorf("executable path contains unsupported control characters")
	}
	return raw, nil
}

// linuxHelperDesktopIDForExec derives a stable per-installation helper desktop
// file ID so coexisting Linux installs (system .deb, user ~/.local/bin, or
// multiple AppImages) never overwrite each other's Exec= target or MimeType=.
func linuxHelperDesktopIDForExec(execPath string) string {
	return "block-assoc-" + stableInstallScopeHash(canonicalLinuxExecPath(execPath)) + ".desktop"
}

// loadActiveMainLinuxDesktopMIMEs inspects XDG application directories in
// precedence order for launchable non-managed main Block desktop entries
// (block-user.desktop and block.desktop) that target execPath, returning the
// MIME types already covered by this installation's main entry.
func loadActiveMainLinuxDesktopMIMEs(dataHome, execPath string) map[string]struct{} {
	dataDirs := linuxXDGDataDirsFromEnv(dataHome, os.Getenv("XDG_DATA_DIRS"))
	out := make(map[string]struct{})
	for _, desktopID := range []string{"block-user.desktop", "block.desktop"} {
		for _, baseDir := range dataDirs {
			p := filepath.Join(baseDir, "applications", desktopID)
			raw, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if strings.Contains(string(raw), linuxManagedMarker) {
				continue
			}
			entry := parseLinuxDesktopEntry(raw)
			if entry.Hidden {
				break
			}
			if !isLinuxDesktopEntryLaunchable(entry) {
				continue
			}
			if linuxDesktopEntryTargetsExec(entry, execPath) {
				for _, mt := range entry.MIMETypes {
					out[mt] = struct{}{}
				}
			}
			break
		}
	}
	return out
}

func ensureLinuxAppImageIcon(dataHome string) {
	appDir := strings.TrimSpace(os.Getenv("APPDIR"))
	if appDir == "" {
		return
	}
	src := filepath.Join(appDir, "block.png")
	if _, err := os.Stat(src); err != nil {
		return
	}
	dst := filepath.Join(dataHome, "icons", "hicolor", "1024x1024", "apps", "block.png")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return
	}
	_ = writeFileAtomic(dst, data, 0o644)
}

func removeLegacyLinuxCustomMIME(dataHome string) error {
	mimeDir := filepath.Join(dataHome, "mime")
	mimePkgPath := filepath.Join(mimeDir, "packages", linuxLegacyMIMEPackage)
	if _, err := os.Stat(mimePkgPath); err != nil {
		return nil
	}
	if err := os.Remove(mimePkgPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if st, err := os.Stat(filepath.Join(mimeDir, "packages")); err == nil && st.IsDir() {
		return runLinuxToolIfPresent("update-mime-database", mimeDir)
	}
	return nil
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func buildLinuxHelperDesktopContent(execPath string, mimeList []string) string {
	return strings.Join([]string{
		linuxManagedMarker,
		"[Desktop Entry]",
		"Type=Application",
		"Name=Block",
		"Exec=" + buildLinuxDesktopExecLine(execPath),
		"Icon=block",
		"Categories=Office;TextEditor;Utility;",
		"MimeType=" + strings.Join(mimeList, ";") + ";",
		"NoDisplay=true",
		"Terminal=false",
		"StartupWMClass=block",
		"",
	}, "\n")
}

func loadLinuxScopeState(st fileAssocState, desktopID, execPath string) (linuxAssocScopeState, bool) {
	if st.LinuxScopes != nil {
		if sc, ok := st.LinuxScopes[desktopID]; ok {
			return sc, true
		}
	}
	// Migrate legacy top-level Linux state only if it belonged to this executable.
	if st.ExecutablePath != "" && canonicalLinuxExecPath(st.ExecutablePath) == canonicalLinuxExecPath(execPath) {
		return linuxAssocScopeState{
			DesktopID:           desktopID,
			ExecutablePath:      st.ExecutablePath,
			Extensions:          st.Extensions,
			PinnedLinuxDefaults: cloneStringMap(st.PinnedLinuxDefaults),
		}, true
	}
	return linuxAssocScopeState{}, false
}

func saveLinuxScopeState(appDataDir string, prev fileAssocState, desktopID string, sc linuxAssocScopeState) error {
	next := prev
	if next.LinuxScopes == nil {
		next.LinuxScopes = make(map[string]linuxAssocScopeState)
		if prev.ExecutablePath != "" && canonicalLinuxExecPath(prev.ExecutablePath) != canonicalLinuxExecPath(sc.ExecutablePath) &&
			(len(prev.Extensions) > 0 || len(prev.PinnedLinuxDefaults) > 0) {
			oldID := linuxHelperDesktopIDForExec(prev.ExecutablePath)
			if oldID != desktopID {
				next.LinuxScopes[oldID] = linuxAssocScopeState{
					DesktopID:           oldID,
					ExecutablePath:      prev.ExecutablePath,
					Extensions:          prev.Extensions,
					PinnedLinuxDefaults: cloneStringMap(prev.PinnedLinuxDefaults),
				}
			}
		}
	}
	sc.DesktopID = desktopID
	next.LinuxScopes[desktopID] = sc
	next.ExecutablePath = sc.ExecutablePath
	next.Extensions = sc.Extensions
	next.PinnedLinuxDefaults = cloneStringMap(sc.PinnedLinuxDefaults)
	return saveFileAssocState(appDataDir, next)
}

func linuxLegacyDesktopOwnedOrOrphaned(desktopPath, execPath string) bool {
	raw, err := os.ReadFile(desktopPath)
	if err != nil {
		return false
	}
	entry := parseLinuxDesktopEntry(raw)
	if linuxDesktopEntryTargetsExec(entry, execPath) {
		return true
	}
	return !isLinuxDesktopEntryLaunchable(entry)
}

func cleanupPlatformFileAssociations(appDataDir string, _ bool) error {
	dataHome := linuxXDGDataHome()
	if dataHome == "" {
		return nil
	}
	configHome := linuxXDGConfigHome()
	execPath, _ := resolveLinuxDesktopExec()
	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktopName := ""
	if execPath != "" {
		helperDesktopName = linuxHelperDesktopIDForExec(execPath)
	}
	legacyHelperDesktop := filepath.Join(desktopDir, linuxLegacyHelperDesktopName)
	legacyMainDesktop := filepath.Join(desktopDir, "block.desktop")
	prev, _ := loadFileAssocState(appDataDir)
	scopePrev, _ := loadLinuxScopeState(prev, helperDesktopName, execPath)

	var firstErr error
	if len(scopePrev.PinnedLinuxDefaults) > 0 {
		if _, err := reconcileLinuxPinnedDefaults(configHome, scopePrev.PinnedLinuxDefaults, nil); err != nil {
			firstErr = err
		}
	}
	// If the tracked user main desktop entry is no longer present under user
	// applications, also clean up Block-managed main desktop pins.
	mainDesktopID := strings.TrimSpace(prev.LinuxMainDesktopID)
	if mainDesktopID == "" {
		mainDesktopID = "block-user.desktop"
	}
	if len(prev.PinnedLinuxMainDefaults) > 0 {
		_, errTracked := os.Stat(filepath.Join(desktopDir, mainDesktopID))
		_, errLegacy := os.Stat(legacyMainDesktop)
		if os.IsNotExist(errTracked) && (mainDesktopID == "block.desktop" || os.IsNotExist(errLegacy)) {
			if _, rmErr := reconcileLinuxPinnedDefaults(configHome, prev.PinnedLinuxMainDefaults, nil); rmErr != nil && firstErr == nil {
				firstErr = rmErr
			} else {
				prev.PinnedLinuxMainDefaults = nil
				prev.LinuxMainDesktopID = ""
			}
		}
	}
	if err := removeLegacyLinuxCustomMIME(dataHome); err != nil && firstErr == nil {
		firstErr = err
	}
	desktopChanged := false
	if helperDesktopName != "" {
		helperDesktop := filepath.Join(desktopDir, helperDesktopName)
		if _, err := os.Stat(helperDesktop); err == nil {
			if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				if firstErr == nil {
					firstErr = rmErr
				}
			} else {
				desktopChanged = true
			}
		}
	}
	if execPath != "" && linuxLegacyDesktopOwnedOrOrphaned(legacyHelperDesktop, execPath) {
		if rmErr := os.Remove(legacyHelperDesktop); rmErr == nil {
			desktopChanged = true
		} else if !os.IsNotExist(rmErr) && firstErr == nil {
			firstErr = rmErr
		}
	}
	if raw, err := os.ReadFile(legacyMainDesktop); err == nil && strings.Contains(string(raw), linuxManagedMarker) {
		if execPath == "" || linuxLegacyDesktopOwnedOrOrphaned(legacyMainDesktop, execPath) {
			_ = os.Remove(legacyMainDesktop)
			desktopChanged = true
		}
	}
	if desktopChanged {
		if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if prev.LinuxScopes != nil && helperDesktopName != "" {
		delete(prev.LinuxScopes, helperDesktopName)
	}
	if len(prev.LinuxScopes) > 0 || len(prev.PinnedLinuxMainDefaults) > 0 {
		if len(prev.LinuxScopes) > 0 {
			keys := make([]string, 0, len(prev.LinuxScopes))
			for k := range prev.LinuxScopes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			keep := prev.LinuxScopes[keys[0]]
			prev.ExecutablePath = keep.ExecutablePath
			prev.Extensions = keep.Extensions
			prev.PinnedLinuxDefaults = cloneStringMap(keep.PinnedLinuxDefaults)
		} else {
			prev.ExecutablePath = ""
			prev.Extensions = []string{}
			prev.PinnedLinuxDefaults = nil
		}
		return saveFileAssocState(appDataDir, prev)
	}
	_ = os.Remove(fileAssocStatePath(appDataDir))
	return nil
}

// syncPlatformFileAssociations manages a dedicated per-installation
// NoDisplay=true helper entry (~/.local/share/applications/block-assoc-<hash>.desktop)
// for safe text/code MIME types not already covered by a launchable main
// desktop entry for this executable. For MIME types whose implicit default
// (including inherited defaults such as text/plain -> text/x-go or another
// Block installation's desktop ID) would otherwise be displaced, it preserves
// the existing effective default in $XDG_CONFIG_HOME/mimeapps.list (with
// write-ahead ownership journaling and rollback on failure) and verifies both
// default preservation and Open-With availability against the host OS resolver.
func syncPlatformFileAssociations(appDataDir string, managedExts []string) error {
	dataHome := linuxXDGDataHome()
	if dataHome == "" {
		return nil
	}
	configHome := linuxXDGConfigHome()
	execPath, err := resolveLinuxDesktopExec()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktopName := linuxHelperDesktopIDForExec(execPath)
	helperDesktop := filepath.Join(desktopDir, helperDesktopName)
	prev, hasPrev := loadFileAssocState(appDataDir)
	scopePrev, hasScopePrev := loadLinuxScopeState(prev, helperDesktopName, execPath)
	prevHelperPins := cloneStringMap(scopePrev.PinnedLinuxDefaults)

	xdgDataDirs := os.Getenv("XDG_DATA_DIRS")
	mimeEnv := linuxMIMEEnv{
		dataDirs:       linuxXDGDataDirsFromEnv(dataHome, xdgDataDirs),
		configDirs:     linuxXDGConfigDirsFromEnv(configHome, os.Getenv("XDG_CONFIG_DIRS")),
		currentDesktop: os.Getenv("XDG_CURRENT_DESKTOP"),
	}

	// Migrate away from any legacy managed block.desktop override, legacy
	// shared block-plugins.desktop (only when owned by this execPath and not
	// referenced as a user default), or custom MIME xml.
	legacyMainDesktop := filepath.Join(desktopDir, "block.desktop")
	if raw, err := os.ReadFile(legacyMainDesktop); err == nil && strings.Contains(string(raw), linuxManagedMarker) {
		if linuxLegacyDesktopOwnedOrOrphaned(legacyMainDesktop, execPath) && !linuxDesktopIDReferencedAsDefault("block.desktop", mimeEnv) {
			_ = os.Remove(legacyMainDesktop)
		}
	}
	legacyHelperDesktop := filepath.Join(desktopDir, linuxLegacyHelperDesktopName)
	if linuxLegacyDesktopOwnedOrOrphaned(legacyHelperDesktop, execPath) && !linuxDesktopIDReferencedAsDefault(linuxLegacyHelperDesktopName, mimeEnv) {
		_ = os.Remove(legacyHelperDesktop)
	}
	_ = removeLegacyLinuxCustomMIME(dataHome)

	resolvedMIMEs := ResolveLinuxMIMETypesForExtensions(managedExts, dataHome, xdgDataDirs)
	mainMIMEs := loadActiveMainLinuxDesktopMIMEs(dataHome, execPath)
	candidates := make([]string, 0, len(resolvedMIMEs))
	for _, mt := range resolvedMIMEs {
		if _, covered := mainMIMEs[mt]; covered {
			continue
		}
		candidates = append(candidates, mt)
	}

	userMimeappsPath := ""
	if configHome != "" {
		userMimeappsPath = filepath.Join(configHome, "mimeapps.list")
	}

	safeDirect, needsPin, expectedDefaults := planLinuxDesktopMIMEs(
		candidates, mimeEnv, helperDesktopName, 0, userMimeappsPath, prevHelperPins,
	)

	// Persist intended pin ownership BEFORE modifying mimeapps.list so a crash
	// or unwritable state file never leaves unowned pins behind.
	journalPins := func(pins map[string]string) error {
		return saveLinuxScopeState(appDataDir, prev, helperDesktopName, linuxAssocScopeState{
			DesktopID:           helperDesktopName,
			ExecutablePath:      execPath,
			Extensions:          scopePrev.Extensions,
			PinnedLinuxDefaults: cloneStringMap(pins),
		})
	}

	activePinned := cloneStringMap(prevHelperPins)
	var verifiedPinned []string
	if len(needsPin) > 0 || len(prevHelperPins) > 0 {
		if err := journalPins(unionStringMaps(prevHelperPins, needsPin)); err != nil {
			return fmt.Errorf("persist file association journal: %w", err)
		}
		pinned, pinErr := reconcileLinuxPinnedDefaults(configHome, prevHelperPins, needsPin)
		if pinErr != nil {
			// Cannot preserve existing defaults safely; keep only directly safe MIME types
			// and restore previous pin metadata so cleanup reflects actual disk state.
			activePinned = cloneStringMap(prevHelperPins)
			_ = journalPins(activePinned)
		} else {
			activePinned = pinned
			if len(needsPin) > 0 {
				var failed []string
				verifiedPinned, failed = verifyLinuxPinnedMIMEs(needsPin, mimeEnv, helperDesktopName)
				if len(failed) > 0 {
					for _, mt := range failed {
						delete(needsPin, mt)
					}
					if rolled, rollErr := reconcileLinuxPinnedDefaults(configHome, activePinned, needsPin); rollErr == nil {
						activePinned = rolled
					}
				}
			}
		}
	}

	mimeList := unionSortedStrings(safeDirect, verifiedPinned)

	if len(mimeList) == 0 {
		if _, err := os.Stat(helperDesktop); err == nil {
			if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				return rmErr
			}
			_ = runLinuxToolIfPresent("update-desktop-database", desktopDir)
		}
		if len(activePinned) > 0 {
			if _, rmErr := reconcileLinuxPinnedDefaults(configHome, activePinned, nil); rmErr != nil {
				return rmErr
			}
			activePinned = nil
		}
		if hasScopePrev || len(prevHelperPins) > 0 {
			return saveLinuxScopeState(appDataDir, prev, helperDesktopName, linuxAssocScopeState{
				DesktopID:           helperDesktopName,
				ExecutablePath:      execPath,
				Extensions:          []string{},
				PinnedLinuxDefaults: nil,
			})
		}
		return nil
	}

	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		hasScopePrev &&
		scopePrev.ExecutablePath == execPath &&
		equalStringSlices(scopePrev.Extensions, mimeList) &&
		equalStringMaps(prevHelperPins, activePinned) {
		if _, err := os.Stat(helperDesktop); err == nil {
			return nil
		}
	}

	ensureLinuxAppImageIcon(dataHome)

	prevHelperBytes, hadPrevHelper := readFileIfExists(helperDesktop)
	rollbackFailure := func() {
		if hadPrevHelper {
			_ = writeFileAtomic(helperDesktop, prevHelperBytes, 0o644)
		} else {
			_ = os.Remove(helperDesktop)
		}
		_ = runLinuxToolIfPresent("update-desktop-database", desktopDir)
		if rolled, rollErr := reconcileLinuxPinnedDefaults(configHome, activePinned, prevHelperPins); rollErr == nil {
			_ = journalPins(rolled)
		} else {
			_ = journalPins(activePinned)
		}
	}

	desktopContent := buildLinuxHelperDesktopContent(execPath, mimeList)
	if err := writeFileAtomic(helperDesktop, []byte(desktopContent), 0o644); err != nil {
		rollbackFailure()
		return fmt.Errorf("write %s: %w", helperDesktopName, err)
	}
	if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil {
		rollbackFailure()
		return fmt.Errorf("update-desktop-database: %w", err)
	}

	// Post-write verification against the host OS resolver (gio / xdg-mime).
	// If any MIME type shifted its real system default or failed to appear in
	// Open With, drop it immediately and roll back its pin.
	okMIMEs, badMIMEs := verifyLinuxSystemRegistration(mimeList, expectedDefaults, mimeEnv, helperDesktopName, 0)
	if len(badMIMEs) > 0 {
		mimeList = okMIMEs
		badSet := make(map[string]struct{}, len(badMIMEs))
		for _, mt := range badMIMEs {
			badSet[mt] = struct{}{}
		}
		trimmedPins := make(map[string]string, len(activePinned))
		for mt, app := range activePinned {
			if _, isBad := badSet[mt]; !isBad {
				trimmedPins[mt] = app
			}
		}
		if rolled, rollErr := reconcileLinuxPinnedDefaults(configHome, activePinned, trimmedPins); rollErr == nil {
			activePinned = rolled
		}
		if len(mimeList) == 0 {
			_ = os.Remove(helperDesktop)
		} else {
			if err := writeFileAtomic(helperDesktop, []byte(buildLinuxHelperDesktopContent(execPath, mimeList)), 0o644); err != nil {
				rollbackFailure()
				return fmt.Errorf("rewrite verified %s: %w", helperDesktopName, err)
			}
		}
		if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil {
			rollbackFailure()
			return fmt.Errorf("update-desktop-database: %w", err)
		}
	}

	if err := saveLinuxScopeState(appDataDir, prev, helperDesktopName, linuxAssocScopeState{
		DesktopID:           helperDesktopName,
		ExecutablePath:      execPath,
		Extensions:          mimeList,
		PinnedLinuxDefaults: activePinned,
	}); err != nil {
		rollbackFailure()
		return fmt.Errorf("persist file association state: %w", err)
	}
	return nil
}

func runLinuxToolIfPresent(name string, args ...string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %w (%s)", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
