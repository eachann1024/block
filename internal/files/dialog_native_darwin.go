//go:build darwin

package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"
)

var errDarwinNativePanelFailed = errors.New("native macOS file panel unavailable")

type darwinFileFilter struct {
	Extensions []string `json:"extensions,omitempty"`
	MIMETypes  []string `json:"mimeTypes,omitempty"`
}

type darwinPanelRequest struct {
	ID                         int64              `json:"id,omitempty"`
	Op                         string             `json:"op"` // "open", "save", "cancel"
	Title                      string             `json:"title,omitempty"`
	Message                    string             `json:"message,omitempty"`
	ButtonLabel                string             `json:"buttonLabel,omitempty"`
	NameFieldLabel             string             `json:"nameFieldLabel,omitempty"`
	DefaultPath                string             `json:"defaultPath,omitempty"`
	Directory                  bool               `json:"directory,omitempty"`
	Multiple                   bool               `json:"multiple,omitempty"`
	ShowHiddenFiles            bool               `json:"showHiddenFiles,omitempty"`
	CreateDirectories          bool               `json:"createDirectories,omitempty"`
	TreatPackagesAsDirectories bool               `json:"treatPackagesAsDirectories,omitempty"`
	WindowLevel                int                `json:"windowLevel,omitempty"`
	Filters                    []darwinFileFilter `json:"filters,omitempty"`
}

func toDarwinFileFilters(filters []mygo.FileFilter) []darwinFileFilter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]darwinFileFilter, 0, len(filters))
	for _, f := range filters {
		if len(f.Extensions) == 0 && len(f.MIMETypes) == 0 {
			continue
		}
		out = append(out, darwinFileFilter{
			Extensions: append([]string(nil), f.Extensions...),
			MIMETypes:  append([]string(nil), f.MIMETypes...),
		})
	}
	return out
}

func openOptsToDarwinRequest(opts mygo.OpenDialogOptions) darwinPanelRequest {
	return darwinPanelRequest{
		Op:                         "open",
		Title:                      opts.Title,
		Message:                    opts.Message,
		ButtonLabel:                opts.ButtonLabel,
		DefaultPath:                opts.DefaultPath,
		Directory:                  opts.Directory,
		Multiple:                   opts.Multiple,
		ShowHiddenFiles:            opts.ShowHiddenFiles,
		CreateDirectories:          opts.CreateDirectories,
		TreatPackagesAsDirectories: opts.TreatPackagesAsDirectories,
		Filters:                    toDarwinFileFilters(opts.Filters),
	}
}

func saveOptsToDarwinRequest(opts mygo.SaveDialogOptions) darwinPanelRequest {
	return darwinPanelRequest{
		Op:                         "save",
		Title:                      opts.Title,
		Message:                    opts.Message,
		ButtonLabel:                opts.ButtonLabel,
		NameFieldLabel:             opts.NameFieldLabel,
		DefaultPath:                opts.DefaultPath,
		ShowHiddenFiles:            opts.ShowHiddenFiles,
		CreateDirectories:          opts.CreateDirectories,
		TreatPackagesAsDirectories: opts.TreatPackagesAsDirectories,
		Filters:                    toDarwinFileFilters(opts.Filters),
	}
}

var (
	darwinAppKitOnce    sync.Once
	darwinCGIdleOnce    sync.Once
	darwinCGSecondsIdle func(stateID int32, eventType uint32) float64
)

func ensureDarwinAppKitLoaded() {
	darwinAppKitOnce.Do(func() {
		_, _ = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
		_, _ = purego.Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
	})
}

