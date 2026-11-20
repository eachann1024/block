package systemintegration

import (
	"context"
	"errors"
	"runtime"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const notificationAuthorizationDenied = 1

func CheckNotificationPermission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	class := objc.GetClass("UNUserNotificationCenter")
	if class == 0 {
		return errors.ErrUnsupported
	}
	result := make(chan int, 1)
	callback := objc.NewBlock(func(_ objc.Block, settings objc.ID) {
		result <- objc.Send[int](settings, objc.RegisterName("authorizationStatus"))
	})
	runtime.LockOSThread()
	pool := objc.Send[objc.ID](objc.ID(objc.GetClass("NSAutoreleasePool")), objc.RegisterName("new"))
	center := objc.Send[objc.ID](objc.ID(class), objc.RegisterName("currentNotificationCenter"))
	objc.Send[objc.ID](center, objc.RegisterName("getNotificationSettingsWithCompletionHandler:"), callback)
	objc.Send[objc.ID](pool, objc.RegisterName("drain"))
	runtime.UnlockOSThread()
	callback.Release()
	select {
	case status := <-result:
		if status == notificationAuthorizationDenied {
			return mygo.ErrNotificationsDenied
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
