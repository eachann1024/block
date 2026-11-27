import {
  DefaultPlugin,
  MetaFile,
  Player,
  Stage,
  ValueDispatcher,
  Vector2,
  all,
  bootstrap,
  chain,
  createRef,
  createSignal,
  easeInCubic,
  easeInOutCubic,
  easeOutBack,
  easeOutCubic,
  easeOutExpo,
  linear,
  sequence,
  useScene,
  waitFor,
  type FullSceneDescription,
} from "@motion-canvas/core";
import {
  Circle,
  Gradient,
  Grid,
  Line,
  Node,
  Rect,
  Txt,
  jsx,
  makeScene2D,
} from "@motion-canvas/2d";
import {
  SCENE_FPS,
  SCENE_HEIGHT,
  SCENE_WIDTH,
  WORKFLOW_STEPS,
} from "./aiWorkflowSteps";

interface Palette {
  bg: string;
  grid: string;
  surface: string;
  surfaceAlt: string;
  shell: string;
  codeBg: string;
  border: string;
  borderStrong: string;
  text1: string;
  text2: string;
  text3: string;
  btnBg: string;
  btnText: string;
  accent: string;
  accentStrong: string;
  accentSoft: string;
  accentGlow: string;
  ok: string;
  okSoft: string;
  shadow: string;
  dotMuted: string;
}

const LIGHT_PALETTE: Palette = {
  bg: "#f7f9fc",
  grid: "rgba(32, 36, 44, 0.055)",
  surface: "#ffffff",
  surfaceAlt: "#f9fafc",
  shell: "#f1f3f7",
  codeBg: "#f3f1ec",
  border: "#dfe3eb",
  borderStrong: "#c7d2e4",
  text1: "#1d222b",
  text2: "#526071",
  text3: "#7c899b",
  btnBg: "#20242c",
  btnText: "#ffffff",
  accent: "#547dd0",
  accentStrong: "#3d63b0",
  accentSoft: "rgba(84, 125, 208, 0.12)",
  accentGlow: "rgba(84, 125, 208, 0.22)",
  ok: "#26894b",
  okSoft: "rgba(38, 137, 75, 0.12)",
  shadow: "rgba(18, 28, 45, 0.10)",
  dotMuted: "#cfd5df",
};

const DARK_PALETTE: Palette = {
  bg: "#13161c",
  grid: "rgba(236, 240, 248, 0.048)",
  surface: "#1b1f27",
  surfaceAlt: "#222732",
  shell: "#262c38",
  codeBg: "#171a21",
  border: "#2c3342",
  borderStrong: "#3e4b64",
  text1: "#eef1f6",
  text2: "#aeb7c6",
  text3: "#7c8698",
  btnBg: "#eceff5",
  btnText: "#14171f",
  accent: "#779de8",
  accentStrong: "#95b5f4",
  accentSoft: "rgba(119, 157, 232, 0.16)",
  accentGlow: "rgba(119, 157, 232, 0.24)",
  ok: "#46c274",
  okSoft: "rgba(70, 194, 116, 0.15)",
  shadow: "rgba(0, 0, 0, 0.42)",
  dotMuted: "#3f4758",
};

const FONT_SANS =
  '-apple-system, BlinkMacSystemFont, "SF Pro Display", "PingFang SC", "Segoe UI", sans-serif';
const FONT_MONO =
  '"JetBrains Mono", Menlo, ui-monospace, SFMono-Regular, Consolas, monospace';

