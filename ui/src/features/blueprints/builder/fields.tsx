import { useState, type ReactNode } from "react";
import { useIconRenderer } from "@cjlapao/ui-kit";
import { Alert, CollapsiblePanel, Loader, Panel, FormField, IconButton, Input, MultiToggle, Pill, Select, Textarea, Toggle } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";

// The small pieces every form of the builder is made of. They are the kit's own
// controls — FormField, Input, Textarea, Select, MultiToggle, Toggle, Pill,
// CollapsiblePanel, Alert — with the app's tones, so the builder takes the kit's
// variants, sizes, corners and light/dark treatment instead of carrying its own.

export function Field({ label, hint, error, children, htmlFor }: { label: string; hint?: ReactNode; error?: string; children: ReactNode; htmlFor?: string }) {
  return <FormField label={label} labelFor={htmlFor} description={hint} error={error} size="sm">{children}</FormField>;
}

/** A titled group of fields that folds away, so an inspector shows only what is being worked on. */
export function Section({ title, hint, children, action, collapsed }: { title: string; hint?: string; children: ReactNode; action?: ReactNode; collapsed?: boolean }) {
  const { surfaceTone } = useAppSettings();
  return <CollapsiblePanel title={title} subtitle={hint} actions={action} defaultExpanded={!collapsed} variant="subtle" transparency="none" tone={surfaceTone} padding="sm" bodyClassName="space-y-3">
    {children}
  </CollapsiblePanel>;
}

export function TextField({ value, onChange, placeholder, disabled, id, mono, multiline, rows = 3, label }: {
  value: string; onChange: (v: string) => void; placeholder?: string; disabled?: boolean; id?: string; mono?: boolean; multiline?: boolean; rows?: number; label?: string;
}) {
  const { controlTone } = useAppSettings();
  const mark = mono ? "font-mono" : undefined;
  return multiline
    ? <Textarea id={id} rows={rows} tone={controlTone} aria-label={label} className={mark} value={value} placeholder={placeholder} disabled={disabled} fullWidth onChange={(e) => onChange(e.target.value)} />
    : <Input id={id} size="sm" tone={controlTone} aria-label={label} className={mark} value={value} placeholder={placeholder} disabled={disabled} onChange={(e) => onChange(e.target.value)} />;
}

export function NumberField({ value, onChange, label, min, placeholder }: { value: number | ""; onChange: (v: string) => void; label: string; min?: number; placeholder?: string }) {
  const { controlTone } = useAppSettings();
  return <Input type="number" size="sm" tone={controlTone} aria-label={label} min={min} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />;
}

export interface Option { value: string; label: string; hint?: string }

