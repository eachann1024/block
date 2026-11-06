use std::cell::RefCell;
use gpui::{App, AppContext, Div, Entity, MouseButton, WeakEntity, Window, div, prelude::*, px};
use gpui_component::input::{Input, InputState};
use super::{backend::DshOptions, chat::AiChat};
use crate::{theme::{Theme, tr}, ui::{ai_controls, settings_controls}};

struct Form { model: Entity<InputState>, block: bool, error: Option<String> }
thread_local! { static FORM: RefCell<Option<Form>> = const { RefCell::new(None) }; }

pub(super) fn open(chat: &AiChat, window: &mut Window, cx: &mut App) {
    let options = chat.dsh_options();
    let model = cx.new(|cx| InputState::new(window, cx).placeholder(tr("输入模型名称", "Enter model name")).default_value(options.model));
    FORM.with(|form| *form.borrow_mut() = Some(Form { model, block: options.auth_source == "block", error: None }));
}

pub(super) fn close() -> bool { FORM.with(|form| form.borrow_mut().take().is_some()) }
pub(super) fn is_open() -> bool { FORM.with(|form| form.borrow().is_some()) }

pub(super) fn inline_view(chat: WeakEntity<AiChat>) -> Option<Div> {
    let (model, block, error) = FORM.with(|form| form.borrow().as_ref().map(|form| (form.model.clone(), form.block, form.error.clone())))?;
    let theme = Theme::ocean();
    let mut row = div().flex().gap(px(6.));
    for value in [true, false] {
        let target = chat.clone();
        let model_for_auth = model.clone();
        row = row.child(ai_controls::action_chip(if value { "dsh-auth-block" } else { "dsh-auth-native" }, None,
            if value { tr("复用 Block 配置", "Use Block Config") } else { tr("dsh 原生配置", "dsh Native Config") }, false, value == block,
            move |_, _, cx| {
                cx.stop_propagation();
                FORM.with(|form| if let Some(form) = form.borrow_mut().as_mut() { form.block = value; });
                let model_val = model_for_auth.read(cx).value().to_string();
                let _ = target.update(cx, |chat, cx| {
                    if !model_val.trim().is_empty() {
                        let options = DshOptions { provider: "deepseek-official".into(), model: model_val, auth_source: if value { "block" } else { "native" }.into() };
                        let _ = chat.set_dsh_options(options, cx);
                    }
                    cx.notify();
                });
            }));
    }
    let save_model = model.clone();
    Some(div().h(px(104.)).p(px(8.)).rounded(px(10.)).bg(theme.shell).border_1().border_color(theme.accent).flex().flex_col().justify_between()
        .child(div().flex().justify_between().child(tr("dsh 认证来源", "dsh Auth Source")).child(div().text_color(theme.accent).child("DeepSeek")))
        .child(row)
        .child(div().flex().items_center().gap(px(6.))
            .child(div().flex_1().min_w_0().bg(theme.paper).rounded(crate::ui::radius::CONTROL).child(Input::new(&model).appearance(false).bordered(false).h(px(30.))))
            .child(settings_controls::action_button("dsh-save", tr("保存", "Save"), true, true).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                cx.stop_propagation();
                let model = save_model.read(cx).value().to_string();
                let block = FORM.with(|form| form.borrow().as_ref().is_some_and(|form| form.block));
                let options = DshOptions { provider: "deepseek-official".into(), model, auth_source: if block { "block" } else { "native" }.into() };
                let _ = chat.update(cx, |chat, cx| {
                    match chat.set_dsh_options(options, cx) {
                        Ok(()) => { FORM.with(|form| if let Some(form) = form.borrow_mut().as_mut() { form.error = None; }); chat.refresh_models(cx); },
                        Err(error) => FORM.with(|form| if let Some(form) = form.borrow_mut().as_mut() { form.error = Some(error); }),
                    }
                    cx.notify();
                });
            })))
        .children(error.map(|error| div().text_color(theme.text_secondary).child(error))))
}
