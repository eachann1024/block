package files

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

const (
	pickerShow         = 3
	pickerSetFileTypes = 4
	pickerSetOptions   = 9
	pickerGetOptions   = 10
	pickerSetFolder    = 12
	pickerSetFileName  = 15
	pickerSetTitle     = 17
	pickerGetResult    = 20
	pickerClose        = 23
	pickerGetResults   = 27
	pickerItemPath     = 5
	pickerItemCount    = 7
	pickerItemAt       = 8
	pickerCanceled     = 0x800704c7
	pickerOverwrite    = 0x2
	pickerFolders      = 0x20
	pickerFilesystem   = 0x40
	pickerMultiselect  = 0x200
	pickerPathExists   = 0x800
	pickerFileExists   = 0x1000
)

var (
	pickerOLE            = windows.NewLazySystemDLL("ole32.dll")
	pickerCreateInstance = pickerOLE.NewProc("CoCreateInstance")
	pickerMemFree        = pickerOLE.NewProc("CoTaskMemFree")
	pickerShellItem      = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
)

type pickerCOM struct{ vtable *[28]uintptr }

//go:uintptrescapes
func (object *pickerCOM) call(method int, args ...uintptr) uint32 {
	result, _, _ := syscall.SyscallN(object.vtable[method], append([]uintptr{uintptr(unsafe.Pointer(object))}, args...)...)
	return uint32(result)
}

func (object *pickerCOM) setString(method int, value string) {
	if value == "" {
		return
	}
	if text, err := windows.UTF16PtrFromString(value); err == nil {
		object.call(method, uintptr(unsafe.Pointer(text)))
		runtime.KeepAlive(text)
	}
}

func showPluginPicker(ctx context.Context, opts pluginPickerOptions, allowed func() bool) ([]string, error) {
	dir, name := opts.defaultLocation()
	var paths []string
	var err error = context.Canceled
	mygo.RunOnMain(func() {
		if ctx.Err() != nil || !allowed() {
			return
		}
		parent := opts.Parent.NativeHandle()
		if parent == 0 {
			err = ErrFileDialogUnavailable
			return
		}
		classID, interfaceID := "{dc1c5a9c-e88a-4dde-a5a1-60f82a20aef7}", "{d57c7288-d4ad-4768-be02-9d969532d960}"
		if opts.save {
			classID, interfaceID = "{c0b4e2f3-ba21-4773-8dba-335ec946eb8b}", "{84bccd23-5fde-4cdb-aea4-af64b83d78ab}"
		}
		class, _ := windows.GUIDFromString(classID)
		iface, _ := windows.GUIDFromString(interfaceID)
		var dialog *pickerCOM
		result, _, _ := pickerCreateInstance.Call(uintptr(unsafe.Pointer(&class)), 0, 1, uintptr(unsafe.Pointer(&iface)), uintptr(unsafe.Pointer(&dialog)))
		if int32(result) < 0 || dialog == nil {
			err = ErrFileDialogUnavailable
			return
		}
		defer dialog.call(2)
		var options uint32
		dialog.call(pickerGetOptions, uintptr(unsafe.Pointer(&options)))
		options |= pickerFilesystem | pickerPathExists
		switch {
		case opts.save:
			options |= pickerOverwrite
		case opts.Directory:
			options |= pickerFolders
		default:
			options |= pickerFileExists
		}
		if opts.Multiple {
			options |= pickerMultiselect
		}
		dialog.call(pickerSetOptions, uintptr(options))
		dialog.setString(pickerSetTitle, opts.Title)
		setPickerFolder(dialog, dir)
		dialog.setString(pickerSetFileName, name)
		setPickerFileTypes(dialog, opts)
		if ctx.Err() != nil || !allowed() {
			return
		}
		active := true
		stop := context.AfterFunc(ctx, func() {
			mygo.RunOnMain(func() {
				if active {
					dialog.call(pickerClose, pickerCanceled)
				}
			})
		})
		response := dialog.call(pickerShow, parent)
		active = false
		stop()
		err = nil
		if response == pickerCanceled || ctx.Err() != nil {
			return
		}
		if int32(response) < 0 {
			err = fmt.Errorf("show file picker: HRESULT 0x%08x", response)
			return
		}
		if opts.Multiple {
			paths = multiplePickerPaths(dialog)
			return
		}
		var item *pickerCOM
		if dialog.call(pickerGetResult, uintptr(unsafe.Pointer(&item))) == 0 && item != nil {
			defer item.call(2)
			if path := pickerPath(item); path != "" {
				paths = []string{path}
			}
		}
	})
	return paths, err
}

func setPickerFolder(dialog *pickerCOM, dir string) {
	if dir == "" {
		return
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	iid, _ := windows.GUIDFromString("{43826d1e-e718-42ee-bc55-a1e261c37bfe}")
	var folder *pickerCOM
	result, _, _ := pickerShellItem.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&folder)))
	if int32(result) >= 0 && folder != nil {
		dialog.call(pickerSetFolder, uintptr(unsafe.Pointer(folder)))
		folder.call(2)
	}
}

func setPickerFileTypes(dialog *pickerCOM, opts pluginPickerOptions) {
	type filterSpec struct{ name, pattern *uint16 }
	var filters []filterSpec
	for _, filter := range opts.Filters {
		var patterns []string
		for _, ext := range filter.Extensions {
			if ext == "*" {
				patterns = append(patterns, "*.*")
				continue
			}
			patterns = append(patterns, "*."+strings.TrimPrefix(ext, "."))
		}
		if len(patterns) != 0 {
			name, _ := windows.UTF16PtrFromString(filter.Name)
			pattern, _ := windows.UTF16PtrFromString(strings.Join(patterns, ";"))
			filters = append(filters, filterSpec{name, pattern})
		}
	}
	if len(filters) != 0 {
		dialog.call(pickerSetFileTypes, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0])))
		runtime.KeepAlive(filters)
	}
}

func pickerPath(item *pickerCOM) string {
	var path *uint16
	if item.call(pickerItemPath, 0x80058000, uintptr(unsafe.Pointer(&path))) != 0 || path == nil {
		return ""
	}
	defer pickerMemFree.Call(uintptr(unsafe.Pointer(path)))
	return windows.UTF16PtrToString(path)
}

func multiplePickerPaths(dialog *pickerCOM) []string {
	var items *pickerCOM
	if dialog.call(pickerGetResults, uintptr(unsafe.Pointer(&items))) != 0 || items == nil {
		return nil
	}
	defer items.call(2)
	var count uint32
	items.call(pickerItemCount, uintptr(unsafe.Pointer(&count)))
	var paths []string
	for index := uint32(0); index < count; index++ {
		var item *pickerCOM
		if items.call(pickerItemAt, uintptr(index), uintptr(unsafe.Pointer(&item))) == 0 && item != nil {
			if path := pickerPath(item); path != "" {
				paths = append(paths, path)
			}
			item.call(2)
		}
	}
	return paths
}
