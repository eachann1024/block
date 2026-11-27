<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useData, withBase } from "vitepress";
import AiWorkflowStage from "./AiWorkflowStage.vue";

const { localeIndex, isDark } = useData();
const isEn = computed(() => localeIndex.value !== "zh");

const copyState = ref<"idle" | "copied" | "failed">("idle");
const cardCopyState = ref<"idle" | "copied" | "failed">("idle");
const cmdCopyState = ref<"idle" | "copied" | "failed">("idle");
const stageOpen = ref(false);
const hubRef = ref<HTMLElement | null>(null);
const cardTipId = "bp-card-ai-tip";
const canvasRef = ref<HTMLCanvasElement | null>(null);
const heroRef = ref<HTMLElement | null>(null);

const content = computed(() => {
  if (isEn.value) {
    return {
      heroLine1: "Build native extensions",
      heroLine2: "for Block.",
      heroLeadStrong: "Built-in wazero & quickjs-ng sandbox,",
      heroLeadMuted:
        " bidirectional JSON-RPC 2.0, and 16 native Ocean Paper controls — publish from your own GitHub repo without PRs.",
      ctaMerged: "Copy for AI & Start Building",
      ctaMergedCopied: "Spec URL Copied · Paste to AI",
      ctaMergedFailed: "Copy Failed",
      ctaDemoToggle: "Watch Flow",
      ctaDemoClose: "Hide Flow",
      aiCardHint: "Just click once to copy the link, paste it with your prompt, and generate a complete plugin.",
      aiCopySuccess: "Link copied — paste with your prompt to any AI",
      aiCopyFailed: "Clipboard unavailable — copy the spec URL below:",
      aiCardBadgeIdle: "Click to copy",
      aiCardBadgeCopied: "✓ Copied",
      aiCardBadgeFailed: "Copy failed",
      aiCardRaw: "View raw",
      aiCardAria:
        "Copy AI-Ready Full Spec URL. Just click once to copy the link, paste it with your prompt, and generate a complete plugin.",
      aiSpecUrl: "https://docs.block.cooool.top/plugin-dev-for-ai.md",
      aiSpecRel: withBase("/plugin-dev-for-ai.md"),
      llmsRel: withBase("/llms.txt"),
      paths: [
        {
          id: "sandbox",
          title: "Sandboxed Plugins",
          desc: "Ship interactive extensions with entry.js or entry.wasm inside Block's built-in WASI & QuickJS runtime — no user-installed Python required.",
          link: withBase("/plugins/protocol-and-ui"),
          tag: "entry.js · entry.wasm",
          icon: "sandbox",
        },
        {
          id: "languages",
          title: "Language Extensions",
          desc: "Add syntax highlighting for additional formats with zero process, zero permissions, and automatic OS file associations.",
          link: withBase("/plugins/languages"),
          tag: "grammars/*.json",
          icon: "language",
        },
        {
          id: "ai-spec",
          title: "AI-Ready Full Spec",
          desc: "One self-contained Markdown file combining all 6 chapters, schemas, RPC limits, and a bilingual starter skeleton for coding agents.",
          link: "https://docs.block.cooool.top/plugin-dev-for-ai.md",
          external: true,
          tag: "plugin-dev-for-ai.md",
          icon: "ai",
        },
      ],
      metricsTitle: "Built for predictable local execution.",
      metricsSub: "Hard runtime limits enforced in the host so extensions stay fast, isolated, and safe.",
      metrics: [
        { value: "32 MiB", label: "WASI LINEAR MEMORY" },
        { value: "16", label: "NATIVE UI CONTROLS" },
        { value: "30", label: "SCOPED PERMISSIONS" },
        { value: "TOP 100", label: "AUTO-CRAWLED STORE" },
      ],
      chaptersStrong: "Block Docs",
      chaptersMuted: " covers every layer from manifest rules to automated store publishing.",
      chaptersBtn: "Open AI Spec",
      chapters: [
        {
          num: "01",
          title: "Overview & AI Guide",
          desc: "Three development paths, 8 hard rules, verification checklist, sandbox boundaries, and migration from entry.command.",
          meta: "3 paths · 8 rules · migration",
          link: withBase("/plugins/"),
        },
        {
          num: "02",
          title: "Manifest & Permissions",
          desc: "Directory layout, strict manifest schema, 30 permissions across 3 tiers, network_domains, and exec_allowlist.",
          meta: "manifest.json · 30 permissions",
          link: withBase("/plugins/manifest"),
        },
        {
          num: "03",
          title: "Protocol, UI & SDK",
          desc: "Bidirectional JSON-RPC 2.0, host.* capabilities, 16 declarative UIItem controls, and bilingual block-sdk.js skeleton.",
          meta: "JSON-RPC 2.0 · 16 UIItem",
          link: withBase("/plugins/protocol-and-ui"),
        },
        {
          num: "04",
          title: "Host UI Contributions",
          desc: "Multi-surface isolation, titlebar, menus, sidebars, backdrops, themes, menubar items, and offline WebView stages.",
          meta: "surfaces · contributes · stages",
          link: withBase("/plugins/ui-contributions"),
        },
        {
          num: "05",
          title: "Language Extensions",
          desc: "Zero-process RE2 stateful line tokenizers, 25 token types, and cross-platform Open With registration.",
          meta: "contributes.languages · RE2",
          link: withBase("/plugins/languages"),
        },
        {
          num: "06",
          title: "Check & Store",
          desc: "block-plugin-check CLI coverage, bilingual gate, blockmd-plugin auto-crawling, and desktop install verification.",
          meta: "block-plugin-check · store-index",
          link: withBase("/plugins/check-and-store"),
        },
      ],
      cliTitle: "Verify locally before publishing",
      cliSub: "Run the official protocol checker against your plugin directory.",
      cliCmd: "CGO_ENABLED=0 go -C /tmp/block run ./cmd/block-plugin-check /path/to/plugin",
      cliCopy: "Copy",
      cliCopied: "Copied",
      refsTitle: "Core References",
      refs: [
        { label: "Design System (DESIGN.md)", href: "https://github.com/eachann1024/block/blob/main/DESIGN.md", external: true },
        { label: "Project README", href: "https://github.com/eachann1024/block/blob/main/README.md", external: true },
        { label: "Agent Index (llms.txt)", href: "https://docs.block.cooool.top/llms.txt", external: true },
        { label: "Block Website", href: "https://block.cooool.top/en/", external: true },
        { label: "Privacy Policy", href: "https://block.cooool.top/en/privacy/", external: true },
        { label: "GitHub Repository", href: "https://github.com/eachann1024/block", external: true },
      ],
      footerLeft: "Block Docs · Apache-2.0",
      footerSwitchLabel: "简体中文",
      footerSwitchLink: withBase("/zh/"),
    };
  }
  return {
    heroLine1: "为 Block 构建",
    heroLine2: "原生级沙盒扩展。",
    heroLeadStrong: "内置 wazero 与 quickjs-ng 沙盒，",
    heroLeadMuted: "双向 JSON-RPC 2.0、16 种 Ocean Paper 原生控件，独立建仓无需提 PR 即可上架。",
    ctaMerged: "复制给 AI 开始开发",
    ctaMergedCopied: "已复制规范链接 · 发给 AI 即可",
    ctaMergedFailed: "复制失败",
    ctaDemoToggle: "查看流程动效",
    ctaDemoClose: "收起动效",
    aiCardHint: "只需要点一下复制链接，带上你的内容发给 AI，就能直接生成一个插件。",
    aiCopySuccess: "已复制链接，带上内容发给 AI 即可生成",
    aiCopyFailed: "剪贴板不可用，请手动复制下方链接：",
    aiCardBadgeIdle: "点击复制",
    aiCardBadgeCopied: "✓ 已复制",
    aiCardBadgeFailed: "复制失败",
    aiCardRaw: "查看原文",
    aiCardAria:
      "复制 AI 自包含全量规范链接：只需要点一下复制链接，带上你的内容发给 AI，就能直接生成一个插件。",
    aiSpecUrl: "https://docs.block.cooool.top/zh/plugin-dev-for-ai.md",
    aiSpecRel: withBase("/zh/plugin-dev-for-ai.md"),
    llmsRel: withBase("/zh/llms.txt"),
    paths: [
      {
        id: "sandbox",
        title: "交互式沙盒插件",
        desc: "通过 entry.js 或 entry.wasm 运行在内置 WASI 与 QuickJS 沙盒中，无需用户预装 Python 等外部运行时。",
        link: withBase("/zh/plugins/protocol-and-ui"),
        tag: "entry.js · entry.wasm",
        icon: "sandbox",
      },
      {
        id: "languages",
        title: "声明式语言扩展",
        desc: "零子进程、零权限，为代码块与源码编辑器补充小众语言高亮，并自动同步系统「打开方式」。",
        link: withBase("/zh/plugins/languages"),
        tag: "grammars/*.json",
        icon: "language",
      },
      {
        id: "ai-spec",
        title: "AI 自包含全量规范",
        desc: "一份自动同源拼装的纯文本规范，涵盖全部 6 章 Schema、RPC 边界与双语起步骨架，可直接交给 AI 构建插件。",
        link: "https://docs.block.cooool.top/zh/plugin-dev-for-ai.md",
        external: true,
        tag: "zh/plugin-dev-for-ai.md",
        icon: "ai",
      },
    ],
    metricsTitle: "为可预测的本地运行而设计。",
    metricsSub: "在宿主层统一收敛内存、权限与打包边界，让每个扩展都轻快、隔离、可控。",
    metrics: [
      { value: "32 MiB", label: "WASI 线性内存上限" },
      { value: "16", label: "原生声明式控件" },
      { value: "30", label: "分层权限与事件" },
      { value: "TOP 100", label: "免 PR 自动采集商店" },
    ],
    chaptersStrong: "Block 文档中心",
    chaptersMuted: "，六章覆盖清单、协议、界面扩展与上架流程。",
    chaptersBtn: "查看 AI 全量规范",
    chapters: [
      {
        num: "01",
        title: "开发前必读与 AI 指南",
        desc: "三类开发路径、8 条红线、四步验收清单、沙盒硬边界，以及旧版 entry.command 的中立迁移说明。",
        meta: "3 paths · 8 rules · migration",
        link: withBase("/zh/plugins/"),
      },
      {
        num: "02",
        title: "清单与权限",
        desc: "目录结构、严格 JSON 校验、30 项分层权限、network_domains SSRF 限制与 exec_allowlist 白名单。",
        meta: "manifest.json · 30 permissions",
        link: withBase("/zh/plugins/manifest"),
      },
      {
        num: "03",
        title: "协议、UI 与骨架",
        desc: "双向 JSON-RPC 2.0、host.* 宿主能力、16 种声明式 UIItem 控件与双语 block-sdk.js 同步起步骨架。",
        meta: "JSON-RPC 2.0 · 16 UIItem",
        link: withBase("/zh/plugins/protocol-and-ui"),
      },
      {
        num: "04",
        title: "宿主界面扩展",
        desc: "多表面状态隔离，挂载顶栏、右键菜单、侧栏、背景、主题、系统菜单栏与四层隔离离线 WebView 舞台。",
        meta: "surfaces · contributes · stages",
        link: withBase("/zh/plugins/ui-contributions"),
      },
      {
        num: "05",
        title: "语言扩展",
        desc: "零子进程 RE2 有状态行分词器、25 个合法 Token 名称与 macOS / Windows / Linux 文件关联机制。",
        meta: "contributes.languages · RE2",
        link: withBase("/zh/plugins/languages"),
      },
      {
        num: "06",
        title: "检查与上架",
        desc: "block-plugin-check 本地校验覆盖边界、双语门禁、blockmd-plugin 自动采集规则与桌面安装验真。",
        meta: "block-plugin-check · store-index",
        link: withBase("/zh/plugins/check-and-store"),
      },
    ],
    cliTitle: "发版前本地协议检查",
    cliSub: "一行命令在内置沙盒中实跑清单校验、渲染与动作检查。",
    cliCmd: "CGO_ENABLED=0 go -C /tmp/block run ./cmd/block-plugin-check /path/to/plugin",
    cliCopy: "复制",
    cliCopied: "已复制",
    refsTitle: "核心规范与入口",
    refs: [
      { label: "设计语言规范（DESIGN.md）", href: "https://github.com/eachann1024/block/blob/main/DESIGN.md", external: true },
      { label: "项目主介绍（README.zh-CN.md）", href: "https://github.com/eachann1024/block/blob/main/README.zh-CN.md", external: true },
      { label: "Agent 索引（zh/llms.txt）", href: "https://docs.block.cooool.top/zh/llms.txt", external: true },
      { label: "Block 官网", href: "https://block.cooool.top/", external: true },
      { label: "隐私政策", href: "https://block.cooool.top/privacy/", external: true },
      { label: "GitHub 仓库", href: "https://github.com/eachann1024/block", external: true },
    ],
    footerLeft: "Block 文档中心 · Apache-2.0",
    footerSwitchLabel: "English",
    footerSwitchLink: withBase("/"),
  };
});

