const BLURBS = [
  ["quick-note panel independent", "improve", "每次打开速记，它都单独待着，不再跟着主窗口绑在一起。", "Quick Note stays on its own each time you open it, instead of tying itself to the main window."],
  ["performance monitor V2 artboard", "other", "性能监视器的设计稿可以点着试用。", "The performance monitor design board can be clicked through."],
  ["Option-B sidebar toggle", "feat", "可以显示或收起侧栏；指向顶栏上的按钮时，会说明它是做什么的。", "You can show or hide the sidebar, and the title bar explains the control when you point at it."],
  ["Simplify feedback categories", "improve", "反馈的类型更少、说法更清楚，写完后顶部标题会收起。", "Feedback types are fewer and the wording is clearer, and the header hides when you finish."],
  ["Recreate Beautiful UI", "improve", "常用控件重新画过，AI 对话看起来更简单。", "Shared controls were redrawn, and AI chat is simpler to look at."],
  ["decouple builtin AI", "feat", "内置助手不再绑在总开关上。每个智能体可以单独设置启动参数和优先级。", "The built-in assistant is no longer tied to one master switch. Each agent can have its own launch options and priority."],
  ["move entry to Block menu", "feat", "性能监视器改到 Block 菜单里打开，并有了快捷键。", "The performance monitor opens from the Block menu, and it has a shortcut."],
  ["Streamline build pipeline", "other", "本地构建和发版构建走了更短的同一条流水线。", "Local builds and release builds now share a shorter pipeline."],
  ["align composer input edges", "fix", "对话框和下面的按钮对齐了，发送图标也更清楚。", "The chat box lines up with the buttons under it, and the send icon is sharper."],
  ["refine monitor UI", "feat", "性能监视器可以拖动，会显示正在加载，统计笔记本占用时会跳过你设为忽略的文件夹。", "The performance monitor can be dragged, shows a loading state, and skips folders you set to ignore when measuring the notebook."],
  ["keep this version's notes", "other", "更新说明先写英文，下面再写中文。", "Release notes are written in English first, with Chinese below."],
  ["remove review button", "feat", "笔记的修改建议会直接在编辑器里打开给你看，不再单独放一个审阅按钮。", "A suggested change to a note opens in the editor for review, without a separate review button."],
  ["upload only the installer allowlist", "other", "发布页只放给人下载的安装包。", "The release page only keeps the installers people download."],
  ["Simplify agent selection and save", "improve", "挑选智能体时不会立刻启用，确认后才会保存。", "You can look through agents without turning one on, and the choice is saved when you confirm."],
  ["require exactly the allowlisted", "other", "发布之后会再核对一遍，多出来的附件会被去掉。", "After publishing, the release is checked again and extra files are removed."],
  ["fit new tables", "fix", "新建的表格会适应页面宽度；整张表被选中时，可以把它删掉。", "A new table fits the page, and selecting the whole table deletes it."],
  ["prefer above control bounds", "fix", "提示优先出现在按钮上方，靠近屏幕边缘时会挪开，避免被挡住。", "Tips prefer to appear above a control, and they move so they stay on screen."],
  ["avoid restarting the app", "other", "本地构建不再在你没要求时重新打开 Block。", "A local build no longer restarts Block when you did not ask it to."],
  ["move-to-trash permission", "fix", "在 macOS 上把笔记移到废纸篓不再因为权限失败；也可以把文件夹拖到侧栏来打开。", "Moving a note to the Trash on macOS no longer fails with a permission error, and you can drop a folder on the sidebar to open it."],
  ["Separate agent selection from activation", "feat", "选择智能体和启用它分成了两步，智能体卡片也更大。", "Choosing an agent is separate from turning it on, and the agent cards are larger."],
  ["eliminate empty diff rows", "improve", "编辑器里查看 AI 修改的卡片重新排过，不再出现空行。", "The card that reviews an AI change in the note was redesigned, and it no longer shows empty rows."],
  ["text overflow and skip code fence", "fix", "审阅卡片里的文字不再溢出；代码块两端的标记行不会被当成修改。", "Text in the review card stays inside the card, and the opening and closing lines of a code block are not shown as changes."],
  ["nested list code rendering", "fix", "嵌套列表里的代码能正常显示；AI 给出的笔记修改会先检查，再写入。", "Code inside a nested list renders correctly, and an AI suggestion is checked before it changes the note."],
  ["full-permissions toggle", "improve", "AI 设置收成完整权限开关，以及后端实际提供的模型列表。", "AI settings are a switch for full permissions and the model list the backend actually returns."],
  ["ship five installers", "other", "安装包固定为五个；手动强制构建时，也会带上自上一个版本以来的全部更新说明。", "The release ships the five installers, and a forced build still gets notes for every commit since the previous release."],
  ["publish block-bin", "other", "可以用 Linux 安装包给 Arch 用户发布。", "The Linux package can be published for Arch users."],
];

export function kindOf(subject) {
  const typed = String(subject).match(/^([a-z]+)(?:\([^)]*\))?!?:/i)?.[1]?.toLowerCase();
  if (typed === "feat") return "feat";
  if (typed === "fix") return "fix";
  if (["perf", "refactor", "style", "improve"].includes(typed || "")) return "improve";
  if (["ci", "build", "chore", "docs", "test", "design", "release"].includes(typed || "")) return "other";
  if (/^(fix|resolve)\b/i.test(subject)) return "fix";
  if (/^(add|introduce)\b/i.test(subject)) return "feat";
  return "improve";
}

function finish(text, punct) {
  const clean = String(text).trim().replace(/[.。!！?？]+$/, "");
  return `${clean}${punct}`;
}

export function describeCommit(commit) {
  const subject = String(commit.subject || "").replace(/\s*\(#\d+\)$/, "").trim();
  const known = BLURBS.find(([pattern]) => subject.toLowerCase().includes(pattern.toLowerCase()));
  const kind = known?.[1] || kindOf(subject);
  const zhTrailer = String(commit.releaseNote || "").trim();
  const enTrailer = String(commit.releaseNoteEn || "").trim();
  if (zhTrailer && enTrailer && /[\u3400-\u9fff]/.test(zhTrailer) && !/[\u3400-\u9fff]/.test(enTrailer)) {
    return { kind, zh: finish(zhTrailer, "。"), en: finish(enTrailer, ".") };
  }
  if (known) return { kind, zh: known[2], en: known[3] };
  const bare = subject.replace(/^[a-z]+(?:\([^)]*\))?!?:\s*/i, "").trim() || subject;
  const en = finish(bare.charAt(0).toUpperCase() + bare.slice(1), ".");
  const zh = kind === "fix"
    ? "修了一处会在使用里碰到的问题。"
    : kind === "feat"
      ? "加了一项可以直接用到的能力。"
      : kind === "other"
        ? "还有一项构建、发布或工程上的改动。"
        : "调整了一处日常使用时会碰到的行为。";
  return { kind, zh, en };
}
