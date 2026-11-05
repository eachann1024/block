import assert from "node:assert/strict";
import { existsSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { INSTALLERS, writeChecksums } from "./checksum.mjs";
import { ROOT } from "./select-release.mjs";

const dir = resolve(ROOT, "dist/release");
for (const name of readdirSync(dir)) {
  assert.ok(!/\.(zip|tar\.gz|tgz)$/i.test(name), `Release directory must not contain archives: ${name}`);
}
for (const name of INSTALLERS) {
  assert.ok(existsSync(join(dir, name)), `Missing installer ${name}`);
}
writeChecksums(dir, INSTALLERS);
console.log(`Wrote ${join(dir, "SHA256SUMS.txt")}`);
