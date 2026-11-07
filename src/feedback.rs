//! User feedback modal state, draft persistence, and submission to feedback-api.cooool.top.
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Mutex, OnceLock};
use gpui::{App, AppContext as _, Context, Entity, FocusHandle, Subscription, Window};
use gpui_component::input::{InputEvent, InputState, TextareaState};
use crate::files::{FeedbackDraft, load_state, update_state};
use crate::theme::tr;

const FEEDBACK_ENDPOINT: &str = "https://feedback-api.cooool.top/api/feedback";
static OPEN: AtomicBool = AtomicBool::new(false);
static SEQ: AtomicU64 = AtomicU64::new(0);
static HAS_DRAFT: AtomicBool = AtomicBool::new(false);
static DRAFT_INIT: OnceLock<()> = OnceLock::new();
static MODAL: OnceLock<Mutex<Option<Entity<FeedbackModal>>>> = OnceLock::new();

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum FeedbackCategory { #[default] Bug, Idea, Other }

impl FeedbackCategory {
    pub const ALL: [Self; 3] = [Self::Bug, Self::Idea, Self::Other];
    pub fn code(self) -> &'static str {
        match self { Self::Bug => "bug", Self::Idea => "idea", Self::Other => "other" }
    }
    pub fn from_code(raw: &str) -> Self {
        match raw { "idea" => Self::Idea, "other" | "ask" => Self::Other, _ => Self::Bug }
    }
    pub fn label(self) -> &'static str {
        match self {
            Self::Bug => tr("问题反馈", "Bug"),
            Self::Idea => tr("功能建议", "Feature idea"),
            Self::Other => tr("其他", "Other"),
        }
    }
    pub fn placeholder(self) -> &'static str {
        match self {
            Self::Bug => tr("哪里出了问题？怎么操作会出现？", "What went wrong, and how can we make it happen again?"),
            Self::Idea => tr("希望加什么功能，或者哪里可以更好用？", "What would you like added, or made easier?"),
            Self::Other => tr("有什么想法或疑问，随便聊聊。", "Anything on your mind — questions, thoughts, or notes."),
        }
    }
}

pub fn contact_placeholder() -> &'static str { tr("留了邮箱，回复会发到这里", "We reply here if you leave one") }

pub struct FeedbackModal {
    pub focus: FocusHandle,
    pub category: FeedbackCategory,
    pub message: Entity<TextareaState>,
    pub contact: Entity<InputState>,
    pub sending: bool,
    pub done: bool,
    pub sent_contact: String,
    pub error: Option<String>,
    pub chip_seq: u64,
    pub done_seq: u64,
    pub syncing: bool,
    pub last_en: bool,
    _subs: Vec<Subscription>,
}

