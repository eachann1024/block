//! Bodies of the non-AI Settings tabs: appearance, general, shortcuts and local folder.
use gpui::{Context, CursorStyle, MouseButton, div, prelude::*, px};
use gpui_component::input::Input;
use crate::files::state::{LanguageMode, LineHeight, ThemeMode};
use crate::theme::{
    DEFAULT_ASSETS_FOLDER, DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, Theme, assets_folder,
    custom_code_font, custom_editor_font, hide_assets_folder, language_mode, line_height,
    quick_shortcut, resolved_mono_font, resolved_serif_font, search_shortcut, theme_mode, tr,
};
use crate::ui::font_picker::{FontPickerRole, font_picker_popover, font_picker_trigger};
use crate::ui::settings_controls::{
    card_title, segmented, setting_row, settings_card, shortcut_recorder, static_badge, static_keycap,
};
use super::Workspace;
use super::settings::ShortcutKind;

impl Workspace {
    pub(super) fn appearance_tab(&self, cx: &mut Context<Self>) -> gpui::Div {
        let (cur_lang, cur_theme, cur_lh) = (language_mode(), theme_mode(), line_height());
        let theme_seg = segmented("theme-seg", [
            (tr("跟随系统", "System"), cur_theme == ThemeMode::System, ThemeMode::System),
            (tr("浅色", "Light"), cur_theme == ThemeMode::Light, ThemeMode::Light),
            (tr("深色", "Dark"), cur_theme == ThemeMode::Dark, ThemeMode::Dark),
        ], |mode, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.select_theme(mode, cx))));
        let lang_seg = segmented("lang-seg", [
            (tr("跟随系统", "System"), cur_lang == LanguageMode::System, LanguageMode::System),
            ("简体中文", cur_lang == LanguageMode::Zh, LanguageMode::Zh),
            ("English", cur_lang == LanguageMode::En, LanguageMode::En),
        ], |mode, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.select_language(mode, cx))));

        let cur_editor_font = custom_editor_font();
        let (body_name, body_fam) = match cur_editor_font.as_deref() {
            None => (tr("系统无衬线", "System Sans").into(), ".SystemUIFont".into()),
            Some("system-serif") => (tr("系统衬线", "System Serif").into(), resolved_serif_font().into()),
            Some(name) => (name.to_owned().into(), name.to_owned().into()),
        };
        let body_open = self.settings.as_ref().and_then(|m| m.font_picker.as_ref()).is_some_and(|p| p.role == FontPickerRole::Body);
        let body_trigger = font_picker_trigger("body-font-trigger", FontPickerRole::Body, body_open, body_name, body_fam, cx);

        let cur_code_font = custom_code_font();
        let (code_name, code_fam) = match cur_code_font.as_deref() {
            None => (format!("{} ({})", tr("自动", "Auto"), resolved_mono_font()).into(), resolved_mono_font().into()),
            Some(name) => (name.to_owned().into(), name.to_owned().into()),
        };
        let code_open = self.settings.as_ref().and_then(|m| m.font_picker.as_ref()).is_some_and(|p| p.role == FontPickerRole::Code);
        let code_trigger = font_picker_trigger("code-font-trigger", FontPickerRole::Code, code_open, code_name, code_fam, cx);

        let lh_seg = segmented("lh-seg", [
            (tr("紧凑", "Compact"), cur_lh == LineHeight::Tight, LineHeight::Tight),
            (tr("标准", "Standard"), cur_lh == LineHeight::Normal, LineHeight::Normal),
            (tr("宽松", "Relaxed"), cur_lh == LineHeight::Loose, LineHeight::Loose),
        ], |lh, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.select_line_height(lh, cx))));

        let popover = self.settings.as_ref().and_then(|m| m.font_picker.as_ref()).map(|picker| {
            let q = picker.search_input.read(cx).value().to_string();
            font_picker_popover(picker.role, &picker.search_input, &q, cx)
        });

        div().flex().flex_col().gap(px(24.))
            .child(settings_card(16.).child(card_title(tr("主题与语言", "Theme & Language")))
                .child(setting_row(tr("外观主题", "Theme"), theme_seg(cx)))
                .child(setting_row(tr("界面语言", "Language"), lang_seg(cx))))
            .child(settings_card(16.).child(card_title(tr("编辑器", "Editor")))
                .child(setting_row(tr("正文字体", "Body Font"), body_trigger))
                .child(setting_row(tr("代码字体", "Code Font"), code_trigger))
                .child(setting_row(tr("行高", "Line Height"), lh_seg(cx))))
            .when_some(popover, |d, p| d.child(p))
    }

    pub(super) fn general_tab(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let hide_assets = hide_assets_folder();
        let hide_seg = segmented("hide-assets-seg", [
            (tr("默认隐藏", "Hidden by Default"), hide_assets, true),
            (tr("始终显示", "Always Show"), !hide_assets, false),
        ], |hide, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.set_hide_assets(hide, cx))));
        let assets_customized = assets_folder() != DEFAULT_ASSETS_FOLDER || !hide_assets_folder();
        let reset_btn = div().id("reset-assets")
            .px(px(8.)).py(px(4.)).rounded(crate::ui::radius::CONTROL).text_size(px(14.)).text_color(theme.accent)
            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                cx.stop_propagation();
                this.reset_assets_settings(window, cx);
            }))
            .child(tr("恢复默认", "Reset"));

        div().flex().flex_col().gap(px(24.))
            .child(settings_card(16.)
                .child(div().flex().items_center().justify_between()
                    .child(card_title(tr("静态资源", "Static Assets")))
                    .when(assets_customized, |row| row.child(reset_btn)))
                .when_some(self.settings.as_ref(), |card, modal| {
                    card.child(setting_row(
                        tr("全局资源文件夹名", "Global Assets Folder"),
                        div().flex().items_center().gap(px(10.))
                            .child(
                                div().w(px(168.)).h(px(32.)).px(px(8.)).rounded(crate::ui::radius::CONTROL)
                                    .bg(theme.shell).border_1().border_color(theme.btn_border)
                                    .flex().items_center()
                                    .child(Input::new(&modal.assets_input).appearance(false).bordered(false).focus_bordered(false))
                            )
                            .child(static_badge(format!("./{}/", assets_folder()), true)),
                    ))
                })
                .child(setting_row(tr("列表与搜索可见性", "List & Search Visibility"), hide_seg(cx))))
    }

    pub(super) fn shortcuts_tab(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let recording = self.settings.as_ref().and_then(|m| m.recording);
        let (search_spec, quick_spec) = (search_shortcut(), quick_shortcut());
        let customized = search_spec != DEFAULT_SEARCH_SHORTCUT || quick_spec != DEFAULT_QUICK_SHORTCUT;
        let record = |kind: ShortcutKind| cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            if let Some(m) = &mut this.settings {
                m.recording = Some(kind);
                window.focus(&m.focus, cx);
            }
            cx.notify();
        });
        div().child(settings_card(12.)
            .child(div().flex().items_center().justify_between()
                .child(card_title(tr("基础快捷键", "Shortcuts")))
                .when(customized, |row| row.child(div().id("reset-shortcuts")
                    .px(px(8.)).py(px(4.)).rounded(crate::ui::radius::CONTROL).text_size(px(14.)).text_color(theme.accent)
                    .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                        cx.stop_propagation();
                        this.reset_shortcuts(cx);
                    }))
                    .child(tr("恢复默认", "Reset")))))
            .child(setting_row(tr("全局唤出", "Global Summon"),
                shortcut_recorder("rec-search", &search_spec, recording == Some(ShortcutKind::Search), record(ShortcutKind::Search))))
            .child(setting_row(tr("速记小窗", "Quick Note"),
                shortcut_recorder("rec-quick", &quick_spec, recording == Some(ShortcutKind::Quick), record(ShortcutKind::Quick))))
            .child(setting_row(tr("搜索笔记", "Search Notes"), static_keycap("secondary-k")))
            .child(setting_row(tr("当前笔记查找", "Find in Note"), static_keycap("secondary-f")))
            .child(setting_row(tr("新建笔记", "New Note"), static_keycap("secondary-n")))
            .child(setting_row(tr("AI 助手面板", "AI Assistant Panel"), static_keycap("secondary-j"))))
    }


}
