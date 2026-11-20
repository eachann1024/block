package plugins

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed all:builtin_langs
var builtinLangsFS embed.FS

func init() {
	entries, err := fs.ReadDir(builtinLangsFS, "builtin_langs")
	if err != nil {
		return
	}
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		sub := path.Join("builtin_langs", de.Name())
		raw, err := fs.ReadFile(builtinLangsFS, path.Join(sub, "manifest.json"))
		if err != nil {
			continue
		}
		m, err := DecodeManifestStrict(raw)
		if err != nil {
			continue
		}
		perms := m.Permissions
		if perms == nil {
			perms = []Permission{}
		}
		surfs := m.Surfaces
		if surfs == nil {
			surfs = []Surface{}
		}
		entry := StoreAppEntry{
			App:            m.App,
			ID:             m.ID,
			Category:       m.EffectiveCategory(),
			Name:           m.Name,
			Version:        m.Version,
			Author:         m.Author,
			Repo:           m.Repo,
			Summary:        m.Summary,
			DownloadURL:    "builtin://" + m.ID,
			Verified:       IsOfficialRepo(m.Repo),
			Beta:              m.Beta,
			Permissions:       perms,
			PermissionReasons: ClonePermissionReasons(m.PermissionReasons),
			Surfaces:          surfs,
			FileExtensions: m.DisplayFileExtensions(),
			Icon:           m.Icon,
		}
		RegisterBuiltinCatalogApp(entry, builtinLangsFS, sub)
	}
}
