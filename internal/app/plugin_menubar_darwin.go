//go:build darwin

package app

import (
	"log"
	"sync"
	"unsafe"

	"block/internal/plugins"

	"github.com/ebitengine/purego/objc"
	"golang.org/x/sys/unix"
)

// mygo's tray has no drop target, so Block finds the NSStatusBarButton
// behind a tray (by a one-off tooltip) and registers its status bar window
// for dragged types, with a delegate of its own: NSWindow forwards the
// NSDraggingDestination messages to its delegate (the approach uPic uses).
// Registering the button itself does not work, as the status bar window
// does not route drags to its subviews.

const (
	pbFileURL = "public.file-url"
	pbPNG     = "public.png"
	pbTIFF    = "public.tiff"
	pbText    = "public.utf8-plain-text"

	dragOperationNone = 0
	dragOperationCopy = 1
	bitmapTypePNG     = 4 // NSBitmapImageFileTypePNG
)

type menubarDropTarget struct {
	button objc.ID
	window objc.ID
	types  []string
	onDrop func(plugins.MenubarPayload)
}

var menubarDrops struct {
	once    sync.Once
	class   objc.Class
	targets map[objc.ID]*menubarDropTarget // delegate -> target; main thread only
}

func mbSend(receiver objc.ID, selector string, args ...any) objc.ID {
	return objc.Send[objc.ID](receiver, objc.RegisterName(selector), args...)
}

func mbClass(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func mbString(value string) objc.ID {
	return mbSend(mbClass("NSString"), "stringWithUTF8String:", value)
}

func mbGoString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	return unix.BytePtrToString(objc.Send[*byte](s, objc.RegisterName("UTF8String")))
}

func mbBytes(data objc.ID) []byte {
	if data == 0 {
		return nil
	}
	n := objc.Send[uint](data, objc.RegisterName("length"))
	ptr := objc.Send[unsafe.Pointer](data, objc.RegisterName("bytes"))
	if n == 0 || ptr == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(ptr), n)...)
}

func mbArray(values []string) objc.ID {
	arr := mbSend(mbClass("NSMutableArray"), "array")
	for _, v := range values {
		mbSend(arr, "addObject:", mbString(v))
	}
	return arr
}

func menubarDropDelegateClass() objc.Class {
	menubarDrops.once.Do(func() {
		menubarDrops.targets = map[objc.ID]*menubarDropTarget{}
		accepts := func(t *menubarDropTarget, info objc.ID) bool {
			if t == nil || info == 0 {
				return false
			}
			pb := mbSend(info, "draggingPasteboard")
			return pb != 0 && mbSend(pb, "availableTypeFromArray:", mbArray(t.types)) != 0
		}
		highlight := func(t *menubarDropTarget, on bool) {
			if t != nil {
				mbSend(t.button, "highlight:", on)
			}
		}
		entered := func(self objc.ID, _ objc.SEL, info objc.ID) uint {
			t := menubarDrops.targets[self]
			if !accepts(t, info) {
				return dragOperationNone
			}
			highlight(t, true)
			return dragOperationCopy
		}
		cls, err := objc.RegisterClass("BlockMenubarDropDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("draggingEntered:"), Fn: entered},
			{Cmd: objc.RegisterName("draggingUpdated:"), Fn: entered},
			{Cmd: objc.RegisterName("draggingExited:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				highlight(menubarDrops.targets[self], false)
			}},
			{Cmd: objc.RegisterName("prepareForDragOperation:"), Fn: func(self objc.ID, _ objc.SEL, info objc.ID) bool {
				return accepts(menubarDrops.targets[self], info)
			}},
			{Cmd: objc.RegisterName("performDragOperation:"), Fn: func(self objc.ID, _ objc.SEL, info objc.ID) bool {
				t := menubarDrops.targets[self]
				if t == nil {
					return false
				}
				highlight(t, false)
				payload := readMenubarPasteboard(mbSend(info, "draggingPasteboard"))
				if payload.Empty() {
					return false
				}
				go t.onDrop(payload)
				return true
			}},
		})
		if err != nil {
			log.Printf("menubar: drop delegate: %v; drop disabled", err)
			return
		}
		menubarDrops.class = cls
	})
	return menubarDrops.class
}

