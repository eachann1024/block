package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"block/assets"
	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ProviderPreset defines a built-in AI API provider preset.
type ProviderPreset struct {
	ID          string
	Zh          string
	En          string
	Protocol    model.Protocol
	BaseURL     string
	BrandColor  ui.Color
	Icon        icons.Icon
	KeyRequired bool
}

var providerPresets = []ProviderPreset{
	{
		ID:          "deepseek",
		Zh:          "DeepSeek",
		En:          "DeepSeek",
		Protocol:    model.ProtocolOpenAI,
		BaseURL:     "https://api.deepseek.com/v1",
		BrandColor:  ui.Hex("#4d6bfe"), // design:allow brand
		Icon:        icons.ProvDeepseek,
		KeyRequired: true,
	},
	{
		ID:          "openai",
		Zh:          "OpenAI 协议",
		En:          "OpenAI Protocol",
		Protocol:    model.ProtocolOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		BrandColor:  ui.Hex("#10a37f"), // design:allow brand
		Icon:        icons.ProvOpenai,
		KeyRequired: true,
	},
	{
		ID:          "anthropic",
		Zh:          "Claude 协议",
		En:          "Claude Protocol",
		Protocol:    model.ProtocolAnthropic,
		BaseURL:     "https://api.anthropic.com/v1",
		BrandColor:  ui.Hex("#d97757"), // design:allow brand
		Icon:        icons.ProvAnthropic,
		KeyRequired: true,
	},
}

func findProviderPreset(id string) *ProviderPreset {
	if id == "claude" {
		id = "anthropic"
	}
	for i := range providerPresets {
		if providerPresets[i].ID == id {
			return &providerPresets[i]
		}
	}
	return nil
}

func normalizeProviderID(id string, proto model.Protocol, baseURL string) string {
	low := strings.ToLower(strings.TrimSpace(baseURL))
	if id == "deepseek" || strings.Contains(low, "deepseek") {
		return "deepseek"
	}
	if id == "anthropic" || id == "claude" || proto == model.ProtocolAnthropic || strings.Contains(low, "anthropic") {
		return "anthropic"
	}
	if strings.TrimSpace(id) == "" && strings.TrimSpace(baseURL) == "" {
		return "deepseek"
	}
	return "openai"
}

func normalizeProviderConfig(cfg model.AIProviderConfig) model.AIProviderConfig {
	cfg.Provider = normalizeProviderID(cfg.Provider, cfg.Protocol, cfg.BaseURL)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	if p := findProviderPreset(cfg.Provider); p != nil {
		cfg.Protocol = p.Protocol
		if cfg.BaseURL == "" {
			cfg.BaseURL = p.BaseURL
		}
	}
	if cfg.Protocol == "" {
		cfg.Protocol = model.ProtocolOpenAI
	}
	return cfg
}

func validateProviderConfig(cfg model.AIProviderConfig) (string, bool) {
	if !cfg.Enabled {
		return theme.Tr("请先在「设置 › AI 助手」开启 AI 功能", "Enable AI Assistant in Settings › AI Assistant first"), true
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return theme.Tr("请前往「设置 › AI 助手」配置服务商与接口地址", "Go to Settings › AI Assistant to configure a provider and Base URL"), true
	}
	keyReq := true
	if p := findProviderPreset(cfg.Provider); p != nil {
		keyReq = p.KeyRequired
	}
	if keyReq && strings.TrimSpace(cfg.APIKey) == "" {
		return theme.Tr("请前往「设置 › AI 助手」选择服务商并填写 API 密钥", "Go to Settings › AI Assistant to select a provider and enter an API Key"), true
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return theme.Tr("请前往「设置 › AI 助手」选择默认模型", "Go to Settings › AI Assistant to select a default model"), true
	}
	return "", false
}

func builtinReady(cfg model.AIProviderConfig) bool {
	msg, _ := validateProviderConfig(cfg)
	return msg == ""
}

// IsLiveProviderReady reports whether the built-in provider is configured and mock mode is disabled.
func (c *Controller) IsLiveProviderReady() bool {
	return !c.IsMockEnabled() && builtinReady(normalizeProviderConfig(c.ProviderConfig()))
}

// RefreshBuiltinModels queries the configured built-in provider's /models endpoint and caches the result.
func (c *Controller) RefreshBuiltinModels(ctx context.Context) ([]ModelOption, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}
	c.mu.Lock()
	c.modelGenSeq++
	gen := c.modelGenSeq
	c.modelGeneration[model.BackendBuiltin] = gen
	c.modelStatus[model.BackendBuiltin] = "loading"
	cfg := normalizeProviderConfig(c.providerCfg)
	c.mu.Unlock()

	opts, err := listModelsForBackend(ctx, model.BackendBuiltin, cfg)
	c.mu.Lock()
	_ = c.finishModelRefreshLocked(model.BackendBuiltin, gen, opts, err)
	c.mu.Unlock()
	c.requestRedraw()
	return opts, err
}

func (c *Controller) annotationBuiltinConfigLocked() (model.AIProviderConfig, error) {
	cfg := normalizeProviderConfig(c.providerCfg)
	targetProv := strings.TrimSpace(c.annotCfg.Provider)
	if targetProv != "" && targetProv != cfg.Provider {
		prof, ok := cfg.Providers[targetProv]
		if !ok {
			return cfg, errors.New(theme.Tr("标注使用的服务商尚未配置", "The annotation provider is not configured"))
		}
		cfg.Provider = targetProv
		cfg.Protocol = prof.Protocol
		cfg.BaseURL = prof.BaseURL
		cfg.APIKey = prof.APIKey
		cfg = normalizeProviderConfig(cfg)
	}
	cfg.Model = strings.TrimSpace(c.annotCfg.Model)
	return cfg, nil
}

