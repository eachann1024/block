package app

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"block/internal/files"
	"block/internal/theme"
	"github.com/egoist/mygo"
)

// ResolveExternalPath normalizes a raw external file/folder argument or file:// URL
// into a clean local path, resolving leaf symlinks to their target file.
func ResolveExternalPath(rawPath, baseDir string) string {
	if strings.TrimSpace(rawPath) == "" {
		return ""
	}
	p := rawPath
	if _, err := os.Lstat(p); err != nil {
		p = strings.TrimSpace(rawPath)
	}
	if strings.HasPrefix(strings.ToLower(p), "file://") {
		if u, err := url.Parse(p); err == nil && strings.EqualFold(u.Scheme, "file") {
			p = filepath.FromSlash(u.Path)
			if runtime.GOOS == "windows" {
				if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
					p = `\\` + u.Host + p
				} else if len(p) >= 3 && p[0] == '\\' && p[2] == ':' {
					p = p[1:]
				}
			}
		}
	}
	if !filepath.IsAbs(p) {
		if strings.TrimSpace(baseDir) != "" {
			p = filepath.Join(baseDir, p)
		} else if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	clean := filepath.Clean(p)
	if lstat, err := os.Lstat(clean); err == nil && lstat.Mode()&os.ModeSymlink != 0 {
		if realPath, evalErr := filepath.EvalSymlinks(clean); evalErr == nil && realPath != "" {
			clean = filepath.Clean(realPath)
		}
	}
	return clean
}

