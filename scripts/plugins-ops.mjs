#!/usr/bin/env bun
/**
 * Batch status, validation, release, and store crawl script for official Block plugins.
 *
 * Usage:
 *   bun run plugins:ops -- <status|check|release> [--plugin <name>] [--dry-run]
 */
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { basename, resolve } from "node:path";

const ROOT = resolve(import.meta.dir, "..");
const PLUGINS_MD = resolve(ROOT, ".agents/plugins.md");
const STORE_INDEX = resolve(ROOT, "store/store-index.json");

function fail(msg, code = 1) {
  console.error(`[plugins-ops] ERROR: ${msg}`);
  process.exit(code);
}

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, {
    cwd: opts.cwd || ROOT,
    encoding: "utf8",
    stdio: opts.capture ? ["ignore", "pipe", "pipe"] : "inherit",
    env: opts.env ? { ...process.env, ...opts.env } : process.env,
  });
  if (res.error) fail(`${cmd} failed: ${res.error.message}`);
  if (!opts.allowFail && res.status !== 0) {
    if (opts.capture && res.stderr) console.error(res.stderr.trim());
    fail(`Command exited with ${res.status}: ${cmd} ${args.join(" ")}`, res.status || 1);
  }
  return res;
}

const DEFAULT_TIMEOUT_SEC = 1200;

function parseArgs(argv) {
  let command = "";
  let plugin = "";
  let dryRun = false;
  let timeoutSec = DEFAULT_TIMEOUT_SEC;

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--plugin" || arg === "-p") {
      plugin = argv[++i] ?? "";
    } else if (arg.startsWith("--plugin=")) {
      plugin = arg.slice("--plugin=".length);
    } else if (arg === "--dry-run") {
      dryRun = true;
    } else if (arg === "--timeout-sec") {
      const raw = argv[++i] ?? "";
      timeoutSec = Number(raw);
      if (!Number.isFinite(timeoutSec) || timeoutSec <= 0) {
        fail(`Invalid --timeout-sec value: "${raw}"`);
      }
    } else if (arg.startsWith("--timeout-sec=")) {
      const raw = arg.slice("--timeout-sec=".length);
      timeoutSec = Number(raw);
      if (!Number.isFinite(timeoutSec) || timeoutSec <= 0) {
        fail(`Invalid --timeout-sec value: "${raw}"`);
      }
    } else if (arg === "-h" || arg === "--help") {
      console.log(
        "Usage: bun run plugins:ops -- <status|check|release> [--plugin <name>] [--dry-run] [--timeout-sec <seconds>]"
      );
      process.exit(0);
    } else if (!arg.startsWith("-") && !command) {
      command = arg;
    } else {
      fail(`Unknown argument: ${arg}`);
    }
  }

  if (!["status", "check", "release"].includes(command)) {
    fail("Subcommand must be one of: status, check, release");
  }
  return { command, plugin: plugin.trim(), dryRun, timeoutSec };
}