func darwinSecondsSinceLastKeyOrClick() (float64, bool) {
	darwinCGIdleOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY)
		if err != nil {
			return
		}
		sym, err := purego.Dlsym(lib, "CGEventSourceSecondsSinceLastEventType")
		if err != nil {
			return
		}
		purego.RegisterFunc(&darwinCGSecondsIdle, sym)
	})
	if darwinCGSecondsIdle == nil {
		return 0, false
	}
	// kCGEventSourceStateCombinedSessionState = 0
	// kCGEventLeftMouseDown = 1, kCGEventKeyDown = 10
	keyIdle := darwinCGSecondsIdle(0, 10)
	clickIdle := darwinCGSecondsIdle(0, 1)
	if clickIdle < keyIdle {
		return clickIdle, true
	}
	return keyIdle, true
}

type cachedDarwinPanel struct {
	panel                 objc.ID
	defaultTitle          string
	defaultPrompt         string
	defaultNameFieldLabel string
	defaultLevel          int
	shownOnce             bool
}

// Main-thread-only cached AppKit panels & UTType cache.
var (
	darwinCachedOpenPanel cachedDarwinPanel
	darwinCachedSavePanel cachedDarwinPanel
	darwinUTTypeCache     = map[string]objc.ID{}
	darwinPanelActive     bool
	darwinPrewarmOnce     sync.Once
)

var darwinMIMEKindUTIs = map[string]string{
	"image/*": "public.image",
	"audio/*": "public.audio",
	"video/*": "public.movie",
	"text/*":  "public.text",
	"font/*":  "public.font",
}

var darwinCommonExtensions = []string{
	"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif",
	"heic", "heif", "tiff", "mp4", "mov", "m4v", "webm", "mkv", "avi",
	"md", "markdown", "pdf", "html", "txt", "json", "zip",
	"xmind", "mm", "opml",
}

func pickerSend(receiver objc.ID, selector string, args ...any) objc.ID {
	return objc.Send[objc.ID](receiver, objc.RegisterName(selector), args...)
}

func pickerSendBool(receiver objc.ID, selector string, args ...any) bool {
	return objc.Send[bool](receiver, objc.RegisterName(selector), args...)
}

func pickerRespondsTo(receiver objc.ID, selector string) bool {
	if receiver == 0 {
		return false
	}
	return objc.Send[bool](receiver, objc.RegisterName("respondsToSelector:"), objc.RegisterName(selector))
}

