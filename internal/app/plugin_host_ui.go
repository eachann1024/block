package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/editor"
	"block/internal/icons"
	"block/internal/modals"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	pluginToastInfoDuration    = 4 * time.Second
	pluginToastWarningDuration = 6 * time.Second
	pluginToastMinResume       = 250 * time.Millisecond
	pluginToastVisibleMax      = 3
	// pluginToastQueueMax bounds queued toasts so a chatty plugin cannot
	// build an endless backlog while still allowing 4-tone showcase queues.
	pluginToastQueueMax = 6
	// pluginToastPerSourceMax bounds one plugin's share of the queue so it
	// can never push out app notices (save failures etc.) or other plugins.
	pluginToastPerSourceMax = 4
)

var errHostClosing = errors.New("host is shutting down")

type pluginToast struct {
	ID        uint64
	PluginID  string
	Message   string
	Body      string
	Tone      string
	Icon      *plugins.DuoIconSpec
	Action    *plugins.ToastAction
	OnAction  func()
	remaining time.Duration
	startedAt time.Time
	hovered   bool
	timer     *time.Timer
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

	menubar *pluginMenubar // system tray icons of plugins
}

var _ plugins.UIHost = (*pluginHostUI)(nil)

func newPluginHostUI(w *Workspace) *pluginHostUI {
	h := &pluginHostUI{
		w:             w,
		panelWidthVal: ChatDefaultW,
		insertSource:  map[string]string{},
		closing:       make(chan struct{}),
	}
	h.startMenubar()
	return h
}

// HostClosing unblocks plugin calls waiting on the UI thread and stops any
// active toast timers; the supervisor calls it before shutting runtimes down.
func (h *pluginHostUI) HostClosing() {
	h.closingOnce.Do(func() { close(h.closing) })
	h.mu.Lock()
	for i := range h.toasts {
		if h.toasts[i].timer != nil {
			h.toasts[i].timer.Stop()
			h.toasts[i].timer = nil
		}
	}
	h.mu.Unlock()
	if h.w != nil && h.w.pluginsUI != nil {
		h.w.pluginsUI.ClearPermissionPrompts()
	}
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

func toastDurationFor(tone string, hasAction bool) time.Duration {
	if hasAction || tone == "error" {
		return 0
	}
	if tone == "warning" {
		return pluginToastWarningDuration
	}
	return pluginToastInfoDuration
}

func cloneToastAction(a *plugins.ToastAction) *plugins.ToastAction {
	if a == nil {
		return nil
	}
	id := strings.TrimSpace(a.ID)
	label := strings.TrimSpace(a.Label)
	if id == "" || label == "" {
		return nil
	}
	cp := &plugins.ToastAction{ID: id, Label: label}
	if len(a.Payload) > 0 {
		cp.Payload = make(map[string]any, len(a.Payload))
		for k, v := range a.Payload {
			cp.Payload[k] = v
		}
	}
	return cp
}

func toastActionID(a *plugins.ToastAction) string {
	if a == nil {
		return ""
	}
	return a.ID
}

func (h *pluginHostUI) Toast(req plugins.ToastRequest) bool {
	return h.toastReq(req)
}

func (h *pluginHostUI) OpenPluginSettings(pluginID string) {
	id := strings.TrimSpace(pluginID)
	if id == "" {
		return
	}
	h.onMain(func() {
		if h.systemReveal != nil {
			h.systemReveal()
		}
		h.w.OpenSettings(modals.TabPluginPrefix + id)
	})
}

func (h *pluginHostUI) toastReq(req plugins.ToastRequest) bool {
	select {
	case <-h.closing:
		return false
	default:
	}
	msg := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(req.Message, "\r\n", "\n"), "\r", "\n"))
	if msg == "" {
		return false
	}
	body := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(req.Body, "\r\n", "\n"), "\r", "\n"))
	normTone := normalizeToastTone(req.Tone)
	var icCopy *plugins.DuoIconSpec
	if req.Icon != nil && len(req.Icon.StrokePaths) > 0 {
		cp := *req.Icon
		icCopy = &cp
	}
	actCopy := cloneToastAction(req.Action)
	hasAction := actCopy != nil || req.OnAction != nil
	dur := toastDurationFor(normTone, hasAction)
	actID := toastActionID(actCopy)

	h.mu.Lock()
	for i := range h.toasts {
		t := &h.toasts[i]
		if t.PluginID == req.PluginID && t.Message == msg && t.Body == body && t.Tone == normTone && toastActionID(t.Action) == actID {
			if icCopy != nil {
				t.Icon = icCopy
			}
			t.Action = actCopy
			t.OnAction = req.OnAction
			t.remaining = dur
			if i < pluginToastVisibleMax {
				if dur == 0 {
					if t.timer != nil {
						t.timer.Stop()
						t.timer = nil
					}
				} else if !t.hovered {
					h.startToastTimerLocked(i)
				}
			}
			h.mu.Unlock()
			h.w.RequestRedraw()
			return true
		}
	}
	if req.PluginID != "" && h.countToastsLocked(req.PluginID) >= pluginToastPerSourceMax {
		if !h.evictQueuedToastLocked(func(t pluginToast) bool { return t.PluginID == req.PluginID }) {
			h.mu.Unlock()
			return false
		}
	}
	if len(h.toasts) >= pluginToastQueueMax {
		evicted := false
		if req.PluginID != "" {
			// A plugin may only displace its own queued backlog.
			evicted = h.evictQueuedToastLocked(func(t pluginToast) bool { return t.PluginID == req.PluginID })
		} else {
			evicted = h.evictQueuedToastLocked(func(t pluginToast) bool { return t.PluginID != "" }) ||
				h.evictQueuedToastLocked(func(t pluginToast) bool { return t.Tone != "error" }) ||
				h.evictQueuedToastLocked(func(pluginToast) bool { return true })
		}
		if !evicted {
			h.mu.Unlock()
			return false
		}
	}
	h.nextToastID++
	id := h.nextToastID
	h.toasts = append(h.toasts, pluginToast{
		ID:        id,
		PluginID:  req.PluginID,
		Message:   msg,
		Body:      body,
		Tone:      normTone,
		Icon:      icCopy,
		Action:    actCopy,
		OnAction:  req.OnAction,
		remaining: dur,
	})
	idx := len(h.toasts) - 1
	if idx < pluginToastVisibleMax && dur > 0 {
		h.startToastTimerLocked(idx)
	}
	h.mu.Unlock()
	h.w.RequestRedraw()
	return true
}

