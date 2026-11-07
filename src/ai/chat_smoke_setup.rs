use gpui::{App, Window};
use super::{chat, recognized_backends, sync_settings};
use super::super::{backend, provider, settings};

pub(crate) fn enable_smoke_builtin(window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("AI fixture requires smoke mode".into()); }
    let config = provider::Config { enabled: true, provider: "openai".into(), base_url: "http://127.0.0.1:1/v1".into(),
        api_key: "smoke-fixture".into(), model: "smoke-fixture".into(), ..Default::default() };
    provider::save(&config)?;
    let mut backend_config = backend::BackendConfig::load();
    let recognized = recognized_backends(cx);
    let _ = backend_config.set_enabled(backend::BackendKind::Builtin, true, &recognized);
    settings::reload(window, cx);
    sync_settings(cx);
    chat(window, cx).update(cx, |_, cx| cx.notify());
    window.refresh();
    Ok(())
}

pub(crate) fn set_process_smoke_status(status: &str, window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("Process fixture requires smoke mode".into()); }
    if !matches!(status, "running" | "completed" | "failed" | "stopped") { return Err("Unknown process status".into()); }
    chat(window, cx).update(cx, |this, cx| {
        this.streaming = None;
        if let Some(conversation) = this.current.and_then(|id| this.convs.iter_mut().find(|item| item.id == id)) {
            conversation.messages.retain(|message| message.role != "turn_outcome");
        }
        if status == "running" {
            this.streaming = Some((this.serial, std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false))));
        } else { super::super::chat_process::record_outcome(this, status); }
        cx.notify();
    });
    window.refresh();
    Ok(())
}

pub(crate) fn seed_source_smoke(root: &std::path::Path, page: &str, window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("Source fixture requires smoke mode".into()); }
    let root = root.to_path_buf(); let page = page.to_owned();
    chat(window, cx).update(cx, |this, cx| {
        let context = this.context_for_root(&root, cx)?;
        context.checked_path(&page)?;
        let args = serde_json::json!({"pageId": page, "notebookRoot": root.to_string_lossy()});
        let (result, _) = super::super::agent::exec("readPage", &args.to_string(), &context);
        if let Some(error) = result.get("error") { return Err(error.to_string()); }
        this.new_conv(window, cx); this.open = true;
        this.push_msg(super::Msg { role: "user".into(), content: "Read source".into(), ..Default::default() });
        this.push_msg(super::Msg { role: "assistant".into(), tool_calls: Some(serde_json::json!([{ "id": "source-smoke", "type": "function", "function": { "name": "readPage", "arguments": args.to_string() } }])), ..Default::default() });
        this.push_msg(super::Msg { role: "tool".into(), tool_call_id: Some("source-smoke".into()), content: result.to_string(), ..Default::default() });
        cx.notify(); Ok(())
    })?;
    window.refresh(); Ok(())
}

pub(crate) fn set_stream_smoke(text: &str, streaming: bool, window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("Stream fixture requires smoke mode".into()); }
    chat(window, cx).update(cx, |this, cx| {
        this.partial = text.to_owned();
        this.streaming = streaming.then(|| (this.serial, std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false))));
        if !streaming && !text.is_empty() { this.push_msg(super::Msg { role: "assistant".into(), content: text.to_owned(), ..Default::default() }); this.partial.clear(); }
        cx.notify();
    });
    window.refresh(); Ok(())
}
