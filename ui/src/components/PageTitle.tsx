import { HelpButton } from "@cjlapao/ui-kit";
import type { ReactNode } from "react";

/**
 * The title block of a management page: the heading, its one-line purpose, and — when the page
 * has something worth explaining — the kit's `HelpButton` beside the heading. `help.content` is
 * Markdown (a string is rendered as GitHub-flavoured Markdown by the kit), so the explanation
 * lives in the i18n catalogs like every other sentence. Use it as the child of `ContentActions`.
 */
export interface PageTitleProps {
  title: ReactNode;
  purpose?: ReactNode;
  help?: { title: ReactNode; content: string };
}

export function PageTitle({ title, purpose, help }: PageTitleProps) {
  return (
    <div>
      <div className="flex items-center gap-2">
        <h1 className="text-3xl font-semibold">{title}</h1>
        {help && <HelpButton title={help.title} content={help.content} color="blue" size="md" placement="bottom" />}
      </div>
      {purpose && <p className="mt-1 text-sm opacity-70">{purpose}</p>}
    </div>
  );
}
