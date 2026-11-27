package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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

	client := &http.Client{
		Timeout: time.Duration(timeoutMS) * time.Millisecond,
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
