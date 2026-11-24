package feedback

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/files"
)

// Wire protocol. The receiving service is three plain HTTP calls under one
// base URL, so a failed part can be retried without resending the rest:
//
//	POST {base}/reports                       JSON reportRequest -> {"id": "...", "token": "..."}
//	PUT  {base}/reports/{id}/parts/{partID}   raw bytes, one call per attachment / log
//	POST {base}/reports/{id}/complete         marks the report whole
//
// "token", when the service returns one, is sent back as a Bearer token on
// the part and complete calls. Any 2xx is success.

// DefaultEndpoint is the base URL of the feedback service. Kept empty in the
// public repository and injected at release build time via -ldflags -X; when
// empty, sending fails with ErrNotConfigured instead of pretending it sent.
var DefaultEndpoint = ""

// DefaultClientToken is an optional build-time client identifier sent on
// POST /reports via X-Feedback-Client when configured. Kept empty in git.
// Treat it as a public client identifier (anti-casual-spam), not a secret;
// primary abuse defense lives in server-side rate limits and quotas.
var DefaultClientToken = ""

// LogPartID is the part id of the diagnostic log.
const LogPartID = "log"

// Endpoint resolves the service base URL: BLOCK_FEEDBACK_URL, else DefaultEndpoint.
func Endpoint() string {
	if env := strings.TrimSpace(os.Getenv("BLOCK_FEEDBACK_URL")); env != "" {
		return strings.TrimRight(env, "/")
	}
	return strings.TrimRight(strings.TrimSpace(DefaultEndpoint), "/")
}

// ClientToken resolves the optional client token: BLOCK_FEEDBACK_CLIENT_TOKEN,
// else DefaultClientToken.
func ClientToken() string {
	if env := strings.TrimSpace(os.Getenv("BLOCK_FEEDBACK_CLIENT_TOKEN")); env != "" {
		return env
	}
	return strings.TrimSpace(DefaultClientToken)
}

// ErrKind groups failures by what the user can do about them.
type ErrKind int

const (
	ErrNone          ErrKind = iota
	ErrNotConfigured         // no service address: nothing was sent
	ErrOffline               // could not reach the service: retry later
	ErrServer                // the service failed (5xx, 408): retry later
	ErrRateLimited           // the service asked us to slow down (429)
	ErrTooLarge              // the service refused the size (413)
	ErrRejected              // the service refused the report (other 4xx)
	ErrLocalFile             // an attachment could not be read any more
)

// SendError is a failed step with its kind and, for HTTP failures, status.
type SendError struct {
	Kind       ErrKind
	Status     int
	Code       string
	RetryAfter time.Duration
	Err        error
}

func (e *SendError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("feedback: http %d", e.Status)
}

func (e *SendError) Unwrap() error { return e.Err }

// retryable reports whether an automatic retry can help right away.
// Long rate-limit cooldowns (> 5s) surface to the user instead of stalling
// silently or adding more rate-limit strikes.
func (e *SendError) retryable() bool {
	if e.Kind == ErrOffline || e.Kind == ErrServer {
		return true
	}
	return e.Kind == ErrRateLimited && e.RetryAfter > 0 && e.RetryAfter <= 5*time.Second
}

func (e *SendError) sessionExpired() bool {
	return e != nil && e.Kind == ErrRejected &&
		(e.Code == "report_expired" || e.Code == "upload_budget_exhausted")
}

// Report is everything one feedback submission carries.
type Report struct {
	ClientRequestID string // optional idempotency key; auto-generated when empty
	Category        string
	Message         string
	Contact         string
	AppVersion      string
	Lang            string
	Attachments     []Attachment
	Log             string // masked diagnostic log; empty = not attached
}

// PartState is the upload state of one part.
type PartState int

const (
	PartWaiting PartState = iota
	PartUploading
	PartDone
	PartFailed
)

// Part is the progress of one attachment or the log.
type Part struct {
	ID      string
	Name    string
	Kind    Kind
	IsLog   bool
	Total   int64
	Sent    int64
	State   PartState
	Attempt int // 1-based try of the current upload; >1 means it is retrying
	Err     *SendError
}

