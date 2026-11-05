//! Editor font settings actions: custom body/code fonts, line height, picker toggle and search.
use gpui::{AppContext, Context, Window};
use gpui_component::input::InputState;
use crate::files::state::{FontChoice, LineHeight, update_state};
use crate::ui::font_picker::FontPickerRole;
use super::Workspace;
use super::settings::FontPickerModal;

impl Workspace {
    pub(crate) fn select_editor_font(&mut self, font: String, _window: &mut Window, cx: &mut Context<Self>) {
        let save_font = (!font.eq_ignore_ascii_case(crate::theme::resolved_body_font())).then_some(font);
        crate::theme::set_custom_editor_font(save_font.clone());
        crate::theme::set_font_choice(FontChoice::Sans);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.editor_font = save_font;
                s.font_family = FontChoice::Sans;
            });
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        self.close_font_picker(cx);
        cx.notify();
    }

    pub(crate) fn select_code_font(&mut self, font: String, _window: &mut Window, cx: &mut Context<Self>) {
        let save_font = (!font.eq_ignore_ascii_case(crate::theme::resolved_mono_font())).then_some(font);
        crate::theme::set_custom_code_font(save_font.clone());
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| { s.code_font = save_font; });
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        self.close_font_picker(cx);
        cx.notify();
    }

    pub(super) fn select_line_height(&mut self, lh: LineHeight, cx: &mut Context<Self>) {
        crate::theme::set_line_height(lh);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.line_height = (lh != LineHeight::Normal).then_some(lh);
            });
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    pub(crate) fn toggle_font_picker(&mut self, role: FontPickerRole, window: &mut Window, cx: &mut Context<Self>) {
        let is_same = self.settings.as_ref()
            .and_then(|m| m.font_picker.as_ref())
            .is_some_and(|p| p.role == role);
        if is_same {
            self.close_font_picker(cx);
        } else {
            self.open_font_picker(role, window, cx);
        }
    }

    pub(super) fn open_font_picker(&mut self, role: FontPickerRole, window: &mut Window, cx: &mut Context<Self>) {
        let Some(modal) = &mut self.settings else { return };
        let cur_font = match role {
            FontPickerRole::Body => crate::theme::active_editor_font(),
            FontPickerRole::Code => crate::theme::active_code_font(),
        };
        let catalog = crate::ui::font_picker_cache::get_or_build_catalog(role);
        let filtered = crate::ui::font_picker_cache::filter_candidates(&catalog, "", &cur_font);
        let scroll_handle = gpui::UniformListScrollHandle::new();
        if let Some(ix) = filtered.iter().position(|item| item.selected) {
            scroll_handle.scroll_to_item_strict(ix, gpui::ScrollStrategy::Center);
        }
        let search_input = cx.new(|cx| {
            InputState::new(window, cx).placeholder(crate::theme::tr("搜索字体", "Search fonts"))
        });
        let _search_sub = cx.subscribe_in(&search_input, window, |this, input, event: &gpui_component::input::InputEvent, _, cx| {
            if matches!(event, gpui_component::input::InputEvent::Change) {
                let q = input.read(cx).value().to_string();
                this.update_font_picker_query(q, cx);
            }
        });
        search_input.update(cx, |i, cx| i.focus(window, cx));
        modal.font_picker = Some(FontPickerModal {
            role,
            search_input,
            scroll_handle,
            catalog,
            filtered,
            last_query: String::new(),
            _search_sub,
        });
        cx.notify();
    }

    pub(crate) fn update_font_picker_query(&mut self, q: String, cx: &mut Context<Self>) {
        let Some(modal) = &mut self.settings else { return };
        let Some(picker) = &mut modal.font_picker else { return };
        if picker.last_query == q {
            return;
        }
        picker.last_query = q.clone();
        let cur_font = match picker.role {
            FontPickerRole::Body => crate::theme::active_editor_font(),
            FontPickerRole::Code => crate::theme::active_code_font(),
        };
        picker.filtered = crate::ui::font_picker_cache::filter_candidates(&picker.catalog, &q, &cur_font);
        if q.trim().is_empty() {
            if let Some(ix) = picker.filtered.iter().position(|item| item.selected) {
                picker.scroll_handle.scroll_to_item_strict(ix, gpui::ScrollStrategy::Center);
            } else {
                picker.scroll_handle.scroll_to_item_strict(0, gpui::ScrollStrategy::Top);
            }
        } else {
            picker.scroll_handle.scroll_to_item_strict(0, gpui::ScrollStrategy::Top);
        }
        cx.notify();
    }

    pub(crate) fn close_font_picker(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(modal) = &mut self.settings else { return false };
        if modal.font_picker.take().is_some() {
            cx.notify();
            true
        } else {
            false
        }
    }
}
