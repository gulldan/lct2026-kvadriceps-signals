import { ArrowUpRight, Clock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router-dom";
import { toneClasses, type Tone } from "@/shared/design-system/tones";
import { cn } from "@/shared/lib/cn";
import { formatDate, formatNumber } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";
import { AiNote } from "@/shared/ui/AiNote";
import { Button } from "@/shared/ui/Button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Kicker } from "@/shared/ui/Kicker";
import { Notice } from "@/shared/ui/Notice";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Spinner } from "@/shared/ui/Spinner";
import {
  writeLiveExecutive,
  type LiveDetail,
  type LiveExecutive,
  type LiveExecutiveItem,
} from "../liveApi";
import { liveTopicName } from "./LiveOverview";
import { spellAcronyms } from "../signalSources";
import { SignalHypothesisDialog } from "./SignalHypothesisDialog";

type Step = NonNullable<LiveExecutiveItem["step"]>;

const STEPS: Step[] = ["pilot", "watch", "reject"];
const STEP_TONE: Record<Step, Tone> = { pilot: "ok", watch: "brand", reject: "muted" };

export function ExecutiveDialog({
  open,
  onOpenChange,
  detail,
  query,
  write = writeLiveExecutive,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  detail: LiveDetail;
  query?: string;
  /** Витрина подставляет готовую справку: каталог работает без сервиса. */
  write?: typeof writeLiveExecutive;
}) {
  const { t } = useTranslation("trend");
  const topic = detail.topic;
  const brief = useRequest(open ? `executive:${query ?? `topic:${topic.id}`}` : null, (signal) =>
    write(query ? { q: query } : { topic: topic.id }, signal),
  );
  const title = query ? spellAcronyms(query) : liveTopicName(topic, t);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85dvh] gap-6 overflow-y-auto sm:max-w-3xl">
        <DialogHeader className="space-y-2">
          <Kicker>{t("executive.docType")}</Kicker>
          <DialogTitle className="text-2xl leading-tight">
            {title.charAt(0).toLocaleUpperCase() + title.slice(1)}
          </DialogTitle>
          <DialogDescription>
            {brief.data ? (
              <>
                <span className="block">
                  {t("signalList.collectedAt", { date: formatDate(brief.data.observed_at) })}
                </span>
                <span className="block">
                  {t("executive.coverage", {
                    sources: formatNumber(brief.data.sources_processed),
                    candidates: formatNumber(brief.data.candidates),
                    selected: formatNumber(brief.data.selected),
                  })}
                </span>
              </>
            ) : (
              t("executive.writingHint")
            )}
          </DialogDescription>
        </DialogHeader>
        {brief.status === "error" ? (
          <Notice tone="warn" role="alert" className="space-y-3">
            <p>{t("executive.failed")}</p>
            <Button variant="outline" onClick={brief.retry}>
              {t("executive.retry")}
            </Button>
          </Notice>
        ) : brief.data ? (
          <ExecutiveBrief
            brief={brief.data}
            detail={detail}
            query={query}
            onNavigate={() => onOpenChange(false)}
          />
        ) : (
          <div className="space-y-4">
            <output className="flex items-center gap-3 text-sm text-muted-foreground">
              <span aria-hidden="true">
                <Spinner />
              </span>
              {t("executive.writing")}
            </output>
            <div className="grid grid-cols-3 gap-2" aria-hidden="true">
              {STEPS.map((step) => (
                <Skeleton key={step} className="h-20 rounded-xl" />
              ))}
            </div>
            {[0, 1, 2].map((row) => (
              <Skeleton key={row} className="h-36 rounded-xl" aria-hidden="true" />
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ExecutiveBrief({
  brief,
  detail,
  query,
  onNavigate,
}: {
  brief: LiveExecutive;
  detail: LiveDetail;
  query?: string;
  onNavigate: () => void;
}) {
  const { t } = useTranslation("trend");
  if (brief.items.length === 0) return <p className="text-sm">{t("executive.none")}</p>;
  // Строка без шага — отказ модели выбрать, а не решение «отказ»: отдельная группа в конце.
  const groups = [...STEPS, undefined]
    .map((step) => ({ step, items: brief.items.filter((item) => item.step === step) }))
    .filter((group) => group.items.length > 0);
  return (
    <div className="space-y-6">
      <section aria-label={t("executive.decisions")} className="space-y-3">
        <p className="text-sm text-muted-foreground">
          {t("executive.lead", { shown: brief.items.length, selected: brief.selected })}
        </p>
        <dl className="grid grid-cols-3 gap-2">
          {STEPS.map((step) => (
            <div key={step} className="flex flex-col-reverse gap-1.5 rounded-xl bg-secondary p-4">
              <dt className="text-sm text-muted-foreground">{t(`executive.steps.${step}`)}</dt>
              <dd
                className={cn(
                  "font-mono text-3xl leading-none font-semibold tabular-nums",
                  toneClasses[STEP_TONE[step]].text,
                )}
              >
                {brief.items.filter((item) => item.step === step).length}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      {groups.map(({ step, items }) => (
        <section key={step ?? "none"} aria-labelledby={`executive-${step ?? "none"}`}>
          <div className="mb-3 flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <span
              className={cn(
                "size-2 shrink-0 self-center rounded-full",
                toneClasses[step ? STEP_TONE[step] : "muted"].marker,
              )}
              aria-hidden="true"
            />
            <h3 id={`executive-${step ?? "none"}`} className="font-semibold">
              {t(`executive.steps.${step ?? "none"}`)}
            </h3>
            <p className="text-sm text-muted-foreground">
              {t(`executive.stepHints.${step ?? "none"}`)}
            </p>
          </div>
          <ul className="space-y-3">
            {items.map((item) => (
              <BriefItem
                key={item.id}
                item={item}
                detail={detail}
                query={query}
                onNavigate={onNavigate}
              />
            ))}
          </ul>
        </section>
      ))}
      {brief.model && <AiNote>{t("executive.basis", { model: brief.model })}</AiNote>}
    </div>
  );
}

function BriefItem({
  item,
  detail,
  query,
  onNavigate,
}: {
  item: LiveExecutiveItem;
  detail: LiveDetail;
  query?: string;
  onNavigate: () => void;
}) {
  const { t } = useTranslation("trend");
  const [params] = useSearchParams();
  const candidate = detail.candidates?.find((row) => row.id === item.id);
  const href = () => {
    const next = new URLSearchParams(params);
    next.delete("view");
    next.set("signal", item.id);
    return `/trends?${next}`;
  };
  const missing = <span className="text-muted-foreground italic">{t("executive.noData")}</span>;
  return (
    <li className="space-y-3 rounded-xl bg-secondary p-4">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <h4 className="min-w-0 text-base leading-snug font-semibold">
          {candidate ? (
            <Link
              to={href()}
              onClick={onNavigate}
              className="group hover:text-accent-foreground hover:underline"
            >
              {liveTopicName(candidate, t)}
              <ArrowUpRight
                className="ml-1 inline size-4 align-[-0.125em] text-muted-foreground group-hover:text-accent-foreground"
                aria-hidden
              />
            </Link>
          ) : (
            item.name
          )}
        </h4>
        {candidate && (item.step === "pilot" || item.step === "watch") && (
          <SignalHypothesisDialog
            detail={detail}
            candidate={candidate}
            query={query ?? liveTopicName(detail.topic, t)}
            topic={query ? undefined : detail.topic.id}
            horizon={item.horizon}
            compact
          />
        )}
      </div>
      {item.summary && <p className="text-sm text-muted-foreground">{item.summary}</p>}
      <dl className="grid gap-x-6 gap-y-3 border-t pt-3 text-sm sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_8rem]">
        <div className="space-y-1">
          <dt className="kicker text-muted-foreground">{t("executive.benefit")}</dt>
          <dd>{item.benefit || missing}</dd>
        </div>
        <div className="space-y-1">
          <dt className="kicker text-muted-foreground">{t("executive.reason")}</dt>
          <dd>{item.reason || missing}</dd>
        </div>
        <div className="space-y-1">
          <dt className="kicker text-muted-foreground">{t("executive.horizon")}</dt>
          <dd className="flex items-center gap-1.5 whitespace-nowrap">
            <Clock className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
            {item.horizon ? t(`executive.horizons.${item.horizon}`) : missing}
          </dd>
        </div>
      </dl>
    </li>
  );
}
