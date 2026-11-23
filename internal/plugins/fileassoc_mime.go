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
	var ignoreSet map[string]struct{}
	if ignoreDesktopID != "" {
		ignoreSet = map[string]struct{}{ignoreDesktopID: {}}
	}
	return resolveLinuxEffectiveDefaultAppAtDirExcluding(
		mimeType, env, apps, perDirHandlers, subclasses,
		extraDesktopID, extraDirIdx, extraMIMEs, ignoreSet,
	)
}

func resolveLinuxEffectiveDefaultAppAtDirExcluding(
	mimeType string,
	env linuxMIMEEnv,
	apps linuxMIMEAppsHierarchy,
	perDirHandlers []map[string][]string,
	subclasses map[string][]string,
	extraDesktopID string,
	extraDirIdx int,
	extraMIMEs map[string]struct{},
	ignoreDesktopIDs map[string]struct{},
) string {
	visited := make(map[string]struct{})
	queue := []string{strings.ToLower(strings.TrimSpace(mimeType))}
	isAvailable := func(id string) bool {
		if id == "" {
			return false
		}
		if _, ignored := ignoreDesktopIDs[id]; ignored {
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
	safe, pin, _ := planLinuxDesktopMIMEs(candidates, env, helperDesktopID, 0, userMimeappsPath, prevPinned, nil)
	return safe, pin
}

// discoverLinuxDesktopOverrideNames scans the XDG config and application
// directories for any desktop-specific `<desktop>-mimeapps.list` files so
// headless syncs (where XDG_CURRENT_DESKTOP is unset, e.g. under sudo/dpkg)
// never pin a generic mimeapps.list fallback that would override a desktop
// session's inherited default (such as gnome-mimeapps.list).
func discoverLinuxDesktopOverrideNames(env linuxMIMEEnv) []string {
	seen := make(map[string]struct{})
	scanDir := func(dir string) {
		if strings.TrimSpace(dir) == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(ent.Name()))
			if !strings.HasSuffix(name, "-mimeapps.list") {
				continue
			}
			desktop := strings.TrimSuffix(name, "-mimeapps.list")
			if desktop != "" {
				seen[desktop] = struct{}{}
			}
		}
	}
	for _, dir := range env.configDirs {
		scanDir(dir)
	}
	for _, dir := range env.dataDirs {
		scanDir(filepath.Join(dir, "applications"))
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func planLinuxDesktopMIMEs(
	candidates []string,
	env linuxMIMEEnv,
	desktopID string,
	targetDirIdx int,
	targetMimeappsPath string,
	prevPinned map[string]string,
	pinHoldersByMIME map[string]map[string]struct{},
) (safeWithoutPin []string, needsPin map[string]string, expectedDefaults map[string]string) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}
	actualApps := loadLinuxMIMEAppsHierarchy(env)
	baseApps := cloneLinuxMIMEAppsWithoutPins(actualApps, targetMimeappsPath, prevPinned)
	perDir := loadLinuxDirMIMEHandlers(env.dataDirs)
	subclasses := loadLinuxMIMESubclasses(env.dataDirs)

	// Always inspect every other desktop-specific *-mimeapps.list present on
	// disk (plus the generic desktop-agnostic view when XDG_CURRENT_DESKTOP is
	// set) so writing a generic pin into mimeapps.list — or adding desktopID
	// without a pin — can never displace another desktop environment's default
	// (e.g. GNOME vs. KDE configuring different text/plain editors for the same
	// user account).
	type altDesktopPlan struct {
		env      linuxMIMEEnv
		baseApps linuxMIMEAppsHierarchy
	}
	var altDesktops []altDesktopPlan
	curDesktopSet := make(map[string]struct{})
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(env.currentDesktop)), ":") {
		if p := strings.TrimSpace(part); p != "" {
			curDesktopSet[p] = struct{}{}
		}
	}
	for _, d := range discoverLinuxDesktopOverrideNames(env) {
		if _, isCur := curDesktopSet[strings.ToLower(d)]; isCur {
			continue
		}
		dEnv := env
		dEnv.currentDesktop = d
		dEnv.systemQuery = nil
		dActual := loadLinuxMIMEAppsHierarchy(dEnv)
		altDesktops = append(altDesktops, altDesktopPlan{
			env:      dEnv,
			baseApps: cloneLinuxMIMEAppsWithoutPins(dActual, targetMimeappsPath, prevPinned),
		})
	}
	if len(curDesktopSet) > 0 {
		gEnv := env
		gEnv.currentDesktop = ""
		gEnv.systemQuery = nil
		gActual := loadLinuxMIMEAppsHierarchy(gEnv)
		altDesktops = append(altDesktops, altDesktopPlan{
			env:      gEnv,
			baseApps: cloneLinuxMIMEAppsWithoutPins(gActual, targetMimeappsPath, prevPinned),
		})
	}

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
		// Exclude desktopID itself PLUS any other live Block desktop ID that
		// currently holds a managed pin on mt, so an already-pinned sibling
		// Block installation never masquerades as the pre-Block default when
		// managed pins are stripped in baseApps.
		ignoreSet := map[string]struct{}{desktopID: {}}
		for holderID := range pinHoldersByMIME[mt] {
			ignoreSet[holderID] = struct{}{}
		}
		beforeBase := resolveLinuxEffectiveDefaultAppAtDirExcluding(
			mt, env, baseApps, perDir, subclasses,
			"", targetDirIdx, nil, ignoreSet,
		)
		afterBase := resolveLinuxEffectiveDefaultAppAtDirExcluding(
			mt, env, baseApps, perDir, subclasses,
			desktopID, targetDirIdx, map[string]struct{}{mt: {}}, ignoreSet,
		)

		// When desktopID is not yet registered for mt and we don't have an active
		// pin for mt, consult the host OS's real resolver (gio / xdg-mime) so any
		// distro-specific default mechanism (defaults.list, desktop overrides, etc.)
		// is respected verbatim. Compare strictly against desktopID (not all Block
		// IDs) so one Block installation never displaces a different Block
		// installation's default.
		_, alreadyInDesktop := existingDesktopMIMEs[mt]
		pinnedApp, alreadyManagedPin := prevPinned[mt]
		if !alreadyInDesktop && !alreadyManagedPin {
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
		} else if !alreadyInDesktop && alreadyManagedPin && pinnedApp != "" && pinnedApp != desktopID && beforeBase == "" {
			// Another Block scope already holds a managed pin for mt (e.g.
			// originally discovered via gio/distro-only defaults). Inherit that
			// expected default so this scope also records a shared pin dependency.
			beforeBase = pinnedApp
			if afterBase != pinnedApp {
				afterBase = desktopID
			}
		}

		if beforeBase != "" && beforeBase != desktopID {
			expectedDefaults[mt] = beforeBase
		}

		// If XDG_CURRENT_DESKTOP was unset and any desktop-specific
		// *-mimeapps.list resolves mt differently (or would be disrupted by
		// desktopID / a generic pin), defer registering mt until the real
		// desktop session runs so we never override a desktop-specific default.
		if len(altDesktops) > 0 {
			deferForDesktop := false
			willPin := beforeBase != "" && beforeBase != desktopID && afterBase != beforeBase
			for _, alt := range altDesktops {
				altBefore := resolveLinuxEffectiveDefaultAppAtDirExcluding(
					mt, alt.env, alt.baseApps, perDir, subclasses,
					"", targetDirIdx, nil, ignoreSet,
				)
				altAfter := resolveLinuxEffectiveDefaultAppAtDirExcluding(
					mt, alt.env, alt.baseApps, perDir, subclasses,
					desktopID, targetDirIdx, map[string]struct{}{mt: {}}, ignoreSet,
				)
				if willPin {
					if altBefore != "" && altBefore != beforeBase {
						deferForDesktop = true
						break
					}
				} else {
					if altBefore != "" && altBefore != desktopID && altAfter != altBefore {
						deferForDesktop = true
						break
					}
				}
			}
			if deferForDesktop {
				delete(expectedDefaults, mt)
				continue
			}
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

// resolveLinuxMimeappsTarget inspects <configHome>/mimeapps.list without
// clobbering symlinks (e.g. dotfiles managers). If mimeapps.list is a symlink
// to an existing regular file, writes are staged and atomically renamed onto
// the resolved target path so the symlink remains intact. Dangling symlinks,
// non-regular files, or unwritable target directories return an error so the
// caller skips displacement-risk registrations safely.
func resolveLinuxMimeappsTarget(configHome string) (linkPath, targetPath, targetDir string, existed bool, mode os.FileMode, err error) {
	linkPath = filepath.Join(configHome, "mimeapps.list")
	linfo, lerr := os.Lstat(linkPath)
	if os.IsNotExist(lerr) {
		return linkPath, linkPath, configHome, false, 0o644, nil
	}
	if lerr != nil {
		return "", "", "", false, 0, lerr
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		resolved, rerr := filepath.EvalSymlinks(linkPath)
		if rerr != nil {
			return "", "", "", false, 0, fmt.Errorf("resolve mimeapps.list symlink: %w", rerr)
		}
		st, serr := os.Stat(resolved)
		if serr != nil {
			return "", "", "", false, 0, fmt.Errorf("stat mimeapps.list symlink target: %w", serr)
		}
		if !st.Mode().IsRegular() {
			return "", "", "", false, 0, fmt.Errorf("mimeapps.list symlink target %s is not a regular file", resolved)
		}
		return linkPath, resolved, filepath.Dir(resolved), true, st.Mode().Perm(), nil
	}
	if !linfo.Mode().IsRegular() {
		return "", "", "", false, 0, fmt.Errorf("%s is not a regular file", linkPath)
	}
	return linkPath, linkPath, configHome, true, linfo.Mode().Perm(), nil
}

// readLinuxMimeappsSingleDefaults reads [Default Applications] single-entry
// values from <configHome>/mimeapps.list (following symlinks) for crash
// recovery of two-phase pin transactions.
func readLinuxMimeappsSingleDefaults(configHome string) map[string]string {
	if strings.TrimSpace(configHome) == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(configHome, "mimeapps.list"))
	if err != nil {
		return nil
	}
	out := make(map[string]string)
	section := ""
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			continue
		}
		if section != "[Default Applications]" || trimmed == "" || strings.HasPrefix(trimmed, "#") {
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
		var ids []string
		for _, part := range strings.Split(v, ";") {
			if p := strings.TrimSpace(part); p != "" {
				ids = append(ids, p)
			}
		}
		if len(ids) == 1 {
			out[mt] = ids[0]
		} else {
			delete(out, mt)
		}
	}
	return out
}

