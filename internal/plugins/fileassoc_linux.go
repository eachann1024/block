//go:build linux

package plugins

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	linuxCustomMIMEPrefix  = "application/x-block-ext-"
	linuxHelperDesktopName = "block-plugins.desktop"
	linuxManagedMarker     = "# Managed-By=block-file-associations"
	linuxLegacyMIMEPackage = "block-plugins.xml"
)

func isFileAssocLockPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func linuxXDGDataHome() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Clean(xdg)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share")
	}
	return ""
}

func linuxXDGDataDirs(dataHome string) []string {
	dirsEnv := strings.TrimSpace(os.Getenv("XDG_DATA_DIRS"))
	if dirsEnv == "" {
		dirsEnv = "/usr/local/share:/usr/share"
	}
	var out []string
	seen := make(map[string]struct{})
	add := func(d string) {
		clean := filepath.Clean(strings.TrimSpace(d))
		if clean == "" || clean == "." {
			return
		}
		if _, dup := seen[clean]; dup {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	add(dataHome)
	for _, d := range filepath.SplitList(dirsEnv) {
		add(d)
	}
	return out
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

// buildLinuxDesktopExecLine builds the Freedesktop Desktop Entry Exec= value.
// For paths containing literal '%', GIO checks argv[0] before field-code
// expansion yet expands '%' across the entire Exec line on launch; wrapping
// through /usr/bin/env -- with '%%' lets GIO both validate argv[0] and pass
// the literal '%' path at launch time.
func buildLinuxDesktopExecLine(path string) string {
	encoded := encodeDesktopExecToken(path)
	if strings.Contains(path, "%") {
		encoded = strings.ReplaceAll(encoded, "%", "%%")
		return "/usr/bin/env -- " + encoded + " %F"
	}
	return encoded + " %F"
}

func encodeDesktopExecToken(path string) string {
	const reserved = " \t\n\"'\\><~|&;$*?#()`"
	quoted := path
	if strings.ContainsAny(path, reserved) {
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(path); i++ {
			c := path[i]
			if c == '"' || c == '`' || c == '$' || c == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
		b.WriteByte('"')
		quoted = b.String()
	}
	return strings.ReplaceAll(quoted, `\`, `\\`)
}

type linuxGlobMatch struct {
	mimeType string
	weight   int
}

func loadLinuxMIMEGlobs(dataHome string) map[string][]string {
	bestByExt := make(map[string][]linuxGlobMatch)
	seen := make(map[string]map[string]int)
	blockedLower := make(map[string]struct{})

	// Scan XDG data dirs from highest precedence (dataHome) to system defaults.
	// Per Shared MIME-info spec, __NOGLOBS__ suppresses globs for that MIME type
	// from LOWER-precedence directories while still honoring replacement globs
	// declared in the SAME directory.
	for _, baseDir := range linuxXDGDataDirs(dataHome) {
		f, err := os.Open(filepath.Join(baseDir, "mime", "globs2"))
		if err != nil {
			continue
		}
		dirBlocked := make(map[string]struct{})
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, ":")
			if len(parts) < 3 {
				continue
			}
			weight, wErr := strconv.Atoi(strings.TrimSpace(parts[0]))
			if wErr != nil {
				weight = 50
			}
			mimeType := strings.TrimSpace(parts[1])
			pattern := strings.TrimSpace(parts[2])
			if mimeType == "" || strings.HasPrefix(mimeType, linuxCustomMIMEPrefix) {
				continue
			}
			if pattern == "__NOGLOBS__" {
				dirBlocked[mimeType] = struct{}{}
				continue
			}
			if _, suppressed := blockedLower[mimeType]; suppressed {
				continue
			}
			if !strings.HasPrefix(pattern, "*.") {
				continue
			}
			if len(parts) >= 4 && strings.Contains(parts[3], "cs") && pattern != strings.ToLower(pattern) {
				continue
			}
			ext := strings.ToLower(pattern[1:])
			if strings.ContainsAny(ext, "*?[]") {
				continue
			}
			if seen[ext] == nil {
				seen[ext] = make(map[string]int)
			}
			if prevW, ok := seen[ext][mimeType]; ok {
				if weight <= prevW {
					continue
				}
			}
			seen[ext][mimeType] = weight
			bestByExt[ext] = append(bestByExt[ext], linuxGlobMatch{mimeType: mimeType, weight: weight})
		}
		f.Close()
		for mimeType := range dirBlocked {
			blockedLower[mimeType] = struct{}{}
		}
	}

	out := make(map[string][]string, len(bestByExt))
	for ext, matches := range bestByExt {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].weight != matches[j].weight {
				return matches[i].weight > matches[j].weight
			}
			return matches[i].mimeType < matches[j].mimeType
		})
		var types []string
		dedup := make(map[string]struct{}, len(matches))
		for _, m := range matches {
			if _, ok := dedup[m.mimeType]; ok {
				continue
			}
			dedup[m.mimeType] = struct{}{}
			types = append(types, m.mimeType)
		}
		out[ext] = types
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

func cleanupPlatformFileAssociations(appDataDir string, _ bool) error {
	dataHome := linuxXDGDataHome()
	if dataHome == "" {
		return nil
	}
	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktop := filepath.Join(desktopDir, linuxHelperDesktopName)
	legacyDesktop := filepath.Join(desktopDir, "block.desktop")

	var firstErr error
	if err := removeLegacyLinuxCustomMIME(dataHome); err != nil {
		firstErr = err
	}
	desktopChanged := false
	if _, err := os.Stat(helperDesktop); err == nil {
		if rmErr := os.Remove(helperDesktop); rmErr != nil && !os.IsNotExist(rmErr) {
			if firstErr == nil {
				firstErr = rmErr
			}
		} else {
			desktopChanged = true
		}
	}
	if raw, err := os.ReadFile(legacyDesktop); err == nil && strings.Contains(string(raw), linuxManagedMarker) {
		_ = os.Remove(legacyDesktop)
		desktopChanged = true
	}
	if desktopChanged {
		if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// syncPlatformFileAssociations manages a dedicated NoDisplay=true helper entry
// (~/.local/share/applications/block-plugins.desktop) for extensions already
// recognized by shared-mime-info / system MIME tables. It intentionally avoids
// defining synthetic custom MIME types so GIO never promotes Block over the
// user's default plain-text editor, and never overwrites block.desktop.
func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	dataHome := linuxXDGDataHome()
	if dataHome == "" {
		return nil
	}
	execPath, err := resolveLinuxDesktopExec()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	desktopDir := filepath.Join(dataHome, "applications")
	helperDesktop := filepath.Join(desktopDir, linuxHelperDesktopName)
	prev, hasPrev := loadFileAssocState(appDataDir)

	// Migrate away from any legacy managed block.desktop override or custom MIME xml.
	legacyDesktop := filepath.Join(desktopDir, "block.desktop")
	if raw, err := os.ReadFile(legacyDesktop); err == nil && strings.Contains(string(raw), linuxManagedMarker) {
		_ = os.Remove(legacyDesktop)
	}
	_ = removeLegacyLinuxCustomMIME(dataHome)

	globs := loadLinuxMIMEGlobs(dataHome)
	var mimeList []string
	for _, ext := range pluginExts {
		if matched := globs[ext]; len(matched) > 0 {
			mimeList = append(mimeList, matched...)
			continue
		}
		if mt := mime.TypeByExtension(ext); mt != "" {
			if idx := strings.IndexByte(mt, ';'); idx >= 0 {
				mt = strings.TrimSpace(mt[:idx])
			}
			if mt != "" && mt != "application/octet-stream" {
				mimeList = append(mimeList, mt)
			}
		}
	}
	mimeList = unionSortedStrings(mimeList)

	if len(mimeList) == 0 {
		if err := cleanupPlatformFileAssociations(appDataDir, false); err != nil {
			return err
		}
		if hasPrev && len(prev.Extensions) > 0 {
			return saveFileAssocState(appDataDir, fileAssocState{
				ExecutablePath: execPath,
				Extensions:     []string{},
			})
		}
		return nil
	}

	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.ExecutablePath == execPath &&
		equalStringSlices(prev.Extensions, mimeList) {
		if _, err := os.Stat(helperDesktop); err == nil {
			return nil
		}
	}

	ensureLinuxAppImageIcon(dataHome)

	desktopContent := strings.Join([]string{
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
	if err := writeFileAtomic(helperDesktop, []byte(desktopContent), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", linuxHelperDesktopName, err)
	}
	if err := runLinuxToolIfPresent("update-desktop-database", desktopDir); err != nil {
		return fmt.Errorf("update-desktop-database: %w", err)
	}

	return saveFileAssocState(appDataDir, fileAssocState{
		ExecutablePath: execPath,
		Extensions:     mimeList,
	})
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
