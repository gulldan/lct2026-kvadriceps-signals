import { InfoHint } from "@/shared/ui/InfoHint";

export interface CounterItem {
  label: string;
  value: string;
  hint?: string;
}

export function CounterStrip({ items }: { items: CounterItem[] }) {
  return (
    <dl className="grid grid-cols-[repeat(auto-fit,minmax(6.5rem,1fr))] gap-x-4 gap-y-3">
      {items.map((item) => (
        <div key={item.label} className="row-span-2 grid min-w-0 grid-rows-subgrid gap-y-1">
          <dt className="text-sm text-muted-foreground">
            {item.label}
            {item.hint && (
              <InfoHint label={item.label} className="ml-0.5 align-middle">
                {item.hint}
              </InfoHint>
            )}
          </dt>
          <dd className="text-xl leading-none font-semibold tabular-nums">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}