// findStatusButton returns the NSStatusBarButton whose tooltip is sentinel.
func findStatusButton(sentinel string) objc.ID {
	app := mbSend(mbClass("NSApplication"), "sharedApplication")
	windows := mbSend(app, "windows")
	buttonClass := mbClass("NSStatusBarButton")
	var walk func(view objc.ID, depth int) objc.ID
	walk = func(view objc.ID, depth int) objc.ID {
		if view == 0 || depth > 4 {
			return 0
		}
		if objc.Send[bool](view, objc.RegisterName("isKindOfClass:"), buttonClass) &&
			mbGoString(mbSend(view, "toolTip")) == sentinel {
			return view
		}
		subs := mbSend(view, "subviews")
		for i := uint(0); i < objc.Send[uint](subs, objc.RegisterName("count")); i++ {
			if found := walk(mbSend(subs, "objectAtIndex:", i), depth+1); found != 0 {
				return found
			}
		}
		return 0
	}
	for i := uint(0); i < objc.Send[uint](windows, objc.RegisterName("count")); i++ {
		win := mbSend(windows, "objectAtIndex:", i)
		if found := walk(mbSend(win, "contentView"), 0); found != 0 {
			return found
		}
	}
	return 0
}

// attachMenubarDrop makes the tray whose tooltip is currently sentinel a drop
// target for the drop's accepted kinds. Main thread only.
func attachMenubarDrop(sentinel string, drop *plugins.MenubarDrop, onDrop func(plugins.MenubarPayload)) func() {
	if drop == nil {
		return nil
	}
	cls := menubarDropDelegateClass()
	if cls == 0 {
		return nil
	}
	button := findStatusButton(sentinel)
	window := objc.ID(0)
	if button != 0 {
		window = mbSend(button, "window")
	}
	if window == 0 {
		log.Printf("menubar: status button not found; drop disabled")
		return nil
	}
	if mbSend(window, "delegate") != 0 {
		log.Printf("menubar: status bar window already has a delegate; drop disabled")
		return nil
	}
	var types []string
	if drop.Accepts(plugins.MenubarAcceptFiles) {
		types = append(types, pbFileURL)
	}
	if drop.Accepts(plugins.MenubarAcceptImage) {
		types = append(types, pbPNG, pbTIFF)
	}
	if drop.Accepts(plugins.MenubarAcceptText) {
		types = append(types, pbText)
	}
	delegate := mbSend(objc.ID(cls), "new")
	menubarDrops.targets[delegate] = &menubarDropTarget{button: button, window: window, types: types, onDrop: onDrop}
	mbSend(window, "setDelegate:", delegate)
	mbSend(window, "registerForDraggedTypes:", mbArray(types))
	return func() {
		mbSend(window, "unregisterDraggedTypes")
		mbSend(window, "setDelegate:", objc.ID(0))
		delete(menubarDrops.targets, delegate)
		mbSend(delegate, "release")
	}
}

// readMenubarPasteboard reads local files, an image (PNG, or TIFF converted
// to PNG) and plain text from a pasteboard. Main thread only.
func readMenubarPasteboard(pb objc.ID) plugins.MenubarPayload {
	var out plugins.MenubarPayload
	if pb == 0 {
		return out
	}
	pool := mbSend(mbClass("NSAutoreleasePool"), "new")
	defer mbSend(pool, "drain")

	classes := mbSend(mbClass("NSArray"), "arrayWithObject:", mbClass("NSURL"))
	yes := mbSend(mbClass("NSNumber"), "numberWithBool:", true)
	opts := mbSend(mbClass("NSDictionary"), "dictionaryWithObject:forKey:", yes, mbString("NSPasteboardURLReadingFileURLsOnlyKey"))
	if urls := mbSend(pb, "readObjectsForClasses:options:", classes, opts); urls != 0 {
		for i := uint(0); i < objc.Send[uint](urls, objc.RegisterName("count")); i++ {
			if p := mbGoString(mbSend(mbSend(urls, "objectAtIndex:", i), "path")); p != "" {
				out.Files = append(out.Files, p)
			}
		}
	}
	if png := mbBytes(mbSend(pb, "dataForType:", mbString(pbPNG))); len(png) > 0 {
		out.Images = append(out.Images, png)
	} else if tiff := mbSend(pb, "dataForType:", mbString(pbTIFF)); tiff != 0 {
		if rep := mbSend(mbClass("NSBitmapImageRep"), "imageRepWithData:", tiff); rep != 0 {
			props := mbSend(mbClass("NSDictionary"), "dictionary")
			if png := mbBytes(mbSend(rep, "representationUsingType:properties:", uint(bitmapTypePNG), props)); len(png) > 0 {
				out.Images = append(out.Images, png)
			}
		}
	}
	out.Text = mbGoString(mbSend(pb, "stringForType:", mbString(pbText)))
	return out
}

// readMenubarClipboard reads the general pasteboard. Main thread only.
func readMenubarClipboard() plugins.MenubarPayload {
	return readMenubarPasteboard(mbSend(mbClass("NSPasteboard"), "generalPasteboard"))
}
