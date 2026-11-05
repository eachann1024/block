//! Async SVG preview for ```mermaid fenced blocks, cached by source hash.
use std::{
    cell::RefCell,
    collections::HashMap,
    hash::{DefaultHasher, Hash, Hasher},
    sync::Arc,
};
use gpui::{AnyElement, Context, Entity, Image, ImageFormat, div, img, prelude::*, px};
use mermaid_rs_renderer::{
    LayoutConfig, Theme as MermaidTheme, compute_layout, measure_svg_dimensions,
    parse_mermaid_strict, render::render_svg_with_dimensions,
};
use crate::theme::{Theme, is_dark, tr};
use super::view::Editor;

const MAX_PREVIEW_W: f32 = 650.;

#[derive(Clone)]
pub(super) enum PreviewState {
    Pending,
    Ready { image: Arc<Image>, width: f32, height: f32 },
    Error(String),
}

thread_local! {
    static CACHE: RefCell<HashMap<u64, PreviewState>> = Default::default();
    static LAST_READY: RefCell<HashMap<u64, (Arc<Image>, f32, f32)>> = Default::default();
}

pub(super) fn hash_source(source: &str, dark: bool) -> u64 {
    let mut hasher = DefaultHasher::new();
    source.hash(&mut hasher);
    dark.hash(&mut hasher);
    hasher.finish()
}

/// GPUI 0.2.2 uploads `ImageFormat::Svg` pixmaps directly to BGRA8Unorm without R/B swap.
/// Swapping `#RRGGBB` / `#RGB` channels in the SVG keeps rendered colours faithful on Metal.
pub(super) fn swap_svg_rb(svg: &str) -> String {
    let bytes = svg.as_bytes();
    let mut out = String::with_capacity(svg.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'#' {
            let rem = &bytes[i + 1..];
            let hex_len = rem.iter().take_while(|b| b.is_ascii_hexdigit()).count();
            if hex_len == 6 && (i + 7 == bytes.len() || !bytes[i + 7].is_ascii_alphanumeric()) {
                let h = &svg[i + 1..i + 7];
                out.push('#');
                out.push_str(&h[4..6]);
                out.push_str(&h[2..4]);
                out.push_str(&h[0..2]);
                i += 7;
                continue;
            } else if hex_len == 3 && (i + 4 == bytes.len() || !bytes[i + 4].is_ascii_alphanumeric()) {
                let h = &svg[i + 1..i + 4];
                out.push('#');
                out.push_str(&h[2..3]);
                out.push_str(&h[1..2]);
                out.push_str(&h[0..1]);
                i += 4;
                continue;
            }
        }
        let ch = svg[i..].chars().next().unwrap();
        out.push(ch);
        i += ch.len_utf8();
    }
    out
}

fn mermaid_theme(dark: bool) -> MermaidTheme {
    let mut t = MermaidTheme::modern();
    t.font_family = "Helvetica Neue, PingFang SC, Arial, sans-serif".into();
    t.background = "transparent".into();
    if dark {
        let fg = "#e6e9ef";
        t.primary_text_color = fg.into();
        t.text_color = fg.into();
        t.line_color = "#9aa3b2".into();
        t.primary_color = "#1e222a".into();
        t.primary_border_color = "#6c9eff".into();
        t.secondary_color = "#232a38".into();
        t.tertiary_color = "#1b1f27".into();
        t.edge_label_background = "#16191e".into();
        t.cluster_background = "#1b1f26".into();
        t.cluster_border = "#343b48".into();
        t.sequence_actor_fill = "#1e222a".into();
        t.sequence_actor_border = "#6c9eff".into();
        t.sequence_actor_line = "#636d80".into();
        t.sequence_note_fill = "#2d2818".into();
        t.sequence_note_border = "#8a7030".into();
        t.sequence_activation_fill = "#232d42".into();
        t.sequence_activation_border = "#6c9eff".into();
        t.pie_title_text_color = fg.into();
        t.pie_section_text_color = fg.into();
        t.pie_legend_text_color = fg.into();
        t.pie_stroke_color = "#16191e".into();
    } else {
        let fg = "#20242c";
        t.primary_text_color = fg.into();
        t.text_color = fg.into();
        t.line_color = "#526071".into();
        t.primary_color = "#ffffff".into();
        t.primary_border_color = "#4f7cff".into();
        t.secondary_color = "#eef3ff".into();
        t.tertiary_color = "#f8f9fb".into();
        t.edge_label_background = "#f5f6f8".into();
        t.cluster_background = "#eef0f4".into();
        t.cluster_border = "#cfd6e0".into();
        t.sequence_actor_fill = "#ffffff".into();
        t.sequence_actor_border = "#4f7cff".into();
        t.sequence_actor_line = "#8a94a6".into();
        t.pie_title_text_color = fg.into();
        t.pie_section_text_color = fg.into();
        t.pie_legend_text_color = fg.into();
        t.pie_stroke_color = "#f5f6f8".into();
    }
    t
}

