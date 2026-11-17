import { describe, expect, it } from "vitest";
import { inScope } from "./AutomationSection";

describe("automation scope", () => {
  const roots = new Set(["/w/aeon/ui"]);
  it("keeps a workspace that is a scoped root or inside one", () => {
    expect(inScope("/w/aeon/ui", roots)).toBe(true);
    expect(inScope("/w/aeon/ui/kit", roots)).toBe(true);
  });
  it("drops a sibling with the same prefix, another project, and no workspace", () => {
    expect(inScope("/w/aeon/ui-old", roots)).toBe(false);
    expect(inScope("/w/helper", roots)).toBe(false);
    expect(inScope(undefined, roots)).toBe(false);
  });
});
