import { useI18n } from "@cjlapao/ui-kit";
import { Button, EmptyState, Panel, Pill, Alert, SidePanel } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError, rpcTyped, toRpcError } from "../../../lib/bridge";
import { RelativeTime } from "../../../components/RelativeTime";
import { flowTitle, originBadge, type BlueprintRow } from "../blueprintsModel";
import { Busy, Segmented, TextField, Field, SelectField } from "./fields";
import { useMemo, useState } from "react";
import { FlowModel, validId } from "./flowModel";

// ── FlowGallery.tsx ─────────────────────────────────────────────
/** The library: every flow as a card that says what it is for, so the right one can be found without opening each. */
export function FlowGallery({
  rows,
  loading,
  error,
  onRetry,
  onOpen,
  onNew,
  canWrite,
  q,
  origin,
  onFilter,
}: {
  rows: BlueprintRow[];
  loading: boolean;
  error: string | undefined;
  onRetry: () => void;
  onOpen: (name: string) => void;
  onNew: () => void;
  canWrite: boolean;
  /** The filters travel to the daemon as params: what is shown is what it answered, never a list hidden here. */
  q: string;
  origin: string;
  onFilter: (f: { q?: string; origin?: string }) => void;
}) {
  const { surfaceTone } = useAppSettings();
  const { t } = useI18n();
  const shown = rows;

  if (error) return <ErrorPanel error={new RpcError(0, error)} onRetry={onRetry} />;
  return (
    <div className="space-y-5" data-testid="flow-gallery">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-56 flex-1">
          <TextField value={q} onChange={(v) => onFilter({ q: v })} placeholder="Search flows by name" />
        </div>
        <Segmented
          label="Show"
          value={origin}
          onChange={(v) => onFilter({ origin: v })}
          options={[
            { value: '', label: 'All' },
            { value: 'system', label: t('blueprints.list.shipped') },
            { value: 'user', label: t('blueprints.list.yours') },
          ]}
        />
        <Button onClick={onNew} disabled={!canWrite} data-testid="new-flow-button">
          New flow
        </Button>
      </div>
      {loading && rows.length === 0 ? (
        <Busy label="Loading flows…" />
      ) : shown.length === 0 ? (
        <EmptyState title={q !== '' || origin !== '' ? 'No flow matches' : 'No flows yet'} />
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {shown.map((r) => {
            const badge = originBadge(r.origin);
            return (
              <li key={r.name} className="h-full">
                <Panel
                  variant="simple"
                  tone={surfaceTone}
                  padding="sm"
                  corner="rounded-md"
                  onClick={() => onOpen(r.name)}
                  data-testid="flow-card"

                >
                  <span className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate">
                      <span className="block truncate text-base font-semibold">{flowTitle(r)}</span>
                      {flowTitle(r) !== r.name && <span className="block truncate font-mono text-xs opacity-60">{r.name}</span>}
                    </span>
                    {badge && (
                      <Pill size="sm" tone={badge.tone}>
                        {t(badge.key === 'shipped' ? 'blueprints.list.shipped' : 'blueprints.list.yours')}
                      </Pill>
                    )}
                    {r.divergedFromShipped && (
                      <Pill size="sm" tone="amber">
                        {t('blueprints.list.edited')}
                      </Pill>
                    )}
                  </span>
                  <span className={`text-sm ${r.description ? '' : 'italic opacity-50'}`}>{r.description || 'No description yet'}</span>
                  <span className="mt-auto flex flex-wrap gap-x-3 pt-2 text-xs opacity-70">
                    <span>{r.usedByCount ? `used by ${r.usedByCount}` : 'not used yet'}</span>
                    {typeof r.revision === 'number' && <span>rev {r.revision}</span>}
                    {r.updatedAt && <RelativeTime iso={r.updatedAt} />}
                  </span>
                </Panel>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

// ── NewFlowDialog.tsx ───────────────────────────────────────────
// Starting a flow: a name, a sentence about it, and a shape to begin from. The
// shapes are the three things people actually build; each one is a flow that
// already validates, so the first thing on screen is something that works.

type Start = "agent" | "approval" | "conversation" | "copy";

const STARTS: { value: Start; label: string; hint: string }[] = [
  { value: "agent", label: "One agent", hint: "A single agent step. The simplest flow there is." },
  { value: "approval", label: "Plan and approve", hint: "An agent writes a plan, then a person approves it or sends it back." },
  { value: "conversation", label: "Conversation", hint: "Runs once, then keeps the conversation open so people can keep talking." },
  { value: "copy", label: "Copy of…", hint: "Start from a flow that already exists." },
];

/** "Triage Bugs" -> "triage-bugs": a suggestion for the identifier, which the person may overwrite. */
function slugFromTitle(title: string): string {
  return title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 64);
}

function startingModel(id: string, title: string, description: string, start: Exclude<Start, "copy">): FlowModel {
  const base = { id, title: title || undefined, description: description || undefined, schemaVersion: 1, version: 1, questions: {} as FlowModel["questions"] };
  if (start === "approval") {
    return { ...base, questions: { approve: { prompt: "Approve the plan?", answers: ["approve", "reject"] } }, lanes: [
      { id: "plan", step: { kind: "model-step", agent: "project-planner", produces: ["plan.md"] } },
      { id: "plan-gate", step: { kind: "gate", question: "approve" }, onReject: { goto: "plan", counter: "plan_rounds", max: 2, then: "escalate" } },
    ] };
  }
  if (start === "conversation") return { ...base, after: "converse", lanes: [{ id: "talk", step: { kind: "model-step", agent: "chat" } }] };
  return { ...base, lanes: [{ id: "step", step: { kind: "model-step", agent: "" } }] };
}

export function NewFlowDialog({ existing, onClose, onCreated }: { existing: string[]; onClose: () => void; onCreated: (name: string) => void }) {
  const { controlTone, surfaceTone } = useAppSettings();
  const [title, setTitle] = useState("");
  const [name, setName] = useState("");
  // The slug follows the title until a person types their own.
  const [slugTouched, setSlugTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [start, setStart] = useState<Start>("agent");
  const [from, setFrom] = useState(existing[0] ?? "");
  const [error, setError] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);

  const nameError = useMemo(() => {
    if (name === "") return undefined;
    if (!validId(name)) return "Use lower-case letters, numbers, - or _, starting with a letter or number.";
    if (existing.includes(name)) return "A flow with this name already exists.";
    return undefined;
  }, [name, existing]);

  const create = async () => {
    setBusy(true);
    setError(undefined);
    try {
      if (start === "copy") {
        const copy = await rpcTyped("blueprint.duplicate", { from, name });
        onCreated(copy.name);
        return;
      }
      const rendered = (await rpcTyped("blueprint.render", { model: startingModel(name, title.trim(), description.trim(), start) } as never)) as unknown as { content: string };
      const saved = await rpcTyped("blueprint.save", { name, content: rendered.content, createOnly: true });
      onCreated(saved.name);
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  return <SidePanel isOpen onClose={onClose} title="New flow" subtitle="Title it, say what it is for, pick where to start." side="right" variant="floating-glass" tone={controlTone} surfaceTone={surfaceTone} width={460}
    footer={<div className="flex gap-2 border-t border-white/10 pt-3"><Button onClick={() => void create()} disabled={busy || name === "" || !!nameError || (start === "copy" && from === "")} data-testid="create-flow">Create flow</Button><Button variant="ghost" onClick={onClose}>Cancel</Button></div>}>
    <div className="space-y-5 p-1" data-testid="new-flow">
      {error && <Alert intent="danger" title={error} />}
      <Field label="Title" hint="What people see: the library and the page header.">
        <TextField value={title} onChange={(v) => { setTitle(v); if (!slugTouched) setName(slugFromTitle(v)); }} placeholder="Triage Bugs" label="Title" />
      </Field>
      <Field label="Name" hint="The flow's identifier. Loops, schedules and queues refer to a flow by it, so it cannot be changed later." error={nameError}>
        <TextField value={name} onChange={(v) => { setSlugTouched(true); setName(v); }} mono placeholder="triage-bugs" />
      </Field>
      <Field label="Description" hint="What it is for, in a sentence or two.">
        <TextField multiline rows={3} value={description} onChange={setDescription} placeholder="Sorts new bug reports and asks a person to approve the plan." />
      </Field>
      <Field label="Start from">
        <Segmented label="Start from" value={start} onChange={(v) => setStart(v as Start)} options={STARTS} />
      </Field>
      {start === "copy" && <Field label="Copy of"><SelectField label="Copy of" value={from} onChange={setFrom} options={existing.map((n) => ({ value: n, label: n }))} /></Field>}
    </div>
  </SidePanel>;
}
