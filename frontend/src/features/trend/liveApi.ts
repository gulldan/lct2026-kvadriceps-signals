import { ApiError, authFetch, postJSON, request } from "@/shared/api/client";
import type { RadarBet } from "@/features/hypothesis";
import type { StudyPaper, StudyTopic } from "./studyApi";
import { orderCandidates } from "./signalScore";
import { readableReason } from "./signalSources";

export interface LiveLimit {
  code?: string;
  values?: Record<string, number>;
  text: string;
}

interface LivePeriod {
  period: string;
  start: string;
  end_exclusive: string;
  complete: boolean;
  population: number;
}

/** Обзор не возит semantic, market, adoption и паспорта: поля отсутствуют, а не обнуляются. */
export interface LiveTopicMoney {
  total: number;
  kinds: Record<string, number>;
  stages?: Record<string, number>;
  investors?: string[];
}

export interface LiveTopic extends Omit<StudyTopic, "id" | "screening"> {
  id: string;
  screening: "query";
  name: string;
  query: { arxiv: string; github: string; hn: string; terms?: string[] };
  curated: boolean;
  source: "curated" | "discovered" | "search" | "candidate";
  branch?: string;
  /** Отрасль каталога dealroom_research_v1: приходит только в обзоре радара. */
  industry?: string;
  /** Ранние деньги, где упомянута технология направления, — маркер «инвесторы уже заметили». */
  money?: LiveTopicMoney | null;
  label?: string;
  description?: string;
  events?: LiveAdoptionEvent[] | null;
  tz?: LiveTZAssessment | null;
  citations?: {
    source: string;
    observed_at: string;
    papers: number;
    missing: number;
    total: number;
    cited: number;
    expected: number;
    normalized: number | null;
  } | null;
  papers_count: number;
  documents_count?: number;
  records_count?: number;
  evidence_level?: "strict" | "relaxed" | "thin";
  shortfall?: { code: string; reason: string } | null;
  repos: number[] | null;
  discussions: number[] | null;
  limits: LiveLimit[];
  uniqueness: number | null;
  relevance: number | null;
  /** Близость этой технологии к запросу: относимость зонтика её не заменяет. */
  query_relevance?: number | null;
  comparison?: {
    prior_start: string;
    recent_start: string;
    end_exclusive: string;
    days: number;
    prior: number;
    current: number;
    fresh: number;
  } | null;
  patents?: LiveChannel | null;
  patents_cn?: LiveChannel | null;
  registry?: LiveChannel | null;
  web?: LiveChannel | null;
  corpus?: LiveChannel | null;
  research?: LiveChannel | null;
  confidence?: LiveConfidence | null;
  market?: LiveMarket | null;
  adoption?: LiveAdoption | null;
  card?: LiveCard | null;
  model_score?: LiveModelScore | null;
  emergence?: LiveEmergence | null;
  qualification?: {
    label: string;
    technology: boolean;
    relevant: boolean;
    coherent: boolean;
    verified: boolean;
    reason: string;
    method: string;
    model: string;
    verifier_model: string;
    evidence?: { source_id: string; quote: string }[];
  } | null;
  analysis?: {
    version: string;
    discovery?: {
      method: string;
      sampled: boolean;
      profile_basis?: string;
      missing_abstracts?: number;
      aliases: string[];
      history: { kind: string; at: string; name: string; old_name?: string; parents?: string[] }[];
    } | null;
    evidence: {
      observed_at?: string;
      attributed_papers: number;
      unattributed_papers: number;
      disjoint_organization_groups: number;
      distinct_events: number;
      dependent_records: number;
      independent_implementations: number | null;
      groups: { paper_ids: string[]; organization_ids: string[]; sources: string[] }[];
      links: { kind: string; url: string; source_url: string }[];
    };
    burst: {
      status: string;
      flag: boolean | null;
      period?: string;
      count: number;
      expected: number | null;
      quality_status: string;
    };
  } | null;
  semantic?: {
    profile_basis?: string;
    quality_status?: string;
    sample_distance_range?: { low: number; high: number; runs: number } | null;
    compared_topic_ids?: string[];
    compared_topic_names?: string[];
    nearest_sample_paper_ids?: string[];
    missing_abstracts?: number;
    method: string;
    model: string;
    revision: string;
    compared_topics: number;
    sample_paper_ids: string[];
    nearest_id: string;
    nearest_name: string;
    similarity: number;
    distance: number;
    query_similarity: number | null;
  } | null;
}

