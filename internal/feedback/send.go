package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
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

// DefaultEndpoint is the base URL of the feedback service. Empty until the
// service is live: sending then fails with ErrNotConfigured instead of
// pretending the report went anywhere.
const DefaultEndpoint = ""

// LogPartID is the part id of the diagnostic log.
const LogPartID = "log"

// Endpoint resolves the service base URL: BLOCK_FEEDBACK_URL, else DefaultEndpoint.
func Endpoint() string {
	if env := strings.TrimSpace(os.Getenv("BLOCK_FEEDBACK_URL")); env != "" {
		return strings.TrimRight(env, "/")
	}
	return strings.TrimRight(DefaultEndpoint, "/")
}

// ErrKind groups failures by what the user can do about them.
type ErrKind int

const (
	ErrNone          ErrKind = iota
	ErrNotConfigured         // no service address: nothing was sent
	ErrOffline               // could not reach the service: retry later
	ErrServer                // the service failed (5xx, 429): retry later
	ErrTooLarge              // the service refused the size (413)
	ErrRejected              // the service refused the report (other 4xx)
	ErrLocalFile             // an attachment could not be read any more
)

// SendError is a failed step with its kind and, for HTTP failures, status.
type SendError struct {
	Kind   ErrKind
	Status int
	Err    error
}

func (e *SendError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("feedback: http %d", e.Status)
}

func (e *SendError) Unwrap() error { return e.Err }

// retryable reports whether an automatic retry can help.
func (e *SendError) retryable() bool { return e.Kind == ErrOffline || e.Kind == ErrServer }

// Report is everything one feedback submission carries.
type Report struct {
	Category    string
	Message     string
	Contact     string
	AppVersion  string
	Lang        string
	Attachments []Attachment
	Log         string // masked diagnostic log; empty = not attached
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
	Phase   Phase
	Parts   []Part
	Sent    int64
	Total   int64
	Attempt int // try of the step in flight, across create / part / complete
	Err     *SendError
}

// Percent is the overall upload progress, 0–100.
func (s Snapshot) Percent() float32 {
	if s.Phase == PhaseDone {
		return 100
	}
	if s.Total <= 0 {
		return 0
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
	Category string        `json:"category"`
	Message  string        `json:"message"`
	Contact  string        `json:"contact,omitempty"`
	App      reportApp     `json:"app"`
	Parts    []reportPartV `json:"parts"`
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
	ID    string `json:"id"`
	Token string `json:"token"`
}

// Sender uploads one report in the background. It is safe to read from the
// UI thread through Snapshot; onChange is called (from the worker) whenever
// the snapshot changes.
type Sender struct {
	report   Report
	endpoint string
	client   *http.Client
	onChange func()

	mu       sync.Mutex
	snap     Snapshot
	paths    map[string]string // part id -> file to upload
	reportID string
	token    string
	cancel   context.CancelFunc
	running  bool
	lastTick time.Time
}

// NewSender prepares a submission; nothing is sent until Start.
func NewSender(r Report, onChange func()) *Sender {
	s := &Sender{
		report:   r,
		endpoint: Endpoint(),
		onChange: onChange,
		paths:    map[string]string{},
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

func (s *Sender) run(ctx context.Context) {
	if s.endpoint == "" {
		s.finish(PhaseFailed, &SendError{Kind: ErrNotConfigured, Err: errors.New("feedback service is not configured")})
		return
	}
	if s.reportID == "" {
		if err := s.withRetry(ctx, nil, s.createReport); err != nil {
			s.finish(PhaseFailed, err)
			return
		}
	}
	var firstErr *SendError
	for i := range s.report.Attachments {
		a := s.report.Attachments[i]
		if err := s.uploadPart(ctx, a.ID); err != nil && firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			return
		}
	}
	if s.report.Log != "" {
		if err := s.uploadPart(ctx, LogPartID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if ctx.Err() != nil {
		return
	}
	if firstErr != nil {
		s.finish(PhaseFailed, firstErr)
		return
	}
	if err := s.withRetry(ctx, nil, s.completeReport); err != nil {
		s.finish(PhaseFailed, err)
		return
	}
	s.finish(PhaseDone, nil)
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
		select {
		case <-ctx.Done():
			return last
		case <-time.After(retryBackoff[min(attempt-1, len(retryBackoff)-1)]):
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
		Category: s.report.Category,
		Message:  s.report.Message,
		Contact:  s.report.Contact,
		App:      reportApp{Version: s.report.AppVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, Lang: s.report.Lang},
		Parts:    []reportPartV{},
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
	respBody, sendErr := s.do(httpReq)
	if sendErr != nil {
		return sendErr
	}
	var out reportResponse
	if err := json.Unmarshal(respBody, &out); err != nil || strings.TrimSpace(out.ID) == "" {
		return &SendError{Kind: ErrServer, Err: errors.New("feedback service returned no report id")}
	}
	s.mu.Lock()
	s.reportID, s.token = out.ID, out.Token
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
			p.State, p.Err = PartFailed, err
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
		f, err := os.Open(s.paths[id])
		if err != nil {
			return &SendError{Kind: ErrLocalFile, Err: err}
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return &SendError{Kind: ErrLocalFile, Err: err}
		}
		body, size = f, info.Size()
		s.setPart(id, func(p *Part) { name = p.Name })
	}

	counted := &countingReader{r: body, onRead: func(n int) {
		s.setPart(id, func(p *Part) {
			p.Sent += int64(n)
			s.snap.Sent += int64(n)
		})
		s.notify(false)
	}}
	u := s.endpoint + "/reports/" + url.PathEscape(s.reportID) + "/parts/" + url.PathEscape(id)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, u, counted)
	if err != nil {
		return &SendError{Kind: ErrNotConfigured, Err: err}
	}
	httpReq.ContentLength = size
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	httpReq.Header.Set("X-Part-Name", url.PathEscape(name))
	_, sendErr := s.do(httpReq)
	return sendErr
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return body, nil
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return nil, &SendError{Kind: ErrTooLarge, Status: resp.StatusCode}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout:
		return nil, &SendError{Kind: ErrServer, Status: resp.StatusCode}
	default:
		return nil, &SendError{Kind: ErrRejected, Status: resp.StatusCode}
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
