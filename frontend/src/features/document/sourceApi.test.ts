import { expect, test } from "bun:test";
import { locateSourceQuote, sourcePath, sourceReturnPath, type SourceEvidence } from "./sourceApi";

test("source links preserve their preview context without accepting external paths", () => {
  const catalogue = "/trends?entity=technologies&q=водород&language=ru";
  const technology = `/technologies/technology-1?${new URLSearchParams({ return: catalogue })}`;
  for (const returnTo of [
    technology,
    "/technologies/technology-1#sources",
    "/trends",
    "/trends?run=analysis-1&view=timeline#topic-topic-1",
    "/trends/analysis-1/topics/topic-1?return=%2Ftrends%3Frun%3Danalysis-1#sources",
    "/trends/analysis-1/method?candidate=topic-1#method-link",
    "/operator/documents?q=retrieval&type=pdf&status=indexed&page=2",
    "/operator/documents?type=pdf&q=retrieval&page=2",
    "/search?chat=chat-1",
    "/hypotheses/hypothesis-1#evidence",
    "/directions/direction-1",
    "/clusters/cluster-1",
    "/hypotheses?view=graph&kpi=kpi-1",
    "/kpi",
  ]) {
    const link = sourcePath("source-1", "revision-1", "fragment-1", { returnTo });
    const params = new URL(link, "https://app.example").searchParams;
    expect(sourceReturnPath(params.get("return") ?? "", "analysis-1")).toBe(returnTo);
  }
  for (const returnTo of [
    "",
    "https://external.example/trends",
    "//external.example/trends",
    "/\\external.example/trends",
    "javascript:alert(1)",
    "/technologies/../admin",
    "/admin/settings",
    "/operator/documents/../admin",
    "/operator/documents.evil",
    "/operator/documents\\external.example",
    "/search.evil?chat=chat-1",
    "/search/../admin?chat=chat-1",
    "/hypotheses/../admin",
    "/hypotheses/hypothesis-1/extra",
    "/directions/%2e%2e%2fadmin",
    "/clusters/cluster-1\\external.example",
    "/kpi/../admin",
  ]) {
    expect(sourceReturnPath(returnTo)).toBe("/operator/documents");
    expect(sourceReturnPath(returnTo, "analysis-1&view=study")).toBe(
      "/trends?run=analysis-1%26view%3Dstudy",
    );
  }
});

test("source anchors count Unicode points across overlapping chunks and preserve negation", () => {
  const source = {
    text_hash: "saved",
    chunks: [
      { id: "first", char_start: 0, char_end: 7, text: "😀 10% —" },
      { id: "second", char_start: 6, char_end: 18, text: "— не прирост" },
    ],
  } as SourceEvidence;
  expect(locateSourceQuote(source, { start: 2, end: 18, textHash: "saved" })).toEqual({
    fragment: "first",
    quote: "10% — не прирост",
  });
  expect(locateSourceQuote(source, { start: 2, end: 18, textHash: "changed" })).toBeNull();
  expect(locateSourceQuote(source, { start: 2, end: 19, textHash: "saved" })).toBeNull();
  source.chunks[1]!.char_start = 8;
  expect(locateSourceQuote(source, { start: 2, end: 18, textHash: "saved" })).toBeNull();
});
