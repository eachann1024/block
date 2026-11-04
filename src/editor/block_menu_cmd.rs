//! The block command vocabulary: `Cmd` plus its labels, search keywords, icons and the language / slash tables.
use crate::{editor::model::Kind, icons::Icon, theme::tr};
use super::kind_icon;

/// Code languages offered by the code block picker in `C-Code-Block.dc.html` order: (fence tag, label).
pub(in crate::editor) const LANGUAGES: [(&str, &str); 18] = [
    ("", "auto"),
    ("csv", "CSV"),
    ("latex", "LaTeX"),
    ("markdown", "Markdown"),
    ("mermaid", "Mermaid"),
    ("plaintext", "Plain Text"),
    ("bash", "Bash"),
    ("css", "CSS"),
    ("html", "HTML"),
    ("javascript", "JavaScript"),
    ("json", "JSON"),
    ("lean", "Lean"),
    ("python", "Python"),
    ("rust", "Rust"),
    ("sql", "SQL"),
    ("typescript", "TypeScript"),
    ("xml", "XML"),
    ("yaml", "YAML"),
];

pub(in crate::editor) fn canonical_lang_tag(tag: &str) -> &'static str {
    let token = tag.split_whitespace().next().unwrap_or("").to_ascii_lowercase();
    match token.as_str() {
        "" | "auto" => "",
        "csv" => "csv",
        "latex" | "math" | "tex" | "katex" => "latex",
        "markdown" | "md" => "markdown",
        "mermaid" => "mermaid",
        "plaintext" | "text" | "plain" | "txt" => "plaintext",
        "bash" | "sh" | "shell" | "zsh" => "bash",
        "css" => "css",
        "html" | "htm" => "html",
        "javascript" | "js" | "jsx" => "javascript",
        "json" | "jsonc" => "json",
        "lean" | "lean4" => "lean",
        "python" | "py" => "python",
        "rust" | "rs" => "rust",
        "sql" => "sql",
        "typescript" | "ts" | "tsx" => "typescript",
        "xml" | "svg" => "xml",
        "yaml" | "yml" => "yaml",
        _ => "",
    }
}

pub(in crate::editor) fn language_label(tag: &str) -> &'static str {
    let canon = canonical_lang_tag(tag);
    if canon.is_empty() && !tag.trim().is_empty() && !tag.trim().eq_ignore_ascii_case("auto") {
        return tr("其他", "Other");
    }
    LANGUAGES.iter().find(|(t, _)| *t == canon).map_or("auto", |(_, l)| *l)
}

