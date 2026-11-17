//! Floating selection toolbar (F-Toolbar): single-row pill that folds overflow items into `•••`.
use std::{cell::RefCell, collections::HashMap};
use gpui::{Bounds, Context, Entity, MouseButton, Pixels, Point, SharedString, Window,
    prelude::*, px, rgb};
use crate::{
    dropdown::{self, RowSpec},
    icons::{Icon, stateful_icon},
    theme::{Theme, is_en, tr},
    ui::menu_nav::Slot,
};
pub(super) use crate::ui::floating_bar::{
    ToolbarColors, ai_action_button, animate as animate_bar, duo_icon_colors, icon_toggle_button, sep, shell as pill,
};
use super::{format_cmds::{FormatPop, ToolbarState}, model::{Kind, MarkKind}, view::Editor};

thread_local! { static BOUNDS: RefCell<HashMap<&'static str, Bounds<Pixels>>> = Default::default(); }

pub(super) fn more_key() -> &'static str {
    tr("更多格式", "More formatting")
}

/// Placement spec for a toolbar dropdown button: left-aligned to the button (or `•••` when overflowed), flipping above if needed.
pub(super) fn drop_spec_for(key: &'static str, viewport: Bounds<Pixels>, window: &Window) -> crate::dropdown::PlacementSpec {
    BOUNDS
        .with(|b| {
            let map = b.borrow();
            map.get(key).or_else(|| map.get(more_key())).copied()
        })
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

#[derive(Clone, Copy, PartialEq, Eq)]
enum ToolbarItem {
    AiRewrite,
    Comment,
    Reaction,
    TextType,
    Bold,
    Italic,
    Underline,
    Strike,
    Code,
    Palette,
    Lists,
    Link,
}

impl ToolbarItem {
    fn group(self) -> u8 {
        match self {
            Self::AiRewrite => 0,
            Self::Comment | Self::Reaction => 1,
            Self::TextType => 2,
            Self::Bold | Self::Italic | Self::Underline | Self::Strike | Self::Code | Self::Palette => 3,
            Self::Lists => 4,
            Self::Link => 5,
        }
    }

    fn width(self) -> f32 {
        match self {
            Self::AiRewrite => if is_en() { 116. } else { 96. },
            Self::TextType | Self::Palette | Self::Lists => 41.,
            _ => 28.,
        }
    }

    fn icon(self) -> Icon {
        match self {
            Self::AiRewrite => Icon::Sparkles,
            Self::Comment => Icon::TbComment,
            Self::Reaction => Icon::TbReaction,
            Self::TextType => Icon::TbType,
            Self::Bold => Icon::TbBold,
            Self::Italic => Icon::TbItalic,
            Self::Underline => Icon::TbUnderline,
            Self::Strike => Icon::TbStrike,
            Self::Code => Icon::TbCode,
            Self::Palette => Icon::TbPalette,
            Self::Lists => Icon::TbList,
            Self::Link => Icon::TbLink,
        }
    }

    fn tip(self) -> &'static str {
        match self {
            Self::AiRewrite => tr("AI 改写", "AI Rewrite"),
            Self::Comment => tr("添加批注", "Add annotation"),
            Self::Reaction => tr("添加表情回应", "Add reaction"),
            Self::TextType => tr("文字类型", "Text type"),
            Self::Bold => tr("粗体", "Bold"),
            Self::Italic => tr("斜体", "Italic"),
            Self::Underline => tr("下划线", "Underline"),
            Self::Strike => tr("删除线", "Strikethrough"),
            Self::Code => tr("行内代码", "Inline code"),
            Self::Palette => tr("文本与高亮颜色", "Text and highlight color"),
            Self::Lists => tr("列表", "Lists"),
            Self::Link => tr("链接 ⌘⇧K", "Link ⌘⇧K"),
        }
    }

    fn menu_label(self) -> &'static str {
        match self {
            Self::Link => tr("链接", "Link"),
            _ => self.tip(),
        }
    }

    fn hint(self) -> Option<&'static str> {
        let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
        match self {
            Self::Comment => Some(if mac { "⌥⌘M" } else { "Ctrl+Alt+M" }),
            Self::Bold => Some(if mac { "⌘B" } else { "Ctrl+B" }),
            Self::Italic => Some(if mac { "⌘I" } else { "Ctrl+I" }),
            Self::Underline => Some(if mac { "⌘U" } else { "Ctrl+U" }),
            Self::Strike => Some(if mac { "⇧⌘S" } else { "Ctrl+Shift+S" }),
            Self::Code => Some(if mac { "⌘E" } else { "Ctrl+E" }),
            Self::Link => Some(if mac { "⇧⌘K" } else { "Ctrl+Shift+K" }),
            _ => None,
        }
    }

    fn has_submenu(self) -> bool {
        matches!(self, Self::TextType | Self::Palette | Self::Lists)
    }

    fn is_on(self, state: &ToolbarState) -> bool {
        match self {
            Self::Bold => state.bold,
            Self::Italic => state.italic,
            Self::Underline => state.underline,
            Self::Strike => state.strike,
            Self::Code => state.code,
            Self::Palette => state.colored,
            Self::Link => state.link,
            _ => false,
        }
    }

    fn is_open(self, format: &Option<FormatPop>, comment_open: bool, reaction_open: bool) -> bool {
        match self {
            Self::AiRewrite => matches!(format, Some(FormatPop::Ai(_))),
            Self::Comment => comment_open,
            Self::Reaction => reaction_open,
            Self::TextType => matches!(format, Some(FormatPop::Kinds { .. })),
            Self::Palette => matches!(format, Some(FormatPop::Palette { .. })),
            Self::Lists => matches!(format, Some(FormatPop::Lists { .. })),
            Self::Link => matches!(format, Some(FormatPop::Link { .. })),
            _ => false,
        }
    }
}

