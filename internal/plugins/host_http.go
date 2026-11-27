package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultHTTPTimeoutMS       = 30000
	maxHTTPTimeoutMS           = 120000
	defaultHTTPMaxRespBytes    = 16 * 1024 * 1024
	maxHTTPMaxRespBytes        = 64 * 1024 * 1024
	maxHTTPRedirects           = 10
)

var allowedHTTPMethods = map[string]bool{
	"GET":      true,
	"POST":     true,
	"PUT":      true,
	"DELETE":   true,
	"HEAD":     true,
	"PATCH":    true,
	"OPTIONS":  true,
	"PROPFIND": true,
	"MKCOL":    true,
	"MOVE":     true,
	"COPY":     true,
}

func matchNetworkDomains(u *url.URL, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	hostPort := strings.ToLower(u.Host)
	for _, pat := range patterns {
		p := strings.ToLower(strings.TrimSpace(pat))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			suffix := p[1:] // ".example.com"
			if strings.HasSuffix(hostname, suffix) && len(hostname) > len(suffix) {
				return true
			}
			continue
		}
		if strings.Contains(p, ":") {
			if hostPort == p {
				return true
			}
			continue
		}
		if hostname == p {
			return true
		}
	}
	return false
}

var (
	v4LinkLocalPrefix   = netip.MustParsePrefix("169.254.0.0/16")
	v6LinkLocalPrefix   = netip.MustParsePrefix("fe80::/10")
	v4ThisNetworkPrefix = netip.MustParsePrefix("0.0.0.0/8")
	v4LoopbackPrefix    = netip.MustParsePrefix("127.0.0.0/8")
	v6LoopbackPrefix    = netip.MustParsePrefix("::1/128")
	v4Private10         = netip.MustParsePrefix("10.0.0.0/8")
	v4Private172        = netip.MustParsePrefix("172.16.0.0/12")
	v4Private192        = netip.MustParsePrefix("192.168.0.0/16")
	v6UniqueLocal       = netip.MustParsePrefix("fc00::/7")
)

func isLocalhostDomain(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return h == "localhost" || strings.HasSuffix(h, ".localhost")
}

func validateDialAddr(addr netip.Addr, reqHost string, networkDomains []string) error {
	ip := addr.Unmap()
	if !ip.IsValid() {
		return errors.New("invalid remote IP address")
	}
	ipNoZone := ip.WithZone("")
	if ipNoZone.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		v4LinkLocalPrefix.Contains(ipNoZone) || v6LinkLocalPrefix.Contains(ipNoZone) ||
		v4ThisNetworkPrefix.Contains(ipNoZone) {
		return fmt.Errorf("connection to restricted IP %s is forbidden", ip)
	}
	if len(networkDomains) > 0 {
		trimmedHost := strings.TrimSuffix(strings.TrimSpace(reqHost), ".")
		if _, err := netip.ParseAddr(trimmedHost); err != nil && !isLocalhostDomain(trimmedHost) {
			if ipNoZone.IsLoopback() || ipNoZone.IsPrivate() ||
				v4LoopbackPrefix.Contains(ipNoZone) || v6LoopbackPrefix.Contains(ipNoZone) ||
				v4Private10.Contains(ipNoZone) || v4Private172.Contains(ipNoZone) ||
				v4Private192.Contains(ipNoZone) || v6UniqueLocal.Contains(ipNoZone) {
				return fmt.Errorf("connection to private or loopback IP %s via hostname %q is forbidden", ip, reqHost)
			}
		}
	}
	return nil
}

func (rt *Runtime) newHTTPTransport(timeout time.Duration) *http.Transport {
	var tr *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		tr = base.Clone()
	} else {
		tr = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	lookup := rt.lookupNetIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	domains := append([]string(nil), rt.manifest.NetworkDomains...)

	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		if req != nil && req.URL != nil {
			host := strings.TrimSuffix(strings.TrimSpace(req.URL.Hostname()), ".")
			if ip, err := netip.ParseAddr(host); err == nil {
				if err := validateDialAddr(ip, host, domains); err != nil {
					return nil, err
				}
			}
		}
		if rt.lookupNetIP != nil {
			return nil, nil
		}
		return http.ProxyFromEnvironment(req)
	}

	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		trimmedHost := strings.TrimSuffix(strings.TrimSpace(host), ".")
		if ip, perr := netip.ParseAddr(trimmedHost); perr == nil {
			if err := validateDialAddr(ip, host, domains); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, addr)
		}

		lookupNet := "ip"
		switch network {
		case "tcp4":
			lookupNet = "ip4"
		case "tcp6":
			lookupNet = "ip6"
		}
		ips, err := lookup(ctx, lookupNet, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses found for %s", host)
		}
		for _, ip := range ips {
			if err := validateDialAddr(ip, host, domains); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("dial %s failed", addr)
		}
		return nil, lastErr
	}
	return tr
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func (rt *Runtime) hostCallContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	done := make(chan struct{})
	go func() {
		select {
		case <-rt.exited:
			cancel()
		case <-done:
		}
	}()
	return ctx, func() {
		close(done)
		cancel()
	}
}

