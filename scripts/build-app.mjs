import { chmodSync, copyFileSync, cpSync, lstatSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { appName, assertSystemLibraries, binaryPath, installPath, logIssue, plist, remove, sign, workspaceTemp } from "./app-bundle.mjs";
import { commitInstall, prepareInstall } from "./install-app.mjs";
import { buildDictation } from "./build-dictation.mjs";

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

export async function buildApp({ install = true, profile = process.env.BLOCK_PROFILE || (install ? "fast" : "release") } = {}) {
  if (process.platform !== "darwin") throw new Error("macOS is required");
  const job = { staging: "", plan: null, release: null };
  const onSignal = () => {
    if (job.plan?.keep) console.error(`install interrupted; staging kept at ${job.plan.staging}`);
    cleanup(job);
    process.exit(1);
  };
  process.once("SIGINT", onSignal);
  process.once("SIGTERM", onSignal);
  try {
    job.release = await acquireLock();
    job.staging = workspaceTemp();
    const executable = binaryPath(root, profile);
    if (profile === "release") assertSystemLibraries(executable);
    const bundle = join(job.staging, `${appName}.app`);
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
    buildDictation(join(macos, "block-dictation"));
    sign(bundle);
    if (install) job.plan = prepareInstall(bundle);
    const distApp = publishDist(bundle);
    if (install) commitInstall(job.plan);
    return distApp;
  } finally {
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    cleanup(job);
  }
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

async function acquireLock() {
  mkdirSync(join(root, "dist"), { recursive: true });
  const deadline = Date.now() + 90_000;
  while (true) {
    try {
      writeFileSync(lockPath, `${process.pid}\n`, { flag: "wx" });
      return () => remove(lockPath);
    } catch (error) {
      if (error?.code !== "EEXIST") throw error;
      let holder = 0;
      try { holder = Number.parseInt(readFileSync(lockPath, "utf8").trim(), 10); } catch {}
      if (!isProcessAlive(holder)) {
        remove(lockPath);
        continue;
      }
      if (Date.now() >= deadline) {
        throw new Error(`[BUILD FAILED] 等待其他并行构建锁 (${lockPath}, PID ${holder}) 超时`);
      }
      await new Promise((resolve) => setTimeout(resolve, 250));
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
