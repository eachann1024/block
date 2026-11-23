package editor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"block/internal/files"
	"block/internal/media"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	inlinePathLinkPrefix  = "\x00block-path:"
	inlinePathMaxBytes    = 4096
	inlinePathMaxLine     = 10_000_000
	maxExternalCopyBytes  = 256 * 1024 * 1024
	pathProbeTTL          = 5 * time.Second
	pathProbeMaxEntries   = 256
	pathProbeMaxInFlight  = 4
)

type pathOpenPosHost interface {
	OpenNoteAt(absPath string, lineHint, colHint int) error
}

type pathOpenClassifiedHost interface {
	OpenClassifiedAt(target model.VerifiedFileTarget, cf model.ClassifiedFile, lineHint, colHint int) error
}

type pathPrepareHost interface {
	PrepareClassifiedTarget(target model.VerifiedFileTarget) (model.PreparedTabTarget, error)
	ApplyPreparedClassifiedAt(prep model.PreparedTabTarget, cf model.ClassifiedFile, lineHint, colHint int) error
	PrepareRevealDirectory(absPath string) model.PreparedDirectoryTarget
	ApplyPreparedRevealDirectory(prep model.PreparedDirectoryTarget)
}

type pathAsyncHost interface {
	RunOnMain(fn func()) bool
}

type pathRevealHost interface {
	RevealPath(absPath string)
}

type pathRevealDirHost interface {
	RevealDirectory(absPath string)
}

type pathNotifyHost interface {
	NotifyPathError(msg string)
}

type inlinePathCandidate struct {
	start int
	end   int
	raw   string
}

type parsedPathRef struct {
	path      string
	rawPath   string
	line      int
	col       int
	isAbs     bool
	isHome    bool
	isFileURL bool
}

type pathProbeKey struct {
	raw      string
	noteDir  string
	projRoot string
}

type pathScopeAnchor struct {
	noteDir           string
	noteDirInfo       os.FileInfo
	projRoot          string
	projRootInfo      os.FileInfo
	noteBelongsToProj bool
}

type resolvedPathTarget struct {
	absPath         string
	confineRoot     string
	confineRel      string
	confineRootInfo os.FileInfo
	confineRootID   string
	line            int
	col             int
	isDir           bool
	exists          bool
	permErr         bool
}

type pathProbeEntry struct {
	target    resolvedPathTarget
	checkedAt time.Time
	pending   bool
}

type hoveredPathTooltip struct {
	rect    ui.Rect
	display string
	isDir   bool
}

func encodeInlinePathLink(raw string) string {
	return inlinePathLinkPrefix + raw
}

