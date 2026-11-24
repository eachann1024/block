// Runs the packaged Windows setup on a real Windows machine and fails unless
// interactive UI, folder picker, icon resources, security boundaries, seamless
// updates, and uninstall-while-running protection all work end-to-end.
// Gate for the release workflow:
//   node scripts/release/verify-windows-installer.mjs <setup.exe> <mygo-automation.exe> <screenshot-dir>
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

if (process.platform !== "win32") {
  throw new Error("verify-windows-installer.mjs only runs on Windows");
}
const [setupArg, automationArg, shotsArg] = process.argv.slice(2);
assert.ok(
  setupArg && automationArg && shotsArg,
  "usage: verify-windows-installer.mjs <setup.exe> <mygo-automation.exe> <screenshot-dir>",
);
const setupExe = resolve(setupArg);
const automationExe = resolve(automationArg);
const shotsDir = resolve(shotsArg);
assert.ok(existsSync(setupExe), `setup not found: ${setupExe}`);
assert.ok(existsSync(automationExe), `mygo-automation not found: ${automationExe}`);
assert.ok(!/\s/.test(setupExe), `setup path must not contain spaces: ${setupExe}`);
mkdirSync(shotsDir, { recursive: true });

// Spaces on purpose: NSIS reads /D= verbatim, the easiest thing to get wrong.
const SMOKE_ROOT = "C:\\Block Smoke";
const USER_INITIAL_DIR = join(SMOKE_ROOT, "Initial Dir");
const USER_PICK_BASE = join(SMOKE_ROOT, "Picked Dir");
const USER_DIR = join(USER_PICK_BASE, "Block");
const USER_EXE = join(USER_DIR, "block.exe");
const USER_UNINSTALL = join(USER_DIR, "uninstall.exe");

const PROGRAM_FILES = process.env.ProgramW6432 || process.env.ProgramFiles || "C:\\Program Files";
const MACHINE_SMOKE_ROOT = join(PROGRAM_FILES, "Block Smoke");
const MACHINE_DIR = join(MACHINE_SMOKE_ROOT, "Machine Dir");
const MACHINE_EXE = join(MACHINE_DIR, "block.exe");
const MACHINE_UNINSTALL = join(MACHINE_DIR, "uninstall.exe");

const UNSAFE_MACHINE_DIR = "C:\\Users\\Public\\Block Smoke Unsafe";
const DEFAULT_USER_DIR = join(
  process.env.LOCALAPPDATA || join(process.env.USERPROFILE || "C:\\Users\\Default", "AppData", "Local"),
  "Programs",
  "Block",
);

