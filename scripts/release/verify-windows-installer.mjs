// Runs the packaged Windows setup on a real Windows machine and fails unless
// Block's own setup window installs it. Gate for the release workflow:
//   node scripts/release/verify-windows-installer.mjs <setup.exe> <mygo-automation.exe> <screenshot-dir>
//
// 1. Interactive run with a custom directory holding spaces: the setup window
//    must come up, show that directory, install on "Install", and reach "Open".
// 2. Updater run (/UPDATE): setup must install unattended and exit by itself.
// 3. Silent uninstall, so the uninstaller keeps working too.
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
const INSTALL_DIR = "C:\\Block Smoke\\Custom Dir";
const APP_EXE = join(INSTALL_DIR, "block.exe");
const UNINSTALL_EXE = join(INSTALL_DIR, "uninstall.exe");
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

function assertInstalled(label) {
  assert.ok(existsSync(APP_EXE), `${label}: ${APP_EXE} is missing`);
  assert.ok(existsSync(UNINSTALL_EXE), `${label}: ${UNINSTALL_EXE} is missing`);
  const reg = spawnSync(
    "reg",
    ["query", "HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall", "/s", "/d", "/f", INSTALL_DIR],
    { encoding: "utf8" },
  );
  assert.equal(reg.status, 0, `${label}: no uninstall entry points at ${INSTALL_DIR}\n${reg.stdout}${reg.stderr}`);
}

async function interactiveInstall() {
  console.log("[1/3] interactive install through the setup window");
  const run = startSetup([`/D=${INSTALL_DIR}`], { ...process.env, MYGO_AUTOMATION: SOCKET });
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
    assertInstalled("interactive install");

    automation(["press", "key=setup-close"]);
    const code = await exitCodeWithin(run, "setup after closing its window", 30_000);
    assert.equal(code, 0, `setup exited ${code} after an interactive install`);
  } finally {
    automation(["close"], { check: false });
    run.child.kill();
  }
}

async function updaterInstall() {
  console.log("[2/3] unattended install as started by the in-app updater");
  const run = startSetup(["/UPDATE", `/D=${INSTALL_DIR}`]);
  const code = await exitCodeWithin(run, "setup /UPDATE", 180_000);
  assert.equal(code, 0, `setup /UPDATE exited ${code}`);
  assertInstalled("updater install");
  // Reopening Block goes through the desktop shell, which a CI session may not
  // run; report it without failing the release on that alone.
  let reopened = false;
  for (let i = 0; i < 20 && !reopened; i += 1) {
    reopened = blockProcessPaths().some((p) => p.toLowerCase() === APP_EXE.toLowerCase());
    if (!reopened) await sleep(500);
  }
  console.log(reopened ? "Block was reopened after the update" : "::warning::Block was not reopened after the update on this runner");
  killBlock();
  await sleep(1000);
}

async function silentUninstall() {
  console.log("[3/3] silent uninstall");
  // _?= keeps the uninstaller from re-launching itself from %TEMP%, so its exit means it is done.
  const res = spawnSync(UNINSTALL_EXE, ["/S", `_?=${INSTALL_DIR}`], { windowsVerbatimArguments: true, timeout: 120_000 });
  assert.equal(res.status, 0, `uninstall exited ${res.status}`);
  assert.ok(!existsSync(APP_EXE), `${APP_EXE} survived uninstall`);
}

killBlock();
try {
  await interactiveInstall();
  await updaterInstall();
  await silentUninstall();
  console.log("Windows setup verified.");
} finally {
  killBlock();
  rmSync("C:\\Block Smoke", { recursive: true, force: true });
}
