//go:build !darwin

package systemintegration

import "context"

func CheckNotificationPermission(context.Context) error { return nil }
