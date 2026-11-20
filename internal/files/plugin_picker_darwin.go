package files

import (
	"context"
	"strings"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"
)

func showPluginPicker(ctx context.Context, opts pluginPickerOptions, allowed func() bool) ([]string, error) {
	dir, name := opts.defaultLocation()
	done := make(chan []string, 1)
	var started bool
	var setupErr error
	mygo.RunOnMain(func() {
		if ctx.Err() != nil || !allowed() {
			setupErr = context.Canceled
			return
		}
		parent := objc.ID(opts.Parent.NativeHandle())
		if parent == 0 {
			setupErr = ErrFileDialogUnavailable
			return
		}
		pool := pickerSend(objc.ID(objc.GetClass("NSAutoreleasePool")), "new")
		defer pickerSend(pool, "drain")
		var panel objc.ID
		if opts.save {
			panel = pickerSend(objc.ID(objc.GetClass("NSSavePanel")), "savePanel")
		} else {
			panel = pickerSend(objc.ID(objc.GetClass("NSOpenPanel")), "openPanel")
			pickerSend(panel, "setCanChooseFiles:", !opts.Directory)
			pickerSend(panel, "setCanChooseDirectories:", opts.Directory)
			pickerSend(panel, "setAllowsMultipleSelection:", opts.Multiple)
		}
		if opts.Title != "" {
			pickerSend(panel, "setTitle:", pickerString(opts.Title))
		}
		pickerSend(panel, "setCanCreateDirectories:", opts.CreateDirectories)
		if dir != "" {
			url := pickerSend(objc.ID(objc.GetClass("NSURL")), "fileURLWithPath:isDirectory:", pickerString(dir), true)
			pickerSend(panel, "setDirectoryURL:", url)
		}
		if name != "" {
			pickerSend(panel, "setNameFieldStringValue:", pickerString(name))
		}
		setPickerFileTypes(panel, opts)
		if ctx.Err() != nil || !allowed() {
			setupErr = context.Canceled
			return
		}
		pickerSend(panel, "retain")
		active := true
		stop := context.AfterFunc(ctx, func() {
			mygo.RunOnMain(func() {
				if active {
					pickerSend(panel, "cancel:", objc.ID(0))
				}
			})
		})
		completion := objc.NewBlock(func(_ objc.Block, response int) {
			active = false
			stop()
			defer pickerSend(panel, "release")
			var paths []string
			if response == 1 && ctx.Err() == nil {
				if opts.save {
					paths = append(paths, pickerURLPath(pickerSend(panel, "URL")))
				} else {
					urls := pickerSend(panel, "URLs")
					count := objc.Send[uint](urls, objc.RegisterName("count"))
					for index := uint(0); index < count; index++ {
						paths = append(paths, pickerURLPath(pickerSend(urls, "objectAtIndex:", index)))
					}
				}
			}
			done <- paths
		})
		defer completion.Release()
		started = true
		pickerSend(panel, "beginSheetModalForWindow:completionHandler:", parent, completion)
	})
	if !started {
		if setupErr == nil {
			setupErr = context.Canceled
		}
		return nil, setupErr
	}
	return <-done, nil
}

func pickerSend(receiver objc.ID, selector string, args ...any) objc.ID {
	return objc.Send[objc.ID](receiver, objc.RegisterName(selector), args...)
}

func pickerString(value string) objc.ID {
	return pickerSend(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", value)
}

func pickerURLPath(url objc.ID) string {
	value := objc.Send[*byte](pickerSend(url, "path"), objc.RegisterName("UTF8String"))
	return unix.BytePtrToString(value)
}

func setPickerFileTypes(panel objc.ID, opts pluginPickerOptions) {
	types := pickerSend(objc.ID(objc.GetClass("NSMutableArray")), "array")
	for _, filter := range opts.Filters {
		for _, ext := range filter.Extensions {
			if ext == "*" {
				return
			}
			pickerSend(types, "addObject:", pickerString(strings.TrimPrefix(ext, ".")))
		}
	}
	if objc.Send[uint](types, objc.RegisterName("count")) != 0 {
		pickerSend(panel, "setAllowedFileTypes:", types)
	}
}
