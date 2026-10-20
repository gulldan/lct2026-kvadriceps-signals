import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowRight,
  Check,
  Factory,
  FilePlus,
  Lightbulb,
  Rocket,
  TrendingDown,
  TrendingUp,
  Wrench,
} from "lucide-react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card, CardButton } from "@/shared/ui/Card";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";
import { ResultGrid, ResultItem, ResultTile, ResultTileMedia } from "@/shared/ui/ResultCard";
import { Segmented } from "@/shared/ui/Segmented";
import { CounterStrip, type CounterItem } from "@/shared/ui/CounterStrip";
import { formatNumber, upperFirst } from "@/shared/lib/format";
import type { LiveDetail, LiveRejection, LiveTopic } from "../liveApi";
import { LiveLimits } from "./LiveLimits";
import { LiveCandidatesTable, RejectedTable } from "./LiveCandidatesTable";
import { liveTopicName } from "./LiveOverview";
import {
  candidateCountsAgree,
  LIVE_TOP_CANDIDATES,
  rejectionGroups,
  topStatus,
} from "../candidatesReport";
import {
  hasModelDecision,
  isConfirmedSignal,
  isUnchecked,
  notPassedNote,
  signalScore,
  signalVerdict,
} from "../signalScore";
import { growthLabel } from "../signalReport";
import { freshSourceCount, signalSources } from "../signalSources";
import { TechnologyCover } from "./TechnologyCover";

const REJECTED_PAGE = 12;

/** Вид хранит страница в адресе: компонент работает и вне маршрутизатора (каталог UI). */
export type CandidatesView = "cards" | "table";

export function LiveCandidates({
  detail,
  view,
  onView,
  onOpen,
  covers = true,
}: {
  detail: LiveDetail;
  view: CandidatesView;
  onView: (view: CandidatesView) => void;
  onOpen: (candidate: LiveTopic) => void;
  covers?: boolean;
}) {
  const { t } = useTranslation("trend");
  const candidates = detail.candidates ?? [];
  const top = topStatus(candidates);
  const rejected = rejectedRows(detail.rejected ?? [], t);
  // Непроверенные — не отказ по существу: их считаем и показываем отдельно, чтобы сумма сходилась.
  const judged = rejected.filter(({ row }) => !isUnchecked(row));
  const unchecked = rejected.filter(({ row }) => isUnchecked(row));
  const counters = detail.counters;
  const offTopic = (detail.topic.limits ?? []).some((limit) =>
    typeof limit === "string" ? false : limit.code === "off_topic",
  );
  const tally: CounterItem[] = counters
    ? [
        ...(detail.volume?.scanned
          ? [{ label: t("counters.volumeScanned"), value: formatNumber(detail.volume.scanned) }]
          : []),
        { label: t("counters.sources"), value: formatNumber(counters.sources_processed) },
        ...(counters.lowered_sources > 0
          ? [
              {
                label: t("signalList.tallyLowered"),
                value: formatNumber(counters.lowered_sources),
              },
            ]
          : []),
        { label: t("counters.candidates"), value: formatNumber(counters.candidates) },
        {
          label: t("counters.selected"),
          value: formatNumber(candidates.length),
          hint: counters.top_up
            ? t("signalList.topUpTally", { count: counters.top_up })
            : t("signalList.tallyHint"),
        },
        { label: t("counters.rejected"), value: formatNumber(judged.length) },
        ...(unchecked.length > 0
          ? [
              {
                label: t("counters.unchecked"),
                value: formatNumber(unchecked.length),
                hint: t("signalList.uncheckedHint"),
              },
            ]
          : []),
        // ТЗ просит число сигналов, в которых модель уверена больше чем на 75 %.
        counters.model_scored
          ? {
              label: t("counters.modelAbove"),
              value: formatNumber(counters.model_above_threshold ?? 0),
            }
          : { label: t("signalList.tallyAbove"), value: formatNumber(counters.above_threshold) },
      ]
    : [];
  return (
    <div className="min-w-0 space-y-6">
      <header className="space-y-4">
        {tally.length > 0 && <CounterStrip items={tally} />}
        {!candidateCountsAgree(detail) && (
          <Notice tone="warn">{t("signalList.countMismatch")}</Notice>
        )}
        {top.incomplete && (
          <Notice tone="warn">
            <p className="font-medium">
              {t("signalList.incompleteTitle", {
                limit: LIVE_TOP_CANDIDATES,
              })}
            </p>
            <p>
              {t("signalList.incompleteReason", {
                shown: candidates.length,
                count: top.confirmed,
                limit: LIVE_TOP_CANDIDATES,
              })}
            </p>
          </Notice>
        )}
        {top.toppedUp && (
          <p className="text-caption text-muted-foreground">
            {t("signalList.toppedUp", { count: top.confirmed, limit: LIVE_TOP_CANDIDATES })}
          </p>
        )}
        <LiveLimits limits={candidates.flatMap((row) => row.limits ?? [])} />
        {(candidates.length > 0 || rejected.length > 0) && (
          <Segmented
            aria-label={t("signalList.view")}
            value={view}
            onChange={onView}
            options={[
              { value: "cards", label: t("signalList.viewCards") },
              { value: "table", label: t("signalList.viewTable") },
            ]}
          />
        )}
      </header>

      {candidates.length === 0 ? (
        offTopic ? null : (
          <Notice tone="warn">{t("signalList.empty")}</Notice>
        )
      ) : view === "table" ? (
        <LiveCandidatesTable candidates={candidates} onOpen={onOpen} />
      ) : (
        <section className="space-y-3">
          <p className="text-caption text-muted-foreground">{t("tile.order")}</p>
          <ResultGrid ordered tiles>
            {candidates.map((candidate) => {
              const sources = signalSources(detail, candidate);
              return (
                <ResultItem key={candidate.id}>
                  <TechnologyTile
                    topic={candidate}
                    sources={sources.length}
                    fresh={freshSourceCount(sources, detail.observed_at)}
                    covers={covers}
                    onOpen={() => onOpen(candidate)}
                  />
                </ResultItem>
              );
            })}
          </ResultGrid>
        </section>
      )}

      {judged.length > 0 && (
        <RejectedList
          rows={judged}
          view={view}
          title={t("signalList.rejectedSection", { count: judged.length })}
        />
      )}
      {unchecked.length > 0 && (
        <RejectedList
          rows={unchecked}
          view={view}
          title={t("signalList.uncheckedSection", { count: unchecked.length })}
          note={t("signalList.uncheckedHint")}
        />
      )}
    </div>
  );
}

