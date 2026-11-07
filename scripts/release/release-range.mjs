import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

function isAncestor(sha, head) {
  try {
    execFileSync("git", ["merge-base", "--is-ancestor", sha, head], { cwd: ROOT, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function releaseCommitOnHistory(tag, head) {
  const needle = `chore(release): ${tag}`;
  try {
    const sha = execFileSync(
      "git",
      ["log", head, "-1", "--fixed-strings", `--grep=${needle}`, "--format=%H"],
      { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    ).trim();
    if (/^[0-9a-f]{40}$/i.test(sha) && isAncestor(sha, head)) return { sha, needle };
  } catch {}
  return null;
}

export function resolveReleaseRange({ previous, previousSha, headSha }) {
  if (!previous) return { range: headSha, note: "" };
  if (previousSha && isAncestor(previousSha, headSha)) {
    return { range: `${previousSha}..${headSha}`, note: "" };
  }
  const found = releaseCommitOnHistory(previous.tag_name, headSha);
  if (found) {
    return {
      range: `${found.sha}..${headSha}`,
      note: `上一个 Release ${previous.tag_name} 的标签提交不在当前历史上，改用 ${found.needle}（${found.sha.slice(0, 7)}）作为区间起点。`,
    };
  }
  throw new Error(
    `找不到从 ${previous.tag_name} 到当前提交的完整区间（标签提交不在历史上，也没有 chore(release): ${previous.tag_name}）。已停止，避免只用最近若干条提交。`,
  );
}
