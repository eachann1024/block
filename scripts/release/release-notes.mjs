import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { request } from "node:https";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const DEFAULT_CHANGELOG_MODEL = "antigravity/gemini-3.8-flash";
export const SECTION_HEADERS = ["✨ 新功能", "🪐 优化", "🐛 修复"];
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

export function listCommits(root = ROOT, rangeSpec = "-n 20") {
  const args = ["log", "--no-merges", "--reverse", "--date=format-local:%Y-%m-%d", "--format=%H%x00%an%x00%cd%x00%s%x00%(trailers:key=Release-Note,valueonly,separator=%x20)"];
  if (rangeSpec.startsWith("-n")) args.push(...rangeSpec.split(/\s+/));
  else args.push(rangeSpec);
  const out = execFileSync("git", args, { cwd: root, encoding: "utf8", env: { ...process.env, TZ: "Asia/Shanghai" } }).trimEnd();
  if (!out) return [];
  return out.split("\n").filter(Boolean).map((line) => {
    const [sha, author, date, subject, releaseNote = ""] = line.split("\x00");
    const paths = execFileSync("git", ["diff-tree", "--no-commit-id", "--name-only", "-r", sha], { cwd: root, encoding: "utf8" })
      .trimEnd().split("\n").filter(Boolean);
    return { sha, author, date, subject, releaseNote: releaseNote.trim(), paths };
  });
}

function stripFence(text) {
  return text.replace(/\r\n?/g, "\n").trim().replace(/^```(?:markdown|md)?\n([\s\S]*?)\n```$/i, "$1").trim();
}

const EN_HEADERS = ["✨ New", "🪐 Improvements", "🐛 Fixes"];

