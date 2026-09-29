import { Card } from "@/shared/ui/Card";
import { ChevronDown, ChevronUp, Clock, Sparkles } from "lucide-react";
import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { formatDate, formatDateShort } from "@/shared/lib/format";
import { getReaderContent, DocBlock } from "@/features/document/docContent";
import { highlightParts } from "@/shared/lib/highlight";
import { KbDoc } from "@/shared/types";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui";
import { FileTypeBadge } from "@/shared/ui/FileTypeBadge";
import { Separator } from "@/shared/ui/Separator";
import { Skeleton } from "@/shared/ui/Skeleton";

interface DocumentPreviewProps {
  doc: KbDoc;
  query?: string;
  fragmentId?: string;
  citedFragmentIds?: string[];
  fragmentLocations?: Record<string, string>;
  relevance?: number;
  showHeader?: boolean;
  loading?: boolean;
  error?: boolean;
  onRetry?: () => void;
}

function LoadingBody() {
  const { t } = useTranslation("document");
  return (
    <Card asChild className="block px-5 py-5">
      <output aria-label={t("preview.loadingAria")}>
        <div className="space-y-3">
          <Skeleton className="mx-auto h-5 w-2/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-11/12" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-4/5" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/5" />
        </div>
      </output>
    </Card>
  );
}

function HighlightedText({ text, query }: { text: string; query?: string }) {
  if (!query) return <>{text}</>;
  let offset = 0;
  const nodes = highlightParts(text, query).map((p) => {
    const at = offset;
    offset += p.part.length;
    return p.hit ? (
      <mark key={at} className="rounded-sm bg-marker px-0.5 font-medium text-foreground">
        {p.part}
      </mark>
    ) : (
      <Fragment key={at}>{p.part}</Fragment>
    );
  });
  return <>{nodes}</>;
}

function withBlockKeys(blocks: DocBlock[]): { block: DocBlock; key: string }[] {
  const seen = new Map<string, number>();
  return blocks.map((block) => {
    const base =
      block.kind === "paragraph" && block.fragmentId !== undefined
        ? block.fragmentId
        : `${block.kind}:${block.text.slice(0, 40)}`;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return { block, key: n === 0 ? base : `${base}#${n}` };
  });
}

