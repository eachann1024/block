import { spawnSync } from "node:child_process";
import { ROOT } from "./select-release.mjs";

export function run(cmd, args, extra = {}) {
  const result = spawnSync(cmd, args, { stdio: "inherit", cwd: ROOT, ...extra });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${cmd} ${args.join(" ")} exited ${result.status}`);
}

export function cargoRelease() {
  run("cargo", ["build", "--release", "--locked"]);
}