// recoverLinuxPendingPins reconciles an interrupted two-phase pin transaction
// (old committed values vs. pending desired values) against the actual
// mimeapps.list content on disk so ownership of neither old nor new values is
// lost after a mid-transaction crash.
func recoverLinuxPendingPins(configHome string, committed, pending map[string]string, hasPendingTx bool) map[string]string {
	if !hasPendingTx && len(pending) == 0 {
		return cloneStringMap(committed)
	}
	onDisk := readLinuxMimeappsSingleDefaults(configHome)
	keys := make(map[string]struct{}, len(committed)+len(pending))
	for k := range committed {
		keys[k] = struct{}{}
	}
	for k := range pending {
		keys[k] = struct{}{}
	}
	actual := make(map[string]string, len(keys))
	for mt := range keys {
		cur, exists := onDisk[mt]
		if !exists || cur == "" {
			continue
		}
		if pVal := pending[mt]; pVal != "" && cur == pVal {
			actual[mt] = pVal
			continue
		}
		if cVal := committed[mt]; cVal != "" && cur == cVal {
			actual[mt] = cVal
			continue
		}
	}
	if len(actual) == 0 {
		return nil
	}
	return actual
}

// recoverAllLinuxStatePins resolves any interrupted two-phase pin transactions
// across both the main desktop scope and all per-installation helper scopes.
func recoverAllLinuxStatePins(st *fileAssocState, configHome string) {
	if st == nil {
		return
	}
	if st.PendingLinuxMainPinTx || len(st.PendingPinnedLinuxMainDefaults) > 0 {
		st.PinnedLinuxMainDefaults = recoverLinuxPendingPins(
			configHome, st.PinnedLinuxMainDefaults, st.PendingPinnedLinuxMainDefaults, st.PendingLinuxMainPinTx,
		)
		st.PendingPinnedLinuxMainDefaults = nil
		st.PendingLinuxMainPinTx = false
	}
	if len(st.LinuxScopes) > 0 {
		for id, sc := range st.LinuxScopes {
			if sc.PendingPinTx || len(sc.PendingPinnedLinuxDefaults) > 0 {
				sc.PinnedLinuxDefaults = recoverLinuxPendingPins(
					configHome, sc.PinnedLinuxDefaults, sc.PendingPinnedLinuxDefaults, sc.PendingPinTx,
				)
				sc.PendingPinnedLinuxDefaults = nil
				sc.PendingPinTx = false
				st.LinuxScopes[id] = sc
			}
		}
	}
}

