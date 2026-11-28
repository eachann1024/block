package pluginui

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"block/internal/icons"
	"block/internal/media"
	"block/internal/plugins"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const (
	maxLogMediaEntries = 64
	maxLogMediaBytes   = 10 << 20
	maxLogMediaSVG     = 512 << 10
	maxLogMediaPixels  = 8_000_000
	maxLogMediaTotalPx = 24_000_000
	logMediaRetryTTL   = 60 * time.Second
)

type logMediaAsset struct {
	Image       ui.ImageSource
	Loading     bool
	Unavailable bool
	failedAt    time.Time
}

type logMediaStore struct {
	mu      sync.Mutex
	entries map[string]logMediaAsset
	order   []string
	pixels  int
	slots   chan struct{}
}

func newLogMediaStore() *logMediaStore {
	return &logMediaStore{
		entries: make(map[string]logMediaAsset),
		slots:   make(chan struct{}, 3),
	}
}

var defaultLogMediaStore = newLogMediaStore()

type logEntryMediaSpec struct {
	HasBox     bool
	FetchImage bool
	MediaURL   string
	FileName   string
	Icon       icons.Icon
}

func classifyLogEntryMedia(e plugins.UILogEntry) logEntryMediaSpec {
	rawMedia := strings.TrimSpace(e.MediaURL)
	rawFile := strings.TrimSpace(e.FileName)
	if rawMedia == "" && rawFile == "" {
		return logEntryMediaSpec{}
	}
	name := rawFile
	if name == "" {
		name = fileNameFromMediaURL(rawMedia)
	}
	if name == "" {
		name = inferFileNameFromTitle(e.Title)
	}
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(name, "\\", "/")))
	if ext == "" && rawMedia != "" {
		if u, err := url.Parse(rawMedia); err == nil {
			ext = strings.ToLower(path.Ext(u.Path))
		}
	}
	fetchable := validLogMediaURLString(rawMedia) && (ext == "" || isDecodableLogImageExt(ext))
	return logEntryMediaSpec{
		HasBox:     true,
		FetchImage: fetchable,
		MediaURL:   rawMedia,
		FileName:   name,
		Icon:       logEntryFileIcon(name, ext),
	}
}

func fileNameFromMediaURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Path == "" {
		return ""
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
		return decoded
	}
	return base
}

func inferFileNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if idx := strings.Index(t, " -> "); idx >= 0 {
		t = strings.TrimSpace(t[idx+4:])
	}
	return t
}

func isDecodableLogImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	default:
		return false
	}
}

func logEntryFileIcon(name, ext string) icons.Icon {
	if name != "" {
		if ico, ok := icons.FileIcon(name); ok {
			return ico
		}
	}
	if ext != "" {
		if ico, ok := icons.FileIcon("file" + ext); ok {
			return ico
		}
	}
	return icons.Paperclip
}

func validLogMediaURLString(raw string) bool {
	if raw == "" || len(raw) > 16*1024 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return validLogMediaURL(u)
}

func validLogMediaURL(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Hostname() != ""
}

func resolveLogMediaAsset(bindings *InputBindings, rawURL string, services ui.Services) logMediaAsset {
	store := defaultLogMediaStore
	if bindings != nil && bindings.LogMedia != nil {
		store = bindings.LogMedia
	}
	return store.resolve(rawURL, services)
}

func (s *logMediaStore) resolve(rawURL string, services ui.Services) logMediaAsset {
	if !validLogMediaURLString(rawURL) {
		return logMediaAsset{Unavailable: true}
	}
	if testing.Testing() && !isLoopbackURL(rawURL) {
		return logMediaAsset{Unavailable: true}
	}
	now := time.Now()
	s.mu.Lock()
	if cur, ok := s.entries[rawURL]; ok {
		if cur.Image != nil || cur.Loading {
			s.mu.Unlock()
			return cur
		}
		if cur.Unavailable && now.Sub(cur.failedAt) < logMediaRetryTTL {
			s.mu.Unlock()
			return cur
		}
	}
	s.entries[rawURL] = logMediaAsset{Loading: true}
	s.touchOrderLocked(rawURL)
	s.mu.Unlock()

	go s.load(rawURL, services)
	return logMediaAsset{Loading: true}
}

func isLoopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

func (s *logMediaStore) touchOrderLocked(key string) {
	for i, existing := range s.order {
		if existing == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, key)
}

func (s *logMediaStore) storeLocked(key string, asset logMediaAsset) {
	if prev, ok := s.entries[key]; ok && prev.Image != nil {
		w, h := ui.ImageSize(prev.Image)
		s.pixels -= int(w * h)
	}
	if asset.Image != nil {
		w, h := ui.ImageSize(asset.Image)
		s.pixels += int(w * h)
	}
	s.entries[key] = asset
	s.touchOrderLocked(key)
	for len(s.order) > 0 && (len(s.order) > maxLogMediaEntries || s.pixels > maxLogMediaTotalPx) {
		evict := s.order[0]
		s.order = s.order[1:]
		if evict == key {
			continue
		}
		if prev, ok := s.entries[evict]; ok {
			if prev.Image != nil {
				w, h := ui.ImageSize(prev.Image)
				s.pixels -= int(w * h)
			}
			delete(s.entries, evict)
		}
	}
}

