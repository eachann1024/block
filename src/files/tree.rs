//! Virtualized sidebar. Selection is committed only by the editor observer.
use std::path::{Path, PathBuf};
use std::sync::Arc;
use gpui::{
    App, Context, CursorStyle, Entity, FocusHandle, FontWeight, KeyBinding, MouseButton,
    UniformListScrollHandle, actions, div, prelude::*, px, uniform_list,
};
use gpui_component::input::InputState;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::Theme;
use super::index::{Row, TreeIndex};

actions!(file_tree, [RenameSelected, TrashSelected]);

#[derive(Clone)]
pub(crate) struct DragItem { pub(crate) path: PathBuf, dir: bool, label: gpui::SharedString }
impl DragItem {
    fn can_move_into(&self, folder: &Path) -> bool {
        folder != self.path.as_path() && self.path.parent() != Some(folder) && !(self.dir && folder.starts_with(&self.path))
    }
}

struct DragGhost { dir: bool, label: gpui::SharedString }
impl Render for DragGhost {
    fn render(&mut self, _: &mut gpui::Window, _: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let kind = if self.dir { Icon::Folder } else { Icon::Note };
        div().h(px(30.)).px(px(10.)).max_w(px(220.)).flex().items_center().gap(px(6.))
            .rounded(crate::ui::radius::CONTROL).bg(theme.paper).border_1().border_color(theme.accent).opacity(0.94)
            .text_size(theme.ui_font).font_weight(FontWeight(460.)).text_color(theme.text)
            .child(icon(kind, px(16.), theme.icon_colors(theme.accent)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().relative().top(LABEL_OPTICAL_NUDGE).child(self.label.clone()))
    }
}

pub fn bind_keys(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("enter", RenameSelected, Some("FileTree")),
        KeyBinding::new("f2", RenameSelected, Some("FileTree")),
        KeyBinding::new("cmd-backspace", TrashSelected, Some("FileTree")),
        KeyBinding::new("delete", TrashSelected, Some("FileTree")),
    ]);
}

