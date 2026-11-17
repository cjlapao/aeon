import { useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { MessageComposer, type ComposerDraft, type ChatPanel, type ChatSpecialKeyRequest } from "../components/agent-chat";
import "@fontsource-variable/inter";
import "../index.css";
import "@cjlapao/ui-kit/styles.css";
import "./chat-panels-demo.css";

function Demo() {
  const [draft, setDraft] = useState<ComposerDraft>({ text: "", attachments: [] });
  const [component, setComponent] = useState("aeon-ui");
  const [blueprint, setBlueprint] = useState("");
  const [request, setRequest] = useState<ChatSpecialKeyRequest | null>(null);
  const [result, setResult] = useState("");
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const choices: Record<string, string[]> = {
    "/": ["Frontend design", "Review changes"], "//": ["Chat panel design", "Component API review"],
    "@": ["README.md", "MessageComposer.tsx"], "$": ["Builder", "Reviewer"], commands: ["Choose model", "Export draft"],
  };
  const titles: Record<string, string> = { "/": "Skills and prompts", "//": "Link another loop", "@": "Files", "$": "Agents", commands: "Commands" };
  const picker: ChatPanel | undefined = request ? {
    id: request.key, title: titles[request.key] ?? "Context",
    items: (choices[request.key] ?? []).filter((label) => label.toLowerCase().includes(request.query.toLowerCase())).map((label) => ({
      id: label, label, onSelect: () => { setResult(`Selected ${label}`); setRequest(null); },
    })),
  } : undefined;
  return <main className="panels-demo">
    <header className="panels-demo-header"><div><p className="panels-demo-eyebrow">AEON / COMPOSER LAB</p>
      <h1>Start with a message.</h1><p>Standalone shared composer. Custom selections. Local fixture submission.</p></div></header>
    <div className="panels-demo-controls">
      <button onClick={() => {
        setDraft((current) => ({ ...current, text: "Review the current changes and check for regressions." }));
        inputRef.current?.focus();
      }}>Use quick starter</button>
      <a href="/chat-panels-demo.html">Open full chat demo</a>
    </div>
    <div style={{ marginTop: "min(24vh, 160px)" }}>
      <MessageComposer value={draft} onChange={setDraft} inputRef={inputRef} inputLabel="First message" placeholder="Describe the work… / skills, // loops, @ files, $ agents"
        submitLabel="Start loop" submittingLabel="Starting…" submitDisabled={!component || !blueprint}
        picker={picker} onPickerDismiss={() => setRequest(null)} onSpecialKeyChange={setRequest}
        onAddAction={(action) => {
          const key = { files: "", folders: "", skills: "/", loops: "//", agents: "$", commands: "commands" }[action];
          setRequest(key ? { key, query: "", start: 0, end: 0 } : null);
        }}
        selectors={({ disabled }) => <>
          <label>Component <select aria-label="Component" value={component} disabled={disabled} onChange={(event) => setComponent(event.target.value)}>
            <option value="aeon-ui">Aeon UI</option><option value="aeon-daemon">Aeon daemon</option>
          </select></label>
          <label>Blueprint <select aria-label="Blueprint" value={blueprint} disabled={disabled} onChange={(event) => setBlueprint(event.target.value)}>
            <option value="">Choose a blueprint</option><option value="review">Review</option><option value="build">Build</option>
          </select></label>
        </>}
        footer={<p className="chat-panel-muted" style={{ marginTop: 10, fontSize: 12 }}>{blueprint ? "Ready to submit the first message." : "Choose a blueprint to enable Start loop."}</p>}
        onSubmit={async (submitted) => {
          setResult(JSON.stringify({ component, blueprint, text: submitted.text, files: submitted.attachments.map((item) => item.relativePath ?? item.file.name) }, null, 2));
        }} />
    </div>
    {result && <pre role="status" style={{ marginTop: 20, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{result}</pre>}
  </main>;
}

createRoot(document.getElementById("root")!).render(<Demo />);
