//! Annotation mode (F-Annotate): selection comments, emoji reactions, inline
//! highlights (`#fef0d1` anchor / `#fdda8c` active / `#fdf3c4` AI-modified), and AI rewrites.
use std::{collections::BTreeMap, fs, ops::Range, path::{Path, PathBuf}, time::{SystemTime, UNIX_EPOCH}};
use gpui::{BorderStyle, Context, Entity, Pixels, Point, Subscription, Window, prelude::*, px, quad, rgb, transparent_black};
use gpui_component::input::{InputEvent, InputState};
use serde::{Deserialize, Serialize};
use crate::theme::tr;
use super::{model::Doc, view::{Editor, layout::TextLayout}};
#[path = "annotate_ai.rs"] mod ai;
#[path = "annotate_actions.rs"] mod actions;
#[path = "annotate_apply.rs"] mod apply;
#[path = "annotate_reactions.rs"] mod reactions;
#[path = "annotate_revert.rs"] mod revert;

pub(super) const HL_SELECT_BG: u32 = 0xfef0d1;
pub(super) const HL_ACTIVE_BG: u32 = 0xfdda8c;
pub(super) const HL_SELECT_BG_DARK: u32 = 0x4a3c21;
pub(super) const HL_ACTIVE_BG_DARK: u32 = 0x81611e;
pub(super) const HL_DONE_BG: u32 = 0xe5edfc;
pub(super) const HL_DONE_FLASH: u32 = 0xc9d8f3;
pub(super) const HL_DONE_BG_DARK: u32 = 0x2d426b;
pub(super) const HL_DONE_FLASH_DARK: u32 = 0x31456a;
pub(super) const HL_TABLE_BORDER: u32 = 0xb77700;
pub(super) const HL_TABLE_BORDER_ACTIVE: u32 = 0xa66a00;
pub(super) const HL_TABLE_BORDER_DARK: u32 = 0xdb9300;
pub(super) const HL_TABLE_BORDER_ACTIVE_DARK: u32 = 0xfab219;
pub(super) const ANNOTATION_PENDING: u32 = 0xf5c84c;
#[derive(Clone, Debug, Serialize, Deserialize)]
pub(super) struct Annotation {
    pub id: u64, pub block_index: usize, pub start: usize, pub end: usize,
    pub quote: String, pub body: String,
    #[serde(default)] pub reactions: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub ai_rewritten: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub ai_error: Option<String>,
    /// Missing means a legacy rewrite already changed the document.
    #[serde(default, skip_serializing_if = "Option::is_none")] pub applied: Option<bool>,
    #[serde(skip)] pub streaming: bool,
    #[serde(skip)] pub request_id: Option<u64>,
}

impl Annotation {
    pub(super) fn is_applied(&self) -> bool { self.applied.unwrap_or(self.ai_rewritten.is_some()) }
    pub(super) fn is_pending(&self) -> bool {
        !self.is_applied() && !self.has_rewrite() && !self.streaming && self.ai_error.is_none()
            && !self.body.trim().is_empty() && !self.quote.trim().is_empty()
    }
    pub(super) fn is_generated(&self) -> bool { !self.is_applied() && self.ai_rewritten.as_ref().is_some_and(|text| !text.is_empty()) }
    pub(super) fn has_rewrite(&self) -> bool { self.ai_rewritten.as_ref().is_some_and(|text| !text.is_empty()) }
}

pub(super) struct AnnotateComposer {
    pub block_id: u64, pub block_index: usize, pub range: Range<usize>, pub quote: String,
    pub input: Entity<InputState>, _sub: Subscription,
}

pub(super) struct AnnotationFollowup {
    pub annotation_id: u64, pub input: Entity<InputState>, pub _sub: Subscription,
}

pub(super) struct AnnotationEdit {
    pub annotation_id: u64, pub input: Entity<InputState>, pub _sub: Subscription,
}

#[derive(Clone)]
pub(super) struct ReactionPicker {
    pub block_id: u64, pub block_index: usize, pub range: Range<usize>,
    pub quote: String, pub annotation_id: Option<u64>, pub anchor: Point<Pixels>,
}

