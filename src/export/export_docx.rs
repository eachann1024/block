//! Editable OpenXML export, using the preview's type scale, colors and content column.
use super::export_docx_assets::Assets;
use super::export_docx_xml::{body, font, hex, size, spacing};
use super::export_zip::zip_store;
use super::{Cancellation, ExportError, RenderOptions};
use crate::editor::model::{Block, Kind};

const NS: &str = concat!(
    "xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\" ",
    "xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\" ",
    "xmlns:wp=\"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing\" ",
    "xmlns:a=\"http://schemas.openxmlformats.org/drawingml/2006/main\" ",
    "xmlns:pic=\"http://schemas.openxmlformats.org/drawingml/2006/picture\""
);
const DECLARATION: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>";
const CONTENT_TYPES: &str = concat!(
    "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">",
    "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>",
    "<Default Extension=\"xml\" ContentType=\"application/xml\"/>",
    "<Default Extension=\"png\" ContentType=\"image/png\"/>",
    "<Override PartName=\"/word/document.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml\"/>",
    "<Override PartName=\"/word/styles.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml\"/>",
    "<Override PartName=\"/word/settings.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml\"/>",
    "<Override PartName=\"/word/header1.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml\"/>",
    "</Types>"
);
const ROOT_RELS: &str = "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\"><Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"word/document.xml\"/></Relationships>";

fn styles(options: &RenderOptions) -> String {
    let spec = options.theme.spec();
    let pal = options.theme.palette();
    let mut out = format!(
        "{DECLARATION}<w:styles {NS}><w:docDefaults><w:rPrDefault><w:rPr>{}<w:color w:val=\"{}\"/>{}<w:lang w:val=\"en-US\" w:eastAsia=\"zh-CN\"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:widowControl w:val=\"0\"/><w:autoSpaceDE w:val=\"0\"/><w:autoSpaceDN w:val=\"0\"/><w:snapToGrid w:val=\"0\"/>{}</w:pPr></w:pPrDefault></w:docDefaults><w:style w:type=\"paragraph\" w:default=\"1\" w:styleId=\"Normal\"><w:name w:val=\"Normal\"/><w:qFormat/></w:style>",
        font(spec.body_family),
        hex(pal.text),
        size(spec.body),
        spacing(0., 0., spec.body * spec.leading)
    );
    for (level, kind) in [
        (1, Kind::H1),
        (2, Kind::H2),
        (3, Kind::H3),
        (4, Kind::H3),
        (5, Kind::H3),
        (6, Kind::H3),
    ] {
        let mut b = Block::new(0, kind, String::new());
        b.meta.heading_level = Some(level);
        let px = super::style::block_font_size(options.theme, &b, false);
        let weight = if level == 6 {
            "<w:b w:val=\"0\"/><w:bCs w:val=\"0\"/>"
        } else {
            "<w:b/><w:bCs/>"
        };
        out.push_str(&format!("<w:style w:type=\"paragraph\" w:styleId=\"Heading{level}\"><w:name w:val=\"heading {level}\"/><w:basedOn w:val=\"Normal\"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val=\"{}\"/></w:pPr><w:rPr>{}{weight}<w:color w:val=\"{}\"/>{}</w:rPr></w:style>",level-1,font(spec.heading_family),hex(pal.title),size(px)));
    }
    out.push_str("</w:styles>");
    out
}

pub fn render_docx(
    blocks: &[Block],
    options: &RenderOptions,
    cancel: &Cancellation,
) -> Result<Vec<u8>, ExportError> {
    super::budget::validate(blocks)?;
    cancel.check()?;
    let pal = options.theme.palette();
    let mut assets = Assets::default();
    assets.page_background(options, cancel, NS)?;
    let document = format!(
        "{DECLARATION}<w:document {NS}><w:background w:color=\"{}\"/><w:body>{}</w:body></w:document>",
        hex(pal.inner_canvas),
        body(blocks, options, &pal, cancel, &mut assets)?
    );
    let settings = format!(
        "{DECLARATION}<w:settings {NS}><w:displayBackgroundShape/><w:autoHyphenation w:val=\"0\"/><w:compat><w:compatSetting w:name=\"compatibilityMode\" w:uri=\"http://schemas.microsoft.com/office/word\" w:val=\"15\"/></w:compat></w:settings>"
    );
    let mut files = vec![
        (
            "[Content_Types].xml".into(),
            CONTENT_TYPES.as_bytes().to_vec(),
        ),
        ("_rels/.rels".into(), ROOT_RELS.as_bytes().to_vec()),
        (
            "word/_rels/document.xml.rels".into(),
            assets.relationships().into_bytes(),
        ),
        ("word/styles.xml".into(), styles(options).into_bytes()),
        ("word/settings.xml".into(), settings.into_bytes()),
        ("word/document.xml".into(), document.into_bytes()),
    ];
    files.extend(assets.files);
    cancel.check()?;
    zip_store(
        &files
            .iter()
            .map(|(n, b)| (n.as_str(), b.as_slice()))
            .collect::<Vec<_>>(),
    )
}
