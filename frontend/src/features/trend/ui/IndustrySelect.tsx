import { useTranslation } from "react-i18next";
import {
  researchCategoryLabel,
  researchTaxonomy,
  sortedResearchCategories,
} from "@/shared/lib/researchTaxonomy";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";

export function IndustrySelect({
  onChoose,
  disabled,
  compact = false,
}: {
  onChoose: (query: string) => void;
  disabled?: boolean;
  compact?: boolean;
}) {
  const { t, i18n } = useTranslation("trend");
  return (
    <Select
      value=""
      disabled={disabled}
      onValueChange={(id) => {
        const category = researchTaxonomy.categories.find((item) => item.id === id);
        if (category)
          onChoose(
            t("study.live.industryQuery", {
              industry: researchCategoryLabel(category, i18n.language),
            }),
          );
      }}
    >
      <SelectTrigger
        className={compact ? "w-auto max-w-full" : "w-72 max-w-full"}
        aria-label={t("study.live.chooseIndustry")}
      >
        <SelectValue
          placeholder={t(compact ? "study.live.industryCatalog" : "study.live.allIndustries")}
        />
      </SelectTrigger>
      <SelectContent>
        {sortedResearchCategories(researchTaxonomy.categories, i18n.language).map((category) => (
          <SelectItem key={category.id} value={category.id}>
            {researchCategoryLabel(category, i18n.language)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
