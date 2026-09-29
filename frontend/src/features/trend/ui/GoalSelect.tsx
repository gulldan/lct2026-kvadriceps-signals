import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { listKPIs, type ApiKPI } from "@/features/kpi";
import { formatPercent } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";
import { Label } from "@/shared/ui/Label";
import { Notice } from "@/shared/ui/Notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { signalAffinity } from "../liveApi";

/** Одно название короче, чем нужно кодировщику. */
const goalText = (goal: ApiKPI) =>
  [goal.title, goal.metric, goal.description].filter(Boolean).join(". ");

export function GoalSelect({
  id,
  value,
  onChange,
  disabled,
  signal,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  signal?: string;
}) {
  const { t } = useTranslation("trend");
  const goals = useRequest("transfer-goals", listKPIs);
  const active = goals.data?.filter((goal) => goal.status !== "archived") ?? [];
  const affinity = useRequest(
    signal && active.length
      ? `goal-affinity:${signal}:${active.map((goal) => goal.id).join()}`
      : null,
    () => signalAffinity(signal ?? "", active.map(goalText)),
  );
  const similarity = (goal: ApiKPI) => affinity.data?.similarity[active.indexOf(goal)];
  const ranked = affinity.data
    ? active.toSorted((a, b) => (similarity(b) ?? 0) - (similarity(a) ?? 0))
    : active;
  const best = affinity.data ? ranked[0] : undefined;
  const bestFits = best !== undefined && (similarity(best) ?? 0) >= (affinity.data?.floor ?? 1);
  // Подсказка ставится один раз и только поверх «без привязки»: выбор человека она не перебивает.
  const suggested = useRef(false);
  useEffect(() => {
    if (suggested.current || !best || value !== "none") return;
    suggested.current = true;
    if (bestFits) onChange(best.id);
  }, [best, bestFits, value, onChange]);
  return (
    <>
      <div className="space-y-1">
        <Label htmlFor={id}>{t("transfer.goal")}</Label>
        <Select
          value={value}
          onValueChange={onChange}
          disabled={disabled || goals.status === "loading"}
        >
          <SelectTrigger id={id}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">{t("transfer.noGoal")}</SelectItem>
            {ranked.map((goal) => {
              const close = similarity(goal);
              return (
                <SelectItem key={goal.id} value={goal.id}>
                  {goal.title}
                  {close !== undefined && (
                    <span className="text-muted-foreground">
                      {" · "}
                      {t("transfer.goalAffinity", { value: formatPercent(close, 0) })}
                    </span>
                  )}
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        {affinity.data && (
          <p className="text-xs text-muted-foreground">
            {t(bestFits ? "transfer.goalRanked" : "transfer.goalNoMatch")}
          </p>
        )}
      </div>
      {goals.status === "error" && <Notice tone="warn">{t("transfer.goalsUnavailable")}</Notice>}
    </>
  );
}