type CopyTargetKey = "hero" | "card" | "cmd";

const copyTargets: Record<CopyTargetKey, typeof copyState> = {
  hero: copyState,
  card: cardCopyState,
  cmd: cmdCopyState,
};
const copyTimers = new Map<CopyTargetKey, number>();
let isUnmounted = false;

function scheduleCopyReset(key: CopyTargetKey, delayMs: number) {
  if (typeof window === "undefined" || isUnmounted) return;
  const prev = copyTimers.get(key);
  if (prev !== undefined) {
    window.clearTimeout(prev);
  }
  const target = copyTargets[key];
  const timerId = window.setTimeout(() => {
    if (!isUnmounted) {
      target.value = "idle";
    }
    copyTimers.delete(key);
  }, delayMs);
  copyTimers.set(key, timerId);
}

async function copyText(text: string, key: CopyTargetKey) {
  const target = copyTargets[key];
  if (
    typeof navigator === "undefined" ||
    !navigator.clipboard ||
    typeof window === "undefined" ||
    !window.isSecureContext
  ) {
    target.value = "failed";
    scheduleCopyReset(key, 6000);
    return;
  }
  try {
    await navigator.clipboard.writeText(text);
    if (isUnmounted) return;
    target.value = "copied";
    scheduleCopyReset(key, 2400);
  } catch {
    if (isUnmounted) return;
    target.value = "failed";
    scheduleCopyReset(key, 6000);
  }
}

