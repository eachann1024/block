package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	inlineAIPanelWidth  float32 = 380
	inlineAIChipH       float32 = theme.HeightMicro
	inlineAIChipGap     float32 = 5
	inlineAIMaxChipRows int     = 3
	inlineAIChipAreaH   float32 = inlineAIChipH*float32(inlineAIMaxChipRows) + inlineAIChipGap*float32(inlineAIMaxChipRows-1)
	inlineAIPanelInnerW float32 = inlineAIPanelWidth - 16
	inlineAIChipChromeW float32 = 40
)

type PromptScope string

const (
	PromptScopeEdit  PromptScope = "edit"
	PromptScopeEmpty PromptScope = "empty"
)

type PromptPreset string

const (
	PresetContinue   PromptPreset = "continue"
	PresetOutline    PromptPreset = "outline"
	PresetTasks      PromptPreset = "tasks"
	PresetBrainstorm PromptPreset = "brainstorm"
	PresetPolish     PromptPreset = "polish"
	PresetSimplify   PromptPreset = "simplify"
	PresetPlain      PromptPreset = "plain"
	PresetTranslate  PromptPreset = "translate"
)

var emptyPresets = []PromptPreset{
	PresetContinue,
	PresetOutline,
	PresetTasks,
	PresetBrainstorm,
}

var editPresets = []PromptPreset{
	PresetPolish,
	PresetSimplify,
	PresetPlain,
	PresetTranslate,
}

func (p PromptPreset) labels() (zh, en string) {
	switch p {
	case PresetContinue:
		return "续写", "Continue"
	case PresetOutline:
		return "列大纲", "Outline"
	case PresetTasks:
		return "待办清单", "To-do list"
	case PresetBrainstorm:
		return "头脑风暴", "Brainstorm"
	case PresetPolish:
		return "润色", "Polish"
	case PresetSimplify:
		return "精简", "Simplify"
	case PresetPlain:
		return "通俗一点", "Plain language"
	case PresetTranslate:
		return "翻译", "Translate"
	default:
		return string(p), string(p)
	}
}

func (p PromptPreset) Label() string {
	zh, en := p.labels()
	return theme.Tr(zh, en)
}

func (p PromptPreset) MatchesInput(raw string) bool {
	zh, en := p.labels()
	return raw == zh || strings.EqualFold(raw, en)
}

func (p PromptPreset) Prompt() string {
	switch p {
	case PresetContinue:
		return theme.Tr(
			"顺着上文语境自然往下续写，保持语气与结构连贯，不要重复上文。",
			"Continue naturally from the text above. Keep the same voice and structure, and do not repeat what is already written.",
		)
	case PresetOutline:
		return theme.Tr(
			"结合当前主题列出结构清晰的三级标题（###）与要点大纲。",
			"Turn the current topic into a clear outline of level-3 headings (###) and bullet points.",
		)
	case PresetTasks:
		return theme.Tr(
			"生成具体可执行的 Markdown 待办清单，每项使用独立的 `- [ ] ` 行。",
			"Write a concrete Markdown to-do list. Put each item on its own `- [ ] ` line.",
		)
	case PresetBrainstorm:
		return theme.Tr(
			"围绕当前主题发散列出 4 到 5 个有启发性的思路要点，每项使用独立的 `- ` 行。",
			"List 4 or 5 useful ideas about the current topic, each on its own `- ` line.",
		)
	case PresetPolish:
		return theme.Tr(
			"润色给定片段，使表达更清晰流畅，保留原意与关键细节。",
			"Polish the given text so it reads more clearly, while keeping the meaning and important details.",
		)
	case PresetSimplify:
		return theme.Tr(
			"精简给定片段，删去冗余修饰，只保留核心信息。",
			"Shorten the given text. Remove extra wording and keep only the core point.",
		)
	case PresetPlain:
		return theme.Tr(
			"用通俗直白的大白话改写给定片段，去掉生僻黑话或绕口表达。",
			"Rewrite the given text in plain language. Remove jargon and awkward phrasing.",
		)
	case PresetTranslate:
		return theme.Tr(
			"智能中英互译：若给定片段主要为中文则准确自然地翻译成英文；若主要为英文或其他外语则准确自然地翻译成中文。保留专有名词、代码标识符与原有 Markdown 格式。",
			"Translate between Chinese and English: if the text is mostly Chinese, translate it into natural English; if it is mostly English or another language, translate it into natural Chinese. Keep proper nouns, code identifiers, and the existing Markdown formatting.",
		)
	default:
		return ""
	}
}

type promptTarget struct {
	Kind   string       `json:"kind"` // "preset" | "custom"
	Preset PromptPreset `json:"preset,omitempty"`
	Text   string       `json:"text,omitempty"`
}

func (t promptTarget) ShortLabel() string {
	if t.Kind == "preset" {
		return t.Preset.Label()
	}
	return trunc12Runes(t.Text)
}

func (t promptTarget) FullLabel() string {
	if t.Kind == "preset" {
		return t.Preset.Prompt()
	}
	return strings.TrimSpace(t.Text)
}

type promptEntry struct {
	promptTarget
	Starred bool `json:"starred,omitempty"`
}

type storedPrompts struct {
	Edit  []promptEntry `json:"edit"`
	Empty []promptEntry `json:"empty"`
}

type legacySavedPrompt struct {
	Text    string `json:"text"`
	Starred bool   `json:"starred"`
}

type inlinePromptStore struct {
	dir    string
	loaded bool
	data   storedPrompts
}

func seededPresets(scope PromptScope) []promptEntry {
	src := editPresets
	if scope == PromptScopeEmpty {
		src = emptyPresets
	}
	out := make([]promptEntry, len(src))
	for i, p := range src {
		out[i] = promptEntry{
			promptTarget: promptTarget{Kind: "preset", Preset: p},
			Starred:      true,
		}
	}
	return out
}

func defaultSeededPrompts() storedPrompts {
	return storedPrompts{
		Edit:  seededPresets(PromptScopeEdit),
		Empty: seededPresets(PromptScopeEmpty),
	}
}

func sanitizePromptList(items []promptEntry) []promptEntry {
	var starred, unstarred []promptEntry
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Kind == "custom" {
			it.Text = strings.TrimSpace(it.Text)
			if it.Text == "" {
				continue
			}
		} else if it.Kind != "preset" || it.Preset == "" {
			continue
		}
		key := it.Kind + ":" + string(it.Preset) + ":" + it.Text
		if seen[key] {
			continue
		}
		seen[key] = true
		if it.Starred {
			starred = append(starred, it)
		} else {
			unstarred = append(unstarred, it)
		}
	}
	return append(starred, unstarred...)
}

func trunc12Runes(raw string) string {
	trimmed := strings.TrimSpace(raw)
	runes := []rune(trimmed)
	if len(runes) <= 12 {
		return trimmed
	}
	return string(runes[:12]) + "…"
}

func estimateChipWidth(shortText string) float32 {
	var textW float32
	for _, ch := range shortText {
		if ch < 0x80 {
			switch ch {
			case ' ':
				textW += 3.8
			case 'i', 'l', 'I', '1', '.', ',', ':', ';', '\'', '!', '|':
				textW += 4.2
			case 'm', 'w':
				textW += 8.4
			default:
				if ch >= 'A' && ch <= 'Z' {
					textW += 8.4
				} else {
					textW += 7.2
				}
			}
		} else {
			textW += 13.0
		}
	}
	return min(inlineAIChipChromeW+textW, inlineAIPanelInnerW)
}

func chipRowIndices(items []promptEntry) []int {
	out := make([]int, len(items))
	row := 0
	var x float32
	for i, it := range items {
		w := estimateChipWidth(it.ShortLabel())
		if x > 0 && x+inlineAIChipGap+w > inlineAIPanelInnerW {
			row++
			x = w
		} else {
			if x > 0 {
				x += inlineAIChipGap
			}
			x += w
		}
		out[i] = row
	}
	return out
}

func pruneUnstarredPastMaxRows(items []promptEntry) []promptEntry {
	rows := chipRowIndices(items)
	out := make([]promptEntry, 0, len(items))
	for i, it := range items {
		if it.Starred || rows[i] < inlineAIMaxChipRows {
			out = append(out, it)
		}
	}
	return out
}

func (st *inlinePromptStore) slot(scope PromptScope) *[]promptEntry {
	if scope == PromptScopeEmpty {
		return &st.data.Empty
	}
	return &st.data.Edit
}

func (st *inlinePromptStore) storagePath() string {
	if st.dir == "" {
		return ""
	}
	return filepath.Join(st.dir, "ai-inline-prompts.json")
}

