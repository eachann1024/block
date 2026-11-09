//! HTML projection using the same theme metrics, semantic styles and media as the preview.
use super::export_html::css_color as color;
use super::palette::Palette;
use super::{Cancellation, ExportError, RenderOptions, document, style};
pub(super) use crate::editor::markdown::render::esc;
use crate::editor::model::{Block, Kind, Table};

pub(super) fn inline(block: &Block, pal: &Palette) -> String {
    document::segments(block, pal)
        .into_iter()
        .map(|s| {
            let mut css = String::new();
            if s.bold {
                css.push_str("font-weight:700;");
            }
            if s.italic {
                css.push_str("font-style:italic;");
            }
            let lines = [
                s.underline.then_some("underline"),
                s.strike.then_some("line-through"),
            ]
            .into_iter()
            .flatten()
            .collect::<Vec<_>>()
            .join(" ");
            if !lines.is_empty() {
                css.push_str(&format!("text-decoration:{lines};"));
            }
            if s.code && s.text.is_ascii() {
                css.push_str(&format!(
                    "font-family:\"{}\";",
                    document::font_family("monospace")
                ));
            }
            if let Some(c) = s.color {
                css.push_str(&format!("color:{};", color(c)));
            }
            if let Some(c) = s.background {
                css.push_str(&format!("background:{};", color(c)));
            }
            if let Some(v) = s.script {
                css.push_str(&format!(
                    "vertical-align:{};font-size:.75em;",
                    if v == "superscript" { "super" } else { "sub" }
                ));
            }
            let text = format!(
                "<span style=\"{}\">{}</span>",
                esc(&css),
                esc(s.text).replace('\n', "<br>")
            );
            s.href.map_or_else(
                || text.clone(),
                |h| format!("<a href=\"{}\">{text}</a>", esc(h)),
            )
        })
        .collect()
}

fn table(
    table: &Table,
    block: &Block,
    options: &RenderOptions,
    width: f32,
) -> Result<String, ExportError> {
    use crate::editor::model::CellAlignment;
    use crate::editor::table_layout::{FONT_SIZE, LINE_HEIGHT, MIN_ROW, PAD_X, PAD_Y};
    let pal = options.theme.palette();
    let (columns, scale) = document::table_columns(table, &options.source, block.id, width)?;
    let total: f32 = columns.iter().sum();
    let family = if options.source.font_family.is_empty() {
        options.theme.body_family()
    } else {
        &options.source.font_family
    };
    let mut out = format!(
        "<table style=\"width:{total}px;font-family:&quot;{}&quot;;font-size:{}px;line-height:{}px\"><colgroup>",
        esc(family),
        FONT_SIZE * scale,
        LINE_HEIGHT * scale
    );
    for w in columns {
        out.push_str(&format!("<col style=\"width:{w}px\">"));
    }
    out.push_str("</colgroup>");
    let geometry = options
        .source
        .block(block.id)
        .and_then(|b| b.table.as_ref());
    for (r, row) in table.rows.iter().enumerate() {
        let height = geometry
            .and_then(|g| g.rows.get(r))
            .copied()
            .unwrap_or(MIN_ROW)
            * scale;
        out.push_str(&format!("<tr style=\"height:{height}px\">"));
        for cell in &row.cells {
            let tag = if cell.header { "th" } else { "td" };
            let align = match super::table::alignment(&cell.attrs)
                .or_else(|| super::table::alignment(&table.attrs))
            {
                Some(cosmic_text::Align::Center) => "center",
                Some(cosmic_text::Align::Right) => "right",
                Some(_) => "left",
                None => match table
                    .alignments
                    .get(cell.column)
                    .copied()
                    .unwrap_or_default()
                {
                    CellAlignment::Left => "left",
                    CellAlignment::Center => "center",
                    CellAlignment::Right => "right",
                },
            };
            let mut css = format!(
                "padding:{}px {}px;text-align:{align};",
                PAD_Y * scale,
                PAD_X * scale
            );
            if let Some(c) = style::cell_background(cell, table, &pal) {
                css.push_str(&format!("background:{};", color(c)));
            }
            if let Some(c) = style::cell_foreground(cell, table, &pal) {
                css.push_str(&format!("color:{};", color(pal.fg(c))));
            }
            if (geometry.is_none() && cell.header)
                || style::bold(&table.attrs)
                || style::bold(&cell.attrs)
            {
                css.push_str("font-weight:700;");
            }
            if style::italic(&table.attrs) || style::italic(&cell.attrs) {
                css.push_str("font-style:italic;");
            }
            out.push_str(&format!(
                "<{tag} colspan=\"{}\" rowspan=\"{}\" style=\"{}\">{}</{tag}>",
                cell.colspan,
                cell.rowspan,
                esc(&css),
                inline(&cell.content, &pal)
            ));
        }
        out.push_str("</tr>");
    }
    out.push_str("</table>");
    Ok(out)
}

