import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Panel, Pill } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { useHistoryShow } from "./historyRpc";
import { drilldownBody, type ScopeDraft } from "./historyModel";

/**
 * HistoryTranscript — the drill-down, `history.show` for ONE entry (S53 T5,
 * HV7; the drill-down S25 T2 built).
 *
 * The entry is read through its OWN door, and the answer says which door it
 * was: a live entry's `transcript` (the message sequence, oldest first), an
 * archived entry's cold-stored `files`, or a `refused` sentence travelling
 * verbatim (DB19 — a wrong-door entry is never read as an empty one, because
 * an archived loop rendered as "no transcript" would be a lie).
 *
 * So the four shapes are four renderings, never one collapsing into "empty":
 * loading, refused (verbatim), transcript, files — and the honest absence
 * when the answer genuinely carried neither.
 */
export interface HistoryTranscriptProps {
  id: string;
  draft: ScopeDraft;
  controller: LiveController;
  live: LiveState;
  onClose: () => void;
}

export function HistoryTranscript({ id, draft, controller, live, onClose }: HistoryTranscriptProps) {
  const { t } = useI18n();
  const show = useHistoryShow(id, draft, controller, live);
  const body = drilldownBody(show.data);

  return (
    <Panel
      title={t("history.manage.drilldown.title")}
      subtitle={id}
      tone="blue"
      data-testid="history-drilldown"
      data-drilldown-body={show.error !== undefined ? "refused" : body.kind}
    >
      <div className="space-y-3">
        {show.error !== undefined && (
          // The daemon's refusal, whole: `what` it refused, and the parts it
          // sent with it. An id that names nothing is said, not shrugged at.
          <Alert intent="danger" title={t("history.manage.drilldown.refused")} description={show.error} data-testid="history-drilldown-refused" />
        )}

        {show.error === undefined && show.loading && (
          <p className="text-sm text-slate-500" data-testid="history-drilldown-loading">
            {t("history.manage.drilldown.loading")}
          </p>
        )}

        {show.error === undefined && body.kind === "transcript" && (
          <ol className="space-y-2" data-testid="history-transcript">
            {body.messages.map((message, index) => (
              <li key={`${message.role}-${index}`} className="rounded-lg border border-slate-300/20 p-2 dark:border-white/10" data-testid={`history-message-${index}`}>
                <Pill tone="slate" data-testid={`history-message-role-${index}`}>
                  {message.role}
                </Pill>
                <div className="mt-1 space-y-1">
                  {message.content.map((part, partIndex) => (
                    <p
                      key={partIndex}
                      className={`whitespace-pre-wrap text-sm ${part.isError ? "text-red-500" : "text-slate-600 dark:text-slate-300"}`}
                      data-part-error={part.isError}
                      data-testid={`history-part-${index}-${partIndex}`}
                    >
                      {part.text}
                    </p>
                  ))}
                </div>
              </li>
            ))}
          </ol>
        )}

        {show.error === undefined && body.kind === "files" && (
          <ul className="space-y-1 text-sm" data-testid="history-files">
            {body.files.map((file) => (
              <li key={file.path} data-testid={`history-file-${file.path}`}>
                {file.path} · {file.bytes} · {file.sha256}
              </li>
            ))}
          </ul>
        )}

        {show.error === undefined && body.kind === "refused" && (
          <Alert intent="warning" title={t("history.manage.drilldown.refused")} description={body.sentence} data-testid="history-drilldown-answer-refused" />
        )}

        {show.error === undefined && body.kind === "nothing" && !show.loading && (
          <p className="text-sm text-slate-500" data-testid="history-drilldown-empty">
            {t("history.manage.drilldown.nothing")}
          </p>
        )}

        <button type="button" onClick={onClose} className="text-xs underline aeon-accent-text" data-testid="history-drilldown-close">
          {t("history.manage.drilldown.close")}
        </button>
      </div>
    </Panel>
  );
}
