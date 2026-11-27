import { copyFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { defineConfig } from "vitepress";
import { sourceLinks } from "./source-links.mjs";

const docsRoot = resolve(__dirname, "..");
const repo = "https://github.com/eachann1024/block";
const aiGuide = "plugin-dev-for-ai.md";
const zhAiGuide = "zh/plugin-dev-for-ai.md";
const llmsTxt = "llms.txt";
const zhLlmsTxt = "zh/llms.txt";

const LEGACY_EN_REDIRECTS: Array<[string, string]> = [
  ["en/index.html", "/"],
  ["en/plugins/index.html", "/plugins/"],
  ["en/plugins/manifest.html", "/plugins/manifest"],
  ["en/plugins/protocol-and-ui.html", "/plugins/protocol-and-ui"],
  ["en/plugins/ui-contributions.html", "/plugins/ui-contributions"],
  ["en/plugins/languages.html", "/plugins/languages"],
  ["en/plugins/check-and-store.html", "/plugins/check-and-store"],
];

function redirectHtml(target: string): string {
  const url = `https://docs.block.cooool.top${target}`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="0; url=${target}">
<link rel="canonical" href="${url}">
<title>Redirecting…</title>
<script>location.replace(${JSON.stringify(target)} + location.hash);</script>
</head>
<body>
<p>Redirecting to <a href="${target}">${url}</a>…</p>
</body>
</html>
`;
}

export default defineConfig({
  title: "Block Docs",
  titleTemplate: ":title · Block",
  base: "/",
  cleanUrls: true,
  lastUpdated: true,
  srcExclude: [aiGuide, zhAiGuide, llmsTxt, zhLlmsTxt, "node_modules/**"],
  rewrites: {
    "README.md": "index.md",
    "plugins/README.md": "plugins/index.md",
    "zh/README.md": "zh/index.md",
    "zh/plugins/README.md": "zh/plugins/index.md",
  },
  transformPageData(pageData) {
    if (pageData.relativePath === "index.md" || pageData.relativePath === "zh/index.md") {
      pageData.frontmatter.layout = "DocsHome";
      pageData.frontmatter.sidebar = false;
      pageData.frontmatter.aside = false;
    }
  },
  head: [["link", { rel: "icon", href: "https://block.cooool.top/favicon.ico" }]],
  vite: {
    plugins: [sourceLinks(docsRoot, [aiGuide, zhAiGuide])],
  },
  markdown: {
    lineNumbers: false,
  },
  locales: {
    root: {
      label: "English",
      lang: "en-US",
      title: "Block Docs",
      description: "Block plugin developer documentation: manifest, JSON-RPC protocol, UI contributions, language extensions, and store publishing.",
      themeConfig: {
        siteTitle: "Block Docs",
        nav: [
          { text: "Plugin Guide", link: "/plugins/" },
          { text: "AI Full Spec", link: `https://docs.block.cooool.top/${aiGuide}`, target: "_blank" },
          { text: "Website", link: "https://block.cooool.top/en/" },
        ],
        sidebar: {
          "/": [
            { text: "Documentation", link: "/" },
            {
              text: "Plugin Development",
              items: [
                { text: "1. Overview & AI Guide", link: "/plugins/" },
                { text: "2. Manifest & Permissions", link: "/plugins/manifest" },
                { text: "3. Protocol, UI & SDK", link: "/plugins/protocol-and-ui" },
                { text: "4. Host UI Contributions", link: "/plugins/ui-contributions" },
                { text: "5. Language Extensions", link: "/plugins/languages" },
                { text: "6. Check & Store", link: "/plugins/check-and-store" },
              ],
            },
          ],
        },
        outline: { level: [2, 3], label: "On this page" },
        editLink: { pattern: `${repo}/edit/main/docs/:path`, text: "Edit this page on GitHub" },
        lastUpdated: { text: "Last updated", formatOptions: { dateStyle: "medium", timeStyle: "short" } },
        docFooter: { prev: "Previous", next: "Next" },
        darkModeSwitchLabel: "Appearance",
        lightModeSwitchTitle: "Switch to light theme",
        darkModeSwitchTitle: "Switch to dark theme",
        sidebarMenuLabel: "Menu",
        returnToTopLabel: "Return to top",
        langMenuLabel: "Language",
        notFound: {
          title: "Page not found",
          quote: "This page may have been moved or renamed.",
          linkText: "Back to documentation",
        },
      },
    },
    zh: {
      label: "简体中文",
      lang: "zh-CN",
      link: "/zh/",
      title: "Block 文档",
      description: "Block 插件开发文档：清单、协议、界面扩展、语言扩展与上架流程。",
      themeConfig: {
        siteTitle: "Block 文档",
        nav: [
          { text: "插件开发", link: "/zh/plugins/" },
          { text: "AI 全量规范", link: `https://docs.block.cooool.top/${zhAiGuide}`, target: "_blank" },
          { text: "官网", link: "https://block.cooool.top" },
        ],
        sidebar: {
          "/zh/": [
            { text: "文档中心", link: "/zh/" },
            {
              text: "插件开发",
              items: [
                { text: "1. 开发前必读与 AI 指南", link: "/zh/plugins/" },
                { text: "2. 清单与权限", link: "/zh/plugins/manifest" },
                { text: "3. 协议、UI 与骨架", link: "/zh/plugins/protocol-and-ui" },
                { text: "4. 宿主界面扩展", link: "/zh/plugins/ui-contributions" },
                { text: "5. 语言扩展", link: "/zh/plugins/languages" },
                { text: "6. 检查与上架", link: "/zh/plugins/check-and-store" },
              ],
            },
          ],
        },
        outline: { level: [2, 3], label: "本页目录" },
        editLink: { pattern: `${repo}/edit/main/docs/:path`, text: "在 GitHub 上编辑此页" },
        lastUpdated: { text: "最后更新", formatOptions: { dateStyle: "medium", timeStyle: "short" } },
        docFooter: { prev: "上一章", next: "下一章" },
        darkModeSwitchLabel: "外观",
        lightModeSwitchTitle: "切换到浅色",
        darkModeSwitchTitle: "切换到深色",
        sidebarMenuLabel: "菜单",
        returnToTopLabel: "回到顶部",
        langMenuLabel: "语言",
        notFound: { title: "页面不存在", quote: "这一页可能已被移动或改名。", linkText: "回到文档中心" },
      },
    },
  },
  themeConfig: {
    socialLinks: [{ icon: "github", link: repo }],
    search: {
      provider: "local",
      options: {
        locales: {
          zh: {
            translations: {
              button: { buttonText: "搜索文档", buttonAriaLabel: "搜索文档" },
              modal: {
                noResultsText: "没有找到相关结果",
                resetButtonTitle: "清除",
                footer: { selectText: "打开", navigateText: "切换", closeText: "关闭" },
              },
            },
          },
        },
      },
    },
  },
  async buildEnd(site) {
    await Promise.all([
      mkdir(resolve(site.outDir, "zh"), { recursive: true }),
      mkdir(resolve(site.outDir, "en/plugins"), { recursive: true }),
    ]);
    await Promise.all([
      copyFile(resolve(docsRoot, aiGuide), resolve(site.outDir, aiGuide)),
      copyFile(resolve(docsRoot, llmsTxt), resolve(site.outDir, llmsTxt)),
      copyFile(resolve(docsRoot, zhAiGuide), resolve(site.outDir, zhAiGuide)),
      copyFile(resolve(docsRoot, zhLlmsTxt), resolve(site.outDir, zhLlmsTxt)),
      // Keep legacy /en/* URLs working without 404 for existing clients and external links
      copyFile(resolve(docsRoot, aiGuide), resolve(site.outDir, "en", aiGuide)),
      copyFile(resolve(docsRoot, llmsTxt), resolve(site.outDir, "en", llmsTxt)),
      ...LEGACY_EN_REDIRECTS.map(([file, target]) =>
        writeFile(resolve(site.outDir, file), redirectHtml(target), "utf8"),
      ),
    ]);
  },
});
