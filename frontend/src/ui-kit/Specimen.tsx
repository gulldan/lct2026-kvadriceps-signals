import type { ReactNode } from "react";

export function Specimen({
  id,
  title,
  description,
  code,
  children,
}: {
  id: string;
  title: string;
  description: string;
  code: string;
  children: ReactNode;
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-title`}
      className="grid scroll-mt-14 gap-6 border-t py-10 first:border-t-0 sm:py-12 lg:grid-cols-[15rem_minmax(0,1fr)] lg:gap-10"
    >
      <div>
        <h2
          id={`${id}-title`}
          className="text-lg leading-snug font-semibold tracking-tight text-balance"
        >
          {title}
        </h2>
        <p className="mt-2 max-w-prose text-sm leading-relaxed text-muted-foreground">
          {description}
        </p>
      </div>
      <div className="min-w-0">
        <div className="grid min-w-0 gap-6">{children}</div>
        <details className="mt-6 border-t pt-4 text-sm">
          <summary className="w-fit cursor-pointer text-brand underline-offset-4 hover:underline">
            Использование
          </summary>
          <pre className="mt-3 overflow-x-auto rounded-lg bg-secondary p-4 font-mono text-xs leading-relaxed">
            <code>{code}</code>
          </pre>
        </details>
      </div>
    </section>
  );
}

export function SampleGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <fieldset className="min-w-0">
      <legend className="mb-3 text-xs font-medium text-muted-foreground">{label}</legend>
      <div className="flex min-w-0 flex-wrap items-center gap-3">{children}</div>
    </fieldset>
  );
}
