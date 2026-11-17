import type { ChatItem, ChatQuestion, QuestionKind } from "../../components/agent-chat";
import type { BlockQuestion } from "./pendingQuestion";

// The saved transcript draws a question as the plain tool call the agent made;
// the live chat draws it as a card with the answer the person gave. The store
// still holds every question and its answer (with the id of the call that asked
// it), so a reopened chat puts the same card where the call was (S76).

const KINDS: readonly QuestionKind[] = ["clarify", "gate", "permission", "free"];
const kindOf = (v: string): QuestionKind => ((KINDS as readonly string[]).includes(v) ? (v as QuestionKind) : "free");

const asQuestion = (q: BlockQuestion): ChatQuestion => ({
  id: String(q.id), prompt: q.prompt, kind: kindOf(q.kind),
  ...(q.answers.length > 0 ? { options: q.answers } : {}),
  ...(q.answer ? { answer: q.answer } : {}),
});

/**
 * History items with each answered question put back as the live chat shows it:
 * a `question` call becomes its card (every question that call asked, as one
 * block); a call the policy parked on a person keeps its row and gains the
 * decision. Anything the store has no row for is left exactly as saved.
 */
export function withPastQuestions(history: ChatItem[], past: readonly BlockQuestion[]): ChatItem[] {
  if (past.length === 0) return history;
  const byCall = new Map<string, BlockQuestion[]>();
  for (const q of past) if (q.toolUseId) byCall.set(q.toolUseId, [...(byCall.get(q.toolUseId) ?? []), q]);
  if (byCall.size === 0) return history;
  return history.map((item) => {
    if (item.kind !== "tool" || !item.toolUseId) return item;
    const asked = byCall.get(item.toolUseId);
    if (!asked) return item;
    if (item.tool === "question") {
      const first = asked[0]!;
      return { id: `q:${first.id}`, at: item.at, agent: item.agent, kind: "question", questions: asked.map(asQuestion) } as ChatItem;
    }
    const permission = asked.find((q) => kindOf(q.kind) === "permission");
    return permission ? { ...item, approval: asQuestion(permission) } : item;
  });
}
