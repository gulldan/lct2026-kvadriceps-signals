import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { Spinner } from "@/shared/ui/Spinner";
import { formatDate } from "@/shared/lib/format";
import {
  getDocumentTechnologies,
  technologyPath,
  type RevisionTechnologies,
} from "../technologyApi";
import { COVERAGE_STATUSES, EXTRACTION_STATUSES, ROLES, known } from "../technologyLabels";

export function DocumentTechnologies({ documentId }: { documentId: string }) {
  const { t } = useTranslation("document");
  const location = useLocation();
  const [result, setResult] = useState<RevisionTechnologies | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    setResult(null);
    setError(null);
    getDocumentTechnologies(documentId, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setResult(value);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(failure instanceof ApiError ? failure : new ApiError(0, String(failure)));
      });
    return () => controller.abort();
  }, [documentId]);
  const returnTo = location.pathname + location.search;
  return (
    <Section title={t("source.technologies.title")}>
      {error ? (
        <Notice tone={error.status === 403 ? "warn" : "risk"}>
          {error.status === 403
            ? t("source.technologies.forbidden")
            : t("source.technologies.error")}
        </Notice>
      ) : !result ? (
        <output className="flex items-center gap-2 text-sm text-muted-foreground">
          <span aria-hidden="true">
            <Spinner />
          </span>
          {t("source.technologies.loading")}
        </output>
      ) : (
        <div className="space-y-3 text-sm">
          <CoverageLine coverage={result.coverage} />
          {result.coverage.extracted_at && (
            <p className="text-xs text-muted-foreground">
              {t("source.technologies.extracted", {
                date: formatDate(result.coverage.extracted_at),
                extractor: result.extractor_version,
              })}
            </p>
          )}
          {result.technologies.length > 0 && (
            <ul className="divide-y">
              {result.technologies.map((mention) => {
                const role = known(ROLES, mention.role) ?? "unknown";
                const status = known(EXTRACTION_STATUSES, mention.extraction_status);
                const quote = mention.evidence[0]?.quote;
                return (
                  <li key={mention.mention_id} className="space-y-1 py-3 first:pt-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <Button variant="link" size="inline" asChild className="font-medium">
                        <Link
                          to={technologyPath(mention.technology_id, returnTo)}
                          aria-label={`${mention.name}: ${t("source.technologies.open")}`}
                        >
                          {mention.name}
                        </Link>
                      </Button>
                      <Badge variant={role === "primary" ? "brand" : "outline"}>
                        {t(`source.technologies.roles.${role}`)}
                      </Badge>
                      {status && status !== "complete" && (
                        <Badge variant="warn">
                          {t(`source.technologies.extraction.${status}`)}
                        </Badge>
                      )}
                    </div>
                    {quote && (
                      <p className="text-xs leading-relaxed text-muted-foreground">
                        <mark className="rounded-sm bg-brand-wash px-0.5">
                          {quote.length > 200 ? `${quote.slice(0, 200)}…` : quote}
                        </mark>
                      </p>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </Section>
  );
}

function CoverageLine({ coverage }: { coverage: RevisionTechnologies["coverage"] }) {
  const { t } = useTranslation("document");
  const status = known(COVERAGE_STATUSES, coverage.status);
  if (!status) return <p className="text-muted-foreground">{coverage.status}</p>;
  const tone =
    status === "failed"
      ? "risk"
      : status === "unsupported_language" || status === "partial"
        ? "warn"
        : undefined;
  const text = t(`source.technologies.status.${status}`, {
    reason: coverage.reason_code || "—",
    processed: coverage.processed_sections,
    total: coverage.total_sections,
  });
  return tone ? (
    <Notice tone={tone}>{text}</Notice>
  ) : (
    <p className="text-muted-foreground">{text}</p>
  );
}
