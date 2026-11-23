//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"block/internal/files"
	"block/internal/plugins"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const (
	macRelayExecName      = "block-opener"
	macRelayBundleID      = "dev.eachann.block"
	macRelayLegacyID      = "com.block.notes"
	macRelayDefaultApp    = "/Applications/Block.app"
	macRelayBatchDebounce = 40 * time.Millisecond
	macRelayQuietExit     = 160 * time.Millisecond
	macRelayIdleTimeout   = 1500 * time.Millisecond
	macRelayOpenTimeout   = 10 * time.Second
)

// maybeRunMacOpenRelay handles launches of the per-user file-association helper
// bundle (~/Library/Application Support/Block/Associations/Block.app). It
// receives Finder's kAEOpenDocuments events via mygo without opening a window,
// forwards batches serially to the verified main Block.app, and exits once idle.
func maybeRunMacOpenRelay() bool {
	if len(os.Args) == 0 || filepath.Base(os.Args[0]) != macRelayExecName {
		return false
	}
	for _, arg := range os.Args[1:] {
		if arg == "--prune-if-orphaned" {
			pruneSelfMacRelayIfOrphaned()
			return true
		}
	}

	var (
		mu          sync.Mutex
		pending     []string
		seen        = make(map[string]struct{})
		forwarding  bool
		hadAnyBatch bool
		quitting    bool
		batchTimer  *time.Timer
		quitTimer   *time.Timer
		quitOnce    sync.Once
	)

	normalizePath := func(raw string) string {
		if raw == "" {
			return ""
		}
		// Preserve leading/trailing whitespace in valid macOS filenames; only
		// convert relative paths to absolute so /usr/bin/open never treats a
		// leading '-' filename as an option flag.
		if !filepath.IsAbs(raw) {
			if abs, err := filepath.Abs(raw); err == nil && abs != "" {
				return abs
			}
		}
		return raw
	}

	quitApp := func() {
		quitOnce.Do(func() {
			mygo.App.Quit()
			time.AfterFunc(2*time.Second, func() {
				os.Exit(0)
			})
		})
	}

	var pumpLocked func()
	pumpLocked = func() {
		if forwarding {
			return
		}
		if len(pending) == 0 {
			if hadAnyBatch {
				if quitTimer != nil {
					quitTimer.Stop()
				}
				quitTimer = time.AfterFunc(macRelayQuietExit, func() {
					mu.Lock()
					if forwarding || len(pending) > 0 || quitting {
						mu.Unlock()
						return
					}
					quitting = true
					mu.Unlock()
					quitApp()
				})
			}
			return
		}
		if quitTimer != nil {
			quitTimer.Stop()
			quitTimer = nil
		}
		batch := append([]string(nil), pending...)
		pending = nil
		forwarding = true
		hadAnyBatch = true

		go func(filesToOpen []string) {
			forwardPathsToMainMacBlock(filesToOpen)
			mu.Lock()
			forwarding = false
			pumpLocked()
			mu.Unlock()
		}(batch)
	}

	enqueuePath := func(raw string) {
		p := normalizePath(raw)
		if p == "" {
			return
		}
		mu.Lock()
		if _, dup := seen[p]; dup {
			mu.Unlock()
			return
		}
		seen[p] = struct{}{}
		if quitting {
			mu.Unlock()
			forwardPathsToMainMacBlock([]string{p})
			return
		}
		pending = append(pending, p)
		if quitTimer != nil {
			quitTimer.Stop()
			quitTimer = nil
		}
		if batchTimer != nil {
			batchTimer.Stop()
		}
		batchTimer = time.AfterFunc(macRelayBatchDebounce, func() {
			mu.Lock()
			batchTimer = nil
			pumpLocked()
			mu.Unlock()
		})
		mu.Unlock()
	}

	endOfFlags := false
	for _, arg := range os.Args[1:] {
		if arg == "" {
			continue
		}
		if !endOfFlags {
			if arg == "--" {
				endOfFlags = true
				continue
			}
			if strings.HasPrefix(arg, "-psn_") {
				continue
			}
		}
		enqueuePath(arg)
	}

	mygo.App.OnOpenFile(func(path string) {
		enqueuePath(path)
	})

	mygo.App.WhenReady(func() {
		// MyGo's Run() sets Regular activation policy by default; restore
		// Accessory policy on the main thread so the relay stays out of Dock.
		setMacActivationPolicyAccessory()

		mu.Lock()
		if len(pending) == 0 && !forwarding && !hadAnyBatch && batchTimer == nil {
			quitTimer = time.AfterFunc(macRelayIdleTimeout, func() {
				mu.Lock()
				if len(pending) > 0 || forwarding || hadAnyBatch || quitting {
					mu.Unlock()
					return
				}
				quitting = true
				mu.Unlock()
				go func() {
					forwardPathsToMainMacBlock(nil)
					quitApp()
				}()
			})
		}
		mu.Unlock()
	})

	_ = mygo.App.Run()
	return true
}

