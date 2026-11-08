//! Floating image toolbar (7 actions), `⋯` 4-item menu, and AltText / Size / Crop popovers.
use gpui::{Context, CursorStyle, Entity, FontWeight, MouseButton, Window, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, InputState};
use crate::{dropdown::{self, RowSpec}, icons::Icon, theme::tr, ui::{floating_bar, popover}};
use super::{
    image_edit::ImagePop,
    image_source::{Align, spec_of},
    image_view::sizes,
    toolbar::{ToolbarColors, pill, sep},
    view::{Editor, selection::Position},
};

fn tb_btn(
    editor: &Entity<Editor>, id: u64, slot: usize, g: Icon, tip: &'static str, on: bool, open: bool,
    run: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let editor = editor.clone();
    floating_bar::icon_toggle_button(("img-tb", id as usize * 16 + slot), g, tip, on, open)
        .size(px(32.))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| run(this, window, cx));
            cx.stop_propagation();
        })
}

/// Top-centered floating capsule toolbar + active dropdown/popover for a selected image block.
pub(super) fn image_toolbar(
    editor: &Entity<Editor>, id: u64, align: Align, percent: u32, pop: Option<ImagePop>, cx: &mut gpui::App,
) -> impl IntoElement {
    let crop_bounds = std::rc::Rc::new(std::cell::Cell::new(None));
    let more_bounds = std::rc::Rc::new(std::cell::Cell::new(None));
    let crop_open = matches!(pop, Some(ImagePop::Crop));
    let more_open = matches!(pop, Some(ImagePop::More | ImagePop::AltText(_) | ImagePop::Size));
    let bar = pill().id(("img-toolbar", id as usize)).py(px(5.)).px(px(7.)).gap(px(3.)).rounded(crate::ui::radius::SURFACE)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .child(tb_btn(editor, id, 0, Icon::TbComment, tr("添加批注", "Add annotation"), false, false, move |this, w, cx| {
            this.image_menu = None;
            let at = Position { block: id, offset: 0 };
            this.caret.anchor = at; this.caret.head = at;
            this.start_annotation(w, cx);
        }))
        .child(tb_btn(editor, id, 1, Icon::TbReaction, tr("添加表情回应", "Add reaction"), false, false, move |this, w, cx| {
            this.image_menu = None;
            let at = Position { block: id, offset: 0 };
            this.caret.anchor = at; this.caret.head = at;
            this.open_reactions(w, cx);
        }))
        .child(sep())
        .child(tb_btn(editor, id, 2, Icon::AlignLeft, tr("左对齐", "Align left"), align == Align::Left, false,
            move |this, _, cx| { this.image_menu = None; this.edit_image(id, |s| s.align = Align::Left, cx); }))
        .child(tb_btn(editor, id, 3, Icon::AlignCenter, tr("居中对齐", "Align center"), align == Align::Center, false,
            move |this, _, cx| { this.image_menu = None; this.edit_image(id, |s| s.align = Align::Center, cx); }))
        .child(tb_btn(editor, id, 4, Icon::AlignRight, tr("右对齐", "Align right"), align == Align::Right, false,
            move |this, _, cx| { this.image_menu = None; this.edit_image(id, |s| s.align = Align::Right, cx); }))
        .child(sep())
        .child(tb_btn(editor, id, 5, Icon::Crop, tr("裁剪图片", "Crop image"), false, crop_open, move |this, _, cx| {
            this.image_menu = if crop_open { None } else { Some((id, ImagePop::Crop)) };
            cx.notify();
        }).child(crate::ui::placement::track_bounds(crop_bounds.clone(), false)))
        .child(tb_btn(editor, id, 6, Icon::More, tr("更多图片操作", "More image options"), false, more_open, move |this, _, cx| {
            this.image_menu = if more_open { None } else { Some((id, ImagePop::More)) };
            cx.notify();
        }).child(crate::ui::placement::track_bounds(more_bounds.clone(), false)));

    let trigger_bounds = if crop_open { crop_bounds } else { more_bounds };
    let pos_editor = editor.clone();
    let image_key = format!("image-pop-{id}");
    let motion = crate::ui::motion::popup_hold(&image_key, pop);
    if let Some((_, pop_gen, _)) = motion.as_ref() {
        crate::ui::motion::schedule_popup_finish_for(&image_key, *pop_gen, editor.clone(), cx);
    }
    let sub = motion.map(|(phase, pop_gen, p)| {
        let body = match p {
            ImagePop::More => more_menu(editor, id).into_any_element(),
            ImagePop::AltText(input) => alt_popover(editor, id, &input).into_any_element(),
            ImagePop::Size => size_popover(editor, id, percent).into_any_element(),
            ImagePop::Crop => crop_popover(editor, id).into_any_element(),
        };
        dropdown::anchored_motion_with(
            gpui::SharedString::from(format!("image-pop-{id}-{pop_gen}")),
            move |_, cx| {
                dropdown::PlacementSpec::below(trigger_bounds.get().unwrap_or_default())
                    .viewport(pos_editor.read(cx).list.viewport_bounds())
            },
            body,
            phase,
            2,
        )
    });

    div().relative().flex().flex_col().items_center()
        .child(floating_bar::animate(bar, ("img-toolbar-enter", id as usize)))
        .children(sub)
}

