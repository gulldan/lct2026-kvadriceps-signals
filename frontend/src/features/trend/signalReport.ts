import type { TFunction } from "i18next";
import { passportLine, signalSources } from "./signalSources";
import type { ReportBlock, ReportDoc } from "@/shared/report/blocks";
import { formatDate, formatMonth, formatNumber, humanSize, upperFirst } from "@/shared/lib/format";
import { currentLanguage } from "@/shared/i18n";
import type {
  LiveChannel,
  LiveClaimEvidence,
  LiveConfidence,
  LiveDetail,
  LiveLimit,
  LiveTopic,
  LiveVolume,
} from "./liveApi";
import { liveLimitKind } from "./liveLimits";
import {
  dataGapLabel,
  hasModelDecision,
  independentSources,
  isConfirmedSignal,
  isUnchecked,
  MIN_INDEPENDENT_SOURCES,
  scoreSummary,
  signalScore,
  signalVerdict,
} from "./signalScore";
import { caseTypeLabel, eventStage, eventTypeLabel, eventsBlocks, isCounter } from "./tzReport";

/** Ноль — не порог, а старый снимок без поля (в Go нет omitempty). */
export function thresholdPercent(confidence: LiveConfidence | null | undefined): number | null {
  const value = confidence?.threshold;
  return typeof value === "number" && value > 0 ? Math.round(value * 100) : null;
}

function aboveThresholdLabel(confidence: LiveConfidence | null | undefined, t: TFunction<"trend">) {
  const percent = thresholdPercent(confidence);
  return percent === null
    ? t("counters.aboveThresholdUnknown")
    : t("counters.aboveThreshold", { percent });
}

/** Исключённые по существу и непроверенные считаются по списку: один счётчик «исключено» их смешивал. */
export function coverageRows(
  { counters, volume, rejected }: Pick<LiveDetail, "counters" | "volume" | "rejected">,
  confidence: LiveConfidence | null | undefined,
  t: TFunction<"trend">,
) {
  if (!counters) return [];
  const unchecked = rejected?.filter(isUnchecked).length;
  const values: [string, number | undefined][] = [
    [t("counters.sources"), counters.sources_processed],
    [t("counters.candidates"), counters.candidates],
    [t("counters.selected"), counters.selected],
    [t("counters.rejected"), rejected ? rejected.length - (unchecked ?? 0) : counters.rejected],
    [t("counters.unchecked"), unchecked],
    [aboveThresholdLabel(confidence, t), counters.above_threshold],
    [t("counters.modelScored"), counters.model_scored],
    [t("counters.modelAbove"), counters.model_above_threshold],
  ];
  return values
    .flatMap(([label, value]) =>
      typeof value === "number" ? [{ label, value: formatNumber(value) }] : [],
    )
    .concat(volumeRows(volume, t));
}

function volumeRows(volume: LiveVolume | undefined, t: TFunction<"trend">) {
  if (!volume?.processed) return [];
  return [
    // Ноль — запасной путь через API arXiv, где окна нет: строка с нулём соврала бы о сборе.
    ...(volume.scanned
      ? [{ label: t("counters.volumeScanned"), value: formatNumber(volume.scanned) }]
      : []),
    { label: t("counters.volumeFetched"), value: formatNumber(volume.fetched) },
    { label: t("counters.volumeStored"), value: formatNumber(volume.stored) },
    {
      label: t("counters.volumeText"),
      value: t("counters.volumeTextValue", {
        pages: formatNumber(volume.pages),
        tokens: formatNumber(volume.tokens),
        chars: formatNumber(volume.chars),
      }),
    },
    ...(volume.model?.calls
      ? [
          {
            label: t("counters.volumeModel"),
            value: t("counters.volumeModelValue", {
              calls: formatNumber(volume.model.calls),
              prompt: formatNumber(volume.model.prompt_tokens),
              completion: formatNumber(volume.model.completion_tokens),
            }),
          },
        ]
      : []),
    ...(volume.analysis?.papers
      ? [
          {
            label: t("counters.volumeAnalysis"),
            value: t("counters.volumeAnalysisValue", {
              papers: formatNumber(volume.analysis.papers),
              tokens: formatNumber(volume.analysis.tokens),
            }),
          },
        ]
      : []),
    ...(volume.classifier?.candidates
      ? [
          {
            label: t("counters.volumeClassifier"),
            value: t("counters.volumeClassifierValue", {
              candidates: formatNumber(volume.classifier.candidates),
              sources: formatNumber(volume.classifier.sources),
              tokens: formatNumber(volume.classifier.tokens),
              size: humanSize(volume.classifier.bytes),
            }),
          },
        ]
      : []),
  ];
}

