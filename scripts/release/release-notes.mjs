import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  EN_SECTIONS,
  ZH_SECTIONS,
  lineMixesLanguages,
  validateReleaseNotes,
} from "./release-notes-check.mjs";

export { lineMixesLanguages, validateReleaseNotes };

const MAGPIE_ORIGIN = "http://100.115.90.80:3425";
// 用户 Mac 上的 Magpie（Surge 内置 Tailscale 节点 surge-mac），只在在线时用。
const MAC_MAGPIE_CHAT_URL = "http://100.118.27.43:3425/v1/chat/completions";

// 按顺序尝试，前一个失败才换下一个：
// Mac Magpie agy-cli/gemini-3.8-flash（用户 agy 账号，Mac 离线就跳过）→ VPS Magpie gpt-6-luna（用户 codex 账号）→ VPS Magpie deepseek-flash（付费）。
// 不要用 Google AI Studio 的 Gemini key，用户的 key 不可用。
export const CHANGELOG_PROVIDERS = [
  { model: "agy-cli/gemini-3.8-flash", via: "mac" },
  { model: "gpt-6-luna", via: "magpie" },
  { model: "deepseek-flash", via: "magpie" },
];

const DIR = fileURLToPath(new URL(".", import.meta.url));
const ROOT = resolve(DIR, "../..");
const PROMPT_PATH = resolve(DIR, "changelog-prompt.md");
const MAX_ATTEMPTS = 2;
// 已观察到上游 502 约 30 秒才返回。超时短于这个时间会被记成连接失败，掩盖真实状态码。
const TIMEOUT_MS = 90_000;
const BATCH_THRESHOLD = 50;
const BATCH_SIZE = 40;
const MAX_POLISH_MS = 8 * 60 * 1000;
const BOT_AUTHORS = new Set(["github-actions[bot]", "github-actions"]);
const VERSION_BUMP = /^chore\(release\):\s*v\d+\.\d+\.\d+\s*$/i;
const INTERNAL_COMMIT = /^(?:chore|ci|docs|test)(?:\([^)]*\))?!?:\s|^Update README\.md$|^(?:feat|fix)\((?:src|assets|scripts|deps|designs)\):\s*(?:add|update|remove)\b/i;
const NON_SHIPPABLE_EXACT = new Set([
  "updates/feed.json",
  ".ai-map.md",
  "README.md",
  "README.en.md",
  "AGENTS.md",
  "DESIGN.md",
  ".github/pull_request_template.md",
  ".github/workflows/ci.yml",
  ".github/workflows/prune-release-assets.yml",
  "scripts/lint_design.py",
  "scripts/gen_ai_map.py",
  "scripts/dump_ai_state.py",
  "scripts/fast-check.sh",
  "scripts/build_readme_svgs.py",
]);

