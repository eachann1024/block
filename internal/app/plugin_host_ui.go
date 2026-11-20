package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/editor"
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	pluginToastDuration = 3 * time.Second
	// pluginToastQueueMax bounds queued toasts so a chatty plugin cannot
	// build an endless backlog while still allowing 4-tone showcase queues.
	pluginToastQueueMax = 6
)

var errHostClosing = errors.New("host is shutting down")

type pluginToast struct {
	ID       uint64
	PluginID string
	Message  string
	Tone     string
	EventKey string
	Icon     *plugins.DuoIconSpec
	native   func(context.Context) error
}

type pluginDialogRef struct {
	PluginID string
	DialogID string
	Token    string
}

type pluginPanelRef struct {
	PluginID string
	PanelID  string
}

type pluginHostUI struct {
	systemReveal func()
	w            *Workspace

	mu              sync.Mutex
	nextToastID     uint64
	nextDialogID    uint64
	toasts          []pluginToast
	deferredToasts  []pluginToast
	toastTimer      *time.Timer
	syncToastRetry  bool
	activeDialog    *pluginDialogRef
	activePanel     *pluginPanelRef
	lastPanel       *pluginPanelRef
	panelWidthVal   float32
	editorHasFocus  bool
	editorFocusNote string
	// insertSource maps a note path to the plugin whose editor write is not
	// saved yet, so that plugin does not receive its own note.saved echo.
	insertSource map[string]string

	closing     chan struct{}
	closingOnce sync.Once
}

var _ plugins.UIHost = (*pluginHostUI)(nil)

func newPluginHostUI(w *Workspace) *pluginHostUI {
	return &pluginHostUI{
		w:             w,
		panelWidthVal: ChatDefaultW,
		insertSource:  map[string]string{},
		closing:       make(chan struct{}),
	}
}

// HostClosing unblocks plugin calls waiting on the UI thread; the supervisor
// calls it before shutting runtimes down.
func (h *pluginHostUI) HostClosing() {
	h.closingOnce.Do(func() { close(h.closing) })
}

// onMainWait runs fn on the UI thread and waits for it, unless the host starts
// closing first. Plugin RPC goroutines use it for calls that return a result.
func (h *pluginHostUI) onMainWait(fn func()) error {
	if !mygo.App.IsReady() {
		h.w.headlessMainMu.Lock()
		defer h.w.headlessMainMu.Unlock()
		fn()
		return nil
	}
	select {
	case <-h.closing:
		return errHostClosing
	default:
	}
	done := make(chan struct{})
	go func() {
		mygo.RunOnMain(fn)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-h.closing:
		return errHostClosing
	}
}

func (h *pluginHostUI) takeInsertSource(absPath string) string {
	key := filepath.Clean(absPath)
	h.mu.Lock()
	defer h.mu.Unlock()
	src := h.insertSource[key]
	delete(h.insertSource, key)
	return src
}

func (h *pluginHostUI) onMain(fn func()) {
	if mygo.App.IsReady() {
		mygo.RunOnMain(fn)
	} else {
		fn()
	}
}

func normalizeToastTone(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "success":
		return "success"
	case "error", "danger":
		return "error"
	case "warning", "warn":
		return "warning"
	default:
		return "info"
	}
}

func (h *pluginHostUI) Toast(pluginID, message, tone string, icon *plugins.DuoIconSpec) {
	h.onMain(func() { h.toast(pluginID, message, tone, "", icon, nil) })
}

func (h *pluginHostUI) toast(pluginID, message, tone, eventKey string, icon *plugins.DuoIconSpec, native func(context.Context) error) bool {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return false
	}
	normTone := normalizeToastTone(tone)
	var icCopy *plugins.DuoIconSpec
	if icon != nil && len(icon.StrokePaths) > 0 {
		cp := *icon
		icCopy = &cp
	}
	h.mu.Lock()
	for _, queue := range [][]pluginToast{h.toasts, h.deferredToasts} {
		for index, toast := range queue {
			if toast.PluginID == pluginID && toast.Message == msg && toast.Tone == normTone && toast.EventKey == eventKey {
				if icCopy != nil {
					queue[index].Icon = icCopy
				}
				h.mu.Unlock()
				h.w.RequestRedraw()
				return true
			}
		}
	}
	if len(h.toasts)+len(h.deferredToasts) >= pluginToastQueueMax {
		drop := 0
		for index := 1; index < len(h.toasts); index++ {
			if h.toasts[index].EventKey == "" {
				drop = index
				break
			}
		}
		if drop == 0 {
			if pluginID == "block-sync" && eventKey != "" {
				h.syncToastRetry = true
			}
			h.mu.Unlock()
			return false
		}
		h.toasts = append(h.toasts[:drop], h.toasts[drop+1:]...)
	}
	h.nextToastID++
	id := h.nextToastID
	h.toasts = append(h.toasts, pluginToast{
		ID:       id,
		PluginID: pluginID,
		Message:  msg,
		Tone:     normTone,
		EventKey: eventKey,
		Icon:     icCopy,
		native:   native,
	})
	if len(h.toasts) == 1 {
		h.scheduleToastTimerLocked(id)
	}
	h.mu.Unlock()
	h.w.RequestRedraw()
	return true
}

