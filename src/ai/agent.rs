//! thinkly's notebook agent (`src/lib/notebook-ai/tools/*`, `src/agent/*`): the
//! AGENTS.md routing prompt, `loadSkill` with the built-in Skills, the note tools
//! (listPages / searchNotes / readPage) and `executeBatchPlan`, whose edits become a
//! 笔记变更计划 card that the user applies (and can 撤回) from the chat panel.
use std::path::{Path, PathBuf};
use serde_json::{Value, json};
#[path = "markdown_preflight.rs"] pub(super) mod markdown_preflight;

pub(super) const AGENTS: &str = include_str!("skills/AGENTS.md");
pub(super) const MARKDOWN_SKILL: &str = include_str!("skills/markdown.md");
/// (id, description, SKILL.md, tools the skill unlocks) — thinkly `skills.ts`.
pub(super) const SKILLS: &[(&str, &str, &str, &[&str])] = &[
    ("createNoote", "在本轮确定的笔记本和目录新建页面", include_str!("skills/createNoote.md"), &["listPages", "readPage", "executeBatchPlan"]),
    ("updateNote", "改写、追加、补充或重命名当前页", include_str!("skills/updateNote.md"), &["readPage", "executeBatchPlan"]),
    ("searchNotes", "跨笔记本和目录搜索并读取页面", include_str!("skills/searchNotes.md"), &["listPages", "searchNotes", "readPage"]),
    ("chat", "仅解释或建议，不改笔记", include_str!("skills/chat.md"), &["readPage"]),
    ("markdown", "创建或修改笔记时的 Markdown 指引", MARKDOWN_SKILL, &["executeBatchPlan"]),
];

const BATCH_DESC: &str = "准备并处理笔记变更计划。修改已有页面会生成审批卡片，用户确认后才写入；纯新建自动写入。局部修改必须用 search_replace（oldString 须为 readPage 返回的精确片段，newString 可为空表示删除该片段，replaceAll 可选），整页重写才用 edit，新建用 create。参数格式：{title:\"整理笔记\",summary:\"更新账号页\",operations:[{type:\"search_replace\",pageId:\"page-1\",oldString:\"旧片段\",newString:\"新片段\"}]}。计划生成后停止，不要重复提交；结构预检失败可修正并重试一次。同页操作按顺序模拟。";

fn tool(name: &str, desc: &str, params: Value) -> Value {
    json!({ "type": "function", "function": { "name": name, "description": desc, "parameters": params } })
}

pub(super) fn tools() -> Value {
    let catalog = SKILLS.iter().map(|(id, d, ..)| format!("{id}：{d}")).collect::<Vec<_>>().join("；");
    let ids: Vec<&str> = SKILLS.iter().map(|s| s.0).collect();
    json!([
        tool("loadSkill", &format!("有明确任务时加载最匹配的 Skill。{{{catalog}}}。"),
            json!({ "type": "object", "properties": { "skill": { "type": "string", "enum": ids } }, "required": ["skill"] })),
        tool("listNotebooks", "列出已登记的笔记本根目录。", json!({ "type": "object", "properties": {} })),
        tool("readSkillResource", "读取已授权 Skill 的只读文本资源，使用上下文提供的稳定 skillId 和相对路径。", json!({ "type": "object", "properties": { "skillId": { "type": "string" }, "path": { "type": "string" } }, "required": ["skillId", "path"] })),
        tool("listPages", "列出笔记本的页面；可指定 listNotebooks 返回的 notebookRoot。", json!({ "type": "object", "properties": { "notebookRoot": { "type": "string" } } })),
        tool("searchNotes", "在笔记本中按关键词搜索页面，返回前 20 条命中及片段。",
            json!({ "type": "object", "properties": { "query": { "type": "string" }, "notebookRoot": { "type": "string" } }, "required": ["query"] })),
        tool("readPage", "读取页面的标题和 Markdown 正文；省略 pageId 时读取当前打开页面。",
            json!({ "type": "object", "properties": { "pageId": { "type": "string" }, "notebookRoot": { "type": "string" } } })),
        tool("executeBatchPlan", BATCH_DESC, json!({ "type": "object", "properties": {
            "title": { "type": "string" }, "summary": { "type": "string" }, "notebookRoot": { "type": "string" },
            "operations": { "type": "array", "items": { "type": "object", "properties": {
                "type": { "type": "string", "enum": ["search_replace", "edit", "create"] },
                "pageId": { "type": "string" }, "oldString": { "type": "string" }, "newString": { "type": "string" },
                "replaceAll": { "type": "boolean" }, "markdown": { "type": "string" }, "title": { "type": "string" } },
                "required": ["type"] } } }, "required": ["operations"] })),
    ])
}