// linuxHelperDesktopIDForExec derives a stable per-installation helper desktop
// file ID so coexisting Linux installs (system .deb, user ~/.local/bin, or
// multiple AppImages) never overwrite each other's Exec= target or MimeType=.
func linuxHelperDesktopIDForExec(execPath string) string {
	return "block-assoc-" + stableInstallScopeHash(canonicalLinuxExecPath(execPath)) + ".desktop"
}

// linuxSharedXDGDir returns the shared per-XDG_CONFIG_HOME coordination
// directory used by all Block installations that mutate the same user
// mimeapps.list — even when those installations use separate BLOCK_DATA_DIR
// app data directories.
func linuxSharedXDGDir(configHome string) string {
	c := strings.TrimSpace(configHome)
	if c == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(c), ".block-fileassoc")
}

func sameCanonicalDir(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ra, err := filepath.EvalSymlinks(ca); err == nil && ra != "" {
		ca = ra
	}
	if rb, err := filepath.EvalSymlinks(cb); err == nil && rb != "" {
		cb = rb
	}
	return ca == cb
}

// acquireLinuxXDGStateLock acquires an OS lock anchored to configHome so
// installations using different appDataDirs but sharing the same mimeapps.list
// strictly serialize mutations and share pin ownership records.
func acquireLinuxXDGStateLock(appDataDir, configHome string, timeout time.Duration) (func(), error) {
	xdgDir := linuxSharedXDGDir(configHome)
	if xdgDir == "" || sameCanonicalDir(appDataDir, xdgDir) {
		return func() {}, nil
	}
	return acquireFileAssocLock(xdgDir, timeout)
}