func (h *pluginHostUI) scheduleToastTimerLocked(id uint64) {
	if h.toastTimer != nil {
		h.toastTimer.Stop()
	}
	h.toastTimer = time.AfterFunc(pluginToastDuration, func() {
		h.onMain(func() { h.expireToast(id) })
	})
}

func (h *pluginHostUI) expireToast(id uint64) {
	toast, ok := h.currentToast()
	if !ok || toast.ID != id {
		return
	}
	if win := h.w.Window(); toast.native == nil || win != nil && win.IsFocused() {
		h.finishToast(id, false)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := toast.native(ctx)
		h.onMain(func() {
			h.finishToast(id, err != nil && !errors.Is(err, plugins.ErrSystemCanceled))
		})
	}()
}

func (h *pluginHostUI) restoreDeferredToasts() {
	h.mu.Lock()
	if len(h.deferredToasts) == 0 {
		h.mu.Unlock()
		return
	}
	idle := len(h.toasts) == 0
	h.toasts = append(h.toasts, h.deferredToasts...)
	h.deferredToasts = nil
	if idle {
		h.scheduleToastTimerLocked(h.toasts[0].ID)
	}
	h.mu.Unlock()
	h.w.RequestRedraw()
}

func (h *pluginHostUI) finishToast(id uint64, deferred bool) {
	focused := false
	if deferred {
		win := h.w.Window()
		focused = win != nil && win.IsFocused()
	}
	h.mu.Lock()
	if len(h.toasts) == 0 || h.toasts[0].ID != id {
		h.mu.Unlock()
		return
	}
	if deferred {
		if focused {
			h.scheduleToastTimerLocked(id)
			h.mu.Unlock()
			return
		}
		h.deferredToasts = append(h.deferredToasts, h.toasts[0])
	}
	h.toasts[0] = pluginToast{}
	h.toasts = h.toasts[1:]
	if h.toastTimer != nil {
		h.toastTimer.Stop()
		h.toastTimer = nil
	}
	if len(h.toasts) > 0 {
		h.scheduleToastTimerLocked(h.toasts[0].ID)
	}
	retry := !deferred && h.syncToastRetry
	if retry {
		h.syncToastRetry = false
	}
	h.mu.Unlock()
	if retry && h.w.Plugins() != nil {
		h.w.Plugins().RetrySyncNotifications()
	}
	h.w.RequestRedraw()
}

func (h *pluginHostUI) currentToast() (pluginToast, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) == 0 {
		return pluginToast{}, false
	}
	return h.toasts[0], true
}

