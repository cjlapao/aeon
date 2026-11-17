import { lazy, Suspense } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import type { Components } from "react-markdown";
import { CodeBlock } from "../../components/CodeBlock";
import { CodeBlock as KitCodeBlock, FormField, InfoRow, Input, Panel, Pill, Select, Textarea } from "../../components/kit";
import { MinionAvatar, type MinionMood } from "../../components/minion-avatar";
import {
  agentMinion,
  type AgentDetailRow,
  type AgentDraft,
  type AgentFieldSpec,
  type AgentStringField,
  type AgentTyping,
} from "./agentsModel";

// How an agent row is displayed: the field-spec-driven form (the specs live in
// agentsModel.ts, so create and edit cannot grow two field lists), the tab
// bodies the side panel shows (identity, prompt in markdown, capabilities,
// metadata with the proving bytes), and the detail renderer a create/import
// answer prints. Selects show the stored word — the avatar vocabularies are
// the database's CHECKs, and translating one would show a word the row does
// not hold.

export interface AgentFieldsFormProps {
  /** Which fields to render: the nine for edit, the three for create. */
  fields: readonly AgentFieldSpec[];
  /** The current values, keyed as the row keys them. A missing key reads as empty. */
  values: AgentTyping;
  onChange: (key: AgentStringField, value: string) => void;
  disabled?: boolean;
  /** The row's own slug, for the one helper line that has to name it. */
  slug?: string;
  /** Draw the controls in the ghost variant, like the rest of the agent panels. */
  ghost?: boolean;
  testId?: string;
}

/** The nine (or the three), one `FormField` each, wired to the caller's draft — this component holds no draft of its own. */
export function AgentFieldsForm({
  fields,
  values,
  onChange,
  disabled = false,
  slug,
  ghost = false,
  testId = "agent-fields",
}: AgentFieldsFormProps) {
  const { t } = useI18n();
  return (
    <div className="space-y-3" data-testid={testId}>
      {fields.map((field) => {
        // The tools and handoff controls are drawn by their own pickers, not by this form.
        if (field.control === "tools" || field.control === "handoff") return null;
        const value = values[field.key] ?? "";
        const hint =
          field.hintKey === undefined ? undefined : t(field.hintKey, slug === undefined ? {} : { slug });
        const controlId = `agent-field-${field.key}`;
        return (
          <FormField
            key={field.key}
            label={t(field.labelKey)}
            labelFor={controlId}
            hint={hint}
            required={field.required}
            data-testid={`agent-field-${field.key}`}
          >
            {field.control === "text" && (
              <Input
                id={controlId}
                variant={ghost ? "ghost" : undefined}
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
              />
            )}
            {field.control === "textarea" && field.key === "metadata" && (
              // The metadata is JSON: the kit's code block edits it in place, coloured like the read view.
              <KitCodeBlock
                id={controlId}
                language="json"
                value={value}
                readOnly={disabled}
                onChange={disabled ? undefined : (next) => onChange(field.key, next)}
                showTitle={false}
                showCopy
                editorLabel={t(field.labelKey)}
                data-testid="agent-field-metadata-editor"
              />
            )}
            {field.control === "textarea" && field.key !== "metadata" && (
              <Textarea
                id={controlId}
                variant={ghost ? "ghost" : undefined}
                rows={4}
                spellCheck={false}
                className="font-mono text-xs"
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
              />
            )}
            {field.control === "select" && (
              <Select
                id={controlId}
                variant={ghost ? "ghost" : undefined}
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
              >
                {(field.options ?? []).map((word) => (
                  <option key={word === "" ? "(none)" : word} value={word}>
                    {word === "" ? t("agents.field.avatarNone") : word}
                  </option>
                ))}
              </Select>
            )}
          </FormField>
        );
      })}
    </div>
  );
}

/** The identity block every surface that names a row prints: avatar, name/slug, the three truth pills, and the signer sentence when the row carries one. */
type AgentIdentityFields = Pick<
  AgentDraft,
  "name" | "avatarType" | "avatarColor" | "avatarMood" | "slug" | "version" | "signed" | "internal" | "certificate" | "signature"
>;