pub(super) fn render_blocks(
    blocks: &[Block],
    options: &RenderOptions,
    cancel: &Cancellation,
) -> Result<String, ExportError> {
    let theme = options.theme;
    let pal = theme.palette();
    let (_, right, _, left) = theme.margins();
    let width = super::layout::PAGE_WIDTH - right - left;
    let blocks = document::blocks(blocks, options);
    let numbers = crate::editor::model::list_numbers(blocks.iter());
    let gutters = document::list_gutters(blocks, options)?;
    let mut out = String::new();
    for (block, number) in blocks.iter().zip(numbers) {
        cancel.check()?;
        let indent = document::indent(block, width);
        let available = width - indent;
        let mut picture = if let Some(spec) = crate::editor::image_source::spec_of(block) {
            Some(super::media::load(
                &spec,
                block.id,
                &options.source,
                0.,
                0.,
                available,
                cancel,
            )?)
        } else {
            super::media::preview(block, options.theme, 0., 0., available, cancel)?
        };
        if let Some(p) = picture.take() {
            let data = document::base64(&p.png()?);
            out.push_str(&format!("<div style=\"margin-left:{indent}px;margin-bottom:10px;height:{}px\"><img alt=\"{}\" src=\"data:image/png;base64,{data}\" style=\"display:block;margin-left:{}px;width:{}px;height:{}px\"></div>", p.height, esc(&block.text), p.x, p.width, p.height));
            if !options
                .source
                .block(block.id)
                .is_some_and(|b| b.show_source)
                || crate::editor::image_source::spec_of(block).is_some()
            {
                continue;
            }
        }
        if let Some(t) = &block.meta.table {
            out.push_str(&format!(
                "<div style=\"margin-left:{indent}px;margin-bottom:10px\">{}</div>",
                table(t, block, options, available)?
            ));
            continue;
        }
        let code = block.meta.code_language.is_some();
        let rule = super::format::is_rule(block);
        let (size, line) = document::block_metrics(block, options, available)?;
        let (gutter, pad_x, top, bottom) = style::block_box_metrics(
            theme,
            block.kind,
            code,
            number.is_some(),
            block.meta.indent,
            &gutters,
        );
        let gap = style::block_gap_after(block.kind, code);
        if rule {
            out.push_str(&format!("<div style=\"margin-left:{indent}px;height:{line}px;margin-bottom:{gap}px;display:flex;align-items:center\"><hr></div>"));
            continue;
        }
        let fg = color(style::row_text_color(block.kind, code, &pal));
        let mut css = format!(
            "--line:{line}px;margin-left:{indent}px;margin-bottom:{gap}px;padding:{top}px {pad_x}px {bottom}px;font-size:{size}px;line-height:{line}px;color:{fg};"
        );
        let family = if code {
            "monospace"
        } else if super::format::heading_level(block).is_some() {
            theme.heading_family()
        } else {
            theme.body_family()
        };
        css.push_str(&format!(
            "font-family:&quot;{}&quot;;",
            esc(&document::font_family(family))
        ));
        if block.kind == Kind::H2 {
            match theme {
                super::ExportTheme::Press => {
                    css.push_str(&format!("box-shadow:inset 0 -1px {};", color(pal.divider)))
                }
                super::ExportTheme::Poster => css.push_str(&format!("position:relative;")),
                _ => {}
            }
        }
        if super::format::heading_level(block).is_some() {
            css.push_str(if block.meta.heading_level == Some(6) {
                "font-weight:500;"
            } else {
                "font-weight:700;"
            });
        }
        let radius = super::decor_blocks::radius(theme);
        if code {
            css.push_str(&format!(
                "background:{};box-shadow:inset 0 0 0 1px {};border-radius:{radius}px;",
                color(pal.code_bg),
                color(pal.code_border)
            ));
        }
        if block.kind == Kind::Quote {
            if theme == super::ExportTheme::Press {
                css.push_str(&format!(
                    "box-shadow:inset 0 1px {},inset 0 -1px {};",
                    color(pal.quote_bar),
                    color(pal.quote_bar)
                ));
            } else {
                css.push_str(&format!(
                    "background:{};border-radius:{radius}px;box-shadow:inset 4px 0 {};",
                    color(pal.quote_bg),
                    color(pal.quote_bar)
                ));
            }
        }
        let mut label = block
            .meta
            .code_language
            .as_deref()
            .filter(|s| !s.is_empty())
            .map(|s| {
                if matches!(s, "yaml-frontmatter" | "frontmatter") {
                    "yaml"
                } else {
                    s
                }
            });
        let mut label_color = pal.secondary;
        if let Some(kind) = block.meta.callout {
            let (bg, border, accent) = pal.callout_colors(kind);
            css.push_str(&format!(
                "background:{};border-radius:8px;box-shadow:inset 4px 0 {},inset 0 0 0 1px {};",
                color(bg),
                color(accent),
                color(border)
            ));
            label = Some(crate::ui::callout_controls::label(kind));
            label_color = accent;
        }
        out.push_str(&format!("<div class=\"block\" style=\"{css}\">"));
        if let Some(label) = label {
            let extra = if block.meta.callout.is_some() {
                26.
            } else {
                0.
            };
            out.push_str(&format!("<div style=\"font:13px/18.2px &quot;{}&quot;;color:{};margin-bottom:{}px\">{}</div>", esc(theme.body_family()), color(label_color), 4. + extra, esc(label)));
        }
        if matches!(block.kind, Kind::Numbered | Kind::Bullet | Kind::Todo) && !code {
            let marker = match block.kind {
                Kind::Numbered => format!("{}.", number.unwrap_or(1)),
                Kind::Todo => if block.checked { "☑" } else { "☐" }.into(),
                _ => if matches!(
                    theme,
                    super::ExportTheme::Poster
                        | super::ExportTheme::Terminal
                        | super::ExportTheme::Riso
                ) {
                    "▪"
                } else {
                    "•"
                }
                .into(),
            };
            out.push_str(&format!("<div style=\"display:flex\"><span style=\"flex:0 0 {gutter}px;color:{}\">{marker}</span><div style=\"min-width:0;flex:1\">{}</div></div>", color(pal.accent), inline(block, &pal)));
        } else if code {
            out.push_str(&format!(
                "<div class=\"block-text\" style=\"white-space:pre-wrap\">{}</div>",
                esc(&block.text)
            ));
        } else {
            let text = inline(block, &pal);
            if block.kind == Kind::H2 && theme == super::ExportTheme::Riso {
                let ink =
                    cosmic_text::Color::rgba(pal.accent.r(), pal.accent.g(), pal.accent.b(), 80);
                out.push_str(&format!("<div class=\"block-text\"><span style=\"background:linear-gradient(transparent 55%,{} 55%,{} 90%,transparent 90%);box-decoration-break:clone\">{text}</span></div>",color(ink),color(ink)));
            } else {
                if block.kind == Kind::H2 && theme == super::ExportTheme::Poster {
                    out.push_str(&format!("<span style=\"position:absolute;left:-16px;top:{}px;width:5px;height:{}px;background:{}\"></span>", top+line*0.19,line*0.62,color(pal.accent)));
                }
                out.push_str(&format!("<div class=\"block-text\">{text}</div>"));
            }
        }
        out.push_str("</div>\n");
    }
    Ok(out)
}
