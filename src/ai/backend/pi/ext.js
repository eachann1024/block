import net from "node:net";

const port = Number(process.env.BLOCK_PI_PORT);
const token = process.env.BLOCK_PI_TOKEN || "";
const specs = JSON.parse(process.env.BLOCK_PI_TOOLS || "[]");

function callBridge(payload) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: "127.0.0.1", port }, () => {
      socket.write(JSON.stringify({ token, ...payload }) + "\n");
    });
    let buffer = "";
    socket.setEncoding("utf8");
    socket.on("data", (chunk) => {
      buffer += chunk;
    });
    socket.on("error", reject);
    socket.on("end", () => {
      try {
        resolve(buffer.trim() ? JSON.parse(buffer) : { ok: true, value: {} });
      } catch (error) {
        reject(error);
      }
    });
  });
}

export default function blockPiExtension(pi) {
  for (const spec of specs) {
    pi.registerTool({
      name: spec.name,
      label: spec.name,
      description: spec.description || spec.name,
      parameters: spec.inputSchema || { type: "object", properties: {} },
      async execute(toolCallId, params) {
        const response = await callBridge({
          id: String(toolCallId || ""),
          name: spec.name,
          arguments: params || {},
        });
        if (!response?.ok) {
          throw new Error(response?.value?.message || "Block tool failed");
        }
        const text =
          typeof response.value === "string"
            ? response.value
            : JSON.stringify(response.value);
        return {
          content: [{ type: "text", text }],
          details: response.value ?? {},
        };
      },
    });
  }
}
