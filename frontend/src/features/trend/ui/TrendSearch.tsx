import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { SearchField } from "@/shared/ui/SearchField";

export function TrendSearch({
  value,
  onChange,
  onSearch,
  placeholder,
  label = placeholder,
  disabled = false,
  allowEmpty = false,
}: {
  value: string;
  onChange: (value: string) => void;
  onSearch: (value: string) => void;
  placeholder: string;
  label?: string;
  disabled?: boolean;
  allowEmpty?: boolean;
}) {
  const { t } = useTranslation("trend");
  return (
    <form
      className="flex min-w-0 items-center gap-2"
      onSubmit={(event) => {
        event.preventDefault();
        if ((value.trim() || allowEmpty) && !disabled) onSearch(value.trim());
      }}
    >
      <SearchField
        className="min-w-0 flex-1"
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        ariaLabel={label}
      />
      <Button type="submit" disabled={(!value.trim() && !allowEmpty) || disabled}>
        {t("study.live.searchButton")}
      </Button>
    </form>
  );
}
