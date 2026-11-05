import assert from "node:assert/strict";
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
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
  if (process.arch === "arm64") return { label: "arm64", deb: "arm64", rpm: "aarch64" };
  if (process.arch === "x64") return { label: "x64", deb: "amd64", rpm: "x86_64" };
  throw new Error(`unsupported arch ${process.arch}`);
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
    const outfile = join(RELEASE_DIR, `block-linux-${names.label}.deb`);
    run("dpkg-deb", ["--root-owner-group", "--build", root, outfile]);
    console.log(`  ${outfile}`);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

function buildRpm(binary, version, names) {
  const top = mkdtempSync(join(tmpdir(), "block-rpm-"));
  try {
    for (const dir of ["BUILD", "BUILDROOT", "RPMS", "SOURCES", "SPECS", "SRPMS"]) {
      mkdirSync(join(top, dir));
    }
    const spec = join(top, "SPECS/block.spec");
    writeFileSync(spec, [
      "Name: block",
      `Version: ${version}`,
      "Release: 1",
      "Summary: Block notes editor",
      "License: Apache-2.0",
      `BuildArch: ${names.rpm}`,
      "AutoReq: no",
      "AutoProv: no",
      "Requires: libxkbcommon libxkbcommon-x11 libxcb libwayland-client libwayland-egl vulkan-loader",
      "",
      "%description",
      "A block-based notes editor.",
      "",
      "%install",
      "mkdir -p %{buildroot}/usr/bin",
      "mkdir -p %{buildroot}/usr/share/applications",
      "mkdir -p %{buildroot}/usr/share/icons/hicolor/1024x1024/apps",
      "cp %{_sourcedir}/block %{buildroot}/usr/bin/block",
      "chmod 755 %{buildroot}/usr/bin/block",
      "cp %{_sourcedir}/block.png %{buildroot}/usr/share/icons/hicolor/1024x1024/apps/block.png",
      "cp %{_sourcedir}/block.desktop %{buildroot}/usr/share/applications/block.desktop",
      "",
      "%files",
      "/usr/bin/block",
      "/usr/share/applications/block.desktop",
      "/usr/share/icons/hicolor/1024x1024/apps/block.png",
      "",
    ].join("\n"));
    const source = join(top, "SOURCES");
    copyFileSync(binary, join(source, "block"));
    copyFileSync(resolve(ROOT, "assets/app-icon/icon-1024.png"), join(source, "block.png"));
    writeFileSync(join(source, "block.desktop"), [
      "[Desktop Entry]",
      "Type=Application",
      "Name=Block",
      "Exec=block",
      "Icon=block",
      "Categories=Office;",
      "Terminal=false",
      "",
    ].join("\n"));
    run("rpmbuild", ["-bb", spec, "--define", `_topdir ${top}`]);
    const built = readdirSync(join(top, "RPMS", names.rpm)).find((name) => name.endsWith(".rpm"));
    assert.ok(built, "rpmbuild did not produce an rpm");
    const outfile = join(RELEASE_DIR, `block-linux-${names.label}.rpm`);
    copyFileSync(join(top, "RPMS", names.rpm, built), outfile);
    console.log(`  ${outfile}`);
  } finally {
    rmSync(top, { recursive: true, force: true });
  }
}

export async function packageNative() {
  assert.equal(process.platform, "linux", "Linux is required to build deb and rpm packages");
  const version = applyReleasePlanVersion();
  const names = archNames();
  const binary = resolve(ROOT, "target/release/block");
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  mkdirSync(RELEASE_DIR, { recursive: true });
  buildDeb(binary, version, names);
  buildRpm(binary, version, names);
}

if (import.meta.main) {
  await packageNative();
}
