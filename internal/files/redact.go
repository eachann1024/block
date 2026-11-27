package files

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Log redaction. Every line is masked BEFORE it is written to disk, so the
// log file never holds note content, names, paths or secrets, and the same
// pass runs again when a log is read for a feedback report (older, unmasked
// lines included). The rules err on the side of masking too much.
//
// A masked value becomes a tag: <path:3f2a.md>, <file:91c0.pdf>, <host:07be>,
// <text:5d11>, <key>, <email>, <ip>, <user>. The 4 hex digits are a salted
// hash, stable within one run so repeated values can be told apart, and
// meaningless outside it (the salt is random per process and never stored).

var redactSalt = func() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b
}()

func redactHash(value string) string {
	h := sha256.New()
	h.Write(redactSalt)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil)[:2])
}

// safeExt keeps a short alphanumeric extension: it says what kind of file
// failed without saying which.
func safeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) < 2 || len(ext) > 6 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

func pathTag(p string) string { return "<path:" + redactHash(p) + safeExt(p) + ">" }
func fileTag(p string) string { return "<file:" + redactHash(p) + safeExt(p) + ">" }
func textTag(s string) string { return "<text:" + redactHash(s) + ">" }

func hostTag(raw string) string {
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	return "<host:" + redactHash(strings.ToLower(host)) + ">"
}

const fileExtAlternation = `md|markdown|mdx|txt|rtf|pdf|docx?|xlsx?|pptx?|pages|html?|png|jpe?g|gif|webp|bmp|heic|tiff?|svg|json|ya?ml|toml|csv|zip|7z|rar|tar|gz|mov|mp4|m4v|webm|mkv|avi|mp3|wav|m4a`

var (
	reTag    = regexp.MustCompile(`<(?:path|file|host|text):[0-9a-f]{4}(?:\.[a-z0-9]{1,5})?>|<(?:key|email|ip|user)>`)
	reURL    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reKV     = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|auth[_-]?token|client[_-]?secret|private[_-]?key|secret[_-]?key|key|token|secret|password|passwd|pwd|authorization|auth|cookie|session|signature))\b(\s*[=:]\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s,;&]+)`)
	reBearer = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	reSecret = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}|(?:pk|rk)_(?:live|test)_[A-Za-z0-9]{8,}|gh[posu]_[A-Za-z0-9]{12,}|github_pat_[A-Za-z0-9_]{16,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}(?:\.[A-Za-z0-9_-]*)?|xai-[A-Za-z0-9_-]{12,}|gsk_[A-Za-z0-9]{12,}|pplx-[A-Za-z0-9]{12,}|hf_[A-Za-z0-9]{12,}|glpat-[A-Za-z0-9_-]{12,}|npm_[A-Za-z0-9]{12,}|pypi-[A-Za-z0-9_-]{12,}|sntryu_[A-Za-z0-9]{12,})`)
	reBlob   = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
	reIPv4        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6C       = regexp.MustCompile(`(?:\b[0-9a-fA-F]{1,4}:|::)[0-9a-fA-F:.]*(?:%[A-Za-z0-9._-]+)?`)
	rePath        = regexp.MustCompile(`(?:~[/\\][^/\\\n\r\t:"'<>|*?]+(?:[/\\][^/\\\n\r\t:"'<>|*?]+)*|(?:[A-Za-z]:)?(?:[/\\][^/\\\n\r\t:"'<>|*?]+){2,})`)
	reSingleDrive = regexp.MustCompile(`\b[A-Za-z]:[/\\][^/\\\s:"'<>|*?]+`)
	reSingleRoot  = regexp.MustCompile(`(?i)\b(open|stat|lstat|read|write|mkdir|remove|rename|chmod|chown|symlink|readlink|create)\s+(/[A-Za-z0-9._-]+)`)
	reFile        = regexp.MustCompile(`(?i)[^\s/\\:"'<>|()\[\]{},;=]+\.(?:` + fileExtAlternation + `)\b`)
	reQuoted      = regexp.MustCompile(`"[^"\n]+"`)
	reExtEnd      = regexp.MustCompile(`\.[A-Za-z0-9]{1,5}$`)
)

// outsideTags applies fn to the stretches of s that are not already tags, so
// a later rule never rewrites the inside of an earlier rule's output.
func outsideTags(s string, fn func(string) string) string {
	locs := reTag.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return fn(s)
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		b.WriteString(fn(s[prev:loc[0]]))
		b.WriteString(s[loc[0]:loc[1]])
		prev = loc[1]
	}
	b.WriteString(fn(s[prev:]))
	return b.String()
}

