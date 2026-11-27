package modals

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"block/internal/feedback"
	"block/internal/files"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	internalui "block/internal/ui"
	"block/internal/updater"
)

// The Feedback dialog has four faces, picked each frame from its state:
// the thank-you card, the upload progress (also the failed / retry state),
// the log preview, and the form.

const (
	feedbackWidth     float32 = 480
	feedbackDoneWidth float32 = 400
	feedbackLogLines          = 200
	feedbackRowHeight float32 = 44
	feedbackTileSize  float32 = 32
)

type feedbackView uint8

const (
	feedbackViewForm feedbackView = iota
	feedbackViewLog
)

// feedbackUI is the dialog state beyond the text draft: attachments, the
// diagnostic log choice and the upload in flight. Attachments live for the
// session only; the text draft is what persists.
type feedbackUI struct {
	view        feedbackView
	attachments []feedback.Attachment
	pending     []string // paths being probed / compressed
	notice      string   // why some picked files were not added
	includeLog  bool
	log         files.DiagnosticLog
	logScroll   ui.ScrollState
	copiedAt    time.Time

	sender        *feedback.Sender
	sendContact   string
	lastRequestID string // reused when resending identical content after cancel
	lastSubmitKey string

	// Isolated state when opened from the updater diagnostics feedback flow so
	// ordinary user feedback drafts and personal attachments are never clobbered
	// or accidentally attached.
	updaterMode         bool
	updaterDraftMsg     string
	stashedAttachments  []feedback.Attachment
	stashedIncludeLog   bool
	stashedIncludeSaved bool

	mu    sync.Mutex
	inbox []func() // worker results, applied on the UI thread by pumpFeedback
}

// postFeedback hands a worker's result to the UI thread.
func (m *Manager) postFeedback(fn func()) {
	m.fb.mu.Lock()
	m.fb.inbox = append(m.fb.inbox, fn)
	m.fb.mu.Unlock()
	m.requestRedraw()
}

// pumpFeedback applies worker results and settles an upload that finished,
// whether or not the dialog is showing. It runs every frame.
func (m *Manager) pumpFeedback() {
	m.fb.mu.Lock()
	inbox := m.fb.inbox
	m.fb.inbox = nil
	m.fb.mu.Unlock()
	for _, fn := range inbox {
		fn()
	}
	s := m.fb.sender
	if s == nil {
		return
	}
	switch s.Snapshot().Phase {
	case feedback.PhaseDone:
		m.finishFeedbackSent(m.fb.sendContact)
		if !m.feedbackOpen {
			// Sent in the background: confirm with a toast, not a dialog.
			m.feedbackDone = false
			if m.FeedbackNoticeFn != nil {
				m.FeedbackNoticeFn(theme.Tr("反馈已发送，谢谢你", "Feedback sent, thank you"), "success")
			}
		}
	case feedback.PhaseFailed:
		if !m.feedbackOpen {
			// A background upload that stopped needs the user: bring it back.
			m.feedbackOpen = true
		}
	case feedback.PhaseCanceled:
		m.fb.sender = nil
	}
}

// OpenFeedback opens the Feedback modal, restoring any saved FeedbackDraft.
// An upload still running in the background is shown again instead.
func (m *Manager) OpenFeedback() {
	if m.fb.sender == nil {
		if m.fb.updaterMode {
			m.leaveUpdaterFeedbackModeLocked()
		}
		st := m.currentState()
		if st.FeedbackDraft != nil {
			m.feedbackCategory = normalizeFeedbackCat(st.FeedbackDraft.Category)
			m.feedbackMessage = st.FeedbackDraft.Message
			m.feedbackContact = st.FeedbackDraft.Contact
		} else {
			m.feedbackCategory = "bug"
			m.feedbackMessage = ""
			m.feedbackContact = ""
		}
	}
	m.feedbackDone = false
	m.feedbackErr = ""
	m.feedbackSentContact = ""
	m.fb.view = feedbackViewForm
	m.fb.notice = ""
	m.feedbackOpen = true
	go func() {
		log := files.ReadDiagnosticLog(feedbackLogLines)
		m.postFeedback(func() { m.fb.log = log })
	}()
	m.requestRedraw()
}

func (m *Manager) leaveUpdaterFeedbackModeLocked() {
	for _, a := range m.fb.attachments {
		a.Discard()
	}
	m.fb.attachments = m.fb.stashedAttachments
	m.fb.stashedAttachments = nil
	if m.fb.stashedIncludeSaved {
		m.fb.includeLog = m.fb.stashedIncludeLog
		m.fb.stashedIncludeSaved = false
	}
	m.fb.updaterMode = false
}

// OpenFeedbackWithCategory opens the Feedback modal pre-selecting category
// ("bug", "idea", or "other") when no non-empty draft is currently in progress.
func (m *Manager) OpenFeedbackWithCategory(category string) {
	m.OpenFeedback()
	if m.fb.sender == nil && strings.TrimSpace(m.feedbackMessage) == "" {
		m.feedbackCategory = normalizeFeedbackCat(category)
	}
}

const updaterDiagMarker = "--- Updater Diagnostics ---"

func mergeUpdaterFeedbackMessage(existingDraft, freshDiagnostics string) string {
	existing := strings.TrimSpace(existingDraft)
	if existing == "" {
		return freshDiagnostics
	}
	userPrefix := ""
	if idx := strings.Index(existing, updaterDiagMarker); idx >= 0 {
		rawPrefix := strings.TrimSpace(existing[:idx])
		if rawPrefix != "" && rawPrefix != "（可在上方补充问题描述）" && rawPrefix != "(Optional: add details above)" {
			userPrefix = rawPrefix
		}
	} else {
		userPrefix = existing
	}
	if userPrefix == "" {
		return freshDiagnostics
	}
	diagBody := freshDiagnostics
	if idx := strings.Index(freshDiagnostics, updaterDiagMarker); idx >= 0 {
		diagBody = freshDiagnostics[idx:]
	}
	const maxTotalRunes = 3900
	const maxPrefixRunes = 1000
	if utf8.RuneCountInString(userPrefix) > maxPrefixRunes {
		userPrefix = string([]rune(userPrefix)[:maxPrefixRunes])
	}
	combined := userPrefix + "\n\n" + diagBody
	if utf8.RuneCountInString(combined) <= maxTotalRunes {
		return combined
	}
	budget := maxTotalRunes - utf8.RuneCountInString(userPrefix) - 5
	if budget < 200 {
		return userPrefix + "\n\n" + updaterDiagMarker
	}
	runes := []rune(diagBody)
	if len(runes) > budget {
		diagBody = string(runes[:budget]) + "..."
	}
	return userPrefix + "\n\n" + diagBody
}