let openStageTimer: number | null = null;
let closeStageTimer: number | null = null;

function clearStageTimers() {
  if (typeof window === "undefined") return;
  if (openStageTimer !== null) {
    window.clearTimeout(openStageTimer);
    openStageTimer = null;
  }
  if (closeStageTimer !== null) {
    window.clearTimeout(closeStageTimer);
    closeStageTimer = null;
  }
}

function openStage(immediate = false) {
  if (typeof window === "undefined" || isUnmounted) return;
  clearStageTimers();
  if (immediate || stageOpen.value) {
    stageOpen.value = true;
    return;
  }
  openStageTimer = window.setTimeout(() => {
    openStageTimer = null;
    if (!isUnmounted) stageOpen.value = true;
  }, 85);
}

function closeStage(immediate = false) {
  if (typeof window === "undefined" || isUnmounted) return;
  clearStageTimers();
  if (immediate) {
    stageOpen.value = false;
    return;
  }
  closeStageTimer = window.setTimeout(() => {
    closeStageTimer = null;
    if (!isUnmounted) stageOpen.value = false;
  }, 160);
}

function toggleStage() {
  clearStageTimers();
  stageOpen.value = !stageOpen.value;
}

function handleHubFocusOut(event: FocusEvent) {
  const hub = hubRef.value;
  const next = event.relatedTarget as Node | null;
  if (!hub || !next || !hub.contains(next)) {
    closeStage(true);
  }
}

function selectCodeElement(event: Event) {
  if (typeof window === "undefined" || typeof document === "undefined") return;
  const el = event.currentTarget as HTMLElement | null;
  if (!el) return;
  const sel = window.getSelection?.();
  if (!sel) return;
  const range = document.createRange();
  range.selectNodeContents(el);
  sel.removeAllRanges();
  sel.addRange(range);
}

// Subtle Base.org-inspired living pixel-block field in Ocean Paper tones (SSR-safe)
let rafId = 0;
let resizeObserver: ResizeObserver | null = null;
let mediaQuery: MediaQueryList | null = null;
let handleMotionChange: (() => void) | null = null;

