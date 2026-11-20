//go:build darwin

package main

import (
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const (
	macRelayExecName      = "block-opener"
	macRelayBundleID      = "dev.eachann.block"
	macRelayDefaultApp    = "/Applications/Block.app"
	macRelayBatchDebounce = 120 * time.Millisecond
	macRelayIdleTimeout   = 1500 * time.Millisecond
)

// maybeRunMacOpenRelay handles launches of the per-user file-association helper
// bundle (~/Library/Application Support/Block/Associations/Block.app). It
// receives Finder's kAEOpenDocuments events via mygo without opening a window,
// forwards the file paths to the main Block.app, and exits immediately.
func maybeRunMacOpenRelay() bool {
	if len(os.Args) == 0 || filepath.Base(os.Args[0]) != macRelayExecName {
		return false
	}

	var (
		mu       sync.Mutex
		paths    []string
		seen     = make(map[string]struct{})
		flushed  bool
		timer    *time.Timer
		quitOnce sync.Once
	)

	addPath := func(raw string) bool {
		p := strings.TrimSpace(raw)
		if p == "" {
			return false
		}
		if abs, err := filepath.Abs(p); err == nil && abs != "" {
			p = abs
		}
		if _, dup := seen[p]; dup {
			return false
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
		return true
	}

	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		addPath(arg)
	}

	quitApp := func() {
		quitOnce.Do(func() {
			mygo.App.Quit()
			time.AfterFunc(2*time.Second, func() {
				os.Exit(0)
			})
		})
	}

	forwardNow := func() {
		mu.Lock()
		if flushed {
			mu.Unlock()
			return
		}
		flushed = true
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		batch := append([]string(nil), paths...)
		mu.Unlock()

		forwardPathsToMainMacBlock(batch)
		quitApp()
	}

	armTimer := func(d time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(d, forwardNow)
	}

	mygo.App.OnOpenFile(func(path string) {
		mu.Lock()
		if flushed {
			mu.Unlock()
			if strings.TrimSpace(path) != "" {
				forwardPathsToMainMacBlock([]string{path})
			}
			return
		}
		if addPath(path) {
			armTimer(macRelayBatchDebounce)
		}
		mu.Unlock()
	})

	mygo.App.WhenReady(func() {
		mu.Lock()
		if !flushed {
			if len(paths) > 0 {
				armTimer(macRelayBatchDebounce)
			} else if timer == nil {
				armTimer(macRelayIdleTimeout)
			}
		}
		mu.Unlock()
	})

	_ = mygo.App.Run()
	return true
}

func forwardPathsToMainMacBlock(paths []string) {
	mainApp := resolveMainMacBlockAppPath()
	var args []string
	if mainApp != "" {
		args = append(args, "-a", mainApp)
	} else {
		args = append(args, "-b", macRelayBundleID)
	}
	args = append(args, paths...)
	_ = exec.Command("/usr/bin/open", args...).Run()
}

func resolveMainMacBlockAppPath() string {
	if exe, err := os.Executable(); err == nil {
		plistPath := filepath.Join(filepath.Dir(exe), "..", "Info.plist")
		if raw, err := os.ReadFile(plistPath); err == nil {
			if p := extractPlistStringValue(raw, "BlockMainAppPath"); p != "" {
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					return p
				}
			}
		}
	}
	if st, err := os.Stat(macRelayDefaultApp); err == nil && st.IsDir() {
		return macRelayDefaultApp
	}
	return ""
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
