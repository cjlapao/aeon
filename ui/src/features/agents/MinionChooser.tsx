import { useI18n } from "@cjlapao/ui-kit";
import { MinionAvatar } from "../../components/minion-avatar";
import { nextMinion } from "./agentsModel";

// The minion, as a control: click it and it becomes the next one — pip, bop, noodle, and back to
// "drawn from the name" (what every agent was before it could choose). The name under it says which
// it is, so the click is never a guess. The colour is the caller's (a row of swatches beside it).

export interface MinionChooserProps {
  /** The agent's name, which draws the minion when none is chosen. */
  name: string;
  /** The minion\'s colour. */
  color: string;
  /** The chosen minion; empty when drawn from the name. */
  minion: string;
  onChange: (next: string) => void;
  disabled?: boolean;
  size?: number;
}

export function MinionChooser({ name, color, minion, onChange, disabled = false, size = 80 }: MinionChooserProps) {
  const { t } = useI18n();
  const label = minion === "" ? t("agents.minion.auto") : minion;
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={() => onChange(nextMinion(minion))}
      aria-label={t("agents.minion.change", { current: label })}
      title={t("agents.minion.hint")}
      data-testid="agent-minion-chooser"
      className="group flex flex-col items-center gap-1 rounded-lg p-1 transition-colors hover:bg-white/5 disabled:cursor-default disabled:hover:bg-transparent"
    >
      <MinionAvatar name={minion === "" ? (name.trim() === "" ? "agent" : name) : minion} color={color === "" ? "yellow" : color} mood="idle" size={size} decorative data-testid="agent-create-preview" />
      <span className="text-xs opacity-70 group-hover:opacity-100" data-testid="agent-minion-name">{label}</span>
    </button>
  );
}
