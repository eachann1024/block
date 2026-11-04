import { spawnSync } from "node:child_process";
import { chmodSync, cpSync, lstatSync, mkdirSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { appName, assertSystemLibraries, binaryPath, installPath, logIssue, plist, remove, sign, workspaceTemp } from "./app-bundle.mjs";
import { commitInstall, openInstalled, prepareInstall } from "./install-app.mjs";
import { matchingPids, stopRunningApp } from "./stop-app.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const lockPath = join(root, "dist/.build.lock");

if (import.meta.main) {
  try {
    const app = await buildApp();
    console.log(app);
    console.log(installPath);
  } catch (error) {
    const detail = error instanceof Error ? error.stack || error.message : String(error);
    logIssue("ERROR", detail);
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  }
}

export async function buildApp() {
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
    job.release = acquireLock();
    job.staging = workspaceTemp();
    const executable = binaryPath(root);
    assertSystemLibraries(executable);
    const bundle = join(job.staging, `${appName}.app`);
    const macos = join(bundle, "Contents/MacOS");
    mkdirSync(macos, { recursive: true });
    writeFileSync(join(bundle, "Contents/Info.plist"), plist());
    const resources = join(bundle, "Contents/Resources");
    mkdirSync(resources, { recursive: true });
    cpSync(join(root, "assets/app-icon/AppIcon.icns"), join(resources, "AppIcon.icns"));
    const packaged = join(macos, "thinkly-fast");
    const copied = spawnSync("cp", [executable, packaged]);
    if (copied.status !== 0) throw new Error("failed to copy the release binary");
    chmodSync(packaged, 0o755);
    assertSystemLibraries(packaged);
    sign(bundle);
    job.plan = prepareInstall(bundle);
    const distApp = publishDist(bundle);
    const previous = matchingPids(executable);
    await stopRunningApp(executable);
    commitInstall(job.plan);
    await openInstalled(previous);
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

function acquireLock() {
  mkdirSync(join(root, "dist"), { recursive: true });
  try {
    writeFileSync(lockPath, `${process.pid}\n`, { flag: "wx" });
  } catch (error) {
    if (error?.code === "EEXIST") throw new Error(`another build holds ${lockPath}`);
    throw error;
  }
  return () => remove(lockPath);
}

function cleanup(job) {
  if (job.staging) remove(job.staging);
  if (job.plan && !job.plan.keep) remove(job.plan.staging);
  job.release?.();
  job.staging = "";
  job.plan = null;
  job.release = null;
}
