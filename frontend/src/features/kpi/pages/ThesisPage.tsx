import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ArrowRight, Radar } from "lucide-react";
import { dataGapLabel, getLiveSearchTop, signalScore, signalVerdict } from "@/features/trend";
import { useRequest } from "@/shared/lib/useRequest";
import { formatDate, formatNumber, upperFirst } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { PageLayout } from "@/shared/ui/PageLayout";
import { RichText } from "@/shared/ui/RichText";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import {
  getKPI,
  thesisQuery,
  thesisSearches,
  thesisSignalsPath,
  type ApiKPI,
  type ThesisSearch,
} from "../api";
import { ThesisBetList } from "../ui/ThesisBets";

const SHOWN_SEARCHES = 3;
const SHOWN_TECHNOLOGIES = 5;
type TZStage = "1" | "2" | "3" | "4";

export function ThesisPage() {
  const { id = "" } = useParams();
  const { t } = useTranslation("kpi");
  const thesis = useRequest(`kpi:${id}`, (signal) => getKPI(id, signal));
  const kpi = thesis.data;
  return (
    <PageLayout className="space-y-6">
      <Button variant="link" size="inline" asChild>
        <Link to="/kpi">
          <ArrowLeft className="size-4" aria-hidden />
          {t("page.back")}
        </Link>
      </Button>
      {thesis.status === "error" && <ErrorState message={t("page.error")} onRetry={thesis.retry} />}
      {!kpi && thesis.status !== "error" && <Skeleton className="h-64 w-full" />}
      {kpi && <ThesisBody kpi={kpi} />}
    </PageLayout>
  );
}

function ThesisBody({ kpi }: { kpi: ApiKPI }) {
  const { t } = useTranslation("kpi");
  // Выдача самого тезиса уже сохранена: пустой блок «ещё не искали» прятал бы готовые сигналы.
  const searches = [...thesisSearches(kpi), { query: thesisQuery(kpi), at: "" }]
    .filter(
      (search, index, all) => all.findIndex((other) => other.query === search.query) === index,
    )
    .slice(0, SHOWN_SEARCHES);
  const hasScale = kpi.baseline !== null || kpi.target !== null;
  return (
    <>
      <PageHeader
        kicker={kpi.function_area || undefined}
        title={kpi.title}
        description={kpi.description ? <RichText>{kpi.description}</RichText> : undefined}
        actions={
          <Button asChild>
            <Link to={thesisSignalsPath(kpi)}>
              <Radar className="size-4" aria-hidden />
              {t("card.findSignals")}
            </Link>
          </Button>
        }
      />
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
        {hasScale && (
          <p className="flex items-center gap-2">
            {kpi.metric && <span className="text-muted-foreground">{kpi.metric}:</span>}
            <span className="font-mono tabular-nums">
              {kpi.baseline === null ? "—" : formatNumber(kpi.baseline)}
            </span>
            <ArrowRight className="size-3.5 text-muted-foreground" aria-hidden />
            <span className="font-mono font-semibold text-ok tabular-nums">
              {kpi.target === null ? t("valueLine.target") : formatNumber(kpi.target)}
            </span>
            {kpi.unit && <span className="text-muted-foreground">{kpi.unit}</span>}
          </p>
        )}
        <p className="text-muted-foreground">{t("page.radarQuery", { query: thesisQuery(kpi) })}</p>
        <Button variant="link" size="inline" asChild>
          <Link to={`/hypotheses?kpi=${kpi.id}&kind=hypotheses&queue=all`}>
            {t("page.hypotheses")}
          </Link>
        </Button>
        <Button variant="link" size="inline" asChild>
          <Link to={`/hypotheses?kpi=${kpi.id}&kind=drafts&queue=all`}>{t("page.drafts")}</Link>
        </Button>
        <Button variant="link" size="inline" asChild>
          <Link to={`/hypotheses?kpi=${kpi.id}&kind=directions&queue=all`}>
            {t("page.directions")}
          </Link>
        </Button>
      </div>
      <Section title={t("page.betsTitle")}>
        <ThesisBetList kpiId={kpi.id} />
      </Section>
      <Section title={t("signals.title")} description={t("signals.description")}>
        {searches.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("signals.empty")}</p>
        ) : (
          <div className="space-y-3">
            {searches.map((search) => (
              <SearchSignals key={search.query} thesisId={kpi.id} search={search} />
            ))}
          </div>
        )}
      </Section>
    </>
  );
}

function SearchSignals({ thesisId, search }: { thesisId: string; search: ThesisSearch }) {
  const { t } = useTranslation("kpi");
  const { t: tt } = useTranslation("trend");
  const top = useRequest(`live-top:${search.query}`, (signal) =>
    getLiveSearchTop(search.query, SHOWN_TECHNOLOGIES, signal),
  );
  const path = `/trends?${new URLSearchParams({ q: search.query, thesis: thesisId })}`;
  const candidates = top.data?.found ? top.data.candidates : [];
  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Button variant="link" size="inline" asChild>
          <Link to={path}>{search.query}</Link>
        </Button>
        {search.at && (
          <span className="text-xs text-muted-foreground">
            {t("signals.searchedAt", { date: formatDate(search.at) })}
          </span>
        )}
      </div>
      {top.status === "loading" && <Skeleton className="h-16 w-full" />}
      {top.status === "error" && (
        <p className="text-sm text-muted-foreground">{t("signals.error")}</p>
      )}
      {top.data && !top.data.found && (
        <p className="text-sm text-muted-foreground">{t("signals.pending")}</p>
      )}
      {top.data?.found && candidates.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("signals.noCandidates")}</p>
      )}
      {candidates.length > 0 && (
        <ol className="divide-y">
          {candidates.map((candidate) => {
            const score = signalScore(candidate);
            const tz = candidate.tz?.stage_code ?? 0;
            const name = candidate.label || candidate.name;
            return (
              <li
                key={candidate.id}
                className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2"
              >
                <Button variant="link" size="inline" asChild>
                  <Link to={`${path}&${new URLSearchParams({ signal: candidate.id })}`}>
                    {upperFirst(name)}
                  </Link>
                </Button>
                <div className="flex flex-wrap items-center gap-2">
                  {tz >= 1 && tz <= 4 && (
                    <Badge variant="secondary">
                      {t("signals.tzStage", {
                        stage: t(`signals.tzStages.${String(tz) as TZStage}`),
                      })}
                    </Badge>
                  )}
                  <Badge variant={signalVerdict(candidate, tt).tone}>
                    {signalVerdict(candidate, tt).text}
                  </Badge>
                  {score && (
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {score.gap
                        ? dataGapLabel(score.gap, tt)
                        : `${tt(score.model ? "confidence.modelLabel" : "confidence.label")}: ${tt("confidence.value", { percent: score.percent })}`}
                    </span>
                  )}
                </div>
              </li>
            );
          })}
        </ol>
      )}
      <Button variant="outline" size="sm" asChild>
        <Link to={path}>{t("signals.open")}</Link>
      </Button>
    </Card>
  );
}
