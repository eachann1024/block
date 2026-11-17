use super::super::model::{Block, InlineTag, MarkKind, Span};
use super::html::{self, HtmlValue};
use html5ever::{tendril::StrTendril, tokenizer::{BufferQueue, TagKind, Token, TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts}};
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};
use std::{cell::RefCell, collections::HashMap, rc::Rc};

pub(super) type RichText = (String, Vec<Span>, Vec<InlineTag>);

#[derive(Clone, Debug)]
enum RawTok {
    Text(String),
    Tag { name: String, attrs: Vec<(String, String)>, close: bool, empty: bool, raw: String },
}

#[derive(Clone)]
struct PermissiveSink(Rc<RefCell<Vec<RawTok>>>);
impl TokenSink for PermissiveSink {
    type Handle = ();
    fn process_token(&self, token: Token, _: u64) -> TokenSinkResult<()> {
        match token {
            Token::CharacterTokens(t) => self.0.borrow_mut().push(RawTok::Text(t.to_string())),
            Token::TagToken(tag) => self.0.borrow_mut().push(RawTok::Tag {
                name: tag.name.to_string().to_ascii_lowercase(),
                close: tag.kind == TagKind::EndTag,
                empty: tag.self_closing,
                attrs: tag.attrs.into_iter().map(|a| (a.name.local.to_string(), a.value.to_string())).collect(),
                raw: String::new(),
            }),
            _ => {}
        }
        TokenSinkResult::Continue
    }
}

fn tokenize_permissive(input: &str) -> Vec<RawTok> {
    let mut out = Vec::new();
    let mut at = 0;
    while at < input.len() {
        let start = at;
        let starts_comment = input[at..].starts_with("<!--");
        if starts_comment {
            if let Some(end) = input[at + 4..].find("-->") {
                at += 4 + end + 3;
                continue;
            }
            out.push(RawTok::Text(input[at..].to_owned()));
            break;
        }
        let is_tag = input[at..].starts_with('<')
            && input.as_bytes().get(at + 1).is_some_and(|b| b.is_ascii_alphabetic() || matches!(b, b'/' | b'!' | b'?'));
        if is_tag {
            let mut quote = None;
            at += 1;
            while at < input.len() {
                let byte = input.as_bytes()[at];
                at += 1;
                if let Some(q) = quote {
                    if byte == q { quote = None; }
                } else if matches!(byte, b'\'' | b'"') {
                    quote = Some(byte);
                } else if byte == b'>' {
                    break;
                }
            }
            let slice = &input[start..at];
            if !slice.ends_with('>') || quote.is_some() || slice.starts_with("<!") || slice.starts_with("<?") {
                if !slice.starts_with("<!") && !slice.starts_with("<?") {
                    out.push(RawTok::Text(slice.to_owned()));
                }
                continue;
            }
            let sink = Rc::new(RefCell::new(Vec::new()));
            let tokenizer = Tokenizer::new(PermissiveSink(sink.clone()), TokenizerOpts::default());
            let queue = BufferQueue::default();
            queue.push_back(StrTendril::from(slice));
            let _ = tokenizer.feed(&queue);
            tokenizer.end();
            let mut parsed = sink.borrow_mut().drain(..).collect::<Vec<_>>();
            if parsed.len() == 1 && let RawTok::Tag { raw, .. } = &mut parsed[0] {
                *raw = slice.to_owned();
                out.extend(parsed);
            } else {
                out.push(RawTok::Text(slice.to_owned()));
            }
        } else {
            at += input[at..].chars().next().map_or(1, char::len_utf8);
            while at < input.len() && !input[at..].starts_with('<') {
                at += input[at..].chars().next().map_or(1, char::len_utf8);
            }
            let sink = Rc::new(RefCell::new(Vec::new()));
            let tokenizer = Tokenizer::new(PermissiveSink(sink.clone()), TokenizerOpts::default());
            let queue = BufferQueue::default();
            queue.push_back(StrTendril::from(&input[start..at]));
            let _ = tokenizer.feed(&queue);
            tokenizer.end();
            out.extend(sink.borrow_mut().drain(..));
        }
    }
    out
}

