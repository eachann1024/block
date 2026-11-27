package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/app"
	"block/internal/plugins"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	smokePluginID           = "smoke-ui-probe"
	smokePluginInsertMarker = "[SMOKE_PLUGIN_INSERTED]"
)

// installSmokePluginFixture writes a self-contained sandboxed test
// plugin into a temporary directory and installs it into dataDir via StoreManager.
// It stays disabled until the dedicated plugin UI acceptance step enables it so
// earlier workspace steps run unaffected.
func installSmokePluginFixture(baseTempDir, dataDir string) error {
	srcDir := filepath.Join(baseTempDir, "smoke-plugin-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("mkdir smoke plugin src: %w", err)
	}

	icon := plugins.AppStoreIconSpec()
	manifest := plugins.Manifest{
		App:           plugins.TargetApp,
		SchemaVersion: plugins.CurrentSchemaVersion,
		ID:            smokePluginID,
		Name:          plugins.LocalizedText{Zh: "烟测界面插件", En: "Smoke UI Probe"},
		Version:       "1.0.0",
		Author:        "Block Smoke",
		Summary:       plugins.LocalizedText{Zh: "验证插件顶栏、命令面板、面板、提示与对话框", En: "Exercises plugin titlebar, palette, panel, toast and dialog UI"},
		Entry:         plugins.EntrySpec{JS: "plugin.js"},
		Permissions: []plugins.Permission{
			plugins.PermUITitlebar,
			plugins.PermUISettings,
			plugins.PermUICommands,
			plugins.PermUIPanels,
			plugins.PermUIStatus,
			plugins.PermUINotify,
			plugins.PermEditorRead,
			plugins.PermEditorEdit,
			plugins.PermEventsUI,
		},
		Surfaces: []plugins.Surface{plugins.SurfaceSettingsPage},
		Icon:     icon,
		Contributes: &plugins.Contributes{
			TitlebarManager: true,
			TitlebarButtons: []plugins.TitlebarButtonContrib{
				{
					ID:      "toggle_panel",
					Icon:    icon,
					Tooltip: plugins.LocalizedText{Zh: "打开烟测面板", En: "Toggle Smoke Panel"},
					Panel:   "inspector",
				},
				{
					ID:      "quick_toast",
					Icon:    icon,
					Tooltip: plugins.LocalizedText{Zh: "触发烟测提示", En: "Trigger Smoke Toast"},
					Command: "show_toast",
				},
			},
			SettingsSections: []plugins.SettingsSectionContrib{
				{
					ID:    "titlebar_ctrl",
					Tab:   "appearance",
					Title: plugins.LocalizedText{Zh: "顶栏图标控制", En: "Titlebar Controls"},
				},
			},
			Commands: []plugins.CommandContrib{
				{
					ID:    "show_toast",
					Title: plugins.LocalizedText{Zh: "显示烟测提示", En: "Show Smoke Toast"},
				},
				{
					ID:    "open_dialog",
					Title: plugins.LocalizedText{Zh: "打开烟测对话框", En: "Open Smoke Dialog"},
				},
				{
					ID:    "open_panel",
					Title: plugins.LocalizedText{Zh: "打开烟测面板", En: "Open Smoke Panel"},
				},
				{
					ID:    "hide_slots",
					Title: plugins.LocalizedText{Zh: "隐藏顶栏反馈与 AI", En: "Hide Feedback and AI"},
				},
				{
					ID:    "restore_slots",
					Title: plugins.LocalizedText{Zh: "恢复顶栏图标", En: "Restore Titlebar Icons"},
				},
				{
					ID:    "insert_note",
					Title: plugins.LocalizedText{Zh: "插入烟测标记", En: "Insert Smoke Marker"},
				},
			},
			PageMenu: []plugins.MenuContrib{
				{
					ID:      "menu_dialog",
					Title:   plugins.LocalizedText{Zh: "打开烟测弹窗", En: "Open Smoke Dialog"},
					Command: "open_dialog",
				},
			},
			Panels: []plugins.PanelContrib{
				{
					ID:    "inspector",
					Title: plugins.LocalizedText{Zh: "烟测检查面板", En: "Smoke Inspector"},
					Icon:  icon,
				},
			},
			StatusItems: []plugins.StatusItemContrib{
				{
					ID:      "counter",
					Text:    plugins.LocalizedText{Zh: "烟测就绪", En: "Smoke Ready"},
					Command: "show_toast",
				},
			},
			Dialogs: []plugins.DialogContrib{
				{
					ID:    "confirm_box",
					Title: plugins.LocalizedText{Zh: "烟测确认对话框", En: "Smoke Confirm Dialog"},
					Width: 440,
				},
			},
		},
	}

	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal smoke plugin manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), rawManifest, 0o600); err != nil {
		return fmt.Errorf("write smoke plugin manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.js"), []byte(smokePluginScript), 0o600); err != nil {
		return fmt.Errorf("write smoke plugin script: %w", err)
	}

	sm := plugins.NewStoreManager(dataDir)
	if err := sm.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure plugin dirs: %w", err)
	}
	if _, err := sm.InstallFromDirectory(srcDir); err != nil {
		return fmt.Errorf("install smoke plugin fixture: %w", err)
	}
	return nil
}

