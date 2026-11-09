//! Tab context menu (batch close) and open/close width animations for `Tabs-1-Titlebar`.
use std::cell::RefCell;
use std::path::{Path, PathBuf};
use std::time::Duration;
use gpui::{
    Animation, AnimationExt, AnyElement, Context, Hsla, IntoElement,
    MouseButton, Pixels, Point, SharedString, anchored, deferred, div, ease_out_quint, point,
    linear_color_stop, linear_gradient, prelude::*, px, transparent_black,
};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::{Theme, tr};
use super::Workspace;

pub(super) const TAB_ANIM_MS: u64 = 150;
pub(super) const TAB_BASIS_W: f32 = 220.;
pub(super) const TAB_MIN_W: f32 = 76.;
pub(super) const TAB_MAX_W: f32 = 220.;
pub(super) const TAB_FADE_W: f32 = 20.;

#[derive(Clone)]
struct TabMenu { index: usize, path: PathBuf, position: Point<Pixels> }

#[derive(Clone)]
struct TabMenuSnap { menu: Option<TabMenu>, page: Option<Point<Pixels>> }

#[derive(Clone)]
pub(super) struct ClosingGhost { pub index: usize, pub label: String, pub seq: u64 }

#[derive(Default)]
struct TabUiState {
    menu: Option<TabMenu>, page_menu: Option<Point<Pixels>>,
    tab_cursor: usize, page_cursor: usize,
    opening: Option<(PathBuf, u64)>, closing: Option<ClosingGhost>, seq: u64,
}

thread_local! {
    static TAB_UI: RefCell<TabUiState> = RefCell::new(TabUiState::default());
}

pub(crate) fn clear_tab_menu() {
    TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.menu = None; s.page_menu = None; s.opening = None; });
}

pub(super) fn is_page_menu_open() -> bool {
    TAB_UI.with(|ui| ui.borrow().page_menu.is_some())
}

thread_local! { pub(super) static PAGE_TRIGGER: std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<Pixels>>>> = Default::default(); }

pub(super) fn toggle_page_menu(anchor: Point<Pixels>) {
    TAB_UI.with(|ui| {
        let mut s = ui.borrow_mut();
        s.menu = None;
        s.page_menu = if s.page_menu.is_some() { None } else { s.page_cursor = 0; Some(anchor) };
    });
}

pub(crate) fn record_tab_open(path: PathBuf) {
    TAB_UI.with(|ui| {
        let mut s = ui.borrow_mut();
        s.seq = s.seq.wrapping_add(1);
        let seq = s.seq;
        s.opening = Some((path, seq));
    });
}

pub(super) fn opening_seq(path: &Path) -> Option<u64> {
    TAB_UI.with(|ui| ui.borrow().opening.as_ref().and_then(|(p, seq)| (p == path).then_some(*seq)))
}

pub(super) fn closing_ghost() -> Option<ClosingGhost> {
    TAB_UI.with(|ui| ui.borrow().closing.clone())
}

pub(super) fn record_tab_close(index: usize, label: String, cx: &mut Context<Workspace>) {
    let seq = TAB_UI.with(|ui| {
        let mut s = ui.borrow_mut();
        s.seq = s.seq.wrapping_add(1);
        let seq = s.seq;
        s.closing = Some(ClosingGhost { index, label, seq });
        seq
    });
    cx.spawn(async move |this, cx| {
        cx.background_executor().timer(Duration::from_millis(TAB_ANIM_MS)).await;
        let _ = this.update(cx, |_, cx| {
            let cleared = TAB_UI.with(|ui| {
                let mut s = ui.borrow_mut();
                if s.closing.as_ref().is_some_and(|g| g.seq == seq) { s.closing = None; true } else { false }
            });
            if cleared { cx.notify(); }
        });
    }).detach();
}

/// Renders an icon that is mono (`base`) at rest and switches to duo (`hover`, `fill`, `accent`) when `group` is hovered.
pub(crate) fn hover_tinted_icon(
    group: SharedString, glyph: Icon, size: f32, base: Hsla, hover: Hsla, fill: Hsla,
) -> gpui::Div {
    let theme = Theme::ocean();
    let idle = IconColors { line: base, fill: transparent_black(), accent: base };
    let active = IconColors { line: hover, fill, accent: theme.accent };
    div().relative().flex_none().size(px(size))
        .child(div().group_hover(group.clone(), |s| s.invisible()).child(icon(glyph, px(size), idle)))
        .child(div().absolute().top_0().left_0().invisible().group_hover(group, |s| s.visible())
            .child(icon(glyph, px(size), active)))
}

