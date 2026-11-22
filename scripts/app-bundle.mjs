import { spawnSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { accessSync, appendFileSync, constants, existsSync, lstatSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadFileAssociations } from "./file-associations.mjs";
import { resolveLocalBuildVersion } from "./local-version.mjs";

const repoRoot = fileURLToPath(new URL("..", import.meta.url));

export const appName = "Block";
export const bundleId = "dev.eachann.block";
export const acceptedBundleIds = new Set([bundleId, "com.block.notes"]);
export const installPath = "/Applications/Block.app";
export const installedExecutable = `${installPath}/Contents/MacOS/block`;
export const legacyBundleId = ["dev.eachann", ["thinkly", "fast"].join("-")].join(".");
export const legacyInstallPath = `/Applications/${["Thinkly", "Fast"].join(" ")}.app`;
export const legacyInstalledExecutable = `${legacyInstallPath}/Contents/MacOS/${["thinkly", "fast"].join("-")}`;
export const lsregisterPath =
  "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";
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

function buildPlistKeys(version, { markdownExtensions, documentExtensions }) {
  const documentTypes = [
    {
      CFBundleTypeName: "Markdown Document",
      CFBundleTypeIconFile: "DocumentIcon.icns",
      CFBundleTypeRole: "Editor",
      LSHandlerRank: "Owner",
      CFBundleTypeExtensions: markdownExtensions,
      CFBundleTypeMIMETypes: ["text/markdown", "text/x-markdown"],
      CFBundleTypeOSTypes: ["TEXT", "utxt", "TUTX", "****"],
    },
    {
      CFBundleTypeName: "Markdown Content",
      CFBundleTypeIconFile: "DocumentIcon.icns",
      CFBundleTypeRole: "Editor",
      LSHandlerRank: "Default",
      LSItemContentTypes: ["net.daringfireball.markdown", "public.markdown"],
    },
    {
      CFBundleTypeName: "Plain Text Document",
      CFBundleTypeIconFile: "DocumentIcon.icns",
      CFBundleTypeRole: "Editor",
      LSHandlerRank: "Alternate",
      LSItemContentTypes: ["public.plain-text"],
    },
  ];
  if (documentExtensions.length > 0) {
    documentTypes.push({
      CFBundleTypeName: "Block Code and Config Document",
      CFBundleTypeRole: "Editor",
      LSHandlerRank: "Alternate",
      CFBundleTypeExtensions: documentExtensions,
    });
  }
  return [
    ["CFBundleName", appName],
    ["CFBundleDisplayName", "Block"],
    ["CFBundleIdentifier", bundleId],
    ["CFBundleExecutable", "block"],
    ["CFBundlePackageType", "APPL"],
    ["CFBundleIconFile", "AppIcon"],
    ["CFBundleVersion", version],
    ["CFBundleShortVersionString", version],
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
    ["CFBundleDocumentTypes", documentTypes],
    [
      "UTImportedTypeDeclarations",
      [
        {
          UTTypeIdentifier: "net.daringfireball.markdown",
          UTTypeDescription: "Markdown Document",
          UTTypeIconFile: "DocumentIcon.icns",
          UTTypeConformsTo: ["public.utf8-plain-text", "public.plain-text", "public.text"],
          UTTypeTagSpecification: {
            "public.filename-extension": markdownExtensions,
            "public.mime-type": ["text/markdown", "text/x-markdown"],
          },
        },
      ],
    ],
  ];
}

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

export function buildVersion(root = repoRoot) {
  return resolveLocalBuildVersion(root, readAppVersion(root));
}

export function plist(version = buildVersion()) {
  const assoc = loadFileAssociations(repoRoot);
  const body = buildPlistKeys(version, assoc)
    .map(([key, value]) => `  <key>${key}</key>\n${tag(value, "  ")}`)
    .join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0">\n<dict>\n${body}\n</dict>\n</plist>\n`;
}

function escapeXml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function tag(value, indent = "") {
  if (typeof value === "boolean") return `${indent}<${value}/>`;
  if (typeof value === "number") return `${indent}<integer>${value}</integer>`;
  if (Array.isArray(value)) {
    const items = value.map((item) => tag(item, `${indent}  `)).join("\n");
    return `${indent}<array>\n${items}\n${indent}</array>`;
  }
  if (value && typeof value === "object") {
    const entries = Object.entries(value)
      .map(([k, v]) => `${indent}  <key>${escapeXml(k)}</key>\n${tag(v, `${indent}  `)}`)
      .join("\n");
    return `${indent}<dict>\n${entries}\n${indent}</dict>`;
  }
  return `${indent}<string>${escapeXml(value)}</string>`;
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

function readLocalBuildEnv(root = repoRoot) {
  const out = {};
  const envFile = join(root, ".env.local");
  if (!existsSync(envFile)) return out;
  try {
    const raw = readFileSync(envFile, "utf8");
    for (const line of raw.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) continue;
      const eq = trimmed.indexOf("=");
      if (eq <= 0) continue;
      const key = trimmed.slice(0, eq).trim();
      let val = trimmed.slice(eq + 1).trim();
      if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
        val = val.slice(1, -1);
      }
      out[key] = val;
    }
  } catch {}
  return out;
}