const smokePluginScript = `let clickCount = 0;

function buildPage(surface) {
  switch (surface) {
    case 'settings_section:titlebar_ctrl':
      return {
        title: '顶栏图标控制',
        cards: [{ id: 'titlebar_card', title: '右上角图标显示', items: [
          { kind: 'actions', buttons: [
            { id: 'apply_hide', label: '隐藏反馈与 AI', variant: 'primary' },
            { id: 'apply_restore', label: '恢复全部图标', variant: 'secondary' },
          ] },
        ] }],
      };
    case 'panel:inspector':
      return {
        title: '烟测检查面板',
        cards: [{ id: 'panel_card', title: '面板操作', items: [
          { kind: 'kv', rows: [{ label: '点击计数', value: String(clickCount) }] },
          { kind: 'actions', buttons: [
            { id: 'panel_open_dialog', label: '打开确认对话框', variant: 'primary' },
            { id: 'panel_toast', label: '发送面板提示', variant: 'secondary' },
          ] },
        ] }],
      };
    case 'dialog:confirm_box':
      return {
        title: '烟测确认对话框',
        cards: [{ id: 'dialog_card', title: '对话框动作', items: [
          { kind: 'actions', buttons: [
            { id: 'dialog_insert', label: '插入标记文本', variant: 'primary' },
            { id: 'dialog_close', label: '关闭对话框', variant: 'secondary' },
          ] },
        ] }],
      };
    default:
      return {
        title: '烟测界面插件',
        cards: [{ id: 'overview', title: '概览', items: [
          { kind: 'kv', rows: [{ label: '状态', value: '运行中' }] },
        ] }],
      };
  }
}

function triggerToast() {
  clickCount++;
  callHost('host.titlebar.set_button', { id: 'quick_toast', badge: String(clickCount) });
  callHost('host.status.set', { id: 'counter', text: '烟测已触发 ' + clickCount, tooltip: '烟测状态更新' });
  callHost('host.ui.toast', { message: '烟测提示 #' + clickCount, tone: 'success' });
}

function handleAction({ action_id, surface }) {
  let result = { ok: true };
  switch (action_id) {
    case 'apply_hide':
      callHost('host.titlebar.set_hidden', { hidden: ['feedback', 'ai', 'more'] });
      result = { ...result, tone: 'success', status: '已隐藏反馈与 AI' };
      break;
    case 'apply_restore':
      callHost('host.titlebar.set_hidden', { hidden: [] });
      result = { ...result, tone: 'success', status: '已恢复顶栏图标' };
      break;
    case 'panel_open_dialog':
      callHost('host.ui.open_dialog', { id: 'confirm_box' });
      result.tone = 'info';
      break;
    case 'panel_toast':
      triggerToast();
      result.tone = 'success';
      break;
    case 'dialog_insert':
      callHost('host.editor.insert', { text: '` + smokePluginInsertMarker + `' });
      callHost('host.ui.toast', { message: '已插入烟测标记', tone: 'success' });
      result.tone = 'success';
      break;
    case 'dialog_close':
      callHost('host.ui.close_dialog', { id: 'confirm_box' });
      return { ok: true, tone: 'info' };
  }
  return { ...result, page: buildPage(surface) };
}

function handleCommand(id) {
  switch (id) {
    case 'show_toast':
      triggerToast();
      return { ok: true, tone: 'success' };
    case 'open_dialog':
      callHost('host.ui.open_dialog', { id: 'confirm_box' });
      return { ok: true, tone: 'info' };
    case 'open_panel':
      callHost('host.ui.open_panel', { id: 'inspector' });
      return { ok: true, tone: 'info' };
    case 'hide_slots':
      callHost('host.titlebar.set_hidden', { hidden: ['feedback', 'ai', 'more'] });
      return { ok: true, tone: 'success', status: '已隐藏顶栏图标' };
    case 'restore_slots':
      callHost('host.titlebar.set_hidden', { hidden: [] });
      return { ok: true, tone: 'success', status: '已恢复顶栏图标' };
    case 'insert_note':
      callHost('host.editor.insert', { text: '` + smokePluginInsertMarker + `' });
      return { ok: true, tone: 'success', status: '已插入烟测标记' };
    default:
      return { ok: true };
  }
}

serve((method, params) => {
  switch (method) {
    case 'plugin.initialize': return { ready: true };
    case 'plugin.render': return { page: buildPage(params.surface) };
    case 'plugin.action': return handleAction(params);
    case 'plugin.command': return handleCommand(params.command_id);
    case 'plugin.event': return { handled: true };
    case 'plugin.shutdown': return { ok: true };
    default: throw new HostRPCError(-32601, 'unknown method: ' + method);
  }
});
`

