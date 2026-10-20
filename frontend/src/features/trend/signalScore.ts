import type { TFunction } from "i18next";
import type { LiveTopic } from "./liveApi";

/** До кандидата не дошёл срок поиска или проверка оборвалась: решения по существу нет, и в число
 *  исключённых он не входит. */
export const isUnchecked = (row: { code: string }) =>
  row.code === "qualification_budget" || row.code === "qualification_unavailable";

/** Справочный балл при нехватке данных не считается решением модели для топа. */
export function hasModelDecision(topic: Pick<LiveTopic, "model_score">): boolean {
  const score = topic.model_score;
  return !!(
    score &&
    score.label === "weak_signal" &&
    !score.data_gap &&
    Number.isFinite(score.value) &&
    score.value >= 0 &&
    score.value <= 1 &&
    score.threshold !== undefined &&
    Number.isFinite(score.threshold) &&
    score.threshold >= 0 &&
    score.threshold <= 1 &&
    score.value >= score.threshold
  );
}

/** Метод одной статьи — наблюдение, а не сигнал, какой бы высокой ни была оценка модели. */
export const MIN_INDEPENDENT_SOURCES = 2;

/** Строка краткого списка каналов не несёт: число подтверждений для неё посчитал сервер. */
type Evidence = Partial<
  Pick<LiveTopic, "groups" | "papers_count" | "patents" | "patents_cn" | "registry" | "web">
> & { independent_sources?: number };

/** Как liveIndependentSources на сервере: авторские группы работ или разные издатели записей каналов. */
export function independentSources(topic: Evidence): number {
  if (topic.independent_sources !== undefined) return topic.independent_sources;
  const publishers = new Set(
    [topic.patents, topic.patents_cn, topic.registry, topic.web]
      .flatMap((channel) => channel?.records ?? [])
      .map((record) => {
        const host = URL.canParse(record.url) ? new URL(record.url).hostname.toLowerCase() : "";
        return host.replace(/^www\./, "") || record.source;
      })
      .filter(Boolean),
  );
  return Math.max(topic.groups ?? 0, Math.min(topic.papers_count ?? 0, 1) + publishers.size);
}

/** Модель, иначе доля правил — одно число на всех видах: смысл не меняется при переключении. */
export function signalScore(
  topic: Pick<LiveTopic, "model_score" | "confidence"> & Pick<Partial<LiveTopic>, "shortfall">,
) {
  // Добранная до пятнадцати технология отбор не прошла, что бы ни показывало её число.
  const topUp = !!topic.shortfall;
  const model = topic.model_score;
  if (model) {
    // Модель воздержалась: число остаётся для справки, но «выше порога» оно не значит.
    const gap =
      model.data_gap ||
      (model.label === "insufficient_data" ? ("unspecified" as const) : undefined);
    // Оценка внутри конформной полосы: гарантия допускает обе метки, и «выше порога» она тоже не значит.
    const undecided = model.label === "undecided";
    return {
      percent: Math.round(model.value * 100),
      passed:
        !topUp &&
        !gap &&
        !undecided &&
        model.threshold !== undefined &&
        model.value >= model.threshold,
      model: true,
      threshold: model.threshold,
      gap,
      topUp,
      undecided,
    };
  }
  const confidence = topic.confidence;
  if (!confidence) return null;
  return {
    percent: Math.round(confidence.value * 100),
    passed: !topUp && !!confidence.above_threshold,
    model: false,
    threshold: undefined,
    gap: undefined,
    topUp,
    undecided: false,
  };
}

export function thresholdLabelKey(score: {
  passed: boolean;
  model: boolean;
  topUp?: boolean;
  undecided?: boolean;
}) {
  if (score.topUp) return "report.topUp";
  if (score.undecided) return "report.modelClass.undecided";
  if (score.model)
    return score.passed ? "report.aboveModelThreshold" : "report.belowModelThreshold";
  return score.passed ? "report.aboveThreshold" : "report.belowThreshold";
}

type SignalScore = NonNullable<ReturnType<typeof signalScore>>;

export function dataGapLabel(gap: NonNullable<SignalScore["gap"]>, t: TFunction<"trend">) {
  return `${t("report.modelClass.insufficient_data")}: ${t(`report.dataGap.${gap}`)}`;
}