func mergeLinuxAssocStates(base, extra fileAssocState) fileAssocState {
	out := base
	if out.LinuxScopes == nil && len(extra.LinuxScopes) > 0 {
		out.LinuxScopes = make(map[string]linuxAssocScopeState, len(extra.LinuxScopes))
	}
	for id, sc := range extra.LinuxScopes {
		if existing, ok := out.LinuxScopes[id]; ok {
			merged := existing
			if merged.DataHome == "" {
				merged.DataHome = sc.DataHome
			}
			if merged.ExecutablePath == "" {
				merged.ExecutablePath = sc.ExecutablePath
			}
			if len(merged.Extensions) == 0 {
				merged.Extensions = sc.Extensions
			}
			merged.PinnedLinuxDefaults = unionStringMaps(sc.PinnedLinuxDefaults, merged.PinnedLinuxDefaults)
			if sc.PendingPinTx || len(sc.PendingPinnedLinuxDefaults) > 0 {
				merged.PendingPinTx = true
				merged.PendingPinnedLinuxDefaults = unionStringMaps(sc.PendingPinnedLinuxDefaults, merged.PendingPinnedLinuxDefaults)
			}
			out.LinuxScopes[id] = merged
		} else {
			out.LinuxScopes[id] = sc
		}
	}
	if out.LinuxMainDesktopID == "" {
		out.LinuxMainDesktopID = extra.LinuxMainDesktopID
	}
	if out.LinuxMainDataHome == "" {
		out.LinuxMainDataHome = extra.LinuxMainDataHome
	}
	out.PinnedLinuxMainDefaults = unionStringMaps(extra.PinnedLinuxMainDefaults, out.PinnedLinuxMainDefaults)
	if extra.PendingLinuxMainPinTx || len(extra.PendingPinnedLinuxMainDefaults) > 0 {
		out.PendingLinuxMainPinTx = true
		out.PendingPinnedLinuxMainDefaults = unionStringMaps(extra.PendingPinnedLinuxMainDefaults, out.PendingPinnedLinuxMainDefaults)
	}
	return out
}

// loadLinuxXDGMergedState loads and merges Linux pin ownership state from both
// appDataDir and the shared XDG config coordination directory, recovering any
// interrupted two-phase pin transactions.
func loadLinuxXDGMergedState(appDataDir, configHome string) (merged fileAssocState, local fileAssocState, hasLocal bool, shared fileAssocState, hasShared bool) {
	if strings.TrimSpace(appDataDir) != "" {
		local, hasLocal = loadFileAssocState(appDataDir)
	}
	if xdgDir := linuxSharedXDGDir(configHome); xdgDir != "" && !sameCanonicalDir(appDataDir, xdgDir) {
		shared, hasShared = loadFileAssocState(xdgDir)
	}
	merged = mergeLinuxAssocStates(local, shared)
	recoverAllLinuxStatePins(&merged, configHome)
	recoverAllLinuxStatePins(&local, configHome)
	recoverAllLinuxStatePins(&shared, configHome)
	return merged, local, hasLocal, shared, hasShared
}

// syncLinuxScopePinsSidecar writes (or removes when empty) a tiny TSV sidecar
// (<mime>\t<appID>) in linuxSharedXDGDir(configHome) containing every unique
// (mime, appID) pair from both committed and pending pin maps so POSIX shell
// deferred uninstall scripts share exact journal parity with Go runtime recovery.
func syncLinuxScopePinsSidecar(configHome, desktopID, dataHome string, committed, pending map[string]string) error {
	xdgDir := linuxSharedXDGDir(configHome)
	if xdgDir == "" || strings.TrimSpace(desktopID) == "" || strings.ContainsAny(desktopID, "/\\") {
		return nil
	}
	sidecar := filepath.Join(xdgDir, "pins-"+desktopID+".tsv")
	pairSet := make(map[string]struct{}, len(committed)+len(pending))
	addMap := func(m map[string]string) {
		for rawMT, rawApp := range m {
			mt := strings.TrimSpace(rawMT)
			app := strings.TrimSpace(rawApp)
			if mt != "" && app != "" {
				pairSet[mt+"\t"+app] = struct{}{}
			}
		}
	}
	addMap(committed)
	addMap(pending)
	if len(pairSet) == 0 {
		_ = os.Remove(sidecar)
		return nil
	}
	pairs := make([]string, 0, len(pairSet))
	for p := range pairSet {
		pairs = append(pairs, p)
	}
	sort.Strings(pairs)
	var b strings.Builder
	if strings.TrimSpace(dataHome) != "" {
		b.WriteString("#data_home=" + filepath.Clean(dataHome) + "\n")
	}
	for _, p := range pairs {
		b.WriteString(p + "\n")
	}
	return writeFileAtomic(sidecar, []byte(b.String()), 0o644)
}