func (st *inlinePromptStore) ensureLoaded() {
	if st.loaded {
		return
	}
	st.loaded = true
	p := st.storagePath()
	if p == "" {
		st.data = defaultSeededPrompts()
		return
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		st.data = defaultSeededPrompts()
		return
	}
	var sp storedPrompts
	if err := json.Unmarshal(raw, &sp); err == nil && (len(sp.Edit) > 0 || len(sp.Empty) > 0) {
		st.data = storedPrompts{
			Edit:  sanitizePromptList(sp.Edit),
			Empty: sanitizePromptList(sp.Empty),
		}
		return
	}
	var legacy []legacySavedPrompt
	if err := json.Unmarshal(raw, &legacy); err == nil {
		edit := seededPresets(PromptScopeEdit)
		for _, wantStar := range []bool{true, false} {
			for _, item := range legacy {
				if item.Starred != wantStar {
					continue
				}
				clean := strings.TrimSpace(item.Text)
				if clean == "" || slices.ContainsFunc(editPresets, func(p PromptPreset) bool { return p.MatchesInput(clean) }) {
					continue
				}
				edit = append(edit, promptEntry{
					promptTarget: promptTarget{Kind: "custom", Text: clean},
					Starred:      item.Starred,
				})
			}
		}
		edit = pruneUnstarredPastMaxRows(sanitizePromptList(edit))
		st.data = storedPrompts{
			Edit:  edit,
			Empty: seededPresets(PromptScopeEmpty),
		}
		st.save()
		return
	}
	st.data = defaultSeededPrompts()
}

func (st *inlinePromptStore) save() {
	p := st.storagePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if data, err := json.MarshalIndent(st.data, "", "  "); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}

func (st *inlinePromptStore) list(scope PromptScope) []promptEntry {
	st.ensureLoaded()
	src := *st.slot(scope)
	out := make([]promptEntry, len(src))
	copy(out, src)
	return out
}

func (st *inlinePromptStore) record(scope PromptScope, raw string) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return
	}
	st.ensureLoaded()
	slot := st.slot(scope)
	items := *slot
	for i, entry := range items {
		matched := false
		if entry.Kind == "preset" {
			matched = entry.Preset.MatchesInput(clean)
		} else {
			matched = entry.Text == clean
		}
		if matched {
			if entry.Starred {
				return
			}
			items = append(items[:i], items[i+1:]...)
			insertAt := firstUnstarredIndex(items)
			items = slices.Insert(items, insertAt, entry)
			*slot = pruneUnstarredPastMaxRows(items)
			st.save()
			return
		}
	}

	presets := editPresets
	if scope == PromptScopeEmpty {
		presets = emptyPresets
	}
	target := promptTarget{Kind: "custom", Text: clean}
	for _, p := range presets {
		if p.MatchesInput(clean) {
			target = promptTarget{Kind: "preset", Preset: p}
			break
		}
	}
	insertAt := firstUnstarredIndex(items)
	items = slices.Insert(items, insertAt, promptEntry{promptTarget: target, Starred: false})
	*slot = pruneUnstarredPastMaxRows(items)
	st.save()
}

func (st *inlinePromptStore) toggleStar(scope PromptScope, target promptTarget) {
	st.ensureLoaded()
	slot := st.slot(scope)
	items := *slot
	pos := -1
	for i, it := range items {
		if it.promptTarget == target {
			pos = i
			break
		}
	}
	if pos < 0 {
		return
	}
	entry := items[pos]
	items = append(items[:pos], items[pos+1:]...)
	if entry.Starred {
		entry.Starred = false
		items = append(items, entry)
		items = pruneUnstarredPastMaxRows(items)
	} else {
		entry.Starred = true
		insertAt := firstUnstarredIndex(items)
		items = slices.Insert(items, insertAt, entry)
		items = pruneUnstarredPastMaxRows(items)
	}
	*slot = items
	st.save()
}

func firstUnstarredIndex(items []promptEntry) int {
	for i, it := range items {
		if !it.Starred {
			return i
		}
	}
	return len(items)
}

// --- Session Configuration APIs ---

func (s *Session) SetDataDir(dir string) {
	dir = strings.TrimSpace(dir)
	if s.notePath == "" && s.dataDir != dir {
		s.resetMedia()
	}
	s.dataDir = dir
	s.promptStore = &inlinePromptStore{dir: s.dataDir}
	s.annotLoadedPath = ""
	s.ensureAnnotationsLoaded()
}

func (s *Session) ensurePromptStore() *inlinePromptStore {
	if s.promptStore == nil {
		s.promptStore = &inlinePromptStore{dir: s.dataDir}
	}
	return s.promptStore
}

func (s *Session) SetInlineAIResponder(fn func(ctx, orig, draft, instruction string) string) {
	s.inlineAIResponder = fn
}

func (s *Session) SetInlineAIStreamResponder(fn func(ctx context.Context, ctxBefore, orig, draft, instruction string, onDelta func(string)) (string, error)) {
	s.inlineAIStreamResponder = fn
}

// --- Inline AI Session State ---

type aiStatusKind uint8

const (
	aiStatusInput aiStatusKind = iota
	aiStatusThinking
	aiStatusReview
)

type aiTargetPart struct {
	BlockID   model.BlockID
	StartRune int
	EndRune   int
	Source    string
}

type aiAsyncResult struct {
	Serial uint64
	Draft  string
	Err    string
}

type inlineAISession struct {
	TargetAnchor  Position
	TargetHead    Position
	Parts         []aiTargetPart
	Original      string
	ContextBefore string
	NotePath      string

	InputText        string
	InputHandle      ui.Handle
	FocusPending     bool
	SelectEndPending bool
	Status           aiStatusKind
	Error            string
	Prompt           string
	RestoreInput     string
	Partial          string
	Draft            string
	PrevDraft        string
	HasPrevDraft     bool
	Tick             int

	Serial        uint64
	Cancel        *atomic.Bool
	CancelCtx     context.CancelFunc
	StreamMu      sync.Mutex
	StreamPartial string
	ResultCh      chan aiAsyncResult
	AnchorRect    ui.Rect
	PreviewRect   ui.Rect
	CachedSource  string
	CachedBlocks  []model.Block
	HasCachedDoc  bool
}

func (ai *inlineAISession) isEmptyCtx() bool {
	return strings.TrimSpace(ai.Original) == ""
}

// SetAIEnabled updates editor AI actions and cancels an open inline rewrite when disabled.
func (s *Session) SetAIEnabled(enabled bool) {
	if s.aiDisabled == !enabled {
		return
	}
	s.aiDisabled = !enabled
	if s.blockMenu != nil && s.blockMenu.Mode == menuSlash {
		s.blockMenu = nil
	}
	if !enabled {
		s.aiClose()
	}
}

func (s *Session) IsInlineAIOpen() bool {
	return s.inlineAI != nil
}

func inlineAIInputPlaceholder(emptyCtx bool) string {
	if emptyCtx {
		return theme.Tr("写点什么，或直接点下面标签…", "Write something, or pick a tag below…")
	}
	return theme.Tr("怎么改这段话，或直接点下面标签…", "How to edit this, or pick a tag below…")
}

func inlineAIReviewPlaceholder() string {
	return theme.Tr("不满意？直接打字继续改，或按回车完成…", "Type to refine further, or press Enter to finish…")
}

func (s *Session) precedingContext(beforeIdx int) string {
	if s.doc == nil || beforeIdx <= 0 || beforeIdx > len(s.doc.Blocks) {
		return ""
	}
	var blocks []model.Block
	for i := 0; i < beforeIdx; i++ {
		blocks = append(blocks, s.doc.Blocks[i].Clone())
	}
	return strings.TrimSpace(s.serializeBlocks(blocks))
}

func (s *Session) OpenInlineAIOnSelection() {
	if s.aiDisabled || s.readOnly || !s.isMarkdownNote() || !s.canReplaceSelection() || !s.hasNonEmptySelection() {
		return
	}
	a, b := s.orderedSelection()
	var parts []aiTargetPart
	for idx := a.Block; idx <= b.Block && idx < len(s.doc.Blocks); idx++ {
		if idx == b.Block && a.Block != b.Block && b.Offset == 0 {
			break
		}
		lo, hi := s.selectionSpanRange(idx, a, b)
		if lo < hi || idx > a.Block || a.Block == b.Block {
			parts = append(parts, aiTargetPart{
				BlockID:   s.doc.Blocks[idx].ID,
				StartRune: lo,
				EndRune:   hi,
			})
		}
	}
	if len(parts) == 0 {
		return
	}
	orig := s.SelectedMarkdown()
	var anchor ui.Rect
	lastID := parts[len(parts)-1].BlockID
	if r, ok := s.blockRect(lastID); ok {
		anchor = r
	}
	s.startInlineAI(a, b, parts, orig, s.precedingContext(a.Block), anchor)
}

func (s *Session) OpenInlineAIOnBlock(idx int) {
	if s.aiDisabled || s.readOnly || !s.isMarkdownNote() || s.doc == nil || idx < 0 || idx >= len(s.doc.Blocks) {
		return
	}
	b := &s.doc.Blocks[idx]
	if b.Kind == model.BlockTable {
		return
	}
	text := b.PlainText()
	rlen := utf8.RuneCountInString(text)
	posA := Position{Block: idx, Offset: 0}
	posB := Position{Block: idx, Offset: rlen}
	orig := ""
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "/", "、", "/ai", "、ai":
		// The slash command stays intact until a suggestion is accepted.
	default:
		orig = strings.TrimSpace(s.serializeBlocks([]model.Block{b.Clone()}))
	}
	s.startInlineAI(posA, posB, []aiTargetPart{{BlockID: b.ID, StartRune: 0, EndRune: rlen}}, orig, s.precedingContext(idx), s.blockRectValue(b.ID))
}

