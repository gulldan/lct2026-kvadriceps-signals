import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { SearchField } from "@/shared/ui/SearchField";

export function LiveQueryBar({
  query,
  onSearch,
}: {
  query: string;
  onSearch: (value: string) => void;
}) {
  const { t } = useTranslation("trend");
  const [value, setValue] = useState(query);
  const next = value.trim();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (next && next !== query) onSearch(next);
  };
  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-2 sm:flex-row sm:items-center print:hidden"
    >
      <SearchField
        className="min-w-0 flex-1"
        value={value}
        onChange={setValue}
        placeholder={t("study.live.searchPlaceholder")}
        ariaLabel={t("study.live.searchLabel")}
      />
      <Button type="submit" disabled={!next || next === query}>
        {t("study.live.searchButton")}
      </Button>
    </form>
  );
}
