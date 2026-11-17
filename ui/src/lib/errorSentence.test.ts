import { describe, expect, it } from "vitest";
import { RpcError, errorSentence, toRpcError } from "./bridge";

describe("the sentence a failed call shows", () => {
  it("is the domain's own sentence for a refusal code, not the generic title", () => {
    const err = toRpcError(JSON.stringify({
      code: -31853, message: "the MCP management door refused",
      data: { what: 'refused: public: server name "Github" is not a namespace (lower-case letters, digits, _ and -)', means: "", todo: "", fatal: false },
    }));
    expect(errorSentence(err)).toContain('server name "Github" is not a namespace');
  });

  it("keeps the message of anything that is not a refusal", () => {
    const failed = new RpcError(-32603, "the store is unavailable", { what: "a longer explanation", means: "", todo: "", fatal: true });
    expect(errorSentence(failed)).toBe("the store is unavailable");
    expect(errorSentence(new RpcError(0, "not connected to the daemon"))).toBe("not connected to the daemon");
  });
});
