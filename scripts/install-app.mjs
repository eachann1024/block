import { spawnSync } from "node:child_process";
import { cpSync, lstatSync, renameSync } from "node:fs";
import { dirname, join } from "node:path";
import { assertInstallTarget, assertWritable, installPath, installedExecutable, remove, sameVolumeTemp, verifySignature } from "./app-bundle.mjs";
import { appPids } from "./stop-app.mjs";

const readyWaitMs = 8000;

export function prepareInstall(bundle) {
  const present = assertInstallTarget(installPath);
  assertWritable(dirname(installPath));
  const staging = sameVolumeTemp(installPath);
  const staged = join(staging, "Thinkly Fast.app");
  try {
    cpSync(bundle, staged, { recursive: true });
    verifySignature(staged);
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

export async function openInstalled(previous) {
  const opened = spawnSync("open", [installPath], { encoding: "utf8", env: detachedLaunchEnv() });
  if (opened.status !== 0) throw new Error((opened.stderr || opened.stdout || "open failed").trim());
  const deadline = Date.now() + readyWaitMs;
  while (Date.now() < deadline) {
    const running = appPids().filter((pid) => !previous.includes(pid));
    if (running.length) return running;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`open returned but no new process has executable ${installedExecutable}`);
}
