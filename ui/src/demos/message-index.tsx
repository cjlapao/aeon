import { useState } from "react";
import { createRoot } from "react-dom/client";
import { AgentChat, type ChatItem, type LoopMessageIndexEntry } from "../components/agent-chat";
import "@fontsource-variable/inter";
import "../index.css";
import "@cjlapao/ui-kit/styles.css";
import "./chat-panels-demo.css";

const kinds = ["goal", "message", "steer", "answer", "approval", "command"];
const history: ChatItem[] = [];
const index: LoopMessageIndexEntry[] = [];
for (let turn = 0; turn < 36; turn++) {
  const kind = kinds[turn % kinds.length]!;
  const text = `${turn + 1}. ${kind === "goal" ? "Build the loop workspace" : kind === "steer" ? "Keep going and check the edge cases" : kind === "answer" ? "Use the existing component library" : kind === "approval" ? "Approve the proposed change" : kind === "command" ? "Review the current configuration" : "Explain the next implementation step"}`;
  const id = `human:${turn}`;
  history.push(kind === "steer" ? { id, kind: "steer", target: "builder", text, state: "delivered" } : { id, kind: "message", author: "human", text });
  history.push({ id: `reply:${turn}`, kind: "text", agent: "builder", text: `Working on interaction ${turn + 1}. This reply belongs to a bounded history fixture. The navigator keeps only nearby messages loaded, while its markers describe the loop's human interactions.` });
  history.push({ id: `detail:${turn}`, kind: "thinking", agent: "builder", text: "Check state transitions, retained history, keyboard access, and cancellation." });
  index.push({ id: `anchor:${turn}`, targetItemId: id, preview: text, kind, contextPreview: `Builder response to interaction ${turn + 1}.` });
}
async function pause(signal: AbortSignal) {
  await new Promise<void>((resolve, reject) => {
    if (signal.aborted) { reject(new DOMException("Aborted", "AbortError")); return; }
    const abort = () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); };
    const timer = setTimeout(() => { signal.removeEventListener("abort", abort); resolve(); }, 120);
    signal.addEventListener("abort", abort, { once: true });
  });
}

function Demo() {
  const [items, setItems] = useState(history.slice(-24));
  const [markerCount, setMarkerCount] = useState(12);
  const [description, setDescription] = useState("Live tail: last 24 records");
  return <main className="panels-demo">
    <header className="panels-demo-header"><div><p className="panels-demo-eyebrow">AEON / LOOP HISTORY LAB</p>
      <h1>Find the human moments.</h1><p>Goals, messages, steers, answers, approvals, and commands. Fixture history; no daemon calls.</p></div></header>
    <div className="panels-demo-controls"><span role="status">{description} · {items.length}/{history.length} records in memory · {markerCount}/{index.length} markers</span></div>
    <section className="panels-demo-chat" aria-label="Loop history preview">
      <AgentChat items={items} height="100%" readOnly messageIndex={{ scopeKey: "fixture-loop",
        entries: index.slice(0, markerCount), hasMore: markerCount < index.length,
        onLoadMore: () => setMarkerCount((count) => Math.min(index.length, count + 12)),
        onRequestJump: async (entry, { before, after, signal }) => {
          await pause(signal);
          const position = history.findIndex((item) => item.id === entry.targetItemId);
          if (position < 0) throw new Error("Interaction not found in fixture history.");
          if (signal.aborted) throw new DOMException("Aborted", "AbortError");
          setItems(history.slice(Math.max(0, position - before), position + after + 1));
          setDescription(`Window around ${entry.kind} ${entry.preview}`);
          return { targetItemId: history[position]!.id };
        },
        onRequestLatest: async ({ signal }) => {
          await pause(signal);
          if (signal.aborted) throw new DOMException("Aborted", "AbortError");
          setItems(history.slice(-24)); setDescription("Live tail: last 24 records");
          return { targetItemId: history[history.length - 1]!.id };
        },
      }} />
    </section>
    <p className="panels-demo-footer">Hover or focus a marker · jump to unloaded history · choose another marker quickly · Return to latest · + loads marker metadata</p>
  </main>;
}
createRoot(document.getElementById("root")!).render(<Demo />);
