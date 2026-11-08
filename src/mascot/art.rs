//! Mascot empty-state illustrations: Scheme C (Paper & Blueprint).
//!
//! Each empty state is a 4-layer vector composition (`280x220`) split into
//! `.grid.svg`, `.fill.svg`, `.line.svg`, and `.accent.svg` masks so GPUI can
//! tint every layer dynamically for Ocean Light and Ocean Dark.
use gpui::{Div, Hsla, div, prelude::*, px, svg};
use crate::theme::Theme;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MascotState {
    NoFolder,
    EmptyFolder,
    NoNote,
    ReadError,
    AiDisabled,
}

#[derive(Clone, Copy)]
enum Layer {
    Grid,
    Fill,
    Line,
    Accent,
}

macro_rules! mascot_assets {
    ($($slug:literal),* $(,)?) => {
        const SVG_ASSETS: &[(&str, &[u8])] = &[$(
            (
                concat!("mascot/", $slug, ".grid.svg"),
                include_bytes!(concat!("../../assets/mascot/", $slug, ".grid.svg")),
            ),
            (
                concat!("mascot/", $slug, ".fill.svg"),
                include_bytes!(concat!("../../assets/mascot/", $slug, ".fill.svg")),
            ),
            (
                concat!("mascot/", $slug, ".line.svg"),
                include_bytes!(concat!("../../assets/mascot/", $slug, ".line.svg")),
            ),
            (
                concat!("mascot/", $slug, ".accent.svg"),
                include_bytes!(concat!("../../assets/mascot/", $slug, ".accent.svg")),
            ),
        )*];

        fn layers_for(slug: &str) -> &'static [(Layer, &'static str)] {
            match slug {
                $(
                    $slug => &[
                        (Layer::Grid, concat!("mascot/", $slug, ".grid.svg")),
                        (Layer::Fill, concat!("mascot/", $slug, ".fill.svg")),
                        (Layer::Line, concat!("mascot/", $slug, ".line.svg")),
                        (Layer::Accent, concat!("mascot/", $slug, ".accent.svg")),
                    ],
                )*
                _ => &[],
            }
        }
    };
}

mascot_assets!(
    "no-folder",
    "empty-folder",
    "no-note",
    "read-error",
    "ai-disabled",
    "ai-greeting",
);

impl MascotState {
    const fn slug(self) -> &'static str {
        match self {
            Self::NoFolder => "no-folder",
            Self::EmptyFolder => "empty-folder",
            Self::NoNote => "no-note",
            Self::ReadError => "read-error",
            Self::AiDisabled => "ai-disabled",
        }
    }
}

pub fn load_asset(path: &str) -> Option<&'static [u8]> {
    SVG_ASSETS.iter().find(|(name, _)| *name == path).map(|(_, bytes)| *bytes)
}

const EMPTY_W: f32 = 280.;
const EMPTY_H: f32 = 220.;
const GREET_W: f32 = 280.;
const GREET_H: f32 = 220.;

fn blueprint_frame(slug: &str, max_w: f32, full_w: f32, full_h: f32) -> Div {
    let theme = Theme::ocean();
    let w = full_w.min(max_w.max(1.));
    let h = full_h * w / full_w;
    let mut root = div()
        .relative()
        .w(px(w))
        .h(px(h))
        .flex_none();

    for (layer, path) in layers_for(slug) {
        let color: Hsla = match layer {
            Layer::Grid => theme.hairline,
            Layer::Fill => theme.icon_fill,
            Layer::Line => theme.text,
            Layer::Accent => theme.accent,
        };
        root = root.child(
            svg()
                .absolute()
                .top_0()
                .left_0()
                .size_full()
                .path(*path)
                .text_color(color),
        );
    }
    root
}

pub fn illustration(state: MascotState) -> Div {
    illustration_within(state, EMPTY_W)
}

/// Same vector illustration as [`illustration`], never wider than `max_w`.
pub fn illustration_within(state: MascotState, max_w: f32) -> Div {
    blueprint_frame(
        state.slug(),
        max_w,
        EMPTY_W,
        EMPTY_H,
    )
}

/// The AI chat greeting in Scheme C (Paper & Blueprint).
pub fn ai_greeting_within(max_w: f32) -> Div {
    blueprint_frame(
        "ai-greeting",
        max_w,
        GREET_W,
        GREET_H,
    )
}

