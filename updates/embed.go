package updates

import _ "embed"

// BundledFeed holds the compile-time snapshot of updates/feed.json for instant offline notes.
//
//go:embed feed.json
var BundledFeed []byte
