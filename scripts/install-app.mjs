import { spawn, spawnSync } from "node:child_process";
import { chmodSync, closeSync, constants as fsConstants, copyFileSync, cpSync, existsSync, lstatSync, mkdirSync, openSync, readdirSync, readFileSync, renameSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { installLinuxDesktopProtected, windowsExtensions } from "./file-associations.mjs";
import {
  assertInstallTarget,
  assertWritable,
  bundleId,
  installPath,
  installedExecutable,
  isProcessAlive,
  legacyInstallPath,
  pruneLaunchServicesRegistrations,
  refreshUserIconCache,
  remove,
  sameVolumeTemp,
  unregisterMacBundle,
  verifySignature,
} from "./app-bundle.mjs";
import { appPids } from "./stop-app.mjs";

const repoRoot = fileURLToPath(new URL("..", import.meta.url));
const readyWaitMs = 8000;

export function prepareInstall(bundle) {
  const present = assertInstallTarget(installPath);
  const hadDocumentIcon = present && pathExists(join(installPath, "Contents/Resources/DocumentIcon.icns"));
  assertWritable(dirname(installPath));
  const staging = sameVolumeTemp(installPath);
  const staged = join(staging, "staged-bundle");
  try {
    cpSync(bundle, staged, { recursive: true });
  } catch (error) {
    remove(staging);
    throw error;
  }
  return { staging, staged, backup: join(staging, "previous-bundle"), present, hadDocumentIcon, keep: false };
}

export function commitInstall(plan) {
  // Never kill or stop any running Block process; atomically replace on disk so the
  // running process stays undisturbed and the next launch opens the new build.
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
  const replacedPaths = [plan.staged];
  if (plan.present) {
    replacedPaths.push(plan.backup);
    unregisterMacBundle(plan.backup);
    remove(plan.backup);
  }
  if (pathExists(legacyInstallPath)) {
    replacedPaths.push(legacyInstallPath);
    unregisterMacBundle(legacyInstallPath);
    remove(legacyInstallPath);
  }
  cleanStaleInstallStaging(dirname(installPath), plan.staging);
  plan.keep = false;
  try {
    const now = new Date();
    utimesSync(installPath, now, now);
  } catch {}
  pruneLaunchServicesRegistrations(installPath, replacedPaths);
  spawnSync("open", ["-n", "-g", "-j", installPath, "--args", "--version"], {
    stdio: "ignore",
    env: detachedLaunchEnv(),
  });
  if (!plan.hadDocumentIcon) {
    refreshUserIconCache();
  }
  return installPath;
}

function cleanStaleInstallStaging(appsDir, currentStaging) {
  let entries = [];
  try {
    entries = readdirSync(appsDir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const ent of entries) {
    if (!ent.isDirectory() || !ent.name.startsWith(".block-install.")) continue;
    const candidate = join(appsDir, ent.name);
    if (candidate === currentStaging) continue;
    const parts = ent.name.split(".");
    const pid = Number.parseInt(parts[2] || "", 10);
    if (isProcessAlive(pid)) continue;
    try {
      for (const sub of readdirSync(candidate)) {
        if (sub.endsWith(".app")) unregisterMacBundle(join(candidate, sub));
      }
    } catch {}
    remove(candidate);
  }
}

function ensureDirWritable(dir) {
  mkdirSync(dir, { recursive: true });
  const probe = join(dir, `.block-write-probe-${process.pid}-${Date.now()}`);
  writeFileSync(probe, "");
  rmSync(probe, { force: true });
}

const sleepBuf = new Int32Array(new SharedArrayBuffer(4));
function sleepSync(ms) {
  Atomics.wait(sleepBuf, 0, 0, ms);
}

function uniqueTxToken() {
  return `${process.pid}-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}

let cachedOsLockBackend;
function loadOsLockBackend() {
  if (cachedOsLockBackend !== undefined) return cachedOsLockBackend;
  cachedOsLockBackend = null;
  if (typeof Bun === "undefined") return null;
  try {
    const req = createRequire(import.meta.url);
    const { dlopen, FFIType, ptr } = req("bun:ffi");
    if (process.platform === "win32") {
      const k32 = dlopen("kernel32.dll", {
        CreateFileW: {
          args: [FFIType.ptr, FFIType.u32, FFIType.u32, FFIType.ptr, FFIType.u32, FFIType.u32, FFIType.ptr],
          returns: FFIType.i64,
        },
        CloseHandle: {
          args: [FFIType.i64],
          returns: FFIType.i32,
        },
      });
      cachedOsLockBackend = { kind: "win32", CreateFileW: k32.symbols.CreateFileW, CloseHandle: k32.symbols.CloseHandle, ptr };
      return cachedOsLockBackend;
    }
    const candidates = process.platform === "darwin"
      ? ["/usr/lib/libSystem.B.dylib", "libSystem.B.dylib"]
      : ["libc.so.6", "libc.musl-x86_64.so.1", "libc.musl-aarch64.so.1", "libc.so"];
    for (const libName of candidates) {
      try {
        const libc = dlopen(libName, {
          flock: { args: [FFIType.i32, FFIType.i32], returns: FFIType.i32 },
        });
        if (typeof libc.symbols.flock === "function") {
          cachedOsLockBackend = { kind: "posix-flock", flock: libc.symbols.flock };
          return cachedOsLockBackend;
        }
      } catch {}
    }
  } catch {}
  return null;
}

function assertNotSymlinkLock(lockPath) {
  try {
    const st = lstatSync(lockPath);
    if (st.isSymbolicLink()) {
      throw new Error(`Refusing to acquire install lock on symlink: ${lockPath}`);
    }
  } catch (err) {
    if (err?.code !== "ENOENT") throw err;
  }
}

function acquireOsLockViaHelper(lockPath, timeoutMs) {
  const readyPath = `${lockPath}.ready-${uniqueTxToken()}`;
  const deadline = Date.now() + timeoutMs;
  let child;
  if (process.platform === "win32") {
    const psScript = [
      "$ErrorActionPreference = 'Stop';",
      "$lockPath = $args[0];",
      "$readyPath = $args[1];",
      "$deadline = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() + [int64]$args[2];",
      "$fs = $null;",
      "while ($true) {",
      "  try {",
      "    $fs = [System.IO.File]::Open($lockPath, [System.IO.FileMode]::OpenOrCreate, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None);",
      "    break;",
      "  } catch {",
      "    if ([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() -ge $deadline) { exit 2 }",
      "    Start-Sleep -Milliseconds 25;",
      "  }",
      "}",
      "try {",
      "  [System.IO.File]::WriteAllText($readyPath, 'ok');",
      "  [void][Console]::In.ReadToEnd();",
      "} finally {",
      "  $fs.Dispose();",
      "}",
    ].join("\n");
    child = spawn("powershell", ["-NoProfile", "-Command", psScript, lockPath, readyPath, String(timeoutMs)], {
      stdio: ["pipe", "ignore", "ignore"],
    });
  } else {
    const pyScript = [
      "import fcntl, os, sys, time",
      "lock_path, ready_path, timeout_ms = sys.argv[1], sys.argv[2], int(sys.argv[3])",
      "flags = os.O_CREAT | os.O_RDWR | getattr(os, 'O_NOFOLLOW', 0)",
      "fd = os.open(lock_path, flags, 0o644)",
      "deadline = time.monotonic() + timeout_ms / 1000.0",
      "while True:",
      "    try:",
      "        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)",
      "        break",
      "    except BlockingIOError:",
      "        if time.monotonic() >= deadline:",
      "            os.close(fd)",
      "            sys.exit(2)",
      "        time.sleep(0.025)",
      "with open(ready_path, 'w') as rf:",
      "    rf.write('ok')",
      "try:",
      "    sys.stdin.read()",
      "finally:",
      "    try: fcntl.flock(fd, fcntl.LOCK_UN)",
      "    except OSError: pass",
      "    os.close(fd)",
    ].join("\n");
    child = spawn("python3", ["-c", pyScript, lockPath, readyPath, String(timeoutMs)], {
      stdio: ["pipe", "ignore", "ignore"],
    });
  }

  let exited = false;
  let exitCode = null;
  child.once("exit", (code) => {
    exited = true;
    exitCode = code;
  });

  try {
    while (!existsSync(readyPath)) {
      if (exited || Date.now() >= deadline) {
        try { child.kill(); } catch {}
        throw new Error(`Timed out waiting for install lock at ${lockPath} (helper exit=${exitCode})`);
      }
      sleepSync(20);
    }
  } finally {
    try { rmSync(readyPath, { force: true }); } catch {}
  }

  return () => {
    try { child.stdin.end(); } catch {}
    try { child.kill(); } catch {}
  };
}

// withInstallLock serializes concurrent installer processes targeting the same
// directory using an OS kernel file lock (flock on POSIX, exclusive CreateFileW
// on Windows) that automatically releases when a holder exits or is killed.
// The lock file is never unlinked while held or released so concurrent waiters
// always synchronize on the exact same inode and corrupt/truncated lock file
// contents can never wedge future installs.
export function withInstallLock(dir, fn, timeoutMs = 30000) {
  mkdirSync(dir, { recursive: true });
  const lockPath = join(dir, ".block-install.lock");
  assertNotSymlinkLock(lockPath);

  const backend = loadOsLockBackend();
  const deadline = Date.now() + timeoutMs;

  if (backend?.kind === "posix-flock") {
    const LOCK_EX = 2;
    const LOCK_NB = 4;
    const LOCK_UN = 8;
    const flags = fsConstants.O_CREAT | fsConstants.O_RDWR | (fsConstants.O_NOFOLLOW || 0);
    const fd = openSync(lockPath, flags, 0o644);
    let acquired = false;
    try {
      while (true) {
        if (backend.flock(fd, LOCK_EX | LOCK_NB) === 0) {
          acquired = true;
          break;
        }
        if (Date.now() >= deadline) {
          throw new Error(`Timed out waiting for install lock at ${lockPath}`);
        }
        sleepSync(25);
      }
      return fn();
    } finally {
      if (acquired) {
        try { backend.flock(fd, LOCK_UN); } catch {}
      }
      try { closeSync(fd); } catch {}
    }
  }

  if (backend?.kind === "win32") {
    const GENERIC_READ_WRITE = 0xc0000000;
    const OPEN_ALWAYS = 4;
    const FILE_ATTRIBUTE_NORMAL = 0x80;
    const INVALID_HANDLE_VALUE = -1n;
    const pathBuf = Buffer.from(`${lockPath}\0`, "utf16le");
    let handle = INVALID_HANDLE_VALUE;
    try {
      while (true) {
        handle = BigInt(backend.CreateFileW(
          backend.ptr(pathBuf),
          GENERIC_READ_WRITE,
          0,
          0,
          OPEN_ALWAYS,
          FILE_ATTRIBUTE_NORMAL,
          0,
        ));
        if (handle !== INVALID_HANDLE_VALUE && handle !== 0n) {
          break;
        }
        if (Date.now() >= deadline) {
          throw new Error(`Timed out waiting for install lock at ${lockPath}`);
        }
        sleepSync(25);
      }
      return fn();
    } finally {
      if (handle !== INVALID_HANDLE_VALUE && handle !== 0n) {
        try { backend.CloseHandle(handle); } catch {}
      }
    }
  }

  const release = acquireOsLockViaHelper(lockPath, timeoutMs);
  try {
    return fn();
  } finally {
    release();
  }
}

function psSingleQuote(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

export function installWindows(exePath) {
  const localAppData = process.env.LOCALAPPDATA || join(homedir(), "AppData", "Local");
  const appData = process.env.APPDATA || join(homedir(), "AppData", "Roaming");
  const installDir = join(localAppData, "Programs", "Block");
  const targetExe = join(installDir, "block.exe");
  const startMenuDir = join(appData, "Microsoft", "Windows", "Start Menu", "Programs");
  const shortcut = join(startMenuDir, "Block.lnk");

  // Pre-flight checks BEFORE acquiring lock or replacing the installed binary:
  // 1) Resolve generated extension list.
  // 2) Verify both installDir and Start Menu dir can be created and written.
  const winExts = windowsExtensions(repoRoot);
  ensureDirWritable(installDir);
  ensureDirWritable(startMenuDir);

  return withInstallLock(installDir, () => {
    const tx = uniqueTxToken();
    const shortcutBackup = join(startMenuDir, `.Block.lnk.bak-${tx}`);

    const hadShortcut = existsSync(shortcut);
    if (hadShortcut) {
      copyFileSync(shortcut, shortcutBackup);
    }
    const cleanupShortcutBackup = () => {
      try { rmSync(shortcutBackup, { force: true }); } catch {}
    };

    const winUserProgId = "Block.User.Document";
    const psExtArray = winExts.map((ext) => psSingleQuote(ext)).join(",");
    const probeScript = [
      "$ErrorActionPreference = 'Stop';",
      "$hkcu = [Microsoft.Win32.Registry]::CurrentUser;",
      `$prog = 'Software\\Classes\\${winUserProgId}';`,
      "function Get-RegString([string]$subKey, [string]$name) {",
      "  $k = $hkcu.OpenSubKey($subKey, $false);",
      "  if ($null -eq $k) { return $null }",
      "  try { return $k.GetValue($name, $null) } finally { $k.Close() }",
      "}",
      "function Has-RegValue([string]$subKey, [string]$name) {",
      "  $k = $hkcu.OpenSubKey($subKey, $false);",
      "  if ($null -eq $k) { return $false }",
      "  try { return $null -ne $k.GetValue($name, $null) } finally { $k.Close() }",
      "}",
      "$hadProg = $false;",
      "$existingProgKey = $hkcu.OpenSubKey($prog, $false);",
      "if ($null -ne $existingProgKey) { $hadProg = $true; $existingProgKey.Close() }",
      "$existingExts = New-Object System.Collections.Generic.List[string];",
      `foreach ($ext in @(${psExtArray})) {`,
      `  if (Has-RegValue ('Software\\Classes\\' + $ext + '\\OpenWithProgids') '${winUserProgId}') {`,
      "    [void]$existingExts.Add($ext);",
      "  }",
      "}",
      "$snap = [ordered]@{",
      "  hadProg = $hadProg;",
      "  prevDefault = Get-RegString $prog '';",
      "  prevFriendly = Get-RegString $prog 'FriendlyTypeName';",
      "  prevIcon = Get-RegString ($prog + '\\DefaultIcon') '';",
      "  prevAppName = Get-RegString ($prog + '\\Application') 'ApplicationName';",
      "  prevAppDesc = Get-RegString ($prog + '\\Application') 'ApplicationDescription';",
      "  prevOpenFriendly = Get-RegString ($prog + '\\shell\\open') 'FriendlyAppName';",
      "  prevCmd = Get-RegString ($prog + '\\shell\\open\\command') '';",
      "  existingExts = @($existingExts.ToArray());",
      "};",
      "Write-Output ($snap | ConvertTo-Json -Compress -Depth 4);",
    ].join("\n");

    const probeRes = spawnSync("powershell", ["-NoProfile", "-Command", probeScript], { encoding: "utf8" });
    if (probeRes.error || probeRes.status !== 0 || probeRes.signal) {
      cleanupShortcutBackup();
      const detail = probeRes.error
        ? probeRes.error.message
        : probeRes.signal
          ? `powershell terminated by signal ${probeRes.signal}`
          : `${probeRes.stderr ?? ""}${probeRes.stdout ?? ""}`.trim() || `powershell exited ${probeRes.status}`;
      throw new Error(`Windows registry pre-flight snapshot failed: ${detail}`);
    }

    let regSnapshot;
    try {
      const rawOut = (probeRes.stdout ?? "").trim();
      const lastLine = rawOut.split(/\r?\n/).filter(Boolean).at(-1) ?? "";
      regSnapshot = JSON.parse(lastLine);
    } catch (error) {
      cleanupShortcutBackup();
      throw new Error(`Windows registry pre-flight snapshot parse failed: ${error.message}`);
    }
    const existingExtList = Array.isArray(regSnapshot?.existingExts)
      ? regSnapshot.existingExts
      : typeof regSnapshot?.existingExts === "string" && regSnapshot.existingExts
        ? [regSnapshot.existingExts]
        : [];
    const existingExtSet = new Set(existingExtList.map((ext) => String(ext).toLowerCase()));
    const addedCandidateExts = winExts.filter((ext) => !existingExtSet.has(ext.toLowerCase()));

    const staged = join(installDir, `.block.exe.staging-${tx}`);
    const backup = join(installDir, `block.exe.old-${tx}`);
    copyFileSync(exePath, staged);

    let movedOld = false;
    try {
      if (existsSync(targetExe)) {
        renameSync(targetExe, backup);
        movedOld = true;
      }
      renameSync(staged, targetExe);
    } catch (error) {
      if (movedOld && !existsSync(targetExe)) {
        try { renameSync(backup, targetExe); } catch {}
      }
      rmSync(staged, { force: true });
      cleanupShortcutBackup();
      throw error;
    }

    const rollbackShortcut = () => {
      if (hadShortcut && existsSync(shortcutBackup)) {
        copyFileSync(shortcutBackup, shortcut);
        cleanupShortcutBackup();
      } else if (!hadShortcut) {
        rmSync(shortcut, { force: true });
      }
    };

    const rollbackRegistry = () => {
      const restoreLines = [
        "$ErrorActionPreference = 'Continue';",
        "$hkcu = [Microsoft.Win32.Registry]::CurrentUser;",
        `$prog = 'Software\\Classes\\${winUserProgId}';`,
        "function Restore-RegString([string]$subKey, [string]$name, $value) {",
        "  if ($null -eq $value) {",
        "    $k = $hkcu.OpenSubKey($subKey, $true);",
        "    if ($null -ne $k) { try { $k.DeleteValue($name, $false) } finally { $k.Close() } }",
        "    return;",
        "  }",
        "  $k = $hkcu.CreateSubKey($subKey);",
        "  if ($null -ne $k) { try { $k.SetValue($name, [string]$value, [Microsoft.Win32.RegistryValueKind]::String) } finally { $k.Close() } }",
        "}",
      ];
      if (addedCandidateExts.length > 0) {
        const psAdded = addedCandidateExts.map((ext) => psSingleQuote(ext)).join(",");
        restoreLines.push(
          `foreach ($ext in @(${psAdded})) {`,
          "  try {",
          "    $rk = $hkcu.OpenSubKey('Software\\Classes\\' + $ext + '\\OpenWithProgids', $true);",
          `    if ($null -ne $rk) { try { $rk.DeleteValue('${winUserProgId}', $false) } finally { $rk.Close() } }`,
          "  } catch {}",
          "}",
        );
      }
      if (!regSnapshot?.hadProg) {
        restoreLines.push("try { $hkcu.DeleteSubKeyTree($prog, $false) } catch {}");
      } else {
        const toPsVal = (v) => (v === null || v === undefined ? "$null" : psSingleQuote(v));
        restoreLines.push(
          `Restore-RegString $prog '' ${toPsVal(regSnapshot.prevDefault)};`,
          `Restore-RegString $prog 'FriendlyTypeName' ${toPsVal(regSnapshot.prevFriendly)};`,
          `Restore-RegString ($prog + '\\DefaultIcon') '' ${toPsVal(regSnapshot.prevIcon)};`,
          `Restore-RegString ($prog + '\\Application') 'ApplicationName' ${toPsVal(regSnapshot.prevAppName)};`,
          `Restore-RegString ($prog + '\\Application') 'ApplicationDescription' ${toPsVal(regSnapshot.prevAppDesc)};`,
          `Restore-RegString ($prog + '\\shell\\open') 'FriendlyAppName' ${toPsVal(regSnapshot.prevOpenFriendly)};`,
          `Restore-RegString ($prog + '\\shell\\open\\command') '' ${toPsVal(regSnapshot.prevCmd)};`,
        );
      }
      restoreLines.push(
        "try {",
        "  Add-Type -Namespace BlockAssocRollback -Name Shell32 -MemberDefinition '[System.Runtime.InteropServices.DllImport(\"shell32.dll\")] public static extern void SHChangeNotify(int wEventId, uint uFlags, System.IntPtr dwItem1, System.IntPtr dwItem2);' -ErrorAction SilentlyContinue;",
        "  [BlockAssocRollback.Shell32]::SHChangeNotify(0x08000000, 0, [System.IntPtr]::Zero, [System.IntPtr]::Zero);",
        "} catch {}",
      );
      const rbRes = spawnSync("powershell", ["-NoProfile", "-Command", restoreLines.join("\n")], { encoding: "utf8" });
      if (rbRes.error || rbRes.status !== 0 || rbRes.signal) {
        const detail = rbRes.error
          ? rbRes.error.message
          : rbRes.signal
            ? `terminated by signal ${rbRes.signal}`
            : `${rbRes.stderr ?? ""}${rbRes.stdout ?? ""}`.trim() || `exited ${rbRes.status}`;
        throw new Error(`registry restore failed (${detail})`);
      }
    };

    const rollbackBinary = () => {
      const failedNew = join(installDir, `.block.exe.failed-${tx}`);
      if (movedOld) {
        if (existsSync(targetExe)) {
          try { renameSync(targetExe, failedNew); } catch {}
        }
        if (!existsSync(targetExe) && existsSync(backup)) {
          renameSync(backup, targetExe);
        }
        try { rmSync(failedNew, { force: true }); } catch {}
      } else {
        rmSync(targetExe, { force: true });
      }
    };

    const psExe = psSingleQuote(targetExe);
    const psInstallDir = psSingleQuote(installDir);
    const psShortcut = psSingleQuote(shortcut);
    const applyScript = [
      "$ErrorActionPreference = 'Stop';",
      "$hkcu = [Microsoft.Win32.Registry]::CurrentUser;",
      `$prog = 'Software\\Classes\\${winUserProgId}';`,
      "function Set-RegValue([string]$subKey, [string]$name, [string]$value) {",
      "  $k = $hkcu.CreateSubKey($subKey);",
      "  if ($null -eq $k) { throw \"Failed to open registry key: $subKey\" }",
      "  try { $k.SetValue($name, $value, [Microsoft.Win32.RegistryValueKind]::String) } finally { $k.Close() }",
      "}",
      "$ws = New-Object -ComObject WScript.Shell;",
      `$s = $ws.CreateShortcut(${psShortcut});`,
      `$s.TargetPath = ${psExe};`,
      `$s.WorkingDirectory = ${psInstallDir};`,
      `$s.IconLocation = (${psExe} + ',0');`,
      "$s.Save();",
      "Set-RegValue $prog '' 'Block Document';",
      "Set-RegValue $prog 'FriendlyTypeName' 'Block Document';",
      `Set-RegValue ($prog + '\\DefaultIcon') '' ('"' + ${psExe} + '",0');`,
      "Set-RegValue ($prog + '\\Application') 'ApplicationName' 'Block';",
      "Set-RegValue ($prog + '\\Application') 'ApplicationDescription' 'Block';",
      "Set-RegValue ($prog + '\\shell\\open') 'FriendlyAppName' 'Block';",
      `Set-RegValue ($prog + '\\shell\\open\\command') '' ('"' + ${psExe} + '" "%1"');`,
      `foreach ($ext in @(${psExtArray})) {`,
      `  Set-RegValue ('Software\\Classes\\' + $ext + '\\OpenWithProgids') '${winUserProgId}' '';`,
      "}",
      "try {",
      "  Add-Type -Namespace BlockAssoc -Name Shell32 -MemberDefinition '[System.Runtime.InteropServices.DllImport(\"shell32.dll\")] public static extern void SHChangeNotify(int wEventId, uint uFlags, System.IntPtr dwItem1, System.IntPtr dwItem2);' -ErrorAction SilentlyContinue;",
      "  [BlockAssoc.Shell32]::SHChangeNotify(0x08000000, 0, [System.IntPtr]::Zero, [System.IntPtr]::Zero);",
      "} catch {}",
    ].join("\n");

    const psRes = spawnSync("powershell", ["-NoProfile", "-Command", applyScript], { encoding: "utf8" });
    if (psRes.error || psRes.status !== 0 || psRes.signal) {
      const detail = psRes.error
        ? psRes.error.message
        : psRes.signal
          ? `powershell terminated by signal ${psRes.signal}`
          : `${psRes.stderr ?? ""}${psRes.stdout ?? ""}`.trim() || `powershell exited ${psRes.status}`;
      const msg = `Windows shortcut/file-association registration failed: ${detail}`;
      const rollbackErrors = [];
      try { rollbackShortcut(); } catch (err) { rollbackErrors.push(`shortcut: ${err.message}`); }
      try { rollbackRegistry(); } catch (err) { rollbackErrors.push(`registry: ${err.message}`); }
      try { rollbackBinary(); } catch (err) { rollbackErrors.push(`binary: ${err.message}`); }
      if (rollbackErrors.length > 0) {
        throw new Error(`${msg}; rollback issues: ${rollbackErrors.join("; ")}`);
      }
      throw new Error(msg);
    }

    cleanupShortcutBackup();
    // Only delete this transaction's own backup (plus any stale backups left by
    // already-dead PIDs); never remove another live process's backup.
    if (movedOld) {
      try { rmSync(backup, { force: true }); } catch {}
    }
    for (const name of readdirSync(installDir)) {
      const match = /^block\.exe\.old-(\d+)(?:-.*)?$/.exec(name);
      if (!match) continue;
      const pid = Number.parseInt(match[1], 10);
      if (pid === process.pid || isProcessAlive(pid)) continue;
      try { rmSync(join(installDir, name), { force: true }); } catch {}
    }
    return targetExe;
  });
}

export function installLinux(binPath) {
  const home = process.env.HOME || homedir();
  const binDir = join(home, ".local", "bin");
  const targetBin = join(binDir, "block");
  const dataHome = process.env.XDG_DATA_HOME || join(home, ".local", "share");
  const configHome = process.env.XDG_CONFIG_HOME || join(home, ".config");
  const appDataDir = (process.env.BLOCK_DATA_DIR || "").trim() || join(dataHome, "Block");
  const iconDir = join(dataHome, "icons", "hicolor", "1024x1024", "apps");
  const appDir = join(dataHome, "applications");

  // Pre-flight directory permission checks BEFORE replacing ~/.local/bin/block.
  ensureDirWritable(binDir);
  ensureDirWritable(iconDir);
  ensureDirWritable(appDir);
  ensureDirWritable(configHome);
  ensureDirWritable(appDataDir);

  return withInstallLock(binDir, () => {
    // Recover or clean up any interrupted previous Linux install transaction
    // while holding the exclusive per-binDir install lock.
    try {
      const entries = readdirSync(binDir);
      const staleBackups = [];
      const staleStaged = [];
      for (const name of entries) {
        if (name.startsWith(".block.old-")) {
          const full = join(binDir, name);
          try {
            const st = lstatSync(full);
            if (st.isFile() && st.size > 0) {
              staleBackups.push({ full, mtimeMs: st.mtimeMs });
            } else {
              staleStaged.push(full);
            }
          } catch {}
        } else if (name.startsWith(".block.staging-")) {
          staleStaged.push(join(binDir, name));
        }
      }
      if (!existsSync(targetBin) && staleBackups.length > 0) {
        staleBackups.sort((a, b) => b.mtimeMs - a.mtimeMs);
        const newest = staleBackups.shift();
        try {
          renameSync(newest.full, targetBin);
          chmodSync(targetBin, 0o755);
        } catch {}
      }
      for (const item of staleBackups) {
        try { rmSync(item.full, { force: true }); } catch {}
      }
      for (const full of staleStaged) {
        try { rmSync(full, { force: true }); } catch {}
      }
    } catch {}
    try {
      for (const name of readdirSync(iconDir)) {
        if (name.startsWith(".block.png.bak-")) {
          try { rmSync(join(iconDir, name), { force: true }); } catch {}
        }
      }
    } catch {}

    const tx = uniqueTxToken();
    const staged = join(binDir, `.block.staging-${tx}`);
    const backup = join(binDir, `.block.old-${tx}`);
    copyFileSync(binPath, staged);
    chmodSync(staged, 0o755);

    let hadOld = false;
    try {
      if (existsSync(targetBin)) {
        // Copy (never rename away) the existing binary before atomically
        // replacing targetBin so there is zero window where ~/.local/bin/block
        // is missing on disk if the process receives SIGKILL mid-install.
        copyFileSync(targetBin, backup);
        chmodSync(backup, 0o755);
        hadOld = true;
      }
      renameSync(staged, targetBin);
    } catch (error) {
      if (hadOld && !existsSync(targetBin) && existsSync(backup)) {
        try { renameSync(backup, targetBin); } catch {}
      }
      try { rmSync(staged, { force: true }); } catch {}
      try { rmSync(backup, { force: true }); } catch {}
      throw error;
    }

    const iconDst = join(iconDir, "block.png");
    const iconBackup = join(iconDir, `.block.png.bak-${tx}`);
    const hadIcon = existsSync(iconDst);

    try {
      const srcIcon = join(repoRoot, "assets", "app-icon", "icon-1024.png");
      if (existsSync(srcIcon)) {
        if (hadIcon) copyFileSync(iconDst, iconBackup);
        copyFileSync(srcIcon, iconDst);
      }
      // Publish block-user.desktop through Block's target-machine default-application
      // protection pipeline (checks existing defaults, pins implicit non-Block
      // defaults with write-ahead journaling, updates desktop DB, and verifies).
      installLinuxDesktopProtected({
        execPath: targetBin,
        dataHome,
        configHome,
        appDataDir,
        root: repoRoot,
      });
    } catch (error) {
      try {
        if (hadIcon && existsSync(iconBackup)) {
          copyFileSync(iconBackup, iconDst);
        } else if (!hadIcon) {
          rmSync(iconDst, { force: true });
        }
      } catch {}
      try { rmSync(iconBackup, { force: true }); } catch {}
      try {
        if (hadOld && existsSync(backup)) {
          renameSync(backup, targetBin);
        } else {
          rmSync(targetBin, { force: true });
        }
      } catch {}
      try { rmSync(backup, { force: true }); } catch {}
      throw error;
    }

    try { rmSync(iconBackup, { force: true }); } catch {}
    if (hadOld) {
      try { rmSync(backup, { force: true }); } catch {}
    }
    return targetBin;
  });
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
    const activated = spawnSync("osascript", ["-e", `tell application id "${bundleId}" to activate`], {
      encoding: "utf8",
      timeout: 4000,
    });
    if (activated.status !== 0) throw new Error((activated.stderr || activated.stdout || "activate failed").trim());
  }
  return running;
}
