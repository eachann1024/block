import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export const INSTALLERS = [
  "block-macos-arm64.dmg",
  "block-macos-x64.dmg",
  "block-windows-x64-setup.exe",
  "block-windows-arm64-setup.exe",
  "block-linux-x64.AppImage",
  "block-linux-arm64.AppImage",
  "block-linux-x64.deb",
  "block-linux-arm64.deb",
  "block-linux-x64.rpm",
  "block-linux-arm64.rpm",
];

export const RELEASE_ASSETS = [...INSTALLERS, "SHA256SUMS.txt"];

export function assertReleaseAsset(name) {
  if (!RELEASE_ASSETS.includes(name)) {
    throw new Error(`Refusing to upload ${name}; it is not on the release allowlist`);
  }
}

export function sha256File(filePath) {
  return createHash("sha256").update(readFileSync(filePath)).digest("hex");
}

export function writeChecksums(dir, names) {
  const sums = names.map((name) => `${sha256File(join(dir, name))}  ${name}\n`).join("");
  writeFileSync(join(dir, "SHA256SUMS.txt"), sums, "utf8");
}
