import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import { ArrowLeft, Check, ExternalLink, Minus } from "lucide-react";
import { AiNote } from "@/shared/ui/AiNote";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Notice } from "@/shared/ui/Notice";
import { BackButton, PageToolbar } from "@/shared/ui/PageHeader";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { ReportExport } from "@/shared/report/ReportExport";
import { cn } from "@/shared/lib/cn";
import { formatDate, formatNumber } from "@/shared/lib/format";
import type { LiveClaimEvidence, LiveDetail, LivePriorArt, LiveTopic } from "../liveApi";
import { topicBranch } from "../liveBranches";
import { liveTopicName } from "./LiveOverview";
import { TopicHeader } from "./TopicHeader";
import { SignalHypothesisDialog } from "./SignalHypothesisDialog";
import { SignalNext } from "./SignalNext";
import { SourceQuote } from "./SourceQuote";
import {
  adoptionLabel,
  marketLabel,
  relevanceLabel,
  signalFigures,
  signalReportDoc,
  thresholdPercent,
  verdictReasons,
} from "../signalReport";
import { isConfirmedSignal, notPassedNote, signalScore, signalVerdict } from "../signalScore";
import { passportLine, signalSources, spellAcronyms } from "../signalSources";
import { caseTypeLabel, eventStage } from "../tzReport";
import { LiveEvents } from "./LiveEvents";

type Sources = ReturnType<typeof signalSources>;

