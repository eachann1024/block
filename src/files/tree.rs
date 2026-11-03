//! Virtualized sidebar. Selection is committed only by the editor observer.
use std::path::{Path, PathBuf};
use std::sync::Arc;
use gpui::{Context, CursorStyle, MouseButton, UniformListScrollHandle, div, prelude::*, px, uniform_list};
use crate::icons::{Icon, icon};
use crate::theme::Theme;
use super::index::{Row, TreeIndex};

pub struct FileTree {
    index: TreeIndex,
    selected: Option<PathBuf>,
    enabled: bool,
    scroll: UniformListScrollHandle,
    on_open: Option<Box<dyn Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static>>,
}

impl FileTree {
    pub fn new() -> Self {
        Self { index: TreeIndex::default(), selected: None, enabled: true,
            scroll: UniformListScrollHandle::new(), on_open: None }
    }

    pub fn set_index(&mut self, index: TreeIndex, cx: &mut Context<Self>) {
        let previous = std::mem::replace(&mut self.index, index);
        // Destroying an old million-node tree is worker work as well.
        cx.background_executor().spawn(async move { drop(previous); }).detach();
        self.selected = None;
        self.scroll = UniformListScrollHandle::new();
        cx.notify();
    }

    pub fn set_enabled(&mut self, enabled: bool, cx: &mut Context<Self>) {
        if self.enabled != enabled { self.enabled = enabled; cx.notify(); }
    }

    pub fn select(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) {
        if self.selected != path { self.selected = path; cx.notify(); }
    }

    pub fn contains_root_path(&self, path: &Path) -> bool { self.index.contains_root_path(path) }

    pub fn insert_note(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.index.insert_note(path);
        cx.notify();
    }

    pub fn on_open(&mut self, handler: impl Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static) {
        self.on_open = Some(Box::new(handler));
    }
}

impl Render for FileTree {
    fn render(&mut self, _: &mut gpui::Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        uniform_list("file-list", self.index.len(), cx.processor(|this, range: std::ops::Range<usize>, _, cx| {
            range.filter_map(|rank| this.index.row(rank)).map(|(index, row, expanded)| {
                file_row(index, row, expanded, this.selected.as_deref(), this.enabled, cx)
            }).collect::<Vec<_>>()
        }))
        .track_scroll(self.scroll.clone())
        .size_full()
        .text_size(theme.ui_font)
        .text_color(theme.text)
    }
}

fn file_row(index: usize, row: Arc<Row>, expanded: bool, selected: Option<&Path>, enabled: bool,
    cx: &mut Context<FileTree>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let path = row.path.clone();
    let dir = row.dir;
    let is_selected = selected == Some(path.as_ref());
    let (arrow, kind) = match (dir, expanded) {
        (false, _) => (None, Icon::Note),
        (true, false) => (Some(Icon::ChevronRight), Icon::Folder),
        (true, true) => (Some(Icon::ChevronDown), Icon::FolderOpen),
    };
    let colors = theme.icon_colors(theme.text_secondary);
    let label = if dir { row.name.clone() } else {
        Path::new(&row.name).file_stem().unwrap_or_default().to_string_lossy().into_owned()
    };
    div()
        .id(path.clone())
        .h(theme.menu_item_height)
        .mx_1()
        .pl(px(12. + row.depth as f32 * 14.))
        .pr_2()
        .flex()
        .items_center()
        .gap_1()
        .rounded_sm()
        .border_1()
        .border_color(gpui::transparent_black())
        .when(enabled, |row| row.cursor(CursorStyle::PointingHand))
        .when(is_selected, |row| row.bg(theme.selected).border_color(theme.accent))
        .when(!is_selected && enabled, |row| row.hover(|style| style.bg(theme.hover)))
        .child(div().w(px(16.)).h(px(16.)).flex_none().children(arrow.map(|a| icon(a, px(16.), colors))))
        .child(icon(kind, px(16.), colors))
        .child(div().flex_1().overflow_hidden().child(label))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            if !this.enabled { return; }
            if dir {
                this.index.toggle(index, &path);
                cx.notify();
            } else if let Some(handler) = &this.on_open {
                // The editor treats the current path as a no-op and cancels a
                // pending different open, so A → B → A keeps A selected.
                handler(path.to_path_buf(), window, cx);
            }
        }))
}
