package wasmrt

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedQJSWasmSHA256(t *testing.T) {
	raw, err := LoadQJSWasm()
	if err != nil {
		t.Fatalf("LoadQJSWasm: %v", err)
	}
	if len(raw) < 1024*1024 {
		t.Fatalf("unexpected qjs-wasi.wasm size: %d", len(raw))
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != ExpectedQJSSHA256 {
		t.Fatalf("SHA-256 mismatch: got %s want %s", got, ExpectedQJSSHA256)
	}
}

func TestMemPipeZeroByteWriteAndCloseWakeup(t *testing.T) {
	r, w := newMemPipe(8)

	// 0-byte Write must return (0, nil) immediately even before any reader runs.
	n, err := w.Write(nil)
	if n != 0 || err != nil {
		t.Fatalf("Write(nil) = (%d, %v), want (0, nil)", n, err)
	}
	n, err = w.Write([]byte{})
	if n != 0 || err != nil {
		t.Fatalf("Write([]) = (%d, %v), want (0, nil)", n, err)
	}

	// Fill pipe completely; 0-byte Write must still succeed immediately without deadlocking.
	if n, err := w.Write([]byte("12345678")); n != 8 || err != nil {
		t.Fatalf("Write full = (%d, %v), want (8, nil)", n, err)
	}
	if n, err := w.Write([]byte{}); n != 0 || err != nil {
		t.Fatalf("Write([]) on full pipe = (%d, %v), want (0, nil)", n, err)
	}

	// Blocked Write must wake up immediately on reader CloseWithError.
	writeDone := make(chan error, 1)
	go func() {
		_, wErr := w.Write([]byte("more"))
		writeDone <- wErr
	}()
	time.Sleep(15 * time.Millisecond)
	sentinel := errors.New("reader aborted")
	_ = r.CloseWithError(sentinel)
	select {
	case wErr := <-writeDone:
		if !errors.Is(wErr, sentinel) {
			t.Fatalf("blocked Write error = %v, want %v", wErr, sentinel)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Write did not wake up after reader CloseWithError")
	}

	// Blocked Read must wake up immediately on writer CloseWithError.
	r2, w2 := newMemPipe(8)
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, rErr := r2.Read(buf)
		readDone <- rErr
	}()
	time.Sleep(15 * time.Millisecond)
	_ = w2.Close()
	select {
	case rErr := <-readDone:
		if !errors.Is(rErr, io.EOF) {
			t.Fatalf("blocked Read error = %v, want io.EOF", rErr)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Read did not wake up after writer Close")
	}

	// SetWriteDeadline must unblock full Write.
	r3, w3 := newMemPipe(4)
	defer r3.Close()
	defer w3.Close()
	if _, err := w3.Write([]byte("abcd")); err != nil {
		t.Fatal(err)
	}
	_ = w3.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := w3.Write([]byte("e")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Write past deadline = %v, want os.ErrDeadlineExceeded", err)
	}
}

func TestEngineWarmupAndFallbackCache(t *testing.T) {
	// Valid disk cache dir.
	cacheDir := filepath.Join(t.TempDir(), "wasm-cache")
	eng, err := NewEngine(cacheDir)
	if err != nil {
		t.Fatalf("NewEngine(cacheDir): %v", err)
	}
	defer eng.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := eng.Warmup(ctx); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected disk compilation cache entries in %s, got len=%d err=%v", cacheDir, len(entries), err)
	}

	// Invalid cache dir (existing regular file) must fall back cleanly to memory cache.
	badPath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	engFallback, err := NewEngine(badPath)
	if err != nil {
		t.Fatalf("NewEngine(fallback): %v", err)
	}
	defer engFallback.Close(context.Background())
	if err := engFallback.Warmup(ctx); err != nil {
		t.Fatalf("Warmup(fallback): %v", err)
	}
}

func TestJSModuleImportGlobalAndOutOfOrderQueue(t *testing.T) {
	t.Setenv("PATH", "")

	eng, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(context.Background())
	if err := eng.Warmup(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 1. ES module import + relative helper import + out-of-order message queueing during callHost.
	pluginDir := t.TempDir()
	helperJS := `export function greet(name) { return "hello:" + name; }`
	mainJS := `
import { serve, callHost, log } from '/runtime/block-sdk.js';
import { greet } from './helper.js';

serve((method, params) => {
  if (method === 'plugin.initialize') {
    log('init-from-es-module');
    return { ready: true };
  }
  if (method === 'plugin.action') {
    const res = callHost('host.storage.get', { key: 'k' });
    return { ok: true, status: greet(String(res.value || 'none')) };
  }
  if (method === 'plugin.event') {
    return { handled: true, outcome: 'queued-' + (params.path || '') };
  }
  if (method === 'plugin.shutdown') {
    return { ok: true };
  }
  return {};
});
`
	if err := os.WriteFile(filepath.Join(pluginDir, "helper.js"), []byte(helperJS), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "main.mjs"), []byte(mainJS), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderrBuf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess, err := eng.StartJS(ctx, pluginDir, "main.mjs", StartOptions{Stderr: &stderrBuf})
	if err != nil {
		t.Fatalf("StartJS: %v", err)
	}
	defer sess.Kill()

	sc := bufio.NewScanner(sess.Stdout())
	writeRPC := func(v any) {
		b, _ := json.Marshal(v)
		b = append(b, '\n')
		if _, err := sess.Stdin().Write(b); err != nil {
			t.Fatalf("stdin write: %v", err)
		}
	}
	readRPC := func() map[string]any {
		if !sc.Scan() {
			t.Fatalf("unexpected EOF from stdout (stderr=%q, scanErr=%v)", stderrBuf.String(), sc.Err())
		}
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			t.Fatalf("unmarshal %q: %v", sc.Text(), err)
		}
		return msg
	}

	writeRPC(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "plugin.initialize", "params": map[string]any{}})
	initResp := readRPC()
	if initResp["result"] == nil {
		t.Fatalf("unexpected initResp: %v (stderr=%q)", initResp, stderrBuf.String())
	}

	// Send plugin.action; plugin calls host.storage.get; before answering host.storage.get,
	// inject an out-of-order plugin.event request to verify block-sdk.js queues it cleanly.
	writeRPC(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "plugin.action", "params": map[string]any{}})
	hostReq := readRPC()
	if hostReq["method"] != "host.storage.get" {
		t.Fatalf("expected host.storage.get, got %v", hostReq)
	}
	writeRPC(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "plugin.event", "params": map[string]any{"path": "a.md"}})
	writeRPC(map[string]any{"jsonrpc": "2.0", "id": hostReq["id"], "result": map[string]any{"value": "world"}})

	actionResp := readRPC()
	resMap, _ := actionResp["result"].(map[string]any)
	if resMap["status"] != "hello:world" {
		t.Fatalf("unexpected actionResp: %v", actionResp)
	}
	evtResp := readRPC()
	evtMap, _ := evtResp["result"].(map[string]any)
	if evtMap["outcome"] != "queued-a.md" {
		t.Fatalf("unexpected queued evtResp: %v", evtResp)
	}

	writeRPC(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "plugin.shutdown", "params": map[string]any{}})
	_ = readRPC()
	_ = sess.Stdin().Close()
	if err := sess.Wait(); err != nil {
		t.Fatalf("sess.Wait: %v (stderr=%q)", err, stderrBuf.String())
	}
	if !strings.Contains(stderrBuf.String(), "init-from-es-module") {
		t.Fatalf("expected log output in stderr, got %q", stderrBuf.String())
	}
}