fn sequence_width(items: &[ToolbarItem], with_more: bool) -> f32 {
    if items.is_empty() && !with_more { return 0.; }
    let mut w = 14.;
    let mut prev_group: Option<u8> = None;
    for &item in items {
        if let Some(g) = prev_group {
            w += if g == item.group() { 2. } else { 13. };
        }
        w += item.width();
        prev_group = Some(item.group());
    }
    if with_more {
        if prev_group.is_some() { w += 13.; }
        w += 28.;
    }
    w
}

fn split_for_room(enabled: &[ToolbarItem], room: f32) -> (usize, f32) {
    let full = sequence_width(enabled, false);
    if full <= room {
        return (enabled.len(), full);
    }
    let keep = (0..enabled.len())
        .rev()
        .find(|&k| sequence_width(&enabled[..k], true) <= room)
        .unwrap_or(0);
    (keep, sequence_width(&enabled[..keep], true))
}

pub(super) fn selection_toolbar(editor: Entity<Editor>, _id: u64, anchor: Point<Pixels>) -> SelectionToolbar { SelectionToolbar { editor, anchor } }

/// Single-row selection toolbar; trailing items that do not fit fold into `•••` (like Notion / Feishu).
#[derive(IntoElement)]
pub(super) struct SelectionToolbar { editor: Entity<Editor>, anchor: Point<Pixels> }
impl RenderOnce for SelectionToolbar {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        let editor = self.editor;
        let (state, inline, overflow, more_open, comment_open, reaction_open, format_snapshot, dismiss_empty_more) = {
            let ed = editor.read(cx);
            let state = ed.toolbar_state();
            let enabled = ed.enabled_toolbar_items(&state);
            let room = (f32::from(ed.list.viewport_bounds().size.width) - 16.).max(48.);
            let (keep, _) = split_for_room(&enabled, room);
            let inline = enabled[..keep].to_vec();
            let overflow = enabled[keep..].to_vec();
            let comment_open = ed.annotate.composer.is_some();
            let reaction_open = ed.annotate.reactions.is_some();
            let raw_more = matches!(ed.format, Some(FormatPop::More { .. }));
            let more_open = raw_more || overflow.iter().any(|item| item.is_open(&ed.format, comment_open, reaction_open));
            (state, inline, overflow, more_open, comment_open, reaction_open, ed.format.clone(), raw_more && keep == enabled.len())
        };
        if dismiss_empty_more {
            let ed = editor.clone();
            cx.defer(move |cx| {
                ed.update(cx, |this, cx| {
                    if matches!(this.format, Some(FormatPop::More { .. })) {
                        this.format = None;
                        cx.notify();
                    }
                });
            });
        }
        BOUNDS.with(|m| {
            m.borrow_mut().retain(|&k, _| {
                (!overflow.is_empty() && k == more_key()) || inline.iter().any(|item| item.tip() == k)
            });
        });

