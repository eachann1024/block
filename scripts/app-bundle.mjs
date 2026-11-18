import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { accessSync, appendFileSync, constants, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("..", import.meta.url));

export const appName = "Block";
export const bundleId = "dev.eachann.block";
export const acceptedBundleIds = new Set([bundleId, "com.block.notes"]);
export const installPath = "/Applications/Block.app";
export const installedExecutable = `${installPath}/Contents/MacOS/block`;
export const legacyBundleId = ["dev.eachann", ["thinkly", "fast"].join("-")].join(".");
export const legacyInstallPath = `/Applications/${["Thinkly", "Fast"].join(" ")}.app`;
export const legacyInstalledExecutable = `${legacyInstallPath}/Contents/MacOS/${["thinkly", "fast"].join("-")}`;
export const logDir = join(homedir(), "Library/Application Support/Block/logs");
export const runtimeLogPath = join(logDir, "block.log");

export function logIssue(level, msg) {
  const ts = (Date.now() / 1000).toFixed(3);
  const line = `[${ts}] [${level}] (build) ${msg}\n`;
  process.stderr.write(line);
  try {
    mkdirSync(logDir, { recursive: true });
    appendFileSync(runtimeLogPath, line);
  } catch {}
}

export const markdownExtensions = ["md", "markdown", "mdown", "mkd", "mkdn", "mdwn", "mdtxt", "mdtext", "mdx"];

const plistKeys = [
  ["CFBundleName", appName],
  ["CFBundleDisplayName", "Block"],
  ["CFBundleIdentifier", bundleId],
  ["CFBundleExecutable", "block"],
  ["CFBundlePackageType", "APPL"],
  ["CFBundleIconFile", "AppIcon"],
  ["CFBundleVersion", "1.0.0"],
  ["CFBundleShortVersionString", "1.0.0"],
  ["LSMinimumSystemVersion", "12.0"],
  ["NSHighResolutionCapable", true],
  ["NSHumanReadableCopyright", "Copyright © 2026 Block Contributors"],
  ["NSPrincipalClass", "NSApplication"],
  ["NSSupportsAutomaticGraphicsSwitching", true],
  [
    "CFBundleURLTypes",
    [
      {
        CFBundleURLName: bundleId,
        CFBundleURLSchemes: ["block", "blockmd"],
      },
    ],
  ],
  [
    "CFBundleDocumentTypes",
    [
      {
        CFBundleTypeName: "Markdown Document",
        CFBundleTypeRole: "Editor",
        LSHandlerRank: "Alternate",
        LSItemContentTypes: ["net.daringfireball.markdown", "public.markdown", "public.plain-text"],
        CFBundleTypeExtensions: markdownExtensions,
        CFBundleTypeMIMETypes: ["text/markdown", "text/x-markdown"],
      },
    ],
  ],
  [
    "UTImportedTypeDeclarations",
    [
      {
        UTTypeIdentifier: "net.daringfireball.markdown",
        UTTypeDescription: "Markdown Document",
        UTTypeConformsTo: ["public.plain-text", "public.text"],
        UTTypeTagSpecification: {
          "public.filename-extension": markdownExtensions,
          "public.mime-type": ["text/markdown", "text/x-markdown"],
        },
      },
    ],
  ],
];

export function readAppVersion(root = repoRoot) {
  try {
    const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
    if (typeof pkg.version === "string" && /^\d+\.\d+\.\d+$/.test(pkg.version)) return pkg.version;
  } catch {}
  try {
    const mygo = JSON.parse(readFileSync(join(root, "mygo.json"), "utf8"));
    if (typeof mygo.version === "string" && /^\d+\.\d+\.\d+$/.test(mygo.version)) return mygo.version;
  } catch {}
  return "1.0.0";
}

export function plist(version = readAppVersion()) {
  const body = plistKeys
    .map(([key, value]) => [key, key === "CFBundleShortVersionString" || key === "CFBundleVersion" ? version : value])
    .map(([key, value]) => `  <key>${key}</key>\n${tag(value, "  ")}`)
    .join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0">\n<dict>\n${body}\n</dict>\n</plist>\n`;
}

