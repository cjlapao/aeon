import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, FormField, Pill, Select } from "../../../components/kit";
import { allowedValuesForKey, applicableKeys, buildMatch, type TestAnswer, type DryRunAnswer, type Vocabulary } from "./automationModel";
import { runDryMatch, runMatchTest } from "./automationRpc";

/**
 * MatchBuilder — the event match, assembled from the vocabulary only
 * (AN9/AN11): the event select comes from `automation.vocabulary`, and the
 * keys shown are **only the ones the chosen event declares applicable**.
 * An inapplicable key could only ever match nothing, so offering one is a
 * lie about the grammar — the UI structurally cannot offer it.
 *
 * Values are chips chosen from the vocabulary's closed sets (task types,
 * goal types): free text here would be a value no event can ever carry.
 * The match this builds is the object `automation.trigger.create` takes;
 * whether it is VALID is settled by the one validator on the daemon.
 */
export interface MatchBuilderProps {
  event: string;
  values: Record<string, string[]>;
  vocabulary: Vocabulary | undefined;
  onEvent: (event: string) => void;
  onValues: (values: Record<string, string[]>) => void;
}

export function MatchBuilder({ event, values, vocabulary, onEvent, onValues }: MatchBuilderProps) {
  const { t } = useI18n();
  const keys = applicableKeys(event, vocabulary);
  const meaning = vocabulary?.events.find((e) => e.event === event)?.meaning;

  const toggle = (key: string, entry: string) => {
    const held = values[key] ?? [];
    const next = held.includes(entry) ? held.filter((v) => v !== entry) : [...held, entry];
    onValues({ ...values, [key]: next });
  };

  return (
    <div className="space-y-3" data-testid="match-builder">
      <FormField label={t("settings.automation.form.event")}>
        <Select
          variant="ghost"
          aria-label={t("settings.automation.form.event")}
          value={event}
          onChange={(e) => {
            // Changing the event invalidates every value picked under the
            // previous event's keys — the sets are per-event by definition.
            onEvent(e.target.value);
            onValues({});
          }}
        >
          <option value="">—</option>
          {(vocabulary?.events ?? []).map((entry) => (
            <option key={entry.event} value={entry.event}>{entry.event}</option>
          ))}
        </Select>
      </FormField>
      {meaning !== undefined && <p className="text-xs opacity-60">{meaning}</p>}

      {event !== "" && keys.map((key) => (
        <MatchKeyChips
          key={key}
          eventKey={key}
          selected={values[key] ?? []}
          options={allowedValuesForKey(key, vocabulary)}
          onToggle={(entry) => toggle(key, entry)}
        />
      ))}
    </div>
  );
}

function MatchKeyChips({ eventKey, selected, options, onToggle }: {
  eventKey: string;
  selected: string[];
  options: string[];
  onToggle: (entry: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-1" data-testid={`match-key-${eventKey}`}>
      <p className="text-xs uppercase tracking-wide opacity-70">
        {eventKey} <span className="normal-case opacity-60">— {t("settings.automation.match.emptyMeansAny")}</span>
      </p>
      <div className="flex flex-wrap gap-1.5">
        {options.map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={selected.includes(option)}
            className={`rounded-full border px-2.5 py-0.5 text-xs ${selected.includes(option) ? "border-sky-400 bg-sky-400/10" : "border-slate-300 dark:border-slate-700"}`}
            onClick={() => onToggle(option)}
          >
            {option}
          </button>
        ))}
        {selected.length > 0 && (
          <Pill tone="blue">{selected.length} {t("settings.automation.match.picked")}</Pill>
        )}
      </div>
    </div>
  );
}

/**
 * MatchTester — the sample tester (AN11): the match, a sample event, and the
 * daemon's per-key ✓/✗ answer with its want/have lines verbatim. No client
 * matching anywhere: "does it match?" is a door question (`automation.test`)
 * and this only renders the door's answer.
 */
export function MatchTester({ event, values, vocabulary }: {
  event: string;
  values: Record<string, string[]>;
  vocabulary: Vocabulary | undefined;
}) {
  const { t } = useI18n();
  const [taskType, setTaskType] = useState("");
  const [answer, setAnswer] = useState<TestAnswer | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const match = useMemo(() => buildMatch(event, values), [event, values]);

  const test = async () => {
    setError(undefined);
    try {
      setAnswer(await runMatchTest(match, taskType === "" ? { event } : { event, taskType }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setAnswer(undefined);
    }
  };

  return (
    <div className="space-y-2" data-testid="match-tester">
      <p className="text-xs opacity-70">{t("settings.automation.tester.title")}</p>
      <FormField label={t("settings.automation.tester.sampleTaskType")}>
        <Select
          variant="ghost"
          aria-label={t("settings.automation.tester.sampleTaskType")}
          value={taskType}
          onChange={(e) => setTaskType(e.target.value)}
        >
          <option value="">—</option>
          {(vocabulary?.taskTypes ?? []).map((option) => (
            <option key={option} value={option}>{option}</option>
          ))}
        </Select>
      </FormField>
      <Button size="sm" variant="ghost" disabled={event === ""} onClick={() => void test()} data-testid="match-test-run">
        {t("settings.automation.tester.run")}
      </Button>
      {error !== undefined && <p className="text-xs text-rose-500" data-testid="match-test-error">{error}</p>}
      {answer !== undefined && (
        <div className="space-y-1" data-testid="match-test-answer">
          {answer.matches ? (
            <Pill size="sm" tone="green" data-testid="match-test-matches">{t("settings.automation.tester.matches")}</Pill>
          ) : (
            <Pill size="sm" tone="red" data-testid="match-test-nomatch">{t("settings.automation.tester.noMatch")}</Pill>
          )}
          <p className="font-mono text-xs opacity-70" data-testid="match-test-canonical">{answer.canonical}</p>
          {answer.perKey.map((line) => (
            <p key={line.key} className="text-xs" data-testid={`match-perkey-${line.key}`}>
              {line.ok ? "✓" : "✗"} {line.key}: want [{line.want.join(", ")}] have [{line.have}]
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

/** The dry-run block lives where an id exists (edit form / drawer): it needs
 *  a STORED event trigger to scan the recent outbox against, which a
 *  brand-new draft does not have. The counts and list are the daemon's. */
export function DryRunBlock({ id }: { id: string }) {
  const { t } = useI18n();
  const [answer, setAnswer] = useState<DryRunAnswer | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);

  const run = async () => {
    setError(undefined);
    try {
      setAnswer(await runDryMatch(id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-1" data-testid="dryrun-block">
      <Button size="sm" variant="ghost" onClick={() => void run()} data-testid="dryrun-run">
        {t("settings.automation.dryrun.run")}
      </Button>
      {error !== undefined && <p className="text-xs text-rose-500">{error}</p>}
      {answer !== undefined && (
        <div data-testid="dryrun-answer">
          <p className="text-xs">{t("settings.automation.dryrun.summary")}: {answer.wouldMatch.length}/{answer.scanned}</p>
          {answer.wouldMatch.map((row) => (
            <p key={row.outboxId} className="font-mono text-xs opacity-70">
              {row.at} {row.event} {row.subjectKind}:{row.subjectId}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}