#[derive(Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub(super) enum OpKind { Replace { old: String, new: String, all: bool }, Edit { markdown: String }, Create { markdown: String } }
#[derive(Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub(super) struct Op { pub page: String, pub kind: OpKind }
#[derive(Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub(super) enum PlanState { Pending, Applied, Partial, Canceled, Undone, UndoConflict, Stale }
/// One write the plan made: page, content before and after, and whether it created the file.
#[derive(Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub(super) struct Receipt { pub page: String, pub before: String, pub after: String, pub created: bool }
#[derive(Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub(super) struct Plan {
    pub title: String, pub summary: String, pub ops: Vec<Op>, pub state: PlanState,
    #[serde(default)] pub receipts: Vec<Receipt>,
    #[serde(default)] pub expected: Option<Vec<(String, Option<String>)>>,
    #[serde(default)] pub root: Option<PathBuf>,
}

/// What the tools can see: the notebook folder and the open page (its live text).
pub(super) struct Ctx { pub root: PathBuf, pub current: Option<String>, pub current_md: String }

impl Ctx {
    pub fn path(&self, page: &str) -> PathBuf { self.root.join(format!("{page}.md")) }
    pub fn checked_path(&self, page: &str) -> Result<PathBuf, String> {
        use std::path::Component;
        let relative = Path::new(page);
        if page.is_empty() || relative.components().any(|part| !matches!(part, Component::Normal(_))) {
            return Err("Invalid page path".into());
        }
        let root = self.root.canonicalize().map_err(|error| error.to_string())?;
        let path = root.join(format!("{page}.md"));
        let mut part_path = root.clone();
        let relative = path.strip_prefix(&root).map_err(|error| error.to_string())?;
        for part in relative.components() {
            part_path.push(part.as_os_str());
            match std::fs::symlink_metadata(&part_path) {
                Ok(metadata) if metadata.file_type().is_symlink() => return Err("Symbolic links are not supported in note paths".into()),
                Ok(_) => {},
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {},
                Err(error) => return Err(error.to_string()),
            }
        }
        Ok(path)
    }
    pub fn pages(&self) -> Vec<String> {
        fn walk(dir: &Path, base: &Path, out: &mut Vec<String>) {
            let Ok(rd) = std::fs::read_dir(dir) else { return };
            for e in rd.flatten() {
                let p = e.path();
                if p.file_name().and_then(|n| n.to_str()).is_some_and(|n| n.starts_with('.')) { continue; }
                if e.file_type().is_ok_and(|kind| kind.is_symlink()) { continue; }
                if p.is_dir() { walk(&p, base, out); }
                else if p.extension().is_some_and(|x| x == "md") {
                    if let Ok(rel) = p.with_extension("").strip_prefix(base) { out.push(rel.to_string_lossy().into_owned()); }
                }
            }
        }
        let mut out = Vec::new();
        walk(&self.root, &self.root, &mut out);
        out.sort();
        out
    }
    pub fn read(&self, page: &str) -> Option<String> {
        self.read_checked(page).ok().flatten()
    }
    pub fn read_checked(&self, page: &str) -> Result<Option<String>, String> {
        let path = self.checked_path(page)?;
        if self.current.as_deref() == Some(page) { return Ok(Some(self.current_md.clone())); }
        match std::fs::read_to_string(path) {
            Ok(text) => Ok(Some(text)),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error.to_string()),
        }
    }
    /// Workspace Skills (`SKILL.md` files in the notebook), thinkly `localContext.ts`.
    pub fn local_skills(&self) -> Vec<(String, String, String)> {
        self.skills_from_pages(self.pages())
    }
    pub fn skills_from_pages(&self, pages: impl IntoIterator<Item = String>) -> Vec<(String, String, String)> {
        pages.into_iter().filter(|p| p.ends_with("SKILL")).filter_map(|p| {
            let body = self.read(&p)?;
            let field = |k: &str| body.lines().find_map(|l| l.strip_prefix(k).map(|v| v.trim().trim_matches('"').to_owned()));
            let name = field("name:").unwrap_or_else(|| Path::new(&p).parent().and_then(|d| d.file_name()).map(|s| s.to_string_lossy().into_owned()).unwrap_or(p.clone()));
            Some((name, field("description:").unwrap_or_default(), body))
        }).collect()
    }
}

fn arg<'a>(v: &'a Value, k: &str) -> &'a str { v.get(k).and_then(|s| s.as_str()).unwrap_or("") }

