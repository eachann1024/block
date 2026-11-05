import assert from "node:assert/strict";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { gzipSync } from "node:zlib";
import { appName, run, verifySignature } from "../app-bundle.mjs";
import { buildApp } from "../build-app.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const DMG_NAME = process.env.BLOCK_DMG_NAME
  || (process.arch === "arm64" ? "block-macos-arm64.dmg" : "block-macos-x64.dmg");

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

    const deltaArch = process.arch === "arm64" ? "arm64" : "x64";
    const deltaName = `block-macos-${deltaArch}.bin.gz`;
    const binaryPath = join(appBundle, "Contents/MacOS", appName);
    assert.ok(existsSync(binaryPath), `Binary missing at ${binaryPath}`);
    const deltaPath = join(RELEASE_DIR, deltaName);
    writeFileSync(deltaPath, gzipSync(readFileSync(binaryPath), { level: 9 }));
    assert.ok(existsSync(deltaPath), `Failed to create ${deltaName}`);

    console.log(`Packaged Block v${version}:`);
    console.log(`  ${dmgPath}`);
    console.log(`  ${deltaPath}`);
  } finally {
    rmSync(stageDir, { recursive: true, force: true });
  }
}

if (import.meta.main) {
  await packageRelease();
}
