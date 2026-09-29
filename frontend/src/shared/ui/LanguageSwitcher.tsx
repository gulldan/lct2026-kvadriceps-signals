import { Check, Languages } from "lucide-react";
import { useTranslation } from "react-i18next";

import { LANGUAGES, currentLanguage, setLanguage } from "@/shared/i18n";
import { Button } from "@/shared/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";
import { cn } from "@/shared/lib/cn";

export function LanguageSwitcher({ className }: { className?: string }) {
  const { t } = useTranslation("app");
  const active = currentLanguage();
  const label = LANGUAGES.find((l) => l.code === active)?.nativeLabel;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className={cn("gap-2", className)}
          title={t("language")}
          aria-label={label}
        >
          <Languages className="size-4" aria-hidden />
          <span className="hidden sm:inline">{label}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {LANGUAGES.map((l) => (
          <DropdownMenuItem key={l.code} onSelect={() => setLanguage(l.code)}>
            <span className="flex-1">{l.nativeLabel}</span>
            {l.code === active && <Check className="size-4 text-brand" aria-hidden />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