// Phase is where the whole submission stands.
type Phase int

const (
	PhaseSending Phase = iota
	PhaseFailed
	PhaseDone
	PhaseCanceled
)

// Snapshot is a consistent copy of the sender's state for one frame.
type Snapshot struct {
	Phase        Phase
	Parts        []Part
	Sent         int64
	Total        int64
	StepProgress float32 // fallback 0..100 progress when Total == 0 (text-only report)
	Attempt      int     // try of the step in flight, across create / part / complete
	Err          *SendError
}

// Percent is the overall upload progress, 0–100.
func (s Snapshot) Percent() float32 {
	if s.Phase == PhaseDone {
		return 100
	}
	if s.Total <= 0 {
		return min(max(s.StepProgress, 0), 100)
	}
	return min(float32(s.Sent)/float32(s.Total)*100, 100)
}

// FailedParts counts the parts that need a retry.
func (s Snapshot) FailedParts() int {
	n := 0
	for _, p := range s.Parts {
		if p.State == PartFailed {
			n++
		}
	}
	return n
}

// MaxAttempts is how many times each step is tried before the submission
// stops and waits for the user's Retry.
const MaxAttempts = 3

var retryBackoff = []time.Duration{time.Second, 3 * time.Second}

type reportRequest struct {
	Project         string        `json:"project,omitempty"`
	ClientRequestID string        `json:"client_request_id,omitempty"`
	Category        string        `json:"category"`
	Message         string        `json:"message"`
	Contact         string        `json:"contact,omitempty"`
	App             reportApp     `json:"app"`
	Parts           []reportPartV `json:"parts"`
}

type reportApp struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Lang    string `json:"lang,omitempty"`
}

type reportPartV struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes"`
}

type reportResponse struct {
	ID               string `json:"id"`
	Token            string `json:"token"`
	AlreadyCompleted bool   `json:"already_completed"`
}

// Sender uploads one report in the background. It is safe to read from the
// UI thread through Snapshot; onChange is called (from the worker) whenever
// the snapshot changes.
type Sender struct {
	report          Report
	endpoint        string
	clientToken     string
	clientRequestID string
	client          *http.Client
	onChange        func()

	mu               sync.Mutex
	snap             Snapshot
	paths            map[string]string // part id -> file to upload
	reportID         string
	token            string
	alreadyCompleted bool
	cancel           context.CancelFunc
	running          bool
	lastTick         time.Time
}

// NewClientRequestID generates a 32-hex idempotency key for a feedback report.
func NewClientRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// NewSender prepares a submission; nothing is sent until Start.
func NewSender(r Report, onChange func()) *Sender {
	reqID := strings.TrimSpace(r.ClientRequestID)
	if reqID == "" {
		reqID = NewClientRequestID()
	}
	s := &Sender{
		report:          r,
		endpoint:        Endpoint(),
		clientToken:     ClientToken(),
		clientRequestID: reqID,
		onChange:        onChange,
		paths:           map[string]string{},
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		}},
	}
	for _, a := range r.Attachments {
		s.snap.Parts = append(s.snap.Parts, Part{ID: a.ID, Name: a.Name, Kind: a.Kind, Total: a.Bytes})
		s.paths[a.ID] = a.UploadPath
		s.snap.Total += a.Bytes
	}
	if r.Log != "" {
		s.snap.Parts = append(s.snap.Parts, Part{ID: LogPartID, Name: "block.log", IsLog: true, Total: int64(len(r.Log))})
		s.snap.Total += int64(len(r.Log))
	}
	return s
}

// Snapshot returns a copy of the current state.
func (s *Sender) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snap
	out.Parts = append([]Part(nil), s.snap.Parts...)
	return out
}