func TestJSGlobalHelperAndNativeQJSStdCompat(t *testing.T) {
	t.Setenv("PATH", "")

	eng, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(context.Background())

	// 1. Global helper without explicit import.
	globalDir := t.TempDir()
	globalJS := `
serve((method) => {
  if (method === 'plugin.initialize') return { ready: true };
  if (method === 'plugin.shutdown') return { ok: true };
  return { ok: true };
});
`
	if err := os.WriteFile(filepath.Join(globalDir, "main.js"), []byte(globalJS), 0o600); err != nil {
		t.Fatal(err)
	}
	sess1, err := eng.StartJS(context.Background(), globalDir, "main.js", StartOptions{})
	if err != nil {
		t.Fatalf("StartJS global: %v", err)
	}
	defer sess1.Kill()
	sc1 := bufio.NewScanner(sess1.Stdout())
	_, _ = sess1.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.initialize","params":{}}` + "\n"))
	if !sc1.Scan() || !strings.Contains(sc1.Text(), `"ready":true`) {
		t.Fatalf("global helper init failed: %q err=%v", sc1.Text(), sc1.Err())
	}
	_, _ = sess1.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"plugin.shutdown","params":{}}` + "\n"))
	if !sc1.Scan() {
		t.Fatalf("global helper shutdown failed: %v", sc1.Err())
	}
	_ = sess1.Stdin().Close()
	if err := sess1.Wait(); err != nil {
		t.Fatalf("global helper Wait: %v", err)
	}

	// 2. Hand-written native qjs:std stdin/stdout JSON-RPC 2.0.
	nativeDir := t.TempDir()
	nativeJS := `
