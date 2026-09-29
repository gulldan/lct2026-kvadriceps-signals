import { describe, expect, it } from "bun:test";
import { liveLimitFailure, liveLimitKind } from "./liveLimits";

describe("liveLimitKind — запасной разбор оговорок без кода", () => {
  it("отличает правила отбора от отказа источника", () => {
    expect(
      liveLimitKind(
        "discovery: 11 candidate directions with fewer than 30 papers on the full window were left out",
      ),
    ).toBe("discovery_below_floor");
    expect(
      liveLimitKind(
        "discovery: candidates found on a stratified deterministic sample of 12288 of 372569 eligible papers; each direction is recounted on the full window",
      ),
    ).toBe("discovery_sample");
    expect(
      liveLimitKind("affiliations: 14153 unchecked papers remain after the 64-record budget"),
    ).toBe("affiliations_budget");
  });

  it("не читает пополнение корпуса как отказ arXiv", () => {
    expect(liveLimitKind("scholar: взяты 1200 работ из 33046 найденных")).toBe("scholar_capped");
    expect(liveLimitKind("scholar: добавлено 1161 работ из журналов вне arXiv")).toBe(
      "scholar_journals",
    );
  });

  it("не называет отказом то, что о нём не говорит", () => {
    expect(liveLimitKind("something nobody wrote a rule for")).toBe("other");
    expect(liveLimitKind("news feed collector: request failed")).toBe("web_sample");
    expect(liveLimitKind("collector refused the connection")).toBe("source_unavailable");
  });
});

describe("liveLimitFailure — тревожный тон только настоящему отказу", () => {
  it("границы метода не краснеют", () => {
    for (const code of [
      "scholar_capped",
      "scholar_journals",
      "classifier_not_connected",
      "qualification_unavailable",
      "affiliations_budget",
      "нового кода никто ещё не видел",
    ])
      expect(liveLimitFailure(code)).toBe(false);
  });

  it("несобранный канал остаётся отказом", () => {
    expect(liveLimitFailure("arxiv_unavailable")).toBe(true);
    expect(liveLimitFailure("previous_snapshot")).toBe(true);
  });
});
