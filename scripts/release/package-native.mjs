import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { installerFor } from "./allowed-assets.mjs";
import { canReuseReleaseBinary, goRelease, releaseBinaryPath, run } from "./go-release.mjs";
import { applyReleasePlanVersion, readPackageVersion, ROOT } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const DEB_DEPENDS = [
  "libc6",
  "libxkbcommon0",
  "libxkbcommon-x11-0",
  "libxcb1",
  "libxcb-xkb1",
  "libxau6",
  "libxdmcp6",
  "libwayland-client0",
  "libwayland-egl1",
  "libvulkan1",
  "libegl1",
].join(", ");

function archNames() {
  if (process.arch !== "x64") throw new Error(`Linux packages are built for x64 only, got ${process.arch}`);
  return { label: "x64", deb: "amd64" };
}

function stagePayload(root, binary) {
  const binDir = join(root, "usr/bin");
  const iconDir = join(root, "usr/share/icons/hicolor/1024x1024/apps");
  const appDir = join(root, "usr/share/applications");
  mkdirSync(binDir, { recursive: true });
  mkdirSync(iconDir, { recursive: true });
  mkdirSync(appDir, { recursive: true });
  copyFileSync(binary, join(binDir, "block"));
  chmodSync(join(binDir, "block"), 0o755);
  copyFileSync(resolve(ROOT, "assets/app-icon/icon-1024.png"), join(iconDir, "block.png"));
  // Ship base block.desktop without MimeType= in the static payload so root
  // package install never overrides any user's private default fallback (e.g.
  // text/plain -> text/x-go); per-user Open-With associations are synchronized
  // in each user's own session with full default-application protection.
  writeFileSync(join(appDir, "block.desktop"), [
    "[Desktop Entry]",
    "Type=Application",
    "Name=Block",
    "Exec=block %F",
    "Icon=block",
    "Categories=Office;TextEditor;Utility;",
    "Terminal=false",
    "StartupWMClass=block",
    "",
  ].join("\n"));
}

let supportsDebThreads = null;
function debThreadArgs() {
  if (supportsDebThreads === null) {
    const help = spawnSync("dpkg-deb", ["--help"], { encoding: "utf8" });
    supportsDebThreads = `${help.stdout ?? ""}${help.stderr ?? ""}`.includes("--threads-max");
  }
  return supportsDebThreads ? ["--threads-max=0"] : [];
}

function buildDeb(binary, version, names) {
  const root = mkdtempSync(join(tmpdir(), "block-deb-"));
  try {
    stagePayload(root, binary);
    mkdirSync(join(root, "DEBIAN"));
    writeFileSync(join(root, "DEBIAN/control"), [
      "Package: block",
      `Version: ${version}`,
      `Architecture: ${names.deb}`,
      "Maintainer: Block",
      `Depends: ${DEB_DEPENDS}`,
      "Section: editors",
      "Priority: optional",
      "Description: Block",
      " A block-based notes editor.",
      "",
    ].join("\n"));
    const postinst = join(root, "DEBIAN/postinst");
    writeFileSync(postinst, [
      "#!/bin/sh",
      "set -e",
      'if [ "$1" = "configure" ] && [ -x /usr/bin/block ]; then',
      "  /usr/bin/block --sync-system-linux-desktop || true",
      "fi",
      "",
    ].join("\n"));
    chmodSync(postinst, 0o755);
    const prerm = join(root, "DEBIAN/prerm");
    writeFileSync(prerm, [
      "#!/bin/sh",
      "set -e",
      'if { [ "$1" = "remove" ] || [ "$1" = "purge" ]; } && [ -x /usr/bin/block ]; then',
      "  /usr/bin/block --cleanup-system-linux-desktop || true",
      "fi",
      "",
    ].join("\n"));
    chmodSync(prerm, 0o755);
    const postrm = join(root, "DEBIAN/postrm");
    writeFileSync(postrm, [
      "#!/bin/sh",
      "set -e",
      'if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then',
      "  if command -v update-desktop-database >/dev/null 2>&1; then",
      "    update-desktop-database /usr/share/applications >/dev/null 2>&1 || true",
      "  fi",
      "fi",
      "",
    ].join("\n"));
    chmodSync(postrm, 0o755);
    const outfile = join(RELEASE_DIR, installerFor({ platform: "linux", arch: names.label, kind: "deb" }));
    run("dpkg-deb", ["--root-owner-group", ...debThreadArgs(), "--build", root, outfile]);
    console.log(`  ${outfile}`);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

export async function packageNative() {
  assert.equal(process.platform, "linux", "Linux is required to build the deb package");
  const reuse = process.env.BLOCK_REUSE_BINARY === "1";
  const version = reuse ? readPackageVersion(ROOT) : applyReleasePlanVersion();
  const names = archNames();
  let binary = releaseBinaryPath("linux", names.deb);
  if (!reuse || !canReuseReleaseBinary({ goos: "linux", goarch: names.deb, version, outFile: binary })) {
    binary = goRelease({ goos: "linux", goarch: names.deb, outFile: binary });
  }
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  buildDeb(binary, version, names);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await packageNative();
}
