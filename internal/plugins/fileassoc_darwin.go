//go:build darwin

package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"block/internal/updater"
	"github.com/ebitengine/purego"
)

const (
	macAssocHelperBundleID       = "dev.eachann.block.associations"
	macAssocHelperBundleIDPrefix = "dev.eachann.block.associations."
	macAssocHelperExecName       = "block-opener"
	macLSRegisterPath            = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	macCodesignPath              = "/usr/bin/codesign"
	macPlutilPath                = "/usr/bin/plutil"
	macDefaultMainAppPath        = "/Applications/Block.app"
	kCFStringEncodingUTF8        = 0x08000100
	kLSRolesViewer               = 0x00000002
	kLSRolesEditor               = 0x00000004
	kLSRolesAll                  = 0xFFFFFFFF
)

var (
	macUTOnce                sync.Once
	macUTReady               bool
	macLSDefaultReady        bool
	cfStrCreate              func(alloc uintptr, cstr string, encoding uint32) uintptr
	cfStrGetCString          func(str uintptr, buf *byte, bufSize int, encoding uint32) uint8
	cfReleaseFn              func(cf uintptr)
	cfArrayGetCount          func(arr uintptr) int
	cfArrayGetValAt          func(arr uintptr, idx int) uintptr
	utCreateAllTagsFn        func(inTagClass, inTag, inConformingToUTI uintptr) uintptr
	utConformsFn             func(inUTI, inConformsToUTI uintptr) uint8
	lsCopyDefaultRoleHandler func(inContentType uintptr, inRole uint32) uintptr

	macReconciledOnce atomic.Bool
	macLSRegSuffixRe  = regexp.MustCompile(`\s+\(0x[0-9a-fA-F]+\)\s*$`)
)

func initMacUTType() {
	macUTOnce.Do(func() {
		cfLib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		csLib, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		s1, err1 := purego.Dlsym(cfLib, "CFStringCreateWithCString")
		s2, err2 := purego.Dlsym(cfLib, "CFRelease")
		s3, err3 := purego.Dlsym(cfLib, "CFArrayGetCount")
		s4, err4 := purego.Dlsym(cfLib, "CFArrayGetValueAtIndex")
		s5, err5 := purego.Dlsym(csLib, "UTTypeCreateAllIdentifiersForTag")
		s6, err6 := purego.Dlsym(csLib, "UTTypeConformsTo")
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			return
		}
		purego.RegisterFunc(&cfStrCreate, s1)
		purego.RegisterFunc(&cfReleaseFn, s2)
		purego.RegisterFunc(&cfArrayGetCount, s3)
		purego.RegisterFunc(&cfArrayGetValAt, s4)
		purego.RegisterFunc(&utCreateAllTagsFn, s5)
		purego.RegisterFunc(&utConformsFn, s6)
		macUTReady = true

		if s7, err7 := purego.Dlsym(cfLib, "CFStringGetCString"); err7 == nil {
			if s8, err8 := purego.Dlsym(csLib, "LSCopyDefaultRoleHandlerForContentType"); err8 == nil {
				purego.RegisterFunc(&cfStrGetCString, s7)
				purego.RegisterFunc(&lsCopyDefaultRoleHandler, s8)
				macLSDefaultReady = true
			}
		}
	})
}

func cfStringToGo(cfStr uintptr) string {
	if cfStr == 0 || cfStrGetCString == nil {
		return ""
	}
	var buf [512]byte
	if cfStrGetCString(cfStr, &buf[0], len(buf), kCFStringEncodingUTF8) == 0 {
		return ""
	}
	n := bytes.IndexByte(buf[:], 0)
	if n < 0 {
		n = len(buf)
	}
	return string(buf[:n])
}

