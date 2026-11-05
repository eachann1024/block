//! thinkly `inlinePreview`: while a rewrite is under review the target block shows the
//! change in place — the removed middle struck through on red, the added middle on
//! green after it — and the target is outlined with a dashed ring.
use std::hash::{Hash, Hasher};

use gpui::{Bounds, Hsla, StrikethroughStyle, TextRun, UnderlineStyle, Window, hsla, px};

use super::session::AiStatus;
use crate::editor::view::Editor;
use crate::editor::view::layout::TextLayout;

fn del_fg() -> Hsla { hsla(4. / 360., 0.75, 0.42, 1.) }
fn ins_fg() -> Hsla { hsla(152. / 360., 0.86, 0.24, 1.) }

/// Common prefix / suffix lengths in bytes, on char boundaries (thinkly `inlineDiffEdges`).
fn edges(before: &str, after: &str) -> (usize, usize) {
    let prefix: usize = before.chars().zip(after.chars()).take_while(|(a, b)| a == b).map(|(a, _)| a.len_utf8()).sum();
    let (b_rest, a_rest) = (&before[prefix..], &after[prefix..]);
    let suffix: usize = b_rest.chars().rev().zip(a_rest.chars().rev()).take_while(|(a, b)| a == b).map(|(a, _)| a.len_utf8()).sum();
    (prefix, suffix)
}

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
    fn review(&self, block: u64) -> Option<(std::ops::Range<usize>, Option<String>, bool)> {
        let s = self.ai_session()?;
        let AiStatus::Review { draft } = &s.status else { return None };
        let at = s.parts.iter().position(|(id, _)| *id == block)?;
        let last = at + 1 == s.parts.len();
        let range = s.parts[at].1.clone();
        let single = s.parts.len() == 1;
        // A one-block target diffs in place; a multi-block one strikes every part and
        // shows the whole rewrite after the last part.
        Some((range, (single || last).then(|| draft.replace('\n', " ↵ ")), single))
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
        let (range, draft, single) = self.review(block)?;
        let text = &self.doc.get(block)?.text;
        if range.end > text.len() || !text.is_char_boundary(range.start) || !text.is_char_boundary(range.end) { return None; }
        let base: Vec<TextRun> = if runs.iter().map(|r| r.len).sum::<usize>() == text.len() { runs.to_vec() } else { return None };
        let old = &text[range.clone()];
        let new = draft.unwrap_or_default();
        let (p, sfx) = if single { edges(old, &new) } else { (0, 0) };
        let p = p.min(old.len()).min(new.len());
        let sfx = sfx.min(old.len() - p).min(new.len() - p);
        let (del_s, del_e) = (range.start + p, range.end - sfx);
        let added = &new[p..new.len() - sfx];
        let mut out_text = String::with_capacity(text.len() + added.len());
        out_text.push_str(&text[..del_e]);
        out_text.push_str(added);
        out_text.push_str(&text[del_e..]);
        let mut out = slice(&base, 0, del_s);
        for mut r in slice(&base, del_s, del_e) {
            r.color = del_fg();
            r.background_color = Some(hsla(4. / 360., 0.75, 0.5, 0.14));
            r.strikethrough = Some(StrikethroughStyle { thickness: px(1.), color: Some(del_fg()) });
            out.push(r);
        }
        if !added.is_empty() {
            let mut r = slice(&base, del_s.saturating_sub(1), del_s.max(1)).into_iter().next()
                .or_else(|| base.first().cloned()).expect("block has a run");
            r.len = added.len();
            r.color = ins_fg();
            r.background_color = Some(hsla(152. / 360., 0.6, 0.4, 0.16));
            r.strikethrough = None;
            r.underline = Some(UnderlineStyle { thickness: px(1.), color: Some(hsla(152. / 360., 0.6, 0.35, 0.8)), wavy: false });
            out.push(r);
        }
        out.extend(slice(&base, del_e, text.len()));
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
