//! Standalone HTML export: one self-contained file with the theme's colors and fonts inlined.
use super::export_html_blocks::{esc, render_blocks};
use super::palette::Palette;
use super::RenderOptions;
use crate::editor::model::Block;
use cosmic_text::Color;

pub(super) fn css_color(color: Color) -> String {
    if color.a() == 255 {
        format!("rgb({}, {}, {})", color.r(), color.g(), color.b())
    } else {
        format!(
            "rgba({}, {}, {}, {:.3})",
            color.r(),
            color.g(),
            color.b(),
            color.a() as f32 / 255.0
        )
    }
}

fn stylesheet(options: &RenderOptions, pal: &Palette) -> String {
    let theme = options.theme;
    let (top, right, bottom, left) = theme.margins();
    let width = if options.card { 640 } else { 816 };
    let body_font = format!("\"{}\", -apple-system, \"Helvetica Neue\", \"Noto Sans SC\", sans-serif", theme.body_family());
    let head_font = format!("\"{}\", \"{}\", serif", theme.heading_family(), theme.body_family());
    let page_bg = if options.card { css_color(pal.canvas) } else { css_color(pal.inner_canvas) };
    let paper_radius = if options.card { "16px" } else { "0" };
    let (text, title, secondary) = (css_color(pal.text), css_color(pal.title), css_color(pal.secondary));
    let (accent, divider) = (css_color(pal.accent), css_color(pal.divider));
    let (code_bg, code_border) = (css_color(pal.code_bg), css_color(pal.code_border));
    let (quote_bg, quote_fg, quote_bar) = (css_color(pal.quote_bg), css_color(pal.quote_fg), css_color(pal.quote_bar));
    let table_border = css_color(pal.table_border);
    let paper_bg = css_color(pal.inner_canvas);
    format!(
        "*{{box-sizing:border-box}}
body{{margin:0;padding:{pad}px 16px;background:{page_bg};color:{text};font:16px/1.75 {body_font};-webkit-font-smoothing:antialiased}}
.paper{{max-width:{width}px;margin:0 auto;padding:{top}px {right}px {bottom}px {left}px;background:{paper_bg};border-radius:{paper_radius};overflow-wrap:anywhere}}
h1,h2,h3,h4,h5,h6{{font-family:{head_font};color:{title};line-height:1.35;margin:1.6em 0 .5em}}
h1{{font-size:2em}}h2{{font-size:1.5em}}h3{{font-size:1.25em}}h4,h5,h6{{font-size:1.05em}}
.doc-header{{margin-bottom:1.5em;padding-bottom:1em;border-bottom:1px solid {divider}}}
.doc-title{{margin:0;font-size:2.25em}}
p{{margin:.75em 0}}a{{color:{accent}}}mark{{background:{quote_bg};color:inherit;padding:0 .15em}}
ul,ol{{margin:.75em 0;padding-left:1.6em}}li{{margin:.25em 0}}
ul.todo-list{{list-style:none;padding-left:.2em}}li.todo input{{margin-right:.5em;accent-color:{accent}}}
blockquote{{margin:1em 0;padding:.5em 1em;background:{quote_bg};color:{quote_fg};border-left:4px solid {quote_bar};border-radius:0 6px 6px 0}}
blockquote p,.callout p{{margin:.25em 0}}
.callout{{margin:1em 0;padding:.75em 1em;background:{quote_bg};border-left:4px solid {accent};border-radius:0 6px 6px 0}}
.callout-title{{font-weight:600;color:{accent};font-size:.875em;letter-spacing:.04em}}
pre{{margin:1em 0;padding:1em;background:{code_bg};border:1px solid {code_border};border-radius:8px;overflow:auto;font:14px/1.6 Menlo,Consolas,monospace}}
code{{font-family:Menlo,Consolas,monospace;font-size:.9em;padding:.1em .35em;background:{code_bg};border-radius:4px}}
pre code{{padding:0;background:none;font-size:inherit}}
table{{width:100%;border-collapse:collapse;margin:1em 0;font-size:.95em}}
th,td{{padding:.5em .75em;border:1px solid {table_border};text-align:left;vertical-align:top}}
th{{font-weight:600}}
hr{{border:0;border-top:1px solid {divider};margin:2em 0}}
.doc-footer{{display:flex;justify-content:space-between;margin-top:3em;padding-top:1em;border-top:1px solid {divider};color:{secondary};font-size:.875em}}
@media (max-width:640px){{.paper{{padding:32px 20px}}}}
",
        pad = if options.card { 40 } else { 0 },
    )
}

pub fn render_html(blocks: &[Block], options: &RenderOptions) -> String {
    let pal = options.theme.palette();
    let title = options.title.trim();
    let mut out = String::from(
        "<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n",
    );
    out.push_str(&format!("<title>{}</title>\n<style>\n{}</style></head>\n<body>\n<article class=\"paper\">\n", esc(title), stylesheet(options, &pal)));
    if options.show_title && !title.is_empty() {
        out.push_str(&format!("<header class=\"doc-header\"><h1 class=\"doc-title\">{}</h1></header>\n", esc(title)));
    }
    out.push_str(&render_blocks(blocks));
    if options.footer {
        out.push_str(&format!(
            "<footer class=\"doc-footer\"><span>Thinkly</span><span>{}</span></footer>\n",
            esc(options.theme.label())
        ));
    }
    out.push_str("</article>\n</body></html>\n");
    out
}
