//go:build darwin

package files

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"
)

const (
	darwinPickerHelperFlag = "--darwin-file-picker"
	darwinPickerLinePrefix = "BLOCK_PICKER:"
)

var (
	errDarwinHelperUnavailable = errors.New("macOS file picker helper unavailable")
	errDarwinHelperStale       = errors.New("macOS file picker helper bundle replaced")
)

type darwinPanelResponse struct {
	Ready bool     `json:"ready,omitempty"`
	ID    int64    `json:"id,omitempty"`
	Paths []string `json:"paths,omitempty"`
	Stale bool     `json:"stale,omitempty"`
	Error string   `json:"error,omitempty"`
}

// --- Helper process main-thread GCD dispatch (no mygo dependency) ---

var (
	helperMainOnce   sync.Once
	helperMainQueue  uintptr
	helperDispatchFn func(queue, ctx, work uintptr)
	helperDrainCB    uintptr
	helperQueueMu    sync.Mutex
	helperQueueFns   []func()
)

func initHelperGCD() bool {
	ok := true
	helperMainOnce.Do(func() {
		q, err := purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil {
			ok = false
			return
		}
		fn, err := purego.Dlsym(purego.RTLD_DEFAULT, "dispatch_async_f")
		if err != nil {
			ok = false
			return
		}
		helperMainQueue = q
		purego.RegisterFunc(&helperDispatchFn, fn)
		helperDrainCB = purego.NewCallback(func(_ uintptr) {
			drainHelperMainQueue()
		})
	})
	return ok && helperMainQueue != 0 && helperDispatchFn != nil
}

func postHelperMain(fn func()) {
	helperQueueMu.Lock()
	helperQueueFns = append(helperQueueFns, fn)
	helperQueueMu.Unlock()
	helperDispatchFn(helperMainQueue, 0, helperDrainCB)
}

func drainHelperMainQueue() {
	for {
		helperQueueMu.Lock()
		if len(helperQueueFns) == 0 {
			helperQueueMu.Unlock()
			return
		}
		fn := helperQueueFns[0]
		helperQueueFns[0] = nil
		helperQueueFns = helperQueueFns[1:]
		helperQueueMu.Unlock()
		fn()
	}
}

// MaybeRunDarwinPickerHelper checks if Block was invoked in the lightweight
// out-of-process macOS file-picker helper mode (used after a hot in-place
// Block.app replacement while an older Block process stays running).
// It pre-warms NSOpenPanel on startup and serves pick requests over stdin/stdout
// in ~5-15ms with zero Dock icon and automatic exit when the parent closes stdin.
func MaybeRunDarwinPickerHelper() bool {
	if len(os.Args) != 2 || os.Args[1] != darwinPickerHelperFlag {
		return false
	}
	runDarwinPickerHelperServer()
	return true
}