// parseMacLSHandlersReferencedByBundle parses an XML LaunchServices plist
// (com.apple.launchservices.secure.plist) and returns any bare filename
// extensions whose default handler matches bundleID, plus whether any
// UTI-level (LSHandlerContentType) default references bundleID.
func parseMacLSHandlersReferencedByBundle(xmlData []byte, bundleID string) (exts []string, hasUTIRef bool) {
	target := strings.ToLower(strings.TrimSpace(bundleID))
	if target == "" || len(xmlData) == 0 {
		return nil, false
	}
	dec := xml.NewDecoder(bytes.NewReader(xmlData))
	type frame struct {
		kind        string
		lastKey     string
		inHandlers  bool
		isEntryDict bool
		entry       map[string]string
	}
	var stack []frame
	seen := make(map[string]struct{})

	clearParentDictKey := func() {
		if len(stack) > 0 && stack[len(stack)-1].kind == "dict" {
			stack[len(stack)-1].lastKey = ""
		}
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				isEntry := len(stack) > 0 && stack[len(stack)-1].kind == "array" && stack[len(stack)-1].inHandlers
				fr := frame{kind: "dict", isEntryDict: isEntry}
				if isEntry {
					fr.entry = make(map[string]string)
				}
				stack = append(stack, fr)
			case "array":
				inHandlers := len(stack) > 0 && stack[len(stack)-1].kind == "dict" && stack[len(stack)-1].lastKey == "LSHandlers"
				stack = append(stack, frame{kind: "array", inHandlers: inHandlers})
			case "key":
				var key string
				if err := dec.DecodeElement(&key, &t); err == nil && len(stack) > 0 && stack[len(stack)-1].kind == "dict" {
					stack[len(stack)-1].lastKey = strings.TrimSpace(key)
				}
			case "string":
				var val string
				if err := dec.DecodeElement(&val, &t); err == nil {
					if len(stack) > 0 && stack[len(stack)-1].kind == "dict" && stack[len(stack)-1].isEntryDict {
						k := stack[len(stack)-1].lastKey
						if k != "" {
							stack[len(stack)-1].entry[k] = strings.TrimSpace(val)
						}
					}
					clearParentDictKey()
				}
			case "true", "false", "integer", "real", "data", "date":
				_ = dec.Skip()
				clearParentDictKey()
			}
		case xml.EndElement:
			if (t.Name.Local == "dict" || t.Name.Local == "array") && len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.isEntryDict && len(top.entry) > 0 {
					matched := false
					for _, roleKey := range []string{"LSHandlerRoleAll", "LSHandlerRoleEditor", "LSHandlerRoleViewer"} {
						if strings.ToLower(top.entry[roleKey]) == target {
							matched = true
							break
						}
					}
					if matched {
						tag := strings.ToLower(strings.TrimPrefix(top.entry["LSHandlerContentTag"], "."))
						tagClass := top.entry["LSHandlerContentTagClass"]
						if tag != "" && (tagClass == "" || tagClass == "public.filename-extension") && bareAssocExtRe.MatchString(tag) {
							if _, dup := seen[tag]; !dup {
								seen[tag] = struct{}{}
								exts = append(exts, tag)
							}
						} else if top.entry["LSHandlerContentType"] != "" {
							hasUTIRef = true
						}
					}
				}
				clearParentDictKey()
			}
		}
	}
	sort.Strings(exts)
	return exts, hasUTIRef
}

// macExtensionDefaultUsesBundle queries live LaunchServices via CoreServices
// to see if any UTI for bare extension currently defaults to bundleID.
func macExtensionDefaultUsesBundle(bareExt, bundleID string) bool {
	initMacUTType()
	if !macUTReady || !macLSDefaultReady || bareExt == "" || bundleID == "" {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(bundleID))
	tagClass := cfStrCreate(0, "public.filename-extension", kCFStringEncodingUTF8)
	if tagClass == 0 {
		return false
	}
	defer cfReleaseFn(tagClass)
	tag := cfStrCreate(0, bareExt, kCFStringEncodingUTF8)
	if tag == 0 {
		return false
	}
	defer cfReleaseFn(tag)
	arr := utCreateAllTagsFn(tagClass, tag, 0)
	if arr == 0 {
		return false
	}
	defer cfReleaseFn(arr)

	roles := []uint32{kLSRolesAll, kLSRolesEditor, kLSRolesViewer}
	count := cfArrayGetCount(arr)
	for i := 0; i < count; i++ {
		uti := cfArrayGetValAt(arr, i)
		if uti == 0 {
			continue
		}
		for _, role := range roles {
			handler := lsCopyDefaultRoleHandler(uti, role)
			if handler == 0 {
				continue
			}
			id := strings.ToLower(strings.TrimSpace(cfStringToGo(handler)))
			cfReleaseFn(handler)
			if id == target {
				return true
			}
		}
	}
	return false
}

// macHelperReferencedDefaultExtensions reports extensions (and whether any
// UTI-level default) currently point to bundleID in macOS LaunchServices user
// preferences or live LaunchServices state.
func macHelperReferencedDefaultExtensions(bundleID string, prevExts []string, managedExts []string) (exts []string, hasUTIRef bool) {
	if strings.TrimSpace(bundleID) == "" {
		return nil, false
	}
	seen := make(map[string]struct{})
	addExt := func(bare string) {
		bare = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(bare), "."))
		if bare == "" || !bareAssocExtRe.MatchString(bare) {
			return
		}
		if _, dup := seen[bare]; !dup {
			seen[bare] = struct{}{}
			exts = append(exts, bare)
		}
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		plistPaths := []string{
			filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist"),
			filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices.plist"),
		}
		for _, p := range plistPaths {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, macPlutilPath, "-convert", "xml1", "-o", "-", p).Output()
			cancel()
			if err != nil {
				continue
			}
			foundExts, utiRef := parseMacLSHandlersReferencedByBundle(out, bundleID)
			for _, e := range foundExts {
				addExt(e)
			}
			if utiRef {
				hasUTIRef = true
			}
		}
	}

	// Also check live LaunchServices defaults for previously helper-managed and
	// currently managed extensions in case LaunchServices has not flushed prefs.
	checked := make(map[string]struct{})
	checkCandidate := func(raw string) {
		bare := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "."))
		if bare == "" || !bareAssocExtRe.MatchString(bare) {
			return
		}
		if _, done := checked[bare]; done {
			return
		}
		checked[bare] = struct{}{}
		if macExtensionDefaultUsesBundle(bare, bundleID) {
			addExt(bare)
		}
	}
	for _, e := range prevExts {
		checkCandidate(e)
	}
	for _, e := range managedExts {
		if isSingleSegmentExtension(e) && isSafeAssocExtension(e) {
			checkCandidate(e)
		}
	}

	sort.Strings(exts)
	return exts, hasUTIRef
}

