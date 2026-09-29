import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Segmented } from "@/shared/ui/Segmented";
import { readEntity, withScope, type Entity } from "../scope";
import { useCatalogueAvailable } from "../useCatalogueAvailable";

export function TrendScope() {
  const { t } = useTranslation("trend");
  const [params, setParams] = useSearchParams();
  const entity = readEntity(params);
  const catalogue = useCatalogueAvailable() || entity === "technologies";
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
      {catalogue && (
        <Segmented
          id="trend-entity"
          size="md"
          aria-label={t("scope.entity")}
          options={[
            { value: "directions", label: t("scope.directions") },
            { value: "technologies", label: t("scope.technologies") },
          ]}
          value={entity}
          onChange={(value: Entity) => setParams(withScope(params, { entity: value }))}
        />
      )}
    </div>
  );
}
