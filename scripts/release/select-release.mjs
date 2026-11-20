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

export function nextReleaseVersion({ currentVersion, releases = [], tagVersions = [] }) {
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
  return version;
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

export function getReleases() {
  try {
    return JSON.parse(gh(["api", `repos/${REPO}/releases?per_page=100`]));
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

function warnGeneration(notes) {
  if (!notes?.generationFailed) return;
  const reason = String(notes.reason || "grok-4.7 没有生成更新说明").replace(/[\r\n%]+/g, " ").replace(/::/g, " ").slice(0, 240);
  const text = `grok-4.7 没有生成更新说明（${reason}）。本次发布说明不是模型结果。`;
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

async function main() {
  const headSha = process.env.SOURCE_SHA || process.env.GITHUB_SHA
    || execFileSync("git", ["rev-parse", "HEAD"], { cwd: ROOT, encoding: "utf8" }).trim();
  const force = process.env.FORCE === "true";
  const publishInput = process.env.PUBLISH !== "false";
  const onMain = !process.env.GITHUB_REF || process.env.GITHUB_REF === "refs/heads/main";

  const currentVersion = readPackageVersion(ROOT);
  const releases = getReleases();
  const tagVersions = listGitTagVersions(ROOT);
  const version = nextReleaseVersion({ currentVersion, releases, tagVersions });
  const stable = releases
    .filter((r) => !r.draft && !r.prerelease && versionFromTag(r.tag_name))
    .sort((a, b) => compareVersions(versionFromTag(a.tag_name), versionFromTag(b.tag_name)));
  const previous = stable.at(-1) || null;

  const previousSha = resolveTagCommit(previous);
  if (previousSha && previousSha === headSha && !force) {
    summary(`当前 HEAD (${headSha.slice(0, 7)}) 与最新 Release ${previous.tag_name} 完全一致，跳过构建。`);
    const plan = { shouldBuild: false, publish: false, version: currentVersion, tag: previous.tag_name, commit: headSha };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: currentVersion, release_tag: previous.tag_name });
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
      const plan = { shouldBuild: false, publish: false, version: currentVersion, tag: previous?.tag_name || "", commit: headSha };
      writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
      writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: currentVersion, release_tag: previous?.tag_name || "" });
      return;
    }
  }

  const autoBump = version !== currentVersion;
  const commits = rangeSpec ? listCommits(ROOT, rangeSpec) : [];
  if (rangeSpec) summary(`更新说明区间 ${rangeSpec}，共 ${commits.length} 条提交。`);
  let notes = await buildReleaseNotes({ version, commits });
  warnGeneration(notes);

  if (previous && notes.skip && !force) {
    summary(`自 ${previous.tag_name} 以来${notes.reason}（共扫描 ${commits.length} 条提交），跳过自动发布。`);
    const plan = {
      shouldBuild: false,
      publish: false,
      version: currentVersion,
      tag: previous.tag_name,
      commit: headSha,
      ...(notes.generationFailed ? { notesSource: "fallback", generationFailed: true, generationFailure: notes.reason || "" } : {}),
    };
    writeFileSync(resolve(ROOT, "release-plan.json"), `${JSON.stringify(plan, null, 2)}\n`);
    writeOutputs({ should_build: outputFlag(false), publish: outputFlag(false), version: currentVersion, release_tag: previous.tag_name });
    return;
  }

  if (!notes.markdown) {
    if (commits.length === 0 && force) {
      notes = { markdown: forcedReleaseNotes(), source: "forced" };
      summary("没有新提交，因 force=true 使用手动发布说明。");
    } else {
      summary(`更新说明没有覆盖区间里的 ${commits.length} 条提交（${notes.reason || "空"}），停止发布。`);
      const plan = {
        shouldBuild: false,
        publish: false,
        version,
        tag: `v${version}`,
        commit: headSha,
        ...(notes.generationFailed ? { notesSource: "fallback", generationFailed: true, generationFailure: notes.reason || "" } : {}),
      };
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
    notesModel: notes.generationFailed ? "" : (notes.model || ""),
    generationFailed: Boolean(notes.generationFailed),
    generationFailure: notes.generationFailed ? (notes.reason || "") : "",
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
  const notesLabel = notes.source === "polished" && notes.model
    ? `grok-4.7 响应模型 ${notes.model}`
    : notes.generationFailed
      ? "本地规则，grok-4.7 未生成"
      : (notes.source || "fallback");
  summary(`准备发布 Block ${tag}（autoBump=${autoBump}, notes=${notesLabel}）`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await main();
}
