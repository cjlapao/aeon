import type { ReactNode } from "react";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";

// The Tools page's detail shell: the app's shared overlay panel (see
// `components/OverlaySidePanel.tsx` for why it is an overlay and how its
// open/close animation works), keeping the `data-testid` the drawers carry.

export interface DetailSidePanelProps {
  title: string;
  subtitle?: string;
  testId: string;
  onClose: () => void;
  children: ReactNode;
}

export function DetailSidePanel({ title, subtitle, testId, onClose, children }: DetailSidePanelProps) {
  return <OverlaySidePanel title={title} titleClassName="text-xl font-semibold" subtitle={subtitle} testId={testId} variant="simple" width={640} onClose={onClose}>{children}</OverlaySidePanel>;
}

