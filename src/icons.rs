//! The Duo two-color icon set. GPUI paints an SVG as a one-color mask, so each
//! icon is split into `.fill`, `.line` and `.accent` files that are stacked
//! and tinted separately.

use std::borrow::Cow;

use gpui::{AssetSource, Hsla, IntoElement, Pixels, Result, SharedString, div, prelude::*, svg};

/// Declares every icon with the layers its SVG files provide.
macro_rules! icons {
    ($($variant:ident => $name:literal [$($layer:ident),*]),* $(,)?) => {
        #[derive(Clone, Copy, Debug, PartialEq, Eq)]
        pub enum Icon { $($variant),* }

        impl Icon {
            fn layers(self) -> &'static [(Layer, &'static str)] {
                match self {
                    $(Icon::$variant => &[$(
                        (Layer::$layer, concat!("icons/", $name, ".", layer_name!($layer), ".svg"))
                    ),*]),*
                }
            }
        }

        const ASSETS: &[(&str, &[u8])] = &[$($(
            (
                concat!("icons/", $name, ".", layer_name!($layer), ".svg"),
                include_bytes!(concat!("../assets/icons/", $name, ".", layer_name!($layer), ".svg")),
            ),
        )*)*];
    };
}

macro_rules! layer_name {
    (Fill) => { "fill" };
    (Line) => { "line" };
    (Accent) => { "accent" };
}

#[derive(Clone, Copy)]
enum Layer {
    Fill,
    Line,
    Accent,
}

// Tree glyphs follow Duo-Tree: notes are scheme 2 (flat tab, inner rule);
// closed and open folders are scheme 3 (back sheet, front panel). Chrome
// icons stay the refined Duo set, which has no per-column variant.
icons! {
    Folder => "folder" [Fill, Line],
    FolderOpen => "folder-open" [Fill, Line],
    Note => "note" [Fill, Line],
    ChevronRight => "chevron-right" [Fill, Line],
    ChevronLeft => "chevron-left" [Fill, Line],
    Download => "download" [Fill, Line],
    Copy => "copy" [Fill, Line],
    Check => "check" [Fill, Line],
    ChevronDown => "chevron-down" [Fill, Line],
    Plus => "plus" [Line],
    Drag => "drag" [Line],
    Code => "code" [Fill, Line],
    Heading => "heading" [Fill, Line],
    Eraser => "eraser" [Fill, Line],
    Image => "image" [Fill, Line, Accent],
    AlignLeft => "align-left" [Line],
    AlignCenter => "align-center" [Line],
    AlignRight => "align-right" [Line],
    Trash => "trash" [Line],
    Text => "text" [Line],
    Heading1 => "heading-1" [Line],
    Heading2 => "heading-2" [Line],
    Heading3 => "heading-3" [Line],
    ListBullet => "list-bullet" [Line],
    ListNumbered => "list-numbered" [Line],
    Todo => "todo" [Line],
    Quote => "quote" [Line],
    Table => "table" [Line],
    Divider => "divider" [Line],
    Diagram => "diagram" [Line],
    Math => "math" [Line],
    ArrowUp => "arrow-up" [Line],
    ArrowDown => "arrow-down" [Line],
    Wrap => "wrap" [Line],
    SelectAll => "select-all" [Line],
    Source => "source" [Line],
    Duplicate => "duplicate" [Line],
    Cut => "cut" [Line],
    NotePlus => "note-plus" [Line],
    FolderPlus => "folder-plus" [Line],
    Pencil => "pencil" [Line],
    ArrowRight => "arrow-right" [Line],
    InsertAbove => "insert-above" [Line],
    InsertBelow => "insert-below" [Line],
    Sparkles => "sparkles" [Line],
    Close => "close" [Line],
    More => "more" [Line],
    FindSearch => "search" [Line],
    Sliders => "sliders" [Fill, Line],
    Stop => "stop" [Line],
    // Dark selection toolbar, link popover and image size pill (F-Inline).
    TbType => "tb-type" [Line],
    TbBold => "tb-bold" [Line],
    TbItalic => "tb-italic" [Line],
    TbUnderline => "tb-underline" [Line],
    TbStrike => "tb-strike" [Line],
    TbCode => "tb-code" [Line],
    TbPalette => "tb-palette" [Line],
    TbList => "tb-list" [Line],
    TbLink => "tb-link" [Line],
    TbImage => "tb-image" [Line],
    TbChevron => "tb-chevron" [Line],
    LinkOpen => "link-open" [Line],
    LinkCopy => "link-copy" [Line],
    LinkEdit => "link-edit" [Line],
    LinkOff => "link-off" [Line],
    TbMinus => "tb-minus" [Line],
    TbPlus => "tb-plus" [Line],
}

/// The three tints of one icon.
#[derive(Clone, Copy)]
pub struct IconColors {
    pub line: Hsla,
    pub fill: Hsla,
    pub accent: Hsla,
}

/// Serves the embedded icon SVGs to `svg().path(..)`.
pub struct Icons;

impl AssetSource for Icons {
    fn load(&self, path: &str) -> Result<Option<Cow<'static, [u8]>>> {
        Ok(ASSETS.iter().find(|(name, _)| *name == path).map(|(_, bytes)| Cow::Borrowed(*bytes)))
    }

    fn list(&self, path: &str) -> Result<Vec<SharedString>> {
        Ok(ASSETS
            .iter()
            .filter(|(name, _)| name.starts_with(path))
            .map(|(name, _)| SharedString::from(*name))
            .collect())
    }
}

/// Stacks the fill, line and accent layers, painted in that order.
pub fn icon(icon: Icon, size: Pixels, colors: IconColors) -> impl IntoElement {
    let mut root = div().relative().flex_none().size(size);
    for (layer, path) in icon.layers() {
        let color = match layer {
            Layer::Fill => colors.fill,
            Layer::Line => colors.line,
            Layer::Accent => colors.accent,
        };
        root = root.child(svg().absolute().top_0().left_0().size(size).path(*path).text_color(color));
    }
    root
}
