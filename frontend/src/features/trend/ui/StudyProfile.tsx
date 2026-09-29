import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { Badge } from "@/shared/ui/Badge";
import { DateStrip } from "@/shared/ui/DateStrip";
import { InfoHint } from "@/shared/ui/InfoHint";
import { SignalDial } from "@/shared/ui/SignalDial";
import { formatMonth, formatNumber, formatPercent } from "@/shared/lib/format";
import type { StudySignals, StudyTopic, TrendStudy } from "../studyApi";

type Tone = "ok" | "warn" | "muted";

const TONES: { [K in keyof StudySignals]: Partial<Record<StudySignals[K], Tone>> } = {
  novelty: { new: "ok", recent: "ok" },
  momentum: { up: "ok", down: "warn" },
  coherence: { screened: "ok", titles: "warn", unscreened: "warn", query: "warn" },
  spread: { broad: "ok", several: "ok", single: "warn", none: "warn" },
  application: { releases: "ok", implementations: "ok" },
  concentration: { low: "ok", medium: "warn", high: "warn" },
};

const ORDER = [
  "novelty",
  "momentum",
  "coherence",
  "spread",
  "application",
  "concentration",
] as const;

type ProfileStudy = Pick<TrendStudy, "papers" | "observed_at">;
type ProfileTopic = Omit<StudyTopic, "id" | "screening"> & { screening: StudyTopic["screening"] };

export function StudyDates({ study, topic }: { study: ProfileStudy; topic: ProfileTopic }) {
  const { t } = useTranslation("trend");
  const papers = study.papers.filter((paper) => topic.paper_ids.includes(paper.id));
  const first = papers.reduce<string>((min, p) => (!min || p.date < min ? p.date : min), "");
  return (
    <DateStrip
      columns={4}
      items={[
        { label: t("study.dates.first"), date: first || undefined },
        {
          label: t("study.dates.external"),
          date: topic.earliest_external || undefined,
          empty: t("study.dates.none"),
        },
        {
          label: t("study.dates.growth"),
          date: topic.growth_start || undefined,
          empty: t("study.dates.none"),
        },
        { label: t("study.dates.detected"), date: study.observed_at },
      ]}
    />
  );
}

export interface ProfileExtra {
  key: string;
  label: string;
  value: string;
  level: string;
  tone: Tone;
  basis: string;
}

const NO_EXTRA: ProfileExtra[] = [];

export function useProfileRows({
  study,
  topic,
  compact = false,
  complete = topic.series.length,
  extra = NO_EXTRA,
}: {
  study: ProfileStudy;
  topic: ProfileTopic;
  compact?: boolean;
  complete?: number;
  extra?: ProfileExtra[];
}): ProfileExtra[] {
  const { t } = useTranslation("trend");
  const papers = study.papers.filter((paper) => topic.paper_ids.includes(paper.id));
  const first = papers.reduce<string>((min, p) => (!min || p.date < min ? p.date : min), "");
  const last = topic.series[complete - 1];
  const growth = topic.growth;
  const releases = topic.external.filter((e) => e.kind === "release").length;
  const implementations = topic.external.filter((e) => e.kind === "implementation").length;
  const repositoryCandidates = topic.screening === "query";
  const communities = new Set(
    topic.external.filter((e) => e.community !== "academia").map((e) => e.community),
  ).size;
  const values: Record<(typeof ORDER)[number], string> = {
    novelty:
      formatMonth(topic.first_year) +
      (topic.earliest_external && topic.earliest_external < first
        ? ` · ${t("study.profile.externalEarlier", { year: formatMonth(topic.earliest_external) })}`
        : ""),
    momentum:
      growth.delta_pp === null
        ? "—"
        : `${growth.delta_pp > 0 ? "+" : ""}${formatNumber(growth.delta_pp === 0 ? 0 : growth.delta_pp, { maximumSignificantDigits: 2 })} ${t("study.pp")}`,
    coherence: t("study.profile.coherenceValue", { count: topic.paper_ids.length }),
    spread: last
      ? t(compact ? "study.profile.spreadShort" : "study.profile.spreadValue", {
          groups: last.groups,
          fresh: last.new_groups,
          year: formatMonth(last.period),
        })
      : "—",
    application: !topic.external.length
      ? "—"
      : repositoryCandidates
        ? t("study.profile.applicationCandidates", { implementations, releases })
        : compact
          ? t("study.profile.applicationShort", { implementations, releases })
          : t("study.profile.applicationValue", { implementations, releases, communities }),
    concentration: topic.top_group_share === null ? "—" : formatPercent(topic.top_group_share),
  };
  const basis: Record<(typeof ORDER)[number], string> = {
    novelty: t("study.profile.novelty.basis", { year: formatMonth(topic.first_year) }),
    momentum: t("study.profile.momentum.basis", {
      prior: formatMonth(growth.prior.period),
      recent: formatMonth(growth.recent.period),
    }),
    coherence: t(`study.profile.coherence.basis_${topic.signals.coherence}`),
    spread: t("study.profile.spread.basis", {
      year: formatMonth(last?.period ?? ""),
      groups: last?.groups ?? "—",
      fresh: last?.new_groups ?? "—",
      retained: last?.retained_groups ?? "—",
      total: topic.groups,
    }),
    application: t(
      topic.screening === "query"
        ? "study.profile.application.basisLive"
        : topic.external.length
          ? "study.profile.application.basis"
          : "study.profile.application.basisNone",
    ),
    concentration: t("study.profile.concentration.basis"),
  };
  return [
    ...extra,
    ...ORDER.map((key) => ({
      key: key as string,
      label: t(
        key === "application" && repositoryCandidates
          ? "study.profile.application.liveLabel"
          : `study.profile.${key}.label`,
      ),
      value: values[key],
      basis: basis[key],
      level: t(
        key === "application" && repositoryCandidates && implementations > 0
          ? "study.profile.application.candidates"
          : `study.profile.${key}.${topic.signals[key]}`,
        {
          defaultValue: topic.signals[key],
        },
      ),
      tone:
        key === "application" && repositoryCandidates
          ? ("muted" as const)
          : ((TONES[key] as Record<string, Tone | undefined>)[topic.signals[key]] ?? "muted"),
    })),
  ];
}

