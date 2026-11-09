//! Single-line image block syntax.
//!
//! Plain images stay `![alt](src)` (the only form thinkly itself writes). A resized image uses
//! `<img src alt width>` (thinkly's `toExternalHTML` form), optionally wrapped in
//! `<p align="center|right">` for alignment.
use std::path::{Path, PathBuf};
use super::model::Block;

#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub(crate) enum Align { #[default] Left, Center, Right }
#[derive(Clone, PartialEq, Debug)]
pub(crate) struct ImageSpec {
    pub alt: String, pub src: String, pub title: Option<String>, pub width: Option<u32>, pub align: Align,
}

fn unescape_attr(value: &str) -> String {
    value.replace("&quot;", "\"").replace("&#39;", "'").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")
}
fn escape_attr(value: &str) -> String {
    value.replace('&', "&amp;").replace('"', "&quot;").replace('<', "&lt;").replace('>', "&gt;")
}

/// Parse one tag starting at `s[0] == '<'`: (name, attributes, byte length).
fn tag(s: &str) -> Option<(String, Vec<(String, String)>, usize)> {
    let bytes = s.as_bytes();
    if bytes.first() != Some(&b'<') { return None; }
    let mut at = 1;
    let name_len = s[at..].bytes().take_while(u8::is_ascii_alphanumeric).count();
    if name_len == 0 { return None; }
    let name = s[at..at + name_len].to_ascii_lowercase(); at += name_len;
    let mut attrs = Vec::new();
    loop {
        while at < s.len() && (bytes[at].is_ascii_whitespace() || bytes[at] == b'/') { at += 1; }
        if at >= s.len() { return None; }
        if bytes[at] == b'>' { return Some((name, attrs, at + 1)); }
        let key_len = s[at..].bytes().take_while(|b| !b.is_ascii_whitespace() && !matches!(b, b'=' | b'>' | b'/')).count();
        if key_len == 0 { return None; }
        let key = s[at..at + key_len].to_ascii_lowercase(); at += key_len;
        while at < s.len() && bytes[at].is_ascii_whitespace() { at += 1; }
        let mut value = String::new();
        if bytes.get(at) == Some(&b'=') {
            at += 1;
            while at < s.len() && bytes[at].is_ascii_whitespace() { at += 1; }
            match bytes.get(at) {
                Some(&q) if q == b'"' || q == b'\'' => {
                    let end = s[at + 1..].find(q as char)?;
                    value = unescape_attr(&s[at + 1..at + 1 + end]); at += end + 2;
                }
                Some(_) => {
                    let len = s[at..].bytes().take_while(|b| !b.is_ascii_whitespace() && *b != b'>').count();
                    value = unescape_attr(&s[at..at + len]); at += len;
                }
                None => return None,
            }
        }
        attrs.push((key, value));
    }
}
fn attr<'a>(attrs: &'a [(String, String)], key: &str) -> Option<&'a str> {
    attrs.iter().find(|(k, _)| k == key).map(|(_, v)| v.as_str())
}
fn align_of(attrs: &[(String, String)]) -> Align {
    let raw = attr(attrs, "align").map(str::to_owned)
        .or_else(|| attr(attrs, "style").and_then(|s| s.split(';').find_map(|p|
            p.trim().strip_prefix("text-align:").map(|v| v.trim().to_owned()))));
    match raw.as_deref() { Some("center") => Align::Center, Some("right") => Align::Right, _ => Align::Left }
}

fn parse_html(source: &str) -> Option<ImageSpec> {
    let (mut name, mut attrs, mut len) = tag(source)?;
    let mut align = Align::Left; let mut rest = &source[len..];
    let mut closing = None;
    if name == "p" || name == "div" {
        align = align_of(&attrs); closing = Some(format!("</{name}>"));
        rest = rest.trim();
        (name, attrs, len) = tag(rest)?; rest = &rest[len..];
    }
    if name != "img" { return None; }
    let rest = rest.trim();
    match closing {
        Some(close) if rest.eq_ignore_ascii_case(&close) => {}
        None if rest.is_empty() => {}
        _ => return None,
    }
    let src = attr(&attrs, "src").filter(|s| !s.is_empty())?.to_owned();
    let width = attr(&attrs, "width").and_then(|w| w.trim().trim_end_matches("px").parse::<f32>().ok())
        .filter(|w| *w >= 1.).map(|w| w.round() as u32);
    Some(ImageSpec { alt: attr(&attrs, "alt").unwrap_or("").to_owned(), src,
        title: attr(&attrs, "title").map(str::to_owned), width, align })
}

