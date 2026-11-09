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
        let cursor = filtered.iter().position(|item| item.selected).unwrap_or(0);
        if !filtered.is_empty() { crate::ui::menu_nav::reveal_uniform(&scroll_handle, cursor); }
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
        modal.font_picker_leaving = false;
        crate::ui::motion::popup_reset("font-picker-body");
        crate::ui::motion::popup_reset("font-picker-code");
        modal.font_picker = Some(FontPickerModal {
            role,
            search_input,
            scroll_handle,
            catalog,
            filtered,
            cursor,
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
        let len = picker.filtered.len();
        if q.trim().is_empty() {
            picker.cursor = picker.filtered.iter().position(|item| item.selected).unwrap_or(0);
        } else if len == 0 {
            picker.cursor = 0;
        } else {
            picker.cursor = picker.cursor.min(len - 1);
        }
        if len > 0 { crate::ui::menu_nav::reveal_uniform(&picker.scroll_handle, picker.cursor); }
        cx.notify();
    }

    pub(crate) fn close_font_picker(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(modal) = &mut self.settings else { return false };
        if modal.font_picker.is_none() { return false; }
        if modal.font_picker_leaving { return true; }
        modal.font_picker_leaving = true;
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_millis(crate::ui::motion::POPUP_MS)).await;
            let _ = this.update(cx, |this, cx| {
                if let Some(modal) = &mut this.settings && modal.font_picker_leaving {
                    modal.font_picker = None;
                    modal.font_picker_leaving = false;
                }
                cx.notify();
            });
        }).detach();
        true
    }

    pub(crate) fn step_font_picker(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(picker) = self.settings.as_mut().and_then(|modal| modal.font_picker.as_mut()) else { return false };
        let len = picker.filtered.len();
        if len == 0 { return false; }
        let next = crate::ui::menu_nav::step_uniform(picker.cursor, len, down);
        let changed = next != picker.cursor.min(len - 1);
        picker.cursor = next;
        if changed { crate::ui::menu_nav::reveal_uniform(&picker.scroll_handle, next); }
        cx.notify();
        true
    }

    pub(crate) fn accept_font_picker(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(picker) = self.settings.as_ref().and_then(|modal| modal.font_picker.as_ref()) else { return false };
        if picker.filtered.is_empty() { return false; }
        let name = picker.filtered[picker.cursor.min(picker.filtered.len() - 1)].name.to_string();
        let role = picker.role;
        match role {
            FontPickerRole::Body => self.select_editor_font(name, window, cx),
            FontPickerRole::Code => self.select_code_font(name, window, cx),
        }
        true
    }
}
