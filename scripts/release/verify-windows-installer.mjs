// Runs the packaged Windows setup on a real Windows machine and fails unless
// both per-user (default, seamless updates) and machine-wide (/ALLUSERS /
// legacy Program Files) flows work end-to-end. Gate for the release workflow:
//   node scripts/release/verify-windows-installer.mjs <setup.exe> <mygo-automation.exe> <screenshot-dir>
//
// 1. Interactive per-user install with a custom directory holding spaces.
// 2. Seamless silent updater run (/S /UPDATE) on that per-user install.
// 3. Silent per-user uninstall.
// 4. Machine-wide install (/S /ALLUSERS) + seamless silent in-place update
//    preserving HKLM scope without duplicating to HKCU.
// 5. Silent machine-wide uninstall.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

if (process.platform !== "win32") {
  throw new Error("verify-windows-installer.mjs only runs on Windows");
}
const [setupArg, automationArg, shotsArg] = process.argv.slice(2);
assert.ok(setupArg && automationArg && shotsArg, "usage: verify-windows-installer.mjs <setup.exe> <mygo-automation.exe> <screenshot-dir>");
const setupExe = resolve(setupArg);
const automationExe = resolve(automationArg);
const shotsDir = resolve(shotsArg);
assert.ok(existsSync(setupExe), `setup not found: ${setupExe}`);
assert.ok(existsSync(automationExe), `mygo-automation not found: ${automationExe}`);
assert.ok(!/\s/.test(setupExe), `setup path must not contain spaces: ${setupExe}`);
mkdirSync(shotsDir, { recursive: true });

// Spaces on purpose: NSIS reads /D= verbatim, the easiest thing to get wrong.
const USER_DIR = "C:\\Block Smoke\\Custom Dir";
const USER_EXE = join(USER_DIR, "block.exe");
const USER_UNINSTALL = join(USER_DIR, "uninstall.exe");

const MACHINE_DIR = "C:\\Block Smoke\\Machine Dir";
const MACHINE_EXE = join(MACHINE_DIR, "block.exe");
const MACHINE_UNINSTALL = join(MACHINE_DIR, "uninstall.exe");

const SOCKET = join(tmpdir(), `block-setup-smoke-${process.pid}.sock`);
const SESSION_ENV = { ...process.env, MYGO_AUTOMATION_SESSION: `block-setup-smoke-${process.pid}` };

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function automation(args, { check = true } = {}) {
  const res = spawnSync(automationExe, args, { env: SESSION_ENV, encoding: "utf8", timeout: 30_000 });
  const out = `${res.stdout || ""}${res.stderr || ""}`;
  if (check && res.status !== 0) {
    throw new Error(`mygo-automation ${args.join(" ")} exited ${res.status}: ${out}`);
  }
  return { ok: res.status === 0, out };
}

// /D= must reach setup unquoted, so arguments are passed verbatim.
function startSetup(args, env = process.env) {
  const child = spawn(setupExe, args, { env, windowsVerbatimArguments: true, stdio: "ignore" });
  const exited = new Promise((res, rej) => {
    child.on("error", rej);
    child.on("exit", (code) => res(code));
  });
  return { child, exited };
}

