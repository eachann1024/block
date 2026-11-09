//! Left category navigation shown in place of the file tree while Settings is open.
use gpui::{Context, CursorStyle, FontWeight, MouseButton, SharedString, Window, div, prelude::*, px};
use crate::icons::stateful_icon;
use crate::theme::{Theme, tr};
use super::Workspace;
use super::settings::SettingsTab;

impl Workspace {
    /// Start only after the first Settings frame has resolved its actual layout.
    pub(super) fn settings_sidebar_intro(&mut self, drawer_open: bool, window: &mut Window, cx: &mut Context<Self>) {
        let Some(modal) = self.settings.as_mut() else { return; };
        if !drawer_open {
            self.cancel_settings_sidebar_intro();
            return;
        }
        if !modal.sidebar_intro_pending { return; }
        modal.sidebar_intro_pending = false;
        modal.sidebar_intro_task = Some(cx.spawn_in(window, async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_millis(650)).await;
            let leaving = this.update_in(cx, |this, window, cx| {
                if !this.shell_split(window, cx).sidebar_drawer || !this.sidebar_pinned { return false; }
                let Some(modal) = this.settings.as_mut() else { return false; };
                modal.sidebar_intro_leaving = true;
                cx.notify();
                true
            }).unwrap_or(false);
            if !leaving { return; }
            cx.background_executor().timer(std::time::Duration::from_millis(crate::ui::motion::DRAWER_EXIT_MS)).await;
            let _ = this.update_in(cx, |this, _, cx| {
                if let Some(modal) = this.settings.as_mut() {
                    modal.sidebar_intro_leaving = false;
                    this.sidebar_pinned = false;
                    cx.notify();
                }
            });
        }));
    }

    /// User input takes ownership; dropping the task also cancels any stale close.
    pub(super) fn cancel_settings_sidebar_intro(&mut self) {
        if let Some(modal) = self.settings.as_mut() {
            modal.sidebar_intro_task = None;
            modal.sidebar_intro_pending = false;
            modal.sidebar_intro_leaving = false;
        }
    }

    pub(super) fn settings_sidebar(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let current = self.settings.as_ref().map(|m| m.tab);
        div().w(self.sidebar_width).flex_none().h_full().relative().flex().flex_col()
            .bg(theme.sidebar).border_r_1().border_color(theme.sidebar_border)
            .child(div().flex_none().pt(px(18.)).px(px(16.)).pb(px(10.))
                .text_size(px(18.)).line_height(px(24.)).font_weight(FontWeight(560.)).text_color(theme.text)
                .child(tr("设置", "Settings")))
            .child(div().flex_1().min_h_0().px(px(10.)).flex().flex_col().gap(px(2.))
                .children(SettingsTab::ALL.into_iter().enumerate().map(|(ix, tab)| {
                    let active = current == Some(tab);
                    let cat_group = SharedString::from(format!("settings-cat-{ix}"));
                    div().id(("settings-tab", ix)).group(cat_group.clone()).relative().w_full().h(px(34.)).flex_none().px(px(10.)).rounded(crate::ui::radius::CONTROL).flex().items_center().gap(px(8.))
                        .when(tab == SettingsTab::Folders, |d| d.children(crate::ui::smoke_overlay::probe("settings-tab-folders")))
                        .border_1().border_color(gpui::transparent_black())
                        .text_size(px(14.)).line_height(px(20.)).cursor(CursorStyle::PointingHand)
                        .when(active, |d| d.bg(theme.selected).border_color(theme.accent)
                            .text_color(theme.text).font_weight(FontWeight(460.)))
                        .when(!active, |d| d.text_color(theme.text_secondary)
                            .hover(move |s| s.bg(theme.hover).text_color(theme.text)))
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                            if this.shell_split(window, cx).sidebar_drawer { this.sidebar_pinned = false; cx.notify(); }
                            this.select_settings_tab(tab, cx);
                        }))
                        .child(stateful_icon(cat_group, tab.icon(), px(15.), active, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if active { theme.accent } else { theme.text })))
                        .child(tab.title())
                })))
            .child(self.resize_handle(cx))
    }
}
