import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../../lib/i18n";
import { emptyScheduleDraft } from "./automationModel";
import { payloadPath, presetOf, WebhookFields, WEBHOOK_PRESETS } from "./WebhookFields";

afterEach(() => cleanup());

describe("the webhook form", () => {
  it("knows its presets, and calls anything else custom", () => {
    expect(presetOf("issues:opened")).toBe("github-issue");
    expect(presetOf("pull_request:opened")).toBe("github-pr");
    expect(presetOf("")).toBe("any");
    expect(presetOf("issues:closed")).toBe("custom");
    expect(payloadPath("bug-hunt")).toBe("/v1/webhooks/bug-hunt");
    expect(payloadPath("")).toBe("/v1/webhooks/<id>");
  });

  it("picking the GitHub issue preset sets the filter and a goal that names the issue, shows the address to give GitHub, and keeps a goal the person wrote", () => {
    let draft = { ...emptyScheduleDraft(""), id: "bug-hunt" };
    const set = vi.fn((patch: Partial<typeof draft>) => { draft = { ...draft, ...patch }; });
    const view = () => <I18nProvider locales={APP_LOCALES}><WebhookFields draft={draft} set={set} secretOk headingClass="h" /></I18nProvider>;
    const { rerender } = render(view());
    expect((screen.getByTestId("webhook-url") as HTMLInputElement).value).toBe("https://<your-aeon-host>/v1/webhooks/bug-hunt");
    fireEvent.change(screen.getByTestId("webhook-preset"), { target: { value: "github-issue" } });
    expect(draft.eventFilter).toBe("issues:opened");
    expect(draft.goal).toContain("{issue.title}");
    // The person's own goal is not overwritten by choosing another preset.
    draft = { ...draft, goal: "Hunt the bug and write a failing test first" };
    rerender(view());
    fireEvent.change(screen.getByTestId("webhook-preset"), { target: { value: "github-pr" } });
    expect(draft.eventFilter).toBe("pull_request:opened");
    expect(draft.goal).toBe("Hunt the bug and write a failing test first");
    // A webhook has no clock: nothing about a timezone or a window is offered.
    expect(screen.queryByLabelText(/timezone/i)).toBeNull();
    expect(WEBHOOK_PRESETS.length).toBeGreaterThan(1);
  });
});
