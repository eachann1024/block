//! Composer observations restricted to the isolated smoke sandbox.
use super::*;
use gpui::EntityInputHandler;
use super::super::backend::{BackendConfig, BackendKind, BackendStatus};

pub(crate) fn set_composition(window: &mut Window, cx: &mut App, text: &str) {
    assert!(crate::smoke::is_requested());
    let entity = chat(window, cx);
    entity.update(cx, |chat, cx| chat.input.update(cx, |input, cx| {
        input.replace_and_mark_text_in_range(None, text, None, window, cx);
    }));
}

pub(crate) fn clear_composition(window: &mut Window, cx: &mut App) {
    assert!(crate::smoke::is_requested());
    let entity = chat(window, cx);
    entity.update(cx, |chat, cx| chat.input.update(cx, |input, cx| input.unmark_text(window, cx)));
}

pub(crate) fn is_composing(window: &mut Window, cx: &mut App) -> bool {
    assert!(crate::smoke::is_requested());
    let entity = chat(window, cx);
    entity.update(cx, |chat, cx| chat.composing(window, cx))
}

pub(crate) fn seed_tab_conversations(window: &mut Window, cx: &mut App) -> (u64, u64) {
    assert!(crate::smoke::is_requested());
    let ids = (9_000_001, 9_000_002);
    chat(window, cx).update(cx, |this, cx| {
        this.convs.retain(|conversation| conversation.id != ids.0 && conversation.id != ids.1);
        for (id, title) in [(ids.1, "Smoke B"), (ids.0, "Smoke A")] {
            this.convs.insert(0, Conv { id, title: title.into(), messages: vec![Msg { role: "user".into(), content: title.into(), ..Default::default() }], backend: BackendKind::Builtin, native_session_id: None });
        }
        this.tab_drafts.clear();
        this.current = Some(ids.0);
        this.refs.clear(); this.skills.clear(); this.retry_draft = None;
        this.input.update(cx, |input, cx| input.set_value("", window, cx));
        cx.notify();
    });
    window.refresh();
    ids
}

pub(crate) fn tab_observation(cx: &App) -> Option<(Option<u64>, String, Vec<String>, Vec<String>)> {
    assert!(crate::smoke::is_requested());
    cx.try_global::<Holder>().map(|holder| {
        let chat = holder.0.read(cx);
        (chat.current, chat.input.read(cx).value().to_string(), chat.refs.clone(), chat.skills.clone())
    })
}

thread_local! {
    static STATUS_OVERRIDES: std::cell::RefCell<Vec<(BackendKind, BackendStatus)>> = const { std::cell::RefCell::new(Vec::new()) };
}

pub(super) fn status_override(kind: BackendKind) -> Option<BackendStatus> {
    if !crate::smoke::is_requested() { return None; }
    STATUS_OVERRIDES.with(|cell| cell.borrow().iter().find(|(k, _)| *k == kind).map(|(_, s)| s.clone()))
}

pub(crate) fn seed_all_backends_ready(window: &mut Window, cx: &mut App) {
    assert!(crate::smoke::is_requested());
    let mut config = BackendConfig::load();
    for kind in BackendKind::ALL { config.set_enabled(kind, true, &BackendKind::ALL).expect("enable smoke backend"); }
    let statuses: Vec<(BackendKind, BackendStatus)> = BackendKind::ALL
        .into_iter()
        .map(|kind| (kind, BackendStatus::Ready { version: "smoke".into() }))
        .collect();
    STATUS_OVERRIDES.with(|cell| *cell.borrow_mut() = statuses.clone());
    chat(window, cx).update(cx, |this, cx| {
        this.backend_statuses = statuses; this.backend_config = config;
        let recognized = this.recognized_backends();
        this.backend_config.ensure_valid_selection(&recognized);
        cx.notify();
    });
    window.refresh();
}

pub(crate) fn reset_external_backends_unchecked(window: &mut Window, cx: &mut App) {
    assert!(crate::smoke::is_requested());
    let mut config = BackendConfig::load();
    config.priority.clear(); config.selected = BackendKind::Builtin;
    for kind in BackendKind::ALL {
        config.set_enabled(kind, kind == BackendKind::Builtin, &BackendKind::ALL).expect("reset smoke backend");
        config.set_full_permissions(kind, false).expect("reset smoke permissions");
    }
    crate::editor::ai::settings::reload(window, cx);
    super::sync_settings(cx);
    window.refresh();
}

pub(crate) fn focused_settings_backend(cx: &App) -> BackendKind {
    assert!(crate::smoke::is_requested());
    crate::editor::ai::settings::section(cx).expect("settings").read(cx).focused_backend
}

pub(crate) fn agent_full_permissions(kind: BackendKind) -> bool {
    assert!(crate::smoke::is_requested()); BackendConfig::load().full_permissions(kind)
}

pub(crate) fn set_backend_missing(kind: BackendKind, window: &mut Window, cx: &mut App) {
    assert!(crate::smoke::is_requested());
    STATUS_OVERRIDES.with(|cell| {
        let mut list = cell.borrow_mut();
        list.retain(|(k, _)| *k != kind);
        list.push((kind, BackendStatus::Missing));
    });
    chat(window, cx).update(cx, |this, cx| {
        this.backend_statuses.retain(|(k, _)| *k != kind);
        this.backend_statuses.push((kind, BackendStatus::Missing));
        let recognized = this.recognized_backends();
        this.backend_config.ensure_valid_selection(&recognized);
        let _ = this.backend_config.save();
        cx.notify();
    });
    window.refresh();
}

pub(crate) fn selected_backend(window: &mut Window, cx: &mut App) -> BackendKind {
    assert!(crate::smoke::is_requested());
    chat(window, cx).read(cx).selected_backend()
}

pub(crate) fn is_backend_enabled(kind: BackendKind) -> bool {
    assert!(crate::smoke::is_requested());
    BackendConfig::load().is_enabled(kind)
}

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

pub(crate) fn picker_folder_state(window: &mut Window, cx: &mut App) -> (String, usize, Option<usize>) {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let this = chat.read(cx);
    let index = this.picker_items("@", cx).iter().position(|(pick, _, _)| matches!(pick, Pick::Folder(path) if path == "picker-smoke"));
    (this.picker_cache.folder.clone(), this.picker_cache.pages.len(), index)
}
