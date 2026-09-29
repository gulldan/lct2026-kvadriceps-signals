import { beforeAll, expect, test } from "bun:test";
import { initI18n, loadNamespace } from "@/shared/i18n";
import type { ApiHypothesis, ApiRevision } from "./api";
import { betPassportBlocks, radarSignalHref } from "./betPassport";
import type { RadarProvenance } from "./radarProvenance";

beforeAll(async () => {
  // initI18n выставляет язык документа, а DOM в bun test нет.
  const documentDescriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
  (globalThis as { document?: unknown }).document ??= { documentElement: { lang: "" } };
  try {
    await initI18n();
    await loadNamespace("hypothesis");
  } finally {
    if (documentDescriptor) Object.defineProperty(globalThis, "document", documentDescriptor);
    else Reflect.deleteProperty(globalThis, "document");
  }
});

const source = (id: string, title: string) => ({
  id,
  title,
  url: `https://arxiv.org/abs/${id}`,
  date: "2026-06-18",
  quotes: [{ field: "advantage", quote: `${title} cuts error by 60%.`, verified: true }],
});

test("паспорт читается сигнал → индикаторы → проверка → решение, довод проверки из радара ведёт к работе", () => {
  const radar: RadarProvenance = {
    version: "radar-transfer-v1",
    query: "голосовая идентификация",
    candidate_id: "ngram:keyword-spotting",
    name: "Keyword Spotting",
    observed_at: "2026-09-18T04:23:43Z",
    method: "live-signals-v8",
    sources: [source("a", "First work"), source("b", "Second work"), source("c", "Third work")],
    model_score: {
      value: 0.93,
      model: "logreg",
      version: "v3",
      label: "insufficient_data",
      data_gap: "too_early",
    },
  };
  const h = {
    id: "f48c6587-afa9",
    status: "approved",
    trl: 4,
    assessment: { check: { verdict: "supported", supporting: ["x"], contradicting: [] } },
    evidence: [
      { stance: "supports", filename: "corpus.pdf", snippet: "corpus quote", score: 0.9 },
      { stance: "supports", filename: "Third work", snippet: "Third work cuts error", score: 0.1 },
    ],
  } as unknown as ApiHypothesis;
  const revisions = [
    { revision_no: 1, action: "verified", editor_id: "system", summary: "", created_at: "" },
    {
      revision_no: 2,
      action: "approved",
      editor_id: "analyst",
      summary: "Пилот в Q1",
      created_at: "2026-09-25T10:00:00Z",
    },
  ] as ApiRevision[];

  const blocks = betPassportBlocks({ h, radar, revisions });
  expect(blocks.filter((b) => b.kind === "subheading").map((b) => b.text)).toEqual([
    "1. Сигнал",
    "2. Индикаторы",
    "3. Проверка",
    "4. Решение",
  ]);
  const scores = blocks.find((b) => b.kind === "kv")?.rows.at(-1)?.value;
  expect(scores).toMatch(/^сходство со слабыми сигналами — Мало данных: .+ · для справки: 93/);
  const quotes = blocks.filter((b) => b.kind === "quote").map((b) => b.source);
  // Работа радара идёт первой, хотя корпусный фрагмент оценён выше.
  expect(quotes.slice(-2)).toEqual(["за · работа радара [3]", "за · corpus.pdf"]);
  expect(blocks.findLast((b) => b.kind === "quote" && b.source.includes("[3]"))).toMatchObject({
    note: "https://arxiv.org/abs/c",
  });
  const decision = blocks.at(-1);
  expect(decision?.kind === "kv" && decision.rows.map((r) => r.value)).toEqual([
    expect.stringMatching(/^Пилот — analyst, /),
    "Пилот в Q1",
  ]);
  const href = new URL(radarSignalHref(radar), "https://stand");
  expect([href.pathname, href.searchParams.get("q"), href.searchParams.get("signal")]).toEqual([
    "/trends",
    radar.query,
    radar.candidate_id,
  ]);
});