export function AgentIdentity({ agent }: { agent: AgentIdentityFields }) {
  const { t } = useI18n();
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-3">
        <AgentAvatar agent={agent} />
        <div className="min-w-40">
          <p className="text-base font-semibold" data-testid="agent-detail-name">
            {agent.name}
          </p>
          <p className="font-mono text-xs text-slate-400" data-testid="agent-detail-slug">
            {agent.slug || "—"}
          </p>
        </div>
        <Pill tone="neutral" size="sm">
          v{agent.version || "—"}
        </Pill>
        <Pill tone={agent.signed ? "green" : "amber"} size="sm" data-testid="agent-detail-signed" data-signed={agent.signed}>
          {agent.signed ? t("agents.flag.signed") : t("agents.flag.unsigned")}
        </Pill>
        {agent.internal && (
          <Pill tone="violet" size="sm" data-testid="agent-detail-internal">
            {t("agents.flag.firstParty")}
          </Pill>
        )}
      </div>
      {agent.signed && (agent.certificate !== "" || agent.signature !== "") && (
        <p className="text-xs text-slate-500" data-testid="agent-detail-signer">
          {t("agents.detail.signer", {
            certificate: agent.certificate !== "" ? agent.certificate : t("agents.detail.signerNone"),
          })}
        </p>
      )}
    </div>
  );
}

/** The Overview tab of a row: the identity, the two sentences, and the stamps the daemon set. */
type AgentOverviewFields = AgentDetailRow & Pick<AgentDraft, "id" | "origin" | "createdAt" | "updatedAt">;

type OverviewAgent = AgentOverviewFields & {
  tools?: string;
  version?: string;
  signed?: boolean;
  skills?: string;
  handoff?: string;
  policyAgent?: string;
  temperature?: number;
  bodyHash?: string;
};

export function AgentOverviewView({ agent }: { agent: OverviewAgent }) {
  const { t } = useI18n();
  const hasTools = typeof agent.tools === "string" && agent.tools.trim() !== "" && agent.tools.trim() !== "[]";
  const hasFunctionality = (agent.functionality ?? "").trim() !== "";
  const skills = tryParse(agent.skills ?? "");
  const hasSkills = Array.isArray(skills) ? skills.length > 0 : (agent.skills ?? "").trim() !== "";
  const handoffParsed = tryParse(agent.handoff ?? "");
  const hasHandoff = (agent.handoff ?? "").trim() !== "" && !(handoffParsed !== null && typeof handoffParsed === "object" && Object.keys(handoffParsed).length === 0);
  // The page's header already says the name, the slug, what kind of row it is and what it does in a
  // sentence, so this tab says only what the header does not: what the agent can do, and the facts of
  // the row. A fact the daemon has no value for is left out, never shown as a dash.
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,360px)]" data-testid="agent-tab-overview">
      <Panel variant="simple" padding="md" clickable={false}>
        <h2 className="mb-3 text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.overview.canDo")}</h2>
        {hasFunctionality && <p className="whitespace-pre-wrap text-sm" data-testid="agent-tab-functionality">{agent.functionality}</p>}
        {hasTools && <div className={hasFunctionality ? "mt-3" : undefined}><ToolsView tools={agent.tools as string} /></div>}
        {hasSkills && (
          <div className="mt-4 space-y-1">
            <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.detail.skills")}</h3>
            <SkillsView skills={agent.skills as string} />
          </div>
        )}
        {hasHandoff && (
          <div className="mt-4 space-y-1">
            <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.readonly.handoff")}</h3>
            <HandoffView handoff={agent.handoff as string} />
          </div>
        )}
        {!hasFunctionality && !hasTools && !hasSkills && !hasHandoff && <p className="text-sm opacity-60" data-testid="agent-tab-functionality-empty">{t("agents.overview.nothingDeclared")}</p>}
      </Panel>
      <div className="space-y-4">
      <Panel variant="simple" padding="md" clickable={false}>
        <h2 className="mb-1 text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.overview.runsOn")}</h2>
        <div>
          <InfoRow label={t("agents.readonly.policyAgent")} labelWidth="w-28" variant="plain" mono hideIfEmpty={false} emptyText={t("agents.flag.none")} wrap value={agent.policyAgent ?? ""} data-testid="agent-tab-policyAgent" />
          <InfoRow label={t("agents.readonly.temperature")} labelWidth="w-28" variant="plain" hideIfEmpty={false} emptyText={t("agents.flag.none")} value={agent.temperature === undefined ? "" : String(agent.temperature)} data-testid="agent-tab-temperature" />
        </div>
      </Panel>
      <Panel variant="simple" padding="md" clickable={false}>
        <h2 className="mb-1 text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.overview.details")}</h2>
        <div>
          <InfoRow label={t("agents.readonly.version")} labelWidth="w-28" variant="plain" value={agent.version ? `v${agent.version}` : ""} data-testid="agent-tab-version" />
          {/* Cut at the row's edge with the whole hash in the tooltip; the copy button copies all of it. */}
          <InfoRow label={t("agents.readonly.bodyHash")} labelWidth="w-28" variant="plain" mono tooltipOnTruncate copyable value={agent.bodyHash ?? ""} data-testid="agent-tab-bodyHash" />
          <InfoRow label={t("agents.readonly.updatedAt")} labelWidth="w-28" variant="plain" mono wrap value={agent.updatedAt} data-testid="agent-tab-updatedAt" />
          <InfoRow label={t("agents.readonly.createdAt")} labelWidth="w-28" variant="plain" mono wrap value={agent.createdAt} data-testid="agent-tab-createdAt" />
          <InfoRow label={t("agents.readonly.id")} labelWidth="w-28" variant="plain" mono value={String(agent.id ?? "")} data-testid="agent-tab-id" />
        </div>
      </Panel>
      </div>
    </div>
  );
}

