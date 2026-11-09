//! Render options and the 12 paper styles (6 light, 6 dark) the exporter paints with.
use cosmic_text::Color;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ExportTheme {
    #[default]
    Mono,
    Journal,
    Riso,
    Press,
    Poster,
    Mist,
    Ink,
    Kraft,
    Neon,
    Film,
    Night,
    Terminal,
}

pub use super::source::SourceContext;

#[derive(Clone, Debug, PartialEq)]
pub struct RenderOptions {
    pub theme: ExportTheme,
    pub card: bool,
    pub show_title: bool,
    pub footer: bool,
    pub title: String,
    pub source: SourceContext,
}

impl Default for RenderOptions {
    fn default() -> Self {
        Self {
            theme: ExportTheme::default(),
            card: false,
            show_title: true,
            footer: true,
            title: String::new(),
            source: SourceContext::default(),
        }
    }
}

pub(super) struct Palette {
    pub dark: bool,
    /// The page around the paper (card styles) or the paper itself.
    pub canvas: Color,
    /// The paper the text sits on: the card for card styles, else the canvas.
    pub inner_canvas: Color,
    pub text: Color,
    pub title: Color,
    pub secondary: Color,
    pub accent: Color,
    /// Text on the accent (Poster's title band) or the tape color for journal styles.
    pub accent2: Color,
    pub code_bg: Color,
    pub code_border: Color,
    pub quote_bg: Color,
    pub quote_fg: Color,
    pub quote_bar: Color,
    pub table_border: Color,
    pub divider: Color,
}

const LIGHT: [ExportTheme; 6] = [
    ExportTheme::Mono,
    ExportTheme::Journal,
    ExportTheme::Riso,
    ExportTheme::Press,
    ExportTheme::Poster,
    ExportTheme::Mist,
];
const DARK: [ExportTheme; 6] = [
    ExportTheme::Ink,
    ExportTheme::Kraft,
    ExportTheme::Neon,
    ExportTheme::Film,
    ExportTheme::Night,
    ExportTheme::Terminal,
];

impl ExportTheme {
    pub fn is_dark(self) -> bool {
        DARK.contains(&self)
    }

    pub fn label(self) -> &'static str {
        use crate::theme::tr;
        match self {
            Self::Mono => tr("黑白素纸", "Mono"),
            Self::Journal => tr("手账便签", "Journal"),
            Self::Riso => tr("孔版双色", "Risograph"),
            Self::Press => tr("铅字报刊", "Press"),
            Self::Poster => tr("大字海报", "Poster"),
            Self::Mist => tr("雾面卡片", "Mist"),
            Self::Ink => tr("墨黑", "Ink"),
            Self::Kraft => tr("黑卡手账", "Kraft"),
            Self::Neon => tr("霓虹夜色", "Neon"),
            Self::Film => tr("暗房胶片", "Film"),
            Self::Night => tr("夜读卡片", "Night"),
            Self::Terminal => tr("终端绿屏", "Terminal"),
        }
    }

    pub fn group(dark: bool) -> &'static [Self] {
        if dark { &DARK } else { &LIGHT }
    }

    pub fn default_for(dark: bool) -> Self {
        if dark { Self::Ink } else { Self::Mono }
    }

    /// `(paper_rgb, accent_rgb, inner_card_rgb)` for the sidebar swatch cards.
    pub fn swatch(self) -> (u32, u32, u32) {
        match self {
            Self::Mono => (0xffffff, 0x111111, 0xffffff),
            Self::Journal => (0xfef9e7, 0xd97706, 0xfffef5),
            Self::Riso => (0xf4efe4, 0xff5aa8, 0xf4efe4),
            Self::Press => (0xf1ead9, 0x9c2b1f, 0xf1ead9),
            Self::Poster => (0xffffff, 0xe2471b, 0xffffff),
            Self::Mist => (0xe9edf2, 0x3b6cf6, 0xffffff),
            Self::Ink => (0x121212, 0xededed, 0x121212),
            Self::Kraft => (0x1c1a17, 0xf0c35a, 0x27241f),
            Self::Neon => (0x14152b, 0x3fe0d0, 0x14152b),
            Self::Film => (0x141210, 0xf2a33a, 0x0b0a09),
            Self::Night => (0x0d1117, 0x7aa2f7, 0x161b22),
            Self::Terminal => (0x0c0f0c, 0x7ee787, 0x0c0f0c),
        }
    }

    /// Logical `(top, right, bottom, left)` distance from the page edge to the text column
    /// on an 816px page (card inset plus card padding for card styles).
    pub(super) fn margins(self) -> (f32, f32, f32, f32) {
        let spec = self.spec();
        let inset = spec.card.map_or(0.0, |c| c.inset);
        let (t, r, b, l) = spec.pad;
        (t + inset, r + inset, b + inset, l + inset)
    }

    pub(super) fn heading_family(self) -> &'static str {
        self.spec().heading_family
    }

    pub(super) fn body_family(self) -> &'static str {
        self.spec().body_family
    }

    pub(super) fn palette(self) -> Palette {
        super::palette_colors::palette(self)
    }
}

impl Palette {
    /// Explicit text color from the document; dark themes lift colors that would vanish.
    pub fn fg(&self, color: Color) -> Color {
        if !self.dark {
            return color;
        }
        let luma = (0.299 * color.r() as f32 + 0.587 * color.g() as f32 + 0.114 * color.b() as f32)
            / 255.0;
        if luma >= 0.5 {
            return color;
        }
        let t = (0.5 - luma) / 0.5 * 0.7 + 0.2;
        mix(color, Color::rgb(255, 255, 255), t)
    }

    /// Highlight / cell background from the document, kept subtle on dark canvases.
    pub fn bg(&self, color: Color) -> Color {
        if !self.dark || color.a() == 0 {
            return color;
        }
        let mixed = mix(color, self.inner_canvas, 0.78);
        Color::rgba(mixed.r(), mixed.g(), mixed.b(), color.a())
    }
}

fn mix(a: Color, b: Color, t: f32) -> Color {
    let lerp = |x: u8, y: u8| (x as f32 + (y as f32 - x as f32) * t).round() as u8;
    Color::rgb(lerp(a.r(), b.r()), lerp(a.g(), b.g()), lerp(a.b(), b.b()))
}