export function adoptionLabel(topic: LiveTopic, t: TFunction<"trend">) {
  const code = topic.adoption?.code;
  if (code && code !== "unknown") return t(`report.adoptionStage.${code}`);
  return t(topic.tz ? "report.adoptionNotFound" : "report.unknown");
}

export function marketLabel(topic: LiveTopic, t: TFunction<"trend">) {
  const market = topic.market;
  if (market?.status === "mature" || market?.mature) return t("report.marketMature");
  if (market?.status === "forming") return t("report.marketForming");
  if (market?.status === "early")
    return t(market.evidence?.length ? "report.marketYoung" : "report.marketNoSigns");
  return t("report.marketUnknown");
}

export function emergenceLabel(topic: LiveTopic, t: TFunction<"trend">) {
  return t(`report.emergenceClass.${topic.emergence?.class ?? "unmeasured"}`);
}

/** У старого снимка порога нет: показываем одну долю, а не порог наугад. */
export function relevanceLabel(detail: LiveDetail, topic: LiveTopic, t: TFunction<"trend">) {
  const value = topic.query_relevance;
  if (typeof value !== "number") return t("report.notMeasured");
  const percent = Math.round(value * 100);
  const floor = detail.relevance_floor;
  return typeof floor === "number" && floor > 0
    ? t("report.relevanceValue", { percent, floor: Math.round(floor * 100) })
    : t("confidence.value", { percent });
}

export function momentumLabel(topic: LiveTopic, t: TFunction<"trend">) {
  return t(`report.momentumValue.${topic.momentum}`, { defaultValue: t("report.notMeasured") });
}

function momentumBasis(topic: LiveTopic, t: TFunction<"trend">) {
  const delta = topic.growth?.delta_pp;
  const comparison = topic.comparison;
  return (
    [
      typeof delta === "number" &&
        t("report.momentumDelta", {
          value: `${delta > 0 ? "+" : ""}${formatNumber(delta, { maximumSignificantDigits: 2 })}`,
        }),
      comparison &&
        t("report.momentumCompare", {
          current: formatNumber(comparison.current),
          prior: formatNumber(comparison.prior),
        }),
    ]
      .filter(Boolean)
      .join(" · ") || undefined
  );
}

/** Пустое поле — «не измерена»: строгая ступень по умолчанию выдала бы сильнейший отбор. */
export function evidenceLevelLabel(topic: LiveTopic, t: TFunction<"trend">) {
  if (topic.shortfall) {
    return t("signalList.topUpWith", { check: shortfallLabel(topic.shortfall, t) });
  }
  const level = topic.evidence_level;
  return level ? t(`signalList.evidenceLevel.${level}`) : t("report.notMeasured");
}

function shortfallLabel(shortfall: { code: string }, t: TFunction<"trend">) {
  return t(`signalList.reject.${shortfall.code}`, { defaultValue: shortfall.code });
}

/** Сервер часто начинает основание тем же вердиктом — второй раз он не пишется. */
const join = (verdict: string, basis?: string) =>
  !basis
    ? verdict
    : basis.toLowerCase().startsWith(verdict.toLowerCase())
      ? basis
      : `${verdict} · ${basis}`;

