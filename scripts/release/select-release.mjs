import { execFileSync } from "node:child_process";
import { appendFileSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { EN_FOOTER, ZH_FOOTER } from "./release-notes-check.mjs";
import { buildReleaseNotes, isExcludedCommit, listCommits } from "./release-notes.mjs";
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

export function normalizeReleaseChannel(raw) {
  return String(raw || "").trim().toLowerCase() === "stable" ? "stable" : "nightly";
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
  const [major, minor, patch] = version.split(".").map(Number);
  return `${major}.${minor}.${patch + 1}`;
}

export function listGitTagVersions(root = ROOT) {
  try {
    const out = execFileSync("git", ["tag", "--list", "v*"], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    return out
      .split("\n")
      .map((tag) => versionFromTag(tag.trim()))
      .filter(Boolean);
  } catch {
    return [];
  }
}

export const MAX_WINDOWS_PE_PART = 65535;

export function assertValidWindowsPEVersion(version) {
  if (typeof version !== "string" || !/^\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`Invalid semantic version: ${version}`);
  }
  const parts = version.split(".").map(Number);
  for (const part of parts) {
    if (!Number.isInteger(part) || part < 0 || part > MAX_WINDOWS_PE_PART) {
      throw new Error(
        `Version ${version} exceeds Windows PE 16-bit component limit (${MAX_WINDOWS_PE_PART}); bump major/minor version in package.json/mygo.json first.`,
      );
    }
  }
  return version;
}

export function nextReleaseVersion({ currentVersion, releases = [], tagVersions = [], headSha = "" }) {
  const publishedOnly = releases
    .filter((r) => r && !r.draft)
    .map((release) => versionFromTag(release.tag_name))
    .filter(Boolean);
  const highestPublished = publishedOnly.reduce(
    (best, item) => (!best || compareVersions(item, best) > 0 ? item : best),
    null,
  );
  if (headSha) {
    const matchingDrafts = releases.filter(
      (r) => r?.draft && r.target_commitish === headSha && versionFromTag(r.tag_name),
    );
    if (matchingDrafts.length === 1) {
      const draftVer = versionFromTag(matchingDrafts[0].tag_name);
      if (draftVer && (!highestPublished || compareVersions(draftVer, highestPublished) > 0)) {
        return assertValidWindowsPEVersion(draftVer);
      }
    }
  }

  const taken = [
    ...releases.map((release) => versionFromTag(release?.tag_name)),
    ...tagVersions.map((tag) => versionFromTag(tag)),
  ].filter(Boolean);
  const highestTaken = taken.reduce(
    (best, item) => (!best || compareVersions(item, best) > 0 ? item : best),
    null,
  );
  const normalizedCurrent = versionFromTag(currentVersion);
  let version;
  if (!highestTaken) {
    version = normalizedCurrent || "1.0.1";
  } else if (normalizedCurrent && compareVersions(normalizedCurrent, highestTaken) > 0) {
    version = normalizedCurrent;
  } else {
    version = bumpPatch(highestTaken);
  }
  const used = new Set(taken);
  while (used.has(version)) version = bumpPatch(version);
  return assertValidWindowsPEVersion(version);
}

export function readPackageVersion(root = ROOT) {
  const pkgPath = resolve(root, "package.json");
  if (existsSync(pkgPath)) {
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
    if (typeof pkg.version === "string" && /^\d+\.\d+\.\d+$/.test(pkg.version)) return pkg.version;
  }
  const mygoPath = resolve(root, "mygo.json");
  if (existsSync(mygoPath)) {
    const mygo = JSON.parse(readFileSync(mygoPath, "utf8"));
    if (typeof mygo.version === "string" && /^\d+\.\d+\.\d+$/.test(mygo.version)) return mygo.version;
  }
  throw new Error("package.json / mygo.json is missing a valid version");
}

export function setPackageJsonVersion(text, version) {
  const pkg = JSON.parse(text);
  pkg.version = version;
  return `${JSON.stringify(pkg, null, 2)}\n`;
}

export function setMyGoJsonVersion(text, version) {
  const mygo = JSON.parse(text);
  mygo.version = version;
  return `${JSON.stringify(mygo, null, 2)}\n`;
}

export function applyReleasePlanVersion() {
  const envVer = process.env.BLOCK_RELEASE_VERSION?.trim();
  if (envVer && /^\d+\.\d+\.\d+$/.test(envVer)) {
    syncVersionFiles(ROOT, envVer);
    return envVer;
  }
  const planPath = resolve(ROOT, "release-plan.json");
  let version = readPackageVersion(ROOT);
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
  const pkgPath = resolve(root, "package.json");
  if (existsSync(pkgPath)) {
    writeFileSync(pkgPath, setPackageJsonVersion(readFileSync(pkgPath, "utf8"), version));
  }
  const mygoPath = resolve(root, "mygo.json");
  if (existsSync(mygoPath)) {
    writeFileSync(mygoPath, setMyGoJsonVersion(readFileSync(mygoPath, "utf8"), version));
  }
}

function sleepSync(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

export function fetchReleasePage(repo = REPO, page = 1) {
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      const batch = JSON.parse(gh(["api", `repos/${repo}/releases?per_page=100&page=${page}`]));
      if (!Array.isArray(batch)) {
        throw new Error(`Unexpected GitHub releases page ${page} response for ${repo}`);
      }
      return batch;
    } catch (err) {
      lastError = err;
      if (attempt < 3) sleepSync(1500 * attempt);
    }
  }
  throw lastError;
}

