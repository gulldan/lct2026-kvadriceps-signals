import { useState } from "react";
import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import type { MoneyPoint, MoneyQuadrant } from "../earlyMoney";

// Хвост из отраслей с парой событий прячется: вывод по ним всё равно «мало данных».
const SHOWN = 12;
const QUADRANTS = ["angel_window", "money_first", "consensus", "quiet"] as const;
const TONE: Record<MoneyQuadrant, "brand" | "warn" | "ok" | "muted"> = {
  angel_window: "brand",
  money_first: "warn",
  consensus: "ok",
  quiet: "muted",
};

const shift = (value: number) =>
  `${value >= 0 ? "+" : "−"}${formatNumber(Math.abs(2 ** value - 1) * 100, { maximumFractionDigits: 0 })} %`;

/** Отрасли таблицей: число событий, рост денег и науки и вывод словом — вместо облака точек и списка-простыни. */
export function MoneyRubricTable({
  rubrics,
  points,
  label,
  periods,
}: {
  rubrics: { rubric: string; count: number }[];
  points: MoneyPoint[];
  label: (rubric: string) => string;
  /** Окна сравнения денег и науки с датами: без них «+35 %» не говорит, к чему. */
  periods: string[];
}) {
  const { t } = useTranslation("trend");
  const [all, setAll] = useState(false);
  const byRubric = new Map(points.map((point) => [point.rubric, point]));
  const widest = Math.max(1, ...rubrics.map((item) => item.count));
  return (
    <figure className="min-w-0 space-y-3">
      <figcaption className="space-y-1">
        <span className="text-sm font-medium">{t("money.rubricsTitle")}</span>
        <span className="block text-xs text-muted-foreground">{t("money.rubricsHint")}</span>
        {periods.map((line) => (
          <span key={line} className="block text-xs">
            {line}
          </span>
        ))}
      </figcaption>
      <Table aria-label={t("money.rubricsTitle")} className="text-sm">
        <TableHeader>
          <TableRow>
            <TableHead>{t("money.table.industry")}</TableHead>
            <TableHead className="text-right">{t("money.table.events")}</TableHead>
            <TableHead className="text-right">{t("money.table.money")}</TableHead>
            <TableHead className="text-right">{t("money.table.science")}</TableHead>
            <TableHead>{t("money.table.verdict")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rubrics.slice(0, all ? undefined : SHOWN).map((item) => {
            const point = byRubric.get(item.rubric);
            return (
              <TableRow key={item.rubric} data-money-rubric={item.rubric}>
                <TableCell className="max-w-56 min-w-36">{label(item.rubric)}</TableCell>
                <TableCell className="w-40">
                  <span className="flex items-center justify-end gap-2">
                    <span className="h-1.5 w-20 rounded-sm bg-muted" aria-hidden>
                      <span
                        className="block h-1.5 rounded-sm bg-chart-1"
                        style={{ width: `${(item.count / widest) * 100}%` }}
                      />
                    </span>
                    <span className="w-12 text-right font-mono text-xs tabular-nums">
                      {formatNumber(item.count)}
                    </span>
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {point ? shift(point.money) : "—"}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {point ? shift(point.science) : "—"}
                </TableCell>
                <TableCell>
                  {point ? (
                    <Badge variant={TONE[point.quadrant]}>
                      {t(`money.quadrant.${point.quadrant}`)}
                    </Badge>
                  ) : (
                    <span className="text-xs text-muted-foreground">{t("money.table.few")}</span>
                  )}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      {rubrics.length > SHOWN && (
        <Button variant="link" size="inline" onClick={() => setAll(!all)}>
          {all ? t("money.table.collapse") : t("money.table.showAll", { count: rubrics.length })}
        </Button>
      )}
      <dl className="grid gap-x-6 gap-y-1 text-xs sm:grid-cols-2">
        {QUADRANTS.map((quadrant) => (
          <div key={quadrant} className="flex items-baseline gap-2">
            <dt className="shrink-0">
              <Badge variant={TONE[quadrant]}>{t(`money.quadrant.${quadrant}`)}</Badge>
            </dt>
            <dd className="text-muted-foreground">{t(`money.quadrant.${quadrant}Hint`)}</dd>
          </div>
        ))}
      </dl>
    </figure>
  );
}
