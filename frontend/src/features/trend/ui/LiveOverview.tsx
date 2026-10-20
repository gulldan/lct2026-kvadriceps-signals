import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, Radar } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { EmptyState } from "@/shared/ui/EmptyState";
import { Notice } from "@/shared/ui/Notice";
import { FieldRow } from "@/shared/ui/Fields";
import { FilterBar } from "@/shared/ui/FilterBar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";
import { TrendSearch } from "./TrendSearch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Spinner } from "@/shared/ui/Spinner";
import { formatDateTime, formatNumber, upperFirst } from "@/shared/lib/format";
import { LiveStale } from "./LiveStale";
import { liveFailureReason } from "../liveCoverage";
import type { LiveOverview, LiveTopic } from "../liveApi";
import {
  researchCategoryLabel,
  researchTaxonomy,
  sortedResearchCategories,
} from "@/shared/lib/researchTaxonomy";
import { spellAcronyms } from "../signalSources";
import { LiveMap } from "./LiveMap";
import { LiveTable } from "./LiveTable";
import { IndustrySelect } from "./IndustrySelect";

const STAGES = ["emerging_signal", "early_signal", "established", "unassessed"] as const;
type Stage = (typeof STAGES)[number];
const WEAK: readonly Stage[] = ["emerging_signal", "early_signal"];
const FILTERS = ["all", "weak", ...STAGES] as const;
type StageFilter = (typeof FILTERS)[number];

function stageMatches(filter: StageFilter, stage: string) {
  if (filter === "all") return true;
  if (filter === "weak") return (WEAK as readonly string[]).includes(stage);
  return stage === filter;
}

// Совпадают с livePinnedSearches в trend_live_store.go (с точностью до регистра): эти ответы
// пересобираются после каждого снимка первыми.
const QUICK_STARTS = [
  { key: "medicine", query: "слабые сигналы в медицине и диагностике" },
  { key: "agriculture", query: "слабые сигналы в сельском хозяйстве и продовольствии" },
  { key: "energy", query: "слабые сигналы в энергетике и хранении энергии" },
  { key: "climate", query: "слабые сигналы в климате и экологии" },
  { key: "materials", query: "слабые сигналы в новых материалах и химии" },
  { key: "biotech", query: "слабые сигналы в биотехнологиях и разработке лекарств" },
  { key: "economics", query: "слабые сигналы в экономике и социальных науках" },
  { key: "space", query: "слабые сигналы в космосе и астрофизике" },
  { key: "robotics", query: "слабые сигналы в робототехнике" },
  { key: "aiSecurity", query: "слабые сигналы в безопасности систем искусственного интеллекта" },
] as const;
const DIRECTION_PREFIX = "direction:";

export function liveTopicName(
  topic: Pick<LiveTopic, "id" | "name" | "label">,
  t: (key: string, options: { defaultValue: string }) => string,
) {
  // Кандидат-направление несёт английское имя карты: русское из словаря направлений важнее ярлыка.
  const direction = topic.id.startsWith(DIRECTION_PREFIX)
    ? topic.id.slice(DIRECTION_PREFIX.length)
    : "";
  const known = direction && t(`study.live.topics.${direction}`, { defaultValue: "" });
  const name =
    known || topic.label || t(`study.live.topics.${topic.id}`, { defaultValue: topic.name });
  const spelled = spellAcronyms(name);
  return upperFirst(spelled);
}

const format = (n: number, digits = 1) => formatNumber(n, { maximumFractionDigits: digits });

