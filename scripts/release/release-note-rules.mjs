const RULES = [
  [/active selection and close actions on single tab/i, "feat", "只有一个标签页时，关闭按钮和选中底色会收起，顶栏更干净", "With one tab open, the close button and highlight stay hidden so the tab bar is quieter"],
  [/global static assets folder/i, "feat", "可以在「设置 → 通用与编辑」里指定静态资源文件夹", "You can choose a static assets folder in Settings → General & Editing"],
  [/single-line draft annotation/i, "fix", "单行批注草稿的文字和右侧按钮对齐了", "Single-line annotation drafts now line up with their buttons"],
  [/settings header back button/i, "improve", "设置页顶部的返回按钮更简洁", "The Settings back button is simpler"],
  [/double text contrast|canvas CJK glyphs/i, "fix", "网页版里的中文不再显得又粗又糊", "Chinese text on the web version no longer looks overly bold"],
  [/expand narrow-mode cards inline|badge popovers/i, "improve", "页边够宽时批注卡片会自动展开，指向标记就能看内容", "Annotation cards open when there is room, and pointing at a marker shows the note"],
  [/round-4 light\/dark variants|app icons/i, "improve", "浅色和深色模式换了新的应用图标", "The app icon has new light and dark versions"],
  [/selection ring.*side pill resize handles for images/i, "improve", "选中图片后，两侧的缩放手柄更好拖", "Image resize handles are easier to drag"],
  [/redesign annotation review cards and batch send/i, "feat", "可以一次送出多条批注意见，卡片也更好读", "You can send several annotation comments at once, and the cards are easier to read"],
  [/replace model label with filter input.*test button/i, "feat", "在「设置 → AI 助手」里可以搜索模型，连接成功会有明确提示", "Settings → AI Assistant lets you search models and confirms a successful connection"],
  [/stabilize sidebar tab geometry/i, "fix", "切换设置页左侧栏目时，位置不再轻轻跳一下", "Switching sections in the Settings sidebar no longer jumps"],
  [/sync IME cursor geometry|guard IME composition/i, "fix", "中文输入法的候选框不再偏掉，选词也不容易被打断", "The Chinese input candidate window stays in place, and choosing a word is no longer interrupted"],
  [/image container and selection box to rendered image/i, "fix", "图片的选中框和实际显示的大小一致了", "An image selection frame now matches the image you see"],
  [/GitHub Ice dark syntax and card palette for code blocks/i, "improve", "代码块改用更耐看的深色配色，高亮更好认", "Code blocks use a darker palette that is easier to read"],
  [/load and render remote http\/https images/i, "feat", "笔记里的网络图片可以直接显示，并缓存到本地", "Images from the web show up in the note and are kept locally"],
  [/top hover insertion slot.*structural first block/i, "feat", "笔记开头是表格或代码块时，可以在上面插入段落，或用方向键走出去", "When a note starts with a table or code block, you can add a paragraph above it or move out with the arrow key"],
  [/remove image export from tab context menu/i, "improve", "标签页右键菜单里不再重复出现导出图片", "The tab menu no longer repeats Export image"],
  [/vertically center key icons.*shortcut_label/i, "fix", "快捷键提示里的按键图标和文字对齐了", "Key icons in shortcut hints line up with the labels"],
  [/full model tag list and disallow manual model/i, "improve", "AI 设置里直接列出可以点选的模型", "AI settings list the models you can pick"],
  [/todo checkbox colors/i, "improve", "待办勾选框的颜色更好认，完成后更醒目", "To-do checkboxes are easier to see, including completed ones"],
  [/cross-cell text selection/i, "fix", "在表格里跨格子划选文字时，不会把旁边的格子一起选中", "Selecting text across table cells no longer grabs the next cell"],
  [/remove insert row\/column items from the table menu/i, "improve", "表格菜单更短，增减行列用边上的按钮", "The table menu is shorter; add or remove rows and columns from the edge buttons"],
  [/optically balance key icons/i, "improve", "快捷键按键图标的大小更统一", "Shortcut key icons are a consistent size"],
  [/drag a column boundary to resize.*double-click to reset/i, "feat", "可以拖动表格列边改变列宽，双击恢复默认宽度", "Drag a table column edge to resize it, and double-click to reset the width"],
  [/keep column drag alive when the pointer leaves/i, "fix", "拖动列宽时，指针离开编辑区也不会中断", "Resizing a column continues if the pointer leaves the editor"],
  [/Fit to page width toggle/i, "feat", "表格菜单里可以让表格适应页面宽度", "The table menu can fit the table to the page width"],
  [/auto-scroll while dragging rows\/columns/i, "improve", "把表格的行或列拖到边缘时会自动滚动", "Dragging a table row or column to the edge scrolls the note"],
  [/drag the add-column bar to grow or shrink columns/i, "feat", "按住表格右侧的加号条，可以连续增减列", "Drag the plus bar on a table's right edge to add or remove columns"],
  [/persistent editor content zoom|editor content zoom/i, "feat", "编辑器会记住缩放比例；从速记回到主窗口时，主窗口会到前面来", "The editor remembers your zoom, and Quick Note brings the main window forward"],
  [/Remove document icons from title bar tabs/i, "improve", "顶栏标签不再显示文档小图标", "Title-bar tabs no longer show a document icon"],
  [/Sync app theme automatically when system appearance/i, "feat", "应用会跟着系统的浅色或深色外观切换", "Block follows the system light or dark appearance"],
  [/entity reentrancy crash on theme appearance/i, "fix", "切换浅色或深色时不再闪退", "Switching light and dark appearance no longer quits the app"],
  [/Keep text selection within inline code chip height/i, "fix", "行内代码的选中高亮不再高出文字", "Selecting inside inline code stays within the code"],
  [/Fix reopening closed tabs with platform shortcuts/i, "fix", "可以用原来的快捷键重新打开刚关掉的标签页", "The usual shortcut opens a tab you just closed"],
  [/Inline code: outline chip style/i, "improve", "行内代码改成描边样式，点一下可以复制，光标在两端也能走出去", "Inline code has an outline, a click copies it, and the cursor can leave either end"],
  [/immediate inline code copy confirmation/i, "improve", "复制行内代码后会马上提示已复制", "Copying inline code confirms immediately"],
  [/hide.*cursor.*inactive window|inactive window.*cursor/i, "fix", "窗口不在前面时，文字光标会藏起来", "The text cursor hides while the window is in the background"],
  [/first block.*insert|top insert/i, "feat", "可以在第一个块的上方插入新段落", "You can insert a paragraph above the first block"],
  [/reply copy|copy icon|copy button/i, "feat", "鼠标停在 AI 回复上时，右下角会出现复制按钮", "Resting on an AI reply shows a copy button at the bottom right"],
  [/performance monitor|storage monitor/i, "feat", "可以查看应用当前的运行情况和笔记本占用的空间", "You can see how Block is running and how much space the notebook uses"],
  [/outline bar|heading outline/i, "feat", "编辑器左上角可以展开大纲，窄窗口里会自动收起", "An outline in the top-left lists headings and hides itself in a narrow window"],
];

