import assert from "node:assert/strict";
import { existsSync, mkdirSync } from "node:fs";
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
  const script = resolve(ROOT, "scripts/release/block.nsi");
  const nsisPath = (value) => value.replaceAll("\\", "/");
  run("makensis", [
    `/DVERSION=${version}`,
    `/DOUTFILE=${nsisPath(outfile)}`,
    `/DBINARY=${nsisPath(binary)}`,
    script,
  ]);
  assert.ok(existsSync(outfile), `Failed to create ${SETUP_NAME}`);
  console.log(`Packaged Block v${version}:`);
  console.log(`  ${outfile}`);
}

if (import.meta.main) {
  await packageWindows();
}