func macBuiltinFallbackHandled(ext string) bool {
	for _, md := range builtinMarkdownExtensions() {
		if ext == md {
			return true
		}
	}
	return ext == ".txt" || ext == ".text"
}

// macExtensionHandledByMainApp checks all UTIs associated with ext via
// UTTypeCreateAllIdentifiersForTag; if any conforms to public.plain-text or
// Markdown, main Block.app already appears in Finder's "Open With" menu.
func macExtensionHandledByMainApp(ext string) bool {
	if macBuiltinFallbackHandled(ext) {
		return true
	}
	initMacUTType()
	if !macUTReady {
		return false
	}
	bare := strings.TrimPrefix(ext, ".")
	if bare == "" {
		return true
	}

	tagClass := cfStrCreate(0, "public.filename-extension", kCFStringEncodingUTF8)
	if tagClass == 0 {
		return false
	}
	defer cfReleaseFn(tagClass)

	tag := cfStrCreate(0, bare, kCFStringEncodingUTF8)
	if tag == 0 {
		return false
	}
	defer cfReleaseFn(tag)

	arr := utCreateAllTagsFn(tagClass, tag, 0)
	if arr == 0 {
		return false
	}
	defer cfReleaseFn(arr)

	count := cfArrayGetCount(arr)
	if count <= 0 {
		return false
	}

	baseNames := []string{"public.plain-text", "net.daringfireball.markdown", "public.markdown"}
	bases := make([]uintptr, 0, len(baseNames))
	for _, name := range baseNames {
		if b := cfStrCreate(0, name, kCFStringEncodingUTF8); b != 0 {
			bases = append(bases, b)
		}
	}
	defer func() {
		for _, b := range bases {
			cfReleaseFn(b)
		}
	}()

	for i := 0; i < count; i++ {
		uti := cfArrayGetValAt(arr, i)
		if uti == 0 {
			continue
		}
		for _, base := range bases {
			if utConformsFn(uti, base) != 0 {
				return true
			}
		}
	}
	return false
}

func findEnclosingMacAppBundle(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 6; i++ {
		if strings.HasSuffix(strings.ToLower(dir), ".app") {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if st, err := os.Stat(macDefaultMainAppPath); err == nil && st.IsDir() {
		return macDefaultMainAppPath
	}
	return ""
}

func canonicalMacPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil && abs != "" {
		p = abs
	}
	if realPath, err := filepath.EvalSymlinks(p); err == nil && realPath != "" {
		p = realPath
	}
	return filepath.Clean(p)
}

// macInstallScopeIdentity derives a stable per-installation helper Bundle ID
// and directory key from the canonical main Block.app bundle (or standalone
// executable) so coexisting macOS installs sharing ~/Library/Application Support/Block
// never overwrite each other's BlockMainAppPath or default handler target.
func macInstallScopeIdentity(mainAppPath, exePath string) (scopeKey, bundleID, target string) {
	target = canonicalMacPath(mainAppPath)
	if target == "" {
		target = canonicalMacPath(exePath)
	}
	if target == "" {
		target = macDefaultMainAppPath
	}
	scopeKey = stableInstallScopeHash(target)
	bundleID = macAssocHelperBundleIDPrefix + scopeKey
	return scopeKey, bundleID, target
}

func macLegacyHelperBundlePath(appDataDir string) string {
	return filepath.Join(appDataDir, "Associations", "Block.app")
}

func macHelperBundlePath(appDataDir, scopeKey string) string {
	if strings.TrimSpace(scopeKey) == "" {
		return macLegacyHelperBundlePath(appDataDir)
	}
	return filepath.Join(appDataDir, "Associations", scopeKey, "Block.app")
}

func readMacPlistTopLevelString(bundlePath, targetKey string) string {
	if strings.TrimSpace(bundlePath) == "" || strings.TrimSpace(targetKey) == "" {
		return ""
	}
	plistPath := filepath.Join(bundlePath, "Contents", "Info.plist")
	raw, err := os.ReadFile(plistPath)
	if err != nil || len(raw) == 0 {
		return ""
	}
	if bytes.HasPrefix(raw, []byte("bplist")) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		xmlOut, convErr := exec.CommandContext(ctx, macPlutilPath, "-convert", "xml1", "-o", "-", plistPath).Output()
		if convErr != nil || len(xmlOut) == 0 {
			return ""
		}
		raw = xmlOut
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				depth++
			case "key":
				var k string
				if dec.DecodeElement(&k, &t) == nil && depth == 1 {
					lastKey = strings.TrimSpace(k)
				}
			case "string":
				var v string
				if dec.DecodeElement(&v, &t) == nil {
					if depth == 1 && lastKey == targetKey {
						return strings.TrimSpace(v)
					}
					if depth == 1 {
						lastKey = ""
					}
				}
			case "array", "true", "false", "integer", "real", "data", "date":
				_ = dec.Skip()
				if depth == 1 {
					lastKey = ""
				}
			}
		case xml.EndElement:
			if t.Name.Local == "dict" && depth > 0 {
				depth--
			}
		}
	}
}

