use super::super::model::{Block, MarkKind, Span};
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};

pub(super) fn parse(input: &str) -> Option<(String,Vec<Span>)> {
    if !input.bytes().any(|b| b.is_ascii_punctuation()) { return Some((input.to_owned(),Vec::new())); }
    if let Some(simple) = simple_mark(input) { return Some(simple); }
    // Sentinel prevents inline fragments from being parsed as headings, lists or HTML blocks.
    let source = format!("x {input}"); let mut skip = 2;
    let mut text = String::with_capacity(input.len()); let mut spans = Vec::new();
    let mut marks = [0u32;4]; let mut html_stack = Vec::new();
    for event in Parser::new_ext(&source,Options::ENABLE_STRIKETHROUGH) {
        match event {
            Event::Start(Tag::Paragraph) | Event::End(TagEnd::Paragraph) => {},
            Event::Start(Tag::Strong) => marks[0]+=1,
            Event::End(TagEnd::Strong) => marks[0]=marks[0].saturating_sub(1),
            Event::Start(Tag::Emphasis) => marks[1]+=1,
            Event::End(TagEnd::Emphasis) => marks[1]=marks[1].saturating_sub(1),
            Event::Start(Tag::Strikethrough) => marks[2]+=1,
            Event::End(TagEnd::Strikethrough) => marks[2]=marks[2].saturating_sub(1),
            Event::Text(value) => {
                let n = skip.min(value.len()); skip -= n; append(&mut text,&mut spans,&value[n..],&marks);
            },
            Event::Code(value) => { marks[3]+=1; append(&mut text,&mut spans,&value,&marks); marks[3]-=1; },
            Event::InlineHtml(value) => {
                let (mark,close) = match value.as_ref() {
                    "<strong>" => (0,false),"</strong>" => (0,true),
                    "<em>" => (1,false),"</em>" => (1,true),
                    "<del>" => (2,false),"</del>" => (2,true),
                    "<code>" => (3,false),"</code>" => (3,true),
                    _ => return None,
                };
                if close { if html_stack.pop()!=Some(mark) { return None; } marks[mark]-=1; }
                else { marks[mark]+=1; html_stack.push(mark); }
            },
            Event::SoftBreak => append(&mut text,&mut spans,"\n",&marks),
            _ => return None,
        }
    }
    if !html_stack.is_empty() { return None; }
    Some((text,spans))
}
fn simple_mark(input:&str)->Option<(String,Vec<Span>)> {
    // Common single-run lines avoid allocating an entire CommonMark parser per block.
    for (token,mark) in [("**",MarkKind::Bold),("*",MarkKind::Italic),("~~",MarkKind::Strike),("`",MarkKind::Code)] {
        let Some(start)=input.find(token) else {continue;};
        let body_start=start+token.len();
        let Some(relative)=input[body_start..].find(token) else {continue;};
        let close=body_start+relative; let after=close+token.len();
        let body=&input[body_start..close];
        if !body.chars().all(|c|c.is_alphanumeric() || c.is_whitespace())
            || body.is_empty() || body.starts_with(char::is_whitespace) || body.ends_with(char::is_whitespace)
            || input[..start].bytes().chain(body.bytes()).chain(input[after..].bytes()).any(|b|b.is_ascii_punctuation()) {continue;}
        let mut text=String::with_capacity(input.len()-2*token.len());
        text.push_str(&input[..start]);text.push_str(body);text.push_str(&input[after..]);
        return Some((text,vec![Span{start,end:start+body.len(),marks:vec![mark]}]));
    }
    None
}
fn append(text: &mut String, spans: &mut Vec<Span>, value: &str, counts: &[u32;4]) {
    let start=text.len(); text.push_str(value);
    if value.is_empty() { return; }
    let marks:Vec<_>=[MarkKind::Bold,MarkKind::Italic,MarkKind::Strike,MarkKind::Code]
        .into_iter().filter(|m| counts[*m as usize]>0).collect();
    if marks.is_empty() { return; }
    if let Some(last)=spans.last_mut() && last.end==start && last.marks==marks { last.end=text.len(); }
    else { spans.push(Span {start,end:text.len(),marks}); }
}
/// HTML inline marks are valid Markdown and avoid ambiguous adjacent delimiter runs,
/// whitespace-sensitive emphasis, and code/strong combinations. Entities protect literals.
pub(super) fn serialize(block: &Block,out: &mut String) {
    let mut cursor=0;
    // Public Block fields may arrive from deserialization: normalize before slicing if needed.
    let normalized;
    let mut end=0;
    let valid=block.spans.iter().all(|s| {
        let ok=s.start>=end && s.start<s.end && s.end<=block.text.len()
            && block.text.is_char_boundary(s.start) && block.text.is_char_boundary(s.end);
        end=s.end; ok
    });
    let spans=if valid { &block.spans } else { normalized={let mut b=block.clone();b.normalize_spans();b}; &normalized.spans };
    for span in spans {
        escape(&block.text[cursor..span.start],out);
        for mark in &span.marks { out.push_str(tag(*mark,false)); }
        escape(&block.text[span.start..span.end],out);
        for mark in span.marks.iter().rev() { out.push_str(tag(*mark,true)); }
        cursor=span.end;
    }
    escape(&block.text[cursor..],out);
}
fn tag(mark:MarkKind,close:bool)->&'static str {
    match (mark,close) {
        (MarkKind::Bold,false)=>"<strong>",(MarkKind::Bold,true)=>"</strong>",
        (MarkKind::Italic,false)=>"<em>",(MarkKind::Italic,true)=>"</em>",
        (MarkKind::Strike,false)=>"<del>",(MarkKind::Strike,true)=>"</del>",
        (MarkKind::Code,false)=>"<code>",(MarkKind::Code,true)=>"</code>",
    }
}
fn escape(value:&str,out:&mut String) {
    use std::fmt::Write;
    let leading = value.len()-value.trim_start_matches(' ').len();
    let trailing = value.trim_end_matches(' ').len();
    for (at,ch) in value.char_indices() {
        if ch.is_ascii_punctuation() || matches!(ch,'\n'|'\r'|'\t') { let _=write!(out,"&#{};",ch as u32); }
        else if ch==' ' && (at<leading || at>=trailing) { out.push_str("&#32;"); }
        else { out.push(ch); }
    }
}
