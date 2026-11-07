import assert from "node:assert/strict";
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { installerFor } from "./allowed-assets.mjs";
import { run } from "./cargo-release.mjs";
import { applyReleasePlanVersion, ROOT } from "./select-release.mjs";

const RELEASE_DIR = resolve(ROOT, "dist/release");
const DEB_DEPENDS = [
  "libc6",
  "libgcc-s1",
  "libxkbcommon0",
  "libxkbcommon-x11-0",
  "libxcb1",
  "libxcb-xkb1",
  "libxau6",
  "libxdmcp6",
  "libbsd0",
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
  writeFileSync(join(appDir, "block.desktop"), [
    "[Desktop Entry]",
    "Type=Application",
    "Name=Block",
    "Exec=block",
    "Icon=block",
    "Categories=Office;",
    "Terminal=false",
    "",
  ].join("\n"));
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
    const outfile = join(RELEASE_DIR, installerFor({ platform: "linux", arch: names.label, kind: "deb" }));
    run("dpkg-deb", ["--root-owner-group", "--build", root, outfile]);
    console.log(`  ${outfile}`);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

export async function packageNative() {
  assert.equal(process.platform, "linux", "Linux is required to build the deb package");
  const version = applyReleasePlanVersion();
  const names = archNames();
  const binary = resolve(ROOT, "target/release/block");
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  buildDeb(binary, version, names);
}

if (import.meta.main) {
  await packageNative();
}
