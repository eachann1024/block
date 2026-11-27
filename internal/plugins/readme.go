package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxReadmeBytes     = 512 << 10
	readmeStageTimeout = 5 * time.Second
)

var ErrReadmeUnavailable = errors.New("plugin README unavailable")

type PluginReadme struct {
	Text       string
	LocalRoot  string
	LocalFS    fs.FS
	LocalFSSub string
	RemoteBase *url.URL
}

// ReadPluginReadme prefers the installed copy, which also works offline.
// Scheme B (Chinese-first): zh uses README.md (or README.zh.md), en prefers
// README.en.md and falls back to README.md when unavailable.
func (s *StoreManager) ReadPluginReadme(ctx context.Context, entry StoreAppEntry, installed InstalledApp, locale string) (PluginReadme, error) {
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en")
	var localErr error
	for _, p := range installed.readmePaths(en) {
		doc, err := readLocalReadme(p)
		if err == nil {
			return PluginReadme{Text: doc, LocalRoot: installed.InstallDir}, nil
		}
		localErr = err
	}
	if strings.HasPrefix(strings.TrimSpace(entry.DownloadURL), "builtin://") {
		if doc, ok := readBuiltinSeedReadme(entry.ID, en); ok {
			return doc, nil
		}
	}
	repo := readmeRepo(entry.Repo)
	if repo == "" {
		repo = readmeRepo(installed.Manifest.Repo)
	}
	if repo == "" {
		if localErr != nil {
			return PluginReadme{}, localErr
		}
		return PluginReadme{}, ErrReadmeUnavailable
	}

	ghCtx, ghCancel := withStageTimeout(ctx, readmeStageTimeout)
	var remoteErr error
	if en {
		doc, err := s.fetchGitHubReadme(ghCtx, repo, "https://api.github.com/repos/"+repo+"/contents/README.en.md")
		if err == nil {
			ghCancel()
			return doc, nil
		}
		if !errors.Is(err, ErrReadmeUnavailable) {
			remoteErr = err
		}
	}
	if remoteErr == nil {
		doc, err := s.fetchGitHubReadme(ghCtx, repo, "https://api.github.com/repos/"+repo+"/readme")
		if err == nil {
			ghCancel()
			return doc, nil
		}
		remoteErr = err
	}
	ghCancel()

	cdnCtx, cdnCancel := withStageTimeout(ctx, readmeStageTimeout)
	defer cdnCancel()
	if doc, cdnErr := s.fetchCDNReadme(cdnCtx, repo, en); cdnErr == nil {
		return doc, nil
	}
	if doc, ok := readBuiltinSeedReadme(entry.ID, en); ok {
		return doc, nil
	}
	return PluginReadme{}, remoteErr
}

func withStageTimeout(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), limit)
	}
	if dl, ok := parent.Deadline(); ok {
		rem := time.Until(dl)
		if rem > 0 && rem < limit {
			limit = rem
		}
	}
	return context.WithTimeout(parent, limit)
}

func (s *StoreManager) fetchCDNReadme(ctx context.Context, repo string, en bool) (PluginReadme, error) {
	files := []string{"README.md"}
	if en {
		files = []string{"README.en.md", "README.md"}
	}
	base := &url.URL{Scheme: "https", Host: "cdn.jsdelivr.net", Path: "/gh/" + repo + "@HEAD/"}
	var lastErr error
	for _, name := range files {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+name, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := s.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			doc, readErr := readReadmeText(resp.Body)
			resp.Body.Close()
			if readErr == nil {
				return PluginReadme{Text: doc, RemoteBase: base}, nil
			}
			lastErr = readErr
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			lastErr = ErrReadmeUnavailable
			continue
		}
		lastErr = fmt.Errorf("fetch plugin README via CDN: HTTP %d", resp.StatusCode)
	}
	if lastErr == nil {
		return PluginReadme{}, ErrReadmeUnavailable
	}
	return PluginReadme{}, lastErr
}

func (s *StoreManager) fetchGitHubReadme(ctx context.Context, repo, endpoint string) (PluginReadme, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PluginReadme{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return PluginReadme{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var src struct {
			Path     string `json:"path"`
			Size     int    `json:"size"`
			Content  []byte `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2*maxReadmeBytes)).Decode(&src); err != nil {
			return PluginReadme{}, err
		}
		if src.Encoding != "base64" || src.Size > maxReadmeBytes {
			return PluginReadme{}, errors.New("unsupported README size or encoding")
		}
		doc, err := readReadmeText(strings.NewReader(string(src.Content)))
		if err != nil {
			return PluginReadme{}, err
		}
		base := &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: path.Join("/", repo, "HEAD") + "/"}
		if dir := path.Dir(path.Clean("/" + src.Path)); dir != "/" {
			base.Path += strings.TrimPrefix(dir, "/") + "/"
		}
		return PluginReadme{Text: doc, RemoteBase: base}, nil
	case http.StatusNotFound:
		return PluginReadme{}, ErrReadmeUnavailable
	default:
		return PluginReadme{}, fmt.Errorf("fetch plugin README: HTTP %d", resp.StatusCode)
	}
}

// ArtworkBaseURL returns the directory used to fetch optional App Store SVGs.
func (e StoreAppEntry) ArtworkBaseURL() *url.URL {
	repo := readmeRepo(e.Repo)
	if repo == "" {
		return nil
	}
	return &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: "/" + repo + "/HEAD/store/"}
}

func readmeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	owner, name, _ := strings.Cut(repo, "/")
	if !repoSlugRe.MatchString(repo) || owner == "." || owner == ".." || name == "." || name == ".." {
		return ""
	}
	return repo
}

func (app InstalledApp) readmePaths(en bool) []string {
	if app.InstallDir == "" {
		return nil
	}
	files, err := os.ReadDir(app.InstallDir)
	if err != nil {
		return nil
	}
	candidates := []string{"readme.zh.md", "readme.md", "readme.markdown", "readme.txt", "readme"}
	if en {
		candidates = []string{"readme.en.md", "readme.en.markdown", "readme.en.txt", "readme.md", "readme.markdown", "readme.txt", "readme"}
	}
	var out []string
	for _, name := range candidates {
		for _, file := range files {
			if strings.EqualFold(file.Name(), name) && file.Type().IsRegular() {
				out = append(out, filepath.Join(app.InstallDir, file.Name()))
			}
		}
	}
	return out
}

func readLocalReadme(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return readReadmeText(file)
}

func readReadmeText(src io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(src, maxReadmeBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxReadmeBytes || !utf8.Valid(raw) {
		return "", errors.New("unsupported README size or encoding")
	}
	doc := strings.TrimSpace(strings.TrimPrefix(string(raw), "\uFEFF"))
	if doc == "" {
		return "", ErrReadmeUnavailable
	}
	return doc, nil
}
