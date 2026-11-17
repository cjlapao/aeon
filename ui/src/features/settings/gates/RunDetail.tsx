import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Pill, formatBytes } from "../../../components/kit";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RunResultView } from "./gatesModel";
import { pillTone, wallLine } from "./gatesModel";

/**
 * One run's detail (S57 T11): `verify.runShow`, mounted only when a ledger
 * row is selected, so the door opens on the run the person is looking at.
 * The per-gate results render as the runner recorded them — exit code,
 * wall time, TRUE byte counts, digest — and the tails, when they exist,
 * are behind an explicit disclosure with the honest notice: they are the
 * runner's scrubbed best-effort excerpt, they can still contain secrets,
 * and when `truncated` is set the counts are the truth and the tail is an
 * excerpt. Nothing here is stored client-side; closing the row closes the
 * output.
 */
export interface RunDetailProps {
  runId: string;
  states: readonly string[];
  controller: LiveController;
  live: LiveState;
}

const STATE_TONES = ["emerald", "rose", "amber", "slate"] as const;

export function RunDetail({ runId, states, controller, live }: RunDetailProps) {
  const { t } = useI18n();
  const detail = useRpcQuery("verify.runShow", { id: runId }, controller, live);

  if (detail.error) {
    return (
      <div className="" data-testid={`run-detail-error-${runId}`}>
        <Alert intent="danger" title={t("settings.gates.runs.detailFailed")} description={detail.error} />
      </div>
    );
  }
  if (!detail.data) {
    return <p className="text-sm opacity-70" data-testid={`run-detail-loading-${runId}`}>{t("settings.gates.runs.loading")}</p>;
  }

  const run = detail.data.run;
  return (
    <div className="space-y-8" data-testid={`run-detail-${runId}`}>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs opacity-90">
        <span data-testid={`run-detail-trigger-${runId}`}>{t(`settings.gates.word.${run.trigger}`)}</span>
        <span data-testid={`run-detail-actor-${runId}`}>{`${run.actorKind}:${run.actorId}`}</span>
        <span className="font-mono" data-testid={`run-detail-head-${runId}`}>{run.headSha}</span>
        {run.reason && <span data-testid={`run-detail-reason-${runId}`}>{run.reason}</span>}
        {run.digest && <span className="font-mono" data-testid={`run-detail-digest-${runId}`}>{run.digest}</span>}
      </div>
      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.gates.runs.gateResults")}</h3>
        <RunResults results={detail.data.results} states={states} />
      </section>
    </div>
  );
}

/**
 * The per-gate result rows, shared by the run detail and the try panel —
 * one `VerifyRunResultView` struct answers both doors.
 */
export function RunResults({ results, states }: { results: readonly RunResultView[]; states: readonly string[] }) {
  const { t } = useI18n();
  return (
    <ul className="divide-y divide-[var(--border)]" data-testid="run-results">
      {results.map((result) => (
        <li key={`${result.seq}`} className="space-y-1 py-3 text-sm" data-testid={`run-result-${result.seq}`}>
          <div className="flex flex-wrap items-center gap-2">
            <span className="opacity-70">#{result.seq}</span>
            <span className="font-medium">{result.name}</span>
            <Pill size="sm" tone="slate">{t(`settings.gates.word.${result.requirement}`)}</Pill>
            <Pill size="sm" tone={pillTone(states, result.state, STATE_TONES)} data-testid={`run-result-state-${result.seq}`}>
              {t(`settings.gates.word.${result.state}`)}
            </Pill>
            <span className="ml-auto flex flex-wrap gap-x-3 text-xs opacity-80">
              {result.timedOut && <span data-testid={`run-result-timeout-${result.seq}`}>{t("settings.gates.runs.timedOut")}</span>}
              <span data-testid={`run-result-exit-${result.seq}`}>{t("settings.gates.runs.exit", { code: result.exitCode })}</span>
              <span>{wallLine(result.wallMs)}</span>
              <span data-testid={`run-result-bytes-${result.seq}`}>
                {t("settings.gates.runs.bytes", { out: formatBytes(result.stdoutBytes), err: formatBytes(result.stderrBytes) })}
              </span>
            </span>
          </div>
          <p className="font-mono text-xs opacity-70">{result.command}</p>
          {result.reason && <p className="text-xs italic opacity-80" data-testid={`run-result-reason-${result.seq}`}>{result.reason}</p>}
          {(result.stdoutTail || result.stderrTail) && (
            <details data-testid={`run-result-tails-${result.seq}`}>
              <summary className="cursor-pointer text-xs opacity-80">{t("settings.gates.runs.showTails")}</summary>
              <p className="my-1 text-xs font-medium" data-testid={`run-result-secret-${result.seq}`}>
                {t("settings.gates.runs.secretNotice")}
              </p>
              {result.truncated && (
                <p className="text-xs opacity-80" data-testid={`run-result-truncated-${result.seq}`}>
                  {t("settings.gates.runs.truncated")}
                </p>
              )}
              <pre className="max-h-64 overflow-auto rounded bg-slate-100 p-2 text-xs dark:bg-slate-900" data-testid={`run-result-stdout-${result.seq}`}>
                {result.stdoutTail ?? ""}
              </pre>
              {result.stderrTail ? (
                <pre className="max-h-64 overflow-auto rounded bg-slate-100 p-2 text-xs dark:bg-slate-900" data-testid={`run-result-stderr-${result.seq}`}>
                  {result.stderrTail}
                </pre>
              ) : null}
            </details>
          )}
        </li>
      ))}
    </ul>
  );
}
