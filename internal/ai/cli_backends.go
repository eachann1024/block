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
	Group string
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

var customCLIBinaryPaths sync.Map // map[string]string (bin name -> custom path)

func setCustomCLIBinaryPath(kind model.BackendKind, customPath string) {
	trimmed := strings.TrimSpace(customPath)
	if trimmed == "" {
		customCLIBinaryPaths.Delete(string(kind))
	} else {
		customCLIBinaryPaths.Store(string(kind), trimmed)
	}
}

func expandHomePath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func resolveAutoCLIBinary(bin string) (string, bool) {
	for _, dir := range filepath.SplitList(enrichedPATH()) {
		cand := filepath.Join(dir, bin)
		info, err := os.Stat(cand)
		if err == nil && !info.IsDir() {
			return cand, true
		}
	}
	return "", false
}

func resolveCLIBinary(bin string) (string, bool) {
	if raw, ok := customCLIBinaryPaths.Load(bin); ok {
		if custom, ok2 := raw.(string); ok2 && strings.TrimSpace(custom) != "" {
			cand := expandHomePath(custom)
			if info, err := os.Stat(cand); err == nil && !info.IsDir() {
				return cand, true
			}
			return "", false
		}
	}
	return resolveAutoCLIBinary(bin)
}

// AgentDriverGuide returns CLI install and sign-in commands for the Add Agent wizard.
func AgentDriverGuide(kind model.BackendKind) (installCmd, loginCmd string) {
	switch kind {
	case model.BackendCodex:
		return "npm install -g @openai/codex", "codex login"
	case model.BackendClaude:
		return "npm install -g @anthropic-ai/claude-code", "claude auth login"
	case model.BackendDsh:
		return "curl -fsSL https://dsh.deepseek.com/install.sh | sh", "dsh login"
	case model.BackendPi:
		return "npm install -g @mariozechner/pi-coding-agent", "pi --login"
	case model.BackendAgy:
		return "brew install antigravity", "agy login"
	default:
		return "", ""
	}
}

// ProbeBackendWithPath detects CLI availability and auth status using an optional custom executable path.
func ProbeBackendWithPath(kind model.BackendKind, customPath string) BackendStatus {
	trimmed := strings.TrimSpace(customPath)
	if trimmed == "" {
		return ProbeBackend(kind)
	}
	cand := expandHomePath(trimmed)
	info, err := os.Stat(cand)
	if err != nil || info.IsDir() {
		return BackendStatus{
			State:   BackendStateMissing,
			Message: theme.Tr("指定的可执行文件不存在", "Specified executable path does not exist"),
		}
	}
	prevVal, hadPrev := customCLIBinaryPaths.Load(string(kind))
	customCLIBinaryPaths.Store(string(kind), cand)
	defer func() {
		if hadPrev {
			customCLIBinaryPaths.Store(string(kind), prevVal)
		} else {
			customCLIBinaryPaths.Delete(string(kind))
		}
	}()
	return ProbeBackend(kind)
}

func cliCommand(ctx context.Context, bin string, args ...string) *exec.Cmd {
	exe := bin
	if found, ok := resolveCLIBinary(bin); ok {
		exe = found
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(envWithout(os.Environ(), "PATH"), "PATH="+enrichedPATH())
	return cmd
}

func envWithout(env []string, dropKeys ...string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		skip := false
		for _, k := range dropKeys {
			if strings.HasPrefix(entry, k+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, entry)
		}
	}
	return out
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
	opt, hasOpt := c.backendsCfg.Adapters[string(kind)]
	if !hasOpt || !opt.Added {
		return false
	}
	if st, ok := c.backendStatuses[kind]; ok {
		return st.State != BackendStateMissing
	}
	return isCLIInstalled(kind)
}

