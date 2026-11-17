import { describe, expect, it } from "vitest";
import {
  canDelete,
  describeUsage,
  editedBadge,
  listRows,
  originBadge,
  revisionLabel,
  saveOutcome,
  sortDisplay,
} from "./blueprintsModel";
import { diffLineKind } from "./panels/DiffView";

// blueprintsModel.test.ts — the few decisions the UI is allowed to make, and the
// shape of the ones it is not.
//
// Nothing here computes a verdict, a reference or a revision: the model's whole
// job is to read the ANSWER correctly. So the tests are mostly about absence —
// a field the daemon did not send is not turned into a badge, an empty reference
// set is not turned into "in use", a save that changed nothing is not reported as
// a new revision.

const usage = {
  actions: ["commit"],
  agents: ["golang-engineer"],
  loopsStarted: 0,
  queueItems: [{ id: "3", kind: "queued", name: "nightly", status: "queued" }],
  schedules: [] as { id: string; kind: string; name: string }[],
  skills: [],
  triggers: [],
};

describe("blueprintsModel", () => {
  it("reads rows from either shape the daemon may answer with", () => {
    const rich = listRows({
      blueprints: [{ name: "two-step", origin: "user", revision: 3, version: 2, contentHash: "h", updatedAt: "t", updatedBy: "cli", usedByCount: 0, divergedFromShipped: true, title: "Two Step", description: "" }],
      names: ["two-step"],
    });
    expect(rich.map((row) => row.name)).toEqual(["two-step"]);
    expect(rich[0]?.revision).toBe(3);
    // Names only: every other column stays UNKNOWNS rather than zeros.
    const thin = listRows({ names: ["dev-loop"] });
    expect(thin[0]?.origin).toBeUndefined();
    expect(thin[0]?.usedByCount).toBeUndefined();
    expect(listRows(undefined)).toEqual([]);
  });

  it("sorts shipped rows first, each group by name", () => {
    const rows = sortDisplay([{ name: "zeta" }, { name: "alpha", origin: "system" }, { name: "beta", origin: "system" }, { name: "aaa", origin: "user" }]);
    expect(rows.map((row) => row.name)).toEqual(["alpha", "beta", "aaa", "zeta"]);
  });

  it("names an origin badge only when the daemon named an origin", () => {
    expect(originBadge("system")).toEqual({ key: "shipped", tone: "violet" });
    expect(originBadge("user")).toEqual({ key: "yours", tone: "green" });
    expect(originBadge(undefined)).toBeNull();
    expect(originBadge("something-new")).toBeNull();
  });

  it("treats an absent divergedFromShipped as no information, not as unedited", () => {
    expect(editedBadge({ divergedFromShipped: true })).toBe(true);
    expect(editedBadge({ divergedFromShipped: false })).toBe(false);
    expect(editedBadge({})).toBeUndefined();
  });

  it("offers delete only for a row the daemon has not shipped", () => {
    expect(canDelete({ origin: "user" })).toBe(true);
    expect(canDelete({ origin: "system" })).toBe(false);
    // A row whose origin the answer did not name still offers the button: this
    // is advisory only, and the refusal that matters is the daemon's — it will
    // not delete a shipped row because the UI asked. Hiding the button on an
    // unknown word would be the UI making a claim nobody made.
    expect(canDelete({})).toBe(true);
  });

  it("labels a revision the way the history does, and never a version", () => {
    expect(revisionLabel(7)).toBe("#7");
  });

  it("drops the reference groups that are empty and keeps the counts the daemon sent", () => {
    const report = describeUsage(usage);
    expect(report.empty).toBe(false);
    expect(report.loopsStarted).toBe(0);
    expect(report.groups.map((group) => group.key)).toEqual(["queueItems", "agents", "actions"]);
    expect(report.groups[0]?.items).toEqual(["nightly"]);
    const nothing = describeUsage({ ...usage, actions: [], agents: [], queueItems: [] });
    expect(nothing.groups).toEqual([]);
    expect(nothing.empty).toBe(true);
  });

  it("reports a save as the response said: saved with a revision, or unchanged", () => {
    expect(saveOutcome({ changed: true, revision: 4 })).toEqual({ key: "saved", revision: 4 });
    expect(saveOutcome({ changed: false, revision: 4 })).toEqual({ key: "unchanged", revision: 4 });
  });

  it("marks a diff line for colour without touching its text", () => {
    expect(diffLineKind("+version: 2")).toBe("add");
    expect(diffLineKind("-version: 1")).toBe("remove");
    expect(diffLineKind("@@ -1,2 +1,2 @@")).toBe("hunk");
    expect(diffLineKind("--- r2")).toBe("header");
    expect(diffLineKind(" id: two-step")).toBe("context");
    // A line that merely mentions a marker is still context: colour follows the
    // first character, which is what the daemon's own text put there.
    expect(diffLineKind("plus + something")).toBe("context");
  });
});

describe("flowTitle", () => {
  it("shows the title, and the slug when a flow has none", async () => {
    const { flowTitle } = await import("./blueprintsModel");
    expect(flowTitle({ name: "dev-loop", title: "Developer Loop" })).toBe("Developer Loop");
    expect(flowTitle({ name: "dev-loop" })).toBe("dev-loop");
    expect(flowTitle({ name: "dev-loop", title: "  " })).toBe("dev-loop");
  });
});
