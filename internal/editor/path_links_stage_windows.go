//go:build windows

package editor

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsZoneIDStreamName = ":Zone.Identifier"
	maxOriginMarkBytes      = 64 * 1024
)

func openWindowsZoneStream(parent *os.File, access uint32, disposition uint32) (windows.Handle, error) {
	if parent == nil {
		return windows.InvalidHandle, errors.New("nil file handle")
	}
	streamName, err := windows.NewNTUnicodeString(windowsZoneIDStreamName)
	if err != nil {
		return windows.InvalidHandle, err
	}
	oa := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    streamName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var (
		h    windows.Handle
		iosb windows.IO_STATUS_BLOCK
	)
	ntErr := windows.NtCreateFile(
		&h,
		access|windows.SYNCHRONIZE,
		&oa,
		&iosb,
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_NON_DIRECTORY_FILE,
		0,
		0,
	)
	runtime.KeepAlive(parent)
	runtime.KeepAlive(streamName)
	if ntErr != nil {
		return windows.InvalidHandle, ntErr
	}
	return h, nil
}

func volumeSupportsNamedStreams(f *os.File) (bool, error) {
	if f == nil {
		return false, errors.New("nil file handle")
	}
	var fsFlags uint32
	err := windows.GetVolumeInformationByHandle(
		windows.Handle(f.Fd()),
		nil, 0,
		nil, nil,
		&fsFlags,
		nil, 0,
	)
	runtime.KeepAlive(f)
	if err != nil {
		return false, err
	}
	return fsFlags&windows.FILE_NAMED_STREAMS != 0, nil
}

func readWindowsZoneStreamHandle(f *os.File) ([]byte, bool, error) {
	h, err := openWindowsZoneStream(f, windows.FILE_READ_DATA, windows.FILE_OPEN)
	if err != nil {
		if errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) {
			return nil, false, nil
		}
		if errors.Is(err, windows.STATUS_NOT_SUPPORTED) ||
			errors.Is(err, windows.STATUS_INVALID_PARAMETER) ||
			errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND) ||
			errors.Is(err, windows.STATUS_INVALID_DEVICE_REQUEST) {
			supported, vErr := volumeSupportsNamedStreams(f)
			if vErr == nil && !supported {
				return nil, false, nil
			}
		}
		return nil, false, err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var out bytes.Buffer
	buf := make([]byte, 4096)
	for {
		var n uint32
		rErr := windows.ReadFile(h, buf, &n, nil)
		if n > 0 {
			if out.Len()+int(n) > maxOriginMarkBytes {
				return nil, false, errors.New("origin security mark exceeds size limit")
			}
			out.Write(buf[:n])
		}
		if rErr != nil {
			if errors.Is(rErr, windows.ERROR_HANDLE_EOF) {
				break
			}
			return nil, false, rErr
		}
		if n == 0 {
			break
		}
	}
	return out.Bytes(), true, nil
}

// preserveOriginSecurityMark reads Windows Mark of the Web (:Zone.Identifier)
// directly relative to the already-opened srcFile kernel handle (immune to path
// swaps) and writes it directly onto dstFile's handle.
func preserveOriginSecurityMark(_ string, srcFile *os.File, dstFile *os.File, _ string) ([]byte, bool, error) {
	if srcFile == nil || dstFile == nil {
		return nil, false, errors.New("missing file handle")
	}
	data, present, err := readWindowsZoneStreamHandle(srcFile)
	if err != nil {
		return nil, false, err
	}
	if !present {
		return nil, false, nil
	}

	hDst, err := openWindowsZoneStream(dstFile, windows.FILE_WRITE_DATA, windows.FILE_OVERWRITE_IF)
	if err != nil {
		return nil, false, err
	}
	written := 0
	for written < len(data) {
		var n uint32
		wErr := windows.WriteFile(hDst, data[written:], &n, nil)
		if wErr != nil {
			_ = windows.CloseHandle(hDst)
			return nil, false, wErr
		}
		if n == 0 {
			_ = windows.CloseHandle(hDst)
			return nil, false, errors.New("short write on origin security mark")
		}
		written += int(n)
	}
	flushErr := windows.FlushFileBuffers(hDst)
	closeErr := windows.CloseHandle(hDst)
	if flushErr != nil {
		return nil, false, flushErr
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	return data, true, nil
}

// verifyOriginSecurityMark confirms the final staged file handle carries the exact
// expected :Zone.Identifier alternate data stream before handing it to the OS viewer.
func verifyOriginSecurityMark(finalFile *os.File, expected []byte) error {
	got, present, err := readWindowsZoneStreamHandle(finalFile)
	if err != nil {
		return err
	}
	if !present || !bytes.Equal(got, expected) {
		return errors.New("origin security mark verification failed")
	}
	return nil
}
