use gpui::{AnyElement, IntoElement, ParentElement, Styled, div, px};
use serde_json::Value;
use super::super::chat::{AiChat, Msg};

fn plain_excerpt(markdown: &str) -> String {
    use pulldown_cmark::{Event, Parser, TagEnd};
    let mut text = String::new();
    for event in Parser::new(markdown) {
        match event {
            Event::Text(value) | Event::Code(value) => text.push_str(&value),
            Event::SoftBreak | Event::HardBreak => text.push(' '),
            Event::End(TagEnd::Paragraph | TagEnd::Heading(_) | TagEnd::CodeBlock | TagEnd::Item) if !text.trim().is_empty() => break,
            _ => {}
        }
        if text.chars().count() >= 90 { break; }
    }
    text.trim().chars().take(90).collect()
}

pub(super) fn render(turn: &[Msg], start: usize, cx: &mut gpui::Context<AiChat>) -> Vec<AnyElement> {
    let mut sources = Vec::new();
    let mut seen = Vec::new();
    for message in turn {
        for call in message.tool_calls.as_ref().and_then(Value::as_array).into_iter().flatten() {
            if call.pointer("/function/name").and_then(Value::as_str) != Some("readPage") { continue; }
            let id = call.get("id").and_then(Value::as_str);
            let result = turn.iter().find(|item| item.role == "tool" && item.tool_call_id.as_deref() == id)
                .and_then(|item| serde_json::from_str::<Value>(&item.content).ok()).unwrap_or_default();
            if result.get("error").is_some() { continue; }
            let Some(page) = result.get("pageId").and_then(Value::as_str) else { continue };
            let args = call.pointer("/function/arguments").and_then(Value::as_str).and_then(|text| serde_json::from_str::<Value>(text).ok()).unwrap_or_default();
            let root = message.source_roots.as_ref().and_then(|roots| roots.iter().find(|(call_id, _)| Some(call_id.as_str()) == id)).map(|(_, root)| root.clone())
                .or_else(|| args.get("notebookRoot").and_then(Value::as_str).map(str::to_owned));
            if seen.contains(&(root.clone(), page.to_owned())) { continue; }
            seen.push((root.clone(), page.to_owned()));
            let page = page.to_owned();
            let title: String = result.get("title").and_then(Value::as_str).unwrap_or(&page).chars().take(100).collect();
            let markdown = result.get("markdown").and_then(Value::as_str).unwrap_or("");
            let count = markdown.chars().count();
            let excerpt = plain_excerpt(markdown);
            let Some(root) = root else {
                let label = div().text_size(px(14.)).text_color(crate::ui::ai_theme::theme().text_secondary).child(page).into_any_element();
                sources.push(crate::ui::ai_plan_card::source_card(title, count, excerpt, label).into_any_element());
                continue;
            };
            let link = crate::ui::ai_plan_card::pill(format!("ai-source-{start}-{}", sources.len()), page.clone(), false,
                cx.listener(move |this, _, _, cx| {
                    let path = this.context_for_root(std::path::Path::new(&root), cx).and_then(|context| { context.checked_path(&page)?; Ok(context.path(&page)) });
                    match path {
                        Ok(path) if path.is_file() => {
                            if let Some((_, workspace)) = crate::search::window::main_window(cx) { workspace.update(cx, |workspace, cx| workspace.open_file_with_mode(path, false, true, cx)); }
                            else if let Some(editor) = this.editor.as_ref().and_then(|editor| editor.upgrade()) { editor.update(cx, |editor, cx| editor.open(path, cx)); }
                        },
                        Ok(_) => { this.error = Some("Note does not exist".into()); cx.notify(); },
                        Err(error) => { this.error = Some(error); cx.notify(); },
                    }
                })).h(px(24.)).py(px(0.)).text_size(px(12.)).into_any_element();
            sources.push(crate::ui::ai_plan_card::source_card(title, count, excerpt, link).into_any_element());
        }
    }
    sources
}
