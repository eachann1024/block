// Package telemetry sends one anonymous "this install is alive" ping per day.
//
// Payload: a random install id (generated locally, not derived from hardware),
// app version, OS, OS major version, CPU architecture and UI language. Nothing
// else — no paths, note content, account names or hardware identifiers.
//
// Users can turn it off in Settings; DO_NOT_TRACK=1 or BLOCK_NO_TELEMETRY=1
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
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Endpoint is the default receiver of the daily ping (cooool-block worker).
const Endpoint = "https://block.cooool.top/api/ping"

const (
	startDelay    = 5 * time.Second // short enough that quick open-and-close sessions still count
	checkInterval = 3 * time.Hour
	retryInterval = 15 * time.Minute // after a failed ping, so a short session later today still counts
	wakeStep      = 5 * time.Minute  // longest single sleep, so a laptop waking from sleep notices a passed deadline
	fileName      = "telemetry.json"
)

var idRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Config wires the sender to the app. Enabled and Lang are read before every check,
// so turning the setting off takes effect without a restart.
type Config struct {
	DataDir  string
	Version  string
	Enabled  func() bool
	Lang     func() string
	Endpoint string       // empty means Endpoint
	Client   *http.Client // nil means a client with a short timeout
}

type record struct {
	ID       string `json:"id"`
	LastPing string `json:"last_ping,omitempty"` // UTC+8 day, matches the server's day boundary
}

type payload struct {
	ID   string `json:"id"`
	V    string `json:"v"`
	OS   string `json:"os"`
	OSV  string `json:"osv"`
	Arch string `json:"arch"`
	Lang string `json:"lang"`
}

var startOnce sync.Once

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
	for {
		wait := checkInterval
		if _, err := CheckOnce(context.Background(), cfg); err != nil {
			wait = retryInterval
		}
		now := time.Now()
		// Check again right after midnight so a session spanning it counts for the new day too.
		wait = min(wait, untilNextDay(now))
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
// accepted a request. Only a 2xx or a rejection of the payload itself (400, 413,
// 422, which a retry cannot fix) marks today as done; anything else, such as a
// 5xx, 429, an edge 403 or a network error, leaves today unmarked for a later check.
func CheckOnce(ctx context.Context, cfg Config) (sent bool, err error) {
	if cfg.DataDir == "" || cfg.Enabled == nil || envDisabled() || !cfg.Enabled() {
		return false, nil
	}
	path := filepath.Join(cfg.DataDir, fileName)
	rec, err := load(path)
	if err != nil {
		// An id that cannot be kept would be counted as a new install on every check.
		return false, err
	}
	day := today()
	if rec.LastPing == day {
		return false, nil
	}
	lang := "en"
	if cfg.Lang != nil && cfg.Lang() == "zh" {
		lang = "zh"
	}
	body, err := json.Marshal(payload{ID: rec.ID, V: cfg.Version, OS: runtime.GOOS, OSV: osMajor(), Arch: runtime.GOARCH, Lang: lang})
	if err != nil {
		return false, err
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
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Block")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300,
		resp.StatusCode == http.StatusBadRequest,
		resp.StatusCode == http.StatusRequestEntityTooLarge,
		resp.StatusCode == http.StatusUnprocessableEntity:
	default:
		return false, fmt.Errorf("telemetry: server %d", resp.StatusCode)
	}
	rec.LastPing = day
	_ = save(path, rec)
	return resp.StatusCode < 300, nil
}

func load(path string) (record, error) {
	var rec record
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &rec)
	}
	if idRe.MatchString(rec.ID) {
		return rec, nil
	}
	rec = record{ID: newID()}
	if errors.Is(err, fs.ErrNotExist) {
		// First run: create the file only if nobody else did, so two processes sharing a
		// fresh data directory end up with one id instead of being counted as two installs.
		err = create(path, rec)
		if errors.Is(err, fs.ErrExist) {
			return load(path)
		}
	} else {
		err = save(path, rec) // unreadable or damaged: replace it
	}
	if err != nil {
		return rec, fmt.Errorf("telemetry: cannot keep install id: %w", err)
	}
	return rec, nil
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
	if err := os.Link(tmp, path); err != nil {
		if _, serr := os.Lstat(path); serr == nil {
			return fs.ErrExist
		}
		return err
	}
	return nil
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