function setupPixelField() {
  const canvas = canvasRef.value;
  const host = heroRef.value;
  if (!canvas || !host || typeof window === "undefined") return;
  const ctx = canvas.getContext("2d");
  if (!ctx) return;

  const cell = 10;
  const gap = 3;
  const step = cell + gap;
  let width = 0;
  let height = 0;
  let cols = 0;
  let rows = 0;
  let reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches ?? false;

  const resize = () => {
    const rect = host.getBoundingClientRect();
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    width = Math.max(1, Math.floor(rect.width));
    height = Math.max(1, Math.floor(rect.height));
    canvas.width = Math.floor(width * dpr);
    canvas.height = Math.floor(height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    cols = Math.ceil(width / step) + 1;
    rows = Math.ceil(height / step) + 1;
  };

  const drawFrame = (timeMs: number) => {
    ctx.clearRect(0, 0, width, height);
    const dark = isDark.value;
    const t = timeMs * 0.00045;
    const cx = width * 0.5;
    const cy = height * 0.48;
    const clearRx = Math.min(width * 0.34, 340);
    const clearRy = Math.min(height * 0.36, 175);

    const palette = dark
      ? ["#31456a", "#4e72b8", "#779de8", "#2d333d"]
      : ["#c9d8f3", "#9ab7e8", "#547dd0", "#dfe3ea"];

    for (let r = 0; r < rows; r++) {
      const y = r * step;
      for (let c = 0; c < cols; c++) {
        const x = c * step;
        // Keep central reading zone completely clean and airy
        const dx = (x - cx) / clearRx;
        const dy = (y - cy) / clearRy;
        const dist = dx * dx + dy * dy;
        if (dist < 1.08) continue;

        // Deterministic organic diagonal wave clusters (echoing Base.org's airy pixel ribbons)
        const wave1 = Math.sin(c * 0.21 + r * 0.17 - t * 1.6);
        const wave2 = Math.cos(c * 0.13 - r * 0.24 + t * 1.1);
        const hash = Math.sin(c * 12.9898 + r * 78.233) * 43758.5453;
        const frac = hash - Math.floor(hash);
        const density = wave1 * 0.55 + wave2 * 0.45;

        if (density < 0.64 || frac < 0.36) continue;

        const edgeFade = Math.min(1, (dist - 1.08) * 1.2);
        const alpha = Math.min(dark ? 0.42 : 0.5, (density - 0.6) * 1.15 * edgeFade);
        if (alpha <= 0.03) continue;

        const colorIdx = Math.floor(frac * palette.length) % palette.length;
        ctx.globalAlpha = alpha;
        ctx.fillStyle = palette[colorIdx];
        ctx.fillRect(x, y, cell - 2, cell - 2);
      }
    }
    ctx.globalAlpha = 1;
  };

  const stopLoop = () => {
    if (rafId) {
      window.cancelAnimationFrame(rafId);
      rafId = 0;
    }
  };

  const tick = (now: number) => {
    drawFrame(now);
    if (!reducedMotion) {
      rafId = window.requestAnimationFrame(tick);
    }
  };

  resize();
  drawFrame(1400);
  if (!reducedMotion) {
    rafId = window.requestAnimationFrame(tick);
  }

  if (typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver(() => {
      resize();
      if (reducedMotion) drawFrame(1400);
    });
    resizeObserver.observe(host);
  }

  if (typeof window.matchMedia === "function") {
    mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    handleMotionChange = () => {
      reducedMotion = mediaQuery?.matches ?? false;
      stopLoop();
      drawFrame(1400);
      if (!reducedMotion) {
        rafId = window.requestAnimationFrame(tick);
      }
    };
    mediaQuery.addEventListener?.("change", handleMotionChange);
  }
}

onMounted(() => {
  setupPixelField();
});

watch(isDark, () => {
  if (typeof window === "undefined") return;
  const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches ?? false;
  if (reduced) {
    if (rafId) window.cancelAnimationFrame(rafId);
    resizeObserver?.disconnect();
    setupPixelField();
  }
});

onBeforeUnmount(() => {
  isUnmounted = true;
  clearStageTimers();
  if (typeof window !== "undefined") {
    if (rafId) {
      window.cancelAnimationFrame(rafId);
      rafId = 0;
    }
    for (const timerId of copyTimers.values()) {
      window.clearTimeout(timerId);
    }
    copyTimers.clear();
  }
  resizeObserver?.disconnect();
  resizeObserver = null;
  if (mediaQuery && handleMotionChange) {
    mediaQuery.removeEventListener?.("change", handleMotionChange);
  }
});
</script>

<template>
  <div class="bp-home">
    <!-- Hero: airy canvas with floating Ocean Paper pixel ribbons -->
    <section ref="heroRef" class="bp-hero" :class="{ 'is-stage-open': stageOpen }">
      <canvas ref="canvasRef" class="bp-hero-canvas" aria-hidden="true" />
      <div class="bp-container bp-hero-inner">
        <h1 class="bp-hero-title">
          {{ content.heroLine1 }}<br />{{ content.heroLine2 }}
        </h1>
        <p class="bp-hero-lead">
          <span class="bp-lead-strong">{{ content.heroLeadStrong }}</span>
          <span class="bp-lead-muted">{{ content.heroLeadMuted }}</span>
        </p>
        <div class="bp-hero-actions">
          <div
            ref="hubRef"
            class="bp-hero-ai-hub"
            :class="{
              'is-open': stageOpen,
              'is-copied': copyState === 'copied',
              'is-failed': copyState === 'failed',
            }"
            @mouseenter="openStage(false)"
            @mouseleave="closeStage(false)"
            @focusin="openStage(true)"
            @focusout="handleHubFocusOut"
            @keydown.escape="closeStage(true)"
          >
            <div class="bp-hero-btn-row">
              <button
                type="button"
                class="bp-pill bp-pill-primary bp-pill-merged"
                :class="{
                  'is-copied': copyState === 'copied',
                  'is-failed': copyState === 'failed',
                  'is-open': stageOpen,
                }"
                :aria-expanded="stageOpen"
                @click="copyText(content.aiSpecUrl, 'hero')"
              >
                <span class="bp-pill-mark" aria-hidden="true" />
                <span v-if="copyState === 'copied'">{{ content.ctaMergedCopied }}</span>
                <span v-else-if="copyState === 'failed'">{{ content.ctaMergedFailed }}</span>
                <span v-else>{{ content.ctaMerged }}</span>
                <span class="bp-pill-chevron" aria-hidden="true">
                  <svg viewBox="0 0 16 16" width="13" height="13" fill="none">
                    <path
                      d="M4.5 6.5L8 10l3.5-3.5"
                      stroke="currentColor"
                      stroke-width="1.6"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    />
                  </svg>
                </span>
              </button>

              <button
                type="button"
                class="bp-pill bp-pill-secondary bp-pill-touch-toggle"
                :aria-expanded="stageOpen"
                @click="toggleStage"
              >
                {{ stageOpen ? content.ctaDemoClose : content.ctaDemoToggle }}
              </button>
            </div>

            <div
              v-if="copyState === 'failed'"
              class="bp-hero-fallback"
              aria-live="polite"
            >
              <span class="bp-tip-dot" aria-hidden="true" />
              <div class="bp-tip-body">
                <span>{{ content.aiCopyFailed }}</span>
                <code
                  class="bp-tip-code"
                  tabindex="0"
                  @click.stop="selectCodeElement"
                  @focus="selectCodeElement"
                >{{ content.aiSpecUrl }}</code>
              </div>
            </div>

            <AiWorkflowStage
              :open="stageOpen"
              :is-dark="isDark"
              :is-en="isEn"
              :copy-state="copyState"
              :ai-spec-url="content.aiSpecUrl"
              @copy="copyText(content.aiSpecUrl, 'hero')"
            />
          </div>
        </div>
      </div>
    </section>

    <!-- 3-Column Development Paths (Base.org crisp flat card triad) -->
    <section class="bp-section">
      <div class="bp-container">
        <div class="bp-triad">
          <template v-for="item in content.paths" :key="item.id">
            <div
              v-if="item.id === 'ai-spec'"
              class="bp-card-ai-wrap"
              :class="{
                'is-copied': cardCopyState === 'copied',
                'is-failed': cardCopyState === 'failed',
              }"
            >
              <article
                class="bp-card bp-card-ai"
                :class="{
                  'is-copied': cardCopyState === 'copied',
                  'is-failed': cardCopyState === 'failed',
                }"
              >
                <button
                  type="button"
                  class="bp-card-trigger"
                  :aria-label="content.aiCardAria"
                  :aria-describedby="cardTipId"
                  @click="copyText(content.aiSpecUrl, 'card')"
                />
                <div class="bp-card-top">
                  <span class="bp-duo-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
                      <rect x="4" y="5" width="16" height="14" rx="3" class="bp-svg-fill" />
                      <rect x="4" y="5" width="16" height="14" rx="3" stroke="currentColor" stroke-width="1.5" />
                      <path d="M8.5 10l2.5 2-2.5 2M12.5 14h3" class="bp-svg-accent" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                    </svg>
                  </span>
                  <span
                    class="bp-card-status"
                    :class="{ 'is-copied': cardCopyState === 'copied', 'is-failed': cardCopyState === 'failed' }"
                    aria-hidden="true"
                  >
                    <span v-if="cardCopyState === 'copied'">{{ content.aiCardBadgeCopied }}</span>
                    <span v-else-if="cardCopyState === 'failed'">{{ content.aiCardBadgeFailed }}</span>
                    <span v-else>{{ content.aiCardBadgeIdle }}</span>
                  </span>
                </div>
                <h2 class="bp-card-title">{{ item.title }}</h2>
                <p class="bp-card-desc">{{ item.desc }}</p>
                <div class="bp-card-meta bp-card-meta-split">
                  <span class="bp-card-tag">{{ item.tag }}</span>
                  <a
                    :href="item.link"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="bp-card-raw-link"
                    @click.stop
                  >
                    <span>{{ content.aiCardRaw }}</span>
                    <span class="bp-raw-arrow" aria-hidden="true">↗</span>
                  </a>
                </div>
              </article>

              <div
                :id="cardTipId"
                class="bp-card-drawer"
                :class="{
                  'is-copied': cardCopyState === 'copied',
                  'is-failed': cardCopyState === 'failed',
                }"
                aria-live="polite"
                @click="cardCopyState !== 'failed' && copyText(content.aiSpecUrl, 'card')"
              >
                <span class="bp-tip-dot" aria-hidden="true" />
                <div class="bp-tip-body">
                  <span v-if="cardCopyState === 'copied'">{{ content.aiCopySuccess }}</span>
                  <span v-else-if="cardCopyState === 'failed'">{{ content.aiCopyFailed }}</span>
                  <span v-else>{{ content.aiCardHint }}</span>
                  <code
                    v-if="cardCopyState === 'failed'"
                    class="bp-tip-code"
                    tabindex="0"
                    @click.stop="selectCodeElement"
                    @focus="selectCodeElement"
                  >{{ content.aiSpecUrl }}</code>
                </div>
              </div>
            </div>

            <a
              v-else
              :href="item.link"
              :target="item.external ? '_blank' : undefined"
              :rel="item.external ? 'noopener noreferrer' : undefined"
              class="bp-card"
            >
              <div class="bp-card-top">
                <span class="bp-duo-icon" aria-hidden="true">
                  <!-- Scheme A two-tone 24x24 icons -->
                  <svg v-if="item.icon === 'sandbox'" viewBox="0 0 24 24" width="22" height="22" fill="none">
                    <rect x="4" y="4" width="16" height="16" rx="3.5" class="bp-svg-fill" />
                    <rect x="4" y="4" width="16" height="16" rx="3.5" stroke="currentColor" stroke-width="1.5" />
                    <path d="M9 12L11.2 14.2L15.5 9.8" class="bp-svg-accent" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                  <svg v-else viewBox="0 0 24 24" width="22" height="22" fill="none">
                    <path d="M5.5 3.5h8.5l4.5 4.5v12.5a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5v-17Z" class="bp-svg-fill" />
                    <path d="M5.5 3.5h8.5l4.5 4.5v12.5a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5v-17Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
                    <path d="M14 3.5V8h4.5" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
                    <path d="M9 13h6M9 16.5h4" class="bp-svg-accent" stroke-width="1.6" stroke-linecap="round" />
                  </svg>
                </span>
                <span class="bp-card-arrow" aria-hidden="true">↗</span>
              </div>
              <h2 class="bp-card-title">{{ item.title }}</h2>
              <p class="bp-card-desc">{{ item.desc }}</p>
              <div class="bp-card-meta">{{ item.tag }}</div>
            </a>
          </template>
        </div>
      </div>
    </section>

    <!-- Full-Bleed Architectural Metrics Band -->
    <section class="bp-metrics-band">
      <div class="bp-container">
        <div class="bp-metrics-head">
          <h2 class="bp-metrics-title">{{ content.metricsTitle }}</h2>
          <p class="bp-metrics-sub">{{ content.metricsSub }}</p>
        </div>
        <div class="bp-metrics-grid">
          <div v-for="m in content.metrics" :key="m.label" class="bp-metric">
            <div class="bp-metric-value">{{ m.value }}</div>
            <div class="bp-metric-label">{{ m.label }}</div>
          </div>
        </div>
      </div>
    </section>

    <!-- 6-Chapter Editorial Documentation Index -->
    <section id="plugin-docs" class="bp-section bp-section-chapters">
      <div class="bp-container">
        <div class="bp-split-head">
          <h2 class="bp-editorial-h2">
            <span class="bp-h2-strong">{{ content.chaptersStrong }}</span>
            <span class="bp-h2-muted">{{ content.chaptersMuted }}</span>
          </h2>
          <a
            :href="content.aiSpecUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="bp-pill bp-pill-secondary bp-pill-compact"
          >
            <span>{{ content.chaptersBtn }}</span>
            <span aria-hidden="true">›</span>
          </a>
        </div>

        <div class="bp-chapters-grid">
          <a
            v-for="ch in content.chapters"
            :key="ch.num"
            :href="ch.link"
            class="bp-card bp-chapter-card"
          >
            <div class="bp-card-top">
              <span class="bp-chapter-num">{{ ch.num }}</span>
              <span class="bp-card-arrow" aria-hidden="true">↗</span>
            </div>
            <h3 class="bp-card-title">{{ ch.title }}</h3>
            <p class="bp-card-desc">{{ ch.desc }}</p>
            <div class="bp-card-meta">{{ ch.meta }}</div>
          </a>
        </div>
      </div>
    </section>

    <!-- Local Verification Command & Core References -->
    <section class="bp-section bp-section-bottom">
      <div class="bp-container">
        <div class="bp-bottom-grid">
          <div class="bp-panel">
            <h3 class="bp-panel-title">{{ content.cliTitle }}</h3>
            <p class="bp-panel-sub">{{ content.cliSub }}</p>
            <div class="bp-cmd-box">
              <code class="bp-cmd-code">{{ content.cliCmd }}</code>
              <button
                type="button"
                class="bp-cmd-btn"
                :class="{ 'is-copied': cmdCopyState === 'copied' }"
                @click="copyText(content.cliCmd, 'cmd')"
              >
                {{ cmdCopyState === "copied" ? content.cliCopied : content.cliCopy }}
              </button>
            </div>
          </div>

          <div class="bp-panel">
            <h3 class="bp-panel-title">{{ content.refsTitle }}</h3>
            <div class="bp-refs-grid">
              <a
                v-for="r in content.refs"
                :key="r.label"
                :href="r.href"
                :target="r.external ? '_blank' : undefined"
                :rel="r.external ? 'noopener noreferrer' : undefined"
                class="bp-ref-link"
              >
                <span>{{ r.label }}</span>
                <span class="bp-ref-arrow" aria-hidden="true">↗</span>
              </a>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Minimal Footer -->
    <footer class="bp-footer">
      <div class="bp-container bp-footer-inner">
        <span>{{ content.footerLeft }}</span>
        <a :href="content.footerSwitchLink" class="bp-footer-link">
          {{ content.footerSwitchLabel }}
        </a>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.bp-home {
  background: var(--bp-bg);
  color: var(--bp-text-1);
  overflow-x: clip;
}

