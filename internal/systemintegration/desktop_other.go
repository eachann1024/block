//go:build !linux && !windows

package systemintegration

import (
	"context"
	"errors"
	"runtime"

	"github.com/egoist/mygo"
)

func NotificationSupport(context.Context) (bool, func(string) string) {
	return runtime.GOOS == "darwin", func(text string) string { return text }
}

func CredentialsAvailable(context.Context) bool { return runtime.GOOS == "darwin" }
func BadgeAvailable(context.Context) bool       { return runtime.GOOS == "darwin" }
func WakeAvailable(context.Context) bool        { return runtime.GOOS == "darwin" }

func SetBadge(_ *mygo.Window, count int) error {
	if runtime.GOOS != "darwin" {
		return errors.ErrUnsupported
	}
	mygo.App.SetBadgeCount(count)
	return nil
}