function tag(value, indent = "") {
  if (typeof value === "boolean") return `${indent}<${value}/>`;
  if (Array.isArray(value)) {
    const items = value.map((item) => tag(item, `${indent}  `)).join("\n");
    return `${indent}<array>\n${items}\n${indent}</array>`;
  }
  if (value && typeof value === "object") {
    const entries = Object.entries(value)
      .map(([k, v]) => `${indent}  <key>${k}</key>\n${tag(v, `${indent}  `)}`)
      .join("\n");
    return `${indent}<dict>\n${entries}\n${indent}</dict>`;
  }
  return `${indent}<string>${value}</string>`;
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

export function normalizePlatform(raw = process.platform) {
  const v = String(raw || "").toLowerCase();
  if (v === "darwin" || v === "macos" || v === "mac") return "darwin";
  if (v === "win32" || v === "windows" || v === "win") return "windows";
  if (v === "linux") return "linux";
  throw new Error(`unsupported platform: ${raw}`);
}

export function normalizeArch(raw = process.arch) {
  const v = String(raw || "").toLowerCase();
  if (v === "arm64" || v === "aarch64") return "arm64";
  if (v === "x64" || v === "x86_64" || v === "amd64") return "amd64";
  throw new Error(`unsupported arch: ${raw}`);
}

export function goBuild({ root = repoRoot, goos, goarch, outFile, profile = "fast", useMygo = false, tempDir } = {}) {
  mkdirSync(dirname(outFile), { recursive: true });
  const env = { ...process.env, CGO_ENABLED: "0" };
  if (tempDir) env.TMPDIR = tempDir;
  if (useMygo) {
    const result = spawnSync(
      "go",
      ["tool", "mygo", "build", "-platform", `${goos}/${goarch}`, "-skip-dmg", "-skip-installer", "-o", join(root, "dist")],
      {
        cwd: root,
        encoding: "utf8",
        env,
        maxBuffer: 64 * 1024 * 1024,
      },
    );
    if (result.error) throw result.error;
    if (result.status !== 0) {
      const detail = `${result.stderr ?? ""}${result.stdout ?? ""}`.trim();
      throw new Error(detail || `go tool mygo build exited ${result.status}`);
    }
  }
  const ldflags = [`-X block/internal/updater.BuildVersion=${readAppVersion(root)}`];
  if (profile === "release" || goos !== "darwin") {
    ldflags.push("-s", "-w");
  }
  if (goos === "windows") {
    ldflags.push("-H=windowsgui");
  }
  const args = ["build"];
  if (ldflags.length) {
    args.push(`-ldflags=${ldflags.join(" ")}`);
  }
  args.push("-o", outFile, "./cmd/block");
  const result = spawnSync("go", args, {
    cwd: root,
    encoding: "utf8",
    env: { ...env, GOOS: goos, GOARCH: goarch },
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const detail = `${result.stderr ?? ""}${result.stdout ?? ""}`.trim();
    throw new Error(detail || `go build exited ${result.status}`);
  }
  if (!existsSync(outFile)) {
    throw new Error(`go build did not produce ${outFile}`);
  }
  return outFile;
}

export function assertSystemLibraries(executable) {
  if (process.platform !== "darwin") return;
  const linked = run("otool", ["-L", executable])
    .split("\n")
    .slice(1)
    .map((line) => line.trim().split(/\s+/)[0])
    .filter(Boolean);
  const external = linked.filter((path) => !path.startsWith("/System/Library/") && !path.startsWith("/usr/lib/"));
  if (external.length || !linked.length) {
    throw new Error(`external dylibs must be bundled first: ${external.join(", ") || "none listed"}`);
  }
}

export function sign(bundle, { development = false } = {}) {
  run("plutil", ["-lint", join(bundle, "Contents/Info.plist")]);
  const requirements = development
    ? ["--identifier", bundleId, "--requirements", `=designated => identifier "${bundleId}"`]
    : [];
  run("codesign", ["--force", "--sign", "-", ...requirements, bundle]);
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
  const staging = join(root, `.block-install.${process.pid}.${randomBytes(4).toString("hex")}`);
  mkdirSync(staging);
  return staging;
}

export function workspaceTemp() {
  const prefix = `block-app.${process.pid}.`;
  try {
    return mkdtempSync(join(tmpdir(), prefix));
  } catch (error) {
    if (!["ENOENT", "ENOTDIR", "EACCES", "EPERM", "EROFS"].includes(error?.code)) throw error;
    // A runner can remove its private TMPDIR before the build starts.
    return mkdtempSync(join("/tmp", prefix));
  }
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
  if (!acceptedBundleIds.has(id) && id !== legacyBundleId) {
    throw new Error(`${path} is ${id}, not ${bundleId}`);
  }
  return true;
}