func pickerString(value string) objc.ID {
	return pickerSend(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", value)
}

func pickerGoString(nsStr objc.ID) string {
	if nsStr == 0 {
		return ""
	}
	ptr := objc.Send[*byte](nsStr, objc.RegisterName("UTF8String"))
	if ptr == nil {
		return ""
	}
	return unix.BytePtrToString(ptr)
}

func pickerURLPath(url objc.ID) string {
	if url == 0 {
		return ""
	}
	return pickerGoString(pickerSend(url, "path"))
}

func darwinWithAutoreleasePool(fn func()) {
	pool := pickerSend(objc.ID(objc.GetClass("NSAutoreleasePool")), "new")
	defer pickerSend(pool, "drain")
	fn()
}

// darwinWarmSavePanelService triggers AppKit's non-blocking async XPC pre-warm
// for com.apple.appkit.xpc.openAndSavePanelService (~0.02ms on main thread).
func darwinWarmSavePanelService() {
	ensureDarwinAppKitLoaded()
	cls := objc.ID(objc.GetClass("NSSavePanel"))
	if cls != 0 && pickerRespondsTo(cls, "_warmUp") {
		pickerSend(cls, "_warmUp")
	}
}

func darwinPanelHealthy(panel objc.ID) bool {
	if panel == 0 {
		return false
	}
	if pickerRespondsTo(panel, "remoteView") {
		rv := pickerSend(panel, "remoteView")
		if rv == 0 {
			return false
		}
		if pickerRespondsTo(rv, "isValid") && !pickerSendBool(rv, "isValid") {
			return false
		}
		if pickerRespondsTo(rv, "viewServiceProcessIdentifier") {
			pid := objc.Send[int32](rv, objc.RegisterName("viewServiceProcessIdentifier"))
			if pid <= 0 {
				return false
			}
			// Catch an XPC service crash even before NSRunLoop processes the disconnect.
			if err := unix.Kill(int(pid), 0); errors.Is(err, unix.ESRCH) {
				return false
			}
		}
	}
	return true
}

func darwinInvalidateCachedPanel(save bool, expected objc.ID) {
	slot := &darwinCachedOpenPanel
	if save {
		slot = &darwinCachedSavePanel
	}
	if slot.panel == 0 {
		return
	}
	if expected != 0 && slot.panel != expected {
		return
	}
	pickerSend(slot.panel, "release")
	*slot = cachedDarwinPanel{}
}

func darwinEnsureCachedPanel(save bool) (*cachedDarwinPanel, error) {
	ensureDarwinAppKitLoaded()
	slot := &darwinCachedOpenPanel
	if save {
		slot = &darwinCachedSavePanel
	}
	if slot.panel != 0 {
		if darwinPanelHealthy(slot.panel) {
			return slot, nil
		}
		darwinInvalidateCachedPanel(save, slot.panel)
	}

	var panel objc.ID
	if save {
		panel = pickerSend(objc.ID(objc.GetClass("NSSavePanel")), "savePanel")
	} else {
		panel = pickerSend(objc.ID(objc.GetClass("NSOpenPanel")), "openPanel")
	}
	if !darwinPanelHealthy(panel) {
		return nil, errDarwinNativePanelFailed
	}
	pickerSend(panel, "retain")
	slot.panel = panel
	slot.defaultTitle = pickerGoString(pickerSend(panel, "title"))
	slot.defaultPrompt = pickerGoString(pickerSend(panel, "prompt"))
	slot.defaultLevel = objc.Send[int](panel, objc.RegisterName("level"))
	if save {
		slot.defaultNameFieldLabel = pickerGoString(pickerSend(panel, "nameFieldLabel"))
	}
	slot.shownOnce = false
	return slot, nil
}

func darwinPrewarmCommonUTTypes() {
	ensureDarwinAppKitLoaded()
	utClass := objc.ID(objc.GetClass("UTType"))
	if utClass == 0 {
		return
	}
	for _, ext := range darwinCommonExtensions {
		e := ext
		_ = darwinCachedUTType("ext:"+e, func() objc.ID {
			return pickerSend(utClass, "typeWithFilenameExtension:", pickerString(e))
		})
	}
}

func resolveDarwinDefaultLocation(raw string, isSave bool) (dir, name string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	clean := filepath.Clean(trimmed)
	if !filepath.IsAbs(clean) {
		if isSave {
			return "", filepath.Base(clean)
		}
		return "", ""
	}
	if info, err := os.Stat(clean); err == nil && info.IsDir() {
		return clean, ""
	}
	dir = resolveExistingDir(filepath.Dir(clean))
	if isSave {
		name = filepath.Base(clean)
	}
	return dir, name
}

func darwinCachedUTType(key string, lookup func() objc.ID) objc.ID {
	if cached, ok := darwinUTTypeCache[key]; ok {
		return cached
	}
	t := lookup()
	if t != 0 {
		pickerSend(t, "retain")
	}
	darwinUTTypeCache[key] = t
	return t
}

func darwinApplyPanelFilters(panel objc.ID, filters []darwinFileFilter, directory bool) {
	var exts, mimes []string
	unrestricted := directory || len(filters) == 0
	if !unrestricted {
		for _, f := range filters {
			for _, e := range f.Extensions {
				el := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
				if el == "*" {
					unrestricted = true
					break
				}
				if el != "" {
					exts = append(exts, el)
				}
			}
			if unrestricted {
				break
			}
			for _, m := range f.MIMETypes {
				ml := strings.ToLower(strings.TrimSpace(m))
				if ml == "*" || ml == "*/*" {
					unrestricted = true
					break
				}
				if ml != "" {
					mimes = append(mimes, ml)
				}
			}
			if unrestricted {
				break
			}
		}
		if len(exts) == 0 && len(mimes) == 0 {
			unrestricted = true
		}
	}

	utClass := objc.ID(objc.GetClass("UTType"))
	if utClass != 0 && pickerRespondsTo(panel, "setAllowedContentTypes:") {
		if unrestricted {
			empty := pickerSend(objc.ID(objc.GetClass("NSArray")), "array")
			pickerSend(panel, "setAllowedContentTypes:", empty)
			return
		}
		arr := pickerSend(objc.ID(objc.GetClass("NSMutableArray")), "array")
		for _, e := range exts {
			ext := e
			t := darwinCachedUTType("ext:"+ext, func() objc.ID {
				res := pickerSend(utClass, "typeWithFilenameExtension:", pickerString(ext))
				if res == 0 {
					if dot := strings.LastIndexByte(ext, '.'); dot >= 0 && dot+1 < len(ext) {
						res = pickerSend(utClass, "typeWithFilenameExtension:", pickerString(ext[dot+1:]))
					}
				}
				return res
			})
			if t != 0 {
				pickerSend(arr, "addObject:", t)
			}
		}
		for _, m := range mimes {
			mime := m
			t := darwinCachedUTType("mime:"+mime, func() objc.ID {
				if kind := darwinMIMEKindUTIs[mime]; kind != "" {
					return pickerSend(utClass, "typeWithIdentifier:", pickerString(kind))
				}
				return pickerSend(utClass, "typeWithMIMEType:", pickerString(mime))
			})
			if t != 0 {
				pickerSend(arr, "addObject:", t)
			}
		}
		pickerSend(panel, "setAllowedContentTypes:", arr)
		return
	}

	// Pre-macOS 11 fallback using allowedFileTypes.
	if unrestricted {
		pickerSend(panel, "setAllowedFileTypes:", objc.ID(0))
		return
	}
	arr := pickerSend(objc.ID(objc.GetClass("NSMutableArray")), "array")
	for _, e := range exts {
		pickerSend(arr, "addObject:", pickerString(e))
	}
	for _, m := range mimes {
		if kind := darwinMIMEKindUTIs[m]; kind != "" {
			pickerSend(arr, "addObject:", pickerString(kind))
		}
	}
	if objc.Send[uint](arr, objc.RegisterName("count")) != 0 {
		pickerSend(panel, "setAllowedFileTypes:", arr)
	} else {
		pickerSend(panel, "setAllowedFileTypes:", objc.ID(0))
	}
}

func darwinConfigurePanel(cp *cachedDarwinPanel, req darwinPanelRequest) {
	panel := cp.panel
	isSave := req.Op == "save"

	if !isSave {
		pickerSend(panel, "setCanChooseFiles:", !req.Directory)
		pickerSend(panel, "setCanChooseDirectories:", req.Directory)
		pickerSend(panel, "setAllowsMultipleSelection:", req.Multiple)
	} else {
		label := req.NameFieldLabel
		if label == "" {
			label = cp.defaultNameFieldLabel
		}
		if label != "" {
			pickerSend(panel, "setNameFieldLabel:", pickerString(label))
		}
	}

	title := req.Title
	if title == "" {
		title = cp.defaultTitle
	}
	pickerSend(panel, "setTitle:", pickerString(title))
	if req.Message == "" {
		pickerSend(panel, "setMessage:", objc.ID(0))
	} else {
		pickerSend(panel, "setMessage:", pickerString(req.Message))
	}

	prompt := req.ButtonLabel
	if prompt == "" {
		prompt = cp.defaultPrompt
	}
	if prompt != "" {
		pickerSend(panel, "setPrompt:", pickerString(prompt))
	}

	pickerSend(panel, "setShowsHiddenFiles:", req.ShowHiddenFiles)
	pickerSend(panel, "setCanCreateDirectories:", req.CreateDirectories)
	pickerSend(panel, "setTreatsFilePackagesAsDirectories:", req.TreatPackagesAsDirectories)

	darwinApplyPanelFilters(panel, req.Filters, req.Directory)

	dir, name := resolveDarwinDefaultLocation(req.DefaultPath, isSave)
	if dir != "" {
		url := pickerSend(objc.ID(objc.GetClass("NSURL")), "fileURLWithPath:isDirectory:", pickerString(dir), true)
		if url != 0 {
			pickerSend(panel, "setDirectoryURL:", url)
		}
	}
	if isSave {
		pickerSend(panel, "setNameFieldStringValue:", pickerString(name))
	}
}

func darwinExtractPanelPaths(panel objc.ID, isSave bool) []string {
	if isSave {
		if p := pickerURLPath(pickerSend(panel, "URL")); p != "" {
			return []string{p}
		}
		return nil
	}
	urls := pickerSend(panel, "URLs")
	if urls == 0 {
		return nil
	}
	count := objc.Send[uint](urls, objc.RegisterName("count"))
	if count == 0 {
		return nil
	}
	paths := make([]string, 0, count)
	for i := uint(0); i < count; i++ {
		if p := pickerURLPath(pickerSend(urls, "objectAtIndex:", i)); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func darwinActivateSelf() {
	app := pickerSend(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	if app == 0 {
		return
	}
	if pickerRespondsTo(app, "activateIgnoringOtherApps:") {
		pickerSend(app, "activateIgnoringOtherApps:", true)
	}
	if pickerRespondsTo(app, "activate") {
		pickerSend(app, "activate")
	}
}

func darwinYieldActivationToPID(pid int32) {
	if pid <= 0 {
		return
	}
	app := pickerSend(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	if app == 0 || !pickerRespondsTo(app, "yieldActivationToApplication:") {
		return
	}
	runCls := objc.ID(objc.GetClass("NSRunningApplication"))
	if runCls == 0 {
		return
	}
	target := pickerSend(runCls, "runningApplicationWithProcessIdentifier:", pid)
	if target != 0 {
		pickerSend(app, "yieldActivationToApplication:", target)
	}
}

type darwinPanelOutcome struct {
	paths   []string
	crashed bool
}

// showDarwinNativePanel presents the cached/reused NSOpenPanel or NSSavePanel in-process.
// Returns errDarwinNativePanelFailed if AppKit/ViewBridge cannot provide a healthy panel.
func showDarwinNativePanel(ctx context.Context, win *mygo.Window, req darwinPanelRequest, allowed func() bool) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if allowed != nil && win == nil {
		return nil, context.Canceled
	}
	isSave := req.Op == "save"

	for attempt := 0; attempt < 2; attempt++ {
		done := make(chan darwinPanelOutcome, 1)
		var started bool
		var setupErr error

		mygo.RunOnMain(func() {
			darwinWithAutoreleasePool(func() {
				if ctx.Err() != nil || (allowed != nil && !allowed()) {
					setupErr = context.Canceled
					return
				}
				var parent objc.ID
				if win != nil {
					parent = objc.ID(win.NativeHandle())
				}
				if allowed != nil && parent == 0 {
					setupErr = ErrFileDialogUnavailable
					return
				}

				cp, err := darwinEnsureCachedPanel(isSave)
				if err != nil {
					setupErr = err
					return
				}
				darwinConfigurePanel(cp, req)

				if ctx.Err() != nil || (allowed != nil && !allowed()) {
					setupErr = context.Canceled
					return
				}

				panel := cp.panel
				darwinPanelActive = true
				active := true
				stop := context.AfterFunc(ctx, func() {
					mygo.RunOnMain(func() {
						if active {
							pickerSend(panel, "cancel:", objc.ID(0))
						}
					})
				})

				completion := objc.NewBlock(func(_ objc.Block, response int) {
					darwinWithAutoreleasePool(func() {
						active = false
						darwinPanelActive = false
						stop()
						var outcome darwinPanelOutcome
						if response == 1 && ctx.Err() == nil {
							outcome.paths = darwinExtractPanelPaths(panel, isSave)
						}
						healthy := darwinPanelHealthy(panel)
						if !healthy {
							darwinInvalidateCachedPanel(isSave, panel)
						}
						if response != 1 && !healthy && ctx.Err() == nil {
							outcome.crashed = true
						}
						done <- outcome
					})
				})
				defer completion.Release()

				started = true
				useSheet := parent != 0 && (allowed != nil || pickerSendBool(parent, "isVisible"))
				if useSheet {
					pickerSend(panel, "beginSheetModalForWindow:completionHandler:", parent, completion)
				} else {
					targetLevel := cp.defaultLevel
					if parent != 0 {
						if parentLevel := objc.Send[int](parent, objc.RegisterName("level")); parentLevel > targetLevel {
							targetLevel = parentLevel + 1
						}
					}
					pickerSend(panel, "setLevel:", targetLevel)
					darwinActivateSelf()
					if !cp.shownOnce {
						pickerSend(panel, "center")
					}
					pickerSend(panel, "beginWithCompletionHandler:", completion)
					pickerSend(panel, "makeKeyAndOrderFront:", objc.ID(0))
				}
				cp.shownOnce = true
			})
		})

		if !started {
			if setupErr == nil {
				setupErr = context.Canceled
			}
			return nil, setupErr
		}
		outcome := <-done
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if outcome.crashed {
			if attempt == 0 && darwinBundleIntact() {
				continue
			}
			return nil, errDarwinNativePanelFailed
		}
		return outcome.paths, nil
	}
	return nil, errDarwinNativePanelFailed
}

// PrewarmFileDialogs eliminates cold-start latency on macOS file pickers:
//  1. Immediately triggers AppKit's non-blocking [NSSavePanel _warmUp] (~0.02ms)
//     so launchd boots openAndSavePanelService in the background while Block loads.
//  2. Starts a kernel kqueue watcher on the running bundle so hot in-place
//     rebuilds immediately pre-warm the out-of-process helper before any click.
//  3. After startup settles and the user pauses input briefly, pre-creates and
//     retains the cached NSOpenPanel + common UTTypes so the very first click
//     opens instantaneously.
func PrewarmFileDialogs() {
	darwinPrewarmOnce.Do(func() {
		darwinHelperMgr.startBundleWatcher()
		if !darwinBundleIntact() {
			darwinHelperMgr.prewarmAsync()
			return
		}
		if !mygo.App.IsReady() {
			return
		}
		mygo.RunOnMain(func() {
			darwinWithAutoreleasePool(func() {
				darwinWarmSavePanelService()
			})
		})
		go func() {
			waitForIdle := func(initialDelay, maxWait time.Duration) bool {
				time.Sleep(initialDelay)
				deadline := time.Now().Add(maxWait)
				for time.Now().Before(deadline) {
					if !mygo.App.IsReady() || fileDialogBusy.Load() {
						return false
					}
					if !darwinBundleIntact() {
						darwinHelperMgr.prewarmAsync()
						return false
					}
					idleSec, ok := darwinSecondsSinceLastKeyOrClick()
					if !ok || idleSec >= 0.25 {
						return true
					}
					time.Sleep(250 * time.Millisecond)
				}
				return mygo.App.IsReady() && !fileDialogBusy.Load() && darwinBundleIntact()
			}

			if !waitForIdle(1800*time.Millisecond, 12*time.Second) {
				return
			}
			mygo.RunOnMain(func() {
				darwinWithAutoreleasePool(func() {
					if darwinPanelActive || darwinCachedOpenPanel.panel != 0 || !darwinBundleIntact() {
						return
					}
					if _, err := darwinEnsureCachedPanel(false); err == nil {
						darwinPrewarmCommonUTTypes()
					}
				})
			})

			// Stagger NSSavePanel pre-creation so export/save pickers also open
			// instantaneously on the first click without adding startup stutter.
			if !waitForIdle(400*time.Millisecond, 12*time.Second) {
				return
			}
			mygo.RunOnMain(func() {
				darwinWithAutoreleasePool(func() {
					if darwinPanelActive || darwinCachedSavePanel.panel != 0 || !darwinBundleIntact() {
						return
					}
					_, _ = darwinEnsureCachedPanel(true)
				})
			})
		}()
	})
}

// ShutdownFileDialogs stops the macOS bundle watcher and any warm helper process.
func ShutdownFileDialogs() {
	darwinHelperMgr.shutdown()
}

// ResumeFileDialogs re-enables the macOS picker helper manager if a quit was canceled.
func ResumeFileDialogs() {
	darwinHelperMgr.resume()
}

func openDialogDarwin(ctx context.Context, win *mygo.Window, opts mygo.OpenDialogOptions, allowed func() bool) ([]string, error) {
	req := openOptsToDarwinRequest(opts)

	if darwinBundleIntact() {
		paths, err := showDarwinNativePanel(ctx, win, req, allowed)
		if !errors.Is(err, errDarwinNativePanelFailed) {
			if allowed == nil {
				refocusWindow(win)
			}
			if err == nil && allowed != nil && !opts.Directory && !dialogPathsMatchExtensions(paths, opts.Filters) {
				return nil, errors.New("selected file does not match the requested extensions")
			}
			return paths, err
		}
		LogWarnf("in-process macOS open panel unavailable, falling back to warm helper: %v", err)
	}

	if allowed != nil && !allowed() {
		return nil, context.Canceled
	}
	paths, helperErr := darwinHelperMgr.pick(ctx, win, req, allowed)
	if helperErr == nil {
		if allowed == nil {
			refocusWindow(win)
		}
		if !opts.Directory && !dialogPathsMatchExtensions(paths, opts.Filters) {
			return nil, errors.New("selected file does not match the requested extensions")
		}
		return paths, nil
	}
	if ctx.Err() != nil || errors.Is(helperErr, context.Canceled) {
		return nil, helperErr
	}

	if _, err := os.Stat("/usr/bin/osascript"); err == nil {
		if allowed != nil && !allowed() {
			return nil, context.Canceled
		}
		paths, osaErr := openDialogDarwinOsascript(ctx, opts)
		if allowed == nil {
			refocusWindow(win)
		}
		if osaErr == nil && !opts.Directory && !dialogPathsMatchExtensions(paths, opts.Filters) {
			return nil, errors.New("selected file does not match the requested extensions")
		}
		return paths, osaErr
	}
	return nil, ErrFileDialogUnavailable
}

func saveDialogDarwin(ctx context.Context, win *mygo.Window, opts mygo.SaveDialogOptions, allowed func() bool) (string, error) {
	req := saveOptsToDarwinRequest(opts)

	if darwinBundleIntact() {
		paths, err := showDarwinNativePanel(ctx, win, req, allowed)
		if !errors.Is(err, errDarwinNativePanelFailed) {
			if allowed == nil {
				refocusWindow(win)
			}
			if len(paths) > 0 {
				return paths[0], err
			}
			return "", err
		}
		LogWarnf("in-process macOS save panel unavailable, falling back to warm helper: %v", err)
	}

	if allowed != nil && !allowed() {
		return "", context.Canceled
	}
	paths, helperErr := darwinHelperMgr.pick(ctx, win, req, allowed)
	if helperErr == nil {
		if allowed == nil {
			refocusWindow(win)
		}
		if len(paths) > 0 {
			return paths[0], nil
		}
		return "", nil
	}
	if ctx.Err() != nil || errors.Is(helperErr, context.Canceled) {
		return "", helperErr
	}

	if _, err := os.Stat("/usr/bin/osascript"); err == nil {
		if allowed != nil && !allowed() {
			return "", context.Canceled
		}
		chosen, osaErr := saveDialogDarwinOsascript(ctx, opts)
		if allowed == nil {
			refocusWindow(win)
		}
		return chosen, osaErr
	}
	return "", ErrFileDialogUnavailable
}