function loadActivePlugins(filterName) {
  if (!existsSync(PLUGINS_MD)) fail(`Missing ${PLUGINS_MD}`);
  const raw = readFileSync(PLUGINS_MD, "utf8");
  const plugins = [];
  const lineRe = /^-\s+`([^`]+)`[：:]\s*(.*)$/gm;
  for (const match of raw.matchAll(lineRe)) {
    const dir = match[1].trim();
    const desc = match[2].trim();
    if (desc.includes("已下架")) continue;
    plugins.push({
      name: basename(dir),
      dir,
      desc,
    });
  }

  if (!filterName) return plugins;
  const matched = plugins.filter(
    (p) => p.name === filterName || p.name === `block-${filterName}`
  );
  if (matched.length === 0) {
    fail(
      `Plugin "${filterName}" not found in active plugins (${plugins.map((p) => p.name).join(", ")})`
    );
  }
  return matched;
}

function parseSemver(tag) {
  const clean = tag.replace(/^v/, "");
  const parts = clean.split(".").map((n) => parseInt(n, 10));
  if (parts.length < 3 || parts.some(Number.isNaN)) return null;
  return parts;
}

function cmpSemverDesc(a, b) {
  const pa = parseSemver(a);
  const pb = parseSemver(b);
  if (!pa && !pb) return b.localeCompare(a);
  if (!pa) return 1;
  if (!pb) return -1;
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const diff = (pb[i] ?? 0) - (pa[i] ?? 0);
    if (diff !== 0) return diff;
  }
  return 0;
}

function getRepoStatus(plugin) {
  if (!existsSync(plugin.dir)) {
    return {
      ...plugin,
      exists: false,
      branch: "-",
      dirty: "-",
      ahead: "-",
      latestTag: "-",
    };
  }

  const branch = run("git", ["-C", plugin.dir, "rev-parse", "--abbrev-ref", "HEAD"], {
    capture: true,
    allowFail: true,
  }).stdout.trim() || "-";

  const statusOut = run("git", ["-C", plugin.dir, "status", "--porcelain"], {
    capture: true,
    allowFail: true,
  }).stdout;
  const dirty = statusOut ? statusOut.trim().split("\n").filter(Boolean).length : 0;

  let ahead = 0;
  const aheadRes = run("git", ["-C", plugin.dir, "rev-list", "--count", "@{u}..HEAD"], {
    capture: true,
    allowFail: true,
  });
  if (aheadRes.status === 0) {
    ahead = parseInt(aheadRes.stdout.trim(), 10) || 0;
  } else {
    const fallback = run("git", ["-C", plugin.dir, "rev-list", "--count", `origin/${branch}..HEAD`], {
      capture: true,
      allowFail: true,
    });
    ahead = fallback.status === 0 ? parseInt(fallback.stdout.trim(), 10) || 0 : 0;
  }

  const tagsRes = run("git", ["-C", plugin.dir, "ls-remote", "--tags", "--refs", "origin"], {
    capture: true,
    allowFail: true,
  });
  let latestTag = "-";
  if (tagsRes.status === 0 && tagsRes.stdout.trim()) {
    const tags = tagsRes.stdout
      .trim()
      .split("\n")
      .map((line) => line.split("\t")[1]?.replace("refs/tags/", ""))
      .filter(Boolean)
      .sort(cmpSemverDesc);
    if (tags.length > 0) latestTag = tags[0];
  }

  return { ...plugin, exists: true, branch, dirty, ahead, latestTag };
}

function loadStoreVersions() {
  const map = new Map();
  if (!existsSync(STORE_INDEX)) return map;
  const data = JSON.parse(readFileSync(STORE_INDEX, "utf8"));
  for (const app of data.apps || []) {
    if (app.id) map.set(app.id, app.version || "-");
    if (app.repo) map.set(basename(app.repo), app.version || "-");
  }
  return map;
}

function pad(str, width) {
  const s = String(str);
  return s.length >= width ? s : s + " ".repeat(width - s.length);
}

function cmdStatus(plugins) {
  const storeVersions = loadStoreVersions();
  const rows = plugins.map((p) => {
    const st = getRepoStatus(p);
    return {
      plugin: st.name,
      branch: st.branch,
      dirty: String(st.dirty),
      ahead: String(st.ahead),
      remoteTag: st.latestTag,
      storeVer: storeVersions.get(st.name) || "-",
    };
  });

  const headers = {
    plugin: "PLUGIN",
    branch: "BRANCH",
    dirty: "DIRTY",
    ahead: "AHEAD",
    remoteTag: "REMOTE TAG",
    storeVer: "STORE",
  };
  const cols = ["plugin", "branch", "dirty", "ahead", "remoteTag", "storeVer"];
  const widths = {};
  for (const c of cols) {
    widths[c] = Math.max(headers[c].length, ...rows.map((r) => r[c].length)) + 2;
  }

  console.log(cols.map((c) => pad(headers[c], widths[c])).join(""));
  console.log(cols.map((c) => "-".repeat(widths[c] - 2) + "  ").join(""));
  for (const r of rows) {
    console.log(cols.map((c) => pad(r[c], widths[c])).join(""));
  }
}

function extractSvgRefs(markdown) {
  const refs = new Set();
  const attrRe = /(?:src|srcset)=["']([^"']+\.svg(?:\?[^"']*)?)["']/gi;
  const mdRe = /!\[[^\]]*\]\(([^)]+\.svg(?:\?[^)]*)?)\)/gi;
  for (const re of [attrRe, mdRe]) {
    for (const m of markdown.matchAll(re)) {
      const target = m[1].split(/[?#]/)[0].trim();
      if (!target || /^(https?:|data:)/i.test(target)) continue;
      refs.add(target);
    }
  }
  return [...refs];
}

function cmdCheck(plugins) {
  let failures = 0;

  for (const p of plugins) {
    console.log(`\n=== Checking ${p.name} (${p.dir}) ===`);
    if (!existsSync(p.dir)) {
      console.error(`[FAIL] Directory missing: ${p.dir}`);
      failures++;
      continue;
    }

    const res = run("go", ["run", "./cmd/block-plugin-check", p.dir], {
      cwd: ROOT,
      env: { CGO_ENABLED: "0" },
      allowFail: true,
    });
    if (res.status !== 0) {
      console.error(`[FAIL] block-plugin-check failed for ${p.name}`);
      failures++;
      continue;
    }

    let docOk = true;
    for (const docName of ["README.md", "README.en.md"]) {
      const docPath = resolve(p.dir, docName);
      if (!existsSync(docPath)) {
        console.error(`[FAIL] Missing ${docName} in ${p.name}`);
        docOk = false;
        continue;
      }
      const content = readFileSync(docPath, "utf8");
      const svgRefs = extractSvgRefs(content);
      for (const ref of svgRefs) {
        const svgPath = resolve(p.dir, ref);
        if (!existsSync(svgPath)) {
          console.error(`[FAIL] ${docName} references missing SVG: ${ref}`);
          docOk = false;
        }
      }
    }

    if (!docOk) {
      failures++;
    } else {
      console.log(`[OK] ${p.name}: README.md, README.en.md & referenced SVGs verified`);
    }
  }

  if (failures > 0) {
    fail(`${failures} plugin(s) failed check.`);
  }
  console.log(`\n[plugins-ops] All ${plugins.length} plugin(s) passed check.`);
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function triggerAndWaitRelease(plugin, timeoutSec = DEFAULT_TIMEOUT_SEC) {
  const beforeRes = run(
    "gh",
    ["run", "list", "--workflow", "release.yml", "--branch", "main", "--limit", "10", "--json", "databaseId"],
    { cwd: plugin.dir, capture: true, allowFail: true }
  );
  let prevMaxId = 0;
  if (beforeRes.status === 0 && beforeRes.stdout.trim()) {
    const arr = JSON.parse(beforeRes.stdout);
    prevMaxId = arr.reduce((max, r) => Math.max(max, Number(r?.databaseId) || 0), 0);
  }

  const triggeredAtMs = Date.now();
  run("gh", ["workflow", "run", "release.yml", "--ref", "main"], { cwd: plugin.dir });

  let runId = 0;
  for (let i = 0; i < 10; i++) {
    await sleep(3000);
    const listRes = run(
      "gh",
      [
        "run",
        "list",
        "--workflow",
        "release.yml",
        "--branch",
        "main",
        "--limit",
        "10",
        "--json",
        "databaseId,status,conclusion,url,createdAt",
      ],
      { cwd: plugin.dir, capture: true }
    );
    const runs = JSON.parse(listRes.stdout || "[]");
    const newer = runs.find(
      (r) =>
        Number(r.databaseId) > prevMaxId &&
        (!r.createdAt || Date.parse(r.createdAt) >= triggeredAtMs - 60_000)
    );
    if (newer) {
      runId = newer.databaseId;
      break;
    }
  }
  if (!runId) fail(`Timed out waiting for release.yml run to appear for ${plugin.name}`);

  const deadline = Date.now() + timeoutSec * 1000;
  while (Date.now() < deadline) {
    const viewRes = run(
      "gh",
      ["run", "view", String(runId), "--json", "status,conclusion,url"],
      { cwd: plugin.dir, capture: true }
    );
    const info = JSON.parse(viewRes.stdout);
    if (info.status === "completed") {
      if (info.conclusion !== "success") {
        fail(`Release failed for ${plugin.name}: ${info.conclusion} (${info.url})`);
      }
      console.log(`[OK] ${plugin.name} released: ${info.url}`);
      return;
    }
    await sleep(8000);
  }
  fail(`Timed out after ${timeoutSec}s waiting for release.yml run ${runId} (${plugin.name}) to complete`);
}

async function cmdRelease(plugins, explicitPlugin, dryRun, timeoutSec) {
  const targets = [];
  for (const p of plugins) {
    const st = getRepoStatus(p);
    if (!st.exists) {
      console.warn(`[WARN] Skipping missing directory: ${p.dir}`);
      continue;
    }
    if (explicitPlugin) {
      targets.push(st);
      continue;
    }
    let behindTag = false;
    if (st.latestTag !== "-") {
      const cnt = run("git", ["-C", st.dir, "rev-list", "--count", `${st.latestTag}..HEAD`], {
        capture: true,
        allowFail: true,
      });
      if (cnt.status === 0 && parseInt(cnt.stdout.trim(), 10) > 0) {
        behindTag = true;
      }
    }
    if (st.ahead > 0 || behindTag) {
      targets.push(st);
    }
  }

  if (targets.length === 0) {
    console.log("[plugins-ops] No plugins have pending commits or unreleased changes.");
    return;
  }

  for (const t of targets) {
    if (dryRun) {
      console.log(`[dry-run] Would release ${t.name} (ahead=${t.ahead}, tag=${t.latestTag}):`);
      if (t.ahead > 0) {
        console.log(`  git -C ${t.dir} pull --rebase --autostash origin main`);
        console.log(`  git -C ${t.dir} push origin main`);
      }
      console.log(`  gh workflow run release.yml --ref main (cwd=${t.dir})`);
      console.log(`  poll release.yml until success`);
    } else {
      console.log(`[plugins-ops] Releasing ${t.name}...`);
      if (t.ahead > 0) {
        run("git", ["-C", t.dir, "pull", "--rebase", "--autostash", "origin", "main"]);
        run("git", ["-C", t.dir, "push", "origin", "main"]);
      }
      await triggerAndWaitRelease(t, timeoutSec);
    }
  }

  if (dryRun) {
    console.log("[dry-run] Would trigger store index crawl:");
    console.log("  gh workflow run crawl.yml --repo blockmd-plugins/store-index --ref main");
  } else {
    console.log("[plugins-ops] Triggering store-index crawl.yml...");
    run("gh", ["workflow", "run", "crawl.yml", "--repo", "blockmd-plugins/store-index", "--ref", "main"]);
    console.log("[plugins-ops] Store index crawl triggered.");
  }
}

async function main() {
  const { command, plugin, dryRun, timeoutSec } = parseArgs(process.argv.slice(2));
  const plugins = loadActivePlugins(plugin);

  if (command === "status") {
    cmdStatus(plugins);
  } else if (command === "check") {
    cmdCheck(plugins);
  } else if (command === "release") {
    await cmdRelease(plugins, Boolean(plugin), dryRun, timeoutSec);
  }
}

main().catch((err) => fail(err?.message || String(err)));
