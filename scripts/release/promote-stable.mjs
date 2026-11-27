import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { RELEASE_ASSETS } from "./allowed-assets.mjs";
import {
  buildFeedsForTag,
  fetchReleaseById,
  resolveRemoteTagCommit,
  sha256RemoteAsset,
  syncPublishedRelease,
  writebackVersionCommit,
} from "./publish-release.mjs";
import { EN_FOOTER, EN_SECTIONS, validateReleaseNotes, ZH_FOOTER, ZH_SECTIONS } from "./release-notes-check.mjs";
import { compareVersions, getReleases, gh, REPO, ROOT, versionFromTag } from "./select-release.mjs";
import { parseReleaseBody } from "./update-feed.mjs";

const FAILED_CONCLUSIONS = new Set(["failure", "timed_out", "startup_failure", "action_required"]);

function summary(line) {
  console.log(line);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `${line}\n`);
}

function writeOutputs(entries) {
  if (!process.env.GITHUB_OUTPUT) return;
  const lines = Object.entries(entries).map(([k, v]) => `${k}=${v}`).join("\n") + "\n";
  appendFileSync(process.env.GITHUB_OUTPUT, lines);
}

function parseArgs(argv) {
  let tag = process.env.PROMOTE_TAG?.trim() || "";
  let force = process.env.FORCE_PROMOTE === "true";
  let dryRun = process.env.DRY_RUN === "true";
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--tag" && argv[i + 1]) {
      tag = argv[++i].trim();
    } else if (arg.startsWith("--tag=")) {
      tag = arg.slice("--tag=".length).trim();
    } else if (arg === "--force") {
      force = true;
    } else if (arg === "--dry-run") {
      dryRun = true;
    }
  }
  if (tag && /^\d+\.\d+\.\d+$/.test(tag)) {
    tag = `v${tag}`;
  }
  return { tag, force, dryRun };
}

async function verifyCandidateAssets(release) {
  const remoteAssets = Array.isArray(release?.assets) ? release.assets : [];
  if (remoteAssets.length !== RELEASE_ASSETS.length) {
    return { ok: false, reason: `附件数量为 ${remoteAssets.length}，期望 ${RELEASE_ASSETS.length}` };
  }
  const fallbackHashes = {};
  for (const name of RELEASE_ASSETS) {
    const matches = remoteAssets.filter((a) => a?.name === name);
    if (matches.length !== 1) {
      return { ok: false, reason: `缺少或重复安装包 ${name}` };
    }
    const asset = matches[0];
    if (asset.state !== "uploaded" || !(Number(asset.size) > 0)) {
      return { ok: false, reason: `安装包 ${name} 未上传完成` };
    }
    if (asset.digest && /^sha256:[0-9a-f]{64}$/i.test(asset.digest.trim())) {
      fallbackHashes[name] = asset.digest.trim().slice("sha256:".length).toLowerCase();
    } else if (asset.id) {
      const hex = await sha256RemoteAsset(asset.id);
      if (!/^[0-9a-f]{64}$/i.test(hex)) {
        return { ok: false, reason: `无法校验 ${name} 的 SHA-256` };
      }
      fallbackHashes[name] = hex.toLowerCase();
    } else {
      return { ok: false, reason: `安装包 ${name} 缺少摘要信息` };
    }
  }
  return { ok: true, fallbackHashes };
}

