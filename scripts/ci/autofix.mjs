// CI 挂了以后自动修：先在最新 main 上重跑检查，还挂就按 release-notes.mjs 的模型顺序
// （CHANGELOG_PROVIDERS，当前 gpt-6-luna → deepseek-flash）让模型给出修改，通过 scripts/ci/check.sh 才提交推送。
// 结果写到 GITHUB_OUTPUT：status=clean|fixed|failed；失败说明写到 AUTOFIX_REPORT。
import { execFileSync, spawnSync } from "node:child_process";
import { appendFileSync, existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { resolve, posix } from "node:path";
import { fileURLToPath } from "node:url";
import { publicFailure, readCompletion, resolveProviders } from "../release/release-notes.mjs";

const ROOT = resolve(fileURLToPath(new URL(".", import.meta.url)), "../..");
const EDITABLE = ["cmd/", "internal/", "assets/"];
const CHECKS_PER_MODEL = 2;
const TURNS_PER_CHECK = 4;
const MODEL_TIMEOUT_MS = 240_000;
const BUDGET_MS = 40 * 60 * 1000;
const MAX_FILE_BYTES = 120_000;
const MAX_CONTEXT_FILES = 12;
const AUTOFIX_SUBJECT = "fix(ci): autofix";

const started = Date.now();
const env = process.env;
const report = [];

function out(key, value) {
  if (env.GITHUB_OUTPUT) appendFileSync(env.GITHUB_OUTPUT, `${key}=${value}\n`);
}

function git(...args) {
  return execFileSync("git", args, { cwd: ROOT, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 }).trim();
}

function tail(text, max) {
  return text.length > max ? `…(前面省略)\n${text.slice(-max)}` : text;
}

function runCheck() {
  const res = spawnSync("bash", ["scripts/ci/check.sh"], { cwd: ROOT, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  const log = `${res.stdout || ""}\n${res.stderr || ""}`;
  process.stdout.write(tail(log, 20_000));
  return { ok: res.status === 0, log };
}

// 只保留真正出错的行和附近几行，日志里大量 "ok package" 对模型没用。
function failureExcerpt(log) {
  const lines = log.split("\n");
  const keep = new Set();
  lines.forEach((line, i) => {
    if (/FAIL|panic:|error|Error|undefined|cannot|\.go:\d+/.test(line) && !/^ok\s/.test(line)) {
      for (let j = Math.max(0, i - 3); j <= Math.min(lines.length - 1, i + 6); j += 1) keep.add(j);
    }
  });
  const picked = [...keep].sort((a, b) => a - b).map((i) => lines[i]).join("\n");
  return tail(picked || log, 40_000);
}

function safePath(p) {
  const norm = posix.normalize(String(p || "").replaceAll("\\", "/").replace(/^\.\//, ""));
  if (!norm || norm.startsWith("..") || norm.startsWith("/")) return "";
  return norm;
}

function readable(p) {
  const path = safePath(p);
  if (!path) return "";
  const abs = resolve(ROOT, path);
  if (!existsSync(abs) || !statSync(abs).isFile() || statSync(abs).size > MAX_FILE_BYTES) return "";
  return path;
}

function filesFromLog(log) {
  const found = new Set();
  const failedPkgDirs = [...log.matchAll(/^(?:FAIL|---)?\s*FAIL\s+block\/(\S+)/gm)].map((m) => m[1]);
  for (const m of log.matchAll(/([\w./-]+\.go):\d+/g)) {
    const name = m[1];
    const direct = readable(name);
    if (direct) found.add(direct);
    else for (const dir of failedPkgDirs) {
      const guess = readable(`${dir}/${posix.basename(name)}`);
      if (guess) found.add(guess);
    }
  }
  return [...found];
}

function recentChangedFiles() {
  const names = git("log", "-3", "--name-only", "--format=").split("\n");
  return names.filter((n) => n.endsWith(".go") && readable(n));
}

function fileBlock(path) {
  return `=== FILE: ${path} ===\n${readFileSync(resolve(ROOT, path), "utf8")}\n=== END FILE ===`;
}

const SYSTEM = `You fix failing CI for a Go desktop app (module "block").
Make the smallest correct change so "go vet", cross-platform builds and "go test ./..." pass.
Prefer fixing product code. Change a test only when the recent commit intentionally changed behavior and the test expectation is outdated; never delete, skip or weaken a test just to make it pass.
You may only edit files under ${EDITABLE.join(", ")}.
Reply with ONE JSON object and nothing else, in one of these shapes:
{"need_files": ["path/one.go", "path/two.go"]}   // ask to read more files first (repo-relative paths)
{"summary": "one line in English", "edits": [{"path": "internal/x/y.go", "search": "exact existing text", "replace": "new text"}]}
Each "search" must match the current file exactly once, including whitespace and tabs. Keep each search short but unique.`;

function parseReply(raw) {
  const fenced = /```(?:json)?\s*([\s\S]*?)```/.exec(raw);
  const text = fenced ? fenced[1] : raw;
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start < 0 || end <= start) throw new Error("回复里没有 JSON");
  return JSON.parse(text.slice(start, end + 1));
}

async function ask(provider, messages) {
  const res = await fetch(provider.url, {
    method: "POST",
    headers: provider.headers,
    redirect: "error",
    signal: AbortSignal.timeout(MODEL_TIMEOUT_MS),
    body: JSON.stringify({ model: provider.model, temperature: 0.1, messages }),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    const err = new Error("响应不是 JSON");
    err.publicReason = "响应不是 JSON";
    throw err;
  }
  return readCompletion(provider.model, data).raw;
}

// 全部校验通过才写盘，避免半套修改留在工作区。
function applyEdits(edits) {
  if (!Array.isArray(edits) || edits.length === 0) return "没有给出任何修改";
  const next = new Map();
  for (const edit of edits) {
    const path = safePath(edit?.path);
    if (!path || !EDITABLE.some((prefix) => path.startsWith(prefix))) return `不允许修改 ${edit?.path}`;
    if (!existsSync(resolve(ROOT, path))) return `${path} 不存在`;
    const current = next.get(path) ?? readFileSync(resolve(ROOT, path), "utf8");
    const search = String(edit.search ?? "");
    if (!search) return `${path} 的 search 为空`;
    const first = current.indexOf(search);
    if (first < 0) return `${path} 里找不到这段 search：\n${search.slice(0, 400)}`;
    if (current.indexOf(search, first + 1) >= 0) return `${path} 里这段 search 出现了不止一次：\n${search.slice(0, 400)}`;
    next.set(path, current.slice(0, first) + String(edit.replace ?? "") + current.slice(first + search.length));
  }
  for (const [path, content] of next) writeFileSync(resolve(ROOT, path), content);
  return "";
}

function revert() {
  git("checkout", "--", ".");
}

async function tryProvider(provider, baseContext, firstLog) {
  let lastLog = firstLog;
  for (let check = 1; check <= CHECKS_PER_MODEL; check += 1) {
    const messages = [
      { role: "system", content: SYSTEM },
      { role: "user", content: baseContext },
    ];
    if (check > 1) {
      messages.push({
        role: "user",
        content: `Your previous fix was applied and reverted because CI still failed. Edits apply to the original files shown above.\nNew failure:\n${failureExcerpt(lastLog)}`,
      });
    }
    let applied = false;
    let summary = "";
    for (let turn = 1; turn <= TURNS_PER_CHECK && !applied; turn += 1) {
      if (Date.now() - started > BUDGET_MS) return { ok: false, reason: `${provider.model}：总时间预算已用完` };
      let raw;
      try {
        raw = await ask(provider, messages);
      } catch (err) {
        return { ok: false, reason: publicFailure(err, provider.model).replace("没有生成更新说明", "没有给出修改") };
      }
      messages.push({ role: "assistant", content: raw });
      let reply;
      try {
        reply = parseReply(raw);
      } catch {
        messages.push({ role: "user", content: "That was not a single valid JSON object. Reply again with JSON only." });
        continue;
      }
      if (Array.isArray(reply.need_files) && !reply.edits) {
        const files = reply.need_files.map(readable).filter(Boolean).slice(0, 6);
        messages.push({
          role: "user",
          content: files.length ? files.map(fileBlock).join("\n\n") : "None of those files exist or they are too large. Work with what you have.",
        });
        continue;
      }
      const problem = applyEdits(reply.edits);
      if (problem) {
        messages.push({ role: "user", content: `Edits rejected, nothing was applied: ${problem}\nReply again with corrected JSON.` });
        continue;
      }
      applied = true;
      summary = String(reply.summary || "").replace(/\s+/g, " ").trim().slice(0, 120);
    }
    if (!applied) {
      report.push(`${provider.model} 第 ${check} 轮：没有给出可用的修改`);
      continue;
    }
    console.log(`::group::${provider.model} 第 ${check} 轮修改后重跑检查`);
    const result = runCheck();
    console.log("::endgroup::");
    if (result.ok) return { ok: true, summary };
    report.push(`${provider.model} 第 ${check} 轮：修改后检查仍失败`);
    lastLog = result.log;
    revert();
  }
  return { ok: false, reason: `${provider.model}：${CHECKS_PER_MODEL} 轮都没修好` };
}

function commitAndPush(model, summary) {
  git("config", "user.name", "github-actions[bot]");
  git("config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com");
  git("add", "-A", ...EDITABLE.map((p) => p.replace(/\/$/, "")));
  const runUrl = `${env.GITHUB_SERVER_URL}/${env.GITHUB_REPOSITORY}/actions/runs/${env.GITHUB_RUN_ID}`;
  const message = `${AUTOFIX_SUBJECT}: ${summary || "repair failing checks"}\n\nAutofix-Model: ${model}\nAutofix-Run: ${runUrl}\nRelease-Note: none\n`;
  git("commit", "-m", message);
  for (let attempt = 1; attempt <= 2; attempt += 1) {
    const push = spawnSync("git", ["push", "origin", "HEAD:main"], { cwd: ROOT, encoding: "utf8" });
    if (push.status === 0) return "";
    // main 在修的过程中又有新提交：变基后必须重新检查一次再推。
    git("fetch", "origin", "main");
    const rebase = spawnSync("git", ["rebase", "origin/main"], { cwd: ROOT, encoding: "utf8" });
    if (rebase.status !== 0) {
      spawnSync("git", ["rebase", "--abort"], { cwd: ROOT });
      return "main 有新提交，自动修改与之冲突，无法变基";
    }
    if (!runCheck().ok) return "变基到最新 main 后检查又失败";
  }
  return "推送 main 失败";
}

async function main() {
  console.log("::group::在最新 main 上重跑检查");
  const first = runCheck();
  console.log("::endgroup::");
  if (first.ok) {
    console.log("最新 main 已经能通过检查，不需要修。");
    out("status", "clean");
    return;
  }

  const { ready, skipped } = resolveProviders(env);
  report.push(...skipped);
  if (!ready.length) {
    out("status", "failed");
    return finish(first.log);
  }

  const context = [
    ...new Set([...filesFromLog(first.log), ...recentChangedFiles()]),
  ].slice(0, MAX_CONTEXT_FILES);
  const recent = tail(git("log", "-3", "-p", "--stat", "--format=commit %h%nSubject: %s%n"), 60_000);
  const baseContext = [
    `CI failure (excerpt):\n${failureExcerpt(first.log)}`,
    `Recent commits on main (newest first, likely cause):\n${recent}`,
    `Current file contents:\n${context.map(fileBlock).join("\n\n") || "(no files detected; ask with need_files)"}`,
  ].join("\n\n");

  for (const provider of ready) {
    const result = await tryProvider(provider, baseContext, first.log);
    if (result.ok) {
      const pushError = commitAndPush(provider.model, result.summary);
      if (!pushError) {
        console.log(`已由 ${provider.model} 修复并推送 main。`);
        out("status", "fixed");
        out("model", provider.model);
        return;
      }
      report.push(pushError);
      out("status", "failed");
      return finish(first.log);
    }
    report.push(result.reason);
  }
  out("status", "failed");
  finish(first.log);
}

function finish(log) {
  const body = [
    "CI 失败，自动修复也没成功，需要人工处理。",
    "",
    "**尝试记录**",
    ...report.map((line) => `- ${line}`),
    "",
    "**最新 main 的失败摘要**",
    "```",
    tail(failureExcerpt(log), 6_000),
    "```",
  ].join("\n");
  if (env.AUTOFIX_REPORT) writeFileSync(env.AUTOFIX_REPORT, body);
  console.log(body);
}

main().catch((err) => {
  report.push(`自动修复脚本异常：${err?.message || err}`);
  out("status", "failed");
  finish("");
});
