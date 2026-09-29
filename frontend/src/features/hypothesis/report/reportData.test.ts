import { expect, test } from "bun:test";
import { invalidateClusters } from "@/features/cluster";
import type { ApiHypothesis } from "../api";
import { createData } from "../../../../tests/product/data";
import { loadReportData } from "./reportData";

test("отчёт собирает портфель по страницам, исключает направления, черновики и закрытые, показывает топ-40", async () => {
  const fixture = createData();
  const base = fixture.hypotheses[0]!;
  const kpi = fixture.kpis[0]!;
  const rows: ApiHypothesis[] = Array.from({ length: 223 }, (_, index) => ({
    ...base,
    id: `hypothesis-${index}`,
    kpi_id: kpi.id,
    status: index === 219 ? "archived" : index >= 200 ? "approved" : "generated",
    composite_score: index / 222,
    assessment: {},
    detail: {},
    evidence: [],
    generation:
      index % 5 === 0
        ? { semantic_kind: "research_direction" }
        : index % 5 === 1
          ? { kind: "fallback_evidence" }
          : { kind: "kpi", inputs: { constraints: `constraint-${index}` } },
  }));
  const pages: { limit: number; offset: number }[] = [];
  const originalFetch = globalThis.fetch;
  invalidateClusters();
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "https://app.example");
    if (url.pathname === "/api/v1/hypotheses/board") {
      const requestedLimit = Number(url.searchParams.get("limit"));
      const limit = requestedLimit > 0 && requestedLimit <= 200 ? requestedLimit : 60;
      const offset = Number(url.searchParams.get("offset"));
      pages.push({ limit: requestedLimit, offset });
      return Response.json({
        items: rows.slice(offset, offset + limit),
        total: rows.length,
        limit,
        offset,
        queue_counts: {},
        facets: {},
      });
    }
    if (url.pathname === "/api/v1/kpis") return Response.json([kpi]);
    if (url.pathname === "/api/v1/clusters") return Response.json([]);
    const row = rows.find((item) => url.pathname === `/api/v1/hypotheses/${item.id}`);
    if (row) return Response.json(row);
    throw new Error(`Unexpected request: ${url.pathname}`);
  }) as typeof fetch;
  try {
    const report = await loadReportData();
    expect(pages).toEqual([
      { limit: 200, offset: 0 },
      { limit: 200, offset: 200 },
    ]);
    expect(report.ranked).toHaveLength(132);
    expect(report.closedTotal).toBe(1);
    expect(report.queues).toEqual({ ready: 12, risk: 0, needs_verify: 120, insufficient: 0 });
    expect(report.shown).toHaveLength(40);
    expect(report.shown[0]?.id).toBe("hypothesis-222");
    expect(report.kpiRows).toEqual([{ kpi, total: 132, ready: 12, best: 100 }]);
    expect(report.pilots.map((h) => h.id)).not.toContain("hypothesis-219");
    expect(report.pilots).toHaveLength(5);
    expect(report.constraints["hypothesis-222"]).toBe("constraint-222");
  } finally {
    globalThis.fetch = originalFetch;
    invalidateClusters();
  }
});
