use std::collections::HashMap;
use serde_json::{json, Value};
use super::{Op, apply_op};

fn unclosed_constructs(source: &str) -> Vec<(usize, &'static str)> {
    let mut issues = Vec::new();
    let mut open_fence: Option<(usize, u8, usize)> = None;
    let mut open_table: Option<usize> = None;
    for (line_idx, raw) in source.lines().enumerate() {
        let trimmed = raw.trim_start_matches([' ', '\t']);
        if let Some((_, ch, width)) = open_fence {
            if trimmed.bytes().take_while(|&b| b == ch).count() >= width
                && trimmed.trim_start_matches(ch as char).trim().is_empty()
            {
                open_fence = None;
            }
            continue;
        }
        let first = trimmed.as_bytes().first().copied();
        if let Some(ch) = first.filter(|&b| b == b'`' || b == b'~') {
            let width = trimmed.bytes().take_while(|&b| b == ch).count();
            if width >= 3 && !(ch == b'`' && trimmed[width..].contains('`')) {
                open_fence = Some((line_idx, ch, width));
                continue;
            }
        }
        let lower = trimmed.to_ascii_lowercase();
        if lower.starts_with("<table") && !lower.contains("</table>") {
            open_table.get_or_insert(line_idx);
        }
        if open_table.is_some() && lower.contains("</table>") {
            open_table = None;
        }
    }
    if let Some((line, _, _)) = open_fence { issues.push((line, "unclosed_fence")); }
    if let Some(line) = open_table { issues.push((line, "unclosed_html_table")); }
    issues
}

fn diagnostics(before: Option<&str>, after: &str) -> Vec<(usize, &'static str)> {
    let prev: Vec<&'static str> = unclosed_constructs(before.unwrap_or("")).into_iter().map(|(_, c)| c).collect();
    let mut remaining = prev;
    let mut out = Vec::new();
    for (line, code) in unclosed_constructs(after) {
        if let Some(pos) = remaining.iter().position(|&c| c == code) {
            remaining.remove(pos);
        } else {
            out.push((line, code));
        }
    }
    out
}

pub(in crate::editor::ai) fn check(before: Option<&str>, after: &str) -> bool {
    diagnostics(before, after).is_empty()
}

pub(super) fn simulate(ops: &[Op], expected: &[(String, Option<String>)]) -> Result<(), Value> {
    let mut pages: HashMap<&str, Option<String>> = expected.iter().map(|(page, text)| (page.as_str(), text.clone())).collect();
    for (index, op) in ops.iter().enumerate() {
        let current = pages.get_mut(op.page.as_str()).ok_or_else(|| json!({"error":{"code":"invalid_plan", "operationIndex":index}}))?;
        *current = Some(apply_op(op, current.as_deref()).ok_or_else(|| json!({"error":{"code":"operation_precondition_failed", "operationIndex":index, "message":"操作无法按顺序应用，请核对原文并修正。"}}))?);
    }
    for (index, (page, before)) in expected.iter().enumerate() {
        if let Some(Some(after)) = pages.get(page.as_str()) {
            let issues: Vec<_> = diagnostics(before.as_deref(), after).into_iter()
                .map(|(line, reason)| json!({"lineIndex":line, "reasonCode":reason})).collect();
            if !issues.is_empty() {
                let operation_indices: Vec<_> = ops.iter().enumerate().filter_map(|(at, op)| (op.page == *page).then_some(at)).collect();
                return Err(json!({"error":{"code":"unsupported_markdown", "pageIndex":index, "operationIndices":operation_indices, "blocks":issues, "message":"最终内容包含未闭合的代码围栏或 HTML 表格，请修正后最多重试一次。"}}));
            }
        }
    }
    Ok(())
}
