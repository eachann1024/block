import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { appendFileSync, createReadStream, existsSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { assertReleaseAsset, RELEASE_ASSETS } from "./allowed-assets.mjs";
import {
  compareVersions,
  getReleases,
  gh,
  normalizeReleaseChannel,
  REPO,
  ROOT,
  setMyGoJsonVersion,
  setPackageJsonVersion,
  versionFromTag,
} from "./select-release.mjs";
import { fetchGithubReleaseByTag, syncBothFeedsFromGithub, syncFeedFromGithub } from "./update-feed.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");

function isNotFoundError(err) {
  const detail = String(err?.stderr || err?.message || "");
  return /\b404\b|Not Found/i.test(detail);
}

function readFileAtRef(ref, path, { allowMissing = false } = {}) {
  try {
    return execFileSync("git", ["show", `${ref}:${path}`], {
      cwd: ROOT,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {}
  try {
    const data = JSON.parse(gh(["api", `repos/${REPO}/contents/${path}?ref=${encodeURIComponent(ref)}`]));
    return Buffer.from(String(data.content).replaceAll("\n", ""), "base64").toString("utf8");
  } catch (err) {
    if (allowMissing && isNotFoundError(err)) return null;
    throw err;
  }
}

function sha256File(filePath) {
  return new Promise((resolveHash, rejectHash) => {
    const hash = createHash("sha256");
    const stream = createReadStream(filePath);
    stream.on("data", (chunk) => hash.update(chunk));
    stream.on("error", rejectHash);
    stream.on("end", () => resolveHash(hash.digest("hex")));
  });
}

export function sha256RemoteAsset(assetId) {
  return new Promise((resolveHash, rejectHash) => {
    const hash = createHash("sha256");
    const child = spawn(
      "gh",
      ["api", "-H", "Accept: application/octet-stream", `repos/${REPO}/releases/assets/${assetId}`],
      { cwd: ROOT, stdio: ["ignore", "pipe", "pipe"] },
    );
    let stderr = "";
    child.stdout.on("data", (chunk) => hash.update(chunk));
    child.stderr.on("data", (chunk) => { stderr += chunk.toString("utf8"); });
    child.on("error", rejectHash);
    child.on("close", (code) => {
      if (code !== 0) rejectHash(new Error(`gh api asset #${assetId} exited ${code}: ${stderr.trim()}`));
      else resolveHash(hash.digest("hex"));
    });
  });
}

export function resolveRemoteTagCommit(tag) {
  let refData;
  try {
    refData = JSON.parse(gh(["api", `repos/${REPO}/git/ref/tags/${encodeURIComponent(tag)}`]));
  } catch (err) {
    if (isNotFoundError(err)) return null;
    throw err;
  }
  let obj = refData?.object;
  for (let depth = 0; depth < 5 && obj?.type === "tag" && obj?.sha; depth += 1) {
    const tagObj = JSON.parse(gh(["api", `repos/${REPO}/git/tags/${obj.sha}`]));
    obj = tagObj?.object;
  }
  return obj?.type === "commit" && /^[0-9a-f]{40}$/i.test(obj.sha || "") ? obj.sha : null;
}

function ensureTagRefMatches(tag, commit) {
  const existingCommit = resolveRemoteTagCommit(tag);
  if (existingCommit) {
    assert.equal(
      existingCommit,
      commit,
      `Tag ${tag} already points to ${existingCommit}, refusing to publish binaries built from ${commit}.`,
    );
    return;
  }
  try {
    gh(
      ["api", "--method", "POST", `repos/${REPO}/git/refs`, "--input", "-"],
      JSON.stringify({ ref: `refs/tags/${tag}`, sha: commit }),
    );
  } catch (err) {
    const detail = String(err?.stderr || err?.message || "");
    if (!/\b422\b|Reference already exists/i.test(detail)) throw err;
  }
  const verifiedCommit = resolveRemoteTagCommit(tag);
  assert.equal(
    verifiedCommit,
    commit,
    `Tag ${tag} resolved to ${verifiedCommit || "missing"} instead of planned commit ${commit}.`,
  );
}

export function fetchReleaseById(id) {
  return JSON.parse(gh(["api", `repos/${REPO}/releases/${id}`]));
}

function uploadReleaseAsset(releaseId, name, filePath) {
  const url = `https://uploads.github.com/repos/${REPO}/releases/${releaseId}/assets?name=${encodeURIComponent(name)}`;
  return JSON.parse(
    gh([
      "api",
      "--method",
      "POST",
      "-H",
      "Content-Type: application/octet-stream",
      "--input",
      filePath,
      url,
    ]),
  );
}

export async function verifyReleaseAssets(release, assets, assetSizes, fallbackHashes, tag) {
  const remoteAssets = release?.assets ?? [];
  assert.equal(
    remoteAssets.length,
    assets.length,
    `Release ${tag} has ${remoteAssets.length} assets, expected exactly ${assets.length}.`,
  );
  for (const name of assets) {
    const matches = remoteAssets.filter((asset) => asset?.name === name);
    assert.equal(matches.length, 1, `Release ${tag} must have exactly one asset named ${name}.`);
    const asset = matches[0];
    assert.equal(asset.state, "uploaded", `Release ${tag} asset ${name} is not uploaded (state=${asset.state}).`);
    assert.equal(asset.size, assetSizes[name], `Release ${tag} asset ${name} size mismatch.`);
    const expectedHex = fallbackHashes[name];
    if (asset.digest) {
      assert.equal(
        asset.digest.toLowerCase(),
        `sha256:${expectedHex}`,
        `Release ${tag} asset ${name} digest mismatch.`,
      );
    } else {
      assert.ok(asset.id, `Release ${tag} asset ${name} lacks both digest and asset id.`);
      const downloadedHex = await sha256RemoteAsset(asset.id);
      assert.equal(
        downloadedHex.toLowerCase(),
        expectedHex,
        `Release ${tag} asset ${name} downloaded content SHA-256 mismatch.`,
      );
    }
  }
}

function sleepSync(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function parseJsonVersion(text) {
  try {
    const parsed = JSON.parse(text);
    return versionFromTag(parsed?.version);
  } catch {
    return null;
  }
}

function feedContentSignature(jsonText) {
  if (!jsonText) return null;
  try {
    const parsed = JSON.parse(jsonText);
    const { updated_at: _ignored, ...rest } = parsed || {};
    return JSON.stringify(rest);
  } catch {
    return null;
  }
}

function feedLatestVersion(jsonText) {
  if (!jsonText) return null;
  try {
    const parsed = JSON.parse(jsonText);
    return versionFromTag(parsed?.latest_version || parsed?.latest_tag);
  } catch {
    return null;
  }
}

export function writebackVersionCommit(version, tag, { buildFeeds, buildFeed, sourceCommit = "", subject = "" }) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const feeds = buildFeeds ? buildFeeds() : { stable: buildFeed() };
    const head = JSON.parse(gh(["api", `repos/${REPO}/git/ref/heads/main`])).object.sha;
    const commit = JSON.parse(gh(["api", `repos/${REPO}/git/commits/${head}`]));
    const files = {};

    const pkgText = readFileAtRef(head, "package.json");
    const pkgVer = parseJsonVersion(pkgText);
    if (!pkgVer || compareVersions(version, pkgVer) > 0) {
      files["package.json"] = setPackageJsonVersion(pkgText, version);
    }
    const mygoText = readFileAtRef(head, "mygo.json");
    const mygoVer = parseJsonVersion(mygoText);
    if (!mygoVer || compareVersions(version, mygoVer) > 0) {
      files["mygo.json"] = setMyGoJsonVersion(mygoText, version);
    }

    for (const [feedFile, feedState] of [
      ["updates/feed.json", feeds?.stable],
      ["updates/nightly-feed.json", feeds?.nightly],
    ]) {
      if (!feedState?.json) continue;
      const headFeedText = readFileAtRef(head, feedFile, { allowMissing: true });
      const headFeedVer = feedLatestVersion(headFeedText);
      const nextFeedVer = versionFromTag(feedState.feed?.latest_version || feedState.feed?.latest_tag);
      const shouldWriteFeed = (!headFeedVer || !nextFeedVer || compareVersions(nextFeedVer, headFeedVer) >= 0)
        && feedContentSignature(headFeedText) !== feedContentSignature(feedState.json);
      if (shouldWriteFeed) {
        files[feedFile] = feedState.json;
      }
    }

    if (Object.keys(files).length === 0) return;
    const tree = JSON.parse(gh(["api", "--method", "POST", `repos/${REPO}/git/trees`, "--input", "-"], JSON.stringify({
      base_tree: commit.tree.sha,
      tree: Object.entries(files).map(([path, content]) => ({ path, mode: "100644", type: "blob", content })),
    })));
    if (tree.sha === commit.tree.sha) return;
    const trailers = [];
    if (/^[0-9a-f]{40}$/i.test(sourceCommit)) {
      trailers.push(`Release-Source-SHA: ${sourceCommit}`);
    }
    trailers.push("[skip ci]");
    const title = subject || `chore(release): ${tag}`;
    const next = JSON.parse(gh(["api", "--method", "POST", `repos/${REPO}/git/commits`, "--input", "-"], JSON.stringify({
      message: `${title}\n\n${trailers.join("\n")}`,
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
      if (attempt === 4 || !/409|422/.test(String(err.stderr || err.message))) throw err;
      sleepSync(500 * (attempt + 1));
    }
  }
}

export function buildFeedsForTag(tag, fallbackHashes) {
  // 每次重试都重新读取远端发布状态，避免并发升稳后用过期的 prerelease 标记回写清单。
  const fresh = fetchGithubReleaseByTag(tag);
  assert.ok(fresh && !fresh.draft, `Published release ${tag} not found`);
  return fresh.prerelease
    ? { nightly: syncFeedFromGithub({ includePrerelease: true, write: false, tagRelease: fresh, fallbackHashes }) }
    : syncBothFeedsFromGithub({ write: false, tagRelease: fresh, fallbackHashes });
}

export async function syncPublishedRelease(tag) {
  const version = versionFromTag(tag);
  assert.ok(version, `Invalid release tag for sync-only: ${tag}`);
  const publishedRelease = fetchGithubReleaseByTag(tag);
  assert.ok(publishedRelease && !publishedRelease.draft, `Published release ${tag} not found`);
  const isNightly = Boolean(publishedRelease.prerelease);
  const fallbackHashes = {};
  for (const asset of publishedRelease.assets ?? []) {
    if (!asset?.name || !RELEASE_ASSETS.includes(asset.name)) continue;
    if (asset.digest && /^sha256:[0-9a-f]{64}$/i.test(asset.digest.trim())) continue;
    if (asset.id) {
      fallbackHashes[asset.name] = await sha256RemoteAsset(asset.id);
    }
  }
  const sourceCommit = resolveRemoteTagCommit(tag) || "";
  writebackVersionCommit(version, tag, {
    sourceCommit,
    buildFeeds: () => buildFeedsForTag(tag, fallbackHashes),
  });
  if (process.env.GITHUB_OUTPUT) {
    appendFileSync(process.env.GITHUB_OUTPUT, `is_stable=${isNightly ? "no" : "yes"}\n`);
  }
  const line = `已补齐 ${tag} (${isNightly ? "nightly" : "stable"}) 的版本与 feed 回写。`;
  console.log(line);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${line}\n`);
}

export async function publishRelease({ skipWriteback = false } = {}) {
  const planPath = resolve(ROOT, "release-plan.json");
  assert.ok(existsSync(planPath), "release-plan.json not found");
  const plan = JSON.parse(readFileSync(planPath, "utf8"));
  if (!plan.shouldBuild || !plan.publish) {
    console.log("Publish skipped according to release-plan.json");
    return;
  }

  const { version, tag, commit, releaseNotes } = plan;
  const channel = normalizeReleaseChannel(plan.channel);
  const isNightly = channel === "nightly";
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
  const assetPathMap = {};
  const assetSizes = {};
  for (const name of assets) {
    const p = join(RELEASE_DIR, name);
    assert.ok(existsSync(p), `Missing release asset: ${p}`);
    assetPathMap[name] = p;
    assetSizes[name] = statSync(p).size;
  }
  const fallbackHashes = Object.fromEntries(
    await Promise.all(assets.map(async (name) => [name, await sha256File(assetPathMap[name])])),
  );

  const remoteTagCommit = resolveRemoteTagCommit(tag);
  if (remoteTagCommit) {
    assert.equal(
      remoteTagCommit,
      commit,
      `Tag ${tag} already points to ${remoteTagCommit}, refusing to publish binaries built from ${commit}.`,
    );
  }

  const allReleases = getReleases();
  const existingPublished = allReleases.find((r) => r && !r.draft && r.tag_name === tag) || null;
  const matchingDrafts = allReleases.filter((r) => r && r.draft && r.tag_name === tag);
  const title = `Block ${version}`;
  let publishedRelease;

  if (existingPublished) {
    assert.equal(
      remoteTagCommit,
      commit,
      `Published release ${tag} tag commit (${remoteTagCommit || "missing"}) does not match planned commit ${commit}.`,
    );
    publishedRelease = fetchReleaseById(existingPublished.id);
    await verifyReleaseAssets(publishedRelease, assets, assetSizes, fallbackHashes, tag);
  } else {
    let draft = null;
    let createdDraftId = null;
    if (matchingDrafts.length === 1 && matchingDrafts[0].target_commitish === commit) {
      draft = fetchReleaseById(matchingDrafts[0].id);
      console.log(`Resuming existing draft release #${draft.id} for ${tag} (${commit.slice(0, 7)})`);
    } else if (matchingDrafts.length > 0) {
      throw new Error(
        `Release ${tag} has ${matchingDrafts.length} conflicting draft(s) requiring manual reconciliation.`,
      );
    }

    let published = false;
    try {
      if (!draft) {
        draft = JSON.parse(
          gh(
            ["api", "--method", "POST", `repos/${REPO}/releases`, "--input", "-"],
            JSON.stringify({
              tag_name: tag,
              target_commitish: commit,
              name: title,
              body: releaseNotes,
              draft: true,
              prerelease: isNightly,
            }),
          ),
        );
        createdDraftId = draft.id;
      }

      const allowSet = new Set(assets);
      const validUploaded = new Set();
      for (const remote of draft.assets ?? []) {
        const matchesLocal = allowSet.has(remote.name)
          && remote.state === "uploaded"
          && remote.size === assetSizes[remote.name]
          && Boolean(remote.digest)
          && remote.digest.toLowerCase() === `sha256:${fallbackHashes[remote.name]}`;
        if (matchesLocal && !validUploaded.has(remote.name)) {
          validUploaded.add(remote.name);
        } else if (remote.id) {
          gh(["api", "--method", "DELETE", `repos/${REPO}/releases/assets/${remote.id}`]);
        }
      }

      for (const name of assets) {
        if (!validUploaded.has(name)) {
          uploadReleaseAsset(draft.id, name, assetPathMap[name]);
        }
      }

      const verifiedDraft = fetchReleaseById(draft.id);
      await verifyReleaseAssets(verifiedDraft, assets, assetSizes, fallbackHashes, tag);

      ensureTagRefMatches(tag, commit);
      const latestReleases = getReleases();
      const hasNewerStable = latestReleases.some((r) => {
        if (!r || r.draft || r.prerelease) return false;
        const other = versionFromTag(r.tag_name);
        return Boolean(other && compareVersions(other, version) > 0);
      });

      publishedRelease = JSON.parse(
        gh(
          ["api", "--method", "PATCH", `repos/${REPO}/releases/${draft.id}`, "--input", "-"],
          JSON.stringify({
            name: title,
            body: releaseNotes,
            target_commitish: commit,
            draft: false,
            prerelease: isNightly,
            make_latest: !isNightly && !hasNewerStable ? "true" : "false",
          }),
        ),
      );
      published = true;

      const postTagCommit = resolveRemoteTagCommit(tag);
      assert.equal(
        postTagCommit,
        commit,
        `Post-publish tag ${tag} resolved to ${postTagCommit || "missing"} instead of ${commit}.`,
      );
    } catch (err) {
      if (createdDraftId && !published) {
        try {
          const current = fetchReleaseById(createdDraftId);
          if (current?.draft === true) {
            gh(["api", "--method", "DELETE", `repos/${REPO}/releases/${createdDraftId}`]);
          }
        } catch (cleanupErr) {
          console.error(
            `::warning title=publish-cleanup::Failed to clean up partial draft #${createdDraftId} for ${tag}: ${cleanupErr?.message || cleanupErr}`,
          );
        }
      }
      throw err;
    }
  }

  if (!skipWriteback) {
    writebackVersionCommit(version, tag, {
      sourceCommit: commit,
      buildFeeds: () => buildFeedsForTag(tag, fallbackHashes),
    });
  }

  const summary = `已发布 ${title} (${tag}, ${channel})\n\n${releaseNotes}\n`;
  console.log(summary);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${summary}\n`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  if (process.argv.includes("--sync-only")) {
    const tagIdx = process.argv.indexOf("--tag");
    const tag = (tagIdx >= 0 ? process.argv[tagIdx + 1] : "") || process.env.RELEASE_TAG || "";
    await syncPublishedRelease(tag);
  } else {
    await publishRelease({ skipWriteback: process.argv.includes("--no-writeback") });
  }
}
