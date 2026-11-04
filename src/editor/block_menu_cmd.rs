//! The block command vocabulary: `Cmd` plus its labels, search keywords, icons and the language / slash tables.
use crate::{editor::model::Kind, icons::Icon};
use super::kind_icon;

/// Code languages offered by the code block picker: (fence tag, label).
pub(in crate::editor) const LANGUAGES: [(&str, &str); 18] = [
    ("", "纯文本"), ("rust", "Rust"), ("javascript", "JavaScript"), ("typescript", "TypeScript"), ("python", "Python"),
    ("bash", "Shell"), ("json", "JSON"), ("html", "HTML"), ("css", "CSS"), ("markdown", "Markdown"), ("sql", "SQL"),
    ("go", "Go"), ("swift", "Swift"), ("java", "Java"), ("cpp", "C++"), ("yaml", "YAML"), ("mermaid", "Mermaid 图表"),
    ("math", "数学公式"),
];
pub(in crate::editor) fn language_label(tag: &str) -> &'static str {
    let tag = tag.split_whitespace().next().unwrap_or("").to_ascii_lowercase();
    let tag = match tag.as_str() { "js" => "javascript", "ts" => "typescript", "py" => "python", "sh" | "shell" | "zsh" => "bash",
        "md" => "markdown", "c++" => "cpp", "yml" => "yaml", "latex" | "tex" => "math", other => other }.to_owned();
    LANGUAGES.iter().find(|(t, _)| *t == tag).map_or("其他", |(_, l)| *l)
}
#[derive(Clone, Copy, PartialEq)]
pub(in crate::editor) enum Cmd {
    Turn(Kind), Callout, Columns, Code(&'static str), Table, Divider, Image, Ai,
    Delete, SetLanguage(&'static str, bool),
}
impl Cmd {
    pub(in crate::editor) fn label(self) -> &'static str {
        match self {
            Cmd::Turn(Kind::Paragraph) => "正文", Cmd::Turn(Kind::H1) => "一级标题", Cmd::Turn(Kind::H2) => "二级标题",
            Cmd::Turn(Kind::H3) => "三级标题", Cmd::Turn(Kind::Bullet) => "无序列表", Cmd::Turn(Kind::Numbered) => "有序列表",
            Cmd::Turn(Kind::Quote) => "引用", Cmd::Turn(Kind::Todo) => "待办清单",
            Cmd::Callout => "标注", Cmd::Columns => "分栏",
            Cmd::Code("mermaid") => "Mermaid 图表", Cmd::Code("math") => "数学公式", Cmd::Code(_) => "代码块",
            Cmd::Table => "表格", Cmd::Divider => "分隔线", Cmd::Image => "图片", Cmd::Ai => "AI 写作",
            Cmd::Delete => "删除",
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
