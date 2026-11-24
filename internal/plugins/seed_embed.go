package plugins

import (
	_ "embed"

	"block/plugins"
)

//go:embed default_index.json
var embeddedDefaultIndex []byte

func init() {
	if idx, err := ParseStoreIndex(embeddedDefaultIndex); err == nil && idx != nil {
		setEmbeddedFallbackIndex(idx)
	}
	const dir = "tag-manager"
	raw, err := bundled.Files.ReadFile(dir + "/manifest.json")
	if err != nil {
		panic(err)
	}
	m, err := DecodeManifestStrict(raw)
	if err != nil {
		panic(err)
	}
	RegisterBuiltinCatalogApp(StoreAppEntry{
		App: m.App, ID: m.ID, Category: m.EffectiveCategory(), Name: m.Name,
		Version: m.Version, Author: m.Author, Summary: m.Summary, Icon: m.Icon,
		Permissions: m.Permissions, PermissionReasons: m.PermissionReasons,
		Surfaces: m.Surfaces, DownloadURL: "builtin://" + m.ID,
	}, bundled.Files, dir)
}
