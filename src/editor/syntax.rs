//! Tree-sitter highlighting through gpui-component; all ranges remain UTF-8 bytes.
#[cfg(not(target_family = "wasm"))]
use std::{cell::RefCell, collections::VecDeque, ops::Range, sync::{Arc, LazyLock, Once}};
#[cfg(not(target_family = "wasm"))]
use gpui::HighlightStyle;
use gpui::TextRun;
#[cfg(not(target_family = "wasm"))]
use gpui_component::highlighter::{HighlightTheme, LanguageConfig, LanguageRegistry, SyntaxHighlighter};
#[cfg(not(target_family = "wasm"))]
use ropey::Rope;

#[cfg(not(target_family = "wasm"))]
type StyledRanges = Arc<[(Range<usize>, HighlightStyle)]>;
#[cfg(not(target_family = "wasm"))]
struct CachedHighlight { language: String, source: String, dark: bool, styles: StyledRanges }
#[cfg(not(target_family = "wasm"))]
thread_local! {
    // Bound memory while retaining visible blocks across repeated layout passes.
    static CACHE: RefCell<VecDeque<CachedHighlight>> = const { RefCell::new(VecDeque::new()) };
}

#[cfg(not(target_family = "wasm"))]
static DARK_THEME: LazyLock<HighlightTheme> = LazyLock::new(|| {
    serde_json::from_str(r##"{
        "name": "GitHub Ice Dark",
        "appearance": "dark",
        "style": {
            "editor.foreground": "#e6edf3",
            "editor.background": "#1d2026",
            "syntax": {
                "attribute": { "color": "#79c0ff" },
                "boolean": { "color": "#79c0ff" },
                "comment": { "color": "#87909c" },
                "comment.doc": { "color": "#87909c" },
                "constant": { "color": "#79c0ff" },
                "constructor": { "color": "#ffa657" },
                "embedded": { "color": "#e6edf3" },
                "emphasis": { "font_style": "italic" },
                "emphasis.strong": { "font_weight": 700 },
                "enum": { "color": "#ffa657" },
                "function": { "color": "#d2a8ff" },
                "keyword": { "color": "#ff7b72" },
                "label": { "color": "#79c0ff" },
                "link_text": { "color": "#a5d6ff" },
                "link_uri": { "color": "#79c0ff", "font_style": "italic" },
                "number": { "color": "#79c0ff" },
                "operator": { "color": "#79c0ff" },
                "preproc": { "color": "#ff7b72" },
                "property": { "color": "#7ee787" },
                "punctuation": { "color": "#8b949e" },
                "punctuation.bracket": { "color": "#8b949e" },
                "punctuation.delimiter": { "color": "#8b949e" },
                "punctuation.list_marker": { "color": "#79c0ff" },
                "punctuation.special": { "color": "#ff7b72" },
                "string": { "color": "#a5d6ff" },
                "string.escape": { "color": "#79c0ff" },
                "string.regex": { "color": "#a5d6ff" },
                "string.special": { "color": "#a5d6ff" },
                "string.special.symbol": { "color": "#79c0ff" },
                "tag": { "color": "#7ee787" },
                "tag.doctype": { "color": "#ff7b72" },
                "text.code.span": { "color": "#a5d6ff" },
                "text.literal": { "color": "#a5d6ff" },
                "title": { "color": "#79c0ff", "font_weight": 600 },
                "type": { "color": "#ffa657" },
                "variable": { "color": "#e6edf3" },
                "variable.special": { "color": "#ff7b72" },
                "variant": { "color": "#ffa657" }
            }
        }
    }"##).expect("valid dark highlight theme JSON")
});

#[cfg(not(target_family = "wasm"))]
fn init_languages() {
    static INIT: Once = Once::new();
    INIT.call_once(|| {
        let registry = LanguageRegistry::singleton();
        let mut plain = registry.language("json").expect("built-in JSON grammar");
        plain.name = "text".into();
        plain.highlights = "".into();
        registry.register("text", &plain);
        macro_rules! register {
            ($name:expr, $grammar:expr, $highlights:expr, $injections:expr, [$($dependency:expr),*]) => {{
                let config = LanguageConfig::new(
                    $name, $grammar.into(), vec![$($dependency.into()),*],
                    &normalize_captures($highlights), $injections, "",
                );
                registry.register($name, &config);
            }};
        }
        register!("python", tree_sitter_python::LANGUAGE,
            tree_sitter_python::HIGHLIGHTS_QUERY, "", []);
        register!("rust", tree_sitter_rust::LANGUAGE,
            tree_sitter_rust::HIGHLIGHTS_QUERY, "", []);
        let javascript = format!("{}\n{}", tree_sitter_javascript::HIGHLIGHT_QUERY,
            tree_sitter_javascript::JSX_HIGHLIGHT_QUERY);
        register!("javascript", tree_sitter_javascript::LANGUAGE, &javascript, "", []);
        let typescript = format!("{}\n{}", tree_sitter_javascript::HIGHLIGHT_QUERY,
            tree_sitter_typescript::HIGHLIGHTS_QUERY);
        register!("typescript", tree_sitter_typescript::LANGUAGE_TYPESCRIPT, &typescript, "", []);
        let tsx = format!("{}\n{}", javascript, tree_sitter_typescript::HIGHLIGHTS_QUERY);
        register!("tsx", tree_sitter_typescript::LANGUAGE_TSX, &tsx, "", []);
        register!("css", tree_sitter_css::LANGUAGE, tree_sitter_css::HIGHLIGHTS_QUERY, "", []);
        register!("html", tree_sitter_html::LANGUAGE, tree_sitter_html::HIGHLIGHTS_QUERY,
            tree_sitter_html::INJECTIONS_QUERY, ["javascript", "css"]);
        register!("markdown_inline", tree_sitter_md::INLINE_LANGUAGE,
            tree_sitter_md::HIGHLIGHT_QUERY_INLINE, "", []);
        register!("yaml", tree_sitter_yaml::LANGUAGE, tree_sitter_yaml::HIGHLIGHTS_QUERY, "", []);
        register!("markdown", tree_sitter_md::LANGUAGE, tree_sitter_md::HIGHLIGHT_QUERY_BLOCK,
            tree_sitter_md::INJECTION_QUERY_BLOCK, ["markdown_inline", "html", "yaml"]);
    });
}

