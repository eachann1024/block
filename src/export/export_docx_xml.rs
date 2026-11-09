//! WordprocessingML paragraphs, rich runs, native tables and media.
use super::export_docx_assets::Assets;
use super::format::{heading_level, is_rule};
use super::palette::Palette;
use super::{Cancellation, ExportError, RenderOptions, WATERMARK_LABEL, document, style};
use crate::editor::model::{Block, Kind};
use cosmic_text::Color;

pub(super) fn hex(color: Color) -> String {
    format!("{:02X}{:02X}{:02X}", color.r(), color.g(), color.b())
}
pub(super) fn twips(px: f32) -> u32 {
    (px * 15.).round().max(0.) as u32
}
pub(super) fn size(px: f32) -> String {
    let half = (px * 1.5).round() as u32;
    format!("<w:sz w:val=\"{half}\"/><w:szCs w:val=\"{half}\"/>")
}
pub(super) fn font(family: &str) -> String {
    let family = esc(&document::font_family(family));
    format!(
        "<w:rFonts w:ascii=\"{family}\" w:hAnsi=\"{family}\" w:eastAsia=\"{family}\" w:cs=\"{family}\"/>"
    )
}
pub(super) fn spacing(before: f32, after: f32, line: f32) -> String {
    format!(
        "<w:spacing w:before=\"{}\" w:after=\"{}\" w:line=\"{}\" w:lineRule=\"exact\"/>",
        twips(before),
        twips(after),
        twips(line)
    )
}
pub(super) fn esc(text: &str) -> String {
    crate::editor::markdown::render::esc(
        &text
            .chars()
            .filter(|c| *c >= ' ' || matches!(c, '\t' | '\n'))
            .collect::<String>(),
    )
}
pub(super) fn text_xml(text: &str) -> String {
    let mut out = String::new();
    for (index, line) in text.split('\n').enumerate() {
        if index > 0 {
            out.push_str("<w:br/>");
        }
        for (index, piece) in line.split('\t').enumerate() {
            if index > 0 {
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
pub(super) struct Base<'a> {
    pub bold: bool,
    pub italic: bool,
    pub family: Option<&'a str>,
    pub px: Option<f32>,
    pub color: Option<Color>,
}
pub(super) fn runs(block: &Block, pal: &Palette, base: Base<'_>, assets: &mut Assets) -> String {
    document::segments(block, pal)
        .into_iter()
        .map(|s| {
            let mut rpr = String::new();
            if let Some(family) = if s.code && s.text.is_ascii() {
                Some("monospace")
            } else {
                base.family
            } {
                rpr.push_str(&font(family));
            }
            if base.bold || s.bold {
                rpr.push_str("<w:b/><w:bCs/>");
            }
            if base.italic || s.italic {
                rpr.push_str("<w:i/><w:iCs/>");
            }
            if s.strike {
                rpr.push_str("<w:strike/>");
            }
            if let Some(c) = s.color.or(base.color.map(|c| pal.fg(c))) {
                rpr.push_str(&format!("<w:color w:val=\"{}\"/>", hex(c)));
            }
            if let Some(px) = base.px {
                rpr.push_str(&size(px));
            }
            if s.underline {
                rpr.push_str("<w:u w:val=\"single\"/>");
            }
            if let Some(bg) = s.background {
                rpr.push_str(&shade(bg));
            }
            if let Some(script) = s.script {
                rpr.push_str(&format!("<w:vertAlign w:val=\"{script}\"/>"));
            }
            let run = format!("<w:r><w:rPr>{rpr}</w:rPr>{}</w:r>", text_xml(s.text));
            s.href.map_or_else(
                || run.clone(),
                |href| {
                    format!(
                        "<w:hyperlink r:id=\"{}\">{run}</w:hyperlink>",
                        assets.link(href)
                    )
                },
            )
        })
        .collect()
}
pub(super) fn para(style: Option<&str>, props: &str, content: &str) -> String {
    let style = style.map_or(String::new(), |s| format!("<w:pStyle w:val=\"{s}\"/>"));
    format!("<w:p><w:pPr>{style}{props}</w:pPr>{content}</w:p>")
}
pub(super) fn spacer(height: f32) -> String {
    para(
        None,
        &spacing(0., 0., height),
        "<w:r><w:rPr><w:sz w:val=\"2\"/></w:rPr></w:r>",
    )
}
pub(super) fn shade(c: Color) -> String {
    format!(
        "<w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"{}\"/>",
        hex(c)
    )
}
pub(super) fn border(side: &str, c: Color, px: f32) -> String {
    format!(
        "<w:{side} w:val=\"single\" w:sz=\"{}\" w:space=\"0\" w:color=\"{}\"/>",
        (px * 6.).round() as u32,
        hex(c)
    )
}
pub(super) fn marker(text: &str, c: Color) -> String {
    format!(
        "<w:r><w:rPr><w:color w:val=\"{}\"/></w:rPr>{}</w:r>",
        hex(c),
        text_xml(text)
    )
}
pub(super) fn body(
    blocks: &[Block],
    options: &RenderOptions,
    pal: &Palette,
    cancel: &Cancellation,
    assets: &mut Assets,
) -> Result<String, ExportError> {
    let theme = options.theme;
    let (top, right, bottom, left) = theme.margins();
    let width = super::layout::PAGE_WIDTH - left - right;
    let mut out = String::new();
    if options.show_title {
        out.push_str(&super::export_docx_chrome::header(
            options, pal, width, assets,
        )?);
    }
    let blocks = document::blocks(blocks, options);
    let numbers = crate::editor::model::list_numbers(blocks.iter());
    let gutters = document::list_gutters(blocks, options)?;
    for (block, number) in blocks.iter().zip(numbers) {
        cancel.check()?;
        let indent = document::indent(block, width);
        let available = width - indent;
        let picture = if let Some(spec) = crate::editor::image_source::spec_of(block) {
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
            super::media::preview(block, theme, 0., 0., available, cancel)?
        };
        if let Some(p) = picture {
            out.push_str(&assets.picture(&p, indent, &block.text)?);
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
            out.push_str(&super::export_docx_table::table(
                t, block, options, available, assets,
            )?);
            out.push_str(&spacer(10.));
            continue;
        }
        let code = block.meta.code_language.is_some();
        let (px, line) = document::block_metrics(block, options, available)?;
        let gap = style::block_gap_after(block.kind, code);
        let (gutter, pad_x, pad_top, pad_bottom) = style::block_box_metrics(
            theme,
            block.kind,
            code,
            number.is_some(),
            block.meta.indent,
            &gutters,
        );
        if is_rule(block) {
            let props = format!(
                "<w:pBdr>{}</w:pBdr>{}",
                border("bottom", pal.divider, 1.),
                spacing(line / 2., gap + line / 2., 1.)
            );
            out.push_str(&para(None, &props, ""));
            continue;
        }
        let family = if code {
            "monospace"
        } else if heading_level(block).is_some() {
            theme.heading_family()
        } else {
            theme.body_family()
        };
        let base = Base {
            family: Some(family),
            px: Some(px),
            color: Some(style::row_text_color(block.kind, code, pal)),
            bold: heading_level(block).is_some() && block.meta.heading_level != Some(6),
            ..Base::default()
        };
        let text = if code {
            format!(
                "<w:r><w:rPr>{}{}</w:rPr>{}</w:r>",
                font(family),
                size(px),
                text_xml(&block.text)
            )
        } else {
            runs(block, pal, base, assets)
        };
        if code || block.kind == Kind::Quote || block.meta.callout.is_some() {
            let (bg, borders) = if let Some(kind) = block.meta.callout {
                let (bg, border_color, accent) = pal.callout_colors(kind);
                (
                    bg,
                    [
                        border("top", border_color, 1.),
                        border("bottom", border_color, 1.),
                        border("right", border_color, 1.),
                        border("left", accent, 4.),
                    ]
                    .concat(),
                )
            } else if code {
                (
                    pal.code_bg,
                    ["top", "bottom", "left", "right"]
                        .map(|s| border(s, pal.code_border, 1.))
                        .concat(),
                )
            } else if theme == super::ExportTheme::Press {
                (
                    pal.quote_bg,
                    [
                        border("top", pal.quote_bar, 1.),
                        border("bottom", pal.quote_bar, 1.),
                    ]
                    .concat(),
                )
            } else {
                (pal.quote_bg, border("left", pal.quote_bar, 4.))
            };
            let label = block
                .meta
                .callout
                .map(crate::ui::callout_controls::label)
                .or_else(|| {
                    block
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
                        })
                });
            let mut content = String::new();
            if let Some(label) = label {
                let color = block
                    .meta
                    .callout
                    .map_or(pal.secondary, |k| pal.callout_colors(k).2);
                let run = super::export_docx_assets::label(label, color, theme.body_family());
                content.push_str(&para(
                    None,
                    &spacing(
                        0.,
                        if block.meta.callout.is_some() {
                            30.
                        } else {
                            4.
                        },
                        18.2,
                    ),
                    &run,
                ));
            }
            content.push_str(&para(None, &spacing(0., 0., line), &text));
            out.push_str(&super::export_docx_table::boxed(
                content,
                available,
                indent,
                (pad_x, pad_top, pad_bottom),
                bg,
                &borders,
            ));
            out.push_str(&spacer(gap));
            continue;
        }
        let mut props = String::new();
        let content = if matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) {
            let marker_text = match block.kind {
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
            props.push_str(&format!("<w:tabs><w:tab w:val=\"left\" w:pos=\"{}\"/></w:tabs>{}<w:ind w:left=\"{}\" w:hanging=\"{}\"/>",twips(indent+gutter),spacing(0.,0.,line),twips(indent+gutter),twips(gutter)));
            format!(
                "{}<w:r><w:tab/></w:r>{text}",
                marker(&marker_text, pal.accent)
            )
        } else {
            props.push_str(&format!(
                "{}<w:ind w:left=\"{}\"/>",
                spacing(0., 0., line),
                twips(indent)
            ));
            text
        };
        let heading = heading_level(block).map(|n| format!("Heading{n}"));
        if pad_top > 0. {
            out.push_str(&spacer(pad_top));
        }
        out.push_str(&para(heading.as_deref(), &props, &content));
        out.push_str(&spacer(gap + pad_bottom));
    }
    if options.footer {
        let (y, m, d) = super::chrome::today();
        let props = format!(
            "<w:pBdr>{}</w:pBdr><w:tabs><w:tab w:val=\"right\" w:pos=\"{}\"/></w:tabs>{}",
            border("top", pal.divider, 1.),
            twips(width),
            spacing(28., 0., 19.6)
        );
        let run = format!(
            "<w:r><w:rPr><w:color w:val=\"{}\"/>{}</w:rPr>{}<w:tab/>{}</w:r>",
            hex(pal.secondary),
            size(14.),
            text_xml(WATERMARK_LABEL),
            text_xml(&format!("{y:04}.{m:02}.{d:02}"))
        );
        out.push_str(&para(None, &props, &run));
    }
    let header = assets.header_id.as_ref().map_or(String::new(), |id| {
        format!("<w:headerReference w:type=\"default\" r:id=\"{id}\"/>")
    });
    let page_top = if options.show_title && theme.spec().mark == super::spec::TitleMark::Band {
        0.
    } else {
        top
    };
    out.push_str(&format!("<w:sectPr>{header}<w:pgSz w:w=\"{}\" w:h=\"16838\"/><w:pgMar w:top=\"{}\" w:right=\"{}\" w:bottom=\"{}\" w:left=\"{}\" w:header=\"0\" w:footer=\"0\" w:gutter=\"0\"/></w:sectPr>",twips(super::layout::PAGE_WIDTH),twips(page_top),twips(right),twips(bottom),twips(left)));
    Ok(out)
}