const CJK = /[\u3400-\u9fff]/;

function kindOf(subject) {
  const typed = /^([a-z]+)(?:\([^)]*\))?!?:/i.exec(subject)?.[1]?.toLowerCase() ?? "";
  if (typed === "fix" || /^(fix|resolve|correct|prevent)\b/i.test(subject)) return "fix";
  if (typed === "feat") return "feat";
  if (/polish|simplify|adjust|improve|streamline|redesign|perf\b/i.test(subject)) return "improve";
  return "feat";
}

function sentence(text, cjk) {
  const body = text.trim();
  if (!body) return "";
  if (cjk) return /[。！？]$/.test(body) ? body : `${body}。`;
  return /[.!?]$/.test(body) ? body : `${body}.`;
}

export function bilingualFromCommit(commit) {
  const subject = (commit.subject || "").replace(/\s*\(#\d+\)$/, "").trim();
  const note = (commit.releaseNote || "").trim();
  const noteEn = (commit.releaseNoteEn || "").trim();
  const source = note || subject;
  for (const [pattern, kind, zh, en] of RULES) {
    if (pattern.test(source) || pattern.test(subject)) {
      return { kind, zh: sentence(zh, true), en: sentence(en, false) };
    }
  }
  if (note && noteEn && CJK.test(note) && !CJK.test(noteEn)) {
    return { kind: kindOf(subject), zh: sentence(note, true), en: sentence(noteEn, false) };
  }
  const body = subject.replace(/^[a-z]+(?:\([^)]*\))?!?:\s*/i, "").trim();
  if (CJK.test(body) && noteEn && !CJK.test(noteEn)) {
    return { kind: kindOf(subject), zh: sentence(body, true), en: sentence(noteEn, false) };
  }
  return null;
}
