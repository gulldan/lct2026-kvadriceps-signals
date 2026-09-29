import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/shared/ui/Sheet";
import { DocumentPreview } from "@/features/document/ui/DocumentPreview";
import { OriginalDocViewer } from "@/features/document/ui/OriginalDocViewer";
import type { PdfHighlight } from "@/shared/ui/PdfViewer";
import { KbDoc } from "@/shared/types";
import { Segmented } from "@/shared/ui/Segmented";
import { publishedLabel } from "@/features/document/docAdapter";
import { SourceVersionLink } from "./SourceVersionLink";

type Tab = "original" | "text";

interface DocPreviewSheetProps {
  doc: KbDoc | null;
  query?: string;
  fragmentId?: string;
  highlight?: PdfHighlight;
  citedFragmentIds?: string[];
  fragmentLocations?: Record<string, string>;
  relevance?: number;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
  onClose: () => void;
  onCloseAutoFocus?: (event: Event) => void;
}

export function DocPreviewSheet({
  doc,
  query,
  fragmentId,
  highlight,
  citedFragmentIds,
  fragmentLocations,
  relevance,
  loading,
  error,
  onRetry,
  onClose,
  onCloseAutoFocus,
}: DocPreviewSheetProps) {
  const { t } = useTranslation("document");
  const [tab, setTab] = useState<Tab>("original");
  const [originalUnavailable, setOriginalUnavailable] = useState(false);
  useEffect(() => {
    // Цитату в оригинале подсвечивает только PDF; прочее открывается на восстановленном тексте.
    const isPdf = doc?.fileType === "pdf";
    setTab(fragmentId && !isPdf ? "text" : "original");
    setOriginalUnavailable(false);
  }, [doc?.id, doc?.fileType, fragmentId]);

  return (
    <Sheet open={doc !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side="right"
        className="flex w-[95vw] flex-col gap-0 p-0 sm:w-[88vw] sm:max-w-5xl"
        onCloseAutoFocus={onCloseAutoFocus}
      >
        {doc && (
          <>
            <div className="flex items-center gap-3 border-b px-4 py-3 pr-12">
              <div className="min-w-0 flex-1">
                <SheetTitle className="truncate text-base">{doc.title}</SheetTitle>
                <SheetDescription className="truncate text-xs">{doc.fileName}</SheetDescription>
                <SourceVersionLink doc={doc} fragment={fragmentId} />
                {(doc.author || doc.publishedAt) && (
                  <p className="truncate text-xs text-muted-foreground">
                    {[doc.author, publishedLabel(doc.publishedAt)].filter(Boolean).join(" · ")}
                  </p>
                )}
              </div>
              <Segmented
                size="sm"
                aria-label={t("preview.tabOriginal") + " / " + t("preview.tabText")}
                options={[
                  {
                    value: "original",
                    label: t("preview.tabOriginal"),
                    disabled: originalUnavailable,
                  },
                  { value: "text", label: t("preview.tabText") },
                ]}
                value={tab}
                onChange={setTab}
                className="shrink-0"
              />
            </div>
            <div className="min-h-0 flex-1">
              {tab === "original" ? (
                <OriginalDocViewer
                  docId={doc.id}
                  fileName={doc.fileName}
                  fileType={doc.fileType}
                  highlight={highlight}
                  onUnavailable={() => {
                    setOriginalUnavailable(true);
                    setTab("text");
                  }}
                />
              ) : (
                <div className="h-full overflow-y-auto p-6">
                  {originalUnavailable && (
                    <p className="mb-4 rounded-lg bg-secondary px-3 py-2 text-sm text-muted-foreground">
                      {t("preview.originalUnavailable")}
                    </p>
                  )}
                  <DocumentPreview
                    doc={doc}
                    query={query}
                    fragmentId={fragmentId}
                    citedFragmentIds={citedFragmentIds}
                    fragmentLocations={fragmentLocations}
                    relevance={relevance}
                    loading={loading}
                    error={error}
                    onRetry={onRetry}
                  />
                </div>
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