pub(in crate::editor) fn language_icon(tag: &str) -> Option<Icon> {
    match canonical_lang_tag(tag) {
        "csv" => Some(Icon::Table),
        "latex" => Some(Icon::Math),
        "markdown" => Some(Icon::Note),
        "mermaid" => Some(Icon::Diagram),
        _ => None,
    }
}
#[derive(Clone, Copy, PartialEq)]
pub(in crate::editor) enum Cmd {
    Turn(Kind), Callout, Columns, Code(&'static str), Table, Divider, Image, Ai,
    Delete, SetLanguage(&'static str, bool),
}
impl Cmd {
    pub(in crate::editor) fn label(self) -> &'static str {
        match self {
            Cmd::Turn(Kind::Paragraph) => tr("正文", "Text"),
            Cmd::Turn(Kind::H1) => tr("一级标题", "Heading 1"),
            Cmd::Turn(Kind::H2) => tr("二级标题", "Heading 2"),
            Cmd::Turn(Kind::H3) => tr("三级标题", "Heading 3"),
            Cmd::Turn(Kind::Bullet) => tr("无序列表", "Bulleted list"),
            Cmd::Turn(Kind::Numbered) => tr("有序列表", "Numbered list"),
            Cmd::Turn(Kind::Quote) => tr("引用", "Quote"),
            Cmd::Turn(Kind::Todo) => tr("待办清单", "Checklist"),
            Cmd::Callout => tr("标注", "Callout"),
            Cmd::Columns => tr("分栏", "Columns"),
            Cmd::Code("mermaid") => tr("Mermaid 图表", "Mermaid Diagram"),
            Cmd::Code("math") => tr("数学公式", "Math Formula"),
            Cmd::Code(_) => tr("代码块", "Code Block"),
            Cmd::Table => tr("表格", "Table"),
            Cmd::Divider => tr("分隔线", "Divider"),
            Cmd::Image => tr("图片", "Image"),
            Cmd::Ai => tr("AI 写作", "AI Write"),
            Cmd::Delete => tr("删除", "Delete"),
            Cmd::SetLanguage(tag, _) => language_label(tag),
        }
    }
    pub(in crate::editor) fn keywords(self) -> &'static str {
        match self {
            Cmd::Turn(Kind::Paragraph) => "text paragraph zhengwen", Cmd::Turn(Kind::H1) => "h1 heading title biaoti",
            Cmd::Turn(Kind::H2) => "h2 heading", Cmd::Turn(Kind::H3) => "h3 heading",
            Cmd::Turn(Kind::Bullet) => "bullet list ul liebiao", Cmd::Turn(Kind::Numbered) => "number ordered list ol",
            Cmd::Turn(Kind::Quote) => "quote yinyong", Cmd::Turn(Kind::Todo) => "todo task check daiban",
            Cmd::Callout => "callout note tip warning alert biaozhu 标注 提示",
            Cmd::Columns => "columns column fenlan 分栏 两栏",
            Cmd::Code("mermaid") => "mermaid diagram flow", Cmd::Code("math") => "math latex formula gongshi",
            Cmd::Code(_) => "code daima", Cmd::Table => "table biaoge", Cmd::Divider => "divider hr line fengexian",
            Cmd::Image => "image picture photo tupian", Cmd::Ai => "ai write polish xiezuo runse 写作 润色", _ => "",
        }
    }
    /// Keyboard shortcut shown at the right of a row, for commands that have one.
    pub(in crate::editor) fn hint(self) -> Option<&'static str> {
        Some(match self {
            Cmd::Turn(Kind::Paragraph) => "⌘⌥0",
            Cmd::Turn(Kind::H1) => "⌘⌥1",
            Cmd::Turn(Kind::H2) => "⌘⌥2",
            Cmd::Turn(Kind::H3) => "⌘⌥3",
            Cmd::Turn(Kind::Todo) => "⌘⌥4",
            Cmd::Turn(Kind::Bullet) => "⌘⌥5",
            Cmd::Turn(Kind::Numbered) => "⌘⌥6",
            Cmd::Code("") => "⌘⌥8",
            Cmd::Turn(Kind::Quote) => "⌘⌥9",
            Cmd::Delete => "⌫",
            _ => return None,
        })
    }
    pub(in crate::editor) fn danger(self) -> bool { matches!(self, Cmd::Delete) }
    pub(in crate::editor) fn checked(self) -> bool {
        matches!(self, Cmd::SetLanguage(_, true))
    }
    pub(in crate::editor) fn leading(self) -> Option<Icon> {
        Some(match self {
            Cmd::Turn(kind) => kind_icon(kind),
            Cmd::Callout => Icon::Callout,
            Cmd::Columns => Icon::Columns,
            Cmd::Code("mermaid") | Cmd::SetLanguage("mermaid", _) => Icon::Diagram,
            Cmd::Code("math") | Cmd::SetLanguage("math", _) => Icon::Math,
            Cmd::Code(_) | Cmd::SetLanguage(..) => Icon::Code,
            Cmd::Table => Icon::Table, Cmd::Divider => Icon::Divider, Cmd::Image => Icon::Image, Cmd::Ai => Icon::Sparkles,
            Cmd::Delete => Icon::Trash,
        })
    }
}
pub(super) const SLASH: [Option<Cmd>; 19] = [
    Some(Cmd::Turn(Kind::H1)), Some(Cmd::Turn(Kind::H2)), Some(Cmd::Turn(Kind::H3)), Some(Cmd::Table),
    None,
    Some(Cmd::Turn(Kind::Paragraph)), Some(Cmd::Turn(Kind::Bullet)), Some(Cmd::Turn(Kind::Todo)),
    Some(Cmd::Code("")), Some(Cmd::Divider), Some(Cmd::Image),
    Some(Cmd::Turn(Kind::Numbered)), Some(Cmd::Turn(Kind::Quote)), Some(Cmd::Callout), Some(Cmd::Columns),
    None,
    Some(Cmd::Code("mermaid")), Some(Cmd::Code("math")), Some(Cmd::Ai),
];
