//go:build darwin

package plugins

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"block/internal/updater"
	"github.com/ebitengine/purego"
)

const (
	macAssocHelperBundleID = "dev.eachann.block.associations"
	macAssocHelperExecName = "block-opener"
	macLSRegisterPath      = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	macCodesignPath        = "/usr/bin/codesign"
	macDefaultMainAppPath  = "/Applications/Block.app"
	kCFStringEncodingUTF8  = 0x08000100
)

var (
	macUTOnce     sync.Once
	macUTReady    bool
	cfStrCreate   func(alloc uintptr, cstr string, encoding uint32) uintptr
	cfReleaseFn   func(cf uintptr)
	utCreateTagFn func(inTagClass, inTag, inConformingToUTI uintptr) uintptr
	utConformsFn  func(inUTI, inConformsToUTI uintptr) bool
)

func initMacUTType() {
	macUTOnce.Do(func() {
		cfLib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		csLib, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cfStrCreate, cfLib, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfReleaseFn, cfLib, "CFRelease")
		purego.RegisterLibFunc(&utCreateTagFn, csLib, "UTTypeCreatePreferredIdentifierForTag")
		purego.RegisterLibFunc(&utConformsFn, csLib, "UTTypeConformsTo")
		macUTReady = true
	})
}

func macBuiltinFallbackHandled(ext string) bool {
	for _, md := range builtinMarkdownExtensions {
		if ext == md {
			return true
		}
	}
	return ext == ".txt" || ext == ".text"
}

// macExtensionHandledByMainApp reports whether main Block.app already appears
// in Finder's "Open With" menu for ext (via Markdown or public.plain-text),
// preventing duplicate "Block.app" entries in Finder.
func macExtensionHandledByMainApp(ext string) bool {
	if macBuiltinFallbackHandled(ext) {
		return true
	}
	initMacUTType()
	if !macUTReady {
		return false
	}
	bare := strings.TrimPrefix(ext, ".")
	if bare == "" {
		return true
	}

	tagClass := cfStrCreate(0, "public.filename-extension", kCFStringEncodingUTF8)
	if tagClass == 0 {
		return false
	}
	defer cfReleaseFn(tagClass)

	tag := cfStrCreate(0, bare, kCFStringEncodingUTF8)
	if tag == 0 {
		return false
	}
	defer cfReleaseFn(tag)

	uti := utCreateTagFn(tagClass, tag, 0)
	if uti == 0 {
		return false
	}
	defer cfReleaseFn(uti)

	for _, baseUTI := range []string{"public.plain-text", "net.daringfireball.markdown", "public.markdown"} {
		base := cfStrCreate(0, baseUTI, kCFStringEncodingUTF8)
		if base == 0 {
			continue
		}
		conforms := utConformsFn(uti, base)
		cfReleaseFn(base)
		if conforms {
			return true
		}
	}
	return false
}

