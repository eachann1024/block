//! Dark floating selection toolbar (F-Inline): a pill of 32px icon buttons split by hairline separators.
use std::{cell::RefCell, collections::HashMap};
use gpui::{Bounds, Context, CursorStyle, Entity, MouseButton, Pixels, Point, Window, div, point, prelude::*, px, rgb};
use crate::icons::{Icon, IconColors, icon};
use super::{format_cmds::FormatPop, model::{Kind, MarkKind}, view::Editor};

pub(super) const BG: u32 = 0x26282d;
pub(super) const BORDER: u32 = 0x3a3e46;
pub(super) const INK: u32 = 0xe8eaee;
pub(super) const MUTED: u32 = 0x9aa0aa;
pub(super) const HOVER: u32 = 0x3a3e46;
pub(super) const ON_BG: u32 = 0x33415c;
pub(super) const ON_INK: u32 = 0x9dbcf5;
pub(super) const SEP: u32 = 0x454a53;
pub(super) const ACCENT: u32 = 0x547dd0;

/// Dark rounded container shared by the toolbar, link pill and image size pill.
pub(super) fn pill() -> gpui::Div {
    div().flex().items_center().gap(px(2.)).p(px(4.)).rounded(px(12.)).border_1().border_color(rgb(BORDER)).bg(rgb(BG)).shadow_lg()
}
pub(super) fn sep() -> gpui::Div { div().w(px(1.)).h(px(20.)).mx(px(4.)).flex_none().bg(rgb(SEP)) }
pub(super) fn glyph(glyph: Icon, size: f32, color: u32) -> impl IntoElement {
    icon(glyph, px(size), IconColors { line: rgb(color).into(), fill: gpui::transparent_black(), accent: gpui::transparent_black() })
}

thread_local! { static BOUNDS: RefCell<HashMap<&'static str, Bounds<Pixels>>> = Default::default(); }
/// Where a dropdown button was last painted: the popover opens just under it.
fn drop_anchor(key: &'static str) -> Option<Point<Pixels>> {
    BOUNDS.with(|b| b.borrow().get(key).map(|r| r.bottom_left() + point(px(0.), px(6.))))
}

pub(super) fn selection_toolbar(editor: Entity<Editor>, _id: u64, anchor: Point<Pixels>) -> SelectionToolbar { SelectionToolbar { editor, anchor } }

/// [文字类型▾] | 粗体 斜体 下划线 删除线 行内代码 清除格式 | 颜色▾ 列表▾ | 链接 生成图片.
/// Rendered lazily so pressed states read the live selection.
#[derive(IntoElement)]
pub(super) struct SelectionToolbar { editor: Entity<Editor>, anchor: Point<Pixels> }
impl RenderOnce for SelectionToolbar {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        let editor = self.editor;
        let state = editor.read(cx).toolbar_state();
        let mark = |g, tip, kind: MarkKind, on| button(g, tip, on, false, editor.clone(), move |this, _, cx| this.mark(kind, cx));
        let text_blocks = !state.table;
        gpui::anchored().position(self.anchor).snap_to_window_with_margin(px(8.)).child(
            pill().id("selection-toolbar").on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .when(text_blocks, |bar| bar.child(button(Icon::TbType, "文字类型", false, true, editor.clone(), |this, w, cx| this.toggle_menu(true, w, cx)))
                    .child(sep()))
                .child(mark(Icon::TbBold, "粗体", MarkKind::Bold, state.bold))
                .child(mark(Icon::TbItalic, "斜体", MarkKind::Italic, state.italic))
                .child(button(Icon::TbUnderline, "下划线", state.underline, false, editor.clone(), |this, _, cx| this.underline(cx)))
                .child(mark(Icon::TbStrike, "删除线", MarkKind::Strike, state.strike))
                .when(!state.table, |bar| bar.child(mark(Icon::TbCode, "行内代码", MarkKind::Code, state.code)))
                .child(button(Icon::Eraser, "清除格式", false, false, editor.clone(), |this, _, cx| this.clear_formatting(cx)))
                .child(sep())
                .child(button(Icon::TbPalette, "文字颜色", state.colored, true, editor.clone(), |this, w, cx| this.toggle_palette(w, cx)))
                .when(text_blocks, |bar| bar.child(button(Icon::TbList, "列表", false, true, editor.clone(), |this, w, cx| this.toggle_menu(false, w, cx))))
                .child(sep())
                .when(!state.grid, |bar| bar.child(button(Icon::TbLink, "链接 ⌘⇧K", state.link, false, editor.clone(), |this, w, cx| this.open_link(w, cx))))
                .when(!state.table, |bar| bar.child(button(Icon::TbImage, "生成图片", false, false, editor.clone(), |this, w, cx| super::export_selected(this, w, cx))))
                .child(sep())
                .child(button(Icon::Sparkles, "AI 润色", false, false, editor.clone(), |this, w, cx| this.open_ai(w, cx))))
    }
}

