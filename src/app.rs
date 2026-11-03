//! Window shell: sidebar on the shell color, paper column for the editor.

use std::path::PathBuf;

use gpui::{
    App, Context, Focusable, IntoElement, ParentElement, PathPromptOptions, Render,
    StatefulInteractiveElement, Window, div, prelude::*, px,
};

use crate::editor::view::Editor;
use crate::files::{
    fresh_note_path, load_folder, save_folder, scan,
    tree::FileTree,
};
use crate::theme::Theme;

pub struct Workspace {
    folder: Option<PathBuf>,
    tree: gpui::Entity<FileTree>,
    pub(crate) editor: gpui::Entity<Editor>,
}

impl Workspace {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let tree = cx.new(|_| FileTree::new());
        let editor = cx.new(|cx| Editor::new(cx));
        let workspace = cx.entity();
        tree.update(cx, |tree, _| {
            tree.on_open(move |path, window, cx| {
                workspace.update(cx, |this, cx| {
                    this.editor.update(cx, |editor, cx| editor.open(path, cx));
                    window.focus(&this.editor.focus_handle(cx));
                });
            });
        });
        let mut this = Self { folder: None, tree, editor };
        if let Some(folder) = load_folder() {
            this.use_folder(folder, cx);
        }
        let _ = window;
        this
    }

    fn use_folder(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        let nodes = scan(&folder);
        self.folder = Some(folder);
        self.tree.update(cx, |tree, cx| tree.set_nodes(nodes, cx));
        cx.notify();
    }

    fn choose_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let prompt = cx.prompt_for_paths(PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some("选择文件夹".into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let Ok(Ok(Some(paths))) = prompt.await else { return };
            if let Some(folder) = paths.into_iter().next() {
                save_folder(&folder);
                let _ = this.update(cx, |this, cx| this.use_folder(folder, cx));
            }
        })
        .detach();
    }

    fn new_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(folder) = self.folder.clone() else { return };
        let path = fresh_note_path(&folder);
        if std::fs::write(&path, "").is_err() {
            return;
        }
        self.use_folder(folder, cx);
        self.tree.update(cx, |tree, cx| tree.select(path.clone(), cx));
        self.editor.update(cx, |editor, cx| editor.open(path, cx));
        window.focus(&self.editor.focus_handle(cx));
    }
}

impl Render for Workspace {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let folder = self.folder.clone();
        div()
            .size_full()
            .flex()
            .bg(theme.shell)
            .text_color(theme.text)
            .child(sidebar(self.tree.clone(), folder, theme.clone_shell(), cx))
            .child(div().flex_1().h_full().child(self.editor.clone()))
    }
}

fn sidebar(
    tree: gpui::Entity<FileTree>,
    folder: Option<PathBuf>,
    theme: Theme,
    cx: &mut Context<Workspace>,
) -> gpui::Div {
    div()
        .w(theme.sidebar_width)
        .h_full()
        .flex()
        .flex_col()
        .bg(theme.sidebar)
        .border_r_1()
        .border_color(theme.hairline)
        .child(header(folder, cx))
        .child(div().id("file-scroll").flex_1().overflow_y_scroll().child(tree))
}

fn header(folder: Option<PathBuf>, cx: &mut Context<Workspace>) -> gpui::Div {
    let theme = Theme::ocean();
    let title = folder
        .as_ref()
        .and_then(|path| path.file_name())
        .map(|name| name.to_string_lossy().to_string())
        .unwrap_or_else(|| "未打开文件夹".into());
    div()
        .px_3()
        .pt_8()
        .pb_2()
        .flex()
        .flex_col()
        .gap_1()
        .child(div().text_size(theme.ui_font).child(title))
        .child(
            div()
                .flex()
                .gap_1()
                .child(action_button("选择文件夹", cx.listener(|this, _, window, cx| {
                    this.choose_folder(window, cx);
                })))
                .child(action_button("新建笔记", cx.listener(|this, _, window, cx| {
                    this.new_note(window, cx);
                }))),
        )
}

fn action_button(
    label: &'static str,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div()
        .id(label)
        .h(px(28.))
        .px_2()
        .flex()
        .items_center()
        .rounded_lg()
        .bg(theme.paper)
        .border_1()
        .border_color(theme.hairline)
        .text_size(px(13.))
        .cursor(gpui::CursorStyle::PointingHand)
        .hover(|style| style.bg(theme.hover))
        .child(label)
        .on_mouse_down(gpui::MouseButton::Left, on_down)
}

trait ShellCopy {
    fn clone_shell(self) -> Self;
}

impl ShellCopy for Theme {
    fn clone_shell(self) -> Self {
        self
    }
}
