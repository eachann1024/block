package plugins

import _ "embed"

//go:embed default_index.json
var embeddedDefaultIndex []byte

func init() {
	if idx, err := ParseStoreIndex(embeddedDefaultIndex); err == nil && idx != nil {
		setEmbeddedFallbackIndex(idx)
	}
}
