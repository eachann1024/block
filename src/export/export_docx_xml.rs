//! WordprocessingML body: paragraphs, runs, lists and tables for the .docx export.
use super::export_docx::HEADING_SIZES;
use super::format::{heading_level, is_rule};
use super::palette::Palette;
use super::{Cancellation, ExportError, RenderOptions};
use crate::editor::model::{Block, CellAlignment, Kind, MarkKind, Table};
use cosmic_text::Color;

const PAGE_WIDTH: u32 = 11906;

pub(super) fn hex(color: Color) -> String {
    format!("{:02X}{:02X}{:02X}", color.r(), color.g(), color.b())
}

fn esc(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for ch in text.chars() {
        match ch {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\t' | '\n' => out.push(ch),
            c if (c as u32) < 0x20 => {}
            c => out.push(c),
        }
    }
    out
}

/// Run content: tabs and line breaks become real Word elements.
fn text_xml(text: &str) -> String {
    let mut out = String::new();
    for (index, line) in text.split('\n').enumerate() {
        if index > 0 {
            out.push_str("<w:br/>");
        }
        for (part, piece) in line.split('\t').enumerate() {
            if part > 0 {
                out.push_str("<w:tab/>");
            }
            if !piece.is_empty() {
                out.push_str(&format!("<w:t xml:space=\"preserve\">{}</w:t>", esc(piece)));
            }
        }
    }
    out
}

#[derive(Clone, Copy, Default)]
struct Base {
    bold: bool,
    size: Option<u32>,
}

fn css_color(style: &str) -> Option<String> {
    let value = style.split(';').find_map(|part| {
        let (name, value) = part.split_once(':')?;
        (name.trim() == "color").then(|| value.trim().trim_start_matches('#').to_string())
    })?;
    (value.len() == 6 && value.chars().all(|c| c.is_ascii_hexdigit())).then(|| value.to_ascii_uppercase())
}

fn runs(block: &Block, pal: &Palette, base: Base) -> String {
    let mut cuts = vec![0, block.text.len()];
    for span in &block.spans {
        cuts.extend([span.start, span.end]);
    }
    for tag in &block.meta.inline {
        cuts.extend([tag.start, tag.end]);
    }
    cuts.sort_unstable();
    cuts.dedup();
    let mut out = String::new();
    for pair in cuts.windows(2) {
        let (start, end) = (pair[0], pair[1]);
        let Some(text) = block.text.get(start..end) else { continue };
        let marks = block.spans.iter().find(|s| s.start <= start && end <= s.end).map_or(&[][..], |s| &s.marks[..]);
        let tags: Vec<_> = block.meta.inline.iter().filter(|t| t.start <= start && end <= t.end).collect();
        let has = |name: &str| tags.iter().any(|t| t.name == name);
        let mut color = None;
        if has("a") {
            color = Some(hex(pal.accent));
        }
        for tag in tags.iter().filter(|t| t.name == "span") {
            if let Some((_, style)) = tag.attrs.iter().find(|(k, _)| k == "style") {
                color = css_color(style).map(|c| hex(pal.fg(parse(&c)))).or(color);
            }
        }
        let mut rpr = String::new();
        if marks.contains(&MarkKind::Code) {
            rpr.push_str("<w:rFonts w:ascii=\"Menlo\" w:hAnsi=\"Menlo\" w:cs=\"Consolas\" w:eastAsia=\"Menlo\"/>");
        }
        if base.bold || marks.contains(&MarkKind::Bold) {
            rpr.push_str("<w:b/>");
        }
        if marks.contains(&MarkKind::Italic) {
            rpr.push_str("<w:i/>");
        }
        if marks.contains(&MarkKind::Strike) {
            rpr.push_str("<w:strike/>");
        }
        if let Some(color) = color {
            rpr.push_str(&format!("<w:color w:val=\"{color}\"/>"));
        }
        if let Some(size) = base.size {
            rpr.push_str(&format!("<w:sz w:val=\"{size}\"/><w:szCs w:val=\"{size}\"/>"));
        }
        if has("mark") {
            rpr.push_str("<w:highlight w:val=\"yellow\"/>");
        }
        if has("u") || has("a") {
            rpr.push_str("<w:u w:val=\"single\"/>");
        }
        if marks.contains(&MarkKind::Code) {
            rpr.push_str(&format!("<w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"{}\"/>", hex(pal.code_bg)));
        }
        if has("sup") {
            rpr.push_str("<w:vertAlign w:val=\"superscript\"/>");
        } else if has("sub") {
            rpr.push_str("<w:vertAlign w:val=\"subscript\"/>");
        }
        out.push_str(&format!("<w:r><w:rPr>{rpr}</w:rPr>{}</w:r>", text_xml(text)));
    }
    out
}

