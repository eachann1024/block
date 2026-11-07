use std::{cell::{Cell, RefCell}, collections::HashMap, sync::mpsc, time::Duration};
use gpui::{Context, Entity, EntityId, Window};
use gpui_component::input::TextareaState;
use super::AiChat;
use super::super::dictation::{Event, Process};

struct Session { generation: u64, _process: Option<Process> }
struct SessionGuard { id: EntityId, generation: u64 }
impl Drop for SessionGuard {
    fn drop(&mut self) { remove_session(self.id, self.generation); }
}
thread_local! {
    static ACTIVE: RefCell<HashMap<EntityId, Session>> = RefCell::new(HashMap::new());
    static NEXT: Cell<u64> = const { Cell::new(0) };
    static SMOKE_SENDERS: RefCell<HashMap<u64, mpsc::Sender<Event>>> = RefCell::new(HashMap::new());
}

pub(in crate::editor::ai) fn is_recording(id: EntityId) -> bool { ACTIVE.with(|active| active.borrow().contains_key(&id)) }

pub(in crate::editor::ai) fn stop_for_input(input: &Entity<TextareaState>) {
    ACTIVE.with(|active| { active.borrow_mut().remove(&input.entity_id()); });
}

fn matches_session(id: EntityId, generation: u64) -> bool {
    ACTIVE.with(|active| active.borrow().get(&id).is_some_and(|session| session.generation == generation))
}

fn remove_session(id: EntityId, generation: u64) {
    ACTIVE.with(|active| {
        let mut active = active.borrow_mut();
        if active.get(&id).is_some_and(|session| session.generation == generation) { active.remove(&id); }
    });
    SMOKE_SENDERS.with(|senders| { senders.borrow_mut().remove(&generation); });
}

fn error_message(code: &str) -> String {
    let (zh, en) = match code {
        "unsupported" => ("语音输入目前仅支持 macOS。", "Voice input is available only on macOS."),
        "helper_missing" => ("语音输入助手缺失，请重新安装 Block。", "Voice input helper is missing. Reinstall Block."),
        "speech_permission" => ("语音识别权限未开启，请在系统设置中允许。", "Speech recognition permission is denied. Allow it in System Settings."),
        "microphone_permission" => ("麦克风权限未开启，请在系统设置中允许。", "Microphone permission is denied. Allow it in System Settings."),
        "unavailable" => ("当前语言的语音识别不可用。", "Speech recognition is unavailable for this language."),
        "audio_unavailable" => ("无法启动麦克风。", "Could not start the microphone."),
        "recognition_failed" => ("语音识别失败，请重试。", "Speech recognition failed. Please try again."),
        "interrupted" => ("语音输入意外中断。", "Voice input stopped unexpectedly."),
        "edited" => ("输入内容已更改，语音输入已停止。", "Voice input stopped because the draft was edited."),
        _ => ("无法启动语音输入助手。", "Could not start the voice input helper."),
    };
    crate::theme::tr(zh, en).to_owned()
}

pub(in crate::editor::ai) fn toggle(chat: &mut AiChat, window: &mut Window, cx: &mut Context<AiChat>) {
    let id = chat.input.entity_id();
    if is_recording(id) { stop_for_input(&chat.input); cx.notify(); return; }
    if crate::smoke::is_requested() { let _ = smoke_start(chat, window, cx); return; }
    let (process, receiver) = match Process::start() {
        Ok(result) => result,
        Err(error) => { chat.error = Some(error_message(&error)); chat.error_settings = false; cx.notify(); return; }
    };
    start_session(chat, window, cx, Some(process), receiver);
}

