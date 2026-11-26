package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBundledTagManagerOffline(t *testing.T) {
	store := NewStoreManager(t.TempDir())
	index := store.LoadCachedOrSeedCatalog()
	const id = "block-tag-manager"
	i := slices.IndexFunc(index.Apps, func(a StoreAppEntry) bool { return a.ID == id })
	if i < 0 || index.Apps[i].DownloadURL != "builtin://"+id {
		t.Fatal("missing bundled tag manager")
	}
	app, err := store.InstallEntry(context.Background(), index.Apps[i])
	if err != nil {
		t.Fatal(err)
	}
	if app.Enabled || app.Manifest.ID != id {
		t.Fatal("expected a newly installed, disabled tag manager")
	}
	for _, file := range []string{"main.py", "frontmatter.py", "vendor/yaml/__init__.py", "vendor/PyYAML-LICENSE"} {
		if _, err := os.Stat(filepath.Join(app.InstallDir, file)); err != nil {
			t.Fatal(err)
		}
	}

	// A fresher cached catalog may have removed older remote entries. Add only
	// the actual bundled package to it, preserving the remote refresh timestamp.
	cached := StoreIndex{App: TargetApp, SchemaVersion: CurrentSchemaVersion, Topic: PluginTopic,
		UpdatedAt: "2099-01-01T00:00:00Z", Apps: []StoreAppEntry{{ID: "remote-only"}}}
	raw, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.cacheIndexPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	merged := store.LoadCachedOrSeedCatalog()
	if merged.UpdatedAt != cached.UpdatedAt || len(merged.Apps) != 2 ||
		!slices.ContainsFunc(merged.Apps, func(a StoreAppEntry) bool { return a.ID == id }) ||
		!slices.ContainsFunc(merged.Apps, func(a StoreAppEntry) bool { return a.ID == "remote-only" }) {
		t.Fatal("cached catalog lost its timestamp or entries, or resurrected a delisted app")
	}
}
