import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Segmented } from "@/shared/ui/Segmented";

export function TrendViewSwitch({ value }: { value: "live" | "study" }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const { t } = useTranslation("trend");
  const keep = (view: string) => {
    const next = new URLSearchParams(params);
    if (view === "study") next.set("view", "study");
    else next.delete("view");
    const query = next.toString();
    return query ? `/trends?${query}` : "/trends";
  };
  return (
    <Segmented
      size="md"
      aria-label={t("study.live.view")}
      value={value}
      onChange={(view) => navigate(keep(view))}
      options={[
        { value: "live", label: t("study.live.view") },
        { value: "study", label: t("study.live.viewStudy") },
      ]}
    />
  );
}
