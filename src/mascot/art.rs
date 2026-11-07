//! Mascot empty-state illustrations: one raster image per state.
//!
//! The five images are the blue-eyed white cat in the avatar's
//! Disney/Pixar fluffy-fur style (`assets/mascot/*.webp`, 880x704,
//! shown at 220x176). They are animated WebPs, built procedurally
//! from the approved stills: each rests for 15s, then plays one
//! short gentle motion. They are embedded with `include_bytes!` and
//! served through the app's `AssetSource`, so `gpui::img` loads them
//! by the `mascot/<slug>.webp` path.
//! The pictures have a white background, so they are shown as a
//! rounded white card in both light and dark mode.
use gpui::{Div, div, prelude::*, px};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MascotState {
    NoFolder,
    EmptyFolder,
    NoNote,
    ReadError,
    AiDisabled,
}

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

    fn asset_path(self) -> String {
        format!("mascot/{}.webp", self.slug())
    }
}

pub fn load_asset(path: &str) -> Option<&'static [u8]> {
    Some(match path {
        "mascot/no-folder.webp" => include_bytes!("../../assets/mascot/no-folder.webp"),
        "mascot/empty-folder.webp" => include_bytes!("../../assets/mascot/empty-folder.webp"),
        "mascot/no-note.webp" => include_bytes!("../../assets/mascot/no-note.webp"),
        "mascot/read-error.webp" => include_bytes!("../../assets/mascot/read-error.webp"),
        "mascot/ai-disabled.webp" => include_bytes!("../../assets/mascot/ai-disabled.webp"),
        "mascot/ai-tech-cat.webp" => include_bytes!("../../assets/mascot/ai-tech-cat.webp"),
        _ => return None,
    })
}

/// The `.id()` is required: GPUI keeps the animation frame state under
/// the element id, so an image without one never advances past frame 0.
pub fn illustration(state: MascotState) -> Div {
    div()
        .w(px(220.))
        .h(px(176.))
        .flex_none()
        .rounded(px(14.))
        .overflow_hidden()
        .child(
            gpui::img(state.asset_path())
                .id(format!("mascot-{}", state.slug()))
                .size_full(),
        )
}

/// The AI chat greeting: the tech cat (animated WebP, transparent bg).
/// Shown at 280x200, the image's own 1.4:1 aspect, as a rounded card.
/// It rests for 15s, blinks, rests another 15s, then raises a paw
/// to touch the glasses edge, taps twice, lowers it, and blinks.
/// The `.id()` is required: GPUI keeps the animation frame state under
/// the element id, so an image without one never advances past frame 0.
pub fn ai_greeting() -> Div {
    div()
        .w(px(280.))
        .h(px(200.))
        .flex_none()
        .rounded(px(14.))
        .overflow_hidden()
        .child(gpui::img("mascot/ai-tech-cat.webp").id("ai-greeting-cat").size_full())
}
