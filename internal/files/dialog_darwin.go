//go:build darwin

package files

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type darwinFileIdentity struct {
	valid     bool
	dev       int32
	ino       uint64
	size      int64
	mtimeNano int64
}

var (
	darwinStartupRawExecPath string
	darwinStartupExecPath    string
	darwinStartupExecID      darwinFileIdentity
	darwinStartupCodeResPath string
	darwinStartupCodeResID   darwinFileIdentity
)

func init() {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return
	}
	darwinStartupRawExecPath = exe
	if resolved, rErr := filepath.EvalSymlinks(exe); rErr == nil && resolved != "" {
		exe = resolved
	}
	darwinStartupExecPath = exe
	darwinStartupExecID = readDarwinFileIdentity(exe)

	if idx := strings.LastIndex(exe, ".app/Contents/MacOS/"); idx > 0 {
		codeRes := filepath.Join(exe[:idx+4], "Contents", "_CodeSignature", "CodeResources")
		if id := readDarwinFileIdentity(codeRes); id.valid {
			darwinStartupCodeResPath = codeRes
			darwinStartupCodeResID = id
		}
	}
}

func readDarwinFileIdentity(path string) darwinFileIdentity {
	st, err := os.Stat(path)
	if err != nil {
		return darwinFileIdentity{}
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys == nil {
		return darwinFileIdentity{}
	}
	return darwinFileIdentity{
		valid:     true,
		dev:       sys.Dev,
		ino:       sys.Ino,
		size:      st.Size(),
		mtimeNano: st.ModTime().UnixNano(),
	}
}

// darwinBundleRootPath returns the ".app" bundle root containing the launched
// executable, or "" if Block was not launched from a macOS app bundle.
func darwinBundleRootPath() string {
	for _, p := range []string{darwinStartupRawExecPath, darwinStartupExecPath} {
		if idx := strings.LastIndex(p, ".app/Contents/MacOS/"); idx > 0 {
			return p[:idx+4]
		}
	}
	return ""
}

// darwinBundleIntact reports whether the running executable (and its app bundle
// code signature, when bundled) still match what was launched. When a hot
// in-place rebuild replaces Block.app on disk while an older process is still
// running, in-process NSOpenPanel / NSSavePanel can fail ViewBridge signature
// checks, so we fall back to the out-of-process osascript picker only then.
func darwinBundleIntact() bool {
	if darwinStartupRawExecPath == "" || darwinStartupExecPath == "" || !darwinStartupExecID.valid {
		return false
	}
	resolved, err := filepath.EvalSymlinks(darwinStartupRawExecPath)
	if err != nil || resolved != darwinStartupExecPath {
		return false
	}
	if readDarwinFileIdentity(darwinStartupExecPath) != darwinStartupExecID {
		return false
	}
	if darwinStartupCodeResID.valid {
		if readDarwinFileIdentity(darwinStartupCodeResPath) != darwinStartupCodeResID {
			return false
		}
	}
	return true
}
