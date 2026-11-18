//go:build darwin

package theme

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var configureSmoothingOnce sync.Once

// ConfigureDefaultFontSmoothing registers AppleFontSmoothing = 0 in the process's
// volatile NSRegistrationDomain so Core Graphics renders crisp grayscale glyphs
// (matching -webkit-font-smoothing: antialiased) unless the user explicitly set
// AppleFontSmoothing on disk.
func ConfigureDefaultFontSmoothing() {
	configureSmoothingOnce.Do(func() {
		_, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		objcLib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		poolPushSym, err1 := purego.Dlsym(objcLib, "objc_autoreleasePoolPush")
		poolPopSym, err2 := purego.Dlsym(objcLib, "objc_autoreleasePoolPop")
		if err1 != nil || err2 != nil || poolPushSym == 0 || poolPopSym == 0 {
			return
		}

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		pool, _, _ := purego.SyscallN(poolPushSym)
		defer purego.SyscallN(poolPopSym, pool)

		nsStringCls := objc.ID(objc.GetClass("NSString"))
		nsNumberCls := objc.ID(objc.GetClass("NSNumber"))
		nsDictCls := objc.ID(objc.GetClass("NSDictionary"))
		nsDefaultsCls := objc.ID(objc.GetClass("NSUserDefaults"))
		if nsStringCls == 0 || nsNumberCls == 0 || nsDictCls == 0 || nsDefaultsCls == 0 {
			return
		}

		const (
			keyText              = "AppleFontSmoothing"
			nsUTF8StringEncoding = uintptr(4)
		)
		rawKey := nsStringCls.Send(objc.RegisterName("alloc"))
		if rawKey == 0 {
			return
		}
		key := rawKey.Send(
			objc.RegisterName("initWithBytes:length:encoding:"),
			uintptr(unsafe.Pointer(unsafe.StringData(keyText))),
			uintptr(len(keyText)),
			nsUTF8StringEncoding,
		)
		runtime.KeepAlive(keyText)
		if key == 0 {
			return
		}
		key.Send(objc.RegisterName("autorelease"))

		val := nsNumberCls.Send(objc.RegisterName("numberWithInt:"), 0)
		if val == 0 {
			return
		}
		dict := nsDictCls.Send(objc.RegisterName("dictionaryWithObject:forKey:"), val, key)
		if dict == 0 {
			return
		}
		defaults := nsDefaultsCls.Send(objc.RegisterName("standardUserDefaults"))
		if defaults != 0 {
			defaults.Send(objc.RegisterName("registerDefaults:"), dict)
		}
	})
}
