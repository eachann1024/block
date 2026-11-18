package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"block/assets"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

const (
	defaultPanelWidth    float32 = theme.AIPanelDefaultWidth
	minPanelWidth        float32 = theme.AIPanelMinWidth
	maxPanelWidth        float32 = theme.AIPanelMaxWidth
	maxBuiltinSteps              = 8
	streamRedrawInterval         = 16 * time.Millisecond
	draftPersistDelay            = 300 * time.Millisecond
)

type tabDraft struct {
	Text   string
	Refs   []string
	Skills []string
	Retry  *model.SentDraft
}

type pickKind uint8

const (
	pickFolder pickKind = iota
	pickPage
	pickSkill
	pickNewConv
)

type pickItem struct {
	kind  pickKind
	value string
	label string
	hint  string
}

type questionUIState struct {
	qi         int
	answers    map[int]map[int]bool
	customText map[int]string
	dismissed  bool
	submitted  bool
}

type approvalUIState struct {
	choice    model.ApprovalDecision
	submitted bool
}

// Controller manages AI assistant state, persistence, protocols, plans, and UI rendering.
type Controller struct {
	mu      sync.RWMutex
	dataDir string
	host    model.WorkspaceHost
	codec   model.MarkdownCodec

	services ui.Services

	open        bool
	fullscreen  bool
	panelWidth  float32
	renderWidth float32

	providerCfg model.AIProviderConfig
	backendsCfg model.AIBackendsConfig

	convs     []model.Conversation
	currentID uint64

	draftText      string
	draftRefs      []string
	draftSkills    []string
	retryDraft     *model.SentDraft
	sentDraft      *model.SentDraft
	deferredDrafts []model.SentDraft
	tabDrafts      map[uint64]tabDraft

	composerEpoch     int
	composerHandle    ui.Handle
	composerComposing bool
	composerSelectEnd bool
	messagesList      ui.ListState
	pickerList        ui.ListState
	mdOpts            markdown.Options

	pickIndex       int
	pickerDismissed bool
	pickerFolder    string
	pickerFolders   []string
	pickerPages     []string
	pickerSkills    []model.SkillSpec
	pickerLoading   bool
	pickerInjected  bool
	pickerRoot      string
	pickerRefreshed time.Time

	historyOpen      bool
	historyScroll    ui.ScrollState
	panelBox         ui.Rect
	histTriggerBox   ui.Rect
	deckOpen         bool
	deckSearch       string
	deckTriggerBox   ui.Rect
	deckSearchHandle ui.Handle
	deckScroll       ui.ScrollState

	backendStatuses map[model.BackendKind]BackendStatus
	backendModels   map[model.BackendKind][]ModelOption
	modelStatus     map[model.BackendKind]string
	modelRefreshed  map[model.BackendKind]time.Time
	modelGeneration map[model.BackendKind]uint64
	modelGenSeq     uint64
	statusOverrides map[model.BackendKind]BackendStatus
	panelProbedOnce bool

	streaming         bool
	streamSerial      uint64
	streamCancel      func()
	streamRedrawLast  time.Time
	streamRedrawTimer *time.Timer
	draftPersistTimer *time.Timer
	partialText       string
	partialThink      string
	partialSteps      []model.ProcessStep
	turnError         string
	turnErrorSettings bool

	mockResponder        bool
	mockApprovalFailOnce bool
	onDismissFloating    func()

	activeExternal *runningExternalTurn
	activePlan     *ActivePlanPreview
	planSelections map[planSelKey]*diffSelection

	expandedProcess     map[string]bool
	expandedStepDetails map[string]bool
	expandedSources     map[string]bool
	expandedNotes       map[string]bool
	questionUI          map[string]*questionUIState
	approvalUI          map[string]*approvalUIState

	settingsFocused      model.BackendKind
	settingsExpandedProv string
	settingsExpandedAg   model.BackendKind
	settingsProbedOnce   bool
	settingsShowKey      bool
	settingsProvShowKey  map[string]bool
	settingsTesting      bool
	settingsTestingProv  string
	settingsTestOK       bool
	settingsTestErr      string
	settingsModelStatus  string
	settingsBaseURL      string
	settingsAPIKey       string
	settingsModel        string

	addProvModalOpen bool
	addProvPreset    string
	addProvName      string
	addProvProto     model.Protocol
	addProvBaseURL   string
	addProvAPIKey    string
	addProvShowKey   bool
	addProvTesting   bool
	addProvTestOK    bool
	addProvTestErr   string

	addAgentModalOpen bool
	addAgentStep      int
	addAgentShowMore  bool
	addAgentKind      model.BackendKind
	addAgentName      string
	addAgentBinPath   string
	addAgentThinking  string
	addAgentProbing   bool
	addAgentStatus    BackendStatus
	addAgentHasStatus bool
	addAgentCopiedCmd string
	addAgentErr       string

	annotCfg            model.AnnotationConfig
	annotMenuOpen       bool
	annotMenuBackend    model.BackendKind
	annotMenuSearch     string
	annotMenuTriggerBox ui.Rect
	annotMenuScroll     ui.ScrollState
	annotSearchHandle   ui.Handle
}

// NewController initializes an AI Controller backed by dataDir, host, and codec.
func NewController(dataDir string, host model.WorkspaceHost, codec model.MarkdownCodec) *Controller {
	if strings.TrimSpace(dataDir) == "" {
		if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
			dataDir = filepath.Join(cfgDir, "Block")
		} else {
			dataDir = filepath.Join(os.TempDir(), "block-ai")
		}
	}
	ctrl := &Controller{
		dataDir:             dataDir,
		host:                host,
		codec:               codec,
		panelWidth:          defaultPanelWidth,
		tabDrafts:           make(map[uint64]tabDraft),
		backendStatuses:     make(map[model.BackendKind]BackendStatus),
		backendModels:       make(map[model.BackendKind][]ModelOption),
		modelStatus:         make(map[model.BackendKind]string),
		modelRefreshed:      make(map[model.BackendKind]time.Time),
		modelGeneration:     make(map[model.BackendKind]uint64),
		statusOverrides:     make(map[model.BackendKind]BackendStatus),
		planSelections:      make(map[planSelKey]*diffSelection),
		expandedProcess:     make(map[string]bool),
		expandedStepDetails: make(map[string]bool),
		expandedSources:     make(map[string]bool),
		expandedNotes:       make(map[string]bool),
		questionUI:          make(map[string]*questionUIState),
		approvalUI:          make(map[string]*approvalUIState),
		settingsFocused:     model.BackendBuiltin,
		settingsProvShowKey: make(map[string]bool),
	}
	ctrl.backendStatuses[model.BackendBuiltin] = BackendStatus{
		State:   BackendStateReady,
		Version: "Block",
	}
	ctrl.loadAllFromDisk()
	ctrl.settingsExpandedProv = ctrl.providerCfg.Provider
	ctrl.syncSettingsFieldsLocked()
	ctrl.loadBuiltinSkillsLocked()
	ctrl.messagesList.FollowEnd = true
	return ctrl
}

