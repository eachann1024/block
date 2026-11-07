//! Window independence checks run only inside the isolated, non-activating smoke worker.
use gpui::{AsyncApp, Keystroke};
use super::{QuickState, is_open, is_quick_panel_visible};
use crate::{search::window as main, window_visibility as native};

pub(crate) async fn verify(cx: &AsyncApp) -> Result<(), String> {
    if !crate::smoke::is_worker() || !cfg!(target_os = "macos") { return Ok(()); }
    cx.update(super::close_if_open);
    cx.update(|_| { native::hide_main_window_on_close(); });
    pause(cx).await;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, false, true, "打开小窗保留主窗口隐藏状态")?;
    let (handle, view) = cx.update(|cx| cx.global::<QuickState>().window.clone()).ok_or("小窗未创建")?;
    let handle: gpui::AnyWindowHandle = handle.into();
    let editor = cx.update(|cx| view.read(cx).editor.clone());
    for _ in 0..100 {
        if cx.update(|cx| !editor.read(cx).is_loading()) { break; }
        cx.background_executor().timer(std::time::Duration::from_millis(50)).await;
    }
    cx.update(|cx| handle.update(cx, |_, window, cx| {
        window.draw(cx).clear(cx);
        for key in ["cmd-a", "i", "n", "d", "e", "p", "e", "n", "d", "e", "n", "t"] {
            window.dispatch_keystroke(Keystroke::parse(key).unwrap(), cx);
            window.draw(cx).clear(cx);
        }
    })).map_err(|e| e.to_string())?;
    let before = cx.update(|cx| editor.read(cx).markdown_text());
    if !before.contains("independent") { return Err("小窗 GPUI 键盘输入未进入编辑器".into()); }
    repeat_toggles(cx, handle, false).await?;

    // Launchers send an app reopen with hasVisibleWindows=true while the quick panel is open.
    // Invoke the actual delegate outside cx.update so GPUI can borrow App for its callback.
    crate::app_reopen::smoke_reopen(true)?;
    pause(cx).await;
    check(cx, true, true, "外部打开 Block：小窗可见时仍唤起主窗口")?;
    crate::app_reopen::smoke_reopen(true)?;
    pause(cx).await;
    check(cx, true, true, "重复外部打开 Block 不切回或关闭小窗")?;
    cx.update(|cx| { main::ensure_main_window(cx); });
    let main_handle = cx.update(|cx| main::main_window(cx).and_then(|(handle, _)| handle)).ok_or("主窗口不存在")?;
    cx.update(|cx| main_handle.update(cx, |_, window, cx| main::reveal_main_window(window, cx)))
        .map_err(|e| e.to_string())?;
    pause(cx).await;
    check(cx, true, true, "主窗口菜单与编辑区操作不影响小窗")?;
    let preserved = cx.update(|cx| {
        cx.global::<QuickState>().window.as_ref().is_some_and(|(current, _)| gpui::AnyWindowHandle::from(*current) == handle)
            && editor.read(cx).markdown_text() == before
    });
    if !preserved { return Err("唤起主窗口改变了小窗实例或文本".into()); }

    cx.update(|_| { native::hide_main_window_on_close(); });
    check(cx, false, true, "关闭主窗口不影响小窗")?;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, false, true, "再次唤起小窗不打开主窗口")?;
    crate::app_reopen::smoke_reopen(true)?;
    pause(cx).await;
    // Escape follows the real QuickNote action route; only this window closes.
    cx.update(|cx| handle.update(cx, |_, window, cx| {
        window.draw(cx).clear(cx);
        window.dispatch_keystroke(Keystroke::parse("escape").unwrap(), cx);
    })).map_err(|e| e.to_string())?;
    pause(cx).await;
    check(cx, true, false, "小窗 Escape 关闭后主窗口仍打开")?;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, true, true, "主窗口可见时打开小窗，两窗同时保留")?;
    repeat_toggles(cx, handle, true).await?;
    cx.update(|cx| cx.hide());
    pause(cx).await;
    check(cx, false, true, "隐藏应用不关闭独立小窗")?;
    repeat_toggles(cx, handle, false).await?;
    cx.update(super::close_if_open);
    pause(cx).await;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, false, true, "应用隐藏时仍能打开小窗且不唤起主窗口")?;
    crate::app_reopen::smoke_reopen(true)?;
    pause(cx).await;
    check(cx, true, true, "应用隐藏后恢复主窗口保留小窗")?;
    cx.update(super::close_if_open);
    check(cx, true, false, "显式关闭小窗不影响主窗口")?;
    cx.update(|_| { native::hide_main_window_on_close(); });
    crate::app_reopen::smoke_reopen(false)?;
    pause(cx).await;
    check(cx, true, false, "全部窗口隐藏时外部打开 Block 只唤起主窗口")?;
    cx.update(|_| { native::hide_main_window_on_close(); });
    Ok(())
}

