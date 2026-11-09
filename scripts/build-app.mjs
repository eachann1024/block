import { chmodSync, copyFileSync, cpSync, existsSync, lstatSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { isAbsolute, join } from "node:path";
import { fileURLToPath } from "node:url";
import { appName, assertSystemLibraries, binaryPath, installPath, logIssue, plist, remove, sign, workspaceTemp } from "./app-bundle.mjs";
import { commitInstall, prepareInstall } from "./install-app.mjs";
import { buildDictation } from "./build-dictation.mjs";
import { createBuildTimer, elapsedMs, resolveCargoTimings } from "./build-timing.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const lockPath = join(root, "dist/.build.lock");

if (import.meta.main) {
  try {
    const install = !process.argv.includes("--no-install") && process.env.BLOCK_NO_INSTALL !== "1";
    const app = await buildApp({ install });
    console.log(app);
    if (install) console.log(installPath);
  } catch (error) {
    const detail = error instanceof Error ? error.stack || error.message : String(error);
    logIssue("ERROR", detail);
    process.exitCode = 1;
  }
}

export async function buildApp({
  install = true,
  profile = process.env.BLOCK_PROFILE || (install ? "fast" : "release"),
  cargoTimings,
} = {}) {
  if (process.platform !== "darwin") throw new Error("macOS is required");
  const timings = resolveCargoTimings({ explicit: cargoTimings });
  const timer = createBuildTimer({
    dest: join(root, "dist", "build-timing.json"),
    profile,
    install: Boolean(install),
    cargoTimings: timings,
  });
  const job = { staging: "", plan: null, release: null };
  const outcome = { error: null };
  const lockDetail = { contended: false, sleep_ms: 0 };
  const onSignal = (signal) => {
    if (job.plan?.keep) console.error(`install interrupted; staging kept at ${job.plan.staging}`);
    outcome.error ??= new Error(`signal ${signal}`);
    try {
      timer.annotate(lockDetail, "lock_wait");
      timer.finish({ ok: false, interrupted: true, error: outcome.error, ...cargoTimingFields(timings) });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`BUILD_TIMING_WRITE_FAILED ${message}\n`);
    }
    cleanup(job);
    process.exit(1);
  };
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);
  try {
    try {
      job.release = await timer.measure("lock_wait", () => acquireLock(lockDetail));
    } finally {
      timer.annotate(lockDetail, "lock_wait");
    }
    job.staging = timer.measure("workspace_temp", () => workspaceTemp());
    let executable;
    try {
      executable = timer.measure("cargo", () => binaryPath(root, profile, { timings }));
    } finally {
      if (timings) timer.annotate({ timings: true }, "cargo");
    }
    if (profile === "release") timer.measure("system_libraries", () => assertSystemLibraries(executable));
    const bundle = join(job.staging, `${appName}.app`);
    timer.measure("package", () => assembleBundle(bundle, executable, profile));
    timer.measure("helper", () => buildDictation(join(bundle, "Contents/MacOS", "block-dictation")));
    timer.measure("sign", () => sign(bundle, { development: profile === "fast" }));
    if (install) job.plan = timer.measure("install_prepare", () => prepareInstall(bundle));
    const distApp = timer.measure("dist_publish", () => publishDist(bundle));
    if (install) timer.measure("install_commit", () => commitInstall(job.plan));
    return distApp;
  } catch (error) {
    outcome.error ??= error;
    throw error;
  } finally {
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    try {
      timer.finish({ ok: outcome.error == null, error: outcome.error, ...cargoTimingFields(timings) });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`BUILD_TIMING_WRITE_FAILED ${message}\n`);
    }
    cleanup(job);
  }
}

function assembleBundle(bundle, executable, profile) {
  const macos = join(bundle, "Contents/MacOS");
  mkdirSync(macos, { recursive: true });
  writeFileSync(join(bundle, "Contents/Info.plist"), plist());
  const resources = join(bundle, "Contents/Resources");
  mkdirSync(resources, { recursive: true });
  cpSync(join(root, "assets/app-icon/AppIcon.icns"), join(resources, "AppIcon.icns"));
  const notices = join(root, "THIRD_PARTY_NOTICES.md");
  if (profile === "release" || pathExists(notices)) {
    copyFileSync(notices, join(resources, "THIRD_PARTY_NOTICES.md"));
  } else {
    logIssue("WARN", "THIRD_PARTY_NOTICES.md 缺失，已跳过本地打包；发布构建仍需该文件。");
  }
  const packaged = join(macos, "block");
  copyFileSync(executable, packaged);
  chmodSync(packaged, 0o755);
}

function cargoTimingFields(enabled) {
  if (!enabled) return { cargoTimingsDir: null, cargoTimingsReport: null };
  const configured = process.env.CARGO_TARGET_DIR;
  const targetDir = configured ? (isAbsolute(configured) ? configured : join(root, configured)) : join(root, "target");
  const cargoTimingsDir = join(targetDir, "cargo-timings");
  const report = join(cargoTimingsDir, "cargo-timing.html");
  return { cargoTimingsDir, cargoTimingsReport: existsSync(report) ? report : null };
}

function publishDist(bundle) {
  const dist = join(root, "dist");
  mkdirSync(dist, { recursive: true });
  const finalApp = join(dist, `${appName}.app`);
  const staged = join(dist, `.${appName}.app.staging-${process.pid}`);
  const previous = join(dist, `.${appName}.app.previous-${process.pid}`);
  cpSync(bundle, staged, { recursive: true });
  let aside = false;
  try {
    if (pathExists(finalApp)) {
      renameSync(finalApp, previous);
      aside = true;
    }
    renameSync(staged, finalApp);
  } catch (error) {
    if (aside && !pathExists(finalApp)) renameSync(previous, finalApp);
    remove(staged);
    throw error;
  }
  if (aside) remove(previous);
  return finalApp;
}

function pathExists(path) {
  try {
    lstatSync(path);
    return true;
  } catch (error) {
    if (error?.code === "ENOENT") return false;
    throw error;
  }
}

function isProcessAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === "EPERM";
  }
}

async function acquireLock(detail) {
  mkdirSync(join(root, "dist"), { recursive: true });
  const deadline = Date.now() + 90_000;
  let sleepNs = 0n;
  const publish = () => {
    detail.sleep_ms = elapsedMs(0n, sleepNs);
  };
  while (true) {
    try {
      writeFileSync(lockPath, `${process.pid}\n`, { flag: "wx" });
      publish();
      return () => remove(lockPath);
    } catch (error) {
      if (error?.code !== "EEXIST") {
        publish();
        throw error;
      }
      let holder = 0;
      try { holder = Number.parseInt(readFileSync(lockPath, "utf8").trim(), 10); } catch {}
      if (!isProcessAlive(holder)) {
        remove(lockPath);
        continue;
      }
      if (Date.now() >= deadline) {
        publish();
        throw new Error(`[BUILD FAILED] 等待其他并行构建锁 (${lockPath}, PID ${holder}) 超时`);
      }
      detail.contended = true;
      const sleepStart = process.hrtime.bigint();
      await new Promise((resolve) => setTimeout(resolve, 250));
      sleepNs += process.hrtime.bigint() - sleepStart;
      publish();
    }
  }
}

function cleanup(job) {
  if (job.staging) remove(job.staging);
  if (job.plan && !job.plan.keep) remove(job.plan.staging);
  job.release?.();
  job.staging = "";
  job.plan = null;
  job.release = null;
}
