import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * S59 T11 test fixtures — the REAL conformance answers, read from the same JSON
 * files the Go suite round-trips (the discipline `gate-fixtures.ts` set). A UI
 * test that hand-typed its own daemon answer drifts from the wire; these answer
 * with the fixtures, so every assertion is made against what the daemon
 * actually says.
 *
 * Lives in ui/test/ (not in the environments folder) on purpose: the production
 * sources are the guarded ones; fixture data is allowed to name protocol words
 * because naming them IS its job.
 */

const CONFORMANCE = join(__dirname, "../../internal/protocol/conformance");

export function fixtureResult(tree: string, file: string): unknown {
  const doc = JSON.parse(readFileSync(join(CONFORMANCE, tree, file), "utf8")) as {
    response: { result: unknown };
  };
  return doc.response.result;
}

/** The whole fixture document, when a test wants the refusal. */
export function fixtureDoc(tree: string, file: string): {
  response: { result?: unknown; error?: { code: number; message: string; data?: Record<string, unknown> } };
} {
  return JSON.parse(readFileSync(join(CONFORMANCE, tree, file), "utf8"));
}

/**
 * A refusal as the wire sends it. The fake daemon re-throws a thrown value as
 * its bare message, and a message is too small a bag for a refusal: the parts
 * live in `data`. So the throw is the raw JSON string — the same text the real
 * socket hands the bridge, where `toRpcError` parses it back into the three
 * parts the panel renders verbatim.
 */
export function refusedFixture(tree: string, file: string): unknown {
  return JSON.stringify(fixtureDoc(tree, file).response.error);
}

export const environmentVocabulary = fixtureResult("environment", "vocabulary-definitions.json");
export const environmentListTwo = fixtureResult("environment", "list-two-environments.json");
export const environmentShowOne = fixtureResult("environment", "show-one-environment.json");
export const environmentChanges = fixtureResult("environment", "changes-one-write.json");
export const environmentProvisions = fixtureResult("environment", "provisions-ready-row.json");
export const environmentEffective = fixtureResult("environment", "effective-workspace-binding.json");
export const environmentBindDryRun = fixtureResult("environment", "bind-dry-run-statement.json");
export const environmentCreateOk = fixtureResult("environment", "create-ok.json");
export const environmentUpdateOk = fixtureResult("environment", "update-new-revision.json");
export const environmentArchive = fixtureResult("environment", "archive-soft-delete.json");
export const environmentRestore = fixtureResult("environment", "restore-archived.json");
export const environmentUnbind = fixtureResult("environment", "unbind-component.json");
export const environmentTeardown = fixtureResult("environment", "teardown-removed-row.json");

export const secretRefList = fixtureResult("secretref", "list-one-reference.json");
export const secretRefShow = fixtureResult("secretref", "show-one-reference.json");
export const secretRefCheck = fixtureResult("secretref", "check-unresolvable.json");
export const secretRefCreate = fixtureResult("secretref", "create-reference.json");

/** A provision whose removal FAILED: the ledger carries the driver's sentence. */
export function stuckProvision(overrides: Record<string, unknown> = {}) {
  return {
    id: "prv_stuck_0001",
    environmentId: "env_9a8b7c6d",
    componentRoot: "/home/cjlapao/code/acme/api",
    loopSlug: "release-2026-11",
    backend: "container",
    handle: "aeon-s59-acme-api-dead01",
    owner: "daemon:1000",
    state: "tearing_down",
    createdAt: "2026-11-03T09:30:00Z",
    readyAt: "2026-11-03T09:30:04Z",
    lastError: "docker rm: the container is still running after the stop timeout — the driver did not reap it",
    ...overrides,
  };
}

/**
 * The row the seed leaves behind on a store that predates S59: the LEGACY
 * backend, bound at the workspace scope. `legacy` is the daemon's boolean and
 * the banner below it is the daemon's sentence — the test supplies both because
 * no S59 fixture lists an inheriting row (the seed never lays one).
 */
export function legacyRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "env_legacy_0001",
    slug: "host-inherit",
    name: "Daemon inherited",
    description: "The continuity row on stores that predate S59.",
    backend: "host-inherit",
    origin: "system",
    legacy: true,
    revision: 1,
    spec: {
      env: { passthrough: [], set: {}, secrets: [] },
      home: "real",
      limits: {},
      egress: { mode: "unenforced", allow: [] },
      image: null,
      services: [],
      preview: null,
    },
    createdAt: "2026-11-03T09:00:00Z",
    updatedAt: "2026-11-03T09:00:00Z",
    capabilities: ["wall-limit"],
    bindings: [{ scopeKind: "workspace", environmentId: "env_legacy_0001", boundAt: "2026-11-03T09:00:00Z" }],
    ...overrides,
  };
}

/** The component roster the pickers read; two rows so ordering is visible. */
export const componentList = {
  components: [
    {
      id: "cmp_42",
      name: "acme-api",
      slug: "acme-api",
      root: "/home/cjlapao/code/acme/api",
      rootId: "cmp_42",
      createdAt: "2026-11-01T00:00:00Z",
      updatedAt: "2026-11-01T00:00:00Z",
    },
    {
      id: "cmp_7",
      name: "aeon-ui",
      slug: "aeon-ui",
      root: "/home/cjlapao/code/cjlapao/opencode-agents/aeon/ui",
      rootId: "cmp_7",
      createdAt: "2026-11-02T00:00:00Z",
      updatedAt: "2026-11-02T00:00:00Z",
    },
  ],
};

/** Every door a mounted EnvironmentsSection opens, answered with the fixtures. */
export const environmentsBaseResponses: Record<string, unknown> = {
  "environment.vocabulary": environmentVocabulary,
  "environment.list": environmentListTwo,
  "environment.show": environmentShowOne,
  "environment.changes": environmentChanges,
  "environment.effective": environmentEffective,
  "environment.provisions": [...(environmentProvisions as unknown[]), stuckProvision()],
  "environment.bind": environmentBindDryRun,
  "environment.create": environmentCreateOk,
  "environment.update": environmentUpdateOk,
  "environment.archive": environmentArchive,
  "environment.restore": environmentRestore,
  "environment.unbind": environmentUnbind,
  "environment.teardown": environmentTeardown,
  "secretref.list": secretRefList,
  "secretref.show": secretRefShow,
  "secretref.check": secretRefCheck,
  "secretref.create": secretRefCreate,
  "component.list": componentList,
  "component.show": { ...componentList.components[0] },
};
