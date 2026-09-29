import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import { FieldRow } from "@/shared/ui/Fields";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Segmented } from "@/shared/ui/Segmented";
import type { AnalysisRequest } from "../trendApi";

export interface TrendFormValue {
  query: string;
  start: string;
  cutoff: string;
  mode: AnalysisRequest["mode"];
}

export function TrendForm({
  initial,
  onSubmit,
  busy = false,
  compact = false,
}: {
  initial: TrendFormValue;
  onSubmit: (value: TrendFormValue) => void;
  busy?: boolean;
  compact?: boolean;
}) {
  const { t } = useTranslation("trend");
  const [value, setValue] = useState(initial);
  const dirty = useRef(false);
  useEffect(() => {
    if (!dirty.current) setValue(initial);
  }, [initial]);
  const change = (next: TrendFormValue) => {
    dirty.current = true;
    setValue(next);
  };
  const [invalid, setInvalid] = useState(false);
  const [settings, setSettings] = useState(false);
  const today = new Date().toISOString().slice(0, 10);
  const pending = (next: TrendFormValue) =>
    busy &&
    (["query", "start", "cutoff", "mode"] as const).every((key) => next[key] === initial[key]);
  const submitValue = (next: TrendFormValue) => {
    if (pending(next)) return;
    const length = Array.from(next.query.trim()).length;
    if (
      length < 2 ||
      length > 300 ||
      !next.start ||
      !next.cutoff ||
      next.start > next.cutoff ||
      next.cutoff > today
    ) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    onSubmit({ ...next, query: next.query.trim() });
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    submitValue(value);
  };
  const input = (
    <Input
      id="trend-query"
      value={value.query}
      required
      aria-label={t("query")}
      aria-invalid={invalid}
      aria-describedby={invalid ? "trend-form-error" : undefined}
      onChange={(event) => change({ ...value, query: event.target.value })}
      placeholder={t("queryPlaceholder")}
    />
  );
  return (
    <form
      onSubmit={submit}
      className={compact ? "space-y-2" : "space-y-4"}
      aria-label={t("formLabel")}
    >
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-0 flex-1 basis-64">
          <FieldRow id="trend-query" label={t("query")} layout={compact ? "compact" : "stack"}>
            {input}
          </FieldRow>
        </div>
        <Button type="submit" disabled={pending(value)}>
          <Search aria-hidden />
          {t("analyze")}
        </Button>
        {compact && (
          <Button
            variant="outline"
            aria-expanded={settings}
            onClick={() => setSettings((expanded) => !expanded)}
          >
            {t("map.querySettings")}
          </Button>
        )}
      </div>
      {(!compact || settings) && (
        <>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
            <span id="trend-form-hint" className="text-muted-foreground">
              {t("examples")}
            </span>
            {["RAG", "Robotics", "Solar cells", "CRISPR"].map((query) => (
              <Button
                key={query}
                variant="link"
                size="inline"
                disabled={pending({ ...value, query })}
                onClick={() => {
                  const next = { ...value, query };
                  change(next);
                  submitValue(next);
                }}
              >
                {query}
              </Button>
            ))}
          </div>
          <Disclosure title={t("period", { start: value.start, end: value.cutoff })}>
            <div className="grid gap-4 sm:grid-cols-2">
              <FieldRow id="trend-start" label={t("start")}>
                <Input
                  id="trend-start"
                  type="date"
                  required
                  value={value.start}
                  max={value.cutoff}
                  onChange={(event) => change({ ...value, start: event.target.value })}
                />
              </FieldRow>
              <FieldRow id="trend-cutoff" label={t("cutoff")}>
                <Input
                  id="trend-cutoff"
                  type="date"
                  required
                  value={value.cutoff}
                  min={value.start}
                  max={today}
                  onChange={(event) => change({ ...value, cutoff: event.target.value })}
                />
              </FieldRow>
            </div>
            <div className="mt-4 space-y-2">
              <Segmented
                aria-label={t("mode")}
                value={value.mode}
                onChange={(mode) => change({ ...value, mode })}
                options={[
                  { value: "as_of", label: t("asObserved") },
                  { value: "retrospective_current_membership", label: t("reconstruction") },
                ]}
              />
              <p className="max-w-prose text-sm text-muted-foreground">{t("modeHint")}</p>
            </div>
          </Disclosure>
        </>
      )}
      {invalid && (
        <p id="trend-form-error" role="alert" className="text-sm text-risk">
          {t("invalidInput")}
        </p>
      )}
    </form>
  );
}
