import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readlinkSync, rmSync } from "node:fs";
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

// The drag-to-install window (arrow background, icon positions, window size)
// lives in the volume's .DS_Store. Its background reference is an alias bound
// to that exact volume, so a prebuilt .DS_Store cannot be copied in; dmgbuild
// writes it against the real image and needs no Finder automation.
const DMGBUILD_VERSION = "1.6.5";
const DMG_ASSETS = resolve(ROOT, "scripts/release/dmg");

function ensureDmgbuild() {
  const venv = resolve(ROOT, "dist/.dmgbuild-venv");
  const bin = join(venv, "bin/dmgbuild");
  const marker = join(venv, `.dmgbuild-${DMGBUILD_VERSION}`);
  if (existsSync(bin) && existsSync(marker)) return bin;
  rmSync(venv, { recursive: true, force: true });
  const steps = [
    ["python3", ["-m", "venv", venv]],
    [join(venv, "bin/pip"), ["install", "--quiet", "--disable-pip-version-check", `dmgbuild==${DMGBUILD_VERSION}`]],
    ["touch", [marker]],
  ];
  for (const [cmd, args] of steps) {
    const res = spawnSync(cmd, args, { stdio: "inherit" });
    assert.equal(res.status, 0, `${cmd} ${args.join(" ")} failed`);
  }
  return bin;
}

// dmgbuild 1.6.5 invokes ditto without checking its exit code, so mount the
// generated DMG read-only and verify the app bundle and its signature inside.
function verifyDmgBundle(dmgPath) {
  assert.ok(existsSync(dmgPath), `DMG missing at ${dmgPath}`);
  const mountPoint = mkdtempSync(join(tmpdir(), "block-dmg-verify-"));
  let attached = false;
  try {
    const attach = spawnSync(
      "hdiutil",
      ["attach", "-readonly", "-nobrowse", "-noautoopen", "-mountpoint", mountPoint, dmgPath],
      { encoding: "utf8" },
    );
    assert.equal(
      attach.status,
      0,
      `Failed to mount ${dmgPath} for verification: ${attach.stderr || attach.stdout || attach.error}`,
    );
    attached = true;

    const mountedApp = join(mountPoint, `${appName}.app`);
    const mountedBin = join(mountedApp, "Contents", "MacOS", appName);
    const applicationsLink = join(mountPoint, " ");
    assert.ok(existsSync(mountedApp), `Mounted DMG missing ${appName}.app`);
    assert.ok(existsSync(mountedBin), `Mounted DMG missing ${appName}.app/Contents/MacOS/${appName}`);
    assert.ok(lstatSync(applicationsLink).isSymbolicLink(), "Mounted DMG missing Applications symlink");
    assert.equal(readlinkSync(applicationsLink), "/Applications", "Mounted DMG Applications symlink target mismatch");

    const verify = spawnSync("codesign", ["--verify", "--deep", "--strict", "--verbose=2", mountedApp], {
      encoding: "utf8",
    });
    assert.equal(
      verify.status,
      0,
      `Mounted DMG codesign verification failed for ${dmgPath}:\n${verify.stderr || verify.stdout}`,
    );
  } finally {
    if (attached) {
      const detach = spawnSync("hdiutil", ["detach", mountPoint, "-quiet"], { stdio: "ignore" });
      if (detach.status !== 0) {
        spawnSync("hdiutil", ["detach", mountPoint, "-force", "-quiet"], { stdio: "ignore" });
      }
    }
    rmSync(mountPoint, { recursive: true, force: true });
  }
}

async function createDmgWithRetry(dmgbuild, stagedApp, outDmg) {
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      rmSync(outDmg, { force: true });
      await runAsync(dmgbuild, [
        "-s",
        join(DMG_ASSETS, "settings.py"),
        "-D",
        `app=${stagedApp}`,
        "-D",
        `background=${join(DMG_ASSETS, "background.tiff")}`,
        "Block",
        outDmg,
      ]);
      verifyDmgBundle(outDmg);
      return;
    } catch (err) {
      lastError = err;
      rmSync(outDmg, { force: true });
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
  // DMG builds are chained: each one mounts a volume named "Block".
  const dmgbuild = ensureDmgbuild();
  const dmgTasks = [];
  let dmgQueue = Promise.resolve();
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
    } catch (err) {
      rmSync(stageDir, { recursive: true, force: true });
      throw err;
    }

    const dmgPath = join(RELEASE_DIR, dmgName);
    const previous = dmgQueue;
    const task = (async () => {
        try {
          await previous.catch(() => {});
          await createDmgWithRetry(dmgbuild, stagedApp, dmgPath);
          assert.ok(existsSync(dmgPath), `Failed to create ${dmgName}`);
          return dmgPath;
        } finally {
          rmSync(stageDir, { recursive: true, force: true });
        }
      })();
    dmgQueue = task;
    dmgTasks.push(task);
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
