import { memo, type ReactNode } from "react";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/shared/ui/Table";
import { Button } from "@/shared/ui/Button";
import { PageTitle } from "@/shared/ui/PageHeader";
import { cn } from "@/shared/lib/cn";
import { metricLength, type ReportBlock, type ReportFact, type ReportMetric } from "./blocks";

/** Отчёт собирается раз на ответ: сравнение ссылок отсекает перерисовку скрытой печатной версии. */
export const ReportBlocks = memo(function ReportBlocks({ blocks }: { blocks: ReportBlock[] }) {
  return (
    <div className="min-w-0">
      {withKeys(blocks, (block) => block.kind).map(({ item, key }, i) => (
        <Block key={key} b={item} afterHeading={blocks[i - 1]?.kind === "heading"} />
      ))}
    </div>
  );
});

function withKeys<T>(items: T[], tag: (item: T) => string): { item: T; key: string }[] {
  return items.map((item, i) => ({ item, key: `${i}-${tag(item)}` }));
}

const LABEL = "font-mono text-caption leading-snug tracking-[0.06em] uppercase";
const BODY = "text-[0.9375rem] leading-[1.6] tracking-[-0.01em]";
const AVOID_BREAK = { breakInside: "avoid" } as const;
const KEEP_WITH_NEXT = { breakAfter: "avoid" } as const;

function metricSize(m: ReportMetric): string {
  const length = metricLength(m);
  if (length <= 3) return "text-[4rem]";
  if (length <= 5) return "text-[3rem]";
  return length <= 8 ? "text-[2.25rem]" : "text-[1.5rem]";
}

function Metrics({ metrics }: { metrics: ReportMetric[] }) {
  return (
    <div className="grid grid-cols-2 gap-5 sm:col-start-2">
      {metrics.map((m) => (
        <div key={m.label} className="min-w-0">
          <p
            className={cn(
              "flex flex-wrap items-baseline gap-x-[0.06em] font-medium tracking-[-0.01em] tabular-nums",
              metricSize(m),
              "leading-none",
            )}
          >
            <span>{m.value}</span>
            {m.unit && (
              <small className="text-[0.52em] font-medium">
                {/^\p{L}/u.test(m.unit) ? `\u00a0${m.unit}` : m.unit}
              </small>
            )}
          </p>
          <p className={cn(LABEL, "mt-3")}>{m.label}</p>
        </div>
      ))}
    </div>
  );
}

