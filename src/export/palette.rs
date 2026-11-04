//! Render options and the 8 paper styles (4 light, 4 dark) the exporter paints with.
use cosmic_text::Color;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ExportTheme {
    #[default]
    Mono,
    Journal,
    Riso,
    Press,
    Ink,
    Kraft,
    Neon,
    Film,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RenderOptions {
    pub theme: ExportTheme,
    pub card: bool,
    pub show_title: bool,
    pub footer: bool,
    pub title: String,
}

impl Default for RenderOptions {
    fn default() -> Self {
        Self {
            theme: ExportTheme::default(),
            card: false,
            show_title: true,
            footer: true,
            title: String::new(),
        }
    }
}

pub(super) struct Palette {
    pub dark: bool,
    pub canvas: Color,
    pub inner_canvas: Color,
    pub text: Color,
    pub title: Color,
    pub secondary: Color,
    pub accent: Color,
    pub accent2: Color,
    pub code_bg: Color,
    pub code_border: Color,
    pub quote_bg: Color,
    pub quote_fg: Color,
    pub quote_bar: Color,
    pub table_border: Color,
    pub divider: Color,
}

impl ExportTheme {
    pub fn is_dark(self) -> bool {
        matches!(self, Self::Ink | Self::Kraft | Self::Neon | Self::Film)
    }

    pub fn label(self) -> &'static str {
        use crate::theme::tr;
        match self {
            Self::Mono => tr("黑白素纸", "Mono"),
            Self::Journal => tr("手账便签", "Journal"),
            Self::Riso => tr("野性孔版", "Risograph"),
            Self::Press => tr("铅字号外", "Press"),
            Self::Ink => tr("墨黑", "Ink"),
            Self::Kraft => tr("黑卡手账", "Kraft"),
            Self::Neon => tr("霓虹丝印", "Neon"),
            Self::Film => tr("暗房胶片", "Film"),
        }
    }

    pub fn group(dark: bool) -> [Self; 4] {
        if dark {
            [Self::Ink, Self::Kraft, Self::Neon, Self::Film]
        } else {
            [Self::Mono, Self::Journal, Self::Riso, Self::Press]
        }
    }

    pub fn default_for(dark: bool) -> Self {
        if dark { Self::Ink } else { Self::Mono }
    }

    /// `(paper_rgb, accent_rgb, inner_card_rgb)` for the sidebar swatch cards.
    pub fn swatch(self) -> (u32, u32, u32) {
        match self {
            Self::Mono => (0xffffff, 0x111111, 0xffffff),
            Self::Journal => (0xf6f0e1, 0xb5462f, 0xfff6b8),
            Self::Riso => (0xf3eee3, 0xff5aa8, 0xf3eee3),
            Self::Press => (0xece4d2, 0x9c2b1f, 0xe3d9c3),
            Self::Ink => (0x0e0e0e, 0xededed, 0x1c1c1c),
            Self::Kraft => (0x1d1b18, 0xf0c35a, 0x2c2a24),
            Self::Neon => (0x14152b, 0x3fe0d0, 0x1e2040),
            Self::Film => (0x141210, 0xf2a33a, 0x0a0908),
        }
    }

    /// Logical `(top, right, bottom, left)` padding around the content flow on an 816px page.
    pub(super) fn margins(self) -> (f32, f32, f32, f32) {
        match self {
            Self::Mono | Self::Ink => (64.0, 48.0, 44.0, 48.0),
            Self::Journal | Self::Kraft => (78.0, 56.0, 48.0, 92.0),
            Self::Riso | Self::Neon => (56.0, 48.0, 44.0, 48.0),
            Self::Press => (44.0, 48.0, 42.0, 48.0),
            Self::Film => (92.0, 60.0, 84.0, 60.0),
        }
    }

    pub(super) fn heading_family(self) -> &'static str {
        match self {
            Self::Journal | Self::Kraft => "Kaiti SC",
            Self::Press | Self::Film => "Songti SC",
            Self::Mono | Self::Riso | Self::Ink | Self::Neon => "PingFang SC",
        }
    }

    pub(super) fn body_family(self) -> &'static str {
        match self {
            Self::Press => "Songti SC",
            _ => "PingFang SC",
        }
    }

    pub(super) fn palette(self) -> Palette {
        match self {
            Self::Mono => Palette {
                dark: false,
                canvas: Color::rgb(255, 255, 255),
                inner_canvas: Color::rgb(255, 255, 255),
                text: Color::rgb(17, 17, 17),
                title: Color::rgb(17, 17, 17),
                secondary: Color::rgb(68, 68, 68),
                accent: Color::rgb(17, 17, 17),
                accent2: Color::rgb(17, 17, 17),
                code_bg: Color::rgb(242, 242, 242),
                code_border: Color::rgb(242, 242, 242),
                quote_bg: Color::rgba(0, 0, 0, 0),
                quote_fg: Color::rgb(17, 17, 17),
                quote_bar: Color::rgb(17, 17, 17),
                table_border: Color::rgb(180, 180, 180),
                divider: Color::rgb(17, 17, 17),
            },
            Self::Journal => Palette {
                dark: false,
                canvas: Color::rgb(246, 240, 225),
                inner_canvas: Color::rgb(246, 240, 225),
                text: Color::rgb(58, 50, 38),
                title: Color::rgb(47, 42, 32),
                secondary: Color::rgb(138, 122, 92),
                accent: Color::rgb(181, 70, 47),
                accent2: Color::rgb(245, 215, 122),
                code_bg: Color::rgb(251, 248, 239),
                code_border: Color::rgb(201, 185, 145),
                quote_bg: Color::rgb(255, 246, 184),
                quote_fg: Color::rgb(58, 50, 38),
                quote_bar: Color::rgb(181, 70, 47),
                table_border: Color::rgb(208, 195, 165),
                divider: Color::rgb(224, 161, 150),
            },
            Self::Riso => Palette {
                dark: false,
                canvas: Color::rgb(243, 238, 227),
                inner_canvas: Color::rgb(243, 238, 227),
                text: Color::rgb(29, 63, 168),
                title: Color::rgb(29, 63, 168),
                secondary: Color::rgb(47, 95, 208),
                accent: Color::rgb(255, 90, 168),
                accent2: Color::rgb(29, 63, 168),
                code_bg: Color::rgb(243, 238, 227),
                code_border: Color::rgb(29, 63, 168),
                quote_bg: Color::rgb(29, 63, 168),
                quote_fg: Color::rgb(243, 238, 227),
                quote_bar: Color::rgb(255, 90, 168),
                table_border: Color::rgb(29, 63, 168),
                divider: Color::rgb(29, 63, 168),
            },
            Self::Press => Palette {
                dark: false,
                canvas: Color::rgb(236, 228, 210),
                inner_canvas: Color::rgb(236, 228, 210),
                text: Color::rgb(34, 32, 27),
                title: Color::rgb(34, 32, 27),
                secondary: Color::rgb(90, 82, 68),
                accent: Color::rgb(156, 43, 31),
                accent2: Color::rgb(34, 32, 27),
                code_bg: Color::rgb(227, 217, 195),
                code_border: Color::rgb(34, 32, 27),
                quote_bg: Color::rgba(0, 0, 0, 0),
                quote_fg: Color::rgb(34, 32, 27),
                quote_bar: Color::rgb(34, 32, 27),
                table_border: Color::rgb(34, 32, 27),
                divider: Color::rgb(34, 32, 27),
            },
            Self::Ink => Palette {
                dark: true,
                canvas: Color::rgb(14, 14, 14),
                inner_canvas: Color::rgb(14, 14, 14),
                text: Color::rgb(237, 237, 237),
                title: Color::rgb(255, 255, 255),
                secondary: Color::rgb(168, 168, 168),
                accent: Color::rgb(237, 237, 237),
                accent2: Color::rgb(58, 58, 58),
                code_bg: Color::rgb(28, 28, 28),
                code_border: Color::rgb(58, 58, 58),
                quote_bg: Color::rgba(0, 0, 0, 0),
                quote_fg: Color::rgb(255, 255, 255),
                quote_bar: Color::rgb(237, 237, 237),
                table_border: Color::rgb(58, 58, 58),
                divider: Color::rgb(58, 58, 58),
            },
            Self::Kraft => Palette {
                dark: true,
                canvas: Color::rgb(29, 27, 24),
                inner_canvas: Color::rgb(29, 27, 24),
                text: Color::rgb(236, 227, 207),
                title: Color::rgb(251, 243, 223),
                secondary: Color::rgb(179, 167, 140),
                accent: Color::rgb(240, 195, 90),
                accent2: Color::rgb(224, 122, 95),
                code_bg: Color::rgb(19, 18, 16),
                code_border: Color::rgb(92, 85, 70),
                quote_bg: Color::rgb(44, 42, 36),
                quote_fg: Color::rgb(251, 243, 223),
                quote_bar: Color::rgb(74, 69, 56),
                table_border: Color::rgb(74, 69, 56),
                divider: Color::rgb(140, 74, 62),
            },
            Self::Neon => Palette {
                dark: true,
                canvas: Color::rgb(20, 21, 43),
                inner_canvas: Color::rgb(20, 21, 43),
                text: Color::rgb(241, 236, 255),
                title: Color::rgb(255, 255, 255),
                secondary: Color::rgb(63, 224, 208),
                accent: Color::rgb(63, 224, 208),
                accent2: Color::rgb(255, 106, 61),
                code_bg: Color::rgb(20, 21, 43),
                code_border: Color::rgb(63, 224, 208),
                quote_bg: Color::rgb(63, 224, 208),
                quote_fg: Color::rgb(20, 21, 43),
                quote_bar: Color::rgb(255, 106, 61),
                table_border: Color::rgb(63, 224, 208),
                divider: Color::rgb(63, 224, 208),
            },
            Self::Film => Palette {
                dark: true,
                canvas: Color::rgb(20, 18, 16),
                inner_canvas: Color::rgb(10, 9, 8),
                text: Color::rgb(232, 224, 208),
                title: Color::rgb(251, 245, 232),
                secondary: Color::rgb(168, 156, 134),
                accent: Color::rgb(242, 163, 58),
                accent2: Color::rgb(242, 163, 58),
                code_bg: Color::rgb(31, 28, 24),
                code_border: Color::rgb(48, 43, 36),
                quote_bg: Color::rgba(0, 0, 0, 0),
                quote_fg: Color::rgb(251, 245, 232),
                quote_bar: Color::rgb(242, 163, 58),
                table_border: Color::rgb(64, 58, 48),
                divider: Color::rgb(168, 156, 134),
            },
        }
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