.bp-container {
  max-width: 1180px;
  margin: 0 auto;
  padding: 0 28px;
}

/* Hero */
.bp-hero {
  position: relative;
  z-index: 2;
  min-height: 520px;
  display: flex;
  align-items: center;
  padding: 88px 0 80px;
  overflow-x: clip;
  overflow-y: visible;
}

.bp-hero.is-stage-open {
  z-index: 25;
}

.bp-hero-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 0;
}

.bp-hero-inner {
  position: relative;
  z-index: 1;
  text-align: center;
  max-width: 780px;
}

.bp-hero-title {
  margin: 0;
  font-size: clamp(40px, 5.6vw, 68px);
  line-height: 1.06;
  font-weight: 500;
  letter-spacing: -0.04em;
  color: var(--bp-text-1);
}

.bp-hero-lead {
  margin: 24px auto 0;
  max-width: 600px;
  font-size: clamp(16px, 1.5vw, 18px);
  line-height: 1.6;
}

.bp-lead-strong {
  color: var(--bp-text-1);
  font-weight: 450;
}

.bp-lead-muted {
  color: var(--bp-text-2);
}

.bp-hero-actions {
  margin-top: 34px;
  display: flex;
  justify-content: center;
}

.bp-hero-ai-hub {
  position: relative;
  display: inline-flex;
  flex-direction: column;
  align-items: center;
}

