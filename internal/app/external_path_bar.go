package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"block/internal/icons"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

// ExternalPathBarState tracks transient copy feedback and measured width for the
// Scheme C external-file breadcrumb bar.
type ExternalPathBarState struct {
	CopiedPath  string
	CopiedUntil time.Time
	NavWidth    float32
}

type pathCrumb struct {
	Label     string
	Path      string
	IsCur     bool
	Collapsed bool
}

var (
	macRootVolOnce sync.Once
	macRootVolName = "Macintosh HD"

	userHomeOnce sync.Once
	userHomeDir  string

	canonDirMu   sync.Mutex
	canonDirRaw  string
	canonDirReal string
)

func cachedUserHomeDir() string {
	userHomeOnce.Do(func() {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			userHomeDir = filepath.Clean(h)
		}
	})
	return userHomeDir
}

func macStartupVolumeName() string {
	macRootVolOnce.Do(func() {
		if runtime.GOOS != "darwin" {
			return
		}
		entries, err := os.ReadDir("/Volumes")
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.Type()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(filepath.Join("/Volumes", e.Name()))
			if err == nil && filepath.Clean(target) == "/" && e.Name() != "" {
				macRootVolName = e.Name()
				return
			}
		}
	})
	return macRootVolName
}

func canonicalNotebookDir(dir string) string {
	clean := filepath.Clean(dir)
	canonDirMu.Lock()
	defer canonDirMu.Unlock()
	if clean == canonDirRaw {
		return canonDirReal
	}
	realPath := clean
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		realPath = filepath.Clean(resolved)
	}
	canonDirRaw = clean
	canonDirReal = realPath
	return realPath
}

func pathInsideDir(cleanFile, cleanDir string) bool {
	if cleanFile == "" || cleanDir == "" {
		return false
	}
	rel, err := filepath.Rel(cleanDir, cleanFile)
	if err == nil && rel != "." && filepath.IsLocal(rel) {
		return true
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		relLower, errLower := filepath.Rel(strings.ToLower(cleanDir), strings.ToLower(cleanFile))
		if errLower == nil && relLower != "." && filepath.IsLocal(relLower) {
			return true
		}
	}
	return false
}

// isExternalFile reports whether filePath lies outside the active notebook folder.
// Returns false for empty paths; returns true when no notebook folder is active.
func isExternalFile(filePath, notebookDir string) bool {
	if strings.TrimSpace(filePath) == "" {
		return false
	}
	if strings.TrimSpace(notebookDir) == "" {
		return true
	}
	cleanFile := filepath.Clean(filePath)
	cleanDir := filepath.Clean(notebookDir)
	if pathInsideDir(cleanFile, cleanDir) {
		return false
	}
	realDir := canonicalNotebookDir(cleanDir)
	if realDir != cleanDir && pathInsideDir(cleanFile, realDir) {
		return false
	}
	return true
}

// tabDirSuffixes returns a minimal disambiguating directory suffix for each tab
// whose display title collides with another open tab (e.g. "block" vs "~").
// Tabs with unique titles receive an empty suffix.
func tabDirSuffixes(tabs []string) map[string]string {
	groups := make(map[string][]string, len(tabs))
	for _, p := range tabs {
		title := tabTitle(p)
		groups[title] = append(groups[title], p)
	}
	out := make(map[string]string, len(tabs))
	home := cachedUserHomeDir()
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		for depth := 1; depth <= 6; depth++ {
			counts := make(map[string]int, len(group))
			cand := make(map[string]string, len(group))
			for _, p := range group {
				s := dirSuffixAtDepth(p, home, depth)
				cand[p] = s
				counts[s]++
			}
			unique := true
			for _, p := range group {
				if counts[cand[p]] > 1 {
					unique = false
					break
				}
			}
			for _, p := range group {
				out[p] = cand[p]
			}
			if unique {
				break
			}
		}
	}
	return out
}

