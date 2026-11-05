//! Per-style page geometry and type scale: card frame, padding, sizes, families, title mark.
use cosmic_text::Color;

use super::palette::ExportTheme;

/// How the big title (the file name) is set off from the body.
#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum TitleMark {
    /// Full-width rule under the title.
    Rule,
    /// Short accent bar under the title.
    Bar,
    /// Hand-drawn wavy pen underline (Thinkly stationery).
    Wavy,
    /// Marker highlight behind the lower half of the title.
    Highlight,
    /// Thick-thin newspaper double rule, title centered.
    DoubleRule,
    /// Full-bleed accent band with the title reversed out.
    Band,
    /// Block cursor after the title.
    Cursor,
}

#[derive(Clone, Copy)]
pub(super) struct Card {
    /// Distance from the page edge to the card.
    pub inset: f32,
    pub radius: f32,
    pub border: Option<Color>,
    /// Soft drop shadow color (alpha 0: none).
    pub shadow: Color,
}

pub(super) struct Spec {
    pub card: Option<Card>,
    /// `(top, right, bottom, left)` from the paper or card edge to the text column.
    pub pad: (f32, f32, f32, f32),
    pub body: f32,
    /// Body line height as a multiple of `body`.
    pub leading: f32,
    pub title: f32,
    /// In-content H1, H2, H3 sizes.
    pub heads: [f32; 3],
    pub mark: TitleMark,
    pub title_family: &'static str,
    pub heading_family: &'static str,
    pub body_family: &'static str,
    pub title_bold: bool,
}

const PING: &str = "PingFang SC";
const SONG: &str = "Songti SC";
const KAI: &str = "Kaiti SC";
/// Resolved to the system monospace family; it has no CJK glyphs, so only ASCII text uses it.
pub(super) const MONO: &str = "monospace";

const NO_SHADOW: Color = Color::rgba(0, 0, 0, 0);

impl ExportTheme {
    #[rustfmt::skip]
    pub(super) fn spec(self) -> Spec {
        let open = |mark, title, title_family, body_family| Spec {
            card: None, pad: (72.0, 72.0, 64.0, 72.0), body: 17.0, leading: 1.75, title,
            heads: [28.0, 22.0, 19.0], mark, title_family, heading_family: title_family,
            body_family, title_bold: true,
        };
        // Thinkly's stationery card at 1.2× (its 680px card scaled to the 816px page).
        let journal = |border| Spec {
            card: Some(Card { inset: 60.0, radius: 14.0, border: Some(border), shadow: Color::rgba(90, 60, 10, 16) }),
            pad: (54.0, 44.0, 44.0, 44.0), body: 18.0, leading: 1.85, title: 34.0,
            heads: [30.0, 25.0, 21.0], mark: TitleMark::Wavy, title_family: KAI,
            heading_family: KAI, body_family: PING, title_bold: false,
        };
        let floating = |border, shadow, mark| Spec {
            card: Some(Card { inset: 48.0, radius: 20.0, border, shadow }),
            pad: (56.0, 56.0, 52.0, 56.0), body: 17.0, leading: 1.75, title: 38.0,
            heads: [27.0, 21.0, 18.5], mark, title_family: PING, heading_family: PING,
            body_family: PING, title_bold: true,
        };
        match self {
            Self::Mono => open(TitleMark::Rule, 42.0, PING, PING),
            Self::Ink => open(TitleMark::Rule, 42.0, PING, PING),
            Self::Journal => journal(Color::rgb(240, 232, 208)),
            Self::Kraft => journal(Color::rgb(58, 53, 45)),
            Self::Riso => open(TitleMark::Highlight, 44.0, PING, PING),
            Self::Neon => open(TitleMark::Bar, 44.0, PING, PING),
            Self::Press => Spec { pad: (64.0, 72.0, 60.0, 72.0), body: 17.5, leading: 1.8, ..open(TitleMark::DoubleRule, 46.0, SONG, SONG) },
            Self::Poster => Spec { pad: (56.0, 72.0, 64.0, 72.0), ..open(TitleMark::Band, 50.0, PING, PING) },
            Self::Terminal => Spec { title_family: MONO, ..open(TitleMark::Cursor, 36.0, PING, PING) },
            Self::Film => Spec {
                card: Some(Card { inset: 40.0, radius: 10.0, border: None, shadow: NO_SHADOW }),
                pad: (56.0, 56.0, 52.0, 56.0), leading: 1.8, title_bold: false,
                ..open(TitleMark::Bar, 42.0, SONG, PING)
            },
            Self::Mist => floating(None, Color::rgba(30, 50, 90, 22), TitleMark::Bar),
            Self::Night => floating(Some(Color::rgb(38, 44, 54)), NO_SHADOW, TitleMark::Bar),
        }
    }
}
