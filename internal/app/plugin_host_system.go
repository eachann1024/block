package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/plugins"
	"block/internal/systemintegration"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/zalando/go-keyring"
)

var _ plugins.SystemHost = (*pluginHostUI)(nil)

func (w *Workspace) AttachPluginSystem(reveal func()) {
	sup := w.Plugins()
	w.pluginUI.systemReveal = reveal
	mygo.Power.OnResume(func() { sup.EmitSystemEvent("system.wake", mygo.Network.Status().Online) })
	mygo.Network.OnChanged(func(status mygo.NetworkStatus) {
		sup.EmitSystemEvent("network.changed", status.Online)
	})
	go watchPluginNetworkInterfaces(w.pluginUI.closing, sup)
	if runtime.GOOS == "darwin" {
		mygo.App.OnDidBecomeActive(func() { sup.EmitSystemEvent("app.focus", false) })
		return
	}
	active := false
	var blurTimer *time.Timer
	updateFocus := func() {
		focused := false
		for _, win := range mygo.Windows() {
			if win.IsFocused() {
				focused = true
				break
			}
		}
		if focused && !active {
			sup.EmitSystemEvent("app.focus", false)
		}
		active = focused
	}
	watchWindow := func(win *mygo.Window) {
		win.OnFocus(updateFocus)
		lostFocus := func() {
			if blurTimer != nil {
				blurTimer.Stop()
			}
			blurTimer = time.AfterFunc(75*time.Millisecond, func() {
				select {
				case <-w.pluginUI.closing:
				default:
					mygo.RunOnMain(updateFocus)
				}
			})
		}
		win.OnBlur(lostFocus)
		win.OnClosed(lostFocus)
	}
	mygo.App.OnWindowCreated(watchWindow)
	for _, win := range mygo.Windows() {
		watchWindow(win)
	}
	updateFocus()
}

func watchPluginNetworkInterfaces(closing <-chan struct{}, sup *plugins.Supervisor) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	previous, err := networkInterfaceSnapshot()
	known := err == nil
	for {
		select {
		case <-closing:
			return
		case <-ticker.C:
			current, err := networkInterfaceSnapshot()
			if err != nil {
				continue
			}
			if known && current != previous {
				sup.EmitSystemEvent("network.changed", mygo.Network.Status().Online)
			}
			previous, known = current, true
		}
	}
}

func networkInterfaceSnapshot() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var addresses []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		assigned, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range assigned {
			addresses = append(addresses, iface.Name+"\x00"+addr.String())
		}
	}
	sort.Strings(addresses)
	return strings.Join(addresses, "\n"), nil
}

func (h *pluginHostUI) Capabilities() map[string]bool {
	ready := mygo.App.IsReady()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	notify, _ := systemintegration.NotificationSupport(ctx)
	return map[string]bool{
		"notify":   ready && notify && mygo.NotificationsSupported(),
		"secrets":  systemintegration.CredentialsAvailable(ctx),
		"open_url": ready, "reveal": ready, "open_dialog": ready, "save_dialog": ready,
		"clipboard_read": ready, "clipboard_write": ready, "trash": ready,
		"badge": ready && systemintegration.BadgeAvailable(ctx),
		"wake":  ready && systemintegration.WakeAvailable(ctx), "network": ready, "focus": ready,
	}
}

