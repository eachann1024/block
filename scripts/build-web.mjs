import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, constants as zlibConstants, gzipSync } from "node:zlib";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const webDir = join(root, "web");
const outDir = resolve(root, "../public/fast/editor");
const fontsOut = join(outDir, "fonts");

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
  <title>Yu</title>
  <style>
    @font-face {
      font-family: "IBM Plex Sans";
      src: url("./fonts/IBMPlexSans-Regular.ttf") format("truetype");
      font-weight: 400;
      font-style: normal;
      font-display: swap;
    }
    @font-face {
      font-family: "IBM Plex Sans";
      src: url("./fonts/IBMPlexSans-SemiBold.ttf") format("truetype");
      font-weight: 600;
      font-style: normal;
      font-display: swap;
    }
    @font-face {
      font-family: "JetBrains Mono";
      src: url("./fonts/JetBrainsMono-Regular.ttf") format("truetype");
      font-weight: 400;
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
    <div id="editor-loading">正在加载 Yu 编辑器…</div>
  </div>
  <script type="module">
    import init from "./thinkly_fast_web.js";
    await init("./thinkly_fast_web_bg.wasm");
  </script>
</body>
</html>
`;

export function buildWeb() {
  mkdirSync(fontsOut, { recursive: true });

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

  const rawWasm = join(webDir, "target/wasm32-unknown-unknown/release/thinkly_fast_web.wasm");
  run("wasm-bindgen", [
    "--target",
    "web",
    "--no-typescript",
    "--out-dir",
    outDir,
    "--out-name",
    "thinkly_fast_web",
    rawWasm,
  ]);

  const bgWasm = join(outDir, "thinkly_fast_web_bg.wasm");
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

  cpSync(join(webDir, "fonts"), fontsOut, { recursive: true });
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

if (import.meta.main) {
  buildWeb();
}
