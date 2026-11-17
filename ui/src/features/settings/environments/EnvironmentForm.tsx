import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill, Select, Toggle } from "../../../components/kit";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useSecretRefList, writeEnvironmentCreate, writeEnvironmentUpdate } from "./environmentsRpc";
import type { LiveController, LiveState } from "../../../lib/live";
import {
  backendIsPartial,
  backends,
  defaultAllowlist,
  draftFromSpec,
  egressModes,
  emptyDraft,
  homeModes,
  listOf,
  pullPolicies,
  specJson,
  type DisplayRow,
  type SpecDraft,
  type Vocabulary,
} from "./environmentsModel";

/**
 * The environment editor (S59 T11), generated from `environment.vocabulary`:
 * the backend select is the daemon's backend list with the strength number
 * beside each name, `passthrough` is chips over the daemon's default allowlist
 * with that allowlist one click away, the mode selects are the daemon's closed
 * sets, and the secret-reference picker offers NAMES from `secretref.list`.
 *
 * There is **no value field** in this form and none anywhere in the section: a
 * secret enters the product through `secretref.create`'s location, and the
 * source grep guard in `test/environmentsRpc.guard.test.ts` fails the build on
 * a field that could hold one.
 *
 * A refusal is rendered with the daemon's own parts and NOTHING is cleared:
 * `draft` is the person's text until the daemon accepts it. `expectedRevision`
 * is carried on an edit, so a stale save is refused rather than overwriting
 * what someone else wrote.
 */
export interface EnvironmentFormProps {
  mode: "add" | "edit";
  row?: DisplayRow;
  vocabulary: Vocabulary | undefined;
  canWrite: boolean;
  busy: boolean;
  controller: LiveController;
  live: LiveState;
  /** Called with the daemon's answer; the caller updates the row from IT. */
  onSaved: (row: DisplayRow) => void;
  onClose: () => void;
}

