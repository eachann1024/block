//go:build darwin

package theme

import (
	"bytes"
	"sync"

	"github.com/ebitengine/purego"
)

const cfStringEncodingUTF8 = 0x08000100

var (
	macLocaleOnce      sync.Once
	macLocaleReady     bool
	cfCopyPrefLangs    func() uintptr
	cfLangArrayCount   func(uintptr) int
	cfLangArrayValueAt func(uintptr, int) uintptr
	cfLangStrGetCStr   func(uintptr, *byte, int, uint32) byte
	cfLangRelease      func(uintptr)
)

func initMacLocale() {
	macLocaleOnce.Do(func() {
		cfLib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		sCopy, e1 := purego.Dlsym(cfLib, "CFLocaleCopyPreferredLanguages")
		sCount, e2 := purego.Dlsym(cfLib, "CFArrayGetCount")
		sVal, e3 := purego.Dlsym(cfLib, "CFArrayGetValueAtIndex")
		sCStr, e4 := purego.Dlsym(cfLib, "CFStringGetCString")
		sRel, e5 := purego.Dlsym(cfLib, "CFRelease")
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
			return
		}
		purego.RegisterFunc(&cfCopyPrefLangs, sCopy)
		purego.RegisterFunc(&cfLangArrayCount, sCount)
		purego.RegisterFunc(&cfLangArrayValueAt, sVal)
		purego.RegisterFunc(&cfLangStrGetCStr, sCStr)
		purego.RegisterFunc(&cfLangRelease, sRel)
		macLocaleReady = true
	})
}

func platformPreferredLanguages() []string {
	initMacLocale()
	if macLocaleReady {
		if arr := cfCopyPrefLangs(); arr != 0 {
			defer cfLangRelease(arr)
			count := cfLangArrayCount(arr)
			if count > 0 {
				out := make([]string, 0, count)
				var buf [128]byte
				for i := 0; i < count; i++ {
					cfStr := cfLangArrayValueAt(arr, i)
					if cfStr == 0 {
						continue
					}
					if cfLangStrGetCStr(cfStr, &buf[0], len(buf), cfStringEncodingUTF8) == 0 {
						continue
					}
					n := bytes.IndexByte(buf[:], 0)
					if n < 0 {
						n = len(buf)
					}
					if s := string(buf[:n]); s != "" {
						out = append(out, s)
					}
				}
				if len(out) > 0 {
					return out
				}
			}
		}
	}
	return posixPreferredLanguages()
}