export function LiveOverviewView({
  overview,
  failed,
  onRetry,
  onOpen,
  onSearch,
}: {
  overview?: LiveOverview;
  failed: boolean;
  onRetry: () => void;
  onOpen: (id: string) => void;
  onSearch: (query: string) => void;
}) {
  const { t, i18n } = useTranslation("trend");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<StageFilter>("weak");
  const [industry, setIndustry] = useState("all");
  const name = useCallback((topic: LiveTopic) => liveTopicName(topic, t), [t]);
  const topics = useMemo(() => overview?.topics ?? [], [overview]);
  const complete = overview?.periods.filter((period) => period.complete).length ?? 0;
  // Иначе каждая набранная в поле буква пересчитывала отбор, счётчики стадий, карту и таблицу.
  // Отрасль, а не научная область: инвестор ищет, куда идут деньги, а не где публикуются статьи.
  const industries = useMemo(
    () =>
      sortedResearchCategories(
        researchTaxonomy.categories.filter((category) =>
          topics.some((topic) => topic.industry === category.id),
        ),
        i18n.language,
      ),
    [topics, i18n.language],
  );
  const industryTopics = useMemo(
    () => topics.filter((topic) => industry === "all" || topic.industry === industry),
    [topics, industry],
  );
  const visible = useMemo(
    () => industryTopics.filter((topic) => stageMatches(filter, topic.stage)),
    [industryTopics, filter],
  );
  const counts = useMemo(
    () =>
      new Map(
        FILTERS.map((value) => [
          value,
          industryTopics.filter((topic) => stageMatches(value, topic.stage)).length,
        ]),
      ),
    [industryTopics],
  );
  const ready = overview?.status === "ready" && topics.length > 0;
  // Карточки в снимке у запроса оператора нет: он открывается своим сохранённым ответом.
  const open = useCallback(
    (id: string) => {
      const topic = topics.find((row) => row.id === id);
      if (topic?.source === "search") onSearch(topic.name);
      else onOpen(id);
    },
    [topics, onSearch, onOpen],
  );
  // Открытый запрос собирает корпус сам, поэтому кнопка не ждёт готовности снимка.
  const collecting = overview?.status === "collecting";
  const stageLabel = (value: StageFilter) =>
    value === "weak"
      ? t("study.live.weakSignals")
      : value === "all"
        ? t("study.live.all")
        : t(`study.live.table.stages.${value}`);
  return (
    <div className="space-y-3">
      <TrendSearch
        value={query}
        onChange={setQuery}
        onSearch={onSearch}
        placeholder={t("study.live.searchPlaceholder")}
        label={t("study.live.searchLabel")}
        disabled={collecting}
      />
      <FilterBar className="gap-3">
        {ready && (
          <FieldRow id="live-stage" label={t("study.live.filterLabel")}>
            <Select value={filter} onValueChange={(value) => setFilter(value as StageFilter)}>
              <SelectTrigger id="live-stage" className="w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {FILTERS.map((value) => (
                  <SelectItem key={value} value={value}>
                    {stageLabel(value)} · {counts.get(value) ?? 0}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>
        )}
        {ready && industries.length > 1 && (
          <FieldRow id="live-industry" label={t("study.live.industryLabel")}>
            <Select value={industry} onValueChange={setIndustry}>
              <SelectTrigger id="live-industry" className="w-56 max-w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">
                  {t("study.live.industryAll")} ·{" "}
                  {topics.filter((topic) => stageMatches(filter, topic.stage)).length}
                </SelectItem>
                {/* Число — направления выбранной стадии: видно, в каких отраслях уже есть слабые сигналы. */}
                {industries.map((category) => (
                  <SelectItem key={category.id} value={category.id}>
                    {researchCategoryLabel(category, i18n.language)} ·{" "}
                    {
                      topics.filter(
                        (topic) =>
                          topic.industry === category.id && stageMatches(filter, topic.stage),
                      ).length
                    }
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline">
              {t("study.live.quickQueries")}
              <ChevronDown aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {QUICK_STARTS.map(({ key, query: phrase }) => (
              <DropdownMenuItem key={key} onSelect={() => onSearch(phrase)}>
                {t(`study.live.quickStarts.${key}`)}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <IndustrySelect compact disabled={collecting} onChoose={onSearch} />
      </FilterBar>
      {failed && (
        <Notice tone="risk">
          {t("study.error")}{" "}
          <Button variant="link" size="inline" onClick={onRetry}>
            {t("retry")}
          </Button>
        </Notice>
      )}
      {overview && overview.status === "failed" && !ready && (
        <Notice tone="risk">
          {t("study.live.failedTitle")}:{" "}
          {t(`study.live.collection.reason.${liveFailureReason(overview.error_code)}`)}
        </Notice>
      )}
      {overview && overview.status === "collecting" && !ready && (
        <EmptyState
          icon={Radar}
          title={t("study.live.collectingTitle")}
          description={t("study.live.collectingBody")}
          action={<Spinner />}
        />
      )}
      {overview && ready && (
        <>
          <LiveStale overview={overview} />
          {visible.length === 0 ? (
            <Notice>{t("study.live.emptyFilter")}</Notice>
          ) : (
            <>
              <LiveMap
                topics={visible}
                allTopics={topics}
                periods={overview.periods}
                observedAt={overview.observed_at}
                name={name}
                format={format}
                onOpen={open}
              />
              <Disclosure
                lazy
                summaryClassName="py-2"
                title={
                  <span className="flex flex-1 flex-wrap items-center justify-between gap-x-4 gap-y-1">
                    <span>{t("study.live.tableTitle", { count: visible.length })}</span>
                    <span className="text-xs font-normal text-muted-foreground">
                      {t("study.live.observed", { date: formatDateTime(overview.observed_at) })}
                      {overview.refreshing && <> · {t("study.live.refreshing")}…</>}
                    </span>
                  </span>
                }
              >
                <LiveTable
                  topics={visible}
                  complete={complete}
                  name={name}
                  format={format}
                  onOpen={open}
                />
              </Disclosure>
            </>
          )}
          {/* Красное — только за возраст снимка (LiveStale): незавершённый сбор уже переделывается. */}
          {overview.collection?.error &&
            (overview.refreshing ? (
              <Notice>{t("study.live.collection.rerunning")}</Notice>
            ) : liveFailureReason(overview.collection.error_code) === "canceled" ? (
              <Notice>{t("study.live.collection.reason.canceled")}</Notice>
            ) : (
              <Notice tone="risk">{t("study.live.collection.stale")}</Notice>
            ))}
        </>
      )}
    </div>
  );
}
