import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  Button,
  EmptyState,
  FormField,
  Input,
  Panel,
  PasswordInput,
  Pill,
  Select,
  Spinner,
} from "../../../components/kit";
import { rpc, RpcError } from "../../../lib/bridge";
import type { CapabilityStateView, RouteView, RoutesListResult, RoutesProbeResult, SetRouteParams } from "../../../lib/protocol";
import { OPENAI_ENDPOINTS } from "../../../lib/protocol";
import { availableEfforts } from "../../models/modelConfig";
import { ErrorPanel } from "../../../components/ErrorPanel";

/**
 * Providers — where a route is configured without editing the environment.
 *
 * # The one rule this page is built around
 *
 * **It never holds a credential.** The daemon tells it `hasKey` and a
 * four-character hint, and nothing more. So:
 *
 *   - the key field starts **empty on an edit**, and empty means *leave the
 *     existing key alone* — which is why editing a model name does not require
 *     re-typing a key
 *   - there is no "show key" affordance, because there is no key here to show
 *   - nothing is written to `localStorage`. A credential in browser storage
 *     would survive the window, outlive the daemon, and be readable by
 *     anything that can read that profile
 *
 * The key travels once, over the local socket — which is `0600`, in a `0700`
 * directory, with the peer's credentials checked against the running user
 * (P26). The transport logs method names and never params.
 */
