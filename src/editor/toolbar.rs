//! Floating selection toolbar (F-Toolbar): light pill of 32px icon buttons split by hairline separators.
use std::{cell::RefCell, collections::HashMap};
use gpui::{BoxShadow, Bounds, Context, CursorStyle, Entity, MouseButton, Pixels, Point, Window,
    div, point, prelude::*, px, rgb, rgba};
use crate::{icons::{Icon, IconColors, icon}, theme::{is_dark, tr}};
use super::{format_cmds::{FormatPop, palette::button_preview_colors}, model::{Kind, MarkKind}, view::Editor};

pub(super) const BG: u32 = 0xffffff;
pub(super) const BORDER: u32 = 0xececea;
pub(super) const INK: u32 = 0x37352f;
pub(super) const MUTED: u32 = 0x5e5d59;
pub(super) const HOVER: u32 = 0xf1f1ef;
pub(super) const ON_BG: u32 = 0xe7effd;
pub(super) const ON_INK: u32 = 0x2f6fe4;
pub(super) const OPEN_BG: u32 = 0xefefed;
pub(super) const SEP: u32 = 0xe6e6e3;
pub(super) const ACCENT: u32 = 0x2f6fe4;

#[derive(Clone, Copy)]
pub(super) struct ToolbarColors {
    pub bg: u32,
    pub border: u32,
    pub ink: u32,
    pub muted: u32,
    pub hover: u32,
    pub on_bg: u32,
    pub on_ink: u32,
    pub open_bg: u32,
    pub sep: u32,
    pub accent: u32,
    pub input_bg: u32,
}

impl ToolbarColors {
    pub fn current() -> Self {
        if is_dark() {
            Self {
                bg: 0x26282d, border: 0x3a3e46, ink: 0xe8eaee, muted: 0x9aa0aa,
                hover: 0x3a3e46, on_bg: 0x33415c, on_ink: 0x9dbcf5, open_bg: 0x3a3e46,
                sep: 0x454a53, accent: 0x547dd0, input_bg: 0x33363c,
            }
        } else {
            Self {
                bg: BG, border: BORDER, ink: INK, muted: MUTED,
                hover: HOVER, on_bg: ON_BG, on_ink: ON_INK, open_bg: OPEN_BG,
                sep: SEP, accent: ACCENT, input_bg: 0xf1f1ef,
            }
        }
    }
}

pub(super) fn bar_shadows() -> Vec<BoxShadow> {
    vec![
        BoxShadow { color: rgba(0x1f1e1d1a).into(), offset: point(px(0.), px(8.)), blur_radius: px(24.), spread_radius: px(0.) },
        BoxShadow { color: rgba(0x1f1e1d0f).into(), offset: point(px(0.), px(1.)), blur_radius: px(3.), spread_radius: px(0.) },
    ]
}

pub(super) fn menu_shadows() -> Vec<BoxShadow> {
    vec![
        BoxShadow { color: rgba(0x1f1e1d1f).into(), offset: point(px(0.), px(12.)), blur_radius: px(32.), spread_radius: px(0.) },
        BoxShadow { color: rgba(0x1f1e1d0f).into(), offset: point(px(0.), px(1.)), blur_radius: px(3.), spread_radius: px(0.) },
    ]
}

/// Rounded capsule container shared by the selection toolbar, link pill and image size pill.
pub(super) fn pill() -> gpui::Div {
    let tc = ToolbarColors::current();
    div().flex().items_center().gap(px(2.)).py(px(4.)).px(px(6.)).rounded(px(14.)).border_1()
        .border_color(rgb(tc.border)).bg(rgb(tc.bg)).shadow(bar_shadows())
}
pub(super) fn sep() -> gpui::Div {
    let tc = ToolbarColors::current();
    div().w(px(1.)).h(px(20.)).mx(px(4.)).flex_none().bg(rgb(tc.sep))
}
pub(super) fn glyph(glyph: Icon, size: f32, color: u32) -> impl IntoElement {
    let resolved = if is_dark() && color == INK { 0xe8eaee } else { color };
    icon(glyph, px(size), IconColors { line: rgb(resolved).into(), fill: gpui::transparent_black(), accent: gpui::transparent_black() })
}