function Hero({
  label,
  title,
  level,
  facts,
  lead,
  aside,
  about,
  text,
  metrics,
}: {
  label: string;
  title: string;
  level: "h1" | "h3";
  facts?: ReportFact[];
  lead?: string;
  aside?: ReactNode;
  about?: string;
  text?: string;
  metrics?: ReportMetric[];
}) {
  const right = lead || aside;
  return (
    <section className="mt-10 border-t border-border-strong first:mt-0" style={AVOID_BREAK}>
      <div
        className={cn(
          "grid gap-5 border-b border-border-strong pt-6 pb-7 sm:gap-8",
          right && "sm:grid-cols-2",
        )}
      >
        <div className="min-w-0">
          <p className={LABEL}>{label}</p>
          {level === "h1" ? (
            <PageTitle className="mt-3.5 font-medium tracking-[-0.035em] print:text-[2.25rem] print:leading-[1.06]">
              {title}
            </PageTitle>
          ) : (
            <h3 className="mt-3.5 text-[1.75rem] leading-[1.06] font-medium tracking-[-0.035em] text-balance">
              {title}
            </h3>
          )}
          {!!facts?.length && (
            <dl className="mt-6 grid gap-3">
              {facts.map((f) => (
                <div key={f.label} className="grid gap-1">
                  <dt className={LABEL}>{f.label}</dt>
                  <dd className="text-caption leading-normal text-muted-foreground">{f.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
        {right && (
          <div className="min-w-0 space-y-3">
            {lead && <p className={cn(BODY, "text-muted-foreground")}>{lead}</p>}
            {aside}
          </div>
        )}
      </div>
      {(about || text || !!metrics?.length) && (
        <div className="pt-6">
          {about && <p className="mb-5 text-lg font-medium tracking-[-0.02em]">{about}</p>}
          <div className="grid gap-8 sm:grid-cols-2">
            {text && <p className={cn(BODY, "text-muted-foreground")}>{text}</p>}
            {!!metrics?.length && <Metrics metrics={metrics} />}
          </div>
        </div>
      )}
    </section>
  );
}

function Block({ b, afterHeading }: { b: ReportBlock; afterHeading: boolean }) {
  switch (b.kind) {
    case "doctitle":
      return (
        <Hero
          level="h1"
          label={[b.doctype, b.org].filter(Boolean).join(" — ")}
          title={b.title}
          facts={b.facts}
          lead={b.lead}
          about={b.about}
          text={b.text}
          metrics={b.metrics}
          aside={
            (b.meta.length > 0 || b.lines.length > 0) && (
              <>
                {b.meta.length > 0 && (
                  <p className={cn(LABEL, "text-muted-foreground")}>{b.meta.join(" · ")}</p>
                )}
                {b.lines.map((line) => (
                  <p key={line} className="text-xs leading-normal text-muted-foreground">
                    {line}
                  </p>
                ))}
              </>
            )
          }
        />
      );
    case "card":
      return (
        <Hero
          level="h3"
          label={b.label}
          title={b.title}
          facts={b.facts}
          lead={b.lead}
          about={b.about}
          text={b.text}
          metrics={b.metrics}
        />
      );
    case "heading":
      return (
        <h2
          className="mt-12 border-t border-border-strong pt-5 text-[1.375rem] leading-tight font-medium tracking-[-0.025em]"
          style={KEEP_WITH_NEXT}
        >
          {b.text}
        </h2>
      );
    case "subheading":
      return (
        <h3
          className="mt-7 text-[1.0625rem] leading-snug font-medium tracking-[-0.02em]"
          style={KEEP_WITH_NEXT}
        >
          {b.text}
        </h3>
      );
    case "paragraph":
      // Как в Word: иначе заголовок с пояснением остаётся внизу листа, а карточка уходит дальше.
      return (
        <p
          style={afterHeading ? KEEP_WITH_NEXT : undefined}
          className={cn(
            "mt-3",
            b.muted ? "text-[0.8125rem] leading-normal text-muted-foreground" : BODY,
            b.italic && "italic",
            b.bold && "font-medium",
          )}
        >
          {b.text}
        </p>
      );
    case "kv":
      return (
        <dl className="mt-4 border-t border-border">
          {b.rows.map((r) => (
            <div
              key={r.label}
              className="grid gap-1 border-b border-border py-2.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] sm:gap-6"
              style={AVOID_BREAK}
            >
              <dt className={cn(LABEL, "pt-0.5")}>{r.label}</dt>
              <dd className="text-sm leading-normal">{r.value}</dd>
            </div>
          ))}
        </dl>
      );
    case "table":
      return (
        <figure className="mt-5">
          {b.title && (
            <figcaption className={cn(LABEL, "mb-2 text-muted-foreground")} style={KEEP_WITH_NEXT}>
              {b.title}
            </figcaption>
          )}
          <Table className="text-[0.8125rem]">
            <TableHeader>
              <TableRow className="border-border-strong hover:bg-transparent">
                {b.head.map((hd) => (
                  <TableHead
                    key={hd}
                    className={cn(
                      LABEL,
                      "h-auto px-2 py-2 align-bottom text-foreground first:pl-0",
                    )}
                  >
                    {hd}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {withKeys(b.rows, (row) => row[0] ?? "").map(({ item: row, key }) => (
                <TableRow key={key} className="hover:bg-transparent" style={AVOID_BREAK}>
                  {withKeys(row, (c) => c.slice(0, 12)).map(({ item: c, key: ck }) => (
                    <TableCell key={ck} className="px-2 py-2 align-top first:pl-0">
                      {c}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </figure>
      );
    case "list": {
      const List = b.ordered ? "ol" : "ul";
      return (
        <List
          className={cn(
            "mt-3 space-y-1.5 pl-6 marker:text-muted-foreground",
            b.ordered ? "list-decimal marker:font-mono marker:text-caption" : "list-disc",
            BODY,
          )}
        >
          {withKeys(b.items, (item) => item.slice(0, 24)).map(({ item, key }) => (
            <li key={key} className="pl-1">
              {item}
            </li>
          ))}
        </List>
      );
    }
    case "quote":
      return (
        <figure className="mt-4 border-l-2 border-border-strong pl-4" style={AVOID_BREAK}>
          <figcaption className={cn(LABEL, "text-muted-foreground")}>{b.source}</figcaption>
          <blockquote className="mt-1.5 text-sm leading-[1.55]">«{b.text}»</blockquote>
          {b.note && <p className="mt-1 text-caption text-muted-foreground">{b.note}</p>}
        </figure>
      );
    case "references":
      return (
        <ol className="mt-4 border-t border-border">
          {b.items.map((item) => (
            <li
              key={item.n}
              className="grid grid-cols-[2.25rem_minmax(0,1fr)] gap-2 border-b border-border py-2 text-[0.8125rem] leading-normal"
              style={AVOID_BREAK}
            >
              <span className="pt-px font-mono text-caption text-muted-foreground tabular-nums">
                {String(item.n).padStart(2, "0")}
              </span>
              <span className="min-w-0">
                {item.text}
                {item.note && <span className="mt-1 block text-muted-foreground">{item.note}</span>}
                {item.url && (
                  <Button
                    asChild
                    variant="link"
                    size="inline"
                    className="block font-normal break-all text-muted-foreground"
                  >
                    <a href={item.url} target="_blank" rel="noreferrer">
                      {item.url}
                    </a>
                  </Button>
                )}
              </span>
            </li>
          ))}
        </ol>
      );
  }
}