function isNonShippablePath(filePath) {
  const p = String(filePath || "").replaceAll("\\", "/").replace(/^\.\//, "");
  if (!p) return true;
  if (NON_SHIPPABLE_EXACT.has(p)) return true;
  if (p.startsWith("docs/")) return true;
  if (p.endsWith("_test.go")) return true;
  return false;
}

export function isExcludedCommit({ author = "", subject = "", releaseNote = "", releaseNoteEn = "", paths = [] }) {
  if (BOT_AUTHORS.has(author)) return true;
  if (/^none$/i.test(releaseNote.trim()) || /^none$/i.test(releaseNoteEn.trim())) return true;
  if (VERSION_BUMP.test(subject)) return true;
  const hasExplicitNote = Boolean(releaseNote.trim() || releaseNoteEn.trim());
  if (!hasExplicitNote && INTERNAL_COMMIT.test(subject.trim())) return true;
  if (!hasExplicitNote && Array.isArray(paths) && (paths.length === 0 || paths.every(isNonShippablePath))) return true;
  return false;
}

function splitNote(raw = "") {
  const text = raw.trim();
  if (!text || !text.includes("|||")) return { zh: text, en: "" };
  const [zh, ...rest] = text.split("|||");
  return { zh: zh.trim(), en: rest.join("|||").trim() };
}

export function listCommits(root = ROOT, rangeSpec = "-n 20") {
  const COMMIT_MARKER = "<<<BLOCK_COMMIT>>>";
  const args = [
    "-c",
    "core.quotepath=false",
    "log",
    "--no-merges",
    "--topo-order",
    "--reverse",
    "--date=format-local:%Y-%m-%d",
    "--name-only",
    "-z",
    `--format=${COMMIT_MARKER}%x00%H%x00%an%x00%cd%x00%s%x00%(trailers:key=Release-Note,valueonly,separator=%x20)%x00%(trailers:key=Release-Note-En,valueonly,separator=%x20)%x00%b%x00`,
  ];
  if (rangeSpec.startsWith("-n")) args.push(...rangeSpec.split(/\s+/));
  else args.push(rangeSpec);
  const out = execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    env: { ...process.env, TZ: "Asia/Shanghai" },
  });
  if (!out) return [];
  const tokens = out.split("\x00");
  const commits = [];
  let idx = 0;
  while (idx < tokens.length) {
    const token = tokens[idx].replace(/^\n+/, "");
    if (token !== COMMIT_MARKER) {
      idx += 1;
      continue;
    }
    const sha = (tokens[idx + 1] || "").trim();
    const author = tokens[idx + 2] || "";
    const date = tokens[idx + 3] || "";
    const subject = tokens[idx + 4] || "";
    const releaseNoteRaw = tokens[idx + 5] || "";
    const releaseNoteEnRaw = tokens[idx + 6] || "";
    const body = cleanBody(tokens[idx + 7] || "");
    idx += 8;
    const paths = [];
    while (idx < tokens.length) {
      const next = tokens[idx].replace(/^\n+/, "");
      if (next === COMMIT_MARKER) break;
      const trimmed = next.trim();
      if (trimmed) paths.push(trimmed);
      idx += 1;
    }
    if (!/^[0-9a-f]{40}$/i.test(sha)) continue;
    const split = splitNote(releaseNoteRaw);
    commits.push({
      sha,
      author,
      date,
      subject,
      releaseNote: split.zh,
      releaseNoteEn: (releaseNoteEnRaw || "").trim() || split.en,
      body,
      paths,
    });
  }
  return commits;
}

const TRAILER_LINE = /^(?:Release-Note(?:-En)?|Co-authored-by|Signed-off-by|Change-Id):/i;
const MAX_BODY = 600;
const MAX_PATHS = 8;

// 提交正文去掉 trailer 和空行，压成一行，给模型判断用户能感受到什么。
function cleanBody(raw) {
  const text = String(raw)
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !TRAILER_LINE.test(line))
    .join(" ");
  return text.length > MAX_BODY ? `${text.slice(0, MAX_BODY)}…` : text;
}

function shortToken(value) {
  if (typeof value !== "string") return "";
  const token = value.trim();
  return /^[A-Za-z0-9_.:-]{1,40}$/.test(token) ? token : "";
}

function errorToken(body) {
  const data = Array.isArray(body) ? body[0] : body;
  if (!data || typeof data !== "object") return "";
  const error = data.error;
  if (typeof error === "string") return shortToken(error);
  if (error && typeof error === "object") {
    return shortToken(error.status) || shortToken(error.type) || shortToken(error.code) || shortToken(error.kind);
  }
  return shortToken(data.type) || shortToken(data.code);
}

export function publicFailure(err, model = "模型") {
  if (typeof err?.publicReason === "string" && err.publicReason) return `${model}：${err.publicReason}`;
  const name = err?.name || "";
  const msg = String(err?.message || "");
  if (name === "TimeoutError" || /The operation was aborted|timeout/i.test(msg)) return `${model}：请求超时`;
  if (name === "AbortError") return `${model}：请求已中止`;
  const http = /^HTTP (\d{3})(?: ([A-Za-z0-9_.:-]{1,40}))?$/.exec(msg);
  if (http) return `${model}：HTTP ${http[1]}${http[2] ? `（${http[2]}）` : ""}`;
  if (/redirect/i.test(msg)) return `${model}：拒绝跳转到其他地址`;
  if (/fetch failed|ECONN|ENOTFOUND|EHOSTUNREACH|network/i.test(msg)) return `${model}：无法连接`;
  return `${model}：没有生成更新说明`;
}

