import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, FileText } from "lucide-react";
import { sourcePath } from "@/features/document";
import { Button } from "@/shared/ui/Button";
import { BarChart } from "@/shared/ui/BarChart";
import { Badge } from "@/shared/ui/Badge";
import { StatStrip } from "@/shared/ui/Fields";
import { trendPath, type AnalysisJob } from "../trendApi";
import { visibleWorks, type MapTopic } from "../mapData";

export function TrendMapInspector({
  job,
  topic,
  workID,
  year,
  years,
  onSelect,
}: {
  job: AnalysisJob;
  topic: MapTopic;
  workID: string;
  year: number | null;
  years: number[];
  onSelect: (topic: string, work?: string) => void;
}) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const works = visibleWorks(topic, year);
  const work = works.find((item) => item.id === workID);
  const candidate = topic.candidate;
  const papers = works.filter((item) => item.kind === "paper");
  const unknown = works.filter((item) => item.year === null).length;
  const sources = works.toSorted(
    (a, b) => (b.year ?? -1) - (a.year ?? -1) || a.id.localeCompare(b.id),
  );
  const sourceLink = (item: (typeof works)[number]) =>
    sourcePath(item.source.document_id, item.source.snapshot_id, undefined, {
      analysis: job.id,
      returnTo: location.pathname + location.search + `#topic-${candidate.id}`,
    });
  return (
    <aside
      className="min-w-0 space-y-4 border-t p-4 lg:border-t-0 lg:border-l"
      aria-label={t("map.inspector")}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="secondary">{work ? t("map.source") : t("map.selectedTopic")}</Badge>
        {work && (
          <span className="text-xs text-muted-foreground">
            {work.source.source.provider} ·{" "}
            {work.source.source.publication?.value ?? t("map.undated")}
          </span>
        )}
      </div>
      <h3 className="text-base leading-snug font-semibold wrap-anywhere">
        {work?.title ?? candidate.title}
      </h3>
      {work ? (
        <>
          <p className="text-sm text-muted-foreground">{t("map.belongsTo")}</p>
          <Button variant="link" size="inline" onClick={() => onSelect(candidate.id)}>
            {candidate.title}
          </Button>
          <Button asChild variant="outline" className="w-full">
            <Link id={`map-source-${work.source.document_id}`} to={sourceLink(work)}>
              <FileText aria-hidden />
              {t("map.openSource")}
            </Link>
          </Button>
          <p className="text-xs leading-relaxed text-muted-foreground">{t("map.sourceHint")}</p>
        </>
      ) : (
        <>
          <StatStrip
            columns={2}
            items={[
              { label: t("map.papers"), value: String(papers.length) },
              { label: t("map.otherWorks"), value: String(works.length - papers.length) },
            ]}
          />
          <BarChart
            label={t("map.topicHistory")}
            data={years.map((value) => ({
              label: String(value),
              value: papers.filter((item) => item.year === value).length,
              displayValue: t("explore.papers", {
                count: papers.filter((item) => item.year === value).length,
              }),
            }))}
          />
          {unknown > 0 && (
            <p className="text-xs text-muted-foreground">
              {t("map.undatedCount", { count: unknown })}
            </p>
          )}
          {candidate.brief.problem && (
            <p className="line-clamp-3 text-sm leading-relaxed">{candidate.brief.problem.text}</p>
          )}
          <Button asChild variant="outline" className="w-full">
            <Link to={trendPath(job.id, candidate.id, location.pathname + location.search)}>
              {t("map.details")}
              <ArrowUpRight aria-hidden />
            </Link>
          </Button>
        </>
      )}
      <div className="space-y-2 border-t pt-3">
        <p className="text-xs font-medium text-muted-foreground">{t("map.latestSources")}</p>
        <ul className="divide-y">
          {sources.slice(0, 3).map((item) => (
            <li key={item.id} className="py-2">
              <Button
                variant="link"
                size="inline"
                onClick={() => onSelect(candidate.id, item.id)}
                className="text-left"
              >
                <span className="line-clamp-2">{item.title}</span>
              </Button>
              <p className="mt-1 text-xs text-muted-foreground">
                {item.source.source.provider} ·{" "}
                {item.source.source.publication?.value ?? t("map.undated")}
              </p>
            </li>
          ))}
        </ul>
        {!sources.length && <p className="text-sm text-muted-foreground">{t("map.noYearWorks")}</p>}
      </div>
    </aside>
  );
}