function inspectCommitHealth(commit) {
  // 查询失败直接抛错，让任务结果可见，而不是把所有候选静默跳过。
  const runsData = JSON.parse(gh(["api", `repos/${REPO}/actions/runs?head_sha=${commit}&per_page=100`]));
  const runs = Array.isArray(runsData?.workflow_runs) ? runsData.workflow_runs : [];
  const latestOf = (pred) => runs.filter(pred).sort((a, b) => (Number(b.id) || 0) - (Number(a.id) || 0))[0] || null;

  const releaseRun = latestOf((r) => r?.path === ".github/workflows/release.yml" || r?.name === "Release");
  if (!releaseRun) {
    return { ok: false, reason: "缺少 Release 工作流运行记录" };
  }
  if (releaseRun.status !== "completed") {
    return { ok: false, reason: `Release 工作流尚未完成 (${releaseRun.status})` };
  }
  if (releaseRun.conclusion !== "success") {
    return { ok: false, reason: `Release 工作流未成功 (${releaseRun.conclusion})` };
  }

  const ciRun = latestOf((r) => (r?.path === ".github/workflows/ci.yml" || r?.name === "CI") && r?.event !== "pull_request");
  if (ciRun) {
    if (ciRun.status !== "completed") {
      return { ok: false, reason: `CI 尚未完成 (${ciRun.status})` };
    }
    if (ciRun.conclusion !== "success") {
      return { ok: false, reason: `CI 未通过 (${ciRun.conclusion})` };
    }
  }

  const checksData = JSON.parse(gh(["api", `repos/${REPO}/commits/${commit}/check-runs?per_page=100`]));
  const checkRuns = Array.isArray(checksData?.check_runs) ? checksData.check_runs : [];
  const latestByName = new Map();
  for (const cr of checkRuns.sort((a, b) => (Number(b.id) || 0) - (Number(a.id) || 0))) {
    if (cr?.name && !latestByName.has(cr.name)) {
      latestByName.set(cr.name, cr);
    }
  }
  for (const [name, cr] of latestByName.entries()) {
    if (cr.status !== "completed") {
      return { ok: false, reason: `检查 "${name}" 尚未完成 (${cr.status})` };
    }
    if (FAILED_CONCLUSIONS.has(cr.conclusion) || cr.conclusion === "cancelled") {
      return { ok: false, reason: `检查 "${name}" 未通过 (${cr.conclusion})` };
    }
  }

  return { ok: true };
}

function localStableFeedVersion() {
  try {
    const feed = JSON.parse(readFileSync(resolve(ROOT, "updates/feed.json"), "utf8"));
    return versionFromTag(feed?.latest_version || feed?.latest_tag || "");
  } catch {
    return null;
  }
}

function hoursSinceLastStableFeedWrite() {
  try {
    const ts = Number(execFileSync("git", ["log", "-1", "--format=%ct", "--", "updates/feed.json"], { cwd: ROOT, encoding: "utf8" }).trim());
    if (!(ts > 0)) return Infinity;
    return (Date.now() / 1000 - ts) / 3600;
  } catch {
    return Infinity;
  }
}

function buildPromotedBody({ allReleases, targetRelease, latestStableVer }) {
  const targetVer = versionFromTag(targetRelease.tag_name);
  const between = allReleases
    .filter((r) => {
      if (!r || r.draft) return false;
      const ver = versionFromTag(r.tag_name);
      if (!ver) return false;
      if (latestStableVer && compareVersions(ver, latestStableVer) <= 0) return false;
      return compareVersions(ver, targetVer) <= 0;
    })
    .sort((a, b) => compareVersions(versionFromTag(b.tag_name), versionFromTag(a.tag_name)));

  if (between.length <= 1) {
    return targetRelease.body || "";
  }

  const keys = ["new", "improvements", "fixes", "other"];
  const mergedEn = { new: [], improvements: [], fixes: [], other: [] };
  const mergedZh = { new: [], improvements: [], fixes: [], other: [] };
  const seenEn = { new: new Set(), improvements: new Set(), fixes: new Set(), other: new Set() };
  const seenZh = { new: new Set(), improvements: new Set(), fixes: new Set(), other: new Set() };

  for (const rel of between) {
    const parsed = parseReleaseBody(rel.body);
    for (const key of keys) {
      const enItems = parsed.en?.[key] || [];
      const zhItems = parsed.zh?.[key] || [];
      if (enItems.length > 0 && enItems.length === zhItems.length) {
        for (let i = 0; i < enItems.length; i += 1) {
          const enText = String(enItems[i] || "").trim();
          const zhText = String(zhItems[i] || "").trim();
          if (!enText || !zhText) continue;
          if (seenEn[key].has(enText) || seenZh[key].has(zhText)) continue;
          seenEn[key].add(enText);
          seenZh[key].add(zhText);
          mergedEn[key].push(enText);
          mergedZh[key].push(zhText);
        }
      }
    }
  }

  const renderHalf = (title, sections, buckets, footer) => {
    const out = [title, ""];
    let any = false;
    for (let i = 0; i < keys.length; i += 1) {
      const items = buckets[keys[i]];
      if (!items.length) continue;
      if (any) out.push("");
      out.push(sections[i], ...items.map((item) => `- ${item}`));
      any = true;
    }
    if (!any) return "";
    out.push("", footer);
    return out.join("\n");
  };

  const enHalf = renderHalf("## English", EN_SECTIONS, mergedEn, EN_FOOTER);
  const zhHalf = renderHalf("## 中文", ZH_SECTIONS, mergedZh, ZH_FOOTER);
  if (enHalf && zhHalf) {
    try {
      const validated = validateReleaseNotes(`${enHalf}\n\n---\n\n${zhHalf}`);
      if (validated) return validated;
    } catch {}
  }
  return targetRelease.body || "";
}

