import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Button } from "@/shared/ui/Button";
import { formatDate } from "@/shared/lib/format";
import type { LiveChannel } from "../liveApi";
import { LiveLimits } from "./LiveLimits";
import { SourcePassport } from "./SourcePassport";

const PAGE = 4;

export function ChannelBlock({ source }: { source?: LiveChannel | null }) {
  const { t } = useTranslation("trend");
  const [limit, setLimit] = useState(PAGE);
  const records = source?.records ?? [];
  const shown = records.slice(0, limit);
  return (
    <div className="min-w-0 space-y-2">
      {source && (
        <p className="text-caption text-muted-foreground">
          {t("study.live.additional.observed", {
            source: source.source,
            date: formatDate(source.observed_at),
          })}
        </p>
      )}
      {shown.length ? (
        <ul className="divide-y">
          {shown.map((row) => (
            <li key={row.id} className="space-y-1 py-2">
              <Button asChild variant="link" size="inline" className="text-left">
                <a href={row.url} target="_blank" rel="noopener noreferrer">
                  {row.title}
                </a>
              </Button>
              <SourcePassport
                publisherType={row.publisher_type}
                genre={row.genre}
                trust={row.trust}
                trustBasis={row.trust_basis}
                language={row.language}
                languageBasis={row.language_basis}
              />
              <p className="text-caption text-muted-foreground">
                {formatDate(row.date)} · {row.source}
                {row.text_status &&
                  ` · ${t(`study.live.sourceCoverage.${row.text_status === "article_text" ? "text" : row.text_status === "representative_claim" ? "claim" : "metadata"}`)}`}
                {(row.filing || row.priority) &&
                  ` · ${t("study.live.sourceCoverage.dates", {
                    filing: row.filing || "—",
                    priority: row.priority || "—",
                  })}`}
              </p>
              {row.original_url && row.original_url !== row.url && (
                <Button asChild variant="link" size="inline">
                  <a href={row.original_url} target="_blank" rel="noopener noreferrer">
                    {t("study.live.sourceCoverage.original")}
                  </a>
                </Button>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-xs text-muted-foreground">
          {t(source ? "study.live.additional.empty" : "study.live.additional.unavailable")}
        </p>
      )}
      {records.length > shown.length && (
        <Button variant="outline" size="sm" onClick={() => setLimit(limit + 12)}>
          {t("study.moreSources", { count: records.length - shown.length })}
        </Button>
      )}
      {!!source?.limits?.length && <LiveLimits limits={source.limits} />}
      {!!source?.coverage?.length && (
        <Disclosure title={t("study.live.sourceCoverage.title")}>
          <p className="text-caption text-muted-foreground">
            {t("study.live.sourceCoverage.hint")}
          </p>
          <ul className="divide-y text-xs">
            {source.coverage.map((row) => (
              <li key={`${row.source}-${row.from}`} className="space-y-1 py-2">
                <p className="font-medium">
                  {row.source} · {row.from}–{row.through}
                </p>
                <p>
                  {t("study.live.sourceCoverage.counts", {
                    received: row.received,
                    content: row.content,
                  })}
                </p>
                <p>
                  {t(
                    `study.live.sourceCoverage.${row.exhausted ? "complete" : row.next_cursor ? "partial" : "unknown"}`,
                  )}
                </p>
                {row.missing_titles > 0 && (
                  <p>{t("study.live.sourceCoverage.missing", { count: row.missing_titles })}</p>
                )}
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
    </div>
  );
}
