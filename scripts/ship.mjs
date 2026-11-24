#!/usr/bin/env bun
/**
 * Parallel-safe atomic release script for Block main repo.
 *
 * Usage:
 *   bun run ship -- -m "<commit msg>" --files <f1> <f2> ... [--dry-run] [--no-build] [--ci-build]
 */
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { resolve, relative, extname } from "node:path";

const ROOT = resolve(import.meta.dir, "..");
const DESIGN_EXTS = new Set([".pen", ".fig", ".sketch", ".xd", ".psd"]);

function fail(msg, code = 1) {
  console.error(`[ship] ERROR: ${msg}`);
  process.exit(code);
}

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, {
    cwd: ROOT,
    encoding: "utf8",
    stdio: opts.capture ? ["ignore", "pipe", "pipe"] : "inherit",
    ...opts,
  });
  if (res.error) fail(`${cmd} failed: ${res.error.message}`);
  if (!opts.allowFail && res.status !== 0) {
    if (opts.capture && res.stderr) console.error(res.stderr.trim());
    fail(`Command exited with ${res.status}: ${cmd} ${args.join(" ")}`, res.status || 1);
  }
  return res;
}

function parseArgs(argv) {
  let message = "";
  const files = [];
  let dryRun = false;
  let noBuild = false;
  let ciBuild = false;

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "-m" || arg === "--message") {
      message = argv[++i] ?? "";
    } else if (arg.startsWith("--message=")) {
      message = arg.slice("--message=".length);
    } else if (arg === "--files" || arg === "-f") {
      while (i + 1 < argv.length && !argv[i + 1].startsWith("-")) {
        files.push(argv[++i]);
      }
    } else if (arg.startsWith("--files=")) {
      const val = arg.slice("--files=".length);
      if (val) files.push(...val.split(",").map((s) => s.trim()).filter(Boolean));
    } else if (arg === "--dry-run") {
      dryRun = true;
    } else if (arg === "--no-build") {
      noBuild = true;
    } else if (arg === "--ci-build") {
      ciBuild = true;
    } else if (arg === "-h" || arg === "--help") {
      console.log(
        "Usage: bun run ship -- -m \"<commit msg>\" --files <f1> <f2> ... [--dry-run] [--no-build] [--ci-build]"
      );
      process.exit(0);
    } else {
      fail(`Unknown argument: ${arg}`);
    }
  }

  return { message: message.trim(), files, dryRun, noBuild, ciBuild };
}

function isDesignFile(relPath) {
  const norm = relPath.replaceAll("\\", "/").toLowerCase();
  if (norm === "designs" || norm.startsWith("designs/") || norm.includes("/designs/")) {
    return true;
  }
  if (norm.endsWith(".dc.html")) return true;
  return DESIGN_EXTS.has(extname(norm));
}

