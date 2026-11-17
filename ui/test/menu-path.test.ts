import { describe, expect, it } from "vitest";
import { menuPath, MENU } from "../src/app/routes";

describe("the rail's links", () => {
  it("never carry a route's optional parameters into the address", () => {
    expect(menuPath("/work/:component?/:tab?")).toBe("/work");
    expect(menuPath("/bench/:page?")).toBe("/bench");
    expect(menuPath("/")).toBe("/");
    expect(menuPath("/loops/:id")).toBe("/loops/:id"); // a required parameter is not optional: left alone, and never a menu link
    for (const item of MENU) if ("path" in item && item.path) expect(item.path).not.toMatch(/[:?]/);
  });
});
