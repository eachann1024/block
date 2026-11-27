package updater

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"block/internal/files"
)

var knownPublicUpdaterHosts = map[string]bool{
	"gh-proxy.com":                         true,
	"ghproxy.net":                          true,
	"raw.githubusercontent.com":            true,
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
	"codeload.github.com":                  true,
}

func isKnownUpdaterHost(host string) bool {
	return knownPublicUpdaterHosts[strings.ToLower(strings.TrimSpace(host))]
}

var (
	reDiagHostToken = regexp.MustCompile(`(?i)\b([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)(:\d{1,5})?\b`)
	reDiagLookup    = regexp.MustCompile(`(?i)\blookup\s+([^\s:;,]+)`)
	reDiagOnDNS     = regexp.MustCompile(`(?i)\bon\s+([^\s:;,]+(?::\d{1,5})?)`)
	reDiagProxyConn = regexp.MustCompile(`(?i)\b(proxyconnect\s+[a-z0-9]+:\s*dial\s+[a-z0-9]+\s+)([^\s:;,]+(?::\d{1,5})?)`)
)

func scrubUnknownHosts(s string) string {
	s = reDiagLookup.ReplaceAllStringFunc(s, func(m string) string {
		sub := reDiagLookup.FindStringSubmatch(m)
		if len(sub) != 2 {
			return "lookup <host>"
		}
		if isKnownUpdaterHost(sub[1]) {
			return "lookup " + strings.ToLower(sub[1])
		}
		return "lookup <host>"
	})
	s = reDiagOnDNS.ReplaceAllStringFunc(s, func(m string) string {
		sub := reDiagOnDNS.FindStringSubmatch(m)
		if len(sub) != 2 {
			return "on <dns>"
		}
		host := sub[1]
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if isKnownUpdaterHost(host) {
			return "on " + strings.ToLower(host)
		}
		return "on <dns>"
	})
	s = reDiagProxyConn.ReplaceAllString(s, "$1<proxy>")
	s = reDiagHostToken.ReplaceAllStringFunc(s, func(m string) string {
		host := m
		if idx := strings.LastIndexByte(host, ':'); idx > 0 {
			host = host[:idx]
		}
		lower := strings.ToLower(host)
		if isKnownUpdaterHost(lower) {
			return lower
		}
		// Keep version-like numbers (e.g. 1.0.942) intact.
		allDigitsDots := true
		for i := 0; i < len(lower); i++ {
			c := lower[i]
			if (c < '0' || c > '9') && c != '.' {
				allDigitsDots = false
				break
			}
		}
		if allDigitsDots {
			return m
		}
		return "<host>"
	})
	return s
}

func truncateDiagField(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 3 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-3]) + "..."
}

// SanitizeNetworkError strips credentials, local paths, usernames, IPs, internal hostnames,
// and non-official URLs from an updater error while preserving technical failure reasons.
func SanitizeNetworkError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch v := e.(type) {
		case *fs.PathError:
			if v.Path != "" {
				msg = strings.ReplaceAll(msg, v.Path, "<path>")
			}
		case *os.LinkError:
			if v.Old != "" {
				msg = strings.ReplaceAll(msg, v.Old, "<path>")
			}
			if v.New != "" {
				msg = strings.ReplaceAll(msg, v.New, "<path>")
			}
		case *net.DNSError:
			if v.Name != "" && !isKnownUpdaterHost(v.Name) {
				msg = strings.ReplaceAll(msg, v.Name, "<host>")
			}
			if v.Server != "" {
				msg = strings.ReplaceAll(msg, v.Server, "<dns>")
			}
		case *net.OpError:
			if v.Addr != nil {
				addrStr := v.Addr.String()
				host := addrStr
				if h, _, splitErr := net.SplitHostPort(addrStr); splitErr == nil {
					host = h
				}
				if addrStr != "" && !isKnownUpdaterHost(host) {
					msg = strings.ReplaceAll(msg, addrStr, "<addr>")
				}
			}
			if v.Source != nil {
				srcStr := v.Source.String()
				if srcStr != "" {
					msg = strings.ReplaceAll(msg, srcStr, "<local>")
				}
			}
		case *url.Error:
			if v.URL != "" {
				rep := "<url>"
				if parsed, pErr := url.Parse(v.URL); pErr == nil && isKnownUpdaterHost(parsed.Hostname()) {
					rep = "host:" + strings.ToLower(parsed.Hostname())
				}
				msg = strings.ReplaceAll(msg, v.URL, rep)
			}
		}
	}
	msg = files.RedactDiagnostics(msg)
	msg = scrubUnknownHosts(msg)
	return truncateDiagField(msg, 96)
}

