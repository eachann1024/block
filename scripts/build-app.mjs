import { chmodSync, copyFileSync, cpSync, lstatSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  appName,
  assertSystemLibraries,
  goBuild,
  installPath,
  logIssue,
  normalizeArch,
  normalizePlatform,
  plist,
  remove,
  sign,
  workspaceTemp,
} from "./app-bundle.mjs";
import { commitInstall, installLinux, installWindows, prepareInstall } from "./install-app.mjs";
import { createBuildTimer, elapsedMs } from "./build-timing.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const lockPath = join(root, "dist/.build.lock");

function argValue(flag, argv = process.argv) {
  const eq = argv.find((a) => a.startsWith(`${flag}=`));
  if (eq) return eq.slice(flag.length + 1);
  const idx = argv.indexOf(flag);
  if (idx >= 0 && idx + 1 < argv.length) return argv[idx + 1];
  return null;
}

if (import.meta.main) {
  try {
    const rawPlatform = argValue("--platform") || process.env.BLOCK_PLATFORM || process.platform;
    const rawArch = argValue("--arch") || process.env.BLOCK_ARCH || process.arch;
    const platform = normalizePlatform(rawPlatform);
    const arch = normalizeArch(rawArch);
    const hostPlatform = normalizePlatform(process.platform);
    const hostArch = normalizeArch(process.arch);
    const noInstall = process.argv.includes("--no-install") || process.env.BLOCK_NO_INSTALL === "1";
    const install = !noInstall && platform === hostPlatform && arch === hostArch;
    const profile = argValue("--profile") || process.env.BLOCK_PROFILE || (install ? "fast" : "release");
    const useMygo = process.argv.includes("--mygo") || process.env.BLOCK_USE_MYGO === "1";

    const result = await buildApp({ install, profile, platform, arch, useMygo });
    console.log(result.artifact);
    if (result.installedPath) console.log(result.installedPath);
  } catch (error) {
    const detail = error instanceof Error ? error.stack || error.message : String(error);
    logIssue("ERROR", detail);
    process.exitCode = 1;
  }
}

export async function buildApp({
  install = true,
  profile = process.env.BLOCK_PROFILE || (install ? "fast" : "release"),
  platform = normalizePlatform(process.platform),
  arch = normalizeArch(process.arch),
  useMygo = process.env.BLOCK_USE_MYGO === "1",
} = {}) {
  const targetPlatform = normalizePlatform(platform);
  const targetArch = normalizeArch(arch);
  const hostPlatform = normalizePlatform(process.platform);
  const canInstall = Boolean(install) && targetPlatform === hostPlatform;

  const timer = createBuildTimer({
    dest: join(root, "dist", "build-timing.json"),
    profile,
    install: canInstall,
    platform: targetPlatform,
    arch: targetArch,
  });
  const job = { staging: "", plan: null, release: null };
  const outcome = { error: null };
  const lockDetail = { contended: false, sleep_ms: 0 };

  const onSignal = (signal) => {
    if (job.plan?.keep) console.error(`install interrupted; staging kept at ${job.plan.staging}`);
    outcome.error ??= new Error(`signal ${signal}`);
    try {
      timer.annotate(lockDetail, "lock_wait");
      timer.finish({ ok: false, interrupted: true, error: outcome.error });
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

    if (targetPlatform === "darwin") {
      if (hostPlatform !== "darwin") {
        throw new Error("macOS app bundling and codesign require macOS");
      }
      job.staging = timer.measure("workspace_temp", () => workspaceTemp());
      const stageBin = join(job.staging, "block");
      timer.measure("go_build", () =>
        goBuild({ root, goos: "darwin", goarch: targetArch, outFile: stageBin, profile, useMygo }),
      );
      if (profile === "release") {
        timer.measure("system_libraries", () => assertSystemLibraries(stageBin));
      }
      const distBin = join(root, "dist", "block");
      timer.measure("dist_binary", () => publishFile(stageBin, distBin, 0o755));

      const bundle = join(job.staging, `${appName}.app`);
      timer.measure("package", () => assembleMacBundle(bundle, stageBin, profile));
      timer.measure("sign", () => sign(bundle, { development: profile !== "release" }));

      if (canInstall) {
        job.plan = timer.measure("install_prepare", () => prepareInstall(bundle));
      }

      const distArchApp = join(root, "dist", `darwin-${targetArch}`, `${appName}.app`);
      const distRootApp = join(root, "dist", `${appName}.app`);
      timer.measure("dist_publish", () => {
        publishDir(bundle, distArchApp);
        publishDir(bundle, distRootApp);
      });

      let installedPath = null;
      if (canInstall) {
        installedPath = timer.measure("install_commit", () => commitInstall(job.plan));
      }
      return { artifact: distRootApp, archArtifact: distArchApp, binary: distBin, installedPath };
    }

    if (targetPlatform === "windows") {
      const outExe = join(root, "dist", `windows-${targetArch}`, "block.exe");
      timer.measure("go_build", () =>
        goBuild({ root, goos: "windows", goarch: targetArch, outFile: outExe, profile, useMygo }),
      );
      let installedPath = null;
      if (canInstall) {
        installedPath = timer.measure("install_commit", () => installWindows(outExe));
      }
      return { artifact: outExe, binary: outExe, installedPath };
    }

    // linux
    const outBin = join(root, "dist", `linux-${targetArch}`, "block");
    timer.measure("go_build", () =>
      goBuild({ root, goos: "linux", goarch: targetArch, outFile: outBin, profile, useMygo }),
    );
    chmodSync(outBin, 0o755);
    let installedPath = null;
    if (canInstall) {
      installedPath = timer.measure("install_commit", () => installLinux(outBin));
    }
    return { artifact: outBin, binary: outBin, installedPath };
  } catch (error) {
    outcome.error ??= error;
    throw error;
  } finally {
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    try {
      timer.finish({ ok: outcome.error == null, error: outcome.error });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`BUILD_TIMING_WRITE_FAILED ${message}\n`);
    }
    cleanup(job);
  }
}

function assembleMacBundle(bundle, executable, profile) {
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

function publishFile(src, dest, mode = 0o755) {
  const dir = join(dest, "..");
  mkdirSync(dir, { recursive: true });
  const staged = join(dir, `.block.staging-${process.pid}`);
  copyFileSync(src, staged);
  chmodSync(staged, mode);
  renameSync(staged, dest);
  return dest;
}

function publishDir(srcDir, finalDir) {
  const parent = join(finalDir, "..");
  mkdirSync(parent, { recursive: true });
  const staged = join(parent, `.${appName}.app.staging-${process.pid}`);
  const previous = join(parent, `.${appName}.app.previous-${process.pid}`);
  remove(staged);
  cpSync(srcDir, staged, { recursive: true });
  let aside = false;
  try {
    if (pathExists(finalDir)) {
      renameSync(finalDir, previous);
      aside = true;
    }
    renameSync(staged, finalDir);
  } catch (error) {
    if (aside && !pathExists(finalDir)) renameSync(previous, finalDir);
    remove(staged);
    throw error;
  }
  if (aside) remove(previous);
  return finalDir;
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
