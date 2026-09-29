import { Radar } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { LanguageSwitcher } from "@/shared/ui/LanguageSwitcher";
import { ThemeSwitcher } from "@/shared/ui/ThemeSwitcher";

/** Вход и служебные страницы сохраняют навигацию даже без рабочей сессии. */
export function StandaloneLayout({
  children,
  preview = false,
}: {
  children: ReactNode;
  preview?: boolean;
}) {
  const { t } = useTranslation();
  const Content = preview ? "div" : "main";
  return (
    <div className={cn("flex flex-col bg-background", !preview && "min-h-svh")}>
      <header className="mx-auto flex w-full max-w-6xl items-center justify-between gap-4 px-5 py-5 sm:px-8 sm:py-7">
        <a
          href="/"
          className="flex min-w-0 items-center gap-3 rounded-md focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
        >
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Radar className="size-5" aria-hidden />
          </span>
          <span className="text-sm leading-tight font-semibold sm:text-base">
            {t("brand", { ns: "app" })}
          </span>
        </a>
        <div className="flex shrink-0 items-center gap-1">
          <LanguageSwitcher />
          <ThemeSwitcher />
        </div>
      </header>
      <Content className="mx-auto flex w-full max-w-6xl flex-1 flex-col justify-center px-5 py-8 sm:px-8 sm:py-12">
        {children}
      </Content>
      <footer className="mx-auto w-full max-w-6xl px-5 sm:px-8">
        <p className="border-t py-5 text-xs leading-relaxed text-muted-foreground">
          {t("standalone.footer")}
        </p>
      </footer>
    </div>
  );
}