func dirSuffixAtDepth(absPath, home string, depth int) string {
	dir := filepath.Clean(filepath.Dir(absPath))
	if dir == "" || dir == "." {
		return ""
	}
	if home != "" {
		if dir == home || ((runtime.GOOS == "darwin" || runtime.GOOS == "windows") && strings.EqualFold(dir, home)) {
			return "~"
		}
	}
	var parts []string
	cur := dir
	for i := 0; i < depth; i++ {
		if home != "" && (cur == home || ((runtime.GOOS == "darwin" || runtime.GOOS == "windows") && strings.EqualFold(cur, home))) {
			parts = append([]string{"~"}, parts...)
			break
		}
		base := filepath.Base(cur)
		if base == "" || base == "." || base == string(filepath.Separator) {
			if len(parts) == 0 {
				if vol := filepath.VolumeName(cur); vol != "" {
					return vol
				}
				return "/"
			}
			break
		}
		parts = append([]string{base}, parts...)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return strings.Join(parts, "/")
}

// buildPathBreadcrumbs splits an absolute file path into Finder-style breadcrumb segments.
func buildPathBreadcrumbs(absPath string) []pathCrumb {
	clean := filepath.Clean(absPath)
	if clean == "" || clean == "." {
		return nil
	}
	var crumbs []pathCrumb

	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(clean)
		rest := strings.TrimPrefix(clean, vol)
		rest = strings.TrimLeft(rest, `\/`)
		rootPath := vol + string(filepath.Separator)
		if vol != "" {
			crumbs = append(crumbs, pathCrumb{Label: vol, Path: rootPath})
		}
		if rest != "" {
			segs := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
			acc := rootPath
			for i, seg := range segs {
				acc = filepath.Join(acc, seg)
				crumbs = append(crumbs, pathCrumb{
					Label: seg,
					Path:  acc,
					IsCur: i == len(segs)-1,
				})
			}
		} else if len(crumbs) > 0 {
			crumbs[len(crumbs)-1].IsCur = true
		}
		return crumbs
	}

	if !strings.HasPrefix(clean, "/") {
		return []pathCrumb{{Label: filepath.Base(clean), Path: clean, IsCur: true}}
	}

	segs := strings.FieldsFunc(strings.TrimPrefix(clean, "/"), func(r rune) bool { return r == '/' })
	if runtime.GOOS == "darwin" {
		if len(segs) >= 2 && segs[0] == "Volumes" {
			volRoot := filepath.Join("/Volumes", segs[1])
			crumbs = append(crumbs, pathCrumb{Label: segs[1], Path: volRoot})
			acc := volRoot
			for i := 2; i < len(segs); i++ {
				acc = filepath.Join(acc, segs[i])
				crumbs = append(crumbs, pathCrumb{
					Label: segs[i],
					Path:  acc,
					IsCur: i == len(segs)-1,
				})
			}
			if len(crumbs) > 0 && !crumbs[len(crumbs)-1].IsCur {
				crumbs[len(crumbs)-1].IsCur = true
			}
			return crumbs
		}
		crumbs = append(crumbs, pathCrumb{Label: macStartupVolumeName(), Path: "/"})
	} else {
		crumbs = append(crumbs, pathCrumb{Label: "/", Path: "/"})
	}

	acc := "/"
	for i, seg := range segs {
		acc = filepath.Join(acc, seg)
		crumbs = append(crumbs, pathCrumb{
			Label: seg,
			Path:  acc,
			IsCur: i == len(segs)-1,
		})
	}
	if len(crumbs) > 0 && !crumbs[len(crumbs)-1].IsCur {
		crumbs[len(crumbs)-1].IsCur = true
	}
	return crumbs
}

func estimateCrumbWidth(label string) float32 {
	var w float32 = theme.Space6 * 2 // horizontal padding
	for _, r := range label {
		switch {
		case r <= 0x7f:
			w += 7.4
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			w += 13.0
		default:
			w += 9.5
		}
	}
	return w
}

