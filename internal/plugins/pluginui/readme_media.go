package pluginui

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"block/internal/editor"
	"block/internal/plugins"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	readmeImageLimit = 6 << 20
	readmeVideoLimit = 24 << 20
	readmePixelLimit = 8_000_000
)

type readmeMediaKey struct {
	source string
	video  bool
}

type readmeMedia struct {
	ctx      context.Context
	doc      plugins.PluginReadme
	services ui.Services
	slots    chan struct{}
	pixels   atomic.Int64
	mu       sync.Mutex
	assets   map[readmeMediaKey]editor.PreviewAsset
	rawImage map[string][]byte
	videos   int
}

func (m *readmeMedia) asset(src string, video, load bool) editor.PreviewAsset {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.assets == nil {
		m.assets = make(map[readmeMediaKey]editor.PreviewAsset)
	}
	key := readmeMediaKey{src, video}
	if a, ok := m.assets[key]; ok {
		return a
	}
	if src == "" || len(m.assets) >= 24 || (video && m.videos >= 2) {
		return editor.PreviewAsset{Unavailable: true}
	}
	if !load || m.ctx == nil || m.ctx.Err() != nil {
		return editor.PreviewAsset{}
	}
	if video {
		m.videos++
	}
	pending := editor.PreviewAsset{Loading: true}
	m.assets[key] = pending
	go m.load(key)
	return pending
}

func (m *readmeMedia) load(key readmeMediaKey) {
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-m.ctx.Done():
		return
	}
	var asset editor.PreviewAsset
	limit := int64(readmeImageLimit)
	if key.video {
		limit = readmeVideoLimit
	}
	data, err := m.read(key.source, limit)
	if err == nil && m.ctx.Err() == nil {
		if key.video && isReadmeVideoData(data) {
			asset.Video = data
		} else if !key.video {
			asset.Animation = m.decodeAnimation(data)
			if asset.Animation == nil {
				asset.Image = m.decodeImage(data)
			}
		}
	}
	asset.Unavailable = len(asset.Video) == 0 && asset.Image == nil && asset.Animation == nil
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		if asset.Animation != nil {
			asset.Animation.Close()
		}
		return
	}
	m.assets[key] = asset
	if !key.video && !asset.Unavailable && len(data) > 0 {
		if m.rawImage == nil {
			m.rawImage = make(map[string][]byte)
		}
		m.rawImage[key.source] = data
	}
	if key.video && asset.Unavailable {
		m.videos--
	}
	m.mu.Unlock()
	if m.ctx.Err() == nil {
		m.services.Invalidate()
	}
}

func (m *readmeMedia) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, asset := range m.assets {
		if asset.Animation != nil {
			asset.Animation.Close()
		}
	}
	clear(m.assets)
	clear(m.rawImage)
}

// resolveOpenURL resolves src to a safe external HTTPS URL when the README media
// comes from a remote repository or HTTPS link. Local/embedded assets return "".
func (m *readmeMedia) resolveOpenURL(src string) string {
	ref, err := url.Parse(strings.TrimSpace(src))
	if err != nil || ref.Path == "" || strings.Contains(ref.Path, "\\") {
		return ""
	}
	if ref.Scheme == "" && ref.Host == "" {
		if m.doc.LocalRoot != "" || m.doc.LocalFS != nil || m.doc.RemoteBase == nil {
			return ""
		}
		base := m.doc.RemoteBase
		parts := strings.SplitN(base.EscapedPath(), "/", 5)
		if len(parts) < 5 {
			return ""
		}
		repoRoot := strings.Join(parts[:4], "/") + "/"
		escapedRef := ref.EscapedPath()
		if strings.HasPrefix(escapedRef, "/") {
			joined := repoRoot + strings.TrimLeft(escapedRef, "/")
			if parsedRoot, err := url.Parse(joined); err == nil {
				ref.Path = parsedRoot.Path
				ref.RawPath = parsedRoot.RawPath
			}
		}
		ref = base.ResolveReference(ref)
		if !strings.HasPrefix(ref.EscapedPath(), repoRoot) {
			return ""
		}
	}
	if !validReadmeMediaURL(ref) {
		return ""
	}
	return ref.String()
}

var pngMagicHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// copyMedia copies an image bitmap as PNG to the clipboard when available, or
// falls back to copying the resolved HTTPS media URL.
func (m *readmeMedia) copyMedia(svc ui.Services, src string, isVideo bool) bool {
	if m == nil {
		return false
	}
	if !isVideo {
		m.mu.Lock()
		raw := m.rawImage[src]
		m.mu.Unlock()
		if len(raw) > 0 {
			if bytes.HasPrefix(raw, pngMagicHeader) {
				svc.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, raw))))
				return true
			}
			if img, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
				var buf bytes.Buffer
				if err := png.Encode(&buf, img); err == nil {
					svc.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, buf.Bytes()))))
					return true
				}
			}
		}
	}
	if u := m.resolveOpenURL(src); u != "" {
		svc.WriteClipboard(u)
		return true
	}
	return false
}

func (m *readmeMedia) decodeAnimation(data []byte) *ui.VideoPlayer {
	var pixels int64
	var makeDecoder func() (ui.VideoDecoder, error)
	if dec, svgPixels, ok := newSVGAnimationDecoder(data); ok {
		pixels = svgPixels
		makeDecoder = func() (ui.VideoDecoder, error) { return dec, nil }
	} else if gifPixels := gifAnimationPixels(data); gifPixels > 0 {
		pixels = gifPixels
		makeDecoder = func() (ui.VideoDecoder, error) { return ui.NewGIFDecoder(data) }
	} else {
		return nil
	}
	if m.pixels.Add(pixels) <= 3*readmePixelLimit {
		if decoder, err := makeDecoder(); err == nil {
			if player, err := ui.NewVideoPlayer(decoder); err == nil {
				return player.SetLoop(true)
			}
		}
	}
	m.pixels.Add(-pixels)
	return nil
}

func isReadmeVideoData(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	switch string(data[4:8]) {
	case "ftyp", "moov", "mdat", "wide":
		return true
	}
	return bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}) ||
		bytes.HasPrefix(data, []byte("OggS")) ||
		(string(data[:4]) == "RIFF" && string(data[8:12]) == "AVI ")
}

func (m *readmeMedia) decodeImage(data []byte) ui.ImageSource {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= readmePixelLimit/cfg.Height {
		pixels := int64(cfg.Width) * int64(cfg.Height)
		if m.pixels.Add(pixels) <= 3*readmePixelLimit {
			if img, err := ui.DecodeBitmap(data); err == nil {
				return img
			}
		}
		m.pixels.Add(-pixels)
	} else if err != nil && len(data) <= 256<<10 {
		if svg, err := ui.ParseSVG(data); err == nil {
			w, h := svg.Size()
			if w > 0 && h > 0 && w <= 4096 && h <= 4096 {
				return svg
			}
		}
	}
	return nil
}

var errReadmeMedia = errors.New("README media unavailable")