interface LiveSourceCoverage {
  source: string;
  from: string;
  through: string;
  received: number;
  titles: number;
  content: number;
  missing_titles: number;
  families: number;
  provider_total: number | null;
  next_cursor: string;
  stop_reason: string;
  exhausted: boolean;
}
export interface LiveVolume {
  scanned: number;
  fetched: number;
  processed: number;
  stored: number;
  chars: number;
  pages: number;
  tokens: number;
  channels?: { channel: string; records: number; chars: number }[];
  model?: {
    calls: number;
    failed: number;
    unreported: number;
    prompt_chars: number;
    prompt_tokens: number;
    completion_tokens: number;
    duration_ms: number;
    slowest_ms: number;
    operations?: {
      operation: string;
      calls: number;
      failed: number;
      prompt_tokens: number;
      completion_tokens: number;
      duration_ms: number;
      slowest_ms: number;
    }[];
  };
  analysis?: {
    requests: number;
    papers: number;
    chars: number;
    tokens: number;
    duration_ms: number;
  };
  classifier?: {
    connected: boolean;
    candidates: number;
    sources: number;
    chars: number;
    tokens: number;
    bytes: number;
  };
}

export interface LiveCounters {
  sources_processed: number;
  papers: number;
  registry: number;
  research?: number;
  patents: number;
  web: number;
  external: number;
  trusted_sources: number;
  lowered_sources: number;
  candidates: number;
  selected?: number;
  rejected?: number;
  above_threshold: number;
  model_scored?: number;
  model_above_threshold?: number;
  top_up?: number;
}

interface LiveCard {
  summary?: string;
  advantage?: string;
  example?: string;
  report_assessment?: string;
  study_limitations?: string;
  example_id?: string;
  example_type?: LiveEventType | "";
  model?: string;
  basis?: string;
  missing?: string[];
  evidence?: LiveClaimEvidence[];
  verification_model?: string;
  prior_art?: LivePriorArt;
}

export interface LivePriorArt {
  status: "candidates" | "not_found" | "unavailable";
  query?: string;
  before?: string;
  note: string;
  source_url?: string;
  works?: {
    id: string;
    title: string;
    date: string;
    url: string;
    doi?: string;
    abstract?: string;
  }[];
}

export interface LiveClaimEvidence {
  field: string;
  source_id: string;
  quote: string;
  verified: boolean;
}

interface LiveModelScore {
  value: number;
  model: string;
  version: string;
  basis: string;
  label?: "weak_signal" | "mature" | "noise" | "failed" | "insufficient_data" | "undecided";
  data_gap?: "not_collected" | "not_found" | "too_early";
  threshold?: number;
  feature_schema?: string;
  features?: { name: string; value: string; contribution: number }[];
}

interface LiveMarket {
  known: boolean;
  mature: boolean;
  basis: string;
  facts?: string[];
  checks?: string[];
  status?: "unknown" | "early" | "forming" | "mature";
  evidence?: LiveClaimEvidence[];
}

/** rank 0 — «не измерено»: пустая стадия показывается словом, а не прячется. */
interface LiveAdoption {
  code: "concept" | "prototype" | "pilot" | "early_adoption" | "unknown";
  rank: number;
  documents: number;
  matched: number;
  basis: string;
  facts?: string[];
  evidence?: LiveClaimEvidence[];
}

export interface LiveConfidence {
  value: number;
  passed: number;
  total: number;
  measured?: number;
  method: string;
  basis: string;
  /** Константа метода живёт на сервере: подпись читает её отсюда, а не держит свою копию. */
  threshold: number;
  reasons?: string[];
  missing?: string[];
  /** Правил нечем проверить: их нет ни в числителе, ни в знаменателе доли. */
  unmeasured?: string[];
  above_threshold: boolean;
}

