use super::super::model::{Block, InlineTag, MarkKind, Span};
use super::html::{self, HtmlValue};
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};

type RichText = (String, Vec<Span>, Vec<InlineTag>);
#[derive(Default)]
struct Builder {
    text: String, spans: Vec<Span>, inline: Vec<InlineTag>, marks: [u32;4],
    html: Vec<(String, usize, Vec<(String, String)>, Option<usize>)>,
    links: Vec<(usize, Vec<(String, String)>)>,
}
impl Builder {
    fn append(&mut self, value: &str) {
        let start = self.text.len(); self.text.push_str(value);
        if value.is_empty() { return; }
        let marks: Vec<_> = [MarkKind::Bold,MarkKind::Italic,MarkKind::Strike,MarkKind::Code]
            .into_iter().filter(|mark| self.marks[*mark as usize] > 0).collect();
        if marks.is_empty() { return; }
        if let Some(last) = self.spans.last_mut() && last.end == start && last.marks == marks {
            last.end = self.text.len();
        } else { self.spans.push(Span { start, end:self.text.len(), marks }); }
    }
    fn html(&mut self, value: HtmlValue) -> Option<()> {
        match value {
            HtmlValue::Text(text) => self.append(&text),
            HtmlValue::Tag { name, attrs, close, empty } => {
                let name = name.to_ascii_lowercase();
                if name == "br" {
                    if !close { self.append("\n"); }
                    return Some(());
                }
                if name == "wbr" { return Some(()); }
                if !matches!(
                    name.as_str(),
                    "a" | "span" | "strong" | "b" | "em" | "i" | "del" | "s" | "strike"
                    | "code" | "kbd" | "samp" | "tt" | "var" | "u" | "ins" | "mark" | "font"
                    | "sub" | "sup" | "small" | "big" | "q" | "cite" | "abbr" | "dfn"
                    | "time" | "ruby" | "rt" | "rp" | "bdi" | "bdo" | "label"
                ) {
                    return None;
                }
                if empty { return Some(()); }
                let mark = match name.as_str() {
                    "strong" | "b" => Some(0),
                    "em" | "i" | "cite" | "dfn" | "var" => Some(1),
                    "del" | "s" | "strike" => Some(2),
                    "code" | "kbd" | "samp" | "tt" => Some(3),
                    _ => None,
                };
                if close {
                    let (opened, start, attrs, mark) = self.html.pop()?;
                    if opened != name { return None; }
                    if let Some(mark) = mark { self.marks[mark] = self.marks[mark].checked_sub(1)?; }
                    if start < self.text.len() && (mark.is_none() || !attrs.is_empty()) {
                        self.inline.push(InlineTag { start, end:self.text.len(),
                            name:if mark.is_some() { "span".into() } else { name }, attrs });
                    }
                } else {
                    if let Some(mark) = mark { self.marks[mark] += 1; }
                    self.html.push((name, self.text.len(), attrs, mark));
                }
            }
        }
        Some(())
    }
    fn finish(mut self) -> Option<RichText> {
        if !self.html.is_empty() || !self.links.is_empty() { return None; }
        self.inline.sort_by_key(|tag| (tag.start, std::cmp::Reverse(tag.end)));
        // Wrappers are re-emitted per segment, so adjacent equal tags are one logical range.
        let mut merged: Vec<InlineTag> = Vec::new();
        for tag in self.inline {
            if let Some(prev) = merged.iter_mut().rev().find(|p| p.end == tag.start && p.name == tag.name && p.attrs == tag.attrs) {
                prev.end = tag.end;
            } else { merged.push(tag); }
        }
        Some((self.text, self.spans, merged))
    }
}