fn is_known_inline_tag(name: &str) -> bool {
    matches!(
        name,
        "a" | "span" | "strong" | "b" | "em" | "i" | "del" | "s" | "strike"
        | "code" | "kbd" | "samp" | "tt" | "var" | "u" | "ins" | "mark" | "font"
        | "sub" | "sup" | "small" | "big" | "q" | "cite" | "abbr" | "dfn"
        | "time" | "ruby" | "rt" | "rp" | "bdi" | "bdo" | "label" | "br" | "wbr"
    )
}

fn is_known_html_element(name: &str) -> bool {
    is_known_inline_tag(name) || matches!(
        name,
        "p" | "div" | "center" | "section" | "article" | "header" | "footer" | "main" | "aside" | "nav"
        | "blockquote" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "details" | "summary"
        | "figure" | "figcaption" | "ul" | "ol" | "li" | "dl" | "dt" | "dd" | "pre"
        | "table" | "thead" | "tbody" | "tfoot" | "tr" | "td" | "th" | "colgroup" | "col"
        | "hr" | "img" | "picture" | "source" | "video" | "audio" | "iframe" | "svg" | "path"
        | "input" | "button" | "select" | "option" | "textarea" | "form"
    )
}

fn mark_index(name: &str) -> Option<usize> {
    match name {
        "strong" | "b" | "summary" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "th" => Some(0),
        "em" | "i" | "cite" | "dfn" | "var" => Some(1),
        "del" | "s" | "strike" => Some(2),
        "code" | "kbd" | "samp" | "tt" => Some(3),
        _ => None,
    }
}

fn is_block_break_tag(name: &str) -> bool {
    matches!(
        name,
        "p" | "div" | "center" | "section" | "article" | "header" | "footer" | "blockquote"
        | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "li" | "dt" | "dd" | "summary" | "tr"
    )
}

#[derive(Default)]
struct Builder {
    text: String,
    spans: Vec<Span>,
    inline: Vec<InlineTag>,
    marks: [u32; 4],
    html: Vec<(String, usize, Vec<(String, String)>, Option<usize>)>,
    links: Vec<(usize, Vec<(String, String)>)>,
    images: Vec<(usize, String, String)>,
}

impl Builder {
    fn append(&mut self, value: &str) {
        if value.is_empty() { return; }
        let start = self.text.len();
        self.text.push_str(value);
        let marks: Vec<_> = [MarkKind::Bold, MarkKind::Italic, MarkKind::Strike, MarkKind::Code]
            .into_iter().filter(|mark| self.marks[*mark as usize] > 0).collect();
        if marks.is_empty() { return; }
        if let Some(last) = self.spans.last_mut() && last.end == start && last.marks == marks {
            last.end = self.text.len();
        } else {
            self.spans.push(Span { start, end: self.text.len(), marks });
        }
    }

    fn push_newline_if_needed(&mut self) {
        if !self.text.is_empty() && !self.text.ends_with('\n') {
            self.append("\n");
        }
    }

    fn strict_html(&mut self, value: HtmlValue) -> Option<()> {
        match value {
            HtmlValue::Text(text) => self.append(&text),
            HtmlValue::Tag { name, attrs, close, empty } => {
                let name = name.to_ascii_lowercase();
                if name == "br" {
                    if !close { self.append("\n"); }
                    return Some(());
                }
                if name == "wbr" { return Some(()); }
                if !is_known_inline_tag(&name) { return None; }
                if empty { return Some(()); }
                let mark = mark_index(&name);
                if close {
                    let (opened, start, attrs, mark) = self.html.pop()?;
                    if opened != name { return None; }
                    if let Some(mark) = mark { self.marks[mark] = self.marks[mark].checked_sub(1)?; }
                    if start < self.text.len() && (mark.is_none() || !attrs.is_empty()) {
                        self.inline.push(InlineTag { start, end: self.text.len(),
                            name: if mark.is_some() { "span".into() } else { name }, attrs });
                    }
                } else {
                    if let Some(mark) = mark { self.marks[mark] += 1; }
                    self.html.push((name, self.text.len(), attrs, mark));
                }
            }
        }
        Some(())
    }

