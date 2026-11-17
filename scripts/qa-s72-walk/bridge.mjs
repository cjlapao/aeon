// S72 T4 — the walk harness: a plain-browser stand-in for the Tauri shell.
//
// # What it is
// The bridge (`ui/src/lib/bridge.ts`) reaches the daemon only through the Tauri
// shell: `connect`, `disconnect`, `rpc_call`, `endpoint_path`, `workspace_*`,
// and the `aeon://*` event channels. In a plain browser that shell is absent —
// so this file IS the shell: it serves the built UI over HTTP, speaks the
// `@tauri-apps/api` internals contract to the page over a WebSocket, and talks
// to the REAL daemon over its unix socket with newline-delimited JSON-RPC —
// the same framing `src-tauri/src/session.rs` uses (`initialize` first, then
// `version`, then whatever the page calls).
//
// # What it cannot show
// Nothing Rust-side: the desktop window, tray, OS notifications, real Tauri
// ACL failures, and the true unix-socket transport are out of frame. Every
// byte of PROTOCOL here is the real thing; only the transport hop Rust would
// own is stubbed. The T4 Notes say this plainly.
//
// Run:  node bridge.mjs --sock <rpc.sock> --dist <ui/dist> [--port 8712]
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { extname, join, normalize, resolve } from "node:path";

const require = createRequire(import.meta.url);
const { WebSocketServer } = require(process.env.WS_MODULE ?? resolve(import.meta.dirname, "../../ui/node_modules/ws"));

const argv = process.argv.slice(2);
const arg = (name) => { const i = argv.indexOf(`--${name}`); return i >= 0 ? argv[i + 1] : undefined; };
const SOCK = arg("sock") ?? process.env.AEON_SOCK ?? (() => { console.error("--sock required"); process.exit(2); })();
const DIST = resolve(arg("dist") ?? resolve(import.meta.dirname, "../../ui/dist"));
const PORT = Number(arg("port") ?? process.env.WALK_PORT ?? 8712);

const MIME = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".json": "application/json", ".woff2": "font/woff2", ".map": "application/json" };

// --- the real daemon over the real socket ------------------------------------
let sockLineId = 0;
const pending = new Map(); // jsonrpc id -> {resolve, reject}
let sock = null;
let negotiated = null;
const pageClients = new Set();

const openSocket = () => new Promise((resolveOpen, rejectOpen) => {
  const net = require("net");
  const s = net.createConnection(SOCK);
  let buf = "";
  s.on("connect", () => { sock = s; resolveOpen(s); });
  s.on("error", (e) => {
    sock = null; negotiated = null;
    for (const { reject } of pending.values()) reject(e);
    pending.clear();
    // The shell's own closed notice, as session.rs emits it on aeon://closed.
    deliver("aeon://closed", String(e?.message ?? e));
    rejectOpen(e);
  });
  s.on("data", (chunk) => {
    buf += chunk.toString("utf8");
    let nl;
    while ((nl = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, nl).trim();
      buf = buf.slice(nl + 1);
      if (!line) continue;
      let msg; try { msg = JSON.parse(line); } catch { continue; }
      if (msg.id !== undefined && pending.has(msg.id)) {
        const { resolve: r, reject: j } = pending.get(msg.id);
        pending.delete(msg.id);
        if (msg.error) j(msg.error); else r(msg.result);
      } else if (msg.method) {
        // Forwarded exactly as session.rs does: event → aeon://event, the two
        // terminal notifications → aeon://terminal, everything else dropped.
        if (msg.method === "event") deliver("aeon://event", msg.params ?? null);
        else if (msg.method === "terminal.output" || msg.method === "terminal.exit") deliver("aeon://terminal", msg.params ?? null);
      }
    }
  });
});

function call(method, params, timeoutMs = 10000) {
  if (!sock) return Promise.reject(new Error("harness: no daemon connection"));
  const id = ++sockLineId;
  const line = JSON.stringify(params === undefined || params === null
    ? { jsonrpc: "2.0", id, method }
    : { jsonrpc: "2.0", id, method, params }) + "\n";
  return new Promise((res, rej) => {
    const timer = setTimeout(() => { pending.delete(id); rej(new Error(`harness timeout: ${method}`)); }, timeoutMs);
    pending.set(id, { resolve: (v) => { clearTimeout(timer); res(v); }, reject: (e) => { clearTimeout(timer); rej(e); } });
    sock.write(line);
  });
}

