//! Floating selection toolbar (F-Toolbar): light pill of 28px icon buttons split by hairline separators.
use std::{cell::RefCell, collections::HashMap};
use gpui::{BoxShadow, Bounds, Context, Entity, MouseButton, Pixels, Point, SharedString, Window,
    point, prelude::*, px, rgb, rgba};
use crate::{icons::{Icon, stateful_icon}, theme::{Theme, tr}};
pub(super) use crate::ui::floating_bar::{
    ToolbarColors, animate as animate_bar, disabled_icon_button, duo_icon_colors, icon_toggle_button, sep, shell as pill,
};
use super::{format_cmds::FormatPop, model::{Kind, MarkKind}, view::Editor};

#[allow(dead_code)]
pub(super) fn menu_shadows() -> Vec<BoxShadow> {
    vec![
        BoxShadow { color: rgba(0x1f1e1d1f).into(), offset: point(px(0.), px(12.)), blur_radius: px(32.), spread_radius: px(0.), inset: false },
        BoxShadow { color: rgba(0x1f1e1d0f).into(), offset: point(px(0.), px(1.)), blur_radius: px(3.), spread_radius: px(0.), inset: false },
    ]
}

thread_local! { static BOUNDS: RefCell<HashMap<&'static str, Bounds<Pixels>>> = Default::default(); }
/// Placement spec for a toolbar dropdown button: left-aligned to the button, flipping above if needed.
pub(super) fn drop_spec_for(key: &'static str, viewport: Bounds<Pixels>, window: &Window) -> crate::dropdown::PlacementSpec {
    BOUNDS
        .with(|b| b.borrow().get(key).copied())
        .map(|r| {
            crate::dropdown::PlacementSpec::below(r)
                .align_x(crate::dropdown::AlignX::Left)
                .viewport(viewport)
                .gap(px(4.))
        })
        .unwrap_or_else(|| crate::dropdown::PlacementSpec::point(window.mouse_position()).viewport(viewport))
}

/// Last painted bounds of a toolbar button. Menus read this while open so a resize
/// does not leave them on the click-time rectangle.
pub(super) fn tracked_button(key: &'static str) -> Option<Bounds<Pixels>> {
    BOUNDS.with(|bounds| bounds.borrow().get(key).copied())
}

pub(super) fn selection_toolbar(editor: Entity<Editor>, _id: u64, anchor: Point<Pixels>) -> SelectionToolbar { SelectionToolbar { editor, anchor } }

/// 添加批注 添加表情回应 | 文字类型▾ | B I U S {} 调色盘▾ | 列表▾ | 链接.
/// Rendered lazily so pressed states read the live selection.
#[derive(IntoElement)]
pub(super) struct SelectionToolbar { editor: Entity<Editor>, anchor: Point<Pixels> }
impl RenderOnce for SelectionToolbar {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        let editor = self.editor;
        let (state, kinds_open, lists_open, palette_open, link_open, comment_open, reaction_open) = {
            let ed = editor.read(cx);
            (
                ed.toolbar_state(),
                matches!(ed.format, Some(FormatPop::Kinds { .. })),
                matches!(ed.format, Some(FormatPop::Lists { .. })),
                matches!(ed.format, Some(FormatPop::Palette { .. })),
                matches!(ed.format, Some(FormatPop::Link { .. })),
                ed.annotate.composer.is_some(),
                ed.annotate.reactions.is_some(),
            )
        };
        let mark = |g, tip, kind: MarkKind, on| button(g, tip, on, false, false, editor.clone(), move |this, _, cx| this.mark(kind, cx));
        let text_blocks = !state.locked;
        let rich = !state.plain;
        // Narrow editors (AI 助手 open): wrap onto a second row inside the editor
        // instead of running under the chat panel.
        let room = editor.read(cx).list.viewport_bounds().size.width - px(16.);
        let bar = pill().px(px(4.)).py(px(6.)).gap(px(4.)).id("selection-toolbar").max_w(room).flex_wrap().on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(button(Icon::TbComment, tr("添加批注", "Add annotation"), false, comment_open, false, editor.clone(), |this, w, cx| this.start_annotation(w, cx)))
            .child(button(Icon::TbReaction, tr("添加表情回应", "Add reaction"), false, reaction_open, false, editor.clone(), |this, w, cx| this.open_reactions(w, cx)))
            .child(sep())
            .when(text_blocks, |bar| bar
                .child(button(Icon::TbType, tr("文字类型", "Text type"), false, kinds_open, true, editor.clone(), |this, w, cx| this.toggle_menu(true, w, cx)))
                .child(sep()))
            .when(rich, |bar| bar
                .child(mark(Icon::TbBold, tr("粗体", "Bold"), MarkKind::Bold, state.bold))
                .child(mark(Icon::TbItalic, tr("斜体", "Italic"), MarkKind::Italic, state.italic))
                .child(button(Icon::TbUnderline, tr("下划线", "Underline"), state.underline, false, false, editor.clone(), |this, _, cx| this.underline(cx)))
                .child(mark(Icon::TbStrike, tr("删除线", "Strikethrough"), MarkKind::Strike, state.strike))
                .child(mark(Icon::TbCode, tr("行内代码", "Inline code"), MarkKind::Code, state.code))
                .child(palette_button(state.colored, palette_open, editor.clone()))
                .child(sep()))
            .when(text_blocks, |bar| bar
                .child(button(Icon::TbList, tr("列表", "Lists"), false, lists_open, true, editor.clone(), |this, w, cx| this.toggle_menu(false, w, cx)))
                .child(sep()))
            .when(rich && !state.grid, |bar| bar
                .child(button(Icon::TbLink, tr("链接 ⌘⇧K", "Link ⌘⇧K"), state.link, link_open, false, editor.clone(), |this, w, cx| this.open_toolbar_link(w, cx))))
            .when(rich && state.grid, |bar| bar.child(disabled_icon_button(Icon::TbLink)));
        gpui::anchored().position(self.anchor).snap_to_window_with_margin(px(8.)).child(animate_bar(bar, "selection-toolbar-enter"))
    }
}

