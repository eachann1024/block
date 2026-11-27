package assets

import (
	"embed"
	"path"
)

//go:embed store/*.svg store/featured.json
var storeFS embed.FS

// ReadStore returns the embedded bytes for an App Store asset inside assets/store/.
func ReadStore(name string) ([]byte, error) {
	return storeFS.ReadFile(path.Join("store", path.Clean(name)))
}
