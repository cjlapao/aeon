import { useI18n } from "@cjlapao/ui-kit";
import { TagPicker } from "../../components/kit";

// The technologies an agent works in, chosen with the kit's tag picker: a field of tags with a
// searchable list behind it. The vocabulary is the daemon's (`handlers.list` → `techTags`), a
// closed list, so nothing can be typed that the daemon would refuse.

export interface TechPickerProps {
  /** The daemon's closed list. */
  tags: readonly { tag: string; label: string; group: string }[];
  value: readonly string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  loading?: boolean;
}

export function TechPicker({ tags, value, onChange, disabled = false, loading = false }: TechPickerProps) {
  const { t } = useI18n();
  return (
    <TagPicker
      items={tags.map((tag) => ({ id: tag.tag, label: tag.label }))}
      value={[...value]}
      onChange={onChange}
      placeholder={t("agents.tech.placeholder")}
      searchPlaceholder={t("agents.tech.search")}
      emptyMessage={t("agents.tech.empty")}
      loading={loading}
      highlightNew={false}
      tagLimit={6}
      disabled={disabled}
      size="md"
    />
  );
}