export function LiveSignalReport({
  detail,
  candidate,
  query,
  direction,
  topic,
  rank,
  total,
  onBack,
  onMeasurements,
}: {
  detail: LiveDetail;
  candidate: LiveTopic;
  query: string;
  direction?: boolean;
  topic?: string;
  rank: number;
  total: number;
  onBack: () => void;
  onMeasurements: () => void;
}) {
  const { t } = useTranslation("trend");
  const card = candidate.card;
  const why = verdictReasons(candidate, t);
  const sources = signalSources(detail, candidate);
  const exampleSource = card?.example_id
    ? sources.find(({ paper }) => paper.id === card.example_id)
    : undefined;
  const branch = topicBranch(candidate);
  const branchLabel = branch
    ? t(`study.live.branches.${branch}`, { defaultValue: branch })
    : undefined;
  const name = liveTopicName(candidate, t);
  const caseType = caseTypeLabel(candidate, t);
  const report = signalReportDoc({
    detail,
    candidate,
    query: spellAcronyms(query),
    direction,
    rank,
    total,
    name,
    branchLabel,
    t,
  });
  return (
    <>
      <ReadingContent className="hidden print:block print:max-w-none print:text-sm">
        <ReportBlocks blocks={report.blocks} />
      </ReadingContent>
      <div className="space-y-4 print:hidden">
        {/* Правило «не разрывать» здесь было бы глобальным и выталкивало бы раздел источников. */}
        <style>{`@media print { @page { size: A4; margin: 16mm; } }`}</style>
        <PageToolbar>
          <BackButton onClick={onBack}>
            <ArrowLeft aria-hidden />
            {t("report.backToList")}
          </BackButton>
          <div className="flex flex-wrap items-center gap-2">
            <ReportExport doc={() => report} />
            <SignalHypothesisDialog
              key={candidate.id}
              detail={detail}
              candidate={candidate}
              query={query}
              topic={topic}
            />
          </div>
        </PageToolbar>
        <Card className="@container space-y-6 p-4 sm:p-6">
          <TopicHeader
            topic={candidate}
            score={false}
            kicker={t(direction ? "report.kickerDirection" : "report.kicker", {
              rank,
              total,
              query: spellAcronyms(query),
            })}
          />
          <Figures candidate={candidate} sources={sources} />
          <div className="grid items-start gap-x-10 gap-y-8 @4xl:grid-cols-[minmax(0,1fr)_20rem]">
            <ReadingContent className="max-w-none">
              <Field title={t("report.summary")} text={card?.summary}>
                <Evidence field="summary" topic={candidate} sources={sources} />
              </Field>
              <Field title={t("report.advantage")} text={card?.advantage}>
                <Evidence field="advantage" topic={candidate} sources={sources} />
              </Field>
              <Field
                title={t("report.example")}
                text={card?.example}
                badge={
                  caseType && (
                    <Badge variant={card?.example_type === "lab_result" ? "muted" : "ok"}>
                      {caseType}
                    </Badge>
                  )
                }
              >
                <Evidence
                  field="example"
                  topic={candidate}
                  sources={sources}
                  fallback={exampleSource}
                />
              </Field>
              <section className="space-y-2">
                <SectionTitle>{t("report.study_limitations")}</SectionTitle>
                {card?.study_limitations ? (
                  <>
                    <Prose>{card.study_limitations}</Prose>
                    <Evidence field="study_limitations" topic={candidate} sources={sources} />
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    {t("report.studyLimitationsMissing")}
                  </p>
                )}
              </section>
              <section className="space-y-2">
                <SectionTitle>{t("report.report_assessment")}</SectionTitle>
                {card?.report_assessment ? (
                  <>
                    <Prose>{card.report_assessment}</Prose>
                    <Evidence field="report_assessment" topic={candidate} sources={sources} />
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    {t("report.reportAssessmentMissing")}
                  </p>
                )}
              </section>
              {card?.model && <AiNote>{t("report.aiWritten", { model: card.model })}</AiNote>}
              <LiveEvents topic={candidate} />
              <section className="space-y-3">
                <SectionTitle>{why.heading}</SectionTitle>
                {why.lead && <Prose>{why.lead}</Prose>}
                {why.passed.length || why.against.length ? (
                  <div className="grid gap-x-8 gap-y-4 @2xl:grid-cols-2">
                    <Checks
                      title={t("report.passed")}
                      items={why.passed}
                      tone="ok"
                      empty={t("report.noReasons")}
                    />
                    <Checks
                      title={t("report.against")}
                      items={why.against}
                      tone="muted"
                      empty={t("report.nothingMissing")}
                    />
                  </div>
                ) : (
                  <p className="text-muted-foreground">{t("report.noReasons")}</p>
                )}
              </section>
              {candidate.qualification && (
                <section className="space-y-2">
                  <SectionTitle>{t("report.qualification")}</SectionTitle>
                  <Prose>{candidate.qualification.reason}</Prose>
                  <Evidence
                    items={candidate.qualification.evidence?.map((item) => ({
                      ...item,
                      field: "qualification",
                      verified: candidate.qualification?.verified ?? false,
                    }))}
                    sources={sources}
                  />
                </section>
              )}
              {card?.prior_art && <PriorArt result={card.prior_art} />}
              <SourceList detail={detail} sources={sources} />
            </ReadingContent>
            <ScorePanel
              detail={detail}
              candidate={candidate}
              onMeasurements={onMeasurements}
              next={
                <SignalNext candidateId={candidate.id} query={query} topic={topic} name={name} />
              }
            />
          </div>
        </Card>
      </div>
    </>
  );
}

