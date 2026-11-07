use gpui::{Hsla, rgb};
use crate::theme::{Theme, is_dark};

fn color(lightness: f64, chroma: f64, hue: f64) -> Hsla {
    let angle = hue.to_radians();
    let a = chroma * angle.cos();
    let b = chroma * angle.sin();
    let l = (lightness + 0.3963377774 * a + 0.2158037573 * b).powi(3);
    let m = (lightness - 0.1055613458 * a - 0.0638541728 * b).powi(3);
    let s = (lightness - 0.0894841775 * a - 1.2914855480 * b).powi(3);
    let channel = |value: f64| -> u32 {
        let srgb = if value <= 0.0031308 { 12.92 * value } else { 1.055 * value.powf(1. / 2.4) - 0.055 };
        (srgb.clamp(0., 1.) * 255.).round() as u32
    };
    let red = channel(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s);
    let green = channel(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s);
    let blue = channel(-0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s);
    rgb((red << 16) | (green << 8) | blue).into()
}

pub fn theme() -> Theme {
    let mut theme = Theme::ocean();
    let dark = is_dark();
    let choose = |light: (f64, f64, f64), dark_value: (f64, f64, f64)| {
        let (l, c, h) = if dark { dark_value } else { light };
        color(l, c, h)
    };
    theme.text = choose((0.247, 0.006, 258.361), (0.964, 0.002, 247.839));
    theme.text_secondary = choose((0.506, 0.01, 264.477), (0.731, 0.008, 260.731));
    theme.paper = choose((1., 0., 0.), (0.26, 0.006, 271.191));
    theme.shell = choose((0.985, 0.001, 286.376), (0.209, 0.004, 264.477));
    theme.hover = choose((0.97, 0.002, 247.839), (0.289, 0.006, 271.22));
    theme.selected = choose((0.933, 0.003, 247.86), (0.318, 0.007, 274.747));
    theme.hairline = choose((0.946, 0.003, 264.542), (0.308, 0.006, 258.354));
    theme.btn_border = choose((0.912, 0.005, 258.326), (0.356, 0.007, 264.474));
    theme.accent = choose((0.626, 0.205, 254.947), (0.68, 0.173, 253.301));
    theme
}
