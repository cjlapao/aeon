import { useState } from "react";
import { createRoot } from "react-dom/client";
import { AgentChat, type ChatItem, type ChatPanel, type ChatTask, type ChatPersistentPanel, type ChatSpecialKeyRequest } from "../components/agent-chat";
import "@fontsource-variable/inter";
import "../index.css";
import "@cjlapao/ui-kit/styles.css";
import "./chat-panels-demo.css";

const initialTasks: ChatTask[] = [
  { id: "inspect", label: "Inspect chat and carousel APIs", agent: "Explorer", status: "completed" },
  { id: "build", label: "Build floating chat panels", agent: "Builder", status: "in_progress" },
  { id: "test", label: "Run focused component tests", agent: "Reviewer", status: "pending" },
  { id: "keyboard", label: "Check keyboard navigation", agent: "Reviewer", status: "pending" },
  { id: "theme", label: "Review light and dark themes", agent: "Builder", status: "pending" },
  { id: "docs", label: "Update component documentation", agent: "Builder", status: "pending" },
  { id: "review", label: "Review final changes", agent: "Reviewer", status: "pending" },
];
const initialItems: ChatItem[] = [
  { id: "intro", kind: "text", agent: "Builder", text: "I’m adding floating panels above the composer. Browse files, skills, and agents while the loop keeps working." },
  { id: "context", kind: "text", agent: "Explorer", text: "Persistent context starts as compact rows. Add a third panel to see the expandable stack. Type /, @, or $ to open one temporary picker over everything." },
];