fn start_session(chat: &mut AiChat, window: &mut Window, cx: &mut Context<AiChat>, process: Option<Process>, receiver: mpsc::Receiver<Event>) -> u64 {
    let id = chat.input.entity_id();
    let generation = NEXT.with(|next| { let generation = next.get().wrapping_add(1); next.set(generation); generation });
    ACTIVE.with(|active| { active.borrow_mut().insert(id, Session { generation, _process: process }); });
    chat.error = None;
    chat.error_settings = false;
    let mut rendered = chat.input.read(cx).value().to_string();
    let prefix = rendered.clone();
    let conversation = chat.current;
    let editor_context = chat.editor.as_ref().and_then(|editor| editor.upgrade()).map(|editor| {
        (editor.entity_id(), editor.read(cx).current_path().map(|path| path.to_path_buf()))
    });
    let guard = SessionGuard { id, generation };
    cx.spawn_in(window, async move |this, cx| { let _guard = guard; loop {
        cx.background_executor().timer(Duration::from_millis(40)).await;
        let mut finished = false;
        if this.update_in(cx, |chat, window, cx| {
            if !matches_session(id, generation) { finished = true; return; }
            let current_editor = chat.editor.as_ref().and_then(|editor| editor.upgrade()).map(|editor| {
                (editor.entity_id(), editor.read(cx).current_path().map(|path| path.to_path_buf()))
            });
            if chat.current != conversation || !chat.open || current_editor != editor_context { remove_session(id, generation); finished = true; cx.notify(); return; }
            if chat.input.read(cx).value().as_ref() != rendered {
                chat.error = Some(error_message("edited")); chat.error_settings = false;
                remove_session(id, generation); finished = true; cx.notify(); return;
            }
            let mut changed = false;
            loop {
                match receiver.try_recv() {
                    Ok(Event::Text(text)) => {
                        changed = true;
                        if chat.input.read(cx).value().as_ref() != rendered {
                            chat.error = Some(error_message("edited")); finished = true; break;
                        }
                        rendered = format!("{prefix}{text}");
                        chat.input.update(cx, |input, cx| { input.set_value(rendered.clone(), window, cx); input.set_selected_range(rendered.len()..rendered.len(), cx); });
                    }
                    Ok(Event::Error(code)) => { chat.error = Some(error_message(&code)); chat.error_settings = false; finished = true; break; }
                    Ok(Event::Done) => { finished = true; break; }
                    Err(mpsc::TryRecvError::Empty) => break,
                    Err(mpsc::TryRecvError::Disconnected) => { chat.error = Some(error_message("interrupted")); finished = true; break; }
                }
            }
            if finished { remove_session(id, generation); }
            if changed || finished { cx.notify(); }
        }).is_err() { remove_session(id, generation); break; }
        if finished { SMOKE_SENDERS.with(|senders| { senders.borrow_mut().remove(&generation); }); break; }
    }}).detach();
    cx.notify();
    generation
}

pub(crate) fn smoke_start(chat: &mut AiChat, window: &mut Window, cx: &mut Context<AiChat>) -> Result<u64, &'static str> {
    if !crate::smoke::is_requested() { return Err("dictation fixture requires smoke mode"); }
    let (sender, receiver) = mpsc::channel();
    let generation = start_session(chat, window, cx, None, receiver);
    SMOKE_SENDERS.with(|senders| { senders.borrow_mut().insert(generation, sender); });
    Ok(generation)
}

pub(crate) fn smoke_generation(id: EntityId) -> Option<u64> {
    if !crate::smoke::is_requested() { return None; }
    ACTIVE.with(|active| active.borrow().get(&id).map(|session| session.generation))
}

pub(crate) fn smoke_text(generation: u64, text: &str) -> bool {
    if !crate::smoke::is_requested() { return false; }
    SMOKE_SENDERS.with(|senders| senders.borrow().get(&generation).is_some_and(|sender| sender.send(Event::Text(text.to_owned())).is_ok()))
}

pub(crate) fn smoke_done(generation: u64) -> bool {
    if !crate::smoke::is_requested() { return false; }
    SMOKE_SENDERS.with(|senders| senders.borrow().get(&generation).is_some_and(|sender| sender.send(Event::Done).is_ok()))
}