// macLegacyHelperOwnedOrOrphaned reports whether the legacy shared helper
// bundle (Associations/Block.app with dev.eachann.block.associations) belongs
// to the current installation or points to a removed Block.app. A legacy helper
// pointing to another live Block.app is never touched.
func macLegacyHelperOwnedOrOrphaned(legacyBundlePath, mainAppPath string) bool {
	if _, err := os.Stat(legacyBundlePath); err != nil {
		return false
	}
	if id := readMacPlistTopLevelString(legacyBundlePath, "CFBundleIdentifier"); id != macAssocHelperBundleID {
		return false
	}
	recordedMain := readMacPlistTopLevelString(legacyBundlePath, "BlockMainAppPath")
	if recordedMain == "" {
		return false
	}
	if mainAppPath != "" && canonicalMacPath(recordedMain) == canonicalMacPath(mainAppPath) {
		return true
	}
	if _, err := os.Stat(recordedMain); os.IsNotExist(err) {
		return true
	}
	return false
}

func loadMacScopeState(st fileAssocState, bundleID, mainAppPath, exePath string) (macAssocScopeState, bool) {
	if st.MacScopes != nil {
		if sc, ok := st.MacScopes[bundleID]; ok {
			return sc, true
		}
	}
	wantTarget := canonicalMacPath(mainAppPath)
	if wantTarget == "" {
		wantTarget = canonicalMacPath(exePath)
	}
	prevTarget := canonicalMacPath(st.AppPath)
	if prevTarget == "" {
		prevTarget = canonicalMacPath(st.ExecutablePath)
	}
	if prevTarget != "" && wantTarget != "" && prevTarget == wantTarget {
		return macAssocScopeState{
			BundleID:        bundleID,
			AppVersion:      st.AppVersion,
			AppPath:         st.AppPath,
			ExecutablePath:  st.ExecutablePath,
			ExecutableSize:  st.ExecutableSize,
			ExecutableMtime: st.ExecutableMtime,
			Extensions:      st.Extensions,
		}, true
	}
	return macAssocScopeState{}, false
}

func saveMacScopeState(appDataDir string, prev fileAssocState, bundleID string, sc macAssocScopeState) error {
	next := prev
	if next.MacScopes == nil {
		next.MacScopes = make(map[string]macAssocScopeState)
		prevTarget := canonicalMacPath(prev.AppPath)
		if prevTarget == "" {
			prevTarget = canonicalMacPath(prev.ExecutablePath)
		}
		curTarget := canonicalMacPath(sc.AppPath)
		if curTarget == "" {
			curTarget = canonicalMacPath(sc.ExecutablePath)
		}
		if prevTarget != "" && prevTarget != curTarget && len(prev.Extensions) > 0 {
			oldKey := stableInstallScopeHash(prevTarget)
			oldID := macAssocHelperBundleIDPrefix + oldKey
			if oldID != bundleID {
				next.MacScopes[oldID] = macAssocScopeState{
					BundleID:        oldID,
					HelperPath:      macLegacyHelperBundlePath(appDataDir),
					AppVersion:      prev.AppVersion,
					AppPath:         prev.AppPath,
					ExecutablePath:  prev.ExecutablePath,
					ExecutableSize:  prev.ExecutableSize,
					ExecutableMtime: prev.ExecutableMtime,
					Extensions:      prev.Extensions,
				}
			}
		}
	}
	sc.BundleID = bundleID
	next.MacScopes[bundleID] = sc
	next.AppVersion = sc.AppVersion
	next.AppPath = sc.AppPath
	next.ExecutablePath = sc.ExecutablePath
	next.ExecutableSize = sc.ExecutableSize
	next.ExecutableMtime = sc.ExecutableMtime
	next.Extensions = sc.Extensions
	return saveFileAssocState(appDataDir, next)
}

