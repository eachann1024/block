//go:build darwin

package editor

import (
	"bytes"
	"errors"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

const (
	darwinQuarantineAttr = "com.apple.quarantine"
	maxOriginMarkBytes   = 64 * 1024
)

func readFDXattr(f *os.File, attr string) ([]byte, bool, error) {
	if f == nil {
		return nil, false, errors.New("nil file handle")
	}
	fd := int(f.Fd())
	sz, err := unix.Fgetxattr(fd, attr, nil)
	runtime.KeepAlive(f)
	if err != nil {
		if errors.Is(err, unix.ENOATTR) || errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if sz <= 0 {
		return nil, false, nil
	}
	if sz > maxOriginMarkBytes {
		return nil, false, errors.New("origin security mark exceeds size limit")
	}
	buf := make([]byte, sz)
	n, err := unix.Fgetxattr(fd, attr, buf)
	runtime.KeepAlive(f)
	if err != nil {
		return nil, false, err
	}
	return buf[:n], true, nil
}

// preserveOriginSecurityMark copies macOS com.apple.quarantine directly from the
// verified open srcFile descriptor onto the open dstFile descriptor.
func preserveOriginSecurityMark(_ string, srcFile *os.File, dstFile *os.File, _ string) ([]byte, bool, error) {
	if srcFile == nil || dstFile == nil {
		return nil, false, errors.New("missing file handle")
	}
	val, present, err := readFDXattr(srcFile, darwinQuarantineAttr)
	if err != nil {
		return nil, false, err
	}
	if !present {
		return nil, false, nil
	}
	err = unix.Fsetxattr(int(dstFile.Fd()), darwinQuarantineAttr, val, 0)
	runtime.KeepAlive(dstFile)
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

// verifyOriginSecurityMark confirms the final staged file descriptor carries the
// exact expected com.apple.quarantine xattr before handing it to the OS viewer.
func verifyOriginSecurityMark(finalFile *os.File, expected []byte) error {
	got, present, err := readFDXattr(finalFile, darwinQuarantineAttr)
	if err != nil {
		return err
	}
	if !present || !bytes.Equal(got, expected) {
		return errors.New("origin security mark verification failed")
	}
	return nil
}
