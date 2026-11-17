import { describe, expect, it } from "vitest";
import { agentMinion, nextMinion, withAgentMinion } from "./agentsModel";

describe("the minion an agent wears", () => {
  it("is read from the metadata, and only if it is one of the built-ins", () => {
    expect(agentMinion('{"minion":"bop","tech":["go"]}')).toBe("bop");
    expect(agentMinion('{"minion":"kevin"}')).toBe("");
    expect(agentMinion("")).toBe("");
    expect(agentMinion("not json")).toBe("");
  });

  it("is set without touching the other keys, removed when empty, and left alone when the text is mid-edit", () => {
    expect(JSON.parse(withAgentMinion('{"tech":["go"]}', "noodle"))).toEqual({ tech: ["go"], minion: "noodle" });
    expect(withAgentMinion('{"minion":"pip"}', "")).toBe("");
    expect(withAgentMinion('{"minion":"pip","owner":"x"}', "")).toBe('{"owner":"x"}');
    expect(withAgentMinion("{broken", "pip")).toBe("{broken");
  });

  it("goes round: the name, then each minion, then back to the name", () => {
    expect(nextMinion("")).toBe("pip");
    expect(nextMinion("pip")).toBe("bop");
    expect(nextMinion("bop")).toBe("noodle");
    expect(nextMinion("noodle")).toBe("");
  });
});
