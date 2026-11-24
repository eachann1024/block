package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

type fakeHost struct {
	mu          sync.Mutex
	root        string
	notebooks   []string
	activeNote  string
	files       map[string]string
	openedNotes []string
	openedTab   string
	previewed   []*model.BatchPlan
	applied     []*model.BatchPlan
	rejected    []string
	undone      []*model.BatchPlan
	redrawCount int
}

func newFakeHost(root string) *fakeHost {
	return &fakeHost{
		root:      root,
		notebooks: []string{root},
		files:     make(map[string]string),
	}
}

func (h *fakeHost) ActiveNotebook() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.root
}

func (h *fakeHost) Notebooks() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.notebooks...)
}

func (h *fakeHost) ActiveNotePath() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeNote
}

func (h *fakeHost) ListNotebookFiles(root string) ([]model.FileEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []model.FileEntry
	for p := range h.files {
		if strings.HasPrefix(p, root) {
			name := strings.TrimSuffix(filepath.Base(p), ".md")
			out = append(out, model.FileEntry{
				Path:       p,
				Name:       name,
				RawName:    filepath.Base(p),
				IsMarkdown: true,
			})
		}
	}
	return out, nil
}

func (h *fakeHost) SearchWorkspace(query string, allNotebooks bool) ([]model.SearchHit, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	low := strings.ToLower(query)
	var hits []model.SearchHit
	for p, body := range h.files {
		if strings.Contains(strings.ToLower(p), low) || strings.Contains(strings.ToLower(body), low) {
			hits = append(hits, model.SearchHit{
				Path:     p,
				Notebook: filepath.Base(h.root),
				Title:    strings.TrimSuffix(filepath.Base(p), ".md"),
				Snippet:  firstTitleLine(body),
			})
		}
	}
	return hits, nil
}

func (h *fakeHost) ReadNote(absPath string) (string, error) {
	h.mu.Lock()
	if v, ok := h.files[absPath]; ok {
		h.mu.Unlock()
		return v, nil
	}
	h.mu.Unlock()
	b, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *fakeHost) WriteNote(absPath string, content string) error {
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return err
	}
	h.mu.Lock()
	h.files[absPath] = content
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) OpenNote(absPath string, lineHint int) error {
	h.mu.Lock()
	h.openedNotes = append(h.openedNotes, absPath)
	h.activeNote = absPath
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) PreviewPlanInEditor(plan *model.BatchPlan) error {
	h.mu.Lock()
	h.previewed = append(h.previewed, plan)
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) ApplyBatchPlan(plan *model.BatchPlan) error {
	if err := ApplyPlanFiles(h, plan, false); err != nil {
		return err
	}
	h.mu.Lock()
	h.applied = append(h.applied, plan)
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) RejectBatchPlan(planID string) error {
	h.mu.Lock()
	h.rejected = append(h.rejected, planID)
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) UndoLastPlan(plan *model.BatchPlan) error {
	if err := ApplyPlanFiles(h, plan, true); err != nil {
		return err
	}
	h.mu.Lock()
	h.undone = append(h.undone, plan)
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) OpenSettings(tab string) {
	h.mu.Lock()
	h.openedTab = tab
	h.mu.Unlock()
}

func (h *fakeHost) RequestRedraw() {
	h.mu.Lock()
	h.redrawCount++
	h.mu.Unlock()
}

func setupReadyController(t *testing.T) (*Controller, *fakeHost, string) {
	t.Helper()
	theme.SetLanguage(model.LangZh)
	dataDir := t.TempDir()
	nbRoot := t.TempDir()
	host := newFakeHost(nbRoot)
	notePath := filepath.Join(nbRoot, "daily.md")
	if err := host.WriteNote(notePath, "# Daily\n\nInitial content.\n"); err != nil {
		t.Fatal(err)
	}
	host.activeNote = notePath

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled:  true,
		Provider: "openai",
		Protocol: model.ProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:1/v1",
		APIKey:   "test-key",
		Model:    "gpt-test",
	}); err != nil {
		t.Fatal(err)
	}
	return ctrl, host, nbRoot
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestStoreAndBackendsPersistence(t *testing.T) {
	ctrl, _, _ := setupReadyController(t)

	if ctrl.IsOpen() {
		t.Fatal("panel should start closed")
	}
	ctrl.ToggleOpen()
	if !ctrl.IsOpen() {
		t.Fatal("ToggleOpen should open panel")
	}
	ctrl.ToggleFullscreen()
	if !ctrl.IsFullscreen() {
		t.Fatal("ToggleFullscreen should enable fullscreen")
	}
	ctrl.SetPanelWidth(999)
	if ctrl.PanelWidth() != maxPanelWidth {
		t.Fatalf("PanelWidth = %v, want %v", ctrl.PanelWidth(), maxPanelWidth)
	}
	ctrl.SetPanelWidth(100)
	if ctrl.PanelWidth() != minPanelWidth {
		t.Fatalf("PanelWidth = %v, want %v", ctrl.PanelWidth(), minPanelWidth)
	}

	// Only Builtin is enabled initially -> cannot disable it.
	if ctrl.CanDisableBackend(model.BackendBuiltin) {
		t.Fatal("should not allow disabling the last enabled backend")
	}
	if err := ctrl.SetBackendEnabled(model.BackendBuiltin, false); err == nil {
		t.Fatal("expected error when disabling last enabled backend")
	}

	ctrl.SetBackendStatusOverride(model.BackendPi, &BackendStatus{State: BackendStateReady, Version: "1.0.0"})
	if err := ctrl.SetBackendEnabled(model.BackendPi, true); err != nil {
		t.Fatal(err)
	}
	if !ctrl.CanDisableBackend(model.BackendBuiltin) {
		t.Fatal("should allow disabling Builtin once Pi is also enabled")
	}
	if err := ctrl.SetBackendLocked(model.BackendPi, true); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.PromoteBackend(model.BackendPi); err != nil {
		t.Fatal(err)
	}
	bcfg := ctrl.BackendsConfig()
	if bcfg.Selected != model.BackendPi || len(bcfg.Priority) == 0 || bcfg.Priority[0] != model.BackendPi {
		t.Fatalf("PromoteBackend did not update selected/priority: %+v", bcfg)
	}
	if !bcfg.Adapters["pi"].Locked {
		t.Fatal("expected Pi backend locked flag persisted")
	}

	// Reload from disk. The status override does not survive, so put a pi
	// binary on PATH for machines (CI) that do not have one installed.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	reloaded := NewController(ctrl.dataDir, nil, nil)
	if reloaded.ProviderConfig().Model != "gpt-test" {
		t.Fatalf("expected persisted model gpt-test, got %q", reloaded.ProviderConfig().Model)
	}
	if reloaded.BackendsConfig().Selected != model.BackendPi {
		t.Fatalf("expected persisted selected Pi, got %v", reloaded.BackendsConfig().Selected)
	}
}

func TestBuiltinSSEToolLoopAndAnthropic(t *testing.T) {
	dataDir := t.TempDir()
	nbRoot := t.TempDir()
	host := newFakeHost(nbRoot)
	notePath := filepath.Join(nbRoot, "plan-target.md")
	if err := host.WriteNote(notePath, "# Title\n\nOld line\n"); err != nil {
		t.Fatal(err)
	}
	host.activeNote = notePath

	stepCount := 0
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		stepCount++
		if stepCount == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","function":{"name":"loadSkill","arguments":"{\"skill\":\"updateNote\"}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"tc2","function":{"name":"readPage","arguments":"{\"pageId\":\"plan-target\"}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		} else {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"已读取笔记并完成分析。"}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer openaiSrv.Close()

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled:  true,
		Provider: "openai",
		Protocol: model.ProtocolOpenAI,
		BaseURL:  openaiSrv.URL + "/v1",
		APIKey:   "k",
		Model:    "gpt-test",
	}); err != nil {
		t.Fatal(err)
	}

	ctrl.SendPrompt("分析一下当前笔记")
	waitUntil(t, 3*time.Second, func() bool {
		conv := ctrl.CurrentConversation()
		return conv != nil && !ctrl.IsStreaming() && len(conv.Messages) >= 5
	})

	conv := ctrl.CurrentConversation()
	foundTool := false
	foundReply := false
	for _, m := range conv.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "Old line") {
			foundTool = true
		}
		if m.Role == "assistant" && strings.Contains(m.Content, "已读取笔记并完成分析") {
			foundReply = true
		}
	}
	if !foundTool || !foundReply {
		t.Fatalf("expected tool result and final reply, got %+v", conv.Messages)
	}

	// Verify Anthropic SSE protocol as well.
	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `event: content_block_delta`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Claude SSE OK"}}`)
		fmt.Fprintln(w, `event: message_stop`)
		fmt.Fprintln(w, `data: {"type":"message_stop"}`)
	}))
	defer anthSrv.Close()

	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled:  true,
		Provider: "anthropic",
		Protocol: model.ProtocolAnthropic,
		BaseURL:  anthSrv.URL + "/v1",
		APIKey:   "sk-ant-test",
		Model:    "claude-sonnet-4-6",
	}); err != nil {
		t.Fatal(err)
	}
	ctrl.NewConversation()
	ctrl.SendPrompt("hello claude")
	waitUntil(t, 3*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	if !strings.Contains(ctrl.CurrentConversation().Messages[1].Content, "Claude SSE OK") {
		t.Fatalf("unexpected anthropic reply: %+v", ctrl.CurrentConversation().Messages)
	}
}