fn menu_card(id: (&'static str, usize), width: f32) -> gpui::Stateful<gpui::Div> {
    dropdown::shell(id).w(px(width))
}

fn more_menu(editor: &Entity<Editor>, id: u64) -> impl IntoElement {
    let item = |slot: usize, g: Icon, label: &'static str, danger: bool,
        run: Box<dyn Fn(&mut Editor, &mut Window, &mut Context<Editor>)>| {
        let ed = editor.clone();
        dropdown::row_with(
            ("img-more-item", id as usize * 8 + slot),
            RowSpec {
                icon: Some(g),
                label: label.into(),
                danger,
                ..Default::default()
            },
        )
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            ed.update(cx, |this, cx| run(this, window, cx));
            cx.stop_propagation();
        })
    };
    menu_card(("img-more-menu", id as usize), 184.)
        .child(item(0, Icon::Copy, tr("复制图片", "Copy image"), false,
            Box::new(move |this, _, cx| { this.image_menu = None; this.copy_image(id, cx); })))
        .child(item(1, Icon::LinkEdit, tr("编辑替代文本", "Edit alt text"), false,
            Box::new(move |this, w, cx| this.open_image_alt(id, w, cx))))
        .child(item(2, Icon::Sliders, tr("设置图片尺寸", "Set image size"), false,
            Box::new(move |this, _, cx| { this.image_menu = Some((id, ImagePop::Size)); cx.notify(); })))
        .child(dropdown::separator())
        .child(item(3, Icon::Trash, tr("删除图片", "Delete image"), true,
            Box::new(move |this, w, cx| this.delete_image(id, w, cx))))
}

fn alt_popover(editor: &Entity<Editor>, id: u64, input: &Entity<InputState>) -> impl IntoElement {
    let (e_save, e_cancel, e_esc) = (editor.clone(), editor.clone(), editor.clone());
    popover::card(("img-alt-pop", id as usize), Some(px(340.)))
        .on_action(move |_: &Escape, w, cx| {
            e_esc.update(cx, |this, cx| { this.image_menu = None; w.focus(&this.focus, cx); cx.notify(); });
            cx.stop_propagation();
        })
        .child(div().flex().items_center().gap(px(6.))
            .child(popover::inline_input_slot(input, None))
            .child(popover::inline_action_button(("img-alt-save", id as usize), tr("保存", "Save"), true, move |_, w, cx| {
                e_save.update(cx, |this, cx| this.save_image_alt(id, w, cx));
                cx.stop_propagation();
            }))
            .child(popover::inline_action_button(("img-alt-cancel", id as usize), tr("取消", "Cancel"), false, move |_, w, cx| {
                e_cancel.update(cx, |this, cx| { this.image_menu = None; w.focus(&this.focus, cx); cx.notify(); });
                cx.stop_propagation();
            })))
}