func (h *pluginHostUI) Notify(ctx context.Context, pluginID, title, body, tone, root, relPath string, allowed func() bool) error {
	if !mygo.App.IsReady() {
		return plugins.ErrSystemUnavailable
	}
	if !allowed() {
		return plugins.ErrSystemCanceled
	}
	if win := h.w.Window(); win != nil && win.IsFocused() {
		h.Toast(pluginID, title+" · "+body, tone, nil)
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	supported, encode := systemintegration.NotificationSupport(probeCtx)
	cancel()
	if !supported || !mygo.NotificationsSupported() {
		return plugins.ErrSystemUnavailable
	}
	if !allowed() {
		return plugins.ErrSystemCanceled
	}
	note := mygo.NewNotification(mygo.NotificationOptions{
		ID: "block-plugin-" + pluginID, Title: title, Body: encode(body),
		Silent: tone == "info" || tone == "success",
	})
	note.OnClick(func() {
		installed, ok := h.w.Plugins().GetInstalled(pluginID)
		if !ok || !installed.Enabled || !installed.Manifest.HasPermission(plugins.PermSystemNotify) {
			return
		}
		if h.systemReveal != nil {
			h.systemReveal()
		}
		if root != "" && relPath != "" && installed.Manifest.HasPermission(plugins.PermNotesRead) &&
			h.w.OpenDeepLinkedFile(filepath.Join(root, filepath.FromSlash(relPath))) == nil {
			return
		}
		if installed.Manifest.HasSurface(plugins.SurfaceSettingsPage) {
			h.w.OpenSettings("plugin:" + pluginID)
		}
	})
	if err := note.ShowContext(ctx); errors.Is(err, mygo.ErrNotificationsDenied) {
		return plugins.ErrSystemUnavailable
	} else {
		return err
	}
}

func (h *pluginHostUI) desktopAction(allowed func() bool, run func() error) error {
	if !mygo.App.IsReady() {
		return plugins.ErrSystemUnavailable
	}
	var err error
	if waitErr := h.onMainWait(func() {
		win := h.w.Window()
		if win == nil || !win.IsFocused() || !allowed() {
			err = plugins.ErrSystemCanceled
			return
		}
		err = run()
	}); waitErr != nil {
		return plugins.ErrSystemCanceled
	}
	return err
}

func (h *pluginHostUI) OpenURL(rawURL string, allowed func() bool) error {
	return h.desktopAction(allowed, func() error { return mygo.Shell.OpenExternal(rawURL) })
}

func (h *pluginHostUI) Reveal(path string, allowed func() bool) error {
	return h.desktopAction(allowed, func() error {
		mygo.Shell.ShowItemInFolder(path)
		return nil
	})
}

func (h *pluginHostUI) Pick(save, directory, multiple bool, title, defaultPath string, extensions []string, allowed func() bool) ([]string, error) {
	if err := h.desktopAction(allowed, func() error { return nil }); err != nil {
		return nil, err
	}
	var filters []mygo.FileFilter
	if len(extensions) != 0 {
		filters = []mygo.FileFilter{{Name: theme.Tr("文件类型", "File types"), Extensions: extensions}}
	}
	win := h.w.Window()
	var paths []string
	var err error
	if save {
		var path string
		path, err = files.SaveDialog(win, mygo.SaveDialogOptions{Title: title, DefaultPath: defaultPath, Filters: filters, CreateDirectories: true})
		if path != "" {
			paths = []string{path}
		}
	} else {
		paths, err = files.OpenDialog(win, mygo.OpenDialogOptions{
			Title: title, DefaultPath: defaultPath, Filters: filters, Directory: directory, Multiple: multiple, CreateDirectories: directory,
		})
	}
	if !allowed() {
		return nil, plugins.ErrSystemCanceled
	}
	return paths, err
}

func (h *pluginHostUI) Clipboard(read bool, text string, allowed func() bool) (string, error) {
	err := h.desktopAction(allowed, func() error {
		if !read {
			return mygo.Clipboard.Write(transfer.TextData(text))
		}
		raw, err := mygo.Clipboard.ReadFormat(transfer.Text)
		if errors.Is(err, transfer.ErrFormat) {
			return nil
		}
		if err == nil {
			text = string(raw)
		}
		return err
	})
	return text, err
}

func (h *pluginHostUI) Trash(path string, allowed, validPath func() bool) error {
	var info os.FileInfo
	if err := h.desktopAction(func() bool { return allowed() && validPath() }, func() error {
		if err := h.w.flushDirtySessions(); err != nil {
			return err
		}
		if !allowed() || !validPath() {
			return plugins.ErrSystemCanceled
		}
		parent, err := os.OpenRoot(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer parent.Close()
		info, err = parent.Lstat(filepath.Base(path))
		if err != nil || !info.Mode().IsRegular() {
			return plugins.ErrSystemCanceled
		}
		return nil
	}); err != nil {
		return err
	}
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: h.w.Window(), Type: mygo.MessageWarning,
		Title:         theme.Tr("移到回收站", "Move to Trash"),
		Message:       theme.Tr("将这个文件移到系统回收站？", "Move this file to the system trash?"),
		Detail:        path,
		Buttons:       []string{theme.Tr("移到回收站", "Move to Trash"), theme.Tr("取消", "Cancel")},
		DefaultButton: 1, CancelButton: 1,
	})
	if err != nil {
		return err
	}
	if result.Button != 0 {
		return plugins.ErrSystemCanceled
	}
	var trashErr error
	if waitErr := h.onMainWait(func() {
		trashErr = h.w.trashWorkspaceItem(path, func() error {
			return systemintegration.TrashFile(path, info, allowed, validPath)
		})
	}); waitErr != nil {
		return plugins.ErrSystemCanceled
	}
	return trashErr
}

func (h *pluginHostUI) Badge(count int) error {
	if !mygo.App.IsReady() {
		return plugins.ErrSystemUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !systemintegration.BadgeAvailable(ctx) {
		return plugins.ErrSystemUnavailable
	}
	return systemintegration.SetBadge(h.w.Window(), count)
}

var credentialSlot = make(chan struct{}, 1)

func (h *pluginHostUI) Secret(method, account, value string) (string, bool, error) {
	select {
	case credentialSlot <- struct{}{}:
	default:
		return "", false, plugins.ErrSystemBusy
	}
	type response struct {
		value string
		err   error
	}
	done := make(chan response, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		const service = "Block plugin credentials"
		var res response
		res.value, res.err = systemintegration.Secret(ctx, method, service, account, value)
		<-credentialSlot
		done <- res
	}()
	select {
	case res := <-done:
		if errors.Is(res.err, keyring.ErrNotFound) && method != "host.secrets.set" {
			return "", false, nil
		}
		return res.value, res.err == nil, res.err
	case <-ctx.Done():
		return "", false, plugins.ErrSystemBusy
	case <-h.closing:
		return "", false, plugins.ErrSystemCanceled
	}
}