func TestMCPBridgeProtocol(t *testing.T) {
	ctrl, host, _ := setupReadyController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	turn := &assistantTurn{ctx: ctx, cancel: cancel, notebookScope: captureNotebook(host), convID: 1, serial: 1}
	ctrl.mu.Lock()
	st := ctrl.ensureRunStateLocked(1)
	st.turn = turn
	st.serial = 1
	st.streaming = true
	ctrl.mu.Unlock()
	bridge, err := startMCPBridge(ctrl, turn)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()

	// Unauthorized without bearer token.
	unauthReq, _ := http.NewRequest(http.MethodPost, bridge.addr, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauthResp.StatusCode)
	}

	// Authorized tools/call for listPages.
	payload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"listPages","arguments":{}}}`
	req, _ := http.NewRequest(http.MethodPost, bridge.addr, bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer "+bridge.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	res, _ := out["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected MCP tool result content, got %+v", out)
	}
}

func TestCreateNotePreviewRejectAcceptUndo(t *testing.T) {
	ctrl, _, nbRoot := setupReadyController(t)
	ctrl.EnableMockResponder(true)
	ctrl.SendPrompt("请 createNote 新建预览")

	waitUntil(t, 2*time.Second, func() bool {
		return ctrl.ActivePlan() != nil
	})
	preview := ctrl.ActivePlan()
	if !preview.IsCreate {
		t.Fatal("expected IsCreate=true")
	}
	if _, err := os.Stat(preview.Path); !os.IsNotExist(err) {
		t.Fatalf("createNote preview must NOT write file before Accept: %s", preview.Path)
	}

	// Toggle off the "排除" row so only "保留" is written.
	lines := ctrl.PlanDiffLines(preview.ConvID, preview.MsgIndex, preview.Page, preview.BeforeText, preview.AfterText)
	for _, dl := range lines {
		if dl.Change > 0 && strings.Contains(dl.Text, "排除") {
			ctrl.TogglePlanDiffLine(preview.ConvID, preview.MsgIndex, preview.Page, dl.SelectionIndex)
		}
	}
	selected := ctrl.SelectedPlanText(preview.ConvID, preview.MsgIndex, preview.Page, preview.BeforeText, preview.AfterText)
	if !strings.Contains(selected, "保留") || strings.Contains(selected, "排除") {
		t.Fatalf("selective diff reconstruction failed: %q", selected)
	}

	// Reject must leave disk clean.
	if err := ctrl.RejectPlan(preview.ConvID, preview.MsgIndex); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(preview.Path); !os.IsNotExist(err) {
		t.Fatalf("RejectPlan must not create file on disk: %s", preview.Path)
	}

	// Create again and Accept -> writes file -> Undo -> deletes file.
	ctrl.NewConversation()
	ctrl.SendPrompt("请 createNote 新建")
	waitUntil(t, 2*time.Second, func() bool {
		return ctrl.ActivePlan() != nil
	})
	preview2 := ctrl.ActivePlan()
	expectedPath := filepath.Join(nbRoot, preview2.Page+".md")
	if err := ctrl.AcceptPlan(preview2.ConvID, preview2.MsgIndex); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("AcceptPlan did not write created note: %v", err)
	}
	if !strings.Contains(string(written), "保留") {
		t.Fatalf("unexpected written content: %s", string(written))
	}
	if err := ctrl.UndoPlan(preview2.ConvID, preview2.MsgIndex); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("UndoPlan should remove newly created file: %s", expectedPath)
	}
}

func TestUIPanelCoverDraftsPickerAndCards(t *testing.T) {
	bui.ResetSmoke()
	bui.EnableSmokeMode()
	defer bui.DisableSmokeMode()

	// 1. Unconfigured cover test.
	dataDir := t.TempDir()
	host := newFakeHost(t.TempDir())
	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{Enabled: true, Provider: "deepseek"}); err != nil {
		t.Fatal(err)
	}

	ttCover := ui.NewTester(ctrl.RenderPanel, 420, 720)
	if _, ok := bui.TargetBounds("ai-cover-settings"); !ok {
		t.Fatal("expected ai-cover-settings probe on unconfigured cover")
	}
	if err := ttCover.Click("前往设置"); err != nil {
		t.Fatalf("click cover settings: %v", err)
	}
	if host.openedTab != "ai" {
		t.Fatalf("expected OpenSettings(\"ai\"), got %q", host.openedTab)
	}

	// 2. Configured panel with mock responder.
	ctrl, host, _ = setupReadyController(t)
	ctrl.EnableMockResponder(true)
	ctrl.SetOpen(true)

	tt := ui.NewTester(ctrl.RenderPanel, 440, 760)

	// Send first prompt to create conv1, then send in conv2 to test isolated drafts.
	ctrl.SendPrompt("第一会话消息")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	conv1 := ctrl.CurrentConversation().ID
	tt.Frame()

	ctrl.NewConversation()
	ctrl.SendPrompt("第二会话消息")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && c.ID != conv1 && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	conv2 := ctrl.CurrentConversation().ID
	tt.Frame()

	ctrl.SelectConversation(conv1)
	ctrl.SetDraftText("draft alpha")
	ctrl.SelectConversation(conv2)
	if ctrl.ActiveDraftText() != "" {
		t.Fatalf("expected isolated empty draft for conv2, got %q", ctrl.ActiveDraftText())
	}
	ctrl.SetDraftText("draft beta")
	ctrl.SelectConversation(conv1)
	if ctrl.ActiveDraftText() != "draft alpha" {
		t.Fatalf("expected conv1 draft restored, got %q", ctrl.ActiveDraftText())
	}
	ctrl.SelectConversation(conv2)
	if ctrl.ActiveDraftText() != "draft beta" {
		t.Fatalf("expected conv2 draft restored, got %q", ctrl.ActiveDraftText())
	}

	// Test header buttons: ai-fullscreen-toggle, ai-history-toggle, ai-new-chat.
	fsPt, ok := bui.TargetCenter("ai-fullscreen-toggle")
	if !ok {
		t.Fatal("missing ai-fullscreen-toggle")
	}
	tt.ClickAt(fsPt.X, fsPt.Y)
	if !ctrl.IsFullscreen() {
		t.Fatal("expected fullscreen toggled on")
	}
	fsPt, _ = bui.TargetCenter("ai-fullscreen-toggle")
	tt.ClickAt(fsPt.X, fsPt.Y)
	if ctrl.IsFullscreen() {
		t.Fatal("expected fullscreen toggled off")
	}

	histPt, ok := bui.TargetCenter("ai-history-toggle")
	if !ok {
		t.Fatal("missing ai-history-toggle")
	}
	tt.ClickAt(histPt.X, histPt.Y)
	if _, ok := bui.TargetBounds("ai-history-list"); !ok {
		t.Fatal("expected history popover opened")
	}
	tt.Key(0, ui.KeyEscape)

	newPt, ok := bui.TargetCenter("ai-new-chat")
	if !ok {
		t.Fatal("missing ai-new-chat")
	}
	tt.ClickAt(newPt.X, newPt.Y)
	if ctrl.CurrentConversation() != nil {
		t.Fatal("expected NewConversation via ai-new-chat button")
	}

	// Test composer Shift+Enter vs Enter, and 1200+ file @ picker + IME guard + / skill picker.
	files := make([]string, 0, 1205)
	for i := 0; i < 1200; i++ {
		files = append(files, fmt.Sprintf("picker-smoke/note-%04d.md", i))
	}
	files = append(files, "top-note.md")
	ctrl.InjectTestFiles(files)

	compPt, ok := bui.TargetCenter("ai-composer")
	if !ok {
		t.Fatal("missing ai-composer")
	}
	tt.ClickAt(compPt.X, compPt.Y)
	tt.Type("第一行")
	tt.Key(ui.Shift, ui.KeyEnter)
	if ctrl.ActiveDraftText() != "第一行\n" {
		t.Fatalf("Shift+Enter should insert newline, got %q", ctrl.ActiveDraftText())
	}
	tt.Key(0, ui.KeyEnter)
	waitUntil(t, 2*time.Second, func() bool {
		return ctrl.CurrentConversation() != nil && !ctrl.IsStreaming() && len(ctrl.CurrentConversation().Messages) >= 2
	})
	tt.Frame()

	// Process card, tool chips, source click, and hover copy button.
	procToggle, ok := bui.TargetCenter("ai-process-toggle")
	if !ok {
		t.Fatal("missing ai-process-toggle")
	}
	tt.ClickAt(procToggle.X, procToggle.Y)
	if _, ok := bui.TargetBounds("ai-process-steps"); !ok {
		t.Fatal("expected process steps expanded")
	}
	chip0, ok := bui.TargetCenter("ai-process-tool-chip-0")
	if !ok {
		t.Fatal("missing ai-process-tool-chip-0")
	}
	tt.ClickAt(chip0.X, chip0.Y)
	if _, ok := bui.TargetBounds("ai-process-tool-detail-0"); !ok {
		t.Fatal("expected ai-process-tool-detail-0 expanded")
	}

	scroll, ok := bui.TargetBounds("ai-chat-scroll")
	if !ok {
		t.Fatal("missing chat scroll area")
	}
	// Expanded process and source cards can push the reply below the viewport.
	tt.Scroll(scroll.X+scroll.W/2, scroll.Y+scroll.H/2, 0, scroll.H)
	if err := tt.Click("复制回复"); err != nil {
		t.Fatalf("click copy reply: %v", err)
	}
	if !strings.Contains(tt.Clipboard(), "hello block") {
		t.Fatalf("expected copied markdown code in clipboard, got %q", tt.Clipboard())
	}

	// Test @ virtualized picker with 1200+ files, folder navigation, and IME guard.
	compPt, _ = bui.TargetCenter("ai-composer")
	tt.ClickAt(compPt.X, compPt.Y)
	tt.Type("@")
	if _, ok := bui.TargetBounds("ai-ref-picker"); !ok {
		t.Fatal("expected ai-ref-picker opened on @")
	}
	// Click folder "picker-smoke" to navigate into it.
	if err := tt.Click("picker-smoke"); err != nil {
		t.Fatalf("click picker-smoke folder: %v", err)
	}
	if !tt.HasText("返回上级") {
		t.Fatal("expected parent folder item after entering picker-smoke")
	}
	// Navigate back up.
	if err := tt.Click("返回上级"); err != nil {
		t.Fatalf("click parent folder: %v", err)
	}

	// IME composition: Enter during composition must NOT select or send!
	tt.Compose("picker-smoke/note-1199", 22)
	tt.Key(0, ui.KeyEnter)
	if ctrl.ActiveDraftText() != "@" {
		t.Fatalf("IME composition Enter should not mutate draft, got %q", ctrl.ActiveDraftText())
	}
	tt.Compose("", 0)
	tt.Type("picker-smoke/note-1199")
	tt.Key(0, ui.KeyTab)
	root, _ := canonicalNotebook(ctrl.host.ActiveNotebook())
	if !containsString(ctrl.draftRefs, filepath.Join(root, "picker-smoke", "note-1199.md")) {
		t.Fatalf("expected picker-smoke/note-1199 attached via Tab, refs=%v", ctrl.draftRefs)
	}

	// Test / skill picker.
	tt.Type("/updateNote")
	if _, ok := bui.TargetBounds("ai-skill-picker"); !ok {
		t.Fatal("expected ai-skill-picker opened on /")
	}
	tt.Key(0, ui.KeyEnter)
	if !containsString(ctrl.draftSkills, "updateNote") {
		t.Fatalf("expected updateNote skill attached, skills=%v", ctrl.draftSkills)
	}

	// Test interactive question card.
	ctrl.NewConversation()
	ctrl.SendPrompt("请确认选择题")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && len(c.Messages) >= 2 && c.Messages[1].Question != nil
	})
	tt.Frame()
	if _, ok := bui.TargetBounds("ai-question-card"); !ok {
		t.Fatal("expected ai-question-card rendered")
	}
	optPt, ok := bui.TargetCenter("ai-question-opt-0-0")
	if !ok {
		t.Fatal("missing ai-question-opt-0-0")
	}
	tt.ClickAt(optPt.X, optPt.Y)
	contPt, _ := bui.TargetCenter("ai-question-continue")
	tt.ClickAt(contPt.X, contPt.Y)
	skipPt, _ := bui.TargetCenter("ai-question-skip")
	tt.ClickAt(skipPt.X, skipPt.Y)
	if !ctrl.CurrentConversation().Messages[1].Question.Submitted {
		t.Fatal("expected question card submitted")
	}

	// Test approval card with fail-once retry.
	ctrl.NewConversation()
	ctrl.SetMockApprovalFailOnce(true)
	ctrl.SendPrompt("请审批权限")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && len(c.Messages) >= 2 && c.Messages[1].Approval != nil
	})
	tt.Frame()
	if _, ok := bui.TargetBounds("ai-approval-continue-disabled"); !ok {
		t.Fatal("expected continue disabled before option selection")
	}
	allowPt, _ := bui.TargetCenter("ai-approval-option-allow")
	tt.ClickAt(allowPt.X, allowPt.Y)
	contPt, _ = bui.TargetCenter("ai-approval-continue")
	tt.ClickAt(contPt.X, contPt.Y)
	if !ctrl.CurrentConversation().Messages[1].Approval.Failed {
		t.Fatal("expected simulated approval failure on first click")
	}
	contPt, _ = bui.TargetCenter("ai-approval-continue")
	tt.ClickAt(contPt.X, contPt.Y)
	if ctrl.CurrentConversation().Messages[1].Approval.Decision != model.ApprovalAllow {
		t.Fatalf("expected ApprovalAllow after retry, got %v", ctrl.CurrentConversation().Messages[1].Approval.Decision)
	}

	// Test Agent Deck single-row backend cards.
	ctrl.SetBackendStatusOverride(model.BackendPi, &BackendStatus{State: BackendStateReady, Version: "1.0"})
	_ = ctrl.SetBackendEnabled(model.BackendPi, true)
	tt.Frame()
	deckTrig, ok := bui.TargetCenter("ai-deck-trigger")
	if !ok {
		t.Fatal("missing ai-deck-trigger")
	}
	tt.ClickAt(deckTrig.X, deckTrig.Y)
	cardRect, ok := bui.TargetBounds("ai-backend-Builtin")
	if !ok {
		t.Fatal("missing ai-backend-Builtin in deck")
	}
	if cardRect.H > 42 {
		t.Fatalf("expected single-row backend card <= 42px, got %v", cardRect.H)
	}
	piPt, ok := bui.TargetCenter("ai-backend-Pi")
	if !ok {
		t.Fatal("missing ai-backend-Pi in deck")
	}
	tt.ClickAt(piPt.X, piPt.Y)
	if ctrl.BackendsConfig().Selected != model.BackendPi {
		t.Fatalf("expected Pi selected after click, got %v", ctrl.BackendsConfig().Selected)
	}
}

func TestAISettingsSchemeAAndAddProvider(t *testing.T) {
	bui.ResetSmoke()
	bui.EnableSmokeMode()
	defer bui.DisableSmokeMode()

	ctrl, _, _ := setupReadyController(t)
	ctrl.SetBackendStatusOverride(model.BackendCodex, &BackendStatus{State: BackendStateReady, Version: "0.42.0"})
	ctrl.SetBackendStatusOverride(model.BackendClaude, &BackendStatus{State: BackendStateUnauth, Version: "2.1.320"})

	// New and previously saved agents without a preference use full local tools.
	for _, k := range model.AllBackends {
		if supportsCLI(k) && !ctrl.BackendsConfig().Adapters[string(k)].AllowsNativeTools() {
			t.Fatalf("native tools disabled by default for %s", k)
		}
	}

	// Render in a narrow window width (560px): local CLI agents are NOT auto-populated before adding.
	ttSet := ui.NewTester(ctrl.RenderSettingsTab, 560, 760)
	if _, ok := bui.TargetBounds("ai-settings-section"); !ok {
		t.Fatal("missing ai-settings-section")
	}
	if !ttSet.HasText("API 供应商") || !ttSet.HasText("添加供应商") || !ttSet.HasText("本机 Agent") || !ttSet.HasText("添加 Agent") || !ttSet.HasText("尚未添加本机 Agent") {
		t.Fatal("missing expected AI Settings labels or empty local agent state")
	}
	if ttSet.HasText("允许本机工具") {
		t.Fatal("native tool setting visible before adding an agent")
	}
	if _, ok := bui.TargetBounds("ai-backend-card-Codex"); ok {
		t.Fatal("local CLI agent must not appear in list before being explicitly added")
	}

	// 1. Open Add Provider modal -> verify ONLY 3 provider presets exist (deepseek, openai, anthropic).
	addBtnRect, ok := bui.TargetBounds("ai-add-provider-btn")
	if !ok {
		t.Fatal("missing ai-add-provider-btn")
	}
	ttSet.ClickAt(addBtnRect.X+addBtnRect.W*0.5, addBtnRect.Y+addBtnRect.H*0.5)
	if _, ok := bui.TargetBounds("ai-add-provider-modal"); !ok {
		t.Fatal("expected ai-add-provider-modal to open")
	}
	for _, pid := range []string{"deepseek", "openai", "anthropic"} {
		if _, ok := bui.TargetBounds("ai-preset-" + pid); !ok {
			t.Fatalf("missing expected preset chip ai-preset-%s", pid)
		}
	}
	for _, extra := range []string{"openrouter", "siliconflow", "ollama"} {
		if _, ok := bui.TargetBounds("ai-preset-" + extra); ok {
			t.Fatalf("unexpected extra preset chip ai-preset-%s (only 3 allowed)", extra)
		}
	}

	claudeChip, _ := bui.TargetBounds("ai-preset-anthropic")
	ttSet.ClickAt(claudeChip.X+claudeChip.W*0.5, claudeChip.Y+claudeChip.H*0.5)
	subBtn, ok := bui.TargetBounds("ai-add-provider-submit")
	if !ok {
		t.Fatal("missing ai-add-provider-submit")
	}
	ttSet.ClickAt(subBtn.X+subBtn.W*0.5, subBtn.Y+subBtn.H*0.5)
	if _, ok := bui.TargetBounds("ai-provider-row-anthropic"); !ok {
		t.Fatal("expected newly added anthropic provider row in list")
	}
	if ctrl.ProviderConfig().Provider != "anthropic" || ctrl.ProviderConfig().Protocol != model.ProtocolAnthropic {
		t.Fatalf("expected anthropic provider active after adding, got %+v", ctrl.ProviderConfig())
	}

	// 2. Open T3 Code 2-step Add Local Agent modal -> Step 1 select Codex -> Step 2 configure & submit -> verify row.
	addAgRect, ok := bui.TargetBounds("ai-add-agent-btn")
	if !ok {
		t.Fatal("missing ai-add-agent-btn")
	}
	ttSet.ClickAt(addAgRect.X+addAgRect.W*0.5, addAgRect.Y+addAgRect.H*0.5)
	if _, ok := bui.TargetBounds("ai-add-agent-modal"); !ok {
		t.Fatal("expected ai-add-agent-modal to open")
	}
	// Step 1 shows driver catalog; submit button should not exist before picking a driver.
	if _, ok := bui.TargetBounds("ai-add-agent-submit"); ok {
		t.Fatal("expected Step 1 driver catalog before Step 2 submit form")
	}
	codexDrv, ok := bui.TargetBounds("ai-agent-driver-Codex")
	if !ok {
		t.Fatal("missing ai-agent-driver-Codex in Step 1 catalog")
	}
	ttSet.ClickAt(codexDrv.X+codexDrv.W*0.5, codexDrv.Y+codexDrv.H*0.5)
	// Now in Step 2: verify Display Name & Binary Path fields + Add & Enable button.
	if _, ok := bui.TargetBounds("ai-add-agent-binpath"); !ok {
		t.Fatal("expected ai-add-agent-binpath in Step 2 setup form")
	}
	agSubmit, ok := bui.TargetBounds("ai-add-agent-submit")
	if !ok {
		t.Fatal("missing ai-add-agent-submit in Step 2")
	}
	ttSet.ClickAt(agSubmit.X+agSubmit.W*0.5, agSubmit.Y+agSubmit.H*0.5)

	if _, ok := bui.TargetBounds("ai-backend-card-Codex"); !ok {
		t.Fatal("expected Codex row in Local Agents list after adding")
	}
	if !ttSet.HasText("可执行路径") || !ttSet.HasText("思考强度") {
		t.Fatal("expected expanded Codex row to show Binary Path and Thinking Level")
	}
	toolsCenter, ok := bui.TargetCenter("ai-agent-native-tools-Codex")
	if !ok || !ttSet.HasText("允许本机工具") {
		t.Fatal("missing native tool setting in expanded Codex row")
	}
	ttSet.ClickAt(toolsCenter.X, toolsCenter.Y)
	persisted := ctrl.readBackendsFile()
	if persisted.Adapters[string(model.BackendCodex)].AllowsNativeTools() || !persisted.Adapters[string(model.BackendClaude)].AllowsNativeTools() {
		t.Fatal("native tool opt-out was not saved for Codex only")
	}
	ttSet.ClickAt(toolsCenter.X, toolsCenter.Y)
	if !ctrl.readBackendsFile().Adapters[string(model.BackendCodex)].AllowsNativeTools() {
		t.Fatal("native tool re-enable was not saved")
	}

	// 3. Verify Annotation Model popover still works.
	annRect, ok := bui.TargetBounds("annotation-model-trigger")
	if !ok {
		t.Fatal("missing annotation-model-trigger")
	}
	ttSet.ClickAt(annRect.X+annRect.W*0.5, annRect.Y+annRect.H*0.5)
	if _, ok := bui.TargetBounds("annotation-model-menu"); !ok {
		t.Fatal("missing annotation-model-menu popover after clicking trigger")
	}
}

func TestSetRenderWidthPreservesPanelWidth(t *testing.T) {
	bui.ResetSmoke()
	bui.EnableSmokeMode()
	defer bui.DisableSmokeMode()

	ctrl, _, _ := setupReadyController(t)
	ctrl.SetOpen(true)
	ctrl.SetPanelWidth(500)
	if ctrl.PanelWidth() != 500 {
		t.Fatalf("PanelWidth = %v, want 500", ctrl.PanelWidth())
	}

	ctrl.SetRenderWidth(320)
	if ctrl.PanelWidth() != 500 {
		t.Fatalf("SetRenderWidth overwrote PanelWidth: got %v, want 500", ctrl.PanelWidth())
	}

	tt := ui.NewTester(ctrl.RenderPanel, 640, 720)
	b, ok := bui.TargetBounds("ai-chat-panel")
	if !ok {
		t.Fatal("missing ai-chat-panel bounds")
	}
	if b.W < 319 || b.W > 321 {
		t.Fatalf("expected rendered width 320 when renderWidth=320, got %v", b.W)
	}

	ctrl.SetRenderWidth(0)
	tt.Frame()
	b, _ = bui.TargetBounds("ai-chat-panel")
	if b.W < 499 || b.W > 501 {
		t.Fatalf("expected fallback to PanelWidth 500, got %v", b.W)
	}

	ctrl.SetRenderWidth(320)
	ctrl.SetFullscreen(true)
	tt.Frame()
	b, _ = bui.TargetBounds("ai-chat-panel")
	if b.W < 639 {
		t.Fatalf("expected fullscreen width 640, got %v", b.W)
	}
}

func TestRealOpenAISSEToolLoopAndBatchPlanE2E(t *testing.T) {
	dataDir := t.TempDir()
	nbRoot := t.TempDir()
	host := newFakeHost(nbRoot)
	notePath := filepath.Join(nbRoot, "target.md")
	const origContent = "# Target\n\nOld body\n\n| Item | Keep |\n| --- | --- |\n| Base | yes |\n"
	if err := host.WriteNote(notePath, origContent); err != nil {
		t.Fatal(err)
	}
	host.activeNote = notePath

	var mu sync.Mutex
	var sawToolResults int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1-live"},{"id":"deepseek-chat"}]}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			var req struct {
				Stream   bool `json:"stream"`
				Messages []struct {
					Role    string `json:"role"`
					Content any    `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if !req.Stream {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"ping","choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)

			if len(req.Messages) > 0 {
				if sys, _ := req.Messages[0].Content.(string); strings.Contains(sys, "Inline rewrite system") {
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"第一段改写，"}}]}`)
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"第二段完成。"}}]}`)
					fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
					fmt.Fprintln(w, `data: [DONE]`)
					if flusher != nil {
						flusher.Flush()
					}
					return
				}
			}

			toolCount := 0
			for _, m := range req.Messages {
				if m.Role == "tool" {
					toolCount++
				}
			}
			if toolCount == 0 {
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"先列出笔记本文件、搜索并读取 target.md，再生成变更计划。"}}]}`)
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc_list","function":{"name":"listNotebookFiles","arguments":"{}"}}]}}]}`)
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"tc_search","function":{"name":"searchNotes","arguments":"{\"query\":\"Target\"}"}}]}}]}`)
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"tc_read","function":{"name":"readNote","arguments":"{\"pageId\":\"target\"}"}}]}}]}`)
				planArgs, _ := json.Marshal(map[string]any{
					"title":   "更新 Target",
					"summary": "更新 target.md 并补充条目",
					"operations": []map[string]any{{
						"type":      "search_replace",
						"pageId":    "target",
						"oldString": "Old body\n\n| Item | Keep |\n| --- | --- |\n| Base | yes |",
						"newString": "Updated body\n\n| Item | Keep |\n| --- | --- |\n| Base | yes |\n| Keep item | yes |\n| Skip item | no |",
					}},
				})
				tcChunk, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index": 3,
								"id":    "tc_plan",
								"function": map[string]any{
									"name":      "executeBatchPlan",
									"arguments": string(planArgs),
								},
							}},
						},
					}},
				})
				fmt.Fprintf(w, "data: %s\n", tcChunk)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
			} else {
				mu.Lock()
				sawToolResults = toolCount
				mu.Unlock()
				replyChunk, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{
							"content": "已完成 [target](target.md) 的变更方案：\n\n```go\nfunc updated() bool { return true }\n```",
						},
					}},
				})
				fmt.Fprintf(w, "data: %s\n", replyChunk)
				fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}]}`)
				fmt.Fprintln(w, `data: [DONE]`)
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled:  true,
		Provider: "openai",
		Protocol: model.ProtocolOpenAI,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "live-key",
		Model:    "gpt-4.1-live",
	}); err != nil {
		t.Fatal(err)
	}
	if !ctrl.IsLiveProviderReady() {
		t.Fatal("expected IsLiveProviderReady() == true")
	}

	models, err := ctrl.RefreshBuiltinModels(context.Background())
	if err != nil {
		t.Fatalf("RefreshBuiltinModels: %v", err)
	}
	if len(models) != 2 || models[0].Value != "gpt-4.1-live" || models[1].Value != "deepseek-chat" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if err := ctrl.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	ctrl.SendPrompt("请整理 target.md")
	waitUntil(t, 4*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && ctrl.ActivePlan() != nil && len(c.Messages) >= 7
	})

	mu.Lock()
	gotToolResults := sawToolResults
	mu.Unlock()
	if gotToolResults != 4 {
		t.Fatalf("expected 4 tool results sent back on turn 2, got %d", gotToolResults)
	}

	conv := ctrl.CurrentConversation()
	var stepTools []string
	hasThink := false
	hasCodeReply := false
	for _, m := range conv.Messages {
		for _, st := range m.Process {
			if st.Kind == model.ToolKindThink {
				hasThink = true
			}
			if st.ToolName != "" {
				stepTools = append(stepTools, st.ToolName)
			}
		}
		if m.Role == "assistant" && strings.Contains(m.Content, "[target](target.md)") && strings.Contains(m.Content, "```go") {
			hasCodeReply = true
		}
	}
	if !hasThink || len(stepTools) < 4 || !hasCodeReply {
		t.Fatalf("missing process trace or summary reply: think=%v tools=%v codeReply=%v", hasThink, stepTools, hasCodeReply)
	}

	preview := ctrl.ActivePlan()
	if preview == nil || preview.IsCreate || preview.Page != "target" {
		t.Fatalf("unexpected ActivePlan: %+v", preview)
	}
	lines := ctrl.PlanDiffLines(preview.ConvID, preview.MsgIndex, preview.Page, preview.BeforeText, preview.AfterText)
	for _, dl := range lines {
		if dl.Change > 0 && strings.Contains(dl.Text, "Skip item") {
			ctrl.TogglePlanDiffLine(preview.ConvID, preview.MsgIndex, preview.Page, dl.SelectionIndex)
		}
	}
	if err := ctrl.AcceptPlan(preview.ConvID, preview.MsgIndex); err != nil {
		t.Fatalf("AcceptPlan: %v", err)
	}
	updatedDisk, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedDisk), "Updated body") || !strings.Contains(string(updatedDisk), "Keep item") || strings.Contains(string(updatedDisk), "Skip item") {
		t.Fatalf("unexpected updated note content: %q", string(updatedDisk))
	}
	if err := ctrl.UndoPlan(preview.ConvID, preview.MsgIndex); err != nil {
		t.Fatalf("UndoPlan: %v", err)
	}
	revertedDisk, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(revertedDisk) != origContent {
		t.Fatalf("expected reverted content %q, got %q", origContent, string(revertedDisk))
	}

	inlineOut, err := ctrl.CompletePrompt(context.Background(), "Inline rewrite system", "Rewrite this note")
	if err != nil {
		t.Fatalf("CompletePrompt: %v", err)
	}
	if inlineOut != "第一段改写，第二段完成。" {
		t.Fatalf("unexpected CompletePrompt output: %q", inlineOut)
	}
}