fn size_popover(editor: &Entity<Editor>, id: u64, percent: u32) -> impl IntoElement {
    let tc = ToolbarColors::current();
    let mut row = pill().id(("img-size-pop", id as usize)).p(px(5.)).gap(px(3.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    for (idx, pct) in [25_u32, 50, 75, 100].into_iter().enumerate() {
        let on = (percent as i32 - pct as i32).abs() <= 4;
        let ed = editor.clone();
        row = row.child(div().id(("img-pct", id as usize * 8 + idx)).h(px(28.)).px(px(8.)).rounded(crate::ui::radius::CONTROL)
            .flex().items_center().cursor(CursorStyle::PointingHand).text_size(px(12.)).font_weight(FontWeight(500.))
            .when(on, |b| b.bg(rgb(tc.on_bg)).text_color(rgb(tc.on_ink)))
            .when(!on, |b| b.hover(move |s| s.bg(rgb(tc.hover))).text_color(rgb(tc.ink)))
            .child(format!("{pct}%"))
            .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                ed.update(cx, |this, cx| this.set_image_percent(id, pct, cx));
                cx.stop_propagation();
            }));
    }
    row.child(sep())
        .child(tb_btn(editor, id, 8, Icon::TbMinus, tr("缩小", "Zoom out"), false, false,
            move |this, _, cx| this.step_image(id, -10, cx)))
        .child(div().min_w(px(38.)).flex().justify_center().text_size(px(12.)).text_color(rgb(tc.ink)).child(format!("{percent}%")))
        .child(tb_btn(editor, id, 9, Icon::TbPlus, tr("放大", "Zoom in"), false, false,
            move |this, _, cx| this.step_image(id, 10, cx)))
        .child(tb_btn(editor, id, 10, Icon::TbReset, tr("原始宽度", "Original size"), false, false,
            move |this, _, cx| { this.image_menu = None; this.edit_image(id, |s| s.width = None, cx); }))
}

fn crop_popover(editor: &Entity<Editor>, id: u64) -> impl IntoElement {
    let tc = ToolbarColors::current();
    let presets: [(&'static str, Option<(u32, u32)>); 4] = [
        ("16:9", Some((16, 9))),
        ("4:3", Some((4, 3))),
        ("1:1", Some((1, 1))),
        (tr("裁边 15%", "Trim 15%"), None),
    ];
    let mut row = pill().id(("img-crop-pop", id as usize)).relative().children(crate::ui::smoke_overlay::probe("image-crop-popover")).p(px(5.)).gap(px(4.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation());
    for (idx, (label, ratio)) in presets.into_iter().enumerate() {
        let ed = editor.clone();
        row = row.child(div().id(("img-crop-opt", id as usize * 8 + idx)).h(px(28.)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
            .flex().items_center().cursor(CursorStyle::PointingHand).text_size(px(12.)).font_weight(FontWeight(500.))
            .text_color(rgb(tc.ink)).hover(move |s| s.bg(rgb(tc.hover)))
            .child(label)
            .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                ed.update(cx, |this, cx| this.crop_image(id, ratio, cx));
                cx.stop_propagation();
            }));
    }
    row
}

impl Editor {
    pub(super) fn set_image_percent(&mut self, id: u64, pct: u32, cx: &mut Context<Self>) {
        if pct >= 100 {
            self.edit_image(id, |s| s.width = None, cx);
            return;
        }
        let (base, _) = sizes(cx, cx.entity_id(), id, self.minimal);
        let base = if base > 0. { base } else { 560. };
        let width = (base * pct as f32 / 100.).round() as u32;
        self.edit_image(id, |s| s.width = Some(width), cx);
    }
    /// Moves the width to the next multiple of 10% of the original width, within 10%..=100%.
    pub(super) fn step_image(&mut self, id: u64, delta: i32, cx: &mut Context<Self>) {
        let (base, _) = sizes(cx, cx.entity_id(), id, self.minimal);
        if base <= 0. { return; }
        let Some(spec) = self.doc.get(id).and_then(spec_of) else { return };
        let current_pct = spec.width.map_or(100., |w| (w as f32 / base * 10.).round() * 10.);
        let percent = (current_pct + delta as f32).clamp(10., 100.);
        let width = (base * percent / 100.).round() as u32;
        self.edit_image(id, |s| s.width = Some(width), cx);
    }
    /// Cmd+= / Cmd+-: resize the image holding the caret, if the caret block is one.
    #[allow(dead_code)]
    pub(super) fn step_selected_image(&mut self, delta: i32, cx: &mut Context<Self>) {
        let id = self.caret.head.block;
        if self.doc.get(id).and_then(spec_of).is_some() { self.step_image(id, delta, cx); }
    }
}
