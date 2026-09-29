import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { AiNote } from "@/shared/ui/AiNote";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { Spinner } from "@/shared/ui/Spinner";
import { writeTechnologyBrief, type TechnologyBrief as Brief } from "../technologyApi";

export function useWriteBrief(id: string, onBrief: (brief: Brief) => void) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<"unconfigured" | "failed" | null>(null);
  const write = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      onBrief(await writeTechnologyBrief(id));
    } catch (failure) {
      setError(failure instanceof ApiError && failure.status === 503 ? "unconfigured" : "failed");
    } finally {
      setBusy(false);
    }
  }, [id, onBrief]);
  return { busy, error, write };
}

export function TechnologyBrief({
  id,
  brief,
  missing,
  onBrief,
}: {
  id: string;
  brief: Brief | null;
  missing: boolean;
  onBrief: (brief: Brief) => void;
}) {
  const { t } = useTranslation("trend");
  const { error, write } = useWriteBrief(id, onBrief);
  // После отказа модели — только руками, иначе каждая перерисовка гоняла бы её по кругу.
  useEffect(() => {
    if (missing && !brief && !error) void write();
  }, [missing, brief, error, write]);
  return (
    <Section title={t("technology.brief.title")}>
      {brief?.advantage ? (
        <div className="space-y-2">
          <p className="max-w-prose text-sm leading-relaxed">{brief.advantage}</p>
          <AiNote>{brief.basis}</AiNote>
        </div>
      ) : brief ? (
        <p className="text-sm text-muted-foreground">{t("technology.brief.none")}</p>
      ) : error ? (
        <div className="space-y-3">
          <Notice tone="warn">{t(`technology.brief.${error}`)}</Notice>
          <Button variant="outline" size="sm" onClick={() => void write()}>
            {t("technology.brief.write")}
          </Button>
        </div>
      ) : (
        <output className="flex items-center gap-3 text-sm text-muted-foreground">
          <span aria-hidden="true">
            <Spinner />
          </span>
          {t("technology.brief.writing")}
        </output>
      )}
    </Section>
  );
}
