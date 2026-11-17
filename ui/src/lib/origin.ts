/**
 * Where a person was before they opened a loop, so the loop page can send them back to it — Home,
 * a component's Loops tab, anywhere. Kept in memory for the life of the app window: a deep link or a
 * reload has no origin, and the loop page then falls back to the Work page. Only pages that are not
 * themselves loop pages are remembered, so moving between a loop and its steps never loses the way back.
 */
let origin: string | null = null;

export function rememberOrigin(path: string): void {
  origin = path;
}

export function originPath(): string | null {
  return origin;
}

export function forgetOrigin(): void {
  origin = null;
}

/** A short, honest label for where Back goes. */
export function originLabel(path: string | null): string {
  if (!path || path === "/") return path === "/" ? "Home" : "Work";
  if (path.startsWith("/work")) return path === "/work" || path.startsWith("/work/all") ? "All projects" : "the project";
  return "the previous page";
}
