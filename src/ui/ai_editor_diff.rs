use std::collections::HashSet;
use gpui::{AnyElement, CursorStyle, FontWeight, Hsla, IntoElement, MouseButton, SharedString, div, prelude::*, px, rgb};
use crate::{
    editor::{ai::plan_link::{self, ActivePlanLink}, model::{Block, Kind, TableRow}, text_runs::styled_text as text},
    icons::{Icon, icon}, theme::{is_dark, tr}, ui::{radius, smoke_overlay},
};
use super::ai_plan_card::pill;

mod parse {
    use crate::editor::model::{Block, InlineTag, Kind, MarkKind, Span, TableCell, TableRow};
    use html5ever::{tendril::StrTendril, tokenizer::{BufferQueue, TagKind, Token, TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts}};
    use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};
    use std::{cell::RefCell, rc::Rc};

    pub(super) fn is_table_scaffolding(trimmed: &str) -> bool {
        if trimmed.starts_with('|') && trimmed.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':')) {
            return true;
        }
        let lower = trimmed.to_ascii_lowercase();
        lower.starts_with("<table") || lower.starts_with("</table") || lower.starts_with("<colgroup")
            || lower.starts_with("</colgroup") || lower.starts_with("<col ") || lower.starts_with("<col>")
            || lower.starts_with("<thead") || lower.starts_with("</thead") || lower.starts_with("<tbody")
            || lower.starts_with("</tbody") || lower.starts_with("<tfoot") || lower.starts_with("</tfoot")
            || lower == "<tr>" || lower == "</tr>"
    }

    pub(super) fn is_table_row_line(trimmed: &str) -> bool {
        if is_table_scaffolding(trimmed) { return false; }
        trimmed.starts_with('|') || trimmed.to_ascii_lowercase().starts_with("<tr")
    }

    pub(super) fn is_meaningful_line(source: &str) -> bool {
        let trimmed = source.trim();
        if trimmed.is_empty() || trimmed.starts_with("```") || trimmed.starts_with("~~~") || is_table_scaffolding(trimmed) {
            return false;
        }
        if trimmed.starts_with('|') { return true; }
        if trimmed.to_ascii_lowercase().starts_with("<tr") {
            return parse_table_row(trimmed, false).is_some_and(|row| row.cells.iter().any(|c| !c.content.text.trim().is_empty()));
        }
        let block = parse_line_block(trimmed);
        !block.text.trim().is_empty() || block.meta.table.is_some()
    }

    pub(super) fn parse_table_row(trimmed: &str, default_header: bool) -> Option<TableRow> {
        if trimmed.starts_with('|') {
            let count = trimmed.chars().filter(|ch| *ch == '|').count().saturating_sub(1).max(1);
            let separator = format!("|{}|", vec![" --- "; count].join("|"));
            let doc = crate::editor::markdown::parse(&format!("{trimmed}\n{separator}\n"));
            if let Some(mut row) = doc.blocks.iter().find_map(|b| b.meta.table.as_ref()).and_then(|t| t.rows.first()).cloned() {
                for cell in &mut row.cells {
                    if default_header { cell.header = true; }
                    sanitize_block(&mut cell.content);
                }
                return Some(row);
            }
            let inner = trimmed.strip_prefix('|').unwrap_or(trimmed);
            let inner = inner.strip_suffix('|').unwrap_or(inner);
            let cells: Vec<TableCell> = inner.split('|').enumerate().map(|(col, raw)| {
                let content = parse_inline_fragment(raw.trim());
                TableCell {
                    original: Box::new(content.clone()), content, row: 0, column: col,
                    colspan: 1, rowspan: 1, header: default_header, attrs: Vec::new(), source_range: 0..0,
                }
            }).collect();
            return (!cells.is_empty()).then_some(TableRow { cells });
        }
        if trimmed.to_ascii_lowercase().starts_with("<tr") {
            let doc = crate::editor::markdown::parse(&format!("<table><tbody>{trimmed}</tbody></table>"));
            if let Some(mut row) = doc.blocks.iter().find_map(|b| b.meta.table.as_ref()).and_then(|t| t.rows.first()).cloned() {
                for cell in &mut row.cells {
                    if default_header { cell.header = true; }
                    sanitize_block(&mut cell.content);
                }
                return Some(row);
            }
            let cells = extract_html_cells(trimmed, default_header);
            if !cells.is_empty() { return Some(TableRow { cells }); }
        }
        None
    }

    fn extract_html_cells(source: &str, default_header: bool) -> Vec<TableCell> {
        let mut cells = Vec::new();
        let mut rest = source;
        while let Some(open_at) = rest.find('<') {
            let after = &rest[open_at + 1..];
            let lower = after.to_ascii_lowercase();
            let (is_th, tag_len) = if lower.starts_with("th") && lower.as_bytes().get(2).is_some_and(|b| b.is_ascii_whitespace() || *b == b'>') {
                (true, 2)
            } else if lower.starts_with("td") && lower.as_bytes().get(2).is_some_and(|b| b.is_ascii_whitespace() || *b == b'>') {
                (false, 2)
            } else {
                rest = &after[after.chars().next().map_or(0, char::len_utf8)..];
                continue;
            };
            let Some(gt) = after[tag_len..].find('>') else { break };
            let inner_start = open_at + 1 + tag_len + gt + 1;
            let body = &rest[inner_start..];
            let close_pat = if is_th { "</th>" } else { "</td>" };
            let close_at = body.to_ascii_lowercase().find(close_pat).unwrap_or(body.len());
            let content = parse_inline_fragment(&body[..close_at]);
            let col = cells.len();
            cells.push(TableCell {
                original: Box::new(content.clone()), content, row: 0, column: col,
                colspan: 1, rowspan: 1, header: is_th || default_header, attrs: Vec::new(), source_range: 0..0,
            });
            rest = &body[(close_at + close_pat.len()).min(body.len())..];
        }
        cells
    }

    pub(super) fn parse_line_block(source: &str) -> Block {
        let line = source.trim_end_matches(['\r', '\n']);
        let doc = crate::editor::markdown::parse(line);
        let mut block = doc.blocks.iter().next().cloned().unwrap_or_else(|| Block::new(1, Kind::Paragraph, String::new()));
        sanitize_block(&mut block);
        block
    }

    pub(super) fn sanitize_block(block: &mut Block) {
        if block.meta.code_language.is_some() { return; }
        if let Some(table) = &mut block.meta.table {
            for row in &mut table.rows {
                for cell in &mut row.cells { sanitize_block(&mut cell.content); }
            }
            return;
        }
        if block.meta.raw_source.is_none() && !has_html_tag(&block.text) { return; }
        let raw = block.text.trim_end_matches(['\r', '\n']).trim_start_matches([' ', '\t']);
        let (kind, body, checked, level) = if block.meta.raw_source.is_some() {
            classify_prefix(raw)
        } else {
            (block.kind, block.text.as_str(), block.checked, block.meta.heading_level)
        };
        let parsed = parse_inline_fragment(body);
        block.kind = kind;
        block.checked = checked;
        block.meta.heading_level = level;
        block.text = parsed.text;
        block.spans = parsed.spans;
        block.meta.inline = parsed.meta.inline;
        block.meta.raw_source = None;
        block.meta.unsupported_reason = None;
    }

    fn has_html_tag(text: &str) -> bool {
        let bytes = text.as_bytes();
        for i in 0..bytes.len().saturating_sub(2) {
            if bytes[i] == b'<' && (bytes[i + 1].is_ascii_alphabetic() || bytes[i + 1] == b'/') && text[i + 2..].contains('>') {
                return true;
            }
        }
        false
    }

    fn classify_prefix(body: &str) -> (Kind, &str, bool, Option<u8>) {
        for (prefix, checked) in [("- [ ] ", false), ("- [x] ", true), ("- [X] ", true)] {
            if let Some(rest) = body.strip_prefix(prefix) { return (Kind::Todo, rest, checked, None); }
        }
        for prefix in ["- ", "+ ", "* "] {
            if let Some(rest) = body.strip_prefix(prefix) { return (Kind::Bullet, rest, false, None); }
        }
        let digits = body.bytes().take_while(u8::is_ascii_digit).count();
        if digits > 0 && (body[digits..].starts_with(". ") || body[digits..].starts_with(") ")) {
            return (Kind::Numbered, &body[digits + 2..], false, None);
        }
        for (prefix, kind, lvl) in [
            ("###### ", Kind::H3, Some(6)), ("##### ", Kind::H3, Some(5)), ("#### ", Kind::H3, Some(4)),
            ("### ", Kind::H3, None), ("## ", Kind::H2, None), ("# ", Kind::H1, None), ("> ", Kind::Quote, None),
        ] {
            if let Some(rest) = body.strip_prefix(prefix) { return (kind, rest, false, lvl); }
        }
        (Kind::Paragraph, body, false, None)
    }

    #[derive(Clone)]
    enum Tok { Text(String), Tag { name: String, attrs: Vec<(String, String)>, close: bool, empty: bool } }

    #[derive(Clone)]
    struct Sink(Rc<RefCell<Vec<Tok>>>);
    impl TokenSink for Sink {
        type Handle = ();
        fn process_token(&self, token: Token, _: u64) -> TokenSinkResult<()> {
            match token {
                Token::CharacterTokens(t) => self.0.borrow_mut().push(Tok::Text(t.to_string())),
                Token::TagToken(tag) => self.0.borrow_mut().push(Tok::Tag {
                    name: tag.name.to_string().to_ascii_lowercase(),
                    close: tag.kind == TagKind::EndTag,
                    empty: tag.self_closing,
                    attrs: tag.attrs.into_iter().map(|a| (a.name.local.to_string(), a.value.to_string())).collect(),
                }),
                _ => {}
            }
            TokenSinkResult::Continue
        }
    }

    pub(super) fn parse_inline_fragment(input: &str) -> Block {
        let mut text = String::new();
        let mut spans: Vec<Span> = Vec::new();
        let mut inline: Vec<InlineTag> = Vec::new();
        let mut marks = [0u32; 4];
        let mut open_tags: Vec<(String, usize, Vec<(String, String)>, Option<usize>)> = Vec::new();
        let mut open_links: Vec<(usize, Vec<(String, String)>)> = Vec::new();
        let push_text = |value: &str, text: &mut String, spans: &mut Vec<Span>, marks: &[u32; 4]| {
            if value.is_empty() { return; }
            let start = text.len();
            text.push_str(value);
            let active: Vec<_> = [MarkKind::Bold, MarkKind::Italic, MarkKind::Strike, MarkKind::Code]
                .into_iter().filter(|m| marks[*m as usize] > 0).collect();
            if active.is_empty() { return; }
            if let Some(last) = spans.last_mut() && last.end == start && last.marks == active {
                last.end = text.len();
            } else {
                spans.push(Span { start, end: text.len(), marks: active });
            }
        };
        let handle_html = |raw_html: &str, text: &mut String, spans: &mut Vec<Span>, inline: &mut Vec<InlineTag>,
            marks: &mut [u32; 4], open_tags: &mut Vec<(String, usize, Vec<(String, String)>, Option<usize>)>| {
            let out = Rc::new(RefCell::new(Vec::new()));
            let tokenizer = Tokenizer::new(Sink(out.clone()), TokenizerOpts::default());
            let queue = BufferQueue::default();
            queue.push_back(StrTendril::from(raw_html));
            let _ = tokenizer.feed(&queue);
            tokenizer.end();
            for tok in out.borrow().iter() {
                match tok {
                    Tok::Text(t) => push_text(t, text, spans, marks),
                    Tok::Tag { name, attrs, close, empty } => {
                        if name == "br" { if !*close { push_text("\n", text, spans, marks); } continue; }
                        if name == "wbr" { continue; }
                        let mark = match name.as_str() {
                            "strong" | "b" => Some(0),
                            "em" | "i" | "cite" | "dfn" | "var" => Some(1),
                            "del" | "s" | "strike" => Some(2),
                            "code" | "kbd" | "samp" | "tt" => Some(3),
                            _ => None,
                        };
                        if *close {
                            if let Some(pos) = open_tags.iter().rposition(|(n, _, _, _)| n == name) {
                                let (opened, start, attrs, m) = open_tags.remove(pos);
                                if let Some(m) = m { marks[m] = marks[m].saturating_sub(1); }
                                if start < text.len() && (m.is_none() || !attrs.is_empty()) {
                                    inline.push(InlineTag { start, end: text.len(), name: if m.is_some() { "span".into() } else { opened }, attrs });
                                }
                            }
                        } else if !*empty {
                            if let Some(m) = mark { marks[m] += 1; }
                            open_tags.push((name.clone(), text.len(), attrs.clone(), mark));
                        }
                    }
                }
            }
        };
        let source = format!("x {input}");
        let mut skip = 2usize;
        for event in Parser::new_ext(&source, Options::ENABLE_STRIKETHROUGH) {
            match event {
                Event::Start(Tag::Strong) => marks[0] += 1,
                Event::End(TagEnd::Strong) => marks[0] = marks[0].saturating_sub(1),
                Event::Start(Tag::Emphasis) => marks[1] += 1,
                Event::End(TagEnd::Emphasis) => marks[1] = marks[1].saturating_sub(1),
                Event::Start(Tag::Strikethrough) => marks[2] += 1,
                Event::End(TagEnd::Strikethrough) => marks[2] = marks[2].saturating_sub(1),
                Event::Start(Tag::Link { dest_url, title, .. }) => {
                    let mut attrs = vec![("href".into(), dest_url.to_string())];
                    if !title.is_empty() { attrs.push(("title".into(), title.to_string())); }
                    open_links.push((text.len(), attrs));
                }
                Event::End(TagEnd::Link) => if let Some((start, attrs)) = open_links.pop() && start < text.len() {
                    inline.push(InlineTag { start, end: text.len(), name: "a".into(), attrs });
                },
                Event::Text(val) => {
                    let n = skip.min(val.len());
                    skip -= n;
                    push_text(&val[n..], &mut text, &mut spans, &marks);
                }
                Event::Code(val) => {
                    marks[3] += 1;
                    push_text(&val, &mut text, &mut spans, &marks);
                    marks[3] = marks[3].saturating_sub(1);
                }
                Event::InlineHtml(val) | Event::Html(val) => {
                    handle_html(&val, &mut text, &mut spans, &mut inline, &mut marks, &mut open_tags);
                }
                Event::SoftBreak | Event::HardBreak => push_text("\n", &mut text, &mut spans, &marks),
                _ => {}
            }
        }
        for (opened, start, attrs, m) in open_tags {
            if start < text.len() && (m.is_none() || !attrs.is_empty()) {
                inline.push(InlineTag { start, end: text.len(), name: if m.is_some() { "span".into() } else { opened }, attrs });
            }
        }
        let mut block = Block::new(0, Kind::Paragraph, text);
        block.spans = spans;
        block.meta.inline = inline;
        block
    }
}