// fitPathBreadcrumbs collapses middle directory segments into "…" only when the
// full breadcrumb trail exceeds availW, always preserving the root and leaf filename.
func fitPathBreadcrumbs(crumbs []pathCrumb, availW float32) []pathCrumb {
	if availW <= 0 || len(crumbs) <= 3 {
		return crumbs
	}
	const sepW float32 = 12 // "›" + surrounding 2px gaps
	totalW := float32(0)
	for i, cr := range crumbs {
		if i > 0 {
			totalW += sepW
		}
		totalW += estimateCrumbWidth(cr.Label)
	}
	if totalW <= availW {
		return crumbs
	}

	first := crumbs[0]
	last := crumbs[len(crumbs)-1]
	ellipsis := pathCrumb{
		Label:     "…",
		Path:      crumbs[len(crumbs)-2].Path,
		Collapsed: true,
	}
	usedW := estimateCrumbWidth(first.Label) + sepW + estimateCrumbWidth(ellipsis.Label) + sepW + estimateCrumbWidth(last.Label)

	// Keep as many trailing parent directories as fit beside root + "…" + leaf.
	var tail []pathCrumb
	for i := len(crumbs) - 2; i >= 1; i-- {
		w := estimateCrumbWidth(crumbs[i].Label) + sepW
		if usedW+w > availW {
			break
		}
		usedW += w
		tail = append([]pathCrumb{crumbs[i]}, tail...)
	}

	// If every middle item fit after all, return original.
	if len(tail) == len(crumbs)-2 {
		return crumbs
	}
	out := make([]pathCrumb, 0, 3+len(tail))
	out = append(out, first, ellipsis)
	out = append(out, tail...)
	out = append(out, last)
	return out
}

func openDirInShellLabel() string {
	if theme.IsMacOS() {
		return theme.Tr("在访达中打开", "Open in Finder")
	}
	return theme.Tr("在文件管理器中打开", "Open in File Manager")
}