func (s *Session) startInlineAI(anchor, head Position, parts []aiTargetPart, original, ctxBefore string, anchorRect ui.Rect) {
	s.cancelInlineAI()
	s.toolbarPop = ""
	s.blockMenu = nil
	s.calloutMenuBlock = 0
	s.selAnchor = head
	s.selHead = head
	for i := range parts {
		idx := s.blockIndexByID(parts[i].BlockID)
		parts[i].Source = s.serializeBlocks([]model.Block{s.doc.Blocks[idx].Clone()})
	}
	s.inlineAI = &inlineAISession{
		TargetAnchor:  anchor,
		TargetHead:    head,
		Parts:         parts,
		Original:      original,
		ContextBefore: ctxBefore,
		NotePath:      s.notePath,
		FocusPending:  true,
		Status:        aiStatusInput,
		Cancel:        &atomic.Bool{},
		AnchorRect:    anchorRect,
	}
}

func (s *Session) cancelInlineAI() {
	if s.inlineAI != nil {
		if s.inlineAI.Cancel != nil {
			s.inlineAI.Cancel.Store(true)
		}
		if s.inlineAI.CancelCtx != nil {
			s.inlineAI.CancelCtx()
			s.inlineAI.CancelCtx = nil
		}
	}
}

func (s *Session) pollInlineAI() {
	ai := s.inlineAI
	if ai == nil || ai.Status != aiStatusThinking {
		return
	}
	ai.Tick++
	ai.StreamMu.Lock()
	if ai.StreamPartial != "" && ai.StreamPartial != ai.Partial {
		ai.Partial = ai.StreamPartial
	}
	ai.StreamMu.Unlock()
	if ai.ResultCh == nil {
		return
	}
	select {
	case res := <-ai.ResultCh:
		s.applyInlineAIResult(res)
	default:
	}
}

func (s *Session) applyInlineAIResult(res aiAsyncResult) {
	ai := s.inlineAI
	if ai == nil || res.Serial != ai.Serial || ai.Status != aiStatusThinking {
		return
	}
	if res.Err != "" {
		ai.Error = res.Err
		if ai.HasPrevDraft {
			ai.Status = aiStatusReview
			ai.Draft = ai.PrevDraft
		} else {
			ai.Status = aiStatusInput
		}
		ai.InputText = ai.RestoreInput
		ai.FocusPending = true
		ai.SelectEndPending = true
		return
	}
	clean := cleanInlineAIMarkdown(res.Draft)
	if clean == "" {
		ai.Error = theme.Tr("AI 未返回可写入的内容。", "AI returned empty content.")
		if ai.HasPrevDraft {
			ai.Status = aiStatusReview
			ai.Draft = ai.PrevDraft
		} else {
			ai.Status = aiStatusInput
		}
		ai.InputText = ai.RestoreInput
		ai.FocusPending = true
		ai.SelectEndPending = true
		return
	}
	ai.Error = ""
	ai.Draft = clean
	ai.Partial = clean
	ai.PrevDraft = ""
	ai.HasPrevDraft = false
	ai.Status = aiStatusReview
	ai.FocusPending = true
}

func (s *Session) aiSubmitInput() {
	ai := s.inlineAI
	if ai == nil || ai.Status == aiStatusThinking {
		return
	}
	isReview := ai.Status == aiStatusReview
	scope := PromptScopeEdit
	if !isReview && ai.isEmptyCtx() {
		scope = PromptScopeEmpty
	}
	text := strings.TrimSpace(ai.InputText)
	if text == "" {
		if isReview {
			s.aiAccept()
		}
		return
	}
	s.ensurePromptStore().record(scope, text)
	presets := editPresets
	if scope == PromptScopeEmpty {
		presets = emptyPresets
	}
	instruction := text
	for _, p := range presets {
		if p.MatchesInput(text) {
			instruction = p.Prompt()
			break
		}
	}
	s.aiRun(instruction, text, true)
}

func (s *Session) aiRunPromptTarget(target promptTarget) {
	ai := s.inlineAI
	if ai == nil || ai.Status == aiStatusThinking {
		return
	}
	if target.Kind == "preset" {
		s.aiRun(target.Preset.Prompt(), ai.InputText, false)
		return
	}
	clean := strings.TrimSpace(target.Text)
	if clean != "" {
		s.aiRun(clean, ai.InputText, false)
	}
}

func (s *Session) aiTogglePromptStar(scope PromptScope, target promptTarget) {
	s.ensurePromptStore().toggleStar(scope, target)
}

func (s *Session) aiRetry() {
	ai := s.inlineAI
	if ai == nil || strings.TrimSpace(ai.Prompt) == "" {
		return
	}
	s.aiRun(ai.Prompt, ai.RestoreInput, false)
}

func (s *Session) aiStop() {
	ai := s.inlineAI
	if ai == nil {
		return
	}
	s.cancelInlineAI()
	ai.Serial++
	ai.Error = ""
	if ai.HasPrevDraft {
		ai.Status = aiStatusReview
		ai.Draft = ai.PrevDraft
		ai.HasPrevDraft = false
	} else {
		ai.Status = aiStatusInput
	}
	ai.InputText = ai.RestoreInput
	ai.FocusPending = true
	ai.SelectEndPending = true
}

func (s *Session) aiEscape() {
	ai := s.inlineAI
	if ai == nil {
		return
	}
	if ai.Status == aiStatusThinking {
		s.aiStop()
		return
	}
	s.aiClose()
}

func (s *Session) aiClose() {
	ai := s.inlineAI
	if ai == nil {
		return
	}
	anchor, head := ai.TargetAnchor, ai.TargetHead
	s.cancelInlineAI()
	s.inlineAI = nil
	if s.doc != nil && anchor.Block >= 0 && anchor.Block < len(s.doc.Blocks) && head.Block >= 0 && head.Block < len(s.doc.Blocks) {
		s.selAnchor = s.clampPos(anchor)
		s.selHead = s.clampPos(head)
		s.focusedBlock = s.selHead.Block
		s.selSyncPending = true
	}
	s.focusPending = true
}

func (s *Session) aiRun(instruction, restoreInput string, refineExisting bool) {
	ai := s.inlineAI
	if ai == nil || !s.checkInlineAITarget() {
		return
	}
	s.cancelInlineAI()
	cancel := &atomic.Bool{}
	runCtx, cancelCtx := context.WithCancel(context.Background())
	ai.Cancel = cancel
	ai.CancelCtx = cancelCtx
	ai.Serial++
	serial := ai.Serial
	ai.Prompt = instruction
	ai.RestoreInput = restoreInput
	ai.Error = ""
	prevDraft := ""
	hasPrev := false
	if ai.Status == aiStatusReview && refineExisting && ai.Draft != "" {
		prevDraft = ai.Draft
		hasPrev = true
	}
	ai.PrevDraft = prevDraft
	ai.HasPrevDraft = hasPrev
	ai.Partial = ""
	ai.StreamMu.Lock()
	ai.StreamPartial = ""
	ai.StreamMu.Unlock()
	ai.Tick = 0
	ai.Status = aiStatusThinking
	ai.InputText = ""

	if s.inlineAIStreamResponder == nil && s.inlineAIResponder == nil {
		cancelCtx()
		ai.CancelCtx = nil
		out := offlineInlineAI(ai.ContextBefore, ai.Original, prevDraft, instruction)
		s.applyInlineAIResult(aiAsyncResult{Serial: serial, Draft: out})
		return
	}

	resCh := make(chan aiAsyncResult, 1)
	ai.ResultCh = resCh
	streamResponder := s.inlineAIStreamResponder
	responder := s.inlineAIResponder
	ctxBefore := ai.ContextBefore
	orig := ai.Original

	go func() {
		defer cancelCtx()
		if streamResponder != nil {
			var acc strings.Builder
			var redrawMu sync.Mutex
			var lastRedraw time.Time
			var trailingTimer *time.Timer
			const streamRedrawInterval = 16 * time.Millisecond
			scheduleStreamRedraw := func() {
				redrawMu.Lock()
				elapsed := time.Since(lastRedraw)
				if elapsed >= streamRedrawInterval {
					lastRedraw = time.Now()
					if trailingTimer != nil {
						trailingTimer.Stop()
						trailingTimer = nil
					}
					redrawMu.Unlock()
					s.requestRedraw()
					return
				}
				if trailingTimer == nil {
					wait := streamRedrawInterval - elapsed
					trailingTimer = time.AfterFunc(wait, func() {
						redrawMu.Lock()
						trailingTimer = nil
						lastRedraw = time.Now()
						redrawMu.Unlock()
						if !cancel.Load() {
							s.requestRedraw()
						}
					})
				}
				redrawMu.Unlock()
			}
			stopStreamTimer := func() {
				redrawMu.Lock()
				if trailingTimer != nil {
					trailingTimer.Stop()
					trailingTimer = nil
				}
				redrawMu.Unlock()
			}
			defer stopStreamTimer()

			out, err := streamResponder(runCtx, ctxBefore, orig, prevDraft, instruction, func(chunk string) {
				if cancel.Load() || chunk == "" {
					return
				}
				acc.WriteString(chunk)
				cur := acc.String()
				ai.StreamMu.Lock()
				ai.StreamPartial = cur
				ai.StreamMu.Unlock()
				scheduleStreamRedraw()
			})
			stopStreamTimer()
			if cancel.Load() || runCtx.Err() == context.Canceled {
				return
			}
			if err != nil {
				resCh <- aiAsyncResult{Serial: serial, Err: err.Error()}
			} else {
				resCh <- aiAsyncResult{Serial: serial, Draft: out}
			}
			s.requestRedraw()
			return
		}
		out := responder(ctxBefore, orig, prevDraft, instruction)
		if cancel.Load() {
			return
		}
		resCh <- aiAsyncResult{Serial: serial, Draft: out}
		s.requestRedraw()
	}()

	// Fast synchronous mocks/responders settle immediately without flashing Thinking state;
	// slow providers fall into Thinking so the user can cancel with Esc / Stop and restore input.
	select {
	case res := <-resCh:
		s.applyInlineAIResult(res)
	case <-time.After(15 * time.Millisecond):
	}
}

