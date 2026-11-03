//! The folder sidebar: a header with the two actions and an expandable tree.

use std::collections::HashSet;
use std::path::PathBuf;

use gpui::{Context, CursorStyle, MouseButton, div, prelude::*, px};

use crate::icons::{Icon, icon};
use crate::theme::Theme;

use super::scan::Node;

pub struct FileTree {
    nodes: Vec<Node>,
    open: HashSet<PathBuf>,
    selected: Option<PathBuf>,
    on_open: Option<Box<dyn Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static>>,
}

impl FileTree {
    pub fn new() -> Self {
        Self { nodes: Vec::new(), open: HashSet::new(), selected: None, on_open: None }
    }

    pub fn set_nodes(&mut self, nodes: Vec<Node>, cx: &mut Context<Self>) {
        self.nodes = nodes;
        cx.notify();
    }

    pub fn select(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.selected = Some(path);
        cx.notify();
    }

    pub fn on_open(
        &mut self,
        handler: impl Fn(PathBuf, &mut gpui::Window, &mut gpui::App) + 'static,
    ) {
        self.on_open = Some(Box::new(handler));
    }

    fn toggle(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        if !self.open.remove(&path) {
            self.open.insert(path);
        }
        cx.notify();
    }
}

impl Render for FileTree {
    fn render(&mut self, _: &mut gpui::Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        div()
            .flex()
            .flex_col()
            .py_2()
            .children(rows(&self.nodes, &self.open, &self.selected, 0, cx))
            .text_size(theme.ui_font)
            .text_color(theme.text)
    }
}

fn rows(
    nodes: &[Node],
    open: &HashSet<PathBuf>,
    selected: &Option<PathBuf>,
    depth: usize,
    cx: &mut Context<FileTree>,
) -> Vec<gpui::AnyElement> {
    let mut out = Vec::new();
    for node in nodes {
        out.push(row(node, open, selected, depth, cx).into_any_element());
        if node.dir && open.contains(&node.path) {
            out.extend(rows(&node.children, open, selected, depth + 1, cx));
        }
    }
    out
}

fn row(
    node: &Node,
    open: &HashSet<PathBuf>,
    selected: &Option<PathBuf>,
    depth: usize,
    cx: &mut Context<FileTree>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let path = node.path.clone();
    let dir = node.dir;
    let is_selected = selected.as_ref() == Some(&node.path);
    let expanded = dir && open.contains(&node.path);
    let (arrow, kind) = match (dir, expanded) {
        (false, _) => (None, Icon::Note),
        (true, false) => (Some(Icon::ChevronRight), Icon::Folder),
        (true, true) => (Some(Icon::ChevronDown), Icon::FolderOpen),
    };
    let colors = theme.icon_colors(theme.text_secondary);
    let label = node.name.trim_end_matches(".md").trim_end_matches(".markdown").to_string();
    div()
        .id(("file", node.path.to_string_lossy().chars().map(|ch| ch as u64).sum::<u64>()))
        .h(theme.menu_item_height)
        .mx_1()
        .pl(px(12. + depth as f32 * 14.))
        .pr_2()
        .flex()
        .items_center()
        .gap_1()
        .rounded_sm()
        .cursor(CursorStyle::PointingHand)
        .when(is_selected, |row| row.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!is_selected, |row| row.hover(|style| style.bg(theme.hover)))
        .child(div().w(px(16.)).h(px(16.)).flex_none().children(arrow.map(|a| icon(a, px(16.), colors))))
        .child(icon(kind, px(16.), colors))
        .child(div().flex_1().overflow_hidden().child(label))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            if dir {
                this.toggle(path.clone(), cx);
            } else if let Some(handler) = &this.on_open {
                this.selected = Some(path.clone());
                handler(path.clone(), window, cx);
                cx.notify();
            }
        }))
}
