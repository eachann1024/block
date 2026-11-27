import { dirname, relative, resolve } from "node:path";

const REPO_BLOB = "https://github.com/eachann1024/block/blob/main";
const SKIP_BLOCK_RE = /<!--\s*ai-guide:skip-start\s*-->([\s\S]*?)<!--\s*ai-guide:skip-end\s*-->/g;
const MD_LINK_RE = /\]\(([^)\s]+?\.md)(#[^)\s]*)?\)/g;

function stripHandNav(block) {
  return block
    .split("\n")
    .filter((line) => {
      const text = line.trim();
      if (/^>\s*\[/.test(text)) return false;
      if (/^-{3,}$/.test(text)) return false;
      if (/^下一步[:：]/.test(text)) return false;
      if (/^(Next|Previous)[:：]/i.test(text)) return false;
      return true;
    })
    .join("\n");
}

export function sourceLinks(docsRoot, standalone) {
  return {
    name: "block-docs-source-links",
    enforce: "pre",
    transform(code, id) {
      const file = id.split("?")[0];
      if (!file.endsWith(".md") || !file.startsWith(docsRoot)) return null;
      let title = "";
      const body = code.replace(SKIP_BLOCK_RE, (_, inner) => {
        const kept = stripHandNav(inner);
        if (!/^\s*#{2}\s/m.test(code.replace(SKIP_BLOCK_RE, ""))) return kept;
        return kept.replace(/^#\s+(.+)$/m, (_, heading) => {
          title ||= heading.replace(/`/g, "").trim();
          return "";
        });
      });
      const withTitle = title && !code.startsWith("---")
        ? `---\ntitle: ${JSON.stringify(title)}\n---\n${body}`
        : body;
      return withTitle.replace(MD_LINK_RE, (match, target, hash = "") => {
        if (/^[a-z][a-z0-9+.-]*:/i.test(target)) return match;
        const absolute = resolve(dirname(file), target);
        const fromDocs = relative(docsRoot, absolute);
        const outside = fromDocs.startsWith("..");
        if (!outside && !standalone.includes(fromDocs)) {
          if (!/(^|\/)README\.md$/i.test(target)) return match;
          return `](${target.replace(/README\.md$/i, "index.md")}${hash})`;
        }
        const repoPath = relative(resolve(docsRoot, ".."), absolute);
        return `](${REPO_BLOB}/${repoPath}${hash})`;
      });
    },
  };
}