func (s *Session) validateInlineAITarget() bool {
	ai := s.inlineAI
	if ai == nil || s.aiDisabled || s.readOnly || s.notePath != ai.NotePath || s.doc == nil || len(ai.Parts) == 0 {
		return false
	}
	first := s.blockIndexByID(ai.Parts[0].BlockID)
	if first < 0 || first+len(ai.Parts) > len(s.doc.Blocks) {
		return false
	}
	for i, part := range ai.Parts {
		block := &s.doc.Blocks[first+i]
		if block.ID != part.BlockID || part.Source != s.serializeBlocks([]model.Block{block.Clone()}) {
			return false
		}
	}
	return true
}

func (s *Session) checkInlineAITarget() bool {
	if s.validateInlineAITarget() {
		return true
	}
	s.inlineAI.Error = theme.Tr("原文或选区已变化，请重新选择后再试。", "The text or selection target changed. Select it again.")
	return false
}

func (s *Session) aiAccept() {
	ai := s.inlineAI
	if ai == nil || ai.Status != aiStatusReview {
		return
	}
	if !s.checkInlineAITarget() {
		return
	}
	firstIdx := s.blockIndexByID(ai.Parts[0].BlockID)
	lastIdx := s.blockIndexByID(ai.Parts[len(ai.Parts)-1].BlockID)
	s.selAnchor = Position{Block: firstIdx, Offset: ai.Parts[0].StartRune}
	s.selHead = Position{Block: lastIdx, Offset: ai.Parts[len(ai.Parts)-1].EndRune}
	if !s.ReplaceAIMarkdown(ai.Draft) {
		ai.Error = theme.Tr("无法在当前选区应用此格式，请选择完整块后重试", "Select whole blocks to apply this format")
		return
	}
	s.inlineAI = nil
	s.focusPending = true
	s.selSyncPending = true
}

