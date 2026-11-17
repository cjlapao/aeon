import type { ComponentProps } from "react";
import {
  Alert as KitAlert, Button as KitButton, EmptyState as KitEmptyState,
  FormSection as KitFormSection, Hero as KitHero, InfoRow as KitInfoRow,
  Input as KitInput,
  MultiToggle as KitMultiToggle, PageBodyLayout as KitPageBodyLayout, Panel as KitPanel, Pill as KitPill,
  SplitView as KitSplitView, Table as KitTable, type TableProps, Tabs as KitTabs,
} from "@cjlapao/ui-kit";
import { useAppSettings } from "../lib/appSettings";

// Aeon's surface default. Explicit variants on individual controls still win.
export * from "@cjlapao/ui-kit";
export const Alert = (props: ComponentProps<typeof KitAlert>) => <KitAlert variant="glass" {...props} />;
export const Button = (props: ComponentProps<typeof KitButton>) => { const { controlTone } = useAppSettings(); return <KitButton variant="glass" color={controlTone} {...props} />; };
export const EmptyState = (props: ComponentProps<typeof KitEmptyState>) => <KitEmptyState variant="simple" {...props} />;
export const FormSection = (props: ComponentProps<typeof KitFormSection>) => <KitFormSection variant="glass" {...props} />;
export const Hero = (props: ComponentProps<typeof KitHero>) => <KitHero variant="glass" {...props} />;
export const InfoRow = (props: ComponentProps<typeof KitInfoRow>) => <KitInfoRow variant="glass" {...props} />;
export const Input = (props: ComponentProps<typeof KitInput>) => { const { controlTone } = useAppSettings(); return <KitInput variant="glass" tone={controlTone} {...props} />; };
export const MultiToggle = (props: ComponentProps<typeof KitMultiToggle>) => { const { controlTone } = useAppSettings(); return <KitMultiToggle variant="glass" tone={controlTone} {...props} />; };
export const PageBodyLayout = (props: ComponentProps<typeof KitPageBodyLayout>) => { const { surfaceTone } = useAppSettings(); return <KitPageBodyLayout variant="glass" tone={surfaceTone} {...props} />; };
export const Panel = (props: ComponentProps<typeof KitPanel>) => { const { surfaceTone } = useAppSettings(); return <KitPanel variant="glass" tone={surfaceTone} {...props} />; };
export const Pill = (props: ComponentProps<typeof KitPill>) => { const { controlTone } = useAppSettings(); return <KitPill variant="glass" tone={controlTone} {...props} />; };
export const SplitView = (props: ComponentProps<typeof KitSplitView>) => <KitSplitView variant="glass" {...props} />;
export const Tabs = (props: ComponentProps<typeof KitTabs>) => <KitTabs variant="minimal" {...props} />;

// S73 T6: the kit's Table, which this app had never mounted, on Aeon's surface
// default — the same one `Panel` wears, because a table IS the card beside it.
//
// `Table` is generic, so this wrapper is generic too: `ComponentProps<typeof
// KitTable>` would pin the row type to the kit's inference and every column's
// `render` would lose its row. `TableProps<T>` is the prop type, T included.
//
// Two families of surface prop exist and only one belongs here. `variant` and
// `tone` are the CONTAINER — the panel the table draws its card with.
// `panelVariant`/`panelTone` are the header's TOOLBAR MENUS (columns, group by,
// sticky), and both default to `"auto"`: they follow the table's own variant,
// with the kit's one map-over that puts a glass table's popups on the frosted
// `default` surface, which is the readable one over any page. So the default is
// what this app wants, and a wrapper that pinned them would be a second surface
// opinion to disagree with. Explicit props still win, as everywhere above.
export const Table = <T,>(props: TableProps<T>) => { const { surfaceTone } = useAppSettings(); return <KitTable<T> variant="glass" tone={surfaceTone} {...props} />; };
