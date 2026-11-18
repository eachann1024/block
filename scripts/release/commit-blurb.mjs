import { bilingualFromCommit } from "./release-note-rules.mjs";

const BLURBS = [
  ["bilingual cross-version changelog", "feat", "支持跨版本更新摘要汇总、中英双语切换，以及国内加速镜像与官方源自动优选。", "Added cross-version changelog summaries, Chinese and English switching, and automatic failover between mirrors and GitHub."],
  ["Scheme A updater modal", "feat", "全新的应用内更新卡片支持分类合并与按版本查看，并记录完整的诊断日志。", "Redesigned the in-app update dialog with grouped summaries, per-version views, and full diagnostic logging."],
  ["Hide empty block hints during IME", "fix", "中文输入法拼写过程中会自动隐藏空行快捷键提示，避免遮挡输入文字。", "Empty-line shortcut hints now hide automatically while typing with an input method."],
  ["serialize empty paragraphs as blank lines", "fix", "空段落保存为干净的空行，不再写入旧版注释标记。", "Empty paragraphs now save as clean blank lines without legacy comment markers."],
  ["support all installed fonts and adapt caret", "feat", "支持选择电脑上已安装的任意字体，文字光标高度会随所选字体自动贴合。", "You can pick any font installed on your computer, and the text caret adapts to the font metrics."],
  ["harden narrow block-foot annotations", "fix", "窄窗口下的块底批注在长文档滚动和标题折叠时保持稳定显示。", "Inline block-foot annotations in narrow windows stay stable while scrolling or folding headings."],
  ["empty-line plus button only on hovered row", "improve", "空行左侧的插入加号只在鼠标指向该行时显示，编辑区更安静。", "The plus button on an empty line only appears when you point at that line."],
  ["hide caret after reactions and record annotation undo", "fix", "点选表情回应后会自动收起文字光标，批注修改也支持撤销。", "Picking a reaction hides the text caret, and annotation edits can be undone."],
  ["anchor narrow-window reactions and annotations inline", "feat", "窄窗口下表情回应与批注卡片会直接贴在对应段落下方展示。", "In narrow windows, reactions and annotations sit directly beneath their block."],
  ["anchor caret geometry to font baseline", "fix", "文字光标与字体基线精准对齐，并在行尾留出舒适间距。", "The text caret aligns with the font baseline and leaves comfortable space at the end of a line."],
  ["blinking caret, empty-line hint band", "feat", "编辑器加入呼吸闪烁光标、空行快捷提示、多级列表引导线与纸感待办框。", "Added a blinking caret, empty-line shortcut hints, nested list guides, and paper-style to-do boxes."],
  ["keep selection toolbar single-row", "improve", "划词工具栏保持单行紧凑排列，超出项自动收入更多菜单。", "The selection toolbar stays on a single row and tucks extra actions into a menu."],
  ["align annotation highlight bands with inline code", "fix", "批注高亮底色与行内代码圆角框的高度整齐一致。", "Annotation highlights now line up cleanly with inline code chips."],
  ["unify inline AI presets into starred prompt chips", "feat", "行内助手常用指令升级为可收藏的快捷标签，最多紧凑展示三行。", "Inline assistant presets are unified into starred prompt chips arranged in up to three rows."],
  ["raise row icon token", "improve", "列表与菜单行首图标尺寸统一放大，视觉对齐更清晰。", "Leading icons in lists and menus are slightly larger and cleaner to scan."],
  ["remove callout top-right chips", "improve", "提示块去掉右上角多余标签，图标与标题垂直居中对齐。", "Callout blocks drop extra corner tags and center their icon with the heading."],
  ["replace triangle chevrons with thin V strokes", "improve", "展开箭头与关闭按钮换用更纤细利落的线条图标。", "Chevrons and close buttons use cleaner, lighter stroke icons."],
  ["align inline AI preview blocks", "fix", "行内助手预览内容的段落与列表缩进同正文完全一致。", "Inline assistant previews match the spacing and list markers of the note."],
  ["harden Markdown round-trip", "fix", "改进复杂块结构的读写还原，反复保存不会丢失原本排版。", "Complex blocks keep their formatting reliably across repeated saves."],
  ["keep inline AI session and preview alive", "fix", "滚动长笔记或切换窗口时，正在进行的行内助手预览不会中断。", "Scrolling the note or switching windows no longer interrupts an active inline assistant session."],
  ["suppress block handles when overlays are open", "fix", "打开弹窗或菜单时自动收起段落把手，输入框文字垂直居中。", "Block handles stay hidden while a dialog or menu is open, and input text sits centered."],
  ["normalize orphan indentation and table slices", "fix", "复制多层缩进列表或局部表格时，粘贴格式更干净规整。", "Copying indented list items or partial table selections produces cleaner text."],
  ["normalize all Markdown into native blocks", "feat", "打开任意笔记都会直接转为原生段落与块结构进行编辑。", "Every note opens directly as native editable blocks without fallback views."],
  ["streamline inline AI with saved prompts", "feat", "行内助手支持保存常用提问、实时流式生成，取消后恢复原先输入。", "Inline assistant supports saved prompts, live streaming output, and input recovery on cancel."],
  ["unify icon sizes, selected states", "improve", "全应用的图标字号、选中态底色与字阶层级做了统一梳理。", "Icon sizes, selection highlights, and type sizes are unified across the app."],
  ["preserve Markdown formatting when copying", "fix", "复制选中文本时会完整保留粗体、链接与列表等原有格式。", "Copying selected text preserves bold, links, lists, and other formatting."],
  ["repair crash, updater and persistence bugs", "fix", "修复若干异常退出与配置保存问题，提升长时间使用稳定性。", "Fixed several stability and state-saving issues during long sessions."],
  ["prevent viewport scroll bounce", "fix", "连续输入或删除文字时，页面滚动位置平稳不跳动。", "Typing or deleting text no longer causes the scroll position to bounce."],
  ["load VFS state and initial note synchronously", "improve", "启动应用时首屏直接呈现上次打开的笔记，无需等待闪烁。", "Launching the app shows your last open note immediately on the first frame."],
  ["load local UI fonts in parallel", "improve", "启动时并行加载界面字体，主窗口打开更快。", "Interface fonts load in parallel on startup so the window opens faster."],
  ["copy selected images and file surfaces", "feat", "选中笔记里的图片或附件卡片后，可以直接复制为文件粘贴到别处。", "Selected images and file cards in a note can be copied directly as files."],
  ["align editable formats with paper preview", "improve", "导出可编辑文档时的字号与行距同纸张预览保持一致。", "Exported documents match the typography shown in the paper preview."],
  ["remember destinations and harden save", "improve", "导出笔记时会记住上次保存的文件夹位置。", "Exporting a note remembers the folder you chose last time."],
  ["Remove duplicate backend metadata", "improve", "精简设置页左侧导航栏，去掉重复的状态文字。", "Removed duplicate status labels from the settings navigation sidebar."],
  ["preserve themed links inside table cells", "fix", "导出包含链接的表格时，单元格里的链接样式完整保留。", "Links inside table cells keep their theme styling when exported."],
  ["cache agent model lists", "improve", "打开模型选择器时直接展示已缓存列表，不必每次重新加载。", "The model picker opens right away using its cached list."],
  ["tighten folder rules layout", "improve", "设置页里的文件夹规则排版更紧凑，名称输入框更宽。", "Folder rule settings have a tighter layout and a wider name field."],
  ["Unify clipboard selection semantics", "improve", "跨段落、列表与表格的复制粘贴行为更加统一自然。", "Copy and paste work consistently across paragraphs, lists, and tables."],
  ["stabilize agent settings layout", "fix", "助手设置面板边缘圆角与卡片布局更加稳固整齐。", "Agent settings keep a steady layout with cleaner rounded corners."],
  ["unify watermark branding", "improve", "导出长图与网页时使用统一简洁的品牌角标。", "Exported images and pages share a cleaner, consistent watermark."],
  ["support agy agent", "feat", "助手设置支持接入本地智能体服务协同编辑笔记。", "Added local agent integration for collaborative note editing."],
  ["remove voice input", "other", "精简不再使用的语音听写辅助模块，减小安装包体积。", "Removed the legacy dictation helper to keep the app lean."],
  ["inline text copy and paste in formatted blocks", "fix", "在标题、引用和列表中复制粘贴行内格式文字更加准确。", "Copying and pasting styled text inside headings, quotes, and lists is more accurate."],
  ["persist paper theme and page element", "feat", "导出窗口会自动记住你上次选择的纸张主题与页眉页脚开关。", "Export remembers your chosen paper theme and page element toggles."],
  ["isolate note presentation and preserve per-block", "fix", "多篇笔记切换时各自保留独立的视图滚动与段落尺寸。", "Switching notes keeps each note's scroll position and block layout separate."],
  ["preserve table widths and render local and remote images", "fix", "导出文档时完整保留表格列宽，并正常渲染本地与网络图片。", "Exports preserve table column widths and include both local and web images."],
  ["Preserve manual file order", "feat", "手动拖拽排好的笔记文件顺序会在切换笔记本后继续保留。", "Manual file ordering in the sidebar stays intact when switching notebooks."],
  ["show selection tools only for supported blocks", "fix", "划词工具栏只在支持文字排版的段落中弹出。", "The selection toolbar only appears inside text blocks that support formatting."],
  ["image resize handles and default to full content width", "improve", "新插入的图片默认铺满正文宽度，两侧缩放手柄更好拖拽。", "Newly inserted images fit the content width by default with smoother resize handles."],
  ["outline anchor navigation without moving the caret", "fix", "点击大纲标题跳转章节时不会打乱当前文字光标的位置。", "Clicking an outline heading scrolls to the section without moving your caret."],
  ["bold rendering for fonts without bold faces", "fix", "没有自带粗体字重的字体也能清晰显示加粗效果。", "Fonts without a dedicated bold face still render clear bold text."],
  ["remove Journal paper dot pattern", "improve", "日志纸张导出主题去掉背景点阵，版面更干净。", "Removed the background dot grid from the Journal export paper theme."],
  ["allow hash prefixes to change heading levels", "feat", "在标题行首增减井号即可快速切换标题级别。", "Typing or removing hash marks at the start of a heading changes its level."],
  ["show placeholders for empty headings", "improve", "空白标题行会显示淡淡的层级占位提示。", "Empty headings display a subtle level placeholder."],
  ["Recalculate numbered lists", "fix", "拆分或导入有序列表后会自动重新连续编号。", "Numbered lists renumber cleanly after splitting or importing items."],
  ["Synthesize missing italic faces", "fix", "缺少斜体字库的字体也能正常渲染斜体样式。", "Fonts without a native italic face still render slanted text cleanly."],
  ["Render AI content with editor styles", "improve", "助手回复直接采用与正文一致的排版样式展示。", "Assistant replies render with the same clean typography as the note editor."],
  ["persistent notebook access", "fix", "重启应用后自动恢复笔记本文件夹访问授权，无需重复选择。", "Notebook folder permissions are restored automatically across restarts."],
  ["lossless standard Markdown serialization", "fix", "特殊字符与转义符号在保存和重新打开后保持原样。", "Special characters and entities round-trip cleanly when saving notes."],
  ["prevent AiChat re-entrant panic", "fix", "修复云盘同步冲突与高频对话刷新时的偶发卡顿。", "Fixed occasional stalls during cloud drive sync conflicts and rapid chat updates."],
  ["Match titlebar action edge spacing", "improve", "顶栏右侧操作按钮的边距与窗口左上角交通灯对称协调。", "Title-bar buttons match the edge spacing of the window controls."],
  ["Show settings navigation before collapsing", "improve", "窗口变窄时会尽可能保留设置页左侧分类导航。", "Settings keeps its side navigation visible longer before collapsing in narrow windows."],
  ["initial notebook switcher highlight", "fix", "打开笔记本切换菜单时准确高亮当前所在的笔记本。", "Opening the notebook switcher highlights the active notebook right away."],
  ["insertion indicators and persistent file list reordering", "feat", "侧栏文件列表与笔记本支持拖拽排序，并带有清晰的插入指示线。", "Sidebar files and notebooks can be reordered by dragging with clear drop lines."],
  ["redesign AI & Local Agents settings", "feat", "助手设置改为左右分栏卡片，切换与配置智能体更直观。", "Redesigned assistant settings into a split view for easier agent setup."],
  ["redesign agent and model selector", "feat", "助手与模型选择器支持搜索过滤与思考强度调节。", "The agent and model picker adds search filtering and thinking depth controls."],
  ["show plan diff directly", "feat", "助手给出的多处修改会直接展示增删对比，包括表格变动。", "Assistant edit plans display inline additions and deletions, including table changes."],
  ["quick-note panel independent", "improve", "每次打开速记，它都单独待着，不再跟着主窗口绑在一起。", "Quick Note stays on its own each time you open it, instead of tying itself to the main window."],
  ["Option-B sidebar toggle", "feat", "可以显示或收起侧栏；指向顶栏上的按钮时，会说明它是做什么的。", "You can show or hide the sidebar, and the title bar explains the control when you point at it."],
  ["Simplify feedback categories", "improve", "反馈的类型更少、说法更清楚，写完后顶部标题会收起。", "Feedback types are fewer and the wording is clearer, and the header hides when you finish."],
  ["Recreate Beautiful UI", "improve", "常用控件重新画过，助手对话看起来更简单。", "Shared controls were redrawn, and assistant chat is simpler to look at."],
  ["decouple builtin AI", "feat", "内置助手不再绑在总开关上。每个智能体可以单独设置启动参数和优先级。", "The built-in assistant is no longer tied to one master switch. Each agent can have its own launch options and priority."],
  ["move entry to Block menu", "feat", "性能监视器改到应用菜单里打开，并有了快捷键。", "The performance monitor opens from the app menu, and it has a shortcut."],
  ["align composer input edges", "fix", "对话框和下面的按钮对齐了，发送图标也更清楚。", "The chat box lines up with the buttons under it, and the send icon is sharper."],
  ["refine monitor UI", "feat", "性能监视器可以拖动，会显示正在加载，统计笔记本占用时会跳过你设为忽略的文件夹。", "The performance monitor can be dragged, shows a loading state, and skips folders you set to ignore when measuring the notebook."],
  ["remove review button", "feat", "笔记的修改建议会直接在编辑器里打开给你看，不再单独放一个审阅按钮。", "A suggested change to a note opens in the editor for review, without a separate review button."],
  ["Simplify agent selection and save", "improve", "挑选智能体时不会立刻启用，确认后才会保存。", "You can look through agents without turning one on, and the choice is saved when you confirm."],
  ["fit new tables", "fix", "新建的表格会适应页面宽度；整张表被选中时，可以把它删掉。", "A new table fits the page, and selecting the whole table deletes it."],
  ["prefer above control bounds", "fix", "提示优先出现在按钮上方，靠近屏幕边缘时会挪开，避免被挡住。", "Tips prefer to appear above a control, and they move so they stay on screen."],
  ["move-to-trash permission", "fix", "把笔记移到废纸篓不再因为权限失败；也可以把文件夹拖到侧栏来打开。", "Moving a note to the Trash no longer fails with a permission error, and you can drop a folder on the sidebar to open it."],
  ["Separate agent selection from activation", "feat", "选择智能体和启用它分成了两步，智能体卡片也更大。", "Choosing an agent is separate from turning it on, and the agent cards are larger."],
  ["eliminate empty diff rows", "improve", "编辑器里查看修改建议的卡片重新排过，不再出现空行。", "The card that reviews a suggested change in the note was redesigned, and it no longer shows empty rows."],
  ["text overflow and skip code fence", "fix", "审阅卡片里的文字不再溢出；代码块两端的标记行不会被当成修改。", "Text in the review card stays inside the card, and the opening and closing lines of a code block are not shown as changes."],
  ["nested list code rendering", "fix", "嵌套列表里的代码能正常显示；助手给出的笔记修改会先检查，再写入。", "Code inside a nested list renders correctly, and an assistant suggestion is checked before it changes the note."],
  ["full-permissions toggle", "improve", "助手设置收成完整权限开关，以及后端实际提供的模型列表。", "Assistant settings are a switch for full permissions and the model list the backend actually returns."],
];

