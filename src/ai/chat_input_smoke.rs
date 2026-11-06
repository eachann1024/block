//! Composer observations restricted to the isolated smoke sandbox.
use super::*;

pub(crate) fn snapshot(window: &mut Window, cx: &mut App) -> (String, gpui::Point<gpui::Pixels>) {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let input = chat.read(cx).input.read(cx);
    (input.value().to_string(), input.scroll_offset())
}

pub(crate) fn is_focused(window: &mut Window, cx: &mut App) -> bool {
    use gpui::Focusable;
    assert!(crate::smoke::is_requested());
    chat(window, cx).read(cx).input.read(cx).focus_handle(cx).is_focused(window)
}

pub(crate) fn prepare_picker(window: &mut Window, cx: &mut App) -> Result<(), String> {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let (root, _) = chat.read(cx).picker_location(cx);
    if root.as_os_str().is_empty() { return Err("Missing sandbox notebook".into()); }
    let fixture = root.join("picker-smoke");
    std::fs::create_dir_all(&fixture).map_err(|e| e.to_string())?;
    for index in 0..1200 {
        std::fs::write(fixture.join(format!("note-{index:04}.md")), "fixture").map_err(|e| e.to_string())?;
    }
    std::fs::write(fixture.join("SKILL.md"), "name: picker-fixture\ndescription: picker smoke skill\n").map_err(|e| e.to_string())?;
    chat.update(cx, |this, _| this.picker_cache = Default::default());
    Ok(())
}

pub(crate) fn picker_state(window: &mut Window, cx: &mut App) -> (bool, usize, Vec<String>, Vec<String>, u128) {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let this = chat.read(cx);
    let start = std::time::Instant::now();
    let count = this.picker_token(cx).map(|token| this.picker_items(&token, cx).len()).unwrap_or(0);
    (this.picker_cache.loading, count, this.refs.clone(), this.skills.clone(), start.elapsed().as_millis())
}

pub(crate) fn cleanup_picker(window: &mut Window, cx: &mut App) {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let (root, _) = chat.read(cx).picker_location(cx);
    if !root.as_os_str().is_empty() { let _ = std::fs::remove_dir_all(root.join("picker-smoke")); }
    chat.update(cx, |this, cx| { this.picker_cache = Default::default(); this.new_conv(window, cx); });
}