func (s *logMediaStore) load(rawURL string, services ui.Services) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := fetchLogMediaBytes(ctx, rawURL)
	var decoded ui.ImageSource
	if err == nil {
		decoded = decodeLogMediaImage(data)
	}

	s.mu.Lock()
	if decoded != nil {
		s.storeLocked(rawURL, logMediaAsset{Image: decoded})
	} else {
		s.storeLocked(rawURL, logMediaAsset{Unavailable: true, failedAt: time.Now()})
	}
	s.mu.Unlock()
	services.Invalidate()
}

func decodeLogMediaImage(data []byte) ui.ImageSource {
	if len(data) == 0 || len(data) > maxLogMediaBytes {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil && cfg.Width > 0 && cfg.Height > 0 &&
		cfg.Width <= media.MaxImageSide && cfg.Height <= media.MaxImageSide &&
		cfg.Width <= maxLogMediaPixels/cfg.Height {
		if img, decErr := ui.DecodeBitmap(data); decErr == nil {
			return img
		}
	}
	if err != nil && len(data) <= maxLogMediaSVG {
		if svg, svgErr := parseLogMediaSVG(data); svgErr == nil {
			return svg
		}
	}
	return nil
}

func parseLogMediaSVG(raw []byte) (*ui.SVG, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, nodes := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok := token.(type) {
		case xml.Directive:
			return nil, media.ErrInvalid
		case xml.StartElement:
			if depth == 0 && (nodes != 0 || tok.Name.Local != "svg") {
				return nil, media.ErrInvalid
			}
			depth++
			nodes++
			if depth > 64 || nodes > 8192 {
				return nil, media.ErrInvalid
			}
			for _, attr := range tok.Attr {
				if attr.Name.Local == "href" && !strings.HasPrefix(strings.TrimSpace(attr.Value), "#") {
					return nil, media.ErrInvalid
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 || nodes == 0 {
		return nil, media.ErrInvalid
	}
	svg, err := ui.ParseSVG(raw)
	if err != nil {
		return nil, err
	}
	w, h := svg.Size()
	if !(w > 0 && h > 0 && w <= 4096 && h <= 4096) {
		return nil, media.ErrInvalid
	}
	return svg, nil
}

var errLogMediaBlocked = errors.New("log media unavailable")

var (
	logMediaThisNet     = netip.MustParsePrefix("0.0.0.0/8")
	logMediaLinkLocalV4 = netip.MustParsePrefix("169.254.0.0/16")
	logMediaLinkLocalV6 = netip.MustParsePrefix("fe80::/10")
)

func validateLogMediaAddr(addr netip.Addr) error {
	ip := addr.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		logMediaThisNet.Contains(ip) || logMediaLinkLocalV4.Contains(ip) || logMediaLinkLocalV6.Contains(ip) {
		return errLogMediaBlocked
	}
	return nil
}

var logMediaClient = &http.Client{
	Transport: &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			if req != nil && req.URL != nil {
				host := strings.TrimSuffix(strings.TrimSpace(req.URL.Hostname()), ".")
				if ip, err := netip.ParseAddr(host); err == nil {
					if err := validateLogMediaAddr(ip); err != nil {
						return nil, err
					}
				}
			}
			proxyURL, err := plugins.ProxyForRequest(req)
			if err != nil || proxyURL == nil || req == nil || req.URL == nil {
				return proxyURL, err
			}
			host := strings.TrimSuffix(strings.TrimSpace(req.URL.Hostname()), ".")
			if _, perr := netip.ParseAddr(host); perr != nil {
				ips, lerr := net.DefaultResolver.LookupNetIP(req.Context(), "ip", host)
				if lerr != nil || len(ips) == 0 {
					return nil, errLogMediaBlocked
				}
				for _, ip := range ips {
					if verr := validateLogMediaAddr(ip); verr != nil {
						return nil, verr
					}
				}
			}
			return proxyURL, nil
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			trimmed := strings.TrimSuffix(strings.TrimSpace(host), ".")
			if ip, err := netip.ParseAddr(trimmed); err == nil {
				if err := validateLogMediaAddr(ip); err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errLogMediaBlocked
			}
			for _, ip := range ips {
				if err := validateLogMediaAddr(ip); err != nil {
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
				lastErr = errLogMediaBlocked
			}
			return nil, lastErr
		},
		DisableKeepAlives: true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validLogMediaURL(req.URL) {
			return errLogMediaBlocked
		}
		return nil
	},
}

func fetchLogMediaBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Block/1.0")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	resp, err := logMediaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxLogMediaBytes {
		return nil, errLogMediaBlocked
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if ct != "" && !strings.HasPrefix(ct, "image/") && ct != "application/octet-stream" && ct != "binary/octet-stream" {
		return nil, errLogMediaBlocked
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogMediaBytes+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maxLogMediaBytes {
		return nil, errLogMediaBlocked
	}
	return data, nil
}