const TOPIC_FALLBACKS = [
  [/updater|update|mirror|release/i, "支持应用内检查更新、跨版本日志展示与平滑升级。", "Improved in-app update checks, release notes display, and smooth installation."],
  [/table|column|cell/i, "改进表格编辑、列宽调整与单元格选择体验。", "Improved table editing, column resizing, and cell selection."],
  [/annotat|reaction/i, "优化段落批注与表情回应的展示和交互细节。", "Refined block annotations and reaction interactions."],
  [/export|paper|watermark/i, "优化笔记导出排版、纸张主题与图片渲染效果。", "Improved note export layout, paper themes, and image rendering."],
  [/font|caret|cursor|ime/i, "优化字体渲染、输入法候选框与文字光标定位。", "Improved font rendering, input method handling, and caret placement."],
  [/sidebar|notebook|tree|tab/i, "优化侧栏文件树、标签页与笔记本切换体验。", "Refined the sidebar file tree, tabs, and notebook switching."],
  [/search|find|outline/i, "优化全局搜索、页内查找与大纲定位体验。", "Improved search, in-note find, and outline navigation."],
  [/drag|drop|reorder/i, "优化段落与列表拖拽移动时的跟手度与指示线。", "Smoothed block and list drag reordering with clearer drop indicators."],
  [/copy|paste|clipboard|markdown/i, "优化复制粘贴与文档格式转换的准确性。", "Improved copy-and-paste fidelity and document formatting."],
  [/ai|agent|model|prompt|chat/i, "优化智能体连接、模型选择与对话辅助体验。", "Improved assistant connection, model selection, and inline editing."],
  [/perf|memory|monitor/i, "优化运行性能与资源占用表现。", "Improved responsiveness and memory efficiency."],
  [/setting|shortcut|menu|icon|theme/i, "打磨设置面板、快捷键与菜单界面细节。", "Polished settings, keyboard shortcuts, and menu details."],
];

