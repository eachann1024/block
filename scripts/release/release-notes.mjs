import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { request } from "node:https";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const DEFAULT_CHANGELOG_MODEL = "antigravity/gemini-3.8-flash";
export const SECTION_HEADERS_ZH = ["✨ 新功能", "🪐 优化", "🐛 修复"];
export const SECTION_HEADERS_EN = ["✨ New", "🪐 Improved", "🐛 Fixed"];
export const SECTION_HEADERS = SECTION_HEADERS_ZH;
const DIR = fileURLToPath(new URL(".", import.meta.url));
const ROOT = resolve(DIR, "../..");
const PROMPT_PATH = resolve(DIR, "changelog-prompt.md");
const MAX_ATTEMPTS = 2;
const TIMEOUT_MS = 90_000;
const MAX_PER_SECTION = 6;
const BOT_AUTHORS = new Set(["github-actions[bot]", "github-actions"]);
const SKIP_PREFIX = /^(ci|docs|build|test|chore|design|release)(\([^)]*\))?:/i;
const RELEASE_SUBJECT = /^(chore\(release\):|release:)/i;
const TECH_WORDS = /\b(?:ci|pr|lint|refactor|typecheck|ipc|commit|feat|fix|chore|build|hydration|debounce|renderer|gpui|wgpu|wasm|sdk|npm|bun|cargo|hover|fallback|token|overlay|ime)\b|#\d+|\b[0-9a-f]{7,40}\b|`/i;
const VAGUE = /^(?:修复了?)?(?:优化了?性能|提升了?体验|一些已知问题|若干细节|细节调整|修复了?一些问题)[。.]?$/;

export function isMetaPath(path) {
  return /^(\.github\/|scripts\/|designs\/|docs\/|tests?\/|benches\/|web\/vendor\/)/.test(path)
    || [" README.md", "README.md", "LICENSE", "NOTICE", "DESIGN.md", ".gitignore", "Cargo.toml", "Cargo.lock", "package.json"].includes(path);
}

export function isExcludedCommit({ author = "", subject = "", paths = [], releaseNote = "" }) {
  if (BOT_AUTHORS.has(author)) return true;
  if (/^none$/i.test(releaseNote.trim())) return true;
  if (SKIP_PREFIX.test(subject) || RELEASE_SUBJECT.test(subject)) return true;
  if (paths.length > 0 && paths.every((p) => isMetaPath(p))) return true;
  return false;
}

function splitBilingualNote(raw = "") {
  const text = raw.trim();
  if (!text) return { zh: "", en: "" };
  if (text.includes("|||")) {
    const [zh, ...rest] = text.split("|||");
    return { zh: zh.trim(), en: rest.join("|||").trim() };
  }
  return { zh: text, en: "" };
}

export function listCommits(root = ROOT, rangeSpec = "-n 20") {
  const args = ["log", "--no-merges", "--reverse", "--format=%H%x00%an%x00%s%x00%(trailers:key=Release-Note,valueonly,separator=%x20)%x00%(trailers:key=Release-Note-En,valueonly,separator=%x20)"];
  if (rangeSpec.startsWith("-n")) args.push(...rangeSpec.split(/\s+/));
  else args.push(rangeSpec);
  const out = execFileSync("git", args, { cwd: root, encoding: "utf8" }).trimEnd();
  if (!out) return [];
  return out.split("\n").filter(Boolean).map((line) => {
    const [sha, author, subject, releaseNoteRaw = "", releaseNoteEnRaw = ""] = line.split("\x00");
    const paths = execFileSync("git", ["diff-tree", "--no-commit-id", "--name-only", "-r", sha], { cwd: root, encoding: "utf8" })
      .trimEnd().split("\n").filter(Boolean);
    const split = splitBilingualNote(releaseNoteRaw);
    const releaseNote = split.zh;
    const releaseNoteEn = (releaseNoteEnRaw || "").trim() || split.en;
    return { sha, author, subject, releaseNote, releaseNoteEn, paths };
  });
}

function stripFence(text) {
  return text.replace(/\r\n?/g, "\n").trim().replace(/^```(?:markdown|md)?\n([\s\S]*?)\n```$/i, "$1").trim();
}