/** The tools the row declares: a JSON list becomes pills, anything else prints verbatim. */
export function ToolsView({ tools }: { tools: string }) {
  const { t } = useI18n();
  if (tools === undefined || tools === null || String(tools).trim() === "") {
    return <p className="text-sm text-slate-500" data-testid="agent-tab-tools-empty">{t("agents.readonly.toolsEmpty")}</p>;
  }
  const parsed = tryParse(tools);
  if (!Array.isArray(parsed)) {
    return <CodeBlock code={String(tools)} language="plain" data-testid="agent-tab-tools-raw" />;
  }
  const words: string[] = [];
  for (const item of parsed as unknown[]) {
    if (typeof item === "string") words.push(item);
    else if (item !== null && typeof item === "object") {
      const named = item as { name?: string; id?: string };
      words.push(named.name ?? named.id ?? JSON.stringify(item));
    }
  }
  if (words.length === 0) {
    return <p className="text-sm text-slate-500" data-testid="agent-tab-tools-empty">{t("agents.readonly.toolsEmpty")}</p>;
  }
  return (
    <div className="flex flex-wrap gap-1" data-testid="agent-tab-tools">
      {words.map((word) => (
        <Pill key={word} tone="neutral" size="sm">
          {word}
        </Pill>
      ))}
    </div>
  );
}

/** The Capabilities tab of a row: what it can do (skills, tools), what it says on handoff, and the dispatch facts. While the skills field is being edited the list yields to the textarea (`skillsHidden`). */
export function AgentCapabilitiesView({ agent, skillsHidden = false, toolsHidden = false, handoffHidden = false }: { agent: AgentDraft; skillsHidden?: boolean; toolsHidden?: boolean; handoffHidden?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="space-y-4" data-testid="agent-tab-capabilities">
      {!skillsHidden && (
        <div className="space-y-1">
          <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.detail.skills")}</p>
          <SkillsView skills={agent.skills} />
        </div>
      )}
      {!toolsHidden && (
        <div className="space-y-1">
          <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.readonly.tools")}</p>
          <ToolsView tools={agent.tools} />
        </div>
      )}
      {!handoffHidden && (
        <div className="space-y-1">
          <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.readonly.handoff")}</p>
          <HandoffView handoff={agent.handoff} />
        </div>
      )}
      <div className="rounded-lg border border-white/10">
        <InfoRow label={t("agents.readonly.policyAgent")} labelWidth="w-44" variant="plain" mono hideIfEmpty={false} emptyText={t("agents.flag.none")} wrap value={agent.policyAgent} data-testid="agent-tab-policyAgent" />
        <InfoRow label={t("agents.readonly.temperature")} labelWidth="w-44" variant="plain" hideIfEmpty={false} emptyText={t("agents.flag.none")} value={String(agent.temperature)} data-testid="agent-tab-temperature" />
      </div>
    </div>
  );
}

