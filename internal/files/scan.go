package files

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"block/internal/model"
	"block/internal/theme"
)

const maxScanDepth = 256

// ScanOptions configures directory filtering and custom sibling ordering.
type ScanOptions struct {
	HideNonMarkdown bool
	HideAssets      bool
	AssetsFolder    string
	FileOrder       map[string][]string
	SortMode        model.TreeSortMode
}

var ignoredCommonNames = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"__pycache__":  true,
	".next":        true,
	".nuxt":        true,
	".venv":        true,
	"venv":         true,
}

var markdownExtensions = map[string]bool{
	"md":       true,
	"markdown": true,
	"mdown":    true,
	"mkd":      true,
	"mkdn":     true,
	"mdwn":     true,
	"mdtxt":    true,
	"mdtext":   true,
	"mdx":      true,
}

// IsMarkdown reports whether name has a supported Markdown file extension.
func IsMarkdown(name string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" {
		return false
	}
	return markdownExtensions[ext]
}

func isCommonIgnoredName(name string) bool {
	if strings.HasPrefix(name, "~$") {
		return true
	}
	if ignoredCommonNames[name] {
		return true
	}
	return strings.EqualFold(name, "thumbs.db") || strings.EqualFold(name, "desktop.ini")
}

func isIgnoredFile(name string, opts ScanOptions) bool {
	if strings.HasPrefix(name, ".") || isCommonIgnoredName(name) {
		return true
	}
	if opts.HideNonMarkdown && !IsMarkdown(name) {
		return true
	}
	return false
}

func isIgnoredDir(name string, opts ScanOptions) bool {
	if theme.IsFolderHidden(name, opts.AssetsFolder, opts.HideAssets) {
		return true
	}
	return isCommonIgnoredName(name)
}

func entryDisplayName(rawName string, isDir bool) string {
	if isDir || !IsMarkdown(rawName) {
		return rawName
	}
	ext := filepath.Ext(rawName)
	return strings.TrimSuffix(rawName, ext)
}

// ScanNotebook walks root recursively and returns sorted FileEntry trees.
// Directory symlinks are not followed to avoid cycles and workspace escapes.
func ScanNotebook(root string, opts ScanOptions) ([]model.FileEntry, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("notebook root is empty")
	}
	cleanRoot := filepath.Clean(root)
	return readScanLevel(cleanRoot, opts, 0)
}