func (h *pluginHostUI) renderToast(c *ui.Context) {
	toast, ok := h.currentToast()
	if !ok {
		return
	}
	pal := theme.Ocean()
	bg := pal.Menu
	border := pal.MenuBorder
	dotCol := pal.Accent
	textCol := pal.Text
	switch toast.Tone {
	case "success":
		bg = pal.SuccessSurface
		border = pal.Success
		dotCol = pal.Success
		textCol = pal.Success
	case "error":
		bg = pal.DangerSurface
		border = pal.Danger
		dotCol = pal.Danger
		textCol = pal.Danger
	case "warning":
		bg = pal.WarningSurface
		border = pal.Warning
		dotCol = pal.Warning
		textCol = pal.Warning
	}

	layer := ui.Row(c.Key("plugin-toast-layer")).Absolute().
		Bottom(theme.Space16).Left(theme.Space16).Right(theme.Space16).
		Justify(ui.Center).PassThrough()
	layer.Children(func() {
		pill := ui.Row(c.Key("plugin-toast")).MaxWidth(480).
			Padding(theme.Space6, theme.Space8, theme.Space6, theme.Space12).
			Gap(theme.Space8).AlignItems(ui.Center).
			Radius(theme.RadiusMD).Background(bg).Border(1, border).
			Transition(blockui.PopoverSpringTransition())
		blockui.Elevate(pill, theme.ElevationPopover)
		blockui.Probe(pill, "plugin-toast")
		pill.Children(func() {
			if toast.Icon != nil && len(toast.Icon.StrokePaths) > 0 {
				ic := theme.IconColors{
					Line:   textCol,
					Fill:   dotCol.Alpha(0.22),
					Accent: dotCol,
				}
				pluginui.DrawDuoSpec(c, *toast.Icon, theme.IconAction, ic)
			} else {
				ui.Box(c).Size(theme.Space6, theme.Space6).Radius(theme.RadiusPill).
					Background(dotCol).Shrink(0)
			}
			ui.Text(c, toast.Message).
				FontSize(theme.FontSmall).
				FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightControl).
				TextColor(textCol).
				MinWidth(0).
				Shrink(1).
				SingleLine().
				Ellipsis("…")
			cls := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
				ID:       "plugin-toast-close",
				Variant:  blockui.IconPlain,
				Size:     theme.HeightMicro,
				IconSize: theme.IconInline,
				Label:    theme.Tr("关闭提示", "Dismiss"),
				Tooltip:  theme.Tr("关闭", "Dismiss"),
			})
			if cls.Clicked() {
				h.finishToast(toast.ID, false)
			}
		})
	})
}

func (h *pluginHostUI) OpenDialog(pluginID, dialogID string, checkValid func() error) (string, error) {
	type res struct {
		token string
		err   error
	}
	ch := make(chan res, 1)
	if cerr := h.onMainWait(func() {
		if checkValid != nil {
			if verr := checkValid(); verr != nil {
				ch <- res{err: verr}
				return
			}
		}
		sup := h.w.Plugins()
		if sup == nil {
			ch <- res{err: errors.New("plugin host unavailable")}
			return
		}
		if _, ok := sup.Dialog(pluginID, dialogID); !ok {
			ch <- res{err: errors.New("unknown dialog")}
			return
		}
		h.mu.Lock()
		h.nextDialogID++
		tok := fmt.Sprintf("dlg-%d", h.nextDialogID)
		h.activeDialog = &pluginDialogRef{PluginID: pluginID, DialogID: dialogID, Token: tok}
		h.mu.Unlock()
		sup.ResetDialogView(pluginID, dialogID, tok)
		h.w.RequestRedraw()
		ch <- res{token: tok}
	}); cerr != nil {
		return "", cerr
	}
	r := <-ch
	return r.token, r.err
}

func (h *pluginHostUI) ActiveDialogToken(pluginID, dialogID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeDialog != nil && h.activeDialog.PluginID == pluginID && h.activeDialog.DialogID == dialogID {
		return h.activeDialog.Token
	}
	return ""
}

func (h *pluginHostUI) CloseDialog(pluginID, dialogID string) {
	h.onMain(func() {
		h.closeDialogLocal(pluginID, dialogID)
	})
}

func (h *pluginHostUI) closeDialogLocal(pluginID, dialogID string) {
	var closedPlugin, closedDialog string
	h.mu.Lock()
	if h.activeDialog == nil {
		h.mu.Unlock()
		return
	}
	if (pluginID == "" || h.activeDialog.PluginID == pluginID) &&
		(dialogID == "" || h.activeDialog.DialogID == dialogID) {
		closedPlugin = h.activeDialog.PluginID
		closedDialog = h.activeDialog.DialogID
		h.activeDialog = nil
	}
	h.mu.Unlock()
	if closedPlugin != "" && closedDialog != "" {
		if sup := h.w.Plugins(); sup != nil {
			sup.ResetDialogView(closedPlugin, closedDialog, "")
		}
	}
	h.w.RequestRedraw()
}

func (h *pluginHostUI) hasDialog() bool {
	h.mu.Lock()
	ref := h.activeDialog
	h.mu.Unlock()
	if ref == nil {
		return false
	}
	sup := h.w.Plugins()
	if sup == nil {
		return false
	}
	_, ok := sup.Dialog(ref.PluginID, ref.DialogID)
	return ok
}

