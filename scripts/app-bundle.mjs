import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { accessSync, appendFileSync, constants, existsSync, lstatSync, mkdirSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join } from "node:path";

export const appName = "Thinkly Fast";
export const bundleId = "dev.eachann.thinkly-fast";
export const installPath = "/Applications/Thinkly Fast.app";
export const installedExecutable = `${installPath}/Contents/MacOS/thinkly-fast`;
export const logDir = join(homedir(), "Library/Logs/ThinklyFast");
export const buildLogPath = join(logDir, "build.log");
export const runtimeLogPath = join(logDir, "thinkly-fast.log");

export function buildLog(msg) {
  const line = `[${new Date().toISOString()}] [build] ${msg}\n`;
  process.stdout.write(line);
  try {
    mkdirSync(logDir, { recursive: true });
    appendFileSync(buildLogPath, line);
  } catch {}
}

const plistKeys = [
  ["CFBundleName", appName],
  ["CFBundleDisplayName", appName],
  ["CFBundleIdentifier", bundleId],
  ["CFBundleExecutable", "thinkly-fast"],
  ["CFBundlePackageType", "APPL"],
  ["CFBundleVersion", "1"],
  ["CFBundleShortVersionString", "0.1.0"],
  ["LSMinimumSystemVersion", "12.0"],
  ["NSHighResolutionCapable", true],
];

export function plist() {
  const body = plistKeys
    .map(([key, value]) => `  <key>${key}</key>\n  ${tag(value)}`)
    .join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0">\n<dict>\n${body}\n</dict>\n</plist>\n`;
}

function tag(value) {
  return typeof value === "boolean" ? `<${value}/>` : `<string>${value}</string>`;
}

export function run(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: "utf8", ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const detail = `${result.stderr ?? ""}${result.stdout ?? ""}`.trim();
    throw new Error(detail || `${command} exited ${result.status}`);
  }
  return result.stdout ?? "";
}

export function binaryPath(root) {
  const t0 = Date.now();
  buildLog("compiling release binary (cargo build --locked --release)...");
  const result = spawnSync("cargo", ["build", "--locked", "--release", "--message-format", "json"], {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.error) throw result.error;
  const entries = (result.stdout ?? "")
    .split("\n")
    .filter(Boolean)
    .flatMap((line) => {
      try { return [JSON.parse(line)]; } catch { return []; }
    });
  const rendered = [];
  for (const entry of entries) {
    if (entry.reason === "compiler-message" && entry.message?.rendered) {
      const msg = entry.message.rendered.trim();
      rendered.push(msg);
      buildLog(`compiler ${entry.message.level ?? "diag"}: ${msg}`);
    }
  }
  if (result.status !== 0) {
    const fallback = (result.stderr ?? "").trim();
    throw new Error(rendered.join("\n\n") || fallback || `cargo exited ${result.status}`);
  }
  const built = entries.filter(
    (entry) => entry.reason === "compiler-artifact" && entry.target?.name === "thinkly-fast" && entry.target?.kind?.includes("bin") && entry.executable
  );
  const executable = built.at(-1)?.executable;
  if (!executable?.startsWith("/") || !executable.endsWith("/thinkly-fast") || !existsSync(executable)) {
    throw new Error("release build did not report the thinkly-fast executable");
  }
  buildLog(`compiled ${executable} in ${((Date.now() - t0) / 1000).toFixed(2)}s`);
  return executable;
}

export function assertSystemLibraries(executable) {
  const linked = run("otool", ["-L", executable])
    .split("\n")
    .slice(1)
    .map((line) => line.trim().split(/\s+/)[0])
    .filter(Boolean);
  const external = linked.filter((path) => !path.startsWith("/System/Library/") && !path.startsWith("/usr/lib/"));
  if (external.length || !linked.length) throw new Error(`external dylibs must be bundled first: ${external.join(", ") || "none listed"}`);
}

export function sign(bundle) {
  run("plutil", ["-lint", join(bundle, "Contents/Info.plist")]);
  // Preserve symbol table and DWARF line tables so release backtraces and macOS .ips crash logs resolve file:line.
  run("codesign", ["--force", "--sign", "-", bundle]);
  verifySignature(bundle);
}

export function verifySignature(bundle) {
  run("codesign", ["--verify", "--verbose=2", bundle]);
}

export function assertWritable(directory) {
  try {
    accessSync(directory, constants.W_OK);
  } catch {
    throw new Error(`${directory} is not writable`);
  }
}

export function sameVolumeTemp(path) {
  const root = dirname(path);
  mkdirSync(root, { recursive: true });
  const staging = join(root, `.thinkly-fast-install.${process.pid}.${randomBytes(4).toString("hex")}`);
  mkdirSync(staging);
  return staging;
}

export function workspaceTemp() {
  const staging = join(tmpdir(), `thinkly-fast-app.${process.pid}.${randomBytes(4).toString("hex")}`);
  mkdirSync(staging);
  return staging;
}

export function remove(path) {
  rmSync(path, { recursive: true, force: true });
}

export function assertInstallTarget(path) {
  let stat;
  try {
    stat = lstatSync(path);
  } catch (error) {
    if (error?.code === "ENOENT") return false;
    throw error;
  }
  if (stat.isSymbolicLink() || !stat.isDirectory()) throw new Error(`${path} is not an app bundle directory`);
  const info = join(path, "Contents/Info.plist");
  const id = run("plutil", ["-extract", "CFBundleIdentifier", "raw", "-o", "-", info]).trim();
  if (id !== bundleId) throw new Error(`${path} is ${id}, not ${bundleId}`);
  return true;
}
