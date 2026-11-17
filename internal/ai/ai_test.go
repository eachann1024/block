package ai

import (
	"bytes"
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
	mu           sync.Mutex
	root         string
	notebooks    []string
	activeNote   string
	files        map[string]string
	openedNotes  []string
	openedTab    string
	previewed    []*model.BatchPlan
	applied      []*model.BatchPlan
	rejected     []string
	undone       []*model.BatchPlan
	redrawCount  int
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

	// Reload from disk.
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
	ctrl, _, _ := setupReadyController(t)
	bridge, err := startMCPBridge(ctrl, 1)
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

	srcToggle, ok := bui.TargetCenter("ai-sources-toggle")
	if !ok {
		t.Fatal("missing ai-sources-toggle")
	}
	tt.ClickAt(srcToggle.X, srcToggle.Y)
	if !tt.HasText("daily.md") {
		t.Fatal("expected source note daily.md listed")
	}

	copyPt, ok := bui.TargetCenter("ai-msg-copy-0")
	if !ok {
		t.Fatal("missing ai-msg-copy-0")
	}
	tt.ClickAt(copyPt.X, copyPt.Y)
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
	if !containsString(ctrl.draftRefs, "picker-smoke/note-1199") {
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

	// Test Settings Tab rendering & interaction.
	ttSet := ui.NewTester(ctrl.RenderSettingsTab, 760, 620)
	if _, ok := bui.TargetBounds("ai-settings-section"); !ok {
		t.Fatal("missing ai-settings-section")
	}
	if err := ttSet.Click("OpenAI 协议"); err != nil {
		t.Fatalf("click OpenAI provider card: %v", err)
	}
	if ctrl.ProviderConfig().Provider != "openai" {
		t.Fatalf("expected openai provider selected, got %q", ctrl.ProviderConfig().Provider)
	}
}
