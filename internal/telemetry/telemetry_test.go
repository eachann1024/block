package telemetry_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"block/internal/telemetry"
)

var hex32Re = regexp.MustCompile(`^[0-9a-f]{32}$`)

type capturedPayload struct {
	ID   string `json:"id"`
	V    string `json:"v"`
	OS   string `json:"os"`
	OSV  string `json:"osv"`
	Arch string `json:"arch"`
	Lang string `json:"lang"`
}

type storedRecord struct {
	ID       string `json:"id"`
	LastPing string `json:"last_ping"`
}

func clearOptOutEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("BLOCK_NO_TELEMETRY", "")
}

func utc8Today() string {
	return time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
}

func readStoredRecord(t *testing.T, dir string) storedRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "telemetry.json"))
	if err != nil {
		t.Fatalf("read telemetry.json: %v", err)
	}
	var rec storedRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal telemetry.json: %v", err)
	}
	return rec
}

func TestCheckOnce_Sends204AndSkipsSameDay(t *testing.T) {
	clearOptOutEnv(t)

	var hits atomic.Int32
	var got capturedPayload
	var gotContentType, gotUserAgent, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotUserAgent = r.Header.Get("User-Agent")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal request payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := telemetry.Config{
		DataDir:  dir,
		Version:  "1.2.3",
		Enabled:  func() bool { return true },
		Lang:     func() string { return "zh" },
		Endpoint: srv.URL,
		Client:   srv.Client(),
	}

	sent, err := telemetry.CheckOnce(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first CheckOnce returned unexpected error: %v", err)
	}
	if !sent {
		t.Fatalf("first CheckOnce sent = false, want true")
	}
	if hits.Load() != 1 {
		t.Fatalf("server hits = %d, want 1", hits.Load())
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotUserAgent != "Block" {
		t.Errorf("User-Agent = %q, want Block", gotUserAgent)
	}
	if !hex32Re.MatchString(got.ID) {
		t.Errorf("payload id = %q, want 32-char lowercase hex", got.ID)
	}
	if got.V != "1.2.3" {
		t.Errorf("payload v = %q, want 1.2.3", got.V)
	}
	if got.OS != runtime.GOOS {
		t.Errorf("payload os = %q, want %q", got.OS, runtime.GOOS)
	}
	if got.OSV == "" {
		t.Errorf("payload osv is empty, want non-empty")
	}
	if got.Arch != runtime.GOARCH {
		t.Errorf("payload arch = %q, want %q", got.Arch, runtime.GOARCH)
	}
	if got.Lang != "zh" {
		t.Errorf("payload lang = %q, want zh", got.Lang)
	}

	rec := readStoredRecord(t, dir)
	if rec.ID != got.ID {
		t.Errorf("stored id = %q, want %q", rec.ID, got.ID)
	}
	if rec.LastPing != utc8Today() {
		t.Errorf("stored last_ping = %q, want %q", rec.LastPing, utc8Today())
	}

	// Second call on the same day must not send.
	sent, err = telemetry.CheckOnce(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second CheckOnce returned unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("second CheckOnce sent = true, want false")
	}
	if hits.Load() != 1 {
		t.Fatalf("server hits after second call = %d, want 1", hits.Load())
	}
}

func TestCheckOnce_DisabledByConfig(t *testing.T) {
	clearOptOutEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := telemetry.Config{
		DataDir:  t.TempDir(),
		Version:  "1.0.0",
		Enabled:  func() bool { return false },
		Endpoint: srv.URL,
		Client:   srv.Client(),
	}

	sent, err := telemetry.CheckOnce(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CheckOnce returned unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("CheckOnce sent = true, want false")
	}
	if hits.Load() != 0 {
		t.Fatalf("server hits = %d, want 0", hits.Load())
	}
}

func TestCheckOnce_DisabledByEnv(t *testing.T) {
	for _, envName := range []string{"DO_NOT_TRACK", "BLOCK_NO_TELEMETRY"} {
		t.Run(envName, func(t *testing.T) {
			clearOptOutEnv(t)
			t.Setenv(envName, "1")

			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			cfg := telemetry.Config{
				DataDir:  t.TempDir(),
				Version:  "1.0.0",
				Enabled:  func() bool { return true },
				Endpoint: srv.URL,
				Client:   srv.Client(),
			}

			sent, err := telemetry.CheckOnce(context.Background(), cfg)
			if err != nil {
				t.Fatalf("CheckOnce returned unexpected error: %v", err)
			}
			if sent {
				t.Fatalf("CheckOnce sent = true, want false")
			}
			if hits.Load() != 0 {
				t.Fatalf("server hits = %d, want 0", hits.Load())
			}
		})
	}
}

func TestCheckOnce_RetriesAfterTransientStatusAndKeepsID(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			clearOptOutEnv(t)

			var hits atomic.Int32
			var ids []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := hits.Add(1)
				body, _ := io.ReadAll(r.Body)
				var p capturedPayload
				if err := json.Unmarshal(body, &p); err == nil {
					ids = append(ids, p.ID)
				}
				if n == 1 {
					w.WriteHeader(status)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			dir := t.TempDir()
			cfg := telemetry.Config{
				DataDir:  dir,
				Version:  "1.0.0",
				Enabled:  func() bool { return true },
				Endpoint: srv.URL,
				Client:   srv.Client(),
			}

			sent, err := telemetry.CheckOnce(context.Background(), cfg)
			if err == nil {
				t.Fatalf("first CheckOnce err = nil, want non-nil on %d", status)
			}
			if sent {
				t.Fatalf("first CheckOnce sent = true, want false on %d", status)
			}
			rec := readStoredRecord(t, dir)
			if rec.LastPing != "" {
				t.Fatalf("stored last_ping after %d = %q, want empty", status, rec.LastPing)
			}

			sent, err = telemetry.CheckOnce(context.Background(), cfg)
			if err != nil {
				t.Fatalf("retry CheckOnce returned unexpected error: %v", err)
			}
			if !sent {
				t.Fatalf("retry CheckOnce sent = false, want true")
			}
			if hits.Load() != 2 {
				t.Fatalf("server hits = %d, want 2", hits.Load())
			}
			if len(ids) != 2 || !hex32Re.MatchString(ids[0]) || ids[0] != ids[1] {
				t.Fatalf("ids across retry = %v, want two identical 32-char hex ids", ids)
			}
		})
	}
}

