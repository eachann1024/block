//! Multi-tab state management and `Tabs-1-Titlebar` top bar rendering.
use std::path::{Path, PathBuf};
use std::time::Duration;
use gpui::{
    Animation, AnimationExt, App, Context, CursorStyle, FontWeight, KeyBinding,
    MouseButton, SharedString, WindowControlArea, actions, div, ease_out_quint,
    prelude::*, px, transparent_black,
};
use crate::icons::Icon;
use crate::theme::Theme;
use super::{Workspace, file_ops::display_name, search::Dismiss};

#[path = "app_tabs_menu.rs"]
mod tab_menu;
#[path = "app_tabs_reopen.rs"]
mod reopen;
#[path = "app_tabs_annotations.rs"]
mod annotations;

pub(super) use reopen::ClosedTabs;
pub(super) use tab_menu::{clear_tab_menu, hover_tinted_icon, record_tab_open};
use tab_menu::{
    TAB_ANIM_MS, TAB_BASIS_W, TAB_FADE_W, TAB_MAX_W, TAB_MIN_W, closing_ghost,
    opening_seq, record_tab_close, render_closing_ghost,
    render_inactive_tab_body, tab_fade,
};

pub(super) const TITLEBAR_HEIGHT: f32 = 40.;
const SINGLE_TAB_MAX_W: f32 = 240.;

actions!(workspace_tabs, [
    NewTab, CloseTab, ReopenClosedTab, NextTab, PrevTab, ToggleSidebar,
    SelectTab1, SelectTab2, SelectTab3, SelectTab4, SelectTab5,
    SelectTab6, SelectTab7, SelectTab8, SelectTab9
]);

pub(crate) fn bind_keys(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("cmd-t", NewTab, Some("Workspace")), KeyBinding::new("cmd-w", CloseTab, Some("Workspace")),
        KeyBinding::new("cmd-shift-t", ReopenClosedTab, Some("Workspace")), KeyBinding::new("cmd-\\", ToggleSidebar, Some("Workspace")),
        KeyBinding::new("ctrl-tab", NextTab, Some("Workspace")), KeyBinding::new("ctrl-pagedown", NextTab, Some("Workspace")),
        KeyBinding::new("cmd-shift-]", NextTab, Some("Workspace")), KeyBinding::new("cmd-alt-right", NextTab, Some("Workspace")),
        KeyBinding::new("ctrl-shift-tab", PrevTab, Some("Workspace")), KeyBinding::new("ctrl-pageup", PrevTab, Some("Workspace")),
        KeyBinding::new("cmd-shift-[", PrevTab, Some("Workspace")), KeyBinding::new("cmd-alt-left", PrevTab, Some("Workspace")),
        KeyBinding::new("cmd-1", SelectTab1, Some("Workspace")), KeyBinding::new("cmd-2", SelectTab2, Some("Workspace")),
        KeyBinding::new("cmd-3", SelectTab3, Some("Workspace")), KeyBinding::new("cmd-4", SelectTab4, Some("Workspace")),
        KeyBinding::new("cmd-5", SelectTab5, Some("Workspace")), KeyBinding::new("cmd-6", SelectTab6, Some("Workspace")),
        KeyBinding::new("cmd-7", SelectTab7, Some("Workspace")), KeyBinding::new("cmd-8", SelectTab8, Some("Workspace")),
        KeyBinding::new("cmd-9", SelectTab9, Some("Workspace")),
    ]);
    #[cfg(not(target_os = "macos"))]
    cx.bind_keys([
        KeyBinding::new("ctrl-t", NewTab, Some("Workspace")), KeyBinding::new("ctrl-w", CloseTab, Some("Workspace")),
        KeyBinding::new("ctrl-shift-t", ReopenClosedTab, Some("Workspace")), KeyBinding::new("ctrl-\\", ToggleSidebar, Some("Workspace")),
        KeyBinding::new("ctrl-1", SelectTab1, Some("Workspace")), KeyBinding::new("ctrl-2", SelectTab2, Some("Workspace")),
        KeyBinding::new("ctrl-3", SelectTab3, Some("Workspace")), KeyBinding::new("ctrl-4", SelectTab4, Some("Workspace")),
        KeyBinding::new("ctrl-5", SelectTab5, Some("Workspace")), KeyBinding::new("ctrl-6", SelectTab6, Some("Workspace")),
        KeyBinding::new("ctrl-7", SelectTab7, Some("Workspace")), KeyBinding::new("ctrl-8", SelectTab8, Some("Workspace")),
        KeyBinding::new("ctrl-9", SelectTab9, Some("Workspace")),
    ]);
}

