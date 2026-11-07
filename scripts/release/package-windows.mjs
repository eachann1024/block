import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { installerFor } from "./allowed-assets.mjs";
import { cargoRelease, run } from "./cargo-release.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const WINDOWS_ARCH = process.env.BLOCK_ARCH || process.arch;
if (WINDOWS_ARCH !== "x64") throw new Error(`Windows installers are built for x64 only, got ${WINDOWS_ARCH}`);
const SETUP_NAME = installerFor({ platform: "windows", arch: WINDOWS_ARCH, kind: "exe" });
const RELEASE_DIR = resolve(ROOT, "dist/release");

export async function packageWindows() {
  assert.equal(process.platform, "win32", "Windows is required to build the NSIS installer");
  assert.equal(process.arch, "x64", `Windows installers are built for x64 only, got ${process.arch}`);
  const version = applyReleasePlanVersion();
  assert.match(version, /^\d+\.\d+\.\d+$/);
  cargoRelease();
  const binary = resolve(ROOT, "target/release/block.exe");
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  const outfile = join(RELEASE_DIR, SETUP_NAME);
  const script = resolve(ROOT, "scripts/release/block.nsi");
  // `File` looks beside the .nsi, and a path containing "/" is parsed as a switch.
  const staged = join(dirname(script), "block.exe");
  copyFileSync(binary, staged);
  const makensis = [
    process.env.MAKENSIS,
    "C:\\Program Files (x86)\\NSIS\\makensis.exe",
    "C:\\Program Files\\NSIS\\makensis.exe",
  ].find((path) => path && existsSync(path)) || "makensis";
  try {
    run(makensis, [
      `/DVERSION=${version}`,
      `/DOUTFILE=${outfile.replaceAll("\\", "/")}`,
      "/DBINARY=block.exe",
      script,
    ]);
  } finally {
    rmSync(staged, { force: true });
  }
  assert.ok(existsSync(outfile), `Failed to create ${SETUP_NAME}`);
  console.log(`Packaged Block v${version}:`);
  console.log(`  ${outfile}`);
}

if (import.meta.main) {
  await packageWindows();
}