// Grammar crates use Tree-sitter's text.* names; the component theme uses these names.
#[cfg(not(target_family = "wasm"))]
fn normalize_captures(query: &str) -> String {
    let mut query = query.to_owned();
    for (from, to) in [
        ("@text.title", "@title"), ("@text.emphasis", "@emphasis"),
        ("@text.strong", "@emphasis.strong"), ("@text.uri", "@link_uri"),
        ("@text.reference", "@link_text"),
    ] {
        query = query.replace(from, to);
    }
    query
}

#[cfg(not(target_family = "wasm"))]
fn language_name(language: &str) -> Option<&'static str> {
    match language.split_whitespace().next().unwrap_or("").to_ascii_lowercase().as_str() {
        "python" | "py" => Some("python"),
        "rust" | "rs" => Some("rust"),
        "javascript" | "js" | "jsx" => Some("javascript"),
        "typescript" | "ts" => Some("typescript"),
        "tsx" => Some("tsx"),
        "json" | "jsonc" => Some("json"),
        "markdown" | "md" => Some("markdown"),
        "html" | "htm" => Some("html"),
        "css" => Some("css"),
        "yaml" | "yml" | "yaml-frontmatter" | "frontmatter" => Some("yaml"),
        _ => None,
    }
}

#[cfg(not(target_family = "wasm"))]
fn styles(source: &str, language: &str) -> StyledRanges {
    let dark = crate::theme::is_dark();
    CACHE.with(|cache| {
        let mut cache = cache.borrow_mut();
        if let Some(index) = cache.iter().position(|entry| entry.language == language && entry.dark == dark && entry.source == source) {
            let entry = cache.remove(index).unwrap();
            let styles = entry.styles.clone();
            cache.push_back(entry);
            return styles;
        }
        init_languages();
        let mut highlighter = SyntaxHighlighter::new(language);
        highlighter.update(None, &Rope::from_str(source), None);
        let theme: &HighlightTheme = if dark { &DARK_THEME } else { &HighlightTheme::default_light() };
        let styles: StyledRanges = highlighter.styles(&(0..source.len()), theme).into();
        // Large opaque blocks should not occupy the cache indefinitely.
        if source.len() <= 256 * 1024 {
            if cache.len() == 64 { cache.pop_front(); }
            cache.push_back(CachedHighlight { language: language.into(), source: source.into(), dark, styles: styles.clone() });
        }
        styles
    })
}

#[cfg(target_family = "wasm")]
pub(in crate::editor) fn runs(source: &str, _language: &str, mut base: TextRun) -> Vec<TextRun> {
    if source.is_empty() { return Vec::new(); }
    base.len = source.len();
    vec![base]
}

#[cfg(not(target_family = "wasm"))]
pub(in crate::editor) fn runs(source: &str, language: &str, mut base: TextRun) -> Vec<TextRun> {
    if source.is_empty() { return Vec::new(); }
    base.len = source.len();
    let Some(language) = language_name(language) else { return vec![base]; };
    let mut runs = Vec::new();
    let mut cursor = 0;
    for (range, style) in styles(source, language).iter() {
        if range.start < cursor || range.start >= range.end || range.end > source.len()
            || !source.is_char_boundary(range.start) || !source.is_char_boundary(range.end) { continue; }
        if cursor < range.start {
            let mut run = base.clone(); run.len = range.start - cursor; runs.push(run);
        }
        let mut run = base.clone();
        run.len = range.len();
        if let Some(color) = style.color { run.color = color; }
        if let Some(weight) = style.font_weight { run.font.weight = weight; }
        if let Some(font_style) = style.font_style { run.font.style = font_style; }
        if let Some(color) = style.background_color { run.background_color = Some(color); }
        if let Some(underline) = style.underline { run.underline = Some(underline); }
        if let Some(strikethrough) = style.strikethrough { run.strikethrough = Some(strikethrough); }
        runs.push(run);
        cursor = range.end;
    }
    if cursor < source.len() { base.len = source.len() - cursor; runs.push(base); }
    runs
}