func (m *readmeMedia) read(src string, limit int64) ([]byte, error) {
	ref, err := url.Parse(strings.TrimSpace(src))
	if err != nil || ref.Path == "" || strings.Contains(ref.Path, "\\") {
		return nil, errReadmeMedia
	}
	if ref.Scheme == "" && ref.Host == "" {
		if m.doc.LocalRoot != "" {
			return readLocalMedia(m.doc.LocalRoot, ref.Path, limit)
		}
		if m.doc.LocalFS != nil {
			return readEmbeddedMedia(m.doc.LocalFS, m.doc.LocalFSSub, ref.Path, limit)
		}
		if m.doc.RemoteBase == nil {
			return nil, errReadmeMedia
		}
		base := m.doc.RemoteBase
		parts := strings.SplitN(base.EscapedPath(), "/", 5)
		if len(parts) < 5 {
			return nil, errReadmeMedia
		}
		repoRoot := strings.Join(parts[:4], "/") + "/"
		escapedRef := ref.EscapedPath()
		if strings.HasPrefix(escapedRef, "/") {
			joined := repoRoot + strings.TrimLeft(escapedRef, "/")
			if parsedRoot, err := url.Parse(joined); err == nil {
				ref.Path = parsedRoot.Path
				ref.RawPath = parsedRoot.RawPath
			}
		}
		ref = base.ResolveReference(ref)
		if !strings.HasPrefix(ref.EscapedPath(), repoRoot) {
			return nil, errReadmeMedia
		}
	}
	if !validReadmeMediaURL(ref) {
		return nil, errReadmeMedia
	}
	if strings.EqualFold(ref.Hostname(), "github.com") {
		parts := strings.SplitN(ref.EscapedPath(), "/", 6)
		if len(parts) == 6 && parts[3] == "blob" {
			rawJoined := "/" + parts[1] + "/" + parts[2] + "/" + parts[4] + "/" + parts[5]
			if parsedRaw, err := url.Parse("https://raw.githubusercontent.com" + rawJoined); err == nil {
				ref = parsedRaw
			}
		}
	}
	primaryCtx, primaryCancel := context.WithTimeout(m.ctx, 8*time.Second)
	data, err := fetchReadmeMediaURL(primaryCtx, ref.String(), limit)
	primaryCancel()
	if err == nil {
		return data, nil
	}
	if fallback := rawGitHubToJsDelivr(ref); fallback != "" {
		cdnCtx, cdnCancel := context.WithTimeout(m.ctx, 8*time.Second)
		defer cdnCancel()
		return fetchReadmeMediaURL(cdnCtx, fallback, limit)
	}
	return nil, err
}

func fetchReadmeMediaURL(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errReadmeMedia
	}
	resp, err := readmeMediaClient.Do(req)
	if err != nil {
		return nil, errReadmeMedia
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		return nil, errReadmeMedia
	}
	return readMediaBytes(resp.Body, limit)
}

func rawGitHubToJsDelivr(u *url.URL) string {
	if u == nil || !strings.EqualFold(u.Hostname(), "raw.githubusercontent.com") {
		return ""
	}
	escaped := strings.TrimPrefix(u.EscapedPath(), "/")
	parts := strings.SplitN(escaped, "/", 4)
	if len(parts) < 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return ""
	}
	return "https://cdn.jsdelivr.net/gh/" + parts[0] + "/" + parts[1] + "@" + parts[2] + "/" + parts[3]
}

func readEmbeddedMedia(fsys fs.FS, sub, name string, limit int64) ([]byte, error) {
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	if clean == "" || !fs.ValidPath(clean) {
		return nil, errReadmeMedia
	}
	full := clean
	if sub != "" {
		full = path.Join(sub, clean)
	}
	f, err := fsys.Open(full)
	if err != nil {
		return nil, errReadmeMedia
	}
	defer f.Close()
	return readMediaBytes(f, limit)
}

func readLocalMedia(dir, name string, limit int64) ([]byte, error) {
	name = filepath.FromSlash(path.Clean(name))
	if !filepath.IsLocal(name) {
		return nil, errReadmeMedia
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Stat(name)
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errReadmeMedia
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readMediaBytes(f, limit)
}

func readMediaBytes(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, errReadmeMedia
	}
	return data, nil
}

func validReadmeMediaURL(u *url.URL) bool {
	return u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443")
}

var privateMediaNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
}

var publicIPv6Network = netip.MustParsePrefix("2000::/3")

var readmeMediaClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(_, addr string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return err
				}
				ip, err := netip.ParseAddr(host)
				ip = ip.Unmap()
				if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !publicIPv6Network.Contains(ip)) {
					return errReadmeMedia
				}
				for _, subnet := range privateMediaNetworks {
					if subnet.Contains(ip) {
						return errReadmeMedia
					}
				}
				return nil
			},
		}).DialContext,
		DisableKeepAlives: true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validReadmeMediaURL(req.URL) {
			return errReadmeMedia
		}
		return nil
	},
}
