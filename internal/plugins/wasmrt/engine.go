package wasmrt

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const (
	// MemoryLimitPages caps each Wasm instance at 512 * 64 KiB = 32 MiB.
	MemoryLimitPages  uint32 = 512
	qjsMemoryLimitKiB        = "16384"
	qjsStackSizeKiB          = "1024"
)

type StartOptions struct {
	Env    map[string]string
	Stderr io.Writer
}

type Engine struct {
	cacheDir string
	cache    wazero.CompilationCache

	warmupMu   sync.Mutex
	warmedUp   bool
	warmupErr  error
	closedOnce sync.Once
	closeErr   error
}

type Session struct {
	stdinW  *memPipeWriter
	stdinR  *memPipeReader
	stdoutR *memPipeReader
	stdoutW *memPipeWriter
	cancel  context.CancelFunc
	done    chan struct{}

	errMu   sync.Mutex
	waitErr error
}

func NewEngine(cacheDir string) (*Engine, error) {
	var cache wazero.CompilationCache
	if dir := strings.TrimSpace(cacheDir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			if dirCache, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
				cache = dirCache
			}
		}
	}
	if cache == nil {
		cache = wazero.NewCompilationCache()
	}
	return &Engine{
		cacheDir: cacheDir,
		cache:    cache,
	}, nil
}

func (e *Engine) runtimeConfig() wazero.RuntimeConfig {
	return wazero.NewRuntimeConfig().
		WithMemoryLimitPages(MemoryLimitPages).
		WithCloseOnContextDone(true).
		WithCompilationCache(e.cache)
}

func (e *Engine) Warmup(ctx context.Context) error {
	e.warmupMu.Lock()
	defer e.warmupMu.Unlock()
	if e.warmedUp {
		return e.warmupErr
	}
	wasmBytes, err := LoadQJSWasm()
	if err != nil {
		e.warmupErr = err
		return err
	}
	r := wazero.NewRuntimeWithConfig(ctx, e.runtimeConfig())
	defer func() {
		_ = r.Close(context.Background())
	}()
	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err == nil {
		_ = compiled.Close(ctx)
		e.warmedUp = true
	}
	e.warmupErr = err
	return err
}

func (e *Engine) Close(ctx context.Context) error {
	e.closedOnce.Do(func() {
		if e.cache != nil {
			e.closeErr = e.cache.Close(ctx)
		}
	})
	return e.closeErr
}

func (e *Engine) StartJS(ctx context.Context, pluginDir, entryRel string, opts StartOptions) (*Session, error) {
	cleanRel, err := ValidateEntryPath(entryRel, ".js", ".mjs")
	if err != nil {
		return nil, err
	}
	pluginRoot, _, err := openRegularPluginEntry(pluginDir, cleanRel)
	if err != nil {
		return nil, err
	}
	wasmBytes, err := LoadQJSWasm()
	if err != nil {
		_ = pluginRoot.Close()
		return nil, err
	}
	guestEntry := "/plugin/" + cleanRel
	bootstrap := fmt.Sprintf("import '/runtime/block-sdk.js'; import %s;", strconv.Quote(guestEntry))
	args := []string{
		"qjs",
		"--std",
		"--memory-limit", qjsMemoryLimitKiB,
		"--stack-size", qjsStackSizeKiB,
		"-m",
		"-e", bootstrap,
	}
	return e.startWasmSession(ctx, pluginRoot, wasmBytes, args, opts)
}

func (e *Engine) StartWasm(ctx context.Context, pluginDir, entryRel string, opts StartOptions) (*Session, error) {
	cleanRel, err := ValidateEntryPath(entryRel, ".wasm")
	if err != nil {
		return nil, err
	}
	pluginRoot, _, err := openRegularPluginEntry(pluginDir, cleanRel)
	if err != nil {
		return nil, err
	}
	wasmBytes, err := readRegularPluginEntry(pluginRoot, cleanRel, maxWasmEntryBytes)
	if err != nil {
		_ = pluginRoot.Close()
		return nil, err
	}
	args := []string{"/plugin/" + cleanRel}
	return e.startWasmSession(ctx, pluginRoot, wasmBytes, args, opts)
}

func (e *Engine) startWasmSession(ctx context.Context, pluginRoot *os.Root, wasmBytes []byte, args []string, opts StartOptions) (*Session, error) {
	sessCtx, cancel := context.WithCancel(ctx)
	r := wazero.NewRuntimeWithConfig(sessCtx, e.runtimeConfig())
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			cancel()
			_ = r.Close(context.Background())
			_ = pluginRoot.Close()
		}
	}()

	if _, err := wasi_snapshot_preview1.Instantiate(sessCtx, r); err != nil {
		return nil, fmt.Errorf("instantiate wasi: %w", err)
	}
	compiled, err := r.CompileModule(sessCtx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	stdinR, stdinW := newMemPipe(defaultPipeCapacity)
	stdoutR, stdoutW := newMemPipe(defaultPipeCapacity)
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	// Only expose read-only /plugin (jailed via os.OpenRoot) and read-only /runtime.
	fsCfg := wazero.NewFSConfig().
		WithFSMount(pluginRoot.FS(), "/plugin").
		WithFSMount(runtimeEmbedFS, "/runtime")

	modCfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs(args...).
		WithStdin(stdinR).
		WithStdout(stdoutW).
		WithStderr(stderr).
		WithFSConfig(fsCfg).
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader)

	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			modCfg = modCfg.WithEnv(k, opts.Env[k])
		}
	}

	sess := &Session{
		stdinW:  stdinW,
		stdinR:  stdinR,
		stdoutR: stdoutR,
		stdoutW: stdoutW,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	cleanupOnErr = false

	go func() {
		defer close(sess.done)
		defer func() {
			_ = r.Close(context.Background())
			_ = pluginRoot.Close()
			_ = stdoutW.Close()
			_ = stdinR.Close()
			cancel()
		}()
		_, runErr := r.InstantiateModule(sessCtx, compiled, modCfg)
		var exitErr *sys.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 0 {
			runErr = nil
		}
		sess.setWaitErr(runErr)
	}()
	return sess, nil
}

func (s *Session) Stdin() io.WriteCloser {
	return s.stdinW
}

func (s *Session) Stdout() io.ReadCloser {
	return s.stdoutR
}

func (s *Session) Kill() error {
	s.cancel()
	closePipePair(s.stdinR, s.stdinW, io.ErrClosedPipe)
	closePipePair(s.stdoutR, s.stdoutW, io.ErrClosedPipe)
	return nil
}

func (s *Session) Wait() error {
	<-s.done
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.waitErr
}

func (s *Session) setWaitErr(err error) {
	s.errMu.Lock()
	s.waitErr = err
	s.errMu.Unlock()
}
