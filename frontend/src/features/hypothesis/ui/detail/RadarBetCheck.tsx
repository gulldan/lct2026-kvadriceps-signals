import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { checkRadarBet, getHypothesis, type ApiHypothesis } from "@/features/hypothesis/api";
import { Button } from "@/shared/ui/Button";
import { toast } from "@/shared/ui/Sonner";

export function RadarBetCheck({
  hypothesis,
  onChanged,
}: {
  hypothesis: ApiHypothesis;
  onChanged?: (updated: ApiHypothesis) => void;
}) {
  const { t } = useTranslation("hypothesisDetail");
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    try {
      const report = await checkRadarBet(hypothesis.id);
      const count = report.changed.reduce((sum, row) => sum + row.changes.length, 0);
      if (!report.checked) {
        toast.warning(t("provenance.betNoAnswer"));
        return;
      }
      toast.success(t("provenance.betChecked", { count }));
      onChanged?.(await getHypothesis(hypothesis.id));
    } catch {
      toast.error(t("provenance.betCheckFailed"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button variant="outline" size="sm" disabled={busy} onClick={() => void run()}>
      <RefreshCw aria-hidden />
      {t("provenance.betCheck")}
    </Button>
  );
}
