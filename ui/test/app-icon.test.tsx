import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { readdirSync } from "node:fs";
import { AppIcon, ICON_NAMES, ICONS, parseSvg } from "../src/components/icons";

// Aeon's own icon set: the files in src/icons/ are the source, the names in
// icons.ts are the contract, and this file keeps the two in step.

afterEach(() => cleanup());

describe("the icon registry", () => {
  it("has exactly one registered name per SVG file in src/icons", () => {
    const files = readdirSync("src/icons").filter((f) => f.endsWith(".svg")).map((f) => f.replace(/\.svg$/, "")).sort();
    expect([...ICON_NAMES].sort()).toEqual(files);
    for (const name of ICON_NAMES) expect(ICONS[name]?.body, name).toBeTruthy();
  });

  it("keeps each icon's own frame and paint", () => {
    expect(ICONS.bash.viewBox).toBe("0 0 16 16");
    expect(ICONS.bash.attrs).toMatchObject({ fill: "none", "stroke-width": "1" });
  });

  it("strips classes, scripts, handlers and foreignObject from the markup", () => {
    const parsed = parseSvg(`<svg viewBox="0 0 24 24" class="hashed_1" width="99"><script>alert(1)</script>
      <path class="x" d="M0 0" onclick="steal()"/><foreignObject><div>x</div></foreignObject></svg>`);
    expect(parsed?.viewBox).toBe("0 0 24 24");
    expect(parsed?.body).toBe('<path d="M0 0"/>');
  });

  it("refuses text that is not an svg", () => {
    expect(parseSvg("<div>nope</div>")).toBeNull();
  });
});

describe("AppIcon", () => {
  it("renders decoration hidden from assistive technology, at the drawn size", () => {
    const { container } = render(<AppIcon name="read" className="text-red-500" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("aria-hidden")).toBe("true");
    expect(svg.getAttribute("width")).toBe("14");
    expect(svg.getAttribute("viewBox")).toBe("0 0 16 16");
    expect(svg.getAttribute("class")).toBe("text-red-500");
    expect(svg.innerHTML).toContain("<path");
  });

  it("becomes a labelled image with a title, escaped", () => {
    render(<AppIcon name="check" title="done <ok>" size={20} strokeWidth={2} />);
    const img = screen.getByRole("img");
    expect(img.querySelector("title")?.textContent).toBe("done <ok>");
    expect(img.getAttribute("width")).toBe("20");
    expect(img.getAttribute("stroke-width")).toBe("2");
  });
});