func (rt *Runtime) dispatchHTTPHostMethod(params json.RawMessage) (any, *rpcError) {
	if !rt.manifest.HasPermission(PermNetwork) {
		return nil, missingPerm(PermNetwork)
	}
	var p struct {
		URL              string            `json:"url"`
		Method           string            `json:"method"`
		Headers          map[string]string `json:"headers"`
		Body             *string           `json:"body"`
		BodyBase64       *string           `json:"body_base64"`
		BodyFilePath     *string           `json:"body_file_path"`
		SaveToPath       string            `json:"save_to_path"`
		MkdirParents     bool              `json:"mkdir_parents"`
		ResponseEncoding string            `json:"response_encoding"`
		TimeoutMS        int               `json:"timeout_ms"`
		MaxResponseBytes int64             `json:"max_response_bytes"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErr(-32602, "invalid host.http.request params: "+err.Error())
	}

	rawURL := strings.TrimSpace(p.URL)
	if rawURL == "" || strings.ContainsRune(rawURL, 0) {
		return nil, rpcErr(-32602, "host.http.request requires a valid url")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, rpcErr(-32602, "invalid url: "+err.Error())
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if (scheme != "http" && scheme != "https") || parsedURL.Hostname() == "" {
		return nil, rpcErr(-32602, "only http and https URLs with a valid host are allowed")
	}
	if !matchNetworkDomains(parsedURL, rt.manifest.NetworkDomains) {
		return nil, rpcErr(-32001, "target domain is not in manifest.network_domains allowlist")
	}

	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = "GET"
	}
	if !allowedHTTPMethods[method] {
		return nil, rpcErr(-32602, "unsupported HTTP method: "+method)
	}

	bodyCount := 0
	if p.Body != nil {
		bodyCount++
	}
	if p.BodyBase64 != nil {
		bodyCount++
	}
	if p.BodyFilePath != nil {
		bodyCount++
	}
	if bodyCount > 1 {
		return nil, rpcErr(-32602, "at most one of body, body_base64, or body_file_path may be set")
	}

	if p.TimeoutMS < 0 {
		return nil, rpcErr(-32602, "timeout_ms must be non-negative")
	}
	timeoutMS := p.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = defaultHTTPTimeoutMS
	}
	if timeoutMS > maxHTTPTimeoutMS {
		timeoutMS = maxHTTPTimeoutMS
	}

	if p.MaxResponseBytes < 0 {
		return nil, rpcErr(-32602, "max_response_bytes must be non-negative")
	}
	maxRespBytes := p.MaxResponseBytes
	if maxRespBytes == 0 {
		maxRespBytes = defaultHTTPMaxRespBytes
	}
	if maxRespBytes > maxHTTPMaxRespBytes {
		maxRespBytes = maxHTTPMaxRespBytes
	}

	respEnc := strings.ToLower(strings.TrimSpace(p.ResponseEncoding))
	if respEnc == "" {
		if strings.TrimSpace(p.SaveToPath) != "" {
			respEnc = "none"
		} else {
			respEnc = "utf8"
		}
	}
	if respEnc != "utf8" && respEnc != "base64" && respEnc != "none" {
		return nil, rpcErr(-32602, "response_encoding must be one of utf8, base64, none")
	}

	var saveRoot *os.Root
	var saveTarget *resolvedFSTarget
	if strings.TrimSpace(p.SaveToPath) != "" {
		var rerr *rpcError
		saveRoot, saveTarget, rerr = rt.openFSWriteRoot(p.SaveToPath, p.MkdirParents)
		if rerr != nil {
			return nil, rerr
		}
		defer saveRoot.Close()
	}

	var bodyReader io.Reader
	var contentLen int64 = -1
	switch {
	case p.Body != nil:
		data := []byte(*p.Body)
		bodyReader = bytes.NewReader(data)
		contentLen = int64(len(data))
	case p.BodyBase64 != nil:
		data, err := base64.StdEncoding.DecodeString(*p.BodyBase64)
		if err != nil {
			return nil, rpcErr(-32602, "invalid body_base64: "+err.Error())
		}
		bodyReader = bytes.NewReader(data)
		contentLen = int64(len(data))
	case p.BodyFilePath != nil:
		f, info, froot, _, rerr := rt.openFSReadFile(*p.BodyFilePath)
		if rerr != nil {
			return nil, rerr
		}
		defer f.Close()
		defer froot.Close()
		bodyReader = f
		contentLen = info.Size()
	}

	ctx, cancel := rt.hostCallContext(time.Duration(timeoutMS) * time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bodyReader)
	if err != nil {
		return nil, rpcErr(-32602, "build HTTP request: "+err.Error())
	}
	if contentLen >= 0 {
		req.ContentLength = contentLen
	}
	for k, v := range p.Headers {
		if strings.ContainsAny(k, "\r\n\x00") || strings.ContainsAny(v, "\r\n\x00") {
			return nil, rpcErr(-32602, "header key and value must not contain CR, LF, or NUL")
		}
		if strings.EqualFold(strings.TrimSpace(k), "Host") {
			continue
		}
		req.Header.Set(k, v)
	}

	tr := rt.newHTTPTransport(time.Duration(timeoutMS) * time.Millisecond)
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Timeout:   time.Duration(timeoutMS) * time.Millisecond,
		Transport: tr,
		CheckRedirect: func(nextReq *http.Request, via []*http.Request) error {
			if len(via) >= maxHTTPRedirects {
				return errors.New("stopped after 10 redirects")
			}
			sc := strings.ToLower(nextReq.URL.Scheme)
			if (sc != "http" && sc != "https") || nextReq.URL.Hostname() == "" {
				return errors.New("redirect to non-http/https URL is forbidden")
			}
			if !matchNetworkDomains(nextReq.URL, rt.manifest.NetworkDomains) {
				return errors.New("redirect target domain is not in manifest.network_domains allowlist")
			}
			if len(via) > 0 && !sameHTTPOrigin(via[0].URL, nextReq.URL) {
				nextReq.Header.Del("Authorization")
				nextReq.Header.Del("Proxy-Authorization")
				nextReq.Header.Del("Cookie")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, rpcErr(-32005, err.Error())
	}
	defer resp.Body.Close()

	respHeaders := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		respHeaders[strings.ToLower(k)] = strings.Join(vals, ", ")
	}

	var respBytes int64
	var respData []byte

	if saveTarget != nil {
		n, err := writeHTTPBodyToRoot(saveRoot, saveTarget, resp.Body, maxRespBytes)
		if err != nil {
			return nil, rpcErr(-32005, err.Error())
		}
		respBytes = n
		if respEnc == "utf8" || respEnc == "base64" {
			b, err := readPluginFile(saveRoot, saveTarget.rel, maxRespBytes)
			if err != nil {
				return nil, rpcErr(-32004, err.Error())
			}
			respData = b
		}
	} else {
		if respEnc == "none" {
			n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxRespBytes+1))
			if err != nil {
				return nil, rpcErr(-32005, err.Error())
			}
			if n > maxRespBytes {
				return nil, rpcErr(-32005, "response body exceeds max_response_bytes")
			}
			respBytes = n
		} else {
			b, err := readLimited(resp.Body, maxRespBytes)
			if err != nil {
				return nil, rpcErr(-32005, err.Error())
			}
			respData = b
			respBytes = int64(len(b))
		}
	}

	finalURL := parsedURL.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	out := map[string]any{
		"status":    resp.StatusCode,
		"headers":   respHeaders,
		"bytes":     respBytes,
		"final_url": finalURL,
	}
	if saveTarget != nil {
		out["saved_path"] = saveTarget.canonPath
	}
	switch respEnc {
	case "utf8":
		if !utf8.Valid(respData) {
			return nil, rpcErr(-32005, "response body is not valid UTF-8; use response_encoding \"base64\" or \"none\"")
		}
		out["body"] = string(respData)
	case "base64":
		out["body_base64"] = base64.StdEncoding.EncodeToString(respData)
	}
	return out, nil
}

func writeHTTPBodyToRoot(root *os.Root, target *resolvedFSTarget, body io.Reader, maxBytes int64) (int64, error) {
	if !target.isDirRoot {
		b, err := readLimited(body, maxBytes)
		if err != nil {
			return 0, err
		}
		f, err := root.OpenFile(target.rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return 0, err
		}
		if _, err := f.Write(b); err != nil {
			_ = f.Close()
			return 0, err
		}
		if err := f.Close(); err != nil {
			return 0, err
		}
		return int64(len(b)), nil
	}

	f, tmpRel, err := createTempInRoot(root, target.rel)
	if err != nil {
		return 0, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = f.Close()
			_ = root.Remove(tmpRel)
		}
	}()

	n, err := io.Copy(f, io.LimitReader(body, maxBytes+1))
	if err != nil {
		return 0, err
	}
	if n > maxBytes {
		return 0, errors.New("response body exceeds max_response_bytes")
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	if err := root.Rename(tmpRel, target.rel); err != nil {
		return 0, err
	}
	cleanup = false
	return n, nil
}