func readScanLevel(dir string, opts ScanOptions, depth int) ([]model.FileEntry, error) {
	if depth > maxScanDepth {
		return nil, errors.New(theme.Tr("目录层级超过 256 层", "Directory depth exceeds 256 levels"))
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	out := make([]model.FileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		rawName := de.Name()
		fullPath := filepath.Join(dir, rawName)
		mode := de.Type()

		if mode.IsDir() {
			if isIgnoredDir(rawName, opts) {
				continue
			}
			children, childErr := readScanLevel(fullPath, opts, depth+1)
			if childErr != nil {
				LogWarnf("scan directory %s failed: %v", fullPath, childErr)
				children = nil
			}
			var modUnix int64
			if info, infoErr := de.Info(); infoErr == nil {
				modUnix = info.ModTime().Unix()
			}
			out = append(out, model.FileEntry{
				Path:       fullPath,
				Name:       rawName,
				RawName:    rawName,
				IsDir:      true,
				IsMarkdown: false,
				ModUnix:    modUnix,
				Children:   children,
			})
			continue
		}

		// Regular files, or symlinks that resolve to regular files (never dir symlinks).
		var info fs.FileInfo
		if mode.IsRegular() {
			info, _ = de.Info()
		} else if mode&os.ModeSymlink != 0 {
			st, statErr := os.Stat(fullPath)
			if statErr != nil || !st.Mode().IsRegular() {
				continue
			}
			info = st
		} else {
			continue
		}

		if isIgnoredFile(rawName, opts) {
			continue
		}

		var modUnix, size int64
		if info != nil {
			modUnix = info.ModTime().Unix()
			size = info.Size()
		}
		isMD := IsMarkdown(rawName)
		out = append(out, model.FileEntry{
			Path:       fullPath,
			Name:       entryDisplayName(rawName, false),
			RawName:    rawName,
			IsDir:      false,
			IsMarkdown: isMD,
			ModUnix:    modUnix,
			Size:       size,
		})
	}

	sortEntries(dir, out, opts.FileOrder, opts.SortMode)
	return out, nil
}

func isDefaultNewFolderName(rawName string) bool {
	for _, base := range []string{"新建文件夹", "New Folder"} {
		if rawName == base {
			return true
		}
		if len(rawName) > len(base)+1 && strings.HasPrefix(rawName, base+" ") {
			rest := rawName[len(base)+1:]
			allDigits := true
			for i := 0; i < len(rest); i++ {
				if !isASCIIDigit(rest[i]) {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	return false
}

type sortableFileEntry struct {
	entry        model.FileEntry
	isDefaultNew bool
}

func sortEntries(parentDir string, entries []model.FileEntry, fileOrder map[string][]string, mode model.TreeSortMode) {
	if len(entries) <= 1 {
		return
	}
	items := make([]sortableFileEntry, len(entries))
	for i, e := range entries {
		items[i] = sortableFileEntry{
			entry:        e,
			isDefaultNew: e.IsDir && isDefaultNewFolderName(e.RawName),
		}
	}
	slices.SortFunc(items, func(ai, bi sortableFileEntry) int {
		a, b := &ai.entry, &bi.entry
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		if a.IsDir {
			if ai.isDefaultNew != bi.isDefaultNew {
				if !ai.isDefaultNew {
					return -1
				}
				return 1
			}
			if ai.isDefaultNew && bi.isDefaultNew {
				if a.ModUnix != b.ModUnix {
					if a.ModUnix < b.ModUnix {
						return -1
					}
					return 1
				}
				if c := naturalCmp(a.RawName, b.RawName); c != 0 {
					return c
				}
				if a.RawName != b.RawName {
					if a.RawName < b.RawName {
						return -1
					}
					return 1
				}
				if a.Path < b.Path {
					return -1
				}
				if a.Path > b.Path {
					return 1
				}
				return 0
			}
		}
		if mode == model.TreeSortModified && a.ModUnix != b.ModUnix {
			if a.ModUnix > b.ModUnix {
				return -1
			}
			return 1
		}
		if c := naturalCmp(a.RawName, b.RawName); c != 0 {
			return c
		}
		if a.RawName != b.RawName {
			if a.RawName < b.RawName {
				return -1
			}
			return 1
		}
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	for i := range items {
		entries[i] = items[i].entry
	}

	if mode != model.TreeSortManual || len(fileOrder) == 0 || len(entries) <= 1 {
		return
	}
	order, ok := lookupFileOrder(fileOrder, parentDir)
	if !ok || len(order) == 0 {
		return
	}

	ranks := make(map[string]int, len(order))
	for idx, item := range order {
		if item == "" {
			continue
		}
		p := item
		if !filepath.IsAbs(p) {
			p = filepath.Join(parentDir, p)
		}
		p = filepath.Clean(p)
		if _, exists := ranks[p]; !exists {
			ranks[p] = idx
		}
	}
	if len(ranks) == 0 {
		return
	}

	rankOf := func(p string) int {
		if r, exists := ranks[filepath.Clean(p)]; exists {
			return r
		}
		return math.MaxInt
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return rankOf(entries[i].Path) < rankOf(entries[j].Path)
	})
}

func lookupFileOrder(fileOrder map[string][]string, parentDir string) ([]string, bool) {
	if order, ok := fileOrder[parentDir]; ok {
		return order, true
	}
	clean := filepath.Clean(parentDir)
	if order, ok := fileOrder[clean]; ok {
		return order, true
	}
	return nil, false
}

// naturalCmp performs deterministic natural ordering matching src/files/sort.rs:
// ASCII digit runs compare numerically (stripping leading zeros, breaking ties
// by digit run length), and non-digit Unicode runes compare case-insensitively.
func naturalCmp(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if isASCIIDigit(a[0]) && isASCIIDigit(b[0]) {
			na, restA := takeNumber(a)
			nb, restB := takeNumber(b)
			a, b = restA, restB

			sa := strings.TrimLeft(na, "0")
			sb := strings.TrimLeft(nb, "0")
			if len(sa) != len(sb) {
				if len(sa) < len(sb) {
					return -1
				}
				return 1
			}
			if sa != sb {
				if sa < sb {
					return -1
				}
				return 1
			}
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			continue
		}

		ra, sizeA := utf8.DecodeRuneInString(a)
		rb, sizeB := utf8.DecodeRuneInString(b)
		a = a[sizeA:]
		b = b[sizeB:]

		la := unicode.ToLower(ra)
		lb := unicode.ToLower(rb)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
	}

	if len(a) == len(b) {
		return 0
	}
	if len(a) == 0 {
		return -1
	}
	return 1
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func takeNumber(s string) (string, string) {
	i := 0
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}