// LocalFileURL encodes an absolute local filesystem path into a cross-platform file:// URL
// (supporting Unix paths, Windows drive paths, UNC paths, and percent-encoding spaces/non-ASCII bytes).
func LocalFileURL(absPath string) string {
	p := filepath.ToSlash(absPath)
	isUNC := strings.HasPrefix(absPath, `\\`) || strings.HasPrefix(p, "//")
	if isUNC || (len(p) >= 3 && p[1] == ':' && p[2] == '\\') {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	if isUNC {
		p = strings.TrimLeft(p, "/")
	} else if len(p) > 0 && p[0] != '/' {
		p = "/" + p
	}
	const hex = "0123456789ABCDEF"
	b := make([]byte, 0, len(p)+16)
	b = append(b, "file://"...)
	for i := 0; i < len(p); i++ {
		ch := p[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '/', ch == '-', ch == '_', ch == '.', ch == '~', ch == ':':
			b = append(b, ch)
		default:
			b = append(b, '%', hex[ch>>4], hex[ch&15])
		}
	}
	return string(b)
}

func sameFileOnDisk(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, errA := os.Stat(a)
	if errA != nil {
		return false
	}
	ib, errB := os.Stat(b)
	if errB != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

func findExistingTabForFile(tabs []string, clean string) string {
	for _, t := range tabs {
		if t == clean {
			return t
		}
	}
	for _, t := range tabs {
		if sameFileOnDisk(t, clean) {
			return t
		}
	}
	return ""
}

// OpenExternalTarget handles an external path passed from OS file associations,
// Dock drop, CLI arguments, or deep links: directories switch the active notebook,
// while files open in a new tab unless no file is currently open.
func (w *Workspace) OpenExternalTarget(rawPath, baseDir string) error {
	clean := ResolveExternalPath(rawPath, baseDir)
	if clean == "" {
		return errors.New("empty external path")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return w.SetNotebook(clean)
	}
	return w.OpenExternalFile(clean)
}

// OpenDeepLinkedFile opens files only inside notebooks already chosen in Block.
func (w *Workspace) OpenDeepLinkedFile(rawPath string) error {
	path := ResolveExternalPath(rawPath, "")
	if path == "" {
		return errors.New("empty note path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for _, root := range w.Notebooks() {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(realRoot, resolved)
		if err == nil && rel != "." && filepath.IsLocal(rel) {
			return w.OpenExternalFile(resolved)
		}
	}
	err = errors.New(theme.Tr("外部链接只能打开已添加笔记本中的文件", "External links can only open files in added notebooks"))
	w.setNotice(err.Error())
	return err
}

// OpenExternalFile opens an external file in a new tab when at least one file is
// already open, or directly in the single view when no file is currently open.
func (w *Workspace) OpenExternalFile(rawPath string) error {
	clean := ResolveExternalPath(rawPath, "")
	if clean == "" {
		return errors.New("empty note path")
	}

	w.mu.RLock()
	if existing := findExistingTabForFile(w.state.OpenTabs, clean); existing != "" {
		clean = existing
	}
	w.mu.RUnlock()

	if err := w.ensureTabLoaded(clean); err != nil {
		return err
	}
	w.activateExternalTab(clean)
	return nil
}

func (w *Workspace) activateExternalTab(clean string) {
	w.modals.CloseSettings()
	w.modals.CloseSearch()
	if w.searchWin != nil {
		w.searchWin.Hide()
	}
	w.exportCtrl.Close()
	w.historyView.Close()
	w.tabMenu.Close()
	w.sidebarUI.ClosePopups()
	w.edgeFloat.DismissAll(w)
	w.sidebarUI.FocusDir = ""

	w.mu.Lock()
	w.sidebarPinned = false
	if w.historyToast != nil && filepath.Clean(w.historyToast.NotePath) != clean {
		w.historyToast = nil
	}
	if prevSess := w.sessions[w.state.ActiveTab]; prevSess != nil && w.state.ActiveTab != clean {
		prevSess.ClearTransientOverlays()
	}
	if w.state.Folder != "" && strings.HasPrefix(clean, w.state.Folder+string(filepath.Separator)) {
		dir := filepath.Dir(clean)
		for dir != "" && dir != w.state.Folder && strings.HasPrefix(dir, w.state.Folder) {
			w.sidebarUI.ExpandedDirs[dir] = true
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	prevTabs := append([]string(nil), w.state.OpenTabs...)
	prevActive := w.state.ActiveTab
	if !slices.Contains(w.state.OpenTabs, clean) {
		w.state.OpenTabs = append(w.state.OpenTabs, clean)
	}
	w.state.ActiveTab = clean

	tabs := append([]string(nil), w.state.OpenTabs...)
	active := w.state.ActiveTab
	tabsChanged := active != prevActive || !stringSlicesEqual(tabs, prevTabs)
	w.mu.Unlock()

	w.nav.Push(clean)
	if tabsChanged {
		w.persistTabs(tabs, active)
	}
	w.RequestRedraw()
}

func (w *Workspace) handleWindowFileDrop(e *mygo.FileDropEvent) {
	if e == nil || len(e.Paths) == 0 {
		return
	}
	if w.modals.HasActiveModal() || w.hasActivePluginModal() || w.palette.IsOpen() || w.exportCtrl.IsOpen() || w.IsPageHistoryOpen() || (w.ai != nil && w.ai.IsLightboxOpen()) || (w.pluginsUI != nil && w.pluginsUI.IsStoreLightboxOpen()) || (w.ActiveSession() != nil && w.ActiveSession().IsLightboxOpen()) {
		return
	}
	if w.isAIPanelVisible(0) && w.ai.ContainsPoint(float32(e.X), float32(e.Y)) {
		var aiFiles []string
		for _, raw := range e.Paths {
			clean := ResolveExternalPath(raw, "")
			if clean == "" {
				continue
			}
			info, err := os.Stat(clean)
			if err != nil || info.IsDir() {
				continue
			}
			aiFiles = append(aiFiles, clean)
		}
		if len(aiFiles) > 0 {
			w.ai.AddAttachmentFiles(aiFiles)
			w.RequestRedraw()
		}
		return
	}
	sess := w.ActiveSession()
	dropInTitlebar := e.Y >= 0 && float32(e.Y) <= TitlebarHeight

	var mediaImports []string
	for _, raw := range e.Paths {
		clean := ResolveExternalPath(raw, "")
		if clean == "" {
			continue
		}
		info, err := os.Stat(clean)
		if err != nil || info.IsDir() {
			continue
		}
		if sess == nil || dropInTitlebar || files.IsMarkdown(clean) {
			_ = w.OpenExternalFile(clean)
			continue
		}
		mediaImports = append(mediaImports, clean)
	}

	if sess != nil && len(mediaImports) > 0 {
		if sess.ImportFiles(mediaImports, 0, false) > 0 {
			w.flushOrScheduleDirtySessions()
			w.RequestRedraw()
		}
	}
}
