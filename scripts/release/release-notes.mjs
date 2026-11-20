import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describeCommit } from "./commit-blurb.mjs";
import {
  EN_SECTIONS,
  ZH_SECTIONS,
  lineMixesLanguages,
  validateReleaseNotes,
} from "./release-notes-check.mjs";

export const DEFAULT_CHANGELOG_MODEL = "grok-4.7";
export { lineMixesLanguages, validateReleaseNotes };

const GROK_47 = /^grok-4\.7(?:@[\w.+-]{1,32})?$/;
const MAGPIE_ORIGIN = "http://100.115.90.80:3425";

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
  if (!hasExplicitNote && Array.isArray(paths) && paths.length > 0 && paths.every(isNonShippablePath)) return true;
  return false;
}

function splitNote(raw = "") {
  const text = raw.trim();
  if (!text || !text.includes("|||")) return { zh: text, en: "" };
  const [zh, ...rest] = text.split("|||");
  return { zh: zh.trim(), en: rest.join("|||").trim() };
}

export function listCommits(root = ROOT, rangeSpec = "-n 20") {
  const args = [
    "log",
    "--no-merges",
    "--topo-order",
    "--reverse",
    "--date=format-local:%Y-%m-%d",
    "--name-only",
    "--format=%x1e%H%x1f%an%x1f%cd%x1f%s%x1f%(trailers:key=Release-Note,valueonly,separator=%x20)%x1f%(trailers:key=Release-Note-En,valueonly,separator=%x20)%x1f",
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
  const commits = [];
  for (const chunk of out.split("\x1e")) {
    if (!chunk) continue;
    const [sha = "", author = "", date = "", subject = "", releaseNoteRaw = "", releaseNoteEnRaw = "", rest = ""] = chunk.split("\x1f");
    if (!sha.trim()) continue;
    const paths = rest
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean);
    const split = splitNote(releaseNoteRaw);
    commits.push({
      sha: sha.trim(),
      author,
      date,
      subject,
      releaseNote: split.zh,
      releaseNoteEn: (releaseNoteEnRaw || "").trim() || split.en,
      paths,
    });
  }
  return commits;
}

function shortToken(value) {
  if (typeof value !== "string") return "";
  const token = value.trim();
  return /^[A-Za-z0-9_.:-]{1,40}$/.test(token) ? token : "";
}

function magpieErrorToken(data) {
  if (!data || typeof data !== "object") return "";
  const error = data.error;
  if (typeof error === "string") return shortToken(error);
  if (error && typeof error === "object") {
    return shortToken(error.type) || shortToken(error.code) || shortToken(error.kind);
  }
  return shortToken(data.type) || shortToken(data.code);
}

export function publicFailure(err) {
  if (typeof err?.publicReason === "string" && err.publicReason) return err.publicReason;
  const name = err?.name || "";
  const msg = String(err?.message || "");
  if (name === "TimeoutError" || /The operation was aborted|timeout/i.test(msg)) {
    return "连接 Magpie 超时，grok-4.7 没有返回结果";
  }
  if (name === "AbortError") return "连接 Magpie 已中止，grok-4.7 没有返回结果";
  const http = /^HTTP (\d{3})(?: ([A-Za-z0-9_.:-]{1,40}))?$/.exec(msg);
  if (http) {
    const code = http[2] ? `（${http[2]}）` : "";
    return `Magpie HTTP ${http[1]}${code}，没有可用的 grok-4.7 结果`;
  }
  if (/redirect/i.test(msg)) return "Magpie 拒绝跳转到其他地址，未采用该结果";
  if (/fetch failed|ECONN|ENOTFOUND|EHOSTUNREACH|network/i.test(msg)) {
    return "无法连接 Magpie，grok-4.7 没有返回结果";
  }
  return "grok-4.7 没有生成更新说明";
}

export function magpieHttpFailure(status, data) {
  const token = magpieErrorToken(data);
  const err = new Error(token ? `HTTP ${status} ${token}` : `HTTP ${status}`);
  err.publicReason = publicFailure(err);
  return err;
}

export function resolveChangelogModel(env) {
  const requested = env.CHANGELOG_MODEL?.trim() || DEFAULT_CHANGELOG_MODEL;
  if (!GROK_47.test(requested)) {
    return { ok: false, reason: "CHANGELOG_MODEL 必须是 grok-4.7，未调用其他模型" };
  }
  return { ok: true, model: requested };
}

