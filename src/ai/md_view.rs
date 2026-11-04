//! Read-only Markdown for AI chat bubbles (thinkly renders replies with its Markdown
//! component): headings, lists, todos, quotes, fenced code, tables as text, and
//! inline bold / italic / strike / code / links. Parsed with the editor's own parser.
use gpui::{AnyElement, FontStyle, FontWeight, HighlightStyle, InteractiveText, IntoElement, ParentElement, SharedString,
    StrikethroughStyle, Styled, StyledText, UnderlineStyle, div, px};

use crate::editor::markdown;
use crate::editor::model::{Block, Kind, MarkKind};
use crate::theme::Theme;

fn inline(block: &Block, id: String) -> AnyElement {
    let theme = Theme::ocean();
    let text = block.text.clone();
    let mut cuts: Vec<usize> = vec![0, text.len()];
    for s in &block.spans { cuts.extend([s.start, s.end]); }
    let links: Vec<_> = block.meta.inline.iter().filter(|t| t.name == "a").collect();
    for t in &links { cuts.extend([t.start, t.end]); }
    cuts.retain(|&c| c <= text.len() && text.is_char_boundary(c));
    cuts.sort(); cuts.dedup();
    let mut hl = Vec::new();
    for w in cuts.windows(2) {
        let (a, b) = (w[0], w[1]);
        if a >= b { continue; }
        let mut st = HighlightStyle::default();
        let mut any = false;
        if let Some(s) = block.spans.iter().find(|s| s.start <= a && b <= s.end) {
            for m in &s.marks {
                any = true;
                match m {
                    MarkKind::Bold => st.font_weight = Some(FontWeight::SEMIBOLD),
                    MarkKind::Italic => st.font_style = Some(FontStyle::Italic),
                    MarkKind::Strike => st.strikethrough = Some(StrikethroughStyle { thickness: px(1.), color: None }),
                    MarkKind::Code => { st.background_color = Some(theme.code_bg); st.color = Some(gpui::rgb(0xc7254e).into()); }
                }
            }
        }
        if links.iter().any(|t| t.start <= a && b <= t.end) {
            any = true;
            st.color = Some(theme.accent);
            st.underline = Some(UnderlineStyle { thickness: px(1.), color: Some(theme.accent), wavy: false });
        }
        if any { hl.push((a..b, st)); }
    }
    let ranges: Vec<_> = links.iter().map(|t| t.start..t.end).collect();
    let hrefs: Vec<String> = links.iter().map(|t| t.attr("href").unwrap_or_default().to_owned()).collect();
    let styled = StyledText::new(SharedString::from(text)).with_highlights(hl);
    if ranges.is_empty() { return styled.into_any_element(); }
    InteractiveText::new(SharedString::from(id), styled)
        .on_click(ranges, move |ix, _, _| { if let Some(h) = hrefs.get(ix) { crate::editor::open_external_url(h, None); } })
        .into_any_element()
}

pub(crate) fn render(source: &str, key: &str) -> AnyElement {
    let theme = Theme::ocean();
    let doc = markdown::parse(source);
    let mut out = div().flex().flex_col().gap(px(6.)).text_size(px(14.)).line_height(px(22.));
    for (i, b) in doc.blocks.iter().enumerate() {
        let id = format!("md-{key}-{i}");
        if b.meta.code_language.is_some() {
            out = out.child(div().p(px(10.)).rounded(px(6.)).bg(theme.code_bg).font_family("Menlo").text_size(px(12.5)).line_height(px(19.))
                .child(SharedString::from(b.text.clone())));
            continue;
        }
        if let Some(raw) = &b.meta.raw_source {
            out = out.child(div().font_family("Menlo").text_size(px(12.5)).child(SharedString::from(raw.clone())));
            continue;
        }
        if b.meta.table.is_some() {
            let md = markdown::serialize(&crate::editor::model::Doc::from_blocks(vec![b.clone()]));
            out = out.child(div().font_family("Menlo").text_size(px(12.)).text_color(theme.text_secondary).child(SharedString::from(md.trim().to_owned())));
            continue;
        }
        if b.text.trim().is_empty() && b.kind == Kind::Paragraph { continue; }
        let indent = px(b.meta.indent as f32 * 18.);
        let body = div().flex_1().min_w_0().child(inline(b, id));
        let row = match b.kind {
            Kind::H1 | Kind::H2 | Kind::H3 => {
                let size = match b.kind { Kind::H1 => 18., Kind::H2 => 16., _ => 15. };
                div().pt(px(4.)).text_size(px(size)).font_weight(FontWeight::SEMIBOLD).child(body)
            }
            Kind::Bullet => div().pl(indent).flex().gap(px(6.)).child(div().flex_none().w(px(12.)).text_color(theme.text_secondary).child("•")).child(body),
            Kind::Numbered => {
                let n = doc.list_number(b.id).unwrap_or(1);
                div().pl(indent).flex().gap(px(6.)).child(div().flex_none().min_w(px(16.)).text_color(theme.text_secondary).child(SharedString::from(format!("{n}.")))).child(body)
            }
            Kind::Todo => div().pl(indent).flex().gap(px(6.)).child(div().flex_none().w(px(16.)).text_color(theme.text_secondary).child(if b.checked { "☑" } else { "☐" })).child(body),
            Kind::Quote => div().pl(px(10.)).border_l_2().border_color(theme.quote_bar).text_color(theme.text_secondary).child(body),
            Kind::Paragraph => div().child(body),
        };
        out = out.child(row);
    }
    out.into_any_element()
}
