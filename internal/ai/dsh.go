package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"block/internal/theme"
)

func dshVersion(ctx context.Context) (string, error) {
	out, err := cliCommand(ctx, "dsh", "--version").Output()
	return strings.TrimSpace(string(out)), err
}

func dshCredentialError() error {
	return errors.New(theme.Tr("请为 Block 的启动环境设置 DEEPSEEK_API_KEY。", "Set DEEPSEEK_API_KEY in Block's launch environment."))
}

func dshCLIArgs(settingsDir string, nativeTools bool) ([]string, error) {
	if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
		return nil, dshCredentialError()
	}

	var patch strings.Builder
	profile := "sdk-minimal"
	if nativeTools {
		profile = "sdk"
		patch.WriteString("- id: sandbox-policy\n  config:\n    mode: danger-full-access\n    workspaceRoot: !!js process.cwd()\n")
		patch.WriteString("- id: approval\n  config:\n    policy: never\n")
	} else {
		for _, id := range []string{
			"persistent-bash", "persistent-pwsh", "terminal-bash", "terminal-pwsh",
			"pty", "subprocess", "tool-fs", "tool-working-directory", "mcp-resources",
			"attachment-local", "image-offload", "sandbox", "sandbox-policy",
			"session-log-deepseek", "plugin-package-inventory-deepseek",
		} {
			fmt.Fprintf(&patch, "- id: %s\n  disabled: true\n", id)
		}
	}
	path := filepath.Join(settingsDir, "block-dsh.yaml")
	if err := os.WriteFile(path, []byte(patch.String()), 0o600); err != nil {
		return nil, err
	}
	return []string{"--profile", profile, "--patch", path}, nil
}

