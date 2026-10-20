import type { TFunction } from "i18next";
import type { CreateHypothesisInput, RadarBet } from "@/features/hypothesis";
import type { LiveDetail, LiveTopic } from "./liveApi";
import { signalSources, sourceGenreLabel } from "./signalSources";

export interface SignalBetForm {
  title: string;
  problem: string;
  hypothesis: string;
  grounds: string;
  method: string;
  criterion: string;
}

/** Прогноз уносит снимок индикаторов: по нему радар потом сверяет, сбылся ли он. */
export function signalHypothesisInput({
  detail,
  candidate,
  query,
  topic,
  form,
  bet,
  model,
  kpiId,
  t,
}: {
  detail: LiveDetail;
  candidate: LiveTopic;
  query: string;
  topic?: string;
  form: SignalBetForm;
  bet?: RadarBet | null;
  model?: string;
  kpiId?: string;
  t: TFunction<"trend">;
}): CreateHypothesisInput {
  const evidence = [
    ...(candidate.card?.evidence ?? []),
    ...(candidate.market?.evidence ?? []),
    ...(candidate.adoption?.evidence ?? []),
    ...(candidate.qualification?.evidence ?? []).map((item) => ({
      source_id: item.source_id,
      quote: item.quote,
      field: "qualification",
      verified: candidate.qualification?.verified ?? false,
    })),
  ];
  return {
    title: form.title.trim(),
    statement: form.hypothesis.trim(),
    rationale: form.grounds.trim() || candidate.card?.advantage || "",
    method: "manual",
    status: "draft",
    source_type: "Технологический радар",
    kpi_id: kpiId || undefined,
    measurable: false,
    generation: {
      kind: "radar",
      model: model ?? candidate.card?.model,
      prompt_version: detail.method,
    },
    detail: {
      problem_addressed: form.problem.trim() || undefined,
      verification: { method: form.method.trim(), success_criteria: form.criterion.trim() },
      radar: {
        version: "radar-transfer-v1",
        query,
        topic,
        tz: candidate.tz,
        events: candidate.events,
        bet,
        candidate_id: candidate.id,
        name: candidate.label || candidate.name,
        observed_at: detail.observed_at,
        method: detail.method,
        rules: candidate.confidence || undefined,
        model_score: candidate.model_score || undefined,
        market: candidate.market || undefined,
        wording_model: candidate.card?.model,
        verification_model: candidate.card?.verification_model,
        sources: signalSources(detail, candidate).map(({ paper, passport }) => ({
          id: paper.id,
          title: paper.title,
          url: paper.url,
          date: paper.date,
          language: passport?.language,
          language_basis: passport?.language_basis,
          publisher_type: passport?.publisher_type,
          genre: passport?.genre,
          source_name: passport?.source_name,
          discovery_source: passport?.discovery_source,
          trust: passport?.trust,
          trust_basis: passport?.trust_basis,
          digest: passport?.digest,
          digest_model: passport?.digest_model,
          passport: [
            passport?.source_name
              ? t("source.sourceName", { name: passport.source_name })
              : undefined,
            passport?.discovery_source
              ? t("source.discoveredVia", {
                  source: t(`source.discovery.${passport.discovery_source}`, {
                    defaultValue: passport.discovery_source,
                  }),
                })
              : undefined,
            t(`source.publisher.${passport?.publisher_type || "unknown"}`, {
              defaultValue: passport?.publisher_type || t("source.publisher.unknown"),
            }),
            passport?.genre && passport.genre !== "unknown"
              ? sourceGenreLabel(passport.genre, t)
              : undefined,
            t("source.language", {
              language: t(`source.languages.${passport?.language || "unknown"}`, {
                defaultValue: passport?.language || t("source.languages.unknown"),
              }),
            }),
            t(`source.trust.${passport?.trust || "unknown"}`, {
              defaultValue: passport?.trust || t("source.trust.unknown"),
            }),
          ]
            .filter(Boolean)
            .join(" · "),
          quotes: evidence
            .filter((item) => item.source_id === paper.id)
            .map(({ field, quote, verified }) => ({ field, quote, verified })),
        })),
      },
    },
  };
}
