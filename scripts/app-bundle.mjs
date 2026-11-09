import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { accessSync, appendFileSync, constants, existsSync, lstatSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("..", import.meta.url));

export const appName = "Block";
export const bundleId = "dev.eachann.block";
export const installPath = "/Applications/Block.app";
export const installedExecutable = `${installPath}/Contents/MacOS/block`;
export const legacyBundleId = ["dev.eachann", ["thinkly", "fast"].join("-")].join(".");
export const legacyInstallPath = `/Applications/${["Thinkly", "Fast"].join(" ")}.app`;
export const legacyInstalledExecutable = `${legacyInstallPath}/Contents/MacOS/${["thinkly", "fast"].join("-")}`;
export const logDir = join(homedir(), "Library/Logs/Block");
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

const markdownExtensions = ["md", "markdown", "mdown", "mkd", "mkdn", "mdwn", "mdtxt", "mdtext", "mdx"];

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
    const cargo = readFileSync(join(root, "Cargo.toml"), "utf8");
    const match = /^version\s*=\s*"(\d+\.\d+\.\d+)"/m.exec(cargo);
    if (match) return match[1];
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

export function cargoArgs(profile = "release", options = {}) {
  const args = ["build", ...(profile === "release" ? ["--locked", "--release"] : ["--profile", profile]), "--message-format", "json"];
  const target = options.target === undefined ? process.env.CARGO_BUILD_TARGET : options.target;
  if (target) args.push("--target", target);
  if (options.timings === true) args.push("--timings");
  return args;
}

export function binaryPath(root, profile = "release", options = {}) {
  const args = cargoArgs(profile, options);
  const result = spawnSync("cargo", args, {
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
  const errors = [];
  const errorLocations = [];
  for (const entry of entries) {
    if (entry.reason === "compiler-message" && entry.message?.rendered) {
      const msg = entry.message.rendered.trim();
      rendered.push(msg);
      if (entry.message.level === "error") {
        logIssue("ERROR", msg);
        errors.push(msg);
        const span = entry.message.spans?.find((s) => s.is_primary) || entry.message.spans?.[0];
        const loc = span?.file_name ? `${span.file_name}:${span.line_start}` : "cargo";
        errorLocations.push(`${loc} (${entry.message.message})`);
      }
    }
  }
  if (result.status !== 0) {
    const fallback = (result.stderr ?? "").trim();
    const detail = (errors.length ? errors : rendered).join("\n\n") || fallback || `cargo exited ${result.status}`;
    const summary = errorLocations.length ? `\n\n[BUILD FAILED] 失败位置与原因: ${errorLocations.slice(0, 5).join(" | ")}` : "";
    throw new Error(`${detail}${summary}`);
  }
  const built = entries.filter(
    (entry) => entry.reason === "compiler-artifact" && entry.target?.name === "block" && entry.target?.kind?.includes("bin") && entry.executable
  );
  const executable = built.at(-1)?.executable;
  if (!executable?.startsWith("/") || !executable.endsWith("/block") || !existsSync(executable)) {
    throw new Error("release build did not report the block executable");
  }
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

export function sign(bundle, { development = false } = {}) {
  run("plutil", ["-lint", join(bundle, "Contents/Info.plist")]);
  // Preserve symbol table and DWARF line tables so release backtraces and macOS .ips crash logs resolve file:line.
  // Local ad-hoc builds otherwise use a changing cdhash as their designated
  // requirement, invalidating macOS folder grants after every rebuild. The
  // identifier-only requirement is for local development, never distribution.
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
  const staging = join(tmpdir(), `block-app.${process.pid}.${randomBytes(4).toString("hex")}`);
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