func TestRealAnthropicSSEStreamAndQuestionToolE2E(t *testing.T) {
	bui.ResetSmoke()
	bui.EnableSmokeMode()
	defer bui.DisableSmokeMode()

	dataDir := t.TempDir()
	host := newFakeHost(t.TempDir())

	var sawAnswer string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "sk-ant-live" || r.Header.Get("anthropic-version") != "2023-06-01" {
			http.Error(w, "missing anthropic headers", http.StatusUnauthorized)
			return
		}
		var req struct {
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		if strings.Contains(req.System, "Anthropic inline") {
			fmt.Fprintln(w, `event: message_start`)
			fmt.Fprintln(w, `data: {"type":"message_start","message":{"id":"msg_inline","role":"assistant"}}`)
			fmt.Fprintln(w, `event: content_block_start`)
			fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Anthropic Complete OK"}}`)
			fmt.Fprintln(w, `event: message_stop`)
			fmt.Fprintln(w, `data: {"type":"message_stop"}`)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		rawMsgs, _ := json.Marshal(req.Messages)
		if !strings.Contains(string(rawMsgs), `"tool_result"`) {
			fmt.Fprintln(w, `event: message_start`)
			fmt.Fprintln(w, `data: {"type":"message_start","message":{"id":"msg_1","role":"assistant"}}`)
			fmt.Fprintln(w, `event: content_block_start`)
			fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先通过交互卡片确认导出格式。"}}`)
			fmt.Fprintln(w, `event: content_block_stop`)
			fmt.Fprintln(w, `data: {"type":"content_block_stop","index":0}`)
			fmt.Fprintln(w, `event: content_block_start`)
			fmt.Fprintln(w, `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"请先确认格式："}}`)
			fmt.Fprintln(w, `event: content_block_stop`)
			fmt.Fprintln(w, `data: {"type":"content_block_stop","index":1}`)
			fmt.Fprintln(w, `event: content_block_start`)
			fmt.Fprintln(w, `data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_q1","name":"askQuestion","input":{}}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"questions\":[{\"q\":\"选择导出格式\","}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"type\":\"radio\",\"options\":[\"Markdown\",\"PDF\"]}]}"}}`)
			fmt.Fprintln(w, `event: content_block_stop`)
			fmt.Fprintln(w, `data: {"type":"content_block_stop","index":2}`)
			fmt.Fprintln(w, `event: message_stop`)
			fmt.Fprintln(w, `data: {"type":"message_stop"}`)
		} else {
			mu.Lock()
			sawAnswer = string(rawMsgs)
			mu.Unlock()
			fmt.Fprintln(w, `event: message_start`)
			fmt.Fprintln(w, `data: {"type":"message_start","message":{"id":"msg_2","role":"assistant"}}`)
			fmt.Fprintln(w, `event: content_block_start`)
			fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			fmt.Fprintln(w, `event: content_block_delta`)
			fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"已按 Markdown 偏好完成处理。"}}`)
			fmt.Fprintln(w, `event: message_stop`)
			fmt.Fprintln(w, `data: {"type":"message_stop"}`)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled:  true,
		Provider: "anthropic",
		Protocol: model.ProtocolAnthropic,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-ant-live",
		Model:    "claude-sonnet-4-6",
	}); err != nil {
		t.Fatal(err)
	}
	ctrl.SetOpen(true)
	ctrl.SendPrompt("帮我导出笔记")
	waitUntil(t, 4*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		if c == nil || ctrl.IsStreaming() {
			return false
		}
		for _, m := range c.Messages {
			if m.Question != nil {
				return true
			}
		}
		return false
	})

	conv := ctrl.CurrentConversation()
	hasThink := false
	for _, m := range conv.Messages {
		for _, st := range m.Process {
			if st.Kind == model.ToolKindThink && strings.Contains(st.Detail, "确认导出格式") {
				hasThink = true
			}
		}
	}
	if !hasThink {
		t.Fatalf("expected thinking trace in Anthropic turn, got %+v", conv.Messages)
	}

	tt := ui.NewTester(ctrl.RenderPanel, 440, 760)
	if _, ok := bui.TargetBounds("ai-question-card"); !ok {
		t.Fatal("expected ai-question-card rendered")
	}
	optPt, ok := bui.TargetCenter("ai-question-opt-0-0")
	if !ok {
		t.Fatal("missing ai-question-opt-0-0")
	}
	tt.ClickAt(optPt.X, optPt.Y)
	contPt, ok := bui.TargetCenter("ai-question-continue")
	if !ok {
		t.Fatal("missing ai-question-continue")
	}
	tt.ClickAt(contPt.X, contPt.Y)

	waitUntil(t, 4*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		if c == nil || ctrl.IsStreaming() {
			return false
		}
		for _, m := range c.Messages {
			if m.Role == "assistant" && strings.Contains(m.Content, "已按 Markdown 偏好完成处理") {
				return true
			}
		}
		return false
	})

	mu.Lock()
	ansPayload := sawAnswer
	mu.Unlock()
	if !strings.Contains(ansPayload, "Markdown") {
		t.Fatalf("expected Markdown answer forwarded to Anthropic turn 2, got %s", ansPayload)
	}

	completeOut, err := ctrl.CompletePrompt(context.Background(), "Anthropic inline", "test")
	if err != nil {
		t.Fatalf("Anthropic CompletePrompt: %v", err)
	}
	if completeOut != "Anthropic Complete OK" {
		t.Fatalf("unexpected Anthropic CompletePrompt output: %q", completeOut)
	}
}

func TestRealCLISubprocessCodexAndClaudeJSONLStream(t *testing.T) {
	binDir := t.TempDir()
	claudeScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "2.1.320 (Claude Code)"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo '{"loggedIn":true}'
  exit 0
fi
case "$*" in
  *"--input-format stream-json"*)
    while IFS= read -r line; do
      case "$line" in
        *'"request_id":"block_models"'*)
          printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"block_models","response":{"models":[{"value":"default","displayName":"Default (recommended)"},{"value":"opus","displayName":"Opus 5.5"},{"value":"sonnet","displayName":"Sonnet 5.5"}]}}}'
          exit 0
          ;;
      esac
    done
    exit 0
    ;;
esac
case "$*" in
  *"--dangerously-skip-permissions"*) ;;
  *) exit 1 ;;
esac
printf '%s\n' '{"type":"system","subtype":"init","session_id":"claude-sess-e2e"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Inspecting workspace note"},{"type":"tool_use","id":"tu_1","name":"readPage","input":{"pageId":"daily"}}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Claude CLI E2E reply OK"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","session_id":"claude-sess-e2e"}'
`
	codexScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.2.0"
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  echo "Logged in"
  exit 0
fi
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
      ;;
    *'"method":"model/list"'*)
      printf '%s\n' '{"id":2,"result":{"data":[{"id":"gpt-6.2-sol","model":"gpt-6.2-sol","displayName":"GPT-6.2-Sol","hidden":false},{"id":"hidden-internal","model":"hidden-internal","displayName":"Hidden","hidden":true},{"id":"gpt-6.1-terra","model":"gpt-6.1-terra","displayName":"GPT-6.1 Terra","hidden":false}],"nextCursor":null}}'
      ;;
    *'"method":"thread/start"'*)
      case "$line" in *'"approvalPolicy":"never"'*'"sandbox":"danger-full-access"'*) ;; *) exit 1 ;; esac
      printf '%s\n' '{"id":2,"result":{"thread":{"id":"codex-thread-e2e"}}}'
      ;;
    *'"method":"turn/start"'*)
      printf '%s\n' '{"id":3,"result":{"turn":{"id":"t1"}}}'
      printf '%s\n' '{"method":"item/started","params":{"item":{"id":"it_1","type":"mcpToolCall","tool":"readPage","arguments":{"pageId":"daily"}}}}'
      printf '%s\n' '{"method":"item/completed","params":{"item":{"id":"it_1","type":"mcpToolCall","tool":"readPage","arguments":{"pageId":"daily"},"status":"completed"}}}'
      printf '%s\n' '{"method":"item/agentMessage/delta","params":{"delta":"Codex CLI "}}'
      printf '%s\n' '{"method":"item/agentMessage/delta","params":{"delta":"E2E reply OK"}}'
      printf '%s\n' '{"method":"turn/completed","params":{"turn":{"id":"t1","status":"completed"}}}'
      exit 0
      ;;
  esac