func findEnclosingMacAppBundle(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 6; i++ {
		if strings.HasSuffix(strings.ToLower(dir), ".app") {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if st, err := os.Stat(macDefaultMainAppPath); err == nil && st.IsDir() {
		return macDefaultMainAppPath
	}
	return ""
}

func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	var bareExts []string
	for _, ext := range pluginExts {
		if macExtensionHandledByMainApp(ext) {
			continue
		}
		if bare := strings.TrimPrefix(ext, "."); bare != "" {
			bareExts = append(bareExts, bare)
		}
	}
	sort.Strings(bareExts)

	assocDir := filepath.Join(appDataDir, "Associations")
	helperApp := filepath.Join(assocDir, "Block.app")
	prev, hasPrev := loadFileAssocState(appDataDir)

	if len(bareExts) == 0 {
		if _, err := os.Stat(helperApp); err == nil {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
			_ = os.RemoveAll(helperApp)
		} else if hasPrev && len(prev.Extensions) > 0 {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
		}
		if hasPrev && len(prev.Extensions) > 0 {
			return saveFileAssocState(appDataDir, fileAssocState{Extensions: []string{}})
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}
	exeInfo, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	mainAppPath := findEnclosingMacAppBundle(exePath)

	helperExecPath := filepath.Join(helperApp, "Contents", "MacOS", macAssocHelperExecName)
	helperPlistPath := filepath.Join(helperApp, "Contents", "Info.plist")

	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.AppPath == mainAppPath &&
		prev.ExecutablePath == exePath &&
		prev.ExecutableSize == exeInfo.Size() &&
		prev.ExecutableMtime == exeInfo.ModTime().UnixNano() &&
		equalStringSlices(prev.Extensions, bareExts) {
		if _, err1 := os.Stat(helperExecPath); err1 == nil {
			if _, err2 := os.Stat(helperPlistPath); err2 == nil {
				return nil
			}
		}
	}

	if err := os.MkdirAll(filepath.Join(helperApp, "Contents", "MacOS"), 0o755); err != nil {
		return fmt.Errorf("create helper bundle macos dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(helperApp, "Contents", "Resources"), 0o755); err != nil {
		return fmt.Errorf("create helper bundle resources dir: %w", err)
	}
	_ = os.WriteFile(filepath.Join(assocDir, ".metadata_never_index"), nil, 0o644)

	needBinaryCopy := true
	if hasPrev &&
		prev.ExecutablePath == exePath &&
		prev.ExecutableSize == exeInfo.Size() &&
		prev.ExecutableMtime == exeInfo.ModTime().UnixNano() {
		if _, err := os.Stat(helperExecPath); err == nil {
			needBinaryCopy = false
		}
	}
	if needBinaryCopy {
		if err := copyFileAtomic(exePath, helperExecPath, 0o755); err != nil {
			return fmt.Errorf("copy helper executable: %w", err)
		}
	}

	if mainAppPath != "" {
		srcIcon := filepath.Join(mainAppPath, "Contents", "Resources", "AppIcon.icns")
		dstIcon := filepath.Join(helperApp, "Contents", "Resources", "AppIcon.icns")
		if srcStat, err := os.Stat(srcIcon); err == nil {
			dstStat, dstErr := os.Stat(dstIcon)
			if dstErr != nil || dstStat.Size() != srcStat.Size() {
				_ = copyFileAtomic(srcIcon, dstIcon, 0o644)
			}
		}
	}

	plistBytes := buildMacHelperInfoPlist(updater.CurrentVersion(), mainAppPath, bareExts)
	if err := writeFileAtomic(helperPlistPath, plistBytes, 0o644); err != nil {
		return fmt.Errorf("write helper Info.plist: %w", err)
	}

	// Ad-hoc sign the locally generated helper so LaunchServices and Gatekeeper
	// accept its bundle metadata cleanly without touching the main Block.app.
	_ = runCommandTimeout(10*time.Second, macCodesignPath,
		"--force", "--sign", "-",
		"--identifier", macAssocHelperBundleID,
		helperApp,
	)

	if err := runCommandTimeout(10*time.Second, macLSRegisterPath, "-f", helperApp); err != nil {
		return fmt.Errorf("lsregister helper bundle: %w", err)
	}

	return saveFileAssocState(appDataDir, fileAssocState{
		AppPath:         mainAppPath,
		ExecutablePath:  exePath,
		ExecutableSize:  exeInfo.Size(),
		ExecutableMtime: exeInfo.ModTime().UnixNano(),
		Extensions:      bareExts,
	})
}

func buildMacHelperInfoPlist(version, mainAppPath string, bareExts []string) []byte {
	ver := strings.TrimSpace(version)
	if ver == "" || ver == "dev" {
		ver = "1.0.0"
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	writePlistKV(&b, "CFBundleName", "Block")
	writePlistKV(&b, "CFBundleDisplayName", "Block")
	writePlistKV(&b, "CFBundleIdentifier", macAssocHelperBundleID)
	writePlistKV(&b, "CFBundleExecutable", macAssocHelperExecName)
	writePlistKV(&b, "CFBundlePackageType", "APPL")
	writePlistKV(&b, "CFBundleIconFile", "AppIcon")
	writePlistKV(&b, "CFBundleVersion", ver)
	writePlistKV(&b, "CFBundleShortVersionString", ver)
	writePlistKV(&b, "LSMinimumSystemVersion", "12.0")
	b.WriteString("  <key>LSUIElement</key>\n  <true/>\n")
	if mainAppPath != "" {
		writePlistKV(&b, "BlockMainAppPath", mainAppPath)
	}
	b.WriteString("  <key>CFBundleDocumentTypes</key>\n  <array>\n    <dict>\n")
	b.WriteString("      <key>CFBundleTypeName</key>\n      <string>Block Language Plugin Document</string>\n")
	b.WriteString("      <key>CFBundleTypeRole</key>\n      <string>Editor</string>\n")
	b.WriteString("      <key>LSHandlerRank</key>\n      <string>Alternate</string>\n")
	b.WriteString("      <key>CFBundleTypeExtensions</key>\n      <array>\n")
	for _, ext := range bareExts {
		b.WriteString("        <string>")
		_ = xml.EscapeText(&b, []byte(ext))
		b.WriteString("</string>\n")
	}
	b.WriteString("      </array>\n    </dict>\n  </array>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

func writePlistKV(b *bytes.Buffer, key, value string) {
	b.WriteString("  <key>")
	_ = xml.EscapeText(b, []byte(key))
	b.WriteString("</key>\n  <string>")
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</string>\n")
}

func copyFileAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeFileAtomic(dst string, data []byte, mode os.FileMode) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func runCommandTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s %v: %w (%s)", name, args, err, msg)
		}
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
