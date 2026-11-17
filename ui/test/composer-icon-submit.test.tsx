import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MessageComposer } from "../src/components/agent-chat/MessageComposer";

afterEach(() => cleanup());

describe("an icon-only send button", () => {
  it("keeps the label only as the accessible name (screen-reader text), and sends when there is text", () => {
    const onSubmit = vi.fn();
    render(<MessageComposer onSubmit={onSubmit} submitLabel="Launch loop" submitIconOnly value={{ text: "build it", attachments: [] }} onChange={() => undefined} />);
    const send = screen.getByTestId("composer-submit");
    expect(screen.getByRole("button", { name: "Launch loop" })).toBe(send);
    expect(send.querySelector(".sr-only")?.textContent).toBe("Launch loop");   // present for screen readers, not drawn
    fireEvent.click(send);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
  it("is off with nothing to send", () => {
    render(<MessageComposer onSubmit={() => undefined} submitIconOnly submitLabel="Launch loop" value={{ text: "", attachments: [] }} onChange={() => undefined} />);
    expect((screen.getByTestId("composer-submit") as HTMLButtonElement).disabled).toBe(true);
  });
});

import { effortLabel, isNonTextModel } from "../src/features/home/pages/HomeLaunchComposer";

describe("what the launch composer offers", () => {
  it("leaves speech and embedding models out of the provider list, and keeps the ones that did not say they cannot write text", () => {
    expect(isNonTextModel(["tts"])).toBe(true);
    expect(isNonTextModel(["asr", "tts"])).toBe(true);
    expect(isNonTextModel(["embedding"])).toBe(true);
    expect(isNonTextModel(["text", "tts"])).toBe(false);          // an omni model that also writes text
    expect(isNonTextModel(["text", "tool-calling"])).toBe(false);
    expect(isNonTextModel(["image", "file", "tool-calling"])).toBe(false);
    expect(isNonTextModel(undefined)).toBe(false);
    expect(isNonTextModel([])).toBe(false);
  });
  it("names each reasoning effort the way a person says it", () => {
    const t = (k: string) => ({ "home.composer.effortName.xhigh": "Extra High", "home.composer.effortName.low": "Low" } as Record<string, string>)[k] ?? k;
    expect(effortLabel(t, "xhigh")).toBe("Extra High");
    expect(effortLabel(t, "low")).toBe("Low");
    expect(effortLabel(t, "turbo")).toBe("turbo");               // an effort this app has no name for shows as the daemon spells it
  });
});