export function scoreSummary(score: SignalScore, t: TFunction<"trend">) {
  return score.gap
    ? `${dataGapLabel(score.gap, t)} · ${t("confidence.reference", { percent: score.percent })}`
    : `${t("confidence.value", { percent: score.percent })} · ${t(thresholdLabelKey(score))}`;
}

/** Группы обещает подпись над плитками: подтверждённые слабые сигналы, затем прошедшие проверки радара,
 *  в конце добранные до пятнадцати. Внутри группы — по проценту на плитке: читатель сверяет порядок с числом. */
export function orderCandidates<T extends VerdictTopic>(list: T[]): T[] {
  const group = (topic: T) => (isConfirmedSignal(topic) ? 0 : topic.shortfall ? 2 : 1);
  const percent = (topic: T) => {
    const score = signalScore(topic);
    return score && !score.gap ? score.percent : -1;
  };
  return list.toSorted((a, b) => group(a) - group(b) || percent(b) - percent(a));
}

const SHORTFALL_VERDICT: Record<
  string,
  readonly [
    (
      | "single"
      | "thin"
      | "oneGroup"
      | "mixed"
      | "draft"
      | "unchecked"
      | "model"
      | "known"
      | "offTopic"
      | "notTech"
    ),
    "warn-solid" | "muted-solid",
  ]
> = {
  single_source: ["single", "warn-solid"],
  thin_evidence: ["thin", "warn-solid"],
  one_group: ["oneGroup", "warn-solid"],
  incoherent_evidence: ["mixed", "warn-solid"],
  incomplete_card: ["draft", "muted-solid"],
  incomplete_interpretation: ["draft", "muted-solid"],
  qualification_unavailable: ["unchecked", "muted-solid"],
  qualification_budget: ["unchecked", "muted-solid"],
  unassessed: ["unchecked", "muted-solid"],
  below_top: ["unchecked", "muted-solid"],
  classifier_rejected: ["model", "muted-solid"],
  classifier_unavailable: ["unchecked", "muted-solid"],
  mature: ["known", "muted-solid"],
  market_formed: ["known", "muted-solid"],
  well_known: ["known", "muted-solid"],
  long_history: ["known", "muted-solid"],
  off_topic: ["offTopic", "muted-solid"],
  not_technology: ["notTech", "muted-solid"],
};

type VerdictTopic = Parameters<typeof signalScore>[0] & Evidence;

/** Вердикт «слабый сигнал»: решение модели или правил и второе независимое подтверждение. */
export function isConfirmedSignal(topic: VerdictTopic) {
  const score = signalScore(topic);
  return (
    !!score &&
    !score.gap &&
    score.passed &&
    (!score.model || hasModelDecision(topic)) &&
    independentSources(topic) >= MIN_INDEPENDENT_SOURCES
  );
}

export function signalVerdict(topic: VerdictTopic, t: TFunction<"trend">) {
  const score = signalScore(topic);
  const code = topic.shortfall?.code;
  if (code) {
    const [key, tone] = SHORTFALL_VERDICT[code] ?? [];
    return key
      ? ({ text: t(`tile.verdict.${key}`), tone } as const)
      : ({
          text: t(`signalList.reject.${code}`, { defaultValue: code }),
          tone: "warn-solid",
        } as const);
  }
  if (!score || score.gap) return { text: t("tile.verdict.gap"), tone: "muted-solid" } as const;
  if (isConfirmedSignal(topic))
    return { text: t("tile.verdict.weak"), tone: "complement-solid" } as const;
  if (score.passed && (!score.model || hasModelDecision(topic)))
    return { text: t("tile.verdict.single"), tone: "warn-solid" } as const;
  const label = topic.model_score?.label;
  return {
    text:
      label && label !== "weak_signal" && label !== "insufficient_data"
        ? t(`report.modelClass.${label as "mature"}`)
        : t("tile.verdict.model"),
    tone: "muted-solid",
  } as const;
}

export function notPassedNote(shortfall: { code: string }, sources: number, t: TFunction<"trend">) {
  return shortfall.code === "single_source" && sources > 1
    ? t("tile.reason.singleOf", { count: sources })
    : t("tile.notPassed");
}