async function main() {
  const { tag: requestedTag, force, dryRun } = parseArgs(process.argv.slice(2));
  const allReleases = getReleases({ includeBody: true });

  const stable = allReleases
    .filter((r) => r && !r.draft && !r.prerelease && versionFromTag(r.tag_name))
    .sort((a, b) => compareVersions(versionFromTag(a.tag_name), versionFromTag(b.tag_name)));
  const latestStable = stable.at(-1) || null;
  const latestStableVer = latestStable ? versionFromTag(latestStable.tag_name) : null;

  if (!requestedTag) {
    // 上次升稳若已改了 GitHub 状态但清单回写失败，先从失败处补齐（清单 + AUR）。
    const feedVer = localStableFeedVersion();
    if (latestStable && latestStableVer && (!feedVer || compareVersions(latestStableVer, feedVer) > 0)) {
      summary(`稳定清单落后于最新稳定版 ${latestStable.tag_name}，本次先补齐同步。`);
      if (!dryRun) {
        await syncPublishedRelease(latestStable.tag_name);
      }
      writeOutputs({ promoted: dryRun ? "no" : "yes", release_tag: latestStable.tag_name, version: latestStableVer });
      return;
    }
    // 定时任务每天跑一次，但距上次稳定版不足 3 天就跳过，避免按月内日期计算导致间隔不均。
    if (process.env.PROMOTE_SCHEDULED === "true") {
      const hours = hoursSinceLastStableFeedWrite();
      if (hours < 72 - 2) {
        summary(`距上次稳定版仅 ${hours.toFixed(1)} 小时，未满 3 天，跳过。`);
        writeOutputs({ promoted: "no", release_tag: latestStable?.tag_name || "", version: latestStableVer || "" });
        return;
      }
    }
  }

  let candidates;
  if (requestedTag) {
    const exact = allReleases.find((r) => r && !r.draft && r.tag_name === requestedTag) || null;
    if (!exact) {
      throw new Error(`找不到已发布的 Release ${requestedTag}`);
    }
    if (!exact.prerelease) {
      summary(`${requestedTag} 已经是稳定版，补齐清单与 AUR 同步。`);
      if (!dryRun) {
        await syncPublishedRelease(requestedTag);
      }
      writeOutputs({ promoted: dryRun ? "no" : "yes", release_tag: requestedTag, version: versionFromTag(requestedTag) });
      return;
    }
    const ver = versionFromTag(exact.tag_name);
    if (!ver) {
      throw new Error(`无效版本号标签: ${requestedTag}`);
    }
    if (latestStableVer && compareVersions(ver, latestStableVer) <= 0) {
      throw new Error(`${requestedTag} 不高于当前最新稳定版 ${latestStable.tag_name}，拒绝降级升级`);
    }
    candidates = [exact];
  } else {
    candidates = allReleases
      .filter((r) => {
        if (!r || r.draft || !r.prerelease) return false;
        const ver = versionFromTag(r.tag_name);
        if (!ver) return false;
        return !latestStableVer || compareVersions(ver, latestStableVer) > 0;
      })
      .sort((a, b) => compareVersions(versionFromTag(b.tag_name), versionFromTag(a.tag_name)));
  }

  if (candidates.length === 0) {
    summary(`当前没有比最新稳定版 (${latestStable?.tag_name || "无"}) 更新的每夜版，跳过升级。`);
    writeOutputs({ promoted: "no", release_tag: latestStable?.tag_name || "", version: latestStableVer || "" });
    return;
  }

  let selected = null;
  const skippedReasons = [];

  for (const cand of candidates) {
    const tag = cand.tag_name;
    const version = versionFromTag(tag);
    const fullRelease = fetchReleaseById(cand.id);
    const assetCheck = await verifyCandidateAssets(fullRelease);
    if (!assetCheck.ok) {
      skippedReasons.push(`${tag}: ${assetCheck.reason}`);
      continue;
    }
    const commit = resolveRemoteTagCommit(tag);
    if (!commit) {
      skippedReasons.push(`${tag}: 无法解析标签提交 SHA`);
      continue;
    }
    if (/^[0-9a-f]{40}$/i.test(fullRelease.target_commitish || "") && fullRelease.target_commitish !== commit) {
      skippedReasons.push(`${tag}: target_commitish 与标签提交不一致`);
      continue;
    }
    if (!force) {
      const health = inspectCommitHealth(commit);
      if (!health.ok) {
        skippedReasons.push(`${tag}: ${health.reason}`);
        continue;
      }
    }
    selected = {
      tag,
      version,
      commit,
      fullRelease,
      fallbackHashes: assetCheck.fallbackHashes,
    };
    break;
  }

  for (const reason of skippedReasons) {
    summary(`跳过每夜版 ${reason}`);
  }

  if (!selected) {
    const msg = "没有找到符合发布健康标准的每夜版，本次不升级稳定版。";
    if (requestedTag) {
      throw new Error(skippedReasons[0] || msg);
    }
    summary(msg);
    writeOutputs({ promoted: "no", release_tag: latestStable?.tag_name || "", version: latestStableVer || "" });
    return;
  }

  const { tag, version, commit, fullRelease, fallbackHashes } = selected;
  const promotedBody = buildPromotedBody({
    allReleases,
    targetRelease: fullRelease,
    latestStableVer,
  });

  if (dryRun) {
    summary(`[dry-run] 将把每夜版 ${tag} (${commit.slice(0, 7)}) 升为稳定版。`);
    writeOutputs({ promoted: "no", release_tag: tag, version });
    return;
  }

  // 写入前重新读取远端：若期间已有更高的稳定版发布，放弃本次升稳，避免把官网下载指针退回旧版。
  const freshStableVer = getReleases()
    .filter((r) => r && !r.draft && !r.prerelease && versionFromTag(r.tag_name))
    .map((r) => versionFromTag(r.tag_name))
    .sort(compareVersions)
    .at(-1) || null;
  if (freshStableVer && compareVersions(freshStableVer, version) >= 0) {
    summary(`期间已发布稳定版 v${freshStableVer}，不再升级 ${tag}。`);
    writeOutputs({ promoted: "no", release_tag: `v${freshStableVer}`, version: freshStableVer });
    return;
  }

  const promotedRelease = JSON.parse(
    gh(
      ["api", "--method", "PATCH", `repos/${REPO}/releases/${fullRelease.id}`, "--input", "-"],
      JSON.stringify({
        name: `Block ${version}`,
        body: promotedBody,
        draft: false,
        prerelease: false,
        make_latest: "true",
      }),
    ),
  );
  assert.equal(promotedRelease.prerelease, false, `Failed to clear prerelease flag on ${tag}`);

  writebackVersionCommit(version, tag, {
    sourceCommit: commit,
    subject: `chore(release): ${tag} (stable)`,
    buildFeeds: () => buildFeedsForTag(tag, fallbackHashes),
  });

  writeOutputs({ promoted: "yes", release_tag: tag, version });
  summary(`已将每夜版 ${tag} 升为稳定版（上一稳定版：${latestStable?.tag_name || "无"}）。`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await main();
}
