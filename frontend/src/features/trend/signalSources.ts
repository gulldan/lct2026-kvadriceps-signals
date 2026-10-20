import type { TFunction } from "i18next";
import type { LiveDetail, LivePaperPassport, LiveTopic } from "./liveApi";
import { upperFirst } from "@/shared/lib/format";

/** Рыночные записи лежат в evidence_ids: кейс на новости или патенте по одним paper_ids не
 *  найти. */
export function signalSources(detail: LiveDetail, topic: LiveTopic) {
  const ids = new Set([
    ...topic.paper_ids,
    ...topic.evidence_ids,
    ...(topic.card?.evidence ?? []).map((item) => item.source_id),
    ...(topic.market?.evidence ?? []).map((item) => item.source_id),
    ...(topic.adoption?.evidence ?? []).map((item) => item.source_id),
    ...(topic.qualification?.evidence ?? []).map((item) => item.source_id),
  ]);
  // Одна работа бывает в статьях направления и технологии, а номер источника означает одну работу.
  const seen = new Set<string>();
  return detail.papers
    .filter((paper) => ids.has(paper.id) && !seen.has(paper.id) && !!seen.add(paper.id))
    .toSorted((a, b) => b.date.localeCompare(a.date))
    .map((paper, index) => ({
      n: index + 1,
      paper,
      passport: readablePassport(detail.passports?.[paper.id]),
    }));
}

/** Новые источники — с датой не раньше полугода до наблюдения ответа; дата одним годом новизну не
 *  доказывает и в счёт не идёт. */
export function freshSourceCount(sources: { paper: { date: string } }[], observedAt: string) {
  const since = new Date(observedAt);
  if (Number.isNaN(since.getTime())) since.setTime(Date.now());
  since.setUTCMonth(since.getUTCMonth() - 6);
  const floor = since.toISOString().slice(0, 10);
  return sources.filter(({ paper }) => paper.date.length >= 7 && paper.date >= floor).length;
}

// Английская присказка модели («Based on the title and abstract, …») есть и в сохранённых ответах;
// английский термин перед запятой («Mixture of Experts, …») — часть пересказа.
const PREAMBLE =
  /^(?:based on|here is|here's|i understand|according to|in this|below is|sure|okay)\b[^а-яё]*?[,:.]\s+(?=[а-яё])/i;
const SUBJECT = /^(?:this|the)\s+(?:material|paper|article|work|study)\s+(?=[а-яё])/i;

function readableDigest(digest: string) {
  const text = digest.replace(PREAMBLE, "").replace(SUBJECT, "Материал ");
  return upperFirst(text);
}

function readablePassport(passport: LivePaperPassport | undefined) {
  return passport?.digest ? { ...passport, digest: readableDigest(passport.digest) } : passport;
}

export function sourceGenreLabel(genre: string, t: TFunction<"trend">) {
  return t(`source.genre.${genre}`, { defaultValue: t("source.genre.other") });
}

/** Паспорт источника по ТЗ одной строкой для экрана, печати и Word. Неизвестные тип, язык и доверие
 *  подписаны словами: пропуск читался бы как «проверено», а не как «не определено». */
export function passportLine(
  date: string,
  passport: LivePaperPassport | undefined,
  t: TFunction<"trend">,
) {
  const language = passport?.language || "unknown";
  const trust = passport?.trust || "unknown";
  return [
    date || t("source.unknownDate"),
    passport?.source_name,
    t(`source.publisher.${passport?.publisher_type || "unknown"}`, { defaultValue: "" }),
    passport?.genre && passport.genre !== "unknown" && sourceGenreLabel(passport.genre, t),
    t("source.language", {
      language: t(`source.languages.${language}`, { defaultValue: language.toUpperCase() }),
    }),
    [t(`source.trust.${trust}`, { defaultValue: "" }), passport?.trust_basis]
      .filter(Boolean)
      .join(" — "),
    trust === "lowered" && t("source.trustWarning"),
  ]
    .filter(Boolean)
    .join(" · ");
}

// Проверка содержания повторяла служебное из подсказки модели: «соответствует scope», «задаче b1»,
// «(c1)», двойное «Механизм:». Сохранённые ответы бессрочны, поэтому текст правится при чтении.
const ID = String.raw`[bc]\d+(?:\s*(?:,|и)\s*[bc]\d+)*`;
const NOUN = String.raw`(?:[Вв]етк|[Вв]етв|[Уу]слови|[Зз]адач|[Нн]аправлени|[Кк]ритери)[а-яё]*`;
const REASON_FIXES: [RegExp, string][] = [
  [/(?<!\.)\.\.(?!\.)/g, "."],
  [/Механизм:\s*Механизм:/g, "Механизм:"],
  [/\s*\([bc]\d+\)/g, ""],
  [/\b[bc]\d+\s*\(([^()]+)\)/g, "«$1»"],
  [new RegExp(String.raw`(${NOUN})\s+${ID}`, "gu"), "$1 запроса"],
  [/\bscope(?:\s+запроса)?/giu, "области запроса"],
  [new RegExp(String.raw`(^|\s)с\s+${ID}`, "gu"), "$1с запросом"],
  [
    new RegExp(String.raw`(^|\s)(отвечает|соответствует|соответствие|к)\s+${ID}`, "gu"),
    "$1$2 запросу",
  ],
  [new RegExp(String.raw`(^|\s)(для|без|из|от|до)\s+${ID}`, "gu"), "$1$2 части запроса"],
  [
    new RegExp(String.raw`\b${ID}\s+не\s+подтвержд[её]н[а-яё]*`, "gu"),
    "часть запроса не подтверждена",
  ],
  [new RegExp(String.raw`\b${ID}\b`, "gu"), "часть запроса"],
];

export function readableReason(reason: string) {
  return REASON_FIXES.reduce((text, [pattern, value]) => text.replace(pattern, value), reason);
}

// Запрос хранится в нижнем регистре: без словаря заголовок выходит «Технологии в ии».
const ACRONYMS = new Map(
  [
    "LLM",
    "RL",
    "GUI",
    "MCP",
    "KV",
    "RAG",
    "VLA",
    "SSM",
    "AI",
    "ML",
    "NLP",
    "GPU",
    "API",
    "SQL",
    "IoT",
    "5G",
    "6G",
    "ИИ",
    "ИТ",
    "ИБ",
    "ЦОД",
    "БПЛА",
    "СУБД",
  ].map((word) => [word.toLowerCase(), word]),
);

export function spellAcronyms(text: string) {
  return text.replaceAll(/[\p{L}\p{N}]+/gu, (word) => ACRONYMS.get(word.toLowerCase()) ?? word);
}
