//! Unified Folder Settings tab view (Design A).
use gpui::{
    Context, CursorStyle, MouseButton, TextAlign, div, prelude::*, px,
};
use gpui_component::input::Input;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{Theme, assets_folder, display_folder_path, hide_assets_folder, is_search_folder_ignored, tr};
use crate::ui::folder_controls::{
    folder_search_ignore_pill, folder_table_row, folder_visibility_segmented, slash_affix_input,
};
use crate::ui::icon_button::icon_button;
use crate::ui::settings_controls::{
    action_button, card_title, mono_font, settings_card,
};
use super::Workspace;

impl Workspace {
    pub(super) fn folders_tab(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let Some(modal) = self.settings.as_ref() else { return div(); };

        let hide_assets = hide_assets_folder();
        let current_assets = assets_folder();
        let rules = crate::theme::folder_rules();
        let notebooks = crate::search::all_notebook_folders(self.folder.clone(), &self.notebooks);
        let notebook_rows = notebooks.into_iter().enumerate().map(|(ix, folder_path)| {
            let is_ignored = is_search_folder_ignored(&folder_path);
            let is_current = self.folder.as_ref() == Some(&folder_path);
            let folder_colors = if is_ignored { theme.mono_icon_colors(theme.text_secondary) } else { theme.icon_colors(theme.text) };
            let name = crate::search::folder_name(&folder_path);
            let short_path = display_folder_path(&folder_path);
            let left = div().flex().items_center().gap(px(10.)).flex_1().min_w_0()
                .child(icon(Icon::Folder, px(16.), folder_colors))
                .child(div().flex_none().font(mono_font()).text_size(px(14.)).text_color(theme.text).child(name))
                .child(div().min_w_0().truncate().font(mono_font()).text_size(px(14.)).text_color(theme.text_secondary).child(short_path));
            let toggle_path = folder_path.clone();
            let probe = if ix == 0 { Some("folder-search-ignore-0") } else { None };
            let pill = folder_search_ignore_pill(
                ("folder-search-ignore", ix), probe, is_ignored,
                cx.listener(move |this, _, _, cx| this.toggle_search_folder_ignored(toggle_path.clone(), cx)),
            );
            let trailing = if is_current {
                div().flex_none().min_w(px(28.)).whitespace_nowrap().text_align(TextAlign::Right).text_size(px(14.)).text_color(theme.text_secondary).child(tr("当前", "Current")).into_any_element()
            } else {
                let remove_path = folder_path.clone();
                div().flex_none().min_w(px(28.)).flex().justify_end()
                    .child(icon_button(("folder-remove-notebook", ix), Icon::Close, true,
                        cx.listener(move |this, _, _, cx| this.remove_notebook_folder(remove_path.clone(), cx))))
                    .into_any_element()
            };
            let right = div().flex().items_center().gap(px(10.)).flex_none().child(pill).child(trailing);
            folder_table_row(("folder-row-notebook", ix), is_ignored, left, right)
        }).collect::<Vec<_>>();

        let assets_seg = folder_visibility_segmented(
            "assets-visibility-seg",
            hide_assets,
            |hide, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.set_hide_assets(hide, cx))),
        )(cx);

        let assets_left = div().flex().items_center().gap(px(10.)).min_w_0()
            .child(icon(Icon::Folder, px(16.), theme.icon_colors(theme.text)))
            .child(
                div().w(px(150.)).h(px(30.)).px(px(8.)).rounded(crate::ui::radius::CONTROL)
                    .bg(theme.shell).border_1().border_color(theme.btn_border)
                    .flex().items_center().font(mono_font()).text_size(px(14.))
                    .child(Input::new(&modal.assets_input).appearance(false).bordered(false).focus_bordered(false))
            );

        let assets_right = div().flex().items_center().gap(px(10.)).flex_none()
            .child(assets_seg)
            .child(div().flex_none().min_w(px(28.)).whitespace_nowrap().text_align(TextAlign::Right).text_size(px(14.)).text_color(theme.text_secondary).child(tr("系统", "System")));

        let assets_row = folder_table_row("folder-row-assets", hide_assets, assets_left, assets_right);

        let exact_rows = rules.iter().enumerate().filter(|(_, r)| !r.regex && !r.pattern.eq_ignore_ascii_case(&current_assets)).map(|(ix, rule)| {
            let folder_colors = if rule.hidden {
                theme.mono_icon_colors(theme.text_secondary)
            } else {
                theme.icon_colors(theme.text)
            };
            let left = div().flex().items_center().gap(px(10.)).flex_1().min_w_0()
                .child(icon(Icon::Folder, px(16.), folder_colors))
                .child(div().min_w_0().truncate().font(mono_font()).text_size(px(14.)).text_color(theme.text).child(rule.pattern.clone()));
            let seg = folder_visibility_segmented(
                ("folder-seg-exact", ix),
                rule.hidden,
                move |hidden, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.toggle_folder_rule_visibility(ix, hidden, cx))),
            )(cx);
            let remove_btn = icon_button(
                ("folder-remove-exact", ix),
                Icon::Close,
                true,
                cx.listener(move |this, _, _, cx| this.remove_folder_rule(ix, cx)),
            );
            let right = div().flex().items_center().gap(px(10.)).flex_none().child(seg).child(remove_btn);
            folder_table_row(("folder-row-exact", ix), rule.hidden, left, right)
        });

        let regex_rules: Vec<_> = rules.iter().enumerate().filter(|(_, r)| r.regex).collect();
        let regex_count = regex_rules.len();
        let advanced_open = modal.advanced_open;
        let toggle_icon = if advanced_open { Icon::ChevronDown } else { Icon::ChevronRight };

        let folder_error = modal.folder_error.as_ref().map(|err| {
            div().px(px(2.)).text_size(px(14.)).text_color(theme.accent).child(err.clone())
        });
        let regex_error = modal.regex_error.as_ref().map(|err| {
            div().px(px(2.)).text_size(px(14.)).text_color(theme.accent).child(err.clone())
        });

        div().flex().flex_col().gap(px(24.))
            .child(settings_card(16.)
                .child(div().flex().flex_col().gap(px(2.))
                    .child(card_title(tr("工作区文件夹清单", "Workspace Folder Rules")))
                    .child(div().text_size(px(14.)).text_color(theme.text_secondary)
                        .child(tr("隐藏影响侧栏与搜索，不删除文件", "Hiding affects sidebar & search; files are not deleted"))))
                .child(div().flex().flex_col().gap(px(8.))
                    .child(assets_row)
                    .children(exact_rows))
                .child(div().flex().flex_col().gap(px(6.))
                    .child(div().flex().items_center().gap(px(8.)).pt(px(4.))
                        .child(
                            div().id("folder-draft-container").flex_1().h(px(34.)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
                                .bg(theme.shell).border_1().border_color(theme.btn_border).flex().items_center()
                                .child(Input::new(&modal.folder_draft_input).appearance(false).bordered(false).focus_bordered(false))
                        )
                        .child(
                            action_button("folder-add-btn", tr("添加文件夹", "Add Folder"), false, true)
                                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.add_exact_folder_rule(window, cx)))
                        ))
                    .children(folder_error))
                .child(div().pt(px(12.)).border_t_1().border_color(theme.hairline).flex().flex_col().gap(px(10.))
                    .child(
                        div().id("folder-advanced-toggle").group("folder-advanced-toggle").flex().items_center().gap(px(6.)).cursor(CursorStyle::PointingHand)
                            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                                if let Some(m) = &mut this.settings { m.advanced_open = !m.advanced_open; cx.notify(); }
                            }))
                            .child(stateful_icon("folder-advanced-toggle", toggle_icon, px(14.), advanced_open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
                            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(tr("高级规则（正则表达式）", "Advanced Rules (Regex)")))
                            .when(regex_count > 0, |d| {
                                d.child(div().px(px(6.)).py(px(1.)).rounded(crate::ui::radius::CONTROL)
                                    .bg(theme.shell).border_1().border_color(theme.hairline)
                                    .text_size(px(14.)).text_color(theme.text_secondary).child(format!("{regex_count}")))
                            })
                    )
                    .when(advanced_open, |adv| {
                        adv.child(div().flex().flex_col().gap(px(8.))
                            .child(div().flex().items_center().gap(px(8.))
                                .child(slash_affix_input("regex-draft-container", &modal.regex_draft_input))
                                .child(
                                    action_button("regex-add-btn", tr("添加正则", "Add Regex"), false, true)
                                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.add_regex_folder_rule(window, cx)))
                                ))
                            .children(regex_error)
                            .when(!regex_rules.is_empty(), |list| {
                                list.child(div().flex().flex_col().gap(px(8.)).pt(px(4.))
                                    .children(regex_rules.into_iter().map(|(ix, rule)| {
                                        let code_colors = if rule.hidden {
                                            theme.mono_icon_colors(theme.text_secondary)
                                        } else {
                                            theme.icon_colors(theme.text)
                                        };
                                        let left = div().flex().items_center().gap(px(10.)).flex_1().min_w_0()
                                            .child(icon(Icon::Code, px(15.), code_colors))
                                            .child(div().min_w_0().truncate().font(mono_font()).text_size(px(14.)).text_color(theme.text).child(format!("/{}/", rule.pattern)));
                                        let seg = folder_visibility_segmented(
                                            ("folder-seg-regex", ix),
                                            rule.hidden,
                                            move |hidden, cx: &mut Context<Self>| Box::new(cx.listener(move |this, _, _, cx| this.toggle_folder_rule_visibility(ix, hidden, cx))),
                                        )(cx);
                                        let remove_btn = icon_button(
                                            ("folder-remove-regex", ix),
                                            Icon::Close,
                                            true,
                                            cx.listener(move |this, _, _, cx| this.remove_folder_rule(ix, cx)),
                                        );
                                        let right = div().flex().items_center().gap(px(10.)).flex_none().child(seg).child(remove_btn);
                                        folder_table_row(("folder-row-regex", ix), rule.hidden, left, right)
                                    })))
                            }))
                    })))
            .child(settings_card(16.)
                .child(card_title(tr("全局搜索忽略列表", "Global Search Ignore List")))
                .child(div().flex().flex_col().gap(px(8.)).children(notebook_rows)))
    }
}