// pruneStaleMacHelperRegistrations unregisters any LaunchServices records for
// bundleID pointing to paths other than canonicalHelper.
func pruneStaleMacHelperRegistrations(bundleID, canonicalHelper string) {
	if strings.TrimSpace(bundleID) == "" {
		return
	}
	canonical := ""
	if canonicalHelper != "" {
		canonical = canonicalMacPath(canonicalHelper)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, macLSRegisterPath, "-dump")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}

	seen := make(map[string]struct{})
	var stale []string
	var curPath, curID string
	flush := func() {
		if curPath != "" && curID == bundleID {
			clean := canonicalMacPath(curPath)
			if canonical == "" || clean != canonical {
				if _, dup := seen[curPath]; !dup {
					seen[curPath] = struct{}{}
					stale = append(stale, curPath)
				}
			}
		}
		curPath = ""
		curID = ""
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "--------------------------------------------------------------------------------") {
			flush()
			continue
		}
		if curPath == "" && strings.HasPrefix(line, "path:") {
			curPath = strings.TrimSpace(macLSRegSuffixRe.ReplaceAllString(line[len("path:"):], ""))
		} else if curID == "" && strings.HasPrefix(line, "identifier:") {
			curID = strings.TrimSpace(macLSRegSuffixRe.ReplaceAllString(line[len("identifier:"):], ""))
		}
	}
	flush()
	_ = cmd.Wait()

	for _, p := range stale {
		_ = runCommandTimeout(5*time.Second, macLSRegisterPath, "-u", p)
	}
}

func cleanupPlatformFileAssociations(appDataDir string, isolated bool) error {
	exePath, exeErr := os.Executable()
	if exeErr == nil {
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
			exePath = resolved
		}
	}
	mainAppPath := ""
	if exeErr == nil {
		mainAppPath = findEnclosingMacAppBundle(exePath)
	}
	scopeKey, helperBundleID, _ := macInstallScopeIdentity(mainAppPath, exePath)
	helperApp := macHelperBundlePath(appDataDir, scopeKey)
	_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
	if !isolated {
		pruneStaleMacHelperRegistrations(helperBundleID, "")
	}
	if err := os.RemoveAll(helperApp); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(helperApp))

	legacyHelperApp := macLegacyHelperBundlePath(appDataDir)
	if macLegacyHelperOwnedOrOrphaned(legacyHelperApp, mainAppPath) {
		_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", legacyHelperApp)
		if !isolated {
			pruneStaleMacHelperRegistrations(macAssocHelperBundleID, "")
		}
		_ = os.RemoveAll(legacyHelperApp)
	}

	prev, hasPrev := loadFileAssocState(appDataDir)
	if !hasPrev {
		return nil
	}
	if prev.MacScopes != nil {
		delete(prev.MacScopes, helperBundleID)
	}
	if len(prev.MacScopes) > 0 {
		keys := make([]string, 0, len(prev.MacScopes))
		for k := range prev.MacScopes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		keep := prev.MacScopes[keys[0]]
		prev.AppVersion = keep.AppVersion
		prev.AppPath = keep.AppPath
		prev.ExecutablePath = keep.ExecutablePath
		prev.ExecutableSize = keep.ExecutableSize
		prev.ExecutableMtime = keep.ExecutableMtime
		prev.Extensions = keep.Extensions
		return saveFileAssocState(appDataDir, prev)
	}
	_ = os.Remove(fileAssocStatePath(appDataDir))
	return nil
}

// readMacBundleDeclaredExtensions parses CFBundleDocumentTypes ->
// CFBundleTypeExtensions from bundlePath/Contents/Info.plist (supporting XML,
// binary bplist, and OpenStep plists via /usr/bin/plutil fallback) so the
// runtime helper bundle skips every extension already claimed by main
// Block.app (preventing duplicate "Block" entries in Finder's "Open With").
func readMacBundleDeclaredExtensions(bundlePath string) map[string]struct{} {
	if strings.TrimSpace(bundlePath) == "" {
		return nil
	}
	plistPath := filepath.Join(bundlePath, "Contents", "Info.plist")
	raw, err := os.ReadFile(plistPath)
	if err != nil || len(raw) == 0 {
		return nil
	}
	if !bytes.HasPrefix(raw, []byte("bplist")) {
		if exts, ok := parsePlistDocumentExtensionsStrict(raw); ok {
			return exts
		}
	}
	// Binary plist (bplist00) or non-XML plist: convert read-only to XML1 stdout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	xmlOut, convErr := exec.CommandContext(ctx, macPlutilPath, "-convert", "xml1", "-o", "-", plistPath).Output()
	if convErr != nil || len(xmlOut) == 0 {
		return nil
	}
	exts, _ := parsePlistDocumentExtensionsStrict(xmlOut)
	return exts
}

func parsePlistDocumentExtensions(raw []byte) map[string]struct{} {
	exts, _ := parsePlistDocumentExtensionsStrict(raw)
	return exts
}