func waitForProbe(tt *ui.Tester, id string, timeout time.Duration) (ui.Rect, error) {
	var found ui.Rect
	err := waitUntil(timeout, func() bool {
		tt.Frame()
		tt.Advance(40 * time.Millisecond)
		r, ok := blockui.TargetBounds(id)
		if ok && r.W > 0 && r.H > 0 {
			found = r
			return true
		}
		return false
	})
	if err != nil {
		return ui.Rect{}, fmt.Errorf("probe %q did not appear: %w", id, err)
	}
	return found, nil
}

func waitForProbeGone(tt *ui.Tester, id string, timeout time.Duration) error {
	return waitUntil(timeout, func() bool {
		tt.Frame()
		tt.Advance(60 * time.Millisecond)
		_, ok := blockui.TargetBounds(id)
		return !ok
	})
}

// scrollSettingsUntilVisible scrolls the right settings content column until
// target probe id's click center lies within the visible settings-page viewport.
func scrollSettingsUntilVisible(tt *ui.Tester, id string) (ui.Rect, error) {
	tt.Frame()
	minY, maxY := float32(56), float32(776)
	scrollX, scrollY := float32(760), float32(420)
	if page, ok := blockui.TargetBounds("settings-page"); ok {
		minY = page.Y + 4
		maxY = page.Y + page.H - 4
		scrollX = page.X + page.W*0.5
		scrollY = page.Y + page.H*0.5
	}
	inView := func(r ui.Rect) bool {
		cy := r.Y + r.H*0.5
		return r.W > 0 && r.H > 0 && cy >= minY && cy <= maxY
	}
	initial, ok := blockui.TargetBounds(id)
	if ok && inView(initial) {
		return initial, nil
	}
	for i := 0; i < 20; i++ {
		tt.Scroll(scrollX, scrollY, 0, 200)
		tt.Frame()
		cur, curOK := blockui.TargetBounds(id)
		if curOK && inView(cur) {
			return cur, nil
		}
	}
	r, ok := blockui.TargetBounds(id)
	if !ok {
		return ui.Rect{}, fmt.Errorf("probe %q missing after scrolling settings", id)
	}
	return r, fmt.Errorf("probe %q outside viewport [%.1f..%.1f] (init=%+v final=%+v)", id, minY, maxY, initial, r)
}

