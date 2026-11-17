import type { LiveController, LiveState } from "../../../lib/live";
import { ScheduleForm } from "./ScheduleForm";
import type { ShowAutomation, Vocabulary } from "./automationModel";

/**
 * WebhookForm — the webhook family's form. It IS the schedule form's shape
 * (blueprint, zone, window, workspace) with the secret-env field swapped in
 * for cron+goal, which is exactly how the plane itself is built: one
 * `trigger.add`/`trigger.update` door, the `kind` deciding the family
 * (the daemon's one validator settles what each kind may carry). Keeping a
 * separate file is the spec's shape for it; the behaviour is one form, so a
 * webhook rule can never drift from the schedule's.
 */
export interface WebhookFormProps {
  initial?: ShowAutomation;
  vocabulary: Vocabulary | undefined;
  authorityStatement: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
}

export function WebhookForm(props: WebhookFormProps) {
  return <ScheduleForm family="webhook" {...props} />;
}