func (s *Session) aiInsertBelow() {
	ai := s.inlineAI
	if ai == nil || ai.Status != aiStatusReview || !s.checkInlineAITarget() {
		return
	}
	lastIdx := s.blockIndexByID(ai.Parts[len(ai.Parts)-1].BlockID)
	if lastIdx < 0 {
		return
	}
	parsed := s.parseMarkdownDoc(ai.Draft)
	if parsed == nil || len(parsed.Blocks) == 0 {
		return
	}
	blocks := make([]model.Block, len(parsed.Blocks))
	for i, blk := range parsed.Blocks {
		blk.ID = model.NewBlockID()
		if blk.Language == "yaml-frontmatter" {
			blk.Language = "yaml"
		}
		blk.Dirty = true
		blk.CleanSource = ""
		blocks[i] = blk
	}
	s.pushUndo()
	insertAt := lastIdx + 1
	s.spliceBlocks(insertAt, insertAt, blocks)
	endIdx := insertAt + len(blocks) - 1
	endOff := len([]rune(s.doc.Blocks[endIdx].PlainText()))
	s.focusedBlock = endIdx
	s.selAnchor = Position{Block: endIdx, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	s.inlineAI = nil
}

// ReplaceAIMarkdown replaces the current selection with Markdown-parsed native blocks in one undo step.
func (s *Session) ReplaceAIMarkdown(source string) bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 || !s.canReplaceSelection() {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block >= len(s.doc.Blocks) {
		return false
	}
	first, last := a.Block, b.Block
	if first != last && s.crossesHiddenFold(first, last) && !s.selectionCoversWholeDoc(a, b) {
		return false
	}

	norm := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	startBlk := &s.doc.Blocks[first]

	// Literal replacement in code blocks or empty replacement.
	if (first == last && (startBlk.Kind == model.BlockCode || startBlk.Kind == model.BlockRawHTML)) || norm == "" {
		s.pushUndo()
		s.replaceRangeWithPlain(a, b, norm)
		return true
	}

	parsed := s.parseMarkdownDoc(norm)
	if parsed == nil || len(parsed.Blocks) == 0 {
		return false
	}
	blocks := make([]model.Block, len(parsed.Blocks))
	for i, blk := range parsed.Blocks {
		blk.ID = model.NewBlockID()
		blk.Dirty = true
		blk.CleanSource = ""
		blocks[i] = blk
	}

	isInline := len(blocks) == 1 && blocks[0].Kind == model.BlockParagraph && blocks[0].Indent == 0

	// Single-cell table replacement: inline only, preserving cell boundaries.
	if first == last && startBlk.Kind == model.BlockTable && startBlk.Table != nil {
		rlen := len([]rune(startBlk.PlainText()))
		if a.Offset != 0 || b.Offset != rlen {
			row, col, relLo, relHi, ok := tableCellAtRuneRange(startBlk.Table, a.Offset, b.Offset)
			if !ok {
				return false
			}
			if !isInline {
				return false
			}
			s.pushUndo()
			cell := &startBlk.Table.Rows[row][col]
			cellRunes := len([]rune(cell.PlainText()))
			var merged []model.Span
			merged = append(merged, sliceSpans(cell.Spans, 0, relLo)...)
			merged = append(merged, blocks[0].Spans...)
			merged = append(merged, sliceSpans(cell.Spans, relHi, cellRunes)...)
			cell.Spans = normalizeSpans(merged)
			startBlk.Dirty = true
			startBlk.CleanSource = ""
			newOffset := a.Offset + len([]rune(blocks[0].PlainText()))
			s.focusedBlock = first
			s.selAnchor = Position{Block: first, Offset: newOffset}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.markDirty()
			return true
		}
	}

	// When a cross-block selection stops at offset 0 of a trailing code/table/atomic block,
	// zero content of that block is selected — keep it untouched outside the splice range.
	effectiveLast := last
	effectiveEndOff := b.Offset
	if first < last && b.Offset == 0 {
		tk := s.doc.Blocks[last].Kind
		if tk == model.BlockCode || tk == model.BlockRawHTML || tk == model.BlockTable || tk == model.BlockImage || tk == model.BlockDivider {
			effectiveLast = last - 1
			effectiveEndOff = len([]rune(s.doc.Blocks[effectiveLast].PlainText()))
		}
	}

	// Inline replacement across rich-text blocks (no tables, atomic blocks, or code blocks).
	allRich := true
	for i := first; i <= effectiveLast; i++ {
		k := s.doc.Blocks[i].Kind
		if k == model.BlockTable || k == model.BlockImage || k == model.BlockDivider || k == model.BlockCode || k == model.BlockRawHTML {
			allRich = false
			break
		}
	}
	if isInline && allRich {
		s.pushUndo()
		prefix := sliceSpans(s.doc.Blocks[first].Spans, 0, a.Offset)
		lastRunes := len([]rune(s.doc.Blocks[effectiveLast].PlainText()))
		suffix := sliceSpans(s.doc.Blocks[effectiveLast].Spans, effectiveEndOff, lastRunes)
		var merged []model.Span
		merged = append(merged, prefix...)
		merged = append(merged, blocks[0].Spans...)
		merged = append(merged, suffix...)
		updated := s.doc.Blocks[first].Clone()
		updated.Spans = normalizeSpans(merged)
		updated.Dirty = true
		updated.CleanSource = ""
		s.spliceBlocks(first, effectiveLast+1, []model.Block{updated})
		caret := a.Offset + len([]rune(blocks[0].PlainText()))
		s.focusedBlock = first
		s.selAnchor = Position{Block: first, Offset: caret}
		s.selHead = s.selAnchor
		s.focusPending = true
		s.selSyncPending = true
		s.markDirty()
		return true
	}

	// Structural multi-block splice.
	s.pushUndo()
	var inserted []model.Block
	if a.Offset > 0 && first < len(s.doc.Blocks) {
		firstBlk := &s.doc.Blocks[first]
		if firstBlk.Kind == model.BlockCode || firstBlk.Kind == model.BlockRawHTML {
			runes := []rune(firstBlk.Code)
			cut := clampInt(a.Offset, 0, len(runes))
			if cut > 0 {
				pb := firstBlk.Clone()
				pb.Code = string(runes[:cut])
				pb.Dirty = true
				pb.CleanSource = ""
				inserted = append(inserted, pb)
			}
		} else if firstBlk.Kind == model.BlockTable && firstBlk.Table != nil {
			total := len([]rune(firstBlk.PlainText()))
			cut := clampInt(a.Offset, 0, total)
			if cut > 0 {
				pb := firstBlk.Clone()
				clearTableRange(pb.Table, cut, total, "")
				pb.Dirty = true
				pb.CleanSource = ""
				inserted = append(inserted, pb)
			}
		} else if firstBlk.Kind != model.BlockTable {
			prefix := sliceSpans(firstBlk.Spans, 0, a.Offset)
			if len(prefix) > 0 {
				pb := firstBlk.Clone()
				pb.Spans = prefix
				pb.Dirty = true
				pb.CleanSource = ""
				inserted = append(inserted, pb)
			}
		}
	}
	inserted = append(inserted, blocks...)
	pastedEndIdx := first + len(inserted) - 1
	if effectiveLast < len(s.doc.Blocks) {
		lastBlk := &s.doc.Blocks[effectiveLast]
		if lastBlk.Kind == model.BlockCode || lastBlk.Kind == model.BlockRawHTML {
			runes := []rune(lastBlk.Code)
			cut := clampInt(effectiveEndOff, 0, len(runes))
			if cut < len(runes) {
				sb := lastBlk.Clone()
				sb.ID = model.NewBlockID()
				sb.Code = string(runes[cut:])
				sb.Dirty = true
				sb.CleanSource = ""
				inserted = append(inserted, sb)
			}
		} else if lastBlk.Kind == model.BlockTable && lastBlk.Table != nil {
			total := len([]rune(lastBlk.PlainText()))
			cut := clampInt(effectiveEndOff, 0, total)
			if cut < total {
				sb := lastBlk.Clone()
				sb.ID = model.NewBlockID()
				clearTableRange(sb.Table, 0, cut, "")
				sb.Dirty = true
				sb.CleanSource = ""
				inserted = append(inserted, sb)
			}
		} else if lastBlk.Kind != model.BlockTable {
			lastRunes := len([]rune(lastBlk.PlainText()))
			if effectiveEndOff < lastRunes {
				suffix := sliceSpans(lastBlk.Spans, effectiveEndOff, lastRunes)
				if len(suffix) > 0 {
					sb := model.Block{ID: model.NewBlockID(), Kind: model.BlockParagraph, Spans: suffix, Dirty: true}
					inserted = append(inserted, sb)
				}
			}
		}
	}
	s.spliceBlocks(first, effectiveLast+1, inserted)
	endIdx := clampInt(pastedEndIdx, 0, len(s.doc.Blocks)-1)
	endOff := len([]rune(s.doc.Blocks[endIdx].PlainText()))
	s.focusedBlock = endIdx
	s.selAnchor = Position{Block: endIdx, Offset: endOff}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
	return true
}

func (s *Session) replaceRangeWithPlain(a, b Position, plain string) {
	first, last := a.Block, b.Block
	if first == last {
		blk := &s.doc.Blocks[first]
		if blk.Kind == model.BlockCode || blk.Kind == model.BlockRawHTML {
			runes := []rune(blk.Code)
			lo := clampInt(a.Offset, 0, len(runes))
			hi := clampInt(b.Offset, lo, len(runes))
			ins := []rune(plain)
			next := append(append(slices.Clone(runes[:lo]), ins...), runes[hi:]...)
			blk.SetText(string(next))
			s.focusedBlock = first
			s.selAnchor = Position{Block: first, Offset: lo + len(ins)}
			s.selHead = s.selAnchor
			s.focusPending = true
			s.selSyncPending = true
			s.markDirty()
			return
		}
	}
	prefix := sliceSpans(s.doc.Blocks[first].Spans, 0, a.Offset)
	lastRunes := len([]rune(s.doc.Blocks[last].PlainText()))
	suffix := sliceSpans(s.doc.Blocks[last].Spans, b.Offset, lastRunes)
	var merged []model.Span
	merged = append(merged, prefix...)
	if plain != "" {
		merged = append(merged, model.Span{Text: plain})
	}
	merged = append(merged, suffix...)
	updated := s.doc.Blocks[first].Clone()
	updated.Spans = normalizeSpans(merged)
	updated.Dirty = true
	updated.CleanSource = ""
	s.spliceBlocks(first, last+1, []model.Block{updated})
	caret := a.Offset + len([]rune(plain))
	s.focusedBlock = first
	s.selAnchor = Position{Block: first, Offset: caret}
	s.selHead = s.selAnchor
	s.focusPending = true
	s.selSyncPending = true
	s.markDirty()
}

func cleanInlineAIMarkdown(raw string) string {
	trimmed := strings.TrimLeft(raw, " \t\r\n")
	if strings.HasPrefix(trimmed, "```") {
		rest := strings.TrimPrefix(trimmed, "```")
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			lang := strings.ToLower(strings.TrimSpace(rest[:nl]))
			if lang == "" || lang == "markdown" || lang == "md" {
				body := strings.TrimRight(rest[nl+1:], " \t\r\n")
				body = strings.TrimSuffix(body, "```")
				return strings.TrimSpace(body)
			}
		}
	}
	return strings.TrimSpace(raw)
}

func offlineInlineAI(ctxBefore, orig, prevDraft, instruction string) string {
	inst := strings.TrimSpace(instruction)
	src := strings.TrimSpace(orig)
	if strings.TrimSpace(prevDraft) != "" {
		src = strings.TrimSpace(prevDraft)
	}

	if src == "" {
		topic := lastNonEmptyLine(ctxBefore)
		if topic == "" {
			topic = theme.Tr("当前主题", "this topic")
		}
		switch {
		case PresetContinue.MatchesInput(inst) || strings.Contains(inst, "续写"):
			return theme.Tr(
				"在此基础上，可以进一步明确关键步骤与预期产出，确保后续执行清晰可追踪。",
				"From here, spell out the next steps and the expected result so the work stays easy to follow.",
			)
		case PresetOutline.MatchesInput(inst) || strings.Contains(inst, "大纲"):
			return fmt.Sprintf(theme.Tr(
				"### 背景与目标\n- 明确围绕「%s」的核心目标\n\n### 核心方案\n- 拆解关键里程碑与交付物",
				"### Background and goal\n- State the goal for %s\n\n### Approach\n- Break out the milestones and deliverables",
			), topic)
		case PresetTasks.MatchesInput(inst) || strings.Contains(inst, "待办"):
			return theme.Tr(
				"- [ ] 梳理核心需求与边界\n- [ ] 完成第一版实现并自测\n- [ ] 复核细节并整理结论",
				"- [ ] List the requirements and boundaries\n- [ ] Build a first version and try it\n- [ ] Review the details and write up the result",
			)
		case PresetBrainstorm.MatchesInput(inst) || strings.Contains(inst, "头脑风暴"):
			return theme.Tr(
				"- 从用户高频场景切入，减少前置配置成本\n- 优先打磨核心闭环体验\n- 提供可逆操作与清晰反馈\n- 结合示例模板降低上手门槛",
				"- Start from the most common task and cut setup work\n- Polish the core loop first\n- Make actions reversible and the feedback clear\n- Add an example template so it is easier to start",
			)
		default:
			return inst
		}
	}

	switch {
	case PresetSimplify.MatchesInput(inst) || strings.Contains(inst, "精简"):
		lines := strings.Split(src, "\n")
		if len(lines) > 1 {
			return strings.TrimSpace(lines[0])
		}
		runes := []rune(src)
		if len(runes) > 36 {
			return strings.TrimSpace(string(runes[:36])) + theme.Tr("。", ".")
		}
		return src
	case PresetTranslate.MatchesInput(inst) || strings.Contains(inst, "译") || strings.Contains(strings.ToLower(inst), "translate"):
		if containsCJK(src) {
			return "[EN] " + src
		}
		return theme.Tr("[译] ", "[ZH] ") + src
	case PresetPlain.MatchesInput(inst) || strings.Contains(inst, "通俗"):
		return strings.TrimSpace(src) + theme.Tr("（简言之：核心意思保持不变，表达更直观）", " (in short: same meaning, plainer wording)")
	case PresetPolish.MatchesInput(inst) || strings.Contains(inst, "润色"):
		return strings.TrimSpace(src)
	default:
		if strings.TrimSpace(prevDraft) != "" {
			return strings.TrimSpace(prevDraft) + "\n\n" + inst
		}
		return strings.TrimSpace(src)
	}
}