export function resolveMagpieChatUrl(baseUrl) {
  const raw = String(baseUrl || "").trim();
  if (!raw) return { ok: false, reason: "未配置 MAGPIE_BASE_URL，未调用 grok-4.7" };
  let url;
  try {
    url = new URL(raw);
  } catch {
    return { ok: false, reason: "MAGPIE_BASE_URL 不是合法 URL，未调用模型" };
  }
  if (url.username || url.password) {
    return { ok: false, reason: "MAGPIE_BASE_URL 不能携带凭据，未调用模型" };
  }
  if (url.protocol !== "http:" || url.hostname !== "100.115.90.80" || url.port !== "3425") {
    return { ok: false, reason: "MAGPIE_BASE_URL 只能是 http://100.115.90.80:3425，未调用其他地址" };
  }
  if (url.search || url.hash) {
    return { ok: false, reason: "MAGPIE_BASE_URL 不能带查询或片段，未调用模型" };
  }
  const path = url.pathname.replace(/\/+$/, "");
  if (path !== "" && path !== "/v1") {
    return { ok: false, reason: "MAGPIE_BASE_URL 路径只能是空或 /v1，未调用模型" };
  }
  return { ok: true, url: `${MAGPIE_ORIGIN}/v1/chat/completions` };
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

export function readCompletion(requested, data) {
  const used = typeof data?.model === "string" ? data.model.trim() : "";
  if (!GROK_47.test(used) || used.toLowerCase().split("@")[0] !== "grok-4.7") {
    const err = new Error("响应没有 model");
    err.publicReason = "响应没有 grok-4.7 的 model，不能当作生成成功";
    throw err;
  }
  if (requested.toLowerCase().split("@")[0] !== "grok-4.7") {
    const err = new Error("请求模型不是 grok-4.7");
    err.publicReason = "请求模型不是 grok-4.7，未采用该结果";
    throw err;
  }
  const raw = messageText(data?.choices?.[0]?.message?.content);
  if (!raw.trim()) {
    const err = new Error("模型返回为空");
    err.publicReason = "响应没有 choices 内容，不能当作 grok-4.7 生成成功";
    err.retry = true;
    throw err;
  }
  return { model: used, raw };
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
  return commits.map(({ subject, releaseNote = "", releaseNoteEn = "" }) => {
    const clean = subject.replace(/\s*\(#\d+\)$/, "").trim();
    if (releaseNote && releaseNoteEn) return `${clean} | ${releaseNote} ||| ${releaseNoteEn}`;
    if (releaseNote) return `${clean} | ${releaseNote}`;
    if (releaseNoteEn) return `${clean} ||| ${releaseNoteEn}`;
    return clean;
  });
}

async function polishBatch({ version, commits, url, headers, model, system }) {
  const inputLines = commitLines(commits);
  if (!inputLines.length) return { status: "empty", model };
  const asked = `These are the only commits for Block ${version} since the previous tag (${inputLines.length}):\n\n${inputLines.join("\n")}`;
  let lastRaw = "";
  let failure = "grok-4.7 没有生成更新说明";
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    const messages = [{ role: "system", content: system }, { role: "user", content: asked }];
    if (attempt > 1 && lastRaw) {
      messages.push({ role: "assistant", content: lastRaw });
      messages.push({
        role: "user",
        content: "Proofread and rewrite. The previous draft failed release-note validation. Keep one language per line. English above ---, Simplified Chinese below. Output markdown only.",
      });
    }
    try {
      const res = await fetch(url, {
        method: "POST",
        headers,
        redirect: "error",
        signal: AbortSignal.timeout(TIMEOUT_MS),
        body: JSON.stringify({ model, temperature: 0.2, messages }),
      });
      const data = await readJsonBody(res);
      if (!res.ok) throw magpieHttpFailure(res.status, data);
      if (!data) {
        const err = new Error("响应不是 JSON");
        err.publicReason = "响应不是 JSON，没有 grok-4.7 结果";
        throw err;
      }
      const completion = readCompletion(model, data);
      lastRaw = completion.raw;
      let markdown = "";
      try {
        markdown = validateReleaseNotes(completion.raw);
      } catch {
        const err = new Error("生成结果未通过更新说明校验");
        err.publicReason = "生成结果未通过更新说明校验";
        err.retry = true;
        throw err;
      }
      if (!markdown) {
        const err = new Error("模型返回为空");
        err.publicReason = "响应没有可用的更新说明";
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
      failure = publicFailure(err);
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

export async function polishReleaseNotes({ version, commits, env = process.env }) {
  if (env.MAGPIE_REACHABLE === "no") {
    return { status: "fallback", reason: "CI 未连通 100.115.90.80:3425，未调用 grok-4.7" };
  }
  const modelResult = resolveChangelogModel(env);
  if (!modelResult.ok) return { status: "fallback", reason: modelResult.reason };
  const endpoint = resolveMagpieChatUrl(env.MAGPIE_BASE_URL);
  if (!endpoint.ok) return { status: "fallback", reason: endpoint.reason };
  const model = modelResult.model;
  if (!commits.length) return { status: "empty", model };
  const headers = { "content-type": "application/json" };
  const system = readFileSync(PROMPT_PATH, "utf8");
  const url = endpoint.url;

  if (commits.length <= BATCH_THRESHOLD) {
    return polishBatch({ version, commits, url, headers, model, system });
  }

  const batchMarkdowns = [];
  let usedModel = "";
  const deadline = Date.now() + MAX_POLISH_MS;
  for (let offset = 0; offset < commits.length; offset += BATCH_SIZE) {
    if (Date.now() >= deadline) {
      return { status: "fallback", reason: "grok-4.7 多批生成超出时间预算，改用本地规则" };
    }
    const batch = commits.slice(offset, offset + BATCH_SIZE);
    const result = await polishBatch({ version, commits: batch, url, headers, model, system });
    if (result.status !== "polished") {
      return result.status === "fallback"
        ? result
        : { status: "fallback", reason: `第 ${offset / BATCH_SIZE + 1} 批没有生成结果` };
    }
    if (!result.model) return { status: "fallback", reason: "响应没有 grok-4.7 的 model，不能当作生成成功" };
    usedModel = result.model;
    batchMarkdowns.push(result.markdown);
  }
  try {
    const markdown = mergePolishedBatches(batchMarkdowns);
    if (!markdown || !usedModel) return { status: "fallback", reason: "生成结果未通过更新说明校验" };
    const listed = listedItems(markdown);
    if (listed < commits.length) {
      return { status: "fallback", reason: `条目数 ${listed}/${commits.length}` };
    }
    return { status: "polished", markdown, model: usedModel };
  } catch {
    return { status: "fallback", reason: "生成结果未通过更新说明校验" };
  }
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

function sanitizeFallbackBullet(text, cjk) {
  let clean = String(text || "")
    .replace(/`/g, "")
    .replace(/\(#\d+\)/g, "")
    .replace(/#\d+/g, "")
    .replace(/\b[0-9a-f]{7,40}\b/gi, "")
    .replace(/\b(?:gpui|wgpu|wasm|ipc|debounce|typecheck)\b/gi, "")
    .replace(/\s+/g, " ")
    .trim();
  if (!clean) return "";
  if (cjk) {
    clean = clean.replace(/[.!?]+$/, "").trim();
    if (!/[。！？]$/.test(clean)) clean += "。";
  } else {
    clean = clean.replace(/[。！？]+$/, "").trim();
    if (!/[.!?]$/.test(clean)) clean += ".";
  }
  return clean;
}

function isValidFallbackPair(zh, en) {
  if (!zh || !en) return false;
  if (lineMixesLanguages(`- ${zh}`) || lineMixesLanguages(`- ${en}`)) return false;
  try {
    validateReleaseNotes(`## English\n\n### Improvements\n- ${en}\n\n---\n\n## 中文\n\n### 改进\n- ${zh}`);
    return true;
  } catch {
    return false;
  }
}

export function fallbackReleaseNotes(commits) {
  const buckets = [{ zh: [], en: [] }, { zh: [], en: [] }, { zh: [], en: [] }, { zh: [], en: [] }];
  const kindIndex = { feat: 0, improve: 1, fix: 2, other: 3 };
  for (const commit of commits.filter((item) => !isExcludedCommit(item))) {
    const pair = describeCommit(commit);
    const zh = sanitizeFallbackBullet(pair.zh, true);
    const en = sanitizeFallbackBullet(pair.en, false);
    const bucket = buckets[kindIndex[pair.kind] ?? 3];
    if (!isValidFallbackPair(zh, en)) continue;
    if (bucket.zh.includes(zh) || bucket.en.includes(en)) continue;
    bucket.zh.push(zh);
    bucket.en.push(en);
  }
  const en = renderHalf("## English", EN_SECTIONS, buckets, "en");
  const zh = renderHalf("## 中文", ZH_SECTIONS, buckets, "zh");
  if (!en || !zh) return "";
  return validateReleaseNotes(`${en}\n\n---\n\n${zh}`);
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
  const reason = polished.reason || "grok-4.7 没有生成更新说明";
  try {
    const markdown = fallbackReleaseNotes(userCommits);
    const listed = markdown ? listedItems(markdown) : 0;
    if (markdown && listed > 0) {
      return { skip: false, source: "fallback", reason, generationFailed: true, markdown };
    }
    return {
      skip: true,
      source: "fallback",
      generationFailed: true,
      reason: `本地规则只写了 ${listed}/${userCommits.length} 条（${reason}）`,
      markdown: "",
    };
  } catch {
    return { skip: true, source: "fallback", generationFailed: true, reason, markdown: "" };
  }
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
  if (result.generationFailed) {
    console.error(`警告：grok-4.7 没有生成更新说明（${result.reason}）。下面不是模型结果。`);
  }
  console.log(result.markdown || `跳过：${result.reason}`);
}