struct DiffColors {
    del_bg: Hsla, del_border: Hsla, del_text: Hsla, del_badge: Hsla,
    add_bg: Hsla, add_border: Hsla, add_text: Hsla, add_badge: Hsla, code_bg: Hsla,
}

fn palette() -> DiffColors {
    if is_dark() {
        DiffColors {
            del_bg: rgb(0x262223).into(), del_border: rgb(0x936569).into(), del_text: rgb(0xd5a5a7).into(), del_badge: rgb(0x302729).into(),
            add_bg: rgb(0x202723).into(), add_border: rgb(0x648972).into(), add_text: rgb(0xa6cbb3).into(), add_badge: rgb(0x27322b).into(),
            code_bg: rgb(0x202022).into(),
        }
    } else {
        DiffColors {
            del_bg: rgb(0xfcf8f8).into(), del_border: rgb(0xb47777).into(), del_text: rgb(0x965353).into(), del_badge: rgb(0xf5eaea).into(),
            add_bg: rgb(0xf5faf6).into(), add_border: rgb(0x709b80).into(), add_text: rgb(0x376d4d).into(), add_badge: rgb(0xeaf3ed).into(),
            code_bg: rgb(0xf6f5f2).into(),
        }
    }
}

fn table_row(row: &TableRow, change: i8, header: Option<bool>, bottom_border: bool) -> impl IntoElement {
    let (theme, pal, dark) = (crate::theme::Theme::ocean(), palette(), is_dark());
    let (is_header, count) = (header.unwrap_or_else(|| row.cells.iter().any(|c| c.header)), row.cells.len());
    let header_bg: Hsla = if dark { gpui::rgba(0xffffff08).into() } else { rgb(0xf5f3ed).into() };
    div().relative().when(change < 0, |el| el.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .flex().w_full().min_w_0().overflow_hidden()
        .when(bottom_border, |el| el.border_b_1().border_color(theme.hairline))
        .when(change == 0 && is_header, |el| el.bg(header_bg))
        .when(change < 0, |el| el.bg(pal.del_bg).text_color(theme.text_secondary))
        .when(change > 0, |el| el.bg(pal.add_bg))
        .children(row.cells.iter().enumerate().map(|(idx, cell)| {
            let cell_header = header.unwrap_or(cell.header);
            div().flex_1().min_w_0().overflow_hidden().px(px(8.)).py(px(6.)).text_size(px(13.5)).line_height(px(20.))
                .when(idx + 1 < count, |el| el.border_r_1().border_color(theme.hairline))
                .when(cell_header, |el| el.font_weight(FontWeight(600.)).text_color(theme.text_secondary))
                .child(text(&cell.content))
        }))
}

fn block_view(block: &Block, change: i8) -> AnyElement {
    let (theme, pal) = (crate::theme::Theme::ocean(), palette());
    let mut clean = block.clone();
    parse::sanitize_block(&mut clean);
    let block = &clean;
    if let Some(table) = &block.meta.table {
        let len = table.rows.len();
        return div().w_full().min_w_0().rounded(px(8.)).border_1().border_color(theme.hairline).overflow_hidden()
            .children(table.rows.iter().enumerate().map(|(idx, row)| table_row(row, change, None, idx + 1 < len))).into_any_element();
    }
    let is_heading = matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3);
    let (font_size, line_h) = match block.kind {
        Kind::H1 => (20., 28.), Kind::H2 => (17., 24.), Kind::H3 => (15.5, 22.), _ => (14.5, 22.),
    };
    let is_tsv = block.kind == Kind::Paragraph && block.text.matches('\t').count() >= 2;
    let is_code = block.meta.code_language.is_some();
    let content: AnyElement = if is_tsv {
        let cells: Vec<_> = block.text.split('\t').map(str::trim).filter(|s| !s.is_empty()).collect();
        div().w_full().min_w_0().overflow_hidden().flex().flex_wrap().items_center().gap(px(6.)).py(px(2.)).children(cells.into_iter().map(|cell| {
            div().px(px(7.)).py(px(2.)).rounded(px(5.)).bg(theme.shell).border_1().border_color(theme.hairline)
                .text_size(px(12.5)).line_height(px(18.)).child(cell.to_owned())
        })).into_any_element()
    } else if is_code {
        div().w_full().min_w_0().overflow_hidden().px(px(10.)).py(px(6.)).rounded(px(6.)).bg(pal.code_bg).border_1().border_color(theme.hairline)
            .font_family("JetBrains Mono").text_size(px(12.5)).line_height(px(19.)).child(text(block)).into_any_element()
    } else if block.kind == Kind::Todo {
        let checked = block.checked;
        div().w_full().min_w_0().overflow_hidden().flex().items_start().gap(px(8.))
            .child(div().mt(px(3.5)).size(px(15.)).flex_none().rounded(px(4.)).border_1()
                .border_color(if checked { theme.accent } else { theme.btn_border })
                .when(checked, |b| b.bg(theme.accent).flex().items_center().justify_center()
                    .child(icon(Icon::Check, px(10.), theme.mono_icon_colors(rgb(0xffffff).into())))))
            .child(div().flex_1().min_w_0().overflow_hidden().child(text(block))).into_any_element()
    } else if matches!(block.kind, Kind::Bullet | Kind::Numbered) {
        div().w_full().min_w_0().overflow_hidden().flex().items_baseline().gap(px(6.))
            .child(div().flex_none().text_color(theme.text_secondary).child(if block.kind == Kind::Bullet { "•" } else { "1." }))
            .child(div().flex_1().min_w_0().overflow_hidden().child(text(block))).into_any_element()
    } else if block.kind == Kind::Quote {
        div().w_full().min_w_0().overflow_hidden().border_l_2().border_color(theme.accent).pl(px(10.)).text_color(theme.text_secondary).child(text(block)).into_any_element()
    } else {
        div().w_full().min_w_0().overflow_hidden().child(text(block)).into_any_element()
    };
    div().relative().w_full().min_w_0().overflow_hidden()
        .when(change < 0 && !block.text.is_empty(), |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .py(px(2.)).text_size(px(font_size)).line_height(px(line_h))
        .when(is_heading, |row| row.font_weight(FontWeight(600.)))
        .when(change < 0, |row| row.text_color(theme.text_secondary))
        .child(content).into_any_element()
}

fn line_view(source: &str, header: bool, change: i8) -> AnyElement {
    let trimmed = source.trim_end_matches(['\r', '\n']).trim();
    if trimmed.is_empty() { return div().h(px(6.)).into_any_element(); }
    if let Some(row) = parse::parse_table_row(trimmed, header) {
        return table_row(&row, 0, Some(header || row.cells.iter().any(|c| c.header)), false).into_any_element();
    }
    let block = parse::parse_line_block(source);
    block_view(&block, change)
}

fn diff_counts(link: &ActivePlanLink) -> Option<(usize, usize)> {
    let lines = plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text).ok()?;
    let removed = lines.iter().filter(|l| l.change < 0 && l.included && parse::is_meaningful_line(&l.text)).map(|l| l.selection_index).collect::<HashSet<_>>().len();
    let added = lines.iter().filter(|l| l.change > 0 && l.included && parse::is_meaningful_line(&l.text)).map(|l| l.selection_index).collect::<HashSet<_>>().len();
    Some((removed, added))
}

