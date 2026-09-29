import { useEffect, useMemo, useState } from "react";
import { Link, useLocation, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ArrowLeft, Radar } from "lucide-react";
import { ApiError } from "@/shared/api/client";
import { describeError } from "@/shared/api/errors";
import { useAuth } from "@/features/auth";
import { PageLayout } from "@/shared/ui/PageLayout";
import { Button } from "@/shared/ui/Button";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { Badge } from "@/shared/ui/Badge";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { Spinner } from "@/shared/ui/Spinner";
import { DateStrip } from "@/shared/ui/DateStrip";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { ReportExport } from "@/shared/report/ReportExport";
import {
  getTechnology,
  getTechnologyBrief,
  type Technology,
  type TechnologyBrief as Brief,
} from "../technologyApi";
import { distinctStatements, technologyReportDoc } from "../technologyReport";
import { ALIAS_BASES, NAME_KINDS, VALIDATION_STATUSES, known } from "../technologyLabels";
import { ImplementationHypothesisDialog } from "../ui/ImplementationHypothesisDialog";
import { TechnologyBrief } from "../ui/TechnologyBrief";
import { groupBySource, TechnologySources } from "../ui/TechnologySources";
import { TechnologyReview } from "../ui/TechnologyReview";

const STATUS_TONE = { proposed: "muted", verified: "ok", needs_review: "warn" } as const;