export function SelectField({ value, onChange, options, placeholder, disabled, id, label }: {
  value: string; onChange: (v: string) => void; options: Option[]; placeholder?: string; disabled?: boolean; id?: string; label?: string;
}) {
  const { controlTone } = useAppSettings();
  const known = options.some((o) => o.value === value);
  return <Select id={id} aria-label={label} size="sm" tone={controlTone} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
    {placeholder !== undefined && <option value="">{placeholder}</option>}
    {/* A value the vocabulary does not list stays visible, so it can be seen and replaced. */}
    {value !== "" && !known && <option value={value}>{value} (not offered)</option>}
    {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
  </Select>;
}

/** A short choice, with a line under the chosen one saying what it means. */
export function Segmented({ value, onChange, options, label }: { value: string; onChange: (v: string) => void; options: Option[]; label: string }) {
  const { controlTone } = useAppSettings();
  const chosen = options.find((o) => o.value === value);
  return <div className="space-y-1.5">
    <MultiToggle aria-label={label} size="sm" tone={controlTone} value={value} onChange={onChange} options={options.map((o) => ({ value: o.value, label: o.label }))} />
    {chosen?.hint && <p className="text-xs opacity-70">{chosen.hint}</p>}
  </div>;
}

export function Switch({ checked, onChange, label, hint, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; hint?: string; disabled?: boolean }) {
  const { controlTone } = useAppSettings();
  return <Toggle size="sm" tone={controlTone} label={label} description={hint} checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />;
}

/** A list of short words, added with Enter and taken away with ×. */
export function Chips({ values, onChange, placeholder, label, suggestions, mono = true }: {
  values: string[]; onChange: (v: string[]) => void; placeholder?: string; label: string; suggestions?: string[]; mono?: boolean;
}) {
  const { controlTone } = useAppSettings();
  const [draft, setDraft] = useState("");
  const add = (word: string) => {
    const w = word.trim();
    if (w !== "" && !values.includes(w)) onChange([...values, w]);
    setDraft("");
  };
  const offered = (suggestions ?? []).filter((s) => !values.includes(s));
  return <div className="space-y-2">
    {values.length > 0 && <div className="flex flex-wrap gap-1.5">
      {values.map((v) => <Pill key={v} size="sm" tone={controlTone} className={mono ? "font-mono" : undefined} onRemove={() => onChange(values.filter((x) => x !== v))}>{v}</Pill>)}
    </div>}
    <Input size="sm" tone={controlTone} aria-label={label} value={draft} placeholder={placeholder} onChange={(e) => setDraft(e.target.value)}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === ",") { e.preventDefault(); add(draft); } }} onBlur={() => add(draft)} />
    {offered.length > 0 && <div className="flex flex-wrap items-center gap-1">
      {offered.map((s) => <Pill key={s} size="sm" variant="outline" icon={<IconButton icon="Add" size="xs" variant="ghost" srLabel={`Add ${s}`} onClick={() => add(s)} />}>{s}</Pill>)}
    </div>}
  </div>;
}

export function Callout({ tone, children }: { tone: "info" | "warn" | "error" | "ok"; children: ReactNode }) {
  const intent = { info: "info", warn: "warning", error: "danger", ok: "success" }[tone] as "info" | "warning" | "danger" | "success";
  return <Alert intent={intent} size="sm" description={children} />;
}

/** The × that takes one row away: the kit's ghost icon button, named for what it removes. */
export function RemoveButton({ label, onClick, disabled }: { label: string; onClick: () => void; disabled?: boolean }) {
  return <IconButton icon="Close" size="xs" variant="ghost" color="rose" srLabel={label} tooltip={label} disabled={disabled} onClick={onClick} />;
}

/** A framed group inside a section: the kit's outlined panel at the compact inset. */
export function Card({ children, tone, className }: { children: ReactNode; tone?: "rose" | "indigo"; className?: string }) {
  const { surfaceTone } = useAppSettings();
  return <Panel variant="outlined" transparency="none" tone={tone ?? surfaceTone} padding="sm" corner="rounded-lg" clickable={false} className={className} bodyClassName="space-y-2">{children}</Panel>;
}

/** A kit icon by its registry name, drawn at the kit's own size scale. */
export function KitIcon({ name, size = "sm" }: { name: string; size?: "xs" | "sm" | "md" }) {
  const render = useIconRenderer();
  return <>{render(name, size)}</>;
}

/** What a part of the page shows while it loads: the kit's loader, centred in the room it will fill. */
export function Busy({ label }: { label: string }) {
  return <div className="flex h-full min-h-[8rem] w-full items-center justify-center" role="status" aria-label={label}><Loader size="md" title={label} /></div>;
}

/**
 * Something pressed like a button, inside something dragged. A real <button> inside a
 * draggable element swallows the press in some engines (WebKit among them), so the card
 * could not be picked up by its own text; this is a div with the button's role and keys.
 */
export function Pressable({ onPress, className, children }: { onPress: () => void; className?: string; children: ReactNode }) {
  return <div role="button" tabIndex={0} className={`cursor-[inherit] ${className ?? ""}`} onClick={onPress}
    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onPress(); } }}>{children}</div>;
}