export function DocumentPreview({
  doc,
  query,
  fragmentId,
  citedFragmentIds,
  fragmentLocations,
  relevance,
  showHeader = true,
  loading = false,
  error = false,
  onRetry,
}: DocumentPreviewProps) {
  const { t } = useTranslation("document");
  const highlightRef = useRef<HTMLDivElement>(null);

  const [activeFragmentId, setActiveFragmentId] = useState<string | null>(fragmentId ?? null);
  // Новый ключ перемонтирует блок, и повторный переход к тому же месту снова мигает.
  const [flashNonce, setFlashNonce] = useState(0);
  useEffect(() => {
    setActiveFragmentId(fragmentId ?? null);
    setFlashNonce((n) => n + 1);
  }, [fragmentId, doc.id]);

  const ready = !loading && !error;
  const content = useMemo(() => getReaderContent(doc), [doc]);

  const cited = useMemo(() => {
    const wanted = new Set(citedFragmentIds ?? (fragmentId ? [fragmentId] : []));
    if (wanted.size === 0) return [];
    const ordered: string[] = [];
    const blocks = content.type === "pdf" ? content.pages.flat() : content.blocks;
    for (const b of blocks) {
      if (b.kind === "paragraph" && b.fragmentId && wanted.has(b.fragmentId)) {
        ordered.push(b.fragmentId);
      }
    }
    return ordered;
  }, [content, citedFragmentIds, fragmentId]);

  const activeIdx = activeFragmentId ? cited.indexOf(activeFragmentId) : -1;

  const activeLocation = (activeFragmentId && fragmentLocations?.[activeFragmentId]) || undefined;

  const jumpTo = (id: string) => {
    setActiveFragmentId(id);
    setFlashNonce((n) => n + 1);
  };

  const jumpBy = (delta: number) => {
    const next = cited.at((activeIdx + delta + cited.length) % cited.length);
    if (next !== undefined) jumpTo(next);
  };

  useEffect(() => {
    if (!ready) return;
    const first = cited.at(0);
    if (first === undefined) return;
    if (activeFragmentId && cited.includes(activeFragmentId)) return;
    setActiveFragmentId(first);
    setFlashNonce((n) => n + 1);
  }, [ready, cited, activeFragmentId]);

  const scrollToHighlight = () => {
    highlightRef.current?.scrollIntoView({ behavior: "smooth", block: "center" });
  };
  // Двойной проход страхует от сдвигов раскладки (шрифты, анимация панели).
  useEffect(() => {
    if (!activeFragmentId || !ready) return;
    const t1 = setTimeout(scrollToHighlight, 80);
    const t2 = setTimeout(scrollToHighlight, 420);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
    };
  }, [doc, activeFragmentId, flashNonce, ready]);

  const renderBlock = (block: DocBlock, key: string) => {
    if (block.kind === "heading") {
      return (
        <h4 key={key} className="pt-1 text-sm font-semibold">
          {block.text}
        </h4>
      );
    }
    const isActive = block.fragmentId !== undefined && block.fragmentId === activeFragmentId;
    const isCited = !isActive && block.fragmentId !== undefined && cited.includes(block.fragmentId);

    if (isActive) {
      return (
        <div key={`${key}-${flashNonce}`} ref={highlightRef}>
          <span className="mr-2 mb-1.5 inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-caption font-semibold text-primary-foreground">
            <Sparkles className="size-3" aria-hidden />
            {t("preview.answersQuestion")}
          </span>
          {activeLocation && (
            <span className="text-caption text-muted-foreground">{activeLocation}</span>
          )}
          <p className="hl-frag hl-frag-flash px-3 py-2 leading-relaxed">
            <HighlightedText text={block.text} query={query} />
          </p>
        </div>
      );
    }
    if (isCited) {
      return (
        <Button
          variant="ghost"
          size="content"
          key={key}
          type="button"
          className="hl-frag-ctx block w-full px-3 py-2 text-left leading-relaxed"
          title={t("preview.alsoAnswers")}
          onClick={() => block.fragmentId && jumpTo(block.fragmentId)}
        >
          <HighlightedText text={block.text} query={query} />
        </Button>
      );
    }
    return (
      <p key={key} className="px-0.5 leading-relaxed text-foreground">
        {block.text}
      </p>
    );
  };

  const renderBody = () => {
    if (content.type === "pdf") {
      const total = content.pages.length;
      const pages = content.pages.map((blocks, idx) => ({
        num: idx + 1,
        entries: withBlockKeys(blocks),
      }));
      return (
        <div className="space-y-4">
          {pages.map(({ num, entries }) => {
            const first = entries.at(0);
            const cover = num === 1 && first?.block.kind === "heading" ? first : undefined;
            const body = cover ? entries.slice(1) : entries;
            return (
              <Card key={`page-${num}`} className="px-5 py-5">
                <div className="space-y-3 text-sm">
                  {cover && (
                    <h3 className="pb-1 text-center text-base font-semibold">{cover.block.text}</h3>
                  )}
                  {body.map(({ block, key }) => renderBlock(block, key))}
                </div>
                <p className="mt-5 border-t pt-2 text-center text-caption text-muted-foreground">
                  {t("preview.pageOf", { page: num, total })}
                </p>
              </Card>
            );
          })}
        </div>
      );
    }
    if (content.type === "docx") {
      return (
        <Card className="px-5 py-5">
          <h3 className="pb-3 text-center text-base font-semibold">{doc.title}</h3>
          <div className="space-y-3 text-sm">
            {withBlockKeys(content.blocks).map(({ block, key }) => renderBlock(block, key))}
          </div>
        </Card>
      );
    }
    return (
      <Card>
        {doc.emailMeta && (
          <div className="border-b bg-muted px-5 py-3">
            <dl className="space-y-1 text-xs">
              <div className="flex gap-2">
                <dt className="w-12 shrink-0 text-muted-foreground">{t("preview.emailFrom")}</dt>
                <dd className="font-medium">{doc.emailMeta.from}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-12 shrink-0 text-muted-foreground">{t("preview.emailTo")}</dt>
                <dd>{doc.emailMeta.to}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-12 shrink-0 text-muted-foreground">{t("preview.emailSubject")}</dt>
                <dd className="font-medium">{doc.emailMeta.subject}</dd>
              </div>
              <div className="flex gap-2">
                <dt className="w-12 shrink-0 text-muted-foreground">{t("preview.emailDate")}</dt>
                <dd>{formatDateShort(doc.emailMeta.date)}</dd>
              </div>
            </dl>
          </div>
        )}
        <div className="space-y-3 px-5 py-4 text-sm">
          {withBlockKeys(content.blocks).map(({ block, key }) => renderBlock(block, key))}
        </div>
      </Card>
    );
  };

  const renderError = () => {
    const snippet = (doc.snippet ?? "").trim();
    return (
      <div className="space-y-3">
        <ErrorState
          variant="compact"
          message={t(snippet ? "preview.snippetOnly" : "preview.contentFailed")}
          onRetry={onRetry}
        />
        {snippet && (
          <Card className="px-5 py-5 text-sm leading-relaxed whitespace-pre-line">{snippet}</Card>
        )}
      </div>
    );
  };

  // Часть цитат может не попасть в просмотр: у очень больших документов бэкенд отдаёт начало.
  const wantedCount = (citedFragmentIds ?? (fragmentId ? [fragmentId] : [])).length;
  const missingCited = ready && wantedCount > 0 && cited.length < wantedCount;

  const fragmentNav = ready && cited.length > 0 && (
    <div className="sticky top-0 z-(--layer-sticky) -mx-4 mb-3 flex items-center gap-1.5 border-b bg-background px-4 py-2 backdrop-blur">
      <Button
        variant="outline"
        size="icon-sm"
        aria-label={t("preview.prevMatch")}
        disabled={cited.length < 2}
        onClick={() => jumpBy(-1)}
      >
        <ChevronUp className="size-3.5" aria-hidden />
      </Button>
      <Button
        variant="outline"
        size="icon-sm"
        aria-label={t("preview.nextMatch")}
        disabled={cited.length < 2}
        onClick={() => jumpBy(1)}
      >
        <ChevronDown className="size-3.5" aria-hidden />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        type="button"
        className=""
        onClick={() => {
          const target = activeFragmentId ?? cited.at(0);
          if (target !== undefined) jumpTo(target);
        }}
        title={t("preview.goToMatch")}
      >
        {t("preview.matchOf", { current: activeIdx >= 0 ? activeIdx + 1 : 1, total: cited.length })}
      </Button>
      {missingCited && (
        <span className="ml-auto text-caption text-warn">{t("preview.matchesOutside")}</span>
      )}
    </div>
  );

  return (
    <div className="break-words">
      {showHeader && (
        <div className="space-y-1.5 px-4 pt-1 pb-3">
          <h2 className="pr-6 text-base leading-snug font-semibold">{doc.title}</h2>
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FileTypeBadge type={doc.fileType} />
            <span className="truncate">{doc.fileName}</span>
            {relevance !== undefined && (
              <Badge variant="outline" className="font-mono text-ok">
                {t("preview.relevance", { value: relevance })}
              </Badge>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <Badge variant="secondary">{doc.section}</Badge>
            <span>{doc.sourceName}</span>
            {doc.updatedAt && (
              <span className="inline-flex items-center gap-1">
                <Clock className="size-3" aria-hidden />
                {formatDate(doc.updatedAt)}
              </span>
            )}
          </div>
        </div>
      )}
      <Separator />
      <div className="bg-muted px-4 py-4 pb-8">
        {fragmentNav}
        {loading ? <LoadingBody /> : error ? renderError() : renderBody()}
      </div>
    </div>
  );
}