/// CommonMark does not treat `**` after fullwidth CJK punctuation (`**11/11：**汕头`) as
/// right-flanking when followed by non-punctuation text. Normalize such pairs outside backticks.
fn normalize_cjk_strong(input: &str) -> std::borrow::Cow<'_, str> {
    if !input.contains("**") { return std::borrow::Cow::Borrowed(input); }
    let bytes = input.as_bytes();
    let mut out = String::with_capacity(input.len() + 16);
    let (mut i, mut changed) = (0, false);
    while i < bytes.len() {
        if bytes[i] == b'`' {
            let ticks = bytes[i..].iter().take_while(|&&b| b == b'`').count();
            out.push_str(&input[i..i + ticks]); i += ticks;
            while i < bytes.len() {
                if bytes[i] == b'`' {
                    let run = bytes[i..].iter().take_while(|&&b| b == b'`').count();
                    out.push_str(&input[i..i + run]); i += run;
                    if run == ticks { break; }
                } else {
                    let ch = input[i..].chars().next().unwrap();
                    out.push(ch); i += ch.len_utf8();
                }
            }
            continue;
        }
        if i + 4 <= bytes.len() && &bytes[i..i + 2] == b"**" && bytes.get(i + 2) != Some(&b'*') {
            let inner_start = i + 2;
            if let Some(rel_end) = input[inner_start..].find("**") {
                let inner = &input[inner_start..inner_start + rel_end];
                let valid = !inner.is_empty() && !inner.starts_with(char::is_whitespace)
                    && !inner.ends_with(char::is_whitespace) && !inner.contains('`') && !inner.contains('\n');
                let cjk_punct = inner.chars().last().is_some_and(|ch| matches!(
                    ch, '：' | '）' | '”' | '’' | '》' | '】' | '！' | '？' | '，' | '。' | '、' | '；' | '—' | '…'
                ));
                if valid && cjk_punct {
                    out.push_str("<strong>"); out.push_str(inner); out.push_str("</strong>");
                    i = inner_start + rel_end + 2; changed = true; continue;
                }
            }
        }
        let ch = input[i..].chars().next().unwrap();
        out.push(ch); i += ch.len_utf8();
    }
    if changed { std::borrow::Cow::Owned(out) } else { std::borrow::Cow::Borrowed(input) }
}

pub(super) fn parse(input: &str) -> Option<RichText> {
    if !input.bytes().any(|b| b.is_ascii_punctuation()) {
        return Some((input.to_owned(), Vec::new(), Vec::new()));
    }
    let input = normalize_cjk_strong(input);
    // The sentinel prevents an inline fragment from being interpreted as a block.
    let source = format!("x {input}");
    let (text, spans, tags) = parse_events(Parser::new_ext(&source, Options::ENABLE_STRIKETHROUGH))?;
    let text = text.strip_prefix("x ")?.to_owned();
    Some((text, spans.into_iter().map(|span| Span { start:span.start - 2, end:span.end - 2, marks:span.marks }).collect(),
        tags.into_iter().map(|tag| InlineTag { start:tag.start - 2, end:tag.end - 2, ..tag }).collect()))
}

/// Consume the parser's events directly, including GFM's table-specific escaping.
pub(super) fn parse_events<'a>(events: impl IntoIterator<Item = Event<'a>>) -> Option<RichText> {
    let mut b = Builder::default();
    for event in events {
        match event {
            Event::Start(Tag::Paragraph) | Event::End(TagEnd::Paragraph) => {},
            Event::Start(Tag::Strong) => b.marks[0] += 1,
            Event::End(TagEnd::Strong) => b.marks[0] = b.marks[0].checked_sub(1)?,
            Event::Start(Tag::Emphasis) => b.marks[1] += 1,
            Event::End(TagEnd::Emphasis) => b.marks[1] = b.marks[1].checked_sub(1)?,
            Event::Start(Tag::Strikethrough) => b.marks[2] += 1,
            Event::End(TagEnd::Strikethrough) => b.marks[2] = b.marks[2].checked_sub(1)?,
            Event::Start(Tag::Link { dest_url, title, .. }) => {
                let mut attrs = vec![("href".into(), dest_url.to_string())];
                if !title.is_empty() { attrs.push(("title".into(), title.to_string())); }
                b.links.push((b.text.len(), attrs));
            },
            Event::End(TagEnd::Link) => {
                let (start, attrs) = b.links.pop()?;
                if start < b.text.len() { b.inline.push(InlineTag {
                    start, end:b.text.len(), name:"a".into(), attrs }); }
            },
            Event::Text(value) => b.append(&value),
            Event::Code(value) => { b.marks[3] += 1; b.append(&value); b.marks[3] -= 1; },
            Event::InlineHtml(value) => for token in html::tokens(&value)? { b.html(token.value)?; },
            Event::SoftBreak | Event::HardBreak => b.append("\n"),
            _ => return None,
        }
    }
    b.finish()
}
/// HTML cell text must not accidentally interpret literal Markdown punctuation.
pub(super) fn parse_html(input: &str) -> Option<RichText> {
    let mut b = Builder::default();
    for token in html::tokens(input)? { b.html(token.value)?; }
    b.finish()
}

/// Markdown and HTML have separate serialization rules.
pub(super) fn serialize(block: &Block, out: &mut String) {
    super::write::markdown(block, out);
}
pub(super) fn serialize_html(block: &Block, out: &mut String) {
    super::write::html(block, out);
}
