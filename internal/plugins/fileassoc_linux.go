//go:build linux

package plugins

import (
	"context"
	"fmt"
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
	installedAny := false
	for _, size := range []string{"48x48", "128x128", "256x256", "512x512"} {
		src := filepath.Join(appDir, "usr", "share", "icons", "hicolor", size, "apps", "block.png")
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(dataHome, "icons", "hicolor", size, "apps", "block.png")
		if _, err := os.Stat(dst); err != nil {
			_ = writeFileAtomic(dst, data, 0o644)
		}
		installedAny = true
	}
	if !installedAny {
		src := filepath.Join(appDir, "block.png")
		if data, err := os.ReadFile(src); err == nil {
			dst := filepath.Join(dataHome, "icons", "hicolor", "256x256", "apps", "block.png")
			if _, err := os.Stat(dst); err != nil {
				_ = writeFileAtomic(dst, data, 0o644)
			}
		}
	}
	_ = os.Remove(filepath.Join(dataHome, "icons", "hicolor", "1024x1024", "apps", "block.png"))
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

// LinuxHelperDesktopID returns the deterministic per-installation Linux helper
// desktop file ID (e.g. block-assoc-<hash>.desktop) for the running binary.
func LinuxHelperDesktopID() string {
	execPath, err := resolveLinuxDesktopExec()
	if err != nil || strings.TrimSpace(execPath) == "" {
		return ""
	}
	return linuxHelperDesktopIDForExec(execPath)
}

func saveLinuxScopeStateToDir(stateDir string, prev fileAssocState, desktopID string, sc linuxAssocScopeState) (fileAssocState, error) {
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
	if strings.TrimSpace(stateDir) == "" {
		return next, nil
	}
	if err := saveFileAssocState(stateDir, next); err != nil {
		return prev, err
	}
	return next, nil
}

func deleteLinuxScopeStateFromDir(stateDir string, st fileAssocState, desktopID string) (fileAssocState, error) {
	if st.LinuxScopes != nil && desktopID != "" {
		delete(st.LinuxScopes, desktopID)
	}
	if strings.TrimSpace(stateDir) == "" {
		return st, nil
	}
	if len(st.LinuxScopes) > 0 || len(st.PinnedLinuxMainDefaults) > 0 {
		if len(st.LinuxScopes) > 0 {
			keys := make([]string, 0, len(st.LinuxScopes))
			for k := range st.LinuxScopes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			keep := st.LinuxScopes[keys[0]]
			st.ExecutablePath = keep.ExecutablePath
			st.Extensions = keep.Extensions
			st.PinnedLinuxDefaults = cloneStringMap(keep.PinnedLinuxDefaults)
		} else {
			st.ExecutablePath = ""
			st.Extensions = []string{}
			st.PinnedLinuxDefaults = nil
		}
		return st, saveFileAssocState(stateDir, st)
	}
	_ = os.Remove(fileAssocStatePath(stateDir))
	return st, nil
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
	if configHome != "" {
		unlockXDG, lockErr := acquireLinuxXDGStateLock(appDataDir, configHome, 10*time.Second)
		if lockErr != nil {
			if !os.IsPermission(lockErr) {
				return fmt.Errorf("lock XDG file association state: %w", lockErr)
			}
		} else {
			defer unlockXDG()
		}
	}
	execPath, _ := resolveLinuxDesktopExec()
	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktopName := ""
	if execPath != "" {
		helperDesktopName = linuxHelperDesktopIDForExec(execPath)
	}
	legacyHelperDesktop := filepath.Join(desktopDir, linuxLegacyHelperDesktopName)
	legacyMainDesktop := filepath.Join(desktopDir, "block.desktop")
	merged, localState, _, sharedState, _ := loadLinuxXDGMergedState(appDataDir, configHome)
	scopePrev, _ := loadLinuxScopeState(merged, helperDesktopName, execPath)

	// CRITICAL ORDERING: remove the .desktop entry and refresh the desktop
	// database FIRST. If removing the launcher or updating the database fails
	// (e.g. read-only applications directory), return immediately WITHOUT
	// dropping default-application pins from mimeapps.list so existing defaults
	// can never shift to Block.
	desktopChanged := false
	if helperDesktopName != "" {
		helperDesktop := filepath.Join(desktopDir, helperDesktopName)
		if _, err := os.Stat(helperDesktop); err == nil {
			if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("remove %s: %w", helperDesktop, rmErr)
			}
			desktopChanged = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", helperDesktop, err)
		}
	}
	if execPath != "" && linuxLegacyDesktopOwnedOrOrphaned(legacyHelperDesktop, execPath) {
		if rmErr := os.Remove(legacyHelperDesktop); rmErr == nil {
			desktopChanged = true
		} else if !os.IsNotExist(rmErr) {
			return fmt.Errorf("remove %s: %w", legacyHelperDesktop, rmErr)
		}
	}
	if raw, err := os.ReadFile(legacyMainDesktop); err == nil && strings.Contains(string(raw), linuxManagedMarker) {
		if execPath == "" || linuxLegacyDesktopOwnedOrOrphaned(legacyMainDesktop, execPath) {
			if rmErr := os.Remove(legacyMainDesktop); rmErr == nil {
				desktopChanged = true
			} else if !os.IsNotExist(rmErr) {
				return fmt.Errorf("remove %s: %w", legacyMainDesktop, rmErr)
			}
		}
	}
	if desktopChanged {
		if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil {
			return fmt.Errorf("update-desktop-database: %w", err)
		}
	}

	var firstErr error
	otherPins := collectOtherLinuxPinnedDefaults(merged, desktopDir, false, helperDesktopName)
	if len(scopePrev.PinnedLinuxDefaults) > 0 {
		prevCombined := unionStringMaps(otherPins, scopePrev.PinnedLinuxDefaults)
		if _, err := reconcileLinuxPinnedDefaults(configHome, prevCombined, otherPins); err != nil {
			firstErr = err
		}
	}
	// If the tracked user main desktop entry is no longer present under user
	// applications, also clean up Block-managed main desktop pins while
	// keeping pins still depended on by other live Block scopes.
	mainDesktopID := strings.TrimSpace(merged.LinuxMainDesktopID)
	if mainDesktopID == "" {
		mainDesktopID = "block-user.desktop"
	}
	mainDesktopDir := desktopDir
	if strings.TrimSpace(merged.LinuxMainDataHome) != "" {
		mainDesktopDir = filepath.Join(filepath.Clean(merged.LinuxMainDataHome), "applications")
	}
	if len(merged.PinnedLinuxMainDefaults) > 0 {
		_, errTracked := os.Stat(filepath.Join(mainDesktopDir, mainDesktopID))
		_, errLegacy := os.Stat(filepath.Join(mainDesktopDir, "block.desktop"))
		if os.IsNotExist(errTracked) && (mainDesktopID == "block.desktop" || os.IsNotExist(errLegacy)) {
			otherForMain := collectOtherLinuxPinnedDefaults(merged, desktopDir, true, helperDesktopName)
			prevCombined := unionStringMaps(otherForMain, merged.PinnedLinuxMainDefaults)
			if _, rmErr := reconcileLinuxPinnedDefaults(configHome, prevCombined, otherForMain); rmErr != nil && firstErr == nil {
				firstErr = rmErr
			} else {
				clearMain := func(st *fileAssocState) {
					st.PinnedLinuxMainDefaults = nil
					st.PendingPinnedLinuxMainDefaults = nil
					st.PendingLinuxMainPinTx = false
					st.LinuxMainDesktopID = ""
					st.LinuxMainDataHome = ""
				}
				clearMain(&localState)
				clearMain(&sharedState)
				_ = syncLinuxScopePinsSidecar(configHome, mainDesktopID, "", nil, nil)
			}
		}
	}
	if err := removeLegacyLinuxCustomMIME(dataHome); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr != nil {
		return firstErr
	}

	if helperDesktopName != "" {
		_ = syncLinuxScopePinsSidecar(configHome, helperDesktopName, "", nil, nil)
	}
	if xdgDir := linuxSharedXDGDir(configHome); xdgDir != "" && !sameCanonicalDir(appDataDir, xdgDir) {
		_, _ = deleteLinuxScopeStateFromDir(xdgDir, sharedState, helperDesktopName)
	}
	_, err := deleteLinuxScopeStateFromDir(appDataDir, localState, helperDesktopName)
	return err
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

	if configHome != "" {
		unlockXDG, lockErr := acquireLinuxXDGStateLock(appDataDir, configHome, 10*time.Second)
		if lockErr != nil {
			// Only tolerate XDG lock failure if configHome itself is read-only
			// (no pins can be written there anyway); otherwise propagate error.
			if !os.IsPermission(lockErr) {
				return fmt.Errorf("lock XDG file association state: %w", lockErr)
			}
		} else {
			defer unlockXDG()
		}
	}

	xdgDataDirs := os.Getenv("XDG_DATA_DIRS")
	mimeEnv := linuxMIMEEnv{
		dataDirs:       linuxXDGDataDirsFromEnv(dataHome, xdgDataDirs),
		configDirs:     linuxXDGConfigDirsFromEnv(configHome, os.Getenv("XDG_CONFIG_DIRS")),
		currentDesktop: os.Getenv("XDG_CURRENT_DESKTOP"),
	}

	// If XDG_CURRENT_DESKTOP is absent while desktop-specific *-mimeapps.list
	// files exist (e.g. gnome-mimeapps.list), defer syncing until a real
	// desktop session runs so we never pin against a mismatched generic default.
	if strings.TrimSpace(mimeEnv.currentDesktop) == "" && hasDesktopSpecificMIMEApps(mimeEnv) {
		return nil
	}

	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktopName := linuxHelperDesktopIDForExec(execPath)
	helperDesktop := filepath.Join(desktopDir, helperDesktopName)
	merged, localState, hasLocal, sharedState, hasShared := loadLinuxXDGMergedState(appDataDir, configHome)
	hasPrev := hasLocal || hasShared
	hadPendingTx := merged.PendingLinuxMainPinTx || len(merged.PendingPinnedLinuxMainDefaults) > 0
	if !hadPendingTx {
		if rawScope, ok := localState.LinuxScopes[helperDesktopName]; ok {
			hadPendingTx = rawScope.PendingPinTx || len(rawScope.PendingPinnedLinuxDefaults) > 0
		}
		if !hadPendingTx {
			if rawScope, ok := sharedState.LinuxScopes[helperDesktopName]; ok {
				hadPendingTx = rawScope.PendingPinTx || len(rawScope.PendingPinnedLinuxDefaults) > 0
			}
		}
	}

	// Prune sibling helper scopes ONLY when either:
	//   1. Their .desktop file is already gone on disk (e.g. removed by a
	//      deferred uninstaller), OR
	//   2. An explicit OS package uninstall marker exists for that scope and
	//      its executable is confirmed absent.
	// Never delete a still-present sibling .desktop entry merely because its
	// ExecutablePath returned ENOENT (e.g. an AppImage on an unmounted external
	// drive), so remounting the drive keeps the user's association intact.
	prunedSibling := false
	for otherID, otherSc := range merged.LinuxScopes {
		if otherID == helperDesktopName || strings.TrimSpace(otherSc.ExecutablePath) == "" {
			continue
		}
		otherDesktopDir := desktopDir
		if strings.TrimSpace(otherSc.DataHome) != "" {
			otherDesktopDir = filepath.Join(filepath.Clean(otherSc.DataHome), "applications")
		}
		otherDesktop := filepath.Join(otherDesktopDir, otherID)
		_, desktopStatErr := os.Stat(otherDesktop)
		desktopAlreadyGone := os.IsNotExist(desktopStatErr)
		if !desktopAlreadyGone {
			if desktopStatErr != nil || !linuxScopeExplicitlyUninstalled(otherID, otherSc.ExecutablePath) {
				continue
			}
			if rmErr := os.Remove(otherDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				continue
			}
			if dbErr := runLinuxToolIfPresent("update-desktop-database", otherDesktopDir); dbErr != nil {
				continue
			}
		}
		if len(otherSc.PinnedLinuxDefaults) > 0 {
			keepPins := collectOtherLinuxPinnedDefaults(merged, desktopDir, false, otherID)
			prevCombined := unionStringMaps(keepPins, otherSc.PinnedLinuxDefaults)
			if _, pinErr := reconcileLinuxPinnedDefaults(configHome, prevCombined, keepPins); pinErr != nil {
				continue
			}
		}
		_ = syncLinuxScopePinsSidecar(configHome, otherID, "", nil, nil)
		delete(merged.LinuxScopes, otherID)
		if localState.LinuxScopes != nil {
			delete(localState.LinuxScopes, otherID)
		}
		if sharedState.LinuxScopes != nil {
			delete(sharedState.LinuxScopes, otherID)
		}
		prunedSibling = true
	}

	scopePrev, hasScopePrev := loadLinuxScopeState(merged, helperDesktopName, execPath)
	prevHelperPins := cloneStringMap(scopePrev.PinnedLinuxDefaults)
	otherPins, pinHolders := collectOtherLinuxPinsAndHolders(merged, desktopDir, false, helperDesktopName)
	allManagedPins := unionStringMaps(otherPins, prevHelperPins)

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
		candidates, mimeEnv, helperDesktopName, 0, userMimeappsPath, allManagedPins, pinHolders,
	)

	persistScope := func(sc linuxAssocScopeState) error {
		sc.DataHome = filepath.Clean(dataHome)
		var saveErr error
		localState, saveErr = saveLinuxScopeStateToDir(appDataDir, localState, helperDesktopName, sc)
		if saveErr != nil {
			return saveErr
		}
		if xdgDir := linuxSharedXDGDir(configHome); xdgDir != "" && !sameCanonicalDir(appDataDir, xdgDir) {
			if nextShared, sharedErr := saveLinuxScopeStateToDir(xdgDir, sharedState, helperDesktopName, sc); sharedErr == nil {
				sharedState = nextShared
			} else if len(sc.PinnedLinuxDefaults) > 0 || len(sc.PendingPinnedLinuxDefaults) > 0 {
				return sharedErr
			}
		}
		if err := syncLinuxScopePinsSidecar(configHome, helperDesktopName, dataHome, sc.PinnedLinuxDefaults, sc.PendingPinnedLinuxDefaults); err != nil && (len(sc.PinnedLinuxDefaults) > 0 || len(sc.PendingPinnedLinuxDefaults) > 0) {
			return err
		}
		return nil
	}

	// Two-phase write-ahead journal: keep old committed pins alongside pending
	// target pins BEFORE modifying mimeapps.list so a crash before or after
	// commit never loses ownership of either old or new values.
	journalPins := func(committed, pending map[string]string, pendingTx bool) error {
		return persistScope(linuxAssocScopeState{
			DesktopID:                  helperDesktopName,
			DataHome:                   filepath.Clean(dataHome),
			ExecutablePath:             execPath,
			Extensions:                 scopePrev.Extensions,
			PinnedLinuxDefaults:        cloneStringMap(committed),
			PendingPinnedLinuxDefaults: cloneStringMap(pending),
			PendingPinTx:               pendingTx,
		})
	}

	// Phase 1 (add-only BEFORE mutating the .desktop file): ensure all newly
	// required pins are written while keeping every existing prevHelperPins
	// intact so an interruption before helperDesktop + desktop DB update can
	// never leave the previous .desktop entry unprotected.
	activePinned := cloneStringMap(prevHelperPins)
	desiredFinalPins := cloneStringMap(needsPin)
	var verifiedPinned []string
	if len(needsPin) > 0 {
		stage1SelfPins := unionStringMaps(prevHelperPins, needsPin)
		stage1Combined := mergeLinuxPinsPreservingOthers(otherPins, stage1SelfPins)
		if err := journalPins(prevHelperPins, stage1SelfPins, true); err != nil {
			return fmt.Errorf("persist file association journal: %w", err)
		}
		allActive, pinErr := reconcileLinuxPinnedDefaults(configHome, allManagedPins, stage1Combined)
		if pinErr != nil {
			// Cannot preserve existing defaults safely (e.g. read-only symlink
			// target); keep only directly safe MIME types and restore previous
			// pin metadata so cleanup reflects actual disk state.
			activePinned = cloneStringMap(prevHelperPins)
			_ = journalPins(activePinned, nil, false)
			desiredFinalPins = nil
		} else {
			activePinned = subsetLinuxPins(allActive, stage1SelfPins)
			var failed []string
			verifiedPinned, failed = verifyLinuxPinnedMIMEs(needsPin, mimeEnv, helperDesktopName)
			for _, mt := range failed {
				delete(desiredFinalPins, mt)
			}
		}
	}

	mimeList := unionSortedStrings(safeDirect, verifiedPinned)

	if len(mimeList) == 0 {
		if _, err := os.Stat(helperDesktop); err == nil {
			if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				return rmErr
			}
			if dbErr := runLinuxToolIfPresent("update-desktop-database", desktopDir); dbErr != nil {
				return dbErr
			}
		}
		if len(activePinned) > 0 {
			prevCombined := unionStringMaps(otherPins, activePinned)
			if err := journalPins(activePinned, nil, true); err != nil {
				return fmt.Errorf("persist file association cleanup journal: %w", err)
			}
			if _, rmErr := reconcileLinuxPinnedDefaults(configHome, prevCombined, otherPins); rmErr != nil {
				return rmErr
			}
			activePinned = nil
		}
		if hasScopePrev || len(prevHelperPins) > 0 || hadPendingTx {
			return persistScope(linuxAssocScopeState{
				DesktopID:           helperDesktopName,
				DataHome:            filepath.Clean(dataHome),
				ExecutablePath:      execPath,
				Extensions:          []string{},
				PinnedLinuxDefaults: nil,
			})
		}
		return nil
	}

	if hasPrev &&
		!hadPendingTx &&
		!prunedSibling &&
		merged.Version == fileAssocStateVersion &&
		hasScopePrev &&
		scopePrev.ExecutablePath == execPath &&
		equalStringSlices(scopePrev.Extensions, mimeList) &&
		equalStringMaps(prevHelperPins, desiredFinalPins) &&
		equalStringMaps(activePinned, desiredFinalPins) {
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
		prevCombined := unionStringMaps(otherPins, activePinned)
		restoreCombined := mergeLinuxPinsPreservingOthers(otherPins, prevHelperPins)
		_ = journalPins(activePinned, prevHelperPins, true)
		if rolled, rollErr := reconcileLinuxPinnedDefaults(configHome, prevCombined, restoreCombined); rollErr == nil {
			_ = journalPins(subsetLinuxPins(rolled, prevHelperPins), nil, false)
		} else {
			_ = journalPins(activePinned, nil, false)
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
	// Open With, rewrite/remove the .desktop entry and refresh the database
	// BEFORE dropping its pin from mimeapps.list.
	okMIMEs, badMIMEs := verifyLinuxSystemRegistration(mimeList, expectedDefaults, mimeEnv, helperDesktopName, 0)
	if len(badMIMEs) > 0 {
		mimeList = okMIMEs
		for _, mt := range badMIMEs {
			delete(desiredFinalPins, mt)
		}
		if len(mimeList) == 0 {
			if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
				rollbackFailure()
				return fmt.Errorf("remove unverified %s: %w", helperDesktopName, rmErr)
			}
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

	// Phase 3 (prune obsolete pins ONLY AFTER helperDesktop and desktop DB have
	// been updated and verified).
	if !equalStringMaps(activePinned, desiredFinalPins) {
		prevCombined := unionStringMaps(otherPins, activePinned)
		finalCombined := mergeLinuxPinsPreservingOthers(otherPins, desiredFinalPins)
		if err := journalPins(activePinned, desiredFinalPins, true); err != nil {
			return fmt.Errorf("persist file association cleanup journal: %w", err)
		}
		if rolled, rollErr := reconcileLinuxPinnedDefaults(configHome, prevCombined, finalCombined); rollErr == nil {
			activePinned = subsetLinuxPins(rolled, desiredFinalPins)
		}
	}

	if err := persistScope(linuxAssocScopeState{
		DesktopID:           helperDesktopName,
		DataHome:            filepath.Clean(dataHome),
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
