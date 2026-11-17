import { useEffect, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ConfirmModal, FormField, Pill, Select } from "../../../components/kit";
import { errorSentence, refusalBody, toRpcError, type RpcError } from "../../../lib/bridge";
import { runEnvironmentBind } from "./environmentsRpc";
import {
  SCOPE_COMPONENT,
  SCOPE_WORKSPACE,
  backendOf,
  type BindAnswer,
  type ComponentEntry,
  type DisplayRow,
  type Vocabulary,
} from "./environmentsModel";

/**
 * The bind door (S59 T11, EV16/EV18), in the order the daemon expects:
 *
 *   1. `environment.bind {dryRun: true}` — the answer IS the sentence the
 *      applied write will use (one formatter, `execenv.DescribeBind`), so the
 *      person reads the truth of what is about to change: the scope, the
 *      environment by slug and id, the backend WITH its strength number, the
 *      comparison against what was bound before, the references by name, and
 *      EV6's "unattended loops never inherit".
 *   2. `useConfirm` — declining sends NOTHING. There is no second call and no
 *      local edit: the dry run applied nothing either (`changed: false`).
 *   3. On confirm, `environment.bind` again with `dryRun: false`, and the
 *      caller updates from THAT answer — never optimistically.
 *
 * A refusal is rendered verbatim, and the dialog keeps the person's choices so
 * they can read the refusal against the request that provoked it.
 */
export interface BindDialogProps {
  row: DisplayRow;
  components: readonly ComponentEntry[];
  vocabulary: Vocabulary | undefined;
  canWrite: boolean;
  busy: boolean;
  /** Called with the APPLIED answer (dryRun false) — the caller refreshes from it. */
  onBound: (answer: BindAnswer) => void;
  onRefusal: (message: string) => void;
  onClose: () => void;
}

export function BindDialog({ row, components, vocabulary, canWrite, busy, onBound, onRefusal, onClose }: BindDialogProps) {
  const { t } = useI18n();
  const [scope, setScope] = useState<string>(SCOPE_WORKSPACE);
  const [componentId, setComponentId] = useState("");
  const [statement, setStatement] = useState<string | undefined>(undefined);
  const [failure, setFailure] = useState<RpcError | undefined>(undefined);
  const [confirming, setConfirming] = useState(false);

  const strength = backendOf(vocabulary, row.backend)?.strength;
  const scoped = scope === SCOPE_COMPONENT ? componentId : "";

  // The preview is asked for when the target changes, before anything is
  // confirmed; it is a dry run, so it applies nothing (P3).
  useEffect(() => {
    if (scope === SCOPE_COMPONENT && componentId === "") {
      setStatement(undefined);
      return;
    }
    let alive = true;
    setFailure(undefined);
    setStatement(undefined);
    void runEnvironmentBind({
      scope,
      componentId: scoped,
      environmentId: row.id,
      dryRun: true,
    })
      .then((answer) => {
        if (alive) setStatement(answer.statement);
      })
      .catch((cause: unknown) => {
        if (alive) setFailure(toRpcError(cause));
      });
    return () => {
      alive = false;
    };
  }, [scope, componentId, scoped, row.id]);

  const apply = async () => {
    setConfirming(false);
    try {
      onBound(
        await runEnvironmentBind({
          scope,
          componentId: scoped,
          environmentId: row.id,
          dryRun: false,
        }),
      );
    } catch (cause) {
      // The refusal is the caller's toast, verbatim; the scope/component choices are left as they were.
      onRefusal(refusalBody(toRpcError(cause)));
    }
  };

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <div className="space-y-8" data-testid="environments-bind-dialog">
      <p className="text-sm font-medium" data-testid="environments-bind-target">
        {row.name} <Pill size="sm" tone="slate">{row.backend}{strength === undefined ? "" : ` · ${strength}`}</Pill>
      </p>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.apply")}</h3>
        <FormField label={t("settings.environments.bind.scope")}>
          <Select variant="ghost" aria-label={t("settings.environments.bind.scope")} value={scope} onChange={(event) => setScope(event.target.value)} data-testid="environments-bind-scope">
            <option value={SCOPE_WORKSPACE}>{SCOPE_WORKSPACE}</option>
            <option value={SCOPE_COMPONENT}>{SCOPE_COMPONENT}</option>
          </Select>
        </FormField>
        {scope === SCOPE_COMPONENT && (
          <FormField label={t("settings.environments.bind.component")}>
            <Select variant="ghost" aria-label={t("settings.environments.bind.component")} value={componentId} onChange={(event) => setComponentId(event.target.value)} data-testid="environments-bind-component">
              <option value="">{t("settings.environments.bind.componentNone")}</option>
              {components.map((component) => <option key={component.id} value={component.id}>{component.name}</option>)}
            </Select>
          </FormField>
        )}
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.will")}</h3>
        {failure && (
          <div data-testid="environments-bind-failure">
            {/* The daemon's own sentence, verbatim — the preview could not be produced. */}
            <Alert intent="warning" title={t("settings.environments.bind.failed")} description={errorSentence(failure)} />
          </div>
        )}
        {statement ? (
          <p className="rounded-sm bg-[var(--surface-muted,rgba(127,127,127,0.12))] p-2 font-mono text-xs" data-testid="environments-bind-statement">{statement}</p>
        ) : (
          !failure && <p className="text-xs opacity-70" data-testid="environments-bind-pending">{t("settings.environments.bind.previewPending")}</p>
        )}
      </section>

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!canWrite || busy || statement === undefined} onClick={() => setConfirming(true)} data-testid="environments-bind-ask">{t("settings.environments.bind.ask")}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} data-testid="environments-bind-cancel">{t("settings.environments.actions.cancel")}</Button>
      </div>

      {confirming && (
        <ConfirmModal
          isOpen
          title={t("settings.environments.bind.confirmTitle", { name: row.name })}
          confirmLabel={t("settings.environments.bind.confirm")}
          cancelLabel={t("settings.environments.bind.decline")}
          onClose={() => setConfirming(false)} // declining sends NOTHING
          onConfirm={() => void apply()}
        >
          <p data-testid="environments-bind-confirm-statement">{statement}</p>
        </ConfirmModal>
      )}
    </div>
  );
}
