//! The Duo two-color icon set. GPUI paints an SVG as a one-color mask, so each
//! icon is split into `.fill`, `.line` and `.accent` files that are stacked
//! and tinted separately.

use std::borrow::Cow;

use gpui::{AssetSource, Div, Hsla, IntoElement, Pixels, Result, SharedString, div, prelude::*, svg};

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
    Folder => "folder" [Fill, Line, Accent],
    FolderOpen => "folder-open" [Fill, Line, Accent],
    Note => "note" [Fill, Line, Accent],
    ChevronRight => "chevron-right" [Fill, Line, Accent],
    ChevronLeft => "chevron-left" [Fill, Line, Accent],
    Download => "download" [Fill, Line, Accent],
    Copy => "copy" [Fill, Line, Accent],
    Check => "check" [Fill, Line, Accent],
    ChevronDown => "chevron-down" [Fill, Line, Accent],
    Plus => "plus" [Fill, Line, Accent],
    Drag => "drag" [Fill, Line, Accent],
    Code => "code" [Fill, Line, Accent],
    Heading => "heading" [Fill, Line, Accent],
    Eraser => "eraser" [Fill, Line, Accent],
    Image => "image" [Fill, Line, Accent],
    AlignLeft => "align-left" [Fill, Line, Accent],
    AlignCenter => "align-center" [Fill, Line, Accent],
    AlignRight => "align-right" [Fill, Line, Accent],
    Crop => "crop" [Fill, Line, Accent],
    Trash => "trash" [Fill, Line, Accent],
    Text => "text" [Fill, Line, Accent],
    Heading1 => "heading-1" [Fill, Line, Accent],
    Heading2 => "heading-2" [Fill, Line, Accent],
    Heading3 => "heading-3" [Fill, Line, Accent],
    ListBullet => "list-bullet" [Fill, Line, Accent],
    ListNumbered => "list-numbered" [Fill, Line, Accent],
    Todo => "todo" [Fill, Line, Accent],
    Quote => "quote" [Fill, Line, Accent],
    Callout => "callout" [Fill, Line, Accent],
    Columns => "columns" [Fill, Line, Accent],
    Table => "table" [Fill, Line, Accent],
    Divider => "divider" [Fill, Line, Accent],
    Diagram => "diagram" [Fill, Line, Accent],
    Math => "math" [Fill, Line, Accent],
    ArrowUp => "arrow-up" [Fill, Line, Accent],
    ArrowDown => "arrow-down" [Fill, Line, Accent],
    Wrap => "wrap" [Fill, Line, Accent],
    SelectAll => "select-all" [Fill, Line, Accent],
    Source => "source" [Fill, Line, Accent],
    Duplicate => "duplicate" [Fill, Line, Accent],
    Cut => "cut" [Fill, Line, Accent],
    NotePlus => "note-plus" [Fill, Line, Accent],
    FolderPlus => "folder-plus" [Fill, Line, Accent],
    Pencil => "pencil" [Fill, Line, Accent],
    ArrowLeft => "arrow-left" [Fill, Line, Accent],
    ArrowRight => "arrow-right" [Fill, Line, Accent],
    Sparkles => "sparkles" [Fill, Line, Accent],
    Close => "close" [Fill, Line, Accent],
    More => "more" [Fill, Line, Accent],
    FindSearch => "search" [Fill, Line, Accent],
    Sliders => "sliders" [Fill, Line, Accent],
    Stop => "stop" [Fill, Line, Accent],
    MessageSquare => "message-square" [Fill, Line, Accent],
    History => "history" [Fill, Line, Accent],
    AiPlus => "ai-plus" [Fill, Line, Accent],
    // Selection toolbar, link popover and image size pill (F-Toolbar / F-Inline).
    TbComment => "tb-comment" [Fill, Line, Accent],
    TbReaction => "tb-reaction" [Fill, Line, Accent],
    TbType => "tb-type" [Fill, Line, Accent],
    TbBold => "tb-bold" [Fill, Line, Accent],
    TbItalic => "tb-italic" [Fill, Line, Accent],
    TbUnderline => "tb-underline" [Fill, Line, Accent],
    TbStrike => "tb-strike" [Fill, Line, Accent],
    TbCode => "tb-code" [Fill, Line, Accent],
    TbPalette => "tb-palette" [Fill, Line, Accent],
    TbList => "tb-list" [Fill, Line, Accent],
    TbLink => "tb-link" [Fill, Line, Accent],
    TbChevron => "tb-chevron" [Fill, Line, Accent],
    TbChevronRight => "tb-chevron-right" [Fill, Line, Accent],
    LinkOpen => "link-open" [Fill, Line, Accent],
    LinkCopy => "link-copy" [Fill, Line, Accent],
    LinkEdit => "link-edit" [Fill, Line, Accent],
    LinkOff => "link-off" [Fill, Line, Accent],
    TbMinus => "tb-minus" [Fill, Line, Accent],
    TbPlus => "tb-plus" [Fill, Line, Accent],
    TbReset => "tb-reset" [Fill, Line, Accent],
    NoteCheck => "note-check" [Fill, Line, Accent],
    HelpCircle => "help-circle" [Fill, Line, Accent],
    ChevronUp => "chevron-up" [Fill, Line, Accent],
    FolderReveal => "folder-reveal" [Fill, Line, Accent],
    SidebarCollapse => "sidebar-collapse" [Fill, Line, Accent],
    SidebarExpand => "sidebar-expand" [Fill, Line, Accent],
    ColInsertLeft => "col-insert-left" [Fill, Line, Accent],
    ColInsertRight => "col-insert-right" [Fill, Line, Accent],
    ColDelete => "col-delete" [Fill, Line, Accent],
    RowInsertAbove => "row-insert-above" [Fill, Line, Accent],
    RowInsertBelow => "row-insert-below" [Fill, Line, Accent],
    RowDelete => "row-delete" [Fill, Line, Accent],
    Minus => "minus" [Fill, Line, Accent],
    WindowMax => "window-max" [Fill, Line, Accent],
    WindowRestore => "window-restore" [Fill, Line, Accent],
    // Scheme A · Duo semantic menu icons (languages, tabs, inline AI, providers).
    LangPlain => "lang-plain" [Fill, Line, Accent],
    LangBash => "lang-bash" [Fill, Line, Accent],
    LangCss => "lang-css" [Fill, Line, Accent],
    LangHtml => "lang-html" [Fill, Line, Accent],
    LangJs => "lang-js" [Fill, Line, Accent],
    LangJson => "lang-json" [Fill, Line, Accent],
    LangLean => "lang-lean" [Fill, Line, Accent],
    LangPython => "lang-python" [Fill, Line, Accent],
    LangRust => "lang-rust" [Fill, Line, Accent],
    LangSql => "lang-sql" [Fill, Line, Accent],
    LangTs => "lang-ts" [Fill, Line, Accent],
    LangXml => "lang-xml" [Fill, Line, Accent],
    LangYaml => "lang-yaml" [Fill, Line, Accent],
    TabClose => "tab-close" [Fill, Line, Accent],
    TabCloseOthers => "tab-close-others" [Fill, Line, Accent],
    TabCloseRight => "tab-close-right" [Fill, Line, Accent],
    TabCloseAll => "tab-close-all" [Fill, Line, Accent],
    AiPolish => "ai-polish" [Fill, Line, Accent],
    AiSimplify => "ai-simplify" [Fill, Line, Accent],
    AiTransEn => "ai-trans-en" [Fill, Line, Accent],
    AiTransZh => "ai-trans-zh" [Fill, Line, Accent],
    AiJargon => "ai-jargon" [Fill, Line, Accent],
    AiColloquial => "ai-colloquial" [Fill, Line, Accent],
    AiList => "ai-list" [Fill, Line, Accent],
    AiTasks => "ai-tasks" [Fill, Line, Accent],
    ProvOpenai => "prov-openai" [Line],
    ProvAnthropic => "prov-anthropic" [Line],
    ProvDeepseek => "prov-deepseek" [Line],
    KeyCommand => "key-command" [Line],
    KeyOption => "key-option" [Line],
    KeyReturn => "key-return" [Line],
    KeyShift => "key-shift" [Line],
    KeyControl => "key-control" [Line],
    KeyBackspace => "key-backspace" [Line],
    Refresh => "refresh" [Fill, Line, Accent],
    Spinner => "spinner" [Line],
    UpdateTangle => "update-tangle" [Fill, Line, Accent],
    ToolThink => "tool-think" [Line],
    ToolWrite => "tool-write" [Line],
    ToolRun => "tool-run" [Line],
    ToolRead => "tool-read" [Line],
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
        let brand: Option<&'static [u8]> = match path {
            "agents/builtin.svg" => Some(include_bytes!("../assets/agents/builtin.svg")),
            "agents/codex.svg" => Some(include_bytes!("../assets/agents/codex.svg")),
            "agents/claude.svg" => Some(include_bytes!("../assets/agents/claude.svg")),
            "agents/dsh.svg" => Some(include_bytes!("../assets/agents/dsh.svg")),
            "agents/pi.svg" => Some(include_bytes!("../assets/agents/pi.svg")),
            "agents/agy.svg" => Some(include_bytes!("../assets/agents/agy.svg")),
            _ => None,
        };
        if let Some(bytes) = brand { return Ok(Some(Cow::Borrowed(bytes))); }
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
pub fn icon(icon: Icon, size: Pixels, colors: IconColors) -> Div {
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

/// Renders `active_colors` when `active` is true; otherwise renders `idle` by default
/// and switches to `active_colors` when the enclosing `.group(group)` is hovered.
pub fn stateful_icon(
    group: impl Into<SharedString>,
    glyph: Icon,
    size: Pixels,
    active: bool,
    idle: IconColors,
    active_colors: IconColors,
) -> Div {
    if active {
        return icon(glyph, size, active_colors);
    }
    let g: SharedString = group.into();
    div()
        .relative()
        .size(size)
        .flex_none()
        .child(
            div()
                .size(size)
                .group_hover(g.clone(), |s| s.invisible())
                .child(icon(glyph, size, idle)),
        )
        .child(
            div()
                .absolute()
                .top_0()
                .left_0()
                .size(size)
                .invisible()
                .group_hover(g, |s| s.visible())
                .child(icon(glyph, size, active_colors)),
        )
}

/// Single-silhouette brand glyph painted in one brand colour.
pub fn brand_icon(glyph: Icon, size: Pixels, brand: Hsla) -> impl IntoElement {
    icon(glyph, size, IconColors { line: brand, fill: brand, accent: brand })
}
