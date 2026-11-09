//! Map paper colors into generated diagrams; source-authored colors stay with the renderer.
use super::palette::ExportTheme;
use cosmic_text::Color;
use mermaid_rs_renderer::Theme;

pub(super) fn mermaid(theme: ExportTheme) -> Theme {
    let pal = theme.palette();
    let color = |value: Color| format!("#{:02x}{:02x}{:02x}", value.r(), value.g(), value.b());
    let (text, accent, paper) = (color(pal.text), color(pal.accent), color(pal.inner_canvas));
    let (surface, header, border) = (color(pal.code_bg), color(pal.table_header_bg()), color(pal.table_border));
    let mut result = Theme::modern();
    result.font_family = format!("{}, Helvetica Neue, PingFang SC, sans-serif", theme.body_family());
    result.background = "transparent".into();
    result.primary_text_color = text.clone();
    result.text_color = text.clone();
    result.line_color = color(pal.secondary);
    result.primary_color = surface.clone();
    result.primary_border_color = accent.clone();
    result.secondary_color = header.clone();
    result.tertiary_color = paper.clone();
    result.edge_label_background = paper.clone();
    result.cluster_background = surface.clone();
    result.cluster_border = border.clone();
    result.sequence_actor_fill = surface;
    result.sequence_actor_border = accent.clone();
    result.sequence_actor_line = color(pal.secondary);
    result.sequence_note_fill = color(pal.highlight_bg());
    result.sequence_note_border = accent.clone();
    result.sequence_activation_fill = header.clone();
    result.sequence_activation_border = accent;
    result.git_branch_label_colors = std::array::from_fn(|_| text.clone());
    result.git_commit_label_color = text.clone();
    result.git_commit_label_background = paper.clone();
    result.git_tag_label_color = text.clone();
    result.git_tag_label_background = header;
    result.git_tag_label_border = border.clone();
    result.pie_title_text_color = text.clone();
    result.pie_section_text_color = text.clone();
    result.pie_legend_text_color = text;
    result.pie_stroke_color = border;
    result.pie_outer_stroke_color = color(pal.table_border);
    result
}