/// 32px icon toggle (wide with a chevron when it opens a dropdown); pressed is a blue-tinted chip.
fn button(g: Icon, tip: &'static str, on: bool, dropdown: bool, editor: Entity<Editor>,
    run: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static) -> gpui::Stateful<gpui::Div> {
    let ink = if on { ON_INK } else { INK };
    div().id(tip).relative().h(px(32.)).min_w(px(32.)).flex_none().flex().items_center().justify_center().gap(px(2.)).rounded(px(8.))
        .when(dropdown, |b| b.px(px(6.)))
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(rgb(HOVER))).when(on, |b| b.bg(rgb(ON_BG)))
        .child(glyph(g, 17., ink))
        .when(dropdown, |b| b.child(glyph(Icon::TbChevron, 10., ink))
            .child(gpui::canvas(move |bounds, _, _| { BOUNDS.with(|m| { m.borrow_mut().insert(tip, bounds); }); }, |_, _, _, _| {})
                .absolute().top_0().left_0().size_full()))
        .tooltip(move |window, cx| gpui_component::tooltip::Tooltip::new(tip).build(window, cx))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| { run(this, window, cx); if this.format.is_none() { window.focus(&this.focus); } });
            cx.stop_propagation();
        })
}

impl Editor {
    /// Opens (or closes) the text-type or list dropdown under its toolbar button.
    fn toggle_menu(&mut self, kinds: bool, window: &Window, cx: &mut Context<Self>) {
        let was = matches!((&self.format, kinds), (Some(FormatPop::Kinds { .. }), true) | (Some(FormatPop::Lists { .. }), false));
        let current = self.doc.get(self.caret.head.block).map(|b| b.kind);
        self.format = None;
        if !was {
            let anchor = drop_anchor(if kinds { "文字类型" } else { "列表" }).unwrap_or_else(|| window.mouse_position());
            self.format = Some(if kinds { FormatPop::Kinds { anchor, current } } else { FormatPop::Lists { anchor, current } });
        }
        cx.notify();
    }
    fn toggle_palette(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if matches!(self.format, Some(FormatPop::Palette { .. })) { self.format = None; cx.notify(); return; }
        self.open_palette(window, cx);
        if let (Some(FormatPop::Palette { anchor }), Some(at)) = (self.format.as_mut(), drop_anchor("文字颜色")) { *anchor = at; }
    }
    /// A list item that is already of that kind turns back into body text.
    fn pick_kind(&mut self, kind: Kind, list: bool, window: &mut Window, cx: &mut Context<Self>) {
        let current = self.doc.get(self.caret.head.block).map(|b| b.kind);
        self.set_kind_shortcut(if list && current == Some(kind) { Kind::Paragraph } else { kind }, cx);
        self.close_format(window, cx);
    }
}

/// Dropdown body for the text-type (`kinds`) or list menu.
pub(super) fn menu(editor: Entity<Editor>, kinds: bool, current: Option<Kind>) -> gpui::AnyElement {
    use super::block_menu::Cmd;
    let items: &[Kind] = if kinds { &[Kind::Paragraph, Kind::H1, Kind::H2, Kind::H3] } else { &[Kind::Bullet, Kind::Numbered, Kind::Todo] };
    let mut column = div().w(px(188.)).flex().flex_col().gap(px(1.));
    for (i, kind) in items.iter().copied().enumerate() {
        let on = current == Some(kind); let editor = editor.clone();
        let cmd = Cmd::Turn(kind);
        column = column.child(div().id(("kind-item", i + usize::from(!kinds) * 8)).h(px(32.)).px(px(8.)).flex().items_center().gap(px(8.)).rounded(px(8.))
            .cursor(CursorStyle::PointingHand).hover(|s| s.bg(rgb(HOVER))).when(on, |s| s.bg(rgb(ON_BG)))
            .text_size(px(13.)).text_color(rgb(if on { ON_INK } else { INK }))
            .child(glyph(super::block_menu::kind_icon(kind), 16., if on { ON_INK } else { INK }))
            .child(div().flex_1().child(cmd.label()))
            .when_some(cmd.hint(), |r, hint| r.child(div().text_size(px(11.)).text_color(rgb(MUTED)).child(hint)))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| this.pick_kind(kind, !kinds, window, cx)); cx.stop_propagation();
            }));
    }
    column.into_any_element()
}
