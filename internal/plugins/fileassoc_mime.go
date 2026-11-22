package plugins

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const linuxCustomMIMEPrefix = "application/x-block-ext-"

var linuxMIMETypeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,63}$`)

// allowedLinuxApplicationMIMEs is an explicit allowlist of well-known
// text/code/config application/* MIME types that are always safe to register
// even without consulting shared-mime-info subclasses.
var allowedLinuxApplicationMIMEs = map[string]struct{}{
	"application/atom+xml":      {},
	"application/ecmascript":    {},
	"application/graphql":       {},
	"application/javascript":    {},
	"application/json":          {},
	"application/ld+json":       {},
	"application/manifest+json": {},
	"application/ndjson":        {},
	"application/rss+xml":       {},
	"application/sql":           {},
	"application/toml":          {},
	"application/vnd.dart":      {},
	"application/x-apple-plist": {},
	"application/x-awk":         {},
	"application/x-bat":         {},
	"application/x-csh":         {},
	"application/x-desktop":     {},
	"application/x-erb":         {},
	"application/x-hcl":         {},
	"application/x-httpd-php":   {},
	"application/x-javascript":  {},
	"application/x-json":        {},
	"application/x-latex":       {},
	"application/x-ndjson":      {},
	"application/x-perl":        {},
	"application/x-php":         {},
	"application/x-powershell":  {},
	"application/x-python":      {},
	"application/x-ruby":        {},
	"application/x-sed":         {},
	"application/x-sh":          {},
	"application/x-shellscript": {},
	"application/x-sql":         {},
	"application/x-tex":         {},
	"application/x-toml":        {},
	"application/x-yaml":        {},
	"application/xhtml+xml":     {},
	"application/xml":           {},
	"application/yaml":          {},
}

var rejectedLinuxTextMIMEs = map[string]struct{}{
	"text/calendar":  {},
	"text/directory": {},
	"text/vcard":     {},
}

// linuxTextRootMIMEs are ancestor MIME types in shared-mime-info subclasses
// that guarantee a subtype is textual source/config/markup data.
var linuxTextRootMIMEs = map[string]struct{}{
	"text/plain":                {},
	"text/xml":                  {},
	"application/xml":           {},
	"application/json":          {},
	"application/javascript":    {},
	"application/ecmascript":    {},
	"application/x-shellscript": {},
	"application/yaml":          {},
	"application/toml":          {},
}

// linuxBinaryRootMIMEs are ancestor MIME types in shared-mime-info subclasses
// that identify binary, archive, executable, or container formats.
var linuxBinaryRootMIMEs = map[string]struct{}{
	"application/octet-stream":      {},
	"application/zip":               {},
	"application/gzip":              {},
	"application/x-bzip":            {},
	"application/x-bzip2":           {},
	"application/x-xz":              {},
	"application/zstd":              {},
	"application/x-archive":         {},
	"application/x-ole-storage":     {},
	"application/x-executable":      {},
	"application/x-sharedlib":       {},
	"application/x-object":          {},
	"application/x-cd-image":        {},
	"application/pdf":               {},
	"application/x-sqlite3":         {},
	"application/vnd.sqlite3":       {},
	"application/x-java-archive":    {},
	"application/x-ms-dos-executable": {},
	"application/x-mach-binary":     {},
}

// linuxBinaryMIMETokens blocks binary, ROM, archive, media, executable,
// database, certificate, and office container application/* MIME types when
// inspecting system globs2 entries that lack a shared-mime-info text subclass.
var linuxBinaryMIMETokens = map[string]struct{}{
	"3d": {}, "7z": {}, "amiga": {}, "apk": {}, "archive": {}, "atari": {},
	"audio": {}, "bin": {}, "binary": {}, "bios": {}, "bmp": {}, "bytecode": {},
	"bzip": {}, "bzip2": {}, "c64": {}, "cab": {}, "cad": {}, "cert": {},
	"certificate": {}, "compressed": {}, "core": {}, "database": {}, "deb": {},
	"disk": {}, "dll": {}, "dmg": {}, "dosexec": {}, "dump": {}, "dwg": {},
	"ear": {}, "elf": {}, "emulator": {}, "encrypted": {}, "eps": {},
	"excel": {}, "exec": {}, "executable": {}, "firmware": {}, "flac": {},
	"flash": {}, "font": {}, "game": {}, "gba": {}, "genesis": {}, "gif": {},
	"gzip": {}, "ico": {}, "image": {}, "img": {}, "ipa": {}, "iso": {},
	"jar": {}, "jpeg": {}, "jpg": {}, "mach": {}, "macho": {}, "media": {},
	"midi": {}, "mp3": {}, "mp4": {}, "mpeg": {}, "msdownload": {}, "msi": {},
	"msiexec": {}, "msword": {}, "nds": {}, "nes": {}, "nintendo": {},
	"object": {}, "octet": {}, "officedocument": {}, "ogg": {},
	"opendocument": {}, "otf": {}, "pdf": {}, "pe": {}, "pgp": {}, "pkg": {},
	"pkcs": {}, "png": {}, "postscript": {}, "powerpoint": {}, "psd": {},
	"psp": {}, "psx": {}, "qcow2": {}, "rar": {}, "rom": {}, "rpm": {},
	"sega": {}, "sharedlib": {}, "shockwave": {}, "snes": {}, "sqlite": {},
	"sqlite3": {}, "stl": {}, "tar": {}, "tiff": {}, "torrent": {}, "ttf": {},
	"vhd": {}, "video": {}, "vm": {}, "vmdk": {}, "war": {}, "wasm": {},
	"wav": {}, "webp": {}, "woff": {}, "woff2": {}, "x509": {}, "xz": {},
	"zip": {}, "zstd": {},
}

// baselineLinuxMIMEs provides standard freedesktop/IANA MIME types for core
// built-in text/config/code extensions so minimal Linux environments and
// non-Linux build hosts still derive a complete MIME list from
// BuiltinAssocExtensions().
var baselineLinuxMIMEs = map[string][]string{
	".md":         {"text/markdown", "text/x-markdown"},
	".markdown":   {"text/markdown", "text/x-markdown"},
	".mdown":      {"text/markdown", "text/x-markdown"},
	".mkd":        {"text/markdown", "text/x-markdown"},
	".mkdn":       {"text/markdown", "text/x-markdown"},
	".mdwn":       {"text/markdown", "text/x-markdown"},
	".mdtxt":      {"text/markdown", "text/x-markdown"},
	".mdtext":     {"text/markdown", "text/x-markdown"},
	".mdx":        {"text/markdown", "text/x-markdown"},
	".txt":        {"text/plain"},
	".text":       {"text/plain"},
	".csv":        {"text/csv"},
	".tsv":        {"text/tab-separated-values"},
	".json":       {"application/json"},
	".jsonc":      {"application/json"},
	".json5":      {"application/json"},
	".jsonl":      {"application/ndjson", "application/x-ndjson"},
	".ndjson":     {"application/ndjson", "application/x-ndjson"},
	".yaml":       {"application/yaml", "application/x-yaml"},
	".yml":        {"application/yaml", "application/x-yaml"},
	".toml":       {"application/toml", "application/x-toml"},
	".xml":        {"application/xml", "text/xml"},
	".svg":        {"image/svg+xml"},
	".html":       {"text/html"},
	".htm":        {"text/html"},
	".xhtml":      {"application/xhtml+xml"},
	".css":        {"text/css"},
	".js":         {"text/javascript", "application/javascript"},
	".mjs":        {"text/javascript", "application/javascript"},
	".cjs":        {"text/javascript", "application/javascript"},
	".ts":         {"text/typescript"},
	".mts":        {"text/typescript"},
	".cts":        {"text/typescript"},
	".tsx":        {"text/tsx"},
	".jsx":        {"text/jsx"},
	".py":         {"text/x-python"},
	".pyw":        {"text/x-python"},
	".go":         {"text/x-go"},
	".rs":         {"text/rust"},
	".c":          {"text/x-csrc"},
	".h":          {"text/x-chdr"},
	".cpp":        {"text/x-c++src"},
	".cc":         {"text/x-c++src"},
	".cxx":        {"text/x-c++src"},
	".hpp":        {"text/x-c++hdr"},
	".hh":         {"text/x-c++hdr"},
	".java":       {"text/x-java"},
	".kt":         {"text/x-kotlin"},
	".kts":        {"text/x-kotlin"},
	".cs":         {"text/x-csharp"},
	".swift":      {"text/x-swift"},
	".dart":       {"application/vnd.dart", "text/x-dart"},
	".sh":         {"application/x-shellscript", "text/x-shellscript"},
	".bash":       {"application/x-shellscript"},
	".zsh":        {"application/x-shellscript"},
	".fish":       {"application/x-shellscript"},
	".sql":        {"application/sql", "text/x-sql"},
	".rb":         {"application/x-ruby"},
	".php":        {"application/x-php"},
	".pl":         {"application/x-perl"},
	".lua":        {"text/x-lua"},
	".diff":       {"text/x-patch"},
	".patch":      {"text/x-patch"},
	".ini":        {"text/plain"},
	".conf":       {"text/plain"},
	".cfg":        {"text/plain"},
	".env":        {"text/plain"},
	".properties": {"text/x-java-properties"},
	".cmake":      {"text/x-cmake"},
	".mk":         {"text/x-makefile"},
	".dockerfile": {"text/x-dockerfile"},
	".groovy":     {"text/x-groovy"},
	".gradle":     {"text/x-gradle"},
	".ex":         {"text/x-elixir"},
	".exs":        {"text/x-elixir"},
	".ps1":        {"text/x-powershell", "application/x-powershell"},
	".psm1":       {"text/x-powershell", "application/x-powershell"},
	".psd1":       {"text/x-powershell", "application/x-powershell"},
	".bat":        {"application/x-bat"},
	".cmd":        {"application/x-bat"},
	".tex":        {"text/x-tex"},
	".rst":        {"text/x-rst"},
	".scss":       {"text/x-scss"},
	".sass":       {"text/x-sass"},
	".less":       {"text/x-less"},
	".vue":        {"text/x-vue"},
	".svelte":     {"text/x-svelte"},
	".nix":        {"text/x-nix"},
	".proto":      {"text/x-protobuf"},
	".graphql":    {"application/graphql"},
	".gql":        {"application/graphql"},
	".plist":      {"application/x-apple-plist"},
	".hcl":        {"application/x-hcl"},
	".tf":         {"application/x-hcl"},
	".tfvars":     {"application/x-hcl"},
	".erb":        {"application/x-erb"},
	".hbs":        {"text/x-handlebars-template"},
	".handlebars": {"text/x-handlebars-template"},
	".ejs":        {"text/x-ejs"},
	".thrift":     {"text/x-thrift"},
}

type linuxGlobMatch struct {
	mimeType string
	weight   int
}

func normalizeRawLinuxMIMEType(raw string) (string, bool) {
	mt := strings.TrimSpace(raw)
	if idx := strings.IndexByte(mt, ';'); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	mt = strings.ToLower(mt)
	if !linuxMIMETypeRe.MatchString(mt) {
		return "", false
	}
	if strings.HasPrefix(mt, linuxCustomMIMEPrefix) {
		return "", false
	}
	return mt, true
}

func hasLinuxMIMEAncestor(mt string, subclasses map[string][]string, targets map[string]struct{}) bool {
	if len(subclasses) == 0 {
		return false
	}
	visited := make(map[string]struct{})
	queue := append([]string(nil), subclasses[mt]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if _, seen := visited[cur]; seen {
			continue
		}
		visited[cur] = struct{}{}
		if _, match := targets[cur]; match {
			return true
		}
		queue = append(queue, subclasses[cur]...)
	}
	return false
}

func hasBinaryLinuxMIMEToken(mt string) bool {
	_, subtype, ok := strings.Cut(mt, "/")
	if !ok || subtype == "" {
		return true
	}
	for _, tok := range strings.FieldsFunc(subtype, func(r rune) bool {
		return r == '-' || r == '.' || r == '+' || r == '_'
	}) {
		if _, bad := linuxBinaryMIMETokens[tok]; bad {
			return true
		}
	}
	return false
}

func isSafeLinuxTextOrCodeMIME(mt string, subclasses map[string][]string, fromSystemGlob bool) bool {
	switch {
	case strings.HasPrefix(mt, "text/"):
		if _, bad := rejectedLinuxTextMIMEs[mt]; bad {
			return false
		}
		return true
	case mt == "image/svg+xml":
		return true
	case strings.HasPrefix(mt, "application/"):
		if _, ok := allowedLinuxApplicationMIMEs[mt]; ok {
			return true
		}
		// Reject any application/* subtype that inherits from binary/archive/
		// executable root types in shared-mime-info.
		if hasLinuxMIMEAncestor(mt, subclasses, linuxBinaryRootMIMEs) {
			return false
		}
		// Accept any application/* subtype that explicitly inherits from a
		// textual root type in shared-mime-info.
		if hasLinuxMIMEAncestor(mt, subclasses, linuxTextRootMIMEs) {
			return true
		}
		// Structured-syntax suffixes (RFC 6838).
		for _, badSuffix := range []string{"+zip", "+gzip", "+cbor", "+bson", "+protobuf", "+octet-stream", "+sqlite3", "+ber", "+der"} {
			if strings.HasSuffix(mt, badSuffix) {
				return false
			}
		}
		for _, textSuffix := range []string{"+xml", "+json", "+yaml", "+toml", "+text"} {
			if strings.HasSuffix(mt, textSuffix) {
				return true
			}
		}
		// For system-declared globs2 mappings bound to one of Block's verified
		// safe text/code extensions (e.g. *.prisma -> application/vnd.example.prisma),
		// allow non-binary application/* code/config MIME types while still
		// excluding binary, ROM, archive, media, and executable types.
		if fromSystemGlob && !hasBinaryLinuxMIMEToken(mt) {
			return true
		}
		return false
	default:
		return false
	}
}

func normalizeLinuxMIMEType(raw string) (string, bool) {
	mt, ok := normalizeRawLinuxMIMEType(raw)
	if !ok {
		return "", false
	}
	if !isSafeLinuxTextOrCodeMIME(mt, nil, false) {
		return "", false
	}
	return mt, true
}

func linuxXDGDataDirsFromEnv(dataHome, xdgDataDirs string) []string {
	var dirs []string
	seen := make(map[string]struct{})
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if _, dup := seen[d]; dup {
			return
		}
		seen[d] = struct{}{}
		dirs = append(dirs, d)
	}
	addDir(dataHome)
	if strings.TrimSpace(xdgDataDirs) == "" {
		xdgDataDirs = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(xdgDataDirs, ":") {
		addDir(d)
	}
	return dirs
}

func linuxXDGConfigDirsFromEnv(configHome, xdgConfigDirs string) []string {
	var dirs []string
	seen := make(map[string]struct{})
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if _, dup := seen[d]; dup {
			return
		}
		seen[d] = struct{}{}
		dirs = append(dirs, d)
	}
	addDir(configHome)
	if strings.TrimSpace(xdgConfigDirs) == "" {
		xdgConfigDirs = "/etc/xdg"
	}
	for _, d := range strings.Split(xdgConfigDirs, ":") {
		addDir(d)
	}
	return dirs
}

// loadLinuxMIMEGlobsFromDirs parses Freedesktop shared-mime-info globs2 files
// across XDG data dirs in precedence order, respecting __NOGLOBS__ suppression
// and filtering out non-text/binary/ROM MIME types while preserving valid
// system-declared code/config MIME types (such as vendor application/* types).
func loadLinuxMIMEGlobsFromDirs(dirs []string, subclasses map[string][]string) map[string][]string {
	bestByExt := make(map[string][]linuxGlobMatch)
	seen := make(map[string]map[string]int)
	blockedLower := make(map[string]struct{})

	for _, baseDir := range dirs {
		f, err := os.Open(filepath.Join(baseDir, "mime", "globs2"))
		if err != nil {
			continue
		}
		dirBlocked := make(map[string]struct{})
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 16*1024), 256*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, ":")
			if len(parts) < 3 {
				continue
			}
			weight, wErr := strconv.Atoi(strings.TrimSpace(parts[0]))
			if wErr != nil {
				weight = 50
			}
			rawMIME := strings.TrimSpace(parts[1])
			pattern := strings.TrimSpace(parts[2])
			if rawMIME == "" || strings.HasPrefix(strings.ToLower(rawMIME), linuxCustomMIMEPrefix) {
				continue
			}
			mimeType, ok := normalizeRawLinuxMIMEType(rawMIME)
			if !ok {
				continue
			}
			if pattern == "__NOGLOBS__" {
				dirBlocked[mimeType] = struct{}{}
				continue
			}
			if _, suppressed := blockedLower[mimeType]; suppressed {
				continue
			}
			if !strings.HasPrefix(pattern, "*.") {
				continue
			}
			if len(parts) >= 4 && strings.Contains(parts[3], "cs") && pattern != strings.ToLower(pattern) {
				continue
			}
			ext := strings.ToLower(pattern[1:])
			if strings.ContainsAny(ext, "*?[]") {
				continue
			}
			if !isSafeAssocExtension(ext) {
				continue
			}
			if !isSafeLinuxTextOrCodeMIME(mimeType, subclasses, true) {
				continue
			}
			if seen[ext] == nil {
				seen[ext] = make(map[string]int)
			}
			if prevW, exists := seen[ext][mimeType]; exists {
				if weight <= prevW {
					continue
				}
			}
			seen[ext][mimeType] = weight
			bestByExt[ext] = append(bestByExt[ext], linuxGlobMatch{mimeType: mimeType, weight: weight})
		}
		_ = f.Close()
		for mimeType := range dirBlocked {
			blockedLower[mimeType] = struct{}{}
		}
	}

	out := make(map[string][]string, len(bestByExt))
	for ext, matches := range bestByExt {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].weight != matches[j].weight {
				return matches[i].weight > matches[j].weight
			}
			return matches[i].mimeType < matches[j].mimeType
		})
		var types []string
		dedup := make(map[string]struct{}, len(matches))
		for _, m := range matches {
			if _, ok := dedup[m.mimeType]; ok {
				continue
			}
			dedup[m.mimeType] = struct{}{}
			types = append(types, m.mimeType)
		}
		out[ext] = types
	}
	return out
}

// LinuxPackageMIMETypes derives the packaged block.desktop MimeType list
// directly from BuiltinAssocExtensions() (the single source of truth for all
// built-in languages and formats), combining system shared-mime-info (when
// building on Linux), Go's stdlib mime table, standard baseline mappings, and
// text/plain fallback while excluding binary/non-text MIME types.
func LinuxPackageMIMETypes() []string {
	resolved := ResolveLinuxMIMETypesForExtensions(BuiltinAssocExtensions(), "", "/usr/local/share:/usr/share")
	return unionSortedStrings(resolved, []string{
		"text/markdown",
		"text/plain",
		"text/x-markdown",
	})
}

// ResolveLinuxMIMETypesForExtensions resolves safe Linux freedesktop MIME types
// for the given dot-prefixed extensions using XDG shared-mime-info globs2 +
// subclasses tables, Go's stdlib mime registry, standard baseline mappings, and
// text/plain fallback for recognized text formats without a dedicated MIME type.
func ResolveLinuxMIMETypesForExtensions(exts []string, dataHome, xdgDataDirs string) []string {
	dirs := linuxXDGDataDirsFromEnv(dataHome, xdgDataDirs)
	subclasses := loadLinuxMIMESubclasses(dirs)
	globs := loadLinuxMIMEGlobsFromDirs(dirs, subclasses)
	hasSystemGlobs := len(globs) > 0

	var mimeList []string
	addMIME := func(raw string, fromSystemGlob bool) bool {
		mt, ok := normalizeRawLinuxMIMEType(raw)
		if !ok {
			return false
		}
		if !isSafeLinuxTextOrCodeMIME(mt, subclasses, fromSystemGlob) {
			return false
		}
		mimeList = append(mimeList, mt)
		return true
	}

	for _, rawExt := range exts {
		ext, ok := normalizeAssocExtension(rawExt)
		if !ok || !isSafeAssocExtension(ext) {
			continue
		}
		matchedAny := false
		if matched := globs[ext]; len(matched) > 0 {
			for _, mt := range matched {
				if addMIME(mt, true) {
					matchedAny = true
				}
			}
		}
		if base := baselineLinuxMIMEs[ext]; len(base) > 0 {
			if !hasSystemGlobs || !matchedAny || ext == ".md" || ext == ".markdown" || ext == ".mdx" || ext == ".txt" {
				for _, mt := range base {
					if addMIME(mt, false) {
						matchedAny = true
					}
				}
			}
		}
		if !matchedAny {
			if mt := mime.TypeByExtension(ext); mt != "" {
				if addMIME(mt, false) {
					matchedAny = true
				}
			}
		}
		// Any valid text/code/config extension without a dedicated system MIME
		// type is sniffed as text/plain by shared-mime-info.
		if !matchedAny {
			addMIME("text/plain", false)
		}
	}
	return unionSortedStrings(mimeList)
}

type linuxDesktopEntry struct {
	MIMETypes []string
	Exec      string
	TryExec   string
	Hidden    bool
	NoDisplay bool
}

// parseLinuxDesktopEntry parses the [Desktop Entry] section of a Freedesktop
// .desktop file.
func parseLinuxDesktopEntry(raw []byte) linuxDesktopEntry {
	var entry linuxDesktopEntry
	inDesktopEntry := false
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 8*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = (line == "[Desktop Entry]")
			continue
		}
		if !inDesktopEntry {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "Hidden":
			if strings.EqualFold(val, "true") {
				entry.Hidden = true
			}
		case "NoDisplay":
			if strings.EqualFold(val, "true") {
				entry.NoDisplay = true
			}
		case "Exec":
			entry.Exec = val
		case "TryExec":
			entry.TryExec = val
		case "MimeType":
			for _, part := range strings.Split(val, ";") {
				if mt, valid := normalizeRawLinuxMIMEType(part); valid {
					entry.MIMETypes = append(entry.MIMETypes, mt)
				}
			}
		}
	}
	entry.MIMETypes = unionSortedStrings(entry.MIMETypes)
	return entry
}

// parseLinuxDesktopMIMETypes extracts MimeType= entries and Hidden flag from a
// Freedesktop .desktop file.
func parseLinuxDesktopMIMETypes(raw []byte) (mimes []string, hidden bool) {
	entry := parseLinuxDesktopEntry(raw)
	if entry.Hidden {
		return nil, true
	}
	return entry.MIMETypes, false
}

// unescapeDesktopStringValue reverses Freedesktop Desktop Entry string-value
// escape sequences (\s, \n, \t, \r, \\).
func unescapeDesktopStringValue(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			switch raw[i] {
			case 's':
				b.WriteByte(' ')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte('\\')
				b.WriteByte(raw[i])
			}
			continue
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

// splitDesktopExecTokens parses a Freedesktop Exec= value into argv tokens
// per the Desktop Entry Specification (handles value-level escaping, double
// quotes, and quoted-argument backslash escapes).
func splitDesktopExecTokens(raw string) ([]string, bool) {
	s := unescapeDesktopStringValue(raw)
	var tokens []string
	var cur strings.Builder
	inToken := false
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' && i+1 < len(s) {
				next := s[i+1]
				if next == '"' || next == '`' || next == '$' || next == '\\' {
					cur.WriteByte(next)
					i++
					continue
				}
			}
			if c == '"' {
				inQuote = false
				continue
			}
			cur.WriteByte(c)
			continue
		}
		switch c {
		case ' ', '\t':
			if inToken {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inToken = false
			}
		case '"':
			inQuote = true
			inToken = true
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	if inQuote {
		return nil, false
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens, true
}

func isExecutableCommandValid(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	if strings.ContainsRune(cmd, '/') {
		st, err := os.Stat(cmd)
		return err == nil && !st.IsDir() && (st.Mode()&0o111 != 0)
	}
	_, err := exec.LookPath(cmd)
	return err == nil
}

// isLinuxDesktopEntryLaunchable verifies that a .desktop entry is not Hidden
// and that both TryExec (when set) and the resolved Exec command exist and are
// executable.
func isLinuxDesktopEntryLaunchable(entry linuxDesktopEntry) bool {
	if entry.Hidden {
		return false
	}
	if entry.TryExec != "" {
		tryExec := unescapeDesktopStringValue(entry.TryExec)
		if !isExecutableCommandValid(tryExec) {
			return false
		}
	}
	tokens, ok := splitDesktopExecTokens(entry.Exec)
	if !ok || len(tokens) == 0 {
		return false
	}
	argv0 := tokens[0]
	if !isExecutableCommandValid(argv0) {
		return false
	}
	// If wrapped via /bin/sh -c (for literal '%' paths) or /usr/bin/env, verify
	// the wrapped executable as well.
	base := filepath.Base(argv0)
	if base == "sh" && len(tokens) >= 5 && tokens[1] == "-c" && tokens[2] == `target=$1; shift; exec "$target" "$@"` && tokens[3] == "_" {
		target := strings.ReplaceAll(tokens[4], "%%", "%")
		return isExecutableCommandValid(target)
	}
	if base == "env" {
		for idx := 1; idx < len(tokens); idx++ {
			tok := tokens[idx]
			if tok == "--" {
				if idx+1 < len(tokens) {
					target := strings.ReplaceAll(tokens[idx+1], "%%", "%")
					return isExecutableCommandValid(target)
				}
				return false
			}
			if strings.HasPrefix(tok, "-") || strings.Contains(tok, "=") {
				continue
			}
			target := strings.ReplaceAll(tok, "%%", "%")
			return isExecutableCommandValid(target)
		}
		return false
	}
	return true
}

type linuxMIMEEnv struct {
	dataDirs       []string
	configDirs     []string
	currentDesktop string
	// systemQuery optionally overrides OS tool probing (gio / xdg-mime) in tests.
	systemQuery func(mimeType string) (defaultApp string, registered []string, ok bool)
}

// buildLinuxDesktopExecLine builds the Freedesktop Desktop Entry Exec= value.
// For paths containing literal '%', GIO checks argv[0] before field-code
// expansion yet expands '%' across the entire Exec line on launch. Wrapping
// via fixed-path /bin/sh -c passes the target path as positional parameter $1
// and execs it directly, avoiding both GIO's argv[0] '%' issue and POSIX env's
// '=' variable-assignment ambiguity (e.g. "Block%dev=1.AppImage").
func buildLinuxDesktopExecLine(path string) string {
	encoded := encodeDesktopExecToken(path)
	if strings.Contains(path, "%") {
		encoded = strings.ReplaceAll(encoded, "%", "%%")
		shCmd := encodeDesktopExecToken(`target=$1; shift; exec "$target" "$@"`)
		return "/bin/sh -c " + shCmd + " _ " + encoded + " %F"
	}
	return encoded + " %F"
}

func encodeDesktopExecToken(path string) string {
	const reserved = " \t\n\"'\\><~|&;$*?#()`"
	quoted := path
	if path == "" || strings.ContainsAny(path, reserved) {
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(path); i++ {
			c := path[i]
			if c == '"' || c == '`' || c == '$' || c == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
		b.WriteByte('"')
		quoted = b.String()
	}
	return strings.ReplaceAll(quoted, `\`, `\\`)
}

func isBlockLinuxDesktopID(id string) bool {
	return id == "block.desktop" ||
		id == "block-user.desktop" ||
		id == "block-plugins.desktop" ||
		(strings.HasPrefix(id, "block-assoc-") && strings.HasSuffix(id, ".desktop"))
}

// linuxDesktopEntryCommandTarget extracts the underlying executable command
// targeted by a .desktop file's Exec= line (unwrapping /bin/sh -c and env).
func linuxDesktopEntryCommandTarget(entry linuxDesktopEntry) string {
	tokens, ok := splitDesktopExecTokens(entry.Exec)
	if !ok || len(tokens) == 0 {
		return ""
	}
	target := tokens[0]
	base := filepath.Base(target)
	if base == "sh" && len(tokens) >= 5 && tokens[1] == "-c" && tokens[2] == `target=$1; shift; exec "$target" "$@"` && tokens[3] == "_" {
		target = strings.ReplaceAll(tokens[4], "%%", "%")
	} else if base == "env" {
		target = ""
		for idx := 1; idx < len(tokens); idx++ {
			tok := tokens[idx]
			if tok == "--" {
				if idx+1 < len(tokens) {
					target = strings.ReplaceAll(tokens[idx+1], "%%", "%")
				}
				break
			}
			if strings.HasPrefix(tok, "-") || strings.Contains(tok, "=") {
				continue
			}
			target = strings.ReplaceAll(tok, "%%", "%")
			break
		}
	}
	return strings.TrimSpace(target)
}

func canonicalLinuxExecPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if !strings.Contains(p, "/") {
		if resolved, err := exec.LookPath(p); err == nil && resolved != "" {
			p = resolved
		}
	}
	if abs, err := filepath.Abs(p); err == nil && abs != "" {
		p = abs
	}
	if realPath, err := filepath.EvalSymlinks(p); err == nil && realPath != "" {
		p = realPath
	}
	return filepath.Clean(p)
}

func linuxDesktopEntryTargetsExec(entry linuxDesktopEntry, execPath string) bool {
	want := canonicalLinuxExecPath(execPath)
	got := canonicalLinuxExecPath(linuxDesktopEntryCommandTarget(entry))
	return want != "" && got != "" && want == got
}

func linuxDesktopIDReferencedAsDefault(desktopID string, env linuxMIMEEnv) bool {
	if desktopID == "" {
		return false
	}
	apps := loadLinuxMIMEAppsHierarchy(env)
	for _, f := range apps.configFiles {
		for _, list := range f.defaults {
			for _, id := range list {
				if id == desktopID {
					return true
				}
			}
		}
	}
	for _, dirFiles := range apps.dataDirFiles {
		for _, f := range dirFiles {
			for _, list := range f.defaults {
				for _, id := range list {
					if id == desktopID {
						return true
					}
				}
			}
		}
	}
	return false
}

// linuxDesktopFileLaunchable checks the highest-precedence .desktop file for
// desktopID across XDG dataDirs and reports whether it exists, is not Hidden,
// and has a valid launchable Exec/TryExec (matching GIO's resolution semantics).
func linuxDesktopFileLaunchable(desktopID string, dataDirs []string) bool {
	if desktopID == "" || strings.ContainsAny(desktopID, "/\\") {
		return false
	}
	for _, dir := range dataDirs {
		p := filepath.Join(dir, "applications", desktopID)
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		entry := parseLinuxDesktopEntry(raw)
		if entry.Hidden {
			return false
		}
		return isLinuxDesktopEntryLaunchable(entry)
	}
	return false
}

type linuxMIMEAppsFile struct {
	path     string
	defaults map[string][]string
	added    map[string][]string
	removed  map[string]map[string]struct{}
}

type linuxMIMEAppsHierarchy struct {
	configFiles  []linuxMIMEAppsFile
	dataDirFiles [][]linuxMIMEAppsFile
}

func parseLinuxMIMEAppsFile(path string) (linuxMIMEAppsFile, bool) {
	f, err := os.Open(path)
	if err != nil {
		return linuxMIMEAppsFile{}, false
	}
	defer f.Close()

	data := linuxMIMEAppsFile{
		path:     path,
		defaults: make(map[string][]string),
		added:    make(map[string][]string),
		removed:  make(map[string]map[string]struct{}),
	}
	section := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 8*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		mt := strings.ToLower(strings.TrimSpace(k))
		if mt == "" {
			continue
		}
		for _, part := range strings.Split(v, ";") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			switch section {
			case "[Default Applications]":
				data.defaults[mt] = append(data.defaults[mt], id)
			case "[Added Associations]":
				data.added[mt] = append(data.added[mt], id)
			case "[Removed Associations]":
				if data.removed[mt] == nil {
					data.removed[mt] = make(map[string]struct{})
				}
				data.removed[mt][id] = struct{}{}
			}
		}
	}
	return data, true
}

func linuxDesktopPrefixes(currentDesktop string) []string {
	var desktops []string
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(currentDesktop, ":") {
		if d := strings.ToLower(strings.TrimSpace(raw)); d != "" {
			if _, dup := seen[d]; !dup {
				seen[d] = struct{}{}
				desktops = append(desktops, d)
			}
		}
	}
	return desktops
}

func loadLinuxMIMEAppsHierarchy(env linuxMIMEEnv) linuxMIMEAppsHierarchy {
	desktops := linuxDesktopPrefixes(env.currentDesktop)
	seen := make(map[string]struct{})
	loadDir := func(dir string) []linuxMIMEAppsFile {
		var files []linuxMIMEAppsFile
		tryLoad := func(p string) {
			if _, dup := seen[p]; dup {
				return
			}
			seen[p] = struct{}{}
			if parsed, ok := parseLinuxMIMEAppsFile(p); ok {
				files = append(files, parsed)
			}
		}
		for _, d := range desktops {
			tryLoad(filepath.Join(dir, d+"-mimeapps.list"))
		}
		tryLoad(filepath.Join(dir, "mimeapps.list"))
		return files
	}

	h := linuxMIMEAppsHierarchy{
		dataDirFiles: make([][]linuxMIMEAppsFile, len(env.dataDirs)),
	}
	for _, dir := range env.configDirs {
		h.configFiles = append(h.configFiles, loadDir(dir)...)
	}
	for i, dir := range env.dataDirs {
		appDir := filepath.Join(dir, "applications")
		h.dataDirFiles[i] = loadDir(appDir)
		// Match GLib desktop_file_dir_unindexed_read_mimeapps_lists: within each
		// data directory, defaults.list follows <desktop>-mimeapps.list and
		// mimeapps.list (and precedes lower-priority data directories), and only
		// [Default Applications] is honored.
		defPath := filepath.Join(appDir, "defaults.list")
		if _, dup := seen[defPath]; !dup {
			seen[defPath] = struct{}{}
			if parsed, ok := parseLinuxMIMEAppsFile(defPath); ok {
				parsed.added = nil
				parsed.removed = nil
				h.dataDirFiles[i] = append(h.dataDirFiles[i], parsed)
			}
		}
	}
	return h
}

func cloneLinuxMIMEAppsWithoutPins(h linuxMIMEAppsHierarchy, userMimeappsPath string, prevPinned map[string]string) linuxMIMEAppsHierarchy {
	if len(prevPinned) == 0 || userMimeappsPath == "" {
		return h
	}
	cleanTarget := filepath.Clean(userMimeappsPath)
	out := linuxMIMEAppsHierarchy{
		configFiles:  make([]linuxMIMEAppsFile, len(h.configFiles)),
		dataDirFiles: h.dataDirFiles,
	}
	for i, f := range h.configFiles {
		if filepath.Clean(f.path) != cleanTarget {
			out.configFiles[i] = f
			continue
		}
		newDefaults := make(map[string][]string, len(f.defaults))
		for mt, ids := range f.defaults {
			if pinnedID, ok := prevPinned[mt]; ok && len(ids) == 1 && ids[0] == pinnedID {
				continue
			}
			newDefaults[mt] = ids
		}
		out.configFiles[i] = linuxMIMEAppsFile{
			path:     f.path,
			defaults: newDefaults,
			added:    f.added,
			removed:  f.removed,
		}
	}
	return out
}

// loadLinuxDirMIMEHandlers returns per-XDG-data-dir MIME -> desktop-ID lists
// in strict XDG directory precedence order (matching GIO's gdesktopappinfo.c,
// where user-dir $XDG_DATA_HOME/applications always outranks system dirs like
// /usr/share/applications regardless of desktop-ID alphabetical order).
// Each directory reads mimeinfo.cache and also scans *.desktop files so newly
// installed entries are respected even before update-desktop-database runs.
func loadLinuxDirMIMEHandlers(dataDirs []string) []map[string][]string {
	perDir := make([]map[string][]string, len(dataDirs))
	for i, dir := range dataDirs {
		dirMap := make(map[string][]string)
		seenPerMIME := make(map[string]map[string]struct{})
		addHandler := func(mt, id string) {
			if mt == "" || id == "" {
				return
			}
			if seenPerMIME[mt] == nil {
				seenPerMIME[mt] = make(map[string]struct{})
			}
			if _, dup := seenPerMIME[mt][id]; dup {
				return
			}
			seenPerMIME[mt][id] = struct{}{}
			dirMap[mt] = append(dirMap[mt], id)
		}

		appDir := filepath.Join(dir, "applications")
		if f, err := os.Open(filepath.Join(appDir, "mimeinfo.cache")); err == nil {
			inCache := false
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 8*1024), 256*1024)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
					inCache = (line == "[MIME Cache]")
					continue
				}
				if !inCache {
					continue
				}
				k, v, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				mt := strings.ToLower(strings.TrimSpace(k))
				for _, part := range strings.Split(v, ";") {
					addHandler(mt, strings.TrimSpace(part))
				}
			}
			_ = f.Close()
		}

		if entries, err := os.ReadDir(appDir); err == nil {
			names := make([]string, 0, len(entries))
			for _, de := range entries {
				if de.IsDir() || !strings.HasSuffix(de.Name(), ".desktop") {
					continue
				}
				names = append(names, de.Name())
			}
			sort.Strings(names)
			for _, name := range names {
				raw, err := os.ReadFile(filepath.Join(appDir, name))
				if err != nil {
					continue
				}
				entry := parseLinuxDesktopEntry(raw)
				if !isLinuxDesktopEntryLaunchable(entry) {
					continue
				}
				for _, mt := range entry.MIMETypes {
					addHandler(mt, name)
				}
			}
		}

		perDir[i] = dirMap
	}
	return perDir
}

func loadLinuxMIMESubclasses(dataDirs []string) map[string][]string {
	out := make(map[string][]string)
	add := func(child, parent string) {
		for _, existing := range out[child] {
			if existing == parent {
				return
			}
		}
		out[child] = append(out[child], parent)
	}
	for _, dir := range dataDirs {
		f, err := os.Open(filepath.Join(dir, "mime", "subclasses"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 8*1024), 256*1024)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 {
				add(strings.ToLower(fields[0]), strings.ToLower(fields[1]))
			}
		}
		_ = f.Close()
	}
	return out
}

func linuxParentMIMETypes(mt string, subclasses map[string][]string) []string {
	var parents []string
	seen := make(map[string]struct{})
	add := func(p string) {
		if p == "" || p == mt {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		parents = append(parents, p)
	}
	for _, p := range subclasses[mt] {
		add(p)
	}
	// Per shared-mime-info conventions, all text/* (except text/plain) and known
	// text/code application/* formats inherit from text/plain.
	if mt != "text/plain" {
		if strings.HasPrefix(mt, "text/") {
			add("text/plain")
		} else if _, ok := allowedLinuxApplicationMIMEs[mt]; ok {
			add("text/plain")
		}
	}
	return parents
}

// resolveLinuxEffectiveDefaultApp models Freedesktop MIME-Apps 1.0 + GIO
// (gdesktopappinfo.c) default-application resolution for mimeType:
//  1. Explicit [Default Applications] across all mimeapps.list files in strict
//     XDG precedence order (configDirs first, then dataDirs/applications).
//  2. Legacy defaults.list [Default Applications] across dataDirs/applications
//     in XDG precedence order (matching GIO's mime_info_cache_dir_init_defaults_list_cb).
//  3. Per-directory merge of [Added Associations], [Removed Associations]
//     (propagating downward to lower-precedence directories only), and each
//     data directory's mimeinfo.cache / .desktop handlers in strict XDG
//     precedence order (configDirs -> dataDirs[0] mimeapps + cache ->
//     dataDirs[1] mimeapps + cache -> ...). This matches GLib's
//     get_all_desktop_entries_for_mime_type, where user-dir cache at
//     dataDirs[0] outranks /usr/share/applications/mimeapps.list [Added Associations].
//  4. If no handler exists for this MIME type, walk parent MIME types via
//     shared-mime-info subclasses.
func resolveLinuxEffectiveDefaultApp(
	mimeType string,
	env linuxMIMEEnv,
	apps linuxMIMEAppsHierarchy,
	perDirHandlers []map[string][]string,
	subclasses map[string][]string,
	extraDesktopID string,
	extraMIMEs map[string]struct{},
	ignoreDesktopID string,
) string {
	return resolveLinuxEffectiveDefaultAppAtDir(
		mimeType, env, apps, perDirHandlers, subclasses,
		extraDesktopID, 0, extraMIMEs, ignoreDesktopID,
	)
}

func resolveLinuxEffectiveDefaultAppAtDir(
	mimeType string,
	env linuxMIMEEnv,
	apps linuxMIMEAppsHierarchy,
	perDirHandlers []map[string][]string,
	subclasses map[string][]string,
	extraDesktopID string,
	extraDirIdx int,
	extraMIMEs map[string]struct{},
	ignoreDesktopID string,
) string {
	visited := make(map[string]struct{})
	queue := []string{strings.ToLower(strings.TrimSpace(mimeType))}
	isAvailable := func(id string) bool {
		if id == "" || id == ignoreDesktopID {
			return false
		}
		if id == extraDesktopID {
			return true
		}
		return linuxDesktopFileLaunchable(id, env.dataDirs)
	}

	for len(queue) > 0 {
		mt := queue[0]
		queue = queue[1:]
		if mt == "" {
			continue
		}
		if _, seen := visited[mt]; seen {
			continue
		}
		visited[mt] = struct{}{}

		_, hasExtra := extraMIMEs[mt]

		// 1. [Default Applications] across mimeapps.list and legacy defaults.list
		// in strict XDG precedence order (configDirs, then per-dataDir mimeapps + defaults.list).
		for _, f := range apps.configFiles {
			for _, id := range f.defaults[mt] {
				if id == extraDesktopID && hasExtra {
					return id
				}
				if isAvailable(id) {
					return id
				}
			}
		}
		for _, dirFiles := range apps.dataDirFiles {
			for _, f := range dirFiles {
				for _, id := range f.defaults[mt] {
					if id == extraDesktopID && hasExtra {
						return id
					}
					if isAvailable(id) {
						return id
					}
				}
			}
		}

		// 2. Per-directory [Added Associations] + mimeinfo.cache / .desktop
		// handlers with downward-only [Removed Associations] blacklisting.
		blacklist := make(map[string]struct{})
		checkAdded := func(f linuxMIMEAppsFile) (string, bool) {
			for _, id := range f.added[mt] {
				if _, isRm := blacklist[id]; isRm {
					continue
				}
				if id == extraDesktopID && hasExtra {
					return id, true
				}
				if isAvailable(id) {
					return id, true
				}
			}
			for id := range f.removed[mt] {
				blacklist[id] = struct{}{}
			}
			return "", false
		}

		for _, f := range apps.configFiles {
			if winner, ok := checkAdded(f); ok {
				return winner
			}
		}

		for dirIdx, dirMap := range perDirHandlers {
			if dirIdx < len(apps.dataDirFiles) {
				for _, f := range apps.dataDirFiles[dirIdx] {
					if winner, ok := checkAdded(f); ok {
						return winner
					}
				}
			}
			if dirIdx == extraDirIdx && hasExtra && extraDesktopID != "" {
				if _, isRm := blacklist[extraDesktopID]; !isRm {
					// extraDesktopID resides in dataDirs[extraDirIdx]/applications.
					// Treat it conservatively as winning any implicit tie inside
					// that directory as well as outranking lower directories,
					// matching GIO cache/alphabetical ordering.
					return extraDesktopID
				}
			}
			for _, id := range dirMap[mt] {
				if _, isRm := blacklist[id]; isRm {
					continue
				}
				if isAvailable(id) {
					return id
				}
			}
		}

		for _, parent := range linuxParentMIMETypes(mt, subclasses) {
			if _, seen := visited[parent]; !seen {
				queue = append(queue, parent)
			}
		}
	}
	return ""
}

func linuxMIMECommandEnv(env linuxMIMEEnv) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "XDG_DATA_HOME", "XDG_DATA_DIRS", "XDG_CONFIG_HOME", "XDG_CONFIG_DIRS", "XDG_CURRENT_DESKTOP", "LC_ALL", "LANG", "LANGUAGE":
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "LC_ALL=C", "LANG=C")
	if len(env.dataDirs) > 0 {
		out = append(out, "XDG_DATA_HOME="+env.dataDirs[0])
		if len(env.dataDirs) > 1 {
			out = append(out, "XDG_DATA_DIRS="+strings.Join(env.dataDirs[1:], ":"))
		} else {
			// Prevent GLib from falling back to host /usr/share in isolated environments.
			out = append(out, "XDG_DATA_DIRS="+env.dataDirs[0])
		}
	}
	if len(env.configDirs) > 0 {
		out = append(out, "XDG_CONFIG_HOME="+env.configDirs[0])
		if len(env.configDirs) > 1 {
			out = append(out, "XDG_CONFIG_DIRS="+strings.Join(env.configDirs[1:], ":"))
		} else {
			out = append(out, "XDG_CONFIG_DIRS="+env.configDirs[0])
		}
	}
	out = append(out, "XDG_CURRENT_DESKTOP="+env.currentDesktop)
	return out
}

func parseGIOMimeOutput(raw string) (defaultApp string, registered []string, ok bool) {
	inRegistered := false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Default application for ") {
			if idx := strings.LastIndex(trimmed, ":"); idx >= 0 {
				defaultApp = strings.TrimSpace(trimmed[idx+1:])
				ok = true
			}
			inRegistered = false
			continue
		}
		if strings.HasPrefix(trimmed, "No default applications for ") {
			defaultApp = ""
			ok = true
			inRegistered = false
			continue
		}
		if strings.HasPrefix(trimmed, "Registered applications:") {
			inRegistered = true
			continue
		}
		if strings.HasPrefix(trimmed, "Recommended applications:") || strings.HasPrefix(trimmed, "No registered applications") {
			inRegistered = false
			continue
		}
		if inRegistered && (strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ")) {
			registered = append(registered, trimmed)
		}
	}
	return defaultApp, registered, ok
}

// queryLinuxSystemDefaultApp queries the host OS's real Freedesktop/GIO default
// application resolver (preferring `gio mime`, falling back to `xdg-mime query default`)
// under env's exact XDG directory variables.
func queryLinuxSystemDefaultApp(mimeType string, env linuxMIMEEnv) (defaultApp string, registered []string, ok bool) {
	if env.systemQuery != nil {
		return env.systemQuery(mimeType)
	}
	if runtime.GOOS != "linux" {
		return "", nil, false
	}
	cmdEnv := linuxMIMECommandEnv(env)
	if gioBin, err := exec.LookPath("gio"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, gioBin, "mime", mimeType)
		cmd.Env = cmdEnv
		out, err := cmd.Output()
		cancel()
		if err == nil {
			if def, reg, parsed := parseGIOMimeOutput(string(out)); parsed {
				return def, reg, true
			}
		}
	}
	if xdgBin, err := exec.LookPath("xdg-mime"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, xdgBin, "query", "default", mimeType)
		cmd.Env = cmdEnv
		out, err := cmd.Output()
		cancel()
		if err == nil {
			return strings.TrimSpace(string(out)), nil, true
		}
	}
	return "", nil, false
}

// filterNonHijackingLinuxHelperMIMEs filters candidate MIME types for the
// runtime helper desktop entry (block-plugins.desktop), omitting any MIME type
// whose effective GIO/Freedesktop default application would change away from an
// existing non-Block handler.
func filterNonHijackingLinuxHelperMIMEs(candidates []string, env linuxMIMEEnv, helperDesktopID string) []string {
	safe, _ := planLinuxHelperMIMEs(candidates, env, helperDesktopID, "", nil)
	return safe
}

// planLinuxHelperMIMEs classifies candidate MIME types for block-plugins.desktop
// into:
//   - safeWithoutPin: MIME types that can be added to block-plugins.desktop
//     directly without changing the current effective default application.
//   - needsPin: MIME types whose implicit default app would otherwise be
//     displaced by adding a user-level .desktop entry, mapped to the existing
//     effective default app ID that must be preserved in $XDG_CONFIG_HOME/mimeapps.list.
func planLinuxHelperMIMEs(
	candidates []string,
	env linuxMIMEEnv,
	helperDesktopID string,
	userMimeappsPath string,
	prevPinned map[string]string,
) (safeWithoutPin []string, needsPin map[string]string) {
	safe, pin, _ := planLinuxDesktopMIMEs(candidates, env, helperDesktopID, 0, userMimeappsPath, prevPinned)
	return safe, pin
}

func planLinuxDesktopMIMEs(
	candidates []string,
	env linuxMIMEEnv,
	desktopID string,
	targetDirIdx int,
	targetMimeappsPath string,
	prevPinned map[string]string,
) (safeWithoutPin []string, needsPin map[string]string, expectedDefaults map[string]string) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}
	actualApps := loadLinuxMIMEAppsHierarchy(env)
	baseApps := cloneLinuxMIMEAppsWithoutPins(actualApps, targetMimeappsPath, prevPinned)
	perDir := loadLinuxDirMIMEHandlers(env.dataDirs)
	subclasses := loadLinuxMIMESubclasses(env.dataDirs)

	// Detect which MIME types desktopID currently declares on disk (if any) so we
	// only query the live system resolver for MIME types not already affected by
	// an existing desktopID or our own previous pin.
	existingDesktopMIMEs := make(map[string]struct{})
	if targetDirIdx >= 0 && targetDirIdx < len(env.dataDirs) {
		p := filepath.Join(env.dataDirs[targetDirIdx], "applications", desktopID)
		if raw, err := os.ReadFile(p); err == nil {
			entry := parseLinuxDesktopEntry(raw)
			for _, mt := range entry.MIMETypes {
				existingDesktopMIMEs[mt] = struct{}{}
			}
		}
	}

	needsPin = make(map[string]string)
	expectedDefaults = make(map[string]string, len(candidates))
	for _, mt := range candidates {
		beforeBase := resolveLinuxEffectiveDefaultAppAtDir(
			mt, env, baseApps, perDir, subclasses,
			"", targetDirIdx, nil, desktopID,
		)
		afterBase := resolveLinuxEffectiveDefaultAppAtDir(
			mt, env, baseApps, perDir, subclasses,
			desktopID, targetDirIdx, map[string]struct{}{mt: {}}, desktopID,
		)

		// When desktopID is not yet registered for mt and we don't have an active
		// pin for mt, consult the host OS's real resolver (gio / xdg-mime) so any
		// distro-specific default mechanism (defaults.list, desktop overrides, etc.)
		// is respected verbatim. Compare strictly against desktopID (not all Block
		// IDs) so one Block installation never displaces a different Block
		// installation's default.
		_, alreadyInDesktop := existingDesktopMIMEs[mt]
		_, alreadyPinned := prevPinned[mt]
		if !alreadyInDesktop && !alreadyPinned {
			if sysDef, _, sysOK := queryLinuxSystemDefaultApp(mt, env); sysOK && sysDef != "" && sysDef != desktopID {
				if sysDef != beforeBase {
					beforeBase = sysDef
					// If our static model did not already predict sysDef via an
					// explicit default above targetDirIdx, require pinning sysDef
					// so adding desktopID cannot displace the real system default.
					if afterBase != sysDef {
						afterBase = desktopID
					}
				}
			}
		}

		if beforeBase != "" && beforeBase != desktopID {
			expectedDefaults[mt] = beforeBase
		}

		if beforeBase == "" || beforeBase == desktopID || afterBase == beforeBase {
			safeWithoutPin = append(safeWithoutPin, mt)
			continue
		}
		needsPin[mt] = beforeBase
	}
	if len(needsPin) == 0 {
		needsPin = nil
	}
	return safeWithoutPin, needsPin, expectedDefaults
}

// linuxMimeappsBeforeCommitHook is nil in production and used by tests to
// simulate a concurrent external settings write between read and atomic commit.
var linuxMimeappsBeforeCommitHook func()

func mergeLinuxMimeappsLines(
	lines []string,
	prevPinned map[string]string,
	desiredPins map[string]string,
) (outLines []string, activePinned map[string]string, changed bool) {
	activePinned = make(map[string]string, len(desiredPins))
	handledDesired := make(map[string]bool, len(desiredPins))
	outLines = make([]string, 0, len(lines)+len(desiredPins)+2)

	section := ""
	defSectionFound := false
	defInsertIdx := -1

	formatPinLine := func(mt, appID string) string {
		return mt + "=" + appID + ";"
	}
	parseSingleApp := func(val string) (string, bool) {
		var ids []string
		for _, part := range strings.Split(val, ";") {
			if p := strings.TrimSpace(part); p != "" {
				ids = append(ids, p)
			}
		}
		if len(ids) == 1 {
			return ids[0], true
		}
		return "", false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if section == "[Default Applications]" {
				defInsertIdx = len(outLines)
			}
			section = trimmed
			if section == "[Default Applications]" {
				defSectionFound = true
			}
			outLines = append(outLines, line)
			continue
		}
		if section == "[Default Applications]" && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if k, v, ok := strings.Cut(line, "="); ok {
				mt := strings.ToLower(strings.TrimSpace(k))
				prevVal, wasPinned := prevPinned[mt]
				wantVal, wantPin := desiredPins[mt]
				curSingle, isSingle := parseSingleApp(v)
				matchesPrevPin := wasPinned && isSingle && curSingle == prevVal

				if wantPin {
					handledDesired[mt] = true
					if matchesPrevPin {
						if wantVal != prevVal {
							outLines = append(outLines, formatPinLine(mt, wantVal))
							changed = true
						} else {
							outLines = append(outLines, line)
						}
						activePinned[mt] = wantVal
						continue
					}
					// Existing user-defined [Default Applications] entry: never overwrite.
					outLines = append(outLines, line)
					continue
				}
				if matchesPrevPin {
					// No longer needed and still untouched by user: remove cleanly.
					changed = true
					continue
				}
			}
		}
		outLines = append(outLines, line)
	}
	if section == "[Default Applications]" {
		defInsertIdx = len(outLines)
	}

	var toInsert []string
	desiredKeys := make([]string, 0, len(desiredPins))
	for mt := range desiredPins {
		if !handledDesired[mt] {
			desiredKeys = append(desiredKeys, mt)
		}
	}
	sort.Strings(desiredKeys)
	for _, mt := range desiredKeys {
		wantVal := desiredPins[mt]
		if wantVal == "" {
			continue
		}
		toInsert = append(toInsert, formatPinLine(mt, wantVal))
		activePinned[mt] = wantVal
	}

	if len(toInsert) > 0 {
		changed = true
		if defSectionFound && defInsertIdx >= 0 {
			updated := make([]string, 0, len(outLines)+len(toInsert))
			updated = append(updated, outLines[:defInsertIdx]...)
			updated = append(updated, toInsert...)
			updated = append(updated, outLines[defInsertIdx:]...)
			outLines = updated
		} else {
			if len(outLines) > 0 && strings.TrimSpace(outLines[len(outLines)-1]) != "" {
				outLines = append(outLines, "")
			}
			outLines = append(outLines, "[Default Applications]")
			outLines = append(outLines, toInsert...)
		}
	}
	return outLines, activePinned, changed
}

// reconcileLinuxPinnedDefaults updates [Default Applications] in
// <configDir>/mimeapps.list to preserve implicit non-Block defaults while
// never overwriting user-chosen defaults, cleanly removing Block-managed pins
// that are no longer needed, and detecting concurrent external edits (e.g.
// from GNOME/KDE Settings) before commit so their changes are merged rather
// than clobbered by a stale snapshot.
func reconcileLinuxPinnedDefaults(
	configHome string,
	prevPinned map[string]string,
	desiredPins map[string]string,
) (map[string]string, error) {
	if strings.TrimSpace(configHome) == "" {
		return nil, nil
	}
	if len(prevPinned) == 0 && len(desiredPins) == 0 {
		return nil, nil
	}
	mimeappsPath := filepath.Join(configHome, "mimeapps.list")
	const maxAttempts = 6

	for attempt := 0; attempt < maxAttempts; attempt++ {
		rawBefore, err := os.ReadFile(mimeappsPath)
		existed := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !existed && len(desiredPins) == 0 {
			return nil, nil
		}
		var stBefore os.FileInfo
		if existed {
			stBefore, _ = os.Stat(mimeappsPath)
		}

		var lines []string
		if existed {
			text := strings.ReplaceAll(string(rawBefore), "\r\n", "\n")
			text = strings.TrimSuffix(text, "\n")
			if text != "" {
				lines = strings.Split(text, "\n")
			}
		}

		outLines, activePinned, changed := mergeLinuxMimeappsLines(lines, prevPinned, desiredPins)
		if !changed {
			if len(activePinned) == 0 {
				return nil, nil
			}
			return activePinned, nil
		}

		var payload []byte
		if len(outLines) > 0 {
			payload = []byte(strings.Join(outLines, "\n") + "\n")
		}

		if err := os.MkdirAll(configHome, 0o755); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(configHome, ".block-mimeapps-*.tmp")
		if err != nil {
			return nil, err
		}
		tmpPath := tmp.Name()
		cleanupTmp := func() {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
		if _, err := tmp.Write(payload); err != nil {
			cleanupTmp()
			return nil, err
		}
		_ = tmp.Sync()
		if err := tmp.Chmod(0o644); err != nil {
			cleanupTmp()
			return nil, err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}

		if linuxMimeappsBeforeCommitHook != nil {
			linuxMimeappsBeforeCommitHook()
		}

		// Re-check the target file right before committing so any concurrent edit
		// made by a desktop settings app between read and commit is merged
		// instead of overwritten.
		rawNow, errNow := os.ReadFile(mimeappsPath)
		nowExists := errNow == nil
		if errNow != nil && !os.IsNotExist(errNow) {
			_ = os.Remove(tmpPath)
			return nil, errNow
		}
		concurrentChange := false
		if existed != nowExists {
			concurrentChange = true
		} else if existed {
			if !bytes.Equal(rawBefore, rawNow) {
				concurrentChange = true
			} else if stNow, stErrNow := os.Stat(mimeappsPath); stBefore != nil && stErrNow == nil {
				if !os.SameFile(stBefore, stNow) || !stBefore.ModTime().Equal(stNow.ModTime()) {
					concurrentChange = true
				}
			}
		}
		if concurrentChange {
			_ = os.Remove(tmpPath)
			continue
		}

		if err := os.Rename(tmpPath, mimeappsPath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}
		if len(activePinned) == 0 {
			return nil, nil
		}
		return activePinned, nil
	}

	return nil, fmt.Errorf("concurrent modification conflict updating %s", mimeappsPath)
}

// verifyLinuxPinnedMIMEs reloads the XDG mimeapps hierarchy after pinning and
// returns only those MIME types in needsPin whose effective default app remains
// unchanged when helperDesktopID is registered for them.
func verifyLinuxPinnedMIMEs(
	needsPin map[string]string,
	env linuxMIMEEnv,
	helperDesktopID string,
) (verified []string, failed []string) {
	return verifyLinuxPinnedMIMEsAtDir(needsPin, env, helperDesktopID, 0)
}

func verifyLinuxPinnedMIMEsAtDir(
	needsPin map[string]string,
	env linuxMIMEEnv,
	desktopID string,
	targetDirIdx int,
) (verified []string, failed []string) {
	if len(needsPin) == 0 {
		return nil, nil
	}
	apps := loadLinuxMIMEAppsHierarchy(env)
	perDir := loadLinuxDirMIMEHandlers(env.dataDirs)
	subclasses := loadLinuxMIMESubclasses(env.dataDirs)

	keys := make([]string, 0, len(needsPin))
	for mt := range needsPin {
		keys = append(keys, mt)
	}
	sort.Strings(keys)
	for _, mt := range keys {
		wantDefault := needsPin[mt]
		after := resolveLinuxEffectiveDefaultAppAtDir(
			mt,
			env,
			apps,
			perDir,
			subclasses,
			desktopID,
			targetDirIdx,
			map[string]struct{}{mt: {}},
			desktopID,
		)
		if after == wantDefault {
			verified = append(verified, mt)
		} else {
			failed = append(failed, mt)
		}
	}
	return verified, failed
}

// verifyLinuxSystemRegistration verifies, after the .desktop file and
// update-desktop-database have been committed, that every registered MIME type
// preserves its expected non-Block default according to the host OS resolver
// (gio / xdg-mime when available, falling back to our XDG parser).
func verifyLinuxSystemRegistration(
	mimeList []string,
	expectedDefaults map[string]string,
	env linuxMIMEEnv,
	desktopID string,
	targetDirIdx int,
) (okMIMEs []string, badMIMEs []string) {
	if len(mimeList) == 0 {
		return nil, nil
	}
	apps := loadLinuxMIMEAppsHierarchy(env)
	perDir := loadLinuxDirMIMEHandlers(env.dataDirs)
	subclasses := loadLinuxMIMESubclasses(env.dataDirs)

	for _, mt := range mimeList {
		wantDef := expectedDefaults[mt]
		sysDef, sysReg, sysOK := queryLinuxSystemDefaultApp(mt, env)
		if sysOK {
			if wantDef != "" && wantDef != desktopID && sysDef != wantDef {
				badMIMEs = append(badMIMEs, mt)
				continue
			}
			// If gio reported a Registered applications list, verify desktopID
			// is actually visible for Open With (unless desktopID itself is default).
			if len(sysReg) > 0 && sysDef != desktopID {
				found := false
				for _, id := range sysReg {
					if id == desktopID {
						found = true
						break
					}
				}
				if !found {
					badMIMEs = append(badMIMEs, mt)
					continue
				}
			}
			okMIMEs = append(okMIMEs, mt)
			continue
		}

		// Fallback to our full XDG parser (including defaults.list).
		if wantDef != "" && wantDef != desktopID {
			after := resolveLinuxEffectiveDefaultAppAtDir(
				mt, env, apps, perDir, subclasses,
				"", targetDirIdx, nil, "",
			)
			if after != wantDef {
				badMIMEs = append(badMIMEs, mt)
				continue
			}
		}
		okMIMEs = append(okMIMEs, mt)
	}
	return okMIMEs, badMIMEs
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func unionStringMaps(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func runLinuxDesktopDBUpdate(desktopDir string) error {
	bin, err := exec.LookPath("update-desktop-database")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, desktopDir).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("update-desktop-database: %w (%s)", err, msg)
		}
		return fmt.Errorf("update-desktop-database: %w", err)
	}
	return nil
}

// LinuxDesktopInstallOptions configures protected installation or cleanup of
// a main Linux desktop entry (user-level block-user.desktop via install-app.mjs
// or system-level block.desktop).
type LinuxDesktopInstallOptions struct {
	ExecPath        string
	DesktopID       string
	TargetDataDir   string
	TargetConfigDir string
	StateDir        string
	DataDirs        []string
	ConfigDirs      []string
	CurrentDesktop  string
	SystemMode      bool
	// SystemQuery optionally overrides gio/xdg-mime resolution (for tests).
	SystemQuery func(mimeType string) (defaultApp string, registered []string, ok bool)
	// UpdateDesktopDB optionally overrides update-desktop-database (for tests).
	UpdateDesktopDB func(desktopDir string) error
}

func buildMainLinuxDesktopContent(execPath string, mimeList []string) string {
	lines := []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Block",
		"Exec=" + buildLinuxDesktopExecLine(execPath),
		"Icon=block",
		"Categories=Office;TextEditor;Utility;",
	}
	if len(mimeList) > 0 {
		lines = append(lines, "MimeType="+strings.Join(mimeList, ";")+";")
	}
	lines = append(lines, "Terminal=false", "StartupWMClass=block", "")
	return strings.Join(lines, "\n")
}

// InstallLinuxDesktopEntry installs or updates block.desktop under
// opts.TargetDataDir/applications while running full target-machine default
// application protection BEFORE publishing MimeType= entries. Existing implicit
// defaults that block.desktop would otherwise displace are preserved in
// opts.TargetConfigDir/mimeapps.list (journaled to StateDir before mutation,
// verified after update-desktop-database, and rolled back on failure).
func InstallLinuxDesktopEntry(opts LinuxDesktopInstallOptions) error {
	if strings.TrimSpace(opts.ExecPath) == "" {
		return fmt.Errorf("exec path is required")
	}
	if strings.ContainsAny(opts.ExecPath, "\r\n\x00") {
		return fmt.Errorf("exec path contains unsupported control characters")
	}
	if strings.TrimSpace(opts.TargetDataDir) == "" {
		return fmt.Errorf("target data dir is required")
	}

	if opts.StateDir != "" {
		unlock, err := acquireFileAssocLock(opts.StateDir, 10*time.Second)
		if err != nil {
			return fmt.Errorf("lock file association state: %w", err)
		}
		defer unlock()
	}

	dataDirs := opts.DataDirs
	if len(dataDirs) == 0 {
		if opts.SystemMode {
			dataDirs = linuxXDGDataDirsFromEnv("", os.Getenv("XDG_DATA_DIRS"))
		} else {
			dataDirs = linuxXDGDataDirsFromEnv(opts.TargetDataDir, os.Getenv("XDG_DATA_DIRS"))
		}
	}
	targetDataClean := filepath.Clean(opts.TargetDataDir)
	targetDirIdx := -1
	for i, d := range dataDirs {
		if filepath.Clean(d) == targetDataClean {
			targetDirIdx = i
			break
		}
	}
	if targetDirIdx < 0 {
		dataDirs = append([]string{targetDataClean}, dataDirs...)
		targetDirIdx = 0
	}

	configDirs := opts.ConfigDirs
	if len(configDirs) == 0 {
		if opts.SystemMode {
			configDirs = linuxXDGConfigDirsFromEnv("", os.Getenv("XDG_CONFIG_DIRS"))
		} else {
			configDirs = linuxXDGConfigDirsFromEnv(opts.TargetConfigDir, os.Getenv("XDG_CONFIG_DIRS"))
		}
	}
	if opts.TargetConfigDir != "" {
		cfgClean := filepath.Clean(opts.TargetConfigDir)
		foundCfg := false
		for _, d := range configDirs {
			if filepath.Clean(d) == cfgClean {
				foundCfg = true
				break
			}
		}
		if !foundCfg {
			configDirs = append([]string{cfgClean}, configDirs...)
		}
	}

	env := linuxMIMEEnv{
		dataDirs:       dataDirs,
		configDirs:     configDirs,
		currentDesktop: opts.CurrentDesktop,
		systemQuery:    opts.SystemQuery,
	}

	// Aggregate both built-in package MIMEs and any target-machine MIMEs mapped
	// to Block's built-in extensions.
	dataHomeForResolve := ""
	var extraDataDirs []string
	if len(dataDirs) > 0 {
		dataHomeForResolve = dataDirs[0]
		if len(dataDirs) > 1 {
			extraDataDirs = dataDirs[1:]
		}
	}
	resolved := ResolveLinuxMIMETypesForExtensions(BuiltinAssocExtensions(), dataHomeForResolve, strings.Join(extraDataDirs, ":"))
	candidates := unionSortedStrings(LinuxPackageMIMETypes(), resolved)

	var prev fileAssocState
	if opts.StateDir != "" {
		prev, _ = loadFileAssocState(opts.StateDir)
	}
	prevMainPins := cloneStringMap(prev.PinnedLinuxMainDefaults)

	targetMimeappsPath := ""
	if opts.TargetConfigDir != "" {
		targetMimeappsPath = filepath.Join(opts.TargetConfigDir, "mimeapps.list")
	}

	desktopID := strings.TrimSpace(opts.DesktopID)
	if desktopID == "" {
		if opts.SystemMode {
			desktopID = "block.desktop"
		} else {
			// User-level installs use a dedicated desktop ID so they never
			// shadow a system /usr/share/applications/block.desktop that the
			// user may have configured as their default handler.
			desktopID = "block-user.desktop"
		}
	}
	if strings.ContainsAny(desktopID, "/\\") || !strings.HasSuffix(desktopID, ".desktop") {
		return fmt.Errorf("invalid desktop ID %q", desktopID)
	}

	safeDirect, needsPin, expectedDefaults := planLinuxDesktopMIMEs(
		candidates, env, desktopID, targetDirIdx, targetMimeappsPath, prevMainPins,
	)

	// Write-ahead ownership journal BEFORE mutating mimeapps.list or desktop
	// files so a crash or unwritable state path never orphans Block-managed
	// pins or leaves dangling desktop entries behind.
	saveMainPins := func(pins map[string]string) error {
		if opts.StateDir == "" {
			return nil
		}
		st := prev
		st.LinuxMainDesktopID = desktopID
		st.PinnedLinuxMainDefaults = cloneStringMap(pins)
		return saveFileAssocState(opts.StateDir, st)
	}

	activePinned := cloneStringMap(prevMainPins)
	var verifiedPinned []string
	if len(needsPin) > 0 || len(prevMainPins) > 0 {
		if err := saveMainPins(unionStringMaps(prevMainPins, needsPin)); err != nil {
			return fmt.Errorf("persist file association journal: %w", err)
		}
		pinned, pinErr := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, prevMainPins, needsPin)
		if pinErr != nil {
			// Cannot safely pin in TargetConfigDir (e.g. read-only /etc/xdg);
			// restore journaled state and omit displacement-risk MIME types.
			activePinned = cloneStringMap(prevMainPins)
			_ = saveMainPins(activePinned)
			needsPin = nil
		} else {
			activePinned = pinned
			if len(needsPin) > 0 {
				var failed []string
				verifiedPinned, failed = verifyLinuxPinnedMIMEsAtDir(needsPin, env, desktopID, targetDirIdx)
				if len(failed) > 0 {
					for _, mt := range failed {
						delete(needsPin, mt)
					}
					if rolled, rollErr := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, activePinned, needsPin); rollErr == nil {
						activePinned = rolled
					}
				}
			}
		}
	}

	mimeList := unionSortedStrings(safeDirect, verifiedPinned)
	desktopDir := filepath.Join(targetDataClean, "applications")
	desktopPath := filepath.Join(desktopDir, desktopID)
	prevDesktopBytes, hadPrevDesktop := readFileIfExists(desktopPath)

	updateDB := opts.UpdateDesktopDB
	if updateDB == nil {
		updateDB = runLinuxDesktopDBUpdate
	}

	rollbackAll := func() {
		if hadPrevDesktop {
			_ = writeFileAtomic(desktopPath, prevDesktopBytes, 0o644)
		} else {
			_ = os.Remove(desktopPath)
		}
		_ = updateDB(desktopDir)
		if rolled, err := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, activePinned, prevMainPins); err == nil {
			_ = saveMainPins(rolled)
		} else {
			_ = saveMainPins(activePinned)
		}
	}

	content := buildMainLinuxDesktopContent(opts.ExecPath, mimeList)
	if err := writeFileAtomic(desktopPath, []byte(content), 0o644); err != nil {
		rollbackAll()
		return fmt.Errorf("write %s: %w", desktopPath, err)
	}
	if err := updateDB(desktopDir); err != nil {
		rollbackAll()
		return err
	}

	// Post-write verification against the host OS resolver (gio / xdg-mime).
	okMIMEs, badMIMEs := verifyLinuxSystemRegistration(mimeList, expectedDefaults, env, desktopID, targetDirIdx)
	if len(badMIMEs) > 0 {
		mimeList = okMIMEs
		badSet := make(map[string]struct{}, len(badMIMEs))
		for _, mt := range badMIMEs {
			badSet[mt] = struct{}{}
		}
		trimmedPins := make(map[string]string, len(activePinned))
		for mt, app := range activePinned {
			if _, isBad := badSet[mt]; !isBad {
				trimmedPins[mt] = app
			}
		}
		if rolled, rollErr := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, activePinned, trimmedPins); rollErr == nil {
			activePinned = rolled
		}
		content = buildMainLinuxDesktopContent(opts.ExecPath, mimeList)
		if err := writeFileAtomic(desktopPath, []byte(content), 0o644); err != nil {
			rollbackAll()
			return fmt.Errorf("rewrite verified %s: %w", desktopPath, err)
		}
		if err := updateDB(desktopDir); err != nil {
			rollbackAll()
			return err
		}
	}

	if err := saveMainPins(activePinned); err != nil {
		rollbackAll()
		return fmt.Errorf("persist file association state: %w", err)
	}

	// If upgrading a legacy user-level install that previously wrote
	// ~/.local/share/applications/block.desktop pointing to this exact ExecPath
	// and it is not referenced as a default, remove the duplicate launcher.
	if !opts.SystemMode && desktopID != "block.desktop" {
		legacyUserDesktop := filepath.Join(desktopDir, "block.desktop")
		if raw, err := os.ReadFile(legacyUserDesktop); err == nil {
			entry := parseLinuxDesktopEntry(raw)
			if linuxDesktopEntryTargetsExec(entry, opts.ExecPath) && !linuxDesktopIDReferencedAsDefault("block.desktop", env) {
				if os.Remove(legacyUserDesktop) == nil {
					_ = updateDB(desktopDir)
				}
			}
		}
	}
	return nil
}

// CleanupLinuxDesktopEntry removes Block-managed main-desktop pins recorded in
// opts.StateDir without touching any user- or admin-modified defaults.
func CleanupLinuxDesktopEntry(opts LinuxDesktopInstallOptions) error {
	if opts.StateDir != "" {
		unlock, err := acquireFileAssocLock(opts.StateDir, 10*time.Second)
		if err != nil {
			return fmt.Errorf("lock file association state: %w", err)
		}
		defer unlock()
	}
	var prev fileAssocState
	if opts.StateDir != "" {
		prev, _ = loadFileAssocState(opts.StateDir)
	}
	if len(prev.PinnedLinuxMainDefaults) > 0 && opts.TargetConfigDir != "" {
		if _, err := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, prev.PinnedLinuxMainDefaults, nil); err != nil {
			return err
		}
	}
	if opts.StateDir != "" {
		prev.PinnedLinuxMainDefaults = nil
		prev.LinuxMainDesktopID = ""
		if opts.SystemMode {
			_ = os.Remove(fileAssocStatePath(opts.StateDir))
		} else {
			_ = saveFileAssocState(opts.StateDir, prev)
		}
	}
	return nil
}

// SyncSystemLinuxDesktop is invoked by Debian package postinst.
// System-wide installs intentionally keep /usr/share/applications/block.desktop
// free of MimeType= declarations because root postinst cannot inspect each
// user's private ~/.config/mimeapps.list (where a user may have set a custom
// text/plain fallback that an exact system MimeType= such as text/x-go would
// override). Per-user Open-With associations are instead synchronized inside
// each user's own session with full default-application protection.
func SyncSystemLinuxDesktop() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	// Clean up any legacy /etc/xdg/mimeapps.list pins written by older builds.
	_ = CleanupSystemLinuxDesktop()

	desktopDir := "/usr/share/applications"
	desktopPath := filepath.Join(desktopDir, "block.desktop")
	if raw, err := os.ReadFile(desktopPath); err == nil && strings.Contains(string(raw), "MimeType=") {
		clean := buildMainLinuxDesktopContent("block", nil)
		if err := writeFileAtomic(desktopPath, []byte(clean), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", desktopPath, err)
		}
	}
	return runLinuxDesktopDBUpdate(desktopDir)
}

// CleanupSystemLinuxDesktop is invoked by Debian package prerm on remove/purge
// to remove Block-managed system pins in /etc/xdg/mimeapps.list.
func CleanupSystemLinuxDesktop() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	return CleanupLinuxDesktopEntry(LinuxDesktopInstallOptions{
		TargetDataDir:   "/usr/share",
		TargetConfigDir: "/etc/xdg",
		StateDir:        "/var/lib/block",
		SystemMode:      true,
	})
}

func readFileIfExists(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}
