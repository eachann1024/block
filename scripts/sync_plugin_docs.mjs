#!/usr/bin/env node
/**
 * Composes docs/plugin-dev-for-ai.md from the modular chapters in docs/plugins/.
 * Usage:
 *   node scripts/sync_plugin_docs.mjs          # regenerate docs/plugin-dev-for-ai.md
 *   node scripts/sync_plugin_docs.mjs --check  # exit 1 if out of sync
 */
import { readFile, writeFile } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const OUTPUT = resolve(ROOT, "docs/plugin-dev-for-ai.md");

const CHAPTERS = [
  "docs/plugins/README.md",
  "docs/plugins/manifest.md",
  "docs/plugins/protocol-and-ui.md",
  "docs/plugins/ui-contributions.md",
  "docs/plugins/languages.md",
  "docs/plugins/check-and-store.md",
];

const HEADER = `# Block 应用商店插件开发指南（AI 自包含规范 v1 · \`blockmd\`）

> 本文由 \`bun run docs:sync\` 自动从 [\`docs/plugins/\`](https://github.com/eachann1024/block/tree/main/docs/plugins) 各章节拼装生成，与 GitHub 仓库文档及在线文档站 [docs.block.cooool.top/plugins](https://docs.block.cooool.top/plugins/) 保持同源同步。

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。`;

const SKIP_BLOCK_RE = /<!--\s*ai-guide:skip-start\s*-->[\s\S]*?<!--\s*ai-guide:skip-end\s*-->/g;

const CHAPTER_LINK_RE = /\]\((README|manifest|protocol-and-ui|ui-contributions|languages|check-and-store)\.md(#[^)\s]*)?\)/g;

function extractChapter(raw) {
  return raw
    .replace(SKIP_BLOCK_RE, "")
    .replace(CHAPTER_LINK_RE, (_, name, anchor = "") => {
      const base =
        name === "README"
          ? "https://docs.block.cooool.top/plugins/"
          : `https://docs.block.cooool.top/plugins/${name}`;
      return `](${base}${anchor})`;
    })
    .trim();
}

async function compose() {
  const parts = [HEADER];
  for (const rel of CHAPTERS) {
    const raw = await readFile(resolve(ROOT, rel), "utf8");
    const body = extractChapter(raw);
    if (!body) {
      throw new Error(`Empty chapter body in ${rel}`);
    }
    parts.push(body);
  }
  return parts.join("\n\n---\n\n") + "\n";
}

const checkOnly = process.argv.includes("--check");
const generated = await compose();

if (checkOnly) {
  let existing = "";
  try {
    existing = await readFile(OUTPUT, "utf8");
  } catch (err) {
    if (err.code !== "ENOENT") throw err;
  }
  if (existing !== generated) {
    console.error(
      "docs-sync: docs/plugin-dev-for-ai.md is out of sync with docs/plugins/*.md; run `bun run docs:sync`",
    );
    process.exit(1);
  }
  process.exit(0);
}

await writeFile(OUTPUT, generated, "utf8");
console.log(`Synced docs/plugin-dev-for-ai.md (${generated.length} chars from ${CHAPTERS.length} chapters)`);