func TestCheckOnce_MarksDayOn400AndDoesNotRetrySameDay(t *testing.T) {
	clearOptOutEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := telemetry.Config{
		DataDir:  dir,
		Version:  "1.0.0",
		Enabled:  func() bool { return true },
		Endpoint: srv.URL,
		Client:   srv.Client(),
	}

	sent, err := telemetry.CheckOnce(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first CheckOnce on 400 returned unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("first CheckOnce on 400 sent = true, want false")
	}
	rec := readStoredRecord(t, dir)
	if rec.LastPing != utc8Today() {
		t.Fatalf("stored last_ping after 400 = %q, want %q", rec.LastPing, utc8Today())
	}

	sent, err = telemetry.CheckOnce(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second CheckOnce after 400 returned unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("second CheckOnce after 400 sent = true, want false")
	}
	if hits.Load() != 1 {
		t.Fatalf("server hits = %d, want 1", hits.Load())
	}
}

func TestCheckOnce_IDPersistsAcrossDays(t *testing.T) {
	clearOptOutEnv(t)

	var ids []string
	var langs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p capturedPayload
		if err := json.Unmarshal(body, &p); err == nil {
			ids = append(ids, p.ID)
			langs = append(langs, p.Lang)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := telemetry.Config{
		DataDir:  dir,
		Version:  "1.0.0",
		Enabled:  func() bool { return true },
		Endpoint: srv.URL,
		Client:   srv.Client(),
	}

	sent, err := telemetry.CheckOnce(context.Background(), cfg)
	if err != nil || !sent {
		t.Fatalf("day 1 CheckOnce = (%v, %v), want (true, nil)", sent, err)
	}

	// Simulate an earlier day stored on disk.
	rec := readStoredRecord(t, dir)
	rec.LastPing = "2000-01-01"
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "telemetry.json"), raw, 0o600); err != nil {
		t.Fatalf("rewrite telemetry.json: %v", err)
	}

	sent, err = telemetry.CheckOnce(context.Background(), cfg)
	if err != nil || !sent {
		t.Fatalf("day 2 CheckOnce = (%v, %v), want (true, nil)", sent, err)
	}

	if len(ids) != 2 || !hex32Re.MatchString(ids[0]) || ids[0] != ids[1] {
		t.Fatalf("ids across days = %v, want identical 32-char hex ids", ids)
	}
	if len(langs) != 2 || langs[0] != "en" || langs[1] != "en" {
		t.Fatalf("default langs = %v, want [en en]", langs)
	}
}