function PriorArt({ result }: { result: LivePriorArt }) {
  const { t } = useTranslation("trend");
  return (
    <section className="space-y-3">
      <SectionTitle>{t("report.priorArt")}</SectionTitle>
      <Prose>{result.note}</Prose>
      {result.query && result.before && (
        <p className="text-sm text-muted-foreground">
          {t("report.priorArtQuery", { query: result.query, date: formatDate(result.before) })}
        </p>
      )}
      {!!result.works?.length && (
        <ul className="space-y-3">
          {result.works.map((work) => (
            <li key={work.id}>
              <a
                href={work.url}
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-4"
              >
                {work.title}
              </a>
              <p className="text-sm text-muted-foreground">{formatDate(work.date)}</p>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Figures({ candidate, sources }: { candidate: LiveTopic; sources: Sources }) {
  const { t } = useTranslation("trend");
  const figures = signalFigures(candidate, sources.length, t);
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-5 border-y py-5 @2xl:grid-cols-3 @4xl:grid-cols-5">
      {figures.map((figure) => (
        <div key={figure.label} className="row-span-3 grid min-w-0 grid-rows-subgrid gap-y-1">
          <dt className="text-sm leading-5 text-muted-foreground">{figure.label}</dt>
          <dd className="flex items-baseline gap-1 text-3xl leading-none font-semibold tabular-nums">
            {figure.value}
            {figure.unit && (
              <span className="text-base font-medium text-muted-foreground">{figure.unit}</span>
            )}
          </dd>
          <dd className={cn("text-xs", figure.warn ? "text-warn" : "text-muted-foreground")}>
            {figure.sub}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function ScorePanel({
  detail,
  candidate,
  onMeasurements,
  next,
}: {
  detail: LiveDetail;
  candidate: LiveTopic;
  onMeasurements: () => void;
  next: ReactNode;
}) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const score = signalScore(candidate);
  const model = candidate.model_score;
  const tz = candidate.tz;
  const threshold = score?.model
    ? Math.round((score.threshold ?? 0) * 100)
    : thresholdPercent(candidate.confidence);
  const stageEvent = tz?.stage_event;
  return (
    <aside className="min-w-0 space-y-5 @4xl:sticky @4xl:top-4" aria-label={t("report.howScored")}>
      <h3 className="text-lg leading-snug font-semibold">{t("report.howScored")}</h3>
      {score && (
        <section className="space-y-3">
          <PanelRow
            label={t(score.model ? "confidence.modelLabel" : "confidence.label")}
            value={score.gap ? "—" : t("confidence.value", { percent: score.percent })}
          />
          {!score.gap && <ScoreBar percent={score.percent} threshold={threshold} />}
          <p className="text-caption text-muted-foreground">
            {[
              threshold !== null && t("report.thresholdAt", { percent: threshold }),
              score.gap
                ? t(`report.dataGap.${score.gap}`)
                : model?.label && t(`report.modelClass.${model.label}`),
            ]
              .filter(Boolean)
              .join(" · ")}
          </p>
          {candidate.shortfall ? (
            <Notice className="text-caption">
              <span className="font-semibold">{signalVerdict(candidate, t).text}. </span>
              {notPassedNote(candidate.shortfall, detail.papers.length, t)}
            </Notice>
          ) : model?.label === "undecided" ? (
            <Notice className="text-caption">{t("report.undecidedReason")}</Notice>
          ) : (
            score.passed &&
            !isConfirmedSignal(candidate) && (
              <Notice className="text-caption">
                <span className="font-semibold">{signalVerdict(candidate, t).text}. </span>
                {t("tile.needSecond")}
              </Notice>
            )
          )}
          {!!model?.features?.length && <Drivers features={model.features} />}
        </section>
      )}
      {tz && (
        <section className="space-y-3 border-t pt-4">
          <PanelRow
            label={t("tz.scoreTitle")}
            value={tz.score ? t("tz.scoreValue", { score: tz.score }) : t("tz.scoreUnknown")}
          />
          <Scale
            label={t("tz.stageShort")}
            value={tz.stage_code}
            total={4}
            text={t(`tz.stageValue.${tz.stage_code as 0 | 1 | 2 | 3 | 4}`)}
          />
          <Scale
            label={t("tz.trendShort")}
            value={tz.trend_code}
            total={3}
            text={t(`tz.trendValue.${tz.trend_code as 0 | 1 | 2 | 3}`)}
          />
          {stageEvent?.record.url && (
            <p className="text-caption text-muted-foreground">
              {t("tz.stageProof")}{" "}
              <Button asChild variant="link" size="inline" className="text-left">
                <a href={stageEvent.record.url} target="_blank" rel="noopener noreferrer">
                  {[
                    t(`tz.caseType.${Math.max(1, eventStage(stageEvent.type)) as 1 | 2 | 3 | 4}`),
                    stageEvent.record.date && formatDate(stageEvent.record.date),
                  ]
                    .filter(Boolean)
                    .join(", ")}
                  <ExternalLink aria-hidden />
                </a>
              </Button>
            </p>
          )}
        </section>
      )}
      <dl className="space-y-2.5 border-t pt-4">
        <Fact label={t("report.adoption")} value={adoptionLabel(candidate, t)} />
        <Fact label={t("report.market")} value={marketLabel(candidate, t)} />
        <Fact label={t("report.relevance")} value={relevanceLabel(detail, candidate, t)} />
      </dl>
      <div className="flex flex-col items-start gap-2 border-t pt-4">
        <Button variant="link" size="inline" onClick={onMeasurements}>
          {t("report.measurements")}
        </Button>
        {model?.model === "weak-signal-logreg" && (
          <Button asChild variant="link" size="inline" className="text-left">
            <Link to="/trends/model" state={{ back: location.pathname + location.search }}>
              {t("modelCheck.link")}
            </Link>
          </Button>
        )}
      </div>
      {next}
    </aside>
  );
}

function PanelRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <h4 className="text-sm font-medium">{label}</h4>
      <span className="shrink-0 text-lg leading-none font-semibold tabular-nums">{value}</span>
    </div>
  );
}

function ScoreBar({ percent, threshold }: { percent: number; threshold: number | null }) {
  const { t } = useTranslation("trend");
  return (
    <div
      // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- составная шкала с отметкой порога, у meter отметки нет
      role="img"
      aria-label={t("report.scoreBar", { percent, threshold: threshold ?? "—" })}
      className="relative h-2 rounded-sm bg-muted"
    >
      <span
        className="absolute inset-y-0 left-0 rounded-sm bg-brand"
        style={{ width: `${Math.max(0, Math.min(100, percent))}%` }}
      />
      {threshold !== null && (
        <span
          className="absolute -inset-y-1 w-0.5 bg-foreground"
          style={{ left: `${Math.max(0, Math.min(100, threshold))}%` }}
        />
      )}
    </div>
  );
}

function Drivers({ features }: { features: { name: string; contribution: number }[] }) {
  const { t } = useTranslation("trend");
  const sorted = features.toSorted((a, b) => Math.abs(b.contribution) - Math.abs(a.contribution));
  const max = Math.max(...sorted.map((item) => Math.abs(item.contribution)), 1e-9);
  return (
    <div className="space-y-2">
      <p className="text-caption text-muted-foreground">{t("report.drivers")}</p>
      <ul className="space-y-2">
        {sorted.map((item) => {
          const width = `${(Math.abs(item.contribution) / max) * 50}%`;
          const up = item.contribution >= 0;
          return (
            <li key={item.name} className="space-y-1">
              <div className="flex items-baseline justify-between gap-2 text-caption">
                <span className="min-w-0">{item.name}</span>
                <span className="shrink-0 font-mono tabular-nums">
                  {`${up ? "+" : "−"}${formatNumber(Math.abs(item.contribution), { maximumFractionDigits: 1, minimumFractionDigits: 1 })}`}
                </span>
              </div>
              <div className="relative h-1.5 rounded-sm bg-muted" aria-hidden>
                <span className="absolute -inset-y-0.5 left-1/2 w-px bg-border-strong" />
                <span
                  className={cn("absolute inset-y-0 rounded-sm", up ? "bg-brand" : "bg-warn")}
                  style={up ? { left: "50%", width } : { right: "50%", width }}
                />
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function Scale({
  label,
  value,
  total,
  text,
}: {
  label: string;
  value: number;
  total: number;
  text: string;
}) {
  return (
    <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-baseline gap-x-2 gap-y-1">
      <span className="text-caption text-muted-foreground">{label}</span>
      <div className="min-w-0 space-y-1">
        <SegmentBar value={value} total={total} tone="brand" label={text} />
        <p className="text-sm leading-snug">{text}</p>
      </div>
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-caption text-muted-foreground">{label}</dt>
      <dd className="text-sm leading-snug first-letter:uppercase">{value}</dd>
    </div>
  );
}

function Evidence({
  field,
  topic,
  items,
  sources,
  fallback,
}: {
  field?: LiveClaimEvidence["field"];
  topic?: LiveTopic;
  items?: LiveClaimEvidence[];
  sources: Sources;
  fallback?: Sources[number];
}) {
  const { t } = useTranslation("trend");
  const list = items ?? topic?.card?.evidence?.filter((item) => item.field === field) ?? [];
  if (!list.length) {
    return fallback ? (
      <SourceQuote
        label={`${t("report.sourceNumber", { n: fallback.n })} ${fallback.paper.title}`}
        href={fallback.paper.url}
      />
    ) : (
      <p className="text-caption text-muted-foreground">{t("report.evidenceMissing")}</p>
    );
  }
  return (
    <div className="space-y-1">
      {list.map((item) => {
        const source = sources.find(({ paper }) => paper.id === item.source_id);
        return (
          <SourceQuote
            key={`${item.field}:${item.source_id}:${item.quote}`}
            label={
              source
                ? `${t("report.sourceNumber", { n: source.n })} ${source.paper.title}`
                : t("report.evidenceSourceMissing", { id: item.source_id })
            }
            href={source?.paper.url}
            quote={item.quote}
            note={t(item.verified ? "report.evidenceVerified" : "report.evidenceUnverified")}
          />
        );
      })}
    </div>
  );
}

function SourceList({ detail, sources }: { detail: LiveDetail; sources: Sources }) {
  const { t } = useTranslation("trend");
  return (
    <section className="space-y-3">
      <SectionTitle>{t("report.sources", { count: sources.length })}</SectionTitle>
      {sources.length === 0 && <p className="text-muted-foreground">{t("report.noSources")}</p>}
      <ol className="divide-y">
        {sources.map(({ n, paper, passport }) => (
          <li
            key={paper.id}
            className="grid min-w-0 grid-cols-[1.75rem_minmax(0,1fr)] items-baseline gap-x-2 py-3 first:pt-0 print:break-inside-avoid"
          >
            <span className="text-sm font-medium text-muted-foreground tabular-nums">{n}</span>
            <div className="min-w-0 space-y-1">
              <Button asChild variant="link" size="inline" className="text-left">
                <a href={paper.url} target="_blank" rel="noopener noreferrer">
                  {paper.title}
                  <ExternalLink aria-hidden />
                </a>
              </Button>
              <p
                className={cn(
                  "text-caption",
                  passport?.trust === "lowered" ? "text-warn" : "text-muted-foreground",
                )}
              >
                {passportLine(paper.date, passport, t)}
              </p>
              {passport?.digest ? (
                <>
                  <p className="text-sm leading-6 text-muted-foreground">{passport.digest}</p>
                  <AiNote>{t("report.digestBy", { model: passport.digest_model || "—" })}</AiNote>
                </>
              ) : passport?.language !== "ru" ? (
                <p className="text-caption text-muted-foreground">
                  {t(detail.digests_pending ? "report.digestPending" : "report.digestMissing")}
                </p>
              ) : null}
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

function SectionTitle({ children }: { children: ReactNode }) {
  return <h3 className="text-lg leading-snug font-semibold">{children}</h3>;
}

/** По ширине — только в широкой колонке: в узкой выключка разгоняет пробелы в реки. */
function Prose({ children }: { children: ReactNode }) {
  return <p className="hyphens-auto @2xl:text-justify">{children}</p>;
}

function Checks({
  title,
  items,
  tone,
  empty,
}: {
  title: string;
  items: string[];
  tone: "ok" | "muted";
  empty: string;
}) {
  const Icon = tone === "ok" ? Check : Minus;
  return (
    <div className="min-w-0 space-y-2">
      <h4 className="text-sm font-medium">{title}</h4>
      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <ul className="space-y-1.5">
          {items.map((item) => (
            <li key={item} className="flex gap-2 text-sm leading-6">
              <Icon
                className={
                  tone === "ok"
                    ? "mt-1 size-4 shrink-0 text-ok"
                    : "mt-1 size-4 shrink-0 text-muted-foreground"
                }
                aria-hidden
              />
              <span className="min-w-0 first-letter:uppercase">{item}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Пустое поле не прячется: постановка задачи требует его, и недобор обязан быть виден. */
function Field({
  title,
  text,
  badge,
  children,
}: {
  title: string;
  text?: string;
  badge?: ReactNode;
  children?: ReactNode;
}) {
  const { t } = useTranslation("trend");
  return (
    <section className="space-y-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <SectionTitle>{title}</SectionTitle>
        {text && badge}
      </div>
      {text ? (
        <Prose>{text}</Prose>
      ) : (
        <p className="text-sm text-muted-foreground">{t("report.missingField")}</p>
      )}
      {text ? children : null}
    </section>
  );
}
