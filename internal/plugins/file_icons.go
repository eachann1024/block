package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	CurrentFileIconPackSchemaVersion = 1
	// NotebookFileIconRelPath is the reserved relative path key storing a
	// notebook's own custom icon inside that notebook's file-icon map.
	NotebookFileIconRelPath = "."
	maxFileIconPackBytes    = 512 * 1024
	maxFileIconCategories   = 16
	maxFileIconDefs         = 256
	maxFileIconsPerScope    = 2048
	maxFileIconRecents      = 6
)

var fileIconIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// FileIconPackContrib declares a custom icon pack and optional interactive
// sidebar file/folder icon picker contributed by a ui:sidebar plugin.
type FileIconPackContrib struct {
	ID       string        `json:"id"`
	Title    LocalizedText `json:"title,omitempty"`
	Pack     string        `json:"pack"`
	Picker   bool          `json:"picker,omitempty"`
	SyncTabs bool          `json:"sync_tabs,omitempty"`
}

// FileIconPack is the JSON schema of a plugin's custom icon pack file.
type FileIconPack struct {
	SchemaVersion int                `json:"schema_version"`
	Categories    []FileIconCategory `json:"categories"`
	Icons         []FileIconDef      `json:"icons"`
}

// FileIconCategory groups icons in the picker category rail and scrollable grid.
type FileIconCategory struct {
	ID   string        `json:"id"`
	Name LocalizedText `json:"name"`
	Icon string        `json:"icon"`
}

// FileIconDef describes one selectable icon in a plugin's icon pack.
// Exactly one of Ref (built-in host icon name) or Spec (inline DuoIconSpec) must be set.
type FileIconDef struct {
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Name     LocalizedText `json:"name"`
	Keywords []string      `json:"keywords,omitempty"`
	Ref      string        `json:"ref,omitempty"`
	Spec     *DuoIconSpec  `json:"spec,omitempty"`
}

// FileIconEntry stores one notebook-relative file or folder icon assignment.
type FileIconEntry struct {
	Icon string `json:"icon"`
	Tint string `json:"tint,omitempty"`
}

// FileIconPrefs stores optional plugin-controlled UI preferences for its icon pack.
type FileIconPrefs struct {
	Picker   *bool `json:"picker,omitempty"`
	SyncTabs *bool `json:"sync_tabs,omitempty"`
}

// ResolvedFileIcon is the host-ready custom icon resolved for one file or folder.
type ResolvedFileIcon struct {
	PluginID string
	PackID   string
	IconID   string
	Tint     string
	Ref      string
	Spec     DuoIconSpec
}

// ResolvedFileIconPack is the active picker icon pack and runtime metadata ready for UI rendering.
type ResolvedFileIconPack struct {
	PluginID string
	PackID   string
	Title    string
	Picker   bool
	SyncTabs bool
	Recent   []string
	Pack     *FileIconPack
	ByID     map[string]FileIconDef
}

type cachedFileIconPack struct {
	modTime time.Time
	size    int64
	pack    *FileIconPack
	byID    map[string]FileIconDef
}

var (
	fileIconPackMu    sync.RWMutex
	fileIconPackCache = map[string]cachedFileIconPack{}
)

func normalizeFileIconTint(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "blue", "teal", "green", "amber", "red", "purple":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "none"
	}
}

func normalizeFileIconRelPath(raw string) (string, bool) {
	if raw == NotebookFileIconRelPath {
		return NotebookFileIconRelPath, true
	}
	return normalizeBadgeRelPath(raw)
}

func validateFileIconPackPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("contributes.file_icons.pack is required")
	}
	if strings.ContainsAny(p, "\\:") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("contributes.file_icons.pack %q must be a relative slash path", raw)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("contributes.file_icons.pack %q escapes plugin directory", raw)
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".json") {
		return "", fmt.Errorf("contributes.file_icons.pack %q must end with .json", raw)
	}
	return clean, nil
}