function Demo() {
  const [dark, setDark] = useState(true);
  const [tasks, setTasks] = useState(initialTasks);
  const [items, setItems] = useState(initialItems);
  const [request, setRequest] = useState<ChatSpecialKeyRequest | null>(null);
  const [panelCount, setPanelCount] = useState(2);
  const [longContent, setLongContent] = useState(false);
  const [showTasks, setShowTasks] = useState(true);
  const [notice, setNotice] = useState("Type / for skills, // for loops, @ for files, $ for agents.");
  const definitions = [
    { id: "commands", title: "Commands", icon: "⌘", description: "Actions for this chat", labels: ["Compact conversation", "Export transcript", "Change model"], details: ["Summarize older turns", "Save this conversation", "Choose another provider"] },
    { id: "loops", title: "Link another loop", icon: "↗", description: "Reference another loop", labels: ["Chat panel design", "Review component API", "Verify keyboard navigation"], details: ["loop-101 · In progress", "loop-102 · Pending", "loop-103 · Completed"] },
    { id: "files", title: "Files", icon: "▤", description: "Project context", labels: ["AgentChat.tsx", "ChatPanels.tsx", "chatPanels.css", "README.md"], details: ["Chat surface", "Floating panels", "Panel styling", "Project guide"] },
    { id: "skills", title: "Skills & prompts", icon: "✧", description: "Tools for this turn", labels: ["Frontend design", "React best practices", "Brainstorming", "Review this change"], details: ["Interface craft", "Rendering and performance", "Explore requirements", "Reusable review prompt"] },
    { id: "agents", title: "Agents", icon: "◎", description: "Agents in this loop", labels: ["Explorer", "Builder", "Reviewer"], details: ["Gather context", "Build the change", "Verify behavior"] },
  ];
  const definition = definitions.find((panel) => panel.id === (request?.key === "commands" ? "commands" : request?.key === "//" ? "loops" : request?.key === "/" ? "skills" : request?.key === "@" ? "files" : "agents"));
  const picker: ChatPanel | undefined = request && definition ? {
    ...definition,
    description: `${request.key}${request.query} · Escape to close`,
    items: (longContent ? Array.from({ length: 60 }, (_, index) => `${definition.labels[index % definition.labels.length]} ${index + 1}`) : definition.labels).flatMap((label, index) => label.toLowerCase().includes(request.query.toLowerCase()) ? [{
      id: label, label, description: definition.details[index % definition.details.length], icon: definition.icon,
      onSelect: () => { setNotice(`Selected ${label}`); setRequest(null); },
    }] : []),
    emptyText: "No matches. Try another search.",
  } : undefined;
  const panels: ChatPersistentPanel[] = [
    { id: "goal", title: "Ongoing goal", summary: "Build the floating chat panel system", icon: "◎",
      content: <>{Array.from({ length: longContent ? 24 : 1 }, (_, index) => <p key={index}>Build temporary composer pickers and compact persistent panels. Keep tasks visible and let the host supply context.</p>)}</>,
      actions: <button aria-label="Pause goal" onClick={() => setNotice("Goal paused in fixture.")}>Ⅱ</button> },
    { id: "queued", title: "Queued message", summary: "Then check keyboard navigation and dark mode", icon: "↳",
      content: <textarea aria-label="Queued message" defaultValue="Then check keyboard navigation and dark mode" />,
      actions: <button aria-label="Remove queued message" onClick={() => setPanelCount(1)}>×</button> },
    { id: "note", title: "Context note", summary: "Keep existing composer behavior", icon: "▤",
      content: <p>Persistent context stays available when a temporary picker closes.</p> },
    { id: "followup", title: "Follow-up", summary: "Review the component API", icon: "↳",
      content: <p>Apps receive trigger keys and query text, then provide their own picker results.</p> },
  ].slice(0, panelCount);
  return <main className="panels-demo">
    <header className="panels-demo-header"><div><p className="panels-demo-eyebrow">AEON / COMPONENT LAB</p>
      <h1>Context within reach.</h1><p>Live React components. Fixture data. No daemon connection.</p></div>
      <button onClick={() => { document.documentElement.classList.toggle("dark", !dark); setDark(!dark); }}>{dark ? "Light theme" : "Dark theme"}</button>
    </header>
    <div className="panels-demo-controls">
      <button onClick={() => setPanelCount((value) => value >= 4 ? 1 : value + 1)}>Persistent panels: {panelCount}</button>
      <button onClick={() => setLongContent((value) => !value)}>{longContent ? "Short content" : "Long content"}</button>
      <button onClick={() => setShowTasks((value) => !value)}>{showTasks ? "Hide tasks" : "Show tasks"}</button>
      <button onClick={() => setTasks((values) => {
        const current = values.findIndex((task) => task.status === "in_progress");
        if (current < 0) return initialTasks;
        return values.map((task, index) => index === current ? { ...task, status: "completed" } :
          index === current + 1 ? { ...task, status: "in_progress" } : task);
      })}>Advance task</button>
      <span role="status">{notice}</span>
    </div>
    <section className="panels-demo-chat" aria-label="Interactive chat preview">
      <AgentChat height="100%" items={items} panels={panels} picker={picker} onPickerDismiss={() => setRequest(null)} taskList={showTasks ? { tasks } : undefined}
        agents={{ Builder: { id: "Builder", role: "Implementation" }, Explorer: { id: "Explorer", role: "Research" } }}
        composer={{ onSpecialKeyChange: setRequest, onAddAction: (action) => {
          const key = { skills: "/", loops: "//", agents: "$", commands: "commands", files: "", folders: "" }[action];
          setRequest(key ? { key, query: "", start: 0, end: 0 } : null);
        }, onAttachmentsChange: (attachments) => setNotice(`${attachments.length} attachment(s) staged locally.`),
        mode: "message", onSteer: () => {}, onMessage: (text, _route, attachments) => {
          setItems((values) => [...values, { id: `message-${values.length}`, kind: "text", agent: "You", text: [text, ...(attachments ?? []).map((item) => `Attached: ${item.relativePath ?? item.file.name}`)].filter(Boolean).join("\n") }]);
          setNotice("Message added to fixture chat.");
        }, placeholder: "Message the loop… / skills, // loops, @ files, $ agents" }} />
    </section>
    <p className="panels-demo-footer">Try: + menu · drop files anywhere in chat · paste an image · click its thumbnail · /, //, @, $</p>
  </main>;
}

createRoot(document.getElementById("root")!).render(<Demo />);
