import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
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