func setMacActivationPolicyAccessory() {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return
	}
	nsAppCls := objc.ID(objc.GetClass("NSApplication"))
	if nsAppCls == 0 {
		return
	}
	nsApp := nsAppCls.Send(objc.RegisterName("sharedApplication"))
	if nsApp == 0 {
		return
	}
	// NSApplicationActivationPolicyAccessory = 1
	nsApp.Send(objc.RegisterName("setActivationPolicy:"), uintptr(1))
}

var (
	macRelayFSGetPathOnce sync.Once
	macRelayFSGetPathFn   func(buf *byte, bufSize uintptr, fsid *[2]int32, objID uint64) int64
)

func isMacRelayTrashPath(p string) bool {
	if p == "" {
		return false
	}
	clean := filepath.Clean(p)
	return strings.Contains(clean, "/.Trash/") || strings.HasSuffix(clean, "/.Trash") ||
		strings.Contains(clean, "/.Trashes/") || strings.HasSuffix(clean, "/.Trashes")
}

func resolveMacRelayMovedPath(fsidStr, fileIDStr string) string {
	fsidStr = strings.TrimSpace(fsidStr)
	fileIDStr = strings.TrimSpace(fileIDStr)
	if fsidStr == "" || fileIDStr == "" {
		return ""
	}
	parts := strings.Split(fsidStr, ":")
	if len(parts) != 2 {
		return ""
	}
	v0, err0 := strconv.ParseInt(parts[0], 10, 32)
	v1, err1 := strconv.ParseInt(parts[1], 10, 32)
	ino, err2 := strconv.ParseUint(fileIDStr, 10, 64)
	if err0 != nil || err1 != nil || err2 != nil || ino == 0 {
		return ""
	}
	macRelayFSGetPathOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		sym, err := purego.Dlsym(lib, "fsgetpath")
		if err != nil {
			return
		}
		purego.RegisterFunc(&macRelayFSGetPathFn, sym)
	})
	if macRelayFSGetPathFn == nil {
		return ""
	}
	fsid := [2]int32{int32(v0), int32(v1)}
	var buf [4096]byte
	n := macRelayFSGetPathFn(&buf[0], uintptr(len(buf)), &fsid, ino)
	if n <= 0 {
		return ""
	}
	if int(n) > len(buf) {
		n = int64(len(buf))
	}
	end := bytes.IndexByte(buf[:n], 0)
	if end < 0 {
		end = int(n)
	}
	return strings.TrimSpace(string(buf[:end]))
}

