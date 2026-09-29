import { originOf } from "../providerLabel";
import type { StudyEvent, StudyPaper } from "../studyApi";
import type { AnalysisJob, TrendCandidate } from "../trendApi";
import { StudyLadder } from "./StudyLadder";

const LANES = ["open_source", "vendor", "patent"] as const;

/** Правая граница — месяц после последней даты, чтобы последняя точка не выходила за шкалу. */
export function TrendLadder({ job, candidate }: { job: AnalysisJob; candidate: TrendCandidate }) {
  const result = job.result!;
  const documents = new Set(
    result.works
      .filter((work) => candidate.work_ids.includes(work.id))
      .flatMap((work) => work.document_ids),
  );
  const papers: StudyPaper[] = [];
  const external: StudyEvent[] = [];
  let observed = "";
  for (const item of job.sources) {
    if (!documents.has(item.document_id)) continue;
    const meta = item.source;
    if (meta.observed_at > observed) observed = meta.observed_at;
    const date =
      meta.publication?.value ??
      (meta.github?.released_at ?? meta.github?.created_at)?.slice(0, 10);
    if (!date) continue;
    const title = item.title || meta.canonical_id;
    if (meta.source_kind === "paper") {
      papers.push({
        id: item.document_id,
        title,
        date,
        url: meta.url,
        authors: [],
        document_id: item.document_id,
        snapshot_id: item.snapshot_id,
      });
      continue;
    }
    const repository = meta.source_kind === "software_repository";
    external.push({
      topic: candidate.id,
      kind: repository
        ? meta.github?.release_id
          ? "release"
          : "implementation"
        : meta.source_kind === "patent"
          ? "patent"
          : "vendor_material",
      community: repository ? "open_source" : meta.source_kind === "patent" ? "patent" : "vendor",
      date,
      title,
      org: originOf(meta.url, meta.provider),
      url: meta.url,
      stars: meta.github?.stars ?? undefined,
    });
  }
  if (!papers.length && !external.length) return null;
  const dates = [...papers.map((paper) => paper.date), ...external.map((event) => event.date)];
  const from = [candidate.measurements.series[0]?.start ?? "", ...dates]
    .filter(Boolean)
    .toSorted()[0];
  const last = [result.cutoff.slice(0, 10), ...dates].toSorted().at(-1)!;
  const [year, month] = last.slice(0, 7).split("-").map(Number) as [number, number];
  const to = month === 12 ? `${year + 1}-01` : `${year}-${String(month + 1).padStart(2, "0")}`;
  return (
    <StudyLadder
      study={{ papers, external_observed_at: observed }}
      topic={{ paper_ids: papers.map((paper) => paper.id), external }}
      lanes={LANES}
      corpusLane="papers"
      from={from}
      to={to}
      limit={5}
    />
  );
}
