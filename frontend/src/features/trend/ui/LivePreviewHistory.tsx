import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { dateFormatter } from "@/shared/lib/format";
import type { LiveOverview } from "../liveApi";

const CHANNEL_TONES = {
  papers: "text-chart-1",
  repos: "text-chart-2",
  hn: "text-chart-3",
} as const;

interface PreviewChannel {
  id: keyof typeof CHANNEL_TONES;
  total: number | null;
  values: (number | null)[];
}

/** Отдельные подписанные шкалы от нуля: малые источники видны рядом с крупными. */
export function LivePreviewHistory({
  channels,
  periods,
  observedAt,
  format,
}: {
  channels: PreviewChannel[];
  periods: LiveOverview["periods"];
  observedAt: string;
  format: (value: number | null) => string;
}) {
  const { t } = useTranslation("trend");
  const dateFormat = dateFormatter({ month: "short", year: "numeric", timeZone: "UTC" });
  const first = periods[0];
  const last = periods.at(-1);
  const end = last?.complete
    ? new Date(new Date(last.end_exclusive).getTime() - 1)
    : new Date(observedAt);
  const step = 160 / Math.max(1, periods.length);
  return (
    <div className="space-y-2">
      <div className="flex justify-between text-caption text-muted-foreground">
        <span>{t("study.live.preview.total")}</span>
        <span>{t("study.live.preview.interval")}</span>
      </div>
      <dl className="space-y-2">
        {channels.map(({ id, total, values }) => {
          const known = values.filter((value) => value !== null);
          const maximum = known.length ? Math.max(...known) : null;
          const scale = Math.max(1, maximum ?? 0);
          const label = t(`study.live.preview.${id}`);
          return (
            <div key={id} className="grid grid-cols-[6.5rem_1fr] items-center gap-x-2">
              <dt className="col-start-1 row-start-1 text-xs">{label}</dt>
              <dd className="col-start-1 row-start-2 mt-0.5 font-mono text-sm tabular-nums">
                {format(total)}
              </dd>
              <dd className="col-start-2 row-span-2 row-start-1">
                <figure
                  className="grid h-9 grid-cols-[2rem_1fr] gap-1"
                  aria-label={t("study.live.preview.history", {
                    label,
                    values: periods
                      .map(
                        (period, index) =>
                          `${dateFormat.format(new Date(period.start))}: ${format(values[index] ?? null)}`,
                      )
                      .join("; "),
                  })}
                >
                  <div
                    aria-hidden
                    className="flex flex-col justify-between text-right font-mono text-micro text-muted-foreground tabular-nums"
                  >
                    <span>{format(maximum)}</span>
                    <span>{maximum === null ? "—" : "0"}</span>
                  </div>
                  <svg
                    viewBox="0 0 160 40"
                    preserveAspectRatio="none"
                    aria-hidden
                    className={cn("h-9 w-full overflow-visible", CHANNEL_TONES[id])}
                  >
                    <path
                      d="M0 1 H160 M0 39 H160"
                      className="stroke-border"
                      strokeWidth="1"
                      vectorEffect="non-scaling-stroke"
                    />
                    {periods.map((period, index) => {
                      const value = values[index];
                      const x = index * step + step * 0.2;
                      const width = step * 0.6;
                      if (value === null || value === undefined)
                        return (
                          <text
                            key={period.period}
                            x={x + width / 2}
                            y="24"
                            textAnchor="middle"
                            fill="currentColor"
                            className="text-caption"
                          >
                            —
                          </text>
                        );
                      if (value === 0)
                        return (
                          <path
                            key={period.period}
                            d={`M${x} 39 h${width}`}
                            stroke="currentColor"
                            strokeWidth="2"
                            vectorEffect="non-scaling-stroke"
                          />
                        );
                      const height = (value / scale) * 38;
                      return (
                        <rect
                          key={period.period}
                          x={x}
                          y={39 - height}
                          width={width}
                          height={height}
                          fill={period.complete ? "currentColor" : "none"}
                          stroke="currentColor"
                          strokeWidth={period.complete ? 0 : 1.5}
                          strokeDasharray={period.complete ? undefined : "2 2"}
                          vectorEffect="non-scaling-stroke"
                        />
                      );
                    })}
                  </svg>
                </figure>
              </dd>
            </div>
          );
        })}
      </dl>
      {first && last && (
        <div className="flex justify-between border-t pt-1 text-micro whitespace-nowrap text-muted-foreground">
          <time dateTime={first.start}>{dateFormat.format(new Date(first.start))}</time>
          <time dateTime={end.toISOString()}>{dateFormat.format(end)}</time>
        </div>
      )}
    </div>
  );
}
