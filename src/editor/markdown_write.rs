//! Standard Markdown events and HTML are distinct output contexts. The CommonMark
//! writer checks its event round trip; only unrepresentable rich text uses HTML.
use super::{html, inline};
use super::super::model::{Block, InlineTag, Kind, MarkKind};
use pulldown_cmark::{Event, HeadingLevel, LinkType, Options, Parser, Tag, TagEnd};

const OPTIONS: Options = Options::ENABLE_STRIKETHROUGH.union(Options::ENABLE_TABLES);

#[derive(Clone, Copy, PartialEq)]
enum Wrapper<'a> { Mark(MarkKind), Annotation(&'a InlineTag) }

fn mark_tag(mark: MarkKind) -> Tag<'static> {
    match mark {
        MarkKind::Bold => Tag::Strong, MarkKind::Italic => Tag::Emphasis,
        MarkKind::Strike => Tag::Strikethrough,
        MarkKind::Code => unreachable!("code is a literal event, never a delimiter wrapper"),
    }
}
fn link(tag: &InlineTag) -> Option<Tag<'static>> {
    if tag.name != "a" || tag.attrs.iter().any(|(key, _)| !matches!(key.as_str(), "href" | "title")) { return None; }
    let href = tag.attrs.iter().find(|(key, _)| key == "href")?;
    let title = tag.attrs.iter().find(|(key, _)| key == "title").map_or("", |(_, value)| value);
    Some(Tag::Link { link_type:LinkType::Inline, dest_url:href.1.clone().into(),
        title:title.to_owned().into(), id:"".into() })
}
fn wrapper_event(wrapper: Wrapper<'_>, close: bool) -> Event<'static> {
    let tag = match wrapper {
        Wrapper::Mark(mark) => mark_tag(mark),
        Wrapper::Annotation(tag) => {
            if let Some(link) = link(tag) { link } else {
                let mut source = String::new();
                if close { source.push_str("</"); source.push_str(&tag.name); source.push('>'); }
                else { html::open_tag(&tag.name, &tag.attrs, &mut source); }
                return Event::InlineHtml(source.into());
            }
        }
    };
    if close { Event::End(tag.to_end()) } else { Event::Start(tag) }
}

/// Stable outer wrappers survive changes to inner marks. Crossing ranges are
/// closed and reopened so links, colors and marks never produce invalid nesting.
pub(super) fn events(block: &Block) -> Vec<Event<'static>> {
    let mut block = block.clone(); block.normalize_spans();
    let tags: Vec<_> = block.meta.inline.iter().filter(|tag|
        tag.start < tag.end && tag.end <= block.text.len()
        && block.text.is_char_boundary(tag.start) && block.text.is_char_boundary(tag.end)).collect();
    let mut boundaries = vec![0, block.text.len()];
    for span in &block.spans { boundaries.extend([span.start, span.end]); }
    for tag in &tags { boundaries.extend([tag.start, tag.end]); }
    boundaries.sort_unstable(); boundaries.dedup();
    let mut events = Vec::new(); let mut opened = Vec::new();
    for pair in boundaries.windows(2) {
        let (start, end) = (pair[0], pair[1]);
        let marks = block.spans.iter().find(|span| span.start <= start && end <= span.end)
            .map_or(&[][..], |span| span.marks.as_slice());
        let mut active: Vec<_> = tags.iter().filter(|tag| tag.start <= start && end <= tag.end)
            .map(|tag| Wrapper::Annotation(tag)).collect();
        active.extend(marks.iter().filter(|mark| **mark != MarkKind::Code).map(|mark| Wrapper::Mark(*mark)));
        let common = opened.iter().zip(&active).take_while(|(a, b)| a == b).count();
        for wrapper in opened[common..].iter().rev() { events.push(wrapper_event(*wrapper, true)); }
        for wrapper in &active[common..] { events.push(wrapper_event(*wrapper, false)); }
        opened = active;
        let text = &block.text[start..end];
        if marks.contains(&MarkKind::Code) { events.push(Event::Code(text.to_owned().into())); }
        else {
            // Editor newlines live inside one block, so use the standard inline
            // HTML break rather than creating new source lines/block prefixes.
            for (index, line) in text.split('\n').enumerate() {
                if index > 0 { events.push(Event::InlineHtml("<br>".into())); }
                if !line.is_empty() { events.push(Event::Text(line.to_owned().into())); }
            }
        }
    }
    for wrapper in opened.iter().rev() { events.push(wrapper_event(*wrapper, true)); }
    events
}

pub(super) fn render(events: impl IntoIterator<Item = Event<'static>>) -> Option<String> {
    let mut source = String::new();
    pulldown_cmark_to_cmark::cmark(events, &mut source, OPTIONS).ok()?;
    Some(simplify_escapes(source))
}