export function EnvironmentForm({
  mode,
  row,
  vocabulary,
  canWrite,
  busy,
  controller,
  live,
  onSaved,
  onClose,
}: EnvironmentFormProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const references = useSecretRefList({ sourceKind: "", limit: 200 }, controller, live);

  const [name, setName] = useState(row?.name ?? "");
  const [slug, setSlug] = useState(row?.slug ?? "");
  const [backend, setBackend] = useState(row?.backend ?? backends(vocabulary)[0]?.name ?? "");
  const [draft, setDraft] = useState<SpecDraft>(() =>
    row ? draftFromSpec(row.spec, vocabulary) : emptyDraft(vocabulary),
  );

  const allowed = defaultAllowlist(vocabulary);
  const partial = backendIsPartial(vocabulary, backend);
  const knownNames = new Set(draft.passthrough);
  const secretNames = listOf(references.data);

  const togglePassthrough = (word: string) => {
    setDraft((held) => ({
      ...held,
      passthrough: held.passthrough.includes(word)
        ? held.passthrough.filter((entry) => entry !== word)
        : [...held.passthrough, word],
    }));
  };

  const save = async () => {
    const spec = specJson(draft);
    const result = await action.run(
      () =>
        mode === "add"
          ? writeEnvironmentCreate({ name, slug, backend, spec })
          : writeEnvironmentUpdate({ id: row?.id ?? "", spec, expectedRevision: row?.revision ?? 0 }),
      { errorTitle: t("settings.environments.form.failed") },
    );
    // A refusal leaves every field above exactly as it was (VG20's rule, and
    // the reason `draft` lives in this component and nowhere else).
    if (result) onSaved(result);
  };

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const off = !canWrite || busy;

  return (
    <div className="space-y-8" data-testid="environment-form">
      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.who")}</h3>
        <div data-testid="environment-form-name">
          <FormField label={t("settings.environments.form.name")}>
            <Input variant="ghost" aria-label={t("settings.environments.form.name")} value={name} onChange={(event) => setName(event.target.value)} disabled={mode === "edit"} />
          </FormField>
        </div>
        <div data-testid="environment-form-slug">
          <FormField label={t("settings.environments.form.slug")}>
            <Input variant="ghost" aria-label={t("settings.environments.form.slug")} value={slug} onChange={(event) => setSlug(event.target.value)} disabled={mode === "edit"} />
          </FormField>
        </div>
        <div data-testid="environment-form-backend">
          <FormField label={t("settings.environments.form.backend")}>
            <Select variant="ghost" aria-label={t("settings.environments.form.backend")} value={backend} onChange={(event) => setBackend(event.target.value)}>
              {backends(vocabulary).map((entry) => <option key={entry.name} value={entry.name}>{entry.name} · {entry.strength}</option>)}
            </Select>
          </FormField>
        </div>
        {partial && (
          <div data-testid="environment-form-partial">
            <Alert intent="warning" title={t("settings.environments.form.partialBackend")} description={t("settings.environments.form.partialBody")} />
          </div>
        )}
        {mode === "edit" && <p className="text-xs opacity-70" data-testid="environment-form-revision">{t("settings.environments.form.revision")}: {row?.revision}</p>}
      </section>

      <section className="space-y-3" data-testid="environment-form-passthrough">
        <h3 className={H}>{t("settings.environments.q.vars")}</h3>
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-xs opacity-70">{t("settings.environments.form.passthrough")}</p>
          <Button size="xs" variant="ghost" onClick={() => setDraft({ ...draft, passthrough: [...allowed] })} data-testid="environment-form-default-allowlist">{t("settings.environments.form.useDefaults")}</Button>
        </div>
        <div className="flex flex-wrap gap-1">
          {allowed.map((word) => (
            <button type="button" key={word} onClick={() => togglePassthrough(word)} className="rounded-full border border-[var(--border)] px-2 py-0.5 text-xs" data-testid={`environment-form-chip-${word}`} data-on={knownNames.has(word) ? "true" : "false"}>
              {knownNames.has(word) ? "✓ " : ""}{word}
            </button>
          ))}
          {draft.passthrough.filter((word) => !allowed.includes(word)).map((word) => (
            <button type="button" key={word} onClick={() => togglePassthrough(word)} className="rounded-full border border-[var(--border)] px-2 py-0.5 text-xs" data-testid={`environment-form-chip-${word}`}>✓ {word}</button>
          ))}
        </div>
        <RowEditor
          label={t("settings.environments.form.set")}
          addLabel={t("settings.environments.form.addPair")}
          namePlaceholder={t("settings.environments.form.setName")}
          valuePlaceholder={t("settings.environments.form.setContent")}
          testid="environment-form-set"
          rows={draft.set}
          disabled={off}
          onChange={(set) => setDraft({ ...draft, set })}
        />
      </section>

      <section className="space-y-3" data-testid="environment-form-secrets">
        <h3 className={H}>{t("settings.environments.q.secrets")}</h3>
        <p className="text-xs opacity-70">{t("settings.environments.form.secretsNote")}</p>
        <ul className="space-y-2">
          {secretNames.map((reference) => (
            <li key={reference.name} className="flex flex-wrap items-center gap-2" data-testid={`environment-form-secret-${reference.name}`}>
              <Toggle
                size="sm"
                aria-label={reference.name}
                label={reference.name}
                checked={draft.secrets.includes(reference.name)}
                onChange={(event) => setDraft({ ...draft, secrets: event.target.checked ? [...draft.secrets, reference.name] : draft.secrets.filter((entry) => entry !== reference.name) })}
              />
              <Pill size="sm" tone="slate">{reference.injectAs}</Pill>
            </li>
          ))}
        </ul>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.net")}</h3>
        <div data-testid="environment-form-egress">
          <FormField label={t("settings.environments.form.egress")}>
            <Select variant="ghost" aria-label={t("settings.environments.form.egress")} value={draft.egressMode} onChange={(event) => setDraft({ ...draft, egressMode: event.target.value })}>
              {egressModes(vocabulary).map((mode2) => <option key={mode2} value={mode2}>{mode2}</option>)}
            </Select>
          </FormField>
        </div>
        <div data-testid="environment-form-home">
          <FormField label={t("settings.environments.form.home")}>
            <Select variant="ghost" aria-label={t("settings.environments.form.home")} value={draft.home} onChange={(event) => setDraft({ ...draft, home: event.target.value })}>
              {homeModes(vocabulary).map((mode2) => <option key={mode2} value={mode2}>{mode2}</option>)}
            </Select>
          </FormField>
        </div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.limits")}</h3>
        <RowEditor
          label={t("settings.environments.form.limits")}
          addLabel={t("settings.environments.form.addLimit")}
          namePlaceholder={t("settings.environments.form.limitKey")}
          valuePlaceholder={t("settings.environments.form.limitValue")}
          testid="environment-form-limits"
          rows={draft.limits.map((limit) => ({ name: limit.name, content: limit.shown }))}
          disabled={off || partial}
          onChange={(limits) => setDraft({ ...draft, limits: limits.map((entry) => ({ name: entry.name, shown: entry.content })) })}
          note={partial ? t("settings.environments.form.notEnforced") : undefined}
        />
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.environments.q.image")}</h3>
        <div data-testid="environment-form-image-ref">
          <FormField label={t("settings.environments.form.imageRef")}>
            <Input variant="ghost" aria-label={t("settings.environments.form.imageRef")} value={draft.imageRef} disabled={off || partial} onChange={(event) => setDraft({ ...draft, imageRef: event.target.value })} />
          </FormField>
        </div>
        <div data-testid="environment-form-image-pull">
          <FormField label={t("settings.environments.form.imagePull")}>
            <Select variant="ghost" aria-label={t("settings.environments.form.imagePull")} value={draft.imagePull} disabled={off || partial} onChange={(event) => setDraft({ ...draft, imagePull: event.target.value })}>
              {pullPolicies(vocabulary).map((policy) => <option key={policy} value={policy}>{policy}</option>)}
            </Select>
          </FormField>
        </div>
        <RowEditor
          label={t("settings.environments.form.services")}
          addLabel={t("settings.environments.form.addService")}
          namePlaceholder={t("settings.environments.form.serviceName")}
          valuePlaceholder={t("settings.environments.form.serviceImage")}
          testid="environment-form-services"
          rows={draft.services.map((service) => ({ name: service.name, content: service.image, port: service.port }))}
          disabled={off || partial}
          onChange={(rows) => setDraft({ ...draft, services: rows.map((entry) => ({ name: entry.name, image: entry.content, port: entry.port ?? "" })) })}
          note={partial ? t("settings.environments.form.notEnforced") : undefined}
        />
      </section>

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={off} onClick={() => void save()} data-testid="environment-form-save">{busy ? t("settings.environments.form.saving") : t("settings.environments.form.save")}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} data-testid="environment-form-cancel">{t("settings.environments.actions.cancel")}</Button>
      </div>
    </div>
  );
}