function validateFiles(rawFiles) {
  if (rawFiles.length === 0) {
    fail("Explicit --files <f1> <f2> ... is required to protect parallel sessions' uncommitted changes.");
  }

  const normalized = [];
  for (const raw of rawFiles) {
    if (raw === "." || raw === "./" || raw === "-A" || raw === "--all" || raw === "*") {
      fail(`Wildcard or whole-repo path rejected: "${raw}". Pass explicit file paths.`);
    }
    const abs = resolve(ROOT, raw);
    const rel = relative(ROOT, abs).replaceAll("\\", "/");
    if (!rel || rel === "." || rel.startsWith("..")) {
      fail(`Path must be inside repo root: "${raw}"`);
    }
    if (isDesignFile(rel)) {
      fail(`Design file blocked from commit/push: "${rel}"`);
    }
    const ign = run("git", ["check-ignore", "-v", "--", rel], { capture: true, allowFail: true });
    if (ign.status === 0) {
      fail(`Path is ignored by .gitignore (${ign.stdout.trim()}): "${rel}"`);
    }
    const tracked = run("git", ["ls-files", "--error-unmatch", "--", rel], {
      capture: true,
      allowFail: true,
    });
    if (!existsSync(abs) && tracked.status !== 0) {
      fail(`File does not exist and is not tracked in git: "${rel}"`);
    }
    normalized.push(rel);
  }
  return [...new Set(normalized)];
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

function getStagedFiles() {
  const res = run("git", ["diff", "--cached", "--name-only", "-z"], { capture: true });
  return (res.stdout || "")
    .split("\0")
    .filter(Boolean)
    .map((p) => p.replaceAll("\\", "/"));
}

function getExternalInterference(targetFiles) {
  const targetSet = new Set(targetFiles);
  const res = run("git", ["status", "--porcelain", "-z"], { capture: true });
  const raw = res.stdout || "";
  if (!raw) return [];
  const tokens = raw.split("\0");
  const interfering = [];
  for (let i = 0; i < tokens.length; i++) {
    const entry = tokens[i];
    if (!entry) continue;
    const status = entry.slice(0, 2);
    const path = entry.slice(3).replaceAll("\\", "/");
    let origPath = "";
    if (status[0] === "R" || status[0] === "C" || status[1] === "R" || status[1] === "C") {
      origPath = (tokens[++i] || "").replaceAll("\\", "/");
    }
    if (status === "??") {
      if (!targetSet.has(path)) {
        interfering.push(path);
      }
      continue;
    }
    if (!targetSet.has(path) || (origPath && !targetSet.has(origPath))) {
      interfering.push(path);
    }
  }
  return interfering;
}

function runCheckAndBuildInArchive(noBuild, buildScript) {
  const tmpDir = `/tmp/block-ship-${process.pid}`;
  mkdirSync(tmpDir, { recursive: true });
  try {
    const arch = spawnSync("sh", ["-c", `git archive HEAD | tar -x -C "${tmpDir}"`], {
      cwd: ROOT,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    if (arch.error) fail(`git archive failed: ${arch.error.message}`);
    if (arch.status !== 0) {
      if (arch.stderr) console.error(arch.stderr.trim());
      fail(`git archive HEAD | tar -x exited with ${arch.status}`, arch.status || 1);
    }

    console.log(`[ship] Running bun run check in clean archive (${tmpDir})...`);
    run("bun", ["run", "check"], { cwd: tmpDir });

    if (!noBuild) {
      console.log(`[ship] Running bun run ${buildScript} in clean archive (${tmpDir})...`);
      run("bun", ["run", buildScript], { cwd: tmpDir });
    } else {
      console.log("[ship] Skipped build (--no-build).");
    }
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }
}

function getMaxReleaseRunId() {
  const res = run(
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
      "databaseId",
    ],
    { capture: true, allowFail: true }
  );
  if (res.status !== 0 || !res.stdout.trim()) return 0;
  const runs = JSON.parse(res.stdout || "[]");
  return runs.reduce((max, r) => Math.max(max, Number(r?.databaseId) || 0), 0);
}

async function waitForReleaseRun(headSha, prevMaxId) {
  for (let attempt = 1; attempt <= 6; attempt++) {
    const res = run(
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
        "databaseId,headSha,status,conclusion,url,event",
      ],
      { capture: true }
    );
    const runs = JSON.parse(res.stdout || "[]");
    const match = runs.find((r) => r.headSha === headSha);
    if (match) return match;
    if (attempt < 6) await sleep(5000);
  }

  console.log("[ship] Push did not auto-trigger release.yml (likely paths-ignore); triggering manually...");
  const dispatchPrevMaxId = Math.max(prevMaxId, getMaxReleaseRunId());
  run("gh", ["workflow", "run", "release.yml", "--ref", "main"]);

  for (let attempt = 1; attempt <= 6; attempt++) {
    await sleep(4000);
    const res = run(
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
        "databaseId,headSha,status,conclusion,url,event",
      ],
      { capture: true }
    );
    const runs = JSON.parse(res.stdout || "[]");
    const match = runs.find(
      (r) => Number(r.databaseId) > dispatchPrevMaxId && r.headSha === headSha
    );
    if (match) return match;
  }
  return null;
}

function commitAndRebase(files, message) {
  const fileSet = new Set(files);
  const stagedBefore = getStagedFiles().filter((f) => !fileSet.has(f));

  for (const f of files) {
    const tracked = run("git", ["ls-files", "--error-unmatch", "--", f], { capture: true, allowFail: true });
    if (tracked.status !== 0) {
      run("git", ["add", "-N", "--", f]);
    }
  }
  run("git", ["commit", "--only", "-m", message, "--", ...files]);

  console.log("[ship] Rebasing onto origin/main...");
  const pullRes = run("git", ["pull", "--rebase", "--autostash", "origin", "main"], { allowFail: true });
  if (pullRes.status !== 0) {
    run("git", ["rebase", "--abort"], { capture: true, allowFail: true });
    if (stagedBefore.length > 0) {
      run("git", ["add", "--", ...stagedBefore], { capture: true, allowFail: true });
    }
    fail(`git pull --rebase --autostash origin main failed (exit ${pullRes.status}); rebase aborted and workspace restored.`);
  }

  if (stagedBefore.length > 0) {
    const toRestore = stagedBefore.filter((f) => {
      if (existsSync(resolve(ROOT, f))) return true;
      const tr = run("git", ["ls-files", "--error-unmatch", "--", f], { capture: true, allowFail: true });
      return tr.status === 0;
    });
    if (toRestore.length > 0) {
      run("git", ["add", "--", ...toRestore]);
    }
  }
}

