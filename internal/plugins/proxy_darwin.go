//go:build darwin

package plugins

import (
	"bytes"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	cfProxyStringEncodingUTF8 = 0x08000100
	cfProxyNumberSInt32Type   = 3
	darwinProxyCacheTTL       = 3 * time.Second
)

type darwinProxySnapshot struct {
	httpProxy              *url.URL
	httpsProxy             *url.URL
	socksProxy             *url.URL
	excludeSimpleHostnames bool
	exceptions             []string
}

var (
	macProxyOnce  sync.Once
	macProxyReady bool

	cfCopySystemProxySettings func() uintptr
	cfProxyStrCreateWithCStr  func(uintptr, *byte, uint32) uintptr
	cfProxyDictGetValue       func(uintptr, uintptr) uintptr
	cfProxyStrGetCStr         func(uintptr, *byte, int, uint32) byte
	cfProxyNumGetValue        func(uintptr, int, unsafe.Pointer) byte
	cfProxyBoolGetValue       func(uintptr) byte
	cfProxyArrGetCount        func(uintptr) int
	cfProxyArrGetValueAt      func(uintptr, int) uintptr
	cfProxyGetTypeID          func(uintptr) uintptr
	cfProxyStrGetTypeID       func() uintptr
	cfProxyNumGetTypeID       func() uintptr
	cfProxyBoolGetTypeID      func() uintptr
	cfProxyArrGetTypeID       func() uintptr
	cfProxyDictGetTypeID      func() uintptr
	cfProxyRelease            func(uintptr)

	cfTypeString uintptr
	cfTypeNumber uintptr
	cfTypeBool   uintptr
	cfTypeArray  uintptr
	cfTypeDict   uintptr

	cfKeyHTTPEnable     uintptr
	cfKeyHTTPProxy      uintptr
	cfKeyHTTPPort       uintptr
	cfKeyHTTPSEnable    uintptr
	cfKeyHTTPSProxy     uintptr
	cfKeyHTTPSPort      uintptr
	cfKeySOCKSEnable    uintptr
	cfKeySOCKSProxy     uintptr
	cfKeySOCKSPort      uintptr
	cfKeyExceptionsList uintptr
	cfKeyExcludeSimple  uintptr

	darwinProxyMu       sync.Mutex
	darwinProxyCachedAt time.Time
	darwinProxyCached   darwinProxySnapshot
)

