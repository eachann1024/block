package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	maxAIHostPromptBytes = 128 * 1024
	maxAIHostTokens      = 16384
	aiHostCallTimeout    = 120 * time.Second
)

// AIStatus describes the availability of the host AI subsystem.
type AIStatus struct {
	Enabled   bool `json:"enabled"`
	Ready     bool `json:"ready"`
	Streaming bool `json:"streaming"`
}

// AICompleteRequest describes a single-turn AI completion request from a plugin.
type AICompleteRequest struct {
	PluginID     string `json:"plugin_id,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	UserPrompt   string `json:"user_prompt"`
	MaxTokens    int    `json:"max_tokens,omitempty"`
}

// AICompleteResult holds the generated text and optional model identifier.
type AICompleteResult struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
}

// AIOpenPromptRequest asks the host to open the AI panel prefilled with a prompt.
type AIOpenPromptRequest struct {
	PluginID string `json:"plugin_id,omitempty"`
	Prompt   string `json:"prompt"`
	NotePath string `json:"note_path,omitempty"`
}

// AIHost is implemented by the host application to serve host.ai.* plugin RPCs.
type AIHost interface {
	Status() AIStatus
	Complete(ctx context.Context, req AICompleteRequest) (AICompleteResult, error)
	OpenWithPrompt(req AIOpenPromptRequest) error
}

func (rt *Runtime) dispatchAIHostMethod(method string, params json.RawMessage) (any, *rpcError) {
	if !rt.manifest.HasPermission(PermAIComplete) {
		return nil, missingPerm(PermAIComplete)
	}
	decode := func(v any) *rpcError {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return rpcErr(-32602, "invalid params: "+err.Error())
		}
		return nil
	}

	switch method {
	case "host.ai.status":
		if rt.cfg.AI == nil {
			return AIStatus{Enabled: false, Ready: false, Streaming: false}, nil
		}
		return rt.cfg.AI.Status(), nil

	case "host.ai.complete":
		var p struct {
			SystemPrompt string `json:"system_prompt"`
			UserPrompt   string `json:"user_prompt"`
			MaxTokens    int    `json:"max_tokens"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		sys := strings.TrimSpace(p.SystemPrompt)
		usr := strings.TrimSpace(p.UserPrompt)
		if usr == "" {
			return nil, rpcErr(-32602, "host.ai.complete requires non-empty user_prompt")
		}
		if len(sys)+len(usr) > maxAIHostPromptBytes {
			return nil, rpcErr(-32602, "host.ai.complete prompt exceeds size limit")
		}
		if p.MaxTokens < 0 || p.MaxTokens > maxAIHostTokens {
			return nil, rpcErr(-32602, "host.ai.complete max_tokens out of range")
		}
		if rt.cfg.AI == nil {
			return nil, rpcErr(-32005, "host AI service unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), aiHostCallTimeout)
		done := make(chan struct{})
		if rt.exited != nil {
			go func() {
				select {
				case <-rt.exited:
					cancel()
				case <-done:
				}
			}()
		}
		res, err := rt.cfg.AI.Complete(ctx, AICompleteRequest{
			PluginID:     rt.manifest.ID,
			SystemPrompt: sys,
			UserPrompt:   usr,
			MaxTokens:    p.MaxTokens,
		})
		close(done)
		cancel()
		if err != nil {
			return nil, rpcErr(-32005, err.Error())
		}
		return res, nil

	case "host.ai.open_with_prompt":
		var p struct {
			Prompt   string `json:"prompt"`
			NotePath string `json:"note_path"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		prompt := strings.TrimSpace(p.Prompt)
		if prompt == "" {
			return nil, rpcErr(-32602, "host.ai.open_with_prompt requires non-empty prompt")
		}
		if len(prompt) > maxAIHostPromptBytes {
			return nil, rpcErr(-32602, "host.ai.open_with_prompt prompt exceeds size limit")
		}
		notePath := strings.TrimSpace(p.NotePath)
		if notePath != "" {
			clean, ok := normalizeBadgeRelPath(notePath)
			if !ok {
				return nil, rpcErr(-32602, "invalid note_path: "+p.NotePath)
			}
			notePath = clean
		}
		if rt.cfg.AI == nil {
			return nil, rpcErr(-32005, "host AI service unavailable")
		}
		if err := rt.cfg.AI.OpenWithPrompt(AIOpenPromptRequest{
			PluginID: rt.manifest.ID,
			Prompt:   prompt,
			NotePath: notePath,
		}); err != nil {
			return nil, rpcErr(-32005, err.Error())
		}
		return map[string]any{"ok": true}, nil
	}

	return nil, rpcErr(-32601, "unknown host method: "+method)
}