function validateLocaleNotes(raw, { highlightPrefix, headers, endPunct }) {
  const lines = raw.split("\n").map((l) => l.trim()).filter(Boolean);
  assert.ok(lines.length >= 3, "输出内容过短");
  assert.ok(lines[0].startsWith(highlightPrefix), `首行必须以「${highlightPrefix}」开头：${lines[0]}`);
  const summaryBody = lines[0].slice(highlightPrefix.length).trim();
  assert.ok(summaryBody.length >= 4 && !TECH_WORDS.test(summaryBody), `更新重点不合格：${lines[0]}`);
  const punctRe = endPunct === "。" ? /[。！？]$/ : /[.!?]$/;
  const defaultPunct = endPunct;
  const prefixOut = endPunct === "." ? `${highlightPrefix} ` : highlightPrefix;
  const out = [`${prefixOut}${punctRe.test(summaryBody) ? summaryBody : `${summaryBody}${defaultPunct}`}`];
  let sectionIdx = -1;
  let count = 0;
  for (const line of lines.slice(1)) {
    const headerIdx = headers.indexOf(line.replace(/^#+\s*/, ""));
    if (headerIdx >= 0) {
      assert.ok(headerIdx > sectionIdx, `分类顺序或重复有误：${line}`);
      assert.ok(sectionIdx < 0 || count > 0, `「${headers[sectionIdx]}」下没有条目`);
      out.push("", headers[headerIdx]);
      sectionIdx = headerIdx;
      count = 0;
      continue;
    }
    const bullet = /^[-*•]\s+(.+)$/.exec(line);
    assert.ok(bullet && sectionIdx >= 0, `格式不合法：${line}`);
    const item = bullet[1].replace(/\*\*/g, "").trim();
    assert.ok(!TECH_WORDS.test(item) && !VAGUE.test(item), `条目含技术词或空话：${item}`);
    count += 1;
    assert.ok(count <= MAX_PER_SECTION, `单类条目超过 ${MAX_PER_SECTION} 条`);
    out.push(`- ${punctRe.test(item) ? item : `${item}${defaultPunct}`}`);
  }
  assert.ok(sectionIdx >= 0 && count > 0, "缺少有效更新条目");
  return out.join("\n");
}

export function validateShipReleaseNotes(raw) {
  const text = stripFence(raw);
  if (!text || /^[（(]?无[)）]?[。.]?$/i.test(text) || /^none$/i.test(text)) return "";
  if (text.includes("\n---\n")) {
    const [enRaw, zhRaw] = text.split(/\n---\n/, 2);
    const en = validateLocaleNotes(enRaw.trim(), {
      highlightPrefix: "🌈 Highlights:",
      headers: SECTION_HEADERS_EN,
      endPunct: ".",
    });
    const zh = validateLocaleNotes(zhRaw.trim(), {
      highlightPrefix: "🌈 本次更新重点：",
      headers: SECTION_HEADERS_ZH,
      endPunct: "。",
    });
    return `${en}\n\n---\n\n${zh}`;
  }
  // Backward compatible: Chinese-only model output becomes bilingual by leaving EN absent — caller wraps.
  return validateLocaleNotes(text, {
    highlightPrefix: "🌈 本次更新重点：",
    headers: SECTION_HEADERS_ZH,
    endPunct: "。",
  });
}

export function combineBilingualNotes(enMarkdown, zhMarkdown) {
  assert.ok(enMarkdown && zhMarkdown, "双语更新说明缺少英文或中文");
  return `${enMarkdown.trim()}\n\n---\n\n${zhMarkdown.trim()}`;
}

function pinnedFetch(caFile) {
  const ca = readFileSync(caFile, "utf8");
  return (url, { method, headers, body, signal }) => new Promise((resolveRes, reject) => {
    const req = request(url, { method, headers, ca, signal }, (res) => {
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolveRes({
        ok: res.statusCode >= 200 && res.statusCode < 300,
        status: res.statusCode,
        json: async () => JSON.parse(Buffer.concat(chunks).toString("utf8")),
      }));
      res.on("error", reject);
    });
    req.on("error", reject);
    req.end(body);
  });
}

export async function polishReleaseNotes({ version, commits, env = process.env }) {
  const baseUrl = env.MAGPIE_BASE_URL?.trim();
  if (!baseUrl) return { status: "fallback", reason: "未配置 MAGPIE_BASE_URL" };
  const caFile = env.MAGPIE_CA_CERT?.trim();
  const fetchImpl = caFile ? pinnedFetch(resolve(ROOT, caFile)) : fetch;
  const model = env.CHANGELOG_MODEL?.trim() || DEFAULT_CHANGELOG_MODEL;
  const inputLines = commits.map(({ subject, releaseNote = "", releaseNoteEn = "" }) => {
    const clean = subject.replace(/\s*\(#\d+\)$/, "").trim();
    if (releaseNote && releaseNoteEn) return `${clean} | ${releaseNote} ||| ${releaseNoteEn}`;
    if (releaseNote) return `${clean} | ${releaseNote}`;
    if (releaseNoteEn) return `${clean} ||| ${releaseNoteEn}`;
    return clean;
  });
  if (!inputLines.length) return { status: "empty", model };
  const url = `${baseUrl.replace(/\/+$/, "").replace(/\/v1$/, "")}/v1/chat/completions`;
  const headers = { "content-type": "application/json" };
  if (env.MAGPIE_API_KEY?.trim()) headers.authorization = `Bearer ${env.MAGPIE_API_KEY.trim()}`;
  let lastError = "";
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    try {
      const res = await fetchImpl(url, {
        method: "POST",
        headers,
        signal: AbortSignal.timeout(TIMEOUT_MS),
        body: JSON.stringify({
          model,
          temperature: 0.3,
          messages: [
            { role: "system", content: readFileSync(PROMPT_PATH, "utf8") },
            { role: "user", content: `Block ${version} 的提交记录（共 ${inputLines.length} 条）：\n\n${inputLines.join("\n")}` },
          ],
        }),
      });
      assert.ok(res.ok, `HTTP ${res.status}`);
      const data = await res.json();
      const raw = data?.choices?.[0]?.message?.content;
      assert.ok(typeof raw === "string", "模型返回为空");
      let markdown = validateShipReleaseNotes(raw);
      if (!markdown) return { status: "empty", model: data.model || model };
      markdown = ensureBilingualMarkdown(markdown, commits);
      if (!markdown) return { status: "fallback", reason: "模型只返回了中文，且无法生成对应英文" };
      return { status: "polished", markdown, model: data.model || model };
    } catch (err) {
      lastError = `第 ${attempt} 次润色失败：${err?.message || err}`;
    }
  }
  return { status: "fallback", reason: lastError };
}

const SUBJECT_RULES = [
  [/active selection and close actions on single tab/i, "只有一个标签页时隐藏关闭按钮与选中底色，顶栏更简洁", "With a single tab, the close button and selected background are hidden for a cleaner top bar"],
  [/global static assets folder/i, "支持在「设置 → 通用与编辑」中配置静态资源文件夹", "You can set a static assets folder in Settings → General & Editing"],
  [/single-line draft annotation/i, "修复单行批注草稿文字与右侧操作按钮未垂直居中的问题", "Single-line annotation draft text and its action buttons now align vertically"],
  [/settings header back button/i, "精简设置页顶部返回按钮与多余卡片，界面更清爽", "The settings header is simpler, with a clearer back button and fewer extra cards"],
  [/double text contrast|canvas CJK glyphs/i, "修复网页端中文文字笔画发粗、对比度异常的问题", "Chinese text on the web no longer looks overly bold or low-contrast"],
  [/expand narrow-mode cards inline|badge popovers/i, "页边距充足时自动展开批注卡片，指向批注标记即可查看详情", "Annotation cards expand automatically when there is enough space; point at a mark to see details"],
  [/round-4 light\/dark variants|app icons/i, "换用全新浅色与深色模式应用图标", "Fresh light and dark app icons"],
  [/selection ring.*side pill resize handles for images/i, "图片选中框与两侧缩放手柄更清晰，拖拽调整大小更顺手", "Image selection handles are clearer, so resizing by drag feels smoother"],
  [/redesign annotation review cards and batch send/i, "支持批量发送多条批注审阅意见，卡片排版更清晰", "You can send multiple annotation reviews in one go, with clearer cards"],
  [/replace model label with filter input.*test button/i, "在「设置 → AI 助手」支持搜索筛选模型，连接测试成功后显示明确提示", "In Settings → AI Assistant you can search models, and a clear tip appears after a successful connection test"],
  [/stabilize sidebar tab geometry/i, "修复切换设置页左侧栏目时位置轻微跳动的问题", "Switching sidebar sections in Settings no longer causes a slight jump"],
  [/sync IME cursor geometry|guard IME composition/i, "修复中文输入法候选框位置偏移与选词被打断的问题", "Chinese input candidate windows stay aligned, and picking a word is no longer interrupted"],
  [/image container and selection box to rendered image/i, "修复笔记内图片选中框与实际显示大小不一致的问题", "Image selection boxes now match the displayed image size"],
  [/GitHub Ice dark syntax and card palette for code blocks/i, "代码块换用深色护眼配色，语法高亮更易读", "Code blocks use a darker eye-friendly palette with clearer highlighting"],
  [/load and render remote http\/https images/i, "支持直接显示笔记中的网络图片并自动缓存到本地", "Network images in notes display directly and are cached locally"],
  [/top hover insertion slot.*structural first block/i, "首段为表格或代码块时，支持在顶部插入新段落或按方向键跳出", "When the first block is a table or code block, you can insert above it or arrow-key out"],
  [/remove image export from tab context menu/i, "精简标签页右键菜单，移除重复的导出图片入口", "The tab context menu is simpler, without a duplicate export-image action"],
  [/vertically center key icons.*shortcut_label/i, "修复快捷键提示中的按键图标与文字未对齐的问题", "Shortcut key icons and labels now align"],
  [/full model tag list and disallow manual model/i, "AI 设置直接展示完整可用模型列表，点选更直观", "AI settings show the full list of available models for easier picking"],
  [/todo checkbox colors/i, "优化待办事项勾选框配色，完成状态更醒目", "Todo checkboxes use clearer colors so completed items stand out"],
  [/cross-cell text selection/i, "修复表格跨单元格划选文字时误选中相邻格子的问题", "Selecting text across table cells no longer grabs neighboring cells by mistake"],
  [/remove insert row\/column items from the table menu/i, "精简表格菜单，行列增删统一由边栏按钮完成", "The table menu is simpler; add or remove rows and columns from the side controls"],
  [/optically balance key icons/i, "统一快捷键按键图标视觉比例", "Shortcut key icons share a more consistent visual size"],
  [/drag a column boundary to resize.*double-click to reset/i, "支持拖动表格列边线调整列宽，双击即可恢复默认宽度", "Drag a table column edge to resize; double-click to reset the width"],
  [/keep column drag alive when the pointer leaves/i, "修复拖动表格列宽时鼠标移出编辑区导致拖拽中断的问题", "Dragging a column width no longer stops when the pointer leaves the editor"],
  [/Fit to page width toggle/i, "表格菜单新增「适应页面宽度」开关", "Table menu adds a Fit to page width toggle"],
  [/auto-scroll while dragging rows\/columns/i, "拖拽表格行列到边缘时支持自动滚动，落点更稳定", "Dragging table rows or columns near the edge auto-scrolls for steadier drops"],
  [/drag the add-column bar to grow or shrink columns/i, "支持按住表格右侧加号条连续增减列数", "Hold the add-column bar on the right to grow or shrink columns continuously"],
  [/persistent editor content zoom|quick-note main window/i, "笔记正文可以按比例缩放并记住，从速记回到主窗口时窗口会到前台", "Note content can be zoomed and remembered; returning from Quick Note brings the main window forward"],
  [/document icons from title bar tabs/i, "标签页不再显示文档小图标，顶栏更干净", "Tabs no longer show document icons, keeping the top bar cleaner"],
  [/theme appearance switch/i, "切换浅色或深色时不再突然退出", "Switching light or dark mode no longer unexpectedly quits the app"],
  [/inline code chip height/i, "行内代码里划选文字时，选中范围不再高出文字", "Highlighting text in inline code no longer extends above the text"],
  [/system appearance changes/i, "系统切换浅色或深色时，笔记外观会跟着变", "Notes follow the system when it switches light or dark mode"],
  [/caret remaining visible in inactive windows/i, "窗口不在前台时，文字光标不再一直留在笔记上", "The text cursor no longer stays on notes when the window is in the background"],
  [/V4 app icon|light\/dark icon variants/i, "换上新的应用图标", "Updated app icon"],
  [/top insert slot|non-paragraph blocks/i, "表格、代码块等块的上方也可以插入新段落", "You can insert a new paragraph above tables, code blocks, and other blocks"],
  [/Latin and math fonts/i, "西文和公式换用更清楚的字体", "Latin text and math use clearer fonts"],
  [/app and release version/i, "设置页会显示当前版本号", "Settings now shows the current version number"],
  [/minimum sidebar width/i, "侧栏加宽后，搜索框可以完整显示", "The search box shows fully when the sidebar is widened"],
  [/search label truncation/i, "窄侧栏里的搜索文字不再被截断", "Search text in a narrow sidebar is no longer cut off"],
  [/quick note modifier events/i, "速记里按下修饰键时，不再打断中文输入", "Pressing modifier keys in Quick Note no longer interrupts Chinese input"],
  [/inline code hover tooltips/i, "行内代码不再弹出悬停提示", "Inline code no longer shows a tip when you point at it"],
  [/inline code copy confirmation/i, "复制行内代码后会立刻出现已复制的提示", "Copying inline code immediately shows a copied notification"],
  [/reopening closed tabs/i, "可以用原来的快捷键重新打开刚关掉的标签页", "You can reopen a just-closed tab with the original shortcut"],
  [/outline chip style|caret escape at chip edges/i, "行内代码改成描边样式，光标走到边缘后可以继续移出", "Inline code uses an outlined style, and the cursor can move past its edges"],
];

function punctuate(text, endPunct) {
  const punctRe = endPunct === "。" ? /[。！？]$/ : /[.!?]$/;
  return punctRe.test(text) ? text : `${text}${endPunct}`;
}

function translateSubjectPair(subject, releaseNote = "", releaseNoteEn = "") {
  const source = (releaseNote || subject).replace(/\s*\(#\d+\)$/, "").trim();
  const body = source.replace(/^[a-z]+(\([^)]*\))?!?:\s*/i, "").trim();
  for (const [pattern, zh, en] of SUBJECT_RULES) {
    if (pattern.test(subject) || pattern.test(source)) {
      return {
        zh: punctuate(zh, "。"),
        en: punctuate(releaseNoteEn || en, "."),
      };
    }
  }
  if (releaseNote && releaseNoteEn) {
    return { zh: punctuate(releaseNote, "。"), en: punctuate(releaseNoteEn, ".") };
  }
  if (/[一-龥]/.test(body)) {
    const cleaned = body.replace(TECH_WORDS, "").replace(/\s+/g, " ").trim();
    if (cleaned.length >= 4) {
      const zh = punctuate(cleaned, "。");
      if (releaseNoteEn) return { zh, en: punctuate(releaseNoteEn, ".") };
      return { zh, en: null };
    }
  }
  if (releaseNoteEn) {
    return {
      zh: releaseNote ? punctuate(releaseNote, "。") : null,
      en: punctuate(releaseNoteEn, "."),
    };
  }
  return null;
}

function renderLocaleNotes({ highlights, buckets, highlightPrefix, headers, joiner, endPunct }) {
  if (highlights.length === 0) return "";
  const out = [`${highlightPrefix}${highlights.join(joiner)}${endPunct}`];
  for (const [key, header] of [["feat", headers[0]], ["opt", headers[1]], ["fix", headers[2]]]) {
    if (buckets[key].length > 0) {
      out.push("", header, ...buckets[key].map((item) => `- ${item}`));
    }
  }
  return out.join("\n");
}

export function fallbackReleaseNotes(commits) {
  const userCommits = commits.filter((c) => !isExcludedCommit(c));
  const bucketsZh = { feat: [], opt: [], fix: [] };
  const bucketsEn = { feat: [], opt: [], fix: [] };
  for (const commit of userCommits.slice().reverse()) {
    const m = /^([a-z]+)(?:\([^)]*\))?!?:\s*/i.exec(commit.subject);
    const prefix = m ? m[1].toLowerCase() : "";
    const kind = prefix === "fix" || /^fix\b/i.test(commit.subject) || /^keep\b/i.test(commit.subject)
      ? "fix"
      : prefix === "feat" || /^add\b/i.test(commit.subject) ? "feat" : "opt";
    const pair = translateSubjectPair(commit.subject, commit.releaseNote, commit.releaseNoteEn);
    if (!pair?.zh || bucketsZh[kind].includes(pair.zh) || bucketsZh[kind].length >= MAX_PER_SECTION) continue;
    bucketsZh[kind].push(pair.zh);
    if (pair.en && !bucketsEn[kind].includes(pair.en) && bucketsEn[kind].length < MAX_PER_SECTION) {
      bucketsEn[kind].push(pair.en);
    }
  }
  const allZh = [...bucketsZh.feat, ...bucketsZh.opt, ...bucketsZh.fix];
  const allEn = [...bucketsEn.feat, ...bucketsEn.opt, ...bucketsEn.fix];
  if (allZh.length === 0 || allEn.length === 0) return "";
  const zh = renderLocaleNotes({
    highlights: allZh.slice(0, 2).map((s) => s.replace(/[。！？]$/, "")),
    buckets: bucketsZh,
    highlightPrefix: "🌈 本次更新重点：",
    headers: SECTION_HEADERS_ZH,
    joiner: "；",
    endPunct: "。",
  });
  const en = renderLocaleNotes({
    highlights: allEn.slice(0, 2).map((s) => s.replace(/[.!?]$/, "")),
    buckets: bucketsEn,
    highlightPrefix: "🌈 Highlights: ",
    headers: SECTION_HEADERS_EN,
    joiner: ". ",
    endPunct: ".",
  });
  return combineBilingualNotes(en, zh);
}

function ensureBilingualMarkdown(markdown, commits) {
  if (!markdown) return "";
  if (markdown.includes("\n---\n")) return markdown;
  // Model returned Chinese-only: rebuild English from rules for the same commits.
  const fallback = fallbackReleaseNotes(commits);
  if (!fallback) return "";
  const en = fallback.split(/\n---\n/, 2)[0].trim();
  return combineBilingualNotes(en, markdown);
}

export async function buildReleaseNotes({ version, commits, env = process.env }) {
  const userCommits = commits.filter((c) => !isExcludedCommit(c));
  if (userCommits.length === 0) return { skip: true, reason: "没有面向用户的新提交", markdown: "" };
  const polished = await polishReleaseNotes({ version, commits: userCommits, env });
  if (polished.status === "polished") {
    return { skip: false, source: "polished", model: polished.model, markdown: polished.markdown };
  }
  if (polished.status === "empty") {
    return { skip: true, reason: `${polished.model} 判断无用户可感知改动`, markdown: "" };
  }
  const markdown = fallbackReleaseNotes(userCommits);
  if (!markdown) return { skip: true, reason: polished.reason || "没有可写给用户的更新", markdown: "" };
  return { skip: false, source: "fallback", reason: polished.reason, markdown };
}

if (import.meta.main) {
  const sinceIdx = process.argv.indexOf("--since");
  const limitIdx = process.argv.indexOf("--limit");
  const range = sinceIdx >= 0 && process.argv[sinceIdx + 1]
    ? `${process.argv[sinceIdx + 1]}..HEAD`
    : `-n ${limitIdx >= 0 && process.argv[limitIdx + 1] ? process.argv[limitIdx + 1] : "12"}`;
  const commits = listCommits(ROOT, range);
  const count = execFileSync("git", ["rev-list", "--count", "HEAD"], { cwd: ROOT, encoding: "utf8" }).trim();
  const result = await buildReleaseNotes({ version: `1.0.${count}`, commits });
  console.log(result.markdown || `跳过：${result.reason}`);
}