done
`
	agyScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "agy 2.0.0"
  exit 0
fi
if [ "$1" = "models" ]; then
  printf 'gemini-4-carbon-medium\tGemini 4 Carbon (Medium)\n'
  printf 'gemini-4-hydrogen-medium\tGemini 4 Hydrogen (Medium)\n'
  exit 0
fi
exit 1
`
	piScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "1.0.0"
  exit 0
fi
if [ "$1" = "--list-models" ]; then
  if [ "$PI_EMPTY" = "1" ]; then
    printf 'No models available. Use /login to log into a provider via OAuth or API key. See:\n  /tmp/docs/models.md\n'
    exit 0
  fi
  printf 'provider  model  context\nanthropic claude-sonnet-5 200k\n'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(claudeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "agy"), []byte(agyScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte(piScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Verify real-time CLI model discovery for Claude, Codex, Agy, and Pi.
	claudeModels, err := listModelsForBackend(context.Background(), model.BackendClaude, model.AIProviderConfig{})
	if err != nil {
		t.Fatalf("listModelsForBackend(Claude): %v", err)
	}
	if len(claudeModels) != 3 || claudeModels[0].Value != "default" || claudeModels[1].Label != "Opus 5.5" {
		t.Fatalf("unexpected Claude models: %+v", claudeModels)
	}
	codexModels, err := listModelsForBackend(context.Background(), model.BackendCodex, model.AIProviderConfig{})
	if err != nil {
		t.Fatalf("listModelsForBackend(Codex): %v", err)
	}
	if len(codexModels) != 2 || codexModels[0].Value != "gpt-6.2-sol" || codexModels[0].Label != "GPT-6.2-Sol" || codexModels[1].Label != "GPT-6.1 Terra" {
		t.Fatalf("unexpected Codex models: %+v", codexModels)
	}
	agyModels, err := listModelsForBackend(context.Background(), model.BackendAgy, model.AIProviderConfig{})
	if err != nil {
		t.Fatalf("listModelsForBackend(Agy): %v", err)
	}
	if len(agyModels) != 2 || agyModels[1].Value != "gemini-4-hydrogen-medium" || agyModels[1].Label != "Gemini 4 Hydrogen (Medium)" {
		t.Fatalf("unexpected Agy models: %+v", agyModels)
	}
	piModels, err := listModelsForBackend(context.Background(), model.BackendPi, model.AIProviderConfig{})
	if err != nil || len(piModels) != 1 || piModels[0].Value != "anthropic/claude-sonnet-5" {
		t.Fatalf("unexpected Pi models: %+v, err=%v", piModels, err)
	}
	t.Setenv("PI_EMPTY", "1")
	emptyPi, err := listModelsForBackend(context.Background(), model.BackendPi, model.AIProviderConfig{})
	if err != nil || len(emptyPi) != 0 {
		t.Fatalf("expected 0 Pi models when unauthenticated, got %+v, err=%v", emptyPi, err)
	}
	t.Setenv("PI_EMPTY", "")

	codexSt := ProbeBackend(model.BackendCodex)
	if codexSt.State != BackendStateReady {
		t.Fatalf("ProbeBackend(Codex) = %+v, want Ready", codexSt)
	}
	claudeSt := ProbeBackend(model.BackendClaude)
	if claudeSt.State != BackendStateReady {
		t.Fatalf("ProbeBackend(Claude) = %+v, want Ready", claudeSt)
	}

	ctrl, _, _ := setupReadyController(t)

	if err := ctrl.SetActiveBackend(model.BackendClaude); err != nil {
		t.Fatalf("SetActiveBackend(Claude): %v", err)
	}
	ctrl.SendPrompt("run claude cli")
	waitUntil(t, 4*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	claudeConv := ctrl.CurrentConversation()
	if claudeConv.NativeSessionID != "claude-sess-e2e" {
		t.Fatalf("expected Claude NativeSessionID claude-sess-e2e, got %q", claudeConv.NativeSessionID)
	}
	var gotClaudeText, gotClaudeTool bool
	for _, m := range claudeConv.Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "Claude CLI E2E reply OK") {
			gotClaudeText = true
		}
		for _, st := range m.Process {
			if st.ToolName == "readPage" {
				gotClaudeTool = true
			}
		}
	}
	if !gotClaudeText || !gotClaudeTool {
		t.Fatalf("expected Claude streamed text & tool trace, got %+v", claudeConv.Messages)
	}

	if err := ctrl.SetActiveBackend(model.BackendCodex); err != nil {
		t.Fatalf("SetActiveBackend(Codex): %v", err)
	}
	ctrl.SendPrompt("run codex cli")
	waitUntil(t, 4*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	codexConv := ctrl.CurrentConversation()
	if codexConv.NativeSessionID != "codex-thread-e2e" {
		t.Fatalf("expected Codex NativeSessionID codex-thread-e2e, got %q", codexConv.NativeSessionID)
	}
	var gotCodexText, gotCodexTool bool
	for _, m := range codexConv.Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "Codex CLI E2E reply OK") {
			gotCodexText = true
		}
		for _, st := range m.Process {
			if st.ToolName == "readPage" && st.Done {
				gotCodexTool = true
			}
		}
	}
	if !gotCodexText || !gotCodexTool {
		t.Fatalf("expected Codex streamed text & tool trace, got %+v", codexConv.Messages)
	}
}

