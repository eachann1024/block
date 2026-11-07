import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
export { INSTALLERS, RELEASE_ASSETS, assertReleaseAsset } from "./allowed-assets.mjs";

const CHECKSUM_NAME = "SHA256SUMS.txt";

export function sha256File(filePath) {
  return createHash("sha256").update(readFileSync(filePath)).digest("hex");
}

export function writeChecksums(dir, names) {
  const sums = names.map((name) => `${sha256File(join(dir, name))}  ${name}\n`).join("");
  writeFileSync(join(dir, CHECKSUM_NAME), sums, "utf8");
}
