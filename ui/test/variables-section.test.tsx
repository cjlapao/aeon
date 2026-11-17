import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { VariablesSection } from "../src/features/settings/variables/VariablesSection";
import { APP_LOCALES } from "../src/lib/i18n";
import { LiveController } from "../src/lib/live";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

const live = { state: "current", endpoint: "/run/user/1000/aeon/rpc.sock", daemonVersion: "9.9.9", protocolVersion: 7, generation: 1 } as const satisfies LiveState;

const LIST = {
  variables: [
    { name: "GITHUB_TOKEN", description: "CI token", secret: true, agentVisible: false, updatedAt: "2026-11-08T12:00:00Z" },
    { name: "REGION", secret: false, agentVisible: true, value: "eu-west-1", updatedAt: "2026-11-08T12:01:00Z" },
    { name: "API_URL", project: "prj-1", projectName: "Aeon", secret: false, agentVisible: false, value: "https://staging", updatedAt: "2026-11-08T12:02:00Z" },
  ],
};

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); resetFakeDaemon(); });

function renderSection(extra: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({
    responses: {
      "vault.list": LIST,
      "vault.changes": { changes: [{ id: 1, name: "GITHUB_TOKEN", op: "set", summary: "created variable GITHUB_TOKEN (secret=true, agentVisible=false)", actorKind: "user", actorId: "1000", occurredAt: "2026-11-08T12:00:00Z" }] },
      "component.list": { components: [{ id: "prj-1", name: "Aeon", parentId: "" }] },
      "vault.set": { variable: { name: "X", secret: true, agentVisible: false, updatedAt: "" } },
      "vault.delete": { deleted: true },
      ...extra,
    },
  });
  render(<I18nProvider locales={APP_LOCALES}><VariablesSection controller={new LiveController()} live={live} canWrite /></I18nProvider>);
  return daemon;
}