interface LiveEmergence {
  method: string;
  class: "weak_signal" | "strong_signal" | "well_known" | "latent" | "unmeasured";
  basis: string;
  periods_used: number;
  dov_mean: number | null;
  dov_growth: number | null;
  dod_mean: number | null;
  dod_growth: number | null;
  set?: {
    size: number;
    dov_mean: number;
    dov_growth: number;
    dod_mean: number;
    dod_growth: number;
  };
}

export type LiveEventType =
  | "grant_early"
  | "grant_late"
  | "code_release"
  | "prototype"
  | "product_launch"
  | "pilot"
  | "deployment"
  | "funding"
  | "hiring"
  | "standard"
  | "lab_result";

export type LiveSourceRecord = NonNullable<LiveChannel["records"]>[number];

export interface LiveAdoptionEvent {
  type: LiveEventType | "";
  source: string;
  record: LiveSourceRecord;
  actor?: string;
  amount?: string;
  phase?: string;
  quote?: string;
  count?: number | null;
  delta?: number | null;
  /** releases, downloads и views считаются за месяц. */
  unit?: string;
  verified: boolean;
  basis?: string;
}

export interface LiveTZAssessment {
  stage_code: number;
  stage_basis: string;
  stage_event?: LiveAdoptionEvent | null;
  trend_code: number;
  trend_basis: string;
  score: number;
}

export interface LiveChannel {
  source_set?: string;
  coverage?: LiveSourceCoverage[];
  source: string;
  observed_at: string;
  counts: (number | null)[];
  total: number;
  partial: boolean;
  /** Канал отдаёт оговорки строкой, без кода и чисел: это не LiveLimit. */
  limits: string[];
  records?: {
    id: string;
    title: string;
    date: string;
    url: string;
    source: string;
    language?: string;
    language_basis?: string;
    publisher_type?: string;
    genre?: string;
    doi?: string;
    authors?: string[];
    trust?: string;
    trust_basis?: string;
    text_status?: string;
    filing?: string;
    priority?: string;
    family_id?: string;
    original_url?: string;
  }[];
}

export interface LivePaperPassport {
  language?: string;
  language_basis?: string;
  publisher_type?: string;
  genre?: string;
  source_name?: string;
  discovery_source?: string;
  trust?: string;
  trust_basis?: string;
  digest?: string;
  digest_model?: string;
}

interface LiveManifest {
  version: string;
  method: string;
  observed_at: string;
  categories: string;
  periods: LivePeriod[];
  limits: LiveLimit[];
  github_authenticated: boolean;
}

export interface LiveOverview extends LiveManifest {
  citations?: {
    source: string;
    observed_at: string;
    cells: number;
    papers: number;
    requests: number;
  } | null;
  status: "ready" | "collecting" | "failed";
  error: string;
  error_code?: string;
  refreshing: boolean;
  collection?: {
    phase: string;
    checked_at: string;
    next_run_at: string;
    last_success_at: string;
    last_source_at: string;
    error: string;
    error_code?: string;
    channels: {
      name: "patents" | "web";
      status: string;
      observed_at: string;
      attached_at: string;
      next_run_at: string;
      error: string;
      error_code?: string;
      collecting: boolean;
    }[];
  } | null;
  topics: LiveTopic[];
  duration: string;
}

export interface LiveRejection {
  name: string;
  original_name?: string;
  code:
    | "mature"
    | "market_formed"
    | "hype"
    | "long_history"
    | "off_topic"
    | "thin_evidence"
    | "unassessed"
    | "below_top"
    | "duplicate"
    | "one_group"
    | "well_known"
    | "not_technology"
    | "incoherent_evidence"
    | "single_source"
    | "qualification_unavailable"
    | "qualification_budget"
    | "incomplete_card"
    | "incomplete_interpretation"
    | "classifier_rejected";
  reason: string;
  stage: StudyTopic["stage"];
  papers: number;
  confidence: number;
  model_value?: number;
}

export interface LiveDetail extends LiveManifest {
  topic: LiveTopic;
  papers: StudyPaper[];
  candidates?: LiveTopic[];
  rejected?: LiveRejection[];
  passports?: Record<string, LivePaperPassport>;
  counters?: LiveCounters;
  volume?: LiveVolume;
  search_contract?: string;
  relevance_floor: number;
  /** Сохранённый ответ открыт с оценкой прежней модели; новая придёт в следующих опросах. */
  rescoring?: boolean;
  digests_pending?: boolean;
}

