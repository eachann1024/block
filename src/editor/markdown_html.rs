//! HTML5 tokenization supplies entity decoding and attribute handling. Source ranges
//! are kept separately so editing a cell never rewrites table scaffolding or styles.
use html5ever::{tendril::StrTendril, tokenizer::{BufferQueue, TagKind, Token,
    TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts}};
use std::{cell::RefCell, ops::Range, rc::Rc};

#[derive(Clone, Debug)]
pub(super) enum HtmlValue {
    Text(String),
    Tag { name: String, attrs: Vec<(String, String)>, close: bool, empty: bool },
}
#[derive(Clone, Debug)]
pub(super) struct HtmlToken { pub range: Range<usize>, pub value: HtmlValue }
#[derive(Clone)]
struct Sink(Rc<RefCell<Option<Vec<HtmlValue>>>>);
impl TokenSink for Sink {
    type Handle = ();
    fn process_token(&self, token: Token, _: u64) -> TokenSinkResult<()> {
        let mut result = self.0.borrow_mut();
        if let Some(values) = result.as_mut() {
            match token {
                Token::CharacterTokens(text) => values.push(HtmlValue::Text(text.to_string())),
                Token::TagToken(tag) => values.push(HtmlValue::Tag {
                    name:tag.name.to_string(), close:tag.kind == TagKind::EndTag,
                    empty:tag.self_closing,
                    attrs:tag.attrs.into_iter().map(|attr|
                        (attr.name.local.to_string(), attr.value.to_string())).collect(),
                }),
                Token::EOFToken => {},
                // Parse errors, comments and declarations cannot be represented losslessly.
                _ => *result = None,
            }
        }
        TokenSinkResult::Continue
    }
}
fn tokenize(input: &str) -> Option<Vec<HtmlValue>> {
    let result = Rc::new(RefCell::new(Some(Vec::new())));
    let tokenizer = Tokenizer::new(Sink(result.clone()), TokenizerOpts::default());
    let queue = BufferQueue::default(); queue.push_back(StrTendril::from(input));
    let _ = tokenizer.feed(&queue); tokenizer.end();
    let value = result.borrow_mut().take(); value
}
/// Split only at actual tag boundaries; quoted `>` characters remain in attributes.
/// Every retained range indexes the original UTF-8 source, never decoded text.
pub(super) fn tokens(input: &str) -> Option<Vec<HtmlToken>> {
    let mut result = Vec::new(); let mut at = 0;
    while at < input.len() {
        let start = at;
        let tag = input[at..].starts_with('<') && input.as_bytes().get(at+1)
            .is_some_and(|b| b.is_ascii_alphabetic() || matches!(b,b'/'|b'!'|b'?'));
        if tag {
            let mut quote = None; at += 1;
            while at < input.len() {
                let byte = input.as_bytes()[at]; at += 1;
                if let Some(q) = quote { if byte == q { quote = None; } }
                else if matches!(byte, b'\'' | b'"') { quote = Some(byte); }
                else if byte == b'>' { break; }
            }
            if !input[start..at].ends_with('>') || quote.is_some() { return None; }
        } else {
            at += input[at..].chars().next()?.len_utf8();
            while at < input.len() && !input[at..].starts_with('<') {
                at += input[at..].chars().next()?.len_utf8();
            }
        }
        let values = tokenize(&input[start..at])?;
        if tag && (values.len() != 1 || !matches!(values[0], HtmlValue::Tag { .. })) { return None; }
        for value in values { result.push(HtmlToken { range:start..at, value }); }
    }
    Some(result)
}

pub(super) fn escape_attribute(value: &str, out: &mut String) {
    for ch in value.chars() {
        match ch { '&' => out.push_str("&amp;"), '"' => out.push_str("&quot;"),
            '<' => out.push_str("&lt;"), '>' => out.push_str("&gt;"), _ => out.push(ch) }
    }
}
pub(super) fn open_tag(name: &str, attrs: &[(String, String)], out: &mut String) {
    out.push('<'); out.push_str(name);
    for (key, value) in attrs {
        out.push(' '); out.push_str(key); out.push_str("=\"");
        escape_attribute(value, out); out.push('"');
    }
    out.push('>');
}