func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(strings.TrimLeft(lines[i], "#>-* "))
		if t != "" {
			if len([]rune(t)) > 24 {
				return string([]rune(t)[:24])
			}
			return t
		}
	}
	return ""
}

// --- Paper Live Preview Rendering ---

func (s *Session) paintInlineAIOutline(c *ui.Context, el ui.Element, blockID model.BlockID, topGap float32) {
	ai := s.inlineAI
	if ai == nil || ai.Status != aiStatusInput {
		return
	}
	isPart := false
	for _, p := range ai.Parts {
		if p.BlockID == blockID {
			isPart = true
			break
		}
	}
	if !isPart {
		return
	}
	pal := theme.Ocean()
	leftGutter := s.leftContentGutter()
	rightInset := s.rightContentInset()
	bleed := s.bandBleed()
	padY := s.scale(3)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		ring := ui.Rect{
			X: r.X + leftGutter - bleed,
			Y: r.Y + topGap - padY,
			W: max(r.W-leftGutter-rightInset+2*bleed, 0),
			H: max(r.H-topGap+2*padY, 0),
		}
		p.StrokeDashed(ring, pal.Accent.Alpha(0.85), 10, 1)
		if ai.Parts[0].BlockID == blockID {
			ai.PreviewRect = ring
		} else {
			ai.PreviewRect = unionRect(ai.PreviewRect, ring)
		}
		appUi.RecordBounds("inline-ai-preview", ai.PreviewRect)
	})
}

func (s *Session) renderInlineAIPreviewRow(c *ui.Context, idx int, readOnly bool) (handled bool, hovered bool) {
	ai := s.inlineAI
	if ai == nil || (ai.Status != aiStatusThinking && ai.Status != aiStatusReview) || len(ai.Parts) == 0 {
		return false, false
	}
	firstIdx := s.blockIndexByID(ai.Parts[0].BlockID)
	lastIdx := s.blockIndexByID(ai.Parts[len(ai.Parts)-1].BlockID)
	if firstIdx < 0 || lastIdx < 0 {
		return false, false
	}
	lo, hi := min(firstIdx, lastIdx), max(firstIdx, lastIdx)
	if idx < lo || idx > hi {
		return false, false
	}
	if idx > lo {
		// Merged into the lead preview row.
		return true, false
	}

	pal := theme.Ocean()
	tintBG := pal.Editor().InlineAIDiffTint
	delFG := pal.DiffDelFG
	delBG := pal.DiffDelBG

	source := ai.Draft
	if ai.Status == aiStatusThinking {
		source = cleanInlineAIMarkdown(ai.Partial)
	}
	var parsedBlocks []model.Block
	if strings.TrimSpace(source) != "" {
		if ai.HasCachedDoc && ai.CachedSource == source {
			parsedBlocks = ai.CachedBlocks
		} else {
			if doc := s.parseMarkdownDoc(source); doc != nil {
				parsedBlocks = doc.Blocks
			}
			ai.CachedSource = source
			ai.CachedBlocks = parsedBlocks
			ai.HasCachedDoc = true
		}
	} else {
		ai.CachedSource = ""
		ai.CachedBlocks = nil
		ai.HasCachedDoc = true
	}

	outerMaxW := s.outerFlowWidth()
	bleed := s.bandBleed()
	row := ui.Row(c.Key(fmt.Sprintf("ai-preview-%d", ai.Parts[0].BlockID))).
		FillWidth().MaxWidth(outerMaxW).Padding(6, max(s.rightContentInset()-bleed, 0), 6, max(s.leftContentGutter()-bleed, 0))
	s.recordBlockLayoutBounds(ai.Parts[0].BlockID, row)
	row.Children(func() {
		box := ui.Column(c.Key("inline-ai-preview-box")).FillWidth().MaxWidth(s.standardWidth()+2*bleed).
			Padding(8, 10).Gap(0).Radius(theme.RadiusLG).Background(tintBG)
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			p.StrokeDashed(r, pal.Accent, 10, 1)
		})
		box.DrawOver(func(_ *ui.Painter, r ui.Rect) {
			if ai.PreviewRect != r {
				ai.PreviewRect = r
				c.Invalidate()
			}
			appUi.RecordBounds("inline-ai-preview", r)
		})

		box.Children(func() {
			if !ai.isEmptyCtx() {
				for pIdx, part := range ai.Parts {
					bIdx := s.blockIndexByID(part.BlockID)
					if bIdx < 0 {
						continue
					}
					blk := &s.doc.Blocks[bIdx]
					txt := blk.PlainText()
					if txt == "" {
						continue
					}
					runes := []rune(txt)
					s0 := clampInt(part.StartRune, 0, len(runes))
					s1 := clampInt(part.EndRune, s0, len(runes))
					if s0 == s1 {
						continue
					}
					frag := string(runes[s0:s1])
					ui.Box(c.Key(fmt.Sprintf("ai-struck-%d", pIdx))).FillWidth().Padding(2, 6).Margin(0, 0, 4, 0).Radius(theme.RadiusSM).Background(delBG).
						Children(func() {
							ui.Text(c, frag).FontSize(s.scale(theme.FontEditor)).TextColor(delFG).Strikethrough().FillWidth()
						})
				}
			}

			if len(parsedBlocks) == 0 && ai.Status == aiStatusThinking {
				ui.Row(c).Height(24).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(2).Height(18).Radius(theme.RadiusXS).Background(pal.Accent)
				})
			}

			numByIndent := make(map[int]int)
			for bIdx := range parsedBlocks {
				blk := &parsedBlocks[bIdx]
				for ind := range numByIndent {
					if ind > blk.Indent {
						delete(numByIndent, ind)
					}
				}
				numRun := 0
				if blk.Kind == model.BlockNumbered {
					if blk.ListIndex > 0 && numByIndent[blk.Indent] == 0 {
						numByIndent[blk.Indent] = blk.ListIndex
					} else {
						numByIndent[blk.Indent]++
					}
					numRun = numByIndent[blk.Indent]
				} else if !blk.Kind.IsList() && blk.Indent <= 0 {
					clear(numByIndent)
				}
				var prevBlk *model.Block
				if bIdx > 0 {
					prevBlk = &parsedBlocks[bIdx-1]
				}
				topGap := blockGutterSpacing(prevBlk, blk)
				if bIdx == 0 {
					topGap = 0
				}
				ui.Box(c.Key(fmt.Sprintf("aip-slot-%d", bIdx))).FillWidth().Padding(topGap, 0, 0, 0).Children(func() {
					s.renderPreviewNativeBlock(c, bIdx, blk, numRun)
				})
			}
		})
	})
	_ = readOnly
	return true, row.Hovered()
}

