//! Standalone themed HTML; fixed paper geometry and embedded media survive moving the file.
use super::export_html_blocks::{esc, render_blocks};
use super::palette::Palette;
use super::{Cancellation, ExportError, RenderOptions, WATERMARK_LABEL, document};
use cosmic_text::Color;

pub(super) fn css_color(color: Color) -> String {
    format!(
        "rgba({},{},{},{:.3})",
        color.r(),
        color.g(),
        color.b(),
        color.a() as f32 / 255.
    )
}

fn stylesheet(options: &RenderOptions, pal: &Palette) -> String {
    let spec = options.theme.spec();
    let (top, right, bottom, left) = spec.pad;
    let inset = spec.card.map_or(0., |c| c.inset);
    let radius = spec.card.map_or(0., |c| c.radius);
    let border = spec
        .card
        .and_then(|c| c.border)
        .map_or("0 0 0 transparent".into(), |c| {
            format!("inset 0 0 0 1px {}", css_color(c))
        });
    let shadow = spec
        .card
        .filter(|c| c.shadow.a() > 0)
        .map_or(String::new(), |c| {
            format!(",0 8px 14px {}", css_color(c.shadow))
        });
    format!(
        r#"*{{box-sizing:border-box}}
html,body{{margin:0;padding:0;background:{canvas}}}
.page{{width:816px;margin:0 auto;padding:{inset}px;background:{canvas}}}
.paper{{display:flex;flex-direction:column;padding:{top}px {right}px {bottom}px {left}px;background:{paper};border-radius:{radius}px;box-shadow:{border}{shadow};font:{size}px/{leading} "{body}";color:{text};overflow-wrap:anywhere}}
.block{{white-space:pre-wrap;min-height:var(--line)}}.block-text{{min-height:var(--line)}}
a{{color:inherit;text-decoration:inherit}}img{{max-width:100%}}
table{{border-collapse:collapse;table-layout:fixed;margin:0;white-space:pre-wrap}}
th,td{{border:.5px solid {table_border};vertical-align:top;font-weight:400;overflow-wrap:anywhere}}
hr{{width:100%;margin:0;border:0;border-top:1px solid {divider}}}
.doc-title{{margin:0;font:{weight} {title_size}px/1.3 "{title_family}";color:{title}}}
.doc-footer{{display:flex;justify-content:space-between;margin-top:28px;padding-top:16px;border-top:1px solid {divider};font-size:14px;line-height:19.6px;color:{secondary}}}
@media print{{@page{{size:612pt auto;margin:0}}html,body{{print-color-adjust:exact;-webkit-print-color-adjust:exact}}.page{{margin:0}}}}
"#,
        canvas = css_color(pal.canvas),
        paper = css_color(pal.inner_canvas),
        size = spec.body,
        leading = spec.leading,
        body = esc(spec.body_family),
        text = css_color(pal.text),
        title = css_color(pal.title),
        title_size = spec.title,
        weight = if spec.title_bold { 700 } else { 400 },
        title_family = esc(&document::font_family(
            if document::title(options).is_ascii() {
                spec.title_family
            } else {
                spec.heading_family
            }
        )),
        table_border = css_color(pal.table_border),
        divider = css_color(pal.divider),
        secondary = css_color(pal.secondary),
    )
}

fn header(options: &RenderOptions, pal: &Palette) -> String {
    use super::spec::TitleMark;
    let spec = options.theme.spec();
    let (margin, mark, title_style) = match spec.mark {
        TitleMark::Rule => (
            36.,
            format!(
                "<div style=\"height:2px;margin-top:20px;background:{}\"></div>",
                css_color(pal.title)
            ),
            String::new(),
        ),
        TitleMark::Bar => (
            34.,
            format!(
                "<div style=\"height:4px;width:48px;border-radius:2px;margin-top:18px;background:{}\"></div>",
                css_color(pal.accent)
            ),
            String::new(),
        ),
        TitleMark::Wavy => (
            30.,
            format!(
                "<svg width=\"150\" height=\"12\" style=\"display:block;margin-top:8px\"><path d=\"M0 6 Q6 0 12 6 T24 6 T36 6 T48 6 T60 6 T72 6 T84 6 T96 6 T108 6 T120 6 T132 6 T144 6 T156 6\" fill=\"none\" stroke=\"{}\" stroke-width=\"2\"/></svg>",
                css_color(pal.accent)
            ),
            String::new(),
        ),
        TitleMark::DoubleRule => (
            34.,
            format!(
                "<div style=\"height:7px;margin-top:18px;border-top:3px solid {};border-bottom:1px solid {}\"></div>",
                css_color(pal.divider),
                css_color(pal.divider)
            ),
            "text-align:center;".into(),
        ),
        TitleMark::Highlight => (
            36.,
            String::new(),
            format!(
                "background:linear-gradient(transparent 50%,{} 50%,{} 88%,transparent 88%);box-decoration-break:clone;",
                css_color(Color::rgba(
                    pal.accent.r(),
                    pal.accent.g(),
                    pal.accent.b(),
                    105
                )),
                css_color(Color::rgba(
                    pal.accent.r(),
                    pal.accent.g(),
                    pal.accent.b(),
                    105
                ))
            ),
        ),
        TitleMark::Cursor => (
            36.,
            format!(
                "<span style=\"display:inline-block;vertical-align:middle;margin-left:10px;width:{}px;height:{}px;background:{}\"></span>",
                spec.title * 0.55,
                spec.title * 0.91,
                css_color(pal.accent)
            ),
            String::new(),
        ),
        TitleMark::Band => (
            52.,
            String::new(),
            format!(
                "display:block;padding-bottom:44px;background:{};color:{};",
                css_color(pal.accent),
                css_color(pal.accent2)
            ),
        ),
    };
    let band = spec.mark == TitleMark::Band;
    let (_, right, _, left) = options.theme.margins();
    let outer = if band {
        format!(
            "margin:-{}px -{right}px 0 -{left}px;padding:{}px {right}px 0 {left}px;background:{};",
            options.theme.margins().0,
            options.theme.margins().0,
            css_color(pal.accent)
        )
    } else {
        String::new()
    };
    let cursor = spec.mark == TitleMark::Cursor;
    format!(
        "<header style=\"{outer}margin-bottom:{margin}px\"><h1 class=\"doc-title\" style=\"{}\"><span style=\"{title_style}\">{}</span>{}</h1>{}</header>",
        if spec.mark == TitleMark::DoubleRule {
            "text-align:center"
        } else {
            ""
        },
        esc(document::title(options)),
        if cursor { mark.as_str() } else { "" },
        if cursor { "" } else { mark.as_str() }
    )
}

pub fn render_html(blocks: &[Block], options: &RenderOptions) -> Result<String, ExportError> {
    super::budget::validate(blocks)?;
    let pal = options.theme.palette();
    let mut out = format!(
        "<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=816\"><title>{}</title><style>{}</style></head><body><div class=\"page\"><article class=\"paper\">",
        esc(document::title(options)),
        stylesheet(options, &pal)
    );
    if options.show_title {
        out.push_str(&header(options, &pal));
    }
    out.push_str(&render_blocks(blocks, options, &Cancellation::default())?);
    if options.footer {
        let (y, m, d) = super::chrome::today();
        out.push_str(&format!("<footer class=\"doc-footer\"><span>{WATERMARK_LABEL}</span><span>{y:04}.{m:02}.{d:02}</span></footer>"));
    }
    if !options.footer {
        out.push_str("<div style=\"height:28px\"></div>");
    }
    out.push_str("</article></div></body></html>\n");
    Ok(out)
}

use crate::editor::model::Block;