// OpenFeedbackForUpdater opens the Feedback modal in an isolated updater-diagnostics
// session: selects "bug", stashes unrelated drafts/attachments without overwriting
// AppState.FeedbackDraft, defaults includeLog off (the scrubbed updater report and
// updater.log tail are already embedded), and preserves user text above the marker.
func (m *Manager) OpenFeedbackForUpdater(diagnostics string) {
	if m.fb.sender == nil {
		if !m.fb.updaterMode {
			m.fb.stashedAttachments = m.fb.attachments
			m.fb.attachments = nil
			m.fb.stashedIncludeLog = m.fb.includeLog
			m.fb.stashedIncludeSaved = true
			m.fb.updaterMode = true
		}
		st := m.currentState()
		if st.FeedbackDraft != nil && strings.TrimSpace(m.feedbackContact) == "" {
			m.feedbackContact = st.FeedbackDraft.Contact
		}
		m.feedbackCategory = "bug"
		m.fb.includeLog = false
		m.feedbackMessage = mergeUpdaterFeedbackMessage(m.fb.updaterDraftMsg, diagnostics)
		m.fb.updaterDraftMsg = m.feedbackMessage
	}
	m.feedbackDone = false
	m.feedbackErr = ""
	m.feedbackSentContact = ""
	m.fb.view = feedbackViewForm
	m.fb.notice = ""
	m.feedbackOpen = true
	go func() {
		log := files.ReadDiagnosticLog(feedbackLogLines)
		m.postFeedback(func() { m.fb.log = log })
	}()
	m.requestRedraw()
}

// CloseFeedback closes the Feedback modal, persisting any non-done draft.
// An upload in flight keeps running in the background.
func (m *Manager) CloseFeedback() {
	if !m.feedbackOpen {
		return
	}
	if !m.feedbackDone {
		m.persistFeedbackDraft()
	}
	if m.fb.updaterMode && m.fb.sender == nil {
		m.leaveUpdaterFeedbackModeLocked()
	}
	m.feedbackOpen = false
	m.requestRedraw()
}

// IsFeedbackOpen reports whether the Feedback modal is open.
func (m *Manager) IsFeedbackOpen() bool {
	return m.feedbackOpen
}

func normalizeFeedbackCat(raw string) string {
	switch raw {
	case "idea", "other":
		return raw
	default:
		return "bug"
	}
}

func (m *Manager) persistFeedbackDraft() {
	if m.fb.updaterMode {
		m.fb.updaterDraftMsg = m.feedbackMessage
		return
	}
	msg := m.feedbackMessage
	contact := m.feedbackContact
	cat := normalizeFeedbackCat(m.feedbackCategory)
	nonEmpty := strings.TrimSpace(msg) != "" || strings.TrimSpace(contact) != "" || cat != "bug"
	m.mutateState(func(s *model.AppState) {
		if !nonEmpty {
			s.FeedbackDraft = nil
			return
		}
		s.FeedbackDraft = &model.FeedbackDraft{
			Category: cat,
			Message:  msg,
			Contact:  contact,
		}
	})
}

