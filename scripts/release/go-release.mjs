import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { applyReleasePlanVersion, readPackageVersion, ROOT } from "./select-release.mjs";

export function run(cmd, args, extra = {}) {
  const result = spawnSync(cmd, args, { stdio: "inherit", cwd: ROOT, ...extra });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${cmd} ${args.join(" ")} exited ${result.status}`);
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

export function canReuseReleaseBinary({ goos, goarch, version, outFile = releaseBinaryPath(goos, goarch) }) {
  if (!existsSync(outFile)) return false;
  const metaPath = releaseBinaryMetaPath(outFile);
  if (!existsSync(metaPath)) return false;
  try {
    const meta = JSON.parse(readFileSync(metaPath, "utf8"));
    return meta?.version === version
      && meta?.goos === goos
      && meta?.goarch === goarch
      && meta?.sha === currentHeadSha();
  } catch {
    return false;
  }
}

export function goRelease({ goos, goarch, outFile = releaseBinaryPath(goos, goarch) }) {
  mkdirSync(dirname(outFile), { recursive: true });
  const version = readPackageVersion(ROOT);
  const verFlag = `-X block/internal/updater.BuildVersion=${version}`;
  const ldflags = goos === "windows" ? `-s -w -H=windowsgui ${verFlag}` : `-s -w ${verFlag}`;
  run("go", ["build", `-ldflags=${ldflags}`, "-o", outFile, "./cmd/block"], {
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
  writeFileSync(
    releaseBinaryMetaPath(outFile),
    `${JSON.stringify({ version, goos, goarch, sha: currentHeadSha() }, null, 2)}\n`,
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
