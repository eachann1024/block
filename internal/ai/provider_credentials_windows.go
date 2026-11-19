package ai

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DPAPI protects the data with the current Windows account. Unix mode bits
// have no equivalent meaning here.
func privateCredentialPath(os.FileInfo) bool { return true }

func lockProviderCredentials(f *os.File) (func(), error) {
	handle := windows.Handle(f.Fd())
	err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
	return func() { _ = windows.UnlockFileEx(handle, 0, 1, 0, &windows.Overlapped{}) }, err
}

// Individual files are flushed through their handles before rename.
func syncCredentialDirectory(*os.Root) error { return nil }

func protectProviderCredentials(data []byte) ([]byte, error) {
	input, output := credentialBlob(data), windows.DataBlob{}
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	return copyCredentialBlob(output), nil
}

func unprotectProviderCredentials(data []byte) ([]byte, error) {
	input, output := credentialBlob(data), windows.DataBlob{}
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	return copyCredentialBlob(output), nil
}

func credentialBlob(data []byte) windows.DataBlob {
	return windows.DataBlob{Size: uint32(len(data)), Data: unsafe.SliceData(data)}
}

func copyCredentialBlob(blob windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(blob.Data))))
	data := unsafe.Slice(blob.Data, blob.Size)
	defer clear(data)
	return append([]byte(nil), data...)
}