describe("variables & secrets", () => {
  it("lists variables: a secret is hidden and says so, a plain one shows its value, and agent access is stated", async () => {
    renderSection();
    await screen.findByTestId("variable-GITHUB_TOKEN");
    expect(screen.getByTestId("kind-GITHUB_TOKEN").textContent).toBe("secret");
    expect(screen.getByTestId("value-GITHUB_TOKEN").textContent).toBe("••••••••");
    expect(screen.getByTestId("agents-GITHUB_TOKEN").textContent).toBe("hidden from agents");
    expect(screen.getByTestId("value-REGION").textContent).toBe("eu-west-1");
    expect(screen.getByTestId("agents-REGION").textContent).toBe("agents can read");
    expect(screen.getByTestId("scope-API_URL").textContent).toContain("project: Aeon");
  });

  it("a new variable is a secret hidden from agents, and the value is only ever sent", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("variable-add"));
    const form = await screen.findByTestId("variable-form");
    expect((within(form).getByLabelText("this is a secret") as HTMLInputElement).checked).toBe(true);
    expect((within(form).getByLabelText("agents can read this") as HTMLInputElement).checked).toBe(false);
    expect((within(form).getByLabelText("variable value") as HTMLInputElement).type).toBe("password");
    expect((screen.getByTestId("variable-save") as HTMLButtonElement).disabled).toBe(true); // a new variable needs a value
    fireEvent.change(within(form).getByLabelText("variable name"), { target: { value: "STRIPE_KEY" } });
    fireEvent.change(within(form).getByLabelText("variable value"), { target: { value: "sk_live_abc" } });
    fireEvent.click(screen.getByTestId("variable-save"));
    await waitFor(() => expect(daemon.paramsOf("vault.set")).toEqual({ name: "STRIPE_KEY", secret: true, agentVisible: false, description: "", value: "sk_live_abc" }));
    // The value is nowhere on the page after the save.
    await waitFor(() => expect(screen.queryByTestId("variable-form")).toBeNull());
    expect(document.body.textContent).not.toContain("sk_live_abc");
  });

  it("making it plain opens agent access until the person decides, and sharing a secret warns", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("variable-add"));
    const form = await screen.findByTestId("variable-form");
    const secret = within(form).getByLabelText("this is a secret") as HTMLInputElement;
    const agents = within(form).getByLabelText("agents can read this") as HTMLInputElement;
    fireEvent.click(secret); // plain
    expect(agents.checked).toBe(true);
    expect((within(form).getByLabelText("variable value") as HTMLInputElement).type).toBe("text");
    fireEvent.click(secret); // secret again, untouched agents box follows
    expect(agents.checked).toBe(false);
    fireEvent.click(agents); // the person shares a secret on purpose
    expect(screen.getByTestId("agent-secret-warning").textContent).toContain("Agents will be able to read this secret");
    fireEvent.click(secret); // a deliberate choice is not overridden by flipping secret
    expect(agents.checked).toBe(true);
  });

  it("an edit starts empty, keeps the stored value, and cannot rename or move the variable", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("edit-GITHUB_TOKEN"));
    const form = await screen.findByTestId("variable-form");
    expect((within(form).getByLabelText("variable value") as HTMLInputElement).value).toBe("");
    expect((within(form).getByLabelText("variable name") as HTMLInputElement).disabled).toBe(true);
    expect((within(form).getByLabelText("variable scope") as HTMLSelectElement).disabled).toBe(true);
    fireEvent.click(within(form).getByLabelText("agents can read this"));
    fireEvent.click(screen.getByTestId("variable-save"));
    await waitFor(() => expect(daemon.paramsOf("vault.set")).toEqual({ name: "GITHUB_TOKEN", secret: true, agentVisible: true, description: "CI token" }));
  });

  it("a project override is created for that project only", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("variable-add"));
    const form = await screen.findByTestId("variable-form");
    await within(form).findByText("only Aeon");
    fireEvent.change(within(form).getByLabelText("variable name"), { target: { value: "API_URL" } });
    fireEvent.change(within(form).getByLabelText("variable scope"), { target: { value: "prj-1" } });
    fireEvent.change(within(form).getByLabelText("variable value"), { target: { value: "https://x" } });
    fireEvent.click(screen.getByTestId("variable-save"));
    await waitFor(() => expect((daemon.paramsOf("vault.set") as { project?: string }).project).toBe("prj-1"));
  });

  it("shows the manager's own sentence when it refuses, and keeps the form", async () => {
    const what = "refused: PATH changes what a process IS rather than what it knows — the name is reserved";
    renderSection({ "vault.set": () => { throw JSON.stringify({ code: -32602, message: "the MCP management door refused", data: { what, means: "", todo: "", fatal: false } }); } });
    fireEvent.click(await screen.findByTestId("variable-add"));
    const form = await screen.findByTestId("variable-form");
    fireEvent.change(within(form).getByLabelText("variable name"), { target: { value: "PATH" } });
    fireEvent.change(within(form).getByLabelText("variable value"), { target: { value: "/x" } });
    fireEvent.click(screen.getByTestId("variable-save"));
    expect((await screen.findByTestId("variable-refusal")).textContent).toContain(what);
    expect((screen.getByLabelText("variable name") as HTMLInputElement).value).toBe("PATH");
  });

  it("deletes one variable in its own scope and shows the ledger", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("delete-API_URL"));
    // Delete asks first; nothing is sent until the person agrees.
    expect(daemon.methods()).not.toContain("vault.delete");
    fireEvent.click(within(await screen.findByTestId("variable-delete-ask")).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(daemon.paramsOf("vault.delete")).toEqual({ name: "API_URL", project: "prj-1" }));
    fireEvent.click(screen.getByRole("tab", { name: "History" }));
    expect((await screen.findByTestId("variables-history")).textContent).toContain("created variable GITHUB_TOKEN");
  });
});
