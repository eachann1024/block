package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"block/assets"
	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

const (
	defaultPanelWidth     float32 = theme.AIPanelDefaultWidth
	minPanelWidth         float32 = theme.AIPanelMinWidth
	maxPanelWidth         float32 = theme.AIPanelMaxWidth
	streamRedrawInterval          = 16 * time.Millisecond
	draftPersistDelay             = 300 * time.Millisecond
	chatPersistDelay              = 400 * time.Millisecond
	pickerRefreshCooldown         = 30 * time.Second
)

type tabDraft struct {
	Text        string
	Refs        []string
	Skills      []string
	Attachments []model.Attachment
	Retry       *model.SentDraft
}

// EditorSelection holds a snapshot of the last non-empty editor selection attached to the composer.
type EditorSelection struct {
	NotePath     string
	Markdown     string
	PlainPreview string
	StartLine    int // 1-based inclusive start block index
	EndLine      int // 1-based inclusive end block index
	AnchorBlock  int
	AnchorOffset int
	HeadBlock    int
	HeadOffset   int
}

type selectionIdentity struct {
	notePath     string
	anchorBlock  int
	anchorOffset int
	headBlock    int
	headOffset   int
}

func (s EditorSelection) identity() selectionIdentity {
	return selectionIdentity{
		notePath:     s.NotePath,
		anchorBlock:  s.AnchorBlock,
		anchorOffset: s.AnchorOffset,
		headBlock:    s.HeadBlock,
		headOffset:   s.HeadOffset,
	}
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
	root  string
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

type convRunState struct {
	streaming         bool
	serial            uint64
	turn              *assistantTurn
	partialText       strings.Builder
	partialThink      strings.Builder
	partialSteps      []model.ProcessStep
	sentDraft         *model.SentDraft
	sentSelection     *EditorSelection
	turnError         string
	turnErrorSettings bool
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

	providerCfg   model.AIProviderConfig
	providerError error
	backendsCfg   model.AIBackendsConfig

	convs          []model.Conversation
	currentID      uint64
	activeNotebook string
	notebookStates map[string]model.NotebookSessionState

	draftText        string
	draftRefs        []string
	draftSkills      []string
	draftAttachments []model.Attachment
	draftSelection   *EditorSelection
	dismissedSel     *selectionIdentity
	retryDraft       *model.SentDraft
	deferredDrafts   []model.SentDraft
	tabDrafts        map[uint64]tabDraft

	composerEpoch       int
	composerHandle      ui.Handle
	composerComposing   bool
	composerSelectEnd   bool
	messagesList        ui.ListState
	messagesScroll      ui.ScrollState
	scrollToBottom      bool
	scrollToBottomForce bool
	lastScrollY         float32
	lastScrollMaxY      float32
	pickerList          ui.ListState
	mdOpts              markdown.Options
	mdThemeKey          mdPaletteKey
	streamCache         *chatStreamCache

	pickIndex        int
	pickerDismissed  bool
	pickerFolder     string
	pickerFolders    []string
	pickerPages      []string
	pickerSkills     []model.SkillSpec
	pickerLoading    bool
	pickerInjected   bool
	pickerRoot       string
	pickerRefreshed  time.Time
	pickerRootMtime  time.Time
	pickerRefreshGen uint64
	pickerSkillCache map[string]cachedSkillEntry
	pickerCache      pickerItemsCache

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

	runs                  map[uint64]*convRunState
	streamSerial          uint64
	streamRedrawLast      time.Time
	streamRedrawTimer     *time.Timer
	draftPersistTimer     *time.Timer
	chatPersistMu         sync.Mutex
	chatPersistTimer      *time.Timer
	chatPersistPending    []model.Conversation
	chatPersistDirty      bool
	chatPersistSeq        uint64
	chatPersistWrittenSeq uint64

	mockResponder        bool
	mockApprovalFailOnce bool
	onDismissFloating    func()
	onPickAttachments    func()
	onRevealPath         func(path string)

	mediaCache *attachmentMediaCache

	indicatorWasStreaming bool
	indicatorGlintAt      time.Time
	lightboxOpen          bool              // synchronized under c.mu for cross-thread queries
	lightbox              bui.LightboxState // UI-thread-only persistent state (stable Scroll pointer)
	lightboxSource        attachmentLightboxSource
	clipSeq               uint64
	pendingClip           *pendingAttachmentClip
	onEnabledChanged      func(bool)

	activePlan     *ActivePlanPreview
	planInFlight   string
	planSelections map[planSelKey]*diffSelection

	expandedProcess     map[string]bool
	expandedStepDetails map[string]bool
	expandedNotes       map[string]bool
	questionUI          map[string]*questionUIState
	approvalUI          map[string]*approvalUIState

	settingsFocused       model.BackendKind
	settingsExpandedProv  string
	settingsExpandedAg    model.BackendKind
	settingsProbedOnce    bool
	settingsShowKey       bool
	settingsProvShowKey   map[string]bool
	settingsAgentProbing  map[model.BackendKind]bool
	settingsAgentProbeOK  map[model.BackendKind]bool
	settingsAgentProbeErr map[model.BackendKind]string
	settingsAgentProbeSeq map[model.BackendKind]uint64
	settingsTesting       bool
	settingsTestingProv   string
	settingsTestOK        bool
	settingsTestErr       string
	settingsModelStatus   string
	settingsBaseURL       string
	settingsAPIKey        string
	settingsModel         string

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
	deepAI              bool
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
		dataDir:               dataDir,
		host:                  host,
		codec:                 codec,
		panelWidth:            defaultPanelWidth,
		notebookStates:        make(map[string]model.NotebookSessionState),
		tabDrafts:             make(map[uint64]tabDraft),
		runs:                  make(map[uint64]*convRunState),
		backendStatuses:       make(map[model.BackendKind]BackendStatus),
		backendModels:         make(map[model.BackendKind][]ModelOption),
		modelStatus:           make(map[model.BackendKind]string),
		modelRefreshed:        make(map[model.BackendKind]time.Time),
		modelGeneration:       make(map[model.BackendKind]uint64),
		statusOverrides:       make(map[model.BackendKind]BackendStatus),
		planSelections:        make(map[planSelKey]*diffSelection),
		expandedProcess:       make(map[string]bool),
		expandedStepDetails:   make(map[string]bool),
		expandedNotes:         make(map[string]bool),
		questionUI:            make(map[string]*questionUIState),
		approvalUI:            make(map[string]*approvalUIState),
		settingsFocused:       model.BackendBuiltin,
		settingsProvShowKey:   make(map[string]bool),
		settingsAgentProbing:  make(map[model.BackendKind]bool),
		settingsAgentProbeOK:  make(map[model.BackendKind]bool),
		settingsAgentProbeErr: make(map[model.BackendKind]string),
		settingsAgentProbeSeq: make(map[model.BackendKind]uint64),
	}
	ctrl.backendStatuses[model.BackendBuiltin] = BackendStatus{
		State:   BackendStateReady,
		Version: "Block",
	}
	needAnnotRefresh := ctrl.loadAllFromDisk()
	ctrl.settingsExpandedProv = ctrl.providerCfg.Provider
	ctrl.syncSettingsFieldsLocked()
	ctrl.loadBuiltinSkillsLocked()
	ctrl.messagesList.FollowEnd = true
	if ctrl.providerCfg.Enabled && !ctrl.mockResponder && needAnnotRefresh != "" {
		ctrl.refreshAnnotationModelsAsync(needAnnotRefresh)
	}
	return ctrl
}