export function getReleases({ repo = REPO, includeBody = false } = {}) {
  const out = [];
  for (let page = 1; ; page += 1) {
    const batch = fetchReleasePage(repo, page);
    if (batch.length === 0) break;
    if (includeBody) {
      out.push(...batch);
    } else {
      for (const r of batch) {
        out.push({
          id: r?.id,
          tag_name: r?.tag_name,
          target_commitish: r?.target_commitish,
          draft: Boolean(r?.draft),
          prerelease: Boolean(r?.prerelease),
          published_at: r?.published_at,
          created_at: r?.created_at,
        });
      }
    }
    if (batch.length < 100) break;
  }
  return out;
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

function warnGeneration(notes) {
  if (!notes?.generationFailed) return;
  const reason = String(notes.reason || "没有可用的模型").replace(/[\r\n%]+/g, " ").replace(/::/g, " ").slice(0, 480);
  const text = `AI 没有生成更新说明（${reason}）。本次停止发版，不会用拼凑文案。`;
  console.log(`::warning title=release-notes::${text}`);
  summary(text);
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

function preflightPath() {
  const custom = process.env.BLOCK_PREFLIGHT_PATH?.trim();
  return custom ? resolve(custom) : resolve(ROOT, "release-preflight.json");
}

function loadPreflight(headSha, force, channel) {
  if (process.env.BLOCK_USE_PREFLIGHT !== "1") return null;
  const path = preflightPath();
  if (!existsSync(path)) {
    throw new Error(`BLOCK_USE_PREFLIGHT=1 is set but preflight snapshot is missing at ${path}`);
  }
  const data = JSON.parse(readFileSync(path, "utf8"));
  const valid = data
    && data.headSha === headSha
    && typeof data.force === "boolean"
    && data.force === Boolean(force)
    && normalizeReleaseChannel(data.channel) === channel
    && typeof data.currentVersion === "string"
    && /^\d+\.\d+\.\d+$/.test(data.currentVersion)
    && typeof data.version === "string"
    && Array.isArray(data.commits);
  if (!valid) {
    throw new Error(`Invalid or mismatched preflight snapshot at ${path}`);
  }
  assertValidWindowsPEVersion(data.version);
  return data;
}

function feedHasReleaseTag(tag, feedRelPath = "updates/feed.json") {
  if (!tag) return true;
  let raw = null;
  for (const ref of ["origin/main", "HEAD"]) {
    try {
      raw = execFileSync("git", ["show", `${ref}:${feedRelPath}`], {
        cwd: ROOT,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      });
      break;
    } catch {}
  }
  if (raw === null) {
    try {
      const data = JSON.parse(gh(["api", `repos/${REPO}/contents/${feedRelPath}?ref=main`]));
      raw = Buffer.from(String(data.content).replaceAll("\n", ""), "base64").toString("utf8");
    } catch (err) {
      if (/\b404\b|Not Found/i.test(String(err?.stderr || err?.message || ""))) return false;
      throw new Error(`Failed to read ${feedRelPath} while verifying ${tag}: ${err?.message || err}`);
    }
  }
  const parsed = JSON.parse(raw);
  if (parsed?.latest_tag !== tag) return false;
  const entry = Array.isArray(parsed?.releases) ? parsed.releases.find((r) => r?.tag === tag) : null;
  if (!entry) return false;
  const assets = Array.isArray(entry.assets) ? entry.assets : [];
  if (assets.length === 0) return false;
  if (assets.some((a) => typeof a?.sha256 !== "string" || !/^[0-9a-f]{64}$/i.test(a.sha256))) return false;
  return true;
}

function writeSkipPlan({ currentVersion, tag, headSha, channel, extraPlan = {}, extraOutputs = {} }) {
  const plan = { shouldBuild: false, publish: false, channel, version: currentVersion, tag, commit: headSha, ...extraPlan };
  writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
  writeOutputs({
    should_build: outputFlag(false),
    publish: outputFlag(false),
    channel,
    version: currentVersion,
    release_tag: tag,
    ...extraOutputs,
  });
}

async function main() {
  const preflightOnly = process.argv.includes("--preflight");
  const headSha = process.env.SOURCE_SHA || process.env.GITHUB_SHA
    || execFileSync("git", ["rev-parse", "HEAD"], { cwd: ROOT, encoding: "utf8" }).trim();
  const force = process.env.FORCE === "true";
  const publishInput = process.env.PUBLISH !== "false";
  const channel = normalizeReleaseChannel(process.env.RELEASE_CHANNEL);
  const onMain = !process.env.GITHUB_REF || process.env.GITHUB_REF === "refs/heads/main";

  const cached = preflightOnly ? null : loadPreflight(headSha, force, channel);
  let currentVersion;
  let version;
  let previous;
  let rangeSpec;
  let commits;

  if (cached) {
    ({ currentVersion, version, previous, rangeSpec, commits } = cached);
    rmSync(preflightPath(), { force: true });
  } else {
    currentVersion = readPackageVersion(ROOT);
    const releases = getReleases();
    const tagVersions = listGitTagVersions(ROOT);
    version = nextReleaseVersion({ currentVersion, releases, tagVersions, headSha });
    const eligiblePrevious = releases
      .filter((r) => !r.draft && (channel === "nightly" || !r.prerelease) && versionFromTag(r.tag_name))
      .sort((a, b) => compareVersions(versionFromTag(a.tag_name), versionFromTag(b.tag_name)));
    previous = eligiblePrevious.at(-1) || null;
    const feedRelPath = channel === "nightly" ? "updates/nightly-feed.json" : "updates/feed.json";
    const previousMissingFeed = Boolean(previous && !feedHasReleaseTag(previous.tag_name, feedRelPath));

    const previousSha = resolveTagCommit(previous);
    const canSyncPostPublish = Boolean(onMain && publishInput);
    if (previousSha && previousSha === headSha && !force) {
      if (previousMissingFeed && canSyncPostPublish) {
        summary(`最新 Release ${previous.tag_name} 已发布但 ${feedRelPath} 尚未回写，触发发布后同步补偿。`);
        writeSkipPlan({
          currentVersion,
          tag: previous.tag_name,
          headSha,
          channel,
          extraOutputs: {
            ...(preflightOnly ? { needs_release: outputFlag(false) } : {}),
            sync_only_tag: previous.tag_name,
          },
        });
        return;
      }
      summary(`当前 HEAD (${headSha.slice(0, 7)}) 与最新 Release ${previous.tag_name} 完全一致，跳过构建。`);
      writeSkipPlan({
        currentVersion,
        tag: previous.tag_name,
        headSha,
        channel,
        extraOutputs: preflightOnly ? { needs_release: outputFlag(false) } : {},
      });
      return;
    }

    rangeSpec = null;
    if (!(previousSha && previousSha === headSha)) {
      const resolved = resolveReleaseRange({ previous, previousSha, headSha });
      rangeSpec = resolved.range;
      if (resolved.note) summary(resolved.note);
    }

    commits = rangeSpec ? listCommits(ROOT, rangeSpec) : [];
    if (rangeSpec) summary(`更新说明区间 ${rangeSpec}，共 ${commits.length} 条提交。`);

    const userCommits = commits.filter((commit) => !isExcludedCommit(commit));
    if (previous && userCommits.length === 0 && !force) {
      if (previousMissingFeed && canSyncPostPublish) {
        summary(`无面向用户的新提交，但 ${previous.tag_name} 尚未写入 ${feedRelPath}，触发发布后同步补偿。`);
        writeSkipPlan({
          currentVersion,
          tag: previous.tag_name,
          headSha,
          channel,
          extraOutputs: {
            ...(preflightOnly ? { needs_release: outputFlag(false) } : {}),
            sync_only_tag: previous.tag_name,
          },
        });
        return;
      }
      summary(`自 ${previous.tag_name} 以来没有面向用户的新提交（共扫描 ${commits.length} 条提交），跳过自动发布。`);
      writeSkipPlan({
        currentVersion,
        tag: previous.tag_name,
        headSha,
        channel,
        extraOutputs: preflightOnly ? { needs_release: outputFlag(false) } : {},
      });
      return;
    }

    if (preflightOnly) {
      const target = preflightPath();
      mkdirSync(dirname(target), { recursive: true });
      writeFileSync(
        target,
        `${JSON.stringify({ headSha, force, channel, currentVersion, version, previous, rangeSpec, commits }, null, 2)}\n`,
      );
      writeOutputs({
        needs_release: outputFlag(true),
        channel,
        version,
        release_tag: `v${version}`,
      });
      summary(`预检通过：准备为 Block v${version} (${channel}) 生成更新说明（面向用户提交 ${userCommits.length} 条）。`);
      return;
    }
  }

  const autoBump = version !== currentVersion;
  const userCommits = commits.filter((commit) => !isExcludedCommit(commit));
  let notes = await buildReleaseNotes({ version, commits });
  warnGeneration(notes);

  if (previous && notes.skip && !force) {
    summary(`自 ${previous.tag_name} 以来${notes.reason}（共扫描 ${commits.length} 条提交），跳过自动发布。`);
    const plan = {
      shouldBuild: false,
      publish: false,
      channel,
      version: currentVersion,
      tag: previous.tag_name,
      commit: headSha,
      ...(notes.generationFailed ? { notesSource: "fallback", generationFailed: true, generationFailure: notes.reason || "" } : {}),
    };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), channel, version: currentVersion, release_tag: previous.tag_name });
    return;
  }

  if (!notes.markdown) {
    if (userCommits.length === 0 && force) {
      notes = { markdown: forcedReleaseNotes(), source: "forced" };
      summary("没有面向用户的新提交，因 force=true 使用手动发布说明。");
    } else {
      summary(`更新说明没有覆盖区间里的 ${commits.length} 条提交（${notes.reason || "空"}），停止发布。`);
      if (notes.generationFailed) process.exitCode = 1;
      const plan = {
        shouldBuild: false,
        publish: false,
        channel,
        version,
        tag: `v${version}`,
        commit: headSha,
        ...(notes.generationFailed ? { notesSource: "fallback", generationFailed: true, generationFailure: notes.reason || "" } : {}),
      };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), channel, version, release_tag: `v${version}` });
      return;
    }
  }
  const markdown = notes.markdown;
  const tag = `v${version}`;
  const shouldPublish = onMain && publishInput;
  const plan = {
    shouldBuild: true,
    publish: shouldPublish,
    channel,
    autoBump,
    version,
    tag,
    commit: headSha,
    previousTag: previous?.tag_name || null,
    notesSource: notes.source || "fallback",
    notesModel: notes.generationFailed ? "" : (notes.model || ""),
    generationFailed: Boolean(notes.generationFailed),
    generationFailure: notes.generationFailed ? (notes.reason || "") : "",
    releaseNotes: markdown,
  };
  writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
  writeOutputs({
    should_build: outputFlag(true),
    publish: outputFlag(shouldPublish),
    channel,
    version,
    release_tag: tag,
    auto_bump: autoBump,
  });
  const notesLabel = notes.source === "polished" && notes.model
    ? `AI 模型 ${notes.model}`
    : (notes.source || "none");
  summary(`准备发布 Block ${tag} (${channel}, autoBump=${autoBump}, notes=${notesLabel})`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await main();
}
