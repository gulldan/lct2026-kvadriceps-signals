import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Braces, Download, FileSpreadsheet, FileText, Printer } from "lucide-react";
import { downloadFile } from "@/shared/api/client";
import { Button } from "@/shared/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";
import { toast } from "@/shared/ui/Sonner";
import { cn } from "@/shared/lib/cn";
import type { ReportDoc } from "@/shared/report/blocks";

export type ExportItem = {
  run: () => void | Promise<unknown>;
  disabled?: boolean;
} & ({ format: "csv" | "json" } | { label: string; icon: ReactNode });

const FORMAT_ICONS = {
  csv: <FileSpreadsheet className="text-ok" aria-hidden />,
  json: <Braces className="text-warn" aria-hidden />,
};

/** Файл сервера оставляет неизвестное пустой ячейкой с причиной, а не нулём. */
export function serverFiles(
  path: string,
  name: string,
  params: Record<string, string> = {},
): ExportItem[] {
  return (["csv", "json"] as const).map((format) => ({
    format,
    run: () =>
      downloadFile(`${path}?${new URLSearchParams({ ...params, format })}`, `${name}.${format}`),
  }));
}

/** Документ — функцией: нужен только в момент выбора, а отчёту гипотезы — ещё и ревизии из сети. */
export function ReportExport({
  doc,
  onPrint,
  items,
  disabled,
  className,
}: {
  doc?: () => ReportDoc | Promise<ReportDoc>;
  onPrint?: () => void;
  items?: ExportItem[];
  disabled?: boolean;
  className?: string;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const choose = (item: ExportItem) => {
    const job = item.run();
    if (!job) return;
    setBusy(true);
    toast.promise(
      job.finally(() => setBusy(false)),
      {
        loading: t("ui.export.building"),
        success: t("ui.export.ready"),
        error: t("ui.export.failed"),
      },
    );
  };
  const docItems: ExportItem[] = doc
    ? [
        {
          label: t("ui.export.word"),
          icon: <FileText className="text-integration-word" aria-hidden />,
          run: async () => {
            const { downloadBlocksDocx } = await import("@/shared/report/docx");
            await downloadBlocksDocx(await doc());
          },
        },
        {
          label: t("ui.export.print"),
          icon: <Printer className="text-muted-foreground" aria-hidden />,
          run: onPrint ?? (() => window.print()),
        },
      ]
    : [];
  const row = (item: ExportItem) => {
    const label = "format" in item ? t(`ui.export.${item.format}`) : item.label;
    return (
      <DropdownMenuItem key={label} disabled={item.disabled} onSelect={() => choose(item)}>
        {"format" in item ? FORMAT_ICONS[item.format] : item.icon}
        {label}
      </DropdownMenuItem>
    );
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          disabled={disabled || busy}
          className={cn("print:hidden", className)}
        >
          <Download aria-hidden />
          {t("actions.export")}
        </Button>
      </DropdownMenuTrigger>
      {/* Печать вызывается прямо из меню: без этого класса открытое меню попало бы на лист. */}
      <DropdownMenuContent align="end" className="print:hidden">
        {items?.map(row)}
        {!!items?.length && docItems.length > 0 && <DropdownMenuSeparator />}
        {docItems.map(row)}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