.bp-hero-btn-row {
  display: inline-flex;
  flex-wrap: wrap;
  justify-content: center;
  align-items: center;
  gap: 10px;
}

.bp-pill-merged {
  height: 48px;
  padding: 0 24px 0 22px;
  font-size: 14.5px;
  box-shadow: 0 8px 24px -12px rgba(15, 23, 42, 0.35);
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    color 0.18s ease,
    transform 0.18s cubic-bezier(0.22, 1, 0.36, 1),
    box-shadow 0.22s ease;
}

.bp-pill-merged:hover,
.bp-pill-merged.is-open {
  transform: translateY(-1px);
  box-shadow:
    0 12px 28px -10px rgba(61, 99, 176, 0.42),
    0 0 0 3px var(--vp-c-brand-soft);
}

.bp-pill-merged:active {
  transform: translateY(0) scale(0.985);
}

.bp-pill-merged.is-copied {
  background: var(--bp-accent-strong);
  border-color: var(--bp-accent-strong);
  color: #ffffff;
}

.dark .bp-pill-merged.is-copied {
  background: var(--bp-accent);
  border-color: var(--bp-accent);
  color: #14171f;
}

.bp-pill-chevron {
  display: inline-flex;
  align-items: center;
  margin-left: -2px;
  opacity: 0.72;
  transition: transform 0.24s cubic-bezier(0.22, 1, 0.36, 1), opacity 0.18s ease;
}

.bp-pill-merged.is-open .bp-pill-chevron {
  transform: rotate(180deg);
  opacity: 1;
}

.bp-pill.bp-pill-touch-toggle {
  display: none;
  height: 48px;
  padding: 0 18px;
}

.bp-hero-fallback {
  margin-top: 12px;
  width: min(420px, calc(100vw - 36px));
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 13px;
  border-radius: 10px;
  border: 1px solid var(--bp-accent);
  background: var(--vp-c-bg-elv);
  color: var(--bp-text-1);
  font-size: 13px;
  line-height: 1.48;
  text-align: left;
}

.bp-tip-dot {
  width: 6px;
  height: 6px;
  margin-top: 6px;
  border-radius: 2px;
  background: var(--bp-accent);
  flex-shrink: 0;
}

.bp-tip-body {
  min-width: 0;
  flex: 1;
}

.bp-tip-code {
  display: block;
  margin-top: 5px;
  padding: 4px 7px;
  border-radius: 6px;
  background: var(--bp-code-bg);
  border: 1px solid var(--bp-border);
  font-family: var(--bp-font-mono);
  font-size: 11.5px;
  line-height: 1.4;
  color: var(--bp-text-1);
  word-break: break-all;
  user-select: all;
  cursor: text;
}

.bp-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  height: 44px;
  padding: 0 22px;
  border-radius: 999px;
  font-size: 14px;
  font-weight: 500;
  text-decoration: none;
  cursor: pointer;
  transition: background-color 0.16s ease, border-color 0.16s ease, color 0.16s ease;
  white-space: nowrap;
}

.bp-pill-primary {
  background: var(--bp-text-1);
  color: var(--bp-bg);
  border: 1px solid var(--bp-text-1);
}

.bp-pill-primary:hover {
  background: var(--bp-accent-strong);
  border-color: var(--bp-accent-strong);
  color: #ffffff;
}

.dark .bp-pill-primary:hover {
  background: var(--bp-accent);
  border-color: var(--bp-accent);
  color: #14171f;
}

.bp-pill-mark {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  background: currentColor;
  flex-shrink: 0;
}

.bp-pill-secondary {
  background: var(--bp-bg);
  color: var(--bp-text-1);
  border: 1px solid var(--bp-btn-border);
}

.bp-pill-secondary:hover {
  background: var(--bp-hover);
  border-color: var(--bp-accent);
}

.bp-pill-secondary.is-copied {
  border-color: var(--bp-accent);
  background: var(--vp-c-brand-soft);
  color: var(--bp-accent-strong);
}

.dark .bp-pill-secondary.is-copied {
  color: var(--bp-accent);
}

.bp-pill-compact {
  height: 38px;
  padding: 0 18px;
}

/* Sections & Flat Cards */
.bp-section {
  padding: 20px 0 72px;
}

