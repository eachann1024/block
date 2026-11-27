//go:build darwin || linux

package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type dirHandle = *os.File

func closeDirHandle(h *dirHandle) error {
	if h == nil || *h == nil {
		return nil
	}
	f := *h
	*h = nil
	return f.Close()
}

func openUnixDirFile(clean string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(clean)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.IsDir() {
		_ = f.Close()
		return nil, nil, errors.New("not a directory")
	}
	return f, info, nil
}

func verifyLiveDirPath(f *os.File, rawPath string) (string, bool) {
	if f == nil || rawPath == "" {
		return "", false
	}
	p := filepath.Clean(rawPath)
	if p == "" || p == "." || IsTrashPath(p) {
		return "", false
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		return "", false
	}
	fInfo, err := f.Stat()
	if err != nil || !os.SameFile(fInfo, st) {
		return "", false
	}
	return p, true
}

// PathIdentity returns a stable identity token for path without following symlinks.
func PathIdentity(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	st, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return FileIdentityToken(st, nil)
}
