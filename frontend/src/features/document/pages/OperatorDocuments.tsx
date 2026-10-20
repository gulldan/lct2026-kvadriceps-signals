import { PageLayout } from "@/shared/ui/PageLayout";
import { Input } from "@/shared/ui/Input";
import { Card } from "@/shared/ui/Card";
import { useCallback, useEffect, useRef, useState } from "react";
import { Eye, RefreshCw, Upload, Wand2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router-dom";
import { toast } from "@/shared/ui/Sonner";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Spinner } from "@/shared/ui/Spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";
import { DocPreviewSheet } from "@/features/document/ui/DocPreviewSheet";
import { FileTypeBadge } from "@/shared/ui/FileTypeBadge";
import { PipelineStepper } from "@/features/document/ui/StatusBadge";
import { InfoHint } from "@/shared/ui/InfoHint";
import { ErrorState, PageHeader, Pagination, SearchField } from "@/shared/ui";
import { adminListDocuments } from "@/features/admin";
import {
  getDocument,
  getDocumentChunks,
  ApiDocument,
  IngestionEvent,
  UPLOAD_ACCEPT,
} from "@/features/document/api";
import { useIngestionEvents } from "@/features/document/useIngestionEvents";
import {
  docWithChunks,
  documentTitle,
  fileTypeOf,
  publishedLabel,
} from "@/features/document/docAdapter";
import { formatDateTime, humanSize } from "@/shared/lib/format";
import { uploadSmart, UploadCancelled, type UploadProgress } from "@/features/document/upload";
import { KbDoc } from "@/shared/types";

const DOCS_PAGE = 20;

const SEARCH_DEBOUNCE_MS = 300;

const TYPE_FILTERS = ["pdf", "docx", "xlsx", "pptx", "eml", "txt", "image"] as const;
const STATUS_FILTERS = ["all", "indexed", "processing", "failed"] as const;

type Phase = "loading" | "ready" | "error";

