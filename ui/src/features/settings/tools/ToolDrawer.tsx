import { Link } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill } from "../../../components/kit";
import { DetailSidePanel } from "./DetailSidePanel";
import { AgentMatrix } from "./AgentMatrix";
import { capabilityPill, capabilityMeaning, linkFor, stateBadge, type CatalogVocabulary, type ToolDetail } from "./toolsModel";

// The tool panel: one tool in full, from `tools.get`, laid out as questions — what is it, what can
// it do, who may call it, which rules apply, what does it take. Verbatim, byte for byte: the
// description, the schema, every capability name and its meaning, the per-agent REASONS, the policy
// rows' own `describe` sentence. Chosen here: the order and the tone of a pill. Read-only,
// structurally (TC1): nothing in this panel reaches the daemon with a change; the policy rows link
// OUT to where rules are authored, and an MCP tool links to its server.

export interface ToolDrawerProps {
  name: string;
  detail: ToolDetail | undefined;
  loading: boolean;
  error?: string;
  vocabulary: CatalogVocabulary | undefined;
  onClose: () => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return <div><dt className="inline opacity-70">{label}: </dt><dd className="inline">{children}</dd></div>;
}

export function ToolDrawer({ name, detail, loading, error, vocabulary, onClose }: ToolDrawerProps) {
  const { t } = useI18n();

  if (loading) return <DetailSidePanel title={name} testId="tools-drawer" onClose={onClose}><EmptyState title={t("settings.tools.drawer.loading")} /></DetailSidePanel>;
  if (error) return <DetailSidePanel title={name} testId="tools-drawer" onClose={onClose}><Alert intent="danger" title={t("settings.tools.drawer.failed")} description={error} /></DetailSidePanel>;
  if (!detail) return <DetailSidePanel title={name} testId="tools-drawer" onClose={onClose}><EmptyState title={t("settings.tools.drawer.none")} /></DetailSidePanel>;

  const tool = detail.tool;
  const serverLink = linkFor(detail.mcp?.link);
  const schemaText = JSON.stringify(tool.schema ?? {}, null, 2);

  return (
    <DetailSidePanel title={tool.name} testId="tools-drawer" onClose={onClose}>
      <div className="space-y-8">
        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.what")}</h3>
          <p className="text-sm" data-testid="tools-drawer-description">{tool.description}</p>
          <dl className="space-y-1 text-sm">
            <Fact label={t("settings.tools.drawer.family")}>{tool.family}</Fact>
            <Fact label={t("settings.tools.drawer.source")}><Pill tone={typeof tool.server === "string" && tool.server !== "" ? "violet" : "blue"} size="sm">{tool.source}</Pill></Fact>
            <Fact label={t("settings.tools.drawer.rerun")}><span data-testid="tools-drawer-rerun">{tool.rerunClass}</span></Fact>
            <Fact label={t("settings.tools.drawer.origin")}>{tool.origin}</Fact>
            <Fact label={t("settings.tools.drawer.version")}>{tool.version}</Fact>
            {tool.server ? <Fact label={t("settings.tools.drawer.server")}>{tool.server}</Fact> : null}
            {tool.tier ? <Fact label={t("settings.tools.drawer.tier")}>{tool.tier}</Fact> : null}
            {tool.effect ? <Fact label={t("settings.tools.drawer.effect")}>{tool.effect}</Fact> : null}
            {tool.twin ? <Fact label={t("settings.tools.drawer.twin")}>{tool.twin}</Fact> : null}
            {(tool.aliases ?? []).length > 0 ? <Fact label={t("settings.tools.drawer.aliases")}>{(tool.aliases ?? []).join(", ")}</Fact> : null}
            {tool.conditional ? <Fact label={`${t("settings.tools.row.conditional")} · ${t("settings.tools.filters.available")}`}>{tool.available ? t("settings.tools.filters.availableYes") : t("settings.tools.filters.availableNo")}</Fact> : null}
            {tool.governed.length > 0 ? (
              <Fact label={t("settings.tools.drawer.governedBy")}>
                <span className="inline-flex flex-wrap gap-1 align-middle">{tool.governed.map((g) => <Pill key={g} tone="indigo" size="sm">{g}</Pill>)}</span>
              </Fact>
            ) : null}
          </dl>
          {serverLink ? (
            <p><Link to={serverLink.to} className="aeon-accent-text text-sm underline" data-testid="tools-drawer-server-link">{t(serverLink.labelKey, { server: detail.mcp?.server ?? "" })}</Link></p>
          ) : null}
        </section>

        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.can")}</h3>
          <ul className="space-y-1" data-testid="tools-drawer-capabilities">
            {tool.capabilities.map((capability) => {
              const pill = capabilityPill(capability);
              const meaning = capabilityMeaning(vocabulary, capability);
              return (
                <li key={capability} className="flex flex-wrap items-center gap-2 text-sm" data-testid={`tools-drawer-cap-${capability}`}>
                  <Pill tone={pill.tone} size="sm">{pill.text}</Pill>
                  <span className="opacity-80">{meaning}</span>
                </li>
              );
            })}
            {tool.capabilities.length === 0 ? <li className="text-sm opacity-60">{t("settings.tools.row.noCapabilities")}</li> : null}
          </ul>
        </section>

        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.who")}</h3>
          <AgentMatrix rows={detail.agents ?? []} />
        </section>

        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.rules")}</h3>
          {tool.address ? (
            <Link to={`/config/policy?tool=${encodeURIComponent(tool.address)}`} className="aeon-accent-text text-sm underline" data-testid="tools-drawer-write-policy">
              {t("settings.tools.drawer.writePolicy")}
            </Link>
          ) : null}
          {(detail.policies ?? []).length === 0 ? (
            <p className="text-sm opacity-60" data-testid="tools-drawer-policies-none">{t("settings.tools.drawer.policy.none")}</p>
          ) : (
            <ul className="divide-y divide-[var(--border)]" data-testid="tools-drawer-policies">
              {(detail.policies ?? []).map((row) => {
                const link = linkFor(row.link);
                const badge = stateBadge(row.action);
                return (
                  <li key={row.id} className="space-y-1 py-2 text-sm" data-testid={`tools-drawer-policy-${row.id}`}>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-semibold">{row.id}</span>
                      <Pill tone={badge.tone} size="sm">{row.action}</Pill>
                      <Pill tone="slate" size="sm">{row.source}</Pill>
                      <Pill tone="slate" size="sm">{row.origin}</Pill>
                      {row.expired ? <Pill tone="slate" size="sm">{t("settings.tools.drawer.policy.expired")}</Pill> : null}
                    </div>
                    <p className="text-xs opacity-60">{row.target}</p>
                    <p className="opacity-80" data-testid={`tools-drawer-policy-describe-${row.id}`}>{row.describe}</p>
                    {link ? <Link to={link.to} className="aeon-accent-text text-xs underline" data-testid={`tools-drawer-policy-link-${row.id}`}>{t(link.labelKey, { id: row.id })}</Link> : null}
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.schema")}</h3>
          <p className="text-xs opacity-70">{t("settings.tools.drawer.schemaNote")} <code className="break-all">{tool.schemaHash}</code></p>
          <pre className="max-h-72 overflow-auto rounded-sm bg-slate-950/90 p-3 text-xs text-slate-100" data-testid="tools-drawer-schema"><code>{schemaText}</code></pre>
        </section>

        <p className="text-xs opacity-60" data-testid="tools-drawer-readonly-note">{t("settings.tools.drawer.readOnlyNote")}</p>
      </div>
    </DetailSidePanel>
  );
}