// TestConnection validates the built-in provider configuration against its live completion endpoint.
func (c *Controller) TestConnection(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	cfg := normalizeProviderConfig(c.ProviderConfig())
	err := testBuiltinConnection(ctx, cfg)
	c.mu.Lock()
	c.settingsTesting = false
	if err != nil {
		c.settingsTestOK = false
		c.settingsTestErr = err.Error()
	} else {
		c.settingsTestOK = true
		c.settingsTestErr = ""
	}
	c.mu.Unlock()
	c.requestRedraw()
	return err
}

// CompletePrompt runs a single-shot streaming SSE completion against the configured live provider.
func (c *Controller) CompletePrompt(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return c.StreamPrompt(ctx, systemPrompt, userPrompt, nil)
}

// StreamPrompt runs a streaming SSE completion and invokes onDelta on each incremental chunk.
func (c *Controller) StreamPrompt(ctx context.Context, systemPrompt, userPrompt string, onDelta func(chunk string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := normalizeProviderConfig(c.ProviderConfig())
	if msg, _ := validateProviderConfig(cfg); msg != "" {
		return "", errors.New(msg)
	}
	msgs := make([]map[string]any, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": systemPrompt})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": userPrompt})

	events := make(chan sseEvent, 32)
	go streamBuiltinCompletion(ctx, cfg, msgs, nil, "", events)

	var sb strings.Builder
	for ev := range events {
		if ev.delta != "" {
			sb.WriteString(ev.delta)
			if onDelta != nil {
				onDelta(ev.delta)
			}
		}
		if ev.err != "" {
			return "", errors.New(ev.err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", errors.New(formatProviderError(err.Error()))
	}
	return strings.TrimSpace(sb.String()), nil
}

func formatProviderError(raw string) string {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "401") || strings.Contains(low, "403") || strings.Contains(low, "unauthorized") ||
		strings.Contains(low, "authentication_error") || strings.Contains(low, "api_key") || strings.Contains(low, "api key"):
		return theme.Tr("密钥无效或未配置", "Invalid or missing API Key")
	case strings.Contains(low, "timeout") || strings.Contains(low, "timed out") ||
		strings.Contains(low, "connection refused") || strings.Contains(low, "no such host"):
		return theme.Tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL")
	case strings.Contains(low, "canceled") || strings.Contains(low, "cancelled") || strings.Contains(low, "abort"):
		return theme.Tr("已停止", "Stopped")
	default:
		return raw
	}
}

type streamToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type sseEvent struct {
	thinking  string
	delta     string
	toolCalls []streamToolCall
	done      bool
	err       string
	settings  bool
}

var sharedHTTPClient = &http.Client{
	Timeout: 180 * time.Second,
}

func doJSONRequest(ctx context.Context, cfg model.AIProviderConfig, path string, body any) (*http.Response, error) {
	var reader io.Reader
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Protocol == model.ProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
		if cfg.APIKey != "" {
			req.Header.Set("x-api-key", cfg.APIKey)
		}
	} else if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	return sharedHTTPClient.Do(req)
}

func readHTTPError(resp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && msg != "" {
				return formatProviderError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg))
			}
		}
		if msg, ok := parsed["message"].(string); ok && msg != "" {
			return formatProviderError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg))
		}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return theme.Tr("密钥无效或未配置", "Invalid or missing API Key")
	}
	return fmt.Sprintf("AI service returned %d", resp.StatusCode)
}

