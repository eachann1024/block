import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { brotliCompressSync, constants as zlibConstants, gzipSync } from "node:zlib";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const webDir = join(root, "web");
const outDir = resolve(root, "../public/fast/editor");
const FONTSOURCE_VER = "5.3.0";

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, { stdio: "inherit", ...opts });
  if (res.status !== 0) {
    throw new Error(`${cmd} ${args.join(" ")} exited with status ${res.status}`);
  }
}

function fmtMiB(bytes) {
  return `${(bytes / (1024 * 1024)).toFixed(2)} MiB (${bytes.toLocaleString()} B)`;
}

const INDEX_HTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Block</title>
  <link rel="icon" type="image/png" sizes="32x32" href="./favicon-32.png?v=7" />
  <link rel="apple-touch-icon" href="./apple-touch-icon.png?v=7" />
  <link rel="modulepreload" href="./block_web.js?v=9" />
  <link rel="preload" href="./block_web_bg.wasm?v=9" as="fetch" type="application/wasm" crossorigin />
  <style>
    @font-face {
      font-family: "IBM Plex Sans";
      src:
        url("https://cdn.jsdelivr.net/fontsource/fonts/ibm-plex-sans@${FONTSOURCE_VER}/latin-400-normal.ttf") format("truetype"),
        url("https://registry.npmmirror.com/@fontsource/ibm-plex-sans/${FONTSOURCE_VER}/files/files/ibm-plex-sans-latin-400-normal.woff2") format("woff2");
      font-weight: 400;
      font-style: normal;
      font-display: swap;
    }
    @font-face {
      font-family: "IBM Plex Sans";
      src:
        url("https://cdn.jsdelivr.net/fontsource/fonts/ibm-plex-sans@${FONTSOURCE_VER}/latin-600-normal.ttf") format("truetype"),
        url("https://registry.npmmirror.com/@fontsource/ibm-plex-sans/${FONTSOURCE_VER}/files/files/ibm-plex-sans-latin-600-normal.woff2") format("woff2");
      font-weight: 600;
      font-style: normal;
      font-display: swap;
    }
    @font-face {
      font-family: "JetBrains Mono";
      src:
        url("https://cdn.jsdelivr.net/fontsource/fonts/jetbrains-mono@${FONTSOURCE_VER}/latin-400-normal.ttf") format("truetype"),
        url("https://registry.npmmirror.com/@fontsource/jetbrains-mono/${FONTSOURCE_VER}/files/files/jetbrains-mono-latin-400-normal.woff2") format("woff2");
      font-weight: 400;
      font-style: normal;
      font-display: swap;
    }
    @font-face {
      font-family: "JetBrains Mono";
      src:
        url("https://cdn.jsdelivr.net/fontsource/fonts/jetbrains-mono@${FONTSOURCE_VER}/latin-500-normal.ttf") format("truetype"),
        url("https://registry.npmmirror.com/@fontsource/jetbrains-mono/${FONTSOURCE_VER}/files/files/jetbrains-mono-latin-500-normal.woff2") format("woff2");
      font-weight: 500;
      font-style: normal;
      font-display: swap;
    }
    *, *::before, *::after { box-sizing: border-box; }
    html, body {
      margin: 0;
      padding: 0;
      width: 100%;
      height: 100%;
      background: #fbfaf7;
      color: #1f1e1d;
      font-family: "IBM Plex Sans", -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif;
      overflow: hidden;
      display: grid;
      place-items: center;
    }
    #editor-stage {
      position: relative;
      width: min(100vw, calc(100vh * 16 / 10));
      height: min(100vh, calc(100vw * 10 / 16));
      aspect-ratio: 16 / 10;
      background: #fbfaf7;
      overflow: hidden;
    }
    #editor-stage > canvas {
      position: absolute !important;
      inset: 0 !important;
      width: 100% !important;
      height: 100% !important;
      display: block !important;
      outline: none !important;
      z-index: 2 !important;
    }
    #editor-loading {
      position: absolute;
      inset: 0;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 14px;
      color: #898781;
      pointer-events: none;
      transition: opacity 160ms ease;
    }
    html[data-editor-ready="1"] #editor-loading {
      opacity: 0;
    }
  </style>
</head>
<body>
  <div id="editor-stage">
    <div id="editor-loading">正在加载 Block 编辑器…</div>
  </div>
  <script type="module">
    import init from "./block_web.js?v=9";
    await init("./block_web_bg.wasm?v=9");
  </script>
</body>
</html>
`;

export function buildWeb() {
  mkdirSync(outDir, { recursive: true });

  run(
    "cargo",
    ["build", "--manifest-path", join(webDir, "Cargo.toml"), "--target", "wasm32-unknown-unknown", "--release"],
    {
      cwd: webDir,
      env: {
        ...process.env,
        RUSTFLAGS: '--cfg getrandom_backend="wasm_js"',
      },
    },
  );

  const rawWasm = join(webDir, "target/wasm32-unknown-unknown/release/block_web.wasm");
  run("wasm-bindgen", [
    "--target",
    "web",
    "--no-typescript",
    "--out-dir",
    outDir,
    "--out-name",
    "block_web",
    rawWasm,
  ]);

  const bgWasm = join(outDir, "block_web_bg.wasm");
  run("wasm-opt", [
    "-Oz",
    "--strip-debug",
    "--strip-producers",
    "--enable-bulk-memory",
    "--enable-nontrapping-float-to-int",
    "--enable-sign-ext",
    "--enable-mutable-globals",
    bgWasm,
    "-o",
    bgWasm,
  ]);

  for (const name of ["favicon-32.png", "apple-touch-icon.png"]) {
    cpSync(join(root, "assets/app-icon", name), join(outDir, name));
  }
  writeFileSync(join(outDir, "index.html"), INDEX_HTML, "utf8");

  const wasmBytes = readFileSync(bgWasm);
  const gzipBytes = gzipSync(wasmBytes, { level: 9 });
  const brotliBytes = brotliCompressSync(wasmBytes, {
    params: { [zlibConstants.BROTLI_PARAM_QUALITY]: 11 },
  });

  console.log(`output: ${outDir}`);
  console.log(`wasm (raw):    ${fmtMiB(wasmBytes.byteLength)}`);
  console.log(`wasm (gzip):   ${fmtMiB(gzipBytes.byteLength)}`);
  console.log(`wasm (brotli): ${fmtMiB(brotliBytes.byteLength)}`);
}

if (import.meta.main || (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)) {
  buildWeb();
}