async function main() {
  const { message, files: rawFiles, dryRun, noBuild, ciBuild } = parseArgs(process.argv.slice(2));
  if (!message) fail("Commit message is required (-m \"<commit msg>\").");

  const files = validateFiles(rawFiles);
  const branch = run("git", ["rev-parse", "--abbrev-ref", "HEAD"], { capture: true }).stdout.trim();
  if (branch !== "main") {
    fail(`Current branch is "${branch}". Block develops and ships on "main" only.`);
  }

  const pkg = JSON.parse(readFileSync(resolve(ROOT, "package.json"), "utf8"));
  const buildScript = ciBuild ? "build:ci" : "build";
  const interference = getExternalInterference(files);
  const useArchive = interference.length > 0;

  if (dryRun) {
    console.log(`[dry-run] Branch: ${branch} (v${pkg.version})`);
    console.log(`[dry-run] Commit message: ${message}`);
    console.log(`[dry-run] Isolated files (${files.length}): ${files.join(", ")}`);
    if (useArchive) {
      console.log(`[dry-run] Detected ${interference.length} external workspace change(s); will verify in /tmp/block-ship-${process.pid} via git archive HEAD`);
      console.log(`[dry-run] Step 1: git commit --only -m ${JSON.stringify(message)} -- ${files.join(" ")}`);
      console.log(`[dry-run] Step 2: git pull --rebase --autostash origin main (preserving other staged files)`);
      console.log(`[dry-run] Step 3: git archive HEAD -> /tmp/block-ship-${process.pid} && bun run check${noBuild ? "" : ` && bun run ${buildScript}`}`);
    } else {
      console.log(`[dry-run] Step 1: bun run check`);
      console.log(`[dry-run] Step 2: git commit --only -m ${JSON.stringify(message)} -- ${files.join(" ")}`);
      console.log(`[dry-run] Step 3: git pull --rebase --autostash origin main (preserving other staged files)`);
      if (!noBuild) console.log(`[dry-run] Step 4: bun run ${buildScript}`);
    }
    console.log(`[dry-run] Step 5: git push origin main`);
    console.log(`[dry-run] Step 6: Poll/trigger gh workflow release.yml for HEAD`);
    return;
  }

  if (useArchive) {
    console.log(`[ship] Detected ${interference.length} external change(s); isolating check/build via git archive HEAD.`);
    console.log(`[ship] Step 1-2/5: Committing ${files.length} isolated file(s) & rebasing...`);
    commitAndRebase(files, message);

    console.log(`[ship] Step 3/5: Running check${noBuild ? "" : ` & ${buildScript}`} in clean archive...`);
    runCheckAndBuildInArchive(noBuild, buildScript);
  } else {
    console.log("[ship] Step 1/6: Running bun run check...");
    run("bun", ["run", "check"]);

    console.log(`[ship] Step 2-3/6: Committing ${files.length} isolated file(s) & rebasing...`);
    commitAndRebase(files, message);

    if (!noBuild) {
      console.log(`[ship] Step 4/6: Running bun run ${buildScript}...`);
      run("bun", ["run", buildScript]);
    } else {
      console.log("[ship] Step 4/6: Skipped build (--no-build).");
    }
  }

  const prevMaxId = getMaxReleaseRunId();
  console.log("[ship] Pushing main to origin...");
  run("git", ["push", "origin", "main"]);

  const headSha = run("git", ["rev-parse", "HEAD"], { capture: true }).stdout.trim();
  console.log(`[ship] Checking release.yml for ${headSha.slice(0, 7)}...`);
  const releaseRun = await waitForReleaseRun(headSha, prevMaxId);

  console.log(`[ship] Done!`);
  console.log(`  Commit:  ${headSha}`);
  console.log(`  Version: v${pkg.version}`);
  console.log(`  Release: ${releaseRun ? `${releaseRun.url} (${releaseRun.status})` : "pending (check GitHub Actions)"}`);
}

main().catch((err) => fail(err?.message || String(err)));
