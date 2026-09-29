import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";
import { getKPI, linkThesisSearch } from "@/features/kpi";
import { reportError } from "@/shared/lib/monitor";
import { useRequest } from "@/shared/lib/useRequest";
import { Button } from "@/shared/ui/Button";

/** Пишется каждый запрос, пока тезис в адресе: тезис помнит уточнения формулировки. */
export function ThesisLink({ thesisId, query }: { thesisId: string; query: string }) {
  const { t } = useTranslation("kpi");
  const thesis = useRequest(`kpi:${thesisId}`, (signal) => getKPI(thesisId, signal));
  useEffect(() => {
    if (!query) return;
    // При следующем открытии связь допишется.
    linkThesisSearch(thesisId, query).catch((error: unknown) => reportError("kpi.link", error));
  }, [thesisId, query]);
  if (!thesis.data) return null;
  return (
    <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
      <span>{t("signals.openedFrom", { title: thesis.data.title })}</span>
      <Button variant="link" size="inline" asChild>
        <Link to={`/kpi/${thesisId}`}>
          <ArrowLeft className="size-4" aria-hidden />
          {t("signals.backToThesis")}
        </Link>
      </Button>
    </p>
  );
}
