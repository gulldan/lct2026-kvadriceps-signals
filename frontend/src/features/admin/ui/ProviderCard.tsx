import { RotateCcw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import type { AppSettingsState } from "../model";
import { Disclosure } from "@/shared/ui/Disclosure";
import { FieldRow, StatStrip } from "@/shared/ui/Fields";

export function ProviderCard({ app }: { app: AppSettingsState }) {
  const { t } = useTranslation("admin");

  return (
    <div>
      <FieldRow id="sys-model" label={t("system.model")} layout="row">
        <Input
          id="sys-model"
          value={app.effective("VLLM_MODEL")}
          onChange={(e) => app.setValue("VLLM_MODEL", e.target.value)}
          placeholder="qwen3-32b"
        />
      </FieldRow>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">{t("system.applyNote")}</p>
        <Button
          variant="ghost"
          size="sm"
          disabled={app.busy}
          onClick={() => app.setValue("VLLM_MODEL", "")}
        >
          <RotateCcw className="size-3.5" aria-hidden />
          {t("system.backToDeployment")}
        </Button>
      </div>
      <div className="mb-4 divide-y">
        <FieldRow
          id="sys-model-display"
          label={t("system.modelDisplay")}
          hint={t("system.modelDisplayHint")}
          layout="row"
        >
          <Input
            id="sys-model-display"
            value={app.effective("LLM_MODEL_DISPLAY_NAME")}
            onChange={(e) => app.setValue("LLM_MODEL_DISPLAY_NAME", e.target.value)}
            aria-describedby="sys-model-display-hint"
          />
        </FieldRow>
        <FieldRow
          id="sys-radar-workers"
          label={t("system.radarWorkers")}
          hint={t("system.radarWorkersHint")}
          layout="row"
        >
          <Input
            id="sys-radar-workers"
            type="number"
            min={1}
            max={256}
            step={1}
            value={app.effective("RADAR_LLM_WORKERS")}
            onChange={(e) => app.setValue("RADAR_LLM_WORKERS", e.target.value)}
            aria-describedby="sys-radar-workers-hint"
            className="w-28"
          />
        </FieldRow>
        <FieldRow
          id="sys-radar-rpm"
          label={t("system.radarRpm")}
          hint={t("system.radarRpmHint")}
          layout="row"
        >
          <Input
            id="sys-radar-rpm"
            type="number"
            min={0}
            step={1}
            value={app.effective("RADAR_LLM_RPM")}
            onChange={(e) => app.setValue("RADAR_LLM_RPM", e.target.value)}
            aria-describedby="sys-radar-rpm-hint"
            className="w-28"
          />
        </FieldRow>
      </div>
      <Disclosure title={t("system.connection")}>
        <StatStrip
          columns={2}
          items={[
            {
              label: t("system.url"),
              value: t(app.item("VLLM_URL")?.envSet ? "system.configured" : "system.notConfigured"),
            },
            {
              label: t("system.apiKey"),
              value: t(
                app.item("VLLM_API_KEY")?.envSet ? "system.apiKeySet" : "system.apiKeyUnset",
              ),
            },
          ]}
        />
        <p className="mt-3 text-xs text-muted-foreground">{t("system.connectionManaged")}</p>
      </Disclosure>
    </div>
  );
}
