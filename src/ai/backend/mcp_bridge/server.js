const readline = require('node:readline');
const net = require('node:net');
const tools = JSON.parse(process.env.BLOCK_MCP_TOOLS);
const send = (message) => process.stdout.write(JSON.stringify(message) + '\n');
const result = (id, value) => send({jsonrpc: '2.0', id, result: value});
readline.createInterface({input: process.stdin}).on('close', () => process.exit(0)).on('line', (line) => {
  let message;
  try { message = JSON.parse(line); } catch { return; }
  if (message.id === undefined) return;
  if (message.method === 'initialize') return result(message.id, {
    protocolVersion: message.params.protocolVersion, capabilities: {tools: {}},
    serverInfo: {name: 'block', version: '1'}
  });
  if (message.method === 'ping') return result(message.id, {});
  if (message.method === 'tools/list') {
    const socket = net.createConnection({host: '127.0.0.1', port: Number(process.env.BLOCK_MCP_PORT)});
    socket.on('connect', () => socket.end(JSON.stringify({token: process.env.BLOCK_MCP_TOKEN, ready: true}) + '\n'));
    socket.on('error', () => {});
    return result(message.id, {tools});
  }
  if (message.method !== 'tools/call') return send({jsonrpc: '2.0', id: message.id, error: {code: -32601, message: 'Method not found'}});
  const socket = net.createConnection({host: '127.0.0.1', port: Number(process.env.BLOCK_MCP_PORT)});
  let response = '';
  socket.setEncoding('utf8');
  socket.setTimeout(300000);
  socket.on('connect', () => socket.write(JSON.stringify({token: process.env.BLOCK_MCP_TOKEN, id: String(message.id), name: message.params.name, arguments: message.params.arguments || {}}) + '\n'));
  socket.on('data', data => {
    response += data;
    const end = response.indexOf('\n');
    if (end < 0) return;
    socket.destroy();
    try {
      const reply = JSON.parse(response.slice(0, end));
      result(message.id, {content: [{type: 'text', text: JSON.stringify(reply.value)}], isError: !reply.ok});
    } catch { result(message.id, {content: [{type: 'text', text: 'Invalid Block bridge response'}], isError: true}); }
  });
  socket.on('error', () => result(message.id, {content: [{type: 'text', text: 'Block bridge unavailable'}], isError: true}));
  socket.on('timeout', () => { socket.destroy(); result(message.id, {content: [{type: 'text', text: 'Block bridge timed out'}], isError: true}); });
});
