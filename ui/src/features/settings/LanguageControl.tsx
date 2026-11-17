import { useI18n } from "@cjlapao/ui-kit";
import { MultiToggle } from "../../components/kit";
import { LOCALE_LABELS, SUPPORTED_LOCALES } from "../../lib/i18n";

/**
 * Restricted to `SUPPORTED_LOCALES` (today: `en`, `pt`) rather than the
 * kit's own broader `CURATED_LOCALES`. The kit ships built-in translations
 * for `fr`/`es`/`de` too, so picking one of those would translate every
 * kit-internal string (buttons, calendars) correctly while leaving every
 * one of Aeon's own screens in English — a confusing half measure this
 * switcher doesn't offer until Aeon has its own catalog for that locale.
 */
export function LanguageControl() {
  const { locale, setLocale } = useI18n();

  return <MultiToggle
    aria-label="Language"
    options={SUPPORTED_LOCALES.map((tag) => ({ value: tag, label: LOCALE_LABELS[tag] }))}
    value={SUPPORTED_LOCALES.includes(locale as (typeof SUPPORTED_LOCALES)[number]) ? locale : "en"}
    onChange={(value) => setLocale(value)}
    size="sm" indicator="tonal"
  />;
}
