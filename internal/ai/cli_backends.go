package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"block/internal/model"
	"block/internal/theme"
)

type BackendState uint8

const (
	BackendStateReady BackendState = iota
	BackendStateInstalled
	BackendStateMissing
	BackendStateUnauth
	BackendStateUnsupported
	BackendStateError
)

// BackendStatus describes the detected installation and authentication state of a backend.
type BackendStatus struct {
	State   BackendState
	Version string
	Message string
}

// ModelOption represents one selectable model entry for a backend.
type ModelOption struct {
	Value string
	Label string
}

func (c *Controller) SetBackendStatusOverride(kind model.BackendKind, status *BackendStatus) {
	c.mu.Lock()
	if status == nil {
		delete(c.statusOverrides, kind)
	} else {
		c.statusOverrides[kind] = *status
		c.backendStatuses[kind] = *status
	}
	c.ensureValidSelectionLocked()
	c.mu.Unlock()
	c.requestRedraw()
}

func enrichedPATH() string {
	var parts []string
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	if cur := os.Getenv("PATH"); cur != "" {
		for _, p := range filepath.SplitList(cur) {
			add(p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, rel := range []string{
			".npm-global/bin",
			".local/share/fnm/aliases/default/bin",
			".local/bin",
			".bun/bin",
			".cargo/bin",
		} {
			add(filepath.Join(home, rel))
		}
	}
	for _, fixed := range []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	} {
		add(fixed)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func resolveCLIBinary(bin string) (string, bool) {
	for _, dir := range filepath.SplitList(enrichedPATH()) {
		cand := filepath.Join(dir, bin)
		info, err := os.Stat(cand)
		if err == nil && !info.IsDir() {
			return cand, true
		}
	}
	return "", false
}

func cliCommand(ctx context.Context, bin string, args ...string) *exec.Cmd {
	exe := bin
	if found, ok := resolveCLIBinary(bin); ok {
		exe = found
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(), "PATH="+enrichedPATH())
	return cmd
}

func isCLIInstalled(kind model.BackendKind) bool {
	if kind == model.BackendBuiltin {
		return true
	}
	_, ok := resolveCLIBinary(string(kind))
	return ok
}

// ProbeBackend detects CLI availability and auth status for kind.
func ProbeBackend(kind model.BackendKind) BackendStatus {
	if kind == model.BackendBuiltin {
		return BackendStatus{State: BackendStateReady, Version: "Block"}
	}
	if !isCLIInstalled(kind) {
		return BackendStatus{State: BackendStateMissing}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch kind {
	case model.BackendCodex:
		out, err := cliCommand(ctx, "codex", "--version").Output()
		if err != nil {
			return BackendStatus{State: BackendStateError, Message: theme.Tr("无法检测 Codex CLI 版本", "Failed to detect Codex CLI version")}
		}
		ver := strings.TrimSpace(string(out))
		if err := cliCommand(ctx, "codex", "login", "status").Run(); err != nil {
			return BackendStatus{State: BackendStateUnauth, Version: ver, Message: theme.Tr("请先使用 codex login 登录", "Sign in with codex login first")}
		}
		return BackendStatus{State: BackendStateReady, Version: ver}

	case model.BackendClaude:
		out, err := cliCommand(ctx, "claude", "--version").Output()
		if err != nil {
			return BackendStatus{State: BackendStateError, Message: "Claude CLI version check failed"}
		}
		ver := strings.TrimSpace(string(out))
		if !claudeVersionOK(ver) {
			return BackendStatus{State: BackendStateUnsupported, Version: ver, Message: "Claude CLI 2.1.318+ required: " + ver}
		}
		authOut, err := cliCommand(ctx, "claude", "auth", "status", "--json").Output()
		if err == nil {
			var auth struct {
				LoggedIn *bool `json:"loggedIn"`
			}
			if json.Unmarshal(authOut, &auth) == nil && auth.LoggedIn != nil && !*auth.LoggedIn {
				hasEnvKey := false
				for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
					if os.Getenv(k) != "" {
						hasEnvKey = true
						break
					}
				}
				if !hasEnvKey {
					return BackendStatus{State: BackendStateUnauth, Version: ver, Message: "Claude CLI is not authenticated"}
				}
			}
		}
		return BackendStatus{State: BackendStateReady, Version: ver}

	case model.BackendDsh:
		out, err := cliCommand(ctx, "dsh", "--version").Output()
		if err != nil {
			return BackendStatus{State: BackendStateError, Message: err.Error()}
		}
		return BackendStatus{State: BackendStateReady, Version: strings.TrimSpace(string(out))}

	case model.BackendPi:
		out, err := cliCommand(ctx, "pi", "--version").Output()
		if err != nil {
			return BackendStatus{State: BackendStateError, Message: err.Error()}
		}
		return BackendStatus{State: BackendStateReady, Version: strings.TrimSpace(string(out))}

	case model.BackendAgy:
		out, err := cliCommand(ctx, "agy", "--version").Output()
		if err != nil {
			return BackendStatus{State: BackendStateInstalled}
		}
		return BackendStatus{State: BackendStateInstalled, Version: strings.TrimSpace(string(out))}

	default:
		return BackendStatus{State: BackendStateMissing}
	}
}

func claudeVersionOK(ver string) bool {
	tok := strings.Fields(ver)
	if len(tok) == 0 {
		return false
	}
	parts := strings.Split(tok[0], ".")
	if len(parts) < 3 {
		return false
	}
	maj, err1 := strconv.Atoi(parts[0])
	minV, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	if maj != 2 {
		return maj > 2
	}
	if minV != 1 {
		return minV > 1
	}
	return pat >= 318
}

func (c *Controller) probeAllBackendsAsync() {
	for _, kind := range model.AllBackends {
		if kind == model.BackendBuiltin {
			continue
		}
		k := kind
		go func() {
			c.mu.RLock()
			_, overridden := c.statusOverrides[k]
			c.mu.RUnlock()
			if overridden {
				return
			}
			st := ProbeBackend(k)
			c.mu.Lock()
			if ov, ok := c.statusOverrides[k]; ok {
				st = ov
			}
			prev, had := c.backendStatuses[k]
			c.backendStatuses[k] = st
			c.ensureValidSelectionLocked()
			changed := !had || prev != st
			c.mu.Unlock()
			if changed {
				c.requestRedraw()
			}
		}()
	}
}

func (c *Controller) isBackendRecognizedLocked(kind model.BackendKind) bool {
	if kind == model.BackendBuiltin {
		return true
	}
	if ov, ok := c.statusOverrides[kind]; ok {
		return ov.State != BackendStateMissing
	}
	if st, ok := c.backendStatuses[kind]; ok {
		return st.State != BackendStateMissing
	}
	return isCLIInstalled(kind)
}

func (c *Controller) recognizedBackendsLocked() []model.BackendKind {
	var out []model.BackendKind
	for _, kind := range model.AllBackends {
		if c.isBackendRecognizedLocked(kind) {
			out = append(out, kind)
		}
	}
	return out
}

func (c *Controller) priorityOrderLocked(recognized []model.BackendKind) []model.BackendKind {
	recSet := make(map[model.BackendKind]bool, len(recognized))
	for _, k := range recognized {
		recSet[k] = true
	}
	seen := make(map[model.BackendKind]bool, len(recognized))
	order := make([]model.BackendKind, 0, len(recognized))
	for _, k := range c.backendsCfg.Priority {
		if recSet[k] && !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	for _, k := range recognized {
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	return order
}

func (c *Controller) visibleBackendsLocked() []model.BackendKind {
	recognized := c.recognizedBackendsLocked()
	order := c.priorityOrderLocked(recognized)
	var enabled []model.BackendKind
	for _, k := range order {
		if isBackendEnabled(c.backendsCfg, k) {
			enabled = append(enabled, k)
		}
	}
	if len(enabled) == 0 && len(recognized) > 0 {
		return recognized[:1]
	}
	return enabled
}

func (c *Controller) ensureValidSelectionLocked() {
	recognized := c.recognizedBackendsLocked()
	for _, r := range recognized {
		if r == c.backendsCfg.Selected && isBackendEnabled(c.backendsCfg, r) {
			return
		}
	}
	visible := c.visibleBackendsLocked()
	if len(visible) > 0 {
		c.backendsCfg.Selected = visible[0]
	} else {
		c.backendsCfg.Selected = model.BackendBuiltin
	}
}

func (c *Controller) selectedBackendLocked() model.BackendKind {
	visible := c.visibleBackendsLocked()
	cand := c.backendsCfg.Selected
	if conv := c.findConvLocked(c.currentID); conv != nil && conv.Backend != "" {
		cand = conv.Backend
	}
	for _, v := range visible {
		if v == cand {
			return cand
		}
	}
	if len(visible) > 0 {
		return visible[0]
	}
	return model.BackendBuiltin
}

func (c *Controller) selectBackendLocked(kind model.BackendKind) {
	if c.selectedBackendLocked() == kind {
		return
	}
	if c.currentID != 0 {
		draft := c.draftText
		refs := append([]string(nil), c.draftRefs...)
		skills := append([]string(nil), c.draftSkills...)
		c.saveTabDraftLocked()
		c.stopStreamingLocked()
		c.clearActivePlanLocked()
		c.currentID = 0
		c.draftText = draft
		c.draftRefs = refs
		c.draftSkills = skills
		c.composerEpoch++
	} else {
		c.turnError = ""
		c.turnErrorSettings = false
		c.retryDraft = nil
	}
	c.backendsCfg.Selected = kind
	_ = c.writeJSONFile("ai-backends.json", c.backendsCfg)
}

func (c *Controller) backendThinkingLocked(kind model.BackendKind) string {
	if opt, ok := c.backendsCfg.Adapters[string(kind)]; ok {
		switch opt.ThinkingLevel {
		case "low", "medium", "high":
			return opt.ThinkingLevel
		}
	}
	return "high"
}

func (c *Controller) backendModelLocked(kind model.BackendKind) string {
	if kind == model.BackendBuiltin {
		return c.providerCfg.Model
	}
	if opt, ok := c.backendsCfg.Adapters[string(kind)]; ok {
		return opt.Model
	}
	return ""
}

func (c *Controller) setBackendModelLocked(kind model.BackendKind, m string) error {
	m = strings.TrimSpace(m)
	if kind == model.BackendBuiltin {
		c.providerCfg.Model = m
		c.settingsModel = m
		if c.providerCfg.Providers == nil {
			c.providerCfg.Providers = make(map[string]model.ProviderProfile)
		}
		if c.providerCfg.Provider != "" {
			prof := c.providerCfg.Providers[c.providerCfg.Provider]
			prof.Model = m
			c.providerCfg.Providers[c.providerCfg.Provider] = prof
		}
		return c.writeJSONFile("ai.json", c.providerCfg)
	}
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	if opt.Locked {
		return errors.New(theme.Tr("后端已锁定", "Backend is locked"))
	}
	opt.Model = m
	c.backendsCfg.Adapters[string(kind)] = opt
	return c.writeJSONFile("ai-backends.json", c.backendsCfg)
}

func (c *Controller) setBackendThinkingLocked(kind model.BackendKind, level string) error {
	switch level {
	case "low", "medium", "high":
	default:
		level = "high"
	}
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	if opt.Locked {
		return errors.New(theme.Tr("后端已锁定", "Backend is locked"))
	}
	opt.ThinkingLevel = level
	c.backendsCfg.Adapters[string(kind)] = opt
	return c.writeJSONFile("ai-backends.json", c.backendsCfg)
}

func (c *Controller) setBackendFullPermissionsLocked(kind model.BackendKind, on bool) error {
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	if opt.Locked {
		return errors.New(theme.Tr("后端已锁定", "Backend is locked"))
	}
	opt.FullPermissions = on
	c.backendsCfg.Adapters[string(kind)] = opt
	return c.writeJSONFile("ai-backends.json", c.backendsCfg)
}

func (c *Controller) refreshModelsAsync(kind model.BackendKind) {
	c.mu.Lock()
	if c.modelStatus[kind] == "loading" {
		c.mu.Unlock()
		return
	}
	c.modelStatus[kind] = "loading"
	cfg := c.providerCfg
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		opts, err := listModelsForBackend(ctx, kind, cfg)
		c.mu.Lock()
		if err != nil {
			c.modelStatus[kind] = "error:" + err.Error()
		} else {
			c.backendModels[kind] = opts
			c.modelStatus[kind] = "ready"
		}
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func listModelsForBackend(ctx context.Context, kind model.BackendKind, cfg model.AIProviderConfig) ([]ModelOption, error) {
	switch kind {
	case model.BackendBuiltin:
		ids, err := listBuiltinModels(ctx, normalizeProviderConfig(cfg))
		if err != nil {
			return nil, err
		}
		out := make([]ModelOption, 0, len(ids))
		for _, id := range ids {
			out = append(out, ModelOption{Value: id, Label: id})
		}
		return out, nil
	case model.BackendDsh:
		return []ModelOption{
			{Value: "deepseek-flash", Label: "DeepSeek-V4.1-Flash"},
			{Value: "deepseek-v4-pro", Label: "DeepSeek-V4-Pro"},
		}, nil
	case model.BackendPi:
		out, err := cliCommand(ctx, "pi", "--list-models").Output()
		if err != nil {
			return nil, err
		}
		var opts []ModelOption
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || (fields[0] == "provider" && fields[1] == "model") {
				continue
			}
			val := fields[0] + "/" + fields[1]
			opts = append(opts, ModelOption{Value: val, Label: val})
		}
		return opts, nil
	case model.BackendAgy:
		out, err := cliCommand(ctx, "agy", "models").Output()
		if err != nil {
			return nil, err
		}
		var opts []ModelOption
		for _, line := range strings.Split(string(out), "\n") {
			val, label, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			if val != "" {
				opts = append(opts, ModelOption{Value: val, Label: strings.TrimSpace(label)})
			}
		}
		return opts, nil
	case model.BackendCodex:
		return []ModelOption{
			{Value: "gpt-5.3-codex", Label: "GPT-5.3 Codex"},
			{Value: "o3", Label: "o3"},
		}, nil
	case model.BackendClaude:
		return []ModelOption{
			{Value: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6"},
			{Value: "claude-opus-4-6", Label: "Claude Opus 4.6"},
		}, nil
	default:
		return nil, nil
	}
}

// mcpBridgeServer provides a local HTTP/JSON-RPC MCP server for external CLI backends.
type mcpBridgeServer struct {
	addr   string
	token  string
	server *http.Server
	ln     net.Listener
}

func startMCPBridge(ctrl *Controller, convID uint64) (*mcpBridgeServer, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(raw[:])

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		auth := r.Header.Get("Authorization")
		tokHdr := r.Header.Get("X-Block-Token")
		if auth != "Bearer "+token && tokHdr != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var rpc struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      any            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024)).Decode(&rpc); err != nil {
			writeRPCError(w, rpc.ID, -32700, "invalid JSON")
			return
		}
		switch rpc.Method {
		case "initialize":
			writeRPCResult(w, rpc.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "block", "version": "1.0"},
			})
		case "tools/list":
			defs := builtinToolDefinitions()
			tools := make([]map[string]any, 0, len(defs))
			for _, d := range defs {
				if dm, ok := d.(map[string]any); ok {
					if fn, ok := dm["function"].(map[string]any); ok {
						tools = append(tools, map[string]any{
							"name":        fn["name"],
							"description": fn["description"],
							"inputSchema": fn["parameters"],
						})
					}
				}
			}
			writeRPCResult(w, rpc.ID, map[string]any{"tools": tools})
		case "tools/call":
			name, _ := rpc.Params["name"].(string)
			argsVal := rpc.Params["arguments"]
			rawArgs, _ := json.Marshal(argsVal)
			ctrl.mu.Lock()
			resJSON, plan, sources := ctrl.executeToolLocked(name, string(rawArgs))
			if plan != nil && ctrl.currentID == convID {
				ctrl.pushMessageLocked(model.ChatMessage{
					Role:    "tool",
					Content: resJSON,
					Plan:    plan,
					Sources: sources,
				})
				conv := ctrl.findConvLocked(convID)
				if conv != nil && len(conv.Messages) > 0 {
					ctrl.linkPlanLocked(convID, len(conv.Messages)-1)
				}
			}
			ctrl.mu.Unlock()
			ctrl.requestRedraw()
			writeRPCResult(w, rpc.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": resJSON}},
			})
		default:
			writeRPCError(w, rpc.ID, -32601, "method not found")
		}
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()

	return &mcpBridgeServer{
		addr:   "http://" + ln.Addr().String() + "/mcp",
		token:  token,
		server: srv,
		ln:     ln,
	}, nil
}

func (b *mcpBridgeServer) close() {
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = b.server.Shutdown(ctx)
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	})
}

