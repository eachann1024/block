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
// behind a tray (by a one-off tooltip) and makes it one: AppKit sends the
// NSDraggingDestination messages to a view registered for dragged types,
// and NSStatusBarButton gets those methods once, dispatching by button.

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
	types  []string
	onDrop func(plugins.MenubarPayload)
}

var menubarDrops struct {
	once    sync.Once
	ok      bool
	targets map[objc.ID]*menubarDropTarget // main thread only
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

func installMenubarDropMethods() bool {
	menubarDrops.once.Do(func() {
		menubarDrops.targets = map[objc.ID]*menubarDropTarget{}
		cls := objc.GetClass("NSStatusBarButton")
		if cls == 0 {
			return
		}
		highlight := func(self objc.ID, on bool) { mbSend(self, "highlight:", on) }
		accepts := func(self, info objc.ID) bool {
			t := menubarDrops.targets[self]
			if t == nil || info == 0 {
				return false
			}
			pb := mbSend(info, "draggingPasteboard")
			return pb != 0 && mbSend(pb, "availableTypeFromArray:", mbArray(t.types)) != 0
		}
		entered := objc.NewIMP(func(self objc.ID, _ objc.SEL, info objc.ID) uint {
			if !accepts(self, info) {
				return dragOperationNone
			}
			highlight(self, true)
			return dragOperationCopy
		})
		exited := objc.NewIMP(func(self objc.ID, _ objc.SEL, _ objc.ID) {
			if menubarDrops.targets[self] != nil {
				highlight(self, false)
			}
		})
		prepare := objc.NewIMP(func(self objc.ID, _ objc.SEL, info objc.ID) bool {
			return accepts(self, info)
		})
		perform := objc.NewIMP(func(self objc.ID, _ objc.SEL, info objc.ID) bool {
			t := menubarDrops.targets[self]
			if t == nil {
				return false
			}
			highlight(self, false)
			payload := readMenubarPasteboard(mbSend(info, "draggingPasteboard"))
			if payload.Empty() {
				return false
			}
			go t.onDrop(payload)
			return true
		})
		ok := cls.AddMethod(objc.RegisterName("draggingEntered:"), entered, "Q@:@")
		ok = cls.AddMethod(objc.RegisterName("draggingUpdated:"), entered, "Q@:@") && ok
		ok = cls.AddMethod(objc.RegisterName("draggingExited:"), exited, "v@:@") && ok
		ok = cls.AddMethod(objc.RegisterName("prepareForDragOperation:"), prepare, "B@:@") && ok
		ok = cls.AddMethod(objc.RegisterName("performDragOperation:"), perform, "B@:@") && ok
		if !ok {
			log.Printf("menubar: NSStatusBarButton already implements dragging; drop disabled")
		}
		menubarDrops.ok = ok
	})
	return menubarDrops.ok
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
	if drop == nil || !installMenubarDropMethods() {
		return nil
	}
	button := findStatusButton(sentinel)
	if button == 0 {
		log.Printf("menubar: status button not found; drop disabled")
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
	menubarDrops.targets[button] = &menubarDropTarget{types: types, onDrop: onDrop}
	mbSend(button, "registerForDraggedTypes:", mbArray(types))
	return func() {
		delete(menubarDrops.targets, button)
		mbSend(button, "unregisterDraggedTypes")
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
