import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PromptMarkdown } from "./AgentFields";

describe("PromptMarkdown read view", () => {
  it("renders markdown and leaves raw HTML as text", async () => {
    render(<PromptMarkdown value={'# Rules\n\n- one\n\n<img src="x" onerror="boom()"><b id="raw">bold</b>'} />);
    const view = await screen.findByTestId("agent-tab-prompt-view");
    expect(view.querySelector("h1")?.textContent).toBe("Rules");
    expect(view.querySelectorAll("li")).toHaveLength(1);
    expect(view.querySelector("img")).toBeNull();
    expect(view.querySelector("#raw")).toBeNull();
    expect(view.textContent).toContain("<b id=");
  });
});
