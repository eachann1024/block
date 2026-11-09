//! Briefly reveal a narrow-window sidebar on launch or when entering Settings.
use gpui::{Context, Window};
use super::Workspace;

pub(super) struct SidebarIntro {
    pending: bool,
    pub(super) leaving: bool,
    task: Option<gpui::Task<()>>,
}

impl SidebarIntro {
    pub(super) fn pending() -> Self {
        Self { pending: true, leaving: false, task: None }
    }
}

impl Workspace {
    /// Wait for restored preferences and the actual layout before starting the delay.
    pub(super) fn advance_sidebar_intro(&mut self, drawer_open: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.loading { return; }
        if !drawer_open {
            self.cancel_sidebar_intro();
            return;
        }
        if !self.sidebar_intro.pending { return; }
        self.sidebar_intro.pending = false;
        self.sidebar_intro.task = Some(cx.spawn_in(window, async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_millis(650)).await;
            let leaving = this.update_in(cx, |this, window, cx| {
                if !this.shell_split(window, cx).sidebar_drawer { return false; }
                this.sidebar_intro.leaving = true;
                cx.notify();
                true
            }).unwrap_or(false);
            if !leaving { return; }
            cx.background_executor().timer(std::time::Duration::from_millis(crate::ui::motion::DRAWER_EXIT_MS)).await;
            let _ = this.update_in(cx, |this, _, cx| {
                this.sidebar_intro.leaving = false;
                this.sidebar_pinned = false;
                cx.notify();
            });
        }));
    }

    /// User input or a page change takes ownership and cancels any stale close.
    pub(super) fn cancel_sidebar_intro(&mut self) {
        self.sidebar_intro.task = None;
        self.sidebar_intro.pending = false;
        self.sidebar_intro.leaving = false;
    }
}
