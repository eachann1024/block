//! Tab context menu (batch close) and open/close width animations for `Tabs-1-Titlebar`.
use std::cell::RefCell;
use std::path::{Path, PathBuf};
use std::time::Duration;
use gpui::{
    Animation, AnimationExt, AnyElement, Context, Hsla, IntoElement,
    MouseButton, Pixels, Point, SharedString, anchored, deferred, div, ease_out_quint,
    linear_color_stop, linear_gradient, point, prelude::*, px, transparent_black,
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
pub(super) struct ClosingGhost { pub index: usize, pub label: String, pub seq: u64 }

#[derive(Default)]
struct TabUiState {
    menu: Option<TabMenu>, page_menu: Option<Point<Pixels>>,
    opening: Option<(PathBuf, u64)>, closing: Option<ClosingGhost>, seq: u64,
}

thread_local! {
    static TAB_UI: RefCell<TabUiState> = RefCell::new(TabUiState::default());
}

pub(crate) fn clear_tab_menu() {
    TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.menu = None; s.page_menu = None; s.opening = None; });
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
    pub(super) fn open_tab_menu(&mut self, index: usize, path: PathBuf, position: Point<Pixels>, cx: &mut Context<Self>) {
        if self.busy() { return; }
        TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.page_menu = None; s.menu = Some(TabMenu { index, path, position }); });
        cx.notify();
    }

    pub(crate) fn dismiss_tab_menu(&mut self, cx: &mut Context<Self>) -> bool {
        let had = TAB_UI.with(|ui| {
            let mut s = ui.borrow_mut();
            s.menu.take().is_some() || s.page_menu.take().is_some()
        });
        if had { cx.notify(); }
        had
    }

    pub(crate) fn export_note(&mut self, path: PathBuf, window: &mut gpui::Window, cx: &mut Context<Self>) {
        clear_tab_menu();
        self.close_overlays(cx);
        if self.editor.read(cx).current_path() != Some(path.as_path()) {
            self.open_file(path, cx);
        }
        if !self.editor.read(cx).is_loading() {
            self.editor.update(cx, |e, cx| crate::editor::export_document(e, window, cx));
            cx.notify();
            return;
        }
        let editor = self.editor.clone();
        cx.spawn_in(window, async move |_, cx| {
            for _ in 0..50 {
                cx.background_executor().timer(Duration::from_millis(10)).await;
                if !editor.read_with(cx, |e, _| e.is_loading()) { break; }
            }
            let _ = cx.update(|window, cx| editor.update(cx, |e, cx| crate::editor::export_document(e, window, cx)));
        }).detach();
    }

    pub(super) fn render_titlebar_tail(&self, has_note: bool, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let page_open = TAB_UI.with(|ui| ui.borrow().page_menu.is_some());
        let more_group: SharedString = "titlebar-more-btn".into();
        let tail_actions = if has_note {
            let send_seg = self.annotation_actions(cx);
            let (sz, rd) = if send_seg.is_some() { (px(24.), px(6.)) } else { (px(26.), crate::ui::radius::CONTROL) };
            let more = div().id("titlebar-more").group(more_group.clone()).size(sz).rounded(rd)
                .flex_none().flex().items_center().justify_center().cursor(gpui::CursorStyle::PointingHand)
                .when(page_open, |b| b.bg(theme.selected)).when(!page_open, |b| b.hover(|s| s.bg(theme.hover)))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                    cx.stop_propagation();
                    this.close_overlays(cx);
                    let anchor = point(event.position.x - px(188.), px(38.));
                    TAB_UI.with(|ui| { let mut s = ui.borrow_mut(); s.menu = None; s.page_menu = if s.page_menu.is_some() { None } else { Some(anchor) }; });
                    cx.notify();
                }))
                .child(if page_open { icon(Icon::More, px(16.), theme.icon_colors(theme.accent)) } else { hover_tinted_icon(more_group, Icon::More, 16., theme.text_secondary, theme.text, theme.icon_fill) });
            let mr = if cfg!(any(target_os = "macos", target_family = "wasm")) { 10. } else { 4. };
            Some(crate::ui::annotation_card::titlebar_split_pill(send_seg, more).mr(px(mr)))
        } else {
            None
        };
        div().h_full().flex_none().flex().items_center().children(tail_actions).child(crate::ui::window_controls::window_controls("main-win", true, true))
    }

    pub(crate) fn close_other_tabs(&mut self, keep_path: PathBuf, cx: &mut Context<Self>) {
        clear_tab_menu();
        if self.busy() || !self.tabs.contains(&keep_path) { return; }
        let removed: Vec<PathBuf> = self.tabs.iter().filter(|p| *p != &keep_path).cloned().collect();
        if removed.is_empty() { return; }
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
        let active_removed = self.selected.as_ref().is_some_and(|s| removed.contains(s))
            || self.editor.read(cx).current_path().is_some_and(|p| removed.iter().any(|r| r == p));
        let fallback = self.tabs.get(index).cloned();
        self.editor.update(cx, |editor, cx| {
            if active_removed { if let Some(ref next) = fallback { editor.open(next.clone(), cx); } }
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
        let current = self.editor.read(cx).current_path().map(Path::to_path_buf);
        self.editor.update(cx, |editor, cx| {
            for tab in &removed { editor.close_session(tab, cx); }
            if let Some(cur) = current.filter(|c| !removed.contains(c)) {
                editor.close_session(&cur, cx);
            }
        });
        self.selected = None;
        self.tree.update(cx, |tree, cx| tree.select(None, cx));
        self.save_tabs_state(cx);
        cx.notify();
    }

    pub(super) fn render_tab_context_menu(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (menu, page_pos) = TAB_UI.with(|ui| { let s = ui.borrow(); (s.menu.clone(), s.page_menu) });
        let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
        let exp_hint = if mac { "⇧⌘E" } else { "Ctrl+Shift+E" };
        let (position, card) = if let Some(pos) = page_pos {
            let path = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(Path::to_path_buf))?;
            let (exp_path, rev_path) = (path.clone(), path);
            let card = crate::dropdown::shell("page-more-menu").w(px(216.))
                .child(tab_menu_row("page-export-image", Some(Icon::Image), tr("生成图片…", "Export as Image…"), Some(exp_hint), true,
                    cx.listener(move |this, _, window, cx| this.export_note(exp_path.clone(), window, cx))))
                .child(tab_menu_row("page-copy-md", Some(Icon::Copy), tr("复制 Markdown", "Copy Markdown"), Some(if mac { "⌥⌘C" } else { "Ctrl+Alt+C" }), true,
                    cx.listener(|this, _, _, cx| { clear_tab_menu(); this.editor.update(cx, |e, cx| e.copy_markdown(cx)); cx.notify(); })))
                .child(crate::dropdown::separator())
                .child(tab_menu_row("page-reveal", Some(Icon::FolderReveal), crate::files::ops::reveal_label(), None, true,
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
            let card = crate::dropdown::shell("tab-context-menu").w(px(204.))
                .child(tab_menu_row("tab-menu-close", Some(Icon::TabClose), tr("关闭", "Close"), None, true, cx.listener(move |this, _, _, cx| this.close_tab(close_one.clone(), cx))))
                .child(tab_menu_row("tab-menu-close-others", Some(Icon::TabCloseOthers), tr("关闭其他", "Close Others"), None, has_others, cx.listener(move |this, _, _, cx| this.close_other_tabs(close_others.clone(), cx))))
                .child(tab_menu_row("tab-menu-close-right", Some(Icon::TabCloseRight), tr("关闭右侧", "Close to the Right"), None, has_right, cx.listener(move |this, _, _, cx| this.close_tabs_to_right(ix, cx))))
                .child(crate::dropdown::separator())
                .child(tab_menu_row("tab-menu-close-all", Some(Icon::TabCloseAll), tr("关闭全部", "Close All"), None, true, cx.listener(|this, _, _, cx| this.close_all_tabs(cx))));
            (menu.position, card)
        };
        let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
            div().id("tab-menu-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black()).occlude()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.dismiss_tab_menu(cx); }))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.dismiss_tab_menu(cx); })),
        )).with_priority(1);
        let popup = deferred(anchored().position(position).snap_to_window_with_margin(px(8.)).child(card)).with_priority(2);
        Some(div().child(backdrop).child(popup).into_any_element())
    }
}

fn tab_menu_row(
    id: &'static str, icon: Option<Icon>, label: &'static str, hint: Option<&'static str>, enabled: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    crate::dropdown::row_with(id, crate::dropdown::RowSpec {
        icon, label: label.into(), hint: hint.map(Into::into), danger: id == "tab-menu-close-all", disabled: !enabled, ..Default::default()
    }).when(enabled, |row| row.on_mouse_down(MouseButton::Left, on_click))
}