func sanitizeDiagString(raw, dataDir string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if d := strings.TrimSpace(dataDir); len(d) > 1 {
		for _, cand := range []string{d, filepath.Clean(d), filepath.ToSlash(d)} {
			if len(cand) > 1 {
				s = strings.ReplaceAll(s, cand, "<appdata>")
			}
		}
	}
	s = files.RedactDiagnostics(s)
	s = scrubUnknownHosts(s)
	return truncateDiagField(s, 96)
}

func isSchemeChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'
}

func sanitizeProxyScheme(raw string) string {
	val := strings.TrimSpace(raw)
	if val == "" {
		return ""
	}
	if idx := strings.Index(val, "://"); idx > 0 {
		cand := strings.ToLower(strings.TrimSpace(val[:idx]))
		if len(cand) > 0 && len(cand) <= 16 {
			valid := true
			for i := 0; i < len(cand); i++ {
				if !isSchemeChar(cand[i]) {
					valid = false
					break
				}
			}
			if valid {
				return cand
			}
		}
		return "unknown"
	}
	return "http"
}

func readEnvCaseInsensitive(upper, lower string) string {
	if v := strings.TrimSpace(os.Getenv(upper)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(lower))
}

// SanitizedProxySummary reports whether proxy environment variables are set and their protocol
// scheme only, strictly omitting hosts, ports, credentials, and internal domains.
func SanitizedProxySummary() string {
	parts := make([]string, 0, 4)
	for _, pair := range [][2]string{
		{"HTTP_PROXY", "http_proxy"},
		{"HTTPS_PROXY", "https_proxy"},
		{"ALL_PROXY", "all_proxy"},
	} {
		if val := readEnvCaseInsensitive(pair[0], pair[1]); val == "" {
			parts = append(parts, pair[0]+"=unset")
		} else {
			parts = append(parts, fmt.Sprintf("%s=set(scheme=%s)", pair[0], sanitizeProxyScheme(val)))
		}
	}
	if noProxy := readEnvCaseInsensitive("NO_PROXY", "no_proxy"); noProxy == "" {
		parts = append(parts, "NO_PROXY=unset")
	} else {
		count := 0
		for _, item := range strings.Split(noProxy, ",") {
			if strings.TrimSpace(item) != "" {
				count++
			}
		}
		parts = append(parts, fmt.Sprintf("NO_PROXY=set(entries=%d)", count))
	}
	return strings.Join(parts, " ")
}

var allowedUpdaterLogEvents = []string{
	"probe.",
	"check.",
	"download.",
	"config.",
	"update.",
	"install.",
}

var allowedUpdaterLogKV = map[string]bool{
	"mode":                true,
	"channel":             true,
	"kind":                true,
	"latency_ms":          true,
	"http_status":         true,
	"err_kind":            true,
	"manual":              true,
	"current":             true,
	"latest":              true,
	"has_newer":           true,
	"aggregated_versions": true,
	"releases":            true,
	"version":             true,
	"tag":                 true,
	"expected_bytes":      true,
	"attempt":             true,
	"elapsed_ms":          true,
	"candidates":          true,
	"os":                  true,
	"arch":                true,
	"asset":               true,
}

func isAllowedUpdaterEvent(event string) bool {
	for _, prefix := range allowedUpdaterLogEvents {
		if strings.HasPrefix(event, prefix) {
			return true
		}
	}
	return false
}

func isSafeScalarToken(v string) bool {
	if v == "" || len(v) > 48 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == '_' || c == '+' {
			continue
		}
		return false
	}
	return true
}