// renderExternalPathBar renders the Scheme C full-width 40px top breadcrumb bar
// inside the editor card for files outside the active workspace folder.
func renderExternalPathBar(
	c *ui.Context,
	absPath string,
	st *ExternalPathBarState,
	onOpenDir func(dirPath string),
	onRevealFile func(filePath string),
	onCopied func(),
) {
	crumbs := buildPathBreadcrumbs(absPath)
	if len(crumbs) == 0 {
		return
	}
	pal := theme.Ocean()
	chrome := pal.ShellChrome()

	c.Key("external-path-bar")
	wrap := ui.Column(c).FillWidth().Height(theme.ExternalPathBarHeight).Shrink(0)
	blockui.Probe(wrap, "external-path-bar")
	wrap.Children(func() {
		bar := ui.Row(c.Key("external-path-bar-row")).Grow(1).FillWidth().
			Padding(0, theme.Space12, 0, theme.Space16).Gap(theme.Space8).AlignItems(ui.Center)
		bar.Children(func() {
			nav := ui.Row(c.Key("external-path-nav")).Grow(1).MinWidth(0).FillHeight().
				AlignItems(ui.Center).Gap(theme.Space2).Clip().
				Label(theme.Tr("文件路径", "File Path"))
			blockui.Probe(nav, "external-path-nav")

			navW := nav.Bounds().W
			if st != nil {
				if navW > 0 && navW != st.NavWidth {
					st.NavWidth = navW
					c.After(16 * time.Millisecond)
				} else if navW <= 0 && st.NavWidth > 0 {
					navW = st.NavWidth
				}
			}
			visible := fitPathBreadcrumbs(crumbs, navW)

			nav.Children(func() {
				for i, seg := range visible {
					if i > 0 {
						ui.Text(c, "›").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.Placeholder).SingleLine().Shrink(0)
					}
					if seg.IsCur {
						cur := ui.Row(c.Key("ext-crumb-cur")).Height(theme.HeightChrome).
							PaddingX(theme.Space6).AlignItems(ui.Center).Shrink(1).MinWidth(0)
						blockui.Tooltip(c, cur, absPath)
						blockui.Probe(cur, "ext-crumb-cur")
						cur.Children(func() {
							ui.Text(c, seg.Label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								FontWeight(theme.WeightRegular).TextColor(pal.Text).
								Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
						})
						continue
					}

					segPath := seg.Path
					segKey := fmt.Sprintf("ext-crumb-%d-%s", i, segPath)
					btn := ui.ButtonBase(c.Key(segKey)).Height(theme.HeightChrome).
						PaddingX(theme.Space6).Shrink(0).Center().Radius(theme.RadiusSM).
						Cursor(ui.CursorPointer).FocusRing(false).Label(seg.Label)
					hovered := btn.Hovered()
					pressed := btn.Pressed()
					bg := ui.Transparent
					fg := pal.TextSecondary
					switch {
					case pressed:
						bg = pal.Selected
						fg = pal.Text
					case hovered:
						bg = pal.Hover
						fg = pal.Text
					}
					blockui.AnimateSurfaceColors(btn, bg, ui.Transparent, false)
					blockui.TactileButton(btn, hovered, pressed, false)
					if seg.Collapsed {
						blockui.RichTooltip(c, btn, blockui.TooltipSpec{
							Rows: []blockui.TooltipRow{
								{Trigger: absPath},
								{Detail: openDirInShellLabel()},
							},
						})
					} else {
						blockui.RichTooltip(c, btn, blockui.TooltipSpec{
							Rows: []blockui.TooltipRow{
								{Trigger: segPath},
								{Detail: openDirInShellLabel()},
							},
						})
					}
					btn.Children(func() {
						ui.Text(c, seg.Label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							FontWeight(theme.WeightRegular).TextColor(fg).SingleLine()
					})
					blockui.Probe(btn, fmt.Sprintf("ext-crumb-%d", i))
					if btn.Clicked() && onOpenDir != nil {
						onOpenDir(segPath)
					}
				}
			})

			// Right-side always-visible action buttons: Copy Path & Reveal in Finder.
			ui.Row(c.Key("external-path-actions")).Shrink(0).AlignItems(ui.Center).Gap(theme.Space4).Children(func() {
				now := c.Now()
				copiedActive := st != nil && st.CopiedPath == absPath && !st.CopiedUntil.IsZero() && now.Before(st.CopiedUntil)
				if copiedActive {
					if rem := st.CopiedUntil.Sub(now); rem > 0 {
						c.After(rem + 5*time.Millisecond)
					}
				}
				copyIcon := icons.Copy
				copyTip := theme.Tr("复制路径", "Copy Path")
				if copiedActive {
					copyIcon = icons.Check
					copyTip = theme.Tr("已复制", "Copied")
				}
				copyBtn := blockui.IconButton(c, copyIcon, blockui.IconButtonOptions{
					ID:       "ext-path-copy",
					Size:     theme.HeightCompact,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusMD,
					Tooltip:  copyTip,
					Active:   copiedActive,
				})
				if copyBtn.Clicked() {
					c.Services().WriteClipboard(absPath)
					if onCopied != nil {
						onCopied()
					}
					if st != nil {
						st.CopiedPath = absPath
						st.CopiedUntil = c.Now().Add(1500 * time.Millisecond)
						c.After(1505 * time.Millisecond)
					}
				}

				revBtn := blockui.IconButton(c, icons.FolderReveal, blockui.IconButtonOptions{
					ID:       "ext-path-reveal",
					Size:     theme.HeightCompact,
					IconSize: theme.IconAction,
					Radius:   theme.RadiusMD,
					Tooltip:  revealFileLabel(),
				})
				if revBtn.Clicked() && onRevealFile != nil {
					onRevealFile(absPath)
				}
			})
		})
		ui.Box(c).Height(1).FillWidth().Shrink(0).Background(chrome.PathBarBorder)
	})
}
