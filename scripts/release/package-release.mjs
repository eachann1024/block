import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { appName, normalizeArch, verifySignature } from "../app-bundle.mjs";
import { buildApp } from "../build-app.mjs";
import { installerFor } from "./allowed-assets.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");

function cloneDir(src, dest) {
  if (process.platform === "darwin") {
    const res = spawnSync("cp", ["-cR", src, dest], { stdio: "ignore" });
    if (res.status === 0) return;
  }
  cpSync(src, dest, { recursive: true });
}

function resolveTargetArches() {
  const list = process.env.BLOCK_ARCHES;
  if (list && list.trim()) {
    const arches = list
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    if (arches.length > 0) return arches;
  }
  return [process.env.BLOCK_ARCH || process.arch];
}

function runAsync(cmd, args) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(cmd, args, { stdio: "inherit", cwd: ROOT });
    child.on("error", rejectPromise);
    child.on("close", (code) => {
      if (code === 0) resolvePromise();
      else rejectPromise(new Error(`${cmd} ${args.join(" ")} exited ${code}`));
    });
  });
}

async function createDmgWithRetry(stageDir, outDmg) {
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      rmSync(outDmg, { force: true });
      await runAsync("hdiutil", [
        "create",
        "-volname",
        "Block",
        "-srcfolder",
        stageDir,
        "-ov",
        "-format",
        "UDZO",
        outDmg,
      ]);
      return;
    } catch (err) {
      lastError = err;
      if (attempt < 3) {
        await new Promise((r) => setTimeout(r, 1000 * attempt));
      }
    }
  }
  throw lastError;
}

export async function packageRelease(arches = resolveTargetArches()) {
  const version = applyReleasePlanVersion();
  mkdirSync(RELEASE_DIR, { recursive: true });

  // Build each arch sequentially (buildApp shares dist/ staging), while overlapping
  // hdiutil DMG compression in the background with the next arch's Go compilation.
  const dmgTasks = [];
  for (const rawArch of arches) {
    const dmgName = installerFor({ platform: "macos", arch: rawArch, kind: "dmg" });
    const built = await buildApp({
      install: false,
      profile: "release",
      platform: "darwin",
      arch: normalizeArch(rawArch),
      publishRoot: false,
    });
    const appBundle = built.archArtifact;
    assert.ok(existsSync(appBundle), `App bundle missing at ${appBundle}`);

    const stageDir = mkdtempSync(join(tmpdir(), "block-dmg-"));
    const stagedApp = join(stageDir, `${appName}.app`);
    try {
      cloneDir(appBundle, stagedApp);
      verifySignature(stagedApp);
      symlinkSync("/Applications", join(stageDir, "Applications"));
    } catch (err) {
      rmSync(stageDir, { recursive: true, force: true });
      throw err;
    }

    const dmgPath = join(RELEASE_DIR, dmgName);
    dmgTasks.push(
      (async () => {
        try {
          await createDmgWithRetry(stageDir, dmgPath);
          assert.ok(existsSync(dmgPath), `Failed to create ${dmgName}`);
          return dmgPath;
        } finally {
          rmSync(stageDir, { recursive: true, force: true });
        }
      })(),
    );
  }

  const dmgPaths = await Promise.all(dmgTasks);
  console.log(`Packaged Block v${version}:`);
  for (const dmgPath of dmgPaths) {
    console.log(`  ${dmgPath}`);
  }
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await packageRelease();
}