fn parse(hex: &str) -> Color {
    let byte = |at: usize| u8::from_str_radix(&hex[at..at + 2], 16).unwrap_or(0);
    Color::rgb(byte(0), byte(2), byte(4))
}

fn para(style: Option<&str>, props: &str, content: &str) -> String {
    let style = style.map_or(String::new(), |s| format!("<w:pStyle w:val=\"{s}\"/>"));
    format!("<w:p><w:pPr>{style}{props}</w:pPr>{content}</w:p>")
}

fn marker_run(text: &str) -> String {
    format!("<w:r><w:t xml:space=\"preserve\">{text}</w:t></w:r>")
}

fn cell(width: u32, span: usize, merge: Option<&str>, fill: Option<String>, content: String) -> String {
    let span = if span > 1 { format!("<w:gridSpan w:val=\"{span}\"/>") } else { String::new() };
    let merge = merge.map_or(String::new(), |m| format!("<w:vMerge{m}/>"));
    let fill = fill.map_or(String::new(), |f| format!("<w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"{f}\"/>"));
    format!("<w:tc><w:tcPr><w:tcW w:w=\"{width}\" w:type=\"dxa\"/>{span}{merge}{fill}</w:tcPr>{content}</w:tc>")
}

/// Emit continuation cells for columns covered by an earlier rowspan, and blanks for gaps.
fn fill(pending: &mut [(usize, usize)], at: &mut usize, upto: usize, each: u32, out: &mut String) {
    while *at < upto.min(pending.len()) {
        let (left, span) = pending[*at];
        if left > 0 {
            pending[*at].0 -= 1;
            out.push_str(&cell(each * span as u32, span, Some(""), None, para(None, "", "")));
            *at += span;
        } else {
            out.push_str(&cell(each, 1, None, None, para(None, "", "")));
            *at += 1;
        }
    }
}

fn table(table: &Table, pal: &Palette, content_width: u32) -> String {
    let columns = table.columns.max(1);
    let each = (content_width / columns as u32).max(1);
    let line = hex(pal.table_border);
    let sides = ["top", "left", "bottom", "right", "insideH", "insideV"]
        .map(|side| format!("<w:{side} w:val=\"single\" w:sz=\"4\" w:space=\"0\" w:color=\"{line}\"/>"))
        .concat();
    let mut out = format!(
        "<w:tbl><w:tblPr><w:tblW w:w=\"{content_width}\" w:type=\"dxa\"/><w:tblBorders>{sides}</w:tblBorders><w:tblLayout w:type=\"fixed\"/><w:tblCellMar><w:left w:w=\"100\" w:type=\"dxa\"/><w:right w:w=\"100\" w:type=\"dxa\"/></w:tblCellMar></w:tblPr><w:tblGrid>{}</w:tblGrid>",
        format!("<w:gridCol w:w=\"{each}\"/>").repeat(columns)
    );
    let mut pending = vec![(0usize, 1usize); columns];
    for row in &table.rows {
        out.push_str("<w:tr>");
        let mut at = 0;
        let mut cells: Vec<_> = row.cells.iter().collect();
        cells.sort_by_key(|c| c.column);
        for item in cells {
            fill(&mut pending, &mut at, item.column, each, &mut out);
            if at != item.column || item.column >= columns {
                continue;
            }
            let span = item.colspan.clamp(1, columns - at);
            if item.rowspan > 1 {
                pending[at] = (item.rowspan - 1, span);
            }
            let merge = (item.rowspan > 1).then_some(" w:val=\"restart\"");
            let jc = match item.alignment() {
                CellAlignment::Left => "",
                CellAlignment::Center => "<w:jc w:val=\"center\"/>",
                CellAlignment::Right => "<w:jc w:val=\"right\"/>",
            };
            let base = Base { bold: item.header, size: None };
            let content = para(None, &format!("<w:spacing w:after=\"40\"/>{jc}"), &runs(&item.content, pal, base));
            let fill = item.header.then(|| hex(pal.code_bg));
            out.push_str(&cell(each * span as u32, span, merge, fill, content));
            at += span;
        }
        fill(&mut pending, &mut at, columns, each, &mut out);
        out.push_str("</w:tr>");
    }
    out.push_str("</w:tbl>");
    out
}

