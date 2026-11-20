//go:build !linux

package systemintegration

import (
	"context"
	"errors"

	"github.com/zalando/go-keyring"
)

func Secret(ctx context.Context, method, service, account, value string) (string, error) {
	if !CredentialsAvailable(ctx) {
		return "", errors.ErrUnsupported
	}
	switch method {
	case "host.secrets.get":
		return keyring.Get(service, account)
	case "host.secrets.set":
		return "", keyring.Set(service, account, value)
	case "host.secrets.delete":
		return "", keyring.Delete(service, account)
	default:
		return "", errors.ErrUnsupported
	}
}