func (c *Controller) loadAllFromDisk() model.BackendKind {
	cfg, err := loadProviderConfig(c.dataDir)
	c.providerCfg = normalizeProviderConfig(cfg)
	c.providerError = err
	c.backendsCfg = c.readBackendsFile()
	c.activeNotebook = normalizeNotebookRoot(c.activeRootLocked())
	c.convs = c.readChatsFile()
	c.deferredDrafts = c.readFailedDraftsFile()

	active := c.readActiveDraftFile()
	if c.inferLegacyConversationNotebooksLocked(active.ByNotebook) {
		c.persistChatsLocked()
	}
	c.notebookStates = make(map[string]model.NotebookSessionState)
	for rawKey, st := range active.ByNotebook {
		key := normalizeNotebookRoot(rawKey)
		if key == "" {
			continue
		}
		c.notebookStates[key] = c.sanitizeNotebookStateLocked(key, st)
	}
	legacyTop := model.NotebookSessionState{
		Conversation: active.Conversation,
		Text:         active.Text,
		Refs:         append([]string(nil), active.Refs...),
		Skills:       append([]string(nil), active.Skills...),
		Attachments:  slices.Clone(active.Attachments),
		Retry:        cloneSentDraft(active.Retry),
	}
	if active.Conversation != nil {
		if conv := c.findConvLocked(*active.Conversation); conv != nil && conv.Notebook != "" {
			if _, exists := c.notebookStates[conv.Notebook]; !exists {
				c.notebookStates[conv.Notebook] = c.sanitizeNotebookStateLocked(conv.Notebook, legacyTop)
			}
		}
	}
	if c.activeNotebook != "" {
		if st, ok := c.notebookStates[c.activeNotebook]; ok {
			c.applyNotebookStateLocked(st, c.activeNotebook)
		} else if active.Conversation == nil && len(active.ByNotebook) == 0 {
			c.applyNotebookStateLocked(legacyTop, c.activeNotebook)
		}
	} else {
		c.applyNotebookStateLocked(legacyTop, "")
	}
	c.ensureValidSelectionLocked()
	c.loadDeepAIConfigLocked()
	return c.loadOrSeedAnnotationConfigLocked()
}

func (c *Controller) loadOrSeedAnnotationConfigLocked() model.BackendKind {
	var cfg model.AnnotationConfig
	if c.readJSONFile("ai-annotations.json", &cfg) && cfg.Backend != "" {
		if cfg.Thinking == "" {
			cfg.Thinking = "high"
		}
		c.annotCfg = cfg
		c.annotMenuBackend = cfg.Backend
		_, needRefresh := c.ensureValidAnnotationConfigLocked()
		return needRefresh
	}
	sel := c.selectedBackendLocked()
	mdl := c.backendModelLocked(sel)
	prov := ""
	if sel == model.BackendBuiltin {
		if entry, ok := latestEnabledProviderEntry(c.providerCfg); ok {
			prov = entry.ID
			if mdl == "" {
				mdl = entry.Model
			}
			if mdl != "" {
				mdl = formatProviderModelRef(entry.ID, mdl)
			}
		} else {
			prov = c.providerCfg.Provider
			if prov == "" {
				prov = "deepseek"
			}
		}
	}
	c.annotCfg = model.AnnotationConfig{
		Backend:  sel,
		Provider: prov,
		Model:    mdl,
		Thinking: c.backendThinkingLocked(sel),
	}
	c.annotMenuBackend = sel
	_ = c.writeJSONFile("ai-annotations.json", c.annotCfg)
	_, needRefresh := c.ensureValidAnnotationConfigLocked()
	return needRefresh
}

func (c *Controller) pickBuiltinAnnotationModelLocked(entry ProviderEntry) (string, bool) {
	normPrimary := normalizeProviderConfig(c.providerCfg).Provider
	cached := c.backendModels[model.BackendBuiltin]
	var firstPure string
	matchedEntryModel := false
	_, pureEntryModel := splitProviderModelRef(strings.TrimSpace(entry.Model))
	for _, opt := range cached {
		optProv, optPure := splitProviderModelRef(opt.Value)
		if optProv == "" {
			optProv = normPrimary
		}
		if optProv != entry.ID || strings.TrimSpace(optPure) == "" {
			continue
		}
		if firstPure == "" {
			firstPure = optPure
		}
		if pureEntryModel != "" && optPure == pureEntryModel {
			matchedEntryModel = true
		}
	}
	if pureEntryModel != "" {
		if firstPure == "" || matchedEntryModel {
			return formatProviderModelRef(entry.ID, pureEntryModel), false
		}
		return formatProviderModelRef(entry.ID, firstPure), false
	}
	if firstPure != "" {
		return formatProviderModelRef(entry.ID, firstPure), false
	}
	return "", true
}

