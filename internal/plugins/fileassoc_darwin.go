//go:build darwin

package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	macUTOnce         sync.Once
	macUTReady        bool
	cfStrCreate       func(alloc uintptr, cstr string, encoding uint32) uintptr
	cfReleaseFn       func(cf uintptr)
	cfArrayGetCount   func(arr uintptr) int
	cfArrayGetValAt   func(arr uintptr, idx int) uintptr
	utCreateAllTagsFn func(inTagClass, inTag, inConformingToUTI uintptr) uintptr
	utConformsFn      func(inUTI, inConformsToUTI uintptr) uint8

	macReconciledOnce atomic.Bool
	macLSRegSuffixRe  = regexp.MustCompile(`\s+\(0x[0-9a-fA-F]+\)\s*$`)
)

func isFileAssocLockPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

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
		s1, err1 := purego.Dlsym(cfLib, "CFStringCreateWithCString")
		s2, err2 := purego.Dlsym(cfLib, "CFRelease")
		s3, err3 := purego.Dlsym(cfLib, "CFArrayGetCount")
		s4, err4 := purego.Dlsym(cfLib, "CFArrayGetValueAtIndex")
		s5, err5 := purego.Dlsym(csLib, "UTTypeCreateAllIdentifiersForTag")
		s6, err6 := purego.Dlsym(csLib, "UTTypeConformsTo")
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			return
		}
		purego.RegisterFunc(&cfStrCreate, s1)
		purego.RegisterFunc(&cfReleaseFn, s2)
		purego.RegisterFunc(&cfArrayGetCount, s3)
		purego.RegisterFunc(&cfArrayGetValAt, s4)
		purego.RegisterFunc(&utCreateAllTagsFn, s5)
		purego.RegisterFunc(&utConformsFn, s6)
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

