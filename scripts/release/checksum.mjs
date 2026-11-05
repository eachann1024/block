import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export const INSTALLERS = [
  "block-macos.dmg",
  "block-windows-x64-setup.exe",
  "block-linux-x64.AppImage",
];

export function sha256File(filePath) {
  return createHash("sha256").update(readFileSync(filePath)).digest("hex");
}

export function writeChecksums(dir, names) {
  const sums = names.map((name) => `${sha256File(join(dir, name))}  ${name}\n`).join("");
  writeFileSync(join(dir, "SHA256SUMS.txt"), sums, "utf8");
}