/** The bytes that prove the row. Long by nature, so each prints in a block that scrolls on its own. */
export function AgentProvenance({ agent }: { agent: AgentDraft }) {
  const { t } = useI18n();
  const blocks: readonly { key: string; labelKey: string; value: string }[] = [
    { key: "signature", labelKey: "agents.readonly.signature", value: agent.signature },
    { key: "certificate", labelKey: "agents.readonly.certificate", value: agent.certificate },
    { key: "bodyHash", labelKey: "agents.readonly.bodyHash", value: agent.bodyHash },
  ];
  // InfoRow gives each proving value its own copy button; a long one wraps instead of scrolling sideways.
  return (
    <Panel variant="simple" padding="md" clickable={false}>
      {blocks.map(
        (block) =>
          block.value === "" ? null : (
            <InfoRow key={block.key} label={t(block.labelKey)} labelWidth="w-28" variant="plain" mono wrap copyable value={block.value} data-testid={`agent-provenance-${block.key}`} />
          ),
      )}
    </Panel>
  );
}

/** The Metadata tab of a row: the open blob, pretty-printed in a block that scrolls, beside the proving bytes. */
export function AgentMetadataView({ agent }: { agent: AgentDraft }) {
  const { t } = useI18n();
  return (
    <div className="space-y-4" data-testid="agent-tab-metadata">
      <div className="space-y-1">
        <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.detail.metadata")}</p>
        <div className="max-h-64 overflow-y-auto">
          <MetaView metadata={agent.metadata} />
        </div>
      </div>
      <AgentProvenance agent={agent} />
    </div>
  );
}

const KitMarkdownEditor = lazy(async () => {
  const mod = await import("@cjlapao/ui-kit/markdown");
  return { default: mod.MarkdownEditor };
});

// Read view: react-markdown builds React elements and ignores raw HTML, so an
// imported prompt cannot draw markup of its own. No rehype-raw, on purpose.
// A fenced block is colored by its language; an inline `code` stays inline.
const PROMPT_MARKDOWN: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const language = /language-([\w-]+)/.exec(className ?? "")?.[1];
    if (language === undefined && !text.includes("\n")) return <code>{children}</code>;
    return <CodeBlock code={text} language={language ?? "plain"} />;
  },
};

const PromptView = lazy(async () => {
  const [{ default: ReactMarkdown }, { default: gfm }] = await Promise.all([import("react-markdown"), import("remark-gfm")]);
  return {
    default: ({ body }: { body: string }) => (
      <article className="aeon-markdown min-w-0 break-words" data-testid="agent-tab-prompt-view">
        <ReactMarkdown remarkPlugins={[gfm]} components={PROMPT_MARKDOWN}>{body}</ReactMarkdown>
      </article>
    ),
  };
});

/**
 * The prompt. Reading renders it with react-markdown (raw HTML stays text).
 * Editing uses the kit editor with no live preview: its preview pipeline runs
 * rehype-raw, which would render HTML typed or imported into the prompt. Both
 * chunks are lazy — they belong to this tab alone, and while one arrives the
 * `pre` shows the same truth.
 */
export function PromptMarkdown({
  value,
  onChange,
  disabled = false,
}: {
  value: string;
  onChange?: (next: string) => void;
  disabled?: boolean;
}) {
  const editing = onChange !== undefined && !disabled;
  const fallback = (
    <pre className="whitespace-pre-wrap font-mono text-xs" data-testid="agent-tab-prompt-loading">
      {value}
    </pre>
  );
  return (
    <div data-testid="agent-tab-prompt">
      <Suspense fallback={fallback}>
        {editing ? (
          <KitMarkdownEditor value={value} preview="edit" height={360} onChange={(next) => onChange?.(next ?? "")} />
        ) : (
          <PromptView body={value} />
        )}
      </Suspense>
    </div>
  );
}