function validateEnglishNotes(raw) {
  const lines = raw.split("\n").map((l) => l.trim()).filter(Boolean);
  assert.ok(lines.length >= 3, "英文内容过短");
  assert.ok(lines[0].startsWith("🌈 Highlights:"), `英文首行必须以「🌈 Highlights:」开头：${lines[0]}`);
  assert.ok(lines[0].slice("🌈 Highlights:".length).trim().length >= 4, "英文重点过短");
  const out = [lines[0]];
  let idx = -1;
  let count = 0;
  for (const line of lines.slice(1)) {
    const h = EN_HEADERS.indexOf(line);
    if (h >= 0) {
      assert.ok(h > idx, `英文分类顺序或重复有误：${line}`);
      assert.ok(idx < 0 || count > 0, `「${EN_HEADERS[idx]}」下没有条目`);
      out.push("", line);
      idx = h;
      count = 0;
      continue;
    }
    const b = /^[-*•]\s+(.+)$/.exec(line);
    assert.ok(b && idx >= 0, `英文格式不合法：${line}`);
    const item = b[1].replace(/\*\*/g, "").trim();
    assert.ok(!/`|#\d+|\b[0-9a-f]{7,40}\b/i.test(item), `英文条目含技术词：${item}`);
    count += 1;
    assert.ok(count <= MAX_PER_SECTION, `英文单类条目超过 ${MAX_PER_SECTION} 条`);
    out.push(`- ${/[.!?]$/.test(item) ? item : `${item}.`}`);
  }
  assert.ok(idx >= 0 && count > 0, "英文缺少有效更新条目");
  return out.join("\n");
}

export function validateShipReleaseNotes(raw) {
  const text = stripFence(raw);
  if (!text || /^[（(]?无[)）]?[。.]?$/.test(text)) return "";
  const parts = text.split(/\n\s*---+\s*\n/);
  if (parts.length > 1) {
    assert.equal(parts.length, 2, "双语内容只能用一个 --- 分隔");
    return `${validateShipReleaseNotes(parts[0])}\n\n---\n\n${validateEnglishNotes(parts[1].trim())}`;
  }
  const lines = text.split("\n").map((l) => l.trim()).filter(Boolean);
  assert.ok(lines.length >= 3, "输出内容过短");
  assert.ok(lines[0].startsWith("🌈 本次更新重点："), `首行必须以「🌈 本次更新重点：」开头：${lines[0]}`);
  const summaryBody = lines[0].slice("🌈 本次更新重点：".length).trim();
  assert.ok(summaryBody.length >= 4 && !TECH_WORDS.test(summaryBody), `更新重点不合格：${lines[0]}`);
  const out = [`🌈 本次更新重点：${/[。！？]$/.test(summaryBody) ? summaryBody : `${summaryBody}。`}`];
  let sectionIdx = -1;
  let count = 0;
  for (const line of lines.slice(1)) {
    const headerIdx = SECTION_HEADERS.indexOf(line.replace(/^#+\s*/, ""));
    if (headerIdx >= 0) {
      assert.ok(headerIdx > sectionIdx, `分类顺序或重复有误：${line}`);
      assert.ok(sectionIdx < 0 || count > 0, `「${SECTION_HEADERS[sectionIdx]}」下没有条目`);
      out.push("", SECTION_HEADERS[headerIdx]);
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
    out.push(`- ${/[。！？]$/.test(item) ? item : `${item}。`}`);
  }
  assert.ok(sectionIdx >= 0 && count > 0, "缺少有效更新条目");
  return out.join("\n");
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
  const inputLines = commits.map(({ subject, releaseNote = "" }) => {
    const clean = subject.replace(/\s*\(#\d+\)$/, "").trim();
    return releaseNote ? `${clean} | ${releaseNote}` : clean;
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
      const markdown = validateShipReleaseNotes(raw);
      if (!markdown) return { status: "empty", model: data.model || model };
      return { status: "polished", markdown, model: data.model || model };
    } catch (err) {
      lastError = `第 ${attempt} 次润色失败：${err?.message || err}`;
    }
  }
  return { status: "fallback", reason: lastError };
}

// [pattern, 中文, English]
const SUBJECT_RULES = [
  [/active selection and close actions on single tab/i, "只有一个标签页时隐藏关闭按钮与选中底色，顶栏更简洁", "With a single tab open, the close button and highlight are hidden for a cleaner tab bar"],
  [/global static assets folder/i, "支持在「设置 → 通用与编辑」中配置静态资源文件夹", "Choose a static assets folder in Settings → General & Editing"],
  [/single-line draft annotation/i, "修复单行批注草稿文字与右侧操作按钮未垂直居中的问题", "Fixed single-line annotation drafts not lining up with their action buttons"],
  [/settings header back button/i, "精简设置页顶部返回按钮与多余卡片，界面更清爽", "Cleaner Settings header with a simpler back button"],
  [/double text contrast|canvas CJK glyphs/i, "修复网页端中文文字笔画发粗、对比度异常的问题", "Fixed bold-looking Chinese text and odd contrast on the web version"],
  [/expand narrow-mode cards inline|badge popovers/i, "页边距充足时自动展开批注卡片，指向批注标记即可查看详情", "Annotation cards expand automatically when there is room; point at a marker to see details"],
  [/round-4 light\/dark variants|app icons/i, "换用全新浅色与深色模式应用图标", "New light and dark app icons"],
  [/selection ring.*side pill resize handles for images/i, "图片选中框与两侧缩放手柄更清晰，拖拽调整大小更顺手", "Clearer image selection frame and side handles make resizing smoother"],
  [/redesign annotation review cards and batch send/i, "支持批量发送多条批注审阅意见，卡片排版更清晰", "Send several annotation review comments at once, with clearer cards"],
  [/replace model label with filter input.*test button/i, "在「设置 → AI 助手」支持搜索筛选模型，连接测试成功后显示明确提示", "Search models in Settings → AI Assistant, with a clear message when the connection test succeeds"],
  [/stabilize sidebar tab geometry/i, "修复切换设置页左侧栏目时位置轻微跳动的问题", "Fixed a slight jump when switching sections in the Settings sidebar"],
  [/sync IME cursor geometry|guard IME composition/i, "修复中文输入法候选框位置偏移与选词被打断的问题", "Fixed misplaced input-method candidate window and interrupted word selection"],
  [/image container and selection box to rendered image/i, "修复笔记内图片选中框与实际显示大小不一致的问题", "Fixed image selection frames not matching the displayed image size"],
  [/GitHub Ice dark syntax and card palette for code blocks/i, "代码块换用深色护眼配色，语法高亮更易读", "Code blocks use an eye-friendly dark palette with easier-to-read highlighting"],
  [/load and render remote http\/https images/i, "支持直接显示笔记中的网络图片并自动缓存到本地", "Web images in notes now display directly and are cached locally"],
  [/top hover insertion slot.*structural first block/i, "首段为表格或代码块时，支持在顶部插入新段落或按方向键跳出", "When a note starts with a table or code block, add a paragraph above it or press the arrow key to step out"],
  [/remove image export from tab context menu/i, "精简标签页右键菜单，移除重复的导出图片入口", "Tab right-click menu no longer repeats the export-image entry"],
  [/vertically center key icons.*shortcut_label/i, "修复快捷键提示中的按键图标与文字未对齐的问题", "Fixed key icons not aligning with text in shortcut hints"],
  [/full model tag list and disallow manual model/i, "AI 设置直接展示完整可用模型列表，点选更直观", "AI settings show the full list of available models to pick from"],
  [/todo checkbox colors/i, "优化待办事项勾选框配色，完成状态更醒目", "To-do checkboxes have better colors and completed items stand out"],
  [/cross-cell text selection/i, "修复表格跨单元格划选文字时误选中相邻格子的问题", "Fixed selecting text across table cells also selecting neighboring cells"],
  [/remove insert row\/column items from the table menu/i, "精简表格菜单，行列增删统一由边栏按钮完成", "Simpler table menu; add or remove rows and columns with the edge buttons"],
  [/optically balance key icons/i, "统一快捷键按键图标视觉比例", "Key icons in shortcut hints have a consistent size"],
  [/drag a column boundary to resize.*double-click to reset/i, "支持拖动表格列边线调整列宽，双击即可恢复默认宽度", "Drag a table column edge to resize it; double-click to reset the width"],
  [/keep column drag alive when the pointer leaves/i, "修复拖动表格列宽时鼠标移出编辑区导致拖拽中断的问题", "Fixed column resizing stopping when the pointer leaves the editor"],
  [/Fit to page width toggle/i, "表格菜单新增「适应页面宽度」开关", "New “Fit to page width” switch in the table menu"],
  [/auto-scroll while dragging rows\/columns/i, "拖拽表格行列到边缘时支持自动滚动，落点更稳定", "Dragging table rows or columns to the edge now scrolls automatically"],
  [/drag the add-column bar to grow or shrink columns/i, "支持按住表格右侧加号条连续增减列数", "Drag the plus bar on a table's right edge to add or remove columns continuously"],
  // 近期提交
  [/persistent editor content zoom|editor content zoom/i, "编辑器的缩放比例会被记住，下次打开保持不变；速记窗口也能正常唤起主窗口", "The editor remembers your zoom level, and Quick Note now reliably brings up the main window"],
  [/Remove document icons from title bar tabs/i, "顶栏标签不再显示文档图标，标题更清爽", "Title bar tabs no longer show document icons, for a cleaner look"],
  [/Sync app theme automatically when system appearance/i, "应用会随系统深浅色外观自动切换", "The app now follows the system light/dark appearance automatically"],
  [/entity reentrancy crash on theme appearance/i, "修复切换深浅色外观时可能闪退的问题", "Fixed a possible crash when switching between light and dark appearance"],
  [/Keep text selection within inline code chip height/i, "行内代码的选中高亮不再超出代码块高度", "Selection highlight on inline code now stays within the code's height"],
  [/Fix reopening closed tabs with platform shortcuts/i, "修复用系统快捷键重新打开已关闭标签页无效的问题", "Fixed reopening closed tabs with the system shortcut"],
  [/Inline code: outline chip style/i, "行内代码改为描边样式，点击可复制，光标在两端也能顺畅移出", "Inline code has a new outlined look, shows a copy tip, and the cursor moves out smoothly at either end"],
  [/immediate inline code copy confirmation/i, "复制行内代码后立即显示已复制提示", "Copying inline code now shows instant confirmation"],
  [/hide.*cursor.*inactive window|inactive window.*cursor/i, "窗口不在前台时隐藏光标，避免误以为仍在输入", "The cursor is hidden while the window is inactive"],
  [/first block.*insert|top insert/i, "首个内容块上方可直接插入新段落", "Insert a new paragraph above the first block"],
];

function stripSubject(subject, releaseNote) {
  const source = (releaseNote || subject).replace(/\s*\(#\d+\)$/, "").trim();
  return { source, body: source.replace(/^[a-z]+(\([^)]*\))?!?:\s*/i, "").trim() };
}

function classify(subject) {
  const m = /^([a-z]+)(?:\([^)]*\))?!?:\s*/i.exec(subject);
  if (m) return m[1].toLowerCase() === "fix" ? "fix" : "feat";
  return /^(fix|resolve|correct|prevent|avoid|keep)\b/i.test(subject) ? "fix" : "feat";
}

// 返回 { zh, en, kind } 或 null（无法生成有意义条目时丢弃，不再编造套话）
function translateSubject(commit) {
  const { source, body } = stripSubject(commit.subject, commit.releaseNote);
  const kind = classify(commit.subject);
  const dot = (t, end) => (new RegExp(`[${end}]$`).test(t) ? t : `${t}${end[0]}`);
  for (const [pattern, zh, en] of SUBJECT_RULES) {
    if (pattern.test(source)) return { kind, zh: dot(zh, "。！？"), en: dot(en, ".!?") };
  }
  if (/[一-龥]/.test(body)) {
    const cleaned = body.replace(TECH_WORDS, "").replace(/\s+/g, " ").trim();
    if (cleaned.length >= 4) return { kind, zh: dot(cleaned, "。！？"), en: dot(cleaned, ".!?") };
  }
  return null;
}

export function groupCommitsByDay(commits) {
  const map = new Map();
  for (const c of commits) {
    const date = c.date || "unknown";
    if (!map.has(date)) map.set(date, []);
    map.get(date).push(c);
  }
  return [...map.entries()]
    .sort((a, b) => (a[0] < b[0] ? 1 : a[0] > b[0] ? -1 : 0))
    .map(([date, list]) => ({ date, commits: list }));
}

function renderBlock(lang, highlights, features, fixes) {
  const zh = lang === "zh";
  const out = [zh ? `🌈 本次更新重点：${highlights}` : `🌈 Highlights: ${highlights}`];
  if (features.length) out.push("", zh ? "✨ 新功能" : "✨ New", ...features.map((i) => `- ${i}`));
  if (fixes.length) out.push("", zh ? "🐛 修复" : "🐛 Fixes", ...fixes.map((i) => `- ${i}`));
  return out.join("\n");
}

function buildDayBlock(commits) {
  const buckets = { feat: { zh: [], en: [] }, fix: { zh: [], en: [] } };
  for (const commit of commits.filter((c) => !isExcludedCommit(c))) {
    const t = translateSubject(commit);
    if (!t) continue;
    const b = buckets[t.kind];
    if (b.zh.includes(t.zh) || b.zh.length >= MAX_PER_SECTION) continue;
    b.zh.push(t.zh);
    b.en.push(t.en);
  }
  const all = { zh: [...buckets.feat.zh, ...buckets.fix.zh], en: [...buckets.feat.en, ...buckets.fix.en] };
  if (!all.zh.length) return null;
  const top = (arr, sep, end) => `${arr.slice(0, 2).map((s) => s.replace(/[。！？.!?]$/, "")).join(sep)}${end}`;
  const hz = top(all.zh, "；", "。");
  const he = top(all.en, "; ", ".");
  const side = (lang, h) => {
    const feats = buckets.feat[lang];
    const fixes = buckets.fix[lang];
    return { highlights: h, features: feats, fixes, markdown: renderBlock(lang, h, feats, fixes) };
  };
  return { zh: side("zh", hz), en: side("en", he) };
}

export function buildDays(commits) {
  const days = [];
  for (const { date, commits: list } of groupCommitsByDay(commits)) {
    const block = buildDayBlock(list);
    if (block) days.push({ date, ...block });
  }
  return days;
}

function composeMarkdown(days, lang, byDay) {
  const one = (l) => {
    if (!days.length) return l === "zh" ? "🌈 本次更新重点：本次无面向用户的可见更新。" : "🌈 Highlights: No user-facing changes in this release.";
    if (!byDay || days.length === 1) {
      if (days.length === 1) return days[0][l].markdown;
      return "";
    }
    return days.map((d) => `📅 ${d.date}\n\n${d[l].markdown}`).join("\n\n");
  };
  const merged = (l) => {
    if (byDay || days.length <= 1) return one(l);
    // 不按天：合并为单块
    const feats = [...new Set(days.flatMap((d) => d[l].features))].slice(0, MAX_PER_SECTION);
    const fixes = [...new Set(days.flatMap((d) => d[l].fixes))].slice(0, MAX_PER_SECTION);
    const hl = days.slice(0, 1)[0][l].highlights;
    return renderBlock(l, hl, feats, fixes);
  };
  const zh = merged("zh");
  const en = merged("en");
  return { zh, en, text: lang === "zh" ? zh : lang === "en" ? en : `${zh}\n\n---\n\n${en}` };
}

export function fallbackReleaseNotes(commits, { lang = "bilingual", byDay = true } = {}) {
  return composeMarkdown(buildDays(commits), lang, byDay).text;
}

export async function buildReleaseNotes({ version, commits, lang = "bilingual", byDay = true, env = process.env }) {
  const userCommits = commits.filter((c) => !isExcludedCommit(c));
  if (userCommits.length === 0) return { skip: true, reason: "没有面向用户的新提交", markdown: "", markdownZh: "", markdownEn: "", days: [] };
  const days = buildDays(userCommits);
  const composed = composeMarkdown(days, lang, byDay);
  const base = { days, markdownZh: composed.zh, markdownEn: composed.en };
  // 多天或单语/按天输出以规则结果为准；仅单天双语时尝试模型润色
  const polished = days.length <= 1 && lang === "bilingual"
    ? await polishReleaseNotes({ version, commits: userCommits, env })
    : { status: "fallback", reason: days.length > 1 ? "跨多天按天展示，使用规则归并" : "单语输出使用规则归并" };
  if (polished.status === "polished") {
    const [zh, en] = polished.markdown.split(/\n\n---\n\n/);
    return { skip: false, source: "polished", model: polished.model, ...base, markdownZh: zh, markdownEn: en || composed.en, markdown: polished.markdown };
  }
  if (polished.status === "empty") {
    return { skip: true, reason: `${polished.model} 判断无用户可感知改动`, markdown: "", markdownZh: "", markdownEn: "", days: [] };
  }
  return { skip: false, source: "fallback", reason: polished.reason, ...base, markdown: composed.text };
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
