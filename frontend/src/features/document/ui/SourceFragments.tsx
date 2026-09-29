import { useEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  sourcePath,
  locateSourceQuote,
  type SourceEvidence,
  type SourceAnchor,
} from "../sourceApi";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { Section } from "@/shared/ui/Section";

const PAGE_SIZE = 30;

export function SourceFragments({
  evidence,
  fragment,
  anchor,
  page,
  onPage,
}: {
  evidence: SourceEvidence;
  fragment: string;
  anchor?: SourceAnchor;
  page: number;
  onPage: (page: number) => void;
}) {
  const { t } = useTranslation("document");
  const { t: trendT } = useTranslation("trend");
  const [params] = useSearchParams();
  const selected = useRef<HTMLElement>(null);
  const quote = anchor ? locateSourceQuote(evidence, anchor) : null;
  const selectedID = quote?.fragment || fragment;
  const found = evidence.chunks.findIndex((chunk) => chunk.id === selectedID);
  const pages = Math.max(1, Math.ceil(evidence.chunks.length / PAGE_SIZE));
  const current = found >= 0 ? Math.floor(found / PAGE_SIZE) + 1 : Math.min(pages, page);
  const visible = evidence.chunks.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE);
  useEffect(() => {
    selected.current?.scrollIntoView({ block: "center" });
    if (selectedID) selected.current?.focus({ preventScroll: true });
  }, [selectedID, evidence.snapshot_id]);
  return (
    <Section
      title={t("source.fragments")}
      action={
        <p className="text-sm text-muted-foreground">
          {t("source.fragmentCount", { count: evidence.chunks.length })}
        </p>
      }
    >
      {fragment && found < 0 && <Notice>{t("source.missingFragment")}</Notice>}
      {anchor && !quote && <Notice>{trendT("quoteUnavailable")}</Notice>}
      {quote && (
        <div className="mb-5 space-y-2 border-l pl-4">
          <h3 className="text-sm font-medium">{trendT("selectedQuote")}</h3>
          <ReadingContent flow="text">
            <blockquote className="whitespace-pre-wrap">{quote.quote}</blockquote>
          </ReadingContent>
        </div>
      )}
      <div className="divide-y">
        {visible.map((chunk) => (
          <ReadingContent
            flow="text"
            key={chunk.id}
            ref={chunk.id === selectedID ? selected : undefined}
            tabIndex={chunk.id === selectedID ? -1 : undefined}
            className={
              chunk.id === selectedID ? "bg-accent px-3 py-6 ring-1 ring-brand-border" : "py-6"
            }
          >
            <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
              <Button asChild variant="link" size="inline">
                <Link
                  to={sourcePath(evidence.document_id, evidence.snapshot_id, chunk.id, {
                    analysis: params.get("analysis") || undefined,
                    returnTo: params.get("return") || undefined,
                  })}
                >
                  {t("source.fragment", { number: chunk.index + 1 })}
                </Link>
              </Button>
              <span className="text-xs text-muted-foreground">
                {chunk.page_start > 0 ? t("source.page", { page: chunk.page_start }) + " · " : ""}
                {t("source.characters", { start: chunk.char_start + 1, end: chunk.char_end })}
                {chunk.locator_kind === "ocr_page" ? " · OCR" : ""}
              </span>
            </div>
            {chunk.section && <p className="mb-2 text-sm text-muted-foreground">{chunk.section}</p>}
            <p className="whitespace-pre-wrap">
              {anchor && quote && chunk.char_start < anchor.end && chunk.char_end > anchor.start ? (
                <>
                  {Array.from(chunk.text)
                    .slice(0, Math.max(0, anchor.start - chunk.char_start))
                    .join("")}
                  <mark className="bg-marker text-foreground">
                    {Array.from(chunk.text)
                      .slice(
                        Math.max(0, anchor.start - chunk.char_start),
                        anchor.end - chunk.char_start,
                      )
                      .join("")}
                  </mark>
                  {Array.from(chunk.text)
                    .slice(Math.max(0, anchor.end - chunk.char_start))
                    .join("")}
                </>
              ) : evidence.mime_type === "application/pdf" ? (
                chunk.text.replace(/\n(?:[^\S\n]*\n){2,}/g, "\n\n")
              ) : (
                chunk.text
              )}
            </p>
          </ReadingContent>
        ))}
      </div>
      {pages > 1 && (
        <nav
          aria-label={t("source.pagination")}
          className="mt-6 flex flex-wrap items-center justify-between gap-3 border-t pt-5"
        >
          <Button variant="outline" disabled={current === 1} onClick={() => onPage(current - 1)}>
            {t("source.previous")}
          </Button>
          <span className="text-sm text-muted-foreground">
            {t("source.pageOf", { current, total: pages })}
          </span>
          <Button
            variant="outline"
            disabled={current === pages}
            onClick={() => onPage(current + 1)}
          >
            {t("source.next")}
          </Button>
        </nav>
      )}
    </Section>
  );
}
