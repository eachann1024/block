import { mkdirSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const ERROR_LIMIT = 4000;

// Monotonic elapsed wall time. Date timestamps below are labels only.
export function elapsedMs(startNs, endNs = process.hrtime.bigint()) {
  return signedElapsedMs(endNs - startNs);
}

function signedElapsedMs(ns) {
  const negative = ns < 0n;
  const magnitude = negative ? -ns : ns;
  const ms = Math.round(Number(magnitude) / 1_000) / 1_000;
  const signed = negative ? -ms : ms;
  return signed === 0 ? 0 : signed;
}

export function clipError(error) {
  const text = error instanceof Error ? error.message : String(error ?? "");
  if (text.length <= ERROR_LIMIT) return { error: text, error_truncated: false };
  return { error: text.slice(0, ERROR_LIMIT), error_truncated: true };
}

export function resolveCargoTimings({ explicit, env = process.env, argv = process.argv } = {}) {
  if (typeof explicit === "boolean") return explicit;
  return env.BLOCK_CARGO_TIMINGS === "1" || argv.includes("--cargo-timings");
}

export function createBuildTimer({ dest, profile, install, cargoTimings }) {
  const startedNs = process.hrtime.bigint();
  const startedAt = new Date().toISOString();
  const stages = [];
  let stagesNs = 0n;
  let current = null;
  let finished = null;

  function close(status, fields = {}) {
    if (!current) return;
    const ns = process.hrtime.bigint() - current.startNs;
    const elapsedNs = ns > 0n ? ns : 0n;
    stagesNs += elapsedNs;
    stages.push({
      name: current.name,
      status,
      elapsed_ms: elapsedMs(0n, elapsedNs),
      error: fields.error ?? null,
      error_truncated: fields.error_truncated ?? false,
      detail: { ...current.detail, ...fields.detail },
    });
    current = null;
  }

  function start(name) {
    if (finished) return;
    if (current) throw new Error(`build timing stage still open: ${current.name}`);
    current = { name, startNs: process.hrtime.bigint(), detail: {} };
  }

  function ok(detail) {
    if (finished) return;
    close("ok", { detail });
  }

  function fail(error, detail) {
    if (finished) return;
    close("failed", { ...clipError(error), detail });
  }

  function measure(name, fn) {
    if (finished) return undefined;
    start(name);
    let value;
    try {
      value = fn();
    } catch (error) {
      fail(error);
      throw error;
    }
    if (value && typeof value.then === "function") {
      return value.then(
        (resolved) => {
          ok();
          return resolved;
        },
        (error) => {
          fail(error);
          throw error;
        },
      );
    }
    ok();
    return value;
  }

  function annotate(detail, name) {
    if (finished || !detail) return;
    if (current && (!name || current.name === name)) {
      Object.assign(current.detail, detail);
      return;
    }
    const stage = stages.at(-1);
    if (!stage || (name && stage.name !== name)) return;
    Object.assign(stage.detail, detail);
  }

  function finish(options = {}) {
    if (finished) return finished;
    if (current) {
      const clipped = options.error ? clipError(options.error) : { error: null, error_truncated: false };
      close(options.interrupted ? "interrupted" : "failed", clipped);
    }
    const totalNs = process.hrtime.bigint() - startedNs;
    const failedStage = stages.find((stage) => stage.status !== "ok") ?? null;
    const clipped = options.error ? clipError(options.error) : { error: null, error_truncated: false };
    const report = {
      schema: 1,
      tool: "build-app",
      pid: process.pid,
      ok: options.ok === true && failedStage == null,
      profile,
      install: Boolean(install),
      cargo_timings: Boolean(cargoTimings),
      cargo_timings_dir: cargoTimings ? options.cargoTimingsDir ?? null : null,
      cargo_timings_report: cargoTimings ? options.cargoTimingsReport ?? null : null,
      cargo_timings_format: cargoTimings ? "html" : null,
      report_path: dest,
      clock: "process.hrtime.bigint",
      started_at: startedAt,
      finished_at: new Date().toISOString(),
      elapsed_ms: signedElapsedMs(totalNs),
      stages_ms: signedElapsedMs(stagesNs),
      unaccounted_ms: signedElapsedMs(totalNs - stagesNs),
      failed_stage: failedStage?.name ?? null,
      error: clipped.error,
      error_truncated: clipped.error_truncated,
      stages: stages.map((stage) => ({ ...stage, detail: { ...stage.detail } })),
    };
    finished = report;
    writeReport(dest, report);
    return report;
  }

  return { start, ok, fail, measure, annotate, finish };
}

export function writeReport(dest, report) {
  const body = `${JSON.stringify(report)}\n`;
  try {
    process.stderr.write(`BUILD_TIMING ${body}`);
  } catch {}
  let tmp = "";
  try {
    mkdirSync(dirname(dest), { recursive: true });
    tmp = join(dirname(dest), `.build-timing.${process.pid}.json`);
    writeFileSync(tmp, body);
    renameSync(tmp, dest);
  } catch (error) {
    if (tmp) {
      try { unlinkSync(tmp); } catch {}
    }
    try {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`BUILD_TIMING_WRITE_FAILED ${message}\n`);
    } catch {}
  }
}
