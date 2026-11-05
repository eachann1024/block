import assert from "node:assert/strict";
import { existsSync, readdirSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { DELTA_ASSETS, INSTALLERS, writeChecksums } from "./checksum.mjs";
import { ROOT } from "./select-release.mjs";

const dir = resolve(ROOT, "dist/release");
for (const name of readdirSync(dir)) {
  if (/\.(zip|tar\.gz|tgz|blockmap|zsync)$/i.test(name)) rmSync(join(dir, name), { force: true });
}
for (const name of INSTALLERS) {
  assert.ok(existsSync(join(dir, name)), `Missing installer ${name}`);
}
const presentDelta = DELTA_ASSETS.filter((name) => existsSync(join(dir, name)));
const allAssets = [...INSTALLERS, ...presentDelta];
writeChecksums(dir, allAssets);
const allowed = new Set([...INSTALLERS, ...DELTA_ASSETS, "SHA256SUMS.txt"]);
for (const name of readdirSync(dir)) {
  if (!allowed.has(name)) rmSync(join(dir, name), { force: true });
}
console.log(`Wrote ${join(dir, "SHA256SUMS.txt")}`);
