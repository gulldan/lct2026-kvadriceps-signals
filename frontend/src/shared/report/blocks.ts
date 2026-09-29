export type ReportFact = { label: string; value: string };

export type ReportMetric = { value: string; unit?: string; label: string };

/** Единица набрана вдвое мельче и считается за ползнака. */
export function metricLength(m: ReportMetric): number {
  return m.value.length + (m.unit?.length ?? 0) / 2;
}

export type ReportBlock =
  | {
      kind: "doctitle";
      org: string | null;
      doctype: string;
      title: string;
      meta: string[];
      lines: string[];
      facts?: ReportFact[];
      lead?: string;
      about?: string;
      text?: string;
      metrics?: ReportMetric[];
    }
  | {
      kind: "card";
      label: string;
      title: string;
      facts: ReportFact[];
      lead?: string;
      about?: string;
      text?: string;
      metrics: ReportMetric[];
    }
  | { kind: "heading"; text: string }
  | { kind: "subheading"; text: string }
  | { kind: "paragraph"; text: string; muted?: boolean; italic?: boolean; bold?: boolean }
  | { kind: "kv"; rows: ReportFact[] }
  | { kind: "table"; title?: string; head: string[]; rows: string[][] }
  | { kind: "list"; items: string[]; ordered?: boolean }
  | { kind: "quote"; source: string; text: string; note?: string }
  | { kind: "references"; items: { n: number; text: string; url?: string; note?: string }[] };

export interface ReportDoc {
  fileName: string;
  title: string;
  blocks: ReportBlock[];
  dense?: boolean;
}
