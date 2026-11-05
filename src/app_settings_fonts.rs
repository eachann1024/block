//! Editor font settings actions: custom body/code fonts, line height, picker toggle and file import.
use std::path::PathBuf;
use gpui::{AppContext, Context, Window};
use gpui_component::input::InputState;
use crate::files::state::{FontChoice, LineHeight, update_state};
use crate::ui::font_picker::FontPickerRole;
use super::Workspace;
use super::settings::FontPickerModal;

impl Workspace {
    pub(crate) fn select_editor_font(&mut self, font: Option<String>, _window: &mut Window, cx: &mut Context<Self>) {
        crate::theme::set_custom_editor_font(font.clone());
        let legacy_choice = if font.as_deref() == Some("system-serif") { FontChoice::Serif } else { FontChoice::Sans };
        crate::theme::set_font_choice(legacy_choice);
        let save_font = font.clone();
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.editor_font = save_font;
                s.font_family = legacy_choice;
            });
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        self.close_font_picker(cx);
        cx.notify();
    }

    pub(crate) fn select_code_font(&mut self, font: Option<String>, _window: &mut Window, cx: &mut Context<Self>) {
        crate::theme::set_custom_code_font(font.clone());
        let save_font = font.clone();
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
        crate::ui::font_picker::set_font_picker_error(None);
        let search_input = cx.new(|cx| {
            InputState::new(window, cx).placeholder(crate::theme::tr("搜索字体", "Search fonts"))
        });
        let _search_sub = cx.subscribe_in(&search_input, window, |_, _, event: &gpui_component::input::InputEvent, _, cx| {
            if matches!(event, gpui_component::input::InputEvent::Change) {
                crate::ui::font_picker::set_font_picker_error(None);
                cx.notify();
            }
        });
        search_input.update(cx, |i, cx| i.focus(window, cx));
        modal.font_picker = Some(FontPickerModal { role, search_input, _search_sub });
        cx.notify();
    }

    pub(crate) fn close_font_picker(&mut self, cx: &mut Context<Self>) -> bool {
        crate::ui::font_picker::set_font_picker_error(None);
        let Some(modal) = &mut self.settings else { return false };
        if modal.font_picker.take().is_some() {
            cx.notify();
            true
        } else {
            false
        }
    }

    pub(crate) fn import_font_dialog(&mut self, role: FontPickerRole, window: &mut Window, cx: &mut Context<Self>) {
        let prompt = cx.prompt_for_paths(gpui::PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some(crate::theme::tr("导入字体文件", "Import Font File").into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let Ok(Ok(Some(paths))) = prompt.await else { return };
            let Some(path) = paths.into_iter().next() else { return };
            let _ = this.update_in(cx, |this, window, cx| {
                this.handle_imported_font(role, path, window, cx);
            });
        }).detach();
    }

    fn handle_imported_font(&mut self, role: FontPickerRole, path: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        match crate::fonts::import_custom_font(&path, cx) {
            Ok(family) => {
                crate::ui::font_picker::set_font_picker_error(None);
                crate::theme::init_fonts(&cx.text_system().all_font_names());
                match role {
                    FontPickerRole::Body => self.select_editor_font(Some(family), window, cx),
                    FontPickerRole::Code => self.select_code_font(Some(family), window, cx),
                }
            }
            Err(err) => {
                crate::log::warn(&format!("Failed to import font from {:?}: {err}", path));
                crate::ui::font_picker::set_font_picker_error(Some(err.clone()));
                self.flash(format!("{}: {err}", crate::theme::tr("字体导入失败", "Failed to import font")), cx);
                cx.notify();
            }
        }
    }
}