thread_local! { static BOUNDS: RefCell<HashMap<&'static str, Bounds<Pixels>>> = Default::default(); }
/// Where a dropdown button was last painted: the popover opens just under it.
pub(super) fn drop_anchor(key: &'static str) -> Option<Point<Pixels>> {
    BOUNDS.with(|b| b.borrow().get(key).map(|r| r.bottom_left() + point(px(0.), px(6.))))
}

pub(super) fn selection_toolbar(editor: Entity<Editor>, _id: u64, anchor: Point<Pixels>) -> SelectionToolbar { SelectionToolbar { editor, anchor } }

/// 添加批注 添加表情回应 | 文字类型▾ | B I U S {} 调色盘▾ | 列表▾ | 链接.
/// Rendered lazily so pressed states read the live selection.
#[derive(IntoElement)]
pub(super) struct SelectionToolbar { editor: Entity<Editor>, anchor: Point<Pixels> }
impl RenderOnce for SelectionToolbar {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        let editor = self.editor;
        let (state, kinds_open, lists_open, palette_open) = {
            let ed = editor.read(cx);
            (
                ed.toolbar_state(),
                matches!(ed.format, Some(FormatPop::Kinds { .. })),
                matches!(ed.format, Some(FormatPop::Lists { .. })),
                matches!(ed.format, Some(FormatPop::Palette { .. })),
            )
        };
        let mark = |g, tip, kind: MarkKind, on| button(g, tip, on, false, false, editor.clone(), move |this, _, cx| this.mark(kind, cx));
        let text_blocks = !state.table;
        // Narrow editors (AI 助手 open): wrap onto a second row inside the editor
        // instead of running under the chat panel.
        let room = editor.read(cx).list.viewport_bounds().size.width - px(16.);
        gpui::anchored().position(self.anchor).snap_to_window_with_margin(px(8.)).child(
            pill().id("selection-toolbar").max_w(room).flex_wrap().on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .child(button(Icon::TbComment, tr("添加批注", "Add comment"), false, false, false, editor.clone(), |this, w, cx| this.start_annotation(w, cx)))
                .child(button(Icon::TbReaction, tr("添加表情回应", "Add reaction"), false, false, false, editor.clone(), |this, w, cx| this.open_reactions(w, cx)))
                .child(sep())
                .when(text_blocks, |bar| bar
                    .child(button(Icon::TbType, tr("文字类型", "Text type"), false, kinds_open, true, editor.clone(), |this, w, cx| this.toggle_menu(true, w, cx)))
                    .child(sep()))
                .child(mark(Icon::TbBold, tr("粗体", "Bold"), MarkKind::Bold, state.bold))
                .child(mark(Icon::TbItalic, tr("斜体", "Italic"), MarkKind::Italic, state.italic))
                .child(button(Icon::TbUnderline, tr("下划线", "Underline"), state.underline, false, false, editor.clone(), |this, _, cx| this.underline(cx)))
                .child(mark(Icon::TbStrike, tr("删除线", "Strikethrough"), MarkKind::Strike, state.strike))
                .child(mark(Icon::TbCode, tr("行内代码", "Inline code"), MarkKind::Code, state.code))
                .child(palette_button(state.colored, palette_open, editor.clone()))
                .child(sep())
                .child(button(Icon::TbList, tr("列表", "Lists"), false, lists_open, true, editor.clone(), |this, w, cx| this.toggle_menu(false, w, cx)))
                .child(sep())
                .when(!state.grid, |bar| bar
                    .child(button(Icon::TbLink, tr("链接 ⌘⇧K", "Link ⌘⇧K"), state.link, false, false, editor.clone(), |this, w, cx| this.open_link(w, cx))))
                .when(state.grid, |bar| bar.child(disabled_button(Icon::TbLink))))
    }
}

fn disabled_button(g: Icon) -> gpui::Div {
    let dim = if is_dark() { 0x5a5a5a } else { 0x9a9994 };
    div().h(px(32.)).min_w(px(32.)).flex_none().flex().items_center().justify_center().px(px(6.)).rounded(px(8.))
        .cursor(CursorStyle::Arrow).child(glyph(g, 18., dim))
}

/// 32px icon toggle (wide with a chevron when it opens a dropdown); pressed is a blue-tinted chip.
fn button(g: Icon, tip: &'static str, on: bool, open: bool, dropdown: bool, editor: Entity<Editor>,
    run: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static) -> gpui::Stateful<gpui::Div> {
    let tc = ToolbarColors::current();
    let ink = if on { tc.on_ink } else { tc.ink };
    div().id(tip).relative().h(px(32.)).min_w(px(32.)).flex_none().flex().items_center().justify_center().gap(px(1.)).px(px(6.)).rounded(px(8.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(rgb(tc.hover)))
        .when(open && !on, |b| b.bg(rgb(tc.open_bg)))
        .when(on, |b| b.bg(rgb(tc.on_bg)))
        .child(glyph(g, 18., ink))
        .when(dropdown, |b| b.child(glyph(Icon::TbChevron, 10., ink))
            .child(gpui::canvas(move |bounds, _, _| { BOUNDS.with(|m| { m.borrow_mut().insert(tip, bounds); }); }, |_, _, _, _| {})
                .absolute().top_0().left_0().size_full()))
        .tooltip(crate::theme::tooltip(tip))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| { run(this, window, cx); if this.format.is_none() { window.focus(&this.focus); } });
            cx.stop_propagation();
        })
}

