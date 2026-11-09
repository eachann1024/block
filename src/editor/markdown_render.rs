//! Block and inline HTML fragments for the standalone HTML export.
use crate::editor::model::{Block, CellAlignment, Kind, MarkKind, Table};

pub(crate) fn esc(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for ch in text.chars() {
        match ch {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            _ => out.push(ch),
        }
    }
    out
}

fn safe_href(href: &str) -> bool {
    let lower = href.trim_start().to_ascii_lowercase();
    !["javascript:", "data:", "vbscript:"].iter().any(|scheme| lower.starts_with(scheme))
}

fn safe_style(style: &str) -> bool {
    let lower = style.to_ascii_lowercase();
    !["url(", "expression", "@import", "javascript:"].iter().any(|bad| lower.contains(bad))
}

/// Inline content: marks from `spans`, plus `u`/`mark`/`sup`/`sub`/`a`/`span` tags.
pub(crate) fn inline(block: &Block) -> String {
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
        let marks = block
            .spans
            .iter()
            .find(|s| s.start <= start && end <= s.end)
            .map_or(&[][..], |s| s.marks.as_slice());
        let mut open = String::new();
        let mut close = String::new();
        for tag in block.meta.inline.iter().filter(|t| t.start <= start && end <= t.end) {
            let attr = |name: &str| tag.attrs.iter().find(|(k, _)| k == name).map(|(_, v)| v.as_str());
            let head = match tag.name.as_str() {
                "a" => match attr("href").filter(|h| safe_href(h)) {
                    Some(href) => format!("<a href=\"{}\">", esc(href)),
                    None => "<a>".to_string(),
                },
                "span" => match attr("style").filter(|s| safe_style(s)) {
                    Some(style) => format!("<span style=\"{}\">", esc(style)),
                    None => "<span>".to_string(),
                },
                "u" | "mark" | "sup" | "sub" => format!("<{}>", tag.name),
                _ => continue,
            };
            open.push_str(&head);
            close.insert_str(0, &format!("</{}>", tag.name));
        }
        for (mark, name) in [
            (MarkKind::Bold, "strong"),
            (MarkKind::Italic, "em"),
            (MarkKind::Strike, "del"),
            (MarkKind::Code, "code"),
        ] {
            if marks.contains(&mark) {
                open.push_str(&format!("<{name}>"));
                close.insert_str(0, &format!("</{name}>"));
            }
        }
        out.push_str(&open);
        out.push_str(&esc(text).replace('\n', "<br>"));
        out.push_str(&close);
    }
    out
}

fn table(table: &Table) -> String {
    let mut out = String::from("<table>\n");
    for row in &table.rows {
        out.push_str("<tr>");
        for cell in &row.cells {
            let tag = if cell.header { "th" } else { "td" };
            let mut attrs = String::new();
            if cell.colspan > 1 {
                attrs.push_str(&format!(" colspan=\"{}\"", cell.colspan));
            }
            if cell.rowspan > 1 {
                attrs.push_str(&format!(" rowspan=\"{}\"", cell.rowspan));
            }
            match cell.alignment() {
                CellAlignment::Center => attrs.push_str(" style=\"text-align:center\""),
                CellAlignment::Right => attrs.push_str(" style=\"text-align:right\""),
                CellAlignment::Left => {}
            }
            out.push_str(&format!("<{tag}{attrs}>{}</{tag}>", inline(&cell.content)));
        }
        out.push_str("</tr>\n");
    }
    out.push_str("</table>\n");
    out
}

fn is_list(block: &Block) -> bool {
    matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo)
        && block.meta.table.is_none()
        && block.meta.code_language.is_none()
        && block.meta.raw_source.is_none()
        && block.meta.callout.is_none()
}

fn list(items: &[Block]) -> String {
    let (open, close) = match items[0].kind {
        Kind::Numbered => match items[0].meta.list_start.filter(|n| *n != 1) {
            Some(n) => (format!("<ol start=\"{n}\">"), "</ol>"),
            None => ("<ol>".to_string(), "</ol>"),
        },
        Kind::Todo => ("<ul class=\"todo-list\">".to_string(), "</ul>"),
        _ => ("<ul>".to_string(), "</ul>"),
    };
    let mut out = format!("{open}\n");
    for item in items {
        let style = match item.meta.indent {
            0 => String::new(),
            n => format!(" style=\"margin-left:{}em\"", n as f32 * 1.5),
        };
        if item.kind == Kind::Todo {
            let checked = if item.checked { " checked" } else { "" };
            out.push_str(&format!(
                "<li class=\"todo\"{style}><input type=\"checkbox\" disabled{checked}> {}</li>\n",
                inline(item)
            ));
        } else {
            out.push_str(&format!("<li{style}>{}</li>\n", inline(item)));
        }
    }
    out.push_str(close);
    out.push('\n');
    out
}

pub(crate) fn render_blocks(blocks: &[Block]) -> String {
    let mut out = String::new();
    let mut at = 0;
    while at < blocks.len() {
        let block = &blocks[at];
        at += 1;
        if let Some(t) = &block.meta.table {
            out.push_str(&table(t));
        } else if let Some(language) = &block.meta.code_language {
            let class = match language.as_str() {
                "" | "yaml-frontmatter" => String::new(),
                other => format!(" class=\"language-{}\"", esc(other)),
            };
            out.push_str(&format!("<pre><code{class}>{}</code></pre>\n", esc(&block.text)));
        } else if let Some(image) = crate::editor::image_source::spec_of(block) {
            if safe_href(&image.src) {
                out.push_str(&format!("<p><img src=\"{}\" alt=\"{}\"></p>\n", esc(&image.src), esc(&image.alt)));
            } else {
                out.push_str(&format!("<p>{}</p>\n", esc(&image.alt)));
            }
        } else if block.meta.raw_source.is_some() {
            if is_rule(block) {
                out.push_str("<hr>\n");
            } else {
                out.push_str(&format!("<pre>{}</pre>\n", esc(&block.text)));
            }
        } else if let Some(callout) = block.meta.callout {
            out.push_str(&format!(
                "<div class=\"callout callout-{}\"><div class=\"callout-title\">{}</div><p>{}</p></div>\n",
                callout.tag().to_ascii_lowercase(),
                callout.tag(),
                inline(block)
            ));
        } else if is_list(block) {
            let start = at - 1;
            while at < blocks.len() && is_list(&blocks[at]) && blocks[at].kind == block.kind {
                at += 1;
            }
            out.push_str(&list(&blocks[start..at]));
        } else if block.kind == Kind::Quote {
            out.push_str(&format!("<blockquote><p>{}</p></blockquote>\n", inline(block)));
        } else if let Some(level) = heading_level(block) {
            out.push_str(&format!("<h{level}>{}</h{level}>\n", inline(block)));
        } else {
            out.push_str(&format!("<p>{}</p>\n", inline(block)));
        }
    }
    out
}

/// Heading level 1..=6; H4-H6 are H3 blocks carrying the exact level.
pub(crate) fn heading_level(block: &Block) -> Option<u8> {
    match block.kind {
        Kind::H1 => Some(1),
        Kind::H2 => Some(2),
        Kind::H3 => Some(block.meta.heading_level.filter(|l| (4..=6).contains(l)).unwrap_or(3)),
        _ => None,
    }
}

/// An unsupported-source block that is just a thematic break.
pub(crate) fn is_rule(block: &Block) -> bool {
    let hit = |line: &str| matches!(line.trim(), "---" | "***" | "___");
    block.meta.raw_source.as_deref().is_some_and(|raw| hit(raw) || hit(&block.text))
}
