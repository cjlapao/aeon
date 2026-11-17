import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { useTheme } from "@cjlapao/ui-kit";
import { appSettings, startAppSettings } from "../src/lib/appSettings";

function UsesKitTheme() { useTheme(); return null; }

describe("the kit's own theme hook", () => {
  afterEach(() => { cleanup(); appSettings.set({ theme: "system" }); document.documentElement.classList.remove("dark"); });
  it("does not override the theme chosen in Settings when a kit chart mounts", () => {
    window.localStorage.removeItem("ui-kit-demo-theme");
    const stop = startAppSettings();
    appSettings.set({ theme: "dark" });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    render(<UsesKitTheme />);   // what a kit chart does on mount; it used to switch the page to light
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    stop();
  });
});
