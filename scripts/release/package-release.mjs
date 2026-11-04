import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { appName, run, verifySignature } from "../app-bundle.mjs";
import { buildApp } from "../build-app.mjs";
import { readCargoVersion, ROOT, syncVersionFiles } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const DMG_NAME = "block-macos.dmg";
const ZIP_NAME = "block-macos.zip";

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

function sha256File(filePath) {
  return createHash("sha256").update(readFileSync(filePath)).digest("hex");
}

export async function packageRelease() {
  const planPath = resolve(ROOT, "release-plan.json");
  let version = readCargoVersion(ROOT);
  if (existsSync(planPath)) {
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    if (plan.version) {
      version = plan.version;
      syncVersionFiles(ROOT, version);
    }
  }

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

    const zipPath = join(RELEASE_DIR, ZIP_NAME);
    run("ditto", ["-c", "-k", "--sequesterRsrc", "--keepParent", appBundle, zipPath]);
    assert.ok(existsSync(zipPath), `Failed to create ${ZIP_NAME}`);

    const files = [DMG_NAME, ZIP_NAME];
    const sums = files.map((name) => `${sha256File(join(RELEASE_DIR, name))}  ${name}\n`).join("");
    writeFileSync(join(RELEASE_DIR, "SHA256SUMS.txt"), sums, "utf8");

    console.log(`Packaged Block v${version}:`);
    console.log(`  ${dmgPath}`);
    console.log(`  ${zipPath}`);
  } finally {
    rmSync(stageDir, { recursive: true, force: true });
  }
}

if (import.meta.main) {
  await packageRelease();
}
