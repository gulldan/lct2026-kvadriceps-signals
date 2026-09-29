import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/Badge";
import { Section } from "@/shared/ui/Section";
import { Separator } from "@/shared/ui/Separator";
import { Spinner } from "@/shared/ui/Spinner";
import { cn } from "@/shared/lib/cn";
import { LIVE_SEARCH_STEPS, type LiveSearchState, type LiveSearchStep } from "../liveApi";

const SOURCE_STEPS = LIVE_SEARCH_STEPS.slice(0, 8);
const SYNTHESIS_STEPS = LIVE_SEARCH_STEPS.slice(8);

export function LibraryStagePanel({
  step,
  visited,
  finishing,
}: {
  step?: LiveSearchState["step"];
  visited: ReadonlySet<LiveSearchStep>;
  finishing: boolean;
}) {
  const { t } = useTranslation("trend");
  const position = LIVE_SEARCH_STEPS.findIndex((item) => item === step);
  const row = (item: LiveSearchStep, index: number) => {
    const current = item === step && !finishing;
    // Этап, к которому поиск уже обращался и ушёл дальше, отмечен как пройденный.
    const done = visited.has(item) && !current;
    return (
      <li
        key={item}
        aria-current={current ? "step" : undefined}
        className={cn(
          "library-progress-source text-sm",
          current
            ? "font-medium text-foreground"
            : done
              ? "text-foreground"
              : "text-muted-foreground",
        )}
      >
        <span
          className={cn(
            "flex size-5 shrink-0 items-center justify-center",
            current ? "text-brand" : "text-muted-foreground",
          )}
          aria-hidden
        >
          {current ? (
            <Spinner size="sm" />
          ) : done ? (
            <span className="library-progress-done flex size-4 items-center justify-center rounded-full bg-ok text-background">
              <Check className="size-3" strokeWidth={3} />
            </span>
          ) : (
            <span className="font-mono text-xs tabular-nums">
              {String(index + 1).padStart(2, "0")}
            </span>
          )}
        </span>
        <span className="min-w-0 wrap-anywhere">
          <span className="sr-only">
            {t(
              done
                ? "study.live.progress.stageDone"
                : current
                  ? "study.live.progress.stageCurrent"
                  : "study.live.progress.stagePending",
            )}{" "}
          </span>
          {t(`study.live.progress.sources.${item}`)}
        </span>
      </li>
    );
  };

  return (
    <Section
      title={t("study.live.progress.stages")}
      className="library-progress-stages"
      action={
        position >= 0 && (
          <Badge
            variant="muted"
            className="shrink-0 font-mono tabular-nums"
            aria-label={t("study.live.progress.stageCount", {
              index: position + 1,
              total: LIVE_SEARCH_STEPS.length,
            })}
          >
            {String(position + 1).padStart(2, "0")} / {LIVE_SEARCH_STEPS.length}
          </Badge>
        )
      }
    >
      <div className="library-progress-plan">
        <Separator />
        <div className="min-w-0">
          <h3 className="library-progress-group-label text-xs text-muted-foreground">
            {t("study.live.progress.library.sourcesGroup")}
          </h3>
          <ol className="library-progress-source-list">{SOURCE_STEPS.map(row)}</ol>
        </div>
        <Separator />
        <div className="min-w-0">
          <h3 className="library-progress-group-label text-xs text-muted-foreground">
            {t("study.live.progress.library.synthesisGroup")}
          </h3>
          <ol className="library-progress-source-list" start={9}>
            {SYNTHESIS_STEPS.map((item, index) => row(item, index + 8))}
          </ol>
        </div>
      </div>
    </Section>
  );
}
