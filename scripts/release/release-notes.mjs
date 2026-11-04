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
  const args = ["log", "--no-merges", "--reverse", "--format=%H%x00%an%x00%s%x00%(trailers:key=Release-Note,valueonly,separator=%x20)"];
  if (rangeSpec.startsWith("-n")) args.push(...rangeSpec.split(/\s+/));
  else args.push(rangeSpec);
  const out = execFileSync("git", args, { cwd: root, encoding: "utf8" }).trimEnd();
  if (!out) return [];
  return out.split("\n").filter(Boolean).map((line) => {
    const [sha, author, subject, releaseNote = ""] = line.split("\x00");
    const paths = execFileSync("git", ["diff-tree", "--no-commit-id", "--name-only", "-r", sha], { cwd: root, encoding: "utf8" })
      .trimEnd().split("\n").filter(Boolean);
    return { sha, author, subject, releaseNote: releaseNote.trim(), paths };
  });
}

function stripFence(text) {
  return text.replace(/\r\n?/g, "\n").trim().replace(/^```(?:markdown|md)?\n([\s\S]*?)\n```$/i, "$1").trim();
}

export function validateShipReleaseNotes(raw) {
  const text = stripFence(raw);
  if (!text || /^[（(]?无[)）]?[。.]?$/.test(text)) return "";
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
            { role: "user", content: `Yu ${version} 的提交记录（共 ${inputLines.length} 条）：\n\n${inputLines.join("\n")}` },
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

const SUBJECT_RULES = [
  [/active selection and close actions on single tab/i, "只有一个标签页时隐藏关闭按钮与选中底色，顶栏更简洁"],
  [/global static assets folder/i, "支持在「设置 → 通用与编辑」中配置静态资源文件夹"],
  [/single-line draft annotation/i, "修复单行批注草稿文字与右侧操作按钮未垂直居中的问题"],
  [/settings header back button/i, "精简设置页顶部返回按钮与多余卡片，界面更清爽"],
  [/double text contrast|canvas CJK glyphs/i, "修复网页端中文文字笔画发粗、对比度异常的问题"],
  [/expand narrow-mode cards inline|badge popovers/i, "页边距充足时自动展开批注卡片，指向批注标记即可查看详情"],
  [/round-4 light\/dark variants|app icons/i, "换用全新浅色与深色模式应用图标"],
  [/selection ring.*side pill resize handles for images/i, "图片选中框与两侧缩放手柄更清晰，拖拽调整大小更顺手"],
  [/redesign annotation review cards and batch send/i, "支持批量发送多条批注审阅意见，卡片排版更清晰"],
  [/replace model label with filter input.*test button/i, "在「设置 → AI 助手」支持搜索筛选模型，连接测试成功后显示明确提示"],
  [/stabilize sidebar tab geometry/i, "修复切换设置页左侧栏目时位置轻微跳动的问题"],
  [/sync IME cursor geometry|guard IME composition/i, "修复中文输入法候选框位置偏移与选词被打断的问题"],
  [/image container and selection box to rendered image/i, "修复笔记内图片选中框与实际显示大小不一致的问题"],
  [/GitHub Ice dark syntax and card palette for code blocks/i, "代码块换用深色护眼配色，语法高亮更易读"],
  [/load and render remote http\/https images/i, "支持直接显示笔记中的网络图片并自动缓存到本地"],
  [/top hover insertion slot.*structural first block/i, "首段为表格或代码块时，支持在顶部插入新段落或按方向键跳出"],
  [/remove image export from tab context menu/i, "精简标签页右键菜单，移除重复的导出图片入口"],
  [/vertically center key icons.*shortcut_label/i, "修复快捷键提示中的按键图标与文字未对齐的问题"],
  [/full model tag list and disallow manual model/i, "AI 设置直接展示完整可用模型列表，点选更直观"],
  [/todo checkbox colors/i, "优化待办事项勾选框配色，完成状态更醒目"],
  [/cross-cell text selection/i, "修复表格跨单元格划选文字时误选中相邻格子的问题"],
  [/remove insert row\/column items from the table menu/i, "精简表格菜单，行列增删统一由边栏按钮完成"],
  [/optically balance key icons/i, "统一快捷键按键图标视觉比例"],
  [/drag a column boundary to resize.*double-click to reset/i, "支持拖动表格列边线调整列宽，双击即可恢复默认宽度"],
  [/keep column drag alive when the pointer leaves/i, "修复拖动表格列宽时鼠标移出编辑区导致拖拽中断的问题"],
  [/Fit to page width toggle/i, "表格菜单新增「适应页面宽度」开关"],
  [/auto-scroll while dragging rows\/columns/i, "拖拽表格行列到边缘时支持自动滚动，落点更稳定"],
  [/drag the add-column bar to grow or shrink columns/i, "支持按住表格右侧加号条连续增减列数"],
];

function translateSubject(subject, releaseNote, kind) {
  const source = (releaseNote || subject).replace(/\s*\(#\d+\)$/, "").trim();
  const body = source.replace(/^[a-z]+(\([^)]*\))?!?:\s*/i, "").trim();
  for (const [pattern, text] of SUBJECT_RULES) {
    if (pattern.test(source)) return text.endsWith("。") ? text : `${text}。`;
  }
  if (/[一-龥]/.test(body)) {
    const cleaned = body.replace(TECH_WORDS, "").replace(/\s+/g, " ").trim();
    if (cleaned.length >= 4) return /[。！？]$/.test(cleaned) ? cleaned : `${cleaned}。`;
  }
  const scope = /^[a-z]+\(([^)]+)\)/i.exec(source)?.[1]?.toLowerCase() || "";
  const area = {
    editor: "编辑器", table: "表格", annotate: "批注", settings: "设置页",
    "ai-settings": "AI 设置", ai: "AI 助手", web: "网页端", tabs: "标签页",
    search: "搜索", export: "导出图片", quick: "速记窗口", ui: "界面",
  }[scope] || "笔记编辑";
  if (kind === "feat") return `新增${area}相关实用功能，日常记录更顺手。`;
  if (kind === "fix") return `修复${area}的一处交互细节问题，使用更稳定。`;
  return `优化${area}的显示与操作体验。`;
}

export function fallbackReleaseNotes(commits) {
  const userCommits = commits.filter((c) => !isExcludedCommit(c));
  const buckets = { feat: [], opt: [], fix: [] };
  for (const commit of userCommits) {
    const m = /^([a-z]+)(?:\([^)]*\))?!?:\s*/i.exec(commit.subject);
    const prefix = m ? m[1].toLowerCase() : "opt";
    const kind = prefix === "feat" ? "feat" : prefix === "fix" ? "fix" : "opt";
    const line = translateSubject(commit.subject, commit.releaseNote, kind);
    if (!buckets[kind].includes(line) && buckets[kind].length < MAX_PER_SECTION) {
      buckets[kind].push(line);
    }
  }
  const all = [...buckets.feat, ...buckets.opt, ...buckets.fix];
  if (all.length === 0) return "🌈 本次更新重点：本次无面向用户的可见更新。";
  const highlights = all.slice(0, 2).map((s) => s.replace(/[。！？]$/, ""));
  const out = [`🌈 本次更新重点：${highlights.join("；")}。`];
  for (const [key, header] of [["feat", "✨ 新功能"], ["opt", "🪐 优化"], ["fix", "🐛 修复"]]) {
    if (buckets[key].length > 0) {
      out.push("", header, ...buckets[key].map((item) => `- ${item}`));
    }
  }
  return out.join("\n");
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
  return {
    skip: false,
    source: "fallback",
    reason: polished.reason,
    markdown: fallbackReleaseNotes(userCommits),
  };
}

if (import.meta.main) {
  const sinceIdx = process.argv.indexOf("--since");
  const limitIdx = process.argv.indexOf("--limit");
  const range = sinceIdx >= 0 && process.argv[sinceIdx + 1]
    ? `${process.argv[sinceIdx + 1]}..HEAD`
    : `-n ${limitIdx >= 0 && process.argv[limitIdx + 1] ? process.argv[limitIdx + 1] : "12"}`;
  const commits = listCommits(ROOT, range);
  const result = await buildReleaseNotes({ version: "0.1.0", commits });
  console.log(result.markdown || `跳过：${result.reason}`);
}