#[derive(Default)]
pub(super) struct AnnotateState {
    pub loaded_path: Option<PathBuf>, pub annotations: Vec<Annotation>, pub composer: Option<AnnotateComposer>,
    pub followup: Option<AnnotationFollowup>, pub editing: Option<AnnotationEdit>, pub reactions: Option<ReactionPicker>,
    pub active_annotation: Option<u64>, pub hovered_annotation: Option<u64>, pub hover_seq: u64,
    pub editor_left: Pixels, pub editor_top: Pixels, pub editor_width: Pixels, pub editor_height: Pixels,
    pub fit_snapshot: super::annotate_fit::LayoutSnapshot, pub request_seq: u64,
}

impl AnnotateState {
    pub(super) fn has_visible_rail(&self) -> bool {
        self.composer.is_some() || self.annotations.iter().any(|note| !note.is_applied() || !note.reactions.is_empty())
    }
}

#[derive(Default, Serialize, Deserialize)]
struct AnnotationStore { notes: BTreeMap<String, Vec<Annotation>> }

fn store_path() -> Option<PathBuf> {
    Some(crate::files::app_data_dir("Thinkly Fast").join("annotations.json"))
}

fn load_for(path: &Path) -> Vec<Annotation> {
    let store: AnnotationStore = store_path().and_then(|p| fs::read_to_string(p).ok())
        .and_then(|raw| serde_json::from_str(&raw).ok()).unwrap_or_default();
    store.notes.get(path.to_string_lossy().as_ref()).cloned().unwrap_or_default()
}

fn save_for(path: &Path, comments: &[Annotation]) {
    let Some(file) = store_path() else { return };
    if let Some(parent) = file.parent() { let _ = fs::create_dir_all(parent); }
    let mut store: AnnotationStore = fs::read_to_string(&file).ok().and_then(|raw| serde_json::from_str(&raw).ok()).unwrap_or_default();
    let key = path.to_string_lossy().into_owned();
    if comments.is_empty() { store.notes.remove(&key); } else { store.notes.insert(key, comments.to_vec()); }
    if let Ok(json) = serde_json::to_string_pretty(&store) { let _ = fs::write(file, json); }
}

#[cfg(not(target_family = "wasm"))]
fn next_id() -> u64 { SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(1) }
#[cfg(target_family = "wasm")]
fn next_id() -> u64 { let _ = (SystemTime::UNIX_EPOCH, UNIX_EPOCH); static SEQ: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1); SEQ.fetch_add(1, std::sync::atomic::Ordering::Relaxed) }
/// Resolves an annotation's current `(block_id, byte_range, is_ai_modified)` in `doc`.
pub(super) fn resolve_annotation_span(doc: &Doc, annotation: &Annotation) -> Option<(u64, Range<usize>, bool)> {
    if annotation.is_applied()
        && let Some(rewritten) = annotation.ai_rewritten.as_deref().filter(|s| !s.is_empty())
    {
        if let Some((id, range)) = find_snippet(doc, annotation.block_index, annotation.start, rewritten) {
            return Some((id, range, true));
        }
    }
    if !annotation.quote.is_empty() {
        if let Some((id, range)) = find_snippet(doc, annotation.block_index, annotation.start, &annotation.quote) {
            return Some((id, range, false));
        }
    }
    doc.blocks.get(annotation.block_index).map(|b| {
        let end = annotation.end.min(b.text.len());
        (b.id, annotation.start.min(end)..end, false)
    })
}

fn find_in_text(text: &str, preferred_start: usize, needle: &str) -> Option<Range<usize>> {
    if needle.is_empty() { return None; }
    let end = preferred_start.saturating_add(needle.len());
    if end <= text.len() && text.get(preferred_start..end) == Some(needle) {
        return Some(preferred_start..end);
    }
    text.match_indices(needle)
        .min_by_key(|(pos, _)| pos.abs_diff(preferred_start))
        .map(|(pos, _)| pos..(pos + needle.len()))
}