export function TechnologyCardPage() {
  const { id = "" } = useParams();
  const { t } = useTranslation("trend");
  const { t: tCommon } = useTranslation("common");
  const [params] = useSearchParams();
  const location = useLocation();
  const { roles } = useAuth();
  const [technology, setTechnology] = useState<Technology | null>(null);
  const [brief, setBrief] = useState<Brief | null>(null);
  const [missing, setMissing] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [version, setVersion] = useState(0);
  const back = params.get("return") || "/trends?entity=technologies";
  // Название направления в адресе не лежит, а тянуть снимок радара ради подписи дорого.
  const backLabel = back.startsWith("/search")
    ? "backResults"
    : back.startsWith("/sources/")
      ? "technology.backSource"
      : back.startsWith("/hypotheses/")
        ? "technology.backBet"
        : new URLSearchParams(back.split("?")[1] ?? "").has("topic")
          ? "backTopic"
          : "technology.back";
  const here = location.pathname + location.search;

  // Готовый бриф отдаётся из кэша без обращения к модели; 404 — «ещё не написан».
  useEffect(() => {
    const controller = new AbortController();
    setBrief(null);
    setMissing(false);
    getTechnologyBrief(id, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setBrief(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setMissing(true);
      });
    return () => controller.abort();
  }, [id]);

  useEffect(() => {
    const controller = new AbortController();
    setError(null);
    getTechnology(id, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setTechnology(result);
      })
      .catch((failure: unknown) => {
        if (controller.signal.aborted) return;
        setTechnology(null);
        setError(failure instanceof ApiError ? failure : new ApiError(0, String(failure)));
      });
    return () => controller.abort();
  }, [id, version]);

  const groups = useMemo(() => groupBySource(technology?.mentions ?? []), [technology]);
  const statements = useMemo(() => distinctStatements(technology?.mentions ?? []), [technology]);
  const dated = groups.filter((group) => group.date).map((group) => group.date);
  const expert = roles.includes("operator") || roles.includes("admin");
  const report = useMemo(
    () => (technology ? technologyReportDoc({ technology, brief, t }) : null),
    [technology, brief, t],
  );

  return (
    <>
      {/* Правило «не разрывать» здесь было бы глобальным и выталкивало бы целые разделы. */}
      <style>{`@media print { @page { size: A4; margin: 16mm; } }`}</style>
      {report && (
        <ReadingContent className="hidden print:block print:max-w-none print:text-sm">
          <ReportBlocks blocks={report.blocks} />
        </ReadingContent>
      )}
      <PageLayout className="space-y-6 print:hidden">
        <PageToolbar>
          <BackButton asChild>
            <Link to={back}>
              <ArrowLeft aria-hidden />
              {t(backLabel)}
            </Link>
          </BackButton>
          {technology && (
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="outline" asChild>
                <Link to={`/trends?${new URLSearchParams({ q: technology.canonical_name })}`}>
                  <Radar aria-hidden />
                  {t("technology.radar")}
                </Link>
              </Button>
              <ImplementationHypothesisDialog
                technology={technology}
                brief={brief}
                onBrief={setBrief}
              />
              {report && <ReportExport doc={() => report} />}
            </div>
          )}
        </PageToolbar>
        {error ? (
          <Notice tone="risk" role="alert">
            {error.status === 403 || error.status === 404
              ? t("technology.notFound")
              : describeError(error, tCommon)}
          </Notice>
        ) : !technology ? (
          <output className="flex items-center gap-3 py-12">
            <span aria-hidden="true">
              <Spinner />
            </span>
            {t("technology.loading")}
          </output>
        ) : (
          <>
            <PageHeader
              title={technology.canonical_name}
              description={kindLine(technology, t)}
              actions={<StatusBadge status={technology.validation_status} />}
            />
            {technology.restricted_revisions > 0 && (
              <Notice tone="muted">
                {t("technology.restricted", { count: technology.restricted_revisions })}
              </Notice>
            )}
            <div className="grid items-start gap-10 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
              <div className="space-y-8">
                <Section title={t("technology.description.title")}>
                  <p className="max-w-prose text-sm leading-relaxed">
                    {technology.description || (
                      <span className="text-muted-foreground">
                        {t("technology.description.empty")}
                      </span>
                    )}
                  </p>
                </Section>
                {/* Маршрут тот же: без ключа отказ модели по прошлой технологии запер бы новую. */}
                <TechnologyBrief
                  key={technology.technology_id}
                  id={technology.technology_id}
                  brief={brief}
                  missing={missing}
                  onBrief={setBrief}
                />
                <Section title={t("technology.statements.title")}>
                  {statements.length === 0 ? (
                    <p className="text-sm text-muted-foreground">
                      {t("technology.statements.empty")}
                    </p>
                  ) : (
                    <ul className="space-y-4">
                      {statements.map((statement) => (
                        <li key={statement.mention.mention_id} className="space-y-2 text-sm">
                          <dl className="grid gap-x-6 gap-y-1 sm:grid-cols-[10rem_minmax(0,1fr)]">
                            {statement.rows.map(([label, value]) => (
                              <div key={label} className="contents">
                                <dt className="text-muted-foreground">
                                  {t(`technology.statements.${label}`)}
                                </dt>
                                <dd className="leading-relaxed">{value}</dd>
                              </div>
                            ))}
                          </dl>
                          <p className="text-xs text-muted-foreground">
                            {t("technology.statements.from", {
                              source:
                                statement.mention.source_title || t("technology.sources.untitled"),
                            })}
                          </p>
                        </li>
                      ))}
                    </ul>
                  )}
                </Section>
                <Section
                  title={t("technology.sources.title")}
                  description={t("technology.sources.hint")}
                >
                  <p className="mb-3 text-xs text-muted-foreground">
                    {t("technology.sources.count", {
                      sources: groups.length,
                      mentions: technology.mentions.length,
                    })}
                  </p>
                  <TechnologySources groups={groups} returnTo={here} />
                </Section>
              </div>
              <div className="space-y-8">
                <Section title={t("technology.aliases.title")}>
                  {technology.aliases.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{t("technology.aliases.empty")}</p>
                  ) : (
                    <ul className="space-y-2 text-sm">
                      {technology.aliases.map((alias) => {
                        const basis = known(ALIAS_BASES, alias.basis);
                        return (
                          <li
                            key={`${alias.language}:${alias.value}`}
                            className="flex flex-wrap items-baseline gap-2"
                          >
                            <span className="font-medium">{alias.value}</span>
                            <span className="text-xs text-muted-foreground">
                              {alias.language}
                              {" · "}
                              {basis ? t(`technology.aliases.basis.${basis}`) : alias.basis}
                            </span>
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </Section>
                <Section title={t("technology.dates.title")}>
                  {dated.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{t("technology.dates.unknown")}</p>
                  ) : (
                    <DateStrip
                      columns={2}
                      items={[
                        { label: t("technology.dates.first"), date: dated.at(-1) },
                        { label: t("technology.dates.last"), date: dated[0] },
                      ]}
                    />
                  )}
                  {dated.length > 0 && dated.length < groups.length && (
                    <p className="mt-2 text-xs text-muted-foreground">
                      {t("technology.dates.known", { known: dated.length, total: groups.length })}
                    </p>
                  )}
                </Section>
                <Section title={t("technology.merge.title")}>
                  <p className="text-sm leading-relaxed text-muted-foreground">
                    {t("technology.merge.body", { method: technology.method_version })}
                  </p>
                </Section>
              </div>
            </div>
            {expert && (
              <TechnologyReview
                technology={technology}
                onChanged={() => setVersion((v) => v + 1)}
              />
            )}
          </>
        )}
      </PageLayout>
    </>
  );
}

function kindLine(technology: Technology, t: TFunction<"trend">) {
  const kind = known(NAME_KINDS, technology.name_kind) ?? "term";
  return `${t(`technology.kind.${kind}`)}: ${t(`technology.kindHint.${kind}`)}`;
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation("trend");
  const key = known(VALIDATION_STATUSES, status);
  if (!key) return <Badge variant="muted">{status}</Badge>;
  return <Badge variant={STATUS_TONE[key]}>{t(`technology.status.${key}`)}</Badge>;
}
