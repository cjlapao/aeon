import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SkillsPage } from "../src/features/test-bench/pages/SkillsPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { getBenchPage } from "../src/features/test-bench/lib/benchRegistry";
import type { SkillListResult, SkillView } from "../src/lib/skills";

// S65's catalog page, held to what the slice is for:
//
//   * every discovered skill is LISTED, broken ones included (P31 —
//     invalid is a rendering, never a silent drop), and the issues print
//     with their own severity words;
//   * the user root is READ-ONLY here: a managed row gets a remove
//     affordance and a user row provably does not;
//   * install sends exactly what was typed to `skills.install` and
//     re-reads the catalog afterward — the daemon decides what lands;
//   * the truncation flag survives to the screen: a cut body says it was
//     cut and says the disk holds the whole file (three-state honesty).

function skill(overrides: Partial<SkillView> & { name: string; status: SkillView["status"] }): SkillView {
  return {
    source: "user",
    root: "/home/dev/.agents/skills",
    path: "/home/dev/.agents/skills/" + overrides.name,
    files: { scripts: 0, references: 0, assets: 0, other: 1 },
    bodyBytes: 100,
    bodyLines: 8,
    totalBytes: 200,
    scannedAt: "2026-10-30T18:00:00Z",
    ...overrides,
  };
}

const PDF = skill({
  name: "pdf",
  status: "valid",
  description: "Extract text and tables from PDF files.",
  files: { scripts: 2, references: 3, assets: 0, other: 1 },
});

const BROKEN = skill({
  name: "broken",
  status: "invalid",
  source: "managed",
  root: "/home/dev/.local/share/aeon/skills",
  path: "/home/dev/.local/share/aeon/skills/broken",
  issues: [{ field: "name", detail: "the frontmatter has no name", severity: "error" }],
});

function listing(skills: SkillView[]): SkillListResult {
  return {
    skills,
    userRoot: "/home/dev/.agents/skills",
    managedRoot: "/home/dev/.local/share/aeon/skills",
    scannedAt: "2026-10-30T18:00:00Z",
    summary: {
      valid: skills.filter((s) => s.status === "valid").length,
      warn: skills.filter((s) => s.status === "warn").length,
      invalid: skills.filter((s) => s.status === "invalid").length,
      shadowed: skills.filter((s) => s.shadowed).length,
    },
  };
}

function mount(fake: FakeDaemon): void {
  render(<SkillsPage />);
  void fake;
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("registration", () => {
  it("registers itself under /bench/skills and needs the daemon", async () => {
    await import("../src/features/test-bench/pages/registerAll");
    const def = getBenchPage("skills");
    expect(def).toBeDefined();
    expect(def?.path).toBe("/bench/skills");
    expect(def?.needsConnection).toBe(true);
  });
});

describe("the mount read", () => {
  it("asks the catalog on connect and lists every discovery, broken included", async () => {
    const fake = installFakeDaemon({
      responses: { "skills.list": listing([PDF, BROKEN]) },
    });
    mount(fake);
    await waitFor(() => expect(fake.methods()).toContain("skills.list"));
    await waitFor(() => expect(screen.queryByTestId("skills-row-pdf")).not.toBeNull());
    expect(screen.getByTestId("skills-row-pdf").textContent).toContain("Extract text");
    expect(screen.getByTestId("skills-row-pdf").textContent).toContain("2 scripts");
    // The invalid one is NOT dropped — it is the third state, rendered.
    expect(screen.getByTestId("skills-row-broken").textContent).toContain("the frontmatter has no name");
    // Roots and counts spoken once, from the wire.
    expect(document.body.textContent).toContain("/home/dev/.agents/skills");
    expect(document.body.textContent).toContain("1 invalid");
  });

  it("an empty catalog is reported, not hidden", async () => {
    const fake = installFakeDaemon({ responses: { "skills.list": listing([]) } });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-empty")).not.toBeNull());
    expect(screen.getByTestId("skills-empty").textContent).toContain("reported");
  });

  it("a shadowed row says what shadows it", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([
          skill({ name: "caveman", status: "valid", shadowed: true, shadowedBy: "/home/dev/.agents/skills/caveman", source: "managed" }),
        ]),
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-caveman")).not.toBeNull());
    expect(screen.getByTestId("skills-row-caveman").textContent).toContain("shadowed by /home/dev/.agents/skills/caveman");
  });
});

