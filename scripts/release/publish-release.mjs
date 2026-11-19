import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { appendFileSync, existsSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { assertReleaseAsset, RELEASE_ASSETS } from "./allowed-assets.mjs";
import {
  getReleases,
  gh,
  REPO,
  ROOT,
  setMyGoJsonVersion,
  setPackageJsonVersion,
} from "./select-release.mjs";
import { fetchGithubReleaseByTag, syncFeedFromGithub } from "./update-feed.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");

function readRemoteFile(ref, path) {
  const data = JSON.parse(gh(["api", `repos/${REPO}/contents/${path}?ref=${encodeURIComponent(ref)}`]));
  return Buffer.from(String(data.content).replaceAll("\n", ""), "base64").toString("utf8");
}

function writebackVersionCommit(version, tag, { autoBump = true, feedJson } = {}) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const head = JSON.parse(gh(["api", `repos/${REPO}/git/ref/heads/main`])).object.sha;
    const commit = JSON.parse(gh(["api", `repos/${REPO}/git/commits/${head}`]));
    const files = {};
    if (autoBump) {
      files["package.json"] = setPackageJsonVersion(readRemoteFile(head, "package.json"), version);
      files["mygo.json"] = setMyGoJsonVersion(readRemoteFile(head, "mygo.json"), version);
    }
    if (typeof feedJson === "string") {
      files["updates/feed.json"] = feedJson;
    }
    if (Object.keys(files).length === 0) return;
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
  assert.equal(tag, `v${version}`);
  assert.notEqual(tag, "v0.1.0");

  for (const name of readdirSync(RELEASE_DIR)) {
    if (!RELEASE_ASSETS.includes(name)) {
      console.log(`Skipping ${name}`);
      rmSync(join(RELEASE_DIR, name), { force: true });
    }
  }
  const assets = RELEASE_ASSETS.filter((name) => {
    assertReleaseAsset(name);
    return true;
  });
  const assetPaths = assets.map((name) => {
    const p = join(RELEASE_DIR, name);
    assert.ok(existsSync(p), `Missing release asset: ${p}`);
    return p;
  });
  const fallbackHashes = Object.fromEntries(
    assets.map((name) => [name, createHash("sha256").update(readFileSync(join(RELEASE_DIR, name))).digest("hex")]),
  );

  const existing = getReleases().find((r) => r.tag_name === tag);
  const title = `Block ${version}`;
  let publishedRelease;

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
    publishedRelease = fetchGithubReleaseByTag(tag);
  } else {
    publishedRelease = fetchGithubReleaseByTag(tag);
    assert.ok(!publishedRelease.draft, `Release ${tag} is an existing draft and requires reconciliation`);
    for (const name of assets) {
      const matches = (publishedRelease.assets ?? []).filter((asset) => asset.name === name);
      assert.ok(
        matches.length === 1 && matches[0].digest?.toLowerCase() === `sha256:${fallbackHashes[name]}`,
        `Release ${tag} already exists with a different or unverifiable ${name}. Publish corrections under a new version.`,
      );
    }
  }

  const { json: feedJson } = syncFeedFromGithub({
    tagRelease: publishedRelease,
    fallbackHashes,
  });

  writebackVersionCommit(version, tag, { autoBump: Boolean(autoBump), feedJson });

  const summary = `已发布 ${title} (${tag})\n\n${releaseNotes}\n`;
  console.log(summary);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${summary}\n`);
}

if (import.meta.main) {
  await publishRelease();
}
