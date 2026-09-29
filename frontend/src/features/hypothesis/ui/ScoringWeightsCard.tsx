import { Range } from "@/shared/ui/Range";
import { useEffect, useState } from "react";
import { Scale, RotateCcw } from "lucide-react";
import { toast } from "@/shared/ui/Sonner";
import { useTranslation } from "react-i18next";

import {
  getScoringWeights,
  saveScoringWeights,
  type ScoringWeights,
} from "@/features/hypothesis/api";
import { Button } from "@/shared/ui/Button";
import { SettingsCard } from "@/shared/ui/SettingsCard";
import { Disclosure } from "@/shared/ui/Disclosure";
import { FieldRow } from "@/shared/ui/Fields";
import { ErrorState } from "@/shared/ui/ErrorState";

const WEIGHT_FIELDS = [
  "kpi_fit",
  "evidence",
  "novelty",
  "value",
  "risk_inv",
  "trl_fit",
] as const satisfies readonly (keyof ScoringWeights)[];

const DEFAULT_WEIGHTS: ScoringWeights = {
  kpi_fit: 0.25,
  evidence: 0.2,
  novelty: 0.2,
  value: 0.15,
  risk_inv: 0.1,
  trl_fit: 0.1,
};

export function ScoringWeightsCard() {
  const { t } = useTranslation("admin");
  const [weights, setWeights] = useState<ScoringWeights | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [loadVersion, setLoadVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setFailed(false);
    setWeights(null);
    getScoringWeights()
      .then((w) => {
        if (!cancelled) setWeights(w);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [loadVersion]);

  const total = weights ? WEIGHT_FIELDS.reduce((sum, f) => sum + (weights[f] || 0), 0) : 0;

  const setField = (key: keyof ScoringWeights, value: number) => {
    setWeights((prev) => (prev ? { ...prev, [key]: value } : prev));
  };

  const save = async () => {
    if (!weights) return;
    if (total <= 0) {
      toast.error(t("weights.atLeastOne"));
      return;
    }
    setBusy(true);
    try {
      const normalized = await saveScoringWeights(weights);
      setWeights(normalized);
      toast.success(t("weights.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("weights.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <SettingsCard icon={Scale} title={t("weights.title")} className="border-t-0 pt-0">
      {failed ? (
        <ErrorState
          variant="compact"
          message={t("weights.loadFailed")}
          onRetry={() => setLoadVersion((version) => version + 1)}
        />
      ) : weights === null ? (
        <p className="text-sm text-muted-foreground">{t("loading")}</p>
      ) : (
        <>
          <div className="divide-y">
            {WEIGHT_FIELDS.map((key) => {
              const value = weights[key] || 0;
              const label = t(`weights.fields.${key}.label`);
              return (
                <FieldRow key={key} id={`weight-${key}`} label={label} layout="row">
                  <Range
                    id={`weight-${key}`}
                    disabled={busy}
                    min={0}
                    max={100}
                    step={1}
                    value={Math.round(value * 100)}
                    onChange={(e) => setField(key, Number(e.target.value) / 100)}
                    aria-label={label}
                  />
                  <output
                    htmlFor={`weight-${key}`}
                    className="w-12 shrink-0 text-right text-sm tabular-nums"
                  >
                    {Math.round(value * 100)}%
                  </output>
                </FieldRow>
              );
            })}
          </div>
          <Disclosure title={t("weights.hintLabel")}>
            <p className="max-w-2xl text-sm text-muted-foreground">{t("weights.hintBody")}</p>
            <dl className="mt-4 space-y-3">
              {WEIGHT_FIELDS.map((key) => (
                <div key={key} className="grid gap-1 sm:grid-cols-2">
                  <dt className="text-sm font-medium">{t(`weights.fields.${key}.label`)}</dt>
                  <dd className="text-sm text-muted-foreground">
                    {t(`weights.fields.${key}.hint`)}
                  </dd>
                </div>
              ))}
            </dl>
          </Disclosure>
          <div className="flex flex-wrap items-center justify-end gap-3 border-t py-4">
            {Math.round(total * 100) !== 100 && (
              <p className="mr-auto text-xs text-muted-foreground">
                {t("weights.total", { total: Math.round(total * 100) })}
              </p>
            )}
            <div className="flex gap-2">
              <Button variant="outline" disabled={busy} onClick={() => setWeights(DEFAULT_WEIGHTS)}>
                <RotateCcw className="size-3.5" aria-hidden />
                {t("actions.resetToDefaults")}
              </Button>
              <Button disabled={busy} onClick={() => void save()}>
                {busy ? t("actions.saving") : t("actions.save")}
              </Button>
            </div>
          </div>
        </>
      )}
    </SettingsCard>
  );
}
