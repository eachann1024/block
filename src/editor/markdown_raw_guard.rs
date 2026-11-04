use super::blocks::{closes_fence, fence};
use std::borrow::Cow;

fn is_marker(line: &str) -> bool {
    let rest = match line.trim_start().strip_prefix("<!--") { Some(rest) => rest.trim_start(), None => return false };
    let rest = match rest.strip_prefix("thinkly").or_else(|| rest.strip_prefix("goose-note")) { Some(rest) => rest, None => return false };
    match rest.strip_prefix(":raw-block") { Some(rest) => rest.trim_start() == "-->", None => false }
}

fn content(line: &str) -> &str { line.trim_end_matches(['\r', '\n']) }

/// Unwrap `thinkly-raw-block` fences so the original HTML is what the editor sees and saves.
pub(super) fn decode(source: &str) -> Cow<'_, str> {
    if !source.contains("raw-block") { return Cow::Borrowed(source); }
    let lines: Vec<&str> = source.split_inclusive('\n').collect();
    let mut out = String::with_capacity(source.len());
    let mut index = 0;
    while index < lines.len() {
        let line = lines[index];
        if content(line).trim() == "$$" {
            let mut end = index + 1;
            while end < lines.len() && content(lines[end]).trim() != "$$" { end += 1; }
            let stop = if end < lines.len() { end + 1 } else { end };
            lines[index..stop].iter().for_each(|l| out.push_str(l));
            index = end + 1; continue;
        }
        let Some((marker, width, info)) = fence(content(line)) else { out.push_str(line); index += 1; continue };
        let Some(end) = (index + 1..lines.len()).find(|&i| closes_fence(content(lines[i]), marker, width)) else {
            lines[index..].iter().for_each(|l| out.push_str(l)); break;
        };
        if matches!(info.split_whitespace().next(), Some("thinkly-raw-block" | "goose-raw-block")) {
            let skip = usize::from(lines.get(index + 1).is_some_and(|l| index + 1 < end && is_marker(content(l))));
            lines[index + 1 + skip..end].iter().for_each(|l| out.push_str(l));
        } else {
            lines[index..=end].iter().for_each(|l| out.push_str(l));
        }
        index = end + 1;
    }
    Cow::Owned(out)
}
