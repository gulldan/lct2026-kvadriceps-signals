import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ExternalLink } from "lucide-react";
import {
  getSourceEvidence,
  sourceContentURL,
  sourceReturnPath,
  locateSourceQuote,
  type SourceEvidence,
} from "../sourceApi";
import { DocumentTechnologies, SourceAnalysisRelations } from "@/features/trend";
import { SourceProvenance } from "../ui/SourceProvenance";
import { SourceFragments } from "../ui/SourceFragments";
import { OriginalDocViewer } from "../ui/OriginalDocViewer";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { Segmented } from "@/shared/ui/Segmented";
import { Disclosure } from "@/shared/ui/Disclosure";

export function SourcePage() {
  const { id = "" } = useParams<{ id: string }>();
  const [params, setParams] = useSearchParams();
  const { t } = useTranslation("document");
  const { t: trendT } = useTranslation("trend");
  const snapshot = params.get("snapshot") || "";
  const fragment = params.get("fragment") || "";
  const analysis = params.get("analysis") || "";
  const rawReturn = params.get("return") || "";
  const returnTo = sourceReturnPath(rawReturn, analysis);
  const returnLabels: Record<string, string> = {
    operator: t("source.back"),
    search: t("source.backSearch"),
    hypotheses: returnTo.startsWith("/hypotheses/")
      ? t("source.backHypothesis")
      : t("source.backHypotheses"),
    directions: t("source.backDirection"),
    clusters: t("source.backCluster"),
    kpi: t("source.backGoals"),
    technologies: t("source.backTechnology"),
    trends: t("source.backRadar"),
  };
  const returnLabel = returnLabels[returnTo.split(/[/?#]/)[1] ?? ""] ?? trendT("backContext");
  const anchor =
    params.has("start") || params.has("end") || params.has("textHash")
      ? {
          start: Number(params.get("start") ?? NaN),
          end: Number(params.get("end") ?? NaN),
          textHash: params.get("textHash") || "",
        }
      : undefined;
  const key = id + ":" + snapshot;
  const [loaded, setLoaded] = useState<{ key: string; value: SourceEvidence } | null>(null);
  const [failed, setFailed] = useState("");
  const [retry, setRetry] = useState(0);
  const [tab, setTab] = useState<"text" | "original">("text");
  const evidence = loaded?.key === key ? loaded.value : null;
  useEffect(() => {
    const controller = new AbortController();
    setFailed("");
    setLoaded(null);
    getSourceEvidence(id, snapshot || undefined, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setLoaded({ key, value });
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(key);
      });
    return () => controller.abort();
  }, [id, snapshot, key, retry]);
  useEffect(() => {
    if (!snapshot && evidence)
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);
          next.set("snapshot", evidence.snapshot_id);
          return next;
        },
        { replace: true },
      );
  }, [snapshot, evidence, setParams]);
  const pagination = Number(params.get("page") || 1);
  const currentPage = Number.isSafeInteger(pagination) && pagination > 0 ? pagination : 1;
  const quote = evidence && anchor ? locateSourceQuote(evidence, anchor) : null;
  const selected = evidence?.chunks.find((chunk) => chunk.id === (quote?.fragment || fragment));
  const original = evidence?.mime_type === "application/pdf";
  return (
    <PageLayout className="space-y-6">
      <PageToolbar>
        <BackButton asChild>
          <Link to={returnTo}>
            <ArrowLeft className="size-4" aria-hidden />
            {returnLabel}
          </Link>
        </BackButton>
      </PageToolbar>
      {failed === key ? (
        <ErrorState
          message={t("source.unavailable")}
          onRetry={() => setRetry((value) => value + 1)}
        />
      ) : !evidence ? (
        <output className="flex items-center gap-3 py-12">
          <span aria-hidden="true">
            <Spinner />
          </span>
          {t("source.loading")}
        </output>
      ) : (
        <>
          <PageHeader
            title={evidence.title || evidence.filename}
            description={
              evidence.source.provider === "arxiv"
                ? t("source.arxiv")
                : evidence.source.provider === "github"
                  ? t("source.github")
                  : t(`source.kind.${evidence.source.genre ?? evidence.source.source_kind}`)
            }
            actions={
              <Button variant="outline" asChild>
                <a href={sourceContentURL(evidence.source)} target="_blank" rel="noreferrer">
                  <ExternalLink className="size-4" aria-hidden />
                  {t("source.providerLink")}
                </a>
              </Button>
            }
          />
          <div className="grid items-start gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
            <div>
              <div className="hidden lg:block">
                <SourceProvenance evidence={evidence} />
              </div>
              <Disclosure className="lg:hidden" title={t("source.sourceDetails")}>
                <SourceProvenance evidence={evidence} />
              </Disclosure>
              <div className="mt-8">
                <DocumentTechnologies documentId={id} />
              </div>
            </div>
            <div className="min-w-0 space-y-6">
              {original && (
                <Segmented
                  aria-label={t("source.view")}
                  value={tab}
                  onChange={setTab}
                  options={[
                    { value: "text", label: t("preview.tabText") },
                    { value: "original", label: t("preview.tabOriginal") },
                  ]}
                />
              )}
              {original && tab === "original" ? (
                <div className="h-[70vh] min-h-96 overflow-hidden rounded-md border">
                  <OriginalDocViewer
                    docId={evidence.document_id}
                    snapshotID={evidence.snapshot_id}
                    fileName={evidence.filename}
                    fileType="pdf"
                    highlight={
                      selected
                        ? {
                            snippet: quote?.quote || selected.text,
                            page: selected.page_start || undefined,
                          }
                        : undefined
                    }
                  />
                </div>
              ) : (
                <SourceFragments
                  evidence={evidence}
                  fragment={fragment}
                  anchor={anchor}
                  page={currentPage}
                  onPage={(page) =>
                    setParams((previous) => {
                      const next = new URLSearchParams(previous);
                      next.set("page", String(page));
                      next.delete("fragment");
                      next.delete("start");
                      next.delete("end");
                      next.delete("textHash");
                      return next;
                    })
                  }
                />
              )}
            </div>
          </div>
          <SourceAnalysisRelations analysis={analysis} documentID={id} />
        </>
      )}
    </PageLayout>
  );
}