func (c *Controller) loadAllFromDisk() {
	c.providerCfg = normalizeProviderConfig(c.readProviderFile())
	c.backendsCfg = c.readBackendsFile()
	c.convs = c.readChatsFile()
	c.deferredDrafts = c.readFailedDraftsFile()

	active := c.readActiveDraftFile()
	if active.Conversation != nil && c.hasConvLocked(*active.Conversation) {
		c.currentID = *active.Conversation
	}
	c.draftText = active.Text
	c.draftRefs = append([]string(nil), active.Refs...)
	c.draftSkills = append([]string(nil), active.Skills...)
	if active.Retry != nil && active.Retry.Conversation == c.currentID && c.currentID != 0 {
		cp := *active.Retry
		c.retryDraft = &cp
	}
	c.ensureValidSelectionLocked()
	c.loadOrSeedAnnotationConfigLocked()
}

func (c *Controller) loadOrSeedAnnotationConfigLocked() {
	var cfg model.AnnotationConfig
	if c.readJSONFile("ai-annotations.json", &cfg) && cfg.Backend != "" {
		if cfg.Thinking == "" {
			cfg.Thinking = "high"
		}
		c.annotCfg = cfg
		c.annotMenuBackend = cfg.Backend
		return
	}
	sel := c.selectedBackendLocked()
	mdl := c.backendModelLocked(sel)
	prov := c.providerCfg.Provider
	if prov == "" {
		prov = "deepseek"
	}
	c.annotCfg = model.AnnotationConfig{
		Backend:  sel,
		Provider: prov,
		Model:    mdl,
		Thinking: c.backendThinkingLocked(sel),
	}
	c.annotMenuBackend = sel
	_ = c.writeJSONFile("ai-annotations.json", c.annotCfg)
}

func (c *Controller) saveAnnotationConfigLocked(cfg model.AnnotationConfig) error {
	if cfg.Backend == "" {
		cfg.Backend = model.BackendBuiltin
	}
	if cfg.Thinking == "" {
		cfg.Thinking = "high"
	}
	if err := c.writeJSONFile("ai-annotations.json", cfg); err != nil {
		return err
	}
	c.annotCfg = cfg
	return nil
}

func (c *Controller) filePath(name string) string {
	return filepath.Join(c.dataDir, name)
}

func (c *Controller) readJSONFile(name string, dst any) bool {
	data, err := os.ReadFile(c.filePath(name))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}

func (c *Controller) writeJSONFile(name string, v any) error {
	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p := c.filePath(name)
	tmp := fmt.Sprintf("%s.tmp.%d", p, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (c *Controller) readProviderFile() model.AIProviderConfig {
	cfg := model.AIProviderConfig{
		Enabled:  true,
		Provider: "deepseek",
		Protocol: model.ProtocolOpenAI,
		BaseURL:  "https://api.deepseek.com/v1",
	}
	_ = c.readJSONFile("ai.json", &cfg)
	return cfg
}

func enforceAllBackendsFullPermissions(cfg *model.AIBackendsConfig) {
	if cfg.Adapters == nil {
		cfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	for _, k := range model.AllBackends {
		if k == model.BackendBuiltin {
			continue
		}
		opt := cfg.Adapters[string(k)]
		opt.FullPermissions = true
		setCustomCLIBinaryPath(k, opt.BinaryPath)
		cfg.Adapters[string(k)] = opt
	}
	for k, opt := range cfg.Adapters {
		opt.FullPermissions = true
		cfg.Adapters[k] = opt
	}
}

func (c *Controller) readBackendsFile() model.AIBackendsConfig {
	var cfg model.AIBackendsConfig
	if !c.readJSONFile("ai-backends.json", &cfg) {
		cfg.Selected = model.BackendBuiltin
	}
	if cfg.Selected == "" {
		cfg.Selected = model.BackendBuiltin
	}
	if cfg.Adapters != nil {
		for k, opt := range cfg.Adapters {
			if k != string(model.BackendBuiltin) && opt.Enabled != nil && *opt.Enabled {
				opt.Added = true
				cfg.Adapters[k] = opt
			}
		}
	}
	enforceAllBackendsFullPermissions(&cfg)
	return cfg
}

func (c *Controller) readChatsFile() []model.Conversation {
	var convs []model.Conversation
	if !c.readJSONFile("ai-chats.json", &convs) {
		return nil
	}
	return convs
}

func (c *Controller) readActiveDraftFile() model.ActiveDraft {
	var draft model.ActiveDraft
	_ = c.readJSONFile("ai-active-draft.json", &draft)
	return draft
}

func (c *Controller) readFailedDraftsFile() []model.SentDraft {
	var list []model.SentDraft
	_ = c.readJSONFile("ai-failed-drafts.json", &list)
	return list
}

func (c *Controller) persistChatsLocked() {
	keep := make([]model.Conversation, 0, len(c.convs))
	for _, conv := range c.convs {
		if len(conv.Messages) > 0 {
			keep = append(keep, conv)
		}
	}
	_ = c.writeJSONFile("ai-chats.json", keep)
}

func (c *Controller) cancelDraftPersistLocked() {
	if c.draftPersistTimer != nil {
		c.draftPersistTimer.Stop()
		c.draftPersistTimer = nil
	}
}

func (c *Controller) schedulePersistActiveDraftLocked() {
	c.cancelDraftPersistLocked()
	var timer *time.Timer
	timer = time.AfterFunc(draftPersistDelay, func() {
		c.mu.Lock()
		if c.draftPersistTimer != timer {
			c.mu.Unlock()
			return
		}
		c.draftPersistTimer = nil
		c.writeActiveDraftLocked()
		c.mu.Unlock()
	})
	c.draftPersistTimer = timer
}

func (c *Controller) persistActiveDraftLocked() {
	c.cancelDraftPersistLocked()
	c.writeActiveDraftLocked()
}

func (c *Controller) writeActiveDraftLocked() {
	var convPtr *uint64
	if c.currentID != 0 {
		id := c.currentID
		convPtr = &id
	}
	draft := model.ActiveDraft{
		Conversation: convPtr,
		Text:         c.draftText,
		Refs:         append([]string(nil), c.draftRefs...),
		Skills:       append([]string(nil), c.draftSkills...),
		Retry:        c.retryDraft,
	}
	_ = c.writeJSONFile("ai-active-draft.json", draft)
}

func (c *Controller) persistFailedDraftsLocked() {
	_ = c.writeJSONFile("ai-failed-drafts.json", c.deferredDrafts)
}

func (c *Controller) requestRedraw() {
	c.mu.RLock()
	svc := c.services
	host := c.host
	c.mu.RUnlock()
	if host != nil {
		host.RequestRedraw()
		return
	}
	svc.Invalidate()
}

func (c *Controller) requestStreamRedraw() {
	now := time.Now()
	c.mu.Lock()
	elapsed := now.Sub(c.streamRedrawLast)
	if elapsed >= streamRedrawInterval {
		if c.streamRedrawTimer != nil {
			c.streamRedrawTimer.Stop()
			c.streamRedrawTimer = nil
		}
		c.streamRedrawLast = now
		c.mu.Unlock()
		c.requestRedraw()
		return
	}
	if c.streamRedrawTimer != nil {
		c.mu.Unlock()
		return
	}
	wait := streamRedrawInterval - elapsed
	var timer *time.Timer
	timer = time.AfterFunc(wait, func() {
		c.mu.Lock()
		if c.streamRedrawTimer != timer {
			c.mu.Unlock()
			return
		}
		c.streamRedrawTimer = nil
		c.streamRedrawLast = time.Now()
		c.mu.Unlock()
		c.requestRedraw()
	})
	c.streamRedrawTimer = timer
	c.mu.Unlock()
}

func (c *Controller) clearStreamRedrawLocked() {
	if c.streamRedrawTimer != nil {
		c.streamRedrawTimer.Stop()
		c.streamRedrawTimer = nil
	}
	c.streamRedrawLast = time.Time{}
}

func (c *Controller) hasConvLocked(id uint64) bool {
	if id == 0 {
		return false
	}
	for i := range c.convs {
		if c.convs[i].ID == id {
			return true
		}
	}
	return false
}

func (c *Controller) findConvLocked(id uint64) *model.Conversation {
	if id == 0 {
		return nil
	}
	for i := range c.convs {
		if c.convs[i].ID == id {
			return &c.convs[i]
		}
	}
	return nil
}

// IsOpen reports whether the AI panel is open.
func (c *Controller) IsOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.open
}

// SetOnDismissFloating registers a callback invoked when the AI close button is
// clicked while rendered inside a temporary floating edge card.
func (c *Controller) SetOnDismissFloating(fn func()) {
	c.mu.Lock()
	c.onDismissFloating = fn
	c.mu.Unlock()
}

// HasActiveOverlay reports whether the AI panel has an open popup (history, model deck,
// or @/slash picker) that should keep a floating edge card alive while interacting.
func (c *Controller) HasActiveOverlay() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	return c.historyOpen || c.deckOpen || pickerOpen
}

// CloseTransientPopups closes transient AI header/composer popups.
func (c *Controller) CloseTransientPopups() {
	c.mu.Lock()
	c.historyOpen = false
	c.deckOpen = false
	c.pickerDismissed = true
	c.mu.Unlock()
}

// SetOpen sets the AI panel open state.
func (c *Controller) SetOpen(open bool) {
	c.mu.Lock()
	dismissFn := c.onDismissFloating
	if c.open == open {
		if !open {
			c.historyOpen = false
			c.deckOpen = false
		}
		c.mu.Unlock()
		if !open && dismissFn != nil {
			dismissFn()
		}
		return
	}
	c.open = open
	if !open {
		c.historyOpen = false
		c.deckOpen = false
	} else {
		c.composerHandle.Focus()
	}
	c.mu.Unlock()
	if !open && dismissFn != nil {
		dismissFn()
	}
	c.requestRedraw()
}

// ToggleOpen toggles the AI panel open state.
func (c *Controller) ToggleOpen() {
	c.mu.Lock()
	next := !c.open
	c.open = next
	if !next {
		c.historyOpen = false
		c.deckOpen = false
	} else {
		c.composerHandle.Focus()
	}
	c.mu.Unlock()
	c.requestRedraw()
}

// IsFullscreen reports whether the AI panel is in fullscreen mode.
func (c *Controller) IsFullscreen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fullscreen
}

