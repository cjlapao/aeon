import { useEffect, useState } from "react";
import type { ChatAgent } from "../../components/agent-chat";
import { rpc } from "../../lib/bridge";
import type { RpcMethods } from "../../proto/contract";

/** The avatar rides `agent.view`'s answer, whose shape is the generated contract's. */
type AgentViewResult = RpcMethods["agent.view"]["result"];

type Avatar = NonNullable<ChatAgent["avatar"]>;

// An agent's avatar does not change mid-session, so one agent.view per slug is
// enough; a failed lookup is remembered as "none" and the chat keeps the
// coloured initial rather than retrying on every render.
const cache = new Map<string, Avatar | null>();

/** The avatar of each agent slug, read once from `agent.view`. */
export function useAgentAvatars(slugs: readonly string[]): Record<string, Avatar> {
  const [, bump] = useState(0);
  const key = slugs.join("\u0000");
  useEffect(() => {
    let alive = true;
    for (const slug of slugs) {
      if (cache.has(slug)) continue;
      cache.set(slug, null);
      rpc<AgentViewResult>("agent.view", { slug })
        .then((a) => {
          if (a.avatarType) cache.set(slug, { type: a.avatarType, color: a.avatarColor, mood: a.avatarMood });
          if (alive) bump((n) => n + 1);
        })
        .catch(() => undefined);
    }
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const out: Record<string, Avatar> = {};
  for (const slug of slugs) {
    const a = cache.get(slug);
    if (a) out[slug] = a;
  }
  return out;
}
