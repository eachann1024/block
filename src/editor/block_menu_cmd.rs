//! The block command vocabulary: `Cmd` plus its labels, search keywords, icons and the language / slash tables.
use crate::{editor::{image_source::Align, model::Kind, table_actions::TableCmd}, icons::Icon};
use super::{Sub, kind_icon};

/// Code languages offered by the handle menu: (fence tag, label).
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
    Turn(Kind), Code(&'static str), Table, Divider, Image, Ai,
    InsertAbove, InsertBelow, Duplicate, Cut, CopyText, MoveUp, MoveDown, Delete,
    /// Group title inside the handle menu; not clickable.
    Header(&'static str),
    /// Opens a cascading side submenu (`转换为 →` or `代码语言 →`).
    Open(Sub),
    /// Conversion target; `true` marks the block's current type.
    TurnTo(Kind, bool), CodeTo(bool),
    SetLanguage(&'static str, bool), CopyCode,
    TableOp(TableCmd),
    CopyImage, RevealImage, AlignImage(Align, bool),
    /// Code block soft wrap; `true` when wrapping is on.
    ToggleWrap(bool),
}
impl Cmd {
    pub(in crate::editor) fn label(self) -> &'static str {
        match self {
            Cmd::Turn(Kind::Paragraph) => "正文", Cmd::Turn(Kind::H1) => "一级标题", Cmd::Turn(Kind::H2) => "二级标题",
            Cmd::Turn(Kind::H3) => "三级标题", Cmd::Turn(Kind::Bullet) => "无序列表", Cmd::Turn(Kind::Numbered) => "有序列表",
            Cmd::Turn(Kind::Quote) => "引用", Cmd::Turn(Kind::Todo) => "待办清单",
            Cmd::Code("mermaid") => "Mermaid 图表", Cmd::Code("math") => "数学公式", Cmd::Code(_) => "代码块",
            Cmd::Table => "表格", Cmd::Divider => "分隔线", Cmd::Image => "图片", Cmd::Ai => "AI 写作",
            Cmd::InsertAbove => "在上方插入空行", Cmd::InsertBelow => "在下方插入空行", Cmd::Duplicate => "复制这个块", Cmd::Cut => "剪切",
            Cmd::CopyText => "复制文本", Cmd::MoveUp => "上移", Cmd::MoveDown => "下移", Cmd::Delete => "删除",
            Cmd::Header(title) => title,
            Cmd::Open(Sub::Convert) => "转换为", Cmd::Open(Sub::Language) => "代码语言", Cmd::Open(Sub::Root) => "",
            Cmd::TurnTo(kind, _) => Cmd::Turn(kind).label(), Cmd::CodeTo(_) => "代码块",
            Cmd::SetLanguage(tag, _) => language_label(tag), Cmd::CopyCode => "复制代码",
            Cmd::TableOp(op) => op.label(),
            Cmd::ToggleWrap(_) => "自动换行",
            Cmd::CopyImage => "复制图片", Cmd::RevealImage => "在访达中显示",
            Cmd::AlignImage(Align::Left, _) => "左对齐", Cmd::AlignImage(Align::Center, _) => "居中", Cmd::AlignImage(Align::Right, _) => "右对齐",
        }
    }
    pub(in crate::editor) fn keywords(self) -> &'static str {
        match self {
            Cmd::Turn(Kind::Paragraph) => "text paragraph zhengwen", Cmd::Turn(Kind::H1) => "h1 heading title biaoti",
            Cmd::Turn(Kind::H2) => "h2 heading", Cmd::Turn(Kind::H3) => "h3 heading",
            Cmd::Turn(Kind::Bullet) => "bullet list ul liebiao", Cmd::Turn(Kind::Numbered) => "number ordered list ol",
            Cmd::Turn(Kind::Quote) => "quote yinyong", Cmd::Turn(Kind::Todo) => "todo task check daiban",
            Cmd::Code("mermaid") => "mermaid diagram flow", Cmd::Code("math") => "math latex formula gongshi",
            Cmd::Code(_) => "code daima", Cmd::Table => "table biaoge", Cmd::Divider => "divider hr line fengexian",
            Cmd::Image => "image picture photo tupian", Cmd::Ai => "ai write polish xiezuo runse 写作 润色", _ => "",
        }
    }
    /// Keyboard shortcut shown at the right of a row, for commands that have one.
    pub(in crate::editor) fn hint(self) -> Option<&'static str> {
        Some(match self {
            Cmd::Turn(Kind::Paragraph) | Cmd::TurnTo(Kind::Paragraph, _) => "⌘⌥0",
            Cmd::Turn(Kind::H1) | Cmd::TurnTo(Kind::H1, _) => "⌘⌥1",
            Cmd::Turn(Kind::H2) | Cmd::TurnTo(Kind::H2, _) => "⌘⌥2",
            Cmd::Turn(Kind::H3) | Cmd::TurnTo(Kind::H3, _) => "⌘⌥3",
            Cmd::Turn(Kind::Todo) | Cmd::TurnTo(Kind::Todo, _) => "⌘⌥4",
            Cmd::Turn(Kind::Bullet) | Cmd::TurnTo(Kind::Bullet, _) => "⌘⌥5",
            Cmd::Turn(Kind::Numbered) | Cmd::TurnTo(Kind::Numbered, _) => "⌘⌥6",
            Cmd::Code("") | Cmd::CodeTo(_) => "⌘⌥8",
            Cmd::Turn(Kind::Quote) | Cmd::TurnTo(Kind::Quote, _) => "⌘⌥9",
            Cmd::Duplicate => "⌘D",
            Cmd::Cut => "⌘X",
            Cmd::Delete => "⌫",
            Cmd::MoveUp => "⌥↑",
            Cmd::MoveDown => "⌥↓",
            _ => return None,
        })
    }
    pub(in crate::editor) fn danger(self) -> bool { matches!(self, Cmd::Delete | Cmd::TableOp(TableCmd::DeleteTable)) }
    pub(in crate::editor) fn checked(self) -> bool { matches!(self, Cmd::TurnTo(_, true) | Cmd::CodeTo(true) | Cmd::SetLanguage(_, true) | Cmd::AlignImage(_, true) | Cmd::ToggleWrap(true)) }
    pub(in crate::editor) fn leading(self) -> Option<Icon> {
        Some(match self {
            Cmd::Turn(kind) | Cmd::TurnTo(kind, _) => kind_icon(kind),
            Cmd::Code("mermaid") | Cmd::SetLanguage("mermaid", _) => Icon::Diagram,
            Cmd::Code("math") | Cmd::SetLanguage("math", _) => Icon::Math,
            Cmd::Code(_) | Cmd::CodeTo(_) | Cmd::SetLanguage(..) => Icon::Code,
            Cmd::Table => Icon::Table, Cmd::Divider => Icon::Divider, Cmd::Image => Icon::Image, Cmd::Ai => Icon::Sparkles,
            Cmd::CopyText | Cmd::CopyCode | Cmd::CopyImage | Cmd::TableOp(TableCmd::CopyTable) => Icon::Copy,
            Cmd::Duplicate => Icon::Duplicate, Cmd::Cut => Icon::Cut,
            Cmd::InsertAbove => Icon::InsertAbove, Cmd::InsertBelow => Icon::InsertBelow,
            Cmd::TableOp(TableCmd::AppendRow | TableCmd::AppendColumn) => Icon::Plus,
            Cmd::TableOp(TableCmd::SelectTable) => Icon::SelectAll,
            Cmd::TableOp(TableCmd::DeleteTable) | Cmd::Delete => Icon::Trash,
            Cmd::MoveUp => Icon::ArrowUp, Cmd::MoveDown => Icon::ArrowDown,
            Cmd::RevealImage => Icon::FolderOpen,
            Cmd::AlignImage(Align::Left, _) => Icon::AlignLeft, Cmd::AlignImage(Align::Center, _) => Icon::AlignCenter,
            Cmd::AlignImage(Align::Right, _) => Icon::AlignRight,
            Cmd::Open(Sub::Language) => Icon::Code, Cmd::Open(Sub::Convert) => Icon::Heading,
            Cmd::ToggleWrap(_) => Icon::Wrap,
            _ => return None,
        })
    }
}
pub(super) const SLASH: [Option<Cmd>; 17] = [
    Some(Cmd::Turn(Kind::H1)), Some(Cmd::Turn(Kind::H2)), Some(Cmd::Turn(Kind::H3)), Some(Cmd::Table),
    None,
    Some(Cmd::Turn(Kind::Paragraph)), Some(Cmd::Turn(Kind::Bullet)), Some(Cmd::Turn(Kind::Todo)),
    Some(Cmd::Code("")), Some(Cmd::Divider), Some(Cmd::Image),
    Some(Cmd::Turn(Kind::Numbered)), Some(Cmd::Turn(Kind::Quote)),
    None,
    Some(Cmd::Code("mermaid")), Some(Cmd::Code("math")), Some(Cmd::Ai),
];