/** Старые снимки несут строку сборщика: русскую сохраняем, английскую техническую не выносим. */
function limitLines(limits: LiveLimit[], t: TFunction<"trend">) {
  const neutral = t("study.live.coverage.other");
  const unique = [...new Map(limits.map((limit) => [limit.text, limit])).values()];
  const lines = unique.map((limit) => {
    const code = limit.code || liveLimitKind(limit.text);
    const line = t(`study.live.coverage.${code}`, {
      replace: Object.fromEntries(
        Object.entries(limit.values ?? {}).map(([key, value]) => [key, formatNumber(value)]),
      ),
      defaultValue: neutral,
    });
    if (!line.includes("{{") && line !== neutral) return line;
    const body = limit.text.replace(/^[a-z_]+:\s*/, "").trim();
    return currentLanguage() === "ru" && /[а-яё]/i.test(body) ? upperFirst(body) : neutral;
  });
  return [...new Set(lines)];
}

export interface SignalFigure {
  label: string;
  value: string;
  unit?: string;
  sub: string;
  warn?: boolean;
}

/** Сравнение по дням — запасной путь у старых снимков без окон ТЗ. */
function halfYears(topic: LiveTopic) {
  const growth = topic.growth;
  if (growth?.recent && growth.prior)
    return { recent: growth.recent.count, prior: growth.prior.count };
  if (topic.comparison) return { recent: topic.comparison.current, prior: topic.comparison.prior };
  return null;
}

export function growthLabel(topic: LiveTopic, t: TFunction<"trend">) {
  const half = halfYears(topic);
  if (!half || (!half.prior && !half.recent)) return null;
  if (!half.prior)
    return { label: t("tile.growthNew", { count: half.recent }), trend: "up" as const };
  // «Спад» у единственной статьи — одна работа, вышедшая в прошлом полугодии, а не динамика.
  if (half.prior + half.recent < 4) return null;
  const ratio = half.recent / half.prior;
  if (ratio >= 1.5)
    return { label: t("tile.growthUp", { ratio: Math.round(ratio) }), trend: "up" as const };
  return ratio >= 0.8
    ? { label: t("tile.growthFlat"), trend: "flat" as const }
    : { label: t("tile.growthDown"), trend: "down" as const };
}

function strongestEvent(topic: LiveTopic) {
  const events = (topic.events ?? []).filter((event) => !isCounter(event));
  return {
    count: events.length,
    best: events.toSorted((a, b) => eventStage(b.type) - eventStage(a.type))[0],
  };
}

export function signalFigures(
  topic: LiveTopic,
  sources: number,
  t: TFunction<"trend">,
): SignalFigure[] {
  const score = signalScore(topic);
  const threshold = score?.model
    ? Math.round((score.threshold ?? 0) * 100)
    : thresholdPercent(topic.confidence);
  const tz = topic.tz;
  const half = halfYears(topic);
  const { count, best } = strongestEvent(topic);
  const dash = "—";
  const doubted = !!topic.shortfall || (!!score?.passed && !isConfirmedSignal(topic));
  return [
    {
      label: t(score?.model ? "confidence.modelLabel" : "confidence.label"),
      value: score && !score.gap ? String(score.percent) : dash,
      unit: score && !score.gap ? "%" : undefined,
      sub: !score
        ? t("report.unknown")
        : score.gap
          ? dataGapLabel(score.gap, t)
          : doubted
            ? signalVerdict(topic, t).text
            : threshold === null
              ? dash
              : t("report.thresholdAt", { percent: threshold }),
      warn: !!score?.gap || doubted,
    },
    {
      label: t("tz.scoreTitle"),
      value: tz?.score ? String(tz.score) : dash,
      unit: tz?.score ? t("figures.ofSeven") : undefined,
      sub: tz?.score
        ? t("tz.scoreBasis", { stage: tz.stage_code, trend: tz.trend_code })
        : t("tz.scoreUnknown"),
    },
    half
      ? {
          label: t("figures.recent"),
          value: formatNumber(half.recent),
          sub: t("figures.prior", { count: half.prior }),
        }
      : {
          label: t("figures.papers"),
          value: formatNumber(topic.papers_count),
          sub: momentumLabel(topic, t),
        },
    {
      label: t("figures.events"),
      value: formatNumber(count),
      sub: best ? t("figures.best", { type: eventTypeLabel(best, t) }) : t("figures.noEvents"),
    },
    {
      label: t("figures.sources"),
      value: formatNumber(sources),
      sub: topic.first_year ? t("figures.since", { date: formatMonth(topic.first_year) }) : dash,
    },
  ];
}

