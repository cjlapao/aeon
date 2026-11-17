import { describe, expect, it } from "vitest";
import { languageOf, parseUnified, toSplit } from "../src/components/diffModel";

const DIFF = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@
 keep
-old one
-old two
+new one
+new two
+new three
 tail
\\ No newline at end of file
`;

describe("diff model", () => {
  it("reads rows with line numbers and drops the file headers", () => {
    const rows = parseUnified(DIFF);
    expect(rows.map((r) => r.kind)).toEqual(["hunk", "ctx", "del", "del", "add", "add", "add", "ctx"]);
    expect(rows[1]).toMatchObject({ oldNo: 1, newNo: 1, text: "keep" });
    expect(rows[2]).toMatchObject({ oldNo: 2, text: "old one" });
    expect(rows[4]).toMatchObject({ newNo: 2, text: "new one" });
    expect(rows[7]).toMatchObject({ oldNo: 4, newNo: 5 });
  });

  it("pairs a removed run with the added run beside it", () => {
    const split = toSplit(parseUnified(DIFF));
    const lines = split.filter((r) => r.kind === "line") as Extract<(typeof split)[number], { kind: "line" }>[];
    expect(lines).toHaveLength(5);
    expect(lines[1]!.left?.text).toBe("old one");
    expect(lines[1]!.right?.text).toBe("new one");
    expect(lines[3]!.left).toBeUndefined();
    expect(lines[3]!.right?.text).toBe("new three");
  });

  it("names a language from the path", () => {
    expect(languageOf("internal/a.go")).toBe("go");
    expect(languageOf("README")).toBe("plain");
  });
});
