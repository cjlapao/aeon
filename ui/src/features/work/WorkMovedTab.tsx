import { Link } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Panel } from "../../components/kit";

/**
 * A tab whose content has not moved into the component yet: it says so and links to the page that
 * still holds it. Honest in place of an empty tab; each page leaves this list as it moves in.
 */
export function WorkMovedTab({ pages }: { pages: readonly { label: string; to: string }[] }) {
  const { t } = useI18n();
  return (
    <Panel tone="neutral" data-testid="work-moved">
      <h2 className="text-base font-semibold">{t("work.moved.title")}</h2>
      <p className="mt-1 text-sm opacity-80">{t("work.moved.body")}</p>
      <ul className="mt-3 flex flex-wrap gap-3">
        {pages.map((page) => (
          <li key={page.to}><Link className="aeon-accent-text underline" to={page.to}>{t("work.moved.open", { page: page.label })}</Link></li>
        ))}
      </ul>
    </Panel>
  );
}
