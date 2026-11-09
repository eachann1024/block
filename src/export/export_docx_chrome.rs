//! Word title and themed title marks.
use super::export_docx_assets::Assets;
use super::export_docx_xml::*;
use super::palette::Palette;
use super::{ExportError, RenderOptions, document};
pub(super) fn header(
    options: &RenderOptions,
    pal: &Palette,
    width: f32,
    assets: &mut Assets,
) -> Result<String, ExportError> {
    use super::decor_shapes::fill_rect;
    use super::spec::TitleMark;
    let spec = options.theme.spec();
    let family = if document::title(options).is_ascii() {
        spec.title_family
    } else {
        spec.heading_family
    };
    let mut props = "<w:keepNext/>".to_string();
    if spec.mark == TitleMark::Band {
        props.push_str(&shade(pal.accent));
    }
    props.push_str(&spacing(0., 0., spec.title * 1.3));
    if spec.mark == TitleMark::DoubleRule {
        props.push_str("<w:jc w:val=\"center\"/>");
    }
    let mut rpr = font(family);
    if spec.title_bold {
        rpr.push_str("<w:b/><w:bCs/>");
    }
    rpr.push_str(&format!(
        "<w:color w:val=\"{}\"/>{}",
        hex(if spec.mark == TitleMark::Band {
            pal.accent2
        } else {
            pal.title
        }),
        size(spec.title)
    ));
    if spec.mark == TitleMark::Highlight {
        rpr.push_str(&shade(pal.highlight_bg()));
    }
    let mut text = format!(
        "<w:r><w:rPr>{rpr}</w:rPr>{}</w:r>",
        text_xml(document::title(options))
    );
    if spec.mark == TitleMark::Cursor {
        text.push_str(&marker(" ▌", pal.accent));
    }
    let mut out = para(None, &props, &text);
    let (w, h, before, after): (f32, f32, f32, f32) = match spec.mark {
        TitleMark::Rule => (width, 2., 20., 36.),
        TitleMark::Bar => (48., 4., 18., 34.),
        TitleMark::Wavy => (150., 12., 8., 30.),
        TitleMark::DoubleRule => (width, 7., 18., 34.),
        TitleMark::Band => {
            let (top, right, _, left) = options.theme.margins();
            let full = twips(super::layout::PAGE_WIDTH);
            out = format!(
                "<w:tbl><w:tblPr><w:tblW w:w=\"{full}\" w:type=\"dxa\"/><w:tblInd w:w=\"-{}\" w:type=\"dxa\"/><w:tblLayout w:type=\"fixed\"/><w:tblCellMar><w:top w:w=\"{}\" w:type=\"dxa\"/><w:left w:w=\"{}\" w:type=\"dxa\"/><w:bottom w:w=\"660\" w:type=\"dxa\"/><w:right w:w=\"{}\" w:type=\"dxa\"/></w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w=\"{full}\"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w=\"{full}\" w:type=\"dxa\"/>{}</w:tcPr>{out}</w:tc></w:tr></w:tbl>",
                twips(left),
                twips(top),
                twips(left),
                twips(right),
                shade(pal.accent)
            );
            out.push_str(&spacer(52.));
            return Ok(out);
        }
        _ => {
            out.push_str(&spacer(36.));
            return Ok(out);
        }
    };
    let mut pixels = tiny_skia::Pixmap::new((w * 2.).ceil() as u32, (h * 2.).ceil() as u32)
        .ok_or(ExportError::Rendering("Title mark allocation failed"))?;
    match spec.mark {
        TitleMark::Wavy => super::decor_marks::stroke_wavy(&mut pixels, 0., 0., w, h, pal.accent),
        TitleMark::DoubleRule => {
            fill_rect(&mut pixels, 0., 0., w, 3., pal.divider);
            fill_rect(&mut pixels, 0., 6., w, 1., pal.divider);
        }
        _ => fill_rect(
            &mut pixels,
            0.,
            0.,
            w,
            h,
            if spec.mark == TitleMark::Rule {
                pal.title
            } else {
                pal.accent
            },
        ),
    }
    let png = pixels
        .encode_png()
        .map_err(|e| ExportError::Encoding(e.to_string()))?;
    let mark = assets.image(png, w, h, "");
    out.push_str(&para(
        None,
        &format!("<w:keepNext/>{}", spacing(before, 0., h)),
        &mark,
    ));
    out.push_str(&spacer(after));
    Ok(out)
}
