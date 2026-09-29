import { useTranslation } from "react-i18next";

import { Input } from "@/shared/ui/Input";
import { Disclosure } from "@/shared/ui/Disclosure";
import type { AppSettingsState } from "../model";
import { FieldRow } from "@/shared/ui/Fields";
import { BoolRow } from "./SettingsBits";

export function GenerationFlagsCard({ app }: { app: AppSettingsState }) {
  const { t } = useTranslation("admin");
  return (
    <div>
      <div className="divide-y">
        <BoolRow
          app={app}
          k="PUBSEARCH_ENABLED"
          label={t("system.pubsearchEnabled")}
          hint={t("system.pubsearchEnabledHint")}
        />
        <BoolRow
          app={app}
          k="RAG_GEN_BREADTH"
          label={t("system.genBreadth")}
          hint={t("system.genBreadthHint")}
        />
        <BoolRow
          app={app}
          k="KPI_SUGGEST"
          label={t("system.kpiSuggest")}
          hint={t("system.kpiSuggestHint")}
        />
      </div>
      <Disclosure title={t("system.sourceOptions")}>
        <div className="divide-y">
          <FieldRow
            id="sys-quality"
            label={t("system.qualityFloor")}
            hint={t("system.qualityFloorHint")}
            layout="row"
          >
            <Input
              id="sys-quality"
              type="number"
              min={0}
              max={1}
              step={0.05}
              value={app.effective("RAG_GEN_QUALITY_FLOOR")}
              onChange={(e) => app.setValue("RAG_GEN_QUALITY_FLOOR", e.target.value)}
              aria-describedby="sys-quality-hint"
              className="w-28"
            />
          </FieldRow>
        </div>
      </Disclosure>
    </div>
  );
}