func parsePlistDocumentExtensionsStrict(raw []byte) (map[string]struct{}, bool) {
	type frame struct {
		kind      string // "dict" or "array"
		lastKey   string
		inDocType bool
		inDocExts bool
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var stack []frame
	out := make(map[string]struct{})
	sawPlist := false
	clearParentDictKey := func() {
		if len(stack) > 0 && stack[len(stack)-1].kind == "dict" {
			stack[len(stack)-1].lastKey = ""
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return out, sawPlist
			}
			return nil, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "plist":
				sawPlist = true
			case "dict":
				inDocType := len(stack) > 0 && stack[len(stack)-1].inDocType
				stack = append(stack, frame{kind: "dict", inDocType: inDocType})
			case "array":
				inDocType := false
				inDocExts := false
				if len(stack) > 0 {
					parent := stack[len(stack)-1]
					if parent.kind == "dict" && parent.lastKey == "CFBundleDocumentTypes" {
						inDocType = true
					} else if parent.kind == "dict" && parent.inDocType && parent.lastKey == "CFBundleTypeExtensions" {
						inDocType = true
						inDocExts = true
					}
				}
				stack = append(stack, frame{kind: "array", inDocType: inDocType, inDocExts: inDocExts})
			case "key":
				var key string
				if err := dec.DecodeElement(&key, &t); err == nil && len(stack) > 0 && stack[len(stack)-1].kind == "dict" {
					stack[len(stack)-1].lastKey = strings.TrimSpace(key)
				}
			case "string":
				var val string
				if err := dec.DecodeElement(&val, &t); err == nil {
					if len(stack) > 0 && stack[len(stack)-1].kind == "array" && stack[len(stack)-1].inDocExts {
						bare := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(val), "."))
						if bare != "" {
							out[bare] = struct{}{}
						}
					} else {
						clearParentDictKey()
					}
				}
			case "true", "false", "integer", "real", "data", "date":
				_ = dec.Skip()
				clearParentDictKey()
			}
		case xml.EndElement:
			if (t.Name.Local == "dict" || t.Name.Local == "array") && len(stack) > 0 {
				stack = stack[:len(stack)-1]
				clearParentDictKey()
			}
		}
	}
}

