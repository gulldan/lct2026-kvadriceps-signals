import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";
import { useRequest } from "@/shared/lib/useRequest";
import { formatDate, formatMonth, formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { CounterStrip } from "@/shared/ui/CounterStrip";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Notice } from "@/shared/ui/Notice";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { getLiveModel, type WeakSignalInputKind, type WeakSignalModelCheck } from "../liveApi";
import { ModelFileCheck } from "../ui/ModelFileCheck";

// Обозначения мер идут в словарь подстановкой: проверка русских строк латиницу не пропускает.
const F1 = "F1";
const AUC = "ROC AUC";
// Коридор ТЗ «75–80 %»: каждая мера сравнивается с ним отдельно, а не приговором по худшей.
const TARGET = { low: 0.75, high: 0.8 };
const BANDS = ["above", "within", "below"] as const;
const band = (share: number) =>
  share > TARGET.high ? "above" : share >= TARGET.low ? "within" : "below";
const VERDICT_TONE = { weak: "ok", missed: "warn" } as const;
const TOP_INPUTS = 10;
// Отчёт отдаёт источники по алфавиту ключей, а эталон должен идти первым.
const SOURCES = ["etalon", "team", "mullany", "cosmos", "hype"] as const;
// Эмбеддинг последним: именованных входов у него нет, только веса главных компонент.
const GROUPS = ["window", "history", "hn", "llm", "embedding"] as const;
const SLICES = ["etalon_team", "etalon_hype", "all"] as const;
const ABLATIONS = ["full", "llm_only", "no_llm", "no_embedding", "no_hn"] as const;
const OUTAGES = ["llm", "history", "hn"] as const;
const AUC_VS = ["team", "mullany", "cosmos", "hype", "all"] as const;
// Группа по приставке вида не расходится с файлом модели, когда в источник добавят признак.
const groupOf = (kind: string) =>
  kind.startsWith("history_")
    ? "history"
    : kind.startsWith("hn_")
      ? "hn"
      : kind.startsWith("llm_")
        ? "llm"
        : "window";
const count = (value: number) => formatNumber(value, { maximumFractionDigits: 1 });
const sumAbs = (values: number[]) => values.reduce((sum, value) => sum + Math.abs(value), 0);

export function TrendModelCheck() {
  const { t } = useTranslation("trend");
  const back = (useLocation().state as { back?: string } | null)?.back ?? "/trends";
  const request = useRequest("live-model", getLiveModel, { cacheMs: 5 * 60_000 });
  return (
    <PageLayout className="space-y-6">
      <PageToolbar>
        <BackButton asChild>
          <Link to={back}>
            <ArrowLeft aria-hidden />
            {t("backRadar")}
          </Link>
        </BackButton>
      </PageToolbar>
      <PageHeader
        title={t("modelCheck.title")}
        description={t("modelCheck.intro")}
        descriptionWidth="full"
      />
      {request.data ? (
        <Report data={request.data} />
      ) : request.status === "error" ? (
        <ErrorState message={t("modelCheck.loadError")} onRetry={request.retry} />
      ) : (
        <Skeleton className="h-96 w-full" />
      )}
    </PageLayout>
  );
}

