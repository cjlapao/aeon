import { Link } from "react-router-dom";
import { TRUE_COLORS } from "@cjlapao/ui-kit";
import { ColorSwatches, Panel, Button, Input, Pill, type TrueColor } from "../../components/kit";
import { appSettings, useAppSettings } from "../../lib/appSettings";
import type { LiveController, LiveState } from "../../lib/live";
import { ThemeControl } from "./ThemeControl";
import { LanguageControl } from "./LanguageControl";
import { OrionSettingsPanel } from "../orion/OrionSettingsPanel";

const tones: readonly TrueColor[] = TRUE_COLORS;

function ToneChoice({ label, description, value, onSelect }: { label: string; description: string; value: TrueColor; onSelect: (tone: TrueColor) => void }) {
  return <div className="space-y-2" role="group" aria-label={label}>
    <div><h3 className="font-medium">{label}</h3><p className="text-sm text-slate-600 dark:text-slate-400">{description}</p></div>
    <ColorSwatches selectable colors={tones} value={value} onSelect={onSelect} size="lg" maxVisible={tones.length} defaultExpanded showNames />
    <p className="text-xs text-slate-500 dark:text-slate-400">Selected: <strong className="capitalize">{value}</strong></p>
  </div>;
}

/**
 * S72 CF8: Settings is what its name says — appearance, language and tones.
 * The thirteen management sections that were stacked here each became their
 * own page under Configuration (CF3); the link below is the door. The props
 * stay in the signature (prefixed, unread) so every existing mount compiles
 * untouched while T2's tests that pinned the old stack move to /config.
 */
export function SettingsScreen(
  _props: { controller: LiveController; live: LiveState; canWrite?: boolean },
) {
  const settings = useAppSettings();
  return <section className="space-y-5  min-w-0 p-4 md:p-7 bg-transparent" data-testid="product-settings">
    <div><p className="aeon-accent-text text-xs uppercase tracking-[.2em]">Aeon preferences</p>
      <h1 className="mt-1 text-3xl font-semibold">Settings</h1>
      <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">Appearance choices apply throughout this app and stay with this device.</p></div>
    <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
      <div className="space-y-5">
        <Panel title="Appearance" subtitle="Choose how Aeon follows your display" tone={settings.surfaceTone} variant="liquid-glass">
          <div className="space-y-2"><p className="text-sm font-medium">Color mode</p><ThemeControl />
            <p className="text-xs text-slate-600 dark:text-slate-400">System follows your operating system and updates when it changes.</p></div>
        </Panel>
        <Panel title="Language" subtitle="Translates Aeon's own screens; kit-internal labels follow the same choice" tone={settings.surfaceTone} variant="liquid-glass">
          <div className="space-y-2"><LanguageControl />
            <p className="text-xs text-slate-600 dark:text-slate-400">Detected from your browser on first run. Only the languages Aeon has its own translations for are offered here.</p></div>
        </Panel>
        <OrionSettingsPanel tone={settings.surfaceTone} />
        <Panel title="Default tones" subtitle="Used across product pages and the test bench" tone={settings.surfaceTone} variant="liquid-glass">
          <div className="grid gap-6 md:grid-cols-2">
            <ToneChoice label="Controls" description="Buttons, inputs, selection indicators, and the side menu." value={settings.controlTone}
              onSelect={(controlTone) => appSettings.set({ controlTone })} />
            <ToneChoice label="Text" description="Inherited text, page labels, links, and navigation accents." value={settings.textTone}
              onSelect={(textTone) => appSettings.set({ textTone })} />
            <ToneChoice label="Surfaces" description="Default panels and the page background tint." value={settings.surfaceTone}
              onSelect={(surfaceTone) => appSettings.set({ surfaceTone })} />
          </div>
        </Panel>
      </div>
      <div className="space-y-5">
        <Panel title="Live preview" subtitle="Changes appear as you choose a tone" tone={settings.surfaceTone} variant="liquid-glass">
          <div className="space-y-4"><p className="aeon-accent-text text-sm font-semibold">A workspace that feels like yours</p>
            <div className="flex flex-wrap gap-2"><Button size="sm">Primary action</Button><Pill tone={settings.controlTone}>Selected</Pill></div>
            <Input aria-label="Preview input" placeholder="Project name" />
            <p className="text-xs text-slate-600 dark:text-slate-400">Status colors, including blocked and approval states, keep their meaning.</p></div>
        </Panel>
        <Panel title="Other configuration" subtitle="Everything else manages the daemon, page by page" tone={settings.surfaceTone} variant="liquid-glass">
          <div className="mb-3">
            <Link className="aeon-accent-text text-sm font-semibold underline" to="/config" data-testid="settings-config-link">
              Configuration →
            </Link>
            <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">Daemon settings, backups, policy, MCP, tools, gates, environments, automation, triage, memory, continuous work and platform status each have their own page there.</p>
          </div>
          <ul className="space-y-3 text-sm">
            <li><Link className="aeon-accent-text underline" to="/bench">Test bench →</Link> Providers holds model routes and credentials</li>
            <li><Link className="aeon-accent-text underline" to="/system">System →</Link> · daemon connection, status, and which workspace you're pointed at</li>
            <li><Link className="aeon-accent-text underline" to="/components">Components →</Link> · component metadata</li>
            <li><Link className="aeon-accent-text underline" to="/projects">Projects →</Link> · the groups components belong to (S38)</li>
          </ul>
          <p className="mt-4 text-xs text-slate-600 dark:text-slate-400">Glass remains the default control and panel variant.</p>
        </Panel>
      </div>
    </div>
  </section>;
}