// DecodeFileIconPackStrict decodes and validates an icon pack JSON document.
func DecodeFileIconPackStrict(raw []byte) (*FileIconPack, error) {
	if len(raw) == 0 {
		return nil, errors.New("icon pack file is empty")
	}
	if len(raw) > maxFileIconPackBytes {
		return nil, fmt.Errorf("icon pack exceeds %d bytes", maxFileIconPackBytes)
	}
	var pack FileIconPack
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pack); err != nil {
		return nil, fmt.Errorf("invalid icon pack JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing data after icon pack JSON object")
	}
	if err := validateFileIconPack(&pack); err != nil {
		return nil, err
	}
	return &pack, nil
}

func validateFileIconPack(p *FileIconPack) error {
	if p == nil {
		return errors.New("icon pack is nil")
	}
	if p.SchemaVersion != CurrentFileIconPackSchemaVersion {
		return fmt.Errorf("icon pack schema_version must be %d", CurrentFileIconPackSchemaVersion)
	}
	if len(p.Categories) == 0 || len(p.Categories) > maxFileIconCategories {
		return fmt.Errorf("icon pack must define 1..%d categories", maxFileIconCategories)
	}
	if len(p.Icons) == 0 || len(p.Icons) > maxFileIconDefs {
		return fmt.Errorf("icon pack must define 1..%d icons", maxFileIconDefs)
	}
	catIDs := make(map[string]bool, len(p.Categories))
	for i := range p.Categories {
		cat := &p.Categories[i]
		cat.ID = strings.TrimSpace(cat.ID)
		if !fileIconIDRe.MatchString(cat.ID) {
			return fmt.Errorf("categories[%d].id %q is invalid", i, cat.ID)
		}
		if catIDs[cat.ID] {
			return fmt.Errorf("categories[%d].id %q is duplicated", i, cat.ID)
		}
		catIDs[cat.ID] = true
		if err := validateContribText(fmt.Sprintf("categories[%d].name", i), cat.Name); err != nil {
			return err
		}
		cat.Icon = strings.TrimSpace(cat.Icon)
		if !fileIconIDRe.MatchString(cat.Icon) {
			return fmt.Errorf("categories[%d].icon %q is invalid", i, cat.Icon)
		}
	}
	iconIDs := make(map[string]bool, len(p.Icons))
	for i := range p.Icons {
		ic := &p.Icons[i]
		ic.ID = strings.TrimSpace(ic.ID)
		if !fileIconIDRe.MatchString(ic.ID) {
			return fmt.Errorf("icons[%d].id %q is invalid", i, ic.ID)
		}
		if iconIDs[ic.ID] {
			return fmt.Errorf("icons[%d].id %q is duplicated", i, ic.ID)
		}
		iconIDs[ic.ID] = true
		ic.Category = strings.TrimSpace(ic.Category)
		if !catIDs[ic.Category] {
			return fmt.Errorf("icons[%d].category %q references unknown category", i, ic.Category)
		}
		if err := validateContribText(fmt.Sprintf("icons[%d].name", i), ic.Name); err != nil {
			return err
		}
		if len(ic.Keywords) > 24 {
			return fmt.Errorf("icons[%d].keywords allows at most 24 entries", i)
		}
		for j, kw := range ic.Keywords {
			kw = strings.TrimSpace(kw)
			if kw == "" || utf8.RuneCountInString(kw) > 40 {
				return fmt.Errorf("icons[%d].keywords[%d] must be 1..40 characters", i, j)
			}
			if ContainsEmoji(kw) {
				return fmt.Errorf("icons[%d].keywords[%d] must not contain emoji", i, j)
			}
			ic.Keywords[j] = kw
		}
		ic.Ref = strings.TrimSpace(ic.Ref)
		hasRef := ic.Ref != ""
		hasSpec := ic.Spec != nil
		if hasRef == hasSpec {
			return fmt.Errorf("icons[%d] (%s) must define exactly one of ref or spec", i, ic.ID)
		}
		if hasRef {
			if !fileIconIDRe.MatchString(ic.Ref) {
				return fmt.Errorf("icons[%d].ref %q is invalid", i, ic.Ref)
			}
		} else {
			if err := validateContribIcon(fmt.Sprintf("icons[%d].spec", i), *ic.Spec); err != nil {
				return err
			}
		}
	}
	for i, cat := range p.Categories {
		if !iconIDs[cat.Icon] {
			return fmt.Errorf("categories[%d].icon %q references unknown icon id", i, cat.Icon)
		}
	}
	return nil
}

