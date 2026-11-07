import { spawnSync } from "node:child_process";
import { chmodSync, copyFileSync, existsSync, mkdirSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export function buildDictation(destination) {
  const source = fileURLToPath(new URL("./block-dictation.swift", import.meta.url));
  const root = fileURLToPath(new URL("..", import.meta.url));
  const cargoTarget = process.env.CARGO_BUILD_TARGET;
  const targetArch = cargoTarget ? cargoTarget.split("-")[0] : process.arch;
  const arch = { aarch64: "arm64", arm64: "arm64", x86_64: "x86_64", x64: "x86_64" }[targetArch];
  if (!arch || (cargoTarget && !/^(aarch64|x86_64)-apple-darwin$/.test(cargoTarget))) {
    throw new Error(`unsupported macOS dictation target: ${cargoTarget || process.arch}`);
  }
  const minimum = process.env.MACOSX_DEPLOYMENT_TARGET || "12.0";
  if (!/^\d+\.\d+(?:\.\d+)?$/.test(minimum)) throw new Error(`invalid MACOSX_DEPLOYMENT_TARGET: ${minimum}`);
  const target = `${arch}-apple-macosx${minimum}`;
  const cachePath = join(root, "target", "dictation-cache", `block-dictation-${target}`);
  if (existsSync(cachePath) && statSync(cachePath).mtimeMs >= statSync(source).mtimeMs) {
    copyFileSync(cachePath, destination);
    chmodSync(destination, 0o755);
    return;
  }
  mkdirSync(dirname(cachePath), { recursive: true });
  const result = spawnSync("xcrun", ["swiftc", "-O", "-target", target, source, "-o", cachePath], { encoding: "utf8" });
  if (result.error || result.status !== 0) throw result.error || new Error(result.stderr || "Swift helper compilation failed");
  const signed = spawnSync("codesign", ["--force", "--sign", "-", cachePath], { encoding: "utf8" });
  if (signed.error || signed.status !== 0) throw signed.error || new Error(signed.stderr || "Swift helper signing failed");
  chmodSync(cachePath, 0o755);
  copyFileSync(cachePath, destination);
  chmodSync(destination, 0o755);
}