func runDarwinPickerHelperServer() {
	runtime.LockOSThread()
	ensureDarwinAppKitLoaded()
	if !initHelperGCD() {
		os.Exit(2)
	}

	var outMu sync.Mutex
	writeResp := func(resp darwinPanelResponse) {
		raw, err := json.Marshal(resp)
		if err != nil {
			return
		}
		outMu.Lock()
		_, _ = os.Stdout.WriteString(darwinPickerLinePrefix)
		_, _ = os.Stdout.Write(raw)
		_, _ = os.Stdout.Write([]byte{'\n'})
		outMu.Unlock()
	}

	var app objc.ID
	darwinWithAutoreleasePool(func() {
		app = pickerSend(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
		if app == 0 {
			os.Exit(2)
		}
		// NSApplicationActivationPolicyAccessory = 1 (no Dock icon, full interactive UI).
		pickerSend(app, "setActivationPolicy:", int(1))
		pickerSend(app, "finishLaunching")

		darwinWarmSavePanelService()
		if _, err := darwinEnsureCachedPanel(false); err != nil {
			os.Exit(2)
		}
		darwinPrewarmCommonUTTypes()
	})

	writeResp(darwinPanelResponse{Ready: true})

	// Main-thread state for active request cancellation.
	var activeID int64
	var activePanel objc.ID
	parentPID := int32(os.Getppid())

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, darwinPickerLinePrefix) {
				continue
			}
			var req darwinPanelRequest
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, darwinPickerLinePrefix)), &req); err != nil {
				continue
			}
			r := req
			postHelperMain(func() {
				darwinWithAutoreleasePool(func() {
					if r.Op == "cancel" {
						if activeID == r.ID && activePanel != 0 {
							pickerSend(activePanel, "cancel:", objc.ID(0))
						}
						return
					}
					if r.Op != "open" && r.Op != "save" {
						return
					}
					if !darwinBundleIntact() {
						writeResp(darwinPanelResponse{ID: r.ID, Stale: true})
						os.Exit(0)
					}
					isSave := r.Op == "save"
					cp, err := darwinEnsureCachedPanel(isSave)
					if err != nil {
						writeResp(darwinPanelResponse{ID: r.ID, Stale: true, Error: err.Error()})
						os.Exit(0)
					}
					darwinConfigurePanel(cp, r)

					panel := cp.panel
					activeID = r.ID
					activePanel = panel
					reqID := r.ID

					completion := objc.NewBlock(func(_ objc.Block, response int) {
						darwinWithAutoreleasePool(func() {
							if activeID == reqID {
								activeID = 0
								activePanel = 0
							}
							var paths []string
							if response == 1 {
								paths = darwinExtractPanelPaths(panel, isSave)
							}
							healthy := darwinPanelHealthy(panel)
							if !healthy {
								darwinInvalidateCachedPanel(isSave, panel)
							}
							darwinYieldActivationToPID(parentPID)
							if response != 1 && !healthy {
								writeResp(darwinPanelResponse{ID: reqID, Stale: true, Error: errDarwinNativePanelFailed.Error()})
								return
							}
							writeResp(darwinPanelResponse{ID: reqID, Paths: paths})
						})
					})
					defer completion.Release()

					darwinActivateSelf()
					if !cp.shownOnce {
						pickerSend(panel, "center")
					}
					pickerSend(panel, "beginWithCompletionHandler:", completion)
					pickerSend(panel, "makeKeyAndOrderFront:", objc.ID(0))
					cp.shownOnce = true
				})
			})
		}
		// Parent closed stdin or exited: terminate immediately with zero orphans.
		os.Exit(0)
	}()

	pickerSend(app, "run")
}

// --- Parent-side warm helper client + kqueue bundle replacement watcher ---

type darwinHelperProc struct {
	cmd       *exec.Cmd
	pid       int
	stdin     io.WriteCloser
	execPath  string
	execID    darwinFileIdentity
	codeResID darwinFileIdentity

	readyOnce sync.Once
	readyCh   chan struct{}
	readyOK   bool

	mu      sync.Mutex
	writeMu sync.Mutex
	closed  bool
	waiters map[int64]chan darwinPanelResponse
}

func (h *darwinHelperProc) markReady(ok bool) {
	h.readyOnce.Do(func() {
		h.readyOK = ok
		close(h.readyCh)
	})
}

func (h *darwinHelperProc) close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	stdin := h.stdin
	h.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
}

func (h *darwinHelperProc) closeAndWait(timeout time.Duration) {
	h.close()
	done := make(chan struct{})
	go func() {
		_ = h.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		<-done
	}
}

func (h *darwinHelperProc) sendRequest(req darwinPanelRequest) error {
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	h.mu.Lock()
	closed := h.closed
	stdin := h.stdin
	h.mu.Unlock()
	if closed || stdin == nil {
		return errDarwinHelperUnavailable
	}
	_, err = fmt.Fprintf(stdin, "%s%s\n", darwinPickerLinePrefix, raw)
	return err
}

func (h *darwinHelperProc) isAlive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed
}

type darwinHelperManager struct {
	mu      sync.Mutex
	helper  *darwinHelperProc
	stopped bool
	nextID  atomic.Int64

	watchOnce  sync.Once
	watchStopR *os.File
	watchStopW *os.File
}

var darwinHelperMgr darwinHelperManager

func currentDiskExecIdentity() (rawPath string, execID, codeResID darwinFileIdentity, ok bool) {
	rawPath = darwinStartupRawExecPath
	if rawPath == "" {
		return "", darwinFileIdentity{}, darwinFileIdentity{}, false
	}
	resolved := rawPath
	if r, err := filepath.EvalSymlinks(rawPath); err == nil && r != "" {
		resolved = r
	}
	st, err := os.Stat(resolved)
	if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return "", darwinFileIdentity{}, darwinFileIdentity{}, false
	}
	execID = readDarwinFileIdentity(resolved)
	if !execID.valid {
		return "", darwinFileIdentity{}, darwinFileIdentity{}, false
	}
	if idx := strings.LastIndex(resolved, ".app/Contents/MacOS/"); idx > 0 {
		codeRes := filepath.Join(resolved[:idx+4], "Contents", "_CodeSignature", "CodeResources")
		codeResID = readDarwinFileIdentity(codeRes)
	}
	return rawPath, execID, codeResID, true
}