        let mut bar = pill()
            .id("selection-toolbar")
            .flex_nowrap()
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
        let mut prev_group: Option<u8> = None;
        for &item in &inline {
            if let Some(g) = prev_group && g != item.group() {
                bar = bar.child(sep());
            }
            prev_group = Some(item.group());
            let open = item.is_open(&format_snapshot, comment_open, reaction_open);
            let on = item.is_on(&state);
            if item == ToolbarItem::AiRewrite {
                let ai_ed = editor.clone();
                bar = bar.child(ai_action_button("toolbar-ai-rewrite", item.tip(), open, move |_, window, cx| {
                    ai_ed.update(cx, |this, cx| this.open_ai_selection(window, cx));
                    cx.stop_propagation();
                }));
            } else {
                bar = bar.child(button(item.icon(), item.tip(), on, open, item.has_submenu(), editor.clone(), move |this, w, cx| {
                    this.run_toolbar_item(item, false, w, cx);
                }));
            }
        }
        if !overflow.is_empty() {
            if prev_group.is_some() {
                bar = bar.child(sep());
            }
            let more_on = overflow.iter().any(|item| item.is_on(&state));
            bar = bar.child(button(Icon::More, more_key(), more_on, more_open, false, editor.clone(), |this, w, cx| {
                this.toggle_more_menu(w, cx);
            }));
        }
        gpui::anchored().position(self.anchor).snap_to_window_with_margin(px(8.)).child(animate_bar(bar, "selection-toolbar-enter"))
    }
}

fn button(g: Icon, tip: &'static str, on: bool, open: bool, dropdown: bool, editor: Entity<Editor>,
    run: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static) -> gpui_base::Button {
    let tc = ToolbarColors::current();
    let ink = if on { tc.on_ink } else { tc.ink };
    let group = SharedString::from(format!("fb-btn-{:?}", gpui::ElementId::from(tip)));
    icon_toggle_button(tip, g, tip, on, open).h(px(28.)).min_w(px(28.))
        .when(dropdown, |b| b.child(stateful_icon(group, Icon::TbChevron, px(crate::ui::icon_size::MICRO), on || open, Theme::ocean().mono_icon_colors(rgb(ink).into()), duo_icon_colors(tc, on))))
        .child(gpui::canvas(move |bounds, _, _| { BOUNDS.with(|m| { m.borrow_mut().insert(tip, bounds); }); }, |_, _, _, _| {})
            .absolute().top_0().left_0().size_full())
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| {
                run(this, window, cx);
                if this.format.is_none() && this.annotate.composer.is_none() { window.focus(&this.focus, cx); }
            });
            cx.stop_propagation();
        })
}

enum OverflowEntry {
    Item(ToolbarItem),
    Separator,
}

fn overflow_entries(items: &[ToolbarItem]) -> Vec<OverflowEntry> {
    let mut entries = Vec::new();
    let mut prev_group: Option<u8> = None;
    for &item in items {
        if let Some(g) = prev_group && g != item.group() {
            entries.push(OverflowEntry::Separator);
        }
        prev_group = Some(item.group());
        entries.push(OverflowEntry::Item(item));
    }
    entries
}

pub(super) fn more_slots(editor: &Editor) -> Vec<Slot> {
    let state = editor.toolbar_state();
    let overflow = editor.overflow_toolbar_items(&state);
    overflow_entries(&overflow)
        .into_iter()
        .map(|entry| match entry {
            OverflowEntry::Item(_) => Slot::Item,
            OverflowEntry::Separator => Slot::Skip,
        })
        .collect()
}

#[derive(IntoElement)]
pub(super) struct MorePopover {
    pub editor: Entity<Editor>,
}

impl RenderOnce for MorePopover {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        more_menu(self.editor, cx)
    }
}

fn more_menu(editor: Entity<Editor>, cx: &gpui::App) -> gpui::AnyElement {
    let (state, entries) = {
        let ed = editor.read(cx);
        let state = ed.toolbar_state();
        let overflow = ed.overflow_toolbar_items(&state);
        (state, overflow_entries(&overflow))
    };
    if entries.is_empty() {
        return gpui::div().into_any_element();
    }
    let slots: Vec<Slot> = entries.iter().map(|e| match e {
        OverflowEntry::Item(_) => Slot::Item,
        OverflowEntry::Separator => Slot::Skip,
    }).collect();
    let active = crate::ui::menu_nav::clamp_index(super::menu_keys::format_index(), &slots);
    let mut column = dropdown::shell("toolbar-more-menu")
        .w(px(228.))
        .track_scroll(&crate::ui::menu_nav::scroll("format"));
    for (i, entry) in entries.into_iter().enumerate() {
        match entry {
            OverflowEntry::Separator => {
                column = column.child(dropdown::separator());
            }
            OverflowEntry::Item(item) => {
                let on = item.is_on(&state);
                let ed = editor.clone();
                column = column.child(
                    dropdown::row_with(
                        ("toolbar-more-item", i),
                        RowSpec {
                            icon: Some(item.icon()),
                            label: item.menu_label().into(),
                            checked: on,
                            highlighted: i == active,
                            hint: item.hint().map(Into::into),
                            chevron: item.has_submenu(),
                            ..Default::default()
                        },
                    )
                    .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        ed.update(cx, |this, cx| {
                            this.run_toolbar_item(item, true, window, cx);
                            if this.format.is_none() && this.annotate.composer.is_none() {
                                window.focus(&this.focus, cx);
                            }
                        });
                        cx.stop_propagation();
                    }),
                );
            }
        }
    }
    column.into_any_element()
}

