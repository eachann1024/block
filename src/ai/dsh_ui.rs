//! dsh configuration presentation; values and persistence belong to the caller.
use std::rc::Rc;
use gpui::{App, Div, Entity, MouseButton, Window, div, prelude::*, px};
use gpui_component::input::{Input, InputState};
use crate::{theme::{Theme, tr}, ui::{ai_controls, dropdown, settings_controls}};

/// The caller owns the input, menu state, validation, and saving; no secrets enter this view.
#[allow(dead_code)]
pub(super) fn config_card(
    model: &Entity<InputState>,
    use_block_config: bool,
    auth_menu_open: bool,
    saving: bool,
    save_error: Option<&str>,
    on_toggle_auth: impl Fn(&mut Window, &mut App) + 'static,
    on_select_auth: impl Fn(bool, &mut Window, &mut App) + 'static,
    on_save: impl Fn(&mut Window, &mut App) + 'static,
    on_cancel: impl Fn(&mut Window, &mut App) + 'static,
) -> Div {
    let theme = Theme::ocean();
    let auth_label = if use_block_config { tr("使用 Block 的 DeepSeek 配置", "Use Block DeepSeek configuration") } else { tr("使用 dsh 配置", "Use dsh configuration") };
    let auth_trigger = ai_controls::action_chip("dsh-auth-trigger", None, auth_label, false, auth_menu_open,
        move |_, window, cx| { cx.stop_propagation(); on_toggle_auth(window, cx); })
        .h(px(32.)).rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border)
        .child(dropdown::line_icon(crate::icons::Icon::ChevronDown, 14., theme.text_secondary));
    let on_select_auth = Rc::new(on_select_auth);
    let menu = auth_menu_open.then(|| {
        [false, true].into_iter().fold(dropdown::shell("dsh-auth-menu").w(px(280.)), |menu, block| {
            let select = on_select_auth.clone();
            menu.child(dropdown::row_with(if block { "dsh-auth-block" } else { "dsh-auth-native" }, dropdown::RowSpec {
                label: if block { tr("使用 Block 的 DeepSeek 配置", "Use Block DeepSeek configuration") } else { tr("使用 dsh 配置", "Use dsh configuration") }.into(),
                checked: block == use_block_config, ..Default::default()
            }).on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); select(block, window, cx); }))
        })
    });
    let auth_control = div().relative().child(auth_trigger)
        .children(menu.map(|menu| gpui::deferred(div().absolute().top(px(34.)).right_0().child(menu)).with_priority(10)));
    let input = div().h(px(34.)).w_full().min_w_0().px(px(10.)).flex().items_center()
        .rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border).bg(theme.shell)
        .child(Input::new(model).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).w_full());
    let save = settings_controls::action_button("dsh-save", if saving { tr("保存中…", "Saving…") } else { tr("保存", "Save") }, true, !saving)
        .when(!saving, |button| button.on_mouse_down(MouseButton::Left, move |_, window, cx| on_save(window, cx)));
    let cancel = settings_controls::action_button("dsh-cancel", tr("取消", "Cancel"), false, !saving)
        .when(!saving, |button| button.on_mouse_down(MouseButton::Left, move |_, window, cx| on_cancel(window, cx)));
    settings_controls::settings_card(14.)
        .child(settings_controls::card_title(tr("dsh 配置", "dsh configuration")))
        .child(settings_controls::setting_row(tr("服务商", "Provider"), settings_controls::static_badge("DeepSeek", false)))
        .child(div().flex().flex_col().gap(px(6.)).text_size(px(14.))
            .child(tr("模型", "Model")).child(input))
        .child(div().flex().flex_col().items_start().gap(px(6.)).text_size(px(14.))
            .child(tr("认证来源", "Authentication source")).child(auth_control))
        .children(save_error.filter(|error| !error.is_empty()).map(|error| div().text_size(px(14.))
            .text_color(theme.text).child(if crate::theme::is_en() { format!("Could not save: {error}") } else { format!("保存失败：{error}") })))
        .child(div().flex().gap(px(8.)).child(save).child(cancel))
}

use std::cell::RefCell;
use gpui::{AppContext, WeakEntity};
use super::{backend::DshOptions, chat::AiChat};

struct Form { model: Entity<InputState>, block: bool, menu: bool, error: Option<String> }
thread_local! { static FORM: RefCell<Option<Form>> = const { RefCell::new(None) }; }

pub(super) fn open(chat: &AiChat, window: &mut Window, cx: &mut App) {
    let options = chat.dsh_options();
    let model = cx.new(|cx| InputState::new(window, cx).placeholder(tr("输入模型名称", "Enter model name")).default_value(options.model));
    FORM.with(|form| *form.borrow_mut() = Some(Form { model, block: options.auth_source == "block", menu: false, error: None }));
}

pub(super) fn close() -> bool { FORM.with(|form| form.borrow_mut().take().is_some()) }

pub(super) fn view(chat: WeakEntity<AiChat>) -> Option<Div> {
    let (model, block, menu, error) = FORM.with(|form| form.borrow().as_ref().map(|f| (f.model.clone(), f.block, f.menu, f.error.clone())))?;
    let toggle = chat.clone();
    let select = chat.clone();
    let save = chat.clone();
    let cancel = chat;
    let save_model = model.clone();
    Some(config_card(&model, block, menu, false, error.as_deref(), move |_, cx| {
        FORM.with(|form| if let Some(f) = form.borrow_mut().as_mut() { f.menu = !f.menu; });
        let _ = toggle.update(cx, |_, cx| cx.notify());
    }, move |block, _, cx| {
        FORM.with(|form| if let Some(f) = form.borrow_mut().as_mut() { f.block = block; f.menu = false; });
        let _ = select.update(cx, |_, cx| cx.notify());
    }, move |_, cx| {
        let model = save_model.read(cx).value().to_string();
        let block = FORM.with(|form| form.borrow().as_ref().is_some_and(|f| f.block));
        let options = DshOptions { provider: "deepseek-official".into(), model, auth_source: if block { "block" } else { "native" }.into() };
        let _ = save.update(cx, |chat, cx| {
            match chat.set_dsh_options(options, cx) {
                Ok(()) => { close(); },
                Err(error) => FORM.with(|form| if let Some(f) = form.borrow_mut().as_mut() { f.error = Some(error); }),
            }
            cx.notify();
        });
    }, move |_, cx| {
        close();
        let _ = cancel.update(cx, |_, cx| cx.notify());
    }))
}
