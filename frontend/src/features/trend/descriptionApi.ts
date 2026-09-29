import { request } from "@/shared/api/client";
import type { AnalysisJob, TrendCandidate } from "./trendApi";

export type DescriptionFields = Pick<TrendCandidate["brief"], "problem" | "advantage" | "example">;

export interface DescriptionJob {
  job_id: string;
  candidate_id: string;
  version: string;
  bundle_sha256: string;
  result_sha256: string;
  evidence_sha256: string;
  status: "prepared" | "validated" | "template";
  attempt: number;
  result: {
    bundle_sha256: string;
    status: "validated" | "template";
    fields: DescriptionFields;
    label?: string;
    summary?: string;
    model: string;
    prompt_sha256: string;
    latency_ms: number;
    reason: string;
  } | null;
}

export async function readDescription(
  job: AnalysisJob,
  candidate: TrendCandidate,
  start: boolean,
  signal: AbortSignal,
) {
  const path = `/trends/analyses/${encodeURIComponent(job.id)}/candidates/${encodeURIComponent(candidate.id)}/description`;
  const description = await request<DescriptionJob>(path, {
    method: start ? "POST" : "GET",
    signal,
  });
  if (
    description.job_id !== job.id ||
    description.candidate_id !== candidate.id ||
    description.result_sha256 !== job.result_sha256 ||
    description.evidence_sha256 !== candidate.brief.evidence_sha256 ||
    description.version !== "source-attribution-v1" ||
    !["prepared", "validated", "template"].includes(description.status)
  )
    throw new Error("description does not match the saved evidence");
  if (description.status === "prepared") {
    if (description.result !== null) throw new Error("unvalidated description exposed");
    return description;
  }
  const result = description.result;
  if (
    !result ||
    result.status !== description.status ||
    result.bundle_sha256 !== description.bundle_sha256
  )
    throw new Error("description status mismatch");
  for (const field of ["problem", "advantage", "example"] as const) {
    const original = candidate.brief[field];
    const generated = result.fields[field];
    if (original === null) {
      if (generated !== null) throw new Error("unknown fact replaced by prose");
    } else if (
      !generated ||
      JSON.stringify(generated.evidence_ids) !== JSON.stringify(original.evidence_ids) ||
      (generated.text !== original.text &&
        (result.status !== "validated" ||
          generated.text !== `В источнике сказано: «${original.text}»`))
    )
      throw new Error("unsupported description");
  }
  return description;
}
