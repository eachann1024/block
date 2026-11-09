//! OpenXML relationships and native image runs for editable Word exports.
use super::{
    ExportError,
    export_docx_xml::{esc, text_xml, twips},
};

#[derive(Default)]
pub(super) struct Assets {
    pub files: Vec<(String, Vec<u8>)>,
    relations: Vec<String>,
    pub header_id: Option<String>,
}

impl Assets {
    pub fn link(&mut self, href: &str) -> String {
        let id = format!("rId{}", self.relations.len() + 3);
        self.relations.push(format!("<Relationship Id=\"{id}\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink\" Target=\"{}\" TargetMode=\"External\"/>", esc(href)));
        id
    }

    pub fn image(&mut self, png: Vec<u8>, width: f32, height: f32, alt: &str) -> String {
        let number = self.files.len() + 1;
        let name = format!("media/image{number}.png");
        let id = format!("rId{}", self.relations.len() + 3);
        self.relations.push(format!("<Relationship Id=\"{id}\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/image\" Target=\"{name}\"/>"));
        self.files.push((format!("word/{name}"), png));
        let (cx, cy) = (
            (width * 9525.).round() as u64,
            (height * 9525.).round() as u64,
        );
        format!(
            r#"<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="{cx}" cy="{cy}"/><wp:docPr id="{number}" name="Image {number}" descr="{}"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image{number}.png"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="{id}"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="{cx}" cy="{cy}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>"#,
            esc(alt)
        )
    }

    pub fn picture(
        &mut self,
        picture: &super::media::Picture,
        indent: f32,
        alt: &str,
    ) -> Result<String, ExportError> {
        let run = self.image(picture.png()?, picture.width, picture.height, alt);
        Ok(format!(
            "<w:p><w:pPr><w:spacing w:before=\"0\" w:after=\"150\" w:line=\"240\" w:lineRule=\"auto\"/><w:ind w:left=\"{}\"/></w:pPr>{run}</w:p>",
            twips(indent + picture.x)
        ))
    }

    /// Paper artwork repeats behind editable content on each Word page.
    pub fn page_background(
        &mut self,
        options: &super::RenderOptions,
        cancel: &super::Cancellation,
        namespaces: &str,
    ) -> Result<(), ExportError> {
        let (width, height): (f32, f32) = (super::layout::PAGE_WIDTH, 16838. / 15.);
        let mut image =
            tiny_skia::Pixmap::new((width * 2.).ceil() as u32, (height * 2.).ceil() as u32)
                .ok_or(ExportError::Rendering("Page background allocation failed"))?;
        super::decor_paper::paint_surface(&mut image, options, &options.theme.palette(), cancel)?;
        let png = image
            .encode_png()
            .map_err(|e| ExportError::Encoding(e.to_string()))?;
        let run = self.image(png, width, height, "");
        let relation = self.relations.last().cloned().unwrap_or_default();
        let anchor = run.replace("<wp:inline distT=\"0\" distB=\"0\" distL=\"0\" distR=\"0\">",
            "<wp:anchor distT=\"0\" distB=\"0\" distL=\"0\" distR=\"0\" simplePos=\"0\" relativeHeight=\"0\" behindDoc=\"1\" locked=\"0\" layoutInCell=\"1\" allowOverlap=\"1\"><wp:simplePos x=\"0\" y=\"0\"/><wp:positionH relativeFrom=\"page\"><wp:posOffset>0</wp:posOffset></wp:positionH><wp:positionV relativeFrom=\"page\"><wp:posOffset>0</wp:posOffset></wp:positionV>")
            .replace("/><wp:docPr", "/><wp:wrapNone/><wp:docPr").replace("</wp:inline>","</wp:anchor>");
        self.files.push((
            "word/header1.xml".into(),
            format!(
                "<w:hdr {namespaces}><w:p><w:pPr>{}</w:pPr>{anchor}</w:p></w:hdr>",
                super::export_docx_xml::spacing(0., 0., 1.)
            )
            .into_bytes(),
        ));
        self.files.push(("word/_rels/header1.xml.rels".into(),format!("<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">{relation}</Relationships>").into_bytes()));
        let id = format!("rId{}", self.relations.len() + 3);
        self.relations.push(format!("<Relationship Id=\"{id}\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/header\" Target=\"header1.xml\"/>"));
        self.header_id = Some(id);
        Ok(())
    }

    pub fn relationships(&self) -> String {
        format!(
            r#"<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>{}</Relationships>"#,
            self.relations.concat()
        )
    }
}

pub(super) fn label(text: &str, color: cosmic_text::Color, family: &str) -> String {
    format!(
        "<w:r><w:rPr>{}<w:color w:val=\"{}\"/><w:sz w:val=\"20\"/><w:szCs w:val=\"20\"/></w:rPr>{}</w:r>",
        super::export_docx_xml::font(family),
        super::export_docx_xml::hex(color),
        text_xml(text)
    )
}