/** Связь «луч → строка» ищет строку по этому ключу на одной странице с карточкой радара. */
const rowId = (key: string) => `profile-row-${key}`;

interface ProfileLink {
  active?: string;
  onActive?: (key?: string) => void;
}

export function ProfileDial({
  rows,
  className,
  active,
  onActive,
}: { rows: ProfileExtra[]; className?: string } & ProfileLink) {
  const { t } = useTranslation("trend");
  return (
    <SignalDial
      className={className}
      active={active}
      onActive={onActive}
      target={onActive && rowId}
      label={t("study.profile.title")}
      caption={t("study.profile.dial.caption")}
      centre={t("study.profile.dial.centre")}
      rings={[
        t("study.profile.dial.muted"),
        t("study.profile.dial.warn"),
        t("study.profile.dial.ok"),
      ]}
      items={rows.map(({ key, label, value, level, tone }) => ({
        key,
        value,
        level,
        tone,
        label: t(`study.profile.short.${key}`, { defaultValue: label }),
      }))}
    />
  );
}

/** Без переносов: иначе строка растягивала соседнюю по сетке; полный текст — в подсказке. */
export function ProfileList({
  rows,
  className,
  active,
  onActive,
}: { rows: ProfileExtra[]; className?: string } & ProfileLink) {
  return (
    <ul className={className}>
      {rows.map(({ key, label, value, basis, level, tone }) => (
        // Заливка шире строки, чтобы та не сдвигалась; data-flash ставит сводка по щелчку на луч.
        <li
          key={key}
          id={rowId(key)}
          tabIndex={-1}
          className={cn(
            "relative isolate min-w-0 space-y-1 border-t py-2.5 before:absolute before:-inset-x-2 before:inset-y-0 before:-z-10 before:rounded-md before:bg-accent before:opacity-0 before:transition-opacity focus-visible:outline-none focus-visible:before:opacity-100 focus-visible:before:ring-2 focus-visible:before:ring-ring data-flash:before:opacity-100",
            active === key && "before:opacity-100",
          )}
          onPointerEnter={() => onActive?.(key)}
          onPointerLeave={() => onActive?.(undefined)}
        >
          <span className="flex min-w-0 items-center gap-1 text-sm text-muted-foreground">
            <span className="truncate" title={label}>
              {label}
            </span>
            <InfoHint label={label}>{basis}</InfoHint>
          </span>
          <span className="flex min-w-0 items-center justify-between gap-3">
            <span
              className="max-w-full shrink-0 truncate text-base font-medium tabular-nums"
              title={value}
            >
              {value}
            </span>
            <Badge variant={tone} className="min-w-0" title={level}>
              <span className="truncate">{level}</span>
            </Badge>
          </span>
        </li>
      ))}
    </ul>
  );
}

export function StudyProfile({ study, topic }: { study: ProfileStudy; topic: ProfileTopic }) {
  const { t } = useTranslation("trend");
  const rows = useProfileRows({ study, topic });
  return (
    <div className="space-y-5">
      <StudyDates study={study} topic={topic} />
      <section aria-label={t("study.profile.title")} className="space-y-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h3 className="text-sm font-medium">{t("study.profile.title")}</h3>
          <p className="max-w-md text-xs text-muted-foreground">{t("study.profile.hint")}</p>
        </div>
        {/* На узкой колонке двенадцать подписей по окружности слипаются, поэтому там список. */}
        <ProfileDial rows={rows} className="hidden sm:block" />
        <ul className="divide-y">
          {rows.map(({ key, label, value, basis: reason, level, tone }) => (
            <li
              key={key}
              className="grid gap-x-4 gap-y-1 py-3 sm:grid-cols-[9rem_minmax(0,1fr)_auto] sm:items-start"
            >
              <span className="font-medium">{label}</span>
              <span className="min-w-0 text-sm leading-6">
                <span className="font-mono text-sm tabular-nums">{value}</span>
                <span className="block text-xs leading-5 text-muted-foreground">{reason}</span>
              </span>
              <Badge variant={tone} className="justify-self-start sm:justify-self-end">
                {level}
              </Badge>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
