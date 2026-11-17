import { lazy, Suspense } from "react";
import { parseFrontmatter } from "../../../lib/frontmatter";

const MarkdownBody = lazy(async () => {
  const [{ default: ReactMarkdown }, { default: gfm }] = await Promise.all([import("react-markdown"), import("remark-gfm")]);
  return { default: ({ body }: { body: string }) => <ReactMarkdown remarkPlugins={[gfm]}>{body}</ReactMarkdown> };
});

export function MarkdownView({ content }: { content: string }) {
  const { body, data, had } = parseFrontmatter(content);
  return <article className="aeon-markdown min-w-0 break-words">
    {had && <dl className="mb-5 grid gap-x-4 gap-y-1 rounded-lg border border-white/10 p-3 text-xs sm:grid-cols-[max-content_1fr]">
      {Object.entries(data).map(([key, value]) => <div key={key} className="contents"><dt className="font-semibold">{key}</dt><dd className="break-all">{value}</dd></div>)}
    </dl>}
    <Suspense fallback={<p>Rendering Markdown…</p>}><MarkdownBody body={body} /></Suspense>
  </article>;
}
