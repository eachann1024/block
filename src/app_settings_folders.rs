//! Folder settings actions, validation, duplicate prevention and persistence (Design A).
use gpui::{Context, Window};
use crate::files::state::update_state;
use crate::theme::{DEFAULT_ASSETS_FOLDER, FolderRule, tr};
use super::Workspace;

pub(crate) fn folder_draft_placeholder() -> &'static str {
    tr("输入文件夹名称，如 build 或 drafts…", "Enter folder name, e.g. build or drafts…")
}

pub(crate) fn regex_draft_placeholder() -> &'static str {
    tr("temp|cache，名称包含 temp 或 cache", "temp|cache, name contains temp or cache")
}

pub(crate) fn normalize_pasted_regex_affixes(val: &str) -> Option<String> {
    let s = val.trim();
    if s.len() >= 2 && s.starts_with('/') && s.ends_with('/') {
        Some(s[1..s.len() - 1].to_string())
    } else {
        None
    }
}

impl Workspace {
    pub(super) fn toggle_search_folder_ignored(&mut self, folder: std::path::PathBuf, cx: &mut Context<Self>) {
        let mut list = crate::theme::ignored_search_folders();
        if list.iter().any(|p| p == &folder) {
            list.retain(|p| p != &folder);
        } else {
            list.push(folder);
        }
        crate::theme::set_ignored_search_folders(list.clone());
        cx.background_executor().spawn(async move {
            let _ = update_state(move |s| s.ignored_search_folders = list);
        }).detach();
        let folders = self.search_folders();
        if let Some(search) = &mut self.search {
            search.state.sync_folders(folders);
        }
        cx.notify();
    }

    pub(super) fn remove_notebook_folder(&mut self, folder: std::path::PathBuf, cx: &mut Context<Self>) {
        if self.folder.as_ref() == Some(&folder) { return; }
        self.notebooks.retain(|known| known != &folder);
        let mut ignored = crate::theme::ignored_search_folders();
        ignored.retain(|p| p != &folder);
        crate::theme::set_ignored_search_folders(ignored.clone());
        cx.background_executor().spawn(async move {
            let _ = update_state(move |s| {
                s.notebooks.retain(|known| known != &folder);
                s.ignored_search_folders = ignored;
            });
        }).detach();
        let folders = self.search_folders();
        if let Some(search) = &mut self.search {
            search.state.sync_folders(folders);
        }
        cx.notify();
    }

    pub(super) fn handle_regex_draft_change(
        &mut self,
        input: &gpui::Entity<gpui_component::input::InputState>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let val = input.read(cx).value().to_string();
        if let Some(normalized) = normalize_pasted_regex_affixes(&val) {
            if normalized != val {
                input.update(cx, |i, cx| i.set_value(normalized, window, cx));
            }
        }
        if let Some(m) = &mut self.settings {
            if m.regex_error.take().is_some() {
                cx.notify();
            }
        }
    }

    pub(super) fn set_assets_folder(&mut self, sanitized: String, cx: &mut Context<Self>) {
        crate::theme::set_assets_folder(sanitized);

        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                let cur_assets = crate::theme::assets_folder();
                s.assets_folder = (cur_assets != DEFAULT_ASSETS_FOLDER).then_some(cur_assets);
                let cur_hide = crate::theme::hide_assets_folder();
                s.hide_assets_folder = (!cur_hide).then_some(false);
            });
        }).detach();
        self.rescan_current_folder(cx);
    }

    pub(super) fn save_folder_rules(&mut self, rules: Vec<FolderRule>, cx: &mut Context<Self>) {
        crate::theme::set_folder_rules(rules);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                let cur_assets = crate::theme::assets_folder();
                s.assets_folder = (cur_assets != DEFAULT_ASSETS_FOLDER).then_some(cur_assets);
                let cur_hide = crate::theme::hide_assets_folder();
                s.hide_assets_folder = (!cur_hide).then_some(false);
                let latest = crate::theme::folder_rules();
                s.folder_rules = Some(latest);
                s.hidden_folder_regex = None;
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn toggle_folder_rule_visibility(&mut self, index: usize, hidden: bool, cx: &mut Context<Self>) {
        let mut rules = crate::theme::folder_rules();
        if let Some(rule) = rules.get_mut(index) {
            if rule.hidden != hidden {
                rule.hidden = hidden;
                self.save_folder_rules(rules, cx);
            }
        }
    }

    pub(super) fn remove_folder_rule(&mut self, index: usize, cx: &mut Context<Self>) {
        let mut rules = crate::theme::folder_rules();
        if index < rules.len() {
            rules.remove(index);
            self.save_folder_rules(rules, cx);
        }
    }

    pub(super) fn add_exact_folder_rule(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(modal) = &mut self.settings else { return };
        let raw = modal.folder_draft_input.read(cx).value().to_string();

        match crate::theme::validate_folder_rule(&raw, false) {
            Err(err) => {
                modal.folder_error = Some(err);
                cx.notify();
            }
            Ok(valid) => {
                let current_assets = crate::theme::assets_folder();
                if valid.eq_ignore_ascii_case(&current_assets) {
                    modal.folder_error = Some(tr("该名称已作为图片保存目录使用", "Already used as images folder").into());
                    cx.notify();
                    return;
                }
                let mut rules = crate::theme::folder_rules();
                if rules.iter().any(|r| !r.regex && r.pattern.eq_ignore_ascii_case(&valid)) {
                    modal.folder_error = Some(tr("文件夹规则已存在", "Folder rule already exists").into());
                    cx.notify();
                    return;
                }
                modal.folder_error = None;
                modal.folder_draft_input.update(cx, |input, cx| input.set_value(String::new(), window, cx));
                rules.push(FolderRule {
                    pattern: valid,
                    regex: false,
                    hidden: true,
                });
                self.save_folder_rules(rules, cx);
            }
        }
    }

    pub(super) fn add_regex_folder_rule(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(modal) = &mut self.settings else { return };
        let raw = modal.regex_draft_input.read(cx).value().to_string();
        let pattern = crate::theme::normalize_folder_pattern(&raw);

        match crate::theme::validate_folder_rule(&pattern, true) {
            Err(err) => {
                modal.regex_error = Some(err);
                cx.notify();
            }
            Ok(valid) => {
                let mut rules = crate::theme::folder_rules();
                if rules.iter().any(|r| r.regex && r.pattern == valid) {
                    modal.regex_error = Some(tr("正则表达式规则已存在", "Regex rule already exists").into());
                    cx.notify();
                    return;
                }
                modal.regex_error = None;
                modal.regex_draft_input.update(cx, |input, cx| input.set_value(String::new(), window, cx));
                rules.push(FolderRule {
                    pattern: valid,
                    regex: true,
                    hidden: true,
                });
                self.save_folder_rules(rules, cx);
            }
        }
    }

    pub(super) fn set_hide_assets(&mut self, hide: bool, cx: &mut Context<Self>) {
        crate::theme::set_hide_assets_folder(hide);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                let cur_assets = crate::theme::assets_folder();
                s.assets_folder = (cur_assets != DEFAULT_ASSETS_FOLDER).then_some(cur_assets);
                let cur_hide = crate::theme::hide_assets_folder();
                s.hide_assets_folder = (!cur_hide).then_some(false);
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn toggle_hide_non_markdown_files(&mut self, cx: &mut Context<Self>) {
        let next = !crate::theme::hide_non_markdown_files();
        crate::theme::set_hide_non_markdown_files(next);
        cx.background_executor().spawn(async move {
            let _ = update_state(move |s| {
                s.hide_non_markdown_files = (!next).then_some(false);
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }
}
