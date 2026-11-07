import { spawn, spawnSync } from "node:child_process";
import { cpSync, lstatSync, renameSync } from "node:fs";
import { dirname, join } from "node:path";
import { assertInstallTarget, assertWritable, bundleId, installPath, installedExecutable, legacyInstallPath, remove, sameVolumeTemp, verifySignature } from "./app-bundle.mjs";
import { appPids } from "./stop-app.mjs";

const readyWaitMs = 8000;

export function prepareInstall(bundle) {
  const present = assertInstallTarget(installPath);
  assertWritable(dirname(installPath));
  const staging = sameVolumeTemp(installPath);
  const staged = join(staging, "Block.app");
  try {
    cpSync(bundle, staged, { recursive: true });
  } catch (error) {
    remove(staging);
    throw error;
  }
  return { staging, staged, backup: join(staging, "previous.app"), present, keep: false };
}

export function commitInstall(plan) {
  if (plan.present) {
    plan.keep = true;
    renameSync(installPath, plan.backup);
  }
  try {
    renameSync(plan.staged, installPath);
  } catch (error) {
    if (plan.present) restore(plan, error);
    throw error;
  }
  try {
    verifySignature(installPath);
  } catch (error) {
    if (plan.present) restore(plan, error);
    else remove(installPath);
    throw error;
  }
  if (plan.present) remove(plan.backup);
  if (pathExists(legacyInstallPath)) remove(legacyInstallPath);
  plan.keep = false;
  const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";
  spawnSync(lsregister, ["-f", installPath], { stdio: "ignore" });
}

function restore(plan, error) {
  try {
    if (pathExists(installPath)) renameSync(installPath, plan.staged);
    renameSync(plan.backup, installPath);
    plan.keep = false;
  } catch (restoreError) {
    throw new Error(`${error.message}; restore failed (${restoreError.message}); previous app kept at ${plan.backup}`);
  }
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

function detachedLaunchEnv() {
  const env = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (
      value === undefined ||
      key.startsWith("HC_") ||
      key.startsWith("HERDR_") ||
      key.startsWith("CLAUDE") ||
      key === "__CFBundleIdentifier"
    ) {
      continue;
    }
    env[key] = value;
  }
  return env;
}

function backgroundLaunch() {
  return process.env.BLOCK_BACKGROUND_LAUNCH === "1";
}

// Detached launch of the installed binary. An immediate exit is not success; the caller
// still has to observe that pid via the same installed-executable check as `open`.
function spawnInstalledBackground() {
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawn(installedExecutable, [], {
        detached: true,
        stdio: "ignore",
        env: detachedLaunchEnv(),
      });
    } catch (error) {
      reject(error instanceof Error ? error : new Error(String(error)));
      return;
    }
    const launch = { pid: child.pid, exited: false, error: null };
    let settled = false;
    const finish = (error) => {
      if (settled) return;
      settled = true;
      if (typeof child.pid === "number") child.unref();
      if (error) reject(error);
      else resolve(launch);
    };
    child.once("error", (error) => {
      launch.exited = true;
      launch.error = error instanceof Error ? error : new Error(String(error));
      finish(launch.error);
    });
    child.once("exit", (code, signal) => {
      launch.exited = true;
      launch.error ??= new Error(`installed executable exited before it was ready (${signal ?? code})`);
      if (!settled) finish(launch.error);
    });
    const started = () => {
      if (launch.exited) {
        finish(launch.error ?? new Error("installed executable exited before it was ready"));
        return;
      }
      launch.pid = child.pid;
      if (typeof launch.pid !== "number") {
        finish(new Error("installed executable did not start"));
        return;
      }
      finish(null);
    };
    if (typeof child.pid === "number") queueMicrotask(started);
    else child.once("spawn", () => queueMicrotask(started));
  });
}

export async function openInstalled(previous) {
  const background = backgroundLaunch();
  let launch = null;
  if (background) {
    launch = await spawnInstalledBackground();
  } else {
    const opened = spawnSync("open", [installPath], { encoding: "utf8", env: detachedLaunchEnv() });
    if (opened.status !== 0) throw new Error((opened.stderr || opened.stdout || "open failed").trim());
  }
  const deadline = Date.now() + readyWaitMs;
  let running = [];
  while (Date.now() < deadline) {
    if (launch?.exited) throw launch.error ?? new Error("installed executable exited before it was ready");
    running = appPids().filter((pid) => !previous.includes(pid));
    if (launch) {
      if (running.includes(launch.pid)) break;
      running = [];
    } else if (running.length) break;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  if (launch?.exited) throw launch.error ?? new Error("installed executable exited before it was ready");
  if (!running.length) throw new Error(`open returned but no new process has executable ${installedExecutable}`);
  if (!background) {
    // `open` from a background build leaves the app hidden; activate unhides its windows.
    const activated = spawnSync("osascript", ["-e", `tell application id "${bundleId}" to activate`], {
      encoding: "utf8",
      timeout: 4000,
    });
    if (activated.status !== 0) throw new Error((activated.stderr || activated.stdout || "activate failed").trim());
  }
  return running;
}
