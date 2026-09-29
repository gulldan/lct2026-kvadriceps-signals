import { useTranslation } from "react-i18next";

import { Input } from "@/shared/ui/Input";
import type { AppSettingsState } from "../model";
import { Disclosure } from "@/shared/ui/Disclosure";
import { FieldRow } from "@/shared/ui/Fields";

export function CostCard({ app }: { app: AppSettingsState }) {
  const { t } = useTranslation("admin");
  return (
    <Disclosure title={t("system.cost.title")}>
      <div className="divide-y">
        <FieldRow
          id="sys-rub-rate"
          label={t("system.cost.rubRate")}
          hint={t("system.cost.rubRateHint")}
          unit={t("system.cost.rubRateUnit")}
          layout="row"
        >
          <Input
            id="sys-rub-rate"
            type="number"
            min={0.01}
            step="any"
            inputMode="decimal"
            value={app.effective("LLM_RUB_PER_USD")}
            onChange={(e) => app.setValue("LLM_RUB_PER_USD", e.target.value)}
            className="w-28"
            aria-describedby="sys-rub-rate-hint"
            placeholder="90"
          />
        </FieldRow>
        <FieldRow id="sys-cost-currency" label={t("system.cost.currency")} layout="row">
          <Input
            id="sys-cost-currency"
            value={app.effective("LLM_COST_CURRENCY")}
            onChange={(e) => app.setValue("LLM_COST_CURRENCY", e.target.value)}
            className="w-28"
            placeholder="₽"
          />
        </FieldRow>
        <FieldRow
          id="sys-prices"
          label={t("system.cost.prices")}
          hint={t("system.cost.pricesHint")}
          layout="row"
        >
          <Input
            id="sys-prices"
            value={app.effective("LLM_PRICES")}
            onChange={(e) => app.setValue("LLM_PRICES", e.target.value)}
            aria-describedby="sys-prices-hint"
            placeholder="model=800/800"
          />
        </FieldRow>
      </div>
    </Disclosure>
  );
}
