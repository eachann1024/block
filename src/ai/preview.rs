//! thinkly `inlinePreview`: while a rewrite is under review the target block shows the
//! original selection struck through in red and outlined with a dashed ring.
//! Generated content is rendered structurally in the shared rich-text preview.
use std::hash::{Hash, Hasher};

use gpui::{Bounds, Hsla, StrikethroughStyle, TextRun, Window, hsla, px};

use super::session::AiStatus;
use crate::editor::view::Editor;
use crate::editor::view::layout::TextLayout;

fn del_fg() -> Hsla { hsla(4. / 360., 0.75, 0.42, 1.) }
/// Keeps `[from, to)` of `runs` (which cover `0..total`).
fn slice(runs: &[TextRun], from: usize, to: usize) -> Vec<TextRun> {
    let mut out = Vec::new();
    let mut at = 0;
    for run in runs {
        let (s, e) = (at.max(from), (at + run.len).min(to));
        if s < e { let mut r = run.clone(); r.len = e - s; out.push(r); }
        at += run.len;
    }
    out
}

impl Editor {
    fn review(&self, block: u64) -> Option<std::ops::Range<usize>> {
        let s = self.ai_session()?;
        let AiStatus::Review { .. } = &s.status else { return None };
        let at = s.parts.iter().position(|(id, _)| *id == block)?;
        Some(s.parts[at].1.clone())
    }
    /// Mixed into the block's layout cache key so the preview reshapes on change.
    pub(in crate::editor) fn ai_preview_key(&self, block: u64) -> u64 {
        let Some(s) = self.ai_session() else { return 0 };
        if !s.parts.iter().any(|(id, _)| *id == block) { return 0; }
        let mut h = std::collections::hash_map::DefaultHasher::new();
        match &s.status { AiStatus::Review { draft } => draft.hash(&mut h), _ => 1u8.hash(&mut h) }
        h.finish() | 1
    }
    /// Display text and runs for a block under review, or `None` to draw it as usual.
    pub(in crate::editor) fn ai_preview(&self, block: u64, runs: &[TextRun]) -> Option<(String, Vec<TextRun>)> {
        let range = self.review(block)?;
        let text = &self.doc.get(block)?.text;
        if range.end > text.len() || !text.is_char_boundary(range.start) || !text.is_char_boundary(range.end) { return None; }
        if runs.iter().map(|r| r.len).sum::<usize>() != text.len() { return None; }
        let out_text = text.clone();
        let mut out = slice(runs, 0, range.start);
        for mut run in slice(runs, range.start, range.end) {
            run.color = del_fg();
            run.background_color = Some(hsla(4. / 360., 0.75, 0.5, 0.14));
            run.strikethrough = Some(StrikethroughStyle { thickness: px(1.), color: Some(del_fg()) });
            out.push(run);
        }
        out.extend(slice(runs, range.end, text.len()));
        if out_text.is_empty() { return None; }
        Some((out_text, out))
    }
}

/// Dashed ring around a block that is the target of an open AI session.
pub(in crate::editor) fn paint_outline(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    let Some(s) = editor.ai_session() else { return };
    if !s.parts.iter().any(|(id, _)| *id == block) { return; }
    let zoom = crate::editor::zoom::zoom_factor(editor.minimal);
    let b = layout.bounds;
    let ring = Bounds::new(b.origin - gpui::point(px(6. * zoom), px(3. * zoom)), b.size + gpui::size(px(12. * zoom), px(6. * zoom)));
    window.paint_quad(gpui::quad(ring, px(10. * zoom), gpui::transparent_black(), px(1.), hsla(214. / 360., 0.7, 0.55, 0.7), gpui::BorderStyle::Dashed));
}