import * as std from 'qjs:std';
while (true) {
  const line = std.in.getline();
  if (line === null || line === undefined) break;
  if (!line.trim()) continue;
  const req = JSON.parse(line);
  if (req.method === 'plugin.initialize') {
    std.out.puts(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: { ready: true } }) + '\n');
    std.out.flush();
  } else if (req.method === 'plugin.shutdown') {
    std.out.puts(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: { ok: true } }) + '\n');
    std.out.flush();
    break;
  }
}
`
	if err := os.WriteFile(filepath.Join(nativeDir, "native.js"), []byte(nativeJS), 0o600); err != nil {
		t.Fatal(err)
	}
	sess2, err := eng.StartJS(context.Background(), nativeDir, "native.js", StartOptions{})
	if err != nil {
		t.Fatalf("StartJS native: %v", err)
	}
	defer sess2.Kill()
	sc2 := bufio.NewScanner(sess2.Stdout())
	_, _ = sess2.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.initialize","params":{}}` + "\n"))
	if !sc2.Scan() || !strings.Contains(sc2.Text(), `"ready":true`) {
		t.Fatalf("native qjs:std init failed: %q err=%v", sc2.Text(), sc2.Err())
	}
	_, _ = sess2.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"plugin.shutdown","params":{}}` + "\n"))
	if !sc2.Scan() {
		t.Fatalf("native qjs:std shutdown failed: %v", sc2.Err())
	}
	_ = sess2.Stdin().Close()
	if err := sess2.Wait(); err != nil {
		t.Fatalf("native qjs:std Wait: %v", err)
	}
}

func TestChaosMemoryBombTimeoutKillAndSandboxEscape(t *testing.T) {
	eng, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(context.Background())
	if err := eng.Warmup(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 1. Memory bomb (2x 20 MiB Uint8Array > 24 MiB qjs limit & 32 MiB Wasm cap) must be intercepted without host memory spike.
	bombDir := t.TempDir()
	bombJS := `