func validFeedbackEmail(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	domain := s[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// --- Attachments ---

func (m *Manager) feedbackOverLimit() int {
	n := 0
	for _, a := range m.fb.attachments {
		if a.OverLimit() {
			n++
		}
	}
	return n
}

// feedbackBlocked reports whether the attachments keep the report from
// being sent: one is too large, they are too large together, or one is
// still being prepared.
func (m *Manager) feedbackBlocked() bool {
	return m.feedbackOverLimit() > 0 || len(m.fb.pending) > 0 ||
		feedback.TotalBytes(m.fb.attachments) > feedback.MaxTotalBytes
}

func (m *Manager) feedbackHasPath(path string) bool {
	for _, a := range m.fb.attachments {
		if a.Path == path {
			return true
		}
	}
	for _, p := range m.fb.pending {
		if p == path {
			return true
		}
	}
	return false
}

// addFeedbackFiles probes picked or dropped files off the UI thread (images
// are compressed there) and adds them as they come back.
func (m *Manager) addFeedbackFiles(paths []string) {
	fb := &m.fb
	fb.notice = ""
	m.feedbackErr = ""
	full, dup := 0, 0
	for _, rawPath := range paths {
		path := filepath.Clean(rawPath)
		switch {
		case m.feedbackHasPath(path):
			dup++
			continue
		case len(fb.attachments)+len(fb.pending) >= feedback.MaxAttachments:
			full++
			continue
		}
		fb.pending = append(fb.pending, path)
		go func() {
			att, err := feedback.Probe(path)
			m.postFeedback(func() {
				for i, p := range fb.pending {
					if p == path {
						fb.pending = append(fb.pending[:i], fb.pending[i+1:]...)
						break
					}
				}
				if err != nil {
					files.LogWarnf("feedback attachment rejected: %v", err)
					fb.notice = theme.Tr("有文件读不了，没有加入。", "A file could not be read and was not added.")
					return
				}
				fb.attachments = append(fb.attachments, att)
			})
		}()
	}
	switch {
	case full > 0:
		fb.notice = fmt.Sprintf(theme.Tr("最多 %d 个附件，多出的 %d 个没有加入。", "Up to %d attachments; %d more were not added."),
			feedback.MaxAttachments, full)
	case dup > 0:
		fb.notice = theme.Tr("这个文件已经在附件里了。", "That file is already attached.")
	}
}

func (m *Manager) pickFeedbackFiles(media bool) {
	if !mygo.App.IsReady() {
		return
	}
	opts := mygo.OpenDialogOptions{Title: theme.Tr("添加附件", "Add Attachments"), Multiple: true}
	if media {
		opts.Filters = []mygo.FileFilter{{
			Name:       theme.Tr("图片或视频", "Images or Videos"),
			Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "tiff", "mp4", "mov", "m4v", "webm", "mkv", "avi"},
		}}
	}
	go func() {
		paths, err := files.OpenDialog(nil, opts)
		if err != nil || len(paths) == 0 {
			return
		}
		m.postFeedback(func() { m.addFeedbackFiles(paths) })
	}()
}

func (m *Manager) removeFeedbackAttachment(id string) {
	fb := &m.fb
	for i, a := range fb.attachments {
		if a.ID == id {
			a.Discard()
			fb.attachments = append(fb.attachments[:i], fb.attachments[i+1:]...)
			break
		}
	}
	fb.notice = ""
}

func (m *Manager) removeFeedbackOverLimit() {
	fb := &m.fb
	kept := fb.attachments[:0]
	for _, a := range fb.attachments {
		if a.OverLimit() {
			a.Discard()
			continue
		}
		kept = append(kept, a)
	}
	fb.attachments = kept
	fb.notice = ""
}

func (m *Manager) feedbackLogAttached() bool {
	return m.fb.includeLog && len(m.fb.log.Lines) > 0
}

// --- Sending ---

func (m *Manager) submitFeedback() {
	msg := strings.TrimSpace(m.feedbackMessage)
	if msg == "" {
		m.feedbackErr = theme.Tr("写几个字再发送吧。", "Write a few words first.")
		return
	}
	if utf8.RuneCountInString(msg) > 4000 || len(msg) > 16_000 {
		m.feedbackErr = theme.Tr("内容最多 4000 字，精简一下再发吧。", "Messages can be up to 4,000 characters.")
		return
	}
	contact := strings.TrimSpace(m.feedbackContact)
	if contact != "" && (len(contact) > 254 || !validFeedbackEmail(contact)) {
		m.feedbackErr = theme.Tr("邮箱格式不太对，检查一下？", "That email doesn't look right.")
		return
	}
	if m.feedbackBlocked() {
		return
	}
	m.feedbackErr = ""
	if m.FeedbackSubmitFn != nil {
		if err := m.FeedbackSubmitFn(m.feedbackCategory, msg, contact); err != nil {
			m.feedbackErr = err.Error()
			return
		}
		m.finishFeedbackSent(contact)
		return
	}
	cat := normalizeFeedbackCat(m.feedbackCategory)
	atts := append([]feedback.Attachment(nil), m.fb.attachments...)
	logText := ""
	if m.feedbackLogAttached() {
		logText = m.fb.log.Text
	}
	submitKey := feedbackSubmitKey(cat, msg, contact, atts, logText)
	reqID := m.fb.lastRequestID
	if reqID == "" || m.fb.lastSubmitKey != submitKey {
		reqID = feedback.NewClientRequestID()
		m.fb.lastRequestID = reqID
		m.fb.lastSubmitKey = submitKey
	}
	report := feedback.Report{
		ClientRequestID: reqID,
		Category:        cat,
		Message:         msg,
		Contact:         contact,
		AppVersion:      updater.CurrentVersion(),
		Lang:            string(theme.Language()),
		Attachments:     atts,
		Log:             logText,
	}
	m.persistFeedbackDraft()
	m.fb.sendContact = contact
	m.fb.sender = feedback.NewSender(report, m.requestRedraw)
	m.fb.sender.Start()
}

func feedbackSubmitKey(category, msg, contact string, atts []feedback.Attachment, logText string) string {
	h := sha256.New()
	writeField := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
		h.Write([]byte{'\n'})
	}
	writeField(category)
	writeField(msg)
	writeField(contact)
	writeField(strconv.Itoa(len(atts)))
	for _, a := range atts {
		writeField(a.ID)
		writeField(a.Path)
		writeField(a.UploadPath)
		writeField(a.Name)
		writeField(strconv.FormatInt(a.Bytes, 10))
	}
	writeField(logText)
	return hex.EncodeToString(h.Sum(nil))
}

// finishFeedbackSent switches to the thank-you state and drops everything
// the report carried.
func (m *Manager) finishFeedbackSent(contact string) {
	m.fb.sender = nil
	m.fb.lastRequestID = ""
	m.fb.lastSubmitKey = ""
	for _, a := range m.fb.attachments {
		a.Discard()
	}
	m.fb.attachments = nil
	m.fb.notice = ""
	m.fb.view = feedbackViewForm
	m.feedbackErr = ""
	m.feedbackSentContact = contact
	m.feedbackDone = true
	m.feedbackMessage = ""
	m.feedbackCategory = "bug"
	if m.fb.updaterMode {
		m.fb.updaterDraftMsg = ""
		m.leaveUpdaterFeedbackModeLocked()
		return
	}
	m.mutateState(func(s *model.AppState) {
		s.FeedbackDraft = nil
	})
}

// backToFeedbackForm abandons the upload and returns to the form with the
// text and attachments intact.
func (m *Manager) backToFeedbackForm() {
	if s := m.fb.sender; s != nil {
		s.Cancel()
	}
	m.fb.sender = nil
	m.fb.view = feedbackViewForm
}

// feedbackSendErrorText says what went wrong in terms of what to do next.
func feedbackSendErrorText(e *feedback.SendError) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case feedback.ErrNotConfigured:
		return theme.Tr("反馈服务还没有开通，这次什么都没有发出去。你写的内容已留作草稿。",
			"The feedback service is not live yet, so nothing was sent. Your text is kept as a draft.")
	case feedback.ErrOffline:
		return theme.Tr("连不上反馈服务。检查网络后重试，已传完的部分不会重传。",
			"Could not reach the feedback service. Check your connection and retry; finished parts are not sent again.")
	case feedback.ErrServer:
		return fmt.Sprintf(theme.Tr("反馈服务暂时出了问题（%d）。稍后重试，已传完的部分不会重传。",
			"The feedback service had a problem (%d). Retry in a moment; finished parts are not sent again."), e.Status)
	case feedback.ErrRateLimited:
		if secs := int((e.RetryAfter + time.Second - 1) / time.Second); secs > 0 {
			return fmt.Sprintf(theme.Tr("提交太频繁，请约 %d 秒后再重试，已传完的部分不会重传。",
				"Too many requests. Wait about %d seconds and retry; finished parts are not sent again."), secs)
		}
		return theme.Tr("提交太频繁，请稍后再重试，已传完的部分不会重传。",
			"Too many requests. Retry in a moment; finished parts are not sent again.")
	case feedback.ErrTooLarge:
		return theme.Tr("服务端拒收：内容超出大小上限。返回移除较大的附件再发。",
			"The service refused the size. Go back and remove the larger attachments.")
	case feedback.ErrLocalFile:
		return theme.Tr("有附件已经读不到了，可能被移动或删除。返回移除它再发。",
			"An attachment can no longer be read; it may have been moved or deleted. Go back and remove it.")
	default:
		return fmt.Sprintf(theme.Tr("服务端没有接受这次反馈（%d）。", "The service did not accept this report (%d)."), e.Status)
	}
}

