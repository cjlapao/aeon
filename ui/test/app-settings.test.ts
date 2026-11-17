import { afterEach, describe, expect, it } from "vitest";
import { appSettings, startAppSettings } from "../src/lib/appSettings";

afterEach(() => {
  window.localStorage.removeItem("aeon-app-settings-v1");
  window.localStorage.removeItem("aeon-theme");
  appSettings.reset();
});

describe("app settings service", () => {
  it("accepts settings from another app window and migrates the earlier theme choice", () => {
    const stop = startAppSettings();
    try {
      window.localStorage.setItem("aeon-app-settings-v1", JSON.stringify({ theme: "dark", controlTone: "blue", textTone: "orange", surfaceTone: "violet" }));
      window.dispatchEvent(new StorageEvent("storage", { key: "aeon-app-settings-v1" }));
      expect(appSettings.getSnapshot()).toMatchObject({ theme: "dark", controlTone: "blue", textTone: "orange", surfaceTone: "violet" });
      expect(document.documentElement.classList.contains("dark")).toBe(true);
      expect(document.documentElement.dataset.controlTone).toBe("blue");
      expect(document.documentElement.style.getPropertyValue("--aeon-background-glow")).toBe("var(--color-blue-900)");
      window.localStorage.removeItem("aeon-app-settings-v1");
      window.localStorage.setItem("aeon-theme", "light");
      window.dispatchEvent(new StorageEvent("storage", { key: "aeon-theme" }));
      expect(appSettings.getSnapshot().theme).toBe("light");
      expect(appSettings.getSnapshot().controlTone).toBe("emerald");
      expect(document.documentElement.style.getPropertyValue("--aeon-background-glow")).toBe("var(--color-emerald-100)");
    } finally { stop(); }
  });
});
