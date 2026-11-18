package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxReadmeBytes = 512 << 10

var ErrReadmeUnavailable = errors.New("plugin README unavailable")

type PluginReadme struct {
	Text       string
	LocalRoot  string
	RemoteBase *url.URL
}

// ReadPluginReadme prefers the installed copy, which also works offline.
func (s *StoreManager) ReadPluginReadme(ctx context.Context, entry StoreAppEntry, installed InstalledApp) (PluginReadme, error) {
	var localErr error
	if path := installed.readmePath(); path != "" {
		var doc string
		doc, localErr = readLocalReadme(path)
		if localErr == nil {
			return PluginReadme{Text: doc, LocalRoot: installed.InstallDir}, nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/readme", nil)
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

func readmeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	owner, name, _ := strings.Cut(repo, "/")
	if !repoSlugRe.MatchString(repo) || owner == "." || owner == ".." || name == "." || name == ".." {
		return ""
	}
	return repo
}

func (app InstalledApp) readmePath() string {
	if app.InstallDir == "" {
		return ""
	}
	files, err := os.ReadDir(app.InstallDir)
	if err != nil {
		return ""
	}
	for _, name := range []string{"readme.md", "readme.markdown", "readme.txt", "readme"} {
		for _, file := range files {
			if strings.EqualFold(file.Name(), name) && file.Type().IsRegular() {
				return filepath.Join(app.InstallDir, file.Name())
			}
		}
	}
	return ""
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