impl Editor {
    fn enabled_toolbar_items(&self, state: &ToolbarState) -> Vec<ToolbarItem> {
        let can_ai = !self.minimal && self.is_markdown_note() && self.can_replace_selection() && !self.selection_ranges().is_empty();
        let available = state.available;
        let mut items = Vec::with_capacity(12);
        if can_ai { items.push(ToolbarItem::AiRewrite); }
        items.push(ToolbarItem::Comment);
        items.push(ToolbarItem::Reaction);
        if available.block_type { items.push(ToolbarItem::TextType); }
        if available.rich_text {
            if available.bold { items.push(ToolbarItem::Bold); }
            items.push(ToolbarItem::Italic);
            items.push(ToolbarItem::Underline);
            items.push(ToolbarItem::Strike);
            items.push(ToolbarItem::Code);
            items.push(ToolbarItem::Palette);
        }
        if available.block_type { items.push(ToolbarItem::Lists); }
        if available.link { items.push(ToolbarItem::Link); }
        items
    }

    fn overflow_toolbar_items(&self, state: &ToolbarState) -> Vec<ToolbarItem> {
        let enabled = self.enabled_toolbar_items(state);
        let room = (f32::from(self.list.viewport_bounds().size.width) - 16.).max(48.);
        let (keep, _) = split_for_room(&enabled, room);
        enabled[keep..].to_vec()
    }

    pub(super) fn selection_toolbar_width(&self, room: Pixels) -> Pixels {
        let state = self.toolbar_state();
        let enabled = self.enabled_toolbar_items(&state);
        let (_, width) = split_for_room(&enabled, f32::from(room).max(48.));
        px(width)
    }

    fn run_toolbar_item(&mut self, item: ToolbarItem, from_more: bool, window: &mut Window, cx: &mut Context<Self>) {
        if from_more && !matches!(item, ToolbarItem::TextType | ToolbarItem::Palette | ToolbarItem::Lists) {
            self.format = None;
        }
        match item {
            ToolbarItem::AiRewrite => self.open_ai_selection(window, cx),
            ToolbarItem::Comment => self.start_annotation(window, cx),
            ToolbarItem::Reaction => self.open_reactions(window, cx),
            ToolbarItem::TextType => self.toggle_menu(true, window, cx),
            ToolbarItem::Bold => self.mark(MarkKind::Bold, cx),
            ToolbarItem::Italic => self.mark(MarkKind::Italic, cx),
            ToolbarItem::Underline => self.underline(cx),
            ToolbarItem::Strike => self.mark(MarkKind::Strike, cx),
            ToolbarItem::Code => self.mark(MarkKind::Code, cx),
            ToolbarItem::Palette => {
                let spec = drop_spec_for(item.tip(), self.list.viewport_bounds(), window);
                self.toggle_palette(spec, cx);
            }
            ToolbarItem::Lists => self.toggle_menu(false, window, cx),
            ToolbarItem::Link => self.open_toolbar_link(window, cx),
        }
    }

    pub(super) fn activate_more_slot(&mut self, slot_index: usize, window: &mut Window, cx: &mut Context<Self>) {
        let state = self.toolbar_state();
        let entries = overflow_entries(&self.overflow_toolbar_items(&state));
        if let Some(OverflowEntry::Item(item)) = entries.get(slot_index) {
            self.run_toolbar_item(*item, true, window, cx);
            if self.format.is_none() && self.annotate.composer.is_none() {
                window.focus(&self.focus, cx);
            }
        }
    }

    fn toggle_more_menu(&mut self, window: &Window, cx: &mut Context<Self>) {
        let state = self.toolbar_state();
        let overflow = self.overflow_toolbar_items(&state);
        let comment_open = self.annotate.composer.is_some();
        let reaction_open = self.annotate.reactions.is_some();
        let was_open = matches!(self.format, Some(FormatPop::More { .. }))
            || overflow.iter().any(|item| item.is_open(&self.format, comment_open, reaction_open));
        self.format = None;
        if !was_open && !overflow.is_empty() {
            let spec = drop_spec_for(more_key(), self.list.viewport_bounds(), window);
            super::menu_keys::arm_format(0);
            self.format = Some(FormatPop::More { spec });
        }
        cx.notify();
    }

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