/// Keep ordinary paths/punctuation readable (including instruction-file `@~/`
/// includes). Remove an escape only when the standard parser proves it redundant.
fn simplify_escapes(mut source: String) -> String {
    if !source.contains('\\') { return source; }
    let expected = parsed_events(&source);
    for ch in ['~', '.', '-', '+', '#', '!', '_', '(', ')', ':', '/', '@'] {
        let escaped = format!("\\{ch}");
        if !source.contains(&escaped) { continue; }
        let candidate = source.replace(&escaped, &ch.to_string());
        if parsed_events(&candidate) == expected { source = candidate; }
    }
    source
}
fn parsed_events(source: &str) -> Vec<Event<'static>> {
    let mut events = Vec::new();
    for event in Parser::new_ext(source, OPTIONS) {
        if let Event::Text(text) = &event && let Some(Event::Text(previous)) = events.last_mut() {
            *previous = format!("{previous}{text}").into();
        } else { events.push(event.into_static()); }
    }
    events
}
pub(super) fn markdown(block: &Block, out: &mut String) {
    let level = block.meta.heading_level.or(match block.kind {
        Kind::H1 => Some(1), Kind::H2 => Some(2), Kind::H3 => Some(3), _ => None,
    }).filter(|level| (1..=6).contains(level));
    let tag = if let Some(level) = level {
        Tag::Heading { level:HeadingLevel::try_from(level as usize).unwrap(), id:None, classes:Vec::new(), attrs:Vec::new() }
    } else { Tag::Paragraph };
    let events = std::iter::once(Event::Start(tag.clone())).chain(events(block))
        .chain(std::iter::once(Event::End(tag.to_end())));
    if let Some(source) = render(events) {
        // The editor also understands inline HTML marks/attributes. Check that
        // interpretation too before accepting the standard writer's output.
        let source = source.trim_end_matches('\n');
        let source = if let Some(level) = level { source.get(level as usize + 1..).unwrap_or("") } else { source };
        let mut expected = block.clone(); expected.normalize_spans();
        if inline::parse(source).is_some_and(|(text, spans, tags)|
            text == expected.text && spans == expected.spans && tags == expected.meta.inline) {
            // These editor extensions are not enabled in the CommonMark writer.
            // Quote their opener when they are ordinary paragraph text.
            if source.starts_with("$$") || source.starts_with(":::") { out.push('\\'); }
            out.push_str(source); return;
        }
    }
    // HTML tags can represent marks that Markdown delimiters cannot (for
    // example bold leading spaces). Text still needs Markdown escaping here.
    let mut source = String::new(); html_with_context(block, &mut source, true);
    out.push_str(&simplify_escapes(source));
}

/// HTML containers never interpret Markdown delimiters. Escape only HTML's
/// reserved characters, and whitespace that the Markdown block layer trims.
pub(super) fn html(block: &Block, out: &mut String) {
    html_with_context(block, out, false);
}
pub(super) fn markdown_html(block: &Block, out: &mut String) {
    html_with_context(block, out, true);
}
fn html_with_context(block: &Block, out: &mut String, markdown: bool) {
    for event in events(block) {
        match event {
            Event::Start(tag) => match tag {
                Tag::Strong => out.push_str("<strong>"), Tag::Emphasis => out.push_str("<em>"),
                Tag::Strikethrough => out.push_str("<del>"),
                Tag::Link { dest_url, title, .. } => {
                    let mut attrs = vec![("href".into(), dest_url.to_string())];
                    if !title.is_empty() { attrs.push(("title".into(), title.to_string())); }
                    html::open_tag("a", &attrs, out);
                }, _ => unreachable!(),
            },
            Event::End(tag) => out.push_str(match tag { TagEnd::Strong => "</strong>",
                TagEnd::Emphasis => "</em>", TagEnd::Strikethrough => "</del>",
                TagEnd::Link => "</a>", _ => unreachable!() }),
            Event::InlineHtml(source) => out.push_str(&source),
            Event::Code(text) => { out.push_str("<code>"); escape_html(&text, out, markdown); out.push_str("</code>"); },
            Event::Text(text) => escape_html(&text, out, markdown),
            _ => unreachable!(),
        }
    }
}
fn escape_html(text: &str, out: &mut String, markdown: bool) {
    let leading = text.len() - text.trim_start_matches(' ').len();
    let trailing = text.trim_end_matches(' ').len();
    for (at, ch) in text.char_indices() {
        match ch {
            '&' => out.push_str("&amp;"), '<' => out.push_str("&lt;"), '>' => out.push_str("&gt;"),
            '\n' => out.push_str("<br>"), '\r' => out.push_str("&#13;"), '\t' => out.push_str("&#9;"),
            ' ' if at < leading || at >= trailing => out.push_str("&#32;"),
            '\\' | '`' | '*' | '_' | '[' | ']' | '~' | '#' | '-' | '+' | '.' | '!' if markdown => {
                out.push('\\'); out.push(ch);
            },
            _ => out.push(ch),
        }
    }
}
