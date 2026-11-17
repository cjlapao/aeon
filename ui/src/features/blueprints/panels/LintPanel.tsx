import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Panel } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { rpcTyped, toRpcError, type RpcError } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";
import type { BlueprintLintResponse } from "../blueprintsModel";

// LintPanel.tsx — the advisory half of BP8: what the tools catalog would say
// about this blueprint's lanes, as of the instant it was asked.
//
// Lint runs daemon-side — over the STORED row, or over draft content when the
// person asks about a draft before saving it. Either way the advisories are the
// daemon's, reasons quoted rather than paraphrased, because the sentence came
// from a policy verdict and a reworded verdict is a second verdict.
//
// The three answers stay separate: advisories, none, and UNAVAILABLE. A daemon
// with no tools catalog wired says so in words (`unavailable`); an empty list
// without that sentence is a genuinely quiet answer. Collapsing the two into an
// all-clear is the lie BP8 names, and it is the quiet kind.

export interface LintPanelProps {
  name: string;
  /** Draft content to ask about instead of the stored row (BP8's other door). */
  content?: string;
  /** Whether the person may ask at all (a reconnecting app cannot). */
  canRun?: boolean;
}

interface LintState {
  loading: boolean;
  data?: BlueprintLintResponse;
  error?: RpcError;
}

export function LintPanel({ name, content, canRun = true }: LintPanelProps) {
  const { t } = useI18n();
  const [state, setState] = useState<LintState>({ loading: false });

  const run = async () => {
    setState({ loading: true, data: state.data });
    try {
      // Draft content travels as `content`; without it the daemon lints the
      // stored bytes. `name` labels the lanes either way.
      const params =
        content === undefined
          ? { name }
          : ({ name, content } as RpcMethods["blueprint.lint"]["params"] & { content: string });
      const result = await rpcTyped("blueprint.lint", params);
      setState({ loading: false, data: result });
    } catch (cause) {
      setState({ loading: false, error: toRpcError(cause) });
    }
  };

  const data = state.data;
  return (
    <Panel title={t("blueprints.lint.title")} subtitle={t("blueprints.lint.subtitle")} data-testid="lint-panel">
      <div className="flex items-center justify-between gap-3 pb-2">
        <span className="text-xs text-slate-400">{t("blueprints.lint.advisory")}</span>
        <Button size="sm" variant="outline" disabled={!canRun || state.loading} onClick={() => void run()} data-testid="run-lint">
          {state.loading ? t("blueprints.lint.loading") : t("blueprints.lint.run")}
        </Button>
      </div>

      {state.loading ? (
        <EmptyState title={t("blueprints.lint.loading")} />
      ) : state.error ? (
        <ErrorPanel error={state.error} onRetry={() => void run()} />
      ) : !data ? (
        <EmptyState title={t("blueprints.lint.idle")} />
      ) : data.unavailable ? (
        <Alert
          intent="warning"
          title={t("blueprints.lint.unavailableTitle")}
          description={data.unavailable}
          data-testid="lint-unavailable"
        />
      ) : data.advisories.length === 0 ? (
        <EmptyState title={t("blueprints.lint.none")} subtitle={asOf(t, data.evaluatedAt)} />
      ) : (
        <div className="space-y-2" data-testid="lint-advisories">
          <div className="text-xs text-slate-400" data-testid="lint-as-of">
            {asOf(t, data.evaluatedAt)}
          </div>
          {data.advisories.map((advisory, index) => (
            <div
              key={`${advisory.lane}-${advisory.tool}-${index}`}
              className="rounded-lg border border-white/10 bg-white/5 px-3 py-2"
              data-testid={`lint-advisory-${index}`}
            >
              <div className="flex flex-wrap items-center gap-2 text-xs text-slate-300">
                <span className="font-medium text-slate-100">{advisory.tool}</span>
                <span className="text-slate-500">{`${advisory.lane}/${advisory.step}`}</span>
                <span>{advisory.agent}</span>
                <span data-testid={`lint-state-${index}`}>{advisory.state}</span>
                {advisory.kind && <span className="text-slate-400">{advisory.kind}</span>}
                <RelativeTime iso={advisory.evaluatedAt} prefix={t("blueprints.lint.evaluated")} />
              </div>
              <p className="mt-1 text-sm text-slate-200" data-testid={`lint-reason-${index}`}>
                {advisory.reason}
              </p>
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}

/** "as of 3m ago" — the evaluation instant is part of the answer, so it is
 *  never dropped; the exact value rides the hover title of the row it stamps. */
function asOf(
  t: (key: string, values?: Record<string, string | number | undefined>) => string,
  evaluatedAt: string,
): string {
  return evaluatedAt ? t("blueprints.lint.asOf", { evaluatedAt }) : t("blueprints.lint.noTimestamp");
}