// SetFullscreen sets the AI panel fullscreen state.
func (c *Controller) SetFullscreen(fs bool) {
	c.mu.Lock()
	if c.fullscreen == fs {
		c.mu.Unlock()
		return
	}
	c.fullscreen = fs
	c.mu.Unlock()
	c.requestRedraw()
}

// ToggleFullscreen toggles fullscreen mode for the AI panel.
func (c *Controller) ToggleFullscreen() {
	c.mu.Lock()
	if !c.open {
		c.open = true
		c.fullscreen = true
	} else {
		c.fullscreen = !c.fullscreen
	}
	c.mu.Unlock()
	c.requestRedraw()
}

// PanelWidth returns the current sidebar width of the AI panel.
func (c *Controller) PanelWidth() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.panelWidth <= 0 {
		return defaultPanelWidth
	}
	return c.panelWidth
}

func clampPanelWidth(w float32) float32 {
	if w < minPanelWidth {
		return minPanelWidth
	}
	if w > maxPanelWidth {
		return maxPanelWidth
	}
	return w
}

// SyncPanelWidth clamps and updates the AI panel width without requesting a redraw.
func (c *Controller) SyncPanelWidth(w float32) {
	w = clampPanelWidth(w)
	c.mu.Lock()
	c.panelWidth = w
	c.mu.Unlock()
}

// SetPanelWidth clamps and updates the AI panel width.
func (c *Controller) SetPanelWidth(w float32) {
	w = clampPanelWidth(w)
	c.mu.Lock()
	if c.panelWidth == w {
		c.mu.Unlock()
		return
	}
	c.panelWidth = w
	c.mu.Unlock()
	c.requestRedraw()
}

// SetRenderWidth sets the transient per-frame render width without altering PanelWidth.
func (c *Controller) SetRenderWidth(w float32) {
	if w < 0 {
		w = 0
	}
	c.mu.Lock()
	c.renderWidth = w
	c.mu.Unlock()
}

// ProviderConfig returns a copy of the built-in AI provider config.
func (c *Controller) ProviderConfig() model.AIProviderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneProviderConfig(c.providerCfg)
}

// SaveProviderConfig normalizes, saves, and applies the built-in AI provider config.
func (c *Controller) SaveProviderConfig(cfg model.AIProviderConfig) error {
	norm := normalizeProviderConfig(cfg)
	if norm.Providers == nil {
		norm.Providers = make(map[string]model.ProviderProfile)
	}
	if norm.Provider != "" {
		existing := norm.Providers[norm.Provider]
		existing.Protocol = norm.Protocol
		existing.BaseURL = norm.BaseURL
		existing.APIKey = norm.APIKey
		existing.Model = norm.Model
		if existing.Enabled == nil {
			on := true
			existing.Enabled = &on
		}
		norm.Providers[norm.Provider] = existing
		if !containsString(norm.ProviderOrder, norm.Provider) {
			norm.ProviderOrder = append(norm.ProviderOrder, norm.Provider)
		}
	}
	if err := c.writeJSONFile("ai.json", norm); err != nil {
		return err
	}
	c.mu.Lock()
	c.providerCfg = norm
	if c.settingsExpandedProv == "" {
		c.settingsExpandedProv = norm.Provider
	}
	c.syncSettingsFieldsLocked()
	if c.turnErrorSettings {
		c.turnError = ""
		c.turnErrorSettings = false
	}
	c.invalidateModelCacheLocked(model.BackendBuiltin)
	shouldRefresh := (c.panelProbedOnce || c.settingsProbedOnce) && !c.mockResponder
	c.mu.Unlock()
	if shouldRefresh {
		c.refreshModelsAsync(model.BackendBuiltin)
	}
	c.requestRedraw()
	return nil
}

