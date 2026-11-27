//go:build darwin

package modals

import (
	"encoding/binary"
	"os"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// rusage_info_v0 layout: uuid[16] + 10 uint64 fields; ri_phys_footprint is the 8th.
const (
	darwinRusageInfoV0Size      = 16 + 10*8
	darwinRusagePhysFootprintAt = 16 + 7*8
)

var darwinProcPidRusage struct {
	once sync.Once
	fn   func(pid int32, flavor int32, buf unsafe.Pointer) int32
}

func init() {
	platformFootprintFn = readDarwinPhysFootprint
}

// readDarwinPhysFootprint returns the kernel's physical footprint for this
// process (the "Memory" figure in Activity Monitor) with one syscall instead
// of forking `ps`.
func readDarwinPhysFootprint() uint64 {
	darwinProcPidRusage.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		sym, err := purego.Dlsym(lib, "proc_pid_rusage")
		if err != nil || sym == 0 {
			return
		}
		purego.RegisterFunc(&darwinProcPidRusage.fn, sym)
	})
	if darwinProcPidRusage.fn == nil {
		return 0
	}
	var buf [darwinRusageInfoV0Size]byte
	if darwinProcPidRusage.fn(int32(os.Getpid()), 0, unsafe.Pointer(&buf[0])) != 0 {
		return 0
	}
	return binary.LittleEndian.Uint64(buf[darwinRusagePhysFootprintAt:])
}
