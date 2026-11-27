package app

import (
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	defaultGoMemLimitBytes int64 = 256 << 20 // 256 MiB soft Go heap ceiling (headroom for large PNG exports)
	defaultGoGCPercent           = 75
	minMemoryReleaseInterval     = 15 * time.Second
)

var (
	configureMemoryOnce sync.Once

	memReleaseMu     sync.Mutex
	memReleaseTimer  *time.Timer
	memReleaseSeq    uint64
	lastMemReleaseAt time.Time
)

// ConfigureRuntimeMemory configures Go's soft memory limit and GC target ratio
// at startup unless the user explicitly set GOMEMLIMIT or GOGC in the environment.
func ConfigureRuntimeMemory() {
	configureMemoryOnce.Do(func() {
		if v, ok := os.LookupEnv("GOMEMLIMIT"); !ok || strings.TrimSpace(v) == "" {
			debug.SetMemoryLimit(defaultGoMemLimitBytes)
		}
		if v, ok := os.LookupEnv("GOGC"); !ok || strings.TrimSpace(v) == "" {
			debug.SetGCPercent(defaultGoGCPercent)
		}
	})
}

// ScheduleMemoryRelease debounces and rate-limits debug.FreeOSMemory so unused
// Go heap pages are returned gently to the OS after startup or inactivity settles.
func ScheduleMemoryRelease(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	memReleaseMu.Lock()
	defer memReleaseMu.Unlock()

	if !lastMemReleaseAt.IsZero() {
		if cooldown := minMemoryReleaseInterval - time.Since(lastMemReleaseAt); cooldown > delay {
			delay = cooldown
		}
	}

	memReleaseSeq++
	seq := memReleaseSeq
	if memReleaseTimer != nil {
		memReleaseTimer.Stop()
	}
	memReleaseTimer = time.AfterFunc(delay, func() {
		memReleaseMu.Lock()
		if memReleaseSeq != seq {
			memReleaseMu.Unlock()
			return
		}
		memReleaseTimer = nil
		lastMemReleaseAt = time.Now()
		memReleaseMu.Unlock()

		debug.FreeOSMemory()
	})
}

// CancelScheduledMemoryRelease cancels any pending debounced memory release timer.
func CancelScheduledMemoryRelease() {
	memReleaseMu.Lock()
	defer memReleaseMu.Unlock()
	memReleaseSeq++
	if memReleaseTimer != nil {
		memReleaseTimer.Stop()
		memReleaseTimer = nil
	}
}
