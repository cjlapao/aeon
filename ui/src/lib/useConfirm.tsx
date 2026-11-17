import { useCallback, useRef, useState, type ReactNode } from "react";
import { DeleteConfirmModal } from "../components/kit";

export interface TypeToConfirmOptions {
  title: ReactNode;
  description?: ReactNode;
  /** The exact string the person must type — almost always something from the record itself (its name), so the destructive action can't be fired by habit. */
  confirmValue: string;
  /** Shown in the modal's own instruction text, e.g. "workspace name". @default "name" */
  confirmValueLabel?: string;
  confirmLabel?: ReactNode;
}

/**
 * An imperative type-to-confirm dialog: `await confirm({...})` resolves to
 * whether the person actually typed the record's name and confirmed, or
 * cancelled. One instance covers every destructive action a screen has —
 * `confirm` can be awaited from as many different buttons as needed.
 *
 * Render `{modal}` once, anywhere in the screen's JSX; nothing else needs to
 * know it exists.
 */
export function useTypeToConfirm(): { confirm: (options: TypeToConfirmOptions) => Promise<boolean>; modal: ReactNode } {
  const [options, setOptions] = useState<TypeToConfirmOptions | null>(null);
  const resolverRef = useRef<((confirmed: boolean) => void) | null>(null);

  const resolve = useCallback((confirmed: boolean) => {
    resolverRef.current?.(confirmed);
    resolverRef.current = null;
    setOptions(null);
  }, []);

  const confirm = useCallback((next: TypeToConfirmOptions): Promise<boolean> => {
    // A second confirm() while one is already open resolves the first as
    // cancelled rather than leaving its promise hanging forever.
    resolverRef.current?.(false);
    setOptions(next);
    return new Promise<boolean>((res) => {
      resolverRef.current = res;
    });
  }, []);

  const modal = options ? (
    <DeleteConfirmModal
      isOpen
      title={options.title}
      description={options.description}
      confirmValue={options.confirmValue}
      confirmValueLabel={options.confirmValueLabel}
      confirmLabel={options.confirmLabel}
      onClose={() => resolve(false)}
      onConfirm={() => resolve(true)}
    />
  ) : null;

  return { confirm, modal };
}
