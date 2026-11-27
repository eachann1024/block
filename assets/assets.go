package assets

import (
	"embed"
	"path"
)

//go:embed app-icon/apple-touch-icon.png app-icon/cat-mark.png app-icon/cat-mark-dark.png fonts/*.ttf icons/*.svg mascot/*.svg agents/*.svg skills/*.md media/*.mp4
var FS embed.FS

// Read returns the embedded bytes for a relative asset path such as
// "icons/note.fill.svg", "mascot/no-note.1.svg", "agents/codex.svg",
// or "skills/AGENTS.md".
func Read(rel string) ([]byte, error) {
	return FS.ReadFile(path.Clean(rel))
}

// ReadSkill returns the embedded Markdown text for a built-in AI skill.
func ReadSkill(name string) string {
	b, err := FS.ReadFile("skills/" + name)
	if err != nil {
		return ""
	}
	return string(b)
}