func decodeInlinePathLink(link string) (string, bool) {
	if !strings.HasPrefix(link, inlinePathLinkPrefix) {
		return "", false
	}
	return strings.TrimPrefix(link, inlinePathLinkPrefix), true
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isWindowsDrivePath(s string) bool {
	return len(s) >= 3 && isASCIIAlpha(s[0]) && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

func isUNCPath(s string) bool {
	return len(s) >= 3 && s[0] == '\\' && s[1] == '\\' && s[2] != '\\'
}

func hasDisallowedPathChars(s string) bool {
	if s == "" || len(s) > inlinePathMaxBytes {
		return true
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return true
	}
	return strings.ContainsAny(s, "*?<>|\"'`{}$;&\t\r\n")
}

func looksLikeSingleSegmentFileName(base string) bool {
	if base == "" || base == "." || base == ".." || base[0] == '~' || base[0] == '-' || strings.HasSuffix(base, ".") {
		return false
	}
	if strings.ContainsAny(base, `/\`) || strings.ContainsFunc(base, unicode.IsSpace) {
		return false
	}
	// Known dotfile form such as ".gitignore", ".env", ".editorconfig".
	if base[0] == '.' {
		return len(base) >= 2 && isASCIIAlpha(base[1])
	}
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot >= len(base)-1 {
		return false
	}
	ext := base[dot+1:]
	if len(ext) == 0 || len(ext) > 10 {
		return false
	}
	hasAlpha := false
	for i := 0; i < len(ext); i++ {
		b := ext[i]
		if isASCIIAlpha(b) {
			hasAlpha = true
		} else if b < '0' || b > '9' {
			return false
		}
	}
	return hasAlpha
}

func isURISchemeByte(b byte) bool {
	return isASCIIAlpha(b) || (b >= '0' && b <= '9') || b == '+' || b == '-' || b == '.'
}

func hasNonFileScheme(s string, isWindows bool) bool {
	if strings.Contains(s, "://") {
		return true
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "www.") {
		return true
	}
	// RFC 3986 scheme syntax: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ) ":"
	// occurring before any path separator. Single-letter Windows drive paths
	// (e.g. C:\foo or C:/foo) are excluded on Windows, and single-segment file
	// references with a :line[:col] suffix (e.g. README.md:12, main.go:42:5)
	// are excluded when the base has an explicit filename extension.
	colonIdx := strings.IndexByte(s, ':')
	if colonIdx <= 0 {
		return false
	}
	if sepIdx := strings.IndexAny(s, `/\`); sepIdx >= 0 && sepIdx < colonIdx {
		return false
	}
	if colonIdx == 1 && isWindows && isWindowsDrivePath(s) {
		return false
	}
	if base, ln, _ := splitPathColonLineSuffix(s); ln > 0 && looksLikeSingleSegmentFileName(base) {
		return false
	}
	if !isASCIIAlpha(s[0]) {
		return false
	}
	for i := 1; i < colonIdx; i++ {
		if !isURISchemeByte(s[i]) {
			return false
		}
	}
	return true
}

func parsePositiveBoundedInt(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > inlinePathMaxLine {
		return 0, false
	}
	return n, true
}

// parseLineFragment parses GitHub/editor line fragments like "L12", "L12-L20", "L12C5", "L12:5".
func parseLineFragment(frag string) (line, col int, ok bool) {
	if len(frag) < 2 || (frag[0] != 'L' && frag[0] != 'l') {
		return 0, 0, false
	}
	rest := frag[1:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, 0, false
	}
	ln, valid := parsePositiveBoundedInt(rest[:i])
	if !valid {
		return 0, 0, false
	}
	tail := rest[i:]
	if tail == "" {
		return ln, 0, true
	}
	if tail[0] == 'C' || tail[0] == 'c' || tail[0] == ':' {
		colStr := tail[1:]
		if cEnd := strings.IndexByte(colStr, '-'); cEnd >= 0 {
			colStr = colStr[:cEnd]
		}
		if c, ok := parsePositiveBoundedInt(colStr); ok {
			return ln, c, true
		}
		return 0, 0, false
	}
	if tail[0] == '-' {
		end := tail[1:]
		if len(end) >= 2 && (end[0] == 'L' || end[0] == 'l') {
			end = end[1:]
		}
		if _, ok := parsePositiveBoundedInt(end); ok {
			return ln, 0, true
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// splitPathColonLineSuffix extracts optional :line or :line:col suffixes after the last path separator.
func splitPathColonLineSuffix(s string) (base string, line, col int) {
	lastSep := strings.LastIndexAny(s, `/\`)
	tailStart := lastSep + 1
	trimmed := strings.TrimSuffix(s, ":")
	if tailStart >= len(trimmed) {
		return s, 0, 0
	}
	tail := trimmed[tailStart:]
	c1 := strings.LastIndexByte(tail, ':')
	if c1 <= 0 {
		return s, 0, 0
	}
	p1, ok1 := parsePositiveBoundedInt(tail[c1+1:])
	if !ok1 {
		return s, 0, 0
	}
	before1 := tail[:c1]
	if c2 := strings.LastIndexByte(before1, ':'); c2 > 0 {
		if p2, ok2 := parsePositiveBoundedInt(before1[c2+1:]); ok2 {
			return trimmed[:tailStart+c2], p2, p1
		}
	}
	return trimmed[:tailStart+c1], p1, 0
}

// splitPathLineSuffix extracts optional :line, :line:col, or #Lline suffixes after the last path separator.
func splitPathLineSuffix(s string) (base string, line, col int) {
	lastSep := strings.LastIndexAny(s, `/\`)
	tailStart := lastSep + 1
	if tailStart >= len(s) {
		return s, 0, 0
	}
	if hashRel := strings.LastIndexByte(s[tailStart:], '#'); hashRel > 0 {
		hashIdx := tailStart + hashRel
		if ln, c, ok := parseLineFragment(s[hashIdx+1:]); ok {
			return s[:hashIdx], ln, c
		}
	}
	return splitPathColonLineSuffix(s)
}

func isTrivialPath(p string, isWindows bool) bool {
	trimmed := strings.TrimRight(p, "/")
	if isWindows {
		trimmed = strings.TrimRight(trimmed, `\`)
	}
	if trimmed == "" || trimmed == "." || trimmed == ".." || trimmed == "~" {
		return true
	}
	if isWindows && len(trimmed) == 2 && isASCIIAlpha(trimmed[0]) && trimmed[1] == ':' {
		return true
	}
	// Reject chains consisting solely of dots and separators (e.g. "./", "../", "../../").
	hasNonDotSep := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch != '.' && ch != '/' && (!isWindows || ch != '\\') {
			hasNonDotSep = true
			break
		}
	}
	return !hasNonDotSep
}

func parseInlinePathRef(raw string) (parsedPathRef, bool) {
	return parseInlinePathRefForOS(raw, runtime.GOOS == "windows")
}

func parseInlinePathRefForOS(raw string, isWindows bool) (parsedPathRef, bool) {
	if raw == "" || len(raw) > inlinePathMaxBytes || strings.ContainsFunc(raw, unicode.IsControl) {
		return parsedPathRef{}, false
	}
	s := strings.Trim(raw, " ")
	if s == "" || s[0] == '-' || hasDisallowedPathChars(s) {
		return parsedPathRef{}, false
	}

	if strings.HasPrefix(strings.ToLower(s), "file://") {
		u, err := url.Parse(s)
		if err != nil || !strings.EqualFold(u.Scheme, "file") || u.User != nil ||
			u.RawQuery != "" || u.ForceQuery || u.Opaque != "" {
			return parsedPathRef{}, false
		}
		decodeFileURLPath := func(rawEscaped string) (string, bool) {
			unesc, err := url.PathUnescape(rawEscaped)
			if err != nil {
				return "", false
			}
			var p string
			if isWindows {
				if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
					p = `\\` + u.Host + filepath.FromSlash(unesc)
				} else {
					p = unesc
					if len(p) >= 3 && p[0] == '/' && isASCIIAlpha(p[1]) && p[2] == ':' {
						p = p[1:]
					}
					p = filepath.FromSlash(p)
				}
			} else {
				if (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || !strings.HasPrefix(unesc, "/") {
					return "", false
				}
				p = unesc
				if isWindowsDrivePath(strings.TrimPrefix(p, "/")) {
					return "", false
				}
			}
			if hasDisallowedPathChars(p) || isTrivialPath(p, isWindows) {
				return "", false
			}
			return p, true
		}

		rawEscapedPath := u.EscapedPath()
		rawP, okRaw := decodeFileURLPath(rawEscapedPath)
		if !okRaw {
			return parsedPathRef{}, false
		}
		p := rawP
		var line, col int
		if u.Fragment != "" {
			if ln, c, okFrag := parseLineFragment(u.Fragment); okFrag {
				line, col = ln, c
			}
		} else if baseEsc, ln, c := splitPathColonLineSuffix(rawEscapedPath); ln > 0 {
			// Distinguish path from :line[:col] before percent-decoding so %23L12
			// and %3A12 remain literal characters in the target filename.
			if baseP, okBase := decodeFileURLPath(baseEsc); okBase {
				p = baseP
				line, col = ln, c
			}
		}
		return parsedPathRef{
			path:      p,
			rawPath:   rawP,
			line:      line,
			col:       col,
			isAbs:     true,
			isFileURL: true,
		}, true
	}

	if hasNonFileScheme(s, isWindows) {
		return parsedPathRef{}, false
	}
	// Reject protocol-relative web URLs such as "//cdn.example.com/a.js" on every OS
	// while keeping Windows backslash UNC paths ("\\server\share\file").
	if strings.HasPrefix(s, "//") {
		return parsedPathRef{}, false
	}
	if !isWindows {
		if isWindowsDrivePath(s) || isUNCPath(s) || strings.ContainsRune(s, '\\') {
			return parsedPathRef{}, false
		}
	}

	base, line, col := splitPathLineSuffix(s)
	if base == "" {
		return parsedPathRef{}, false
	}

	hasSep := strings.ContainsRune(base, '/') || (isWindows && strings.ContainsRune(base, '\\'))
	if isTrivialPath(base, isWindows) || (!hasSep && !looksLikeSingleSegmentFileName(base)) {
		return parsedPathRef{}, false
	}

	isHome := strings.HasPrefix(base, "~/") || (isWindows && strings.HasPrefix(base, `~\`))
	if strings.HasPrefix(base, "~") && !isHome {
		return parsedPathRef{}, false
	}
	if isHome && strings.Trim(base[2:], `/\ `) == "" {
		return parsedPathRef{}, false
	}

	isAbs := false
	if isWindows {
		isAbs = isWindowsDrivePath(base) || isUNCPath(base)
	} else {
		isAbs = strings.HasPrefix(base, "/")
	}

	return parsedPathRef{
		path:    base,
		rawPath: s,
		line:    line,
		col:     col,
		isAbs:   isAbs,
		isHome:  isHome,
	}, true
}

func inlineCodePathCandidates(spans []model.Span) []inlinePathCandidate {
	if len(spans) == 0 {
		return nil
	}
	var out []inlinePathCandidate
	pos := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		if sp.Marks&model.MarkCode == 0 || sp.Marks&model.MarkMath != 0 || sp.Link != "" || n == 0 {
			pos += n
			continue
		}
		start := pos
		pos += n
		if _, ok := parseInlinePathRef(sp.Text); ok {
			out = append(out, inlinePathCandidate{
				start: start,
				end:   pos,
				raw:   sp.Text,
			})
		}
	}
	return out
}

func joinRawPath(base, rel string) string {
	if base == "" {
		return rel
	}
	sep := string(os.PathSeparator)
	if base == "/" || (runtime.GOOS == "windows" && (base == `\` || (len(base) == 3 && isASCIIAlpha(base[0]) && base[1] == ':' && (base[2] == '\\' || base[2] == '/')))) {
		return base + rel
	}
	trimmed := strings.TrimRight(base, `/\`)
	if trimmed == "" {
		return sep + rel
	}
	return trimmed + sep + rel
}

func pathHasDotDotSegment(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

type confinedRelCandidate struct {
	scopeRoot        string
	rel              string
	expectedRootInfo os.FileInfo
}

func pathLexicallyUnder(root, child string) (string, bool) {
	if root == "" || child == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return "", false
	}
	if rel == "." || filepath.IsLocal(rel) {
		return filepath.ToSlash(rel), true
	}
	return "", false
}

func (s *Session) SetPathScopeAnchor(noteDir string, noteDirInfo os.FileInfo, projRoot string, projRootInfo os.FileInfo, noteBelongsToProj bool) {
	if s == nil {
		return
	}
	cleanNote := ""
	if strings.TrimSpace(noteDir) != "" {
		cleanNote = filepath.Clean(noteDir)
	}
	cleanProj := ""
	if strings.TrimSpace(projRoot) != "" {
		cleanProj = filepath.Clean(projRoot)
	}
	if !noteBelongsToProj {
		_, noteBelongsToProj = pathLexicallyUnder(cleanProj, cleanNote)
	}
	s.pathScopeMu.Lock()
	s.pathScope = pathScopeAnchor{
		noteDir:           cleanNote,
		noteDirInfo:       noteDirInfo,
		projRoot:          cleanProj,
		projRootInfo:      projRootInfo,
		noteBelongsToProj: noteBelongsToProj,
	}
	s.pathScopeMu.Unlock()
}

func (s *Session) initDefaultPathScope() {
	if s == nil {
		return
	}
	// Never call back into s.host (e.g. projectRootDir/ActiveNotebook) from
	// NewSession, because callers such as Workspace may hold their own mutex.
	noteDir := s.mediaDir()
	cleanNote := ""
	if strings.TrimSpace(noteDir) != "" {
		cleanNote = filepath.Clean(noteDir)
	}
	var noteInfo os.FileInfo
	if testing.Testing() && cleanNote != "" {
		if st, err := os.Stat(cleanNote); err == nil && st.IsDir() {
			noteInfo = st
		}
	}
	s.pathScopeMu.Lock()
	s.pathScope = pathScopeAnchor{
		noteDir:     cleanNote,
		noteDirInfo: noteInfo,
	}
	s.pathScopeMu.Unlock()
}

func (s *Session) refreshPathScopeOnNoteChange() {
	if s == nil {
		return
	}
	// Never call back into s.host from SetNotePath (may be called under Workspace.mu).
	noteDir := s.mediaDir()
	cleanNote := ""
	if strings.TrimSpace(noteDir) != "" {
		cleanNote = filepath.Clean(noteDir)
	}
	s.pathScopeMu.Lock()
	prev := s.pathScope
	var noteInfo os.FileInfo
	if prev.noteDir == cleanNote {
		noteInfo = prev.noteDirInfo
	}
	_, belongs := pathLexicallyUnder(prev.projRoot, cleanNote)
	s.pathScope = pathScopeAnchor{
		noteDir:           cleanNote,
		noteDirInfo:       noteInfo,
		projRoot:          prev.projRoot,
		projRootInfo:      prev.projRootInfo,
		noteBelongsToProj: belongs,
	}
	s.pathScopeMu.Unlock()
}

func (s *Session) pathScopeSnapshot(noteDir, projRoot string) pathScopeAnchor {
	if s == nil {
		return pathScopeAnchor{}
	}
	cleanNote := ""
	if strings.TrimSpace(noteDir) != "" {
		cleanNote = filepath.Clean(noteDir)
	}
	cleanProj := ""
	if strings.TrimSpace(projRoot) != "" {
		cleanProj = filepath.Clean(projRoot)
	}
	s.pathScopeMu.Lock()
	snap := s.pathScope
	if snap.noteDir == cleanNote && snap.projRoot == cleanProj {
		s.pathScopeMu.Unlock()
		return snap
	}
	_, lexInProj := pathLexicallyUnder(cleanProj, cleanNote)
	projInfo := snap.projRootInfo
	if snap.projRoot != cleanProj {
		projInfo = nil
	}
	noteInfo := snap.noteDirInfo
	if snap.noteDir != cleanNote {
		noteInfo = nil
	}
	belongs := lexInProj || (snap.noteDir == cleanNote && snap.projRoot == cleanProj && snap.noteBelongsToProj)
	// First-time lazy initialization in unit tests where SetPathScopeAnchor wasn't called.
	if testing.Testing() && snap.projRoot == "" && cleanProj != "" {
		if st, err := os.Stat(cleanProj); err == nil && st.IsDir() {
			projInfo = st
		}
		if noteInfo == nil && cleanNote != "" {
			if st, err := os.Stat(cleanNote); err == nil && st.IsDir() {
				noteInfo = st
			}
		}
		if !belongs && cleanNote != "" {
			if realProj, errP := filepath.EvalSymlinks(cleanProj); errP == nil && realProj != "" {
				if realNote, errN := filepath.EvalSymlinks(cleanNote); errN == nil && realNote != "" {
					_, belongs = pathLexicallyUnder(realProj, realNote)
				}
			}
		}
		snap = pathScopeAnchor{
			noteDir:           cleanNote,
			noteDirInfo:       noteInfo,
			projRoot:          cleanProj,
			projRootInfo:      projInfo,
			noteBelongsToProj: belongs,
		}
		s.pathScope = snap
		s.pathScopeMu.Unlock()
		return snap
	}
	s.pathScopeMu.Unlock()
	return pathScopeAnchor{
		noteDir:           cleanNote,
		noteDirInfo:       noteInfo,
		projRoot:          cleanProj,
		projRootInfo:      projInfo,
		noteBelongsToProj: belongs,
	}
}

func confinedRelativeCandidates(rawRel, noteDir, projectRoot string, anchor pathScopeAnchor) []confinedRelCandidate {
	relSlash := strings.TrimSpace(filepath.ToSlash(rawRel))
	if relSlash == "" {
		return nil
	}
	var out []confinedRelCandidate
	cleanNote := ""
	if strings.TrimSpace(noteDir) != "" {
		cleanNote = filepath.Clean(noteDir)
	}
	cleanProj := ""
	if strings.TrimSpace(projectRoot) != "" {
		cleanProj = filepath.Clean(projectRoot)
	}

	var expectedProjInfo, expectedNoteInfo os.FileInfo
	if anchor.projRoot == cleanProj {
		expectedProjInfo = anchor.projRootInfo
	}
	if anchor.noteDir == cleanNote {
		expectedNoteInfo = anchor.noteDirInfo
	}

	if cleanNote != "" {
		lexRel, lexUnderProj := pathLexicallyUnder(cleanProj, cleanNote)
		wasUnderProj := anchor.noteDir == cleanNote && anchor.projRoot == cleanProj && anchor.noteBelongsToProj

		if lexUnderProj {
			// noteDir belongs to projectRoot lexically: confine strictly through
			// projectRoot so replacing an intermediate folder (e.g. proj/notes)
			// with an external symlink can NEVER fall back to trusting noteDir.
			if lexRel == "." {
				out = append(out, confinedRelCandidate{scopeRoot: cleanProj, rel: relSlash, expectedRootInfo: expectedProjInfo})
			} else {
				out = append(out, confinedRelCandidate{scopeRoot: cleanProj, rel: lexRel + "/" + relSlash, expectedRootInfo: expectedProjInfo})
			}
		} else if cleanProj != "" {
			realUnderProj := false
			if realProj, errP := filepath.EvalSymlinks(cleanProj); errP == nil && realProj != "" {
				if realNote, errN := filepath.EvalSymlinks(cleanNote); errN == nil && realNote != "" {
					if noteRel, okR := pathLexicallyUnder(realProj, realNote); okR {
						realUnderProj = true
						if noteRel == "." {
							out = append(out, confinedRelCandidate{scopeRoot: cleanProj, rel: relSlash, expectedRootInfo: expectedProjInfo})
						} else {
							out = append(out, confinedRelCandidate{scopeRoot: cleanProj, rel: noteRel + "/" + relSlash, expectedRootInfo: expectedProjInfo})
						}
					}
				}
			}
			// If noteDir previously belonged to projectRoot and lost ownership,
			// reject noteDir as an independent root instead of trusting it.
			if !realUnderProj && !wasUnderProj {
				out = append(out, confinedRelCandidate{scopeRoot: cleanNote, rel: relSlash, expectedRootInfo: expectedNoteInfo})
			}
		} else if !wasUnderProj {
			out = append(out, confinedRelCandidate{scopeRoot: cleanNote, rel: relSlash, expectedRootInfo: expectedNoteInfo})
		}
	}
	if cleanProj != "" && cleanProj != cleanNote {
		if len(out) == 0 || out[0].scopeRoot != cleanProj || out[0].rel != relSlash {
			out = append(out, confinedRelCandidate{scopeRoot: cleanProj, rel: relSlash, expectedRootInfo: expectedProjInfo})
		}
	}
	return out
}

// resolveConfinedRelativePath resolves a relative path strictly inside the allowed
// scope roots (note directory / owning project root first, then project root),
// validating root directory identity and every path component & symlink hop via os.Root.
func resolveConfinedRelativePath(rawRel, noteDir, projectRoot string, anchor pathScopeAnchor, line, col int) resolvedPathTarget {
	cands := confinedRelativeCandidates(rawRel, noteDir, projectRoot, anchor)
	var sawPermErr bool
	for _, cand := range cands {
		root, err := os.OpenRoot(cand.scopeRoot)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				sawPermErr = true
			}
			continue
		}
		rInfo, rID, rStatErr := files.RootDirIdentityToken(root)
		if rStatErr != nil || (cand.expectedRootInfo != nil && !os.SameFile(rInfo, cand.expectedRootInfo)) {
			_ = root.Close()
			if errors.Is(rStatErr, fs.ErrPermission) {
				sawPermErr = true
			}
			continue
		}
		resolvedRel, rErr := resolveRootRelTarget(root, cand.rel)
		if rErr != nil {
			_ = root.Close()
			if errors.Is(rErr, fs.ErrPermission) {
				sawPermErr = true
			}
			continue
		}
		linfo, lErr := root.Lstat(resolvedRel)
		_ = root.Close()
		if lErr != nil {
			if errors.Is(lErr, fs.ErrPermission) {
				sawPermErr = true
			}
			continue
		}
		if !linfo.Mode().IsRegular() && !linfo.IsDir() {
			continue
		}
		absPath := filepath.Clean(filepath.Join(cand.scopeRoot, resolvedRel))
		return resolvedPathTarget{
			absPath:         absPath,
			confineRoot:     cand.scopeRoot,
			confineRel:      resolvedRel,
			confineRootInfo: rInfo,
			confineRootID:   rID,
			line:            line,
			col:             col,
			isDir:           linfo.IsDir(),
			exists:          true,
		}
	}
	return resolvedPathTarget{permErr: sawPermErr}
}

func resolvePathTarget(raw, noteDir, projectRoot string) resolvedPathTarget {
	return resolvePathTargetWithAnchor(raw, noteDir, projectRoot, pathScopeAnchor{})
}

func resolvePathTargetWithAnchor(raw, noteDir, projectRoot string, anchor pathScopeAnchor) resolvedPathTarget {
	parsed, ok := parseInlinePathRef(raw)
	if !ok {
		return resolvedPathTarget{}
	}

	tryResolve := func(pathStr string, isHome, isAbs bool, line, col int) (resolvedPathTarget, bool) {
		if !isHome && !isAbs {
			res := resolveConfinedRelativePath(pathStr, noteDir, projectRoot, anchor, line, col)
			return res, res.exists
		}
		var cand string
		if isHome {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return resolvedPathTarget{}, false
			}
			rest := strings.TrimLeft(pathStr[2:], `/\`)
			if rest == "" {
				return resolvedPathTarget{}, false
			}
			cand = joinRawPath(home, filepath.FromSlash(rest))
		} else {
			cand = pathStr
		}
		if cand == "" {
			return resolvedPathTarget{}, false
		}
		// Pass the uncleaned path to os.Stat so the OS resolves symlinks before "..".
		info, err := os.Stat(cand)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return resolvedPathTarget{permErr: true}, false
			}
			return resolvedPathTarget{}, false
		}
		// Never follow or open special OS nodes (FIFOs, Unix sockets, device files).
		if !info.Mode().IsRegular() && !info.IsDir() {
			return resolvedPathTarget{}, false
		}
		var resolved string
		if pathHasDotDotSegment(cand) {
			if eval, evalErr := filepath.EvalSymlinks(cand); evalErr == nil && eval != "" {
				resolved = filepath.Clean(eval)
			}
		}
		if resolved == "" {
			abs, absErr := filepath.Abs(cand)
			if absErr != nil {
				return resolvedPathTarget{}, false
			}
			resolved = filepath.Clean(abs)
		}
		return resolvedPathTarget{
			absPath: resolved,
			line:    line,
			col:     col,
			isDir:   info.IsDir(),
			exists:  true,
		}, true
	}

	if res, found := tryResolve(parsed.path, parsed.isHome, parsed.isAbs, parsed.line, parsed.col); found {
		return res
	} else if parsed.line > 0 && parsed.rawPath != parsed.path {
		// Fallback for a literal filename that happens to contain ":<digits>" or "#L<digits>".
		if literal, litFound := tryResolve(parsed.rawPath, parsed.isHome, parsed.isAbs, 0, 0); litFound {
			return literal
		} else if literal.permErr || res.permErr {
			return resolvedPathTarget{permErr: true}
		}
		return resolvedPathTarget{}
	} else {
		return res
	}
}

func (s *Session) projectRootDir() string {
	if s == nil || s.host == nil {
		return ""
	}
	return strings.TrimSpace(s.host.ActiveNotebook())
}

func (s *Session) resetPathProbeCache() {
	if s == nil {
		return
	}
	s.clearInlinePathContextMenuLock()
	s.pathProbeMu.Lock()
	s.pathProbeEpoch++
	clear(s.pathProbeCache)
	s.pathProbeMu.Unlock()
}

func (s *Session) storePathProbeLocked(key pathProbeKey, entry pathProbeEntry) {
	if s.pathProbeCache == nil {
		s.pathProbeCache = make(map[pathProbeKey]pathProbeEntry)
	}
	if len(s.pathProbeCache) >= pathProbeMaxEntries {
		now := time.Now()
		for k, v := range s.pathProbeCache {
			if !v.pending && now.Sub(v.checkedAt) >= pathProbeTTL {
				delete(s.pathProbeCache, k)
			}
		}
		if len(s.pathProbeCache) >= pathProbeMaxEntries {
			for k, v := range s.pathProbeCache {
				if !v.pending {
					delete(s.pathProbeCache, k)
				}
			}
		}
	}
	s.pathProbeCache[key] = entry
}

// probeInlinePath checks the cached existence of a candidate inline path.
// In live UI runs, misses trigger an asynchronous background os.Stat so hovering
// over slow cloud/network paths never blocks rendering.
func (s *Session) probeInlinePath(raw string) (resolvedPathTarget, bool) {
	if s == nil || s.previewMedia != nil || raw == "" {
		return resolvedPathTarget{}, false
	}
	noteDir := s.mediaDir()
	projRoot := s.projectRootDir()
	anchor := s.pathScopeSnapshot(noteDir, projRoot)
	key := pathProbeKey{raw: raw, noteDir: noteDir, projRoot: projRoot}
	now := time.Now()

	s.pathProbeMu.Lock()
	if entry, ok := s.pathProbeCache[key]; ok {
		if entry.pending {
			s.pathProbeMu.Unlock()
			return resolvedPathTarget{}, false
		}
		if now.Sub(entry.checkedAt) < pathProbeTTL {
			s.pathProbeMu.Unlock()
			return entry.target, true
		}
	}
	if s.host == nil || testing.Testing() || appUi.IsProbeEnabled() {
		epoch := s.pathProbeEpoch
		s.pathProbeMu.Unlock()
		res := resolvePathTargetWithAnchor(raw, noteDir, projRoot, anchor)
		s.pathProbeMu.Lock()
		if s.pathProbeEpoch == epoch {
			s.storePathProbeLocked(key, pathProbeEntry{
				target:    res,
				checkedAt: time.Now(),
			})
		}
		s.pathProbeMu.Unlock()
		return res, true
	}
	if s.pathProbeInFlight >= pathProbeMaxInFlight {
		s.pathProbeMu.Unlock()
		return resolvedPathTarget{}, false
	}
	s.pathProbeInFlight++
	epoch := s.pathProbeEpoch
	s.storePathProbeLocked(key, pathProbeEntry{pending: true})
	s.pathProbeMu.Unlock()

	go func() {
		res := resolvePathTargetWithAnchor(raw, noteDir, projRoot, anchor)
		s.pathProbeMu.Lock()
		if s.pathProbeInFlight > 0 {
			s.pathProbeInFlight--
		}
		active := s.pathProbeEpoch == epoch
		if active {
			s.storePathProbeLocked(key, pathProbeEntry{
				target:    res,
				checkedAt: time.Now(),
			})
		}
		s.pathProbeMu.Unlock()
		if active {
			s.requestRedraw()
		}
	}()
	return resolvedPathTarget{}, false
}

// resolveInlinePathNow resolves raw freshly on disk for an explicit user click
// or context menu action (bypassing the hover TTL cache) and refreshes the cache.
func (s *Session) resolveInlinePathNow(raw string) resolvedPathTarget {
	if s == nil || s.previewMedia != nil || raw == "" {
		return resolvedPathTarget{}
	}
	noteDir := s.mediaDir()
	projRoot := s.projectRootDir()
	anchor := s.pathScopeSnapshot(noteDir, projRoot)
	key := pathProbeKey{raw: raw, noteDir: noteDir, projRoot: projRoot}

	s.pathProbeMu.Lock()
	epoch := s.pathProbeEpoch
	s.pathProbeMu.Unlock()

	res := resolvePathTargetWithAnchor(raw, noteDir, projRoot, anchor)
	s.pathProbeMu.Lock()
	if s.pathProbeEpoch == epoch {
		s.storePathProbeLocked(key, pathProbeEntry{
			target:    res,
			checkedAt: time.Now(),
		})
	}
	s.pathProbeMu.Unlock()
	return res
}

func isMacOSPackageBundleExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".app", ".bundle", ".framework", ".pkg", ".kext", ".plugin", ".prefpane", ".xpc", ".xcodeproj", ".xcworkspace":
		return true
	default:
		return false
	}
}

func isMacOSPackageBundle(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if isMacOSPackageBundleExt(path) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" && resolved != path {
		return isMacOSPackageBundleExt(resolved)
	}
	return false
}

// isExternalOnlyDocOrMediaExt matches document/media formats that should always
// be handed to the OS default viewer (never opened as raw text source in Block).
func isExternalOnlyDocOrMediaExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".rtf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".odt", ".ods", ".odp", ".zip", ".mp3", ".wav", ".m4a", ".flac",
		".mp4", ".mov", ".m4v", ".webm", ".mkv", ".avi", ".ogv":
		return true
	default:
		return false
	}
}

func isSafeExternalDocOrMediaExt(path string) bool {
	if isExternalOnlyDocOrMediaExt(path) {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".csv", ".tsv", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func isExecutableFile(realPath string, info os.FileInfo) bool {
	return isExecutableFileForOS(runtime.GOOS, realPath, info)
}

func isExecutableFileForOS(goos, realPath string, info os.FileInfo) bool {
	if info == nil {
		return true
	}
	if goos != "windows" && info.Mode().Perm()&0111 != 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(realPath))
	switch ext {
	case ".sh", ".command", ".tool", ".csh", ".zsh", ".bash", ".ksh", ".fish",
		".exe", ".bat", ".cmd", ".com", ".msi", ".msp", ".mst",
		".ps1", ".psm1", ".psd1", ".ps1xml",
		".vbs", ".vbe", ".jse", ".wsf", ".wsh", ".vb",
		".scr", ".pif", ".lnk", ".url", ".scf", ".cpl",
		".msc", ".reg", ".hta", ".chm", ".inf", ".ins", ".isp",
		".application", ".gadget", ".diagcab",
		".msix", ".appx", ".msixbundle", ".appxbundle",
		".msh", ".msh1", ".msh2", ".mshxml", ".msh1xml", ".msh2xml",
		".jar", ".appimage", ".run", ".dmg", ".pkg":
		return true
	}
	if goos == "windows" {
		switch ext {
		case ".js":
			return true
		}
	}
	return false
}

func cleanupStagedDir(subDir, dstPath string) {
	if dstPath != "" {
		_ = os.Chmod(dstPath, 0o600)
	}
	if subDir != "" {
		_ = os.RemoveAll(subDir)
	}
}

func sanitizedStagedFileName(realPath string) string {
	base := filepath.Base(realPath)
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	var b strings.Builder
	for _, r := range stem {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' || r == ' ' {
			b.WriteRune(r)
		}
	}
	cleanStem := strings.Trim(b.String(), " .")
	if cleanStem == "" {
		cleanStem = "file"
	}
	if len(cleanStem) > 96 {
		cleanStem = cleanStem[:96]
	}
	return cleanStem + ext
}

// stageExternalPreviewCopy copies an already-verified, opened regular file descriptor
// into a private read-only staging file, preserves and verifies any OS origin security
// mark (Windows Zone.Identifier / macOS com.apple.quarantine) via open file handles,
// and confirms read-only permissions before handing dstPath to the OS viewer.
func stageExternalPreviewCopy(realPath string, f *os.File, info os.FileInfo) (string, error) {
	if f == nil || info == nil || !info.Mode().IsRegular() {
		return "", files.ErrNotRegularFile
	}
	if info.Size() > maxExternalCopyBytes {
		return "", errors.New("file exceeds external viewer copy limit")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	subDir, err := os.MkdirTemp("", "block-open-*")
	if err != nil {
		return "", err
	}
	dstPath := filepath.Join(subDir, sanitizedStagedFileName(realPath))
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		cleanupStagedDir(subDir, "")
		return "", err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(f, maxExternalCopyBytes+1))
	if copyErr != nil || n > maxExternalCopyBytes {
		_ = dst.Close()
		cleanupStagedDir(subDir, dstPath)
		if copyErr != nil {
			return "", copyErr
		}
		return "", errors.New("file exceeds external viewer copy limit")
	}
	if syncErr := dst.Sync(); syncErr != nil {
		_ = dst.Close()
		cleanupStagedDir(subDir, dstPath)
		return "", syncErr
	}
	dstInfo, statErr := dst.Stat()
	if statErr != nil || !dstInfo.Mode().IsRegular() {
		_ = dst.Close()
		cleanupStagedDir(subDir, dstPath)
		if statErr != nil {
			return "", statErr
		}
		return "", files.ErrNotRegularFile
	}
	expectedMark, hasMark, markErr := preserveOriginSecurityMark(realPath, f, dst, dstPath)
	closeErr := dst.Close()
	if markErr != nil || closeErr != nil {
		cleanupStagedDir(subDir, dstPath)
		if markErr != nil {
			return "", markErr
		}
		return "", closeErr
	}
	if chmodErr := os.Chmod(dstPath, 0o400); chmodErr != nil {
		cleanupStagedDir(subDir, dstPath)
		return "", chmodErr
	}

	// Final pre-launch verification on the staged file: confirm identity, read-only
	// permissions (including Windows FILE_ATTRIBUTE_READONLY), and origin security mark.
	finalFile, finalInfo, verifyOpenErr := files.OpenRegularNonBlockNoFollow(dstPath)
	if verifyOpenErr != nil {
		cleanupStagedDir(subDir, dstPath)
		return "", verifyOpenErr
	}
	sameStaged := os.SameFile(finalInfo, dstInfo)
	readOnlyOK := finalInfo.Mode().Perm()&0o222 == 0
	var markVerifyErr error
	if sameStaged && readOnlyOK && hasMark {
		markVerifyErr = verifyOriginSecurityMark(finalFile, expectedMark)
	}
	_ = finalFile.Close()
	if !sameStaged || !readOnlyOK || markVerifyErr != nil {
		cleanupStagedDir(subDir, dstPath)
		if markVerifyErr != nil {
			return "", markVerifyErr
		}
		return "", errors.New("staged preview file verification failed")
	}
	return dstPath, nil
}

type preparedPathActionKind uint8

const (
	pathActionNone preparedPathActionKind = iota
	pathActionPermissionDenied
	pathActionRevealPath
	pathActionRevealDir
	pathActionOpenClassified
	pathActionOpenExternalURL
)

type preparedPathAction struct {
	kind           preparedPathActionKind
	path           string
	url            string
	target         model.VerifiedFileTarget
	preparedTab    model.PreparedTabTarget
	hasPreparedTab bool
	preparedDir    model.PreparedDirectoryTarget
	hasPreparedDir bool
	classified     model.ClassifiedFile
	line           int
	col            int
}

func resolveRootRelTarget(root *os.Root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", media.ErrInvalid
	}
	slash := filepath.ToSlash(rel)
	if strings.HasPrefix(slash, "/") {
		return "", media.ErrInvalid
	}
	parts := strings.Split(slash, "/")
	var resolved []string
	hops := 0
	for len(parts) > 0 {
		seg := parts[0]
		parts = parts[1:]
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(resolved) == 0 {
				return "", media.ErrInvalid
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		cand := filepath.FromSlash(strings.Join(append(resolved, seg), "/"))
		info, err := root.Lstat(cand)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			hops++
			if hops > 40 {
				return "", media.ErrInvalid
			}
			target, err := root.Readlink(cand)
			if err != nil {
				return "", err
			}
			if target == "" || filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				return "", media.ErrInvalid
			}
			targetSlash := filepath.ToSlash(target)
			if strings.HasPrefix(targetSlash, "/") {
				return "", media.ErrInvalid
			}
			targetParts := strings.Split(targetSlash, "/")
			parts = append(targetParts, parts...)
			continue
		}
		resolved = append(resolved, seg)
	}
	if len(resolved) == 0 {
		return "", media.ErrInvalid
	}
	out := filepath.FromSlash(strings.Join(resolved, "/"))
	if out == "." || !filepath.IsLocal(out) {
		return "", media.ErrInvalid
	}
	return out, nil
}

func prepareStagedExternalAction(realPath string, f *os.File, info os.FileInfo) preparedPathAction {
	stagedPath, err := stageExternalPreviewCopy(realPath, f, info)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{kind: pathActionRevealPath, path: realPath}
	}
	return preparedPathAction{kind: pathActionOpenExternalURL, url: localFileURL(stagedPath)}
}

func resolveMarkdownLocalTarget(rawLink, noteDir, projectRoot string, anchor pathScopeAnchor) resolvedPathTarget {
	s := strings.Trim(rawLink, " ")
	if s == "" || len(s) > 16*1024 || strings.ContainsFunc(s, unicode.IsControl) {
		return resolvedPathTarget{}
	}
	if strings.HasPrefix(strings.ToLower(s), "file:") {
		// file: links in Markdown are accepted only when they point inside noteDir or projectRoot.
		for _, base := range []string{noteDir, projectRoot} {
			if strings.TrimSpace(base) == "" {
				continue
			}
			if rel, err := media.RelativePath(base, s); err == nil {
				if base == noteDir {
					if res := resolveConfinedRelativePath(rel, noteDir, projectRoot, anchor, 0, 0); res.exists || res.permErr {
						return res
					}
				} else {
					if res := resolveConfinedRelativePath(rel, "", projectRoot, anchor, 0, 0); res.exists || res.permErr {
						return res
					}
				}
			}
		}
		return resolvedPathTarget{}
	}
	decoded := s
	if unesc, err := url.PathUnescape(s); err == nil {
		decoded = unesc
	}
	decoded = filepath.ToSlash(decoded)
	if decoded == "" || strings.ContainsFunc(decoded, unicode.IsControl) || strings.Contains(decoded, `\`) || strings.HasPrefix(decoded, "//") || strings.Contains(decoded, "://") {
		return resolvedPathTarget{}
	}
	p := filepath.FromSlash(decoded)
	if strings.Contains(strings.TrimPrefix(p, filepath.VolumeName(p)), ":") {
		return resolvedPathTarget{}
	}
	if filepath.IsAbs(p) {
		for _, base := range []string{noteDir, projectRoot} {
			if strings.TrimSpace(base) == "" {
				continue
			}
			if rel, err := media.RelativePath(base, rawLink); err == nil {
				if base == noteDir {
					if res := resolveConfinedRelativePath(rel, noteDir, projectRoot, anchor, 0, 0); res.exists || res.permErr {
						return res
					}
				} else {
					if res := resolveConfinedRelativePath(rel, "", projectRoot, anchor, 0, 0); res.exists || res.permErr {
						return res
					}
				}
			}
		}
		return resolvedPathTarget{}
	}
	return resolveConfinedRelativePath(decoded, noteDir, projectRoot, anchor, 0, 0)
}

func prepareConfinedTargetAction(res resolvedPathTarget, markdownLinkOnly bool, hasHost bool) preparedPathAction {
	if !res.exists || res.confineRoot == "" || res.confineRel == "" {
		if res.permErr {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	root, err := os.OpenRoot(res.confineRoot)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	defer func() { _ = root.Close() }()

	rInfo, rID, rStatErr := files.RootDirIdentityToken(root)
	if rStatErr != nil || (res.confineRootInfo != nil && !os.SameFile(rInfo, res.confineRootInfo)) {
		if errors.Is(rStatErr, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}

	resolvedRel, err := resolveRootRelTarget(root, res.confineRel)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	linfo, err := root.Lstat(resolvedRel)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	realAbsPath := filepath.Clean(filepath.Join(res.confineRoot, resolvedRel))
	if linfo.IsDir() {
		if markdownLinkOnly {
			return preparedPathAction{}
		}
		if isMacOSPackageBundle(res.absPath) || isMacOSPackageBundle(realAbsPath) {
			return preparedPathAction{kind: pathActionRevealPath, path: realAbsPath}
		}
		return preparedPathAction{kind: pathActionRevealDir, path: realAbsPath}
	}
	if !linfo.Mode().IsRegular() {
		return preparedPathAction{}
	}

	// Handle executables FIRST before opening, classifying, or handing to OS viewer.
	if isExecutableFile(res.absPath, linfo) || isExecutableFile(realAbsPath, linfo) {
		return preparedPathAction{kind: pathActionRevealPath, path: realAbsPath}
	}

	f, fInfo, err := files.OpenRootRegularNonBlockNoFollow(root, resolvedRel)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	defer func() { _ = f.Close() }()

	if !os.SameFile(linfo, fInfo) {
		return preparedPathAction{}
	}
	if isExecutableFile(res.absPath, fInfo) || isExecutableFile(realAbsPath, fInfo) {
		return preparedPathAction{kind: pathActionRevealPath, path: realAbsPath}
	}

	verified := model.VerifiedFileTarget{
		AbsPath:         realAbsPath,
		ConfineRoot:     res.confineRoot,
		ConfineRel:      resolvedRel,
		ConfineRootInfo: rInfo,
		FileInfo:        fInfo,
		RootIdentity:    rID,
		FileIdentity:    files.FileIdentityToken(fInfo, f),
		EnforceIdentity: true,
	}

	if markdownLinkOnly {
		if files.IsMarkdown(realAbsPath) && hasHost {
			cf, cfErr := files.ClassifyFileHandle(realAbsPath, f, fInfo)
			if cfErr != nil {
				if errors.Is(cfErr, fs.ErrPermission) {
					return preparedPathAction{kind: pathActionPermissionDenied}
				}
				return preparedPathAction{}
			}
			if cf.Kind != model.OpenKindMarkdown {
				return preparedPathAction{}
			}
			return preparedPathAction{
				kind:       pathActionOpenClassified,
				target:     verified,
				classified: cf,
			}
		}
		if isSafeExternalDocOrMediaExt(realAbsPath) {
			return prepareStagedExternalAction(realAbsPath, f, fInfo)
		}
		return preparedPathAction{}
	}

	if isExternalOnlyDocOrMediaExt(realAbsPath) {
		return prepareStagedExternalAction(realAbsPath, f, fInfo)
	}
	if hasHost {
		cf, cfErr := files.ClassifyFileHandle(realAbsPath, f, fInfo)
		if cfErr != nil {
			if errors.Is(cfErr, fs.ErrPermission) {
				return preparedPathAction{kind: pathActionPermissionDenied}
			}
			return preparedPathAction{}
		}
		if cf.Kind == model.OpenKindMarkdown || cf.Kind == model.OpenKindSource || cf.Kind == model.OpenKindPreview {
			return preparedPathAction{
				kind:       pathActionOpenClassified,
				target:     verified,
				classified: cf,
				line:       res.line,
				col:        res.col,
			}
		}
	}
	if isSafeExternalDocOrMediaExt(realAbsPath) {
		return prepareStagedExternalAction(realAbsPath, f, fInfo)
	}
	return preparedPathAction{kind: pathActionRevealPath, path: realAbsPath}
}

func prepareAbsoluteTargetAction(res resolvedPathTarget, hasHost bool) preparedPathAction {
	if !res.exists || res.absPath == "" {
		if res.permErr {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	realPath, err := filepath.EvalSymlinks(res.absPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	realPath = filepath.Clean(realPath)
	realInfo, err := os.Lstat(realPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	if realInfo.IsDir() {
		if isMacOSPackageBundle(res.absPath) || isMacOSPackageBundle(realPath) {
			return preparedPathAction{kind: pathActionRevealPath, path: res.absPath}
		}
		return preparedPathAction{kind: pathActionRevealDir, path: res.absPath}
	}
	if !realInfo.Mode().IsRegular() {
		return preparedPathAction{}
	}

	// Handle executables FIRST before opening, classifying, or handing to OS viewer.
	if isExecutableFile(realPath, realInfo) {
		return preparedPathAction{kind: pathActionRevealPath, path: realPath}
	}

	// Open once with O_NONBLOCK|O_NOFOLLOW to check readability and prevent TOCTOU swaps.
	f, fInfo, err := files.OpenRegularNonBlockNoFollow(realPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return preparedPathAction{kind: pathActionPermissionDenied}
		}
		return preparedPathAction{}
	}
	defer func() { _ = f.Close() }()

	if !os.SameFile(realInfo, fInfo) {
		return preparedPathAction{}
	}
	if isExecutableFile(realPath, fInfo) {
		return preparedPathAction{kind: pathActionRevealPath, path: realPath}
	}

	if isExternalOnlyDocOrMediaExt(realPath) {
		return prepareStagedExternalAction(realPath, f, fInfo)
	}
	if hasHost {
		cf, cfErr := files.ClassifyFileHandle(realPath, f, fInfo)
		if cfErr != nil {
			if errors.Is(cfErr, fs.ErrPermission) {
				return preparedPathAction{kind: pathActionPermissionDenied}
			}
			return preparedPathAction{}
		}
		if cf.Kind == model.OpenKindMarkdown || cf.Kind == model.OpenKindSource || cf.Kind == model.OpenKindPreview {
			return preparedPathAction{
				kind: pathActionOpenClassified,
				target: model.VerifiedFileTarget{
					AbsPath:         realPath,
					FileInfo:        fInfo,
					FileIdentity:    files.FileIdentityToken(fInfo, f),
					EnforceIdentity: true,
				},
				classified: cf,
				line:       res.line,
				col:        res.col,
			}
		}
	}
	if isSafeExternalDocOrMediaExt(realPath) {
		return prepareStagedExternalAction(realPath, f, fInfo)
	}
	return preparedPathAction{kind: pathActionRevealPath, path: realPath}
}

func prepareResolvedPathAction(res resolvedPathTarget, hasHost bool) preparedPathAction {
	if res.confineRoot != "" {
		return prepareConfinedTargetAction(res, false, hasHost)
	}
	return prepareAbsoluteTargetAction(res, hasHost)
}

func (s *Session) finalizeBackgroundPathAction(act preparedPathAction) preparedPathAction {
	if s == nil || s.host == nil {
		return act
	}
	ph, ok := s.host.(pathPrepareHost)
	if !ok {
		return act
	}
	switch act.kind {
	case pathActionOpenClassified:
		prep, err := ph.PrepareClassifiedTarget(act.target)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return preparedPathAction{kind: pathActionPermissionDenied}
			}
			return preparedPathAction{}
		}
		act.preparedTab = prep
		act.hasPreparedTab = true
	case pathActionRevealDir:
		act.preparedDir = ph.PrepareRevealDirectory(act.path)
		act.hasPreparedDir = true
	}
	return act
}

func (s *Session) applyPreparedPathAction(svc ui.Services, act preparedPathAction) {
	if s == nil || s.previewMedia != nil {
		return
	}
	switch act.kind {
	case pathActionPermissionDenied:
		s.notifyPathPermissionError()
	case pathActionRevealPath:
		s.revealPath(act.path)
	case pathActionRevealDir:
		if act.hasPreparedDir {
			if ph, ok := s.host.(pathPrepareHost); ok {
				ph.ApplyPreparedRevealDirectory(act.preparedDir)
				return
			}
		}
		s.revealDirectory(act.path)
	case pathActionOpenExternalURL:
		if act.url != "" {
			svc.OpenURL(act.url)
		}
	case pathActionOpenClassified:
		if s.host == nil {
			return
		}
		var openErr error
		if act.hasPreparedTab {
			if ph, ok := s.host.(pathPrepareHost); ok {
				openErr = ph.ApplyPreparedClassifiedAt(act.preparedTab, act.classified, act.line, act.col)
			} else if ch, ok := s.host.(pathOpenClassifiedHost); ok {
				openErr = ch.OpenClassifiedAt(act.target, act.classified, act.line, act.col)
			}
		} else if ch, ok := s.host.(pathOpenClassifiedHost); ok {
			openErr = ch.OpenClassifiedAt(act.target, act.classified, act.line, act.col)
		} else if ph, ok := s.host.(pathOpenPosHost); ok {
			openErr = ph.OpenNoteAt(act.target.AbsPath, act.line, act.col)
		} else {
			openErr = s.host.OpenNote(act.target.AbsPath, act.line)
		}
		if openErr != nil && errors.Is(openErr, fs.ErrPermission) {
			s.notifyPathPermissionError()
		}
	}
}

func (s *Session) runPathAction(svc ui.Services, work func(noteDir, projRoot string, anchor pathScopeAnchor, hasHost bool) preparedPathAction) {
	if s == nil || s.previewMedia != nil || work == nil {
		return
	}
	noteDir := s.mediaDir()
	projRoot := s.projectRootDir()
	anchor := s.pathScopeSnapshot(noteDir, projRoot)
	hasHost := s.host != nil

	asyncHost, canAsync := s.host.(pathAsyncHost)
	if !canAsync || testing.Testing() || appUi.IsProbeEnabled() {
		act := s.finalizeBackgroundPathAction(work(noteDir, projRoot, anchor, hasHost))
		s.applyPreparedPathAction(svc, act)
		return
	}

	s.pathProbeMu.Lock()
	if s.pathOpenInFlight >= pathProbeMaxInFlight {
		s.pathProbeMu.Unlock()
		return
	}
	s.pathOpenInFlight++
	s.pathOpenSeq++
	seq := s.pathOpenSeq
	epoch := s.pathProbeEpoch
	s.pathProbeMu.Unlock()

	go func() {
		act := s.finalizeBackgroundPathAction(work(noteDir, projRoot, anchor, hasHost))
		s.pathProbeMu.Lock()
		if s.pathOpenInFlight > 0 {
			s.pathOpenInFlight--
		}
		stillLatest := s.pathProbeEpoch == epoch && s.pathOpenSeq == seq
		s.pathProbeMu.Unlock()
		if !stillLatest || act.kind == pathActionNone {
			return
		}
		_ = asyncHost.RunOnMain(func() {
			s.pathProbeMu.Lock()
			valid := s.pathProbeEpoch == epoch && s.pathOpenSeq == seq
			s.pathProbeMu.Unlock()
			if !valid || s.previewMedia != nil {
				return
			}
			s.applyPreparedPathAction(svc, act)
			s.requestRedraw()
		})
	}()
}

// openLocalMarkdownLink opens an ordinary Markdown local link using the exact same
// root-confined relative path policy, executable-first check, and staged external
// viewer handling as inline relative paths.
func (s *Session) openLocalMarkdownLink(svc ui.Services, rawLink string) {
	if s == nil || s.previewMedia != nil {
		return
	}
	s.runPathAction(svc, func(noteDir, projRoot string, anchor pathScopeAnchor, hasHost bool) preparedPathAction {
		res := resolveMarkdownLocalTarget(rawLink, noteDir, projRoot, anchor)
		return prepareConfinedTargetAction(res, true, hasHost)
	})
}

func (s *Session) notifyPathPermissionError() {
	if s == nil || s.host == nil {
		return
	}
	if nh, ok := s.host.(pathNotifyHost); ok {
		nh.NotifyPathError(theme.Tr("无法访问该路径：权限被拒绝", "Cannot access path: permission denied"))
	}
}

func (s *Session) revealPath(absPath string) {
	if s == nil || s.host == nil || absPath == "" {
		return
	}
	if rh, ok := s.host.(pathRevealHost); ok {
		rh.RevealPath(absPath)
	}
}

func (s *Session) revealDirectory(absPath string) {
	if s == nil || s.host == nil || absPath == "" {
		return
	}
	if dh, ok := s.host.(pathRevealDirHost); ok {
		dh.RevealDirectory(absPath)
		return
	}
	s.revealPath(absPath)
}

func (s *Session) openInlinePathLink(svc ui.Services, raw string) {
	if s == nil || s.previewMedia != nil || raw == "" {
		return
	}
	s.runPathAction(svc, func(noteDir, projRoot string, anchor pathScopeAnchor, hasHost bool) preparedPathAction {
		res := resolvePathTargetWithAnchor(raw, noteDir, projRoot, anchor)
		s.pathProbeMu.Lock()
		key := pathProbeKey{raw: raw, noteDir: noteDir, projRoot: projRoot}
		s.storePathProbeLocked(key, pathProbeEntry{
			target:    res,
			checkedAt: time.Now(),
		})
		s.pathProbeMu.Unlock()
		return prepareResolvedPathAction(res, hasHost)
	})
}

func (s *Session) revealInlinePathLink(raw string) {
	if s == nil || s.previewMedia != nil || raw == "" {
		return
	}
	s.runPathAction(ui.Services{}, func(noteDir, projRoot string, anchor pathScopeAnchor, _ bool) preparedPathAction {
		res := resolvePathTargetWithAnchor(raw, noteDir, projRoot, anchor)
		s.pathProbeMu.Lock()
		key := pathProbeKey{raw: raw, noteDir: noteDir, projRoot: projRoot}
		s.storePathProbeLocked(key, pathProbeEntry{
			target:    res,
			checkedAt: time.Now(),
		})
		s.pathProbeMu.Unlock()
		if !res.exists {
			if res.permErr {
				return preparedPathAction{kind: pathActionPermissionDenied}
			}
			return preparedPathAction{}
		}
		return preparedPathAction{kind: pathActionRevealPath, path: res.absPath}
	})
}

func (s *Session) openResolvedPath(svc ui.Services, res resolvedPathTarget) {
	if s == nil || s.previewMedia != nil || !res.exists || res.absPath == "" {
		return
	}
	s.runPathAction(svc, func(_, _ string, _ pathScopeAnchor, hasHost bool) preparedPathAction {
		return prepareResolvedPathAction(res, hasHost)
	})
}

func (s *Session) cancelAllPendingRichLinks() {
	if s == nil {
		return
	}
	for _, st := range s.richInputs {
		if st != nil {
			st.CancelPendingLink()
		}
	}
	for _, st := range s.tableInputs {
		if st != nil {
			st.CancelPendingLink()
		}
	}
}

func findInlinePathCandidateAtPoint(spans []model.Span, plain string, lay *ui.TextLayout, localPt ui.Point) (inlinePathCandidate, ui.Rect, bool) {
	cands := inlineCodePathCandidates(spans)
	if len(cands) == 0 || lay == nil {
		return inlinePathCandidate{}, ui.Rect{}, false
	}
	for _, cand := range cands {
		rects := appUi.RichLinkRects(lay, plain, appUi.RichLinkRange{Start: cand.start, End: cand.end})
		for _, rect := range rects {
			if rect.Contains(localPt.X, localPt.Y) {
				return cand, rect, true
			}
		}
	}
	return inlinePathCandidate{}, ui.Rect{}, false
}

func findInlinePathCandidateAtOffset(spans []model.Span, runeOff int) (inlinePathCandidate, bool) {
	for _, cand := range inlineCodePathCandidates(spans) {
		if runeOff >= cand.start && runeOff <= cand.end {
			return cand, true
		}
	}
	return inlinePathCandidate{}, false
}

func formatResolvedPathTooltip(res resolvedPathTarget) string {
	if res.line > 0 {
		if res.col > 0 {
			return fmt.Sprintf("%s:%d:%d", res.absPath, res.line, res.col)
		}
		return fmt.Sprintf("%s:%d", res.absPath, res.line)
	}
	return res.absPath
}

func (s *Session) hoveredInlinePathRange(spans []model.Span, plain string, lay *ui.TextLayout, textOrigin ui.Point, ptrWin ui.Point, cmdActive bool) (appUi.RichLinkRange, bool) {
	if !cmdActive || s == nil || s.previewMedia != nil || lay == nil || s.hasMenuPopupOpen() || s.lightbox.Open {
		return appUi.RichLinkRange{}, false
	}
	localPt := ui.Point{X: ptrWin.X - textOrigin.X, Y: ptrWin.Y - textOrigin.Y}
	cand, localRect, ok := findInlinePathCandidateAtPoint(spans, plain, lay, localPt)
	if !ok {
		return appUi.RichLinkRange{}, false
	}
	res, ready := s.probeInlinePath(cand.raw)
	if !ready || !res.exists {
		return appUi.RichLinkRange{}, false
	}
	s.framePathHover = &hoveredPathTooltip{
		rect: ui.Rect{
			X: textOrigin.X + localRect.X,
			Y: textOrigin.Y + localRect.Y,
			W: localRect.W,
			H: localRect.H,
		},
		display: formatResolvedPathTooltip(res),
		isDir:   res.isDir,
	}
	return appUi.RichLinkRange{
		Start:        cand.start,
		End:          cand.end,
		URL:          encodeInlinePathLink(cand.raw),
		ModifierOnly: true,
	}, true
}

func (s *Session) resolveCmdClickPathAtPoint(spans []model.Span, plain string, lay *ui.TextLayout, localPt ui.Point) string {
	if s == nil || s.previewMedia != nil || lay == nil {
		return ""
	}
	cand, _, ok := findInlinePathCandidateAtPoint(spans, plain, lay, localPt)
	if !ok {
		return ""
	}
	// Never run synchronous disk/network stat on PointerDown in live UI mode;
	// reuse any in-flight background probe and re-verify off the UI thread on release.
	res, ready := s.probeInlinePath(cand.raw)
	if ready && !res.exists && !res.permErr {
		return ""
	}
	return encodeInlinePathLink(cand.raw)
}

func (s *Session) tableCellTextLayout(b *model.Block, rIdx, cIdx int) (cellBlock model.Block, plain string, lay *ui.TextLayout, textOrigin ui.Point, ok bool) {
	if b == nil || b.Table == nil || rIdx < 0 || rIdx >= len(b.Table.Rows) || cIdx < 0 || cIdx >= len(b.Table.Rows[rIdx]) {
		return model.Block{}, "", nil, ui.Point{}, false
	}
	cr, rOk := s.tableCellRect(b.ID, rIdx, cIdx)
	if !rOk || cr.W <= 0 || cr.H <= 0 {
		return model.Block{}, "", nil, ui.Point{}, false
	}
	cell := &b.Table.Rows[rIdx][cIdx]
	plain = cell.PlainText()
	if plain == "" {
		return model.Block{}, "", nil, ui.Point{}, false
	}
	tFontSz, tLineH, _, _, _, tPadY, tPadX := s.tableMetrics()
	weight := tableCellTextWeight(b.Table, rIdx, cIdx)
	cellAlign := tableCellTextAlign(b.Table, rIdx, cIdx)
	lhFactor := tableCellLineHeight / tableCellFontSize
	cellBlock = model.Block{ID: b.ID, Kind: model.BlockParagraph, Spans: cell.Spans}
	pureSpans := s.buildPureDisplaySpans(&cellBlock, tFontSz, weight)
	lay = s.cachedShapeTableCell(
		tableCellKey{BlockID: b.ID, Row: rIdx, Col: cIdx},
		plain,
		pureSpans,
		resolvedEditorFont(),
		tFontSz,
		weight,
		lhFactor,
		cellAlign,
		max(cr.W-2*tPadX, 1),
	)
	if lay == nil {
		return model.Block{}, "", nil, ui.Point{}, false
	}
	_, textH := lay.Size()
	textY := cr.Y + max((cr.H-max(tLineH, textH))*0.5, tPadY)
	return cellBlock, plain, lay, ui.Point{X: cr.X + tPadX, Y: textY}, true
}

func (s *Session) richBlockTextLayout(idx int, b *model.Block) (plain string, lay *ui.TextLayout, textOrigin ui.Point, ok bool) {
	if b == nil || len(b.Spans) == 0 {
		return "", nil, ui.Point{}, false
	}
	if b.Kind == model.BlockCode || b.Kind == model.BlockRawHTML || b.Kind == model.BlockTable || b.Kind == model.BlockDivider || b.Kind == model.BlockImage {
		return "", nil, ui.Point{}, false
	}
	tr, trOk := s.blockTextRect(b.ID)
	if !trOk || tr.W <= 0 || tr.H <= 0 {
		return "", nil, ui.Point{}, false
	}
	plain = b.PlainText()
	if plain == "" {
		return "", nil, ui.Point{}, false
	}
	fontSize, weight, lh := s.textMetrics(b.Kind)
	lay = s.cachedShapeBlock(b.ID, plain, b.Spans, idx, resolvedEditorFont(), fontSize, weight, lh, tr.W, true)
	if lay == nil {
		return "", nil, ui.Point{}, false
	}
	return plain, lay, ui.Point{X: tr.X, Y: tr.Y}, true
}

// hitExplicitLinkAtPoint checks whether pt falls inside an explicit Markdown link span
// (supporting both regular blocks and table cells).
func (s *Session) hitExplicitLinkAtPoint(idx int, b *model.Block, pt ui.Point) string {
	if b == nil {
		return ""
	}
	if b.Kind == model.BlockTable && b.Table != nil {
		rIdx, cIdx, ok := s.hitTableCellCoords(b.ID, pt.X, pt.Y)
		if !ok {
			return ""
		}
		cellBlock, plain, lay, origin, ok := s.tableCellTextLayout(b, rIdx, cIdx)
		if !ok {
			return ""
		}
		localPt := ui.Point{X: pt.X - origin.X, Y: pt.Y - origin.Y}
		for _, lr := range blockLinkRanges(&cellBlock) {
			for _, rect := range appUi.RichLinkRects(lay, plain, lr) {
				if rect.Contains(localPt.X, localPt.Y) {
					return lr.URL
				}
			}
		}
		return ""
	}
	plain, lay, origin, ok := s.richBlockTextLayout(idx, b)
	if !ok {
		return ""
	}
	localPt := ui.Point{X: pt.X - origin.X, Y: pt.Y - origin.Y}
	for _, lr := range blockLinkRanges(b) {
		for _, rect := range appUi.RichLinkRects(lay, plain, lr) {
			if rect.Contains(localPt.X, localPt.Y) {
				return lr.URL
			}
		}
	}
	return ""
}

// hitInlinePathAtPoint checks whether pt falls inside a valid inline-code path
// (supporting both regular blocks and table cells).
func (s *Session) hitInlinePathAtPoint(idx int, b *model.Block, pt ui.Point) (inlinePathCandidate, bool) {
	if s == nil || s.previewMedia != nil || b == nil {
		return inlinePathCandidate{}, false
	}
	if b.Kind == model.BlockTable && b.Table != nil {
		rIdx, cIdx, ok := s.hitTableCellCoords(b.ID, pt.X, pt.Y)
		if !ok {
			return inlinePathCandidate{}, false
		}
		cellBlock, plain, lay, origin, ok := s.tableCellTextLayout(b, rIdx, cIdx)
		if !ok {
			return inlinePathCandidate{}, false
		}
		localPt := ui.Point{X: pt.X - origin.X, Y: pt.Y - origin.Y}
		cand, _, found := findInlinePathCandidateAtPoint(cellBlock.Spans, plain, lay, localPt)
		return cand, found
	}
	plain, lay, origin, ok := s.richBlockTextLayout(idx, b)
	if !ok {
		return inlinePathCandidate{}, false
	}
	localPt := ui.Point{X: pt.X - origin.X, Y: pt.Y - origin.Y}
	cand, _, found := findInlinePathCandidateAtPoint(b.Spans, plain, lay, localPt)
	return cand, found
}

func (s *Session) inlinePathCandidateAtPointer(pt ui.Point) (inlinePathCandidate, bool) {
	if s == nil || s.previewMedia != nil || s.doc == nil || len(s.doc.Blocks) == 0 {
		return inlinePathCandidate{}, false
	}
	pos := s.hitPositionAtPoint(pt.X, pt.Y)
	if pos.Block >= 0 && pos.Block < len(s.doc.Blocks) {
		if cand, ok := s.hitInlinePathAtPoint(pos.Block, &s.doc.Blocks[pos.Block], pt); ok {
			return cand, true
		}
	}
	return inlinePathCandidate{}, false
}

func (s *Session) inlinePathCandidateAtCaret() (inlinePathCandidate, bool) {
	if s == nil || s.previewMedia != nil || s.doc == nil || len(s.doc.Blocks) == 0 {
		return inlinePathCandidate{}, false
	}
	if s.selHead.Block >= 0 && s.selHead.Block < len(s.doc.Blocks) {
		blk := &s.doc.Blocks[s.selHead.Block]
		if blk.Kind == model.BlockTable && blk.Table != nil {
			if r, c, relLo, _, ok := tableCellAtRuneRange(blk.Table, s.selHead.Offset, s.selHead.Offset); ok {
				if cand, found := findInlinePathCandidateAtOffset(blk.Table.Rows[r][c].Spans, relLo); found {
					return cand, true
				}
			}
			return inlinePathCandidate{}, false
		}
		if cand, found := findInlinePathCandidateAtOffset(blk.Spans, s.selHead.Offset); found {
			return cand, true
		}
	}
	return inlinePathCandidate{}, false
}

func revealPathMenuLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return theme.Tr("在访达中显示", "Reveal in Finder")
	case "windows":
		return theme.Tr("在资源管理器中显示", "Show in Explorer")
	default:
		return theme.Tr("在文件管理器中显示", "Show in File Manager")
	}
}

func (s *Session) appendInlinePathContextMenu(m *ui.Menu, svc ui.Services, ownerKey string) {
	if s == nil || s.previewMedia != nil {
		return
	}
	if s.ctxMenuLocked && s.ctxMenuOwnerKey != ownerKey {
		s.clearInlinePathContextMenuLock()
	}
	switch {
	case s.ctxMenuPtrValid:
		// Context menu opened by pointer right-click: hit-test ONLY at the right-click point
		// (never fall back to caret) and lock the candidate for this single menu instance
		// without ever performing synchronous network/disk I/O on the UI thread.
		pt := s.ctxMenuPtrWin
		s.ctxMenuPtrValid = false
		s.ctxMenuSeq++
		s.ctxMenuOwnerKey = ownerKey
		s.ctxMenuLocked = true
		s.ctxMenuLockedFromPtr = true
		s.ctxMenuLockedValid = false
		s.ctxMenuLockedRaw = ""
		s.ctxMenuLockedTarget = resolvedPathTarget{}
		if cand, ok := s.inlinePathCandidateAtPointer(pt); ok {
			res, ready := s.probeInlinePath(cand.raw)
			if !ready || res.exists {
				s.ctxMenuLockedValid = true
				s.ctxMenuLockedRaw = cand.raw
				s.ctxMenuLockedTarget = res
			}
		}
	case !s.ctxMenuLocked || (!s.ctxMenuLockedFromPtr && s.ctxMenuLockedSel != s.selHead):
		// Context menu opened via keyboard (no pointer right-click): use caret position and lock.
		s.ctxMenuSeq++
		s.ctxMenuOwnerKey = ownerKey
		s.ctxMenuLocked = true
		s.ctxMenuLockedFromPtr = false
		s.ctxMenuLockedSel = s.selHead
		s.ctxMenuLockedValid = false
		s.ctxMenuLockedRaw = ""
		s.ctxMenuLockedTarget = resolvedPathTarget{}
		if cand, ok := s.inlinePathCandidateAtCaret(); ok {
			res, ready := s.probeInlinePath(cand.raw)
			if !ready || res.exists {
				s.ctxMenuLockedValid = true
				s.ctxMenuLockedRaw = cand.raw
				s.ctxMenuLockedTarget = res
			}
		}
	}
	if !s.ctxMenuLockedValid || s.ctxMenuLockedRaw == "" {
		return
	}
	raw := s.ctxMenuLockedRaw
	seq := s.ctxMenuSeq
	m.Separator()
	if m.Item(theme.Tr("打开路径", "Open Path")).ID(fmt.Sprintf("inline-path-open:%d", seq)).Chosen() {
		s.clearInlinePathContextMenuLock()
		s.openInlinePathLink(svc, raw)
	}
	if s.host != nil {
		if _, ok := s.host.(pathRevealHost); ok {
			if m.Item(revealPathMenuLabel()).ID(fmt.Sprintf("inline-path-reveal:%d", seq)).Chosen() {
				s.clearInlinePathContextMenuLock()
				s.revealInlinePathLink(raw)
			}
		}
	}
}

func (s *Session) renderHoveredPathTooltip(c *ui.Context) {
	if s == nil || s.previewMedia != nil || s.framePathHover == nil || s.hasMenuPopupOpen() || s.lightbox.Open {
		return
	}
	if !c.WindowActive() || c.Modifiers() != ui.Cmd {
		return
	}
	winW, winH := c.Size()
	if winW <= 0 || winH <= 0 {
		return
	}
	pal := theme.Ocean()
	tip := s.framePathHover
	trigger := theme.Tr("⌘ + 点击", "⌘ + Click")
	if !theme.IsMacOS() {
		trigger = theme.Tr("Ctrl + 点击", "Ctrl + Click")
	}
	maxW := min(float32(520), max(winW-2*theme.Space12, 180))
	estW, estH := s.measuredOrEstSize("inline-path-tooltip", min(maxW, 320), theme.TooltipHeight)
	x := tip.rect.X + (tip.rect.W-estW)*0.5
	x = clampFloat32(x, theme.Space8, max(theme.Space8, winW-estW-theme.Space8))
	y := tip.rect.Y - estH - theme.TooltipOffset
	if y < theme.Space8 {
		y = tip.rect.Y + tip.rect.H + theme.TooltipOffset
	}
	if y+estH > winH-theme.Space8 {
		y = max(theme.Space8, winH-estH-theme.Space8)
	}

	ui.Overlay(c, func() {
		pill := appUi.Elevate(
			ui.Row(c.Key("inline-path-tooltip")).Absolute().Left(x).Top(y).MaxWidth(maxW).
				Padding(theme.Space4, theme.Space10).Gap(theme.Space6).AlignItems(ui.Center).
				Radius(theme.RadiusPill).Background(pal.TooltipBG).Border(1, pal.TooltipBorder).
				IgnoreInput(true),
			theme.ElevationPopover,
		)
		s.recordOverlayPanel("inline-path-tooltip", pill)
		pill.Children(func() {
			ui.Text(c.Key("path-tip-trigger"), trigger).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightControl).TextColor(pal.TooltipAccent).
				SingleLine().Shrink(0)
			ui.Text(c.Key("path-tip-dest"), tip.display).
				FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
				FontWeight(theme.WeightRegular).TextColor(pal.TooltipMuted).
				SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		})
	})
}
