// Package store provides the App Store's curated display data and blocklist.
package store

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
)

//go:embed featured.json
var FeaturedJSON []byte

//go:embed blocklist.json
var blocklistJSON []byte

var blocklist = func() []string {
	var entries []string
	_ = json.Unmarshal(blocklistJSON, &entries)
	for i, entry := range entries {
		entries[i] = strings.ToLower(strings.TrimSpace(entry))
	}
	return entries
}()

// Blocklisted reports whether a plugin's ID or repository is hidden in the store.
func Blocklisted(id, repo string) bool {
	return slices.Contains(blocklist, strings.ToLower(strings.TrimSpace(id))) ||
		slices.Contains(blocklist, strings.ToLower(strings.TrimSpace(repo)))
}