// listBuiltinModels queries GET /models from the configured OpenAI/DeepSeek/Anthropic endpoint.
func listBuiltinModels(ctx context.Context, cfg model.AIProviderConfig) ([]string, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New(theme.Tr("请前往「设置 › AI 助手」配置服务商与接口地址", "Go to Settings › AI Assistant to configure a provider and Base URL"))
	}
	keyReq := true
	if p := findProviderPreset(cfg.Provider); p != nil {
		keyReq = p.KeyRequired
	}
	if keyReq && strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New(theme.Tr("请前往「设置 › AI 助手」选择服务商并填写 API 密钥", "Go to Settings › AI Assistant to select a provider and enter an API Key"))
	}
	path := "/models"
	if cfg.Protocol == model.ProtocolAnthropic {
		path = "/models?limit=1000"
	}
	resp, err := doJSONRequest(ctx, cfg, path, nil)
	if err != nil {
		return nil, errors.New(formatProviderError(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(readHTTPError(resp))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

// testBuiltinConnection performs a minimal completion check against the configured endpoint.
func testBuiltinConnection(ctx context.Context, cfg model.AIProviderConfig) error {
	if msg, _ := validateProviderConfig(cfg); msg != "" {
		return errors.New(msg)
	}
	path := "/chat/completions"
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": 8,
		"messages":   []map[string]any{{"role": "user", "content": "ping"}},
	}
	if cfg.Protocol == model.ProtocolAnthropic {
		path = "/messages"
	}
	resp, err := doJSONRequest(ctx, cfg, path, body)
	if err != nil {
		return errors.New(formatProviderError(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(readHTTPError(resp))
	}
	return nil
}

func streamBuiltinCompletion(ctx context.Context, cfg model.AIProviderConfig, msgs []map[string]any, tools []any, thinkingLevel string, out chan<- sseEvent) {
	defer close(out)
	if cfg.Protocol == model.ProtocolAnthropic {
		streamAnthropicSSE(ctx, cfg, msgs, tools, thinkingLevel, out)
		return
	}
	streamOpenAISSE(ctx, cfg, msgs, tools, thinkingLevel, out)
}

func streamOpenAISSE(ctx context.Context, cfg model.AIProviderConfig, msgs []map[string]any, tools []any, thinkingLevel string, out chan<- sseEvent) {
	body := map[string]any{
		"model":    cfg.Model,
		"stream":   true,
		"messages": msgs,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if thinkingLevel != "" {
		body["reasoning_effort"] = thinkingLevel
		if cfg.Provider == "deepseek" {
			body["thinking"] = map[string]any{"type": "enabled"}
		}
	}

	resp, err := doJSONRequest(ctx, cfg, "/chat/completions", body)
	if err != nil {
		out <- sseEvent{err: formatProviderError(err.Error()), settings: true}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out <- sseEvent{
			err:      readHTTPError(resp),
			settings: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound,
		}
		return
	}

	var calls []streamToolCall
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	completed := false
	gotAny := false

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			completed = true
			continue
		}
		var chunk struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			out <- sseEvent{err: formatProviderError(chunk.Error.Message)}
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			completed = true
		}
		if r := ch.Delta.ReasoningContent; r != "" {
			gotAny = true
			out <- sseEvent{thinking: r}
		} else if r := ch.Delta.Reasoning; r != "" {
			gotAny = true
			out <- sseEvent{thinking: r}
		}
		for _, tc := range ch.Delta.ToolCalls {
			idx := len(calls)
			if tc.Index != nil && *tc.Index >= 0 {
				idx = *tc.Index
			}
			for len(calls) <= idx {
				calls = append(calls, streamToolCall{})
			}
			if tc.ID != "" {
				calls[idx].ID = tc.ID
			}
			if tc.Function.Name != "" {
				calls[idx].Name += tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				calls[idx].Arguments += tc.Function.Arguments
			}
			gotAny = true
		}
		if ch.Delta.Content != "" {
			gotAny = true
			out <- sseEvent{delta: ch.Delta.Content}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		out <- sseEvent{err: formatProviderError(err.Error())}
		return
	}
	if !completed && !gotAny {
		out <- sseEvent{err: theme.Tr("AI 未返回可写入的内容。", "AI returned an empty response.")}
		return
	}
	for i := range calls {
		if calls[i].ID == "" {
			calls[i].ID = fmt.Sprintf("call_%d", i)
		}
		if strings.TrimSpace(calls[i].Arguments) == "" {
			calls[i].Arguments = "{}"
		}
	}
	out <- sseEvent{toolCalls: calls, done: true}
}

func streamAnthropicSSE(ctx context.Context, cfg model.AIProviderConfig, msgs []map[string]any, tools []any, thinkingLevel string, out chan<- sseEvent) {
	systemText, anthropicMsgs := convertMessagesToAnthropic(msgs)
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": 8192,
		"stream":     true,
		"messages":   anthropicMsgs,
	}
	if systemText != "" {
		body["system"] = systemText
	}
	if len(tools) > 0 {
		body["tools"] = convertToolsToAnthropic(tools)
	}
	if thinkingLevel != "" {
		body["thinking"] = map[string]any{"type": "adaptive"}
		body["output_config"] = map[string]any{"effort": thinkingLevel}
	}

	resp, err := doJSONRequest(ctx, cfg, "/messages", body)
	if err != nil {
		out <- sseEvent{err: formatProviderError(err.Error()), settings: true}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out <- sseEvent{
			err:      readHTTPError(resp),
			settings: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound,
		}
		return
	}

	type anthropicCall struct {
		blockIdx int
		call     streamToolCall
	}
	var calls []anthropicCall
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	completed := false

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Message      string `json:"message"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_stop":
			completed = true
		case "error":
			msg := theme.Tr("Anthropic 服务返回错误", "Anthropic service error")
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			} else if ev.Message != "" {
				msg = ev.Message
			}
			out <- sseEvent{err: formatProviderError(msg)}
			return
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				calls = append(calls, anthropicCall{
					blockIdx: ev.Index,
					call: streamToolCall{
						ID:   ev.ContentBlock.ID,
						Name: ev.ContentBlock.Name,
					},
				})
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					out <- sseEvent{delta: ev.Delta.Text}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					out <- sseEvent{thinking: ev.Delta.Thinking}
				}
			case "input_json_delta":
				for i := range calls {
					if calls[i].blockIdx == ev.Index {
						calls[i].call.Arguments += ev.Delta.PartialJSON
						break
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		out <- sseEvent{err: formatProviderError(err.Error())}
		return
	}
	if !completed && len(calls) == 0 {
		out <- sseEvent{err: theme.Tr("AI 响应中断，请重试。", "AI response interrupted. Please retry.")}
		return
	}
	finalCalls := make([]streamToolCall, len(calls))
	for i, c := range calls {
		if c.call.ID == "" {
			c.call.ID = fmt.Sprintf("call_%d", i)
		}
		if strings.TrimSpace(c.call.Arguments) == "" {
			c.call.Arguments = "{}"
		}
		finalCalls[i] = c.call
	}
	out <- sseEvent{toolCalls: finalCalls, done: true}
}

func convertMessagesToAnthropic(msgs []map[string]any) (string, []map[string]any) {
	var systems []string
	var out []map[string]any
	var pendingToolResults []map[string]any

	flushTools := func() {
		if len(pendingToolResults) > 0 {
			out = append(out, map[string]any{
				"role":    "user",
				"content": pendingToolResults,
			})
			pendingToolResults = nil
		}
	}

	for _, m := range msgs {
		role, _ := m["role"].(string)
		switch role {
		case "system":
			if s, _ := m["content"].(string); strings.TrimSpace(s) != "" {
				systems = append(systems, s)
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			content, _ := m["content"].(string)
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": id,
				"content":     content,
			})
		case "assistant":
			flushTools()
			text, _ := m["content"].(string)
			if rawCalls, ok := m["tool_calls"].([]map[string]any); ok && len(rawCalls) > 0 {
				var blocks []map[string]any
				if text != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": text})
				}
				for _, tc := range rawCalls {
					id, _ := tc["id"].(string)
					fn, _ := tc["function"].(map[string]any)
					name, _ := fn["name"].(string)
					argStr, _ := fn["arguments"].(string)
					var input map[string]any
					if json.Unmarshal([]byte(argStr), &input) != nil || input == nil {
						input = map[string]any{}
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    id,
						"name":  name,
						"input": input,
					})
				}
				out = append(out, map[string]any{"role": "assistant", "content": blocks})
			} else {
				out = append(out, map[string]any{"role": "assistant", "content": text})
			}
		default:
			flushTools()
			out = append(out, map[string]any{"role": "user", "content": m["content"]})
		}
	}
	flushTools()
	return strings.Join(systems, "\n\n"), out
}

func convertToolsToAnthropic(tools []any) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":         fn["name"],
			"description":  fn["description"],
			"input_schema": fn["parameters"],
		})
	}
	return out
}

func builtinToolDefinitions() []any {
	skillIDs := []string{"createNoote", "updateNote", "searchNotes", "chat", "markdown"}
	fn := func(name, desc string, params map[string]any) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters":  params,
			},
		}
	}
	return []any{
		fn("loadSkill", "有明确任务时加载最匹配的 Skill。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill": map[string]any{"type": "string", "enum": skillIDs},
			},
			"required": []string{"skill"},
		}),
		fn("listNotebooks", "列出已登记的笔记本根目录。", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
		fn("listPages", "列出笔记本的页面；可指定 listNotebooks 返回的 notebookRoot。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"notebookRoot": map[string]any{"type": "string"},
			},
		}),
		fn("searchNotes", "在笔记本中按关键词搜索页面，返回前 20 条命中及片段。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"notebookRoot": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
		fn("readPage", "读取页面的标题和 Markdown 正文；省略 pageId 时读取当前打开页面。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pageId":       map[string]any{"type": "string"},
				"notebookRoot": map[string]any{"type": "string"},
			},
		}),
		fn("askUserQuestion", "向用户展示交互式单选/多选卡片确认需求。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"q":       map[string]any{"type": "string"},
							"type":    map[string]any{"type": "string", "enum": []string{"radio", "check"}},
							"options": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"q", "options"},
					},
				},
			},
			"required": []string{"questions"},
		}),
		fn("executeBatchPlan", "准备笔记变更计划。修改已有页面会生成审阅卡片，新建页面会生成预览卡片。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":        map[string]any{"type": "string"},
				"summary":      map[string]any{"type": "string"},
				"notebookRoot": map[string]any{"type": "string"},
				"operations": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":       map[string]any{"type": "string", "enum": []string{"search_replace", "edit", "create"}},
							"pageId":     map[string]any{"type": "string"},
							"oldString":  map[string]any{"type": "string"},
							"newString":  map[string]any{"type": "string"},
							"replaceAll": map[string]any{"type": "boolean"},
							"markdown":   map[string]any{"type": "string"},
							"title":      map[string]any{"type": "string"},
						},
						"required": []string{"type"},
					},
				},
			},
			"required": []string{"operations"},
		}),
	}
}

func (c *Controller) startBuiltinTurn(convID uint64, step int) {
	c.mu.Lock()
	cfg := normalizeProviderConfig(c.providerCfg)
	thinking := c.backendThinkingLocked(model.BackendBuiltin)
	root := c.activeRootLocked()
	currentPage := c.activePageLocked(root)
	currentMD := ""
	if currentPage != "" {
		currentMD = c.readPageContentLocked(root, currentPage)
	}
	msgs := c.buildBuiltinAPIMessagesLocked(root, currentPage, currentMD, step)
	ctx, cancel := context.WithCancel(context.Background())
	c.streamSerial++
	serial := c.streamSerial
	c.streaming = true
	c.partialText = ""
	c.partialThink = ""
	c.partialSteps = nil
	c.streamCancel = cancel
	c.mu.Unlock()

	var tools []any
	if step < maxBuiltinSteps {
		tools = builtinToolDefinitions()
	}
	events := make(chan sseEvent, 32)
	go streamBuiltinCompletion(ctx, cfg, msgs, tools, thinking, events)

	go func() {
		for ev := range events {
			c.mu.Lock()
			if c.streamSerial != serial || c.currentID != convID {
				c.mu.Unlock()
				return
			}
			if ev.thinking != "" {
				c.partialThink += ev.thinking
			}
			if ev.delta != "" {
				c.partialText += ev.delta
			}
			if ev.err != "" {
				c.streaming = false
				c.streamCancel = nil
				c.clearStreamRedrawLocked()
				c.turnError = ev.err
				c.turnErrorSettings = ev.settings
				c.partialText = ""
				c.partialThink = ""
				c.partialSteps = nil
				c.recordTurnOutcomeLocked("failed")
				c.restoreFailedDraftLocked()
				c.mu.Unlock()
				c.requestRedraw()
				return
			}
			if ev.done {
				c.streaming = false
				c.streamCancel = nil
				c.clearStreamRedrawLocked()
				if len(ev.toolCalls) > 0 && step >= maxBuiltinSteps {
					c.turnError = theme.Tr("已达到工具调用上限，请缩小范围后继续。", "Tool limit reached. Narrow the scope and continue.")
					c.partialText = ""
					c.partialThink = ""
					c.partialSteps = nil
					c.recordTurnOutcomeLocked("failed")
					c.restoreFailedDraftLocked()
					c.mu.Unlock()
					c.requestRedraw()
					return
				}
				text := c.partialText
				think := c.partialThink
				c.partialText = ""
				c.partialThink = ""
				c.partialSteps = nil
				if len(ev.toolCalls) == 0 {
					var proc []model.ProcessStep
					if strings.TrimSpace(think) != "" {
						proc = append(proc, model.ProcessStep{
							ID:     fmt.Sprintf("think-%d-%d", convID, step),
							Kind:   model.ToolKindThink,
							Title:  theme.Tr("思考过程", "Thinking"),
							Detail: strings.TrimSpace(think),
							Done:   true,
						})
					}
					if strings.TrimSpace(text) != "" || len(proc) > 0 {
						c.pushMessageLocked(model.ChatMessage{
							Role:    "assistant",
							Content: text,
							Process: proc,
						})
					}
					c.recordTurnOutcomeLocked("completed")
					c.sentDraft = nil
					c.mu.Unlock()
					c.requestRedraw()
					return
				}
				waitingQuestion := c.handleBuiltinToolCallsLocked(convID, step, text, think, ev.toolCalls)
				c.mu.Unlock()
				c.requestRedraw()
				if !waitingQuestion && step < maxBuiltinSteps {
					c.startBuiltinTurn(convID, step+1)
				}
				return
			}
			c.mu.Unlock()
			c.requestStreamRedraw()
		}
	}()
}

func (c *Controller) buildBuiltinAPIMessagesLocked(root, currentPage, currentMD string, step int) []map[string]any {
	agentsMD := assets.ReadSkill("AGENTS.md")
	markdownMD := assets.ReadSkill("markdown.md")
	notebookName := "（无）"
	if root != "" {
		notebookName = filepath.Base(root)
	}
	pageLabel := "（无当前打开页面）"
	if currentPage != "" {
		pageLabel = fmt.Sprintf("[%s] %s（pageId: %s）", currentPage, currentPage, currentPage)
	}
	system := fmt.Sprintf("%s\n\n%s\n\n# 当前上下文\n\n- 当前笔记本：%s\n- 当前打开页面：%s\n\n%s",
		agentsMD, markdownMD, notebookName, pageLabel, currentMD)

	msgs := []map[string]any{
		{"role": "system", "content": system},
	}
	if conv := c.findConvLocked(c.currentID); conv != nil {
		for _, m := range conv.Messages {
			switch m.Role {
			case "activity", "public_summary", "turn_outcome":
				continue
			case "tool":
				msgs = append(msgs, map[string]any{
					"role":         "tool",
					"tool_call_id": m.ToolCallID,
					"content":      m.Content,
				})
			case "assistant":
				if len(m.ToolCalls) > 0 {
					var raw []map[string]any
					_ = json.Unmarshal(m.ToolCalls, &raw)
					msgs = append(msgs, map[string]any{
						"role":       "assistant",
						"content":    m.Content,
						"tool_calls": raw,
					})
				} else if (m.Question != nil || m.Approval != nil) && strings.TrimSpace(m.Content) == "" {
					continue
				} else {
					msgs = append(msgs, map[string]any{
						"role":    "assistant",
						"content": m.Content,
					})
				}
			default:
				msgs = append(msgs, map[string]any{
					"role":    m.Role,
					"content": m.Content + m.Context,
				})
			}
		}
	}
	if step == maxBuiltinSteps {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": "本轮工具调用已达上限。请根据已有工具结果回答，明确未完成或未覆盖的范围，不要声称全部完成。",
		})
	}
	return msgs
}

func (c *Controller) handleBuiltinToolCallsLocked(convID uint64, step int, text, think string, calls []streamToolCall) bool {
	rawCalls := make([]map[string]any, 0, len(calls))
	for _, tc := range calls {
		rawCalls = append(rawCalls, map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			},
		})
	}
	rawJSON, _ := json.Marshal(rawCalls)
	var assistantProc []model.ProcessStep
	if strings.TrimSpace(think) != "" {
		assistantProc = append(assistantProc, model.ProcessStep{
			ID:     fmt.Sprintf("think-%d-%d", convID, step),
			Kind:   model.ToolKindThink,
			Title:  theme.Tr("思考过程", "Thinking"),
			Detail: strings.TrimSpace(think),
			Done:   true,
		})
	}
	c.pushMessageLocked(model.ChatMessage{
		Role:      "assistant",
		Content:   text,
		ToolCalls: rawJSON,
		Process:   assistantProc,
	})

	waitingQuestion := false
	for _, tc := range calls {
		procStep := processStepForTool(tc.Name, tc.Arguments)
		if (tc.Name == "askUserQuestion" || tc.Name == "askQuestion") && !waitingQuestion {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Arguments), &args)
			card := parseQuestionCard(tc.ID, args)
			if card != nil && len(card.Questions) > 0 {
				c.pushMessageLocked(model.ChatMessage{
					Role:       "assistant",
					ToolCallID: tc.ID,
					Question:   card,
					Process:    []model.ProcessStep{procStep},
				})
				waitingQuestion = true
				continue
			}
		}
		resJSON, plan, sources := c.executeToolLocked(tc.Name, tc.Arguments)
		msg := model.ChatMessage{
			Role:       "tool",
			Content:    resJSON,
			ToolCallID: tc.ID,
			Plan:       plan,
			Sources:    sources,
			Process:    []model.ProcessStep{procStep},
		}
		c.pushMessageLocked(msg)
		if plan != nil {
			conv := c.findConvLocked(convID)
			if conv != nil && len(conv.Messages) > 0 {
				c.linkPlanLocked(convID, len(conv.Messages)-1)
			}
		}
	}
	c.persistChatsLocked()
	return waitingQuestion
}

func processStepForTool(name, rawArgs string) model.ProcessStep {
	var args map[string]any
	_ = json.Unmarshal([]byte(rawArgs), &args)
	switch name {
	case "loadSkill":
		sk, _ := args["skill"].(string)
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRun,
			Title:    theme.Tr("加载工作区技能", "Load skill"),
			Detail:   sk,
			ToolName: name,
			Done:     true,
		}
	case "listNotebooks":
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRun,
			Title:    theme.Tr("列出笔记本", "List notebooks"),
			ToolName: name,
			Done:     true,
		}
	case "listPages", "listNotebookFiles":
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRead,
			Title:    theme.Tr("扫描笔记本目录", "List notebook files"),
			ToolName: name,
			Done:     true,
		}
	case "searchNotes":
		q, _ := args["query"].(string)
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRun,
			Title:    theme.Tr("搜索笔记", "Search notes"),
			Detail:   q,
			ToolName: name,
			Done:     true,
		}
	case "readPage", "readNote":
		p, _ := args["pageId"].(string)
		if p == "" {
			p, _ = args["path"].(string)
		}
		p = strings.TrimSuffix(p, ".md")
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRead,
			Title:    theme.Tr("读取笔记", "Read note"),
			Detail:   p + ".md",
			ToolName: name,
			FileRef:  p,
			Done:     true,
		}
	case "askUserQuestion", "askQuestion":
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRun,
			Title:    theme.Tr("向用户确认", "Ask user"),
			ToolName: name,
			Done:     true,
		}
	case "executeBatchPlan":
		sum, _ := args["summary"].(string)
		if sum == "" {
			sum, _ = args["title"].(string)
		}
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindWrite,
			Title:    theme.Tr("生成变更计划", "Prepare batch plan"),
			Detail:   sum,
			ToolName: name,
			Done:     true,
		}
	default:
		return model.ProcessStep{
			ID:       fmt.Sprintf("tool-%d", time.Now().UnixNano()),
			Kind:     model.ToolKindRun,
			Title:    name,
			ToolName: name,
			Done:     true,
		}
	}
}

func (c *Controller) executeToolLocked(name, rawArgs string) (string, *model.BatchPlan, []model.SourceNote) {
	var args map[string]any
	_ = json.Unmarshal([]byte(rawArgs), &args)
	if args == nil {
		args = map[string]any{}
	}
	root, _ := args["notebookRoot"].(string)
	if root == "" {
		root = c.activeRootLocked()
	}

	switch name {
	case "loadSkill":
		skillName, _ := args["skill"].(string)
		if spec, ok := c.findSkillLocked(skillName); ok {
			return marshalJSON(map[string]any{
				"skill":        spec.ID,
				"supported":    true,
				"instructions": spec.Content,
			}), nil, nil
		}
		return marshalJSON(map[string]any{"skill": skillName, "supported": false, "error": "未知 Skill"}), nil, nil

	case "listNotebooks":
		var nbs []string
		if c.host != nil {
			nbs = c.host.Notebooks()
		}
		if len(nbs) == 0 && root != "" {
			nbs = []string{root}
		}
		return marshalJSON(map[string]any{"notebooks": nbs}), nil, nil

	case "listPages", "listNotebookFiles":
		pages := c.listPagesForRootLocked(root)
		arr := make([]map[string]any, 0, len(pages))
		for _, p := range pages {
			arr = append(arr, map[string]any{"pageId": p, "title": pathBaseSlash(p)})
		}
		return marshalJSON(arr), nil, nil

	case "searchNotes":
		q, _ := args["query"].(string)
		if c.host != nil {
			if hits, err := c.host.SearchWorkspace(q, root == ""); err == nil {
				results := make([]map[string]any, 0, len(hits))
				for i, h := range hits {
					if i >= 20 {
						break
					}
					pageID := strings.TrimSuffix(filepath.Base(h.Path), ".md")
					if root != "" {
						if rel, rerr := filepath.Rel(root, h.Path); rerr == nil && !strings.HasPrefix(rel, "..") {
							pageID = strings.TrimSuffix(filepath.ToSlash(rel), ".md")
						}
					}
					results = append(results, map[string]any{
						"pageId":  pageID,
						"title":   h.Title,
						"snippet": h.Snippet,
					})
				}
				return marshalJSON(map[string]any{"query": q, "results": results}), nil, nil
			}
		}
		lowQ := strings.ToLower(q)
		pages := c.listPagesForRootLocked(root)
		var results []map[string]any
		for _, p := range pages {
			body := c.readPageContentLocked(root, p)
			if strings.Contains(strings.ToLower(p), lowQ) || strings.Contains(strings.ToLower(body), lowQ) {
				results = append(results, map[string]any{
					"pageId":  p,
					"title":   pathBaseSlash(p),
					"snippet": firstTitleLine(body),
				})
				if len(results) >= 20 {
					break
				}
			}
		}
		return marshalJSON(map[string]any{"query": q, "results": results}), nil, nil

	case "readPage", "readNote":
		page, _ := args["pageId"].(string)
		if page == "" {
			page, _ = args["path"].(string)
		}
		if page == "" {
			page = c.activePageLocked(root)
		} else if !filepath.IsAbs(page) {
			page = strings.TrimSuffix(page, ".md")
		}
		md := c.readPageContentLocked(root, page)
		if md == "" {
			return marshalJSON(map[string]any{"error": "找不到页面：" + page}), nil, nil
		}
		absPath := page
		if !filepath.IsAbs(absPath) && root != "" {
			absPath = filepath.Join(root, filepath.FromSlash(page)+".md")
		}
		src := []model.SourceNote{{Title: pathBaseSlash(page), Path: absPath}}
		return marshalJSON(map[string]any{"pageId": page, "title": pathBaseSlash(page), "markdown": md}), nil, src

	case "executeBatchPlan":
		return c.buildBatchPlanLocked(args, root)

	default:
		return marshalJSON(map[string]any{"error": "未知工具：" + name}), nil, nil
	}
}

func (c *Controller) listPagesForRootLocked(root string) []string {
	if len(c.pickerPages) > 0 && (root == c.pickerRoot || c.pickerInjected) {
		return append([]string(nil), c.pickerPages...)
	}
	if c.host != nil && root != "" {
		if entries, err := c.host.ListNotebookFiles(root); err == nil {
			var out []string
			var walk func(list []model.FileEntry)
			walk = func(list []model.FileEntry) {
				for _, ent := range list {
					if ent.IsDir {
						walk(ent.Children)
						continue
					}
					if ent.IsMarkdown || strings.HasSuffix(strings.ToLower(ent.RawName), ".md") {
						if rel, err := filepath.Rel(root, ent.Path); err == nil {
							out = append(out, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
						}
					}
				}
			}
			walk(entries)
			sort.Strings(out)
			return out
		}
	}
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && p != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			if rel, err := filepath.Rel(root, p); err == nil {
				out = append(out, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (c *Controller) startMockTurn(convID uint64, prompt string) {
	c.mu.Lock()
	c.streamSerial++
	serial := c.streamSerial
	c.streaming = true
	c.partialText = ""
	root := c.activeRootLocked()
	currentPage := c.activePageLocked(root)
	if currentPage == "" {
		currentPage = "mock-note"
	}
	c.mu.Unlock()

	low := strings.ToLower(prompt)
	go func() {
		time.Sleep(15 * time.Millisecond)
		c.mu.Lock()
		if c.streamSerial != serial || c.currentID != convID {
			c.mu.Unlock()
			return
		}

		steps := mockSevenSteps(currentPage)
		sources := mockSources(root, currentPage)

		switch {
		case strings.Contains(low, "approval") || strings.Contains(low, "审批") || strings.Contains(low, "权限"):
			c.pushMessageLocked(model.ChatMessage{
				Role:    "assistant",
				Process: steps,
				Sources: sources,
				Approval: &model.ApprovalCard{
					RequestID: fmt.Sprintf("mock-approval-%d", time.Now().UnixNano()),
					ToolName:  "readPage",
					Command:   "read " + currentPage + ".md",
					Reason:    "读取工作区笔记以完成检查",
				},
			})
			c.streaming = false
			c.sentDraft = nil
			c.mu.Unlock()
			c.requestRedraw()
			return

		case strings.Contains(low, "question") || strings.Contains(prompt, "确认") || strings.Contains(prompt, "选择题"):
			c.pushMessageLocked(model.ChatMessage{
				Role:    "assistant",
				Process: steps,
				Sources: sources,
				Question: &model.QuestionCard{
					RequestID: fmt.Sprintf("mock-q-%d", time.Now().UnixNano()),
					Questions: []model.QuestionItem{
						{
							ID:      "q-0",
							Prompt:  "是否将相关笔记汇总并统一更新格式？",
							Options: []model.QuestionOption{{Label: "是，立即汇总"}, {Label: "仅生成草稿"}},
						},
						{
							ID:          "q-1",
							Prompt:      "选择需要包含的章节：",
							MultiSelect: true,
							Options:     []model.QuestionOption{{Label: "背景说明"}, {Label: "操作步骤"}, {Label: "注意事项"}},
						},
					},
				},
			})
			c.streaming = false
			c.sentDraft = nil
			c.mu.Unlock()
			c.requestRedraw()
			return

		case strings.Contains(low, "createnote") || strings.Contains(prompt, "新建") || strings.Contains(prompt, "创建笔记"):
			page := "AI 新建预览"
			args := map[string]any{
				"title":   "新建笔记",
				"summary": "在当前笔记本新建预览笔记（接受后写入磁盘）",
				"operations": []any{
					map[string]any{
						"type":     "create",
						"title":    page,
						"markdown": "# 新建预览\n\n| 项目 | 内容 |\n| --- | --- |\n| 保留 | 已选择 |\n| 排除 | 不写入 |\n",
					},
				},
			}
			resJSON, plan, _ := c.buildBatchPlanLocked(args, root)
			c.pushMessageLocked(model.ChatMessage{
				Role:    "tool",
				Content: resJSON,
				Process: steps,
				Plan:    plan,
				Sources: sources,
			})
			if plan != nil {
				conv := c.findConvLocked(convID)
				if conv != nil && len(conv.Messages) > 0 {
					c.linkPlanLocked(convID, len(conv.Messages)-1)
				}
			}
			c.recordTurnOutcomeLocked("completed")
			c.streaming = false
			c.sentDraft = nil
			c.mu.Unlock()
			c.requestRedraw()
			return

		case strings.Contains(low, "plan") || strings.Contains(prompt, "计划") || strings.Contains(prompt, "修改"):
			oldMD := c.readPageContentLocked(root, currentPage)
			newMD := oldMD
			if oldMD == "" {
				oldMD = "# 原始笔记\n\n待整理内容"
				newMD = "# 原始笔记\n\n## 总览\n\n已整理内容"
			} else {
				newMD = strings.TrimRight(oldMD, "\n") + "\n\n## AI 整理补充\n\n- 已按计划补充结构化条目"
			}
			args := map[string]any{
				"title":   "整理笔记",
				"summary": "更新 " + currentPage + ".md",
				"operations": []any{
					map[string]any{
						"type":      "search_replace",
						"pageId":    currentPage,
						"oldString": oldMD,
						"newString": newMD,
					},
				},
			}
			resJSON, plan, _ := c.buildBatchPlanLocked(args, root)
			c.pushMessageLocked(model.ChatMessage{
				Role:    "tool",
				Content: resJSON,
				Process: steps,
				Plan:    plan,
				Sources: sources,
			})
			if plan != nil {
				conv := c.findConvLocked(convID)
				if conv != nil && len(conv.Messages) > 0 {
					c.linkPlanLocked(convID, len(conv.Messages)-1)
				}
			}
			c.recordTurnOutcomeLocked("completed")
			c.streaming = false
			c.sentDraft = nil
			c.mu.Unlock()
			c.requestRedraw()
			return
		}

		// Default mock streaming reply: process card (7 steps) + markdown + fenced code block + sources.
		chunks := []string{
			"已完成工作区检索与分析，示例实现如下：\n\n",
			"```go\npackage main\n\nfunc main() {\n\tprintln(\"hello block\")\n}\n```\n\n",
			"以上结果已关联参考笔记，可点击下方来源卡片直接打开。",
		}
		c.mu.Unlock()

		for _, ch := range chunks {
			time.Sleep(15 * time.Millisecond)
			c.mu.Lock()
			if c.streamSerial != serial || c.currentID != convID {
				c.mu.Unlock()
				return
			}
			c.partialText += ch
			c.mu.Unlock()
			c.requestRedraw()
		}

		c.mu.Lock()
		if c.streamSerial != serial || c.currentID != convID {
			c.mu.Unlock()
			return
		}
		reply := c.partialText
		c.partialText = ""
		c.streaming = false
		c.pushMessageLocked(model.ChatMessage{
			Role:    "assistant",
			Content: reply,
			Process: steps,
			Sources: sources,
		})
		c.recordTurnOutcomeLocked("completed")
		c.sentDraft = nil
		c.mu.Unlock()
		c.requestRedraw()
	}()
}

func mockSevenSteps(currentPage string) []model.ProcessStep {
	return []model.ProcessStep{
		{ID: "step-0", Kind: model.ToolKindRun, Title: "加载工作区技能", Detail: "searchNotes", ToolName: "loadSkill", Done: true},
		{ID: "step-1", Kind: model.ToolKindRun, Title: "加载工作区目录", Detail: "1 个工作区", ToolName: "listNotebooks", Done: true},
		{ID: "step-2", Kind: model.ToolKindRead, Title: "扫描笔记本目录 ×5", Detail: "6 篇笔记", ToolName: "listPages", Done: true},
		{ID: "step-3", Kind: model.ToolKindRead, Title: "读取笔记", Detail: currentPage + ".md", ToolName: "readPage", FileRef: currentPage, Done: true},
		{ID: "step-4", Kind: model.ToolKindRun, Title: "搜索相关笔记", Detail: "关键词命中 2 篇", ToolName: "searchNotes", Done: true},
		{ID: "step-5", Kind: model.ToolKindRead, Title: "读取关联笔记", Detail: "Claude.md", ToolName: "readPage", FileRef: "Claude", Done: true},
		{ID: "step-6", Kind: model.ToolKindThink, Title: "汇总并生成回答", Detail: "已校验引用与代码格式", Done: true},
	}
}

func mockSources(root, currentPage string) []model.SourceNote {
	p1 := currentPage + ".md"
	p2 := "Claude.md"
	if root != "" {
		p1 = filepath.Join(root, currentPage+".md")
		p2 = filepath.Join(root, "Claude.md")
	}
	return []model.SourceNote{
		{Title: currentPage, Path: p1},
		{Title: "Claude", Path: p2},
	}
}
