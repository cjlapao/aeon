import { useMemo } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { TagPicker } from "./kit";
import { LiveController, type LiveState } from "../lib/live";
import { useRpcQuery } from "../lib/useRpcQuery";

// Agents chosen with the kit's tag picker: a field of tags with a searchable list behind it, the way
// the app picks any closed list of things (docs/aeon/UI-DESIGN-LANGUAGE.md §5).
//
// The roster is the daemon's (`agent.list`), so a person can only pick an agent that exists; nothing
// free-typed is ever emitted. The identity shown and emitted is the slug when the roster sends one
// (policy targets are slugs), the name otherwise — the same rule the older list-style picker follows.
// `AgentTagPicker` takes one agent (picking another replaces it); `AgentTagsPicker` takes several.

const fallbackController = new LiveController();

interface Entry { id: string; label: string }

function useRoster(controller: LiveController | undefined, live: LiveState | undefined) {
  const host = controller ?? fallbackController;
  const query = useRpcQuery("agent.list", {}, host, live ?? host.getState());
  const items = useMemo(() => {
    const out: Entry[] = [];
    for (const agent of Array.isArray(query.data?.agents) ? query.data.agents : []) {
      const slug = (agent as { slug?: unknown }).slug;
      const id = typeof slug === "string" && slug !== "" ? slug : agent.name;
      out.push({ id, label: id === agent.name ? id : `${id} · ${agent.name}` });
    }
    return out;
  }, [query.data]);
  return { items, loading: query.loading, error: query.error };
}

interface CommonProps {
  controller?: LiveController;
  live?: LiveState;
  disabled?: boolean;
  placeholder?: string;
  testId?: string;
}

function Picker({ values, onValues, multi, controller, live, disabled = false, placeholder, testId }: CommonProps & { values: string[]; onValues: (next: string[]) => void; multi: boolean }) {
  const { t } = useI18n();
  const roster = useRoster(controller, live);
  return (
    <div data-testid={testId ?? "agent-tag-picker"}>
      <TagPicker
        items={roster.items}
        value={values}
        onChange={onValues}
        multi={multi}
        placeholder={placeholder ?? t("components.agentTagPicker.placeholder")}
        searchPlaceholder={t("components.agentTagPicker.search")}
        emptyMessage={roster.error ?? t("components.agentTagPicker.empty")}
        loading={roster.loading}
        highlightNew={false}
        tagLimit={6}
        disabled={disabled}
        size="md"
      />
    </div>
  );
}

/** One agent. "" is none. */
export function AgentTagPicker({ value, onChange, ...rest }: CommonProps & { value: string; onChange: (next: string) => void }) {
  return <Picker {...rest} multi={false} values={value === "" ? [] : [value]} onValues={(next) => onChange(next.length === 0 ? "" : (next[next.length - 1] as string))} />;
}

/** Several agents. */
export function AgentTagsPicker({ value, onChange, ...rest }: CommonProps & { value: readonly string[]; onChange: (next: string[]) => void }) {
  return <Picker {...rest} multi values={[...value]} onValues={onChange} />;
}