func (h *pluginHostUI) countToastsLocked(pluginID string) int {
	n := 0
	for _, t := range h.toasts {
		if t.PluginID == pluginID {
			n++
		}
	}
	return n
}

// evictQueuedToastLocked drops the oldest queued (not yet visible) toast
// matching keep; visible banners in [0, pluginToastVisibleMax) are never evicted.
func (h *pluginHostUI) evictQueuedToastLocked(match func(pluginToast) bool) bool {
	for i := pluginToastVisibleMax; i < len(h.toasts); i++ {
		if match(h.toasts[i]) {
			if h.toasts[i].timer != nil {
				h.toasts[i].timer.Stop()
				h.toasts[i].timer = nil
			}
			h.toasts = append(h.toasts[:i], h.toasts[i+1:]...)
			return true
		}
	}
	return false
}

func (h *pluginHostUI) startToastTimerLocked(idx int) {
	if idx < 0 || idx >= len(h.toasts) {
		return
	}
	t := &h.toasts[idx]
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.remaining <= 0 || t.hovered {
		return
	}
	id := t.ID
	t.startedAt = time.Now()
	var tm *time.Timer
	tm = time.AfterFunc(t.remaining, func() {
		h.expireToast(id, tm)
	})
	t.timer = tm
}

func (h *pluginHostUI) expireToast(id uint64, tm *time.Timer) {
	select {
	case <-h.closing:
		return
	default:
	}
	h.mu.Lock()
	idx := -1
	for i := range h.toasts {
		if h.toasts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 || h.toasts[idx].timer != tm || h.toasts[idx].hovered {
		h.mu.Unlock()
		return
	}
	h.removeToastAtLocked(idx)
	h.mu.Unlock()
	h.w.RequestRedraw()
}

func (h *pluginHostUI) setToastHovered(id uint64, hovered bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	idx := -1
	for i := range h.toasts {
		if h.toasts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	t := &h.toasts[idx]
	if t.hovered == hovered {
		return
	}
	t.hovered = hovered
	if hovered {
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
			if !t.startedAt.IsZero() && t.remaining > 0 {
				elapsed := time.Since(t.startedAt)
				t.remaining = max(t.remaining-elapsed, pluginToastMinResume)
			}
		}
		return
	}
	if idx < pluginToastVisibleMax && t.remaining > 0 {
		h.startToastTimerLocked(idx)
	}
}

func (h *pluginHostUI) finishToast(id uint64) {
	h.mu.Lock()
	idx := -1
	for i := range h.toasts {
		if h.toasts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return
	}
	h.removeToastAtLocked(idx)
	h.mu.Unlock()
	h.w.RequestRedraw()
}

func (h *pluginHostUI) removeToastAtLocked(idx int) {
	if idx < 0 || idx >= len(h.toasts) {
		return
	}
	if h.toasts[idx].timer != nil {
		h.toasts[idx].timer.Stop()
		h.toasts[idx].timer = nil
	}
	h.toasts[idx] = pluginToast{}
	h.toasts = append(h.toasts[:idx], h.toasts[idx+1:]...)
	limit := min(len(h.toasts), pluginToastVisibleMax)
	for i := 0; i < limit; i++ {
		if h.toasts[i].timer == nil && !h.toasts[i].hovered && h.toasts[i].remaining > 0 {
			h.startToastTimerLocked(i)
		}
	}
}

func (h *pluginHostUI) visibleToasts() []pluginToast {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := min(len(h.toasts), pluginToastVisibleMax)
	if n == 0 {
		return nil
	}
	out := make([]pluginToast, n)
	for i := 0; i < n; i++ {
		cp := h.toasts[i]
		cp.timer = nil
		out[i] = cp
	}
	return out
}

func toastTextUnits(s string) int {
	units := 0
	for _, r := range s {
		if r > 0x7f {
			units += 2
		} else {
			units++
		}
	}
	return units
}

func toastMessageWraps(msg string) bool {
	return strings.Contains(msg, "\n") || toastTextUnits(msg) > 34
}

func toastSingleLineText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func toastNeedsFullTooltip(toast pluginToast, hasAction bool) bool {
	if toast.Body != "" {
		if strings.Contains(toast.Message, "\n") || toastTextUnits(toast.Message) > 34 {
			return true
		}
		if strings.Count(toast.Body, "\n") >= 4 || toastTextUnits(toast.Body) > 120 {
			return true
		}
		return false
	}
	if hasAction {
		return strings.Contains(toast.Message, "\n") || toastTextUnits(toast.Message) > 34
	}
	return strings.Count(toast.Message, "\n") >= 2 || toastTextUnits(toast.Message) > 68
}

func (h *pluginHostUI) renderToast(c *ui.Context) {
	visible := h.visibleToasts()
	if len(visible) == 0 {
		return
	}
	pal := theme.Ocean()
	vpW, _ := c.Size()
	bannerW := theme.ToastBannerWidth
	if vpW > 0 && vpW < bannerW+2*theme.Space16 {
		bannerW = max(vpW-2*theme.Space16, 1)
	}

	ui.Overlay(c, func() {
		stack := ui.Column(c.Key("global-toast-layer")).Absolute().
			Top(theme.TitleBarHeight + theme.Space12).
			Right(theme.Space16).
			Width(bannerW).
			Gap(theme.Space8).
			AlignItems(ui.Stretch).
			PassThrough()
		stack.Children(func() {
			for i, toast := range visible {
				t := toast
				toneCol := pal.Accent
				borderCol := pal.MenuBorder
				switch t.Tone {
				case "success":
					toneCol = pal.Success
				case "error":
					toneCol = pal.Danger
					borderCol = pal.Danger.Alpha(0.35)
				case "warning":
					toneCol = pal.Warning
				}

				hasBody := t.Body != ""
				hasAction := t.Action != nil || t.OnAction != nil
				isCompact := !hasBody && !hasAction && !toastMessageWraps(t.Message)

				card := ui.Row(c.Key(fmt.Sprintf("global-toast-%d", t.ID))).
					FillWidth().
					MinHeight(theme.ToastBannerMinHeight).
					Padding(theme.Space10, theme.Space10, theme.Space10, theme.Space12).
					Gap(theme.Space10).
					Radius(theme.RadiusLG).
					Background(pal.Menu).
					Border(1, borderCol).
					Transition(blockui.ToastBannerTransition())
				if isCompact {
					card.AlignItems(ui.Center)
				} else {
					card.AlignItems(ui.Start)
				}
				blockui.Elevate(card, theme.ElevationPopover)
				if i == 0 {
					blockui.Probe(card, "global-toast")
				} else {
					blockui.Probe(card, fmt.Sprintf("global-toast-%d", t.ID))
				}
				if t.remaining > 0 {
					h.setToastHovered(t.ID, card.Hovered())
				}

				card.Children(func() {
					iconWrap := ui.Row(c).Size(theme.ToastBannerIconBox, theme.ToastBannerIconBox).
						AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
					if !isCompact {
						iconWrap.Margin(1, 0, 0, 0)
					}
					iconWrap.Children(func() {
						if t.Icon != nil && len(t.Icon.StrokePaths) > 0 {
							ic := theme.IconColors{
								Line:   pal.Text,
								Fill:   toneCol.Alpha(0.20),
								Accent: toneCol,
							}
							pluginui.DrawDuoSpec(c, *t.Icon, theme.ToastBannerIconBox, ic)
						} else {
							ic := theme.IconColors{
								Line:   toneCol,
								Fill:   toneCol.Alpha(0.16),
								Accent: toneCol,
							}
							pluginui.DrawDuoSpec(c, plugins.ToastToneIconSpec(t.Tone), theme.ToastBannerIconBox, ic)
						}
					})

					col := ui.Column(c.Key(fmt.Sprintf("global-toast-col-%d", t.ID))).
						Grow(1).MinWidth(0).Gap(theme.Space4)
					col.Children(func() {
						textGroup := ui.Column(c.Key(fmt.Sprintf("global-toast-text-%d", t.ID))).
							FillWidth().MinWidth(0).Gap(theme.Space4)
						textGroup.Children(func() {
							if isCompact {
								ui.Text(c, t.Message).
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									FontWeight(theme.WeightControl).
									TextColor(pal.Text).
									MinWidth(0).
									SingleLine().
									Ellipsis("…")
							} else if !hasBody && !hasAction {
								ui.Text(c, t.Message).
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									FontWeight(theme.WeightControl).
									TextColor(pal.Text).
									MinWidth(0).
									MaxLines(2)
							} else {
								ui.Text(c, toastSingleLineText(t.Message)).
									FontSize(theme.FontSmall).
									FixedLineHeight(theme.LineSmall).
									FontWeight(theme.WeightControl).
									TextColor(pal.Text).
									MinWidth(0).
									SingleLine().
									Ellipsis("…")
								if hasBody {
									ui.Text(c, t.Body).
										FontSize(theme.FontSmall).
										FixedLineHeight(theme.LineSmall).
										FontWeight(theme.WeightRegular).
										TextColor(pal.TextSecondary).
										MinWidth(0).
										MaxLines(4)
								}
							}
						})
						if toastNeedsFullTooltip(t, hasAction) {
							fullText := t.Message
							if t.Body != "" {
								fullText = t.Message + "\n" + t.Body
							}
							blockui.WrappedTooltip(c, textGroup, fullText)
						}
						if hasAction {
							actLabel := theme.Tr("查看", "View")
							if t.Action != nil && strings.TrimSpace(t.Action.Label) != "" {
								actLabel = t.Action.Label
							}
							ui.Row(c).Margin(theme.Space4, 0, 0, 0).AlignItems(ui.Center).Children(func() {
								btn := blockui.ActionButtonWithOptions(c, blockui.ActionButtonOptions{
									ID:          fmt.Sprintf("global-toast-action-%d", t.ID),
									Label:       actLabel,
									Style:       blockui.ButtonSecondary,
									CompactText: true,
									Height:      theme.HeightMicro,
									PaddingX:    theme.Space8,
								})
								if btn.Clicked() {
									h.finishToast(t.ID)
									if t.OnAction != nil {
										t.OnAction()
									} else if t.PluginID != "" && t.Action != nil {
										if sup := h.w.Plugins(); sup != nil {
											sup.TriggerToastActionAsync(t.PluginID, t.Action.ID, t.Action.Payload)
										}
									}
								}
							})
						}
					})

					cls := blockui.IconButton(c, icons.Close, blockui.IconButtonOptions{
						ID:       fmt.Sprintf("global-toast-close-%d", t.ID),
						Variant:  blockui.IconPlain,
						Size:     theme.HeightMicro,
						IconSize: theme.IconInline,
						Label:    theme.Tr("关闭提示", "Dismiss"),
						Tooltip:  theme.Tr("关闭", "Dismiss"),
					})
					if i == 0 {
						blockui.Probe(cls, "global-toast-close")
					}
					if cls.Clicked() {
						h.finishToast(t.ID)
					}
				})
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
						FontSize(theme.FontTitle).
						FixedLineHeight(theme.LineTitle).
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

// isHostReservedShortcut delegates to internal/ui.IsHostReservedShortcut so
// Settings shortcut recording and runtime dispatch share one source of truth.
func isHostReservedShortcut(mods ui.Modifiers, key ui.Key) bool {
	return blockui.IsHostReservedShortcut(mods, key)
}