// AddProviderEntry appends a new API provider profile, activates & expands it, and ensures BackendBuiltin is enabled.
func (c *Controller) AddProviderEntry(presetID, name string, proto model.Protocol, baseURL, apiKey string) (string, error) {
	cfg := c.ProviderConfig()
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]model.ProviderProfile)
	}
	trimmedName := strings.TrimSpace(name)
	trimmedURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if proto == "" {
		proto = model.ProtocolOpenAI
	}
	if p := findProviderPreset(presetID); p != nil {
		if trimmedName == "" {
			trimmedName = p.DefaultName
		}
		if trimmedURL == "" {
			trimmedURL = p.BaseURL
		}
	}
	if trimmedName == "" {
		if h := endpointHostLabel(trimmedURL); h != "" {
			trimmedName = h
		} else {
			trimmedName = theme.Tr("自定义供应商", "Custom Provider")
		}
	}

	// Pick clean unique ID.
	baseSlug := strings.ToLower(strings.TrimSpace(presetID))
	if baseSlug == "" || baseSlug == "custom" {
		var sb strings.Builder
		for _, r := range strings.ToLower(trimmedName) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				sb.WriteRune(r)
			} else if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "-") {
				sb.WriteByte('-')
			}
		}
		baseSlug = strings.Trim(sb.String(), "-")
		if baseSlug == "" {
			baseSlug = "provider"
		}
	}
	id := baseSlug
	if _, exists := cfg.Providers[id]; exists || id == cfg.Provider {
		for n := 2; ; n++ {
			cand := fmt.Sprintf("%s-%d", baseSlug, n)
			if _, ok := cfg.Providers[cand]; !ok {
				id = cand
				break
			}
		}
	}

	on := true
	cfg.Providers[id] = model.ProviderProfile{
		Name:     trimmedName,
		Protocol: proto,
		BaseURL:  trimmedURL,
		APIKey:   strings.TrimSpace(apiKey),
		Enabled:  &on,
		Custom:   true,
	}
	cfg.ProviderOrder = append(cfg.ProviderOrder, id)
	cfg.Enabled = true
	cfg.Provider = id
	cfg.Protocol = proto
	cfg.BaseURL = trimmedURL
	cfg.APIKey = strings.TrimSpace(apiKey)
	cfg.Model = ""

	if err := c.SaveProviderConfig(cfg); err != nil {
		return "", err
	}
	_ = c.SetBackendEnabled(model.BackendBuiltin, true)
	c.mu.Lock()
	c.settingsExpandedProv = id
	c.settingsTestErr = ""
	c.settingsTestOK = false
	c.mu.Unlock()
	return id, nil
}

// UpdateProviderEntry updates a specific provider profile and syncs active fields if it is the primary provider.
func (c *Controller) UpdateProviderEntry(id string, mutate func(prof *model.ProviderProfile)) error {
	cfg := c.ProviderConfig()
	entries := orderedProviderEntries(cfg)
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]model.ProviderProfile, len(entries))
	}
	for _, e := range entries {
		if _, ok := cfg.Providers[e.ID]; !ok {
			on := e.Enabled
			cfg.Providers[e.ID] = model.ProviderProfile{
				Name:     e.Name,
				Protocol: e.Protocol,
				BaseURL:  e.BaseURL,
				APIKey:   e.APIKey,
				Model:    e.Model,
				Enabled:  &on,
				Custom:   e.Custom,
			}
		}
		if !containsString(cfg.ProviderOrder, e.ID) {
			cfg.ProviderOrder = append(cfg.ProviderOrder, e.ID)
		}
	}
	prof := cfg.Providers[id]
	mutate(&prof)
	prof.BaseURL = strings.TrimRight(strings.TrimSpace(prof.BaseURL), "/")
	prof.APIKey = strings.TrimSpace(prof.APIKey)
	if prof.Protocol == "" {
		prof.Protocol = model.ProtocolOpenAI
	}
	cfg.Providers[id] = prof
	if cfg.Provider == id {
		cfg.Protocol = prof.Protocol
		cfg.BaseURL = prof.BaseURL
		cfg.APIKey = prof.APIKey
		cfg.Model = prof.Model
	}
	return c.SaveProviderConfig(cfg)
}

// ActivateProviderEntry selects id as the active primary API provider when enabled.
func (c *Controller) ActivateProviderEntry(id string) error {
	cfg := c.ProviderConfig()
	entries := orderedProviderEntries(cfg)
	for _, e := range entries {
		if e.ID == id {
			cfg.Provider = e.ID
			cfg.Protocol = e.Protocol
			cfg.BaseURL = e.BaseURL
			cfg.APIKey = e.APIKey
			cfg.Model = e.Model
			return c.SaveProviderConfig(cfg)
		}
	}
	return nil
}

// SetProviderEntryEnabled toggles one API provider entry and syncs BackendBuiltin enablement.
func (c *Controller) SetProviderEntryEnabled(id string, on bool) error {
	cfg := c.ProviderConfig()
	entries := orderedProviderEntries(cfg)
	if !on {
		otherProvEnabled := false
		for _, e := range entries {
			if e.ID != id && e.Enabled {
				otherProvEnabled = true
				break
			}
		}
		if !otherProvEnabled && !c.CanDisableBackend(model.BackendBuiltin) {
			return errors.New(theme.Tr("至少需要保留一个启用的 AI 后端", "At least one AI backend must remain enabled"))
		}
	}

	if err := c.UpdateProviderEntry(id, func(prof *model.ProviderProfile) {
		val := on
		prof.Enabled = &val
	}); err != nil {
		return err
	}

	updated := c.ProviderConfig()
	afterEntries := orderedProviderEntries(updated)
	anyEnabled := false
	var firstEnabled *ProviderEntry
	for i := range afterEntries {
		if afterEntries[i].Enabled {
			anyEnabled = true
			if firstEnabled == nil {
				firstEnabled = &afterEntries[i]
			}
		}
	}
	if on {
		_ = c.ActivateProviderEntry(id)
		_ = c.SetBackendEnabled(model.BackendBuiltin, true)
	} else if !anyEnabled {
		_ = c.SetBackendEnabled(model.BackendBuiltin, false)
	} else if updated.Provider == id && firstEnabled != nil {
		_ = c.ActivateProviderEntry(firstEnabled.ID)
	}
	return nil
}

// RemoveProviderEntry deletes a custom provider entry from ai.json.
func (c *Controller) RemoveProviderEntry(id string) error {
	cfg := c.ProviderConfig()
	entries := orderedProviderEntries(cfg)
	if len(entries) <= 1 {
		return nil
	}
	delete(cfg.Providers, id)
	cfg.ProviderOrder = removeString(append([]string(nil), cfg.ProviderOrder...), id)
	remaining := orderedProviderEntries(cfg)
	if cfg.Provider == id && len(remaining) > 0 {
		pick := remaining[0]
		for _, r := range remaining {
			if r.Enabled {
				pick = r
				break
			}
		}
		cfg.Provider = pick.ID
		cfg.Protocol = pick.Protocol
		cfg.BaseURL = pick.BaseURL
		cfg.APIKey = pick.APIKey
		cfg.Model = pick.Model
	}
	if err := c.SaveProviderConfig(cfg); err != nil {
		return err
	}
	c.mu.Lock()
	if c.settingsExpandedProv == id {
		if len(remaining) > 0 {
			c.settingsExpandedProv = remaining[0].ID
		} else {
			c.settingsExpandedProv = ""
		}
	}
	c.mu.Unlock()
	return nil
}

