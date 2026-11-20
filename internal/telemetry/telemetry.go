// Package telemetry sends one anonymous "this install is alive" ping per day.
//
// Payload: a random install id (generated locally, not derived from hardware),
// app version, OS, CPU architecture and UI language. Nothing else — no paths,
// note content, account names or hardware identifiers.
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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Endpoint receives the daily ping. See public/deploy/cloudflare/telemetry.js.
const Endpoint = "https://block.cooool.top/api/ping"

const (
	startDelay    = 45 * time.Second
	checkInterval = 3 * time.Hour
	fileName      = "telemetry.json"
)

var idRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Config wires the sender to the app. Enabled and Lang are read before every ping.
type Config struct {
	DataDir string
	Version string
	Enabled func() bool
	Lang    func() string
}

type record struct {
	ID       string `json:"id"`
	LastPing string `json:"last_ping,omitempty"` // UTC+8 day, matches the server's day boundary
}

type payload struct {
	ID   string `json:"id"`
	V    string `json:"v"`
	OS   string `json:"os"`
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
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		tick(cfg, client)
		time.Sleep(checkInterval)
	}
}

func today() string {
	return time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
}

func tick(cfg Config, client *http.Client) {
	if !cfg.Enabled() {
		return
	}
	path := filepath.Join(cfg.DataDir, fileName)
	rec := load(path)
	day := today()
	if rec.LastPing == day {
		return
	}
	lang := "en"
	if cfg.Lang != nil && cfg.Lang() == "zh" {
		lang = "zh"
	}
	body, _ := json.Marshal(payload{ID: rec.ID, V: cfg.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Lang: lang})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Block")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	// 4xx means the server rejected this payload; retrying today would not help.
	if resp.StatusCode >= 500 {
		return
	}
	rec.LastPing = day
	save(path, rec)
}

func load(path string) record {
	var rec record
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &rec)
	}
	if !idRe.MatchString(rec.ID) {
		rec = record{ID: newID()}
		save(path, rec)
	}
	return rec
}

func save(path string, rec record) {
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
