// Package bundled holds the plugin files that ship with Block.
package bundled

import "embed"

//go:embed tag-manager/manifest.json tag-manager/README*.md tag-manager/media/tags-panel.gif tag-manager/*.py tag-manager/vendor/PyYAML-LICENSE tag-manager/vendor/yaml/*.py
var Files embed.FS
