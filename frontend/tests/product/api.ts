import { createData, date } from "./data";
import trendStudy from "../fixtures/trend-study.json" with { type: "json" };
import trendLive from "../fixtures/trend-live.json" with { type: "json" };
import trendLiveTopic from "../fixtures/trend-live-topic.json" with { type: "json" };
import trendLiveModel from "../fixtures/trend-live-model.json" with { type: "json" };
import technology from "../fixtures/technology.json" with { type: "json" };
import type { ApiHypothesis } from "../../src/features/hypothesis/api";

type Reply = { status: number; json?: unknown; pdf?: boolean; text?: string };
export type TestApi = ReturnType<typeof createTestApi>;
const ok = (json: unknown): Reply => ({ status: 200, json });
const missing = (): Reply => ({ status: 404, json: { error: "Запись не найдена" } });

function topicMetrics(run: string, topic: string) {
  const base = {
    method_version: "signalgraph-method-v7",
    computed_at: "2026-09-13T10:00:00Z",
    window: { from: "2026-03-01", to: "2026-08-31" },
  };
  return {
    analysis_id: run,
    analysis_run_id: run,
    schema_version: "analysis-result-v1",
    method_hash: "m".repeat(64),
    topic,
    title: "Направление",
    history_mode: "as_of",
    cutoff: "2026-09-01",
    metrics: {
      volume: {
        ...base,
        value: 128,
        unit: "работ",
        status: "measured",
        numerator: 128,
        denominator: 118981,
      },
      growth: {
        ...base,
        value: 0.42,
        status: "measured",
        numerator: 128,
        denominator: 90,
        interval: { low: 0.1, high: 0.7, level: 0.9, method: "cluster-bootstrap" },
      },
      velocity: {
        ...base,
        value: null,
        status: "unavailable",
        reason_codes: ["insufficient_quarters"],
      },
      spike_flag: { ...base, value: 0, status: "measured" },
      hhi: {
        ...base,
        value: null,
        status: "not_comparable",
        reason_codes: ["affiliations_missing"],
      },
      applied_events: { ...base, value: null, status: "not_observed" },
      novelty_corpus: { ...base, value: 1, unit: "квартал", status: "measured" },
    },
    limitations: ["Покрытие — нижняя оценка: веб-канал собран частично."],
    sensitivity: [
      {
        check: "largest_participant",
        sign_changed: false,
        stage_changed: false,
        position_changed: true,
      },
      { check: "largest_peak", sign_changed: null, stage_changed: null, position_changed: null },
    ],
  };
}

const coverage = (status: string, extra: Record<string, unknown> = {}) => ({
  status,
  processed_sections: status === "not_extracted" ? 0 : 6,
  total_sections: 6,
  ...extra,
});

function documentTechnologies(id: string) {
  const base = {
    source_revision_id: id,
    extractor_version: "extract-v1",
    schema_version: "technology-1",
  };
  if (id === "doc-1")
    return {
      ...base,
      coverage: coverage("complete", { extracted_at: "2026-09-10T08:00:00Z" }),
      technologies: technology.mentions
        .slice(0, 2)
        .map((mention) => Object.assign({}, mention, { source_revision_id: id })),
    };
  if (id === "doc-2")
    return {
      ...base,
      coverage: coverage("no_technology", { extracted_at: "2026-09-10T08:00:00Z" }),
      technologies: [],
    };
  if (id === "doc-3")
    return {
      ...base,
      coverage: coverage("failed", { reason_code: "model_timeout" }),
      technologies: [],
    };
  if (id === "doc-4")
    return {
      ...base,
      coverage: coverage("unsupported_language", { reason_code: "de" }),
      technologies: [],
    };
  return { ...base, coverage: coverage("not_extracted"), technologies: [] };
}

