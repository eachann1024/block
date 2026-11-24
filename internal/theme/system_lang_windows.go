//go:build windows

package theme

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const muiLanguageName = 0x8

var (
	langKernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetUserPreferredUILanguages   = langKernel32.NewProc("GetUserPreferredUILanguages")
	procThemeGetUserDefaultUILanguage = langKernel32.NewProc("GetUserDefaultUILanguage")
)

func platformPreferredLanguages() []string {
	if langs := windowsPreferredUILanguages(); len(langs) > 0 {
		return langs
	}
	if tag := windowsDefaultUILanguageTag(); tag != "" {
		return []string{tag}
	}
	return posixPreferredLanguages()
}

func windowsPreferredUILanguages() []string {
	var numLangs uint32
	var bufSize uint32
	ok, _, _ := procGetUserPreferredUILanguages.Call(
		muiLanguageName,
		uintptr(unsafe.Pointer(&numLangs)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ok == 0 || bufSize == 0 {
		return nil
	}
	buf := make([]uint16, bufSize)
	ok, _, _ = procGetUserPreferredUILanguages.Call(
		muiLanguageName,
		uintptr(unsafe.Pointer(&numLangs)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if ok == 0 {
		return nil
	}
	var out []string
	start := 0
	for i, r := range buf {
		if r == 0 {
			if i == start {
				break
			}
			out = append(out, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	return out
}

func windowsDefaultUILanguageTag() string {
	lang, _, _ := procThemeGetUserDefaultUILanguage.Call()
	if lang == 0 {
		return ""
	}
	primary := lang & 0x3ff
	sub := (lang >> 10) & 0x3f
	switch primary {
	case 0x04: // LANG_CHINESE
		// SUBLANG_CHINESE_TRADITIONAL (0x01), HONGKONG (0x03), MACAU (0x05)
		if sub == 0x01 || sub == 0x03 || sub == 0x05 {
			return "zh-Hant"
		}
		return "zh-Hans"
	case 0x09:
		return "en"
	case 0x11:
		return "ja"
	case 0x12:
		return "ko"
	case 0x0c:
		return "fr"
	case 0x07:
		return "de"
	case 0x0a:
		return "es"
	case 0x16:
		return "pt"
	case 0x19:
		return "ru"
	case 0x10:
		return "it"
	default:
		return "en"
	}
}
