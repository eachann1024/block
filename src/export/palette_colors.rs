//! Color tables for each paper style. Body text on paper passes WCAG AA (4.5:1) in every style.
use cosmic_text::Color;

use super::palette::{ExportTheme, Palette};

const fn hex(v: u32) -> Color {
    Color::rgb((v >> 16) as u8, (v >> 8) as u8, v as u8)
}

const CLEAR: Color = Color::rgba(0, 0, 0, 0);

#[rustfmt::skip]
pub(super) fn palette(theme: ExportTheme) -> Palette {
    // (canvas, inner, text, title, secondary, accent, accent2,
    //  code_bg, code_border, quote_bg, quote_fg, quote_bar, table_border, divider)
    let (dark, c) = match theme {
        // Plain white sheet, near-black ink.
        ExportTheme::Mono => (false, [hex(0xffffff), hex(0xffffff), hex(0x1f1e1d), hex(0x111111), hex(0x5e5d59), hex(0x111111), hex(0x111111),
            hex(0xf4f4f2), hex(0xe4e4e0), CLEAR, hex(0x1f1e1d), hex(0x111111), hex(0xd6d6d1), hex(0xdcdcd7)]),
        // Thinkly 「手账便签」: cream page, ivory card, amber ink.
        ExportTheme::Journal => (false, [hex(0xfef9e7), hex(0xfffef5), hex(0x1f1e1d), hex(0x1f1e1d), hex(0x5e5d59), hex(0xd97706), Color::rgba(252, 196, 57, 140),
            hex(0xfaf5e6), hex(0xe8dcc8), CLEAR, hex(0x5e5d59), hex(0xe8dcc8), hex(0xe8dcc8), hex(0xe8dcc8)]),
        // Two-ink risograph: navy text, fluorescent pink accents only behind or beside text.
        ExportTheme::Riso => (false, [hex(0xf4efe4), hex(0xf4efe4), hex(0x1f2a44), hex(0x1d3fa8), hex(0x4a5470), hex(0xff5aa8), hex(0x1d3fa8),
            hex(0xfbf7ee), hex(0x1d3fa8), hex(0xffe4f0), hex(0x1f2a44), hex(0xff5aa8), hex(0x9aa6cc), hex(0x1d3fa8)]),
        // Newsprint: warm paper, serif ink, brick-red markers.
        ExportTheme::Press => (false, [hex(0xf1ead9), hex(0xf1ead9), hex(0x23201b), hex(0x1a1814), hex(0x5a5244), hex(0x9c2b1f), hex(0x23201b),
            hex(0xe8dfcb), hex(0xb9ab8d), CLEAR, hex(0x23201b), hex(0x23201b), hex(0x8c826e), hex(0x23201b)]),
        // White page under a full-bleed vermilion title band.
        ExportTheme::Poster => (false, [hex(0xffffff), hex(0xffffff), hex(0x1c1c1c), hex(0x1c1c1c), hex(0x5c5c5c), hex(0xd8401a), hex(0xffffff),
            hex(0xf5f3f1), hex(0xebe6e1), hex(0xfff1ea), hex(0x1c1c1c), hex(0xd8401a), hex(0xe0dcd8), hex(0xe8e4e0)]),
        // Cool grey page, floating white card, calm blue.
        ExportTheme::Mist => (false, [hex(0xe9edf2), hex(0xffffff), hex(0x1e293b), hex(0x0f172a), hex(0x55627a), hex(0x3b6cf6), hex(0x3b6cf6),
            hex(0xf3f5f9), hex(0xe3e8ef), hex(0xf2f6fd), hex(0x1e293b), hex(0x3b6cf6), hex(0xdfe5ee), hex(0xe3e8ef)]),
        ExportTheme::Ink => (true, [hex(0x121212), hex(0x121212), hex(0xe6e6e6), hex(0xffffff), hex(0xa3a3a3), hex(0xededed), hex(0x3a3a3a),
            hex(0x1e1e1e), hex(0x2e2e2e), CLEAR, hex(0xf5f5f5), hex(0xededed), hex(0x3a3a3a), hex(0x3a3a3a)]),
        // The journal card on black stock.
        ExportTheme::Kraft => (true, [hex(0x1c1a17), hex(0x27241f), hex(0xece3cf), hex(0xfbf3df), hex(0xb3a78c), hex(0xf0c35a), Color::rgba(240, 195, 90, 120),
            hex(0x1f1d19), hex(0x4a4538), CLEAR, hex(0xc9bea4), hex(0x5c5446), hex(0x4a4538), hex(0x3d382f)]),
        ExportTheme::Neon => (true, [hex(0x14152b), hex(0x14152b), hex(0xecebff), hex(0xffffff), hex(0xa9a8d0), hex(0x3fe0d0), hex(0xff6a3d),
            hex(0x0f1024), hex(0x2e3170), hex(0x1d1f3d), hex(0xecebff), hex(0x3fe0d0), hex(0x34376e), hex(0x34376e)]),
        // Warm black frame on a film strip, amber safelight accent.
        ExportTheme::Film => (true, [hex(0x141210), hex(0x0b0a09), hex(0xe8e0d0), hex(0xfbf5e8), hex(0xa89c86), hex(0xf2a33a), hex(0xf2a33a),
            hex(0x1f1c18), hex(0x302b24), CLEAR, hex(0xfbf5e8), hex(0xf2a33a), hex(0x403a30), hex(0x302b24)]),
        // Midnight page, raised slate card, soft blue.
        ExportTheme::Night => (true, [hex(0x0d1117), hex(0x161b22), hex(0xd1d7e0), hex(0xf0f3f6), hex(0x8b949e), hex(0x7aa2f7), hex(0x7aa2f7),
            hex(0x0f1318), hex(0x262c36), hex(0x1b2230), hex(0xd1d7e0), hex(0x7aa2f7), hex(0x30363d), hex(0x262c36)]),
        // Phosphor green on black; body stays a soft grey-green for long reading.
        ExportTheme::Terminal => (true, [hex(0x0c0f0c), hex(0x0c0f0c), hex(0xd6e4d6), hex(0x8ee08a), hex(0x7f957f), hex(0x7ee787), hex(0x7ee787),
            hex(0x121812), hex(0x243024), CLEAR, hex(0xb8ccb8), hex(0x3f6b3f), hex(0x2a3a2a), hex(0x2a3a2a)]),
    };
    let [canvas, inner_canvas, text, title, secondary, accent, accent2, code_bg, code_border, quote_bg, quote_fg, quote_bar, table_border, divider] = c;
    Palette { dark, canvas, inner_canvas, text, title, secondary, accent, accent2, code_bg, code_border, quote_bg, quote_fg, quote_bar, table_border, divider }
}
