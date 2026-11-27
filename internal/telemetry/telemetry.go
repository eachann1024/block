// Package telemetry sends one anonymous "this install is alive" ping per day.
//
// Payload: a random install id (generated locally, not derived from hardware),
// app version, OS, OS major version, CPU architecture and UI language. Nothing
// else — no paths, note content, account names or hardware identifiers.
//
// Users can turn it off in Settings -> About & Privacy; DO_NOT_TRACK=1 or BLOCK_NO_TELEMETRY=1
// also disable it. Development builds never send.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Endpoint is the default receiver of the daily ping (cooool-block worker).
const Endpoint = "https://block.cooool.top/api/ping"

const (
	startDelay      = 5 * time.Second // short enough that quick open-and-close sessions still count
	checkInterval   = 3 * time.Hour
	retryInterval   = 15 * time.Minute // after a failed ping, so a short session later today still counts
	limitedRetry    = time.Minute      // after a 429: the server's window is 60 s, plus up to limitedJitter
	limitedJitter   = 2 * time.Minute  // spreads out many clients behind one address that were limited together
	wakeStep        = 5 * time.Minute  // longest single sleep, so a laptop waking from sleep notices a passed deadline
	behindRetry     = time.Minute      // first recheck after the server counted an earlier day; doubles up to retryInterval
	fileName        = "telemetry.json"
	runningFileName = "telemetry.running"
)

var (
	idRe       = regexp.MustCompile(`^[0-9a-f]{32}$`)
	dayRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
)

// errLimited reports a 429: the source address sent too many pings in the last minute.
var errLimited = errors.New("telemetry: rate limited")

// Config wires the sender to the app. Enabled and Lang are read before every check,
// so turning the setting off takes effect without a restart. All extra metric
// callbacks are optional; nil callbacks omit the corresponding fields.
type Config struct {
	DataDir      string
	Version      string
	Enabled      func() bool
	Lang         func() string
	Theme        func() string
	UpdateSource func() string
	NoteCount    func() int
	AIBackend    func() string
	Plugins      func() []string
	Startup      func() time.Duration
	DirtyExit    func() bool
	Endpoint     string       // empty means Endpoint
	Client       *http.Client // nil means a client with a short timeout
}

type record struct {
	ID       string `json:"id"`
	LastPing string `json:"last_ping,omitempty"` // UTC+8 day, matches the server's day boundary
}

