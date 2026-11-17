package modals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	quickSlotCount     = 3
	quickHotkeyDebounce = 180 * time.Millisecond
)

type quickMeta struct {
	Names  []string `json:"names"`
	Active int      `json:"active"`
}

// QuickNoteController manages the standalone 3-slot Quick Note floating panel.
type QuickNoteController struct {
	dataDir string
	host    model.WorkspaceHost
	codec   model.MarkdownCodec

	mu           sync.Mutex
	win          *mygo.Window
	visible      bool
	activeSlot   int
	slots        [quickSlotCount]string
	names        [quickSlotCount]string
	lastTrigger  time.Time
	notice       string
	savedFlash   bool
	editorHandle ui.Handle
}

// NewQuickNoteController creates a QuickNoteController backed by `dataDir/quick`.
func NewQuickNoteController(dataDir string, host model.WorkspaceHost, codec model.MarkdownCodec) *QuickNoteController {
	q := &QuickNoteController{
		dataDir: dataDir,
		host:    host,
		codec:   codec,
		names: [quickSlotCount]string{
			defaultQuickSlotName(0),
			defaultQuickSlotName(1),
			defaultQuickSlotName(2),
		},
	}
	q.loadFromDisk()
	return q
}

func defaultQuickSlotName(slot int) string {
	if slot == 0 {
		return theme.Tr("灵感", "Ideas")
	}
	if theme.IsEn() {
		return fmt.Sprintf("Quick %d", slot+1)
	}
	return fmt.Sprintf("速记 %d", slot+1)
}

func isDefaultQuickSlotName(slot int, name string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed == fmt.Sprintf("速记 %d", slot+1) ||
		trimmed == fmt.Sprintf("Quick %d", slot+1) ||
		(slot == 0 && (trimmed == "灵感" || trimmed == "Ideas"))
}

func (q *QuickNoteController) quickDir() string {
	if strings.TrimSpace(q.dataDir) == "" {
		return ""
	}
	return filepath.Join(q.dataDir, "quick")
}

func (q *QuickNoteController) slotFilePath(slot int) string {
	dir := q.quickDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("slot-%d.md", slot+1))
}

func (q *QuickNoteController) loadFromDisk() {
	dir := q.quickDir()
	if dir == "" {
		return
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "names.json")); err == nil {
		var meta quickMeta
		if json.Unmarshal(raw, &meta) == nil {
			for i := 0; i < quickSlotCount; i++ {
				if i < len(meta.Names) && strings.TrimSpace(meta.Names[i]) != "" {
					q.names[i] = meta.Names[i]
				}
			}
			if meta.Active >= 0 && meta.Active < quickSlotCount {
				q.activeSlot = meta.Active
			}
		}
	}
	for i := 0; i < quickSlotCount; i++ {
		if b, err := os.ReadFile(q.slotFilePath(i)); err == nil {
			q.slots[i] = string(b)
		}
	}
}

func (q *QuickNoteController) saveMetaLocked() {
	dir := q.quickDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	meta := quickMeta{
		Names:  q.names[:],
		Active: q.activeSlot,
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	target := filepath.Join(dir, "names.json")
	tmp := target + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, target)
	}
}

func (q *QuickNoteController) saveSlotLocked(slot int) {
	p := q.slotFilePath(slot)
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(q.slots[slot]), 0o644)
}

// AttachDesktop registers the standalone floating Quick Note panel window.
// Red-light close is intercepted as Hide() so the main window is unaffected.
func (q *QuickNoteController) AttachDesktop() {
	q.mu.Lock()
	already := q.win != nil
	q.mu.Unlock()
	if already {
		return
	}

	create := func() {
		q.mu.Lock()
		if q.win != nil {
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:                theme.Tr("速记", "Quick Note"),
			Width:                440,
			Height:               520,
			MinWidth:             380,
			MinHeight:            320,
			Hidden:               true,
			Panel:                true,
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 16, Y: 16},
			StateKey:             "quick-note",
			Content:              ui.View(q.RenderView),
		})
		win.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			q.Hide()
		})

		q.mu.Lock()
		q.win = win
		vis := q.visible
		q.mu.Unlock()
		if vis {
			win.Show()
			win.Focus()
		}
	}

	if mygo.App.IsReady() {
		create()
	} else {
		mygo.App.WhenReady(create)
	}
}

// IsVisible reports whether the Quick Note panel is currently visible.
func (q *QuickNoteController) IsVisible() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.visible
}

// Show displays and focuses the Quick Note panel.
func (q *QuickNoteController) Show() {
	q.mu.Lock()
	q.visible = true
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Show()
		win.Focus()
		win.Invalidate()
	}
}

// Hide flushes the active slot and hides the Quick Note panel without closing the app.
func (q *QuickNoteController) Hide() {
	q.mu.Lock()
	q.visible = false
	q.saveSlotLocked(q.activeSlot)
	q.saveMetaLocked()
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Hide()
	}
}