func (h *pluginHostUI) renderDialog(c *ui.Context) {
	h.mu.Lock()
	ref := h.activeDialog
	h.mu.Unlock()
	if ref == nil {
		return
	}
	sup := h.w.Plugins()
	if sup == nil {
		h.closeDialogLocal("", "")
		return
	}
	dlg, ok := sup.Dialog(ref.PluginID, ref.DialogID)
	if !ok {
		h.closeDialogLocal(ref.PluginID, ref.DialogID)
		return
	}

	pal := theme.Ocean()
	open := true
	id := "plugin-dialog-" + dlg.PluginID + "-" + dlg.ID
	blockui.Dialog(c, id, dlg.Title, &open, blockui.ModalLayout{
		Width:    float32(dlg.Width),
		Centered: true,
	}, func() {
		ui.Column(c.Key(id + "-body")).FillWidth().Padding(theme.Space20).Gap(theme.Space16).
			Transition(blockui.SurfaceStageTransition()).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space12).Children(func() {
				ui.Row(c).MinWidth(0).Shrink(1).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
					if len(dlg.Icon.StrokePaths) > 0 {
						pluginui.DrawDuoSpec(c, dlg.Icon, theme.IconRow, pal.DuoIconColors(pal.Text))
					}
					ui.Text(c, dlg.Title).
						FontSize(theme.FontHeading).
						FixedLineHeight(theme.LineHeading).
						FontWeight(theme.WeightTitle).
						TextColor(pal.Text).
						MinWidth(0).
						Shrink(1).
						SingleLine().
						Ellipsis("…")
				})
				cls := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
					ID:      id + "-close",
					Label:   theme.Tr("关闭", "Close"),
					Tooltip: theme.Tr("关闭", "Close"),
				})
				if cls.Clicked() {
					open = false
				}
			})
			if h.w.pluginsUI != nil {
				h.w.pluginsUI.RenderSurface(c, dlg.PluginID, plugins.DialogSurface(dlg.ID))
			}
		})
	})
	if !open {
		h.closeDialogLocal(dlg.PluginID, dlg.ID)
	}
}

func (h *pluginHostUI) OpenNote(notebookRoot, relPath string, rootInfo os.FileInfo, content []byte, checkValid func() error) error {
	ch := make(chan error, 1)
	if err := h.onMainWait(func() {
		openErr := h.w.openRootValidatedNote(notebookRoot, relPath, rootInfo, content, checkValid)
		h.w.RequestRedraw()
		ch <- openErr
	}); err != nil {
		return err
	}
	return <-ch
}

func (h *pluginHostUI) setEditorFocus(notePath string, focused bool) {
	h.mu.Lock()
	h.editorFocusNote = notePath
	h.editorHasFocus = focused
	h.mu.Unlock()
}

func (h *pluginHostUI) EditorSelection() (plugins.EditorSelection, error) {
	type result struct {
		sel plugins.EditorSelection
		err error
	}
	ch := make(chan result, 1)
	if cerr := h.onMainWait(func() {
		var (
			out plugins.EditorSelection
			err error
		)
		defer func() { ch <- result{out, err} }()
		if h.w.modals != nil && h.w.modals.IsSettingsOpen() {
			err = errors.New("no active note editor")
			return
		}
		if (h.w.exportCtrl != nil && h.w.exportCtrl.IsOpen()) || h.w.IsPageHistoryOpen() {
			err = errors.New("no active note editor")
			return
		}
		activeTab := h.w.ActiveNotePath()
		if activeTab == "" || h.w.SurfaceFor(activeTab) != nil {
			err = errors.New("no active note editor")
			return
		}
		sess := h.w.SessionFor(activeTab)
		if sess == nil || !sess.IsMarkdownNote() {
			err = errors.New("no active note editor")
			return
		}
		noteRel := ""
		if sup := h.w.Plugins(); sup != nil {
			noteRel = sup.ActiveNoteRel()
		}
		h.mu.Lock()
		hasFocus := h.editorHasFocus && h.editorFocusNote == activeTab
		h.mu.Unlock()
		out = plugins.EditorSelection{
			Note:     noteRel,
			Text:     sess.SelectedMarkdownForPlugin(),
			HasFocus: hasFocus,
		}
	}); cerr != nil {
		return plugins.EditorSelection{}, cerr
	}
	r := <-ch
	return r.sel, r.err
}