/// Right-edge horizontal fade ramp from transparent `color` to solid `color`.
pub(super) fn tab_fade(color: Hsla) -> gpui::Div {
    div().w(px(TAB_FADE_W)).h_full().flex_none()
        .bg(linear_gradient(90., linear_color_stop(color.opacity(0.), 0.), linear_color_stop(color, 1.)))
}

/// Renders the title and right-side fade / dirty-dot / close-button slot for an inactive multi-tab pill.
pub(super) fn render_inactive_tab_body(
    label: String, group: SharedString, dirty: bool, close_btn: gpui::Stateful<gpui::Div>,
) -> gpui::Div {
    let theme = Theme::ocean();
    let normal_end = div().absolute().top_0().bottom_0().right_0().flex().items_center()
        .group_hover(group.clone(), |s| s.invisible())
        .child(tab_fade(theme.shell))
        .when(dirty, |s| s.child(div().w(px(14.)).h_full().bg(theme.shell).flex().items_center().justify_center()
            .child(div().size(px(6.)).rounded_full().bg(theme.accent))));
    let hover_end = div().absolute().top_0().bottom_0().right_0().flex().items_center()
        .invisible().group_hover(group, |s| s.visible())
        .child(tab_fade(theme.hover))
        .child(div().w(px(18.)).h_full().bg(theme.hover).flex().items_center().justify_center().child(close_btn));
    div().relative().flex_1().min_w_0().h_full().flex().items_center().overflow_hidden()
        .child(div().whitespace_nowrap().child(label))
        .child(normal_end)
        .child(hover_end)
}

