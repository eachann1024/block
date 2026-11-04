(function () {
  if (window.__THINKLY_GPUI_MOUNTED__) return;
  window.__THINKLY_GPUI_MOUNTED__ = true;

  // Prevent automatic focus() inside GPUI's ImeMirror from scrolling the host page
  const origFocus = HTMLElement.prototype.focus;
  HTMLElement.prototype.focus = function (options) {
    if (!options || typeof options !== "object") {
      return origFocus.call(this, { preventScroll: true });
    }
    if (options.preventScroll === undefined) {
      return origFocus.call(this, Object.assign({}, options, { preventScroll: true }));
    }
    return origFocus.call(this, options);
  };

  let gpuiCanvas = null;

  // Capture GPUI's WebGPU/WebGL2 canvas the instant WebWindow::prepare_canvas appends it to body
  const origAppendChild = Node.prototype.appendChild;
  Node.prototype.appendChild = function (child) {
    const res = origAppendChild.call(this, child);
    if (
      child &&
      child.nodeName === "CANVAS" &&
      child.id !== "editor-canvas" &&
      (this === document.body || (this.id && this.id === "editor-stage"))
    ) {
      gpuiCanvas = child;
    }
    return res;
  };

  function resolveBundleUrls() {
    try {
      if (window.parent && window.parent !== window) {
        const pHref = window.parent.location.href || "";
        const m = /^(https?:\/\/[^/]+\/design\/[^/]+\/)/.exec(pHref);
        if (m) {
          return {
            jsUrl: m[1] + "project/thinkly_fast_web.js",
            wasmUrl: m[1] + "project/thinkly_fast_web_bg.wasm",
          };
        }
      }
    } catch (_) {}
    const base = new URL(".", location.href).href;
    if (base.endsWith("/editor/")) {
      return {
        jsUrl: base + "thinkly_fast_web.js",
        wasmUrl: base + "thinkly_fast_web_bg.wasm",
      };
    }
    return {
      jsUrl: base + "editor/thinkly_fast_web.js",
      wasmUrl: base + "editor/thinkly_fast_web_bg.wasm",
    };
  }

  function styleAndAttachCanvas(stage) {
    if (!stage) return false;
    if (!gpuiCanvas) {
      gpuiCanvas =
        document.querySelector("canvas[data-raw-handle]") ||
        stage.querySelector("canvas:not(#editor-canvas)") ||
        document.querySelector("body > canvas");
    }
    if (!gpuiCanvas) return false;

    const computedPos = getComputedStyle(stage).position;
    if (computedPos === "static") {
      stage.style.position = "relative";
    }
    stage.style.overflow = "hidden";

    gpuiCanvas.dataset.gpuiCanvas = "1";
    gpuiCanvas.style.position = "absolute";
    gpuiCanvas.style.inset = "0";
    gpuiCanvas.style.width = "100%";
    gpuiCanvas.style.height = "100%";
    gpuiCanvas.style.display = "block";
    gpuiCanvas.style.outline = "none";
    gpuiCanvas.style.zIndex = "2";

    if (gpuiCanvas.parentElement !== stage) {
      origAppendChild.call(stage, gpuiCanvas);
    }

    const loader = stage.querySelector("#editor-loading, [data-editor-loading]");
    if (loader) {
      loader.style.opacity = "0";
      loader.style.pointerEvents = "none";
    }
    document.documentElement.setAttribute("data-editor-ready", "1");
    return true;
  }

  function waitForStage() {
    return new Promise((resolve) => {
      const check = () => {
        const stage = document.getElementById("editor-stage");
        if (stage && stage.clientWidth > 0 && stage.clientHeight > 0) {
          resolve(stage);
          return;
        }
        requestAnimationFrame(check);
      };
      if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", check, { once: true });
      } else {
        requestAnimationFrame(check);
      }
    });
  }

  async function boot() {
    const stage = await waitForStage();
    const { jsUrl, wasmUrl } = resolveBundleUrls();
    try {
      const mod = await import(jsUrl);
      if (typeof mod.default === "function") {
        await mod.default(wasmUrl);
      }
    } catch (err) {
      try {
        const fallbackJs = new URL("./thinkly_fast_web.js", location.href).href;
        const fallbackWasm = new URL("./thinkly_fast_web_bg.wasm", location.href).href;
        const mod2 = await import(fallbackJs);
        if (typeof mod2.default === "function") {
          await mod2.default(fallbackWasm);
        }
      } catch (err2) {
        console.error("[Thinkly Fast GPUI] Failed to boot WASM editor:", err, err2);
        return;
      }
    }

    styleAndAttachCanvas(document.getElementById("editor-stage") || stage);

    const observer = new MutationObserver(() => {
      const currentStage = document.getElementById("editor-stage");
      if (currentStage && gpuiCanvas && gpuiCanvas.parentElement !== currentStage) {
        styleAndAttachCanvas(currentStage);
      }
    });
    observer.observe(document.body, { childList: true, subtree: true });
  }

  boot();
})();