func syncPlatformFileAssociations(appDataDir string, managedExts []string) error {
	exePath, exeErr := os.Executable()
	if exeErr == nil {
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
			exePath = resolved
		}
	}
	mainAppPath := ""
	if exeErr == nil {
		mainAppPath = findEnclosingMacAppBundle(exePath)
	} else if st, err := os.Stat(macDefaultMainAppPath); err == nil && st.IsDir() {
		mainAppPath = macDefaultMainAppPath
	}
	mainAppExts := readMacBundleDeclaredExtensions(mainAppPath)

	var bareExts []string
	for _, ext := range managedExts {
		if !isSingleSegmentExtension(ext) || !isSafeAssocExtension(ext) {
			continue
		}
		bare := strings.TrimPrefix(ext, ".")
		if bare == "" {
			continue
		}
		if _, handled := mainAppExts[bare]; handled {
			continue
		}
		if macExtensionHandledByMainApp(ext) {
			continue
		}
		bareExts = append(bareExts, bare)
	}
	sort.Strings(bareExts)

	scopeKey, helperBundleID, _ := macInstallScopeIdentity(mainAppPath, exePath)
	assocDir := filepath.Join(appDataDir, "Associations")
	scopeAssocDir := filepath.Join(assocDir, scopeKey)
	helperApp := macHelperBundlePath(appDataDir, scopeKey)
	prev, hasPrev := loadFileAssocState(appDataDir)
	scopePrev, hasScopePrev := loadMacScopeState(prev, helperBundleID, mainAppPath, exePath)
	firstRun := macReconciledOnce.CompareAndSwap(false, true)

	// Retire the legacy shared helper bundle (dev.eachann.block.associations)
	// ONLY if it belongs to this exact installation (or an already-removed app)
	// AND LaunchServices no longer references it as a user default. A legacy
	// helper owned by another live Block.app or still referenced as a default
	// is left intact so existing defaults keep forwarding to their original app.
	legacyHelperApp := macLegacyHelperBundlePath(appDataDir)
	if macLegacyHelperOwnedOrOrphaned(legacyHelperApp, mainAppPath) {
		legacyRefExts, legacyUTIRef := macHelperReferencedDefaultExtensions(macAssocHelperBundleID, prev.Extensions, managedExts)
		if len(legacyRefExts) == 0 && !legacyUTIRef {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", legacyHelperApp)
			_ = os.RemoveAll(legacyHelperApp)
			if firstRun {
				pruneStaleMacHelperRegistrations(macAssocHelperBundleID, "")
			}
		}
	}

	// Retain this installation's helper bundle (and its forwarding capability
	// to this installation's main Block.app) whenever LaunchServices still
	// references helperBundleID as the user's default handler for any extension
	// or UTI, even after main Block.app starts declaring those extensions directly.
	refExts, hasUTIRef := macHelperReferencedDefaultExtensions(helperBundleID, scopePrev.Extensions, managedExts)
	if len(refExts) > 0 {
		bareExts = unionSortedStrings(bareExts, refExts)
	}
	if len(bareExts) == 0 && hasUTIRef && len(scopePrev.Extensions) > 0 {
		bareExts = unionSortedStrings(scopePrev.Extensions)
	}

	if len(bareExts) == 0 && !hasUTIRef {
		if _, err := os.Stat(helperApp); err == nil {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
			if err := os.RemoveAll(helperApp); err != nil {
				return fmt.Errorf("remove helper bundle: %w", err)
			}
			_ = os.Remove(scopeAssocDir)
		} else if hasScopePrev && len(scopePrev.Extensions) > 0 {
			_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-u", helperApp)
		}
		if firstRun {
			pruneStaleMacHelperRegistrations(helperBundleID, "")
		}
		if hasScopePrev && (len(scopePrev.Extensions) > 0 || prev.Version != fileAssocStateVersion) {
			return saveMacScopeState(appDataDir, prev, helperBundleID, macAssocScopeState{
				BundleID:       helperBundleID,
				HelperPath:     helperApp,
				AppPath:        mainAppPath,
				ExecutablePath: exePath,
				Extensions:     []string{},
			})
		}
		return nil
	}

	if exeErr != nil {
		return fmt.Errorf("resolve executable: %w", exeErr)
	}
	exeInfo, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	appVer := normalizedMacBundleVersion(updater.CurrentVersion())

	helperExecPath := filepath.Join(helperApp, "Contents", "MacOS", macAssocHelperExecName)
	helperPlistPath := filepath.Join(helperApp, "Contents", "Info.plist")

	if hasPrev &&
		prev.Version == fileAssocStateVersion &&
		hasScopePrev &&
		scopePrev.BundleID == helperBundleID &&
		scopePrev.HelperPath == helperApp &&
		scopePrev.AppVersion == appVer &&
		scopePrev.AppPath == mainAppPath &&
		scopePrev.ExecutablePath == exePath &&
		scopePrev.ExecutableSize == exeInfo.Size() &&
		scopePrev.ExecutableMtime == exeInfo.ModTime().UnixNano() &&
		equalStringSlices(scopePrev.Extensions, bareExts) {
		if _, err1 := os.Stat(helperExecPath); err1 == nil {
			if _, err2 := os.Stat(helperPlistPath); err2 == nil {
				if firstRun {
					pruneStaleMacHelperRegistrations(helperBundleID, helperApp)
					_ = runCommandTimeout(10*time.Second, macLSRegisterPath, "-f", helperApp)
				}
				return nil
			}
		}
	}

	if err := os.MkdirAll(scopeAssocDir, 0o755); err != nil {
		return fmt.Errorf("create associations dir: %w", err)
	}
	_ = os.WriteFile(filepath.Join(assocDir, ".metadata_never_index"), nil, 0o644)

	stagingDir, err := os.MkdirTemp(scopeAssocDir, ".Block.staging-*")
	if err != nil {
		return fmt.Errorf("create helper staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	stagedApp := filepath.Join(stagingDir, "Block.app")
	if err := os.MkdirAll(filepath.Join(stagedApp, "Contents", "MacOS"), 0o755); err != nil {
		return fmt.Errorf("create staged macos dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(stagedApp, "Contents", "Resources"), 0o755); err != nil {
		return fmt.Errorf("create staged resources dir: %w", err)
	}

	stagedExec := filepath.Join(stagedApp, "Contents", "MacOS", macAssocHelperExecName)
	if err := cloneOrCopyFileAtomic(exePath, stagedExec, 0o755); err != nil {
		return fmt.Errorf("stage helper executable: %w", err)
	}

	if mainAppPath != "" {
		srcIcon := filepath.Join(mainAppPath, "Contents", "Resources", "AppIcon.icns")
		dstIcon := filepath.Join(stagedApp, "Contents", "Resources", "AppIcon.icns")
		if srcStat, err := os.Stat(srcIcon); err == nil {
			if copyErr := copyFileAtomic(srcIcon, dstIcon, 0o644); copyErr == nil {
				_ = os.Chtimes(dstIcon, srcStat.ModTime(), srcStat.ModTime())
			}
		}
	}

	plistBytes := buildMacHelperInfoPlist(helperBundleID, appVer, mainAppPath, bareExts)
	if err := writeFileAtomic(filepath.Join(stagedApp, "Contents", "Info.plist"), plistBytes, 0o644); err != nil {
		return fmt.Errorf("write staged Info.plist: %w", err)
	}

	if err := runCommandTimeout(10*time.Second, macCodesignPath,
		"--force", "--sign", "-",
		"--identifier", helperBundleID,
		stagedApp,
	); err != nil {
		return fmt.Errorf("codesign staged helper bundle: %w", err)
	}
	if err := runCommandTimeout(10*time.Second, macCodesignPath, "--verify", "--verbose=2", stagedApp); err != nil {
		return fmt.Errorf("verify staged helper bundle: %w", err)
	}

	backupApp := filepath.Join(stagingDir, "Block.previous.app")
	hadExisting := false
	if _, err := os.Stat(helperApp); err == nil {
		if err := os.Rename(helperApp, backupApp); err != nil {
			return fmt.Errorf("move previous helper aside: %w", err)
		}
		hadExisting = true
	}

	rollbackPublished := func() {
		_ = runCommandTimeout(5*time.Second, macLSRegisterPath, "-u", helperApp)
		_ = os.RemoveAll(helperApp)
		if hadExisting {
			if os.Rename(backupApp, helperApp) == nil {
				_ = runCommandTimeout(5*time.Second, macLSRegisterPath, "-f", helperApp)
			}
		} else {
			_ = os.Remove(scopeAssocDir)
		}
	}

	if err := os.Rename(stagedApp, helperApp); err != nil {
		if hadExisting {
			_ = os.Rename(backupApp, helperApp)
		}
		return fmt.Errorf("publish helper bundle: %w", err)
	}

	if err := runCommandTimeout(10*time.Second, macLSRegisterPath, "-f", helperApp); err != nil {
		rollbackPublished()
		return fmt.Errorf("lsregister helper bundle: %w", err)
	}

	if firstRun {
		pruneStaleMacHelperRegistrations(helperBundleID, helperApp)
	}

	if err := saveMacScopeState(appDataDir, prev, helperBundleID, macAssocScopeState{
		BundleID:        helperBundleID,
		HelperPath:      helperApp,
		AppVersion:      appVer,
		AppPath:         mainAppPath,
		ExecutablePath:  exePath,
		ExecutableSize:  exeInfo.Size(),
		ExecutableMtime: exeInfo.ModTime().UnixNano(),
		Extensions:      bareExts,
	}); err != nil {
		rollbackPublished()
		return fmt.Errorf("persist file association state: %w", err)
	}
	return nil
}

func normalizedMacBundleVersion(version string) string {
	ver := strings.TrimSpace(version)
	if ver == "" || ver == "dev" {
		return "1.0.0"
	}
	return ver
}

func buildMacHelperInfoPlist(bundleID, version, mainAppPath string, bareExts []string) []byte {
	if strings.TrimSpace(bundleID) == "" {
		bundleID = macAssocHelperBundleID
	}
	ver := normalizedMacBundleVersion(version)
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	writePlistKV(&b, "CFBundleName", "Block")
	writePlistKV(&b, "CFBundleDisplayName", "Block")
	writePlistKV(&b, "CFBundleIdentifier", bundleID)
	writePlistKV(&b, "CFBundleExecutable", macAssocHelperExecName)
	writePlistKV(&b, "CFBundlePackageType", "APPL")
	writePlistKV(&b, "CFBundleIconFile", "AppIcon")
	writePlistKV(&b, "CFBundleVersion", ver)
	writePlistKV(&b, "CFBundleShortVersionString", ver)
	writePlistKV(&b, "LSMinimumSystemVersion", "12.0")
	b.WriteString("  <key>LSUIElement</key>\n  <true/>\n")
	if mainAppPath != "" {
		writePlistKV(&b, "BlockMainAppPath", mainAppPath)
	}
	if len(bareExts) > 0 {
		b.WriteString("  <key>CFBundleDocumentTypes</key>\n  <array>\n    <dict>\n")
		b.WriteString("      <key>CFBundleTypeName</key>\n      <string>Block Language Plugin Document</string>\n")
		b.WriteString("      <key>CFBundleTypeRole</key>\n      <string>Editor</string>\n")
		b.WriteString("      <key>LSHandlerRank</key>\n      <string>Alternate</string>\n")
		b.WriteString("      <key>CFBundleTypeExtensions</key>\n      <array>\n")
		for _, ext := range bareExts {
			b.WriteString("        <string>")
			_ = xml.EscapeText(&b, []byte(ext))
			b.WriteString("</string>\n")
		}
		b.WriteString("      </array>\n    </dict>\n  </array>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

func writePlistKV(b *bytes.Buffer, key, value string) {
	b.WriteString("  <key>")
	_ = xml.EscapeText(b, []byte(key))
	b.WriteString("</key>\n  <string>")
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</string>\n")
}

func cloneOrCopyFileAtomic(src, dst string, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".block-clone-*.tmp")
	if err == nil {
		tmpPath := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if err := runCommandTimeout(5*time.Second, "/bin/cp", "-c", src, tmpPath); err == nil {
			if err := os.Chmod(tmpPath, mode); err == nil {
				if err := os.Rename(tmpPath, dst); err == nil {
					return nil
				}
			}
			_ = os.Remove(tmpPath)
		}
	}
	return copyFileAtomic(src, dst, mode)
}

func copyFileAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".block-copy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func runCommandTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s %v: %w (%s)", name, args, err, msg)
		}
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
