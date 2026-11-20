package systemintegration

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/zalando/go-keyring"
)

const keychainBase64Prefix = "go-keyring-base64:"

func Secret(ctx context.Context, method, service, account, value string) (string, error) {
	const security = "/usr/bin/security"
	var cmd *exec.Cmd
	switch method {
	case "host.secrets.get":
		cmd = exec.CommandContext(ctx, security, "find-generic-password", "-s", service, "-wa", account)
	case "host.secrets.delete":
		cmd = exec.CommandContext(ctx, security, "delete-generic-password", "-s", service, "-a", account)
	case "host.secrets.set":
		encode := keychainBase64Prefix + base64.StdEncoding.EncodeToString([]byte(value))
		quote := func(arg string) string { return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'" }
		cmd = exec.CommandContext(ctx, security, "-i")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", quote(service), quote(account), encode))
	default:
		return "", errors.ErrUnsupported
	}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		if method != "host.secrets.set" && strings.Contains(string(output), "The specified item could not be found in the keychain.") {
			return "", keyring.ErrNotFound
		}
		return "", err
	}
	if method != "host.secrets.get" {
		return "", nil
	}
	stored := strings.TrimSpace(string(output))
	if encoded, ok := strings.CutPrefix(stored, keychainBase64Prefix); ok {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		return string(decoded), err
	}
	if encoded, ok := strings.CutPrefix(stored, "go-keyring-encoded:"); ok {
		decoded, err := hex.DecodeString(encoded)
		return string(decoded), err
	}
	return stored, nil
}
