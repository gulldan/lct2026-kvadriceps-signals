import { useTranslation } from "react-i18next";
import { CardButton } from "@/shared/ui/Card";
import { Disclosure } from "@/shared/ui/Disclosure";
import { FileTypeBadge } from "@/shared/ui/FileTypeBadge";
import { Kicker } from "@/shared/ui/Kicker";
import type { SmartAnswer, SmartAnswerSource } from "@/shared/types";

interface SourceListProps {
  sources: SmartAnswerSource[];
  numbered?: boolean;
  selectedDocId?: string;
  onOpen: (docId: string, fragmentId?: string) => void;
}

function SourceList({ sources, numbered, selectedDocId, onOpen }: SourceListProps) {
  return (
    <ol className="space-y-2">
      {sources.map((source, index) => {
        const selected = selectedDocId === source.doc.id;
        const metaLine = [source.doc.fileName, source.page, source.section]
          .filter(Boolean)
          .join(" · ");
        return (
          <li key={source.doc.id}>
            <CardButton
              type="button"
              onClick={() => onOpen(source.doc.id, source.fragmentId)}
              className={`flex items-start gap-3 px-4 py-3 ${selected ? "border-brand ring-1 ring-brand" : ""}`}
            >
              {numbered && (
                <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md bg-brand-wash font-mono text-xs font-semibold text-accent-foreground">
                  {index + 1}
                </span>
              )}
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm leading-snug font-medium">
                  {source.doc.title}
                </span>
                <span className="mt-0.5 flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
                  <FileTypeBadge type={source.doc.fileType} />
                  <span className="truncate">{metaLine}</span>
                </span>
              </span>
              {source.relevance !== undefined && (
                <span className="mt-0.5 shrink-0 font-mono text-xs font-semibold text-muted-foreground">
                  {source.relevance} %
                </span>
              )}
            </CardButton>
          </li>
        );
      })}
    </ol>
  );
}

export function AnswerSources({
  answer,
  selectedDocId,
  onOpen,
}: Omit<SourceListProps, "sources" | "numbered"> & { answer: SmartAnswer }) {
  const { t } = useTranslation("search");
  const citedIds = new Set(answer.chips.map((chip) => chip.docId));
  const cited = answer.sources.filter((source) => citedIds.has(source.doc.id));
  const retrieved = answer.sources.filter((source) => !citedIds.has(source.doc.id));
  return (
    <div className="mt-5 space-y-4">
      {cited.length > 0 && (
        <div>
          <Kicker className="mb-2">{t("answer.sourcesHeading", { count: cited.length })}</Kicker>
          <SourceList sources={cited} numbered selectedDocId={selectedDocId} onOpen={onOpen} />
        </div>
      )}
      {retrieved.length > 0 && (
        <Disclosure title={t("answer.retrievedHeading", { count: retrieved.length })}>
          <p className="mb-3 text-sm text-muted-foreground">{t("answer.retrievedDescription")}</p>
          <SourceList sources={retrieved} selectedDocId={selectedDocId} onOpen={onOpen} />
        </Disclosure>
      )}
    </div>
  );
}