func isPathBoundary(s string, at int) bool {
	if at == 0 {
		return true
	}
	switch s[at-1] {
	case ' ', '\t', '=', '(', '[', '{', ',', '"', '\'', ':', ';', '>':
		return true
	}
	return false
}

// redactPaths masks absolute, home-relative and drive paths. A segment may
// hold spaces, so a match runs to the end of the line or the next colon or
// quote; when the last segment starts with a file name ("b.md failed after
// 3ms") the match is cut back to that name. Anything else stays masked.
func redactPaths(s string) string {
	locs := rePath.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		if start < prev || !isPathBoundary(s, start) {
			continue
		}
		match := s[start:end]
		lastSep := strings.LastIndexAny(match, `/\`)
		last := match[lastSep+1:]
		if sp := strings.IndexAny(last, " \t"); sp > 0 && reExtEnd.MatchString(last[:sp]) {
			end = start + lastSep + 1 + sp
			match = s[start:end]
		}
		match = strings.TrimRight(match, " \t.,;)")
		end = start + len(match)
		b.WriteString(s[prev:start])
		b.WriteString(pathTag(match))
		prev = end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// redactNonASCII masks every run of non-ASCII text. The app's own messages
// are ASCII; anything else on a log line came from the user's content, a
// file name or a localized system message.
func redactNonASCII(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] < utf8.RuneSelf {
			b.WriteByte(s[i])
			i++
			continue
		}
		// A run starts at a non-ASCII rune and continues through spaces and
		// further non-ASCII runes; trailing spaces are given back.
		j, endWide := i, i
		for j < len(s) {
			if s[j] >= utf8.RuneSelf {
				_, size := utf8.DecodeRuneInString(s[j:])
				j += size
				endWide = j
				continue
			}
			if s[j] != ' ' {
				break
			}
			j++
		}
		b.WriteString(textTag(s[i:endWide]))
		i = endWide
	}
	return b.String()
}

// redactQuoted masks quoted text (titles, names printed with %q). Quotes are
// paired across the whole line, tags included, so a second pass pairs them
// the same way; a quote that already holds exactly one tag is left alone.
func redactQuoted(s string) string {
	return reQuoted.ReplaceAllStringFunc(s, func(q string) string {
		inner := q[1 : len(q)-1]
		if reTag.FindString(inner) == inner {
			return q
		}
		return `"` + textTag(inner) + `"`
	})
}

var redactUserName = func() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
	}
	if name == "" {
		if home, err := os.UserHomeDir(); err == nil {
			name = filepath.Base(home)
		}
	}
	// Short or generic names would mask ordinary words.
	switch strings.ToLower(name) {
	case "", "root", "user", "admin", "home", "block":
		return ""
	}
	if len(name) < 3 {
		return ""
	}
	return name
}()

var reUserName = func() *regexp.Regexp {
	if redactUserName == "" {
		return nil
	}
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(redactUserName))
}()

func redactIPv6(s string) string {
	return reIPv6C.ReplaceAllStringFunc(s, func(cand string) string {
		prefix, suffix := "", ""
		trimmed := cand
		if strings.HasPrefix(trimmed, ":") && !strings.HasPrefix(trimmed, "::") {
			prefix = ":"
			trimmed = trimmed[1:]
		}
		if dot := strings.TrimRight(trimmed, "."); len(dot) != len(trimmed) {
			suffix = trimmed[len(dot):] + suffix
			trimmed = dot
		}
		if strings.HasSuffix(trimmed, ":") && !strings.HasSuffix(trimmed, "::") {
			suffix = ":" + suffix
			trimmed = trimmed[:len(trimmed)-1]
		}
		if strings.Count(trimmed, ":") < 2 {
			return cand
		}
		if addr, err := netip.ParseAddr(trimmed); err == nil && addr.Is6() {
			return prefix + "<ip>" + suffix
		}
		if strings.HasPrefix(trimmed, ":::") {
			if addr, err := netip.ParseAddr(trimmed[1:]); err == nil && addr.Is6() {
				return prefix + ":<ip>" + suffix
			}
		}
		return cand
	})
}

// Redact masks everything on a log line that could identify the user or
// their notes. It is idempotent: its own tags pass through unchanged.
func Redact(s string) string {
	return redactInternal(s, true)
}

// RedactDiagnostics applies the same path, host, IP, secret, user, and
// non-ASCII masking as Redact, plus explicit home-directory scrubbing, while
// preserving safe ASCII quoted error phrases in diagnostic reports.
func RedactDiagnostics(s string) string {
	if s == "" {
		return s
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, cand := range []string{home, filepath.Clean(home), filepath.ToSlash(home)} {
			if len(cand) > 1 {
				s = strings.ReplaceAll(s, cand, "<home>")
			}
		}
	}
	return redactInternal(s, false)
}

func redactInternal(s string, maskQuoted bool) string {
	if s == "" {
		return s
	}
	stages := []func(string) string{
		func(p string) string { return reURL.ReplaceAllStringFunc(p, hostTag) },
		func(p string) string { return reEmail.ReplaceAllString(p, "<email>") },
		func(p string) string { return reBearer.ReplaceAllString(p, "$1 <key>") },
		func(p string) string { return reKV.ReplaceAllString(p, "$1$2<key>") },
		func(p string) string { return reSecret.ReplaceAllString(p, "<key>") },
		redactPaths,
		func(p string) string { return reSingleDrive.ReplaceAllStringFunc(p, pathTag) },
		func(p string) string {
			return reSingleRoot.ReplaceAllStringFunc(p, func(m string) string {
				sub := reSingleRoot.FindStringSubmatch(m)
				if len(sub) != 3 {
					return m
				}
				return sub[1] + " " + pathTag(sub[2])
			})
		},
	}
	if maskQuoted {
		stages = append(stages, nil) // quoted text: runs on the whole line, see redactQuoted
	}
	stages = append(stages,
		func(p string) string { return reBlob.ReplaceAllString(p, "<key>") },
		redactIPv6,
		func(p string) string { return reIPv4.ReplaceAllString(p, "<ip>") },
		func(p string) string { return reFile.ReplaceAllStringFunc(p, fileTag) },
		redactNonASCII,
	)
	if reUserName != nil {
		stages = append(stages, func(p string) string { return reUserName.ReplaceAllString(p, "<user>") })
	}
	for _, stage := range stages {
		if stage == nil {
			s = redactQuoted(s)
			continue
		}
		s = outsideTags(s, stage)
	}
	return s
}

// redactError masks the paths and URLs an error chain carries as fields
// before the generic rules run, so a path with spaces is masked whole.
func redactError(err error) string {
	msg := err.Error()
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch v := e.(type) {
		case *fs.PathError:
			msg = replaceLiteral(msg, v.Path, pathTag(v.Path))
		case *os.LinkError:
			msg = replaceLiteral(msg, v.Old, pathTag(v.Old))
			msg = replaceLiteral(msg, v.New, pathTag(v.New))
		case *url.Error:
			msg = replaceLiteral(msg, v.URL, hostTag(v.URL))
		}
	}
	return msg
}

func replaceLiteral(s, old, tag string) string {
	if old == "" {
		return s
	}
	return strings.ReplaceAll(s, old, tag)
}

var reFileNameArg = regexp.MustCompile(`(?i)\.(?:` + fileExtAlternation + `)$`)

// redactArgs masks log arguments by what they are, before formatting: a
// string that is a path or a file name is replaced whole (spaces included),
// and errors have their path fields masked.
func redactArgs(args []any) []any {
	if len(args) == 0 {
		return args
	}
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case error:
			if v == nil {
				out[i] = a
				continue
			}
			out[i] = redactError(v)
		case string:
			trimmed := strings.TrimSpace(v)
			switch {
			case trimmed == "" || reTag.MatchString(trimmed) && reTag.FindString(trimmed) == trimmed:
				out[i] = v
			case strings.Contains(v, "://"):
				out[i] = v
			case strings.ContainsAny(v, `/\`) && !strings.Contains(v, "\n"):
				out[i] = pathTag(v)
			case reFileNameArg.MatchString(trimmed) && !strings.Contains(v, "\n"):
				out[i] = fileTag(v)
			default:
				out[i] = v
			}
		default:
			out[i] = a
		}
	}
	return out
}

// LogPath masks a path for a log line that is not built through the Log*f
// helpers.
func LogPath(p string) string {
	if p == "" {
		return ""
	}
	return pathTag(p)
}

// RedactionTagSpans returns the [start, end) byte ranges of the mask tags in
// an already-masked line, for highlighting them in the log preview.
func RedactionTagSpans(s string) [][]int {
	return reTag.FindAllStringIndex(s, -1)
}
