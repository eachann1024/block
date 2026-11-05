import assert from "node:assert/strict";
import { appendFileSync, existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { INSTALLERS } from "./checksum.mjs";
import {
  getReleases,
  gh,
  REPO,
  ROOT,
  setCargoLockVersion,
  setCargoTomlVersion,
  setPackageJsonVersion,
} from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const ASSETS = [...INSTALLERS, "SHA256SUMS.txt"];

function readRemoteFile(ref, path) {
  const data = JSON.parse(gh(["api", `repos/${REPO}/contents/${path}?ref=${encodeURIComponent(ref)}`]));
  return Buffer.from(String(data.content).replaceAll("\n", ""), "base64").toString("utf8");
}

function writebackVersionCommit(version, tag) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const head = JSON.parse(gh(["api", `repos/${REPO}/git/ref/heads/main`])).object.sha;
    const commit = JSON.parse(gh(["api", `repos/${REPO}/git/commits/${head}`]));
    const files = {
      "Cargo.toml": setCargoTomlVersion(readRemoteFile(head, "Cargo.toml"), version),
      "Cargo.lock": setCargoLockVersion(readRemoteFile(head, "Cargo.lock"), version),
      "package.json": setPackageJsonVersion(readRemoteFile(head, "package.json"), version),
    };
    const tree = JSON.parse(gh(["api", "--method", "POST", `repos/${REPO}/git/trees`, "--input", "-"], JSON.stringify({
      base_tree: commit.tree.sha,
      tree: Object.entries(files).map(([path, content]) => ({ path, mode: "100644", type: "blob", content })),
    })));
    if (tree.sha === commit.tree.sha) return;
    const next = JSON.parse(gh(["api", "--method", "POST", `repos/${REPO}/git/commits`, "--input", "-"], JSON.stringify({
      message: `chore(release): ${tag}`,
      tree: tree.sha,
      parents: [head],
    })));
    try {
      gh(["api", "--method", "PATCH", `repos/${REPO}/git/refs/heads/main`, "--input", "-"], JSON.stringify({
        sha: next.sha,
        force: false,
      }));
      return;
    } catch (err) {
      if (attempt === 2 || !/409|422/.test(String(err.stderr || err.message))) throw err;
    }
  }
}

export async function publishRelease() {
  const planPath = resolve(ROOT, "release-plan.json");
  assert.ok(existsSync(planPath), "release-plan.json not found");
  const plan = JSON.parse(readFileSync(planPath, "utf8"));
  if (!plan.shouldBuild || !plan.publish) {
    console.log("Publish skipped according to release-plan.json");
    return;
  }

  const { version, tag, commit, releaseNotes, autoBump } = plan;
  assert.ok(version && tag && commit && releaseNotes, "Incomplete release-plan.json");

  const assetPaths = ASSETS.map((name) => {
    const p = join(RELEASE_DIR, name);
    assert.ok(existsSync(p), `Missing release asset: ${p}`);
    return p;
  });

  const existing = getReleases().find((r) => r.tag_name === tag);
  const title = `Block ${version}`;

  if (!existing) {
    gh([
      "release",
      "create",
      tag,
      ...assetPaths,
      "--repo",
      REPO,
      "--target",
      commit,
      "--title",
      title,
      "--notes",
      releaseNotes,
      "--latest",
    ]);
  } else {
    gh(["release", "edit", tag, "--repo", REPO, "--title", title, "--notes", releaseNotes, "--draft=false", "--latest"]);
    gh(["release", "upload", tag, ...assetPaths, "--repo", REPO, "--clobber"]);
  }

  if (autoBump) {
    writebackVersionCommit(version, tag);
  }

  const summary = `已发布 ${title} (${tag})\n\n${releaseNotes}\n`;
  console.log(summary);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${summary}\n`);
}

if (import.meta.main) {
  await publishRelease();
}
