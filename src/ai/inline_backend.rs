//! Inline rewrites share the selected backend, but never apply tool side effects.
use std::sync::{Arc, atomic::{AtomicBool, AtomicU64, Ordering}, mpsc};
use std::time::Duration;
use gpui::App;
use super::{backend::{self, BackendConfig, BackendEventKind, BackendKind, NoteContext, ToolBridge, TurnRequest}, provider::{self, Event}};

pub(super) fn stream(system: String, user: String, cancel: Arc<AtomicBool>, cx: &App) -> mpsc::Receiver<Event> {
    let mut config = BackendConfig::load();
    let recognized = super::chat::recognized_backends(cx);
    let kind = super::chat::entity(cx).map(|chat| chat.read(cx).selected_backend())
        .unwrap_or_else(|| config.ensure_valid_selection(&recognized));
    if kind == BackendKind::Builtin || !provider::config().enabled {
        return provider::stream(system, user, cancel);
    }
    let (sender, receiver) = mpsc::channel();
    let mut options = config.options(kind);
    options["fullPermissions"] = false.into();
    options["textOnly"] = true.into();
    static NEXT: AtomicU64 = AtomicU64::new(0);
    let id = format!("inline-{}-{}", std::process::id(), NEXT.fetch_add(1, Ordering::Relaxed));
    let request = TurnRequest {
        backend: kind, conversation_id: id.clone(), turn_id: id, native_session_id: None,
        message: format!("{system}\n\nReturn only the rewritten text. Do not use tools or edit files.\n\n{user}"),
        context: NoteContext::default(), config: options,
    };
    std::thread::spawn(move || {
        let fail = |message: String| { let _ = sender.send(Event::Failed { message, settings: false }); };
        let mut adapter = match backend::adapter(kind) { Ok(value) => value, Err(error) => { fail(error.message); return; } };
        let (events, incoming) = mpsc::channel();
        let (bridge, tools) = ToolBridge::channel();
        let mut running = match adapter.start(request, events, bridge.clone()) {
            Ok(value) => value, Err(error) => { fail(error.message); return; }
        };
        loop {
            if cancel.load(Ordering::Relaxed) { break; }
            while let Ok(tool) = tools.try_recv() {
                let _ = tool.responder.respond(Err(backend::BackendError::new("inline_text_only", "Return the rewritten text without tools.")));
            }
            let event = match incoming.recv_timeout(Duration::from_millis(40)) {
                Ok(event) => event.kind,
                Err(mpsc::RecvTimeoutError::Timeout) => continue,
                Err(mpsc::RecvTimeoutError::Disconnected) => { fail(crate::theme::tr("生成中断，未对正文进行修改", "Generation interrupted; text unchanged").into()); break; }
            };
            let output = match event {
                BackendEventKind::Delta(text) => Event::Delta(text),
                BackendEventKind::Thinking(text) => Event::Thinking(text),
                BackendEventKind::Completed => { let _ = sender.send(Event::Done); break; }
                BackendEventKind::Error(error) => { fail(error.message); break; }
                BackendEventKind::Cancelled => { fail(crate::theme::tr("生成已取消", "Generation cancelled").into()); break; }
                BackendEventKind::Approval { request_id, .. } | BackendEventKind::Questions { request_id, .. } => {
                    let _ = running.respond(&request_id, backend::ApprovalResponse::Deny);
                    continue;
                }
                _ => continue,
            };
            if sender.send(output).is_err() { break; }
        }
        bridge.cancel();
        let _ = running.cancel();
    });
    receiver
}
