import { Link, useLocation, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { sourcePath } from "@/features/document";
import { formatDate, formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Chip } from "@/shared/ui/Chip";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/shared/ui/Table";
import { providerLabel } from "../providerLabel";
import type { AnalysisJob, TrendCandidate } from "../trendApi";

export function TrendSources({ job, candidate }: { job: AnalysisJob; candidate: TrendCandidate }) {
  const { t } = useTranslation("trend");
  const { t: sourceT } = useTranslation("document");
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const provider = params.get("provider") || "all";
  const documents = new Set(
    job
      .result!.works.filter((work) => candidate.work_ids.includes(work.id))
      .flatMap((work) => work.document_ids),
  );
  const sources = job.sources.filter((source) => documents.has(source.document_id));
  const providers = [...new Set(sources.map((source) => source.source.provider))].toSorted();
  const visible = sources.filter(
    (source) => provider === "all" || source.source.provider === provider,
  );
  const select = (value: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (value === "all") next.delete("provider");
      else next.set("provider", value);
      return next;
    });
  return (
    <div className="space-y-4">
      {sources.length > 0 && (
        <fieldset className="flex flex-wrap gap-2">
          <legend className="sr-only">{t("providerFilter")}</legend>
          <Chip aria-pressed={provider === "all"} onClick={() => select("all")}>
            {t("allProviders")} · {sources.length}
          </Chip>
          {providers.map((name) => (
            <Chip key={name} aria-pressed={provider === name} onClick={() => select(name)}>
              {providerLabel(name)} ·{" "}
              {sources.filter((source) => source.source.provider === name).length}
            </Chip>
          ))}
        </fieldset>
      )}
      {!sources.length ? (
        <p className="text-sm text-muted-foreground">{t("sourcesUnavailable")}</p>
      ) : (
        <Table caption={t("sources")}>
          <TableHeader>
            <TableRow>
              {(["date", "title", "kind", "origin"] as const).map((key) => (
                <TableHead key={key}>{t(`topic.columns.${key}`)}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.map((item) => {
              const id = `source-${item.document_id}`;
              const source = item.source;
              return (
                <TableRow key={item.document_id}>
                  <TableCell
                    className="font-mono text-xs whitespace-nowrap tabular-nums"
                    title={
                      source.publication
                        ? t(`precision_${source.publication.precision}`)
                        : undefined
                    }
                  >
                    {source.publication?.value || t("unknown")}
                  </TableCell>
                  <TableCell className="min-w-64">
                    <Button variant="link" size="inline" asChild>
                      <Link
                        id={id}
                        className="wrap-anywhere"
                        to={sourcePath(item.document_id, item.snapshot_id, undefined, {
                          analysis: job.id,
                          returnTo: location.pathname + location.search + `#${id}`,
                        })}
                      >
                        {item.title || t("sourceUntitled")}
                      </Link>
                    </Button>
                    {source.github && (
                      <span className="ml-2 text-xs whitespace-nowrap text-muted-foreground">
                        {source.github.stars === null
                          ? `★ ${t("unknown")}`
                          : t("stars", {
                              count: formatNumber(source.github.stars),
                              date: formatDate(source.observed_at),
                            })}
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{sourceT(`source.kind.${source.source_kind}`)}</Badge>
                  </TableCell>
                  <TableCell className="text-xs wrap-anywhere text-muted-foreground">
                    {providerLabel(source.provider)}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
      <p className="text-xs text-muted-foreground">
        {t("filterHint")}
        {sources.some((item) => item.source.github) && ` ${t("starsHint")}`}
      </p>
    </div>
  );
}