fn render_mermaid_svg(source: &str, dark: bool) -> Result<(Vec<u8>, f32, f32), String> {
    let parsed = parse_mermaid_strict(source).map_err(|e| concise_error(&e.to_string()))?;
    let theme = mermaid_theme(dark);
    let config = LayoutConfig::default();
    let layout = compute_layout(&parsed.graph, &theme, &config);
    let dims = measure_svg_dimensions(&layout, &config, None);
    let raw_w = dims.width.max(40.);
    let raw_h = dims.height.max(24.);
    let scale = if raw_w > MAX_PREVIEW_W { MAX_PREVIEW_W / raw_w } else { 1. };
    let logical_w = (raw_w * scale).round().max(40.);
    let logical_h = (raw_h * scale).round().max(24.);
    let svg = render_svg_with_dimensions(
        &layout,
        &theme,
        &config,
        Some(((logical_w * 2.).round(), (logical_h * 2.).round())),
    );
    Ok((swap_svg_rb(&svg).into_bytes(), logical_w, logical_h))
}

fn concise_error(raw: &str) -> String {
    let line = raw.lines().find(|l| !l.trim().is_empty()).unwrap_or(tr("语法错误", "Syntax error")).trim();
    line.chars().take(120).collect()
}

pub(super) fn render_state(
    editor: Entity<Editor>,
    block_id: u64,
    source: &str,
    cx: &mut Context<Editor>,
) -> (Option<AnyElement>, Option<String>) {
    let theme = Theme::ocean();
    if source.trim().is_empty() {
        return (Some(placeholder(tr("输入 Mermaid 代码以预览图表", "Enter Mermaid code to preview"), &theme)), None);
    }
    let dark = is_dark();
    let key = hash_source(source, dark);
    let state = CACHE.with(|c| c.borrow().get(&key).cloned());
    match state {
        Some(PreviewState::Ready { image, width, height }) => {
            LAST_READY.with(|lr| { lr.borrow_mut().insert(block_id, (image.clone(), width, height)); });
            (Some(render_image(image, width, height)), None)
        }
        Some(PreviewState::Error(msg)) => (None, Some(msg)),
        Some(PreviewState::Pending) => (Some(fallback_or_loading(block_id, &theme)), None),
        None => {
            CACHE.with(|c| { c.borrow_mut().insert(key, PreviewState::Pending); });
            let src = source.to_owned();
            let entity = editor.downgrade();
            cx.spawn(async move |_, cx| {
                let res = cx.background_executor().spawn(async move {
                    render_mermaid_svg(&src, dark)
                }).await;
                CACHE.with(|c| {
                    let next = match res {
                        Ok((bytes, width, height)) => PreviewState::Ready {
                            image: Arc::new(Image::from_bytes(ImageFormat::Svg, bytes)),
                            width,
                            height,
                        },
                        Err(msg) => PreviewState::Error(msg),
                    };
                    c.borrow_mut().insert(key, next);
                });
                let _ = entity.update(cx, |_, cx| cx.notify());
            }).detach();
            (Some(fallback_or_loading(block_id, &theme)), None)
        }
    }
}

pub(super) fn render_image(image: Arc<Image>, width: f32, height: f32) -> AnyElement {
    let zoom = super::zoom::zoom_factor();
    div().w_full().flex().items_center().justify_center().py(px(6.))
        .child(img(image).w(px(width * zoom)).h(px(height * zoom)).flex_none())
        .into_any_element()
}

pub(super) fn placeholder(text: &'static str, theme: &Theme) -> AnyElement {
    div().w_full().py(px(10.)).flex().items_center().justify_center()
        .text_size(px(14.)).text_color(theme.text_secondary)
        .child(text)
        .into_any_element()
}

fn fallback_or_loading(block_id: u64, theme: &Theme) -> AnyElement {
    if let Some((image, w, h)) = LAST_READY.with(|lr| lr.borrow().get(&block_id).cloned()) {
        render_image(image, w, h)
    } else {
        placeholder(tr("正在渲染图表…", "Rendering diagram…"), theme)
    }
}