const aiWorkflowScene = makeScene2D(function* (view) {
  const darkVar = useScene().variables.get("dark", false);
  const enVar = useScene().variables.get("en", false);

  const pal = () => (darkVar() ? DARK_PALETTE : LIGHT_PALETTE);
  const isEn = () => Boolean(enVar());

  const activeStep = createSignal(0);
  const glowPos = Vector2.createSignal([0, -10]);
  const glowSize = createSignal(420);
  const glowAlpha = createSignal(0.55);

  // Subtle Ocean Paper architectural backdrop
  view.add(
    jsx(Rect, {
      width: SCENE_WIDTH,
      height: SCENE_HEIGHT,
      fill: () => pal().bg,
    }),
  );

  view.add(
    jsx(Grid, {
      width: SCENE_WIDTH,
      height: SCENE_HEIGHT,
      spacing: 32,
      stroke: () => pal().grid,
      lineWidth: 1,
    }),
  );

  // Ambient cobalt radial glow that follows the narrative focus (GPU-friendly radial gradient)
  const ambientGradient = new Gradient({
    type: "radial",
    fromRadius: 0,
    toRadius: 240,
    stops: [
      { offset: 0, color: () => pal().accentGlow },
      { offset: 0.55, color: () => pal().accentSoft },
      { offset: 1, color: "rgba(84, 125, 208, 0)" },
    ],
  });

  view.add(
    jsx(Circle, {
      position: glowPos,
      size: glowSize,
      fill: ambientGradient,
      opacity: glowAlpha,
    }),
  );

  // Top telemetry strip inside canvas
  const stepPillWidth = createSignal(214);
  view.add(
    jsx(Rect, {
      x: -440,
      y: -234,
      offset: [-1, 0],
      width: stepPillWidth,
      height: 28,
      radius: 14,
      fill: () => pal().surface,
      stroke: () => pal().border,
      lineWidth: 1,
      children: [
        jsx(Circle, {
          x: -91,
          size: 7,
          fill: () => pal().accent,
        }),
        jsx(Txt, {
          x: -76,
          offset: [-1, 0],
          fontFamily: FONT_MONO,
          fontSize: 11.5,
          fontWeight: 600,
          fill: () => pal().text1,
          text: () => {
            const step = WORKFLOW_STEPS[Math.round(activeStep())] ?? WORKFLOW_STEPS[0];
            return `STEP ${step.num} · ${isEn() ? step.en.toUpperCase() : step.zh}`;
          },
        }),
      ],
    }),
  );

  view.add(
    jsx(Txt, {
      x: 440,
      y: -234,
      offset: [1, 0],
      fontFamily: FONT_MONO,
      fontSize: 11,
      fontWeight: 500,
      fill: () => pal().text3,
      text: "quickjs-ng · JSON-RPC 2.0 · 16 UIItem",
    }),
  );

  // =========================================================================
  // ACT 1: One-click Copy Spec URL (0.0s -> 2.6s)
  // =========================================================================
  const act1 = createRef<Node>();
  const heroBtn = createRef<Rect>();
  const heroBtnLabel = createRef<Txt>();
  const heroBtnMark = createRef<Rect>();
  const ripple1 = createRef<Circle>();
  const ripple2 = createRef<Circle>();
  const cursor = createRef<Node>();
  const specDock = createRef<Rect>();
  const specUrlText = () =>
    isEn()
      ? "docs.block.cooool.top/en/plugin-dev-for-ai.md"
      : "docs.block.cooool.top/plugin-dev-for-ai.md";
  const btnCopied = createSignal(false);

  view.add(
    jsx(Node, {
      ref: act1,
      children: [
        jsx(Circle, {
          ref: ripple1,
          position: [0, -10],
          size: 48,
          stroke: () => pal().accent,
          lineWidth: 2.5,
          opacity: 0,
        }),
        jsx(Circle, {
          ref: ripple2,
          position: [0, -10],
          size: 48,
          stroke: () => pal().accentStrong,
          lineWidth: 1.5,
          opacity: 0,
        }),
        jsx(Rect, {
          ref: heroBtn,
          position: [0, -10],
          width: 326,
          height: 62,
          radius: 31,
          fill: () => (btnCopied() ? pal().accentStrong : pal().btnBg),
          stroke: () => (btnCopied() ? pal().accent : pal().borderStrong),
          lineWidth: 1.2,
          shadowColor: () => pal().shadow,
          shadowBlur: 26,
          shadowOffsetY: 10,
          scale: 0.9,
          opacity: 0,
          children: [
            jsx(Rect, {
              ref: heroBtnMark,
              x: -124,
              size: 11,
              radius: 2.5,
              fill: () => (btnCopied() ? "#ffffff" : pal().accent),
            }),
            jsx(Txt, {
              ref: heroBtnLabel,
              x: 10,
              fontFamily: FONT_SANS,
              fontSize: 16,
              fontWeight: 600,
              fill: () => (btnCopied() ? "#ffffff" : pal().btnText),
              text: () =>
                btnCopied()
                  ? isEn()
                    ? "✓ Spec URL Copied"
                    : "✓ 已复制 AI 规范链接"
                  : isEn()
                    ? "Copy for AI & Start Building"
                    : "复制给 AI 开始开发",
            }),
          ],
        }),
        // Crisp geometric pointer cursor
        jsx(Node, {
          ref: cursor,
          position: [152, 86],
          opacity: 0,
          children: [
            jsx(Line, {
              points: [
                [0, 0],
                [0, 22],
                [6, 17],
                [10.5, 25],
                [14, 23.2],
                [9.5, 15.5],
                [16.5, 15.5],
              ],
              closed: true,
              fill: () => pal().text1,
              stroke: () => pal().surface,
              lineWidth: 2,
              shadowColor: () => pal().shadow,
              shadowBlur: 10,
            }),
          ],
        }),
      ],
    }),
  );

  // Floating Spec URL capsule that persists into Act 2
  view.add(
    jsx(Rect, {
      ref: specDock,
      position: [0, 56],
      width: 392,
      height: 40,
      radius: 20,
      fill: () => pal().surface,
      stroke: () => pal().accent,
      lineWidth: 1.4,
      shadowColor: () => pal().shadow,
      shadowBlur: 18,
      shadowOffsetY: 6,
      opacity: 0,
      scale: 0.86,
      children: [
        jsx(Circle, {
          x: -174,
          size: 8,
          fill: () => pal().accent,
        }),
        jsx(Txt, {
          x: -158,
          offset: [-1, 0],
          fontFamily: FONT_MONO,
          fontSize: 12.2,
          fontWeight: 500,
          fill: () => pal().text1,
          text: specUrlText,
        }),
      ],
    }),
  );

  // =========================================================================
  // ACT 2: Paste to AI + Auto-Read 172KB Spec (2.6s -> 5.6s)
  // =========================================================================
  const act2 = createRef<Node>();
  const wireToAi = createRef<Line>();
  const orbToAi = createRef<Circle>();
  const orbToAiT = createSignal(0);

  const aiCard = createRef<Rect>();
  const attachedChip = createRef<Rect>();
  const promptTxt = createRef<Txt>();
  const promptCaret = createRef<Circle>();

  const wireToSpec = createRef<Line>();
  const orbToSpec = createRef<Circle>();
  const orbToSpecT = createSignal(0);

  const specCard = createRef<Rect>();
  const scanBeam = createRef<Rect>();
  const specRowLights = [
    createSignal(false),
    createSignal(false),
    createSignal(false),
    createSignal(false),
  ];

  const specRowsZh = [
    "01–06 完整插件章节与红线",
    "manifest.json 严格 Schema",
    "16 种原生 Ocean Paper 控件",
    "30 项分层权限 · 双语骨架",
  ];
  const specRowsEn = [
    "01–06 Chapters & Hard Rules",
    "Strict manifest.json Schema",
    "16 Native Ocean Paper UIItem",
    "30 Scoped Perms & Starter SDK",
  ];

  view.add(
    jsx(Node, {
      ref: act2,
      opacity: 0,
      children: [
        // Wire from docked URL capsule into AI prompt window
        jsx(Line, {
          ref: wireToAi,
          points: [
            [-235, -138],
            [-235, -82],
            [-155, -82],
          ],
          radius: 16,
          stroke: () => pal().accent,
          lineWidth: 2,
          end: 0,
          endArrow: true,
          arrowSize: 9,
        }),
        jsx(Circle, {
          ref: orbToAi,
          size: 9,
          fill: () => pal().accentStrong,
          shadowColor: () => pal().accent,
          shadowBlur: 14,
          opacity: 0,
          position: () =>
            wireToAi().getPointAtPercentage(orbToAiT()).position,
        }),

        // AI Coding Agent Card
        jsx(Rect, {
          ref: aiCard,
          position: [-155, 36],
          width: 472,
          height: 278,
          radius: 16,
          fill: () => pal().surface,
          stroke: () => pal().border,
          lineWidth: 1.2,
          shadowColor: () => pal().shadow,
          shadowBlur: 28,
          shadowOffsetY: 12,
          scale: 0.94,
          opacity: 0,
          children: [
            // Card header
            jsx(Rect, {
              y: -115,
              width: 472,
              height: 48,
              radius: [16, 16, 0, 0],
              fill: () => pal().surfaceAlt,
            }),
            jsx(Line, {
              points: [
                [-236, -91],
                [236, -91],
              ],
              stroke: () => pal().border,
              lineWidth: 1,
            }),
            jsx(Txt, {
              x: -214,
              y: -115,
              offset: [-1, 0],
              fontFamily: FONT_SANS,
              fontSize: 13.5,
              fontWeight: 600,
              fill: () => pal().text1,
              text: () => (isEn() ? "Any AI Coding Agent" : "任意 AI 编程助手"),
            }),
            ...["Claude", "Codex", "Cursor"].map((name, idx) =>
              jsx(Rect, {
                x: 76 + idx * 58,
                y: -115,
                width: 52,
                height: 22,
                radius: 11,
                fill: () => pal().shell,
                stroke: () => pal().border,
                lineWidth: 1,
                children: [
                  jsx(Txt, {
                    fontFamily: FONT_MONO,
                    fontSize: 10.2,
                    fontWeight: 500,
                    fill: () => pal().text2,
                    text: name,
                  }),
                ],
              }),
            ),
            // Prompt composer area
            jsx(Rect, {
              y: 12,
              width: 432,
              height: 170,
              radius: 12,
              fill: () => pal().codeBg,
              stroke: () => pal().borderStrong,
              lineWidth: 1,
              children: [
                // Attached URL chip inside prompt
                jsx(Rect, {
                  ref: attachedChip,
                  x: -198,
                  y: -52,
                  offset: [-1, 0],
                  width: 248,
                  height: 30,
                  radius: 8,
                  fill: () => pal().accentSoft,
                  stroke: () => pal().accent,
                  lineWidth: 1,
                  scale: 0.7,
                  opacity: 0,
                  children: [
                    jsx(Circle, {
                      x: -108,
                      size: 6,
                      fill: () => pal().accent,
                    }),
                    jsx(Txt, {
                      x: -96,
                      offset: [-1, 0],
                      fontFamily: FONT_MONO,
                      fontSize: 11.5,
                      fontWeight: 600,
                      fill: () => pal().accentStrong,
                      text: () =>
                        isEn()
                          ? "en/plugin-dev-for-ai.md"
                          : "plugin-dev-for-ai.md",
                    }),
                  ],
                }),
                // Typewriter user prompt
                jsx(Txt, {
                  ref: promptTxt,
                  x: -198,
                  y: 4,
                  offset: [-1, -1],
                  width: 396,
                  textWrap: true,
                  fontFamily: FONT_SANS,
                  fontSize: 15,
                  lineHeight: 24,
                  fontWeight: 500,
                  fill: () => pal().text1,
                  text: "",
                }),
                jsx(Circle, {
                  ref: promptCaret,
                  x: -194,
                  y: 66,
                  size: 7,
                  fill: () => pal().accent,
                  opacity: 0,
                }),
                jsx(Txt, {
                  x: -180,
                  y: 66,
                  offset: [-1, 0],
                  fontFamily: FONT_MONO,
                  fontSize: 10.5,
                  fill: () => pal().text3,
                  text: () => (isEn() ? "Reading spec..." : "自动读取规范中…"),
                }),
                jsx(Txt, {
                  x: 196,
                  y: 66,
                  offset: [1, 0],
                  fontFamily: FONT_MONO,
                  fontSize: 10.5,
                  fill: () => pal().text3,
                  text: () => (isEn() ? "↵ Auto-reads URL" : "↵ 自动抓取规范全文"),
                }),
              ],
            }),
          ],
        }),

        // Wire from AI Agent to Auto-Fetch Spec Card
        jsx(Line, {
          ref: wireToSpec,
          points: [
            [81, 36],
            [140, 36],
          ],
          stroke: () => pal().accent,
          lineWidth: 2,
          end: 0,
          endArrow: true,
          arrowSize: 9,
        }),
        jsx(Circle, {
          ref: orbToSpec,
          size: 9,
          fill: () => pal().accentStrong,
          shadowColor: () => pal().accent,
          shadowBlur: 14,
          opacity: 0,
          position: () =>
            wireToSpec().getPointAtPercentage(orbToSpecT()).position,
        }),

        // Auto-Fetch Spec Ingestion Card
        jsx(Rect, {
          ref: specCard,
          position: [288, 36],
          width: 292,
          height: 278,
          radius: 16,
          fill: () => pal().surface,
          stroke: () => pal().border,
          lineWidth: 1.2,
          shadowColor: () => pal().shadow,
          shadowBlur: 28,
          shadowOffsetY: 12,
          clip: true,
          scale: 0.94,
          opacity: 0,
          children: [
            jsx(Txt, {
              x: -124,
              y: -112,
              offset: [-1, 0],
              fontFamily: FONT_MONO,
              fontSize: 11,
              fontWeight: 600,
              fill: () => pal().accentStrong,
              text: "AUTO-FETCH · 172 KB",
            }),
            jsx(Txt, {
              x: -124,
              y: -88,
              offset: [-1, 0],
              fontFamily: FONT_SANS,
              fontSize: 14,
              fontWeight: 600,
              fill: () => pal().text1,
              text: () => (isEn() ? "AI Reads Full Spec" : "AI 自动读取全量规范"),
            }),
            ...[0, 1, 2, 3].map((idx) =>
              jsx(Rect, {
                y: -36 + idx * 46,
                width: 252,
                height: 36,
                radius: 9,
                fill: () =>
                  specRowLights[idx]() ? pal().accentSoft : pal().surfaceAlt,
                stroke: () =>
                  specRowLights[idx]() ? pal().accent : pal().border,
                lineWidth: 1,
                children: [
                  jsx(Circle, {
                    x: -108,
                    size: 7,
                    fill: () =>
                      specRowLights[idx]() ? pal().ok : pal().dotMuted,
                  }),
                  jsx(Txt, {
                    x: -94,
                    offset: [-1, 0],
                    fontFamily: FONT_SANS,
                    fontSize: 12,
                    fontWeight: 500,
                    fill: () =>
                      specRowLights[idx]() ? pal().text1 : pal().text2,
                    text: () => (isEn() ? specRowsEn[idx] : specRowsZh[idx]),
                  }),
                ],
              }),
            ),
            // Scanning laser bar sweeping down the spec rows
            jsx(Rect, {
              ref: scanBeam,
              y: -70,
              width: 292,
              height: 22,
              fill: () => pal().accentGlow,
              opacity: 0,
            }),
          ],
        }),
      ],
    }),
  );

  // =========================================================================
  // ACT 3: Generate Files & Sandbox Check (5.6s -> 8.6s)
  // =========================================================================
  const act3 = createRef<Node>();
  const fileRefs = [createRef<Rect>(), createRef<Rect>(), createRef<Rect>()];
  const fileWires = [createRef<Line>(), createRef<Line>(), createRef<Line>()];
  const fileOrbs = [
    createRef<Circle>(),
    createRef<Circle>(),
    createRef<Circle>(),
  ];
  const fileOrbTs = [createSignal(0), createSignal(0), createSignal(0)];

  const checkTerminal = createRef<Rect>();
  const checkBar = createRef<Rect>();
  const checkRows = [createRef<Node>(), createRef<Node>(), createRef<Node>()];
  const checkPassed = createSignal(false);

  const fileDefs = [
    {
      y: -86,
      name: "manifest.json",
      zh: "清单 · 30 项最小权限声明",
      en: "Manifest · Scoped permissions",
    },
    {
      y: 12,
      name: "entry.js",
      zh: "quickjs-ng 沙盒 · JSON-RPC 2.0",
      en: "quickjs-ng sandbox · JSON-RPC 2.0",
    },
    {
      y: 110,
      name: "README.md",
      zh: "双语文档 · 自动上架工作流",
      en: "Bilingual docs · Auto-release",
    },
  ];

  const checkDefs = [
    {
      zh: "✓ 清单与 30 项分层权限校验通过",
      en: "✓ Manifest & 30-perm schema OK",
    },
    {
      zh: "✓ quickjs-ng 32 MiB 沙盒运行通过",
      en: "✓ quickjs-ng 32 MiB sandbox OK",
    },
    {
      zh: "✓ [PASS] 16 种原生控件协议达标",
      en: "✓ [PASS] Ready for Block Desktop",
    },
  ];

  view.add(
    jsx(Node, {
      ref: act3,
      opacity: 0,
      children: [
        // 3 Staggered generated plugin artifact cards
        ...fileDefs.map((file, idx) =>
          jsx(Rect, {
            ref: fileRefs[idx],
            position: [-305, file.y],
            width: 274,
            height: 76,
            radius: 13,
            fill: () => pal().surface,
            stroke: () => pal().border,
            lineWidth: 1.2,
            shadowColor: () => pal().shadow,
            shadowBlur: 20,
            shadowOffsetY: 8,
            opacity: 0,
            children: [
              jsx(Rect, {
                x: -112,
                width: 28,
                height: 28,
                radius: 7,
                fill: () => pal().accentSoft,
                children: [
                  jsx(Txt, {
                    fontFamily: FONT_MONO,
                    fontSize: 11,
                    fontWeight: 700,
                    fill: () => pal().accentStrong,
                    text: idx === 0 ? "{}" : idx === 1 ? "JS" : "MD",
                  }),
                ],
              }),
              jsx(Txt, {
                x: -88,
                y: -11,
                offset: [-1, 0],
                fontFamily: FONT_MONO,
                fontSize: 13.5,
                fontWeight: 600,
                fill: () => pal().text1,
                text: file.name,
              }),
              jsx(Txt, {
                x: -88,
                y: 13,
                offset: [-1, 0],
                fontFamily: FONT_SANS,
                fontSize: 11.5,
                fill: () => pal().text2,
                text: () => (isEn() ? file.en : file.zh),
              }),
            ],
          }),
        ),

        // 3 Converging circuit wires + travelling orbs
        ...fileDefs.map((file, idx) =>
          jsx(Line, {
            ref: fileWires[idx],
            points:
              idx === 1
                ? [
                    [-118, 12],
                    [22, 12],
                  ]
                : [
                    [-118, file.y],
                    [-48, file.y],
                    [-48, 12],
                    [22, 12],
                  ],
            radius: 16,
            stroke: () => (checkPassed() ? pal().ok : pal().accent),
            lineWidth: 2,
            end: 0,
            endArrow: idx === 1,
            arrowSize: 9,
          }),
        ),
        ...fileDefs.map((_, idx) =>
          jsx(Circle, {
            ref: fileOrbs[idx],
            size: 8.5,
            fill: () => pal().accentStrong,
            shadowColor: () => pal().accent,
            shadowBlur: 12,
            opacity: 0,
            position: () =>
              fileWires[idx]().getPointAtPercentage(fileOrbTs[idx]()).position,
          }),
        ),

        // Sandbox Check Terminal Card
        jsx(Rect, {
          ref: checkTerminal,
          position: [232, 12],
          width: 404,
          height: 274,
          radius: 16,
          fill: () => pal().surface,
          stroke: () => (checkPassed() ? pal().ok : pal().border),
          lineWidth: 1.5,
          shadowColor: () => pal().shadow,
          shadowBlur: 28,
          shadowOffsetY: 12,
          scale: 0.94,
          opacity: 0,
          children: [
            jsx(Rect, {
              y: -114,
              width: 404,
              height: 46,
              radius: [16, 16, 0, 0],
              fill: () => pal().surfaceAlt,
            }),
            jsx(Line, {
              points: [
                [-202, -91],
                [202, -91],
              ],
              stroke: () => pal().border,
              lineWidth: 1,
            }),
            jsx(Circle, { x: -176, y: -114, size: 8, fill: () => pal().dotMuted }),
            jsx(Circle, { x: -162, y: -114, size: 8, fill: () => pal().dotMuted }),
            jsx(Circle, { x: -148, y: -114, size: 8, fill: () => pal().dotMuted }),
            jsx(Txt, {
              x: -128,
              y: -114,
              offset: [-1, 0],
              fontFamily: FONT_MONO,
              fontSize: 12,
              fontWeight: 600,
              fill: () => pal().text1,
              text: "block-plugin-check",
            }),
            jsx(Rect, {
              x: 152,
              y: -114,
              width: 64,
              height: 22,
              radius: 11,
              fill: () => (checkPassed() ? pal().okSoft : pal().accentSoft),
              children: [
                jsx(Txt, {
                  fontFamily: FONT_MONO,
                  fontSize: 10.5,
                  fontWeight: 700,
                  fill: () => (checkPassed() ? pal().ok : pal().accentStrong),
                  text: () => (checkPassed() ? "PASS" : "CHECK"),
                }),
              ],
            }),

            // Progress rail
            jsx(Rect, {
              y: -60,
              width: 356,
              height: 8,
              radius: 4,
              fill: () => pal().shell,
              children: [
                jsx(Rect, {
                  ref: checkBar,
                  x: -178,
                  offset: [-1, 0],
                  width: 0,
                  height: 8,
                  radius: 4,
                  fill: () => (checkPassed() ? pal().ok : pal().accent),
                }),
              ],
            }),

            // 3 Staggered check result rows
            ...checkDefs.map((row, idx) =>
              jsx(Node, {
                ref: checkRows[idx],
                y: -10 + idx * 46,
                opacity: 0,
                children: [
                  jsx(Rect, {
                    width: 356,
                    height: 36,
                    radius: 9,
                    fill: () => (idx === 2 ? pal().okSoft : pal().surfaceAlt),
                    stroke: () => (idx === 2 ? pal().ok : pal().border),
                    lineWidth: 1,
                    children: [
                      jsx(Txt, {
                        x: -162,
                        offset: [-1, 0],
                        fontFamily: FONT_MONO,
                        fontSize: 12,
                        fontWeight: idx === 2 ? 600 : 500,
                        fill: () => (idx === 2 ? pal().ok : pal().text1),
                        text: () => (isEn() ? row.en : row.zh),
                      }),
                    ],
                  }),
                ],
              }),
            ),
          ],
        }),
      ],
    }),
  );

  // =========================================================================
  // ACT 4: Live Inside Block Desktop App (8.6s -> 11.8s)
  // =========================================================================
  const act4 = createRef<Node>();
  const appWindow = createRef<Rect>();
  const titlebarPlugin = createRef<Rect>();
  const titlebarRipple = createRef<Circle>();
  const pluginPanel = createRef<Rect>();
  const ringArc = createRef<Circle>();
  const timerSec = createSignal(1500); // 25:00
  const editorCaret = createRef<Rect>();
  const footerBadge = createRef<Rect>();

  const formatTimer = () => {
    const s = Math.max(0, Math.round(timerSec()));
    const mm = String(Math.floor(s / 60)).padStart(2, "0");
    const ss = String(s % 60).padStart(2, "0");
    return `${mm}:${ss}`;
  };

  view.add(
    jsx(Node, {
      ref: act4,
      opacity: 0,
      children: [
        jsx(Rect, {
          ref: appWindow,
          position: [0, -2],
          width: 792,
          height: 364,
          radius: 16,
          fill: () => pal().surface,
          stroke: () => pal().borderStrong,
          lineWidth: 1.3,
          shadowColor: () => pal().shadow,
          shadowBlur: 34,
          shadowOffsetY: 16,
          clip: true,
          scale: 0.92,
          children: [
            // Titlebar
            jsx(Rect, {
              y: -160,
              width: 792,
              height: 44,
              fill: () => pal().surfaceAlt,
            }),
            jsx(Line, {
              points: [
                [-396, -138],
                [396, -138],
              ],
              stroke: () => pal().border,
              lineWidth: 1,
            }),
            jsx(Circle, { x: -372, y: -160, size: 9, fill: () => pal().dotMuted }),
            jsx(Circle, { x: -356, y: -160, size: 9, fill: () => pal().dotMuted }),
            jsx(Circle, { x: -340, y: -160, size: 9, fill: () => pal().dotMuted }),
            jsx(Txt, {
              x: -312,
              y: -160,
              offset: [-1, 0],
              fontFamily: FONT_SANS,
              fontSize: 12.5,
              fontWeight: 600,
              fill: () => pal().text1,
              text: "Block — Weekly Focus.md",
            }),
            // Mounted Titlebar Plugin Chip
            jsx(Circle, {
              ref: titlebarRipple,
              x: 332,
              y: -160,
              size: 16,
              stroke: () => pal().accent,
              lineWidth: 2,
              opacity: 0,
            }),
            jsx(Rect, {
              ref: titlebarPlugin,
              x: 332,
              y: -160,
              width: 88,
              height: 26,
              radius: 13,
              fill: () => pal().accentSoft,
              stroke: () => pal().accent,
              lineWidth: 1.2,
              scale: 0.5,
              opacity: 0,
              children: [
                jsx(Circle, {
                  x: -28,
                  size: 6,
                  fill: () => pal().accent,
                }),
                jsx(Txt, {
                  x: 8,
                  fontFamily: FONT_MONO,
                  fontSize: 11.5,
                  fontWeight: 600,
                  fill: () => pal().accentStrong,
                  text: formatTimer,
                }),
              ],
            }),

            // Left Notes Sidebar
            jsx(Rect, {
              x: -322,
              y: 22,
              width: 148,
              height: 320,
              fill: () => pal().surfaceAlt,
            }),
            jsx(Line, {
              points: [
                [-248, -138],
                [-248, 182],
              ],
              stroke: () => pal().border,
              lineWidth: 1,
            }),
            ...[
              { y: -102, label: "Quick Notes", active: false },
              { y: -64, label: "Weekly Focus", active: true },
              { y: -26, label: "Plugin Ideas", active: false },
              { y: 12, label: "Release Log", active: false },
            ].map((item) =>
              jsx(Rect, {
                x: -322,
                y: item.y,
                width: 124,
                height: 30,
                radius: 8,
                fill: () => (item.active ? pal().shell : "transparent"),
                stroke: () => (item.active ? pal().borderStrong : "transparent"),
                lineWidth: 1,
                children: [
                  jsx(Txt, {
                    x: -50,
                    offset: [-1, 0],
                    fontFamily: FONT_SANS,
                    fontSize: 11.5,
                    fontWeight: item.active ? 600 : 400,
                    fill: () => (item.active ? pal().text1 : pal().text2),
                    text: item.label,
                  }),
                ],
              }),
            ),

            // Center Markdown Document Editor
            jsx(Txt, {
              x: -214,
              y: -96,
              offset: [-1, 0],
              fontFamily: FONT_SANS,
              fontSize: 20,
              fontWeight: 600,
              fill: () => pal().text1,
              text: () => (isEn() ? "Sprint Focus" : "本周专注计划"),
            }),
            jsx(Rect, {
              x: -214,
              y: -58,
              offset: [-1, 0],
              width: 312,
              height: 10,
              radius: 5,
              fill: () => pal().shell,
            }),
            jsx(Rect, {
              x: -214,
              y: -36,
              offset: [-1, 0],
              width: 274,
              height: 10,
              radius: 5,
              fill: () => pal().shell,
            }),
            jsx(Rect, {
              x: -214,
              y: -14,
              offset: [-1, 0],
              width: 210,
              height: 10,
              radius: 5,
              fill: () => pal().shell,
            }),
            jsx(Rect, {
              x: -214,
              y: 26,
              offset: [-1, 0],
              width: 320,
              height: 76,
              radius: 10,
              fill: () => pal().codeBg,
              stroke: () => pal().border,
              lineWidth: 1,
              children: [
                jsx(Txt, {
                  x: -144,
                  y: -14,
                  offset: [-1, 0],
                  fontFamily: FONT_MONO,
                  fontSize: 11.5,
                  fill: () => pal().accentStrong,
                  text: "// Built in 30s from plugin-dev-for-ai.md",
                }),
                jsx(Txt, {
                  x: -144,
                  y: 12,
                  offset: [-1, 0],
                  fontFamily: FONT_MONO,
                  fontSize: 11.5,
                  fill: () => pal().text2,
                  text: "host.ui.render(pomodoroPanel)",
                }),
              ],
            }),
            jsx(Rect, {
              ref: editorCaret,
              x: 4,
              y: -14,
              width: 2.2,
              height: 16,
              radius: 1,
              fill: () => pal().accent,
            }),

            // Right Native Plugin Sidebar (slides in dynamically!)
            jsx(Rect, {
              ref: pluginPanel,
              x: 310,
              y: 22,
              width: 244,
              height: 320,
              fill: () => pal().surfaceAlt,
              opacity: 0,
              children: [
                jsx(Line, {
                  points: [
                    [-122, -160],
                    [-122, 160],
                  ],
                  stroke: () => pal().border,
                  lineWidth: 1,
                }),
                jsx(Txt, {
                  x: -98,
                  y: -124,
                  offset: [-1, 0],
                  fontFamily: FONT_SANS,
                  fontSize: 13.5,
                  fontWeight: 600,
                  fill: () => pal().text1,
                  text: () => (isEn() ? "Pomodoro Focus" : "番茄专注插件"),
                }),
                jsx(Txt, {
                  x: -98,
                  y: -102,
                  offset: [-1, 0],
                  fontFamily: FONT_MONO,
                  fontSize: 10,
                  fill: () => pal().accentStrong,
                  text: "UIItem · Ocean Paper",
                }),
                // Countdown Ring Track + Animated Arc
                jsx(Circle, {
                  y: -14,
                  size: 118,
                  stroke: () => pal().border,
                  lineWidth: 7,
                }),
                jsx(Circle, {
                  ref: ringArc,
                  y: -14,
                  size: 118,
                  stroke: () => pal().accent,
                  lineWidth: 7,
                  lineCap: "round",
                  startAngle: -90,
                  endAngle: 210,
                }),
                jsx(Txt, {
                  y: -18,
                  fontFamily: FONT_MONO,
                  fontSize: 22,
                  fontWeight: 700,
                  fill: () => pal().text1,
                  text: formatTimer,
                }),
                jsx(Txt, {
                  y: 10,
                  fontFamily: FONT_SANS,
                  fontSize: 10.5,
                  fill: () => pal().text3,
                  text: () => (isEn() ? "DEEP FOCUS" : "专注中"),
                }),
                // Native Plugin Buttons
                jsx(Rect, {
                  x: -44,
                  y: 78,
                  width: 96,
                  height: 32,
                  radius: 8,
                  fill: () => pal().accentStrong,
                  children: [
                    jsx(Txt, {
                      fontFamily: FONT_SANS,
                      fontSize: 12,
                      fontWeight: 600,
                      fill: "#ffffff",
                      text: () => (isEn() ? "Pause" : "暂停"),
                    }),
                  ],
                }),
                jsx(Rect, {
                  x: 56,
                  y: 78,
                  width: 76,
                  height: 32,
                  radius: 8,
                  fill: () => pal().surface,
                  stroke: () => pal().borderStrong,
                  lineWidth: 1,
                  children: [
                    jsx(Txt, {
                      fontFamily: FONT_SANS,
                      fontSize: 12,
                      fontWeight: 500,
                      fill: () => pal().text1,
                      text: () => (isEn() ? "Reset" : "重置"),
                    }),
                  ],
                }),
                // Stats row
                jsx(Rect, {
                  y: 124,
                  width: 196,
                  height: 28,
                  radius: 8,
                  fill: () => pal().shell,
                  children: [
                    jsx(Txt, {
                      fontFamily: FONT_MONO,
                      fontSize: 10.5,
                      fill: () => pal().text2,
                      text: () =>
                        isEn() ? "Today: 4 sessions · 100m" : "今日完成 4 轮 · 100 分钟",
                    }),
                  ],
                }),
              ],
            }),
          ],
        }),

        // Bottom Floating Verification Pill
        jsx(Rect, {
          ref: footerBadge,
          position: [0, 214],
          width: 468,
          height: 34,
          radius: 17,
          fill: () => pal().surface,
          stroke: () => pal().ok,
          lineWidth: 1.2,
          shadowColor: () => pal().shadow,
          shadowBlur: 18,
          shadowOffsetY: 6,
          opacity: 0,
          scale: 0.9,
          children: [
            jsx(Circle, {
              x: -214,
              size: 7,
              fill: () => pal().ok,
            }),
            jsx(Txt, {
              x: 6,
              fontFamily: FONT_SANS,
              fontSize: 12.5,
              fontWeight: 600,
              fill: () => pal().text1,
              text: () =>
                isEn()
                  ? "Zero manual docs clicking · Independent GitHub repo auto-publishes"
                  : "无需手翻文档 · 复制发给 AI 即可生成并自动上架商店",
            }),
          ],
        }),
      ],
    }),
  );

  // =========================================================================
  // CHOREOGRAPHY TIMELINE (11.8s total @ 60fps)
  // =========================================================================

  // --- ACT 1: 0.0s -> 2.6s ---
  activeStep(0);
  yield* all(
    heroBtn().opacity(1, 0.36, easeOutCubic),
    heroBtn().scale(1, 0.42, easeOutBack),
    cursor().opacity(1, 0.3, easeOutCubic),
    cursor().position([42, 8], 0.62, easeInOutCubic),
  );

  // Click micro-press + shockwave + copied flip
  yield* heroBtn().scale(0.94, 0.1, easeInCubic);
  btnCopied(true);
  yield* all(
    heroBtn().scale(1, 0.22, easeOutBack),
    ripple1().opacity(0.65, 0.05).to(0, 0.6, easeOutExpo),
    ripple1().size(280, 0.65, easeOutExpo),
    ripple2().opacity(0.42, 0.08).to(0, 0.72, easeOutExpo),
    ripple2().size(360, 0.8, easeOutExpo),
    cursor().opacity(0, 0.28, easeOutCubic),
    chain(
      waitFor(0.14),
      all(
        specDock().opacity(1, 0.32, easeOutCubic),
        specDock().scale(1, 0.38, easeOutBack),
        specDock().position([0, 62], 0.38, easeOutCubic),
      ),
    ),
  );

  yield* waitFor(0.28);

  // Dock spec URL to top-left while hero button recedes
  yield* all(
    heroBtn().opacity(0, 0.32, easeInCubic),
    heroBtn().scale(0.9, 0.32, easeInCubic),
    specDock().position([-235, -158], 0.56, easeInOutCubic),
    specDock().scale(0.84, 0.56, easeInOutCubic),
    glowPos([-155, 24], 0.6, easeInOutCubic),
  );

  // --- ACT 2: 2.6s -> 5.6s ---
  activeStep(1);
  act2().opacity(1);
  yield* all(
    aiCard().opacity(1, 0.36, easeOutCubic),
    aiCard().scale(1, 0.42, easeOutCubic),
    wireToAi().end(1, 0.44, easeInOutCubic),
    chain(
      orbToAi().opacity(1, 0.06),
      orbToAiT(1, 0.48, easeInOutCubic),
      orbToAi().opacity(0, 0.12),
    ),
  );

  // Lock attached spec pill + typewriter prompt
  const fullPrompt = isEn()
    ? "Build a Block Pomodoro plugin with titlebar countdown & sidebar stats."
    : "帮我做一个 Block 番茄钟插件，支持顶栏倒计时与右侧栏统计。";

  yield* all(
    attachedChip().opacity(1, 0.24, easeOutCubic),
    attachedChip().scale(1, 0.32, easeOutBack),
    promptCaret().opacity(1, 0.15),
    chain(
      waitFor(0.12),
      promptTxt().text(fullPrompt, 0.96, linear),
    ),
  );

  // Beam into Auto-Fetch Spec Card + laser scan rows
  yield* all(
    glowPos([260, 32], 0.55, easeInOutCubic),
    specCard().opacity(1, 0.34, easeOutCubic),
    specCard().scale(1, 0.4, easeOutCubic),
    wireToSpec().end(1, 0.38, easeInOutCubic),
    chain(
      orbToSpec().opacity(1, 0.05),
      orbToSpecT(1, 0.42, easeInOutCubic),
      orbToSpec().opacity(0, 0.12),
    ),
  );

  yield* all(
    scanBeam().opacity(0.85, 0.12).to(0.85, 0.66).to(0, 0.16),
    scanBeam().y(124, 0.94, easeInOutCubic),
    sequence(
      0.18,
      ...specRowLights.map((sig) =>
        chain(waitFor(0.06), function* () {
          sig(true);
        }()),
      ),
    ),
  );

  yield* waitFor(0.24);

  // Transition Act 2 -> Act 3
  yield* all(
    act2().opacity(0, 0.34, easeInCubic),
    specDock().opacity(0, 0.3, easeInCubic),
    glowPos([180, 10], 0.45, easeInOutCubic),
  );

  // --- ACT 3: 5.6s -> 8.6s ---
  activeStep(2);
  act3().opacity(1);
  yield* all(
    sequence(
      0.12,
      ...fileRefs.map((ref) =>
        all(
          ref().opacity(1, 0.32, easeOutCubic),
          ref().x(-255, 0.42, easeOutCubic),
        ),
      ),
    ),
    chain(
      waitFor(0.18),
      all(
        checkTerminal().opacity(1, 0.36, easeOutCubic),
        checkTerminal().scale(1, 0.42, easeOutCubic),
      ),
    ),
  );

  // Circuit wires fire + packets race into sandbox checker
  yield* all(
    ...fileWires.map((w) => w().end(1, 0.42, easeInOutCubic)),
    ...fileOrbs.map((orb, idx) =>
      chain(
        waitFor(idx * 0.07),
        orb().opacity(1, 0.05),
        fileOrbTs[idx](1, 0.48, easeInOutCubic),
        orb().opacity(0, 0.12),
      ),
    ),
    chain(
      waitFor(0.24),
      checkBar().width(356, 0.72, easeInOutCubic),
    ),
  );

  // Staggered sandbox PASS rows
  yield* sequence(
    0.16,
    ...checkRows.map((row, idx) =>
      all(
        row().opacity(1, 0.24, easeOutCubic),
        row().y(-10 + idx * 46, 0.24, easeOutCubic),
      ),
    ),
  );
  checkPassed(true);
  yield* waitFor(0.56);

  // Transition Act 3 -> Act 4
  yield* all(
    act3().opacity(0, 0.34, easeInCubic),
    glowPos([160, -10], 0.45, easeInOutCubic),
  );

  // --- ACT 4: 8.6s -> 11.8s ---
  activeStep(3);
  act4().opacity(1);
  yield* all(
    appWindow().scale(1, 0.44, easeOutCubic),
    glowSize(480, 0.5, easeOutCubic),
  );

  // Titlebar plugin chip + Right native sidebar slide-in
  yield* all(
    titlebarPlugin().opacity(1, 0.28, easeOutCubic),
    titlebarPlugin().scale(1, 0.36, easeOutBack),
    titlebarRipple().opacity(0.7, 0.05).to(0, 0.55, easeOutExpo),
    titlebarRipple().size(96, 0.55, easeOutExpo),
    pluginPanel().opacity(1, 0.36, easeOutCubic),
    pluginPanel().x(274, 0.46, easeOutCubic),
    chain(
      waitFor(0.22),
      all(
        footerBadge().opacity(1, 0.32, easeOutCubic),
        footerBadge().scale(1, 0.36, easeOutBack),
      ),
    ),
  );

  // Live countdown tick & progress ring sweep + blinking caret
  yield* all(
    ringArc().endAngle(255, 1.5, easeInOutCubic),
    timerSec(1497, 1.5, linear),
    chain(
      editorCaret().opacity(0.15, 0.36),
      editorCaret().opacity(1, 0.36),
      editorCaret().opacity(0.15, 0.36),
      editorCaret().opacity(1, 0.36),
    ),
  );

  yield* waitFor(0.32);

  // Clean fade out for seamless loop back to Act 1
  yield* all(
    act4().opacity(0, 0.36, easeInCubic),
    glowAlpha(0.2, 0.36, easeInCubic),
  );
});