function httpFailure(status, body) {
  const token = errorToken(body);
  return new Error(token ? `HTTP ${status} ${token}` : `HTTP ${status}`);
}

export function resolveMagpieChatUrl(baseUrl) {
  const raw = String(baseUrl || "").trim();
  if (!raw) return { ok: false, reason: "未配置 MAGPIE_BASE_URL" };
  let url;
  try {
    url = new URL(raw);
  } catch {
    return { ok: false, reason: "MAGPIE_BASE_URL 不是合法 URL" };
  }
  if (url.username || url.password || url.search || url.hash) {
    return { ok: false, reason: "MAGPIE_BASE_URL 不能带凭据、查询或片段" };
  }
  if (url.protocol !== "http:" || url.hostname !== "100.115.90.80" || url.port !== "3425") {
    return { ok: false, reason: "MAGPIE_BASE_URL 只能是 http://100.115.90.80:3425" };
  }
  const path = url.pathname.replace(/\/+$/, "");
  if (path !== "" && path !== "/v1") return { ok: false, reason: "MAGPIE_BASE_URL 路径只能是空或 /v1" };
  return { ok: true, url: `${MAGPIE_ORIGIN}/v1/chat/completions` };
}

// 把配置好的模型按顺序排出来；没配好的写明原因，不调用。
export function resolveProviders(env) {
  const ready = [];
  const skipped = [];
  for (const { model, via } of CHANGELOG_PROVIDERS) {
    if (via === "mac") {
      if (env.MAC_MAGPIE_REACHABLE === "no") {
        skipped.push(`${model}：Mac 不在线`);
        continue;
      }
      ready.push({ model, url: MAC_MAGPIE_CHAT_URL, headers: { "content-type": "application/json" }, timeoutMs: 120_000 });
      continue;
    }
    if (env.MAGPIE_REACHABLE === "no") {
      skipped.push(`${model}：CI 未连通 Magpie`);
      continue;
    }
    const endpoint = resolveMagpieChatUrl(env.MAGPIE_BASE_URL);
    if (!endpoint.ok) {
      skipped.push(`${model}：${endpoint.reason}`);
      continue;
    }
    ready.push({ model, url: endpoint.url, headers: { "content-type": "application/json" } });
  }
  return { ready, skipped };
}

function messageText(content) {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content.map((part) => {
    if (typeof part === "string") return part;
    if (part && typeof part.text === "string") return part.text;
    return "";
  }).join("");
}

function baseModel(name) {
  return String(name || "").trim().toLowerCase().split("@")[0].split("/").pop();
}

export function readCompletion(requested, data) {
  const used = typeof data?.model === "string" && data.model.trim() ? data.model.trim() : requested;
  if (baseModel(used) !== baseModel(requested)) {
    const err = new Error("响应模型不符");
    err.publicReason = `响应模型是 ${shortToken(used) || "未知"}，未采用`;
    throw err;
  }
  const raw = messageText(data?.choices?.[0]?.message?.content);
  if (!raw.trim()) {
    const err = new Error("模型返回为空");
    err.publicReason = "返回内容为空";
    err.retry = true;
    throw err;
  }
  return { model: baseModel(used), raw };
}

