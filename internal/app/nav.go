package app

// NavHistory implements browser-style Back/Forward note history (Cmd+[ / Cmd+]),
// mirroring thinkly-fast/src/app_nav.rs.
type NavHistory struct {
	entries []string
	cursor  int // -1 when empty
}

// NewNavHistory creates an empty navigation history.
func NewNavHistory() *NavHistory {
	return &NavHistory{cursor: -1}
}

// Push records visiting path, truncating any forward branch unless path equals current.
func (h *NavHistory) Push(path string) {
	if path == "" {
		return
	}
	if h.cursor >= 0 && h.cursor < len(h.entries) && h.entries[h.cursor] == path {
		return
	}
	if h.cursor+1 < len(h.entries) {
		h.entries = append([]string(nil), h.entries[:h.cursor+1]...)
	}
	// Deduplicate adjacent identical entries.
	if n := len(h.entries); n > 0 && h.entries[n-1] == path {
		h.cursor = n - 1
		return
	}
	const maxNavEntries = 128
	h.entries = append(h.entries, path)
	if len(h.entries) > maxNavEntries {
		drop := len(h.entries) - maxNavEntries
		h.entries = append([]string(nil), h.entries[drop:]...)
	}
	h.cursor = len(h.entries) - 1
}

// CanBack reports whether there is a previous entry.
func (h *NavHistory) CanBack() bool {
	return h.cursor > 0 && len(h.entries) > 1
}

// CanForward reports whether there is a forward entry.
func (h *NavHistory) CanForward() bool {
	return h.cursor >= 0 && h.cursor+1 < len(h.entries)
}

// Back moves back one step and returns the target path, or "" if unavailable.
func (h *NavHistory) Back() string {
	if !h.CanBack() {
		return ""
	}
	h.cursor--
	return h.entries[h.cursor]
}

// Forward moves forward one step and returns the target path, or "" if unavailable.
func (h *NavHistory) Forward() string {
	if !h.CanForward() {
		return ""
	}
	h.cursor++
	return h.entries[h.cursor]
}

// ReplacePath updates renamed paths in-place so history stays valid after rename/move.
func (h *NavHistory) ReplacePath(oldPath, newPath string) {
	if oldPath == "" || newPath == "" {
		return
	}
	for i := range h.entries {
		if h.entries[i] == oldPath {
			h.entries[i] = newPath
		}
	}
}

// RemovePath removes deleted paths and clamps cursor.
func (h *NavHistory) RemovePath(target string) {
	if target == "" || len(h.entries) == 0 {
		return
	}
	next := make([]string, 0, len(h.entries))
	newCursor := h.cursor
	for i, p := range h.entries {
		if p == target {
			if i <= h.cursor {
				newCursor--
			}
			continue
		}
		// Collapse consecutive duplicates created by removal.
		if len(next) > 0 && next[len(next)-1] == p {
			if i <= h.cursor {
				newCursor--
			}
			continue
		}
		next = append(next, p)
	}
	h.entries = next
	if len(h.entries) == 0 {
		h.cursor = -1
	} else if newCursor < 0 {
		h.cursor = 0
	} else if newCursor >= len(h.entries) {
		h.cursor = len(h.entries) - 1
	} else {
		h.cursor = newCursor
	}
}

// ClosedTabs maintains a LIFO stack of recently closed tab paths for Cmd+Shift+T,
// mirroring thinkly-fast/src/app_tabs_reopen.rs.
type ClosedTabs struct {
	stack []string
}

// NewClosedTabs creates an empty closed-tab stack.
func NewClosedTabs() *ClosedTabs {
	return &ClosedTabs{}
}

// Push records one closed tab path on top of the stack.
func (c *ClosedTabs) Push(path string) {
	if path == "" {
		return
	}
	// Remove older occurrence so the latest close wins.
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.stack[i] == path {
			c.stack = append(c.stack[:i], c.stack[i+1:]...)
		}
	}
	const maxClosed = 32
	c.stack = append(c.stack, path)
	if len(c.stack) > maxClosed {
		c.stack = append([]string(nil), c.stack[len(c.stack)-maxClosed:]...)
	}
}

// Pop removes and returns the most recently closed tab path, or "".
func (c *ClosedTabs) Pop() string {
	n := len(c.stack)
	if n == 0 {
		return ""
	}
	top := c.stack[n-1]
	c.stack = c.stack[:n-1]
	return top
}

