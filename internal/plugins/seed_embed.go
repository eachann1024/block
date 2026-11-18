package plugins

import (
	"embed"
	"io/fs"
)

//go:embed seed/block-sync/*
var embeddedSeedFS embed.FS

func init() {
	raw, err := fs.ReadFile(embeddedSeedFS, "seed/block-sync/manifest.json")
	if err != nil {
		return
	}
	m, err := DecodeManifestStrict(raw)
	if err != nil {
		return
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
		Verified:    true,
		Permissions: m.Permissions,
		Surfaces:    m.Surfaces,
		Icon:        m.Icon,
	}, embeddedSeedFS, "seed/block-sync")
}