/** An agent's content, in full: the identity block and every content field the row carries — the create/import answer prints this. */
export function AgentDetail({ agent }: { agent: AgentDetailRow }) {
  const { t } = useI18n();
  return (
    <div className="mt-3 space-y-3" data-testid="agent-detail">
      <AgentIdentity agent={agent} />

      <ContentField label={t("agents.detail.description")} empty={t("agents.detail.descriptionEmpty")} value={agent.description} testId="agent-detail-description" />
      <ContentField label={t("agents.detail.functionality")} empty={t("agents.detail.functionalityEmpty")} value={agent.functionality} testId="agent-detail-functionality" />
      <ContentField label={t("agents.detail.prompt")} empty={t("agents.detail.promptEmpty")} value={agent.prompt} pre testId="agent-detail-prompt" />
      <div className="space-y-1">
        <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.detail.skills")}</p>
        <SkillsView skills={agent.skills} />
      </div>
      <div className="space-y-1">
        <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{t("agents.detail.metadata")}</p>
        <MetaView metadata={agent.metadata} />
      </div>
    </div>
  );
}

function ContentField({
  label,
  value,
  empty,
  pre,
  testId,
}: {
  label: string;
  value: string;
  empty: string;
  pre?: boolean;
  testId: string;
}) {
  const has = value !== undefined && value !== null && String(value).trim() !== "";
  return (
    <div className="space-y-1">
      <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">{label}</p>
      {has ? (
        pre ? (
          <pre className="whitespace-pre-wrap font-mono text-xs" data-testid={testId}>{String(value)}</pre>
        ) : (
          <p className="text-sm" data-testid={testId}>{String(value)}</p>
        )
      ) : (
        <p className="text-sm text-slate-500" data-testid={testId}>{empty}</p>
      )}
    </div>
  );
}

/** The skills JSON array (AM3 shape: { id, name, description }), rendered as a list — or verbatim when it does not parse. */
function SkillsView({ skills }: { skills: string }) {
  const { t } = useI18n();
  if (skills === undefined || skills === null || String(skills).trim() === "") {
    return <p className="text-sm text-slate-500" data-testid="agent-detail-skills-empty">{t("agents.detail.skillsEmpty")}</p>;
  }
  const parsed = tryParse(skills);
  if (!Array.isArray(parsed)) {
    return <CodeBlock code={String(skills)} language="plain" data-testid="agent-detail-skills-raw" />;
  }
  const list = parsed as Array<{ id?: string; name?: string; description?: string }>;
  if (list.length === 0) {
    return <p className="text-sm text-slate-500" data-testid="agent-detail-skills-empty">{t("agents.detail.skillsEmpty")}</p>;
  }
  return (
    <ul className="space-y-1" data-testid="agent-detail-skills">
      {list.map((s, i) => (
        <li key={s.id ?? s.name ?? i} className="text-sm">
          <span className="font-mono text-xs">{s.id ?? s.name ?? `#${i}`}</span>
          {s.description ? <span className="ml-1 text-xs text-slate-400">{s.description}</span> : null}
        </li>
      ))}
    </ul>
  );
}

/**
 * What the agent must report when it finishes, as the daemon reads it: a map of
 * field → purpose, where a `?` on the field's name marks it optional. Shown as a
 * list of fields a reader can scan; anything that is not that shape is printed
 * as the colored JSON it is, never hidden.
 */
export function HandoffView({ handoff }: { handoff: string }) {
  const { t } = useI18n();
  if (handoff.trim() === "") {
    return <p className="text-sm text-slate-500" data-testid="agent-tab-handoff">{t("agents.readonly.handoffEmpty")}</p>;
  }
  const parsed = tryParse(handoff);
  const isMap = parsed !== null && typeof parsed === "object" && !Array.isArray(parsed);
  const entries = isMap ? Object.entries(parsed as Record<string, unknown>) : [];
  if (!isMap || entries.length === 0 || entries.some(([, purpose]) => typeof purpose !== "string")) {
    return isMap && entries.length === 0
      ? <p className="text-sm text-slate-500" data-testid="agent-tab-handoff">{t("agents.readonly.handoffEmpty")}</p>
      : <CodeBlock code={parsed === null ? handoff : JSON.stringify(parsed, null, 2)} language={parsed === null ? "plain" : "json"} data-testid="agent-tab-handoff" />;
  }
  return (
    <ul className="space-y-2" data-testid="agent-tab-handoff">
      {entries.map(([key, purpose]) => {
        const optional = key.endsWith("?");
        const name = key.replace(/\?$/, "");
        return (
          <li key={key} className="text-sm" data-testid={`agent-handoff-${name}`}>
            <div className="flex flex-wrap items-center gap-2">
              <code className="font-mono text-xs font-semibold" style={{ color: "var(--code-key)" }}>{name}</code>
              <Pill size="sm" tone={optional ? "neutral" : "amber"}>{optional ? t("agents.handoff.optional") : t("agents.handoff.required")}</Pill>
            </div>
            <p className="mt-0.5 opacity-80">{String(purpose)}</p>
          </li>
        );
      })}
    </ul>
  );
}

