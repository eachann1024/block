//go:build linux

package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const linuxCustomMIMEPrefix = "application/x-block-ext-"

var linuxBuiltinMIMETypes = []string{
	"text/markdown",
	"text/x-markdown",
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

func resolveLinuxDesktopExec() (string, error) {
	if appImage := strings.TrimSpace(os.Getenv("APPIMAGE")); appImage != "" {
		return appImage, nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}
	return exePath, nil
}

func quoteDesktopExec(path string) string {
	if !strings.ContainsAny(path, " \t\n\"'\\`$()<>|&;") {
		return path
	}
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "$", `\$`)
	return `"` + escaped + `"`
}

func loadLinuxMIMEGlobs(dataHome string) map[string][]string {
	out := make(map[string][]string)
	seen := make(map[string]map[string]struct{})
	paths := []string{
		"/usr/share/mime/globs2",
		"/usr/local/share/mime/globs2",
		filepath.Join(dataHome, "mime", "globs2"),
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
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
			mimeType := strings.TrimSpace(parts[1])
			pattern := strings.ToLower(strings.TrimSpace(parts[2]))
			if mimeType == "" || strings.HasPrefix(mimeType, linuxCustomMIMEPrefix) {
				continue
			}
			if !strings.HasPrefix(pattern, "*.") {
				continue
			}
			ext := pattern[1:] // ".plist"
			if seen[ext] == nil {
				seen[ext] = make(map[string]struct{})
			}
			if _, dup := seen[ext][mimeType]; dup {
				continue
			}
			seen[ext][mimeType] = struct{}{}
			out[ext] = append(out[ext], mimeType)
		}
		f.Close()
	}
	return out
}

func sanitizeLinuxCustomMIMESuffix(ext string) string {
	bare := strings.TrimPrefix(strings.ToLower(ext), ".")
	var b strings.Builder
	for _, r := range bare {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '+' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

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
	desktopPath := filepath.Join(desktopDir, "block.desktop")
	mimeDir := filepath.Join(dataHome, "mime")
	mimePkgPath := filepath.Join(mimeDir, "packages", "block-plugins.xml")

	globs := loadLinuxMIMEGlobs(dataHome)
	mimeList := append([]string(nil), linuxBuiltinMIMETypes...)
	type customMIMEEntry struct {
		mimeType string
		ext      string
	}
	var customEntries []customMIMEEntry
	var customMIMENames []string

	for _, ext := range pluginExts {
		if matched := globs[ext]; len(matched) > 0 {
			mimeList = append(mimeList, matched...)
			continue
		}
		if mt := mime.TypeByExtension(ext); mt != "" {
			if idx := strings.IndexByte(mt, ';'); idx >= 0 {
				mt = strings.TrimSpace(mt[:idx])
			}
			if mt != "" {
				mimeList = append(mimeList, mt)
				continue
			}
		}
		suffix := sanitizeLinuxCustomMIMESuffix(ext)
		if suffix == "" {
			continue
		}
		custom := linuxCustomMIMEPrefix + suffix
		mimeList = append(mimeList, custom)
		customEntries = append(customEntries, customMIMEEntry{mimeType: custom, ext: ext})
		customMIMENames = append(customMIMENames, custom)
	}

	mimeList = unionSortedStrings(mimeList)
	sort.Strings(customMIMENames)

	prev, hasPrev := loadFileAssocState(appDataDir)
	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.ExecutablePath == execPath &&
		equalStringSlices(prev.Extensions, pluginExts) &&
		equalStringSlices(prev.CustomMIMEs, customMIMENames) {
		if _, err := os.Stat(desktopPath); err == nil {
			return nil
		}
	}

	// Update or remove custom MIME definitions for extensions unknown to system globs.
	mimeDBChanged := !hasPrev || !equalStringSlices(prev.CustomMIMEs, customMIMENames)
	if len(customEntries) == 0 {
		if _, err := os.Stat(mimePkgPath); err == nil {
			_ = os.Remove(mimePkgPath)
			mimeDBChanged = true
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(mimePkgPath), 0o755); err != nil {
			return fmt.Errorf("create xdg mime packages dir: %w", err)
		}
		var xmlBuf bytes.Buffer
		xmlBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
		xmlBuf.WriteString(`<mime-info xmlns="http://www.freedesktop.org/standards/shared-mime-info">` + "\n")
		for _, entry := range customEntries {
			xmlBuf.WriteString(`  <mime-type type="`)
			_ = xml.EscapeText(&xmlBuf, []byte(entry.mimeType))
			xmlBuf.WriteString(`">` + "\n")
			xmlBuf.WriteString("    <comment>Block Language Extension Document</comment>\n")
			xmlBuf.WriteString(`    <sub-class-of type="text/plain"/>` + "\n")
			xmlBuf.WriteString(`    <glob pattern="*`)
			_ = xml.EscapeText(&xmlBuf, []byte(entry.ext))
			xmlBuf.WriteString(`"/>` + "\n")
			xmlBuf.WriteString("  </mime-type>\n")
		}
		xmlBuf.WriteString("</mime-info>\n")
		if err := os.WriteFile(mimePkgPath, xmlBuf.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write custom mime package: %w", err)
		}
		mimeDBChanged = true
	}

	if mimeDBChanged {
		runLinuxToolBestEffort("update-mime-database", mimeDir)
	}

	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		return fmt.Errorf("create xdg applications dir: %w", err)
	}
	desktopContent := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Block",
		"Exec=" + quoteDesktopExec(execPath) + " %F",
		"Icon=block",
		"Categories=Office;TextEditor;Utility;",
		"MimeType=" + strings.Join(mimeList, ";") + ";",
		"Terminal=false",
		"StartupWMClass=block",
		"",
	}, "\n")
	if err := os.WriteFile(desktopPath, []byte(desktopContent), 0o644); err != nil {
		return fmt.Errorf("write block.desktop: %w", err)
	}
	runLinuxToolBestEffort("update-desktop-database", desktopDir)

	return saveFileAssocState(appDataDir, fileAssocState{
		ExecutablePath: execPath,
		Extensions:     pluginExts,
		CustomMIMEs:    customMIMENames,
	})
}

func runLinuxToolBestEffort(name string, args ...string) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, bin, args...).Run()
}
