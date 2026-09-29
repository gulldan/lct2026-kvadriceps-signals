import { Badge } from "@/shared/ui/Badge";
import { statusTone, toneClasses } from "@/shared/design-system/tones";
import { i18n } from "@/shared/i18n";
import { cn } from "@/shared/lib/cn";
import { DocStatus } from "@/features/document/api";

const STATUS_LABELS: Record<DocStatus, string> = {
  get uploaded() {
    return i18n.t("document:status.uploaded");
  },
  get queued() {
    return i18n.t("document:status.queued");
  },
  get parsing() {
    return i18n.t("document:status.parsing");
  },
  get ocr() {
    return i18n.t("document:status.ocr");
  },
  get parsed() {
    return i18n.t("document:status.parsed");
  },
  get chunking() {
    return i18n.t("document:status.chunking");
  },
  get indexed() {
    return i18n.t("document:status.indexed");
  },
  get failed() {
    return i18n.t("document:status.failed");
  },
};

function stepOf(status: DocStatus): number {
  switch (status) {
    case "uploaded":
      return 0;
    case "queued":
      return 1;
    case "parsing":
    case "ocr":
      return 2;
    case "parsed":
    case "chunking":
      return 3;
    case "indexed":
      return 4;
    case "failed":
      return -1;
  }
}

const STEPS_TOTAL = 5;

export function PipelineStepper({
  status,
  statusMsg,
  progress,
  className,
}: {
  status: DocStatus;
  statusMsg?: string;
  progress?: { current: number; total: number };
  className?: string;
}) {
  const step = stepOf(status);
  const tone = statusTone(status, "brand");
  if (step < 0) {
    return (
      <span className={cn("inline-flex max-w-full items-center gap-2", className)}>
        <Badge variant={tone}>{STATUS_LABELS.failed}</Badge>
        {statusMsg && (
          <span className="truncate text-xs text-muted-foreground" title={statusMsg}>
            {statusMsg}
          </span>
        )}
      </span>
    );
  }
  if (status === "indexed")
    return (
      <span className={cn("inline-flex max-w-full items-center gap-2", className)}>
        <Badge variant="ok">{STATUS_LABELS[status]}</Badge>
      </span>
    );
  return (
    <span
      className={cn(
        "inline-flex flex-col items-start gap-1.5 sm:flex-row sm:items-center sm:gap-2.5",
        className,
      )}
    >
      <span className="flex items-center gap-[3px]" aria-hidden>
        {Array.from({ length: STEPS_TOTAL }, (_, i) => (
          <span
            key={i}
            className={cn(
              "h-2 w-3 rounded-sm",
              i < step && toneClasses.brand.marker,
              i === step && cn(toneClasses[tone].marker, "animate-pulse"),
              i > step && "bg-border",
            )}
          />
        ))}
      </span>
      <span
        className={cn(
          "order-first max-w-28 text-xs whitespace-normal sm:order-last sm:max-w-none sm:whitespace-nowrap",
          "text-foreground",
        )}
      >
        {STATUS_LABELS[status]}
        {progress && progress.total > 0 && (
          <span className="ml-1 font-mono text-muted-foreground">
            {progress.current}/{progress.total}
          </span>
        )}
      </span>
    </span>
  );
}
