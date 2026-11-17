import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Input } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { ToolAddressPicker } from "./pickers/ToolAddressPicker";
import { controlFor, type SpecField } from "./policyModel";

/**
 * Renders one policy type's spec descriptor — fields, kinds, closed state
 * values and all, exactly as `policy.vocabulary` shipped them (PM6). An
 * enum is a select over the descriptor's own `enumValues`; a string is text
 * (bounded when the descriptor ships a `maxLength`); a number is numeric.
 * Nothing here recalls what any type's spec "usually" looks like: change the
 * daemon's descriptors and this form changes with them, unchanged.
 */
export interface SpecFieldsProps {
  fields: SpecField[];
  values: Record<string, string>;
  disabled?: boolean;
  onChange: (name: string, value: string) => void;
  /** The daemon connection the pickers read the catalog through. */
  controller?: LiveController;
  live?: LiveState;
}

export function SpecFields({ fields, values, disabled = false, onChange, controller, live }: SpecFieldsProps) {
  const { t } = useI18n();
  if (fields.length === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">{t("settings.policy.form.specNone")}</p>;
  }
  return (
    <div className="space-y-3" data-testid="spec-fields">
      {fields.map((field) => {
        const control = controlFor(field);
        const value = values[field.name] ?? "";
        const label = `${field.name}${field.required ? " *" : ""}`;
        return (
          <label key={field.name} className="block space-y-1 text-sm">
            <span className="text-xs uppercase tracking-wide opacity-70">{label}</span>
            {field.picker === "tool-address" ? (
              <ToolAddressPicker
                value={value}
                disabled={disabled}
                controller={controller}
                live={live}
                onChange={(next) => onChange(field.name, next)}
              />
            ) : control.control === "select" ? (
              <select
                aria-label={field.name}
                className="w-full rounded-md border border-slate-300 bg-transparent px-2 py-1 text-sm dark:border-slate-700"
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(field.name, event.target.value)}
              >
                <option value="">—</option>
                {control.options.map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
            ) : (
              <Input
                aria-label={field.name}
                type={control.control === "numeric" ? "number" : "text"}
                inputMode={control.control === "numeric" ? "numeric" : undefined}
                maxLength={control.maxLength}
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(field.name, event.target.value)}
              />
            )}
            {field.description !== "" && (
              <span className="block text-xs text-slate-500 dark:text-slate-400">{field.description}</span>
            )}
            {control.unknownKind && (
              <Alert intent="warning" title={t("settings.policy.form.specUnknownKind", { kind: field.kind })} />
            )}
          </label>
        );
      })}
    </div>
  );
}