export function OperatorDocuments() {
  const { t } = useTranslation("document");
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const statusFilter = STATUS_FILTERS.find((value) => value === params.get("status")) ?? "all";
  const typeFilter = TYPE_FILTERS.find((value) => value === params.get("type")) ?? "all";
  const pagination = Number(params.get("page") || 1);
  const docsPage = Number.isSafeInteger(pagination) && pagination > 0 ? pagination : 1;

  const [docs, setDocs] = useState<ApiDocument[] | null>(null);
  const [total, setTotal] = useState(0);
  const [phase, setPhase] = useState<Phase>("loading");
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState(query);
  const [preview, setPreview] = useState<KbDoc | null>(null);
  const [previewLoadingId, setPreviewLoadingId] = useState<string | null>(null);
  const [uploading, setUploading] = useState(0);
  const [sessionStatus, setSessionStatus] = useState<Record<string, string>>({});
  const [dragOver, setDragOver] = useState(false);
  const dragDepth = useRef(0);
  const [stageProgress, setStageProgress] = useState<
    Record<string, { current: number; total: number }>
  >({});
  const [transfers, setTransfers] = useState<
    { id: string; name: string; fraction: number; totalBytes: number; cancel: () => void }[]
  >([]);
  const fileInput = useRef<HTMLInputElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const previewOpener = useRef<HTMLButtonElement | null>(null);

  // seq отбрасывает устаревшие ответы, loaded держит таблицу на экране при фоновых обновлениях.
  const requestSeq = useRef(0);
  const loaded = useRef(false);

  const update = useCallback(
    (key: "q" | "type" | "status" | "page", value: string, replace = false) =>
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          if (key !== "page") next.delete("page");
          if (value) next.set(key, value);
          else next.delete(key);
          return next;
        },
        { replace },
      ),
    [setParams],
  );

  const load = useCallback(async () => {
    const seq = ++requestSeq.current;
    if (!loaded.current) setPhase("loading");
    try {
      const page = await adminListDocuments({
        limit: DOCS_PAGE,
        offset: (docsPage - 1) * DOCS_PAGE,
        q: query.trim() || undefined,
        type: typeFilter === "all" ? undefined : typeFilter,
        status: statusFilter === "all" ? undefined : statusFilter,
      });
      if (seq !== requestSeq.current) return;
      const lastPage = Math.max(1, Math.ceil(page.total / DOCS_PAGE));
      if (docsPage > lastPage) {
        update("page", lastPage === 1 ? "" : String(lastPage), true);
        return;
      }
      setDocs(page.items);
      setTotal(page.total);
      loaded.current = true;
      setPhase("ready");
    } catch (e) {
      if (seq !== requestSeq.current) return;
      if (!loaded.current) setPhase("error");
      toast.error(e instanceof Error ? e.message : t("page.loadFailed"));
    }
  }, [docsPage, query, typeFilter, statusFilter, t, update]);

  useEffect(() => {
    void load();
    return () => {
      requestSeq.current += 1;
    };
  }, [load]);

  useEffect(() => setFilter(query), [query]);

  useEffect(() => {
    if (filter === query) return;
    const timer = setTimeout(() => update("q", filter, true), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [filter, query, update]);

  // Чужие загрузки по WebSocket не приходят — только перезапрос.
  const refresh = async () => {
    setRefreshing(true);
    try {
      await load();
    } finally {
      setRefreshing(false);
    }
  };

  const { live } = useIngestionEvents(
    useCallback((ev: IngestionEvent) => {
      setSessionStatus((prev) =>
        prev[ev.document_id] === undefined ? prev : { ...prev, [ev.document_id]: ev.status },
      );
      setDocs((prev) =>
        prev === null
          ? prev
          : prev.map((d) =>
              d.id === ev.document_id
                ? { ...d, status: ev.status, status_msg: ev.message, updated_at: ev.timestamp }
                : d,
            ),
      );
      setStageProgress((prev) => {
        const stageTotal = ev.stage_total ?? 0;
        if (stageTotal > 0) {
          return {
            ...prev,
            [ev.document_id]: { current: ev.stage_current ?? 0, total: stageTotal },
          };
        }
        if (prev[ev.document_id]) {
          const next = { ...prev };
          delete next[ev.document_id];
          return next;
        }
        return prev;
      });
      if (ev.status === "indexed" || ev.status === "failed") {
        getDocument(ev.document_id)
          .then((fresh) => {
            setDocs((prev) =>
              prev === null ? prev : prev.map((d) => (d.id === fresh.id ? fresh : d)),
            );
          })
          .catch(() => undefined);
      }
    }, []),
  );

  const noteUploaded = (doc: ApiDocument) => {
    setSessionStatus((prev) => ({ ...prev, [doc.id]: doc.status }));
    setDocs((prev) => (prev === null ? [doc] : [doc, ...prev]));
    setTotal((n) => n + 1);
    if (doc.duplicate) {
      toast.info(t("page.duplicate", { name: doc.filename }));
    }
  };

  const uploadOne = (file: File) => {
    setUploading((n) => n + 1);
    const controller = new AbortController();
    const id = `${file.name}-${file.size}-${file.lastModified}`;
    setTransfers((prev) => [
      { id, name: file.name, fraction: 0, totalBytes: file.size, cancel: () => controller.abort() },
      ...prev,
    ]);
    const onProgress = (p: UploadProgress) =>
      setTransfers((prev) =>
        prev.map((tr) => (tr.id === id ? { ...tr, fraction: p.fraction } : tr)),
      );
    uploadSmart(file, onProgress, controller.signal)
      .then(noteUploaded)
      .catch((e: unknown) => {
        if (e instanceof UploadCancelled) {
          toast(t("page.uploadCancelled", { name: file.name }));
        } else {
          toast.error(
            `«${file.name}»: ${e instanceof Error ? e.message : t("page.uploadFailedGeneric")}`,
          );
        }
      })
      .finally(() => {
        setTransfers((prev) => prev.filter((tr) => tr.id !== id));
        setUploading((n) => n - 1);
      });
  };

  const onFiles = (files: FileList | null) => {
    if (!files || files.length === 0) return;
    const picked = [...files];
    const archives = picked.filter((f) => /\.(zip|rar|7z|tar|gz|tgz)$/i.test(f.name)).length;
    toast(
      archives > 0
        ? t("page.uploadingBatchArchives", { count: picked.length })
        : t("page.uploadingBatch", { count: picked.length }),
    );
    for (const file of picked) uploadOne(file);
  };

  const openPreview = async (d: ApiDocument, opener: HTMLButtonElement) => {
    // Заблокированная на время загрузки кнопка теряет фокус до открытия панели.
    previewOpener.current = opener;
    setPreviewLoadingId(d.id);
    try {
      const { document, chunks } = await getDocumentChunks(d.id);
      setPreview(docWithChunks(document, chunks));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("page.openFailed"));
    } finally {
      setPreviewLoadingId(null);
    }
  };

  const visible = docs ?? [];
  const docsPageCount = Math.max(1, Math.ceil(total / DOCS_PAGE));
  const hasFilters = query.trim() !== "" || typeFilter !== "all" || statusFilter !== "all";

  const sessionIds = Object.keys(sessionStatus);
  const sessionDone = sessionIds.filter((id) =>
    ["indexed", "failed"].includes(sessionStatus[id] ?? ""),
  ).length;
  const sessionIndexed = sessionIds.filter((id) => sessionStatus[id] === "indexed").length;
  const sessionAllDone = sessionIds.length > 0 && sessionDone === sessionIds.length;

  return (
    <PageLayout
      onDragEnter={(e) => {
        if (![...e.dataTransfer.types].includes("Files")) return;
        e.preventDefault();
        dragDepth.current += 1;
        setDragOver(true);
      }}
      onDragOver={(e) => {
        if ([...e.dataTransfer.types].includes("Files")) e.preventDefault();
      }}
      onDragLeave={() => {
        dragDepth.current = Math.max(0, dragDepth.current - 1);
        if (dragDepth.current === 0) setDragOver(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        dragDepth.current = 0;
        setDragOver(false);
        onFiles(e.dataTransfer.files);
      }}
    >
      {dragOver && (
        <div className="pointer-events-none fixed inset-0 z-(--layer-overlay) flex items-center justify-center bg-background">
          <div className="rounded-2xl border-2 border-dashed border-brand bg-card px-10 py-8 text-center">
            <Upload className="mx-auto size-8 text-brand" aria-hidden />
            <p className="mt-3 text-lg font-medium">{t("page.dropHere")}</p>
            <p className="mt-1 text-sm text-muted-foreground">{t("page.dropHint")}</p>
          </div>
        </div>
      )}
      <PageHeader
        title={t("page.title")}
        description={t("page.description")}
        actions={
          <>
            {live && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <output className="relative mr-1 flex size-2" aria-label={t("page.liveAria")}>
                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-ok opacity-50" />
                    <span className="relative inline-flex size-2 rounded-full bg-ok" />
                  </output>
                </TooltipTrigger>
                <TooltipContent>{t("page.liveTooltip")}</TooltipContent>
              </Tooltip>
            )}
            <Button variant="outline" onClick={() => void refresh()} disabled={refreshing}>
              <RefreshCw className={`size-4 ${refreshing ? "animate-spin" : ""}`} aria-hidden />
              {t("actions.refresh", { ns: "common" })}
            </Button>
            <Button onClick={() => fileInput.current?.click()}>
              {uploading > 0 ? <Spinner /> : <Upload className="size-4" aria-hidden />}
              {t("page.upload")}
            </Button>
            <Input
              ref={fileInput}
              type="file"
              multiple
              accept={UPLOAD_ACCEPT}
              className="hidden"
              aria-label={t("page.chooseFiles")}
              onChange={(e) => {
                onFiles(e.target.files);
                e.target.value = "";
              }}
            />
          </>
        }
      />

      {sessionIds.length > 0 && (
        <div
          className={`mt-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3 ${
            sessionAllDone ? "border-transparent bg-ok-wash" : "bg-secondary"
          }`}
        >
          <div className="flex items-center gap-3">
            {sessionAllDone ? (
              <span className="text-sm font-medium text-ok">
                {t("page.sessionReady", { count: sessionIndexed })}
              </span>
            ) : (
              <>
                <Spinner />
                <span className="text-sm">
                  {t("page.sessionProgress", { done: sessionDone, total: sessionIds.length })}
                </span>
              </>
            )}
          </div>
          {sessionAllDone && sessionIndexed > 0 && (
            <Button asChild>
              <Link to="/hypotheses">
                <Wand2 className="size-4" aria-hidden />
                {t("page.sessionGenerate")}
              </Link>
            </Button>
          )}
        </div>
      )}

      <div className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchField
          ref={searchInput}
          className="flex-1"
          value={filter}
          onChange={setFilter}
          placeholder={t("page.searchPlaceholder")}
          ariaLabel={t("page.searchAria")}
        />
        <Select
          value={typeFilter}
          onValueChange={(value) => update("type", value === "all" ? "" : value)}
        >
          <SelectTrigger className="w-full sm:w-52" aria-label={t("page.typeFilterAria")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("page.allTypes")}</SelectItem>
            {TYPE_FILTERS.map((type) => (
              <SelectItem key={type} value={type}>
                {t(`page.typeFilters.${type}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={statusFilter}
          onValueChange={(value) => update("status", value === "all" ? "" : value)}
        >
          <SelectTrigger className="w-full sm:w-48" aria-label={t("page.statusFilterAria")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_FILTERS.map((status) => (
              <SelectItem key={status} value={status}>
                {t(`page.statusFilters.${status}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {/* Строка счёта держит место и при загрузке: иначе таблица съезжала вниз, когда приходил ответ. */}
      {phase !== "error" && (
        <p className="mt-3 min-h-4 text-xs text-muted-foreground">
          {phase === "ready" && t("page.libraryCount", { count: total })}
        </p>
      )}

      {transfers.length > 0 && (
        <div className="mt-4 space-y-2">
          {transfers.map((tr) => (
            <Card key={tr.id} className="rounded-lg px-4 py-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{tr.name}</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {t("page.transferProgress", {
                      percent: Math.round(tr.fraction * 100),
                      size: humanSize(tr.totalBytes),
                    })}
                  </p>
                </div>
                <span className="text-sm text-muted-foreground tabular-nums">
                  {Math.round(tr.fraction * 100)}%
                </span>
                <Button variant="outline" size="sm" onClick={tr.cancel}>
                  {t("page.cancelUpload")}
                </Button>
              </div>
              <div
                className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted"
                // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- стилизованный индикатор, native progress не стилизуется под дизайн
                role="progressbar"
                aria-valuenow={Math.round(tr.fraction * 100)}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label={t("page.uploadingAria", { name: tr.name })}
              >
                <div
                  className="duration-normal ease-standard h-full origin-left rounded-full bg-primary motion-safe:transition-transform"
                  style={{ transform: `scaleX(${tr.fraction})` }}
                />
              </div>
            </Card>
          ))}
        </div>
      )}

      {phase === "error" ? (
        <ErrorState message={t("page.loadErrorState")} onRetry={() => void load()} />
      ) : (
        <div className="@container/documents mt-4 border-y">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("page.columns.document")}</TableHead>
                <TableHead className="hidden @4xl/documents:table-cell">
                  {t("page.columns.size")}
                </TableHead>
                <TableHead className="hidden text-right @4xl/documents:table-cell">
                  <span className="inline-flex items-center gap-1">
                    {t("page.columns.chunks")}
                    <InfoHint>{t("page.chunksHint")}</InfoHint>
                  </span>
                </TableHead>
                <TableHead className="hidden @4xl/documents:table-cell">
                  {t("page.columns.updated")}
                </TableHead>
                <TableHead>{t("page.columns.status")}</TableHead>
                <TableHead className="w-16 text-right">{t("page.columns.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {phase === "loading" &&
                [0, 1, 2, 3, 4].map((i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <Skeleton className="h-4 w-48 max-w-full" />
                    </TableCell>
                    <TableCell className="hidden @4xl/documents:table-cell">
                      <Skeleton className="h-4 w-14" />
                    </TableCell>
                    <TableCell className="hidden @4xl/documents:table-cell">
                      <Skeleton className="ml-auto h-4 w-10" />
                    </TableCell>
                    <TableCell className="hidden @4xl/documents:table-cell">
                      <Skeleton className="h-4 w-28" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="h-5 w-20 rounded-full" />
                    </TableCell>
                    <TableCell>
                      <Skeleton className="ml-auto size-7 rounded-md" />
                    </TableCell>
                  </TableRow>
                ))}

              {phase === "ready" &&
                visible.map((d) => {
                  // Оригинал лежит в хранилище с момента загрузки: просмотр не ждёт индексации.
                  const opening = previewLoadingId === d.id;
                  return (
                    <TableRow key={d.id}>
                      <TableCell className="max-w-64 py-3 font-medium md:max-w-md">
                        <div className="flex min-w-0 flex-col">
                          <div className="flex min-w-0 items-center gap-2">
                            <Button
                              variant="ghost"
                              size="content"
                              className="min-w-0 p-0 text-left font-medium"
                              disabled={opening}
                              onClick={(event) => void openPreview(d, event.currentTarget)}
                            >
                              <span className="line-clamp-2 break-words" title={documentTitle(d)}>
                                {documentTitle(d)}
                              </span>
                            </Button>
                            <span className="shrink-0">
                              <FileTypeBadge type={fileTypeOf(d.filename, d.mime_type)} />
                            </span>
                            {d.kind === "hypotheses" && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Badge variant="warn" className="shrink-0">
                                    {t("page.hypothesesDoc.badge")}
                                  </Badge>
                                </TooltipTrigger>
                                <TooltipContent className="max-w-xs">
                                  {t("page.hypothesesDoc.hint")}
                                </TooltipContent>
                              </Tooltip>
                            )}
                          </div>
                          {d.title?.trim() && (
                            <span
                              className="truncate text-xs font-normal text-muted-foreground"
                              title={d.filename}
                            >
                              {d.filename}
                            </span>
                          )}
                          {(d.author?.trim() || d.published_at?.trim()) && (
                            <span className="truncate text-xs font-normal text-muted-foreground">
                              {[d.author?.trim(), publishedLabel(d.published_at)]
                                .filter(Boolean)
                                .join(" · ")}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground @4xl/documents:table-cell">
                        {humanSize(d.size)}
                      </TableCell>
                      <TableCell className="hidden text-right text-muted-foreground tabular-nums @4xl/documents:table-cell">
                        {d.chunk_count}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground @4xl/documents:table-cell">
                        {formatDateTime(d.updated_at)}
                      </TableCell>
                      <TableCell>
                        <PipelineStepper
                          status={d.status}
                          statusMsg={d.status_msg}
                          progress={stageProgress[d.id]}
                        />
                      </TableCell>
                      <TableCell className="text-right">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="inline-flex">
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={t("page.previewAria", { name: d.filename })}
                                disabled={opening}
                                onClick={(event) => void openPreview(d, event.currentTarget)}
                              >
                                {opening ? <Spinner /> : <Eye className="size-4" aria-hidden />}
                              </Button>
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>{t("page.preview")}</TooltipContent>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  );
                })}

              {phase === "ready" && visible.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="py-10 text-center text-sm text-muted-foreground"
                  >
                    {hasFilters ? t("page.emptyFiltered") : t("page.emptyNoDocs")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {phase === "ready" && visible.length > 0 && (
        <>
          <p className="mt-2 text-center text-xs text-muted-foreground">
            {t("page.pageRange", {
              from: (docsPage - 1) * DOCS_PAGE + 1,
              to: (docsPage - 1) * DOCS_PAGE + visible.length,
              count: total,
            })}
          </p>
          <Pagination
            className="mt-2"
            page={docsPage}
            pageCount={docsPageCount}
            onPage={(page) => update("page", page === 1 ? "" : String(page))}
          />
        </>
      )}

      <DocPreviewSheet
        doc={preview}
        onClose={() => setPreview(null)}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          const opener = previewOpener.current;
          (opener?.isConnected ? opener : searchInput.current)?.focus({ preventScroll: true });
        }}
      />
    </PageLayout>
  );
}
