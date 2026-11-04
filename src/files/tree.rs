//! Virtualized sidebar. Selection is committed only by the editor observer.
use std::path::{Path, PathBuf};
use std::sync::Arc;
use gpui::{
    App, Context, CursorStyle, Entity, FocusHandle, FontWeight, KeyBinding, MouseButton,
    UniformListScrollHandle, actions, div, prelude::*, px, uniform_list,
};
use gpui_component::input::{Input, InputState};
use crate::icons::{Icon, icon};
use crate::theme::Theme;
use super::index::{Row, TreeIndex};

actions!(file_tree, [RenameSelected, TrashSelected]);

pub fn bind_keys(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("enter", RenameSelected, Some("FileTree")),
        KeyBinding::new("f2", RenameSelected, Some("FileTree")),
        KeyBinding::new("cmd-backspace", TrashSelected, Some("FileTree")),
        KeyBinding::new("delete", TrashSelected, Some("FileTree")),
    ]);
}

pub struct FileTree {
    index: TreeIndex,
    selected: Option<PathBuf>,
    enabled: bool,
    scroll: UniformListScrollHandle,
    focus: FocusHandle,
    on_open: Option<Box<dyn Fn(PathBuf, bool, &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_rename: Option<Box<dyn Fn((PathBuf, bool), &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_trash: Option<Box<dyn Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static>>,
    on_context: Option<std::rc::Rc<ContextHandler>>,
    menu_target: Option<PathBuf>,
    /// Row whose name is being edited in place.
    renaming: Option<(PathBuf, Entity<InputState>)>,
    /// Folder row last clicked: the target of new notes and folders.
    focus_dir: Option<PathBuf>,
}

/// Right-click target: a row (path, is_dir) or the empty list area.
type ContextHandler = dyn Fn(Option<(PathBuf, bool)>, gpui::Point<gpui::Pixels>, &mut gpui::Window, &mut gpui::App);

impl FileTree {
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self { index: TreeIndex::default(), selected: None, enabled: true,
            scroll: UniformListScrollHandle::new(), focus: cx.focus_handle(),
            on_open: None, on_rename: None, on_trash: None, on_context: None, menu_target: None,
            renaming: None, focus_dir: None }
    }

    pub fn focus_handle(&self) -> &FocusHandle { &self.focus }
    pub fn note_count(&self) -> usize { self.index.note_count() }

    pub fn is_focused(&self, window: &gpui::Window) -> bool { self.focus.is_focused(window) }

    pub fn active_target(&self) -> Option<(PathBuf, bool)> {
        if let Some(dir) = &self.focus_dir {
            Some((dir.clone(), true))
        } else {
            self.selected.as_ref().map(|p| (p.clone(), false))
        }
    }

    pub fn set_index(&mut self, index: TreeIndex, cx: &mut Context<Self>) {
        let previous = std::mem::replace(&mut self.index, index);
        // Destroying an old million-node tree is worker work as well.
        cx.background_executor().spawn(async move { drop(previous); }).detach();
        self.selected = None;
        self.focus_dir = None;
        self.scroll = UniformListScrollHandle::new();
        cx.notify();
    }

    /// Rescan result after a file operation: keep expansion, selection and scroll.
    pub fn replace_index(&mut self, mut index: TreeIndex, expand: Option<PathBuf>, cx: &mut Context<Self>) {
        let mut expanded = self.index.expanded_paths();
        expanded.extend(expand);
        index.restore_expanded(&expanded);
        let previous = std::mem::replace(&mut self.index, index);
        cx.background_executor().spawn(async move { drop(previous); }).detach();
        cx.notify();
    }

    pub fn on_context(&mut self, handler: impl Fn(Option<(PathBuf, bool)>, gpui::Point<gpui::Pixels>, &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_context = Some(std::rc::Rc::new(handler));
    }

    pub fn set_menu_target(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) {
        if self.menu_target != path { self.menu_target = path; cx.notify(); }
    }

    pub fn set_enabled(&mut self, enabled: bool, cx: &mut Context<Self>) {
        if self.enabled != enabled { self.enabled = enabled; cx.notify(); }
    }

    pub fn select(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) {
        let next_focus_dir = if path.is_some() { None } else { self.focus_dir.clone() };
        if self.selected == path && self.focus_dir == next_focus_dir { return; }
        self.focus_dir = next_focus_dir;
        self.selected = path;
        cx.notify();
    }

    pub fn focus_dir(&self) -> Option<&Path> { self.focus_dir.as_deref() }

    pub fn selected(&self) -> Option<&Path> { self.selected.as_deref() }

    pub fn set_renaming(&mut self, renaming: Option<(PathBuf, Entity<InputState>)>, cx: &mut Context<Self>) {
        self.renaming = renaming;
        cx.notify();
    }

    pub fn folders(&self) -> Vec<(PathBuf, usize, String)> { self.index.folders() }

    pub fn scroll_to(&mut self, path: &Path) {
        if let Some(rank) = self.index.visible_rank(path) {
            self.scroll.scroll_to_item(rank, gpui::ScrollStrategy::Center);
        }
    }

    pub fn contains_root_path(&self, path: &Path) -> bool { self.index.contains_root_path(path) }

    pub fn has_notes(&self) -> bool { self.index.has_notes() }

    pub fn insert_note(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.index.insert_note(path);
        cx.notify();
    }

    pub fn on_open(&mut self, handler: impl Fn(PathBuf, bool, &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_open = Some(Box::new(handler));
    }

    pub fn on_rename(&mut self, handler: impl Fn((PathBuf, bool), &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_rename = Some(Box::new(handler));
    }

    pub fn on_trash(&mut self, handler: impl Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_trash = Some(Box::new(handler));
    }
}

impl Render for FileTree {
    fn render(&mut self, _: &mut gpui::Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        div()
            .id("file-tree-root")
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
            .when(self.renaming.is_none(), |d| d.key_context("FileTree"))
            .on_action(cx.listener(|this, _: &RenameSelected, window, cx| {
                if !this.enabled || this.renaming.is_some() { return; }
                if let (Some(target), Some(handler)) = (this.active_target(), &this.on_rename) {
                    handler(target, window, cx);
                }
            }))
            .on_action(cx.listener(|this, _: &TrashSelected, window, cx| {
                if !this.enabled || this.renaming.is_some() { return; }
                if let (Some((path, _)), Some(handler)) = (this.active_target(), &this.on_trash) {
                    handler(path, window, cx);
                }
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
                .size_full()
                .px(px(12.))
                .text_size(theme.ui_font)
                .text_color(theme.text),
            )
            .child(crate::ui::scrollbar::vertical("tree-scrollbar", &self.scroll, px(0.)))
    }
}

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
    let chevron_colors = theme.icon_colors(theme.text_secondary);
    let icon_colors = theme.icon_colors(if highlighted { theme.accent } else { theme.text_secondary });
    let label = row.stem.clone();
    div()
        .id(path.clone())
        .group("tree-row")
        .w_full()
        .h(theme.menu_item_height)
        .pl(px(8. + row.depth as f32 * 20.))
        .pr(px(8.))
        .flex()
        .items_center()
        .gap(px(6.))
        .rounded(px(6.))
        .border_1()
        .border_color(gpui::transparent_black())
        .when(enabled, |row| row.cursor(CursorStyle::PointingHand))
        .when(highlighted, |row| row.bg(theme.selected).border_color(theme.accent).font_weight(FontWeight(460.)))
        .when(focused && !highlighted, |row| row.bg(theme.selected))
        .when(is_target && !highlighted && !focused, |row| row.bg(theme.hover))
        .when(!highlighted && !focused && enabled, |row| row.hover(|style| style.bg(theme.hover)))
        .when_some(arrow, |row, a| row.child(
            div().size(px(18.)).flex_none().flex().items_center().justify_center().child(icon(a, px(18.), chevron_colors))))
        .child(div().size(px(16.)).flex_none().flex().items_center().justify_center().child(icon(kind, px(16.), icon_colors)))
        .when_some(editing.clone(), |row, _| row.border_color(theme.accent).bg(theme.paper))
        .child(match editing {
            Some(input) => div().flex_1().h(px(24.)).rounded(px(3.)).bg(theme.selection)
                .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(24.)).px(px(2.))),
            None => div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label),
        })
        // Hover "⋯": opens the same menu as a right-click, anchored under the button.
        .when(!is_editing, |row| row.child(
            div().id("row-more").size(px(22.)).flex_none().flex().items_center().justify_center().rounded(px(5.))
                .invisible()
                .when(enabled, |b| b.group_hover("tree-row", |style| style.visible()))
                .when(enabled && is_target, |b| b.visible().bg(theme.btn_hover))
                .hover(|style| style.bg(theme.btn_hover))
                .on_mouse_down(MouseButton::Left, cx.listener({
                    let path = path.clone();
                    move |this, event: &gpui::MouseDownEvent, window, cx| {
                        cx.stop_propagation();
                        if !this.enabled || this.renaming.is_some() { return; }
                        if let Some(handler) = this.on_context.clone() {
                            handler(Some((path.to_path_buf(), dir)), event.position, window, cx);
                        }
                    }
                }))
                .child(icon(Icon::More, px(14.), chevron_colors))))
        .on_mouse_down(MouseButton::Right, cx.listener({
            let path = path.clone();
            move |this, event: &gpui::MouseDownEvent, window, cx| {
                cx.stop_propagation();
                if !this.enabled { return; }
                window.focus(&this.focus, cx);
                if let Some(handler) = this.on_context.clone() {
                    handler(Some((path.to_path_buf(), dir)), event.position, window, cx);
                }
            }
        }))
        .on_mouse_down(MouseButton::Middle, cx.listener({
            let path = path.clone();
            move |this, _, window, cx| {
                cx.stop_propagation();
                if !this.enabled || this.renaming.is_some() || dir { return; }
                window.focus(&this.focus, cx);
                if this.focus_dir.take().is_some() { cx.notify(); }
                if let Some(handler) = &this.on_open {
                    handler(path.to_path_buf(), true, window, cx);
                }
            }
        }))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
            if !this.enabled { return; }
            if this.renaming.is_some() { return; }
            window.focus(&this.focus, cx);
            if dir {
                this.index.toggle(index, &path);
                this.focus_dir = Some(path.to_path_buf());
                cx.notify();
            } else {
                if this.focus_dir.take().is_some() { cx.notify(); }
                if let Some(handler) = &this.on_open {
                    let in_new_tab = event.modifiers.platform || event.modifiers.control;
                    handler(path.to_path_buf(), in_new_tab, window, cx);
                }
            }
        }))
}
