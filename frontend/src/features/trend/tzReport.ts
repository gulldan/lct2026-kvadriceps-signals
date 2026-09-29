import type { TFunction } from "i18next";
import type { ReportBlock } from "@/shared/report/blocks";
import { formatNumber } from "@/shared/lib/format";
import type { LiveAdoptionEvent, LiveEventType, LiveTopic } from "./liveApi";

/** То же соответствие, что LiveEventStage на сервере. */
const EVENT_STAGE: Partial<Record<LiveEventType, number>> = {
  lab_result: 1,
  code_release: 2,
  prototype: 2,
  product_launch: 2,
  grant_early: 2,
  pilot: 3,
  grant_late: 3,
  deployment: 4,
};

type Stage = 0 | 1 | 2 | 3 | 4;

export const eventStage = (type?: string) => (EVENT_STAGE[type as LiveEventType] ?? 0) as Stage;

export function tzSummary(topic: LiveTopic, t: TFunction<"trend">) {
  const tz = topic.tz;
  if (!tz) return "—";
  const dash = "—";
  return t("tz.summary", {
    stage: tz.stage_code || dash,
    trend: tz.trend_code || dash,
    score: tz.score || dash,
  });
}

export function caseTypeLabel(topic: LiveTopic, t: TFunction<"trend">) {
  const stage = eventStage(topic.card?.example_type);
  return stage ? t(`tz.caseType.${stage as Exclude<Stage, 0>}`) : null;
}

export const eventSourceLabel = (source: string, t: TFunction<"trend">) =>
  t(`events.source.${source}`, { defaultValue: source });

export const eventTypeLabel = (event: LiveAdoptionEvent, t: TFunction<"trend">) =>
  event.type ? t(`events.type.${event.type}`) : t("events.mention");

export function eventFacts(event: LiveAdoptionEvent) {
  return [event.actor, event.amount, event.phase, event.record.date?.slice(0, 10)]
    .filter(Boolean)
    .join(" · ");
}

const UNITS = ["releases", "downloads", "stars", "vacancies", "views", "count"] as const;

function counterUnit(event: LiveAdoptionEvent): (typeof UNITS)[number] {
  const named = UNITS.find((unit) => unit === event.unit);
  if (named) return named;
  if (event.type === "hiring") return "vacancies";
  if (event.source === "wikipedia") return "views";
  if (["pypi", "npm", "huggingface"].includes(event.source)) return "downloads";
  if (event.type === "code_release") return "releases";
  return "count";
}

export const isCounter = (event: LiveAdoptionEvent) => typeof event.count === "number";

export function counterLine(event: LiveAdoptionEvent, t: TFunction<"trend">) {
  const count = formatNumber(event.count ?? 0);
  if (event.source === "wikipedia") {
    return t("events.wikipedia", {
      title: event.record.title,
      year: event.record.date.slice(0, 4),
      count,
    });
  }
  const parts = [t(`events.unit.${counterUnit(event)}`, { count })];
  if (typeof event.delta === "number") {
    parts.push(
      t("events.delta", {
        value: `${event.delta > 0 ? "+" : ""}${formatNumber(event.delta)}`,
      }),
    );
  }
  const title = event.record.title ? ` (${event.record.title})` : "";
  return `${eventSourceLabel(event.source, t)}${title}: ${parts.join(", ")}`;
}

/** Цитата — только на экране: в файле английские абзацы вытесняли раздел. */
export function eventsBlocks(topic: LiveTopic, t: TFunction<"trend">): ReportBlock[] {
  const events = topic.events ?? [];
  const blocks: ReportBlock[] = [{ kind: "heading", text: t("events.title") }];
  const counters = events.filter(isCounter);
  const typed = events
    .filter((event) => !isCounter(event))
    .toSorted((a, b) => eventStage(b.type) - eventStage(a.type));
  if (!counters.length && !typed.length) {
    blocks.push({ kind: "paragraph", muted: true, text: t("events.none") });
    return blocks;
  }
  if (counters.length) {
    blocks.push({ kind: "list", items: counters.map((event) => counterLine(event, t)) });
  }
  if (typed.length) {
    blocks.push({
      kind: "list",
      items: typed.map((event) =>
        [
          eventTypeLabel(event, t),
          eventFacts(event),
          `${eventSourceLabel(event.source, t)}: ${event.record.title}`,
          event.record.url,
        ]
          .filter(Boolean)
          .join(" · "),
      ),
    });
  }
  return blocks;
}
