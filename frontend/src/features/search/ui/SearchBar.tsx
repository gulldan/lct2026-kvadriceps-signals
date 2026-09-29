import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight } from "lucide-react";
import { SearchField } from "@/shared/ui/SearchField";
import { Button } from "@/shared/ui/Button";

interface SearchBarProps {
  initialValue?: string;
  autoFocus?: boolean;
  onSearch: (query: string) => void;
}

export function SearchBar({ initialValue = "", autoFocus, onSearch }: SearchBarProps) {
  const { t } = useTranslation("search");
  const [value, setValue] = useState(initialValue);
  useEffect(() => {
    setValue(initialValue);
  }, [initialValue]);
  const submit = (query: string) => {
    const q = query.trim();
    if (!q) return;
    onSearch(q);
  };
  return (
    <form
      className="flex w-full flex-col gap-3 sm:flex-row sm:items-center"
      onSubmit={(event) => {
        event.preventDefault();
        submit(value);
      }}
    >
      <SearchField
        ariaLabel={t("bar.queryAriaLabel")}
        autoFocus={autoFocus}
        value={value}
        placeholder={t("bar.placeholder")}
        className="min-w-0 flex-1"
        onChange={setValue}
      />

      <Button type="submit" className="self-start" disabled={!value.trim()}>
        {t("bar.submit")}
        <ArrowRight className="size-4" aria-hidden />
      </Button>
    </form>
  );
}