func feedbackRetryable(e *feedback.SendError) bool {
	return e != nil && (e.Kind == feedback.ErrOffline || e.Kind == feedback.ErrServer || e.Kind == feedback.ErrRateLimited)
}

func feedbackProgressPair(sent, total int64) string {
	const mb = 1 << 20
	if total >= mb {
		return fmt.Sprintf("%.1f / %.1f MB", float64(sent)/mb, float64(total)/mb)
	}
	totalKB := max((total+512)/1024, 1)
	sentKB := (sent + 512) / 1024
	if sent >= total && total > 0 {
		sentKB = totalKB
	} else if sentKB > totalKB {
		sentKB = totalKB
	}
	return fmt.Sprintf("%d / %d KB", sentKB, totalKB)
}

// --- Rendering ---

func feedbackMeta(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
		FontFeatures("tnum").TextColor(color).SingleLine().Shrink(0)
}

func feedbackFieldLabel(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
		FontWeight(theme.WeightRegular).TextColor(theme.Ocean().TextSecondary).SingleLine()
}

func feedbackTile(c *ui.Context, glyph icons.Icon, fg, bg ui.Color) {
	ui.Box(c).Size(feedbackTileSize, feedbackTileSize).Shrink(0).Radius(theme.RadiusSM).Background(bg).Center().
		Children(func() {
			icons.Draw(c, glyph, 16, theme.Ocean().DuoIconColors(fg))
		})
}

func feedbackAttachmentIcon(k feedback.Kind, name string) icons.Icon {
	switch k {
	case feedback.KindImage:
		if strings.EqualFold(filepath.Ext(name), ".svg") {
			return icons.FileSvg
		}
		return icons.Image
	case feedback.KindVideo:
		return icons.Video
	}
	if name != "" {
		if ic, ok := icons.FileIcon(name); ok {
			return ic
		}
	}
	return icons.Note
}

func feedbackRowName(c *ui.Context, name string) {
	ui.Text(c, name).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
		TextColor(theme.Ocean().Text).SingleLine().Grow(1).MinWidth(0)
}

func feedbackHeader(c *ui.Context, key string, body func()) {
	ui.Row(c.Key(key)).FillWidth().Height(48).PaddingX(theme.Space20).Gap(theme.Space8).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).
		BorderWidth(0, 0, 1, 0).BorderColor(theme.Ocean().Hairline).Children(body)
}

func feedbackTitle(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
		FontWeight(theme.WeightTitle).TextColor(theme.Ocean().Text).SingleLine()
}

func feedbackFooter(c *ui.Context, key string, body func()) {
	ui.Row(c.Key(key)).FillWidth().MinWidth(0).Padding(theme.Space12, theme.Space20).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Ocean().Hairline).
		AlignItems(ui.Center).Justify(ui.End).Gap(theme.Space8).Children(body)
}

// feedbackBand is a one-paragraph notice with an optional action: danger
// for something that blocks sending, neutral otherwise.
func feedbackBand(c *ui.Context, key, text string, danger bool, action func()) {
	pal := theme.Ocean()
	bg, bd, fg := pal.HintBand, pal.Hairline, pal.TextSecondary
	if danger {
		bg, bd, fg = pal.DangerSurface, pal.Danger.Alpha(0.35), pal.MenuDangerFG
	}
	ui.Row(c.Key(key)).FillWidth().MinWidth(0).Padding(theme.Space8, theme.Space12).Gap(theme.Space12).
		AlignItems(ui.Center).Radius(theme.RadiusMD).Background(bg).Border(1, bd).
		Transition(internalui.CascadeRowTransition(0)).Children(func() {
		ui.Text(c, text).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
			TextColor(fg).Grow(1).MinWidth(0)
		if action != nil {
			action()
		}
	})
}

func (m *Manager) renderFeedbackModal(c *ui.Context) {
	open := true
	title := theme.Tr("意见反馈", "Feedback")
	width := feedbackWidth
	if m.feedbackDone {
		width = feedbackDoneWidth
	}
	internalui.Dialog(c, "feedback-modal", title, &open, internalui.ModalLayout{Width: width, Centered: true}, func() {
		switch {
		case m.feedbackDone:
			m.renderFeedbackDone(c)
		case m.fb.sender != nil:
			m.renderFeedbackSending(c, m.fb.sender.Snapshot())
		case m.fb.view == feedbackViewLog:
			m.renderFeedbackLog(c)
		default:
			m.renderFeedbackForm(c, title)
		}
	})
	if !open {
		if m.fb.view == feedbackViewLog && m.fb.sender == nil && !m.feedbackDone {
			// Escape steps back from the log preview before closing.
			m.fb.view = feedbackViewForm
			return
		}
		m.CloseFeedback()
	}
}

func (m *Manager) renderFeedbackDone(c *ui.Context) {
	pal := theme.Ocean()
	doneBox := ui.Column(c.Key("feedback-done")).FillWidth().Padding(theme.Space24).Gap(theme.Space12).AlignItems(ui.Center).
		Transition(internalui.SurfaceStageTransition())
	internalui.Probe(doneBox, "feedback-done")
	doneBox.Children(func() {
		ui.Box(c).Size(52, 52).Radius(theme.RadiusPill).Background(pal.SuccessSurface).
			Border(1, pal.Success).Center().Children(func() {
			icons.DrawAnimated(c, icons.Check, 24, pal.DuoIconColors(pal.Success), true)
		})
		ui.Text(c, theme.Tr("收到了，谢谢你", "Got it, thank you")).
			FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).FontWeight(theme.WeightTitle).TextColor(pal.Text)
		if m.feedbackSentContact != "" {
			ui.Text(c, theme.Tr("有进展会回复到 "+m.feedbackSentContact, "We will reply to "+m.feedbackSentContact)).
				FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary)
		}
		doneBtn := internalui.BtnConfirm(c, "feedback-done-close", internalui.PresetOptions{
			Label: theme.Tr("好的", "Done"),
			Style: internalui.StyleRef(internalui.ButtonSecondary),
		})
		if doneBtn.Clicked() {
			m.CloseFeedback()
		}
	})
}