fn button(g: Icon, tip: &'static str, on: bool, open: bool, dropdown: bool, editor: Entity<Editor>,
    run: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static) -> gpui::Stateful<gpui::Div> {
    let tc = ToolbarColors::current();
    let ink = if on { tc.on_ink } else { tc.ink };
    let group = SharedString::from(format!("fb-btn-{:?}", gpui::ElementId::from(tip)));
    icon_toggle_button(tip, g, tip, on, open).h(px(28.)).min_w(px(28.))
        .when(dropdown, |b| b.child(stateful_icon(group, Icon::TbChevron, px(10.), on || open, Theme::ocean().mono_icon_colors(rgb(ink).into()), duo_icon_colors(tc, on))))
        .child(gpui::canvas(move |bounds, _, _| { BOUNDS.with(|m| { m.borrow_mut().insert(tip, bounds); }); }, |_, _, _, _| {})
            .absolute().top_0().left_0().size_full())
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| { run(this, window, cx); if this.format.is_none() { window.focus(&this.focus, cx); } });
            cx.stop_propagation();
        })
}

/// Palette button (`Text and highlight color` / `文本与高亮颜色`): toggles the color & highlight dropdown.
fn palette_button(on: bool, open: bool, editor: Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    let tip = tr("文本与高亮颜色", "Text and highlight color");
    button(Icon::TbPalette, tip, on, open, true, editor, move |this, w, cx| {
        let spec = drop_spec_for(tip, this.list.viewport_bounds(), w);
        this.toggle_palette(spec, cx);
    })
}

impl Editor {
    fn open_toolbar_link(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let trigger = drop_spec_for(tr("链接 ⌘⇧K", "Link ⌘⇧K"), self.list.viewport_bounds(), window);
        self.open_link(window, cx);
        if let Some(FormatPop::Link { spec, .. }) = &mut self.format { *spec = trigger; }
    }

    /// Opens (or closes) the text-type or list dropdown under its toolbar button.
    fn toggle_menu(&mut self, kinds: bool, window: &Window, cx: &mut Context<Self>) {
        let was = matches!((&self.format, kinds), (Some(FormatPop::Kinds { .. }), true) | (Some(FormatPop::Lists { .. }), false));
        let current = self.doc.get(self.caret.head.block).map(|b| b.kind);
        self.format = None;
        if !was {
            let key = if kinds { tr("文字类型", "Text type") } else { tr("列表", "Lists") };
            let spec = drop_spec_for(key, self.list.viewport_bounds(), window);
            let index = current.and_then(|kind| menu_kinds(kinds).iter().position(|item| *item == kind)).unwrap_or(0);
            super::menu_keys::arm_format(index);
            self.format = Some(if kinds { FormatPop::Kinds { spec, current } } else { FormatPop::Lists { spec, current } });
        }
        cx.notify();
    }
    /// A list item that is already of that kind turns back into body text.
    pub(super) fn pick_kind(&mut self, kind: Kind, list: bool, window: &mut Window, cx: &mut Context<Self>) {
        let current = self.doc.get(self.caret.head.block).map(|b| b.kind);
        self.set_kind_shortcut(if list && current == Some(kind) { Kind::Paragraph } else { kind }, cx);
        self.close_format(window, cx);
    }
}

pub(super) fn menu_kinds(kinds: bool) -> &'static [Kind] {
    if kinds { &[Kind::Paragraph, Kind::H1, Kind::H2, Kind::H3] } else { &[Kind::Bullet, Kind::Numbered, Kind::Todo] }
}

/// Dropdown body for the text-type (`kinds`) or list menu (`F-Toolbar.dc.html`).
pub(super) fn menu(editor: Entity<Editor>, kinds: bool, current: Option<Kind>) -> gpui::AnyElement {
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
    let slots = vec![crate::ui::menu_nav::Slot::Item; items.len()];
    let active = crate::ui::menu_nav::clamp_index(super::menu_keys::format_index(), &slots);
    let mut column = crate::dropdown::shell("toolbar-kind-menu").w(px(206.))
        .track_scroll(&crate::ui::menu_nav::scroll("format"));
    for (i, &(kind, label)) in items.iter().enumerate() {
        let on = current == Some(kind);
        let editor = editor.clone();
        column = column.child(
            crate::dropdown::row_with(
                ("kind-item", i + usize::from(!kinds) * 8),
                crate::dropdown::RowSpec {
                    icon: Some(super::block_menu::kind_icon(kind)),
                    label: label.into(),
                    checked: on,
                    highlighted: i == active,
                    ..Default::default()
                },
            )
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| this.pick_kind(kind, !kinds, window, cx));
                cx.stop_propagation();
            }),
        );
    }
    column.into_any_element()
}
