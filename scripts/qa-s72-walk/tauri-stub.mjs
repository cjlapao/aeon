// The `@tauri-apps` internals the built UI expects, stubbed for a plain
// browser: an invoke/event bridge over the WebSocket `bridge.mjs` serves.
//
// Extracted from `walk.mjs` (S72) so any probe that drives the built UI in
// Chrome — the walk, or a page-load timing probe — speaks the same shell
// contract rather than keeping a second copy that can drift from the first.
//
// Two pieces exist because of what the real plugin does and a stub must not
// pretend otherwise about: `registerListener`/`unregisterListener` are no-ops
// the event plugin calls during `listen`, and the protocol guard exists
// because an init script also runs on `about:blank`, where a `ws://` dial
// fails as a spurious page error.

export const initScript = `(() => {
  if (!(location.protocol === "http:" || location.protocol === "https:")) return; // the stub means nothing on about:blank (v2: guards the spurious ws://bridge/ console error)
  const ws = new WebSocket((location.protocol === "https:" ? "wss" : "ws") + "://" + location.host + "/bridge");
  const callbacks = new Map();
  let cbId = 0;
  let reqId = 0;
  const waiting = new Map();
  const queue = [];
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.kind === "result") {
      const { resolve: r, reject: j } = waiting.get(m.id) ?? {};
      waiting.delete(m.id);
      if (m.error !== undefined) j(m.error); else r(m.value);
    } else if (m.kind === "deliver") {
      const entry = callbacks.get(m.handler);
      if (entry) { if (entry.once) callbacks.delete(m.handler); entry.fn(m.detail); }
    } else if (m.kind === "closed") {
      for (const [id, entry] of callbacks) if (String(id).startsWith("closed:")) { /* channels re-listen on reconnect */ }
    }
  };
  const send = (msg) => { (ws.readyState === 1 ? (p) => ws.send(p) : (p) => queue.push(p))(JSON.stringify(msg)); };
  ws.onopen = () => { while (queue.length) ws.send(queue.shift()); };
  window.__TAURI_INTERNALS__ = {
    transformCallback: (fn, once = false) => { const id = ++cbId; callbacks.set(id, { fn, once }); return id; },
    unregisterCallback: (id) => callbacks.delete(id),
    invoke: (cmd, args) => new Promise((res, rej) => {
      const id = ++reqId;
      waiting.set(id, { resolve: res, reject: rej });
      send({ kind: "invoke", id, cmd, args });
    }),
    metadata: { currentWindow: { label: "main" }, currentWebview: { windowLabel: "main", label: "main" } },
  };
  window.__TAURI_EVENT_PLUGIN_INTERNALS__ = {
    registerListener: () => {},
    unregisterListener: () => {},
  };
  const flush = () => { while (queue.length && ws.readyState === 1) ws.send(queue.shift()); };
  setInterval(flush, 10);
})()`;
