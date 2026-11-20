package systemintegration

import (
	"context"
	"html"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/godbus/dbus/v5"
)

func NotificationSupport(ctx context.Context) (bool, bool, func(string) string) {
	plain := func(text string) string { return text }
	bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return false, false, plain
	}
	defer bus.Close()
	const service = "org.freedesktop.Notifications"
	var caps []string
	err = bus.Object(service, "/org/freedesktop/Notifications").CallWithContext(ctx, service+".GetCapabilities", 0).Store(&caps)
	if err != nil {
		return false, false, plain
	}
	click := slices.Contains(caps, "actions")
	if slices.Contains(caps, "body-markup") {
		return true, click, html.EscapeString
	}
	return true, click, plain
}

func CredentialsAvailable(ctx context.Context) bool {
	bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer bus.Close()
	const service = "org.freedesktop.secrets"
	if serviceRunning(ctx, bus, service) {
		return true
	}
	var services []string
	err = bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&services)
	return err == nil && slices.Contains(services, service)
}

func WakeAvailable(ctx context.Context) bool {
	bus, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer bus.Close()
	return serviceRunning(ctx, bus, "org.freedesktop.login1")
}

func BadgeAvailable(ctx context.Context) bool {
	desktops := strings.Split(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), ":")
	for _, desktop := range []string{"ubuntu", "unity", "kde", "pantheon"} {
		if slices.Contains(desktops, desktop) {
			bus, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
			if err != nil {
				return false
			}
			_ = bus.Close()
			return true
		}
	}
	return false
}

func SetBadge(_ *mygo.Window, count int) error {
	mygo.App.SetBadgeCount(count)
	return nil
}

func serviceRunning(ctx context.Context, bus *dbus.Conn, name string) bool {
	var owned bool
	err := bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned)
	return err == nil && owned
}
