import { Disclosure } from "@/shared/ui/Disclosure";
import { Textarea } from "@/shared/ui/Textarea";
import { useEffect, useState } from "react";
import { RotateCcw } from "lucide-react";
import { toast } from "@/shared/ui/Sonner";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import { Label } from "@/shared/ui/Label";
import { Section } from "@/shared/ui/Section";
import {
  fetchHypothesisRuntimeSettings,
  loadHypothesisRuntimeSettings,
  persistDefaultHypothesisRuntimeSettings,
  persistHypothesisRuntimeSettings,
  type HypothesisRuntimeSettings,
} from "@/shared/appSettings";
import { FieldRow, ToggleRow } from "@/shared/ui/Fields";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";

type RuntimeNumberKey = Exclude<
  keyof HypothesisRuntimeSettings,
  "deepPostprocessEnabled" | "excludedDirections"
>;

interface RuntimeField {
  key: RuntimeNumberKey;
  min: number;
  max: number;
  unit?: "pcs" | "sec" | "percent";
}

const GENERATION_FIELDS: RuntimeField[] = [
  { key: "defaultGenerateCount", min: 3, max: 10, unit: "pcs" },
  { key: "clusterGenerateCount", min: 3, max: 10, unit: "pcs" },
  { key: "directionGenerateCount", min: 1, max: 10, unit: "pcs" },
];

const LIMIT_FIELDS: RuntimeField[] = [
  { key: "generationTimeoutSec", min: 30, max: 600, unit: "sec" },
  { key: "graphDirectionLimit", min: 1, max: 20, unit: "pcs" },
];

const THRESHOLD_FIELDS: RuntimeField[] = [
  { key: "readyTrlMin", min: 1, max: 9 },
  { key: "readyScoreMin", min: 0, max: 100, unit: "percent" },
  { key: "riskScoreMin", min: 0, max: 100, unit: "percent" },
];

function RuntimeFieldInput({
  field,
  value,
  onChange,
}: {
  field: RuntimeField;
  value: number;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation("admin");
  return (
    <FieldRow
      id={`runtime-${field.key}`}
      label={t(`runtime.fields.${field.key}.label`)}
      layout="row"
      unit={field.unit ? t(`units.${field.unit}`) : undefined}
    >
      <Input
        id={`runtime-${field.key}`}
        type="number"
        required
        min={field.min}
        max={field.max}
        value={Number.isFinite(value) ? value : ""}
        onChange={(e) => onChange(e.target.value)}
        className="w-28"
      />
    </FieldRow>
  );
}

export function RuntimeSettingsCard() {
  const { t } = useTranslation("admin");
  const [settings, setSettings] = useState<HypothesisRuntimeSettings>(() =>
    loadHypothesisRuntimeSettings(),
  );
  const [busy, setBusy] = useState(false);
  const [phase, setPhase] = useState<"loading" | "ready" | "error">("loading");
  const [loadVersion, setLoadVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setPhase("loading");
    fetchHypothesisRuntimeSettings()
      .then((next) => {
        if (!cancelled) {
          setSettings(next);
          setPhase("ready");
        }
      })
      .catch(() => {
        if (!cancelled) setPhase("error");
      });
    return () => {
      cancelled = true;
    };
  }, [loadVersion]);

  const setField = (key: RuntimeNumberKey, value: string) => {
    setSettings((prev) => ({ ...prev, [key]: value === "" ? Number.NaN : Number(value) }));
  };

  const save = async () => {
    setBusy(true);
    try {
      const next = await persistHypothesisRuntimeSettings(settings);
      setSettings(next);
      toast.success(t("runtime.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("runtime.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    try {
      const next = await persistDefaultHypothesisRuntimeSettings();
      setSettings(next);
      toast.success(t("runtime.resetDone"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("runtime.resetFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (!busy) void save();
      }}
    >
      {phase === "error" ? (
        <ErrorState
          variant="compact"
          message={t("runtime.loadFailed")}
          onRetry={() => setLoadVersion((version) => version + 1)}
        />
      ) : phase === "loading" ? (
        <div className="flex justify-center py-6">
          <Spinner label={t("loading")} />
        </div>
      ) : (
        <fieldset disabled={busy} className="min-w-0">
          <Section title={t("runtime.generation")}>
            <div className="divide-y">
              {GENERATION_FIELDS.map((f) => (
                <RuntimeFieldInput
                  key={f.key}
                  field={f}
                  value={settings[f.key]}
                  onChange={(v) => setField(f.key, v)}
                />
              ))}
            </div>
          </Section>

          <ToggleRow
            id="runtime-deep-postprocess"
            label={t("runtime.deepTitle")}
            hint={t("runtime.deepHint")}
            checked={settings.deepPostprocessEnabled}
            onCheckedChange={(checked) =>
              setSettings((prev) => ({ ...prev, deepPostprocessEnabled: checked }))
            }
          />

          <Disclosure title={t("runtime.advanced")}>
            <div className="divide-y">
              {LIMIT_FIELDS.map((f) => (
                <RuntimeFieldInput
                  key={f.key}
                  field={f}
                  value={settings[f.key]}
                  onChange={(v) => setField(f.key, v)}
                />
              ))}
            </div>
            <Label htmlFor="runtime-excluded" className="mt-4 block">
              {t("runtime.excluded")}
            </Label>
            <Textarea
              id="runtime-excluded"
              value={settings.excludedDirections}
              onChange={(e) =>
                setSettings((prev) => ({ ...prev, excludedDirections: e.target.value }))
              }
              maxLength={600}
              rows={2}
              className="mt-2"
              placeholder={t("runtime.excludedPlaceholder")}
            />
          </Disclosure>

          <Disclosure title={t("runtime.queues")}>
            <p className="max-w-2xl text-xs leading-relaxed text-muted-foreground">
              {t("runtime.queuesHint", {
                trl: settings.readyTrlMin,
                score: settings.readyScoreMin,
                risk: settings.riskScoreMin,
              })}
            </p>
            <div className="divide-y">
              {THRESHOLD_FIELDS.map((f) => (
                <RuntimeFieldInput
                  key={f.key}
                  field={f}
                  value={settings[f.key]}
                  onChange={(v) => setField(f.key, v)}
                />
              ))}
            </div>
          </Disclosure>
          <div className="flex flex-wrap justify-end gap-2 border-t py-4">
            <Button variant="outline" disabled={busy} onClick={() => void reset()}>
              <RotateCcw className="size-3.5" aria-hidden />
              {t("actions.resetToDefaults")}
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? t("actions.saving") : t("actions.save")}
            </Button>
          </div>
        </fieldset>
      )}
    </form>
  );
}