const driver = (feature: { name: string; value: string }) => `${feature.name}: ${feature.value}`;

/** Правила и вклады признаков модели вместе: иначе блок «почему» спорит с оценкой, на которой держится вердикт. */
export function verdictReasons(topic: LiveTopic, t: TFunction<"trend">) {
  const score = signalScore(topic);
  const sources = independentSources(topic);
  const confirmed = isConfirmedSignal(topic);
  const features = (topic.model_score?.features ?? []).toSorted(
    (a, b) => Math.abs(b.contribution) - Math.abs(a.contribution),
  );
  const values = score && {
    percent: score.percent,
    threshold: Math.round((score.threshold ?? 0) * 100),
    count: sources,
  };
  return {
    heading: t(confirmed ? "report.whySignal" : "report.whyVerdict"),
    lead:
      values && hasModelDecision(topic) && !topic.shortfall
        ? t(confirmed ? "report.whyConfirmed" : "report.whyShort", values)
        : undefined,
    passed: [
      ...(topic.confidence?.reasons ?? []),
      ...features.filter((feature) => feature.contribution > 0).map(driver),
    ],
    against: [
      ...(sources < MIN_INDEPENDENT_SOURCES ? [t("report.fewSources", { count: sources })] : []),
      ...(topic.confidence?.missing ?? []),
      ...features.filter((feature) => feature.contribution < 0).map(driver),
    ],
  };
}

function verdictLine(topic: LiveTopic, t: TFunction<"trend">) {
  const score = signalScore(topic);
  const model = topic.model_score;
  const tz = topic.tz;
  const { count, best } = strongestEvent(topic);
  const parts: string[] = [];
  if (score?.gap) parts.push(`${dataGapLabel(score.gap, t)}.`);
  else if (score)
    parts.push(
      t("report.verdictScore", {
        // Класс модели без второго подтверждения — не вердикт: итог тот же, что на плитке.
        verdict: hasModelDecision(topic)
          ? signalVerdict(topic, t).text
          : model?.label
            ? t(`report.modelClass.${model.label}`)
            : scoreSummary(score, t),
        percent: score.percent,
        threshold: score.model
          ? Math.round((score.threshold ?? 0) * 100)
          : (thresholdPercent(topic.confidence) ?? "—"),
      }),
    );
  if (topic.shortfall)
    parts.push(`${t("signalList.topUpWith", { check: shortfallLabel(topic.shortfall, t) })}.`);
  if (tz?.stage_code || tz?.trend_code)
    parts.push(
      t("report.verdictTz", {
        stage: t(`tz.stageValue.${tz.stage_code as 0 | 1 | 2 | 3 | 4}`),
        trend: t(`tz.trendValue.${tz.trend_code as 0 | 1 | 2 | 3}`),
      }),
    );
  parts.push(
    best
      ? t("report.verdictEvents", { count, type: eventTypeLabel(best, t) })
      : t("report.verdictNoEvents"),
  );
  return parts.join(" ");
}

