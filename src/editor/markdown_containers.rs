//! Markdown round-trip for GitHub alert callouts (`> [!NOTE]`).
use super::inline;
use super::super::model::{Block, CalloutKind, Kind};

pub(super) fn parse_callout(id: u64, lines: &[&str]) -> Option<(Block, usize, usize)> {
    let first = lines.first()?.trim_end_matches(['\r', '\n']);
    let trimmed = first.trim_start_matches(' ');
    if first.len() - trimmed.len() > 3 { return None; }
    let kind = CalloutKind::from_alert(trimmed.strip_prefix('>')?)?;
    let mut bodies: Vec<&str> = Vec::new();
    let mut end = 1;
    while end < lines.len() {
        let line = lines[end].trim_end_matches(['\r', '\n']);
        let rest = line.trim_start_matches(' ');
        if line.len() - rest.len() > 3 { break; }
        let Some(after) = rest.strip_prefix('>') else { break };
        let body = after.strip_prefix(' ').unwrap_or(after);
        if CalloutKind::from_alert(body).is_some() { break; }
        bodies.push(body);
        end += 1;
    }
    let mut block = Block::new(id, Kind::Quote, String::new());
    block.meta.callout = Some(kind);
    for (i, body) in bodies.iter().enumerate() {
        if i > 0 { block.replace_range(block.text.len()..block.text.len(), "\n"); }
        if body.is_empty() { continue; }
        let (text, spans, tags) = inline::parse(body)?;
        let mut part = Block::new(0, Kind::Paragraph, text);
        part.spans = spans; part.meta.inline = tags;
        block.append(&part);
    }
    let bytes = lines[..end].iter().map(|l| l.len()).sum();
    Some((block, end, bytes))
}

pub(super) fn serialize_callout(block: &Block, kind: CalloutKind, out: &mut String) {
    out.push_str("> [!"); out.push_str(kind.tag()); out.push_str("]\n");
    if block.text.is_empty() && block.spans.is_empty() && block.meta.inline.is_empty() { return; }
    let mut start = 0;
    for part in block.text.split('\n') {
        let end = start + part.len();
        let slice = block.slice(start..end);
        out.push_str("> ");
        inline::serialize(&slice, out);
        out.push('\n');
        start = end + 1;
    }
}
