#[cfg(target_family = "wasm")]
use std::sync::{Arc, atomic::AtomicBool};
use std::{collections::HashMap, sync::mpsc};
use serde::{Deserialize, Serialize};
use crate::editor::ai::provider;
use crate::theme::tr;

#[derive(Serialize)]
pub(super) struct RewriteTask {
    pub id: u64,
    pub selected_text: String,
    pub annotation: String,
    pub previous_rewrite: Option<String>,
    pub follow_up: Option<String>,
}

#[derive(Deserialize)]
struct RewriteResult {
    id: u64,
    replacement: String,
}

pub(super) fn stream(document: String, tasks: Vec<RewriteTask>, cx: &gpui::App) -> mpsc::Receiver<provider::Event> {
    let system = "你是 Block 笔记的批注修订助手。请结合笔记全文和每条批注，为每条选中文字生成替换文本。严格遵循批注与追问，保留原文语言、语气和必要格式；若提供了 previous_rewrite 且有 follow_up，则在 previous_rewrite 基础上按 follow_up 修订；若提供了 previous_rewrite 但没有 follow_up，请换一种表达重新改写，避免与 previous_rewrite 重复；不要改动未选中的文字。只输出 JSON 数组，每项包含数字 id 和字符串 replacement，不要输出代码围栏或解释。".to_owned();
    let tasks = serde_json::to_string(&tasks).unwrap_or_else(|_| "[]".into());
    let user = format!("笔记全文（Markdown）：\n---\n{document}\n---\n\n批注任务（JSON）：\n{tasks}\n\n请为每条任务返回替换后的选中文字。replacement 只包含选区的新内容，不要包含解释或选区之外的内容。结果格式：[{{\"id\":1,\"replacement\":\"改写后的文字\"}}]。");
    #[cfg(not(target_family = "wasm"))]
    { crate::editor::ai::inline_backend::annotation_stream(system, user, cx) }
    #[cfg(target_family = "wasm")]
    { let _ = cx; provider::stream(system, user, Arc::new(AtomicBool::new(false))) }
}

pub(super) fn parse_response(raw: &str) -> Result<HashMap<u64, String>, String> {
    let start = raw.find('[').ok_or_else(|| tr("模型未返回批注结果", "Model returned no annotation results").to_owned())?;
    let end = raw.rfind(']').filter(|end| *end >= start)
        .ok_or_else(|| tr("模型返回的批注结果格式异常", "Malformed annotation response from model").to_owned())?;
    let results: Vec<RewriteResult> = serde_json::from_str(&raw[start..=end])
        .map_err(|_| tr("模型返回的批注结果格式异常", "Malformed annotation response from model").to_owned())?;
    let mut rewrites = HashMap::new();
    for result in results {
        let replacement = result.replacement.trim().to_owned();
        if !replacement.is_empty() { rewrites.insert(result.id, replacement); }
    }
    if rewrites.is_empty() { Err(tr("模型没有生成可用的批注结果", "Model produced no usable annotation results").to_owned()) } else { Ok(rewrites) }
}