fn find_snippet(doc: &Doc, preferred_index: usize, preferred_start: usize, needle: &str) -> Option<(u64, Range<usize>)> {
    if let Some(block) = doc.blocks.get(preferred_index)
        && let Some(range) = find_in_text(&block.text, preferred_start, needle)
    {
        return Some((block.id, range));
    }
    doc.blocks.iter().enumerate()
        .filter_map(|(idx, b)| {
            find_in_text(&b.text, preferred_start, needle)
                .map(|range| (idx.abs_diff(preferred_index), range.start.abs_diff(preferred_start), b.id, range))
        })
        .min_by_key(|(block_dist, start_dist, _, _)| (*block_dist, *start_dist))
        .map(|(_, _, id, range)| (id, range))
}

pub(super) fn is_whole_table_span(doc: &Doc, block_id: u64, range: &Range<usize>) -> bool {
    doc.get(block_id).is_some_and(|b| b.meta.table.is_some() && (range.is_empty() || (range.start == 0 && range.end >= b.text.len())))
}

/// Paints comment anchors (`#fef0d1` / `#fdda8c` active or breathing while streaming, `#e5edfc` when applied).
pub(super) fn paint_highlights(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    let dark = crate::theme::is_dark();
    let (base, active_bg) = if dark { (HL_SELECT_BG_DARK, HL_ACTIVE_BG_DARK) } else { (HL_SELECT_BG, HL_ACTIVE_BG) };
    let (done_bg, done_flash) = if dark { (HL_DONE_BG_DARK, HL_DONE_FLASH_DARK) } else { (HL_DONE_BG, HL_DONE_FLASH) };
    for annotation in &editor.annotate.annotations {
        let Some((id, range, is_ai)) = resolve_annotation_span(&editor.doc, annotation) else { continue };
        if id != block || range.is_empty() || is_whole_table_span(&editor.doc, block, &range) { continue; }
        let active = editor.annotate.active_annotation == Some(annotation.id) || editor.annotate.hovered_annotation == Some(annotation.id);
        let color = if annotation.streaming {
            let level = crate::ui::fade::breathe(window);
            crate::ui::fade::mix(base, active_bg, level)
        } else if is_ai {
            let flash_t = crate::ui::fade::flash(annotation.id, window);
            let hover_t = crate::ui::fade::level(annotation.id, if active { 1. } else { 0. }, window);
            crate::ui::fade::mix(done_bg, done_flash, flash_t.max(hover_t))
        } else {
            let level = crate::ui::fade::level(annotation.id, if active { 1. } else { 0. }, window);
            crate::ui::fade::mix(base, active_bg, level)
        };
        for rect in layout.rects(range) {
            window.paint_quad(quad(rect, px(2.), color, px(0.), transparent_black(), BorderStyle::default()));
        }
    }
    let composing = editor.annotate.composer.as_ref().filter(|c| c.block_id == block && !c.range.is_empty() && !is_whole_table_span(&editor.doc, block, &c.range)).map(|c| c.range.clone());
    let reacting = editor.annotate.reactions.as_ref().filter(|p| p.block_id == block && !p.range.is_empty() && !is_whole_table_span(&editor.doc, block, &p.range)).map(|p| p.range.clone());
    for range in composing.into_iter().chain(reacting) {
        for rect in layout.rects(range) {
            window.paint_quad(quad(rect, px(2.), rgb(base), px(0.), transparent_black(), BorderStyle::default()));
        }
    }
}