func TestNotebookIsolationAndLegacyInference(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	dataDir := t.TempDir()
	nbA := t.TempDir()
	nbB := t.TempDir()
	host := newFakeHost(nbA)
	host.notebooks = []string{nbA, nbB}
	noteA := filepath.Join(nbA, "alpha.md")
	noteB := filepath.Join(nbB, "beta.md")
	if err := host.WriteNote(noteA, "# Alpha\n"); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteNote(noteB, "# Beta\n"); err != nil {
		t.Fatal(err)
	}
	host.activeNote = noteA

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled: true, Provider: "openai", Protocol: model.ProtocolOpenAI,
		BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}
	ctrl.EnableMockResponder(true)

	ctrl.SendPrompt("hello in A")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	convA := ctrl.CurrentConversation()
	if convA == nil || convA.Notebook != normalizeNotebookRoot(nbA) {
		t.Fatalf("expected convA bound to nbA, got %+v", convA)
	}
	ctrl.SetDraftText("draft A")

	// Switch to nbB.
	host.mu.Lock()
	host.root = nbB
	host.activeNote = noteB
	host.mu.Unlock()
	ctrl.OnNotebookSwitched(nbB)

	if ctrl.CurrentConversation() != nil {
		t.Fatal("expected fresh blank conversation on nbB")
	}
	if ctrl.ActiveDraftText() != "" {
		t.Fatalf("expected empty draft on nbB, got %q", ctrl.ActiveDraftText())
	}
	if got := ctrl.Conversations(); len(got) != 0 {
		t.Fatalf("expected 0 conversations in nbB, got %d", len(got))
	}

	ctrl.SendPrompt("hello in B")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	convB := ctrl.CurrentConversation()
	if convB == nil || convB.Notebook != normalizeNotebookRoot(nbB) {
		t.Fatalf("expected convB bound to nbB, got %+v", convB)
	}
	ctrl.SetDraftText("draft B")

	// Switch back to nbA: convA and draft A should be restored.
	host.mu.Lock()
	host.root = nbA
	host.activeNote = noteA
	host.mu.Unlock()
	ctrl.OnNotebookSwitched(nbA)

	if cur := ctrl.CurrentConversation(); cur == nil || cur.ID != convA.ID {
		t.Fatalf("expected convA (%d) restored on nbA, got %+v", convA.ID, cur)
	}
	if ctrl.ActiveDraftText() != "draft A" {
		t.Fatalf("expected draft A restored, got %q", ctrl.ActiveDraftText())
	}
	if got := ctrl.Conversations(); len(got) != 1 || got[0].ID != convA.ID {
		t.Fatalf("expected only convA in nbA Conversations(), got %+v", got)
	}

	// Verify reload restores ByNotebook and infers legacy conversations without Notebook.
	ctrl.mu.Lock()
	ctrl.convs = append(ctrl.convs,
		model.Conversation{
			ID:       901,
			Title:    "Legacy Plan",
			Messages: []model.ChatMessage{{Role: "tool", Plan: &model.BatchPlan{Root: nbB}}},
		},
		model.Conversation{
			ID:       902,
			Title:    "Legacy Sources",
			Messages: []model.ChatMessage{{Role: "assistant", Sources: []model.SourceNote{{Path: noteA}}}},
		},
		model.Conversation{
			ID:       903,
			Title:    "Legacy Refs",
			Messages: []model.ChatMessage{{Role: "user", Refs: []string{noteB}}},
		},
	)
	ctrl.persistChatsLocked()
	ctrl.mu.Unlock()

	reloaded := NewController(dataDir, host, nil)
	if cur := reloaded.CurrentConversation(); cur == nil || cur.ID != convA.ID {
		t.Fatalf("expected reloaded nbA conversation %d, got %+v", convA.ID, cur)
	}
	if reloaded.ActiveDraftText() != "draft A" {
		t.Fatalf("expected reloaded draft A, got %q", reloaded.ActiveDraftText())
	}
	var idsA []uint64
	for _, c := range reloaded.Conversations() {
		idsA = append(idsA, c.ID)
	}
	if len(idsA) != 2 || idsA[0] != convA.ID || idsA[1] != 902 {
		t.Fatalf("unexpected nbA conversations after legacy inference: %v", idsA)
	}

	host.mu.Lock()
	host.root = nbB
	host.activeNote = noteB
	host.mu.Unlock()
	reloaded.OnNotebookSwitched(nbB)
	if cur := reloaded.CurrentConversation(); cur == nil || cur.ID != convB.ID {
		t.Fatalf("expected reloaded nbB conversation %d, got %+v", convB.ID, cur)
	}
	if reloaded.ActiveDraftText() != "draft B" {
		t.Fatalf("expected reloaded draft B, got %q", reloaded.ActiveDraftText())
	}
	if gotB := reloaded.Conversations(); len(gotB) != 3 {
		t.Fatalf("expected 3 conversations in nbB (convB + 901 + 903), got %d", len(gotB))
	}
}

