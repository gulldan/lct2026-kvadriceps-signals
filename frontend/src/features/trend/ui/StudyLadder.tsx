import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ExternalLink } from "lucide-react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { chartTones } from "@/shared/design-system/tones";
import { cn } from "@/shared/lib/cn";
import { formatDate, formatNumber } from "@/shared/lib/format";
import type { StudyEvent, StudyTopic, TrendStudy } from "../studyApi";

export type LadderLane = StudyEvent["community"];
type CorpusLane = "corpus" | "arxiv" | "papers";
const CURATED_LANES: readonly LadderLane[] = ["academia", "open_source", "vendor", "startup"];
const TONE: Record<LadderLane, keyof typeof chartTones> = {
  academia: 1,
  open_source: 2,
  vendor: 3,
  startup: 4,
  organization: 3,
  discussion: 4,
  patent: 5,
};

function months(date: string) {
  return Number(date.slice(0, 4)) * 12 + Number(date.slice(5, 7) || "1") - 1;
}

const kindLabel = (t: TFunction<"trend">, corpusLane: CorpusLane, event: StudyEvent) =>
  t(
    `study.ladder.kinds.${corpusLane === "arxiv" && event.kind === "implementation" ? "repository" : event.kind}`,
  );

export function StudyLadder({
  study,
  topic,
  lanes = CURATED_LANES,
  corpusLane = "corpus",
  from,
  to,
  limit,
}: {
  study: Pick<TrendStudy, "papers" | "external_observed_at">;
  topic: Pick<StudyTopic, "paper_ids" | "external">;
  lanes?: readonly LadderLane[];
  corpusLane?: Exclude<CorpusLane, "arxiv">;
  from?: string;
  to?: string;
  limit?: number;
}) {
  const { t } = useTranslation("trend");
  const papers = study.papers.filter((paper) => topic.paper_ids.includes(paper.id));
  const dates = [...papers.map((p) => p.date), ...topic.external.map((e) => e.date)];
  if (!dates.length) return null;
  const startYear = from
    ? Number(from.slice(0, 4))
    : Math.min(...dates.map((d) => Number(d.slice(0, 4))));
  const start = from ? months(from) : startYear * 12;
  const end = to ? months(to) : 2026 * 12;
  const x = (date: string) => ((months(date) - start + 0.5) / (end - start)) * 100;
  const years = Array.from(
    { length: Math.ceil(end / 12) - startYear },
    (_, i) => startYear + i,
  ).filter((year) => year * 12 >= start && year * 12 < end);
  const external = topic.external.toSorted((a, b) => a.date.localeCompare(b.date));
  const byMonth = new Map<string, number>();
  for (const paper of papers) {
    const month = paper.date.slice(0, 7);
    byMonth.set(month, (byMonth.get(month) ?? 0) + 1);
  }
  const label = (event: StudyEvent) =>
    `${formatDate(event.date)} · ${event.title} · ${event.org} · ${kindLabel(t, corpusLane, event)}`;
  return (
    <section
      aria-label={t("study.ladder.title")}
      className="space-y-4 overflow-x-clip border-t pt-4"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">{t("study.ladder.title")}</h3>
        <p className="max-w-md text-xs text-muted-foreground">{t("study.ladder.hint")}</p>
      </div>
      <div className="grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3 text-xs">
        <span aria-hidden />
        <div className="relative h-4" aria-hidden>
          {years.map((year) => (
            <span
              key={year}
              className="absolute -translate-x-1/2 font-mono text-muted-foreground"
              style={{ left: `${((year * 12 - start) / (end - start)) * 100}%` }}
            >
              {year}
            </span>
          ))}
        </div>
        <span className="py-2 text-muted-foreground">{t(`study.ladder.lanes.${corpusLane}`)}</span>
        <div className="relative h-8 border-t" aria-hidden>
          {[...byMonth].map(([month, count]) => (
            <span
              key={month}
              title={`${month} · ${count}`}
              className={cn(
                "absolute top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-muted-foreground",
                count >= 6 ? "size-3.5" : count >= 3 ? "size-2.5" : "size-1.5",
              )}
              style={{ left: `${x(month)}%` }}
            />
          ))}
        </div>
        {lanes.map((lane) => {
          const events = external.filter((event) => event.community === lane);
          return (
            <div key={lane} className="contents">
              <span className="py-2 text-muted-foreground">{t(`study.ladder.lanes.${lane}`)}</span>
              <div className="relative h-8 border-t">
                {events.length === 0 && (
                  <span className="absolute top-1/2 -translate-y-1/2 text-muted-foreground italic">
                    {t("study.ladder.empty")}
                  </span>
                )}
                {events.map((event) => (
                  <a
                    key={event.url}
                    href={event.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={label(event)}
                    aria-label={label(event)}
                    className={cn(
                      "absolute top-1/2 block size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 focus-visible:outline-2 focus-visible:outline-ring",
                      chartTones[TONE[lane]].circle,
                    )}
                    style={{ left: `${x(event.date)}%` }}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
      <StudyEventList study={study} events={external} corpusLane={corpusLane} limit={limit} />
    </section>
  );
}

export function StudyEventList({
  study,
  events,
  corpusLane = "corpus",
  limit,
}: {
  study: Pick<TrendStudy, "external_observed_at">;
  events: StudyEvent[];
  corpusLane?: CorpusLane;
  limit?: number;
}) {
  const { t } = useTranslation("trend");
  const [expanded, setExpanded] = useState(false);
  if (!events.length)
    return <p className="text-xs leading-5 text-muted-foreground">{t("study.ladder.none")}</p>;
  const external = events.toSorted((a, b) => a.date.localeCompare(b.date));
  return (
    <>
      <ol className="divide-y">
        {(limit && !expanded ? external.slice(0, limit) : external).map((event) => (
          <li
            key={event.url}
            className="grid min-w-0 grid-cols-[6rem_minmax(0,1fr)] items-start gap-3 py-2"
          >
            <time dateTime={event.date} className="pt-1 text-xs text-muted-foreground tabular-nums">
              {event.date}
            </time>
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <Button asChild variant="link" size="inline" className="text-left">
                  <a href={event.url} target="_blank" rel="noopener noreferrer">
                    {event.title}
                    <ExternalLink aria-hidden />
                  </a>
                </Button>
                <Badge variant="outline">{kindLabel(t, corpusLane, event)}</Badge>
              </div>
              <p className="text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  <span
                    className={cn("size-2 rounded-full", chartTones[TONE[event.community]].dot)}
                    aria-hidden
                  />
                  {t(`study.ladder.lanes.${event.community}`)}
                </span>
                {" · "}
                {event.org}
                {event.stars !== undefined && (
                  <>
                    {" · "}
                    {t(event.kind === "discussion" ? "study.ladder.points" : "study.ladder.stars", {
                      count: event.stars,
                      value: formatNumber(event.stars),
                      date: formatDate(study.external_observed_at),
                    })}
                  </>
                )}
              </p>
            </div>
          </li>
        ))}
      </ol>
      {limit && !expanded && external.length > limit && (
        <Button variant="outline" size="sm" onClick={() => setExpanded(true)}>
          {t("study.moreSources", { count: external.length - limit })}
        </Button>
      )}
      <p className="text-xs leading-5 text-muted-foreground">
        {t("study.ladder.catalog", {
          count: external.length,
          date: formatDate(study.external_observed_at).replace(/\.$/, ""),
        })}
      </p>
    </>
  );
}