type payload struct {
	ID        string   `json:"id"`
	V         string   `json:"v"`
	OS        string   `json:"os"`
	OSV       string   `json:"osv"`
	Arch      string   `json:"arch"`
	Lang      string   `json:"lang"`
	Theme     string   `json:"theme,omitempty"`
	UpdateSrc string   `json:"update_src,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	AI        string   `json:"ai,omitempty"`
	Plugins   []string `json:"plugins,omitempty"`
	Startup   string   `json:"startup,omitempty"`
	DirtyExit bool     `json:"dirty_exit,omitempty"`
}

var startOnce sync.Once

// EnvDisabled reports whether telemetry is disabled via DO_NOT_TRACK or BLOCK_NO_TELEMETRY.
func EnvDisabled() bool {
	return envDisabled()
}

// BeginSession records an active-session marker in dataDir and reports whether
// the previous session's marker was still present (indicating an unclean exit).
func BeginSession(dataDir string) bool {
	if dataDir == "" {
		return false
	}
	marker := filepath.Join(dataDir, runningFileName)
	_, err := os.Lstat(marker)
	dirty := err == nil
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.WriteFile(marker, []byte("1\n"), 0o600)
	return dirty
}

// MarkCleanExit removes the active-session marker from dataDir on normal shutdown.
func MarkCleanExit(dataDir string) {
	if dataDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(dataDir, runningFileName))
}

// Start launches the background sender once per process.
func Start(cfg Config) {
	if cfg.DataDir == "" || cfg.Enabled == nil || envDisabled() {
		return
	}
	startOnce.Do(func() {
		go loop(cfg)
	})
}

func envDisabled() bool {
	for _, k := range []string{"DO_NOT_TRACK", "BLOCK_NO_TELEMETRY"} {
		v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
		if v != "" && v != "0" && v != "false" {
			return true
		}
	}
	return false
}

func loop(cfg Config) {
	time.Sleep(startDelay)
	behind := behindRetry
	for {
		wait := checkInterval
		day := today()
		if counted, err := check(context.Background(), cfg); errors.Is(err, errLimited) {
			wait = limitedRetry + mrand.N(limitedJitter)
		} else if err != nil {
			wait = retryInterval
		} else if counted != "" && counted < day {
			// The clock runs ahead of the server's around midnight, so the ping counted for
			// yesterday and today is still due: look again soon, backing off if the gap persists.
			wait = behind
			behind = min(behind*2, retryInterval)
		} else {
			behind = behindRetry
		}
		now := time.Now()
		// Check again right after midnight so a session spanning it counts for the new day too.
		wait = min(wait, untilNextDay(now))
		if today() != day {
			// Midnight passed while that check ran, so it covered yesterday: today is still due.
			wait = min(wait, time.Minute)
		}
		sleepUntil(now.Add(wait).Round(0))
	}
}

// untilNextDay is the time from now until one minute past the next Beijing midnight.
func untilNextDay(now time.Time) time.Duration {
	bj := now.UTC().Add(8 * time.Hour)
	return bj.Truncate(24 * time.Hour).Add(24*time.Hour + time.Minute).Sub(bj)
}

// sleepUntil waits for a wall-clock deadline in short steps. The monotonic clock behind
// time.Sleep can stop while the machine sleeps, which would stretch one long sleep far
// past the deadline; deadline carries no monotonic reading, so time.Until uses the wall clock.
func sleepUntil(deadline time.Time) {
	for {
		d := time.Until(deadline)
		if d <= 0 {
			return
		}
		time.Sleep(min(d, wakeStep))
	}
}

func today() string {
	return time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
}

// CheckOnce sends today's ping if it is due. sent reports whether the server
// accepted a request. Only a 2xx naming the counted day or a rejection of the payload itself (400, 413,
// 422, which a retry cannot fix) marks today as done; anything else, such as a
// 5xx, 429, an edge 403 or a network error, leaves today unmarked for a later check.
func CheckOnce(ctx context.Context, cfg Config) (sent bool, err error) {
	counted, err := check(ctx, cfg)
	return counted != "", err
}

// check is CheckOnce returning the day the server counted the ping for ("" when nothing was
// counted). It is earlier than today() when the local clock runs ahead of the server's.
func check(ctx context.Context, cfg Config) (counted string, err error) {
	if cfg.DataDir == "" || cfg.Enabled == nil || envDisabled() || !cfg.Enabled() {
		return "", nil
	}
	path := filepath.Join(cfg.DataDir, fileName)
	rec, err := load(path)
	if err != nil {
		// An id that cannot be kept would be counted as a new install on every check.
		return "", err
	}
	day := today()
	if rec.LastPing == day {
		return "", nil
	}
	lang := "en"
	if cfg.Lang != nil && cfg.Lang() == "zh" {
		lang = "zh"
	}
	p := payload{
		ID:   rec.ID,
		V:    cfg.Version,
		OS:   runtime.GOOS,
		OSV:  osMajor(),
		Arch: runtime.GOARCH,
		Lang: lang,
	}
	if cfg.Theme != nil {
		p.Theme = normalizeTheme(cfg.Theme())
	}
	if cfg.UpdateSource != nil {
		p.UpdateSrc = normalizeUpdateSource(cfg.UpdateSource())
	}
	if cfg.NoteCount != nil {
		p.Notes = bucketNotes(cfg.NoteCount())
	}
	if cfg.AIBackend != nil {
		p.AI = normalizeAI(cfg.AIBackend())
	}
	if cfg.Plugins != nil {
		p.Plugins = sanitizePlugins(cfg.Plugins())
	}
	if cfg.Startup != nil {
		p.Startup = bucketStartup(cfg.Startup())
	}
	if cfg.DirtyExit != nil && cfg.DirtyExit() {
		p.DirtyExit = true
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	// Never follow redirects: a misrouted endpoint must not get the install id or count
	// as delivered. The 3xx is returned as is and retried like any other failure.
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &noFollow
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Block")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var reply struct {
		Day string `json:"day"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 512)).Decode(&reply)
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300,
		resp.StatusCode == http.StatusBadRequest,
		resp.StatusCode == http.StatusRequestEntityTooLarge,
		resp.StatusCode == http.StatusUnprocessableEntity:
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", errLimited
	default:
		return "", fmt.Errorf("telemetry: server %d", resp.StatusCode)
	}
	if resp.StatusCode < 300 {
		// Record the day the server counted: with a clock running ahead around midnight the
		// ping lands on yesterday, and marking today would skip today altogether. A success
		// without a readable day (a proxy or captive portal answering 200) proves nothing,
		// so it is retried like a failure instead of marking today as done.
		if _, perr := time.Parse("2006-01-02", reply.Day); perr != nil || !dayRe.MatchString(reply.Day) {
			return "", fmt.Errorf("telemetry: server %d without a day", resp.StatusCode)
		}
		day = reply.Day
	}
	rec.LastPing = day
	_ = save(path, rec)
	if resp.StatusCode >= 300 {
		return "", nil // the payload was rejected: retrying cannot fix it
	}
	return day, nil
}

