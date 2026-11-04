use super::Kind;
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