func initMacProxy() {
	macProxyOnce.Do(func() {
		cfLib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		cfnLib, err := purego.Dlopen("/System/Library/Frameworks/CFNetwork.framework/CFNetwork", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			_ = purego.Dlclose(cfLib)
			return
		}
		ready := false
		defer func() {
			if !ready {
				_ = purego.Dlclose(cfnLib)
				_ = purego.Dlclose(cfLib)
			}
		}()

		sCopy, e0 := purego.Dlsym(cfnLib, "CFNetworkCopySystemProxySettings")
		sStrCreate, e1 := purego.Dlsym(cfLib, "CFStringCreateWithCString")
		sDictGet, e2 := purego.Dlsym(cfLib, "CFDictionaryGetValue")
		sStrGetCStr, e3 := purego.Dlsym(cfLib, "CFStringGetCString")
		sNumGet, e4 := purego.Dlsym(cfLib, "CFNumberGetValue")
		sBoolGet, e5 := purego.Dlsym(cfLib, "CFBooleanGetValue")
		sArrCount, e6 := purego.Dlsym(cfLib, "CFArrayGetCount")
		sArrAt, e7 := purego.Dlsym(cfLib, "CFArrayGetValueAtIndex")
		sGetType, e8 := purego.Dlsym(cfLib, "CFGetTypeID")
		sStrType, e9 := purego.Dlsym(cfLib, "CFStringGetTypeID")
		sNumType, e10 := purego.Dlsym(cfLib, "CFNumberGetTypeID")
		sBoolType, e11 := purego.Dlsym(cfLib, "CFBooleanGetTypeID")
		sArrType, e12 := purego.Dlsym(cfLib, "CFArrayGetTypeID")
		sDictType, e13 := purego.Dlsym(cfLib, "CFDictionaryGetTypeID")
		sRel, e14 := purego.Dlsym(cfLib, "CFRelease")
		for _, symErr := range []error{e0, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, e13, e14} {
			if symErr != nil {
				return
			}
		}

		purego.RegisterFunc(&cfCopySystemProxySettings, sCopy)
		purego.RegisterFunc(&cfProxyStrCreateWithCStr, sStrCreate)
		purego.RegisterFunc(&cfProxyDictGetValue, sDictGet)
		purego.RegisterFunc(&cfProxyStrGetCStr, sStrGetCStr)
		purego.RegisterFunc(&cfProxyNumGetValue, sNumGet)
		purego.RegisterFunc(&cfProxyBoolGetValue, sBoolGet)
		purego.RegisterFunc(&cfProxyArrGetCount, sArrCount)
		purego.RegisterFunc(&cfProxyArrGetValueAt, sArrAt)
		purego.RegisterFunc(&cfProxyGetTypeID, sGetType)
		purego.RegisterFunc(&cfProxyStrGetTypeID, sStrType)
		purego.RegisterFunc(&cfProxyNumGetTypeID, sNumType)
		purego.RegisterFunc(&cfProxyBoolGetTypeID, sBoolType)
		purego.RegisterFunc(&cfProxyArrGetTypeID, sArrType)
		purego.RegisterFunc(&cfProxyDictGetTypeID, sDictType)
		purego.RegisterFunc(&cfProxyRelease, sRel)

		cfTypeString = cfProxyStrGetTypeID()
		cfTypeNumber = cfProxyNumGetTypeID()
		cfTypeBool = cfProxyBoolGetTypeID()
		cfTypeArray = cfProxyArrGetTypeID()
		cfTypeDict = cfProxyDictGetTypeID()

		cfKeyHTTPEnable = makeCFString("HTTPEnable")
		cfKeyHTTPProxy = makeCFString("HTTPProxy")
		cfKeyHTTPPort = makeCFString("HTTPPort")
		cfKeyHTTPSEnable = makeCFString("HTTPSEnable")
		cfKeyHTTPSProxy = makeCFString("HTTPSProxy")
		cfKeyHTTPSPort = makeCFString("HTTPSPort")
		cfKeySOCKSEnable = makeCFString("SOCKSEnable")
		cfKeySOCKSProxy = makeCFString("SOCKSProxy")
		cfKeySOCKSPort = makeCFString("SOCKSPort")
		cfKeyExceptionsList = makeCFString("ExceptionsList")
		cfKeyExcludeSimple = makeCFString("ExcludeSimpleHostnames")

		for _, k := range []uintptr{
			cfKeyHTTPEnable, cfKeyHTTPProxy, cfKeyHTTPPort,
			cfKeyHTTPSEnable, cfKeyHTTPSProxy, cfKeyHTTPSPort,
			cfKeySOCKSEnable, cfKeySOCKSProxy, cfKeySOCKSPort,
			cfKeyExceptionsList, cfKeyExcludeSimple,
		} {
			if k == 0 {
				return
			}
		}
		ready = true
		macProxyReady = true
	})
}

func makeCFString(s string) uintptr {
	b := append([]byte(s), 0)
	return cfProxyStrCreateWithCStr(0, &b[0], cfProxyStringEncodingUTF8)
}

