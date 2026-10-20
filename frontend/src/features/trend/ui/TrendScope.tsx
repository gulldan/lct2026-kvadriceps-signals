import type { ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Badge } from "@/shared/ui/Badge";
import { Segmented } from "@/shared/ui/Segmented";
import { readEntity, withScope, type Entity } from "../scope";
import { useCatalogueAvailable } from "../useCatalogueAvailable";

// С плашками «новое» четыре вида входят в строку с заголовком полными именами только с 2xl;
// ниже — короткие имена, полное остаётся у радиокнопки.
const label = (full: string, short: string) => (
  <>
    <span aria-hidden className="2xl:hidden">
      {short}
    </span>
    <span className="sr-only 2xl:not-sr-only">{full}</span>
  </>
);

export function TrendScope() {
  const { t } = useTranslation("trend");
  // Карта и деньги появились после первой сдачи: плашка показывает жюри, что нового.
  // На узком ноутбуке слово не входит в строку, поэтому там вместо него точка.
  const fresh = (name: ReactNode) => (
    <>
      {name}
      <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-complement xl:hidden" />
      <Badge
        variant="complement-solid"
        className="sr-only px-1.5 py-0 text-micro whitespace-nowrap xl:not-sr-only"
      >
        {t("scope.new")}
      </Badge>
    </>
  );
  const [params, setParams] = useSearchParams();
  const entity = readEntity(params);
  const onCatalogue = entity === "technologies";
  const catalogue = useCatalogueAvailable(onCatalogue) || onCatalogue;
  return (
    // На телефоне четыре вида не входят в строку: переключатель прокручивается, а не ломает слова.
    <div className="flex max-w-full flex-wrap items-center gap-x-6 gap-y-3 overflow-x-auto">
      <Segmented
        id="trend-entity"
        size="md"
        className="w-max max-w-none shrink-0"
        aria-label={t("scope.entity")}
        options={[
          { value: "directions", label: t("scope.directions") },
          ...(catalogue
            ? [
                {
                  value: "technologies" as const,
                  label: label(t("scope.technologies"), t("scope.technologiesShort")),
                },
              ]
            : []),
          { value: "map", label: fresh(label(t("scope.map"), t("scope.mapShort"))) },
          { value: "money", label: fresh(label(t("scope.money"), t("scope.moneyShort"))) },
        ]}
        value={entity}
        onChange={(value: Entity) => setParams(withScope(params, { entity: value }))}
      />
    </div>
  );
}
