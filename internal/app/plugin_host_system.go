package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"block/internal/files"
	"block/internal/model"
	"block/internal/plugins"
	"block/internal/systemintegration"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/zalando/go-keyring"
)

var _ plugins.SystemHost = (*pluginHostUI)(nil)

var pluginNotificationID = regexp.MustCompile(`^block-plugin-click-[A-Z2-7]{26}$`)

var pluginNativeNotifications = struct {
	gate    chan struct{}
	notices []*mygo.Notification
}{gate: make(chan struct{}, 1)}

type pluginNotificationTarget struct {
	PluginID    string
	InstalledAt time.Time
	Root        string
	Relative    string
}

func (w *Workspace) AttachPluginSystem(reveal func()) {
	sup := w.Plugins()
	w.pluginUI.systemReveal = reveal
	go systemintegration.PendingTrashRecovery(files.AppDataDir(), func(original, recovery string) {
		files.LogWarnf("system trash recovery: original=%s recovery=%s", original, recovery)
		w.setNotice(theme.Tr("可从回收暂存目录恢复文件", "A file is available in Trash staging") + " · " + recovery + " → " + original)
	})
	mygo.Power.OnResume(func() { sup.EmitSystemEvent("system.wake", mygo.Network.Status().Online) })
	mygo.Network.OnChanged(func(status mygo.NetworkStatus) {
		sup.EmitSystemEvent("network.changed", status.Online)
	})
	go watchPluginNetworkInterfaces(w.pluginUI.closing, sup)
	onFocus := func() { sup.EmitSystemEvent("app.focus", false) }
	if win := w.Window(); win != nil {
		win.OnFocus(w.pluginUI.restoreDeferredToasts)
	}
	if runtime.GOOS == "darwin" {
		mygo.App.OnDidBecomeActive(onFocus)
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
			onFocus()
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
	notify, notifyClick, _ := systemintegration.NotificationSupport(ctx)
	notify = ready && notify && mygo.NotificationsSupported() && systemintegration.CheckNotificationPermission(ctx) == nil
	return map[string]bool{
		"notify": notify, "notify_click": notify && notifyClick,
		"secrets":  systemintegration.CredentialsAvailable(ctx),
		"open_url": ready, "reveal": ready, "open_dialog": ready, "save_dialog": ready,
		"clipboard_read": ready, "clipboard_write": ready, "trash": ready,
		"badge": ready && systemintegration.BadgeAvailable(ctx),
		"wake":  ready && systemintegration.WakeAvailable(ctx), "network": ready, "focus": ready,
	}
}

func (h *pluginHostUI) Notify(ctx context.Context, pluginID, title, body, tone, root, relPath, eventKey string, allowed func() bool) error {
	if !mygo.App.IsReady() {
		return plugins.ErrSystemUnavailable
	}
	if !allowed() {
		return plugins.ErrSystemCanceled
	}
	sup := h.w.Plugins()
	installed, ok := sup.GetInstalled(pluginID)
	if !ok || !allowed() {
		return plugins.ErrSystemCanceled
	}
	target := pluginNotificationTarget{pluginID, installed.InstalledAt, root, relPath}
	if win := h.w.Window(); win != nil && win.IsFocused() {
		configRoot := root
		if configRoot == "" {
			configRoot = sup.NotebookRoot()
		}
		native := func(ctx context.Context) error {
			return h.showNativePluginNotification(ctx, target, title, body, tone, eventKey, func() bool {
				current, ok := sup.GetInstalled(pluginID)
				return ok && current.Enabled && current.InstalledAt.Equal(target.InstalledAt) && current.Manifest.HasPermission(plugins.PermSystemNotify) &&
					(pluginID != "block-sync" || plugins.SyncNotificationsEnabled(current.DataDir, configRoot))
			})
		}
		if !h.toast(pluginID, title+" · "+body, tone, eventKey, nil, native) {
			return plugins.ErrSystemBusy
		}
		return nil
	}
	return h.showNativePluginNotification(ctx, target, title, body, tone, eventKey, allowed)
}

func (h *pluginHostUI) showNativePluginNotification(ctx context.Context, target pluginNotificationTarget, title, body, tone, eventKey string, allowed func() bool) error {
	select {
	case <-h.closing:
		return plugins.ErrSystemCanceled
	default:
	}
	if !mygo.App.IsReady() || !allowed() {
		return plugins.ErrSystemCanceled
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	supported, click, encode := systemintegration.NotificationSupport(probeCtx)
	cancel()
	if !supported || !mygo.NotificationsSupported() {
		return plugins.ErrSystemUnavailable
	}
	if err := systemintegration.CheckNotificationPermission(ctx); err != nil {
		if errors.Is(err, mygo.ErrNotificationsDenied) {
			return plugins.ErrSystemUnavailable
		}
		return err
	}
	select {
	case pluginNativeNotifications.gate <- struct{}{}:
		defer func() { <-pluginNativeNotifications.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if !allowed() {
		return plugins.ErrSystemCanceled
	}
	id := "block-plugin-" + target.PluginID + "-" + eventKey
	var record string
	if runtime.GOOS == "darwin" {
		id = "block-plugin-click-" + rand.Text()
		var err error
		record, err = persistPluginNotificationTarget(id, target)
		if err != nil {
			return err
		}
	}
	note := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: encode(body), Silent: tone == "info" || tone == "success"})
	if click && record == "" {
		note.OnClick(func() { h.openNotificationTarget(target) })
	}
	err := note.ShowContext(ctx)
	if err == nil {
		err = systemintegration.CheckNotificationPermission(ctx)
		if err != nil {
			note.Close()
		}
	}
	if err != nil && record != "" {
		_ = os.Remove(record)
	}
	if err == nil {
		retainPluginNotification(note)
	}
	if errors.Is(err, mygo.ErrNotificationsDenied) {
		return plugins.ErrSystemUnavailable
	}
	return err
}

func retainPluginNotification(note *mygo.Notification) {
	notices := pluginNativeNotifications.notices
	for index, previous := range notices {
		if previous.ID() == note.ID() {
			copy(notices[index:], notices[index+1:])
			notices = notices[:len(notices)-1]
			break
		}
	}
	if len(notices) >= 128 {
		notices[0].Close()
		notices[0] = nil
		notices = notices[1:]
	}
	pluginNativeNotifications.notices = append(notices, note)
}

func persistPluginNotificationTarget(id string, target pluginNotificationTarget) (string, error) {
	dir := filepath.Join(files.AppDataDir(), "plugin-notifications")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(dir)
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, entry := range entries {
		if pluginNotificationID.MatchString(entry.Name()) {
			if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	path := filepath.Join(dir, id)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if err := errors.Join(json.NewEncoder(file).Encode(target), file.Close()); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (w *Workspace) HandlePluginNotificationClick(id string) {
	if !pluginNotificationID.MatchString(id) {
		return
	}
	path := filepath.Join(files.AppDataDir(), "plugin-notifications", id)
	expected, err := os.Lstat(path)
	if err != nil || !expected.Mode().IsRegular() {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	info, statErr := file.Stat()
	if statErr != nil || !os.SameFile(expected, info) || info.ModTime().Before(time.Now().AddDate(0, 0, -7)) {
		_ = file.Close()
		return
	}
	var target pluginNotificationTarget
	err = json.NewDecoder(io.LimitReader(file, 32*1024)).Decode(&target)
	_ = file.Close()
	_ = os.Remove(path)
	if err == nil {
		w.pluginUI.openNotificationTarget(target)
	}
}

func (h *pluginHostUI) openNotificationTarget(target pluginNotificationTarget) {
	installed, ok := h.w.Plugins().GetInstalled(target.PluginID)
	if !ok || !installed.Enabled || !installed.InstalledAt.Equal(target.InstalledAt) || !installed.Manifest.HasPermission(plugins.PermSystemNotify) {
		return
	}
	if h.systemReveal != nil {
		h.systemReveal()
	}
	if target.Root != "" && installed.Manifest.HasPermission(plugins.PermNotesRead) {
		notebooks := h.w.Notebooks()
		if target.Relative != "" {
			if h.w.openNotificationNote(target.Root, target.Relative, notebooks) {
				return
			}
		} else if target.Root != h.w.ActiveNotebook() {
			for _, notebook := range notebooks {
				if candidate, err := filepath.Abs(notebook); err == nil && candidate == target.Root {
					_ = h.w.SetNotebook(candidate)
					break
				}
			}
		}
	}
	if installed.Manifest.HasSurface(plugins.SurfaceSettingsPage) {
		h.w.OpenSettings("plugin:" + target.PluginID)
	}
}

func (w *Workspace) openNotificationNote(notebook, relative string, notebooks []string) bool {
	base, err := filepath.EvalSymlinks(notebook)
	if err != nil {
		return false
	}
	expected, err := os.Lstat(base)
	if err != nil || !expected.IsDir() {
		return false
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, info) {
		return false
	}
	path, err := plugins.SystemNotificationNotePath(notebook, relative, notebooks)
	local := filepath.FromSlash(relative)
	if err != nil || path != filepath.Join(base, local) {
		return false
	}
	classified, noteInfo, err := files.ClassifyNotebookNote(root, local)
	if err != nil || classified.Kind != model.OpenKindMarkdown {
		return false
	}
	w.mu.RLock()
	loaded := w.sessions[path] != nil
	tabs := append([]string(nil), w.state.OpenTabs...)
	w.mu.RUnlock()
	if loaded {
		w.activateExternalTab(path)
		return true
	}
	for _, candidate := range tabs {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !sameNotificationPath(path, resolved) {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && os.SameFile(noteInfo, info) {
			w.mu.RLock()
			loaded := w.sessions[candidate] != nil
			w.mu.RUnlock()
			if loaded {
				w.activateExternalTab(candidate)
				return true
			}
		}
	}
	w.installClassifiedTab(path, classified)
	w.activateExternalTab(path)
	return true
}

func sameNotificationPath(path, candidate string) bool {
	if path == candidate {
		return true
	}
	if (runtime.GOOS != "darwin" && runtime.GOOS != "windows") || !strings.EqualFold(path, candidate) {
		return false
	}
	dir, candidateDir := filepath.Dir(path), filepath.Dir(candidate)
	if dir != candidateDir {
		info, err := os.Stat(dir)
		other, otherErr := os.Stat(candidateDir)
		if err != nil || otherErr != nil || !os.SameFile(info, other) {
			return false
		}
	}
	name, candidateName := filepath.Base(path), filepath.Base(candidate)
	if name == candidateName {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	var matches int
	for _, entry := range entries {
		if entry.Name() == name || entry.Name() == candidateName {
			matches++
		}
	}
	return matches == 1
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

func (h *pluginHostUI) Pick(ctx context.Context, save, directory, multiple bool, title, defaultPath string, extensions []string, allowed func() bool) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.closing:
				cancel()
				return
			case <-ticker.C:
				if !allowed() {
					cancel()
					return
				}
			}
		}
	}()
	var filters []mygo.FileFilter
	if len(extensions) != 0 {
		filters = []mygo.FileFilter{{Name: theme.Tr("文件类型", "File types"), Extensions: extensions}}
	}
	win := h.w.Window()
	checkBeforeShow := func() bool {
		return ctx.Err() == nil && win != nil && win == h.w.Window() && win.IsFocused() && allowed()
	}
	var paths []string
	var err error
	if !allowed() {
		return nil, plugins.ErrSystemCanceled
	}
	if save {
		var path string
		path, err = files.SaveDialogGuarded(ctx, win, mygo.SaveDialogOptions{Title: title, DefaultPath: defaultPath, Filters: filters, CreateDirectories: true}, checkBeforeShow)
		if path != "" {
			paths = []string{path}
		}
	} else {
		paths, err = files.OpenDialogGuarded(ctx, win, mygo.OpenDialogOptions{
			Title: title, DefaultPath: defaultPath, Filters: filters, Directory: directory, Multiple: multiple, CreateDirectories: directory,
		}, checkBeforeShow)
	}
	if !allowed() {
		return nil, plugins.ErrSystemCanceled
	}
	if errors.Is(err, files.ErrFileDialogBusy) {
		return nil, plugins.ErrSystemBusy
	}
	if errors.Is(err, files.ErrFileDialogUnavailable) {
		return nil, plugins.ErrSystemUnavailable
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
	if result.Button != 0 || !allowed() {
		return plugins.ErrSystemCanceled
	}
	var trashErr error
	if waitErr := h.onMainWait(func() {
		trashErr = h.w.trashWorkspaceItem(path, func() error {
			return systemintegration.TrashFile(files.AppDataDir(), path, info, allowed, validPath)
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