export function kindOf(subject) {
  const typed = String(subject).match(/^([a-z]+)(?:\([^)]*\))?!?:/i)?.[1]?.toLowerCase();
  if (typed === "feat") return "feat";
  if (typed === "fix") return "fix";
  if (["perf", "refactor", "style", "improve"].includes(typed || "")) return "improve";
  if (["ci", "build", "chore", "docs", "test", "design", "release"].includes(typed || "")) return "other";
  if (/^(fix|resolve)\b/i.test(subject)) return "fix";
  if (/^(add|introduce|support)\b/i.test(subject)) return "feat";
  return "improve";
}

function finish(text, punct) {
  const clean = String(text).trim().replace(/[.。!！?？]+$/, "");
  return `${clean}${punct}`;
}

function sanitizeEnglish(raw) {
  let text = String(raw || "")
    .replace(/`+/g, "")
    .replace(/\(#\d+\)/g, "")
    .replace(/#\d+\b/g, "")
    .replace(/#([A-Za-z]+)/g, "$1")
    .replace(/[\u3400-\u9fff]+/g, " ")
    .replace(/\bgpui-base\b/gi, "shared interface")
    .replace(/\bgpui\b/gi, "window")
    .replace(/\bwgpu\b/gi, "graphics")
    .replace(/\bwasm\b/gi, "web")
    .replace(/\bipc\b/gi, "background")
    .replace(/\bdebounce\b/gi, "timing")
    .replace(/\btypecheck\b/gi, "validation")
    .replace(/\b[0-9a-f]{7,40}\b/gi, "")
    .replace(/[A-Za-z0-9_]+::[A-Za-z0-9_:]+/g, "status")
    .replace(/\b[a-z]+(?:_[a-z0-9]+)+\b/gi, (m) => m.replaceAll("_", " "))
    .replace(/\s+/g, " ")
    .trim();
  if (!text || text.length < 4) {
    text = "Polished interface details and everyday editing reliability";
  }
  return finish(text.charAt(0).toUpperCase() + text.slice(1), ".");
}

export function describeCommit(commit) {
  const subject = String(commit.subject || "").replace(/\s*\(#\d+\)$/, "").trim();
  const zhTrailer = String(commit.releaseNote || "").trim();
  const enTrailer = String(commit.releaseNoteEn || "").trim();
  const kind = kindOf(subject);

  if (zhTrailer && enTrailer && /[\u3400-\u9fff]/.test(zhTrailer) && !/[\u3400-\u9fff]/.test(enTrailer)) {
    const cleanZh = zhTrailer.replace(/[A-Za-z]{2,}(?:\s+[A-Za-z]{2,})+/g, "").replace(/\s+/g, " ").trim();
    if (cleanZh) {
      return { kind, zh: finish(cleanZh, "。"), en: sanitizeEnglish(enTrailer) };
    }
  }

  const known = BLURBS.find(([pattern]) => subject.toLowerCase().includes(pattern.toLowerCase()));
  if (known) {
    return { kind: known[1], zh: finish(known[2], "。"), en: sanitizeEnglish(known[3]) };
  }

  const fromRule = bilingualFromCommit(commit);
  if (fromRule && !/[A-Za-z]{2,}\s+[A-Za-z]{2,}/.test(fromRule.zh)) {
    return { kind: fromRule.kind, zh: finish(fromRule.zh, "。"), en: sanitizeEnglish(fromRule.en) };
  }

  const bare = subject.replace(/^[a-z]+(?:\([^)]*\))?!?:\s*/i, "").trim() || subject;
  const topic = TOPIC_FALLBACKS.find(([re]) => re.test(subject));
  if (topic) {
    return { kind, zh: finish(topic[1], "。"), en: sanitizeEnglish(bare) };
  }

  const zh = kind === "fix"
    ? "修复了一处日常编辑与界面交互中的细节问题。"
    : kind === "feat"
      ? "新增了一项提升日常记录与编辑效率的实用功能。"
      : kind === "other"
        ? "完善了安装包构建与跨平台发布体验。"
        : "优化了界面细节与日常编辑操作的顺滑度。";
  return { kind, zh, en: sanitizeEnglish(bare) };
}
