package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"block/internal/theme"
)

const cliIdleTimeout = 90 * time.Second

func withCLIIdleTimeout(parent context.Context, interval time.Duration) (context.Context, func(), func()) {
	ctx, cancel := context.WithCancelCause(parent)
	cause := errors.New(theme.Tr("本机 Agent 连续 90 秒无响应，已停止。", "The local agent stopped after 90 seconds without a response."))
	var mu sync.Mutex
	deadline := time.Now().Add(interval)
	var timer *time.Timer
	mu.Lock()
	timer = time.AfterFunc(interval, func() {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if remaining := time.Until(deadline); remaining > 0 {
			timer.Reset(remaining)
			return
		}
		cancel(cause)
	})
	mu.Unlock()
	activity := func() {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() == nil && time.Now().Before(deadline) {
			deadline = time.Now().Add(interval)
			timer.Reset(interval)
		}
	}
	stop := func() {
		mu.Lock()
		defer mu.Unlock()
		timer.Stop()
		cancel(nil)
	}
	return ctx, activity, stop
}

// Startup, heartbeats and other protocol bookkeeping don't count as a reply.
func cliActivitySplit(activity func()) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (int, []byte, error) {
		advance, line, err := bufio.ScanLines(data, atEOF)
		if len(line) != 0 && hasCLIActivity(line) {
			activity()
		}
		return advance, line, err
	}
}

func hasCLIActivity(line []byte) bool {
	var frame struct {
		Type, Method          string
		Result                json.RawMessage
		Message               struct{ Content []json.RawMessage }
		AssistantMessageEvent struct {
			Type, Delta string
		}
		Params struct {
			Delta  string
			Item   struct{ Type string }
			Event  struct{ Type string }
			Update struct {
				SessionUpdate, ToolCallID string
				Content                   json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(line, &frame); err != nil {
		return false
	}
	switch frame.Type {
	case "assistant":
		return len(frame.Message.Content) > 0
	case "result":
		var result string
		return json.Unmarshal(frame.Result, &result) == nil && result != ""
	case "message_update":
		event := frame.AssistantMessageEvent
		return (event.Type == "text_delta" || event.Type == "thinking_delta") && event.Delta != ""
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		return true
	}
	switch frame.Method {
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/reasoning/textDelta",
		"item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		return frame.Params.Delta != ""
	case "item/started", "item/completed":
		switch frame.Params.Item.Type {
		case "mcpToolCall", "commandExecution", "fileChange":
			return true
		}
	case "session/update":
		update := frame.Params.Update
		switch update.SessionUpdate {
		case "agent_message_chunk", "agent_thought_chunk":
			var content struct{ Text string }
			return json.Unmarshal(update.Content, &content) == nil && content.Text != ""
		case "tool_call", "tool_call_update":
			return update.ToolCallID != ""
		}
	case "session.event":
		return frame.Params.Event.Type == "assistant/message"
	}
	return false
}