func load(path string) (record, error) {
	for attempt := 0; ; attempt++ {
		var rec record
		data, err := os.ReadFile(path)
		if err == nil {
			_ = json.Unmarshal(data, &rec)
		}
		if idRe.MatchString(rec.ID) {
			return rec, nil
		}
		last := attempt >= 2
		if err == nil && !last {
			// Possibly another process still writing it (see create's fallback): look again.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err == nil && freshlyWritten(path) {
			// Still unfinished after the retries, but only just created: its writer may be paused
			// and will keep using the id it is writing. Replacing it now would count this install
			// twice, so leave it for a later check.
			return rec, errors.New("telemetry: install id file is still being written")
		}
		rec = record{ID: newID()}
		if errors.Is(err, fs.ErrNotExist) && !last {
			// First run: create the file only if nobody else did, so two processes sharing a
			// fresh data directory end up with one id instead of being counted as two installs.
			err = create(path, rec)
			if errors.Is(err, fs.ErrExist) {
				continue
			}
		} else {
			// Damaged, unreadable, or a dangling symlink that reads as missing yet blocks
			// creation: replace it instead of retrying forever.
			err = save(path, rec)
		}
		if err != nil {
			return rec, fmt.Errorf("telemetry: cannot keep install id: %w", err)
		}
		return rec, nil
	}
}

// freshlyWritten reports whether path was modified in the last ten seconds.
func freshlyWritten(path string) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()).Abs() < 10*time.Second
}

// writeTemp writes rec to a new temporary file next to path and returns its name.
func writeTemp(path string, rec record) (string, error) {
	data, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// create publishes rec at path only if path does not exist yet (fs.ErrExist otherwise).
// A hard link is atomic and never replaces, so a reader sees either nothing or the full file.
func create(path string, rec record) error {
	tmp, err := writeTemp(path, rec)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	err = os.Link(tmp, path)
	if err == nil {
		return nil
	}
	if _, serr := os.Lstat(path); serr == nil {
		return fs.ErrExist
	}
	// No hard links on this filesystem (exFAT, some network shares): an exclusive create
	// still lets only one process win; a reader catching it half-written looks again.
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err // fs.ErrExist when another process won
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func save(path string, rec record) error {
	tmp, err := writeTemp(path, rec)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// majorLabel builds "<family>-<major>" and falls back to "unknown" when major is not a small number.
func majorLabel(family, major string) string {
	if len(major) == 0 || len(major) > 3 {
		return "unknown"
	}
	for _, r := range major {
		if r < '0' || r > '9' {
			return "unknown"
		}
	}
	return family + "-" + major
}

func normalizeTheme(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "system", "light", "dark":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "unknown"
	}
}

func normalizeUpdateSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto", "mirror", "github":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "unknown"
	}
}

func bucketNotes(count int) string {
	switch {
	case count < 0:
		return "unknown"
	case count == 0:
		return "0"
	case count <= 10:
		return "1-10"
	case count <= 50:
		return "11-50"
	case count <= 200:
		return "51-200"
	default:
		return "200+"
	}
}

func normalizeAI(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none", "deepseek", "openai", "claude", "claude-code", "codex", "antigravity", "pi", "dsh", "other":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "unknown"
	}
}

func sanitizePlugins(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, min(len(raw), 20))
	for _, id := range raw {
		id = strings.ToLower(strings.TrimSpace(id))
		if !pluginIDRe.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	slices.Sort(out)
	if len(out) > 20 {
		out = out[:20]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func bucketStartup(d time.Duration) string {
	switch {
	case d < 0:
		return "unknown"
	case d < 300*time.Millisecond:
		return "<300ms"
	case d < 800*time.Millisecond:
		return "300-800ms"
	case d < 2000*time.Millisecond:
		return "800-2000ms"
	default:
		return ">2s"
	}
}
