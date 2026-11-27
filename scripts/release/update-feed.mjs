import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { RELEASE_ASSETS } from "./allowed-assets.mjs";
import {
  compareVersions,
  fetchReleasePage,
  gh,
  REPO,
  ROOT,
  versionFromTag,
} from "./select-release.mjs";

export const FEED_PATH = resolve(ROOT, "updates/feed.json");
export const NIGHTLY_FEED_PATH = resolve(ROOT, "updates/nightly-feed.json");
export const MAX_FEED_RELEASES = 50;

const ASSET_ORDER = new Map(RELEASE_ASSETS.map((name, index) => [name, index]));
const SHA256_DIGEST_RE = /^sha256:([0-9a-f]{64})$/i;
const SHA256_HEX_RE = /^[0-9a-f]{64}$/i;

function emptyLangNotes() {
  return {
    highlights: "",
    new: [],
    improvements: [],
    fixes: [],
    other: [],
  };
}

function detectHalfLang(part) {
  if (/^##\s*中文\b/m.test(part)) return "zh";
  if (/^##\s*English\b/im.test(part)) return "en";

  const hasZhMarker = /(?:^|\n)\s*(?:🌈\s*)?本次更新重点\s*[：:]|^(?:#{1,6}\s*)?(?:✨\s*新功能|🪐\s*(?:优化|改进)|🐛\s*修复|新功能|改进|优化|修复|其他|下载)\s*$/m.test(part);
  const hasEnMarker = /(?:^|\n)\s*(?:🌈\s*)?Highlights\s*[：:]|^(?:#{1,6}\s*)?(?:✨\s*New|🪐\s*(?:Improved|Improvements)|🐛\s*(?:Fixed|Fixes)|New|Improvements|Improved|Fixes|Fixed|Other|Download)\s*$/im.test(part);

  if (hasZhMarker && !hasEnMarker) return "zh";
  if (hasEnMarker && !hasZhMarker) return "en";

  const cjkCount = (part.match(/[\u3400-\u9fff]/g) || []).length;
  const latinCount = (part.match(/[A-Za-z]+/g) || []).length;
  return cjkCount > 0 && cjkCount >= latinCount ? "zh" : "en";
}

function matchSectionKey(line) {
  if (/^(?:#{1,6}\s*)?(?:✨\s*)?(?:New|新功能)\s*$/i.test(line)) return "new";
  if (/^(?:#{1,6}\s*)?(?:🪐\s*)?(?:Improvements|Improved|改进|优化)\s*$/i.test(line)) return "improvements";
  if (/^(?:#{1,6}\s*)?(?:🐛\s*)?(?:Fixes|Fixed|修复)\s*$/i.test(line)) return "fixes";
  if (/^(?:#{1,6}\s*)?(?:[📦🔧📝🛠️]\s*)?(?:Other|其他)\s*$/i.test(line)) return "other";
  return null;
}

function isFooterLine(line) {
  return /^(?:Website|官网)\s*[：:]/i.test(line)
    || /^Download the macOS, Windows, or Linux installer/i.test(line)
    || /^macOS、Windows\s*和\s*Linux\s*的安装包/i.test(line);
}

function parseHalf(part, target) {
  const lines = part.split("\n");
  let currentKey = null;
  let inHighlights = false;
  let inFooter = false;

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line) {
      inHighlights = false;
      continue;
    }
    if (/^##\s*(?:English|中文)\s*$/i.test(line)) {
      inHighlights = false;
      continue;
    }
    if (/^#{1,6}\s*(?:Download|下载)\s*$/i.test(line)) {
      inFooter = true;
      currentKey = null;
      inHighlights = false;
      continue;
    }
    if (inFooter || isFooterLine(line)) {
      inHighlights = false;
      continue;
    }

    const highlightsMatch = /^(?:🌈\s*)?(?:Highlights|本次更新重点)\s*[：:]\s*(.*)$/i.exec(line);
    if (highlightsMatch) {
      const text = highlightsMatch[1].trim();
      target.highlights = target.highlights ? `${target.highlights} ${text}`.trim() : text;
      currentKey = null;
      inHighlights = true;
      continue;
    }

    const sectionKey = matchSectionKey(line);
    if (sectionKey) {
      currentKey = sectionKey;
      inHighlights = false;
      continue;
    }

    const bulletMatch = /^[-*•]\s+(.+)$/.exec(line);
    if (bulletMatch) {
      inHighlights = false;
      target[currentKey || "other"].push(bulletMatch[1].trim());
      continue;
    }

    if (inHighlights) {
      target.highlights = target.highlights ? `${target.highlights} ${line}`.trim() : line;
    }
  }
}

export function parseReleaseBody(body) {
  const result = {
    zh: emptyLangNotes(),
    en: emptyLangNotes(),
  };
  const normalized = String(body ?? "").replace(/\r\n?/g, "\n").trim();
  if (!normalized) return result;

  const parts = normalized.split(/\n\s*---\s*\n/).map((part) => part.trim()).filter(Boolean);
  for (const part of parts) {
    const lang = detectHalfLang(part);
    parseHalf(part, result[lang]);
  }
  return result;
}

function extractSha256(asset, fallbackHashes = {}) {
  const digest = String(asset?.digest || "").trim();
  const match = SHA256_DIGEST_RE.exec(digest);
  if (match) return match[1].toLowerCase();
  const fallback = fallbackHashes[asset?.name];
  if (typeof fallback === "string" && SHA256_HEX_RE.test(fallback.trim())) {
    return fallback.trim().toLowerCase();
  }
  return "";
}

function formatLatestAssets(rawAssets, fallbackHashes) {
  if (!Array.isArray(rawAssets)) return [];
  return rawAssets
    .filter((asset) => asset && ASSET_ORDER.has(asset.name))
    .map((asset) => ({
      name: asset.name,
      browser_download_url: String(asset.browser_download_url || ""),
      size: Number(asset.size) || 0,
      sha256: extractSha256(asset, fallbackHashes),
    }))
    .sort((a, b) => ASSET_ORDER.get(a.name) - ASSET_ORDER.get(b.name));
}

function countTrustedAssets(release, fallbackHashes = {}, fallbackHashesTag = "") {
  const tag = String(release?.tag_name || "").trim();
  const hashes = !fallbackHashesTag || tag === fallbackHashesTag ? fallbackHashes : {};
  return formatLatestAssets(release?.assets, hashes).filter((a) => Boolean(a.sha256)).length;
}

function releasePreferenceScore(release, version, fallbackHashes = {}, fallbackHashesTag = "") {
  const trustedCount = countTrustedAssets(release, fallbackHashes, fallbackHashesTag);
  let score = trustedCount > 0 ? 100 + trustedCount * 10 : 0;
  if (release?.tag_name === `v${version}`) score += 1;
  return score;
}

export function buildFeedFromReleases(
  rawReleases,
  {
    repo = REPO,
    updatedAt = new Date().toISOString(),
    fallbackHashes = {},
    fallbackHashesTag = "",
    includePrerelease = false,
  } = {},
) {
  const byVersion = new Map();
  for (const release of rawReleases || []) {
    if (!release || release.draft || (!includePrerelease && release.prerelease)) continue;
    const version = versionFromTag(release.tag_name);
    if (!version || !/^\d+\.\d+\.\d+$/.test(version)) continue;
    const existing = byVersion.get(version);
    if (
      !existing
      || releasePreferenceScore(release, version, fallbackHashes, fallbackHashesTag)
        > releasePreferenceScore(existing, version, fallbackHashes, fallbackHashesTag)
    ) {
      byVersion.set(version, release);
    }
  }

  const sorted = Array.from(byVersion.entries())
    .sort(([a], [b]) => compareVersions(b, a))
    .slice(0, MAX_FEED_RELEASES);

  const coveredPlatforms = new Set();
  const releases = sorted.map(([version, release], index) => {
    const tag = String(release.tag_name || "").trim() || `v${version}`;
    const parsed = parseReleaseBody(release.body);
    const matchingFallbackHashes = !fallbackHashesTag || tag === fallbackHashesTag ? fallbackHashes : {};
    const formattedAssets = formatLatestAssets(release.assets, matchingFallbackHashes);
    let keptAssets = [];
    if (index === 0) {
      keptAssets = formattedAssets;
      for (const a of keptAssets) {
        if (a.sha256) coveredPlatforms.add(a.name);
      }
    } else if (coveredPlatforms.size < RELEASE_ASSETS.length) {
      keptAssets = formattedAssets.filter((a) => a.sha256 && !coveredPlatforms.has(a.name));
      for (const a of keptAssets) coveredPlatforms.add(a.name);
    }
    const entry = {
      version,
      tag,
      published_at: String(release.published_at || release.created_at || ""),
      html_url: String(release.html_url || `https://github.com/${repo}/releases/tag/${encodeURIComponent(tag)}`),
      ...(includePrerelease
        ? {
            prerelease: Boolean(release.prerelease),
            channel: release.prerelease ? "nightly" : "stable",
          }
        : {}),
      zh: parsed.zh,
      en: parsed.en,
      assets: keptAssets,
    };
    return entry;
  });

  const latest = releases[0] || null;
  return {
    schema_version: 1,
    repo,
    updated_at: updatedAt,
    latest_version: latest?.version || "",
    latest_tag: latest?.tag || "",
    releases,
  };
}

export function formatFeedJson(feed) {
  return `${JSON.stringify(feed, null, 2)}\n`;
}

export function fetchGithubReleases(repo = REPO, { includePrerelease = false, preserveStableTop = false } = {}) {
  const byVersion = new Map();
  const trimToTop = () => {
    if (byVersion.size <= MAX_FEED_RELEASES * 2) return;
    const allVersions = Array.from(byVersion.keys()).sort((a, b) => compareVersions(b, a));
    const keepSet = new Set(allVersions.slice(0, MAX_FEED_RELEASES));
    if (preserveStableTop) {
      let stableKept = 0;
      for (const ver of allVersions) {
        const rel = byVersion.get(ver);
        if (rel && !rel.prerelease) {
          keepSet.add(ver);
          stableKept += 1;
          if (stableKept >= MAX_FEED_RELEASES) break;
        }
      }
    }
    for (const key of byVersion.keys()) {
      if (!keepSet.has(key)) byVersion.delete(key);
    }
  };
  for (let page = 1; ; page += 1) {
    const batch = fetchReleasePage(repo, page);
    if (batch.length === 0) break;
    for (const release of batch) {
      if (!release || release.draft || (!includePrerelease && release.prerelease)) continue;
      const version = versionFromTag(release.tag_name);
      if (!version || !/^\d+\.\d+\.\d+$/.test(version)) continue;
      const existing = byVersion.get(version);
      if (!existing || releasePreferenceScore(release, version) > releasePreferenceScore(existing, version)) {
        byVersion.set(version, release);
      }
    }
    trimToTop();
    if (batch.length < 100) break;
  }
  return Array.from(byVersion.values());
}

export function fetchGithubReleaseByTag(tag, repo = REPO) {
  const raw = gh(["api", `repos/${repo}/releases/tags/${encodeURIComponent(tag)}`]);
  return JSON.parse(raw);
}

export function syncFeedFromGithub({
  repo = REPO,
  outPath = FEED_PATH,
  write = true,
  tagRelease = null,
  fallbackHashes = {},
  updatedAt = new Date().toISOString(),
  includePrerelease = false,
} = {}) {
  const targetPath = outPath === FEED_PATH && includePrerelease ? NIGHTLY_FEED_PATH : outPath;
  const releases = fetchGithubReleases(repo, { includePrerelease });
  if (tagRelease?.tag_name) {
    const merged = releases.filter((item) => item?.tag_name !== tagRelease.tag_name);
    merged.unshift(tagRelease);
    releases.splice(0, releases.length, ...merged);
  }
  const feed = buildFeedFromReleases(releases, {
    repo,
    updatedAt,
    fallbackHashes,
    fallbackHashesTag: tagRelease?.tag_name || "",
    includePrerelease,
  });
  const json = formatFeedJson(feed);
  if (write) {
    mkdirSync(dirname(targetPath), { recursive: true });
    writeFileSync(targetPath, json, "utf8");
  }
  return { feed, json };
}

export function syncBothFeedsFromGithub({
  repo = REPO,
  stableOutPath = FEED_PATH,
  nightlyOutPath = NIGHTLY_FEED_PATH,
  write = true,
  tagRelease = null,
  fallbackHashes = {},
  updatedAt = new Date().toISOString(),
} = {}) {
  const releases = fetchGithubReleases(repo, { includePrerelease: true, preserveStableTop: true });
  if (tagRelease?.tag_name) {
    const merged = releases.filter((item) => item?.tag_name !== tagRelease.tag_name);
    merged.unshift(tagRelease);
    releases.splice(0, releases.length, ...merged);
  }
  const stableFeed = buildFeedFromReleases(releases, {
    repo,
    updatedAt,
    fallbackHashes,
    fallbackHashesTag: tagRelease?.tag_name || "",
    includePrerelease: false,
  });
  const stableJson = formatFeedJson(stableFeed);
  const nightlyFeed = buildFeedFromReleases(releases, {
    repo,
    updatedAt,
    fallbackHashes,
    fallbackHashesTag: tagRelease?.tag_name || "",
    includePrerelease: true,
  });
  const nightlyJson = formatFeedJson(nightlyFeed);
  if (write) {
    mkdirSync(dirname(stableOutPath), { recursive: true });
    writeFileSync(stableOutPath, stableJson, "utf8");
    mkdirSync(dirname(nightlyOutPath), { recursive: true });
    writeFileSync(nightlyOutPath, nightlyJson, "utf8");
  }
  return {
    stable: { feed: stableFeed, json: stableJson },
    nightly: { feed: nightlyFeed, json: nightlyJson },
  };
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  const args = process.argv.slice(2);
  if (!args.includes("--sync-from-github")) {
    console.error("Usage: node scripts/release/update-feed.mjs --sync-from-github [--channel stable|nightly|all] [--out <path>]");
    process.exit(1);
  }
  const outIdx = args.indexOf("--out");
  const chIdx = args.indexOf("--channel");
  const channel = chIdx >= 0 && args[chIdx + 1] ? args[chIdx + 1].trim().toLowerCase() : "all";
  if (outIdx >= 0 && args[outIdx + 1]) {
    const outPath = resolve(ROOT, args[outIdx + 1]);
    const includePrerelease = channel === "nightly";
    const { feed } = syncFeedFromGithub({ outPath, write: true, includePrerelease });
    if (!existsSync(outPath)) {
      throw new Error(`Failed to write feed file: ${outPath}`);
    }
    console.log(`Synced ${feed.releases.length} releases to ${outPath} (latest: ${feed.latest_tag || "none"})`);
  } else if (channel === "stable" || channel === "nightly") {
    const includePrerelease = channel === "nightly";
    const outPath = includePrerelease ? NIGHTLY_FEED_PATH : FEED_PATH;
    const { feed } = syncFeedFromGithub({ outPath, write: true, includePrerelease });
    console.log(`Synced ${feed.releases.length} ${channel} releases to ${outPath} (latest: ${feed.latest_tag || "none"})`);
  } else {
    const { stable, nightly } = syncBothFeedsFromGithub({ write: true });
    console.log(
      `Synced ${stable.feed.releases.length} stable (${stable.feed.latest_tag || "none"}) and ${nightly.feed.releases.length} nightly (${nightly.feed.latest_tag || "none"}) releases.`,
    );
  }
}
