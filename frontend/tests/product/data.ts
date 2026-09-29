import type { ApiAccount, AppSettingView, LlmUsage } from "../../src/features/admin/api";
import type { ApiCluster } from "../../src/features/cluster/api";
import type { ApiDocument } from "../../src/features/document/api";
import type {
  ApiHypothesis,
  ApiHypothesisJob,
  ApiRevision,
  Itc,
} from "../../src/features/hypothesis/api";
import type { ApiKPI } from "../../src/features/kpi/api";
import type { ApiChat, ApiMessage } from "../../src/features/search/api";
import type { Watch } from "../../src/features/watch";

// Синтетика прогоняет настоящие компоненты и контракт: ни учётных данных, ни модели не нужно.
export const date = "2026-09-08T09:00:00Z";
const itc: Itc = {
  score: 8,
  techscore: 0.64,
  axes: { momentum: 0.6, novelty: 0.8, impact: 0.7, diffusion: 0.4 },
  components: Object.fromEntries(
    ["SM", "NV", "IP", "HR"].map((key, index) => [
      key,
      {
        key,
        name: key,
        value: [60, 80, 70, 40][index]!,
        norm: [0.6, 0.8, 0.7, 0.4][index]!,
        weight: [0.4, 0.25, 0.2, 0.15][index]!,
        contribution: [0.24, 0.2, 0.14, 0.06][index]!,
        note: "Оценка по демонстрационным источникам.",
      },
    ]),
  ),
  signals: {
    document_count: 8,
    pub_count: 6,
    org_count: 3,
    orgs: ["Тестовая лаборатория"],
    years: [2024, 2025, 2026],
    year_min: 2024,
    year_max: 2026,
    evidence_citations: 0,
    novelty_distance: 0.2,
    probe_count: 5,
    embedded_count: 5,
    freshness_available: true,
    novelty_available: true,
  },
  method: "corpus",
  computed_at: date,
};

