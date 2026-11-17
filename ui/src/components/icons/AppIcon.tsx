import type { SVGProps } from "react";
import { ICONS, type IconName } from "./icons";

export interface AppIconProps extends Omit<SVGProps<SVGSVGElement>, "name" | "dangerouslySetInnerHTML" | "children"> {
  name: IconName;
  /** Width and height. A number is pixels. @default 14 (the drawn size) */
  size?: number | string;
  /** Overrides the icon's own stroke width. */
  strokeWidth?: number;
  /**
   * An accessible name. Omit it and the icon is decoration — hidden from
   * assistive technology, which is right beside a label and wrong standing
   * alone (an icon-only button should label the button instead).
   */
  title?: string;
}

const escapeText = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

/**
 * One glyph from Aeon's own set (`src/icons/`, see icons.ts) — the
 * app-side companion to the kit's CustomIcon, for markup Aeon draws itself.
 */
export function AppIcon({ name, size = 14, strokeWidth, title, ...rest }: AppIconProps) {
  const icon = ICONS[name];
  if (!icon) return null;
  const { attrs } = icon;
  return <svg
    xmlns="http://www.w3.org/2000/svg"
    viewBox={icon.viewBox}
    width={size}
    height={size}
    fill={attrs.fill ?? "none"}
    stroke={attrs.stroke}
    strokeWidth={strokeWidth ?? attrs["stroke-width"]}
    strokeLinecap={attrs["stroke-linecap"] as SVGProps<SVGSVGElement>["strokeLinecap"]}
    strokeLinejoin={attrs["stroke-linejoin"] as SVGProps<SVGSVGElement>["strokeLinejoin"]}
    aria-hidden={title ? undefined : true}
    role={title ? "img" : undefined}
    focusable="false"
    data-icon={name}
    {...rest}
    // The markup is our own files, sanitised at load (icons.ts).
    dangerouslySetInnerHTML={{ __html: (title ? `<title>${escapeText(title)}</title>` : "") + icon.body }}
  />;
}
