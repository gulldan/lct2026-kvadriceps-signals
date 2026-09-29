// Графики на Canvas перечитывают токены по событию темы.
import { useEffect, useState } from "react";
import { Check, Monitor, Moon, Sun } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  currentTheme,
  setTheme,
  THEMES,
  THEME_EVENT,
  watchSystemTheme,
  type Theme,
} from "@/shared/theme";
import { Button } from "@/shared/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";
import { cn } from "@/shared/lib/cn";

const ICONS = { system: Monitor, light: Sun, dark: Moon } as const;

export function ThemeSwitcher({ className }: { className?: string }) {
  const { t } = useTranslation("app");
  const [active, setActive] = useState<Theme>(currentTheme);
  useEffect(() => {
    const sync = () => setActive(currentTheme());
    window.addEventListener(THEME_EVENT, sync);
    const stop = watchSystemTheme();
    return () => {
      window.removeEventListener(THEME_EVENT, sync);
      stop();
    };
  }, []);
  const Icon = ICONS[active];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className={className}
          aria-label={t("theme.label")}
          title={t("theme.label")}
        >
          <Icon className="size-4" aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {THEMES.map((theme) => {
          const ItemIcon = ICONS[theme];
          return (
            <DropdownMenuItem key={theme} onSelect={() => setTheme(theme)}>
              <ItemIcon className="size-4 text-muted-foreground" aria-hidden />
              <span className="flex-1">{t(`theme.${theme}`)}</span>
              <Check
                className={cn("size-4 text-brand", theme !== active && "invisible")}
                aria-hidden
              />
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