func (m *darwinHelperManager) getOrStartHelper() (*darwinHelperProc, error) {
	rawPath, execID, codeResID, ok := currentDiskExecIdentity()
	if !ok {
		return nil, errDarwinHelperUnavailable
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, errDarwinHelperUnavailable
	}

	if m.helper != nil && m.helper.isAlive() &&
		m.helper.execPath == rawPath &&
		m.helper.execID == execID &&
		m.helper.codeResID == codeResID {
		return m.helper, nil
	}

	if m.helper != nil {
		old := m.helper
		m.helper = nil
		go old.close()
	}

	cmd := exec.Command(rawPath, darwinPickerHelperFlag)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	h := &darwinHelperProc{
		cmd:       cmd,
		pid:       cmd.Process.Pid,
		stdin:     stdin,
		execPath:  rawPath,
		execID:    execID,
		codeResID: codeResID,
		readyCh:   make(chan struct{}),
		waiters:   make(map[int64]chan darwinPanelResponse),
	}
	m.helper = h

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, darwinPickerLinePrefix) {
				continue
			}
			var resp darwinPanelResponse
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, darwinPickerLinePrefix)), &resp); err != nil {
				continue
			}
			if resp.Ready {
				h.markReady(true)
				continue
			}
			h.mu.Lock()
			ch := h.waiters[resp.ID]
			delete(h.waiters, resp.ID)
			h.mu.Unlock()
			if ch != nil {
				ch <- resp
			}
		}

		// Helper exited or pipe closed: unblock ready and all pending callers, then reap.
		h.markReady(false)
		h.mu.Lock()
		h.closed = true
		pending := h.waiters
		h.waiters = nil
		h.mu.Unlock()
		for _, ch := range pending {
			ch <- darwinPanelResponse{Error: errDarwinHelperUnavailable.Error()}
		}
		_ = cmd.Wait()

		m.mu.Lock()
		if m.helper == h {
			m.helper = nil
		}
		m.mu.Unlock()
	}()

	return h, nil
}

func (m *darwinHelperManager) invalidate(h *darwinHelperProc) {
	if h == nil {
		return
	}
	m.mu.Lock()
	if m.helper == h {
		m.helper = nil
	}
	m.mu.Unlock()
	go h.close()
}

func (m *darwinHelperManager) prewarmAsync() {
	go func() {
		_, _ = m.getOrStartHelper()
	}()
}

func (m *darwinHelperManager) pick(ctx context.Context, req darwinPanelRequest, allowed func() bool) ([]string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		h, err := m.getOrStartHelper()
		if err != nil {
			return nil, errDarwinHelperUnavailable
		}

		select {
		case <-h.readyCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			m.invalidate(h)
			return nil, errDarwinHelperUnavailable
		}

		if !h.readyOK || !h.isAlive() {
			m.invalidate(h)
			return nil, errDarwinHelperUnavailable
		}

		if allowed != nil && !allowed() {
			return nil, context.Canceled
		}

		reqID := m.nextID.Add(1)
		req.ID = reqID
		respCh := make(chan darwinPanelResponse, 1)

		h.mu.Lock()
		if h.closed || h.waiters == nil {
			h.mu.Unlock()
			m.invalidate(h)
			continue
		}
		h.waiters[reqID] = respCh
		h.mu.Unlock()

		// Yield activation cooperatively to the helper PID on macOS 14+ so its
		// NSOpenPanel/NSSavePanel immediately becomes key in front of Block.
		if mygo.App.IsReady() {
			helperPID := int32(h.pid)
			mygo.RunOnMain(func() {
				darwinWithAutoreleasePool(func() {
					darwinYieldActivationToPID(helperPID)
				})
			})
		}

		if err := h.sendRequest(req); err != nil {
			h.mu.Lock()
			delete(h.waiters, reqID)
			h.mu.Unlock()
			m.invalidate(h)
			continue
		}

		stop := context.AfterFunc(ctx, func() {
			_ = h.sendRequest(darwinPanelRequest{ID: reqID, Op: "cancel"})
		})

		var resp darwinPanelResponse
		select {
		case resp = <-respCh:
			stop()
		case <-ctx.Done():
			stop()
			select {
			case <-respCh:
			case <-time.After(400 * time.Millisecond):
			}
			h.mu.Lock()
			delete(h.waiters, reqID)
			h.mu.Unlock()
			return nil, ctx.Err()
		}

		if resp.Stale {
			m.invalidate(h)
			continue
		}
		if resp.Error != "" {
			return nil, errDarwinHelperUnavailable
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return resp.Paths, nil
	}
	return nil, errDarwinHelperUnavailable
}