import { serve } from '/runtime/block-sdk.js';
serve((method) => {
  if (method === 'plugin.initialize') {
    const a = new Uint8Array(20 * 1024 * 1024);
    const b = new Uint8Array(20 * 1024 * 1024);
    a[0] = 1;
    b[0] = 2;
    return { ready: true };
  }
  return {};
});
`
	if err := os.WriteFile(filepath.Join(bombDir, "main.js"), []byte(bombJS), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var mBefore, mAfter runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	var bombErrBuf bytes.Buffer
	bombCtx, bombCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bombCancel()
	bombSess, err := eng.StartJS(bombCtx, bombDir, "main.js", StartOptions{Stderr: &bombErrBuf})
	if err != nil {
		t.Fatalf("StartJS bomb: %v", err)
	}
	_, _ = bombSess.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.initialize","params":{}}` + "\n"))
	scBomb := bufio.NewScanner(bombSess.Stdout())
	gotOOM := false
	if scBomb.Scan() {
		var resp map[string]any
		_ = json.Unmarshal(scBomb.Bytes(), &resp)
		if resp["error"] == nil {
			t.Fatalf("expected memory bomb to return RPC error, got %v", resp)
		}
		gotOOM = true
	}
	waitBombErr := bombSess.Wait()
	if !gotOOM && (waitBombErr == nil || !strings.Contains(strings.ToLower(bombErrBuf.String()), "out of memory")) {
		t.Fatalf("expected memory bomb OOM failure, waitErr=%v stderr=%q", waitBombErr, bombErrBuf.String())
	}
	_ = bombSess.Kill()

	runtime.GC()
	runtime.ReadMemStats(&mAfter)
	if mAfter.HeapAlloc > mBefore.HeapAlloc+64*1024*1024 {
		t.Fatalf("host heap grew unexpectedly: before=%d after=%d", mBefore.HeapAlloc, mAfter.HeapAlloc)
	}

	// 2. Infinite loop while(true){} killed on timeout, and next restart recovers in <50ms.
	loopDir := t.TempDir()
	loopJS := `while (true) {}`
	if err := os.WriteFile(filepath.Join(loopDir, "loop.js"), []byte(loopJS), 0o600); err != nil {
		t.Fatal(err)
	}
	loopCtx, loopCancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer loopCancel()
	loopSess, err := eng.StartJS(loopCtx, loopDir, "loop.js", StartOptions{})
	if err != nil {
		t.Fatalf("StartJS loop: %v", err)
	}
	killStart := time.Now()
	waitErr := loopSess.Wait()
	if waitErr == nil {
		t.Fatal("expected infinite loop to fail with timeout/cancellation")
	}
	if elapsed := time.Since(killStart); elapsed > 2*time.Second {
		t.Fatalf("infinite loop took too long to terminate: %v", elapsed)
	}

	goodDir := t.TempDir()
	goodJS := `
import { serve } from '/runtime/block-sdk.js';
serve((method) => method === 'plugin.initialize' ? { ready: true } : { ok: true });
`
	if err := os.WriteFile(filepath.Join(goodDir, "main.js"), []byte(goodJS), 0o600); err != nil {
		t.Fatal(err)
	}
	restartStart := time.Now()
	goodSess, err := eng.StartJS(context.Background(), goodDir, "main.js", StartOptions{})
	if err != nil {
		t.Fatalf("StartJS restart: %v", err)
	}
	defer goodSess.Kill()
	_, _ = goodSess.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.initialize","params":{}}` + "\n"))
	scGood := bufio.NewScanner(goodSess.Stdout())
	if !scGood.Scan() || !strings.Contains(scGood.Text(), `"ready":true`) {
		t.Fatalf("restart init failed: %q err=%v", scGood.Text(), scGood.Err())
	}
	restartDur := time.Since(restartStart)
	t.Logf("restart-to-ready duration: %v", restartDur)
	maxRestart := 50 * time.Millisecond
	if raceDetectorEnabled {
		maxRestart = 500 * time.Millisecond
	}
	if restartDur >= maxRestart {
		t.Fatalf("restart took %v, want <%v", restartDur, maxRestart)
	}
	_, _ = goodSess.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"plugin.shutdown","params":{}}` + "\n"))
	_ = scGood.Scan()
	_ = goodSess.Stdin().Close()
	_ = goodSess.Wait()

	// 3. Sandbox filesystem isolation: read /etc/passwd, write /plugin/hack.txt, symlink escape.
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET_DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	secDir := t.TempDir()
	_ = os.Symlink(outsideFile, filepath.Join(secDir, "leak.js"))
	_ = os.Symlink(outsideFile, filepath.Join(secDir, "leak.txt"))
	secJS := `