export function ProvidersPage() {
  const [state, setState] = useState<RoutesListResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [editing, setEditing] = useState<DraftRoute | null>(null);
  const [busy, setBusy] = useState(false);
  // S18 T10 (M32): the probe exists ONLY behind an operator click. Nothing
  // on this page — not mount, not refresh, not save — may fire it; the
  // in-flight id disables the button so a second click sends nothing while
  // the one request the operator asked for is still out. The sentence is
  // the daemon's own reply to THIS click (a failure or a no-cache note),
  // rendered verbatim and re-rendered nowhere: the durable verdicts come
  // back through `routes.list` on the refresh, one source of facts.
  const [probingId, setProbingId] = useState<string | null>(null);
  const [probeSentence, setProbeSentence] = useState<{ route: string; text: string } | null>(null);

  const refresh = useCallback(async () => {
    try {
      setState(await rpc<RoutesListResult>("routes.list"));
      setError(null);
    } catch (e) {
      setError(e as RpcError);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const probe = useCallback(async (route: string) => {
    if (probingId !== null) return; // the lock is the page's; M32's is the daemon's
    setProbingId(route);
    setProbeSentence(null);
    try {
      const r = await rpc<RoutesProbeResult>("routes.probe", { route });
      const sentence = r.failure ?? r.note ?? null;
      setProbeSentence(sentence === null ? null : { route, text: sentence });
      await refresh(); // the earned states arrive here, never from this reply
    } catch (e) {
      setError(e as RpcError);
    } finally {
      setProbingId(null);
    }
  }, [probingId, refresh]);

  const save = async (draft: DraftRoute) => {
    setBusy(true);
    try {
      const params: SetRouteParams = {
        id: draft.id.trim(),
        provider: draft.provider,
        model: draft.model.trim(),
        baseUrl: draft.baseUrl.trim() || undefined,
        // Empty means KEEP. The page cannot send a key it does not have.
        apiKey: draft.apiKey || undefined,
        clearKey: draft.clearKey || undefined,
        maxOutputTokens: draft.maxOutputTokens ? Number(draft.maxOutputTokens) : undefined,
        contextWindow: draft.contextWindow ? Number(draft.contextWindow) : undefined,
        prices: pricesFromDraft(draft),
        // O8: blank prices are only allowed when the absence is a DECISION.
        // Absent keeps whatever is saved; true declares the route unmetered.
        // Neither, on a new route, is what the daemon refuses — by design.
        noPrice: draft.noPrice ? true : undefined,
        // "" means KEEP, so clearing it is explicit — the same distinction the
        // key has, for the same reason: a client editing prices must not drop
        // a setting it never showed.
        reasoningEffort: draft.reasoningEffort || undefined,
        openaiEndpoint: draft.openaiEndpoint || undefined,
        clearReasoningEffort: draft.existing && draft.reasoningEffort === "" ? true : undefined,
      };
      await rpc("routes.set", params);
      setEditing(null);
      await refresh();
    } catch (e) {
      setError(e as RpcError);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    setBusy(true);
    try {
      await rpc("routes.delete", { id });
      await refresh();
    } catch (e) {
      setError(e as RpcError);
    } finally {
      setBusy(false);
    }
  };

  if (!state && !error) {
    return <Spinner />;
  }

  return (
    <div className="space-y-4">
      {/* **The page says what the DAEMON says**, in both directions.
          A settings screen that looks like it persisted is worse than one that
          admits it did not — and now that routes do persist, a page still
          claiming they do not is the same lie the other way round.

          S3 shipped `persisted: false` honestly and named S4. S4 shipped
          without keeping that promise, and a person configuring a route here
          watched it vanish on restart. This is the fixed version. */}
      {state && !state.persisted && (
        <Alert
          tone="amber"
          title="Nothing here is saved"
          description="This daemon has no store attached, so routes and keys live in its memory and are gone when it restarts. That is not a configuration — `aeon serve` always attaches one — so if you are seeing this, the store failed to open and the Store tab will say why. For something that survives meanwhile, set ANTHROPIC_API_KEY or OPENAI_API_KEY in the environment before starting the daemon."
          data-testid="not-persisted"
        />
      )}
      {state && state.persisted && (
        <Alert
          tone="slate"
          title="Routes are saved; keys are kept beside the store, not in it"
          description="A route's configuration is a row in the store and survives a restart. Its credential is written to a 0600 file in the state directory — never into the database, because a key in a database is a key that gets copied with a backup and migrated by every schema change. `aeon db info` prints where both live."
          data-testid="persisted"
        />
      )}

      {error && <ErrorPanel error={error} onRetry={() => void refresh()} />}

      {state && state.routes.length === 0 && !editing && (
        <EmptyState
          title="No routes configured"
          subtitle="A route names a provider, a model and its prices. Without one there is nothing to call."
          actionLabel="Add a route"
          onAction={() => setEditing(emptyDraft(state.providers[0] ?? "anthropic"))}
          data-testid="no-routes"
        />
      )}

      {state?.routes.map((route) => (
        <RouteCard
          key={route.id}
          route={route}
          busy={busy}
          onEdit={() => setEditing(draftFrom(route))}
          onDelete={() => void remove(route.id)}
          onProbe={(id) => void probe(id)}
          probingId={probingId}
          probeSentence={probeSentence?.route === route.id ? probeSentence.text : null}
        />
      ))}

      {state && state.routes.length > 0 && !editing && (
        <Button
          variant="outline"
          onClick={() => setEditing(emptyDraft(state.providers[0] ?? "anthropic"))}
          data-testid="add-route"
        >
          Add a route
        </Button>
      )}

      {editing && state && (
        <RouteForm
          draft={editing}
          providers={state.providers}
          busy={busy}
          onChange={setEditing}
          onCancel={() => setEditing(null)}
          onSave={() => void save(editing)}
        />
      )}
    </div>
  );
}

function RouteCard({
  route,
  busy,
  onEdit,
  onDelete,
  onProbe,
  probingId,
  probeSentence,
}: {
  route: RouteView;
  busy: boolean;
  onEdit: () => void;
  onDelete: () => void;
  onProbe: (id: string) => void;
  probingId: string | null;
  probeSentence: string | null;
}) {
  return (
    <Panel data-testid={`route-${route.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <code className="font-medium">{route.id}</code>
            <Pill tone={route.source === "environment" ? "blue" : "violet"} size="sm">
              {route.source}
            </Pill>
            {/* **Priced or not, before a call rather than after one.** An
                unpriced route reports an `unknown` cost, and saying so here is
                better than explaining it afterwards. */}
            {!route.priced && (
              <Pill tone="amber" size="sm" data-testid={`unpriced-${route.id}`}>
                {route.noPrice
                  ? "unmetered — declared no price on purpose; cost reads unknown with its reason"
                  : "no prices — cost will read “not reported”"}
              </Pill>
            )}
          </div>
          <div className="mt-1 text-sm opacity-70">
            {route.provider} / {route.model}
          </div>
          {route.contextWindow ? <div className="mt-1 text-xs opacity-60">context window: {route.contextWindow} tokens</div> : null}
          {route.baseUrl && (
            <div className="mt-1 text-xs opacity-60">
              at <code>{route.baseUrl}</code>
            </div>
          )}
          <div className="mt-1 text-xs opacity-60" data-testid={`key-${route.id}`}>
            {route.hasKey ? (
              <>
                key set <code>{route.keyHint}</code>
              </>
            ) : (
              "no key"
            )}
          </div>
          <CapabilityPills route={route} />
          {probeSentence !== null && (
            // Verbatim. The daemon wrote this sentence — about a probe that
            // could not check, or verdicts the cache refused to keep — and a
            // surface that re-cases a refusal builds a second truth.
            <pre className="whitespace-pre-wrap text-xs mt-2 bg-subtle p-1" data-testid={`probe-sentence-${route.id}`}>
              {probeSentence}
            </pre>
          )}
        </div>

        <div className="flex gap-2">
          {/* M32's lock in a button: the probe is ONE minimal request and
              only ever answers to this click. In flight, the button is
              disabled — a second click sends nothing. */}
          <Button
            size="sm"
            variant="outline"
            disabled={busy || probingId !== null}
            loading={probingId === route.id}
            onClick={() => onProbe(route.id)}
            data-testid={`probe-${route.id}`}
          >
            {probingId === route.id ? "Probing…" : "Probe"}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={onEdit}>
            Edit
          </Button>
          <Button size="sm" variant="ghost" color="red" disabled={busy} onClick={onDelete}>
            Delete
          </Button>
        </div>
      </div>
    </Panel>
  );
}

/**
 * The wire capabilities as pills in exactly three visual states (M28, P31) —
 * and the two honest non-states above them: UNDECLARED (the route made no
 * declaration, so the probe asks anyway — I5) and DECLARED-NONE (a step
 * needing tools here is refused before the call — M29).
 *
 * `verified` is never assumed: it renders only with the probe's timestamp and
 * the model the verdict was earned against (O2). `contradicted` is loud and
 * carries the daemon's `detail` verbatim. `declared` is neutral — the
 * *absence* of a probe verdict, never a softened `verified`.
 */
function CapabilityPills({ route }: { route: RouteView }) {
  if (route.wireCapabilities == null) {
    return (
      <div className="mt-2 text-xs opacity-60" data-testid={`caps-undeclared-${route.id}`}>
        wire capabilities <strong>undeclared</strong> — the probe asks the server anyway (absence of a
        declaration is not a declaration of absence); declare them to earn verdicts
      </div>
    );
  }
  if (route.wireCapabilities.length === 0) {
    return (
      <div className="mt-2 text-xs opacity-60" data-testid={`caps-none-${route.id}`}>
        wire capabilities <strong>declared none</strong> — a step needing tools here is refused before
        the call, and no model request is ever made
      </div>
    );
  }
  const stateOf = (capability: string): CapabilityStateView | undefined =>
    route.capabilityStates?.find((c) => c.capability === capability);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1" data-testid={`caps-${route.id}`}>
      {route.wireCapabilities.map((flag) => {
        const verdict = stateOf(flag);
        const state = verdict?.state ?? "declared";
        return (
          <span key={flag} data-testid={`cap-${route.id}-${flag}`} className="inline-flex items-center">
            <Pill tone={state === "verified" ? "green" : state === "contradicted" ? "red" : "slate"} size="sm" title={verdict?.detail}>
              {flag}: {state}
            </Pill>
            {state === "verified" && verdict?.probedAt && (
              <span className="ml-1 text-[10px] opacity-60" data-testid={`probed-${route.id}-${flag}`}>
                probed {verdict.probedAt}
                {verdict.probedModel ? ` against ${verdict.probedModel}` : ""}
              </span>
            )}
            {state === "contradicted" && verdict?.detail && (
              <span className="ml-1 text-[10px] text-red-500" data-testid={`contradicted-detail-${route.id}-${flag}`}>
                {verdict.detail}
              </span>
            )}
          </span>
        );
      })}
    </div>
  );
}

export interface DraftRoute {
  id: string;
  provider: string;
  model: string;
  baseUrl: string;
  apiKey: string;
  clearKey: boolean;
  maxOutputTokens: string;
  contextWindow: string;
  reasoningEffort: string;
  reasoningEfforts?: string[] | null;
  openaiEndpoint: string;
  inputPrice: string;
  outputPrice: string;
  cacheReadPrice: string;
  cacheWritePrice: string;
  /** Declared unmetered on purpose (S18 O8) — the decision a blank price must not hide. */
  noPrice: boolean;
  /** True when editing an existing route, which changes what an empty key means. */
  existing: boolean;
  hasKey: boolean;
}

function emptyDraft(provider: string): DraftRoute {
  return {
    id: "",
    provider,
    model: "",
    baseUrl: "",
    apiKey: "",
    clearKey: false,
    maxOutputTokens: "4096",
    contextWindow: "",
    reasoningEffort: "",
    openaiEndpoint: "",
    inputPrice: "",
    outputPrice: "",
    cacheReadPrice: "",
    cacheWritePrice: "",
    noPrice: false,
    existing: false,
    hasKey: false,
  };
}

function draftFrom(route: RouteView): DraftRoute {
  return {
    id: route.id,
    provider: route.provider,
    model: route.model,
    baseUrl: route.baseUrl ?? "",
    // **Always empty.** The page was never told the key, and empty means keep.
    apiKey: "",
    clearKey: false,
    maxOutputTokens: String(route.maxOutputTokens || 4096),
    contextWindow: route.contextWindow ? String(route.contextWindow) : "",
    reasoningEffort: route.reasoningEffort ?? "",
    reasoningEfforts: route.reasoningEfforts,
    openaiEndpoint: route.openaiEndpoint ?? "",
    inputPrice: numberToField(route.prices?.inputUsdPerMTok),
    outputPrice: numberToField(route.prices?.outputUsdPerMTok),
    cacheReadPrice: numberToField(route.prices?.cacheReadUsdPerMTok),
    cacheWritePrice: numberToField(route.prices?.cacheWriteUsdPerMTok),
    noPrice: route.noPrice ?? false,
    existing: true,
    hasKey: route.hasKey,
  };
}

function numberToField(n: number | undefined): string {
  return n === undefined ? "" : String(n);
}

function fieldToNumber(s: string): number | undefined {
  const trimmed = s.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  return Number.isFinite(n) ? n : undefined;
}

function pricesFromDraft(d: DraftRoute) {
  const prices = {
    inputUsdPerMTok: fieldToNumber(d.inputPrice),
    outputUsdPerMTok: fieldToNumber(d.outputPrice),
    cacheReadUsdPerMTok: fieldToNumber(d.cacheReadPrice),
    cacheWriteUsdPerMTok: fieldToNumber(d.cacheWritePrice),
  };
  const any = Object.values(prices).some((v) => v !== undefined);
  return any ? prices : undefined;
}

function RouteForm({
  draft,
  providers,
  busy,
  onChange,
  onCancel,
  onSave,
}: {
  draft: DraftRoute;
  providers: string[];
  busy: boolean;
  onChange: (d: DraftRoute) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const set = <K extends keyof DraftRoute>(key: K, value: DraftRoute[K]) =>
    onChange({ ...draft, [key]: value });

  return (
    <Panel data-testid="route-form">
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          onSave();
        }}
      >
        <FormField label="Route id" hint="What `--route` names. Lower-case, no spaces." required>
          <Input
            value={draft.id}
            disabled={draft.existing}
            onChange={(e) => set("id", e.target.value)}
            placeholder="anthropic"
            data-testid="field-id"
          />
        </FormField>

        <FormField label="Provider" required>
          {/* A choice, not a free-text box: the daemon told us which adapters
              exist, and a typo here would otherwise fail after a prompt was
              typed. */}
          <Select
            value={draft.provider}
            onChange={(e) => set("provider", e.target.value)}
            data-testid="field-provider"
          >
            {providers.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </Select>
        </FormField>

        <FormField label="Model" required>
          <Input
            value={draft.model}
            onChange={(e) => set("model", e.target.value)}
            placeholder="claude-sonnet-5"
            data-testid="field-model"
          />
        </FormField>

        <FormField
          label="Base URL"
          hint="Leave empty for the provider's own endpoint. Point it at a local server for Ollama, a gateway, or the fixture server."
        >
          <Input
            value={draft.baseUrl}
            onChange={(e) => set("baseUrl", e.target.value)}
            placeholder="http://127.0.0.1:11434/v1"
            data-testid="field-base-url"
          />
        </FormField>

        <FormField
          label="API key"
          hint={
            draft.existing && draft.hasKey
              ? "A key is already set. Leave this empty to keep it — this page is never told what it is."
              : "Sent once over the local socket. Saved in a dedicated 0600 file beside the state directory, never in the database."
          }
        >
          <PasswordInput
            value={draft.apiKey}
            onChange={(e) => set("apiKey", e.target.value)}
            placeholder={draft.existing && draft.hasKey ? "unchanged" : "sk-…"}
            autoComplete="off"
            data-testid="field-api-key"
          />
        </FormField>

        {draft.existing && draft.hasKey && (
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={draft.clearKey}
              onChange={(e) => set("clearKey", e.target.checked)}
              data-testid="field-clear-key"
            />
            Remove the stored key
          </label>
        )}

        <FormField label="Max output tokens" hint="Required by Anthropic; a sensible bound on both.">
          <Input
            type="number"
            value={draft.maxOutputTokens}
            onChange={(e) => set("maxOutputTokens", e.target.value)}
            data-testid="field-max-tokens"
          />
        </FormField>

        <FormField label="Context window" hint="Model input budget in tokens. Set this to exercise step compaction. Blank on a new route means unknown; blank on an edit keeps the saved value.">
          <Input
            type="number"
            min="1"
            value={draft.contextWindow}
            onChange={(e) => set("contextWindow", e.target.value)}
            data-testid="field-context-window"
          />
        </FormField>

        {draft.provider === "openai" && (
          <FormField
            label="OpenAI API"
            hint="`responses` is the only one that accepts function tools AND reasoning together."
          >
            <Select
              value={draft.openaiEndpoint}
              onChange={(e) => set("openaiEndpoint", e.target.value)}
              data-testid="field-openai-endpoint"
            >
              <option value="">/v1/chat/completions (default)</option>
              {OPENAI_ENDPOINTS.filter((e) => e !== "chat").map((e) => (
                <option key={e} value={e}>
                  /v1/{e}
                </option>
              ))}
            </Select>
          </FormField>
        )}

        <FormField
          label="Reasoning effort"
          hint="OpenAI's dial. Leave on the provider's default unless you run steps — see below."
        >
          <Select
            value={draft.reasoningEffort}
            onChange={(e) => set("reasoningEffort", e.target.value)}
            data-testid="field-reasoning-effort"
          >
            <option value="">the provider's default</option>
            {availableEfforts(draft).map((e) => (
              <option key={e} value={e}>
                {e}
              </option>
            ))}
          </Select>
        </FormField>

        {/* The warning is attached to the ABSENCE, because the absence is what
            breaks a step — and it only applies where the restriction exists. */}
        {/* Scoped to the endpoint that actually has the restriction. On
            `/v1/responses` tools and reasoning coexist, so warning there would
            be teaching a rule that does not apply. */}
        {draft.provider === "openai" &&
          draft.openaiEndpoint !== "responses" &&
          draft.reasoningEffort !== "none" && (
            <Alert
              tone="amber"
              title="A step on this route will lower reasoning to `none`"
              description="/v1/chat/completions refuses function tools while reasoning is on, and every Aeon step offers tools. Ask keeps this setting; a step lowers itself and says so. Switch this route to /v1/responses to have both."
              data-testid="reasoning-effort-warning"
            />
          )}

        <fieldset className="rounded border border-white/10 p-3">
          <legend className="px-1 text-sm opacity-70">
            Prices — <strong>USD per million tokens</strong>
          </legend>
          {/* The unit is in the legend AND every label, because a
              factor-of-a-million error looks plausible in both directions. */}
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Input $/Mtok">
              <Input
                value={draft.inputPrice}
                onChange={(e) => set("inputPrice", e.target.value)}
                placeholder="3.00"
                data-testid="field-price-input"
              />
            </FormField>
            <FormField label="Output $/Mtok">
              <Input
                value={draft.outputPrice}
                onChange={(e) => set("outputPrice", e.target.value)}
                placeholder="15.00"
                data-testid="field-price-output"
              />
            </FormField>
            <FormField label="Cache read $/Mtok">
              <Input
                value={draft.cacheReadPrice}
                onChange={(e) => set("cacheReadPrice", e.target.value)}
                placeholder="0.30"
              />
            </FormField>
            <FormField label="Cache write $/Mtok">
              <Input
                value={draft.cacheWritePrice}
                onChange={(e) => set("cacheWritePrice", e.target.value)}
                placeholder="3.75"
              />
            </FormField>
          </div>
          {/* O8's checkbox: "there is no price" and "nobody said" are
              different facts, and only one of them is a decision. A NEW
              unpriced route without this is refused by the daemon — and
              says why — rather than reporting `unknown` forever. */}
          <label className="mt-2 flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              data-testid="field-no-price"
              checked={draft.noPrice}
              onChange={(e) => set("noPrice", e.target.checked)}
            />
            No price — declared unmetered on purpose
          </label>
          <p className="mt-2 text-xs opacity-60">
            A new route must carry a price or this declaration. A route left unpriced
            reports every call’s cost as “not reported”, which is honest but not useful —
            so the absence has to be a decision somebody made, not a field nobody filled.
          </p>
        </fieldset>

        <div className="flex gap-2">
          <Button type="submit" loading={busy} data-testid="save-route">
            Save
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}