function RejectedList({
  rows,
  view,
  title,
  note,
}: {
  rows: ReturnType<typeof rejectedRows>;
  view: CandidatesView;
  title: string;
  note?: string;
}) {
  const { t } = useTranslation("trend");
  const [shown, setShown] = useState(REJECTED_PAGE);
  return (
    <Disclosure lazy title={title}>
      <div className="space-y-4">
        {note && <p className="text-caption text-muted-foreground">{note}</p>}
        {view === "table" ? (
          <RejectedTable caption={title} rows={rows.slice(0, shown)} />
        ) : (
          <ResultGrid tiles>
            {rows.slice(0, shown).map(({ row, name }) => (
              <ResultItem key={`${row.code}:${row.original_name ?? row.name}`}>
                <RejectedTile rejection={row} name={name} />
              </ResultItem>
            ))}
          </ResultGrid>
        )}
        {rows.length > shown && (
          <Button variant="outline" onClick={() => setShown((count) => count + REJECTED_PAGE)}>
            {t("catalogue.more")}
          </Button>
        )}
      </div>
    </Disclosure>
  );
}

/** У исключённых без русского названия общая заглушка, поэтому их называет фрагмент источника. */
function rejectedRows(rows: LiveRejection[], t: Parameters<typeof liveTopicName>[1]) {
  const counts = new Map<string, number>();
  for (const row of rows) counts.set(row.name, (counts.get(row.name) ?? 0) + 1);
  const generic = (row: LiveRejection) => (counts.get(row.name) ?? 0) > 1;
  return rejectionGroups(rows)
    .flatMap((group) => group.items)
    .toSorted(
      (a, b) =>
        (b.model_value ?? -1) - (a.model_value ?? -1) || Number(generic(a)) - Number(generic(b)),
    )
    .map((row) => {
      const name = (generic(row) && row.original_name) || row.name;
      return { row, name: liveTopicName({ id: "", name, label: name }, t) };
    });
}

