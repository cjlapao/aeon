import { ProgressSpinner } from "@cjlapao/ui-kit";
import { useAppSettings } from "../lib/appSettings";
import { Logo } from "../components/Logo";

/** Shown once, while the very first connection to the daemon is still being made — a plain daemon
 * restart or a background reconnect never show this again (App.tsx latches that). */
export function SplashScreen({ status }: { status: string }) {
  const { controlTone } = useAppSettings();
  return (
    <div className="flex h-full min-h-0 w-full flex-col items-center justify-center gap-6 bg-white dark:bg-[#101817]" role="status" aria-live="polite">
      <div className="flex flex-col items-center gap-4">
        <Logo className="h-16 w-16 object-contain" isAnimated={true} />
        <p className="text-xl font-extrabold tracking-[.18em] text-slate-900 dark:text-slate-100">
          AEON <span className="ml-3 text-xs font-medium tracking-[.22em] aeon-accent-text">LOOP WORKSPACE</span>
        </p>
      </div>
      <ProgressSpinner size="md" color={controlTone} />
      <p className="text-sm text-slate-500 dark:text-slate-400">{status}</p>
    </div>
  );
}