func (h *pluginHostUI) EditorInsert(pluginID, expectedRoot, expectedNote, mode, text string, checkValid func() error) error {
	type insertOutcome struct {
		err                  error
		activeTab            string
		sess                 *editor.Session
		docRev               uint64
		rollback             editor.HostEditRollback
		saveGen              uint64
		batch                []pendingSessionSave
		inFlight             bool
		stoppedAutoSaveTimer bool
		syncErr              error
		rolledBack           bool
	}
	ch := make(chan insertOutcome, 1)
	if cerr := h.onMainWait(func() {
		var out insertOutcome
		defer func() { ch <- out }()
		if checkValid != nil {
			if verr := checkValid(); verr != nil {
				out.err = verr
				return
			}
		}
		if h.w.modals != nil && h.w.modals.IsSettingsOpen() {
			out.err = errors.New("no active note editor")
			return
		}
		if (h.w.exportCtrl != nil && h.w.exportCtrl.IsOpen()) || h.w.IsPageHistoryOpen() {
			out.err = errors.New("no active note editor")
			return
		}
		activeTab := h.w.ActiveNotePath()
		if activeTab == "" {
			out.err = errors.New("no active note editor")
			return
		}
		if h.w.SurfaceFor(activeTab) != nil {
			out.err = errors.New("active note is read-only")
			return
		}
		sess := h.w.SessionFor(activeTab)
		if sess == nil || !sess.IsMarkdownNote() {
			out.err = errors.New("no active note editor")
			return
		}
		activeRoot := strings.TrimSpace(h.w.State().Folder)
		if expectedRoot != "" {
			expAbs, errA := filepath.Abs(expectedRoot)
			actAbs, errB := filepath.Abs(activeRoot)
			if errA != nil || errB != nil || expAbs != actAbs {
				out.err = errors.New("active notebook changed")
				return
			}
		}
		if expectedNote != "" {
			if expectedRoot != "" {
				expTab := filepath.Clean(filepath.Join(expectedRoot, filepath.FromSlash(expectedNote)))
				if filepath.Clean(activeTab) != expTab {
					out.err = errors.New("active note changed")
					return
				}
			} else if sup := h.w.Plugins(); sup == nil || sup.ActiveNoteRel() != expectedNote {
				out.err = errors.New("active note changed")
				return
			}
		}
		rb := sess.CaptureHostEditRollback()
		replaceSelection := mode != "insert"
		if !sess.InsertMarkdown(text, replaceSelection) {
			out.err = errors.New("editor insert rejected")
			return
		}
		out.activeTab = activeTab
		out.sess = sess
		out.docRev = sess.DocRev()
		out.rollback = rb
		h.mu.Lock()
		h.insertSource[filepath.Clean(activeTab)] = pluginID
		h.mu.Unlock()
		h.w.mu.Lock()
		virtual := h.w.virtualPreview[activeTab]
		syncInline := h.w.win == nil || blockui.IsProbeEnabled()
		if !virtual && !syncInline {
			if h.w.autoSaveTimer != nil {
				h.w.autoSaveTimer.Stop()
				h.w.autoSaveTimer = nil
				out.stoppedAutoSaveTimer = true
			}
			h.w.saveGeneration++
			out.saveGen = h.w.saveGeneration
			h.w.pluginInsertInFlight[activeTab]++
			out.inFlight = true
			out.batch = append(out.batch, pendingSessionSave{
				path: activeTab,
				sess: sess,
				rev:  out.docRev,
				md:   sess.Markdown(),
			})
		}
		h.w.mu.Unlock()
		if !virtual && syncInline {
			if err := h.w.flushSingleSessionNoRecovery(activeTab, out.docRev); err != nil {
				// Disk save conflicted/failed in headless mode: revert atomically
				// without touching redo history, in-progress annotation drafts, or existing recovery files.
				if sess.DocRev() == out.docRev && sess.RollbackHostEdit(out.rollback) {
					h.w.mu.Lock()
					if h.w.autoSaveTimer != nil {
						h.w.autoSaveTimer.Stop()
						h.w.autoSaveTimer = nil
					}
					h.w.saveGeneration++
					h.w.mu.Unlock()
					out.rolledBack = true
				}
				h.mu.Lock()
				delete(h.insertSource, filepath.Clean(activeTab))
				h.mu.Unlock()
				out.syncErr = err
			}
		}
		h.w.RequestRedraw()
	}); cerr != nil {
		return cerr
	}
	res := <-ch
	insertSucceeded := false
	if res.inFlight {
		defer func() {
			var reschedule bool
			h.w.mu.Lock()
			if n := h.w.pluginInsertInFlight[res.activeTab]; n <= 1 {
				delete(h.w.pluginInsertInFlight, res.activeTab)
			} else {
				h.w.pluginInsertInFlight[res.activeTab] = n - 1
			}
			deferred := h.w.autoSaveDeferred[res.activeTab]
			delete(h.w.autoSaveDeferred, res.activeTab)
			if s := h.w.sessions[res.activeTab]; s != nil && s.Dirty() && !h.w.virtualPreview[res.activeTab] {
				if deferred || res.stoppedAutoSaveTimer || insertSucceeded {
					reschedule = true
				}
			} else if res.stoppedAutoSaveTimer {
				reschedule = true
			}
			h.w.mu.Unlock()
			if reschedule {
				_ = h.onMainWait(func() {
					h.w.flushOrScheduleDirtySessions()
				})
			}
		}()
	}
	if res.err != nil {
		return res.err
	}
	if res.syncErr != nil {
		if res.rolledBack {
			return fmt.Errorf("%w: disk save failed and editor insert was reverted: %v", plugins.ErrNoteConflict, res.syncErr)
		}
		return fmt.Errorf("%w: inserted into editor draft but disk save failed: %v", plugins.ErrNoteConflict, res.syncErr)
	}
	if len(res.batch) > 0 {
		errs, completed := h.w.runSessionSaves(res.saveGen, res.batch, true)
		saveErr := errs[res.activeTab]
		if saveErr == nil && !completed[res.activeTab] {
			saveErr = errors.New("disk save did not complete")
		}
		if saveErr != nil {
			rolledBack := false
			_ = h.onMainWait(func() {
				h.w.mu.RLock()
				sess := h.w.sessions[res.activeTab]
				h.w.mu.RUnlock()
				if sess != nil && sess == res.sess && sess.DocRev() == res.docRev && sess.RollbackHostEdit(res.rollback) {
					h.w.mu.Lock()
					if h.w.autoSaveTimer != nil {
						h.w.autoSaveTimer.Stop()
						h.w.autoSaveTimer = nil
					}
					h.w.saveGeneration++
					h.w.mu.Unlock()
					rolledBack = true
				}
				h.mu.Lock()
				delete(h.insertSource, filepath.Clean(res.activeTab))
				h.mu.Unlock()
				h.w.RequestRedraw()
			})
			if rolledBack {
				return fmt.Errorf("%w: disk save failed and editor insert was reverted: %v", plugins.ErrNoteConflict, saveErr)
			}
			return fmt.Errorf("%w: inserted into editor draft but disk save failed: %v", plugins.ErrNoteConflict, saveErr)
		}
		insertSucceeded = true
	}
	return nil
}

