import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { useRequest } from "@/shared/lib/useRequest";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getPaperCover, type LiveTopic } from "../liveApi";

const CACHE_MS = 30 * 60_000;
const MONTHS = 12;

function coverPaper(topic: LiveTopic) {
  return [
    topic.card?.example_id,
    ...(topic.card?.evidence ?? []).map((item) => item.source_id),
    ...topic.paper_ids,
  ].find((id) => id?.startsWith("arxiv:"));
}

/** Рисунок тянется, только когда плитка видна: пятнадцать разом — тридцать обращений к arXiv. */
export function TechnologyCover({
  topic,
  alt,
  offline = false,
}: {
  topic: LiveTopic;
  alt: string;
  offline?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);
  const paper = offline ? undefined : coverPaper(topic);
  useEffect(() => {
    const element = ref.current;
    if (!element || seen || !paper) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) setSeen(true);
      },
      { rootMargin: "1200px 0px" },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [seen, paper]);
  const { data: cover, status } = useRequest(
    seen && paper ? `paper-cover:${paper}` : null,
    (signal) => getPaperCover(paper!, signal),
    { cacheMs: CACHE_MS },
  );
  const waiting = !!paper && (!seen || status === "loading" || status === "idle");
  return (
    <div ref={ref} className="absolute inset-0">
      {cover ? (
        <img src={cover.image} alt={alt} className="size-full object-contain p-2" />
      ) : waiting ? (
        <Skeleton className="size-full rounded-none" />
      ) : (
        <MonthlyWorks topic={topic} />
      )}
    </div>
  );
}

function MonthlyWorks({ topic }: { topic: LiveTopic }) {
  const { t } = useTranslation("trend");
  const series = (topic.series ?? []).slice(-MONTHS);
  // Шкала не короче пяти работ: одна статья в месяц не должна выглядеть пиком.
  const top = Math.max(5, ...series.map((period) => period.count));
  return (
    <figure className="relative flex size-full items-end bg-brand-wash px-4 pt-10 pb-10">
      {/* Только для диктора: на виду подпись перекрывали плашки. */}
      <figcaption className="sr-only">{t("tile.monthly")}</figcaption>
      <div className="flex h-full w-full items-end gap-1" aria-hidden>
        {series.map((period, index) => (
          <span
            key={period.period}
            className={cn(
              "min-h-0.5 flex-1 rounded-t-sm",
              index >= series.length - 6 ? "bg-brand" : "bg-brand-border",
            )}
            style={{ height: `${(period.count / top) * 100}%` }}
          />
        ))}
      </div>
    </figure>
  );
}