/// Palette button: hover opens panel (150ms leave closes); click applies/clears last color.
fn palette_button(on: bool, open: bool, editor: Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    let tip = tr("颜色；点击应用上次颜色", "Color · click to apply last color");
    let tc = ToolbarColors::current();
    let btn_ink = if on { tc.on_ink } else { tc.ink };
    let (text_tint, bar_color) = button_preview_colors();
    let icon_ink = text_tint.unwrap_or(btn_ink);
    let hover_editor = editor.clone();
    div().id(tip).relative().h(px(32.)).min_w(px(32.)).flex_none().flex().items_center().justify_center().gap(px(1.)).px(px(6.)).rounded(px(8.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(rgb(tc.hover)))
        .when(open && !on, |b| b.bg(rgb(tc.open_bg)))
        .when(on, |b| b.bg(rgb(tc.on_bg)))
        .child(div().relative().flex().items_center().justify_center()
            .child(glyph(Icon::TbPalette, 18., icon_ink))
            .child(div().absolute().left(px(2.)).right(px(2.)).bottom(px(-2.)).h(px(2.)).rounded(px(1.))
                .when_some(bar_color, |d, c| d.bg(rgb(c)))))
        .child(glyph(Icon::TbChevron, 10., btn_ink))
        .child(gpui::canvas(move |bounds, _, _| { BOUNDS.with(|m| { m.borrow_mut().insert(tip, bounds); }); }, |_, _, _, _| {})
            .absolute().top_0().left_0().size_full())
        .tooltip(crate::theme::tooltip(tip))
        .on_hover(move |hovered, _, cx| {
            let anchor = drop_anchor(tip);
            hover_editor.update(cx, |this, cx| this.hover_palette_zone(*hovered, anchor, cx));
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| {
                this.click_palette_button(window, cx);
                if this.format.is_none() { window.focus(&this.focus); }
            });
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
            let anchor = drop_anchor(if kinds { tr("文字类型", "Text type") } else { tr("列表", "Lists") }).unwrap_or_else(|| window.mouse_position());
            self.format = Some(if kinds { FormatPop::Kinds { anchor, current } } else { FormatPop::Lists { anchor, current } });
        }
        cx.notify();
    }
    /// A list item that is already of that kind turns back into body text.
    fn pick_kind(&mut self, kind: Kind, list: bool, window: &mut Window, cx: &mut Context<Self>) {
        let current = self.doc.get(self.caret.head.block).map(|b| b.kind);
        self.set_kind_shortcut(if list && current == Some(kind) { Kind::Paragraph } else { kind }, cx);
        self.close_format(window, cx);
    }
}

/// Dropdown body for the text-type (`kinds`) or list menu (`F-Toolbar.dc.html`).
pub(super) fn menu(editor: Entity<Editor>, kinds: bool, current: Option<Kind>) -> gpui::AnyElement {
    let tc = ToolbarColors::current();
    let kind_items = [
        (Kind::Paragraph, tr("正文", "Text")),
        (Kind::H1, tr("一级标题", "Heading 1")),
        (Kind::H2, tr("二级标题", "Heading 2")),
        (Kind::H3, tr("三级标题", "Heading 3")),
    ];
    let list_items = [
        (Kind::Bullet, tr("无序列表", "Bulleted list")),
        (Kind::Numbered, tr("有序列表", "Numbered list")),
        (Kind::Todo, tr("待办清单", "Checklist")),
    ];
    let items: &[(Kind, &'static str)] = if kinds { &kind_items } else { &list_items };
    let mut column = div().w(px(206.)).flex().flex_col();
    for (i, &(kind, label)) in items.iter().enumerate() {
        let on = current == Some(kind);
        let editor = editor.clone();
        column = column.child(
            div().id(("kind-item", i + usize::from(!kinds) * 8))
                .h(px(34.)).px(px(10.)).flex().items_center().gap(px(10.)).rounded(px(8.))
                .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(rgb(tc.hover)))
                .text_size(px(14.)).text_color(rgb(tc.ink))
                .child(div().w(px(20.)).flex_none().flex().items_center().justify_center()
                    .child(glyph(super::block_menu::kind_icon(kind), 18., tc.muted)))
                .child(div().flex_1().child(label))
                .when(on, |r| r.child(glyph(Icon::Check, 16., tc.ink)))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    editor.update(cx, |this, cx| this.pick_kind(kind, !kinds, window, cx));
                    cx.stop_propagation();
                }),
        );
    }
    column.into_any_element()
}
