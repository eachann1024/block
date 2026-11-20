package files

import (
	"context"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"
)

type pickerStringList struct {
	value *byte
	next  *pickerStringList
}

var gtkPicker struct {
	create           func(string, uintptr, int32, uintptr, uintptr) uintptr
	show             func(uintptr)
	destroy          func(uintptr)
	setModal         func(uintptr, int32)
	setMultiple      func(uintptr, int32)
	setOverwrite     func(uintptr, int32)
	setFolder        func(uintptr, string) int32
	setName          func(uintptr, string)
	getPaths         func(uintptr) *pickerStringList
	newFilter        func() uintptr
	setFilterName    func(uintptr, string)
	addPattern       func(uintptr, string)
	addFilter        func(uintptr, uintptr)
	connect          func(uintptr, string, uintptr, uintptr, uintptr, int32) uint64
	disconnect       func(uintptr, uint64)
	unref            func(uintptr)
	free             func(unsafe.Pointer)
	freeList         func(*pickerStringList)
	responseCallback uintptr
	respond          func(int32)
}

var loadGTKPicker = sync.OnceValue(func() bool {
	lib, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return false
	}
	for _, symbol := range []struct {
		name string
		dst  any
	}{
		{"gtk_file_chooser_native_new", &gtkPicker.create},
		{"gtk_native_dialog_show", &gtkPicker.show},
		{"gtk_native_dialog_destroy", &gtkPicker.destroy},
		{"gtk_native_dialog_set_modal", &gtkPicker.setModal},
		{"gtk_file_chooser_set_select_multiple", &gtkPicker.setMultiple},
		{"gtk_file_chooser_set_do_overwrite_confirmation", &gtkPicker.setOverwrite},
		{"gtk_file_chooser_set_current_folder", &gtkPicker.setFolder},
		{"gtk_file_chooser_set_current_name", &gtkPicker.setName},
		{"gtk_file_chooser_get_filenames", &gtkPicker.getPaths},
		{"gtk_file_filter_new", &gtkPicker.newFilter},
		{"gtk_file_filter_set_name", &gtkPicker.setFilterName},
		{"gtk_file_filter_add_pattern", &gtkPicker.addPattern},
		{"gtk_file_chooser_add_filter", &gtkPicker.addFilter},
		{"g_signal_connect_data", &gtkPicker.connect},
		{"g_signal_handler_disconnect", &gtkPicker.disconnect},
		{"g_object_unref", &gtkPicker.unref},
		{"g_free", &gtkPicker.free},
		{"g_slist_free", &gtkPicker.freeList},
	} {
		address, err := purego.Dlsym(lib, symbol.name)
		if err != nil {
			_ = purego.Dlclose(lib)
			return false
		}
		purego.RegisterFunc(symbol.dst, address)
	}
	gtkPicker.responseCallback = purego.NewCallback(func(_ uintptr, response int32, _ uintptr) {
		if respond := gtkPicker.respond; respond != nil {
			respond(response)
		}
	})
	return true
})

func showPluginPicker(ctx context.Context, opts pluginPickerOptions, allowed func() bool) ([]string, error) {
	if !loadGTKPicker() {
		return nil, ErrFileDialogUnavailable
	}
	dir, name := opts.defaultLocation()
	done := make(chan []string, 1)
	var dialog uintptr
	var setupErr error
	mygo.RunOnMain(func() {
		if ctx.Err() != nil || !allowed() {
			return
		}
		parent := opts.Parent.NativeHandle()
		if parent == 0 {
			setupErr = ErrFileDialogUnavailable
			return
		}
		var action int32
		if opts.save {
			action = 1
		} else if opts.Directory {
			action = 2
		}
		dialog = gtkPicker.create(opts.Title, parent, action, 0, 0)
		if dialog == 0 {
			setupErr = ErrFileDialogUnavailable
			return
		}
		gtkPicker.setModal(dialog, 1)
		if opts.Multiple {
			gtkPicker.setMultiple(dialog, 1)
		}
		if dir != "" {
			gtkPicker.setFolder(dialog, dir)
		}
		if opts.save {
			gtkPicker.setOverwrite(dialog, 1)
			if name != "" {
				gtkPicker.setName(dialog, name)
			}
		}
		for _, filter := range opts.Filters {
			item := gtkPicker.newFilter()
			gtkPicker.setFilterName(item, filter.Name)
			for _, ext := range filter.Extensions {
				if ext == "*" {
					gtkPicker.addPattern(item, "*")
					continue
				}
				ext = strings.TrimPrefix(ext, ".")
				gtkPicker.addPattern(item, "*."+strings.ToLower(ext))
				gtkPicker.addPattern(item, "*."+strings.ToUpper(ext))
			}
			gtkPicker.addFilter(dialog, item)
		}
		if ctx.Err() != nil || !allowed() {
			gtkPicker.destroy(dialog)
			gtkPicker.unref(dialog)
			dialog = 0
			return
		}
		signal := gtkPicker.connect(dialog, "response", gtkPicker.responseCallback, 0, 0, 0)
		active := true
		stop := context.AfterFunc(ctx, func() {
			mygo.RunOnMain(func() {
				if active {
					gtkPicker.respond(0)
				}
			})
		})
		gtkPicker.respond = func(response int32) {
			gtkPicker.respond = nil
			active = false
			stop()
			var paths []string
			if response == -3 && ctx.Err() == nil {
				list := gtkPicker.getPaths(dialog)
				for item := list; item != nil; item = item.next {
					paths = append(paths, unix.BytePtrToString(item.value))
					gtkPicker.free(unsafe.Pointer(item.value))
				}
				gtkPicker.freeList(list)
			}
			gtkPicker.disconnect(dialog, signal)
			gtkPicker.destroy(dialog)
			done <- paths
		}
		gtkPicker.show(dialog)
	})
	if dialog == 0 {
		if setupErr == nil {
			setupErr = context.Canceled
		}
		return nil, setupErr
	}
	paths := <-done
	mygo.RunOnMain(func() { gtkPicker.unref(dialog) })
	return paths, nil
}