/** The Rust `connect`: initialize first, always, then version, per session.rs. */
async function shellConnect() {
  if (negotiated) return negotiated;
  if (!sock) await openSocket();
  const init = await call("initialize", { clientName: "s72-walk-harness", clientVersion: "s72", protocolVersion: 2 });
  const version = await call("version", null);
  negotiated = {
    daemonVersion: typeof version?.daemonVersion === "string" ? version.daemonVersion : "unknown",
    protocolVersion: typeof init?.protocolVersion === "number" ? init.protocolVersion : 2,
    endpoint: SOCK,
  };
  return negotiated;
}

// --- the Tauri contract to the page ------------------------------------------
const listens = new Map(); // channel -> Set<handlerId>

/** Fire every page callback registered for `channel`, in the api's own shape. */
function deliver(channel, payload) {
  for (const handler of listens.get(channel) ?? []) {
    for (const ws of pageClients) ws.send(JSON.stringify({ kind: "deliver", handler, detail: { event: channel, payload, id: 0 } }));
  }
}

async function handleInvoke(cmd, params = {}) {
  switch (cmd) {
    case "connect": return await shellConnect();
    case "disconnect": return null;
    case "endpoint_path": return SOCK;
    case "workspace_list": return [{ id: "walk", name: "S72 walk (throwaway store)", kind: "local" }];
    case "workspace_active": return { id: "walk", name: "S72 walk (throwaway store)", kind: "local" };
    case "rpc_call": return await call(params.method, params.params ?? null);
    // @tauri-apps/api/event 2.12 spells these `plugin:event|listen` (a colon
    // before the plugin name, the pipe before the verb) — measured, not assumed.
    case "plugin:event|listen":
    case "plugin|event|listen": {
      if (!listens.has(params.event)) listens.set(params.event, new Set());
      listens.get(params.event).add(params.handler);
      return null;
    }
    case "plugin:event|unlisten":
    case "plugin|event|unlisten": { listens.get(params.event)?.delete(params.handler); return null; }
    default: {
      // Other plugin commands (notification, os shell bits the walk never
      // exercises) answer inertly, logged — the walk must not die on a plugin
      // the desktop shell would serve.
      if (cmd.startsWith("plugin:")) { console.error(`harness: inert plugin answer for ${cmd}`); return null; }
      throw JSON.stringify({ code: -32601, message: `harness: unknown shell command ${cmd}`, kind: "rpc" });
    }
  }
}

const server = createServer(async (req, res) => {
  try {
    const path = normalize(decodeURIComponent((req.url ?? "/").split("?")[0]));
    const file = path === "/" ? "index.html" : path.replace(/^\//, "");
    const full = join(DIST, file);
    if (!full.startsWith(DIST) || !existsSync(full)) {
      // HashRouter: every unknown path is still the shell.
      res.writeHead(200, { "content-type": "text/html" }).end(await readFile(join(DIST, "index.html")));
      return;
    }
    res.writeHead(200, { "content-type": MIME[extname(full)] ?? "application/octet-stream" }).end(await readFile(full));
  } catch (e) {
    res.writeHead(500).end(String(e));
  }
});

const wss = new WebSocketServer({ server, path: "/bridge" });
wss.on("connection", (ws) => {
  pageClients.add(ws);
  ws.on("message", async (raw) => {
    let msg; try { msg = JSON.parse(raw.toString("utf8")); } catch { return; }
    if (msg.kind !== "invoke") return;
    try {
      ws.send(JSON.stringify({ kind: "result", id: msg.id, value: await handleInvoke(msg.cmd, msg.args) }));
    } catch (e) {
      // A DAEMON refusal and a harness failure are different things, and S72's
      // first version of this line conflated them: it negated the daemon's code,
      // labelled every refusal `kind: "transport"` and dropped `data` — which is
      // where a refusal's sentence lives (`protocol.Error.WithWhat` writes
      // `data.what` and leaves `message` the code's generic label, errors.go:115
      // and :68). The page then said "The call did not reach the daemon" about a
      // call that had reached it and been answered. Measured end to end by
      // S73 T8's walk (G-2); the shell this file stands in for keeps all three
      // parts (`src-tauri/src/rpc.rs`'s RpcError), so now so does the stand-in.
      const wire =
        typeof e === "string"
          ? e // the harness's own refusal, already an envelope
          : typeof e?.code === "number"
            ? JSON.stringify({ code: e.code, message: String(e.message ?? ""), data: e.data, kind: "rpc" })
            : JSON.stringify({ code: -32000, message: String(e?.message ?? e), kind: "transport" });
      ws.send(JSON.stringify({ kind: "result", id: msg.id, error: wire }));
    }
  });
  ws.on("close", () => pageClients.delete(ws));
});

server.listen(PORT, "127.0.0.1", () => console.log(`s72 walk harness: http://127.0.0.1:${PORT}/ sock=${SOCK}`));
