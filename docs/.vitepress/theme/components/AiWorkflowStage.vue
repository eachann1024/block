<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  REDUCED_MOTION_PREVIEW_SEC,
  TOTAL_DURATION_SEC,
  WORKFLOW_STEPS,
} from "../motion/aiWorkflowSteps";
import type { AiWorkflowRuntime } from "../motion/aiWorkflowScene";

const props = defineProps<{
  open: boolean;
  isDark: boolean;
  isEn: boolean;
  copyState: "idle" | "copied" | "failed";
  aiSpecUrl: string;
}>();

const emit = defineEmits<{
  (e: "copy"): void;
}>();

const hostRef = ref<HTMLElement | null>(null);
const ready = ref(false);
const paused = ref(false);
const currentSec = ref(0);
const durationSec = ref(TOTAL_DURATION_SEC);

let runtime: AiWorkflowRuntime | null = null;
let initPromise: Promise<AiWorkflowRuntime | null> | null = null;
let deactivateTimer: number | null = null;
let unmounted = false;
let reducedMotion = false;
let motionQuery: MediaQueryList | null = null;
let onMotionChange: (() => void) | null = null;

const activeStepIndex = computed(() => {
  const sec = currentSec.value;
  for (let i = WORKFLOW_STEPS.length - 1; i >= 0; i--) {
    if (sec >= WORKFLOW_STEPS[i].startSec - 0.02) {
      return i;
    }
  }
  return 0;
});

const activeStep = computed(() => WORKFLOW_STEPS[activeStepIndex.value] ?? WORKFLOW_STEPS[0]);

function stepProgress(index: number): number {
  const step = WORKFLOW_STEPS[index];
  if (!step) return 0;
  if (currentSec.value <= step.startSec) return 0;
  if (currentSec.value >= step.endSec) return 100;
  const span = Math.max(0.01, step.endSec - step.startSec);
  return Math.min(100, Math.max(0, ((currentSec.value - step.startSec) / span) * 100));
}

function clearDeactivateTimer() {
  if (deactivateTimer !== null && typeof window !== "undefined") {
    window.clearTimeout(deactivateTimer);
    deactivateTimer = null;
  }
}

async function ensureRuntime(): Promise<AiWorkflowRuntime | null> {
  if (runtime) return runtime;
  if (initPromise) return initPromise;
  if (typeof window === "undefined") return null;

  initPromise = (async () => {
    const mod = await import("../motion/aiWorkflowScene");
    if (unmounted) return null;
    const rt = mod.createAiWorkflowRuntime({
      dark: props.isDark,
      en: props.isEn,
      onFrame(_frame, totalFrames, sec) {
        if (unmounted) return;
        currentSec.value = sec;
        if (totalFrames > 1) {
          durationSec.value = totalFrames / 60;
        }
      },
    });
    if (unmounted) {
      rt.destroy();
      return null;
    }
    runtime = rt;
    mountCanvas(rt.canvas);
    ready.value = true;
    return rt;
  })();

  return initPromise;
}

function mountCanvas(canvas: HTMLCanvasElement) {
  const host = hostRef.value;
  if (!host) return;
  canvas.className = "bp-mc-canvas";
  canvas.setAttribute("aria-hidden", "true");
  host.innerHTML = "";
  host.appendChild(canvas);
}

async function startStagePlayback() {
  clearDeactivateTimer();
  const rt = await ensureRuntime();
  if (!rt || unmounted || !props.open) return;
  rt.setTheme(props.isDark);
  rt.setLocale(props.isEn);
  if (reducedMotion) {
    paused.value = true;
    currentSec.value = REDUCED_MOTION_PREVIEW_SEC;
    rt.seekSeconds(REDUCED_MOTION_PREVIEW_SEC, false);
  } else {
    paused.value = false;
    rt.playFromStart();
  }
}

function scheduleStageShutdown() {
  clearDeactivateTimer();
  if (!runtime || typeof window === "undefined") return;
  deactivateTimer = window.setTimeout(() => {
    deactivateTimer = null;
    if (!props.open && runtime) {
      runtime.deactivate();
      paused.value = true;
    }
  }, 300);
}

