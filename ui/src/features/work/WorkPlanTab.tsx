import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Tabs } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { GoalsScreen } from "../goals/GoalsScreen";
import type { GoalsScope } from "../goals/GoalsPanel";
import { WorkTasksView } from "./WorkTasksView";
import { WorkRoadmapView } from "./WorkRoadmapView";

type View = "goals" | "tasks" | "roadmap";

/** Plan — goals, the task board and the roadmap on the Gantt. */
export function WorkPlanTab({ controller, live, canWrite, scope, root = "" }: { controller: LiveController; live: LiveState; canWrite: boolean; scope?: GoalsScope; root?: string }) {
  const { t } = useI18n();
  const [view, setView] = useState<View>("tasks");
  return (
    <div className="flex flex-col gap-3" data-testid="work-plan">
      <div className="shrink-0">
        <Tabs
          variant="pill"
          items={(["goals", "tasks", "roadmap"] as const).map((id) => ({ id, label: t(`work.plan.${id}`) }))}
          value={view}
          onChange={(id) => setView(id as View)}
        />
      </div>
      <div>
        {view === "tasks" && <WorkTasksView controller={controller} live={live} componentIds={scope?.componentIds} />}
        {view === "goals" && <GoalsScreen controller={controller} live={live} canWrite={canWrite} scope={scope} />}
        {view === "roadmap" && <WorkRoadmapView controller={controller} live={live} canWrite={canWrite} root={root || undefined} />}
      </div>
    </div>
  );
}