.bp-triad {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.bp-card {
  display: flex;
  flex-direction: column;
  padding: 26px 26px 24px;
  background: var(--bp-bg);
  border: 1px solid var(--bp-border);
  border-radius: 16px;
  text-decoration: none;
  color: inherit;
  transition: border-color 0.16s ease, background-color 0.16s ease;
}

.bp-card:hover {
  border-color: var(--bp-accent);
  background: var(--bp-bg-alt);
}

.bp-card-ai-wrap {
  position: relative;
  display: flex;
  z-index: 1;
}

.bp-card-ai-wrap:hover,
.bp-card-ai-wrap:focus-within,
.bp-card-ai-wrap.is-copied,
.bp-card-ai-wrap.is-failed {
  z-index: 10;
}

.bp-card-ai {
  position: relative;
  z-index: 2;
  width: 100%;
}

.bp-card-ai-wrap:hover .bp-card-ai,
.bp-card-ai-wrap:focus-within .bp-card-ai,
.bp-card-ai.is-copied,
.bp-card-ai.is-failed {
  border-color: var(--bp-accent);
  background: var(--bp-bg-alt);
}

.bp-card-ai > :not(.bp-card-trigger) {
  position: relative;
  z-index: 2;
  pointer-events: none;
}

.bp-card-trigger {
  position: absolute;
  inset: 0;
  z-index: 1;
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  border: 0;
  border-radius: inherit;
  background: transparent;
  cursor: pointer;
  appearance: none;
}

.bp-card-trigger:focus-visible {
  outline: 2px solid var(--bp-accent);
  outline-offset: -2px;
}

.bp-card-status {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 9px;
  border-radius: 999px;
  border: 1px solid var(--bp-border);
  background: var(--bp-bg-alt);
  font-size: 12px;
  font-weight: 500;
  line-height: 1;
  color: var(--bp-text-2);
  white-space: nowrap;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
}

.bp-card-ai-wrap:hover .bp-card-status:not(.is-copied),
.bp-card-ai-wrap:focus-within .bp-card-status:not(.is-copied) {
  border-color: var(--bp-btn-border);
  color: var(--bp-text-1);
}

.bp-card-status.is-copied {
  border-color: var(--bp-accent);
  background: var(--vp-c-brand-soft);
  color: var(--bp-accent-strong);
}

.dark .bp-card-status.is-copied {
  color: var(--bp-accent);
}

.bp-card-status.is-failed {
  border-color: var(--bp-accent);
  color: var(--bp-text-1);
}

.bp-card-meta-split {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 6px 12px;
}

.bp-card-tag {
  min-width: 0;
  overflow-wrap: anywhere;
}

.bp-card-raw-link {
  position: relative;
  z-index: 3;
  pointer-events: auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  margin: -2px -6px;
  border-radius: 6px;
  font-family: var(--bp-font-sans);
  font-size: 12px;
  font-weight: 500;
  color: var(--bp-text-2);
  text-decoration: none;
  white-space: nowrap;
  flex-shrink: 0;
  transition: color 0.14s ease, background-color 0.14s ease;
}

.bp-card-raw-link:hover {
  color: var(--bp-accent-strong);
  background: var(--bp-hover);
}

.dark .bp-card-raw-link:hover {
  color: var(--bp-accent);
}

.bp-card-raw-link:focus-visible {
  outline: 2px solid var(--bp-accent);
  outline-offset: 2px;
}

.bp-raw-arrow {
  font-size: 11px;
  transition: transform 0.14s ease;
}

.bp-card-raw-link:hover .bp-raw-arrow {
  transform: translate(1px, -1px);
}

.bp-card-drawer {
  position: absolute;
  left: 0;
  right: 0;
  top: calc(100% - 16px);
  z-index: 1;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 26px 22px 14px;
  border: 1px solid var(--bp-accent);
  border-top: 0;
  border-radius: 0 0 16px 16px;
  background: var(--bp-canvas);
  color: var(--bp-text-2);
  font-size: 13px;
  line-height: 1.5;
  opacity: 0;
  visibility: hidden;
  pointer-events: none;
  transform: translateY(-12px);
  transition:
    opacity 0.16s ease,
    transform 0.2s cubic-bezier(0.22, 1, 0.36, 1),
    visibility 0.16s ease,
    border-color 0.16s ease,
    background-color 0.16s ease,
    color 0.16s ease;
}

.bp-card-drawer:not(.is-failed) {
  cursor: pointer;
}

.bp-card-ai-wrap:hover .bp-card-drawer,
.bp-card-ai-wrap:focus-within .bp-card-drawer,
.bp-card-ai-wrap.is-copied .bp-card-drawer,
.bp-card-ai-wrap.is-failed .bp-card-drawer {
  opacity: 1;
  visibility: visible;
  pointer-events: auto;
  transform: translateY(0);
}

.bp-card-drawer.is-copied {
  border-color: var(--bp-accent);
  background: var(--bp-bg-alt);
  background: color-mix(in srgb, var(--bp-accent) 10%, var(--bp-bg-alt));
  color: var(--bp-accent-strong);
}

.dark .bp-card-drawer.is-copied {
  color: var(--bp-accent);
}

.bp-card-drawer.is-failed {
  border-color: var(--bp-accent);
  color: var(--bp-text-1);
}

.bp-card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 18px;
}

.bp-duo-icon {
  display: inline-flex;
  color: var(--bp-text-1);
}

.bp-svg-fill {
  fill: var(--bp-duo-fill);
}

.bp-svg-accent {
  stroke: var(--bp-accent);
}

.bp-card-arrow {
  font-size: 14px;
  color: var(--bp-text-3);
  transition: transform 0.16s ease, color 0.16s ease;
}

.bp-card:hover .bp-card-arrow {
  color: var(--bp-accent);
  transform: translate(2px, -2px);
}

.bp-card-title {
  margin: 0;
  font-size: 18px;
  line-height: 1.35;
  font-weight: 560;
  letter-spacing: -0.015em;
  color: var(--bp-text-1);
}

.bp-card-desc {
  margin: 10px 0 0;
  font-size: 14px;
  line-height: 1.62;
  color: var(--bp-text-2);
  flex: 1;
}

.bp-card-meta {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid var(--bp-border);
  font-family: var(--bp-font-mono);
  font-size: 12px;
  color: var(--bp-text-3);
}

/* Metrics Band (Base.org subtle dot-paper band) */
.bp-metrics-band {
  padding: 80px 0;
  background-color: var(--bp-canvas);
  background-image: radial-gradient(var(--bp-dot) 1px, transparent 1px);
  background-size: 20px 20px;
  border-top: 1px solid var(--bp-border);
  border-bottom: 1px solid var(--bp-border);
}

.bp-metrics-head {
  text-align: center;
  max-width: 620px;
  margin: 0 auto 48px;
}

.bp-metrics-title {
  margin: 0;
  font-size: clamp(26px, 3.2vw, 36px);
  line-height: 1.18;
  font-weight: 500;
  letter-spacing: -0.03em;
  color: var(--bp-text-1);
}

.bp-metrics-sub {
  margin: 12px 0 0;
  font-size: 15px;
  line-height: 1.6;
  color: var(--bp-text-2);
}

