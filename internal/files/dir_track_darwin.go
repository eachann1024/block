//go:build darwin

package files

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

var (
	darwinFSGetPathOnce sync.Once
	darwinFSGetPathFn   func(buf *byte, bufSize uintptr, fsid *[2]int32, objID uint64) int64
)

func openDirHandle(clean string) (dirHandle, string, error) {
	f, _, err := openUnixDirFile(clean)
	if err != nil {
		return nil, "", err
	}
	var locator string
	fd := int(f.Fd())
	var sfs unix.Statfs_t
	var st unix.Stat_t
	if unix.Fstatfs(fd, &sfs) == nil && unix.Fstat(fd, &st) == nil && st.Ino != 0 {
		locator = fmt.Sprintf("darwin:%d:%d:%d", sfs.Fsid.Val[0], sfs.Fsid.Val[1], st.Ino)
	}
	runtime.KeepAlive(f)
	return f, locator, nil
}

func currentDirPath(h dirHandle, _ string) (string, bool) {
	if h == nil {
		return "", false
	}
	var buf [4096]byte
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, h.Fd(), uintptr(unix.F_GETPATH), uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(h)
	if errno != 0 {
		return "", false
	}
	n := bytes.IndexByte(buf[:], 0)
	if n <= 0 {
		return "", false
	}
	return verifyLiveDirPath(h, string(buf[:n]))
}

// CaptureDirLocator returns a persistent cross-restart locator for dir.
func CaptureDirLocator(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	clean := filepath.Clean(dir)
	var sfs unix.Statfs_t
	if err := unix.Statfs(clean, &sfs); err != nil {
		return ""
	}
	var st unix.Stat_t
	if err := unix.Stat(clean, &st); err != nil {
		return ""
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Ino == 0 {
		return ""
	}
	return fmt.Sprintf("darwin:%d:%d:%d", sfs.Fsid.Val[0], sfs.Fsid.Val[1], st.Ino)
}

// ResolveDirLocator resolves a persisted locator back to its current directory
// path when oldPath no longer exists.
func ResolveDirLocator(_ string, locator string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(locator), ":")
	if len(parts) != 4 || parts[0] != "darwin" {
		return "", false
	}
	v0, err0 := strconv.ParseInt(parts[1], 10, 32)
	v1, err1 := strconv.ParseInt(parts[2], 10, 32)
	ino, err2 := strconv.ParseUint(parts[3], 10, 64)
	if err0 != nil || err1 != nil || err2 != nil || ino == 0 {
		return "", false
	}

	darwinFSGetPathOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		sym, err := purego.Dlsym(lib, "fsgetpath")
		if err != nil {
			return
		}
		purego.RegisterFunc(&darwinFSGetPathFn, sym)
	})
	if darwinFSGetPathFn == nil {
		return "", false
	}

	fsid := [2]int32{int32(v0), int32(v1)}
	var buf [4096]byte
	n := darwinFSGetPathFn(&buf[0], uintptr(len(buf)), &fsid, ino)
	if n <= 0 {
		return "", false
	}
	if int(n) > len(buf) {
		n = int64(len(buf))
	}
	end := bytes.IndexByte(buf[:n], 0)
	if end < 0 {
		end = int(n)
	}
	raw := string(buf[:end])
	if raw == "" {
		return "", false
	}
	p := filepath.Clean(raw)
	if p == "" || p == "." || IsTrashPath(p) {
		return "", false
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		return "", false
	}
	if CaptureDirLocator(p) != locator {
		return "", false
	}
	return p, true
}

func actualExistingPath(clean string) string {
	linfo, lerr := os.Lstat(clean)
	if lerr != nil || isSymlinkOrReparseInfo(linfo) || (!linfo.IsDir() && !linfo.Mode().IsRegular()) {
		return clean
	}
	fd, err := unix.Open(clean, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return clean
	}
	defer unix.Close(fd)
	var buf [4096]byte
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(unix.F_GETPATH), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return clean
	}
	n := bytes.IndexByte(buf[:], 0)
	if n <= 0 {
		return clean
	}
	p := filepath.Clean(string(buf[:n]))
	if p == "" || p == "." {
		return clean
	}
	return p
}
