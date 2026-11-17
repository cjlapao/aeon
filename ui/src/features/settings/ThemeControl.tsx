import { Moon, MultiToggle, Sun, ThemeAuto } from "../../components/kit";
import { appSettings, useAppSettings, type AeonTheme } from "../../lib/appSettings";

export type { AeonTheme } from "../../lib/appSettings";

export function ThemeControl() {
  const { theme } = useAppSettings();

  return <MultiToggle
    aria-label="Theme"
    options={[
      { value: "light", label: "Light", icon: <Sun className="h-4 w-4" /> },
      { value: "dark", label: "Dark", icon: <Moon className="h-4 w-4" /> },
      { value: "system", label: "System", icon: <ThemeAuto className="h-4 w-4" /> },
    ]}
    value={theme} onChange={(value) => appSettings.set({ theme: value as AeonTheme })}
    size="sm" indicator="tonal"
  />;
}
