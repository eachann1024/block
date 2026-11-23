//go:build windows

package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	winBlockMachineProgID          = "Block.Machine.Document"
	winBlockMachineProgIDPrefix    = "Block.Machine."
	winBlockMachineUserProgID      = "Block.Machine.User.Document"
	winBlockMachineUserProgIDPref  = "Block.Machine.User."
	winBlockUserProgID             = "Block.User.Document"
	winBlockLegacyProgID           = "Block.Document"
	winBlockLegacyPortableProgID   = "Block.Portable.Document"
	winBlockPortableProgIDPrefix   = "Block.Portable."
	winSHCNEAssocChanged           = 0x08000000
	winSHCNFIdList                 = 0x0000
)

func LinuxHelperDesktopID() string {
	return ""
}

var (
	winShell32DLL     = syscall.NewLazyDLL("shell32.dll")
	winSHChangeNotif  = winShell32DLL.NewProc("SHChangeNotify")
	winReconciledOnce atomic.Bool
)

func normalizeWinPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != "" {
		p = resolved
	}
	return strings.ToLower(filepath.Clean(p))
}

// winMachineProgIDForExe searches HKLM\Software\Classes\Block.Machine.* and
// HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\Block* to find the
// exact machine-wide ProgID whose open command or InstallLocation matches
// exePath, supporting multiple side-by-side NSIS installs (e.g. C:\Block-A and
// D:\Block-B) as well as legacy shared Block.Machine.Document.
func winMachineProgIDForExe(exePath string) string {
	normExe := normalizeWinPath(exePath)
	if normExe == "" {
		return ""
	}
	if clsKey, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Classes`, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := clsKey.ReadSubKeyNames(-1)
		clsKey.Close()
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasPrefix(name, winBlockMachineProgIDPrefix) {
				continue
			}
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Classes\`+name+`\shell\open\command`, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			cmd, _, err := k.GetStringValue("")
			k.Close()
			if err == nil && normalizeWinPath(extractWinCommandExePath(cmd)) == normExe {
				return name
			}
		}
	}
	if uninstKey, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := uninstKey.ReadSubKeyNames(-1)
		uninstKey.Close()
		sort.Strings(names)
		for _, name := range names {
			if name != "Block" && !strings.HasPrefix(name, "Block-") {
				continue
			}
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			loc, _, locErr := k.GetStringValue("InstallLocation")
			progID, _, _ := k.GetStringValue("BlockMachineProgID")
			k.Close()
			if locErr == nil && strings.TrimSpace(loc) != "" {
				candExe := filepath.Join(strings.TrimSpace(loc), "block.exe")
				if normalizeWinPath(candExe) == normExe {
					if strings.TrimSpace(progID) != "" {
						return strings.TrimSpace(progID)
					}
					return winBlockMachineProgID
				}
			}
		}
	}
	return ""
}

func winMachineInstallExePath() string {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall\Block`, registry.QUERY_VALUE); err == nil {
		loc, _, err := k.GetStringValue("InstallLocation")
		k.Close()
		if err == nil && strings.TrimSpace(loc) != "" {
			return filepath.Join(strings.TrimSpace(loc), "block.exe")
		}
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Classes\`+winBlockMachineProgID+`\shell\open\command`, registry.QUERY_VALUE); err == nil {
		cmd, _, err := k.GetStringValue("")
		k.Close()
		if err == nil {
			if exe := extractWinCommandExePath(cmd); exe != "" {
				return exe
			}
		}
	}
	return ""
}

func winUserInstallExePath() string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
	}
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "Programs", "Block", "block.exe")
}

func extractWinCommandExePath(cmd string) string {
	s := strings.TrimSpace(cmd)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end > 0 {
			return s[1 : 1+end]
		}
	}
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// resolveWinProgID assigns a dedicated HKCU runtime ProgID per installation
// scope so coexisting installs never overwrite each other's commands or state:
//   - Per-machine NSIS install -> Block.Machine.User.<12-hex-hash> (HKCU plugin
//     deltas scoped per install directory, paired with its HKLM machineProgID)
//   - Per-user install (%LOCALAPPDATA%\Programs\Block) -> Block.User.Document
//   - Portable / ad-hoc binary -> Block.Portable.<12-hex-hash>
// Legacy Block.Document, Block.Machine.User.Document, and
// Block.Portable.Document are never reused directly by new installs so an older
// install's default association is never hijacked.
func resolveWinProgID(exePath string) (progID string, machineProgID string) {
	normExe := normalizeWinPath(exePath)
	if mProgID := winMachineProgIDForExe(exePath); mProgID != "" {
		return winBlockMachineUserProgIDPref + stableInstallScopeHash(normExe), mProgID
	}
	if userExe := winUserInstallExePath(); userExe != "" && normalizeWinPath(userExe) == normExe {
		return winBlockUserProgID, ""
	}
	return winBlockPortableProgIDPrefix + stableInstallScopeHash(normExe), ""
}

func winMachineProgIDMatchesFor(machineProgID, exePath string) bool {
	if strings.TrimSpace(machineProgID) == "" {
		return false
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Classes\`+machineProgID+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue("")
	if err != nil {
		return false
	}
	return normalizeWinPath(extractWinCommandExePath(val)) == normalizeWinPath(exePath)
}

func winMachineHasOpenWithProgidFor(ext, machineProgID string) bool {
	if strings.TrimSpace(machineProgID) == "" {
		return false
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Classes\`+ext+`\OpenWithProgids`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetValue(machineProgID, nil)
	return err == nil
}

func winProgIDCommandExe(progID string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+progID+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return extractWinCommandExePath(cmd)
}

func winIsActiveStableInstallExe(exePath string) bool {
	norm := normalizeWinPath(exePath)
	if norm == "" {
		return false
	}
	if _, err := os.Stat(exePath); err != nil {
		return false
	}
	if machineExe := winMachineInstallExePath(); machineExe != "" && normalizeWinPath(machineExe) == norm {
		return true
	}
	if userExe := winUserInstallExePath(); userExe != "" && normalizeWinPath(userExe) == norm {
		return true
	}
	return false
}

// winUserDefaultExtensionsForProgID returns every file extension currently
// configured by the user to open by default with progID (via Explorer
// UserChoice, Windows 11 24H2+ UserChoiceLatest\ProgId, or legacy
// HKCU\Software\Classes\<ext> default value).
func winUserDefaultExtensionsForProgID(progID string) map[string]struct{} {
	target := strings.ToLower(strings.TrimSpace(progID))
	if target == "" {
		return nil
	}
	refs := make(map[string]struct{})
	checkValue := func(subkey, valueName, ext string) {
		k, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		val, _, err := k.GetStringValue(valueName)
		if err == nil && strings.ToLower(strings.TrimSpace(val)) == target {
			refs[strings.ToLower(ext)] = struct{}{}
		}
	}

	if feKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts`, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := feKey.ReadSubKeyNames(-1)
		feKey.Close()
		for _, ext := range names {
			if !strings.HasPrefix(ext, ".") {
				continue
			}
			base := `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\` + ext
			checkValue(base+`\UserChoice`, "ProgId", ext)
			checkValue(base+`\UserChoiceLatest\ProgId`, "ProgId", ext)
		}
	}

	if clsKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := clsKey.ReadSubKeyNames(-1)
		clsKey.Close()
		for _, ext := range names {
			if !strings.HasPrefix(ext, ".") {
				continue
			}
			checkValue(`Software\Classes\`+ext, "", ext)
		}
	}

	if len(refs) == 0 {
		return nil
	}
	return refs
}

// winRegisteredExtensionsForProgID scans HKCU\Software\Classes\.* keys to find
// every extension whose OpenWithProgids currently contains progID, so cleanup
// and stale-entry pruning remain exact even if file-associations.json was lost
// or reset by another installation.
func winRegisteredExtensionsForProgID(progID string) []string {
	if strings.TrimSpace(progID) == "" {
		return nil
	}
	clsKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	names, _ := clsKey.ReadSubKeyNames(-1)
	clsKey.Close()
	var out []string
	for _, name := range names {
		if !strings.HasPrefix(name, ".") || !isSingleSegmentExtension(strings.ToLower(name)) {
			continue
		}
		if winHasOpenWithProgid(name, progID) {
			out = append(out, strings.ToLower(name))
		}
	}
	sort.Strings(out)
	return out
}

// winProgIDOwnedByCurrentScope returns true if progID is empty/absent, its
// registered command belongs to this executable, or its previously registered
// executable no longer exists on disk. A live executable at a different path
// is never overwritten.
func winProgIDOwnedByCurrentScope(progID, currentExe, _ string) bool {
	cmdExe := winProgIDCommandExe(progID)
	if cmdExe == "" {
		return true
	}
	if normalizeWinPath(cmdExe) == normalizeWinPath(currentExe) {
		return true
	}
	if _, statErr := os.Stat(cmdExe); errors.Is(statErr, os.ErrNotExist) {
		return true
	}
	return false
}

// winLegacyProgIDOwnedByMigration verifies that an old HKCU ProgID actually
// belonged to this executable (or to a deleted executable previously tracked by
// this scope) rather than a separate live installation on disk.
func winLegacyProgIDOwnedByMigration(oldProgID, currentExe, prevStateExe string) bool {
	if oldProgID == "" {
		return false
	}
	cmdExe := winProgIDCommandExe(oldProgID)
	if cmdExe == "" {
		return prevStateExe != "" && normalizeWinPath(prevStateExe) == normalizeWinPath(currentExe)
	}
	if normalizeWinPath(cmdExe) == normalizeWinPath(currentExe) {
		return true
	}
	if _, statErr := os.Stat(cmdExe); errors.Is(statErr, os.ErrNotExist) {
		return prevStateExe != "" && normalizeWinPath(cmdExe) == normalizeWinPath(prevStateExe)
	}
	return false
}

// migrateWinLegacyProgID removes OpenWithProgids entries and deletes oldProgID
// unless the user still has one or more file extensions defaulted to oldProgID.
// When referenced as a user default, oldProgID and its referenced extensions'
// entries are retained and kept pointing to forwardExe so double-click opening
// continues working until the user changes their default away.
func migrateWinLegacyProgID(oldProgID string, candidateExts []string, forwardExe string) (remaining []string, retainedDefault bool, err error) {
	defaultRefs := winUserDefaultExtensionsForProgID(oldProgID)
	allCandidates := unionSortedStrings(candidateExts, winRegisteredExtensionsForProgID(oldProgID))
	var firstErr error
	for _, ext := range allCandidates {
		if !isSingleSegmentExtension(ext) {
			continue
		}
		if _, isDefault := defaultRefs[strings.ToLower(ext)]; isDefault {
			remaining = append(remaining, ext)
			continue
		}
		if rmErr := removeWinOpenWithProgid(ext, oldProgID); rmErr != nil {
			if firstErr == nil {
				firstErr = rmErr
			}
			remaining = append(remaining, ext)
		}
	}
	if len(defaultRefs) > 0 {
		for ext := range defaultRefs {
			remaining = append(remaining, ext)
		}
		if forwardExe != "" {
			if regErr := registerWinProgID(oldProgID, forwardExe); regErr != nil && firstErr == nil {
				firstErr = regErr
			}
		}
		return unionSortedStrings(remaining), true, firstErr
	}
	if len(remaining) == 0 {
		deleteRegTree(registry.CURRENT_USER, `Software\Classes\`+oldProgID)
	}
	return unionSortedStrings(remaining), false, firstErr
}

// collectWinPendingUninstallExes reads explicit uninstall records written by
// Block's installer/uninstaller (HKLM/HKCU\Software\Block\PendingCleanup\*)
// so cross-install cleanup only removes ProgIDs whose owning installation was
// explicitly uninstalled, never temporarily offline portable/removable drives
// (even when the drive letter has since been reassigned to another disk).
func collectWinPendingUninstallExes() map[string]struct{} {
	out := make(map[string]struct{})
	scanRoot := func(root registry.Key) {
		parent, err := registry.OpenKey(root, `Software\Block\PendingCleanup`, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			return
		}
		defer parent.Close()
		names, err := parent.ReadSubKeyNames(-1)
		if err != nil {
			return
		}
		for _, sub := range names {
			k, kerr := registry.OpenKey(parent, sub, registry.QUERY_VALUE)
			if kerr != nil {
				continue
			}
			exe, _, verr := k.GetStringValue("ExecutablePath")
			k.Close()
			if verr == nil {
				if norm := normalizeWinPath(exe); norm != "" {
					out[norm] = struct{}{}
				}
			}
		}
	}
	scanRoot(registry.LOCAL_MACHINE)
	scanRoot(registry.CURRENT_USER)
	return out
}

// cleanupOrphanedWinProgID removes a per-user ProgID and its OpenWithProgids
// entries ONLY if its registered executable is explicitly listed in
// uninstalledExes (recorded by an uninstaller) AND no longer exists on disk.
func cleanupOrphanedWinProgID(progID string, candidateExts []string, uninstalledExes map[string]struct{}) {
	if len(uninstalledExes) == 0 {
		return
	}
	targetExe := winProgIDCommandExe(progID)
	if targetExe == "" {
		return
	}
	if _, explicitlyUninstalled := uninstalledExes[normalizeWinPath(targetExe)]; !explicitlyUninstalled {
		return
	}
	if _, statErr := os.Stat(targetExe); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
		return
	}
	for _, ext := range unionSortedStrings(candidateExts, winRegisteredExtensionsForProgID(progID)) {
		if !isSingleSegmentExtension(ext) {
			continue
		}
		_ = removeWinOpenWithProgid(ext, progID)
	}
	deleteRegTree(registry.CURRENT_USER, `Software\Classes\`+progID)
}

// cleanupOrphanedWinScopedProgIDs removes any per-user Block.* ProgID (other
// than excludeProgID) whose owning installation has an explicit uninstall
// record and whose executable no longer exists on disk, preserving temporarily
// offline portable installs on removable or reassigned drives.
func cleanupOrphanedWinScopedProgIDs(candidateExts []string, excludeProgID string) {
	uninstalledExes := collectWinPendingUninstallExes()
	if len(uninstalledExes) == 0 {
		return
	}
	clsKey, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	names, _ := clsKey.ReadSubKeyNames(-1)
	clsKey.Close()
	for _, name := range names {
		if !strings.HasPrefix(name, "Block.") {
			continue
		}
		if strings.EqualFold(name, excludeProgID) {
			continue
		}
		cleanupOrphanedWinProgID(name, candidateExts, uninstalledExes)
	}
}

// collectOwnedWinLegacyProgIDs returns legacy HKCU ProgIDs (Block.Document,
// Block.Portable.Document, or a previously recorded LegacyProgID) that belong
// exclusively to this executable and should be migrated away from while
// preserving any extensions still defaulted to them.
func collectOwnedWinLegacyProgIDs(progID, exePath string, prev fileAssocState, hasPrev bool, scopePrev winAssocScopeState, allDesired []string) (legacyIDs []string, candidateExts []string) {
	seen := make(map[string]struct{}, 3)
	add := func(id string, extra []string) {
		if id == "" || id == progID {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		legacyIDs = append(legacyIDs, id)
		candidateExts = unionSortedStrings(candidateExts, extra, BuiltinAssocExtensions(), allDesired)
	}
	if scopePrev.LegacyProgID != "" && scopePrev.LegacyProgID != progID &&
		winLegacyProgIDOwnedByMigration(scopePrev.LegacyProgID, exePath, scopePrev.ExecutablePath) {
		add(scopePrev.LegacyProgID, scopePrev.LegacyExtensions)
	}
	for _, legacyID := range []string{winBlockLegacyProgID, winBlockLegacyPortableProgID, winBlockMachineUserProgID} {
		if legacyID == progID {
			continue
		}
		if !winLegacyProgIDOwnedByMigration(legacyID, exePath, prev.ExecutablePath) {
			continue
		}
		cmdExe := winProgIDCommandExe(legacyID)
		if cmdExe != "" && normalizeWinPath(cmdExe) == normalizeWinPath(exePath) {
			add(legacyID, prev.Extensions)
		} else if hasPrev && (prev.ProgID == legacyID || (legacyID == winBlockLegacyProgID && prev.ProgID == "")) &&
			normalizeWinPath(prev.ExecutablePath) == normalizeWinPath(exePath) {
			add(legacyID, prev.Extensions)
		}
	}
	return legacyIDs, candidateExts
}

func loadWinScopeState(st fileAssocState, progID string) (winAssocScopeState, bool) {
	if st.WinScopes != nil {
		if sc, ok := st.WinScopes[progID]; ok {
			return sc, true
		}
	}
	// Migrate legacy top-level state if it belonged to this ProgID (or legacy Block.Document).
	legacyProg := st.ProgID
	if legacyProg == "" && len(st.Extensions) > 0 {
		legacyProg = winBlockLegacyProgID
	}
	if legacyProg == progID {
		return winAssocScopeState{
			ExecutablePath:   st.ExecutablePath,
			Extensions:       st.Extensions,
			LegacyProgID:     st.LegacyProgID,
			LegacyExtensions: st.LegacyExtensions,
		}, true
	}
	return winAssocScopeState{}, false
}

func saveWinScopeState(appDataDir string, prev fileAssocState, progID string, sc winAssocScopeState) error {
	next := prev
	if next.WinScopes == nil {
		next.WinScopes = make(map[string]winAssocScopeState)
		// Preserve pre-v5 top-level scope if it belonged to a different ProgID.
		oldProg := prev.ProgID
		if oldProg == "" && len(prev.Extensions) > 0 {
			oldProg = winBlockLegacyProgID
		}
		if oldProg != "" && oldProg != progID {
			next.WinScopes[oldProg] = winAssocScopeState{
				ExecutablePath:   prev.ExecutablePath,
				Extensions:       prev.Extensions,
				LegacyProgID:     prev.LegacyProgID,
				LegacyExtensions: prev.LegacyExtensions,
			}
		}
	}
	next.WinScopes[progID] = sc
	// Keep top-level fields synced for diagnostics/backwards readers without
	// losing other scopes tracked inside WinScopes.
	next.ProgID = progID
	next.ExecutablePath = sc.ExecutablePath
	next.Extensions = sc.Extensions
	next.LegacyProgID = sc.LegacyProgID
	next.LegacyExtensions = sc.LegacyExtensions
	return saveFileAssocState(appDataDir, next)
}

func syncPlatformFileAssociations(appDataDir string, managedExts []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}

	filtered := make([]string, 0, len(managedExts))
	for _, ext := range managedExts {
		if isSingleSegmentExtension(ext) && isSafeAssocExtension(ext) {
			filtered = append(filtered, ext)
		}
	}
	allDesired := unionSortedStrings(builtinMarkdownExtensions(), filtered)

	progID, machineProgID := resolveWinProgID(exePath)

	// Clean up any orphaned Block.* ProgIDs whose executables were deleted
	// from disk (including machine-wide installs uninstalled while this user
	// was logged out), without touching live coexisting installs.
	cleanupOrphanedWinScopedProgIDs(unionSortedStrings(BuiltinAssocExtensions(), allDesired), progID)

	// For a machine-wide NSIS install whose HKLM Block.Machine.* already
	// points to this executable, only register delta extensions (e.g. newly
	// installed language plugins) under HKCU Block.Machine.User.<hash>.
	desired := allDesired
	if machineProgID != "" && winMachineProgIDMatchesFor(machineProgID, exePath) {
		delta := make([]string, 0, len(allDesired))
		for _, ext := range allDesired {
			if !winMachineHasOpenWithProgidFor(ext, machineProgID) {
				delta = append(delta, ext)
			}
		}
		desired = delta
	}

	prev, hasPrev := loadFileAssocState(appDataDir)
	scopePrev, hasScopePrev := loadWinScopeState(prev, progID)

	// Guard: never overwrite a ProgID owned by a different live installation.
	if !winProgIDOwnedByCurrentScope(progID, exePath, scopePrev.ExecutablePath) {
		return nil
	}

	// Detect owned legacy ProgIDs that need migration (e.g., an older build
	// upgraded from HKCU Block.Document or shared Block.Portable.Document).
	// Never migrate a legacy ProgID whose registered command points to a
	// different still-existing Block executable on disk.
	pendingLegacyProgIDs, pendingLegacyExts := collectOwnedWinLegacyProgIDs(progID, exePath, prev, hasPrev, scopePrev, allDesired)

	// Discover extensions actually registered for progID in HKCU so stale
	// OpenWithProgids entries are cleaned up even if file-associations.json was
	// missing or cleared.
	liveRegistered := winRegisteredExtensionsForProgID(progID)

	// If no HKCU delta extensions are needed (e.g. machine-wide NSIS install
	// already covers everything), remove any stale HKCU entries owned by this
	// scope while PRESERVING any ProgID still referenced as a user default.
	if len(desired) == 0 {
		changed := false
		var firstErr error
		keptSelfExts := make([]string, 0)
		selfRefs := winUserDefaultExtensionsForProgID(progID)
		staleCandidates := unionSortedStrings(scopePrev.Extensions, liveRegistered)
		if len(staleCandidates) > 0 && winProgIDOwnedByCurrentScope(progID, exePath, scopePrev.ExecutablePath) {
			for _, oldExt := range staleCandidates {
				if !isSingleSegmentExtension(oldExt) {
					continue
				}
				if _, isDef := selfRefs[strings.ToLower(oldExt)]; isDef {
					keptSelfExts = append(keptSelfExts, oldExt)
					continue
				}
				if rmErr := removeWinOpenWithProgid(oldExt, progID); rmErr != nil && firstErr == nil {
					firstErr = rmErr
					keptSelfExts = append(keptSelfExts, oldExt)
				} else {
					changed = true
				}
			}
		}
		if len(selfRefs) > 0 {
			for ext := range selfRefs {
				keptSelfExts = append(keptSelfExts, ext)
			}
			if !winProgIDMatches(progID, exePath) {
				if regErr := registerWinProgID(progID, exePath); regErr != nil && firstErr == nil {
					firstErr = regErr
				} else {
					changed = true
				}
			}
		} else if winProgIDMatches(progID, exePath) {
			deleteRegTree(registry.CURRENT_USER, `Software\Classes\`+progID)
			changed = true
		}

		nextLegacyProgID := ""
		var nextLegacyExts []string
		for _, legacyID := range pendingLegacyProgIDs {
			rem, retainedDef, migErr := migrateWinLegacyProgID(legacyID, pendingLegacyExts, exePath)
			changed = true
			if migErr != nil && firstErr == nil {
				firstErr = migErr
			}
			if retainedDef || migErr != nil || len(rem) > 0 {
				if nextLegacyProgID == "" {
					nextLegacyProgID = legacyID
				}
				nextLegacyExts = unionSortedStrings(nextLegacyExts, rem)
			}
		}
		if changed {
			notifyWinAssocChanged()
		}
		nextScope := winAssocScopeState{
			ExecutablePath:   exePath,
			Extensions:       unionSortedStrings(keptSelfExts),
			LegacyProgID:     nextLegacyProgID,
			LegacyExtensions: nextLegacyExts,
		}
		if !hasPrev || prev.Version != fileAssocStateVersion || !hasScopePrev ||
			scopePrev.ExecutablePath != nextScope.ExecutablePath ||
			!equalStringSlices(scopePrev.Extensions, nextScope.Extensions) ||
			scopePrev.LegacyProgID != nextScope.LegacyProgID ||
			!equalStringSlices(scopePrev.LegacyExtensions, nextScope.LegacyExtensions) {
			saveErr := saveWinScopeState(appDataDir, prev, progID, nextScope)
			if firstErr != nil {
				return firstErr
			}
			return saveErr
		}
		return firstErr
	}

	// Include any extension currently defaulted to progID so we never strip a
	// user's active default even if a plugin was disabled.
	if selfRefs := winUserDefaultExtensionsForProgID(progID); len(selfRefs) > 0 {
		extra := make([]string, 0, len(selfRefs))
		for ext := range selfRefs {
			if isSingleSegmentExtension(ext) {
				extra = append(extra, ext)
			}
		}
		desired = unionSortedStrings(desired, extra)
	}

	firstRun := winReconciledOnce.CompareAndSwap(false, true)
	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		hasScopePrev &&
		len(pendingLegacyProgIDs) == 0 &&
		scopePrev.LegacyProgID == "" &&
		scopePrev.ExecutablePath == exePath &&
		equalStringSlices(scopePrev.Extensions, desired) &&
		winProgIDMatches(progID, exePath) &&
		(!firstRun || (equalStringSlices(liveRegistered, desired) && winAllExtensionsRegistered(desired, progID))) {
		return nil
	}

	if err := registerWinProgID(progID, exePath); err != nil {
		return err
	}

	prevSet := make(map[string]struct{}, len(scopePrev.Extensions))
	for _, ext := range scopePrev.Extensions {
		prevSet[ext] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	registered := make([]string, 0, len(desired))
	var firstErr error
	for _, ext := range desired {
		desiredSet[ext] = struct{}{}
		if err := setWinOpenWithProgid(ext, progID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if _, wasPrev := prevSet[ext]; wasPrev || winHasOpenWithProgid(ext, progID) {
				registered = append(registered, ext)
			}
			continue
		}
		registered = append(registered, ext)
	}

	var keptStale []string
	for _, oldExt := range unionSortedStrings(scopePrev.Extensions, liveRegistered) {
		if _, keep := desiredSet[oldExt]; keep {
			continue
		}
		if err := removeWinOpenWithProgid(oldExt, progID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			keptStale = append(keptStale, oldExt)
		}
	}

	// Only retire owned legacy ProgIDs after the new ProgID and its
	// OpenWithProgids entries succeeded, and keep them forwarding if still
	// referenced as a user default.
	nextLegacyProgID := ""
	var nextLegacyExts []string
	if len(pendingLegacyProgIDs) > 0 {
		if firstErr == nil {
			for _, legacyID := range pendingLegacyProgIDs {
				rem, retainedDef, migErr := migrateWinLegacyProgID(legacyID, pendingLegacyExts, exePath)
				if migErr != nil && firstErr == nil {
					firstErr = migErr
				}
				if retainedDef || migErr != nil || len(rem) > 0 {
					if nextLegacyProgID == "" {
						nextLegacyProgID = legacyID
					}
					nextLegacyExts = unionSortedStrings(nextLegacyExts, rem)
				}
			}
		} else {
			nextLegacyProgID = pendingLegacyProgIDs[0]
			nextLegacyExts = pendingLegacyExts
		}
	}

	notifyWinAssocChanged()

	actual := unionSortedStrings(registered, keptStale)
	saveErr := saveWinScopeState(appDataDir, prev, progID, winAssocScopeState{
		ExecutablePath:   exePath,
		Extensions:       actual,
		LegacyProgID:     nextLegacyProgID,
		LegacyExtensions: nextLegacyExts,
	})
	if firstErr != nil {
		return firstErr
	}
	return saveErr
}

func cleanupPlatformFileAssociations(appDataDir string, _ bool) error {
	exePath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}
	prev, _ := loadFileAssocState(appDataDir)
	ownedProgID, _ := resolveWinProgID(exePath)
	scopePrev, _ := loadWinScopeState(prev, ownedProgID)
	allExts := unionSortedStrings(BuiltinAssocExtensions(), scopePrev.Extensions, scopePrev.LegacyExtensions)

	// Determine which per-user ProgIDs belong exclusively to this installation
	// so cleaning up one installation scope never removes a separate live
	// installation's ProgID.
	progIDs := make([]string, 0, 3)
	addProgID := func(pid string) {
		if pid == "" {
			return
		}
		for _, existing := range progIDs {
			if existing == pid {
				return
			}
		}
		progIDs = append(progIDs, pid)
	}
	if ownedProgID != "" && winProgIDOwnedByCurrentScope(ownedProgID, exePath, scopePrev.ExecutablePath) {
		addProgID(ownedProgID)
	}
	if scopePrev.LegacyProgID != "" && scopePrev.LegacyProgID != ownedProgID && winLegacyProgIDOwnedByMigration(scopePrev.LegacyProgID, exePath, scopePrev.ExecutablePath) {
		addProgID(scopePrev.LegacyProgID)
	}
	for _, legacyID := range []string{winBlockLegacyProgID, winBlockLegacyPortableProgID, winBlockMachineUserProgID} {
		if ownedProgID != legacyID {
			cmdExe := winProgIDCommandExe(legacyID)
			if cmdExe != "" && normalizeWinPath(cmdExe) == normalizeWinPath(exePath) {
				addProgID(legacyID)
			}
		}
	}
	cleanupOrphanedWinScopedProgIDs(allExts, ownedProgID)

	var firstErr error
	var remaining []string
	for _, pid := range progIDs {
		for _, ext := range unionSortedStrings(allExts, winRegisteredExtensionsForProgID(pid)) {
			if !isSingleSegmentExtension(ext) {
				continue
			}
			if err := removeWinOpenWithProgid(ext, pid); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				remaining = append(remaining, ext)
			}
		}
		deleteRegTree(registry.CURRENT_USER, `Software\Classes\`+pid)
	}
	notifyWinAssocChanged()
	if firstErr != nil {
		_ = saveWinScopeState(appDataDir, prev, ownedProgID, winAssocScopeState{
			ExecutablePath: scopePrev.ExecutablePath,
			Extensions:     unionSortedStrings(remaining),
		})
		return firstErr
	}
	if prev.WinScopes != nil {
		delete(prev.WinScopes, ownedProgID)
		if len(prev.WinScopes) > 0 {
			keys := make([]string, 0, len(prev.WinScopes))
			for k := range prev.WinScopes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			keep := prev.WinScopes[keys[0]]
			prev.ProgID = keys[0]
			prev.ExecutablePath = keep.ExecutablePath
			prev.Extensions = keep.Extensions
			prev.LegacyProgID = keep.LegacyProgID
			prev.LegacyExtensions = keep.LegacyExtensions
			return saveFileAssocState(appDataDir, prev)
		}
	}
	_ = os.Remove(fileAssocStatePath(appDataDir))
	return nil
}

func notifyWinAssocChanged() {
	_, _, _ = winSHChangeNotif.Call(
		uintptr(winSHCNEAssocChanged),
		uintptr(winSHCNFIdList),
		0,
		0,
	)
}

func winOpenCommand(exePath string) string {
	return fmt.Sprintf(`"%s" "%%1"`, exePath)
}

func winProgIDMatches(progID, exePath string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+progID+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue("")
	return err == nil && val == winOpenCommand(exePath)
}

func winHasOpenWithProgid(ext, progID string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetValue(progID, nil)
	return err == nil
}

func winAllExtensionsRegistered(exts []string, progID string) bool {
	for _, ext := range exts {
		if !winHasOpenWithProgid(ext, progID) {
			return false
		}
	}
	return true
}

// registerWinProgID registers Block's dedicated ProgID under HKCU\Software\Classes
// without touching shared Applications\<exe>.exe keys owned by other installs.
func registerWinProgID(progID, exePath string) error {
	openCmd := winOpenCommand(exePath)
	iconVal := fmt.Sprintf(`"%s",0`, exePath)
	friendlyApp := "Block"
	if strings.HasPrefix(progID, winBlockPortableProgIDPrefix) {
		friendlyApp = "Block (Portable)"
	}

	progRoot := `Software\Classes\` + progID
	if err := setRegDefault(registry.CURRENT_USER, progRoot, "Block Document"); err != nil {
		return err
	}
	if err := setRegString(registry.CURRENT_USER, progRoot, "FriendlyTypeName", "Block Document"); err != nil {
		return err
	}
	if err := setRegDefault(registry.CURRENT_USER, progRoot+`\DefaultIcon`, iconVal); err != nil {
		return err
	}
	if err := setRegString(registry.CURRENT_USER, progRoot+`\Application`, "ApplicationName", friendlyApp); err != nil {
		return err
	}
	_ = setRegString(registry.CURRENT_USER, progRoot+`\Application`, "ApplicationDescription", friendlyApp)
	_ = setRegString(registry.CURRENT_USER, progRoot+`\shell\open`, "FriendlyAppName", friendlyApp)
	if err := setRegDefault(registry.CURRENT_USER, progRoot+`\shell\open\command`, openCmd); err != nil {
		return err
	}
	return nil
}

func setWinOpenWithProgid(ext, progID string) error {
	return setRegString(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, progID, "")
}

func removeWinOpenWithProgid(ext, progID string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\OpenWithProgids`, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(progID); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func deleteRegTree(root registry.Key, subkey string) {
	k, err := registry.OpenKey(root, subkey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err == nil {
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, name := range names {
			deleteRegTree(root, subkey+`\`+name)
		}
	}
	_ = registry.DeleteKey(root, subkey)
}

func setRegDefault(root registry.Key, subkey, value string) error {
	return setRegString(root, subkey, "", value)
}

func setRegString(root registry.Key, subkey, name, value string) error {
	k, _, err := registry.CreateKey(root, subkey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("registry create %s: %w", subkey, err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("registry write %s\\%s: %w", subkey, name, err)
	}
	return nil
}
