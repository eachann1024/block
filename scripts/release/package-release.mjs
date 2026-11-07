import assert from "node:assert/strict";
import { cpSync, existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { appName, run, verifySignature } from "../app-bundle.mjs";
import { buildApp } from "../build-app.mjs";
import { installerFor } from "./allowed-assets.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const DMG_NAME = installerFor({ platform: "macos", arch: process.env.BLOCK_ARCH || process.arch, kind: "dmg" });

function createDmgWithRetry(stageDir, outDmg) {
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      rmSync(outDmg, { force: true });
      run("hdiutil", [
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
    }
  }
  throw lastError;
}

export async function packageRelease() {
  const version = applyReleasePlanVersion();

  const appBundle = await buildApp({ install: false });
  assert.ok(existsSync(appBundle), `App bundle missing at ${appBundle}`);
  verifySignature(appBundle);

  rmSync(RELEASE_DIR, { recursive: true, force: true });
  mkdirSync(RELEASE_DIR, { recursive: true });

  const stageDir = mkdtempSync(join(tmpdir(), "block-dmg-"));
  try {
    const stagedApp = join(stageDir, `${appName}.app`);
    cpSync(appBundle, stagedApp, { recursive: true });
    verifySignature(stagedApp);
    symlinkSync("/Applications", join(stageDir, "Applications"));

    const dmgPath = join(RELEASE_DIR, DMG_NAME);
    createDmgWithRetry(stageDir, dmgPath);
    assert.ok(existsSync(dmgPath), `Failed to create ${DMG_NAME}`);

    console.log(`Packaged Block v${version}:`);
    console.log(`  ${dmgPath}`);
  } finally {
    rmSync(stageDir, { recursive: true, force: true });
  }
}

if (import.meta.main) {
  await packageRelease();
}
