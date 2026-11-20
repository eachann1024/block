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
	"fmt"
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
		_, _ = CheckOnce(context.Background(), cfg)
		time.Sleep(checkInterval)
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
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &rec)
	}
	if !idRe.MatchString(rec.ID) {
		rec = record{ID: newID()}
		if err := save(path, rec); err != nil {
			return rec, fmt.Errorf("telemetry: cannot keep install id: %w", err)
		}
	}
	return rec, nil
}

func save(path string, rec record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
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
