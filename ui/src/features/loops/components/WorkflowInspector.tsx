import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { rpc, toRpcError } from "../../../lib/bridge";
import { fileReaderError } from "../../../lib/fileReader";
import { costLine } from "../../../lib/cost";
import { formatMs } from "../../../lib/duration";
import type { LiveState } from "../../../lib/live";
import { type LadderLane, type LadderStep } from "./Ladder";
import { MarkdownView } from "./MarkdownView";
import { Transcript } from "../pages/StepScreen";
import type { LoopDetail } from "../pages/LoopScreen";

interface StepBlock { kind: string; name?: string; input?: unknown; text?: string; output?: unknown }
interface StepRecord {
  id: number; status: string; agent?: string; phase?: string; outcome?: string; reason?: string;
  messages?: Array<{ role: string; content: StepBlock[] }>;
  contextManifest?: { files?: Array<{ path: string; changed?: boolean; missing?: boolean }>;
    observedChanges?: Array<{ path: string; kind: "changed" | "missing" }> };
}

function selectedStep(lane: LadderLane, id?: string): LadderStep | undefined {
  if (!id) return undefined;
  if (id.includes("/")) {
    const [itemId, ...parts] = id.split("/");
    return lane.items?.find((item) => item.id === itemId)?.steps.find((step) => step.id === parts.join("/"));
  }
  return lane.steps?.find((step) => step.id === id);
}

function writtenFiles(record: StepRecord | null): string[] {
  const files: string[] = [];
  for (const message of record?.messages ?? []) for (const block of message.content ?? []) {
    if (block.name !== "write_file") continue;
    let input = block.input;
    if (typeof input === "string") {
      try { input = JSON.parse(input); } catch { continue; }
    }
    if (input && typeof input === "object") {
      const path = (input as { path?: unknown; filename?: unknown }).path ?? (input as { filename?: unknown }).filename;
      if (typeof path === "string" && path.trim()) files.push(path);
    }
  }
  return [...new Set(files)];
}

