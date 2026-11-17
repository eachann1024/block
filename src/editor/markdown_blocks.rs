use std::collections::HashSet;
use pulldown_cmark::{Event, LinkType, Options, Parser, Tag};
use super::super::model::{Block, Kind};

pub(super) struct ListMarker<'a> {
    pub kind: Kind,
    pub checked: bool,
    pub indent: usize,
    pub marker_width: usize,
    pub start: Option<u64>,
    pub body: &'a str,
}

pub(super) fn leading_columns(line: &str) -> (usize, &str) {
    let body = line.trim_start_matches([' ', '\t']);
    let indent = line[..line.len() - body.len()].chars().map(|c| if c == '\t' { 4 } else { 1 }).sum();
    (indent, body)
}

pub(super) fn fence(line: &str) -> Option<(u8, usize, &str)> {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 { return None; }
    fence_body(body)
}

pub(super) fn fence_any_indent(line: &str) -> Option<(usize, usize, u8, usize, &str)> {
    let (indent_cols, body) = leading_columns(line);
    let indent_bytes = line.len() - body.len();
    let (marker, width, info) = fence_body(body)?;
    Some((indent_cols, indent_bytes, marker, width, info))
}

fn fence_body(body: &str) -> Option<(u8, usize, &str)> {
    if body.trim() == "$$" { return Some((b'$', 2, "math")); }
    let marker = *body.as_bytes().first()?;
    if marker != b'`' && marker != b'~' { return None; }
    let width = body.bytes().take_while(|b| *b == marker).count();
    if width < 3 { return None; }
    let info = body[width..].trim();
    if marker == b'`' && info.contains('`') { return None; }
    Some((marker, width, info))
}

pub(super) fn closes_fence(line: &str, marker: u8, width: usize) -> bool {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 { return false; }
    closes_fence_body(body, marker, width)
}

pub(super) fn closes_fence_any_indent(line: &str, marker: u8, width: usize) -> bool {
    let (_, body) = leading_columns(line);
    closes_fence_body(body, marker, width)
}

fn closes_fence_body(body: &str, marker: u8, width: usize) -> bool {
    if marker == b'$' { return body.trim() == "$$"; }
    body.bytes().take_while(|b| *b == marker).count() >= width
        && body.trim_start_matches(marker as char).trim().is_empty()
}

pub(super) fn inline_math_block(line: &str) -> Option<(usize, &str)> {
    let (indent, body) = leading_columns(line);
    let trimmed = body.trim_end();
    if trimmed.starts_with("$$") && trimmed.ends_with("$$") && trimmed.len() > 4 {
        let inner = trimmed[2..trimmed.len() - 2].trim();
        if !inner.contains("$$") { return Some((indent, inner)); }
    }
    None
}

pub(super) fn is_html_hr(body: &str) -> bool {
    let b = body.trim();
    b.eq_ignore_ascii_case("<hr>") || b.eq_ignore_ascii_case("<hr/>") || b.eq_ignore_ascii_case("<hr />")
}

pub(super) fn is_thematic_break(line: &str) -> bool {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 { return false; }
    if is_html_hr(body) { return true; }
    let compact: String = body.chars().filter(|c| !matches!(c, ' ' | '\t' | '\r' | '\n')).collect();
    compact.len() >= 3 && ['-', '*', '_'].iter().any(|c| compact.chars().all(|v| v == *c))
}

pub(super) fn is_empty_block_comment(line: &str) -> bool {
    matches!(line.trim(), "<!-- thinkly:empty-block=paragraph -->" | "<!-- goose-note:empty-block=paragraph -->")
}

