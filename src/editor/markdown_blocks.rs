use super::super::model::{Block, Kind};
pub(super) fn fence(line: &str) -> Option<(u8,usize,&str)> {
    let body = line.trim_start_matches(' ');
    if line.len()-body.len() > 3 { return None; }
    if body.trim() == "$$" { return Some((b'$', 2, "math")); }
    let marker = *body.as_bytes().first()?;
    if marker != b'`' && marker != b'~' { return None; }
    let width = body.bytes().take_while(|b| *b == marker).count();
    if width < 3 { return None; }
    let info = body[width..].trim();
    if marker == b'`' && info.contains('`') { return None; }
    Some((marker,width,info))
}
pub(super) fn closes_fence(line: &str, marker: u8, width: usize) -> bool {
    let body = line.trim_start_matches(' ');
    if marker == b'$' { return line.len() - body.len() <= 3 && body.trim() == "$$"; }
    line.len()-body.len() <= 3 && body.bytes().take_while(|b| *b == marker).count() >= width
        && body.trim_start_matches(marker as char).trim().is_empty()
}
pub(super) fn inline_math_block(line: &str) -> Option<(usize, &str)> {
    let body = line.trim_start_matches(' ');
    let indent = line.len() - body.len();
    let trimmed = body.trim_end();
    if indent <= 3 && trimmed.starts_with("$$") && trimmed.ends_with("$$") && trimmed.len() > 4 {
        let inner = trimmed[2..trimmed.len() - 2].trim();
        if !inner.contains("$$") { return Some((indent, inner)); }
    }
    None
}
/// A paragraph with delimiters spanning editor lines must stay opaque as a whole.
#[allow(dead_code)]
pub(super) fn unclosed_inline(line:&str)->bool {
    let bytes=line.as_bytes(); let mut i=0; let mut marks=[false;3]; let mut code=0; let mut brackets=0i32;
    while i<bytes.len() {
        let ch=bytes[i];
        if ch==b'\\' && code==0 {i=(i+2).min(bytes.len());continue;}
        if ch==b'`' {
            let n=bytes[i..].iter().take_while(|b|**b==ch).count();
            if code==0 {code=n;} else if code==n {code=0;} i+=n;continue;
        }
        if code!=0 {i+=1;continue;}
        if ch==b'[' {brackets+=1;} else if ch==b']' {brackets-=1;}
        if ch==b'*' || ch==b'~' {
            let n=bytes[i..].iter().take_while(|b|**b==ch).count();
            if ch==b'*' {marks[0]^=n%2==1;marks[1]^=(n/2)%2==1;}
            else {marks[2]^=(n/2)%2==1;} i+=n;continue;
        }
        i+=1;
    }
    code!=0 || brackets!=0 || marks.into_iter().any(|v|v)
}
/// Unrepresented constructs stay literal instead of being flattened into false semantics.
pub(super) fn classify(line: &str) -> (Kind,&str,bool,usize,Option<u64>,bool) {
    let body = line.trim_start_matches([' ','\t']);
    let indent = line[..line.len()-body.len()].chars().map(|c| if c=='\t' {4} else {1}).sum();
    let compact: String = line.chars().filter(|c| !c.is_whitespace()).collect();
    let rule = compact.len()>=3 && ['-','*','_','='].iter().any(|c| compact.chars().all(|v| v==*c));
    if rule || line.contains("][") {
        return (Kind::Paragraph,line,false,0,None,true);
    }
    for (prefix,kind) in [("- [ ] ",Kind::Todo),("- [x] ",Kind::Todo),("- [X] ",Kind::Todo),
        ("- ",Kind::Bullet),("+ ",Kind::Bullet),("* ",Kind::Bullet)] {
        if let Some(text) = body.strip_prefix(prefix) {
            return (kind,text,prefix.contains('x')||prefix.contains('X'),indent,None,false);
        }
    }
    let digits = body.bytes().take_while(u8::is_ascii_digit).count();
    if digits > 0 && (body[digits..].starts_with(". ") || body[digits..].starts_with(") ")) {
        if let Ok(start) = body[..digits].parse::<u64>() {
            return (Kind::Numbered,&body[digits+2..],false,indent,Some(start),false);
        }
        return (Kind::Paragraph,line,false,0,None,true);
    }
    if indent > 0 { return (Kind::Paragraph,line,false,0,None,true); }
    for (prefix,kind) in [("###### ",Kind::H3),("##### ",Kind::H3),("#### ",Kind::H3),
        ("### ",Kind::H3),("## ",Kind::H2),("# ",Kind::H1),("> ",Kind::Quote)] {
        if let Some(text) = body.strip_prefix(prefix) {
            if kind==Kind::Quote && (text.starts_with('>') || super::super::model::marker_kind(text).is_some() || fence(text).is_some()) { return (Kind::Paragraph,line,false,0,None,true); }
            let mut text = text;
            if kind != Kind::Quote && text.ends_with('#') {
                let stripped = text.trim_end_matches('#');
                if stripped.ends_with(char::is_whitespace) { text = stripped.trim_end(); }
            }
            return (kind,text,false,0,None,false);
        }
    }
    let opaque = rule || line.starts_with("<!--")
        || line.starts_with("$$") || line.starts_with(":::") || line.starts_with("[[")
        || line.starts_with("<!") || line.starts_with("[^") || line.contains("]: ");
    (Kind::Paragraph,line,false,0,None,opaque)
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
    let mut body = lines[1..end].concat();
    let empty_line = body == "\n" || body == "\r\n";
    if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
    let mut block = Block::new(id, Kind::Paragraph, body);
    block.meta.code_empty_line = empty_line;
    block.meta.code_language = Some("yaml-frontmatter".to_owned());
    Some((block, end + 1, lines[..=end].iter().map(|l| l.len()).sum()))
}
/// False when the block must stay a fenced code block so the document reloads unchanged.
pub(super) fn serialize_frontmatter(block: &Block, at_top: bool, out: &mut String) -> bool {
    if !(at_top && block.meta.column.is_none() && block.meta.indent == 0
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