func (c *Controller) streamDsh(turn *assistantTurn, scanner *lineReader, stdin io.Writer, dir, prompt, modelName string) error {
	nativeTools := turn.nativeTools
	var session [16]byte
	if _, err := rand.Read(session[:]); err != nil {
		return err
	}
	sessionID := hex.EncodeToString(session[:])
	out := json.NewEncoder(stdin)
	send := func(id int, method string, params any) error {
		return out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	}
	reply := func(reqID any, result any, rpcErr map[string]any) error {
		frame := map[string]any{"jsonrpc": "2.0", "id": reqID}
		if rpcErr != nil {
			frame["error"] = rpcErr
		} else {
			frame["result"] = result
		}
		return out.Encode(frame)
	}

	toolNames := make(map[string]string)
	var completed bool
	read := func(id int) error {
		for scanner.Scan() && turn.ctx.Err() == nil {
			var frame struct {
				ID     any
				Method string
				Error  map[string]any
				Params struct {
					SessionID string
					Event     struct {
						Type string
						Data json.RawMessage
					}
				}
			}
			dec := json.NewDecoder(strings.NewReader(scanner.Text()))
			dec.UseNumber()
			if err := dec.Decode(&frame); err != nil {
				return err
			}
			if frame.Method != "" && frame.ID != nil {
				if !nativeTools {
					return errors.New(theme.Tr("dsh 发送了意外的权限请求。", "dsh sent an unexpected request."))
				}
				if frame.Method == "approval/request" {
					if err := reply(frame.ID, map[string]any{"outcome": "allowed-once"}, nil); err != nil {
						return err
					}
				} else if err := reply(frame.ID, nil, map[string]any{"code": -32601, "message": "Method not supported"}); err != nil {
					return err
				}
				continue
			}
			if frame.Method == "" && rpcIDMatches(frame.ID, strconv.Itoa(id)) {
				if frame.Error != nil {
					return fmt.Errorf("dsh: %s", rpcErrorMessage(frame.Error))
				}
				return nil
			}
			if frame.Method != "session.event" || frame.Params.SessionID != sessionID {
				continue
			}
			event := frame.Params.Event
			switch event.Type {
			case "request/header":
				if nativeTools {
					continue
				}
				var data struct {
					Header struct{ Tools []json.RawMessage }
				}
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return err
				}
				if len(data.Header.Tools) == 0 {
					continue
				}
				return errors.New(theme.Tr("dsh 本轮意外启用了本地工具。", "dsh enabled tools in a text-only turn."))
			case "tool/call":
				if !nativeTools {
					return errors.New(theme.Tr("dsh 本轮意外启用了本地工具。", "dsh enabled tools in a text-only turn."))
				}
				var data struct {
					CallID    string `json:"callId"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return err
				}
				if data.CallID != "" && data.Name != "" {
					toolNames[data.CallID] = data.Name
				}
				upd := map[string]any{
					"sessionUpdate": "tool_call",
					"toolCallId":    data.CallID,
					"name":          data.Name,
					"title":         data.Name,
					"status":        "in_progress",
				}
				if data.Arguments != "" {
					var rawInput any
					if json.Unmarshal([]byte(data.Arguments), &rawInput) == nil {
						upd["rawInput"] = rawInput
					}
				}
				c.handleCLIStreamFrame(turn.convID, turn.serial, map[string]any{
					"method": "session/update",
					"params": map[string]any{"update": upd},
				})
			case "tool/result":
				if !nativeTools {
					continue
				}
				var data struct {
					Message struct {
						ToolCallID string `json:"toolCallId"`
						IsError    bool   `json:"isError"`
					} `json:"message"`
				}
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return err
				}
				name := toolNames[data.Message.ToolCallID]
				status := "completed"
				if data.Message.IsError {
					status = "failed"
				}
				c.handleCLIStreamFrame(turn.convID, turn.serial, map[string]any{
					"method": "session/update",
					"params": map[string]any{"update": map[string]any{
						"sessionUpdate": "tool_call_update",
						"toolCallId":    data.Message.ToolCallID,
						"name":          name,
						"title":         name,
						"status":        status,
					}},
				})
			case "assistant/message":
				var data struct{ Message map[string]any }
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return err
				}
				// The existing renderer understands text and thinking blocks.
				if blocks, ok := data.Message["content"].([]any); ok {
					for _, raw := range blocks {
						if block, ok := raw.(map[string]any); ok && block["type"] == "reasoning" {
							block["type"], block["thinking"] = "thinking", block["text"]
						}
					}
				}
				c.handleCLIStreamFrame(turn.convID, turn.serial, map[string]any{"type": "assistant", "message": data.Message})
			case "turn/end":
				var data struct {
					Reason struct {
						Kind  string
						Error any
					}
				}
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return err
				}
				if data.Reason.Kind != "completed" {
					detail := rpcErrorMessage(data.Reason.Error)
					if detail == "" {
						detail = data.Reason.Kind
					}
					return fmt.Errorf(theme.Tr("dsh 本轮执行失败：%s", "dsh turn failed: %s"), detail)
				}
				completed = true
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return errors.New(theme.Tr("dsh 进程意外退出。", "The dsh process exited unexpectedly."))
	}

	if modelName == "" || modelName == "default" {
		modelName = "deepseek-flash"
	}
	config := map[string]any{"cwd": dir, "provider": "deepseek-official", "model": modelName}
	if nativeTools {
		config["approvalRequests"] = true
	}
	if turn.thinking != "" {
		config["reasoningEffort"] = turn.thinking
	}
	if err := send(1, "initialize", config); err != nil {
		return err
	}
	if err := read(1); err != nil {
		return err
	}
	if err := send(2, "session/prompt", map[string]any{
		"sessionId": sessionID, "contentBlocks": []map[string]string{{"type": "text", "text": prompt}},
	}); err != nil {
		return err
	}
	if err := read(2); err != nil {
		return err
	}
	if err := send(3, "session/wait", map[string]string{"sessionId": sessionID}); err != nil {
		return err
	}
	if err := read(3); err != nil {
		return err
	}
	if !completed {
		return errors.New(theme.Tr("dsh 未确认本轮完成。", "dsh did not confirm turn completion."))
	}
	if err := send(4, "shutdown", map[string]any{}); err != nil {
		return err
	}
	return read(4)
}