pub(super) fn classify_list_marker(line: &str) -> Option<ListMarker<'_>> {
    if is_thematic_break(line) { return None; }
    let (indent, body) = leading_columns(line);
    for (prefix, checked) in [
        ("- [ ] ", false), ("- [x] ", true), ("- [X] ", true),
        ("+ [ ] ", false), ("+ [x] ", true), ("+ [X] ", true),
        ("* [ ] ", false), ("* [x] ", true), ("* [X] ", true),
    ] {
        if let Some(rest) = body.strip_prefix(prefix) {
            return Some(ListMarker { kind: Kind::Todo, checked, indent, marker_width: 2, start: None, body: rest });
        }
    }
    for (exact, checked) in [("- [ ]", false), ("- [x]", true), ("- [X]", true)] {
        if body.trim_end() == exact {
            return Some(ListMarker { kind: Kind::Todo, checked, indent, marker_width: 2, start: None, body: "" });
        }
    }
    for prefix in ["- ", "+ ", "* ", "-\t", "+\t", "*\t"] {
        if let Some(rest) = body.strip_prefix(prefix) {
            return Some(ListMarker { kind: Kind::Bullet, checked: false, indent, marker_width: 2, start: None, body: rest });
        }
    }
    if matches!(body, "-" | "+" | "*") {
        return Some(ListMarker { kind: Kind::Bullet, checked: false, indent, marker_width: 2, start: None, body: "" });
    }
    let digits = body.bytes().take_while(u8::is_ascii_digit).count();
    if (1..=9).contains(&digits) {
        let rest = &body[digits..];
        if let Some(after) = rest.strip_prefix(". ").or_else(|| rest.strip_prefix(") "))
            .or_else(|| rest.strip_prefix(".\t")).or_else(|| rest.strip_prefix(")\t"))
        {
            if let Ok(start) = body[..digits].parse::<u64>() {
                return Some(ListMarker { kind: Kind::Numbered, checked: false, indent, marker_width: digits + 2, start: Some(start), body: after });
            }
        } else if matches!(rest, "." | ")") && let Ok(start) = body[..digits].parse::<u64>() {
            return Some(ListMarker { kind: Kind::Numbered, checked: false, indent, marker_width: digits + 2, start: Some(start), body: "" });
        }
    }
    None
}

pub(super) fn classify_heading(line: &str) -> Option<(Kind, Option<u8>, &str)> {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 { return None; }
    let hashes = body.bytes().take_while(|&b| b == b'#').count();
    if !(1..=6).contains(&hashes) { return None; }
    let after = &body[hashes..];
    if !after.is_empty() && !after.starts_with([' ', '\t']) { return None; }
    let mut text = after.trim_start_matches([' ', '\t']);
    if text.ends_with('#') {
        let stripped = text.trim_end_matches('#');
        if stripped.is_empty() || stripped.ends_with(char::is_whitespace) {
            text = stripped.trim_end();
        }
    }
    let (kind, level) = match hashes {
        1 => (Kind::H1, None),
        2 => (Kind::H2, None),
        3 => (Kind::H3, None),
        n => (Kind::H3, Some(n as u8)),
    };
    Some((kind, level, text))
}

fn norm_ref_label(label: &str) -> String {
    label.split_whitespace().collect::<Vec<_>>().join(" ").to_lowercase()
}

/// Collects only link reference definitions that are actually referenced in `source`,
/// so accidental `[NOTE]: text` lines without reference links stay ordinary paragraphs.
pub(super) fn collect_ref_defs(source: &str) -> (String, HashSet<String>) {
    if !source.contains("]:") { return (String::new(), HashSet::new()); }
    let parser = Parser::new_ext(source, Options::ENABLE_STRIKETHROUGH);
    if parser.reference_definitions().iter().next().is_none() {
        return (String::new(), HashSet::new());
    }
    let mut defs_map = Vec::new();
    for (label, def) in parser.reference_definitions().iter() {
        defs_map.push((norm_ref_label(label), def.dest.to_string(), def.title.as_deref().unwrap_or("").to_owned()));
    }
    let mut used = HashSet::new();
    for event in parser {
        match event {
            Event::Start(Tag::Link { link_type, id, .. } | Tag::Image { link_type, id, .. })
                if matches!(link_type, LinkType::Reference | LinkType::Collapsed | LinkType::Shortcut) =>
            {
                used.insert(norm_ref_label(&id));
            }
            _ => {}
        }
    }
    if used.is_empty() { return (String::new(), used); }
    let mut suffix = String::new();
    for (label, dest, title) in defs_map {
        if !used.contains(&label) { continue; }
        let safe_dest = dest.replace('<', "%3C").replace('>', "%3E");
        suffix.push_str(&format!("[{label}]: <{safe_dest}>"));
        if !title.is_empty() {
            suffix.push_str(&format!(" \"{}\"", title.replace('"', "\\\"")));
        }
        suffix.push('\n');
    }
    (suffix, used)
}

pub(super) fn consumed_ref_def_lines(lines: &[&str], used: &HashSet<String>) -> Option<usize> {
    if used.is_empty() { return None; }
    let first = lines.first()?.trim_start_matches(' ');
    if lines[0].len() - first.len() > 3 || !first.starts_with('[') || first.starts_with("[^") {
        return None;
    }
    for count in 1..=lines.len().min(4) {
        let chunk = lines[..count].concat();
        let parser = Parser::new(&chunk);
        let labels: Vec<String> = parser.reference_definitions().iter().map(|(k, _)| norm_ref_label(k)).collect();
        if labels.is_empty() || !labels.iter().all(|l| used.contains(l)) { continue; }
        if parser.into_iter().next().is_none() {
            return Some(count);
        }
    }
    None
}