/// Runs one tool call; an `executeBatchPlan` also yields the plan for the card.
pub(super) fn exec(name: &str, args: &str, ctx: &Ctx) -> (Value, Option<Plan>) {
    let a: Value = serde_json::from_str(args).unwrap_or(json!({}));
    let r = match name {
        "listNotebooks" => json!({ "notebooks": super::chat::chat_roots::registered_roots() }),
        "readSkillResource" => {
            let grants = super::chat::chat_skills::catalog(Some(&ctx.root)).into_iter().map(|(_, grant)| grant).collect::<Vec<_>>();
            match super::backend::skill_resources::read(&grants, arg(&a, "skillId"), arg(&a, "path")) {
                Ok(text) => json!({ "text": text }), Err(error) => json!({ "error": error.to_string() }),
            }
        }
        "loadSkill" => match SKILLS.iter().find(|s| s.0 == arg(&a, "skill")) {
            Some((id, _, body, tools)) => json!({ "skill": id, "supported": true, "instructions": body, "availableTools": tools }),
            None => json!({ "skill": arg(&a, "skill"), "supported": false, "error": "未知 Skill" }),
        },
        "listPages" => Value::Array(ctx.pages().into_iter().map(|p| {
            let t = std::fs::metadata(ctx.path(&p)).and_then(|m| m.modified()).ok()
                .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok()).map(|d| d.as_millis() as u64).unwrap_or(0);
            json!({ "pageId": p, "title": title(&p), "updatedAt": t })
        }).collect()),
        "searchNotes" => {
            let q = arg(&a, "query").to_lowercase();
            let hits: Vec<Value> = ctx.pages().into_iter().filter_map(|p| {
                let body = ctx.read(&p)?;
                let line = body.lines().find(|l| l.to_lowercase().contains(&q));
                (title(&p).to_lowercase().contains(&q) || line.is_some()).then(|| json!({ "pageId": p, "title": title(&p),
                    "snippet": line.unwrap_or("").chars().take(160).collect::<String>() }))
            }).take(20).collect();
            json!({ "query": q, "results": hits })
        }
        "readPage" => {
            let page = Some(arg(&a, "pageId")).filter(|s| !s.is_empty()).map(str::to_owned).or(ctx.current.clone()).unwrap_or_default();
            match ctx.read(&page) {
                Some(md) => json!({ "pageId": page, "title": title(&page), "markdown": md }),
                None => json!({ "error": format!("找不到页面：{page}") }),
            }
        }
        "executeBatchPlan" => return plan(&a, ctx),
        _ => json!({ "error": format!("未知工具：{name}") }),
    };
    (r, None)
}

fn title(page: &str) -> String { Path::new(page).file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default() }

fn plan(a: &Value, ctx: &Ctx) -> (Value, Option<Plan>) {
    let mut ops = Vec::new();
    for o in a.get("operations").and_then(|o| o.as_array()).into_iter().flatten() {
        let page = Some(arg(o, "pageId")).filter(|s| !s.is_empty()).map(str::to_owned).or(ctx.current.clone()).unwrap_or_default();
        let kind = match arg(o, "type") {
            "search_replace" => {
                let old = arg(o, "oldString").to_owned();
                OpKind::Replace { old, new: arg(o, "newString").to_owned(), all: o.get("replaceAll").and_then(|b| b.as_bool()).unwrap_or(false) }
            }
            "edit" => OpKind::Edit { markdown: arg(o, "markdown").to_owned() },
            "create" => {
                let t = Some(arg(o, "title")).filter(|s| !s.is_empty()).unwrap_or(crate::theme::tr("未命名", "Untitled"));
                ops.push(Op { page: t.to_owned(), kind: OpKind::Create { markdown: arg(o, "markdown").to_owned() } });
                continue;
            }
            other => return (json!({ "error": format!("暂不支持的操作：{other}") }), None),
        };
        ops.push(Op { page, kind });
    }
    if ops.is_empty() { return (json!({ "error": "operations 为空" }), None); }
    let mut expected = Vec::new();
    for op in &ops {
        if let Err(error) = ctx.checked_path(&op.page) { return (json!({ "error": error }), None); }
        if !expected.iter().any(|(page, _)| page == &op.page) {
            match ctx.read_checked(&op.page) {
                Ok(content) => expected.push((op.page.clone(), content)),
                Err(error) => return (json!({ "error": error }), None),
            }
        }
    }
    if let Err(error) = markdown_preflight::simulate(&ops, &expected) { return (error, None); }
    let p = Plan { title: arg(a, "title").to_owned(), summary: arg(a, "summary").to_owned(), ops,
        state: PlanState::Pending, receipts: Vec::new(), expected: Some(expected), root: Some(ctx.root.clone()) };
    (json!({ "status": "awaiting_approval", "message": "方案已生成，尚未修改笔记。等待用户在卡片中确认，不要重复提交。" }), Some(p))
}

/// The page text after `op`, or None when its 原文 is gone.
pub(super) fn apply_op(op: &Op, current: Option<&str>) -> Option<String> {
    match &op.kind {
        OpKind::Replace { old, new, all } => {
            let md = current?;
            if old.is_empty() || !md.contains(old.as_str()) { return None; }
            Some(if *all { md.replace(old.as_str(), new) } else { md.replacen(old.as_str(), new, 1) })
        }
        OpKind::Edit { markdown } => current.map(|_| markdown.clone()),
        OpKind::Create { markdown } => current.is_none().then(|| markdown.clone()),
    }
}
