import { cn } from "@/shared/lib/cn";

export function ValueScale({
  value,
  label,
  className,
}: {
  value: number;
  label: string;
  className?: string;
}) {
  return (
    <div className={cn("relative", className)}>
      <meter
        min={0}
        max={100}
        value={Math.max(0, Math.min(100, value))}
        aria-label={label}
        className="value-scale block h-1.5 w-full overflow-hidden rounded-sm bg-muted"
      />
      <span
        className="absolute -top-0.5 bottom-[-2px] left-1/2 w-px bg-foreground/45"
        aria-hidden
      />
    </div>
  );
}
