import { useMemo, useState } from "react";
import { Button } from "../../../components/kit";
import type { ChatTaskList } from "../../../components/agent-chat";
import { rpcTyped, toRpcError } from "../../../lib/bridge";
import { taskStatus, type LoopTodos } from "./todoModel";

// The loop's todo list as the chat's own To-dos panel (S75): the task-list slot
// above the composer that `AgentChat` already owns. This only adapts the daemon's
// list into that slot's shape; it draws nothing itself. While the list is active
// the heading carries the person's Close list (`loop.todos.close`); a closed list
// stays, saying who closed it, until a new list replaces it.

export function useTodoPanel(loopId: string, todos: LoopTodos | null | undefined, canWrite: boolean, onChange?: (next: LoopTodos) => void): ChatTaskList | undefined {
  const [error, setError] = useState<string | undefined>(undefined);
  return useMemo(() => {
    if (!todos || todos.items.length === 0) return undefined;
    const close = async () => {
      setError(undefined);
      try {
        onChange?.(await rpcTyped("loop.todos.close", { id: loopId }));
      } catch (cause) {
        setError(toRpcError(cause).message);
      }
    };
    const note = !todos.active ? `closed${todos.closedBy ? ` · ${todos.closedBy}` : ""}` : error;
    return {
      title: "To-dos",
      tasks: todos.items.map((i) => ({ id: i.id, label: i.content, status: taskStatus(i.status) })),
      note,
      actions: todos.active && onChange
        ? <Button size="xs" variant="ghost" disabled={!canWrite} onClick={() => void close()} data-testid="todo-panel-close">Close list</Button>
        : undefined,
    };
  }, [todos, canWrite, onChange, loopId, error]);
}
