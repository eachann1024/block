import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { renderWindowsNsisFileAssociations } from "../file-associations.mjs";
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
  const srcScript = resolve(ROOT, "scripts/release/block.nsi");
  // Stage script, binary, generated .nsh, and installer output in an isolated
  // per-run temp directory so concurrent packaging runs never clobber each other.
  const workDir = mkdtempSync(join(tmpdir(), "block-nsis-"));
  const workScript = join(workDir, "block.nsi");
  const staged = join(workDir, "block.exe");
  const assocNshName = "block-fileassoc.nsh";
  const assocNshPath = join(workDir, assocNshName);
  const stagedOutfile = join(workDir, SETUP_NAME);
  const publishTmp = join(RELEASE_DIR, `.${SETUP_NAME}.tmp-${process.pid}-${Date.now()}`);
  const makensis = [
    process.env.MAKENSIS,
    process.platform === "win32" ? "C:\\Program Files (x86)\\NSIS\\makensis.exe" : null,
    process.platform === "win32" ? "C:\\Program Files\\NSIS\\makensis.exe" : null,
  ].find((path) => path && existsSync(path)) || "makensis";
  const defineFlag = process.platform === "win32" ? "/D" : "-D";
  try {
    copyFileSync(srcScript, workScript);
    writeFileSync(assocNshPath, renderWindowsNsisFileAssociations(ROOT), "utf8");
    copyFileSync(binary, staged);
    run(makensis, [
      `${defineFlag}VERSION=${version}`,
      `${defineFlag}OUTFILE=${stagedOutfile.replaceAll("\\", "/")}`,
      `${defineFlag}BINARY=block.exe`,
      `${defineFlag}ASSOC_NSH=${assocNshName}`,
      workScript,
    ]);
    assert.ok(existsSync(stagedOutfile), `Failed to create ${SETUP_NAME}`);
    copyFileSync(stagedOutfile, publishTmp);
    renameSync(publishTmp, outfile);
  } finally {
    rmSync(publishTmp, { force: true });
    rmSync(workDir, { recursive: true, force: true });
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