func (s *Session) renderPreviewNativeBlock(c *ui.Context, idx int, b *model.Block, listNum int) {
	pal := theme.Ocean()
	fontSize, weight, lh := s.textMetrics(b.Kind)
	switch b.Kind {
	case model.BlockDivider:
		ui.Box(c.Key(fmt.Sprintf("aip-hr-%d", idx))).FillWidth().Height(1).MarginY(theme.Space6).Background(pal.Hairline)
	case model.BlockCode, model.BlockRawHTML:
		ui.Column(c.Key(fmt.Sprintf("aip-code-%d", idx))).FillWidth().Padding(theme.Space10, theme.Space12).Radius(theme.RadiusSM).
			Background(pal.CodeBlockBG).Children(func() {
			ui.Text(c, b.Code).Font(resolvedCodeFont()).FontSize(theme.FontBody).LineHeight(1.5).TextColor(pal.Text).FillWidth()
		})
	case model.BlockCallout:
		st := calloutTone(b.Callout, theme.IsDark())
		card := ui.Box(c.Key(fmt.Sprintf("aip-callout-%d", idx))).FillWidth().
			Padding(38, theme.Space16, theme.Space12, 44).Radius(theme.RadiusLG).Background(st.BG).Border(1, st.Border)
		card.Children(func() {
			ui.Row(c).Absolute().Left(theme.Space12).Top(theme.Space6).Height(theme.HeightChrome).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
				calloutIC := pal.TintedIconColors(st.Accent)
				calloutIC.Fill = st.IconFill
				icons.Draw(c, icons.Callout, 16, calloutIC)
				ui.Text(c, calloutLabel(b.Callout)).FontSize(theme.FontBody).FontWeight(theme.WeightControl).TextColor(st.Text).SingleLine()
			})
			ui.RichText(c, s.buildDisplaySpans(0, b, fontSize, weight)...).FontSize(fontSize).LineHeight(lh).TextColor(pal.Text).FillWidth()
		})
	case model.BlockTable:
		if b.Table == nil || len(b.Table.Rows) == 0 || len(b.Table.Rows[0]) == 0 {
			return
		}
		grid := appUi.TableGrid(c.Key(fmt.Sprintf("aip-tbl-%d", idx)), pal.Table().Border).FillWidth()
		renderPlainTableRows(c, grid, b.Table.Rows, theme.FontBody, 1, model.TableCell.PlainText)
	case model.BlockBullet, model.BlockNumbered, model.BlockTodo:
		depth := min(max(b.Indent, 0)/2, 3)
		indentPx := float32(min(max(b.Indent, 0), 12)) * editorIndentStep
		lineH := fontSize * lh
		ui.Row(c.Key(fmt.Sprintf("aip-list-%d", idx))).FillWidth().AlignItems(ui.Start).Children(func() {
			marker := ui.Row(c).Width(editorMarkerWidth+indentPx).Height(lineH).Shrink(0).Padding(0, 0, 0, indentPx)
			if b.Kind == model.BlockTodo {
				top := max((lineH-16)*0.5, 0)
				marker.AlignItems(ui.Start).Children(func() {
					cb := ui.Box(c).Margin(top, 0, 0, 4.8).Size(16, 16).Center().Radius(theme.RadiusSM).
						Background(pal.TodoBG).Border(1, pal.TodoRing)
					if b.Checked {
						cb.Children(func() {
							icons.Draw(c, icons.Check, theme.IconMicro, pal.MinimalIconColors(pal.Accent))
						})
					}
				})
			} else {
				marker.Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if b.Kind == model.BlockNumbered {
						col := pal.TextSecondary
						if depth == 0 {
							col = pal.Accent
						}
						ui.Text(c, fmt.Sprintf("%d", max(listNum, 1))).Font(resolvedCodeFont()).
							FontSize(fontSize*0.84).FontWeight(theme.WeightMono).TextColor(col).Padding(0, 8, 0, 0).SingleLine()
					} else {
						d := float32(5)
						if depth == 0 {
							d = 6
						}
						ui.Row(c).Height(lineH).AlignItems(ui.Center).Padding(0, 11-d/2, 0, 0).Children(func() {
							dot := ui.Box(c).Size(d, d).Radius(d / 2)
							switch depth {
							case 0:
								dot.Background(pal.Accent)
							case 1:
								dot.Background(pal.ListBullet)
							default:
								dot.Border(1.25, pal.ListBullet)
							}
						})
					}
				})
			}
			spans := s.buildDisplaySpans(0, b, fontSize, weight)
			if len(spans) == 0 {
				ui.Text(c, " ").FontSize(fontSize).LineHeight(lh).Grow(1).MinWidth(0)
			} else {
				ui.RichText(c, spans...).FontSize(fontSize).FontWeight(weight).LineHeight(lh).TextColor(pal.Text).Grow(1).MinWidth(0)
			}
		})
	default:
		wrap := ui.Column(c.Key(fmt.Sprintf("aip-txt-%d", idx))).FillWidth()
		if b.Kind == model.BlockQuote {
			wrap.Padding(2, 0, 2, 16).BorderWidth(0, 0, 0, 3).BorderColor(pal.QuoteBar)
		}
		wrap.Children(func() {
			spans := s.buildDisplaySpans(0, b, fontSize, weight)
			if len(spans) == 0 {
				ui.Text(c, " ").FontSize(fontSize).LineHeight(lh).FillWidth()
			} else {
				ui.RichText(c, spans...).FontSize(fontSize).FontWeight(weight).LineHeight(lh).TextColor(pal.Text).FillWidth()
			}
		})
	}
}

// --- Floating Inline AI Panel ---