const SOCKET = join(tmpdir(), `block-setup-smoke-${process.pid}.sock`);
const SESSION_ENV = { ...process.env, MYGO_AUTOMATION_SESSION: `block-setup-smoke-${process.pid}` };

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function runPowershell(script, args = [], { timeout = 30_000 } = {}) {
  const env = { ...process.env };
  args.forEach((val, idx) => {
    env[`BLOCK_PS_ARG${idx}`] = String(val);
  });
  const res = spawnSync(
    "powershell",
    ["-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script],
    { encoding: "utf8", timeout, env },
  );
  return {
    status: res.status,
    stdout: res.stdout || "",
    stderr: res.stderr || "",
    error: res.error,
  };
}

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

function readLastUpdateError() {
  const res = runPowershell(
    "(Get-ItemProperty -Path 'HKCU:\\Software\\Block' -Name 'LastUpdateError' -ErrorAction SilentlyContinue).LastUpdateError",
  );
  return res.stdout.trim();
}

function describeDirAcl(dir) {
  const res = spawnSync("icacls", [dir], { encoding: "utf8" });
  return `${res.stdout || ""}${res.stderr || ""}`.trim();
}

function blockProcessPaths() {
  const res = runPowershell(
    "Get-Process block -ErrorAction SilentlyContinue | ForEach-Object { $_.Path }",
  );
  return res.stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
}

async function killBlockAndWait() {
  spawnSync("taskkill", ["/F", "/IM", "block.exe"], { stdio: "ignore" });
  await waitFor("block.exe processes to exit", 15_000, () => {
    if (blockProcessPaths().length === 0) return true;
    spawnSync("taskkill", ["/F", "/IM", "block.exe"], { stdio: "ignore" });
    return false;
  });
  // Give Windows a beat to release file handles after process termination.
  await sleep(500);
}

async function waitBlockReopened(expectedExe, label) {
  const reopened = await waitFor(`${label} to reopen ${expectedExe}`, 25_000, () =>
    blockProcessPaths().some((p) => p.toLowerCase() === expectedExe.toLowerCase()),
  );
  assert.ok(reopened, `${label}: ${expectedExe} did not reopen`);
  console.log(`Confirmed reopened process: ${expectedExe}`);
}

function readUninstallMetadata(hive, installDir) {
  const script = `
    $ErrorActionPreference = 'Stop'
    $hiveName = $env:BLOCK_PS_ARG0
    $targetDir = $env:BLOCK_PS_ARG1.TrimEnd('\\').ToLowerInvariant()
    $base = if ($hiveName -eq 'HKLM') {
      [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::LocalMachine, [Microsoft.Win32.RegistryView]::Registry32)
    } else {
      [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryView]::Default)
    }
    $uninst = $base.OpenSubKey('Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall')
    if (-not $uninst) { exit 0 }
    foreach ($name in $uninst.GetSubKeyNames()) {
      if ($name -ne 'Block' -and -not $name.StartsWith('Block-')) { continue }
      $sub = $uninst.OpenSubKey($name)
      if (-not $sub) { continue }
      $loc = [string]$sub.GetValue('InstallLocation', '')
      if ($loc.TrimEnd('\\').ToLowerInvariant() -eq $targetDir) {
        [pscustomobject]@{
          KeyName         = $name
          InstallLocation = $loc
          DisplayIcon     = [string]$sub.GetValue('DisplayIcon', '')
          ShortcutPath    = [string]$sub.GetValue('BlockShortcutPath', '')
          UninstallString = [string]$sub.GetValue('UninstallString', '')
          Scope           = [string]$sub.GetValue('BlockInstallScope', '')
        } | ConvertTo-Json -Compress
        exit 0
      }
    }
  `;
  const res = runPowershell(script, [hive, installDir]);
  assert.equal(res.status, 0, `registry query failed (${hive}): ${res.stderr || res.stdout}`);
  const text = res.stdout.trim();
  return text ? JSON.parse(text) : null;
}

function verifyExecutableAndShortcutIcon(label, exePath, shortcutPath) {
  const script = `
    $ErrorActionPreference = 'Stop'
    $exe = $env:BLOCK_PS_ARG0
    $lnk = $env:BLOCK_PS_ARG1
    Add-Type -AssemblyName System.Drawing
    Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class BlockIconProbe {
  [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
  public static extern uint ExtractIconEx(string lpszFile, int nIconIndex, IntPtr[] phiconLarge, IntPtr[] phiconSmall, uint nIcons);
}
"@
    $count = [BlockIconProbe]::ExtractIconEx($exe, -1, $null, $null, 0)
    if ($count -lt 1) {
      throw "Executable has no embedded PE icon resource (ExtractIconEx count=$count): $exe"
    }
    $ico = [System.Drawing.Icon]::ExtractAssociatedIcon($exe)
    if ($null -eq $ico -or $ico.Width -le 0 -or $ico.Height -le 0) {
      throw "ExtractAssociatedIcon failed for $exe"
    }
    $ico.Dispose()
    if (-not (Test-Path -LiteralPath $lnk)) {
      throw "Start Menu shortcut missing: $lnk"
    }
    $ws = New-Object -ComObject WScript.Shell
    $sc = $ws.CreateShortcut($lnk)
    [pscustomobject]@{
      IconCount    = [int]$count
      TargetPath   = [string]$sc.TargetPath
      IconLocation = [string]$sc.IconLocation
    } | ConvertTo-Json -Compress
  `;
  const res = runPowershell(script, [exePath, shortcutPath]);
  assert.equal(res.status, 0, `${label}: icon/shortcut check failed:\n${res.stderr || res.stdout}`);
  const info = JSON.parse(res.stdout.trim());
  assert.ok(info.IconCount >= 1, `${label}: expected >=1 embedded PE icon in ${exePath}`);
  assert.equal(
    info.TargetPath.toLowerCase(),
    exePath.toLowerCase(),
    `${label}: shortcut TargetPath mismatch (${info.TargetPath} != ${exePath})`,
  );
  assert.equal(
    info.IconLocation.toLowerCase(),
    `${exePath.toLowerCase()},0`,
    `${label}: shortcut IconLocation mismatch (${info.IconLocation} != ${exePath},0)`,
  );
}

function assertInstalled(label, { dir, exe, uninstall, hive }) {
  assert.ok(existsSync(exe), `${label}: ${exe} is missing`);
  assert.ok(statSync(exe).size > 1_000_000, `${label}: ${exe} looks truncated (${statSync(exe).size} bytes)`);
  assert.ok(existsSync(uninstall), `${label}: ${uninstall} is missing`);
  assert.ok(!existsSync(`${exe}.new`), `${label}: leftover ${exe}.new found`);
  assert.ok(!existsSync(`${exe}.old`), `${label}: leftover ${exe}.old found`);

  const meta = readUninstallMetadata(hive, dir);
  assert.ok(meta, `${label}: no ${hive} uninstall entry points at ${dir}`);
  assert.equal(
    meta.DisplayIcon.toLowerCase(),
    `${exe.toLowerCase()},0`,
    `${label}: DisplayIcon mismatch (${meta.DisplayIcon})`,
  );
  assert.ok(meta.ShortcutPath, `${label}: BlockShortcutPath missing from ${hive} uninstall entry`);
  verifyExecutableAndShortcutIcon(label, exe, meta.ShortcutPath);
}

function driveFolderPickerDialog(targetFolder) {
  const script = `
    $ErrorActionPreference = 'Stop'
    $target = $env:BLOCK_PS_ARG0
    Add-Type -AssemblyName UIAutomationClient
    Add-Type -AssemblyName UIAutomationTypes

    function Find-PickerDialog {
      $root = [System.Windows.Automation.AutomationElement]::RootElement
      $top = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
      for ($i = 0; $i -lt $top.Count; $i++) {
        $w = $top.Item($i)
        if ($w.Current.Name -eq 'Choose Install Location' -or $w.Current.Name -eq '选择安装位置') {
          return $w
        }
        $kids = $w.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
        for ($j = 0; $j -lt $kids.Count; $j++) {
          $k = $kids.Item($j)
          if ($k.Current.Name -eq 'Choose Install Location' -or $k.Current.Name -eq '选择安装位置') {
            return $k
          }
        }
      }
      return $null
    }

    $deadline = (Get-Date).AddSeconds(20)
    $dlg = $null
    while ((Get-Date) -lt $deadline) {
      $dlg = Find-PickerDialog
      if ($null -ne $dlg) { break }
      Start-Sleep -Milliseconds 250
    }
    if ($null -eq $dlg) {
      throw 'Timed out waiting for Windows folder picker dialog ("Choose Install Location")'
    }

    $editCond = New-Object System.Windows.Automation.PropertyCondition(
      [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
      [System.Windows.Automation.ControlType]::Edit
    )
    $edits = $dlg.FindAll([System.Windows.Automation.TreeScope]::Descendants, $editCond)
    $folderEdit = $null
    for ($i = 0; $i -lt $edits.Count; $i++) {
      $e = $edits.Item($i)
      $aid = [string]$e.Current.AutomationId
      $nm = [string]$e.Current.Name
      if ($aid -eq '1152' -or $aid -eq '1148' -or $nm -match 'Folder|文件夹') {
        $folderEdit = $e
        break
      }
    }
    if ($null -eq $folderEdit) {
      $dump = @()
      for ($i = 0; $i -lt $edits.Count; $i++) {
        $e = $edits.Item($i)
        $dump += "Edit(id=$($e.Current.AutomationId), name=$($e.Current.Name))"
      }
      throw ("Folder edit box not found in picker dialog. Edits: " + ($dump -join '; '))
    }

    $vp = $folderEdit.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
    $vp.SetValue($target)
    Start-Sleep -Milliseconds 200

    $btnCond = New-Object System.Windows.Automation.PropertyCondition(
      [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
      [System.Windows.Automation.ControlType]::Button
    )
    for ($attempt = 0; $attempt -lt 3; $attempt++) {
      $stillOpen = Find-PickerDialog
      if ($null -eq $stillOpen) { exit 0 }
      $btns = $stillOpen.FindAll([System.Windows.Automation.TreeScope]::Descendants, $btnCond)
      $selectBtn = $null
      for ($i = 0; $i -lt $btns.Count; $i++) {
        $b = $btns.Item($i)
        $aid = [string]$b.Current.AutomationId
        $nm = [string]$b.Current.Name
        if ($aid -eq '1' -or $nm -eq 'Select' -or $nm -eq '选择') {
          $selectBtn = $b
          break
        }
      }
      if ($null -eq $selectBtn) {
        throw 'Select button not found in folder picker dialog'
      }
      $ip = $selectBtn.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
      $ip.Invoke()
      Start-Sleep -Milliseconds 500
    }

    if ($null -ne (Find-PickerDialog)) {
      throw 'Folder picker dialog did not close after invoking Select'
    }
  `;
  const res = runPowershell(script, [targetFolder], { timeout: 35_000 });
  assert.equal(res.status, 0, `folder picker interaction failed:\n${res.stderr || res.stdout}`);
}

async function interactiveUserInstallWithChangeDir() {
  console.log("[1/6] interactive per-user install + Change Location dialog");
  mkdirSync(USER_PICK_BASE, { recursive: true });
  const run = startSetup([`/D=${USER_INITIAL_DIR}`], { ...process.env, MYGO_AUTOMATION: SOCKET });
  try {
    await waitFor("the setup window", 90_000, () => automation(["open", "--socket", SOCKET], { check: false }).ok);
    const ready = await waitFor("the Install button", 30_000, () => {
      const snap = automation(["snapshot"], { check: false });
      return snap.ok && snap.out.includes("key=setup-install") ? snap.out : null;
    });
    console.log(ready);
    assert.ok(ready.includes("Initial Dir"), "setup window does not show the initial directory passed with /D=");
    assert.ok(ready.includes("key=setup-change-dir"), "setup window has no Change Location button");
    automation(["screenshot", join(shotsDir, "1-ready.png")], { check: false });

    // Trigger the real native folder picker dialog and select USER_PICK_BASE.
    automation(["press", "key=setup-change-dir"]);
    driveFolderPickerDialog(USER_PICK_BASE);
    const updated = await waitFor("selected folder to update in setup window", 15_000, () => {
      const snap = automation(["snapshot"], { check: false });
      return snap.ok && snap.out.includes("Picked Dir") && snap.out.includes("Block") ? snap.out : null;
    });
    console.log("After Change Location:\n" + updated);

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

async function seamlessUserUpdateAndRunningUninstallGuard() {
  console.log("[2/6] seamless silent per-user update (/S /UPDATE) from older binary + mandatory relaunch");
  // Simulate an older installed binary so we confirm atomic replacement really happened.
  writeFileSync(USER_EXE, "old-block-user-placeholder");
  assert.ok(statSync(USER_EXE).size < 1024);

  const run = startSetup(["/S", "/UPDATE", `/D=${USER_DIR}`]);
  const code = await exitCodeWithin(run, "setup /S /UPDATE (per-user)", 180_000);
  assert.equal(code, 0, `setup /S /UPDATE (per-user) exited ${code}`);
  assertInstalled("per-user updater install", {
    dir: USER_DIR,
    exe: USER_EXE,
    uninstall: USER_UNINSTALL,
    hive: "HKCU",
  });
  await waitBlockReopened(USER_EXE, "per-user /UPDATE");

  console.log("[3/6] uninstall while Block is running must refuse to orphan registry/uninstaller");
  const blockedUninst = spawnSync(USER_UNINSTALL, ["/S", `_?=${USER_DIR}`], {
    windowsVerbatimArguments: true,
    timeout: 60_000,
  });
  assert.notEqual(blockedUninst.status, 0, "silent uninstall unexpectedly succeeded while block.exe was running");
  assert.ok(existsSync(USER_EXE), "block.exe unexpectedly disappeared while running");
  assert.ok(existsSync(USER_UNINSTALL), "uninstall.exe was deleted even though block.exe remained locked");
  assert.ok(readUninstallMetadata("HKCU", USER_DIR), "HKCU uninstall entry was deleted while block.exe remained locked");

  await killBlockAndWait();
  const res = spawnSync(USER_UNINSTALL, ["/S", `_?=${USER_DIR}`], {
    windowsVerbatimArguments: true,
    timeout: 120_000,
  });
  assert.equal(res.status, 0, `per-user uninstall exited ${res.status}`);
  assert.ok(!existsSync(USER_EXE), `${USER_EXE} survived uninstall`);
  assert.ok(!readUninstallMetadata("HKCU", USER_DIR), `HKCU uninstall entry survived for ${USER_DIR}`);
}

async function rejectUnsafeAllUsersDirectory() {
  console.log("[4/6] /ALLUSERS into user-writable directory must be rejected");
  mkdirSync(UNSAFE_MACHINE_DIR, { recursive: true });
  const run = startSetup(["/S", "/ALLUSERS", `/D=${UNSAFE_MACHINE_DIR}`]);
  const code = await exitCodeWithin(run, "setup /S /ALLUSERS (unsafe dir)", 120_000);
  assert.notEqual(code, 0, `/ALLUSERS unexpectedly succeeded into user-writable dir ${UNSAFE_MACHINE_DIR}`);
  assert.ok(!existsSync(join(UNSAFE_MACHINE_DIR, "block.exe")), "block.exe was written into unsafe /ALLUSERS dir");
  assert.ok(!readUninstallMetadata("HKLM", UNSAFE_MACHINE_DIR), "HKLM uninstall entry was written for unsafe /ALLUSERS dir");
}

async function machineInstallCoexistenceAndUpdate() {
  console.log("[5/6] machine-wide install (/S /ALLUSERS in Program Files), explicit /D= coexistence, and silent update");
  const installRun = startSetup(["/S", "/ALLUSERS", `/D=${MACHINE_DIR}`]);
  const installCode = await exitCodeWithin(installRun, "setup /S /ALLUSERS", 180_000);
  assert.equal(installCode, 0, `setup /S /ALLUSERS exited ${installCode}`);
  assertInstalled("machine-wide install", {
    dir: MACHINE_DIR,
    exe: MACHINE_EXE,
    uninstall: MACHINE_UNINSTALL,
    hive: "HKLM",
  });

  // Explicit /D=%LOCALAPPDATA%\Programs\Block must NOT be redirected to the existing HKLM install.
  const explicitUserRun = startSetup(["/S", `/D=${DEFAULT_USER_DIR}`]);
  const explicitUserCode = await exitCodeWithin(explicitUserRun, "setup /S /D=%LOCALAPPDATA%\\Programs\\Block", 180_000);
  assert.equal(explicitUserCode, 0, `explicit default-user /D= install exited ${explicitUserCode}`);
  assertInstalled("explicit per-user install alongside HKLM", {
    dir: DEFAULT_USER_DIR,
    exe: join(DEFAULT_USER_DIR, "block.exe"),
    uninstall: join(DEFAULT_USER_DIR, "uninstall.exe"),
    hive: "HKCU",
  });
  const uninstDefault = spawnSync(join(DEFAULT_USER_DIR, "uninstall.exe"), ["/S", `_?=${DEFAULT_USER_DIR}`], {
    windowsVerbatimArguments: true,
    timeout: 120_000,
  });
  assert.equal(uninstDefault.status, 0, `explicit default-user uninstall exited ${uninstDefault.status}`);
  assert.ok(!readUninstallMetadata("HKCU", DEFAULT_USER_DIR), "HKCU entry survived default-user uninstall");

  // Replace existing machine binary with a stub and update without /ALLUSERS
  // to confirm existing HKLM installs are auto-detected, atomically replaced,
  // and reopened cleanly without writing any HKCU uninstall entry.
  writeFileSync(MACHINE_EXE, "old-block-machine-placeholder");
  assert.ok(statSync(MACHINE_EXE).size < 1024);

  const updateRun = startSetup(["/S", "/UPDATE", `/D=${MACHINE_DIR}`]);
  const updateCode = await exitCodeWithin(updateRun, "setup /S /UPDATE (machine)", 180_000);
  if (updateCode !== 0) {
    const lastErr = readLastUpdateError();
    const aclInfo = describeDirAcl(MACHINE_DIR);
    assert.equal(
      updateCode,
      0,
      `setup /S /UPDATE (machine) exited ${updateCode}\nLastUpdateError: ${lastErr || "(empty)"}\nicacls:\n${aclInfo}`,
    );
  }
  assertInstalled("machine-wide updater install", {
    dir: MACHINE_DIR,
    exe: MACHINE_EXE,
    uninstall: MACHINE_UNINSTALL,
    hive: "HKLM",
  });
  assert.ok(
    !readUninstallMetadata("HKCU", MACHINE_DIR),
    `machine-wide update unexpectedly wrote HKCU uninstall entry for ${MACHINE_DIR}`,
  );
  await waitBlockReopened(MACHINE_EXE, "machine-wide /UPDATE");
  await killBlockAndWait();
}

async function silentMachineUninstall() {
  console.log("[6/6] silent machine-wide uninstall");
  const res = spawnSync(MACHINE_UNINSTALL, ["/S", `_?=${MACHINE_DIR}`], {
    windowsVerbatimArguments: true,
    timeout: 120_000,
  });
  assert.equal(res.status, 0, `machine-wide uninstall exited ${res.status}`);
  assert.ok(!existsSync(MACHINE_EXE), `${MACHINE_EXE} survived uninstall`);
  assert.ok(!readUninstallMetadata("HKLM", MACHINE_DIR), `HKLM uninstall entry survived for ${MACHINE_DIR}`);
}

await killBlockAndWait();
try {
  await interactiveUserInstallWithChangeDir();
  await seamlessUserUpdateAndRunningUninstallGuard();
  await rejectUnsafeAllUsersDirectory();
  await machineInstallCoexistenceAndUpdate();
  await silentMachineUninstall();
  console.log("Windows setup verified (UI, Change Location, icons, ACL guard, per-user & machine-wide updates, uninstall).");
} finally {
  try { await killBlockAndWait(); } catch {}
  try { rmSync(SMOKE_ROOT, { recursive: true, force: true }); } catch {}
  try { rmSync(MACHINE_SMOKE_ROOT, { recursive: true, force: true }); } catch {}
  try { rmSync(UNSAFE_MACHINE_DIR, { recursive: true, force: true }); } catch {}
}
