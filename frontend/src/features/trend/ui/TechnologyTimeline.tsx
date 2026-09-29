/* oxlint-disable jsx-a11y/prefer-tag-over-role -- Interactive SVG nodes need roles; HTML buttons are not valid SVG children. */
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import { cn } from "@/shared/lib/cn";
import { topicLabel, type MapTopic } from "../mapData";
import { publicationColor } from "./TechnologyNetwork";

/** Связь соединяет годовые наблюдения одной темы; это не цитирование. */
export function TechnologyTimeline({
  topics,
  years,
  year,
  selectedTopic,
  onSelect,
}: {
  topics: MapTopic[];
  years: number[];
  year: number | null;
  selectedTopic: string;
  onSelect: (topic: string, work?: string) => void;
}) {
  const { t } = useTranslation("trend");
  const maximum = Math.max(
    1,
    ...topics.flatMap((topic) =>
      years.map(
        (value) =>
          topic.works.filter((work) => work.kind === "paper" && work.year === value).length,
      ),
    ),
  );
  const cell = 70;
  return (
    <figure aria-label={t("map.timeline")} className="min-w-0 bg-card">
      <figcaption className="border-b px-3 py-3 text-sm font-medium">
        {t("map.timeline")}
        <span className="mt-1 block text-xs font-normal text-muted-foreground sm:hidden">
          {t("map.timelinePan")}
        </span>
      </figcaption>
      <ScrollArea
        className="h-[440px] overflow-auto md:h-[490px]"
        aria-label={t("map.timelineScroll")}
      >
        <div className="min-w-[520px]">
          <div className="sticky top-0 z-10 grid grid-cols-[160px_minmax(0,1fr)_48px] border-b bg-card px-3 py-3 text-xs text-muted-foreground sm:grid-cols-[190px_minmax(0,1fr)_48px]">
            <span className="sticky left-0 z-10 bg-card">{t("map.topic")}</span>
            <div className="flex">
              {years.map((value) => (
                <span key={value} className="flex-1 text-center font-mono">
                  {value}
                </span>
              ))}
            </div>
            <span className="text-right">{t("map.total")}</span>
          </div>
          {topics.map((topic) => {
            const counts = years.map(
              (value) =>
                topic.works.filter(
                  (work) =>
                    work.kind === "paper" &&
                    work.year === value &&
                    (year === null || value <= year),
                ).length,
            );
            const first = counts.findIndex((count) => count > 0),
              last = counts.findLastIndex((count) => count > 0);
            const total = counts.reduce((sum, count) => sum + count, 0);
            const active = selectedTopic === topic.candidate.id;
            return (
              <div
                key={topic.candidate.id}
                className={cn(
                  "grid grid-cols-[160px_minmax(0,1fr)_48px] items-center border-b px-3 sm:grid-cols-[190px_minmax(0,1fr)_48px]",
                  active && "bg-brand-wash",
                )}
              >
                <div
                  className={cn(
                    "sticky left-0 z-1 self-stretch",
                    active ? "bg-brand-wash" : "bg-card",
                  )}
                >
                  <Button
                    id={`topic-${topic.candidate.id}`}
                    variant="ghost"
                    size="content"
                    className="h-full w-full justify-start py-3 pr-3 text-left"
                    onClick={() => onSelect(topic.candidate.id)}
                    aria-pressed={active}
                    title={topic.candidate.title}
                  >
                    <span className="line-clamp-2">{topicLabel(topic.candidate.title)}</span>
                  </Button>
                </div>
                <div className="relative flex min-w-0 items-center">
                  <div
                    aria-hidden="true"
                    className="absolute top-1/2 right-0 left-0 border-t border-dashed"
                  />
                  {first >= 0 && (
                    <div
                      aria-hidden="true"
                      className="absolute top-1/2 border-t-2 border-brand-border"
                      style={{
                        left: `${((first + 0.5) / years.length) * 100}%`,
                        width: `${((last - first) / years.length) * 100}%`,
                      }}
                    />
                  )}
                  {counts.map((count, index) => (
                    <svg
                      key={years[index]}
                      className="relative h-14 w-0 min-w-0 flex-1"
                      viewBox={`0 0 ${cell} 56`}
                      role="group"
                      aria-label={String(years[index])}
                    >
                      <g
                        role="button"
                        tabIndex={0}
                        aria-label={t("map.yearNode", {
                          title: topic.candidate.title,
                          year: years[index],
                          count,
                        })}
                        onClick={() => onSelect(topic.candidate.id)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter" || event.key === " ") {
                            event.preventDefault();
                            onSelect(topic.candidate.id);
                          }
                        }}
                        className="cursor-pointer outline-none focus-visible:[&>rect]:stroke-foreground"
                      >
                        <rect
                          x="9"
                          y="3"
                          width={cell - 18}
                          height="50"
                          fill="transparent"
                          stroke="transparent"
                          rx="4"
                        />
                        <circle
                          cx={cell / 2}
                          cy="28"
                          r={count ? 9 + Math.sqrt(count / maximum) * 12 : 2}
                          fill={count ? publicationColor(years[index]!, years) : "var(--border)"}
                        />
                        {count > 0 && (
                          <text
                            x={cell / 2}
                            y="28"
                            dominantBaseline="central"
                            textAnchor="middle"
                            fontSize="11"
                            fill="var(--card)"
                            className="font-mono"
                            aria-hidden="true"
                          >
                            {count}
                          </text>
                        )}
                      </g>
                    </svg>
                  ))}
                </div>
                <span className="text-right font-mono text-sm">{total}</span>
              </div>
            );
          })}
        </div>
      </ScrollArea>
      <p className="border-t px-3 py-2 text-xs leading-relaxed text-muted-foreground">
        {t("map.timelineHint")}
      </p>
    </figure>
  );
}
