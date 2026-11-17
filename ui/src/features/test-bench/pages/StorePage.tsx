import React, { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, InfoRow, Input, Panel, Pill } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { costLine } from "../../../lib/cost";
import {
  METHOD_LOOP_CREATE,
  METHOD_LOOP_LIST,
  METHOD_LOOP_WRITE,
  METHOD_STEP_LIST,
  METHOD_STEP_SHOW,
  METHOD_STEP_VERIFY,
  METHOD_STORE_AWAITING,
  METHOD_STORE_INFO,
  type AwaitingResult,
  type DocumentWriteResult,
  type LoopListResult,
  type LoopView,
  type StepListResult,
  type StepVerifyResult,
  type StepView,
  type StoreInfoResult,
} from "../../../lib/protocol";
import { ErrorPanel } from "../../../components/ErrorPanel";

/**
 * The store, where it will be consumed.
 *
 * # What this page is for
 *
 * S4 gave Aeon a memory. A memory is the one capability whose failures are
 * **invisible from where they happen**: a wrong migration, a lost `thinking`
 * signature or a stale derived answer all present somewhere else — a replay
 * rejected in S7, a loop the app shows as running that is actually waiting.
 *
 * So this page shows the things that would otherwise only be checkable with
 * `sqlite3`:
 *
 *   - the PATH, copyable, because every debugging session starts there
 *   - `foreign_keys` as READ FROM THE CONNECTION, because the wrong DSN
 *     spelling produces a store that looks configured and silently performs no
 *     cascades
 *   - the migration ledger, because a `failed` row means an upgrade stopped
 *     part-way and the store otherwise looks fine
 *   - the derived awaiting query, SQL and all, because there is no
 *     `awaiting_user` column for anyone arriving from v1 to find
 *   - a refused document path, with its reason, because a refusal that reads as
 *     a success is the failure mode that matters here
 *
 * It is a **test bench** (AGENTS §4b), not an early draft of S9: no loop
 * ladder, no thread, no designed experience. S9 builds those.
 */
export function StorePage() {
  const [info, setInfo] = useState<StoreInfoResult | null>(null);
  const [infoError, setInfoError] = useState<RpcError | null>(null);

  const loadInfo = useCallback(async () => {
    try {
      setInfo(await rpc<StoreInfoResult>(METHOD_STORE_INFO));
      setInfoError(null);
    } catch (e) {
      setInfoError(toRpcError(e));
    }
  }, []);

  useEffect(() => {
    void loadInfo();
  }, [loadInfo]);

  return (
    <div className="space-y-4">
      <Alert
        tone="slate"
        title="The store, as the app sees it"
        description="A memory's failures are invisible from where they happen: a lost signature surfaces three slices away as a provider error about something else. These panels show the facts that are otherwise only checkable with sqlite3. Nothing here runs a loop — the runtime is S5."
        data-testid="store-note"
      />

      {infoError && (
        <div data-testid="store-info-error">
          <ErrorPanel error={infoError} />
        </div>
      )}

      {info && <StoreFacts info={info} onRefresh={loadInfo} />}
      <LoopsPanel onChanged={loadInfo} />
      <StepsPanel />
      <AwaitingPanel />
    </div>
  );
}