func (m *Manager) renderFeedbackForm(c *ui.Context, title string) {
	pal := theme.Ocean()
	form := ui.Column(c.Key("feedback-form")).FillWidth().MinWidth(0).
		Transition(internalui.SurfaceStageTransition())
	// Files dropped anywhere on the form become attachments.
	if dropped := form.DroppedFiles(); len(dropped) > 0 {
		m.addFeedbackFiles(dropped)
	}
	form.Children(func() {
		feedbackHeader(c, "feedback-header", func() {
			feedbackTitle(c, title)
			cls := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
				ID:    "feedback-close",
				Label: theme.Tr("关闭", "Close"),
			})
			if cls.Clicked() {
				m.CloseFeedback()
			}
		})

		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space16).Children(func() {
			cats := []struct {
				code string
				zh   string
				en   string
				phZh string
				phEn string
			}{
				{"bug", "问题反馈", "Bug", "描述遇到的问题或复现步骤…", "Describe the issue or steps to reproduce…"},
				{"idea", "功能建议", "Feature idea", "描述希望新增或改进的功能…", "Describe the feature or improvement…"},
				{"other", "其他", "Other", "写下你的想法或问题…", "Write your feedback…"},
			}
			catIdx := 0
			segOpts := make([]internalui.SegmentOption, len(cats))
			for i, cat := range cats {
				if m.feedbackCategory == cat.code {
					catIdx = i
				}
				segOpts[i] = internalui.SegmentOption{
					ID:    "feedback-cat-" + cat.code,
					Label: theme.Tr(cat.zh, cat.en),
				}
			}
			activePH := theme.Tr(cats[catIdx].phZh, cats[catIdx].phEn)
			if internalui.SegmentedControlWithOptions(c, "feedback-cat-seg", &catIdx, segOpts, internalui.SegmentedControlOptions{
				FillWidth:     true,
				Quiet:         true,
				TrackHeight:   theme.HeightLarge,
				TrackPadding:  theme.Space4,
				SegmentHeight: theme.HeightCompact,
				SegmentRadius: theme.RadiusSM,
			}) {
				m.feedbackCategory = cats[catIdx].code
				m.feedbackErr = ""
				m.persistFeedbackDraft()
			}

			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				feedbackFieldLabel(c, theme.Tr("内容", "Message"))
				msgH := float32(72)
				if strings.Contains(m.feedbackMessage, updaterDiagMarker) {
					msgH = 136
				}
				msgIn := ui.TextArea(c.Key("feedback-message"), &m.feedbackMessage).
					Placeholder(activePH).PlaceholderColor(pal.Placeholder).
					Label(theme.Tr("反馈内容", "Feedback message")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					Padding(theme.Space8, theme.Space10).Radius(theme.RadiusMD).TextColor(pal.Text).
					FillWidth().MinWidth(0).Height(msgH)
				feedbackInputChrome(msgIn)
				internalui.AutoHideScrollbars(c, msgIn)
				internalui.Probe(msgIn, "feedback-message")
				if msgIn.Changed() {
					m.feedbackErr = ""
					m.persistFeedbackDraft()
				}
			})

			m.renderFeedbackAttachments(c)
			m.renderFeedbackLogChoice(c)

			ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				feedbackFieldLabel(c, theme.Tr("回复邮箱（选填）", "Reply email (optional)"))
				contactIn := ui.TextInput(c.Key("feedback-contact"), &m.feedbackContact).
					Placeholder("name@example.com").PlaceholderColor(pal.Placeholder).
					Label(theme.Tr("回复邮箱", "Reply email")).
					FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					Height(theme.HeightControl).Padding(0, theme.Space10).Radius(theme.RadiusMD).TextColor(pal.Text).
					FillWidth().MinWidth(0)
				feedbackInputChrome(contactIn)
				internalui.Probe(contactIn, "feedback-contact")
				if contactIn.Changed() {
					m.feedbackErr = ""
					m.persistFeedbackDraft()
				}
			})

			if m.feedbackErr != "" {
				feedbackBand(c, "feedback-error", m.feedbackErr, true, nil)
			}
		})

		feedbackFooter(c, "feedback-footer", func() {
			cancelBtn := internalui.BtnCancel(c, "feedback-cancel", internalui.PresetOptions{})
			if cancelBtn.Clicked() {
				m.CloseFeedback()
			}
			sendIc := icons.Send
			sendBtn := internalui.ActionButton(c, "feedback-send", theme.Tr("发送", "Send"), &sendIc, internalui.ButtonPrimary, m.feedbackBlocked())
			if sendBtn.Clicked() {
				m.submitFeedback()
			}
		})
	})
}

func feedbackInputChrome(in ui.Element) {
	pal := theme.Ocean()
	switch {
	case in.Focused():
		in.Background(pal.Card).Border(1, pal.Accent)
	case in.Hovered():
		in.Background(pal.Shell).Border(1, pal.BtnBorder)
	default:
		in.Background(pal.Shell).Border(1, pal.Hairline)
	}
}