/// Paints a 2px solid amber outline around commented/reacted whole tables (`--cds-border-editor-comment`).
pub(super) fn paint_table_highlights(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    if layout.cells.is_empty() { return; }
    let Some(grid) = super::table_layout::grid_rect(layout) else { return };
    let dark = crate::theme::is_dark();
    let (base, active) = if dark { (HL_TABLE_BORDER_DARK, HL_TABLE_BORDER_ACTIVE_DARK) } else { (HL_TABLE_BORDER, HL_TABLE_BORDER_ACTIVE) };
    let mut border_color = None;
    for annotation in &editor.annotate.annotations {
        let Some((id, range, _)) = resolve_annotation_span(&editor.doc, annotation) else { continue };
        if id != block || !is_whole_table_span(&editor.doc, block, &range) { continue; }
        let engaged = editor.annotate.active_annotation == Some(annotation.id) || editor.annotate.hovered_annotation == Some(annotation.id);
        let level = if annotation.streaming {
            crate::ui::fade::breathe(window)
        } else {
            crate::ui::fade::level(annotation.id, if engaged { 1. } else { 0. }, window)
        };
        border_color = Some(crate::ui::fade::mix(base, active, level));
        if engaged || annotation.streaming { break; }
    }
    if border_color.is_none() {
        let composing = editor.annotate.composer.as_ref().is_some_and(|c| c.block_id == block && is_whole_table_span(&editor.doc, block, &c.range));
        let reacting = editor.annotate.reactions.as_ref().is_some_and(|p| p.block_id == block && is_whole_table_span(&editor.doc, block, &p.range));
        if composing || reacting { border_color = Some(rgb(base).into()); }
    }
    if let Some(color) = border_color {
        window.paint_quad(quad(grid, px(0.), transparent_black(), px(2.), color, BorderStyle::Solid));
    }
}

impl Editor {
    pub(super) fn sync_annotations(&mut self) {
        if self.annotate.loaded_path != self.path {
            self.annotate.loaded_path = self.path.clone();
            self.annotate.annotations = self.path.as_deref().map(load_for).unwrap_or_default();
            self.annotate.composer = None;
            self.annotate.followup = None;
            self.annotate.editing = None;
            self.annotate.reactions = None;
            self.annotate.active_annotation = None;
            self.annotate.hovered_annotation = None;
            if self.normalize_annotations() {
                self.persist_annotations();
            }
        }
    }

    pub(super) fn persist_annotations(&self) {
        if let Some(path) = self.path.as_deref() { save_for(path, &self.annotate.annotations); }
    }

    pub(super) fn selected_annotation_target(&self) -> Option<(u64, usize, Range<usize>, String)> {
        let (a, _b, ai, _bi) = self.ordered();
        let block = self.doc.blocks.get(ai)?;
        if let Some(spec) = super::image_source::spec_of(block) {
            let label = if spec.alt.trim().is_empty() { spec.src } else { spec.alt };
            return Some((a.block, ai, 0..0, format!("[Image: {label}]")));
        }
        let range = self.selected_range(block.id).filter(|r| !r.is_empty()).unwrap_or(0..block.text.len());
        let quote = block.text.get(range.clone()).unwrap_or("").to_owned();
        (!quote.trim().is_empty()).then_some((a.block, ai, range, quote))
    }

    /// Toolbar 「添加批注」: highlight the selection and open the composer.
    pub(super) fn start_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.sync_annotations();
        let Some((block_id, block_index, range, quote)) = self.selected_annotation_target() else { return };
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(tr("输入批注或修改指令…", "Add an annotation or edit instruction…")));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::PressEnter { secondary: false, .. } => this.submit_annotation(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let focus_input = input.clone();
        self.annotate.reactions = None;
        self.format = None;
        self.annotate.composer = Some(AnnotateComposer { block_id, block_index, range, quote, input, _sub: sub });
        cx.defer_in(window, move |_, window, cx| { focus_input.update(cx, |i, cx| i.focus(window, cx)); });
        cx.notify();
    }

    pub(super) fn close_annotation_composer(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.annotate.composer = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }

    pub(super) fn submit_annotation(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(composer) = &self.annotate.composer else { return };
        let body = composer.input.read(cx).value().trim().to_owned();
        if body.is_empty() { return; }
        let Some(composer) = self.annotate.composer.take() else { return };
        self.annotate.annotations.push(Annotation {
            id: next_id(), block_index: composer.block_index, start: composer.range.start, end: composer.range.end,
            quote: composer.quote, body, reactions: Vec::new(), ai_rewritten: None, ai_error: None,
            applied: Some(false), streaming: false, request_id: None,
        });
        self.persist_annotations();
        self.caret.anchor = self.caret.head; // collapse selection so the floating toolbar doesn't reappear
        self.table_sel = None;
        self.toolbar_anchor = None;
        window.focus(&self.focus, cx);
        cx.notify();
    }
}
