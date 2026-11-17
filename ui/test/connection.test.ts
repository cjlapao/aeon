import { describe, expect, it } from "vitest";
import { connected, connecting, describe as label, failed } from "../src/lib/connection";

const CONNECTED = connected("0.4.2", 7, "/run/user/1000/aeon/rpc.sock");
const FAILED = failed("permission denied", "/run/user/1000/aeon/rpc.sock");

describe("the three-state connection", () => {
  it("gives each state a different sentence", () => {
    const labels = [label(connecting), label(CONNECTED), label(FAILED)];
    expect(new Set(labels).size).toBe(3);
  });

  it("names the daemon's version when connected", () => {
    expect(label(CONNECTED)).toContain("0.4.2");
  });

  // A bare number reads as a constant. The word is part of the requirement.
  it("marks the protocol version as negotiated", () => {
    expect(label(CONNECTED)).toContain("protocol 7");
    expect(label(CONNECTED)).toContain("negotiated");
  });

  it("carries the reason when it failed", () => {
    expect(label(FAILED)).toContain("permission denied");
  });

  // The type itself forbids a protocol number in the disconnected states, which
  // is stronger than a test: it is the defect T7 is written against, made
  // unrepresentable.
  it("has no protocol version to show unless connected", () => {
    expect("protocolVersion" in connecting).toBe(false);
    expect("protocolVersion" in FAILED).toBe(false);
    expect("protocolVersion" in CONNECTED).toBe(true);
  });

  // The thing the ui-kit cannot express, asserted here so a later refactor
  // cannot quietly map onto its ConnectionState.
  it("is not the ui-kit's ConnectionState", () => {
    const ours = [connecting.kind, CONNECTED.kind, FAILED.kind];
    const uiKit = ["flowing", "stopped", "disabled"];
    expect(ours.some((k) => uiKit.includes(k))).toBe(false);
  });
});