func (h *pluginHostUI) TarotState() (plugins.TarotState, error) {
	ch := make(chan plugins.TarotState, 1)
	if err := h.onMainWait(func() {
		ch <- h.w.TarotStateSnapshot()
	}); err != nil {
		return plugins.TarotState{}, err
	}
	return <-ch, nil
}

func (h *pluginHostUI) SetTarotMode(enabled *bool, toggle bool, room string) (plugins.TarotState, error) {
	ch := make(chan plugins.TarotState, 1)
	if err := h.onMainWait(func() {
		ch <- h.w.ApplyTarotPluginControl(enabled, toggle, room)
	}); err != nil {
		return plugins.TarotState{}, err
	}
	return <-ch, nil
}

// isHostReservedShortcut reports chords the editor or app handles after plugin
// shortcuts run (find, formatting, clipboard, zoom, tab navigation). Plugins
// cannot bind them, so a plugin never shadows a built-in action.
func isHostReservedShortcut(mods ui.Modifiers, key ui.Key) bool {
	switch mods {
	case ui.Cmd:
		switch key {
		case ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyI,
			ui.KeyS, ui.KeyU, ui.KeyV, ui.KeyX, ui.KeyY, ui.KeyZ,
			ui.KeyEqual, ui.KeyMinus, ui.Key0, ui.KeyBackslash,
			ui.KeyBracketLeft, ui.KeyBracketRight:
			return true
		}
		return key >= ui.Key1 && key <= ui.Key9
	case ui.Cmd | ui.Shift:
		switch key {
		case ui.KeyC, ui.KeyS, ui.KeyV, ui.KeyX, ui.KeyZ, ui.KeyEqual, ui.KeyMinus,
			ui.KeyBracketLeft, ui.KeyBracketRight:
			return true
		}
	case ui.Cmd | ui.Shift | ui.Alt:
		return key == ui.KeyV
	case ui.Cmd | ui.Alt:
		return key == ui.KeyC || key == ui.KeyLeft || key == ui.KeyRight
	}
	return false
}