fn parse_markdown(source: &str) -> Option<ImageSpec> {
    if !source.starts_with("![") || !source.ends_with(')') { return None; }
    let mid = source.find("](")?;
    let inner = &source[mid + 2..source.len() - 1];
    let (dest, rest) = if let Some(body) = inner.strip_prefix('<') {
        let end = body.find('>')?; (&body[..end], &body[end + 1..])
    } else {
        let end = inner.find(char::is_whitespace).unwrap_or(inner.len()); (&inner[..end], &inner[end..])
    };
    let rest = rest.trim();
    let title = if rest.is_empty() { None } else {
        let quoted = rest.len() >= 2 && matches!((rest.as_bytes()[0], rest.as_bytes()[rest.len() - 1]),
            (b'"', b'"') | (b'\'', b'\'') | (b'(', b')'));
        if !quoted || rest[1..rest.len() - 1].contains(['\n']) { return None; }
        Some(rest[1..rest.len() - 1].to_owned())
    };
    if dest.is_empty() { return None; }
    let alt = source[2..mid].replace("\\]", "]").replace("\\\\", "\\");
    Some(ImageSpec { alt, src: dest.to_owned(), title, width: None, align: Align::Left })
}

/// `None` unless the whole (single) line is exactly one image.
pub(super) fn parse(source: &str) -> Option<ImageSpec> {
    let source = source.trim();
    if source.is_empty() || source.contains('\n') { return None; }
    if source.starts_with('<') { parse_html(source) } else { parse_markdown(source) }
}

/// Serialize back to one source line (with trailing newline).
pub(super) fn format(spec: &ImageSpec) -> String {
    if spec.width.is_none() && spec.align == Align::Left {
        let alt = spec.alt.replace('\\', "\\\\").replace(']', "\\]");
        let dest = if spec.src.contains(|c: char| c.is_whitespace() || matches!(c, '(' | ')' | '<' | '>')) {
            format!("<{}>", spec.src.replace('<', "%3C").replace('>', "%3E"))
        } else { spec.src.clone() };
        let title = spec.title.as_ref().map(|t| format!(" \"{}\"", t.replace('"', "\\\""))).unwrap_or_default();
        return format!("![{alt}]({dest}{title})\n");
    }
    let mut img = format!("<img src=\"{}\" alt=\"{}\"", escape_attr(&spec.src), escape_attr(&spec.alt));
    if let Some(title) = &spec.title { img.push_str(&format!(" title=\"{}\"", escape_attr(title))); }
    if let Some(width) = spec.width { img.push_str(&format!(" width=\"{width}\"")); }
    img.push('>');
    match spec.align {
        Align::Left => format!("{img}\n"),
        Align::Center => format!("<p align=\"center\">{img}</p>\n"),
        Align::Right => format!("<p align=\"right\">{img}</p>\n"),
    }
}

#[derive(Clone, PartialEq, Debug)]
pub(super) enum ImageTarget {
    Local(PathBuf),
    Remote(String),
}

pub(crate) fn is_remote(src: &str) -> bool {
    let s = src.trim().as_bytes();
    (s.len() > 7 && s[..7].eq_ignore_ascii_case(b"http://"))
        || (s.len() > 8 && s[..8].eq_ignore_ascii_case(b"https://"))
}

pub(crate) fn spec_of(block: &Block) -> Option<ImageSpec> { parse(block.meta.raw_source.as_deref()?) }

pub(crate) fn resolve_path(spec: &ImageSpec, note: Option<&Path>) -> Option<PathBuf> {
    let src = spec.src.trim();
    if is_remote(src) {
        return super::image_remote::cached_path(src);
    }
    if src.as_bytes().get(..7).is_some_and(|p| p.eq_ignore_ascii_case(b"file://")) {
        let rest = &src[7..];
        let rest = if rest.as_bytes().get(..9).is_some_and(|p| p.eq_ignore_ascii_case(b"localhost")) {
            &rest[9..]
        } else {
            rest
        };
        return Some(PathBuf::from(rest.replace("%20", " ")));
    }
    if src.contains("://") { return None; }
    let path = PathBuf::from(src.replace("%20", " "));
    if path.is_absolute() { return Some(path); }
    Some(note?.parent()?.join(path))
}

pub(super) fn resolve_target(spec: &ImageSpec, note: Option<&Path>) -> Option<ImageTarget> {
    if is_remote(&spec.src) {
        return Some(ImageTarget::Remote(spec.src.trim().to_owned()));
    }
    resolve_path(spec, note).map(ImageTarget::Local)
}

/// Local image path for an image block, resolved next to the note.
pub(super) fn image_path(block: &Block, note: Option<&Path>) -> Option<PathBuf> {
    resolve_path(&spec_of(block)?, note)
}