pub(super) fn body(
    blocks: &[Block],
    options: &RenderOptions,
    pal: &Palette,
    cancel: &Cancellation,
) -> Result<String, ExportError> {
    let side = if options.card { 2268 } else { 1304 };
    let content_width = PAGE_WIDTH - side * 2;
    let mut out = String::new();
    let title = options.title.trim();
    if options.show_title && !title.is_empty() {
        out.push_str(&para(Some("Title"), "", &format!("<w:r>{}</w:r>", text_xml(title))));
    }
    let mut counters: Vec<u64> = Vec::new();
    for block in blocks {
        cancel.check()?;
        let indent = block.meta.indent;
        if !matches!(block.kind, Kind::Numbered | Kind::Bullet | Kind::Todo) {
            counters.clear();
        }
        if let Some(t) = &block.meta.table {
            out.push_str(&table(t, pal, content_width));
            out.push_str("<w:p/>");
        } else if block.meta.code_language.is_some() || (block.meta.raw_source.is_some() && !is_rule(block)) {
            let content = format!("<w:r>{}</w:r>", text_xml(&block.text));
            out.push_str(&para(Some("CodeBlock"), "", &content));
        } else if is_rule(block) {
            let line = format!("<w:pBdr><w:bottom w:val=\"single\" w:sz=\"6\" w:space=\"1\" w:color=\"{}\"/></w:pBdr>", hex(pal.divider));
            out.push_str(&para(None, &line, ""));
        } else if let Some(callout) = block.meta.callout {
            let tag = marker_run(&format!("[!{}] ", callout.tag()));
            out.push_str(&para(Some("Quote"), "", &(tag + &runs(block, pal, Base::default()))));
        } else if block.kind == Kind::Quote {
            out.push_str(&para(Some("Quote"), "", &runs(block, pal, Base::default())));
        } else if let Some(level) = heading_level(block) {
            let base = Base { bold: true, size: Some(HEADING_SIZES[level as usize - 1]) };
            out.push_str(&para(Some(&format!("Heading{level}")), "", &runs(block, pal, base)));
        } else if matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) {
            counters.truncate(indent + 1);
            counters.resize(indent + 1, 0);
            let marker = match block.kind {
                Kind::Numbered => {
                    let slot = &mut counters[indent];
                    *slot = if *slot == 0 { block.meta.list_start.unwrap_or(1) } else { *slot + 1 };
                    format!("{}. ", *slot)
                }
                Kind::Todo => (if block.checked { "☑ " } else { "☐ " }).to_string(),
                _ => "• ".to_string(),
            };
            let left = 360 * (indent + 1);
            let props = format!("<w:ind w:left=\"{left}\" w:hanging=\"360\"/>");
            out.push_str(&para(None, &props, &(marker_run(&marker) + &runs(block, pal, Base::default()))));
        } else {
            let props = if indent > 0 { format!("<w:ind w:left=\"{}\"/>", 360 * indent) } else { String::new() };
            out.push_str(&para(None, &props, &runs(block, pal, Base::default())));
        }
    }
    if options.footer {
        let props = format!("<w:pBdr><w:top w:val=\"single\" w:sz=\"4\" w:space=\"8\" w:color=\"{}\"/></w:pBdr><w:spacing w:before=\"480\"/>", hex(pal.divider));
        let run = format!("<w:r><w:rPr><w:color w:val=\"{}\"/><w:sz w:val=\"18\"/></w:rPr><w:t>Thinkly</w:t></w:r>", hex(pal.secondary));
        out.push_str(&para(None, &props, &run));
    }
    out.push_str(&format!(
        "<w:sectPr><w:pgSz w:w=\"{PAGE_WIDTH}\" w:h=\"16838\"/><w:pgMar w:top=\"1440\" w:right=\"{side}\" w:bottom=\"1440\" w:left=\"{side}\" w:header=\"720\" w:footer=\"720\" w:gutter=\"0\"/></w:sectPr>"
    ));
    Ok(out)
}
