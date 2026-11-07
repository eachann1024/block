mod art;

pub use art::{MascotState, ai_greeting, illustration, load_asset};

use gpui::{
    AnyElement, App, Context, CursorStyle, Div, FontWeight, Hsla, IntoElement,
    MouseButton, MouseDownEvent, ParentElement, Stateful, Styled, Window, div, prelude::*, px, rgb,
};
use crate::app::Workspace;
use crate::editor::view::Editor;
use crate::theme::{Theme, is_dark, tr};

pub fn editor_empty(_cx: &mut Context<Editor>) -> Div {
    card(
        MascotState::NoNote,
        tr("打开一篇，继续想", "Open a Note to Continue"),
        &[tr("从左侧选择笔记开始编辑，", "Select a note from the sidebar,"), tr("也可以写下新的想法。", "or start a new thought.")],
        None,
    )
}

impl Workspace {
    pub(crate) fn mascot_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let editor = self.editor.read(cx);
        if editor.current_path().is_some() || editor.is_loading() {
            return None;
        }
        let available = !self.busy();
        let theme = Theme::ocean();
        let content = if self.folder_error.is_some() {
            let retry = primary_button(
                "mascot-retry",
                tr("重试", "Retry"),
                available,
                cx.listener(|this, _, _, cx| this.retry_folder(cx)),
            );
            let choose = secondary_button(
                "mascot-choose-other",
                tr("选择其他文件夹", "Choose Another Folder"),
                available,
                cx.listener(|this, _, window, cx| this.choose_folder(window, cx)),
            );
            card(
                MascotState::ReadError,
                tr("暂时无法读取文件夹", "Cannot Read Folder"),
                &[tr("检查文件夹是否仍可访问，", "Check if the folder is still accessible,"), tr("然后重试，或选择其他文件夹。", "then retry or choose another folder.")],
                Some(div().child(retry).child(choose)),
            )
        } else if self.loading {
            return Some(div().flex_1().min_w_0().h_full().bg(theme.paper).into_any_element());
        } else if self.folder.is_none() {
            let choose = primary_button(
                "mascot-choose-folder",
                tr("选择文件夹", "Choose Folder"),
                available,
                cx.listener(|this, _, window, cx| this.choose_folder(window, cx)),
            );
            card(
                MascotState::NoFolder,
                tr("先选一个笔记文件夹", "Choose a Notes Folder"),
                &[tr("打开本地文件夹，", "Open a local folder"), tr("让想法有个落脚的地方。", "to keep your notes in one place.")],
                Some(div().child(choose)),
            )
        } else if !self.tree.read(cx).has_notes() {
            let create = primary_button(
                "mascot-new-note",
                tr("新建笔记", "New Note"),
                available,
                cx.listener(|this, _, window, cx| this.new_tab(window, cx)),
            );
            let switch = secondary_button(
                "mascot-switch-folder",
                tr("切换文件夹", "Switch Folder"),
                available,
                cx.listener(|this, _, window, cx| this.choose_folder(window, cx)),
            );
            card(
                MascotState::EmptyFolder,
                tr("从第一篇笔记开始", "Start with Your First Note"),
                &[tr("这个文件夹里还没有笔记。", "This folder has no notes yet."), tr("写下一点，慢慢展开。", "Jot something down and let it grow.")],
                Some(div().child(create).child(switch)),
            )
        } else {
            return None;
        };
        Some(
            div()
                .flex_1()
                .min_w_0()
                .h_full()
                .bg(theme.paper)
                .child(content)
                .into_any_element(),
        )
    }
}

fn card(state: MascotState, title: &'static str, body: &[&'static str], actions: Option<Div>) -> Div {
    let theme = Theme::ocean();
    div()
        .size_full()
        .flex()
        .flex_col()
        .items_center()
        .justify_center()
        .px(px(24.))
        .text_center()
        .child(illustration(state))
        .child(
            div()
                .mt(px(22.))
                .mb(px(9.))
                .text_size(px(18.))
                .line_height(px(26.))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(theme.text)
                .child(title),
        )
        .child(
            div()
                .min_h(px(44.))
                .flex()
                .flex_col()
                .items_center()
                .text_size(px(13.))
                .line_height(px(22.))
                .text_color(theme.text_secondary)
                .children(body.iter().copied().map(|line| div().child(line))),
        )
        .when_some(actions, |el, actions| {
            el.child(actions.mt(px(24.)).flex().items_center().gap(px(10.)))
        })
}

fn primary_button(
    id: &'static str,
    label: &'static str,
    enabled: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let hover_bg: Hsla = if is_dark() { rgb(0x3b537f).into() } else { rgb(0xbacdf0).into() };
    div()
        .id(id)
        .h(px(44.))
        .px(px(22.))
        .rounded(crate::ui::radius::CONTROL)
        .border_1()
        .border_color(gpui::transparent_black())
        .bg(theme.icon_fill)
        .text_color(theme.text)
        .text_size(px(13.))
        .font_weight(FontWeight::SEMIBOLD)
        .flex()
        .items_center()
        .justify_center()
        .when(enabled, |b| {
            b.cursor(CursorStyle::PointingHand)
                .hover(move |s| s.bg(hover_bg))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(label)
}

fn secondary_button(
    id: &'static str,
    label: &'static str,
    enabled: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(44.))
        .px(px(16.))
        .rounded(crate::ui::radius::CONTROL)
        .text_color(theme.text_secondary)
        .text_size(px(12.))
        .font_weight(FontWeight::MEDIUM)
        .flex()
        .items_center()
        .justify_center()
        .when(enabled, |b| {
            b.cursor(CursorStyle::PointingHand)
                .hover(move |s| s.bg(theme.hover).text_color(theme.text))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(label)
}
