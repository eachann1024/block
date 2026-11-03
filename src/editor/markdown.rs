use super::model::{Block, Doc, Kind, MarkKind, Span};

pub fn parse(source: &str) -> Doc {
    let mut doc = Doc { blocks: Vec::new(), next_id: 1 };
    let mut numbered = 0u32;
    for raw in source.split_inclusive('\n') {
        let line = raw.trim_end_matches(['\n', '\r']);
        if line.is_empty() {
            numbered = 0;
            doc.push(Kind::Paragraph, String::new());
            continue;
        }
        let (kind, body, checked) = classify(line, &mut numbered);
        let (text, spans) = inline(body);
        let id = doc.push(kind, text);
        let block = doc.blocks.iter_mut().find(|b| b.id == id).unwrap();
        block.checked = checked;
        block.spans = spans;
    }
    if doc.blocks.is_empty() {
        doc.push(Kind::Paragraph, String::new());
    }
    doc
}

fn classify<'a>(line: &'a str, numbered: &mut u32) -> (Kind, &'a str, bool) {
    if let Some(body) = line.strip_prefix("# ") {
        *numbered = 0;
        return (Kind::H1, body, false);
    }
    if let Some(body) = line.strip_prefix("## ") {
        *numbered = 0;
        return (Kind::H2, body, false);
    }
    if let Some(body) = line.strip_prefix("### ") {
        *numbered = 0;
        return (Kind::H3, body, false);
    }
    if let Some(body) = line.strip_prefix("> ") {
        *numbered = 0;
        return (Kind::Quote, body, false);
    }
    if let Some(body) = line.strip_prefix("- [ ] ").or_else(|| line.strip_prefix("- [x] ")) {
        *numbered = 0;
        return (Kind::Todo, body, line.starts_with("- [x]"));
    }
    if let Some(body) = line.strip_prefix("- ") {
        *numbered = 0;
        return (Kind::Bullet, body, false);
    }
    if let Some(rest) = line.strip_prefix(|c: char| c.is_ascii_digit())
        && let Some(body) = rest.strip_prefix(". ")
    {
        *numbered += 1;
        return (Kind::Numbered, body, false);
    }
    *numbered = 0;
    (Kind::Paragraph, line, false)
}

/// Splits `**bold**`, `*italic*`, `~~strike~~` and `` `code` `` out of a line.
fn inline(input: &str) -> (String, Vec<Span>) {
    let bytes = input.as_bytes();
    let mut text = String::new();
    let mut spans = Vec::new();
    let mut i = 0;
    while i < bytes.len() {
        let (mark, token) = if bytes[i..].starts_with(b"~~") {
            (Some(MarkKind::Strike), 2)
        } else if bytes[i..].starts_with(b"**") {
            (Some(MarkKind::Bold), 2)
        } else if bytes[i] == b'*' {
            (Some(MarkKind::Italic), 1)
        } else if bytes[i] == b'`' {
            (Some(MarkKind::Code), 1)
        } else {
            (None, 0)
        };
        let Some(mark) = mark else {
            let end = next_char_end(bytes, i);
            text.push_str(&input[i..end]);
            i = end;
            continue;
        };
        let content_start = i + token;
        let Some(close) = find_close(bytes, content_start, token) else {
            text.push_str(&input[i..i + token]);
            i += token;
            continue;
        };
        let start = text.len();
        text.push_str(&input[content_start..close]);
        spans.push(Span { start, end: text.len(), marks: vec![mark] });
        i = close + token;
    }
    (text, spans)
}

fn next_char_end(bytes: &[u8], i: usize) -> usize {
    let mut end = i + 1;
    while end < bytes.len() && bytes[end] & 0b1100_0000 == 0x80 {
        end += 1;
    }
    end
}

fn find_close(bytes: &[u8], from: usize, token: usize) -> Option<usize> {
    let mut j = from;
    while j + token <= bytes.len() {
        if bytes[j..].starts_with(&bytes[from - token..from]) && j > from {
            return Some(j);
        }
        j += 1;
    }
    None
}

pub fn serialize(doc: &Doc) -> String {
    let mut out = String::new();
    let mut number = 0u32;
    for block in &doc.blocks {
        if block.kind == Kind::Numbered {
            number += 1;
        } else {
            number = 0;
        }
        out.push_str(&prefix(block, number));
        out.push_str(&inline_text(block));
        out.push('\n');
    }
    out
}

fn prefix(block: &Block, number: u32) -> String {
    match block.kind {
        Kind::Paragraph => String::new(),
        Kind::H1 => "# ".into(),
        Kind::H2 => "## ".into(),
        Kind::H3 => "### ".into(),
        Kind::Bullet => "- ".into(),
        Kind::Numbered => format!("{number}. "),
        Kind::Quote => "> ".into(),
        Kind::Todo => if block.checked { "- [x] " } else { "- [ ] " }.into(),
    }
}

fn inline_text(block: &Block) -> String {
    let mut out = String::new();
    let mut cursor = 0;
    let mut spans = block.spans.clone();
    spans.sort_by_key(|span| span.start);
    for span in spans {
        if span.start > cursor {
            out.push_str(&block.text[cursor..span.start.min(block.text.len())]);
        }
        let end = span.end.min(block.text.len());
        let start = span.start.min(end);
        let (open, close) = tokens(&span.marks);
        out.push_str(open);
        out.push_str(&block.text[start..end]);
        out.push_str(close);
        cursor = end;
    }
    if cursor < block.text.len() {
        out.push_str(&block.text[cursor..]);
    }
    out
}

fn tokens(marks: &[MarkKind]) -> (&'static str, &'static str) {
    if marks.contains(&MarkKind::Code) {
        ("`", "`")
    } else if marks.contains(&MarkKind::Strike) {
        ("~~", "~~")
    } else if marks.contains(&MarkKind::Bold) {
        ("**", "**")
    } else if marks.contains(&MarkKind::Italic) {
        ("*", "*")
    } else {
        ("", "")
    }
}
