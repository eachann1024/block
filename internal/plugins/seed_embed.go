package plugins

import (
	"embed"
	"io/fs"
)

//go:embed seed/*/*
var embeddedSeedFS embed.FS

var seedBaselineDownloads = map[string]int{
	"block-sync":        1280,
	"block-image-host":  940,
	"block-publisher":   760,
	"block-migrator":    615,
	"block-diagnostics": 320,
}

func init() {
	entries, err := fs.ReadDir(embeddedSeedFS, "seed")
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := "seed/" + e.Name()
		raw, err := fs.ReadFile(embeddedSeedFS, subDir+"/manifest.json")
		if err != nil {
			continue
		}
		m, err := DecodeManifestStrict(raw)
		if err != nil {
			continue
		}
		RegisterBuiltinCatalogApp(StoreAppEntry{
			App:         m.App,
			ID:          m.ID,
			Name:        m.Name,
			Version:     m.Version,
			Author:      m.Author,
			Repo:        m.Repo,
			Summary:     m.Summary,
			DownloadURL: "builtin://" + m.ID,
			Downloads:   seedBaselineDownloads[m.ID],
			Verified:    true,
			Permissions: m.Permissions,
			Surfaces:    m.Surfaces,
			Icon:        m.Icon,
		}, embeddedSeedFS, subDir)
	}
}