function quoteBlocks(
  items: LiveClaimEvidence[] | undefined,
  sources: ReturnType<typeof signalSources>,
  t: TFunction<"trend">,
): ReportBlock[] {
  return (items ?? []).map((item) => {
    const source = sources.find(({ paper }) => paper.id === item.source_id);
    return {
      kind: "quote",
      source: source
        ? `${t("report.sourceNumber", { n: source.n })} ${source.paper.title}`
        : t("report.evidenceSourceMissing", { id: item.source_id }),
      text: item.quote.trim(),
      note: item.verified ? undefined : t("report.evidenceUnverified"),
    };
  });
}

interface SignalReportInput {
  detail: LiveDetail;
  candidate: LiveTopic;
  query: string;
  direction?: boolean;
  rank: number;
  total: number;
  name: string;
  branchLabel?: string;
  t: TFunction<"trend">;
}

export function signalReportDoc({
  detail,
  candidate,
  query,
  direction,
  rank,
  total,
  name,
  branchLabel,
  t,
}: SignalReportInput): ReportDoc {
  const confidence = candidate.confidence;
  const card = candidate.card;
  const model = candidate.model_score;
  const tz = candidate.tz;
  const sources = signalSources(detail, candidate);
  const dash = "—";
  const evidence = (field: LiveClaimEvidence["field"]) =>
    card?.evidence?.filter((item) => item.field === field);
  const blocks: ReportBlock[] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("report.docType"),
      title: name,
      meta: [`${t("report.observedAt")}: ${formatDate(detail.observed_at)}`],
      lines: [],
      facts: [
        { label: t(direction ? "report.factDirection" : "report.factQuery"), value: query },
        { label: t("report.factRank"), value: t("report.rankValue", { rank, total }) },
        ...(branchLabel ? [{ label: t("report.area"), value: branchLabel }] : []),
        ...(candidate.label && candidate.label !== candidate.name
          ? [{ label: t("report.originalName"), value: candidate.name }]
          : []),
      ],
      lead: verdictLine(candidate, t),
      about: t("report.summary"),
      text: card?.summary || t("report.missingField"),
      metrics: signalFigures(candidate, sources.length, t)
        .slice(0, 4)
        .map(({ value, unit, label }) => ({ value, unit, label })),
    },
    ...quoteBlocks(card?.summary ? evidence("summary") : [], sources, t),
  ];

  blocks.push({ kind: "heading", text: t("report.howScored") });
  const score = signalScore(candidate);
  const half = halfYears(candidate);
  blocks.push({
    kind: "kv",
    rows: [
      {
        label: t(score?.model ? "confidence.modelLabel" : "confidence.label"),
        value: score ? scoreSummary(score, t) : dash,
      },
      ...(tz
        ? [
            {
              label: t("tz.stage"),
              value: join(
                t(`tz.stageValue.${tz.stage_code as 0 | 1 | 2 | 3 | 4}`),
                tz.stage_event
                  ? [
                      t(
                        `tz.caseType.${Math.max(1, eventStage(tz.stage_event.type)) as 1 | 2 | 3 | 4}`,
                      ),
                      tz.stage_event.record.date && formatDate(tz.stage_event.record.date),
                    ]
                      .filter(Boolean)
                      .join(", ")
                  : undefined,
              ),
            },
            {
              label: t("tz.trend"),
              value: join(
                t(`tz.trendValue.${tz.trend_code as 0 | 1 | 2 | 3}`),
                half ? t("report.halfYears", half) : undefined,
              ),
            },
            {
              label: t("tz.scoreTitle"),
              value: tz.score
                ? `${t("tz.scoreValue", { score: tz.score })} · ${t("tz.scoreBasis", { stage: tz.stage_code, trend: tz.trend_code })}`
                : t("tz.scoreMissing"),
            },
          ]
        : []),
      // Со шкалой ТЗ основание внедрения повторяет основание стадии строкой выше.
      {
        label: t("report.adoption"),
        value: tz
          ? adoptionLabel(candidate, t)
          : join(adoptionLabel(candidate, t), candidate.adoption?.basis),
      },
      {
        label: t("report.market"),
        value: join(marketLabel(candidate, t), candidate.market?.basis),
      },
      {
        label: t("report.scienceHistory"),
        value: candidate.first_year
          ? t("report.scienceSince", { date: candidate.first_year, count: candidate.papers_count })
          : dash,
      },
      ...(tz
        ? []
        : [
            {
              label: t("report.momentum"),
              value: join(momentumLabel(candidate, t), momentumBasis(candidate, t)),
            },
          ]),
      { label: t("report.relevance"), value: relevanceLabel(detail, candidate, t) },
    ],
  });
  if (model?.features?.length) {
    blocks.push({
      kind: "table",
      title: t("report.modelFeatures"),
      head: [t("report.feature"), t("report.featureValue"), t("report.featureContribution")],
      rows: model.features
        .toSorted((a, b) => Math.abs(b.contribution) - Math.abs(a.contribution))
        .map((feature) => [
          feature.name,
          feature.value,
          `${feature.contribution >= 0 ? "+" : "−"}${formatNumber(Math.abs(feature.contribution), { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`,
        ]),
    });
    blocks.push({ kind: "paragraph", muted: true, text: t("report.modelNote") });
  } else if (!model) {
    blocks.push({ kind: "paragraph", muted: true, text: t("report.modelUnavailable") });
  }

  const why = verdictReasons(candidate, t);
  blocks.push({ kind: "heading", text: why.heading });
  if (why.lead) blocks.push({ kind: "paragraph", text: why.lead });
  blocks.push(
    why.passed.length
      ? { kind: "list", items: why.passed }
      : { kind: "paragraph", text: t("report.noReasons") },
  );
  if (why.against.length) {
    blocks.push({ kind: "subheading", text: t("report.against") });
    blocks.push({ kind: "list", items: why.against });
  }

  for (const field of ["advantage", "example", "study_limitations", "report_assessment"] as const) {
    blocks.push({ kind: "heading", text: t(`report.${field}`) });
    const text = card?.[field];
    blocks.push(
      text
        ? { kind: "paragraph", text }
        : {
            kind: "paragraph",
            muted: field === "report_assessment" || field === "study_limitations",
            text: t(
              field === "report_assessment"
                ? "report.reportAssessmentMissing"
                : field === "study_limitations"
                  ? "report.studyLimitationsMissing"
                  : "report.missingField",
            ),
          },
    );
    if (!text) continue;
    if (field === "example") {
      const caseType = caseTypeLabel(candidate, t);
      if (caseType)
        blocks.push({ kind: "paragraph", bold: true, text: `${t("tz.caseLabel")}: ${caseType}` });
    }
    const quotes = quoteBlocks(evidence(field), sources, t);
    blocks.push(...quotes);
    const exampleSource =
      field === "example" && card?.example_id
        ? sources.find(({ paper }) => paper.id === card.example_id)
        : undefined;
    if (!quotes.length && exampleSource) {
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: `${t("report.exampleSource")} ${t("report.sourceNumber", { n: exampleSource.n })} ${exampleSource.paper.title}`,
      });
    }
  }
  if (card?.model) {
    blocks.push({
      kind: "paragraph",
      muted: true,
      italic: true,
      text: t("report.aiWritten", { model: card.model }),
    });
  }
  blocks.push(...eventsBlocks(candidate, t));
  if (candidate.qualification) {
    blocks.push({ kind: "heading", text: t("report.qualification") });
    blocks.push({ kind: "paragraph", text: candidate.qualification.reason });
    blocks.push(
      ...quoteBlocks(
        candidate.qualification.evidence?.map((item) => ({
          ...item,
          field: "qualification",
          verified: candidate.qualification?.verified ?? false,
        })),
        sources,
        t,
      ),
    );
  }

  if (card?.prior_art) {
    const prior = card.prior_art;
    blocks.push({ kind: "heading", text: t("report.priorArt") });
    blocks.push({ kind: "paragraph", text: prior.note });
    if (prior.query && prior.before) {
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: t("report.priorArtQuery", { query: prior.query, date: formatDate(prior.before) }),
      });
    }
    if (prior.works?.length) {
      blocks.push({
        kind: "references",
        items: prior.works.map((work, index) => ({
          n: index + 1,
          url: work.url,
          text: `${work.title} · ${formatDate(work.date)}`,
        })),
      });
    }
  }

  blocks.push({ kind: "heading", text: t("report.sources", { count: sources.length }) });
  blocks.push({
    kind: "references",
    items: sources.map(({ n, paper, passport }) => ({
      n,
      url: paper.url,
      text: `${paper.title} · ${passportLine(paper.date, passport, t)}`,
      // ТЗ требует пометку о генеративном пересказе возле каждого источника, а не общей строкой.
      note: passport?.digest
        ? `${passport.digest} (${t("report.digestBy", { model: passport.digest_model || "—" })})`
        : passport?.language !== "ru"
          ? t(detail.digests_pending ? "report.digestPending" : "report.digestMissing")
          : undefined,
    })),
  });

  blocks.push({ kind: "heading", text: t("report.aboutMethod") });
  blocks.push({
    kind: "kv",
    rows: [
      { label: t("report.methodVersion"), value: detail.method || dash },
      ...(model
        ? [{ label: t("report.modelVersion"), value: `${model.model} ${model.version}` }]
        : []),
      ...(confidence
        ? [
            {
              label: t("report.confidenceRules"),
              value: join(
                confidence.basis,
                [
                  confidence.method,
                  thresholdPercent(confidence) === null
                    ? undefined
                    : t("confidence.threshold", { percent: thresholdPercent(confidence) }),
                ]
                  .filter(Boolean)
                  .join(" · "),
              ),
            },
          ]
        : []),
      ...(detail.counters
        ? [
            {
              label: t("report.searchCoverage"),
              value: t("report.searchCoverageValue", {
                candidates: formatNumber(detail.counters.candidates ?? 0),
                selected: formatNumber(detail.counters.selected ?? 0),
              }),
            },
          ]
        : []),
    ],
  });
  const limits = limitLines([...detail.topic.limits, ...candidate.limits], t);
  if (limits.length) {
    blocks.push({ kind: "subheading", text: t("report.limits") });
    blocks.push({ kind: "list", items: limits });
  }
  return {
    fileName: `${name.replaceAll(/[^\p{L}\p{N} _-]/gu, "").trim() || "signal"}.docx`,
    title: name,
    blocks,
  };
}