// Toggle toggles the Quick Note panel visibility.
func (q *QuickNoteController) Toggle() {
	if q.IsVisible() {
		q.Hide()
	} else {
		q.Show()
	}
}

// TriggerHotkey toggles the panel with a built-in 180ms debounce window.
// Returns false when called again within 180ms of the previous accepted trigger.
func (q *QuickNoteController) TriggerHotkey() bool {
	now := time.Now()
	q.mu.Lock()
	if !q.lastTrigger.IsZero() && now.Sub(q.lastTrigger) < quickHotkeyDebounce {
		q.mu.Unlock()
		return false
	}
	q.lastTrigger = now
	q.mu.Unlock()

	q.Toggle()
	return true
}

// Text returns the Markdown text of the currently active Quick Note slot.
func (q *QuickNoteController) Text() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.slots[q.activeSlot]
}

// SetText updates the active Quick Note slot text and persists it to disk.
func (q *QuickNoteController) SetText(s string) {
	q.mu.Lock()
	q.slots[q.activeSlot] = s
	q.saveSlotLocked(q.activeSlot)
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

func (q *QuickNoteController) switchSlot(slot int) {
	if slot < 0 || slot >= quickSlotCount {
		return
	}
	q.mu.Lock()
	if q.activeSlot != slot {
		q.saveSlotLocked(q.activeSlot)
		q.activeSlot = slot
		q.notice = ""
		q.savedFlash = false
		q.saveMetaLocked()
	}
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

// SaveToNotebook exports the active slot's text as a new Markdown note inside the active notebook.
func (q *QuickNoteController) SaveToNotebook() (string, error) {
	q.mu.Lock()
	slot := q.activeSlot
	name := q.names[slot]
	raw := q.slots[slot]
	q.mu.Unlock()

	if strings.TrimSpace(raw) == "" {
		msg := theme.Tr("速记为空，无需保存", "Quick note is empty")
		q.setNotice(msg, false)
		return "", errors.New(msg)
	}

	folder := q.targetNotebookFolder()
	if folder == "" {
		msg := theme.Tr("请先在主窗口选择文件夹", "Choose a folder in the main window first")
		q.setNotice(msg, false)
		return "", errors.New(msg)
	}

	content := raw
	if q.codec != nil {
		doc := q.codec.Parse(raw)
		if serialized := q.codec.Serialize(doc); serialized != "" {
			content = serialized
		}
	}

	title := quickNoteTitle(slot, name, raw)
	outPath, err := createUniqueQuickNoteFile(folder, title, content)
	if err != nil {
		q.setNotice(err.Error(), false)
		return "", err
	}

	if q.host != nil {
		if err := q.host.WriteNote(outPath, content); err != nil {
			_ = os.Remove(outPath)
			q.setNotice(err.Error(), false)
			return "", err
		}
		q.host.RequestRedraw()
	}

	q.SetText("")
	q.setNotice(theme.Tr("已存入 ", "Saved to ")+filepath.Base(outPath), true)
	return outPath, nil
}

func (q *QuickNoteController) targetNotebookFolder() string {
	if q.host == nil {
		return ""
	}
	if active := strings.TrimSpace(q.host.ActiveNotebook()); active != "" {
		if st, err := os.Stat(active); err == nil && st.IsDir() {
			return active
		}
	}
	for _, nb := range q.host.Notebooks() {
		nb = strings.TrimSpace(nb)
		if nb == "" {
			continue
		}
		if st, err := os.Stat(nb); err == nil && st.IsDir() {
			return nb
		}
	}
	return ""
}

func (q *QuickNoteController) setNotice(text string, flash bool) {
	q.mu.Lock()
	q.notice = text
	q.savedFlash = flash
	win := q.win
	q.mu.Unlock()

	if win != nil {
		win.Invalidate()
	}
}

func quickNoteTitle(slot int, slotName, content string) string {
	if !isDefaultQuickSlotName(slot, slotName) {
		if clean := sanitizeQuickTitle(slotName); clean != "" {
			return clean
		}
	}
	body := stripQuickFrontmatter(content)
	if strings.TrimSpace(body) == "" {
		body = content
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(strings.TrimSpace(line), "#>-* ")
		if clean := sanitizeQuickTitle(trimmed); clean != "" {
			return clean
		}
	}
	return theme.Tr("速记", "Quick Note")
}

func stripQuickFrontmatter(src string) string {
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return src
	}
	rest := src[4:]
	if idx := strings.Index(rest, "\n---"); idx >= 0 {
		after := rest[idx+4:]
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			return after[nl+1:]
		}
		return ""
	}
	return src
}