func (c *Controller) ensureValidAnnotationConfigLocked() (changed bool, needRefreshKind model.BackendKind) {
	next := c.annotCfg
	if next.Thinking == "" {
		next.Thinking = "high"
	}

	targetBackend := next.Backend
	backendChanged := false
	if targetBackend == "" || !c.isBackendAvailableLocked(targetBackend) {
		targetBackend = c.latestAvailableBackendLocked()
		backendChanged = true
	}

	if targetBackend == model.BackendBuiltin {
		targetProv := ""
		if !backendChanged {
			provFromModel, _ := splitProviderModelRef(strings.TrimSpace(next.Model))
			targetProv = strings.TrimSpace(next.Provider)
			if provFromModel != "" {
				targetProv = provFromModel
			}
		}
		entry, ok := findEnabledProviderEntry(c.providerCfg, targetProv)
		if backendChanged || !ok {
			entry, ok = latestEnabledProviderEntry(c.providerCfg)
			if !ok {
				for _, k := range model.AllBackends {
					if k != model.BackendBuiltin && c.isBackendAvailableLocked(k) {
						targetBackend = k
						backendChanged = true
						break
					}
				}
				if targetBackend == model.BackendBuiltin {
					next.Backend = model.BackendBuiltin
					next.Provider = ""
					next.Model = ""
				}
			} else {
				next.Backend = model.BackendBuiltin
				next.Provider = entry.ID
				mdl, needRef := c.pickBuiltinAnnotationModelLocked(entry)
				next.Model = mdl
				if needRef {
					needRefreshKind = model.BackendBuiltin
				}
			}
		} else {
			next.Backend = model.BackendBuiltin
			next.Provider = entry.ID
			if strings.TrimSpace(next.Model) == "" {
				mdl, needRef := c.pickBuiltinAnnotationModelLocked(entry)
				next.Model = mdl
				if needRef {
					needRefreshKind = model.BackendBuiltin
				}
			} else {
				_, pure := splitProviderModelRef(strings.TrimSpace(next.Model))
				next.Model = formatProviderModelRef(entry.ID, pure)
			}
		}
	}

	if targetBackend != model.BackendBuiltin {
		next.Backend = targetBackend
		next.Provider = ""
		if backendChanged || strings.TrimSpace(next.Model) == "" {
			mdl := strings.TrimSpace(c.backendModelLocked(targetBackend))
			if mdl == "" {
				if opts := c.backendModels[targetBackend]; len(opts) > 0 {
					mdl = opts[0].Value
				}
			}
			if mdl == "" {
				needRefreshKind = targetBackend
			}
			next.Model = mdl
		}
	}

	if next != c.annotCfg {
		changed = true
		_ = c.saveAnnotationConfigLocked(next)
	}
	if c.annotMenuBackend == "" || !c.isBackendAvailableLocked(c.annotMenuBackend) || changed {
		c.annotMenuBackend = c.annotCfg.Backend
	}
	return changed, needRefreshKind
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

func configureBackendBinaries(cfg *model.AIBackendsConfig) {
	if cfg.Adapters == nil {
		cfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	for _, k := range model.AllBackends {
		if k == model.BackendBuiltin {
			continue
		}
		setCustomCLIBinaryPath(k, cfg.Adapters[string(k)].BinaryPath)
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
	configureBackendBinaries(&cfg)
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

func (c *Controller) snapshotChatsLocked() []model.Conversation {
	keep := make([]model.Conversation, 0, len(c.convs))
	for _, conv := range c.convs {
		if len(conv.Messages) > 0 {
			keep = append(keep, cloneConversation(conv))
		}
	}
	return keep
}

func (c *Controller) persistChatsLocked() {
	snap := c.snapshotChatsLocked()
	c.chatPersistSeq++
	seq := c.chatPersistSeq
	if testing.Testing() {
		c.chatPersistDirty = false
		c.chatPersistPending = nil
		if c.chatPersistTimer != nil {
			c.chatPersistTimer.Stop()
			c.chatPersistTimer = nil
		}
		c.writeChatsSnapshot(snap, seq)
		return
	}
	c.chatPersistPending = snap
	c.chatPersistDirty = true
	if c.chatPersistTimer == nil {
		c.chatPersistTimer = time.AfterFunc(chatPersistDelay, c.flushPendingChatsAsync)
	}
}

func (c *Controller) flushPendingChatsAsync() {
	c.mu.Lock()
	c.chatPersistTimer = nil
	if !c.chatPersistDirty {
		c.mu.Unlock()
		return
	}
	snap := c.chatPersistPending
	seq := c.chatPersistSeq
	c.chatPersistPending = nil
	c.chatPersistDirty = false
	c.mu.Unlock()

	c.writeChatsSnapshot(snap, seq)
}

func (c *Controller) writeChatsSnapshot(snap []model.Conversation, seq uint64) {
	c.chatPersistMu.Lock()
	defer c.chatPersistMu.Unlock()
	if seq < c.chatPersistWrittenSeq {
		return
	}
	_ = c.writeJSONFile("ai-chats.json", snap)
	c.chatPersistWrittenSeq = seq
}

// Flush synchronously persists any pending conversation or active draft writes to disk.
func (c *Controller) Flush() {
	c.mu.Lock()
	if c.chatPersistTimer != nil {
		c.chatPersistTimer.Stop()
		c.chatPersistTimer = nil
	}
	needChats := c.chatPersistDirty
	var snap []model.Conversation
	var seq uint64
	if needChats {
		snap = c.chatPersistPending
		seq = c.chatPersistSeq
		c.chatPersistPending = nil
		c.chatPersistDirty = false
	}
	if c.draftPersistTimer != nil {
		c.cancelDraftPersistLocked()
		c.writeActiveDraftLocked()
	}
	c.mu.Unlock()
	if needChats {
		c.writeChatsSnapshot(snap, seq)
	}
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
	curState := c.currentNotebookStateLocked()
	if c.activeNotebook != "" {
		c.notebookStates[c.activeNotebook] = curState
	}
	var byNotebook map[string]model.NotebookSessionState
	if len(c.notebookStates) > 0 {
		byNotebook = make(map[string]model.NotebookSessionState, len(c.notebookStates))
		for k, st := range c.notebookStates {
			byNotebook[k] = cloneNotebookSessionState(st)
		}
	}
	draft := model.ActiveDraft{
		Conversation: curState.Conversation,
		Text:         curState.Text,
		Refs:         append([]string(nil), curState.Refs...),
		Skills:       append([]string(nil), curState.Skills...),
		Attachments:  slices.Clone(curState.Attachments),
		Retry:        cloneSentDraft(curState.Retry),
		ByNotebook:   byNotebook,
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

// SetOnPickAttachments registers a callback that opens a native file picker
// and adds chosen files into the AI composer draft.
func (c *Controller) SetOnPickAttachments(fn func()) {
	c.mu.Lock()
	c.onPickAttachments = fn
	c.mu.Unlock()
}

// SetOnRevealPath registers a callback that reveals a cached attachment in Finder / File Manager.
func (c *Controller) SetOnRevealPath(fn func(path string)) {
	c.mu.Lock()
	c.onRevealPath = fn
	c.mu.Unlock()
}

// ContainsPoint reports whether a window-space point falls inside the rendered AI panel.
func (c *Controller) ContainsPoint(x, y float32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.open || !c.providerCfg.Enabled {
		return false
	}
	if c.fullscreen {
		return true
	}
	b := c.panelBox
	if b.W <= 0 || b.H <= 0 {
		return false
	}
	return x >= b.X && x <= b.X+b.W && y >= b.Y && y <= b.Y+b.H
}

// HasActiveOverlay reports whether the AI panel has an open popup (history, model deck,
// attachment lightbox, or @/slash picker) that should keep a floating edge card alive while interacting.
func (c *Controller) HasActiveOverlay() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, pickerOpen := activePickerToken(c.draftText, c.composerComposing, c.pickerDismissed)
	return c.historyOpen || c.deckOpen || c.lightboxOpen || pickerOpen
}

// IsLightboxOpen reports whether the full-window AI attachment lightbox is currently open
// and its owning AI panel is active and enabled.
func (c *Controller) IsLightboxOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lightboxOpen && c.open && c.providerCfg.Enabled
}

// CloseTransientPopups closes transient AI header/composer popups.
func (c *Controller) CloseTransientPopups() {
	c.mu.Lock()
	c.historyOpen = false
	c.deckOpen = false
	c.lightboxOpen = false
	c.pickerDismissed = true
	c.mu.Unlock()
}

// SetOpen sets the AI panel open state.
func (c *Controller) SetOpen(open bool) {
	c.mu.Lock()
	if open && !c.providerCfg.Enabled {
		c.mu.Unlock()
		return
	}
	dismissFn := c.onDismissFloating
	if c.open == open {
		if !open {
			c.historyOpen = false
			c.deckOpen = false
			c.lightboxOpen = false
			c.clipSeq++
			c.pendingClip = nil
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
		c.lightboxOpen = false
		c.clipSeq++
		c.pendingClip = nil
		c.draftSelection = nil
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
	if !c.providerCfg.Enabled {
		c.mu.Unlock()
		return
	}
	next := !c.open
	c.open = next
	if !next {
		c.historyOpen = false
		c.deckOpen = false
		c.lightboxOpen = false
		c.clipSeq++
		c.pendingClip = nil
		c.draftSelection = nil
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
	if c.fullscreen == fs || (fs && !c.providerCfg.Enabled) {
		c.mu.Unlock()
		return
	}
	c.fullscreen = fs
	if fs {
		c.draftSelection = nil
	}
	c.mu.Unlock()
	c.requestRedraw()
}

// ToggleFullscreen toggles fullscreen mode for the AI panel.
func (c *Controller) ToggleFullscreen() {
	c.mu.Lock()
	if !c.providerCfg.Enabled {
		c.mu.Unlock()
		return
	}
	if !c.open {
		c.open = true
		c.fullscreen = true
	} else {
		c.fullscreen = !c.fullscreen
	}
	if c.fullscreen {
		c.draftSelection = nil
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

// SetEditorSelection updates the composer's attached editor selection snapshot
// unless the same selection (note, anchor, and head) was dismissed with ×.
func (c *Controller) SetEditorSelection(sel *EditorSelection) {
	if sel == nil || strings.TrimSpace(sel.Markdown) == "" || sel.StartLine <= 0 || sel.EndLine < sel.StartLine {
		c.ClearEditorSelection()
		return
	}
	cp := *sel
	id := cp.identity()

	c.mu.Lock()
	if c.dismissedSel != nil && *c.dismissedSel == id {
		changed := c.draftSelection != nil
		c.draftSelection = nil
		c.mu.Unlock()
		if changed {
			c.requestRedraw()
		}
		return
	}
	c.dismissedSel = nil
	if c.draftSelection != nil && *c.draftSelection == cp {
		c.mu.Unlock()
		return
	}
	c.draftSelection = &cp
	c.mu.Unlock()
	c.requestRedraw()
}

// ClearEditorSelection clears the attached selection snapshot and any sticky dismissal.
func (c *Controller) ClearEditorSelection() {
	c.mu.Lock()
	changed := c.draftSelection != nil
	c.draftSelection = nil
	c.dismissedSel = nil
	c.mu.Unlock()
	if changed {
		c.requestRedraw()
	}
}

// HideEditorSelection clears the attached selection snapshot while preserving sticky dismissal
// (used when the AI sidebar is temporarily hidden, fullscreen, in settings, or disabled).
func (c *Controller) HideEditorSelection() {
	c.mu.Lock()
	changed := c.draftSelection != nil
	c.draftSelection = nil
	c.mu.Unlock()
	if changed {
		c.requestRedraw()
	}
}

// DismissEditorSelection removes the current selection chip and marks its identity sticky-dismissed.
func (c *Controller) DismissEditorSelection() {
	c.mu.Lock()
	if c.draftSelection == nil {
		c.mu.Unlock()
		return
	}
	id := c.draftSelection.identity()
	c.dismissedSel = &id
	c.draftSelection = nil
	c.mu.Unlock()
	c.requestRedraw()
}

// Enabled reports the master AI switch, independent of backend readiness.
func (c *Controller) Enabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.providerCfg.Enabled
}

// SetOnEnabledChanged registers the workspace handler for changes to the master switch.
func (c *Controller) SetOnEnabledChanged(fn func(bool)) {
	c.mu.Lock()
	c.onEnabledChanged = fn
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
	norm := normalizeProviderConfig(cloneProviderConfig(cfg))
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
	c.mu.Lock()
	if err := c.saveProviderConfigLocked(norm); err != nil {
		c.mu.Unlock()
		c.requestRedraw()
		return err
	}
	enabledChanged := c.providerCfg.Enabled != norm.Enabled
	c.providerCfg = norm
	if enabledChanged && !norm.Enabled {
		c.open = false
		c.fullscreen = false
		c.historyOpen = false
		c.deckOpen = false
		c.lightboxOpen = false
		c.clipSeq++
		c.pendingClip = nil
		c.pickerDismissed = true
		c.draftSelection = nil
		c.stopAllStreamingLocked()
	}
	onEnabledChanged := c.onEnabledChanged
	if c.settingsExpandedProv == "" {
		c.settingsExpandedProv = norm.Provider
	}
	c.syncSettingsFieldsLocked()
	for _, st := range c.runs {
		if st != nil && st.turnErrorSettings {
			st.turnError = ""
			st.turnErrorSettings = false
		}
	}
	c.invalidateModelCacheLocked(model.BackendBuiltin)
	_, needAnnotRefresh := c.ensureValidAnnotationConfigLocked()
	shouldPrewarm := enabledChanged && norm.Enabled && !c.mockResponder
	shouldRefresh := !shouldPrewarm && norm.Enabled && (c.panelProbedOnce || c.settingsProbedOnce) && !c.mockResponder
	c.mu.Unlock()
	if enabledChanged && onEnabledChanged != nil {
		onEnabledChanged(norm.Enabled)
	}
	if shouldPrewarm {
		c.PrewarmModelsAsync()
	} else if shouldRefresh {
		c.refreshModelsAsync(model.BackendBuiltin)
	} else if needAnnotRefresh != "" && norm.Enabled && !c.mockResponder {
		c.refreshAnnotationModelsAsync(needAnnotRefresh)
	}
	c.requestRedraw()
	return nil
}

func (c *Controller) saveProviderConfigLocked(cfg model.AIProviderConfig) error {
	// A controller that could not load its credentials must not save its
	// incomplete in-memory configuration over a recoverable credential record.
	if c.providerError != errProviderCredentialsRead {
		c.providerError = saveProviderConfig(c.dataDir, cfg)
	}
	if c.providerError != nil {
		return errors.New(providerCredentialsMessage(c.providerError))
	}
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
			trimmedName = theme.Tr(p.Zh, p.En)
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
	latestEnabled, hasLatest := latestEnabledProviderEntry(updated)
	if on {
		_ = c.ActivateProviderEntry(id)
		_ = c.SetBackendEnabled(model.BackendBuiltin, true)
	} else if !hasLatest {
		_ = c.SetBackendEnabled(model.BackendBuiltin, false)
	} else if updated.Provider == id {
		_ = c.ActivateProviderEntry(latestEnabled.ID)
	}
	return nil
}

// RemoveProviderEntry deletes a provider entry from ai.json.
func (c *Controller) RemoveProviderEntry(id string) error {
	cfg := c.ProviderConfig()
	entries := orderedProviderEntries(cfg)
	if len(entries) <= 1 {
		return nil
	}
	delete(cfg.Providers, id)
	cfg.ProviderOrder = removeString(append([]string(nil), cfg.ProviderOrder...), id)
	// The active fields still describe the removed provider until a replacement
	// is selected. Do not let orderedProviderEntries reconstruct it from them.
	remaining := slices.DeleteFunc(entries, func(entry ProviderEntry) bool { return entry.ID == id })
	anyEnabled := false
	for _, r := range remaining {
		if r.Enabled {
			anyEnabled = true
			break
		}
	}
	if cfg.Provider == id && len(remaining) > 0 {
		pick := remaining[len(remaining)-1]
		for i := len(remaining) - 1; i >= 0; i-- {
			if remaining[i].Enabled {
				pick = remaining[i]
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
	if !anyEnabled {
		_ = c.SetBackendEnabled(model.BackendBuiltin, false)
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
	configureBackendBinaries(&c.backendsCfg)
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
	c.backendsCfg.Adapters[string(kind)] = opt
	c.ensureValidSelectionLocked()
	_, needAnnotRefresh := c.ensureValidAnnotationConfigLocked()
	if err := c.writeJSONFile("ai-backends.json", c.backendsCfg); err != nil {
		c.backendsCfg = prev
		c.mu.Unlock()
		return err
	}
	_, hasOv := c.statusOverrides[kind]
	shouldRefresh := on && c.providerCfg.Enabled && !c.mockResponder && !hasOv && (c.panelProbedOnce || c.settingsProbedOnce)
	canRefreshAnnot := needAnnotRefresh != "" && c.providerCfg.Enabled && !c.mockResponder && (!shouldRefresh || needAnnotRefresh != kind)
	c.mu.Unlock()
	if shouldRefresh {
		c.refreshModelsAsync(kind)
	}
	if canRefreshAnnot {
		c.refreshAnnotationModelsAsync(needAnnotRefresh)
	}
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
			_, _ = c.ensureValidAnnotationConfigLocked()
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
	previous := c.backendsCfg.Adapters[string(kind)]
	opt := previous
	mutate(&opt)
	opt.BinaryPath = strings.TrimSpace(opt.BinaryPath)
	opt.Name = strings.TrimSpace(opt.Name)
	c.backendsCfg.Adapters[string(kind)] = opt
	if err := c.writeJSONFile("ai-backends.json", c.backendsCfg); err != nil {
		c.backendsCfg.Adapters[string(kind)] = previous
		c.mu.Unlock()
		return err
	}
	setCustomCLIBinaryPath(kind, opt.BinaryPath)
	c.mu.Unlock()
	c.requestRedraw()
	return nil
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
	opt.NativeTools = nil
	c.backendsCfg.Adapters[string(kind)] = opt
	if c.settingsExpandedAg == kind {
		c.settingsExpandedAg = ""
	}
	c.resetAgentProbeStateLocked(kind)
	c.ensureValidSelectionLocked()
	_, needAnnotRefresh := c.ensureValidAnnotationConfigLocked()
	err := c.writeJSONFile("ai-backends.json", c.backendsCfg)
	canRefreshAnnot := needAnnotRefresh != "" && c.providerCfg.Enabled && !c.mockResponder
	c.mu.Unlock()
	if err == nil && canRefreshAnnot {
		c.refreshAnnotationModelsAsync(needAnnotRefresh)
	}
	c.requestRedraw()
	return err
}

func (c *Controller) resetAgentProbeStateLocked(kind model.BackendKind) {
	if c.settingsAgentProbing != nil {
		delete(c.settingsAgentProbing, kind)
	}
	if c.settingsAgentProbeOK != nil {
		delete(c.settingsAgentProbeOK, kind)
	}
	if c.settingsAgentProbeErr != nil {
		delete(c.settingsAgentProbeErr, kind)
	}
	if c.settingsAgentProbeSeq == nil {
		c.settingsAgentProbeSeq = make(map[model.BackendKind]uint64)
	}
	c.settingsAgentProbeSeq[kind]++
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
	c.clearActivePlanLocked()
	delete(c.tabDrafts, 0)
	delete(c.runs, 0)
	c.currentID = 0
	c.scrollToBottom = false
	c.scrollToBottomForce = false
	c.draftText = ""
	c.draftRefs = nil
	c.draftSkills = nil
	c.draftAttachments = nil
	c.retryDraft = nil
	c.historyOpen = false
	c.lightboxOpen = false
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
	if id != 0 && !c.hasConvInActiveNotebookLocked(id) {
		c.mu.Unlock()
		return
	}
	if c.currentID == id {
		c.historyOpen = false
		if id != 0 {
			c.scrollToBottom = true
			c.scrollToBottomForce = true
		}
		linkedPlan := c.linkLatestPendingPlanLocked()
		c.mu.Unlock()
		c.previewPlanInEditor(linkedPlan)
		c.requestRedraw()
		return
	}
	c.saveTabDraftLocked()
	c.clearActivePlanLocked()
	c.currentID = id
	c.scrollToBottom = id != 0
	c.scrollToBottomForce = id != 0
	c.historyOpen = false
	c.lightboxOpen = false
	c.pickIndex = 0
	c.pickerDismissed = false
	td := c.tabDrafts[id]
	c.draftText = td.Text
	c.draftRefs = append([]string(nil), td.Refs...)
	c.draftSkills = append([]string(nil), td.Skills...)
	c.draftAttachments = slices.Clone(td.Attachments)
	if td.Retry != nil {
		cp := *td.Retry
		c.retryDraft = &cp
	} else {
		c.retryDraft = nil
	}
	c.composerEpoch++
	c.composerHandle.Focus()
	c.persistActiveDraftLocked()
	linkedPlan := c.linkLatestPendingPlanLocked()
	c.mu.Unlock()
	c.previewPlanInEditor(linkedPlan)
	c.requestRedraw()
}

// OnNotebookSwitched isolates and switches per-notebook conversation selection and drafts
// without interrupting background streaming tasks in other notebooks.
func (c *Controller) OnNotebookSwitched(newRoot string) {
	nextRoot := normalizeNotebookRoot(newRoot)
	c.mu.Lock()
	if nextRoot == c.activeNotebook {
		if c.currentID != 0 && !c.hasConvInActiveNotebookLocked(c.currentID) {
			c.currentID = 0
			c.persistActiveDraftLocked()
		}
		c.mu.Unlock()
		return
	}
	c.saveTabDraftLocked()
	if c.activeNotebook != "" {
		c.notebookStates[c.activeNotebook] = c.currentNotebookStateLocked()
	}
	c.clearActivePlanLocked()
	c.draftSelection = nil
	c.dismissedSel = nil
	c.historyOpen = false
	c.lightboxOpen = false
	c.pickerDismissed = false
	c.pickIndex = 0
	if !c.pickerInjected {
		c.pickerFolder = ""
		c.pickerFolders = nil
		c.pickerPages = nil
		c.pickerRoot = ""
		c.pickerRefreshed = time.Time{}
		c.pickerRootMtime = time.Time{}
		c.pickerCache.valid = false
	}

	c.activeNotebook = nextRoot
	if nextRoot != "" {
		if st, ok := c.notebookStates[nextRoot]; ok {
			c.applyNotebookStateLocked(st, nextRoot)
		} else {
			c.applyNotebookStateLocked(model.NotebookSessionState{}, nextRoot)
		}
	} else {
		c.applyNotebookStateLocked(model.NotebookSessionState{}, "")
	}
	c.saveTabDraftLocked()
	c.scrollToBottom = c.currentID != 0
	c.scrollToBottomForce = c.currentID != 0
	c.composerEpoch++
	c.persistActiveDraftLocked()
	linkedPlan := c.linkLatestPendingPlanLocked()
	c.mu.Unlock()
	c.previewPlanInEditor(linkedPlan)
	c.requestRedraw()
}

func (c *Controller) saveTabDraftLocked() {
	var retryCopy *model.SentDraft
	if c.retryDraft != nil {
		cp := *c.retryDraft
		retryCopy = &cp
	}
	c.tabDrafts[c.currentID] = tabDraft{
		Text:        c.draftText,
		Refs:        append([]string(nil), c.draftRefs...),
		Skills:      append([]string(nil), c.draftSkills...),
		Attachments: slices.Clone(c.draftAttachments),
		Retry:       retryCopy,
	}
}

// Conversations returns a snapshot slice of stored conversations for the active notebook.
func (c *Controller) Conversations() []model.Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []model.Conversation
	for i := range c.convs {
		if c.convMatchesActiveNotebookLocked(c.convs[i]) {
			out = append(out, cloneConversation(c.convs[i]))
		}
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
	if c.isConvStreamingLocked(c.currentID) || c.hasPendingQuestionLocked() || !c.providerCfg.Enabled {
		c.mu.Unlock()
		return
	}
	if text != "" {
		c.draftText = text
	}
	original := c.draftText
	question := strings.TrimSpace(original)
	hasAttachments := len(c.draftAttachments) > 0
	if !hasAttachments && (question == "" || question == "@" || question == "/") {
		c.mu.Unlock()
		return
	}
	if hasAttachments && (question == "@" || question == "/") {
		question = ""
	}
	backend := c.selectedBackendLocked()
	if !c.mockResponder && backend != model.BackendBuiltin && !supportsCLI(backend) {
		c.setTurnErrorLocked(c.currentID, theme.Tr("暂不支持此本机 Agent，请选择其他后端。", "This local agent is not supported. Select another backend."), false)
		c.mu.Unlock()
		c.requestRedraw()
		return
	}
	if backend == model.BackendBuiltin && !c.mockResponder {
		if msg, needSettings := validateProviderConfig(c.providerCfg); msg != "" {
			c.setTurnErrorLocked(c.currentID, msg, needSettings)
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
	atts := append([]model.Attachment(nil), c.draftAttachments...)
	opt := c.backendsCfg.Adapters[string(backend)]
	nativeTools := supportsCLI(backend) && opt.AllowsNativeTools()
	attachmentText, images, err := c.prepareAttachments(atts, backend == model.BackendBuiltin, nativeTools)
	if err != nil {
		c.setTurnErrorLocked(c.currentID, err.Error(), false)
		c.mu.Unlock()
		c.requestRedraw()
		return
	}
	var sel *EditorSelection
	if c.draftSelection != nil && !c.fullscreen {
		cp := *c.draftSelection
		sel = &cp
	}
	existingConv := c.findConvLocked(c.currentID)
	var scope notebookScope
	if existingConv != nil && existingConv.Notebook != "" {
		scope = scopeForNotebook(c.host, existingConv.Notebook)
		scope.originPage = existingConv.OriginPage
		scope.pageSwitched = conversationPageSwitched(existingConv, scope.page)
	} else {
		scope = captureNotebook(c.host)
		if existingConv == nil {
			scope.originPage = scope.page
			scope.pageSwitched = false
		} else {
			scope.originPage = existingConv.OriginPage
			scope.pageSwitched = conversationPageSwitched(existingConv, scope.page)
		}
	}
	var ctxBuf strings.Builder
	var shownBuf strings.Builder
	notebook, _ := scope.open(c.host)
	if notebook != nil {
		defer notebook.Close()
	}
	for _, ref := range refs {
		page := referencePageID(scope.root, ref)
		filename, err := noteFilename(page)
		if notebook == nil || err != nil {
			c.setTurnErrorLocked(c.currentID, theme.Tr("请移除无法读取或属于其他笔记本的引用。", "Remove unavailable or out-of-notebook references."), false)
			c.mu.Unlock()
			c.requestRedraw()
			return
		}
		md, err := readNotebookNote(context.Background(), notebook, filename)
		if err != nil {
			c.setTurnErrorLocked(c.currentID, theme.Tr("附件无法读取，请移除后重试。", "An attachment could not be read. Remove it and try again."), false)
			c.mu.Unlock()
			c.requestRedraw()
			return
		}
		shownBuf.WriteString("@" + page + " ")
		ctxBuf.WriteString(fmt.Sprintf(theme.Tr("\n\n# 引用文件：%s（pageId: %s）\n\n%s", "\n\n# Referenced file: %s (pageId: %s)\n\n%s"), page, page, md))
	}
	if sel != nil {
		page, err := notePageID(scope.root, sel.NotePath)
		if err == nil && notebook != nil {
			name, _ := noteFilename(page)
			_, err = inspectNote(notebook, name)
		}
		if err != nil || notebook == nil {
			c.setTurnErrorLocked(c.currentID, theme.Tr("选区不在当前授权笔记本内，请移除后重试。", "The selection is outside the authorized notebook. Remove it and try again."), false)
			c.mu.Unlock()
			c.requestRedraw()
			return
		}
		id := sel.identity()
		c.dismissedSel = &id
	}
	c.draftRefs = nil
	c.draftSkills = nil
	c.draftAttachments = nil
	c.draftSelection = nil
	if c.lightboxOpen && c.lightboxSource.isDraft {
		c.lightboxOpen = false
	}
	ctxBuf.WriteString(attachmentText)
	if sel != nil {
		marker := "「" + selectionExcerpt(sel, 16) + "」 "
		shownBuf.WriteString(marker)

		noteName := filepath.Base(sel.NotePath)
		if noteName == "" || noteName == "." || noteName == string(filepath.Separator) {
			noteName = theme.Tr("当前笔记", "current note")
		}
		var lineLabel string
		if sel.StartLine == sel.EndLine {
			lineLabel = fmt.Sprintf(theme.Tr("第 %d 段", "paragraph %d"), sel.StartLine)
		} else {
			lineLabel = fmt.Sprintf(theme.Tr("第 %d–%d 段", "paragraphs %d–%d"), sel.StartLine, sel.EndLine)
		}
		ctxBuf.WriteString(fmt.Sprintf(theme.Tr("\n\n# 用户选中的内容（来自 %s，%s）\n\n%s", "\n\n# User selection (from %s, %s)\n\n%s"), noteName, lineLabel, sel.Markdown))
	}
	for _, sk := range skills {
		shownBuf.WriteString("/")
		shownBuf.WriteString(sk)
		shownBuf.WriteByte(' ')
		if spec, ok := c.findSkillLocked(sk); ok && spec.Content != "" {
			ctxBuf.WriteString(fmt.Sprintf(theme.Tr("\n\n# 用户调用的 Skill：%s\n\n%s", "\n\n# Skill the user called: %s\n\n%s"), sk, spec.Content))
		}
	}

	content := question
	if shownBuf.Len() > 0 {
		if question == "" {
			content = strings.TrimSpace(shownBuf.String())
		} else {
			content = shownBuf.String() + question
		}
	}

	retryMatch := c.retryDraft != nil &&
		c.canRetryDraftLocked(c.retryDraft) &&
		c.retryDraft.Text == original &&
		slicesEqual(c.retryDraft.Refs, refs) &&
		slicesEqual(c.retryDraft.Skills, skills) &&
		slices.Equal(c.retryDraft.Attachments, atts) &&
		sel == nil

	var sentDraft *model.SentDraft
	if retryMatch {
		cp := *c.retryDraft
		sentDraft = &cp
		conv := c.findConvLocked(c.currentID)
		conv.Messages[cp.MessageIndex].Context = ctxBuf.String()
		conv.Messages[cp.MessageIndex].ActivePage = scope.page
		if conv.Notebook == "" && scope.root != "" {
			conv.Notebook = normalizeNotebookRoot(scope.root)
		}
		c.persistChatsLocked()
	} else {
		isNewConv := c.currentID == 0
		c.pushMessageLocked(model.ChatMessage{
			Role:                "user",
			Content:             content,
			Context:             ctxBuf.String(),
			Refs:                refs,
			Skills:              skills,
			ActivePage:          scope.page,
			Attachments:         atts,
			AttachmentsSeparate: true,
		})
		conv := c.findConvLocked(c.currentID)
		if conv != nil {
			changedScope := false
			if isNewConv {
				conv.Notebook = normalizeNotebookRoot(scope.root)
				conv.OriginPage = scope.page
				changedScope = true
			} else if conv.Notebook == "" && scope.root != "" {
				conv.Notebook = normalizeNotebookRoot(scope.root)
				changedScope = true
			}
			if changedScope {
				c.persistChatsLocked()
			}
			if len(conv.Messages) > 0 {
				sentDraft = &model.SentDraft{
					Text:         original,
					Refs:         refs,
					Skills:       skills,
					Attachments:  atts,
					Conversation: conv.ID,
					MessageIndex: len(conv.Messages) - 1,
				}
			}
		}
	}

	delete(c.runs, 0)
	c.retryDraft = nil
	c.draftText = ""
	c.pickerDismissed = false
	c.pickIndex = 0
	c.saveTabDraftLocked()
	c.persistActiveDraftLocked()

	isMock := c.mockResponder
	convID := c.currentID
	st := c.ensureRunStateLocked(convID)
	st.sentDraft = sentDraft
	st.sentSelection = sel
	st.turnError = ""
	st.turnErrorSettings = false
	var turn *assistantTurn
	if !isMock {
		ctx, cancel := context.WithCancel(context.Background())
		c.streamSerial++
		conv := c.findConvLocked(convID)
		turn = &assistantTurn{
			ctx: ctx, cancel: cancel, notebookScope: scope, convID: convID, serial: c.streamSerial,
			cfg: resolveProviderExecutionConfig(c.providerCfg), thinking: c.backendThinkingLocked(backend),
			nativeTools: nativeTools, attachmentImages: images, deepUI: c.deepAI,
			userIndex: st.sentDraft.MessageIndex, historyEnd: len(conv.Messages), questionIndex: -1,
		}
		if backend != model.BackendBuiltin {
			if cliPath, ok := resolveConfiguredCLIBinary(string(backend), opt.BinaryPath); ok {
				turn.cliPath = cliPath
			}
		}
		st.turn = turn
		st.serial = turn.serial
		st.streaming = true
		st.partialText.Reset()
		st.partialThink.Reset()
		st.partialSteps = nil
	}
	// 新一轮开始：回到底部并持续跟随，直到用户主动上滑或展开查看旧内容。
	c.scrollToBottom = true
	c.scrollToBottomForce = true
	c.mu.Unlock()

	c.requestRedraw()
	if isMock {
		c.startMockTurn(convID, question)
		return
	}
	if backend == model.BackendBuiltin {
		c.startBuiltinTurn(turn, 0)
	} else {
		c.startExternalTurn(turn, backend)
	}
}

// IsStreaming reports whether the active conversation has an AI response turn in progress.
func (c *Controller) IsStreaming() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConvStreamingLocked(c.currentID)
}

// StopStreaming cancels any in-flight AI response on the active conversation and preserves partial output.
func (c *Controller) StopStreaming() {
	c.mu.Lock()
	c.stopConvStreamingLocked(c.currentID)
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) stopAllStreamingLocked() {
	ids := make([]uint64, 0, len(c.runs))
	for id := range c.runs {
		ids = append(ids, id)
	}
	for _, id := range ids {
		c.stopConvStreamingLocked(id)
	}
}

func (c *Controller) stopConvStreamingLocked(convID uint64) {
	st := c.runs[convID]
	wasBusy := (st != nil && st.streaming) || c.hasPendingQuestionForConvLocked(convID)
	if wasBusy && convID != 0 {
		c.recordTurnOutcomeForConvLocked(convID, "stopped")
	}
	c.finishConvTurnLocked(convID)
	if st == nil {
		return
	}
	st.sentDraft = nil
	st.sentSelection = nil
	st.streaming = false
	st.serial++
	if convID == c.currentID {
		c.clearStreamRedrawLocked()
	}
	partial := st.partialText.String()
	st.partialText.Reset()
	st.partialThink.Reset()
	st.partialSteps = nil
	if strings.TrimSpace(partial) != "" && convID != 0 {
		c.pushMessageToConvLocked(convID, model.ChatMessage{
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
		title := conversationTitleFromMessage(msg)
		conv := model.Conversation{
			ID:       id,
			Title:    title,
			Notebook: c.activeNotebook,
			Backend:  c.selectedBackendLocked(),
		}
		c.convs = append([]model.Conversation{conv}, c.convs...)
		c.currentID = id
	}
	c.pushMessageToConvLocked(c.currentID, msg)
}

func (c *Controller) pushMessageToConvLocked(convID uint64, msg model.ChatMessage) {
	if conv := c.findConvLocked(convID); conv != nil {
		conv.Messages = append(conv.Messages, msg)
	}
	c.persistChatsLocked()
}

func (c *Controller) recordTurnOutcomeForConvLocked(convID uint64, outcome string) {
	conv := c.findConvLocked(convID)
	if conv == nil {
		return
	}
	conv.Messages = append(conv.Messages, model.ChatMessage{
		Role:    "turn_outcome",
		Content: outcome,
	})
	c.persistChatsLocked()
}

func (c *Controller) turnActiveLocked(turn *assistantTurn) bool {
	if turn == nil || turn.ctx.Err() != nil || !c.providerCfg.Enabled {
		return false
	}
	st := c.runs[turn.convID]
	return st != nil && st.turn == turn && st.serial == turn.serial
}

func (c *Controller) finishConvTurnLocked(convID uint64) {
	if st := c.runs[convID]; st != nil && st.turn != nil {
		st.turn.cancel()
		st.turn = nil
	}
}

func (c *Controller) runStateLocked(convID uint64) *convRunState {
	return c.runs[convID]
}

func (c *Controller) ensureRunStateLocked(convID uint64) *convRunState {
	st := c.runs[convID]
	if st == nil {
		st = &convRunState{}
		c.runs[convID] = st
	}
	return st
}

func (c *Controller) isConvStreamingLocked(convID uint64) bool {
	st := c.runs[convID]
	return st != nil && st.streaming
}

func (c *Controller) setTurnErrorLocked(convID uint64, msg string, settings bool) {
	st := c.ensureRunStateLocked(convID)
	st.turnError = msg
	st.turnErrorSettings = settings
}

func (c *Controller) clearTurnErrorLocked(convID uint64) {
	if st := c.runs[convID]; st != nil {
		st.turnError = ""
		st.turnErrorSettings = false
	}
}

func (c *Controller) linkAndPreviewPlanLocked(convID uint64, msgIdx int) *model.BatchPlan {
	if convID == 0 || convID != c.currentID || !c.hasConvInActiveNotebookLocked(convID) {
		return nil
	}
	return c.linkPlanLocked(convID, msgIdx)
}

func (c *Controller) hasPendingQuestionLocked() bool {
	return c.hasPendingQuestionForConvLocked(c.currentID)
}

func (c *Controller) hasPendingQuestionForConvLocked(convID uint64) bool {
	if st := c.runs[convID]; st != nil && c.turnActiveLocked(st.turn) && st.turn.questionIndex >= 0 {
		return true
	}
	if !c.mockResponder {
		return false
	}
	conv := c.findConvLocked(convID)
	if conv == nil {
		return false
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		m := conv.Messages[i]
		if m.Role == "user" || m.Role == "turn_outcome" {
			break
		}
		if m.Question != nil && !m.Question.Submitted {
			return true
		}
	}
	return false
}

func (c *Controller) composerDraftEmptyLocked() bool {
	return c.draftText == "" && len(c.draftRefs) == 0 && len(c.draftSkills) == 0 && len(c.draftAttachments) == 0
}

func (c *Controller) canRetryDraftLocked(draft *model.SentDraft) bool {
	if draft.Conversation != c.currentID {
		return false
	}
	conv := c.findConvLocked(draft.Conversation)
	if conv == nil || draft.MessageIndex < 0 || draft.MessageIndex >= len(conv.Messages) {
		return false
	}
	for i := len(conv.Messages) - 1; i >= draft.MessageIndex; i-- {
		if msg := conv.Messages[i]; msg.Role == "user" {
			return i == draft.MessageIndex && slices.Equal(msg.Attachments, draft.Attachments)
		}
	}
	return false
}

func (c *Controller) restoreFailedDraftForConvLocked(convID uint64) {
	st := c.runs[convID]
	if st == nil || st.sentDraft == nil {
		return
	}
	draft := *st.sentDraft
	st.sentDraft = nil
	sentSel := st.sentSelection
	st.sentSelection = nil
	conv := c.findConvLocked(draft.Conversation)
	if c.currentID == draft.Conversation && c.composerDraftEmptyLocked() {
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
		c.draftAttachments = slices.Clone(draft.Attachments)
		if sentSel != nil && c.draftSelection == nil {
			c.draftSelection = sentSel
			c.dismissedSel = nil
		}
		c.composerEpoch++
		c.saveTabDraftLocked()
		c.persistActiveDraftLocked()
		return
	}
	c.deferredDrafts = append(c.deferredDrafts, draft)
	c.persistFailedDraftsLocked()
}

func (c *Controller) convMatchesNotebookLocked(conv model.Conversation, notebookRoot string) bool {
	return normalizeNotebookRoot(conv.Notebook) == normalizeNotebookRoot(notebookRoot)
}

func (c *Controller) convMatchesActiveNotebookLocked(conv model.Conversation) bool {
	return c.convMatchesNotebookLocked(conv, c.activeNotebook)
}

func (c *Controller) hasConvInActiveNotebookLocked(id uint64) bool {
	conv := c.findConvLocked(id)
	return conv != nil && c.convMatchesActiveNotebookLocked(*conv)
}

func (c *Controller) currentNotebookStateLocked() model.NotebookSessionState {
	var convPtr *uint64
	if c.currentID != 0 {
		id := c.currentID
		convPtr = &id
	}
	return model.NotebookSessionState{
		Conversation: convPtr,
		Text:         c.draftText,
		Refs:         append([]string(nil), c.draftRefs...),
		Skills:       append([]string(nil), c.draftSkills...),
		Attachments:  slices.Clone(c.draftAttachments),
		Retry:        cloneSentDraft(c.retryDraft),
	}
}

func (c *Controller) sanitizeNotebookStateLocked(root string, st model.NotebookSessionState) model.NotebookSessionState {
	var convPtr *uint64
	if st.Conversation != nil && *st.Conversation != 0 {
		if conv := c.findConvLocked(*st.Conversation); conv != nil && c.convMatchesNotebookLocked(*conv, root) {
			id := *st.Conversation
			convPtr = &id
		}
	}
	var retry *model.SentDraft
	if st.Retry != nil && convPtr != nil && st.Retry.Conversation == *convPtr {
		retry = cloneSentDraft(st.Retry)
	}
	return model.NotebookSessionState{
		Conversation: convPtr,
		Text:         st.Text,
		Refs:         append([]string(nil), st.Refs...),
		Skills:       append([]string(nil), st.Skills...),
		Attachments:  slices.Clone(st.Attachments),
		Retry:        retry,
	}
}

func (c *Controller) applyNotebookStateLocked(st model.NotebookSessionState, root string) {
	clean := c.sanitizeNotebookStateLocked(root, st)
	if clean.Conversation != nil {
		c.currentID = *clean.Conversation
	} else {
		c.currentID = 0
	}
	c.draftText = clean.Text
	c.draftRefs = append([]string(nil), clean.Refs...)
	c.draftSkills = append([]string(nil), clean.Skills...)
	c.draftAttachments = slices.Clone(clean.Attachments)
	c.retryDraft = cloneSentDraft(clean.Retry)
}

func (c *Controller) inferLegacyConversationNotebooksLocked(byNotebook map[string]model.NotebookSessionState) bool {
	var known []string
	seen := make(map[string]bool)
	addKnown := func(raw string) {
		norm := normalizeNotebookRoot(raw)
		if norm != "" && !seen[norm] {
			seen[norm] = true
			known = append(known, norm)
		}
	}
	if c.host != nil {
		addKnown(c.host.ActiveNotebook())
		for _, nb := range c.host.Notebooks() {
			addKnown(nb)
		}
	}
	for k := range byNotebook {
		addKnown(k)
	}
	for i := range c.convs {
		addKnown(c.convs[i].Notebook)
	}

	changed := false
	for i := range c.convs {
		conv := &c.convs[i]
		if conv.Notebook != "" {
			norm := normalizeNotebookRoot(conv.Notebook)
			if norm != conv.Notebook {
				conv.Notebook = norm
				changed = true
			}
			addKnown(norm)
			continue
		}
		if inferred := inferNotebookFromConversation(*conv, known); inferred != "" {
			conv.Notebook = inferred
			addKnown(inferred)
			changed = true
		}
	}
	return changed
}

func inferNotebookFromConversation(conv model.Conversation, known []string) string {
	for _, m := range conv.Messages {
		if m.Plan != nil && strings.TrimSpace(m.Plan.Root) != "" {
			if norm := normalizeNotebookRoot(m.Plan.Root); norm != "" {
				return norm
			}
		}
	}
	for _, m := range conv.Messages {
		for _, s := range m.Sources {
			for _, nb := range known {
				if pathInsideNotebookRoot(nb, s.Path) {
					return nb
				}
			}
		}
		for _, ref := range m.Refs {
			for _, nb := range known {
				if pathInsideNotebookRoot(nb, ref) {
					return nb
				}
			}
		}
	}
	return ""
}

func pathInsideNotebookRoot(root, abs string) bool {
	if root == "" || !filepath.IsAbs(abs) {
		return false
	}
	normAbs := normalizeNotebookRoot(abs)
	if rel, err := filepath.Rel(root, normAbs); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cloneSentDraft(d *model.SentDraft) *model.SentDraft {
	if d == nil {
		return nil
	}
	cp := *d
	cp.Refs = append([]string(nil), d.Refs...)
	cp.Skills = append([]string(nil), d.Skills...)
	cp.Attachments = slices.Clone(d.Attachments)
	return &cp
}

func cloneNotebookSessionState(st model.NotebookSessionState) model.NotebookSessionState {
	var convPtr *uint64
	if st.Conversation != nil {
		id := *st.Conversation
		convPtr = &id
	}
	return model.NotebookSessionState{
		Conversation: convPtr,
		Text:         st.Text,
		Refs:         append([]string(nil), st.Refs...),
		Skills:       append([]string(nil), st.Skills...),
		Attachments:  slices.Clone(st.Attachments),
		Retry:        cloneSentDraft(st.Retry),
	}
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
	canonical, err := AuthorizedNotebookRoot(c.host, root)
	if err != nil {
		return ""
	}
	page, _ := notePageID(canonical, c.host.ActiveNotePath())
	return page
}

func (c *Controller) readPageContentLocked(root, page string) string {
	canonical, err := AuthorizedNotebookRoot(c.host, root)
	if err != nil {
		return ""
	}
	dir, err := openNotebook(canonical)
	if err != nil {
		return ""
	}
	defer dir.Close()
	name, err := noteFilename(page)
	if err != nil {
		return ""
	}
	body, _ := readNotebookNote(context.Background(), dir, name)
	return body
}

func (c *Controller) loadBuiltinSkillsLocked() {
	builtins := []struct {
		id   string
		file string
		desc string
	}{
		{"createNoote", "createNoote.md", "在本轮确定的笔记本和目录新建页面"},
		{"updateNote", "updateNote.md", "改写、追加或补充当前页"},
		{"searchNotes", "searchNotes.md", "在本轮授权的笔记本内搜索并读取页面"},
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

type cachedSkillEntry struct {
	fileMtime time.Time
	dirMtime  time.Time
	spec      model.SkillSpec
}

func (c *Controller) refreshPickerIfNeeded() {
	c.mu.Lock()
	if c.pickerInjected || c.host == nil {
		c.mu.Unlock()
		return
	}
	root := c.activeRootLocked()
	if root == "" {
		c.mu.Unlock()
		return
	}
	rootChanged := c.pickerRoot != root
	if !rootChanged && c.pickerLoading {
		c.mu.Unlock()
		return
	}
	if !rootChanged && !c.pickerRefreshed.IsZero() && time.Since(c.pickerRefreshed) < pickerRefreshCooldown {
		c.mu.Unlock()
		return
	}
	prevSkillCache := make(map[string]cachedSkillEntry, len(c.pickerSkillCache))
	for k, v := range c.pickerSkillCache {
		prevSkillCache[k] = v
	}
	prevRootMtime := c.pickerRootMtime
	if rootChanged {
		c.pickerFolder = ""
		c.pickerFolders = nil
		c.pickerPages = nil
		c.pickerSkills = nil
		c.pickerRootMtime = time.Time{}
		c.pickerRefreshed = time.Time{}
		c.pickerCache.valid = false
		prevRootMtime = time.Time{}
		prevSkillCache = make(map[string]cachedSkillEntry)
	}
	prevHasData := !rootChanged && (len(c.pickerPages) > 0 || len(c.pickerFolders) > 0)
	prevFolders := append([]string(nil), c.pickerFolders...)
	prevPages := append([]string(nil), c.pickerPages...)
	c.pickerRefreshGen++
	refreshGen := c.pickerRefreshGen
	c.pickerLoading = true
	c.pickerRoot = root
	host := c.host
	c.mu.Unlock()

	go func() {
		rootInfo, rootStatErr := os.Stat(root)
		entries, err := host.ListNotebookFiles(root)
		foldersSet := make(map[string]struct{})
		var pages []string
		var skills []model.SkillSpec
		nextSkillCache := make(map[string]cachedSkillEntry)
		skillsChanged := false

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
						fileInfo, ferr := os.Stat(ent.Path)
						dirInfo, derr := os.Stat(filepath.Dir(ent.Path))
						if cached, ok := prevSkillCache[ent.Path]; ok && ferr == nil && derr == nil &&
							cached.fileMtime.Equal(fileInfo.ModTime()) && cached.dirMtime.Equal(dirInfo.ModTime()) {
							skills = append(skills, cached.spec)
							nextSkillCache[ent.Path] = cached
							continue
						}
						if txt, rErr := host.ReadNote(ent.Path); rErr == nil {
							spec := parseSkillMarkdown(page, txt)
							skills = append(skills, spec)
							entry := cachedSkillEntry{spec: spec}
							if ferr == nil {
								entry.fileMtime = fileInfo.ModTime()
							}
							if derr == nil {
								entry.dirMtime = dirInfo.ModTime()
							}
							nextSkillCache[ent.Path] = entry
							skillsChanged = true
						}
					}
				}
			}
		}
		if err == nil {
			walk(entries)
		}
		if len(nextSkillCache) != len(prevSkillCache) {
			skillsChanged = true
		}
		folders := make([]string, 0, len(foldersSet))
		for f := range foldersSet {
			folders = append(folders, f)
		}
		sort.Strings(folders)
		sort.Strings(pages)

		listsChanged := !prevHasData || !slicesEqual(prevFolders, folders) || !slicesEqual(prevPages, pages)
		rootMtimeUnchanged := rootStatErr == nil && !prevRootMtime.IsZero() && rootInfo.ModTime().Equal(prevRootMtime)
		changed := listsChanged || skillsChanged || !rootMtimeUnchanged

		c.mu.Lock()
		if !c.pickerInjected && c.pickerRoot == root && c.pickerRefreshGen == refreshGen {
			if err == nil {
				if listsChanged {
					c.pickerFolders = folders
					c.pickerPages = pages
					c.pickerCache.valid = false
				}
				if skillsChanged || !prevHasData {
					c.loadBuiltinSkillsLocked()
					for _, sk := range skills {
						c.upsertSkillLocked(sk)
					}
					c.pickerCache.valid = false
				}
				c.pickerSkillCache = nextSkillCache
				if rootStatErr == nil {
					c.pickerRootMtime = rootInfo.ModTime()
				}
				c.pickerRefreshed = time.Now()
			}
			c.pickerLoading = false
		}
		c.mu.Unlock()
		if err == nil && changed {
			c.requestRedraw()
		}
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

// selectionExcerpt returns the opening runes of an editor selection for compact labels.
func selectionExcerpt(sel *EditorSelection, maxRunes int) string {
	if sel == nil {
		return ""
	}
	runes := []rune(strings.TrimSuffix(sel.PlainPreview, "…"))
	if len(runes) == 0 {
		return theme.Tr("选中内容", "Selection")
	}
	if len(runes) <= maxRunes && !strings.HasSuffix(sel.PlainPreview, "…") {
		return string(runes)
	}
	return strings.TrimRight(string(runes[:min(maxRunes, len(runes))]), " ") + "…"
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
			if v.NativeTools != nil {
				b := *v.NativeTools
				v.NativeTools = &b
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
			cp.Messages[i].Attachments = slices.Clone(m.Attachments)
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

// TelemetryBackendCategory returns a coarse, privacy-safe category string for the
// currently configured and active AI backend, or "none" when AI is disabled or unconfigured.
func (c *Controller) TelemetryBackendCategory() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.providerCfg.Enabled {
		return "none"
	}
	kind := c.selectedBackendLocked()
	if kind == model.BackendBuiltin {
		if isBackendEnabled(c.backendsCfg, model.BackendBuiltin) {
			resolved := resolveProviderExecutionConfig(c.providerCfg)
			if builtinReady(resolved) {
				return classifyBuiltinProvider(resolved)
			}
		}
		for _, k := range c.priorityOrderLocked(c.recognizedBackendsLocked()) {
			if k != model.BackendBuiltin && isBackendEnabled(c.backendsCfg, k) {
				return classifyBackendKind(k)
			}
		}
		return "none"
	}
	if !c.isBackendAvailableLocked(kind) {
		return "none"
	}
	return classifyBackendKind(kind)
}

func classifyBackendKind(kind model.BackendKind) string {
	switch kind {
	case model.BackendCodex:
		return "codex"
	case model.BackendClaude:
		return "claude-code"
	case model.BackendAgy:
		return "antigravity"
	case model.BackendPi:
		return "pi"
	case model.BackendDsh:
		return "dsh"
	default:
		return "unknown"
	}
}

func classifyBuiltinProvider(cfg model.AIProviderConfig) string {
	prov := strings.ToLower(strings.TrimSpace(cfg.Provider))
	base := strings.ToLower(strings.TrimSpace(cfg.BaseURL))
	switch {
	case prov == "deepseek" || strings.HasPrefix(prov, "deepseek-") || strings.Contains(base, "api.deepseek.com"):
		return "deepseek"
	case prov == "openai" || strings.HasPrefix(prov, "openai-") || strings.Contains(base, "api.openai.com"):
		return "openai"
	case prov == "anthropic" || prov == "claude" || strings.HasPrefix(prov, "anthropic-") || strings.HasPrefix(prov, "claude-") || strings.Contains(base, "api.anthropic.com"):
		return "claude"
	default:
		return "other"
	}
}