export function getLiveOverview(signal?: AbortSignal) {
  return request<LiveOverview>("/trends/live", { signal });
}

interface WeakSignalScores {
  tp: number;
  fp: number;
  fn: number;
  tn: number;
  precision: number;
  recall: number;
  f1: number;
}

/** etalon — эталон организаторов, team — отрицательные примеры команды, mullany — ретроспектива
 *  хайп-циклов, cosmos — старые малоизвестные технологии, hype — нынешний хайп. */
type WeakSignalSource = "etalon" | "team" | "mullany" | "cosmos" | "hype";

/** Приставка — источник признака: без неё окно arXiv, history_ — OpenAlex, hn_ — Hacker News,
 *  llm_ — оценка зрелости языковой моделью. */
export type WeakSignalInputKind =
  | "log_documents"
  | "active_periods"
  | "groups_per_document"
  | "top_group_share"
  | "growth_per_document"
  | "months_observed"
  | "window_empty"
  | "history_log_works"
  | "history_share_before"
  | "history_growth"
  | "history_log_recent"
  | "history_age_years"
  | "history_slope"
  | "history_share_last3"
  | "hn_log_total"
  | "hn_share_before"
  | "hn_growth"
  | "hn_log_recent"
  | "hn_log_recent_story"
  | "hn_story_growth"
  | "llm_stage"
  | "llm_failed"
  | "llm_confidence";

/** Описаны только поля, которые читает страница проверки; остальное сервер отдаёт как есть. */
export interface WeakSignalModelCheck {
  model: {
    version: string;
    embedded_inference_version?: string;
    threshold: number;
    /** Квантили несоответствия по классам: «сигнал» допустим при оценке не ниже 1 − weak, «не сигнал» — не выше not_weak. */
    conformal?: { alpha: number; weak: number; not_weak: number };
    /** Порядок совпадает с coef. */
    inputs: `${"broad" | "narrow"}_${WeakSignalInputKind}`[];
    /** Веса на стандартизованных входах, поэтому их модули сравнимы. */
    coef: number[];
    embedding: { model: string; coef: number[] };
  };
  evaluation: {
    month: string;
    dataset: Record<WeakSignalSource, number>;
    nested_cv: {
      repeats: number;
      folds: number;
      scope: WeakSignalSource[];
      f1_mean: number;
      f1_sd: number;
      f1_min: number;
      precision_mean: number;
      recall_mean: number;
      auc_mean: number;
      first_repeat: WeakSignalScores;
    };
    baseline_all_weak: WeakSignalScores;
    leave_source_out_false_signal: Partial<Record<WeakSignalSource, number>>;
    leave_area_out_recall: Record<string, number>;
    /** Разделы ниже появились с версией 2026-09-27; прежний отчёт их не содержит. */
    llm_rating?: { model: string; prompt_sha256: string; collected: string };
    selection?: { recall_target: number; c_grid: number[]; thresholds: [number, number, number] };
    /** Меры и матрица ошибок — средние за повтор внешнего цикла. */
    slices?: Record<WeakSignalSlice, WeakSignalScores>;
    /** Доля названных сигналом, когда источник был в обучении. */
    false_signal_by_source?: Partial<Record<WeakSignalSource, number>>;
    ablations?: Record<
      WeakSignalAblation,
      WeakSignalScores & {
        auc: number;
        /** Различение эталона с каждым видом отрицательных и со всеми сразу: не зависит от порога. */
        auc_vs: Record<Exclude<WeakSignalSource, "etalon"> | "all", number>;
        all_f1: number;
        false_signal: Partial<Record<WeakSignalSource, number>>;
        unseen_hype: number;
      }
    >;
    /** Источник не ответил на проверке: вместо его признаков — медиана обучения. */
    outage?: Record<
      WeakSignalOutage,
      WeakSignalScores & { false_signal: Partial<Record<WeakSignalSource, number>> }
    >;
    /** Конформное решение: квантили по четырём частям внешнего цикла, проверка на пятой. */
    conformal?: {
      alpha: number;
      coverage: { weak: number; not_weak: number };
      undecided: number;
      weak_precision: number;
      by_source: Record<WeakSignalSource, Record<"weak" | "undecided" | "not_weak", number>>;
    };
    /** Правило «мало данных» радара; здесь эти строки оценены, число показано отдельно. */
    abstention?: {
      by_source: Record<WeakSignalSource, { total: number; abstained: number }>;
      etalon_team_without: WeakSignalScores;
    };
    items: {
      id: string;
      name: string;
      class: "weak_signal" | "mature" | "noise" | "hype";
      area: string;
      source: WeakSignalSource;
      phrases: string[];
      validation: number;
      validation_weak: boolean;
      data_gap?: "not_found" | "too_early" | "not_collected";
    }[];
  };
}

