import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { EN_FOOTER, ZH_FOOTER } from "./release-notes-check.mjs";
import { buildReleaseNotes, listCommits } from "./release-notes.mjs";
import { resolveReleaseRange } from "./release-range.mjs";

export const ROOT = resolve(fileURLToPath(new URL("../..", import.meta.url)));
export const REPO = process.env.GH_REPO || process.env.GITHUB_REPOSITORY || "eachann1024/block";

export function gh(args, input) {
  return execFileSync("gh", args, {
    cwd: ROOT,
    encoding: "utf8",
    input,
    maxBuffer: 20 * 1024 * 1024,
    stdio: ["pipe", "pipe", "pipe"],
  });
}

export function versionFromTag(tag) {
  return /^v?(\d+\.\d+\.\d+)(?:-[0-9a-f]{7,40})?$/.exec(tag || "")?.[1] || null;
}

export function compareVersions(left, right) {
  const a = left.split(".").map(Number);
  const b = right.split(".").map(Number);
  for (let i = 0; i < 3; i += 1) {
    if (a[i] !== b[i]) return a[i] - b[i];
  }
  return 0;
}

export function releaseVersion(root = ROOT, sha = "HEAD") {
  const count = execFileSync("git", ["rev-list", "--count", sha], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
  assert.match(count, /^[1-9]\d*$/, `git rev-list --count returned ${count}`);
  return `1.0.${count}`;
}

export function readCargoVersion(root = ROOT) {
  const cargo = readFileSync(resolve(root, "Cargo.toml"), "utf8");
  const m = /^version\s*=\s*"(\d+\.\d+\.\d+)"/m.exec(cargo);
  assert.ok(m, "Cargo.toml is missing a valid package version");
  return m[1];
}

export function setCargoTomlVersion(text, version) {
  return text.replace(/^(version\s*=\s*)"\d+\.\d+\.\d+"/m, `$1"${version}"`);
}

export function setCargoLockVersion(text, version) {
  return text.replace(
    /(\[\[package\]\]\r?\nname = "block"\r?\nversion = )"\d+\.\d+\.\d+"(\r?\ndependencies =)/,
    `$1"${version}"$2`,
  );
}

export function setPackageJsonVersion(text, version) {
  const pkg = JSON.parse(text);
  pkg.version = version;
  return `${JSON.stringify(pkg, null, 2)}\n`;
}

export function applyReleasePlanVersion() {
  const planPath = resolve(ROOT, "release-plan.json");
  let version = readCargoVersion(ROOT);
  if (existsSync(planPath)) {
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    if (typeof plan.version === "string" && plan.version) {
      version = plan.version;
      syncVersionFiles(ROOT, version);
    }
  }
  return version;
}

export function syncVersionFiles(root, version) {
  const cargoPath = resolve(root, "Cargo.toml");
  writeFileSync(cargoPath, setCargoTomlVersion(readFileSync(cargoPath, "utf8"), version));
  const lockPath = resolve(root, "Cargo.lock");
  writeFileSync(lockPath, setCargoLockVersion(readFileSync(lockPath, "utf8"), version));
  const pkgPath = resolve(root, "package.json");
  writeFileSync(pkgPath, setPackageJsonVersion(readFileSync(pkgPath, "utf8"), version));
}

function shanghaiDay(isoString) {
  if (!isoString) return "";
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date(isoString));
}

export function getReleases() {
  try {
    return gh(["api", "--paginate", `repos/${REPO}/releases?per_page=100`, "--jq", ".[]"])
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } catch {
    return [];
  }
}