// Start begins (or, after a failure, resumes) the upload. Parts already
// uploaded are not sent again.
func (s *Sender) Start() {
	s.mu.Lock()
	if s.running || s.snap.Phase == PhaseDone {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.snap.Phase = PhaseSending
	s.snap.Err = nil
	s.snap.Attempt = 1
	for i := range s.snap.Parts {
		if s.snap.Parts[i].State == PartFailed {
			p := &s.snap.Parts[i]
			s.snap.Sent -= p.Sent
			p.State, p.Sent, p.Err, p.Attempt = PartWaiting, 0, nil, 0
		}
	}
	s.mu.Unlock()
	s.notify(true)
	go s.run(ctx)
}

// Retry resumes a failed submission.
func (s *Sender) Retry() { s.Start() }

// Cancel stops the upload. The report is left incomplete on the service.
func (s *Sender) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	if s.snap.Phase == PhaseSending || s.snap.Phase == PhaseFailed {
		s.snap.Phase = PhaseCanceled
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.notify(true)
}

func (s *Sender) notify(force bool) {
	if s.onChange == nil {
		return
	}
	s.mu.Lock()
	now := time.Now()
	if !force && now.Sub(s.lastTick) < 80*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastTick = now
	s.mu.Unlock()
	s.onChange()
}

func (s *Sender) finish(phase Phase, err *SendError) {
	s.mu.Lock()
	s.running = false
	if s.snap.Phase != PhaseCanceled {
		s.snap.Phase = phase
		s.snap.Err = err
	}
	phase = s.snap.Phase
	s.mu.Unlock()
	switch {
	case phase == PhaseDone:
		files.LogInfof("feedback sent parts=%d bytes=%d", len(s.snap.Parts), s.snap.Total)
	case phase == PhaseFailed && err != nil:
		files.LogWarnf("feedback send failed kind=%d status=%d: %v", int(err.Kind), err.Status, err)
	}
	s.notify(true)
}

func (s *Sender) resetSession() {
	s.mu.Lock()
	s.reportID = ""
	s.token = ""
	s.snap.Sent = 0
	s.snap.StepProgress = 15
	for i := range s.snap.Parts {
		p := &s.snap.Parts[i]
		p.State, p.Sent, p.Err, p.Attempt = PartWaiting, 0, nil, 0
	}
	s.mu.Unlock()
	s.notify(true)
}

func (s *Sender) setStepProgress(pct float32) {
	s.mu.Lock()
	s.snap.StepProgress = pct
	s.mu.Unlock()
	s.notify(true)
}

func (s *Sender) run(ctx context.Context) {
	if s.endpoint == "" {
		s.finish(PhaseFailed, &SendError{Kind: ErrNotConfigured, Err: errors.New("feedback service is not configured")})
		return
	}
	for sessionTry := 0; sessionTry < 2; sessionTry++ {
		if s.reportID == "" {
			s.setStepProgress(20)
			if err := s.withRetry(ctx, nil, s.createReport); err != nil {
				s.finish(PhaseFailed, err)
				return
			}
			s.mu.Lock()
			alreadyDone := s.alreadyCompleted
			s.mu.Unlock()
			if alreadyDone {
				s.finish(PhaseDone, nil)
				return
			}
			s.setStepProgress(55)
		}
		var firstErr *SendError
		sessionLost := false
		for i := range s.report.Attachments {
			a := s.report.Attachments[i]
			if err := s.uploadPart(ctx, a.ID); err != nil {
				if err.sessionExpired() && sessionTry == 0 {
					sessionLost = true
					break
				}
				if firstErr == nil {
					firstErr = err
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
		if !sessionLost && s.report.Log != "" {
			if err := s.uploadPart(ctx, LogPartID); err != nil {
				if err.sessionExpired() && sessionTry == 0 {
					sessionLost = true
				} else if firstErr == nil {
					firstErr = err
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if sessionLost {
			s.resetSession()
			continue
		}
		if firstErr != nil {
			s.finish(PhaseFailed, firstErr)
			return
		}
		s.setStepProgress(80)
		if err := s.withRetry(ctx, nil, s.completeReport); err != nil {
			if err.sessionExpired() && sessionTry == 0 {
				s.resetSession()
				continue
			}
			s.finish(PhaseFailed, err)
			return
		}
		s.finish(PhaseDone, nil)
		return
	}
}

// withRetry runs one step up to MaxAttempts times, backing off between
// tries. part, when set, has its Attempt kept in step for the UI.
func (s *Sender) withRetry(ctx context.Context, part *string, step func(context.Context) *SendError) *SendError {
	var last *SendError
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		s.mu.Lock()
		s.snap.Attempt = attempt
		if part != nil {
			if p := s.partLocked(*part); p != nil {
				p.Attempt = attempt
			}
		}
		s.mu.Unlock()
		s.notify(true)

		last = step(ctx)
		if last == nil {
			return nil
		}
		if ctx.Err() != nil || !last.retryable() || attempt == MaxAttempts {
			return last
		}
		wait := retryBackoff[min(attempt-1, len(retryBackoff)-1)]
		if last.RetryAfter > wait {
			wait = last.RetryAfter
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(wait):
		}
	}
	return last
}

func (s *Sender) partLocked(id string) *Part {
	for i := range s.snap.Parts {
		if s.snap.Parts[i].ID == id {
			return &s.snap.Parts[i]
		}
	}
	return nil
}

func (s *Sender) setPart(id string, fn func(*Part)) {
	s.mu.Lock()
	if p := s.partLocked(id); p != nil {
		fn(p)
	}
	s.mu.Unlock()
}

func kindName(k Kind, isLog bool) string {
	switch {
	case isLog:
		return "log"
	case k == KindImage:
		return "image"
	case k == KindVideo:
		return "video"
	}
	return "file"
}

func (s *Sender) createReport(ctx context.Context) *SendError {
	req := reportRequest{
		Project:         "block",
		ClientRequestID: s.clientRequestID,
		Category:        s.report.Category,
		Message:         s.report.Message,
		Contact:         s.report.Contact,
		App:             reportApp{Version: s.report.AppVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, Lang: s.report.Lang},
		Parts:           []reportPartV{},
	}
	for _, p := range s.Snapshot().Parts {
		req.Parts = append(req.Parts, reportPartV{ID: p.ID, Name: p.Name, Kind: kindName(p.Kind, p.IsLog), Bytes: p.Total})
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/reports", bytes.NewReader(body))
	if err != nil {
		return &SendError{Kind: ErrNotConfigured, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.clientToken != "" {
		httpReq.Header.Set("X-Feedback-Client", s.clientToken)
	}
	respBody, sendErr := s.do(httpReq)
	if sendErr != nil {
		return sendErr
	}
	var out reportResponse
	if err := json.Unmarshal(respBody, &out); err != nil || strings.TrimSpace(out.ID) == "" {
		return &SendError{Kind: ErrServer, Err: errors.New("feedback service returned no report id")}
	}
	s.mu.Lock()
	s.reportID, s.token, s.alreadyCompleted = out.ID, out.Token, out.AlreadyCompleted
	s.mu.Unlock()
	return nil
}

func (s *Sender) completeReport(ctx context.Context) *SendError {
	u := s.endpoint + "/reports/" + url.PathEscape(s.reportID) + "/complete"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return &SendError{Kind: ErrNotConfigured, Err: err}
	}
	_, sendErr := s.do(httpReq)
	return sendErr
}

// uploadPart sends one part with retries and records its outcome. A part
// already done is skipped, so Retry only resends what failed.
func (s *Sender) uploadPart(ctx context.Context, id string) *SendError {
	s.mu.Lock()
	p := s.partLocked(id)
	if p == nil || p.State == PartDone {
		s.mu.Unlock()
		return nil
	}
	p.State = PartUploading
	s.mu.Unlock()
	s.notify(true)

	err := s.withRetry(ctx, &id, func(ctx context.Context) *SendError { return s.putPart(ctx, id) })
	if ctx.Err() != nil {
		return err
	}
	s.setPart(id, func(p *Part) {
		if err != nil {
			// A failed part counts for nothing: the bar must not read as full.
			s.snap.Sent -= p.Sent
			p.State, p.Sent, p.Err = PartFailed, 0, err
			return
		}
		s.snap.Sent += p.Total - p.Sent
		p.State, p.Sent, p.Err = PartDone, p.Total, nil
	})
	s.notify(true)
	return err
}

func (s *Sender) putPart(ctx context.Context, id string) *SendError {
	// A retried part starts over: give back what the failed try counted.
	s.setPart(id, func(p *Part) {
		s.snap.Sent -= p.Sent
		p.Sent = 0
	})

	var (
		body io.Reader
		size int64
		name string
	)
	if id == LogPartID {
		body, size, name = strings.NewReader(s.report.Log), int64(len(s.report.Log)), "block.log"
	} else {
		p := s.paths[id]
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
			if err == nil {
				err = errors.New("feedback: attachment is not a regular file")
			}
			return &SendError{Kind: ErrLocalFile, Err: err}
		}
		f, err := os.Open(p)
		if err != nil {
			return &SendError{Kind: ErrLocalFile, Err: err}
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = errors.New("feedback: attachment is not a regular file")
			}
			return &SendError{Kind: ErrLocalFile, Err: err}
		}
		body, size = f, info.Size()
		s.mu.Lock()
		if p := s.partLocked(id); p != nil {
			name = p.Name
			if p.Total != size {
				s.snap.Total += size - p.Total
				p.Total = size
			}
		}
		s.mu.Unlock()
	}

	reqCtx, cancelReq := context.WithCancel(ctx)
	defer cancelReq()

	var (
		stallMu   sync.Mutex
		stallDone bool
		sentBytes int64
		stallTmr  *time.Timer
	)
	if size > 0 {
		stallTmr = time.AfterFunc(uploadStallTimeout, cancelReq)
		defer func() {
			stallMu.Lock()
			stallDone = true
			stallTmr.Stop()
			stallMu.Unlock()
		}()
	}

	counted := &countingReader{r: body, onRead: func(n int) {
		if stallTmr != nil {
			stallMu.Lock()
			sentBytes += int64(n)
			if !stallDone {
				if sentBytes >= size {
					stallDone = true
					stallTmr.Stop()
				} else {
					stallTmr.Reset(uploadStallTimeout)
				}
			}
			stallMu.Unlock()
		}
		s.setPart(id, func(p *Part) {
			p.Sent += int64(n)
			s.snap.Sent += int64(n)
		})
		s.notify(false)
	}}
	u := s.endpoint + "/reports/" + url.PathEscape(s.reportID) + "/parts/" + url.PathEscape(id)
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPut, u, counted)
	if err != nil {
		return &SendError{Kind: ErrNotConfigured, Err: err}
	}
	httpReq.ContentLength = size
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	httpReq.Header.Set("X-Part-Name", url.PathEscape(name))
	_, sendErr := s.do(httpReq)
	return sendErr
}

const (
	responseBodyTimeout = 30 * time.Second
	uploadStallTimeout  = 60 * time.Second
)

func readResponseBody(ctx context.Context, body io.ReadCloser, limit int64, timeout time.Duration) ([]byte, error) {
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		_ = body.Close()
	})
	defer timer.Stop()
	data, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil {
		if timedOut.Load() && ctx.Err() == nil {
			return nil, context.DeadlineExceeded
		}
		return nil, err
	}
	if timedOut.Load() && ctx.Err() == nil {
		return nil, context.DeadlineExceeded
	}
	return data, nil
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return min(time.Duration(secs)*time.Second, 10*time.Minute)
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return min(d, 10*time.Minute)
		}
	}
	return 0
}

// do runs a request and maps the outcome to a SendError.
func (s *Sender) do(req *http.Request) ([]byte, *SendError) {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &SendError{Kind: ErrOffline, Err: err}
	}
	defer resp.Body.Close()
	body, err := readResponseBody(req.Context(), resp.Body, 64*1024, responseBodyTimeout)
	if err != nil {
		return nil, &SendError{Kind: ErrOffline, Err: err}
	}
	var errPayload struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &errPayload)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return body, nil
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return nil, &SendError{Kind: ErrTooLarge, Status: resp.StatusCode, Code: errPayload.Code}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &SendError{
			Kind:       ErrRateLimited,
			Status:     resp.StatusCode,
			Code:       errPayload.Code,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		return nil, &SendError{Kind: ErrServer, Status: resp.StatusCode, Code: errPayload.Code}
	default:
		return nil, &SendError{Kind: ErrRejected, Status: resp.StatusCode, Code: errPayload.Code}
	}
}

type countingReader struct {
	r      io.Reader
	onRead func(int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.onRead(n)
	}
	return n, err
}
