package systemintegration

import (
	"context"
	"errors"

	"github.com/godbus/dbus/v5"
	"github.com/zalando/go-keyring"
	secrets "github.com/zalando/go-keyring/secret_service"
)

func Secret(ctx context.Context, method, service, account, value string) (string, error) {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return "", errors.ErrUnsupported
	}
	defer conn.Close()
	const name = "org.freedesktop.secrets"
	const iface = "org.freedesktop.Secret."
	const none dbus.ObjectPath = "/"
	api := conn.Object(name, "/org/freedesktop/secrets")
	attributes := map[string]string{"service": service, "username": account}
	var unlocked, locked []dbus.ObjectPath
	if err := api.CallWithContext(ctx, iface+"Service.SearchItems", 0, attributes).Store(&unlocked, &locked); err != nil {
		return "", err
	}
	if len(locked) > 0 || len(unlocked) > 1 {
		return "", errors.ErrUnsupported
	}
	var object dbus.BusObject
	if len(unlocked) > 0 {
		object = conn.Object(name, unlocked[0])
	} else if method != "host.secrets.set" {
		return "", keyring.ErrNotFound
	}
	if method == "host.secrets.delete" {
		var prompt dbus.ObjectPath
		if err := object.CallWithContext(ctx, iface+"Item.Delete", 0).Store(&prompt); err != nil {
			return "", err
		}
		if prompt != none {
			return "", errors.ErrUnsupported
		}
		return "", nil
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	if err := api.CallWithContext(ctx, iface+"Service.OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		return "", err
	}
	if method == "host.secrets.get" {
		var secret secrets.Secret
		if err := object.CallWithContext(ctx, iface+"Item.GetSecret", 0, session).Store(&secret); err != nil {
			return "", err
		}
		return string(secret.Value), nil
	}
	secret := secrets.NewSecret(session, value)
	if object != nil {
		return "", object.CallWithContext(ctx, iface+"Item.SetSecret", 0, secret).Err
	}
	var collection dbus.ObjectPath
	if err := api.CallWithContext(ctx, iface+"Service.ReadAlias", 0, "default").Store(&collection); err != nil {
		return "", err
	}
	if collection == none {
		return "", errors.ErrUnsupported
	}
	props := map[string]dbus.Variant{
		iface + "Item.Attributes": dbus.MakeVariant(attributes),
		iface + "Item.Label":      dbus.MakeVariant(service + " (" + account + ")"),
	}
	var item, prompt dbus.ObjectPath
	if err := conn.Object(name, collection).CallWithContext(ctx, iface+"Collection.CreateItem", 0, props, secret, true).Store(&item, &prompt); err != nil {
		return "", err
	}
	if prompt != none || item == none {
		return "", errors.ErrUnsupported
	}
	return "", nil
}