// macExtensionHandledByMainApp checks all UTIs associated with ext via
// UTTypeCreateAllIdentifiersForTag; if any conforms to public.plain-text or
// Markdown, main Block.app already appears in Finder's "Open With" menu.
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

	arr := utCreateAllTagsFn(tagClass, tag, 0)
	if arr == 0 {
		return false
	}
	defer cfReleaseFn(arr)

	count := cfArrayGetCount(arr)
	if count <= 0 {
		return false
	}

	baseNames := []string{"public.plain-text", "net.daringfireball.markdown", "public.markdown"}
	bases := make([]uintptr, 0, len(baseNames))
	for _, name := range baseNames {
		if b := cfStrCreate(0, name, kCFStringEncodingUTF8); b != 0 {
			bases = append(bases, b)
		}
	}
	defer func() {
		for _, b := range bases {
			cfReleaseFn(b)
		}
	}()

	for i := 0; i < count; i++ {
		uti := cfArrayGetValAt(arr, i)
		if uti == 0 {
			continue
		}
		for _, base := range bases {
			if utConformsFn(uti, base) != 0 {
				return true
			}
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

func macHelperBundlePath(appDataDir string) string {
	return filepath.Join(appDataDir, "Associations", "Block.app")
}

// pruneStaleMacHelperRegistrations unregisters any LaunchServices records for
// our helper bundle ID pointing to paths other than canonicalHelper.
func pruneStaleMacHelperRegistrations(canonicalHelper string) {
	canonical := ""
	if canonicalHelper != "" {
		canonical = filepath.Clean(canonicalHelper)
		if realPath, err := filepath.EvalSymlinks(canonical); err == nil && realPath != "" {
			canonical = realPath
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, macLSRegisterPath, "-dump")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}

	seen := make(map[string]struct{})
	var stale []string
	var curPath, curID string
	flush := func() {
		if curPath != "" && curID == macAssocHelperBundleID {
			clean := filepath.Clean(curPath)
			if realPath, err := filepath.EvalSymlinks(clean); err == nil && realPath != "" {
				clean = realPath
			}
			if canonical == "" || clean != canonical {
				if _, dup := seen[curPath]; !dup {
					seen[curPath] = struct{}{}
					stale = append(stale, curPath)
				}
			}
		}
		curPath = ""
		curID = ""
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "--------------------------------------------------------------------------------") {
			flush()
			continue
		}
		if curPath == "" && strings.HasPrefix(line, "path:") {
			curPath = strings.TrimSpace(macLSRegSuffixRe.ReplaceAllString(line[len("path:"):], ""))
		} else if curID == "" && strings.HasPrefix(line, "identifier:") {
			curID = strings.TrimSpace(macLSRegSuffixRe.ReplaceAllString(line[len("identifier:"):], ""))
		}
	}
	flush()
	_ = cmd.Wait()

	for _, p := range stale {
		_ = runCommandTimeout(5*time.Second, macLSRegisterPath, "-u", p)
	}
}

func cleanupPlatformFileAssociations(appDataDir string, isolated bool) error {
	helperApp := macHelperBundlePath(appDataDir)
	_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
	if !isolated {
		pruneStaleMacHelperRegistrations("")
	}
	if err := os.RemoveAll(helperApp); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func syncPlatformFileAssociations(appDataDir string, pluginExts []string) error {
	var bareExts []string
	for _, ext := range pluginExts {
		if !isSingleSegmentExtension(ext) || macExtensionHandledByMainApp(ext) {
			continue
		}
		if bare := strings.TrimPrefix(ext, "."); bare != "" {
			bareExts = append(bareExts, bare)
		}
	}
	sort.Strings(bareExts)

	assocDir := filepath.Join(appDataDir, "Associations")
	helperApp := macHelperBundlePath(appDataDir)
	prev, hasPrev := loadFileAssocState(appDataDir)
	firstRun := macReconciledOnce.CompareAndSwap(false, true)

	if len(bareExts) == 0 {
		if _, err := os.Stat(helperApp); err == nil {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
			if err := os.RemoveAll(helperApp); err != nil {
				return fmt.Errorf("remove helper bundle: %w", err)
			}
		} else if hasPrev && len(prev.Extensions) > 0 {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
		}
		if firstRun {
			pruneStaleMacHelperRegistrations("")
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
	appVer := normalizedMacBundleVersion(updater.CurrentVersion())

	helperExecPath := filepath.Join(helperApp, "Contents", "MacOS", macAssocHelperExecName)
	helperPlistPath := filepath.Join(helperApp, "Contents", "Info.plist")

	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		prev.AppVersion == appVer &&
		prev.AppPath == mainAppPath &&
		prev.ExecutablePath == exePath &&
		prev.ExecutableSize == exeInfo.Size() &&
		prev.ExecutableMtime == exeInfo.ModTime().UnixNano() &&
		equalStringSlices(prev.Extensions, bareExts) {
		if _, err1 := os.Stat(helperExecPath); err1 == nil {
			if _, err2 := os.Stat(helperPlistPath); err2 == nil {
				if firstRun {
					pruneStaleMacHelperRegistrations(helperApp)
					_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-f", helperApp)
				}
				return nil
			}
		}
	}

	if err := os.MkdirAll(assocDir, 0o755); err != nil {
		return fmt.Errorf("create associations dir: %w", err)
	}
	_ = os.WriteFile(filepath.Join(assocDir, ".metadata_never_index"), nil, 0o644)

	stagingDir, err := os.MkdirTemp(assocDir, ".Block.staging-*")
	if err != nil {
		return fmt.Errorf("create helper staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	stagedApp := filepath.Join(stagingDir, "Block.app")
	if err := os.MkdirAll(filepath.Join(stagedApp, "Contents", "MacOS"), 0o755); err != nil {
		return fmt.Errorf("create staged macos dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(stagedApp, "Contents", "Resources"), 0o755); err != nil {
		return fmt.Errorf("create staged resources dir: %w", err)
	}

	stagedExec := filepath.Join(stagedApp, "Contents", "MacOS", macAssocHelperExecName)
	if err := cloneOrCopyFileAtomic(exePath, stagedExec, 0o755); err != nil {
		return fmt.Errorf("stage helper executable: %w", err)
	}

	if mainAppPath != "" {
		srcIcon := filepath.Join(mainAppPath, "Contents", "Resources", "AppIcon.icns")
		dstIcon := filepath.Join(stagedApp, "Contents", "Resources", "AppIcon.icns")
		if srcStat, err := os.Stat(srcIcon); err == nil {
			if copyErr := copyFileAtomic(srcIcon, dstIcon, 0o644); copyErr == nil {
				_ = os.Chtimes(dstIcon, srcStat.ModTime(), srcStat.ModTime())
			}
		}
	}

	plistBytes := buildMacHelperInfoPlist(appVer, mainAppPath, bareExts)
	if err := writeFileAtomic(filepath.Join(stagedApp, "Contents", "Info.plist"), plistBytes, 0o644); err != nil {
		return fmt.Errorf("write staged Info.plist: %w", err)
	}

	if err := runCommandTimeout(10*time.Second, macCodesignPath,
		"--force", "--sign", "-",
		"--identifier", macAssocHelperBundleID,
		stagedApp,
	); err != nil {
		return fmt.Errorf("codesign staged helper bundle: %w", err)
	}
	if err := runCommandTimeout(10*time.Second, macCodesignPath, "--verify", "--verbose=2", stagedApp); err != nil {
		return fmt.Errorf("verify staged helper bundle: %w", err)
	}

	backupApp := filepath.Join(stagingDir, "Block.previous.app")
	hadExisting := false
	if _, err := os.Stat(helperApp); err == nil {
		if err := os.Rename(helperApp, backupApp); err != nil {
			return fmt.Errorf("move previous helper aside: %w", err)
		}
		hadExisting = true
	}

	if err := os.Rename(stagedApp, helperApp); err != nil {
		if hadExisting {
			_ = os.Rename(backupApp, helperApp)
		}
		return fmt.Errorf("publish helper bundle: %w", err)
	}

	if err := runCommandTimeout(10*time.Second, macLSRegisterPath, "-f", helperApp); err != nil {
		_ = os.RemoveAll(helperApp)
		if hadExisting {
			if os.Rename(backupApp, helperApp) == nil {
				_ = runCommandTimeout(5*time.Second, macLSRegisterPath, "-f", helperApp)
			}
		}
		return fmt.Errorf("lsregister helper bundle: %w", err)
	}

	if firstRun {
		pruneStaleMacHelperRegistrations(helperApp)
	}

	return saveFileAssocState(appDataDir, fileAssocState{
		AppVersion:      appVer,
		AppPath:         mainAppPath,
		ExecutablePath:  exePath,
		ExecutableSize:  exeInfo.Size(),
		ExecutableMtime: exeInfo.ModTime().UnixNano(),
		Extensions:      bareExts,
	})
}

func normalizedMacBundleVersion(version string) string {
	ver := strings.TrimSpace(version)
	if ver == "" || ver == "dev" {
		return "1.0.0"
	}
	return ver
}

func buildMacHelperInfoPlist(version, mainAppPath string, bareExts []string) []byte {
	ver := normalizedMacBundleVersion(version)
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

func cloneOrCopyFileAtomic(src, dst string, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".block-clone-*.tmp")
	if err == nil {
		tmpPath := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if err := runCommandTimeout(5*time.Second, "/bin/cp", "-c", src, tmpPath); err == nil {
			if err := os.Chmod(tmpPath, mode); err == nil {
				if err := os.Rename(tmpPath, dst); err == nil {
					return nil
				}
			}
			_ = os.Remove(tmpPath)
		}
	}
	return copyFileAtomic(src, dst, mode)
}

func copyFileAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".block-copy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
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