interface EditableRow {
  name: string;
  content: string;
  port?: string;
}

/** A pair list the person can add to and remove from — names typed here are refused by the daemon, verbatim. */
function RowEditor({ label, addLabel, namePlaceholder, valuePlaceholder, testid, rows, disabled, note, onChange }: {
  label: string;
  addLabel: string;
  namePlaceholder: string;
  valuePlaceholder: string;
  testid: string;
  rows: readonly EditableRow[];
  disabled: boolean;
  note?: string;
  onChange: (rows: EditableRow[]) => void;
}) {
  const update = (index: number, patch: Partial<EditableRow>) => onChange(rows.map((held, at) => (at === index ? { ...held, ...patch } : held)));
  return (
    <div className="space-y-2" data-testid={testid}>
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-xs opacity-70">{label}</p>
        <Button size="xs" variant="ghost" disabled={disabled} onClick={() => onChange([...rows, { name: "", content: "" }])} data-testid={`${testid}-add`}>{addLabel}</Button>
      </div>
      {note && <p className="text-[11px] italic opacity-70" data-testid={`${testid}-note`}>{note}</p>}
      {rows.map((entry, index) => (
        <div key={index} className="flex flex-wrap items-center gap-2" data-testid={`${testid}-row-${index}`}>
          <div className="min-w-32 flex-1"><Input variant="ghost" aria-label={`${label} · ${namePlaceholder}`} value={entry.name} disabled={disabled} placeholder={namePlaceholder} onChange={(event) => update(index, { name: event.target.value })} /></div>
          <div className="min-w-32 flex-1"><Input variant="ghost" aria-label={`${label} · ${valuePlaceholder}`} value={entry.content} disabled={disabled} placeholder={valuePlaceholder} onChange={(event) => update(index, { content: event.target.value })} /></div>
          {entry.port !== undefined && <div className="w-24"><Input variant="ghost" aria-label={`${label} · port`} value={entry.port} disabled={disabled} onChange={(event) => update(index, { port: event.target.value })} /></div>}
          <Button size="xs" variant="ghost" disabled={disabled} onClick={() => onChange(rows.filter((_, held) => held !== index))} data-testid={`${testid}-remove-${index}`}>×</Button>
        </div>
      ))}
    </div>
  );
}