func loadFileIconPack(root *os.Root, relPath string) (*FileIconPack, error) {
	clean, err := validateFileIconPackPath(relPath)
	if err != nil {
		return nil, err
	}
	raw, err := readPluginFile(root, clean, maxFileIconPackBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", clean, err)
	}
	pack, err := DecodeFileIconPackStrict(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", clean, err)
	}
	return pack, nil
}

func loadCachedFileIconPack(pluginDir, relPath string) (*FileIconPack, map[string]FileIconDef, error) {
	clean, err := validateFileIconPackPath(relPath)
	if err != nil {
		return nil, nil, err
	}
	full := filepath.Join(pluginDir, filepath.FromSlash(clean))
	info, err := os.Lstat(full)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s must be a regular file", clean)
	}
	cacheKey := filepath.Clean(pluginDir) + "|" + clean
	fileIconPackMu.RLock()
	if c, ok := fileIconPackCache[cacheKey]; ok && c.size == info.Size() && c.modTime.Equal(info.ModTime()) && c.pack != nil {
		fileIconPackMu.RUnlock()
		return c.pack, c.byID, nil
	}
	fileIconPackMu.RUnlock()

	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	pack, err := loadFileIconPack(root, clean)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]FileIconDef, len(pack.Icons))
	for _, ic := range pack.Icons {
		byID[ic.ID] = ic
	}
	fileIconPackMu.Lock()
	if len(fileIconPackCache) > 32 {
		clear(fileIconPackCache)
	}
	fileIconPackCache[cacheKey] = cachedFileIconPack{
		modTime: info.ModTime(),
		size:    info.Size(),
		pack:    pack,
		byID:    byID,
	}
	fileIconPackMu.Unlock()
	return pack, byID, nil
}

// ValidatePluginFileIcons validates every icon pack declared in m.Contributes.FileIcons.
func ValidatePluginFileIcons(pluginDir string, m *Manifest) error {
	if m == nil || m.Contributes == nil || len(m.Contributes.FileIcons) == 0 {
		return nil
	}
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, fi := range m.Contributes.FileIcons {
		if _, err := loadFileIconPack(root, fi.Pack); err != nil {
			return fmt.Errorf("file_icons %q: %w", fi.ID, err)
		}
	}
	return nil
}

