package systemintegration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	ole32                 = windows.NewLazySystemDLL("ole32.dll")
	user32                = windows.NewLazySystemDLL("user32.dll")
	comctl32              = windows.NewLazySystemDLL("comctl32.dll")
	coInitializeEx        = ole32.NewProc("CoInitializeEx")
	coUninitialize        = ole32.NewProc("CoUninitialize")
	coCreateInstance      = ole32.NewProc("CoCreateInstance")
	createIcon            = user32.NewProc("CreateIconFromResourceEx")
	destroyIcon           = user32.NewProc("DestroyIcon")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	setWindowSubclass     = comctl32.NewProc("SetWindowSubclass")
	removeWindowSubclass  = comctl32.NewProc("RemoveWindowSubclass")
	defSubclassProc       = comctl32.NewProc("DefSubclassProc")
	classTaskbarList      = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	interfaceTaskbarList3 = windows.GUID{Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86, Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
)

var (
	taskbarButtonCreated uintptr
	usingOverlay         = make(map[uintptr]int)
	overlaySubclass      uintptr
)

func NotificationSupport(context.Context) (bool, bool, func(string) string) {
	return true, true, func(text string) string { return text }
}

func CredentialsAvailable(context.Context) bool { return true }
func BadgeAvailable(context.Context) bool       { return true }
func WakeAvailable(context.Context) bool        { return true }

func SetBadge(win *mygo.Window, count int) error {
	if win == nil {
		return errors.ErrUnsupported
	}
	var err error
	mygo.RunOnMain(func() {
		hwnd := win.NativeHandle()
		if hwnd == 0 {
			err = errors.ErrUnsupported
			return
		}
		if count > 0 {
			if overlaySubclass == 0 {
				overlaySubclass = windows.NewCallback(overlayWindowProc)
			}
			if taskbarButtonCreated == 0 {
				taskbarButtonCreated, _, err = registerWindowMessage.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarButtonCreated"))))
				if taskbarButtonCreated == 0 {
					return
				}
			}
			if _, tracked := usingOverlay[hwnd]; !tracked {
				installed, _, hookErr := setWindowSubclass.Call(hwnd, overlaySubclass, 1, 0)
				if installed == 0 {
					err = hookErr
					return
				}
				usingOverlay[hwnd] = 0
			}
		}
		err = setTaskbarOverlay(hwnd, count)
		if err == nil && count > 0 {
			usingOverlay[hwnd] = count
		} else if count == 0 || usingOverlay[hwnd] == 0 {
			forgetOverlay(hwnd)
		}
	})
	return err
}

func overlayWindowProc(hwnd uintptr, message uint32, wParam, lParam, _, _ uintptr) uintptr {
	const destroyWindow = 0x0082
	if message == destroyWindow {
		forgetOverlay(hwnd)
	}
	result, _, _ := defSubclassProc.Call(hwnd, uintptr(message), wParam, lParam)
	if uintptr(message) == taskbarButtonCreated && usingOverlay[hwnd] > 0 {
		_ = setTaskbarOverlay(hwnd, usingOverlay[hwnd])
	}
	return result
}

func forgetOverlay(hwnd uintptr) {
	if _, ok := usingOverlay[hwnd]; ok {
		removeWindowSubclass.Call(hwnd, overlaySubclass, 1)
		delete(usingOverlay, hwnd)
	}
}

func setTaskbarOverlay(hwnd uintptr, count int) error {
	const rpcChangedMode = 0x80010106
	result, _, _ := coInitializeEx.Call(0, 2)
	if int32(result) >= 0 {
		defer coUninitialize.Call()
	} else if uint32(result) != rpcChangedMode {
		return comError("CoInitializeEx", result)
	}
	var taskbar *struct{ methods *[19]uintptr }
	result, _, _ = coCreateInstance.Call(uintptr(unsafe.Pointer(&classTaskbarList)), 0, 1,
		uintptr(unsafe.Pointer(&interfaceTaskbarList3)), uintptr(unsafe.Pointer(&taskbar)))
	if int32(result) < 0 || taskbar == nil {
		return comError("CoCreateInstance", result)
	}
	instance := uintptr(unsafe.Pointer(taskbar))
	defer syscall.SyscallN(taskbar.methods[2], instance)
	result, _, _ = syscall.SyscallN(taskbar.methods[3], instance)
	if int32(result) < 0 {
		return comError("ITaskbarList.HrInit", result)
	}
	var icon uintptr
	var accessibleName *uint16
	if count > 0 {
		text := strconv.Itoa(count)
		var err error
		icon, err = makeBadgeIcon(text)
		if err != nil {
			return err
		}
		defer destroyIcon.Call(icon)
		accessibleName = windows.StringToUTF16Ptr(text)
	}
	result, _, _ = syscall.SyscallN(taskbar.methods[18], instance, hwnd, icon, uintptr(unsafe.Pointer(accessibleName)))
	if int32(result) < 0 {
		return comError("ITaskbarList3.SetOverlayIcon", result)
	}
	return nil
}

var badgeDigits = [...]uint16{
	0b111_101_101_101_111, 0b010_110_010_010_111, 0b111_001_111_100_111, 0b111_001_111_001_111,
	0b101_101_111_001_001, 0b111_100_111_001_111, 0b111_100_111_101_111, 0b111_001_001_001_001,
	0b111_101_111_101_111, 0b111_101_111_001_111,
}

func makeBadgeIcon(text string) (uintptr, error) {
	const size, maskStride, headerSize = 16, 4, 40
	resource := [headerSize + size*size*4 + size*maskStride]byte{}
	binary.LittleEndian.PutUint32(resource[:], headerSize)
	binary.LittleEndian.PutUint32(resource[4:], size)
	binary.LittleEndian.PutUint32(resource[8:], size*2)
	binary.LittleEndian.PutUint16(resource[12:], 1)
	binary.LittleEndian.PutUint16(resource[14:], 32)
	xorMask := resource[headerSize : headerSize+size*size*4]
	andMask := resource[headerSize+size*size*4:]
	for row := range size {
		for column := range size {
			dx, dy := 2*column-(size-1), 2*row-(size-1)
			if dx*dx+dy*dy >= size*size {
				andMask[(size-1-row)*maskStride+column/8] |= 1 << (7 - column%8)
				continue
			}
			offset := ((size-1-row)*size + column) * 4
			copy(xorMask[offset:], []byte{0x9C, 0x63, 0x20, 0xFF})
		}
	}
	const glyphWidth, glyphHeight = 3, 5
	width := len(text)*(glyphWidth+1) - 1
	left, top := (size-width)/2, (size-glyphHeight)/2
	for index, digit := range []byte(text) {
		bits := badgeDigits[digit-'0']
		for row := range glyphHeight {
			for column := range glyphWidth {
				if bits&(1<<(glyphWidth*glyphHeight-1-row*glyphWidth-column)) == 0 {
					continue
				}
				offset := ((size-1-top-row)*size + left + index*(glyphWidth+1) + column) * 4
				copy(xorMask[offset:], []byte{0xFF, 0xFF, 0xFF, 0xFF})
			}
		}
	}
	icon, _, err := createIcon.Call(uintptr(unsafe.Pointer(&resource[0])), uintptr(len(resource)), 1, 0x30000, size, size, 0)
	if icon == 0 {
		return 0, err
	}
	return icon, nil
}

func comError(method string, code uintptr) error {
	return fmt.Errorf("%s: HRESULT 0x%08x", method, uint32(code))
}