func (m *darwinHelperManager) startBundleWatcher() {
	m.watchOnce.Do(func() {
		r, w, err := os.Pipe()
		if err != nil {
			return
		}
		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			_ = r.Close()
			_ = w.Close()
			return
		}
		m.watchStopR = r
		m.watchStopW = w
		m.mu.Unlock()

		go m.runBundleWatcher(r)
	})
}

func (m *darwinHelperManager) runBundleWatcher(stopPipe *os.File) {
	kq, err := unix.Kqueue()
	if err != nil {
		return
	}
	defer unix.Close(kq)

	stopFD := int(stopPipe.Fd())
	stopChange := unix.Kevent_t{
		Ident:  uint64(stopFD),
		Filter: unix.EVFILT_READ,
		Flags:  unix.EV_ADD | unix.EV_ENABLE,
	}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{stopChange}, nil, nil); err != nil {
		return
	}

	watchPaths := func() []string {
		var paths []string
		if root := darwinBundleRootPath(); root != "" {
			paths = append(paths, root)
		}
		if darwinStartupRawExecPath != "" {
			paths = append(paths, darwinStartupRawExecPath)
		}
		if darwinStartupCodeResPath != "" {
			paths = append(paths, darwinStartupCodeResPath)
		}
		return paths
	}

	openWatchFDs := func() []int {
		var fds []int
		for _, p := range watchPaths() {
			fd, err := unix.Open(p, unix.O_EVTONLY, 0)
			if err != nil {
				continue
			}
			ev := unix.Kevent_t{
				Ident:  uint64(fd),
				Filter: unix.EVFILT_VNODE,
				Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR,
				Fflags: unix.NOTE_RENAME | unix.NOTE_DELETE | unix.NOTE_WRITE | unix.NOTE_ATTRIB | unix.NOTE_REVOKE,
			}
			if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
				_ = unix.Close(fd)
				continue
			}
			fds = append(fds, fd)
		}
		return fds
	}

	closeFDs := func(fds []int) {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	}

	isStopped := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.stopped
	}

	sleepOrStop := func(d time.Duration) bool {
		ts := unix.NsecToTimespec(d.Nanoseconds())
		var evs [1]unix.Kevent_t
		n, _ := unix.Kevent(kq, nil, evs[:], &ts)
		if n > 0 && int(evs[0].Ident) == stopFD {
			return false
		}
		return !isStopped()
	}

	events := make([]unix.Kevent_t, 8)
	hadMissing := false
	for !isStopped() {
		fds := openWatchFDs()
		if len(fds) == 0 {
			hadMissing = true
			// Bundle may be mid-swap; wait briefly and retry.
			if !sleepOrStop(500 * time.Millisecond) {
				return
			}
			continue
		}
		if hadMissing {
			hadMissing = false
			if !darwinBundleIntact() {
				m.prewarmAsync()
			}
		}

		n, err := unix.Kevent(kq, nil, events, nil)
		closeFDs(fds)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		for i := 0; i < n; i++ {
			if int(events[i].Ident) == stopFD {
				return
			}
		}

		// Debounce atomic rename + codesign verification during commitInstall.
		if !sleepOrStop(500 * time.Millisecond) {
			return
		}
		if !darwinBundleIntact() {
			m.prewarmAsync()
		}
	}
}

func (m *darwinHelperManager) shutdown() {
	m.mu.Lock()
	m.stopped = true
	h := m.helper
	m.helper = nil
	stopW := m.watchStopW
	stopR := m.watchStopR
	m.watchStopW = nil
	m.watchStopR = nil
	m.mu.Unlock()

	if stopW != nil {
		_, _ = stopW.Write([]byte{1})
		_ = stopW.Close()
	}
	if stopR != nil {
		_ = stopR.Close()
	}
	if h != nil {
		h.closeAndWait(250 * time.Millisecond)
	}
}

func (m *darwinHelperManager) resume() {
	m.mu.Lock()
	m.stopped = false
	m.mu.Unlock()
}