function handleStepSelect(index: number) {
  const step = WORKFLOW_STEPS[index];
  if (!step || !runtime) return;
  clearDeactivateTimer();
  const shouldPlay = !reducedMotion;
  paused.value = !shouldPlay;
  currentSec.value = step.startSec + 0.08;
  runtime.seekSeconds(step.startSec + 0.08, shouldPlay);
}

function togglePlayPause() {
  if (!runtime) return;
  if (paused.value) {
    paused.value = false;
    runtime.resume();
  } else {
    paused.value = true;
    runtime.pause();
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      void startStagePlayback();
    } else {
      scheduleStageShutdown();
    }
  },
);

watch(
  () => props.isDark,
  (dark) => {
    runtime?.setTheme(dark);
  },
);

watch(
  () => props.isEn,
  (en) => {
    runtime?.setLocale(en);
  },
);

onMounted(() => {
  if (typeof window === "undefined") return;
  if (typeof window.matchMedia === "function") {
    motionQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    reducedMotion = motionQuery.matches;
    onMotionChange = () => {
      reducedMotion = motionQuery?.matches ?? false;
      if (props.open && runtime) {
        if (reducedMotion) {
          paused.value = true;
          runtime.seekSeconds(REDUCED_MOTION_PREVIEW_SEC, false);
        } else {
          paused.value = false;
          runtime.resume();
        }
      }
    };
    motionQuery.addEventListener?.("change", onMotionChange);
  }

  // Warm up the Motion Canvas bundle lazily when idle so first hover is instant
  const idle = (window as unknown as { requestIdleCallback?: (cb: () => void) => number })
    .requestIdleCallback;
  if (typeof idle === "function") {
    idle(() => {
      if (!unmounted) void ensureRuntime();
    });
  }
});

onBeforeUnmount(() => {
  unmounted = true;
  clearDeactivateTimer();
  if (motionQuery && onMotionChange) {
    motionQuery.removeEventListener?.("change", onMotionChange);
  }
  runtime?.destroy();
  runtime = null;
});
</script>