func readCFInt(dict, key uintptr) (int, bool) {
	if dict == 0 || key == 0 {
		return 0, false
	}
	val := cfProxyDictGetValue(dict, key)
	if val == 0 {
		return 0, false
	}
	switch cfProxyGetTypeID(val) {
	case cfTypeNumber:
		var n int32
		if cfProxyNumGetValue(val, cfProxyNumberSInt32Type, unsafe.Pointer(&n)) == 0 {
			return 0, false
		}
		return int(n), true
	case cfTypeBool:
		if cfProxyBoolGetValue(val) != 0 {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func readCFStringVal(val uintptr) (string, bool) {
	if val == 0 || cfProxyGetTypeID(val) != cfTypeString {
		return "", false
	}
	var buf [512]byte
	if cfProxyStrGetCStr(val, &buf[0], len(buf), cfProxyStringEncodingUTF8) == 0 {
		return "", false
	}
	n := bytes.IndexByte(buf[:], 0)
	if n < 0 {
		n = len(buf)
	}
	return string(buf[:n]), true
}

func readCFProxyEndpoint(dict, enableKey, hostKey, portKey uintptr, scheme string) *url.URL {
	enabled, ok := readCFInt(dict, enableKey)
	if !ok || enabled == 0 {
		return nil
	}
	host, ok := readCFStringVal(cfProxyDictGetValue(dict, hostKey))
	if !ok {
		return nil
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	port, ok := readCFInt(dict, portKey)
	if !ok || port <= 0 || port > 65535 {
		return nil
	}
	return &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
}

func loadDarwinProxySnapshot() darwinProxySnapshot {
	initMacProxy()
	if !macProxyReady {
		return darwinProxySnapshot{}
	}

	now := time.Now()
	darwinProxyMu.Lock()
	defer darwinProxyMu.Unlock()
	if !darwinProxyCachedAt.IsZero() && now.Sub(darwinProxyCachedAt) < darwinProxyCacheTTL {
		return darwinProxyCached
	}

	snap := darwinProxySnapshot{}
	dict := cfCopySystemProxySettings()
	if dict != 0 {
		defer cfProxyRelease(dict)
		if cfProxyGetTypeID(dict) == cfTypeDict {
			snap.httpProxy = readCFProxyEndpoint(dict, cfKeyHTTPEnable, cfKeyHTTPProxy, cfKeyHTTPPort, "http")
			snap.httpsProxy = readCFProxyEndpoint(dict, cfKeyHTTPSEnable, cfKeyHTTPSProxy, cfKeyHTTPSPort, "http")
			snap.socksProxy = readCFProxyEndpoint(dict, cfKeySOCKSEnable, cfKeySOCKSProxy, cfKeySOCKSPort, "socks5")
			if v, ok := readCFInt(dict, cfKeyExcludeSimple); ok && v != 0 {
				snap.excludeSimpleHostnames = true
			}
			if arr := cfProxyDictGetValue(dict, cfKeyExceptionsList); arr != 0 && cfProxyGetTypeID(arr) == cfTypeArray {
				count := cfProxyArrGetCount(arr)
				if count > 0 {
					snap.exceptions = make([]string, 0, count)
					for i := 0; i < count; i++ {
						item := cfProxyArrGetValueAt(arr, i)
						if s, ok := readCFStringVal(item); ok {
							s = strings.ToLower(strings.TrimSpace(s))
							if s != "" {
								snap.exceptions = append(snap.exceptions, s)
							}
						}
					}
				}
			}
		}
	}

	darwinProxyCached = snap
	darwinProxyCachedAt = now
	return snap
}

func parseProxyExceptionPrefix(pat string) (netip.Prefix, bool) {
	slash := strings.IndexByte(pat, '/')
	if slash <= 0 {
		return netip.Prefix{}, false
	}
	if p, err := netip.ParsePrefix(pat); err == nil {
		return p, true
	}
	// macOS ExceptionsList sometimes omits trailing ".0" octets (e.g. "169.254/16").
	ipPart := pat[:slash]
	bitsPart := pat[slash:]
	if !strings.Contains(ipPart, ":") {
		parts := strings.Split(ipPart, ".")
		if len(parts) >= 1 && len(parts) < 4 {
			for len(parts) < 4 {
				parts = append(parts, "0")
			}
			if p, err := netip.ParsePrefix(strings.Join(parts, ".") + bitsPart); err == nil {
				return p, true
			}
		}
	}
	return netip.Prefix{}, false
}

func matchesDarwinProxyException(host string, exceptions []string, excludeSimple bool) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return false
	}
	if isLocalhostDomain(h) {
		return true
	}
	addr, addrErr := netip.ParseAddr(h)
	if addrErr == nil && addr.Unmap().IsLoopback() {
		return true
	}
	if excludeSimple && addrErr != nil && !strings.Contains(h, ".") {
		return true
	}
	for _, rawPat := range exceptions {
		pat := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawPat), "."))
		if pat == "" {
			continue
		}
		if prefix, ok := parseProxyExceptionPrefix(pat); ok {
			if addrErr == nil && prefix.Contains(addr.Unmap()) {
				return true
			}
			continue
		}
		if strings.HasPrefix(pat, "*.") {
			suffix := pat[1:]
			if strings.HasSuffix(h, suffix) && len(h) > len(suffix) {
				return true
			}
			continue
		}
		if strings.HasPrefix(pat, ".") {
			if strings.HasSuffix(h, pat) && len(h) > len(pat) {
				return true
			}
			continue
		}
		if h == pat {
			return true
		}
		if addrErr == nil {
			if patAddr, perr := netip.ParseAddr(pat); perr == nil && patAddr.Unmap() == addr.Unmap() {
				return true
			}
		}
	}
	return false
}

func systemProxyForRequest(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	snap := loadDarwinProxySnapshot()
	if matchesDarwinProxyException(u.Hostname(), snap.exceptions, snap.excludeSimpleHostnames) {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if snap.httpsProxy != nil {
			return snap.httpsProxy
		}
	case "http":
		if snap.httpProxy != nil {
			return snap.httpProxy
		}
	}
	return snap.socksProxy
}
