import { useTranslation } from "react-i18next";

export function ConsensusMeter({
  forCount,
  againstCount,
}: {
  forCount: number;
  againstCount: number;
}) {
  const { t } = useTranslation("hypothesisDetail");
  const total = forCount + againstCount;
  if (total === 0) return null;
  const forPct = (forCount / total) * 100;
  return (
    <div className="space-y-1">
      <div className="flex h-2 overflow-hidden rounded-full bg-muted" aria-hidden>
        <div className="bg-ok" style={{ width: `${forPct}%` }} />
        <div className="bg-risk" style={{ width: `${100 - forPct}%` }} />
      </div>
      <div className="flex items-center justify-between text-xs">
        <span className="inline-flex items-center gap-1 font-medium text-ok">
          <span className="size-2 rounded-full bg-ok" aria-hidden />
          {t("confidence.fragmentsFor", { count: forCount })}
        </span>
        <span className="inline-flex items-center gap-1 font-medium text-risk">
          {t("confidence.fragmentsAgainst", { count: againstCount })}
          <span className="size-2 rounded-full bg-risk" aria-hidden />
        </span>
      </div>
    </div>
  );
}
