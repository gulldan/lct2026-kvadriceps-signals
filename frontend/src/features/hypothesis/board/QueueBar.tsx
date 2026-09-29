import { Chip } from "@/shared/ui/Chip";
import type { QueueKey } from "@/features/hypothesis/board/model";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";

export function QueueFilter({
  queues,
  active,
  onSelect,
}: {
  queues: { key: QueueKey; label: string; hint: string; count: number }[];
  active: QueueKey;
  onSelect: (key: QueueKey) => void;
}) {
  return (
    <div className="inline-flex flex-wrap gap-2">
      {queues.map((q) => {
        const on = q.key === active;
        return (
          <Tooltip key={q.key}>
            <TooltipTrigger asChild>
              <Chip type="button" onClick={() => onSelect(q.key)} aria-pressed={on}>
                {q.label}
                <span className="font-mono text-xs tabular-nums">{q.count}</span>
              </Chip>
            </TooltipTrigger>
            <TooltipContent>{q.hint}</TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}
