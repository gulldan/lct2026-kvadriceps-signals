import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getLiveMoneyMatch } from "../moneyApi";
import { MoneyEventLine } from "./EarlyMoney";

const SHOWN = 3;

/** «Инвесторы заметили»: ранние сделки и гранты, в тексте которых есть название технологии. */
export function MoneyPlaque({ terms }: { terms: string[] }) {
  const { t } = useTranslation("trend");
  const match = useRequest(
    terms.length ? `money-match:${terms.join("|")}` : null,
    (signal) => getLiveMoneyMatch(terms, signal),
    { cacheMs: 5 * 60_000 },
  );
  const data = match.data;
  return (
    <Section title={t("money.plaque.title")} description={t("money.plaque.basis")}>
      {match.status === "error" ? (
        <p className="text-sm text-muted-foreground">{t("money.failed")}</p>
      ) : !data ? (
        <Skeleton className="h-16 w-full" />
      ) : data.status !== "ready" ? (
        <p className="text-sm text-muted-foreground">
          {t(data.status === "loading" ? "money.loading" : "money.failed")}
        </p>
      ) : data.total === 0 ? (
        <p className="text-sm text-muted-foreground">{t("money.plaque.none")}</p>
      ) : (
        <div className="space-y-3">
          <p className="text-sm">
            {t("money.plaque.count", { total: formatNumber(data.total) })}
            <span className="block text-xs text-muted-foreground">
              {t("money.plaque.kinds", {
                round: formatNumber(data.kinds.round ?? 0),
                grant: formatNumber(data.kinds.grant ?? 0),
                accelerator: formatNumber(data.kinds.accelerator ?? 0),
                development: formatNumber(data.kinds.development ?? 0),
                research: formatNumber(data.kinds.research ?? 0),
              })}
            </span>
          </p>
          <ul className="space-y-3">
            {data.events.slice(0, SHOWN).map((event) => (
              <MoneyEventLine key={event.id} event={event} />
            ))}
          </ul>
        </div>
      )}
    </Section>
  );
}