func (s *Session) renderInlineAIPanel(c *ui.Context) {
	ai := s.inlineAI
	if ai == nil {
		return
	}
	pal := theme.Ocean()
	busy := ai.Status == aiStatusThinking
	emptyCtx := ai.isEmptyCtx()

	vp, winW, winH := s.overlayViewport(c)
	var trigger ui.Rect
	if ai.PreviewRect.W > 0 && ai.PreviewRect.H > 0 {
		trigger = ai.PreviewRect
	} else {
		for _, part := range ai.Parts {
			if r, ok := s.blockRect(part.BlockID); ok && r.W > 0 && r.H > 0 {
				content := ui.Rect{
					X: r.X + s.leftContentGutter(),
					Y: r.Y,
					W: max(r.W-s.leftContentGutter()-s.rightContentInset(), 120),
					H: r.H,
				}
				trigger = unionRect(trigger, content)
			}
		}
	}
	if trigger.W <= 0 || trigger.H <= 0 {
		trigger = ai.AnchorRect
	}
	if trigger.W <= 0 || trigger.H <= 0 {
		trigger = ui.Rect{X: max(vp.X+s.leftContentGutter(), 68), Y: vp.Y + 100, W: inlineAIPanelWidth, H: 24}
	}

	panelW := min(inlineAIPanelWidth, max(vp.W-24, 260))
	estH := float32(140)
	switch ai.Status {
	case aiStatusThinking:
		estH = theme.HeightLarge
	case aiStatusReview:
		estH = theme.HeightLarge + 1 + theme.HeightLarge
	case aiStatusInput:
		scope := PromptScopeEdit
		if emptyCtx {
			scope = PromptScopeEmpty
		}
		entries := s.ensurePromptStore().list(scope)
		if len(entries) == 0 {
			estH = theme.HeightLarge
		} else {
			rows := chipRowIndices(entries)
			numRows := min(rows[len(rows)-1]+1, inlineAIMaxChipRows)
			chipsH := float32(numRows)*inlineAIChipH + float32(max(numRows-1, 0))*inlineAIChipGap
			estH = theme.HeightLarge + 1 + theme.Space8*2 + chipsH
		}
	}
	if ai.Error != "" {
		estH += theme.LineSmall + theme.Space8
	}
	sizeKey := fmt.Sprintf("inline-ai-%d", ai.Status)
	_, estH = s.measuredOrEstSize(sizeKey, panelW, estH)

	spec := appUi.PlaceBelow(trigger).WithViewport(vp).
		WithAlignX(appUi.AlignX{Kind: appUi.AlignPinLeft}).WithGap(10).WithMargin(12)
	x, y := appUi.ResolveOrigin(spec, panelW, estH, winW, winH)

	ui.Overlay(c, func() {
		shell := ui.Column(c.Key("inline-ai")).Absolute().Left(x).Top(y).Width(panelW).
			Gap(0).Radius(theme.RadiusXL).Background(pal.Menu).Border(1, pal.Hairline).
			Transition(appUi.PopupTransition())
		appUi.Elevate(shell, theme.ElevationPopover)
		appUi.Probe(shell, "inline-ai")
		s.recordOverlayPanel(sizeKey, shell)

		shell.Children(func() {
			if ai.Error != "" {
				ui.Text(c, ai.Error).FontSize(theme.FontSmall).TextColor(pal.Danger).Padding(theme.Space8, theme.Space12, 0, theme.Space12).FillWidth()
			}

			// Capsule input or Thinking row
			capsule := ui.Row(c.Key("inline-ai-capsule")).FillWidth().Height(theme.HeightLarge).Padding(0, theme.Space8, 0, theme.Space12).
				Gap(theme.Space8).AlignItems(ui.Center)

			capsule.Children(func() {
				if busy {
					ui.Box(c).Size(6, 6).Radius(theme.RadiusPill).Background(pal.Accent)
					lbl := theme.Tr("正在生成…", "Generating…")
					ui.Text(c, lbl).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).Grow(1).SingleLine()
					stopBtn := ui.ButtonBase(c.Key("ai-stop")).Height(theme.HeightMicro).PaddingX(theme.Space8).Center().Radius(theme.RadiusSM).
						Cursor(ui.CursorPointer).Label(theme.Tr("停止", "Stop"))
					if stopBtn.Hovered() {
						stopBtn.Background(pal.Hover)
					}
					stopBtn.Children(func() {
						ui.Text(c, theme.Tr("停止", "Stop")).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
					})
					appUi.Probe(stopBtn, "ai-stop")
					if stopBtn.Clicked() {
						s.aiStop()
					}
				} else {
					icons.Draw(c, icons.Sparkles, theme.IconInline, pal.DuoIconColors(pal.Accent))
					ph := inlineAIInputPlaceholder(emptyCtx)
					if ai.Status == aiStatusReview {
						ph = inlineAIReviewPlaceholder()
					}
					in := ui.TextInputBase(c.Key("inline-ai-input"), &ai.InputText).Grow(1).Height(24).Padding(0).
						FontSize(theme.FontSmall).TextColor(pal.Text).CaretColor(pal.Accent).CaretWidth(2).
						Placeholder(ph).PlaceholderColor(pal.Placeholder).
						Label("inline-ai-input").Bind(&ai.InputHandle)
					appUi.Probe(in, "inline-ai-input")
					in.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEscape && k.Mods == 0 {
							s.aiEscape()
							return true
						}
						if k.Key == ui.KeyTab && (k.Mods == 0 || k.Mods == ui.Shift) {
							return true
						}
						if k.Key == ui.KeyEnter && k.Mods == 0 {
							ai.InputText = k.Text()
							s.aiSubmitInput()
							return true
						}
						return false
					})
					if ai.FocusPending {
						in.Focus()
						ai.FocusPending = false
					}
					if ai.SelectEndPending {
						n := utf8.RuneCountInString(ai.InputText)
						in.SetTextSelection(n, n)
						ai.SelectEndPending = false
					}
					if in.Submitted() {
						s.aiSubmitInput()
					}

					canSend := ai.Status == aiStatusReview || strings.TrimSpace(ai.InputText) != ""
					sendBG := ui.Transparent
					sendFG := pal.Placeholder
					if canSend {
						sendBG = pal.Hover
						sendFG = pal.Accent
					}
					sendBtn := ui.ButtonBase(c.Key("ai-send")).Size(theme.HeightMicro, theme.HeightMicro).Center().Radius(theme.RadiusSM).
						Background(sendBG).Disabled(!canSend).Cursor(ui.CursorPointer).
						Label(theme.Tr("发送（回车）", "Send (Enter)"))
					appUi.WrappedTooltip(c, sendBtn, theme.Tr("使用在线 AI 时，指令、选区或草稿，以及光标前的少量上下文会发给已配置的服务。", "With online AI, the configured provider receives your instruction, selection or draft, and a small amount of preceding text."))
					sendBtn.Children(func() {
						icons.DrawAnimated(c, icons.ArrowUp, theme.IconMicro, pal.MonoIconColors(sendFG), canSend && (sendBtn.Hovered() || sendBtn.Pressed()))
					})
					appUi.Probe(sendBtn, "ai-send")
					if sendBtn.Clicked() && canSend {
						s.aiSubmitInput()
					}
				}
			})

			switch ai.Status {
			case aiStatusInput:
				scope := PromptScopeEdit
				if emptyCtx {
					scope = PromptScopeEmpty
				}
				entries := s.ensurePromptStore().list(scope)
				if len(entries) > 0 {
					footer := inlineAIFooter(ui.Column(c.Key("ai-chip-footer")), pal).Padding(theme.Space8)
					footer.Children(func() {
						grid := ui.Row(c.Key("ai-chip-grid")).FillWidth().MaxHeight(inlineAIChipAreaH).Clip().
							Wrap().AlignItems(ui.Center).Gap(inlineAIChipGap)
						grid.Children(func() {
							for ix, entry := range entries {
								it := entry
								short := it.ShortLabel()
								full := it.FullLabel()
								chip := appUi.ChipSurface(ui.Row(c.Key(fmt.Sprintf("ai-chip-%d", ix))), it.Starred)
								chip.Children(func() {
									runID := fmt.Sprintf("ai-saved-prompt-%d", ix)
									runBtn := ui.ButtonBase(c.Key(runID)).Height(theme.HeightMicro).PaddingX(theme.Space2).
										AlignItems(ui.Center).Radius(theme.RadiusXS).Cursor(ui.CursorPointer).Label(full)
									if full != "" {
										appUi.Tooltip(c, runBtn, full)
									}
									runBtn.Children(func() {
										fg := pal.Text
										if runBtn.Hovered() {
											fg = pal.Accent
										}
										ui.Text(c, short).FontSize(theme.FontSmall).TextColor(fg).SingleLine()
									})
									appUi.Probe(runBtn, runID)
									if runBtn.Clicked() {
										s.aiRunPromptTarget(it.promptTarget)
									}

									starID := fmt.Sprintf("ai-saved-star-%d", ix)
									starTip := theme.Tr("收藏该提示词（防被新输入顶掉）", "Star prompt to keep permanently")
									if it.Starred {
										starTip = theme.Tr("取消收藏该提示词", "Unstar prompt")
									}
									starBtn := ui.ButtonBase(c.Key(starID)).Size(20, 20).Center().Radius(theme.RadiusXS).
										Cursor(ui.CursorPointer).Label(starTip)
									appUi.Tooltip(c, starBtn, starTip)
									if starBtn.Hovered() {
										starBtn.Background(pal.Hover)
									}
									starBtn.Children(func() {
										ic := pal.DuoIconColors(pal.TextSecondary)
										if it.Starred {
											ic = pal.LitDuoIconColors(pal.Accent)
										}
										icons.DrawAnimated(c, icons.Star, 13, ic, it.Starred || starBtn.Hovered() || starBtn.Pressed())
									})
									appUi.Probe(starBtn, starID)
									if starBtn.Clicked() {
										s.aiTogglePromptStar(scope, it.promptTarget)
									}
								})
							}
						})
					})
				}

			case aiStatusReview:
				acceptLabel := theme.Tr("替换", "Replace")
				if emptyCtx {
					acceptLabel = theme.Tr("完成", "Done")
				}
				actions := inlineAIFooter(ui.Row(c.Key("ai-review-actions")), pal).MinHeight(theme.HeightLarge).Padding(theme.Space6).
					AlignItems(ui.Center).Gap(theme.Space2).Wrap()
				actions.Children(func() {
					accBtn := appUi.Chip(c, appUi.ChipOptions{ID: "ai-accept", Label: acceptLabel, Hint: "↵", Style: appUi.ChipPrimary})
					if accBtn.Clicked() {
						s.aiAccept()
					}
					if !emptyCtx {
						insBtn := appUi.Chip(c, appUi.ChipOptions{ID: "ai-insert-below", Label: theme.Tr("插入下方", "Insert Below")})
						if insBtn.Clicked() {
							s.aiInsertBelow()
						}
					}
					retryBtn := appUi.Chip(c, appUi.ChipOptions{ID: "ai-retry", Label: theme.Tr("重写", "Regenerate")})
					if retryBtn.Clicked() {
						s.aiRetry()
					}
					ui.Spacer(c)
					discBtn := appUi.Chip(c, appUi.ChipOptions{ID: "ai-discard", Label: theme.Tr("放弃", "Discard"), Style: appUi.ChipMuted})
					if discBtn.Clicked() {
						s.aiClose()
					}
				})
			}
		})

		if shell.PressedOutside() {
			if ai.Status == aiStatusInput && strings.TrimSpace(ai.InputText) == "" && !ai.HasPrevDraft {
				s.aiClose()
			} else if ai.Status == aiStatusReview {
				ai.FocusPending = true
			}
		}
	})
}

func unionRect(a, b ui.Rect) ui.Rect {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	if b.W <= 0 || b.H <= 0 {
		return a
	}
	x1 := min(a.X, b.X)
	y1 := min(a.Y, b.Y)
	x2 := max(a.X+a.W, b.X+b.W)
	y2 := max(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

func (s *Session) parseMarkdownDoc(src string) *model.Doc {
	if s.codec != nil {
		return s.codec.Parse(src)
	}
	return fallbackParsePaste(src)
}

func (s *Session) serializeBlocks(blocks []model.Block) string {
	doc := &model.Doc{Blocks: blocks, TrailingLine: "\n"}
	if s.codec != nil {
		return s.codec.Serialize(doc)
	}
	return fallbackSerialize(doc)
}

func (s *Session) isMarkdownNote() bool {
	if s.notePath == "" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(s.notePath))
	return ext == "" || ext == ".md" || ext == ".markdown" || ext == ".mdx"
}

func (s *Session) canReplaceSelection() bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	a, b := s.orderedSelection()
	if a.Block < 0 || b.Block >= len(s.doc.Blocks) {
		return false
	}
	if a.Block != b.Block && s.crossesHiddenFold(a.Block, b.Block) && !s.selectionCoversWholeDoc(a, b) {
		return false
	}
	return true
}

func (s *Session) crossesHiddenFold(first, last int) bool {
	for i := first; i <= last && i < len(s.doc.Blocks); i++ {
		if s.isBlockHiddenByFold(i) {
			return true
		}
	}
	return false
}

func (s *Session) selectionCoversWholeDoc(a, b Position) bool {
	if s.doc == nil || len(s.doc.Blocks) == 0 {
		return false
	}
	lastIdx := len(s.doc.Blocks) - 1
	return a.Block == 0 && a.Offset == 0 && b.Block == lastIdx && b.Offset == s.blockSelectionLen(lastIdx)
}

func tableCellAtRuneRange(tbl *model.TableData, startRune, endRune int) (row, col, relLo, relHi int, ok bool) {
	if tbl == nil || startRune > endRune {
		return 0, 0, 0, 0, false
	}
	pos := 0
	for rIdx, r := range tbl.Rows {
		for cIdx, cell := range r {
			clen := utf8.RuneCountInString(cell.PlainText())
			cStart, cEnd := pos, pos+clen
			if startRune >= cStart && endRune <= cEnd {
				return rIdx, cIdx, startRune - cStart, endRune - cStart, true
			}
			pos = cEnd
			if cIdx+1 < len(r) {
				pos++ // '\t'
			}
		}
		if rIdx+1 < len(tbl.Rows) {
			pos++ // '\n'
		}
	}
	return 0, 0, 0, 0, false
}

// inlineAIFooter is the muted strip under the inline AI input: a hairline on
// top and the bottom corners clipped to the panel's inner radius.
func inlineAIFooter(el ui.Element, pal theme.Palette) ui.Element {
	return el.FillWidth().Background(pal.CodeBlockBG).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).
		Radius(0, 0, theme.RadiusXLInner, theme.RadiusXLInner)
}