async function waitFor(label, timeoutMs, probe) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await probe();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out after ${timeoutMs}ms waiting for ${label}`);
    await sleep(500);
  }
}

async function exitCodeWithin(run, label, timeoutMs) {
  const timeout = sleep(timeoutMs).then(() => "timeout");
  const code = await Promise.race([run.exited, timeout]);
  if (code === "timeout") {
    run.child.kill();
    throw new Error(`${label} did not exit within ${timeoutMs}ms`);
  }
  return code;
}

function blockProcessPaths() {
  const res = spawnSync(
    "powershell",
    ["-NoProfile", "-Command", "Get-Process block -ErrorAction SilentlyContinue | ForEach-Object { $_.Path }"],
    { encoding: "utf8" },
  );
  return (res.stdout || "").split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
}

function killBlock() {
  spawnSync("taskkill", ["/F", "/IM", "block.exe"], { stdio: "ignore" });
}

function hasUninstallEntry(hive, installDir) {
  const args = [
    "query",
    `${hive}\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall`,
    "/s",
    "/d",
    "/f",
    installDir,
  ];
  // The NSIS setup is a 32-bit process, so its HKLM writes land in the 32-bit view.
  if (hive === "HKLM") {
    args.push("/reg:32");
  }
  const reg = spawnSync("reg", args, { encoding: "utf8" });
  return { ok: reg.status === 0, out: `${reg.stdout || ""}${reg.stderr || ""}` };
}

function assertInstalled(label, { dir, exe, uninstall, hive }) {
  assert.ok(existsSync(exe), `${label}: ${exe} is missing`);
  assert.ok(existsSync(uninstall), `${label}: ${uninstall} is missing`);
  const reg = hasUninstallEntry(hive, dir);
  assert.ok(reg.ok, `${label}: no ${hive} uninstall entry points at ${dir}\n${reg.out}`);
}

async function interactiveUserInstall() {
  console.log("[1/5] interactive per-user install through the setup window");
  const run = startSetup([`/D=${USER_DIR}`], { ...process.env, MYGO_AUTOMATION: SOCKET });
  try {
    await waitFor("the setup window", 90_000, () => automation(["open", "--socket", SOCKET], { check: false }).ok);
    const ready = await waitFor("the Install button", 30_000, () => {
      const snap = automation(["snapshot"], { check: false });
      return snap.ok && snap.out.includes("key=setup-install") ? snap.out : null;
    });
    console.log(ready);
    assert.ok(ready.includes("Custom Dir"), "setup window does not show the directory passed with /D=");
    assert.ok(ready.includes("key=setup-change-dir"), "setup window has no Change Location button");
    automation(["screenshot", join(shotsDir, "1-ready.png")], { check: false });

    automation(["press", "key=setup-install"]);
    await sleep(700);
    automation(["screenshot", join(shotsDir, "2-installing.png")], { check: false });
    const done = await waitFor("the install to finish", 120_000, () => {
      const snap = automation(["snapshot"], { check: false });
      if (!snap.ok) return null;
      if (snap.out.includes("key=setup-retry")) throw new Error(`setup window reported a failed install:\n${snap.out}`);
      return snap.out.includes("key=setup-open") ? snap.out : null;
    });
    console.log(done);
    automation(["screenshot", join(shotsDir, "3-done.png")], { check: false });
    assertInstalled("interactive per-user install", {
      dir: USER_DIR,
      exe: USER_EXE,
      uninstall: USER_UNINSTALL,
      hive: "HKCU",
    });

    automation(["press", "key=setup-close"]);
    const code = await exitCodeWithin(run, "setup after closing its window", 30_000);
    assert.equal(code, 0, `setup exited ${code} after an interactive install`);
  } finally {
    automation(["close"], { check: false });
    run.child.kill();
  }
}

async function seamlessUserUpdate() {
  console.log("[2/5] seamless silent per-user update (/S /UPDATE)");
  const run = startSetup(["/S", "/UPDATE", `/D=${USER_DIR}`]);
  const code = await exitCodeWithin(run, "setup /S /UPDATE (per-user)", 180_000);
  assert.equal(code, 0, `setup /S /UPDATE (per-user) exited ${code}`);
  assertInstalled("per-user updater install", {
    dir: USER_DIR,
    exe: USER_EXE,
    uninstall: USER_UNINSTALL,
    hive: "HKCU",
  });
  let reopened = false;
  for (let i = 0; i < 20 && !reopened; i += 1) {
    reopened = blockProcessPaths().some((p) => p.toLowerCase() === USER_EXE.toLowerCase());
    if (!reopened) await sleep(500);
  }
  console.log(reopened ? "Block was reopened after the per-user update" : "::warning::Block was not reopened after the per-user update on this runner");
  killBlock();
  await sleep(1000);
}

async function silentUserUninstall() {
  console.log("[3/5] silent per-user uninstall");
  // _?= keeps the uninstaller from re-launching itself from %TEMP%, so its exit means it is done.
  const res = spawnSync(USER_UNINSTALL, ["/S", `_?=${USER_DIR}`], { windowsVerbatimArguments: true, timeout: 120_000 });
  assert.equal(res.status, 0, `per-user uninstall exited ${res.status}`);
  assert.ok(!existsSync(USER_EXE), `${USER_EXE} survived uninstall`);
  assert.ok(!hasUninstallEntry("HKCU", USER_DIR).ok, `HKCU uninstall entry survived for ${USER_DIR}`);
}

async function machineInstallAndUpdate() {
  console.log("[4/5] machine-wide install (/S /ALLUSERS) and silent in-place update");
  const installRun = startSetup(["/S", "/ALLUSERS", `/D=${MACHINE_DIR}`]);
  const installCode = await exitCodeWithin(installRun, "setup /S /ALLUSERS", 180_000);
  assert.equal(installCode, 0, `setup /S /ALLUSERS exited ${installCode}`);
  assertInstalled("machine-wide install", {
    dir: MACHINE_DIR,
    exe: MACHINE_EXE,
    uninstall: MACHINE_UNINSTALL,
    hive: "HKLM",
  });

  // Update without passing /ALLUSERS to confirm existing HKLM installs are
  // auto-detected and updated in place without creating a duplicate HKCU entry.
  const updateRun = startSetup(["/S", "/UPDATE", `/D=${MACHINE_DIR}`]);
  const updateCode = await exitCodeWithin(updateRun, "setup /S /UPDATE (machine)", 180_000);
  assert.equal(updateCode, 0, `setup /S /UPDATE (machine) exited ${updateCode}`);
  assertInstalled("machine-wide updater install", {
    dir: MACHINE_DIR,
    exe: MACHINE_EXE,
    uninstall: MACHINE_UNINSTALL,
    hive: "HKLM",
  });
  assert.ok(!hasUninstallEntry("HKCU", MACHINE_DIR).ok, `machine-wide update unexpectedly wrote HKCU uninstall entry for ${MACHINE_DIR}`);
  killBlock();
  await sleep(1000);
}

async function silentMachineUninstall() {
  console.log("[5/5] silent machine-wide uninstall");
  const res = spawnSync(MACHINE_UNINSTALL, ["/S", `_?=${MACHINE_DIR}`], { windowsVerbatimArguments: true, timeout: 120_000 });
  assert.equal(res.status, 0, `machine-wide uninstall exited ${res.status}`);
  assert.ok(!existsSync(MACHINE_EXE), `${MACHINE_EXE} survived uninstall`);
  assert.ok(!hasUninstallEntry("HKLM", MACHINE_DIR).ok, `HKLM uninstall entry survived for ${MACHINE_DIR}`);
}

killBlock();
try {
  await interactiveUserInstall();
  await seamlessUserUpdate();
  await silentUserUninstall();
  await machineInstallAndUpdate();
  await silentMachineUninstall();
  console.log("Windows setup verified (per-user & machine-wide).");
} finally {
  killBlock();
  rmSync("C:\\Block Smoke", { recursive: true, force: true });
}