pub struct FileTree {
    index: TreeIndex, selected: Option<PathBuf>, enabled: bool, scroll: UniformListScrollHandle, focus: FocusHandle,
    on_open: Option<Box<dyn Fn(PathBuf, bool, &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_rename: Option<Box<dyn Fn((PathBuf, bool), &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_trash: Option<Box<dyn Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_move: Option<Box<dyn Fn(PathBuf, PathBuf, &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_context: Option<std::rc::Rc<ContextHandler>>, menu_target: Option<PathBuf>,
    renaming: Option<(PathBuf, Entity<InputState>)>, focus_dir: Option<PathBuf>,
}

/// Right-click target: a row (path, is_dir) or the empty list area.
type ContextHandler = dyn Fn(Option<(PathBuf, bool)>, gpui::Point<gpui::Pixels>, &mut gpui::Window, &mut gpui::App);

impl FileTree {
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self { index: TreeIndex::default(), selected: None, enabled: true, scroll: UniformListScrollHandle::new(),
            focus: cx.focus_handle(), on_open: None, on_rename: None, on_trash: None, on_move: None,
            on_context: None, menu_target: None, renaming: None, focus_dir: None }
    }

    pub fn focus_handle(&self) -> &FocusHandle { &self.focus }
    #[allow(dead_code)]
    pub fn note_count(&self) -> usize { self.index.note_count() }
    pub fn is_focused(&self, window: &gpui::Window) -> bool { self.focus.is_focused(window) }
    pub fn active_target(&self) -> Option<(PathBuf, bool)> {
        self.focus_dir.as_ref().map(|d| (d.clone(), true)).or_else(|| self.selected.as_ref().map(|p| (p.clone(), false)))
    }

    pub fn set_index(&mut self, index: TreeIndex, cx: &mut Context<Self>) {
        let previous = std::mem::replace(&mut self.index, index);
        cx.background_executor().spawn(async move { drop(previous); }).detach();
        self.selected = None; self.focus_dir = None; self.scroll = UniformListScrollHandle::new(); cx.notify();
    }

    /// Rescan result after a file operation: keep expansion, selection and scroll.
    pub fn replace_index(&mut self, mut index: TreeIndex, expand: Option<PathBuf>, renamed: Option<(&Path, &Path)>, cx: &mut Context<Self>) {
        let mut expanded = self.index.expanded_paths();
        if let Some((from, to)) = renamed {
            expanded = expanded.into_iter().map(|p| super::ops::rebase(&p, from, to).unwrap_or(p)).collect();
            if let Some(p) = self.focus_dir.as_deref().and_then(|p| super::ops::rebase(p, from, to)) { self.focus_dir = Some(p); }
            if let Some(p) = self.selected.as_deref().and_then(|p| super::ops::rebase(p, from, to)) { self.selected = Some(p); }
        }
        expanded.extend(expand);
        index.restore_expanded(&expanded);
        let previous = std::mem::replace(&mut self.index, index);
        cx.background_executor().spawn(async move { drop(previous); }).detach();
        cx.notify();
    }

    pub(crate) fn can_drop_first(&self, item: &DragItem) -> bool {
        self.enabled && self.renaming.is_none() && self.index.is_root(&item.path) && self.index.siblings(&item.path).first()
            .is_some_and(|first| first != &item.path)
    }

    pub(crate) fn drop_first(&mut self, item: &DragItem, cx: &mut Context<Self>) {
        if let Some((_, first, _)) = self.index.row(0) {
            self.reorder_item(&item.path, &first.path, false, cx);
        }
    }

    fn reorder_item(&mut self, source: &Path, target: &Path, after: bool, cx: &mut Context<Self>) {
        if !self.enabled || self.renaming.is_some() || source.parent() != target.parent() { return; }
        let Some(parent) = source.parent() else { return };
        let mut order = self.index.siblings(source);
        if !crate::ui::reorder::move_to(&mut order, &source.to_path_buf(), &target.to_path_buf(), after) { return; }
        if super::update_state(|state| { state.file_order.insert(parent.to_path_buf(), order); }).is_err() { return; }
        self.index.reorder(source, target, after);
        cx.notify();
    }

    pub fn on_context(&mut self, handler: impl Fn(Option<(PathBuf, bool)>, gpui::Point<gpui::Pixels>, &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_context = Some(std::rc::Rc::new(handler));
    }
    pub fn set_menu_target(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) { if self.menu_target != path { self.menu_target = path; cx.notify(); } }
    pub fn set_enabled(&mut self, enabled: bool, cx: &mut Context<Self>) { if self.enabled != enabled { self.enabled = enabled; cx.notify(); } }
    pub fn select(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) {
        let next_focus_dir = if path.is_some() { None } else { self.focus_dir.clone() };
        if self.selected == path && self.focus_dir == next_focus_dir { return; }
        self.focus_dir = next_focus_dir; self.selected = path; cx.notify();
    }
    pub fn focus_dir(&self) -> Option<&Path> { self.focus_dir.as_deref() }
    pub fn selected(&self) -> Option<&Path> { self.selected.as_deref() }
    pub fn set_renaming(&mut self, renaming: Option<(PathBuf, Entity<InputState>)>, cx: &mut Context<Self>) { self.renaming = renaming; cx.notify(); }
    pub fn finish_rename(&mut self, path: &Path, stem: gpui::SharedString, cx: &mut Context<Self>) { self.renaming = None; self.index.set_stem(path, stem); cx.notify(); }
    pub fn folders(&self) -> Vec<(PathBuf, usize, String)> { self.index.folders() }

    pub fn scroll_to(&mut self, path: &Path) {
        use gpui_component::scroll::ScrollbarHandle as _;
        if let Some(rank) = self.index.visible_rank(path) {
            let vh = self.scroll.viewport_bounds().size.height;
            let top = -self.scroll.offset().y;
            let y = px(rank as f32 * 34.);
            if vh <= px(0.) || y < top || y + px(34.) > top + vh {
                self.scroll.scroll_to_item(rank, gpui::ScrollStrategy::Center);
            }
        }
    }

    pub fn contains_root_path(&self, path: &Path) -> bool { self.index.contains_root_path(path) }
    pub fn has_notes(&self) -> bool { self.index.has_notes() }
    pub fn insert_note(&mut self, path: PathBuf, cx: &mut Context<Self>) { self.index.insert_note(path); cx.notify(); }
    pub fn on_open(&mut self, handler: impl Fn(PathBuf, bool, &mut gpui::Window, &mut gpui::App) + 'static) { self.on_open = Some(Box::new(handler)); }
    pub fn on_rename(&mut self, handler: impl Fn((PathBuf, bool), &mut gpui::Window, &mut gpui::App) + 'static) { self.on_rename = Some(Box::new(handler)); }
    pub fn on_trash(&mut self, handler: impl Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static) { self.on_trash = Some(Box::new(handler)); }
    pub fn on_move(&mut self, handler: impl Fn(PathBuf, PathBuf, &mut gpui::Window, &mut gpui::App) + 'static) { self.on_move = Some(Box::new(handler)); }
}

impl Render for FileTree {
    fn render(&mut self, _: &mut gpui::Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        div().id("file-tree-root").size_full().relative().track_focus(&self.focus)
            .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
            .on_drag_move::<DragItem>(cx.listener(|this, event: &gpui::DragMoveEvent<DragItem>, _, cx| {
                let bounds = this.scroll.0.borrow().base_handle.bounds();
                if bounds.size.height > px(0.) {
                    let y = event.event.position.y;
                    let offset = this.scroll.0.borrow().base_handle.offset();
                    if y < bounds.top() + px(28.) {
                        this.scroll.0.borrow().base_handle.set_offset(gpui::point(offset.x, (offset.y + px(14.)).min(px(0.))));
                    } else if y > bounds.bottom() - px(28.) {
                        this.scroll.0.borrow().base_handle.set_offset(gpui::point(offset.x, offset.y - px(14.)));
                    }
                }
                cx.notify();
            }))
            .when(self.renaming.is_none(), |d| d.key_context("FileTree"))
            .on_action(cx.listener(|this, _: &RenameSelected, window, cx| {
                if !this.enabled || this.renaming.is_some() { return; }
                if let (Some(target), Some(handler)) = (this.active_target(), &this.on_rename) { handler(target, window, cx); }
            }))
            .on_action(cx.listener(|this, _: &TrashSelected, window, cx| {
                if !this.enabled || this.renaming.is_some() { return; }
                if let (Some((path, _)), Some(handler)) = (this.active_target(), &this.on_trash) { handler(path, window, cx); }
            }))
            .child(
                uniform_list("file-list", self.index.len(), cx.processor(move |this, range: std::ops::Range<usize>, _, cx| {
                    range.filter_map(|rank| this.index.row(rank)).map(|(index, row, expanded)| {
                        let target = this.menu_target.as_deref() == Some(row.path.as_ref());
                        let focused = this.focus_dir.as_deref() == Some(row.path.as_ref());
                        let editing = this.renaming.as_ref().filter(|(p, _)| p.as_path() == row.path.as_ref()).map(|(_, input)| input.clone());
                        div().w_full().pb(px(2.))
                            .child(file_row(index, row, expanded, this.selected.as_deref(), this.enabled, editing, target, focused, cx))
                    }).collect::<Vec<_>>()
                }))
                .track_scroll(&self.scroll)
                .on_mouse_down(MouseButton::Right, cx.listener(|this, event: &gpui::MouseDownEvent, window, cx| {
                    if !this.enabled { return; }
                    if let Some(handler) = this.on_context.clone() { handler(None, event.position, window, cx); }
                }))
                .size_full().px(px(12.)).text_size(theme.ui_font).text_color(theme.text),
            )
            .child(crate::ui::scrollbar::vertical("tree-scrollbar", &self.scroll, px(0.)))
    }
}

/// GPUI centers labels on the font em box, which sits a little below the row icons.
/// 1.5px lifts Latin lowercase onto the icon but pushes CJK above it. Half a pixel
/// lines the character body up with the icon and keeps every row on one baseline.
const LABEL_OPTICAL_NUDGE: gpui::Pixels = px(-0.5);

fn file_row(index: usize, row: Arc<Row>, expanded: bool, selected: Option<&Path>, enabled: bool,
    editing: Option<Entity<InputState>>, target: bool, focused: bool, cx: &mut Context<FileTree>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let path = row.path.clone();
    let dir = row.dir;
    let is_selected = selected == Some(path.as_ref());
    let is_editing = editing.is_some();
    let is_target = target;
    let (arrow, kind) = match (dir, expanded) {
        (false, _) => (None, Icon::Note),
        (true, false) => (Some(Icon::ChevronRight), Icon::Folder),
        (true, true) => (Some(Icon::ChevronDown), Icon::FolderOpen),
    };
    let active_folder = focused && selected.is_none();
    let highlighted = is_selected || active_folder;
    let rename_vis = editing.as_ref().map(|input| crate::ui::rename_row::inspect(&path, dir, input, cx));
    let row_active = highlighted || is_target || rename_vis.is_some();
    let kind_icon = match &rename_vis {
        Some(vis) => icon(kind, px(16.), vis.icon_colors),
        None => stateful_icon("tree-row", kind, px(16.), highlighted || is_target, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if highlighted { theme.accent } else { theme.text })),
    };
    let label = row.stem.clone();
    let drag_item = DragItem { path: path.to_path_buf(), dir, label: label.clone() };
    div().id(path.clone()).group("tree-row").w_full().h(theme.menu_item_height).line_height(px(20.))
        .pl(px(8. + row.depth as f32 * 20.)).pr(px(8.)).flex().items_center().gap(px(6.)).rounded(crate::ui::radius::CONTROL)
        .border_1().border_color(gpui::transparent_black())
        .when(enabled, |row| row.cursor(CursorStyle::PointingHand))
        .when(highlighted, |row| row.bg(theme.selected).border_color(theme.accent).font_weight(FontWeight(460.)))
        .when(focused && !highlighted, |row| row.bg(theme.selected))
        .when(is_target && !highlighted && !focused, |row| row.bg(theme.hover))
        .when(!is_editing && enabled, |row| row.hover(move |style| if highlighted || focused { style } else { style.bg(theme.hover) }))
        .when(enabled && !is_editing, |row| {
            row.on_drag(drag_item, |item, _, _, cx| { crate::dropdown::dismiss_all(cx); cx.new(|_| DragGhost { dir: item.dir, label: item.label.clone() }) })
        })
        .when(enabled && !is_editing, |row| {
            let folder = path.clone();
            let drop_folder = path.clone();
            let position = crate::ui::reorder::DropPosition::default();
            let row = crate::ui::reorder::insertion_target::<DragItem, _>(row, position.clone(), dir, move |item| {
                item.path != folder.as_ref() && (item.path.parent() == folder.parent() || (dir && item.can_move_into(&folder)))
            });
            let allowed_folder = path.clone();
            let allowed_position = position.clone();
            row.can_drop(move |value, window, _| {
                value.downcast_ref::<DragItem>().is_some_and(|item| {
                    if dir && allowed_position.middle(window) { item.can_move_into(&allowed_folder) }
                    else { item.path != allowed_folder.as_ref() && item.path.parent() == allowed_folder.parent() }
                })
            }).on_drop(cx.listener(move |this, item: &DragItem, window, cx| {
                if !this.enabled || this.renaming.is_some() { return; }
                if dir && position.middle(window) {
                    if item.can_move_into(&drop_folder) {
                        if let Some(handler) = &this.on_move { handler(item.path.clone(), drop_folder.to_path_buf(), window, cx); }
                    }
                } else {
                    this.reorder_item(&item.path, &drop_folder, position.after(window), cx);
                }
            }))
        })
        .when_some(arrow, |row, a| row.child(
            div().size(px(18.)).flex_none().flex().items_center().justify_center()
                .child(stateful_icon("tree-row", a, px(18.), row_active, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text_secondary)))))
        .child(div().size(px(16.)).flex_none().flex().items_center().justify_center().child(kind_icon))
        .when_some(rename_vis.as_ref(), |row, vis| crate::ui::rename_row::style_capsule(row, vis))
        .child(match &editing {
            Some(input) => crate::ui::rename_row::input_slot(input).relative().top(LABEL_OPTICAL_NUDGE),
            None => div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().relative().top(LABEL_OPTICAL_NUDGE).child(label),
        })
        .when_some(editing.as_ref().zip(rename_vis.as_ref()), |row, (input, vis)| row.child(crate::ui::rename_row::trailing_slot(input, vis)))
        // Hover "⋯": opens the same menu as a right-click, anchored under the button.
        .when(!is_editing, |row| row.child(
            div().id("row-more").group("row-more-btn").size(px(22.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_MD)
                .invisible()
                .when(enabled, |b| b.group_hover("tree-row", |style| style.visible()))
                .when(enabled && is_target, |b| b.visible().bg(theme.btn_hover))
                .hover(|style| style.bg(theme.btn_hover))
                .on_mouse_down(MouseButton::Left, cx.listener({
                    let path = path.clone();
                    move |this, event: &gpui::MouseDownEvent, window, cx| {
                        cx.stop_propagation();
                        if !this.enabled || this.renaming.is_some() { return; }
                        this.menu_target = Some(path.to_path_buf());
                        cx.notify();
                        if let Some(handler) = this.on_context.clone() { handler(Some((path.to_path_buf(), dir)), event.position, window, cx); }
                    }
                }))
                .child(stateful_icon("row-more-btn", Icon::More, px(14.), is_target, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))))
        .on_mouse_down(MouseButton::Right, cx.listener({
            let path = path.clone();
            move |this, event: &gpui::MouseDownEvent, window, cx| {
                cx.stop_propagation();
                if !this.enabled { return; }
                this.menu_target = Some(path.to_path_buf());
                cx.notify();
                if let Some(handler) = this.on_context.clone() { handler(Some((path.to_path_buf(), dir)), event.position, window, cx); }
            }
        }))
        .on_mouse_down(MouseButton::Middle, cx.listener({
            let path = path.clone();
            move |this, _, window, cx| {
                cx.stop_propagation();
                if !this.enabled || this.renaming.is_some() || dir { return; }
                window.focus(&this.focus, cx);
                if this.focus_dir.take().is_some() { cx.notify(); }
                if let Some(handler) = &this.on_open { handler(path.to_path_buf(), true, window, cx); }
            }
        }))
        .when(is_editing, |row| row.on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation()))
        .when(!is_editing, |row| {
            row.on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| if this.enabled && this.renaming.is_some() { window.focus(&this.focus, cx); }))
                .on_click(cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                    if !this.enabled || this.renaming.is_some() { return; }
                    window.focus(&this.focus, cx);
                    if dir { this.index.toggle(index, &path); this.focus_dir = Some(path.to_path_buf()); cx.notify(); }
                    else {
                        if this.focus_dir.take().is_some() { cx.notify(); }
                        if let Some(handler) = &this.on_open { let m = event.modifiers(); handler(path.to_path_buf(), m.platform || m.control, window, cx); }
                    }
                }))
        })
}
