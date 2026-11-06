use gpui::{Hsla, rgb};

pub(super) fn category(index: usize) -> Hsla {
    const COLORS: [u32; 7] = [0x547dd0, 0x3ba57b, 0xd19348, 0xa870bc, 0xd16e6e, 0x6a9bab, 0x8c8d96];
    rgb(COLORS[index % COLORS.len()]).into()
}
