import assert from "node:assert/strict";
import { RELEASE_ASSETS } from "./allowed-assets.mjs";
import { gh, REPO } from "./select-release.mjs";

function listAssets(tag) {
  const out = gh(["release", "view", tag, "--repo", REPO, "--json", "assets", "--jq", ".assets[].name"]).trim();
  return out ? out.split("\n").map((name) => name.trim()).filter(Boolean) : [];
}

const tag = process.argv[2] || process.env.RELEASE_TAG;
assert.ok(tag, "RELEASE_TAG is required");
const allowed = new Set(RELEASE_ASSETS);

let names = listAssets(tag);
console.log(`Release ${tag} assets: ${names.join(", ") || "(none)"}`);
for (const name of names) {
  if (!allowed.has(name)) {
    console.log(`Deleting ${name}`);
    gh(["release", "delete-asset", tag, name, "--yes", "--repo", REPO]);
  }
}

names = listAssets(tag);
const extra = names.filter((name) => !allowed.has(name));
const missing = RELEASE_ASSETS.filter((name) => !names.includes(name));
if (extra.length || missing.length || names.length !== RELEASE_ASSETS.length) {
  console.error(`Release ${tag} is not exactly the allowlist of ${RELEASE_ASSETS.length} assets.`);
  console.error(`extra: ${extra.join(", ") || "(none)"}`);
  console.error(`missing: ${missing.join(", ") || "(none)"}`);
  console.error(`actual: ${names.join(", ") || "(none)"}`);
  process.exit(1);
}
console.log(`Release ${tag} has exactly ${names.length} allowlisted assets.`);