interface DirectionReportInput {
  detail: LiveDetail;
  name: string;
  branchLabel?: string;
  t: TFunction<"trend">;
}

/** Несобранный канал в отчёт не идёт: отсутствие данных читалось бы как отсутствие материалов. */
const seriesTotal = (counts: number[] | null | undefined) =>
  counts?.length ? counts.reduce((sum, value) => sum + value, 0) : null;

const channelTotal = (...channels: (LiveChannel | null | undefined)[]) =>
  channels.some(Boolean) ? channels.reduce((sum, channel) => sum + (channel?.total ?? 0), 0) : null;

export function directionReportDoc({
  detail,
  name,
  branchLabel,
  t,
}: DirectionReportInput): ReportDoc {
  const topic = detail.topic;
  const confidence = topic.confidence;
  const percent = confidence ? Math.round(confidence.value * 100) : null;
  const threshold = thresholdPercent(confidence);
  const dash = "—";
  // Тридцати ссылок хватает для проверки вывода; полный список уходит на десятки страниц.
  const sources = signalSources(detail, topic).slice(0, 30);
  const firstPaper = detail.papers
    .filter((paper) => topic.paper_ids.includes(paper.id))
    .reduce<string>((min, paper) => (!min || paper.date < min ? paper.date : min), "");
  const channels: [string, number | null][] = [
    [t("study.live.channels.papers"), topic.papers_count],
    [t("study.live.channels.repos"), seriesTotal(topic.repos)],
    [t("study.live.channels.hn"), seriesTotal(topic.discussions)],
    [t("study.live.channels.patents"), channelTotal(topic.patents, topic.patents_cn)],
    [t("study.live.channels.web"), channelTotal(topic.web)],
    [t("study.live.channels.registry"), channelTotal(topic.registry)],
    [t("study.live.channels.corpus"), channelTotal(topic.corpus)],
    [t("study.live.channels.research"), channelTotal(topic.research)],
  ];
  const blocks: ReportDoc["blocks"] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("report.directionDocType"),
      title: name,
      meta: [`${t("report.observedAt")}: ${formatDate(detail.observed_at)}`],
      lines: [],
      lead: topic.description || undefined,
      metrics: [
        ...(percent === null
          ? []
          : [{ value: String(percent), unit: "%", label: t("confidence.label") }]),
        ...(topic.papers_count
          ? [{ value: formatNumber(topic.papers_count), label: t("study.live.channels.papers") }]
          : []),
      ],
    },
  ];
  blocks.push({
    kind: "kv",
    rows: [
      { label: t("report.stage"), value: t(`study.stage.${topic.stage}`) },
      {
        label: t("confidence.label"),
        value:
          percent === null
            ? dash
            : [
                t("confidence.value", { percent }),
                confidence?.basis,
                threshold === null ? undefined : t("confidence.threshold", { percent: threshold }),
              ]
                .filter(Boolean)
                .join(" · "),
      },
      { label: t("report.area"), value: branchLabel || dash },
      {
        label: t("report.scienceHistory"),
        value: topic.first_year
          ? t("report.scienceSince", { date: topic.first_year, count: topic.papers_count })
          : dash,
      },
      {
        label: t("report.momentum"),
        value: join(momentumLabel(topic, t), momentumBasis(topic, t)),
      },
      {
        label: t("report.emergence"),
        value: join(emergenceLabel(topic, t), topic.emergence?.basis),
      },
      { label: t("report.evidenceBar"), value: evidenceLevelLabel(topic, t) },
      { label: t("study.dates.first"), value: formatDate(firstPaper) },
      { label: t("study.dates.external"), value: formatDate(topic.earliest_external) },
      { label: t("study.dates.growth"), value: formatDate(topic.growth_start) },
      { label: t("study.dates.detected"), value: formatDate(detail.observed_at) },
    ],
  });
  blocks.push({
    kind: "table",
    title: t("study.live.sourcesTitle"),
    head: [t("report.channel"), t("report.channelCount")],
    rows: channels.flatMap(([label, value]) =>
      value === null ? [] : [[label, formatNumber(value)]],
    ),
  });
  blocks.push({ kind: "heading", text: t("report.sources", { count: sources.length }) });
  blocks.push({
    kind: "references",
    items: sources.map(({ n, paper, passport }) => ({
      n,
      url: paper.url,
      text: `${paper.title} · ${passportLine(paper.date, passport, t)}`,
    })),
  });
  const limits = limitLines([...topic.limits, ...detail.limits], t);
  if (limits.length) {
    blocks.push({ kind: "heading", text: t("report.limits") });
    blocks.push({ kind: "list", items: limits });
  }
  blocks.push({
    kind: "paragraph",
    muted: true,
    text: `${t("report.methodVersion")}: ${detail.method}`,
  });
  return {
    // Название бывает кириллицей, а файл уходит в почту: идентификатор латиницей это переживает.
    fileName: `${topic.id.replaceAll(/[^\w-]/g, "") || "direction"}.docx`,
    title: name,
    blocks,
  };
}