// ---- YAML frontmatter: a leading `---` ... `---` pair is shown as a `yaml-frontmatter` code block. ----
fn is_frontmatter_open(line: &str) -> bool {
    let line = line.trim_end_matches(['\r', '\n']);
    line.strip_prefix('\u{feff}').unwrap_or(line).trim_end_matches([' ', '\t']) == "---"
}
fn is_frontmatter_close(line: &str) -> bool {
    line.trim_end_matches(['\r', '\n']).trim_end_matches([' ', '\t']) == "---"
}
fn after_key(rest: &str) -> bool {
    rest.strip_prefix(':').is_some_and(|r| r.is_empty() || r.starts_with([' ', '\t']))
}
/// `key:` where the colon ends the line or is followed by a space/tab; the key is quoted or plain text.
fn has_yaml_key(trimmed: &str) -> bool {
    if let Some(quote) = trimmed.chars().next().filter(|c| matches!(c, '"' | '\'')) {
        let mut escaped = false;
        for (i, ch) in trimmed.char_indices().skip(1) {
            if quote == '"' && ch == '\\' && !escaped { escaped = true; continue; }
            if ch == quote && !escaped { return after_key(trimmed[i + 1..].trim_start_matches([' ', '\t'])); }
            escaped = false;
        }
        return false;
    }
    let digits = trimmed.bytes().take_while(u8::is_ascii_digit).count();
    if trimmed.starts_with(['>', '`', '|']) || (digits > 0 && trimmed[digits..].starts_with(['.', ')'])) { return false; }
    trimmed.char_indices().any(|(i, ch)| ch == ':' && i > 0 && after_key(&trimmed[i..])
        && !trimmed[..i].chars().any(char::is_control))
}
/// Tells real frontmatter from two horizontal rules wrapped around ordinary Markdown.
fn looks_like_frontmatter(inner: &[&str]) -> bool {
    if inner.iter().all(|l| l.trim().is_empty()) { return true; }
    let (mut has_blank, mut has_yaml_construct) = (false, false);
    for raw in inner {
        let line = raw.trim_end_matches(['\r', '\n']);
        if line.trim().is_empty() { has_blank = true; continue; }
        if fence(line).is_some() { return false; }
        let trimmed = line.trim_start_matches([' ', '\t']);
        if trimmed.starts_with('#') || line.len() > trimmed.len() { continue; }
        let construct = trimmed == "-" || trimmed.starts_with("- ") || trimmed.starts_with("-\t")
            || trimmed.starts_with(['%', '{', '}', '[', ']', '&', '*', '?']) || has_yaml_key(trimmed);
        if !construct { return false; }
        has_yaml_construct = true;
    }
    has_yaml_construct || !has_blank
}
pub(super) fn parse_frontmatter(id: u64, lines: &[&str]) -> Option<(Block, usize, usize)> {
    if lines.is_empty() || !is_frontmatter_open(lines[0]) { return None; }
    let end = (1..lines.len()).find(|&i| is_frontmatter_close(lines[i]))?;
    if !looks_like_frontmatter(&lines[1..end]) { return None; }
    let raw_all = lines[..=end].concat();
    let mut body = lines[1..end].concat();
    let empty_line = body == "\n" || body == "\r\n";
    if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
    let mut block = Block::new(id, Kind::Paragraph, body);
    block.meta.code_empty_line = empty_line;
    block.meta.code_language = Some("yaml-frontmatter".to_owned());
    let bytes = raw_all.len();
    block.seal_clean_source(raw_all, None);
    Some((block, end + 1, bytes))
}
/// False when the block must stay a fenced code block so the document reloads unchanged.
pub(super) fn serialize_frontmatter(block: &Block, at_top: bool, out: &mut String) -> bool {
    if !(at_top && block.meta.indent == 0
        && block.meta.code_language.as_deref() == Some("yaml-frontmatter")) { return false; }
    let inner: Vec<&str> = block.text.split('\n').collect();
    if inner.iter().any(|l| is_frontmatter_close(l)) || !looks_like_frontmatter(&inner) { return false; }
    out.push_str("---\n");
    if !block.text.is_empty() { out.push_str(&block.text); out.push('\n'); }
    else if block.meta.code_empty_line { out.push('\n'); }
    out.push_str("---\n");
    true
}
pub(super) fn strip_frontmatter(source: &str) -> &str {
    let lines: Vec<_> = source.split_inclusive('\n').collect();
    match parse_frontmatter(1, &lines) {
        Some((_, _, bytes)) => source[bytes..].trim_start_matches(['\r', '\n']),
        None => source,
    }
}