func splitKVTokens(rest string) []string {
	var tokens []string
	i := 0
	n := len(rest)
	for i < n {
		for i < n && rest[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		start := i
		inQuote := false
		for i < n {
			c := rest[i]
			if c == '\\' && inQuote && i+1 < n {
				i += 2
				continue
			}
			if c == '"' {
				inQuote = !inQuote
			} else if c == ' ' && !inQuote {
				break
			}
			i++
		}
		tokens = append(tokens, rest[start:i])
	}
	return tokens
}

// SanitizeUpdaterLogLine rebuilds an updater.log line from an explicit allowlist of events
// and structured telemetry fields so arbitrary free text, paths, or private domains never leak.
func SanitizeUpdaterLogLine(line, dataDir string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, " ", 4)
	if len(parts) < 3 {
		return ""
	}
	ts, level, event := parts[0], parts[1], parts[2]
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		return ""
	}
	if level != "[INFO]" && level != "[WARN]" && level != "[ERROR]" {
		return ""
	}
	if !isAllowedUpdaterEvent(event) {
		return ""
	}
	if len(parts) == 3 {
		return ts + " " + level + " " + event
	}

	tokens := splitKVTokens(parts[3])
	cleanTokens := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			continue
		}
		k := tok[:eq]
		v := tok[eq+1:]
		if unq, err := strconv.Unquote(v); err == nil {
			v = unq
		}
		switch {
		case k == "host":
			if isKnownUpdaterHost(v) {
				cleanTokens = append(cleanTokens, "host="+strings.ToLower(strings.TrimSpace(v)))
			} else {
				cleanTokens = append(cleanTokens, "host=<host>")
			}
		case k == "url":
			if parsed, err := url.Parse(v); err == nil && isKnownUpdaterHost(parsed.Hostname()) {
				cleanTokens = append(cleanTokens, "host="+strings.ToLower(parsed.Hostname()))
			} else {
				cleanTokens = append(cleanTokens, "host=<host>")
			}
		case k == "err" || k == "reason":
			cleanVal := truncateDiagField(sanitizeDiagString(v, dataDir), 72)
			if cleanVal != "" {
				if strings.ContainsAny(cleanVal, " \t\"") {
					cleanTokens = append(cleanTokens, fmt.Sprintf("%s=%q", k, cleanVal))
				} else {
					cleanTokens = append(cleanTokens, k+"="+cleanVal)
				}
			}
		case allowedUpdaterLogKV[k]:
			cleanVal := strings.TrimSpace(v)
			if k == "asset" {
				if !allowlistedInstallers[cleanVal] {
					cleanVal = "<asset>"
				}
			} else if !isSafeScalarToken(cleanVal) {
				cleanVal = truncateDiagField(sanitizeDiagString(cleanVal, dataDir), 40)
			}
			if cleanVal != "" {
				cleanTokens = append(cleanTokens, k+"="+cleanVal)
			}
		}
	}
	if len(cleanTokens) == 0 {
		return ts + " " + level + " " + event
	}
	return ts + " " + level + " " + event + " " + strings.Join(cleanTokens, " ")
}

// ReadSanitizedUpdaterLogTail reads up to maxLines from the tail of updater.log using strict allowlist sanitization.
func ReadSanitizedUpdaterLogTail(dataDir string, maxLines, maxBytes int) []string {
	logPath := LogFilePath(dataDir)
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	const tailReadCap = 64 * 1024
	seeked := false
	if info, stErr := f.Stat(); stErr == nil && info.Size() > tailReadCap {
		if _, sErr := f.Seek(info.Size()-tailReadCap, 0); sErr == nil {
			seeked = true
		}
	}
	raw, _ := io.ReadAll(io.LimitReader(f, tailReadCap))
	if len(raw) == 0 {
		return nil
	}
	rawLines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if seeked && len(rawLines) > 0 {
		rawLines = rawLines[1:]
	}

	var cleaned []string
	for _, rawLine := range rawLines {
		if s := SanitizeUpdaterLogLine(rawLine, dataDir); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	if maxLines > 0 && len(cleaned) > maxLines {
		cleaned = cleaned[len(cleaned)-maxLines:]
	}
	if maxBytes > 0 {
		total := 0
		start := len(cleaned)
		for i := len(cleaned) - 1; i >= 0; i-- {
			lineCost := len(cleaned[i]) + 1
			if total+lineCost > maxBytes && start < len(cleaned) {
				break
			}
			total += lineCost
			start = i
		}
		cleaned = cleaned[start:]
	}
	return cleaned
}

func formatDiagTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}

func mergeProbeWithDefaults(channel string, cp ChannelProbe) ChannelProbe {
	def := DefaultChannelProbe(channel)
	if len(cp.Nodes) == 0 {
		if cp.Status == "" {
			return def
		}
		cp.Nodes = def.Nodes
		return cp
	}
	byHost := make(map[string]NodeProbe, len(cp.Nodes))
	for _, n := range cp.Nodes {
		byHost[n.Host] = n
	}
	merged := make([]NodeProbe, 0, len(def.Nodes))
	for _, dn := range def.Nodes {
		if existing, ok := byHost[dn.Host]; ok {
			merged = append(merged, existing)
			delete(byHost, dn.Host)
		} else {
			merged = append(merged, dn)
		}
	}
	for _, extra := range cp.Nodes {
		if _, left := byHost[extra.Host]; left {
			merged = append(merged, extra)
		}
	}
	cp.Nodes = merged
	if cp.Channel == "" {
		cp.Channel = channel
	}
	if cp.Status == "" {
		cp.Status = ProbeIdle
	}
	return cp
}

