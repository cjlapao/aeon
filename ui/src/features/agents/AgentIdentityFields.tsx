import { useI18n } from "@cjlapao/ui-kit";
import { FormField, Input } from "../../components/kit";
import { MINION_COLORS } from "../../components/minion-avatar";
import { MinionChooser } from "./MinionChooser";

// Who an agent is, as one block: its minion (click it to change which one; the swatches change its
// colour), its name and its one-line description. The new-agent panel and the edit tab draw the
// same block, so naming and dressing an agent is one habit. The slug is not here: it is made from the
// name when the agent is created and never changes (the caller says so, beside this block).

const COLOURS = Object.keys(MINION_COLORS) as (keyof typeof MINION_COLORS)[];

export interface AgentIdentityFieldsProps {
  name: string;
  onName: (next: string) => void;
  description: string;
  onDescription: (next: string) => void;
  colour: string;
  onColour: (next: string) => void;
  minion: string;
  onMinion: (next: string) => void;
  /** The text under the name field. */
  nameHint: string;
  disabled?: boolean;
  /** Prefix for the test ids, so a surface keeps its own. */
  idPrefix?: string;
}

export function AgentIdentityFields({
  name, onName, description, onDescription, colour, onColour, minion, onMinion, nameHint, disabled = false, idPrefix = "agent-create",
}: AgentIdentityFieldsProps) {
  const { t } = useI18n();
  return (
    <div className="flex items-start gap-5">
      <div className="flex w-24 shrink-0 flex-col items-center gap-3">
        <MinionChooser name={name} color={colour} minion={minion} onChange={onMinion} disabled={disabled} />
        <div role="radiogroup" aria-label={t("agents.create.colour")} className="flex flex-wrap justify-center gap-1.5">
          {COLOURS.map((word) => (
            <button
              key={word}
              type="button"
              role="radio"
              aria-checked={colour === word}
              aria-label={word}
              title={word}
              disabled={disabled}
              onClick={() => onColour(word)}
              data-testid={`${idPrefix}-colour-${word}`}
              className="h-4 w-4 rounded-full outline-offset-2"
              style={{ background: MINION_COLORS[word], outline: colour === word ? "2px solid currentColor" : "none" }}
            />
          ))}
        </div>
      </div>
      <div className="min-w-0 flex-1 space-y-4">
        <FormField label={t("agents.create.name")} labelFor={`${idPrefix}-name`} hint={nameHint} required>
          <Input id={`${idPrefix}-name`} variant="ghost" size="md" value={name} disabled={disabled} onChange={(event) => onName(event.target.value)} data-testid={`${idPrefix}-name`} />
        </FormField>
        <FormField label={t("agents.create.description")} labelFor={`${idPrefix}-description`} hint={t("agents.create.descriptionHint")}>
          <Input id={`${idPrefix}-description`} variant="ghost" size="md" value={description} disabled={disabled} onChange={(event) => onDescription(event.target.value)} data-testid={`${idPrefix}-description`} />
        </FormField>
      </div>
    </div>
  );
}
