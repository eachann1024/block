//go:build darwin

package main

import (
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/files"
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

func forwardPathsToMainMacBlock(paths []string) {
	mainApp := resolveMainMacBlockAppPath()
	if mainApp != "" {
		if err := runMacOpen(append([]string{"-a", mainApp}, paths...)...); err == nil {
			return
		} else {
			files.LogWarnf("mac open relay -a %s failed: %v", mainApp, err)
		}
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

func resolveMainMacBlockAppPath() string {
	if exe, err := os.Executable(); err == nil {
		plistPath := filepath.Join(filepath.Dir(exe), "..", "Info.plist")
		if raw, err := os.ReadFile(plistPath); err == nil {
			if p := extractPlistStringValue(raw, "BlockMainAppPath"); p != "" && isVerifiedMainBlockBundle(p) {
				return p
			}
		}
	}
	if isVerifiedMainBlockBundle(macRelayDefaultApp) {
		return macRelayDefaultApp
	}
	return ""
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