// Len returns the number of reopenable tabs.
func (c *ClosedTabs) Len() int {
	return len(c.stack)
}

// Remove drops a deleted note path from the closed-tab stack.
func (c *ClosedTabs) Remove(path string) {
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.stack[i] == path {
			c.stack = append(c.stack[:i], c.stack[i+1:]...)
		}
	}
}

// TabStripOps provides pure multi-tab manipulation helpers (open, close, close others,
// close to right, close all, cycle next/prev, select 1..9) matching Tabs-1-Titlebar.
type TabStripOps struct{}

// OpenOrFocus ensures path is in tabs (inserting right after activeTab when new, or at end)
// and returns (updatedTabs, newActiveTab).
func (TabStripOps) OpenOrFocus(tabs []string, activeTab, path string) ([]string, string) {
	if path == "" {
		return tabs, activeTab
	}
	for _, t := range tabs {
		if t == path {
			return tabs, path
		}
	}
	out := make([]string, 0, len(tabs)+1)
	inserted := false
	for _, t := range tabs {
		out = append(out, t)
		if t == activeTab && !inserted {
			out = append(out, path)
			inserted = true
		}
	}
	if !inserted {
		out = append(out, path)
	}
	return out, path
}

// CloseTab removes target from tabs and selects an adjacent survivor (prefer right, else left).
func (TabStripOps) CloseTab(tabs []string, activeTab, target string) (nextTabs []string, nextActive string, closed bool) {
	idx := -1
	for i, t := range tabs {
		if t == target {
			idx = i
			break
		}
	}
	if idx < 0 {
		return tabs, activeTab, false
	}
	nextTabs = make([]string, 0, len(tabs)-1)
	nextTabs = append(nextTabs, tabs[:idx]...)
	nextTabs = append(nextTabs, tabs[idx+1:]...)
	if len(nextTabs) == 0 {
		return nextTabs, "", true
	}
	if activeTab != target {
		return nextTabs, activeTab, true
	}
	if idx < len(nextTabs) {
		return nextTabs, nextTabs[idx], true
	}
	return nextTabs, nextTabs[len(nextTabs)-1], true
}

// CloseOthers keeps only keepPath open and returns closed paths in left-to-right order.
func (TabStripOps) CloseOthers(tabs []string, keepPath string) (nextTabs []string, nextActive string, closedPaths []string) {
	found := false
	for _, t := range tabs {
		if t == keepPath {
			found = true
		} else {
			closedPaths = append(closedPaths, t)
		}
	}
	if !found {
		return tabs, keepPath, nil
	}
	return []string{keepPath}, keepPath, closedPaths
}

// CloseToRight closes all tabs strictly to the right of pivotPath.
func (TabStripOps) CloseToRight(tabs []string, activeTab, pivotPath string) (nextTabs []string, nextActive string, closedPaths []string) {
	pivot := -1
	for i, t := range tabs {
		if t == pivotPath {
			pivot = i
			break
		}
	}
	if pivot < 0 || pivot+1 >= len(tabs) {
		return tabs, activeTab, nil
	}
	nextTabs = append([]string(nil), tabs[:pivot+1]...)
	closedPaths = append([]string(nil), tabs[pivot+1:]...)
	nextActive = activeTab
	stillOpen := false
	for _, t := range nextTabs {
		if t == activeTab {
			stillOpen = true
			break
		}
	}
	if !stillOpen {
		nextActive = pivotPath
	}
	return nextTabs, nextActive, closedPaths
}

// Cycle moves delta (+1 next, -1 prev) modulo len(tabs).
func (TabStripOps) Cycle(tabs []string, activeTab string, delta int) string {
	n := len(tabs)
	if n == 0 {
		return ""
	}
	cur := 0
	for i, t := range tabs {
		if t == activeTab {
			cur = i
			break
		}
	}
	next := ((cur + delta) % n + n) % n
	return tabs[next]
}

// SelectOneBased selects Cmd+1..Cmd+9 (Cmd+9 selects the last tab, matching browser/Block convention).
func (TabStripOps) SelectOneBased(tabs []string, num int) string {
	n := len(tabs)
	if n == 0 || num < 1 {
		return ""
	}
	if num == 9 {
		return tabs[n-1]
	}
	if num > n {
		return ""
	}
	return tabs[num-1]
}
