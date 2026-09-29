import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { listRadarBets, RADAR_BETS_KEY } from "@/features/hypothesis";
import { useRequest } from "@/shared/lib/useRequest";
import { Button } from "@/shared/ui/Button";

export function SignalNext({
  candidateId,
  query,
  topic,
  name,
}: {
  candidateId: string;
  query: string;
  topic?: string;
  name: string;
}) {
  const { t } = useTranslation("trend");
  const bets = useRequest(RADAR_BETS_KEY, listRadarBets, { cacheMs: 60_000 });
  const own = (bets.data ?? []).filter((hypothesis) => {
    const radar = hypothesis.detail.radar;
    return (
      radar?.bet &&
      radar.candidate_id === candidateId &&
      (topic ? radar.topic === topic : radar.query === query) &&
      hypothesis.status !== "archived" &&
      hypothesis.status !== "rejected"
    );
  });
  return (
    <section className="space-y-2 border-t pt-4 print:hidden">
      {own.length > 0 && (
        <>
          <h4 className="text-sm font-medium">{t("report.next.bets")}</h4>
          <ul className="space-y-1.5 text-sm">
            {own.map((hypothesis) => (
              <li key={hypothesis.id}>
                <Link to={`/hypotheses/${hypothesis.id}`} className="font-medium hover:underline">
                  {hypothesis.title}
                </Link>
                <span className="block text-caption text-muted-foreground">
                  {t(`report.next.status.${hypothesis.detail.radar?.bet?.status ?? "unchanged"}`)}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
      <Button variant="link" size="inline" asChild>
        <Link to={`/search?${new URLSearchParams({ q: name })}`}>{t("report.next.ownBase")}</Link>
      </Button>
    </section>
  );
}
