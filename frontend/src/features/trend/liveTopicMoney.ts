import type { TFunction } from "i18next";
import type { LiveTopicMoney } from "./liveApi";

/** Подсказка к числу денег: стадии раундов и самые частые фонды, без которых число ничего не говорит. */
export function moneyTitle(money: LiveTopicMoney, t: TFunction<"trend">) {
  const stages = Object.entries(money.stages ?? {})
    .filter(([stage, count]) => count > 0 && stage !== "unknown")
    .map(([stage, count]) => `${t(`money.stages.${stage}`, { defaultValue: stage })}: ${count}`);
  return [
    t("study.live.table.moneyHint", { count: money.total }),
    stages.join(", "),
    money.investors?.length
      ? t("study.live.table.moneyInvestors", { names: money.investors.join(", ") })
      : "",
  ]
    .filter(Boolean)
    .join(". ");
}
