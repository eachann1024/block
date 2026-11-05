//! HTML/XML/JSX tag matching: opening, closing, fragments, and self-closing tags.
use std::ops::Range;

#[allow(dead_code)]
#[derive(Debug, Clone)]
pub struct TagElement {
    pub name: String,
    pub open_range: Range<usize>,
    pub close_range: Option<Range<usize>>,
    pub self_closing: bool,
}

impl TagElement {
    pub fn full_range(&self) -> Range<usize> {
        if let Some(close) = &self.close_range {
            self.open_range.start..close.end
        } else {
            self.open_range.clone()
        }
    }
}

pub fn match_tag_at(
    text: &str,
    offset: usize,
    lang: &str,
    _include_delimiters: bool,
) -> Option<Range<usize>> {
    if !matches!(lang, "html" | "xml" | "javascript" | "typescript" | "markdown") {
        return None;
    }
    let elements = parse_tags(text, lang);
    let mut hit_idx = None;
    let mut min_len = usize::MAX;

    for (idx, el) in elements.iter().enumerate() {
        let at_open = offset >= el.open_range.start && offset <= el.open_range.end;
        let at_close = el.close_range.as_ref().map_or(false, |c| offset >= c.start && offset <= c.end);
        if at_open || at_close {
            let len = el.full_range().len();
            if len < min_len {
                min_len = len;
                hit_idx = Some(idx);
            }
        }
    }

    let target_idx = hit_idx?;
    let target = &elements[target_idx];
    Some(target.full_range())
}

pub fn parse_tags(text: &str, lang: &str) -> Vec<TagElement> {
    let raw_tags = scan_raw_tags(text, lang);
    let is_html = matches!(lang, "html" | "markdown");
    let mut stack: Vec<RawTag> = Vec::new();
    let mut elements = Vec::new();

    for tag in raw_tags {
        if tag.is_closing {
            let pos = stack.iter().rposition(|t| {
                if is_html { t.name.eq_ignore_ascii_case(&tag.name) }
                else { t.name == tag.name }
            });
            if let Some(p) = pos {
                let open = stack.remove(p);
                stack.truncate(p);
                elements.push(TagElement {
                    name: open.name,
                    open_range: open.range,
                    close_range: Some(tag.range),
                    self_closing: false,
                });
            }
        } else if tag.self_closing || (is_html && is_html_void(&tag.name)) {
            elements.push(TagElement {
                name: tag.name,
                open_range: tag.range,
                close_range: None,
                self_closing: true,
            });
        } else {
            stack.push(tag);
        }
    }
    elements
}

#[derive(Debug)]
pub struct RawTag {
    pub name: String,
    pub range: Range<usize>,
    pub is_closing: bool,
    pub self_closing: bool,
}