async function readJsonBody(res) {
  const text = await res.text();
  if (!text.trim()) return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

function commitLines(commits) {
  return commits.map(({ subject, releaseNote = "", releaseNoteEn = "", body = "", paths = [] }, i) => {
    const clean = subject.replace(/\s*\(#\d+\)$/, "").trim();
    let head = clean;
    if (releaseNote && releaseNoteEn) head = `${clean} | ${releaseNote} ||| ${releaseNoteEn}`;
    else if (releaseNote) head = `${clean} | ${releaseNote}`;
    else if (releaseNoteEn) head = `${clean} ||| ${releaseNoteEn}`;
    const lines = [`${i + 1}. ${head}`];
    if (body) lines.push(`   body: ${body}`);
    const shown = paths.filter((p) => !isNonShippablePath(p)).slice(0, MAX_PATHS);
    if (shown.length) lines.push(`   files: ${shown.join(", ")}${paths.length > shown.length ? ", …" : ""}`);
    return lines.join("\n");
  });
}

async function polishBatch({ version, commits, provider, system }) {
  const { model, url, headers, timeoutMs = TIMEOUT_MS } = provider;
  const inputLines = commitLines(commits);
  if (!inputLines.length) return { status: "empty", model };
  const asked = `These are the only commits for Block ${version} since the previous tag (${inputLines.length}):\n\n${inputLines.join("\n\n")}`;
  let lastRaw = "";
  let failure = `${model}：没有生成更新说明`;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    const messages = [{ role: "system", content: system }, { role: "user", content: asked }];
    if (attempt > 1 && lastRaw) {
      messages.push({ role: "assistant", content: lastRaw });
      messages.push({
        role: "user",
        content: "Proofread and rewrite. The previous draft failed release-note validation. Keep one language per line, one bullet per commit, start every bullet with its feature area (English \"Area: ...\", Chinese \"功能位置：...\"), English above ---, Simplified Chinese below. Output markdown only.",
      });
    }
    try {
      const res = await fetch(url, {
        method: "POST",
        headers,
        redirect: "error",
        signal: AbortSignal.timeout(timeoutMs),
        body: JSON.stringify({ model, temperature: 0.2, messages }),
      });
      const data = await readJsonBody(res);
      if (!res.ok) throw httpFailure(res.status, data);
      if (!data) {
        const err = new Error("响应不是 JSON");
        err.publicReason = "响应不是 JSON";
        throw err;
      }
      const completion = readCompletion(model, data);
      lastRaw = completion.raw;
      let markdown = "";
      try {
        markdown = validateReleaseNotes(completion.raw);
      } catch {
        markdown = "";
      }
      if (!markdown) {
        const err = new Error("生成结果未通过更新说明校验");
        err.publicReason = "生成结果未通过更新说明校验";
        err.retry = true;
        throw err;
      }
      const listed = listedItems(markdown);
      if (listed < commits.length) {
        const err = new Error(`notes list ${listed} items for ${commits.length} commits`);
        err.publicReason = `条目数 ${listed}/${commits.length}`;
        err.retry = true;
        throw err;
      }
      return { status: "polished", markdown, model: completion.model };
    } catch (err) {
      failure = publicFailure(err, model);
      if (!err?.retry) break;
    }
  }
  return { status: "fallback", reason: failure };
}

function collectHalfBuckets(half, sections, buckets, lang) {
  let index = -1;
  for (const rawLine of half.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const section = sections.indexOf(line);
    if (section >= 0) {
      index = section;
      continue;
    }
    if (line === "### Download" || line === "### 下载" || line.startsWith("Website:") || line.startsWith("官网：")) {
      break;
    }
    const bullet = /^[-*•]\s+(.+)$/.exec(line);
    if (bullet && index >= 0) {
      buckets[index][lang].push(bullet[1].trim());
    }
  }
}

function mergePolishedBatches(markdowns) {
  const buckets = [{ zh: [], en: [] }, { zh: [], en: [] }, { zh: [], en: [] }, { zh: [], en: [] }];
  for (const markdown of markdowns) {
    const [enHalf = "", zhHalf = ""] = markdown.split(/\n\s*---\s*\n/);
    collectHalfBuckets(enHalf, EN_SECTIONS, buckets, "en");
    collectHalfBuckets(zhHalf, ZH_SECTIONS, buckets, "zh");
  }
  const en = renderHalf("## English", EN_SECTIONS, buckets, "en");
  const zh = renderHalf("## 中文", ZH_SECTIONS, buckets, "zh");
  if (!en || !zh) return "";
  return validateReleaseNotes(`${en}\n\n---\n\n${zh}`);
}

async function polishWith({ version, commits, provider, system, deadline }) {
  if (commits.length <= BATCH_THRESHOLD) {
    return polishBatch({ version, commits, provider, system });
  }
  const batchMarkdowns = [];
  for (let offset = 0; offset < commits.length; offset += BATCH_SIZE) {
    if (Date.now() >= deadline) return { status: "fallback", reason: `${provider.model}：多批生成超出时间预算` };
    const batch = commits.slice(offset, offset + BATCH_SIZE);
    const result = await polishBatch({ version, commits: batch, provider, system });
    if (result.status !== "polished") {
      return result.status === "fallback"
        ? result
        : { status: "fallback", reason: `${provider.model}：第 ${offset / BATCH_SIZE + 1} 批没有生成结果` };
    }
    batchMarkdowns.push(result.markdown);
  }
  try {
    const markdown = mergePolishedBatches(batchMarkdowns);
    if (!markdown) return { status: "fallback", reason: `${provider.model}：生成结果未通过更新说明校验` };
    const listed = listedItems(markdown);
    if (listed < commits.length) return { status: "fallback", reason: `${provider.model}：条目数 ${listed}/${commits.length}` };
    return { status: "polished", markdown, model: provider.model };
  } catch {
    return { status: "fallback", reason: `${provider.model}：生成结果未通过更新说明校验` };
  }
}

export async function polishReleaseNotes({ version, commits, env = process.env }) {
  if (!commits.length) return { status: "empty" };
  const { ready, skipped } = resolveProviders(env);
  const reasons = [...skipped];
  const system = readFileSync(PROMPT_PATH, "utf8");
  const deadline = Date.now() + MAX_POLISH_MS;
  for (const provider of ready) {
    if (Date.now() >= deadline) {
      reasons.push(`${provider.model}：总时间预算已用完，未调用`);
      continue;
    }
    const result = await polishWith({ version, commits, provider, system, deadline });
    if (result.status === "polished" && result.markdown) return { ...result, attempts: reasons };
    reasons.push(result.reason || `${provider.model}：没有生成更新说明`);
  }
  return { status: "fallback", reason: reasons.join("；") || "没有可用的模型" };
}

function listedItems(markdown) {
  const english = markdown.split(/\n\s*---\s*\n/)[0] || "";
  return english.split("\n").filter((line) => /^[-*•]\s+\S/.test(line)).length;
}

function renderHalf(title, sections, buckets, lang) {
  const lines = [title, ""];
  let any = false;
  for (let i = 0; i < sections.length; i += 1) {
    const items = buckets[i][lang];
    if (!items.length) continue;
    if (any) lines.push("");
    lines.push(sections[i], ...items.map((item) => `- ${item}`));
    any = true;
  }
  return any ? lines.join("\n") : "";
}

export async function buildReleaseNotes({ version, commits, env = process.env }) {
  const userCommits = commits.filter((commit) => !isExcludedCommit(commit));
  if (userCommits.length === 0) {
    return { skip: true, reason: "没有面向用户的新提交", markdown: "" };
  }
  const polished = await polishReleaseNotes({ version, commits: userCommits, env });
  if (polished.status === "polished" && polished.model) {
    return { skip: false, source: "polished", model: polished.model, generationFailed: false, markdown: polished.markdown };
  }
  // 所有模型都失败时不再拼凑文案，交给调用方停止发版。
  return { skip: false, source: "none", generationFailed: true, reason: polished.reason || "没有可用的模型", markdown: "" };
}

if (import.meta.main) {
  const sinceIdx = process.argv.indexOf("--since");
  const limitIdx = process.argv.indexOf("--limit");
  const range = sinceIdx >= 0 && process.argv[sinceIdx + 1]
    ? `${process.argv[sinceIdx + 1]}..HEAD`
    : `-n ${limitIdx >= 0 && process.argv[limitIdx + 1] ? process.argv[limitIdx + 1] : "12"}`;
  const commits = listCommits(ROOT, range);
  const { listGitTagVersions, nextReleaseVersion, readPackageVersion } = await import("./select-release.mjs");
  const version = nextReleaseVersion({
    currentVersion: readPackageVersion(ROOT),
    tagVersions: listGitTagVersions(ROOT),
  });
  const result = await buildReleaseNotes({ version, commits });
  if (result.model) console.error(`更新说明由 ${result.model} 生成。`);
  console.log(result.markdown || `没有生成：${result.reason}`);
}