func runPluginUIAcceptance(ws *app.Workspace, tt *ui.Tester, notePath string) (string, error) {
	sup := ws.Plugins()
	if sup == nil {
		return "", fmt.Errorf("workspace has no plugin supervisor")
	}
	if !sup.IsInstalled(smokePluginID) {
		return "", fmt.Errorf("smoke plugin fixture %q not installed", smokePluginID)
	}

	// Ensure an active Markdown note is open and focused.
	if err := ws.OpenNote(notePath, 0); err != nil {
		return "", fmt.Errorf("open note for plugin step: %w", err)
	}
	tt.Frame()

	// 1) Enable test plugin -> verify titlebar plugin buttons & status strip appear.
	sup.SetPluginEnabled(smokePluginID, true)
	defer sup.SetPluginEnabled(smokePluginID, false)

	panelBtnID := "titlebar-plugin-" + smokePluginID + "-toggle_panel"
	toastBtnID := "titlebar-plugin-" + smokePluginID + "-quick_toast"
	statusItemID := "plugin-status-" + smokePluginID + "-counter"

	if _, err := waitForProbe(tt, panelBtnID, 5*time.Second); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, toastBtnID, 5*time.Second); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, "plugin-status-strip", 5*time.Second); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, statusItemID, 5*time.Second); err != nil {
		return "", err
	}
	if _, ok := blockui.TargetBounds("titlebar-feedback"); !ok {
		return "", fmt.Errorf("expected titlebar-feedback visible before hiding")
	}
	if _, ok := blockui.TargetBounds("titlebar-ai"); !ok {
		return "", fmt.Errorf("expected titlebar-ai visible before hiding")
	}
	if _, ok := blockui.TargetBounds("titlebar-more"); !ok {
		return "", fmt.Errorf("expected titlebar-more visible before hiding")
	}

	// 2) Settings appearance plugin section -> hide feedback & AI, attempt to hide more (must stay visible).
	ws.OpenSettings("appearance")
	tt.Frame()
	hideActionID := "plugin-act-" + smokePluginID + "-settings_section-titlebar_ctrl-apply_hide"
	if _, err := waitForProbe(tt, hideActionID, 8*time.Second); err != nil {
		return "", fmt.Errorf("appearance settings plugin section did not render: %w", err)
	}
	hideRect, err := scrollSettingsUntilVisible(tt, hideActionID)
	if err != nil {
		return "", err
	}
	tt.ClickAt(hideRect.X+hideRect.W*0.5, hideRect.Y+hideRect.H*0.5)
	if err := waitUntil(5*time.Second, func() bool {
		tt.Frame()
		return sup.TitlebarHidden(plugins.TitlebarSlotFeedback) && sup.TitlebarHidden(plugins.TitlebarSlotAI)
	}); err != nil {
		return "", fmt.Errorf("host.titlebar.set_hidden did not hide feedback/ai: %w", err)
	}
	if sup.TitlebarHidden(plugins.TitlebarSlotMore) {
		return "", fmt.Errorf("titlebar-more must never be hidden")
	}

	// Return to workspace and verify titlebar-feedback & titlebar-ai are gone while titlebar-more remains.
	if err := clickTarget(tt, "settings-sidebar-back"); err != nil {
		if err := clickTarget(tt, "settings-back"); err != nil {
			return "", err
		}
	}
	tt.Frame()
	tt.Advance(popupSettle)
	tt.Frame()
	if _, ok := blockui.TargetBounds("titlebar-feedback"); ok {
		return "", fmt.Errorf("expected titlebar-feedback hidden in titlebar")
	}
	if _, ok := blockui.TargetBounds("titlebar-ai"); ok {
		return "", fmt.Errorf("expected titlebar-ai hidden in titlebar")
	}
	if _, ok := blockui.TargetBounds("titlebar-more"); !ok {
		return "", fmt.Errorf("expected titlebar-more to stay visible when plugin requested hiding it")
	}

	// 3) Command Palette (Cmd+Shift+P): keyboard navigation + Escape close, then empty filter -> filter & run restore_slots.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	if _, err := waitForProbe(tt, "command-palette", 3*time.Second); err != nil {
		return "", fmt.Errorf("command palette did not open on Cmd+Shift+P: %w", err)
	}
	if _, err := waitForProbe(tt, "command-palette-input", 3*time.Second); err != nil {
		return "", err
	}
	if err := clickTarget(tt, "command-palette-input"); err != nil {
		return "", err
	}
	// Exercise OnTextKey Down/Up/Escape branches directly.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if err := waitForProbeGone(tt, "command-palette", 3*time.Second); err != nil {
		return "", fmt.Errorf("command palette did not close on Escape: %w", err)
	}

	// Re-open command palette, verify empty state on unmatched query, then filter and execute restore_slots.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	if _, err := waitForProbe(tt, "command-palette-input", 3*time.Second); err != nil {
		return "", err
	}
	if err := clickTarget(tt, "command-palette-input"); err != nil {
		return "", err
	}
	tt.Type("zzznomatch")
	if _, err := waitForProbe(tt, "command-palette-empty", 3*time.Second); err != nil {
		return "", fmt.Errorf("expected command-palette-empty on unmatched query: %w", err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("恢复顶栏图标")
	if _, err := waitForProbe(tt, "command-palette-item-0", 3*time.Second); err != nil {
		return "", fmt.Errorf("expected filtered restore command in palette: %w", err)
	}
	tt.Key(0, ui.KeyEnter)
	if err := waitForProbeGone(tt, "command-palette", 3*time.Second); err != nil {
		return "", fmt.Errorf("command palette stayed open after pressing Enter: %w", err)
	}
	if err := waitUntil(5*time.Second, func() bool {
		tt.Frame()
		return !sup.TitlebarHidden(plugins.TitlebarSlotFeedback) && !sup.TitlebarHidden(plugins.TitlebarSlotAI)
	}); err != nil {
		return "", fmt.Errorf("restore_slots command did not restore hidden titlebar slots: %w", err)
	}
	if _, err := waitForProbe(tt, "titlebar-feedback", 3*time.Second); err != nil {
		return "", fmt.Errorf("titlebar-feedback did not reappear after restore: %w", err)
	}
	if _, err := waitForProbe(tt, "titlebar-ai", 3*time.Second); err != nil {
		return "", fmt.Errorf("titlebar-ai did not reappear after restore: %w", err)
	}

	// Dismiss the status toast produced by restore_slots if shown.
	tt.Frame()
	if _, ok := blockui.TargetBounds("global-toast-close"); ok {
		_ = clickTarget(tt, "global-toast-close")
		_ = waitForProbeGone(tt, "global-toast", 2*time.Second)
	}

	// 4) Status bar & titlebar command button clicks -> toast + host.status.set + host.titlebar.set_button badge -> dismiss toast.
	if err := clickTarget(tt, statusItemID); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, "global-toast", 5*time.Second); err != nil {
		return "", fmt.Errorf("expected global-toast after clicking status item: %w", err)
	}
	if _, err := waitForProbe(tt, "global-toast-close", 2*time.Second); err != nil {
		return "", err
	}
	if err := waitUntil(3*time.Second, func() bool {
		for _, it := range sup.StatusItems() {
			if it.PluginID == smokePluginID && it.ID == "counter" && strings.Contains(it.Text, "烟测已触发") {
				return true
			}
		}
		return false
	}); err != nil {
		return "", fmt.Errorf("host.status.set did not update status item text: %w", err)
	}
	if err := clickTarget(tt, "global-toast-close"); err != nil {
		return "", err
	}
	if err := waitForProbeGone(tt, "global-toast", 3*time.Second); err != nil {
		return "", fmt.Errorf("global-toast did not close after clicking close button: %w", err)
	}
	// Click titlebar command button and confirm badge updated to "2" and toast re-appeared.
	if err := clickTarget(tt, toastBtnID); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, "global-toast", 5*time.Second); err != nil {
		return "", fmt.Errorf("expected global-toast after clicking titlebar command button: %w", err)
	}
	if err := waitUntil(3*time.Second, func() bool {
		for _, b := range sup.TitlebarButtons() {
			if b.PluginID == smokePluginID && b.ID == "quick_toast" && b.Badge == "2" {
				return true
			}
		}
		return false
	}); err != nil {
		return "", fmt.Errorf("host.titlebar.set_button did not update button badge to 2: %w", err)
	}
	if err := clickTarget(tt, "global-toast-close"); err != nil {
		return "", err
	}
	if err := waitForProbeGone(tt, "global-toast", 3*time.Second); err != nil {
		return "", fmt.Errorf("global-toast did not close after second dismiss: %w", err)
	}

	// 5) Plugin side panel: click titlebar button -> panel opens -> panel surface renders -> click panel button to open dialog.
	if err := clickTarget(tt, panelBtnID); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, "plugin-side-panel", 5*time.Second); err != nil {
		return "", fmt.Errorf("plugin side panel did not open: %w", err)
	}
	panelOpenDlgBtnID := "plugin-act-" + smokePluginID + "-panel-inspector-panel_open_dialog"
	if _, err := waitForProbe(tt, panelOpenDlgBtnID, 6*time.Second); err != nil {
		return "", fmt.Errorf("plugin side panel surface did not render action button: %w", err)
	}

	// 6) Plugin dialog: opened via panel action -> click dialog insert action -> verify note updated -> close via host.ui.close_dialog.
	if err := clickTarget(tt, panelOpenDlgBtnID); err != nil {
		return "", err
	}
	dialogID := "plugin-dialog-" + smokePluginID + "-confirm_box"
	dialogInsertBtnID := "plugin-act-" + smokePluginID + "-dialog-confirm_box-dialog_insert"
	dialogCloseActBtnID := "plugin-act-" + smokePluginID + "-dialog-confirm_box-dialog_close"
	if _, err := waitForProbe(tt, dialogID, 5*time.Second); err != nil {
		return "", fmt.Errorf("plugin dialog did not open from panel action: %w", err)
	}
	if _, err := waitForProbe(tt, dialogInsertBtnID, 6*time.Second); err != nil {
		return "", fmt.Errorf("plugin dialog surface did not render insert button: %w", err)
	}
	if err := clickTarget(tt, dialogInsertBtnID); err != nil {
		return "", err
	}
	if err := waitUntil(5*time.Second, func() bool {
		tt.Frame()
		sess := ws.ActiveSession()
		return sess != nil && strings.Contains(sess.Markdown(), smokePluginInsertMarker)
	}); err != nil {
		return "", fmt.Errorf("host.editor.insert from plugin dialog did not update active note: %w", err)
	}
	if _, err := waitForProbe(tt, dialogCloseActBtnID, 3*time.Second); err != nil {
		return "", err
	}
	if err := clickTarget(tt, dialogCloseActBtnID); err != nil {
		return "", err
	}
	if err := waitForProbeGone(tt, dialogID, 4*time.Second); err != nil {
		return "", fmt.Errorf("plugin dialog did not close via host.ui.close_dialog: %w", err)
	}
	// Dismiss insert toast if still visible.
	if _, ok := blockui.TargetBounds("global-toast-close"); ok {
		_ = clickTarget(tt, "global-toast-close")
		_ = waitForProbeGone(tt, "global-toast", 2*time.Second)
	}

	// 7) Close plugin side panel via close button.
	if err := clickTarget(tt, "plugin-panel-close"); err != nil {
		return "", err
	}
	if err := waitForProbeGone(tt, "plugin-side-panel", 4*time.Second); err != nil {
		return "", fmt.Errorf("plugin side panel did not close via plugin-panel-close: %w", err)
	}

	// 8) Page "…" menu plugin item -> re-opens dialog -> close via header close button.
	if err := clickTarget(tt, "titlebar-more"); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, "page-more-menu", 3*time.Second); err != nil {
		return "", err
	}
	pageMenuPluginID := "page-plugin-" + smokePluginID + "-menu_dialog"
	if _, err := waitForProbe(tt, pageMenuPluginID, 3*time.Second); err != nil {
		return "", fmt.Errorf("plugin page menu item missing: %w", err)
	}
	if err := clickTarget(tt, pageMenuPluginID); err != nil {
		return "", err
	}
	if _, err := waitForProbe(tt, dialogID, 5*time.Second); err != nil {
		return "", fmt.Errorf("plugin dialog did not open from page menu item: %w", err)
	}
	if _, err := waitForProbe(tt, dialogInsertBtnID, 6*time.Second); err != nil {
		return "", fmt.Errorf("reopened plugin dialog did not freshly render its surface: %w", err)
	}
	if err := clickTarget(tt, dialogID+"-close"); err != nil {
		return "", err
	}
	if err := waitForProbeGone(tt, dialogID, 4*time.Second); err != nil {
		return "", fmt.Errorf("plugin dialog did not close via header close button: %w", err)
	}

	// 9) Disable plugin -> verify titlebar plugin buttons and status strip disappear cleanly.
	sup.SetPluginEnabled(smokePluginID, false)
	if err := waitForProbeGone(tt, panelBtnID, 4*time.Second); err != nil {
		return "", fmt.Errorf("plugin titlebar button did not disappear after disable: %w", err)
	}
	if err := waitForProbeGone(tt, "plugin-status-strip", 4*time.Second); err != nil {
		return "", fmt.Errorf("plugin status strip did not disappear after disable: %w", err)
	}

	return "Titlebar hiding (+more lock), Command Palette, Status bar, Toast, Side Panel, Dialog & Page Menu verified", nil
}