<template>
  <div
    class="bp-mc-popover"
    :class="{ 'is-open': open }"
    role="region"
    :aria-label="isEn ? 'AI plugin workflow preview' : 'AI 插件生成流程动画预览'"
    :aria-hidden="!open"
  >
    <!-- Invisible hover bridge connecting button to stage -->
    <div class="bp-mc-bridge" aria-hidden="true" />

    <div class="bp-mc-shell">
      <!-- Top Bar -->
      <div class="bp-mc-head">
        <div class="bp-mc-head-left">
          <span class="bp-mc-live-dot" :class="{ 'is-paused': paused }" aria-hidden="true" />
          <span class="bp-mc-step-title">
            {{ activeStep.num }} · {{ isEn ? activeStep.en : activeStep.zh }}
          </span>
          <span class="bp-mc-step-desc">
            {{ isEn ? activeStep.descEn : activeStep.descZh }}
          </span>
        </div>

        <div class="bp-mc-head-right">
          <span class="bp-mc-engine-tag">Motion Canvas</span>
          <button
            type="button"
            class="bp-mc-mini-btn"
            :class="{ 'is-copied': copyState === 'copied' }"
            @click.stop="emit('copy')"
          >
            <span v-if="copyState === 'copied'">{{ isEn ? "✓ Copied" : "✓ 已复制" }}</span>
            <span v-else>{{ isEn ? "Copy URL" : "复制链接" }}</span>
          </button>
        </div>
      </div>

      <!-- Motion Canvas 16:9 Stage -->
      <div class="bp-mc-viewport">
        <div ref="hostRef" class="bp-mc-canvas-host" />
        <div v-if="!ready" class="bp-mc-loading" aria-hidden="true">
          <span class="bp-mc-loading-dot" />
          <span>{{ isEn ? "Loading Motion Canvas…" : "正在加载 Motion Canvas 动效…" }}</span>
        </div>
      </div>

      <!-- Interactive 4-Step Progress Timeline -->
      <div class="bp-mc-steps">
        <button
          v-for="(step, idx) in WORKFLOW_STEPS"
          :key="step.id"
          type="button"
          class="bp-mc-step-btn"
          :class="{
            'is-active': activeStepIndex === idx,
            'is-done': activeStepIndex > idx,
          }"
          @click.stop="handleStepSelect(idx)"
        >
          <span class="bp-mc-step-bar" aria-hidden="true">
            <span
              class="bp-mc-step-fill"
              :style="{ width: `${stepProgress(idx)}%` }"
            />
          </span>
          <span class="bp-mc-step-meta">
            <span class="bp-mc-step-num">{{ step.num }}</span>
            <span class="bp-mc-step-name">{{ isEn ? step.en : step.zh }}</span>
          </span>
        </button>

        <button
          type="button"
          class="bp-mc-play-btn"
          :aria-label="paused ? (isEn ? 'Play animation' : '播放动画') : (isEn ? 'Pause animation' : '暂停动画')"
          :title="paused ? (isEn ? 'Play' : '播放') : (isEn ? 'Pause' : '暂停')"
          @click.stop="togglePlayPause"
        >
          <svg v-if="paused" viewBox="0 0 16 16" width="13" height="13" fill="currentColor" aria-hidden="true">
            <path d="M4.5 3.2L12.6 8l-8.1 4.8V3.2z" />
          </svg>
          <svg v-else viewBox="0 0 16 16" width="13" height="13" fill="currentColor" aria-hidden="true">
            <rect x="3.5" y="3" width="3.2" height="10" rx="1" />
            <rect x="9.3" y="3" width="3.2" height="10" rx="1" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bp-mc-popover {
  position: absolute;
  top: calc(100% + 14px);
  left: 50%;
  width: min(780px, calc(100vw - 32px));
  z-index: 40;
  pointer-events: none;
  opacity: 0;
  visibility: hidden;
  transform: translateX(-50%) translateY(-10px) scale(0.955);
  transform-origin: top center;
  clip-path: inset(0 0 16% 0 round 18px);
  filter: blur(4px);
  transition:
    opacity 0.24s cubic-bezier(0.32, 0, 0.67, 0),
    transform 0.28s cubic-bezier(0.32, 0, 0.67, 0),
    clip-path 0.28s cubic-bezier(0.32, 0, 0.67, 0),
    filter 0.24s ease,
    visibility 0s linear 0.28s;
}

.bp-mc-popover.is-open {
  pointer-events: auto;
  opacity: 1;
  visibility: visible;
  transform: translateX(-50%) translateY(0) scale(1);
  clip-path: inset(0 0 0% 0 round 18px);
  filter: blur(0);
  transition:
    opacity 0.28s cubic-bezier(0.16, 1, 0.3, 1),
    transform 0.38s cubic-bezier(0.16, 1, 0.3, 1),
    clip-path 0.38s cubic-bezier(0.16, 1, 0.3, 1),
    filter 0.28s ease,
    visibility 0s linear 0s;
}

.bp-mc-bridge {
  position: absolute;
  top: -18px;
  left: 15%;
  right: 15%;
  height: 20px;
}

.bp-mc-shell {
  border-radius: 18px;
  border: 1px solid var(--bp-border);
  background: var(--bp-bg);
  box-shadow:
    0 24px 64px -16px rgba(15, 23, 42, 0.22),
    0 8px 20px -8px rgba(15, 23, 42, 0.10),
    0 0 0 1px rgba(84, 125, 208, 0.10);
  overflow: hidden;
  text-align: left;
}

.dark .bp-mc-shell {
  border-color: #2f3746;
  background: #171b22;
  box-shadow:
    0 28px 72px -12px rgba(0, 0, 0, 0.68),
    0 0 0 1px rgba(119, 157, 232, 0.16);
}

.bp-mc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--bp-border);
  background: var(--bp-bg-alt);
}

.bp-mc-head-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.bp-mc-live-dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: var(--bp-accent);
  flex-shrink: 0;
  box-shadow: 0 0 0 3px var(--vp-c-brand-soft);
  animation: bp-mc-pulse 1.8s ease-in-out infinite;
}

.bp-mc-live-dot.is-paused {
  animation: none;
  opacity: 0.6;
}

