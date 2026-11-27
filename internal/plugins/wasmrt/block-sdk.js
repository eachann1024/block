import * as std from 'qjs:std';

let seq = 1000;
const inboundQueue = [];

export class HostRPCError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'HostRPCError';
    this.code = code;
  }
}

export function log(...args) {
  const line = args
    .map((v) => {
      if (typeof v === 'string') return v;
      try {
        return JSON.stringify(v);
      } catch (_err) {
        return String(v);
      }
    })
    .join(' ');
  std.err.puts(line + '\n');
  std.err.flush();
}

export function send(obj) {
  std.out.puts(JSON.stringify(obj) + '\n');
  std.out.flush();
}

export function notifyHost(method, params = {}) {
  send({ jsonrpc: '2.0', method, params: params ?? {} });
}

function readMessageLine() {
  while (true) {
    const line = std.in.getline();
    if (line === null || line === undefined) {
      return null;
    }
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    try {
      const msg = JSON.parse(trimmed);
      if (msg && typeof msg === 'object' && !Array.isArray(msg)) {
        return msg;
      }
    } catch (_err) {
      // Ignore malformed lines, matching host readLoop behavior.
    }
  }
}

export function callHost(method, params = {}) {
  seq += 1;
  const reqId = `p-${seq}`;
  send({ jsonrpc: '2.0', id: reqId, method, params: params ?? {} });
  while (true) {
    const msg = readMessageLine();
    if (msg === null) {
      throw new Error('stdin closed while awaiting host response');
    }
    const isResponse =
      msg.id !== undefined &&
      msg.id !== null &&
      String(msg.id) === reqId &&
      !msg.method &&
      ('result' in msg || 'error' in msg);
    if (isResponse) {
      if (msg.error) {
        const code = typeof msg.error.code === 'number' ? msg.error.code : -32000;
        const message = msg.error.message || 'host error';
        throw new HostRPCError(code, message);
      }
      return msg.result ?? {};
    }
    inboundQueue.push(msg);
  }
}

function nextInboundMessage() {
  if (inboundQueue.length > 0) {
    return inboundQueue.shift();
  }
  return readMessageLine();
}

export function serve(handler) {
  if (typeof handler !== 'function') {
    throw new TypeError('serve(handler) requires a function');
  }
  while (true) {
    const req = nextInboundMessage();
    if (req === null) {
      break;
    }
    const method = req.method;
    if (typeof method !== 'string' || !method) {
      continue;
    }
    const reqId = req.id;
    const params = req.params && typeof req.params === 'object' && !Array.isArray(req.params) ? req.params : {};
    try {
      const result = handler(method, params, req);
      if (reqId !== undefined && reqId !== null) {
        send({ jsonrpc: '2.0', id: reqId, result: result === undefined ? null : result });
      }
      if (method === 'plugin.shutdown') {
        break;
      }
    } catch (err) {
      if (reqId !== undefined && reqId !== null) {
        const message = err && err.message ? String(err.message) : String(err);
        const code = err && typeof err.code === 'number' ? err.code : -32000;
        send({ jsonrpc: '2.0', id: reqId, error: { code, message } });
      }
      if (method === 'plugin.shutdown') {
        break;
      }
    }
  }
}

export const BlockSDK = {
  HostRPCError,
  log,
  send,
  notifyHost,
  callHost,
  serve,
};

if (typeof globalThis.BlockSDK === 'undefined') globalThis.BlockSDK = BlockSDK;
if (typeof globalThis.HostRPCError === 'undefined') globalThis.HostRPCError = HostRPCError;
if (typeof globalThis.serve === 'undefined') globalThis.serve = serve;
if (typeof globalThis.callHost === 'undefined') globalThis.callHost = callHost;
if (typeof globalThis.notifyHost === 'undefined') globalThis.notifyHost = notifyHost;
if (typeof globalThis.send === 'undefined') globalThis.send = send;
if (typeof globalThis.log === 'undefined') globalThis.log = log;