fn scan_raw_tags(text: &str, lang: &str) -> Vec<RawTag> {
    let mut tags = Vec::new();
    let bytes = text.as_bytes();
    let n = bytes.len();
    let mut i = 0;
    let is_html = matches!(lang, "html" | "markdown");

    while i < n {
        if bytes[i..].starts_with(b"<!--") {
            let len = bytes[i + 4..].windows(3).position(|w| w == b"-->").map_or(n - (i + 4), |p| p + 3) + 4;
            i += len;
            continue;
        }
        if bytes[i..].starts_with(b"<![CDATA[") {
            let len = bytes[i + 9..].windows(3).position(|w| w == b"]]>").map_or(n - (i + 9), |p| p + 3) + 9;
            i += len;
            continue;
        }
        if matches!(lang, "javascript" | "typescript") {
            if bytes[i..].starts_with(b"//") {
                let len = bytes[i..].iter().position(|&c| c == b'\n').map_or(n - i, |p| p + 1);
                i += len;
                continue;
            }
            if bytes[i..].starts_with(b"/*") {
                let len = bytes[i + 2..].windows(2).position(|w| w == b"*/").map_or(n - (i + 2), |p| p + 2) + 2;
                i += len;
                continue;
            }
        }
        if bytes[i] == b'"' || bytes[i] == b'\'' || (bytes[i] == b'`' && matches!(lang, "javascript" | "typescript")) {
            let q = bytes[i];
            i += 1;
            while i < n {
                if bytes[i] == b'\\' {
                    if i + 1 < n { i += 2; } else { i = n; }
                } else if bytes[i] == q {
                    i += 1;
                    break;
                } else {
                    i += 1;
                }
            }
            continue;
        }

        if bytes[i] == b'<' {
            if let Some(tag) = scan_single_tag(text, bytes, i, is_html) {
                let end = tag.range.end;
                let name_lower = tag.name.to_ascii_lowercase();
                let should_skip_raw_block = is_html && !tag.is_closing && !tag.self_closing
                    && matches!(name_lower.as_str(), "script" | "style");
                tags.push(tag);
                i = end;

                if should_skip_raw_block {
                    let close_pattern = format!("</{}", name_lower).into_bytes();
                    let body = &bytes[i..];
                    let found = body.windows(close_pattern.len()).position(|w| {
                        w.eq_ignore_ascii_case(&close_pattern)
                    });
                    if let Some(offset) = found {
                        i += offset;
                    } else {
                        i = n;
                    }
                }
                continue;
            }
        }
        let ch_len = text[i..].chars().next().map_or(1, |c| c.len_utf8());
        i += ch_len;
    }
    tags
}

fn scan_single_tag(text: &str, bytes: &[u8], start: usize, is_html: bool) -> Option<RawTag> {
    let n = bytes.len();
    if start + 1 >= n { return None; }

    if bytes[start..].starts_with(b"<>") {
        return Some(RawTag { name: String::new(), range: start..start + 2, is_closing: false, self_closing: false });
    }
    if bytes[start..].starts_with(b"</>") {
        return Some(RawTag { name: String::new(), range: start..start + 3, is_closing: true, self_closing: false });
    }

    let mut p = start + 1;
    let is_closing = bytes[p] == b'/';
    if is_closing { p += 1; }
    if p >= n { return None; }

    let b = bytes[p];
    let is_valid_start = if is_html {
        b.is_ascii_alphabetic()
    } else {
        b.is_ascii_alphabetic() || b == b'_'
    };
    if !is_valid_start { return None; }

    let name_start = p;
    while p < n && (bytes[p].is_ascii_alphanumeric() || bytes[p] == b'-' || bytes[p] == b':' || bytes[p] == b'_' || (!is_html && bytes[p] == b'.')) {
        p += 1;
    }
    let name = text[name_start..p].to_string();

    let mut in_quote = None;
    let mut brace_depth = 0usize;
    let mut self_closing = false;

    while p < n {
        let c = bytes[p];
        if let Some(q) = in_quote {
            if c == b'\\' { p += 1; }
            else if c == q { in_quote = None; }
        } else if c == b'"' || c == b'\'' {
            in_quote = Some(c);
        } else if !is_html && c == b'{' {
            brace_depth += 1;
        } else if !is_html && c == b'}' {
            brace_depth = brace_depth.saturating_sub(1);
        } else if brace_depth == 0 {
            if c == b'/' && p + 1 < n && bytes[p + 1] == b'>' {
                self_closing = true;
                p += 2;
                return Some(RawTag { name, range: start..p, is_closing, self_closing });
            } else if c == b'>' {
                p += 1;
                return Some(RawTag { name, range: start..p, is_closing, self_closing });
            }
        }
        p += 1;
    }
    None
}

fn is_html_void(tag: &str) -> bool {
    matches!(
        tag.to_ascii_lowercase().as_str(),
        "area" | "base" | "br" | "col" | "embed" | "hr" | "img" | "input"
            | "link" | "meta" | "param" | "source" | "track" | "wbr"
    )
}
