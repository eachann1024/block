package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultExecTimeoutMS   = 30000
	maxExecTimeoutMS       = 120000
	defaultExecOutputBytes = 1024 * 1024
	maxExecOutputBytes     = 8 * 1024 * 1024
	maxExecArgs            = 256
)

var allowedExecEnvOverrides = map[string]bool{
	"LANG":                true,
	"LC_ALL":              true,
	"LC_CTYPE":            true,
	"LC_MESSAGES":         true,
	"LC_TIME":             true,
	"LC_COLLATE":          true,
	"TZ":                  true,
	"TERM":                true,
	"NO_COLOR":            true,
	"CLICOLOR":            true,
	"CLICOLOR_FORCE":      true,
	"GIT_AUTHOR_NAME":     true,
	"GIT_AUTHOR_EMAIL":    true,
	"GIT_AUTHOR_DATE":     true,
	"GIT_COMMITTER_NAME":  true,
	"GIT_COMMITTER_EMAIL": true,
	"GIT_COMMITTER_DATE":  true,
	"GIT_TERMINAL_PROMPT": true,
}

const maxExecGitConfigCount = 16

func isAllowedGitConfigKey(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "http.extraheader", "user.name", "user.email", "core.quotepath", "advice.detachedhead":
		return true
	}
	return strings.HasPrefix(lower, "http.") && strings.HasSuffix(lower, ".extraheader") && len(lower) > len("http..extraheader")
}

type cappedOutputBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedOutputBuffer) Write(p []byte) (int, error) {
	if c.limit > 0 && c.buf.Len() < c.limit {
		avail := c.limit - c.buf.Len()
		if len(p) <= avail {
			_, _ = c.buf.Write(p)
		} else {
			_, _ = c.buf.Write(p[:avail])
			c.truncated = true
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (rt *Runtime) dispatchExecHostMethod(params json.RawMessage) (any, *rpcError) {
	if !rt.manifest.HasPermission(PermSystemExec) {
		return nil, missingPerm(PermSystemExec)
	}
	var p struct {
		Program        string            `json:"program"`
		Args           []string          `json:"args"`
		Cwd            string            `json:"cwd"`
		Env            map[string]string `json:"env"`
		Stdin          *string           `json:"stdin"`
		TimeoutMS      int               `json:"timeout_ms"`
		MaxOutputBytes int               `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErr(-32602, "invalid host.exec.run params: "+err.Error())
	}

	prog := strings.TrimSpace(p.Program)
	if prog == "" || validateExecAllowlistProgram(prog) != nil {
		return nil, rpcErr(-32001, "program is invalid or forbidden")
	}
	if !slices.Contains(rt.manifest.ExecAllowlist, prog) {
		return nil, rpcErr(-32001, "program is not in manifest.exec_allowlist")
	}
	if len(p.Args) > maxExecArgs {
		return nil, rpcErr(-32602, "too many command arguments")
	}
	for _, a := range p.Args {
		if strings.ContainsRune(a, 0) {
			return nil, rpcErr(-32602, "arguments must not contain NUL bytes")
		}
	}

	var cwd string
	if strings.TrimSpace(p.Cwd) != "" {
		target, rerr := rt.resolveFSScope(p.Cwd, false)
		if rerr != nil {
			return nil, rerr
		}
		if !target.isDirRoot {
			return nil, rpcErr(-32602, "cwd must be within an authorized directory")
		}
		info, err := os.Stat(target.canonPath)
		if err != nil || !info.IsDir() {
			return nil, rpcErr(-32602, "cwd must be an existing directory")
		}
		cwd = target.canonPath
	} else {
		canon, err := resolveCanonicalPath(rt.cfg.DataDir)
		if err != nil {
			return nil, rpcErr(-32004, "plugin data directory unavailable for cwd")
		}
		cwd = canon
	}

	if p.TimeoutMS < 0 {
		return nil, rpcErr(-32602, "timeout_ms must be non-negative")
	}
	timeoutMS := p.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = defaultExecTimeoutMS
	}
	if timeoutMS > maxExecTimeoutMS {
		timeoutMS = maxExecTimeoutMS
	}

	if p.MaxOutputBytes < 0 {
		return nil, rpcErr(-32602, "max_output_bytes must be non-negative")
	}
	maxOut := p.MaxOutputBytes
	if maxOut == 0 {
		maxOut = defaultExecOutputBytes
	}
	if maxOut > maxExecOutputBytes {
		maxOut = maxExecOutputBytes
	}

	sanitizedEnv, envErr := buildSanitizedExecEnv(p.Env)
	if envErr != nil {
		return nil, envErr
	}

	resolvedBin, err := exec.LookPath(prog)
	if err != nil {
		return nil, rpcErr(-32005, err.Error())
	}

	ctx, cancel := rt.hostCallContext(time.Duration(timeoutMS) * time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolvedBin, p.Args...)
	cmd.Dir = cwd
	cmd.Env = sanitizedEnv
	cmd.WaitDelay = 200 * time.Millisecond
	configurePluginProcess(cmd)

	if p.Stdin != nil {
		cmd.Stdin = strings.NewReader(*p.Stdin)
	}
	stdoutBuf := &cappedOutputBuffer{limit: maxOut}
	stderrBuf := &cappedOutputBuffer{limit: maxOut}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	runErr := cmd.Run()
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if timedOut {
			exitCode = -1
		} else {
			return nil, rpcErr(-32005, runErr.Error())
		}
	}
	if timedOut && exitCode == 0 {
		exitCode = -1
	}

	return map[string]any{
		"exit_code":        exitCode,
		"stdout":           stdoutBuf.buf.String(),
		"stderr":           stderrBuf.buf.String(),
		"stdout_truncated": stdoutBuf.truncated,
		"stderr_truncated": stderrBuf.truncated,
		"timed_out":        timedOut,
	}, nil
}

func buildSanitizedExecEnv(overrides map[string]string) ([]string, *rpcError) {
	envMap := make(map[string]string, 8)
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL"} {
		if v := os.Getenv(k); v != "" {
			envMap[k] = v
		}
	}

	hasGitConfig := false
	for k := range overrides {
		if k == "GIT_CONFIG_COUNT" || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			hasGitConfig = true
			break
		}
	}
	if hasGitConfig {
		countRaw, ok := overrides["GIT_CONFIG_COUNT"]
		if !ok {
			return nil, rpcErr(-32602, "GIT_CONFIG_COUNT is required when passing GIT_CONFIG_* variables")
		}
		if countRaw == "" || strings.TrimSpace(countRaw) != countRaw {
			return nil, rpcErr(-32602, "GIT_CONFIG_COUNT must be an integer between 0 and 16")
		}
		for i := 0; i < len(countRaw); i++ {
			if countRaw[i] < '0' || countRaw[i] > '9' {
				return nil, rpcErr(-32602, "GIT_CONFIG_COUNT must be an integer between 0 and 16")
			}
		}
		count, err := strconv.Atoi(countRaw)
		if err != nil || count < 0 || count > maxExecGitConfigCount {
			return nil, rpcErr(-32602, "GIT_CONFIG_COUNT must be an integer between 0 and 16")
		}
		for k := range overrides {
			var suffix string
			switch {
			case strings.HasPrefix(k, "GIT_CONFIG_KEY_"):
				suffix = strings.TrimPrefix(k, "GIT_CONFIG_KEY_")
			case strings.HasPrefix(k, "GIT_CONFIG_VALUE_"):
				suffix = strings.TrimPrefix(k, "GIT_CONFIG_VALUE_")
			default:
				continue
			}
			idx, err := strconv.Atoi(suffix)
			if err != nil || idx < 0 || idx >= count || strconv.Itoa(idx) != suffix {
				return nil, rpcErr(-32602, "invalid or out-of-range git config variable: "+k)
			}
		}
		envMap["GIT_CONFIG_COUNT"] = strconv.Itoa(count)
		for i := 0; i < count; i++ {
			kName := "GIT_CONFIG_KEY_" + strconv.Itoa(i)
			vName := "GIT_CONFIG_VALUE_" + strconv.Itoa(i)
			cfgKey, hasKey := overrides[kName]
			cfgVal, hasVal := overrides[vName]
			if !hasKey || !hasVal {
				return nil, rpcErr(-32602, "missing matching git config key/value for index "+strconv.Itoa(i))
			}
			if strings.ContainsRune(cfgKey, 0) || strings.ContainsRune(cfgVal, 0) {
				return nil, rpcErr(-32602, "git config entries must not contain NUL bytes")
			}
			if !isAllowedGitConfigKey(cfgKey) {
				return nil, rpcErr(-32001, "forbidden git config key: "+cfgKey)
			}
			if strings.HasSuffix(strings.ToLower(cfgKey), ".extraheader") && strings.ContainsAny(cfgVal, "\r\n") {
				return nil, rpcErr(-32602, "http extraheader value must not contain newlines")
			}
			envMap[kName] = cfgKey
			envMap[vName] = cfgVal
		}
	}

	for k, v := range overrides {
		if !allowedExecEnvOverrides[k] {
			continue
		}
		if strings.ContainsRune(v, 0) {
			continue
		}
		envMap[k] = v
	}
	envMap["GIT_TERMINAL_PROMPT"] = "0"

	out := make([]string, 0, len(envMap))
	for k, v := range envMap {
		out = append(out, k+"="+v)
	}
	return out, nil
}
