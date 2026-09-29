import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { listRadarBets, RADAR_BETS_KEY, type RadarBetStatus } from "@/features/hypothesis";
import { formatDate } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";

const STATUSES: RadarBetStatus[] = ["confirming", "weakening", "unchanged"];

function useOwnBets(kpiId: string) {
  const bets = useRequest(RADAR_BETS_KEY, listRadarBets, { cacheMs: 60_000 });
  return (bets.data ?? []).filter(
    (hypothesis) =>
      hypothesis.kpi_id === kpiId &&
      hypothesis.detail.radar?.bet &&
      hypothesis.status !== "archived" &&
      hypothesis.status !== "rejected",
  );
}

export function ThesisBets({ kpiId }: { kpiId: string }) {
  const { t } = useTranslation("kpi");
  const own = useOwnBets(kpiId);
  if (!own.length) return null;
  const counts = STATUSES.map((status) => ({
    status,
    count: own.filter(
      (hypothesis) => (hypothesis.detail.radar?.bet?.status ?? "unchanged") === status,
    ).length,
  })).filter(({ count }) => count > 0);
  const latest = own
    .flatMap((hypothesis) => hypothesis.detail.radar?.bet?.history ?? [])
    .toSorted((a, b) => b.at.localeCompare(a.at))[0];
  return (
    <div className="space-y-0.5 text-sm">
      <Link to={`/kpi/${kpiId}`} className="block hover:underline">
        {t("card.bets", {
          summary: counts
            .map(({ status, count }) => t(`card.betStatus.${status}`, { count }))
            .join(" · "),
        })}
      </Link>
      {latest && (
        <p className="line-clamp-2 text-xs text-muted-foreground">
          {t("card.betLatest", { date: formatDate(latest.at), text: latest.text })}
        </p>
      )}
    </div>
  );
}

export function ThesisBetList({ kpiId }: { kpiId: string }) {
  const { t } = useTranslation("kpi");
  const own = useOwnBets(kpiId);
  if (!own.length) return <p className="text-sm text-muted-foreground">{t("page.betsEmpty")}</p>;
  return (
    <ol className="divide-y">
      {own.map((hypothesis) => {
        const bet = hypothesis.detail.radar?.bet;
        const latest = (bet?.history ?? []).toSorted((a, b) => b.at.localeCompare(a.at))[0];
        return (
          <li key={hypothesis.id} className="space-y-0.5 py-2 text-sm">
            <p className="flex flex-wrap items-baseline justify-between gap-2">
              <Link to={`/hypotheses/${hypothesis.id}`} className="font-medium hover:underline">
                {hypothesis.title}
              </Link>
              <span className="text-xs text-muted-foreground">
                {t(`page.betStatus.${bet?.status ?? "unchanged"}`)}
              </span>
            </p>
            {latest && (
              <p className="line-clamp-2 text-xs text-muted-foreground">
                {t("card.betLatest", { date: formatDate(latest.at), text: latest.text })}
              </p>
            )}
          </li>
        );
      })}
    </ol>
  );
}