export type WeakSignalSlice = "etalon_team" | "etalon_hype" | "all";
export type WeakSignalAblation = "full" | "llm_only" | "no_llm" | "no_embedding" | "no_hn";
export type WeakSignalOutage = "llm" | "history" | "hn";

export function getLiveModel(signal?: AbortSignal) {
  return request<WeakSignalModelCheck>("/trends/live/model", { signal });
}

export type ModelFileNote =
  | "no_name"
  | "no_phrases"
  | "no_latin"
  | "timeout"
  | "window_unavailable"
  | "no_history"
  | "not_in_window";

export interface ModelFileRow {
  number: number;
  name: string;
  label: boolean | null;
  /** Метка, которую сервер не понял: строка оценена, но в меры не входит. */
  label_text?: string;
  phrases: string[] | null;
  phrases_from?: "file" | "name" | "gateway";
  score: number | null;
  note?: ModelFileNote;
  /** Вклад каждого источника в оценку, в единицах логарифма шансов. */
  features?: { name: string; value: string; contribution: number }[];
}

export interface ModelFileMetrics {
  labeled: number;
  scored: number;
  /** Размеченные строки без оценки — воздержание, не ошибка. */
  abstained: number;
  abstained_weak: number;
  unknown_labels: number;
  tp: number;
  fp: number;
  fn: number;
  tn: number;
  precision: number;
  recall: number;
  f1: number;
  accuracy: number;
}

export interface ModelFileReport {
  version: string;
  threshold: number;
  month: string;
  rows: ModelFileRow[];
  metrics: ModelFileMetrics | null;
}

export interface ModelFileCheck {
  id: string;
  status: "running" | "done";
  report?: ModelFileReport;
}

/** Файл разбирает сервер тем же кодом, что и команда обучения, — поэтому он уходит как есть. */
export function startLiveModelCheck(file: File, signal?: AbortSignal) {
  return request<ModelFileCheck>("/trends/live/model/check", {
    method: "POST",
    body: file,
    signal,
  });
}

export function getLiveModelCheck(id: string, signal?: AbortSignal) {
  return request<ModelFileCheck>(`/trends/live/model/check/${encodeURIComponent(id)}`, {
    signal,
  });
}

/** Один порядок технологий и один текст причин для плиток, таблицы, «Технология N из 15» и выгрузки.
 *  Ответ только что разобран из JSON, поэтому причины правятся на месте. */
function ordered(detail: LiveDetail): LiveDetail {
  for (const row of detail.candidates ?? [])
    if (row.qualification) row.qualification.reason = readableReason(row.qualification.reason);
  for (const row of detail.rejected ?? []) row.reason = readableReason(row.reason);
  return detail.candidates ? { ...detail, candidates: orderCandidates(detail.candidates) } : detail;
}

export function getLiveTopic(id: string, signal?: AbortSignal) {
  return request<LiveDetail>(`/trends/live/topics/${encodeURIComponent(id)}`, { signal }).then(
    ordered,
  );
}

export interface LiveCover {
  image_url: string;
  caption: string;
  figure?: string;
  paper: { id: string; title: string; url: string };
}

/** Через API под токеном, которого тег img не несёт; data: не требует отзыва, как blob:. */
export async function getLiveCover(id: string, signal?: AbortSignal) {
  return coverWithImage(`/trends/live/topics/${encodeURIComponent(id)}/cover`, signal);
}

export async function getPaperCover(paperId: string, signal?: AbortSignal) {
  return coverWithImage(`/trends/live/papers/${encodeURIComponent(paperId)}/cover`, signal);
}

