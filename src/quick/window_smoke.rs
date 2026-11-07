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

    // Dock reopen and the explicit main-window shortcut share this production entry point.
    cx.update(main::activate_main_window);
    pause(cx).await;
    check(cx, true, true, "唤起主窗口不隐藏或销毁小窗")?;
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
    cx.update(main::activate_main_window);
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
    cx.update(|cx| cx.hide());
    pause(cx).await;
    check(cx, false, true, "隐藏应用不关闭独立小窗")?;
    cx.update(super::close_if_open);
    pause(cx).await;
    cx.update(super::toggle);
    pause(cx).await;
    check(cx, false, true, "应用隐藏时仍能打开小窗且不唤起主窗口")?;
    cx.update(main::activate_main_window);
    pause(cx).await;
    check(cx, true, true, "应用隐藏后恢复主窗口保留小窗")?;
    cx.update(super::close_if_open);
    check(cx, true, false, "显式关闭小窗不影响主窗口")?;
    cx.update(|_| { native::hide_main_window_on_close(); });
    Ok(())
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
            flag(win, selector("isKeyWindow")) != YES &&
                (flag(win, selector("isVisible")) != YES ||
                    (NSWindow::alphaValue(win) == 0. && flag(win, selector("ignoresMouseEvents")) == YES))
        })
    }
}
