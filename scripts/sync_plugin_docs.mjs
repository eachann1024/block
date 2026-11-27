#!/usr/bin/env node
/**
 * Composes bilingual self-contained AI specs from modular plugin chapters:
 *   - docs/plugin-dev-for-ai.md    from docs/plugins/*.md
 *   - docs/en/plugin-dev-for-ai.md from docs/en/plugins/*.md
 *
 * Usage:
 *   node scripts/sync_plugin_docs.mjs          # regenerate both outputs
 *   node scripts/sync_plugin_docs.mjs --check  # exit 1 if any output is out of sync
 */
import { readFile, writeFile } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const CHAPTER_FILES = [
  "README.md",
  "manifest.md",
  "protocol-and-ui.md",
  "ui-contributions.md",
  "languages.md",
  "check-and-store.md",
];

const TARGETS = [
  {
    name: "zh",
    output: "docs/plugin-dev-for-ai.md",
    chapterDir: "docs/plugins",
    siteBase: "https://docs.block.cooool.top/plugins",
    header: `# Block 应用商店插件开发指南（AI 自包含规范 v1 · \`blockmd\`）

> 本文由 \`bun run docs:sync\` 自动从 [\`docs/plugins/\`](https://github.com/eachann1024/block/tree/main/docs/plugins) 各章节拼装生成，与 GitHub 仓库文档及在线文档站 [docs.block.cooool.top/plugins](https://docs.block.cooool.top/plugins/) 保持同源同步。英文版见 [en/plugin-dev-for-ai.md](https://docs.block.cooool.top/en/plugin-dev-for-ai.md)。

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。`,
  },
  {
    name: "en",
    output: "docs/en/plugin-dev-for-ai.md",
    chapterDir: "docs/en/plugins",
    siteBase: "https://docs.block.cooool.top/en/plugins",
    header: `# Block App Store Plugin Developer Guide (Self-Contained AI Spec v1 · \`blockmd\`)

> Generated automatically by \`bun run docs:sync\` from [\`docs/en/plugins/\`](https://github.com/eachann1024/block/tree/main/docs/en/plugins) and kept in sync with the documentation site at [docs.block.cooool.top/en/plugins/](https://docs.block.cooool.top/en/plugins/). Chinese version: [plugin-dev-for-ai.md](https://docs.block.cooool.top/plugin-dev-for-ai.md).

This document is the authoritative self-contained specification for building, validating, and publishing **Block** App Store plugins. Developers and AI coding assistants can build an installable plugin with native Ocean Paper settings UI that passes \`block-plugin-check\` from this file alone.`,
  },
];

const SKIP_BLOCK_RE = /<!--\s*ai-guide:skip-start\s*-->[\s\S]*?<!--\s*ai-guide:skip-end\s*-->/g;

const CHAPTER_LINK_RE = /\]\((README|manifest|protocol-and-ui|ui-contributions|languages|check-and-store)\.md(#[^)\s]*)?\)/g;

function extractChapter(raw, siteBase) {
  return raw
    .replace(SKIP_BLOCK_RE, "")
    .replace(CHAPTER_LINK_RE, (_, name, anchor = "") => {
      const base = name === "README" ? `${siteBase}/` : `${siteBase}/${name}`;
      return `](${base}${anchor})`;
    })
    .trim();
}

async function composeTarget(target) {
  const parts = [target.header];
  for (const file of CHAPTER_FILES) {
    const rel = `${target.chapterDir}/${file}`;
    const raw = await readFile(resolve(ROOT, rel), "utf8");
    const body = extractChapter(raw, target.siteBase);
    if (!body) {
      throw new Error(`Empty chapter body in ${rel}`);
    }
    parts.push(body);
  }
  return parts.join("\n\n---\n\n") + "\n";
}

const checkOnly = process.argv.includes("--check");
let outOfSync = false;

for (const target of TARGETS) {
  const generated = await composeTarget(target);
  const outPath = resolve(ROOT, target.output);
  if (checkOnly) {
    let existing = "";
    try {
      existing = await readFile(outPath, "utf8");
    } catch (err) {
      if (err.code !== "ENOENT") throw err;
    }
    if (existing !== generated) {
      console.error(
        `docs-sync: ${target.output} is out of sync with ${target.chapterDir}/*.md; run \`bun run docs:sync\``,
      );
      outOfSync = true;
    }
  } else {
    await writeFile(outPath, generated, "utf8");
    console.log(`Synced ${target.output} (${generated.length} chars from ${CHAPTER_FILES.length} chapters)`);
  }
}

if (outOfSync) {
  process.exit(1);
}