func (c *Controller) addedCLIAgentsLocked() []model.BackendKind {
	var out []model.BackendKind
	for _, kind := range model.AllBackends {
		if kind == model.BackendBuiltin {
			continue
		}
		if opt, ok := c.backendsCfg.Adapters[string(kind)]; ok && opt.Added {
			out = append(out, kind)
		}
	}
	return out
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

func (c *Controller) isCLIAvailableLocked(kind model.BackendKind) bool {
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

// SetActiveBackend switches the active backend (adding & enabling it if installed) and persists selection.
func (c *Controller) SetActiveBackend(kind model.BackendKind) error {
	c.mu.Lock()
	if !c.isCLIAvailableLocked(kind) {
		c.mu.Unlock()
		return fmt.Errorf("%s CLI 未安装或不可用", kind.DisplayName())
	}
	if kind != model.BackendBuiltin {
		if c.backendsCfg.Adapters == nil {
			c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
		}
		opt := c.backendsCfg.Adapters[string(kind)]
		on := true
		opt.Added = true
		opt.Enabled = &on
		opt.FullPermissions = true
		c.backendsCfg.Adapters[string(kind)] = opt
	}
	c.selectBackendLocked(kind)
	c.backendsCfg.Selected = kind
	err := c.writeJSONFile("ai-backends.json", c.backendsCfg)
	c.mu.Unlock()
	c.requestRedraw()
	return err
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

func (c *Controller) setBackendFullPermissionsLocked(kind model.BackendKind, _ bool) error {
	if c.backendsCfg.Adapters == nil {
		c.backendsCfg.Adapters = make(map[string]model.AIBackendAdapterOptions)
	}
	opt := c.backendsCfg.Adapters[string(kind)]
	opt.FullPermissions = true
	c.backendsCfg.Adapters[string(kind)] = opt
	return c.writeJSONFile("ai-backends.json", c.backendsCfg)
}

const modelCacheFreshFor = 5 * time.Minute

func (c *Controller) effectiveBackendModelLocked(kind model.BackendKind) string {
	stored := strings.TrimSpace(c.backendModelLocked(kind))
	choices := c.backendModels[kind]
	if stored != "" {
		if len(choices) == 0 {
			return stored
		}
		for _, ch := range choices {
			if ch.Value == stored {
				return stored
			}
		}
	}
	if len(choices) > 0 {
		return choices[0].Value
	}
	return stored
}

func (c *Controller) modelLabelLocked(kind model.BackendKind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, ch := range c.backendModels[kind] {
		if ch.Value == value && strings.TrimSpace(ch.Label) != "" {
			return ch.Label
		}
	}
	if kind == model.BackendBuiltin {
		if _, pureModel := splitProviderModelRef(value); pureModel != "" {
			return pureModel
		}
	}
	return value
}

func (c *Controller) modelFullLabelLocked(kind model.BackendKind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, ch := range c.backendModels[kind] {
		if ch.Value == value {
			lbl := strings.TrimSpace(ch.Label)
			if lbl == "" {
				lbl = value
			}
			if grp := strings.TrimSpace(ch.Group); grp != "" {
				return grp + " · " + lbl
			}
			return lbl
		}
	}
	if kind == model.BackendBuiltin {
		if provID, pureModel := splitProviderModelRef(value); provID != "" && pureModel != "" {
			if pr, ok := c.providerCfg.Providers[provID]; ok {
				return providerDisplayName(provID, pr) + " · " + pureModel
			}
			return provID + " · " + pureModel
		}
	}
	return value
}

func (c *Controller) invalidateModelCacheLocked(kind model.BackendKind) {
	c.modelGenSeq++
	c.modelGeneration[kind] = c.modelGenSeq
	delete(c.backendModels, kind)
	delete(c.modelStatus, kind)
	delete(c.modelRefreshed, kind)
}

func (c *Controller) beginModelRefreshLocked(kind model.BackendKind) (uint64, bool) {
	if c.modelStatus[kind] == "loading" {
		return 0, false
	}
	if at := c.modelRefreshed[kind]; !at.IsZero() && time.Since(at) < modelCacheFreshFor {
		return 0, false
	}
	c.modelGenSeq++
	gen := c.modelGenSeq
	c.modelGeneration[kind] = gen
	c.modelStatus[kind] = "loading"
	return gen, true
}

// finishModelRefreshLocked records model discovery results without discarding a usable cached list on failure.
func (c *Controller) finishModelRefreshLocked(kind model.BackendKind, gen uint64, opts []ModelOption, err error) bool {
	if c.modelGeneration[kind] != gen {
		return false
	}
	if err != nil {
		c.modelStatus[kind] = "error:" + fmt.Sprintf("%s: %v", theme.Tr("获取模型列表失败", "Could not load models"), err)
		return true
	}
	c.backendModels[kind] = opts
	c.modelRefreshed[kind] = time.Now()
	c.modelStatus[kind] = "ready"
	if c.annotCfg.Backend == kind && strings.TrimSpace(c.annotCfg.Model) == "" && len(opts) > 0 {
		next := c.annotCfg
		next.Model = opts[0].Value
		_ = c.saveAnnotationConfigLocked(next)
	}
	return true
}

func (c *Controller) refreshModelsAsync(kind model.BackendKind) {
	c.mu.Lock()
	gen, ok := c.beginModelRefreshLocked(kind)
	cfg := c.providerCfg
	c.mu.Unlock()
	if !ok {
		return
	}
	c.requestRedraw()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		opts, err := listModelsForBackend(ctx, kind, cfg)
		c.mu.Lock()
		changed := c.finishModelRefreshLocked(kind, gen, opts, err)
		c.mu.Unlock()
		if changed {
			c.requestRedraw()
		}
	}()
}

func (c *Controller) refreshAnnotationModelsAsync(kind model.BackendKind) {
	if kind == "" {
		kind = model.BackendBuiltin
	}
	c.mu.Lock()
	gen, ok := c.beginModelRefreshLocked(kind)
	var cfg model.AIProviderConfig
	var cfgErr error
	if kind == model.BackendBuiltin && c.annotCfg.Backend == model.BackendBuiltin {
		cfg, cfgErr = c.annotationBuiltinConfigLocked()
	} else {
		cfg = c.providerCfg
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	c.requestRedraw()

	go func() {
		var opts []ModelOption
		var err error
		if cfgErr != nil {
			err = cfgErr
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			opts, err = listModelsForBackend(ctx, kind, cfg)
		}
		c.mu.Lock()
		changed := c.finishModelRefreshLocked(kind, gen, opts, err)
		c.mu.Unlock()
		if changed {
			c.requestRedraw()
		}
	}()
}

func listModelsForBackend(ctx context.Context, kind model.BackendKind, cfg model.AIProviderConfig) ([]ModelOption, error) {
	switch kind {
	case model.BackendBuiltin:
		entries := orderedProviderEntries(cfg)
		var enabledEntries []ProviderEntry
		for _, e := range entries {
			if e.Enabled && strings.TrimSpace(e.BaseURL) != "" {
				enabledEntries = append(enabledEntries, e)
			}
		}
		if len(enabledEntries) <= 1 {
			ids, err := listBuiltinModels(ctx, resolveProviderExecutionConfig(cfg))
			if err != nil {
				return nil, err
			}
			out := make([]ModelOption, 0, len(ids))
			for _, id := range ids {
				out = append(out, ModelOption{Value: id, Label: id})
			}
			return out, nil
		}
		var out []ModelOption
		var firstErr error
		normPrimary := normalizeProviderConfig(cfg).Provider
		for _, e := range enabledEntries {
			subCfg := model.AIProviderConfig{
				Enabled:  true,
				Provider: e.ID,
				Protocol: e.Protocol,
				BaseURL:  e.BaseURL,
				APIKey:   e.APIKey,
			}
			ids, err := listBuiltinModels(ctx, subCfg)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, id := range ids {
				val := e.ID + "::" + id
				if e.ID == normPrimary {
					val = id
				}
				out = append(out, ModelOption{
					Value: val,
					Label: id,
					Group: e.Name,
				})
			}
		}
		if len(out) == 0 && firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	case model.BackendDsh:
		// Official dsh 0.2.1-rc.5 advisory defaults (dsh CLI has no model list subcommand).
		return []ModelOption{
			{Value: "deepseek-flash", Label: "DeepSeek-V4.1-Flash"},
			{Value: "deepseek-v4-pro", Label: "DeepSeek-V4-Pro"},
		}, nil
	case model.BackendPi:
		return listPiModels(ctx)
	case model.BackendAgy:
		return listAgyModels(ctx)
	case model.BackendCodex:
		return listCodexModels(ctx)
	case model.BackendClaude:
		return listClaudeModels(ctx)
	default:
		return nil, nil
	}
}

func listPiModels(ctx context.Context) ([]ModelOption, error) {
	out, err := cliCommand(ctx, "pi", "--list-models").Output()
	if err != nil {
		return nil, errors.New("Pi model list failed")
	}
	text := string(out)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "no models available") {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	hasHeader := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "provider" && fields[1] == "model" {
			hasHeader = true
			break
		}
	}
	var opts []ModelOption
	afterHeader := !hasHeader
	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "provider" && fields[1] == "model" {
			afterHeader = true
			continue
		}
		if !afterHeader || strings.HasPrefix(fields[0], "-") {
			continue
		}
		val := fields[0] + "/" + fields[1]
		opts = append(opts, ModelOption{Value: val, Label: val})
	}
	return opts, nil
}

func listAgyModels(ctx context.Context) ([]ModelOption, error) {
	agyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := cliCommand(agyCtx, "agy", "models").Output()
	if err != nil {
		return nil, fmt.Errorf("agy models exited with %v; check agy login", err)
	}
	var opts []ModelOption
	for _, line := range strings.Split(string(out), "\n") {
		val, label, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		label = strings.TrimSpace(label)
		if val == "" {
			continue
		}
		if label == "" {
			label = val
		}
		opts = append(opts, ModelOption{Value: val, Label: label})
	}
	if len(opts) == 0 {
		return nil, errors.New("agy returned no models; check agy login")
	}
	return opts, nil
}

func listClaudeModels(parent context.Context) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()

	cmd := cliCommand(
		ctx,
		"claude",
		"-p",
		"--verbose",
		"--dangerously-skip-permissions",
		"--permission-mode", "bypassPermissions",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	)
	cmd.Env = envWithout(cmd.Env, "CLAUDECODE")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	initReq := `{"type":"control_request","request_id":"block_models","request":{"subtype":"initialize","hooks":null}}` + "\n"
	if _, err := io.WriteString(stdin, initReq); err != nil {
		return nil, err
	}

	type result struct {
		opts []ModelOption
		err  error
	}
	done := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var frame struct {
				Response *struct {
					RequestID string `json:"request_id"`
					Response  *struct {
						Models []struct {
							Value       string `json:"value"`
							DisplayName string `json:"displayName"`
						} `json:"models"`
					} `json:"response"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(line), &frame) != nil || frame.Response == nil {
				continue
			}
			if frame.Response.RequestID != "block_models" {
				continue
			}
			if frame.Response.Response == nil {
				done <- result{err: errors.New("Claude did not return a model list")}
				return
			}
			opts := make([]ModelOption, 0, len(frame.Response.Response.Models))
			for _, m := range frame.Response.Response.Models {
				val := strings.TrimSpace(m.Value)
				if val == "" {
					continue
				}
				lbl := strings.TrimSpace(m.DisplayName)
				if lbl == "" {
					lbl = val
				}
				opts = append(opts, ModelOption{Value: val, Label: lbl})
			}
			done <- result{opts: opts}
			return
		}
		done <- result{err: errors.New("Claude exited before returning models")}
	}()

	select {
	case res := <-done:
		return res.opts, res.err
	case <-ctx.Done():
		return nil, errors.New("Claude model list timed out")
	}
}

func listCodexModels(parent context.Context) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()

	cmd := cliCommand(ctx, "codex", "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	type result struct {
		opts []ModelOption
		err  error
	}
	done := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		sendRPC := func(payload map[string]any) error {
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdin, "%s\n", raw)
			return err
		}
		waitResponse := func(wantID int) (map[string]any, error) {
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if !strings.HasPrefix(line, "{") {
					continue
				}
				var msg map[string]any
				if json.Unmarshal([]byte(line), &msg) != nil {
					continue
				}
				_, hasMethod := msg["method"]
				idNum, hasID := rpcIntID(msg["id"])
				if hasID && !hasMethod && idNum == wantID {
					if errObj, hasErr := msg["error"]; hasErr && errObj != nil {
						rawErr, _ := json.Marshal(errObj)
						return nil, errors.New(string(rawErr))
					}
					res, ok := msg["result"].(map[string]any)
					if !ok {
						return nil, errors.New("Codex response missing result")
					}
					return res, nil
				}
				if msg["id"] != nil && hasMethod {
					_ = sendRPC(map[string]any{
						"id":     msg["id"],
						"result": map[string]any{"decision": "accept"},
					})
				}
			}
			return nil, errors.New("Codex app-server exited")
		}

		if err := sendRPC(map[string]any{
			"id":     1,
			"method": "initialize",
			"params": map[string]any{
				"clientInfo":   map[string]any{"name": "block", "version": "1"},
				"capabilities": map[string]any{"experimentalApi": true},
			},
		}); err != nil {
			done <- result{err: err}
			return
		}
		if _, err := waitResponse(1); err != nil {
			done <- result{err: err}
			return
		}
		if err := sendRPC(map[string]any{
			"method": "initialized",
			"params": map[string]any{},
		}); err != nil {
			done <- result{err: err}
			return
		}

		var opts []ModelOption
		var cursor any
		for page := 0; page < 20; page++ {
			reqID := 2 + page
			if err := sendRPC(map[string]any{
				"id":     reqID,
				"method": "model/list",
				"params": map[string]any{"limit": 100, "cursor": cursor},
			}); err != nil {
				done <- result{err: err}
				return
			}
			res, err := waitResponse(reqID)
			if err != nil {
				done <- result{err: err}
				return
			}
			data, ok := res["data"].([]any)
			if !ok {
				done <- result{err: errors.New("Codex model list is unavailable")}
				return
			}
			for _, rawItem := range data {
				item, ok := rawItem.(map[string]any)
				if !ok {
					continue
				}
				if hidden, _ := item["hidden"].(bool); hidden {
					continue
				}
				val, _ := item["model"].(string)
				if strings.TrimSpace(val) == "" {
					val, _ = item["id"].(string)
				}
				val = strings.TrimSpace(val)
				if val == "" {
					continue
				}
				lbl, _ := item["displayName"].(string)
				lbl = strings.TrimSpace(lbl)
				if lbl == "" {
					lbl = val
				}
				opts = append(opts, ModelOption{Value: val, Label: lbl})
			}
			nextCur, _ := res["nextCursor"].(string)
			if nextCur == "" {
				break
			}
			cursor = nextCur
		}
		done <- result{opts: opts}
	}()

	select {
	case res := <-done:
		return res.opts, res.err
	case <-ctx.Done():
		return nil, errors.New("Codex model list timed out")
	}
}

func rpcIntID(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
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
			if ctrl.streaming && ctrl.currentID == convID {
				ctrl.partialSteps = append(ctrl.partialSteps, processStepForTool(name, string(rawArgs)))
			}
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
	if conv == nil || !c.providerCfg.Enabled {
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
	selectedModel := c.effectiveBackendModelLocked(kind)
	fullPerm := true
	nativeSession := conv.NativeSessionID

	c.streamSerial++
	serial := c.streamSerial
	c.streaming = true
	c.partialText = ""
	c.partialThink = ""
	c.partialSteps = nil
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
	if c.streamSerial != serial || c.currentID != convID || !c.providerCfg.Enabled {
		c.mu.Unlock()
		turn.cancel()
		return
	}
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
			var steps []model.ProcessStep
			if strings.TrimSpace(c.partialThink) != "" {
				steps = append(steps, model.ProcessStep{
					ID:     fmt.Sprintf("think-%d", convID),
					Kind:   model.ToolKindThink,
					Title:  theme.Tr("思考过程", "Thinking"),
					Detail: strings.TrimSpace(c.partialThink),
					Done:   true,
				})
			}
			steps = append(steps, c.partialSteps...)
			if strings.TrimSpace(c.partialText) != "" || len(steps) > 0 {
				c.pushMessageLocked(model.ChatMessage{
					Role:    "assistant",
					Content: c.partialText,
					Process: steps,
				})
			}
			c.partialText = ""
			c.partialThink = ""
			c.partialSteps = nil
			c.recordTurnOutcomeLocked("completed")
			c.sentDraft = nil
		}
		c.clearStreamRedrawLocked()
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func (c *Controller) failExternalTurn(convID uint64, serial uint64, msg string) {
	c.mu.Lock()
	if c.streamSerial == serial && c.currentID == convID {
		c.streaming = false
		c.streamCancel = nil
		c.clearStreamRedrawLocked()
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
		if thinking != "" {
			args = append(args, "--effort", thinking)
		}
		if modelName != "" && modelName != "default" {
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
	if kind == model.BackendClaude {
		cmd.Env = envWithout(cmd.Env, "CLAUDECODE")
	}
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

	var stdinClosed sync.Once
	closeStdin := func() {
		if stdin != nil {
			stdinClosed.Do(func() { _ = stdin.Close() })
		}
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
		if c.handleCLIStreamFrame(convID, serial, frame) {
			closeStdin()
		}
	}
	closeStdin()
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

func (c *Controller) handleCLIStreamFrame(convID, serial uint64, frame map[string]any) bool {
	c.mu.Lock()
	if c.streamSerial != serial || c.currentID != convID {
		c.mu.Unlock()
		return false
	}
	typ, _ := frame["type"].(string)
	method, _ := frame["method"].(string)
	turnDone := false
	isDelta := false

	upsertStep := func(step model.ProcessStep) {
		if step.ID != "" {
			for i := range c.partialSteps {
				if c.partialSteps[i].ID == step.ID {
					c.partialSteps[i] = step
					return
				}
			}
		}
		c.partialSteps = append(c.partialSteps, step)
	}

	switch {
	case typ == "system":
		if id, ok := frame["session_id"].(string); ok && id != "" {
			if conv := c.findConvLocked(convID); conv != nil {
				conv.NativeSessionID = id
				c.persistChatsLocked()
			}
		}
	case typ == "assistant":
		if msg, ok := frame["message"].(map[string]any); ok {
			if blocks, ok := msg["content"].([]any); ok {
				for _, b := range blocks {
					bm, _ := b.(map[string]any)
					switch bm["type"] {
					case "text":
						if txt, ok := bm["text"].(string); ok {
							c.partialText += txt
						}
					case "thinking":
						if th, ok := bm["thinking"].(string); ok && strings.TrimSpace(th) != "" {
							c.partialSteps = append(c.partialSteps, model.ProcessStep{
								ID:     fmt.Sprintf("think-%d-%d", convID, len(c.partialSteps)),
								Kind:   model.ToolKindThink,
								Title:  theme.Tr("思考过程", "Thinking"),
								Detail: strings.TrimSpace(th),
								Done:   true,
							})
						}
					case "tool_use":
						name, _ := bm["name"].(string)
						rawArgs, _ := json.Marshal(bm["input"])
						step := processStepForTool(name, string(rawArgs))
						if id, _ := bm["id"].(string); id != "" {
							step.ID = id
						}
						upsertStep(step)
					}
				}
			}
		}
	case typ == "result":
		if id, ok := frame["session_id"].(string); ok && id != "" {
			if conv := c.findConvLocked(convID); conv != nil {
				conv.NativeSessionID = id
				c.persistChatsLocked()
			}
		}
		if res, ok := frame["result"].(string); ok && res != "" && strings.TrimSpace(c.partialText) == "" {
			c.partialText = res
		}
	case typ == "message_update":
		isDelta = true
		if sub, ok := frame["assistantMessageEvent"].(map[string]any); ok {
			switch sub["type"] {
			case "text_delta":
				if d, ok := sub["delta"].(string); ok {
					c.partialText += d
				}
			case "thinking_delta":
				if d, ok := sub["delta"].(string); ok {
					c.partialThink += d
				}
			}
		}
	case method == "thread/started":
		if params, ok := frame["params"].(map[string]any); ok {
			if thr, ok := params["thread"].(map[string]any); ok {
				if id, ok := thr["id"].(string); ok && id != "" {
					if conv := c.findConvLocked(convID); conv != nil {
						conv.NativeSessionID = id
						c.persistChatsLocked()
					}
				}
			}
		}
	case method == "item/agentMessage/delta":
		isDelta = true
		if params, ok := frame["params"].(map[string]any); ok {
			if d, ok := params["delta"].(string); ok {
				c.partialText += d
			}
		}
	case method == "item/reasoning/summaryTextDelta" || method == "item/reasoning/textDelta":
		isDelta = true
		if params, ok := frame["params"].(map[string]any); ok {
			if d, ok := params["delta"].(string); ok {
				c.partialThink += d
			}
		}
	case method == "item/started" || method == "item/completed":
		if params, ok := frame["params"].(map[string]any); ok {
			if item, ok := params["item"].(map[string]any); ok {
				itemID, _ := item["id"].(string)
				itemType, _ := item["type"].(string)
				done := method == "item/completed"
				switch itemType {
				case "mcpToolCall":
					toolName, _ := item["tool"].(string)
					rawArgs, _ := json.Marshal(item["arguments"])
					step := processStepForTool(toolName, string(rawArgs))
					if itemID != "" {
						step.ID = itemID
					}
					step.Done = done
					upsertStep(step)
				case "commandExecution":
					cmdStr, _ := item["command"].(string)
					upsertStep(model.ProcessStep{
						ID:       itemID,
						Kind:     model.ToolKindRun,
						Title:    theme.Tr("执行命令", "Run command"),
						Detail:   cmdStr,
						ToolName: "bash",
						Done:     done,
					})
				case "fileChange":
					upsertStep(model.ProcessStep{
						ID:       itemID,
						Kind:     model.ToolKindWrite,
						Title:    theme.Tr("修改文件", "Edit file"),
						ToolName: "fileChange",
						Done:     done,
					})
				}
			}
		}
	case method == "turn/completed":
		turnDone = true
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
	if !isDelta {
		c.clearStreamRedrawLocked()
		c.mu.Unlock()
		c.requestRedraw()
		return turnDone
	}
	c.mu.Unlock()
	c.requestStreamRedraw()
	return turnDone
}
