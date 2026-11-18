import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { ROOT } from "./select-release.mjs";

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
  const ldflags = goos === "windows" ? "-s -w -H=windowsgui" : "-s -w";
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
