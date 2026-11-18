package plugins

import (
	"os"
	"path/filepath"
	"strings"
)

// RepositoryReadmeURL links to the rendered README on GitHub's default branch.
func RepositoryReadmeURL(repo string) string {
	repo = strings.TrimSpace(repo)
	owner, name, _ := strings.Cut(repo, "/")
	if !repoSlugRe.MatchString(repo) || owner == "." || owner == ".." || name == "." || name == ".." {
		return ""
	}
	return "https://github.com/" + repo + "#readme"
}

// ReadmePath returns a regular documentation file at the installed plugin root.
func (app InstalledApp) ReadmePath() string {
	if app.InstallDir == "" {
		return ""
	}
	files, err := os.ReadDir(app.InstallDir)
	if err != nil {
		return ""
	}
	for _, name := range []string{"readme.md", "readme.markdown", "readme.txt"} {
		for _, file := range files {
			if strings.EqualFold(file.Name(), name) && file.Type().IsRegular() {
				return filepath.Join(app.InstallDir, file.Name())
			}
		}
	}
	return ""
}