function sanitizeLdflagValue(raw, name) {
  const val = String(raw || "").trim();
  if (!val) return "";
  if (/[\s"'`\\]/.test(val)) {
    throw new Error(`${name} must not contain whitespace or quotes`);
  }
  return val;
}

export function resolveFeedbackBuildConfig(root = repoRoot) {
  const local = readLocalBuildEnv(root);
  const endpoint = sanitizeLdflagValue(
    process.env.BLOCK_FEEDBACK_URL ?? local.BLOCK_FEEDBACK_URL ?? "",
    "BLOCK_FEEDBACK_URL",
  );
  const clientToken = sanitizeLdflagValue(
    process.env.BLOCK_FEEDBACK_CLIENT_TOKEN ?? local.BLOCK_FEEDBACK_CLIENT_TOKEN ?? "",
    "BLOCK_FEEDBACK_CLIENT_TOKEN",
  );
  return { endpoint, clientToken };
}

export function feedbackLdflags(root = repoRoot) {
  const { endpoint, clientToken } = resolveFeedbackBuildConfig(root);
  const flags = [];
  if (endpoint) flags.push(`-X block/internal/feedback.DefaultEndpoint=${endpoint}`);
  if (clientToken) flags.push(`-X block/internal/feedback.DefaultClientToken=${clientToken}`);
  return flags;
}

export function feedbackBuildHash(root = repoRoot) {
  const { endpoint, clientToken } = resolveFeedbackBuildConfig(root);
  if (!endpoint && !clientToken) return "";
  return createHash("sha256").update(`${endpoint}\0${clientToken}`).digest("hex").slice(0, 16);
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
  const feedbackCfg = resolveFeedbackBuildConfig(root);
  const ldflags = [
    `-X block/internal/updater.BuildVersion=${buildVersion(root)}`,
    ...feedbackLdflags(root),
  ];
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
  const redactText = (text) => {
    let out = String(text || "");
    if (feedbackCfg.endpoint) out = out.replaceAll(feedbackCfg.endpoint, "<redacted-endpoint>");
    if (feedbackCfg.clientToken) out = out.replaceAll(feedbackCfg.clientToken, "<redacted-token>");
    return out;
  };
  const result = spawnSync("go", args, {
    cwd: root,
    encoding: "utf8",
    env: { ...env, GOOS: goos, GOARCH: goarch },
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.error) {
    throw new Error(`go build failed to start: ${redactText(result.error.message || result.error.code || "unknown error")}`);
  }
  if (result.status !== 0) {
    const detail = redactText(`${result.stderr ?? ""}${result.stdout ?? ""}`.trim());
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

export function markNeverIndex(dir) {
  try {
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, ".metadata_never_index"), "");
  } catch {}
}

export function isProcessAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === "EPERM";
  }
}

export function unregisterMacBundle(bundlePath) {
  if (process.platform !== "darwin" || !bundlePath) return;
  spawnSync(lsregisterPath, ["-u", bundlePath], { stdio: "ignore" });
}

export function refreshUserIconCache() {
  if (process.platform !== "darwin") return;
  try {
    const res = spawnSync("getconf", ["DARWIN_USER_CACHE_DIR"], { encoding: "utf8" });
    const cacheDir = (res.stdout || "").trim();
    if (cacheDir && existsSync(cacheDir)) {
      for (const name of readdirSync(cacheDir)) {
        if (name.startsWith("com.apple.iconservices")) {
          rmSync(join(cacheDir, name), { recursive: true, force: true });
        }
      }
    }
    spawnSync("killall", ["iconservicesagent"], { stdio: "ignore" });
  } catch {}
}

export function pruneLaunchServicesRegistrations(canonicalPath = installPath) {
  if (process.platform !== "darwin") return;
  const canonical = resolve(canonicalPath);
  const res = spawnSync(lsregisterPath, ["-dump"], {
    encoding: "utf8",
    maxBuffer: 128 * 1024 * 1024,
  });
  if (res.status === 0 && typeof res.stdout === "string") {
    const stale = new Set();
    let curPath = "";
    let curId = "";
    const flush = () => {
      if (curPath && (acceptedBundleIds.has(curId) || curId === legacyBundleId)) {
        if (resolve(curPath) !== canonical) stale.add(curPath);
      }
      curPath = "";
      curId = "";
    };
    for (const rawLine of res.stdout.split("\n")) {
      const line = rawLine.replace(/\r$/, "");
      if (line.startsWith("--------------------------------------------------------------------------------")) {
        flush();
        continue;
      }
      if (!curPath && line.startsWith("path:")) {
        curPath = line
          .slice(5)
          .replace(/\s+\(0x[0-9a-fA-F]+\)\s*$/, "")
          .trim();
      } else if (!curId && line.startsWith("identifier:")) {
        curId = line
          .slice(11)
          .replace(/\s+\(0x[0-9a-fA-F]+\)\s*$/, "")
          .trim();
      }
    }
    flush();
    for (const path of stale) {
      unregisterMacBundle(path);
    }
  }
  if (existsSync(canonical)) {
    spawnSync(lsregisterPath, ["-f", canonical], { stdio: "ignore" });
  }
}

export function sameVolumeTemp(path) {
  const root = dirname(path);
  mkdirSync(root, { recursive: true });
  const staging = join(root, `.block-install.${process.pid}.${randomBytes(4).toString("hex")}`);
  mkdirSync(staging);
  markNeverIndex(staging);
  return staging;
}

export function workspaceTemp() {
  const prefix = `block-app.${process.pid}.`;
  let dir;
  try {
    dir = mkdtempSync(join(tmpdir(), prefix));
  } catch (error) {
    if (!["ENOENT", "ENOTDIR", "EACCES", "EPERM", "EROFS"].includes(error?.code)) throw error;
    // A runner can remove its private TMPDIR before the build starts.
    dir = mkdtempSync(join("/tmp", prefix));
  }
  markNeverIndex(dir);
  return dir;
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