function Report({ data: { model, evaluation } }: { data: WeakSignalModelCheck }) {
  const { t } = useTranslation("trend");
  const percent = (share: number, digits = 0) =>
    t("modelCheck.percent", {
      percent: formatNumber(share * 100, { maximumFractionDigits: digits }),
    });
  const threshold = formatNumber(model.threshold * 100, { maximumFractionDigits: 0 });
  const { dataset, nested_cv: cv, baseline_all_weak: baseline } = evaluation;

  const inputs = model.inputs.map((input, index) => {
    const phrase = input.startsWith("narrow_") ? "narrow" : "broad";
    const kind = input.slice(phrase.length + 1) as WeakSignalInputKind;
    return { input, phrase, kind, coef: model.coef[index] ?? 0 } as const;
  });
  const groups = GROUPS.map((group) => ({
    group,
    weight:
      group === "embedding"
        ? sumAbs(model.embedding.coef)
        : sumAbs(inputs.filter((item) => groupOf(item.kind) === group).map((item) => item.coef)),
  }));
  const totalWeight = sumAbs(groups.map((item) => item.weight)) || 1;
  const top = inputs.toSorted((a, b) => Math.abs(b.coef) - Math.abs(a.coef)).slice(0, TOP_INPUTS);
  const strongest = Math.abs(top[0]?.coef ?? 1) || 1;

  const scopeRows = cv.scope.reduce((sum, source) => sum + dataset[source], 0);
  const first = cv.first_repeat;
  const spread = t("modelCheck.spread", {
    mean: formatNumber(cv.f1_mean * 100, { maximumFractionDigits: 1 }),
    sd: formatNumber(cv.f1_sd * 100, { maximumFractionDigits: 1 }),
  });
  const measures = [
    { key: "precision", share: cv.precision_mean },
    { key: "recall", share: cv.recall_mean },
    { key: "f1", share: cv.f1_mean },
    { key: "f1Min", share: cv.f1_min },
  ] as const;
  const comparison = [
    { key: "precision", metric: F1, model: cv.precision_mean, baseline: baseline.precision },
    { key: "recall", metric: F1, model: cv.recall_mean, baseline: baseline.recall },
    { key: "f1", metric: F1, model: cv.f1_mean, baseline: baseline.f1 },
    { key: "auc", metric: AUC, model: cv.auc_mean, baseline: null },
  ] as const;
  const falsePositives = evaluation.items
    .filter(
      (item) =>
        cv.scope.includes(item.source) && item.class !== "weak_signal" && item.validation_weak,
    )
    .map((item) => item.name);

  const areaRecall = Object.entries(evaluation.leave_area_out_recall);
  const meanRecall =
    areaRecall.reduce((sum, [, share]) => sum + share, 0) / Math.max(areaRecall.length, 1);

  const signals = evaluation.items.filter((item) => item.source === "etalon");
  const byArea = [...new Set(signals.map((item) => item.area))]
    .toSorted((a, b) => a.localeCompare(b, "ru"))
    .map((area) => {
      const list = signals.filter((item) => item.area === area);
      return {
        area,
        total: list.length,
        weak: list.filter((item) => item.validation_weak).length,
      };
    });
  const recognised = signals.filter((item) => item.validation_weak).length;
  // Матрица ошибок — средняя за повтор; в прежнем отчёте её нет, и тогда берётся первый повтор.
  const matrix = evaluation.slices?.etalon_team ?? first;
  const { ablations, outage, abstention, selection, conformal } = evaluation;
  const sets = model.conformal;
  const llm = evaluation.llm_rating;

  return (
    <ReadingContent className="max-w-none">
      <Section reading title={t("modelCheck.aboutTitle")}>
        <p>{t("modelCheck.about", { version: model.version, threshold })}</p>
        {model.embedded_inference_version && (
          <p>{t("modelCheck.sourceCoverage", { version: model.embedded_inference_version })}</p>
        )}
        <p>
          {t("modelCheck.training", {
            total: formatNumber(SOURCES.reduce((sum, source) => sum + dataset[source], 0)),
            month: formatMonth(evaluation.month),
          })}
        </p>
        {llm && (
          <p>
            {t("modelCheck.llmRating", {
              model: llm.model,
              date: formatDate(llm.collected),
              hash: llm.prompt_sha256.slice(0, 12),
            })}
          </p>
        )}
        <Table caption={t("modelCheck.trainingTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.source")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.rows")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {SOURCES.map((source) => (
              <TableRow key={source}>
                <TableCell className="min-w-56">
                  <span className="font-medium">{t(`modelCheck.sources.${source}.name`)}</span>
                  <span className="block text-muted-foreground">
                    {t(`modelCheck.sources.${source}.hint`)}
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {formatNumber(dataset[source])}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      <Section reading title={t("modelCheck.featuresTitle")}>
        <p>{t("modelCheck.featuresIntro")}</p>
        <Table caption={t("modelCheck.groupsTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.source")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.groupShare")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map(({ group, weight }) => (
              <TableRow key={group}>
                <TableCell className="min-w-56">
                  <span className="font-medium">{t(`modelCheck.groups.${group}.name`)}</span>
                  <span className="block text-muted-foreground">
                    {t(`modelCheck.groups.${group}.hint`, { model: model.embedding.model })}
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {percent(weight / totalWeight)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <p>{t("modelCheck.topIntro")}</p>
        <Table caption={t("modelCheck.topTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.feature")}</TableHead>
              <TableHead>{t("modelCheck.effect")}</TableHead>
              <TableHead>{t("modelCheck.strength")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.weight")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {top.map(({ input, phrase, kind, coef }) => {
              const share = Math.abs(coef) / strongest;
              const strength = share >= 2 / 3 ? "strong" : share >= 1 / 3 ? "moderate" : "weak";
              return (
                <TableRow key={input}>
                  <TableCell className="min-w-48">
                    <span className="font-medium">
                      {t(`modelCheck.inputs.${kind}`, { defaultValue: kind })}
                    </span>
                    <span className="block text-muted-foreground">
                      {t(`modelCheck.phrases.${phrase}`)}
                    </span>
                  </TableCell>
                  <TableCell className="min-w-56">
                    {t(coef > 0 ? "modelCheck.towards" : "modelCheck.away")}
                  </TableCell>
                  <TableCell>
                    <Badge variant={strength === "weak" ? "muted" : "brand"}>
                      {t(`modelCheck.strengths.${strength}`)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatNumber(coef, {
                      minimumFractionDigits: 2,
                      maximumFractionDigits: 2,
                      signDisplay: "exceptZero",
                    })}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Section>

      <Section reading title={t("modelCheck.qualityTitle")}>
        <p>
          {t("modelCheck.qualityIntro", {
            rows: formatNumber(scopeRows),
            folds: formatNumber(cv.folds),
            repeats: formatNumber(cv.repeats),
          })}
        </p>
        {selection && (
          <p>
            {t("modelCheck.selection", {
              recall: percent(selection.recall_target),
              low: percent(selection.thresholds[0]),
              high: percent(selection.thresholds[2]),
            })}
          </p>
        )}
        <CounterStrip
          items={[
            { label: t("modelCheck.modelF1", { metric: F1 }), value: spread },
            {
              label: t("modelCheck.baselineF1", { metric: F1 }),
              value: percent(baseline.f1, 1),
            },
            { label: t("modelCheck.worstF1", { metric: F1 }), value: percent(cv.f1_min, 1) },
            { label: t("modelCheck.target"), value: t("modelCheck.targetValue") },
          ]}
        />
        <Notice tone={measures.some((m) => band(m.share) === "below") ? "warn" : "ok"}>
          {[
            t("modelCheck.verdictLead", { target: t("modelCheck.targetValue") }),
            ...BANDS.flatMap((key) => {
              const list = measures.filter((m) => band(m.share) === key);
              return list.length > 0
                ? [
                    t(`modelCheck.bands.${key}`, {
                      list: list
                        .map((m) =>
                          t(`modelCheck.measures.${m.key}`, {
                            metric: F1,
                            value: percent(m.share, 1),
                          }),
                        )
                        .join(", "),
                    }),
                  ]
                : [];
            }),
            ...(falsePositives.length > 0
              ? [t("modelCheck.falsePositives", { names: falsePositives.join(", ") })]
              : []),
          ].join(" ")}
        </Notice>
        <Table caption={t("modelCheck.qualityTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.metric")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.modelColumn")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.baselineColumn")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {comparison.map((row) => (
              <TableRow key={row.key}>
                <TableCell className="min-w-56">
                  <span className="font-medium">
                    {t(`modelCheck.${row.key}`, { metric: row.metric })}
                  </span>
                  <span className="block text-muted-foreground">
                    {t(`modelCheck.${row.key}Hint`)}
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {percent(row.model, 1)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {row.baseline === null ? "—" : percent(row.baseline, 1)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <Table caption={t("modelCheck.matrixTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.matrixLabel")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.matrixWeak")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.matrixOther")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[
              { key: "weak", hit: matrix.tp, miss: matrix.fn },
              { key: "other", hit: matrix.fp, miss: matrix.tn },
            ].map((row) => (
              <TableRow key={row.key}>
                <TableCell className="min-w-56">
                  {t(`modelCheck.file.decisions.${row.key as "weak" | "other"}`)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {count(row.hit)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {count(row.miss)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {abstention && (
          <p>
            {t("modelCheck.abstention", {
              signals: formatNumber(abstention.by_source.etalon.abstained),
              total: formatNumber(abstention.by_source.etalon.total),
              others: formatNumber(
                SOURCES.filter((source) => source !== "etalon").reduce(
                  (sum, source) => sum + abstention.by_source[source].abstained,
                  0,
                ),
              ),
              metric: F1,
              f1: percent(abstention.etalon_team_without.f1, 1),
            })}
          </p>
        )}
      </Section>

      {evaluation.slices && (
        <Section reading title={t("modelCheck.slicesTitle")}>
          <p>{t("modelCheck.slicesIntro")}</p>
          <Table caption={t("modelCheck.slicesTitle")} className="text-sm">
            <TableHeader>
              <TableRow>
                <TableHead>{t("modelCheck.slice")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.precision")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.recall")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.f1", { metric: F1 })}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {SLICES.map((key) => {
                const row = evaluation.slices![key];
                return (
                  <TableRow key={key}>
                    <TableCell className="min-w-56">
                      <span className="font-medium">{t(`modelCheck.slices.${key}.name`)}</span>
                      <span className="block text-muted-foreground">
                        {t(`modelCheck.slices.${key}.hint`, {
                          rows: formatNumber(row.tp + row.fp + row.fn + row.tn),
                        })}
                      </span>
                    </TableCell>
                    <ShareCells
                      digits={1}
                      shares={{ precision: row.precision, recall: row.recall, f1: row.f1 }}
                    />
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Section>
      )}

      {conformal && sets && evaluation.slices && (
        <Section reading title={t("modelCheck.conformalTitle")}>
          <p>
            {t("modelCheck.conformalIntro", {
              coverage: percent(1 - sets.alpha),
              low: percent(1 - sets.weak),
              high: percent(sets.not_weak),
              threshold: percent(model.threshold),
            })}
          </p>
          <p>
            {t("modelCheck.conformalCheck", {
              weak: percent(conformal.coverage.weak, 1),
              notWeak: percent(conformal.coverage.not_weak, 1),
              undecided: percent(conformal.undecided),
              precision: percent(conformal.weak_precision),
              baseline: percent(evaluation.slices.all.precision),
            })}
          </p>
          <Table caption={t("modelCheck.conformalTable")} className="text-sm">
            <TableHeader>
              <TableRow>
                <TableHead>{t("modelCheck.source")}</TableHead>
                <TableHead className="text-right">{t("report.modelClass.weak_signal")}</TableHead>
                <TableHead className="text-right">{t("report.modelClass.undecided")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.notWeak")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {SOURCES.map((source) => {
                // Отчёт отдаёт ключи по алфавиту, а столбцы идут от сигнала к не сигналу.
                const { weak, undecided, not_weak: notWeak } = conformal.by_source[source];
                return (
                  <TableRow key={source}>
                    <TableCell className="min-w-56 font-medium">
                      {t(`modelCheck.sources.${source}.name`)}
                    </TableCell>
                    <ShareCells digits={0} shares={{ weak, undecided, notWeak }} />
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Section>
      )}

      <Section reading title={t("modelCheck.robustTitle")}>
        <p>{t("modelCheck.robustIntro")}</p>
        <p>{t("modelCheck.sourceOutIntro")}</p>
        <Table caption={t("modelCheck.sourceOutTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.unseenSource")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.seenFalse")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.falseSignal")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {SOURCES.flatMap((source) => {
              const share = evaluation.leave_source_out_false_signal[source];
              const seen = evaluation.false_signal_by_source?.[source];
              return share === undefined
                ? []
                : [
                    <TableRow key={source}>
                      <TableCell className="min-w-56">
                        <span className="font-medium">
                          {t(`modelCheck.sources.${source}.name`)}
                        </span>
                        <span className="block text-muted-foreground">
                          {t(`modelCheck.sources.${source}.hint`)}
                        </span>
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {seen === undefined ? "—" : percent(seen)}
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {percent(share)}
                      </TableCell>
                    </TableRow>,
                  ];
            })}
          </TableBody>
        </Table>
        <p>{t("modelCheck.areaOutIntro")}</p>
        <Table caption={t("modelCheck.areaOutTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.unseenArea")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.areaRecall")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...areaRecall, ["", meanRecall] as const].map(([area, share]) => (
              <TableRow key={area} className={area ? undefined : "font-medium"}>
                <TableCell lang={area ? "ru" : undefined}>
                  {area || t("modelCheck.average")}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {percent(share)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      {ablations && outage && (
        <Section reading title={t("modelCheck.ablationTitle")}>
          <p>
            {t("modelCheck.ablationLead", {
              metric: F1,
              llm: percent(ablations.llm_only.f1, 1),
              full: percent(ablations.full.f1, 1),
              llmAuc: formatNumber(ablations.llm_only.auc, {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              }),
              fullAuc: formatNumber(ablations.full.auc, {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              }),
            })}
          </p>
          {(ablations.full.false_signal.cosmos ?? 0) <
            (ablations.llm_only.false_signal.cosmos ?? 0) && (
            <p>
              {t("modelCheck.ablationGain", {
                full: percent(ablations.full.false_signal.cosmos ?? 0),
                llm: percent(ablations.llm_only.false_signal.cosmos ?? 0),
                fullMullany: percent(ablations.full.false_signal.mullany ?? 0),
                llmMullany: percent(ablations.llm_only.false_signal.mullany ?? 0),
              })}
            </p>
          )}
          {ablations.full.unseen_hype > ablations.llm_only.unseen_hype && (
            <Notice tone="warn">
              {t("modelCheck.ablationWeak", {
                full: percent(ablations.full.unseen_hype),
                llm: percent(ablations.llm_only.unseen_hype),
              })}
            </Notice>
          )}
          <Table caption={t("modelCheck.ablationTitle")} className="text-sm">
            <TableHeader>
              <TableRow>
                <TableHead>{t("modelCheck.variant")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.precision")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.recall")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.f1", { metric: F1 })}</TableHead>
                <TableHead className="text-right">{AUC}</TableHead>
                <TableHead className="text-right">
                  {t("modelCheck.allF1", { metric: F1 })}
                </TableHead>
                <TableHead className="text-right">{t("modelCheck.cosmosFalse")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.unseenHype")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ABLATIONS.map((key) => {
                const row = ablations[key];
                return (
                  <TableRow key={key} className={key === "full" ? "font-medium" : undefined}>
                    <TableCell className="min-w-48">{t(`modelCheck.ablations.${key}`)}</TableCell>
                    <ShareCells
                      digits={1}
                      shares={{ precision: row.precision, recall: row.recall, f1: row.f1 }}
                    />
                    <TableCell className="text-right font-mono tabular-nums">
                      {formatNumber(row.auc, { maximumFractionDigits: 3 })}
                    </TableCell>
                    <ShareCells
                      digits={0}
                      shares={{
                        all: row.all_f1,
                        cosmos: row.false_signal.cosmos ?? 0,
                        hype: row.unseen_hype,
                      }}
                    />
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          <p>{t("modelCheck.aucIntro")}</p>
          <Table caption={t("modelCheck.aucTitle")} className="text-sm">
            <TableHeader>
              <TableRow>
                <TableHead>{t("modelCheck.variant")}</TableHead>
                {AUC_VS.map((key) => (
                  <TableHead key={key} className="text-right">
                    {t(`modelCheck.aucVs.${key}`)}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {ABLATIONS.map((key) => (
                <TableRow key={key} className={key === "full" ? "font-medium" : undefined}>
                  <TableCell className="min-w-48">{t(`modelCheck.ablations.${key}`)}</TableCell>
                  {AUC_VS.map((negative) => (
                    <TableCell key={negative} className="text-right font-mono tabular-nums">
                      {formatNumber(ablations[key].auc_vs[negative], {
                        minimumFractionDigits: 2,
                        maximumFractionDigits: 2,
                      })}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <p>{t("modelCheck.outageIntro")}</p>
          <Table caption={t("modelCheck.outageTitle")} className="text-sm">
            <TableHeader>
              <TableRow>
                <TableHead>{t("modelCheck.outageSource")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.precision")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.recall")}</TableHead>
                <TableHead className="text-right">{t("modelCheck.f1", { metric: F1 })}</TableHead>
                <TableHead className="text-right">{t("modelCheck.hypeFalse")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {OUTAGES.map((key) => {
                const row = outage[key];
                return (
                  <TableRow key={key}>
                    <TableCell className="min-w-48">{t(`modelCheck.outages.${key}`)}</TableCell>
                    <ShareCells
                      digits={1}
                      shares={{ precision: row.precision, recall: row.recall, f1: row.f1 }}
                    />
                    <ShareCells digits={0} shares={{ hype: row.false_signal.hype ?? 0 }} />
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Section>
      )}

      <Section reading title={t("modelCheck.etalonTitle")}>
        <p className="text-lg font-medium">
          {t("modelCheck.etalonSummary", {
            total: formatNumber(signals.length),
            weak: formatNumber(recognised),
          })}
        </p>
        <p>{t("modelCheck.etalonHint")}</p>
        <Table caption={t("modelCheck.etalonTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead>{t("modelCheck.area")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.signals")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.recognised")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...byArea, { area: "", total: signals.length, weak: recognised }].map((row) => (
              <TableRow key={row.area} className={row.area ? undefined : "font-medium"}>
                <TableCell lang={row.area ? "ru" : undefined}>
                  {row.area || t("modelCheck.total")}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {formatNumber(row.total)}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {formatNumber(row.weak)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      <Section
        reading
        title={t("modelCheck.itemsTitle")}
        description={t("modelCheck.itemsIntro", { threshold })}
      >
        <Table caption={t("modelCheck.itemsTitle")} className="text-sm">
          <TableHeader>
            <TableRow>
              <TableHead className="text-right">{t("modelCheck.number")}</TableHead>
              <TableHead>{t("modelCheck.signal")}</TableHead>
              <TableHead>{t("modelCheck.area")}</TableHead>
              <TableHead>{t("modelCheck.phrasesColumn")}</TableHead>
              <TableHead className="text-right">{t("modelCheck.score")}</TableHead>
              <TableHead>{t("modelCheck.verdict")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {signals.map((item) => {
              const outcome = item.validation_weak ? "weak" : "missed";
              return (
                <TableRow key={item.id}>
                  <TableCell className="text-right font-mono text-xs text-muted-foreground tabular-nums">
                    {Number(item.id.replace(/\D+/g, ""))}
                  </TableCell>
                  <TableCell lang="ru" className="max-w-md min-w-56">
                    {item.name}
                  </TableCell>
                  <TableCell lang="ru" className="whitespace-nowrap">
                    {item.area}
                  </TableCell>
                  <TableCell lang="en" className="min-w-40">
                    {item.phrases.join(", ")}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {percent(item.validation)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={VERDICT_TONE[outcome]} className="whitespace-nowrap">
                      {t(`modelCheck.verdicts.${outcome}`)}
                    </Badge>
                    {item.data_gap && (
                      <span className="block text-xs text-muted-foreground">
                        {t("modelCheck.dataGap")}
                      </span>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Section>

      <ModelFileCheck />
    </ReadingContent>
  );
}

// Ключ ячейки — имя меры, а не позиция в строке.
function ShareCells({ shares, digits }: { shares: Record<string, number>; digits: number }) {
  const { t } = useTranslation("trend");
  return (
    <>
      {Object.entries(shares).map(([key, share]) => (
        <TableCell key={key} className="text-right font-mono tabular-nums">
          {t("modelCheck.percent", {
            percent: formatNumber(share * 100, { maximumFractionDigits: digits }),
          })}
        </TableCell>
      ))}
    </>
  );
}
