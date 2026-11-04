//! Tree-sitter highlighting through gpui-component; all ranges remain UTF-8 bytes.
use std::{cell::RefCell, collections::VecDeque, ops::Range, sync::{Arc, Once}};
use gpui::{HighlightStyle, TextRun};
use gpui_component::highlighter::{HighlightTheme, LanguageConfig, LanguageRegistry, SyntaxHighlighter};
use ropey::Rope;

type StyledRanges = Arc<[(Range<usize>, HighlightStyle)]>;
struct CachedHighlight { language: String, source: String, styles: StyledRanges }
thread_local! {
    // Bound memory while retaining visible blocks across repeated layout passes.
    static CACHE: RefCell<VecDeque<CachedHighlight>> = const { RefCell::new(VecDeque::new()) };
}

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
        register!("markdown", tree_sitter_md::LANGUAGE, tree_sitter_md::HIGHLIGHT_QUERY_BLOCK,
            tree_sitter_md::INJECTION_QUERY_BLOCK, ["markdown_inline", "html"]);
    });
}

// Grammar crates use Tree-sitter's text.* names; the component theme uses these names.
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
        _ => None,
    }
}

fn styles(source: &str, language: &str) -> StyledRanges {
    CACHE.with(|cache| {
        let mut cache = cache.borrow_mut();
        if let Some(index) = cache.iter().position(|entry| entry.language == language && entry.source == source) {
            let entry = cache.remove(index).unwrap();
            let styles = entry.styles.clone();
            cache.push_back(entry);
            return styles;
        }
        init_languages();
        let mut highlighter = SyntaxHighlighter::new(language);
        highlighter.update(None, &Rope::from_str(source), None);
        let styles: StyledRanges = highlighter.styles(&(0..source.len()), &*HighlightTheme::default_light()).into();
        // Large opaque blocks should not occupy the cache indefinitely.
        if source.len() <= 256 * 1024 {
            if cache.len() == 64 { cache.pop_front(); }
            cache.push_back(CachedHighlight { language: language.into(), source: source.into(), styles: styles.clone() });
        }
        styles
    })
}

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
