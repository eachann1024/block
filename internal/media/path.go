package media

import (
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var ErrInvalid = errors.New("invalid or out-of-scope media")

// WebLink returns the URL that can be exposed as an external hyperlink.
func WebLink(raw string) string {
	s := strings.Trim(raw, " ")
	if !validReference(s) || strings.ContainsAny(s, "\\<>\"") || strings.ContainsFunc(s, unicode.IsSpace) {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		if u.Hostname() != "" {
			return s
		}
	case "mailto":
		addr, err := url.PathUnescape(u.Opaque)
		if err == nil && validReference(addr) && strings.Contains(addr, "@") && !strings.ContainsAny(addr, "/\\") {
			return s
		}
	}
	return ""
}

func validReference(s string) bool {
	return s != "" && len(s) <= 16*1024 && !strings.ContainsFunc(s, unicode.IsControl)
}

// RelativePath resolves a document reference within an explicitly supplied
// directory. The result must still be opened through os.Root.
func RelativePath(dir, raw string) (string, error) {
	s := strings.Trim(raw, " ")
	if dir == "" || !validReference(s) {
		return "", ErrInvalid
	}
	if strings.HasPrefix(strings.ToLower(s), "file:") {
		if strings.Contains(s, `\`) {
			return "", ErrInvalid
		}
		u, err := url.Parse(s)
		if err != nil || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.User != nil ||
			u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") {
			return "", ErrInvalid
		}
		s = u.Path
		if os.PathSeparator == '\\' && len(s) >= 4 && s[2] == ':' {
			s = s[1:]
		}
	} else {
		// Markdown permits literal percent signs as well as percent-encoded paths.
		if decoded, err := url.PathUnescape(s); err == nil {
			s = decoded
		}
		s = filepath.ToSlash(s)
	}
	if !validReference(s) || strings.Contains(s, `\`) || strings.HasPrefix(s, "//") || strings.Contains(s, "://") {
		return "", ErrInvalid
	}
	p := filepath.FromSlash(s)
	// Disallow URI schemes, drive-relative paths, and NTFS alternate data streams.
	if strings.Contains(strings.TrimPrefix(p, filepath.VolumeName(p)), ":") {
		return "", ErrInvalid
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." {
			return "", ErrInvalid
		}
	}
	if filepath.IsAbs(p) {
		root, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		p, err = filepath.Rel(root, p)
		if err != nil {
			return "", err
		}
	}
	p = filepath.Clean(p)
	if p == "." || !filepath.IsLocal(p) {
		return "", ErrInvalid
	}
	return p, nil
}

func OpenLocal(dir, ref string, limit int64) (*os.File, os.FileInfo, error) {
	rel, err := RelativePath(dir, ref)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil || !fits(info, limit) {
		return nil, nil, ErrInvalid
	}
	f, err := openLocalFile(root, rel)
	if err != nil {
		return nil, nil, err
	}
	opened, err := f.Stat()
	if err != nil || !fits(opened, limit) || !os.SameFile(opened, info) {
		f.Close()
		return nil, nil, ErrInvalid
	}
	return f, opened, nil
}

func fits(info os.FileInfo, limit int64) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() >= 0 && info.Size() <= limit
}

func ReadFile(f *os.File, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return nil, ErrInvalid
	}
	return raw, nil
}

// LocalTarget checks a regular file before handing its canonical path to the
// OS for a user-initiated open. Byte consumers should use OpenLocal instead.
func LocalTarget(dir, ref string) (string, bool) {
	f, info, err := OpenLocal(dir, ref, 1<<62)
	if err != nil {
		return "", false
	}
	defer f.Close()
	path, err := filepath.EvalSymlinks(f.Name())
	if err != nil {
		return "", false
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", false
	}
	target, err := os.Stat(path)
	if err != nil || !os.SameFile(info, target) {
		return "", false
	}
	path, err = filepath.Abs(path)
	return path, err == nil
}