impl Workspace {
    pub(crate) fn new_tab(&mut self, window: &mut gpui::Window, cx: &mut Context<Self>) {
        clear_tab_menu();
        if !self.busy() { self.new_item(false, window, cx); }
    }
    pub(super) fn bind_tab_actions(&self, root: gpui::Div, cx: &mut Context<Self>) -> gpui::Div {
        root.on_action(cx.listener(|this, _: &Dismiss, _, cx| if !this.dismiss_tab_menu(cx) { cx.propagate(); }))
            .on_action(cx.listener(|this, _: &ToggleSidebar, _, cx| this.toggle_sidebar(cx)))
            .on_action(cx.listener(|this, _: &NewTab, window, cx| this.new_tab(window, cx)))
            .on_action(cx.listener(|this, _: &CloseTab, window, cx| this.close_active_tab(window, cx)))
            .on_action(cx.listener(|this, _: &ReopenClosedTab, _, cx| this.reopen_closed_tab(cx)))
            .on_action(cx.listener(|this, _: &NextTab, _, cx| this.step_tab(1, cx)))
            .on_action(cx.listener(|this, _: &PrevTab, _, cx| this.step_tab(-1, cx)))
            .on_action(cx.listener(|this, _: &super::nav::NavigateBack, _, cx| this.navigate_back(cx)))
            .on_action(cx.listener(|this, _: &super::nav::NavigateForward, _, cx| this.navigate_forward(cx)))
            .on_action(cx.listener(|this, _: &SelectTab1, _, cx| this.select_tab_index(0, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab2, _, cx| this.select_tab_index(1, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab3, _, cx| this.select_tab_index(2, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab4, _, cx| this.select_tab_index(3, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab5, _, cx| this.select_tab_index(4, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab6, _, cx| this.select_tab_index(5, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab7, _, cx| this.select_tab_index(6, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab8, _, cx| this.select_tab_index(7, false, cx)))
            .on_action(cx.listener(|this, _: &SelectTab9, _, cx| this.select_tab_index(8, true, cx)))
    }
    pub(crate) fn open_file(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.open_file_with_mode(path, false, true, cx);
    }
    pub(crate) fn close_tab(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        clear_tab_menu();
        if !self.prepare_close_tab(&path, cx) { return; }
        if self.busy() { return; }
        let Some(idx) = self.tabs.iter().position(|p| p == &path) else { return };
        if self.tabs.len() > 1 { record_tab_close(idx, display_name(&path, false), cx); }
        self.tabs.remove(idx);
        self.closed_tabs.push(path.clone(), idx);
        let was_active = self.selected.as_ref() == Some(&path) || self.editor.read(cx).current_path() == Some(path.as_path());
        if was_active {
            if let Some(next_path) = self.tabs.get(idx.min(self.tabs.len().saturating_sub(1))).cloned() {
                self.selected = Some(next_path.clone());
                self.tree.update(cx, |tree, cx| tree.select(Some(next_path.clone()), cx));
                self.editor.update(cx, |editor, cx| { editor.open(next_path, cx); editor.close_session(&path, cx); });
            } else {
                self.editor.update(cx, |editor, cx| editor.close_session(&path, cx));
                self.selected = None;
                self.tree.update(cx, |tree, cx| tree.select(None, cx));
            }
        } else {
            self.editor.update(cx, |editor, cx| editor.close_session(&path, cx));
        }
        self.save_tabs_state(cx);
        cx.notify();
    }
    pub(crate) fn close_active_tab(&mut self, window: &mut gpui::Window, cx: &mut Context<Self>) {
        if self.tabs.len() > 1 {
            if let Some(active) = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(Path::to_path_buf)) {
                self.close_tab(active, cx);
            }
        } else {
            crate::window_close::close(window, &self.editor, cx);
        }
    }
    pub(crate) fn step_tab(&mut self, delta: isize, cx: &mut Context<Self>) {
        if self.busy() || self.tabs.len() < 2 { return; }
        let cur = self.selected.as_ref().and_then(|s| self.tabs.iter().position(|p| p == s)).unwrap_or(0);
        let next = (cur as isize + delta).rem_euclid(self.tabs.len() as isize) as usize;
        self.open_file(self.tabs[next].clone(), cx);
    }
    pub(super) fn select_tab_index(&mut self, index: usize, last_if_nine: bool, cx: &mut Context<Self>) {
        if self.busy() || self.tabs.is_empty() { return; }
        let target = if last_if_nine { self.tabs.len() - 1 } else if index < self.tabs.len() { index } else { return };
        self.open_file(self.tabs[target].clone(), cx);
    }
    pub(super) fn reconcile_tabs_on_rename(&mut self, from: &Path, to: &Path, cx: &mut Context<Self>) {
        let mut updated = Vec::with_capacity(self.tabs.len());
        for tab in self.tabs.drain(..) {
            let next = crate::files::ops::rebase(&tab, from, to).unwrap_or(tab);
            if !updated.contains(&next) { updated.push(next); }
        }
        self.tabs = updated;
        self.closed_tabs.rebase(from, to);
        self.nav.rebase(from, to);
        if let Some(rebased) = self.selected.as_ref().and_then(|sel| crate::files::ops::rebase(sel, from, to)) {
            self.selected = Some(rebased.clone());
            self.tree.update(cx, |tree, cx| tree.select(Some(rebased), cx));
        }
        self.editor.update(cx, |editor, cx| editor.rename_session(from, to, cx));
        self.save_tabs_state(cx);
    }
    pub(super) fn remove_tabs_under(&mut self, prefix: &Path, cx: &mut Context<Self>) -> Option<PathBuf> {
        clear_tab_menu();
        self.closed_tabs.remove_under(prefix);
        self.nav.remove_under(prefix);
        let active_idx = self.selected.as_ref().and_then(|s| self.tabs.iter().position(|p| p == s));
        let active_removed = self.selected.as_ref().is_some_and(|s| s.starts_with(prefix))
            || self.editor.read(cx).current_path().is_some_and(|p| p.starts_with(prefix));
        let removed: Vec<PathBuf> = self.tabs.iter().filter(|t| t.starts_with(prefix)).cloned().collect();
        self.tabs.retain(|t| !t.starts_with(prefix));
        self.editor.update(cx, |editor, cx| for tab in &removed { editor.close_session(tab, cx); });
        self.save_tabs_state(cx);
        if !active_removed { self.selected.clone() }
        else if self.tabs.is_empty() { self.selected = None; self.tree.update(cx, |tree, cx| tree.select(None, cx)); None }
        else { Some(self.tabs[active_idx.unwrap_or(0).min(self.tabs.len() - 1)].clone()) }
    }

    pub(super) fn dirty_mask(&self, cx: &Context<Self>) -> u64 {
        let editor = self.editor.read(cx);
        self.tabs.iter().enumerate().fold(0u64, |acc, (i, p)| acc | ((editor.is_path_dirty(p) as u64) << (i & 63)))
    }

    pub(super) fn save_tabs_state(&self, cx: &mut Context<Self>) {
        let tabs = self.tabs.clone();
        let active = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(Path::to_path_buf));
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(|state| { state.open_tabs = tabs; state.active_tab = active; });
        }).detach();
    }

    pub(super) fn titlebar(&self, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        if self.is_history_open() { return self.render_history_titlebar(cx); }
        let theme = Theme::ocean();
        let tab_count = self.tabs.len();
        let can_create = self.ui_enabled();
        let left = self.titlebar_left(cx);
        let mut right = div().id("titlebar-tabs").flex_1().min_w_0().h_full().flex().items_center()
            .gap(px(4.)).px(px(10.)).overflow_hidden();
        if let Some(modal) = &self.settings {
            right = self.settings_breadcrumb(right, modal.tab);
        } else if tab_count == 0 {
            let title = self.folder.as_ref().map(|p| display_name(p, true)).unwrap_or_else(|| "Block".into());
            right = right.window_control_area(WindowControlArea::Drag)
                .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); })
                .child(div().overflow_hidden().whitespace_nowrap()
                    .text_size(px(13.)).line_height(px(18.)).text_color(theme.text_secondary).child(title));
        } else {
            let multi = tab_count > 1;
            let ghost = closing_ghost();
            let editor = self.editor.read(cx);
            let items: Vec<_> = self.tabs.iter().enumerate().map(|(ix, path)| {
                (ix, path.clone(), display_name(path, false), !multi || self.selected.as_ref() == Some(path), editor.is_path_dirty(path))
            }).collect();
            let mut strip = div().id("titlebar-tab-strip").h_full().min_w_0().flex_shrink_1()
                .flex().items_center().gap(px(4.)).overflow_x_scroll();
            for (ix, path, label, active, dirty) in items {
                if let Some(ref g) = ghost.as_ref().filter(|g| g.index == ix) {
                    strip = strip.child(render_closing_ghost(g));
                }
                strip = strip.child(self.render_tab_pill(ix, path, label, active, multi, dirty, cx));
            }
            if let Some(ref g) = ghost.as_ref().filter(|g| g.index >= tab_count) {
                strip = strip.child(render_closing_ghost(g));
            }
            right = right.child(strip);
            if multi && can_create {
                let new_group: SharedString = "tab-new-btn".into();
                right = right.child(
                    div().id("tab-new").group(new_group.clone()).size(px(26.)).flex_none()
                        .flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL).cursor(CursorStyle::PointingHand)
                        .hover(|s| s.bg(theme.hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                            cx.stop_propagation();
                            this.new_item(false, window, cx);
                        }))
                        .child(hover_tinted_icon(new_group, Icon::Plus, 14., theme.text_secondary, theme.text, theme.icon_fill))
                );
            }
            right = right.child(div().id("titlebar-drag-spacer").flex_1().min_w(px(16.)).h_full()
                .window_control_area(WindowControlArea::Drag)
                .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); }));
        }
        div().id("titlebar").h(px(TITLEBAR_HEIGHT)).w_full().flex_none().flex().items_center()
            .bg(theme.shell).border_b_1().border_color(theme.sidebar_border)
            .child(left).child(right)
            .child(self.render_titlebar_tail(self.settings.is_none() && tab_count > 0, cx))
            .children(self.render_tab_context_menu(cx))
    }

    fn render_tab_pill(
        &self, ix: usize, path: PathBuf, label: String, active: bool, multi: bool, dirty: bool, cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let theme = Theme::ocean();
        let group = SharedString::from(format!("tab-{ix}"));
        let close_group = SharedString::from(format!("tab-close-{ix}"));
        let (open_path, mid_path, menu_path, close_path) = (path.clone(), path.clone(), path.clone(), path.clone());
        let anim_seq = if multi { opening_seq(&path) } else { None };
        let close_btn = div().id(("tab-close", ix)).group(close_group.clone()).size(px(18.)).flex_none()
            .flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM).hover(|s| s.bg(theme.selection))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                this.close_tab(close_path.clone(), cx);
            }))
            .child(hover_tinted_icon(close_group, Icon::Close, 12., theme.text_secondary, theme.text, theme.icon_fill));
        let mut pill = div().id(("tab", ix)).group(group.clone()).h(px(28.)).overflow_hidden()
            .when(multi, |t| t.w(px(TAB_MAX_W)).flex_basis(px(TAB_BASIS_W)).flex_shrink_1().min_w(px(TAB_MIN_W)).max_w(px(TAB_MAX_W)).cursor(CursorStyle::PointingHand))
            .when(!multi, |t| t.min_w_0().max_w(px(SINGLE_TAB_MAX_W)).flex_shrink_1().text_color(theme.text)
                .window_control_area(WindowControlArea::Drag).on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); }))
            .pl(px(10.)).pr(px(if multi { 6. } else { 10. })).rounded(crate::ui::radius::CONTROL).flex().items_center().relative().when(ix == 0, |t| t.children(crate::ui::smoke_overlay::probe("tab-0")))
            .text_size(px(13.)).line_height(px(18.)).font_weight(FontWeight(460.))
            .when(multi && active, |t| t.bg(theme.selected).border_1().border_color(theme.accent).text_color(theme.text))
            .when(multi && !active, |t| t.border_1().border_color(transparent_black()).text_color(theme.text_secondary)
                .hover(|s| s.bg(theme.hover).text_color(theme.text)))
            .when(multi, |t| t
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| if !this.busy() {
                    this.close_settings(window, cx);
                    this.open_file(open_path.clone(), cx);
                }))
                .on_mouse_down(MouseButton::Middle, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.close_tab(mid_path.clone(), cx);
                }))
                .on_mouse_down(MouseButton::Right, cx.listener(move |this, event: &gpui::MouseDownEvent, _, cx| {
                    cx.stop_propagation();
                    this.open_tab_menu(ix, menu_path.clone(), event.position, cx);
                })));
        pill = if multi && active {
            pill.child(div().relative().flex_1().min_w_0().h_full().flex().items_center().overflow_hidden()
                .child(div().whitespace_nowrap().child(label))
                .child(tab_fade(theme.selected).absolute().top_0().bottom_0().right_0()))
                .when(dirty, |t| t.child(div().mr(px(4.)).size(px(6.)).flex_none().rounded_full().bg(theme.accent)))
                .child(close_btn)
        } else if multi {
            pill.child(render_inactive_tab_body(label, group, dirty, close_btn))
        } else {
            let single_fade_x = SINGLE_TAB_MAX_W - 20. - TAB_FADE_W;
            pill.child(div().relative().flex_shrink_1().min_w_0().h_full().flex().items_center().overflow_hidden()
                .child(div().whitespace_nowrap().child(label))
                .child(tab_fade(theme.shell).absolute().top_0().bottom_0().left(px(single_fade_x))))
                .when(dirty, |t| t.child(div().ml(px(6.)).size(px(6.)).flex_none().rounded_full().bg(theme.accent)))
        };
        if let Some(seq) = anim_seq {
            pill.with_animation(("tab-open-anim", seq), Animation::new(Duration::from_millis(TAB_ANIM_MS)).with_easing(ease_out_quint()), |el, d| {
                el.w(px(TAB_MAX_W * d)).flex_basis(px(TAB_BASIS_W * d)).min_w(px(TAB_MIN_W * d)).max_w(px(TAB_MAX_W * d)).opacity(d)
            }).into_any_element()
        } else {
            pill.into_any_element()
        }
    }
}