function StoreFacts({ info, onRefresh }: { info: StoreInfoResult; onRefresh: () => void }) {
  // # Defended against `null`, even though the daemon no longer sends one
  //
  // Go marshals a nil slice as `null`, and `pending` is empty on every healthy
  // store — so it arrived as `null` every time and this page crashed on
  // `info.pending.length`, taking the whole tab blank with it.
  //
  // The daemon is fixed (`protocol/memory.go` normalises every collection, and
  // a test marshals the handlers' output and fails on `null`). This stays
  // anyway, because the page also has to survive an OLDER daemon and a
  // third-party one: the version skew is real, and a blank tab is the worst
  // possible way to report it.
  const applied = info.applied ?? [];
  const pending = info.pending ?? [];
  const tables = info.tables ?? [];
  const failed = applied.filter((m) => m.status && m.status !== "applied");

  return (
    <Panel tone="slate">
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm font-medium">store</span>
        <Button size="sm" variant="ghost" onClick={onRefresh} data-testid="store-refresh">
          refresh
        </Button>
      </div>

      <div className="mt-3 space-y-1" data-testid="store-facts">
        {/* The path first, and copyable: comparing it against the daemon's own
            startup line is how a mismatch gets found. */}
        <InfoRow label="path" value={info.path} mono copyable />
        <InfoRow
          label="size"
          value={
            info.walBytes > 0
              ? `${formatBytes(info.bytes)} + ${formatBytes(info.walBytes)} in the -wal`
              : formatBytes(info.bytes)
          }
          mono
        />
        <InfoRow
          label="journal"
          value={
            <span data-testid="journal-mode">
              <code>{info.journalMode}</code>
              {info.journalMode !== "wal" && (
                <Pill tone="red" size="sm" className="ml-2">
                  not WAL
                </Pill>
              )}
            </span>
          }
        />
        <InfoRow
          label="foreign_keys"
          value={
            // Shown as the ANSWER, with the consequence named when it is off.
            // `foreign_keys` is per connection and defaults off, and the
            // symptom of a missed one is a cascade that silently does not
            // happen — diagnosed as a data-modelling bug.
            <span data-testid="foreign-keys">
              {info.foreignKeys ? (
                <Pill tone="green" size="sm">
                  ON
                </Pill>
              ) : (
                <Pill tone="red" size="sm">
                  OFF — cascades will silently not happen
                </Pill>
              )}
            </span>
          }
        />
        <InfoRow
          label="pools"
          value={`1 writer · ${info.readPoolSize} readers · busy_timeout ${info.busyTimeoutMs}ms`}
          mono
        />
        <InfoRow
          label="sqlite-vec"
          value={
            <span data-testid="vec-version">
              <code>{info.vecWriterVersion}</code>
              <Pill tone="violet" size="sm" className="ml-2">
                statically linked
              </Pill>
              {info.vecReaderVersion !== info.vecWriterVersion && (
                <Pill tone="red" size="sm" className="ml-2">
                  the two pools disagree
                </Pill>
              )}
            </span>
          }
        />
        <InfoRow
          label="schema"
          value={
            <span data-testid="migration-count">
              {applied.length} applied
              {pending.length > 0 && (
                <Pill tone="amber" size="sm" className="ml-2">
                  {pending.length} pending
                </Pill>
              )}
            </span>
          }
        />
      </div>

      {failed.length > 0 && (
        <Alert
          tone="red"
          title="A migration is recorded as failed"
          description={`${failed.map((m) => m.name).join(", ")} — an upgrade stopped part-way, and the schema is at the version before it. The store otherwise looks fine, which is why this is shown here.`}
          data-testid="migration-failed"
          className="mt-3"
        />
      )}

      <details className="mt-3" data-testid="ledger">
        <summary className="cursor-pointer text-sm opacity-70">
          the migration ledger and {tables.length} tables
        </summary>
        <ul className="mt-2 space-y-1 text-xs font-mono">
          {applied.map((m) => (
            <li key={m.name}>
              {m.name} · {m.appliedAt ?? "—"}
            </li>
          ))}
        </ul>
        <p className="mt-2 text-xs font-mono opacity-70">{tables.join("  ")}</p>
      </details>
    </Panel>
  );
}

/**
 * Loops, and the two things about them worth seeing in a UI.
 *
 * The same slug in two project roots is **two loops** (DB4), so the list shows
 * the root beside the slug rather than the slug alone — a list keyed on the
 * slug is one that will eventually show two rows that look identical.
 *
 * `awaiting` is derived every time it is read. There is no column.
 */
