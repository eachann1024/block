//! Link field and colour palette popovers for the selection toolbar.
use gpui::{AnyElement, CursorStyle, Entity, MouseButton, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use crate::{dropdown::{self, RowSpec}, icons::Icon, theme::tr};
use super::toolbar::ToolbarColors;
use super::{format_cmds::{FormatPop, palette::PalettePopover}, view::{Cancel, Editor}};

pub(super) fn popover(editor: Entity<Editor>, pop: &FormatPop) -> AnyElement {
    match pop {
        FormatPop::Ai(session) => super::ai::panel::render(editor, session),
        FormatPop::Link { input, existing, anchor, .. } => {
            dropdown::deferred_popover(
                *anchor,
                crate::ui::popover::card("format-link-card", None).child(link_field(editor, input, *existing)),
                2,
            )
            .into_any_element()
        }
        FormatPop::SelectionMenu { anchor } => dropdown::deferred_popover(*anchor, selection_menu(editor), 2).into_any_element(),
        FormatPop::Palette { anchor } => dropdown::deferred_popover(*anchor, PalettePopover { editor }, 2).into_any_element(),
        FormatPop::Kinds { anchor, current } => dropdown::deferred_popover(*anchor, super::toolbar::menu(editor, true, *current), 2).into_any_element(),
        FormatPop::Lists { anchor, current } => dropdown::deferred_popover(*anchor, super::toolbar::menu(editor, false, *current), 2).into_any_element(),
    }
}

/// Right-click menu over a selection: export it as an image, comment on it, or copy it.
fn selection_menu(editor: Entity<Editor>) -> AnyElement {
    let mac = cfg!(target_os = "macos");
    let row = |id: &'static str, icon: Icon, label: &'static str, hint: &'static str, run: fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>)| {
        let editor = editor.clone();
        dropdown::row_with(id, RowSpec { icon: Some(icon), label: label.into(), hint: Some(hint.into()), ..Default::default() })
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| { this.format = None; run(this, window, cx); cx.notify(); });
                cx.stop_propagation();
            })
    };
    dropdown::shell("selection-context-menu").w(px(236.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .child(row("sel-export-img", Icon::Image, tr("选中内容生成图片…", "Export Selection as Image…"), if mac { "⇧⌘E" } else { "Ctrl+Shift+E" }, |this, w, cx| this.export_image(w, cx)))
        .child(row("sel-annotate", Icon::TbComment, tr("添加批注", "Add Comment"), if mac { "⌥⌘M" } else { "Ctrl+Alt+M" }, |this, w, cx| this.start_annotation(w, cx)))
        .child(dropdown::separator())
        .child(row("sel-copy", Icon::Copy, tr("复制", "Copy"), if mac { "⌘C" } else { "Ctrl+C" }, |this, w, cx| this.handle(&super::view::EditorAction::Copy, w, cx)))
        .into_any_element()
}

fn action_button(label: &'static str, id: &'static str, run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static, editor: Entity<Editor>, accent: bool) -> impl IntoElement {
    let tc = ToolbarColors::current();
    div().id(id).h(px(28.)).px(px(10.)).rounded(px(7.)).flex().items_center().cursor(CursorStyle::PointingHand).text_size(px(12.))
        .font_weight(gpui::FontWeight(500.))
        .when(accent, |b| b.bg(rgb(tc.accent)).text_color(rgb(0xffffff)))
        .when(!accent, |b| b.hover(move |s| s.bg(rgb(tc.hover))).text_color(rgb(tc.ink))).child(label)
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| run(this, window, cx)); cx.stop_propagation();
        })
}

fn link_field(editor: Entity<Editor>, input: &Entity<InputState>, _existing: bool) -> AnyElement {
    let tc = ToolbarColors::current();
    div().id("link-field").flex().items_center().gap(px(6.)).pl(px(6.))
        .on_action({ let e = editor.clone(); move |_: &Escape, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .on_action({ let e = editor.clone(); move |_: &Cancel, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .child(div().w(px(260.)).h(px(28.)).px(px(8.)).rounded(px(6.)).bg(rgb(tc.input_bg)).text_size(px(13.)).text_color(rgb(tc.ink)).flex().items_center().overflow_hidden()
            .child(div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).text_size(px(13.)))))
        .child(action_button(crate::theme::tr("确定", "OK"), "link-save", |this, w, cx| this.link_save(w, cx), editor, true))
        .into_any_element()
}