@keyframes bp-mc-pulse {
  0%, 100% {
    box-shadow: 0 0 0 2px var(--vp-c-brand-soft);
  }
  50% {
    box-shadow: 0 0 0 5px var(--vp-c-brand-soft);
  }
}

.bp-mc-step-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--bp-text-1);
  white-space: nowrap;
}

.bp-mc-step-desc {
  font-size: 12px;
  color: var(--bp-text-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.bp-mc-head-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.bp-mc-engine-tag {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--vp-c-brand-soft);
  color: var(--bp-accent-strong);
  font-family: var(--bp-font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.01em;
}

.dark .bp-mc-engine-tag {
  color: var(--bp-accent);
}

.bp-mc-mini-btn {
  height: 24px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--bp-btn-border);
  background: var(--bp-bg);
  color: var(--bp-text-1);
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 0.14s ease, background-color 0.14s ease, color 0.14s ease;
}

.bp-mc-mini-btn:hover {
  border-color: var(--bp-accent);
  background: var(--bp-hover);
}

.bp-mc-mini-btn.is-copied {
  border-color: var(--bp-accent);
  background: var(--vp-c-brand-soft);
  color: var(--bp-accent-strong);
}

.dark .bp-mc-mini-btn.is-copied {
  color: var(--bp-accent);
}

.bp-mc-viewport {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  background: var(--bp-canvas);
  overflow: hidden;
}

.bp-mc-canvas-host {
  width: 100%;
  height: 100%;
}

.bp-mc-canvas-host :deep(.bp-mc-canvas) {
  display: block;
  width: 100%;
  height: 100%;
}

.bp-mc-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--bp-text-2);
}

.bp-mc-loading-dot {
  width: 7px;
  height: 7px;
  border-radius: 2px;
  background: var(--bp-accent);
}

.bp-mc-steps {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr)) auto;
  align-items: center;
  gap: 8px;
  padding: 10px 14px 12px;
  border-top: 1px solid var(--bp-border);
  background: var(--bp-bg-alt);
}

.bp-mc-step-btn {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 6px 8px;
  border-radius: 8px;
  border: 0;
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.14s ease;
}

.bp-mc-step-btn:hover {
  background: var(--bp-hover);
}

.bp-mc-step-bar {
  display: block;
  width: 100%;
  height: 3px;
  border-radius: 999px;
  background: var(--bp-border);
  overflow: hidden;
}

.bp-mc-step-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--bp-accent);
  transition: width 0.08s linear;
}

.bp-mc-step-meta {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}

.bp-mc-step-num {
  font-family: var(--bp-font-mono);
  font-size: 10.5px;
  font-weight: 600;
  color: var(--bp-text-3);
  transition: color 0.14s ease;
}

.bp-mc-step-name {
  font-size: 12px;
  font-weight: 500;
  color: var(--bp-text-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: color 0.14s ease;
}

.bp-mc-step-btn.is-active .bp-mc-step-num,
.bp-mc-step-btn.is-done .bp-mc-step-num {
  color: var(--bp-accent-strong);
}

.dark .bp-mc-step-btn.is-active .bp-mc-step-num,
.dark .bp-mc-step-btn.is-done .bp-mc-step-num {
  color: var(--bp-accent);
}

.bp-mc-step-btn.is-active .bp-mc-step-name {
  color: var(--bp-text-1);
  font-weight: 600;
}

.bp-mc-play-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  border: 1px solid var(--bp-border);
  background: var(--bp-bg);
  color: var(--bp-text-2);
  cursor: pointer;
  transition: border-color 0.14s ease, color 0.14s ease, background-color 0.14s ease;
}

.bp-mc-play-btn:hover {
  border-color: var(--bp-accent);
  color: var(--bp-text-1);
  background: var(--bp-hover);
}

@media (max-width: 720px) {
  .bp-mc-step-desc {
    display: none;
  }

  .bp-mc-steps {
    grid-template-columns: repeat(2, minmax(0, 1fr)) auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .bp-mc-popover,
  .bp-mc-popover.is-open {
    transition: opacity 0.12s ease !important;
    transform: translateX(-50%) !important;
    clip-path: none !important;
    filter: none !important;
  }

  .bp-mc-live-dot {
    animation: none !important;
  }
}
</style>
