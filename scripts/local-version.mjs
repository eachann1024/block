import { spawnSync } from "node:child_process";

const tagPattern = /^v?(\d+)\.(\d+)\.(\d+)(?:-[0-9a-f]{7,40})?$/;
const cache = new Map();

function git(root, args, timeout = 5000) {
  const result = spawnSync("git", args, { cwd: root, encoding: "utf8", timeout, stdio: ["ignore", "pipe", "ignore"] });
  return result.status === 0 ? result.stdout.trim() : null;
}

function parse(version) {
  const match = tagPattern.exec(String(version || "").trim());
  return match ? match.slice(1, 4).map(Number) : null;
}

function compare(left, right) {
  for (let i = 0; i < 3; i += 1) {
    if (left[i] !== right[i]) return left[i] - right[i];
  }
  return 0;
}

function resolve(root, baseVersion) {
  const base = parse(baseVersion);
  if (!base || process.env.CI || process.env.GITHUB_ACTIONS) return { version: baseVersion };
  if (git(root, ["rev-parse", "--is-inside-work-tree"]) !== "true") return { version: baseVersion };

  git(root, ["fetch", "--quiet", "--tags", "origin"], 10000);
  let highestTag = null;
  let highest = null;
  for (const tag of (git(root, ["tag", "--list", "v*"]) || "").split("\n")) {
    const parsed = parse(tag);
    if (parsed && (!highest || compare(parsed, highest) > 0)) {
      highest = parsed;
      highestTag = tag.trim();
    }
  }
  if (!highest) return { version: baseVersion };

  const log = git(root, ["log", "--no-merges", "--format=%s", `${highestTag}..HEAD`]);
  const pending = log === null ? 1 : log.split("\n").filter((line) => line && !/^chore\(release\)/.test(line)).length;
  let next = pending > 0 ? [highest[0], highest[1], highest[2] + 1] : highest;
  if (compare(base, next) > 0) next = base;
  return { version: next.join("."), highestTag, pending };
}

// resolveLocalBuildVersion predicts the version CI will publish for the current HEAD so a local
// install never reports an older version than the latest release. It only injects the value at
// build time and never rewrites package.json / mygo.json; CI builds keep the synced file version.
export function resolveLocalBuildVersion(root, baseVersion) {
  const key = `${root}\0${baseVersion}`;
  if (!cache.has(key)) {
    const info = resolve(root, baseVersion);
    if (info.version !== baseVersion) {
      process.stderr.write(
        `build version ${info.version} (package.json ${baseVersion}, latest ${info.highestTag} + ${info.pending} unreleased commits)\n`,
      );
    }
    cache.set(key, info.version);
  }
  return cache.get(key);
}
