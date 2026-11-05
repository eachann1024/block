import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { cargoRelease, run } from "./cargo-release.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const SETUP_NAME = "block-windows-x64-setup.exe";
const RELEASE_DIR = resolve(ROOT, "dist/release");

export async function packageWindows() {
  assert.equal(process.platform, "win32", "Windows is required to build the NSIS installer");
  const version = applyReleasePlanVersion();
  assert.match(version, /^\d+\.\d+\.\d+$/);
  cargoRelease();
  const binary = resolve(ROOT, "target/release/block.exe");
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  const outfile = join(RELEASE_DIR, SETUP_NAME);
  const staged = join(RELEASE_DIR, "block.exe");
  copyFileSync(binary, staged);
  const script = resolve(ROOT, "scripts/release/block.nsi");
  const makensis = [
    process.env.MAKENSIS,
    "C:\\Program Files (x86)\\NSIS\\makensis.exe",
    "C:\\Program Files\\NSIS\\makensis.exe",
  ].find((path) => path && existsSync(path)) || "makensis";
  // NSIS treats "/" as a switch, so the payload is a slash-free name in this directory.
  run(makensis, [`/DVERSION=${version}`, `/DOUTFILE=${SETUP_NAME}`, "/DBINARY=block.exe", script], {
    cwd: RELEASE_DIR,
  });
  rmSync(staged, { force: true });
  assert.ok(existsSync(outfile), `Failed to create ${SETUP_NAME}`);
  console.log(`Packaged Block v${version}:`);
  console.log(`  ${outfile}`);
}

if (import.meta.main) {
  await packageWindows();
}