func sanitizeQuickTitle(raw string) string {
	var b strings.Builder
	count := 0
	for _, r := range raw {
		switch r {
		case '/', '\\', ':', '\x00', '\n', '\r', '*', '?', '"', '<', '>', '|':
			continue
		}
		b.WriteRune(r)
		count++
		if count >= 40 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	out = strings.TrimSpace(out)
	if !utf8.ValidString(out) {
		return ""
	}
	return out
}

func createUniqueQuickNoteFile(folder, title, content string) (string, error) {
	for n := 0; n < 200; n++ {
		name := title + ".md"
		if n > 0 {
			name = fmt.Sprintf("%s %d.md", title, n+1)
		}
		cand := filepath.Join(folder, name)
		f, err := os.OpenFile(cand, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		_, wErr := f.WriteString(content)
		cErr := f.Close()
		if wErr != nil {
			_ = os.Remove(cand)
			return "", wErr
		}
		if cErr != nil {
			_ = os.Remove(cand)
			return "", cErr
		}
		return cand, nil
	}
	return "", errors.New(theme.Tr("无法分配唯一笔记名称", "Could not allocate a unique note name"))
}

// RenderView renders the standalone Quick Note panel UI.
func (q *QuickNoteController) RenderView(c *ui.Context) {
	pal := theme.Ocean()
	q.mu.Lock()
	activeSlot := q.activeSlot
	text := q.slots[activeSlot]
	notice := q.notice
	savedFlash := q.savedFlash
	q.mu.Unlock()

	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		q.Hide()
		return
	}
	if c.Shortcut(ui.Cmd, ui.Key1) {
		q.switchSlot(0)
	} else if c.Shortcut(ui.Cmd, ui.Key2) {
		q.switchSlot(1)
	} else if c.Shortcut(ui.Cmd, ui.Key3) {
		q.switchSlot(2)
	} else if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
		_, _ = q.SaveToNotebook()
	}

	root := ui.Column(c.Key("quick-note-root")).Fill().Background(pal.Paper)
	internalui.Probe(root, "quick-note-root")
	root.Children(func() {
		// Minimalist header with window drag region, 3-dot pager, save & close buttons.
		leadPad := float32(16)
		if theme.IsMacOS() {
			leadPad = 78
		}
		header := ui.Row(c.Key("quick-header")).FillWidth().Height(44).
			Padding(0, 12, 0, leadPad).AlignItems(ui.Center).Justify(ui.SpaceBetween).
			Background(pal.Paper).BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline)
		header.DragWindow()
		header.Children(func() {
			// 3-dot slot pager.
			ui.Row(c.Key("quick-pager")).Gap(8).AlignItems(ui.Center).Children(func() {
				for i := 0; i < quickSlotCount; i++ {
					slot := i
					active := slot == activeSlot
					dotID := fmt.Sprintf("quick-slot-%d", slot)
					dot := ui.ButtonBase(c.Key(dotID)).Size(22, 22).Center().
						Radius(theme.RadiusPill).Cursor(ui.CursorPointer).
						Label(fmt.Sprintf("Slot %d", slot+1))
					if active {
						dot.Background(pal.Selected)
					} else if dot.Hovered() {
						dot.Background(pal.Hover)
					}
					dot.Children(func() {
						col := pal.BtnBorder
						sz := float32(7)
						if active {
							col = pal.Accent
							sz = 9
						}
						ui.Box(c).Size(sz, sz).Radius(theme.RadiusPill).Background(col)
					})
					internalui.Probe(dot, dotID)
					if dot.Clicked() {
						q.switchSlot(slot)
					}
				}
			})

			// Save-to-notebook + Hide buttons.
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				saveIcon := icons.NoteCheck
				if savedFlash {
					saveIcon = icons.Check
				}
				saveBtn := internalui.IconButton(c, saveIcon, internalui.IconButtonOptions{
					ID:      "quick-save",
					Label:   theme.Tr("存入正式笔记", "Save to Note"),
					Tooltip: theme.Tr("存入正式笔记", "Save to Note"),
					Active:  savedFlash,
				})
				if saveBtn.Clicked() {
					_, _ = q.SaveToNotebook()
				}
				closeBtn := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
					ID:      "quick-close",
					Label:   theme.Tr("隐藏", "Hide"),
					Tooltip: theme.Tr("隐藏速记", "Hide Quick Note"),
				})
				if closeBtn.Clicked() {
					q.Hide()
				}
			})
		})

		// Multi-line Markdown editor area.
		ui.Box(c).Grow(1).FillWidth().Padding(12, 16, 16, 16).Children(func() {
			area := ui.TextArea(c.Key("quick-note-editor"), &text).
				Placeholder(theme.Tr("随手记点什么…", "Jot something down…")).
				Label(theme.Tr("速记内容", "Quick Note Content")).
				Bind(&q.editorHandle).AutoFocus().Fill()
			internalui.Probe(area, "quick-note-editor")
			if area.Changed() {
				q.SetText(text)
			}
		})
	})

	if notice != "" {
		ui.Row(c.Key("quick-notice")).Absolute().Right(16).Bottom(14).Height(32).PaddingX(12).
			AlignItems(ui.Center).Radius(theme.RadiusPill).Background(pal.Menu).
			Border(1, pal.Hairline).Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.12)).
			Children(func() {
				ui.Text(c, notice).FontSize(14).TextColor(pal.TextSecondary).SingleLine()
			})
	}
}