func (c *Controller) syncSettingsFieldsLocked() {
	c.settingsBaseURL = c.providerCfg.BaseURL
	c.settingsAPIKey = c.providerCfg.APIKey
	c.settingsModel = c.providerCfg.Model
}

// BackendsConfig returns a copy of the backend adapter configuration.
func (c *Controller) BackendsConfig() model.AIBackendsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneBackendsConfig(c.backendsCfg)
}

// SaveBackendsConfig persists and applies the backend adapter configuration.
func (c *Controller) SaveBackendsConfig(cfg model.AIBackendsConfig) error {
	cp := cloneBackendsConfig(cfg)
	enforceAllBackendsFullPermissions(&cp)
	c.mu.Lock()
	prev := c.backendsCfg
	c.backendsCfg = cp
	c.ensureValidSelectionLocked()
	toSave := cloneBackendsConfig(c.backendsCfg)
	if err := c.writeJSONFile("ai-backends.json", toSave); err != nil {
		c.backendsCfg = prev
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

func isBackendEnabled(cfg model.AIBackendsConfig, kind model.BackendKind) bool {
	if opt, ok := cfg.Adapters[string(kind)]; ok && opt.Enabled != nil {
		return *opt.Enabled
	}
	return kind == model.BackendBuiltin
}

// CanDisableBackend reports whether kind can be disabled while keeping at least one enabled backend.
func (c *Controller) CanDisableBackend(kind model.BackendKind) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.canDisableBackendLocked(kind)
}

func (c *Controller) canDisableBackendLocked(kind model.BackendKind) bool {
	if !isBackendEnabled(c.backendsCfg, kind) {
		return false
	}
	recognized := c.recognizedBackendsLocked()
	enabledCount := 0
	for _, b := range recognized {
		if isBackendEnabled(c.backendsCfg, b) {
			enabledCount++
		}
	}
	return enabledCount > 1
}

// SetBackendEnabled enables or disables a backend while ensuring at least one remains enabled.
func (c *Controller) SetBackendEnabled(kind model.BackendKind, on bool) error {
	c.mu.Lock()
	if !on && !c.canDisableBackendLocked(kind) {
		c.mu.Unlock()
		return errors.New(theme.Tr("至少需要保留一个启用的 AI 后端", "At least one AI backend must remain enabled"))
	}
	prev := cloneBackendsConfig(c.backendsCfg)
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	if on && kind != model.BackendBuiltin {
		opt.Added = true
	}
	val := on
	opt.Enabled = &val
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt
	c.ensureValidSelectionLocked()
	if err := c.writeJSONFile("ai-backends.json", c.backendsCfg); err != nil {
		c.backendsCfg = prev
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

// AddAgentEntry registers and enables a local CLI agent instance (T3 Code style provider onboarding).
func (c *Controller) AddAgentEntry(kind model.BackendKind, displayName, binaryPath, thinking string) error {
	if kind == "" || kind == model.BackendBuiltin {
		return errors.New("invalid local agent kind")
	}
	switch thinking {
	case "low", "medium", "high":
	default:
		thinking = "high"
	}
	c.mu.Lock()
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	on := true
	opt.Added = true
	opt.Enabled = &on
	opt.Name = strings.TrimSpace(displayName)
	opt.BinaryPath = strings.TrimSpace(binaryPath)
	opt.ThinkingLevel = thinking
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt
	setCustomCLIBinaryPath(kind, opt.BinaryPath)
	c.settingsExpandedAg = kind
	c.ensureValidSelectionLocked()
	err := c.writeJSONFile("ai-backends.json", c.backendsCfg)
	shouldProbe := !c.mockResponder
	if _, hasOv := c.statusOverrides[kind]; hasOv {
		shouldProbe = false
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if shouldProbe {
		go func() {
			st := ProbeBackend(kind)
			c.mu.Lock()
			c.backendStatuses[kind] = st
			c.ensureValidSelectionLocked()
			c.mu.Unlock()
			c.refreshModelsAsync(kind)
			c.requestRedraw()
		}()
	}
	c.requestRedraw()
	return nil
}

// UpdateAgentEntry mutates an added local CLI agent's options (such as BinaryPath or Name).
func (c *Controller) UpdateAgentEntry(kind model.BackendKind, mutate func(opt *model.AIBackendAdapterOptions)) error {
	c.mu.Lock()
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	mutate(&opt)
	opt.BinaryPath = strings.TrimSpace(opt.BinaryPath)
	opt.Name = strings.TrimSpace(opt.Name)
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt
	setCustomCLIBinaryPath(kind, opt.BinaryPath)
	err := c.writeJSONFile("ai-backends.json", c.backendsCfg)
	c.mu.Unlock()
	c.requestRedraw()
	return err
}

// RemoveAgentEntry removes a local CLI agent from the added list.
func (c *Controller) RemoveAgentEntry(kind model.BackendKind) error {
	if kind == model.BackendBuiltin {
		return nil
	}
	c.mu.Lock()
	if isBackendEnabled(c.backendsCfg, kind) && !c.canDisableBackendLocked(kind) {
		c.mu.Unlock()
		return errors.New(theme.Tr("至少需要保留一个启用的 AI 后端", "At least one AI backend must remain enabled"))
	}
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	off := false
	opt.Added = false
	opt.Enabled = &off
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt
	if c.settingsExpandedAg == kind {
		c.settingsExpandedAg = ""
	}
	c.ensureValidSelectionLocked()
	err := c.writeJSONFile("ai-backends.json", c.backendsCfg)
	c.mu.Unlock()
	c.requestRedraw()
	return err
}

// SetBackendLocked sets the locked flag for a backend adapter and persists it.
func (c *Controller) SetBackendLocked(kind model.BackendKind, locked bool) error {
	c.mu.Lock()
	prev := cloneBackendsConfig(c.backendsCfg)
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	opt.Locked = locked
	c.backendsCfg.Adapters[string(kind)] = opt
	if err := c.writeJSONFile("ai-backends.json", c.backendsCfg); err != nil {
		c.backendsCfg = prev
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

// PromoteBackend enables kind if needed, promotes it to top priority, and selects it.
func (c *Controller) PromoteBackend(kind model.BackendKind) error {
	c.mu.Lock()
	prev := cloneBackendsConfig(c.backendsCfg)
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	if kind != model.BackendBuiltin {
		opt.Added = true
	}
	on := true
	opt.Enabled = &on
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt

	recognized := c.recognizedBackendsLocked()
	order := c.priorityOrderLocked(recognized)
	nextOrder := make([]model.BackendKind, 0, len(order)+1)
	nextOrder = append(nextOrder, kind)
	for _, b := range order {
		if b != kind {
			nextOrder = append(nextOrder, b)
		}
	}
	c.backendsCfg.Priority = nextOrder
	c.backendsCfg.Selected = kind
	if err := c.writeJSONFile("ai-backends.json", c.backendsCfg); err != nil {
		c.backendsCfg = prev
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

// EnableMockResponder toggles deterministic offline mock responses for tests and smoke runs.
func (c *Controller) EnableMockResponder(on bool) {
	c.mu.Lock()
	c.mockResponder = on
	c.mu.Unlock()
}

// IsMockEnabled reports whether deterministic mock responses are enabled.
func (c *Controller) IsMockEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mockResponder
}

// SetMockApprovalFailOnce configures the next approval submission to fail once before succeeding.
func (c *Controller) SetMockApprovalFailOnce(on bool) {
	c.mu.Lock()
	c.mockApprovalFailOnce = on
	c.mu.Unlock()
}

// NewConversation clears the active conversation into a fresh draft and focuses the composer.
func (c *Controller) NewConversation() {
	c.mu.Lock()
	c.saveTabDraftLocked()
	c.stopStreamingLocked()
	c.clearActivePlanLocked()
	delete(c.tabDrafts, 0)
	c.currentID = 0
	c.draftText = ""
	c.draftRefs = nil
	c.draftSkills = nil
	c.retryDraft = nil
	c.turnError = ""
	c.turnErrorSettings = false
	c.historyOpen = false
	c.pickIndex = 0
	c.pickerDismissed = false
	c.composerEpoch++
	c.composerHandle.Focus()
	c.persistActiveDraftLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

// SelectConversation switches to conversation id while isolating per-conversation drafts.
func (c *Controller) SelectConversation(id uint64) {
	c.mu.Lock()
	if id != 0 && !c.hasConvLocked(id) {
		c.mu.Unlock()
		return
	}
	if c.currentID == id {
		c.historyOpen = false
		c.linkLatestPendingPlanLocked()
		c.mu.Unlock()
		c.requestRedraw()
		return
	}
	c.saveTabDraftLocked()
	c.stopStreamingLocked()
	c.clearActivePlanLocked()
	c.currentID = id
	c.historyOpen = false
	c.turnError = ""
	c.turnErrorSettings = false
	c.pickIndex = 0
	c.pickerDismissed = false
	td := c.tabDrafts[id]
	c.draftText = td.Text
	c.draftRefs = append([]string(nil), td.Refs...)
	c.draftSkills = append([]string(nil), td.Skills...)
	if td.Retry != nil {
		cp := *td.Retry
		c.retryDraft = &cp
	} else {
		c.retryDraft = nil
	}
	c.composerEpoch++
	c.composerHandle.Focus()
	c.persistActiveDraftLocked()
	c.linkLatestPendingPlanLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) saveTabDraftLocked() {
	var retryCopy *model.SentDraft
	if c.retryDraft != nil {
		cp := *c.retryDraft
		retryCopy = &cp
	}
	c.tabDrafts[c.currentID] = tabDraft{
		Text:   c.draftText,
		Refs:   append([]string(nil), c.draftRefs...),
		Skills: append([]string(nil), c.draftSkills...),
		Retry:  retryCopy,
	}
}

// Conversations returns a snapshot slice of stored conversations.
func (c *Controller) Conversations() []model.Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.Conversation, len(c.convs))
	for i := range c.convs {
		out[i] = cloneConversation(c.convs[i])
	}
	return out
}

// CurrentConversation returns a copy of the active conversation, or nil for a blank new chat.
func (c *Controller) CurrentConversation() *model.Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	conv := c.findConvLocked(c.currentID)
	if conv == nil {
		return nil
	}
	cp := cloneConversation(*conv)
	return &cp
}

// ActiveDraftText returns the current composer draft text.
func (c *Controller) ActiveDraftText() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.draftText
}

// SetDraftText updates the active composer draft text and persists it.
func (c *Controller) SetDraftText(s string) {
	c.mu.Lock()
	c.draftText = s
	c.pickerDismissed = false
	c.pickIndex = 0
	c.saveTabDraftLocked()
	c.persistActiveDraftLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

// InjectTestFiles populates the reference picker index with synthetic or test file paths.
func (c *Controller) InjectTestFiles(paths []string) {
	foldersSet := make(map[string]struct{})
	pages := make([]string, 0, len(paths))
	skills := make([]model.SkillSpec, 0)
	seenPages := make(map[string]struct{})

	for _, raw := range paths {
		clean := filepath.ToSlash(strings.TrimSpace(raw))
		clean = strings.TrimPrefix(clean, "./")
		if clean == "" {
			continue
		}
		if strings.HasSuffix(clean, "/") {
			dir := strings.TrimSuffix(clean, "/")
			if dir != "" {
				addFolderAncestors(dir, foldersSet)
			}
			continue
		}
		if dir := pathDirSlash(clean); dir != "" && dir != "." {
			addFolderAncestors(dir, foldersSet)
		}
		page := strings.TrimSuffix(clean, ".md")
		if _, dup := seenPages[page]; !dup {
			seenPages[page] = struct{}{}
			pages = append(pages, page)
		}
		if strings.HasSuffix(page, "/SKILL") || page == "SKILL" {
			name := pathBaseSlash(strings.TrimSuffix(page, "SKILL"))
			if name == "" || name == "." {
				name = page
			}
			skills = append(skills, model.SkillSpec{
				ID:          name,
				Name:        name,
				Description: "Workspace skill",
			})
		}
	}

	folders := make([]string, 0, len(foldersSet))
	for f := range foldersSet {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	sort.Strings(pages)

	c.mu.Lock()
	c.pickerFolders = folders
	c.pickerPages = pages
	c.loadBuiltinSkillsLocked()
	for _, sk := range skills {
		c.upsertSkillLocked(sk)
	}
	c.pickerInjected = true
	c.pickerLoading = false
	c.pickerRefreshed = time.Now()
	c.mu.Unlock()
	c.requestRedraw()
}

func addFolderAncestors(dir string, set map[string]struct{}) {
	for dir != "" && dir != "." {
		set[dir] = struct{}{}
		dir = pathDirSlash(dir)
	}
}

func pathDirSlash(p string) string {
	idx := strings.LastIndexByte(p, '/')
	if idx <= 0 {
		return ""
	}
	return p[:idx]
}

func pathBaseSlash(p string) string {
	p = strings.TrimSuffix(p, "/")
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

// SendPrompt submits text (or the current draft when text is empty) as a user prompt.
func (c *Controller) SendPrompt(text string) {
	c.mu.Lock()
	if c.streaming {
		c.mu.Unlock()
		return
	}
	if text != "" {
		c.draftText = text
	}
	original := c.draftText
	question := strings.TrimSpace(original)
	if question == "" || question == "@" || question == "/" {
		c.mu.Unlock()
		return
	}
	backend := c.selectedBackendLocked()
	if backend == model.BackendBuiltin && !c.mockResponder {
		if msg, needSettings := validateProviderConfig(c.providerCfg); msg != "" {
			c.turnError = msg
			c.turnErrorSettings = needSettings
			c.mu.Unlock()
			c.requestRedraw()
			return
		}
	}

	if c.currentID == 0 {
		delete(c.tabDrafts, 0)
	}

	refs := append([]string(nil), c.draftRefs...)
	skills := append([]string(nil), c.draftSkills...)
	c.draftRefs = nil
	c.draftSkills = nil

	var ctxBuf strings.Builder
	var shownBuf strings.Builder
	for _, r := range refs {
		shownBuf.WriteString("@")
		shownBuf.WriteString(r)
		shownBuf.WriteByte(' ')
		md := c.readPageContentLocked(c.activeRootLocked(), r)
		if md != "" {
			ctxBuf.WriteString(fmt.Sprintf("\n\n# 引用文件：%s（pageId: %s）\n\n%s", r, r, truncateRunes(md, 12000)))
		}
	}
	for _, sk := range skills {
		shownBuf.WriteString("/")
		shownBuf.WriteString(sk)
		shownBuf.WriteByte(' ')
		if spec, ok := c.findSkillLocked(sk); ok && spec.Content != "" {
			ctxBuf.WriteString(fmt.Sprintf("\n\n# 用户调用的 Skill：%s\n\n%s", sk, spec.Content))
		}
	}

	content := question
	if shownBuf.Len() > 0 {
		content = shownBuf.String() + question
	}

	retryMatch := c.retryDraft != nil &&
		c.retryDraft.Conversation == c.currentID &&
		c.retryDraft.Text == original &&
		slicesEqual(c.retryDraft.Refs, refs) &&
		slicesEqual(c.retryDraft.Skills, skills)

	if retryMatch {
		cp := *c.retryDraft
		c.sentDraft = &cp
		c.retryDraft = nil
	} else {
		c.pushMessageLocked(model.ChatMessage{
			Role:    "user",
			Content: content,
			Context: ctxBuf.String(),
			Refs:    refs,
			Skills:  skills,
		})
		conv := c.findConvLocked(c.currentID)
		if conv != nil && len(conv.Messages) > 0 {
			c.sentDraft = &model.SentDraft{
				Text:         original,
				Refs:         refs,
				Skills:       skills,
				Conversation: conv.ID,
				MessageIndex: len(conv.Messages) - 1,
			}
		}
	}

	c.turnError = ""
	c.turnErrorSettings = false
	c.draftText = ""
	c.pickerDismissed = false
	c.pickIndex = 0
	c.saveTabDraftLocked()
	c.persistActiveDraftLocked()

	isMock := c.mockResponder
	convID := c.currentID
	c.mu.Unlock()

	c.requestRedraw()
	if isMock {
		c.startMockTurn(convID, question)
		return
	}
	if backend == model.BackendBuiltin {
		c.startBuiltinTurn(convID, 0)
	} else {
		c.startExternalTurn(convID, backend)
	}
}

// IsStreaming reports whether an AI response turn is currently in progress.
func (c *Controller) IsStreaming() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.streaming
}

// StopStreaming cancels any in-flight AI response and preserves partial output.
func (c *Controller) StopStreaming() {
	c.mu.Lock()
	c.stopStreamingLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) stopStreamingLocked() {
	wasBusy := c.streaming || c.hasPendingQuestionLocked()
	if wasBusy && c.currentID != 0 {
		c.recordTurnOutcomeLocked("stopped")
	}
	c.sentDraft = nil
	if c.activeExternal != nil {
		c.activeExternal.cancel()
		c.activeExternal = nil
	}
	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}
	c.streaming = false
	c.streamSerial++
	c.clearStreamRedrawLocked()
	partial := c.partialText
	c.partialText = ""
	c.partialThink = ""
	c.partialSteps = nil
	if strings.TrimSpace(partial) != "" {
		c.pushMessageLocked(model.ChatMessage{
			Role:    "assistant",
			Content: partial,
		})
	}
}

func (c *Controller) pushMessageLocked(msg model.ChatMessage) {
	if c.currentID == 0 {
		id := uint64(time.Now().UnixMilli())
		if id == 0 {
			id = 1
		}
		for c.hasConvLocked(id) {
			id++
		}
		title := firstTitleLine(msg.Content)
		if title == "" {
			title = theme.Tr("新会话", "New Chat")
		}
		conv := model.Conversation{
			ID:      id,
			Title:   title,
			Backend: c.selectedBackendLocked(),
		}
		c.convs = append([]model.Conversation{conv}, c.convs...)
		c.currentID = id
	}
	if conv := c.findConvLocked(c.currentID); conv != nil {
		conv.Messages = append(conv.Messages, msg)
	}
	c.persistChatsLocked()
}

func (c *Controller) recordTurnOutcomeLocked(outcome string) {
	conv := c.findConvLocked(c.currentID)
	if conv == nil {
		return
	}
	conv.Messages = append(conv.Messages, model.ChatMessage{
		Role:    "turn_outcome",
		Content: outcome,
	})
	c.persistChatsLocked()
}

func (c *Controller) hasPendingQuestionLocked() bool {
	conv := c.findConvLocked(c.currentID)
	if conv == nil {
		return false
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		m := conv.Messages[i]
		if m.Role == "user" {
			break
		}
		if m.Question != nil && !m.Question.Submitted {
			return true
		}
	}
	return false
}

func (c *Controller) restoreFailedDraftLocked() {
	if c.sentDraft == nil {
		return
	}
	draft := *c.sentDraft
	c.sentDraft = nil
	conv := c.findConvLocked(draft.Conversation)
	if c.currentID == draft.Conversation && c.draftText == "" && len(c.draftRefs) == 0 && len(c.draftSkills) == 0 {
		if conv != nil && draft.MessageIndex >= 0 && len(conv.Messages) == draft.MessageIndex+1 {
			conv.Messages = conv.Messages[:len(conv.Messages)-1]
			c.persistChatsLocked()
		} else {
			cp := draft
			c.retryDraft = &cp
		}
		c.draftText = draft.Text
		c.draftRefs = append([]string(nil), draft.Refs...)
		c.draftSkills = append([]string(nil), draft.Skills...)
		c.composerEpoch++
		c.persistActiveDraftLocked()
		return
	}
	c.deferredDrafts = append(c.deferredDrafts, draft)
	c.persistFailedDraftsLocked()
}

func (c *Controller) activeRootLocked() string {
	if c.host == nil {
		return ""
	}
	if root := strings.TrimSpace(c.host.ActiveNotebook()); root != "" {
		return root
	}
	if nbs := c.host.Notebooks(); len(nbs) > 0 {
		return nbs[0]
	}
	return ""
}

func (c *Controller) activePageLocked(root string) string {
	if c.host == nil {
		return ""
	}
	p := strings.TrimSpace(c.host.ActiveNotePath())
	if p == "" {
		return ""
	}
	if root != "" {
		if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		}
	}
	return strings.TrimSuffix(filepath.Base(p), ".md")
}

func (c *Controller) readPageContentLocked(root, page string) string {
	if page == "" {
		return ""
	}
	abs := page
	if !filepath.IsAbs(abs) && root != "" {
		abs = filepath.Join(root, filepath.FromSlash(page)+".md")
	}
	if c.host != nil {
		if txt, err := c.host.ReadNote(abs); err == nil {
			return txt
		}
	}
	if abs != "" {
		if b, err := os.ReadFile(abs); err == nil {
			return string(b)
		}
	}
	return ""
}

func (c *Controller) loadBuiltinSkillsLocked() {
	builtins := []struct {
		id   string
		file string
		desc string
	}{
		{"createNoote", "createNoote.md", "在本轮确定的笔记本和目录新建页面"},
		{"updateNote", "updateNote.md", "改写、追加、补充或重命名当前页"},
		{"searchNotes", "searchNotes.md", "跨笔记本和目录搜索并读取页面"},
		{"chat", "chat.md", "仅解释或建议，不改笔记"},
		{"markdown", "markdown.md", "创建或修改笔记时的 Markdown 指引"},
	}
	c.pickerSkills = nil
	for _, b := range builtins {
		content := assets.ReadSkill(b.file)
		c.pickerSkills = append(c.pickerSkills, model.SkillSpec{
			ID:          b.id,
			Name:        b.id,
			Description: b.desc,
			Content:     content,
		})
	}
}

func (c *Controller) upsertSkillLocked(spec model.SkillSpec) {
	for i := range c.pickerSkills {
		if c.pickerSkills[i].ID == spec.ID || c.pickerSkills[i].Name == spec.Name {
			c.pickerSkills[i] = spec
			return
		}
	}
	c.pickerSkills = append(c.pickerSkills, spec)
}

func (c *Controller) findSkillLocked(name string) (model.SkillSpec, bool) {
	for _, s := range c.pickerSkills {
		if s.ID == name || s.Name == name {
			return s, true
		}
	}
	return model.SkillSpec{}, false
}

func (c *Controller) refreshPickerIfNeeded() {
	c.mu.Lock()
	if c.pickerInjected || c.pickerLoading || c.host == nil {
		c.mu.Unlock()
		return
	}
	root := c.activeRootLocked()
	if root == "" {
		c.mu.Unlock()
		return
	}
	if c.pickerRoot == root && time.Since(c.pickerRefreshed) < 5*time.Second {
		c.mu.Unlock()
		return
	}
	c.pickerLoading = true
	c.pickerRoot = root
	host := c.host
	c.mu.Unlock()

	go func() {
		entries, err := host.ListNotebookFiles(root)
		foldersSet := make(map[string]struct{})
		var pages []string
		var skills []model.SkillSpec

		var walk func(list []model.FileEntry)
		walk = func(list []model.FileEntry) {
			for _, ent := range list {
				rel, rerr := filepath.Rel(root, ent.Path)
				if rerr != nil {
					continue
				}
				slash := filepath.ToSlash(rel)
				if ent.IsDir {
					foldersSet[slash] = struct{}{}
					walk(ent.Children)
				} else if ent.IsMarkdown || strings.HasSuffix(strings.ToLower(ent.RawName), ".md") {
					page := strings.TrimSuffix(slash, ".md")
					pages = append(pages, page)
					if strings.HasSuffix(page, "/SKILL") || page == "SKILL" {
						if txt, err := host.ReadNote(ent.Path); err == nil {
							skills = append(skills, parseSkillMarkdown(page, txt))
						}
					}
				}
			}
		}
		if err == nil {
			walk(entries)
		}
		folders := make([]string, 0, len(foldersSet))
		for f := range foldersSet {
			folders = append(folders, f)
		}
		sort.Strings(folders)
		sort.Strings(pages)

		c.mu.Lock()
		if !c.pickerInjected && c.pickerRoot == root {
			c.pickerFolders = folders
			c.pickerPages = pages
			c.loadBuiltinSkillsLocked()
			for _, sk := range skills {
				c.upsertSkillLocked(sk)
			}
			c.pickerRefreshed = time.Now()
		}
		c.pickerLoading = false
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func parseSkillMarkdown(page, body string) model.SkillSpec {
	name := pathBaseSlash(strings.TrimSuffix(page, "SKILL"))
	if name == "" || name == "." {
		name = page
	}
	desc := ""
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(trim, "name:"); ok {
			if clean := strings.Trim(strings.TrimSpace(v), `"`); clean != "" {
				name = clean
			}
		} else if v, ok := strings.CutPrefix(trim, "description:"); ok {
			desc = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return model.SkillSpec{ID: name, Name: name, Description: desc, Content: body}
}

func firstTitleLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, ">") {
			continue
		}
		return truncateRunes(trim, 24)
	}
	return ""
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneProviderConfig(cfg model.AIProviderConfig) model.AIProviderConfig {
	cp := cfg
	cp.ProviderOrder = append([]string(nil), cfg.ProviderOrder...)
	if cfg.Providers != nil {
		cp.Providers = make(map[string]model.ProviderProfile, len(cfg.Providers))
		for k, v := range cfg.Providers {
			if v.Enabled != nil {
				b := *v.Enabled
				v.Enabled = &b
			}
			cp.Providers[k] = v
		}
	}
	return cp
}

func cloneBackendsConfig(cfg model.AIBackendsConfig) model.AIBackendsConfig {
	cp := cfg
	cp.Priority = append([]model.BackendKind(nil), cfg.Priority...)
	if cfg.Adapters != nil {
		cp.Adapters = make(map[string]model.AIBackendAdapterOptions, len(cfg.Adapters))
		for k, v := range cfg.Adapters {
			if v.Enabled != nil {
				b := *v.Enabled
				v.Enabled = &b
			}
			cp.Adapters[k] = v
		}
	}
	return cp
}

func cloneConversation(conv model.Conversation) model.Conversation {
	cp := conv
	if conv.Messages != nil {
		cp.Messages = make([]model.ChatMessage, len(conv.Messages))
		for i, m := range conv.Messages {
			cp.Messages[i] = m
			if m.Process != nil {
				cp.Messages[i].Process = append([]model.ProcessStep(nil), m.Process...)
			}
			if m.Sources != nil {
				cp.Messages[i].Sources = append([]model.SourceNote(nil), m.Sources...)
			}
			if m.Refs != nil {
				cp.Messages[i].Refs = append([]string(nil), m.Refs...)
			}
			if m.Skills != nil {
				cp.Messages[i].Skills = append([]string(nil), m.Skills...)
			}
			if m.Plan != nil {
				p := *m.Plan
				p.Ops = append([]model.PlanOp(nil), m.Plan.Ops...)
				if m.Plan.Expected != nil {
					p.Expected = make(map[string]string, len(m.Plan.Expected))
					for k, v := range m.Plan.Expected {
						p.Expected[k] = v
					}
				}
				cp.Messages[i].Plan = &p
			}
		}
	}
	return cp
}