type runningExternalTurn struct {
	mu      sync.Mutex
	cancel  func()
	respond func(requestID string, decision model.ApprovalDecision) error
	bridge  *mcpBridgeServer
}

func (c *Controller) startExternalTurn(convID uint64, kind model.BackendKind) {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil {
		c.mu.Unlock()
		return
	}
	var promptText string
	if c.sentDraft != nil && c.sentDraft.MessageIndex < len(conv.Messages) {
		m := conv.Messages[c.sentDraft.MessageIndex]
		promptText = m.Content + m.Context
	} else if len(conv.Messages) > 0 {
		m := conv.Messages[len(conv.Messages)-1]
		promptText = m.Content + m.Context
	}
	root := c.activeRootLocked()
	thinking := c.backendThinkingLocked(kind)
	selectedModel := c.backendModelLocked(kind)
	fullPerm := c.backendsCfg.Adapters[string(kind)].FullPermissions
	nativeSession := conv.NativeSessionID

	c.streamSerial++
	serial := c.streamSerial
	c.streaming = true
	c.partialText = ""
	c.partialThink = ""
	c.mu.Unlock()

	bridge, err := startMCPBridge(c, convID)
	if err != nil {
		c.failExternalTurn(convID, serial, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	turn := &runningExternalTurn{
		cancel: func() {
			cancel()
			bridge.close()
		},
		bridge: bridge,
	}

	c.mu.Lock()
	c.activeExternal = turn
	c.streamCancel = turn.cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			bridge.close()
			c.mu.Lock()
			if c.activeExternal == turn {
				c.activeExternal = nil
			}
			c.mu.Unlock()
		}()

		err := c.runCLIAdapterProcess(ctx, convID, serial, kind, root, promptText, selectedModel, thinking, fullPerm, nativeSession, bridge, turn)
		c.mu.Lock()
		if c.streamSerial != serial || c.currentID != convID {
			c.mu.Unlock()
			return
		}
		c.streaming = false
		c.streamCancel = nil
		if err != nil && ctx.Err() == nil {
			c.turnError = err.Error()
			c.recordTurnOutcomeLocked("failed")
			c.restoreFailedDraftLocked()
		} else if ctx.Err() == nil {
			if strings.TrimSpace(c.partialText) != "" {
				c.pushMessageLocked(model.ChatMessage{
					Role:    "assistant",
					Content: c.partialText,
				})
				c.partialText = ""
			}
			c.recordTurnOutcomeLocked("completed")
			c.sentDraft = nil
		}
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func (c *Controller) failExternalTurn(convID uint64, serial uint64, msg string) {
	c.mu.Lock()
	if c.streamSerial == serial && c.currentID == convID {
		c.streaming = false
		c.streamCancel = nil
		c.turnError = msg
		c.recordTurnOutcomeLocked("failed")
		c.restoreFailedDraftLocked()
	}
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) runCLIAdapterProcess(
	ctx context.Context,
	convID, serial uint64,
	kind model.BackendKind,
	root, prompt, modelName, thinking string,
	fullPerm bool,
	nativeSession string,
	bridge *mcpBridgeServer,
	turn *runningExternalTurn,
) error {
	if !isCLIInstalled(kind) {
		return fmt.Errorf("%s CLI 未安装或不在 PATH 中", kind.DisplayName())
	}

	var args []string
	switch kind {
	case model.BackendClaude:
		mcpCfg, _ := json.Marshal(map[string]any{
			"mcpServers": map[string]any{
				"block": map[string]any{
					"type": "http",
					"url":  bridge.addr,
					"headers": map[string]string{
						"Authorization": "Bearer " + bridge.token,
					},
				},
			},
		})
		args = []string{"-p", "--verbose", "--output-format", "stream-json", "--mcp-config", string(mcpCfg)}
		if modelName != "" {
			args = append(args, "--model", modelName)
		}
		if fullPerm {
			args = append(args, "--dangerously-skip-permissions")
		}
		if nativeSession != "" {
			args = append(args, "--resume", nativeSession)
		}
		args = append(args, prompt)

	case model.BackendCodex:
		args = []string{"app-server"}

	case model.BackendDsh:
		args = []string{"--profile", "sdk-minimal"}

	case model.BackendPi:
		args = []string{"--mode", "json", "--thinking", thinking}
		if fullPerm {
			args = append(args, "--approve")
		}
		if modelName != "" {
			args = append(args, "--model", modelName)
		}
		if nativeSession != "" {
			args = append(args, "--session-id", nativeSession)
		}
		args = append(args, "--", prompt)

	case model.BackendAgy:
		args = []string{"acp"}
		if fullPerm {
			args = append(args, "--yolo")
		}
	}

	cmd := cliCommand(ctx, string(kind), args...)
	if root != "" {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			cmd.Dir = root
		}
	}
	cmd.Env = append(cmd.Env,
		"BLOCK_MCP_URL="+bridge.addr,
		"BLOCK_MCP_TOKEN="+bridge.token,
	)

	var stdin io.WriteCloser
	var err error
	if kind == model.BackendCodex || kind == model.BackendDsh || kind == model.BackendAgy {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return err
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	turn.mu.Lock()
	turn.respond = func(requestID string, decision model.ApprovalDecision) error {
		if stdin == nil {
			return nil
		}
		dec := "decline"
		if decision == model.ApprovalAllow {
			dec = "accept"
		}
		idVal := any(requestID)
		if n, perr := strconv.ParseInt(requestID, 10, 64); perr == nil {
			idVal = n
		}
		payload, _ := json.Marshal(map[string]any{
			"id":     idVal,
			"result": map[string]any{"decision": dec},
		})
		_, werr := fmt.Fprintf(stdin, "%s\n", payload)
		return werr
	}
	turn.mu.Unlock()

	if stdin != nil && kind == model.BackendCodex {
		_, _ = fmt.Fprintf(stdin, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"block","version":"1.0"}}}`+"\n")
		_, _ = fmt.Fprintf(stdin, `{"id":2,"method":"thread/start","params":{"cwd":%q,"model":%q}}`+"\n", cmd.Dir, modelName)
		_, _ = fmt.Fprintf(stdin, `{"id":3,"method":"turn/start","params":{"input":[{"type":"text","text":%q}],"effort":%q}}`+"\n", prompt, thinking)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var frame map[string]any
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		c.handleCLIStreamFrame(convID, serial, frame)
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if waitErr != nil {
		errTxt := strings.TrimSpace(stderr.String())
		if errTxt == "" {
			errTxt = waitErr.Error()
		}
		return errors.New(errTxt)
	}
	return nil
}

func (c *Controller) handleCLIStreamFrame(convID, serial uint64, frame map[string]any) {
	c.mu.Lock()
	if c.streamSerial != serial || c.currentID != convID {
		c.mu.Unlock()
		return
	}
	typ, _ := frame["type"].(string)
	method, _ := frame["method"].(string)

	switch {
	case typ == "assistant":
		if msg, ok := frame["message"].(map[string]any); ok {
			if blocks, ok := msg["content"].([]any); ok {
				for _, b := range blocks {
					bm, _ := b.(map[string]any)
					if bm["type"] == "text" {
						if txt, ok := bm["text"].(string); ok {
							c.partialText += txt
						}
					}
				}
			}
		}
	case typ == "message_update":
		if sub, ok := frame["assistantMessageEvent"].(map[string]any); ok {
			if sub["type"] == "text_delta" {
				if d, ok := sub["delta"].(string); ok {
					c.partialText += d
				}
			}
		}
	case method == "item/agentMessage/delta":
		if params, ok := frame["params"].(map[string]any); ok {
			if d, ok := params["delta"].(string); ok {
				c.partialText += d
			}
		}
	case typ == "session":
		if id, ok := frame["id"].(string); ok && id != "" {
			if conv := c.findConvLocked(convID); conv != nil {
				conv.NativeSessionID = id
				c.persistChatsLocked()
			}
		}
	case frame["id"] != nil && method != "":
		reqID := fmt.Sprint(frame["id"])
		c.pushMessageLocked(model.ChatMessage{
			Role: "assistant",
			Approval: &model.ApprovalCard{
				RequestID: reqID,
				ToolName:  method,
				Command:   method,
			},
		})
	}
	c.mu.Unlock()
	c.requestRedraw()
}
