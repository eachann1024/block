import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, copyFileSync, existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { installerFor } from "./allowed-assets.mjs";
import { canReuseReleaseBinary, goRelease, releaseBinaryPath } from "./go-release.mjs";
import { applyReleasePlanVersion, readPackageVersion, ROOT } from "./select-release.mjs";

const ARCH = process.env.BLOCK_ARCH || (process.arch === "arm64" ? "arm64" : "x64");
if (ARCH !== "x64" && ARCH !== "amd64") {
  throw new Error(`Linux packages are built for x64 only, got ${ARCH}`);
}
const APPIMAGE_NAME = installerFor({ platform: "linux", arch: "x64", kind: "AppImage" });
const SQUASH_ARCH = process.env.APPIMAGE_ARCH || "x86_64";
const RELEASE_DIR = resolve(ROOT, "dist/release");
const HOST_LIB = /^(ld-linux|linux-vdso\.|libc\.|libm\.|libdl\.|libpthread\.|librt\.|libresolv\.|libutil\.|libvulkan\.|libGL\.|libGLX\.|libGLdispatch\.|libEGL\.|libOpenGL\.|librenderdoc\.)/;

function run(cmd, args, extra = {}) {
  const result = spawnSync(cmd, args, { stdio: "inherit", cwd: ROOT, ...extra });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${cmd} ${args.join(" ")} exited ${result.status}`);
}

function linkedLibraries(binary) {
  const listed = spawnSync("ldd", [binary], { encoding: "utf8" });
  const combined = `${listed.stdout ?? ""}\n${listed.stderr ?? ""}`;
  if (listed.status !== 0) {
    if (/not a dynamic executable|statically linked/i.test(combined)) {
      return { dynamic: false, paths: [] };
    }
    throw new Error(`ldd failed for ${binary}: ${combined.trim()}`);
  }
  if (/not a dynamic executable|statically linked/i.test(combined)) {
    return { dynamic: false, paths: [] };
  }
  const paths = [];
  for (const line of (listed.stdout ?? "").split("\n")) {
    const mapped = line.match(/=>\s+(\/\S+)/);
    const absolute = line.match(/^\s*(\/\S+)\s+\(/);
    const path = mapped?.[1] || absolute?.[1];
    if (!path || HOST_LIB.test(basename(path))) continue;
    paths.push(path);
  }
  return { dynamic: true, paths };
}

function soname(path) {
  const printed = spawnSync("patchelf", ["--print-soname", path], { encoding: "utf8" });
  const name = printed.stdout?.trim();
  return printed.status === 0 && name ? name : basename(path);
}

function resolveSoname(name) {
  const listed = spawnSync("ldconfig", ["-p"], { encoding: "utf8" });
  if (listed.status !== 0) return null;
  const line = listed.stdout.split("\n").find((row) => row.trimStart().startsWith(`${name} `));
  return line?.match(/=>\s+(\S+)/)?.[1] ?? null;
}

function dlopenLibraries(binary) {
  const dumped = spawnSync("strings", ["-a", "-n", "8", binary], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  if (dumped.status !== 0) return [];
  const paths = [];
  for (const match of dumped.stdout.matchAll(/lib[A-Za-z0-9_+-]+\.so(?:\.\d+)*/g)) {
    const name = match[0];
    if (HOST_LIB.test(name)) continue;
    const path = resolveSoname(name);
    if (path) paths.push(path);
  }
  return paths;
}

function collectLibraries(binary) {
  const linked = linkedLibraries(binary);
  const seen = new Map();
  const queue = [...linked.paths, ...dlopenLibraries(binary)];
  while (queue.length > 0) {
    const path = queue.pop();
    if (seen.has(path)) continue;
    seen.set(path, soname(path));
    for (const next of linkedLibraries(path).paths) {
      if (!seen.has(next)) queue.push(next);
    }
  }
  return { dynamic: linked.dynamic, libs: seen };
}

function writeAppDir(binary, appDir) {
  rmSync(appDir, { recursive: true, force: true });
  const binDir = join(appDir, "usr/bin");
  const libDir = join(appDir, "usr/lib");
  mkdirSync(binDir, { recursive: true });
  mkdirSync(libDir, { recursive: true });
  const staged = join(binDir, "block");
  copyFileSync(binary, staged);
  chmodSync(staged, 0o755);

  const { dynamic, libs } = collectLibraries(binary);
  for (const [path, name] of libs) {
    copyFileSync(path, join(libDir, name));
  }
  if (dynamic) {
    run("patchelf", ["--set-rpath", "$ORIGIN/../lib", staged]);
  }
  for (const name of libs.values()) {
    run("patchelf", ["--set-rpath", "$ORIGIN", join(libDir, name)]);
  }

  copyFileSync(resolve(ROOT, "assets/app-icon/icon-1024.png"), join(appDir, "block.png"));
  // Omit static MimeType= from the AppImage desktop payload so integrators
  // (e.g. AppImageLauncher) never displace existing default editors without
  // target-machine protection; Block registers protected associations on launch.
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
  writeFileSync(join(appDir, "AppRun"), [
    "#!/bin/sh",
    'HERE=$(dirname "$(readlink -f "$0")")',
    'export LD_LIBRARY_PATH="$HERE/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"',
    'exec "$HERE/usr/bin/block" "$@"',
    "",
  ].join("\n"));
  chmodSync(join(appDir, "AppRun"), 0o755);
}

export async function packageLinux() {
  assert.equal(process.platform, "linux", "Linux is required to build the AppImage");
  assert.equal(process.arch, "x64", `Linux packages are built for x64 only, got ${process.arch}`);
  const reuse = process.env.BLOCK_REUSE_BINARY === "1";
  const version = reuse ? readPackageVersion(ROOT) : applyReleasePlanVersion();
  let binary = releaseBinaryPath("linux", "amd64");
  if (!reuse || !canReuseReleaseBinary({ goos: "linux", goarch: "amd64", version, outFile: binary })) {
    binary = goRelease({ goos: "linux", goarch: "amd64", outFile: binary });
  }
  assert.ok(existsSync(binary), `Release binary missing at ${binary}`);
  const appDir = resolve(ROOT, "dist/Block.AppDir");
  writeAppDir(binary, appDir);
  mkdirSync(RELEASE_DIR, { recursive: true });
  const outfile = join(RELEASE_DIR, APPIMAGE_NAME);
  rmSync(outfile, { force: true });
  const tool = process.env.APPIMAGETOOL || "appimagetool";
  run(tool, [appDir, outfile], {
    env: { ...process.env, ARCH: SQUASH_ARCH, APPIMAGE_EXTRACT_AND_RUN: "1" },
  });
  chmodSync(outfile, 0o755);
  rmSync(`${outfile}.zsync`, { force: true });
  assert.ok(existsSync(outfile), `Failed to create ${APPIMAGE_NAME}`);
  console.log(`Packaged Block v${version}:`);
  console.log(`  ${outfile}`);
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  await packageLinux();
}
