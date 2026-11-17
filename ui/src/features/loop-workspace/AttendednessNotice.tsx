/**
 * The loop's attendedness, stated once where the person decides (S39 CR7,
 * D8). The daemon declared it at start and never re-declares it, so the app
 * reads the pin's word and says which of the three it is — attended,
 * unattended, never-declared — rather than collapsing the third into
 * unattended (P31: three states, three texts). `undefined` means the field
 * is absent (an older daemon answered `loop.show` without it): the notice
 * renders nothing, because an absent answer is not a no.
 *
 * The words matter: on an unattended loop the policy gate denies an `ask`
 * AT ONCE with no question row, so a `run_start` the agent proposes here has
 * no door to be confirmed at — the person is told that where they would
 * have typed, not surprised by silence later.
 */
export interface AttendednessNoticeProps {
  attended: boolean | null | undefined;
}

export function AttendednessNotice({ attended }: AttendednessNoticeProps) {
  if (attended === undefined) return null; // older daemon: no word, no claim
  const state = attended ? "attended" : attended === false ? "unattended" : "undeclared";
  const text = attended
    ? "Attended — a person is here to confirm asks."
    : attended === false
      ? "Unattended — asks are denied at once. run_start cannot be confirmed here."
      : "Never declared — treated as unattended. run_start cannot be confirmed here.";
  return <p data-testid="attendedness" data-state={state}
    className={`mt-1 text-xs ${attended ? "text-emerald-700 dark:text-emerald-300!" : "text-amber-700 dark:text-amber-300!"}`}>
    {text}
  </p>;
}