func (m *Manager) renderFeedbackAttachments(c *ui.Context) {
	pal := theme.Ocean()
	fb := &m.fb
	count := len(fb.attachments)
	total := feedback.TotalBytes(fb.attachments)
	over := m.feedbackOverLimit()
	totalOver := total > feedback.MaxTotalBytes

	ui.Column(c.Key("feedback-attachments")).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			feedbackFieldLabel(c, theme.Tr("附件", "Attachments"))
			switch {
			case over > 0:
				feedbackMeta(c, fmt.Sprintf(theme.Tr("%d / %d · %d 个超出大小", "%d / %d · %d over the size limit"),
					count, feedback.MaxAttachments, over), pal.MenuDangerFG)
			case count > 0:
				color := pal.TextSecondary
				if totalOver {
					color = pal.MenuDangerFG
				}
				feedbackMeta(c, fmt.Sprintf("%d / %d · %s / %s", count, feedback.MaxAttachments,
					feedback.FormatBytes(total), feedback.FormatBytes(feedback.MaxTotalBytes)), color)
			}
		})

		if count > 0 || len(fb.pending) > 0 {
			ui.Column(c.Key("feedback-attachment-list")).FillWidth().MinWidth(0).Radius(theme.RadiusMD).
				Border(1, pal.Hairline).Clip().Children(func() {
				for i, a := range fb.attachments {
					m.renderFeedbackAttachmentRow(c, i, a)
				}
				if n := len(fb.pending); n > 0 {
					row := ui.Row(c.Key("feedback-attachment-pending")).FillWidth().Height(feedbackRowHeight).
						Padding(0, theme.Space8, 0, theme.Space6).Gap(theme.Space10).AlignItems(ui.Center).
						Transition(internalui.CascadeRowTransition(count))
					if count > 0 {
						row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
					}
					row.Children(func() {
						ui.Box(c).Size(feedbackTileSize, feedbackTileSize).Shrink(0).Center().Children(func() {
							ui.Spinner(c).FontSize(theme.FontTitle).TextColor(pal.Accent)
						})
						ui.Text(c, fmt.Sprintf(theme.Tr("正在处理 %d 个文件…", "Preparing %d file(s)…"), n)).
							FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).TextColor(pal.TextSecondary).SingleLine()
					})
				}
			})
		}

		switch {
		case over > 0:
			feedbackBand(c, "feedback-limit", fmt.Sprintf(theme.Tr(
				"视频最大 %s，其他文件最大 %s。把录屏剪短到出问题的那几秒再试。",
				"Videos up to %s, other files up to %s. Trim the recording to the few seconds that show the problem."),
				feedback.FormatBytes(feedback.MaxVideoBytes), feedback.FormatBytes(feedback.MaxFileBytes)), true, func() {
				btn := internalui.BtnDelete(c, "feedback-remove-over", internalui.PresetOptions{
					Label:  theme.Tr("移除超限项", "Remove oversized"),
					Style:  internalui.StyleRef(internalui.ButtonSecondary),
					Height: theme.HeightCompact,
				})
				if btn.Clicked() {
					m.removeFeedbackOverLimit()
				}
			})
		case totalOver:
			feedbackBand(c, "feedback-limit", fmt.Sprintf(theme.Tr(
				"附件合计最多 %s，现在是 %s。移除一些再发。",
				"Attachments can total %s; these total %s. Remove some to send."),
				feedback.FormatBytes(feedback.MaxTotalBytes), feedback.FormatBytes(total)), true, nil)
		case fb.notice != "":
			feedbackBand(c, "feedback-notice", fb.notice, false, nil)
		}

		full := count+len(fb.pending) >= feedback.MaxAttachments
		ui.Row(c).FillWidth().Gap(theme.Space8).Children(func() {
			mediaIc, fileIc := icons.Image, icons.Paperclip
			if internalui.ActionButton(c, "feedback-add-media", theme.Tr("图片或视频", "Image or video"), &mediaIc, internalui.ButtonSecondary, full).Clicked() {
				m.pickFeedbackFiles(true)
			}
			if internalui.ActionButton(c, "feedback-add-file", theme.Tr("文件", "File"), &fileIc, internalui.ButtonSecondary, full).Clicked() {
				m.pickFeedbackFiles(false)
			}
		})
	})
}

func (m *Manager) renderFeedbackAttachmentRow(c *ui.Context, index int, a feedback.Attachment) {
	pal := theme.Ocean()
	over := a.OverLimit()
	bg, tileBG, tileFG, metaFG := pal.Card, pal.Shell, pal.TextSecondary, pal.TextSecondary
	if over {
		bg, tileBG, tileFG, metaFG = pal.DangerSurface, pal.Card, pal.Danger, pal.MenuDangerFG
	}
	meta := feedback.FormatBytes(a.Bytes)
	switch {
	case over:
		meta = fmt.Sprintf(theme.Tr("%s · 上限 %s", "%s · limit %s"), meta, feedback.FormatBytes(a.LimitBytes()))
	case a.Compressed():
		meta = feedback.FormatBytes(a.OrigBytes) + " → " + meta
	case a.Kind == feedback.KindVideo && a.Duration > 0:
		meta = feedback.FormatDuration(a.Duration) + " · " + meta
	}
	row := ui.Row(c.Key("feedback-att-"+a.ID)).FillWidth().Height(feedbackRowHeight).
		Padding(0, theme.Space8, 0, theme.Space6).Gap(theme.Space10).AlignItems(ui.Center).
		Background(bg).Transition(internalui.CascadeRowTransition(index))
	if index > 0 {
		row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
	}
	row.Children(func() {
		feedbackTile(c, feedbackAttachmentIcon(a.Kind, a.Name), tileFG, tileBG)
		feedbackRowName(c, a.Name)
		feedbackMeta(c, meta, metaFG)
		rm := internalui.IconButton(c, icons.Close, internalui.IconButtonOptions{
			ID:      "feedback-att-remove-" + a.ID,
			Label:   fmt.Sprintf(theme.Tr("移除 %s", "Remove %s"), a.Name),
			Variant: internalui.IconPlain,
			Danger:  over,
		})
		if rm.Clicked() {
			m.removeFeedbackAttachment(a.ID)
		}
	})
}

func (m *Manager) renderFeedbackLogChoice(c *ui.Context) {
	pal := theme.Ocean()
	fb := &m.fb
	hasLog := len(fb.log.Lines) > 0
	ui.Column(c.Key("feedback-log")).FillWidth().MinWidth(0).Radius(theme.RadiusMD).
		Background(pal.HintBand).Border(1, pal.Hairline).Children(func() {
		ui.Row(c).FillWidth().Height(feedbackRowHeight).PaddingX(theme.Space12).Gap(theme.Space8).
			AlignItems(ui.Center).Children(func() {
			checked := fb.includeLog && hasLog
			if internalui.Checkbox(c, "feedback-log-check", theme.Tr("附带诊断日志", "Attach diagnostic log"), &checked, !hasLog) {
				fb.includeLog = checked
			}
			ui.Box(c).Grow(1)
			if !hasLog {
				feedbackMeta(c, theme.Tr("暂无日志", "No log yet"), pal.TextSecondary)
				return
			}
			feedbackMeta(c, feedback.FormatBytes(fb.log.Bytes()), pal.TextSecondary)
			link := ui.ButtonBase(c.Key("feedback-log-view")).Cursor(ui.CursorPointer).
				Label(theme.Tr("查看日志内容", "View log contents"))
			color := pal.Link
			if link.Hovered() {
				color = pal.SelectionFG
			}
			link.Children(func() {
				ui.Text(c, theme.Tr("查看内容", "View")).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).TextColor(color).SingleLine()
			})
			internalui.Probe(link, "feedback-log-view")
			if link.Clicked() {
				fb.view = feedbackViewLog
			}
		})
		ui.Row(c).FillWidth().MinWidth(0).Padding(theme.Space10, theme.Space12).Gap(theme.Space8).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			icons.Draw(c, icons.ShieldCheck, 16, pal.DuoIconColors(pal.Success))
			ui.Text(c, theme.Tr("日志在这台设备上打码后才发送，不含笔记内容、路径和密钥。你添加的附件会连同文件名原样发送。",
				"The log is masked on this device before sending, with no note content, paths or keys. Attachments you add are sent as-is, file names included.")).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).Grow(1).MinWidth(0)
		})
	})
}