fn context_indices(lines: &[plan_link::selection::DiffLine], first: usize, last: usize) -> HashSet<usize> {
    let mut ctx = HashSet::new();
    for (at, line) in lines.iter().enumerate().skip(first).take(last.saturating_sub(first) + 1) {
        if line.change == 0 || !parse::is_meaningful_line(&line.text) { continue; }
        let trimmed = line.text.trim();
        if trimmed.starts_with('|') {
            if let Some(hdr) = (1..at).rev().take_while(|&i| lines[i].text.trim().starts_with('|')).find(|&i| {
                parse::is_table_scaffolding(lines[i].text.trim()) && lines[i - 1].text.trim().starts_with('|')
            }).map(|i| i - 1) && lines[hdr].change == 0 {
                ctx.insert(hdr);
            }
        } else if trimmed.to_ascii_lowercase().starts_with("<tr") {
            if let Some(hdr) = (0..at).rev().take_while(|&i| !lines[i].text.trim().to_ascii_lowercase().starts_with("<table"))
                .filter(|&i| lines[i].text.trim().to_ascii_lowercase().starts_with("<tr") && parse::is_meaningful_line(&lines[i].text))
                .last() && lines[hdr].change == 0 {
                ctx.insert(hdr);
            }
        }
        if let Some(prev) = (first..at).rev().find(|&i| parse::is_meaningful_line(&lines[i].text))
            && lines[prev].change == 0 && lines[prev].text.trim_start().starts_with('#') {
            ctx.insert(prev);
        }
    }
    ctx
}