// linuxScopeExplicitlyUninstalled reports whether an OS package uninstaller
// recorded an explicit uninstall marker for desktopID matching execPath AND
// execPath is confirmed absent on disk. This prevents coexisting installations
// from pruning a portable/AppImage install whose external drive is merely
// unmounted.
func linuxScopeExplicitlyUninstalled(desktopID, execPath string) bool {
	if strings.TrimSpace(desktopID) == "" || strings.ContainsAny(desktopID, "/\\") || desktopID == "." || desktopID == ".." {
		return false
	}
	canonExec := canonicalLinuxExecPath(execPath)
	if canonExec == "" {
		return false
	}
	markerPath := filepath.Join("/var/lib/block/pending-uninstall", desktopID)
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	recorded := canonicalLinuxExecPath(strings.TrimSpace(string(raw)))
	if recorded == "" || recorded != canonExec {
		return false
	}
	_, statErr := os.Stat(execPath)
	return os.IsNotExist(statErr)
}

// hasDesktopSpecificMIMEApps reports whether any desktop-prefixed
// (<desktop>-mimeapps.list, e.g. gnome-mimeapps.list) file exists in the XDG
// hierarchy. When XDG_CURRENT_DESKTOP is unset and such a file exists,
// computing pins from generic mimeapps.list would risk overriding the real
// desktop's inherited defaults.
func hasDesktopSpecificMIMEApps(env linuxMIMEEnv) bool {
	scanDir := func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, ent := range entries {
			name := ent.Name()
			if strings.HasSuffix(name, "-mimeapps.list") && name != "mimeapps.list" {
				return true
			}
		}
		return false
	}
	for _, dir := range env.configDirs {
		if scanDir(dir) {
			return true
		}
	}
	for _, dir := range env.dataDirs {
		if scanDir(filepath.Join(dir, "applications")) {
			return true
		}
	}
	return false
}

// collectOtherLinuxPinnedDefaults gathers active Block-managed pins currently
// depended on by OTHER live Block installations sharing the same user
// mimeapps.list, so cleaning up or updating one installation never removes a
// default pin that another still-installed Block copy relies on.
func collectOtherLinuxPinnedDefaults(st fileAssocState, desktopDir string, excludeMain bool, excludeScopeID string) map[string]string {
	pins, _ := collectOtherLinuxPinsAndHolders(st, desktopDir, excludeMain, excludeScopeID)
	return pins
}

