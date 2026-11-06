use gpui::{Div, Hsla, Styled, div, prelude::*, px, rgb, svg};
use crate::theme::{Theme, is_dark};

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

    fn asset_path(self, layer: &'static str) -> String {
        format!("mascot/{}.{layer}.svg", self.slug())
    }
}

pub fn load_asset(path: &str) -> Option<&'static [u8]> {
    Some(match path {
        "mascot/no-folder.body.svg" => include_bytes!("../../assets/mascot/no-folder.body.svg"),
        "mascot/no-folder.fill.svg" => include_bytes!("../../assets/mascot/no-folder.fill.svg"),
        "mascot/no-folder.accent.svg" => include_bytes!("../../assets/mascot/no-folder.accent.svg"),
        "mascot/no-folder.line.svg" => include_bytes!("../../assets/mascot/no-folder.line.svg"),
        "mascot/empty-folder.body.svg" => include_bytes!("../../assets/mascot/empty-folder.body.svg"),
        "mascot/empty-folder.fill.svg" => include_bytes!("../../assets/mascot/empty-folder.fill.svg"),
        "mascot/empty-folder.accent.svg" => include_bytes!("../../assets/mascot/empty-folder.accent.svg"),
        "mascot/empty-folder.line.svg" => include_bytes!("../../assets/mascot/empty-folder.line.svg"),
        "mascot/no-note.body.svg" => include_bytes!("../../assets/mascot/no-note.body.svg"),
        "mascot/no-note.fill.svg" => include_bytes!("../../assets/mascot/no-note.fill.svg"),
        "mascot/no-note.accent.svg" => include_bytes!("../../assets/mascot/no-note.accent.svg"),
        "mascot/no-note.line.svg" => include_bytes!("../../assets/mascot/no-note.line.svg"),
        "mascot/read-error.body.svg" => include_bytes!("../../assets/mascot/read-error.body.svg"),
        "mascot/read-error.fill.svg" => include_bytes!("../../assets/mascot/read-error.fill.svg"),
        "mascot/read-error.accent.svg" => include_bytes!("../../assets/mascot/read-error.accent.svg"),
        "mascot/read-error.line.svg" => include_bytes!("../../assets/mascot/read-error.line.svg"),
        "mascot/ai-disabled.body.svg" => include_bytes!("../../assets/mascot/ai-disabled.body.svg"),
        "mascot/ai-disabled.fill.svg" => include_bytes!("../../assets/mascot/ai-disabled.fill.svg"),
        "mascot/ai-disabled.accent.svg" => include_bytes!("../../assets/mascot/ai-disabled.accent.svg"),
        "mascot/ai-disabled.line.svg" => include_bytes!("../../assets/mascot/ai-disabled.line.svg"),
        _ => return None,
    })
}

pub fn illustration(state: MascotState) -> Div {
    let theme = Theme::ocean();
    let body_color: Hsla = if is_dark() { rgb(0x242933).into() } else { rgb(0xffffff).into() };
    div()
        .w(px(220.))
        .h(px(176.))
        .relative()
        .flex_none()
        .child(layer(state.asset_path("body"), body_color))
        .child(layer(state.asset_path("fill"), theme.icon_fill))
        .child(layer(state.asset_path("accent"), theme.accent))
        .child(layer(state.asset_path("line"), theme.text))
}

fn layer(path: String, color: Hsla) -> gpui::Svg {
    svg().path(path).size_full().absolute().top_0().left_0().text_color(color)
}