    fn permissive_html(&mut self, tok: RawTok, paired_unknown: &HashMap<String, (usize, usize)>, block_breaks: bool) {
        match tok {
            RawTok::Text(t) => {
                if block_breaks && t.chars().all(char::is_whitespace) && t.contains('\n') {
                    return;
                }
                self.append(&t);
            }
            RawTok::Tag { name, attrs, close, empty, raw } => {
                if name == "br" {
                    if !close { self.append("\n"); }
                    return;
                }
                if name == "wbr" || name == "hr" { return; }
                if name == "img" && !close {
                    let alt = attrs.iter().find(|(k, _)| k == "alt").map(|(_, v)| v.trim()).unwrap_or("");
                    let src = attrs.iter().find(|(k, _)| k == "src").map(|(_, v)| v.trim()).unwrap_or("");
                    let label = if !alt.is_empty() { alt.to_owned() }
                        else if !src.is_empty() { src.rsplit('/').next().unwrap_or("image").to_owned() }
                        else { return; };
                    let start = self.text.len();
                    self.append(&label);
                    if self.links.is_empty() && !src.is_empty() && start < self.text.len() {
                        self.inline.push(InlineTag { start, end: self.text.len(), name: "a".into(), attrs: vec![("href".into(), src.to_owned())] });
                    }
                    return;
                }
                let paired = paired_unknown.get(&name).is_some_and(|(o, c)| *o > 0 && *c > 0);
                if !is_known_html_element(&name) && !paired {
                    // Orphan `<T>` / `<placeholder>` in technical prose stays literal text.
                    self.append(&raw);
                    return;
                }
                if block_breaks && is_block_break_tag(&name) {
                    self.push_newline_if_needed();
                }
                let keep_tag = is_known_inline_tag(&name);
                let mark = mark_index(&name);
                if close {
                    if let Some(pos) = self.html.iter().rposition(|(n, _, _, _)| *n == name) {
                        let (opened, start, attrs, m) = self.html.remove(pos);
                        if let Some(m) = m { self.marks[m] = self.marks[m].saturating_sub(1); }
                        if keep_tag && start < self.text.len() && (m.is_none() || !attrs.is_empty()) {
                            let tag_name = if m.is_some() { "span".into() } else if opened == "ins" { "u".into() } else { opened };
                            self.inline.push(InlineTag { start, end: self.text.len(), name: tag_name, attrs });
                        }
                    }
                } else if !empty {
                    if let Some(m) = mark { self.marks[m] += 1; }
                    self.html.push((name, self.text.len(), attrs, mark));
                }
            }
        }
    }

    fn finish_strict(self) -> Option<RichText> {
        if !self.html.is_empty() || !self.links.is_empty() { return None; }
        Some(self.coalesce())
    }

    fn finish_permissive(mut self) -> RichText {
        for (opened, start, attrs, m) in std::mem::take(&mut self.html) {
            if is_known_inline_tag(&opened) && start < self.text.len() && (m.is_none() || !attrs.is_empty()) {
                let tag_name = if m.is_some() { "span".into() } else if opened == "ins" { "u".into() } else { opened };
                self.inline.push(InlineTag { start, end: self.text.len(), name: tag_name, attrs });
            }
        }
        for (start, attrs) in std::mem::take(&mut self.links) {
            if start < self.text.len() {
                self.inline.push(InlineTag { start, end: self.text.len(), name: "a".into(), attrs });
            }
        }
        self.coalesce()
    }

