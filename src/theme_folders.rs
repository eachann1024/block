//! Folder rules settings for hiding/showing folders in sidebar and search.

use std::sync::RwLock;
use serde::{Deserialize, Serialize};

fn default_rule_hidden() -> bool {
    true
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
pub struct FolderRule {
    #[serde(default)]
    pub pattern: String,
    #[serde(default)]
    pub regex: bool,
    #[serde(default = "default_rule_hidden")]
    pub hidden: bool,
}

impl Default for FolderRule {
    fn default() -> Self {
        Self {
            pattern: String::new(),
            regex: false,
            hidden: true,
        }
    }
}

#[derive(Clone)]
struct CompiledRule {
    rule: FolderRule,
    compiled: Option<regex::Regex>,
}

static RULES: RwLock<Vec<CompiledRule>> = RwLock::new(Vec::new());

pub fn folder_rules() -> Vec<FolderRule> {
    RULES
        .read()
        .unwrap_or_else(|e| e.into_inner())
        .iter()
        .map(|cr| cr.rule.clone())
        .collect()
}

pub fn set_folder_rules(rules: Vec<FolderRule>) {
    let compiled_rules: Vec<CompiledRule> = rules
        .into_iter()
        .map(|rule| {
            let compiled = if rule.regex {
                let trimmed = rule.pattern.trim();
                if trimmed.is_empty() {
                    None
                } else {
                    regex::Regex::new(trimmed).ok()
                }
            } else {
                None
            };
            CompiledRule { rule, compiled }
        })
        .collect();
    *RULES.write().unwrap_or_else(|e| e.into_inner()) = compiled_rules;
}

/// Strips ONE surrounding slash pair for NEW regex input (e.g. `/foo/` -> `foo`).
pub fn normalize_folder_pattern(raw: &str) -> String {
    let s = raw.trim();
    if s.len() >= 2 && s.starts_with('/') && s.ends_with('/') {
        s[1..s.len() - 1].to_string()
    } else {
        s.to_string()
    }
}

/// Validates nonempty regex or simple folder name (no path components `.` `..` or separators).
pub fn validate_folder_rule(raw: &str, is_regex: bool) -> Result<String, String> {
    if is_regex {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Err(crate::theme::tr("正则表达式不能为空", "Regex pattern cannot be empty").to_string());
        }
        match regex::Regex::new(trimmed) {
            Ok(_) => Ok(trimmed.to_string()),
            Err(_) => Err(crate::theme::tr("正则表达式格式无效", "Invalid regular expression").to_string()),
        }
    } else {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Err(crate::theme::tr("文件夹名称不能为空", "Folder name cannot be empty").to_string());
        }
        if trimmed == "." || trimmed == ".." {
            return Err(crate::theme::tr("不能使用 . 或 .. 作为文件夹名称", "Cannot use . or .. as folder name").to_string());
        }
        if trimmed.chars().any(|c| c == '/' || c == '\\') {
            return Err(crate::theme::tr("文件夹名称不能包含路径分隔符", "Folder name cannot contain path separators").to_string());
        }
        if trimmed.chars().any(|c| c.is_control()) {
            return Err(crate::theme::tr("文件夹名称不能包含控制字符", "Folder name cannot contain control characters").to_string());
        }
        Ok(trimmed.to_string())
    }
}

/// Exact named entries first, regex enabled match returns Some(true), otherwise None.
pub fn matches_folder_rule(name: &str) -> Option<bool> {
    let guard = RULES.read().unwrap_or_else(|e| e.into_inner());

    for cr in guard.iter() {
        if !cr.rule.regex {
            let pat = cr.rule.pattern.trim();
            if !pat.is_empty() && (pat == name || pat.eq_ignore_ascii_case(name)) {
                return Some(cr.rule.hidden);
            }
        }
    }

    for cr in guard.iter() {
        if cr.rule.regex && cr.rule.hidden {
            if let Some(re) = &cr.compiled {
                if re.is_match(name) {
                    return Some(true);
                }
            }
        }
    }

    None
}

pub fn apply_folder_rules_state(folder_rules: Option<&[FolderRule]>, legacy_regex: Option<&str>) {
    if let Some(rules) = folder_rules {
        set_folder_rules(rules.to_vec());
    } else if let Some(legacy) = legacy_regex {
        let trimmed = legacy.trim();
        if !trimmed.is_empty() {
            set_folder_rules(vec![FolderRule {
                pattern: trimmed.to_string(),
                regex: true,
                hidden: true,
            }]);
        } else {
            set_folder_rules(Vec::new());
        }
    } else {
        set_folder_rules(Vec::new());
    }
}
