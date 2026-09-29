import { PageLayout } from "@/shared/ui/PageLayout";
import { Button } from "@/shared/ui/Button";
import { useCallback, useEffect, useState } from "react";
import { FilterBar } from "@/shared/ui/FilterBar";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { History as HistoryIcon } from "lucide-react";
import {
  Badge,
  EmptyState,
  ErrorState,
  ListSkeleton,
  PageHeader,
  Pagination,
  SearchField,
  Segmented,
} from "@/shared/ui";
import { listChats, ApiChat } from "@/features/search/api";
import { useAuth } from "@/features/auth";
import { formatDateTime, formatNumber } from "@/shared/lib/format";
import { useLiveSearches } from "@/features/trend";

type Phase = "loading" | "ready" | "error";
type Scope = "mine" | "all";

const PAGE = 20;

const SOURCE_KEYS = {
  search: "history.source.search",
  trends: "history.source.trends",
} as const;

interface Entry {
  key: string;
  title: string;
  at: string;
  meta?: string;
  source: keyof typeof SOURCE_KEYS;
  open: () => void;
}

export function UserHistory() {
  const { t } = useTranslation("search");
  const navigate = useNavigate();
  const { roles } = useAuth();
  const isAdmin = roles.includes("admin");

  const [scope, setScope] = useState<Scope>("mine");
  const [page, setPage] = useState(1);
  const [chats, setChats] = useState<ApiChat[]>([]);
  const [total, setTotal] = useState(0);
  const [phase, setPhase] = useState<Phase>("loading");
  const [filter, setFilter] = useState("");
  const searches = useLiveSearches();

  const load = useCallback(async (nextPage: number, nextScope: Scope) => {
    setPhase("loading");
    try {
      const result = await listChats({
        limit: PAGE,
        offset: (nextPage - 1) * PAGE,
        all: nextScope === "all",
      });
      setChats(result.items);
      setTotal(result.total);
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, []);

  useEffect(() => {
    void load(page, scope);
  }, [load, page, scope]);

  const pageCount = Math.max(1, Math.ceil(total / PAGE));
  const entries: Entry[] = [
    ...(page === 1
      ? searches.map((search) => ({
          key: `live:${search.query}`,
          title: search.query,
          at: search.observed_at ?? "",
          meta: [
            t("history.live.meta", {
              sources: formatNumber(search.sources),
              papers: formatNumber(search.papers),
            }),
            search.fresh ? "" : t("history.live.stale"),
          ]
            .filter(Boolean)
            .join(" · "),
          source: "trends" as const,
          open: () => navigate(`/trends?q=${encodeURIComponent(search.query)}`),
        }))
      : []),
    ...chats.map((chat) => ({
      key: chat.id,
      title: chat.title,
      at: chat.created_at,
      meta: chat.owner_username ? t("history.byUser", { name: chat.owner_username }) : undefined,
      source: (chat.source && chat.source in SOURCE_KEYS
        ? chat.source
        : "search") as keyof typeof SOURCE_KEYS,
      // Без повторного запроса к модели: тот же вопрос мог бы дать другой ответ.
      open: () => navigate(`/search?chat=${encodeURIComponent(chat.id)}`),
    })),
  ].toSorted((a, b) => b.at.localeCompare(a.at));
  const visible = entries.filter((entry) =>
    entry.title.toLowerCase().includes(filter.toLowerCase()),
  );

  return (
    <PageLayout>
      <PageHeader title={t("history.title")} description={t("history.description")} />

      <FilterBar>
        <SearchField
          className="min-w-56 flex-1"
          value={filter}
          onChange={setFilter}
          placeholder={t("history.filterPlaceholder")}
          ariaLabel={t("history.filterAriaLabel")}
        />
        {isAdmin && (
          <Segmented
            aria-label={t("history.scopeAriaLabel")}
            value={scope}
            onChange={(v) => {
              setScope(v);
              setPage(1);
            }}
            options={[
              { value: "mine", label: t("history.scopeMine") },
              { value: "all", label: t("history.scopeAll") },
            ]}
          />
        )}
      </FilterBar>

      {phase === "loading" && <ListSkeleton rows={4} ariaLabel={t("history.loadingAriaLabel")} />}

      {phase === "error" && (
        <ErrorState message={t("history.loadError")} onRetry={() => void load(page, scope)} />
      )}

      {phase === "ready" &&
        (visible.length === 0 ? (
          <EmptyState
            icon={HistoryIcon}
            title={entries.length === 0 ? t("history.emptyTitle") : t("history.notFoundTitle")}
            description={
              entries.length === 0
                ? t("history.emptyDescription")
                : t("history.notFoundDescription")
            }
          />
        ) : (
          <>
            <p className="mt-4 text-sm text-muted-foreground">
              {filter.trim() === ""
                ? t("history.pageInfo", {
                    count: total,
                    total: formatNumber(total),
                    page,
                    pageCount,
                  })
                : t("history.foundOnPage", { count: visible.length })}
            </p>
            <ul className="mt-3 divide-y border-y">
              {visible.map((entry) => (
                <li key={entry.key}>
                  <Button
                    variant="ghost"
                    size="row"
                    type="button"
                    aria-label={t("history.openAriaLabel", { title: entry.title })}
                    onClick={entry.open}
                    className="gap-3"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="line-clamp-2 text-sm font-medium break-words">{entry.title}</p>
                      <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                        <span>{formatDateTime(entry.at)}</span>
                        {entry.meta && (
                          <>
                            <span aria-hidden>·</span>
                            <span>{entry.meta}</span>
                          </>
                        )}
                      </p>
                    </div>
                    <Badge variant="secondary" className="shrink-0">
                      {t(SOURCE_KEYS[entry.source])}
                    </Badge>
                  </Button>
                </li>
              ))}
            </ul>
          </>
        ))}
      {phase === "ready" && (
        <Pagination className="mt-5" page={page} pageCount={pageCount} onPage={setPage} />
      )}
    </PageLayout>
  );
}