impl FeedbackModal {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let saved = load_state().ok().and_then(|s| s.feedback_draft).unwrap_or_default();
        let category = FeedbackCategory::from_code(&saved.category);
        HAS_DRAFT.store(!saved.message.trim().is_empty() || !saved.contact.trim().is_empty(), Ordering::Relaxed);
        let message = cx.new(|cx| TextareaState::new(window, cx).auto_grow(4, 8).placeholder(category.placeholder()).default_value(saved.message));
        let contact = cx.new(|cx| InputState::new(window, cx).placeholder(contact_placeholder()).default_value(saved.contact));
        let sub_msg = cx.subscribe(&message, |this, _, ev: &InputEvent, cx| {
            if matches!(ev, InputEvent::Change) && !this.syncing { this.error = None; this.persist_draft(cx); cx.notify(); }
        });
        let sub_contact = cx.subscribe(&contact, |this, _, ev: &InputEvent, cx| {
            if matches!(ev, InputEvent::Change) && !this.syncing { this.error = None; this.persist_draft(cx); cx.notify(); }
        });
        Self { focus: cx.focus_handle(), category, message, contact, sending: false, done: false, sent_contact: String::new(), error: None, chip_seq: 0, done_seq: 0, syncing: false, last_en: crate::theme::is_en(), _subs: vec![sub_msg, sub_contact] }
    }

    pub fn sync_placeholders(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.last_en = crate::theme::is_en();
        let ph = self.category.placeholder();
        self.message.update(cx, |m, cx| m.set_placeholder(ph, window, cx));
        self.contact.update(cx, |c, cx| c.set_placeholder(contact_placeholder(), window, cx));
        let words = tr("写几个字再发送吧。", "Write a few words first.");
        let email = tr("邮箱格式不太对，检查一下？", "That email doesn't look right.");
        self.error = match self.error.take() {
            Some(err) if err == "写几个字再发送吧。" || err == "Write a few words first." => Some(words.into()),
            Some(err) if err == "邮箱格式不太对，检查一下？" || err == "That email doesn't look right." => Some(email.into()),
            other => other,
        };
    }

    pub fn select_category(&mut self, cat: FeedbackCategory, window: &mut Window, cx: &mut Context<Self>) {
        if self.category != cat {
            self.category = cat;
            self.chip_seq = self.chip_seq.wrapping_add(1);
            let ph = cat.placeholder();
            self.message.update(cx, |m, cx| m.set_placeholder(ph, window, cx));
            self.persist_draft(cx);
            cx.notify();
        }
    }

    pub fn persist_draft(&self, cx: &App) {
        let msg = self.message.read(cx).value().to_string();
        let contact = self.contact.read(cx).value().to_string();
        let non_empty = !msg.trim().is_empty() || !contact.trim().is_empty();
        HAS_DRAFT.store(non_empty, Ordering::Relaxed);
        let draft = (non_empty || self.category != FeedbackCategory::Bug).then(|| FeedbackDraft {
            category: self.category.code().into(),
            platform: String::new(),
            message: msg,
            contact,
        });
        let _ = update_state(move |state| state.feedback_draft = draft);
    }

    pub fn submit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.sending { return; }
        let msg = self.message.read(cx).value().trim().to_string();
        if msg.is_empty() {
            self.error = Some(tr("写几个字再发送吧。", "Write a few words first.").into());
            self.message.update(cx, |m, cx| m.focus(window, cx));
            cx.notify();
            return;
        }
        let email = self.contact.read(cx).value().trim().to_string();
        if !email.is_empty() && !valid_email(&email) {
            self.error = Some(tr("邮箱格式不太对，检查一下？", "That email doesn't look right.").into());
            self.contact.update(cx, |c, cx| c.focus(window, cx));
            cx.notify();
            return;
        }
        self.sending = true;
        self.error = None;
        cx.notify();
        let category = self.category.code().to_string();
        let sent_email = email.clone();
        let rx = cx.background_executor().spawn(async move { send_feedback_http(&category, &msg, &email) });
        cx.spawn_in(window, async move |this, cx| {
            let res = rx.await;
            let _ = this.update_in(cx, |this, window, cx| {
                this.sending = false;
                match res {
                    Ok(()) => {
                        this.done = true;
                        this.done_seq = this.done_seq.wrapping_add(1);
                        this.sent_contact = sent_email;
                        this.error = None;
                        this.syncing = true;
                        this.category = FeedbackCategory::Bug;
                        let ph = this.category.placeholder();
                        this.message.update(cx, |m, cx| { m.set_value("", window, cx); m.set_placeholder(ph, window, cx); });
                        this.syncing = false;
                        window.focus(&this.focus, cx);
                        HAS_DRAFT.store(false, Ordering::Relaxed);
                        let _ = update_state(|state| state.feedback_draft = None);
                    }
                    Err(err) => this.error = Some(err),
                }
                cx.notify();
            });
        }).detach();
    }
}

fn valid_email(s: &str) -> bool {
    let Some((local, rest)) = s.split_once('@') else { return false };
    let Some((domain, tld)) = rest.rsplit_once('.') else { return false };
    !local.is_empty() && !domain.is_empty() && !tld.is_empty() && !s.chars().any(char::is_whitespace)
}

