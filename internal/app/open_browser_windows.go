//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func shellExecute(file, params string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var paramPtr *uint16
	if params != "" {
		paramPtr, err = syscall.UTF16PtrFromString(params)
		if err != nil {
			return err
		}
	}
	const swShownormal = 1
	r, _, _ := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		uintptr(unsafe.Pointer(paramPtr)),
		0,
		swShownormal,
	)
	if r <= 32 {
		return fmt.Errorf("ShellExecute %q: %d", file, r)
	}
	return nil
}
