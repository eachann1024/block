package pluginui

import (
	"fmt"
	"sort"
	"strings"

	"block/internal/fuzzy"
	"block/internal/plugins"
)

// The App Store renders every frame, but its inputs (catalog, installed set,
// query, filters) change rarely. Source detection for installed plugins reads
// provenance files from disk, so the derived listing is cached in two layers:
// a base layer keyed by catalog + installed set, and a view layer keyed by the
// user's query, source filter and sort order.

type storeBaseItem struct {
	entry       plugins.StoreAppEntry
	installed   plugins.InstalledApp
	isInstalled bool
	source      string
	downloads   int
}

type storeBaseSnapshot struct {
	key       string
	items     []storeBaseItem
	updatable []plugins.StoreAppEntry
}

type storeViewSnapshot struct {
	key       string
	cards     []storeCard
	catCounts map[plugins.Category]int
}

func storeBaseKey(catalog *plugins.StoreIndex, installed []plugins.InstalledApp, deltaGen int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%p|%d|", catalog, deltaGen)
	for _, app := range installed {
		b.WriteString(app.Manifest.ID)
		b.WriteByte('@')
		b.WriteString(app.Manifest.Version)
		b.WriteByte('@')
		b.WriteString(app.InstallDir)
		if app.NeedsUpdate {
			b.WriteByte('!')
		}
		b.WriteByte(';')
	}
	return b.String()
}

func (c *Controller) storeBase() *storeBaseSnapshot {
	if c == nil || c.sup == nil {
		return &storeBaseSnapshot{}
	}
	catalog := c.sup.Catalog()
	installed := c.sup.Installed()
	store := c.sup.Store()
	deltaGen := 0
	if store != nil {
		deltaGen = store.DownloadDeltaGeneration()
	}
	key := storeBaseKey(catalog, installed, deltaGen)
	if c.storeBaseSnap != nil && c.storeBaseSnap.key == key {
		return c.storeBaseSnap
	}
	installedMap := make(map[string]plugins.InstalledApp, len(installed))
	for _, app := range installed {
		installedMap[app.Manifest.ID] = app
	}
	entries := buildStoreEntries(catalog, installed)
	snap := &storeBaseSnapshot{key: key, items: make([]storeBaseItem, 0, len(entries))}
	for _, entry := range entries {
		app, isInstalled := installedMap[entry.ID]
		downloads := max(0, entry.Downloads)
		if store != nil {
			downloads = store.EffectiveDownloads(entry)
		}
		snap.items = append(snap.items, storeBaseItem{
			entry:       entry,
			installed:   app,
			isInstalled: isInstalled,
			source:      storeAppSource(c, entry, app, isInstalled),
			downloads:   downloads,
		})
		if isInstalled && entry.ID != plugins.DiagnosticsPluginID && storeEntryHasUpdate(entry, app) {
			snap.updatable = append(snap.updatable, entry)
		}
	}
	c.storeBaseSnap = snap
	c.storeViewSnap = nil
	return snap
}

// storeView returns the filtered and ranked listing. Ranking rule (mirrored in
// docs/plugins/check-and-store.md): with a search query, best match first;
// otherwise, and as the tie-breaker, most catalog downloads first, then name,
// then id. Ranking uses the catalog count only, so installing a plugin locally
// never makes rows jump; the displayed number still includes local installs.
func (c *Controller) storeView(locale string) *storeViewSnapshot {
	base := c.storeBase()
	query := strings.TrimSpace(c.storeQuery)
	key := base.key + "\x00" + locale + "\x00" + query + "\x00" + c.storeSource + "\x00" + c.storeSort
	if c.storeViewSnap != nil && c.storeViewSnap.key == key {
		return c.storeViewSnap
	}
	type scored struct {
		card  storeCard
		score int
		name  string
	}
	list := make([]scored, 0, len(base.items))
	for _, it := range base.items {
		entry := it.entry
		if c.storeSource != "" && c.storeSource != it.source {
			continue
		}
		// Code highlighting packs stay out of the browsing list; they only
		// surface when searched for or already installed.
		if query == "" && !it.isInstalled && entry.EffectiveCategory() == plugins.CategoryLanguage {
			continue
		}
		score := 0
		if query != "" {
			primary := []string{
				entry.Name.Resolve(locale), entry.Name.Zh, entry.Name.En,
				entry.ID, entry.Author, strings.Join(entry.FileExtensions, " "),
			}
			secondary := []string{entry.Summary.Resolve(locale), entry.Summary.Zh, entry.Summary.En}
			sc, ok := fuzzy.Score(query, primary, secondary, false)
			if !ok {
				continue
			}
			score = sc
		}
		list = append(list, scored{
			card: storeCard{
				entry: entry, installed: it.installed, isInstalled: it.isInstalled,
				source: it.source, downloads: it.downloads,
			},
			score: score,
			name:  strings.ToLower(entry.Name.Resolve(locale)),
		})
	}
	byName := c.storeSort == storeSortName
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if query != "" && a.score != b.score {
			return a.score > b.score
		}
		if da, db := max(0, a.card.entry.Downloads), max(0, b.card.entry.Downloads); !byName && da != db {
			return da > db
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.card.entry.ID < b.card.entry.ID
	})
	snap := &storeViewSnapshot{key: key, cards: make([]storeCard, len(list)), catCounts: map[plugins.Category]int{}}
	for i, s := range list {
		snap.cards[i] = s.card
		snap.catCounts[s.card.entry.EffectiveCategory()]++
	}
	c.storeViewSnap = snap
	return snap
}

func updatableStoreEntries(ctrl *Controller) []plugins.StoreAppEntry {
	if ctrl == nil || ctrl.sup == nil {
		return nil
	}
	return ctrl.storeBase().updatable
}
