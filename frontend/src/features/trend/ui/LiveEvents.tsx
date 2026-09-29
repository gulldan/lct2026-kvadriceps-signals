import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import type { LiveTopic } from "../liveApi";
import {
  counterLine,
  eventFacts,
  eventSourceLabel,
  eventStage,
  eventTypeLabel,
  isCounter,
} from "../tzReport";
import { SourceQuote } from "./SourceQuote";

/** У зрелой технологии два десятка событий, и полный список вытеснял всё, что ниже. */
const SHOWN = 5;

export function LiveEvents({ topic }: { topic: LiveTopic }) {
  const { t } = useTranslation("trend");
  const [all, setAll] = useState(false);
  const events = topic.events ?? [];
  const counters = events.filter(isCounter);
  const typed = events
    .filter((event) => !isCounter(event))
    .toSorted((a, b) => eventStage(b.type) - eventStage(a.type));
  const shown = all ? typed : typed.slice(0, SHOWN);
  return (
    <section className="space-y-3">
      <h3 className="text-lg leading-snug font-semibold">{t("events.title")}</h3>
      {!counters.length && !typed.length && (
        <p className="text-muted-foreground">{t("events.none")}</p>
      )}
      {counters.length > 0 && (
        <ul className="space-y-1 text-sm">
          {counters.map((event) => (
            <li key={`${event.source}:${event.record.id || event.record.url}`}>
              {counterLine(event, t)}
            </li>
          ))}
        </ul>
      )}
      {shown.length > 0 && (
        <ol className="divide-y">
          {shown.map((event) => (
            <li
              key={`${event.type}:${event.source}:${event.record.url || event.record.title}`}
              className="space-y-1 py-2.5 first:pt-0 print:break-inside-avoid"
            >
              <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                <Badge variant={eventStage(event.type) >= 3 ? "ok" : "muted"}>
                  {eventTypeLabel(event, t)}
                </Badge>
                <span className="text-muted-foreground">{eventFacts(event)}</span>
              </p>
              <SourceQuote
                label={`${eventSourceLabel(event.source, t)}: ${event.record.title}`}
                href={event.record.url || undefined}
                quote={event.quote}
                note={t(event.verified ? "events.verified" : "events.unverified")}
              />
            </li>
          ))}
        </ol>
      )}
      {typed.length > SHOWN && (
        <Button variant="link" size="inline" onClick={() => setAll(!all)}>
          {all ? t("events.fewer") : t("events.all", { count: typed.length })}
        </Button>
      )}
    </section>
  );
}
