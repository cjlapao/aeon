import { describe, expect, it } from "vitest";
import { handoffProse } from "./handoffText";

const fenced = "```json\n{\n  \"status\": \"ok\",\n  \"summary\": \"Hi! How can I help?\",\n  \"open_questions\": \"None.\"\n}\n```";

describe("handoffProse", () => {
  it("shows the summary of a fenced handoff", () => expect(handoffProse(fenced)).toBe("Hi! How can I help?"));
  it("surfaces open questions only when there are any", () => {
    expect(handoffProse('{"summary":"Done.","open_questions":"Which one?"}')).toBe("Done.\n\n**Still open:** Which one?");
  });
  it("keeps the prose that precedes a closing fenced handoff", () => {
    expect(handoffProse(`Checked the repo.\n\n${fenced}`)).toBe("Checked the repo.\n\nHi! How can I help?");
  });
  it("leaves ordinary text and non-handoff objects alone", () => {
    expect(handoffProse("Just words")).toBeUndefined();
    expect(handoffProse('{"a":1}')).toBeUndefined();
  });
  it("never flashes fence or brace characters while streaming", () => {
    for (let i = 1; i < fenced.length; i++) {
      const shown = handoffProse(fenced.slice(0, i), true);
      expect(shown, JSON.stringify(fenced.slice(0, i))).not.toBeUndefined();
      expect(shown).not.toMatch(/[`{]/);
    }
  });
});
