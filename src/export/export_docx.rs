//! Word (.docx) export: an OpenXML package built in memory and stored in a ZIP container.
use super::export_docx_xml::{body, hex};
use super::export_zip::zip_store;
use super::palette::Palette;
use super::{Cancellation, ExportError, RenderOptions};
use crate::editor::model::Block;

const NS: &str = "xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\"";
const DECLARATION: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n";
const CONTENT_TYPES: &str = concat!(
    "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n",
    "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">",
    "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>",
    "<Default Extension=\"xml\" ContentType=\"application/xml\"/>",
    "<Override PartName=\"/word/document.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml\"/>",
    "<Override PartName=\"/word/styles.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml\"/>",
    "<Override PartName=\"/word/settings.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml\"/>",
    "</Types>"
);
const ROOT_RELS: &str = concat!(
    "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n",
    "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">",
    "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"word/document.xml\"/>",
    "</Relationships>"
);
const DOCUMENT_RELS: &str = concat!(
    "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n",
    "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">",
    "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles\" Target=\"styles.xml\"/>",
    "<Relationship Id=\"rId2\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings\" Target=\"settings.xml\"/>",
    "</Relationships>"
);

// Heading sizes in half-points: H1..H6.
pub(super) const HEADING_SIZES: [u32; 6] = [44, 34, 28, 24, 22, 20];

fn style(id: &str, name: &str, based: bool, ppr: &str, rpr: &str) -> String {
    let based = if based { "<w:basedOn w:val=\"Normal\"/>" } else { "" };
    format!(
        "<w:style w:type=\"paragraph\" w:styleId=\"{id}\"><w:name w:val=\"{name}\"/>{based}<w:qFormat/><w:pPr>{ppr}</w:pPr><w:rPr>{rpr}</w:rPr></w:style>"
    )
}

fn styles(options: &RenderOptions, pal: &Palette) -> String {
    let theme = options.theme;
    let (body_font, head_font) = (theme.body_family(), theme.heading_family());
    let (text, title) = (hex(pal.text), hex(pal.title));
    let mut out = format!(
        "{DECLARATION}<w:styles {NS}><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii=\"Calibri\" w:hAnsi=\"Calibri\" w:eastAsia=\"{body_font}\" w:cs=\"Calibri\"/><w:color w:val=\"{text}\"/><w:sz w:val=\"22\"/><w:szCs w:val=\"22\"/><w:lang w:val=\"en-US\" w:eastAsia=\"zh-CN\"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after=\"120\" w:line=\"360\" w:lineRule=\"auto\"/></w:pPr></w:pPrDefault></w:docDefaults>"
    );
    out.push_str("<w:style w:type=\"paragraph\" w:default=\"1\" w:styleId=\"Normal\"><w:name w:val=\"Normal\"/><w:qFormat/></w:style>");
    let head_rpr = |size: u32| format!("<w:rFonts w:eastAsia=\"{head_font}\"/><w:b/><w:color w:val=\"{title}\"/><w:sz w:val=\"{size}\"/><w:szCs w:val=\"{size}\"/>");
    out.push_str(&style("Title", "Title", true, "<w:spacing w:before=\"0\" w:after=\"360\"/>", &head_rpr(52)));
    for (index, size) in HEADING_SIZES.iter().enumerate() {
        let level = index + 1;
        let ppr = format!("<w:keepNext/><w:spacing w:before=\"280\" w:after=\"120\"/><w:outlineLvl w:val=\"{index}\"/>");
        out.push_str(&style(&format!("Heading{level}"), &format!("heading {level}"), true, &ppr, &head_rpr(*size)));
    }
    let quote_ppr = format!(
        "<w:pBdr><w:left w:val=\"single\" w:sz=\"18\" w:space=\"8\" w:color=\"{}\"/></w:pBdr><w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"{}\"/><w:ind w:left=\"240\"/>",
        hex(pal.quote_bar),
        hex(pal.quote_bg)
    );
    out.push_str(&style("Quote", "Quote", true, &quote_ppr, &format!("<w:color w:val=\"{}\"/>", hex(pal.quote_fg))));
    let code_ppr = format!(
        "<w:pBdr><w:top w:val=\"single\" w:sz=\"4\" w:space=\"4\" w:color=\"{c}\"/><w:left w:val=\"single\" w:sz=\"4\" w:space=\"4\" w:color=\"{c}\"/><w:bottom w:val=\"single\" w:sz=\"4\" w:space=\"4\" w:color=\"{c}\"/><w:right w:val=\"single\" w:sz=\"4\" w:space=\"4\" w:color=\"{c}\"/></w:pBdr><w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"{}\"/><w:spacing w:line=\"276\" w:lineRule=\"auto\"/>",
        hex(pal.code_bg),
        c = hex(pal.code_border)
    );
    out.push_str(&style("CodeBlock", "Code Block", true, &code_ppr, "<w:rFonts w:ascii=\"Menlo\" w:hAnsi=\"Menlo\" w:cs=\"Consolas\" w:eastAsia=\"Menlo\"/><w:sz w:val=\"20\"/><w:szCs w:val=\"20\"/>"));
    out.push_str("</w:styles>");
    out
}

pub fn render_docx(
    blocks: &[Block],
    options: &RenderOptions,
    cancel: &Cancellation,
) -> Result<Vec<u8>, ExportError> {
    cancel.check()?;
    let pal = options.theme.palette();
    let document = format!(
        "{DECLARATION}<w:document {NS}><w:background w:color=\"{}\"/><w:body>{}</w:body></w:document>",
        hex(pal.inner_canvas),
        body(blocks, options, &pal, cancel)?
    );
    let settings = format!("{DECLARATION}<w:settings {NS}><w:displayBackgroundShape/></w:settings>");
    cancel.check()?;
    let styles = styles(options, &pal);
    zip_store(&[
        ("[Content_Types].xml", CONTENT_TYPES.as_bytes()),
        ("_rels/.rels", ROOT_RELS.as_bytes()),
        ("word/_rels/document.xml.rels", DOCUMENT_RELS.as_bytes()),
        ("word/styles.xml", styles.as_bytes()),
        ("word/settings.xml", settings.as_bytes()),
        ("word/document.xml", document.as_bytes()),
    ])
}