    fn coalesce(mut self) -> RichText {
        self.inline.sort_by_key(|tag| (tag.start, std::cmp::Reverse(tag.end)));
        let mut merged: Vec<InlineTag> = Vec::new();
        for tag in self.inline {
            if tag.start >= tag.end || tag.end > self.text.len() { continue; }
            if let Some(prev) = merged.iter_mut().rev().find(|p| p.end == tag.start && p.name == tag.name && p.attrs == tag.attrs) {
                prev.end = tag.end;
            } else {
                merged.push(tag);
            }
        }
        (self.text, self.spans, merged)
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

pub(super) fn parse(input: &str) -> RichText {
    parse_with_defs(input, "")
}

pub(super) fn parse_with_defs(input: &str, defs: &str) -> RichText {
    if defs.is_empty() && !input.bytes().any(|b| b.is_ascii_punctuation()) {
        return (input.to_owned(), Vec::new(), Vec::new());
    }
    let normalized = normalize_cjk_strong(input);
    // Sentinel `x ` keeps inline fragments from re-triggering block-level markers;
    // any trailing `defs` supplies CommonMark reference-link definitions without emitting events.
    let source = if defs.is_empty() { format!("x {normalized}") } else { format!("x {normalized}\n\n{defs}") };
    let options = Options::ENABLE_STRIKETHROUGH | Options::ENABLE_FOOTNOTES;
    let (text, spans, tags) = parse_events(Parser::new_ext(&source, options));
    if let Some(rest) = text.strip_prefix("x ") {
        let shift = 2usize;
        let spans = spans.into_iter().filter_map(|s| {
            let start = s.start.saturating_sub(shift);
            let end = s.end.saturating_sub(shift).min(rest.len());
            (start < end).then_some(Span { start, end, marks: s.marks })
        }).collect();
        let tags = tags.into_iter().filter_map(|t| {
            let start = t.start.saturating_sub(shift);
            let end = t.end.saturating_sub(shift).min(rest.len());
            (start < end).then_some(InlineTag { start, end, ..t })
        }).collect();
        (rest.to_owned(), spans, tags)
    } else {
        (input.to_owned(), Vec::new(), Vec::new())
    }
}

/// Total inline event projection: never fails on any CommonMark/HTML input.
pub(super) fn parse_events<'a>(events: impl IntoIterator<Item = Event<'a>>) -> RichText {
    let collected: Vec<Event<'a>> = events.into_iter().collect();
    let mut paired_unknown: HashMap<String, (usize, usize)> = HashMap::new();
    let mut html_batches: Vec<Vec<RawTok>> = Vec::new();
    for event in &collected {
        if let Event::InlineHtml(val) | Event::Html(val) = event {
            let toks = tokenize_permissive(val);
            for tok in &toks {
                if let RawTok::Tag { name, close, empty: false, .. } = tok && !is_known_html_element(name) {
                    let entry = paired_unknown.entry(name.clone()).or_default();
                    if *close { entry.1 += 1; } else { entry.0 += 1; }
                }
            }
            html_batches.push(toks);
        }
    }
    let mut html_iter = html_batches.into_iter();
    let mut b = Builder::default();
    let mut skip_footnote_def = 0usize;
    for event in collected {
        match event {
            Event::Start(Tag::FootnoteDefinition(_)) => { skip_footnote_def += 1; }
            Event::End(TagEnd::FootnoteDefinition) => { skip_footnote_def = skip_footnote_def.saturating_sub(1); }
            _ if skip_footnote_def > 0 => {}
            Event::Start(Tag::Paragraph | Tag::Heading { .. } | Tag::BlockQuote(_) | Tag::List(_) | Tag::Item) => {}
            Event::End(TagEnd::Paragraph | TagEnd::Heading(_) | TagEnd::BlockQuote(_) | TagEnd::List(_) | TagEnd::Item) => {}
            Event::Start(Tag::Strong) => b.marks[0] += 1,
            Event::End(TagEnd::Strong) => b.marks[0] = b.marks[0].saturating_sub(1),
            Event::Start(Tag::Emphasis) => b.marks[1] += 1,
            Event::End(TagEnd::Emphasis) => b.marks[1] = b.marks[1].saturating_sub(1),
            Event::Start(Tag::Strikethrough) => b.marks[2] += 1,
            Event::End(TagEnd::Strikethrough) => b.marks[2] = b.marks[2].saturating_sub(1),
            Event::Start(Tag::Link { dest_url, title, .. }) => {
                let mut attrs = vec![("href".into(), dest_url.to_string())];
                if !title.is_empty() { attrs.push(("title".into(), title.to_string())); }
                b.links.push((b.text.len(), attrs));
            }
            Event::End(TagEnd::Link) => {
                if let Some((start, attrs)) = b.links.pop() && start < b.text.len() {
                    b.inline.push(InlineTag { start, end: b.text.len(), name: "a".into(), attrs });
                }
            }
            Event::Start(Tag::Image { dest_url, title, .. }) => {
                b.images.push((b.text.len(), dest_url.to_string(), title.to_string()));
            }
            Event::End(TagEnd::Image) => {
                if let Some((start, dest, title)) = b.images.pop() {
                    if start == b.text.len() {
                        let fallback = dest.rsplit('/').next().filter(|s| !s.is_empty()).unwrap_or("image");
                        b.append(fallback);
                    }
                    if b.links.is_empty() && !dest.is_empty() && start < b.text.len() {
                        let mut attrs = vec![("href".into(), dest)];
                        if !title.is_empty() { attrs.push(("title".into(), title)); }
                        b.inline.push(InlineTag { start, end: b.text.len(), name: "a".into(), attrs });
                    }
                }
            }
            Event::Text(value) => b.append(&value),
            Event::Code(value) => { b.marks[3] += 1; b.append(&value); b.marks[3] = b.marks[3].saturating_sub(1); }
            Event::InlineMath(value) | Event::DisplayMath(value) => {
                b.marks[3] += 1; b.append(&value); b.marks[3] = b.marks[3].saturating_sub(1);
            }
            Event::FootnoteReference(label) => b.append(&format!("[^{label}]")),
            Event::InlineHtml(_) | Event::Html(_) => {
                if let Some(toks) = html_iter.next() {
                    for tok in toks { b.permissive_html(tok, &paired_unknown, false); }
                }
            }
            Event::SoftBreak | Event::HardBreak => b.append("\n"),
            Event::TaskListMarker(_) | Event::Rule => {}
            Event::Start(Tag::CodeBlock(_) | Tag::HtmlBlock | Tag::Table(_) | Tag::TableHead | Tag::TableRow | Tag::TableCell | Tag::MetadataBlock(_) | Tag::DefinitionList | Tag::DefinitionListTitle | Tag::DefinitionListDefinition | Tag::Superscript | Tag::Subscript) => {}
            Event::End(_) => {}
        }
    }
    b.finish_permissive()
}

/// Strict HTML cell/wrapper parser; returns `None` when lossless tag round-trip is impossible.
pub(super) fn parse_html(input: &str) -> Option<RichText> {
    let mut b = Builder::default();
    for token in html::tokens(input)? { b.strict_html(token.value)?; }
    b.finish_strict()
}

/// Permissive HTML fragment extractor: strips unknown wrappers, preserves marks/links/line breaks.
pub(super) fn parse_html_permissive(input: &str, block_breaks: bool) -> RichText {
    let toks = tokenize_permissive(input);
    let mut paired_unknown: HashMap<String, (usize, usize)> = HashMap::new();
    for tok in &toks {
        if let RawTok::Tag { name, close, empty: false, .. } = tok && !is_known_html_element(name) {
            let entry = paired_unknown.entry(name.clone()).or_default();
            if *close { entry.1 += 1; } else { entry.0 += 1; }
        }
    }
    let mut b = Builder::default();
    for tok in toks { b.permissive_html(tok, &paired_unknown, block_breaks); }
    let (text, spans, tags) = b.finish_permissive();
    if !block_breaks { return (text, spans, tags); }
    let lead = text.len() - text.trim_start_matches('\n').len();
    let trimmed_end = text.trim_end_matches('\n').len();
    if lead == 0 && trimmed_end == text.len() { return (text, spans, tags); }
    let mut block = Block::new(0, super::super::model::Kind::Paragraph, text);
    block.spans = spans; block.meta.inline = tags;
    let sliced = block.slice(lead..trimmed_end.max(lead));
    (sliced.text, sliced.spans, sliced.meta.inline)
}

/// Markdown and HTML have separate serialization rules.
pub(super) fn serialize(block: &Block, out: &mut String) {
    super::write::markdown(block, out);
}
pub(super) fn serialize_html(block: &Block, out: &mut String) {
    super::write::html(block, out);
}
