function initialsOf(name: string, slug: string): string {
  const words = (name.trim() || slug.trim()).split(/[\s_-]+/).filter(Boolean);
  return (words.length > 1 ? words.slice(0, 2).map((word) => [...word][0]).join("") : [...(words[0] || "?")].slice(0, 2).join("")).toUpperCase();
}

export type ComponentIconSize = "xs" | "sm" | "md" | "lg" | "xl";

const componentIconSizes: Record<ComponentIconSize, string> = {
  xs: "h-6 w-6 text-md",
  sm: "h-8 w-8 text-xl",
  md: "h-10 w-10 text-xl",
  lg: "h-12 w-12 text-2xl",
  xl: "h-16 w-16 text-3xl",
};

export interface ComponentIconProps {
  icon: string;
  name: string;
  slug: string;
  border?: boolean;
  size?: ComponentIconSize;
}

export function ComponentIcon({ icon, name, slug, size = "md", border = !icon.startsWith("data:image/") }: ComponentIconProps) {
  const label = initialsOf(name, slug);
  return <span aria-hidden="true" className={`${componentIconSizes[size]} ${border ? "border border-slate-300/30 dark:border-white/10 p-2 rounded-xl bg-neutral-50/5 dark:bg-black/20 shadow-inner" : ""} aeon-accent-text inline-flex shrink-0 items-center justify-center overflow-hidden  font-semibold tracking-tight`}>
    {icon.startsWith("data:image/") ? <img className="h-full w-full object-cover" src={icon} alt="" /> : icon || label}
  </span>;
}
