import { copyFile } from "node:fs/promises";
import { resolve } from "node:path";
import { defineConfig } from "vitepress";
import { sourceLinks } from "./source-links.mjs";

const docsRoot = resolve(__dirname, "..");
const repo = "https://github.com/eachann1024/block";
const aiGuide = "plugin-dev-for-ai.md";
const llmsTxt = "llms.txt";

export default defineConfig({
  lang: "zh-CN",
  title: "Block 文档",
  titleTemplate: ":title · Block",
  description: "Block 插件开发文档：清单、协议、界面扩展、语言扩展与上架流程。",
  base: "/",
  cleanUrls: true,
  lastUpdated: true,
  srcExclude: [aiGuide, llmsTxt, "node_modules/**"],
  rewrites: {
    "README.md": "index.md",
    "plugins/README.md": "plugins/index.md",
  },
  head: [["link", { rel: "icon", href: "https://block.cooool.top/favicon.ico" }]],
  vite: {
    plugins: [sourceLinks(docsRoot, [aiGuide])],
  },
  markdown: {
    lineNumbers: false,
  },
  themeConfig: {
    siteTitle: "Block 文档",
    nav: [
      { text: "插件开发", link: "/plugins/" },
      { text: "AI 全量规范", link: `https://docs.block.cooool.top/${aiGuide}`, target: "_blank" },
      { text: "官网", link: "https://block.cooool.top" },
    ],
    sidebar: {
      "/": [
        { text: "文档中心", link: "/" },
        {
          text: "插件开发",
          items: [
            { text: "1. 开发前必读与架构边界", link: "/plugins/" },
            { text: "2. 清单与权限", link: "/plugins/manifest" },
            { text: "3. 协议、UI 与骨架", link: "/plugins/protocol-and-ui" },
            { text: "4. 宿主界面扩展", link: "/plugins/ui-contributions" },
            { text: "5. 语言扩展", link: "/plugins/languages" },
            { text: "6. 检查与上架", link: "/plugins/check-and-store" },
          ],
        },
      ],
    },
    outline: { level: [2, 3], label: "本页目录" },
    socialLinks: [{ icon: "github", link: repo }],
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
    search: {
      provider: "local",
      options: {
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
  async buildEnd(site) {
    await Promise.all([
      copyFile(resolve(docsRoot, aiGuide), resolve(site.outDir, aiGuide)),
      copyFile(resolve(docsRoot, llmsTxt), resolve(site.outDir, llmsTxt)),
    ]);
  },
});