fn contents(link: &ActivePlanLink) -> impl IntoElement {
    let (theme, pal) = (crate::theme::Theme::ocean(), palette());
    let body = div().w_full().min_w_0().overflow_hidden().flex().flex_col().gap(px(8.));
    let Ok(lines) = plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text) else {
        return body.child(div().text_size(px(13.)).text_color(theme.text_secondary).child(tr("改动过大，仅支持完整预览和应用", "Change too large for individual selection; preview and apply all")))
            .children(link.after_blocks.iter().map(|block| block_view(block, 0)));
    };
    let (mut seen_removed, mut seen_added, mut seen_table_removed, mut seen_table_added) = (false, false, false, false);
    let first = lines.iter().position(|line| line.change != 0 && parse::is_meaningful_line(&line.text)).unwrap_or(lines.len());
    let last = lines.iter().rposition(|line| line.change != 0 && parse::is_meaningful_line(&line.text)).unwrap_or(first);
    let ctx_indices = context_indices(&lines, first, last);
    let visible: Vec<_> = lines.iter().filter(|line| {
        (line.change != 0 || ctx_indices.contains(&line.index)) && parse::is_meaningful_line(&line.text)
    }).collect();
    if visible.is_empty() {
        return body.child(div().py(px(6.)).text_size(px(13.)).text_color(theme.text_secondary).child(tr("未选择修改，保留原文", "No changes selected; original remains")));
    }
    let total_visible = visible.len();
    let header_bg: Hsla = if is_dark() { gpui::rgba(0xffffff08).into() } else { rgb(0xf5f3ed).into() };
    let mut list = div().w_full().min_w_0().rounded(px(10.)).border_1().border_color(theme.hairline).bg(theme.paper).overflow_hidden().flex().flex_col();
    for (pos, line) in visible.iter().enumerate() {
        let (trimmed, index, selection_index) = (line.text.trim(), line.index, line.selection_index);
        let is_table = parse::is_table_row_line(trimmed);
        let next_is_table = visible.get(pos + 1).is_some_and(|next| parse::is_table_row_line(next.text.trim()));
        let is_header = lines.get(index + 1).is_some_and(|next| {
            let v = next.text.trim(); v.starts_with('|') && parse::is_table_scaffolding(v)
        }) || trimmed.to_ascii_lowercase().contains("<th");
        let row_bg = if !line.included { theme.paper } else if line.change < 0 { pal.del_bg } else if line.change > 0 { pal.add_bg } else if is_header { header_bg } else { theme.paper };
        let left_bar = if !line.included && line.change != 0 { theme.btn_border } else if line.change < 0 { pal.del_border } else if line.change > 0 { pal.add_border } else { gpui::transparent_black() };
        let gutter_symbol = if line.change < 0 { "−" } else if line.change > 0 { "+" } else { "" };
        let gutter_color = if !line.included { theme.text_secondary } else if line.change < 0 { pal.del_text } else { pal.add_text };
        let show_bottom_border = pos + 1 < total_visible && (is_table || next_is_table || visible.get(pos + 1).is_some_and(|next| next.change != line.change));
        let row = div().id(SharedString::from(format!("ai-plan-diff-row-{index}"))).relative()
            .when(line.change < 0, |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
            .when(line.change < 0 && !seen_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-remove")))
            .when(line.change > 0 && !seen_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-add")))
            .when(is_table && line.change < 0 && !seen_table_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-remove")))
            .when(is_table && line.change > 0 && !seen_table_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-add")))
            .when(line.change != 0, |row| row.children(smoke_overlay::probe(format!("ai-plan-diff-{}-{index}", if line.change < 0 { "remove" } else { "add" }))))
            .flex().items_center().w_full().min_w_0().overflow_hidden().bg(row_bg).border_l(px(1.)).border_color(left_bar)
            .when(show_bottom_border, |row| row.border_b_1().border_color(theme.hairline))
            .when(line.change != 0, |row| row.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                cx.stop_propagation(); plan_link::toggle_diff(selection_index, cx);
            }))
            .child(div().w(px(26.)).flex_none().flex().items_center().justify_center()
                .font_family("JetBrains Mono").text_size(px(13.)).font_weight(FontWeight(600.)).text_color(gutter_color).child(gutter_symbol))
            .child(div().flex_1().min_w_0().overflow_hidden()
                .when(!is_table, |el| el.py(px(6.)).pr(px(8.)))
                .when(line.change > 0 && !line.included, |el| el.text_color(theme.text_secondary).line_through())
                .child(line_view(&line.text, is_header, if line.included { line.change } else { 0 })))
            .when(line.change != 0, |row| row.child(div().flex_none().px(px(10.)).flex().items_center().gap(px(6.))
                .when(!line.included, |el| el.child(div().px(px(5.)).py(px(1.)).rounded(px(4.)).bg(theme.shell).text_size(px(11.5)).text_color(theme.text_secondary)
                    .child(if line.change < 0 { tr("保留", "Kept") } else { tr("跳过", "Skipped") })))
                .child(div().size(px(16.)).flex_none().rounded(px(4.)).border_1()
                    .border_color(theme.btn_border)
                    .bg(if line.included { theme.selected } else { theme.paper }).flex().items_center().justify_center()
                    .when(line.included, |mark| mark.child(icon(Icon::Check, px(11.), theme.mono_icon_colors(theme.text_secondary)))))));
        list = list.child(row);
        seen_removed |= line.change < 0; seen_added |= line.change > 0;
        seen_table_removed |= is_table && line.change < 0; seen_table_added |= is_table && line.change > 0;
    }
    body.child(list)
}

pub(crate) fn editor_plan_review_card(link: ActivePlanLink) -> impl IntoElement {
    let (theme, pal) = (crate::theme::Theme::ocean(), palette());
    let title = if link.is_create { format!("{} · {}", tr("新建笔记", "New note"), link.page_label) } else {
        link.after_blocks.iter().chain(&link.before_blocks).find(|b| matches!(b.kind, Kind::H1 | Kind::H2 | Kind::H3) && !b.text.is_empty())
            .map(|b| b.text.clone()).unwrap_or_else(|| link.page_label.clone())
    };
    let (counts, accept, reject) = (diff_counts(&link), link.clone(), link.clone());
    div().relative().children(smoke_overlay::probe("ai-editor-plan-card"))
        .w_full().min_w_0().flex().flex_col().rounded(px(12.)).border_1().border_color(theme.hairline).bg(theme.paper).overflow_hidden()
        .child(div().w_full().min_w_0().overflow_hidden().px(px(14.)).py(px(10.)).bg(theme.shell).border_b_1().border_color(theme.hairline)
            .flex().items_center().justify_between().gap(px(10.))
            .child(div().flex_1().min_w_0().overflow_hidden().flex().items_center().gap(px(8.))
                .child(div().flex_none().px(px(7.)).py(px(2.)).rounded(px(5.))
                    .flex().items_center().gap(px(4.)).text_size(px(12.)).font_weight(FontWeight(500.)).text_color(theme.accent)
                    .child(icon(Icon::Sparkles, px(12.), theme.mono_icon_colors(theme.accent))).child(tr("AI 审阅", "AI Review")))
                .child(div().min_w_0().overflow_hidden().text_size(px(14.5)).font_weight(FontWeight(600.)).text_color(theme.text).child(title))
                .when_some(counts, |el, (removed, added)| el.child(div().relative().children(smoke_overlay::probe("ai-plan-diff-counts"))
                    .flex_none().flex().items_center().gap(px(4.)).font_family("JetBrains Mono").text_size(px(12.)).font_weight(FontWeight(500.))
                    .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(pal.del_badge).text_color(pal.del_text).child(format!("−{removed}")))
                    .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(pal.add_badge).text_color(pal.add_text).child(format!("+{added}")))))))
        .child(div().w_full().min_w_0().overflow_hidden().p(px(14.)).child(contents(&link)))
        .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-dock"))
            .w_full().min_w_0().overflow_hidden().px(px(14.)).py(px(10.)).bg(theme.shell).border_t_1().border_color(theme.hairline)
            .flex().items_center().justify_between().gap(px(12.))
            .child(div().flex_1().min_w_0().overflow_hidden().flex().items_center().gap(px(8.))
                .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-step"))
                    .flex_none().px(px(7.)).py(px(2.)).rounded(radius::CONTROL).text_color(theme.text_secondary).text_size(px(12.5)).font_weight(FontWeight(500.))
                    .child(if link.page_ops > 1 {
                        if crate::theme::is_en() { format!("{} changes on this page", link.page_ops) } else { format!("本页 {} 项修改", link.page_ops) }
                    } else if crate::theme::is_en() { format!("Change {} / {}", link.op_idx + 1, link.total_ops) }
                    else { format!("修改 {} / {}", link.op_idx + 1, link.total_ops) }))
                .child(div().flex_1().min_w_0().overflow_hidden().text_size(px(13.5)).line_height(px(20.)).text_color(theme.text_secondary).child(link.summary.clone())))
            .child(div().flex_none().flex().items_center().gap(px(6.))
                .child(pill("ai-plan-inline-reject", if link.is_create { tr("关闭预览", "Close preview") } else { tr("拒绝此项", "Reject change") }.into(), false, move |_, _, cx| plan_link::decide(reject.clone(), false, cx)))
                .child(pill("ai-plan-inline-accept", if link.total_ops > 1 { tr("接受全部修改", "Accept all changes") } else { tr("接受修改", "Accept change") }.into(), true, move |_, _, cx| plan_link::decide(accept.clone(), true, cx)))))
}