/** Изолированный API с состоянием: любой нереализованный запрос роняет тест. */
export function createTestApi() {
  const state = createData();
  const calls: { method: string; path: string; body: Record<string, unknown> }[] = [];
  const unhandled: string[] = [];
  const failures = new Map<string, number>();
  const attachments = new Map<string, string[]>([["kpi-1", ["doc-1"]]]);
  const histories = new Map(
    state.chats.map((chat) => [
      chat.id,
      state.messages.map((message) => ({ ...message, chat_id: chat.id })),
    ]),
  );
  const reviews: Record<string, unknown>[] = [
    {
      id: "review-1",
      technology_id: "tech-1",
      author_id: "user-operator",
      operation: "describe",
      reason: "Уточнён диапазон температур по источнику.",
      previous_value: { description: "Электрохимическая ячейка для получения водорода." },
      new_value: { description: technology.description },
      created_at: "2026-09-12T09:30:00Z",
    },
  ];
  const auth = {
    access_token: "ui-review-token",
    token_type: "Bearer",
    expires_at: "2099-01-01T00:00:00Z",
    user: state.users[0]!,
  };

  function dispatch(method: string, rawUrl: string, body: Record<string, unknown> = {}): Reply {
    const url = new URL(rawUrl, "http://127.0.0.1");
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const key = `${method} ${path}`;
    calls.push({ method, path, body });
    const failure = failures.get(key);
    if (failure) {
      failures.delete(key);
      return { status: failure, json: { error: "Тестовая ошибка: повторите попытку" } };
    }
    if (key === "GET /trends/study") return ok(trendStudy);
    if (key === "GET /trends/live") return ok(trendLive);
    if (key === "GET /trends/live/topics/mcp") return ok(trendLiveTopic);
    if (key === "GET /trends/live/model") return ok(trendLiveModel);
    if (method === "GET" && /^\/trends\/live\/papers\/[^/]+\/cover$/.test(path)) return missing();
    if (key === "GET /trends/live/searches") return ok({ entries: state.searches });
    if (key === "GET /trends/live/search")
      return ok({
        query: url.searchParams.get("q") ?? "",
        status: "ready",
        elapsed_seconds: 0,
        detail: trendLiveTopic,
      });
    if (key === "GET /trends/live/search/top")
      return ok({
        query: url.searchParams.get("q") ?? "",
        found: true,
        observed_at: date,
        candidates: [
          {
            id: "candidate-memory",
            name: "смысловая память агентов",
            stage: "emerging_signal",
            model_score: { label: "weak_signal", value: 0.82, threshold: 0.5 },
            tz: { stage_code: 3, stage_basis: "", trend_code: 2, trend_basis: "", score: 5 },
          },
        ],
      });
    if (key.startsWith("GET /trends/live/topics/")) return missing();
    if (key === "GET /trends/live/watches") return ok({ watches: state.watches });
    if (key === "POST /trends/live/watches") {
      const watch = {
        kind: body.kind === "query" ? ("query" as const) : ("topic" as const),
        target: String(body.target),
        title: String(body.title),
        created_at: date,
      };
      state.watches.push(watch);
      return ok(watch);
    }
    const subject = (watch: { kind: string; target: string }) =>
      watch.kind === (body.kind ?? url.searchParams.get("kind")) &&
      watch.target === (body.target ?? url.searchParams.get("target"));
    if (key === "DELETE /trends/live/watches") {
      state.watches = state.watches.filter((watch) => !subject(watch));
      return { status: 204 };
    }
    if (key === "POST /trends/live/watches/ack") {
      for (const watch of state.watches.filter(subject)) delete watch.digest;
      return { status: 204 };
    }
    // Описание темы по умолчанию не готово: страницы обязаны жить без него.
    if (/^(?:GET|POST) \/trends\/analyses\/[^/]+\/candidates\/[^/]+\/description$/.test(key))
      return missing();
    const segments = path.split("/").filter(Boolean);
    const id = segments[1];
    const offset = Number(url.searchParams.get("offset") ?? 0);
    const limit = Number(url.searchParams.get("limit") ?? 20);
    const query = url.searchParams.get("q")?.toLowerCase() ?? "";

    if (key === "GET /technologies/tech-1") return ok(technology);
    if (key === "GET /technologies/tech-1/reviews")
      return ok({ technology_id: "tech-1", reviews: reviews.toReversed() });
    if (key === "POST /technologies/tech-1/reviews") {
      if (!body.reason || !body.operation)
        return { status: 400, json: { error: "invalid review" } };
      const review = {
        id: `review-${reviews.length + 1}`,
        technology_id: "tech-1",
        author_id: auth.user.id,
        ...body,
        created_at: "2026-09-13T12:00:00Z",
      };
      reviews.push(review);
      return { status: 201, json: review };
    }
    if (segments[0] === "technologies" && segments.length >= 2)
      return { status: 403, json: { error: "technology not available to this account" } };
    if (key === "GET /technologies")
      return ok({
        technologies: [
          {
            technology_id: "tech-2",
            canonical_name: "Щелочной электролизёр",
            description: "",
            validation_status: "proposed",
            match_reasons: ["name"],
          },
          {
            technology_id: "tech-1",
            canonical_name: "Твердооксидный электролизёр",
            description: "",
            validation_status: "proposed",
            match_reasons: ["name"],
          },
        ],
        catalogue_version: "audit",
        schema_version: "technology-catalogue-1",
      });
    if (
      segments[0] === "trends" &&
      segments[1] === "analyses" &&
      segments[3] === "topics" &&
      segments[5] === "metrics"
    )
      return ok(topicMetrics(segments[2]!, segments[4]!));
    if (segments[0] === "documents" && segments[2] === "technologies" && method === "GET")
      return ok(documentTechnologies(id!));

    if (key === "POST /auth/login")
      return body.password === "review-password"
        ? ok(auth)
        : { status: 401, json: { error: "Неверный пароль" } };
    if (key === "POST /auth/register") {
      if (state.users.some((user) => user.username === body.username))
        return { status: 409, json: { error: "User already exists" } };
      const user = {
        id: `user-${state.users.length + 1}`,
        username: String(body.username),
        roles: body.roles as string[],
        created_at: date,
      };
      state.users.push(user);
      return ok({ ...auth, user });
    }
    if (key === "GET /system/activity")
      return ok({
        corpus_epoch: 1,
        last_epochs: { clusters: 1, discovery: 1, raptor: 1 },
        workers: ["clusters", "discovery", "raptor", "itc"].map((name) => ({
          name,
          state: "idle",
          epoch: 1,
          updated_at: date,
          last_run_seconds: 12,
          progress_done: 4,
          progress_total: 4,
        })),
      });
    if (key === "GET /admin/users") return ok(state.users);
    if (key === "GET /admin/documents") {
      const items = state.documents.filter(
        (doc) =>
          (!query || `${doc.filename} ${doc.title}`.toLowerCase().includes(query)) &&
          (!url.searchParams.has("status") ||
            (url.searchParams.get("status") === "processing"
              ? !["indexed", "failed"].includes(doc.status)
              : doc.status === url.searchParams.get("status"))) &&
          (!url.searchParams.has("type") ||
            doc.filename.endsWith(`.${url.searchParams.get("type")}`)),
      );
      return ok({ items: items.slice(offset, offset + limit), total: items.length, offset, limit });
    }
    if (key === "GET /admin/documents/stats")
      return ok({
        total: state.documents.length,
        by_status: { indexed: state.documents.length - 1, failed: 1 },
        by_file_type: { pdf: state.documents.length - 1, txt: 1 },
        total_size_bytes: 1703936,
        total_chunks: 300,
        uploads_by_day: [{ day: "2026-09-08", count: state.documents.length }],
        top_by_chunks: state.documents.slice(0, 5),
        indexed_pct: 96,
      });
    if (key === "GET /admin/perf")
      return ok({
        pipeline: { embed: { p50: 0.1, p95: 0.2 }, generate: { p50: 1.2, p95: 2.4 } },
        ingestion: { "parse.pdf": { p50: 2, p95: 4 } },
        hypothesis_jobs: [{ kind: "generate", count: 5, p50_sec: 12, max_sec: 20 }],
      });
    if (key === "GET /admin/llm-usage")
      return ok({ ...state.usage, range_days: Number(url.searchParams.get("days") ?? 7) });
    if (key === "GET /admin/openalex")
      return ok({
        budgets: [
          {
            credential: "key",
            known: true,
            limit: 10000,
            remaining: 6200,
            spent: 3800,
            reserve: 3000,
            reset_at: "2026-09-26T00:00:00Z",
            seen_at: date,
          },
          { credential: "address", known: false },
        ],
      });
    if (path === "/admin/settings" && (method === "GET" || method === "PUT")) {
      if (method === "PUT")
        for (const [setting, value] of Object.entries(
          (body.values ?? {}) as Record<string, string | null>,
        )) {
          const item = state.settings.find((entry) => entry.key === setting);
          if (item)
            Object.assign(item, {
              override: value ?? "",
              hasOverride: !!value,
              source: value ? "db" : "default",
            });
        }
      return ok({ settings: state.settings, revision: 1 });
    }
    if (path === "/hypotheses/runtime-settings" && (method === "GET" || method === "PUT")) {
      if (method === "PUT") Object.assign(state.runtime, body);
      return ok(state.runtime);
    }
    if (path === "/hypotheses/scoring-weights" && (method === "GET" || method === "PUT")) {
      if (method === "PUT") Object.assign(state.weights, body);
      return ok(state.weights);
    }
    if (key === "GET /hypothesis-jobs") return ok(state.jobs);
    if (key === "POST /hypothesis-jobs") {
      const resultIds =
        body.kind === "generate"
          ? Array.from({ length: Number(body.count ?? 3) }, () => {
              const hypothesis = {
                ...structuredClone(state.hypotheses[0]!),
                id: `generated-${state.hypotheses.length + 1}`,
                kpi_id: String(body.kpi_id ?? "kpi-1"),
              };
              state.hypotheses.push(hypothesis);
              return hypothesis.id;
            })
          : [String(body.hypothesis_id ?? "hypothesis-1")];
      const job = {
        id: `job-${state.jobs.length + 1}`,
        owner_id: "user-admin",
        kind: body.kind as "generate",
        status: "succeeded" as const,
        input: body,
        result_ids: resultIds,
        created_at: date,
        started_at: date,
        finished_at: date,
      };
      state.jobs.push(job);
      return ok(job);
    }
    if (segments[0] === "hypothesis-jobs" && id && method === "GET")
      return ok(state.jobs.find((job) => job.id === id));
    if (key === "GET /hypotheses/graph")
      return ok({
        nodes: [
          ...state.hypotheses.map((hypothesis) => ({
            id: hypothesis.id,
            kind: "hypothesis",
            label: hypothesis.title,
            status: hypothesis.status,
            trl: hypothesis.trl,
            verdict: "supported",
            kpi_id: hypothesis.kpi_id,
          })),
          ...state.documents.slice(0, 3).map((document) => ({
            id: document.id,
            kind: "document",
            label: document.filename,
            degree: 5,
          })),
        ],
        edges: state.hypotheses.flatMap((hypothesis) =>
          hypothesis.evidence.map((evidence) => ({
            source: hypothesis.id,
            target: evidence.document_id,
            class: evidence.stance,
            score: evidence.score,
            chunk_id: evidence.chunk_id,
          })),
        ),
      });
    if (method === "GET" && (path === "/hypotheses" || path === "/hypotheses/board")) {
      let items = state.hypotheses.filter(
        (hypothesis) =>
          (!query || `${hypothesis.title} ${hypothesis.statement}`.toLowerCase().includes(query)) &&
          ["status", "kpi_id", "function_area", "source_type", "organization"].every(
            (field) =>
              !url.searchParams.has(field) ||
              hypothesis[field as keyof ApiHypothesis] === url.searchParams.get(field),
          ) &&
          (!url.searchParams.has("cluster_id") ||
            hypothesis.primary_cluster_id === url.searchParams.get("cluster_id")),
      );
      const queue = url.searchParams.get("queue");
      if (queue === "ready") items = items.filter((hypothesis) => hypothesis.status === "approved");
      if (queue === "risk") items = items.filter((hypothesis) => hypothesis.status === "rejected");
      if (queue === "new") items = items.filter((hypothesis) => hypothesis.status === "generated");
      if (path === "/hypotheses") return ok(items);
      return ok({
        items: items.slice(offset, offset + limit),
        total: items.length,
        offset,
        limit,
        queue_counts: { all: state.hypotheses.length, ready: 1, risk: 1, new: 1, review: 1 },
        facets: {
          function_area: ["Материаловедение"],
          source_type: ["article"],
          organization: ["Тестовая лаборатория"],
        },
      });
    }
    if (key === "POST /hypotheses") {
      const hypothesis = {
        ...structuredClone(state.hypotheses[0]!),
        ...body,
        id: `hypothesis-${state.hypotheses.length + 1}`,
      } as ApiHypothesis;
      state.hypotheses.push(hypothesis);
      return ok(hypothesis);
    }
    if (segments[0] === "hypotheses" && id) {
      const hypothesis = state.hypotheses.find((item) => item.id === id);
      if (!hypothesis) return missing();
      if (segments.length === 2 && (method === "GET" || method === "PUT")) {
        if (method === "PUT") Object.assign(hypothesis, body);
        return ok(hypothesis);
      }
      if (segments[2] === "revisions" && (method === "GET" || method === "POST")) {
        if (method === "POST")
          state.revisions.push({
            id: `revision-${state.revisions.length + 1}`,
            hypothesis_id: id,
            revision_no: state.revisions.length + 1,
            editor_id: "user-admin",
            action: String(body.action),
            summary: String(body.summary ?? ""),
            patch: {},
            created_at: date,
          });
        return ok(method === "GET" ? state.revisions : state.revisions.at(-1));
      }
      if (segments[2] === "experiment" && method === "POST") return ok(hypothesis);
    }
    if (key === "GET /clusters") return ok(state.clusters);
    if (segments[0] === "clusters" && id && method === "GET")
      return state.clusters.some((cluster) => cluster.id === id)
        ? ok(state.clusters.find((cluster) => cluster.id === id))
        : missing();
    if (key === "GET /kpis") return ok(state.kpis);
    if (key === "POST /kpis/suggest")
      return ok({
        suggestions: [
          {
            title: "Повысить устойчивость покрытия",
            metric: "Прочность",
            unit: "%",
            direction: "increase",
            rationale: "Цель следует из демонстрационных источников.",
          },
        ],
      });
    if (key === "POST /kpis/parse")
      return ok({
        kpi: {
          title: String(body.prompt),
          description: "Проверка на контрольных образцах",
          metric: "Сохранение прочности",
          unit: "%",
          direction: "increase",
          target: 90,
        },
      });
    if (key === "POST /kpis") {
      const kpi = {
        ...structuredClone(state.kpis[0]!),
        ...body,
        id: `kpi-${state.kpis.length + 1}`,
        status: "active",
      };
      state.kpis.push(kpi);
      return ok(kpi);
    }
    if (segments[0] === "kpis" && id) {
      if (segments.length === 2 && (method === "GET" || method === "PUT")) {
        const kpi = state.kpis.find((item) => item.id === id);
        if (!kpi) return missing();
        if (method === "PUT") Object.assign(kpi, body);
        return ok(kpi);
      }
      if (segments.length === 2 && method === "DELETE") {
        state.kpis = state.kpis.filter((item) => item.id !== id);
        return { status: 204 };
      }
      if (segments[2] === "documents") {
        if (method === "POST")
          attachments.set(id, [...(attachments.get(id) ?? []), ...(body.document_ids as string[])]);
        if (method === "DELETE")
          attachments.set(
            id,
            (attachments.get(id) ?? []).filter((document) => document !== segments[3]),
          );
        return ok({
          documents: state.documents
            .filter((document) => attachments.get(id)?.includes(document.id))
            .map((document) => Object.assign({}, document, { role: "input", attached_at: date })),
        });
      }
      if (segments[2] === "graph-hypotheses" && method === "POST")
        return ok({
          candidates: [
            {
              title: "Новый режим обработки",
              statement: "Сопоставить температуру обработки и сохранение прочности.",
              process: "Термообработка",
              property: "Прочность",
              kpi: "Термостойкость",
              score: 0.8,
              documents: [{ id: "doc-1", filename: state.documents[0]!.filename }],
            },
          ],
        });
    }
    if (key === "GET /chats")
      return ok({ items: state.chats.slice(offset, offset + limit), total: state.chats.length });
    if (key === "POST /chats") {
      const chat = {
        ...state.chats[0]!,
        id: `chat-${state.chats.length + 1}`,
        title: String(body.title),
      };
      state.chats.unshift(chat);
      histories.set(chat.id, []);
      return ok(chat);
    }
    if (segments[0] === "chats" && segments[2] === "messages") {
      if (method === "GET") return ok(histories.get(id!) ?? []);
      if (method === "POST") {
        const history = histories.get(id!) ?? [];
        const answer = { ...state.messages[1]!, id: `message-${history.length + 2}`, chat_id: id! };
        history.push(
          {
            ...state.messages[0]!,
            id: `message-${history.length + 1}`,
            chat_id: id!,
            content: String(body.content),
          },
          answer,
        );
        histories.set(id!, history);
        return ok(answer);
      }
    }
    if (key === "POST /documents") {
      const document = {
        ...state.documents[0]!,
        id: `doc-${state.documents.length + 1}`,
        filename: String(body.filename ?? "review-upload.pdf"),
        title: "Загруженный тестовый документ",
      };
      state.documents.unshift(document);
      return ok(document);
    }
    if (segments[0] === "documents" && id && method === "GET") {
      const document = state.documents.find((item) => item.id === id);
      if (!document) return missing();
      if (segments[2] === "content")
        return document.mime_type === "application/pdf"
          ? { status: 200, pdf: true }
          : { status: 200, text: "Тестовый документ: прочность после обработки сохраняется." };
      if (segments[2] === "chunks")
        return ok({
          document,
          chunks: [
            {
              id: "chunk-1",
              index: 0,
              text: "Сохранение прочности после термообработки составило 90 %.",
            },
          ],
        });
      if (segments.length === 2) return ok(document);
    }
    unhandled.push(key);
    return { status: 501, json: { error: `UI fixture has no handler for ${key}` } };
  }
  return { state, calls, unhandled, failures, auth, dispatch };
}