function resolveTagCommit(release) {
  if (!release) return null;
  try {
    const sha = execFileSync("git", ["rev-parse", `${release.tag_name}^{commit}`], {
      cwd: ROOT,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
    if (/^[0-9a-f]{40}$/i.test(sha)) return sha;
  } catch {}
  try {
    const sha = gh(["api", `repos/${REPO}/commits/${release.tag_name}`, "--jq", ".sha"]).trim();
    if (/^[0-9a-f]{40}$/i.test(sha)) return sha;
  } catch {}
  if (/^[0-9a-f]{40}$/i.test(release.target_commitish || "")) return release.target_commitish;
  return null;
}

function summary(line) {
  console.log(line);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${line}\n`);
}

function writeOutputs(entries) {
  if (!process.env.GITHUB_OUTPUT) return;
  const lines = Object.entries(entries).map(([k, v]) => `${k}=${v}`).join("\n") + "\n";
  appendFileSync(process.env.GITHUB_OUTPUT, lines);
}

// GitHub Actions 会把输出值 false 当成布尔并变成空字符串，后续作业就全部被跳过。
function outputFlag(value) {
  return value ? "yes" : "no";
}

function bumpPatch(version) {
  const [major, minor, patch] = version.split(".").map(Number);
  return `${major}.${minor}.${patch + 1}`;
}

function versionAboveExisting(computed, releases) {
  const taken = releases.map((release) => versionFromTag(release.tag_name)).filter(Boolean);
  let version = computed;
  const highest = taken.reduce((best, item) => (!best || compareVersions(item, best) > 0 ? item : best), null);
  if (highest && compareVersions(version, highest) <= 0) version = bumpPatch(highest);
  const used = new Set(taken);
  while (used.has(version)) version = bumpPatch(version);
  return version;
}

function forcedReleaseNotes() {
  return [
    "## English",
    "",
    "### Improvements",
    "",
    "- This build was published manually.",
    "",
    EN_FOOTER,
    "",
    "---",
    "",
    "## 中文",
    "",
    "### 改进",
    "",
    "- 这一版由手动发布。",
    "",
    ZH_FOOTER,
  ].join("\n");
}

async function main() {
  const headSha = process.env.SOURCE_SHA || process.env.GITHUB_SHA
    || execFileSync("git", ["rev-parse", "HEAD"], { cwd: ROOT, encoding: "utf8" }).trim();
  const scheduled = process.env.GITHUB_EVENT_NAME === "schedule";
  const force = process.env.FORCE === "true";
  const publishInput = process.env.PUBLISH !== "false";
  const onMain = !process.env.GITHUB_REF || process.env.GITHUB_REF === "refs/heads/main";

  const cargoVersion = readCargoVersion(ROOT);
  let version = releaseVersion(ROOT, headSha);
  const releases = getReleases();
  const stable = releases
    .filter((r) => !r.draft && !r.prerelease && versionFromTag(r.tag_name))
    .sort((a, b) => compareVersions(versionFromTag(a.tag_name), versionFromTag(b.tag_name)));
  const previous = stable.at(-1) || null;

  if (scheduled && !force && previous?.published_at) {
    const today = shanghaiDay(new Date().toISOString());
    const lastDay = shanghaiDay(previous.published_at);
    if (today === lastDay) {
      summary(`今日（${today}）已发布过 ${previous.tag_name}，每日最多自动发布一次，本次跳过。`);
      const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: cargoVersion, release_tag: previous.tag_name });
      return;
    }
  }

  const previousSha = resolveTagCommit(previous);
  if (previousSha && previousSha === headSha && !force) {
    summary(`当前 HEAD (${headSha.slice(0, 7)}) 与最新 Release ${previous.tag_name} 完全一致，跳过构建。`);
    const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: cargoVersion, release_tag: previous.tag_name });
    return;
  }

  let rangeSpec = null;
  if (!(previousSha && previousSha === headSha)) {
    try {
      const resolved = resolveReleaseRange({ previous, previousSha, headSha });
      rangeSpec = resolved.range;
      if (resolved.note) summary(resolved.note);
    } catch (error) {
      summary(error?.message || String(error));
      const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous?.tag_name || "", commit: headSha };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: cargoVersion, release_tag: previous?.tag_name || "" });
      return;
    }
  }

  if (force) version = versionAboveExisting(version, releases);
  const autoBump = version !== cargoVersion;
  const commits = rangeSpec ? listCommits(ROOT, rangeSpec) : [];
  if (rangeSpec) summary(`更新说明区间 ${rangeSpec}，共 ${commits.length} 条提交。`);
  let notes = await buildReleaseNotes({ version, commits });

  if (previous && notes.skip && !force) {
    summary(`自 ${previous.tag_name} 以来${notes.reason}（共扫描 ${commits.length} 条提交），跳过自动发布。`);
    const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: cargoVersion, release_tag: previous.tag_name });
    return;
  }

  if (!notes.markdown) {
    if (commits.length === 0 && force) {
      notes = { markdown: forcedReleaseNotes(), source: "forced" };
      summary("没有新提交，因 force=true 使用手动发布说明。");
    } else {
      summary(`更新说明没有覆盖区间里的 ${commits.length} 条提交（${notes.reason || "空"}），停止发布。`);
      const plan = { shouldBuild: false, publish: false, version, tag: `v${version}`, commit: headSha };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version, release_tag: `v${version}` });
      return;
    }
  }
  const markdown = notes.markdown;
  const tag = `v${version}`;
  const shouldPublish = onMain && publishInput;
  const plan = {
    shouldBuild: true,
    publish: shouldPublish,
    autoBump,
    version,
    tag,
    commit: headSha,
    previousTag: previous?.tag_name || null,
    notesSource: notes.source || "fallback",
    releaseNotes: markdown,
  };
  writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
  writeOutputs({
    should_build: outputFlag(true),
    publish: outputFlag(shouldPublish),
    version,
    release_tag: tag,
    auto_bump: autoBump,
  });
  summary(`准备发布 Block ${tag}（autoBump=${autoBump}, notes=${plan.notesSource}）`);
}

if (import.meta.main) {
  await main();
}