func TestBackgroundStreamAndPlanIsolationAcrossNotebookSwitch(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	dataDir := t.TempDir()
	nbA := t.TempDir()
	nbB := t.TempDir()
	host := newFakeHost(nbA)
	host.notebooks = []string{nbA, nbB}
	noteA := filepath.Join(nbA, "alpha.md")
	noteB := filepath.Join(nbB, "beta.md")
	if err := host.WriteNote(noteA, "# Alpha\n\nold line\n"); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteNote(noteB, "# Beta\n"); err != nil {
		t.Fatal(err)
	}
	host.activeNote = noteA

	switched := make(chan struct{})
	reqCount := 0
	var reqMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqCount++
		turnStep := reqCount
		reqMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		if turnStep == 1 {
			// Wait until the UI switches to nbB before emitting the tool call & finishing step 1.
			<-switched
			argsJSON, _ := json.Marshal(map[string]any{
				"summary": "Update Alpha",
				"operations": []map[string]any{{
					"type":      "search_replace",
					"pageId":    "alpha",
					"oldString": "old line",
					"newString": "new line",
				}},
			})
			chunk, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"delta": map[string]any{
						"tool_calls": []map[string]any{{
							"index": 0,
							"id":    "call_plan_1",
							"function": map[string]any{
								"name":      "executeBatchPlan",
								"arguments": string(argsJSON),
							},
						}},
					},
				}},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		chunk, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"delta": map[string]any{"content": "Background reply done."},
			}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	ctrl := NewController(dataDir, host, nil)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled: true, Provider: "openai", Protocol: model.ProtocolOpenAI,
		BaseURL: srv.URL, APIKey: "k", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}

	ctrl.SendPrompt("edit alpha")
	convA := ctrl.CurrentConversation()
	if convA == nil || !ctrl.IsStreaming() {
		t.Fatal("expected convA streaming after SendPrompt")
	}

	// Switch to nbB while convA is still streaming.
	host.mu.Lock()
	host.root = nbB
	host.activeNote = noteB
	host.mu.Unlock()
	ctrl.OnNotebookSwitched(nbB)
	close(switched)

	if ctrl.IsStreaming() {
		t.Fatal("expected nbB view not streaming while convA streams in background")
	}

	// Wait for convA background stream to complete in the background.
	waitUntil(t, 4*time.Second, func() bool {
		ctrl.mu.RLock()
		defer ctrl.mu.RUnlock()
		c := ctrl.findConvLocked(convA.ID)
		return c != nil && !ctrl.isConvStreamingLocked(convA.ID) && len(c.Messages) >= 4
	})

	// While still on nbB, ActivePlan and host.previewed must remain untouched.
	if ctrl.ActivePlan() != nil {
		t.Fatalf("expected no active plan in nbB, got %+v", ctrl.ActivePlan())
	}
	host.mu.Lock()
	previewedOnB := len(host.previewed)
	host.mu.Unlock()
	if previewedOnB != 0 {
		t.Fatalf("expected no editor plan preview triggered while viewing nbB, got %d", previewedOnB)
	}

	// Switch back to nbA -> convA reply is persisted and its pending plan becomes active.
	host.mu.Lock()
	host.root = nbA
	host.activeNote = noteA
	host.mu.Unlock()
	ctrl.OnNotebookSwitched(nbA)

	cur := ctrl.CurrentConversation()
	if cur == nil || cur.ID != convA.ID {
		t.Fatalf("expected convA restored, got %+v", cur)
	}
	var gotReply bool
	for _, m := range cur.Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "Background reply done.") {
			gotReply = true
		}
	}
	if !gotReply {
		t.Fatalf("expected background assistant reply persisted in convA, got %+v", cur.Messages)
	}
	if ctrl.ActivePlan() == nil || ctrl.ActivePlan().Page != "alpha" {
		t.Fatalf("expected pending plan for alpha linked upon switching back, got %+v", ctrl.ActivePlan())
	}
}