async function coverWithImage(path: string, signal?: AbortSignal) {
  const cover = await request<LiveCover | undefined>(path, { signal });
  if (!cover) return null;
  const resp = await authFetch(cover.image_url, { signal });
  if (!resp.ok) throw new ApiError(resp.status, resp.statusText);
  const blob = await resp.blob();
  const image = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => resolve(reader.result as string));
    reader.addEventListener("error", () => reject(reader.error));
    reader.readAsDataURL(blob);
  });
  return { ...cover, image };
}

/** Порядок тот же, что объявляет сервер в step_index; список один на интерфейс. */
export const LIVE_SEARCH_STEPS = [
  "arxiv",
  "scholar",
  "github",
  "hn",
  "patents",
  "web",
  "registry",
  "corpus",
  "analysis",
  "cards",
] as const;
export type LiveSearchStep = (typeof LIVE_SEARCH_STEPS)[number];

export interface LiveSearchState {
  query: string;
  status: "running" | "ready" | "failed";
  reopening?: boolean;
  step?: LiveSearchStep | "rescore";
  step_index?: number;
  step_total?: number;
  visited_steps?: LiveSearchStep[];
  wait_seconds?: number;
  elapsed_seconds: number;
  typical_seconds?: number;
  found?: string[];
  error?: string;
  detail?: LiveDetail;
}

export interface LiveSearchEntry {
  query: string;
  fresh: boolean;
  terms?: string[];
  papers: number;
  sources: number;
  observed_at?: string;
  expires_at?: string;
}

export function listLiveSearches(signal?: AbortSignal) {
  return request<{ entries: LiveSearchEntry[] }>("/trends/live/searches?limit=12", { signal });
}

export interface LiveSearchTop {
  query: string;
  found: boolean;
  observed_at?: string;
  candidates: (Pick<LiveTopic, "id" | "name" | "label" | "stage" | "confidence" | "model_score"> &
    Pick<Partial<LiveTopic>, "tz" | "shortfall"> & { independent_sources?: number })[];
}

export function getLiveSearchTop(query: string, limit: number, signal?: AbortSignal) {
  const params = new URLSearchParams({ q: query, limit: String(limit) });
  return request<LiveSearchTop>(`/trends/live/search/top?${params}`, { signal }).then((top) => ({
    ...top,
    candidates: orderCandidates(top.candidates),
  }));
}

export function searchLive(query: string, signal?: AbortSignal) {
  return request<LiveSearchState>(`/trends/live/search?q=${encodeURIComponent(query)}`, {
    signal,
  }).then((state) => (state.detail ? { ...state, detail: ordered(state.detail) } : state));
}

export interface LiveExecutiveItem {
  id: string;
  name: string;
  summary?: string;
  benefit?: string;
  horizon?: "0-1" | "1-3" | "3+";
  step?: "pilot" | "watch" | "reject";
  reason?: string;
}

export interface LiveExecutive {
  run_id: string;
  query: string;
  observed_at: string;
  sources_processed: number;
  candidates: number;
  selected: number;
  items: LiveExecutiveItem[];
  model?: string;
  written_at: string;
}

/** Запись, а не чтение: несчитанная справка стоит минуту работы модели. */
export function writeLiveExecutive(
  target: { q: string } | { topic: string },
  signal?: AbortSignal,
) {
  return request<LiveExecutive>(`/trends/live/executive?${new URLSearchParams(target)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
    signal,
  });
}

export interface LiveSignalBrief {
  title: string;
  problem: string;
  hypothesis: string;
  grounds: string;
  method: string;
  criterion: string;
  bet: RadarBet;
  model?: string;
  basis: string;
  gap?: "llm_unavailable";
  written_at: string;
}

/** Карточка уходит целиком: выдача могла ещё не лечь в базу. */
export function writeSignalBrief(body: {
  query: string;
  name: string;
  candidate: LiveTopic;
  sources: { id: string; title: string; date: string }[];
}) {
  return postJSON<LiveSignalBrief>("/trends/live/signal-brief", body);
}

export function signalAffinity(text: string, options: string[]) {
  return postJSON<{ similarity: number[]; floor: number }>("/trends/live/affinity", {
    text,
    options,
  });
}
