import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { buildReleaseNotes, listCommits } from "./release-notes.mjs";

export const ROOT = resolve(fileURLToPath(new URL("../..", import.meta.url)));
export const REPO = process.env.GH_REPO || process.env.GITHUB_REPOSITORY || "eachann1024/thinkly-fast";

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

export function bumpPatch(version) {
  assert.match(version, /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/);
  const [major, minor, patch] = version.split(".").map(Number);
  return `${major}.${minor}.${patch + 1}`;
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
    /(\[\[package\]\]\r?\nname = "thinkly-fast"\r?\nversion = )"\d+\.\d+\.\d+"/,
    `$1"${version}"`,
  );
}

export function setPackageJsonVersion(text, version) {
  const pkg = JSON.parse(text);
  pkg.version = version;
  return `${JSON.stringify(pkg, null, 2)}\n`;
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

function isAncestor(sha, head = "HEAD") {
  try {
    execFileSync("git", ["merge-base", "--is-ancestor", sha, head], { cwd: ROOT, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
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

async function main() {
  const headSha = process.env.SOURCE_SHA || process.env.GITHUB_SHA
    || execFileSync("git", ["rev-parse", "HEAD"], { cwd: ROOT, encoding: "utf8" }).trim();
  const scheduled = process.env.GITHUB_EVENT_NAME === "schedule";
  const force = process.env.FORCE === "true";
  const publishInput = process.env.PUBLISH !== "false";
  const onMain = !process.env.GITHUB_REF || process.env.GITHUB_REF === "refs/heads/main";

  const cargoVersion = readCargoVersion(ROOT);
  const releases = getReleases();
  const stable = releases
    .filter((r) => !r.draft && !r.prerelease && versionFromTag(r.tag_name))
    .sort((a, b) => compareVersions(versionFromTag(a.tag_name), versionFromTag(b.tag_name)));
  const previous = stable.at(-1) || null;
  const publishedVersion = previous ? versionFromTag(previous.tag_name) : null;

  if (scheduled && !force && previous?.published_at) {
    const today = shanghaiDay(new Date().toISOString());
    const lastDay = shanghaiDay(previous.published_at);
    if (today === lastDay) {
      summary(`今日（${today}）已发布过 ${previous.tag_name}，每日最多自动发布一次，本次跳过。`);
      const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: false, publish: false, version: cargoVersion, release_tag: previous.tag_name });
      return;
    }
  }

  let version = cargoVersion;
  let autoBump = false;
  if (publishedVersion && compareVersions(version, publishedVersion) <= 0) {
    version = bumpPatch(publishedVersion);
    autoBump = true;
  }

  const previousSha = resolveTagCommit(previous);
  if (previousSha && previousSha === headSha && !force) {
    summary(`当前 HEAD (${headSha.slice(0, 7)}) 与最新 Release ${previous.tag_name} 完全一致，跳过构建。`);
    const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: false, publish: false, version: cargoVersion, release_tag: previous.tag_name });
    return;
  }

  const rangeSpec = previousSha && isAncestor(previousSha, headSha) ? `${previousSha}..${headSha}` : "-n 12";
  const commits = listCommits(ROOT, rangeSpec);
  const notes = await buildReleaseNotes({ version, commits });

  if (previous && notes.skip && !force) {
    summary(`自 ${previous.tag_name} 以来${notes.reason}（共扫描 ${commits.length} 条提交），跳过自动发布。`);
    const plan = { shouldBuild: false, publish: false, version: cargoVersion, tag: previous.tag_name, commit: headSha };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: false, publish: false, version: cargoVersion, release_tag: previous.tag_name });
    return;
  }

  const markdown = notes.markdown || "🌈 本次更新重点：例行构建与稳定性维护更新。";
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
    should_build: true,
    publish: shouldPublish,
    version,
    release_tag: tag,
    auto_bump: autoBump,
  });
  summary(`准备发布 Yu ${tag}（autoBump=${autoBump}, notes=${plan.notesSource}）`);
}

if (import.meta.main) {
  await main();
}