func TestPageTrajectoryAndEmptyPageIDGuards(t *testing.T) {
	theme.SetLanguage(model.LangZh)
	dataDir := t.TempDir()
	nbRoot := t.TempDir()
	host := newFakeHost(nbRoot)
	noteA := filepath.Join(nbRoot, "alpha.md")
	noteB := filepath.Join(nbRoot, "beta.md")
	if err := host.WriteNote(noteA, "# Alpha\n\nhello\n"); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteNote(noteB, "# Beta\n\nworld\n"); err != nil {
		t.Fatal(err)
	}

	ctrl := NewController(dataDir, host, nil)
	ctrl.EnableMockResponder(true)
	if err := ctrl.SaveProviderConfig(model.AIProviderConfig{
		Enabled: true, Provider: "openai", Protocol: model.ProtocolOpenAI,
		BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}

	// Check readPage schema requires pageId.
	var foundReadPage bool
	for _, raw := range builtinToolDefinitions() {
		item, _ := raw.(map[string]any)
		fn, _ := item["function"].(map[string]any)
		if fn["name"] != "readPage" {
			continue
		}
		foundReadPage = true
		params, _ := fn["parameters"].(map[string]any)
		req, _ := params["required"].([]string)
		if len(req) != 1 || req[0] != "pageId" {
			t.Fatalf("expected readPage required=[\"pageId\"], got %+v", params["required"])
		}
	}
	if !foundReadPage {
		t.Fatal("readPage definition missing")
	}

	// Turn 1 with alpha open: OriginPage == "alpha", user msg ActivePage == "alpha".
	host.activeNote = noteA
	ctrl.SendPrompt("first turn")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 2
	})
	conv := ctrl.CurrentConversation()
	if conv.OriginPage != "alpha" {
		t.Fatalf("expected OriginPage=alpha, got %q", conv.OriginPage)
	}
	if conv.Messages[0].ActivePage != "alpha" {
		t.Fatalf("expected first user ActivePage=alpha, got %q", conv.Messages[0].ActivePage)
	}

	// Before switching page, scope allows default page alpha.
	scopeUnchanged := scopeForNotebook(host, nbRoot)
	scopeUnchanged.originPage = conv.OriginPage
	scopeUnchanged.pageSwitched = conversationPageSwitched(conv, scopeUnchanged.page)
	if scopeUnchanged.pageSwitched {
		t.Fatal("expected pageSwitched=false while still on alpha")
	}
	resOK, _, _ := ctrl.executeTool(context.Background(), scopeUnchanged, "readPage", `{}`)
	if !strings.Contains(resOK, "# Alpha") {
		t.Fatalf("expected empty pageId readPage to succeed before switching page, got %s", resOK)
	}

	// Switch to beta and send Turn 2.
	host.mu.Lock()
	host.activeNote = noteB
	host.mu.Unlock()
	ctrl.SendPrompt("second turn")
	waitUntil(t, 2*time.Second, func() bool {
		c := ctrl.CurrentConversation()
		return c != nil && !ctrl.IsStreaming() && len(c.Messages) >= 4
	})
	conv = ctrl.CurrentConversation()
	if conv.OriginPage != "alpha" {
		t.Fatalf("expected OriginPage to stay alpha, got %q", conv.OriginPage)
	}
	var userActivePages []string
	for _, m := range conv.Messages {
		if m.Role == "user" {
			userActivePages = append(userActivePages, m.ActivePage)
		}
	}
	if len(userActivePages) != 2 || userActivePages[0] != "alpha" || userActivePages[1] != "beta" {
		t.Fatalf("unexpected user ActivePage trajectory: %v", userActivePages)
	}

	// Both builtin and CLI prompts include trajectory header + historical ActivePage tag.
	scopeSwitched := scopeForNotebook(host, nbRoot)
	scopeSwitched.originPage = conv.OriginPage
	scopeSwitched.pageSwitched = conversationPageSwitched(conv, scopeSwitched.page)
	if !scopeSwitched.pageSwitched {
		t.Fatal("expected pageSwitched=true after switching to beta")
	}
	turn := &assistantTurn{notebookScope: scopeSwitched, convID: conv.ID, userIndex: len(conv.Messages) - 2}
	ctrl.mu.RLock()
	builtinMsgs := ctrl.buildBuiltinAPIMessagesLocked(turn, 0)
	ctrl.mu.RUnlock()
	sysText, _ := builtinMsgs[0]["content"].(string)
	if !strings.Contains(sysText, "会话起点页：alpha") || !strings.Contains(sysText, "本条发送时打开的页面：beta") {
		t.Fatalf("builtin system prompt missing page trajectory: %s", sysText)
	}
	cliPrompt := cliTurnPrompt(scopeSwitched, conv.Messages, len(conv.Messages)-2)
	if !strings.Contains(cliPrompt, "会话起点页：alpha") || !strings.Contains(cliPrompt, "[发送时打开页面: alpha]") {
		t.Fatalf("CLI prompt missing page trajectory or historical ActivePage: %s", cliPrompt)
	}

	// Empty pageId is rejected for readPage and non-create executeBatchPlan when pageSwitched.
	resReadErr, _, _ := ctrl.executeTool(context.Background(), scopeSwitched, "readPage", `{}`)
	if !strings.Contains(resReadErr, "切换过打开页面") {
		t.Fatalf("expected bilingual switched-page error on readPage, got %s", resReadErr)
	}
	resPlanErr, planNil, _ := ctrl.executeTool(context.Background(), scopeSwitched, "executeBatchPlan", `{"operations":[{"type":"search_replace","oldString":"world","newString":"new"}]}`)
	if planNil != nil || !strings.Contains(resPlanErr, "切换过打开页面") {
		t.Fatalf("expected bilingual switched-page error on executeBatchPlan, got %s", resPlanErr)
	}

	// create operation without pageId remains allowed.
	resCreateOK, createPlan, _ := ctrl.executeTool(context.Background(), scopeSwitched, "executeBatchPlan", `{"operations":[{"type":"create","title":"gamma","markdown":"# Gamma"}]}`)
	if createPlan == nil || !strings.Contains(resCreateOK, "awaiting_approval") {
		t.Fatalf("expected create operation without pageId to succeed, got %s", resCreateOK)
	}

	// No default open page also rejects empty pageId.
	scopeNoPage := notebookScope{root: scopeSwitched.root, info: scopeSwitched.info}
	resNoPageRead, _, _ := ctrl.executeTool(context.Background(), scopeNoPage, "readPage", `{}`)
	if !strings.Contains(resNoPageRead, "没有默认打开的页面") {
		t.Fatalf("expected no-default-page error on readPage, got %s", resNoPageRead)
	}
	resNoPagePlan, _, _ := ctrl.executeTool(context.Background(), scopeNoPage, "executeBatchPlan", `{"operations":[{"type":"edit","markdown":"# X"}]}`)
	if !strings.Contains(resNoPagePlan, "没有默认打开的页面") {
		t.Fatalf("expected no-default-page error on executeBatchPlan, got %s", resNoPagePlan)
	}
}
