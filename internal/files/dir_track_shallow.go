//go:build linux || windows

package files

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	shallowLocatorMaxDepth = 2
	shallowLocatorMaxDirs  = 500
)

func resolveDirLocatorShallow(oldPath, locator string) (string, bool) {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(locator) == "" {
		return "", false
	}
	cleanOld := filepath.Clean(oldPath)
	parent1 := filepath.Dir(cleanOld)
	parent2 := filepath.Dir(parent1)

	type queueItem struct {
		dir   string
		depth int
	}
	var queue []queueItem
	scanned := make(map[string]bool)
	checked := make(map[string]bool)

	enqueueRoot := func(r string) {
		if r == "" || r == "." || r == cleanOld || IsTrashPath(r) || scanned[r] {
			return
		}
		scanned[r] = true
		queue = append(queue, queueItem{dir: r, depth: 0})
	}
	enqueueRoot(parent1)
	enqueueRoot(parent2)

	count := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= shallowLocatorMaxDepth {
			continue
		}
		entries, err := os.ReadDir(cur.dir)
		if err != nil {
			continue
		}
		for _, de := range entries {
			if !de.IsDir() {
				continue
			}
			name := de.Name()
			if strings.HasPrefix(name, ".") || isCommonIgnoredName(name) {
				continue
			}
			cand := filepath.Join(cur.dir, name)
			if IsTrashPath(cand) || checked[cand] {
				continue
			}
			checked[cand] = true
			count++
			if count > shallowLocatorMaxDirs {
				return "", false
			}
			if CaptureDirLocator(cand) == locator {
				return cand, true
			}
			if cur.depth+1 < shallowLocatorMaxDepth && !scanned[cand] {
				scanned[cand] = true
				queue = append(queue, queueItem{dir: cand, depth: cur.depth + 1})
			}
		}
	}
	return "", false
}
