import { spawnSync } from "node:child_process";
import { bundleId, installedExecutable } from "./app-bundle.mjs";

const executable = "thinkly-fast";
const quitWaitMs = 8000;
const quitRequestMs = 4000;

export function appPids() {
  return pidsFor(new Set([installedExecutable]));
}

export function matchingPids(releaseBinary) {
  return pidsFor(new Set([installedExecutable, releaseBinary]));
}

function pidsFor(binaries) {
  const listed = spawnSync("ps", ["-axww", "-o", "pid=,comm="], { encoding: "utf8", maxBuffer: 8 * 1024 * 1024 });
  if (listed.status !== 0) throw new Error(listed.stderr || "ps failed");
  return listed.stdout.split("\n").flatMap((line) => {
    const match = /^\s*(\d+)\s+(.*)$/.exec(line.trimEnd());
    if (!match) return [];
    const pid = Number(match[1]);
    return Number.isInteger(pid) && binaries.has(match[2]) ? [pid] : [];
  });
}

export function requestQuit() {
  if (!appPids().length) return false;
  const result = spawnSync("osascript", ["-e", `tell application id "${bundleId}" to quit`], {
    encoding: "utf8",
    timeout: quitRequestMs,
  });
  return result.status === 0;
}

export async function stopRunningApp(releaseBinary) {
  const before = matchingPids(releaseBinary);
  if (!before.length) return [];
  const deadline = Date.now() + quitWaitMs;
  const asked = requestQuit();
  while (Date.now() < deadline) {
    if (!matchingPids(releaseBinary).length) return before;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  const stuck = matchingPids(releaseBinary);
  if (!asked) throw new Error(`could not ask ${bundleId} to quit; still running: ${stuck.join(", ")}`);
  throw new Error(`${executable} did not quit within ${quitWaitMs}ms: ${stuck.join(", ")}; refusing to kill unsaved work`);
}
