//! Full-window Settings page: header, single-tab body switch and the titlebar breadcrumb.
use gpui::{
    AnyElement, Context, FontWeight, KeyDownEvent, MouseButton, WindowControlArea, div, prelude::*, px,
};
use crate::theme::{Theme, tr};
use super::Workspace;
use super::settings::{SettingsTab, keystroke_to_spec};

impl Workspace {
    /// Titlebar content while Settings is open: `设置 / <current tab>`.
    pub(super) fn settings_breadcrumb(&self, bar: gpui::Stateful<gpui::Div>, tab: SettingsTab) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        bar.px(px(16.)).gap(px(6.)).window_control_area(WindowControlArea::Drag)
            .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); })
            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(tr("设置", "Settings")))
            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child("/"))
            .child(div().text_size(px(14.)).font_weight(FontWeight(460.)).text_color(theme.text).child(tab.title()))
    }

    pub(super) fn settings_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let modal = self.settings.as_ref()?;
        let theme = Theme::ocean();
        let tab = modal.tab;
        Some(div().id("settings-page").track_focus(&modal.focus).flex_1().min_w_0().h_full()
            .overflow_y_scroll().bg(theme.settings_bg).text_color(theme.text)
            .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                if let Some(m) = &mut this.settings {
                    if m.recording.take().is_some() { cx.notify(); }
                }
            }))
            .on_key_down(cx.listener(|this, event: &KeyDownEvent, window, cx| {
                let Some(kind) = this.settings.as_ref().and_then(|m| m.recording) else {
                    if event.keystroke.key == "escape" {
                        cx.stop_propagation();
                        if this.close_font_picker(cx) { return; }
                        this.close_settings(window, cx);
                    }
                    return;
                };
                cx.stop_propagation();
                if event.keystroke.key == "escape" && !event.keystroke.modifiers.modified() {
                    if let Some(m) = &mut this.settings { m.recording = None; }
                    cx.notify();
                    return;
                }
                if let Some(spec) = keystroke_to_spec(&event.keystroke) {
                    this.apply_shortcut(kind, spec, cx);
                }
            }))
            .child(div().w_full().max_w(px(720.)).mx_auto().pt(px(40.)).px(px(28.)).pb(px(64.))
                .flex().flex_col().gap(px(28.))
                .child(div().text_size(px(24.)).line_height(px(32.)).font_weight(FontWeight(560.))
                    .text_color(theme.text).child(tab.title()))
                .child(self.settings_body(tab, cx)))
            .into_any_element())
    }

    fn settings_body(&self, tab: SettingsTab, cx: &mut Context<Self>) -> AnyElement {
        match tab {
            SettingsTab::Ai => div().children(crate::editor::ai::settings::section(cx)).into_any_element(),
            SettingsTab::Appearance => self.appearance_tab(cx).into_any_element(),
            SettingsTab::Folders => self.folders_tab(cx).into_any_element(),
            SettingsTab::Shortcuts => self.shortcuts_tab(cx).into_any_element(),
        }
    }
}