fn send_feedback_http(category: &str, message: &str, contact: &str) -> Result<(), String> {
    if crate::smoke::is_requested() || std::env::var("BLOCK_MOCK_FEEDBACK").ok().as_deref() == Some("1") { return Ok(()); }
    let fallback = || tr("没发出去。过一会儿再试一次。", "That didn't send. Please try again in a moment.").to_string();
    let mut cmd = std::process::Command::new("curl");
    cmd.args([
        "-sS", "-m", "15", "-w", "\n%{http_code}", "-X", "POST", FEEDBACK_ENDPOINT,
        "--form-string", "project=block",
        "--form-string", &format!("category={category}"),
        "--form-string", &format!("message={message}"),
        "--form-string", &format!("contact={contact}"),
        "--form-string", "leave_blank=",
    ]);
    let out = cmd.output().map_err(|_| fallback())?;
    let raw = String::from_utf8_lossy(&out.stdout);
    let (body, code_str) = raw.rsplit_once('\n').unwrap_or(("", raw.trim()));
    if (200..300).contains(&code_str.trim().parse::<u16>().unwrap_or(0)) {
        Ok(())
    } else {
        Err(serde_json::from_str::<serde_json::Value>(body).ok().and_then(|v| v.get("error")?.as_str().map(str::to_string)).unwrap_or_else(fallback))
    }
}

fn modal_slot() -> &'static Mutex<Option<Entity<FeedbackModal>>> { MODAL.get_or_init(|| Mutex::new(None)) }

pub fn ensure_modal(window: &mut Window, cx: &mut App) -> Entity<FeedbackModal> {
    let mut guard = modal_slot().lock().unwrap_or_else(|e| e.into_inner());
    guard.get_or_insert_with(|| cx.new(|cx| FeedbackModal::new(window, cx))).clone()
}

pub fn is_open() -> bool { OPEN.load(Ordering::Relaxed) }
pub fn open_seq() -> u64 { SEQ.load(Ordering::Relaxed) }

pub fn has_draft() -> bool {
    DRAFT_INIT.get_or_init(|| {
        let has = load_state().ok().and_then(|s| s.feedback_draft).is_some_and(|d| !d.message.trim().is_empty() || !d.contact.trim().is_empty());
        HAS_DRAFT.store(has, Ordering::Relaxed);
    });
    HAS_DRAFT.load(Ordering::Relaxed)
}

pub fn open(window: &mut Window, cx: &mut App) {
    let entity = ensure_modal(window, cx);
    OPEN.store(true, Ordering::Relaxed);
    SEQ.fetch_add(1, Ordering::Relaxed);
    entity.update(cx, |m, cx| {
        if m.done { m.done = false; m.error = None; }
        m.sync_placeholders(window, cx);
        m.message.update(cx, |input, cx| input.focus(window, cx));
        cx.notify();
    });
    window.refresh();
}

pub fn sync_language(window: &mut Window, cx: &mut App) {
    let Some(slot) = MODAL.get() else { return };
    let Some(entity) = slot.lock().unwrap_or_else(|e| e.into_inner()).as_ref().cloned() else { return };
    entity.update(cx, |m, cx| { m.sync_placeholders(window, cx); cx.notify(); });
}

pub fn toggle(window: &mut Window, cx: &mut App) {
    if is_open() { let _ = close(window, cx); } else { open(window, cx); }
}

pub fn close(window: &mut Window, cx: &mut App) -> bool {
    if !OPEN.swap(false, Ordering::Relaxed) { return false; }
    if let Some(entity) = modal_slot().lock().unwrap_or_else(|e| e.into_inner()).as_ref().cloned() {
        entity.update(cx, |m, cx| if !m.done { m.persist_draft(cx); });
    }
    window.refresh();
    true
}
