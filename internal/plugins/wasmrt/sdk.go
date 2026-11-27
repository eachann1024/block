package wasmrt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaxWasmEntryBytes caps the maximum size of a standalone .wasm plugin entry.
const MaxWasmEntryBytes = 32 * 1024 * 1024

const maxWasmEntryBytes = MaxWasmEntryBytes

// ValidateEntryFile verifies that entryRel resolves to a regular, non-symlink
// file inside pluginDir with an allowed extension and optional maxBytes cap.
func ValidateEntryFile(pluginDir, entryRel string, maxBytes int64, allowedExts ...string) error {
	cleanRel, err := ValidateEntryPath(entryRel, allowedExts...)
	if err != nil {
		return err
	}
	root, info, err := openRegularPluginEntry(pluginDir, cleanRel)
	if err != nil {
		return err
	}
	_ = root.Close()
	if maxBytes > 0 && info.Size() > maxBytes {
		return fmt.Errorf("plugin entry exceeds %d bytes", maxBytes)
	}
	return nil
}

// ValidateEntryPath verifies that raw is a clean relative slash path inside the
// plugin package whose extension matches one of allowedExts.
func ValidateEntryPath(raw string, allowedExts ...string) (string, error) {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return "", errors.New("entry path must be a non-empty relative path without surrounding whitespace")
	}
	if strings.ContainsAny(raw, "\\:") || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("entry path %q must be a relative slash-separated path", raw)
	}
	if strings.Contains(raw, "..") {
		return "", fmt.Errorf("entry path %q must not contain \"..\"", raw)
	}
	local := filepath.FromSlash(raw)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("entry path %q escapes plugin directory", raw)
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("entry path %q escapes plugin directory", raw)
	}
	if len(allowedExts) > 0 {
		ext := strings.ToLower(path.Ext(clean))
		matched := false
		for _, want := range allowedExts {
			if ext == want {
				matched = true
				break
			}
		}
		if !matched {
			return "", fmt.Errorf("entry path %q must have extension %s", raw, strings.Join(allowedExts, " or "))
		}
	}
	return clean, nil
}

func checkPluginRootParents(root *os.Root, rel string) error {
	dir := filepath.Dir(filepath.FromSlash(rel))
	if dir == "." {
		return nil
	}
	parent := ""
	for part := range strings.SplitSeq(dir, string(filepath.Separator)) {
		parent = filepath.Join(parent, part)
		info, err := root.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("plugin entry parent must be a directory")
		}
	}
	return nil
}

func openRegularPluginEntry(pluginDir, rel string) (*os.Root, os.FileInfo, error) {
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return nil, nil, err
	}
	local := filepath.FromSlash(rel)
	if err := checkPluginRootParents(root, rel); err != nil {
		root.Close()
		return nil, nil, err
	}
	info, err := root.Lstat(local)
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		root.Close()
		return nil, nil, errors.New("plugin entry must be a regular file")
	}
	f, err := root.Open(local)
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	actual, err := f.Stat()
	_ = f.Close()
	if err != nil || !os.SameFile(info, actual) {
		root.Close()
		return nil, nil, errors.New("plugin entry changed while opening")
	}
	return root, actual, nil
}

func readRegularPluginEntry(root *os.Root, rel string, limit int64) ([]byte, error) {
	local := filepath.FromSlash(rel)
	info, err := root.Lstat(local)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("plugin entry must be a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("plugin entry exceeds %d bytes", limit)
	}
	f, err := root.Open(local)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("plugin entry exceeds %d bytes", limit)
	}
	return data, nil
}