// BuildDiagnosticsReport compiles a privacy-scrubbed, developer-ready updater network
// troubleshooting report suitable for pre-filling the Feedback modal.
func BuildDiagnosticsReport(snap Snapshot, dataDir string, showEn bool) string {
	var header string
	if showEn {
		header = "(Optional: add details above)\n\n"
	} else {
		header = "（可在上方补充问题描述）\n\n"
	}

	targetVer := snap.LatestVersion
	if targetVer == "" || targetVer == snap.CurrentVersion {
		targetVer = "none"
	}
	phase := snap.LastPhase
	if phase == "" {
		phase = string(snap.Status)
	}
	activeCh := snap.ActiveChannel
	if activeCh == "" {
		activeCh = "none"
	}
	effectiveCh := snap.EffectiveChannel()
	if effectiveCh == "" {
		effectiveCh = "none"
	}
	lastErr := truncateDiagField(snap.LastErrorSanitized, 96)
	if lastErr == "" && strings.TrimSpace(snap.ErrorMessage) != "" {
		lastErr = sanitizeDiagString(snap.ErrorMessage, dataDir)
	}
	if lastErr == "" {
		lastErr = "none"
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("--- Updater Diagnostics ---\n")
	fmt.Fprintf(&b, "app_version: %s\n", snap.CurrentVersion)
	fmt.Fprintf(&b, "target_version: %s\n", targetVer)
	fmt.Fprintf(&b, "os_arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "status: %s | phase: %s\n", snap.Status, phase)
	fmt.Fprintf(&b, "source_mode: %s | active_channel: %s | effective_channel: %s | latency_ms: %d\n",
		snap.SourceMode, activeCh, effectiveCh, snap.FetchLatencyMs)
	fmt.Fprintf(&b, "proxy: %s\n", SanitizedProxySummary())
	fmt.Fprintf(&b, "last_error: %s\n\n", lastErr)

	b.WriteString("[probes]\n")
	for _, ch := range []string{ChannelMirror, ChannelGitHub} {
		cp := mergeProbeWithDefaults(ch, snap.ProbeFor(ch))
		bestHost := cp.BestHost
		if bestHost == "" || !isKnownUpdaterHost(bestHost) {
			bestHost = "none"
		}
		errKind := cp.ErrorKind
		if errKind == "" {
			errKind = "none"
		}
		fmt.Fprintf(&b, "- channel=%s status=%s latency_ms=%d http_status=%d err_kind=%s best_host=%s probed_at=%s\n",
			ch, cp.Status, cp.LatencyMs, cp.HTTPStatus, errKind, bestHost, formatDiagTime(cp.ProbedAt))
		for _, n := range cp.Nodes {
			nStatus := n.Status
			if nStatus == "" {
				nStatus = ProbeIdle
			}
			nErrKind := n.ErrorKind
			if nErrKind == "" {
				nErrKind = "none"
			}
			nHost := n.Host
			if !isKnownUpdaterHost(nHost) {
				nHost = "<host>"
			}
			nErr := truncateDiagField(sanitizeDiagString(n.Error, dataDir), 80)
			if nErr == "" {
				nErr = "none"
			}
			fmt.Fprintf(&b, "  * host=%s kind=%s status=%s latency_ms=%d http_status=%d err_kind=%s err=%s probed_at=%s\n",
				nHost, n.Kind, nStatus, n.LatencyMs, n.HTTPStatus, nErrKind, nErr, formatDiagTime(n.ProbedAt))
		}
	}

	b.WriteString("\n[updater_log_tail]\n")
	const maxDiagRunes = 2800
	const minLogBudget = 600
	prefixRunes := utf8.RuneCountInString(b.String())
	budgetBytes := max(maxDiagRunes-prefixRunes-24, minLogBudget)
	logLines := ReadSanitizedUpdaterLogTail(dataDir, 10, budgetBytes)
	if len(logLines) == 0 {
		b.WriteString("(no updater log entries)")
	} else {
		for i, line := range logLines {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
	}

	out := b.String()
	if utf8.RuneCountInString(out) > maxDiagRunes {
		runes := []rune(out)
		out = string(runes[:maxDiagRunes-3]) + "..."
	}
	return out
}
