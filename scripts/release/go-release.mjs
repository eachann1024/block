import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { feedbackBuildHash, feedbackLdflags, resolveFeedbackBuildConfig } from "../app-bundle.mjs";
import { ensureWindowsIconSyso } from "./gen-windows-syso.mjs";
import { applyReleasePlanVersion, normalizeReleaseChannel, readPackageVersion, ROOT } from "./select-release.mjs";

function resolveReleaseChannel() {
  const envCh = process.env.BLOCK_RELEASE_CHANNEL?.trim();
  if (envCh) return normalizeReleaseChannel(envCh);
  const planPath = resolve(ROOT, "release-plan.json");
  if (existsSync(planPath)) {
    try {
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      if (plan?.channel) return normalizeReleaseChannel(plan.channel);
    } catch {}
  }
  if (process.env.BLOCK_RELEASE_VERSION?.trim()) return "nightly";
  return "stable";
}

function redactCmdArg(arg) {
  return String(arg).startsWith("-ldflags=") ? "-ldflags=<redacted>" : String(arg);
}

function redactBuildOutput(text) {
  let out = String(text || "");
  if (!out) return out;
  try {
    const { endpoint, clientToken } = resolveFeedbackBuildConfig(ROOT);
    if (endpoint) out = out.replaceAll(endpoint, "<redacted-endpoint>");
    if (clientToken) out = out.replaceAll(clientToken, "<redacted-token>");
  } catch {}
  return out;
}

export function run(cmd, args, extra = {}) {
  const hasLdflags = args.some((arg) => String(arg).startsWith("-ldflags="));
  const opts = hasLdflags
    ? { stdio: ["ignore", "pipe", "pipe"], encoding: "utf8", maxBuffer: 64 * 1024 * 1024, cwd: ROOT, ...extra }
    : { stdio: "inherit", cwd: ROOT, ...extra };
  const result = spawnSync(cmd, args, opts);
  if (hasLdflags) {
    if (result.stdout) process.stdout.write(redactBuildOutput(result.stdout));
    if (result.stderr) process.stderr.write(redactBuildOutput(result.stderr));
  }
  if (result.error) {
    const code = result.error.code ? ` (${result.error.code})` : "";
    throw new Error(`${cmd} ${args.map(redactCmdArg).join(" ")} failed to start${code}`);
  }
  if (result.status !== 0) throw new Error(`${cmd} ${args.map(redactCmdArg).join(" ")} exited ${result.status}`);
}

export function releaseBinaryPath(goos, goarch) {
  const ext = goos === "windows" ? ".exe" : "";
  return resolve(ROOT, "dist", "release-bin", `${goos}-${goarch}`, `block${ext}`);
}

function releaseBinaryMetaPath(outFile) {
  return `${outFile}.meta.json`;
}

function currentHeadSha() {
  try {
    return execFileSync("git", ["rev-parse", "HEAD"], { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return "";
  }
}

export function canReuseReleaseBinary({ goos, goarch, version, channel = resolveReleaseChannel(), outFile = releaseBinaryPath(goos, goarch) }) {
  if (!existsSync(outFile)) return false;
  const metaPath = releaseBinaryMetaPath(outFile);
  if (!existsSync(metaPath)) return false;
  try {
    const meta = JSON.parse(readFileSync(metaPath, "utf8"));
    return meta?.version === version
      && (meta?.channel || "stable") === channel
      && meta?.goos === goos
      && meta?.goarch === goarch
      && meta?.sha === currentHeadSha()
      && (meta?.feedbackHash || "") === feedbackBuildHash(ROOT);
  } catch {
    return false;
  }
}

export function goRelease({ goos, goarch, outFile = releaseBinaryPath(goos, goarch) }) {
  mkdirSync(dirname(outFile), { recursive: true });
  if (goos === "windows" && goarch === "amd64") {
    ensureWindowsIconSyso();
  }
  const version = readPackageVersion(ROOT);
  const channel = resolveReleaseChannel();
  const ldflagParts = [
    "-s",
    "-w",
    ...(goos === "windows" ? ["-H=windowsgui"] : []),
    `-X block/internal/updater.BuildVersion=${version}`,
    `-X block/internal/updater.BuildChannel=${channel}`,
    ...feedbackLdflags(ROOT),
  ];
  run("go", ["build", `-ldflags=${ldflagParts.join(" ")}`, "-o", outFile, "./cmd/block"], {
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
  writeFileSync(
    releaseBinaryMetaPath(outFile),
    `${JSON.stringify({ version, channel, goos, goarch, sha: currentHeadSha(), feedbackHash: feedbackBuildHash(ROOT) }, null, 2)}\n`,
  );
  return outFile;
}

const isMain =
  import.meta.main ??
  (Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url));

if (isMain) {
  const [goos, goarch] = process.argv.slice(2);
  if (!goos || !goarch) {
    throw new Error("Usage: node scripts/release/go-release.mjs <goos> <goarch>");
  }
  if (process.env.BLOCK_SKIP_VERSION_SYNC !== "1") {
    applyReleasePlanVersion();
  }
  const out = goRelease({ goos, goarch });
  console.log(out);
}
