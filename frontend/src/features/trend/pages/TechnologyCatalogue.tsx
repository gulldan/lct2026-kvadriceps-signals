import { useEffect, useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Layers } from "lucide-react";

import { ApiError } from "@/shared/api/client";
import { describeError } from "@/shared/api/errors";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { ListSkeleton } from "@/shared/ui/ListSkeleton";
import { Notice } from "@/shared/ui/Notice";
import { FilterBar } from "@/shared/ui/FilterBar";
import { searchTechnologies, type TechnologyPage } from "../catalogueApi";
import { technologyPath } from "../technologyApi";
import { ROLES } from "../technologyLabels";
import { FieldRow } from "@/shared/ui/Fields";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { ReportExport, serverFiles } from "@/shared/report/ReportExport";
import { TrendSearch } from "../ui/TrendSearch";

const PAGE = 20;
const ANY = "any";
const LANGUAGES = ["ru", "en", "zh"] as const;

export function TechnologyCatalogue() {
  const { t } = useTranslation("trend");
  const { t: tCommon } = useTranslation("common");
  const [params, setParams] = useSearchParams();
  const query = params.get("q") || "";
  const cursor = params.get("cursor") || "";
  const showAll = params.get("all") === "true";
  const language = params.get("language") || "";
  const role = params.get("role") || "";
  const [draft, setDraft] = useState(query);
  const [page, setPage] = useState<TechnologyPage | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => setDraft(query), [query]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setPage(null);
    setError(null);
    searchTechnologies(
      {
        q: query,
        cursor,
        limit: PAGE,
        all: showAll,
        languages: language ? [language] : [],
        roles: role ? [role] : [],
      },
      controller.signal,
    )
      .then((result) => {
        if (!controller.signal.aborted) setPage(result);
      })
      .catch((failure: unknown) => {
        if (controller.signal.aborted) return;
        setPage(null);
        setError(failure instanceof ApiError ? failure : new ApiError(0, String(failure)));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [query, cursor, showAll, language, role]);

  const update = (next: {
    q?: string;
    cursor?: string;
    all?: boolean;
    language?: string;
    role?: string;
  }) =>
    setParams((current) => {
      const updated = new URLSearchParams(current);
      // Курсор привязан к версии каталога, что его выдала.
      if (next.q !== undefined) {
        updated.delete("cursor");
        if (next.q) updated.set("q", next.q);
        else updated.delete("q");
      }
      if (next.all !== undefined) {
        updated.delete("cursor");
        if (next.all) updated.set("all", "true");
        else updated.delete("all");
      }
      if (next.cursor !== undefined) {
        if (next.cursor) updated.set("cursor", next.cursor);
        else updated.delete("cursor");
      }
      for (const [name, value] of [
        ["language", next.language],
        ["role", next.role],
      ] as const) {
        if (value === undefined) continue;
        updated.delete("cursor");
        if (value) updated.set(name, value);
        else updated.delete(name);
      }
      return updated;
    });

  return (
    <>
      <TrendSearch
        value={draft}
        onChange={setDraft}
        onSearch={(q) => update({ q })}
        allowEmpty={!!query}
        placeholder={t("catalogue.searchPlaceholder")}
      />
      <FilterBar className="gap-3">
        <FieldRow id="catalogue-language" label={t("catalogue.filters.language")}>
          <Select
            value={language || ANY}
            onValueChange={(value) => update({ language: value === ANY ? "" : value })}
          >
            <SelectTrigger id="catalogue-language" className="w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>{t("catalogue.filters.anyLanguage")}</SelectItem>
              {LANGUAGES.map((code) => (
                <SelectItem key={code} value={code}>
                  {t(`catalogue.languages.${code}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
        <FieldRow id="catalogue-role" label={t("catalogue.filters.role")}>
          <Select
            value={role || ANY}
            onValueChange={(value) => update({ role: value === ANY ? "" : value })}
          >
            <SelectTrigger id="catalogue-role" className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>{t("catalogue.filters.anyRole")}</SelectItem>
              {ROLES.map((item) => (
                <SelectItem key={item} value={item}>
                  {t(`technology.roles.${item}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
        {(language || role) && (
          <Button variant="outline" onClick={() => update({ language: "", role: "" })}>
            {t("catalogue.filters.reset")}
          </Button>
        )}
        {page && !page.reason && page.technologies.length > 0 && (
          <ReportExport
            items={serverFiles("/technologies/export", "technologies", {
              q: query,
              language,
              role,
            })}
          />
        )}
      </FilterBar>
      {error && <ErrorState message={describeError(error, tCommon)} variant="compact" />}
      {loading && !page && <ListSkeleton rows={5} />}
      {page && <CatalogueBody page={page} filtered={!!(language || role)} onUpdate={update} />}
    </>
  );
}

function CatalogueBody({
  page,
  filtered,
  onUpdate,
}: {
  page: TechnologyPage;
  filtered: boolean;
  onUpdate: (next: {
    q?: string;
    cursor?: string;
    all?: boolean;
    language?: string;
    role?: string;
  }) => void;
}) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const returnTo = location.pathname + location.search;
  if (page.reason === "catalogue_empty")
    return (
      <EmptyState
        icon={Layers}
        title={t("catalogue.emptyTitle")}
        description={t("catalogue.emptyBody")}
      />
    );
  if (page.reason === "query_required")
    return (
      <Notice tone="muted">
        <p>{t("catalogue.queryRequired")}</p>
        <Button className="mt-3" variant="outline" onClick={() => onUpdate({ all: true })}>
          {t("catalogue.showAll")}
        </Button>
      </Notice>
    );
  if (page.reason === "below_relevance_floor")
    return filtered ? (
      <Notice>
        <p>{t("catalogue.filters.emptyWithFilters")}</p>
        <Button
          className="mt-3"
          variant="outline"
          onClick={() => onUpdate({ language: "", role: "" })}
        >
          {t("catalogue.filters.reset")}
        </Button>
      </Notice>
    ) : (
      <Notice>{t("catalogue.belowFloor")}</Notice>
    );
  return (
    <div className="space-y-4">
      <ul className="divide-y">
        {page.technologies.map((hit) => (
          <li key={hit.technology_id} className="space-y-1 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="link" size="inline" asChild className="font-medium">
                <Link to={technologyPath(hit.technology_id, returnTo)}>{hit.canonical_name}</Link>
              </Button>
              {hit.match_reasons.map((reason) => (
                <Badge key={reason} variant="outline">
                  {t(`catalogue.match.${reason}`)}
                </Badge>
              ))}
            </div>
            {hit.description && <p className="text-sm text-muted-foreground">{hit.description}</p>}
            {hit.matched_text && (
              <p className="text-sm">
                <mark>{hit.matched_text}</mark>
              </p>
            )}
          </li>
        ))}
      </ul>
      {page.next_cursor && (
        <Button variant="outline" onClick={() => onUpdate({ cursor: page.next_cursor })}>
          {t("catalogue.more")}
        </Button>
      )}
    </div>
  );
}