// resolveMacRelayTargetFromPlist resolves the exact target installation recorded
// in a helper bundle's Info.plist. When BlockMainAppPath or BlockMainExecPath
// is present (pinned == true), the relay preserves installation identity: if
// the bundle moved on the same volume, it follows its file ID via fsgetpath,
// and if the installation was deleted or trashed, it refuses to hand off files
// to any other Block copy.
func resolveMacRelayTargetFromPlist(raw []byte) (appPath string, execPath string, pinned bool) {
	recordedApp := extractPlistStringValue(raw, "BlockMainAppPath")
	recordedExec := extractPlistStringValue(raw, "BlockMainExecPath")
	fsidStr := extractPlistStringValue(raw, "BlockMainAppFSID")
	fileIDStr := extractPlistStringValue(raw, "BlockMainAppFileID")

	if recordedApp != "" {
		if !isMacRelayTrashPath(recordedApp) && isVerifiedMainBlockBundle(recordedApp) {
			return recordedApp, "", true
		}
		if moved := resolveMacRelayMovedPath(fsidStr, fileIDStr); moved != "" && !isMacRelayTrashPath(moved) && isVerifiedMainBlockBundle(moved) {
			return moved, "", true
		}
		return "", "", true
	}
	if recordedExec != "" {
		if !isMacRelayTrashPath(recordedExec) {
			if st, err := os.Stat(recordedExec); err == nil && !st.IsDir() {
				return "", recordedExec, true
			}
		}
		if moved := resolveMacRelayMovedPath(fsidStr, fileIDStr); moved != "" && !isMacRelayTrashPath(moved) {
			if st, err := os.Stat(moved); err == nil && !st.IsDir() {
				return "", moved, true
			}
		}
		return "", "", true
	}
	return "", "", false
}

func forwardPathsToMainMacBlock(paths []string) {
	mainApp, mainExec, pinned := resolveMainMacBlockTarget()
	if mainApp != "" {
		if err := runMacOpen(append([]string{"-a", mainApp}, paths...)...); err != nil {
			files.LogWarnf("mac open relay -a %s failed: %v", mainApp, err)
		}
		return
	}
	if mainExec != "" {
		cmd := exec.Command(mainExec, paths...)
		if err := cmd.Start(); err != nil {
			files.LogWarnf("mac open relay exec %s failed: %v", mainExec, err)
		}
		return
	}
	if pinned {
		// Target installation was deleted or moved to Trash; do not silently
		// forward files to a different Block installation. If the target is not
		// merely on a temporarily unmounted external volume, unregister and
		// remove this orphaned external association helper immediately.
		if !isMacRelayTargetOnUnmountedVolume() {
			selfCleanupOrphanedMacRelay()
		}
		files.LogWarnf("mac open relay target installation no longer available; refusing cross-install fallback")
		return
	}
	if err := runMacOpen(append([]string{"-b", macRelayBundleID}, paths...)...); err != nil {
		files.LogWarnf("mac open relay -b %s failed: %v", macRelayBundleID, err)
	}
}

func runMacOpen(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), macRelayOpenTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/open", args...).Run()
}

func isMacRelayUnmountedVolumePath(p string) bool {
	clean := filepath.Clean(strings.TrimSpace(p))
	if !strings.HasPrefix(clean, "/Volumes/") {
		return false
	}
	rest := strings.TrimPrefix(clean, "/Volumes/")
	vol := rest
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		vol = rest[:idx]
	}
	if vol == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/Volumes", vol))
	return os.IsNotExist(err)
}

func readSelfMacRelayPlist() (helperAppPath string, raw []byte) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	// Expected layout: <appDataDir>/Associations/<scopeKey>/Block.app/Contents/MacOS/block-opener
	contentsDir := filepath.Dir(filepath.Dir(exe))
	helperAppPath = filepath.Dir(contentsDir)
	raw, err = os.ReadFile(filepath.Join(contentsDir, "Info.plist"))
	if err != nil {
		return helperAppPath, nil
	}
	return helperAppPath, raw
}

func isMacRelayTargetOnUnmountedVolume() bool {
	_, raw := readSelfMacRelayPlist()
	if len(raw) == 0 {
		return false
	}
	target := extractPlistStringValue(raw, "BlockMainAppPath")
	if target == "" {
		target = extractPlistStringValue(raw, "BlockMainExecPath")
	}
	return target != "" && isMacRelayUnmountedVolumePath(target)
}