// ValidatePluginFileIconsBilingual enforces pure English category and icon labels in icon packs.
func ValidatePluginFileIconsBilingual(pluginDir string, m *Manifest) error {
	if m == nil || m.Contributes == nil || len(m.Contributes.FileIcons) == 0 {
		return nil
	}
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, fi := range m.Contributes.FileIcons {
		pack, err := loadFileIconPack(root, fi.Pack)
		if err != nil {
			return fmt.Errorf("file_icons %q: %w", fi.ID, err)
		}
		for i, cat := range pack.Categories {
			if err := validateEnglishOnlyText(fmt.Sprintf("%s.categories[%d].name.en", fi.Pack, i), cat.Name.En); err != nil {
				return err
			}
		}
		for i, ic := range pack.Icons {
			if err := validateEnglishOnlyText(fmt.Sprintf("%s.icons[%d].name.en", fi.Pack, i), ic.Name.En); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEnglishOnlyText(field, v string) error {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return fmt.Errorf("%s must provide English text", field)
	}
	if ContainsEmoji(trimmed) {
		return fmt.Errorf("%s must not contain emoji", field)
	}
	if bad, ok := FindChineseRune(trimmed); ok {
		return fmt.Errorf("%s must be English-only, found Chinese character %q", field, bad)
	}
	return nil
}

// ActiveFileIconPicker returns the highest-priority enabled ui:sidebar plugin's
// resolved file icon pack and UI preferences, or nil if none is active.
func (s *Supervisor) ActiveFileIconPicker() *ResolvedFileIconPack {
	if s == nil || s.contrib == nil {
		return nil
	}
	loc := s.Locale()
	contribs := s.enabledContribs()
	for _, a := range contribs {
		if !a.Manifest.HasPermission(PermUISidebar) || len(a.Manifest.Contributes.FileIcons) == 0 {
			continue
		}
		spec := a.Manifest.Contributes.FileIcons[0]
		pack, byID, err := loadCachedFileIconPack(a.InstallDir, spec.Pack)
		if err != nil || pack == nil {
			continue
		}
		picker := spec.Picker
		syncTabs := spec.SyncTabs
		var recent []string
		s.contrib.mu.RLock()
		if pref, ok := s.contrib.fileIconPrefs[a.Manifest.ID]; ok {
			if pref.Picker != nil {
				picker = *pref.Picker
			}
			if pref.SyncTabs != nil {
				syncTabs = *pref.SyncTabs
			}
		}
		if rec := s.contrib.fileIconRecents[a.Manifest.ID]; len(rec) > 0 {
			recent = make([]string, 0, len(rec))
			for _, id := range rec {
				if _, ok := byID[id]; ok {
					recent = append(recent, id)
				}
			}
		}
		s.contrib.mu.RUnlock()
		title := spec.Title.Resolve(loc)
		if title == "" {
			title = a.Manifest.Name.Resolve(loc)
		}
		return &ResolvedFileIconPack{
			PluginID: a.Manifest.ID,
			PackID:   spec.ID,
			Title:    title,
			Picker:   picker,
			SyncTabs: syncTabs,
			Recent:   recent,
			Pack:     pack,
			ByID:     byID,
		}
	}
	return nil
}

// FileIconsForNotebook returns a snapshot map of notebook-relative slash paths
// (notes and folders) to resolved custom icons from enabled ui:sidebar plugins.
func (s *Supervisor) FileIconsForNotebook(notebookRoot string) map[string]ResolvedFileIcon {
	if s == nil || s.contrib == nil {
		return nil
	}
	nbKey := cleanNotebookKey(notebookRoot)
	if nbKey == "" {
		return nil
	}
	contribs := s.enabledContribs()
	var out map[string]ResolvedFileIcon
	for _, a := range contribs {
		if !a.Manifest.HasPermission(PermUISidebar) || len(a.Manifest.Contributes.FileIcons) == 0 {
			continue
		}
		spec := a.Manifest.Contributes.FileIcons[0]
		s.contrib.mu.RLock()
		entries := s.contrib.fileIcons[a.Manifest.ID][nbKey]
		var snap map[string]FileIconEntry
		if len(entries) > 0 {
			snap = make(map[string]FileIconEntry, len(entries))
			for rel, ent := range entries {
				snap[rel] = ent
			}
		}
		s.contrib.mu.RUnlock()
		if len(snap) == 0 {
			continue
		}
		_, byID, err := loadCachedFileIconPack(a.InstallDir, spec.Pack)
		if err != nil || len(byID) == 0 {
			continue
		}
		for rel, ent := range snap {
			if out != nil {
				if _, exists := out[rel]; exists {
					continue
				}
			}
			def, ok := byID[ent.Icon]
			if !ok {
				continue
			}
			if out == nil {
				out = make(map[string]ResolvedFileIcon, len(snap))
			}
			resolved := ResolvedFileIcon{
				PluginID: a.Manifest.ID,
				PackID:   spec.ID,
				IconID:   def.ID,
				Tint:     normalizeFileIconTint(ent.Tint),
				Ref:      def.Ref,
			}
			if def.Spec != nil {
				resolved.Spec = *def.Spec
			}
			out[rel] = resolved
		}
	}
	return out
}

// NotebookIcon returns the resolved custom icon assigned to notebookRoot itself
// (stored under NotebookFileIconRelPath), or false if none is active.
func (s *Supervisor) NotebookIcon(notebookRoot string) (ResolvedFileIcon, bool) {
	if s == nil || s.contrib == nil || notebookRoot == "" {
		return ResolvedFileIcon{}, false
	}
	m := s.NotebookIcons([]string{notebookRoot})
	ic, ok := m[notebookRoot]
	return ic, ok
}

// NotebookIcons returns a map from each notebook path in notebooks to its
// resolved custom notebook icon (stored under NotebookFileIconRelPath).
func (s *Supervisor) NotebookIcons(notebooks []string) map[string]ResolvedFileIcon {
	if s == nil || s.contrib == nil || len(notebooks) == 0 {
		return nil
	}
	contribs := s.enabledContribs()
	var out map[string]ResolvedFileIcon
	for _, a := range contribs {
		if !a.Manifest.HasPermission(PermUISidebar) || len(a.Manifest.Contributes.FileIcons) == 0 {
			continue
		}
		spec := a.Manifest.Contributes.FileIcons[0]
		s.contrib.mu.RLock()
		byRoot := s.contrib.fileIcons[a.Manifest.ID]
		var matched map[string]FileIconEntry
		if len(byRoot) > 0 {
			for _, nb := range notebooks {
				if out != nil {
					if _, exists := out[nb]; exists {
						continue
					}
				}
				nbKey := cleanNotebookKey(nb)
				if nbKey == "" {
					continue
				}
				if ent, ok := byRoot[nbKey][NotebookFileIconRelPath]; ok && ent.Icon != "" {
					if matched == nil {
						matched = make(map[string]FileIconEntry, len(notebooks))
					}
					matched[nb] = ent
				}
			}
		}
		s.contrib.mu.RUnlock()
		if len(matched) == 0 {
			continue
		}
		_, byID, err := loadCachedFileIconPack(a.InstallDir, spec.Pack)
		if err != nil || len(byID) == 0 {
			continue
		}
		for nb, ent := range matched {
			def, ok := byID[ent.Icon]
			if !ok {
				continue
			}
			if out == nil {
				out = make(map[string]ResolvedFileIcon, len(matched))
			}
			resolved := ResolvedFileIcon{
				PluginID: a.Manifest.ID,
				PackID:   spec.ID,
				IconID:   def.ID,
				Tint:     normalizeFileIconTint(ent.Tint),
				Ref:      def.Ref,
			}
			if def.Spec != nil {
				resolved.Spec = *def.Spec
			}
			out[nb] = resolved
		}
	}
	return out
}

// SetFileIconDirect applies or clears one custom file/folder/notebook icon
// directly from the native icon picker UI without cold-starting the plugin.
func (s *Supervisor) SetFileIconDirect(pluginID, notebookRoot, relPath, iconID, tint string) error {
	if s == nil || s.contrib == nil {
		return errors.New("plugin supervisor unavailable")
	}
	inst, ok := s.GetInstalled(pluginID)
	if !ok || !inst.Enabled || !inst.Manifest.HasPermission(PermUISidebar) ||
		inst.Manifest.Contributes == nil || len(inst.Manifest.Contributes.FileIcons) == 0 {
		return errors.New("file icon plugin is not active")
	}
	nbKey := cleanNotebookKey(notebookRoot)
	if nbKey == "" {
		return errors.New("no active notebook")
	}
	rel, ok := normalizeFileIconRelPath(relPath)
	if !ok {
		return fmt.Errorf("invalid relative path: %s", relPath)
	}
	iconID = strings.TrimSpace(iconID)
	normTint := normalizeFileIconTint(tint)
	if iconID != "" {
		_, byID, err := loadCachedFileIconPack(inst.InstallDir, inst.Manifest.Contributes.FileIcons[0].Pack)
		if err != nil {
			return err
		}
		if _, exists := byID[iconID]; !exists {
			return fmt.Errorf("unknown icon id: %s", iconID)
		}
	}

	s.contrib.mu.Lock()
	if iconID == "" {
		if byRoot := s.contrib.fileIcons[pluginID]; byRoot != nil {
			if entries := byRoot[nbKey]; entries != nil {
				delete(entries, rel)
				if len(entries) == 0 {
					delete(byRoot, nbKey)
				}
			}
			if len(byRoot) == 0 {
				delete(s.contrib.fileIcons, pluginID)
			}
		}
	} else {
		byRoot := s.contrib.fileIcons[pluginID]
		if byRoot == nil {
			byRoot = map[string]map[string]FileIconEntry{}
			s.contrib.fileIcons[pluginID] = byRoot
		}
		entries := byRoot[nbKey]
		if entries == nil {
			entries = map[string]FileIconEntry{}
			byRoot[nbKey] = entries
		}
		prev, existed := entries[rel]
		if !existed && len(entries) >= maxFileIconsPerScope {
			s.contrib.mu.Unlock()
			return fmt.Errorf("file icons exceeds limit (%d)", maxFileIconsPerScope)
		}
		entries[rel] = FileIconEntry{Icon: iconID, Tint: normTint}
		if !existed || prev.Icon != iconID {
			rec := s.contrib.fileIconRecents[pluginID]
			nextRec := make([]string, 0, maxFileIconRecents)
			nextRec = append(nextRec, iconID)
			for _, id := range rec {
				if id != iconID && len(nextRec) < maxFileIconRecents {
					nextRec = append(nextRec, id)
				}
			}
			s.contrib.fileIconRecents[pluginID] = nextRec
		}
	}
	s.contrib.saveLocked()
	s.contrib.mu.Unlock()
	s.notifyChanged()
	return nil
}

// RenameFileIconPath rebases custom file/folder icon mappings when a note or
// folder is renamed or moved inside the same notebook.
func (s *Supervisor) RenameFileIconPath(notebookRoot, oldRel, newRel string) {
	if s == nil || s.contrib == nil {
		return
	}
	nbKey := cleanNotebookKey(notebookRoot)
	if nbKey == "" {
		return
	}
	oldNorm, ok1 := normalizeFileIconRelPath(oldRel)
	newNorm, ok2 := normalizeFileIconRelPath(newRel)
	if !ok1 || !ok2 || oldNorm == newNorm || oldNorm == NotebookFileIconRelPath || newNorm == NotebookFileIconRelPath {
		return
	}
	prefix := oldNorm + "/"
	changed := false
	s.contrib.mu.Lock()
	for _, byRoot := range s.contrib.fileIcons {
		entries := byRoot[nbKey]
		if len(entries) == 0 {
			continue
		}
		moved := map[string]FileIconEntry{}
		for rel, ent := range entries {
			if rel == oldNorm {
				delete(entries, rel)
				moved[newNorm] = ent
				changed = true
			} else if strings.HasPrefix(rel, prefix) {
				delete(entries, rel)
				moved[newNorm+"/"+strings.TrimPrefix(rel, prefix)] = ent
				changed = true
			}
		}
		for rel, ent := range moved {
			entries[rel] = ent
		}
	}
	if changed {
		s.contrib.saveLocked()
	}
	s.contrib.mu.Unlock()
	if changed {
		s.notifyChanged()
	}
}

// DeleteFileIconPath removes custom file/folder icon mappings when a note or
// folder (and its descendants) is moved to Trash or deleted.
func (s *Supervisor) DeleteFileIconPath(notebookRoot, relPath string) {
	if s == nil || s.contrib == nil {
		return
	}
	nbKey := cleanNotebookKey(notebookRoot)
	if nbKey == "" {
		return
	}
	norm, ok := normalizeFileIconRelPath(relPath)
	if !ok || norm == NotebookFileIconRelPath {
		return
	}
	prefix := norm + "/"
	changed := false
	s.contrib.mu.Lock()
	for pluginID, byRoot := range s.contrib.fileIcons {
		entries := byRoot[nbKey]
		if len(entries) == 0 {
			continue
		}
		for rel := range entries {
			if rel == norm || strings.HasPrefix(rel, prefix) {
				delete(entries, rel)
				changed = true
			}
		}
		if len(entries) == 0 {
			delete(byRoot, nbKey)
		}
		if len(byRoot) == 0 {
			delete(s.contrib.fileIcons, pluginID)
		}
	}
	if changed {
		s.contrib.saveLocked()
	}
	s.contrib.mu.Unlock()
	if changed {
		s.notifyChanged()
	}
}

func (s *Supervisor) resolveFileIconPrefsLocked(m Manifest) (picker, syncTabs bool) {
	if m.Contributes != nil && len(m.Contributes.FileIcons) > 0 {
		picker = m.Contributes.FileIcons[0].Picker
		syncTabs = m.Contributes.FileIcons[0].SyncTabs
	}
	if pref, ok := s.contrib.fileIconPrefs[m.ID]; ok {
		if pref.Picker != nil {
			picker = *pref.Picker
		}
		if pref.SyncTabs != nil {
			syncTabs = *pref.SyncTabs
		}
	}
	return picker, syncTabs
}

func (s *Supervisor) handleSidebarGetFileIcons(m Manifest, rt *Runtime, decode func(any) *rpcError) (any, *rpcError, bool) {
	if !m.HasPermission(PermUISidebar) {
		return nil, missingPerm(PermUISidebar), true
	}
	var p struct {
		NotebookRoot string `json:"notebook_root"`
	}
	if e := decode(&p); e != nil {
		return nil, e, true
	}
	nbRoot, err := rt.resolveNotebookRoot(p.NotebookRoot)
	if err != nil {
		return nil, rpcErr(-32602, err.Error()), true
	}
	nbKey := cleanNotebookKey(nbRoot)
	out := map[string]FileIconEntry{}
	var recent []string
	var picker, syncTabs bool
	s.contrib.mu.RLock()
	if nbKey != "" {
		for rel, ent := range s.contrib.fileIcons[m.ID][nbKey] {
			out[rel] = ent
		}
	}
	if rec := s.contrib.fileIconRecents[m.ID]; len(rec) > 0 {
		recent = append([]string(nil), rec...)
	} else {
		recent = []string{}
	}
	picker, syncTabs = s.resolveFileIconPrefsLocked(m)
	s.contrib.mu.RUnlock()
	return map[string]any{
		"entries":       out,
		"recent":        recent,
		"notebook_root": nbRoot,
		"prefs": map[string]bool{
			"picker":    picker,
			"sync_tabs": syncTabs,
		},
	}, nil, true
}

func (s *Supervisor) handleSidebarSetFileIcons(m Manifest, rt *Runtime, decode func(any) *rpcError) (any, *rpcError, bool) {
	if !m.HasPermission(PermUISidebar) {
		return nil, missingPerm(PermUISidebar), true
	}
	if m.Contributes == nil || len(m.Contributes.FileIcons) == 0 {
		return nil, rpcErr(-32602, "plugin does not declare contributes.file_icons"), true
	}
	var p struct {
		NotebookRoot string                    `json:"notebook_root"`
		Clear        bool                      `json:"clear"`
		Patch        bool                      `json:"patch"`
		Entries      map[string]*FileIconEntry `json:"entries"`
		Prefs        *FileIconPrefs            `json:"prefs"`
	}
	if e := decode(&p); e != nil {
		return nil, e, true
	}
	updateEntries := p.Clear || p.Patch || p.Entries != nil
	var nbKey string
	if updateEntries {
		nbRoot, err := rt.resolveNotebookRoot(p.NotebookRoot)
		if err != nil {
			return nil, rpcErr(-32602, err.Error()), true
		}
		nbKey = cleanNotebookKey(nbRoot)
		if nbKey == "" {
			return nil, rpcErr(-32602, "no active notebook for file icons"), true
		}
	}
	if len(p.Entries) > maxFileIconsPerScope {
		return nil, rpcErr(-32602, fmt.Sprintf("entries exceeds limit (%d)", maxFileIconsPerScope)), true
	}
	var byID map[string]FileIconDef
	if len(p.Entries) > 0 {
		inst, ok := s.GetInstalled(m.ID)
		if !ok {
			return nil, rpcErr(-32603, "plugin installation not found"), true
		}
		var err error
		_, byID, err = loadCachedFileIconPack(inst.InstallDir, m.Contributes.FileIcons[0].Pack)
		if err != nil {
			return nil, rpcErr(-32603, err.Error()), true
		}
	}
	parsed := make(map[string]*FileIconEntry, len(p.Entries))
	for rawRel, ent := range p.Entries {
		rel, ok := normalizeFileIconRelPath(rawRel)
		if !ok {
			return nil, rpcErr(-32602, "invalid relative path: "+rawRel), true
		}
		if ent == nil || strings.TrimSpace(ent.Icon) == "" {
			parsed[rel] = nil
			continue
		}
		iconID := strings.TrimSpace(ent.Icon)
		if _, exists := byID[iconID]; !exists {
			return nil, rpcErr(-32602, "unknown icon id: "+iconID), true
		}
		clean := FileIconEntry{
			Icon: iconID,
			Tint: normalizeFileIconTint(ent.Tint),
		}
		parsed[rel] = &clean
	}

	s.mu.RLock()
	if !s.isRuntimeActiveLocked(m.ID, rt) {
		s.mu.RUnlock()
		return nil, rpcErr(-32603, "plugin runtime is no longer active"), true
	}
	s.contrib.mu.Lock()
	if p.Prefs != nil {
		cur := s.contrib.fileIconPrefs[m.ID]
		if p.Prefs.Picker != nil {
			v := *p.Prefs.Picker
			cur.Picker = &v
		}
		if p.Prefs.SyncTabs != nil {
			v := *p.Prefs.SyncTabs
			cur.SyncTabs = &v
		}
		s.contrib.fileIconPrefs[m.ID] = cur
	}
	count := 0
	if updateEntries {
		var next map[string]FileIconEntry
		if p.Clear {
			next = nil
		} else if p.Patch {
			next = make(map[string]FileIconEntry)
			for k, v := range s.contrib.fileIcons[m.ID][nbKey] {
				next[k] = v
			}
			for rel, ent := range parsed {
				if ent == nil {
					delete(next, rel)
				} else {
					next[rel] = *ent
				}
			}
			if len(next) > maxFileIconsPerScope {
				s.contrib.mu.Unlock()
				s.mu.RUnlock()
				return nil, rpcErr(-32602, fmt.Sprintf("entries exceeds limit (%d)", maxFileIconsPerScope)), true
			}
		} else {
			next = make(map[string]FileIconEntry, len(parsed))
			for rel, ent := range parsed {
				if ent != nil {
					next[rel] = *ent
				}
			}
		}
		byRoot := s.contrib.fileIcons[m.ID]
		if len(next) == 0 {
			if byRoot != nil {
				delete(byRoot, nbKey)
				if len(byRoot) == 0 {
					delete(s.contrib.fileIcons, m.ID)
				}
			}
		} else {
			if byRoot == nil {
				byRoot = map[string]map[string]FileIconEntry{}
				s.contrib.fileIcons[m.ID] = byRoot
			}
			byRoot[nbKey] = next
		}
		count = len(next)
	} else if nbKey != "" {
		count = len(s.contrib.fileIcons[m.ID][nbKey])
	}
	picker, syncTabs := s.resolveFileIconPrefsLocked(m)
	s.contrib.saveLocked()
	s.contrib.mu.Unlock()
	s.mu.RUnlock()
	s.notifyChanged()
	return map[string]any{
		"ok":    true,
		"count": count,
		"prefs": map[string]bool{
			"picker":    picker,
			"sync_tabs": syncTabs,
		},
	}, nil, true
}
