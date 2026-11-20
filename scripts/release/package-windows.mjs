import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { installerFor } from "./allowed-assets.mjs";
import { canReuseReleaseBinary, goRelease, releaseBinaryPath, run } from "./go-release.mjs";
import { applyReleasePlanVersion, assertValidWindowsPEVersion, readPackageVersion, ROOT } from "./select-release.mjs";

const WINDOWS_ARCH = process.env.BLOCK_ARCH || "x64";
if (WINDOWS_ARCH !== "x64" && WINDOWS_ARCH !== "amd64") {
  throw new Error(`Windows installers are built for x64 only, got ${WINDOWS_ARCH}`);
}
const SETUP_NAME = installerFor({ platform: "windows", arch: "x64", kind: "exe" });
const RELEASE_DIR = resolve(ROOT, "dist/release");

export async function packageWindows() {
  const reuse = process.env.BLOCK_REUSE_BINARY === "1";
  const version = assertValidWindowsPEVersion(reuse ? readPackageVersion(ROOT) : applyReleasePlanVersion());
  let binary = releaseBinaryPath("windows", "amd64");
  if (!reuse || !canReuseReleaseBinary({ goos: "windows", goarch: "amd64", version, outFile: binary })) {
    binary = goRelease({ goos: "windows", goarch: "amd64", outFile: binary });
  }
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  const outfile = join(RELEASE_DIR, SETUP_NAME);
  const script = resolve(ROOT, "scripts/release/block.nsi");
  // `File` looks beside the .nsi, and a path containing "/" is parsed as a switch.
  const staged = join(dirname(script), "block.exe");
  copyFileSync(binary, staged);
  const makensis = [
    process.env.MAKENSIS,
    process.platform === "win32" ? "C:\\Program Files (x86)\\NSIS\\makensis.exe" : null,
    process.platform === "win32" ? "C:\\Program Files\\NSIS\\makensis.exe" : null,
  ].find((path) => path && existsSync(path)) || "makensis";
  const defineFlag = process.platform === "win32" ? "/D" : "-D";
  try {
    run(makensis, [
      `${defineFlag}VERSION=${version}`,
      `${defineFlag}OUTFILE=${outfile.replaceAll("\\", "/")}`,
      `${defineFlag}BINARY=block.exe`,
      script,
    ]);
  } finally {
    rmSync(staged, { force: true });
  }
  assert.ok(existsSync(outfile), `Failed to create ${SETUP_NAME}`);
  console.log(`Packaged Block v${version}:`);
  console.log(`  ${outfile}`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await packageWindows();
}