const STAGE_ICON = { 1: Lightbulb, 2: Wrench, 3: Rocket, 4: Factory } as const;

const GROWTH = {
  up: { tone: "brand-solid", icon: TrendingUp },
  flat: { tone: "muted-solid", icon: ArrowRight },
  down: { tone: "warn-solid", icon: TrendingDown },
} as const;

function TechnologyTile({
  topic,
  sources,
  fresh,
  covers,
  onOpen,
}: {
  topic: LiveTopic;
  sources: number;
  fresh: number;
  covers: boolean;
  onOpen: () => void;
}) {
  const { t } = useTranslation("trend");
  const name = liveTopicName(topic, t);
  const score = signalScore(topic);
  const verdict = signalVerdict(topic, t);
  const growth = growthLabel(topic, t);
  const stage = (topic.tz?.stage_code ?? 0) as 0 | 1 | 2 | 3 | 4;
  const StageIcon = stage ? STAGE_ICON[stage] : null;
  const Growth = growth ? GROWTH[growth.trend] : null;
  const year = topic.first_year?.slice(0, 4);
  return (
    <CardButton onClick={onOpen} className="flex flex-1 flex-col">
      <ResultTile
        media={
          <ResultTileMedia
            topLeft={<Badge variant={verdict.tone}>{verdict.text}</Badge>}
            topRight={
              score && !score.gap ? (
                <Badge variant="media">{t("confidence.value", { percent: score.percent })}</Badge>
              ) : null
            }
            bottomLeft={
              <>
                {Growth && growth && (
                  <Badge variant={Growth.tone} className="gap-1">
                    <Growth.icon className="size-3 shrink-0" aria-hidden />
                    {growth.label}
                  </Badge>
                )}
                {fresh > 0 && (
                  <Badge variant="media" className="gap-1" title={t("tile.freshHint")}>
                    <FilePlus className="size-3 shrink-0" aria-hidden />
                    {t("tile.fresh", { count: fresh })}
                  </Badge>
                )}
                {StageIcon && (
                  <Badge variant={stage >= 3 ? "ok-solid" : "media"} className="gap-1">
                    <StageIcon className="size-3 shrink-0" aria-hidden />
                    {t(`tile.stage.${stage as 1 | 2 | 3 | 4}`)}
                  </Badge>
                )}
              </>
            }
          >
            <TechnologyCover topic={topic} alt={name} offline={!covers} />
          </ResultTileMedia>
        }
        note={
          topic.shortfall ? (
            <span className="text-warn">{notPassedNote(topic.shortfall, sources, t)}</span>
          ) : score?.gap ? (
            <span className="text-muted-foreground">
              {t("report.modelClass.insufficient_data")}
            </span>
          ) : score?.model && !hasModelDecision(topic) ? (
            <span className="text-muted-foreground">{verdict.text}</span>
          ) : score?.passed && !isConfirmedSignal(topic) ? (
            <span className="text-warn">{t("tile.needSecond")}</span>
          ) : (
            <span className="inline-flex items-center gap-1 text-ok">
              <Check className="size-3 shrink-0" aria-hidden />
              {t("tile.passed")}
            </span>
          )
        }
        title={name}
        meta={
          year
            ? t("tile.factsSince", { count: sources, year })
            : t("tile.sources", { count: sources })
        }
        action={t("tile.open")}
      />
    </CardButton>
  );
}

function RejectedTile({ rejection, name }: { rejection: LiveRejection; name: string }) {
  const { t } = useTranslation("trend");
  const percent =
    rejection.model_value === undefined ? null : Math.round(rejection.model_value * 100);
  return (
    <Card className="flex min-w-0 flex-1 flex-col">
      <ResultTile
        headline={
          <Badge variant="warn-solid" className="max-w-full truncate font-semibold">
            {t(`signalList.reject.${rejection.code}`, { defaultValue: rejection.code })}
          </Badge>
        }
        note={
          <span className="font-normal text-muted-foreground">
            {percent === null ? t("tile.noModelScore") : t("tile.similarityOf", { percent })}
          </span>
        }
        title={name}
        meta={upperFirst(rejection.reason)}
      />
    </Card>
  );
}
