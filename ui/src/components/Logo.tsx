import { useIsDarkMode } from "@cjlapao/ui-kit";
import logoLight from "../assets/logo.svg";
import logoAnimatedLight from "../assets/logo_animated.svg";
import logoDark from "../assets/logo_dark.svg";
import logoAnimatedDark from "../assets/logo_dark_animated.svg";

/**
 * The Aeon mark, swapped for dark mode.
 *
 * Built on `ui-kit`'s `useIsDarkMode` — a read-only observer of the `dark`
 * class on `<html>`, not a second owner of theme state. `lib/appSettings.ts`
 * is what writes that class here (`ui-kit`'s own `useTheme` is a different,
 * competing owner this app does not use), so this hook only ever reports
 * what `appSettings` already decided.
 *
 * Replaces the three-deep copy of "two `<img>` tags, one `dark:hidden`, one
 * `hidden dark:block`" that `PageLayout`, `HomeScreen`, and `SplashScreen`
 * each carried separately — one image element, picked once, rather than
 * both mounted and left to CSS.
 */
export function Logo({ className, alt = "Aeon", isAnimated = false }: { className?: string; alt?: string, isAnimated?: boolean }) {
  const isDark = useIsDarkMode();
  return <img src={isAnimated ? (isDark ? logoAnimatedDark : logoAnimatedLight) : (isDark ? logoDark : logoLight)} alt={alt} className={className} data-animated={isAnimated} />;
}