/** The metadata JSON blob, pretty-printed when it parses, verbatim otherwise. */
function MetaView({ metadata }: { metadata: string }) {
  const { t } = useI18n();
  if (metadata === undefined || metadata === null || String(metadata).trim() === "") {
    return <p className="text-sm text-slate-500" data-testid="agent-detail-metadata-empty">{t("agents.detail.metadataEmpty")}</p>;
  }
  const parsed = tryParse(metadata);
  if (parsed === null) {
    return <KitCodeBlock value={String(metadata)} language="plain" showTitle={false} data-testid="agent-detail-metadata-raw" />;
  }
  return <KitCodeBlock value={JSON.stringify(parsed, null, 2)} language="json" showTitle={false} data-testid="agent-detail-metadata" />;
}

function tryParse(text: string): unknown | null {
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

/**
 * The row's avatar. The minion mood vocabulary is the agent's `avatarMood` set
 * one-to-one; an unknown mood clamps to "idle". A non-minion type is a coloured
 * label — the type, the colour, the mood — not a minion wearing a cat.
 *
 * `compact` is the table's icon rendering of the same truth: the minion shrinks,
 * a coloured type keeps its dot and its title but loses the word (the cell says
 * the name already), and "no avatar" honestly renders nothing rather than a
 * placeholder pretending to be one. The four fields are the whole of what an
 * avatar needs — the detail row satisfies them, a row's `agent.view` answer
 * satisfies them, nothing wider is asked for.
 */
export type AgentAvatarFields = Pick<AgentDetailRow, "name" | "avatarType" | "avatarColor" | "avatarMood"> & Partial<Pick<AgentDetailRow, "metadata">>;

export function AgentAvatar({ agent, compact = false }: { agent: AgentAvatarFields; compact?: boolean }) {
  const { t } = useI18n();
  if (agent.avatarType === "minion") {
    return (
      <MinionAvatar
        // The minion the agent chose, or — as every agent was before it could choose — one drawn from its name.
        name={agentMinion(agent.metadata) || agent.name}
        color={agent.avatarColor || "yellow"}
        mood={toMinionMood(agent.avatarMood)}
        size={compact ? 28 : 44}
        decorative
        data-testid={compact ? undefined : "agent-avatar"}
      />
    );
  }
  if (compact) {
    if (agent.avatarType === "") return null;
    return (
      <span
        className="inline-block h-6 w-6 rounded-full"
        style={{ background: agent.avatarColor || "#94a3b8" }}
        title={`${agent.avatarType} · ${agent.avatarColor || "—"}`}
      />
    );
  }
  return (
    <span
      className="inline-flex items-center gap-1.5"
      title={`${agent.avatarType || t("agents.detail.avatar")} · ${agent.avatarColor || "—"}`}
      data-testid="agent-avatar"
    >
      <span
        className="inline-block h-6 w-6 rounded-full"
        style={{ background: agent.avatarColor || "#94a3b8" }}
      />
      <span className="text-xs text-slate-500">{agent.avatarType || t("agents.detail.avatar")}</span>
    </span>
  );
}

const MINION_MOODS: readonly MinionMood[] = ["idle", "hi", "thinking", "working", "stuck", "done"];

function toMinionMood(mood: string): MinionMood {
  return (MINION_MOODS as readonly string[]).includes(mood) ? (mood as MinionMood) : "idle";
}