export function createData() {
  const users: ApiAccount[] = [
    {
      id: "user-admin",
      username: "review.admin",
      roles: ["admin", "operator", "user"],
      created_at: date,
    },
    { id: "user-researcher", username: "anna.petrova", roles: ["user"], created_at: date },
    { id: "user-operator", username: "ivan.kim", roles: ["operator"], created_at: date },
  ];
  const documents: ApiDocument[] = Array.from({ length: 26 }, (_, index) => ({
    id: `doc-${index + 1}`,
    owner_id: "user-admin",
    filename: `research-${index + 1}.${index === 1 ? "txt" : "pdf"}`,
    title:
      index === 0 ? "Термостойкость полимерных композитов" : `Исследование материалов ${index + 1}`,
    mime_type: index === 1 ? "text/plain" : "application/pdf",
    size: 65536,
    object_key: `review/doc-${index + 1}`,
    status: index === 2 ? "failed" : "indexed",
    status_msg: index === 2 ? "Не удалось прочитать документ" : "",
    chunk_count: index === 2 ? 0 : 12,
    created_at: date,
    updated_at: date,
  }));
  const clusters: ApiCluster[] = [
    {
      id: "cluster-1",
      owner_id: "user-admin",
      label: "Полимерные композиты",
      summary: "Влияние состава и обработки на прочность и термостойкость материалов.",
      keywords: ["полимеры", "прочность", "термостойкость"],
      method: "semantic",
      chunk_count: 42,
      document_count: 8,
      representatives: [
        {
          document_id: "doc-1",
          filename: documents[0]!.filename,
          snippet: "Образцы сохраняют прочность после термообработки.",
        },
      ],
      params: { itc },
      status: "ready",
      created_at: date,
      updated_at: date,
    },
    {
      id: "cluster-2",
      owner_id: "user-admin",
      label: "Защитные покрытия",
      summary: "Стойкость поверхности к износу и коррозии.",
      keywords: ["покрытия", "износ"],
      method: "semantic",
      chunk_count: 24,
      document_count: 4,
      representatives: [],
      params: {
        itc: {
          ...itc,
          score: 6,
          signals: {
            ...itc.signals,
            document_count: 4,
            pub_count: 3,
            probe_count: 4,
            embedded_count: 4,
          },
        },
      },
      status: "ready",
      created_at: date,
      updated_at: date,
    },
  ];
  const kpis: ApiKPI[] = [
    {
      id: "kpi-1",
      owner_id: "user-admin",
      title: "Повысить термостойкость композита",
      description: "Сохранить прочность при рабочей температуре 180 °C.",
      metric: "Сохранение прочности",
      unit: "%",
      direction: "increase",
      baseline: 70,
      target: 90,
      function_area: "Материаловедение",
      status: "active",
      detail: {},
      created_at: date,
      updated_at: date,
    },
    {
      id: "kpi-2",
      owner_id: "user-admin",
      title: "Снизить износ покрытия",
      description: "Сравнить образцы с контрольной группой.",
      metric: "Износ",
      unit: "%",
      direction: "decrease",
      baseline: 20,
      target: 10,
      function_area: "Покрытия",
      status: "archived",
      detail: {},
      created_at: date,
      updated_at: date,
    },
  ];
  const hypothesis: ApiHypothesis = {
    id: "hypothesis-1",
    version: 1,
    owner_id: "user-admin",
    run_id: "run-1",
    title: "Термообработка повышает стойкость композита",
    statement:
      "Термообработка при 180 °C повышает сохранение прочности композита до 90 % при неизменной массе.",
    rationale: "Упорядочение структуры снижает деформацию при нагреве.",
    method: "generated",
    status: "approved",
    kpi_id: "kpi-1",
    primary_cluster_id: "cluster-1",
    trl: 6,
    novelty_score: 0.72,
    risk_score: 0.2,
    value_score: 0.82,
    confidence_score: 0.84,
    composite_score: 0.78,
    measurable: true,
    organization: "Тестовая лаборатория",
    function_area: "Материаловедение",
    source_type: "article",
    location: "Лаборатория",
    tags: ["полимеры", "термообработка"],
    assessment: {
      itc,
      novelty: { score: 0.72, rationale: "Сопоставлены разные режимы обработки." },
      risk: { score: 0.2, level: "low", factors: ["Неравномерный прогрев"] },
      value: { score: 0.82, rationale: "Увеличение рабочего диапазона температур." },
      scores: {
        belief_score: 0.84,
        verdict_confidence: 0.8,
        evidence_quality: 0.75,
        unverified: false,
      },
      schema: { score: 1, complete: true, missing: [] },
      check: {
        verdict: "supported",
        confidence: 0.8,
        rationale: "Два источника подтверждают эффект; один описывает ограничение.",
        supporting: ["chunk-1"],
        contradicting: ["chunk-2"],
        model: "test-model",
        checked_at: date,
      },
      trl: {
        level: 6,
        name: "Проверка прототипа",
        rationale: "Испытания на контрольных образцах.",
        levels: [
          { level: 5, met: true },
          { level: 6, met: true },
          { level: 7, met: false },
        ],
      },
      ranking: {
        score: 0.78,
        factors: [
          {
            key: "evidence",
            label: "Доказательная база",
            weight: 1,
            value: 0.78,
            contribution: 0.78,
            detail: "Проверены три источника.",
            scored: true,
          },
        ],
      },
    },
    detail: {
      material_system: "Полимерный композит",
      process_change: "Контролируемая термообработка",
      target_property: "Сохранение прочности",
      problem_addressed: "Потеря прочности при нагреве.",
      microstructure_mechanism: "Стабилизация структуры.",
      drivers: ["Рабочая температура", "Стоимость обработки"],
      application_potential: {
        objects: ["Защитные детали"],
        conditions: "Температура до 180 °C",
        constraints: ["Не менять массу"],
      },
      quantitative_parameters: [{ name: "Температура", value: 180, unit: "°C" }],
      causal_chain: [
        { stage: "Обработка", change: "Контролируемый нагрев" },
        { stage: "Свойство", change: "Рост термостойкости" },
      ],
      verification: {
        method: "Сравнительные испытания",
        metrics: ["Сохранение прочности"],
        success_criteria: "Не менее 90 %",
        horizon: "2 недели",
      },
      experiment_plan: {
        materials: ["Контрольные образцы"],
        process_parameters: [{ name: "Температура", range: "160–180 °C" }],
        test_methods: ["Испытание на растяжение"],
        controls: ["Без термообработки"],
        success_criteria: ["Сохранение прочности не ниже 90 %"],
        estimated_cost: "50 000 ₽",
        estimated_time: "2 недели",
        risks: ["Неравномерный нагрев"],
      },
      competitors: {
        summary: "Другой подход — изменение состава.",
        items: [
          {
            name: "Изменение наполнителя",
            approach: "Повышение доли наполнителя",
            strengths: ["Устойчивость"],
            weaknesses: ["Рост массы"],
          },
        ],
      },
      classification: {
        research_type: "практическое",
        grnti: [{ code: "61.61", name: "Полимерные материалы" }],
        vak: [{ code: "2.6.17", name: "Материаловедение" }],
        asjc: [{ code: "2500", name: "Materials Science" }],
      },
    },
    generation: { model: "test-model", constraints: "Сохранить массу", kind: "kpi" },
    created_at: date,
    updated_at: date,
    evidence_count: 3,
    evidence_document_count: 3,
    evidence_support_count: 1,
    evidence_contradict_count: 1,
    evidence: ["supports", "contradicts", "context"].map((stance, index) => ({
      id: `evidence-${index + 1}`,
      document_id: `doc-${index + 1}`,
      chunk_id: `chunk-${index + 1}`,
      filename: documents[index]!.filename,
      snippet: `Контрольный образец ${index + 1}: сохранение прочности после нагрева составило 90 %.`,
      stance: stance as "supports" | "contradicts" | "context",
      score: 0.9,
      ord: index,
      page_start: 1,
      page_end: 1,
      relation: "Результат сравнительных испытаний",
      origin: "knowledge" as const,
    })),
  };
  const hypotheses: ApiHypothesis[] = [
    hypothesis,
    ...(["generated", "under_review", "rejected", "archived"] as const).map((status, index) =>
      Object.assign(structuredClone(hypothesis), {
        id: `hypothesis-${index + 2}`,
        title: [
          "Добавка снижает износ покрытия",
          "Наполнитель увеличивает прочность",
          "Новый режим охлаждения",
          "Предыдущая версия состава",
        ][index]!,
        status,
      }),
    ),
    {
      ...structuredClone(hypothesis),
      id: "direction-1",
      title: "Термостойкие композиты для защитных деталей",
      status: "draft",
      generation: { semantic_kind: "research_direction", kind: "auto_cluster" },
    },
  ];
  const chats: ApiChat[] = Array.from({ length: 25 }, (_, index) => ({
    id: `chat-${index + 1}`,
    owner_id: "user-admin",
    owner_username: "review.admin",
    title:
      index === 0 ? "Как повысить термостойкость композита?" : `Сравнение материалов ${index + 1}`,
    source: "search",
    created_at: date,
  }));
  const messages: ApiMessage[] = [
    {
      id: "message-1",
      chat_id: "chat-1",
      role: "user",
      content: chats[0]!.title,
      created_at: date,
    },
    {
      id: "message-2",
      chat_id: "chat-1",
      role: "assistant",
      content:
        "**Термообработка** помогает сохранить прочность при нагреве [S1]. Сравните режимы обработки на контрольных образцах.",
      sources: [
        {
          document_id: "doc-1",
          filename: documents[0]!.filename,
          chunk_id: "chunk-1",
          snippet: "Сохранение прочности составило 90 %.",
          score: 0.9,
          page_start: 1,
        },
      ],
      created_at: date,
      meta: {
        model: "test-model",
        cached: false,
        trace: {
          stages: [{ stage: "generate", millis: 120 }],
          candidatesDense: 24,
          candidatesLexical: 12,
          candidatesFused: 30,
          candidatesReturned: 1,
          cited: ["S1"],
          uncitedRemoved: [],
          topScore: 0.9,
          scoreFloor: 0.4,
          degraded: false,
          degradedReason: "",
          abstainReason: "",
          raptorExpanded: 0,
        },
      },
    },
  ];
  const settings: AppSettingView[] = Object.entries({
    VLLM_URL: "http://test-model:8000/v1",
    VLLM_MODEL: "test-model",
    VLLM_API_KEY: "",
    PUBSEARCH_ENABLED: "true",
    PUBSEARCH_MAILTO: "review@example.test",
    RAG_GEN_BREADTH: "false",
    KPI_SUGGEST: "true",
    RAG_GEN_QUALITY_FLOOR: "0.5",
    LLM_RUB_PER_USD: "90",
    LLM_COST_CURRENCY: "USD",
    LLM_PRICES: "",
  }).map(([key, value]) => ({
    key,
    kind:
      key.includes("ENABLED") || key === "RAG_GEN_BREADTH" || key === "KPI_SUGGEST"
        ? "bool"
        : key.endsWith("KEY")
          ? "secret"
          : "string",
    group: "llm",
    default: ["VLLM_URL", "VLLM_API_KEY", "PUBSEARCH_MAILTO"].includes(key) ? "" : value,
    override: "",
    hasOverride: false,
    envValue: "",
    envSet: key === "VLLM_URL" || key === "PUBSEARCH_MAILTO",
    envOnly: ["VLLM_URL", "VLLM_API_KEY", "PUBSEARCH_MAILTO"].includes(key),
    source: key === "VLLM_URL" || key === "PUBSEARCH_MAILTO" ? "env" : "default",
  }));
  const usageRow = {
    requests: 12,
    prompt_tokens: 2000,
    completion_tokens: 1000,
    total_tokens: 3000,
    cost_usd: 0.25,
  };
  const usage: LlmUsage = {
    range_days: 7,
    from: "2026-09-02",
    to: "2026-09-08",
    provider: "Тестовая модель",
    currency: "₽",
    cost_estimated: true,
    total_cost_rub: 22.5,
    rub_per_usd: 90,
    totals: usageRow,
    providers: [
      {
        ...usageRow,
        key: "local",
        label: "Тестовая модель",
        currency: "₽",
        kind: "free",
        cost_estimated: true,
        cost_rub: 0,
      },
    ],
    by_day: [{ ...usageRow, day: "2026-09-08" }],
    by_model: [{ ...usageRow, model: "test-model" }],
    by_operation: [{ ...usageRow, operation: "generate" }],
    per_hypothesis: {
      ...usageRow,
      hypotheses: 5,
      cost_rub: 22.5,
      operations: ["generate", "verify"],
    },
    budget: { credits_total: 100, credits_used: 25, credits_remaining: 75 },
    quota: { today_requests: 12, daily_limit: 500, per_min_used: 1, per_min_limit: 30 },
  };
  return {
    users,
    documents,
    clusters,
    kpis,
    hypotheses,
    chats,
    messages,
    settings,
    usage,
    searches: [
      {
        query: "поиск с дополненной генерацией",
        fresh: true,
        papers: 1190,
        sources: 1195,
        observed_at: "2026-09-16T01:16:00Z",
        expires_at: "2026-09-17T01:16:00Z",
      },
    ],
    watches: [] as Watch[],
    jobs: [] as ApiHypothesisJob[],
    revisions: [] as ApiRevision[],
    weights: {
      kpi_fit: 0.2,
      evidence: 0.2,
      novelty: 0.15,
      value: 0.15,
      risk_inv: 0.15,
      trl_fit: 0.15,
    },
    runtime: {
      default_generate_count: 5,
      cluster_generate_count: 3,
      direction_generate_count: 3,
      generation_timeout_sec: 300,
      ready_trl_min: 4,
      ready_score_min: 55,
      risk_score_min: 70,
      graph_direction_limit: 5,
      deep_postprocess_enabled: false,
      excluded_directions: "",
    },
  };
}
