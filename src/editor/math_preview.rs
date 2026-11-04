//! Async SVG preview for ```math / ```latex / ```tex fenced blocks, cached by source hash.
use std::{
    cell::RefCell,
    collections::HashMap,
    sync::{Arc, LazyLock},
};
use gpui::{AnyElement, Context, Entity, Image, ImageFormat};
use latex_rust::{
    Color, Dim, MathFont, MathStyle, SvgOptions,
    layout::layout_with_em_size_pt,
    parse,
    render::svg::render_svg,
};
use crate::theme::{Theme, is_dark, tr};
use super::{
    diagram_preview::{
        PreviewState, hash_source, placeholder, render_image, swap_svg_rb,
    },
    view::Editor,
};

const EM_PT: i64 = 18;
const VIEWBOX_PAD: f32 = 4.0;
const MAX_MATH_W: f32 = 650.0;

static MATH_FONT: LazyLock<MathFont> = LazyLock::new(|| {
    MathFont::stix_two_math().expect("embedded STIX Two Math font must load")
});

thread_local! {
    static CACHE: RefCell<HashMap<u64, PreviewState>> = Default::default();
    static LAST_READY: RefCell<HashMap<u64, (Arc<Image>, f32, f32)>> = Default::default();
}

fn parse_math(source: &str) -> Result<latex_rust::MathNode, String> {
    let trimmed = source.trim();
    match parse(trimmed) {
        Ok(ast) => Ok(ast),
        Err(first_err) => {
            if !trimmed.contains(r"\begin{") && (trimmed.contains(r"\\") || trimmed.contains('&')) {
                let wrapped = format!(r"\begin{{aligned}}{trimmed}\end{{aligned}}");
                if let Ok(ast) = parse(&wrapped) {
                    return Ok(ast);
                }
            }
            Err(concise_error(&first_err.to_string()))
        }
    }
}

fn concise_error(raw: &str) -> String {
    let line = raw.lines().find(|l| !l.trim().is_empty()).unwrap_or(tr("语法错误", "Syntax error")).trim();
    line.chars().take(120).collect()
}

fn dim_f32(d: &Dim) -> f32 {
    d.as_ratio().map(|(n, den)| (n as f64 / den as f64) as f32).unwrap_or(16.0)
}

fn render_math_svg(source: &str, dark: bool) -> Result<(Vec<u8>, f32, f32), String> {
    let ast = parse_math(source)?;
    let em = Dim::from_i64(EM_PT);
    let font = &*MATH_FONT;
    let tree = layout_with_em_size_pt(&ast, font, MathStyle::Display, &em)
        .map_err(|e| concise_error(&e.to_string()))?;
    let w_pt = dim_f32(&(tree.width.clone() * em.clone())).max(8.0);
    let h_pt = dim_f32(&((tree.height.clone() + tree.depth.clone()) * em.clone())).max(12.0);
    let color = if dark { Color::rgb(0xe6, 0xe9, 0xef) } else { Color::rgb(0x20, 0x24, 0x2c) };
    let opts = SvgOptions { font_size_pt: em, color, display: true };
    let raw_svg = render_svg(&tree, font, &opts).map_err(|e| concise_error(&e.to_string()))?;

    let vb_w = w_pt + VIEWBOX_PAD * 2.0;
    let vb_h = h_pt + VIEWBOX_PAD * 2.0;
    let scale = if vb_w > MAX_MATH_W { MAX_MATH_W / vb_w } else { 1.0 };
    let logical_w = (vb_w * scale).ceil().max(16.0);
    let logical_h = (vb_h * scale).ceil().max(20.0);

    let patched = match raw_svg.find('>') {
        Some(end) => format!(
            "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"{:.0}\" height=\"{:.0}\" viewBox=\"{:.2} {:.2} {:.2} {:.2}\">{}",
            (logical_w * 2.0).round(),
            (logical_h * 2.0).round(),
            -VIEWBOX_PAD,
            -VIEWBOX_PAD,
            vb_w,
            vb_h,
            &raw_svg[end + 1..]
        ),
        None => raw_svg,
    };
    Ok((swap_svg_rb(&patched).into_bytes(), logical_w, logical_h))
}

pub(super) fn render_state(
    editor: Entity<Editor>,
    block_id: u64,
    source: &str,
    cx: &mut Context<Editor>,
) -> (Option<AnyElement>, Option<String>) {
    let theme = Theme::ocean();
    if source.trim().is_empty() {
        return (Some(placeholder(tr("输入 LaTeX 公式以预览", "Enter LaTeX formula to preview"), &theme)), None);
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
                    render_math_svg(&src, dark)
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

fn fallback_or_loading(block_id: u64, theme: &Theme) -> AnyElement {
    if let Some((image, w, h)) = LAST_READY.with(|lr| lr.borrow().get(&block_id).cloned()) {
        render_image(image, w, h)
    } else {
        placeholder(tr("正在渲染公式…", "Rendering formula…"), theme)
    }
}