// --- Log preview ---

func (m *Manager) renderFeedbackLog(c *ui.Context) {
	pal := theme.Ocean()
	fb := &m.fb
	ui.Column(c.Key("feedback-logview")).FillWidth().MinWidth(0).
		Transition(internalui.SurfaceStageTransition()).Children(func() {
		feedbackHeader(c, "feedback-logview-header", func() {
			ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).MinWidth(0).Children(func() {
				back := internalui.IconButton(c, icons.ArrowLeft, internalui.IconButtonOptions{
					ID:    "feedback-log-back",
					Label: theme.Tr("返回", "Back"),
				})
				if back.Clicked() {
					fb.view = feedbackViewForm
				}
				feedbackTitle(c, theme.Tr("将要发送的日志", "The log that will be sent"))
			})
			feedbackMeta(c, fmt.Sprintf(theme.Tr("最近 %d 条 · %s", "Last %d lines · %s"),
				len(fb.log.Lines), feedback.FormatBytes(fb.log.Bytes())), pal.TextSecondary)
		})

		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space12).Children(func() {
			feedbackFieldLabel(c, theme.Tr("写入日志时就已打码", "Masked when the log is written"))
			rows := []struct{ zh, en, tags string }{
				{"用户名与文件路径", "User name and file paths", "<user>  <path:3f2a.md>"},
				{"笔记标题与文件名", "Note titles and file names", "<text:5d11>  <file:91c0.pdf>"},
				{"密钥与令牌", "Keys and tokens", "<key>"},
				{"邮箱、网址与 IP", "Emails, URLs and IPs", "<email>  <host:07be>  <ip>"},
			}
			ui.Column(c).FillWidth().MinWidth(0).Radius(theme.RadiusMD).Border(1, pal.Hairline).Clip().Children(func() {
				for i, r := range rows {
					row := ui.Row(c).FillWidth().Height(theme.HeightControl).PaddingX(theme.Space12).Gap(theme.Space12).
						AlignItems(ui.Center).Justify(ui.SpaceBetween)
					if i > 0 {
						row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
					}
					row.Children(func() {
						ui.Text(c, theme.Tr(r.zh, r.en)).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
							TextColor(pal.Text).SingleLine()
						ui.Text(c, r.tags).Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							FontWeight(theme.WeightMono).TextColor(pal.SelectionFG).SingleLine()
					})
				}
			})
			ui.Text(c, theme.Tr("笔记正文、AI 对话和剪贴板内容从不写入日志。日志只存在这台设备上，保留 7 天，不勾选就不会发送。",
				"Note text, AI conversations and clipboard contents are never written to the log. It stays on this device for 7 days and is only sent when the box is checked.")).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary).FillWidth().MinWidth(0)

			box := internalui.Scroll(c.Key("feedback-log-lines"), &fb.logScroll).Column().FillWidth().MinWidth(0).
				Height(220).Padding(theme.Space8, theme.Space10).Radius(theme.RadiusMD).
				Background(pal.Shell).Border(1, pal.Hairline)
			box.Children(func() {
				for _, line := range fb.log.Lines {
					feedbackLogLine(c, line)
				}
			})
		})

		feedbackFooter(c, "feedback-logview-footer", func() {
			copied := time.Since(fb.copiedAt) < 1500*time.Millisecond
			label, glyph := theme.Tr("复制", "Copy"), icons.Copy
			if copied {
				label, glyph = theme.Tr("已复制", "Copied"), icons.Check
			}
			if internalui.ActionButton(c, "feedback-log-copy", label, &glyph, internalui.ButtonSecondary, false).Clicked() {
				c.Services().WriteClipboard(fb.log.Text)
				fb.copiedAt = time.Now()
				time.AfterFunc(1600*time.Millisecond, m.requestRedraw)
			}
			if internalui.BtnBack(c, "feedback-log-done", internalui.PresetOptions{
				Style: internalui.StyleRef(internalui.ButtonPrimary),
			}).Clicked() {
				fb.view = feedbackViewForm
			}
		})
	})
}

// feedbackLogLine draws one masked log line with its mask tags highlighted.
func feedbackLogLine(c *ui.Context, line files.DiagnosticLine) {
	pal := theme.Ocean()
	span := func(text string, fg, bg ui.Color) ui.Span {
		return ui.Span{Text: text, Font: "monospace", Size: theme.FontMeta, Weight: theme.WeightMono, Color: fg, Background: bg}
	}
	levelFG := pal.TextSecondary
	switch line.Level {
	case "ERROR":
		levelFG = pal.Danger
	case "WARN":
		levelFG = pal.Text
	}
	spans := []ui.Span{
		span(line.Time+" ", pal.Placeholder, ui.Transparent),
		span(line.Level+" ", levelFG, ui.Transparent),
	}
	prev := 0
	for _, loc := range files.RedactionTagSpans(line.Message) {
		if loc[0] > prev {
			spans = append(spans, span(line.Message[prev:loc[0]], pal.TextSecondary, ui.Transparent))
		}
		spans = append(spans, span(line.Message[loc[0]:loc[1]], pal.SelectionFG, pal.SelectionBG))
		prev = loc[1]
	}
	if prev < len(line.Message) {
		spans = append(spans, span(line.Message[prev:], pal.TextSecondary, ui.Transparent))
	}
	ui.RichText(c, spans...).FixedLineHeight(theme.LineSmall).FillWidth().MinWidth(0).Shrink(0)
}