describe("the user root is read-only", () => {
  it("managed rows get remove; user rows provably do not", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([PDF, BROKEN]),
        "skills.get": { skill: PDF, body: "# pdf\nbody" },
        "skills.remove": { name: "broken", path: BROKEN.path, revealed: false },
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-pdf")).not.toBeNull());

    fireEvent.click(screen.getByTestId("skills-row-broken").querySelector("button")!);
    await waitFor(() => expect(screen.queryByTestId("skills-remove-broken")).not.toBeNull());

    fireEvent.click(screen.getByTestId("skills-row-pdf").querySelector("button")!);
    await waitFor(() => expect(fake.paramsOf("skills.get")).toBeDefined());
    expect(screen.queryByTestId("skills-remove-pdf")).toBeNull();
    expect(screen.getByTestId("skills-row-pdf").textContent).toContain("never deletes");
  });

  it("remove calls skills.remove with the name", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([BROKEN]),
        "skills.get": { skill: BROKEN, body: "body" },
        "skills.remove": { name: "broken", path: BROKEN.path, revealed: true },
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-broken")).not.toBeNull());
    fireEvent.click(screen.getByTestId("skills-row-broken").querySelector("button")!);
    await waitFor(() => expect(screen.queryByTestId("skills-remove-broken")).not.toBeNull());
    fireEvent.click(screen.getByTestId("skills-remove-broken"));
    await waitFor(() => expect(fake.paramsOf("skills.remove")).toEqual({ name: "broken" }));
  });
});

describe("the detail read", () => {
  it("expands to the SKILL.md body, and a cut body says it was cut", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([PDF]),
        "skills.get": { skill: { ...PDF, bodyBytes: 999999 }, body: "# pdf\nthe whole body", truncated: true },
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-pdf")).not.toBeNull());
    fireEvent.click(screen.getByTestId("skills-row-pdf").querySelector("button")!);
    await waitFor(() => expect(fake.paramsOf("skills.get")).toEqual({ name: "pdf" }));
    await waitFor(() => expect(screen.getByTestId("skills-row-pdf").textContent).toContain("the whole body"));
    expect(screen.getByTestId("skills-row-pdf").textContent).toContain("cut at the wire cap");
    expect(screen.getByTestId("skills-row-pdf").textContent).toContain("file on disk is complete");
  });
});

describe("install", () => {
  it("sends the typed locator and flags verbatim, then re-reads", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([PDF]),
        "skills.install": { skill: { ...PDF, source: "managed" }, replaced: true },
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-pdf")).not.toBeNull());

    fireEvent.change(screen.getByPlaceholderText(/owner\/repo/), { target: { value: "acme/skills/pdf@v1" } });
    fireEvent.change(screen.getByPlaceholderText("main"), { target: { value: "release" } });
    const forceBox = screen.getByRole("checkbox") as HTMLInputElement;
    fireEvent.click(forceBox);
    expect(forceBox.checked).toBe(true);
    fireEvent.click(screen.getByTestId("skills-install"));
    await waitFor(() =>
      expect(fake.paramsOf("skills.install")).toEqual({
        source: "acme/skills/pdf@v1",
        ref: "release",
        force: true,
      }),
    );
    // Re-read after landing (the catalog is the daemon's, so we ASK again).
    expect(fake.methods().filter((m) => m === "skills.list").length).toBeGreaterThanOrEqual(2);
  });

  it("a refused install surfaces the daemon's sentence, and nothing is prettied", async () => {
    const fake = installFakeDaemon({
      responses: {
        "skills.list": listing([PDF]),
        "skills.install": () => {
          // The refusal in the shape the shell actually serialises (the
          // automation.test.tsx helper's lesson): the daemon's three parts,
          // verbatim, or the guard on the screen has nothing to show.
          throw JSON.stringify({
            code: -31820,
            message: "the skills operation was refused",
            data: {
              what: 'a managed skill named "pdf" already exists — re-run with force',
              means: "nothing landed",
              todo: "add force",
              fatal: false,
            },
          });
        },
      },
    });
    mount(fake);
    await waitFor(() => expect(screen.queryByTestId("skills-row-pdf")).not.toBeNull());
    fireEvent.change(screen.getByPlaceholderText(/owner\/repo/), { target: { value: "acme/pdf" } });
    fireEvent.click(screen.getByTestId("skills-install"));
    await waitFor(() => expect(document.body.textContent).toContain("re-run with force"));
  });
});
