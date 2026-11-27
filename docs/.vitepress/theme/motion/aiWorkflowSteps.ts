export interface WorkflowStepMeta {
  id: string;
  num: string;
  startSec: number;
  endSec: number;
  zh: string;
  en: string;
  descZh: string;
  descEn: string;
}

export const SCENE_WIDTH = 960;
export const SCENE_HEIGHT = 540;
export const SCENE_FPS = 60;
export const TOTAL_DURATION_SEC = 12.77;
export const REDUCED_MOTION_PREVIEW_SEC = 11.4;

export const WORKFLOW_STEPS: WorkflowStepMeta[] = [
  {
    id: "copy",
    num: "01",
    startSec: 0.0,
    endSec: 2.4,
    zh: "复制规范链接",
    en: "Copy Spec URL",
    descZh: "一键复制完整 AI 开发规范地址，无需翻阅文档页面",
    descEn: "One click copies the full self-contained spec URL",
  },
  {
    id: "prompt",
    num: "02",
    startSec: 2.4,
    endSec: 6.36,
    zh: "发给任意 AI",
    en: "Send to Any AI",
    descZh: "粘贴链接并描述插件想法，AI 自动读取全部 6 章规范",
    descEn: "Paste with your idea — AI fetches all 6 chapters automatically",
  },
  {
    id: "verify",
    num: "03",
    startSec: 6.36,
    endSec: 9.49,
    zh: "生成与沙盒校验",
    en: "Generate & Verify",
    descZh: "输出清单、quickjs-ng 入口与文档，自动通过协议检查",
    descEn: "Outputs manifest, quickjs-ng entry, and passes protocol check",
  },
  {
    id: "run",
    num: "04",
    startSec: 9.49,
    endSec: 12.77,
    zh: "Block 桌面运行",
    en: "Live in Block",
    descZh: "原生 Ocean Paper 控件即时挂载，独立建仓无需提 PR",
    descEn: "Native Ocean Paper UI mounts live — zero PR to publish",
  },
];