// --- Upload progress ---

func (m *Manager) renderFeedbackSending(c *ui.Context, snap feedback.Snapshot) {
	pal := theme.Ocean()
	failed := snap.Phase == feedback.PhaseFailed
	ui.Column(c.Key("feedback-sending")).FillWidth().MinWidth(0).
		Transition(internalui.SurfaceStageTransition()).Children(func() {
		feedbackHeader(c, "feedback-sending-header", func() {
			switch {
			case failed:
				feedbackTitle(c, theme.Tr("发送没有完成", "Sending did not finish"))
			default:
				feedbackTitle(c, theme.Tr("正在发送反馈", "Sending feedback"))
			}
			switch {
			case !failed && snap.Attempt > 1:
				feedbackMeta(c, fmt.Sprintf(theme.Tr("网络不稳，正在重试 %d/%d", "Unstable connection, retry %d/%d"),
					snap.Attempt, feedback.MaxAttempts), pal.TextSecondary)
			case snap.Total > 0:
				feedbackMeta(c, feedbackProgressPair(snap.Sent, snap.Total), pal.TextSecondary)
			}
		})
		internalui.ProgressBar(c, "feedback-progress-total", snap.Percent(), 2, failed).
			Label(theme.Tr("总进度", "Overall progress"))

		ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space16, theme.Space20).Gap(theme.Space16).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(theme.Space8, theme.Space10).Radius(theme.RadiusMD).
				Background(pal.Shell).Border(1, pal.Hairline).Children(func() {
				internalui.FadedTextPreview(c, strings.TrimSpace(m.feedbackMessage), 2,
					theme.FontBody, theme.LineBody, pal.TextSecondary, pal.Shell)
			})

			if len(snap.Parts) > 0 {
				ui.Column(c.Key("feedback-parts")).FillWidth().MinWidth(0).Radius(theme.RadiusMD).
					Border(1, pal.Hairline).Clip().Children(func() {
					for i, p := range snap.Parts {
						renderFeedbackPart(c, i, p)
					}
				})
			}

			if failed {
				feedbackBand(c, "feedback-send-error", feedbackSendErrorText(snap.Err), true, nil)
			}
		})

		feedbackFooter(c, "feedback-sending-footer", func() {
			if failed {
				if internalui.BtnBack(c, "feedback-send-back", internalui.PresetOptions{
					Label: theme.Tr("返回修改", "Back to edit"),
					Style: internalui.StyleRef(internalui.ButtonSecondary),
				}).Clicked() {
					m.backToFeedbackForm()
				}
				if feedbackRetryable(snap.Err) {
					if internalui.BtnRetry(c, "feedback-send-retry", internalui.PresetOptions{
						Style: internalui.StyleRef(internalui.ButtonPrimary),
					}).Clicked() {
						m.fb.sender.Retry()
					}
				}
				return
			}
			if internalui.BtnCancel(c, "feedback-send-cancel", internalui.PresetOptions{
				Label: theme.Tr("取消发送", "Cancel sending"),
			}).Clicked() {
				m.backToFeedbackForm()
			}
			bgIc := icons.Clock
			if internalui.ActionButton(c, "feedback-send-background", theme.Tr("后台继续", "Continue in background"), &bgIc, internalui.ButtonSecondary, false).Clicked() {
				m.CloseFeedback()
			}
		})
	})
}

func renderFeedbackPart(c *ui.Context, index int, p feedback.Part) {
	pal := theme.Ocean()
	uploading := p.State == feedback.PartUploading
	failed := p.State == feedback.PartFailed
	bg := pal.Card
	switch {
	case failed:
		bg = pal.DangerSurface
	case p.IsLog:
		bg = pal.HintBand
	}
	row := ui.Column(c.Key("feedback-part-"+p.ID)).FillWidth().MinWidth(0).MinHeight(feedbackRowHeight).
		Padding(theme.Space6, theme.Space12, theme.Space6, theme.Space6).Gap(theme.Space4).Justify(ui.Center).
		Background(bg).Transition(internalui.CascadeRowTransition(index))
	if index > 0 {
		row.BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space10).AlignItems(ui.Center).Children(func() {
			name := p.Name
			if p.IsLog {
				name = theme.Tr("诊断日志（已打码）", "Diagnostic log (masked)")
				feedbackTile(c, icons.ShieldCheck, pal.Success, pal.SuccessSurface)
			} else {
				feedbackTile(c, feedbackAttachmentIcon(p.Kind, p.Name), pal.TextSecondary, pal.Shell)
			}
			feedbackRowName(c, name)
			size := feedback.FormatBytes(p.Total)
			switch p.State {
			case feedback.PartDone:
				feedbackMeta(c, size, pal.TextSecondary)
				icons.DrawAnimated(c, icons.Check, 16, pal.DuoIconColors(pal.Success), true)
			case feedback.PartUploading:
				if p.Attempt > 1 {
					feedbackMeta(c, fmt.Sprintf(theme.Tr("重试 %d/%d", "Retry %d/%d"), p.Attempt, feedback.MaxAttempts), pal.TextSecondary)
				} else {
					feedbackMeta(c, feedbackProgressPair(p.Sent, p.Total), pal.TextSecondary)
				}
				pct := 0
				if p.Total > 0 {
					pct = int(min(p.Sent*100/p.Total, 100))
				}
				feedbackMeta(c, fmt.Sprintf("%d%%", pct), pal.Accent).Width(38).TextAlign(ui.End)
			case feedback.PartFailed:
				feedbackMeta(c, size+theme.Tr(" · 没传上去", " · not uploaded"), pal.MenuDangerFG)
			default:
				feedbackMeta(c, size+theme.Tr(" · 等待中", " · waiting"), pal.TextSecondary)
			}
		})
		if uploading {
			pct := float32(0)
			if p.Total > 0 {
				pct = float32(p.Sent) / float32(p.Total) * 100
			}
			ui.Box(c).FillWidth().Padding(0, 0, 0, feedbackTileSize+theme.Space10).Children(func() {
				internalui.ProgressBar(c, "feedback-part-bar-"+p.ID, pct, 4, false).Label(p.Name)
			})
		}
	})
}