export function WorkflowInspector({ loop, lane, stepId, live }: {
  loop: LoopDetail; lane: LadderLane; stepId: string; live: LiveState;
}) {
  const step = selectedStep(lane, stepId);
  const direct = lane.structure === "direct";
  const gate = step?.kind === "gate";
  const [record, setRecord] = useState<StepRecord | null>(null);
  const [stepError, setStepError] = useState("");
  const [artifact, setArtifact] = useState("");
  const [fileContent, setFileContent] = useState("");
  const [fileError, setFileError] = useState("");
  const [fileLoading, setFileLoading] = useState(false);

  useEffect(() => {
    setRecord(null); setStepError("");
    if (!step?.stepId || live.state !== "current") return;
    let active = true;
    void rpc<StepRecord>("step.show", { id: step.stepId }).then((result) => {
      if (active) setRecord(result);
    }).catch((cause) => { if (active) setStepError(toRpcError(cause).message); });
    return () => { active = false; };
  }, [step?.stepId, live.state, live.state === "current" ? live.generation : -1]);

  const shownRecord = record?.id === step?.stepId ? record : null;
  const writes = useMemo(() => writtenFiles(shownRecord), [shownRecord]);
  const changes = shownRecord?.contextManifest?.observedChanges ?? [];
  const artifacts = useMemo(() => [...new Set([...writes, ...changes.map((change) => change.path), ...(lane.produces ?? [])])],
    [writes, changes, lane.produces]);
  useEffect(() => { setArtifact(artifacts[0] ?? ""); }, [lane.id, stepId, artifacts.join("\0")]);
  useEffect(() => {
    setFileContent(""); setFileError(""); setFileLoading(false);
    if (!artifact || live.state !== "current") return;
    let active = true;
    setFileLoading(true);
    void rpc<{ content: string }>("loop.file.read", { id: loop.id, path: artifact }).then((result) => {
      if (active) setFileContent(result.content);
    }).catch((cause) => { if (active) setFileError(fileReaderError(cause, "loop.file.read")); })
      .finally(() => { if (active) setFileLoading(false); });
    return () => { active = false; };
  }, [artifact, loop.id, live.state, live.state === "current" ? live.generation : -1]);

  if (!step) return null;

  return <div data-testid="workflow-substep-details" className="min-w-0">
    <div className="min-w-0 space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><p className="text-xs font-semibold uppercase tracking-[.18em] aeon-accent-text">{direct ? gate ? "Approval gate" : "Step details" : "Selected substep"} · {lane.id}</p>
          <h2 className="mt-1 break-all text-xl font-semibold">{stepId}</h2></div>
        {step.stepId && <Link to={`/steps/${step.stepId}`} className="text-sm font-medium aeon-accent-text underline">Open full step →</Link>}
      </div>
      <div className="aeon-accent-outline border-t pt-4">
          <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
            <div><dt className="text-slate-500 dark:text-slate-400">Status</dt><dd>{step.status}{step.outcome ? ` · ${step.outcome}` : ""}</dd></div>
            {!gate && <div><dt className="text-slate-500 dark:text-slate-400">Cost</dt><dd>{costLine(step.cost).text}</dd></div>}
            {!gate && <div><dt className="text-slate-500 dark:text-slate-400">Step time</dt><dd>{typeof step.wallMs === "number" ? formatMs(step.wallMs) : "Not measured"}</dd></div>}
          </dl>
          {gate && loop.gatePrompt && loop.position.lane === lane.id && <p className="mt-3 text-sm">{loop.gatePrompt}</p>}
          {gate && step.outcome && <p className="mt-2 text-sm">Recorded decision: {step.outcome}</p>}
          {step.reason && <p className="mt-3 text-sm">{step.reason}</p>}
          {stepError && <p role="alert" className="mt-3 text-sm text-red-700 dark:text-red-300">{stepError}</p>}
      </div>

      {!gate && <section className="aeon-accent-outline border-t pt-4" aria-label="Step artifacts">
        <h3 className="font-semibold">Artifacts and changes</h3>
        {writes.length > 0 && <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">Files recorded in this step's write calls</p>}
        {artifacts.length ? <div className="mt-3 flex flex-wrap gap-2">{artifacts.map((path) => <button key={path} type="button"
          className={`rounded-lg border px-3 py-1.5 text-left text-sm ${artifact === path ? "aeon-accent-outline aeon-accent-wash" : "aeon-accent-outline hover:bg-white/10"}`}
          onClick={() => setArtifact(path)}>{path}</button>)}</div> : <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">No file paths recorded for this step.</p>}
        {changes.length ? <ul className="mt-3 space-y-1 text-sm">{changes.map((change) =>
          <li key={`${change.kind}:${change.path}`}>{change.kind === "missing" ? "Missing" : "Changed while suspended"}: {change.path}</li>)}</ul> : null}
        {artifact && <div className="aeon-accent-outline mt-3 min-w-0 rounded-lg border bg-white/35 p-3 dark:bg-black/10">
          <p className="mb-3 break-all text-xs font-semibold">{artifact} · {writes.includes(artifact) ? "step write" :
            changes.find((change) => change.path === artifact)?.kind === "missing" ? "missing change" :
            changes.some((change) => change.path === artifact) ? "changed while suspended" : "lane output"}</p>
          {live.state !== "current" ? <p className="text-sm">Reconnect to read this file.</p> : fileLoading ? <p className="text-sm">Reading file…</p> :
            fileError ? <p role="alert" className="text-sm text-red-700 dark:text-red-300">{fileError}</p> :
            artifact.toLowerCase().endsWith(".md") ? <MarkdownView content={fileContent} /> : <pre className="overflow-auto whitespace-pre-wrap break-words text-xs">{fileContent}</pre>}
        </div>}
      </section>}

      <section className="aeon-accent-outline border-t pt-4" aria-label="Step transcript">
        <h3 className="mb-3 font-semibold">Transcript</h3>
        {gate ? <p className="text-sm">Gate decisions and answers appear in the <a href="#loop-decision" className="underline">loop thread below</a>.</p> :
          step.stepId && !shownRecord && !stepError && live.state === "current" ? <p className="text-sm">Loading transcript…</p> :
          <Transcript messages={shownRecord?.messages} />}
      </section>

    </div>
  </div>;
}
