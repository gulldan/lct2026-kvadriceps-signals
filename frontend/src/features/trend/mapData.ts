import type { AnalysisJob, TrendCandidate } from "./trendApi";
import { exploreAnalysis } from "./exploration";

interface MapWork {
  id: string;
  kind: AnalysisJob["sources"][number]["source"]["source_kind"];
  title: string;
  year: number | null;
  source: AnalysisJob["sources"][number];
}

export interface MapTopic {
  candidate: TrendCandidate;
  works: MapWork[];
}

export function technologyMap(job: AnalysisJob) {
  const found = exploreAnalysis(job);
  const sources = new Map(job.sources.map((source) => [source.document_id, source]));
  const works = new Map(
    found.works.flatMap((work) => {
      const versions = work.document_ids.flatMap((id) => {
        const source = sources.get(id);
        return source ? [source] : [];
      });
      const dated = versions
        .filter((source) => source.source.publication?.value)
        .toSorted((a, b) => a.source.publication!.value.localeCompare(b.source.publication!.value));
      const source = dated[0] ?? versions[0];
      if (!source) return [];
      return [
        [
          work.id,
          {
            id: work.id,
            kind: work.kind,
            title: source.title || work.id,
            year: dated[0] ? Number(dated[0].source.publication!.value.slice(0, 4)) : null,
            source,
          },
        ] as const,
      ];
    }),
  );
  const topics: MapTopic[] = found.candidates.map((candidate) => ({
    candidate,
    works: candidate.work_ids.flatMap((id) => (works.has(id) ? [works.get(id)!] : [])),
  }));
  return {
    topics,
    works: [...works.values()],
    years: [
      ...new Set([
        ...found.years.map((item) => item.year),
        ...[...works.values()].flatMap((work) => (work.year === null ? [] : [work.year])),
      ]),
    ].toSorted((a, b) => a - b),
  };
}

export const visibleWorks = (topic: MapTopic, year: number | null) =>
  year === null
    ? topic.works
    : topic.works.filter((work) => work.year !== null && work.year <= year);

export function topicLabel(title: string) {
  const compact = title
    .replace(/retrieval[\s-]+augmented[\s-]+generation/gi, "RAG")
    .replace(/\bRAG\s*\(RAG\)/gi, "RAG");
  return compact.length > 54 ? compact.slice(0, 51).trimEnd() + "…" : compact;
}

export interface MapNode {
  id: string;
  topicID: string;
  workID?: string;
  x: number;
  y: number;
  radius: number;
}

/** Упаковка не зависит от фильтра публикаций: смена года не перестраивает карту. */
export function mapLayout(topics: MapTopic[]) {
  const sorted = topics.toSorted(
    (a, b) => b.works.length - a.works.length || a.candidate.id.localeCompare(b.candidate.id),
  );
  const groups = sorted.map((topic, index) => {
    const angle = index * Math.PI * (3 - Math.sqrt(5));
    const distance = 48 * Math.sqrt(index);
    return {
      topic,
      x: Math.cos(angle) * distance,
      y: Math.sin(angle) * distance,
      radius: 28 + Math.sqrt(topic.works.length) * 9,
    };
  });
  // ponytail: 47 группам хватает ограниченного прохода столкновений; за 300 — пространственный индекс.
  for (let step = 0; step < 70; step++) {
    for (let i = 0; i < groups.length; i++) {
      for (let j = i + 1; j < groups.length; j++) {
        const a = groups[i]!,
          b = groups[j]!;
        const dx = b.x - a.x,
          dy = b.y - a.y;
        const distance = Math.max(0.01, Math.hypot(dx, dy));
        const gap = a.radius + b.radius + 24;
        if (distance >= gap) continue;
        const push = ((gap - distance) / distance) * 0.5;
        a.x -= dx * push;
        a.y -= dy * push;
        b.x += dx * push;
        b.y += dy * push;
      }
    }
  }
  const nodes: MapNode[] = [];
  const positionedWorks = new Set<string>();
  for (const { topic, x, y, radius } of groups) {
    nodes.push({
      id: "topic:" + topic.candidate.id,
      topicID: topic.candidate.id,
      x,
      y,
      radius: 9 + Math.sqrt(topic.works.length) * 1.5,
    });
    const ordered = topic.works.toSorted(
      (a, b) => (a.year ?? Infinity) - (b.year ?? Infinity) || a.id.localeCompare(b.id),
    );
    ordered.forEach((work, index) => {
      if (positionedWorks.has(work.id)) return;
      positionedWorks.add(work.id);
      const angle = index * Math.PI * (3 - Math.sqrt(5));
      const distance = 30 + Math.sqrt((index + 1) / Math.max(1, ordered.length)) * (radius - 30);
      nodes.push({
        id: "work:" + work.id,
        topicID: topic.candidate.id,
        workID: work.id,
        x: x + Math.cos(angle) * distance,
        y: y + Math.sin(angle) * distance,
        radius: 3.5,
      });
    });
  }
  return nodes;
}