import * as std from 'qjs:std';
import { serve } from '/runtime/block-sdk.js';

serve((method) => {
  if (method === 'plugin.initialize') {
    const etcRes = std.open('/etc/passwd', 'r');
    const writeErr = {};
    const wf = std.open('/plugin/hack.txt', 'w', writeErr);
    const leakRes = std.open('/plugin/leak.txt', 'r');
    let importLeakErr = '';
    return {
      ready: true,
      etc_opened: etcRes !== null,
      write_opened: wf !== null,
      write_errno: writeErr.errno || 0,
      leak_opened: leakRes !== null,
      import_leak_err: importLeakErr,
    };
  }
  if (method === 'plugin.render') {
    const f = std.loadFile('/plugin/leak.txt');
    return { leak_content: f };
  }
  return { ok: true };
});
`
	if err := os.WriteFile(filepath.Join(secDir, "main.js"), []byte(secJS), 0o600); err != nil {
		t.Fatal(err)
	}
	secSess, err := eng.StartJS(context.Background(), secDir, "main.js", StartOptions{})
	if err != nil {
		t.Fatalf("StartJS security: %v", err)
	}
	defer secSess.Kill()
	scSec := bufio.NewScanner(secSess.Stdout())
	_, _ = secSess.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"plugin.initialize","params":{}}` + "\n"))
	if !scSec.Scan() {
		t.Fatalf("security init EOF: %v", scSec.Err())
	}
	var secResp struct {
		Result struct {
			Ready       bool `json:"ready"`
			EtcOpened   bool `json:"etc_opened"`
			WriteOpened bool `json:"write_opened"`
			LeakOpened  bool `json:"leak_opened"`
		} `json:"result"`
	}
	if err := json.Unmarshal(scSec.Bytes(), &secResp); err != nil {
		t.Fatal(err)
	}
	if secResp.Result.EtcOpened || secResp.Result.WriteOpened || secResp.Result.LeakOpened {
		t.Fatalf("sandbox violation: %+v", secResp.Result)
	}
	if _, err := os.Stat(filepath.Join(secDir, "hack.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hack.txt should not exist, err=%v", err)
	}
	_, _ = secSess.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"plugin.shutdown","params":{}}` + "\n"))
	_ = scSec.Scan()
	_ = secSess.Stdin().Close()
	_ = secSess.Wait()

	// Entry symlink escape must be rejected by StartJS itself.
	if _, err := eng.StartJS(context.Background(), secDir, "leak.js", StartOptions{}); err == nil {
		t.Fatal("expected StartJS to reject symlink entry")
	}
}

func TestStartWasmMinimalWASI(t *testing.T) {
	eng, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(context.Background())

	// Build a minimal valid WASI module that calls fd_write(1, iov, 1, nwritten) to emit
	// `{"jsonrpc":"2.0","id":1,"result":{"ready":true}}\n` and exits with proc_exit(0).
	line := `{"jsonrpc":"2.0","id":1,"result":{"ready":true}}` + "\n"
	wasmBytes := buildMinimalWASIPrintModule(line)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), wasmBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	sess, err := eng.StartWasm(context.Background(), dir, "plugin.wasm", StartOptions{})
	if err != nil {
		t.Fatalf("StartWasm: %v", err)
	}
	defer sess.Kill()
	out, err := io.ReadAll(sess.Stdout())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(out) != line {
		t.Fatalf("StartWasm stdout = %q, want %q", string(out), line)
	}
	if err := sess.Wait(); err != nil {
		t.Fatalf("StartWasm Wait: %v", err)
	}

	// Reject escaping path and symlinked wasm entry.
	if _, err := eng.StartWasm(context.Background(), dir, "../evil.wasm", StartOptions{}); err == nil {
		t.Fatal("expected StartWasm to reject ../evil.wasm")
	}
}

// buildMinimalWASIPrintModule encodes a minimal valid Wasm/WASI binary printing msg to stdout.
func buildMinimalWASIPrintModule(msg string) []byte {
	var mod []byte
	mod = append(mod, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)

	addSection := func(id byte, payload []byte) {
		mod = append(mod, id)
		mod = binary.AppendUvarint(mod, uint64(len(payload)))
		mod = append(mod, payload...)
	}
	encodeStr := func(s string) []byte {
		b := binary.AppendUvarint(nil, uint64(len(s)))
		return append(b, s...)
	}

	// Type section:
	// 0: (i32, i32, i32, i32) -> (i32)  [fd_write]
	// 1: (i32) -> ()                    [proc_exit]
	// 2: () -> ()                       [_start]
	typeSec := []byte{3,
		0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f,
		0x60, 1, 0x7f, 0,
		0x60, 0, 0,
	}
	addSection(1, typeSec)

	// Import section:
	// 0: wasi_snapshot_preview1.fd_write : func type 0
	// 1: wasi_snapshot_preview1.proc_exit : func type 1
	var impSec []byte
	impSec = append(impSec, 2)
	impSec = append(impSec, encodeStr("wasi_snapshot_preview1")...)
	impSec = append(impSec, encodeStr("fd_write")...)
	impSec = append(impSec, 0x00, 0)
	impSec = append(impSec, encodeStr("wasi_snapshot_preview1")...)
	impSec = append(impSec, encodeStr("proc_exit")...)
	impSec = append(impSec, 0x00, 1)
	addSection(2, impSec)

	// Function section: 1 func (_start) with type 2
	addSection(3, []byte{1, 2})

	// Memory section: 1 memory, min 1 page
	addSection(5, []byte{1, 0x00, 1})

	// Export section: memory ("memory" -> mem 0), "_start" -> func 2
	var expSec []byte
	expSec = append(expSec, 2)
	expSec = append(expSec, encodeStr("memory")...)
	expSec = append(expSec, 0x02, 0)
	expSec = append(expSec, encodeStr("_start")...)
	expSec = append(expSec, 0x00, 2)
	addSection(7, expSec)

	// Code section: _start calls fd_write(1, 0, 1, 8) then drop, proc_exit(0)
	var body []byte
	body = append(body, 0) // 0 locals
	body = append(body,
		0x41, 1, // i32.const 1 (stdout fd)
		0x41, 0, // i32.const 0 (iovs ptr)
		0x41, 1, // i32.const 1 (iovs len)
		0x41, 8, // i32.const 8 (nwritten ptr)
		0x10, 0, // call fd_write
		0x1a,    // drop
		0x41, 0, // i32.const 0
		0x10, 1, // call proc_exit
		0x0b, // end
	)
	var codeSec []byte
	codeSec = append(codeSec, 1)
	codeSec = binary.AppendUvarint(codeSec, uint64(len(body)))
	codeSec = append(codeSec, body...)
	addSection(10, codeSec)

	// Data section:
	// segment 0 at offset 0: iovec { ptr=16, len=len(msg) }
	// segment 1 at offset 16: msg
	var iov [8]byte
	binary.LittleEndian.PutUint32(iov[0:4], 16)
	binary.LittleEndian.PutUint32(iov[4:8], uint32(len(msg)))

	var dataSec []byte
	dataSec = append(dataSec, 2)
	// seg 0
	dataSec = append(dataSec, 0, 0x41, 0, 0x0b)
	dataSec = binary.AppendUvarint(dataSec, 8)
	dataSec = append(dataSec, iov[:]...)
	// seg 1
	dataSec = append(dataSec, 0, 0x41, 16, 0x0b)
	dataSec = binary.AppendUvarint(dataSec, uint64(len(msg)))
	dataSec = append(dataSec, msg...)
	addSection(11, dataSec)

	return mod
}