func pruneSelfMacRelayIfOrphaned() {
	_, raw := readSelfMacRelayPlist()
	if len(raw) == 0 {
		return
	}
	target := extractPlistStringValue(raw, "BlockMainAppPath")
	if target == "" {
		target = extractPlistStringValue(raw, "BlockMainExecPath")
	}
	if target != "" && isMacRelayUnmountedVolumePath(target) {
		return
	}
	appPath, execPath, pinned := resolveMacRelayTargetFromPlist(raw)
	if !pinned || appPath != "" || execPath != "" {
		return
	}
	// If the target disappeared without moving into Trash, debounce briefly so
	// an in-flight in-place upgrade swap is never mistaken for an uninstall.
	fsidStr := extractPlistStringValue(raw, "BlockMainAppFSID")
	fileIDStr := extractPlistStringValue(raw, "BlockMainAppFileID")
	moved := resolveMacRelayMovedPath(fsidStr, fileIDStr)
	if moved == "" || !isMacRelayTrashPath(moved) {
		time.Sleep(500 * time.Millisecond)
		appPath2, execPath2, pinned2 := resolveMacRelayTargetFromPlist(raw)
		if !pinned2 || appPath2 != "" || execPath2 != "" {
			return
		}
	}
	selfCleanupOrphanedMacRelay()
}

func selfCleanupOrphanedMacRelay() {
	helperApp, raw := readSelfMacRelayPlist()
	if helperApp == "" || len(raw) == 0 {
		return
	}
	bundleID := extractPlistStringValue(raw, "CFBundleIdentifier")
	if bundleID == "" {
		return
	}
	// Derive appDataDir from <appDataDir>/Associations/<scopeKey>/Block.app
	scopeDir := filepath.Dir(helperApp)
	assocDir := filepath.Dir(scopeDir)
	if filepath.Base(helperApp) != "Block.app" || filepath.Base(assocDir) != "Associations" {
		return
	}
	appDataDir := filepath.Dir(assocDir)
	if err := plugins.CleanupOrphanedMacRelayScope(appDataDir, bundleID, helperApp); err != nil {
		files.LogWarnf("mac open relay self-cleanup failed: %v", err)
	}
}

func resolveMainMacBlockTarget() (appPath string, execPath string, pinned bool) {
	if exe, err := os.Executable(); err == nil {
		plistPath := filepath.Join(filepath.Dir(exe), "..", "Info.plist")
		if raw, err := os.ReadFile(plistPath); err == nil {
			appPath, execPath, pinned = resolveMacRelayTargetFromPlist(raw)
			if pinned {
				return appPath, execPath, true
			}
		}
	}
	if isVerifiedMainBlockBundle(macRelayDefaultApp) {
		return macRelayDefaultApp, "", false
	}
	return "", "", false
}

// isVerifiedMainBlockBundle ensures the target path is actually a Block.app
// bundle matching Block's CFBundleIdentifier before forwarding user files.
func isVerifiedMainBlockBundle(bundlePath string) bool {
	if bundlePath == "" {
		return false
	}
	st, err := os.Stat(bundlePath)
	if err != nil || !st.IsDir() {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(bundlePath, "Contents", "Info.plist"))
	if err != nil {
		return false
	}
	id := extractPlistStringValue(raw, "CFBundleIdentifier")
	return id == macRelayBundleID || id == macRelayLegacyID
}

func extractPlistStringValue(raw []byte, targetKey string) string {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var expectingVal bool
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "key":
			var key string
			if err := dec.DecodeElement(&key, &se); err == nil {
				expectingVal = (key == targetKey)
			}
		case "string":
			if expectingVal {
				var val string
				if err := dec.DecodeElement(&val, &se); err == nil {
					return strings.TrimSpace(val)
				}
				return ""
			}
		default:
			expectingVal = false
		}
	}
}