function LoopsPanel({ onChanged }: { onChanged: () => void }) {
  const [loops, setLoops] = useState<LoopView[]>([]);
  const [all, setAll] = useState(true);
  const [slug, setSlug] = useState("");
  const [componentRoot, setProjectRoot] = useState("");
  const [goal, setGoal] = useState("");
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      // Scoped or not, EXPLICITLY. A daemon serves every project on the
      // machine, so "every loop" is a choice rather than a default.
      const res = await rpc<LoopListResult>(METHOD_LOOP_LIST, all ? { all: true } : { componentRoot });
      setLoops(res.loops ?? []);  // see StoreFacts on why `?? []` stays
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [all, componentRoot]);

  useEffect(() => {
    void load();
  }, [load]);

  const create = async () => {
    setBusy(true);
    try {
      await rpc<LoopView>(METHOD_LOOP_CREATE, { slug, componentRoot, goal });
      setError(null);
      setSlug("");
      setGoal("");
      await load();
      onChanged();
    } catch (e) {
      setError(toRpcError(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Panel>
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm font-medium">loops</span>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-1 text-xs opacity-70">
            <input
              type="checkbox"
              checked={all}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setAll(e.target.checked)}
              data-testid="loops-all"
            />
            every project
          </label>
          <Button size="sm" variant="ghost" onClick={() => void load()} data-testid="loops-refresh">
            refresh
          </Button>
        </div>
      </div>

      <div className="mt-3 grid gap-2 sm:grid-cols-3">
        <FormField label="slug">
          <Input
          value={slug}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSlug(e.target.value)}
          placeholder="refactor"
          data-testid="loop-slug"
        />
        </FormField>
        <FormField label="project root">
          <Input
          value={componentRoot}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setProjectRoot(e.target.value)}
          placeholder="/home/you/repo"
          data-testid="loop-project"
        />
        </FormField>
        <FormField label="goal">
          <Input
          value={goal}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setGoal(e.target.value)}
          placeholder="what it is for"
          data-testid="loop-goal"
        />
        </FormField>
      </div>
      <p className="mt-1 text-xs opacity-60">
        A loop is its slug <em>and</em> its project root together, so the same slug in two checkouts is
        two loops. Both are required — there is no default project, because the daemon&apos;s directory
        is not yours.
      </p>

      <div className="mt-2 flex gap-2">
        <Button size="sm" onClick={() => void create()} disabled={busy || !slug || !componentRoot} data-testid="loop-create">
          create
        </Button>
      </div>

      {error && (
        <div className="mt-3" data-testid="loops-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {loops.length === 0 ? (
        <p className="mt-3 text-sm opacity-60" data-testid="loops-empty">
          no loops {all ? "on this machine" : "in that project"}
        </p>
      ) : (
        <ul className="mt-3 space-y-2" data-testid="loops-list">
          {loops.map((l) => (
            <li key={`${l.componentRoot}::${l.slug}`} className="text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <code className="font-medium">{l.slug}</code>
                <span className="font-mono text-xs opacity-60">{l.componentRoot}</span>
                <Pill tone="slate" size="sm">
                  {l.phase}
                </Pill>
                {l.awaiting && (
                  <Pill tone="amber" size="sm" data-testid="loop-awaiting">
                    awaiting an answer
                  </Pill>
                )}
              </div>
              <DocumentWriter slug={l.slug} componentRoot={l.componentRoot} />
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

/**
 * Writing a document, and seeing a refusal.
 *
 * # Why the refusal is the feature here
 *
 * A document name reaching the store **comes from a model**. An absolute path
 * or a `..` segment is refused rather than normalised into range, because a
 * silent clamp turns an escape attempt into a successful write somewhere
 * unexpected — and it looks exactly like the feature working.
 *
 * So this input exists mainly so somebody can type `../../etc/passwd` and watch
 * it be refused, with the reason, where they will actually be reading it.
 */
function DocumentWriter({ slug, componentRoot }: { slug: string; componentRoot: string }) {
  const [name, setName] = useState("plan");
  const [content, setContent] = useState("# Plan\n");
  const [result, setResult] = useState<DocumentWriteResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);

  const write = async () => {
    try {
      setResult(await rpc<DocumentWriteResult>(METHOD_LOOP_WRITE, { slug, componentRoot, name, content }));
      setError(null);
    } catch (e) {
      setResult(null);
      setError(toRpcError(e));
    }
  };

  return (
    <details className="mt-1">
      <summary className="cursor-pointer text-xs opacity-60">write a document</summary>
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        <FormField label="name">
          <Input
          value={name}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
          placeholder="plan, or tasks/T1.md"
          data-testid="doc-name"
        />
        </FormField>
        <FormField label="content">
          <Input
          value={content}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setContent(e.target.value)}
          data-testid="doc-content"
        />
        </FormField>
      </div>
      <p className="mt-1 text-xs opacity-60">
        Try <code>../../etc/passwd</code>. It is <strong>refused</strong>, not normalised — a clamp would
        turn an escape attempt into a write somewhere unexpected, and these names come from a model.
      </p>
      <Button size="sm" className="mt-2" onClick={() => void write()} data-testid="doc-write">
        write
      </Button>

      {error && (
        <div className="mt-2" data-testid="doc-error">
          <ErrorPanel error={error} />
        </div>
      )}
      {result && (
        <p className="mt-2 text-xs font-mono" data-testid="doc-result">
          {result.document.file} · rev {result.document.rev}
          {result.unchanged && " · unchanged, so no revision was created"}
        </p>
      )}
    </details>
  );
}

/**
 * Steps, their cost in all three states, and `verify`.
 *
 * `verify` is the human-visible form of the transcript fidelity test: somebody
 * diagnosing a rejected replay at S7 runs it first. The signature's LENGTH is
 * shown because "present" and "the right length" are different claims, and a
 * truncated signature is the failure that reads as present.
 */
function StepsPanel() {
  const [slug, setSlug] = useState("");
  const [componentRoot, setProjectRoot] = useState("");
  const [steps, setSteps] = useState<StepView[]>([]);
  const [shown, setShown] = useState<StepView | null>(null);
  const [verified, setVerified] = useState<StepVerifyResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);

  const list = async () => {
    try {
      const res = await rpc<StepListResult>(METHOD_STEP_LIST, { slug, componentRoot });
      setSteps(res.steps ?? []);
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const show = async (id: number) => {
    try {
      setShown(await rpc<StepView>(METHOD_STEP_SHOW, { id }));
      setVerified(await rpc<StepVerifyResult>(METHOD_STEP_VERIFY, { id }));
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  return (
    <Panel>
      <span className="text-sm font-medium">steps</span>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        <FormField label="loop slug">
          <Input
          value={slug}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSlug(e.target.value)}
          data-testid="steps-slug"
        />
        </FormField>
        <FormField label="project root">
          <Input
          value={componentRoot}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setProjectRoot(e.target.value)}
          data-testid="steps-project"
        />
        </FormField>
      </div>
      <Button size="sm" className="mt-2" onClick={() => void list()} disabled={!slug || !componentRoot} data-testid="steps-list">
        list
      </Button>
      <p className="mt-1 text-xs opacity-60">
        Nothing here <em>runs</em> a step — the runtime is S5. These are steps saved by{" "}
        <code>ask --save-step</code>.
      </p>

      {error && (
        <div className="mt-3" data-testid="steps-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {steps.length > 0 && (
        <ul className="mt-3 space-y-1" data-testid="steps-rows">
          {steps.map((s) => {
            const cost = costLine(s.cost);
            return (
              <li key={s.id} className="flex flex-wrap items-center gap-2 text-sm">
                <button className="underline" onClick={() => void show(s.id)} data-testid={`step-${s.id}`}>
                  step {s.id}
                </button>
                <Pill tone="slate" size="sm">
                  {s.status}
                </Pill>
                {/* All three cost states, through the same renderer the CLI
                    uses. A surface that cannot show `unknown` will show a
                    confident wrong answer. */}
                <Pill tone={cost.tone} size="sm">
                  {cost.text}
                </Pill>
                {cost.detail && <span className="text-xs opacity-60">{cost.detail}</span>}
              </li>
            );
          })}
        </ul>
      )}

      {shown && (
        <div className="mt-3" data-testid="step-detail">
          {verified && (
            <p className="text-xs font-mono" data-testid="step-verify">
              {verified.ok ? "transcript verified" : `NOT verified — ${verified.mismatch}`} ·{" "}
              {verified.messages} messages ·{" "}
              {verified.signatureLength > 0
                ? `signature byte-identical (${verified.signatureLength} bytes)`
                : "no thinking signature in this transcript"}
            </p>
          )}
          <ol className="mt-2 space-y-2 text-sm">
            {(shown.messages ?? []).map((m, i) => (
              <li key={i}>
                <span className="text-xs uppercase opacity-60">{m.role}</span>
                {(m.content ?? []).map((b, j) => (
                  <div key={j} className="ml-2">
                    {b.kind === "thinking" ? (
                      <span className="opacity-70">
                        [thinking · {b.signature ? `signed, ${b.signature.length} bytes` : "UNSIGNED"}]{" "}
                        {b.text}
                      </span>
                    ) : b.kind === "tool_use" ? (
                      <code className="text-xs">
                        [tool_use {b.id} · {b.name}] {JSON.stringify(b.input)}
                      </code>
                    ) : b.kind === "tool_result" ? (
                      <code className="text-xs">
                        [tool_result {b.toolUseId}
                        {b.isError ? " · ERROR" : ""}] {b.content}
                      </code>
                    ) : (
                      <span>{b.text}</span>
                    )}
                  </div>
                ))}
              </li>
            ))}
          </ol>
        </div>
      )}
    </Panel>
  );
}

/**
 * The derived awaiting query — **with the SQL**.
 *
 * Anyone arriving from v1 will look for an `awaiting_user` column and not find
 * one. Showing the query is how they learn that the answer is derived (DB6): a
 * stored flag and an unanswered question can disagree, and when they do nothing
 * says which is right.
 */
function AwaitingPanel() {
  const [res, setRes] = useState<AwaitingResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);

  const load = useCallback(async () => {
    try {
      setRes(await rpc<AwaitingResult>(METHOD_STORE_AWAITING, { all: true }));
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <Panel tone="slate">
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm font-medium">awaiting an answer</span>
        <Button size="sm" variant="ghost" onClick={() => void load()} data-testid="awaiting-refresh">
          refresh
        </Button>
      </div>

      {error && (
        <div className="mt-3" data-testid="awaiting-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {res && (
        <>
          <pre className="mt-3 overflow-x-auto rounded bg-black/5 p-2 text-xs" data-testid="awaiting-sql">
            {res.sql}
          </pre>
          {(res.loops ?? []).length === 0 ? (
            <p className="mt-2 text-sm opacity-60" data-testid="awaiting-none">
              no loops awaiting an answer
            </p>
          ) : (
            <ul className="mt-2 space-y-1 text-sm" data-testid="awaiting-list">
              {(res.loops ?? []).map((l) => (
                <li key={`${l.componentRoot}::${l.slug}`}>
                  <code>{l.slug}</code> <span className="font-mono text-xs opacity-60">{l.componentRoot}</span>
                </li>
              ))}
            </ul>
          )}
          <p className="mt-2 text-xs opacity-60">
            There is no <code>awaiting_user</code> column. The answer is derived, so a flag and an
            unanswered question can never disagree.
          </p>
        </>
      )}
    </Panel>
  );
}

function formatBytes(n: number): string {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MiB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KiB`;
  return `${n} bytes`;
}
