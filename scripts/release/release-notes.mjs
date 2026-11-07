import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { request } from "node:https";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { bilingualFromCommit } from "./release-note-rules.mjs";
import {
  EN_SECTIONS,
  MAX_PER_SECTION,
  ZH_SECTIONS,
  lineMixesLanguages,
  validateReleaseNotes,
} from "./release-notes-check.mjs";

export const DEFAULT_CHANGELOG_MODEL = "antigravity/gemini-3.8-flash";
export { lineMixesLanguages, validateReleaseNotes };

const DIR = fileURLToPath(new URL(".", import.meta.url));
const ROOT = resolve(DIR, "../..");
const PROMPT_PATH = resolve(DIR, "changelog-prompt.md");
const MAX_ATTEMPTS = 2;
const TIMEOUT_MS = 90_000;
const BOT_AUTHORS = new Set(["github-actions[bot]", "github-actions"]);
const SKIP_PREFIX = /^(ci|docs|build|test|chore|design|release)(\([^)]*\))?:/i;
const RELEASE_SUBJECT = /^(chore\(release\):|release:)/i;

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

function splitNote(raw = "") {
  const text = raw.trim();
  if (!text || !text.includes("|||")) return { zh: text, en: "" };
  const [zh, ...rest] = text.split("|||");
  return { zh: zh.trim(), en: rest.join("|||").trim() };
}

export function listCommits(root = ROOT, rangeSpec = "-n 20") {
  const args = ["log", "--no-merges", "--reverse", "--date=format-local:%Y-%m-%d", "--format=%H%x00%an%x00%cd%x00%s%x00%(trailers:key=Release-Note,valueonly,separator=%x20)%x00%(trailers:key=Release-Note-En,valueonly,separator=%x20)"];
  if (rangeSpec.startsWith("-n")) args.push(...rangeSpec.split(/\s+/));
  else args.push(rangeSpec);
  const out = execFileSync("git", args, { cwd: root, encoding: "utf8", env: { ...process.env, TZ: "Asia/Shanghai" } }).trimEnd();
  if (!out) return [];
  return out.split("\n").filter(Boolean).map((line) => {
    const [sha, author, date, subject, releaseNoteRaw = "", releaseNoteEnRaw = ""] = line.split("\x00");
    const paths = execFileSync("git", ["diff-tree", "--no-commit-id", "--name-only", "-r", sha], { cwd: root, encoding: "utf8" })
      .trimEnd().split("\n").filter(Boolean);
    const split = splitNote(releaseNoteRaw);
    return {
      sha,
      author,
      date,
      subject,
      releaseNote: split.zh,
      releaseNoteEn: (releaseNoteEnRaw || "").trim() || split.en,
      paths,
    };
  });
}

function magpieModel(env) {
  const requested = env.CHANGELOG_MODEL?.trim() || "";
  if (/gemini-3\.8/i.test(requested) && !/openai|anthropic|claude|gpt-|deepseek|qwen|grok|ollama/i.test(requested)) {
    return requested;
  }
  return DEFAULT_CHANGELOG_MODEL;
}

function pinnedFetch(caFile) {
  const ca = readFileSync(caFile, "utf8");
  return (url, { method, headers, body, signal }) => new Promise((resolveRes, reject) => {
    const req = request(url, { method, headers, ca, signal }, (res) => {
      const chunks = [];
      res.on("data", (chunk) => chunks.push(chunk));
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

function commitLines(commits) {
  return commits.map(({ subject, releaseNote = "", releaseNoteEn = "" }) => {
    const clean = subject.replace(/\s*\(#\d+\)$/, "").trim();
    if (releaseNote && releaseNoteEn) return `${clean} | ${releaseNote} ||| ${releaseNoteEn}`;
    if (releaseNote) return `${clean} | ${releaseNote}`;
    if (releaseNoteEn) return `${clean} ||| ${releaseNoteEn}`;
    return clean;
  });
}

export async function polishReleaseNotes({ version, commits, env = process.env }) {
  const baseUrl = env.MAGPIE_BASE_URL?.trim();
  if (!baseUrl) return { status: "fallback", reason: "未配置 MAGPIE_BASE_URL" };
  const caFile = env.MAGPIE_CA_CERT?.trim();
  const fetchImpl = caFile ? pinnedFetch(resolve(ROOT, caFile)) : fetch;
  const model = magpieModel(env);
  const inputLines = commitLines(commits);
  if (!inputLines.length) return { status: "empty", model };
  const url = `${baseUrl.replace(/\/+$/, "").replace(/\/v1$/, "")}/v1/chat/completions`;
  const headers = { "content-type": "application/json" };
  if (env.MAGPIE_API_KEY?.trim()) headers.authorization = `Bearer ${env.MAGPIE_API_KEY.trim()}`;
  const system = readFileSync(PROMPT_PATH, "utf8");
  const asked = `These are the only commits for Block ${version} since the previous tag (${inputLines.length}):\n\n${inputLines.join("\n")}`;
  let lastError = "";
  let lastRaw = "";
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    const messages = [{ role: "system", content: system }, { role: "user", content: asked }];
    if (attempt > 1 && lastRaw) {
      messages.push({ role: "assistant", content: lastRaw });
      messages.push({ role: "user", content: `Proofread and rewrite. ${lastError} Keep one language per line. English above ---, Simplified Chinese below. Output markdown only.` });
    }
    try {
      const res = await fetchImpl(url, {
        method: "POST",
        headers,
        signal: AbortSignal.timeout(TIMEOUT_MS),
        body: JSON.stringify({ model, temperature: 0.2, messages }),
      });
      assert.ok(res.ok, `HTTP ${res.status}`);
      const data = await res.json();
      const used = String(data?.model || model);
      assert.ok(/gemini-3\.8/i.test(used), `Magpie returned ${used}, expected gemini 3.8`);
      const raw = data?.choices?.[0]?.message?.content;
      assert.ok(typeof raw === "string" && raw.trim(), "模型返回为空");
      lastRaw = raw;
      const markdown = validateReleaseNotes(raw);
      if (!markdown) return { status: "empty", model: used };
      return { status: "polished", markdown, model: used };
    } catch (err) {
      lastError = err?.message || String(err);
    }
  }
  return { status: "fallback", reason: lastError };
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

export function fallbackReleaseNotes(commits) {
  const buckets = [{ zh: [], en: [] }, { zh: [], en: [] }, { zh: [], en: [] }];
  const kindIndex = { feat: 0, improve: 1, fix: 2 };
  for (const commit of commits.filter((item) => !isExcludedCommit(item))) {
    const pair = bilingualFromCommit(commit);
    if (!pair) continue;
    const bucket = buckets[kindIndex[pair.kind] ?? 0];
    if (bucket.zh.includes(pair.zh) || bucket.zh.length >= MAX_PER_SECTION) continue;
    if (lineMixesLanguages(`- ${pair.zh}`) || lineMixesLanguages(`- ${pair.en}`)) continue;
    bucket.zh.push(pair.zh);
    bucket.en.push(pair.en);
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
  if (polished.status === "polished") {
    return { skip: false, source: "polished", model: polished.model, markdown: polished.markdown };
  }
  try {
    const markdown = fallbackReleaseNotes(userCommits);
    if (markdown) return { skip: false, source: "fallback", reason: polished.reason, markdown };
  } catch (err) {
    return { skip: true, reason: err?.message || polished.reason, markdown: "" };
  }
  return { skip: true, reason: polished.reason || "无法生成双语更新说明", markdown: "" };
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
