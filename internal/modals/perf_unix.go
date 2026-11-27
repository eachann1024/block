//go:build !windows

package modals

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const unixRSSRefreshInterval = 15 * time.Second

var unixPerfSampler = struct {
	mu        sync.Mutex
	prevCPU   time.Duration
	prevWall  time.Time
	lastCPU   float64
	lastRSS   uint64
	lastRSSAt time.Time
}{}

// platformFootprintFn returns the process memory footprint without spawning a
// helper process; nil or 0 falls back to `ps`.
var platformFootprintFn func() uint64

func init() {
	platformProcessStatsFn = readUnixProcessStats
}

func readUnixProcessStats() (cpu float64, rss uint64, ok bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, 0, false
	}

	cpuTime := time.Duration(ru.Utime.Nano()+ru.Stime.Nano()) * time.Nanosecond
	now := time.Now()

	unixPerfSampler.mu.Lock()
	if !unixPerfSampler.prevWall.IsZero() {
		wallDelta := now.Sub(unixPerfSampler.prevWall)
		if wallDelta >= 100*time.Millisecond {
			cpuDelta := cpuTime - unixPerfSampler.prevCPU
			if cpuDelta > 0 {
				unixPerfSampler.lastCPU = (float64(cpuDelta) / float64(wallDelta)) * 100.0
			} else {
				unixPerfSampler.lastCPU = 0
			}
			unixPerfSampler.prevCPU = cpuTime
			unixPerfSampler.prevWall = now
		}
	} else {
		unixPerfSampler.prevCPU = cpuTime
		unixPerfSampler.prevWall = now
	}
	cpu = unixPerfSampler.lastCPU

	needRSS := unixPerfSampler.lastRSSAt.IsZero() || now.Sub(unixPerfSampler.lastRSSAt) >= unixRSSRefreshInterval
	rss = unixPerfSampler.lastRSS
	unixPerfSampler.mu.Unlock()

	if platformFootprintFn != nil {
		if native := platformFootprintFn(); native > 0 {
			return cpu, native, true
		}
	}
	if runtime.GOOS == "linux" {
		if linuxRSS := readLinuxStatmRSS(); linuxRSS > 0 {
			rss = linuxRSS
			unixPerfSampler.mu.Lock()
			unixPerfSampler.lastRSS = rss
			unixPerfSampler.lastRSSAt = now
			unixPerfSampler.mu.Unlock()
		}
	} else if needRSS {
		if sampled := sampleUnixRSSOnce(); sampled > 0 {
			rss = sampled
			unixPerfSampler.mu.Lock()
			unixPerfSampler.lastRSS = rss
			unixPerfSampler.lastRSSAt = now
			unixPerfSampler.mu.Unlock()
		}
	}

	if rss == 0 && ru.Maxrss > 0 {
		if runtime.GOOS == "linux" {
			rss = uint64(ru.Maxrss) * 1024
		} else {
			rss = uint64(ru.Maxrss)
		}
	}
	return cpu, rss, true
}

func readLinuxStatmRSS() uint64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

func sampleUnixRSSOnce() uint64 {
	out, err := exec.Command("ps", "-p", strconv.Itoa(os.Getpid()), "-o", "rss=").Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

func statPhysicalFile(info os.FileInfo) (id fileIdentity, hasID bool, fileBytes uint64) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		id = fileIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino)}
		hasID = true
		if st.Blocks > 0 {
			fileBytes = uint64(st.Blocks) * 512
		}
		return id, hasID, fileBytes
	}
	if info.Size() > 0 {
		fileBytes = uint64(info.Size())
	}
	return fileIdentity{}, false, fileBytes
}