/// Collapsing ghost slot for a recently closed tab (150ms ease-out width animation).
pub(super) fn render_closing_ghost(ghost: &ClosingGhost) -> impl IntoElement {
    let theme = Theme::ocean();
    div().id(("tab-closing", ghost.seq as usize)).h(px(28.)).flex_none().overflow_hidden()
        .rounded(crate::ui::radius::CONTROL).flex().items_center().gap(px(6.)).text_size(px(13.)).line_height(px(18.))
        .text_color(theme.text_secondary)
        .child(icon(Icon::Note, px(14.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().relative().flex_1().min_w_0().h_full().flex().items_center().overflow_hidden()
            .child(div().whitespace_nowrap().child(ghost.label.clone()))
            .child(tab_fade(theme.shell).absolute().top_0().bottom_0().right_0()))
        .with_animation(
            ("tab-close-anim", ghost.seq),
            Animation::new(Duration::from_millis(TAB_ANIM_MS)).with_easing(ease_out_quint()),
            |el, delta| {
                let k = (1.0 - delta).max(0.0);
                el.w(px(TAB_BASIS_W * k)).pl(px(10. * k)).pr(px(6. * k)).opacity(k)
            },
        )
}

impl Workspace {
    pub(in crate::app) fn tab_menu_open(&self) -> bool {
        TAB_UI.with(|ui| { let s = ui.borrow(); s.menu.is_some() || s.page_menu.is_some() })
    }

    pub(in crate::app) fn step_tab_menu(&mut self, down: bool, cx: &mut Context<Self>) {
        let page = TAB_UI.with(|ui| ui.borrow().page_menu.is_some());
        if page {
            let cur = TAB_UI.with(|ui| ui.borrow().page_cursor);
            let next = crate::ui::menu_nav::step(cur, &page_slots(), down);
            TAB_UI.with(|ui| ui.borrow_mut().page_cursor = next);
        } else if TAB_UI.with(|ui| ui.borrow().menu.is_some()) {
            let (has_others, has_right) = self.tab_menu_flags();
            let cur = TAB_UI.with(|ui| ui.borrow().tab_cursor);
            let next = crate::ui::menu_nav::step(cur, &tab_slots(has_others, has_right), down);
            TAB_UI.with(|ui| ui.borrow_mut().tab_cursor = next);
        }
        cx.notify();
    }

    pub(in crate::app) fn activate_tab_menu(&mut self, window: &mut gpui::Window, cx: &mut Context<Self>) {
        if TAB_UI.with(|ui| ui.borrow().page_menu.is_some()) {
            let path = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(Path::to_path_buf));
            let index = crate::ui::menu_nav::clamp_index(TAB_UI.with(|ui| ui.borrow().page_cursor), &page_slots());
            let Some(path) = path else { return };
            match index {
                0 => { clear_tab_menu(); self.open_page_history(window, cx); }
                1 => self.export_note(path, window, cx),
                2 => { clear_tab_menu(); self.editor.update(cx, |editor, cx| editor.copy_markdown(cx)); cx.notify(); }
                4 => { clear_tab_menu(); crate::files::ops::reveal_in_finder(&path); cx.notify(); }
                _ => {}
            }
            return;
        }
        let Some(menu) = TAB_UI.with(|ui| ui.borrow().menu.clone()) else { return };
        let tab_count = self.tabs.len();
        if tab_count == 0 { return; }
        let ix = self.tabs.iter().position(|p| p == &menu.path).unwrap_or(menu.index.min(tab_count - 1));
        let Some(target) = self.tabs.get(ix).cloned() else { return };
        let (has_others, has_right) = (tab_count > 1, ix + 1 < tab_count);
        let index = crate::ui::menu_nav::clamp_index(TAB_UI.with(|ui| ui.borrow().tab_cursor), &tab_slots(has_others, has_right));
        match index {
            0 => self.close_tab(target, cx),
            1 if has_others => self.close_other_tabs(target, cx),
            2 if has_right => self.close_tabs_to_right(ix, cx),
            4 => self.close_all_tabs(cx),
            _ => {}
        }
    }

    fn tab_menu_flags(&self) -> (bool, bool) {
        let Some(menu) = TAB_UI.with(|ui| ui.borrow().menu.clone()) else { return (false, false) };
        let tab_count = self.tabs.len();
        if tab_count == 0 { return (false, false); }
        let ix = self.tabs.iter().position(|p| p == &menu.path).unwrap_or(menu.index.min(tab_count - 1));
        (tab_count > 1, ix + 1 < tab_count)
    }

    pub(super) fn open_tab_menu(&mut self, index: usize, path: PathBuf, position: Point<Pixels>, cx: &mut Context<Self>) {
        if self.busy() { return; }
        TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.page_menu = None; s.tab_cursor = 0; s.menu = Some(TabMenu { index, path, position }); });
        cx.notify();
    }

    pub(crate) fn dismiss_tab_menu(&mut self, cx: &mut Context<Self>) -> bool {
        let had = TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.menu.take().is_some() || s.page_menu.take().is_some() });
        if had { cx.notify(); }
        had
    }

    pub(crate) fn export_note(&mut self, path: PathBuf, window: &mut gpui::Window, cx: &mut Context<Self>) {
        clear_tab_menu();
        self.close_overlays(cx);
        if self.editor.read(cx).current_path() != Some(path.as_path()) { self.open_file(path.clone(), cx); }
        let epoch = self.editor.read(cx).interaction_epoch();
        if !self.editor.read(cx).is_loading() {
            self.editor.update(cx, |e, cx| {
                if e.current_path() == Some(path.as_path()) {
                    crate::editor::export_document(e, window, cx);
                }
            });
            cx.notify();
            return;
        }
        let editor = self.editor.clone();
        cx.spawn_in(window, async move |_, cx| {
            for _ in 0..50 {
                cx.background_executor().timer(Duration::from_millis(10)).await;
                if !editor.read_with(cx, |e, _| e.is_loading()) { break; }
            }
            let _ = cx.update(|window, cx| editor.update(cx, |e, cx| {
                if e.interaction_epoch() != epoch { return; }
                if e.is_loading() {
                    e.report_status(tr("笔记仍在加载，请稍后重试导出", "Note is still loading; try exporting again shortly").into(), cx);
                } else if e.current_path() == Some(path.as_path()) {
                    crate::editor::export_document(e, window, cx);
                }
            }));
        }).detach();
    }

    pub(crate) fn close_other_tabs(&mut self, keep_path: PathBuf, cx: &mut Context<Self>) {
        clear_tab_menu();
        if self.busy() || !self.tabs.contains(&keep_path) { return; }
        let removed: Vec<PathBuf> = self.tabs.iter().filter(|p| *p != &keep_path).cloned().collect();
        if removed.is_empty() { return; }
        self.closed_tabs.record_others(&self.tabs, &keep_path);
        self.tabs = vec![keep_path.clone()];
        let need_open = self.selected.as_ref() != Some(&keep_path) || self.editor.read(cx).current_path() != Some(keep_path.as_path());
        self.editor.update(cx, |editor, cx| {
            if need_open { editor.open(keep_path.clone(), cx); }
            for tab in &removed { editor.close_session(tab, cx); }
        });
        if need_open { self.selected = Some(keep_path.clone()); self.tree.update(cx, |tree, cx| tree.select(Some(keep_path), cx)); }
        self.save_tabs_state(cx);
        cx.notify();
    }

    pub(crate) fn close_tabs_to_right(&mut self, index: usize, cx: &mut Context<Self>) {
        clear_tab_menu();
        if self.busy() || index + 1 >= self.tabs.len() { return; }
        let removed = self.tabs.split_off(index + 1);
        self.closed_tabs.record_from(index + 1, &removed);
        let active_removed = self.selected.as_ref().is_some_and(|s| removed.contains(s))
            || self.editor.read(cx).current_path().is_some_and(|p| removed.iter().any(|r| r == p));
        let fallback = self.tabs.get(index).cloned();
        self.editor.update(cx, |editor, cx| {
            if active_removed && let Some(ref next) = fallback { editor.open(next.clone(), cx); }
            for tab in &removed { editor.close_session(tab, cx); }
        });
        if active_removed { self.selected = fallback.clone(); self.tree.update(cx, |tree, cx| tree.select(fallback, cx)); }
        self.save_tabs_state(cx);
        cx.notify();
    }

    pub(crate) fn close_all_tabs(&mut self, cx: &mut Context<Self>) {
        clear_tab_menu();
        if self.busy() || self.tabs.is_empty() { return; }
        let removed = std::mem::take(&mut self.tabs);
        self.closed_tabs.record_from(0, &removed);
        let current = self.editor.read(cx).current_path().map(Path::to_path_buf);
        self.editor.update(cx, |editor, cx| {
            for tab in &removed { editor.close_session(tab, cx); }
            if let Some(cur) = current.filter(|c| !removed.contains(c)) { editor.close_session(&cur, cx); }
        });
        self.selected = None;
        self.tree.update(cx, |tree, cx| tree.select(None, cx));
        self.save_tabs_state(cx);
        cx.notify();
    }

    pub(super) fn render_tab_context_menu(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (live_menu, live_page) = TAB_UI.with(|ui| { let s = ui.borrow(); (s.menu.clone(), s.page_menu) });
        let snap = (live_menu.is_some() || live_page.is_some()).then(|| TabMenuSnap { menu: live_menu, page: live_page });
        let (phase, pop_gen, snap) = crate::ui::motion::popup_hold("tab-menu", snap)?;
        crate::ui::motion::schedule_popup_finish("tab-menu", pop_gen, cx);
        let (menu, page_pos) = (snap.menu, snap.page);
        let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
        let exp_hint = if mac { "⇧⌘E" } else { "Ctrl+Shift+E" };
        let (position, card) = if let Some(pos) = page_pos {
            let path = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(Path::to_path_buf))?;
            let (exp_path, rev_path) = (path.clone(), path);
            let page_cursor = TAB_UI.with(|ui| crate::ui::menu_nav::clamp_index(ui.borrow().page_cursor, &page_slots()));
            let card = crate::dropdown::shell("page-more-menu").w(px(216.))
                .child(tab_menu_row("page-history", Some(Icon::History), tr("页面历史…", "Page History…"), None, true, page_cursor == 0,
                    cx.listener(|this, _, window, cx| { clear_tab_menu(); this.open_page_history(window, cx); })))
                .child(tab_menu_row("page-export-image", Some(Icon::Image), tr("生成图片…", "Export as Image…"), Some(exp_hint), true, page_cursor == 1,
                    cx.listener(move |this, _, window, cx| this.export_note(exp_path.clone(), window, cx))))
                .child(tab_menu_row("page-copy-md", Some(Icon::Copy), tr("复制 Markdown", "Copy Markdown"), Some(if mac { "⌥⌘C" } else { "Ctrl+Alt+C" }), true, page_cursor == 2,
                    cx.listener(|this, _, _, cx| { clear_tab_menu(); this.editor.update(cx, |e, cx| e.copy_markdown(cx)); cx.notify(); })))
                .child(crate::dropdown::separator())
                .child(tab_menu_row("page-reveal", Some(Icon::FolderReveal), crate::files::ops::reveal_label(), None, true, page_cursor == 4,
                    cx.listener(move |_, _, _, cx| { clear_tab_menu(); crate::files::ops::reveal_in_finder(&rev_path); cx.notify(); })));
            (pos, card)
        } else {
            let menu = menu?;
            let tab_count = self.tabs.len();
            if tab_count == 0 { return None; }
            let ix = self.tabs.iter().position(|p| p == &menu.path).unwrap_or(menu.index.min(tab_count - 1));
            let target_path = self.tabs.get(ix)?.clone();
            let (has_others, has_right) = (tab_count > 1, ix + 1 < tab_count);
            let (close_one, close_others) = (target_path.clone(), target_path);
            let tab_cursor = TAB_UI.with(|ui| crate::ui::menu_nav::clamp_index(ui.borrow().tab_cursor, &tab_slots(has_others, has_right)));
            let card = crate::dropdown::shell("tab-context-menu").w(px(204.))
                .child(tab_menu_row("tab-menu-close", Some(Icon::TabClose), tr("关闭", "Close"), None, true, tab_cursor == 0, cx.listener(move |this, _, _, cx| this.close_tab(close_one.clone(), cx))))
                .child(tab_menu_row("tab-menu-close-others", Some(Icon::TabCloseOthers), tr("关闭其他", "Close Others"), None, has_others, tab_cursor == 1, cx.listener(move |this, _, _, cx| this.close_other_tabs(close_others.clone(), cx))))
                .child(tab_menu_row("tab-menu-close-right", Some(Icon::TabCloseRight), tr("关闭右侧", "Close to the Right"), None, has_right, tab_cursor == 2, cx.listener(move |this, _, _, cx| this.close_tabs_to_right(ix, cx))))
                .child(crate::dropdown::separator())
                .child(tab_menu_row("tab-menu-close-all", Some(Icon::TabCloseAll), tr("关闭全部", "Close All"), None, true, tab_cursor == 4, cx.listener(|this, _, _, cx| this.close_all_tabs(cx))));
            (menu.position, card)
        };
        let entering = phase == crate::ui::motion::PopupMotion::Enter;
        let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
            crate::ui::motion::layer_fade(
                div().id("tab-menu-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black()).occlude()
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.dismiss_tab_menu(cx); }))
                    .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.dismiss_tab_menu(cx); })),
                ("tab-menu-scrim", pop_gen),
                entering,
            ))).with_priority(1);
        let popup = crate::dropdown::anchored_motion_with(("tab-menu-pop", pop_gen), move |_, _| {
            if page_pos.is_some() {
                if let Some(bounds) = PAGE_TRIGGER.with(|b| b.get()) {
                    return crate::dropdown::PlacementSpec::below(bounds).align_x(crate::dropdown::AlignX::Right);
                }
            }
            crate::dropdown::PlacementSpec::point(position)
        }, card, phase, 2);
        Some(div().child(backdrop).child(popup).into_any_element())
    }
}

fn page_slots() -> [crate::ui::menu_nav::Slot; 5] {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    [Item, Item, Item, Skip, Item]
}

fn tab_slots(has_others: bool, has_right: bool) -> [crate::ui::menu_nav::Slot; 5] {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    [Item, if has_others { Item } else { Skip }, if has_right { Item } else { Skip }, Skip, Item]
}

fn tab_menu_row(
    id: &'static str, icon: Option<Icon>, label: &'static str, hint: Option<&'static str>, enabled: bool, highlighted: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> gpui_base::Button {
    crate::dropdown::row_with(id, crate::dropdown::RowSpec {
        icon, label: label.into(), hint: hint.map(Into::into), danger: id == "tab-menu-close-all",
        disabled: !enabled, highlighted, ..Default::default()
    }).when(enabled, |row| row.on_mouse_down(MouseButton::Left, on_click))
}
