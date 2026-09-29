import { useTranslation } from "react-i18next";
import type { Itc } from "../api";
import { ITC_FACTORS, itcFactorValue, itcIndex, type ItcFactorKey } from "../itcProfile";
import { formatDateTime, formatNumber } from "@/shared/lib/format";
import { Disclosure } from "@/shared/ui/Disclosure";
import { ValueScale } from "@/shared/ui/ValueScale";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";

const number = (value: number | undefined) => (value === undefined ? "—" : formatNumber(value));

/** Перцентиль никогда не подменяет TRL. */
export function ItcSummary({
  itc,
  title,
  evidenceCitationLabel,
  bare = false,
}: {
  itc: Itc;
  title?: string;
  evidenceCitationLabel?: string;
  bare?: boolean;
}) {
  const { t } = useTranslation("hypothesisDetail");
  const signals = itc.signals;
  const index = itcIndex(itc);
  const score = index === null ? "—" : formatNumber(index, { maximumFractionDigits: 1 });
  const missing = ITC_FACTORS.some(({ key }) => itcFactorValue(itc, key) === null);

  function fact(key: ItcFactorKey): string {
    switch (key) {
      case "SM":
        return t("itc.facts.SM", {
          dated: number(signals?.pub_count),
          total: number(signals?.document_count),
          years:
            signals?.year_min && signals.year_max
              ? signals.year_min === signals.year_max
                ? `${signals.year_min}`
                : `${signals.year_min}–${signals.year_max}`
              : t("itc.unknownYears"),
        });
      case "NV":
        return signals?.embedded_count === undefined
          ? t("itc.facts.NVLegacy")
          : t("itc.facts.NV", {
              count: signals.embedded_count,
              total: number(signals.document_count),
            });
      case "IP":
        return t("itc.facts.IP", { amount: number(signals?.evidence_citations) });
      case "HR":
        return t("itc.facts.HR", {
          documents: number(signals?.document_count),
          organizations: number(signals?.org_count),
        });
    }
  }

  return (
    <section className={bare ? "@container min-w-0" : "@container min-w-0 border-y py-6"}>
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <h3 className="text-base font-medium">{title ?? t("itc.title")}</h3>
        <p className="flex items-baseline gap-2">
          <span className="text-xs text-muted-foreground">{t("itc.index")}</span>
          <span className="font-mono text-2xl font-medium tabular-nums">{score}</span>
          <span className="text-xs text-muted-foreground">{t("itc.outOf100")}</span>
        </p>
      </div>
      <div className="mt-5 grid items-start gap-6 @3xl:grid-cols-[minmax(0,1fr)_17rem] @3xl:gap-10">
        <div className="divide-y">
          {ITC_FACTORS.map(({ key }) => {
            const value = itcFactorValue(itc, key);
            const label = t(`itc.comp.${key}`);
            return (
              <div key={key} className="py-3 first:pt-0 last:pb-0">
                <div className="flex items-baseline justify-between gap-4">
                  <span className="text-sm font-medium">{label}</span>
                  <span className="shrink-0 font-mono text-sm tabular-nums">
                    {value === null
                      ? t("itc.noData")
                      : formatNumber(value, { maximumFractionDigits: 1 })}
                  </span>
                </div>
                {value !== null && (
                  <ValueScale
                    value={value}
                    label={t("itc.scale", { label, value })}
                    className="mt-2"
                  />
                )}
                <p className="mt-2 text-xs leading-relaxed text-muted-foreground">{fact(key)}</p>
              </div>
            );
          })}
        </div>
        <div className="space-y-4 text-sm leading-relaxed">
          <div>
            <h4 className="font-medium">{t("itc.readingTitle")}</h4>
            <p className="mt-1 text-muted-foreground">{t("itc.reading")}</p>
          </div>
          <div>
            <h4 className="font-medium">{t("itc.readinessTitle")}</h4>
            <p className="mt-1 text-muted-foreground">{t("itc.readiness")}</p>
          </div>
          {missing && <p className="text-warn">{t("itc.incomplete")}</p>}
          {itc.computed_at && (
            <p className="text-xs text-muted-foreground">
              {t("itc.updated", { date: formatDateTime(itc.computed_at) })}
            </p>
          )}
        </div>
      </div>
      <Disclosure title={t("itc.methodTitle")} className="mt-6">
        <p className="max-w-prose text-sm text-muted-foreground">{t("itc.method")}</p>
        {ITC_FACTORS.some(({ key }) => itc.components?.[key]?.weight !== undefined) && (
          <Table className="mt-3">
            <caption className="sr-only">{t("itc.methodTitle")}</caption>
            <TableHeader>
              <TableRow>
                <TableHead>{t("itc.factor")}</TableHead>
                <TableHead className="text-right">{t("itc.weight")}</TableHead>
                <TableHead className="text-right">{t("itc.contribution")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ITC_FACTORS.map(({ key }) => {
                const component = itc.components?.[key];
                return (
                  <TableRow key={key}>
                    <TableCell>{t(`itc.comp.${key}`)}</TableCell>
                    <TableCell className="text-right font-mono">
                      {component?.weight === undefined
                        ? "—"
                        : `${formatNumber(component.weight * 100)}%`}
                    </TableCell>
                    <TableCell className="text-right font-mono">
                      {component?.contribution === undefined
                        ? "—"
                        : formatNumber(component.contribution * 100, { maximumFractionDigits: 1 })}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
        <p className="mt-3 max-w-prose text-xs leading-relaxed text-muted-foreground">
          {t("itc.limitations")}
        </p>
        {evidenceCitationLabel && (
          <p className="mt-2 text-xs text-muted-foreground">
            {t("itc.citationScope", { label: evidenceCitationLabel })}
          </p>
        )}
      </Disclosure>
    </section>
  );
}
