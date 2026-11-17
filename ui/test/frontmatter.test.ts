import { describe, expect, it } from "vitest";
import { isIsoDate, parseFrontmatter } from "../src/lib/frontmatter";

/**
 * These mirror the MCP's own parser rules. The two must agree on what counts as a
 * header — a UI that accepted something the writer would not would show metadata
 * the loop does not believe it has.
 */

const DOC = `---
type: discovery
loop: sms-delivery
generated: "2026-08-19T05:49:46.372Z"
updated: "2026-08-19T05:49:46.372Z"
mcp: "0.4.3"
---

# Discovery

Body text.
`;

describe("parseFrontmatter", () => {
  it("reads the header the loop actually writes", () => {
    const { data, had } = parseFrontmatter(DOC);
    expect(had).toBe(true);
    expect(data).toEqual({
      type: "discovery",
      loop: "sms-delivery",
      generated: "2026-08-19T05:49:46.372Z",
      updated: "2026-08-19T05:49:46.372Z",
      mcp: "0.4.3",
    });
  });

  it("unquotes values the writer had to quote", () => {
    // Timestamps and versions are quoted because bare they would parse as
    // numbers or dates; the quotes are syntax, not content.
    const { data } = parseFrontmatter(DOC);
    expect(data.mcp).toBe("0.4.3");
    expect(data.generated?.startsWith('"')).toBe(false);
  });

  it("returns the body without the header or its blank line", () => {
    const { body } = parseFrontmatter(DOC);
    expect(body.startsWith("# Discovery")).toBe(true);
    expect(body).not.toContain("type: discovery");
  });

  it("leaves a document with no header completely alone", () => {
    const plain = "# Plan\n\nNo header here.\n";
    expect(parseFrontmatter(plain)).toEqual({ data: {}, body: plain, had: false });
  });

  it("treats an unterminated fence as no header", () => {
    // Otherwise the whole document would be swallowed as metadata.
    const broken = "---\ntype: discovery\n\n# Body\n";
    const { had, body } = parseFrontmatter(broken);
    expect(had).toBe(false);
    expect(body).toBe(broken);
  });

  it("rejects the WHOLE header when a line is outside the subset", () => {
    // Half a header is worse than none: it looks complete while silently
    // dropping whatever could not be represented.
    const nested = "---\ntype: discovery\nnested:\n  key: value\n---\n\n# Body\n";
    expect(parseFrontmatter(nested).had).toBe(false);

    const list = "---\ntype: discovery\n- item\n---\n\n# Body\n";
    expect(parseFrontmatter(list).had).toBe(false);
  });

  it("is not fooled by a horizontal rule mid-document", () => {
    const rule = "# Title\n\n---\n\nAfter the rule.\n";
    expect(parseFrontmatter(rule).had).toBe(false);
  });

  it("skips blanks and comments inside the header", () => {
    const withComment = "---\n# a comment\ntype: plan\n\nloop: x\n---\n\nBody\n";
    const { data, had } = parseFrontmatter(withComment);
    expect(had).toBe(true);
    expect(data).toEqual({ type: "plan", loop: "x" });
  });
});

describe("isIsoDate", () => {
  it("recognises the instants the loop stamps", () => {
    expect(isIsoDate("2026-08-19T05:49:46.372Z")).toBe(true);
  });

  it("rejects everything else", () => {
    expect(isIsoDate("0.4.3")).toBe(false);
    expect(isIsoDate("discovery")).toBe(false);
    expect(isIsoDate("2026-13-45T99:99:99Z")).toBe(false);
  });
});