export interface AiWorkflowRuntime {
  canvas: HTMLCanvasElement;
  playFromStart: () => void;
  pause: () => void;
  resume: () => void;
  seekSeconds: (seconds: number, keepPlaying?: boolean) => void;
  setTheme: (dark: boolean) => void;
  setLocale: (en: boolean) => void;
  deactivate: () => void;
  activate: () => void;
  destroy: () => void;
}

export interface CreateAiWorkflowRuntimeOptions {
  dark: boolean;
  en: boolean;
  onFrame: (frame: number, duration: number, seconds: number) => void;
}

export function createAiWorkflowRuntime(
  options: CreateAiWorkflowRuntimeOptions,
): AiWorkflowRuntime {
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  const size = new Vector2(SCENE_WIDTH, SCENE_HEIGHT);

  const sceneDesc = aiWorkflowScene;
  const fullScene: FullSceneDescription = {
    ...(sceneDesc as unknown as FullSceneDescription),
    name: "ai-workflow",
    onReplaced: new ValueDispatcher(sceneDesc as unknown as FullSceneDescription),
  };

  let currentDark = options.dark;
  let currentEn = options.en;

  const project = bootstrap(
    "block-ai-workflow",
    { core: "3.17.2", two: "3.17.2", ui: null, vitePlugin: null },
    [DefaultPlugin()],
    {
      name: "block-ai-workflow",
      scenes: [fullScene],
      variables: { dark: currentDark, en: currentEn },
    },
    new MetaFile("project", false),
    new MetaFile("settings", false),
  );

  const stage = new Stage();
  stage.configure({
    size,
    resolutionScale: dpr,
    colorSpace: "srgb",
  });

  const player = new Player(
    project,
    {
      size,
      resolutionScale: dpr,
      fps: SCENE_FPS,
    },
    {
      loop: true,
      paused: true,
      muted: true,
    },
    0,
  );

  let destroyed = false;
  let hasRecalculated = false;
  let pendingSeek: { seconds: number; keepPlaying: boolean } | null = null;

  const unsubLog = project.logger.onLogged.subscribe((payload) => {
    if (payload.level === "error") {
      console.error("[MotionCanvas]", payload.message, payload.stack ?? "");
    }
  });

  const unsubRecalc = player.onRecalculated.subscribe(() => {
    if (destroyed) return;
    hasRecalculated = true;
    if (pendingSeek) {
      const target = pendingSeek;
      pendingSeek = null;
      player.requestSeek(Math.round(target.seconds * SCENE_FPS));
      player.togglePlayback(target.keepPlaying);
      player.requestRender();
    }
  });

  const unsubRender = player.onRender.subscribe(async () => {
    if (destroyed) return;
    await stage.render(
      player.playback.currentScene,
      player.playback.previousScene,
    );
  });

  const unsubFrame = player.onFrameChanged.subscribe((frame) => {
    if (destroyed) return;
    const duration = Math.max(1, player.playback.duration);
    options.onFrame(frame, duration, frame / SCENE_FPS);
  });

  // Start deactivated until the popover opens so idle CPU stays at 0%
  player.deactivate();

  return {
    canvas: stage.finalBuffer,
    activate() {
      if (destroyed) return;
      player.activate();
    },
    deactivate() {
      if (destroyed) return;
      player.togglePlayback(false);
      player.deactivate();
    },
    playFromStart() {
      if (destroyed) return;
      pendingSeek = null;
      player.activate();
      player.requestReset();
      player.togglePlayback(true);
    },
    pause() {
      if (destroyed) return;
      player.togglePlayback(false);
    },
    resume() {
      if (destroyed) return;
      player.activate();
      player.togglePlayback(true);
    },
    seekSeconds(seconds: number, keepPlaying = true) {
      if (destroyed) return;
      player.activate();
      if (!hasRecalculated) {
        pendingSeek = { seconds, keepPlaying };
        player.togglePlayback(false);
        return;
      }
      player.requestSeek(Math.round(seconds * SCENE_FPS));
      player.togglePlayback(keepPlaying);
      player.requestRender();
    },
    setTheme(dark: boolean) {
      if (destroyed || currentDark === dark) return;
      currentDark = dark;
      player.setVariables({ dark: currentDark, en: currentEn });
      player.requestRender();
    },
    setLocale(en: boolean) {
      if (destroyed || currentEn === en) return;
      currentEn = en;
      hasRecalculated = false;
      player.setVariables({ dark: currentDark, en: currentEn });
      player.playback.reload();
      player.requestReset();
      player.requestRender();
    },
    destroy() {
      destroyed = true;
      unsubLog();
      unsubRecalc();
      unsubRender();
      unsubFrame();
      player.deactivate();
    },
  };
}
