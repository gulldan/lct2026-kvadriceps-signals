import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { CardContent } from "@/shared/ui/Card";

/** Колонки считает ширина сетки, а не окна: при открытом меню карточки иначе сжимались бы. */
export function ResultGrid({
  ordered,
  tiles,
  className,
  children,
}: {
  ordered?: boolean;
  tiles?: boolean;
  className?: string;
  children: ReactNode;
}) {
  const List = ordered ? "ol" : "ul";
  return (
    <List
      className={cn(
        "grid gap-4 print:grid-cols-1",
        tiles
          ? "grid-cols-[repeat(auto-fill,minmax(min(100%,15rem),1fr))]"
          : "grid-cols-[repeat(auto-fill,minmax(min(100%,19rem),1fr))]",
        className,
      )}
    >
      {children}
    </List>
  );
}

export function ResultItem({ children }: { children: ReactNode }) {
  return <li className="flex min-w-0">{children}</li>;
}

export function ResultCard({
  chips,
  corner,
  title,
  summary,
  footer,
  children,
}: {
  chips?: ReactNode;
  corner?: ReactNode;
  title: ReactNode;
  summary?: string;
  footer?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <CardContent className="flex flex-1 flex-col gap-3 p-4">
      {(chips || corner) && (
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">{chips}</div>
          {corner}
        </div>
      )}
      <div className="min-w-0">
        <h3 className="line-clamp-2 text-sm leading-snug font-medium">{title}</h3>
        {summary && <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">{summary}</p>}
      </div>
      {children}
      {footer && (
        <div className="mt-auto flex flex-wrap items-center gap-x-4 gap-y-1 border-t pt-2.5 text-xs text-muted-foreground">
          {footer}
        </div>
      )}
    </CardContent>
  );
}

export function ResultMeta({
  icon: Icon,
  label,
  value,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
}) {
  return (
    <span className="flex items-center gap-1.5 tabular-nums">
      <Icon className="size-3.5 shrink-0" aria-hidden />
      {label.toLocaleLowerCase()} {value}
    </span>
  );
}

/** Обёртка — CardButton, поэтому действие внизу — подпись, а не вторая кнопка. */
export function ResultTile({
  media,
  headline,
  title,
  note,
  meta,
  action,
}: {
  media?: ReactNode;
  headline?: ReactNode;
  title: ReactNode;
  note?: ReactNode;
  meta?: ReactNode;
  action?: string;
}) {
  return (
    <CardContent className="flex flex-1 flex-col gap-1.5 p-3">
      {media && <div className="mb-1.5">{media}</div>}
      {headline && <div className="flex min-h-6 min-w-0 items-center gap-2">{headline}</div>}
      <h3 className="line-clamp-2 min-h-[2lh] font-heading text-lg leading-snug font-bold tracking-tight text-balance">
        {title}
      </h3>
      {/* Пометка со значком выше пометки без него: жёсткая высота держит ряд вровень. */}
      <p className="h-4 truncate text-caption leading-4 font-medium">{note}</p>
      <p className="min-h-5 truncate text-caption text-muted-foreground">{meta}</p>
      {action && (
        <Button asChild className="mt-1.5 w-full">
          <span>{action}</span>
        </Button>
      )}
    </CardContent>
  );
}

export function ResultTileMedia({
  children,
  topLeft,
  topRight,
  bottomLeft,
}: {
  children?: ReactNode;
  topLeft?: ReactNode;
  topRight?: ReactNode;
  bottomLeft?: ReactNode;
}) {
  const chips =
    "absolute flex flex-col items-start gap-1 [&>*]:max-w-full [&>*]:truncate [&>*]:font-semibold [&>*]:shadow-sm";
  return (
    <div className="relative aspect-[4/3] overflow-hidden rounded-xl bg-document-paper ring-1 ring-border">
      {children}
      {topLeft && (
        <div className={cn(chips, "top-2 left-2 max-w-[calc(100%-5rem)]")}>{topLeft}</div>
      )}
      {topRight && <div className={cn(chips, "top-2 right-2 items-end")}>{topRight}</div>}
      {bottomLeft && (
        <div className={cn(chips, "bottom-2 left-2 max-w-[calc(100%-1rem)]")}>{bottomLeft}</div>
      )}
    </div>
  );
}