// collectOtherLinuxPinsAndHolders returns both the merged map of pins held by
// other live Block scopes AND, per MIME type, the set of other live Block
// desktop IDs that hold a managed pin for that MIME type.
func collectOtherLinuxPinsAndHolders(st fileAssocState, desktopDir string, excludeMain bool, excludeScopeID string) (map[string]string, map[string]map[string]struct{}) {
	out := make(map[string]string)
	holders := make(map[string]map[string]struct{})
	addHolder := func(mt, id string) {
		if mt == "" || id == "" {
			return
		}
		if holders[mt] == nil {
			holders[mt] = make(map[string]struct{})
		}
		holders[mt][id] = struct{}{}
	}
	filePresentIn := func(dir, desktopID string) bool {
		if dir == "" || desktopID == "" {
			return true
		}
		_, err := os.Stat(filepath.Join(dir, desktopID))
		return err == nil
	}
	if !excludeMain && len(st.PinnedLinuxMainDefaults) > 0 {
		mainID := strings.TrimSpace(st.LinuxMainDesktopID)
		if mainID == "" {
			mainID = "block-user.desktop"
		}
		mainDesktopDir := desktopDir
		if strings.TrimSpace(st.LinuxMainDataHome) != "" {
			mainDesktopDir = filepath.Join(filepath.Clean(st.LinuxMainDataHome), "applications")
		}
		presentMain := filePresentIn(mainDesktopDir, mainID)
		presentLegacy := mainID != "block.desktop" && filePresentIn(mainDesktopDir, "block.desktop")
		if presentMain || presentLegacy {
			for mt, app := range st.PinnedLinuxMainDefaults {
				if app != "" {
					out[mt] = app
					if presentMain {
						addHolder(mt, mainID)
					}
					if presentLegacy {
						addHolder(mt, "block.desktop")
					}
				}
			}
		}
	}
	if len(st.LinuxScopes) > 0 {
		ids := make([]string, 0, len(st.LinuxScopes))
		for id := range st.LinuxScopes {
			if id == excludeScopeID {
				continue
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			sc := st.LinuxScopes[id]
			scopeDesktopDir := desktopDir
			if strings.TrimSpace(sc.DataHome) != "" {
				scopeDesktopDir = filepath.Join(filepath.Clean(sc.DataHome), "applications")
			}
			if len(sc.PinnedLinuxDefaults) == 0 || !filePresentIn(scopeDesktopDir, id) {
				continue
			}
			for mt, app := range sc.PinnedLinuxDefaults {
				if app != "" {
					if _, exists := out[mt]; !exists {
						out[mt] = app
					}
					addHolder(mt, id)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, holders
}

// mergeLinuxPinsPreservingOthers merges selfDesired with otherPins while never
// overwriting an existing pin held by another live Block installation.
func mergeLinuxPinsPreservingOthers(otherPins, selfDesired map[string]string) map[string]string {
	if len(otherPins) == 0 && len(selfDesired) == 0 {
		return nil
	}
	out := make(map[string]string, len(otherPins)+len(selfDesired))
	for mt, app := range otherPins {
		if app != "" {
			out[mt] = app
		}
	}
	for mt, app := range selfDesired {
		if app == "" {
			continue
		}
		if _, held := out[mt]; !held {
			out[mt] = app
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// subsetLinuxPins extracts the subset of allActive pins corresponding to keys
// requested by selfDesired so each scope only records its own dependencies.
func subsetLinuxPins(allActive, selfDesired map[string]string) map[string]string {
	if len(allActive) == 0 || len(selfDesired) == 0 {
		return nil
	}
	out := make(map[string]string, len(selfDesired))
	for mt := range selfDesired {
		if app, ok := allActive[mt]; ok && app != "" {
			out[mt] = app
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// reconcileLinuxPinnedDefaults updates [Default Applications] in
// <configDir>/mimeapps.list to preserve implicit non-Block defaults while
// never overwriting user-chosen defaults, preserving symlinks to dotfiles by
// atomically updating their real target file, cleanly removing Block-managed
// pins that are no longer needed, and detecting concurrent external edits
// (e.g. from GNOME/KDE Settings) before commit so their changes are merged
// rather than clobbered by a stale snapshot.
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
	const maxAttempts = 6
	var lastPath string

	for attempt := 0; attempt < maxAttempts; attempt++ {
		_, targetPath, targetDir, existed, mode, err := resolveLinuxMimeappsTarget(configHome)
		if err != nil {
			return nil, err
		}
		lastPath = targetPath
		if !existed && len(desiredPins) == 0 {
			return nil, nil
		}

		var rawBefore []byte
		var stBefore os.FileInfo
		if existed {
			rawBefore, err = os.ReadFile(targetPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			stBefore, _ = os.Stat(targetPath)
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

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(targetDir, ".block-mimeapps-*.tmp")
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
		if err := tmp.Chmod(mode); err != nil {
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

		// Re-check both symlink resolution and target content right before
		// committing so any concurrent edit made by a desktop settings app or
		// dotfiles tool between read and commit is merged instead of clobbered.
		_, targetPathNow, _, nowExists, _, errNow := resolveLinuxMimeappsTarget(configHome)
		if errNow != nil {
			_ = os.Remove(tmpPath)
			return nil, errNow
		}
		concurrentChange := false
		if existed != nowExists || targetPathNow != targetPath {
			concurrentChange = true
		} else if existed {
			rawNow, readErrNow := os.ReadFile(targetPath)
			if readErrNow != nil {
				if os.IsNotExist(readErrNow) {
					concurrentChange = true
				} else {
					_ = os.Remove(tmpPath)
					return nil, readErrNow
				}
			} else if !bytes.Equal(rawBefore, rawNow) {
				concurrentChange = true
			} else if stNow, stErrNow := os.Stat(targetPath); stBefore != nil && stErrNow == nil {
				if !os.SameFile(stBefore, stNow) || !stBefore.ModTime().Equal(stNow.ModTime()) {
					concurrentChange = true
				}
			}
		}
		if concurrentChange {
			_ = os.Remove(tmpPath)
			continue
		}

		if err := os.Rename(tmpPath, targetPath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}
		if len(activePinned) == 0 {
			return nil, nil
		}
		return activePinned, nil
	}

	return nil, fmt.Errorf("concurrent modification conflict updating %s", lastPath)
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

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
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
	if !opts.SystemMode && opts.TargetConfigDir != "" {
		unlockXDG, err := acquireLinuxXDGStateLock(opts.StateDir, opts.TargetConfigDir, 10*time.Second)
		if err != nil {
			return fmt.Errorf("lock XDG file association state: %w", err)
		}
		defer unlockXDG()
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

	var prev, localState, sharedState fileAssocState
	if !opts.SystemMode {
		prev, localState, _, sharedState, _ = loadLinuxXDGMergedState(opts.StateDir, opts.TargetConfigDir)
	} else if opts.StateDir != "" {
		prev, _ = loadFileAssocState(opts.StateDir)
		recoverAllLinuxStatePins(&prev, opts.TargetConfigDir)
		localState = prev
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

	desktopDir := filepath.Join(targetDataClean, "applications")
	var otherPins map[string]string
	var pinHolders map[string]map[string]struct{}
	if !opts.SystemMode {
		otherPins, pinHolders = collectOtherLinuxPinsAndHolders(prev, desktopDir, true, desktopID)
	}
	allManagedPins := unionStringMaps(otherPins, prevMainPins)

	// If XDG_CURRENT_DESKTOP is unset while desktop-specific *-mimeapps.list
	// files exist (e.g. gnome-mimeapps.list), we cannot reliably know the real
	// desktop session's inherited defaults, so install the launcher cleanly
	// without MimeType= claims and let the desktop session sync handle them.
	ambiguousDesktop := !opts.SystemMode && strings.TrimSpace(env.currentDesktop) == "" && hasDesktopSpecificMIMEApps(env)

	var safeDirect []string
	var needsPin, expectedDefaults map[string]string
	if !ambiguousDesktop {
		safeDirect, needsPin, expectedDefaults = planLinuxDesktopMIMEs(
			candidates, env, desktopID, targetDirIdx, targetMimeappsPath, allManagedPins, pinHolders,
		)
	}

	// Two-phase write-ahead ownership journal BEFORE mutating mimeapps.list so
	// a crash before or after mimeapps commit never loses ownership of either
	// old committed values or newly written values.
	saveMainPinJournal := func(committed, pending map[string]string, pendingTx bool) error {
		applyMain := func(st fileAssocState) fileAssocState {
			st.LinuxMainDesktopID = desktopID
			st.LinuxMainDataHome = targetDataClean
			st.PinnedLinuxMainDefaults = cloneStringMap(committed)
			st.PendingPinnedLinuxMainDefaults = cloneStringMap(pending)
			st.PendingLinuxMainPinTx = pendingTx
			return st
		}
		if opts.StateDir != "" {
			localState = applyMain(localState)
			if err := saveFileAssocState(opts.StateDir, localState); err != nil {
				return err
			}
		}
		if !opts.SystemMode && opts.TargetConfigDir != "" {
			xdgDir := linuxSharedXDGDir(opts.TargetConfigDir)
			if xdgDir != "" && !sameCanonicalDir(opts.StateDir, xdgDir) {
				sharedState = applyMain(sharedState)
				if err := saveFileAssocState(xdgDir, sharedState); err != nil {
					return err
				}
			}
			if err := syncLinuxScopePinsSidecar(opts.TargetConfigDir, desktopID, targetDataClean, committed, pending); err != nil && (len(committed) > 0 || len(pending) > 0) {
				return err
			}
		}
		return nil
	}

	// Phase 1 (add-only BEFORE mutating the .desktop file): write any newly
	// required pins while retaining all existing prevMainPins so a crash before
	// the updated .desktop + desktop database are published can never leave the
	// old .desktop file claiming a MIME type without its default-app pin.
	activePinned := cloneStringMap(prevMainPins)
	desiredFinalPins := cloneStringMap(needsPin)
	var verifiedPinned []string
	if len(needsPin) > 0 {
		stage1SelfPins := unionStringMaps(prevMainPins, needsPin)
		stage1Combined := mergeLinuxPinsPreservingOthers(otherPins, stage1SelfPins)
		if err := saveMainPinJournal(prevMainPins, stage1SelfPins, true); err != nil {
			return fmt.Errorf("persist file association journal: %w", err)
		}
		allActive, pinErr := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, allManagedPins, stage1Combined)
		if pinErr != nil {
			// Cannot safely pin in TargetConfigDir (e.g. read-only /etc/xdg or
			// unwritable symlink target); restore journaled state and omit
			// displacement-risk MIME types.
			activePinned = cloneStringMap(prevMainPins)
			_ = saveMainPinJournal(activePinned, nil, false)
			desiredFinalPins = nil
		} else {
			activePinned = subsetLinuxPins(allActive, stage1SelfPins)
			var failed []string
			verifiedPinned, failed = verifyLinuxPinnedMIMEsAtDir(needsPin, env, desktopID, targetDirIdx)
			for _, mt := range failed {
				delete(desiredFinalPins, mt)
			}
		}
	}

	mimeList := unionSortedStrings(safeDirect, verifiedPinned)
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
		prevCombined := unionStringMaps(otherPins, activePinned)
		restoreCombined := mergeLinuxPinsPreservingOthers(otherPins, prevMainPins)
		_ = saveMainPinJournal(activePinned, prevMainPins, true)
		if rolled, err := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, prevCombined, restoreCombined); err == nil {
			_ = saveMainPinJournal(subsetLinuxPins(rolled, prevMainPins), nil, false)
		} else {
			_ = saveMainPinJournal(activePinned, nil, false)
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
		for _, mt := range badMIMEs {
			delete(desiredFinalPins, mt)
		}
		// Rewrite the .desktop entry and refresh the desktop database BEFORE
		// removing pins so a failure never drops default protection while the
		// .desktop entry still claims those MIME types.
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

	// Phase 3 (prune obsolete pins ONLY AFTER the updated .desktop entry and
	// desktop database have been committed).
	if !equalStringMaps(activePinned, desiredFinalPins) {
		prevCombined := unionStringMaps(otherPins, activePinned)
		finalCombined := mergeLinuxPinsPreservingOthers(otherPins, desiredFinalPins)
		if err := saveMainPinJournal(activePinned, desiredFinalPins, true); err != nil {
			return fmt.Errorf("persist file association cleanup journal: %w", err)
		}
		if rolled, rollErr := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, prevCombined, finalCombined); rollErr == nil {
			activePinned = subsetLinuxPins(rolled, desiredFinalPins)
		}
	}

	if err := saveMainPinJournal(activePinned, nil, false); err != nil {
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
// opts.StateDir (and the shared XDG pin registry for user installs) without
// touching any user- or admin-modified defaults, while retaining any pins still
// depended on by other live Block installations.
func CleanupLinuxDesktopEntry(opts LinuxDesktopInstallOptions) error {
	if opts.StateDir != "" {
		unlock, err := acquireFileAssocLock(opts.StateDir, 10*time.Second)
		if err != nil {
			return fmt.Errorf("lock file association state: %w", err)
		}
		defer unlock()
	}
	if !opts.SystemMode && opts.TargetConfigDir != "" {
		unlockXDG, err := acquireLinuxXDGStateLock(opts.StateDir, opts.TargetConfigDir, 10*time.Second)
		if err != nil {
			return fmt.Errorf("lock XDG file association state: %w", err)
		}
		defer unlockXDG()
	}
	var prev, localState, sharedState fileAssocState
	if !opts.SystemMode {
		prev, localState, _, sharedState, _ = loadLinuxXDGMergedState(opts.StateDir, opts.TargetConfigDir)
	} else if opts.StateDir != "" {
		prev, _ = loadFileAssocState(opts.StateDir)
		recoverAllLinuxStatePins(&prev, opts.TargetConfigDir)
		localState = prev
	}
	desktopID := strings.TrimSpace(opts.DesktopID)
	if desktopID == "" {
		if opts.SystemMode {
			desktopID = "block.desktop"
		} else {
			desktopID = strings.TrimSpace(prev.LinuxMainDesktopID)
			if desktopID == "" {
				desktopID = "block-user.desktop"
			}
		}
	}
	var otherPins map[string]string
	if !opts.SystemMode {
		desktopDir := ""
		if strings.TrimSpace(opts.TargetDataDir) != "" {
			desktopDir = filepath.Join(filepath.Clean(opts.TargetDataDir), "applications")
		}
		otherPins = collectOtherLinuxPinnedDefaults(prev, desktopDir, true, desktopID)
	}
	if len(prev.PinnedLinuxMainDefaults) > 0 && opts.TargetConfigDir != "" {
		prevCombined := unionStringMaps(otherPins, prev.PinnedLinuxMainDefaults)
		if _, err := reconcileLinuxPinnedDefaults(opts.TargetConfigDir, prevCombined, otherPins); err != nil {
			return err
		}
	}
	clearMain := func(st *fileAssocState) {
		st.PinnedLinuxMainDefaults = nil
		st.PendingPinnedLinuxMainDefaults = nil
		st.PendingLinuxMainPinTx = false
		st.LinuxMainDesktopID = ""
		st.LinuxMainDataHome = ""
	}
	if opts.StateDir != "" {
		clearMain(&localState)
		if opts.SystemMode || len(localState.LinuxScopes) == 0 {
			_ = os.Remove(fileAssocStatePath(opts.StateDir))
		} else {
			_ = saveFileAssocState(opts.StateDir, localState)
		}
	}
	if !opts.SystemMode && opts.TargetConfigDir != "" {
		_ = syncLinuxScopePinsSidecar(opts.TargetConfigDir, desktopID, "", nil, nil)
		xdgDir := linuxSharedXDGDir(opts.TargetConfigDir)
		if xdgDir != "" && !sameCanonicalDir(opts.StateDir, xdgDir) {
			clearMain(&sharedState)
			if len(sharedState.LinuxScopes) == 0 {
				_ = os.Remove(fileAssocStatePath(xdgDir))
			} else {
				_ = saveFileAssocState(xdgDir, sharedState)
			}
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