.bp-metrics-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.bp-metric {
  text-align: center;
  padding: 12px 16px;
  position: relative;
}

.bp-metric + .bp-metric::before {
  content: "";
  position: absolute;
  left: 0;
  top: 18%;
  bottom: 18%;
  width: 1px;
  background: var(--bp-border);
}

.bp-metric-value {
  font-size: clamp(28px, 3vw, 38px);
  line-height: 1.1;
  font-weight: 500;
  letter-spacing: -0.03em;
  font-variant-numeric: tabular-nums;
  color: var(--bp-text-1);
}

.bp-metric-label {
  margin-top: 10px;
  font-family: var(--bp-font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  color: var(--bp-text-2);
}

/* 6 Chapters Editorial Grid */
.bp-section-chapters {
  padding: 84px 0 56px;
}

.bp-split-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 40px;
}

.bp-editorial-h2 {
  margin: 0;
  max-width: 660px;
  font-size: clamp(26px, 3.4vw, 40px);
  line-height: 1.16;
  font-weight: 500;
  letter-spacing: -0.032em;
}

.bp-h2-strong {
  color: var(--bp-text-1);
}

.bp-h2-muted {
  color: var(--bp-text-2);
}

.bp-chapters-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.bp-chapter-num {
  font-family: var(--bp-font-mono);
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.04em;
  color: var(--bp-accent-strong);
}

.dark .bp-chapter-num {
  color: var(--bp-accent);
}

/* Bottom CLI & Core Links */
.bp-section-bottom {
  padding: 0 0 80px;
}

.bp-bottom-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  gap: 16px;
}

.bp-panel {
  padding: 26px;
  border: 1px solid var(--bp-border);
  border-radius: 16px;
  background: var(--bp-bg-alt);
}

.bp-panel-title {
  margin: 0;
  font-size: 17px;
  font-weight: 560;
  letter-spacing: -0.015em;
  color: var(--bp-text-1);
}

.bp-panel-sub {
  margin: 8px 0 0;
  font-size: 14px;
  color: var(--bp-text-2);
}

.bp-cmd-box {
  margin-top: 18px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-radius: 10px;
  background: var(--bp-code-bg);
  border: 1px solid var(--bp-border);
}

.bp-cmd-code {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  font-family: var(--bp-font-mono);
  font-size: 13px;
  line-height: 1.5;
  color: var(--bp-text-1);
  white-space: nowrap;
}

.bp-cmd-btn {
  flex-shrink: 0;
  height: 28px;
  padding: 0 10px;
  border-radius: 6px;
  border: 1px solid var(--bp-btn-border);
  background: var(--bp-bg);
  color: var(--bp-text-1);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background-color 0.14s ease, border-color 0.14s ease;
}

.bp-cmd-btn:hover {
  background: var(--bp-hover);
  border-color: var(--bp-accent);
}

.bp-cmd-btn.is-copied {
  border-color: var(--bp-accent);
  color: var(--bp-accent-strong);
}

.dark .bp-cmd-btn.is-copied {
  color: var(--bp-accent);
}

.bp-refs-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
}

.bp-ref-link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 9px 10px;
  border-radius: 8px;
  font-size: 14px;
  color: var(--bp-text-1);
  text-decoration: none;
  transition: background-color 0.14s ease, color 0.14s ease;
}

.bp-ref-link:hover {
  background: var(--bp-hover);
  color: var(--bp-accent-strong);
}

.dark .bp-ref-link:hover {
  color: var(--bp-accent);
}

.bp-ref-arrow {
  font-size: 12px;
  color: var(--bp-text-3);
}

/* Footer */
.bp-footer {
  border-top: 1px solid var(--bp-border);
  padding: 24px 0;
  font-size: 13px;
  color: var(--bp-text-2);
}

.bp-footer-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.bp-footer-link {
  color: var(--bp-text-1);
  text-decoration: none;
  font-weight: 500;
}

.bp-footer-link:hover {
  color: var(--bp-accent-strong);
}

.dark .bp-footer-link:hover {
  color: var(--bp-accent);
}

/* Responsive */
@media (max-width: 960px) {
  .bp-triad,
  .bp-chapters-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .bp-metrics-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px 0;
  }

  .bp-metric:nth-child(3)::before {
    display: none;
  }

  .bp-bottom-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .bp-container {
    padding: 0 18px;
  }

  .bp-hero {
    min-height: 420px;
    padding: 64px 0 56px;
  }

  .bp-hero-actions {
    position: relative;
  }

  .bp-hero-ai-hub {
    width: 100%;
  }

  .bp-triad,
  .bp-chapters-grid,
  .bp-refs-grid {
    grid-template-columns: 1fr;
  }

  .bp-split-head {
    flex-direction: column;
    align-items: flex-start;
  }

  .bp-metrics-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (hover: none) and (pointer: coarse) {
  .bp-pill.bp-pill-touch-toggle {
    display: inline-flex;
  }
}

@media (hover: none) {
  .bp-card-ai-wrap {
    flex-direction: column;
  }

  .bp-card-ai {
    flex: 1;
    border-bottom-left-radius: 0;
    border-bottom-right-radius: 0;
  }

  .bp-card-drawer {
    position: static;
    padding: 12px 22px 15px;
    border-color: var(--bp-border);
    border-radius: 0 0 16px 16px;
    opacity: 1;
    visibility: visible;
    transform: none;
    pointer-events: auto;
    transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
  }

  .bp-card-ai-wrap:hover .bp-card-drawer,
  .bp-card-ai-wrap:focus-within .bp-card-drawer,
  .bp-card-ai-wrap.is-copied .bp-card-drawer,
  .bp-card-ai-wrap.is-failed .bp-card-drawer {
    border-color: var(--bp-accent);
  }
}

@media (prefers-reduced-motion: reduce) {
  .bp-pill,
  .bp-pill-merged,
  .bp-pill-chevron,
  .bp-card,
  .bp-card-arrow,
  .bp-card-status,
  .bp-card-raw-link,
  .bp-raw-arrow,
  .bp-card-drawer,
  .bp-cmd-btn,
  .bp-ref-link {
    transition: none !important;
  }

  .bp-card-drawer,
  .bp-card-ai-wrap:hover .bp-card-drawer,
  .bp-card-ai-wrap:focus-within .bp-card-drawer,
  .bp-card-ai-wrap.is-copied .bp-card-drawer,
  .bp-card-ai-wrap.is-failed .bp-card-drawer {
    transform: none;
  }
}
</style>
