use std::collections::HashMap;
use serde_json::{json, Value};
use super::{Op, apply_op};

fn unsupported(source: &str) -> Vec<(usize, String, &'static str)> {
    crate::editor::markdown::parse(source).blocks.iter().enumerate().filter_map(|(index, block)| {
        let raw = block.meta.raw_source.as_ref()?;
        if block.meta.unsupported_reason.as_deref() == Some("Image") || crate::editor::block_menu::is_divider(block) {
            return None;
        }
        let reason = block.meta.unsupported_reason.as_deref().unwrap_or("");
        let code = if reason == "Nested or indented code is retained as source" { "nested_or_indented_code" }
            else if reason.to_ascii_lowercase().contains("table") || reason.to_ascii_lowercase().contains("cell") { "unsupported_table" }
            else if reason.to_ascii_lowercase().contains("html") { "unsupported_html" }
            else if reason == "Unsupported construct is retained as original Markdown" { "unsupported_block" }
            else { "unsupported_markdown" };
        Some((index, raw.clone(), code))
    }).collect()
}

fn diagnostics(before: Option<&str>, after: &str) -> Vec<(usize, &'static str)> {
    let mut originals = HashMap::<String, usize>::new();
    for (_, raw, _) in unsupported(before.unwrap_or("")) { *originals.entry(raw).or_default() += 1; }
    let mut issues = Vec::new();
    for (index, raw, code) in unsupported(after) {
        match originals.get_mut(&raw) {
            Some(count) if *count > 0 => *count -= 1,
            _ => { issues.push((index, code)); if issues.len() == 5 { break; } },
        }
    }
    issues
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
                .map(|(block, reason)| json!({"blockIndex":block, "reasonCode":reason})).collect();
            if !issues.is_empty() {
                let operation_indices: Vec<_> = ops.iter().enumerate().filter_map(|(at, op)| (op.page == *page).then_some(at)).collect();
                return Err(json!({"error":{"code":"unsupported_markdown", "pageIndex":index, "operationIndices":operation_indices, "blocks":issues, "message":"最终内容包含新增的不支持 Markdown 结构；索引从 0 开始，blockIndex 为最终内容的解析块序号。请修正后最多重试一次。"}}));
            }
        }
    }
    Ok(())
}