async fn repeat_toggles(cx: &AsyncApp, handle: gpui::AnyWindowHandle, main_visible: bool) -> Result<(), String> {
    let quick_key = crate::theme::quick_shortcut();
    // Exercise the focused-panel key binding, then the global-hotkey entry point.
    for key in [&quick_key, &quick_key, &quick_key, "escape", "cmd-w"] {
        cx.update(|cx| handle.update(cx, |_, window, cx| {
            window.draw(cx).clear(cx);
            window.dispatch_keystroke(Keystroke::parse_with_os(key, gpui::OperatingSystem::Mac).unwrap(), cx);
        })).map_err(|e| e.to_string())?;
        check(cx, main_visible, false, "快捷键隐藏小窗不改变主窗口")?;
        if key == &quick_key {
            cx.update(super::toggle);
            check(cx, main_visible, false, "同一次按键的全局回调不会再次打开小窗")?;
        }
        pause(cx).await;
        cx.update(super::toggle);
        pause(cx).await;
        check(cx, main_visible, true, "连续隐藏再显示小窗不唤起主窗口")?;
        let retained = cx.update(|cx| cx.global::<QuickState>().window.as_ref()
            .is_some_and(|(current, view)| gpui::AnyWindowHandle::from(*current) == handle
                && view.read(cx).editor.read(cx).markdown_text().contains("independent")));
        if !retained { return Err("连续切换销毁了小窗或丢失编辑内容".into()); }
    }
    // Native red close button must also hide, rather than destroy, the retained panel.
    #[cfg(target_os = "macos")]
    close_native_panel();
    pause(cx).await;
    check(cx, main_visible, false, "原生关闭按钮只隐藏小窗")?;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, main_visible, true, "原生关闭后仍可独立打开小窗")?;
    Ok(())
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn close_native_panel() {
    use cocoa::{appkit::NSApplication, base::{SEL, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let get: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        let close: unsafe extern "C" fn(id, SEL, id) = std::mem::transmute(objc_msgSend as *const ());
        let windows = get(NSApplication::sharedApplication(nil), selector("windows"));
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if super::panel::is_quick_panel(win) { close(win, selector("performClose:"), nil); }
        }
    }
}

async fn pause(cx: &AsyncApp) {
    cx.background_executor().timer(std::time::Duration::from_millis(300)).await;
}

fn check(cx: &AsyncApp, main_visible: bool, quick_visible: bool, label: &str) -> Result<(), String> {
    let state = cx.update(|cx| (native::is_main_window_visible(), is_quick_panel_visible(), is_open(cx)));
    if state != (main_visible, quick_visible, quick_visible) {
        return Err(format!("{label}: native main/quick + quick handle = {state:?}"));
    }
    #[cfg(target_os = "macos")]
    if !cx.update(|_| remains_in_background()) { return Err(format!("{label}: 自检窗口不再处于安全的后台隔离状态")); }
    println!("[PASS] Window independence: {label}");
    Ok(())
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn remains_in_background() -> bool {
    use cocoa::{appkit::{NSApplication, NSWindow}, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let flag: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        let get: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        if flag(app, selector("isActive")) == YES { return false; }
        let windows = get(app, selector("windows"));
        (0..NSArray::count(windows)).all(|i| {
            let win = NSArray::objectAtIndex(windows, i);
            (!super::panel::is_quick_panel(win) || flag(win, selector("canBecomeMainWindow")) != YES) &&
                flag(win, selector("isKeyWindow")) != YES &&
                (flag(win, selector("isVisible")) != YES ||
                    (NSWindow::alphaValue(win) == 0. && flag(win, selector("ignoresMouseEvents")) == YES))
        })
    }
}
